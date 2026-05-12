package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ShowInput is the request payload for Show.
type ShowInput struct {
	// ID identifies the item to retrieve.
	ID string
}

// ShowOutput is the response payload for Show.
type ShowOutput struct {
	// Item is the full record including journal.
	Item domain.Item `json:"item"`
}

// Show retrieves a single item by id. Returns wrapped ErrUsage when
// id is empty and ErrNotFound when no such item exists. Falls back to
// alias lookup so a caller holding a pre-Move/Rename id still resolves
// to the canonical item.
func Show(ctx context.Context, d Deps, in ShowInput) (ShowOutput, error) {
	if in.ID == "" {
		return ShowOutput{}, fmt.Errorf("show: id is required: %w", domain.ErrUsage)
	}
	item, err := resolveItem(ctx, d.Store, in.ID)
	if err != nil {
		return ShowOutput{}, err
	}
	return ShowOutput{Item: item}, nil
}
