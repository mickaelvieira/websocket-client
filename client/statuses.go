package client

// Status represents the current connection status of the websocket client
// it can be either Connected or Disconnected.
type Status uint64

const (
	statusDisconnected Status = iota + 1
	statusConnected
)

// IsConnected returns true if the status is Connected
func (s Status) IsConnected() bool {
	return s == statusConnected
}

// IsDisconnected returns true if the status is Disconnected
func (s Status) IsDisconnected() bool {
	return s == statusDisconnected
}
