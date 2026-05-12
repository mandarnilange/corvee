package watch

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/adapter/eventlog"
	"github.com/mandarnilange/corvee/internal/domain"
)

// TestWatch_CrossGoroutinePickup is the integration test from
// TTR-E05-S07: a producer goroutine appends events through the real
// EventLog while a consumer goroutine tails them via the Tailer. It
// verifies that every appended event is delivered, in order, within a
// deadline. Run under -race -count=10 in CI to surface the fsnotify
// rename/create races that midnight rotation could regress.
func TestWatch_CrossGoroutinePickup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	clk := newFakeClock(day)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(2 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	const n = 50
	produced := make([]string, n)
	for i := 0; i < n; i++ {
		produced[i] = idFor(i, "01J0CG")
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i, id := range produced {
			ev := domain.Event{
				EventID:   id,
				Timestamp: day.Add(time.Duration(i) * time.Millisecond),
				Type:      "item_added",
				ItemID:    "RKN-E01",
				Actor:     "alice",
			}
			if err := log.Append(ev); err != nil {
				t.Errorf("append %d: %v", i, err)
				return
			}
		}
	}()

	got := drainExpected(t, ch, n, 5*time.Second)
	wg.Wait()

	if !equalSlices(got, produced) {
		t.Fatalf("delivery mismatch.\n got %v\nwant %v", got, produced)
	}
}

// TestWatch_MidnightRolloverRegression drives the FixedClock across a
// UTC day boundary while the producer appends events to the closing
// shard just before midnight and the opening shard just after. Every
// event must be delivered exactly once, in order. This is the §11.5
// concurrency-confidence guard for the rotation path.
func TestWatch_MidnightRolloverRegression(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day1Late := time.Date(2026, 5, 6, 23, 59, 58, 0, time.UTC)
	day2Early := time.Date(2026, 5, 7, 0, 0, 2, 0, time.UTC)
	clk := newFakeClock(day1Late)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}

	tailer := New(dir, clk).WithPollInterval(2 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	const halfBurst = 15
	want := make([]string, 0, halfBurst*2)
	for i := 0; i < halfBurst; i++ {
		want = append(want, idFor(i, "01J0PRE"))
	}
	for i := 0; i < halfBurst; i++ {
		want = append(want, idFor(i, "01J0POST"))
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Pre-midnight burst: timestamps land in day1's shard.
		for i := 0; i < halfBurst; i++ {
			ev := domain.Event{
				EventID:   idFor(i, "01J0PRE"),
				Timestamp: day1Late.Add(time.Duration(i) * time.Millisecond),
				Type:      "item_added",
				ItemID:    "RKN-E01",
				Actor:     "alice",
			}
			if err := log.Append(ev); err != nil {
				t.Errorf("pre append: %v", err)
				return
			}
		}
		// Cross the boundary on the watcher's clock.
		clk.Set(day2Early)
		// Wait one poll cycle so the watcher closes the day1 handle.
		time.Sleep(8 * time.Millisecond)
		// Post-midnight burst: timestamps land in day2's shard.
		for i := 0; i < halfBurst; i++ {
			ev := domain.Event{
				EventID:   idFor(i, "01J0POST"),
				Timestamp: day2Early.Add(time.Duration(i) * time.Millisecond),
				Type:      "claimed",
				ItemID:    "RKN-E01",
				Actor:     "alice",
			}
			if err := log.Append(ev); err != nil {
				t.Errorf("post append: %v", err)
				return
			}
		}
	}()

	got := drainExpected(t, ch, len(want), 10*time.Second)
	wg.Wait()

	// Exactly-once: every produced id appears precisely once.
	gotSet := make(map[string]int)
	for _, id := range got {
		gotSet[id]++
	}
	for _, id := range want {
		if gotSet[id] != 1 {
			t.Errorf("event %s delivered %d times; want exactly 1; gotSet=%v",
				id, gotSet[id], gotSet)
		}
	}

	// Order: every 01J0PRE id must precede every 01J0POST id in delivery.
	// The producer appends pre-midnight first, then post-midnight, so the
	// watcher should never interleave them in the other direction.
	lastPreIdx, firstPostIdx := -1, len(got)
	for i, id := range got {
		switch {
		case strings.HasPrefix(id, "01J0PRE"):
			lastPreIdx = i
		case strings.HasPrefix(id, "01J0POST"):
			if i < firstPostIdx {
				firstPostIdx = i
			}
		}
	}
	if lastPreIdx >= firstPostIdx {
		t.Fatalf("ordering violated: PRE id at index %d delivered after POST id at index %d; got=%v",
			lastPreIdx, firstPostIdx, got)
	}
}

