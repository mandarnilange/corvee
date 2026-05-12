package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ImportInput is the request payload for Import.
type ImportInput struct {
	// Body is the raw JSON document to import.
	Body []byte
	// Prefix overrides the project code derived from the doc's
	// project_id (phases-and-tasks format only).
	Prefix string
	// DryRun, when true, validates and reports without writing.
	DryRun bool
	// Agent stamps CreatedBy on every imported item.
	Agent string
	// AgentRole stamps CreatedByRole.
	AgentRole domain.Role
}

// ImportOutput is the response payload for Import.
type ImportOutput struct {
	// Created is the IDs that were (or would be on dry-run) created.
	Created []string `json:"created"`
	// Skipped lists items that already existed.
	Skipped []ImportSkip `json:"skipped,omitempty"`
	// Format is the detected input format ("native" | "phases-and-tasks").
	Format string `json:"format"`
}

// ImportSkip records why an input item was not imported.
type ImportSkip struct {
	// ID is the input item's identifier.
	ID string `json:"id"`
	// Reason is a short human-readable cause.
	Reason string `json:"reason"`
}

// nativeDoc is the array-of-Items shape recognised by Import.
type nativeDoc = []domain.Item

// phasesDoc is the AgentForge-style nested shape: a project metadata
// header plus a list of phases, each with a list of tasks.
type phasesDoc struct {
	SchemaVersion int       `json:"schema_version"`
	ProjectID     string    `json:"project_id"`
	ProjectName   string    `json:"project_name"`
	CriticalPath  []string  `json:"critical_path"`
	Phases        []phaseIn `json:"phases"`
}

type phaseIn struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Deliverable string         `json:"deliverable"`
	Status      domain.Status  `json:"status"`
	CompletedAt *time.Time     `json:"completed_at"`
	Tasks       []taskIn       `json:"tasks"`
	Extras      map[string]any `json:"-"`
}

type taskIn struct {
	ID                   string          `json:"id"`
	Title                string          `json:"title"`
	Description          string          `json:"description"`
	Type                 domain.ItemType `json:"type"`
	Kind                 domain.Kind     `json:"kind"`
	Priority             domain.Priority `json:"priority"`
	EstimatedHours       float64         `json:"estimatedHours"`
	Files                []string        `json:"files"`
	RequiredCapabilities []string        `json:"required_capabilities"`
	Dependencies         []string        `json:"dependencies"`
	AcceptanceCriteria   []string        `json:"acceptanceCriteria"`
	Status               domain.Status   `json:"status"`
	CompletedAt          *time.Time      `json:"completed_at"`
}

// Import bulk-creates items from a planning JSON document. The format
// is auto-detected: an array → native; an object with a "phases"
// field → phases-and-tasks. Atomicity: dry-run validation runs across
// every item before any Put; if any item fails, nothing is written.
func Import(ctx context.Context, d Deps, in ImportInput) (ImportOutput, error) {
	if d.Store == nil && !in.DryRun {
		return ImportOutput{}, fmt.Errorf("import: store not wired: %w", domain.ErrUsage)
	}
	if len(in.Body) == 0 {
		return ImportOutput{}, fmt.Errorf("import: empty body: %w", domain.ErrUsage)
	}

	trimmed := strings.TrimLeft(string(in.Body), " \t\n\r")
	if strings.HasPrefix(trimmed, "[") {
		return importNative(ctx, d, in)
	}
	if strings.HasPrefix(trimmed, "{") {
		return importPhases(ctx, d, in)
	}
	return ImportOutput{}, fmt.Errorf("import: unrecognised format: %w", domain.ErrUsage)
}

// importNative ingests an array of full Items. Missing fields are
// filled with sensible defaults (schema_version, version=1, timestamps,
// CreatedBy/CreatedByRole). Items are validated structurally before any
// write.
func importNative(ctx context.Context, d Deps, in ImportInput) (ImportOutput, error) {
	var docs nativeDoc
	if err := json.Unmarshal(in.Body, &docs); err != nil {
		return ImportOutput{}, fmt.Errorf("import: parse native: %w: %w", err, domain.ErrUsage)
	}
	now := clockNow(d)
	plan := make([]domain.Item, 0, len(docs))
	for _, raw := range docs {
		item := stampItem(raw, in, now)
		if err := validateImportItem(item); err != nil {
			return ImportOutput{}, err
		}
		plan = append(plan, item)
	}
	return commitPlan(ctx, d, in, plan, "native")
}

