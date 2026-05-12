package domain

import (
	"errors"
	"testing"
)

func TestValidTransition_LegalTable(t *testing.T) {
	t.Parallel()

	legal := []struct {
		from, to Status
	}{
		// Explicit edges from the §15.2 transition graph.
		{StatusBacklog, StatusReady},
		{StatusReady, StatusClaimed},
		{StatusClaimed, StatusInProgress},
		{StatusClaimed, StatusReady},
		{StatusInProgress, StatusReview},
		{StatusInProgress, StatusBlocked},
		{StatusInProgress, StatusDone},
		{StatusReview, StatusDone},
		{StatusReview, StatusInProgress},
		{StatusBlocked, StatusReady},
		{StatusBlocked, StatusInProgress},
		// any → abandoned (the universal terminal edge).
		{StatusBacklog, StatusAbandoned},
		{StatusReady, StatusAbandoned},
		{StatusClaimed, StatusAbandoned},
		{StatusInProgress, StatusAbandoned},
		{StatusReview, StatusAbandoned},
		{StatusBlocked, StatusAbandoned},
		{StatusDone, StatusAbandoned},
	}
	for _, c := range legal {
		c := c
		name := string(c.from) + "->" + string(c.to)
		t.Run("legal/"+name, func(t *testing.T) {
			t.Parallel()
			if !ValidTransition(c.from, c.to) {
				t.Fatalf("ValidTransition(%q,%q) = false, want true", c.from, c.to)
			}
		})
	}
}

func TestValidTransition_IllegalSamples(t *testing.T) {
	t.Parallel()

	illegal := []struct {
		from, to Status
	}{
		{StatusBacklog, StatusInProgress}, // skipping ready/claimed
		{StatusBacklog, StatusClaimed},    // skipping ready
		{StatusReady, StatusDone},         // skipping claimed/in_progress
		{StatusReady, StatusInProgress},   // skipping claimed
		{StatusInProgress, StatusReady},   // executors don't hand back
		{StatusDone, StatusReady},         // can't un-finish
		{StatusDone, StatusInProgress},
		{StatusAbandoned, StatusReady}, // can't un-abandon
		{StatusBacklog, StatusBacklog}, // self-loop not legal
		{StatusBacklog, "wat"},         // unknown to-status
		{"hmm", StatusReady},           // unknown from-status
	}
	for _, c := range illegal {
		c := c
		name := string(c.from) + "->" + string(c.to)
		t.Run("illegal/"+name, func(t *testing.T) {
			t.Parallel()
			if ValidTransition(c.from, c.to) {
				t.Fatalf("ValidTransition(%q,%q) = true, want false", c.from, c.to)
			}
		})
	}
}

func TestTransition_ReturnsErrInvalidStatusOnIllegal(t *testing.T) {
	t.Parallel()

	if err := Transition(StatusBacklog, StatusReady); err != nil {
		t.Errorf("legal Transition returned err: %v", err)
	}

	err := Transition(StatusBacklog, StatusInProgress)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("Transition(backlog, in_progress) = %v, want errors.Is(ErrInvalidStatus)", err)
	}

	// The error message must name both endpoints so callers can
	// surface a useful diagnostic without re-deriving them.
	msg := err.Error()
	if !contains(msg, "backlog") || !contains(msg, "in_progress") {
		t.Errorf("error message %q lacks both endpoints", msg)
	}
}
