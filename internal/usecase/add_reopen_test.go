package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// TestAdd_ReopensDoneAncestors verifies the auto-rollup behavior
// described by review-report-after-merge: adding a child under a
// `done` parent must demote the ancestor chain back to `in_progress`
// and clear CompletedAt. The natural mental model — "if there's open
// work under it, the epic isn't done" — is what users expect.
func TestAdd_ReopensDoneAncestors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC))
	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Pre-populate a project + epic, both already marked `done`.
	project := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC", Type: domain.TypeProject, ProjectID: "ABC",
		Title:  "P",
		Status: domain.StatusDone, CompletedAt: &completed,
		Version: 5,
	}
	epic := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC-E01", Type: domain.TypeEpic, ProjectID: "ABC", ParentID: "ABC",
		Title:  "E",
		Status: domain.StatusDone, CompletedAt: &completed,
		Version: 7,
	}
	if _, err := store.Put(ctx, project, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, epic, -1); err != nil {
		t.Fatal(err)
	}

	d := Deps{
		Store:    store,
		Clock:    clock,
		IDGen:    testfakes.NewIDGen("EVT"),
		EventLog: testfakes.NewEventLog(),
	}

	// Add a new story under the done epic.
	out, err := Add(ctx, d, AddInput{
		Type:     domain.TypeStory,
		ParentID: "ABC-E01",
		Title:    "Refresh",
		Agent:    "alice",
	})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if out.Item.ID != "ABC-E01-S01" {
		t.Errorf("new story id = %s, want ABC-E01-S01", out.Item.ID)
	}

	// Epic must be reopened.
	gotEpic, _ := store.Get(ctx, "ABC-E01")
	if gotEpic.Status != domain.StatusInProgress {
		t.Errorf("epic.status = %s, want in_progress", gotEpic.Status)
	}
	if gotEpic.CompletedAt != nil {
		t.Errorf("epic.completed_at must be cleared, got %v", gotEpic.CompletedAt)
	}
	if gotEpic.Version != 8 {
		t.Errorf("epic.version = %d, want 8 (was 7)", gotEpic.Version)
	}
	if n := len(gotEpic.Journal); n == 0 || gotEpic.Journal[n-1].Event != "auto_reopened" {
		t.Errorf("epic.journal missing auto_reopened entry; got %+v", gotEpic.Journal)
	}

	// Project must also be reopened (rollup walks the whole chain).
	gotProject, _ := store.Get(ctx, "ABC")
	if gotProject.Status != domain.StatusInProgress {
		t.Errorf("project.status = %s, want in_progress", gotProject.Status)
	}
	if gotProject.CompletedAt != nil {
		t.Errorf("project.completed_at must be cleared, got %v", gotProject.CompletedAt)
	}
}

