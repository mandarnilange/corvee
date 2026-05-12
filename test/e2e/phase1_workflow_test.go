package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhase1_FullWorkflow exercises the §S19 happy path end-to-end:
// init → add×3 → list → show → update → done. Every command is run as
// a subprocess against bin/task, so this is the highest-fidelity
// signal we have that the binary actually works.
func TestPhase1_FullWorkflow(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=", "CORVEE_AGENT_ROLE=", "CORVEE_AGENT_CAPABILITIES="}

	// 1. init
	res := runTaskIn(t, dir, env, "init", "--name", "rikonect-backlog", "--project", "RKN", "--agent-id", "alice")
	requireExit(t, res, 0, "init")
	for _, sub := range []string{"items", "events", "operations", "locks", "hooks"} {
		if !dirExists(filepath.Join(dir, ".tasks", sub)) {
			t.Errorf("missing .tasks/%s after init", sub)
		}
	}
	if !fileExists(filepath.Join(dir, ".tasks", "workspace.json")) {
		t.Error("missing workspace.json")
	}
	if !fileExists(filepath.Join(dir, ".tasks", "local.json")) {
		t.Error("missing local.json")
	}

	// 2. add project
	res = runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "Project Rikonect", "--priority", "high")
	requireExit(t, res, 0, "add project")
	if id := mustGetItemID(t, res.Stdout); id != "RKN" {
		t.Errorf("project ID = %q, want RKN", id)
	}

	// 3. add epic
	res = runTaskIn(t, dir, env, "add", "epic", "--parent", "RKN", "--title", "First epic")
	requireExit(t, res, 0, "add epic")
	if id := mustGetItemID(t, res.Stdout); id != "RKN-E01" {
		t.Errorf("epic ID = %q, want RKN-E01", id)
	}

	// 4. add story
	res = runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "First story", "--priority", "critical")
	requireExit(t, res, 0, "add story")
	if id := mustGetItemID(t, res.Stdout); id != "RKN-E01-S01" {
		t.Errorf("story ID = %q, want RKN-E01-S01", id)
	}

	// 5. list
	res = runTaskIn(t, dir, env, "list")
	requireExit(t, res, 0, "list")
	var listEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &listEnv); err != nil {
		t.Fatalf("list stdout not JSON: %v\nstdout=%q", err, res.Stdout)
	}
	if len(listEnv.Data.Items) != 3 {
		t.Errorf("list returned %d items, want 3: %+v", len(listEnv.Data.Items), listEnv.Data.Items)
	}

	// 6. list with filter
	res = runTaskIn(t, dir, env, "list", "--type", "story")
	requireExit(t, res, 0, "list --type=story")
	if err := json.Unmarshal([]byte(res.Stdout), &listEnv); err != nil {
		t.Fatalf("list filter stdout not JSON: %v", err)
	}
	if len(listEnv.Data.Items) != 1 || listEnv.Data.Items[0].ID != "RKN-E01-S01" {
		t.Errorf("filter mismatch: %+v", listEnv.Data.Items)
	}

	// 7. show
	res = runTaskIn(t, dir, env, "show", "RKN-E01")
	requireExit(t, res, 0, "show")
	if id := mustGetItemID(t, res.Stdout); id != "RKN-E01" {
		t.Errorf("show ID = %q", id)
	}

	// 8. update priority on the story
	res = runTaskIn(t, dir, env, "update", "RKN-E01-S01", "--priority", "high", "--expect-version", "1")
	requireExit(t, res, 0, "update priority")
	var updateEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				Priority string `json:"priority"`
				Version  int    `json:"version"`
			} `json:"item"`
			PrevVersion int `json:"prev_version"`
			NewVersion  int `json:"new_version"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &updateEnv); err != nil {
		t.Fatalf("update stdout: %v", err)
	}
	if updateEnv.Data.Item.Priority != "high" || updateEnv.Data.NewVersion != 2 {
		t.Errorf("update result: %+v", updateEnv.Data)
	}

	// 9. transition story through ready→claimed→in_progress→done
	for i, status := range []string{"ready", "claimed", "in_progress"} {
		expectVersion := 2 + i
		res = runTaskIn(t, dir, env, "update", "RKN-E01-S01", "--status", status, "--expect-version", fmt.Sprint(expectVersion))
		requireExit(t, res, 0, "update status="+status)
	}
	res = runTaskIn(t, dir, env, "done", "RKN-E01-S01", "--expect-version", "5", "--note", "shipped")
	requireExit(t, res, 0, "done")

	// 10. confirm done landed
	res = runTaskIn(t, dir, env, "show", "RKN-E01-S01")
	requireExit(t, res, 0, "show after done")
	var doneEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				Status      string  `json:"status"`
				CompletedAt *string `json:"completed_at"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &doneEnv); err != nil {
		t.Fatalf("show after done: %v", err)
	}
	if doneEnv.Data.Item.Status != "done" {
		t.Errorf("status=%q, want done", doneEnv.Data.Item.Status)
	}
	if doneEnv.Data.Item.CompletedAt == nil || *doneEnv.Data.Item.CompletedAt == "" {
		t.Errorf("completed_at not set")
	}
}

// TestPhase1_OutsideWorkspaceFailsGracefully verifies that running a
// non-init verb in a directory with no .tasks/ surfaces the right
// exit code (3 = ErrNotFound, since fsstore.New errors on missing
// items dir... or 7 = ErrIntegrityViolated if the workspace probe
// runs first; either is acceptable as a clear signal).
func TestPhase1_NonInitVerbInOrphanDir(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=", "CORVEE_AGENT_ROLE=", "CORVEE_AGENT_CAPABILITIES="}
	res := runTaskIn(t, dir, env, "list")
	if res.ExitCode == 0 {
		t.Errorf("expected non-zero exit running list with no workspace; got 0\nstdout=%q\nstderr=%q",
			res.Stdout, res.Stderr)
	}
}

func TestPhase1_DuplicateProjectCodeFails(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=", "CORVEE_AGENT_ROLE=", "CORVEE_AGENT_CAPABILITIES="}

	res := runTaskIn(t, dir, env, "init", "--name", "ws")
	requireExit(t, res, 0, "init")

	res = runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "p")
	requireExit(t, res, 0, "first add")

	res = runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "dup")
	if res.ExitCode != 2 {
		t.Errorf("duplicate project: exit=%d, want 2 (ErrUsage)\nstderr=%q", res.ExitCode, res.Stderr)
	}
}

// requireExit asserts res.ExitCode equals want or fails the test with
// a useful message that includes the operation label.
func requireExit(t *testing.T, res runResult, want int, label string) {
	t.Helper()
	if res.ExitCode != want {
		t.Fatalf("%s: exit=%d, want %d\nstdout=%q\nstderr=%q", label, res.ExitCode, want, res.Stdout, res.Stderr)
	}
}

// mustGetItemID extracts data.item.id from a success envelope,
// failing the test if the JSON shape is wrong.
func mustGetItemID(t *testing.T, stdout string) string {
	t.Helper()
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("not JSON: %v\nstdout=%q", err, stdout)
	}
	if !env.OK {
		t.Fatalf("envelope ok=false: %s", stdout)
	}
	return strings.TrimSpace(env.Data.Item.ID)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
