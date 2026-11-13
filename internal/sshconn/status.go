package sshconn

// Status is a profile's connection state.
type Status string

const (
	StatusDisconnected Status = "disconnected"
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusError        Status = "error"
)

// StatusEvent is a change of a profile's connection state.
type StatusEvent struct {
	ProfileID int64
	Status    Status
	Err       error // for StatusError, and a lost connection's cause
}
