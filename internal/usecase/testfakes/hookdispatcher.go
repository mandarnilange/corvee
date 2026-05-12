package testfakes

import (
	"context"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// HookDispatcher is a recording fake of domain.HookDispatcher.
// Tests assert against the captured calls to verify usecase code
// dispatches lifecycle events at the right moments.
type HookDispatcher struct {
	mu    sync.Mutex
	Calls []HookDispatchCall
	// Err is returned by Dispatch when non-nil. Useful for testing
	// veto handling.
	Err error
}

// HookDispatchCall records one Dispatch invocation.
type HookDispatchCall struct {
	Plugins   []domain.Plugin
	HooksDir  string
	Event     domain.LifecycleEvent
	AgentMode bool
}

// NewHookDispatcher returns a fresh fake.
func NewHookDispatcher() *HookDispatcher { return &HookDispatcher{} }

// Dispatch implements domain.HookDispatcher.
func (h *HookDispatcher) Dispatch(_ context.Context, plugins []domain.Plugin, hooksDir string, ev domain.LifecycleEvent, agentMode bool) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Calls = append(h.Calls, HookDispatchCall{
		Plugins:   plugins,
		HooksDir:  hooksDir,
		Event:     ev,
		AgentMode: agentMode,
	})
	return h.Err
}

// PluginRegistry is a fake of domain.PluginRegistry that returns a
// configurable plugin slice.
type PluginRegistry struct {
	Plugins []domain.Plugin
}

// NewPluginRegistry returns a fake registry serving plugins.
func NewPluginRegistry(plugins ...domain.Plugin) *PluginRegistry {
	return &PluginRegistry{Plugins: plugins}
}

// Discover implements domain.PluginRegistry.
func (r *PluginRegistry) Discover() []domain.Plugin {
	out := make([]domain.Plugin, len(r.Plugins))
	copy(out, r.Plugins)
	return out
}
