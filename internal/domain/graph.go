package domain

import (
	"fmt"
	"sort"
)

// Graph is the in-memory adjacency view of a workspace's items. It
// answers "what are the children of X?", "what does X depend on?",
// and "is the dependency graph acyclic?" without a Store round-trip.
//
// Per spec §6 / §16.4 the Graph is computed from a snapshot of items
// and is *not* the source of truth — the per-item JSON files are.
// Callers rebuild a Graph after any mutation that touches parent_id,
// dependencies, or membership.
//
// The zero Graph is empty and safe to query.
type Graph struct {
	items    map[string]Item
	children map[string][]string
	deps     map[string][]string
}

// NewGraph builds a Graph from items. Children and dependency lists
// are sorted by ID for determinism. Items whose ParentID points at a
// non-present parent are still indexed (so callers can detect orphans
// via Validate); the dangling reference simply does not appear in any
// children slice.
func NewGraph(items []Item) Graph {
	g := Graph{
		items:    make(map[string]Item, len(items)),
		children: make(map[string][]string),
		deps:     make(map[string][]string),
	}
	for _, it := range items {
		g.items[it.ID] = it
	}
	for _, it := range items {
		if it.ParentID != "" {
			if _, ok := g.items[it.ParentID]; ok {
				g.children[it.ParentID] = append(g.children[it.ParentID], it.ID)
			}
		}
		if len(it.Dependencies) > 0 {
			depCopy := make([]string, len(it.Dependencies))
			copy(depCopy, it.Dependencies)
			sort.Strings(depCopy)
			g.deps[it.ID] = depCopy
		}
	}
	for parent := range g.children {
		sort.Strings(g.children[parent])
	}
	return g
}

// Item returns the item with the given id and a found flag.
func (g Graph) Item(id string) (Item, bool) {
	it, ok := g.items[id]
	return it, ok
}

