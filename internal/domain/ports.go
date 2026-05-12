package domain

import (
	"context"
	"time"
)

// Clock abstracts time.Now() so usecases can be tested with a fixed
// clock. Production impl returns time.Now().UTC(); tests inject a
// FixedClock or SequenceClock for determinism.
type Clock interface {
	// Now returns the current wall-clock time in UTC.
	Now() time.Time
}

// IDGen abstracts ULID generation for testability. Production impl
// uses a real ULID source; tests inject a deterministic sequence.
type IDGen interface {
	// ULID returns a sortable, monotonically-increasing identifier
	// suitable for lease_id and op_id.
	ULID() string
}

// Store is the persistence boundary for items. The fsstore adapter
// implements it for production; testfakes.Store implements it for
// unit tests. Implementations must be safe for concurrent use within
// the single-process model Phase 1 targets.
//
// Per spec §14.1 with the contract clarification from S07: Put
// persists exactly the Item the caller supplied — Version and
// UpdatedAt are set by the calling usecase, never by the Store. This
// keeps time and version intent in the application layer.
type Store interface {
	// Get returns the item with the given ID. Returns
	// (Item{}, ErrNotFound) if no such item exists.
	Get(ctx context.Context, id string) (Item, error)

	// List returns items matching filter, AND-combined across fields.
	// An empty filter returns every item. Sort/Order honor the §14.2
	// keys; an unrecognised Sort yields an arbitrary but stable order.
	List(ctx context.Context, filter ListFilter) ([]Item, error)

	// Put persists item verbatim. When expectVersion >= 0, the on-disk
	// Version is compared first and the write is aborted with
	// ErrVersionConflict if it differs. expectVersion == -1 means "no
	// expectation" — appropriate for first-write of a new item. The
	// returned Item is the version that landed on disk (which equals
	// the caller's input on success).
	Put(ctx context.Context, item Item, expectVersion int) (Item, error)

	// Delete removes the item from the store. Returns ErrNotFound if
	// no such item exists. Soft-delete (status=abandoned) is performed
	// via Put, not Delete.
	Delete(ctx context.Context, id string) error

	// Exists is a cheap existence probe that does not read the file
	// payload. Used by usecases that only need a yes/no signal (e.g.
	// parent-must-exist guards in Add).
	Exists(ctx context.Context, id string) (bool, error)
}

// WorkspaceLoader is the boundary for reading and writing the
// committed-to-git workspace config (.tasks/workspace.json). Missing
// file is an integrity error — the workspace must be created via
// `corvee init` before anything else can run.
type WorkspaceLoader interface {
	// Load reads the workspace config. Returns wrapped
	// ErrIntegrityViolated if the file is missing,
	// ErrSchemaInvalid for unknown fields or malformed JSON.
	Load() (Workspace, error)
	// Save persists ws to the configured path with restrictive perms.
	Save(ws Workspace) error
	// Exists reports whether the backing file is currently present.
	// Used by Init to decide between fresh-bootstrap and
	// already-initialized branches without reading the file.
	Exists() (bool, error)
}

// LocalLoader is the boundary for reading and writing the per-VM
// local config (.tasks/local.json). Missing file is OK — defaults
// are returned (agent_role=human, default_agent=$USER if set).
type LocalLoader interface {
	// Load reads the local config, or returns sensible defaults if
	// the file is absent. ErrSchemaInvalid for unknown fields or
	// malformed JSON.
	Load() (Local, error)
	// Save persists local to the configured path with restrictive
	// perms.
	Save(local Local) error
	// Exists reports whether the backing file is currently present.
	// Init uses this to preserve an existing per-VM identity instead
	// of clobbering it on re-init.
	Exists() (bool, error)
}

