package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ListInput is the request payload for List.
type ListInput struct {
	// Filter is the AND-combined filter applied by Store.List.
	Filter domain.ListFilter
}

// ListOutput is the response payload for List.
type ListOutput struct {
	// Items is the filtered, sorted, limited result.
	Items []domain.Item `json:"items"`
}

// List returns items matching in.Filter. Phase 1 delegates entirely
// to Store.List (which delegates to ListFilter.Apply); usecase exists
// so future phases can layer cross-cutting concerns (capability
// matching with the agent's local config, audit, etc.).
func List(ctx context.Context, d Deps, in ListInput) (ListOutput, error) {
	items, err := d.Store.List(ctx, in.Filter)
	if err != nil {
		return ListOutput{}, fmt.Errorf("list: %w", err)
	}
	return ListOutput{Items: items}, nil
}
