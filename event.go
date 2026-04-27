package sugaar

import (
	"encoding/json"
	"time"
)

// Event is a single agentic event flowing through the Hub.
//
// Topic groups subscribers (e.g. "agent.thoughts", "session.42").
// Type is a free-form discriminator for clients ("token", "tool_call", "done").
// Data is any JSON-serialisable payload.
type Event struct {
	ID    string    `json:"id,omitempty"`
	Topic string    `json:"topic"`
	Type  string    `json:"type"`
	Time  time.Time `json:"time"`
	Data  any       `json:"data,omitempty"`
}

// MarshalJSON ensures Time is always populated.
func (e Event) MarshalJSON() ([]byte, error) {
	type alias Event
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	return json.Marshal(alias(e))
}
