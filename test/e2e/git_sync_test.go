package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhase7_PlannerExecutor_TwoRepoBare exercises the canonical
// multi-VM topology end-to-end: a bare repo, a planner clone that
// imports a 20-task plan, and an executor clone that picks up each
// task, marks it done, syncs back. The planner then pulls and
// verifies all 20 are done.
//
// This is the v1.0-shippable gate per spec §13 / TTR-E07-S09.
func TestPhase7_PlannerExecutor_TwoRepoBare(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	planner := filepath.Join(root, "planner")
	executor := filepath.Join(root, "executor")
	envBase := []string{
		"CORVEE_AGENT_ROLE=",
		"CORVEE_AGENT_CAPABILITIES=",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	}

	mustGit(t, root, "init", "-q", "--bare", "-b", "main", bare)
	mustGit(t, root, "clone", "-q", bare, planner)
	configRepo(t, planner, "alice")

	// Planner: init and import 20 tasks.
	res := runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=alice"), "init",
		"--name", "demo", "--project", "DEMO", "--agent-id", "alice")
	requireExit(t, res, 0, "planner init")

	plan := buildPlan20()
	planFile := filepath.Join(planner, "plan.json")
	if err := os.WriteFile(planFile, plan, 0o644); err != nil {
		t.Fatal(err)
	}
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=alice"), "import", planFile)
	requireExit(t, res, 0, "import")

	// Push initial state via task sync --auto-commit.
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=alice"), "sync",
		"--auto-commit", "--message", "seed")
	if res.ExitCode != 0 {
		// First push to a bare repo needs --set-upstream; do it manually.
		mustGit(t, planner, "push", "-u", "origin", "main")
	}

	// Executor: clone, then pick up each task and complete it.
	mustGit(t, root, "clone", "-q", bare, executor)
	configRepo(t, executor, "bob")

	completed := 0
	for i := 0; i < 20; i++ {
		res = runTaskIn(t, executor, append(envBase, "CORVEE_AGENT_ID=bob"),
			"next", "--type", "story", "--auto-claim")
		if res.ExitCode != 0 {
			t.Fatalf("next iter %d: exit=%d stderr=%q stdout=%q", i, res.ExitCode, res.Stderr, res.Stdout)
		}
		var nextEnv struct {
			OK   bool            `json:"ok"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(res.Stdout), &nextEnv); err != nil {
			t.Fatalf("next %d not JSON: %v\nstdout=%q", i, err, res.Stdout)
		}
		var nextPayload struct {
			Item struct {
				ID      string `json:"id"`
				Version int    `json:"version"`
				Claim   *struct {
					LeaseID string `json:"lease_id"`
				} `json:"claim"`
			} `json:"item"`
			LeaseID string `json:"lease_id"`
		}
		if err := json.Unmarshal(nextEnv.Data, &nextPayload); err != nil {
			t.Fatalf("next payload %d: %v\nraw=%s", i, err, string(nextEnv.Data))
		}
		id := nextPayload.Item.ID
		lease := nextPayload.LeaseID
		if lease == "" && nextPayload.Item.Claim != nil {
			lease = nextPayload.Item.Claim.LeaseID
		}
		if id == "" || lease == "" {
			t.Fatalf("next iter %d: empty id=%q lease=%q raw=%s", i, id, lease, res.Stdout)
		}

		// Move claimed → in_progress so done is a legal transition.
		res = runTaskIn(t, executor, append(envBase, "CORVEE_AGENT_ID=bob"),
			"update", id, "--status", "in_progress")
		requireExit(t, res, 0, "update in_progress "+id)

		res = runTaskIn(t, executor, append(envBase, "CORVEE_AGENT_ID=bob"),
			"done", id, "--lease-id", lease, "--note", "ok")
		requireExit(t, res, 0, "done "+id)
		completed++
	}

	// Executor pushes back. First push needs upstream tracking.
	mustGit(t, executor, "add", ".tasks")
	mustGit(t, executor, "commit", "-q", "--allow-empty", "-m", "executor work")
	mustGit(t, executor, "push", "-q", "origin", "main")

	// Planner pulls.
	mustGit(t, planner, "pull", "--rebase", "-q", "origin", "main")
	// Reindex on planner so the cache picks up the merged events.
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=alice"), "reindex")
	requireExit(t, res, 0, "reindex")

	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=alice"), "list", "--status", "done")
	requireExit(t, res, 0, "list done")
	var listEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Items []struct {
				ID string `json:"id"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &listEnv); err != nil {
		t.Fatalf("list done: %v", err)
	}
	if got := len(listEnv.Data.Items); got != 20 {
		t.Errorf("planner sees %d done items, want 20", got)
	}
	_ = completed
}

// configRepo sets the local user identity on a clone so commits work
// in CI environments without global git config.
func configRepo(t *testing.T, dir, name string) {
	t.Helper()
	mustGit(t, dir, "config", "user.email", name+"@example.com")
	mustGit(t, dir, "config", "user.name", name)
}

// mustGit runs git in dir with the given args, fatal on error.
func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, stderr.String())
	}
}

// buildPlan20 constructs a phases-and-tasks document with one project,
// one epic, and 20 ready stories.
func buildPlan20() []byte {
	type task struct {
		ID                 string   `json:"id"`
		Title              string   `json:"title"`
		Type               string   `json:"type"`
		Status             string   `json:"status"`
		AcceptanceCriteria []string `json:"acceptanceCriteria"`
	}
	type phase struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Tasks []task `json:"tasks"`
	}
	type doc struct {
		SchemaVersion int     `json:"schema_version"`
		ProjectID     string  `json:"project_id"`
		ProjectName   string  `json:"project_name"`
		Phases        []phase `json:"phases"`
	}
	tasks := make([]task, 20)
	for i := 0; i < 20; i++ {
		tasks[i] = task{
			ID:                 fmt.Sprintf("DEMO-E01-S%02d", i+1),
			Title:              fmt.Sprintf("Story %d", i+1),
			Type:               "story",
			Status:             "ready",
			AcceptanceCriteria: []string{"works"},
		}
	}
	d := doc{
		SchemaVersion: 1,
		ProjectID:     "DEMO",
		ProjectName:   "Demo Project",
		Phases:        []phase{{ID: "DEMO-E01", Name: "Phase 1", Tasks: tasks}},
	}
	body, _ := json.Marshal(d)
	return body
}

// _ = strings keeps the import meaningful for future helpers.
var _ = strings.TrimSpace