// Items returns every item in the graph, sorted by ID. The returned
// slice is freshly allocated and safe for the caller to mutate.
func (g Graph) Items() []Item {
	out := make([]Item, 0, len(g.items))
	for _, it := range g.items {
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Children returns the IDs of items whose ParentID == parentID, sorted
// by ID. Returns nil for unknown parents or leaves.
func (g Graph) Children(parentID string) []string {
	c := g.children[parentID]
	if len(c) == 0 {
		return nil
	}
	out := make([]string, len(c))
	copy(out, c)
	return out
}

// Dependencies returns the IDs id depends on, sorted by ID. Returns
// nil if id has no declared dependencies.
func (g Graph) Dependencies(id string) []string {
	d := g.deps[id]
	if len(d) == 0 {
		return nil
	}
	out := make([]string, len(d))
	copy(out, d)
	return out
}

// Roots returns every item whose ParentID is empty (i.e. projects),
// sorted by ID.
func (g Graph) Roots() []string {
	out := make([]string, 0)
	for id, it := range g.items {
		if it.ParentID == "" {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// DetectCycle searches the dependency graph for a cycle. On success
// returns nil; on failure returns ErrCycleDetected wrapped with the
// detected cycle as a chain of IDs (a, b, c, a) for diagnostics.
//
// Algorithm: iterative DFS with a recursion-stack set. Uses a
// deterministic visit order (sorted IDs) so the reported cycle is
// stable across runs — important for golden tests and for property
// tests that shrink to a minimal cycle.
func (g Graph) DetectCycle() error {
	const (
		white = 0 // unvisited
		gray  = 1 // on the recursion stack
		black = 2 // fully explored
	)
	color := make(map[string]int, len(g.items))
	parent := make(map[string]string, len(g.items))

	ids := make([]string, 0, len(g.items))
	for id := range g.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, root := range ids {
		if color[root] != white {
			continue
		}
		// Iterative DFS using an explicit stack.
		type frame struct {
			id   string
			next int
		}
		stack := []frame{{id: root}}
		color[root] = gray
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			children := g.deps[top.id]
			if top.next >= len(children) {
				color[top.id] = black
				stack = stack[:len(stack)-1]
				continue
			}
			child := children[top.next]
			top.next++
			// Skip dependencies on items we don't know about — those
			// are integrity violations surfaced separately by Validate.
			if _, known := g.items[child]; !known {
				continue
			}
			switch color[child] {
			case white:
				parent[child] = top.id
				color[child] = gray
				stack = append(stack, frame{id: child})
			case gray:
				return fmt.Errorf("dependency cycle: %s: %w",
					formatCycle(parent, top.id, child), ErrCycleDetected)
			case black:
				// already explored; no cycle through this branch
			}
		}
	}
	return nil
}

// formatCycle reconstructs the cycle path from the DFS parent map.
// `from` is the gray node we backtrack from; `to` is the gray node we
// just attempted to revisit (the cycle's closing edge points from→to).
func formatCycle(parent map[string]string, from, to string) string {
	chain := []string{from}
	for cur := from; cur != to; {
		p, ok := parent[cur]
		if !ok {
			break
		}
		chain = append(chain, p)
		cur = p
	}
	// chain currently runs from→...→to; reverse and append the closing
	// edge back to `to` so the rendered output reads as a directed loop.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	chain = append(chain, to)
	out := ""
	for i, id := range chain {
		if i > 0 {
			out += " -> "
		}
		out += id
	}
	return out
}

// validParentType returns the ItemType the parent of t must have.
// Returns the empty type for projects (no parent allowed) and for
// invalid types.
func validParentType(t ItemType) ItemType {
	switch t {
	case TypeEpic:
		return TypeProject
	case TypeStory:
		return TypeEpic
	case TypeSubtask:
		return TypeStory
	case TypeProject:
		return ""
	}
	return ""
}

// IntegrityIssue describes one violation found by ValidateIntegrity.
// Code is a short stable identifier suitable for machine routing;
// Message is human-readable detail.
type IntegrityIssue struct {
	// ItemID is the ID of the offending item, when applicable.
	ItemID string `json:"item_id,omitempty"`
	// Code categorizes the violation (orphan|parent_type|dep_missing|
	// cycle|self_dep). Stable across versions so callers can branch.
	Code string `json:"code"`
	// Message is the rendered diagnostic.
	Message string `json:"message"`
}

// Integrity codes.
const (
	// IntegrityOrphan marks an item whose ParentID is non-empty but
	// points at an item not present in the workspace.
	IntegrityOrphan = "orphan"
	// IntegrityParentType marks an item whose parent's Type does not
	// match the structural rules of §15.2.
	IntegrityParentType = "parent_type"
	// IntegrityDepMissing marks a dependency reference that points at
	// an unknown item.
	IntegrityDepMissing = "dep_missing"
	// IntegritySelfDep marks an item that depends on itself.
	IntegritySelfDep = "self_dep"
	// IntegrityCycle marks the presence of a cycle in the dependency
	// graph. The Message includes the cycle path.
	IntegrityCycle = "cycle"
)

// ValidateIntegrity runs all structural checks (orphans, parent types,
// dependency references, self-deps, cycles) and returns every issue
// found. An empty result means the workspace is structurally sound.
//
// Determinism: issues are returned sorted by (Code, ItemID) so callers
// can golden-test the output.
func (g Graph) ValidateIntegrity() []IntegrityIssue {
	issues := make([]IntegrityIssue, 0)
	ids := make([]string, 0, len(g.items))
	for id := range g.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		it := g.items[id]
		if it.ParentID != "" {
			parent, ok := g.items[it.ParentID]
			switch {
			case !ok:
				issues = append(issues, IntegrityIssue{
					ItemID:  id,
					Code:    IntegrityOrphan,
					Message: fmt.Sprintf("%s references missing parent %q", id, it.ParentID),
				})
			default:
				expected := validParentType(it.Type)
				if expected != "" && parent.Type != expected {
					issues = append(issues, IntegrityIssue{
						ItemID: id,
						Code:   IntegrityParentType,
						Message: fmt.Sprintf("%s (%s) has parent %s of type %s; expected %s",
							id, it.Type, parent.ID, parent.Type, expected),
					})
				}
			}
		}
		for _, dep := range it.Dependencies {
			if dep == id {
				issues = append(issues, IntegrityIssue{
					ItemID:  id,
					Code:    IntegritySelfDep,
					Message: fmt.Sprintf("%s declares dependency on itself", id),
				})
				continue
			}
			if _, known := g.items[dep]; !known {
				issues = append(issues, IntegrityIssue{
					ItemID:  id,
					Code:    IntegrityDepMissing,
					Message: fmt.Sprintf("%s depends on unknown item %q", id, dep),
				})
			}
		}
	}
	if err := g.DetectCycle(); err != nil {
		issues = append(issues, IntegrityIssue{
			Code:    IntegrityCycle,
			Message: err.Error(),
		})
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Code != issues[j].Code {
			return issues[i].Code < issues[j].Code
		}
		return issues[i].ItemID < issues[j].ItemID
	})
	return issues
}
