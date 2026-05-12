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

func newAddDeps(t *testing.T) (usecase.Deps, *testfakes.Store, *testfakes.Clock) {
	t.Helper()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC))
	return usecase.Deps{Store: store, Clock: clock}, store, clock
}

func TestAdd_Project_AssignsProjectCodeAsID(t *testing.T) {
	t.Parallel()
	d, store, clock := newAddDeps(t)
	out, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:        domain.TypeProject,
		ProjectCode: "RKN",
		Title:       "Project Rikonect",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Item.ID != "RKN" {
		t.Errorf("ID = %q, want RKN", out.Item.ID)
	}
	if out.Item.Type != domain.TypeProject {
		t.Errorf("Type = %q", out.Item.Type)
	}
	if out.Item.Version != 1 {
		t.Errorf("Version = %d, want 1", out.Item.Version)
	}
	if !out.Item.CreatedAt.Equal(clock.Now()) && !out.Item.CreatedAt.Equal(out.Item.UpdatedAt) {
		t.Errorf("CreatedAt/UpdatedAt not from clock: created=%v updated=%v", out.Item.CreatedAt, out.Item.UpdatedAt)
	}
	if got, _ := store.Get(context.Background(), "RKN"); got.Title != "Project Rikonect" {
		t.Errorf("not persisted: %+v", got)
	}
}

func TestAdd_Project_RequiresProjectCode(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	_, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:  domain.TypeProject,
		Title: "no code",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v, want ErrUsage", err)
	}
}

func TestAdd_Project_RejectsParent(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	_, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:        domain.TypeProject,
		ProjectCode: "RKN",
		ParentID:    "RKN",
		Title:       "x",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v, want ErrUsage", err)
	}
}

func TestAdd_Epic_RequiresProjectParent(t *testing.T) {
	t.Parallel()
	d, store, _ := newAddDeps(t)
	// Seed a project.
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}

	out, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeEpic,
		ParentID: "RKN",
		Title:    "First epic",
	})
	if err != nil {
		t.Fatalf("Add epic: %v", err)
	}
	if out.Item.ID != "RKN-E01" {
		t.Errorf("ID = %q, want RKN-E01", out.Item.ID)
	}
	if out.Item.ProjectID != "RKN" || out.Item.ParentID != "RKN" {
		t.Errorf("project/parent: %+v", out.Item)
	}

	// Adding another epic should get E02.
	out2, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeEpic,
		ParentID: "RKN",
		Title:    "Second epic",
	})
	if err != nil {
		t.Fatal(err)
	}
	if out2.Item.ID != "RKN-E02" {
		t.Errorf("ID = %q, want RKN-E02", out2.Item.ID)
	}
	if exists, _ := store.Exists(context.Background(), "RKN-E02"); !exists {
		t.Error("not persisted")
	}
}

func TestAdd_Story_RequiresEpicParent(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	for _, in := range []usecase.AddInput{
		{Type: domain.TypeProject, ProjectCode: "RKN", Title: "p"},
		{Type: domain.TypeEpic, ParentID: "RKN", Title: "e"},
	} {
		if _, err := usecase.Add(context.Background(), d, in); err != nil {
			t.Fatal(err)
		}
	}

	out, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeStory,
		ParentID: "RKN-E01",
		Title:    "First story",
	})
	if err != nil {
		t.Fatalf("Add story: %v", err)
	}
	if out.Item.ID != "RKN-E01-S01" {
		t.Errorf("ID = %q, want RKN-E01-S01", out.Item.ID)
	}
}

func TestAdd_Subtask_RequiresStoryParent(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	for _, in := range []usecase.AddInput{
		{Type: domain.TypeProject, ProjectCode: "RKN", Title: "p"},
		{Type: domain.TypeEpic, ParentID: "RKN", Title: "e"},
		{Type: domain.TypeStory, ParentID: "RKN-E01", Title: "s"},
	} {
		if _, err := usecase.Add(context.Background(), d, in); err != nil {
			t.Fatal(err)
		}
	}

	out, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeSubtask,
		ParentID: "RKN-E01-S01",
		Title:    "subtask",
	})
	if err != nil {
		t.Fatalf("Add subtask: %v", err)
	}
	if out.Item.ID != "RKN-E01-S01-T01" {
		t.Errorf("ID = %q, want RKN-E01-S01-T01", out.Item.ID)
	}
}

func TestAdd_RejectsTypeMismatch(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}

	// Story under a project (skipping epic) — invalid.
	_, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeStory,
		ParentID: "RKN",
		Title:    "wrong-parent",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err=%v, want ErrUsage", err)
	}
}

func TestAdd_RejectsMissingParent(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	_, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:     domain.TypeEpic,
		ParentID: "RKN",
		Title:    "no parent",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestAdd_PopulatesOptionalFields(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	due := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	out, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type:                 domain.TypeProject,
		ProjectCode:          "RKN",
		Title:                "p",
		Description:          "desc",
		Kind:                 domain.KindFeature,
		Priority:             domain.PriorityHigh,
		Tags:                 []string{"a", "b"},
		AcceptanceCriteria:   []string{"x", "y"},
		Dependencies:         []string{"OTHER"},
		RequiredCapabilities: []string{"go"},
		EstimatedHours:       4.5,
		DueDate:              &due,
		Agent:                "alice",
		AgentRole:            domain.RolePlanner,
	})
	if err != nil {
		t.Fatal(err)
	}
	it := out.Item
	if it.Description != "desc" || it.Kind != domain.KindFeature || it.Priority != domain.PriorityHigh {
		t.Errorf("fields: %+v", it)
	}
	if it.Impact.EstimatedHours != 4.5 {
		t.Errorf("EstimatedHours: %v", it.Impact.EstimatedHours)
	}
	if it.CreatedBy != "alice" || it.CreatedByRole != domain.RolePlanner {
		t.Errorf("creator: %+v", it)
	}
	if it.DueDate == nil || !it.DueDate.Equal(due) {
		t.Errorf("DueDate: %v", it.DueDate)
	}
	if len(it.Tags) != 2 || len(it.AcceptanceCriteria) != 2 {
		t.Errorf("slice fields: %+v", it)
	}
}

func TestAdd_RejectsBadProjectCode(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	for _, code := range []string{"", "rkn", "rk", "TOOLONG7"} {
		_, err := usecase.Add(context.Background(), d, usecase.AddInput{
			Type:        domain.TypeProject,
			ProjectCode: code,
			Title:       "x",
		})
		if !errors.Is(err, domain.ErrUsage) {
			t.Errorf("ProjectCode=%q err=%v, want ErrUsage", code, err)
		}
	}
}

func TestAdd_RejectsDuplicateProjectCode(t *testing.T) {
	t.Parallel()
	d, _, _ := newAddDeps(t)
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "dup",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("duplicate err=%v, want ErrUsage", err)
	}
}
