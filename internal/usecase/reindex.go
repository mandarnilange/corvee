package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ReindexInput is the request payload for Reindex.
type ReindexInput struct{}

// ReindexOutput reports the size of the rebuilt cache.
type ReindexOutput struct {
	// ItemCount is the number of entries in the new index.
	ItemCount int `json:"item_count"`
}

// Reindex rebuilds .tasks/index.json from every item the Store
// knows about. The cache is gitignored — callers may run Reindex at
// any time to repair drift.
func Reindex(ctx context.Context, d Deps, _ ReindexInput) (ReindexOutput, error) {
	if d.Indexer == nil {
		return ReindexOutput{}, fmt.Errorf("reindex: indexer is nil: %w", domain.ErrUsage)
	}
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return ReindexOutput{}, fmt.Errorf("reindex: list: %w", err)
	}
	entries := make([]domain.IndexEntry, 0, len(items))
	idx := domain.Index{
		SchemaVersion: domain.CurrentSchemaVersion,
		BuiltAt:       d.Clock.Now(),
		ItemCount:     len(items),
	}
	for _, it := range items {
		entries = append(entries, domain.IndexEntryFromItem(it))
		if it.UpdatedAt.After(idx.MaxItemUpdatedAt) {
			idx.MaxItemUpdatedAt = it.UpdatedAt
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	idx.Items = entries

	if err := d.Indexer.Save(idx); err != nil {
		return ReindexOutput{}, fmt.Errorf("reindex: save: %w", err)
	}
	return ReindexOutput{ItemCount: len(items)}, nil
}
