package client

// Status represents the current connection status of the websocket client
// it can be either Connected or Disconnected.
type Status uint64

const (
	statusDisconnected Status = iota + 1
	statusConnected
)

func (s Status) IsConnected() bool {
	return s == statusConnected
}

func (s Status) IsDisconnected() bool {
	return s == statusDisconnected
}
