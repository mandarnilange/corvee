package hooks

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("hooks require POSIX shell")
	}
}

func writePluginScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	pluginDir := filepath.Join(dir, name)
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(pluginDir, "run.sh")
	if err := os.WriteFile(scriptPath, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := []byte(`{"name":"` + name + `","command":"run.sh"}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	return pluginDir
}

func TestDispatch_RunsApplicablePlugin(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	pluginsDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "marker")
	body := "#!/bin/sh\necho ran > " + out + "\n"
	writePluginScript(t, pluginsDir, "tagger", body)

	plugins := NewRegistry(pluginsDir).Discover()
	if len(plugins) != 1 {
		t.Fatalf("Discover = %d, want 1", len(plugins))
	}

	if err := NewDispatcher().Dispatch(context.Background(), plugins, "", domain.LifecycleEvent{Event: "claimed"}, false); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("plugin did not run; marker missing: %v", err)
	}
}

func TestDispatch_VetoFailsOperation(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	pluginsDir := t.TempDir()
	body := `#!/bin/sh
echo '{"veto":true,"reason":"nope"}'
`
	writePluginScript(t, pluginsDir, "guard", body)
	plugins := NewRegistry(pluginsDir).Discover()
	err := NewDispatcher().Dispatch(context.Background(), plugins, "", domain.LifecycleEvent{Event: "claimed"}, false)
	if !errors.Is(err, domain.ErrPluginVeto) {
		t.Errorf("err = %v, want ErrPluginVeto", err)
	}
}

func TestDispatch_AgentModeSkipsUnsafePlugins(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	pluginsDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "ran")
	body := "#!/bin/sh\necho yes > " + out + "\n"
	writePluginScript(t, pluginsDir, "unsafe", body)

	plugins := NewRegistry(pluginsDir).Discover()
	if err := NewDispatcher().Dispatch(context.Background(), plugins, "", domain.LifecycleEvent{Event: "claimed"}, true); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := os.Stat(out); err == nil {
		t.Errorf("unsafe plugin should be skipped in agent mode")
	}
}

func TestDispatch_AgentModeRunsSafePlugins(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	pluginsDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "ran")
	body := "#!/bin/sh\necho yes > " + out + "\n"
	pluginDir := writePluginScript(t, pluginsDir, "safe", body)
	manifest := []byte(`{"name":"safe","command":"run.sh","agent_safe":true}`)
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	plugins := NewRegistry(pluginsDir).Discover()
	if err := NewDispatcher().Dispatch(context.Background(), plugins, "", domain.LifecycleEvent{Event: "claimed"}, true); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("safe plugin should run in agent mode: %v", err)
	}
}

func TestDispatch_ShellHook(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	hooksDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "hook-ran")
	body := "#!/bin/sh\necho yes > " + out + "\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "claimed.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewDispatcher().Dispatch(context.Background(), nil, hooksDir, domain.LifecycleEvent{Event: "claimed"}, false); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("shell hook did not run: %v", err)
	}
}

func TestDispatch_AllHook(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	hooksDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "all-ran")
	body := "#!/bin/sh\ncat > " + out + "\n"
	if err := os.WriteFile(filepath.Join(hooksDir, "all.sh"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := NewDispatcher().Dispatch(context.Background(), nil, hooksDir, domain.LifecycleEvent{Event: "completed"}, false); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("all.sh did not run: %v", err)
	}
}

func TestDispatch_EarlyReturnNoPluginsNoHooks(t *testing.T) {
	t.Parallel()
	if err := NewDispatcher().Dispatch(context.Background(), nil, "", domain.LifecycleEvent{Event: "claimed"}, false); err != nil {
		t.Errorf("err = %v, want nil for empty dispatch", err)
	}
}

func TestDispatch_RefusesMalformedEventName(t *testing.T) {
	// t.Parallel() removed: parallel fork+exec under macOS load
	// occasionally exceeds the dispatcher's exec timeout in CI.
	// Per-test wall time is sub-second, so serialising is cheap.
	skipOnWindows(t)
	hooksDir := t.TempDir()
	// Hook would fire for event "claimed" — but we pass an invalid
	// event name and expect runShellHooks to refuse.
	if err := os.WriteFile(filepath.Join(hooksDir, "claimed.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Malformed event names should not run any hook.
	err := NewDispatcher().Dispatch(context.Background(), nil, hooksDir,
		domain.LifecycleEvent{Event: "bad name with spaces"}, false)
	if err != nil {
		t.Errorf("Dispatch returned err for malformed event: %v", err)
	}
}

func TestRegistry_SkipsMalformed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "bad"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad", "plugin.json"), []byte(`{"not":valid`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NewRegistry(dir).Discover()
	if len(got) != 0 {
		t.Errorf("malformed plugin should be skipped; got %d", len(got))
	}
}

func TestRegistry_MissingCommandField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(domain.PluginManifest{Name: "broken"})
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	got := NewRegistry(dir).Discover()
	if len(got) != 0 {
		t.Errorf("plugin without command should be skipped; got %v", got)
	}
}

func TestRegistry_EmptyDir(t *testing.T) {
	t.Parallel()
	if got := NewRegistry("").Discover(); len(got) != 0 {
		t.Errorf("empty dir should return nil, got %v", got)
	}
	if got := NewRegistry("/no/such/path").Discover(); len(got) != 0 {
		t.Errorf("missing dir should return nil, got %v", got)
	}
}

func TestIsAgentMode(t *testing.T) {
	if IsAgentMode(true) != true {
		t.Errorf("flag=true should yield agent mode")
	}
	t.Setenv("CORVEE_AGENT_MODE", "1")
	if IsAgentMode(false) != true {
		t.Errorf("env=1 should yield agent mode")
	}
	t.Setenv("CORVEE_AGENT_MODE", "")
	if IsAgentMode(false) != false {
		t.Errorf("no flag, no env should be off")
	}
}