// importPhases ingests AgentForge-style phases-and-tasks. Each phase
// becomes an epic; each task becomes a story (or whatever its
// declared type is). The doc's project_id seeds the project item and
// is used as the parent prefix for synthesised IDs.
func importPhases(ctx context.Context, d Deps, in ImportInput) (ImportOutput, error) {
	var doc phasesDoc
	if err := json.Unmarshal(in.Body, &doc); err != nil {
		return ImportOutput{}, fmt.Errorf("import: parse phases: %w: %w", err, domain.ErrUsage)
	}
	if doc.ProjectID == "" {
		return ImportOutput{}, fmt.Errorf("import: phases doc missing project_id: %w", domain.ErrUsage)
	}
	if in.Prefix != "" {
		doc.ProjectID = in.Prefix
	}
	now := clockNow(d)
	plan := make([]domain.Item, 0, 1+len(doc.Phases)*8)

	// Project item.
	plan = append(plan, domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            doc.ProjectID,
		Type:          domain.TypeProject,
		ProjectID:     doc.ProjectID,
		Title:         coalesce(doc.ProjectName, doc.ProjectID),
		Status:        domain.StatusReady,
		CreatedAt:     now,
		UpdatedAt:     now,
		Version:       1,
		CreatedBy:     in.Agent,
		CreatedByRole: in.AgentRole,
	})

	for _, ph := range doc.Phases {
		epicItem := domain.Item{
			SchemaVersion: domain.CurrentSchemaVersion,
			ID:            ph.ID,
			Type:          domain.TypeEpic,
			ProjectID:     doc.ProjectID,
			ParentID:      doc.ProjectID,
			Title:         coalesce(ph.Name, ph.ID),
			Description:   ph.Description,
			Deliverable:   ph.Deliverable,
			Status:        coalesceStatus(ph.Status, domain.StatusReady),
			CreatedAt:     now,
			UpdatedAt:     now,
			Version:       1,
			CreatedBy:     in.Agent,
			CreatedByRole: in.AgentRole,
			CompletedAt:   ph.CompletedAt,
		}
		plan = append(plan, epicItem)

		for _, t := range ph.Tasks {
			storyItem := domain.Item{
				SchemaVersion:        domain.CurrentSchemaVersion,
				ID:                   t.ID,
				Type:                 coalesceType(t.Type, domain.TypeStory),
				Kind:                 t.Kind,
				ProjectID:            doc.ProjectID,
				ParentID:             ph.ID,
				Title:                t.Title,
				Description:          t.Description,
				Priority:             t.Priority,
				Status:               coalesceStatus(t.Status, domain.StatusReady),
				RequiredCapabilities: t.RequiredCapabilities,
				Dependencies:         t.Dependencies,
				AcceptanceCriteria:   t.AcceptanceCriteria,
				CompletedAt:          t.CompletedAt,
				CreatedAt:            now,
				UpdatedAt:            now,
				Version:              1,
				CreatedBy:            in.Agent,
				CreatedByRole:        in.AgentRole,
				Impact:               domain.Impact{Files: t.Files, EstimatedHours: t.EstimatedHours},
			}
			if err := validateImportItem(storyItem); err != nil {
				return ImportOutput{}, err
			}
			plan = append(plan, storyItem)
		}
	}

	// Critical path lifts from the doc into workspace config (best
	// effort — failures don't abort the import).
	if !in.DryRun && len(doc.CriticalPath) > 0 && d.Workspace != nil {
		ws, err := d.Workspace.Load()
		if err == nil {
			ws.CriticalPath = append([]string(nil), doc.CriticalPath...)
			_ = d.Workspace.Save(ws)
		}
	}

	return commitPlan(ctx, d, in, plan, "phases-and-tasks")
}

