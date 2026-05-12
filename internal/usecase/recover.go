package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// RecoverInput is the request payload for Recover.
type RecoverInput struct {
	// DryRun, when true, reports what would be recovered without
	// making any changes.
	DryRun bool
}

// RecoveryResult describes a single successfully recovered operation.
type RecoveryResult struct {
	// OpID is the recovered operation's identifier.
	OpID string `json:"op_id"`
	// Kind is the operation type (move, rename, etc.).
	Kind domain.OpKind `json:"kind"`
	// StepsCompleted is how many steps were re-executed during recovery.
	StepsCompleted int `json:"steps_completed"`
	// StepsSkipped is how many steps were already done before the crash.
	StepsSkipped int `json:"steps_skipped"`
}

// RecoveryFailure describes an operation that could not be recovered.
type RecoveryFailure struct {
	// OpID identifies the failed operation.
	OpID string `json:"op_id"`
	// Kind is the operation type.
	Kind domain.OpKind `json:"kind"`
	// Reason is the human-readable failure description.
	Reason string `json:"reason"`
}

// RecoverOutput is the response payload for Recover.
type RecoverOutput struct {
	// Recovered lists operations that were successfully rolled forward.
	Recovered []RecoveryResult `json:"recovered"`
	// Failed lists operations where recovery was attempted but failed.
	Failed []RecoveryFailure `json:"failed"`
}

// Recover reads OpJournal.Pending and rolls forward any operations that
// were interrupted mid-execution. For each pending operation, Recover
// resumes from the first step whose Done == false, executing and marking
// each step until the operation is complete.
//
// With DryRun=true, no mutations are made: the store, journal, and event
// log are untouched. The output reports what *would* be recovered (plus
// per-op step counts).
//
// Auto-invoked by list/next when an in-flight op is detected (via
// ErrPendingOperation detection). Failed recoveries surface in
// task validate.
func Recover(ctx context.Context, d Deps, in RecoverInput) (RecoverOutput, error) {
	if d.OpJournal == nil {
		return RecoverOutput{}, nil
	}

	pending, err := d.OpJournal.Pending()
	if err != nil {
		return RecoverOutput{}, fmt.Errorf("recover: list pending: %w", err)
	}

	var recovered []RecoveryResult
	var failed []RecoveryFailure

	for _, op := range pending {
		if in.DryRun {
			recovered = append(recovered, summarizeDryRun(op))
			continue
		}
		result, recErr := recoverOp(ctx, d, op)
		if recErr != nil {
			failed = append(failed, RecoveryFailure{
				OpID:   op.OpID,
				Kind:   op.Kind,
				Reason: recErr.Error(),
			})
		} else {
			recovered = append(recovered, result)
		}
	}

	return RecoverOutput{Recovered: recovered, Failed: failed}, nil
}

// summarizeDryRun returns a RecoveryResult for op without mutating state.
// Each step is classified as either Skipped (already done) or Completed
// (would be replayed by a real recover).
func summarizeDryRun(op domain.Operation) RecoveryResult {
	var done, todo int
	for _, step := range op.Plan {
		if step.Done {
			done++
		} else {
			todo++
		}
	}
	return RecoveryResult{
		OpID:           op.OpID,
		Kind:           op.Kind,
		StepsCompleted: todo,
		StepsSkipped:   done,
	}
}

// recoverOp rolls forward a single pending operation.
func recoverOp(ctx context.Context, d Deps, op domain.Operation) (RecoveryResult, error) {
	var stepsCompleted, stepsSkipped int

	for _, step := range op.Plan {
		if step.Done {
			stepsSkipped++
			continue
		}
		if err := executeRecoveryStep(ctx, d, op, step); err != nil {
			return RecoveryResult{}, fmt.Errorf("step %d (%s): %w", step.Step, step.Kind, err)
		}
		if markErr := d.OpJournal.MarkStep(op.OpID, step.Step); markErr != nil {
			return RecoveryResult{}, fmt.Errorf("mark step %d: %w", step.Step, markErr)
		}
		stepsCompleted++
	}

	if err := d.OpJournal.Complete(op.OpID); err != nil {
		return RecoveryResult{}, fmt.Errorf("complete op: %w", err)
	}

	return RecoveryResult{
		OpID:           op.OpID,
		Kind:           op.Kind,
		StepsCompleted: stepsCompleted,
		StepsSkipped:   stepsSkipped,
	}, nil
}

