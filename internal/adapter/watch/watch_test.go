package watch

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/adapter/eventlog"
	"github.com/mandarnilange/corvee/internal/domain"
)

// fakeClock is a goroutine-safe Clock for tests. Implements domain.Clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock(t time.Time) *fakeClock { return &fakeClock{now: t} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

func mkEvent(id string, ts time.Time, typ string) domain.Event {
	return domain.Event{
		EventID:   id,
		Timestamp: ts,
		Type:      typ,
		ItemID:    "RKN-E01",
		Actor:     "alice",
	}
}

// shortPoll keeps tests fast — production callers tune this themselves.
const shortPoll = 5 * time.Millisecond

func waitForEvent(t *testing.T, ch <-chan domain.Event, want string, deadline time.Duration) domain.Event {
	t.Helper()
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed waiting for event %s", want)
			}
			if ev.EventID == want {
				return ev
			}
			// Skip mismatched events; tests assert sequence elsewhere.
		case <-timer.C:
			t.Fatalf("timed out waiting for event %s", want)
		}
	}
}

func TestTailer_DeliversAppendedEventsFromCurrentShard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	clk := newFakeClock(day)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	if err := log.Append(mkEvent("01J0E1", day, "item_added")); err != nil {
		t.Fatalf("append: %v", err)
	}
	got := waitForEvent(t, ch, "01J0E1", 2*time.Second)
	if got.Type != "item_added" {
		t.Fatalf("got type %q want item_added", got.Type)
	}

	if err := log.Append(mkEvent("01J0E2", day.Add(time.Second), "claimed")); err != nil {
		t.Fatalf("append: %v", err)
	}
	waitForEvent(t, ch, "01J0E2", 2*time.Second)
}

func TestTailer_ContextCancelClosesChannel(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clk := newFakeClock(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC))
	tailer := New(dir, clk).WithPollInterval(shortPoll)

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
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
			t.Fatalf("channel did not close within deadline after ctx cancel")
		}
	}
}

func TestTailer_RollsOverAtMidnight(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day1 := time.Date(2026, 5, 6, 23, 59, 0, 0, time.UTC)
	day2 := time.Date(2026, 5, 7, 0, 0, 1, 0, time.UTC)
	clk := newFakeClock(day1)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	// Event in day1's shard.
	if err := log.Append(mkEvent("01J0DAY1A", day1, "item_added")); err != nil {
		t.Fatalf("append day1: %v", err)
	}
	waitForEvent(t, ch, "01J0DAY1A", 2*time.Second)

	// Cross the midnight boundary on the watcher's clock.
	clk.Set(day2)
	// Allow the next poll cycle to detect the boundary.
	time.Sleep(3 * shortPoll)

	// Event timestamped at day2 lands in day2's shard.
	if err := log.Append(mkEvent("01J0DAY2A", day2, "claimed")); err != nil {
		t.Fatalf("append day2: %v", err)
	}
	waitForEvent(t, ch, "01J0DAY2A", 2*time.Second)
}

func TestTailer_DeliversLateWriteToClosingShardAfterRollover(t *testing.T) {
	t.Parallel()
	// Race 1 from review: an fsnotify WRITE on day1 that arrives after
	// the clock crossed midnight must drain day1 before switching to
	// day2 — otherwise the line is lost.
	dir := t.TempDir()
	day1 := time.Date(2026, 5, 6, 23, 59, 59, 0, time.UTC)
	day2 := time.Date(2026, 5, 7, 0, 0, 1, 0, time.UTC)
	clk := newFakeClock(day1)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	// Cross midnight on the watcher's clock BEFORE the producer appends.
	clk.Set(day2)
	// One poll cycle so the watcher learns about the new shardFor().
	time.Sleep(3 * shortPoll)

	// Now append a backdated event that lands in day1's shard. The
	// watcher must still deliver it.
	if err := log.Append(mkEvent("01J0LATE_DAY1", day1, "item_added")); err != nil {
		t.Fatalf("append day1: %v", err)
	}
	waitForEvent(t, ch, "01J0LATE_DAY1", 2*time.Second)
}

