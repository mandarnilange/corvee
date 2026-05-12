package domain

import (
	"testing"
	"time"
)

func mkItem(id string, opts ...func(*Item)) Item {
	t := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	it := Item{
		SchemaVersion: 1,
		ID:            id,
		Type:          TypeStory,
		Title:         id,
		Status:        StatusBacklog,
		CreatedAt:     t,
		UpdatedAt:     t,
		Version:       1,
	}
	for _, o := range opts {
		o(&it)
	}
	return it
}

func ids(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func eqIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestListFilter_Apply_EmptyFilterReturnsAllSortedByID(t *testing.T) {
	t.Parallel()
	in := []Item{mkItem("C"), mkItem("A"), mkItem("B")}
	out := ListFilter{}.Apply(in)
	if !eqIDs(ids(out), []string{"A", "B", "C"}) {
		t.Fatalf("got %v", ids(out))
	}
}

func TestListFilter_Apply_StatusKindTypeFilters(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("A", func(i *Item) { i.Status = StatusReady; i.Kind = KindFeature; i.Type = TypeStory }),
		mkItem("B", func(i *Item) { i.Status = StatusDone; i.Kind = KindBug; i.Type = TypeStory }),
		mkItem("C", func(i *Item) { i.Status = StatusReady; i.Kind = KindBug; i.Type = TypeEpic }),
	}
	out := ListFilter{Statuses: []Status{StatusReady}}.Apply(in)
	if !eqIDs(ids(out), []string{"A", "C"}) {
		t.Errorf("statuses: got %v", ids(out))
	}
	out = ListFilter{Kinds: []Kind{KindBug}}.Apply(in)
	if !eqIDs(ids(out), []string{"B", "C"}) {
		t.Errorf("kinds: got %v", ids(out))
	}
	out = ListFilter{Types: []ItemType{TypeEpic}}.Apply(in)
	if !eqIDs(ids(out), []string{"C"}) {
		t.Errorf("types: got %v", ids(out))
	}
}

func TestListFilter_Apply_ProjectAndParent(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("RKN", func(i *Item) { i.ProjectID = "RKN"; i.Type = TypeProject }),
		mkItem("RKN-E01", func(i *Item) { i.ProjectID = "RKN"; i.ParentID = "RKN"; i.Type = TypeEpic }),
		mkItem("KAH", func(i *Item) { i.ProjectID = "KAH"; i.Type = TypeProject }),
	}
	out := ListFilter{ProjectID: "RKN"}.Apply(in)
	if !eqIDs(ids(out), []string{"RKN", "RKN-E01"}) {
		t.Errorf("project: got %v", ids(out))
	}
	out = ListFilter{ParentID: "RKN"}.Apply(in)
	if !eqIDs(ids(out), []string{"RKN-E01"}) {
		t.Errorf("parent: got %v", ids(out))
	}
}

func TestListFilter_Apply_AssigneeAndUnassigned(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("A", func(i *Item) { i.Claim = &Claim{Agent: "alice", LeaseID: "x"} }),
		mkItem("B", func(i *Item) { i.Claim = &Claim{Agent: "bob", LeaseID: "y"} }),
		mkItem("C"),
	}
	out := ListFilter{Assignee: "alice"}.Apply(in)
	if !eqIDs(ids(out), []string{"A"}) {
		t.Errorf("assignee: got %v", ids(out))
	}
	out = ListFilter{Unassigned: true}.Apply(in)
	if !eqIDs(ids(out), []string{"C"}) {
		t.Errorf("unassigned: got %v", ids(out))
	}
}

func TestListFilter_Apply_TagsAndCapabilities(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("A", func(i *Item) { i.Tags = []string{"ml", "nlp"}; i.RequiredCapabilities = []string{"go"} }),
		mkItem("B", func(i *Item) { i.Tags = []string{"ml"}; i.RequiredCapabilities = []string{"python"} }),
		mkItem("C"),
	}
	out := ListFilter{Tags: []string{"ml", "nlp"}}.Apply(in)
	if !eqIDs(ids(out), []string{"A"}) {
		t.Errorf("tags: got %v", ids(out))
	}
	out = ListFilter{Capabilities: []string{"go"}}.Apply(in)
	if !eqIDs(ids(out), []string{"A", "C"}) {
		t.Errorf("caps=[go]: got %v", ids(out))
	}
	out = ListFilter{Capabilities: []string{"go", "python"}}.Apply(in)
	if !eqIDs(ids(out), []string{"A", "B", "C"}) {
		t.Errorf("caps=[go,python]: got %v", ids(out))
	}
}

