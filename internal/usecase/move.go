package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// markStepIfJournal calls MarkStep on the journal when present and wraps
// any error so callers can propagate it. Errors here mean the journal
// disk write failed; without surfacing them, a later crash would force
// full re-execution instead of resuming from the failure point.
func markStepIfJournal(d Deps, opID string, step int) error {
	if d.OpJournal == nil {
		return nil
	}
	if err := d.OpJournal.MarkStep(opID, step); err != nil {
		return fmt.Errorf("mark step %d: %w", step, err)
	}
	return nil
}

// completeIfJournal calls Complete on the journal when present and wraps
// any error.
func completeIfJournal(d Deps, opID string) error {
	if d.OpJournal == nil {
		return nil
	}
	if err := d.OpJournal.Complete(opID); err != nil {
		return fmt.Errorf("complete op: %w", err)
	}
	return nil
}

// MoveMapping records a single old→new ID substitution from a Move or Rename.
type MoveMapping struct {
	// OldID is the item's former canonical ID.
	OldID string `json:"old_id"`
	// NewID is the item's new canonical ID after the operation.
	NewID string `json:"new_id"`
}

// MoveInput is the request payload for Move.
type MoveInput struct {
	// SourceID is the item (and its whole subtree) to relocate.
	SourceID string
	// TargetParentID is the new parent for SourceID.
	TargetParentID string
	// Agent stamps journal entries and the operation record.
	Agent string
	// AgentRole stamps journal entries.
	AgentRole domain.Role
}

// MoveOutput is the response payload for Move.
type MoveOutput struct {
	// Item is the moved source item under its new ID.
	Item domain.Item `json:"item"`
	// Mapping is the complete old→new ID table for the moved subtree.
	Mapping []MoveMapping `json:"mapping"`
}

