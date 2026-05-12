package usecase

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/mandarnilange/corvee/internal/domain"
)

// auditAppend writes ev to d.EventLog if one is wired. A failed
// append is logged at warn level via the stdlib slog default logger
// rather than bubbling up — the user-visible state (the Item write
// the event describes) has already been persisted, and the spec
// treats a missing event as recoverable: `corvee validate` and the
// future `corvee recover` (Phase 4) detect events-without-state and
// state-without-events.
//
// Returning an error here would force callers to either roll back the
// item write (impossible without a WAL — Phase 4 territory) or
// double-write on retry. Surfacing the warning to stderr is the
// pragmatic middle ground.
//
// Phase 8 extension: after a successful append, plugins and shell
// hooks are dispatched via HookDispatcher (an adapter port). Plugin
// failures are logged inside the adapter and not surfaced; only a
// plugin veto bubbles up here, but we log + swallow that too — the
// audit append already happened, so vetoing post-hoc would corrupt
// state.
func auditAppend(d Deps, ev domain.Event) {
	if d.EventLog != nil {
		if err := d.EventLog.Append(ev); err != nil {
			slog.Warn("eventlog append failed",
				slog.String("event_id", ev.EventID),
				slog.String("event_type", ev.Type),
				slog.String("item_id", ev.ItemID),
				slog.String("error", err.Error()),
			)
		}
	}
	dispatchHooks(d, ev)
}

// dispatchHooks notifies plugins and shell hooks about a lifecycle
// event. Best-effort: errors are logged at warn level. The dispatcher
// adapter (internal/adapter/hooks) caps each plugin/hook at a short
// timeout so a hung helper can't stall the user-facing CLI.
func dispatchHooks(d Deps, ev domain.Event) {
	if d.HookDispatcher == nil {
		return
	}
	hooksDir := ""
	if d.TasksDir != "" {
		hooksDir = filepath.Join(d.TasksDir, "hooks")
	}
	var plugins []domain.Plugin
	if d.PluginRegistry != nil {
		plugins = d.PluginRegistry.Discover()
	}
	if len(plugins) == 0 && hooksDir == "" {
		return
	}
	lc := domain.LifecycleEvent{
		Event:    ev.Type,
		ItemID:   ev.ItemID,
		Actor:    ev.Actor,
		Metadata: ev.Metadata,
	}
	if !ev.Timestamp.IsZero() {
		lc.Timestamp = ev.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z")
	}
	if from, ok := ev.Metadata["from"].(string); ok {
		lc.From = from
	}
	if to, ok := ev.Metadata["to"].(string); ok {
		lc.To = to
	}
	if err := d.HookDispatcher.Dispatch(context.Background(), plugins, hooksDir, lc, d.AgentMode); err != nil {
		slog.Warn("plugin/hook dispatch error",
			slog.String("event", ev.Type),
			slog.String("item_id", ev.ItemID),
			slog.String("error", err.Error()),
		)
	}
}

// nextEventID returns a fresh ULID from d.IDGen for use as event_id.
// Returns the empty string when no IDGen is wired (test scaffolding
// without idgen — tests that care about event_id supply one).
func nextEventID(d Deps) string {
	if d.IDGen == nil {
		return ""
	}
	return d.IDGen.ULID()
}
