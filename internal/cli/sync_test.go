package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func newSyncCLIDeps(t *testing.T) (Deps, *testfakes.GitClient, *testfakes.WorkspaceLoader) {
	t.Helper()
	g := testfakes.NewGitClient()
	ws := testfakes.NewWorkspaceLoader()
	_ = ws.Save(domain.Workspace{SchemaVersion: 1, ClaimTTLMinutes: 60})
	d := Deps{
		Globals: &Globals{},
		Stdout:  &bytes.Buffer{},
		Stderr:  &bytes.Buffer{},
		Usecase: usecase.Deps{
			GitClient: g,
			Workspace: ws,
		},
	}
	return d, g, ws
}

func TestSync_OneShot_PullsAndPushes(t *testing.T) {
	t.Parallel()
	d, g, _ := newSyncCLIDeps(t)
	root := NewRootCmd(d)
	root.SetArgs([]string{"sync"})
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	hasPull, hasPush := false, false
	for _, c := range g.Calls {
		if c == "PullRebase" {
			hasPull = true
		}
		if c == "Push" {
			hasPush = true
		}
	}
	if !hasPull || !hasPush {
		t.Errorf("expected PullRebase + Push, got %v", g.Calls)
	}
}

func TestSync_Watch_ErrUsageWithoutInterval(t *testing.T) {
	t.Parallel()
	d, _, ws := newSyncCLIDeps(t)
	// Save a workspace with auto_sync_seconds=nil.
	current, _ := ws.Load()
	current.Git.AutoSyncSeconds = nil
	_ = ws.Save(current)

	root := NewRootCmd(d)
	root.SetArgs([]string{"sync", "--watch"})
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected ErrUsage")
	}
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestSync_Watch_ExitsOnContextCancel(t *testing.T) {
	t.Parallel()
	d, g, _ := newSyncCLIDeps(t)
	// One tick fires before cancel.
	g.Errors["Push"] = nil

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(150 * time.Millisecond)
		cancel()
	}()

	root := NewRootCmd(d)
	root.SetArgs([]string{"sync", "--watch", "--interval", "1"})
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	if err := root.ExecuteContext(ctx); err != nil {
		t.Errorf("watch should exit cleanly on cancel; got %v", err)
	}
}

// TestSync_AutoCommitFlag verifies the CLI surface accepts the
// --auto-commit / --message flags and forwards them to the usecase.
func TestSync_AutoCommitFlag(t *testing.T) {
	t.Parallel()
	d, g, _ := newSyncCLIDeps(t)
	root := NewRootCmd(d)
	root.SetArgs([]string{"sync", "--auto-commit", "--message", "hello"})
	root.SetOut(d.Stdout)
	root.SetErr(d.Stderr)
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := strings.Join(g.Calls, " ")
	if !strings.Contains(got, `Commit("hello")`) {
		t.Errorf("missing Commit call: %s", got)
	}
}
