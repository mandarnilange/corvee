package idgen

import (
	"sync"
	"testing"
)

func TestULIDGen_FormatIs26CharCrockfordBase32(t *testing.T) {
	t.Parallel()
	id := New().ULID()
	if len(id) != 26 {
		t.Fatalf("ULID len = %d, want 26", len(id))
	}
	for i, c := range id {
		ok := false
		for _, k := range crockford {
			if c == k {
				ok = true
				break
			}
		}
		if !ok {
			t.Errorf("char %d (%q) not in Crockford alphabet", i, string(c))
		}
	}
}

func TestULIDGen_Monotonic_SequentialCallsAreSortable(t *testing.T) {
	t.Parallel()
	g := New()
	prev := g.ULID()
	for i := 0; i < 1000; i++ {
		next := g.ULID()
		if next <= prev {
			t.Fatalf("ULIDs not monotonic at i=%d: prev=%s next=%s", i, prev, next)
		}
		prev = next
	}
}

func TestULIDGen_Concurrent_ProducesUniqueValues(t *testing.T) {
	t.Parallel()
	g := New()
	const goroutines = 16
	const perG = 200

	var (
		mu  sync.Mutex
		all = make(map[string]struct{}, goroutines*perG)
		wg  sync.WaitGroup
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				id := g.ULID()
				mu.Lock()
				all[id] = struct{}{}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(all) != goroutines*perG {
		t.Errorf("collisions: got %d unique, want %d", len(all), goroutines*perG)
	}
}
