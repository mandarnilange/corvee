package oplog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// TestJournal_Crash_TruncatedFile verifies that a partially-written (truncated)
// operation file does not crash Pending() — the corrupt file is skipped.
func TestJournal_Crash_TruncatedFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Write a valid op, then corrupt its file to simulate a torn write.
	op := makeTestOp("crashTEST")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	// Truncate the file to simulate mid-write crash.
	p := filepath.Join(dir, "op-crashTEST.json")
	if err := os.WriteFile(p, []byte(`{"op_id":"crashTEST"`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Pending() should not panic or error — it should skip the corrupt file.
	pending, err := j.Pending()
	if err != nil {
		t.Fatalf("Pending() should not error on corrupt file: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("corrupt file should be skipped, got %d pending", len(pending))
	}
}

// TestJournal_Crash_RaceOnWrite verifies that concurrent Begin calls do
// not corrupt each other's files. Run under -race -count=10.
func TestJournal_Crash_RaceOnWrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		i := i
		go func() {
			opID := makeOpID(i)
			op := domain.Operation{
				OpID:      opID,
				Kind:      domain.OpKindMove,
				Agent:     "race-test",
				StartedAt: time.Now(),
				Status:    domain.OpStatusPending,
				Plan: []domain.OpStep{
					{Step: 0, Kind: "create_items"},
				},
			}
			done <- j.Begin(op)
		}()
	}
	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Errorf("concurrent Begin: %v", err)
		}
	}
	pending, err := j.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 10 {
		t.Errorf("expected 10 pending ops, got %d", len(pending))
	}
}

// TestJournal_Crash_MarkStepAtomicity verifies that MarkStep's tmp+rename
// atomicity means a Pending() call concurrent with MarkStep sees either
// the old or new state, never a torn file.
func TestJournal_Crash_MarkStepAtomicity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	op := makeTestOp("atomicMARK")
	op.Plan = []domain.OpStep{
		{Step: 0, Kind: "create_items", Done: false},
		{Step: 1, Kind: "delete_items", Done: false},
	}
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}

	// Concurrent MarkStep and Pending should not produce a parse error.
	errs := make(chan error, 50)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)

	// Reader goroutine: continuously calls Pending.
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if _, readErr := j.Pending(); readErr != nil {
					select {
					case errs <- readErr:
					default:
					}
				}
			}
		}
	}()

	// Writer: marks steps done while reader is running.
	for _, step := range []int{0, 1} {
		if err := j.MarkStep("atomicMARK", step); err != nil {
			t.Errorf("MarkStep %d: %v", step, err)
		}
	}
	close(stop)
	wg.Wait()

	close(errs)
	for err := range errs {
		t.Errorf("concurrent read/write error: %v", err)
	}
}

// TestJournal_Crash_CompleteIsIdempotentToReads verifies that after
// Complete, subsequent Pending() calls return an empty list (the
// operation is effectively closed).
func TestJournal_Crash_CompleteIsIdempotentToReads(t *testing.T) {
	t.Parallel()
	j, err := NewJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	op := makeTestOp("completeTEST")
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}
	if err := j.Complete("completeTEST"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		pending, err := j.Pending()
		if err != nil {
			t.Fatalf("Pending call %d: %v", i, err)
		}
		if len(pending) != 0 {
			t.Errorf("call %d: want 0 pending after Complete, got %d", i, len(pending))
		}
	}
}

// makeOpID creates a test op ID from an integer, ensuring it passes the
// validOpID check.
func makeOpID(n int) string {
	return "race" + string(rune('A'+n))
}

// TestJournal_Crash_MarshalledDataIntegrity verifies that the JSON written
// to disk round-trips cleanly, including the full Plan slice.
func TestJournal_Crash_MarshalledDataIntegrity(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	j, err := NewJournal(dir)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	op := domain.Operation{
		OpID:      "integrityOP",
		Kind:      domain.OpKindMove,
		Agent:     "integrity-agent",
		StartedAt: now,
		Status:    domain.OpStatusPending,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Args: map[string]any{"items_json": `[{"id":"X"}]`}, Done: false},
			{Step: 1, Kind: "delete_items", Args: map[string]any{"old_ids_json": `["Y"]`}, Done: false},
		},
	}
	if err := j.Begin(op); err != nil {
		t.Fatal(err)
	}

	// Read the file directly and verify JSON is parseable.
	data, err := os.ReadFile(filepath.Join(dir, "op-integrityOP.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed domain.Operation
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("written JSON is not parseable: %v\n%s", err, data)
	}
	if parsed.OpID != "integrityOP" {
		t.Errorf("OpID: got %q", parsed.OpID)
	}
	if len(parsed.Plan) != 2 {
		t.Errorf("Plan len: got %d want 2", len(parsed.Plan))
	}
}
