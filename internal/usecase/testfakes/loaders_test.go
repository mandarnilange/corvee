package testfakes

import (
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestWorkspaceLoader_LoadAbsentReturnsErrIntegrityViolated(t *testing.T) {
	t.Parallel()
	l := NewWorkspaceLoader()
	exists, err := l.Exists()
	if err != nil || exists {
		t.Errorf("Exists initial: (%v, %v)", exists, err)
	}
	_, err = l.Load()
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Fatalf("err=%v", err)
	}
}

func TestWorkspaceLoader_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	l := NewWorkspaceLoader()
	in := domain.Workspace{WorkspaceName: "w", ClaimTTLMinutes: 60}
	if err := l.Save(in); err != nil {
		t.Fatal(err)
	}
	exists, _ := l.Exists()
	if !exists {
		t.Errorf("Exists after Save = false")
	}
	out, err := l.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.WorkspaceName != "w" || out.ClaimTTLMinutes != 60 {
		t.Errorf("round-trip: %+v", out)
	}
}

func TestLocalLoader_LoadAbsentReturnsHumanDefaults(t *testing.T) {
	t.Parallel()
	out, err := NewLocalLoader().Load()
	if err != nil {
		t.Fatal(err)
	}
	if out.AgentRole != domain.RoleHuman {
		t.Errorf("AgentRole=%q", out.AgentRole)
	}
}

func TestLocalLoader_SaveLoadRoundTrip(t *testing.T) {
	t.Parallel()
	l := NewLocalLoader()
	in := domain.Local{DefaultAgent: "a", AgentRole: domain.RoleExecutor}
	if err := l.Save(in); err != nil {
		t.Fatal(err)
	}
	exists, _ := l.Exists()
	if !exists {
		t.Errorf("Exists after Save = false")
	}
	out, _ := l.Load()
	if out.DefaultAgent != "a" || out.AgentRole != domain.RoleExecutor {
		t.Errorf("round-trip: %+v", out)
	}
}
