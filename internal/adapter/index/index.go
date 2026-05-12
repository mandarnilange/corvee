package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Indexer implements domain.Indexer backed by a single
// .tasks/index.json file plus a walk of .tasks/items/ for staleness
// detection. The cache is gitignored — never source of truth — and is
// rebuildable via the Reindex usecase.
type Indexer struct {
	path     string
	itemsDir string
}

// New returns an Indexer that reads/writes path and computes
// staleness against itemsDir.
func New(path, itemsDir string) *Indexer {
	return &Indexer{path: path, itemsDir: itemsDir}
}

// Load implements domain.Indexer. Returns ErrNotFound when the cache
// file is absent so callers (Reindex) know to rebuild.
func (i *Indexer) Load() (domain.Index, error) {
	data, err := os.ReadFile(i.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.Index{}, fmt.Errorf("index: load: %w", domain.ErrNotFound)
		}
		return domain.Index{}, fmt.Errorf("index: load: %w", err)
	}
	var idx domain.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return domain.Index{}, fmt.Errorf("index: parse: %w", domain.ErrMalformedJSON)
	}
	return idx, nil
}

// Save implements domain.Indexer. Atomic via tmp+fsync+rename so a
// crash never leaves a torn cache visible.
//
// Save also stamps Index.MaxFileModTime by scanning the items
// directory just before serializing. This decouples Reindex from the
// filesystem (it owns the in-memory Index; the adapter owns the
// freshness baseline) and means a subsequent Stale() probe can
// short-circuit on stat alone — see review-finding-8.
func (i *Indexer) Save(idx domain.Index) error {
	latest, _, scanErr := scanItemsModTime(i.itemsDir)
	if scanErr != nil {
		return fmt.Errorf("index: scan for save: %w", scanErr)
	}
	idx.MaxFileModTime = latest
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("index: marshal: %w", err)
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", i.path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("index: open tmp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("index: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("index: fsync: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("index: close: %w", err)
	}
	if err := os.Rename(tmp, i.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("index: rename: %w", err)
	}
	return nil
}

// Stale implements domain.Indexer. The cache is fresh iff its
// MaxFileModTime equals the on-disk max(file ModTime) AND the item
// count matches. A missing cache reads as stale.
//
// Closing review-finding-8: this used to read+parse every .json
// payload to compute max(updated_at). We now scan via os.ReadDir +
// FileInfo, which is N stat calls instead of N file reads + N JSON
// parses. Stale() is on the hot path of every list/show/next call;
// the prior cost was O(items × file-read) per invocation.
func (i *Indexer) Stale() (bool, error) {
	idx, err := i.Load()
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return true, nil
		}
		return true, err
	}
	latest, count, err := scanItemsModTime(i.itemsDir)
	if err != nil {
		return true, err
	}
	if count != idx.ItemCount {
		return true, nil
	}
	if !latest.Equal(idx.MaxFileModTime) {
		return true, nil
	}
	return false, nil
}

// scanItemsModTime reads dir entries and returns max(ModTime) and
// the count of well-named item files. No file payloads are read.
func scanItemsModTime(dir string) (time.Time, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return time.Time{}, 0, nil
		}
		return time.Time{}, 0, fmt.Errorf("index: scan: %w", err)
	}
	var latest time.Time
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.Contains(name, ".tmp.") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if _, err := domain.ParseID(id); err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return time.Time{}, 0, fmt.Errorf("index: stat %s: %w", name, err)
		}
		count++
		if mt := info.ModTime(); mt.After(latest) {
			latest = mt
		}
	}
	return latest, count, nil
}
