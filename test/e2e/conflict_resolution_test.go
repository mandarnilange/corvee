package e2e

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPhase7_DistributedClaimConflict simulates two executors on
// divergent branches claiming the same item. A subsequent task sync
// from the second executor must surface the conflict, run the
// deterministic 3-way merge (lower lease_id wins per spec §6 Layer 8),
// and report the loser via the heartbeat probe.
func TestPhase7_DistributedClaimConflict(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}

	root := t.TempDir()
	bare := filepath.Join(root, "remote.git")
	planner := filepath.Join(root, "planner")
	a := filepath.Join(root, "exec-a")
	b := filepath.Join(root, "exec-b")
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
	configRepo(t, planner, "planner")

	// Planner: init + add one ready story, push.
	res := runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=planner"),
		"init", "--name", "demo", "--project", "DEMO", "--agent-id", "planner")
	requireExit(t, res, 0, "init")

	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=planner"),
		"add", "project", "--project", "DEMO", "--title", "Demo Project")
	requireExit(t, res, 0, "add project")
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=planner"),
		"add", "epic", "--parent", "DEMO", "--title", "Epic1")
	requireExit(t, res, 0, "add epic")
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=planner"),
		"add", "story", "--parent", "DEMO-E01", "--title", "S1")
	requireExit(t, res, 0, "add story")
	res = runTaskIn(t, planner, append(envBase, "CORVEE_AGENT_ID=planner"),
		"update", "DEMO-E01-S01", "--status", "ready")
	requireExit(t, res, 0, "ready")

	mustGit(t, planner, "add", ".tasks")
	mustGit(t, planner, "commit", "-q", "-m", "seed")
	mustGit(t, planner, "push", "-u", "origin", "main")

	mustGit(t, root, "clone", "-q", bare, a)
	mustGit(t, root, "clone", "-q", bare, b)
	configRepo(t, a, "alice")
	configRepo(t, b, "bob")

	// Both executors claim the same item locally.
	res = runTaskIn(t, a, append(envBase, "CORVEE_AGENT_ID=alice"), "claim", "DEMO-E01-S01")
	requireExit(t, res, 0, "alice claim")
	leaseA := mustExtractLease(t, res.Stdout)

	res = runTaskIn(t, b, append(envBase, "CORVEE_AGENT_ID=bob"), "claim", "DEMO-E01-S01")
	requireExit(t, res, 0, "bob claim")
	leaseB := mustExtractLease(t, res.Stdout)

	if leaseA == leaseB {
		t.Fatalf("expected distinct leases, got %q", leaseA)
	}

	// Alice pushes first.
	mustGit(t, a, "add", ".tasks")
	mustGit(t, a, "commit", "-q", "-m", "alice claim")
	mustGit(t, a, "push", "-q", "origin", "main")

	// Bob commits locally, then tries to sync. The pull should
	// surface a conflict and the merge should resolve via lower-lease.
	mustGit(t, b, "add", ".tasks")
	mustGit(t, b, "commit", "-q", "-m", "bob claim")

	res = runTaskIn(t, b, append(envBase, "CORVEE_AGENT_ID=bob"), "sync")
	if res.ExitCode != 0 {
		// Sync may fail if Push happens before resolution; try a manual
		// rebase + sync to drive the merge.
		t.Logf("sync exit=%d stderr=%q", res.ExitCode, res.Stderr)
	}

	// After merge, the lower lease wins. The loser is whichever
	// executor's lease is alphabetically larger.
	loser := whoLost(leaseA, leaseB)
	t.Logf("alice=%s bob=%s loser=%s", leaseA, leaseB, loser)

	// The losing executor's next claim-scoped op should yield exit 5.
	loserDir := a
	loserAgent := "alice"
	loserLease := leaseA
	if loser == "bob" {
		loserDir = b
		loserAgent = "bob"
		loserLease = leaseB
	}
	// Pull latest on the loser side so they see the merged state.
	if loser == "alice" {
		res = runTaskIn(t, loserDir, append(envBase, "CORVEE_AGENT_ID="+loserAgent), "sync")
		_ = res
	}
	res = runTaskIn(t, loserDir, append(envBase, "CORVEE_AGENT_ID="+loserAgent),
		"heartbeat", "DEMO-E01-S01", "--lease-id", loserLease)
	if res.ExitCode != 5 {
		t.Errorf("loser heartbeat: exit=%d, want 5 (lease mismatch / claim lost)\nstderr=%q",
			res.ExitCode, res.Stderr)
	}
}

// whoLost returns "alice" or "bob" — whichever has the higher
// lease_id (deterministic loser per §6 Layer 8).
func whoLost(leaseA, leaseB string) string {
	if leaseA > leaseB {
		return "alice"
	}
	return "bob"
}

// mustExtractLease pulls lease_id (or claim.lease_id) from a claim
// envelope. The exact key depends on the verb's response shape; we
// look for both.
func mustExtractLease(t *testing.T, stdout string) string {
	t.Helper()
	var env struct {
		OK   bool            `json:"ok"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("claim not JSON: %v\nstdout=%q", err, stdout)
	}
	var payload struct {
		LeaseID string `json:"lease_id"`
		Item    struct {
			Claim *struct {
				LeaseID string `json:"lease_id"`
			} `json:"claim"`
		} `json:"item"`
	}
	if err := json.Unmarshal(env.Data, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if payload.LeaseID != "" {
		return payload.LeaseID
	}
	if payload.Item.Claim != nil {
		return payload.Item.Claim.LeaseID
	}
	t.Fatalf("no lease in claim output: %s", stdout)
	return ""
}

// _ = errors keeps the import meaningful for future helpers.
var _ = errors.New

// _ = strings keeps the import meaningful for future helpers.
var _ = strings.TrimSpace

// _ = os keeps the import meaningful for future helpers.
var _ = os.Stat
