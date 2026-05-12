package testfakes

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// helper: builds a minimal valid item suitable for round-trips.
func newItem(id string, opts ...func(*domain.Item)) domain.Item {
	t := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	it := domain.Item{
		SchemaVersion: 1,
		ID:            id,
		Type:          domain.TypeStory,
		Title:         id + " title",
		Status:        domain.StatusBacklog,
		CreatedAt:     t,
		UpdatedAt:     t,
		Version:       1,
	}
	for _, opt := range opts {
		opt(&it)
	}
	return it
}

func TestStore_PutGet_RoundTrip(t *testing.T) {
	t.Parallel()
	s := NewStore()
	in := newItem("RKN-E01-S01", func(i *domain.Item) {
		i.Tags = []string{"a", "b"}
		i.Dependencies = []string{"RKN-E01-S00"}
	})

	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != in.ID || got.Version != in.Version || got.Title != in.Title {
		t.Fatalf("round-trip mismatch:\nin =%+v\ngot=%+v", in, got)
	}
	if len(got.Tags) != 2 || got.Tags[0] != "a" || got.Tags[1] != "b" {
		t.Errorf("tags lost: %v", got.Tags)
	}
}

func TestStore_Put_DoesNotMutateVersionOrUpdatedAt(t *testing.T) {
	t.Parallel()
	s := NewStore()
	stamp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	in := newItem("RKN-E01-S01", func(i *domain.Item) {
		i.Version = 42
		i.UpdatedAt = stamp
	})

	out, err := s.Put(context.Background(), in, -1)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if out.Version != 42 {
		t.Errorf("returned Version = %d, want 42 (store must not mutate)", out.Version)
	}
	if !out.UpdatedAt.Equal(stamp) {
		t.Errorf("returned UpdatedAt = %v, want %v", out.UpdatedAt, stamp)
	}

	got, err := s.Get(context.Background(), in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Version != 42 || !got.UpdatedAt.Equal(stamp) {
		t.Errorf("on-disk Version=%d UpdatedAt=%v, want 42 / %v", got.Version, got.UpdatedAt, stamp)
	}
}

func TestStore_Get_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s := NewStore()
	_, err := s.Get(context.Background(), "RKN")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestStore_Put_ExpectVersionMatchSucceeds(t *testing.T) {
	t.Parallel()
	s := NewStore()
	in := newItem("RKN-E01-S01")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	updated := in
	updated.Version = 2
	updated.Title = "updated"
	if _, err := s.Put(context.Background(), updated, 1); err != nil {
		t.Fatalf("second Put: %v", err)
	}
	got, _ := s.Get(context.Background(), in.ID)
	if got.Version != 2 || got.Title != "updated" {
		t.Fatalf("update failed: %+v", got)
	}
}

func TestStore_Put_ExpectVersionMismatchReturnsErrVersionConflict(t *testing.T) {
	t.Parallel()
	s := NewStore()
	in := newItem("RKN-E01-S01")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatalf("first Put: %v", err)
	}
	stale := in
	stale.Title = "stale"
	_, err := s.Put(context.Background(), stale, 99) // wrong expected version
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("err=%v, want ErrVersionConflict", err)
	}
	// Disk untouched.
	got, _ := s.Get(context.Background(), in.ID)
	if got.Title == "stale" {
		t.Fatalf("disk was modified despite conflict: %+v", got)
	}
}

func TestStore_Put_ExpectVersionAgainstMissingItemReturnsErrVersionConflict(t *testing.T) {
	t.Parallel()
	s := NewStore()
	in := newItem("RKN-E01-S01")
	_, err := s.Put(context.Background(), in, 0) // expects version 0; nothing on disk
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("err=%v, want ErrVersionConflict (expectVersion against missing item)", err)
	}
	if exists, _ := s.Exists(context.Background(), in.ID); exists {
		t.Fatalf("item created despite conflict")
	}
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()
	s := NewStore()
	if err := s.Delete(context.Background(), "ghost"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete missing err=%v, want ErrNotFound", err)
	}
	in := newItem("RKN")
	_, _ = s.Put(context.Background(), in, -1)
	if err := s.Delete(context.Background(), in.ID); err != nil {
		t.Fatalf("Delete existing: %v", err)
	}
	if exists, _ := s.Exists(context.Background(), in.ID); exists {
		t.Errorf("item still exists after Delete")
	}
}

