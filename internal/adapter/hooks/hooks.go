package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Per-plugin / per-hook execution timeout. Kept short so a hung
// helper doesn't stall the user-facing CLI. Operators who
// legitimately need longer should run their plugin async (the plugin
// can spawn its own background work and return immediately).
//
// Five seconds is the compromise between "user-visible latency on
// every mutation" (favors small) and "fork/exec under macOS parallel
// test load" (favors larger). The original Phase 8 implementation
// used 10s; review #2 flagged that as a CLI-latency bomb.
const execTimeout = 5 * time.Second

// validEventName matches the lifecycle-event names the audit log
// emits (lowercase + underscore). Used to refuse env-injection
// attempts via maliciously-named events.
var validEventName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Registry implements domain.PluginRegistry. It walks the supplied
// plugins directory once at construction time and caches the result.
// Re-discovery during a long-lived process (e.g. `corvee sync --watch`)
// is out of scope; restart the binary to pick up new plugins.
type Registry struct {
	plugins []domain.Plugin
}

// NewRegistry walks pluginsDir for plugin.json manifests. Missing
// directory is not an error (returns an empty registry). Malformed
// manifests are logged and skipped.
func NewRegistry(pluginsDir string) *Registry {
	return &Registry{plugins: discover(pluginsDir)}
}

// Discover implements domain.PluginRegistry.
func (r *Registry) Discover() []domain.Plugin {
	if r == nil {
		return nil
	}
	out := make([]domain.Plugin, len(r.plugins))
	copy(out, r.plugins)
	return out
}

func discover(pluginsDir string) []domain.Plugin {
	if pluginsDir == "" {
		return nil
	}
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil
	}
	plugins := make([]domain.Plugin, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		manifestPath := filepath.Join(dir, "plugin.json")
		body, err := os.ReadFile(manifestPath)
		if err != nil {
			slog.Debug("plugin missing manifest",
				slog.String("dir", dir),
				slog.String("error", err.Error()))
			continue
		}
		var m domain.PluginManifest
		if err := json.Unmarshal(body, &m); err != nil {
			slog.Warn("plugin manifest malformed",
				slog.String("dir", dir),
				slog.String("error", err.Error()))
			continue
		}
		if m.Name == "" {
			m.Name = e.Name()
		}
		if m.Command == "" {
			slog.Warn("plugin manifest missing command field",
				slog.String("dir", dir))
			continue
		}
		plugins = append(plugins, domain.Plugin{Manifest: m, Dir: dir})
	}
	return plugins
}

// Dispatcher implements domain.HookDispatcher. Safe to share across
// goroutines: the only state is the per-instance execTimeout, set at
// construction and not mutated.
type Dispatcher struct {
	execTimeout time.Duration
}

// NewDispatcher returns the default Dispatcher with the production
// execTimeout. Use NewDispatcherWithTimeout to override for tests that
// run under heavy parallelism.
func NewDispatcher() *Dispatcher { return &Dispatcher{execTimeout: execTimeout} }

// NewDispatcherWithTimeout returns a Dispatcher whose plugin / shell-hook
// fork+exec is capped at d instead of the production execTimeout. Intended
// for tests under `-race -count=N` where fork+exec scheduling delays on
// macOS can exceed the production cap.
func NewDispatcherWithTimeout(d time.Duration) *Dispatcher {
	return &Dispatcher{execTimeout: d}
}

// Dispatch implements domain.HookDispatcher.
func (d *Dispatcher) Dispatch(ctx context.Context, plugins []domain.Plugin, hooksDir string, ev domain.LifecycleEvent, agentMode bool) error {
	// Fast path: nothing to do.
	if len(plugins) == 0 && hooksDir == "" {
		return nil
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("dispatch: marshal event: %w", err)
	}
	for _, p := range plugins {
		if !pluginListensFor(p.Manifest, ev.Event) {
			continue
		}
		if agentMode && !p.Manifest.AgentSafe {
			slog.Debug("skipping non-agent-safe plugin",
				slog.String("plugin", p.Manifest.Name),
				slog.String("event", ev.Event))
			continue
		}
		if err := runPlugin(ctx, d.execTimeout, p, body); err != nil {
			if errors.Is(err, domain.ErrPluginVeto) {
				return err
			}
			slog.Warn("plugin error",
				slog.String("plugin", p.Manifest.Name),
				slog.String("event", ev.Event),
				slog.String("error", err.Error()))
		}
	}
	if hooksDir != "" {
		runShellHooks(ctx, d.execTimeout, hooksDir, ev.Event, body)
	}
	return nil
}

// runPlugin spawns the plugin binary, feeds it the event JSON on
// stdin, and reads its stdout for an optional veto response. Capped
// at the supplied timeout regardless of the parent context.
func runPlugin(ctx context.Context, timeout time.Duration, p domain.Plugin, eventJSON []byte) error {
	bin := p.Manifest.Command
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(p.Dir, bin)
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// #nosec G204 -- bin is resolved from a discovered plugin
	// manifest under .tasks/plugins/, not external input.
	cmd := exec.CommandContext(cctx, bin)
	cmd.Stdin = strings.NewReader(string(eventJSON))
	cmd.Dir = p.Dir
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("plugin %q: %w", p.Manifest.Name, err)
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		return nil
	}
	var resp domain.PluginResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("plugin %q: parse response: %w", p.Manifest.Name, err)
	}
	if resp.Veto {
		return fmt.Errorf("plugin %q: %s: %w", p.Manifest.Name, resp.Reason, domain.ErrPluginVeto)
	}
	return nil
}

// runShellHooks fires .tasks/hooks/<event>.sh and .tasks/hooks/all.sh
// when present and executable. event must match validEventName so a
// future caller can't smuggle env-injection payloads through the
// CORVEE_HOOK_EVENT variable.
func runShellHooks(ctx context.Context, timeout time.Duration, hooksDir, event string, eventJSON []byte) {
	if !validEventName.MatchString(event) {
		slog.Warn("refusing to run shell hooks for malformed event name",
			slog.String("event", event))
		return
	}
	for _, candidate := range []string{event + ".sh", "all.sh"} {
		path := filepath.Join(hooksDir, candidate)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		runOneHook(ctx, timeout, path, candidate, event, eventJSON)
	}
}

func runOneHook(parentCtx context.Context, timeout time.Duration, path, candidate, event string, eventJSON []byte) {
	cctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()
	// #nosec G204 -- path is a discovered .tasks/hooks/<event>.sh
	// file vetted via os.Stat above; not external input.
	cmd := exec.CommandContext(cctx, path)
	cmd.Stdin = strings.NewReader(string(eventJSON))
	cmd.Env = append(os.Environ(), "CORVEE_HOOK_EVENT="+event)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Warn("shell hook failed",
			slog.String("hook", candidate),
			slog.String("event", event),
			slog.String("error", err.Error()),
			slog.String("output", string(out)))
	}
}

// pluginListensFor reports whether m subscribes to the given event.
// An empty Events list listens to all events.
func pluginListensFor(m domain.PluginManifest, event string) bool {
	if len(m.Events) == 0 {
		return true
	}
	for _, e := range m.Events {
		if e == event {
			return true
		}
	}
	return false
}

// IsAgentMode reports whether the current invocation should run in
// agent-safe mode. Triggered by CORVEE_AGENT_MODE=1 or the --agent
// global flag (the caller passes the flag's value in).
func IsAgentMode(flag bool) bool {
	if flag {
		return true
	}
	return os.Getenv("CORVEE_AGENT_MODE") == "1"
}
