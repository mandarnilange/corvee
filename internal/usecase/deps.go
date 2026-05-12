package usecase

import (
	"github.com/mandarnilange/corvee/internal/domain"
)

// Deps groups all interface dependencies a usecase might need.
// Individual usecases use only the subset they require.
//
// Phase 1 wires Store, Clock, IDGen, Workspace, and Local; later
// phases extend with EventLog, OpLog, Locker, and Indexer.
type Deps struct {
	// Store persists items.
	Store domain.Store
	// Clock supplies usecase-controlled time.
	Clock domain.Clock
	// IDGen generates ULIDs for lease_id and op_id.
	IDGen domain.IDGen
	// Workspace loads/saves the committed workspace config.
	Workspace domain.WorkspaceLoader
	// Local loads/saves the gitignored per-VM config.
	Local domain.LocalLoader
	// EventLog appends audit-trail events. Phase 2+.
	EventLog domain.EventLog
	// Indexer reads/writes the workspace cache. Phase 2+.
	Indexer domain.Indexer
	// Locker takes per-item cross-process locks. Phase 3+.
	Locker domain.Locker
	// OpResults records idempotency-key outcomes. Phase 3+.
	OpResults domain.OpResultStore
	// OpJournal is the WAL journal for crash-safe multi-file ops. Phase 4+.
	OpJournal domain.OpJournal
	// EventTailer is the live event stream for `corvee watch`. Phase 5+.
	EventTailer domain.EventTailer
	// Renderer produces the static HTML dashboard. Phase 6+.
	Renderer domain.Renderer
	// Git exposes git-repo metadata (HEAD SHA) used by the deploy
	// manifest. Phase 6+. May be nil when running outside a repo;
	// usecase.Render falls back to a dev version stamp.
	Git domain.GitInfo
	// GitClient is the read/write surface used by Sync and Merge
	// (Phase 7+). nil when running outside a repo.
	GitClient domain.GitClient
	// TasksDir is the absolute path to the .tasks/ workspace
	// directory. Phase 7+ usecases that work outside the per-item
	// Store boundary (Sync, Merge, Compact, plugin/hook discovery)
	// resolve relative paths against it.
	TasksDir string
	// PluginRegistry exposes the discovered plugin set
	// (.tasks/plugins/<name>/), loaded once at CLI startup. Phase 8.
	// nil disables plugin dispatch.
	PluginRegistry domain.PluginRegistry
	// HookDispatcher is the syscall-owning port that fires plugins
	// and shell hooks for lifecycle events. nil disables dispatch.
	HookDispatcher domain.HookDispatcher
	// AgentMode is true when the CLI is invoked under CORVEE_AGENT_MODE
	// or with --agent. Plugins without agent_safe: true are skipped.
	AgentMode bool
}
