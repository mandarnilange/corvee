package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func makeMoveStore(t *testing.T) (*testfakes.Store, domain.Item, domain.Item, domain.Item, domain.Item) {
	t.Helper()
	store := testfakes.NewStore()
	ctx := context.Background()

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	src := domain.Item{
		SchemaVersion: 1, ID: "SRC", Type: domain.TypeProject,
		Title: "Source", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	dst := domain.Item{
		SchemaVersion: 1, ID: "DST", Type: domain.TypeProject,
		Title: "Dest", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	epic := domain.Item{
		SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC",
		Title: "Epic1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	story := domain.Item{
		SchemaVersion: 1, ID: "SRC-E01-S01", Type: domain.TypeStory, ProjectID: "SRC", ParentID: "SRC-E01",
		Title: "Story1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	for _, item := range []domain.Item{src, dst, epic, story} {
		if _, err := store.Put(ctx, item, -1); err != nil {
			t.Fatal(err)
		}
	}
	return store, src, dst, epic, story
}

func moveDeps(store *testfakes.Store) Deps {
	return Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("move-ulid"),
		OpJournal: testfakes.NewOpJournal(),
	}
}

func TestMove_EpicToAnotherProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)
	d := moveDeps(store)

	out, err := Move(ctx, d, MoveInput{
		SourceID:       "SRC-E01",
		TargetParentID: "DST",
		Agent:          "agent-1",
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	// New epic should be DST-E01 (first epic under DST)
	if out.Item.ID != "DST-E01" {
		t.Errorf("moved item ID: got %q want %q", out.Item.ID, "DST-E01")
	}
	if out.Item.ProjectID != "DST" {
		t.Errorf("moved item ProjectID: got %q want %q", out.Item.ProjectID, "DST")
	}
	if out.Item.ParentID != "DST" {
		t.Errorf("moved item ParentID: got %q want %q", out.Item.ParentID, "DST")
	}

	// Mapping should include both epic and story
	if len(out.Mapping) != 2 {
		t.Errorf("mapping len: got %d want 2", len(out.Mapping))
	}

	// Old items should be gone
	_, err = store.Get(ctx, "SRC-E01")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old SRC-E01 should not exist, got err=%v", err)
	}
	_, err = store.Get(ctx, "SRC-E01-S01")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("old SRC-E01-S01 should not exist, got err=%v", err)
	}

	// New items should exist
	newEpic, err := store.Get(ctx, "DST-E01")
	if err != nil {
		t.Fatalf("new DST-E01 missing: %v", err)
	}
	if newEpic.ParentID != "DST" {
		t.Errorf("new epic ParentID: got %q", newEpic.ParentID)
	}

	newStory, err := store.Get(ctx, "DST-E01-S01")
	if err != nil {
		t.Fatalf("new DST-E01-S01 missing: %v", err)
	}
	if newStory.ParentID != "DST-E01" {
		t.Errorf("new story ParentID: got %q want DST-E01", newStory.ParentID)
	}
	if newStory.ProjectID != "DST" {
		t.Errorf("new story ProjectID: got %q want DST", newStory.ProjectID)
	}
}

func TestMove_AliasesPreserveOldIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)
	d := moveDeps(store)

	_, err := Move(ctx, d, MoveInput{SourceID: "SRC-E01", TargetParentID: "DST", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	// The new epic should carry the old ID in Aliases
	newEpic, err := store.Get(ctx, "DST-E01")
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
		t.Errorf("DST-E01.Aliases should contain SRC-E01, got %v", newEpic.Aliases)
	}
}

