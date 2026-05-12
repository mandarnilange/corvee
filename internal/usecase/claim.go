package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ClaimInput is the request payload for Claim.
type ClaimInput struct {
	// ID is the item to claim.
	ID string
	// Agent is the claimant's agent ID (required).
	Agent string
	// AgentRole stamps the journal entry's role.
	AgentRole domain.Role
	// AgentCapabilities is the claimant's capability set; the item's
	// RequiredCapabilities must be a subset.
	AgentCapabilities []string
	// TTL is the lease lifetime. Zero falls back to the workspace
	// default (Workspace.ClaimTTLMinutes).
	TTL time.Duration
	// LockTimeout is how long the per-item Locker.Acquire may wait.
	LockTimeout time.Duration
	// OperationID is the optional idempotency key.
	OperationID string
}

// ClaimOutput is the response payload for Claim.
type ClaimOutput struct {
	// Item is the claimed item with Status=claimed and Claim populated.
	Item domain.Item `json:"item"`
	// LeaseID is the freshly-minted ULID; the executor must present it
	// for every claim-scoped operation.
	LeaseID string `json:"lease_id"`
}

// Claim acquires the per-item lock, verifies the item is claimable
// (status=ready or expired claim, capability match), mints a lease ID,
// stamps the Claim sub-record, transitions Status to claimed, and
// returns the lease.
//
// Concurrency: Locker.Acquire serializes within a process and across
// processes; the version-aware Put inside the lock is then a strict
// write to a still-current copy. The combination is the §6 Layer 4
// claim primitive.
func Claim(ctx context.Context, d Deps, in ClaimInput) (ClaimOutput, error) {
	if in.ID == "" {
		return ClaimOutput{}, fmt.Errorf("claim: id is required: %w", domain.ErrUsage)
	}
	if in.Agent == "" {
		return ClaimOutput{}, fmt.Errorf("claim: agent is required: %w", domain.ErrUsage)
	}
	if d.Locker == nil {
		return ClaimOutput{}, fmt.Errorf("claim: locker is nil: %w", domain.ErrUsage)
	}

	// Pre-lock fast path for op-id replay: if the result is already
	// committed we can skip the lock entirely.
	prior, replayed, replayErr := opReplay(in.OperationID, d)
	if replayErr != nil {
		return ClaimOutput{}, replayErr
	}
	if replayed {
		return prior, nil
	}

	handle, err := d.Locker.Acquire(in.ID, in.LockTimeout)
	if err != nil {
		return ClaimOutput{}, fmt.Errorf("claim %s: %w", in.ID, err)
	}
	defer func() { _ = handle.Release() }()

	// Closing post-review-finding-4: re-check OpResults *under the
	// lock*. Without this, two concurrent claims with the same op-id
	// can both miss the pre-lock check, queue at Acquire, and the
	// loser sees the winner's claim as ErrAlreadyClaimed instead of
	// replaying the original lease.
	prior, replayed, replayErr = opReplay(in.OperationID, d)
	if replayErr != nil {
		return ClaimOutput{}, replayErr
	}
	if replayed {
		return prior, nil
	}

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return ClaimOutput{}, err
	}
	now := d.Clock.Now()

	if item.Claim != nil && domain.IsLeaseLive(item.Claim, now) {
		return ClaimOutput{}, fmt.Errorf("claim %s: held by %s: %w",
			in.ID, item.Claim.Agent, domain.ErrAlreadyClaimed)
	}

	if !claimableStatus(item.Status) {
		return ClaimOutput{}, fmt.Errorf("claim %s: status=%s is not claimable: %w",
			in.ID, item.Status, domain.ErrInvalidStatus)
	}

	if !capabilitiesCover(item.RequiredCapabilities, in.AgentCapabilities) {
		return ClaimOutput{}, fmt.Errorf("claim %s: agent capabilities %v do not cover required %v: %w",
			in.ID, in.AgentCapabilities, item.RequiredCapabilities, domain.ErrUsage)
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

	leaseID := d.IDGen.ULID()
	claim := &domain.Claim{
		Agent:     in.Agent,
		LeaseID:   leaseID,
		ClaimedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	prevVersion := item.Version
	item.Claim = claim
	item.Status = domain.StatusClaimed
	item.UpdatedAt = now
	item.Version = prevVersion + 1
	item.Journal = append(item.Journal, domain.JournalEntry{
		Timestamp: now,
		Actor:     in.Agent,
		Event:     "claimed",
		Metadata:  map[string]any{"lease_id": leaseID, "ttl_seconds": ttl / time.Second},
	})

	stored, err := d.Store.Put(ctx, item, prevVersion)
	if err != nil {
		return ClaimOutput{}, fmt.Errorf("claim %s: persist: %w", in.ID, err)
	}

	auditAppend(d, domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   now,
		Type:        "claimed",
		ItemID:      in.ID,
		Actor:       in.Agent,
		ActorRole:   in.AgentRole,
		OperationID: in.OperationID,
		LeaseID:     leaseID,
	})

	result := ClaimOutput{Item: stored, LeaseID: leaseID}
	if err := opCommit(in.OperationID, d, result); err != nil {
		return ClaimOutput{}, err
	}
	return result, nil
}

