package usecase

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// TestAuditAppend_DispatchesViaPort verifies that auditAppend
// forwards lifecycle events through the HookDispatcher port. The
// actual exec lives in the adapter (internal/adapter/hooks); this
// test only checks the wiring and that hooksDir is derived from
// d.TasksDir.
func TestAuditAppend_DispatchesViaPort(t *testing.T) {
	t.Parallel()
	tasks := t.TempDir()
	dispatcher := testfakes.NewHookDispatcher()
	registry := testfakes.NewPluginRegistry()

	d := Deps{
		TasksDir:       tasks,
		PluginRegistry: registry,
		HookDispatcher: dispatcher,
	}
	auditAppend(d, domain.Event{
		EventID:   "EV1",
		Timestamp: time.Now().UTC(),
		Type:      "claimed",
		ItemID:    "ABC-E01-S01",
		Actor:     "alice",
	})

	if got := len(dispatcher.Calls); got != 1 {
		t.Fatalf("dispatch calls = %d, want 1", got)
	}
	c := dispatcher.Calls[0]
	if c.Event.Event != "claimed" {
		t.Errorf("event = %q, want claimed", c.Event.Event)
	}
	if c.Event.ItemID != "ABC-E01-S01" {
		t.Errorf("item_id = %q", c.Event.ItemID)
	}
	if c.HooksDir != filepath.Join(tasks, "hooks") {
		t.Errorf("hooksDir = %q", c.HooksDir)
	}
}

// TestAuditAppend_NoDispatcherIsNoop verifies that auditAppend
// short-circuits when no dispatcher is wired (typical for unit tests
// that don't exercise the hook surface).
func TestAuditAppend_NoDispatcherIsNoop(t *testing.T) {
	t.Parallel()
	d := Deps{TasksDir: "/tmp"}
	// Should not panic.
	auditAppend(d, domain.Event{Type: "claimed"})
}

// TestAuditAppend_NoHooksAndNoPluginsSkipsDispatch ensures the
// dispatcher is only called when there's something to fan out to.
// This is the bound-dispatch-latency optimization (review #2/#13).
func TestAuditAppend_NoHooksAndNoPluginsSkipsDispatch(t *testing.T) {
	t.Parallel()
	dispatcher := testfakes.NewHookDispatcher()
	d := Deps{
		HookDispatcher: dispatcher,
		// No TasksDir set → no hooksDir; no PluginRegistry → no plugins.
	}
	auditAppend(d, domain.Event{Type: "claimed"})
	if len(dispatcher.Calls) != 0 {
		t.Errorf("dispatch should be skipped when no plugins/hooks; got %d calls", len(dispatcher.Calls))
	}
}
