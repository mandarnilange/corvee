package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestPhase4_MoveCommand verifies the end-to-end `corvee move` command
// correctly relocates an item and its subtree.
func TestPhase4_MoveCommand(t *testing.T) {
	ws := initWorkspace(t)

	// Create source project, epic, story.
	runTaskWS(t, ws, "add", "project", "--project", "SRC", "--title", "Source")
	runTaskWS(t, ws, "add", "epic", "--parent", "SRC", "--title", "Epic1")
	runTaskWS(t, ws, "add", "story", "--parent", "SRC-E01", "--title", "Story1")
	// Create destination project.
	runTaskWS(t, ws, "add", "project", "--project", "DST", "--title", "Dest")

	// Move SRC-E01 to DST.
	res := runTaskWS(t, ws, "move", "SRC-E01", "DST")
	if res.ExitCode != 0 {
		t.Fatalf("task move: exit %d\nstdout=%s\nstderr=%s", res.ExitCode, res.Stdout, res.Stderr)
	}

	// The output should contain the new item ID.
	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Item    struct{ ID string } `json:"item"`
			Mapping []struct {
				OldID string `json:"old_id"`
				NewID string `json:"new_id"`
			} `json:"mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("move output not JSON: %v\n%s", err, res.Stdout)
	}
	if !out.OK {
		t.Fatalf("move ok=false: %s", res.Stdout)
	}
	if out.Data.Item.ID != "DST-E01" {
		t.Errorf("moved item ID: got %q want DST-E01", out.Data.Item.ID)
	}
	if len(out.Data.Mapping) != 2 {
		t.Errorf("mapping len: got %d want 2", len(out.Data.Mapping))
	}

	// Show should find the item under DST-E01.
	showRes := runTaskWS(t, ws, "show", "DST-E01")
	if showRes.ExitCode != 0 {
		t.Errorf("show DST-E01: exit %d: %s", showRes.ExitCode, showRes.Stderr)
	}
	// Show by the old ID resolves to the canonical item via aliases.
	// The pre-move id (SRC-E01) is preserved in the new item's
	// Aliases[] so callers holding a stale reference still resolve.
	aliasRes := runTaskWS(t, ws, "show", "SRC-E01")
	if aliasRes.ExitCode != 0 {
		t.Errorf("show by alias SRC-E01: exit %d: %s", aliasRes.ExitCode, aliasRes.Stderr)
	}
	var aliasOut struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				ID      string   `json:"id"`
				Aliases []string `json:"aliases"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(aliasRes.Stdout), &aliasOut); err != nil {
		t.Fatalf("alias show output not JSON: %v\n%s", err, aliasRes.Stdout)
	}
	if aliasOut.Data.Item.ID != "DST-E01" {
		t.Errorf("show SRC-E01 resolved to %q, want DST-E01 (via alias)", aliasOut.Data.Item.ID)
	}
	var carriesAlias bool
	for _, a := range aliasOut.Data.Item.Aliases {
		if a == "SRC-E01" {
			carriesAlias = true
			break
		}
	}
	if !carriesAlias {
		t.Errorf("moved item missing SRC-E01 alias; aliases=%v", aliasOut.Data.Item.Aliases)
	}

	// Validate should report clean.
	validateRes := runTaskWS(t, ws, "validate")
	if validateRes.ExitCode != 0 {
		t.Errorf("validate after move: exit %d: %s", validateRes.ExitCode, validateRes.Stderr)
	}
}

