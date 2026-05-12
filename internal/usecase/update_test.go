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

func newUpdateDeps(t *testing.T) (usecase.Deps, *testfakes.Store, *testfakes.Clock) {
	t.Helper()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}

	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	return d, store, clock
}

func ptrStr(s string) *string                   { return &s }
func ptrStatus(s domain.Status) *domain.Status  { return &s }
func ptrPri(p domain.Priority) *domain.Priority { return &p }

func TestUpdate_TitleAndPriority(t *testing.T) {
	t.Parallel()
	d, _, clock := newUpdateDeps(t)
	clock.Set(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC))

	out, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 1,
		Title:         ptrStr("Updated"),
		Priority:      ptrPri(domain.PriorityHigh),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Item.Title != "Updated" || out.Item.Priority != domain.PriorityHigh {
		t.Errorf("fields: %+v", out.Item)
	}
	if out.PrevVersion != 1 || out.NewVersion != 2 {
		t.Errorf("versions: prev=%d new=%d", out.PrevVersion, out.NewVersion)
	}
	if !out.Item.UpdatedAt.Equal(time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("UpdatedAt = %v", out.Item.UpdatedAt)
	}
}

func TestUpdate_StatusValidTransition(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	// Project starts at backlog. backlog → ready is legal.
	out, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 1,
		Status:        ptrStatus(domain.StatusReady),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if out.Item.Status != domain.StatusReady {
		t.Errorf("Status: %v", out.Item.Status)
	}
}

func TestUpdate_StatusIllegalTransitionRejected(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	_, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 1,
		Status:        ptrStatus(domain.StatusInProgress), // skip ready/claimed
	})
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Errorf("err=%v, want ErrInvalidStatus", err)
	}
}

func TestUpdate_TagsAddRemoveDedup(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 1, AddTags: []string{"a", "b"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 2, AddTags: []string{"b", "c"}, // 'b' already present
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: "RKN", ExpectVersion: 3, RemoveTags: []string{"a"},
	}); err != nil {
		t.Fatal(err)
	}
	out, _ := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	want := []string{"b", "c"}
	if len(out.Item.Tags) != 2 || out.Item.Tags[0] != want[0] || out.Item.Tags[1] != want[1] {
		t.Errorf("tags=%v, want %v", out.Item.Tags, want)
	}
}

func TestUpdate_VersionConflictPropagates(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	_, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 99, // wrong
		Title:         ptrStr("won't land"),
	})
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("err=%v, want ErrVersionConflict", err)
	}
}

func TestUpdate_NoteAppendsJournalEntry(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 1,
		Note:          "tweaked priority",
		Agent:         "alice",
	}); err != nil {
		t.Fatal(err)
	}
	out, _ := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	if len(out.Item.Journal) != 1 || out.Item.Journal[0].Note != "tweaked priority" || out.Item.Journal[0].Actor != "alice" {
		t.Errorf("journal=%+v", out.Item.Journal)
	}
}

func TestUpdate_MissingIDReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	_, err := usecase.Update(context.Background(), d, usecase.UpdateInput{ID: "RKN"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

func TestUpdate_DescriptionAndDueDate(t *testing.T) {
	t.Parallel()
	d, _, _ := newUpdateDeps(t)
	due := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	if _, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID:            "RKN",
		ExpectVersion: 1,
		Description:   ptrStr("new description"),
		DueDate:       &due,
	}); err != nil {
		t.Fatal(err)
	}
	out, _ := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	if out.Item.Description != "new description" {
		t.Errorf("desc=%q", out.Item.Description)
	}
	if out.Item.DueDate == nil || !out.Item.DueDate.Equal(due) {
		t.Errorf("due=%v", out.Item.DueDate)
	}
}
