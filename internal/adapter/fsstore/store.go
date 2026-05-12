package fsstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Store implements domain.Store backed by a flat directory of
// <id>.json files. The dir field is the absolute (or working-dir
// relative) path that holds those files.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. Auto-creates dir if missing —
// the Phase 7 multi-VM topology needs this because git doesn't track
// empty directories.
func New(dir string) (*Store, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("fsstore: mkdir %q: %w", dir, mkErr)
		}
		return &Store{dir: dir}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fsstore: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fsstore: %q is not a directory", dir)
	}
	return &Store{dir: dir}, nil
}

// pathFor returns the canonical <dir>/<id>.json path. Validates id
// against the §4 ID grammar so no traversal payloads (slashes, dots,
// empty strings) ever reach the filesystem.
func (s *Store) pathFor(id string) (string, error) {
	if _, err := domain.ParseID(id); err != nil {
		return "", fmt.Errorf("fsstore: invalid id %q: %w", id, domain.ErrUsage)
	}
	return filepath.Join(s.dir, id+".json"), nil
}

// Get implements domain.Store.
func (s *Store) Get(_ context.Context, id string) (domain.Item, error) {
	p, err := s.pathFor(id)
	if err != nil {
		return domain.Item{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.Item{}, fmt.Errorf("fsstore: get %s: %w", id, domain.ErrNotFound)
		}
		return domain.Item{}, fmt.Errorf("fsstore: get %s: %w", id, err)
	}
	var item domain.Item
	if err := json.Unmarshal(data, &item); err != nil {
		return domain.Item{}, fmt.Errorf("fsstore: get %s: %w", id, domain.ErrMalformedJSON)
	}
	return item, nil
}

// Exists implements domain.Store. Cheap stat — does not read the file.
func (s *Store) Exists(_ context.Context, id string) (bool, error) {
	p, err := s.pathFor(id)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("fsstore: exists %s: %w", id, err)
	}
	return true, nil
}

// Delete implements domain.Store. Returns ErrNotFound if the item is
// missing. Soft-delete (status=abandoned) is performed by the Delete
// usecase via Put, not Delete.
func (s *Store) Delete(_ context.Context, id string) error {
	p, err := s.pathFor(id)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("fsstore: delete %s: %w", id, domain.ErrNotFound)
		}
		return fmt.Errorf("fsstore: delete %s: %w", id, err)
	}
	return nil
}

// Put implements domain.Store using the §6 Layer 1 atomic-write
// primitive: write to <id>.json.tmp.<pid>.<ts>, fsync the file, then
// POSIX-rename to <id>.json. The rename is atomic on the local
// filesystems §13 supports, so concurrent readers never see a torn
// or empty file.
//
// expectVersion semantics (§14.1, S09): -1 = no expectation, blind
// overwrite. >= 0 = re-read on-disk Version just before the rename
// and abort with ErrVersionConflict if it differs. A missing file
// counts as "no version to honor" and produces a conflict for any
// non-negative expectation. fsstore never mutates Version or
// UpdatedAt — usecases own time and version intent (no Clock import
// here by design).
//
// Phase 1 ships single-writer; cross-process per-item locking arrives
// in Phase 3 (TTR-E03-S04). The read-then-rename ordering plus POSIX
// rename atomicity is sufficient for that single-writer model.
func (s *Store) Put(_ context.Context, item domain.Item, expectVersion int) (domain.Item, error) {
	p, err := s.pathFor(item.ID)
	if err != nil {
		return domain.Item{}, err
	}

	data, err := json.Marshal(item)
	if err != nil {
		return domain.Item{}, fmt.Errorf("fsstore: put %s: marshal: %w", item.ID, err)
	}

	tmp := fmt.Sprintf("%s.tmp.%d.%d", p, os.Getpid(), time.Now().UnixNano())

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return domain.Item{}, fmt.Errorf("fsstore: put %s: open tmp: %w", item.ID, err)
	}

	cleanup := func() {
		// Best-effort: file already closed before rename, so close-error
		// is just a defensive cover for the partial-write paths below.
		_ = f.Close()
		_ = os.Remove(tmp)
	}

	if _, err := f.Write(data); err != nil {
		cleanup()
		return domain.Item{}, fmt.Errorf("fsstore: put %s: write: %w", item.ID, err)
	}
	if err := f.Sync(); err != nil {
		cleanup()
		return domain.Item{}, fmt.Errorf("fsstore: put %s: fsync: %w", item.ID, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return domain.Item{}, fmt.Errorf("fsstore: put %s: close: %w", item.ID, err)
	}

	if expectVersion >= 0 {
		// Re-read Version just before rename. Tightest possible
		// version-check window we can offer without per-item locking.
		diskVersion, err := s.readVersion(p)
		if err != nil {
			_ = os.Remove(tmp)
			if errors.Is(err, fs.ErrNotExist) {
				return domain.Item{}, fmt.Errorf("fsstore: put %s: expected version %d but item is absent: %w",
					item.ID, expectVersion, domain.ErrVersionConflict)
			}
			return domain.Item{}, fmt.Errorf("fsstore: put %s: re-read for version check: %w", item.ID, err)
		}
		if diskVersion != expectVersion {
			_ = os.Remove(tmp)
			return domain.Item{}, fmt.Errorf("fsstore: put %s: expected version %d, got %d: %w",
				item.ID, expectVersion, diskVersion, domain.ErrVersionConflict)
		}
	}

	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return domain.Item{}, fmt.Errorf("fsstore: put %s: rename: %w", item.ID, err)
	}
	return item, nil
}

// readVersion reads p and returns just the Version field. Used by
// version-aware Put for the re-read step. Wraps fs.ErrNotExist so
// callers can distinguish missing-file from other I/O errors.
func (s *Store) readVersion(p string) (int, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return -1, err
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return -1, fmt.Errorf("%w: %s", domain.ErrMalformedJSON, err.Error())
	}
	return probe.Version, nil
}

// List implements domain.Store. Reads the items directory, parses
// every well-named .json file, drops entries that are tmp files,
// non-canonical names, or corrupt JSON, then delegates filtering and
// sorting to ListFilter.Apply.
func (s *Store) List(ctx context.Context, filter domain.ListFilter) ([]domain.Item, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("fsstore: list: %w", err)
	}
	items := make([]domain.Item, 0, len(entries))
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
			continue // junk filename — skip silently
		}
		item, err := s.Get(ctx, id)
		if err != nil {
			if errors.Is(err, domain.ErrMalformedJSON) {
				continue // corrupt file — skip
			}
			return nil, err
		}
		items = append(items, item)
	}
	return filter.Apply(items), nil
}
