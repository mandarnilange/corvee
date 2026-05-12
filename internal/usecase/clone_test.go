package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestClone_StoryAssignsNextSiblingID(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN"},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", ProjectID: "RKN"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01", ProjectID: "RKN", Title: "orig", Status: domain.StatusDone, Version: 4},
	)
	out, err := Clone(context.Background(), Deps{Store: store, Clock: clock}, CloneInput{
		SourceID: "RKN-E01-S01",
		ParentID: "RKN-E01",
		Agent:    "alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.ID != "RKN-E01-S02" {
		t.Errorf("clone ID = %q, want RKN-E01-S02", out.Item.ID)
	}
	if out.Item.Status != domain.StatusBacklog {
		t.Errorf("clone status = %q, want backlog", out.Item.Status)
	}
	if out.Item.Version != 1 {
		t.Errorf("clone version = %d, want 1", out.Item.Version)
	}
	if out.Item.Title != "orig" {
		t.Errorf("clone title = %q", out.Item.Title)
	}
	if out.Item.CreatedBy != "alice" {
		t.Errorf("created_by = %q", out.Item.CreatedBy)
	}
}

func TestClone_RejectsProject(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{ID: "RKN", Type: domain.TypeProject})
	_, err := Clone(context.Background(), Deps{Store: store, Clock: testfakes.NewClock(time.Now())}, CloneInput{
		SourceID: "RKN", ParentID: "x",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestClone_RejectsParentTypeMismatch(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01"},
	)
	_, err := Clone(context.Background(), Deps{Store: store, Clock: testfakes.NewClock(time.Now())}, CloneInput{
		SourceID: "RKN-E01-S01",
		ParentID: "RKN", // story under project — wrong
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestClone_WithChildren_ClonesDescendants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	mustPut(t, store,
		domain.Item{SchemaVersion: 1, ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN", Title: "p", Status: domain.StatusBacklog, Version: 1},
		domain.Item{SchemaVersion: 1, ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", ProjectID: "RKN", Title: "E1", Status: domain.StatusBacklog, Version: 1},
		domain.Item{SchemaVersion: 1, ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01", ProjectID: "RKN", Title: "S1", Status: domain.StatusDone, Version: 3},
		domain.Item{SchemaVersion: 1, ID: "RKN-E01-S01-T01", Type: domain.TypeSubtask, ParentID: "RKN-E01-S01", ProjectID: "RKN", Title: "T1", Status: domain.StatusInProgress, Version: 2},
	)
	d := Deps{
		Store:     store,
		Clock:     clock,
		IDGen:     testfakes.NewIDGen("op"),
		OpJournal: testfakes.NewOpJournal(),
	}
	out, err := Clone(ctx, d, CloneInput{
		SourceID:     "RKN-E01",
		ParentID:     "RKN",
		Agent:        "alice",
		WithChildren: true,
	})
	if err != nil {
		t.Fatalf("Clone --with-children: %v", err)
	}
	// Root clone should be RKN-E02 (next sibling)
	if out.Item.ID != "RKN-E02" {
		t.Errorf("root clone ID: got %q want RKN-E02", out.Item.ID)
	}
	if len(out.Descendants) != 2 {
		t.Errorf("descendants count: got %d want 2 (story+subtask)", len(out.Descendants))
	}
	// All cloned items should be in store
	for _, id := range []string{"RKN-E02", "RKN-E02-S01", "RKN-E02-S01-T01"} {
		if _, err := store.Get(ctx, id); err != nil {
			t.Errorf("cloned %s missing: %v", id, err)
		}
	}
	// All cloned items should be reset
	for _, desc := range out.Descendants {
		if desc.Status != domain.StatusBacklog {
			t.Errorf("%s status: got %q want backlog", desc.ID, desc.Status)
		}
		if desc.Version != 1 {
			t.Errorf("%s version: got %d want 1", desc.ID, desc.Version)
		}
	}
}

func TestClone_WithChildren_ShallowItemHasNoDescendants(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Now())
	mustPut(t, store,
		domain.Item{SchemaVersion: 1, ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN", Title: "p", Status: domain.StatusBacklog, Version: 1},
		domain.Item{SchemaVersion: 1, ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", ProjectID: "RKN", Title: "E", Status: domain.StatusBacklog, Version: 1},
		domain.Item{SchemaVersion: 1, ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01", ProjectID: "RKN", Title: "S1", Status: domain.StatusBacklog, Version: 1},
	)
	d := Deps{Store: store, Clock: clock, IDGen: testfakes.NewIDGen("op"), OpJournal: testfakes.NewOpJournal()}
	out, err := Clone(ctx, d, CloneInput{
		SourceID:     "RKN-E01-S01",
		ParentID:     "RKN-E01",
		Agent:        "a",
		WithChildren: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Story has no children, so Descendants should be empty
	if len(out.Descendants) != 0 {
		t.Errorf("leaf item should have no descendants, got %d", len(out.Descendants))
	}
}

func TestClone_TitleOverride(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Now())
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN"},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", ProjectID: "RKN"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01", ProjectID: "RKN", Title: "orig"},
	)
	out, err := Clone(context.Background(), Deps{Store: store, Clock: clock}, CloneInput{
		SourceID: "RKN-E01-S01", ParentID: "RKN-E01", TitleOverride: "new",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.Title != "new" {
		t.Errorf("title not overridden: %q", out.Item.Title)
	}
}

// TestClone_WithChildren_StoryUnderEpic_DescendantsKeepProjectID verifies
// finding 3: when cloning a story (under an epic), subtasks must keep
// the original root project ID, not the epic's ID.
func TestClone_WithChildren_StoryUnderEpic_DescendantsKeepProjectID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

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

	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(now),
		IDGen: testfakes.NewIDGen("op"),
	}
	out, err := Clone(ctx, d, CloneInput{
		SourceID: "PRJ-E01-S01", ParentID: "PRJ-E01", Agent: "a", WithChildren: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Cloned story is under PRJ-E01 → ID becomes PRJ-E01-S02; subtask becomes PRJ-E01-S02-T01.
	if out.Item.ProjectID != "PRJ" {
		t.Errorf("cloned story ProjectID: got %q want PRJ", out.Item.ProjectID)
	}
	if len(out.Descendants) != 1 {
		t.Fatalf("want 1 descendant, got %d", len(out.Descendants))
	}
	if out.Descendants[0].ProjectID != "PRJ" {
		t.Errorf("cloned subtask ProjectID: got %q want PRJ (not the epic ID)",
			out.Descendants[0].ProjectID)
	}
}
