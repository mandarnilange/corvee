package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func mkWatchEvent(id, typ, itemID string, ts time.Time) domain.Event {
	return domain.Event{
		EventID: id, Timestamp: ts, Type: typ, ItemID: itemID, Actor: "alice",
	}
}

// collectWatch drains ch until it closes OR no event arrives for
// idleQuiet. Tests using cancel-based termination rely on the idle
// drain because ctx cancel races with pending channel sends.
func collectWatch(t *testing.T, ch <-chan domain.Event, deadline time.Duration) []domain.Event {
	t.Helper()
	const idleQuiet = 100 * time.Millisecond
	var out []domain.Event
	idle := time.NewTimer(idleQuiet)
	defer idle.Stop()
	hard := time.NewTimer(deadline)
	defer hard.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(idleQuiet)
		case <-idle.C:
			return out
		case <-hard.C:
			t.Fatalf("hard deadline exceeded; got %d events so far", len(out))
		}
	}
}

func TestWatch_RequiresEventLogAndTailer(t *testing.T) {
	t.Parallel()
	d := usecase.Deps{}
	_, err := usecase.Watch(context.Background(), d, usecase.WatchInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v want ErrUsage", err)
	}
}

func TestWatch_LiveStreamWithoutSinceSkipsReplay(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	// Pre-existing events should NOT be replayed when Since is zero.
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	_ = log.Append(mkWatchEvent("01J0OLD", "item_added", "RKN-E01", t0))
	tail.Send(mkWatchEvent("01J0NEW", "claimed", "RKN-E01", t0.Add(time.Minute)))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{Limit: 1})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	if len(got) != 1 || got[0].EventID != "01J0NEW" {
		t.Fatalf("got %v; want only the live event 01J0NEW", eventIDs(got))
	}
}

func TestWatch_ReplaysFromSinceThenStreamsLive(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	_ = log.Append(mkWatchEvent("01J0OLD0", "item_added", "RKN-E01", t0.Add(-time.Hour)))
	_ = log.Append(mkWatchEvent("01J0REP1", "item_added", "RKN-E01", t0.Add(time.Minute)))
	_ = log.Append(mkWatchEvent("01J0REP2", "claimed", "RKN-E01", t0.Add(2*time.Minute)))
	// Live event pre-buffered on the tailer so order is deterministic.
	tail.Send(mkWatchEvent("01J0LIVE", "released", "RKN-E01", t0.Add(3*time.Minute)))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Since: t0, HasSince: true, Limit: 3,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	got := collectWatch(t, ch, 2*time.Second)
	wantOrder := []string{"01J0REP1", "01J0REP2", "01J0LIVE"}
	if len(got) != len(wantOrder) {
		t.Fatalf("got %d events: %v, want %v", len(got), eventIDs(got), wantOrder)
	}
	for i, want := range wantOrder {
		if got[i].EventID != want {
			t.Errorf("event[%d]=%s want %s", i, got[i].EventID, want)
		}
	}
}

func TestWatch_DedupesReplayAgainstLive(t *testing.T) {
	t.Parallel()
	// The tailer (fsnotify-backed) re-emits the current shard from the
	// beginning; Watch must drop anything already delivered via replay.
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	_ = log.Append(mkWatchEvent("01J0SHARED", "item_added", "RKN-E01", t0.Add(time.Minute)))
	// Tailer re-emits the same event (from its file replay) plus a new one.
	tail.Send(mkWatchEvent("01J0SHARED", "item_added", "RKN-E01", t0.Add(time.Minute)))
	tail.Send(mkWatchEvent("01J0NEW", "claimed", "RKN-E01", t0.Add(2*time.Minute)))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Since: t0, HasSince: true, Limit: 2,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	if len(got) != 2 {
		t.Fatalf("got %d events: %v; want 2 (no duplicate)", len(got), eventIDs(got))
	}
	if got[0].EventID != "01J0SHARED" || got[1].EventID != "01J0NEW" {
		t.Errorf("ids=%v", eventIDs(got))
	}
}

func TestWatch_AppliesFilter(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	tail.Send(mkWatchEvent("01J0A", "item_added", "RKN-E01", t0))
	tail.Send(mkWatchEvent("01J0B", "claimed", "RKN-E01", t0))
	tail.Send(mkWatchEvent("01J0C", "released", "RKN-E01", t0))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Filter: domain.EventFilter{Types: []string{"claimed"}},
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	if len(got) != 1 || got[0].EventID != "01J0B" {
		t.Fatalf("got %v want only 01J0B", eventIDs(got))
	}
}

func TestWatch_LimitTerminatesStream(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	for i, id := range []string{"01J0A", "01J0B", "01J0C"} {
		tail.Send(mkWatchEvent(id, "item_added", "RKN-E01", t0.Add(time.Duration(i)*time.Second)))
	}

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{Limit: 2})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	if len(got) != 2 {
		t.Fatalf("got %d; want exactly 2 (limit)", len(got))
	}
}

func TestWatch_ExitOnEventTypeTerminates(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	tail.Send(mkWatchEvent("01J0A", "item_added", "RKN-E01", t0))
	tail.Send(mkWatchEvent("01J0B", "claimed", "RKN-E01", t0.Add(time.Second)))
	tail.Send(mkWatchEvent("01J0DONE", "completed", "RKN-E01", t0.Add(2*time.Second)))
	tail.Send(mkWatchEvent("01J0EXTRA", "item_added", "RKN-E02", t0.Add(3*time.Second)))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		ExitOn: []string{"completed"},
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	// The exit-on event itself IS delivered; nothing after it is.
	if len(got) != 3 {
		t.Fatalf("got %d events: %v; want 3 stopping at completed", len(got), eventIDs(got))
	}
	if got[len(got)-1].EventID != "01J0DONE" {
		t.Errorf("last=%s want 01J0DONE", got[len(got)-1].EventID)
	}
}

