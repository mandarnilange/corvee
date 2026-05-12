package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// UpdateInput is the request payload for Update. Pointer fields are
// "set if non-nil"; slice fields are explicit add/remove sets so the
// caller can express idempotent set semantics. ExpectVersion threads
// through to Store.Put — pass -1 for no expectation.
type UpdateInput struct {
	// ID identifies the item to mutate.
	ID string
	// ExpectVersion is forwarded to Store.Put. -1 = no check.
	ExpectVersion int

	// Status, when non-nil, transitions the item; ValidTransition
	// rejects illegal edges with ErrInvalidStatus.
	Status *domain.Status
	// Priority, when non-nil, replaces the priority.
	Priority *domain.Priority
	// Title, when non-nil, replaces the title.
	Title *string
	// Description, when non-nil, replaces the description.
	Description *string
	// DueDate, when non-nil, sets the due date. Phase 1 has no
	// "clear due date" affordance; revisit if needed.
	DueDate *time.Time
	// Risk, when non-nil, sets Impact.Risk.
	Risk *domain.Risk

	// AddTags inserts each tag (dedup on existing set).
	AddTags []string
	// RemoveTags drops each tag if present.
	RemoveTags []string
	// AddImpactFiles inserts each path into Impact.Files.
	AddImpactFiles []string
	// RemoveImpactFiles drops each path from Impact.Files.
	RemoveImpactFiles []string
	// AddDeps inserts each id into Dependencies.
	AddDeps []string
	// RemoveDeps drops each id from Dependencies.
	RemoveDeps []string
	// AddAcceptance appends each criterion (dedup).
	AddAcceptance []string
	// RemoveAcceptance drops each matching criterion.
	RemoveAcceptance []string

	// Note, when non-empty, appends an "updated" journal entry.
	Note string
	// Agent stamps the journal entry.
	Agent string
}

// UpdateOutput is the response payload for Update.
type UpdateOutput struct {
	// Item is the persisted post-mutation item.
	Item domain.Item `json:"item"`
	// PrevVersion is the on-disk version before the mutation.
	PrevVersion int `json:"prev_version"`
	// NewVersion is the on-disk version after the mutation.
	NewVersion int `json:"new_version"`
}

// Update mutates the named item per the input fields and persists with
// the supplied ExpectVersion. ValidTransition is consulted on status
// changes; conflict on Put returns ErrVersionConflict (no retry —
// caller decides). Time and version are stamped by the usecase before
// Put — fsstore never mutates them.
//
// Phase 3 concurrency rule (closing post-review-finding-5): the
// per-item Locker is held around the read-modify-write window. The
// CLI default --expect-version=-1 alone is not enough to prevent a
// stale snapshot from blind-overwriting a concurrent Claim; the lock
// makes the read happen on a coherent post-claim state.
func Update(ctx context.Context, d Deps, in UpdateInput) (UpdateOutput, error) {
	if in.ID == "" {
		return UpdateOutput{}, fmt.Errorf("update: id is required: %w", domain.ErrUsage)
	}
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(in.ID, time.Second)
		if lockErr != nil {
			return UpdateOutput{}, fmt.Errorf("update %s: %w", in.ID, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}
	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return UpdateOutput{}, err
	}
	prevVersion := item.Version

	if in.Status != nil && *in.Status != item.Status {
		if tErr := domain.Transition(item.Status, *in.Status); tErr != nil {
			return UpdateOutput{}, tErr
		}
		item.Status = *in.Status
	}
	if in.Priority != nil {
		item.Priority = *in.Priority
	}
	if in.Title != nil {
		item.Title = *in.Title
	}
	if in.Description != nil {
		item.Description = *in.Description
	}
	if in.DueDate != nil {
		due := *in.DueDate
		item.DueDate = &due
	}
	if in.Risk != nil {
		item.Impact.Risk = *in.Risk
	}

	item.Tags = setAdd(item.Tags, in.AddTags)
	item.Tags = setRemove(item.Tags, in.RemoveTags)
	item.Impact.Files = setAdd(item.Impact.Files, in.AddImpactFiles)
	item.Impact.Files = setRemove(item.Impact.Files, in.RemoveImpactFiles)
	item.Dependencies = setAdd(item.Dependencies, in.AddDeps)
	item.Dependencies = setRemove(item.Dependencies, in.RemoveDeps)
	item.AcceptanceCriteria = setAdd(item.AcceptanceCriteria, in.AddAcceptance)
	item.AcceptanceCriteria = setRemove(item.AcceptanceCriteria, in.RemoveAcceptance)

	now := d.Clock.Now()
	if in.Note != "" {
		item.Journal = append(item.Journal, domain.JournalEntry{
			Timestamp: now,
			Actor:     in.Agent,
			Event:     "updated",
			Note:      in.Note,
		})
	}

	item.UpdatedAt = now
	item.Version = prevVersion + 1

	stored, err := d.Store.Put(ctx, item, in.ExpectVersion)
	if err != nil {
		return UpdateOutput{}, fmt.Errorf("update %s: %w", in.ID, err)
	}

	meta := map[string]any{}
	if in.Status != nil {
		meta["new_status"] = string(*in.Status)
	}
	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "item_updated",
		ItemID:    stored.ID,
		Actor:     in.Agent,
		Metadata:  meta,
	})

	return UpdateOutput{
		Item:        stored,
		PrevVersion: prevVersion,
		NewVersion:  item.Version,
	}, nil
}

// setAdd inserts each addition into existing, preserving order and
// deduplicating by string equality. The original slice is not
// mutated.
func setAdd(existing, additions []string) []string {
	if len(additions) == 0 {
		return existing
	}
	seen := make(map[string]struct{}, len(existing))
	for _, s := range existing {
		seen[s] = struct{}{}
	}
	out := make([]string, len(existing), len(existing)+len(additions))
	copy(out, existing)
	for _, a := range additions {
		if _, ok := seen[a]; !ok {
			out = append(out, a)
			seen[a] = struct{}{}
		}
	}
	return out
}

// setRemove drops every removal from existing, preserving order. A
// removal that's not present is silently ignored — set semantics.
func setRemove(existing, removals []string) []string {
	if len(removals) == 0 {
		return existing
	}
	rm := make(map[string]struct{}, len(removals))
	for _, r := range removals {
		rm[r] = struct{}{}
	}
	out := make([]string, 0, len(existing))
	for _, e := range existing {
		if _, drop := rm[e]; !drop {
			out = append(out, e)
		}
	}
	return out
}