func TestMove_RewritesDependencies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)

	// Add another story that depends on the story being moved
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	watcher := domain.Item{
		SchemaVersion: 1, ID: "SRC-E01-S02", Type: domain.TypeStory,
		ProjectID: "SRC", ParentID: "SRC-E01",
		Title: "Watcher", Status: domain.StatusBacklog,
		Dependencies: []string{"SRC-E01-S01"},
		CreatedAt:    now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, watcher, -1); err != nil {
		t.Fatal(err)
	}

	d := moveDeps(store)
	_, err := Move(ctx, d, MoveInput{SourceID: "SRC-E01", TargetParentID: "DST", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	// Watcher is moved too (it's a descendant of SRC-E01)
	newWatcher, err := store.Get(ctx, "DST-E01-S02")
	if err != nil {
		t.Fatalf("new watcher missing: %v", err)
	}
	if len(newWatcher.Dependencies) != 1 || newWatcher.Dependencies[0] != "DST-E01-S01" {
		t.Errorf("dependencies not rewritten: %v", newWatcher.Dependencies)
	}
}

func TestMove_3LevelHierarchy(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	items := []domain.Item{
		{SchemaVersion: 1, ID: "SRC", Type: domain.TypeProject, Title: "S", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "DST", Type: domain.TypeProject, Title: "D", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic, ProjectID: "SRC", ParentID: "SRC", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01-S01", Type: domain.TypeStory, ProjectID: "SRC", ParentID: "SRC-E01", Title: "S1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01-S01-T01", Type: domain.TypeSubtask, ProjectID: "SRC", ParentID: "SRC-E01-S01", Title: "T1", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := moveDeps(store)
	out, err := Move(ctx, d, MoveInput{SourceID: "SRC-E01", TargetParentID: "DST", Agent: "a"})
	if err != nil {
		t.Fatalf("Move 3-level: %v", err)
	}
	// 3 items should be in the mapping
	if len(out.Mapping) != 3 {
		t.Errorf("mapping len: got %d want 3 (epic+story+subtask)", len(out.Mapping))
	}

	// Verify subtask exists under new IDs
	_, err = store.Get(ctx, "DST-E01-S01-T01")
	if err != nil {
		t.Fatalf("subtask under new IDs: %v", err)
	}
}

func TestMove_RejectsEmptySourceID(t *testing.T) {
	t.Parallel()
	_, err := Move(context.Background(), Deps{}, MoveInput{TargetParentID: "DST"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage for empty source_id, got %v", err)
	}
}

func TestMove_RejectsEmptyTargetParentID(t *testing.T) {
	t.Parallel()
	_, err := Move(context.Background(), Deps{}, MoveInput{SourceID: "SRC-E01"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage for empty target_parent_id, got %v", err)
	}
}

func TestMove_RejectsIncompatibleParentType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)
	d := moveDeps(store)

	// Try to move an epic to a story (invalid: epic must be under project)
	_, err := Move(ctx, d, MoveInput{
		SourceID:       "SRC-E01",
		TargetParentID: "SRC-E01-S01",
		Agent:          "a",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage for type mismatch, got %v", err)
	}
}

func TestMove_RecordsOpJournal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)
	journal := testfakes.NewOpJournal()
	d := Deps{
		Store:     store,
		Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: journal,
	}

	_, err := Move(ctx, d, MoveInput{SourceID: "SRC-E01", TargetParentID: "DST", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	all := journal.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 op in journal, got %d", len(all))
	}
	if all[0].Status != domain.OpStatusCompleted {
		t.Errorf("op status: got %q want completed", all[0].Status)
	}
}

func TestMove_RewritesExternalDependencies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, _, _, _ := makeMoveStore(t)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// External item in a third project that depends on SRC-E01-S01.
	external := domain.Item{
		SchemaVersion: 1, ID: "EXT-E01-S01", Type: domain.TypeStory,
		ProjectID: "EXT", ParentID: "EXT-E01", Title: "Ext",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
		Dependencies: []string{"SRC-E01-S01"},
		Blocks:       []string{"SRC-E01"},
	}
	// EXT project must exist for the ID to be readable.
	if _, err := store.Put(ctx, domain.Item{
		SchemaVersion: 1, ID: "EXT", Type: domain.TypeProject,
		Title: "Ext", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, external, -1); err != nil {
		t.Fatal(err)
	}

	d := moveDeps(store)
	_, err := Move(ctx, d, MoveInput{SourceID: "SRC-E01", TargetParentID: "DST", Agent: "a"})
	if err != nil {
		t.Fatal(err)
	}

	// External item's deps should be rewritten to new IDs.
	it, err := store.Get(ctx, "EXT-E01-S01")
	if err != nil {
		t.Fatalf("get EXT-E01-S01: %v", err)
	}
	if len(it.Dependencies) != 1 || it.Dependencies[0] != "DST-E01-S01" {
		t.Errorf("dependencies not rewritten: got %v want [DST-E01-S01]", it.Dependencies)
	}
	if len(it.Blocks) != 1 || it.Blocks[0] != "DST-E01" {
		t.Errorf("blocks not rewritten: got %v want [DST-E01]", it.Blocks)
	}
}

func TestMove_AutoClosesOldParentWhenLastOpenChildLeaves(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	seedNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Source side: SRC project, SRC-E01 epic that was once `done` and
	// then auto-reopened (journal carries the marker, status flipped to
	// in_progress). One done sibling (S01) and one open child (S02).
	for _, it := range []domain.Item{
		{SchemaVersion: 1, ID: "SRC", Type: domain.TypeProject,
			Title: "Source", Status: domain.StatusBacklog,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01", Type: domain.TypeEpic,
			ProjectID: "SRC", ParentID: "SRC",
			Title: "Reopened epic", Status: domain.StatusInProgress,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 2,
			Journal: []domain.JournalEntry{{
				Timestamp: seedNow, Actor: "system",
				Event: "auto_reopened",
				Note:  "ancestor reopened: new child added under done parent",
			}}},
		{SchemaVersion: 1, ID: "SRC-E01-S01", Type: domain.TypeStory,
			ProjectID: "SRC", ParentID: "SRC-E01",
			Title: "Already done", Status: domain.StatusDone,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1},
		{SchemaVersion: 1, ID: "SRC-E01-S02", Type: domain.TypeStory,
			ProjectID: "SRC", ParentID: "SRC-E01",
			Title: "Last open child", Status: domain.StatusBacklog,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1},

		// Target side: DST project + DST-E01 epic to host the moved story.
		{SchemaVersion: 1, ID: "DST", Type: domain.TypeProject,
			Title: "Dest", Status: domain.StatusBacklog,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1},
		{SchemaVersion: 1, ID: "DST-E01", Type: domain.TypeEpic,
			ProjectID: "DST", ParentID: "DST",
			Title: "Target epic", Status: domain.StatusBacklog,
			CreatedAt: seedNow, UpdatedAt: seedNow, Version: 1},
	} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatalf("seed %s: %v", it.ID, err)
		}
	}

	d := moveDeps(store)
	if _, err := Move(ctx, d, MoveInput{
		SourceID: "SRC-E01-S02", TargetParentID: "DST-E01", Agent: "alice",
	}); err != nil {
		t.Fatalf("Move: %v", err)
	}

	got, err := store.Get(ctx, "SRC-E01")
	if err != nil {
		t.Fatalf("reload SRC-E01: %v", err)
	}
	if got.Status != domain.StatusDone {
		t.Errorf("SRC-E01 status = %q, want %q (last open child moved away)",
			got.Status, domain.StatusDone)
	}
	if got.CompletedAt == nil {
		t.Error("SRC-E01 CompletedAt not set; auto-close must stamp it")
	}
	var hasAutoClosed bool
	for _, je := range got.Journal {
		if je.Event == "auto_closed" {
			hasAutoClosed = true
			break
		}
	}
	if !hasAutoClosed {
		t.Errorf("SRC-E01 journal missing auto_closed entry; got %+v", got.Journal)
	}
}

func TestMove_AppendUniqueSkipsDuplicate(t *testing.T) {
	t.Parallel()
	// appendUnique should not add a value that's already present.
	got := appendUnique([]string{"a", "b"}, "a")
	if len(got) != 2 {
		t.Errorf("appendUnique: got len %d want 2 (no duplicate added)", len(got))
	}
	// Should add a new value.
	got = appendUnique([]string{"a", "b"}, "c")
	if len(got) != 3 || got[2] != "c" {
		t.Errorf("appendUnique: got %v want [a b c]", got)
	}
}
