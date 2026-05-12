package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ReapInput is the request payload for Reap.
type ReapInput struct{}

// ReapedClaim describes one item whose lease was released by Reap.
type ReapedClaim struct {
	// ItemID is the canonical ID of the reaped item.
	ItemID string `json:"item_id"`
	// FormerAgent is the agent that previously held the lease.
	FormerAgent string `json:"former_agent"`
	// FormerLeaseID is the lease that was cleared.
	FormerLeaseID string `json:"former_lease_id"`
}

// ReapOutput is the response payload for Reap.
type ReapOutput struct {
	// Reaped is the list of items whose claims were dropped this run.
	Reaped []ReapedClaim `json:"reaped"`
}

// Reap walks every item, clearing claims whose lease has expired
// (with the spec's skew tolerance applied). Reaped items revert to
// status=ready so a fresh executor can pick them up.
func Reap(ctx context.Context, d Deps, _ ReapInput) (ReapOutput, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return ReapOutput{}, fmt.Errorf("reap: list: %w", err)
	}
	now := d.Clock.Now()
	out := ReapOutput{}
	for _, it := range items {
		if it.Claim == nil {
			continue
		}
		if domain.IsLeaseLive(it.Claim, now) {
			continue
		}
		formerAgent := it.Claim.Agent
		formerLease := it.Claim.LeaseID

		var handle domain.LockHandle
		if d.Locker != nil {
			handle, err = d.Locker.Acquire(it.ID, 0)
			if err != nil {
				continue // contention; another reaper has it
			}
		}
		// re-load post-lock to avoid racing the holder.
		fresh, err := d.Store.Get(ctx, it.ID)
		if err != nil {
			if handle != nil {
				_ = handle.Release()
			}
			continue
		}
		if fresh.Claim == nil || domain.IsLeaseLive(fresh.Claim, now) {
			if handle != nil {
				_ = handle.Release()
			}
			continue
		}
		prev := fresh.Version
		fresh.Claim = nil
		fresh.Status = domain.StatusReady
		fresh.UpdatedAt = now
		fresh.Version = prev + 1
		fresh.Journal = append(fresh.Journal, domain.JournalEntry{
			Timestamp: now, Actor: "reaper", Event: "reaped",
			Metadata: map[string]any{"former_agent": formerAgent, "former_lease_id": formerLease},
		})
		if _, err := d.Store.Put(ctx, fresh, prev); err != nil {
			if handle != nil {
				_ = handle.Release()
			}
			continue
		}
		if handle != nil {
			_ = handle.Release()
		}
		auditAppend(d, domain.Event{
			EventID: nextEventID(d), Timestamp: now, Type: "reaped",
			ItemID: it.ID, LeaseID: formerLease,
			Metadata: map[string]any{"former_agent": formerAgent},
		})
		out.Reaped = append(out.Reaped, ReapedClaim{
			ItemID: it.ID, FormerAgent: formerAgent, FormerLeaseID: formerLease,
		})
	}
	return out, nil
}
