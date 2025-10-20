package websocket

// https://developer.mozilla.org/en-US/docs/Web/API/WebSockets_API/Writing_WebSocket_servers#pings_and_pongs_the_heartbeat_of_websockets

import "errors"

var (
	ErrNotConnected = errors.New("websocket client not connected")
)

// OutboundMessage represents a message
// to be sent over the websocket connection
type OutboundMessage struct {
	DataType int
	Data     []byte
}

type Socket interface {
	// Unique identifier of the websocket peer
	Id() string

	// Channel to receive text messages from the websocket server
	ReadTextMessages() <-chan string

	// Send a text message to the websocket server
	SendTextMessage(string)

	// Channel to receive binary messages from the websocket server
	ReadBinaryMessages() <-chan []byte

	// Send a binary message to the websocket server
	SendBinaryMessage([]byte)

	// Close the websocket connection
	Close() error
}
