package usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// RenameInput is the request payload for Rename.
type RenameInput struct {
	// SourceID is the item (and its subtree) to rename.
	SourceID string
	// TargetID is the explicit new ID for the source item. Children
	// are cascaded: their IDs are recomputed with the new prefix.
	TargetID string
	// Agent stamps journal entries and the operation record.
	Agent string
	// AgentRole stamps journal entries.
	AgentRole domain.Role
}

// RenameOutput is the response payload for Rename.
type RenameOutput struct {
	// Item is the renamed source item under its new ID.
	Item domain.Item `json:"item"`
	// Mapping is the complete old→new ID table for the renamed subtree.
	Mapping []MoveMapping `json:"mapping"`
}

// Rename gives SourceID an explicit new ID (TargetID), cascading all
// descendant IDs via the same prefix-swap approach as Move. The item's
// parent and project are unchanged — only the ID changes.
//
// Refuses with an error when TargetID already exists in the store.
// Uses the same OpJournal WAL pattern as Move for crash-safety.
func Rename(ctx context.Context, d Deps, in RenameInput) (RenameOutput, error) {
	if in.SourceID == "" {
		return RenameOutput{}, fmt.Errorf("rename: source_id is required: %w", domain.ErrUsage)
	}
	if in.TargetID == "" {
		return RenameOutput{}, fmt.Errorf("rename: target_id is required: %w", domain.ErrUsage)
	}
	if in.SourceID == in.TargetID {
		return RenameOutput{}, fmt.Errorf("rename: source and target ID must differ: %w", domain.ErrUsage)
	}

	// Validate the target ID is well-formed.
	if _, err := domain.ParseID(in.TargetID); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: invalid target_id %q: %w", in.TargetID, domain.ErrUsage)
	}

	// Lock the operation root so two concurrent renames of the same
	// subtree don't race the read/write window (§11.1).
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(in.SourceID, time.Second)
		if lockErr != nil {
			return RenameOutput{}, fmt.Errorf("rename: acquire %q: %w", in.SourceID, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}

	src, err := d.Store.Get(ctx, in.SourceID)
	if err != nil {
		return RenameOutput{}, fmt.Errorf("rename: load source %q: %w", in.SourceID, err)
	}

	// Refuse if target already exists.
	exists, err := d.Store.Exists(ctx, in.TargetID)
	if err != nil {
		return RenameOutput{}, fmt.Errorf("rename: probe target %q: %w", in.TargetID, err)
	}
	if exists {
		return RenameOutput{}, fmt.Errorf("rename: target %q already exists: %w", in.TargetID, domain.ErrIntegrityViolated)
	}

	// Validate new target ID is compatible with source's parent type.
	// The parent segment of TargetID must match the parent segment of SourceID
	// to ensure the item stays under its current parent.
	srcParsed, _ := domain.ParseID(in.SourceID)
	dstParsed, _ := domain.ParseID(in.TargetID)
	if srcParsed.Type() != dstParsed.Type() {
		return RenameOutput{}, fmt.Errorf("rename: source type %q and target type %q must match: %w",
			srcParsed.Type(), dstParsed.Type(), domain.ErrUsage)
	}
	// Ensure target stays under the same parent (parent segments must match).
	srcParentStr := srcParsed.Parent().String()
	dstParentStr := dstParsed.Parent().String()
	if srcParentStr != dstParentStr {
		return RenameOutput{}, fmt.Errorf(
			"rename: target %q has different parent segment than source %q (use move to change parent): %w",
			in.TargetID, in.SourceID, domain.ErrUsage)
	}

	all, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return RenameOutput{}, fmt.Errorf("rename: list workspace: %w", err)
	}
	subtree := collectSubtree(in.SourceID, all)

	// Build mapping: prefix-swap SourceID→TargetID for all subtree items.
	mapping := make([]MoveMapping, 0, len(subtree))
	for _, it := range subtree {
		var newID string
		if it.ID == in.SourceID {
			newID = in.TargetID
		} else {
			suffix := strings.TrimPrefix(it.ID, in.SourceID)
			newID = in.TargetID + suffix
		}
		mapping = append(mapping, MoveMapping{OldID: it.ID, NewID: newID})
	}

	now := d.Clock.Now()
	oldToNew := mappingIndex(mapping)

	// Build new items (same parent, same project, new IDs + aliases).
	newItems := make([]domain.Item, 0, len(subtree))
	for _, it := range subtree {
		newID := oldToNew[it.ID]
		updated := it
		updated.ID = newID
		updated.UpdatedAt = now
		updated.Version = it.Version + 1
		if it.ParentID != "" {
			if mapped, ok := oldToNew[it.ParentID]; ok {
				updated.ParentID = mapped
			}
		}
		updated.Dependencies = rewriteIDs(it.Dependencies, oldToNew)
		updated.Blocks = rewriteIDs(it.Blocks, oldToNew)
		updated.Aliases = deduplicateStrings(append(appendUnique(it.Aliases, it.ID), it.Aliases...))
		newItems = append(newItems, updated)
	}

	// Record WAL.
	opID := nextEventID(d)
	if opID == "" {
		opID = fmt.Sprintf("rename-%s-%d", in.SourceID, now.UnixNano())
	}
	steps, _, err := buildMoveSteps(newItems, mapping, all, in.SourceID, in.Agent, now)
	if err != nil {
		return RenameOutput{}, fmt.Errorf("rename: build steps: %w", err)
	}
	op := domain.Operation{
		OpID:      opID,
		Kind:      domain.OpKindRename,
		Agent:     in.Agent,
		StartedAt: now,
		Status:    domain.OpStatusPending,
		Plan:      steps,
	}
	if d.OpJournal != nil {
		if err := d.OpJournal.Begin(op); err != nil {
			return RenameOutput{}, fmt.Errorf("rename: begin op: %w", err)
		}
	}

	// Step 0: create new items.
	for _, it := range newItems {
		if _, err := d.Store.Put(ctx, it, -1); err != nil {
			return RenameOutput{}, fmt.Errorf("rename: put %s: %w", it.ID, err)
		}
	}
	if err := markStepIfJournal(d, opID, 0); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: %w", err)
	}

	// Step 1: rewrite references in non-subtree items.
	oldSet := make(map[string]bool, len(mapping))
	for _, m := range mapping {
		oldSet[m.OldID] = true
	}
	for _, it := range all {
		if oldSet[it.ID] {
			continue
		}
		needsRewrite := false
		for _, dep := range it.Dependencies {
			if _, ok := oldToNew[dep]; ok {
				needsRewrite = true
				break
			}
		}
		if !needsRewrite {
			for _, blk := range it.Blocks {
				if _, ok := oldToNew[blk]; ok {
					needsRewrite = true
					break
				}
			}
		}
		if !needsRewrite {
			continue
		}
		fresh, err := d.Store.Get(ctx, it.ID)
		if err != nil {
			continue
		}
		fresh.Dependencies = rewriteIDs(fresh.Dependencies, oldToNew)
		fresh.Blocks = rewriteIDs(fresh.Blocks, oldToNew)
		fresh.Version++
		if _, err := d.Store.Put(ctx, fresh, -1); err != nil {
			return RenameOutput{}, fmt.Errorf("rename: rewrite ref %s: %w", it.ID, err)
		}
	}
	if err := markStepIfJournal(d, opID, 1); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: %w", err)
	}

	// Step 2: delete old items.
	for _, it := range subtree {
		if err := d.Store.Delete(ctx, it.ID); err != nil {
			return RenameOutput{}, fmt.Errorf("rename: delete %s: %w", it.ID, err)
		}
	}
	if err := markStepIfJournal(d, opID, 2); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: %w", err)
	}

	// Step 3: append audit event.
	auditAppend(d, domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   now,
		Type:        "renamed",
		ItemID:      in.TargetID,
		Actor:       in.Agent,
		ActorRole:   in.AgentRole,
		OperationID: opID,
		Metadata: map[string]any{
			"old_id": in.SourceID,
			"new_id": in.TargetID,
		},
	})
	if err := markStepIfJournal(d, opID, 3); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: %w", err)
	}

	if err := completeIfJournal(d, opID); err != nil {
		return RenameOutput{}, fmt.Errorf("rename: %w", err)
	}

	renamedItem, err := d.Store.Get(ctx, in.TargetID)
	if err != nil {
		return RenameOutput{}, fmt.Errorf("rename: load renamed item: %w", err)
	}
	_ = src // used implicitly via subtree load
	return RenameOutput{Item: renamedItem, Mapping: mapping}, nil
}
