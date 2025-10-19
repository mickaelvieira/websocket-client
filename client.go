package ws_client

import (
	"errors"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	gows "github.com/gorilla/websocket"
)

var (
	ErrNotConnected = errors.New("websocket client not connected")
)

type WebsocketClient interface {
	// Channel to receive status updates
	Statuses() <-chan Status

	// Channel to receive text messages from the websocket server
	ReadTextMessages() <-chan string

	// Send a text message to the websocket server
	SendTextMessage(string) error

	// Channel to receive binary messages from the websocket server
	ReadBinaryMessages() <-chan []byte

	// Send a binary message to the websocket server
	SendBinaryMessage([]byte) error

	// Close the websocket connection
	Close() error

	// IsConnected returns true if the websocket connection is established
	IsConnected() bool
}

// NewClient creates a new websocket client connecting to the given URI.
// It will panic if the URI is invalid.
func NewClient(u string, opts ...OptionModifier) WebsocketClient {
	uri, err := url.ParseRequestURI(u)
	if err != nil {
		log.Fatalf("invalid server URI %s", err)
		return nil
	}

	wc := &websocketClient{
		id:             genId(),
		uri:            uri,
		state:          disconnected,
		states:         make(chan state),
		statuses:       make(chan Status),
		textMessages:   make(chan string),
		binaryMessages: make(chan []byte),
		closeAck:       make(chan bool, 1),
		retryAttempts:  0,
		options:        &defaultOptions,
		dialer: &gows.Dialer{
			Proxy:            http.ProxyFromEnvironment,
			HandshakeTimeout: 45 * time.Second,
		},
	}

	for _, opt := range opts {
		opt(wc.options)
	}

	if wc.options.dialerModifier != nil {
		wc.options.dialerModifier(wc.dialer)
	}

	if wc.options.logger != nil {
		wc.logger = wc.options.logger
	}

	go wc.handle()

	// Start the state machine with initial connection attempt
	go func() {
		// Send initial disconnected status
		time.Sleep(10 * time.Millisecond) // Small delay to ensure handler is ready
		wc.statuses <- StatusDisconnected
		wc.states <- connecting
	}()

	return wc
}

type websocketClient struct {
	// internal unique client
	id string

	// the websocket server URI
	uri *url.URL

	// dialer is used to create new websocket connections
	dialer *gows.Dialer

	// logger for logging client events
	logger *slog.Logger

	// client's options
	options *options

	// internal machine status handling client operations
	states chan state

	// channels used to broadcast status changes
	statuses chan Status

	// incoming binary messages from the peer
	binaryMessages chan []byte

	// incoming text messages from the peer
	textMessages chan string

	// mutex to protect concurrent access to the client
	lock sync.RWMutex

	// the websocket connection
	conn *gows.Conn

	// current client state
	state state

	// current retry attempt count
	retryAttempts int

	// channel to signal close acknowledgment
	closeAck chan bool
}

func (wc *websocketClient) handle() {

	for s := range wc.states {
		wc.lock.Lock()
		p := wc.state
		wc.state = s
		wc.lock.Unlock()

		if s != p {
			wc.logger.Debug("state changed", "from", p.String(), "to", s.String())
		}

		switch s {
		case connecting:
			wc.logger.Debug("attempting to connect", "uri", wc.uri.String())
			go wc.connect()

		case connected:
			wc.logger.Debug("connection established")

			wc.retryAttempts = 0 // Reset retry attempts on successful connection
			wc.statuses <- StatusConnected

			go wc.ping() // regularly ping the server
			go wc.read() // read incoming messages

		case reconnecting:
			wc.retryAttempts++
			wc.logger.Info("attempting to reconnect", "attempt", wc.retryAttempts, "max", wc.options.maxRetryAttempts)

			if wc.retryAttempts >= wc.options.maxRetryAttempts {
				wc.logger.Info("max retry attempts reached, giving up")

				wc.retryAttempts = 0
				wc.statuses <- StatusDisconnected

				return // Exit the loop for final disconnection
			} else {
				// Schedule next connection attempt without blocking the state machine
				go func() {
					time.Sleep(wc.options.retryInterval)
					wc.states <- connecting
				}()
			}

		case closing:
			wc.logger.Debug("closing connection")

			// Wait for close acknowledgment or timeout
			go wc.waitForCloseAck()

		case disconnected:
			wc.logger.Debug("disconnected")

			wc.statuses <- StatusDisconnected
			wc.retryAttempts = 0

			return // Exit the loop for final disconnection
		}
	}
}