// EventLog is the persistence boundary for the append-only audit log
// described in spec §6. Phase 2 introduces sharded JSONL files under
// .tasks/events/<YYYY-MM-DD>.jsonl; Append fsyncs each line so a crash
// loses at most the in-flight write.
type EventLog interface {
	// Append writes ev verbatim. EventID and Timestamp must be set by
	// the caller — the adapter is a transport, not a stamper.
	Append(ev Event) error
	// Read returns every event in time order across all shards. Used
	// by Reindex and by E2E assertions. Loads the entire log into
	// memory; prefer Scan for streaming consumers (e.g. usecase.Render
	// over a long-lived workspace).
	Read() ([]Event, error)
	// Scan invokes fn once per event in chronological order, streaming
	// the log shard-by-shard so the caller never has to hold the full
	// history in memory. Returning a non-nil error from fn aborts the
	// scan and propagates the error back to the caller; adapters must
	// stop early and must not invoke fn after termination.
	Scan(fn func(Event) error) error
}

// EventTailer is the live-stream boundary for the audit log (Phase 5
// `corvee watch`). adapter/watch implements it via fsnotify; tests inject
// an in-memory fake. The returned channel closes on ctx cancel or
// adapter failure; the contract is one event per file line per §15.2.
type EventTailer interface {
	// Tail returns a channel that emits events as they're appended to
	// the current shard. Implementations must close the channel on
	// ctx.Done() so consumers exit cleanly.
	Tail(ctx context.Context) (<-chan Event, error)
}

// Indexer is the persistence boundary for the workspace cache at
// .tasks/index.json. Implementations write atomically (tmp + rename)
// and use a Stale check to decide whether the cache is still trusted.
//
// The cache is *not* source of truth — Reindex rebuilds it from the
// per-item files at any time.
type Indexer interface {
	// Load returns the persisted Index. Returns ErrNotFound when no
	// index file exists yet (callers typically run Reindex in that
	// case).
	Load() (Index, error)
	// Save writes idx atomically. Implementations must fsync.
	Save(idx Index) error
	// Stale reports whether the persisted index lags the on-disk
	// items directory. Implementations compare max(updated_at) across
	// items with the persisted MaxItemUpdatedAt. Missing index reads
	// as stale=true.
	Stale() (bool, error)
}

// Locker is the per-item cross-process lock boundary introduced in
// Phase 3 (TTR-E03-S04). adapter/fsstore implements it via O_EXCL on
// .tasks/locks/<id>.lock; testfakes implements an in-memory variant.
//
// The contract is intentionally simple: Acquire blocks (with a budget)
// until the lock can be taken or returns ErrLockTimeout. Release frees
// the lock; double-release returns ErrUsage so the caller bug is
// surfaced rather than silently swallowed.
type Locker interface {
	// Acquire takes the per-item lock for id. Blocks up to timeout
	// before returning ErrLockTimeout. timeout==0 means "try once".
	Acquire(id string, timeout time.Duration) (LockHandle, error)
}

// LockHandle is the release contract for an acquired lock. It must be
// safe to call Release exactly once; subsequent calls return ErrUsage.
type LockHandle interface {
	// Release frees the lock. Must be called exactly once.
	Release() error
}

// OpResultStore is the idempotency-key boundary for multi-file
// operations (TTR-E03-S11). Usecases consult it before mutating to
// detect a replay, and persist the result on success so a retry of the
// same operation_id returns the original outcome rather than re-doing
// the work.
type OpResultStore interface {
	// Get returns the recorded result for opID and (true, nil) when
	// found. Returns ("", false, nil) when no record exists.
	Get(opID string) (string, bool, error)
	// Put records resultJSON for opID. Implementations must persist
	// atomically so a partial write is invisible to a concurrent Get.
	Put(opID, resultJSON string) error
}

// OpJournal is the WAL (write-ahead log) boundary for multi-file
// operations (TTR-E04-S02). It records intent before mutation, tracks
// per-step progress, and exposes pending operations for crash recovery
// via usecase.Recover. Operations in executing state that survive a
// process restart are re-driven by Recover.
type OpJournal interface {
	// Begin writes the operation intent atomically. op.OpID must be
	// pre-set by the caller (typically from IDGen.ULID()). Status is
	// forced to OpStatusExecuting by the adapter.
	Begin(op Operation) error
	// MarkStep records that step stepNum within opID is done. stepNum
	// must be a valid index into the operation's Plan.
	MarkStep(opID string, stepNum int) error
	// Complete flips the operation's status to OpStatusCompleted.
	Complete(opID string) error
	// Pending returns all operations in pending or executing state.
	Pending() ([]Operation, error)
}

