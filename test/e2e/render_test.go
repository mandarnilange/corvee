package e2e

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// timestampPattern matches the activity feed's per-row timestamps and
// the item-detail "claimed_at" / "completed_at" / "created_at" / journal
// timestamps. ulidPattern matches Crockford-base32 ULIDs (e.g. lease
// IDs and event IDs) which are time-derived and therefore distinct on
// every test run. Both get substituted with fixed placeholders before
// snapshot comparison so the otherwise-deterministic HTML diff isn't
// tripped by wall-clock or ulid variation across e2e runs.
var (
	timestampPattern = regexp.MustCompile(`\b20\d{2}-\d{2}-\d{2}( \d{2}:\d{2}(:\d{2})?( UTC)?)?\b`)
	ulidPattern      = regexp.MustCompile(`\b01[0-9A-HJKMNP-TV-Z]{24}\b`)
)

func normalizeForSnapshot(b []byte) []byte {
	b = timestampPattern.ReplaceAll(b, []byte("<TS>"))
	b = ulidPattern.ReplaceAll(b, []byte("<ULID>"))
	return b
}

// updateRenderSnapshots, when set via `go test -update-render`, rewrites
// the committed snapshot fixtures from the test's actual output. CI
// runs without the flag and rejects any drift.
var updateRenderSnapshots = flag.Bool("update-render", false, "regenerate test/fixtures/rendered/*.html snapshots from the current renderer output")

// TestPhase6_RenderProducesStableHTML drives the full pipeline:
// init → add a small backlog → render. The HTML files are then diffed
// against the committed snapshots in test/fixtures/rendered/. Snapshots
// must be byte-identical across runs (no timestamps embedded in HTML).
func TestPhase6_RenderProducesStableHTML(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=alice", "CORVEE_AGENT_ROLE=human", "CORVEE_AGENT_CAPABILITIES="}

	// Build a small but representative workspace: one project, one
	// epic, two stories, one of which is blocked.
	res := runTaskIn(t, dir, env, "init", "--name", "rkn", "--project", "RKN", "--agent-id", "alice")
	requireExit(t, res, 0, "init")
	res = runTaskIn(t, dir, env, "add", "project", "--project", "RKN", "--title", "Project Rikonect", "--priority", "high")
	requireExit(t, res, 0, "add project")
	res = runTaskIn(t, dir, env, "add", "epic", "--parent", "RKN", "--title", "First epic", "--priority", "high")
	requireExit(t, res, 0, "add epic")
	res = runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "Story one", "--priority", "medium")
	requireExit(t, res, 0, "add story 1")
	res = runTaskIn(t, dir, env, "add", "story", "--parent", "RKN-E01", "--title", "Story two", "--priority", "high")
	requireExit(t, res, 0, "add story 2")
	// Walk RKN-E01-S02 through ready → claimed → in_progress → blocked
	// so the summary view exercises the blocked-task callout.
	res = runTaskIn(t, dir, env, "update", "RKN-E01-S02", "--status", "ready")
	requireExit(t, res, 0, "update ready")
	res = runTaskIn(t, dir, env, "claim", "RKN-E01-S02")
	requireExit(t, res, 0, "claim")
	leaseID := mustGetLeaseID(t, res.Stdout)
	res = runTaskIn(t, dir, env, "heartbeat", "RKN-E01-S02", "--lease-id", leaseID, "--advance-to-in-progress")
	requireExit(t, res, 0, "heartbeat to in_progress")
	res = runTaskIn(t, dir, env, "update", "RKN-E01-S02", "--status", "blocked")
	requireExit(t, res, 0, "update blocked")

	// Render into the workspace's default dist/ directory.
	outDir := filepath.Join(dir, "dist")
	res = runTaskIn(t, dir, env, "render", "--out", outDir, "--theme", "default")
	requireExit(t, res, 0, "render")

	files := []string{"index.html", "tree.html", "summary.html"}
	fixDir := fixtureRenderedDir(t)
	for _, name := range files {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("read rendered %s: %v", name, err)
		}
		gotNorm := normalizeForSnapshot(got)
		fixPath := filepath.Join(fixDir, name)
		if *updateRenderSnapshots {
			if err := os.WriteFile(fixPath, gotNorm, 0o644); err != nil {
				t.Fatalf("update snapshot %s: %v", name, err)
			}
			continue
		}
		want, err := os.ReadFile(fixPath)
		if err != nil {
			t.Fatalf("read snapshot %s (run with -update-render to create): %v", name, err)
		}
		if string(gotNorm) != string(want) {
			t.Errorf("%s drifted from snapshot %s\n--- got (head) ---\n%s\n--- want (head) ---\n%s",
				name, fixPath, head(gotNorm, 400), head(want, 400))
		}
	}
}

// TestPhase6_RenderRejectsUnknownTheme verifies the §S07 invariant:
// `--theme` validates against the embedded theme set and returns the
// usage exit code 2 on a miss, *before* any filesystem mutation.
func TestPhase6_RenderRejectsUnknownTheme(t *testing.T) {
	dir := t.TempDir()
	env := []string{"CORVEE_AGENT_ID=alice", "CORVEE_AGENT_ROLE=human"}
	res := runTaskIn(t, dir, env, "init", "--name", "rkn", "--project", "RKN")
	requireExit(t, res, 0, "init")
	res = runTaskIn(t, dir, env, "render", "--theme", "neon")
	if res.ExitCode != 2 {
		t.Fatalf("exit=%d (want 2)\nstdout=%q\nstderr=%q", res.ExitCode, res.Stdout, res.Stderr)
	}
}

// mustGetLeaseID extracts data.lease_id from a claim success envelope.
func mustGetLeaseID(t *testing.T, stdout string) string {
	t.Helper()
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			LeaseID string `json:"lease_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("claim envelope: %v\nstdout=%q", err, stdout)
	}
	if env.Data.LeaseID == "" {
		t.Fatalf("missing lease_id: %s", stdout)
	}
	return env.Data.LeaseID
}

// fixtureRenderedDir resolves test/fixtures/rendered relative to the
// repo root regardless of the test binary's working directory.
func fixtureRenderedDir(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	dir := filepath.Dir(here) // test/e2e
	root := filepath.Dir(dir) // test
	return filepath.Join(root, "fixtures", "rendered")
}

// head returns the first n bytes of b as a string, with an ellipsis
// when truncated. Used to keep snapshot-diff output legible.
func head(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}
