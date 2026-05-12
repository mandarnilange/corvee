package oplog

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Store implements domain.OpResultStore backed by .tasks/operations/
// where each idempotency key gets its own <opID>.json file. The
// directory is gitignored — operation results are per-VM artifacts of
// concurrent execution, not state others need.
type Store struct {
	dir string
}

// New returns a Store rooted at dir. Auto-creates dir if missing.
func New(dir string) (*Store, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("oplog: mkdir %q: %w", dir, mkErr)
		}
		return &Store{dir: dir}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("oplog: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("oplog: %q is not a directory", dir)
	}
	return &Store{dir: dir}, nil
}

func (s *Store) pathFor(opID string) (string, error) {
	if !validOpID(opID) {
		return "", fmt.Errorf("oplog: invalid op_id %q: %w", opID, domain.ErrUsage)
	}
	return filepath.Join(s.dir, opID+".json"), nil
}

// validOpID restricts opIDs to the same alphabet as ULIDs plus '-' so
// no traversal payload can land on disk.
func validOpID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '-' || c == '_':
		default:
			return false
		}
	}
	return true
}

// Get implements domain.OpResultStore.
func (s *Store) Get(opID string) (string, bool, error) {
	p, err := s.pathFor(opID)
	if err != nil {
		return "", false, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("oplog: get %s: %w", opID, err)
	}
	return string(data), true, nil
}

// Put implements domain.OpResultStore using the §6 Layer 1 atomic
// write pattern (tmp+fsync+link). Linking instead of renaming gives
// us O_EXCL semantics on the destination path: a duplicate operation
// id surfaces ErrOpAlreadyRecorded loudly rather than silently
// overwriting a prior caller's record (post-review-finding-4
// hardening).
//
// The caller-visible contract: a same-opID retry is the user's
// signal "please replay the previous outcome" and is handled by Get
// before Put is reached; a same-opID for a *different* outcome is
// caller misuse and will land here with a collision.
func (s *Store) Put(opID, resultJSON string) error {
	p, err := s.pathFor(opID)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d.%d", p, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("oplog: open tmp %s: %w", tmp, err)
	}
	if _, err := f.Write([]byte(resultJSON)); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("oplog: write %s: %w", opID, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("oplog: fsync %s: %w", opID, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("oplog: close %s: %w", opID, err)
	}
	// os.Link creates the destination only if it doesn't exist —
	// behaves like O_EXCL on rename. If the destination exists we
	// surface ErrOpAlreadyRecorded; the tmp file is removed in either
	// branch since hard-link succeeded → tmp is now redundant; failed
	// → tmp is junk.
	if linkErr := os.Link(tmp, p); linkErr != nil {
		_ = os.Remove(tmp)
		if errors.Is(linkErr, fs.ErrExist) {
			return fmt.Errorf("oplog: %s: %w", opID, domain.ErrOpAlreadyRecorded)
		}
		return fmt.Errorf("oplog: link %s: %w", opID, linkErr)
	}
	_ = os.Remove(tmp)
	return nil
}
