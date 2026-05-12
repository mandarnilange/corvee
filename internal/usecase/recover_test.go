package usecase

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func recoverDeps(store *testfakes.Store, journal *testfakes.OpJournal) Deps {
	return Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("recover"),
		OpJournal: journal,
	}
}

func TestRecover_NoPendingOps_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	journal := testfakes.NewOpJournal()
	d := recoverDeps(testfakes.NewStore(), journal)
	out, err := Recover(context.Background(), d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(out.Recovered) != 0 || len(out.Failed) != 0 {
		t.Errorf("expected empty output, got %+v", out)
	}
}

func TestRecover_ResumesPartialMoveAfterStep0(t *testing.T) {
	t.Parallel()
	// Simulate a crash after step 0 (new items created) but before
	// step 2 (old items deleted).
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Original items that would be deleted in step 2.
	oldItems := []domain.Item{
		{SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	// New items already created by step 0.
	newItems := []domain.Item{
		{SchemaVersion: 1, ID: "DST-E01", Type: domain.TypeEpic, ProjectID: "DST", ParentID: "DST", Title: "E", Status: domain.StatusBacklog, Aliases: []string{"SRC-E01"}, CreatedAt: now, UpdatedAt: now, Version: 2},
	}
	for _, it := range oldItems {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	for _, it := range newItems {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	// Construct the pending operation with step 0 done, steps 1-3 not done.
	oldIDsJSON, _ := json.Marshal([]string{"SRC-E01"})
	op := domain.Operation{
		OpID:      "recover-test-1",
		Kind:      domain.OpKindMove,
		Agent:     "agent-1",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Args: map[string]any{}, Done: true},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": "null"}, Done: false},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": string(oldIDsJSON)}, Done: false},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "agent-1"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	out, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(out.Recovered) != 1 {
		t.Fatalf("want 1 recovered, got %d: %+v", len(out.Recovered), out)
	}
	result := out.Recovered[0]
	if result.OpID != "recover-test-1" {
		t.Errorf("OpID: got %q want recover-test-1", result.OpID)
	}
	if result.StepsCompleted == 0 {
		t.Error("should have completed at least steps 2-3")
	}
	// Old item should now be deleted.
	if exists, _ := store.Exists(ctx, "SRC-E01"); exists {
		t.Error("SRC-E01 should be deleted after recovery")
	}
	// The operation should be completed in the journal.
	pending, _ := journal.Pending()
	if len(pending) != 0 {
		t.Errorf("want 0 pending after recovery, got %d", len(pending))
	}
}

func TestRecover_SkipsAlreadyDoneSteps(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	journal := testfakes.NewOpJournal()
	store := testfakes.NewStore()

	op := domain.Operation{
		OpID:      "recover-test-2",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: time.Now(),
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: true},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": "null"}, Done: true},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": "[]"}, Done: true},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "a"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}
	d := recoverDeps(store, journal)
	out, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(out.Recovered) != 1 {
		t.Fatalf("want 1 recovered, got %d", len(out.Recovered))
	}
	if out.Recovered[0].StepsSkipped != 3 {
		t.Errorf("StepsSkipped: got %d want 3", out.Recovered[0].StepsSkipped)
	}
	if out.Recovered[0].StepsCompleted != 1 {
		t.Errorf("StepsCompleted: got %d want 1", out.Recovered[0].StepsCompleted)
	}
}

func TestRecover_ReportsFailedOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	journal := testfakes.NewOpJournal()
	store := testfakes.NewStore()

	// An op with an unknown step kind should fail recovery.
	op := domain.Operation{
		OpID:      "recover-test-fail",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: time.Now(),
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "unknown_step_kind", Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}
	d := recoverDeps(store, journal)
	out, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover returned error: %v", err)
	}
	if len(out.Failed) != 1 {
		t.Fatalf("want 1 failed, got %d", len(out.Failed))
	}
	if out.Failed[0].OpID != "recover-test-fail" {
		t.Errorf("failed OpID: got %q", out.Failed[0].OpID)
	}
}

func TestRecover_DeleteItemsStep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Put an item that needs to be deleted.
	toDelete := domain.Item{SchemaVersion: 1, ID: "OLD-E01", Type: domain.TypeEpic, ProjectID: "OLD", ParentID: "OLD", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1}
	if _, err := store.Put(ctx, toDelete, -1); err != nil {
		t.Fatal(err)
	}

	oldIDsJSON, _ := json.Marshal([]string{"OLD-E01"})
	op := domain.Operation{
		OpID:      "delete-step-test",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: true},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": "null"}, Done: true},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": string(oldIDsJSON)}, Done: false},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "a"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	_, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if exists, _ := store.Exists(ctx, "OLD-E01"); exists {
		t.Error("OLD-E01 should be deleted after recovery")
	}
}

func TestRecover_CreateItemsStep_IdempotentRecreation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// New items that should be created by the recovery step.
	newItems := []domain.Item{
		{SchemaVersion: 1, ID: "DST-E02", Type: domain.TypeEpic, ProjectID: "DST", ParentID: "DST", Title: "Recovered", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	itemsJSON, _ := json.Marshal(newItems)

	op := domain.Operation{
		OpID:      "create-step-test",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Args: map[string]any{"items_json": string(itemsJSON)}, Done: false},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": "null"}, Done: false},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": "[]"}, Done: false},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "a"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	out, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(out.Recovered) != 1 {
		t.Fatalf("want 1 recovered, got %d: %+v", len(out.Recovered), out)
	}
	// DST-E02 should now exist.
	if exists, _ := store.Exists(ctx, "DST-E02"); !exists {
		t.Error("DST-E02 should exist after recovery")
	}
	// All ops should be completed (no pending remaining).
	pending, _ := journal.Pending()
	if len(pending) != 0 {
		t.Errorf("want 0 pending after recovery, got %d", len(pending))
	}
}

