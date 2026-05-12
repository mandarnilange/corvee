package domain

import "testing"

func TestGraph_Item_FoundFlag(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "A", Type: TypeStory}}
	g := NewGraph(items)
	if it, ok := g.Item("A"); !ok || it.ID != "A" {
		t.Errorf("expected A found, got %+v ok=%v", it, ok)
	}
	if _, ok := g.Item("missing"); ok {
		t.Errorf("expected missing not found")
	}
}

func TestGraph_Items_SortedByID(t *testing.T) {
	t.Parallel()
	items := []Item{{ID: "B"}, {ID: "A"}, {ID: "C"}}
	out := NewGraph(items).Items()
	if len(out) != 3 || out[0].ID != "A" || out[1].ID != "B" || out[2].ID != "C" {
		t.Errorf("not sorted: %+v", out)
	}
}

func TestValidParentType_AllBranches(t *testing.T) {
	t.Parallel()
	cases := map[ItemType]ItemType{
		TypeEpic:            TypeProject,
		TypeStory:           TypeEpic,
		TypeSubtask:         TypeStory,
		TypeProject:         "",
		ItemType("garbage"): "",
	}
	for in, want := range cases {
		if got := validParentType(in); got != want {
			t.Errorf("validParentType(%q) = %q, want %q", in, got, want)
		}
	}
}
