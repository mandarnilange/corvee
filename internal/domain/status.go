package domain

import "fmt"

// validTransitions is the explicit Status→{allowed-next-Status} adjacency
// table from spec §15.2. The any→abandoned edge is handled before the
// table lookup so it doesn't have to repeat across every row.
var validTransitions = map[Status]map[Status]struct{}{
	StatusBacklog:    {StatusReady: {}},
	StatusReady:      {StatusClaimed: {}},
	StatusClaimed:    {StatusInProgress: {}, StatusReady: {}},
	StatusInProgress: {StatusReview: {}, StatusBlocked: {}, StatusDone: {}},
	StatusReview:     {StatusDone: {}, StatusInProgress: {}},
	StatusBlocked:    {StatusReady: {}, StatusInProgress: {}},
}

// ValidTransition reports whether moving an item from `from` to `to`
// is legal per the §15.2 status graph. Unknown statuses on either side
// are rejected. The any→abandoned edge is universally legal.
func ValidTransition(from, to Status) bool {
	if !from.IsValid() || !to.IsValid() {
		return false
	}
	if to == StatusAbandoned {
		return true
	}
	_, ok := validTransitions[from][to]
	return ok
}

// Transition returns nil when (from→to) is a legal status edge per
// ValidTransition; otherwise it wraps ErrInvalidStatus with both
// endpoints in the diagnostic so callers get a useful error without
// re-deriving the message.
func Transition(from, to Status) error {
	if ValidTransition(from, to) {
		return nil
	}
	return fmt.Errorf("status transition %q->%q: %w", from, to, ErrInvalidStatus)
}
