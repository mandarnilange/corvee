package git

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Info implements domain.GitInfo and domain.GitClient by shelling out
// to the git CLI. The adapter shells out rather than linking a git
// library so behavior tracks the user's local git installation.
type Info struct {
	dir        string
	cachedRoot string
}

// New returns an Info that runs git commands rooted at dir. Empty dir
// uses the process's working directory.
func New(dir string) *Info { return &Info{dir: dir} }

// RepoRoot returns the absolute path of the git repository's top
// level. Cached after the first successful call so repeated lookups
// during a single CLI invocation don't fork git twelve times.
// Returns wrapped ErrNotFound when not inside a repo.
func (i *Info) RepoRoot() (string, error) {
	if i.cachedRoot != "" {
		return i.cachedRoot, nil
	}
	stdout, stderr, err := i.run("rev-parse", "--show-toplevel")
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = "not a git repository"
		}
		return "", fmt.Errorf("git rev-parse --show-toplevel: %s: %w", msg, domain.ErrNotFound)
	}
	root := strings.TrimSpace(stdout)
	if root == "" {
		return "", fmt.Errorf("git rev-parse --show-toplevel: empty: %w", domain.ErrNotFound)
	}
	i.cachedRoot = root
	return root, nil
}

// SHA returns the short hash of HEAD. Returns ErrNotFound when git is
// not installed, the directory is not a repo, or HEAD has no commits
// yet — the Render usecase falls back to the dev version stamp on any
// such error per spec §17.2.
func (i *Info) SHA() (string, error) {
	if _, err := exec.LookPath("git"); err != nil {
		return "", fmt.Errorf("git: not installed: %w", domain.ErrNotFound)
	}
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	if i.dir != "" {
		cmd.Dir = i.dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git rev-parse: %w", domain.ErrNotFound)
		}
		return "", fmt.Errorf("git rev-parse: %w", err)
	}
	out := strings.TrimSpace(stdout.String())
	if out == "" {
		return "", fmt.Errorf("git rev-parse: empty: %w", domain.ErrNotFound)
	}
	return out, nil
}

// run executes git with the supplied arguments rooted at i.dir,
// returning combined stdout and the run error.
func (i *Info) run(args ...string) (string, string, error) {
	cmd := exec.Command("git", args...)
	if i.dir != "" {
		cmd.Dir = i.dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// PullRebase implements domain.GitClient. Returns ErrGitConflict on
// conflicts so the caller can dispatch to usecase.Merge.
func (i *Info) PullRebase() error {
	stdout, stderr, err := i.run("pull", "--rebase")
	if err == nil {
		return nil
	}
	combined := stdout + stderr
	if isConflictMessage(combined) {
		return fmt.Errorf("git pull --rebase: %w", domain.ErrGitConflict)
	}
	return fmt.Errorf("git pull --rebase: %s: %w", strings.TrimSpace(stderr), err)
}

// Push implements domain.GitClient.
func (i *Info) Push() error {
	_, stderr, err := i.run("push")
	if err != nil {
		return fmt.Errorf("git push: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

// Add implements domain.GitClient. Empty paths is a no-op so callers
// don't have to guard.
func (i *Info) Add(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	args := append([]string{"add", "--"}, paths...)
	_, stderr, err := i.run(args...)
	if err != nil {
		return fmt.Errorf("git add: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

// Commit implements domain.GitClient. "Nothing to commit" is treated
// as success so callers can issue a Commit unconditionally.
func (i *Info) Commit(message string) error {
	stdout, stderr, err := i.run("commit", "-m", message)
	if err == nil {
		return nil
	}
	combined := stdout + stderr
	if strings.Contains(combined, "nothing to commit") ||
		strings.Contains(combined, "no changes added to commit") ||
		strings.Contains(combined, "nothing added to commit") {
		return nil
	}
	return fmt.Errorf("git commit: %s: %w", strings.TrimSpace(stderr), err)
}

// Mv implements domain.GitClient.
func (i *Info) Mv(from, to string) error {
	_, stderr, err := i.run("mv", from, to)
	if err != nil {
		return fmt.Errorf("git mv %s %s: %s: %w", from, to, strings.TrimSpace(stderr), err)
	}
	return nil
}

// ConflictedFiles implements domain.GitClient. Returns paths in
// conflict per `git diff --name-only --diff-filter=U`. The result is
// repository-relative.
func (i *Info) ConflictedFiles() ([]string, error) {
	stdout, stderr, err := i.run("diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, fmt.Errorf("git diff --diff-filter=U: %s: %w", strings.TrimSpace(stderr), err)
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return out, nil
}

// RebaseContinue implements domain.GitClient.
func (i *Info) RebaseContinue() error {
	cmd := exec.Command("git", "rebase", "--continue")
	if i.dir != "" {
		cmd.Dir = i.dir
	}
	cmd.Env = append(os.Environ(), "GIT_EDITOR=true")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if isConflictMessage(stdout.String() + stderr.String()) {
		return fmt.Errorf("git rebase --continue: %w", domain.ErrGitConflict)
	}
	return fmt.Errorf("git rebase --continue: %s: %w", strings.TrimSpace(stderr.String()), err)
}

// RebaseAbort implements domain.GitClient.
func (i *Info) RebaseAbort() error {
	_, stderr, err := i.run("rebase", "--abort")
	if err != nil {
		return fmt.Errorf("git rebase --abort: %s: %w", strings.TrimSpace(stderr), err)
	}
	return nil
}

// ShowAtRev implements domain.GitClient. Returns ErrNotFound if the
// path does not exist at rev.
func (i *Info) ShowAtRev(rev, path string) ([]byte, error) {
	// #nosec G204 -- rev/path are repository-internal identifiers
	// supplied by the merge usecase, not external user input.
	cmd := exec.Command("git", "show", rev+":"+path)
	if i.dir != "" {
		cmd.Dir = i.dir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist") || strings.Contains(msg, "exists on disk, but not in") || strings.Contains(msg, "unknown revision") {
			return nil, fmt.Errorf("git show %s:%s: %w", rev, path, domain.ErrNotFound)
		}
		return nil, fmt.Errorf("git show %s:%s: %s: %w", rev, path, strings.TrimSpace(msg), err)
	}
	return stdout.Bytes(), nil
}

// isConflictMessage reports whether git's combined output indicates a
// merge/rebase conflict. The patterns track the messages emitted by
// recent git versions (≥2.30).
func isConflictMessage(s string) bool {
	patterns := []string{
		"CONFLICT",
		"could not apply",
		"Resolve all conflicts manually",
		"Merge conflict",
		"after resolving the conflicts",
	}
	for _, p := range patterns {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}