// GitInfo is the boundary for cheap "what commit is this?" lookups
// used by the deploy manifest's version field. Implementations shell
// out to git via os/exec; tests inject a stub that returns a fixed
// value or an error to exercise the fallback.
type GitInfo interface {
	// SHA returns the short hash of HEAD or an error if git is
	// unavailable, the repo has no commits yet, or the call fails.
	// Callers fall back to "0.0.0-dev-<ts>" on any error per §17.2.
	SHA() (string, error)
}

// PluginRegistry discovers the plugin set available to the current
// workspace. Implementations walk a plugins/ directory, parse each
// plugin.json, and skip malformed manifests. Returning an empty
// slice when the directory is missing is normal — it is not an
// error condition.
type PluginRegistry interface {
	// Discover returns the registered plugins. Implementations are
	// allowed to log+skip malformed entries; the call must not
	// fail just because one manifest is broken.
	Discover() []Plugin
}

// HookDispatcher fans an event out to plugins and shell hooks. The
// adapter implementation owns the syscall surface (process spawning,
// file stat, stdin/stdout). usecase code calls Dispatch and inspects
// only the returned veto sentinel — it never invokes os/exec
// directly.
type HookDispatcher interface {
	// Dispatch invokes every plugin that listens for ev.Event (and is
	// agent_safe when agentMode is true), then any shell hook script
	// matching <event>.sh / all.sh under hooksDir. Returns
	// ErrPluginVeto when a plugin returns veto: true. All other
	// errors (including hook script failures) are logged by the
	// implementation and not propagated.
	Dispatch(ctx context.Context, plugins []Plugin, hooksDir string, ev LifecycleEvent, agentMode bool) error
}

// GitClient is the read/write boundary used by usecase.Sync and
// usecase.Merge for distributing the workspace across machines via
// git. Implementations shell out to the user's git binary; tests
// substitute an in-memory fake.
//
// All methods operate against a single repository; the working
// directory is fixed at construction time.
type GitClient interface {
	// PullRebase runs `git pull --rebase`. Returns ErrGitConflict when
	// the rebase pauses on conflicts; the caller is then responsible
	// for invoking usecase.Merge and continuing the rebase.
	PullRebase() error
	// Push runs `git push`. Returns a wrapped error on failure
	// (no special sentinel — Push is non-recoverable from the
	// usecase's point of view).
	Push() error
	// Add stages the given paths via `git add`. Empty paths is a no-op.
	Add(paths ...string) error
	// Commit records the staged changes with the supplied message.
	// Returns nil on success, or a wrapped error on failure. A
	// "nothing to commit" outcome is treated as success.
	Commit(message string) error
	// Mv runs `git mv from to`.
	Mv(from, to string) error
	// ConflictedFiles returns the list of paths currently in conflict
	// (per `git diff --name-only --diff-filter=U`), repository-relative.
	ConflictedFiles() ([]string, error)
	// RebaseContinue runs `git rebase --continue` after the caller has
	// staged conflict resolutions. Returns ErrGitConflict if more
	// conflicts remain.
	RebaseContinue() error
	// RebaseAbort runs `git rebase --abort`. Used by Sync to back out
	// of a partially-resolved rebase if Merge fails.
	RebaseAbort() error
	// ShowAtRev returns the contents of path at the named revision
	// (e.g. "HEAD", "MERGE_HEAD", a SHA). Used by the 3-way merge to
	// fetch the base/ours/theirs versions of a conflicted JSON file.
	// Returns ErrNotFound if the file does not exist at that rev.
	ShowAtRev(rev, path string) ([]byte, error)
	// RepoRoot returns the absolute filesystem path of the git
	// repository's top level (the output of `git rev-parse
	// --show-toplevel`). Conflicted-path resolution in usecase.Merge
	// joins repo-relative paths from `git diff --name-only` against
	// this root rather than the process cwd, so operations from a
	// subdirectory still write to the right place. Returns
	// ErrNotFound when not inside a repo.
	RepoRoot() (string, error)
}

