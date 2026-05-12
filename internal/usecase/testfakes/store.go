package testfakes

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Store is an in-memory fake of domain.Store. It deep-copies items
// in and out via JSON round-trip so tests cannot accidentally share
// slice/pointer memory with the store. Faithful enough that the
// usecase tests using it do not have to know whether they're talking
// to fsstore or this fake.
type Store struct {
	mu    sync.Mutex
	items map[string]domain.Item
}

// NewStore returns a freshly-initialized Store with no items.
func NewStore() *Store {
	return &Store{items: map[string]domain.Item{}}
}

// Get implements domain.Store. Returns ErrNotFound if id is not present.
func (s *Store) Get(_ context.Context, id string) (domain.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[id]
	if !ok {
		return domain.Item{}, fmt.Errorf("get %s: %w", id, domain.ErrNotFound)
	}
	return copyItem(it)
}

// Exists implements domain.Store. Cheap probe — does not deep-copy.
func (s *Store) Exists(_ context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.items[id]
	return ok, nil
}

// Delete implements domain.Store. Returns ErrNotFound if id is missing.
func (s *Store) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[id]; !ok {
		return fmt.Errorf("delete %s: %w", id, domain.ErrNotFound)
	}
	delete(s.items, id)
	return nil
}

// Put implements domain.Store. expectVersion semantics: -1 means "no
// expectation"; >=0 means "fail with ErrVersionConflict if on-disk
// Version differs (or if no item exists at all)". The caller's Item
// is stored verbatim — Version and UpdatedAt are not mutated.
func (s *Store) Put(_ context.Context, item domain.Item, expectVersion int) (domain.Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if expectVersion >= 0 {
		existing, ok := s.items[item.ID]
		if !ok {
			return domain.Item{}, fmt.Errorf("put %s: expected version %d but item is absent: %w",
				item.ID, expectVersion, domain.ErrVersionConflict)
		}
		if existing.Version != expectVersion {
			return domain.Item{}, fmt.Errorf("put %s: expected version %d, got %d: %w",
				item.ID, expectVersion, existing.Version, domain.ErrVersionConflict)
		}
	}

	stored, err := copyItem(item)
	if err != nil {
		return domain.Item{}, err
	}
	s.items[item.ID] = stored
	return copyItem(stored)
}

// List implements domain.Store. Filtering, sorting, and limit are
// delegated to domain.ListFilter.Apply so the fake stays consistent
// with fsstore.
func (s *Store) List(_ context.Context, filter domain.ListFilter) ([]domain.Item, error) {
	s.mu.Lock()
	snapshot := make([]domain.Item, 0, len(s.items))
	for _, it := range s.items {
		dup, err := copyItem(it)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		snapshot = append(snapshot, dup)
	}
	s.mu.Unlock()

	return filter.Apply(snapshot), nil
}

// copyItem deep-copies an Item via JSON round-trip so callers can
// mutate inputs/outputs without affecting the store.
func copyItem(in domain.Item) (domain.Item, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return domain.Item{}, fmt.Errorf("marshal item %s: %w", in.ID, err)
	}
	var out domain.Item
	if err := json.Unmarshal(data, &out); err != nil {
		return domain.Item{}, fmt.Errorf("unmarshal item %s: %w", in.ID, err)
	}
	return out, nil
}
