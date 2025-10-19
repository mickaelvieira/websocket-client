package ws_client

// outside world statuses
type Status uint64

const (
	StatusDisconnected Status = iota
	StatusConnected
)

func (s Status) IsConnected() bool {
	return s == StatusConnected
}

func (s Status) IsDisconnected() bool {
	return s == StatusDisconnected
}
