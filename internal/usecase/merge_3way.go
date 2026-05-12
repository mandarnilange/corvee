package usecase

import (
	"sort"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// MergeItem3Way performs the deterministic 3-way merge described in
// spec §6 (Layer 4 status/journal/tags, Layer 8 claim resolution).
//
// Rules:
//   - status: latest (max version) wins; ties favour ours.
//   - claim: lower lease_id wins (per §6 Layer 8). The losing
//     agent's ID is returned in claimLostAgent so the caller can
//     emit a claim_lost event. If both sides hold the same lease
//     (same agent re-syncing), no claim is lost.
//   - journal: union by (ts+actor+event); dedupe.
//   - tags: union; dedupe; stable order.
//   - description / acceptance_criteria: prefer the higher-version
//     side; if both diverged from base, surface for manual review
//     and pick ours as the active value (prose conflict reported via
//     proseConflict=true).
//   - other prose-ish fields (title, deliverable): prefer higher
//     version; ties favour ours.
//   - everything else: prefer higher version.
//
// Returns the merged item, the loser's agent ID (when a claim was
// resolved), and a prose-conflict flag.
func MergeItem3Way(base *domain.Item, ours, theirs domain.Item) (domain.Item, string, bool) {
	merged := pickByVersion(ours, theirs)

	// Status: latest version wins (already handled by pickByVersion);
	// re-apply explicitly so the case is documented.
	merged.Status = pickStatus(ours, theirs)

	// Claims: deterministic lower-lease-id rule.
	mergedClaim, claimLostAgent := mergeClaims(ours.Claim, theirs.Claim)
	merged.Claim = mergedClaim

	// Journal: union by (ts, actor, event) triple.
	merged.Journal = unionJournal(ours.Journal, theirs.Journal)

	// Tags: union, sorted stable.
	merged.Tags = unionStrings(ours.Tags, theirs.Tags)

	// Aliases: union (so move/rename history isn't lost).
	merged.Aliases = unionStrings(ours.Aliases, theirs.Aliases)

	// Dependencies/Blocks: union — divergent edits both add edges.
	merged.Dependencies = unionStrings(ours.Dependencies, theirs.Dependencies)
	merged.Blocks = unionStrings(ours.Blocks, theirs.Blocks)

	// Version: max + 1 (this is a new write).
	merged.Version = maxInt(ours.Version, theirs.Version) + 1

	// UpdatedAt: latest of the two.
	if theirs.UpdatedAt.After(ours.UpdatedAt) {
		merged.UpdatedAt = theirs.UpdatedAt
	} else {
		merged.UpdatedAt = ours.UpdatedAt
	}

	// CompletedAt: prefer the side that has it set; if both, pick the
	// earlier (the first to mark done wins). Then enforce the
	// invariant that completed_at is non-nil iff status==done — when
	// the status-merge picks a non-done state, the historical
	// completed_at must be cleared, otherwise the merged item is
	// internally inconsistent (e.g. status=ready with completed_at
	// set).
	merged.CompletedAt = mergeCompletedAt(ours.CompletedAt, theirs.CompletedAt)
	if merged.Status != domain.StatusDone {
		merged.CompletedAt = nil
	}

	// Prose conflict detection: description / acceptance_criteria
	// diverged from base on both sides.
	prose := proseConflict(base, ours, theirs)

	return merged, claimLostAgent, prose
}

// pickByVersion returns the higher-version side as the merge starting
// point. Ties favour ours so a re-sync against an unchanged peer is
// idempotent.
func pickByVersion(ours, theirs domain.Item) domain.Item {
	if theirs.Version > ours.Version {
		return theirs
	}
	return ours
}

// pickStatus implements the "latest version wins" rule for status.
// When ours and theirs hold the same version we prefer ours so the
// merge is stable for the side running it.
func pickStatus(ours, theirs domain.Item) domain.Status {
	if theirs.Version > ours.Version {
		return theirs.Status
	}
	return ours.Status
}

// mergeClaims resolves divergent claim objects per §6 Layer 8.
// Returns the surviving claim and the loser's agent ID (empty when
// no claim was lost — both sides nil, both sides identical, or only
// one side held a claim).
func mergeClaims(ours, theirs *domain.Claim) (*domain.Claim, string) {
	switch {
	case ours == nil && theirs == nil:
		return nil, ""
	case ours == nil:
		return theirs, ""
	case theirs == nil:
		return ours, ""
	}
	if ours.LeaseID == theirs.LeaseID {
		return ours, ""
	}
	if ours.LeaseID < theirs.LeaseID {
		return ours, theirs.Agent
	}
	return theirs, ours.Agent
}

// unionJournal merges journal entries by (ts, actor, event, note)
// quad, dedupes, and sorts by timestamp.
//
// The Note field is part of the dedup key on purpose — spec §6 calls
// for a (ts, actor, event) triple, but in practice two agents can
// emit a "progress" event at the same microsecond on different
// machines with different notes (different work happening in
// parallel), and dropping one would lose information. Including
// Note keeps both. The trade-off: an agent that retries the same
// journal entry idempotently produces a single dedup'd row, which
// is the desired behavior.
func unionJournal(a, b []domain.JournalEntry) []domain.JournalEntry {
	seen := map[string]struct{}{}
	out := make([]domain.JournalEntry, 0, len(a)+len(b))
	add := func(entries []domain.JournalEntry) {
		for _, e := range entries {
			key := e.Timestamp.UTC().Format("2006-01-02T15:04:05.000000Z") + "|" + e.Actor + "|" + e.Event + "|" + e.Note
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, e)
		}
	}
	add(a)
	add(b)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Timestamp.Equal(out[j].Timestamp) {
			return out[i].Actor < out[j].Actor
		}
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out
}

// unionStrings merges two string slices, dedupes, and sorts.
func unionStrings(a, b []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, s := range a {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, s := range b {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// mergeCompletedAt picks the earlier of two completion timestamps so
// the first agent to mark done wins. Returns nil when both sides are
// nil, or whichever side is set when only one is.
func mergeCompletedAt(a, b *time.Time) *time.Time {
	switch {
	case a == nil && b == nil:
		return nil
	case a == nil:
		t := *b
		return &t
	case b == nil:
		t := *a
		return &t
	}
	if a.Before(*b) {
		t := *a
		return &t
	}
	t := *b
	return &t
}

// proseConflict reports whether description or acceptance_criteria
// diverged from base on both sides — the only case in which we cannot
// pick a winner deterministically and must surface for human review.
func proseConflict(base *domain.Item, ours, theirs domain.Item) bool {
	if base == nil {
		// Both sides added the item; consider any difference a
		// conflict so reviewers see both versions.
		return ours.Description != theirs.Description ||
			!equalStrings(ours.AcceptanceCriteria, theirs.AcceptanceCriteria)
	}
	descOursChanged := ours.Description != base.Description
	descTheirsChanged := theirs.Description != base.Description
	descConflict := descOursChanged && descTheirsChanged && ours.Description != theirs.Description

	acOursChanged := !equalStrings(ours.AcceptanceCriteria, base.AcceptanceCriteria)
	acTheirsChanged := !equalStrings(theirs.AcceptanceCriteria, base.AcceptanceCriteria)
	acConflict := acOursChanged && acTheirsChanged && !equalStrings(ours.AcceptanceCriteria, theirs.AcceptanceCriteria)

	return descConflict || acConflict
}
