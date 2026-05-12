package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// Targeted tests for branches that the focused per-feature suites
// don't hit. Kept in one file so it's obvious the goal is coverage,
// not behavior.

func TestMergeCompletedAt_AllPaths(t *testing.T) {
	t.Parallel()
	a := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	b := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	if got := mergeCompletedAt(nil, nil); got != nil {
		t.Errorf("nil/nil = %v", got)
	}
	if got := mergeCompletedAt(&a, nil); got == nil || !got.Equal(a) {
		t.Errorf("a/nil = %v", got)
	}
	if got := mergeCompletedAt(nil, &b); got == nil || !got.Equal(b) {
		t.Errorf("nil/b = %v", got)
	}
	if got := mergeCompletedAt(&a, &b); got == nil || !got.Equal(a) {
		t.Errorf("a<b: got %v, want a", got)
	}
	if got := mergeCompletedAt(&b, &a); got == nil || !got.Equal(a) {
		t.Errorf("b>a: got %v, want a (earlier)", got)
	}
}

func TestMaxInt(t *testing.T) {
	t.Parallel()
	if maxInt(3, 5) != 5 {
		t.Error("3<5")
	}
	if maxInt(5, 3) != 5 {
		t.Error("5>3")
	}
}

func TestImport_Coalesce(t *testing.T) {
	t.Parallel()
	if got := coalesce("a", "b"); got != "a" {
		t.Errorf("got %s, want a", got)
	}
	if got := coalesce("", "b"); got != "b" {
		t.Errorf("got %s, want b", got)
	}
	if got := coalesceStatus("", domain.StatusReady); got != domain.StatusReady {
		t.Errorf("got %s", got)
	}
	if got := coalesceType("", domain.TypeStory); got != domain.TypeStory {
		t.Errorf("got %s", got)
	}
}

