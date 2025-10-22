package client

import (
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	gows "github.com/gorilla/websocket"
	"github.com/mickaelvieira/websocket"
)

type Client interface {
	websocket.Socket

	// Channel to receive status updates
	Statuses() <-chan Status

	// IsConnected returns true if the websocket connection is established
	IsConnected() bool
}

// NewClientSocket provides a new websocket client that will connect to the server
// at the given URI. It will panic if the URI is invalid.
// The client will automatically attempt to reconnect on disconnections.
func NewClientSocket(u string, opts ...OptionModifier) Client {
	uri, err := url.ParseRequestURI(u)
	if err != nil {
		log.Fatalf("invalid server URI %s", err)
		return nil
	}

	wc := &client{
		id:             websocket.GenId(),
		uri:            uri,
		state:          disconnected,
		states:         make(chan state),
		statuses:       make(chan Status),
		outbound:       make(chan websocket.OutboundMessage),
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
		// wc.statuses <- StatusDisconnected
		wc.states <- connecting
	}()

	return wc
}

type client struct {
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

	// outgoing messages to the peer
	outbound chan websocket.OutboundMessage

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

func (c *client) handle() {

	for s := range c.states {
		c.lock.Lock()
		p := c.state
		c.state = s
		c.lock.Unlock()

		if s != p {
			c.logger.Debug("state changed", "from", p.String(), "to", s.String())
		}

		switch s {
		case connecting:
			c.logger.Debug("attempting to connect", "uri", c.uri.String())
			go c.connect()

		case connected:
			c.logger.Debug("connection established")

			c.retryAttempts = 0 // Reset retry attempts on successful connection
			c.statuses <- statusConnected

			go c.read()  // read incoming messages
			go c.write() // write outgoing messages

		case reconnecting:
			c.retryAttempts++
			c.logger.Info("attempting to reconnect", "attempt", c.retryAttempts, "max", c.options.maxRetryAttempts)

			if c.retryAttempts >= c.options.maxRetryAttempts {
				c.logger.Info("max retry attempts reached, giving up")

				c.retryAttempts = 0
				c.statuses <- statusDisconnected

				return // Exit the loop for final disconnection
			} else {
				// Schedule next connection attempt without blocking the state machine
				go func() {
					time.Sleep(c.options.retryInterval)
					c.states <- connecting
				}()
			}

		case closing:
			c.logger.Debug("closing connection")

			// Wait for close acknowledgment or timeout
			go c.waitForCloseAck()

		case disconnected:
			c.logger.Debug("disconnected")

			c.statuses <- statusDisconnected
			c.retryAttempts = 0

			return // Exit the loop for final disconnection
		}
	}
}

func (c *client) connect() {
	c.lock.Lock()
	defer c.lock.Unlock()

	conn, _, err := c.dialer.Dial(c.uri.String(), c.options.headers)
	if err != nil {
		c.logger.Error("connection failure", "uri", c.uri.String(), "error", err, "attempt", c.retryAttempts+1)

		c.conn = nil

		// Connection refused/timeout -> go to reconnecting state
		c.states <- reconnecting
		return
	}

	defaultHandler := conn.CloseHandler()
	conn.SetCloseHandler(func(code int, text string) error {
		c.logger.Info("connection closed by peer", "code", code, "text", text)

		// Signal close acknowledgment without blocking
		select {
		case c.closeAck <- true:
		default:
			// No one is waiting for close ack, that's okay
		}

		if code == gows.CloseNormalClosure || code == gows.CloseGoingAway {
			c.states <- disconnected // Expected close from peer
		} else {
			c.states <- reconnecting // Unexpected close or error
		}

		return defaultHandler(code, text)
	})

	conn.SetReadLimit(c.options.readLimit)

	conn.SetPongHandler(func(appData string) error {
		if appData != c.id {
			if err := c.closeWithCode(gows.CloseInvalidFramePayloadData, "invalid pong payload"); err != nil {
				c.logger.Error("failed to close connection on invalid pong", "error", err)
			}
		}
		return nil
	})

	c.conn = conn
	c.states <- connected
}

// closeWithCode initiates a close with a specific code and enters CLOSING state
func (c *client) closeWithCode(code int, reason string) error {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.logger.Info("initiating close", "code", code, "reason", reason)

	if c.conn == nil {
		return nil
	}

	// inform the remote peer that we are closing the connection
	m := gows.FormatCloseMessage(code, reason)
	t := time.Now().Add(c.options.writeWait)

	if err := c.conn.WriteControl(gows.CloseMessage, m, t); err != nil {
		c.logger.Error("failed to send close frame", "error", err)

		c.states <- reconnecting

		return err
	}

	// Enter CLOSING state and wait for the remote peer's acknowledgment
	c.states <- closing

	return nil
}

// waitForCloseAck waits for close acknowledgment or timeout
// https://datatracker.ietf.org/doc/html/rfc6455#section-7.1.2
func (c *client) waitForCloseAck() {
	timer := time.NewTimer(10 * time.Second) // 10 second timeout
	defer timer.Stop()

	select {
	case <-c.closeAck:
		c.logger.Debug("close acknowledgment received")
		// the remote peer acknowledged the close
		// we can safely consider the connection closed
		c.states <- disconnected
	case <-timer.C:
		c.logger.Debug("close acknowledgment timeout")
		// the remote peer did not acknowledge the close in time
		// we consider the connection closed
		c.states <- disconnected
	}
}

// cleanup closes all channels and cleans up resources
func (c *client) cleanup() {
	c.lock.Lock()
	defer c.lock.Unlock()

	c.logger.Info("cleaning up client resources")

	// close connection if it exists
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			c.logger.Error("error closing connection during cleanup", "error", err)
		}
		c.conn = nil
	}

	// informs consumers that we will no longer send statuses or messages
	close(c.statuses)
	close(c.textMessages)
	close(c.binaryMessages)
}

