package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// HeartbeatInput is the request payload for Heartbeat.
type HeartbeatInput struct {
	// ID is the item to heartbeat.
	ID string
	// LeaseID is the claimant's lease (required).
	LeaseID string
	// Agent stamps the journal entry's actor.
	Agent string
	// TTL extends the lease by this duration. Zero falls back to the
	// workspace default (Workspace.HeartbeatIntervalMinutes * 2 as a
	// safe extension).
	TTL time.Duration
	// LockTimeout caps the per-item lock acquisition.
	LockTimeout time.Duration
	// AdvanceToInProgress, when true, also bumps Status from claimed
	// to in_progress on the first heartbeat.
	AdvanceToInProgress bool
}

// HeartbeatOutput is the response payload for Heartbeat.
type HeartbeatOutput struct {
	// Item is the item with the lease's ExpiresAt extended.
	Item domain.Item `json:"item"`
	// ExpiresAt is the new lease expiry.
	ExpiresAt time.Time `json:"expires_at"`
}

// Heartbeat extends the lease's ExpiresAt for an item the caller
// currently holds. Lease ID must match.
func Heartbeat(ctx context.Context, d Deps, in HeartbeatInput) (HeartbeatOutput, error) {
	if in.ID == "" || in.LeaseID == "" {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat: id and lease_id are required: %w", domain.ErrUsage)
	}
	if d.Locker == nil {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat: locker is nil: %w", domain.ErrUsage)
	}
	handle, err := d.Locker.Acquire(in.ID, in.LockTimeout)
	if err != nil {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat %s: %w", in.ID, err)
	}
	defer func() { _ = handle.Release() }()

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return HeartbeatOutput{}, err
	}
	if item.Claim == nil {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat %s: not claimed: %w", in.ID, domain.ErrLeaseMismatch)
	}
	if !domain.LeaseMatches(item.Claim, in.LeaseID) {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat %s: lease %q does not match: %w",
			in.ID, in.LeaseID, domain.ErrLeaseMismatch)
	}

	ttl := in.TTL
	if ttl == 0 {
		if d.Workspace != nil {
			if ws, wsErr := d.Workspace.Load(); wsErr == nil && ws.ClaimTTLMinutes > 0 {
				ttl = time.Duration(ws.ClaimTTLMinutes) * time.Minute
			}
		}
	}
	if ttl == 0 {
		ttl = time.Hour
	}

	now := d.Clock.Now()
	prev := item.Version
	item.Claim.ExpiresAt = now.Add(ttl)
	if in.AdvanceToInProgress && item.Status == domain.StatusClaimed {
		if tErr := domain.Transition(item.Status, domain.StatusInProgress); tErr == nil {
			item.Status = domain.StatusInProgress
		}
	}
	item.UpdatedAt = now
	item.Version = prev + 1
	item.Journal = append(item.Journal, domain.JournalEntry{
		Timestamp: now, Actor: in.Agent, Event: "heartbeat",
		Metadata: map[string]any{"lease_id": in.LeaseID, "expires_at": item.Claim.ExpiresAt},
	})

	stored, err := d.Store.Put(ctx, item, prev)
	if err != nil {
		return HeartbeatOutput{}, fmt.Errorf("heartbeat %s: persist: %w", in.ID, err)
	}
	auditAppend(d, domain.Event{
		EventID: nextEventID(d), Timestamp: now, Type: "heartbeat",
		ItemID: in.ID, Actor: in.Agent, LeaseID: in.LeaseID,
	})
	return HeartbeatOutput{Item: stored, ExpiresAt: item.Claim.ExpiresAt}, nil
}