// Move relocates SourceID and all of its descendants under TargetParentID.
// Procedure per §8.2:
//  1. Compute new IDs (prefix-swap from old source ID to new source ID).
//  2. Create new item files with updated IDs, parent_id, project_id,
//     and old IDs appended to aliases[].
//  3. Scan all items for old IDs in dependencies/blocks and rewrite.
//  4. Delete old item files.
//  5. Append a "moved" event with the full mapping.
//
// The whole operation is journalled via OpJournal so Recover can
// roll forward after a crash at any step.
func Move(ctx context.Context, d Deps, in MoveInput) (MoveOutput, error) {
	if in.SourceID == "" {
		return MoveOutput{}, fmt.Errorf("move: source_id is required: %w", domain.ErrUsage)
	}
	if in.TargetParentID == "" {
		return MoveOutput{}, fmt.Errorf("move: target_parent_id is required: %w", domain.ErrUsage)
	}
	if in.SourceID == in.TargetParentID {
		return MoveOutput{}, fmt.Errorf("move: source and target parent must differ: %w", domain.ErrUsage)
	}

	// Lock the operation root so two concurrent moves of the same subtree
	// can't race their listing/computing/writing windows (§11.1).
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(in.SourceID, time.Second)
		if lockErr != nil {
			return MoveOutput{}, fmt.Errorf("move: acquire %q: %w", in.SourceID, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}

	src, err := d.Store.Get(ctx, in.SourceID)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: load source %q: %w", in.SourceID, err)
	}
	if src.Type == domain.TypeProject {
		return MoveOutput{}, fmt.Errorf("move: cannot move a project: %w", domain.ErrUsage)
	}

	targetParent, err := d.Store.Get(ctx, in.TargetParentID)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: load target parent %q: %w", in.TargetParentID, err)
	}

	expectedParent := expectedParentType(src.Type)
	if targetParent.Type != expectedParent {
		return MoveOutput{}, fmt.Errorf("move: %s requires parent of type %q, got %q: %w",
			src.Type, expectedParent, targetParent.Type, domain.ErrUsage)
	}

	// Collect entire subtree (source + all descendants).
	all, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: list workspace: %w", err)
	}
	subtree := collectSubtree(in.SourceID, all)

	// Compute new source ID: next free sibling under target parent.
	targetChildren := childrenOf(in.TargetParentID, all)
	nextNum, err := nextSiblingNum(targetChildren, targetParent.Type)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: next sibling: %w", err)
	}
	newSourceID, err := domain.ChildID(in.TargetParentID, nextNum)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: compute new id: %w", err)
	}

	// Build old→new mapping by prefix-swapping source ID.
	mapping := buildMapping(in.SourceID, newSourceID, subtree)

	// Determine new project ID for all moved items.
	newProjectID := targetParent.ProjectID
	if newProjectID == "" {
		newProjectID = targetParent.ID // target parent is the project itself
	}

	now := d.Clock.Now()

	// Build new items with updated IDs, parent_id, project_id, aliases.
	newItems := buildNewItems(subtree, mapping, in.SourceID, in.TargetParentID, newProjectID, now)

	// Record operation intent in the WAL before touching any files.
	opID := nextEventID(d)
	if opID == "" {
		opID = fmt.Sprintf("move-%s-%d", in.SourceID, now.UnixNano())
	}

	steps, stepArgs, err := buildMoveSteps(newItems, mapping, all, in.SourceID, in.Agent, now)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: build steps: %w", err)
	}

	op := domain.Operation{
		OpID:      opID,
		Kind:      domain.OpKindMove,
		Agent:     in.Agent,
		StartedAt: now,
		Status:    domain.OpStatusPending,
		Plan:      steps,
	}

	if d.OpJournal != nil {
		if err := d.OpJournal.Begin(op); err != nil {
			return MoveOutput{}, fmt.Errorf("move: begin op: %w", err)
		}
	}

	// Step 0: create new items.
	for _, it := range newItems {
		if _, putErr := d.Store.Put(ctx, it, -1); putErr != nil {
			return MoveOutput{}, fmt.Errorf("move: put %s: %w", it.ID, putErr)
		}
	}
	if err := markStepIfJournal(d, opID, 0); err != nil {
		return MoveOutput{}, fmt.Errorf("move: %w", err)
	}

	// Step 1: rewrite references (dependencies/blocks) in non-moved items.
	if err := rewriteRefs(ctx, d, all, stepArgs[1]); err != nil {
		return MoveOutput{}, fmt.Errorf("move: rewrite refs: %w", err)
	}
	if err := markStepIfJournal(d, opID, 1); err != nil {
		return MoveOutput{}, fmt.Errorf("move: %w", err)
	}

	// Step 2: delete old items.
	for _, it := range subtree {
		if delErr := d.Store.Delete(ctx, it.ID); delErr != nil {
			return MoveOutput{}, fmt.Errorf("move: delete %s: %w", it.ID, delErr)
		}
	}
	if err := markStepIfJournal(d, opID, 2); err != nil {
		return MoveOutput{}, fmt.Errorf("move: %w", err)
	}

	// Step 3: append audit event.
	auditAppend(d, domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   now,
		Type:        "moved",
		ItemID:      newSourceID,
		Actor:       in.Agent,
		ActorRole:   in.AgentRole,
		OperationID: opID,
		Metadata:    mappingToMeta(mapping),
	})
	if err := markStepIfJournal(d, opID, 3); err != nil {
		return MoveOutput{}, fmt.Errorf("move: %w", err)
	}

	if err := completeIfJournal(d, opID); err != nil {
		return MoveOutput{}, fmt.Errorf("move: %w", err)
	}

	// Re-open any ancestor of the new parent that was previously
	// `done`. Moving a subtree under a closed parent invalidates the
	// parent's "no open work" claim. Same semantics as Add — see
	// reopenAncestorsIfDone. Best-effort: log via slog.Warn (matches
	// Add) but don't fail the move; the op-journal makes partial
	// state recoverable.
	if rerr := reopenAncestorsIfDone(ctx, d, in.TargetParentID, in.Agent, in.AgentRole, now); rerr != nil {
		slog.Warn("move: failed to auto-reopen ancestors",
			slog.String("source_id", in.SourceID),
			slog.String("target_parent", in.TargetParentID),
			slog.String("error", rerr.Error()))
	}

	// Symmetric close: re-evaluate the OLD parent chain. If a previously
	// auto-reopened ancestor now has all children settled, flip it back
	// to `done` with an auto_closed marker. Best-effort like the
	// reopen path.
	if src.ParentID != "" {
		if cerr := closeAncestorsIfAllChildrenDone(ctx, d, src.ParentID, in.Agent, in.AgentRole, now); cerr != nil {
			slog.Warn("move: failed to auto-close ancestors of old parent",
				slog.String("source_id", in.SourceID),
				slog.String("old_parent", src.ParentID),
				slog.String("error", cerr.Error()))
		}
	}

	movedItem, err := d.Store.Get(ctx, newSourceID)
	if err != nil {
		return MoveOutput{}, fmt.Errorf("move: load moved item: %w", err)
	}

	return MoveOutput{Item: movedItem, Mapping: mapping}, nil
}

