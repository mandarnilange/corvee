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

func TestShow_ReturnsItem(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}

	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "Rikonect",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	if err != nil {
		t.Fatalf("Show: %v", err)
	}
	if out.Item.ID != "RKN" || out.Item.Title != "Rikonect" {
		t.Fatalf("unexpected: %+v", out.Item)
	}
}

func TestShow_RejectsEmptyID(t *testing.T) {
	t.Parallel()
	d := usecase.Deps{Store: testfakes.NewStore()}
	_, err := usecase.Show(context.Background(), d, usecase.ShowInput{ID: ""})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v, want ErrUsage", err)
	}
}

func TestShow_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	d := usecase.Deps{Store: testfakes.NewStore()}
	_, err := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

func TestShow_ResolvesByAlias(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	now := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)

	// Seed an item whose Aliases[] carries the prior ID it was known by.
	// Models what usecase.Move records when reparenting an item.
	if _, err := store.Put(context.Background(), domain.Item{
		ID:        "RKN-E02-S01",
		Type:      domain.TypeStory,
		ProjectID: "RKN",
		ParentID:  "RKN-E02",
		Title:     "Reparented story",
		Status:    domain.StatusBacklog,
		Version:   2,
		CreatedAt: now,
		UpdatedAt: now,
		Aliases:   []string{"RKN-E01-S03"},
	}, -1); err != nil {
		t.Fatalf("seed: %v", err)
	}

	d := usecase.Deps{Store: store}
	out, err := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN-E01-S03"})
	if err != nil {
		t.Fatalf("Show by alias: %v", err)
	}
	if out.Item.ID != "RKN-E02-S01" {
		t.Errorf("alias resolved to %q, want canonical RKN-E02-S01", out.Item.ID)
	}
	if out.Item.Title != "Reparented story" {
		t.Errorf("title=%q, want full canonical payload", out.Item.Title)
	}
}
