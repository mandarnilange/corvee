package usecase

import (
	"context"
	"errors"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// resolveItem returns the canonical Item identified by id. It first
// attempts a direct Store.Get; on ErrNotFound it scans all items for
// one whose Aliases[] contains id and returns that item if found.
//
// The fallback exists because Move and Rename preserve old IDs in
// Aliases[] but the Store keys on canonical ID only. Without this
// helper, every caller that holds a pre-rename reference would have
// to track its own alias index. Lookup is O(items) on the miss path;
// the hit path stays O(1). The miss path is the exception, not the
// hot path — items are usually addressed by canonical ID.
//
// On a miss with no alias match, the original ErrNotFound from
// Store.Get is returned unchanged so callers' errors.Is checks remain
// stable.
func resolveItem(ctx context.Context, store domain.Store, id string) (domain.Item, error) {
	item, err := store.Get(ctx, id)
	if err == nil {
		return item, nil
	}
	if !errors.Is(err, domain.ErrNotFound) {
		return domain.Item{}, err
	}

	all, listErr := store.List(ctx, domain.ListFilter{})
	if listErr != nil {
		return domain.Item{}, fmt.Errorf("resolve %s: alias scan: %w", id, listErr)
	}
	for _, candidate := range all {
		for _, alias := range candidate.Aliases {
			if alias == id {
				return candidate, nil
			}
		}
	}
	return domain.Item{}, err
}
