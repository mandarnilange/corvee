package testfakes

import (
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// GitClient is an in-memory fake of domain.GitClient. It records
// every call as a string in Calls and supports scripted error returns
// per method via the Errors map (key is the method name, e.g.
// "PullRebase"). ConflictedFilesQueue lets tests stage successive
// conflict-list responses.
type GitClient struct {
	mu                   sync.Mutex
	Calls                []string
	Errors               map[string]error
	ConflictedFilesQueue [][]string
	ShowResponses        map[string][]byte
	// RepoRootValue is what RepoRoot returns. Tests that don't care
	// can leave it empty — the fake then returns "/repo" so callers
	// have a stable path to reason about.
	RepoRootValue string
}

// NewGitClient returns a fresh fake.
func NewGitClient() *GitClient {
	return &GitClient{
		Errors:        map[string]error{},
		ShowResponses: map[string][]byte{},
	}
}

func (g *GitClient) record(call string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.Calls = append(g.Calls, call)
}

func (g *GitClient) errFor(method string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.Errors[method]
}

// PullRebase implements domain.GitClient.
func (g *GitClient) PullRebase() error {
	g.record("PullRebase")
	return g.errFor("PullRebase")
}

// Push implements domain.GitClient.
func (g *GitClient) Push() error {
	g.record("Push")
	return g.errFor("Push")
}

// Add implements domain.GitClient.
func (g *GitClient) Add(paths ...string) error {
	g.record(fmt.Sprintf("Add(%v)", paths))
	return g.errFor("Add")
}

// Commit implements domain.GitClient.
func (g *GitClient) Commit(message string) error {
	g.record(fmt.Sprintf("Commit(%q)", message))
	return g.errFor("Commit")
}

// Mv implements domain.GitClient.
func (g *GitClient) Mv(from, to string) error {
	g.record(fmt.Sprintf("Mv(%s,%s)", from, to))
	return g.errFor("Mv")
}

// ConflictedFiles implements domain.GitClient. Pops from the queue
// per-call so tests can stage successive responses.
func (g *GitClient) ConflictedFiles() ([]string, error) {
	g.record("ConflictedFiles")
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.ConflictedFilesQueue) == 0 {
		return nil, g.Errors["ConflictedFiles"]
	}
	front := g.ConflictedFilesQueue[0]
	g.ConflictedFilesQueue = g.ConflictedFilesQueue[1:]
	return front, g.Errors["ConflictedFiles"]
}

// RebaseContinue implements domain.GitClient.
func (g *GitClient) RebaseContinue() error {
	g.record("RebaseContinue")
	return g.errFor("RebaseContinue")
}

// RebaseAbort implements domain.GitClient.
func (g *GitClient) RebaseAbort() error {
	g.record("RebaseAbort")
	return g.errFor("RebaseAbort")
}

// RepoRoot implements domain.GitClient.
func (g *GitClient) RepoRoot() (string, error) {
	g.record("RepoRoot")
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.RepoRootValue != "" {
		return g.RepoRootValue, nil
	}
	return "/repo", nil
}

// ShowAtRev implements domain.GitClient.
func (g *GitClient) ShowAtRev(rev, path string) ([]byte, error) {
	g.record(fmt.Sprintf("ShowAtRev(%s,%s)", rev, path))
	g.mu.Lock()
	defer g.mu.Unlock()
	body, ok := g.ShowResponses[rev+":"+path]
	if !ok {
		return nil, fmt.Errorf("show %s:%s: %w", rev, path, domain.ErrNotFound)
	}
	return body, nil
}