// Renderer is the persistence boundary for the static HTML dashboard
// produced by `corvee render`. Implementations are responsible for every
// filesystem write under OutDir — usecase.Render only orchestrates,
// validates inputs, and assembles the manifest.
//
// The contract: given the supplied items, events, theme, manifest, and
// destination, the adapter writes index.html, tree.html, summary.html,
// assets/styles.css, assets/app.js, and manifest.json under OutDir.
// Returned Files are repository-relative (i.e., relative to OutDir) so
// callers can echo them in CLI output and snapshot tests can iterate
// without filesystem walking.
type Renderer interface {
	// Render writes the dashboard files described by §17.2 and
	// returns the relative file list plus aggregate stats.
	Render(ctx context.Context, in RenderInput) (RenderOutput, error)
}

// Manifest is the deploy-handoff descriptor written verbatim to
// manifest.json per spec §17.2. The field order, JSON keys, and value
// shape are part of the published contract — changing them breaks the
// vercel-skill handoff.
type Manifest struct {
	// Name is the workspace or project label the deployment skill
	// surfaces to its user.
	Name string `json:"name"`
	// Version is the synthetic build identifier of the form
	// "<git-sha>-<unix-ts>". Falls back to "0.0.0-dev-<ts>" when git
	// is unavailable.
	Version string `json:"version"`
	// GeneratedAt is the RFC3339 timestamp of render invocation.
	GeneratedAt string `json:"generated_at"`
	// Entrypoint is the static landing page (always "index.html").
	Entrypoint string `json:"entrypoint"`
	// Static is true for this artifact — there is no build step.
	Static bool `json:"static"`
	// Framework is null per §17.2 — no framework adapter is involved.
	Framework *string `json:"framework"`
	// BuildCommand is null per §17.2 — there is nothing to build.
	BuildCommand *string `json:"build_command"`
	// OutputDirectory is "." since the artifact root *is* the output.
	OutputDirectory string `json:"output_directory"`
	// Routes is the URL-to-file map consumed by the deployment skill.
	Routes []ManifestRoute `json:"routes"`
}

// ManifestRoute is one entry in Manifest.Routes.
type ManifestRoute struct {
	// Src is the request path the deployment skill rewrites from.
	Src string `json:"src"`
	// Dest is the static file the request resolves to.
	Dest string `json:"dest"`
}

// RenderStats is the aggregate count payload returned by Renderer and
// surfaced in `corvee render`'s success envelope per spec §15.2.
type RenderStats struct {
	// Projects is the number of items with type=project.
	Projects int `json:"projects"`
	// Epics is the number of items with type=epic.
	Epics int `json:"epics"`
	// Stories is the number of items with type=story.
	Stories int `json:"stories"`
	// Subtasks is the number of items with type=subtask.
	Subtasks int `json:"subtasks"`
}

// RenderInput is the request payload for Renderer.Render. Event-derived
// values (EventStats, Activity) are pre-computed by usecase.Render via
// streaming Scan so the adapter never holds the full audit log.
type RenderInput struct {
	// WorkspaceName is the human-facing label rendered in page headers.
	WorkspaceName string
	// Items is the full item set to render. The adapter may not
	// re-load from a Store; this is the source of truth for the run.
	Items []Item
	// EventStats is the workspace-wide rollup over the event log.
	EventStats EventStats
	// Activity is the recent-events slice for the dashboard's activity
	// feed, already in newest-first order and capped to the configured
	// window. The adapter renders it as-is; ordering is the usecase's
	// responsibility.
	Activity []ActivityEvent
	// Theme selects an embedded CSS theme. Validation against the
	// closed set lives in usecase; adapters may assume it's valid.
	Theme string
	// OutDir is the absolute output directory. The adapter creates it
	// (and the assets subdirectory) if missing.
	OutDir string
	// Manifest is the pre-built manifest the adapter must write to
	// manifest.json verbatim.
	Manifest Manifest
	// CriticalPath is the ordered list of workspace.json's critical_path
	// IDs (may be empty). Each entry's Status is resolved against the
	// current Items slice by the usecase before the renderer sees it.
	CriticalPath []RenderCriticalPathStep
}

