package domain

import "time"

// ItemType is the structural hierarchy enum: project / epic / story /
// subtask. See spec §5.
type ItemType string

// ItemType values per §14.2.
const (
	// TypeProject is the root of an item hierarchy (e.g. "RKN").
	TypeProject ItemType = "project"
	// TypeEpic groups stories under a project (e.g. "RKN-E01").
	TypeEpic ItemType = "epic"
	// TypeStory groups subtasks under an epic (e.g. "RKN-E01-S03").
	TypeStory ItemType = "story"
	// TypeSubtask is a leaf unit of work (e.g. "RKN-E01-S03-T02").
	TypeSubtask ItemType = "subtask"
)

// IsValid reports whether t is one of the four ItemType constants.
func (t ItemType) IsValid() bool {
	switch t {
	case TypeProject, TypeEpic, TypeStory, TypeSubtask:
		return true
	}
	return false
}

// Status is the lifecycle state of an item per §5. Transition rules
// live in status.go (TTR-E01-S02).
type Status string

// Status values per §14.2.
const (
	// StatusBacklog is the initial state for newly-created items that
	// are not yet ready for work.
	StatusBacklog Status = "backlog"
	// StatusReady marks an item as eligible to be claimed by an executor.
	StatusReady Status = "ready"
	// StatusClaimed indicates an executor holds a lease on the item.
	StatusClaimed Status = "claimed"
	// StatusInProgress indicates the executor has begun the work.
	StatusInProgress Status = "in_progress"
	// StatusReview indicates the work is awaiting a reviewer.
	StatusReview Status = "review"
	// StatusBlocked indicates the executor is waiting on an external
	// dependency.
	StatusBlocked Status = "blocked"
	// StatusDone marks the item as complete.
	StatusDone Status = "done"
	// StatusAbandoned marks the item as soft-deleted or otherwise
	// terminated without completion.
	StatusAbandoned Status = "abandoned"
)

// IsValid reports whether s is one of the eight Status constants.
func (s Status) IsValid() bool {
	switch s {
	case StatusBacklog, StatusReady, StatusClaimed, StatusInProgress,
		StatusReview, StatusBlocked, StatusDone, StatusAbandoned:
		return true
	}
	return false
}

// Priority is the urgency enum per §5.
type Priority string

// Priority values per §14.2.
const (
	// PriorityCritical is reserved for must-ship items.
	PriorityCritical Priority = "critical"
	// PriorityHigh is for items that should ship soon.
	PriorityHigh Priority = "high"
	// PriorityMedium is the default for routine work.
	PriorityMedium Priority = "medium"
	// PriorityLow is for nice-to-haves.
	PriorityLow Priority = "low"
)

// IsValid reports whether p is one of the four Priority constants.
func (p Priority) IsValid() bool {
	switch p {
	case PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow:
		return true
	}
	return false
}

// Kind captures the nature of work, orthogonal to ItemType. See §5.
type Kind string

// Kind values per §14.2.
const (
	// KindFeature is new functionality.
	KindFeature Kind = "feature"
	// KindBug is a defect fix.
	KindBug Kind = "bug"
	// KindChore is maintenance work.
	KindChore Kind = "chore"
	// KindSpike is investigative or exploratory work.
	KindSpike Kind = "spike"
	// KindDocs is documentation-only changes.
	KindDocs Kind = "docs"
	// KindRefactor is non-behavioral code restructuring.
	KindRefactor Kind = "refactor"
)

// IsValid reports whether k is one of the six Kind constants.
func (k Kind) IsValid() bool {
	switch k {
	case KindFeature, KindBug, KindChore, KindSpike, KindDocs, KindRefactor:
		return true
	}
	return false
}

// Risk is the impact-risk enum embedded in Item.Impact. See §5.
type Risk string

// Risk values per §14.2.
const (
	// RiskLow indicates minimal blast radius.
	RiskLow Risk = "low"
	// RiskMedium indicates moderate blast radius.
	RiskMedium Risk = "medium"
	// RiskHigh indicates large blast radius warranting extra review.
	RiskHigh Risk = "high"
)

// IsValid reports whether r is one of the three Risk constants.
func (r Risk) IsValid() bool {
	switch r {
	case RiskLow, RiskMedium, RiskHigh:
		return true
	}
	return false
}

// Role is the agent-role enum used in Item.CreatedByRole and the
// per-VM Local config. See §7.1.
type Role string

// Role values per §14.2.
const (
	// RolePlanner decomposes goals into items. Does not execute work.
	RolePlanner Role = "planner"
	// RoleExecutor pulls ready work and updates status.
	RoleExecutor Role = "executor"
	// RoleReviewer validates items in status=review.
	RoleReviewer Role = "reviewer"
	// RoleHuman is the catch-all for interactive CLI use.
	RoleHuman Role = "human"
)

