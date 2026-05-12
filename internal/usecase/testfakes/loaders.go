package testfakes

import (
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// WorkspaceLoader is an in-memory fake of domain.WorkspaceLoader.
// Tests assert against the Saved field directly.
type WorkspaceLoader struct {
	mu      sync.Mutex
	Saved   domain.Workspace
	Present bool
}

// NewWorkspaceLoader returns a fake with no saved workspace.
func NewWorkspaceLoader() *WorkspaceLoader {
	return &WorkspaceLoader{}
}

// Load returns the saved workspace or wraps ErrIntegrityViolated when
// none has been saved — mirroring the production loader's contract.
func (l *WorkspaceLoader) Load() (domain.Workspace, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.Present {
		return domain.Workspace{}, fmt.Errorf("workspace missing: %w", domain.ErrIntegrityViolated)
	}
	return l.Saved, nil
}

// Save replaces the in-memory workspace and flips Present.
func (l *WorkspaceLoader) Save(ws domain.Workspace) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Saved = ws
	l.Present = true
	return nil
}

// Exists reports the in-memory presence flag.
func (l *WorkspaceLoader) Exists() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Present, nil
}

// LocalLoader is an in-memory fake of domain.LocalLoader. Defaults
// returned by Load mirror the production loader: agent_role=human;
// default_agent and capabilities are zero-valued.
type LocalLoader struct {
	mu      sync.Mutex
	Saved   domain.Local
	Present bool
}

// NewLocalLoader returns a fake with no saved local.
func NewLocalLoader() *LocalLoader {
	return &LocalLoader{}
}

// Load returns the saved local config when present, or sensible
// defaults (agent_role=human) when absent.
func (l *LocalLoader) Load() (domain.Local, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.Present {
		return domain.Local{AgentRole: domain.RoleHuman}, nil
	}
	return l.Saved, nil
}

// Save replaces the in-memory local and flips Present.
func (l *LocalLoader) Save(local domain.Local) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Saved = local
	l.Present = true
	return nil
}

// Exists reports the in-memory presence flag.
func (l *LocalLoader) Exists() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Present, nil
}