// RenderCriticalPathStep is one position in the workspace's
// critical_path with its resolved current status. Mirrors the structure
// usecase.Summary returns to CLI callers — kept in domain so the
// renderer adapter doesn't import usecase.
type RenderCriticalPathStep struct {
	// ID is the item's canonical ID.
	ID string
	// Title is the item's display label (empty when Found is false).
	Title string
	// Status is the current lifecycle state (empty when Found is false).
	Status Status
	// Found reports whether the workspace currently contains an item
	// with this ID.
	Found bool
}

// EventStats is the streaming aggregate of an EventLog scan, exposed
// to the renderer for the summary view's velocity panel.
type EventStats struct {
	// CompletedCount is the number of "completed" events seen across
	// the entire log (any time window).
	CompletedCount int
	// DistinctActors is the cardinality of the actor set across all
	// events; an event with empty Actor does not contribute.
	DistinctActors int
}

// ActivityEvent is one row in the dashboard's recent-activity feed.
// The struct is intentionally narrow — only fields the summary view
// renders are exposed, so the renderer is uncoupled from the full
// Event payload (and the audit log can grow without breaking the
// renderer contract).
type ActivityEvent struct {
	// Actor is the agent ID that produced the event.
	Actor string
	// Type is the verb describing the event (e.g. "completed").
	Type string
	// ItemID is the item the event references (may be empty for
	// workspace-level events).
	ItemID string
	// Timestamp is the event's wall-clock time. Zero value when the
	// upstream event carried no timestamp.
	Timestamp time.Time
}

// RenderOutput is the response payload from Renderer.Render.
type RenderOutput struct {
	// Files is the OutDir-relative path list of every file written,
	// in the order they were emitted. Used by the CLI envelope and
	// the snapshot tests.
	Files []string `json:"files"`
	// Stats is the per-type item count rollup.
	Stats RenderStats `json:"stats"`
}

// ListFilter is the AND-combined filter passed to Store.List per
// spec §14.2. Zero-value fields are inactive — only populated fields
// constrain the result.
type ListFilter struct {
	// Statuses is the set of allowed Status values. Empty matches all.
	Statuses []Status
	// Types is the set of allowed ItemType values. Empty matches all.
	Types []ItemType
	// Kinds is the set of allowed Kind values. Empty matches all.
	Kinds []Kind
	// ProjectID, when non-empty, restricts to items in that project.
	ProjectID string
	// ParentID, when non-empty, restricts to immediate children of that
	// parent. (Use a tree walk for transitive descendants.)
	ParentID string
	// Assignee, when non-empty, restricts to items currently claimed by
	// that agent ID.
	Assignee string
	// Tags requires every listed tag to be present on the item.
	Tags []string
	// Capabilities is consulted when the caller wants only items whose
	// RequiredCapabilities are a subset of the supplied set
	// (--match-capabilities).
	Capabilities []string
	// Unblocked, when true, restricts to items whose Dependencies are
	// either empty or all in status=done. Computation may require a
	// graph traversal in the implementation.
	Unblocked bool
	// Unassigned, when true, restricts to items with no active claim.
	Unassigned bool
	// Limit caps the result count; 0 means unlimited.
	Limit int
	// Sort selects the ordering key: "priority" | "created_at" |
	// "updated_at" | "due_date". Empty selects an implementation-
	// defined default.
	Sort string
	// Order is "asc" or "desc". Empty defaults to "asc".
	Order string
}
