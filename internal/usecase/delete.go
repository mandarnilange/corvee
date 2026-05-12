package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// DeleteInput is the request payload for Delete.
type DeleteInput struct {
	// ID identifies the item to delete.
	ID string
	// ExpectVersion is forwarded to Store.Put for soft-deletes. -1 = no check.
	ExpectVersion int
	// Hard, when true, removes the JSON files from the store instead of
	// setting status=abandoned. Phase 4+.
	Hard bool
	// Cascade, when true, includes all descendants in the delete operation
	// via OpJournal for crash-safety. Phase 4+.
	Cascade bool
	// Force, when true, bypasses the dependent guard (other items may still
	// reference the deleted ID in their dependencies/blocks). Phase 4+.
	Force bool
	// Agent stamps the journal entry's actor field.
	Agent string
}

// DeleteOutput is the response payload for Delete.
type DeleteOutput struct {
	// Item is the root item after the operation (nil for hard deletes).
	Item domain.Item `json:"item,omitempty"`
	// PrevVersion is the on-disk version before the soft-delete.
	PrevVersion int `json:"prev_version"`
	// NewVersion is the on-disk version after the soft-delete.
	NewVersion int `json:"new_version"`
	// Deleted is the list of IDs removed (populated for cascade and hard).
	Deleted []string `json:"deleted,omitempty"`
}

// Delete removes or soft-deletes an item. With --cascade, the entire
// subtree is removed atomically via OpJournal. With --hard, JSON files are
// removed from the store. Without --force, the dependent guard refuses the
// operation when any external item references this id (or its descendants)
// in dependencies or blocks.
//
// Phase 3 concurrency rule: the per-item Locker is held around the
// read-modify-write window for single-item soft-deletes.
func Delete(ctx context.Context, d Deps, in DeleteInput) (DeleteOutput, error) {
	if in.ID == "" {
		return DeleteOutput{}, fmt.Errorf("delete: id is required: %w", domain.ErrUsage)
	}

	// Lock the operation root for both single-item and cascade paths so
	// concurrent deletes/moves of the same subtree don't race (§11.1).
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(in.ID, time.Second)
		if lockErr != nil {
			return DeleteOutput{}, fmt.Errorf("delete %s: %w", in.ID, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}

	item, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return DeleteOutput{}, err
	}

	all, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return DeleteOutput{}, fmt.Errorf("delete: list workspace: %w", err)
	}

	// Collect the set of IDs being deleted (item + descendants if cascade).
	var subtree []domain.Item
	if in.Cascade {
		subtree = collectSubtree(in.ID, all)
	} else {
		subtree = []domain.Item{item}
	}
	deletedSet := make(map[string]bool, len(subtree))
	for _, it := range subtree {
		deletedSet[it.ID] = true
	}

	// Dependent guard: check that no external item references a deleted ID.
	if !in.Force {
		for _, other := range all {
			if deletedSet[other.ID] {
				continue // part of the delete set; internal references are fine
			}
			for _, dep := range other.Dependencies {
				if deletedSet[dep] {
					return DeleteOutput{}, fmt.Errorf(
						"delete: %q is a dependency of %q; use --force to bypass: %w",
						dep, other.ID, domain.ErrIntegrityViolated)
				}
			}
			for _, blk := range other.Blocks {
				if deletedSet[blk] {
					return DeleteOutput{}, fmt.Errorf(
						"delete: %q is referenced in blocks of %q; use --force to bypass: %w",
						blk, other.ID, domain.ErrIntegrityViolated)
				}
			}
		}
	}

	now := d.Clock.Now()

	if in.Cascade || in.Hard {
		return deleteCascadeOrHard(ctx, d, in, subtree, deletedSet, now)
	}

	// Single-item soft-delete (Phase 1 path).
	return softDeleteSingle(ctx, d, in, item, now)
}

// softDeleteSingle performs the Phase 1 soft-delete (status → abandoned).
func softDeleteSingle(ctx context.Context, d Deps, in DeleteInput, item domain.Item, now time.Time) (DeleteOutput, error) {
	if tErr := domain.Transition(item.Status, domain.StatusAbandoned); tErr != nil {
		return DeleteOutput{}, tErr
	}

	prevVersion := item.Version
	item.Status = domain.StatusAbandoned
	item.UpdatedAt = now
	item.Version = prevVersion + 1
	item.Journal = append(item.Journal, domain.JournalEntry{
		Timestamp: now,
		Actor:     in.Agent,
		Event:     "abandoned",
	})

	stored, err := d.Store.Put(ctx, item, in.ExpectVersion)
	if err != nil {
		return DeleteOutput{}, fmt.Errorf("delete %s: %w", in.ID, err)
	}

	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "abandoned",
		ItemID:    stored.ID,
		Actor:     in.Agent,
	})

	return DeleteOutput{
		Item:        stored,
		PrevVersion: prevVersion,
		NewVersion:  item.Version,
	}, nil
}

