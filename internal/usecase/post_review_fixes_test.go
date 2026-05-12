package usecase_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// fixesDeps is a fully-wired test bundle for the post-review hardening
// tests. Every adapter is a fake so tests stay deterministic.
func fixesDeps(t *testing.T) (usecase.Deps, *testfakes.Store, *testfakes.Clock, *testfakes.EventLog) {
	t.Helper()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	evlog := testfakes.NewEventLog()
	d := usecase.Deps{
		Store:     store,
		Clock:     clock,
		IDGen:     testfakes.NewIDGen("LEASE"),
		Locker:    testfakes.NewLocker(),
		EventLog:  evlog,
		OpResults: testfakes.NewOpResults(),
	}
	return d, store, clock, evlog
}

// ---------------------------------------------------------------------
// Finding 1: Done must not let a non-holder bypass the lease, and must
// always clear the claim on a successful done transition.
// ---------------------------------------------------------------------

func TestDone_RejectsBypass_WhenItemIsClaimed_NoLeaseSupplied(t *testing.T) {
	t.Parallel()
	d, _, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	// Move RKN through ready → claimed (via Claim, which stamps Claim).
	ready := domain.StatusReady
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 1, Status: &ready,
	}); err != nil {
		t.Fatal(err)
	}
	c, err := usecase.Claim(context.Background(), d, usecase.ClaimInput{
		ID: "RKN", Agent: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Advance to in_progress so the Done transition is otherwise legal.
	if _, hbErr := usecase.Heartbeat(context.Background(), d, usecase.HeartbeatInput{
		ID: "RKN", LeaseID: c.LeaseID, AdvanceToInProgress: true,
	}); hbErr != nil {
		t.Fatal(hbErr)
	}
	// A different agent attempts Done with no lease — must be rejected.
	_, err = usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: -1, Agent: "mallory",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("unauthenticated Done: want ErrLeaseMismatch, got %v", err)
	}
}

func TestDone_AlwaysClearsClaim_OnSuccessfulTransition(t *testing.T) {
	t.Parallel()
	d, store, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	ready := domain.StatusReady
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 1, Status: &ready,
	}); err != nil {
		t.Fatal(err)
	}
	c, err := usecase.Claim(context.Background(), d, usecase.ClaimInput{
		ID: "RKN", Agent: "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, hbErr := usecase.Heartbeat(context.Background(), d, usecase.HeartbeatInput{
		ID: "RKN", LeaseID: c.LeaseID, AdvanceToInProgress: true,
	}); hbErr != nil {
		t.Fatal(hbErr)
	}
	if _, doneErr := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: -1, LeaseID: c.LeaseID, Agent: "alice",
	}); doneErr != nil {
		t.Fatal(doneErr)
	}
	got, err := store.Get(context.Background(), "RKN")
	if err != nil {
		t.Fatal(err)
	}
	if got.Claim != nil {
		t.Errorf("done with matching lease must clear claim; got %+v", got.Claim)
	}
}

// ---------------------------------------------------------------------
// Finding 2: concurrent Add (and Clone) must not allocate the same
// child ID. The race lives in "list siblings → compute next ID → Put".
// Wrapping that in Locker.Acquire(parent) closes it.
// ---------------------------------------------------------------------

func TestAdd_Concurrent_AllocatesUniqueChildIDs(t *testing.T) {
	t.Parallel()
	d, store, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeEpic, ParentID: "RKN", Title: "e",
	}); err != nil {
		t.Fatal(err)
	}
	const goroutines = 8
	var (
		wg     sync.WaitGroup
		seenMu sync.Mutex
		seen   = map[string]int{}
		fails  atomic.Int32
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := usecase.Add(context.Background(), d, usecase.AddInput{
				Type:     domain.TypeStory,
				ParentID: "RKN-E01",
				Title:    "s",
			})
			if err != nil {
				fails.Add(1)
				return
			}
			seenMu.Lock()
			seen[out.Item.ID]++
			seenMu.Unlock()
		}()
	}
	wg.Wait()
	if fails.Load() != 0 {
		t.Errorf("Add returned errors under contention: %d", fails.Load())
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("child %q allocated %d times; expected 1", id, n)
		}
	}
	stories, _ := store.List(context.Background(), domain.ListFilter{ParentID: "RKN-E01"})
	if len(stories) != goroutines {
		t.Errorf("store has %d stories under RKN-E01; want %d (silent overwrite?)", len(stories), goroutines)
	}
}

