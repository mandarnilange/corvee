package testfakes

import (
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestIndexer_LoadMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	idx := NewIndexer()
	_, err := idx.Load()
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestIndexer_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	idx := NewIndexer()
	want := domain.Index{SchemaVersion: 1, ItemCount: 3}
	if err := idx.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := idx.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.ItemCount != 3 {
		t.Errorf("got %+v", got)
	}
}

func TestIndexer_StaleDefaultsBasedOnPresence(t *testing.T) {
	t.Parallel()
	idx := NewIndexer()
	stale, _ := idx.Stale()
	if !stale {
		t.Error("missing index should be stale")
	}
	_ = idx.Save(domain.Index{})
	stale, _ = idx.Stale()
	if stale {
		t.Error("present index should not be stale by default")
	}
	override := true
	idx.StaleResult = &override
	stale, _ = idx.Stale()
	if !stale {
		t.Error("StaleResult override ignored")
	}
}
