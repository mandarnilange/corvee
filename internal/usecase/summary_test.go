package usecase

import (
	"context"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestSummary_RollsUpCounts(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, Status: domain.StatusBacklog},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", Status: domain.StatusReady, Deliverable: "ship X"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01", Status: domain.StatusDone},
		domain.Item{ID: "RKN-E01-S02", Type: domain.TypeStory, ParentID: "RKN-E01", Status: domain.StatusInProgress},
	)
	out, err := Summary(context.Background(), Deps{Store: store}, SummaryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 4 {
		t.Errorf("total = %d, want 4", out.Total)
	}
	if out.Counts.Done != 1 || out.Counts.InProgress != 1 || out.Counts.Ready != 1 || out.Counts.Backlog != 1 {
		t.Errorf("counts: %+v", out.Counts)
	}
	if got := out.Deliverables["RKN-E01"]; got != "ship X" {
		t.Errorf("deliverable lost: %q", got)
	}
}

func TestSummary_CriticalPathReportsPerStepStatus(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, Status: domain.StatusReady},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", Status: domain.StatusDone, Title: "epic"},
	)
	ws := testfakes.NewWorkspaceLoader()
	if err := ws.Save(domain.Workspace{
		WorkspaceName: "rkn",
		CriticalPath:  []string{"RKN-E01", "ghost"},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := Summary(context.Background(), Deps{Store: store, Workspace: ws}, SummaryInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CriticalPath) != 2 {
		t.Fatalf("steps: %+v", out.CriticalPath)
	}
	if !out.CriticalPath[0].Found || out.CriticalPath[0].Status != domain.StatusDone {
		t.Errorf("step 0 wrong: %+v", out.CriticalPath[0])
	}
	if out.CriticalPath[1].Found {
		t.Errorf("ghost step should be Found=false: %+v", out.CriticalPath[1])
	}
}

func TestSummary_FilteredByProject(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN", Status: domain.StatusReady},
		domain.Item{ID: "AAA", Type: domain.TypeProject, ProjectID: "AAA", Status: domain.StatusReady},
	)
	out, err := Summary(context.Background(), Deps{Store: store}, SummaryInput{ProjectID: "RKN"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Total != 1 {
		t.Errorf("total = %d, want 1", out.Total)
	}
}