// collectSubtree returns source and all its descendants from all items.
// O(N): one pass to index by parent_id, then a BFS down the tree.
func collectSubtree(sourceID string, all []domain.Item) []domain.Item {
	childrenByParent := make(map[string][]string, len(all))
	for _, it := range all {
		if it.ParentID != "" {
			childrenByParent[it.ParentID] = append(childrenByParent[it.ParentID], it.ID)
		}
	}
	inSubtree := map[string]bool{sourceID: true}
	queue := []string{sourceID}
	for len(queue) > 0 {
		head := queue[0]
		queue = queue[1:]
		for _, childID := range childrenByParent[head] {
			if !inSubtree[childID] {
				inSubtree[childID] = true
				queue = append(queue, childID)
			}
		}
	}

	// Preserve input order so callers that rely on stable iteration
	// (mapping construction, deterministic JSON serialisation) keep
	// working unchanged.
	result := make([]domain.Item, 0, len(inSubtree))
	for _, it := range all {
		if inSubtree[it.ID] {
			result = append(result, it)
		}
	}
	return result
}

// childrenOf returns the direct children of parentID from all items.
func childrenOf(parentID string, all []domain.Item) []domain.Item {
	var children []domain.Item
	for _, it := range all {
		if it.ParentID == parentID {
			children = append(children, it)
		}
	}
	return children
}

// buildMapping computes the old→new ID mapping for the subtree.
// All IDs in the subtree share the oldSourceID prefix; the new IDs
// replace that prefix with newSourceID.
func buildMapping(oldSourceID, newSourceID string, subtree []domain.Item) []MoveMapping {
	m := make([]MoveMapping, 0, len(subtree))
	for _, it := range subtree {
		var newID string
		if it.ID == oldSourceID {
			newID = newSourceID
		} else {
			// E.g., "SRC-E01-S01" → "DST-E01-S01" when oldSourceID="SRC-E01"
			suffix := it.ID[len(oldSourceID):]
			newID = newSourceID + suffix
		}
		m = append(m, MoveMapping{OldID: it.ID, NewID: newID})
	}
	return m
}

// buildNewItems constructs the updated items with new IDs, parent_id,
// project_id, and aliases.
func buildNewItems(
	subtree []domain.Item,
	mapping []MoveMapping,
	oldSourceID, targetParentID, newProjectID string,
	now time.Time,
) []domain.Item {
	oldToNew := mappingIndex(mapping)
	items := make([]domain.Item, 0, len(subtree))
	for _, it := range subtree {
		newID := oldToNew[it.ID]
		updated := it
		updated.ID = newID
		updated.ProjectID = newProjectID
		updated.UpdatedAt = now
		updated.Version = it.Version + 1

		// Parent: source item gets the new target parent; others keep
		// their parent within the subtree (remapped).
		if it.ID == oldSourceID {
			updated.ParentID = targetParentID
		} else {
			updated.ParentID = oldToNew[it.ParentID]
		}

		// Rewrite dependencies and blocks within the subtree.
		updated.Dependencies = rewriteIDs(it.Dependencies, oldToNew)
		updated.Blocks = rewriteIDs(it.Blocks, oldToNew)

		// Preserve old ID in aliases.
		updated.Aliases = append(appendUnique(updated.Aliases, it.ID), it.Aliases...)
		updated.Aliases = deduplicateStrings(updated.Aliases)

		items = append(items, updated)
	}
	return items
}

// rewriteIDs replaces any IDs in slice that appear in oldToNew.
func rewriteIDs(ids []string, oldToNew map[string]string) []string {
	if len(ids) == 0 {
		return ids
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		if newID, ok := oldToNew[id]; ok {
			out[i] = newID
		} else {
			out[i] = id
		}
	}
	return out
}

// buildMoveSteps constructs the WAL plan and returns the step slice and
// pre-serialised args for each step.
func buildMoveSteps(
	newItems []domain.Item,
	mapping []MoveMapping,
	all []domain.Item,
	oldSourceID, agent string,
	now time.Time,
) ([]domain.OpStep, []map[string]any, error) {
	// Collect old IDs in the subtree for step 2 (delete).
	oldIDs := make([]string, 0, len(mapping))
	for _, m := range mapping {
		oldIDs = append(oldIDs, m.OldID)
	}

	// For step 1 (rewrite refs): find non-subtree items that reference old IDs.
	oldSet := make(map[string]bool, len(oldIDs))
	for _, id := range oldIDs {
		oldSet[id] = true
	}
	oldToNew := mappingIndex(mapping)

	type refRewrite struct {
		ItemID   string            `json:"item_id"`
		OldToNew map[string]string `json:"old_to_new"`
	}
	var rewrites []refRewrite
	for _, it := range all {
		if oldSet[it.ID] {
			continue // it's in the subtree, handled by step 0
		}
		needsRewrite := false
		for _, dep := range it.Dependencies {
			if oldSet[dep] {
				needsRewrite = true
				break
			}
		}
		if !needsRewrite {
			for _, blk := range it.Blocks {
				if oldSet[blk] {
					needsRewrite = true
					break
				}
			}
		}
		if needsRewrite {
			rewrites = append(rewrites, refRewrite{ItemID: it.ID, OldToNew: oldToNew})
		}
	}

	newItemsJSON, err := json.Marshal(newItems)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal new items: %w", err)
	}
	rewritesJSON, err := json.Marshal(rewrites)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal rewrites: %w", err)
	}
	oldIDsJSON, err := json.Marshal(oldIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal old ids: %w", err)
	}

	stepArgs := []map[string]any{
		{"items_json": string(newItemsJSON)},
		{"rewrites_json": string(rewritesJSON)},
		{"old_ids_json": string(oldIDsJSON)},
		{"agent": agent, "ts": now.Format("2006-01-02T15:04:05Z")},
	}
	steps := []domain.OpStep{
		{Step: 0, Kind: "create_items", Args: stepArgs[0]},
		{Step: 1, Kind: "rewrite_refs", Args: stepArgs[1]},
		{Step: 2, Kind: "delete_items", Args: stepArgs[2]},
		{Step: 3, Kind: "append_event", Args: stepArgs[3]},
	}
	return steps, stepArgs, nil
}

