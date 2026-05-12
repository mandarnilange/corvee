// Package config implements domain.WorkspaceLoader and
// domain.LocalLoader for the on-disk JSON config files at
// .tasks/workspace.json (committed) and .tasks/local.json
// (gitignored). Env-var overrides for Local are applied via
// LoadLocalEffective.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/mandarnilange/corvee/internal/domain"
)

// WorkspaceLoader implements domain.WorkspaceLoader against a single
// path on disk.
type WorkspaceLoader struct {
	path string
}

// NewWorkspaceLoader returns a loader rooted at path (typically
// .tasks/workspace.json).
func NewWorkspaceLoader(path string) *WorkspaceLoader {
	return &WorkspaceLoader{path: path}
}

// Load reads the workspace config. Missing file is an integrity error
// — `corvee init` writes workspace.json so its absence means the
// workspace was never bootstrapped (or was removed). Unknown fields
// or malformed JSON wrap ErrSchemaInvalid.
func (l *WorkspaceLoader) Load() (domain.Workspace, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return domain.Workspace{}, fmt.Errorf("config: workspace.json missing at %s: %w",
				l.path, domain.ErrIntegrityViolated)
		}
		return domain.Workspace{}, fmt.Errorf("config: read %s: %w", l.path, err)
	}
	var ws domain.Workspace
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&ws); err != nil {
		return domain.Workspace{}, fmt.Errorf("config: parse %s (%v): %w",
			l.path, err, domain.ErrSchemaInvalid)
	}
	return ws, nil
}

// Exists reports whether workspace.json is present on disk.
func (l *WorkspaceLoader) Exists() (bool, error) {
	return fileExists(l.path)
}

// Save persists ws to disk with 0o600 perms.
func (l *WorkspaceLoader) Save(ws domain.Workspace) error {
	data, err := json.MarshalIndent(ws, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal workspace: %w", err)
	}
	if err := os.WriteFile(l.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	return nil
}

// LocalLoader implements domain.LocalLoader against a single path.
type LocalLoader struct {
	path string
}

// NewLocalLoader returns a loader rooted at path (typically
// .tasks/local.json).
func NewLocalLoader(path string) *LocalLoader {
	return &LocalLoader{path: path}
}

// Load reads the local config or, if the file is absent, returns
// sensible defaults (agent_role=human, default_agent=$USER if set).
// Unknown fields or malformed JSON wrap ErrSchemaInvalid.
func (l *LocalLoader) Load() (domain.Local, error) {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return defaultLocal(), nil
		}
		return domain.Local{}, fmt.Errorf("config: read %s: %w", l.path, err)
	}
	var local domain.Local
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&local); err != nil {
		return domain.Local{}, fmt.Errorf("config: parse %s (%v): %w",
			l.path, err, domain.ErrSchemaInvalid)
	}
	if local.AgentRole == "" {
		// Present file with no role: still default to human so
		// downstream code never sees an empty Role.
		local.AgentRole = domain.RoleHuman
	}
	return local, nil
}

// Exists reports whether local.json is present on disk.
func (l *LocalLoader) Exists() (bool, error) {
	return fileExists(l.path)
}

// Save persists local to disk with 0o600 perms.
func (l *LocalLoader) Save(local domain.Local) error {
	data, err := json.MarshalIndent(local, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal local: %w", err)
	}
	if err := os.WriteFile(l.path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", l.path, err)
	}
	return nil
}

// LoadLocalEffective loads local config via loader, then applies env-
// var overrides per spec §7.1 / §18:
//
//	CORVEE_AGENT_ID           overrides Local.DefaultAgent
//	CORVEE_AGENT_ROLE         overrides Local.AgentRole
//	CORVEE_AGENT_CAPABILITIES (CSV) overrides Local.AgentCapabilities
//
// Precedence is env > local.json > built-in default.
func LoadLocalEffective(loader domain.LocalLoader) (domain.Local, error) {
	local, err := loader.Load()
	if err != nil {
		return domain.Local{}, err
	}
	if v := os.Getenv("CORVEE_AGENT_ID"); v != "" {
		local.DefaultAgent = v
	}
	if v := os.Getenv("CORVEE_AGENT_ROLE"); v != "" {
		local.AgentRole = domain.Role(v)
	}
	if v := os.Getenv("CORVEE_AGENT_CAPABILITIES"); v != "" {
		local.AgentCapabilities = parseCSV(v)
	}
	return local, nil
}

// defaultLocal is the fall-back used when local.json is absent.
func defaultLocal() domain.Local {
	return domain.Local{
		DefaultAgent: os.Getenv("USER"),
		AgentRole:    domain.RoleHuman,
	}
}

// fileExists is a thin wrapper around os.Stat used by both loaders'
// Exists methods.
func fileExists(path string) (bool, error) {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("config: stat %s: %w", path, err)
	}
	return true, nil
}

// parseCSV splits s on ',' and trims whitespace, dropping empty
// elements. Used for CORVEE_AGENT_CAPABILITIES.
func parseCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
