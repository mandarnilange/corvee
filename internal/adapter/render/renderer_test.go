package render

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func sampleManifest() domain.Manifest {
	return domain.Manifest{
		Name:            "rkn",
		Version:         "abc1234-1700000000",
		GeneratedAt:     time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Entrypoint:      "index.html",
		Static:          true,
		OutputDirectory: ".",
		Routes: []domain.ManifestRoute{
			{Src: "/", Dest: "/index.html"},
			{Src: "/tree", Dest: "/tree.html"},
			{Src: "/summary", Dest: "/summary.html"},
		},
	}
}

func sampleItems() []domain.Item {
	return []domain.Item{
		{ID: "RKN", Type: domain.TypeProject, ProjectID: "RKN", Title: "rkn project", Status: domain.StatusReady, Priority: domain.PriorityHigh},
		{ID: "RKN-E01", Type: domain.TypeEpic, ProjectID: "RKN", ParentID: "RKN", Title: "first epic", Status: domain.StatusReady, Priority: domain.PriorityHigh},
		{ID: "RKN-E01-S01", Type: domain.TypeStory, ProjectID: "RKN", ParentID: "RKN-E01", Title: "story one", Status: domain.StatusDone, Priority: domain.PriorityMedium},
		{ID: "RKN-E01-S02", Type: domain.TypeStory, ProjectID: "RKN", ParentID: "RKN-E01", Title: "story two", Status: domain.StatusBlocked, Priority: domain.PriorityHigh},
	}
}

