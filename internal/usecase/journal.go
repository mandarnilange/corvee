package usecase

import (
	"context"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// allowedJournalEvents is the set of acceptable Event values for the
// Journal verb (spec §15.2). The default ("note") is included so
// callers may pass an empty Event value and have it normalised.
var allowedJournalEvents = map[string]struct{}{
	"note":     {},
	"progress": {},
	"blocker":  {},
	"decision": {},
	"handoff":  {},
}

// leaseRequiredJournalEvents lists the event subtypes that imply the
// caller is the active worker on the item. When the on-disk item is
// claimed and the requested event is in this set, a matching LeaseID
// is mandatory — observers may still leave "note" or "handoff" entries
// without holding the lease.
var leaseRequiredJournalEvents = map[string]struct{}{
	"progress": {},
	"blocker":  {},
	"decision": {},
}

// JournalInput is the request payload for Journal. The CLI is
// responsible for parsing --metadata / --metadata-file into the
// Metadata map; the usecase consumes a parsed map only.
type JournalInput struct {
	// ID identifies the item to journal against.
	ID string
	// Event is the journal-entry subtype: note (default), progress,
	// blocker, decision, handoff. Empty normalises to "note".
	Event string
	// Note is the human-readable annotation. Optional.
	Note string
	// Metadata is arbitrary structured handoff data. Stored verbatim.
	Metadata map[string]any
	// Agent stamps the journal entry's actor field.
	Agent string
	// LeaseID is required when the item is claimed and Event is one of
	// progress, blocker, decision. Empty for unclaimed items or for
	// note/handoff entries.
	LeaseID string
	// LockTimeout caps the per-item Locker.Acquire wait. Zero means
	// "try once".
	LockTimeout time.Duration
}

// JournalOutput is the response payload for Journal.
type JournalOutput struct {
	// Item is the item with the new journal entry appended.
	Item domain.Item `json:"item"`
	// PrevVersion is the on-disk version before the append.
	PrevVersion int `json:"prev_version"`
	// NewVersion is the on-disk version after the append.
	NewVersion int `json:"new_version"`
}

// Journal appends one entry to an item's per-item Journal slice and
// emits a corresponding "journal" audit event. The Status of the item
// is left unchanged — Journal is purely additive metadata.
//
// Lease enforcement: when the item carries a Claim and the requested
// Event is one of progress/blocker/decision, LeaseID must match the
// holder's lease. note and handoff entries are observer-friendly and
// require no lease even on a claimed item.
func Journal(ctx context.Context, d Deps, in JournalInput) (JournalOutput, error) {
	if in.ID == "" {
		return JournalOutput{}, fmt.Errorf("journal: id is required: %w", domain.ErrUsage)
	}
	if in.Agent == "" {
		return JournalOutput{}, fmt.Errorf("journal: agent is required: %w", domain.ErrUsage)
	}

	event := in.Event
	if event == "" {
		event = "note"
	}
	if _, ok := allowedJournalEvents[event]; !ok {
		return JournalOutput{}, fmt.Errorf(
			"journal: event %q must be one of note,progress,blocker,decision,handoff: %w",
			event, domain.ErrUsage)
	}

	if d.Locker != nil {
		handle, err := d.Locker.Acquire(in.ID, in.LockTimeout)
		if err != nil {
			return JournalOutput{}, fmt.Errorf("journal %s: %w", in.ID, err)
		}
		defer func() { _ = handle.Release() }()
	}

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return JournalOutput{}, err
	}

	if item.Claim != nil {
		if _, requires := leaseRequiredJournalEvents[event]; requires {
			if in.LeaseID == "" || !domain.LeaseMatches(item.Claim, in.LeaseID) {
				return JournalOutput{}, fmt.Errorf(
					"journal %s: event %q on claimed item requires matching lease: %w",
					in.ID, event, domain.ErrLeaseMismatch)
			}
		}
	}

	now := d.Clock.Now()
	prev := item.Version
	entry := domain.JournalEntry{
		Timestamp: now,
		Actor:     in.Agent,
		Event:     event,
	}
	if in.Note != "" {
		entry.Note = in.Note
	}
	if len(in.Metadata) > 0 {
		entry.Metadata = in.Metadata
	}
	item.Journal = append(item.Journal, entry)
	item.UpdatedAt = now
	item.Version = prev + 1

	stored, err := d.Store.Put(ctx, item, prev)
	if err != nil {
		return JournalOutput{}, fmt.Errorf("journal %s: %w", in.ID, err)
	}

	auditMD := map[string]any{"event": event}
	for k, v := range in.Metadata {
		auditMD[k] = v
	}
	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "journal",
		ItemID:    in.ID,
		Actor:     in.Agent,
		LeaseID:   in.LeaseID,
		Metadata:  auditMD,
	})

	return JournalOutput{Item: stored, PrevVersion: prev, NewVersion: item.Version}, nil
}
