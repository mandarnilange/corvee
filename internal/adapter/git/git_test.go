package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestSHA_ReturnsHashInARepo(t *testing.T) {
	t.Parallel()
	g := New("")
	sha, err := g.SHA()
	if err != nil {
		// Running outside a git repo is acceptable; assert the
		// fallback path returns ErrNotFound so the manifest layer
		// can swap in the dev version stamp.
		if !errors.Is(err, domain.ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
		return
	}
	if sha == "" {
		t.Errorf("empty SHA from in-repo run")
	}
}

func TestSHA_ReportsNotFoundForNonRepoDir(t *testing.T) {
	t.Parallel()
	g := New(t.TempDir())
	_, err := g.SHA()
	if err == nil {
		t.Fatal("expected error in non-repo dir")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// requireGit skips the test when no git binary is on PATH so the unit
// suite stays runnable in containers without git.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}
}

// initRepo creates a temp git repo with one initial empty commit and
// returns its path. The repo's user.email/name are configured locally
// so commits work in CI environments without global git config.
func initRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	runIn(t, dir, "git", "init", "-q", "-b", "main")
	runIn(t, dir, "git", "config", "user.email", "test@example.com")
	runIn(t, dir, "git", "config", "user.name", "test")
	runIn(t, dir, "git", "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func runIn(t *testing.T, dir, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func TestAddCommit_CreatesACommit(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := g.Add("a.txt"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := g.Commit("add a.txt"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestCommit_NothingToCommitIsNotAnError(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	if err := g.Commit("noop"); err != nil {
		t.Fatalf("Commit on clean tree: %v", err)
	}
}

func TestMv_RenamesAndStages(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	if err := g.Mv("a.txt", "b.txt"); err != nil {
		t.Fatalf("Mv: %v", err)
	}
	if err := g.Commit("rename"); err != nil {
		t.Fatalf("Commit after Mv: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Errorf("expected b.txt to exist, got %v", err)
	}
}

func TestConflictedFiles_EmptyOnCleanTree(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	files, err := g.ConflictedFiles()
	if err != nil {
		t.Fatalf("ConflictedFiles: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected no conflicted files, got %v", files)
	}
}

func TestPullRebase_NoRemote_Errors(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	err := g.PullRebase()
	if err == nil {
		t.Fatal("expected error pulling without remote")
	}
}

// TestPullRebase_ConflictReturnsErrConflict simulates a divergent
// history that creates a rebase conflict on a single file. Two clones
// of a bare repo each commit different content to data.json on top of
// a shared base; one pushes, the other pull --rebases.
func TestPullRebase_ConflictReturnsErrConflict(t *testing.T) {
	t.Parallel()
	requireGit(t)

	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")

	runIn(t, root, "git", "init", "-q", "--bare", "-b", "main", bare)

	// Clone A, seed file, push.
	runIn(t, root, "git", "clone", "-q", bare, a)
	runIn(t, a, "git", "config", "user.email", "a@example.com")
	runIn(t, a, "git", "config", "user.name", "a")
	if err := os.WriteFile(filepath.Join(a, "data.json"), []byte(`{"v":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, a, "git", "add", "data.json")
	runIn(t, a, "git", "commit", "-q", "-m", "seed")
	runIn(t, a, "git", "push", "-q", "origin", "main")

	// Clone B from current state.
	runIn(t, root, "git", "clone", "-q", bare, b)
	runIn(t, b, "git", "config", "user.email", "b@example.com")
	runIn(t, b, "git", "config", "user.name", "b")

	// A modifies and pushes.
	if err := os.WriteFile(filepath.Join(a, "data.json"), []byte(`{"v":2,"by":"a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, a, "git", "add", "data.json")
	runIn(t, a, "git", "commit", "-q", "-m", "a-change")
	runIn(t, a, "git", "push", "-q", "origin", "main")

	// B modifies the same file and tries to pull --rebase.
	if err := os.WriteFile(filepath.Join(b, "data.json"), []byte(`{"v":2,"by":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, b, "git", "add", "data.json")
	runIn(t, b, "git", "commit", "-q", "-m", "b-change")

	g := New(b)
	err := g.PullRebase()
	if err == nil {
		t.Fatal("expected conflict error from rebase")
	}
	if !errors.Is(err, domain.ErrGitConflict) {
		t.Fatalf("err = %v, want ErrGitConflict", err)
	}

	files, listErr := g.ConflictedFiles()
	if listErr != nil {
		t.Fatalf("ConflictedFiles: %v", listErr)
	}
	if len(files) != 1 || files[0] != "data.json" {
		t.Errorf("conflicted files = %v, want [data.json]", files)
	}

	// Clean up: abort the rebase so the temp dir can be removed.
	if err := g.RebaseAbort(); err != nil {
		t.Errorf("RebaseAbort: %v", err)
	}
}

func TestShowAtRev_ReadsHEAD(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.Add("a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := g.Commit("seed"); err != nil {
		t.Fatal(err)
	}
	body, err := g.ShowAtRev("HEAD", "a.txt")
	if err != nil {
		t.Fatalf("ShowAtRev: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
}

func TestShowAtRev_NotFoundForMissingFile(t *testing.T) {
	t.Parallel()
	dir := initRepo(t)
	g := New(dir)
	_, err := g.ShowAtRev("HEAD", "nope.txt")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
