package usecase

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/adapter/git"
	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// TestMerge_ItemFile_LowerLeaseWins_Integration creates a git repo
// with a divergent claim conflict on items/X.json, then runs
// usecase.Merge end-to-end against the real adapter/git client.
func TestMerge_ItemFile_LowerLeaseWins_Integration(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")

	runIn(t, root, "git", "init", "-q", "--bare", "-b", "main", bare)
	runIn(t, root, "git", "clone", "-q", bare, a)
	cfgRepo(t, a, "alice")

	// Seed an item.
	itemsDir := filepath.Join(a, ".tasks", "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	base := domain.Item{
		ID: "ABC-E01-S01", Type: domain.TypeStory, Title: "S",
		Status: domain.StatusReady, Version: 1, ProjectID: "ABC",
		ParentID: "ABC-E01",
	}
	writeJSON(t, filepath.Join(itemsDir, "ABC-E01-S01.json"), base)
	runIn(t, a, "git", "add", ".tasks")
	runIn(t, a, "git", "commit", "-q", "-m", "seed")
	runIn(t, a, "git", "push", "-u", "-q", "origin", "main")

	runIn(t, root, "git", "clone", "-q", bare, b)
	cfgRepo(t, b, "bob")

	// Each side claims the same item with a different lease.
	leaseLow := "01HX0000000000000000000001"
	leaseHigh := "01HX0000000000000000000002"
	now := time.Now().UTC()

	aClaim := base
	aClaim.Status = domain.StatusClaimed
	aClaim.Version = 2
	aClaim.Claim = &domain.Claim{Agent: "alice", LeaseID: leaseLow, ClaimedAt: now, ExpiresAt: now.Add(time.Hour)}
	writeJSON(t, filepath.Join(a, ".tasks", "items", "ABC-E01-S01.json"), aClaim)
	runIn(t, a, "git", "add", ".tasks")
	runIn(t, a, "git", "commit", "-q", "-m", "alice claim")
	runIn(t, a, "git", "push", "-q")

	bClaim := base
	bClaim.Status = domain.StatusClaimed
	bClaim.Version = 2
	bClaim.Claim = &domain.Claim{Agent: "bob", LeaseID: leaseHigh, ClaimedAt: now, ExpiresAt: now.Add(time.Hour)}
	writeJSON(t, filepath.Join(b, ".tasks", "items", "ABC-E01-S01.json"), bClaim)
	runIn(t, b, "git", "add", ".tasks")
	runIn(t, b, "git", "commit", "-q", "-m", "bob claim")

	// Bob pulls — surfaces the conflict.
	gitClient := git.New(b)
	pullErr := gitClient.PullRebase()
	if pullErr == nil {
		t.Fatalf("expected ErrGitConflict from PullRebase")
	}

	// Run merge from b's working dir (so absPath resolves correctly).
	prev, _ := os.Getwd()
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(b); err != nil {
		t.Fatal(err)
	}

	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("EVT"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: gitClient,
	}
	out, err := Merge(context.Background(), d, MergeInput{Agent: "bob"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if out.Merged != 1 {
		t.Errorf("merged = %d, want 1", out.Merged)
	}
	if len(out.ClaimLost) != 1 || out.ClaimLost[0] != "ABC-E01-S01" {
		t.Errorf("claim_lost = %v, want [ABC-E01-S01]", out.ClaimLost)
	}

	// The merged file should now hold the lower-lease (alice's) claim.
	merged := readItem(t, filepath.Join(b, ".tasks", "items", "ABC-E01-S01.json"))
	if merged.Claim == nil {
		t.Fatalf("merged claim missing: %+v", merged)
	}
	if merged.Claim.LeaseID != leaseLow {
		t.Errorf("survivor lease = %q, want %q", merged.Claim.LeaseID, leaseLow)
	}

	// Clean up rebase state.
	_ = gitClient.RebaseAbort()
}

func cfgRepo(t *testing.T, dir, name string) {
	t.Helper()
	runIn(t, dir, "git", "config", "user.email", name+"@example.com")
	runIn(t, dir, "git", "config", "user.name", name)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readItem(t *testing.T, path string) domain.Item {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var it domain.Item
	if err := json.Unmarshal(body, &it); err != nil {
		t.Fatalf("parse: %v\n%s", err, body)
	}
	return it
}

func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v in %s: %v\n%s", name, args, dir, err, out)
	}
}