// TestWatch_ConcurrentProducerAcrossMidnight stresses the rotation
// path with a producer goroutine that keeps appending across the UTC
// boundary while a separate goroutine flips the watcher's clock. No
// time.Sleep gates the producer's output to "after" the boundary —
// writes interleave with the rollover. Asserts exactly-once delivery
// of every emitted event. Run under -race -count=10 in CI to surface
// any TOCTOU between drain and shard-switch.
func TestWatch_ConcurrentProducerAcrossMidnight(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	day1 := time.Date(2026, 5, 6, 23, 59, 58, 0, time.UTC)
	day2 := time.Date(2026, 5, 7, 0, 0, 2, 0, time.UTC)
	clk := newFakeClock(day1)

	log, err := eventlog.New(dir)
	if err != nil {
		t.Fatalf("new log: %v", err)
	}
	tailer := New(dir, clk).WithPollInterval(2 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("tail: %v", err)
	}

	const total = 40
	want := make([]string, total)
	for i := 0; i < total; i++ {
		want[i] = idFor(i, "01J0CONC")
	}

	var wg sync.WaitGroup
	wg.Add(2)
	// Producer: emits all `total` events back-to-back.
	go func() {
		defer wg.Done()
		for i := 0; i < total; i++ {
			ts := day1.Add(time.Duration(i) * time.Millisecond)
			// Halfway through, switch event timestamps to day2 so the
			// shard the writer targets actually changes mid-burst.
			if i >= total/2 {
				ts = day2.Add(time.Duration(i-total/2) * time.Millisecond)
			}
			ev := domain.Event{
				EventID:   want[i],
				Timestamp: ts,
				Type:      "item_added",
				ItemID:    "RKN-E01",
				Actor:     "alice",
			}
			if err := log.Append(ev); err != nil {
				t.Errorf("append %d: %v", i, err)
				return
			}
		}
	}()
	// Clock-flipper: races against the producer.
	go func() {
		defer wg.Done()
		// Tiny stagger so the flip lands somewhere in the middle of the
		// producer burst rather than always before or after it.
		time.Sleep(time.Millisecond)
		clk.Set(day2)
	}()

	got := drainExpected(t, ch, total, 10*time.Second)
	wg.Wait()
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

// drainExpected reads exactly n events from ch or fails after deadline.
// Returns the IDs in delivery order.
func drainExpected(t *testing.T, ch <-chan domain.Event, n int, deadline time.Duration) []string {
	t.Helper()
	out := make([]string, 0, n)
	timer := time.NewTimer(deadline)
	defer timer.Stop()
	for len(out) < n {
		select {
		case ev, ok := <-ch:
			if !ok {
				t.Fatalf("channel closed after %d/%d events", len(out), n)
			}
			out = append(out, ev.EventID)
		case <-timer.C:
			t.Fatalf("timed out after %d/%d events: got=%v", len(out), n, out)
		}
	}
	return out
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// idFor returns a stable 24-char ULID-shaped id for index i. Padding to
// the canonical length keeps eventlog/Schema validation happy if it's
// ever consulted in tests.
func idFor(i int, prefix string) string {
	const idLen = 24
	out := []byte(prefix)
	rest := make([]byte, idLen-len(prefix))
	for j := range rest {
		rest[j] = '0'
	}
	// Encode i in the trailing bytes (decimal, right-aligned).
	s := []byte{}
	v := i
	if v == 0 {
		s = append(s, '0')
	}
	for v > 0 {
		s = append([]byte{byte('0' + v%10)}, s...)
		v /= 10
	}
	copy(rest[len(rest)-len(s):], s)
	out = append(out, rest...)
	return string(out)
}
