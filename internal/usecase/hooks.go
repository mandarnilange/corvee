package usecase

import (
	"github.com/mandarnilange/corvee/internal/domain"
)

// Plugin and PluginManifest re-exports — kept so existing tests and
// downstream callers don't need to update their imports. The
// authoritative types live in domain (post-review-#1).
type Plugin = domain.Plugin

// PluginManifest re-exports domain.PluginManifest for the same reason.
type PluginManifest = domain.PluginManifest

// LifecycleEvent re-exports the domain type so usecase consumers can
// build event payloads without importing domain directly.
type LifecycleEvent = domain.LifecycleEvent

// PluginResponse re-exports domain.PluginResponse.
type PluginResponse = domain.PluginResponse

// ErrPluginVeto is the sentinel returned by HookDispatcher when a
// plugin sets veto: true on its response. Re-exported for callers.
var ErrPluginVeto = domain.ErrPluginVeto
