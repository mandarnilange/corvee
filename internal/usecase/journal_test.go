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

func mkJournalDeps(t *testing.T) (usecase.Deps, *testfakes.Clock) {
	t.Helper()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC))
	return usecase.Deps{Store: store, Clock: clock, EventLog: testfakes.NewEventLog()}, clock
}

func mustAddProject(t *testing.T, d usecase.Deps, code string) {
	t.Helper()
	if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
		Type: domain.TypeProject, ProjectCode: code, Title: code,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestJournal_DefaultsToNoteEvent(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	mustAddProject(t, d, "RKN")

	out, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Note: "thinking out loud", Agent: "alice",
	})
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "note" {
		t.Errorf("event=%q want note", last.Event)
	}
	if last.Note != "thinking out loud" {
		t.Errorf("note=%q", last.Note)
	}
	if last.Actor != "alice" {
		t.Errorf("actor=%q", last.Actor)
	}
}

func TestJournal_AcceptedEventTypes(t *testing.T) {
	t.Parallel()
	for _, ev := range []string{"note", "progress", "blocker", "decision", "handoff"} {
		ev := ev
		t.Run(ev, func(t *testing.T) {
			t.Parallel()
			d, _ := mkJournalDeps(t) // fresh store per sub-test
			mustAddProject(t, d, "RKN")
			// progress/blocker/decision require either a lease OR an
			// unclaimed item — the project here is unclaimed, so all five
			// are acceptable without a lease.
			out, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
				ID: "RKN", Event: ev, Note: "x", Agent: "alice",
			})
			if err != nil {
				t.Fatalf("Journal event=%s: %v", ev, err)
			}
			last := out.Item.Journal[len(out.Item.Journal)-1]
			if last.Event != ev {
				t.Errorf("event=%q want %q", last.Event, ev)
			}
		})
	}
}

func TestJournal_RejectsUnknownEventType(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	mustAddProject(t, d, "RKN")
	_, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "ramble", Note: "x", Agent: "alice",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v want ErrUsage", err)
	}
}

func TestJournal_StoresMetadataVerbatim(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	mustAddProject(t, d, "RKN")
	md := map[string]any{
		"links":    []any{"http://x", "http://y"},
		"severity": "high",
		"counts":   map[string]any{"tries": float64(3)},
	}
	out, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "decision", Metadata: md, Agent: "alice",
	})
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Metadata["severity"] != "high" {
		t.Errorf("severity=%v", last.Metadata["severity"])
	}
	links, ok := last.Metadata["links"].([]any)
	if !ok || len(links) != 2 {
		t.Errorf("links=%v", last.Metadata["links"])
	}
}

func TestJournal_RequiresIDAndAgent(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	if _, err := usecase.Journal(context.Background(), d, usecase.JournalInput{Agent: "a"}); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("missing id err=%v want ErrUsage", err)
	}
	if _, err := usecase.Journal(context.Background(), d, usecase.JournalInput{ID: "RKN"}); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("missing agent err=%v want ErrUsage", err)
	}
}

func TestJournal_MissingItemReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	_, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Note: "x", Agent: "alice",
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err=%v want ErrNotFound", err)
	}
}

func claimItem(t *testing.T, d usecase.Deps, id, agent string) string {
	t.Helper()
	// Project must transit backlog→ready before claim.
	v := 1
	ready := domain.StatusReady
	out, err := usecase.Update(context.Background(), d, usecase.UpdateInput{
		ID: id, ExpectVersion: v, Status: &ready,
	})
	if err != nil {
		t.Fatalf("update→ready: %v", err)
	}
	v = out.NewVersion
	_ = v // ClaimInput has no version expectation; the locker is the gate.
	cl, err := usecase.Claim(context.Background(), d, usecase.ClaimInput{
		ID: id, Agent: agent, TTL: time.Hour,
	})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	return cl.LeaseID
}

func TestJournal_ProgressOnClaimedRequiresLease(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	d.IDGen = testfakes.NewIDGen("01J0LEASE000000000000001")
	d.Locker = testfakes.NewLocker()
	mustAddProject(t, d, "RKN")
	_ = claimItem(t, d, "RKN", "alice")

	// progress without a lease against a claimed item: rejected.
	_, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "progress", Note: "step 1", Agent: "alice",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("missing lease err=%v want ErrLeaseMismatch", err)
	}

	// progress with the wrong lease against a claimed item: rejected.
	_, err = usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "blocker", Note: "stuck", Agent: "alice",
		LeaseID: "wrong-lease",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("wrong lease err=%v want ErrLeaseMismatch", err)
	}
}

func TestJournal_ProgressOnClaimedAcceptsCorrectLease(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	d.IDGen = testfakes.NewIDGen("01J0LEASE000000000000001")
	d.Locker = testfakes.NewLocker()
	mustAddProject(t, d, "RKN")
	lease := claimItem(t, d, "RKN", "alice")

	out, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "decision", Note: "going with X", Agent: "alice",
		LeaseID: lease,
	})
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	last := out.Item.Journal[len(out.Item.Journal)-1]
	if last.Event != "decision" {
		t.Errorf("event=%q want decision", last.Event)
	}
}

func TestJournal_NoteOnClaimedDoesNotRequireLease(t *testing.T) {
	t.Parallel()
	// note and handoff are observer-friendly events: no lease required even
	// when the item is held — anyone may leave a comment or hand off.
	for _, ev := range []string{"note", "handoff"} {
		ev := ev
		t.Run(ev, func(t *testing.T) {
			t.Parallel()
			d, _ := mkJournalDeps(t) // fresh deps per sub-test
			d.IDGen = testfakes.NewIDGen("01J0LEASE000000000000001")
			d.Locker = testfakes.NewLocker()
			mustAddProject(t, d, "RKN")
			_ = claimItem(t, d, "RKN", "alice")

			out, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
				ID: "RKN", Event: ev, Note: "hi", Agent: "carol",
			})
			if err != nil {
				t.Fatalf("Journal event=%s: %v", ev, err)
			}
			last := out.Item.Journal[len(out.Item.Journal)-1]
			if last.Event != ev {
				t.Errorf("event=%q", last.Event)
			}
		})
	}
}

func TestJournal_AppendsAuditEvent(t *testing.T) {
	t.Parallel()
	d, _ := mkJournalDeps(t)
	d.IDGen = testfakes.NewIDGen("01J0EV000000000000000001")
	mustAddProject(t, d, "RKN")
	_, err := usecase.Journal(context.Background(), d, usecase.JournalInput{
		ID: "RKN", Event: "note", Note: "hello", Agent: "alice",
		Metadata: map[string]any{"k": "v"},
	})
	if err != nil {
		t.Fatalf("Journal: %v", err)
	}
	log := d.EventLog.(*testfakes.EventLog)
	events, _ := log.Read()
	var found *domain.Event
	for i := range events {
		if events[i].Type == "journal" {
			found = &events[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected an event of type 'journal'; got %v", events)
	}
	if found.ItemID != "RKN" || found.Actor != "alice" {
		t.Errorf("event=%+v", *found)
	}
	if found.Metadata["event"] != "note" {
		t.Errorf("audit metadata.event=%v want note", found.Metadata["event"])
	}
	if found.Metadata["k"] != "v" {
		t.Errorf("audit metadata.k=%v want v", found.Metadata["k"])
	}
}
