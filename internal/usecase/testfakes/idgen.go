package testfakes

import (
	"fmt"
	"sync/atomic"
)

// IDGen is a deterministic IDGen fake. ULID() returns sequenced
// strings of the form "<prefix>-<n>" starting at 1, monotonically.
type IDGen struct {
	prefix string
	n      atomic.Uint64
}

// NewIDGen returns a generator that emits prefix-1, prefix-2, ...
func NewIDGen(prefix string) *IDGen {
	return &IDGen{prefix: prefix}
}

// ULID emits the next sequenced identifier.
func (g *IDGen) ULID() string {
	n := g.n.Add(1)
	return fmt.Sprintf("%s-%d", g.prefix, n)
}
