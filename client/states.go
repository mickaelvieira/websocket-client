package client

// internal client states matching the state machine
type state uint64

const (
	disconnected state = iota + 1
	connecting
	connected
	closing
	reconnecting
)

func (s state) String() string {
	switch s {
	case disconnected:
		return "Disconnected"
	case connecting:
		return "Connecting"
	case connected:
		return "Connected"
	case closing:
		return "Closing"
	case reconnecting:
		return "Reconnecting"
	default:
		return "Unknown"
	}
}