func (wc *websocketClient) connect() {
	wc.lock.Lock()
	defer wc.lock.Unlock()

	conn, _, err := wc.dialer.Dial(wc.uri.String(), wc.options.headers)
	if err != nil {
		wc.logger.Error("connection failure", "uri", wc.uri.String(), "error", err, "attempt", wc.retryAttempts+1)

		wc.conn = nil

		// Connection refused/timeout -> go to reconnecting state
		wc.states <- reconnecting
		return
	}

	defaultHandler := conn.CloseHandler()
	conn.SetCloseHandler(func(code int, text string) error {
		wc.logger.Info("connection closed by peer", "code", code, "text", text)

		// Signal close acknowledgment without blocking
		select {
		case wc.closeAck <- true:
		default:
			// No one is waiting for close ack, that's okay
		}

		if code == gows.CloseNormalClosure || code == gows.CloseGoingAway {
			wc.states <- disconnected // Expected close from peer
		} else {
			wc.states <- reconnecting // Unexpected close or error
		}

		return defaultHandler(code, text)
	})

	conn.SetReadLimit(wc.options.readLimit)

	conn.SetPongHandler(func(appData string) error {
		if appData != wc.id {
			if err := wc.closeWithCode(gows.CloseInvalidFramePayloadData, "invalid pong payload"); err != nil {
				wc.logger.Error("failed to close connection on invalid pong", "error", err)
			}
		}
		return nil
	})

	wc.conn = conn
	wc.states <- connected
}

// closeWithCode initiates a close with a specific code and enters CLOSING state
func (wc *websocketClient) closeWithCode(code int, reason string) error {
	wc.lock.Lock()
	defer wc.lock.Unlock()

	wc.logger.Info("initiating close", "code", code, "reason", reason)

	if wc.conn == nil {
		return nil
	}

	// inform the remote peer that we are closing the connection
	m := gows.FormatCloseMessage(code, reason)
	t := time.Now().Add(wc.options.writeWait)

	if err := wc.conn.WriteControl(gows.CloseMessage, m, t); err != nil {
		wc.logger.Error("failed to send close frame", "error", err)

		wc.states <- reconnecting

		return err
	}

	// Enter CLOSING state and wait for the remote peer's acknowledgment
	wc.states <- closing

	return nil
}

// waitForCloseAck waits for close acknowledgment or timeout
// https://datatracker.ietf.org/doc/html/rfc6455#section-7.1.2
func (wc *websocketClient) waitForCloseAck() {
	timer := time.NewTimer(10 * time.Second) // 10 second timeout
	defer timer.Stop()

	select {
	case <-wc.closeAck:
		wc.logger.Debug("close acknowledgment received")
		// the remote peer acknowledged the close
		// we can safely consider the connection closed
		wc.states <- disconnected
	case <-timer.C:
		wc.logger.Debug("close acknowledgment timeout")
		// the remote peer did not acknowledge the close in time
		// we consider the connection closed
		wc.states <- disconnected
	}
}

