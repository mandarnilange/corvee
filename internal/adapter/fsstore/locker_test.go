package fsstore

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func newLockerDir(t *testing.T) *Locker {
	t.Helper()
	dir := t.TempDir()
	l, err := NewLocker(dir)
	if err != nil {
		t.Fatalf("new locker: %v", err)
	}
	return l
}

func TestLocker_Acquire_Release_AllowsReacquire(t *testing.T) {
	t.Parallel()
	l := newLockerDir(t)
	h1, err := l.Acquire("RKN-E01-S01", 0)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if relErr := h1.Release(); relErr != nil {
		t.Fatalf("release: %v", relErr)
	}
	h2, err := l.Acquire("RKN-E01-S01", 0)
	if err != nil {
		t.Fatalf("re-acquire: %v", err)
	}
	_ = h2.Release()
}

func TestLocker_Acquire_HeldByOther_TimesOut(t *testing.T) {
	t.Parallel()
	l := newLockerDir(t)
	h, err := l.Acquire("RKN-E01-S01", 0)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer func() { _ = h.Release() }()

	_, err = l.Acquire("RKN-E01-S01", 50*time.Millisecond)
	if !errors.Is(err, domain.ErrLockTimeout) {
		t.Errorf("want ErrLockTimeout, got %v", err)
	}
}

func TestLocker_Release_DoubleRelease_Rejected(t *testing.T) {
	t.Parallel()
	l := newLockerDir(t)
	h, err := l.Acquire("RKN-E01-S01", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Release(); err != nil {
		t.Fatal(err)
	}
	if err := h.Release(); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestLocker_Concurrent_OnlyOneHolderAtATime(t *testing.T) {
	t.Parallel()
	l := newLockerDir(t)
	const goroutines = 8
	var (
		wg          sync.WaitGroup
		mu          sync.Mutex
		concurrent  int
		maxObserved int
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h, err := l.Acquire("RKN-E01-S01", time.Second)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			mu.Lock()
			concurrent++
			if concurrent > maxObserved {
				maxObserved = concurrent
			}
			mu.Unlock()
			time.Sleep(2 * time.Millisecond)
			mu.Lock()
			concurrent--
			mu.Unlock()
			_ = h.Release()
		}()
	}
	wg.Wait()
	if maxObserved != 1 {
		t.Errorf("max concurrent holders = %d, want 1", maxObserved)
	}
}

func TestLocker_RejectsInvalidID(t *testing.T) {
	t.Parallel()
	l := newLockerDir(t)
	_, err := l.Acquire("bad/id", 0)
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}
