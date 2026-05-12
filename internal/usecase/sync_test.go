package usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func newSyncDeps(t *testing.T) (Deps, *testfakes.GitClient) {
	t.Helper()
	g := testfakes.NewGitClient()
	d := Deps{
		Store:     testfakes.NewStore(),
		Clock:     testfakes.NewClock(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("01HX0000000000000000000000"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	return d, g
}

func TestSync_PullPushHappyPath(t *testing.T) {
	t.Parallel()
	d, g := newSyncDeps(t)
	out, err := Sync(context.Background(), d, SyncInput{Agent: "a"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !out.Pulled || !out.Pushed {
		t.Errorf("expected pulled+pushed, got %+v", out)
	}
	if len(g.Calls) < 2 {
		t.Errorf("expected git calls, got %v", g.Calls)
	}
}

func TestSync_AutoCommitStagesAndCommits(t *testing.T) {
	t.Parallel()
	d, g := newSyncDeps(t)
	_, err := Sync(context.Background(), d, SyncInput{AutoCommit: true, CommitMessage: "snap"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	want := []string{"Add([.tasks])", `Commit("snap")`, "PullRebase", "Push"}
	if !sliceEqual(g.Calls, want) {
		t.Errorf("calls = %v, want %v", g.Calls, want)
	}
}

func TestSync_NoGitClient_ReturnsErrUsage(t *testing.T) {
	t.Parallel()
	d := Deps{}
	_, err := Sync(context.Background(), d, SyncInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestSync_PullRebaseError_NoConflict_PropagatesError(t *testing.T) {
	t.Parallel()
	d, g := newSyncDeps(t)
	g.Errors["PullRebase"] = errors.New("network down")
	_, err := Sync(context.Background(), d, SyncInput{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func sliceEqual(a, b []string) bool {
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

// TestSync_ConflictDispatchesMerge verifies that PullRebase returning
// ErrGitConflict triggers Merge, then RebaseContinue, then Push.
func TestSync_ConflictDispatchesMerge(t *testing.T) {
	t.Parallel()
	d, g := newSyncDeps(t)
	g.Errors["PullRebase"] = fmt.Errorf("conflict: %w", domain.ErrGitConflict)
	// No conflicted files to resolve — the merge usecase will see an
	// empty list and return an empty MergeOutput.
	g.ConflictedFilesQueue = [][]string{nil}

	out, err := Sync(context.Background(), d, SyncInput{Agent: "a"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if out.Merged == nil {
		t.Fatalf("expected MergeOutput, got nil")
	}
	if !out.Pushed {
		t.Errorf("expected push to happen after merge")
	}
	// Expected sequence includes RebaseContinue and Push.
	hasContinue, hasPush := false, false
	for _, c := range g.Calls {
		if c == "RebaseContinue" {
			hasContinue = true
		}
		if c == "Push" {
			hasPush = true
		}
	}
	if !hasContinue || !hasPush {
		t.Errorf("expected RebaseContinue and Push in calls; got %v", g.Calls)
	}
}
