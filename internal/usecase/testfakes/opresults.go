package testfakes

import (
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// OpResults is an in-memory fake of domain.OpResultStore. Tests can
// inspect Stored directly when they want to assert what idempotency
// outcomes were recorded.
//
// Mirroring the production adapter: Put rejects duplicates with
// domain.ErrOpAlreadyRecorded so usecase tests exercise the same
// O_EXCL-style contract the fsstore-backed oplog provides.
type OpResults struct {
	mu     sync.Mutex
	Stored map[string]string
}

// NewOpResults returns an empty fake.
func NewOpResults() *OpResults { return &OpResults{Stored: map[string]string{}} }

// Get implements domain.OpResultStore.
func (o *OpResults) Get(opID string) (string, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	v, ok := o.Stored[opID]
	return v, ok, nil
}

// Put implements domain.OpResultStore. Rejects same-opID writes with
// domain.ErrOpAlreadyRecorded — matches the production adapter's
// O_EXCL semantics.
func (o *OpResults) Put(opID, resultJSON string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, exists := o.Stored[opID]; exists {
		return fmt.Errorf("opresults(fake): %s: %w", opID, domain.ErrOpAlreadyRecorded)
	}
	o.Stored[opID] = resultJSON
	return nil
}
