package oplog

import (
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func makeTestOp(id string) domain.Operation {
	return domain.Operation{
		OpID:      id,
		Kind:      domain.OpKindMove,
		Agent:     "agent-1",
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:    domain.OpStatusPending,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: false},
			{Step: 1, Kind: "delete_items", Done: false},
		},
	}
}

func TestJournal_BeginThenPending(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("op1ABC")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if pending[0].OpID != "op1ABC" {
		t.Errorf("OpID: got %q want %q", pending[0].OpID, "op1ABC")
	}
	if pending[0].Status != domain.OpStatusExecuting {
		t.Errorf("Status: got %q, want executing", pending[0].Status)
	}
}

func TestJournal_MarkStep(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("op2DEF")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkStep("op2DEF", 0); err != nil {
		t.Fatal(err)
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	if !pending[0].Plan[0].Done {
		t.Error("step 0 should be marked done")
	}
	if pending[0].Plan[1].Done {
		t.Error("step 1 should not be done yet")
	}
}

func TestJournal_Complete_RemovesFromPending(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("op3GHI")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.Complete("op3GHI"); err != nil {
		t.Fatal(err)
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Errorf("want 0 pending after complete, got %d", len(pending))
	}
}

func TestJournal_InvalidOpIDRejected(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("../escape")
	if err := j.Begin(op); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("Begin with traversal ID: want ErrUsage, got %v", err)
	}
	if err := j.MarkStep("../escape", 0); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("MarkStep with traversal ID: want ErrUsage, got %v", err)
	}
	if err := j.Complete("../escape"); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("Complete with traversal ID: want ErrUsage, got %v", err)
	}
}

func TestJournal_MarkStepOutOfRange(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("op4JKL")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkStep("op4JKL", 99); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage for out-of-range step, got %v", err)
	}
}

func TestJournal_MultiplePending(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"opABC1", "opDEF2", "opGHI3"} {
		if beginErr := j.Begin(makeTestOp(id)); beginErr != nil {
			t.Fatal(beginErr)
		}
	}
	if err := j.Complete("opDEF2"); err != nil {
		t.Fatal(err)
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Errorf("want 2 pending, got %d", len(pending))
	}
}
