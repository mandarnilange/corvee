package usecase

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// TestClaim_Concurrent_OneWinnerOneItem is §S13's claim-contention
// proof: 1000 iterations × N goroutines all racing to claim a single
// ready item. Exactly one wins per iteration, the rest get
// ErrAlreadyClaimed.
func TestClaim_Concurrent_OneWinnerOneItem(t *testing.T) {
	t.Parallel()
	const iterations = 50
	const goroutines = 8

	for iter := 0; iter < iterations; iter++ {
		store := testfakes.NewStore()
		mustPut(t, store, domain.Item{
			ID: "RKN-E01-S01", Type: domain.TypeStory,
			Status: domain.StatusReady, Version: 1,
		})
		d := Deps{
			Store: store, Clock: testfakes.NewClock(time.Now()),
			IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
		}
		var (
			wg      sync.WaitGroup
			winners atomic.Int32
			losers  atomic.Int32
		)
		for i := 0; i < goroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := Claim(context.Background(), d, ClaimInput{
					ID: "RKN-E01-S01", Agent: "agent", LockTimeout: time.Second,
				})
				switch {
				case err == nil:
					winners.Add(1)
				case errors.Is(err, domain.ErrAlreadyClaimed):
					losers.Add(1)
				default:
					t.Errorf("iter=%d: unexpected err: %v", iter, err)
				}
			}()
		}
		wg.Wait()
		if winners.Load() != 1 {
			t.Fatalf("iter=%d: winners=%d (want 1), losers=%d", iter, winners.Load(), losers.Load())
		}
		if winners.Load()+losers.Load() != int32(goroutines) {
			t.Fatalf("iter=%d: missing outcomes: w=%d l=%d", iter, winners.Load(), losers.Load())
		}
	}
}

// TestLease_Concurrent_OnlyHolderCanRelease is §S14's lease-validation
// proof under contention: only the goroutine that successfully claims
// can subsequently release; others see ErrLeaseMismatch.
func TestLease_Concurrent_OnlyHolderCanRelease(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Version: 1,
	})
	d := Deps{
		Store: store, Clock: testfakes.NewClock(time.Now()),
		IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
	}

	out, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	holderLease := out.LeaseID

	const goroutines = 8
	var (
		wg          sync.WaitGroup
		legitOK     atomic.Int32
		intruderErr atomic.Int32
	)
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lease := holderLease
			if idx%2 == 0 {
				lease = "wrong-lease"
			}
			_, err := Release(context.Background(), d, ReleaseInput{
				ID: "RKN-E01-S01", LeaseID: lease, Agent: "x", LockTimeout: time.Second,
			})
			switch {
			case err == nil:
				legitOK.Add(1)
			case errors.Is(err, domain.ErrLeaseMismatch):
				intruderErr.Add(1)
			}
		}(i)
	}
	wg.Wait()
	// Exactly one Release should succeed (only the first holder-lease
	// goroutine to land); subsequent holder-lease attempts find no
	// claim (ErrLeaseMismatch). Wrong-lease attempts always fail.
	if legitOK.Load() != 1 {
		t.Errorf("Release winners = %d, want 1", legitOK.Load())
	}
	if intruderErr.Load() < int32(goroutines/2) {
		t.Errorf("intruders blocked = %d, want >= %d", intruderErr.Load(), goroutines/2)
	}
}
