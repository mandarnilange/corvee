package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestPruneStaleAliases_DropsDeadAliases(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC))
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	live := domain.Item{
		ID: "ABC", Type: domain.TypeProject, Title: "x", Version: 1,
		Aliases:   []string{"OLD-DEAD", "ABC-OTHER"},
		UpdatedAt: old,
	}
	other := domain.Item{ID: "ABC-OTHER", Type: domain.TypeEpic, Title: "e", Version: 1, UpdatedAt: old}
	if _, err := store.Put(context.Background(), live, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), other, -1); err != nil {
		t.Fatal(err)
	}

	d := Deps{Store: store, Clock: clock}
	count, err := pruneStaleAliases(context.Background(), d, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("pruneStaleAliases: %v", err)
	}
	if count != 1 {
		t.Errorf("pruned = %d, want 1", count)
	}
	got, _ := store.Get(context.Background(), "ABC")
	if len(got.Aliases) != 1 || got.Aliases[0] != "ABC-OTHER" {
		t.Errorf("aliases after prune = %v, want [ABC-OTHER]", got.Aliases)
	}
}

func TestResolveCutoff_ParsesISO(t *testing.T) {
	t.Parallel()
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	got, err := resolveCutoff(d, "2026-01-15")
	if err != nil {
		t.Fatalf("resolveCutoff: %v", err)
	}
	if got.Year() != 2026 || got.Month() != 1 || got.Day() != 15 {
		t.Errorf("got %v, want 2026-01-15", got)
	}
}

func TestResolveCutoff_RejectsGarbage(t *testing.T) {
	t.Parallel()
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	_, err := resolveCutoff(d, "not-a-date")
	if err == nil {
		t.Fatal("expected ErrUsage")
	}
}
