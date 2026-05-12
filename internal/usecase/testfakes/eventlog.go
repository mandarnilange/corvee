package testfakes

import (
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// EventLog is an in-memory fake of domain.EventLog. It captures every
// appended event so tests can assert on the audit trail.
type EventLog struct {
	mu     sync.Mutex
	events []domain.Event
}

// NewEventLog returns an empty EventLog fake.
func NewEventLog() *EventLog { return &EventLog{} }

// Append records ev verbatim. Returns ErrUsage when EventID, Timestamp,
// or Type is missing — mirrors the production adapter.
func (l *EventLog) Append(ev domain.Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, ev)
	return nil
}

// Read returns a copy of the recorded events in append order.
func (l *EventLog) Read() ([]domain.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]domain.Event, len(l.events))
	copy(out, l.events)
	return out, nil
}

// Scan streams the recorded events in append order through fn. The
// fake snapshots the slice under the mutex so concurrent appends
// during iteration do not change the sequence the caller sees.
// Returning a non-nil error from fn aborts immediately.
func (l *EventLog) Scan(fn func(domain.Event) error) error {
	l.mu.Lock()
	snapshot := make([]domain.Event, len(l.events))
	copy(snapshot, l.events)
	l.mu.Unlock()
	for _, ev := range snapshot {
		if err := fn(ev); err != nil {
			return err
		}
	}
	return nil
}
