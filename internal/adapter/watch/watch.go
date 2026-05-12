package watch

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/mandarnilange/corvee/internal/domain"
)

// defaultPollInterval is how often Tail checks the injected Clock for a
// day-boundary crossing and re-drains every known shard for any writes
// that fsnotify might have coalesced. One second is short enough for
// reactive `corvee watch` UX without burning CPU.
const defaultPollInterval = time.Second

// shardSuffix is appended to the date stem to compose a shard filename.
// Matches the layout written by adapter/eventlog.
const shardSuffix = ".jsonl"

// Tailer is a fsnotify-backed live tail of the daily-sharded event log.
// It owns one fsnotify.Watcher and one goroutine per Tail() call;
// closing the returned channel is the only signal the consumer needs.
//
// The Tailer keeps a per-shard read cursor: any shard fsnotify reports
// a write for is drained from where the previous read left off. This
// means a rolled-over shard (yesterday's, after midnight) still has
// late writes delivered — important because eventlog.Append shards by
// the event's own timestamp, not the watcher's wall clock, so a
// backdated or clock-skewed event can land in a shard the watcher has
// already moved past.
//
// Historical events that exist before Tail starts are re-delivered for
// the current shard. The downstream usecase de-duplicates by EventID
// so pairing Tail with an EventLog.Read snapshot does not double-emit.
type Tailer struct {
	dir          string
	clock        domain.Clock
	pollInterval time.Duration
}

// New returns a Tailer rooted at the events directory dir. The
// directory must already exist (created by `corvee init`); Tail will
// fail otherwise. clock supplies the day-boundary signal.
func New(dir string, clock domain.Clock) *Tailer {
	return &Tailer{dir: dir, clock: clock, pollInterval: defaultPollInterval}
}

// WithPollInterval overrides the default tick rate. Tests use a
// millisecond-scale interval so midnight-rollover assertions complete
// quickly; production callers should leave the default.
func (t *Tailer) WithPollInterval(d time.Duration) *Tailer {
	t.pollInterval = d
	return t
}

// Tail starts streaming events from today's shard and any other shard
// that subsequently receives writes. The returned channel closes when
// ctx is cancelled or the underlying watcher fails. Errors during file
// I/O after startup are intentionally swallowed — the audit log is a
// best-effort viewing surface, not a transactional source.
func (t *Tailer) Tail(ctx context.Context) (<-chan domain.Event, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: new fsnotify: %w", err)
	}
	if err := fw.Add(t.dir); err != nil {
		_ = fw.Close()
		return nil, fmt.Errorf("watch: add %s: %w", t.dir, err)
	}
	out := make(chan domain.Event, 64)
	go t.run(ctx, fw, out)
	return out, nil
}

// shardState holds an open file plus a buffered reader positioned at
// the next unread byte for one event-log shard.
type shardState struct {
	f      *os.File
	reader *bufio.Reader
}

func (t *Tailer) run(ctx context.Context, fw *fsnotify.Watcher, out chan<- domain.Event) {
	defer close(out)
	defer func() { _ = fw.Close() }()

	shards := make(map[string]*shardState)
	defer func() {
		for _, s := range shards {
			_ = s.f.Close()
		}
	}()

	openShard := func(name string) *shardState {
		if s, ok := shards[name]; ok {
			return s
		}
		path := filepath.Join(t.dir, name)
		nf, err := os.Open(path)
		if err != nil {
			// Shard not yet created (e.g., we're tracking today before
			// the first append). Caller retries on the next event.
			return nil
		}
		s := &shardState{f: nf, reader: bufio.NewReader(nf)}
		shards[name] = s
		return s
	}

	drainShard := func(name string) {
		s := openShard(name)
		if s == nil {
			return
		}
		for {
			line, err := s.reader.ReadBytes('\n')
			if n := len(line); n > 0 && line[n-1] == '\n' {
				// TrimSpace handles both '\n' and '\r\n' line endings.
				trimmed := bytes.TrimSpace(line)
				if len(trimmed) > 0 {
					var ev domain.Event
					if jerr := json.Unmarshal(trimmed, &ev); jerr == nil {
						select {
						case <-ctx.Done():
							return
						case out <- ev:
						}
					}
				}
			} else if n > 0 {
				// Partial trailing line — writer hasn't flushed the
				// newline. Seek back so we re-read it on the next pass.
				_, _ = s.f.Seek(-int64(n), io.SeekCurrent)
				return
			}
			if err != nil {
				return
			}
		}
	}

	shardFor := func() string {
		return t.clock.Now().UTC().Format("2006-01-02") + shardSuffix
	}

	// Start tracking today's shard so the very first append is observed
	// even when fsnotify coalesces the CREATE+WRITE pair.
	drainShard(shardFor())

	ticker := time.NewTicker(t.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fw.Events:
			if !ok {
				return
			}
			base := filepath.Base(ev.Name)
			if !strings.HasSuffix(base, shardSuffix) {
				continue
			}
			drainShard(base)
		case _, ok := <-fw.Errors:
			if !ok {
				return
			}
			// fsnotify errors are best-effort; loop continues.
		case <-ticker.C:
			// Periodic catch-up:
			//   1. Open today's shard if it's new (handles the post-
			//      midnight case where no fsnotify event has fired yet).
			//   2. Re-drain every known shard so writes that fsnotify
			//      coalesced or dropped are still delivered.
			drainShard(shardFor())
			for name := range shards {
				drainShard(name)
			}
		}
	}
}
