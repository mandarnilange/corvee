package e2e

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestPhase2_TreeValidateSummaryReindexClone exercises the Phase 2
// verbs end-to-end against the binary.
func TestPhase2_TreeValidateSummaryReindexClone(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=", "CORVEE_AGENT_ROLE=", "CORVEE_AGENT_CAPABILITIES="}

	requireExit(t, runTaskIn(t, dir, env, "init", "--name", "ws", "--project", "RKN", "--agent-id", "alice"), 0, "init")
	requireExit(t, runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "p"), 0, "add project")
	requireExit(t, runTaskIn(t, dir, env, "add", "epic", "--parent", "RKN", "--title", "e"), 0, "add epic")
	requireExit(t, runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "s1"), 0, "add story 1")
	requireExit(t, runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "s2"), 0, "add story 2")

	// tree
	res := runTaskIn(t, dir, env, "tree")
	requireExit(t, res, 0, "tree")
	var treeEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Roots []struct {
				Item struct {
					ID string `json:"id"`
				} `json:"item"`
				Children []struct {
					Item struct {
						ID string `json:"id"`
					} `json:"item"`
					Children []struct {
						Item struct {
							ID string `json:"id"`
						} `json:"item"`
					} `json:"children"`
				} `json:"children"`
			} `json:"roots"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &treeEnv); err != nil {
		t.Fatalf("tree json: %v", err)
	}
	if len(treeEnv.Data.Roots) != 1 || treeEnv.Data.Roots[0].Item.ID != "RKN" {
		t.Errorf("tree roots: %+v", treeEnv.Data.Roots)
	}
	if len(treeEnv.Data.Roots[0].Children[0].Children) != 2 {
		t.Errorf("expected 2 stories, got %+v", treeEnv.Data.Roots[0].Children)
	}

	// validate clean
	res = runTaskIn(t, dir, env, "validate")
	requireExit(t, res, 0, "validate")

	// summary
	res = runTaskIn(t, dir, env, "summary")
	requireExit(t, res, 0, "summary")
	var sumEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Total int `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &sumEnv); err != nil {
		t.Fatalf("summary json: %v", err)
	}
	if sumEnv.Data.Total != 4 {
		t.Errorf("summary total = %d, want 4", sumEnv.Data.Total)
	}

	// reindex
	res = runTaskIn(t, dir, env, "reindex")
	requireExit(t, res, 0, "reindex")
	if !fileExists(filepath.Join(dir, ".tasks", "index.json")) {
		t.Error("reindex did not produce index.json")
	}

	// clone
	res = runTaskIn(t, dir, env, "clone", "RKN-E01-S01", "--parent", "RKN-E01", "--title", "cloned")
	requireExit(t, res, 0, "clone")
	if id := mustGetItemID(t, res.Stdout); id != "RKN-E01-S03" {
		t.Errorf("clone ID = %q, want RKN-E01-S03", id)
	}
}
