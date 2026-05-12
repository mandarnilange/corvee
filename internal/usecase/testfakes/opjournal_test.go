package testfakes

import (
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func makeTestOp(id string) domain.Operation {
	return domain.Operation{
		OpID:      id,
		Kind:      domain.OpKindMove,
		Agent:     "test",
		StartedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Status:    domain.OpStatusPending,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: false},
			{Step: 1, Kind: "delete_items", Done: false},
		},
	}
}

func TestOpJournal_BeginAndPending(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	op := makeTestOp("op-001")
	if err := j.Begin(op); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 || pending[0].OpID != "op-001" {
		t.Errorf("Pending: got %v want 1 op", pending)
	}
}

func TestOpJournal_MarkStep(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	op := makeTestOp("op-002")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkStep("op-002", 0); err != nil {
		t.Fatalf("MarkStep: %v", err)
	}
	all := j.All()
	if len(all) != 1 || !all[0].Plan[0].Done {
		t.Errorf("step 0 not marked done: %+v", all)
	}
}

func TestOpJournal_MarkStep_NotFound(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	if err := j.MarkStep("missing", 0); err == nil {
		t.Error("expected error for missing op")
	}
}

func TestOpJournal_MarkStep_OutOfRange(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	op := makeTestOp("op-003")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.MarkStep("op-003", 99); err == nil {
		t.Error("expected error for out-of-range step")
	}
}

func TestOpJournal_Complete(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	op := makeTestOp("op-004")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.Complete("op-004"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	pending, _ := j.Pending()
	if len(pending) != 0 {
		t.Errorf("want 0 pending after Complete, got %d", len(pending))
	}
}

func TestOpJournal_Complete_NotFound(t *testing.T) {
	t.Parallel()
	j := NewOpJournal()
	if err := j.Complete("missing"); err == nil {
		t.Error("expected error for missing op")
	}
}
