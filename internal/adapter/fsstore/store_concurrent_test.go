package fsstore

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// TestStore_Concurrent_VersionedPutNeverLosesAnUpdate is the §S12
// race-aware property test: under N goroutines wrapping Get→bump→Put
// in the Phase 3 Locker, the final version equals the count of
// successful writes — no lost updates, no torn files. Per spec §6
// Layer 2+4, optimistic version checks alone are racy; the per-item
// Locker is the cross-process serialization primitive callers must
// hold around any mutation.
func TestStore_Concurrent_VersionedPutNeverLosesAnUpdate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	lockDir := t.TempDir()
	locker, err := NewLocker(lockDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	seed := domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}
	if _, putErr := s.Put(context.Background(), seed, -1); putErr != nil {
		t.Fatal(putErr)
	}

	const goroutines = 8
	const targetWrites = 100

	var (
		wg        sync.WaitGroup
		successes atomic.Int64
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for successes.Load() < targetWrites {
				h, lockErr := locker.Acquire(seed.ID, time.Second)
				if lockErr != nil {
					return
				}
				cur, getErr := s.Get(context.Background(), seed.ID)
				if getErr != nil {
					_ = h.Release()
					return
				}
				prev := cur.Version
				cur.Version++
				cur.UpdatedAt = time.Now().UTC()
				if _, putErr := s.Put(context.Background(), cur, prev); putErr == nil {
					successes.Add(1)
				}
				_ = h.Release()
			}
		}()
	}
	wg.Wait()

	final, err := s.Get(context.Background(), seed.ID)
	if err != nil {
		t.Fatal(err)
	}
	count := successes.Load()
	expected := int64(1) + count
	if int64(final.Version) != expected {
		t.Errorf("version drift: final=%d want=%d (successes=%d)", final.Version, expected, count)
	}
}