func TestRecover_RewriteRefsStep_UpdatesCrossReferences(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// External item that references the old ID in its dependencies.
	external := domain.Item{
		SchemaVersion: 1, ID: "OTHER-S01", Type: domain.TypeStory,
		ProjectID: "OTHER", ParentID: "OTHER-E01", Title: "Dep",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
		Dependencies: []string{"SRC-E01"},
	}
	if _, err := store.Put(ctx, external, -1); err != nil {
		t.Fatal(err)
	}

	rewrites := []rewriteRefsEntry{
		{ItemID: "OTHER-S01", OldToNew: map[string]string{"SRC-E01": "DST-E01"}},
	}
	rewritesJSON, _ := json.Marshal(rewrites)

	op := domain.Operation{
		OpID:      "rewrite-step-test",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: true},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": string(rewritesJSON)}, Done: false},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": "[]"}, Done: false},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "a"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	_, err := Recover(ctx, d, RecoverInput{})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	// OTHER-S01 dependency should now point to DST-E01.
	it, err := store.Get(ctx, "OTHER-S01")
	if err != nil {
		t.Fatalf("get OTHER-S01: %v", err)
	}
	if len(it.Dependencies) != 1 || it.Dependencies[0] != "DST-E01" {
		t.Errorf("dependencies: got %v want [DST-E01]", it.Dependencies)
	}
}

// TestRecover_DryRun_DoesNotMutate ensures DryRun reports pending ops
// without executing steps, marking the journal, or appending events.
func TestRecover_DryRun_DoesNotMutate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Inject an item that the would-be deletion targets, so we can
	// verify it is NOT removed by the dry run.
	target := domain.Item{
		SchemaVersion: 1, ID: "OLD-E01", Type: domain.TypeEpic, ProjectID: "OLD",
		ParentID: "OLD", Title: "E", Status: domain.StatusBacklog,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, target, -1); err != nil {
		t.Fatal(err)
	}

	oldIDsJSON, _ := json.Marshal([]string{"OLD-E01"})
	op := domain.Operation{
		OpID:      "dryrun-test",
		Kind:      domain.OpKindMove,
		Agent:     "a",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "create_items", Done: true},
			{Step: 1, Kind: "rewrite_refs", Args: map[string]any{"rewrites_json": "null"}, Done: false},
			{Step: 2, Kind: "delete_items", Args: map[string]any{"old_ids_json": string(oldIDsJSON)}, Done: false},
			{Step: 3, Kind: "append_event", Args: map[string]any{"agent": "a"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	out, err := Recover(ctx, d, RecoverInput{DryRun: true})
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if len(out.Recovered) != 1 {
		t.Fatalf("dry-run should report 1 op, got %d", len(out.Recovered))
	}
	if out.Recovered[0].StepsSkipped != 1 || out.Recovered[0].StepsCompleted != 3 {
		t.Errorf("dry-run counts: got skipped=%d completed=%d want 1/3",
			out.Recovered[0].StepsSkipped, out.Recovered[0].StepsCompleted)
	}
	// The item should still exist (no actual deletion).
	if exists, _ := store.Exists(ctx, "OLD-E01"); !exists {
		t.Error("dry-run should not delete OLD-E01")
	}
	// The op should still be pending in the journal.
	pending, _ := journal.Pending()
	if len(pending) != 1 {
		t.Errorf("dry-run should leave op pending, got %d", len(pending))
	}
}

// TestRecover_UpdateItemsStep_AppliesPostMutationState verifies that the
// update_items step (used by soft-cascade delete) re-applies the changed
// item state on recovery, even when the items already exist.
func TestRecover_UpdateItemsStep_AppliesPostMutationState(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Put an item in the live state (not abandoned).
	live := domain.Item{
		SchemaVersion: 1, ID: "LIVE-E01", Type: domain.TypeEpic, ProjectID: "LIVE",
		ParentID: "LIVE", Title: "E", Status: domain.StatusBacklog,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, live, -1); err != nil {
		t.Fatal(err)
	}

	// Step args carry the post-mutation form (status=abandoned).
	abandoned := live
	abandoned.Status = domain.StatusAbandoned
	abandoned.Version = 2
	itemsJSON, _ := json.Marshal([]domain.Item{abandoned})

	op := domain.Operation{
		OpID:      "update-step-test",
		Kind:      domain.OpKindDeleteCascade,
		Agent:     "a",
		StartedAt: now,
		Status:    domain.OpStatusExecuting,
		Plan: []domain.OpStep{
			{Step: 0, Kind: "update_items", Args: map[string]any{"items_json": string(itemsJSON)}, Done: false},
			{Step: 1, Kind: "append_event", Args: map[string]any{"agent": "a", "item_id": "LIVE-E01"}, Done: false},
		},
	}
	if err := journal.Begin(op); err != nil {
		t.Fatal(err)
	}

	d := recoverDeps(store, journal)
	if _, err := Recover(ctx, d, RecoverInput{}); err != nil {
		t.Fatalf("Recover: %v", err)
	}

	// LIVE-E01 should now be abandoned.
	got, err := store.Get(ctx, "LIVE-E01")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status != domain.StatusAbandoned {
		t.Errorf("status: got %q want abandoned", got.Status)
	}
}
