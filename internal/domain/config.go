package domain

// CurrentSchemaVersion is the schema_version this binary writes for
// items and workspace config. Older items are upgraded lazily; items
// with a higher schema_version trigger an upgrade-prompt error
// (TTR-E01-S05).
const CurrentSchemaVersion = 1

// GitConfig is the git-integration sub-config of Workspace.
//
// AutoSyncSeconds is a pointer so the absence of a value (the spec's
// `null` default) round-trips cleanly through JSON. nil means "no
// background sync"; a non-nil value means "sync every N seconds".
type GitConfig struct {
	// AutoCommit, when true, runs `git add` + `git commit` after each
	// mutating CLI command.
	AutoCommit bool `json:"auto_commit"`
	// AutoSyncSeconds, when non-nil, drives a periodic `corvee sync` at
	// that cadence. nil = disabled.
	AutoSyncSeconds *int `json:"auto_sync_seconds"`
}

// Workspace is the committed-to-git workspace config persisted at
// .tasks/workspace.json. Stable across VMs; safe to git sync. Per-VM
// agent identity lives in Local instead.
type Workspace struct {
	// SchemaVersion pins the workspace.json schema for forward-compat.
	SchemaVersion int `json:"schema_version"`
	// WorkspaceName is a human-readable label.
	WorkspaceName string `json:"workspace_name"`
	// DefaultProject is the project code prepended when an ID is given
	// without one (e.g. "E01-S03" → "RKN-E01-S03").
	DefaultProject string `json:"default_project,omitempty"`
	// ClaimTTLMinutes is the default lease duration for `corvee claim`.
	ClaimTTLMinutes int `json:"claim_ttl_minutes"`
	// HeartbeatIntervalMinutes is how often executors should refresh
	// their leases.
	HeartbeatIntervalMinutes int `json:"heartbeat_interval_minutes"`
	// CriticalPath is the ordered list of item IDs that must complete
	// in sequence; consulted by `corvee next` for selection bonuses.
	CriticalPath []string `json:"critical_path"`
	// Git is the git-integration sub-config.
	Git GitConfig `json:"git"`
}

// Local is the per-VM configuration persisted at .tasks/local.json.
// gitignored — never synced — so multiple VMs don't merge-conflict
// over their own agent identities. Resolves SQ-003 in
// docs/spec-questions.md and intentionally diverges from spec §5.1's
// single config.json design.
type Local struct {
	// DefaultAgent is this VM's agent ID, used when no --agent flag is
	// passed to a CLI invocation.
	DefaultAgent string `json:"default_agent,omitempty"`
	// AgentRole is the role this VM acts in (planner|executor|
	// reviewer|human). Defaults to human when absent.
	AgentRole Role `json:"agent_role,omitempty"`
	// AgentCapabilities is the list of capability tags this VM
	// advertises to `corvee next --match-capabilities`.
	AgentCapabilities []string `json:"agent_capabilities,omitempty"`
}
