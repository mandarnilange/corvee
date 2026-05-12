package domain

import "time"

// Item is the unified work record discriminated by Type. The schema is
// authoritatively defined in spec §5; this struct is the source of truth
// for JSON tags and field order. Required fields per §5: SchemaVersion,
// ID, Type, Title, Status, CreatedAt, UpdatedAt, Version.
type Item struct {
	// SchemaVersion pins the per-item schema for forward-compat. See
	// spec §5 — the workspace-level binary version lives in
	// CurrentSchemaVersion (TTR-E01-S05).
	SchemaVersion int `json:"schema_version"`
	// ID is the canonical identifier per §4 (e.g. "RKN-E01-S03").
	ID string `json:"id"`
	// Type is the structural enum (project|epic|story|subtask).
	Type ItemType `json:"type"`
	// Kind is the orthogonal nature-of-work enum.
	Kind Kind `json:"kind,omitempty"`
	// ProjectID is the root-project ID this item belongs to.
	ProjectID string `json:"project_id,omitempty"`
	// ParentID is the immediate parent's ID. Empty for projects.
	ParentID string `json:"parent_id,omitempty"`
	// Title is a one-line human-readable summary.
	Title string `json:"title"`
	// Description is the long-form prose body.
	Description string `json:"description,omitempty"`
	// Status is the current lifecycle state.
	Status Status `json:"status"`
	// Priority is the urgency enum.
	Priority Priority `json:"priority,omitempty"`
	// Tags is a free-form labelling array.
	Tags []string `json:"tags,omitempty"`
	// Deliverable is the epic-only one-sentence outcome statement.
	Deliverable string `json:"deliverable,omitempty"`
	// Claim, when non-nil, indicates the item is currently leased.
	Claim *Claim `json:"claim,omitempty"`
	// RequiredCapabilities are the agent capabilities needed to claim.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	// CreatedAt is the wall-clock creation time.
	CreatedAt time.Time `json:"created_at"`
	// CreatedBy is the agent ID that created the item.
	CreatedBy string `json:"created_by,omitempty"`
	// CreatedByRole is the role that creator was acting in.
	CreatedByRole Role `json:"created_by_role,omitempty"`
	// UpdatedAt is the wall-clock time of the most recent mutation.
	UpdatedAt time.Time `json:"updated_at"`
	// CompletedAt is set when Status flips to done.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// DueDate is an optional deadline.
	DueDate *time.Time `json:"due_date,omitempty"`
	// Version is the optimistic-concurrency counter (§6 Layer 2).
	Version int `json:"version"`
	// Impact is the blast-radius descriptor.
	Impact Impact `json:"impact,omitempty"`
	// Dependencies is the IDs this item depends on (must be done first).
	Dependencies []string `json:"dependencies,omitempty"`
	// Blocks is the IDs this item blocks. Inverse of Dependencies.
	Blocks []string `json:"blocks,omitempty"`
	// Aliases is the prior IDs this item used to be known by, after
	// move/rename operations.
	Aliases []string `json:"aliases,omitempty"`
	// AcceptanceCriteria is the deduplicated, ordered list of
	// observable conditions that must hold for Status=done.
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	// Journal is the append-only history of significant events.
	Journal []JournalEntry `json:"journal,omitempty"`
}
