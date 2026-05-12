package usecase

import (
	"context"
	"strings"
	"testing"
)

func setupExportFixture(t *testing.T) Deps {
	t.Helper()
	d := newImportDeps(t)
	body := []byte(`[
		{"id":"ABC","type":"project","title":"P"},
		{"id":"ABC-E01","type":"epic","title":"E1"},
		{"id":"ABC-E01-S01","type":"story","title":"S1","dependencies":["ABC-E01-S02"],"aliases":["OLD-E01-S01"]},
		{"id":"ABC-E01-S02","type":"story","title":"S2","blocks":["ABC-E01-S01"]}
	]`)
	if _, err := Import(context.Background(), d, ImportInput{Body: body, Agent: "a"}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	return d
}

func TestExport_Graph(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	out, err := Export(context.Background(), d, ExportInput{Format: "graph"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if out.Nodes != 4 {
		t.Errorf("nodes = %d, want 4", out.Nodes)
	}
	// Edges: 3 parent + 1 dependency + 1 blocks + 1 alias = 6
	if out.Edges != 6 {
		t.Errorf("edges = %d, want 6", out.Edges)
	}
	if !strings.Contains(string(out.Body), `"nodes"`) {
		t.Errorf("missing nodes key")
	}
}

func TestExport_GraphIncludeFilter(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	out, err := Export(context.Background(), d, ExportInput{Format: "graph", Include: []string{"parent"}})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if out.Edges != 3 {
		t.Errorf("parent-only edges = %d, want 3", out.Edges)
	}
}

func TestExport_Cypher(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	out, err := Export(context.Background(), d, ExportInput{Format: "cypher"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !strings.Contains(string(out.Body), "CREATE (ABC:Item") {
		t.Errorf("cypher output missing CREATE: %s", out.Body)
	}
}

func TestExport_Dot(t *testing.T) {
	t.Parallel()
	d := setupExportFixture(t)
	out, err := Export(context.Background(), d, ExportInput{Format: "dot"})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	s := string(out.Body)
	if !strings.HasPrefix(s, "digraph workspace") {
		t.Errorf("dot missing prefix: %s", s)
	}
}
