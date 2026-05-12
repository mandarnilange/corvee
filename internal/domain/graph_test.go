package domain

import (
	"errors"
	"strings"
	"testing"
)

func graphItem(id, parent string, t ItemType, deps ...string) Item {
	return Item{ID: id, ParentID: parent, Type: t, Dependencies: deps}
}

func TestGraph_ChildrenAndDeps_PopulatedFromSnapshot(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("RKN", "", TypeProject),
		graphItem("RKN-E01", "RKN", TypeEpic),
		graphItem("RKN-E02", "RKN", TypeEpic),
		graphItem("RKN-E01-S01", "RKN-E01", TypeStory),
		graphItem("RKN-E01-S02", "RKN-E01", TypeStory, "RKN-E01-S01"),
	}
	g := NewGraph(items)

	if got := g.Children("RKN"); len(got) != 2 || got[0] != "RKN-E01" || got[1] != "RKN-E02" {
		t.Errorf("RKN children = %v, want [RKN-E01 RKN-E02]", got)
	}
	if got := g.Children("RKN-E01"); len(got) != 2 || got[0] != "RKN-E01-S01" {
		t.Errorf("epic children = %v", got)
	}
	if got := g.Dependencies("RKN-E01-S02"); len(got) != 1 || got[0] != "RKN-E01-S01" {
		t.Errorf("S02 deps = %v", got)
	}
	if got := g.Children("missing"); got != nil {
		t.Errorf("missing parent should return nil children, got %v", got)
	}
}

func TestGraph_Roots_OnlyParentless(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("AAA", "", TypeProject),
		graphItem("BBB", "", TypeProject),
		graphItem("AAA-E01", "AAA", TypeEpic),
	}
	roots := NewGraph(items).Roots()
	if len(roots) != 2 || roots[0] != "AAA" || roots[1] != "BBB" {
		t.Errorf("roots = %v, want [AAA BBB]", roots)
	}
}

func TestGraph_DetectCycle_Acyclic(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("RKN-E01-S01", "", TypeStory),
		graphItem("RKN-E01-S02", "", TypeStory, "RKN-E01-S01"),
		graphItem("RKN-E01-S03", "", TypeStory, "RKN-E01-S02"),
	}
	if err := NewGraph(items).DetectCycle(); err != nil {
		t.Errorf("DetectCycle returned %v on acyclic graph", err)
	}
}

func TestGraph_DetectCycle_DirectLoop(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("A", "", TypeStory, "B"),
		graphItem("B", "", TypeStory, "A"),
	}
	err := NewGraph(items).DetectCycle()
	if err == nil {
		t.Fatal("expected cycle error")
	}
	if !errors.Is(err, ErrCycleDetected) {
		t.Errorf("error should wrap ErrCycleDetected: %v", err)
	}
	if !strings.Contains(err.Error(), "A") || !strings.Contains(err.Error(), "B") {
		t.Errorf("cycle error should name nodes: %v", err)
	}
}

func TestGraph_DetectCycle_LongerLoop(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("A", "", TypeStory, "B"),
		graphItem("B", "", TypeStory, "C"),
		graphItem("C", "", TypeStory, "A"),
	}
	if err := NewGraph(items).DetectCycle(); err == nil {
		t.Fatal("expected cycle error in 3-node loop")
	}
}

func TestGraph_DetectCycle_IgnoresUnknownDeps(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("A", "", TypeStory, "ghost"),
	}
	if err := NewGraph(items).DetectCycle(); err != nil {
		t.Errorf("unknown dep should not be a cycle, got %v", err)
	}
}

func TestGraph_DetectCycle_DeterministicLoopReport(t *testing.T) {
	t.Parallel()
	// Two structurally-equivalent runs must produce the same error
	// string — this guards the property that cycle detection visits in
	// sorted order.
	items := []Item{
		graphItem("Z", "", TypeStory, "Y"),
		graphItem("Y", "", TypeStory, "Z"),
	}
	err1 := NewGraph(items).DetectCycle()
	err2 := NewGraph(items).DetectCycle()
	if err1.Error() != err2.Error() {
		t.Errorf("cycle reports differ between runs:\n%v\n%v", err1, err2)
	}
}

func TestGraph_ValidateIntegrity_Clean(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("RKN", "", TypeProject),
		graphItem("RKN-E01", "RKN", TypeEpic),
		graphItem("RKN-E01-S01", "RKN-E01", TypeStory),
	}
	if issues := NewGraph(items).ValidateIntegrity(); len(issues) != 0 {
		t.Errorf("clean graph produced issues: %+v", issues)
	}
}

func TestGraph_ValidateIntegrity_Orphan(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("RKN-E01-S01", "ghost", TypeStory),
	}
	issues := NewGraph(items).ValidateIntegrity()
	if len(issues) == 0 || issues[0].Code != IntegrityOrphan {
		t.Errorf("expected orphan issue, got %+v", issues)
	}
}

func TestGraph_ValidateIntegrity_ParentType(t *testing.T) {
	t.Parallel()
	// Story whose parent is a project (should be epic) — type mismatch.
	items := []Item{
		graphItem("RKN", "", TypeProject),
		graphItem("RKN-S99", "RKN", TypeStory),
	}
	issues := NewGraph(items).ValidateIntegrity()
	if len(issues) == 0 || issues[0].Code != IntegrityParentType {
		t.Errorf("expected parent_type issue, got %+v", issues)
	}
}

func TestGraph_ValidateIntegrity_SelfDependency(t *testing.T) {
	t.Parallel()
	items := []Item{graphItem("A", "", TypeStory, "A")}
	issues := NewGraph(items).ValidateIntegrity()
	found := false
	for _, is := range issues {
		if is.Code == IntegritySelfDep {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected self_dep issue, got %+v", issues)
	}
}

func TestGraph_ValidateIntegrity_MissingDep(t *testing.T) {
	t.Parallel()
	items := []Item{graphItem("A", "", TypeStory, "missing")}
	issues := NewGraph(items).ValidateIntegrity()
	if len(issues) == 0 || issues[0].Code != IntegrityDepMissing {
		t.Errorf("expected dep_missing issue, got %+v", issues)
	}
}

func TestGraph_ValidateIntegrity_Cycle(t *testing.T) {
	t.Parallel()
	items := []Item{
		graphItem("A", "", TypeStory, "B"),
		graphItem("B", "", TypeStory, "A"),
	}
	issues := NewGraph(items).ValidateIntegrity()
	found := false
	for _, is := range issues {
		if is.Code == IntegrityCycle {
			found = true
		}
	}
	if !found {
		t.Errorf("expected cycle issue, got %+v", issues)
	}
}