func TestTailer_DeliversConcurrentWriteToOldShardDuringRollover(t *testing.T) {
	t.Parallel()
	// Race 2 from review: a write to day1 between the watcher's drain
	// and openShard(day2) must not be stranded. Repeats across many
	// rollover cycles so the TOCTOU window has the chance to bite.
	dir := t.TempDir()
	day1 := time.Date(2026, 5, 6, 23, 59, 58, 0, time.UTC)
	day2 := time.Date(2026, 5, 7, 0, 0, 2, 0, time.UTC)
	clk := newFakeClock(day1)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	const interleave = 8
	want := make([]string, 0, interleave*2)

	// Burst day1 events; midway, flip the clock to day2 while the producer
	// continues appending day1-timestamped events.
	for i := 0; i < interleave; i++ {
		id := idFor(i, "01J0DAY1") // declared in integration_test.go
		want = append(want, id)
		if err := log.Append(mkEvent(id, day1.Add(time.Duration(i)*time.Millisecond), "item_added")); err != nil {
			t.Fatalf("append day1: %v", err)
		}
		if i == interleave/2 {
			clk.Set(day2)
		}
	}
	// And finally some day2 events.
	for i := 0; i < interleave; i++ {
		id := idFor(i, "01J0DAY2")
		want = append(want, id)
		if err := log.Append(mkEvent(id, day2.Add(time.Duration(i)*time.Millisecond), "claimed")); err != nil {
			t.Fatalf("append day2: %v", err)
		}
	}

	got := drainExpected(t, ch, len(want), 5*time.Second)
	gotSet := make(map[string]int)
	for _, id := range got {
		gotSet[id]++
	}
	for _, id := range want {
		if gotSet[id] != 1 {
			t.Errorf("event %s delivered %d times; want exactly 1", id, gotSet[id])
		}
	}
}

func TestTailer_NoDuplicateDelivery(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	clk := newFakeClock(day)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	for i, id := range []string{"01J0A", "01J0B", "01J0C"} {
		if err := log.Append(mkEvent(id, day.Add(time.Duration(i)*time.Second), "item_added")); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}

	seen := make(map[string]int)
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				break loop
			}
			seen[ev.EventID]++
		case <-deadline:
			break loop
		}
	}
	for _, id := range []string{"01J0A", "01J0B", "01J0C"} {
		if seen[id] != 1 {
			t.Fatalf("event %s delivered %d times (want 1); all=%v", id, seen[id], seen)
		}
	}
}

func TestTailer_TolerantToCRLFLineEndings(t *testing.T) {
	t.Parallel()
	// Defense against the review's "fragile newline stripping" finding.
	// Although the production eventlog only writes '\n', a tool that
	// rewrote the shard with CRLF endings shouldn't poison the watcher.
	dir := t.TempDir()
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	clk := newFakeClock(day)
	tailer := New(dir, clk).WithPollInterval(shortPoll)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	// Hand-craft a CRLF-terminated JSONL shard.
	path := dir + "/2026-05-06.jsonl"
	payload := []byte(`{"event_id":"01J0CRLF","ts":"2026-05-06T12:00:00Z","type":"item_added","item_id":"RKN-E01","actor":"alice"}` + "\r\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitForEvent(t, ch, "01J0CRLF", 2*time.Second)
}

func TestTailer_StartsBeforeAnyShardExists(t *testing.T) {
	t.Parallel()
	// The events directory may be empty when the watcher starts (a fresh
	// workspace). Tail must not error on that — it should wait for the
	// first shard to be created and stream events as soon as they land.
	dir := t.TempDir()
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	clk := newFakeClock(day)

	tailer := New(dir, clk).WithPollInterval(shortPoll)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	if err := log.Append(mkEvent("01J0FIRST", day, "init")); err != nil {
		t.Fatalf("append: %v", err)
	}
	waitForEvent(t, ch, "01J0FIRST", 2*time.Second)
}