// opReplay returns (prior, true, nil) when an OpResults entry for
// opID already exists. Returns (zero, false, nil) when no entry
// exists or replay is not configured. Returns an error only when the
// store reports a failure other than not-found.
func opReplay(opID string, d Deps) (ClaimOutput, bool, error) {
	if opID == "" || d.OpResults == nil {
		return ClaimOutput{}, false, nil
	}
	cached, ok, err := d.OpResults.Get(opID)
	if err != nil {
		return ClaimOutput{}, false, fmt.Errorf("claim: opresults get %s: %w", opID, err)
	}
	if !ok {
		return ClaimOutput{}, false, nil
	}
	var prior ClaimOutput
	if jErr := json.Unmarshal([]byte(cached), &prior); jErr == nil {
		return prior, true, nil
	}
	// Corrupt cache entry — treat as cache miss rather than fail the
	// operation; the next successful Put will overwrite it. (oplog
	// uses O_EXCL so a duplicate key is normally a misuse signal, but
	// a torn read is still possible if a previous process crashed
	// mid-write — see TTR-E04 recovery.)
	return ClaimOutput{}, false, nil
}

// opCommit persists result under opID. A duplicate-op-id collision
// from oplog (which uses O_EXCL since post-review-finding-4) is
// treated as a benign race — the existing record is authoritative
// and our identical-result write would be redundant.
func opCommit(opID string, d Deps, result ClaimOutput) error {
	if opID == "" || d.OpResults == nil {
		return nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("claim: marshal op result: %w", err)
	}
	if err := d.OpResults.Put(opID, string(data)); err != nil {
		if errors.Is(err, domain.ErrOpAlreadyRecorded) {
			// Another caller already committed under the same op-id.
			// Post-lock re-check normally prevents this branch; falling
			// through is correct — the user still gets their lease, and
			// the cached entry stays the source of truth for replays.
			return nil
		}
		return fmt.Errorf("claim: opresults put %s: %w", opID, err)
	}
	return nil
}

// claimableStatus is the subset of Status values that may be claimed.
// We accept ready (canonical) and the expired-lease cases (claimed,
// in_progress) so a reaped item gets re-claimed without an explicit
// reset to ready.
func claimableStatus(s domain.Status) bool {
	switch s {
	case domain.StatusReady, domain.StatusClaimed, domain.StatusInProgress:
		return true
	default:
		return false
	}
}

// capabilitiesCover reports whether `cover` is a superset of
// `required`. Empty required is always satisfied.
func capabilitiesCover(required, cover []string) bool {
	if len(required) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(cover))
	for _, c := range cover {
		set[c] = struct{}{}
	}
	for _, r := range required {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}
