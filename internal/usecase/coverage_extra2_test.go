package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/adapter/git"
	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// TestMerge_UnsupportedConflictPath ensures Merge returns
// ErrIntegrityViolated when an unexpected file (e.g. a binary) is
// in conflict.
func TestMerge_UnsupportedConflictPath(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	g.ConflictedFilesQueue = [][]string{{"README.md"}}
	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("X"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	_, err := Merge(context.Background(), d, MergeInput{})
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Errorf("err = %v, want ErrIntegrityViolated", err)
	}
}

// TestMerge_EventsShardConflict exercises the events-shard branch.
func TestMerge_EventsShardConflict(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	eventsDir := filepath.Join(tmp, ".tasks", "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shard := filepath.Join(eventsDir, "2026-05-06.jsonl")
	if err := os.WriteFile(shard, []byte(`{"event_id":"e1","ts":"2026-05-06T10:00:00Z","type":"x"}
`), 0o600); err != nil {
		t.Fatal(err)
	}

	g := testfakes.NewGitClient()
	g.RepoRootValue = tmp
	g.ConflictedFilesQueue = [][]string{{".tasks/events/2026-05-06.jsonl"}}

	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("X"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	out, err := Merge(context.Background(), d, MergeInput{Agent: "a"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if len(out.ResolvedPaths) != 1 {
		t.Errorf("resolved = %v", out.ResolvedPaths)
	}
}

// TestSync_AutoCommitDispatchesMerge covers the conflict + auto-commit
// path together.
func TestSync_AutoCommitDispatchesMerge(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	g.Errors["PullRebase"] = fmtConflictErr("conflict")
	g.ConflictedFilesQueue = [][]string{nil}
	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("X"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	out, err := Sync(context.Background(), d, SyncInput{AutoCommit: true, CommitMessage: "x", Agent: "a"})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if out.Merged == nil {
		t.Errorf("expected Merged populated")
	}
}

// fmtConflictErr returns an error that errors.Is recognises as
// ErrGitConflict — the test fake equivalent of what adapter/git
// produces on a real rebase conflict.
func fmtConflictErr(msg string) error {
	return wrappedErr{msg: msg, inner: domain.ErrGitConflict}
}

type wrappedErr struct {
	msg   string
	inner error
}

func (w wrappedErr) Error() string { return w.msg }
func (w wrappedErr) Unwrap() error { return w.inner }

// TestCompact_ExistingArchive covers the case where archive/ exists.
func TestCompact_ExistingArchive(t *testing.T) {
	t.Parallel()
	tasksDir := t.TempDir()
	eventsDir := filepath.Join(tasksDir, "events")
	archiveDir := filepath.Join(eventsDir, "archive")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(eventsDir, "2025-01-01.jsonl")
	if err := os.WriteFile(old, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	out, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if out.ShardsArchived != 1 {
		t.Errorf("archived = %d, want 1", out.ShardsArchived)
	}
}

// TestCompact_NoEventsDir handles a workspace without an events
// directory yet (e.g. fresh init).
func TestCompact_NoEventsDir(t *testing.T) {
	t.Parallel()
	tasksDir := t.TempDir()
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	out, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if out.ShardsArchived != 0 {
		t.Errorf("expected 0 shards on empty workspace; got %d", out.ShardsArchived)
	}
}

// TestExport_GraphFiltersAlias verifies the alias edge filter.
func TestExport_GraphFiltersAlias(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	out, err := Export(context.Background(), d, ExportInput{Format: "graph", Include: []string{"alias"}})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if out.Edges != 1 {
		t.Errorf("alias-only edges = %d, want 1", out.Edges)
	}
}

// TestImport_NativeFillsTimestamps covers the stampItem timestamp
// branches.
func TestImport_NativeFillsTimestamps(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[{"id":"ABC","type":"project","title":"P"}]`)
	if _, err := Import(context.Background(), d, ImportInput{Body: body, Agent: "a"}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	got, _ := d.Store.Get(context.Background(), "ABC")
	if got.CreatedAt.IsZero() {
		t.Errorf("CreatedAt not stamped")
	}
	if got.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt not stamped")
	}
	if got.Version != 1 {
		t.Errorf("Version = %d, want 1", got.Version)
	}
}

// TestMerge_ItemFileFromIntegration_BaseAbsent ensures the merge
// works when the item is added on both sides (no base stage).
func TestMerge_ItemFileFromIntegration_BaseAbsent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git binary not installed")
	}
	t.Parallel()
	root := t.TempDir()
	bare := filepath.Join(root, "bare.git")
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")

	runIn(t, root, "git", "init", "-q", "--bare", "-b", "main", bare)
	runIn(t, root, "git", "clone", "-q", bare, a)
	cfgRepo(t, a, "alice")
	if err := os.WriteFile(filepath.Join(a, "stub.md"), []byte("seed"), 0o644); err != nil {
		t.Fatal(err)
	}
	runIn(t, a, "git", "add", "stub.md")
	runIn(t, a, "git", "commit", "-q", "-m", "seed")
	runIn(t, a, "git", "push", "-u", "-q", "origin", "main")

	runIn(t, root, "git", "clone", "-q", bare, b)
	cfgRepo(t, b, "bob")

	itemsA := filepath.Join(a, ".tasks", "items")
	itemsB := filepath.Join(b, ".tasks", "items")
	for _, p := range []string{itemsA, itemsB} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	itemA := domain.Item{ID: "ABC-E01-S01", Type: domain.TypeStory, Title: "S", Status: domain.StatusReady, Version: 1, ProjectID: "ABC", ParentID: "ABC-E01", Description: "ours"}
	itemB := domain.Item{ID: "ABC-E01-S01", Type: domain.TypeStory, Title: "S", Status: domain.StatusReady, Version: 1, ProjectID: "ABC", ParentID: "ABC-E01", Description: "theirs"}
	writeJSONFile(t, filepath.Join(itemsA, "ABC-E01-S01.json"), itemA)
	writeJSONFile(t, filepath.Join(itemsB, "ABC-E01-S01.json"), itemB)
	runIn(t, a, "git", "add", ".tasks")
	runIn(t, a, "git", "commit", "-q", "-m", "alice add")
	runIn(t, a, "git", "push", "-q")
	runIn(t, b, "git", "add", ".tasks")
	runIn(t, b, "git", "commit", "-q", "-m", "bob add")

	g := git.New(b)
	pullErr := g.PullRebase()
	if pullErr == nil {
		t.Skip("no conflict surfaced — git may have auto-merged")
	}

	prev, _ := os.Getwd()
	defer func() { _ = os.Chdir(prev) }()
	if err := os.Chdir(b); err != nil {
		t.Fatal(err)
	}
	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("X"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	out, err := Merge(context.Background(), d, MergeInput{Agent: "bob"})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if out.Merged != 1 {
		t.Errorf("merged = %d, want 1", out.Merged)
	}
	if len(out.ProseConflicts) != 1 {
		t.Errorf("expected prose conflict on description; got %v", out.ProseConflicts)
	}
	_ = g.RebaseAbort()
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
}
