package testfakes

import (
	"fmt"
	"sync"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Locker is an in-memory fake of domain.Locker. Per-id sync.Mutexes
// serialize Acquire calls within one process; cross-process coverage
// is provided by the real fsstore.Locker.
type Locker struct {
	mu    sync.Mutex
	locks map[string]*lockSlot
}

type lockSlot struct {
	mu sync.Mutex
}

// NewLocker returns an empty fake locker.
func NewLocker() *Locker { return &Locker{locks: map[string]*lockSlot{}} }

// Acquire blocks up to timeout waiting for the per-id mutex.
func (l *Locker) Acquire(id string, timeout time.Duration) (domain.LockHandle, error) {
	l.mu.Lock()
	slot, ok := l.locks[id]
	if !ok {
		slot = &lockSlot{}
		l.locks[id] = slot
	}
	l.mu.Unlock()

	if timeout == 0 {
		if !tryLock(&slot.mu) {
			return nil, fmt.Errorf("locker(fake): %s: %w", id, domain.ErrLockTimeout)
		}
		return &fakeHandle{slot: slot}, nil
	}
	deadline := time.Now().Add(timeout)
	for {
		if tryLock(&slot.mu) {
			return &fakeHandle{slot: slot}, nil
		}
		if !time.Now().Before(deadline) {
			return nil, fmt.Errorf("locker(fake): %s: %w", id, domain.ErrLockTimeout)
		}
		time.Sleep(time.Millisecond)
	}
}

func tryLock(m *sync.Mutex) bool {
	return m.TryLock()
}

type fakeHandle struct {
	mu       sync.Mutex
	slot     *lockSlot
	released bool
}

// Release frees the per-id mutex. Double-release is rejected with
// ErrUsage.
func (h *fakeHandle) Release() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.released {
		return fmt.Errorf("locker(fake): release: %w", domain.ErrUsage)
	}
	h.released = true
	h.slot.mu.Unlock()
	return nil
}