// IsValid reports whether r is one of the four Role constants.
func (r Role) IsValid() bool {
	switch r {
	case RolePlanner, RoleExecutor, RoleReviewer, RoleHuman:
		return true
	}
	return false
}

// Claim is the structured lease object recorded on a claimed item.
// See spec §6 Layer 3.
type Claim struct {
	// Agent is the claimant's agent ID.
	Agent string `json:"agent"`
	// LeaseID is the ULID minted at claim time. All claim-scoped
	// operations must present this exact value.
	LeaseID string `json:"lease_id"`
	// ClaimedAt is the wall-clock time the claim was minted.
	ClaimedAt time.Time `json:"claimed_at"`
	// ExpiresAt is when the claim will be reaped if not heartbeated.
	ExpiresAt time.Time `json:"expires_at"`
}

// Impact captures the work's blast radius. See §5.
type Impact struct {
	// Files lists relative paths the work is expected to touch.
	Files []string `json:"files,omitempty"`
	// Modules lists logical modules affected.
	Modules []string `json:"modules,omitempty"`
	// EstimatedHours is the planner's hour estimate.
	EstimatedHours float64 `json:"estimated_hours,omitempty"`
	// EstimatedLOC is a coarse lines-of-code estimate.
	EstimatedLOC int `json:"estimated_loc,omitempty"`
	// Risk is the embedded risk enum.
	Risk Risk `json:"risk,omitempty"`
}

// PluginManifest is the schema of plugins/<name>/plugin.json. Loaded
// once at CLI startup by the PluginRegistry adapter and surfaced
// alongside the resolved plugin directory.
type PluginManifest struct {
	// Name is the plugin's human-readable name.
	Name string `json:"name"`
	// Description is a one-line explanation surfaced in `corvee plugins
	// list` (a future verb).
	Description string `json:"description,omitempty"`
	// Events is the lifecycle event list this plugin opts into. Empty
	// means "all events".
	Events []string `json:"events,omitempty"`
	// Command is the executable name (resolved relative to plugin
	// dir). Required.
	Command string `json:"command"`
	// AgentSafe declares the plugin is safe to invoke under
	// CORVEE_AGENT_MODE=1 — i.e. it does not block, prompt, or alter
	// the agent's view of state. Defaults to false (skipped under
	// agent mode).
	AgentSafe bool `json:"agent_safe,omitempty"`
}

// Plugin pairs a manifest with its on-disk root directory. The
// adapter that implements PluginRegistry produces these; usecase
// passes them to HookDispatcher when firing lifecycle events.
type Plugin struct {
	// Manifest is the parsed plugin.json.
	Manifest PluginManifest
	// Dir is the absolute plugin directory.
	Dir string
}

// LifecycleEvent is the JSON payload sent to plugins on stdin and to
// shell hooks via the same shape. It mirrors the event log's record
// for an item-level mutation but is shaped for external consumers
// rather than internal audit.
type LifecycleEvent struct {
	// Event is the verb (e.g. "claimed", "completed", "status_changed").
	Event string `json:"event"`
	// ItemID is the affected item.
	ItemID string `json:"item_id,omitempty"`
	// Actor is the agent that produced the event.
	Actor string `json:"actor,omitempty"`
	// From / To are status endpoints when Event is a status change.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Timestamp is the event time in RFC3339.
	Timestamp string `json:"ts,omitempty"`
	// Metadata is the open-ended payload (e.g. handoff).
	Metadata map[string]any `json:"metadata,omitempty"`
}

// PluginResponse is the JSON shape a plugin may return on stdout.
type PluginResponse struct {
	// Veto, when true and AgentSafe semantics permit, signals the
	// core operation should fail. Used sparingly.
	Veto bool `json:"veto,omitempty"`
	// Reason is a short human-readable explanation surfaced when
	// Veto is true.
	Reason string `json:"reason,omitempty"`
}

// JournalEntry is one append-only line in the per-item journal. See §5.
type JournalEntry struct {
	// Timestamp is when the entry was recorded.
	Timestamp time.Time `json:"ts"`
	// Actor is the agent ID that produced the entry.
	Actor string `json:"actor"`
	// Event is a short verb describing the entry (e.g. "claimed",
	// "completed"). Schema is intentionally open-ended per §5.
	Event string `json:"event"`
	// Note is an optional human-readable annotation.
	Note string `json:"note,omitempty"`
	// Metadata is arbitrary structured handoff data. The tool stores
	// and surfaces it without validating keys.
	Metadata map[string]any `json:"metadata,omitempty"`
}
