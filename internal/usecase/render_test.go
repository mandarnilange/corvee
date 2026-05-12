package usecase

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// fakeRenderer is a single-call recording fake for domain.Renderer.
// Tests assert on the captured RenderInput rather than on filesystem
// state — usecase.Render is forbidden from touching disk per S04.
type fakeRenderer struct {
	mu       sync.Mutex
	last     domain.RenderInput
	called   int
	err      error
	stats    domain.RenderStats
	files    []string
	touchErr error
}

func (f *fakeRenderer) Render(_ context.Context, in domain.RenderInput) (domain.RenderOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.last = in
	f.called++
	if f.touchErr != nil {
		return domain.RenderOutput{}, f.touchErr
	}
	if f.err != nil {
		return domain.RenderOutput{}, f.err
	}
	return domain.RenderOutput{Files: f.files, Stats: f.stats}, nil
}

// fakeGit is a stub for domain.GitInfo.
type fakeGit struct {
	sha string
	err error
}

func (g fakeGit) SHA() (string, error) { return g.sha, g.err }

func newRenderDeps(t *testing.T, items ...domain.Item) (Deps, *fakeRenderer) {
	t.Helper()
	store := testfakes.NewStore()
	mustPut(t, store, items...)
	ws := testfakes.NewWorkspaceLoader()
	if err := ws.Save(domain.Workspace{WorkspaceName: "rkn"}); err != nil {
		t.Fatal(err)
	}
	r := &fakeRenderer{
		files: []string{"index.html", "tree.html", "summary.html", "assets/styles.css", "assets/app.js", "manifest.json"},
		stats: domain.RenderStats{},
	}
	return Deps{
		Store:     store,
		EventLog:  testfakes.NewEventLog(),
		Clock:     testfakes.NewClock(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("EVT"),
		Workspace: ws,
		Renderer:  r,
		Git:       fakeGit{sha: "deadbee"},
	}, r
}

func TestRender_DelegatesToRendererWithLoadedItems(t *testing.T) {
	t.Parallel()
	deps, r := newRenderDeps(t,
		domain.Item{ID: "RKN", Type: domain.TypeProject, Title: "rkn", Status: domain.StatusReady},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", Title: "epic", Status: domain.StatusReady},
	)
	out, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		Theme:           "default",
		AvailableThemes: []string{"default", "dark"},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if r.called != 1 {
		t.Errorf("renderer called %d times, want 1", r.called)
	}
	if got := len(r.last.Items); got != 2 {
		t.Errorf("items passed = %d, want 2", got)
	}
	if r.last.WorkspaceName != "rkn" {
		t.Errorf("workspace name = %q", r.last.WorkspaceName)
	}
	if r.last.Theme != "default" {
		t.Errorf("theme = %q", r.last.Theme)
	}
	if out.Manifest.Name != "rkn" {
		t.Errorf("manifest name = %q", out.Manifest.Name)
	}
	if len(out.Files) != 6 {
		t.Errorf("files = %v", out.Files)
	}
}

func TestRender_DefaultsThemeWhenEmpty(t *testing.T) {
	t.Parallel()
	deps, r := newRenderDeps(t)
	if _, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		AvailableThemes: []string{"default", "dark"},
	}); err != nil {
		t.Fatal(err)
	}
	if r.last.Theme != "default" {
		t.Errorf("theme = %q, want default", r.last.Theme)
	}
}

