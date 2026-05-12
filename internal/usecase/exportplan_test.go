package usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestExportPlan_NativeRoundTrips(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P","status":"ready"},
		{"id":"ABC-E01","type":"epic","title":"E","status":"ready"},
		{"id":"ABC-E01-S01","type":"story","title":"S","status":"ready"}
	]`)
	if _, err := Import(context.Background(), d, ImportInput{Body: body, Agent: "a"}); err != nil {
		t.Fatalf("import: %v", err)
	}

	out, err := ExportPlan(context.Background(), d, ExportPlanInput{Format: "native"})
	if err != nil {
		t.Fatalf("ExportPlan: %v", err)
	}
	if out.Items != 3 {
		t.Errorf("items = %d, want 3", out.Items)
	}

	// Round-trip: feed export into a fresh deps and import again.
	d2 := newImportDeps(t)
	if _, err := Import(context.Background(), d2, ImportInput{Body: out.Body, Agent: "a"}); err != nil {
		t.Fatalf("re-import: %v", err)
	}
	items, _ := d2.Store.List(context.Background(), domain.ListFilter{})
	if got := len(items); got != 3 {
		t.Errorf("re-imported count = %d, want 3", got)
	}
}

func TestExportPlan_PhasesShape(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P","status":"ready"},
		{"id":"ABC-E01","type":"epic","title":"E","status":"ready"},
		{"id":"ABC-E01-S01","type":"story","title":"S","status":"ready"}
	]`)
	_, _ = Import(context.Background(), d, ImportInput{Body: body, Agent: "a"})

	out, err := ExportPlan(context.Background(), d, ExportPlanInput{Format: "phases"})
	if err != nil {
		t.Fatalf("ExportPlan: %v", err)
	}
	s := string(out.Body)
	if !strings.Contains(s, `"project_id": "ABC"`) {
		t.Errorf("phases doc missing project_id: %s", s)
	}
	if !strings.Contains(s, `"phases"`) {
		t.Errorf("phases doc missing phases: %s", s)
	}
}

func TestExportPlan_Markdown(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, _ = Import(context.Background(), d, ImportInput{Body: []byte(`[
		{"id":"ABC","type":"project","title":"Proj"},
		{"id":"ABC-E01","type":"epic","title":"Epic1","deliverable":"X"}
	]`), Agent: "a"})

	out, err := ExportPlan(context.Background(), d, ExportPlanInput{Format: "markdown"})
	if err != nil {
		t.Fatalf("ExportPlan: %v", err)
	}
	s := string(out.Body)
	if !strings.Contains(s, "# ABC — Proj") {
		t.Errorf("markdown missing project header: %s", s)
	}
	if !strings.Contains(s, "**Deliverable:** X") {
		t.Errorf("markdown missing deliverable: %s", s)
	}
}

func TestExportPlan_UnknownFormat(t *testing.T) {
	t.Parallel()
	d := newImportDeps(t)
	_, err := ExportPlan(context.Background(), d, ExportPlanInput{Format: "xml"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}
