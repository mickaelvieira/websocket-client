package server

import (
	"log/slog"
	"sync"
	"time"

	gows "github.com/gorilla/websocket"
	"github.com/mickaelvieira/websocket-client"
)

type Server interface {
	websocket.Socket

	// Channel to receive close notifications
	Wait() <-chan struct{}
}

func NewServerSocket(conn *gows.Conn, opts ...OptionModifier) Server {
	wc := &server{
		id:             websocket.GenId(),
		conn:           conn,
		wait:           make(chan struct{}),
		outbound:       make(chan websocket.OutboundMessage),
		textMessages:   make(chan string),
		binaryMessages: make(chan []byte),
		options:        &defaultOptions,
	}

	for _, opt := range opts {
		opt(wc.options)
	}

	if wc.options.logger != nil {
		wc.logger = wc.options.logger
	}

	go wc.read()
	go wc.write()

	return wc
}

type server struct {
	// unique peer identifier
	id string

	// logger for logging client events
	logger *slog.Logger

	// client's options
	options *options

	// mutex to protect concurrent writes
	lock sync.Mutex

	// underlying websocket connection
	conn *gows.Conn

	// channel to notify close events
	wait chan struct{}

	// outgoing messages to the peer
	outbound chan websocket.OutboundMessage

	// incoming messages from the peer
	binaryMessages chan []byte

	// incoming text messages from the peer
	textMessages chan string
}

// Id returns the unique identifier of the websocket peer
func (s *server) Id() string {
	return s.id
}

// Wait returns a channel to receive close notifications
func (s *server) Wait() <-chan struct{} {
	return s.wait
}

// ReadBinaryMessages returns a channel to receive binary messages from the websocket server
func (s *server) ReadBinaryMessages() <-chan []byte {
	return s.binaryMessages
}

// SendBinaryMessage sends a binary message to the websocket server
func (s *server) SendBinaryMessage(d []byte) {
	s.outbound <- websocket.OutboundMessage{
		Data:     d,
		DataType: gows.BinaryMessage,
	}
}

// ReadTextMessages returns a channel to receive text messages from the websocket server
func (s *server) ReadTextMessages() <-chan string {
	return s.textMessages
}

// SendTextMessage sends a text message to the websocket server
func (s *server) SendTextMessage(d string) {
	s.outbound <- websocket.OutboundMessage{
		Data:     []byte(d),
		DataType: gows.TextMessage,
	}
}

func (s *server) read() {
	defer func() {
		s.cleanup()
	}()

	s.conn.SetReadLimit(s.options.readLimit)
	if err := s.conn.SetReadDeadline(time.Now().Add(s.options.pongWait)); err != nil {
		s.logger.Error("deadline error", "error", err)
	}
	s.conn.SetPongHandler(func(string) error {
		if err := s.conn.SetReadDeadline(time.Now().Add(s.options.pongWait)); err != nil {
			s.logger.Error("deadline error", "error", err)
		}
		return nil
	})

	for {
		t, d, err := s.conn.ReadMessage()
		if err != nil {
			// when the connection is closed, we'll receive a CloseError
			// we don't really need to log as errors since they are more informative
			if gows.IsUnexpectedCloseError(err, gows.CloseNormalClosure, gows.CloseGoingAway) {
				s.logger.Error("read error", "error", err)
			}
			break
		}

		// https://datatracker.ietf.org/doc/html/rfc6455#section-5.6
		switch t {
		case gows.TextMessage:
			s.textMessages <- string(d)
		case gows.BinaryMessage:
			s.binaryMessages <- d
		}
	}
}

func (c *server) write() {
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

// cleanup closes all channels and cleans up resources
func (s *server) cleanup() {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.logger.Debug("cleaning up", "id", s.id)

	// close connection if it exists
	if s.conn != nil {
		if err := s.conn.Close(); err != nil {
			s.logger.Error("closing error during cleanup", "error", err)
		}
		s.conn = nil
	}

	// informs consumers that we will no longer send statuses or messages
	close(s.outbound)
	close(s.textMessages)
	close(s.binaryMessages)
	close(s.wait)
}

// Close the websocket connection gracefully
func (s *server) Close() error {
	m := gows.FormatCloseMessage(gows.CloseNormalClosure, "")
	t := time.Now().Add(s.options.writeWait)

	s.logger.Info("close connection", "id", s.id)

	// Initiate graceful close
	if err := s.conn.WriteControl(gows.CloseMessage, m, t); err != nil {
		s.logger.Error("close frame failed", "error", err)
		return err
	}

	return nil
}