// rewriteRefsArgs holds the step-1 data for ref rewriting.
type rewriteRefsEntry struct {
	ItemID   string            `json:"item_id"`
	OldToNew map[string]string `json:"old_to_new"`
}

// rewriteRefs scans all non-subtree items for dependencies/blocks containing
// old IDs and updates them.
func rewriteRefs(ctx context.Context, d Deps, all []domain.Item, args map[string]any) error {
	raw, ok := args["rewrites_json"]
	if !ok {
		return nil
	}
	rawStr, ok := raw.(string)
	if !ok {
		return nil
	}
	if rawStr == "" || rawStr == "null" {
		return nil
	}
	var rewrites []rewriteRefsEntry
	if err := json.Unmarshal([]byte(rawStr), &rewrites); err != nil {
		return fmt.Errorf("rewrite refs: parse: %w", err)
	}
	for _, rw := range rewrites {
		it, err := d.Store.Get(ctx, rw.ItemID)
		if err != nil {
			continue // item may have been moved or deleted; skip
		}
		newDeps := rewriteIDs(it.Dependencies, rw.OldToNew)
		newBlocks := rewriteIDs(it.Blocks, rw.OldToNew)
		// Skip the write entirely when nothing changed — keeps Recover
		// idempotent in version history when replayed against an item
		// that was already rewritten before the crash.
		if stringSliceEqual(it.Dependencies, newDeps) && stringSliceEqual(it.Blocks, newBlocks) {
			continue
		}
		it.Dependencies = newDeps
		it.Blocks = newBlocks
		it.Version++
		if _, err := d.Store.Put(ctx, it, -1); err != nil {
			return fmt.Errorf("rewrite refs: put %s: %w", rw.ItemID, err)
		}
	}
	return nil
}

// stringSliceEqual reports whether two string slices contain the same
// elements in the same order. Treats nil and empty as equal.
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mappingIndex converts a []MoveMapping to a map[old]new for O(1) lookup.
func mappingIndex(mapping []MoveMapping) map[string]string {
	m := make(map[string]string, len(mapping))
	for _, mv := range mapping {
		m[mv.OldID] = mv.NewID
	}
	return m
}

// mappingToMeta converts the mapping slice to the event Metadata map.
func mappingToMeta(mapping []MoveMapping) map[string]any {
	pairs := make([]any, len(mapping))
	for i, m := range mapping {
		pairs[i] = map[string]any{"old_id": m.OldID, "new_id": m.NewID}
	}
	return map[string]any{"mapping": pairs}
}

// appendUnique appends value to slice only if not already present.
func appendUnique(slice []string, value string) []string {
	for _, s := range slice {
		if s == value {
			return slice
		}
	}
	return append(slice, value)
}

// deduplicateStrings returns a new slice with duplicates removed,
// preserving first-occurrence order.
func deduplicateStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// rebuildItemFromJSON is used during recovery to re-materialise a list of
// items from a JSON string stored in step args.
func rebuildItemsFromJSON(rawJSON string) ([]domain.Item, error) {
	var items []domain.Item
	if err := json.Unmarshal([]byte(rawJSON), &items); err != nil {
		return nil, fmt.Errorf("rebuild items: %w", err)
	}
	return items, nil
}

// rebuildOldIDsFromJSON re-materialises the delete list from step args.
func rebuildOldIDsFromJSON(rawJSON string) ([]string, error) {
	var ids []string
	if err := json.Unmarshal([]byte(rawJSON), &ids); err != nil {
		return nil, fmt.Errorf("rebuild old ids: %w", err)
	}
	return ids, nil
}