// executeRecoveryStep dispatches to the step-kind handler.
func executeRecoveryStep(ctx context.Context, d Deps, op domain.Operation, step domain.OpStep) error {
	switch step.Kind {
	case "create_items":
		return recoverCreateItems(ctx, d, step)
	case "update_items":
		return recoverUpdateItems(ctx, d, step)
	case "rewrite_refs":
		return rewriteRefs(ctx, d, nil, step.Args)
	case "delete_items":
		return recoverDeleteItems(ctx, d, step)
	case "append_event":
		return recoverAppendEvent(d, op, step)
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
}

// recoverCreateItems re-creates items from the step args. Idempotent:
// items that already exist are skipped.
func recoverCreateItems(ctx context.Context, d Deps, step domain.OpStep) error {
	raw, ok := step.Args["items_json"]
	if !ok {
		return nil // no items to create (step had no args)
	}
	rawStr, ok := raw.(string)
	if !ok || rawStr == "" || rawStr == "null" {
		return nil
	}
	items, err := rebuildItemsFromJSON(rawStr)
	if err != nil {
		return fmt.Errorf("recover create_items: %w", err)
	}
	for _, it := range items {
		exists, probeErr := d.Store.Exists(ctx, it.ID)
		if probeErr != nil {
			return fmt.Errorf("recover create_items: probe %s: %w", it.ID, probeErr)
		}
		if exists {
			continue // already created before the crash
		}
		if _, putErr := d.Store.Put(ctx, it, -1); putErr != nil {
			return fmt.Errorf("recover create_items: put %s: %w", it.ID, putErr)
		}
	}
	return nil
}

// recoverUpdateItems re-applies post-mutation state to existing items
// (e.g., the abandoned-status version produced by soft cascade delete).
// Idempotent in effect: the target state is the same regardless of how
// many times the step is replayed; we always Put with expectVersion=-1
// because the desired end-state is fixed.
func recoverUpdateItems(ctx context.Context, d Deps, step domain.OpStep) error {
	raw, ok := step.Args["items_json"]
	if !ok {
		return nil
	}
	rawStr, ok := raw.(string)
	if !ok || rawStr == "" || rawStr == "null" {
		return nil
	}
	items, err := rebuildItemsFromJSON(rawStr)
	if err != nil {
		return fmt.Errorf("recover update_items: %w", err)
	}
	for _, it := range items {
		// Skip if item is missing (e.g., already hard-deleted by a
		// concurrent operation) — stay idempotent.
		exists, probeErr := d.Store.Exists(ctx, it.ID)
		if probeErr != nil {
			return fmt.Errorf("recover update_items: probe %s: %w", it.ID, probeErr)
		}
		if !exists {
			continue
		}
		if _, putErr := d.Store.Put(ctx, it, -1); putErr != nil {
			return fmt.Errorf("recover update_items: put %s: %w", it.ID, putErr)
		}
	}
	return nil
}

// recoverDeleteItems removes old items. Idempotent: missing items are skipped.
func recoverDeleteItems(ctx context.Context, d Deps, step domain.OpStep) error {
	raw, ok := step.Args["old_ids_json"]
	if !ok {
		return nil
	}
	rawStr, ok := raw.(string)
	if !ok || rawStr == "" || rawStr == "null" || rawStr == "[]" {
		return nil
	}
	ids, err := rebuildOldIDsFromJSON(rawStr)
	if err != nil {
		return fmt.Errorf("recover delete_items: %w", err)
	}
	for _, id := range ids {
		if delErr := d.Store.Delete(ctx, id); delErr != nil {
			if errors.Is(delErr, domain.ErrNotFound) {
				continue // already deleted before the crash
			}
			return fmt.Errorf("recover delete_items: delete %s: %w", id, delErr)
		}
	}
	return nil
}

// recoverAppendEvent appends a recovery audit event. We don't try to
// reconstruct the exact original event (which would require more arg
// serialisation) — instead we emit a "recovered" event. This means the
// events log may record the event twice (once in the original run, once
// here) but the log is tolerant of duplicates.
func recoverAppendEvent(d Deps, op domain.Operation, step domain.OpStep) error {
	agent := ""
	if a, ok := step.Args["agent"]; ok {
		if aStr, ok := a.(string); ok {
			agent = aStr
		}
	}

	// Reconstruct a minimal event from what's in the args.
	ev := domain.Event{
		EventID:     nextEventID(d),
		Timestamp:   d.Clock.Now(),
		Type:        fmt.Sprintf("%s_recovered", op.Kind),
		Actor:       agent,
		OperationID: op.OpID,
	}

	// Try to recover more from item_id if available.
	if raw, ok := step.Args["item_id"]; ok {
		if itemID, ok := raw.(string); ok {
			ev.ItemID = itemID
		}
	}

	auditAppend(d, ev)
	return nil
}