func TestWatch_CtxCancelClosesChannel(t *testing.T) {
	t.Parallel()
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatalf("channel did not close on ctx cancel")
		}
	}
}

func TestWatch_LiveTailerReemitsPreSinceEventsAreFiltered(t *testing.T) {
	t.Parallel()
	// Production Tailer reads today's shard from byte 0 on startup and
	// re-emits every event in it — including events older than --since.
	// Watch must drop those: either via the dedup set (so they're caught
	// because Read() saw them) or via a Since check on the live phase.
	// This test exercises BOTH paths by sending pre-Since events from
	// the tailer that EventLog.Read() also returned.
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	since := t0.Add(-time.Hour)

	// Replay snapshot: 3 events, two before --since, one after.
	preSinceA := mkWatchEvent("01J0PRE_A", "item_added", "RKN-E01", t0.Add(-2*time.Hour))
	preSinceB := mkWatchEvent("01J0PRE_B", "claimed", "RKN-E01", t0.Add(-90*time.Minute))
	postSince := mkWatchEvent("01J0POST", "released", "RKN-E01", t0.Add(-30*time.Minute))
	_ = log.Append(preSinceA)
	_ = log.Append(preSinceB)
	_ = log.Append(postSince)

	// Production Tailer would re-emit the same shard from byte 0. Simulate.
	tail.Send(preSinceA)
	tail.Send(preSinceB)
	tail.Send(postSince)

	// Plus one new live event after --since cut.
	tail.Send(mkWatchEvent("01J0LIVE", "completed", "RKN-E01", t0))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Since: since, HasSince: true, Limit: 2,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	gotIDs := eventIDs(got)
	for _, id := range gotIDs {
		if id == "01J0PRE_A" || id == "01J0PRE_B" {
			t.Errorf("pre-Since event %s leaked through live phase: got=%v", id, gotIDs)
		}
	}
	if len(got) != 2 {
		t.Fatalf("got %d events: %v; want 2 (postSince + live)", len(got), gotIDs)
	}
	if gotIDs[0] != "01J0POST" || gotIDs[1] != "01J0LIVE" {
		t.Errorf("got %v want [01J0POST 01J0LIVE]", gotIDs)
	}
}

func TestWatch_LivePhaseRejectsBackdatedPreSinceEvents(t *testing.T) {
	t.Parallel()
	// An event appended after Watch starts but with a backdated timestamp
	// (e.g. clock skew on a peer VM) must also be filtered by --since.
	// EventLog.Read() never saw it (so seen[] doesn't help), so the live
	// emit must apply the Since check directly.
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	since := t0.Add(-time.Hour)

	tail.Send(mkWatchEvent("01J0BACKDATED", "item_added", "RKN-E01", t0.Add(-2*time.Hour)))
	tail.Send(mkWatchEvent("01J0LIVE", "claimed", "RKN-E01", t0))

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Since: since, HasSince: true, Limit: 1,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 2*time.Second)
	if len(got) != 1 || got[0].EventID != "01J0LIVE" {
		t.Fatalf("got %v want only 01J0LIVE", eventIDs(got))
	}
}

func TestWatch_DedupSetBoundedByReplaySnapshot(t *testing.T) {
	t.Parallel()
	// Regression for the unbounded-memory finding: the dedup set must not
	// grow with the live stream. After 1k live events, the set's footprint
	// must still equal the size of the replay snapshot — otherwise long-
	// running `corvee watch` processes leak memory linearly with throughput.
	tail := testfakes.NewTailer()
	log := testfakes.NewEventLog()
	t0 := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)

	// Replay snapshot: 3 events.
	for i, id := range []string{"01J0R1", "01J0R2", "01J0R3"} {
		_ = log.Append(mkWatchEvent(id, "item_added", "RKN-E01", t0.Add(time.Duration(i)*time.Second)))
	}
	const liveCount = 1000
	for i := 0; i < liveCount; i++ {
		tail.Send(mkWatchEvent(idForWatchTest(i), "claimed", "RKN-E01", t0.Add(time.Hour+time.Duration(i)*time.Millisecond)))
	}

	d := usecase.Deps{EventLog: log, EventTailer: tail}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := usecase.Watch(ctx, d, usecase.WatchInput{
		Since: t0, HasSince: true, Limit: 3 + liveCount,
	})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	got := collectWatch(t, ch, 5*time.Second)
	if len(got) != 3+liveCount {
		t.Fatalf("got %d events, want %d", len(got), 3+liveCount)
	}
	// Indirect check on memory bound: ensure the impl exposes the
	// observed dedup-set size via WatchDedupSize for tests. If the impl
	// were to add live IDs to the set it would exceed the replay size.
	if size := usecase.WatchDedupSizeForTest(); size != 3 {
		t.Fatalf("dedup set size=%d after live stream; want 3 (replay-only)", size)
	}
}

func idForWatchTest(i int) string {
	const idLen = 24
	out := []byte("01J0L")
	rest := make([]byte, idLen-len(out))
	for j := range rest {
		rest[j] = '0'
	}
	v := i
	if v == 0 {
		v = 1
	}
	s := []byte{}
	for v > 0 {
		s = append([]byte{byte('0' + v%10)}, s...)
		v /= 10
	}
	copy(rest[len(rest)-len(s):], s)
	out = append(out, rest...)
	return string(out)
}

func eventIDs(evs []domain.Event) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.EventID
	}
	return out
}
