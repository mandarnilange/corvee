package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// CloneInput is the request payload for Clone.
type CloneInput struct {
	// SourceID is the item to duplicate.
	SourceID string
	// ParentID is where the clone is attached. Must satisfy the
	// usual parent-type rule for the source's Type. Empty is rejected
	// for non-project sources.
	ParentID string
	// TitleOverride, when non-empty, replaces the cloned Title.
	TitleOverride string
	// Agent stamps CreatedBy on the clone.
	Agent string
	// AgentRole stamps CreatedByRole on the clone.
	AgentRole domain.Role
	// WithChildren, when true, recursively clones all descendants via
	// OpJournal for crash-safety. Phase 4+.
	WithChildren bool
}

// CloneOutput is the response payload for Clone.
type CloneOutput struct {
	// Item is the newly persisted root clone.
	Item domain.Item `json:"item"`
	// Descendants contains the cloned descendant items when WithChildren
	// is true. Empty for shallow clones.
	Descendants []domain.Item `json:"descendants,omitempty"`
}

// Clone duplicates an item under ParentID with a new ID assigned by
// the standard sibling-numbering rule. The clone is reset: Status =
// backlog, Claim = nil, Journal = nil, Version = 1, CreatedAt = now.
// With WithChildren=true, all descendants are recursively cloned under
// the new root via OpJournal for crash-safety.
//
// Projects cannot be cloned (their ID *is* the project code, and
// duplicating it would violate the unique-code invariant). Returns
// ErrUsage in that case.
func Clone(ctx context.Context, d Deps, in CloneInput) (CloneOutput, error) {
	if in.SourceID == "" {
		return CloneOutput{}, fmt.Errorf("clone: source_id is required: %w", domain.ErrUsage)
	}
	if in.ParentID == "" {
		return CloneOutput{}, fmt.Errorf("clone: parent_id is required: %w", domain.ErrUsage)
	}

	// Closing post-review-finding-2: lock the parent for the
	// "list siblings → compute next ID → Put" window so two concurrent
	// clones can't allocate the same child ID and silently overwrite.
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(in.ParentID, time.Second)
		if lockErr != nil {
			return CloneOutput{}, fmt.Errorf("clone: acquire %q: %w", in.ParentID, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}

	src, err := d.Store.Get(ctx, in.SourceID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return CloneOutput{}, fmt.Errorf("clone: source %q: %w", in.SourceID, domain.ErrNotFound)
		}
		return CloneOutput{}, fmt.Errorf("clone: load source: %w", err)
	}
	if src.Type == domain.TypeProject {
		return CloneOutput{}, fmt.Errorf("clone: cannot clone a project: %w", domain.ErrUsage)
	}

	parent, err := d.Store.Get(ctx, in.ParentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return CloneOutput{}, fmt.Errorf("clone: parent %q: %w", in.ParentID, domain.ErrNotFound)
		}
		return CloneOutput{}, fmt.Errorf("clone: load parent: %w", err)
	}
	expected := expectedParentType(src.Type)
	if parent.Type != expected {
		return CloneOutput{}, fmt.Errorf("clone: %s requires parent of type %q, got %q: %w",
			src.Type, expected, parent.Type, domain.ErrUsage)
	}

	siblings, err := d.Store.List(ctx, domain.ListFilter{ParentID: in.ParentID})
	if err != nil {
		return CloneOutput{}, fmt.Errorf("clone: list siblings: %w", err)
	}
	n, err := nextSiblingNum(siblings, parent.Type)
	if err != nil {
		return CloneOutput{}, err
	}
	id, err := domain.ChildID(in.ParentID, n)
	if err != nil {
		return CloneOutput{}, err
	}

	now := d.Clock.Now()
	clone := src
	clone.ID = id
	clone.ParentID = in.ParentID
	clone.ProjectID = parent.ProjectID
	if clone.ProjectID == "" {
		clone.ProjectID = parent.ID
	}
	clone.Status = domain.StatusBacklog
	clone.Claim = nil
	clone.Journal = nil
	clone.Aliases = nil
	clone.CompletedAt = nil
	clone.CreatedAt = now
	clone.UpdatedAt = now
	clone.CreatedBy = in.Agent
	clone.CreatedByRole = in.AgentRole
	clone.Version = 1
	if in.TitleOverride != "" {
		clone.Title = in.TitleOverride
	}

	stored, err := d.Store.Put(ctx, clone, -1)
	if err != nil {
		return CloneOutput{}, fmt.Errorf("clone: put %s: %w", id, err)
	}

	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "cloned",
		ItemID:    stored.ID,
		Actor:     in.Agent,
		ActorRole: in.AgentRole,
		Metadata:  map[string]any{"source_id": in.SourceID},
	})

	// Re-open ancestors that were `done`. The cloned item is in
	// `backlog`, so the parent (and grandparents, …) must reflect
	// that there's open work again. See reopenAncestorsIfDone.
	// Best-effort: log via slog.Warn (matches Add) — the clone
	// itself is already persisted, so we don't unwind on failure.
	if rerr := reopenAncestorsIfDone(ctx, d, in.ParentID, in.Agent, in.AgentRole, now); rerr != nil {
		slog.Warn("clone: failed to auto-reopen ancestors",
			slog.String("source_id", in.SourceID),
			slog.String("parent_id", in.ParentID),
			slog.String("error", rerr.Error()))
	}

	if !in.WithChildren {
		return CloneOutput{Item: stored}, nil
	}

	// Deep clone: collect and clone descendants. Pass the resolved
	// project ID so descendants inherit the cloned root's project,
	// not its parent (only equal when cloning an epic under a project).
	descendants, err := cloneDescendants(ctx, d, in, src, id, stored.ProjectID, now)
	if err != nil {
		return CloneOutput{}, fmt.Errorf("clone --with-children: %w", err)
	}

	return CloneOutput{Item: stored, Descendants: descendants}, nil
}

