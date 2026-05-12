package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func mkClaimDeps(t *testing.T) Deps {
	t.Helper()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC))
	mustPut(t, store, domain.Item{
		ID:      "RKN-E01-S01",
		Type:    domain.TypeStory,
		Status:  domain.StatusReady,
		Version: 1,
	})
	return Deps{
		Store:    store,
		Clock:    clock,
		IDGen:    testfakes.NewIDGen("LEASE"),
		Locker:   testfakes.NewLocker(),
		EventLog: testfakes.NewEventLog(),
	}
}

func TestClaim_HappyPath_MintsLeaseAndTransitions(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	out, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if out.LeaseID == "" {
		t.Error("missing lease ID")
	}
	if out.Item.Status != domain.StatusClaimed {
		t.Errorf("status = %q", out.Item.Status)
	}
	if out.Item.Claim == nil || out.Item.Claim.Agent != "alice" || out.Item.Claim.LeaseID != out.LeaseID {
		t.Errorf("claim record wrong: %+v", out.Item.Claim)
	}
	if out.Item.Version != 2 {
		t.Errorf("version = %d, want 2", out.Item.Version)
	}
}

func TestClaim_RejectsWhenAlreadyClaimed(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"}); err != nil {
		t.Fatal(err)
	}
	_, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "bob"})
	if !errors.Is(err, domain.ErrAlreadyClaimed) {
		t.Errorf("want ErrAlreadyClaimed, got %v", err)
	}
}

func TestClaim_AllowsTakeoverOfExpiredLease(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	d.Clock.(*testfakes.Clock).Advance(time.Hour)
	out, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "bob"})
	if err != nil {
		t.Fatalf("takeover: %v", err)
	}
	if out.Item.Claim.Agent != "bob" {
		t.Errorf("expected takeover by bob, claim=%+v", out.Item.Claim)
	}
}

func TestClaim_RejectsWhenStatusNotReady(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusBacklog, Version: 1,
	})
	d := Deps{
		Store: store, Clock: testfakes.NewClock(time.Now()),
		IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
	}
	_, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if !errors.Is(err, domain.ErrInvalidStatus) {
		t.Errorf("want ErrInvalidStatus, got %v", err)
	}
}

func TestClaim_RejectsWhenCapabilitiesMissing(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Version: 1,
		RequiredCapabilities: []string{"go", "concurrent"},
	})
	d := Deps{
		Store: store, Clock: testfakes.NewClock(time.Now()),
		IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
	}
	_, err := Claim(context.Background(), d, ClaimInput{
		ID: "RKN-E01-S01", Agent: "alice", AgentCapabilities: []string{"go"},
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage for capability mismatch, got %v", err)
	}
}

func TestClaim_AppendsEventWhenLogPresent(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"}); err != nil {
		t.Fatal(err)
	}
	events, _ := d.EventLog.Read()
	if len(events) == 0 || events[0].Type != "claimed" {
		t.Errorf("event log: %+v", events)
	}
}