// TestAdd_DoesNotReopenAlreadyOpenAncestor confirms the walk stops
// at the first non-done ancestor — we don't churn versions on
// already-open items.
func TestAdd_DoesNotReopenAlreadyOpenAncestor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()

	project := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC", Type: domain.TypeProject, ProjectID: "ABC",
		Title:   "P",
		Status:  domain.StatusInProgress,
		Version: 1,
	}
	epic := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC-E01", Type: domain.TypeEpic, ProjectID: "ABC", ParentID: "ABC",
		Title:   "E",
		Status:  domain.StatusReady,
		Version: 1,
	}
	for _, it := range []domain.Item{project, epic} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := Deps{
		Store:    store,
		Clock:    testfakes.NewClock(time.Now()),
		IDGen:    testfakes.NewIDGen("EVT"),
		EventLog: testfakes.NewEventLog(),
	}

	if _, err := Add(ctx, d, AddInput{
		Type:     domain.TypeStory,
		ParentID: "ABC-E01",
		Title:    "S",
		Agent:    "a",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	gotEpic, _ := store.Get(ctx, "ABC-E01")
	if gotEpic.Version != 1 {
		t.Errorf("epic version bumped unnecessarily; got %d, want 1", gotEpic.Version)
	}
	gotProject, _ := store.Get(ctx, "ABC")
	if gotProject.Version != 1 {
		t.Errorf("project version bumped unnecessarily; got %d, want 1", gotProject.Version)
	}
}

// TestAdd_DeepWalkSubtaskThroughProject exercises the maximum-depth
// case: adding a subtask under a story walks story → epic → project,
// re-opening every `done` ancestor in the chain. Verifies the
// recursive walk handles arbitrary depth (not just two levels).
func TestAdd_DeepWalkSubtaskThroughProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	project := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC", Type: domain.TypeProject, ProjectID: "ABC",
		Title: "P", Status: domain.StatusDone, CompletedAt: &completed, Version: 1,
	}
	epic := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC-E01", Type: domain.TypeEpic, ProjectID: "ABC", ParentID: "ABC",
		Title: "E", Status: domain.StatusDone, CompletedAt: &completed, Version: 1,
	}
	story := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC-E01-S01", Type: domain.TypeStory, ProjectID: "ABC", ParentID: "ABC-E01",
		Title: "S", Status: domain.StatusDone, CompletedAt: &completed, Version: 1,
	}
	for _, it := range []domain.Item{project, epic, story} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}

	d := Deps{
		Store:    store,
		Clock:    testfakes.NewClock(time.Now()),
		IDGen:    testfakes.NewIDGen("EVT"),
		EventLog: testfakes.NewEventLog(),
	}

	// Add a subtask under the done story.
	if _, err := Add(ctx, d, AddInput{
		Type:     domain.TypeSubtask,
		ParentID: "ABC-E01-S01",
		Title:    "T1",
		Agent:    "alice",
	}); err != nil {
		t.Fatalf("Add subtask: %v", err)
	}

	// All three ancestors must be reopened.
	for _, id := range []string{"ABC-E01-S01", "ABC-E01", "ABC"} {
		got, err := store.Get(ctx, id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if got.Status != domain.StatusInProgress {
			t.Errorf("%s.status = %s, want in_progress", id, got.Status)
		}
		if got.CompletedAt != nil {
			t.Errorf("%s.completed_at must be cleared, got %v", id, got.CompletedAt)
		}
	}
}

// TestAdd_StopsWalkAtFirstNonDoneAncestor exercises the mid-chain
// case: epic is done, project is in_progress. Walk should re-open
// the epic and stop (project is already open, no churn).
func TestAdd_StopsWalkAtFirstNonDoneAncestor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	completed := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	project := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC", Type: domain.TypeProject, ProjectID: "ABC",
		Title:   "P",
		Status:  domain.StatusInProgress,
		Version: 4,
	}
	epic := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "ABC-E01", Type: domain.TypeEpic, ProjectID: "ABC", ParentID: "ABC",
		Title:  "E",
		Status: domain.StatusDone, CompletedAt: &completed,
		Version: 9,
	}
	for _, it := range []domain.Item{project, epic} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatal(err)
		}
	}
	d := Deps{
		Store:    store,
		Clock:    testfakes.NewClock(time.Now()),
		IDGen:    testfakes.NewIDGen("EVT"),
		EventLog: testfakes.NewEventLog(),
	}

	if _, err := Add(ctx, d, AddInput{
		Type:     domain.TypeStory,
		ParentID: "ABC-E01",
		Title:    "S",
		Agent:    "a",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	gotEpic, _ := store.Get(ctx, "ABC-E01")
	if gotEpic.Status != domain.StatusInProgress || gotEpic.CompletedAt != nil {
		t.Errorf("epic should have been re-opened; got %+v", gotEpic)
	}

	gotProject, _ := store.Get(ctx, "ABC")
	if gotProject.Version != 4 {
		t.Errorf("project version churned: got %d, want 4", gotProject.Version)
	}
}