func TestListFilter_Apply_Unblocked(t *testing.T) {
	t.Parallel()
	done := mkItem("DONE", func(i *Item) { i.Status = StatusDone })
	dep := mkItem("DEP", func(i *Item) { i.Status = StatusInProgress })
	a := mkItem("A", func(i *Item) { i.Dependencies = []string{"DEP"} })
	b := mkItem("B")
	c := mkItem("C", func(i *Item) { i.Dependencies = []string{"DONE"} })
	miss := mkItem("MISS", func(i *Item) { i.Dependencies = []string{"GHOST"} }) // unknown dep → blocked

	in := []Item{done, dep, a, b, c, miss}
	out := ListFilter{Unblocked: true}.Apply(in)
	want := []string{"B", "C", "DEP", "DONE"}
	if !eqIDs(ids(out), want) {
		t.Errorf("unblocked: got %v, want %v", ids(out), want)
	}
}

func TestListFilter_Apply_Limit(t *testing.T) {
	t.Parallel()
	in := []Item{mkItem("A"), mkItem("B"), mkItem("C")}
	out := ListFilter{Limit: 2}.Apply(in)
	if len(out) != 2 {
		t.Fatalf("limit: got %d, want 2", len(out))
	}
}

func TestListFilter_Apply_SortByPriority(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("L", func(i *Item) { i.Priority = PriorityLow }),
		mkItem("H", func(i *Item) { i.Priority = PriorityHigh }),
		mkItem("C", func(i *Item) { i.Priority = PriorityCritical }),
		mkItem("M", func(i *Item) { i.Priority = PriorityMedium }),
		mkItem("X"), // no priority — should sort last
	}
	out := ListFilter{Sort: "priority"}.Apply(in)
	if !eqIDs(ids(out), []string{"C", "H", "M", "L", "X"}) {
		t.Errorf("priority asc: got %v", ids(out))
	}
	out = ListFilter{Sort: "priority", Order: "desc"}.Apply(in)
	if !eqIDs(ids(out), []string{"X", "L", "M", "H", "C"}) {
		t.Errorf("priority desc: got %v", ids(out))
	}
}

func TestListFilter_Apply_SortByCreatedUpdatedAt(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("old", func(i *Item) { i.CreatedAt = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC); i.UpdatedAt = i.CreatedAt }),
		mkItem("new", func(i *Item) { i.CreatedAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC); i.UpdatedAt = i.CreatedAt }),
		mkItem("mid", func(i *Item) { i.CreatedAt = time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC); i.UpdatedAt = i.CreatedAt }),
	}
	out := ListFilter{Sort: "created_at"}.Apply(in)
	if !eqIDs(ids(out), []string{"old", "mid", "new"}) {
		t.Errorf("created_at: got %v", ids(out))
	}
	out = ListFilter{Sort: "updated_at", Order: "desc"}.Apply(in)
	if !eqIDs(ids(out), []string{"new", "mid", "old"}) {
		t.Errorf("updated_at desc: got %v", ids(out))
	}
}

func TestListFilter_Apply_SortByDueDate(t *testing.T) {
	t.Parallel()
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	in := []Item{
		mkItem("late", func(i *Item) { i.DueDate = &d2 }),
		mkItem("early", func(i *Item) { i.DueDate = &d1 }),
		mkItem("none"),
	}
	out := ListFilter{Sort: "due_date"}.Apply(in)
	if !eqIDs(ids(out), []string{"early", "late", "none"}) {
		t.Errorf("due_date asc: got %v", ids(out))
	}
	// Tie when both nil → ID asc.
	in2 := []Item{mkItem("Z"), mkItem("A")}
	out = ListFilter{Sort: "due_date"}.Apply(in2)
	if !eqIDs(ids(out), []string{"A", "Z"}) {
		t.Errorf("due_date both-nil tie: got %v", ids(out))
	}
}

func TestListFilter_Apply_PriorityTieBreakByID(t *testing.T) {
	t.Parallel()
	in := []Item{
		mkItem("Z", func(i *Item) { i.Priority = PriorityHigh }),
		mkItem("A", func(i *Item) { i.Priority = PriorityHigh }),
		mkItem("M", func(i *Item) { i.Priority = PriorityHigh }),
	}
	out := ListFilter{Sort: "priority"}.Apply(in)
	if !eqIDs(ids(out), []string{"A", "M", "Z"}) {
		t.Errorf("tie: got %v", ids(out))
	}
}
