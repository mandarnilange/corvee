package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// SummaryInput is the request payload for Summary.
type SummaryInput struct {
	// ProjectID, when non-empty, restricts the summary to one project.
	// Empty rolls up across every project.
	ProjectID string
}

// StatusCounts is a per-status tally.
type StatusCounts struct {
	Backlog    int `json:"backlog"`
	Ready      int `json:"ready"`
	Claimed    int `json:"claimed"`
	InProgress int `json:"in_progress"`
	Review     int `json:"review"`
	Blocked    int `json:"blocked"`
	Done       int `json:"done"`
	Abandoned  int `json:"abandoned"`
}

// CriticalPathStep is one element of the workspace's critical-path
// progress report.
type CriticalPathStep struct {
	// ID is the item's canonical ID.
	ID string `json:"id"`
	// Status is the current lifecycle state.
	Status domain.Status `json:"status"`
	// Title is the display label.
	Title string `json:"title,omitempty"`
	// Found is true when the workspace contains this ID.
	Found bool `json:"found"`
}

// SummaryOutput is the response payload for Summary.
type SummaryOutput struct {
	// Counts is the per-status rollup across the in-scope items.
	Counts StatusCounts `json:"counts"`
	// Total is the number of items considered.
	Total int `json:"total"`
	// Deliverables is the per-epic Deliverable strings (when set), so
	// CLI users can review epic outcomes at a glance.
	Deliverables map[string]string `json:"deliverables,omitempty"`
	// CriticalPath mirrors workspace.CriticalPath with a status read
	// for each step.
	CriticalPath []CriticalPathStep `json:"critical_path,omitempty"`
}

// Summary rolls up status counts, lists epic deliverables, and reports
// per-step status for the workspace's critical path.
func Summary(ctx context.Context, d Deps, in SummaryInput) (SummaryOutput, error) {
	filter := domain.ListFilter{}
	if in.ProjectID != "" {
		filter.ProjectID = in.ProjectID
	}
	items, err := d.Store.List(ctx, filter)
	if err != nil {
		return SummaryOutput{}, fmt.Errorf("summary: list: %w", err)
	}

	out := SummaryOutput{Total: len(items), Deliverables: map[string]string{}}
	for _, it := range items {
		incrementCount(&out.Counts, it.Status)
		if it.Type == domain.TypeEpic && it.Deliverable != "" {
			out.Deliverables[it.ID] = it.Deliverable
		}
	}
	if len(out.Deliverables) == 0 {
		out.Deliverables = nil
	}

	if d.Workspace != nil {
		ws, err := d.Workspace.Load()
		if err == nil && len(ws.CriticalPath) > 0 {
			byID := make(map[string]domain.Item, len(items))
			for _, it := range items {
				byID[it.ID] = it
			}
			steps := make([]CriticalPathStep, 0, len(ws.CriticalPath))
			for _, id := range ws.CriticalPath {
				step := CriticalPathStep{ID: id}
				if it, ok := byID[id]; ok {
					step.Status = it.Status
					step.Title = it.Title
					step.Found = true
				}
				steps = append(steps, step)
			}
			out.CriticalPath = steps
		}
	}

	return out, nil
}

func incrementCount(c *StatusCounts, s domain.Status) {
	switch s {
	case domain.StatusBacklog:
		c.Backlog++
	case domain.StatusReady:
		c.Ready++
	case domain.StatusClaimed:
		c.Claimed++
	case domain.StatusInProgress:
		c.InProgress++
	case domain.StatusReview:
		c.Review++
	case domain.StatusBlocked:
		c.Blocked++
	case domain.StatusDone:
		c.Done++
	case domain.StatusAbandoned:
		c.Abandoned++
	}
}
