package usecase

import (
	"context"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// lastWatchDedupSize records the high-water size of the dedup set from
// the most recent Watch call. Used by TestWatch_DedupSetBounded* to
// regression-test the unbounded-memory fix from the Phase 5 review.
// Production code does not consult this value.
var lastWatchDedupSize atomic.Int64

// WatchDedupSizeForTest returns the dedup-set size observed by the most
// recent Watch invocation. Exported for tests in the _test package.
func WatchDedupSizeForTest() int { return int(lastWatchDedupSize.Load()) }

// WatchInput is the request payload for Watch (spec §15.2).
type WatchInput struct {
	// Filter constrains both replayed and live events.
	Filter domain.EventFilter
	// Since is the lower bound for the historical replay phase. Only
	// consulted when HasSince is true.
	Since time.Time
	// HasSince is true when the caller passed --since. Distinguishes
	// "replay from epoch" (never wanted) from "no replay" (live only).
	HasSince bool
	// Limit, when > 0, terminates the stream after that many events
	// (replayed + live combined) have been delivered.
	Limit int
	// ExitOn lists event Type values that, when seen, terminate the
	// stream. The triggering event IS delivered; subsequent events are
	// not.
	ExitOn []string
}

// Watch returns a channel that first replays historical events from
// EventLog (when HasSince is true) and then streams live events from
// EventTailer. Replay events are de-duplicated against live events by
// EventID so callers don't see doubles for entries that exist in both.
//
// The returned channel closes when ctx is cancelled, the limit is
// reached, an exit-on event has been delivered, or the tailer's
// channel closes.
func Watch(ctx context.Context, d Deps, in WatchInput) (<-chan domain.Event, error) {
	if d.EventLog == nil || d.EventTailer == nil {
		return nil, fmt.Errorf("watch: EventLog and EventTailer required: %w", domain.ErrUsage)
	}

	out := make(chan domain.Event, 64)
	exitOn := make(map[string]struct{}, len(in.ExitOn))
	for _, t := range in.ExitOn {
		exitOn[t] = struct{}{}
	}

	go runWatch(ctx, d, in, out, exitOn)
	return out, nil
}

func runWatch(ctx context.Context, d Deps, in WatchInput, out chan<- domain.Event, exitOn map[string]struct{}) {
	defer close(out)

	delivered := 0
	// seen holds only the EventIDs emitted during the historical replay
	// phase so the live stream can drop the tailer's re-emission of the
	// same shard. Live event IDs are checked but never inserted, so the
	// set's size is bounded by the replay snapshot — not the lifetime of
	// the watch process. This is the fix for the Phase 5 review's
	// unbounded-memory finding.
	seen := make(map[string]struct{})

	emit := func(ev domain.Event, isReplay bool) (stop bool) {
		if !in.Filter.Match(ev) {
			return false
		}
		// Live phase: drop events older than --since. Catches backdated
		// events appended after Watch started (clock skew on a peer VM)
		// that EventLog.Read() never saw, so the seen[] map can't
		// dedup them.
		if !isReplay && in.HasSince && ev.Timestamp.Before(in.Since) {
			return false
		}
		if !isReplay {
			if _, dup := seen[ev.EventID]; dup && ev.EventID != "" {
				return false
			}
		}
		select {
		case <-ctx.Done():
			return true
		case out <- ev:
		}
		// Note: seen-population happens in the replay block below for
		// every snapshot event (including pre-Since), not here. This
		// keeps the live phase from inserting and bounds the set size
		// to the EventLog snapshot.
		delivered++
		if in.Limit > 0 && delivered >= in.Limit {
			return true
		}
		if _, ok := exitOn[ev.Type]; ok {
			return true
		}
		return false
	}

	if in.HasSince {
		events, err := d.EventLog.Read()
		if err == nil {
			sort.SliceStable(events, func(i, j int) bool {
				return events[i].Timestamp.Before(events[j].Timestamp)
			})
			for _, ev := range events {
				// Always seed seen, even for events we won't emit, so the
				// production tailer's byte-0 re-emission of today's shard
				// dedups against the full snapshot — not just the slice
				// the user wanted to see.
				if ev.EventID != "" {
					seen[ev.EventID] = struct{}{}
				}
				if ev.Timestamp.Before(in.Since) {
					continue
				}
				if emit(ev, true) {
					lastWatchDedupSize.Store(int64(len(seen)))
					return
				}
			}
		}
	}

	live, err := d.EventTailer.Tail(ctx)
	if err != nil {
		lastWatchDedupSize.Store(int64(len(seen)))
		return
	}
	for {
		select {
		case <-ctx.Done():
			lastWatchDedupSize.Store(int64(len(seen)))
			return
		case ev, ok := <-live:
			if !ok {
				lastWatchDedupSize.Store(int64(len(seen)))
				return
			}
			if emit(ev, false) {
				lastWatchDedupSize.Store(int64(len(seen)))
				return
			}
		}
	}
}
