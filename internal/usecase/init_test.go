package usecase_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func newInitDeps() (usecase.Deps, *testfakes.WorkspaceLoader, *testfakes.LocalLoader) {
	ws := testfakes.NewWorkspaceLoader()
	local := testfakes.NewLocalLoader()
	return usecase.Deps{
		Workspace: ws,
		Local:     local,
	}, ws, local
}

func TestInit_FreshBootstrap_CreatesTreeAndConfigs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	tasksDir := filepath.Join(dir, ".tasks")
	d, ws, local := newInitDeps()

	out, err := usecase.Init(context.Background(), d, usecase.InitInput{
		TasksDir:       tasksDir,
		WorkspaceName:  "rikonect-backlog",
		DefaultProject: "RKN",
		AgentID:        "alice",
	})
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	for _, sub := range []string{"items", "events", "operations", "locks", "hooks"} {
		info, statErr := os.Stat(filepath.Join(tasksDir, sub))
		if statErr != nil || !info.IsDir() {
			t.Errorf("missing dir %s: stat=%v", sub, statErr)
		}
	}

	gitignore, err := os.ReadFile(filepath.Join(tasksDir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	for _, expected := range []string{"index.json", "operations/", "locks/", "local.json"} {
		if !strings.Contains(string(gitignore), expected) {
			t.Errorf(".gitignore missing %q; got:\n%s", expected, gitignore)
		}
	}

	if !ws.Present {
		t.Fatal("workspace not saved")
	}
	if ws.Saved.WorkspaceName != "rikonect-backlog" || ws.Saved.DefaultProject != "RKN" {
		t.Errorf("workspace fields wrong: %+v", ws.Saved)
	}
	if ws.Saved.SchemaVersion != domain.CurrentSchemaVersion {
		t.Errorf("schema_version = %d, want %d", ws.Saved.SchemaVersion, domain.CurrentSchemaVersion)
	}
	if ws.Saved.ClaimTTLMinutes != 60 || ws.Saved.HeartbeatIntervalMinutes != 30 {
		t.Errorf("default TTL/heartbeat wrong: %+v", ws.Saved)
	}

	if !local.Present {
		t.Fatal("local not saved on fresh init")
	}
	if local.Saved.DefaultAgent != "alice" {
		t.Errorf("DefaultAgent = %q, want alice", local.Saved.DefaultAgent)
	}
	if local.Saved.AgentRole != domain.RoleHuman {
		t.Errorf("AgentRole = %q, want human", local.Saved.AgentRole)
	}

	if out.WorkspacePath == "" || out.LocalPath == "" {
		t.Errorf("output paths empty: %+v", out)
	}
}

func TestInit_AgentIDFallsBackToUserEnv(t *testing.T) {
	t.Setenv("USER", "envuser")
	dir := t.TempDir()
	d, _, local := newInitDeps()

	if _, err := usecase.Init(context.Background(), d, usecase.InitInput{
		TasksDir:      filepath.Join(dir, ".tasks"),
		WorkspaceName: "ws",
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if local.Saved.DefaultAgent != "envuser" {
		t.Errorf("DefaultAgent = %q, want envuser", local.Saved.DefaultAgent)
	}
}

func TestInit_RefusesExistingWorkspaceWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d, ws, _ := newInitDeps()
	ws.Present = true
	ws.Saved = domain.Workspace{WorkspaceName: "preexisting"}

	_, err := usecase.Init(context.Background(), d, usecase.InitInput{
		TasksDir:      filepath.Join(dir, ".tasks"),
		WorkspaceName: "new-name",
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err=%v, want ErrUsage", err)
	}
	if ws.Saved.WorkspaceName != "preexisting" {
		t.Errorf("workspace was overwritten despite no --force: %+v", ws.Saved)
	}
}

func TestInit_ForcePreservesLocalButRewritesWorkspace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d, ws, local := newInitDeps()
	ws.Present = true
	ws.Saved = domain.Workspace{WorkspaceName: "old"}
	local.Present = true
	local.Saved = domain.Local{
		DefaultAgent:      "preserved-agent",
		AgentRole:         domain.RoleExecutor,
		AgentCapabilities: []string{"go"},
	}

	if _, err := usecase.Init(context.Background(), d, usecase.InitInput{
		TasksDir:      filepath.Join(dir, ".tasks"),
		WorkspaceName: "fresh",
		AgentID:       "would-be-overwritten",
		Force:         true,
	}); err != nil {
		t.Fatalf("Init --force: %v", err)
	}

	if ws.Saved.WorkspaceName != "fresh" {
		t.Errorf("workspace not rewritten on --force: %+v", ws.Saved)
	}
	if local.Saved.DefaultAgent != "preserved-agent" || local.Saved.AgentRole != domain.RoleExecutor {
		t.Errorf("local was clobbered: %+v", local.Saved)
	}
}
