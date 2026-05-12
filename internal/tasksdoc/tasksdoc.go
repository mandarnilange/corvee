// Package tasksdoc is the shared schema for .spec/tasks.json — the live
// progress tracker we dogfood until Phase 7's `corvee import` ships and
// promotes tasks.json into a real .tasks/ workspace.
//
// Both cmd/tasks-validate (the make-ci gate) and cmd/tasks-mark (the
// status-update writer) import these types, so the two tools cannot
// drift from each other. If a field is added here it shows up in both
// loaders/savers/validators automatically.
//
// This package is *not* part of the runtime CLI's domain layer — it
// describes the shape of an external doc, not a domain entity. It lives
// under internal/ rather than internal/domain/ so the depguard rule
// stays clean.
package tasksdoc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// SchemaVersion is the only schema_version this package understands.
const SchemaVersion = 1

// ExpectedStoriesPerPhase is the agreed Phase 0 commitment:
// 10 + 19 + 13 + 15 + 12 + 8 + 7 + 10 + 6 = 100 stories.
var ExpectedStoriesPerPhase = map[string]int{
	"TTR-E00": 10,
	"TTR-E01": 19,
	"TTR-E02": 13,
	"TTR-E03": 15,
	"TTR-E04": 12,
	"TTR-E05": 8,
	"TTR-E06": 7,
	"TTR-E07": 10,
	"TTR-E08": 6,
}

// AllowedStatuses matches spec §5's status enum exactly.
var AllowedStatuses = map[string]bool{
	"backlog":     true,
	"ready":       true,
	"claimed":     true,
	"in_progress": true,
	"review":      true,
	"blocked":     true,
	"done":        true,
	"abandoned":   true,
}

// Doc is the top-level shape of .spec/tasks.json.
//
// Field order in the struct controls JSON output order via encoding/json,
// so the serialized form keeps a logical reading order (id-first, status
// near the end) rather than alphabetical.
type Doc struct {
	SchemaVersion int      `json:"schema_version"`
	ProjectID     string   `json:"project_id"`
	ProjectName   string   `json:"project_name"`
	CriticalPath  []string `json:"critical_path"`
	Phases        []Phase  `json:"phases"`
}

// Phase is one row in Doc.Phases. Mirrors a spec.epic plus tracker fields
// (status, completed_at).
type Phase struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Deliverable string `json:"deliverable"`
	Status      string `json:"status"`
	CompletedAt string `json:"completed_at,omitempty"`
	Tasks       []Task `json:"tasks"`
}

// Task is one row in a Phase's tasks list. The shape matches §15.2's
// phases-and-tasks import contract (id, title, description, type,
// priority, estimatedHours, files, acceptanceCriteria) extended with
// the §5 native Item fields the import will use directly when each
// task becomes a story (kind, required_capabilities, dependencies,
// status, completed_at). No fields outside §5 are introduced.
type Task struct {
	ID                   string   `json:"id"`
	Title                string   `json:"title"`
	Description          string   `json:"description,omitempty"`
	Type                 string   `json:"type"`
	Kind                 string   `json:"kind"`
	Priority             string   `json:"priority"`
	EstimatedHours       float64  `json:"estimatedHours"`
	Files                []string `json:"files"`
	RequiredCapabilities []string `json:"required_capabilities"`
	Dependencies         []string `json:"dependencies"`
	AcceptanceCriteria   []string `json:"acceptanceCriteria"`
	Status               string   `json:"status"`
	CompletedAt          string   `json:"completed_at,omitempty"`
}

// Load reads and parses tasks.json. Unknown fields are rejected so any
// schema drift is caught at load time rather than silently ignored.
func Load(path string) (*Doc, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // trusted spec path
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Doc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return &doc, nil
}

// Save writes the document with a 2-space indent, matching the
// project's existing JSON style.
func Save(path string, doc *Doc) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return fmt.Errorf("encode: %w", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// Validate enforces the dogfood schema:
//
//   - schema_version must be SchemaVersion
//   - project_id non-empty
//   - every phase id is one of TTR-E00..TTR-E08
//   - every phase carries the agreed story count
//   - every story id matches its phase prefix; ids are unique
//   - every story has a non-empty title and acceptanceCriteria
//   - every dependency reference resolves to a known story id
//   - every critical_path entry resolves
//   - every phase and story has a status from AllowedStatuses
//   - status=done implies completed_at is RFC3339; otherwise completed_at
//     must be empty (no orphaned timestamps).
func (d *Doc) Validate() error {
	if d.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version = %d, want %d", d.SchemaVersion, SchemaVersion)
	}
	if d.ProjectID == "" {
		return fmt.Errorf("project_id is empty")
	}

	storyIDs := map[string]bool{}
	phaseIDs := map[string]bool{}

	for _, p := range d.Phases {
		phaseIDs[p.ID] = true
		want, known := ExpectedStoriesPerPhase[p.ID]
		if !known {
			return fmt.Errorf("phase %q is not one of TTR-E00..TTR-E08", p.ID)
		}
		if got := len(p.Tasks); got != want {
			return fmt.Errorf("phase %s has %d stories, want %d", p.ID, got, want)
		}
		if err := validateStatus(p.Status, p.CompletedAt); err != nil {
			return fmt.Errorf("phase %s: %w", p.ID, err)
		}

		for _, t := range p.Tasks {
			if t.ID == "" {
				return fmt.Errorf("phase %s contains a story with empty id", p.ID)
			}
			if !strings.HasPrefix(t.ID, p.ID+"-S") {
				return fmt.Errorf("story %s does not match phase prefix %s-S", t.ID, p.ID)
			}
			if storyIDs[t.ID] {
				return fmt.Errorf("duplicate story id %s", t.ID)
			}
			storyIDs[t.ID] = true
			if t.Title == "" {
				return fmt.Errorf("story %s has empty title", t.ID)
			}
			if len(t.AcceptanceCriteria) == 0 {
				return fmt.Errorf("story %s has empty acceptanceCriteria", t.ID)
			}
			if err := validateStatus(t.Status, t.CompletedAt); err != nil {
				return fmt.Errorf("story %s: %w", t.ID, err)
			}
		}
	}

	for id := range ExpectedStoriesPerPhase {
		if !phaseIDs[id] {
			return fmt.Errorf("phase %s missing from document", id)
		}
	}

	for _, p := range d.Phases {
		for _, t := range p.Tasks {
			for _, dep := range t.Dependencies {
				if !storyIDs[dep] {
					return fmt.Errorf("story %s depends on unknown id %s", t.ID, dep)
				}
			}
		}
	}
	for _, id := range d.CriticalPath {
		if !storyIDs[id] {
			return fmt.Errorf("critical_path references unknown story %s", id)
		}
	}
	return nil
}

func validateStatus(status, completedAt string) error {
	if status == "" {
		return fmt.Errorf("status is required (one of: backlog, ready, claimed, in_progress, review, blocked, done, abandoned)")
	}
	if !AllowedStatuses[status] {
		return fmt.Errorf("status %q is not in spec §5 enum", status)
	}
	if status == "done" {
		if completedAt == "" {
			return fmt.Errorf("status=done requires completed_at")
		}
		if _, err := time.Parse(time.RFC3339, completedAt); err != nil {
			return fmt.Errorf("completed_at %q is not RFC3339: %w", completedAt, err)
		}
	} else if completedAt != "" {
		return fmt.Errorf("completed_at is set but status=%q is not done", status)
	}
	return nil
}
