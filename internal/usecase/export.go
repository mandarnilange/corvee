package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ExportInput is the request payload for Export.
type ExportInput struct {
	// Format is one of "graph" | "cypher" | "dot".
	Format string
	// Include filters edge types: "parent", "dependency", "blocks",
	// "alias". Empty includes all four.
	Include []string
}

// ExportOutput is the response payload for Export.
type ExportOutput struct {
	// Format echoes the requested format.
	Format string `json:"format"`
	// Nodes is the count of nodes emitted.
	Nodes int `json:"nodes"`
	// Edges is the count of edges emitted.
	Edges int `json:"edges"`
	// Body is the rendered document.
	Body []byte `json:"-"`
}

// Export emits the workspace as a portable graph for D3, Cytoscape,
// Neo4j, etc. Three formats are supported per spec §15.2; --include
// trims the edge set.
func Export(ctx context.Context, d Deps, in ExportInput) (ExportOutput, error) {
	if d.Store == nil {
		return ExportOutput{}, fmt.Errorf("export: store not wired: %w", domain.ErrUsage)
	}
	format := in.Format
	if format == "" {
		format = "graph"
	}
	switch format {
	case "graph", "cypher", "dot":
	default:
		return ExportOutput{}, fmt.Errorf("export: unknown format %q: %w", format, domain.ErrUsage)
	}

	include := normalizeInclude(in.Include)

	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return ExportOutput{}, fmt.Errorf("export: list: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	nodes, edges := buildGraph(items, include)

	switch format {
	case "graph":
		return exportGraphJSON(nodes, edges)
	case "cypher":
		return exportCypher(nodes, edges)
	case "dot":
		return exportDOT(nodes, edges)
	}
	return ExportOutput{}, fmt.Errorf("export: unhandled format: %w", domain.ErrUsage)
}

// graphNode is one entry in the JSON Graph Format `nodes` array.
type graphNode struct {
	ID     string         `json:"id"`
	Label  string         `json:"label"`
	Type   string         `json:"type"`
	Status string         `json:"status"`
	Meta   map[string]any `json:"meta,omitempty"`
}

// graphEdge is one entry in the JSON Graph Format `edges` array.
type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Kind   string `json:"kind"`
}

func normalizeInclude(in []string) map[string]bool {
	if len(in) == 0 {
		return map[string]bool{"parent": true, "dependency": true, "blocks": true, "alias": true}
	}
	out := map[string]bool{}
	for _, k := range in {
		out[strings.ToLower(strings.TrimSpace(k))] = true
	}
	return out
}

func buildGraph(items []domain.Item, include map[string]bool) ([]graphNode, []graphEdge) {
	nodes := make([]graphNode, 0, len(items))
	edges := []graphEdge{}
	for _, it := range items {
		nodes = append(nodes, graphNode{
			ID:     it.ID,
			Label:  it.Title,
			Type:   string(it.Type),
			Status: string(it.Status),
		})
		if include["parent"] && it.ParentID != "" {
			edges = append(edges, graphEdge{Source: it.ID, Target: it.ParentID, Kind: "parent"})
		}
		if include["dependency"] {
			for _, dep := range it.Dependencies {
				edges = append(edges, graphEdge{Source: it.ID, Target: dep, Kind: "dependency"})
			}
		}
		if include["blocks"] {
			for _, dep := range it.Blocks {
				edges = append(edges, graphEdge{Source: it.ID, Target: dep, Kind: "blocks"})
			}
		}
		if include["alias"] {
			for _, a := range it.Aliases {
				edges = append(edges, graphEdge{Source: it.ID, Target: a, Kind: "alias"})
			}
		}
	}
	return nodes, edges
}

func exportGraphJSON(nodes []graphNode, edges []graphEdge) (ExportOutput, error) {
	doc := struct {
		Nodes []graphNode `json:"nodes"`
		Edges []graphEdge `json:"edges"`
	}{Nodes: nodes, Edges: edges}
	body, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return ExportOutput{}, fmt.Errorf("export graph: %w", err)
	}
	return ExportOutput{Format: "graph", Nodes: len(nodes), Edges: len(edges), Body: body}, nil
}

// cypherEscape doubles single quotes inside cypher string literals.
func cypherEscape(s string) string {
	return strings.ReplaceAll(s, "'", `\'`)
}

// cypherID rewrites ID hyphens to underscores so the result can be a
// neo4j identifier directly.
func cypherID(id string) string {
	return strings.NewReplacer("-", "_").Replace(id)
}

func exportCypher(nodes []graphNode, edges []graphEdge) (ExportOutput, error) {
	var b strings.Builder
	for _, n := range nodes {
		fmt.Fprintf(&b, "CREATE (%s:Item {id: '%s', title: '%s', type: '%s', status: '%s'});\n",
			cypherID(n.ID), cypherEscape(n.ID), cypherEscape(n.Label), n.Type, n.Status)
	}
	for _, e := range edges {
		rel := strings.ToUpper(e.Kind)
		fmt.Fprintf(&b, "MATCH (a:Item {id: '%s'}), (b:Item {id: '%s'}) CREATE (a)-[:%s]->(b);\n",
			cypherEscape(e.Source), cypherEscape(e.Target), rel)
	}
	return ExportOutput{Format: "cypher", Nodes: len(nodes), Edges: len(edges), Body: []byte(b.String())}, nil
}

// dotID escapes hyphens for Graphviz; quotes the whole identifier so
// any character is permissible.
func dotID(id string) string {
	return `"` + strings.ReplaceAll(id, `"`, `\"`) + `"`
}

func exportDOT(nodes []graphNode, edges []graphEdge) (ExportOutput, error) {
	var b strings.Builder
	b.WriteString("digraph workspace {\n")
	b.WriteString("  rankdir=LR;\n")
	for _, n := range nodes {
		fmt.Fprintf(&b, "  %s [label=%q,type=%q,status=%q];\n",
			dotID(n.ID), n.Label, n.Type, n.Status)
	}
	for _, e := range edges {
		fmt.Fprintf(&b, "  %s -> %s [label=%q];\n", dotID(e.Source), dotID(e.Target), e.Kind)
	}
	b.WriteString("}\n")
	return ExportOutput{Format: "dot", Nodes: len(nodes), Edges: len(edges), Body: []byte(b.String())}, nil
}
