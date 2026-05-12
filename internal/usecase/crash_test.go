package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// crashingJournal wraps an OpJournal and panics after marking a specific
// step done, simulating a process crash mid-operation. Used in tests
// guarded by recover() to validate the WAL guarantees.
type crashingJournal struct {
	inner   domain.OpJournal
	crashAt int // panic after marking this step; -1 = never crash
}

func (j *crashingJournal) Begin(op domain.Operation) error { return j.inner.Begin(op) }
func (j *crashingJournal) Complete(opID string) error      { return j.inner.Complete(opID) }
func (j *crashingJournal) Pending() ([]domain.Operation, error) {
	return j.inner.Pending()
}
func (j *crashingJournal) MarkStep(opID string, stepNum int) error {
	if err := j.inner.MarkStep(opID, stepNum); err != nil {
		return err
	}
	if j.crashAt >= 0 && stepNum == j.crashAt {
		panic("crash-injection: simulated crash after step")
	}
	return nil
}

// buildCrashStore builds a store with SRC+DST projects and an epic+story under SRC.
func buildCrashStore(t *testing.T) *testfakes.Store {
	t.Helper()
	store := testfakes.NewStore()
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{SchemaVersion: 1, ID: "SRC", Type: domain.TypeProject, Title: "S", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "DST", Type: domain.TypeProject, Title: "D", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01-S01", Type: domain.TypeStory, ProjectID: "SRC", ParentID: "SRC-E01", Title: "S1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

// recoverAfterCrash runs Recover and asserts no failure.
func recoverAfterCrash(t *testing.T, store *testfakes.Store, journal domain.OpJournal) RecoverOutput {
	t.Helper()
	d := Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("recover"),
		OpJournal: journal,
	}
	out, err := Recover(context.Background(), d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover after crash: %v", err)
	}
	return out
}

// assertConsistentAfterMove checks that exactly one version of the
// moved item exists (either old or new, never both, never neither).
func assertConsistentAfterMove(t *testing.T, store *testfakes.Store) {
	t.Helper()
	ctx := context.Background()
	srcExists, _ := store.Exists(ctx, "SRC-E01")
	dstExists, _ := store.Exists(ctx, "DST-E01")

	if srcExists && dstExists {
		t.Fatal("crash consistency: both SRC-E01 and DST-E01 exist simultaneously")
	}
	if !srcExists && !dstExists {
		t.Fatal("crash consistency: neither SRC-E01 nor DST-E01 exists")
	}
	if dstExists {
		if _, err := store.Get(ctx, "DST-E01-S01"); err != nil {
			t.Error("crash consistency: DST-E01 exists but DST-E01-S01 missing")
		}
	}
}

// runMoveWithCrash runs Move with a crashingJournal, swallowing the panic.
func runMoveWithCrash(t *testing.T, store *testfakes.Store, cj domain.OpJournal) {
	t.Helper()
	ctx := context.Background()
	d := Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: cj,
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = Move(ctx, d, MoveInput{
			SourceID:       "SRC-E01",
			TargetParentID: "DST",
			Agent:          "a",
		})
	}()
}

// TestCrashAfterStep0_RecoveryYieldsConsistentState injects a crash
// after step 0 (new items created) and verifies Recover achieves a
// consistent state.
func TestCrashAfterStep0_RecoveryYieldsConsistentState(t *testing.T) {
	t.Parallel()
	store := buildCrashStore(t)
	inner := testfakes.NewOpJournal()
	cj := &crashingJournal{inner: inner, crashAt: 0}

	runMoveWithCrash(t, store, cj)

	pending, _ := inner.Pending()
	if len(pending) == 0 {
		t.Fatal("expected pending op after crash, got none")
	}

	out := recoverAfterCrash(t, store, inner)
	if len(out.Failed) > 0 {
		t.Errorf("recovery failures: %v", out.Failed)
	}
	assertConsistentAfterMove(t, store)
}

// TestCrashAfterStep2_RecoveryYieldsConsistentState injects a crash
// after step 2 (old items deleted) but before the audit event step.
func TestCrashAfterStep2_RecoveryYieldsConsistentState(t *testing.T) {
	t.Parallel()
	store := buildCrashStore(t)
	inner := testfakes.NewOpJournal()
	cj := &crashingJournal{inner: inner, crashAt: 2}

	runMoveWithCrash(t, store, cj)

	out := recoverAfterCrash(t, store, inner)
	if len(out.Failed) > 0 {
		t.Errorf("recovery failures: %v", out.Failed)
	}
	assertConsistentAfterMove(t, store)
}

// TestCrashAfterStep1_RecoveryYieldsConsistentState injects a crash
// after step 1 (refs rewritten) before old items are deleted.
func TestCrashAfterStep1_RecoveryYieldsConsistentState(t *testing.T) {
	t.Parallel()
	store := buildCrashStore(t)
	inner := testfakes.NewOpJournal()
	cj := &crashingJournal{inner: inner, crashAt: 1}

	runMoveWithCrash(t, store, cj)

	out := recoverAfterCrash(t, store, inner)
	if len(out.Failed) > 0 {
		t.Errorf("recovery failures: %v", out.Failed)
	}
	assertConsistentAfterMove(t, store)
}

// TestCrashInjection_AllSteps exercises every crash-injection point
// under -race. Run via make test-race (-race -count=10).
func TestCrashInjection_AllSteps(t *testing.T) {
	for crashAt := 0; crashAt <= 3; crashAt++ {
		crashAt := crashAt
		t.Run("", func(t *testing.T) {
			t.Parallel()
			store := buildCrashStore(t)
			inner := testfakes.NewOpJournal()
			cj := &crashingJournal{inner: inner, crashAt: crashAt}

			runMoveWithCrash(t, store, cj)
			recoverAfterCrash(t, store, inner)
			assertConsistentAfterMove(t, store)
		})
	}
}
