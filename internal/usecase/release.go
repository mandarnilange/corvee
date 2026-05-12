package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ReleaseInput is the request payload for Release.
type ReleaseInput struct {
	// ID is the item to release.
	ID string
	// LeaseID is the claimant's lease (required). Mismatch returns
	// ErrLeaseMismatch.
	LeaseID string
	// Agent stamps the journal entry's actor.
	Agent string
	// LockTimeout caps the per-item lock acquisition.
	LockTimeout time.Duration
	// ResetToReady, when true, transitions the item back to ready so a
	// new executor can claim it. When false the item drops to
	// blocked (the executor abandoned the work and wants a planner to
	// triage it).
	ResetToReady bool
}

// ReleaseOutput is the response payload for Release.
type ReleaseOutput struct {
	// Item is the item with the claim cleared.
	Item domain.Item `json:"item"`
}

// Release clears the claim on an item the caller currently holds.
// Lease ID must match. The status drops back to ready (when
// ResetToReady) or blocked otherwise.
func Release(ctx context.Context, d Deps, in ReleaseInput) (ReleaseOutput, error) {
	if in.ID == "" || in.LeaseID == "" {
		return ReleaseOutput{}, fmt.Errorf("release: id and lease_id are required: %w", domain.ErrUsage)
	}
	if d.Locker == nil {
		return ReleaseOutput{}, fmt.Errorf("release: locker is nil: %w", domain.ErrUsage)
	}
	handle, err := d.Locker.Acquire(in.ID, in.LockTimeout)
	if err != nil {
		return ReleaseOutput{}, fmt.Errorf("release %s: %w", in.ID, err)
	}
	defer func() { _ = handle.Release() }()

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return ReleaseOutput{}, err
	}
	if item.Claim == nil {
		return ReleaseOutput{}, fmt.Errorf("release %s: not claimed: %w", in.ID, domain.ErrLeaseMismatch)
	}
	if !domain.LeaseMatches(item.Claim, in.LeaseID) {
		return ReleaseOutput{}, fmt.Errorf("release %s: lease %q does not match: %w",
			in.ID, in.LeaseID, domain.ErrLeaseMismatch)
	}

	now := d.Clock.Now()
	prev := item.Version
	item.Claim = nil
	if in.ResetToReady {
		item.Status = domain.StatusReady
	} else {
		item.Status = domain.StatusBlocked
	}
	item.UpdatedAt = now
	item.Version = prev + 1
	item.Journal = append(item.Journal, domain.JournalEntry{
		Timestamp: now, Actor: in.Agent, Event: "released",
	})

	stored, err := d.Store.Put(ctx, item, prev)
	if err != nil {
		return ReleaseOutput{}, fmt.Errorf("release %s: persist: %w", in.ID, err)
	}
	auditAppend(d, domain.Event{
		EventID: nextEventID(d), Timestamp: now, Type: "released",
		ItemID: in.ID, Actor: in.Agent, LeaseID: in.LeaseID,
	})
	return ReleaseOutput{Item: stored}, nil
}
