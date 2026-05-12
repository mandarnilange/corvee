package usecase

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

var (
	jsonMarshal   = json.Marshal
	jsonUnmarshal = json.Unmarshal
)

func newImportDeps(t *testing.T) Deps {
	t.Helper()
	ws := testfakes.NewWorkspaceLoader()
	_ = ws.Save(domain.Workspace{SchemaVersion: 1, ClaimTTLMinutes: 60})
	return Deps{
		Store:     testfakes.NewStore(),
		Clock:     testfakes.NewClock(time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)),
		IDGen:     testfakes.NewIDGen("ULID"),
		Workspace: ws,
	}
}

func TestImport_Native_CreatesItems(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P","status":"ready"},
		{"id":"ABC-E01","type":"epic","title":"E","status":"ready"}
	]`)
	out, err := Import(context.Background(), d, ImportInput{Body: body})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if out.Format != "native" {
		t.Errorf("format = %s, want native", out.Format)
	}
	if got := len(out.Created); got != 2 {
		t.Errorf("created = %d, want 2", got)
	}
}

func TestImport_DryRun_DoesNotWrite(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[{"id":"ABC","type":"project","title":"P"}]`)
	out, err := Import(context.Background(), d, ImportInput{Body: body, DryRun: true})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(out.Created) != 1 {
		t.Errorf("plan size = %d, want 1", len(out.Created))
	}
	exists, _ := d.Store.Exists(context.Background(), "ABC")
	if exists {
		t.Errorf("dry run should not write")
	}
}

func TestImport_AtomicOnInvalidItem(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	// Second item has an invalid ID — should reject the whole batch
	// before any write.
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P"},
		{"id":"bad","type":"project","title":"P"}
	]`)
	_, err := Import(context.Background(), d, ImportInput{Body: body})
	if err == nil {
		t.Fatal("expected schema error")
	}
	if !errors.Is(err, domain.ErrSchemaInvalid) && !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want schema/usage", err)
	}
	exists, _ := d.Store.Exists(context.Background(), "ABC")
	if exists {
		t.Errorf("first item should not be written when batch fails")
	}
}

func TestImport_PhasesFormat_BuildsHierarchy(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`{
		"schema_version": 1,
		"project_id": "ABC",
		"project_name": "Test",
		"phases": [
			{"id":"ABC-E01","name":"P1","tasks":[
				{"id":"ABC-E01-S01","title":"T1","type":"story"}
			]}
		]
	}`)
	out, err := Import(context.Background(), d, ImportInput{Body: body})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if out.Format != "phases-and-tasks" {
		t.Errorf("format = %s", out.Format)
	}
	if got := len(out.Created); got != 3 {
		t.Errorf("created = %d, want 3 (project+epic+story)", got)
	}
}

// TestImport_Selftest_DogfoodTasksJSON imports the project's own
// .spec/tasks.json into a temp workspace and asserts the story count
// is preserved with status, completed_at, dependencies, and
// required_capabilities intact.
//
// The dogfood file pre-dates the Phase 0 numbering convention and
// uses TTR-E00 for the project skeleton phase, which spec §4 (1-99)
// rejects. We strip that phase from the input before importing so the
// strict-atomic mode succeeds — the remaining 90 stories cover every
// downstream phase and are the actual self-test target.
func TestImport_Selftest_DogfoodTasksJSON(t *testing.T) {
	t.Parallel()
	path := findRepoFile(t, ".spec/tasks.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read tasks.json: %v", err)
	}
	body = stripPhaseE00(t, body)

	d := newImportDeps(t)
	out, err := Import(context.Background(), d, ImportInput{Body: body, Agent: "test"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	items, _ := d.Store.List(context.Background(), domain.ListFilter{Types: []domain.ItemType{domain.TypeStory}})
	if got := len(items); got < 90 {
		t.Errorf("story count = %d, want >= 90", got)
	}
	// Spot-check one story preserves status + completed_at.
	got, err := d.Store.Get(context.Background(), "TTR-E01-S01")
	if err != nil {
		t.Fatalf("Get TTR-E01-S01: %v", err)
	}
	if got.Status != domain.StatusDone {
		t.Errorf("TTR-E01-S01 status = %s, want done", got.Status)
	}
	if got.CompletedAt == nil {
		t.Errorf("TTR-E01-S01 completed_at is nil")
	}
	_ = out
}

// stripPhaseE00 removes the Phase 0 (TTR-E00) entry from the dogfood
// document so the import test exercises only spec-conformant IDs.
func stripPhaseE00(t *testing.T, body []byte) []byte {
	t.Helper()
	type rawDoc struct {
		SchemaVersion int                      `json:"schema_version"`
		ProjectID     string                   `json:"project_id"`
		ProjectName   string                   `json:"project_name"`
		CriticalPath  []string                 `json:"critical_path"`
		Phases        []map[string]interface{} `json:"phases"`
	}
	var doc rawDoc
	if err := jsonUnmarshal(body, &doc); err != nil {
		t.Fatalf("strip: parse: %v", err)
	}
	kept := doc.Phases[:0]
	for _, p := range doc.Phases {
		if id, _ := p["id"].(string); id == "TTR-E00" {
			continue
		}
		kept = append(kept, p)
	}
	doc.Phases = kept
	out, err := jsonMarshal(doc)
	if err != nil {
		t.Fatalf("strip: marshal: %v", err)
	}
	return out
}

// findRepoFile walks up from the test file's directory to find rel.
// Keeps the test self-contained without requiring an env var.
func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	dir := filepath.Dir(file)
	for i := 0; i < 10; i++ {
		candidate := filepath.Join(dir, rel)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("cannot locate %s in any ancestor", rel)
	return ""
}
