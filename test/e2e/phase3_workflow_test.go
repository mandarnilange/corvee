package e2e

import (
	"encoding/json"
	"testing"
)

// TestPhase3_ClaimReleaseHeartbeatNextReap exercises the Phase 3
// concurrency verbs end-to-end against the real binary.
func TestPhase3_ClaimReleaseHeartbeatNextReap(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=", "CORVEE_AGENT_ROLE=", "CORVEE_AGENT_CAPABILITIES="}

	requireExit(t, runTaskIn(t, dir, env, "init", "--name", "ws", "--project", "RKN", "--agent-id", "alice"), 0, "init")
	requireExit(t, runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "p"), 0, "add project")
	requireExit(t, runTaskIn(t, dir, env, "add", "epic", "--parent", "RKN", "--title", "e"), 0, "add epic")
	requireExit(t, runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "s1"), 0, "add story")
	// Move it to ready so it's claimable.
	requireExit(t, runTaskIn(t, dir, env, "update", "RKN-E01-S01", "--status", "ready", "--expect-version", "1"), 0, "update ready")

	// claim
	res := runTaskIn(t, dir, env, "claim", "RKN-E01-S01")
	requireExit(t, res, 0, "claim")
	var claimEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Item struct {
				Status string `json:"status"`
			} `json:"item"`
			LeaseID string `json:"lease_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &claimEnv); err != nil {
		t.Fatalf("claim json: %v", err)
	}
	lease := claimEnv.Data.LeaseID
	if lease == "" || claimEnv.Data.Item.Status != "claimed" {
		t.Fatalf("claim wrong: %+v", claimEnv.Data)
	}

	// double claim should fail
	res = runTaskIn(t, dir, env, "claim", "RKN-E01-S01")
	if res.ExitCode != 5 {
		t.Errorf("double claim exit=%d, want 5\nstderr=%s", res.ExitCode, res.Stderr)
	}

	// heartbeat
	res = runTaskIn(t, dir, env, "heartbeat", "RKN-E01-S01", "--lease-id", lease, "--advance-to-in-progress")
	requireExit(t, res, 0, "heartbeat")

	// release
	res = runTaskIn(t, dir, env, "release", "RKN-E01-S01", "--lease-id", lease, "--reset-to-ready")
	requireExit(t, res, 0, "release")

	// next (without auto-claim)
	res = runTaskIn(t, dir, env, "next")
	requireExit(t, res, 0, "next")
	var nextEnv struct {
		OK   bool `json:"ok"`
		Data struct {
			Found bool `json:"found"`
			Item  struct {
				ID string `json:"id"`
			} `json:"item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &nextEnv); err != nil {
		t.Fatalf("next json: %v", err)
	}
	if !nextEnv.Data.Found || nextEnv.Data.Item.ID != "RKN-E01-S01" {
		t.Errorf("next: %+v", nextEnv.Data)
	}

	// reap (no expired claims yet)
	res = runTaskIn(t, dir, env, "reap")
	requireExit(t, res, 0, "reap")
}