func TestRenderer_WritesAllExpectedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := New()
	out, err := r.Render(context.Background(), domain.RenderInput{
		WorkspaceName: "rkn",
		Items:         sampleItems(),
		Theme:         "default",
		OutDir:        dir,
		Manifest:      sampleManifest(),
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Renderer writes the three top-level views, one items/<id>.html per
	// item, the unified stylesheet, the JS bundle, and manifest.json.
	want := []string{
		"index.html", "tree.html", "summary.html",
		"items/RKN.html", "items/RKN-E01.html",
		"items/RKN-E01-S01.html", "items/RKN-E01-S02.html",
		"assets/styles.css", "assets/app.js", "manifest.json",
	}
	if len(out.Files) != len(want) {
		t.Fatalf("files: got %d %v, want %d %v", len(out.Files), out.Files, len(want), want)
	}
	for i, name := range want {
		if out.Files[i] != name {
			t.Errorf("Files[%d] = %q, want %q", i, out.Files[i], name)
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full)
		if err != nil {
			t.Errorf("missing %s: %v", name, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
}

func TestRenderer_HTMLContainsItemIDs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := New()
	if _, err := r.Render(context.Background(), domain.RenderInput{
		WorkspaceName: "rkn",
		Items:         sampleItems(),
		Theme:         "default",
		OutDir:        dir,
		Manifest:      sampleManifest(),
	}); err != nil {
		t.Fatal(err)
	}
	board, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(board), "RKN-E01-S01") {
		t.Errorf("board missing item id")
	}
	tree, err := os.ReadFile(filepath.Join(dir, "tree.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tree), "RKN-E01-S02") {
		t.Errorf("tree missing item id")
	}
	summary, err := os.ReadFile(filepath.Join(dir, "summary.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), "rkn project") {
		t.Errorf("summary missing project title")
	}
	if !strings.Contains(string(summary), "RKN-E01-S02") {
		t.Errorf("summary missing blocked-item id")
	}
}

func TestRenderer_ManifestJSONIsValid(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := New()
	if _, err := r.Render(context.Background(), domain.RenderInput{
		WorkspaceName: "rkn",
		Items:         sampleItems(),
		Theme:         "default",
		OutDir:        dir,
		Manifest:      sampleManifest(),
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m domain.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("manifest.json invalid: %v", err)
	}
	if m.Name != "rkn" {
		t.Errorf("manifest name = %q", m.Name)
	}
	if len(m.Routes) != 3 {
		t.Errorf("routes = %v", m.Routes)
	}
}

func TestRenderer_ThemePropagatesToHTML(t *testing.T) {
	t.Parallel()
	// Theme is now a runtime client-side concern: the CSS is unified
	// and the --theme flag's job is to set the initial data-theme
	// attribute on <html>. Verify that contract.
	cases := map[string]string{
		"default": `data-theme="auto"`,
		"":        `data-theme="auto"`,
		"auto":    `data-theme="auto"`,
		"light":   `data-theme="light"`,
		"dark":    `data-theme="dark"`,
	}
	for theme, wantAttr := range cases {
		dir := t.TempDir()
		r := New()
		if _, err := r.Render(context.Background(), domain.RenderInput{
			WorkspaceName: "rkn",
			Items:         sampleItems(),
			Theme:         theme,
			OutDir:        dir,
			Manifest:      sampleManifest(),
		}); err != nil {
			t.Fatalf("render %q: %v", theme, err)
		}
		body, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if err != nil {
			t.Fatalf("read board for %q: %v", theme, err)
		}
		if !strings.Contains(string(body), wantAttr) {
			t.Errorf("theme %q: missing %s in rendered HTML", theme, wantAttr)
		}
	}
}

func TestRenderer_StatsCountByType(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := New()
	out, err := r.Render(context.Background(), domain.RenderInput{
		WorkspaceName: "rkn",
		Items:         sampleItems(),
		Theme:         "default",
		OutDir:        dir,
		Manifest:      sampleManifest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stats.Projects != 1 || out.Stats.Epics != 1 || out.Stats.Stories != 2 {
		t.Errorf("stats = %+v", out.Stats)
	}
}

func TestRenderer_RequiresOutDir(t *testing.T) {
	t.Parallel()
	r := New()
	_, err := r.Render(context.Background(), domain.RenderInput{Theme: "default"})
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestRenderer_ActivityFeedPreservesProvidedOrder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := New()

	// usecase.Render is responsible for ordering Activity newest-first;
	// the renderer trusts that contract. This test pins the renderer's
	// half: given an Activity slice, it emits the rows in the same
	// order in the rendered HTML — no implicit sort, no shuffle.
	activity := []domain.ActivityEvent{
		{Actor: "carol", Type: "completed", ItemID: "TASK-M"},
		{Actor: "bob", Type: "claimed", ItemID: "TASK-Z"},
		{Actor: "alice", Type: "item_added", ItemID: "TASK-A"},
	}

	if _, err := r.Render(context.Background(), domain.RenderInput{
		WorkspaceName: "rkn",
		Items:         sampleItems(),
		Activity:      activity,
		Theme:         "default",
		OutDir:        dir,
		Manifest:      sampleManifest(),
	}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "summary.html"))
	if err != nil {
		t.Fatal(err)
	}
	html := string(body)
	// Look only inside the activity feed; the per-project rollup above
	// can also mention these IDs and would produce false positives.
	feedStart := strings.Index(html, `panel activity`)
	if feedStart < 0 {
		t.Fatal("activity section missing")
	}
	feed := html[feedStart:]
	posA := strings.Index(feed, "TASK-A")
	posM := strings.Index(feed, "TASK-M")
	posZ := strings.Index(feed, "TASK-Z")
	if posA < 0 || posM < 0 || posZ < 0 {
		t.Fatalf("activity feed missing IDs: A=%d M=%d Z=%d", posA, posM, posZ)
	}
	if !(posM < posZ && posZ < posA) {
		t.Errorf("activity feed reordered by adapter: M=%d Z=%d A=%d", posM, posZ, posA)
	}
}

func TestRenderer_DeterministicOutput(t *testing.T) {
	t.Parallel()
	a, b := t.TempDir(), t.TempDir()
	r := New()
	for _, dir := range []string{a, b} {
		if _, err := r.Render(context.Background(), domain.RenderInput{
			WorkspaceName: "rkn",
			Items:         sampleItems(),
			Theme:         "default",
			OutDir:        dir,
			Manifest:      sampleManifest(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{
		"index.html", "tree.html", "summary.html",
		"items/RKN.html", "items/RKN-E01-S01.html",
		"assets/styles.css", "assets/app.js", "manifest.json",
	} {
		ad, _ := os.ReadFile(filepath.Join(a, name))
		bd, _ := os.ReadFile(filepath.Join(b, name))
		if string(ad) != string(bd) {
			t.Errorf("%s differs across runs", name)
		}
	}
}
