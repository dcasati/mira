package conversation

import "time"

type State string

const (
	StateIdle       State = "IDLE"
	StateActivating State = "ACTIVATING"
	StateActive     State = "ACTIVE"
	StateExpiring   State = "EXPIRING"
)

type Conversation struct {
	State        State
	SessionID    string
	LastActivity time.Time
	Speaker      string
}
