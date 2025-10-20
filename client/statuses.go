package client

// outside world statuses
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
