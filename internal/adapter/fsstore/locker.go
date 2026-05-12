package fsstore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Locker implements domain.Locker using O_EXCL lock files under
// .tasks/locks/<id>.lock. Per spec §6 Layer 4 + §13, this works only
// on local POSIX filesystems — networked storage is explicitly
// unsupported. The file's existence *is* the lock; on release we
// remove it.
//
// Acquire polls at a fixed cadence until it can create the lockfile or
// the supplied budget elapses. Phase 3 ships this; Phase 4 stacks the
// crash-recovery sweep that re-claims orphaned lockfiles.
type Locker struct {
	dir string
}

// NewLocker returns a Locker rooted at dir (e.g. .tasks/locks/).
// Auto-creates dir if missing — Phase 7 multi-VM clones may not yet
// have it.
func NewLocker(dir string) (*Locker, error) {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
			return nil, fmt.Errorf("fsstore.locker: mkdir %q: %w", dir, mkErr)
		}
		return &Locker{dir: dir}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("fsstore.locker: stat %q: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fsstore.locker: %q is not a directory", dir)
	}
	return &Locker{dir: dir}, nil
}

// Acquire implements domain.Locker. timeout==0 means "try once".
// Polling cadence is 25ms which gives sub-millisecond contention test
// reliability without burning CPU under hold.
func (l *Locker) Acquire(id string, timeout time.Duration) (domain.LockHandle, error) {
	if _, err := domain.ParseID(id); err != nil {
		return nil, fmt.Errorf("fsstore.locker: invalid id %q: %w", id, domain.ErrUsage)
	}
	path := filepath.Join(l.dir, id+".lock")
	deadline := time.Now().Add(timeout)
	const poll = 25 * time.Millisecond

	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			// Stamp the file with the holder's pid so a stale lock can
			// be diagnosed by humans inspecting the directory.
			fmt.Fprintf(f, "pid=%d\nacquired_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339Nano))
			_ = f.Close()
			return &lockHandle{path: path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, fmt.Errorf("fsstore.locker: acquire %s: %w", id, err)
		}
		if timeout == 0 || !time.Now().Before(deadline) {
			return nil, fmt.Errorf("fsstore.locker: %s: %w", id, domain.ErrLockTimeout)
		}
		time.Sleep(poll)
	}
}

// lockHandle holds the path of an acquired lockfile and the
// release-once invariant.
type lockHandle struct {
	mu       sync.Mutex
	path     string
	released bool
}

// Release implements domain.LockHandle. Double-release is rejected.
func (h *lockHandle) Release() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return fmt.Errorf("fsstore.locker: lock %s already released: %w", h.path, domain.ErrUsage)
	}
	h.released = true
	if err := os.Remove(h.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("fsstore.locker: release %s: %w", h.path, err)
	}
	return nil
}