func TestClone_Concurrent_AllocatesUniqueChildIDs(t *testing.T) {
	t.Parallel()
	d, store, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeEpic, ParentID: "RKN", Title: "e",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeStory, ParentID: "RKN-E01", Title: "seed",
	}); err != nil {
		t.Fatal(err)
	}
	const goroutines = 8
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := usecase.Clone(context.Background(), d, usecase.CloneInput{
				SourceID: "RKN-E01-S01", ParentID: "RKN-E01", Agent: "alice",
			}); err != nil {
				t.Errorf("Clone: %v", err)
			}
		}()
	}
	wg.Wait()
	stories, _ := store.List(context.Background(), domain.ListFilter{ParentID: "RKN-E01"})
	// 1 seed + N clones.
	if len(stories) != 1+goroutines {
		t.Errorf("store has %d stories; want %d (clone silent-overwrite?)", len(stories), 1+goroutines)
	}
}

// ---------------------------------------------------------------------
// Finding 3: every mutating usecase must emit an EventLog event so the
// audit trail is reconstructable from the log alone.
// ---------------------------------------------------------------------

func eventTypes(events []domain.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Type)
	}
	return out
}

func TestAdd_AppendsItemAddedEvent(t *testing.T) {
	t.Parallel()
	d, _, _, evlog := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := evlog.Read()
	types := eventTypes(got)
	if !containsString(types, "item_added") {
		t.Errorf("event types = %v, want item_added", types)
	}
}

func TestUpdate_AppendsItemUpdatedEvent(t *testing.T) {
	t.Parallel()
	d, _, _, evlog := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	ready := domain.StatusReady
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 1, Status: &ready, Note: "shipping soon",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := evlog.Read()
	if !containsString(eventTypes(got), "item_updated") {
		t.Errorf("event types = %v, want item_updated", eventTypes(got))
	}
}

func TestDone_AppendsCompletedEvent(t *testing.T) {
	t.Parallel()
	d, _, _, evlog := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	v := advanceToInProgress(t, d, "RKN")
	if _, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: v, Agent: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := evlog.Read()
	if !containsString(eventTypes(got), "completed") {
		t.Errorf("event types = %v, want completed", eventTypes(got))
	}
}

func TestDelete_AppendsAbandonedEvent(t *testing.T) {
	t.Parallel()
	d, _, _, evlog := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "RKN", ExpectVersion: 1, Agent: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := evlog.Read()
	if !containsString(eventTypes(got), "abandoned") {
		t.Errorf("event types = %v, want abandoned", eventTypes(got))
	}
}

func TestClone_AppendsClonedEvent(t *testing.T) {
	t.Parallel()
	d, _, _, evlog := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeEpic, ParentID: "RKN", Title: "e",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeStory, ParentID: "RKN-E01", Title: "s",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Clone(context.Background(), d, usecase.CloneInput{
		SourceID: "RKN-E01-S01", ParentID: "RKN-E01", Agent: "alice",
	}); err != nil {
		t.Fatal(err)
	}
	got, _ := evlog.Read()
	if !containsString(eventTypes(got), "cloned") {
		t.Errorf("event types = %v, want cloned", eventTypes(got))
	}
}

func TestMutators_TolerateEventLogFailures(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC))
	evlog := &failingEventLog{}
	d := usecase.Deps{
		Store:    store,
		Clock:    clock,
		IDGen:    testfakes.NewIDGen("L"),
		Locker:   testfakes.NewLocker(),
		EventLog: evlog,
	}
	// Add must not propagate the EventLog failure to the caller — the
	// item write succeeded; the audit-log gap is logged separately
	// (stderr) but does not fail the operation.
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Errorf("Add bubbled EventLog failure: %v", err)
	}
	if _, err := store.Get(context.Background(), "RKN"); err != nil {
		t.Errorf("Add did not persist item despite EventLog failure: %v", err)
	}
}

// failingEventLog returns an error from Append; used to assert that
// usecases tolerate audit-log failures without rolling back the
// item write.
type failingEventLog struct{}

func (f *failingEventLog) Append(domain.Event) error {
	return errors.New("simulated eventlog failure")
}
func (f *failingEventLog) Read() ([]domain.Event, error) { return nil, nil }
func (f *failingEventLog) Scan(func(domain.Event) error) error {
	return nil
}

// ---------------------------------------------------------------------
// Finding 4: same operation_id under contention must replay rather
// than the second caller getting ErrAlreadyClaimed. The fix is to
// re-check OpResults *after* Locker.Acquire.
// ---------------------------------------------------------------------

