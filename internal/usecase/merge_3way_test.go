package usecase

import (
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestMergeItem3Way_StatusLatestVersionWins(t *testing.T) {
	t.Parallel()
	base := &domain.Item{ID: "X", Version: 1, Status: domain.StatusReady}
	ours := domain.Item{ID: "X", Version: 2, Status: domain.StatusInProgress}
	theirs := domain.Item{ID: "X", Version: 3, Status: domain.StatusReview}
	merged, _, _ := MergeItem3Way(base, ours, theirs)
	if merged.Status != domain.StatusReview {
		t.Errorf("status = %s, want review", merged.Status)
	}
	if merged.Version != 4 {
		t.Errorf("version = %d, want 4 (max+1)", merged.Version)
	}
}

func TestMergeItem3Way_TagsUnion(t *testing.T) {
	t.Parallel()
	ours := domain.Item{ID: "X", Tags: []string{"a", "b"}}
	theirs := domain.Item{ID: "X", Tags: []string{"b", "c"}}
	merged, _, _ := MergeItem3Way(nil, ours, theirs)
	want := []string{"a", "b", "c"}
	if !equalStrings(merged.Tags, want) {
		t.Errorf("tags = %v, want %v", merged.Tags, want)
	}
}

func TestMergeItem3Way_JournalUnionDedup(t *testing.T) {
	t.Parallel()
	ts1 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	ts2 := time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC)
	ours := domain.Item{ID: "X", Journal: []domain.JournalEntry{
		{Timestamp: ts1, Actor: "a", Event: "claimed"},
		{Timestamp: ts2, Actor: "a", Event: "progress"},
	}}
	theirs := domain.Item{ID: "X", Journal: []domain.JournalEntry{
		{Timestamp: ts1, Actor: "a", Event: "claimed"}, // dup
		{Timestamp: ts2, Actor: "b", Event: "review"},
	}}
	merged, _, _ := MergeItem3Way(nil, ours, theirs)
	if got := len(merged.Journal); got != 3 {
		t.Errorf("journal len = %d, want 3 (dedup)", got)
	}
}

func TestMergeItem3Way_ClaimLowerLeaseWins(t *testing.T) {
	t.Parallel()
	ours := domain.Item{ID: "X", Claim: &domain.Claim{Agent: "a-vm1", LeaseID: "01HX0000000000000000000001"}}
	theirs := domain.Item{ID: "X", Claim: &domain.Claim{Agent: "a-vm2", LeaseID: "01HX0000000000000000000002"}}
	merged, lostAgent, _ := MergeItem3Way(nil, ours, theirs)
	if merged.Claim == nil || merged.Claim.Agent != "a-vm1" {
		t.Errorf("expected a-vm1 to win, got %+v", merged.Claim)
	}
	if lostAgent != "a-vm2" {
		t.Errorf("loser = %s, want a-vm2", lostAgent)
	}
}

func TestMergeItem3Way_ClaimSameLeaseNoLoser(t *testing.T) {
	t.Parallel()
	c := &domain.Claim{Agent: "a", LeaseID: "01HX0000000000000000000001"}
	ours := domain.Item{ID: "X", Claim: c}
	theirs := domain.Item{ID: "X", Claim: c}
	_, lostAgent, _ := MergeItem3Way(nil, ours, theirs)
	if lostAgent != "" {
		t.Errorf("expected no loser, got %s", lostAgent)
	}
}

func TestMergeItem3Way_ProseConflictSurfacesFlag(t *testing.T) {
	t.Parallel()
	base := &domain.Item{ID: "X", Description: "orig", AcceptanceCriteria: []string{"a"}}
	ours := domain.Item{ID: "X", Description: "ours-edit", AcceptanceCriteria: []string{"a"}}
	theirs := domain.Item{ID: "X", Description: "theirs-edit", AcceptanceCriteria: []string{"a"}}
	_, _, prose := MergeItem3Way(base, ours, theirs)
	if !prose {
		t.Errorf("expected prose conflict, got false")
	}
}

// TestMergeItem3Way_StatusDowngradeClearsCompletedAt covers the
// invariant that completed_at must be nil whenever status != done
// (review #9). Two divergent edits: one side marks done with a
// completed_at; the higher-version side flips the status back to
// in_progress. The merge picks the higher version's status; the
// stale completed_at must be cleared.
func TestMergeItem3Way_StatusDowngradeClearsCompletedAt(t *testing.T) {
	t.Parallel()
	completed := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	ours := domain.Item{ID: "X", Version: 2, Status: domain.StatusDone, CompletedAt: &completed}
	theirs := domain.Item{ID: "X", Version: 3, Status: domain.StatusInProgress}
	merged, _, _ := MergeItem3Way(nil, ours, theirs)
	if merged.Status != domain.StatusInProgress {
		t.Errorf("status = %s, want in_progress", merged.Status)
	}
	if merged.CompletedAt != nil {
		t.Errorf("completed_at must be nil when status != done; got %v", merged.CompletedAt)
	}
}

func TestMergeItem3Way_OnlyOneSideHasClaim(t *testing.T) {
	t.Parallel()
	c := &domain.Claim{Agent: "a", LeaseID: "01HX0000000000000000000001"}
	ours := domain.Item{ID: "X", Claim: c}
	theirs := domain.Item{ID: "X"}
	merged, lost, _ := MergeItem3Way(nil, ours, theirs)
	if merged.Claim == nil || lost != "" {
		t.Errorf("expected ours.claim preserved without loser, got claim=%v lost=%s", merged.Claim, lost)
	}
}
