package server

import (
	"log/slog"
	"time"
)

// OptionModifier defines a function type to modify server options
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

// WithLogger allows passing a custom logger for the websocket client
// @see https://pkg.go.dev/log/slog
func WithLogger(l *slog.Logger) OptionModifier {
	return func(o *options) {
		o.logger = l
	}
}

// WithOnCloseCallback sets a callback function to be called when the socket is closed
func WithOnCloseCallback(cb func()) OptionModifier {
	return func(o *options) {
		o.onClose = cb
	}
}

var defaultOptions = options{
	readWait:      1 * time.Second,
	writeWait:     1 * time.Second,
	pingInterval:  54 * time.Second,
	pongWait:      60 * time.Second,
	retryInterval: 5 * time.Second,
	logger:        slog.New(slog.DiscardHandler),
}

type options struct {
	// logger for logging client events
	logger *slog.Logger

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

	// pongWait is the time allowed to read the next pong message from the peer
	pongWait time.Duration

	// the maximum size in bytes for a message read from the peer
	readLimit int64

	// optional onClose callback when the browser socket is closed
	onClose func()
}
