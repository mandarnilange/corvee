package usecase_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// advanceTo "ready → claimed → in_progress" so Done has a legal source.
func advanceToInProgress(t *testing.T, d usecase.Deps, id string) int {
	t.Helper()
	v := 1
	for _, s := range []domain.Status{
		domain.StatusReady, domain.StatusClaimed, domain.StatusInProgress,
	} {
		next := s
		out, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
			ID: id, ExpectVersion: v, Status: &next,
		})
		if err != nil {
			t.Fatalf("Update→%s: %v", s, err)
		}
		v = out.NewVersion
	}
	return v
}

func TestDone_TransitionsAndStampsCompletedAt(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}

	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	v := advanceToInProgress(t, d, "RKN")

	clock.Set(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	out, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: v, Note: "shipped", Agent: "alice",
	})
	if err != nil {
		t.Fatalf("Done: %v", err)
	}
	if out.Item.Status != domain.StatusDone {
		t.Errorf("status=%v, want done", out.Item.Status)
	}
	if out.Item.CompletedAt == nil || !out.Item.CompletedAt.Equal(clock.Now()) {
		t.Errorf("CompletedAt=%v", out.Item.CompletedAt)
	}
	if out.NewVersion != v+1 {
		t.Errorf("version=%d, want %d", out.NewVersion, v+1)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "completed" || last.Note != "shipped" || last.Actor != "alice" {
		t.Errorf("journal entry: %+v", last)
	}
}

func TestDone_FromIllegalStatusReturnsErrInvalidStatus(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	// Item is at backlog; backlog → done is illegal.
	_, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: 1,
	})
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Errorf("err=%v, want ErrInvalidStatus", err)
	}
}

func TestDone_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	d := usecase.Deps{
		Store: testfakes.NewStore(),
		Clock: testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)),
	}
	_, err := usecase.Done(context.Background(), d, usecase.DoneInput{ID: "RKN"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err=%v, want ErrNotFound", err)
	}
}

func TestDone_AttachesMetadataToJournalEntry(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock, EventLog: testfakes.NewEventLog()}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	v := advanceToInProgress(t, d, "RKN")

	md := map[string]any{
		"deploy_url": "https://example.com/release/42",
		"sha":        "abc123",
	}
	out, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: v, Note: "shipped", Agent: "alice",
		Metadata: md,
	})
	if err != nil {
		t.Fatalf("Done: %v", err)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "completed" {
		t.Errorf("event=%q", last.Event)
	}
	if last.Metadata["deploy_url"] != "https://example.com/release/42" {
		t.Errorf("metadata.deploy_url=%v", last.Metadata["deploy_url"])
	}
	if last.Metadata["sha"] != "abc123" {
		t.Errorf("metadata.sha=%v", last.Metadata["sha"])
	}

	// Audit event should also carry the metadata so observers replaying the
	// log can reconstruct the handoff payload without rereading the item.
	log := d.EventLog.(*testfakes.EventLog)
	events, _ := log.Read()
	var completed *domain.Event
	for i := range events {
		if events[i].Type == "completed" {
			completed = &events[i]
		}
	}
	if completed == nil {
		t.Fatalf("expected a 'completed' audit event")
	}
	if completed.Metadata["deploy_url"] != "https://example.com/release/42" {
		t.Errorf("audit metadata.deploy_url=%v", completed.Metadata["deploy_url"])
	}
}

func TestDone_NoMetadataStillEmitsEntry(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	v := advanceToInProgress(t, d, "RKN")
	out, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: v, Agent: "alice",
	})
	if err != nil {
		t.Fatalf("Done: %v", err)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Metadata != nil {
		t.Errorf("metadata=%v want nil when no --metadata supplied", last.Metadata)
	}
}

func TestDone_NoNoteStillAddsCompletedEntry(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: "RKN", Title: "p",
	}); err != nil {
		t.Fatal(err)
	}
	v := advanceToInProgress(t, d, "RKN")

	if _, err := usecase.Done(context.Background(), d, usecase.DoneInput{
		ID: "RKN", ExpectVersion: v, Agent: "bob",
	}); err != nil {
		t.Fatalf("Done: %v", err)
	}
	out, _ := usecase.Show(context.Background(), d, usecase.ShowInput{ID: "RKN"})
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "completed" || last.Note != "" {
		t.Errorf("entry=%+v", last)
	}
}
