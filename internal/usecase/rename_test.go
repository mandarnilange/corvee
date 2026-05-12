package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func renameDeps(store *testfakes.Store) Deps {
	return Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("rename-op"),
		OpJournal: testfakes.NewOpJournal(),
	}
}

func makeRenameStore(t *testing.T) *testfakes.Store {
	t.Helper()
	store := testfakes.NewStore()
	ctx := context.Background()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	items := []domain.Item{
		{SchemaVersion: 1, ID: "SRC", Type: domain.TypeProject, Title: "Source", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC", Title: "Epic1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01-S01", Type: domain.TypeStory, ProjectID: "SRC", ParentID: "SRC-E01", Title: "Story1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	return store
}

func TestRename_EpicToExplicitID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := makeRenameStore(t)
	d := renameDeps(store)

	out, err := Rename(ctx, d, RenameInput{
		SourceID: "SRC-E01",
		TargetID: "SRC-E02",
		Agent:    "agent-1",
	})
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if out.Item.ID != "SRC-E02" {
		t.Errorf("item ID: got %q want SRC-E02", out.Item.ID)
	}
	// Old ID should be gone
	_, err = store.Get(ctx, "SRC-E01")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old SRC-E01 should be deleted")
	}
	// New ID should exist
	_, err = store.Get(ctx, "SRC-E02")
	if err != nil {
		t.Fatalf("SRC-E02 missing: %v", err)
	}
	// Child should be renamed too
	_, err = store.Get(ctx, "SRC-E01-S01")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old SRC-E01-S01 should be deleted")
	}
	_, err = store.Get(ctx, "SRC-E02-S01")
	if err != nil {
		t.Fatalf("SRC-E02-S01 missing: %v", err)
	}
}

func TestRename_RefusesIfTargetExists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := makeRenameStore(t)
	// Add conflicting target
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	conflict := domain.Item{SchemaVersion: 1, ID: "SRC-E02", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC", Title: "Existing", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1}
	if _, err := store.Put(ctx, conflict, -1); err != nil {
		t.Fatal(err)
	}

	d := renameDeps(store)
	_, err := Rename(ctx, d, RenameInput{
		SourceID: "SRC-E01",
		TargetID: "SRC-E02",
		Agent:    "a",
	})
	if err == nil {
		t.Fatal("expected error when target exists")
	}
	// Should not be ErrUsage since it's a conflict (wrapped in a general error)
	// but should definitely be an error
}

func TestRename_AliasesPreserveOldID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := makeRenameStore(t)
	d := renameDeps(store)

	_, err := Rename(ctx, d, RenameInput{SourceID: "SRC-E01", TargetID: "SRC-E03", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	newEpic, err := store.Get(ctx, "SRC-E03")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, alias := range newEpic.Aliases {
		if alias == "SRC-E01" {
			found = true
		}
	}
	if !found {
		t.Errorf("SRC-E03.Aliases should contain SRC-E01, got %v", newEpic.Aliases)
	}
}

func TestRename_RejectsEmptySourceID(t *testing.T) {
	t.Parallel()
	_, err := Rename(context.Background(), Deps{}, RenameInput{TargetID: "SRC-E02"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestRename_RejectsEmptyTargetID(t *testing.T) {
	t.Parallel()
	_, err := Rename(context.Background(), Deps{}, RenameInput{SourceID: "SRC-E01"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestRename_RejectsSameSourceAndTarget(t *testing.T) {
	t.Parallel()
	_, err := Rename(context.Background(), Deps{}, RenameInput{SourceID: "SRC-E01", TargetID: "SRC-E01"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestRename_ChildrenCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{SchemaVersion: 1, ID: "PRJ", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01", Type: domain.TypeEpic, ProjectID: "PRJ", ParentID: "PRJ", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01-S01", Type: domain.TypeStory, ProjectID: "PRJ", ParentID: "PRJ-E01", Title: "S", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01-S01-T01", Type: domain.TypeSubtask, ProjectID: "PRJ", ParentID: "PRJ-E01-S01", Title: "T", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	d := renameDeps(store)
	out, err := Rename(ctx, d, RenameInput{SourceID: "PRJ-E01", TargetID: "PRJ-E05", Agent: "a"})
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if len(out.Mapping) != 3 {
		t.Errorf("mapping len: got %d want 3", len(out.Mapping))
	}
	// All new items should exist
	for _, id := range []string{"PRJ-E05", "PRJ-E05-S01", "PRJ-E05-S01-T01"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Errorf("new %s missing: %v", id, err)
		}
	}
}

// TestRename_WALStepsHaveReplayableArgs verifies that the journalled
// operation carries the args Recover needs to roll forward after a
// crash (per Phase-4 review finding 1).
func TestRename_WALStepsHaveReplayableArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	// Seed: project + epic.
	items := []domain.Item{
		{SchemaVersion: 1, ID: "PRJ", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01", Type: domain.TypeEpic, ProjectID: "PRJ", ParentID: "PRJ", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := Deps{
		Store:     store,
		Clock:     testfakes.NewClock(now),
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: journal,
	}
	if _, err := Rename(ctx, d, RenameInput{SourceID: "PRJ-E01", TargetID: "PRJ-E05", Agent: "a"}); err != nil {
		t.Fatal(err)
	}

	all := journal.All()
	if len(all) != 1 {
		t.Fatalf("want 1 op in journal, got %d", len(all))
	}
	plan := all[0].Plan
	// Every step must have non-empty Args so Recover can replay it.
	if got, _ := plan[0].Args["items_json"].(string); got == "" || got == "null" {
		t.Errorf("step 0 (create_items) missing items_json: %+v", plan[0].Args)
	}
	if _, ok := plan[1].Args["rewrites_json"].(string); !ok {
		t.Errorf("step 1 (rewrite_refs) missing rewrites_json: %+v", plan[1].Args)
	}
	if got, _ := plan[2].Args["old_ids_json"].(string); got == "" || got == "null" {
		t.Errorf("step 2 (delete_items) missing old_ids_json: %+v", plan[2].Args)
	}
	if _, ok := plan[3].Args["agent"].(string); !ok {
		t.Errorf("step 3 (append_event) missing agent: %+v", plan[3].Args)
	}
}