// commitPlan runs the validated plan through Store.Put (skipping
// existing items) unless in.DryRun.
func commitPlan(ctx context.Context, d Deps, in ImportInput, plan []domain.Item, format string) (ImportOutput, error) {
	out := ImportOutput{Format: format}
	if in.DryRun {
		out.Created = make([]string, 0, len(plan))
		for _, it := range plan {
			out.Created = append(out.Created, it.ID)
		}
		return out, nil
	}
	for _, it := range plan {
		exists, err := d.Store.Exists(ctx, it.ID)
		if err != nil {
			return ImportOutput{}, fmt.Errorf("import: probe %s: %w", it.ID, err)
		}
		if exists {
			out.Skipped = append(out.Skipped, ImportSkip{ID: it.ID, Reason: "already exists"})
			continue
		}
		if _, err := d.Store.Put(ctx, it, -1); err != nil {
			return ImportOutput{}, fmt.Errorf("import: put %s: %w", it.ID, err)
		}
		out.Created = append(out.Created, it.ID)
	}
	return out, nil
}

// validateImportItem performs the structural checks that are required
// for an item to be persistable: ID parses, Type implied by depth
// matches Type field, Status is a known enum.
func validateImportItem(it domain.Item) error {
	if it.ID == "" {
		return fmt.Errorf("import: item missing id: %w", domain.ErrSchemaInvalid)
	}
	parsed, err := domain.ParseID(it.ID)
	if err != nil {
		return fmt.Errorf("import: %s: %w", it.ID, err)
	}
	if it.Type != "" && parsed.Type() != it.Type {
		return fmt.Errorf("import: %s: id implies %s but type=%s: %w",
			it.ID, parsed.Type(), it.Type, domain.ErrSchemaInvalid)
	}
	if it.Status != "" && !it.Status.IsValid() {
		return fmt.Errorf("import: %s: invalid status %q: %w", it.ID, it.Status, domain.ErrSchemaInvalid)
	}
	if it.Title == "" && it.Type != domain.TypeProject {
		return fmt.Errorf("import: %s: title is required: %w", it.ID, domain.ErrSchemaInvalid)
	}
	return nil
}

// stampItem fills in the structural defaults a native-format input
// can omit: schema_version, version, timestamps, creator metadata.
// Caller-supplied non-zero values are preserved.
func stampItem(in domain.Item, args ImportInput, now time.Time) domain.Item {
	out := in
	if out.SchemaVersion == 0 {
		out.SchemaVersion = domain.CurrentSchemaVersion
	}
	if out.Version == 0 {
		out.Version = 1
	}
	if out.CreatedAt.IsZero() {
		out.CreatedAt = now
	}
	if out.UpdatedAt.IsZero() {
		out.UpdatedAt = now
	}
	if out.Status == "" {
		out.Status = domain.StatusReady
	}
	if out.CreatedBy == "" {
		out.CreatedBy = args.Agent
	}
	if out.CreatedByRole == "" {
		out.CreatedByRole = args.AgentRole
	}
	if out.Type == "" {
		if parsed, err := domain.ParseID(out.ID); err == nil {
			out.Type = parsed.Type()
		}
	}
	if out.ProjectID == "" {
		if parsed, err := domain.ParseID(out.ID); err == nil {
			out.ProjectID = parsed.Project
		}
	}
	if out.ParentID == "" && out.Type != domain.TypeProject {
		if parsed, err := domain.ParseID(out.ID); err == nil {
			out.ParentID = parsed.Parent().String()
		}
	}
	return out
}

func coalesce(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func coalesceStatus(a, b domain.Status) domain.Status {
	if a != "" {
		return a
	}
	return b
}

func coalesceType(a, b domain.ItemType) domain.ItemType {
	if a != "" {
		return a
	}
	return b
}

func clockNow(d Deps) time.Time {
	if d.Clock == nil {
		return time.Now().UTC()
	}
	return d.Clock.Now()
}
