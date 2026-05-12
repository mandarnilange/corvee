package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestClaim_RequiresAgent(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestClaim_RequiresID(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Claim(context.Background(), d, ClaimInput{Agent: "x"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestClaim_RequiresLocker(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady, Version: 1})
	_, err := Claim(context.Background(), Deps{
		Store: store, Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"),
	}, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage when locker is nil, got %v", err)
	}
}

func TestRelease_RequiresIDAndLease(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Release(context.Background(), d, ReleaseInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestHeartbeat_RequiresIDAndLease(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Heartbeat(context.Background(), d, HeartbeatInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestHeartbeat_NoClaimRejected(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: "x",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("want ErrLeaseMismatch, got %v", err)
	}
}

func TestNext_RankLess_BothCriticalUsesIndex(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "AAA-E01-S01", Type: domain.TypeStory, Status: domain.StatusReady},
		domain.Item{ID: "AAA-E01-S02", Type: domain.TypeStory, Status: domain.StatusReady},
	)
	ws := testfakes.NewWorkspaceLoader()
	_ = ws.Save(domain.Workspace{CriticalPath: []string{"AAA-E01-S02", "AAA-E01-S01"}})
	d := Deps{
		Store: store, Workspace: ws,
		Clock: testfakes.NewClock(time.Now()), IDGen: testfakes.NewIDGen("L"), Locker: testfakes.NewLocker(),
	}
	out, _ := Next(context.Background(), d, NextInput{})
	if out.Item.ID != "AAA-E01-S02" {
		t.Errorf("expected AAA-E01-S02 (lower critical_path index), got %s", out.Item.ID)
	}
}

func TestReindex_MissingIndexerErrors(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	_, err := Reindex(context.Background(), Deps{Store: store, Clock: testfakes.NewClock(time.Now())}, ReindexInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage when indexer nil, got %v", err)
	}
}

func TestDone_LeaseMismatch_Rejected(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	_ = c
	// Move it to in_progress so the transition to done is legal.
	d.Clock.(*testfakes.Clock).Advance(time.Second)
	if _, hbErr := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, Agent: "alice", AdvanceToInProgress: true,
	}); hbErr != nil {
		t.Fatal(hbErr)
	}
	_, err = Done(context.Background(), d, DoneInput{
		ID: "RKN-E01-S01", LeaseID: "wrong", Agent: "alice", ExpectVersion: -1,
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("want ErrLeaseMismatch, got %v", err)
	}
}

func TestDone_LeaseMatch_ClearsClaim(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, _ := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	d.Clock.(*testfakes.Clock).Advance(time.Second)
	if _, err := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, AdvanceToInProgress: true,
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Done(context.Background(), d, DoneInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, Agent: "alice", ExpectVersion: -1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.Claim != nil {
		t.Errorf("claim not cleared on done: %+v", out.Item.Claim)
	}
}

func TestNext_PriorityRank_AllValuesCovered(t *testing.T) {
	t.Parallel()
	cases := map[domain.Priority]int{
		domain.PriorityCritical: 0,
		domain.PriorityHigh:     1,
		domain.PriorityMedium:   2,
		domain.PriorityLow:      3,
		domain.Priority("???"):  4,
	}
	for p, want := range cases {
		if got := priorityRank(p); got != want {
			t.Errorf("priorityRank(%q) = %d, want %d", p, got, want)
		}
	}
}

func TestSummary_AllStatusesIncrement(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "X1", Type: domain.TypeStory, Status: domain.StatusBacklog},
		domain.Item{ID: "X2", Type: domain.TypeStory, Status: domain.StatusReady},
		domain.Item{ID: "X3", Type: domain.TypeStory, Status: domain.StatusClaimed},
		domain.Item{ID: "X4", Type: domain.TypeStory, Status: domain.StatusInProgress},
		domain.Item{ID: "X5", Type: domain.TypeStory, Status: domain.StatusReview},
		domain.Item{ID: "X6", Type: domain.TypeStory, Status: domain.StatusBlocked},
		domain.Item{ID: "X7", Type: domain.TypeStory, Status: domain.StatusDone},
		domain.Item{ID: "X8", Type: domain.TypeStory, Status: domain.StatusAbandoned},
	)
	out, err := Summary(context.Background(), Deps{Store: store}, SummaryInput{})
	if err != nil {
		t.Fatal(err)
	}
	c := out.Counts
	if c.Backlog != 1 || c.Ready != 1 || c.Claimed != 1 || c.InProgress != 1 ||
		c.Review != 1 || c.Blocked != 1 || c.Done != 1 || c.Abandoned != 1 {
		t.Errorf("status counts wrong: %+v", c)
	}
}