func TestStore_Exists(t *testing.T) {
	t.Parallel()
	s := NewStore()
	if exists, err := s.Exists(context.Background(), "missing"); err != nil || exists {
		t.Errorf("Exists(missing) = (%v, %v); want (false, nil)", exists, err)
	}
	_, _ = s.Put(context.Background(), newItem("RKN"), -1)
	if exists, err := s.Exists(context.Background(), "RKN"); err != nil || !exists {
		t.Errorf("Exists(present) = (%v, %v); want (true, nil)", exists, err)
	}
}

func TestStore_List_EmptyFilterReturnsAll(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for _, id := range []string{"A", "B", "C"} {
		_, _ = s.Put(context.Background(), newItem(id), -1)
	}
	got, err := s.List(context.Background(), domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
}

func TestStore_List_FilterByStatuses(t *testing.T) {
	t.Parallel()
	s := NewStore()
	statuses := []domain.Status{
		domain.StatusBacklog, domain.StatusReady, domain.StatusInProgress, domain.StatusDone,
	}
	for i, st := range statuses {
		it := newItem(string(rune('A' + i)))
		it.Status = st
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, err := s.List(context.Background(), domain.ListFilter{
		Statuses: []domain.Status{domain.StatusReady, domain.StatusInProgress},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
	for _, it := range got {
		if it.Status != domain.StatusReady && it.Status != domain.StatusInProgress {
			t.Errorf("unexpected status %q", it.Status)
		}
	}
}

func TestStore_List_FilterByTypes(t *testing.T) {
	t.Parallel()
	s := NewStore()
	types := []domain.ItemType{domain.TypeProject, domain.TypeEpic, domain.TypeStory, domain.TypeSubtask}
	for i, ty := range types {
		it := newItem(string(rune('A' + i)))
		it.Type = ty
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Types: []domain.ItemType{domain.TypeStory}})
	if len(got) != 1 || got[0].Type != domain.TypeStory {
		t.Fatalf("got=%+v", got)
	}
}

func TestStore_List_FilterByKinds(t *testing.T) {
	t.Parallel()
	s := NewStore()
	a := newItem("A")
	a.Kind = domain.KindBug
	b := newItem("B")
	b.Kind = domain.KindFeature
	_, _ = s.Put(context.Background(), a, -1)
	_, _ = s.Put(context.Background(), b, -1)

	got, _ := s.List(context.Background(), domain.ListFilter{Kinds: []domain.Kind{domain.KindBug}})
	if len(got) != 1 || got[0].ID != "A" {
		t.Fatalf("got=%+v", got)
	}
}

func TestStore_List_FilterByProjectAndParent(t *testing.T) {
	t.Parallel()
	s := NewStore()
	p := newItem("P")
	p.ProjectID = "P"
	q := newItem("Q")
	q.ProjectID = "Q"
	pe := newItem("PE")
	pe.ProjectID = "P"
	pe.ParentID = "P"
	_, _ = s.Put(context.Background(), p, -1)
	_, _ = s.Put(context.Background(), q, -1)
	_, _ = s.Put(context.Background(), pe, -1)

	got, _ := s.List(context.Background(), domain.ListFilter{ProjectID: "P"})
	if len(got) != 2 {
		t.Errorf("ProjectID filter: got len=%d, want 2", len(got))
	}
	got, _ = s.List(context.Background(), domain.ListFilter{ParentID: "P"})
	if len(got) != 1 || got[0].ID != "PE" {
		t.Errorf("ParentID filter: got=%+v", got)
	}
}

func TestStore_List_FilterByAssigneeAndUnassigned(t *testing.T) {
	t.Parallel()
	s := NewStore()
	a := newItem("A")
	a.Claim = &domain.Claim{Agent: "alice", LeaseID: "01HX"}
	b := newItem("B")
	b.Claim = &domain.Claim{Agent: "bob", LeaseID: "01HY"}
	c := newItem("C") // no claim
	_, _ = s.Put(context.Background(), a, -1)
	_, _ = s.Put(context.Background(), b, -1)
	_, _ = s.Put(context.Background(), c, -1)

	got, _ := s.List(context.Background(), domain.ListFilter{Assignee: "alice"})
	if len(got) != 1 || got[0].ID != "A" {
		t.Errorf("Assignee filter: got=%+v", got)
	}
	got, _ = s.List(context.Background(), domain.ListFilter{Unassigned: true})
	if len(got) != 1 || got[0].ID != "C" {
		t.Errorf("Unassigned filter: got=%+v", got)
	}
}

func TestStore_List_FilterByTags(t *testing.T) {
	t.Parallel()
	s := NewStore()
	a := newItem("A")
	a.Tags = []string{"ml", "nlp"}
	b := newItem("B")
	b.Tags = []string{"ml"}
	c := newItem("C")
	c.Tags = []string{"frontend"}
	_, _ = s.Put(context.Background(), a, -1)
	_, _ = s.Put(context.Background(), b, -1)
	_, _ = s.Put(context.Background(), c, -1)

	got, _ := s.List(context.Background(), domain.ListFilter{Tags: []string{"ml", "nlp"}})
	if len(got) != 1 || got[0].ID != "A" {
		t.Errorf("Tags=[ml,nlp] filter: got=%+v", got)
	}
	got, _ = s.List(context.Background(), domain.ListFilter{Tags: []string{"ml"}})
	if len(got) != 2 {
		t.Errorf("Tags=[ml] filter: got len=%d, want 2", len(got))
	}
}

func TestStore_List_FilterByCapabilities(t *testing.T) {
	t.Parallel()
	s := NewStore()
	a := newItem("A")
	a.RequiredCapabilities = []string{"go"}
	b := newItem("B")
	b.RequiredCapabilities = []string{"go", "python"}
	c := newItem("C") // no requirements
	_, _ = s.Put(context.Background(), a, -1)
	_, _ = s.Put(context.Background(), b, -1)
	_, _ = s.Put(context.Background(), c, -1)

	// Agent with [go] caps can do A and C (no requirements).
	got, _ := s.List(context.Background(), domain.ListFilter{Capabilities: []string{"go"}})
	ids := idsOf(got)
	if !sliceEq(ids, []string{"A", "C"}) {
		t.Errorf("Capabilities=[go]: got=%v, want [A C]", ids)
	}
	// Agent with [go, python] caps can do all three.
	got, _ = s.List(context.Background(), domain.ListFilter{Capabilities: []string{"go", "python"}})
	ids = idsOf(got)
	if len(ids) != 3 {
		t.Errorf("Capabilities=[go,python]: got=%v", ids)
	}
}

func TestStore_List_FilterUnblocked(t *testing.T) {
	t.Parallel()
	s := NewStore()
	dep := newItem("DEP")
	dep.Status = domain.StatusInProgress
	a := newItem("A") // depends on DEP (not done) → blocked
	a.Dependencies = []string{"DEP"}
	b := newItem("B") // no deps → unblocked
	c := newItem("C") // depends on a done item → unblocked
	c.Dependencies = []string{"DONE"}
	done := newItem("DONE")
	done.Status = domain.StatusDone

	for _, it := range []domain.Item{dep, a, b, c, done} {
		_, _ = s.Put(context.Background(), it, -1)
	}

	got, _ := s.List(context.Background(), domain.ListFilter{Unblocked: true})
	ids := idsOf(got)
	want := []string{"B", "C", "DEP", "DONE"}
	if !sliceEq(ids, want) {
		t.Errorf("Unblocked: got=%v, want=%v", ids, want)
	}
}

func TestStore_List_LimitCaps(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for _, id := range []string{"A", "B", "C", "D", "E"} {
		_, _ = s.Put(context.Background(), newItem(id), -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Limit: 2})
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2", len(got))
	}
}

func TestStore_List_SortPriority(t *testing.T) {
	t.Parallel()
	s := NewStore()
	items := []struct {
		id  string
		pri domain.Priority
	}{
		{"L", domain.PriorityLow},
		{"H", domain.PriorityHigh},
		{"C", domain.PriorityCritical},
		{"M", domain.PriorityMedium},
	}
	for _, it := range items {
		i := newItem(it.id)
		i.Priority = it.pri
		_, _ = s.Put(context.Background(), i, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Sort: "priority"})
	wantOrder := []string{"C", "H", "M", "L"}
	if !sliceEq(idsOf(got), wantOrder) {
		t.Errorf("priority asc: got=%v, want=%v", idsOf(got), wantOrder)
	}
	got, _ = s.List(context.Background(), domain.ListFilter{Sort: "priority", Order: "desc"})
	if !sliceEq(idsOf(got), reverse(wantOrder)) {
		t.Errorf("priority desc: got=%v", idsOf(got))
	}
}

func TestStore_List_SortByCreatedAt(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for i, id := range []string{"old", "mid", "new"} {
		it := newItem(id)
		it.CreatedAt = time.Date(2026, 1, 1+i, 0, 0, 0, 0, time.UTC)
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Sort: "created_at"})
	if !sliceEq(idsOf(got), []string{"old", "mid", "new"}) {
		t.Errorf("created_at asc: got=%v", idsOf(got))
	}
	got, _ = s.List(context.Background(), domain.ListFilter{Sort: "created_at", Order: "desc"})
	if !sliceEq(idsOf(got), []string{"new", "mid", "old"}) {
		t.Errorf("created_at desc: got=%v", idsOf(got))
	}
}

func TestStore_List_SortByUpdatedAt(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for i, id := range []string{"old", "mid", "new"} {
		it := newItem(id)
		it.UpdatedAt = time.Date(2026, 2, 1+i, 0, 0, 0, 0, time.UTC)
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Sort: "updated_at"})
	if !sliceEq(idsOf(got), []string{"old", "mid", "new"}) {
		t.Errorf("updated_at asc: got=%v", idsOf(got))
	}
}

