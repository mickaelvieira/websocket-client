// Package websocket provides two implementations of websocket connections.
// The client is able to connect to a websocket server and maintains the connection.
// The server represents a connected websocket client to the server.
package websocket

// Socket is an interface representing a websocket peer connection.
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