func TestRender_RejectsUnknownTheme(t *testing.T) {
	t.Parallel()
	deps, _ := newRenderDeps(t)
	_, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		Theme:           "neon",
		AvailableThemes: []string{"default", "dark"},
	})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestRender_RequiresOutDir(t *testing.T) {
	t.Parallel()
	deps, _ := newRenderDeps(t)
	_, err := Render(context.Background(), deps, RenderInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestRender_FallsBackWhenGitUnavailable(t *testing.T) {
	t.Parallel()
	deps, _ := newRenderDeps(t)
	deps.Git = fakeGit{err: errors.New("not a repo")}
	out, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		AvailableThemes: []string{"default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Manifest.Version == "" {
		t.Fatal("manifest.version empty")
	}
	if got := out.Manifest.Version; got[:len("0.0.0-dev-")] != "0.0.0-dev-" {
		t.Errorf("version = %q, want 0.0.0-dev- prefix", got)
	}
}

func TestRender_PropagatesRendererError(t *testing.T) {
	t.Parallel()
	deps, r := newRenderDeps(t)
	r.touchErr = errors.New("write boom")
	_, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		AvailableThemes: []string{"default"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRender_StreamsEventsIntoNewestFirstActivity(t *testing.T) {
	t.Parallel()
	deps, r := newRenderDeps(t)
	evlog := deps.EventLog.(*testfakes.EventLog)

	// Append in chronological (oldest → newest) order. usecase.Render
	// must hand the renderer Activity in newest-first order — the bug
	// fixed in this PR was sorting by ItemID, which produced
	// nonsensical "recent activity".
	t0 := time.Date(2026, 5, 1, 9, 0, 0, 0, time.UTC)
	for i, ev := range []domain.Event{
		{Type: "item_added", ItemID: "TASK-A", Actor: "alice"},
		{Type: "claimed", ItemID: "TASK-Z", Actor: "bob"},
		{Type: "completed", ItemID: "TASK-M", Actor: "carol"},
	} {
		ev.EventID = fmt.Sprintf("01H%015d", i)
		ev.Timestamp = t0.Add(time.Duration(i) * time.Hour)
		if err := evlog.Append(ev); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		AvailableThemes: []string{"default"},
	}); err != nil {
		t.Fatal(err)
	}
	gotIDs := make([]string, 0, len(r.last.Activity))
	for _, a := range r.last.Activity {
		gotIDs = append(gotIDs, a.ItemID)
	}
	want := []string{"TASK-M", "TASK-Z", "TASK-A"}
	if len(gotIDs) != len(want) {
		t.Fatalf("activity len = %d, want %d (%v)", len(gotIDs), len(want), gotIDs)
	}
	for i := range want {
		if gotIDs[i] != want[i] {
			t.Errorf("activity[%d] = %q, want %q", i, gotIDs[i], want[i])
		}
	}
	if r.last.EventStats.CompletedCount != 1 {
		t.Errorf("completed count = %d, want 1", r.last.EventStats.CompletedCount)
	}
	if r.last.EventStats.DistinctActors != 3 {
		t.Errorf("distinct actors = %d, want 3", r.last.EventStats.DistinctActors)
	}
}

func TestRender_ActivityCappedAtWindow(t *testing.T) {
	t.Parallel()
	deps, r := newRenderDeps(t)
	evlog := deps.EventLog.(*testfakes.EventLog)

	t0 := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	const total = activityWindow + 5
	for i := range total {
		if err := evlog.Append(domain.Event{
			EventID:   fmt.Sprintf("01H%015d", i),
			Timestamp: t0.Add(time.Duration(i) * time.Minute),
			Type:      "noise",
			ItemID:    fmt.Sprintf("ITEM-%03d", i),
			Actor:     "agent",
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Render(context.Background(), deps, RenderInput{
		OutDir:          t.TempDir(),
		AvailableThemes: []string{"default"},
	}); err != nil {
		t.Fatal(err)
	}
	if got := len(r.last.Activity); got != activityWindow {
		t.Errorf("activity len = %d, want %d", got, activityWindow)
	}
	// First activity row is the newest event (ITEM-024 with total=25).
	wantNewest := fmt.Sprintf("ITEM-%03d", total-1)
	if r.last.Activity[0].ItemID != wantNewest {
		t.Errorf("activity[0].ItemID = %q, want %q", r.last.Activity[0].ItemID, wantNewest)
	}
}

func TestRender_RequiresRenderer(t *testing.T) {
	t.Parallel()
	deps, _ := newRenderDeps(t)
	deps.Renderer = nil
	_, err := Render(context.Background(), deps, RenderInput{OutDir: t.TempDir()})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}
