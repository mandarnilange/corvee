package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestDelete_SoftSetsAbandoned(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}

	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "RKN", ExpectVersion: 1, Agent: "alice",
	})
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if out.Item.Status != domain.StatusAbandoned {
		t.Errorf("status=%v, want abandoned", out.Item.Status)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "abandoned" || last.Actor != "alice" {
		t.Errorf("journal=%+v", last)
	}
	// Item still exists in store (soft-delete).
	if exists, _ := store.Exists(context.Background(), "RKN"); !exists {
		t.Errorf("soft-delete removed the file")
	}
}

func TestDelete_HardDeletesFile(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock, OpJournal: testfakes.NewOpJournal()}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "RKN", Hard: true, ExpectVersion: 1,
	})
	if err != nil {
		t.Fatalf("hard delete: %v", err)
	}
	// Item should be gone (hard deleted)
	if exists, _ := store.Exists(context.Background(), "RKN"); exists {
		t.Error("hard delete should remove the item from store")
	}
}

func TestDelete_CascadeDeletesChildren(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock, OpJournal: testfakes.NewOpJournal()}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{SchemaVersion: 1, ID: "PRJ", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01", Type: domain.TypeEpic, ProjectID: "PRJ", ParentID: "PRJ", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01-S01", Type: domain.TypeStory, ProjectID: "PRJ", ParentID: "PRJ-E01", Title: "S", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	out, err := usecase.Delete(ctx, d, usecase.DeleteInput{
		ID: "PRJ-E01", Cascade: true, Hard: true,
	})
	if err != nil {
		t.Fatalf("cascade delete: %v", err)
	}
	if len(out.Deleted) == 0 {
		t.Error("Deleted list should be non-empty")
	}
	// Epic and story should both be gone
	for _, id := range []string{"PRJ-E01", "PRJ-E01-S01"} {
		if exists, _ := store.Exists(ctx, id); exists {
			t.Errorf("cascade: %s should be deleted", id)
		}
	}
}

func TestDelete_CascadeRefusesWithExternalDependent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock, OpJournal: testfakes.NewOpJournal()}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{SchemaVersion: 1, ID: "PRJ", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01", Type: domain.TypeEpic, ProjectID: "PRJ", ParentID: "PRJ", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E01-S01", Type: domain.TypeStory, ProjectID: "PRJ", ParentID: "PRJ-E01", Title: "S", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "PRJ-E02", Type: domain.TypeEpic, ProjectID: "PRJ", ParentID: "PRJ", Title: "E2",
			Status: domain.StatusBacklog, Dependencies: []string{"PRJ-E01-S01"}, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	_, err := usecase.Delete(ctx, d, usecase.DeleteInput{
		ID: "PRJ-E01", Cascade: true,
	})
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Errorf("expected ErrIntegrityViolated, got %v", err)
	}
}

func TestDelete_ForceBypassesDependentGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock, OpJournal: testfakes.NewOpJournal()}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []domain.Item{
		{SchemaVersion: 1, ID: "AAA", Type: domain.TypeProject, Title: "A", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "BBB", Type: domain.TypeProject, Title: "B", Status: domain.StatusBacklog, Dependencies: []string{"AAA"}, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	// With --force, should succeed even though BBB depends on AAA
	_, err := usecase.Delete(ctx, d, usecase.DeleteInput{
		ID: "AAA", Force: true, Hard: true,
	})
	if err != nil {
		t.Errorf("force delete: %v", err)
	}
}

func TestDelete_RefusesWhenDependent(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	for _, code := range []string{"AAA", "BBB"} {
		if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
			Type: domain.TypeProject, ProjectCode: code, Title: code,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Make BBB depend on AAA.
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "BBB", ExpectVersion: 1, AddDeps: []string{"AAA"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "AAA", ExpectVersion: 1,
	})
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Errorf("err=%v, want ErrIntegrityViolated", err)
	}
}

func TestDelete_RefusesWhenBlockedBy(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "AAA", Title: "a",
	}); err != nil {
		t.Fatal(err)
	}
	// Manually inject a Blocks reference (not exposed via Update yet).
	got, _ := store.Get(context.Background(), "AAA")
	other := got
	other.ID = "BBB"
	other.Blocks = []string{"AAA"}
	other.Title = "b"
	if _, err := store.Put(context.Background(), other, -1); err != nil {
		t.Fatal(err)
	}
	_, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{
		ID: "AAA", ExpectVersion: 1,
	})
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Errorf("err=%v, want ErrIntegrityViolated", err)
	}
}

func TestDelete_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	d := usecase.Deps{
		Store: testfakes.NewStore(),
		Clock: testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
	}
	_, err := usecase.Delete(context.Background(), d, usecase.DeleteInput{ID: "RKN"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

// TestDelete_HardCascadeWALHasReplayableArgs verifies the journalled
// operation carries old_ids_json so Recover can re-execute step 0
// after a crash (per Phase-4 review finding 1).
func TestDelete_HardCascadeWALHasReplayableArgs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	items := []domain.Item{
		{SchemaVersion: 1, ID: "P1", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P1-E01", Type: domain.TypeEpic, ProjectID: "P1", ParentID: "P1", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := usecase.Deps{
		Store:     store,
		Clock:     testfakes.NewClock(now),
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: journal,
	}
	if _, err := usecase.Delete(ctx, d, usecase.DeleteInput{
		ID: "P1-E01", Hard: true, Cascade: true, Agent: "a",
	}); err != nil {
		t.Fatal(err)
	}

	all := journal.All()
	if len(all) != 1 {
		t.Fatalf("want 1 op in journal, got %d", len(all))
	}
	plan := all[0].Plan
	if plan[0].Kind != "delete_items" {
		t.Errorf("hard cascade step 0: got kind=%q want delete_items", plan[0].Kind)
	}
	if got, _ := plan[0].Args["old_ids_json"].(string); got == "" || got == "null" || got == "[]" {
		t.Errorf("hard cascade step 0 missing old_ids_json: %+v", plan[0].Args)
	}
}

// TestDelete_SoftCascadeWALUsesUpdateItems verifies the journalled
// operation uses update_items (not create_items) for soft cascade so
// Recover replays the abandoned-state writes against existing items
// (per Phase-4 review finding 1).
func TestDelete_SoftCascadeWALUsesUpdateItems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	journal := testfakes.NewOpJournal()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	items := []domain.Item{
		{SchemaVersion: 1, ID: "P2", Type: domain.TypeProject, Title: "P", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P2-E01", Type: domain.TypeEpic, ProjectID: "P2", ParentID: "P2", Title: "E", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1},
	}
	for _, it := range items {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := usecase.Deps{
		Store:     store,
		Clock:     testfakes.NewClock(now),
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: journal,
	}
	if _, err := usecase.Delete(ctx, d, usecase.DeleteInput{
		ID: "P2-E01", Cascade: true, Agent: "a",
	}); err != nil {
		t.Fatal(err)
	}

	all := journal.All()
	if len(all) != 1 {
		t.Fatalf("want 1 op in journal, got %d", len(all))
	}
	plan := all[0].Plan
	if plan[0].Kind != "update_items" {
		t.Errorf("soft cascade step 0: got kind=%q want update_items", plan[0].Kind)
	}
	if got, _ := plan[0].Args["items_json"].(string); got == "" || got == "null" {
		t.Errorf("soft cascade step 0 missing items_json: %+v", plan[0].Args)
	}
}
