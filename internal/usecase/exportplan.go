package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ExportPlanInput is the request payload for ExportPlan.
type ExportPlanInput struct {
	// Format is one of "native" | "phases" | "markdown".
	Format string
	// ProjectCode, when non-empty, filters to one project.
	ProjectCode string
}

// ExportPlanOutput is the response payload for ExportPlan.
type ExportPlanOutput struct {
	// Format echoes the requested format.
	Format string `json:"format"`
	// Items is the count of items emitted.
	Items int `json:"items"`
	// Body is the rendered document. Callers route it to stdout or a
	// file at the CLI layer.
	Body []byte `json:"-"`
}

// ExportPlan produces a planning document in one of three formats.
// It is the inverse of Import: a round-trip of a native export
// followed by an Import yields the same item set.
func ExportPlan(ctx context.Context, d Deps, in ExportPlanInput) (ExportPlanOutput, error) {
	if d.Store == nil {
		return ExportPlanOutput{}, fmt.Errorf("export-plan: store not wired: %w", domain.ErrUsage)
	}
	format := in.Format
	if format == "" {
		format = "native"
	}
	switch format {
	case "native", "phases", "markdown":
	default:
		return ExportPlanOutput{}, fmt.Errorf("export-plan: unknown format %q: %w", format, domain.ErrUsage)
	}

	filter := domain.ListFilter{ProjectID: in.ProjectCode}
	items, err := d.Store.List(ctx, filter)
	if err != nil {
		return ExportPlanOutput{}, fmt.Errorf("export-plan: list: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	switch format {
	case "native":
		body, err := json.MarshalIndent(items, "", "  ")
		if err != nil {
			return ExportPlanOutput{}, fmt.Errorf("export-plan: marshal: %w", err)
		}
		return ExportPlanOutput{Format: format, Items: len(items), Body: body}, nil
	case "phases":
		return exportPlanPhases(items)
	case "markdown":
		return exportPlanMarkdown(items)
	}
	return ExportPlanOutput{}, fmt.Errorf("export-plan: unhandled format: %w", domain.ErrUsage)
}

// exportPhasesDoc mirrors the import-side phases doc; defined locally
// so the export side can shape JSON keys identically.
type exportPhasesDoc struct {
	SchemaVersion int                `json:"schema_version"`
	ProjectID     string             `json:"project_id"`
	ProjectName   string             `json:"project_name"`
	CriticalPath  []string           `json:"critical_path,omitempty"`
	Phases        []exportPhaseEntry `json:"phases"`
}

type exportPhaseEntry struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Deliverable string            `json:"deliverable,omitempty"`
	Status      domain.Status     `json:"status,omitempty"`
	Tasks       []exportTaskEntry `json:"tasks"`
}

type exportTaskEntry struct {
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	Description          string          `json:"description,omitempty"`
	Type                 domain.ItemType `json:"type"`
	Kind                 domain.Kind     `json:"kind,omitempty"`
	Priority             domain.Priority `json:"priority,omitempty"`
	EstimatedHours       float64         `json:"estimatedHours,omitempty"`
	Files                []string        `json:"files,omitempty"`
	RequiredCapabilities []string        `json:"required_capabilities,omitempty"`
	Dependencies         []string        `json:"dependencies,omitempty"`
	AcceptanceCriteria   []string        `json:"acceptanceCriteria,omitempty"`
	Status               domain.Status   `json:"status,omitempty"`
}

func exportPlanPhases(items []domain.Item) (ExportPlanOutput, error) {
	if len(items) == 0 {
		return ExportPlanOutput{Format: "phases"}, nil
	}
	// First project we encounter wins.
	var doc exportPhasesDoc
	doc.SchemaVersion = 1
	for _, it := range items {
		if it.Type == domain.TypeProject {
			doc.ProjectID = it.ID
			doc.ProjectName = it.Title
			break
		}
	}
	// Group epics → tasks under their parent epic.
	epicByID := map[string]*exportPhaseEntry{}
	for _, it := range items {
		if it.Type == domain.TypeEpic {
			ph := exportPhaseEntry{
				ID:          it.ID,
				Name:        it.Title,
				Description: it.Description,
				Deliverable: it.Deliverable,
				Status:      it.Status,
			}
			doc.Phases = append(doc.Phases, ph)
			epicByID[it.ID] = &doc.Phases[len(doc.Phases)-1]
		}
	}
	for _, it := range items {
		if it.Type != domain.TypeStory {
			continue
		}
		ph, ok := epicByID[it.ParentID]
		if !ok {
			continue
		}
		ph.Tasks = append(ph.Tasks, exportTaskEntry{
			ID:                   it.ID,
			Title:                it.Title,
			Description:          it.Description,
			Type:                 it.Type,
			Kind:                 it.Kind,
			Priority:             it.Priority,
			EstimatedHours:       it.Impact.EstimatedHours,
			Files:                it.Impact.Files,
			RequiredCapabilities: it.RequiredCapabilities,
			Dependencies:         it.Dependencies,
			AcceptanceCriteria:   it.AcceptanceCriteria,
			Status:               it.Status,
		})
	}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ExportPlanOutput{}, fmt.Errorf("export-plan phases: %w", err)
	}
	return ExportPlanOutput{Format: "phases", Items: len(items), Body: body}, nil
}

func exportPlanMarkdown(items []domain.Item) (ExportPlanOutput, error) {
	var b strings.Builder
	b.WriteString("# Backlog\n\n")
	for _, it := range items {
		switch it.Type {
		case domain.TypeProject:
			fmt.Fprintf(&b, "# %s — %s\n\n", it.ID, it.Title)
		case domain.TypeEpic:
			fmt.Fprintf(&b, "## %s — %s\n\n", it.ID, it.Title)
			if it.Deliverable != "" {
				fmt.Fprintf(&b, "**Deliverable:** %s\n\n", it.Deliverable)
			}
		case domain.TypeStory:
			fmt.Fprintf(&b, "### %s — %s\n\n", it.ID, it.Title)
			if it.Description != "" {
				fmt.Fprintf(&b, "%s\n\n", it.Description)
			}
			if len(it.AcceptanceCriteria) > 0 {
				b.WriteString("**Acceptance criteria:**\n")
				for _, a := range it.AcceptanceCriteria {
					fmt.Fprintf(&b, "- %s\n", a)
				}
				b.WriteByte('\n')
			}
		case domain.TypeSubtask:
			fmt.Fprintf(&b, "- **%s** — %s\n", it.ID, it.Title)
		}
	}
	return ExportPlanOutput{Format: "markdown", Items: len(items), Body: []byte(b.String())}, nil
}
