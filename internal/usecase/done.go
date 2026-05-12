package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// DoneInput is the request payload for Done.
type DoneInput struct {
	// ID identifies the item to mark done.
	ID string
	// ExpectVersion is forwarded to Store.Put. -1 = no check.
	ExpectVersion int
	// Note is appended to the journal entry as the operator's
	// completion annotation. Empty leaves the entry note-less.
	Note string
	// Agent stamps the journal entry's actor field.
	Agent string
	// LeaseID is the lease the caller claims to hold. Phase 3+: when
	// the on-disk item carries a Claim, LeaseID is required and must
	// match — empty or wrong lease returns ErrLeaseMismatch so a
	// non-holder cannot bypass ownership.
	LeaseID string
	// LockTimeout caps the per-item Locker.Acquire wait. Zero means
	// "try once".
	LockTimeout time.Duration
	// Metadata is arbitrary structured handoff data attached to the
	// "completed" journal entry. The tool stores and surfaces it
	// without validating keys (spec §5).
	Metadata map[string]any
}

// DoneOutput is the response payload for Done.
type DoneOutput struct {
	// Item is the item with Status=done.
	Item domain.Item `json:"item"`
	// PrevVersion is the on-disk version before the transition.
	PrevVersion int `json:"prev_version"`
	// NewVersion is the on-disk version after the transition.
	NewVersion int `json:"new_version"`
}

// Done flips Status to done, sets CompletedAt to clock.Now(), bumps
// version, and appends a "completed" journal entry. ValidTransition
// is consulted; illegal source statuses (e.g. backlog → done) return
// ErrInvalidStatus.
//
// Phase 3 ownership rules (closing the post-review-finding-1 gap):
//
//   - When the on-disk item carries a Claim, the caller MUST supply a
//     matching LeaseID — empty or wrong lease returns
//     ErrLeaseMismatch. The CLI's --lease-id flag claims to be
//     "Required in Phase 3+" and this is where the requirement is
//     enforced.
//   - The Claim is ALWAYS cleared on a successful done transition
//     (regardless of whether the caller supplied a lease — a done
//     item has no holder to retain).
//   - When a Locker is wired, Done acquires the per-item lock for
//     the duration so a concurrent Heartbeat or Release can't race.
func Done(ctx context.Context, d Deps, in DoneInput) (DoneOutput, error) {
	if in.ID == "" {
		return DoneOutput{}, fmt.Errorf("done: id is required: %w", domain.ErrUsage)
	}

	if d.Locker != nil {
		handle, err := d.Locker.Acquire(in.ID, in.LockTimeout)
		if err != nil {
			return DoneOutput{}, fmt.Errorf("done %s: %w", in.ID, err)
		}
		defer func() { _ = handle.Release() }()
	}

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return DoneOutput{}, err
	}

	if item.Claim != nil {
		// Item is currently held — only the lease holder may close it.
		if in.LeaseID == "" || !domain.LeaseMatches(item.Claim, in.LeaseID) {
			return DoneOutput{}, fmt.Errorf("done %s: held by %s, lease required: %w",
				in.ID, item.Claim.Agent, domain.ErrLeaseMismatch)
		}
	}

	if tErr := domain.Transition(item.Status, domain.StatusDone); tErr != nil {
		return DoneOutput{}, tErr
	}

	prevVersion := item.Version
	now := d.Clock.Now()
	completed := now

	item.Status = domain.StatusDone
	item.CompletedAt = &completed
	item.UpdatedAt = now
	item.Version = prevVersion + 1
	// A done item has no holder. Always clear regardless of how the
	// caller supplied (or didn't supply) a lease — the transition
	// itself ends the lease's purpose.
	item.Claim = nil
	entry := domain.JournalEntry{
		Timestamp: now,
		Actor:     in.Agent,
		Event:     "completed",
	}
	if in.Note != "" {
		entry.Note = in.Note
	}
	if len(in.Metadata) > 0 {
		entry.Metadata = in.Metadata
	}
	item.Journal = append(item.Journal, entry)

	stored, err := d.Store.Put(ctx, item, in.ExpectVersion)
	if err != nil {
		return DoneOutput{}, fmt.Errorf("done %s: %w", in.ID, err)
	}

	var auditMD map[string]any
	if len(in.Metadata) > 0 {
		auditMD = make(map[string]any, len(in.Metadata))
		for k, v := range in.Metadata {
			auditMD[k] = v
		}
	}
	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "completed",
		ItemID:    in.ID,
		Actor:     in.Agent,
		LeaseID:   in.LeaseID,
		Metadata:  auditMD,
	})

	return DoneOutput{
		Item:        stored,
		PrevVersion: prevVersion,
		NewVersion:  item.Version,
	}, nil
}
