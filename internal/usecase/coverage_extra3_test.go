package usecase

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// Targeted coverage tests for sync.go helpers and the new
// closeAncestorsIfAllChildrenDone branches. The goal is line
// coverage on defensive error paths and the early-return gates that
// the focused per-feature suites don't exercise.

func TestAbortAfterFailure_SilentSuccess(t *testing.T) {
	t.Parallel()
	gc := testfakes.NewGitClient()
	d := Deps{GitClient: gc}

	abortAfterFailure(d, "pull", errors.New("primary"))

	if len(gc.Calls) != 1 || gc.Calls[0] != "RebaseAbort" {
		t.Errorf("expected one RebaseAbort call; got %v", gc.Calls)
	}
}

func TestAbortAfterFailure_LogsAbortFailure(t *testing.T) {
	t.Parallel()
	gc := testfakes.NewGitClient()
	gc.Errors["RebaseAbort"] = errors.New("abort blew up")
	d := Deps{GitClient: gc}

	// abortAfterFailure is side-effectful only (slog). The contract we
	// verify here is "always calls RebaseAbort, never panics regardless
	// of its return". The slog.Warn output is observed in stderr but
	// not asserted — the important behavior is that the primary error
	// path keeps running.
	abortAfterFailure(d, "rebase", errors.New("primary"))

	if len(gc.Calls) != 1 || gc.Calls[0] != "RebaseAbort" {
		t.Errorf("expected one RebaseAbort call even on error; got %v", gc.Calls)
	}
}

