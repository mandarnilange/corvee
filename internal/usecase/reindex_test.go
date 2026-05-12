package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestReindex_RebuildsCacheFromStore(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	indexer := testfakes.NewIndexer()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))

	t1 := time.Date(2026, 5, 5, 11, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 5, 11, 30, 0, 0, time.UTC)
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, UpdatedAt: t1, Version: 1},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", UpdatedAt: t2, Version: 1},
	)

	out, err := Reindex(context.Background(), Deps{Store: store, Indexer: indexer, Clock: clock}, ReindexInput{})
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if out.ItemCount != 2 {
		t.Errorf("ItemCount = %d, want 2", out.ItemCount)
	}
	if !indexer.Present {
		t.Fatal("indexer.Save was not called")
	}
	if indexer.Saved.ItemCount != 2 || len(indexer.Saved.Items) != 2 {
		t.Errorf("saved index: %+v", indexer.Saved)
	}
	if !indexer.Saved.MaxItemUpdatedAt.Equal(t2) {
		t.Errorf("MaxItemUpdatedAt = %v, want %v", indexer.Saved.MaxItemUpdatedAt, t2)
	}
	if indexer.Saved.Items[0].ID != "RKN" || indexer.Saved.Items[1].ID != "RKN-E01" {
		t.Errorf("entries not sorted: %+v", indexer.Saved.Items)
	}
}