// deleteCascadeOrHard handles --cascade and/or --hard deletes via OpJournal.
func deleteCascadeOrHard(
	ctx context.Context,
	d Deps,
	in DeleteInput,
	subtree []domain.Item,
	_ map[string]bool,
	now time.Time,
) (DeleteOutput, error) {
	deletedIDs := make([]string, 0, len(subtree))
	for _, it := range subtree {
		deletedIDs = append(deletedIDs, it.ID)
	}

	// Build the post-mutation items so soft-cascade recovery can re-apply
	// the abandoned-state writes idempotently (hard delete uses old_ids_json
	// instead, so leave items_json empty in that branch).
	var stepArgs0 map[string]any
	if in.Hard {
		idsJSON, err := json.Marshal(deletedIDs)
		if err != nil {
			return DeleteOutput{}, fmt.Errorf("delete: marshal old ids: %w", err)
		}
		stepArgs0 = map[string]any{"old_ids_json": string(idsJSON)}
	} else {
		abandoned := make([]domain.Item, 0, len(subtree))
		for _, it := range subtree {
			updated := it
			updated.Status = domain.StatusAbandoned
			updated.UpdatedAt = now
			updated.Version = it.Version + 1
			updated.Journal = append(updated.Journal, domain.JournalEntry{
				Timestamp: now,
				Actor:     in.Agent,
				Event:     "abandoned",
			})
			abandoned = append(abandoned, updated)
		}
		itemsJSON, err := json.Marshal(abandoned)
		if err != nil {
			return DeleteOutput{}, fmt.Errorf("delete: marshal items: %w", err)
		}
		stepArgs0 = map[string]any{"items_json": string(itemsJSON)}
	}

	// Record WAL.
	opID := nextEventID(d)
	if opID == "" {
		opID = fmt.Sprintf("delete-%s-%d", in.ID, now.UnixNano())
	}
	step0Kind := "delete_items"
	if !in.Hard {
		// Soft cascade: items already exist; recovery re-applies the
		// abandoned-state writes (update_items always Puts).
		step0Kind = "update_items"
	}
	op := domain.Operation{
		OpID:      opID,
		Kind:      domain.OpKindDeleteCascade,
		Agent:     in.Agent,
		StartedAt: now,
		Status:    domain.OpStatusPending,
		Plan: []domain.OpStep{
			{Step: 0, Kind: step0Kind, Args: stepArgs0},
			{Step: 1, Kind: "append_event", Args: map[string]any{
				"agent":   in.Agent,
				"item_id": in.ID,
			}},
		},
	}
	if d.OpJournal != nil {
		if err := d.OpJournal.Begin(op); err != nil {
			return DeleteOutput{}, fmt.Errorf("delete: begin op: %w", err)
		}
	}

	// Step 0: delete all items in the subtree.
	for _, it := range subtree {
		if in.Hard {
			if err := d.Store.Delete(ctx, it.ID); err != nil {
				return DeleteOutput{}, fmt.Errorf("delete (hard) %s: %w", it.ID, err)
			}
		} else {
			// Soft-delete each item.
			updated := it
			updated.Status = domain.StatusAbandoned
			updated.UpdatedAt = now
			updated.Version = it.Version + 1
			updated.Journal = append(updated.Journal, domain.JournalEntry{
				Timestamp: now,
				Actor:     in.Agent,
				Event:     "abandoned",
			})
			if _, err := d.Store.Put(ctx, updated, -1); err != nil {
				return DeleteOutput{}, fmt.Errorf("delete (soft) %s: %w", it.ID, err)
			}
		}
	}
	if err := markStepIfJournal(d, opID, 0); err != nil {
		return DeleteOutput{}, fmt.Errorf("delete: %w", err)
	}

	// Step 1: append audit event.
	auditAppend(d, domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   now,
		Type:        "deleted_cascade",
		ItemID:      in.ID,
		Actor:       in.Agent,
		OperationID: opID,
		Metadata:    map[string]any{"deleted_ids": deletedIDs, "hard": in.Hard},
	})
	if err := markStepIfJournal(d, opID, 1); err != nil {
		return DeleteOutput{}, fmt.Errorf("delete: %w", err)
	}

	if err := completeIfJournal(d, opID); err != nil {
		return DeleteOutput{}, fmt.Errorf("delete: %w", err)
	}

	return DeleteOutput{Deleted: deletedIDs}, nil
}