func TestStore_List_SortByDueDate(t *testing.T) {
	t.Parallel()
	s := NewStore()
	d1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	a := newItem("A")
	a.DueDate = &d2
	b := newItem("B")
	b.DueDate = &d1
	c := newItem("C") // no due date — should sort last in asc

	for _, it := range []domain.Item{a, b, c} {
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Sort: "due_date"})
	want := []string{"B", "A", "C"}
	if !sliceEq(idsOf(got), want) {
		t.Errorf("due_date asc: got=%v, want=%v", idsOf(got), want)
	}
}

func TestStore_List_PriorityTieBrokenByID(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for _, id := range []string{"Z", "B", "A"} {
		it := newItem(id)
		it.Priority = domain.PriorityHigh
		_, _ = s.Put(context.Background(), it, -1)
	}
	got, _ := s.List(context.Background(), domain.ListFilter{Sort: "priority"})
	if !sliceEq(idsOf(got), []string{"A", "B", "Z"}) {
		t.Errorf("priority tie-break by ID: got=%v", idsOf(got))
	}
}

func TestStore_List_UnknownSortIsStable(t *testing.T) {
	t.Parallel()
	s := NewStore()
	for _, id := range []string{"C", "A", "B"} {
		_, _ = s.Put(context.Background(), newItem(id), -1)
	}
	got1, _ := s.List(context.Background(), domain.ListFilter{Sort: "weird"})
	got2, _ := s.List(context.Background(), domain.ListFilter{Sort: "weird"})
	if !sliceEq(idsOf(got1), idsOf(got2)) {
		t.Errorf("unknown sort produced different orderings: %v vs %v", idsOf(got1), idsOf(got2))
	}
}

func TestStore_StorageIsDeepCopied(t *testing.T) {
	t.Parallel()
	s := NewStore()
	in := newItem("RKN")
	in.Tags = []string{"a"}
	_, _ = s.Put(context.Background(), in, -1)

	in.Tags[0] = "MUTATED" // attempt to corrupt stored copy
	got, _ := s.Get(context.Background(), "RKN")
	if got.Tags[0] != "a" {
		t.Errorf("store shared slice memory with caller: %v", got.Tags)
	}
}

// helpers

func idsOf(items []domain.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func sliceEq(a, b []string) bool {
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

func reverse(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}