// cleanup closes all channels and cleans up resources
func (wc *websocketClient) cleanup() {
	wc.lock.Lock()
	defer wc.lock.Unlock()

	wc.logger.Info("cleaning up client resources")

	// close connection if it exists
	if wc.conn != nil {
		if err := wc.conn.Close(); err != nil {
			wc.logger.Error("error closing connection during cleanup", "error", err)
		}
		wc.conn = nil
	}

	// informs consumers that we will no longer send statuses or messages
	close(wc.statuses)
	close(wc.textMessages)
	close(wc.binaryMessages)
}

func (wc *websocketClient) read() {
	for {
		if !wc.IsConnected() {
			return
		}

		t, m, err := wc.conn.ReadMessage()
		if err != nil {
			wc.logger.Error("read error", "error", err)

			// Check if this is an unexpected close error
			if gows.IsUnexpectedCloseError(err, gows.CloseNormalClosure, gows.CloseGoingAway) {
				wc.states <- reconnecting
			}

			return
		}

		// https://datatracker.ietf.org/doc/html/rfc6455#section-5.6
		switch t {
		case gows.TextMessage:
			wc.textMessages <- string(m)
		case gows.BinaryMessage:
			wc.binaryMessages <- m
		}
	}
}

func (wc *websocketClient) write(data []byte, dataType int) error {
	if !wc.IsConnected() {
		return ErrNotConnected
	}

	wc.logger.Debug("writing message", "data_type", dataType, "data_length", len(data))

	if err := wc.conn.SetWriteDeadline(time.Now().Add(wc.options.writeWait)); err != nil {
		return err
	}

	return wc.conn.WriteMessage(dataType, data)
}

// https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API/Writing_WebSocket_servers#pings_and_pongs_the_heartbeat_of_websockets
func (wc *websocketClient) ping() {
	for {
		time.Sleep(wc.options.pingInterval)

		if !wc.IsConnected() {
			wc.logger.Debug("pinging stopped")

			// stop pinging if disconnected
			// it will resume when the connection is re-established
			return
		}

		d := []byte(wc.id)
		t := time.Now().Add(wc.options.writeWait)

		wc.logger.Debug("pinging server", "data", d, "internal", wc.options.pingInterval)

		if err := wc.conn.WriteControl(gows.PingMessage, d, t); err != nil {
			wc.logger.Error("ping error", "error", err)
			// On ping error, close with error and potentially reconnect
			if err := wc.closeWithCode(gows.CloseNoStatusReceived, err.Error()); err != nil {
				wc.logger.Error("failed to close connection on ping error", "error", err)
			}
			return
		}
	}
}

// IsConnected returns true if the websocket connection is established
func (wc *websocketClient) IsConnected() bool {
	wc.lock.RLock()
	defer wc.lock.RUnlock()

	return wc.conn != nil && wc.state == connected
}

// Statuses returns a channel to receive status updates
func (wc *websocketClient) Statuses() <-chan Status {
	return wc.statuses
}

// ReadTextMessages returns a channel to receive text messages from the websocket server
func (wc *websocketClient) ReadTextMessages() <-chan string {
	return wc.textMessages
}

// SendTextMessage sends a text message to the websocket server
func (wc *websocketClient) SendTextMessage(d string) error {
	return wc.write([]byte(d), gows.TextMessage)
}

// ReadBinaryMessages returns a channel to receive binary messages from the websocket server
func (wc *websocketClient) ReadBinaryMessages() <-chan []byte {
	return wc.binaryMessages
}

// SendBinaryMessage sends a binary message to the websocket server
func (wc *websocketClient) SendBinaryMessage(d []byte) error {
	return wc.write(d, gows.BinaryMessage)
}

// Close the websocket connection gracefully
func (wc *websocketClient) Close() error {
	// Initiate graceful close
	if err := wc.closeWithCode(gows.CloseNormalClosure, ""); err != nil {
		// If close fails, cleanup anyway
		wc.cleanup()
		return err
	}

	// Wait a bit for the close to complete, then cleanup
	go func() {
		time.Sleep(2 * time.Second)
		wc.cleanup()
	}()

	return nil
}