func TestWorkspaceStagePath_FallsBackWhenNoTasksDir(t *testing.T) {
	t.Parallel()
	got, err := workspaceStagePath(Deps{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != ".tasks" {
		t.Errorf("got %q, want .tasks", got)
	}
}

func TestWorkspaceStagePath_FallsBackWhenNoGitClient(t *testing.T) {
	t.Parallel()
	got, err := workspaceStagePath(Deps{TasksDir: "/anywhere/.tasks"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != ".tasks" {
		t.Errorf("got %q, want .tasks", got)
	}
}

func TestWorkspaceStagePath_FallsBackWhenRepoRootErrors(t *testing.T) {
	t.Parallel()
	gc := testfakes.NewGitClient()
	gc.RepoRootValue = "" // forces default
	gc.Errors = map[string]error{}
	// To make RepoRoot return an error, use a custom value the fake
	// inspects: easiest is to set a known sentinel via Errors. The
	// fake's RepoRoot doesn't currently consult Errors — workaround:
	// pass a GitClient whose RepoRoot result causes filepath.Rel to
	// fail. filepath.Rel fails when one path is absolute and the other
	// is not. Set RepoRootValue to a relative dir and TasksDir absolute.
	gc.RepoRootValue = "relative-root"
	d := Deps{TasksDir: "/abs/.tasks", GitClient: gc}
	got, err := workspaceStagePath(d)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != ".tasks" {
		t.Errorf("got %q, want .tasks fallback", got)
	}
}

func TestWorkspaceStagePath_HappyPath(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	tasks := filepath.Join(tmp, ".tasks")
	gc := testfakes.NewGitClient()
	gc.RepoRootValue = tmp
	d := Deps{TasksDir: tasks, GitClient: gc}
	got, err := workspaceStagePath(d)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != ".tasks" {
		t.Errorf("got %q, want .tasks (rel of tasksDir to repoRoot)", got)
	}
}

func TestCloseAncestorsIfAllChildrenDone_NoOpWhenNotInProgress(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Ancestor is already done. The helper must return without rewriting.
	for _, it := range []domain.Item{
		{SchemaVersion: 1, ID: "P", Type: domain.TypeProject,
			Title: "Project", Status: domain.StatusDone,
			CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P-E01", Type: domain.TypeEpic,
			ProjectID: "P", ParentID: "P",
			Title: "Already done epic", Status: domain.StatusDone,
			CreatedAt: now, UpdatedAt: now, Version: 1},
	} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	d := Deps{Store: store, Clock: testfakes.NewClock(now)}
	if err := closeAncestorsIfAllChildrenDone(ctx, d, "P-E01", "system", domain.RoleHuman, now); err != nil {
		t.Fatalf("closeAncestors: %v", err)
	}
	got, _ := store.Get(ctx, "P-E01")
	if got.Version != 1 {
		t.Errorf("epic version %d, want 1 (untouched)", got.Version)
	}
}

func TestCloseAncestorsIfAllChildrenDone_NoOpWithoutAutoReopenedMarker(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Ancestor is in_progress for unrelated reasons (no auto_reopened
	// journal marker). All children settled — but the helper must
	// leave it alone because the auto-close is a system-driven inverse
	// of auto-reopen; only ancestors flagged by the system get flipped.
	for _, it := range []domain.Item{
		{SchemaVersion: 1, ID: "P", Type: domain.TypeProject,
			Title: "Project", Status: domain.StatusBacklog,
			CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P-E01", Type: domain.TypeEpic,
			ProjectID: "P", ParentID: "P",
			Title: "Manually in_progress epic", Status: domain.StatusInProgress,
			CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P-E01-S01", Type: domain.TypeStory,
			ProjectID: "P", ParentID: "P-E01",
			Title: "Settled", Status: domain.StatusDone,
			CreatedAt: now, UpdatedAt: now, Version: 1},
	} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	d := Deps{Store: store, Clock: testfakes.NewClock(now)}
	if err := closeAncestorsIfAllChildrenDone(ctx, d, "P-E01", "system", domain.RoleHuman, now); err != nil {
		t.Fatalf("closeAncestors: %v", err)
	}
	got, _ := store.Get(ctx, "P-E01")
	if got.Status != domain.StatusInProgress {
		t.Errorf("status=%q, want unchanged in_progress", got.Status)
	}
}

func TestCloseAncestorsIfAllChildrenDone_StopsAtOpenChild(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Ancestor is auto-reopened but still has an open child. Must not flip.
	for _, it := range []domain.Item{
		{SchemaVersion: 1, ID: "P", Type: domain.TypeProject,
			Title: "Project", Status: domain.StatusBacklog,
			CreatedAt: now, UpdatedAt: now, Version: 1},
		{SchemaVersion: 1, ID: "P-E01", Type: domain.TypeEpic,
			ProjectID: "P", ParentID: "P",
			Title: "Reopened epic", Status: domain.StatusInProgress,
			CreatedAt: now, UpdatedAt: now, Version: 2,
			Journal: []domain.JournalEntry{{Timestamp: now, Actor: "system",
				Event: "auto_reopened", Note: "n/a"}}},
		{SchemaVersion: 1, ID: "P-E01-S01", Type: domain.TypeStory,
			ProjectID: "P", ParentID: "P-E01",
			Title: "Still open", Status: domain.StatusBacklog,
			CreatedAt: now, UpdatedAt: now, Version: 1},
	} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	d := Deps{Store: store, Clock: testfakes.NewClock(now)}
	if err := closeAncestorsIfAllChildrenDone(ctx, d, "P-E01", "system", domain.RoleHuman, now); err != nil {
		t.Fatalf("closeAncestors: %v", err)
	}
	got, _ := store.Get(ctx, "P-E01")
	if got.Status != domain.StatusInProgress {
		t.Errorf("status=%q, want unchanged in_progress (open child remains)", got.Status)
	}
}

func TestCloseAncestorsIfAllChildrenDone_WalksMultipleLevels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

	// Project and epic both auto-reopened; story is the only descendant
	// and it's done. The walk must close both ancestors.
	for _, it := range []domain.Item{
		{SchemaVersion: 1, ID: "P", Type: domain.TypeProject,
			Title: "Project", Status: domain.StatusInProgress,
			CreatedAt: now, UpdatedAt: now, Version: 2,
			Journal: []domain.JournalEntry{{Timestamp: now, Actor: "system",
				Event: "auto_reopened", Note: "n/a"}}},
		{SchemaVersion: 1, ID: "P-E01", Type: domain.TypeEpic,
			ProjectID: "P", ParentID: "P",
			Title: "Reopened epic", Status: domain.StatusInProgress,
			CreatedAt: now, UpdatedAt: now, Version: 2,
			Journal: []domain.JournalEntry{{Timestamp: now, Actor: "system",
				Event: "auto_reopened", Note: "n/a"}}},
		{SchemaVersion: 1, ID: "P-E01-S01", Type: domain.TypeStory,
			ProjectID: "P", ParentID: "P-E01",
			Title: "Settled", Status: domain.StatusDone,
			CreatedAt: now, UpdatedAt: now, Version: 1},
	} {
		if _, err := store.Put(ctx, it, -1); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	d := Deps{Store: store, Clock: testfakes.NewClock(now)}
	if err := closeAncestorsIfAllChildrenDone(ctx, d, "P-E01", "system", domain.RoleHuman, now); err != nil {
		t.Fatalf("closeAncestors: %v", err)
	}
	gotEpic, _ := store.Get(ctx, "P-E01")
	gotProj, _ := store.Get(ctx, "P")
	if gotEpic.Status != domain.StatusDone {
		t.Errorf("epic status=%q, want done", gotEpic.Status)
	}
	if gotProj.Status != domain.StatusDone {
		t.Errorf("project status=%q, want done (chain walked)", gotProj.Status)
	}
}

func TestResolveItem_PropagatesNonNotFoundError(t *testing.T) {
	t.Parallel()
	// A store that returns a non-NotFound error from Get should have
	// that error surface unchanged — the alias scan only kicks in on
	// ErrNotFound.
	store := &errStoreForResolve{err: errors.New("boom")}
	_, err := resolveItem(context.Background(), store, "ANY")
	if err == nil || err.Error() != "boom" {
		t.Errorf("err=%v, want unchanged 'boom'", err)
	}
}

// errStoreForResolve is a minimal domain.Store that returns a fixed
// error from Get. Other methods are not exercised by resolveItem.
type errStoreForResolve struct {
	err error
}

func (s *errStoreForResolve) Get(_ context.Context, _ string) (domain.Item, error) {
	return domain.Item{}, s.err
}
func (s *errStoreForResolve) List(_ context.Context, _ domain.ListFilter) ([]domain.Item, error) {
	return nil, nil
}
func (s *errStoreForResolve) Put(_ context.Context, item domain.Item, _ int) (domain.Item, error) {
	return item, nil
}
func (s *errStoreForResolve) Delete(_ context.Context, _ string) error { return nil }
func (s *errStoreForResolve) Exists(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func TestExpectedParentType_AllItemTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   domain.ItemType
		want domain.ItemType
	}{
		{domain.TypeProject, ""},
		{domain.TypeEpic, domain.TypeProject},
		{domain.TypeStory, domain.TypeEpic},
		{domain.TypeSubtask, domain.TypeStory},
		{domain.ItemType("unknown"), ""}, // default branch
	}
	for _, tc := range cases {
		if got := expectedParentType(tc.in); got != tc.want {
			t.Errorf("expectedParentType(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringSliceEqual_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"nil vs empty", nil, []string{}, true},
		{"same elements", []string{"x", "y"}, []string{"x", "y"}, true},
		{"different lengths", []string{"x"}, []string{"x", "y"}, false},
		{"different elements", []string{"x", "y"}, []string{"x", "z"}, false},
		{"different order", []string{"y", "x"}, []string{"x", "y"}, false},
	}
	for _, tc := range cases {
		if got := stringSliceEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestAbsPath_FailsWithoutGitClient(t *testing.T) {
	t.Parallel()
	_, err := absPath(Deps{}, "some/path")
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err=%v, want ErrUsage when GitClient unwired", err)
	}
}

func TestAbsPath_JoinsRelativeToRepoRoot(t *testing.T) {
	t.Parallel()
	gc := testfakes.NewGitClient()
	gc.RepoRootValue = "/repo"
	got, err := absPath(Deps{GitClient: gc}, ".tasks/items/X.json")
	if err != nil {
		t.Fatalf("absPath: %v", err)
	}
	if got != "/repo/.tasks/items/X.json" {
		t.Errorf("got %q, want /repo/.tasks/items/X.json", got)
	}
}

func TestClockNow_FallsBackToWallTimeWhenUnwired(t *testing.T) {
	t.Parallel()
	before := time.Now().UTC()
	got := clockNow(Deps{})
	after := time.Now().UTC()
	if got.Before(before) || got.After(after) {
		t.Errorf("clockNow fallback %v outside [%v, %v]", got, before, after)
	}
}

func TestClockNow_UsesInjectedClock(t *testing.T) {
	t.Parallel()
	fixed := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	got := clockNow(Deps{Clock: testfakes.NewClock(fixed)})
	if !got.Equal(fixed) {
		t.Errorf("clockNow = %v, want injected %v", got, fixed)
	}
}

func TestWorkspaceName_FallsBackOnUnwiredLoader(t *testing.T) {
	t.Parallel()
	got, err := workspaceName(Deps{})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "workspace" {
		t.Errorf("got %q, want fallback %q", got, "workspace")
	}
}

func TestWorkspaceName_FallsBackOnLoaderError(t *testing.T) {
	t.Parallel()
	got, err := workspaceName(Deps{Workspace: &erroringWorkspaceLoader{}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "workspace" {
		t.Errorf("got %q, want fallback %q on loader error", got, "workspace")
	}
}

func TestWorkspaceName_ReturnsConfiguredName(t *testing.T) {
	t.Parallel()
	got, err := workspaceName(Deps{Workspace: &namedWorkspaceLoader{name: "rikonect"}})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "rikonect" {
		t.Errorf("got %q, want configured %q", got, "rikonect")
	}
}

type erroringWorkspaceLoader struct{}

func (l *erroringWorkspaceLoader) Load() (domain.Workspace, error) {
	return domain.Workspace{}, errors.New("loader broken")
}
func (l *erroringWorkspaceLoader) Save(_ domain.Workspace) error { return nil }
func (l *erroringWorkspaceLoader) Exists() (bool, error)         { return false, nil }

type namedWorkspaceLoader struct{ name string }

func (l *namedWorkspaceLoader) Load() (domain.Workspace, error) {
	return domain.Workspace{WorkspaceName: l.name}, nil
}
func (l *namedWorkspaceLoader) Save(_ domain.Workspace) error { return nil }
func (l *namedWorkspaceLoader) Exists() (bool, error)         { return false, nil }

func TestBuildCriticalPath_NilWorkspaceLoaderReturnsNil(t *testing.T) {
	t.Parallel()
	got := buildCriticalPath(Deps{}, nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestBuildCriticalPath_LoaderErrorReturnsNil(t *testing.T) {
	t.Parallel()
	got := buildCriticalPath(Deps{Workspace: &erroringWorkspaceLoader{}}, nil)
	if got != nil {
		t.Errorf("loader error should yield nil; got %v", got)
	}
}

func TestBuildCriticalPath_EmptyCriticalPathReturnsNil(t *testing.T) {
	t.Parallel()
	got := buildCriticalPath(Deps{Workspace: &namedWorkspaceLoader{name: "x"}}, nil)
	if got != nil {
		t.Errorf("empty critical_path should yield nil; got %v", got)
	}
}

func TestBuildCriticalPath_ResolvesStatusFromItems(t *testing.T) {
	t.Parallel()
	loader := &criticalPathLoader{ids: []string{"P-E01-S01", "MISSING", "P-E01-S02"}}
	items := []domain.Item{
		{ID: "P-E01-S01", Status: domain.StatusDone, Title: "first"},
		{ID: "P-E01-S02", Status: domain.StatusInProgress, Title: "second"},
	}
	got := buildCriticalPath(Deps{Workspace: loader}, items)
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
	if !got[0].Found || got[0].Status != domain.StatusDone || got[0].Title != "first" {
		t.Errorf("step 0 = %+v", got[0])
	}
	if got[1].Found {
		t.Errorf("step 1 should be missing; got %+v", got[1])
	}
	if !got[2].Found || got[2].Status != domain.StatusInProgress {
		t.Errorf("step 2 = %+v", got[2])
	}
}

type criticalPathLoader struct{ ids []string }

func (l *criticalPathLoader) Load() (domain.Workspace, error) {
	return domain.Workspace{CriticalPath: l.ids}, nil
}
func (l *criticalPathLoader) Save(_ domain.Workspace) error { return nil }
func (l *criticalPathLoader) Exists() (bool, error)         { return true, nil }
