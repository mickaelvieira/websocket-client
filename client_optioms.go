package ws_client

import (
	"log/slog"
	"net/http"
	"time"

	gows "github.com/gorilla/websocket"
)

type DialerModifier func(*gows.Dialer)
type OptionModifier func(*options)

// WithRetryInterval sets the interval between reconnection attempts
func WithRetryInterval(d time.Duration) OptionModifier {
	return func(o *options) {
		o.retryInterval = d
	}
}

// WithPingInterval sets the interval between pings to the peer
func WithPingInterval(d time.Duration) OptionModifier {
	return func(o *options) {
		o.pingInterval = d
	}
}

// WithMaxRetryAttempts sets the maximum number of reconnection attempts
func WithMaxRetryAttempts(attempts int) OptionModifier {
	return func(o *options) {
		o.maxRetryAttempts = attempts
	}
}

// WithHeaders sets custom HTTP headers for the websocket connection
func WithHeaders(h http.Header) OptionModifier {
	return func(o *options) {
		o.headers = h
	}
}

// WithLogger allows passing a custom logger for the websocket client
// @see https://pkg.go.dev/log/slog
func WithLogger(l *slog.Logger) OptionModifier {
	return func(o *options) {
		o.logger = l
	}
}

// WithDialerModifier allows customizing the underlying websocket dialer before connecting
// @see https://github.com/gorilla/websocket/blob/main/client.go#L53
func WithDialerModifier(m DialerModifier) OptionModifier {
	return func(o *options) {
		o.dialerModifier = m
	}
}

var defaultOptions = options{
	readWait:         1 * time.Second,
	writeWait:        1 * time.Second,
	pingInterval:     60 * time.Second,
	retryInterval:    5 * time.Second,
	maxRetryAttempts: 60, // 60 attempts every 5s, meaning we try for 5 minutes default
	logger:           slog.New(slog.DiscardHandler),
}

type options struct {
	// logger for logging client events
	logger *slog.Logger

	// optional HTTP headers to include in the connection request
	headers http.Header

	// optional modifier to customize the dialer before connecting
	dialerModifier DialerModifier

	// retryInterval is the delay between reconnection attempts
	retryInterval time.Duration

	// maxRetryAttempts is the maximum number of reconnection attempts
	maxRetryAttempts int

	// writeWait is the time allowed to write a message to the peer
	writeWait time.Duration

	// readWait is the time allowed to read a message from the peer
	readWait time.Duration

	// pingInterval is the interval between pings to the peer
	pingInterval time.Duration

	// the maximum size in bytes for a message read from the peer
	readLimit int64
}