func TestImport_EmptyBodyRejected(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Import(context.Background(), d, ImportInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestImport_UnrecognisedFormat(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Import(context.Background(), d, ImportInput{Body: []byte(`"not-json"`)})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestImport_PhasesMissingProjectID(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Import(context.Background(), d, ImportInput{Body: []byte(`{"phases":[]}`)})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestImport_MalformedNative(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Import(context.Background(), d, ImportInput{Body: []byte(`[{not-json`)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExportPlan_StoreRequired(t *testing.T) {
	t.Parallel()
	_, err := ExportPlan(context.Background(), Deps{}, ExportPlanInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestExport_StoreRequired(t *testing.T) {
	t.Parallel()
	_, err := Export(context.Background(), Deps{}, ExportInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestExport_UnknownFormat(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Export(context.Background(), d, ExportInput{Format: "xml"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}

func TestExportPlanMarkdown_AllSections(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P"},
		{"id":"ABC-E01","type":"epic","title":"E","deliverable":"X"},
		{"id":"ABC-E01-S01","type":"story","title":"S","description":"d","acceptance_criteria":["AC1","AC2"]},
		{"id":"ABC-E01-S01-T01","type":"subtask","title":"T1"}
	]`)
	_, _ = Import(context.Background(), d, ImportInput{Body: body, Agent: "a"})
	out, err := ExportPlan(context.Background(), d, ExportPlanInput{Format: "markdown"})
	if err != nil {
		t.Fatalf("ExportPlan: %v", err)
	}
	s := string(out.Body)
	for _, want := range []string{"# ABC", "## ABC-E01", "### ABC-E01-S01", "AC1", "**ABC-E01-S01-T01**"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in markdown:\n%s", want, s)
		}
	}
}

func TestSync_AutoCommitFailure(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	g.Errors["Add"] = errors.New("disk full")
	d := Deps{GitClient: g}
	_, err := Sync(context.Background(), d, SyncInput{AutoCommit: true})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSync_PushFailureSurfaces(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	g.Errors["Push"] = errors.New("remote rejected")
	d := Deps{
		GitClient: g,
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("X"),
	}
	_, err := Sync(context.Background(), d, SyncInput{})
	if err == nil {
		t.Fatal("expected push error")
	}
}

func TestCompact_DefaultCutoff(t *testing.T) {
	t.Parallel()
	d := Deps{Clock: testfakes.NewClock(time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC))}
	cutoff, err := resolveCutoff(d, "")
	if err != nil {
		t.Fatal(err)
	}
	expected := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC).Add(-defaultCompactWindow)
	if !cutoff.Equal(expected) {
		t.Errorf("default cutoff = %v, want %v", cutoff, expected)
	}
}

func TestCompact_RFC3339Cutoff(t *testing.T) {
	t.Parallel()
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	got, err := resolveCutoff(d, "2026-03-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got.Year() != 2026 || got.Month() != 3 {
		t.Errorf("got %v", got)
	}
}

func TestParseShardDate(t *testing.T) {
	t.Parallel()
	got, ok := parseShardDate("2026-05-06.jsonl")
	if !ok || got.Year() != 2026 {
		t.Errorf("parse: %v %v", got, ok)
	}
	if _, ok := parseShardDate("garbage.jsonl"); ok {
		t.Error("garbage should not parse")
	}
}

func TestExport_GraphMinimal(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	out, err := Export(context.Background(), d, ExportInput{})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if out.Format != "graph" {
		t.Errorf("default format = %s, want graph", out.Format)
	}
}

func TestImport_NativeWithExistingItem(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[{"id":"ABC","type":"project","title":"P"}]`)
	_, _ = Import(context.Background(), d, ImportInput{Body: body})
	out, err := Import(context.Background(), d, ImportInput{Body: body})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if len(out.Skipped) != 1 {
		t.Errorf("skipped = %d, want 1", len(out.Skipped))
	}
}

func TestImport_ValidateImportItem_AllRejections(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		it   domain.Item
	}{
		{"empty id", domain.Item{}},
		{"bad id", domain.Item{ID: "lowercase"}},
		{"type/depth mismatch", domain.Item{ID: "ABC", Type: domain.TypeStory}},
		{"invalid status", domain.Item{ID: "ABC", Type: domain.TypeProject, Title: "x", Status: "made-up"}},
		{"missing title on epic", domain.Item{ID: "ABC-E01", Type: domain.TypeEpic}},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if err := validateImportItem(c.it); err == nil {
				t.Errorf("expected error for %s", c.name)
			}
		})
	}
}

func TestSync_AutoCommitWithDefaultMessage(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	d := Deps{GitClient: g}
	if _, err := Sync(context.Background(), d, SyncInput{AutoCommit: true}); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	got := strings.Join(g.Calls, " ")
	if !strings.Contains(got, `Commit("task: sync")`) {
		t.Errorf("expected default commit message; got %s", got)
	}
}

func TestMergeClaims_AllPaths(t *testing.T) {
	t.Parallel()
	high := &domain.Claim{Agent: "h", LeaseID: "Z"}
	low := &domain.Claim{Agent: "l", LeaseID: "A"}

	c, lost := mergeClaims(nil, nil)
	if c != nil || lost != "" {
		t.Errorf("nil/nil: %v %s", c, lost)
	}
	c, lost = mergeClaims(low, nil)
	if c != low || lost != "" {
		t.Errorf("low/nil: %v %s", c, lost)
	}
	c, lost = mergeClaims(nil, high)
	if c != high || lost != "" {
		t.Errorf("nil/high: %v %s", c, lost)
	}
	// same lease, no loser
	c, lost = mergeClaims(low, &domain.Claim{LeaseID: low.LeaseID, Agent: "x"})
	if c == nil || lost != "" {
		t.Errorf("same: %v %s", c, lost)
	}
	// theirs higher → ours wins, theirs loses
	c, lost = mergeClaims(low, high)
	if c != low || lost != "h" {
		t.Errorf("low<high: %v %s", c, lost)
	}
	// ours higher → theirs wins, ours loses
	c, lost = mergeClaims(high, low)
	if c != low || lost != "h" {
		t.Errorf("high>low: %v %s", c, lost)
	}
}

func TestUnionJournal_Sorts(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 5, 6, 11, 0, 0, 0, time.UTC)
	a := []domain.JournalEntry{{Timestamp: t2, Actor: "a", Event: "x"}}
	b := []domain.JournalEntry{{Timestamp: t1, Actor: "b", Event: "y"}}
	got := unionJournal(a, b)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	if !got[0].Timestamp.Equal(t1) {
		t.Errorf("not sorted: %v", got)
	}
}

func TestProseConflict_BothModifiedAcceptance(t *testing.T) {
	t.Parallel()
	base := &domain.Item{AcceptanceCriteria: []string{"a"}}
	ours := domain.Item{AcceptanceCriteria: []string{"a", "b"}}
	theirs := domain.Item{AcceptanceCriteria: []string{"a", "c"}}
	if !proseConflict(base, ours, theirs) {
		t.Error("expected prose conflict on AC divergence")
	}
}

func TestPickStatus_TieFavoursOurs(t *testing.T) {
	t.Parallel()
	o := domain.Item{Version: 1, Status: domain.StatusReady}
	th := domain.Item{Version: 1, Status: domain.StatusInProgress}
	if got := pickStatus(o, th); got != domain.StatusReady {
		t.Errorf("tie: got %s, want ready", got)
	}
}

func TestExport_AllFormatsRoundtrip(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	for _, format := range []string{"graph", "cypher", "dot"} {
		out, err := Export(context.Background(), d, ExportInput{Format: format})
		if err != nil {
			t.Errorf("%s: %v", format, err)
			continue
		}
		if out.Nodes != 4 {
			t.Errorf("%s nodes = %d, want 4", format, out.Nodes)
		}
	}
}

func TestImport_MalformedPhasesJSON(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := Import(context.Background(), d, ImportInput{Body: []byte(`{"project_id":"ABC","phases":not-json}`)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestImport_PrefixOverridesProjectID(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`{"project_id":"OLD","phases":[]}`)
	out, err := Import(context.Background(), d, ImportInput{Body: body, Prefix: "ABC"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if out.Format != "phases-and-tasks" {
		t.Errorf("format = %s", out.Format)
	}
	if exists, _ := d.Store.Exists(context.Background(), "ABC"); !exists {
		t.Errorf("prefix override did not apply")
	}
}