// cloneDescendants recursively clones all descendants of src under newRootID.
// Each descendant gets a new ID derived from the new root (prefix-swap),
// is reset (status=backlog, claim=nil, journal=nil, version=1), and is
// written atomically via OpJournal.
func cloneDescendants(
	ctx context.Context,
	d Deps,
	in CloneInput,
	src domain.Item,
	newRootID string,
	newRootProjectID string,
	now time.Time,
) ([]domain.Item, error) {
	all, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return nil, fmt.Errorf("list workspace: %w", err)
	}
	// Collect original descendants (everything under src.ID, not the new root).
	descendants := collectSubtree(src.ID, all)
	// Remove the root itself (it's already been created).
	filtered := make([]domain.Item, 0, len(descendants)-1)
	for _, it := range descendants {
		if it.ID != src.ID {
			filtered = append(filtered, it)
		}
	}
	if len(filtered) == 0 {
		return nil, nil
	}

	// Build a mapping from old descendant IDs to new IDs.
	// The new IDs use newRootID as prefix in place of src.ID.
	oldToNew := map[string]string{src.ID: newRootID}
	for _, it := range filtered {
		suffix := it.ID[len(src.ID):]
		oldToNew[it.ID] = newRootID + suffix
	}

	cloned := make([]domain.Item, 0, len(filtered))
	for _, it := range filtered {
		newIt := it
		newIt.ID = oldToNew[it.ID]
		newIt.ParentID = oldToNew[it.ParentID]
		// Inherit the cloned root's project ID so subtasks of a story
		// clone keep the original project, not the epic's ID.
		newIt.ProjectID = newRootProjectID
		newIt.Status = domain.StatusBacklog
		newIt.Claim = nil
		newIt.Journal = nil
		newIt.CompletedAt = nil
		newIt.Version = 1
		newIt.CreatedAt = now
		newIt.UpdatedAt = now
		newIt.CreatedBy = in.Agent
		newIt.CreatedByRole = in.AgentRole
		newIt.Aliases = nil
		cloned = append(cloned, newIt)
	}

	itemsJSON, err := json.Marshal(cloned)
	if err != nil {
		return nil, fmt.Errorf("marshal cloned items: %w", err)
	}

	// Record WAL for the descendant creation, with full args so Recover
	// can replay step 0 after a crash.
	opID := nextEventID(d)
	if opID == "" {
		opID = fmt.Sprintf("clone-children-%s-%d", newRootID, now.UnixNano())
	}
	op := domain.Operation{
		OpID:      opID,
		Kind:      domain.OpKindCloneWithChildren,
		Agent:     in.Agent,
		StartedAt: now,
		Status:    domain.OpStatusPending,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Args: map[string]any{"items_json": string(itemsJSON)}},
			{Step: 1, Kind: "append_event", Args: map[string]any{
				"agent":   in.Agent,
				"item_id": newRootID,
			}},
		},
	}
	if d.OpJournal != nil {
		if err := d.OpJournal.Begin(op); err != nil {
			return nil, fmt.Errorf("begin op: %w", err)
		}
	}

	for _, newIt := range cloned {
		if _, putErr := d.Store.Put(ctx, newIt, -1); putErr != nil {
			return nil, fmt.Errorf("put %s: %w", newIt.ID, putErr)
		}
	}
	if err := markStepIfJournal(d, opID, 0); err != nil {
		return nil, err
	}

	auditAppend(d, domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   now,
		Type:        "cloned_with_children",
		ItemID:      newRootID,
		Actor:       in.Agent,
		ActorRole:   in.AgentRole,
		OperationID: opID,
		Metadata: map[string]any{
			"source_id":        src.ID,
			"descendant_count": len(cloned),
		},
	})
	if err := markStepIfJournal(d, opID, 1); err != nil {
		return nil, err
	}
	if err := completeIfJournal(d, opID); err != nil {
		return nil, err
	}

	return cloned, nil
}
