package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestNext_PrefersCriticalPath(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Priority: domain.PriorityLow},
		domain.Item{ID: "RKN-E01-S02", Type: domain.TypeStory, Status: domain.StatusReady, Priority: domain.PriorityHigh},
	)
	ws := testfakes.NewWorkspaceLoader()
	_ = ws.Save(domain.Workspace{CriticalPath: []string{"RKN-E01-S01"}})

	d := Deps{
		Store: store, Workspace: ws,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker: testfakes.NewLocker(),
	}
	out, err := Next(context.Background(), d, NextInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Found || out.Item.ID != "RKN-E01-S01" {
		t.Errorf("expected critical-path winner, got %+v", out)
	}
}

func TestNext_PriorityFallback(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Priority: domain.PriorityLow},
		domain.Item{ID: "RKN-E01-S02", Type: domain.TypeStory, Status: domain.StatusReady, Priority: domain.PriorityCritical},
	)
	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker: testfakes.NewLocker(),
	}
	out, err := Next(context.Background(), d, NextInput{})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Found || out.Item.ID != "RKN-E01-S02" {
		t.Errorf("expected priority winner, got %+v", out)
	}
}

func TestNext_FiltersByCapabilities(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, RequiredCapabilities: []string{"rust"}},
		domain.Item{ID: "RKN-E01-S02", Type: domain.TypeStory, Status: domain.StatusReady, RequiredCapabilities: []string{"go"}},
	)
	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker: testfakes.NewLocker(),
	}
	out, err := Next(context.Background(), d, NextInput{
		AgentCapabilities: []string{"go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Found || out.Item.ID != "RKN-E01-S02" {
		t.Errorf("expected go-capable item, got %+v", out)
	}
}

func TestNext_AutoClaim_MintsLease(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{
		ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Version: 1,
	})
	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker:   testfakes.NewLocker(),
		EventLog: testfakes.NewEventLog(),
	}
	out, err := Next(context.Background(), d, NextInput{Agent: "alice", AutoClaim: true})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Found || out.LeaseID == "" {
		t.Errorf("auto-claim failed: %+v", out)
	}
}

func TestNext_NoCandidates_FoundFalse(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker: testfakes.NewLocker(),
	}
	out, err := Next(context.Background(), d, NextInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Found {
		t.Errorf("empty workspace should not find: %+v", out)
	}
}

func TestNext_SkipsBlockedDependencies(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusInProgress},
		domain.Item{ID: "RKN-E01-S02", Type: domain.TypeStory, Status: domain.StatusReady, Dependencies: []string{"RKN-E01-S01"}},
	)
	d := Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
		Locker: testfakes.NewLocker(),
	}
	out, err := Next(context.Background(), d, NextInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Found {
		t.Errorf("should skip blocked, got %+v", out)
	}
}