// TestClaim_OperationIDReplay_UnderContention exercises §S11's
// idempotency contract: when N goroutines fire the same operation_id
// concurrently, all of them must observe the same lease (the
// original) — the post-lock re-check is what closes the race.
//
// Iterations: 25 fresh workspaces × 8 racing goroutines. Without the
// post-lock re-check, ~90% of iterations end with a single winner
// and N-1 ErrAlreadyClaimed losers; so even a single `go test` run
// surfaces the bug.
func TestClaim_OperationIDReplay_UnderContention(t *testing.T) {
	t.Parallel()
	const iterations = 25
	const goroutines = 8

	for iter := 0; iter < iterations; iter++ {
		store := testfakes.NewStore()
		mustClaimSeed(t, store, "RKN-E01-S01")
		d := usecase.Deps{
			Store:     store,
			Clock:     testfakes.NewClock(time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)),
			IDGen:     testfakes.NewIDGen("L"),
			Locker:    testfakes.NewLocker(),
			EventLog:  testfakes.NewEventLog(),
			OpResults: testfakes.NewOpResults(),
		}
		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			leases = map[string]int{}
			errs   = map[string]int{}
		)
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := usecase.Claim(context.Background(), d, usecase.ClaimInput{
					ID:          "RKN-E01-S01",
					Agent:       "alice",
					OperationID: "shared-op-1",
					LockTimeout: time.Second,
				})
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs[err.Error()]++
					return
				}
				leases[out.LeaseID]++
			}()
		}
		wg.Wait()
		if len(errs) != 0 {
			t.Fatalf("iter=%d: same-opID concurrent claims produced errors: %+v", iter, errs)
		}
		if len(leases) != 1 {
			t.Fatalf("iter=%d: same-opID concurrent claims yielded %d distinct leases; want 1: %+v",
				iter, len(leases), leases)
		}
		for _, count := range leases {
			if count != goroutines {
				t.Fatalf("iter=%d: expected all %d callers to see the same lease replay; got %d",
					iter, goroutines, count)
			}
		}
	}
}

// ---------------------------------------------------------------------
// Finding 5: Update and Delete must not blind-overwrite a concurrent
// Claim. The vector is `--expect-version=-1` (CLI default) plus a
// stale read: a planner reads the item before claim, then writes
// after claim, erasing Claim. The fix is to acquire the per-item
// Locker around the read-modify-write window.
// ---------------------------------------------------------------------

// TestUpdate_AcquiresPerItemLock proves Finding 5's fix: Update now
// participates in the per-item Locker contract. With the fix, an
// Update against an item whose lock is already held blocks (and
// times out under our test timeout). Without the fix, Update would
// happily race past the existing lock and blind-overwrite a
// concurrent Claim's record.
//
// We assert via ErrLockTimeout because that's the cleanest signal
// the usecase consulted the Locker; the underlying race-erasure
// guarantee is then a property of the Locker contract itself
// (already exercised by Phase 3's claim_concurrent_test.go).
func TestUpdate_AcquiresPerItemLock(t *testing.T) {
	t.Parallel()
	d, _, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}

	// Hold the lock to simulate a claim in flight.
	handle, err := d.Locker.Acquire("RKN", 0)
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}
	defer func() { _ = handle.Release() }()

	title := "stale-overwrite"
	_, err = usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: -1, Title: &title,
	})
	if !errors.Is(err, domain.ErrLockTimeout) {
		t.Errorf("Update did not respect the per-item lock; want ErrLockTimeout, got %v", err)
	}
}

// TestDelete_AcquiresPerItemLock is the Delete-side mirror of
// TestUpdate_AcquiresPerItemLock — verifies the same Finding 5 fix
// applies to Delete's read-modify-write window.
func TestDelete_AcquiresPerItemLock(t *testing.T) {
	t.Parallel()
	d, _, _, _ := fixesDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}

	handle, err := d.Locker.Acquire("RKN", 0)
	if err != nil {
		t.Fatalf("pre-acquire lock: %v", err)
	}
	defer func() { _ = handle.Release() }()

	_, err = usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "RKN", ExpectVersion: -1, Agent: "planner",
	})
	if !errors.Is(err, domain.ErrLockTimeout) {
		t.Errorf("Delete did not respect the per-item lock; want ErrLockTimeout, got %v", err)
	}
}

func mustClaimSeed(t *testing.T, store *testfakes.Store, id string) {
	t.Helper()
	if _, err := store.Put(context.Background(), domain.Item{
		ID: id, Type: domain.TypeStory, Status: domain.StatusReady, Version: 1,
	}, -1); err != nil {
		t.Fatal(err)
	}
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
