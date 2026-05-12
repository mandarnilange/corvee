package testfakes

import (
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Indexer is an in-memory fake of domain.Indexer. Tests inspect Saved
// directly to assert what the cache rebuild produced.
type Indexer struct {
	mu      sync.Mutex
	Saved   domain.Index
	Present bool
	// StaleResult lets tests force Stale() to return a specific value.
	// nil means "compute from Present and SavedItemCount" (defaults to
	// Present?false:true).
	StaleResult *bool
}

// NewIndexer returns an empty Indexer fake.
func NewIndexer() *Indexer { return &Indexer{} }

// Load returns the saved index or wraps ErrNotFound when none exists.
func (i *Indexer) Load() (domain.Index, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.Present {
		return domain.Index{}, fmt.Errorf("index missing: %w", domain.ErrNotFound)
	}
	return i.Saved, nil
}

// Save records idx verbatim.
func (i *Indexer) Save(idx domain.Index) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.Saved = idx
	i.Present = true
	return nil
}

// Stale returns the configured StaleResult or, when nil, !Present.
func (i *Indexer) Stale() (bool, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.StaleResult != nil {
		return *i.StaleResult, nil
	}
	return !i.Present, nil
}