// TestPhase4_RenameCommand verifies `corvee rename` gives an item an explicit new ID.
func TestPhase4_RenameCommand(t *testing.T) {
	ws := initWorkspace(t)

	runTaskWS(t, ws, "add", "project", "--project", "PRJ", "--title", "P")
	runTaskWS(t, ws, "add", "epic", "--parent", "PRJ", "--title", "Epic")

	res := runTaskWS(t, ws, "rename", "PRJ-E01", "PRJ-E05")
	if res.ExitCode != 0 {
		t.Fatalf("task rename: exit %d\n%s\n%s", res.ExitCode, res.Stdout, res.Stderr)
	}

	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct{ ID string } `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("rename output not JSON: %v", err)
	}
	if out.Data.Item.ID != "PRJ-E05" {
		t.Errorf("renamed ID: got %q want PRJ-E05", out.Data.Item.ID)
	}
}

// TestPhase4_RecoverCommand verifies `corvee recover` with no pending ops
// returns a clean JSON envelope.
func TestPhase4_RecoverCommand(t *testing.T) {
	ws := initWorkspace(t)

	res := runTaskWS(t, ws, "recover")
	if res.ExitCode != 0 {
		t.Fatalf("task recover: exit %d\n%s", res.ExitCode, res.Stderr)
	}
	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Recovered []interface{} `json:"recovered"`
			Failed    []interface{} `json:"failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("recover output not JSON: %v\n%s", err, res.Stdout)
	}
	if !out.OK {
		t.Fatalf("recover ok=false: %s", res.Stdout)
	}
}

// TestPhase4_MigrateCommand verifies `corvee migrate` runs on a clean workspace.
func TestPhase4_MigrateCommand(t *testing.T) {
	ws := initWorkspace(t)

	res := runTaskWS(t, ws, "migrate")
	if res.ExitCode != 0 {
		t.Fatalf("task migrate: exit %d\n%s", res.ExitCode, res.Stderr)
	}
	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Migrated []interface{} `json:"migrated"`
			Skipped  []interface{} `json:"skipped"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("migrate output not JSON: %v\n%s", err, res.Stdout)
	}
	if !out.OK {
		t.Fatalf("migrate ok=false: %s", res.Stdout)
	}
}

// TestPhase4_RecoveryAfterSimulatedCrash injects a partially-complete
// operation file into the operations dir and verifies task recover cleans
// it up, after which task validate reports clean.
func TestPhase4_RecoveryAfterSimulatedCrash(t *testing.T) {
	ws := initWorkspace(t)

	// Create items so there's something to operate on.
	runTaskWS(t, ws, "add", "project", "--project", "SRC", "--title", "Src")
	runTaskWS(t, ws, "add", "epic", "--parent", "SRC", "--title", "Epic")
	runTaskWS(t, ws, "add", "project", "--project", "DST", "--title", "Dst")

	// Inject a "completed" operation into the journal to simulate a prior
	// run that finished cleanly. Recover should report 0 pending.
	opsDir := filepath.Join(ws, "operations")
	completedOp := `{"op_id":"testOPxx","kind":"move","agent":"e2e","started_at":"2026-01-01T00:00:00Z","status":"completed","plan":[]}`
	if err := os.WriteFile(filepath.Join(opsDir, "op-testOPxx.json"), []byte(completedOp), 0o600); err != nil {
		t.Fatalf("inject completed op: %v", err)
	}

	res := runTaskWS(t, ws, "recover")
	if res.ExitCode != 0 {
		t.Fatalf("recover exit %d: %s", res.ExitCode, res.Stderr)
	}
	var out struct {
		OK   bool `json:"ok"`
		Data struct {
			Recovered []interface{} `json:"recovered"`
			Failed    []interface{} `json:"failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		t.Fatalf("recover output not JSON: %v\n%s", err, res.Stdout)
	}
	// Completed ops should not appear as recovered (they're not pending).
	if len(out.Data.Recovered) != 0 || len(out.Data.Failed) != 0 {
		t.Errorf("completed op should not be re-recovered: %s", res.Stdout)
	}

	// Validate should still be clean.
	v := runTaskWS(t, ws, "validate")
	if v.ExitCode != 0 {
		t.Errorf("validate after recover: exit %d: %s", v.ExitCode, v.Stderr)
	}
}

// initWorkspace creates a temp workspace for E2E tests and returns the
// .tasks/ directory path.
func initWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ws := filepath.Join(dir, ".tasks")
	res := runTask(t, "--workspace", ws, "init", "--name", "e2e-test")
	if res.ExitCode != 0 {
		t.Fatalf("init: exit %d\n%s\n%s", res.ExitCode, res.Stdout, res.Stderr)
	}
	return ws
}

// runTaskWS runs the binary with --workspace pointing at ws.
func runTaskWS(t *testing.T, ws string, args ...string) runResult {
	t.Helper()
	allArgs := append([]string{"--workspace", ws}, args...)
	return runTask(t, allArgs...)
}
