package internal

// OutboundMessage represents a message
// to be sent over the websocket connection
type OutboundMessage struct {
	DataType int
	Data     []byte
}
