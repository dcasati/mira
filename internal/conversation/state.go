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
	// Busy is true while a request/response exchange (including any tool
	// call, e.g. Foundry/Fabric IQ retrieval) is in flight. The idle-timeout
	// watchdog must not expire the conversation while Busy is true, since a
	// slow tool call is not conversation inactivity.
	Busy bool
}