func (c *client) read() {
	for {
		if !c.IsConnected() {
			return
		}

		t, m, err := c.conn.ReadMessage()
		if err != nil {
			c.logger.Error("read error", "error", err)

			// Check if this is an unexpected close error
			if gows.IsUnexpectedCloseError(err, gows.CloseNormalClosure, gows.CloseGoingAway) {
				c.states <- reconnecting
			}

			return
		}

		// https://datatracker.ietf.org/doc/html/rfc6455#section-5.6
		switch t {
		case gows.TextMessage:
			c.textMessages <- string(m)
		case gows.BinaryMessage:
			c.binaryMessages <- m
		}
	}
}

func (c *client) write() {
	ticker := time.NewTicker(c.options.pingInterval)
	defer func() {
		ticker.Stop()
	}()

	for {
		select {
		case m, ok := <-c.outbound:

			if !ok {
				// just exist the write loop
				// when outbound channel is closed
				return
			}

			if err := c.conn.SetWriteDeadline(time.Now().Add(c.options.writeWait)); err != nil {
				c.logger.Error("deadline error", "error", err)
			}
			if err := c.conn.WriteMessage(m.DataType, m.Data); err != nil {
				c.logger.Error("write error", "error", err)
				return
			}
		case <-ticker.C:
			d := []byte(c.id)
			t := time.Now().Add(c.options.writeWait)

			c.logger.Debug("pinging client", "data", d, "internal", c.options.pingInterval)

			if err := c.conn.WriteControl(gows.PingMessage, d, t); err != nil {
				c.logger.Error("ping error", "error", err)
				return
			}
		}
	}
}

// ID returns the unique identifier of the websocket client
func (c *client) Id() string {
	return c.id
}

// IsConnected returns true if the websocket connection is established
func (c *client) IsConnected() bool {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.conn != nil && c.state == connected
}

// Statuses returns a channel to receive status updates
func (c *client) Statuses() <-chan Status {
	return c.statuses
}

// ReadTextMessages returns a channel to receive text messages from the websocket server
func (c *client) ReadTextMessages() <-chan string {
	return c.textMessages
}

// SendTextMessage sends a text message to the websocket server
func (s *client) SendTextMessage(d string) {
	s.outbound <- websocket.OutboundMessage{
		Data:     []byte(d),
		DataType: gows.TextMessage,
	}
}

// ReadBinaryMessages returns a channel to receive binary messages from the websocket server
func (c *client) ReadBinaryMessages() <-chan []byte {
	return c.binaryMessages
}

// SendBinaryMessage sends a binary message to the websocket server
func (s *client) SendBinaryMessage(d []byte) {
	s.outbound <- websocket.OutboundMessage{
		Data:     d,
		DataType: gows.BinaryMessage,
	}
}

// Close the websocket connection gracefully
func (c *client) Close() error {
	// Initiate graceful close
	if err := c.closeWithCode(gows.CloseNormalClosure, ""); err != nil {
		// If close fails, cleanup anyway
		c.cleanup()
		return err
	}

	// Wait a bit for the close to complete, then cleanup
	go func() {
		time.Sleep(2 * time.Second)
		c.cleanup()
	}()

	return nil
}
