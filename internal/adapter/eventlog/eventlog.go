package eventlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Log is the daily-sharded JSONL EventLog. One file per UTC date; each
// line is one JSON-encoded domain.Event. Files are append-only; reads
// concatenate every shard in chronological order.
//
// The mutex serializes Append calls within one process; cross-process
// safety relies on POSIX O_APPEND semantics (each write either lands
// or doesn't, never overlaps another writer's record).
type Log struct {
	dir string
	mu  sync.Mutex
}

// New returns a Log rooted at dir. Auto-creates dir if missing — the
// Phase 7 multi-VM topology needs this because git doesn't track
// empty directories, so an executor clone won't have .tasks/events/
// until the planner has written its first event.
func New(dir string) (*Log, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("eventlog: mkdir %q: %w", dir, mkErr)
		}
		return &Log{dir: dir}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("eventlog: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("eventlog: %q is not a directory", dir)
	}
	return &Log{dir: dir}, nil
}

// Append implements domain.EventLog. The shard is chosen from
// ev.Timestamp's UTC date; missing timestamp is rejected as ErrUsage.
func (l *Log) Append(ev domain.Event) error {
	if ev.EventID == "" {
		return fmt.Errorf("eventlog: event_id is required: %w", domain.ErrUsage)
	}
	if ev.Timestamp.IsZero() {
		return fmt.Errorf("eventlog: timestamp is required: %w", domain.ErrUsage)
	}
	if ev.Type == "" {
		return fmt.Errorf("eventlog: type is required: %w", domain.ErrUsage)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("eventlog: marshal: %w", err)
	}
	data = append(data, '\n')

	shardName := ev.Timestamp.UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(l.dir, shardName)

	l.mu.Lock()
	defer l.mu.Unlock()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("eventlog: write %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("eventlog: fsync %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("eventlog: close %s: %w", path, err)
	}
	return nil
}

// Read implements domain.EventLog. Returns every event from every
// shard in chronological order (shard filename ascending, then
// per-line order). Corrupt lines are skipped silently — the log is an
// audit trail, not a transactional source of truth, so a partial line
// from a crashed write does not poison the read.
func (l *Log) Read() ([]domain.Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("eventlog: read dir: %w", err)
	}
	shards := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		shards = append(shards, name)
	}
	sort.Strings(shards)

	var out []domain.Event
	for _, name := range shards {
		path := filepath.Join(l.dir, name)
		events, err := readShard(path)
		if err != nil {
			return nil, err
		}
		out = append(out, events...)
	}
	return out, nil
}

// Scan implements domain.EventLog. Streams events shard-by-shard in
// chronological order, invoking fn once per record. Returning a
// non-nil error from fn aborts the scan immediately. Unlike Read, no
// shard's full body is held in memory across the call — the caller's
// fn sees one event at a time.
func (l *Log) Scan(fn func(domain.Event) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return fmt.Errorf("eventlog: read dir: %w", err)
	}
	shards := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		shards = append(shards, name)
	}
	sort.Strings(shards)
	for _, name := range shards {
		path := filepath.Join(l.dir, name)
		if err := scanShard(path, fn); err != nil {
			return err
		}
	}
	return nil
}

// scanShard streams one shard file into fn line-by-line. Returning an
// error from fn aborts immediately and propagates without wrapping so
// callers can errors.Is against their sentinel.
func scanShard(path string, fn func(domain.Event) error) error {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev domain.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("eventlog: scan %s: %w", path, err)
	}
	return nil
}

func readShard(path string) ([]domain.Event, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("eventlog: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var out []domain.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev domain.Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // skip corrupt line
		}
		out = append(out, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: scan %s: %w", path, err)
	}
	return out, nil
}
