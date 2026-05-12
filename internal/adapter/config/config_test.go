package config

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestWorkspace_LoadSave_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	loader := NewWorkspaceLoader(path)

	autoSync := 60
	in := domain.Workspace{
		SchemaVersion:            1,
		WorkspaceName:            "ws",
		DefaultProject:           "RKN",
		ClaimTTLMinutes:          60,
		HeartbeatIntervalMinutes: 30,
		CriticalPath:             []string{"RKN-E01-S01"},
		Git:                      domain.GitConfig{AutoCommit: true, AutoSyncSeconds: &autoSync},
	}
	if err := loader.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin =%+v\nout=%+v", in, out)
	}
}

func TestWorkspace_LoadMissing_IsIntegrityViolation(t *testing.T) {
	t.Parallel()
	loader := NewWorkspaceLoader(filepath.Join(t.TempDir(), "absent.json"))
	_, err := loader.Load()
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Fatalf("err=%v, want ErrIntegrityViolated", err)
	}
}

func TestWorkspace_LoadUnknownField_IsSchemaInvalid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "workspace.json")
	junk := []byte(`{
  "schema_version": 1,
  "workspace_name": "ws",
  "claim_ttl_minutes": 60,
  "heartbeat_interval_minutes": 30,
  "critical_path": [],
  "git": {"auto_commit": false, "auto_sync_seconds": null},
  "secret_field": 42
}`)
	if err := os.WriteFile(path, junk, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewWorkspaceLoader(path).Load()
	if !errors.Is(err, domain.ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
}

func TestLocal_LoadSave_RoundTrip(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "local.json")
	loader := NewLocalLoader(path)

	in := domain.Local{
		DefaultAgent:      "alice",
		AgentRole:         domain.RoleExecutor,
		AgentCapabilities: []string{"go"},
	}
	if err := loader.Save(in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch:\nin =%+v\nout=%+v", in, out)
	}
}

func TestLocal_LoadMissing_ReturnsDefaults(t *testing.T) {
	t.Setenv("USER", "tester")
	loader := NewLocalLoader(filepath.Join(t.TempDir(), "absent.json"))
	out, err := loader.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.DefaultAgent != "tester" {
		t.Errorf("DefaultAgent = %q, want %q (from $USER)", out.DefaultAgent, "tester")
	}
	if out.AgentRole != domain.RoleHuman {
		t.Errorf("AgentRole = %q, want %q", out.AgentRole, domain.RoleHuman)
	}
	if len(out.AgentCapabilities) != 0 {
		t.Errorf("AgentCapabilities = %v, want empty", out.AgentCapabilities)
	}
}

func TestLocal_LoadMissing_NoUserEnv(t *testing.T) {
	t.Setenv("USER", "")
	out, err := NewLocalLoader(filepath.Join(t.TempDir(), "absent.json")).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if out.DefaultAgent != "" {
		t.Errorf("DefaultAgent = %q, want empty when $USER unset", out.DefaultAgent)
	}
}

func TestLocal_LoadUnknownField_IsSchemaInvalid(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "local.json")
	if err := os.WriteFile(path, []byte(`{"weird":"field"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := NewLocalLoader(path).Load()
	if !errors.Is(err, domain.ErrSchemaInvalid) {
		t.Fatalf("err=%v, want ErrSchemaInvalid", err)
	}
}

func TestLocal_LoadFileWithEmptyRole_DefaultsToHuman(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "local.json")
	if err := os.WriteFile(path, []byte(`{"default_agent":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := NewLocalLoader(path).Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.AgentRole != domain.RoleHuman {
		t.Errorf("AgentRole = %q, want human", out.AgentRole)
	}
}

func TestLoadLocalEffective_EnvOverridesFile(t *testing.T) {
	t.Setenv("CORVEE_AGENT_ID", "env-agent")
	t.Setenv("CORVEE_AGENT_ROLE", "executor")
	t.Setenv("CORVEE_AGENT_CAPABILITIES", "go, python ,frontend")

	path := filepath.Join(t.TempDir(), "local.json")
	loader := NewLocalLoader(path)
	if err := loader.Save(domain.Local{
		DefaultAgent:      "file-agent",
		AgentRole:         domain.RoleHuman,
		AgentCapabilities: []string{"file-only"},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := LoadLocalEffective(loader)
	if err != nil {
		t.Fatalf("LoadLocalEffective: %v", err)
	}
	if out.DefaultAgent != "env-agent" {
		t.Errorf("DefaultAgent = %q, want env-agent", out.DefaultAgent)
	}
	if out.AgentRole != domain.RoleExecutor {
		t.Errorf("AgentRole = %q, want executor", out.AgentRole)
	}
	want := []string{"go", "python", "frontend"}
	if !reflect.DeepEqual(out.AgentCapabilities, want) {
		t.Errorf("AgentCapabilities = %v, want %v", out.AgentCapabilities, want)
	}
}

func TestLoadLocalEffective_FileWinsWhenEnvUnset(t *testing.T) {
	t.Setenv("CORVEE_AGENT_ID", "")
	t.Setenv("CORVEE_AGENT_ROLE", "")
	t.Setenv("CORVEE_AGENT_CAPABILITIES", "")

	path := filepath.Join(t.TempDir(), "local.json")
	loader := NewLocalLoader(path)
	if err := loader.Save(domain.Local{
		DefaultAgent:      "file-agent",
		AgentRole:         domain.RoleReviewer,
		AgentCapabilities: []string{"go"},
	}); err != nil {
		t.Fatal(err)
	}
	out, err := LoadLocalEffective(loader)
	if err != nil {
		t.Fatal(err)
	}
	if out.DefaultAgent != "file-agent" || out.AgentRole != domain.RoleReviewer {
		t.Errorf("file values lost: %+v", out)
	}
}

func TestLoadLocalEffective_DefaultsWhenAllAbsent(t *testing.T) {
	t.Setenv("CORVEE_AGENT_ID", "")
	t.Setenv("CORVEE_AGENT_ROLE", "")
	t.Setenv("CORVEE_AGENT_CAPABILITIES", "")
	t.Setenv("USER", "userdef")

	loader := NewLocalLoader(filepath.Join(t.TempDir(), "absent.json"))
	out, err := LoadLocalEffective(loader)
	if err != nil {
		t.Fatal(err)
	}
	if out.DefaultAgent != "userdef" {
		t.Errorf("DefaultAgent = %q, want userdef", out.DefaultAgent)
	}
	if out.AgentRole != domain.RoleHuman {
		t.Errorf("AgentRole = %q, want human", out.AgentRole)
	}
}
