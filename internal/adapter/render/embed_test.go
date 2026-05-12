package render

import (
	"io/fs"
	"testing"
)

func TestEmbed_HasAllTemplates(t *testing.T) {
	t.Parallel()
	want := []string{
		"templates/board.html.tmpl",
		"templates/tree.html.tmpl",
		"templates/summary.html.tmpl",
		"templates/item.html.tmpl",
		"templates/_header.html.tmpl",
	}
	for _, name := range want {
		data, err := fs.ReadFile(templatesFS, name)
		if err != nil {
			t.Errorf("missing embedded template %s: %v", name, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("embedded template %s is empty", name)
		}
	}
}

func TestEmbed_HasStylesheet(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(themesFS, "themes/default.css")
	if err != nil {
		t.Fatalf("missing embedded stylesheet: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded stylesheet is empty")
	}
}

func TestEmbed_HasAppJS(t *testing.T) {
	t.Parallel()
	data, err := fs.ReadFile(assetsFS, "assets/app.js")
	if err != nil {
		t.Fatalf("missing embedded app.js: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("embedded app.js is empty")
	}
}

func TestAvailableThemes_ReturnsClosedSet(t *testing.T) {
	t.Parallel()
	got := AvailableThemes()
	want := map[string]bool{"auto": true, "light": true, "dark": true, "default": true}
	if len(got) != len(want) {
		t.Fatalf("themes: want %d, got %v", len(want), got)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("unexpected theme %q", name)
		}
	}
}

func TestResolveTheme_DefaultMapsToAuto(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":        "auto",
		"default": "auto",
		"auto":    "auto",
		"light":   "light",
		"dark":    "dark",
	}
	for in, want := range cases {
		if got := resolveTheme(in); got != want {
			t.Errorf("resolveTheme(%q) = %q, want %q", in, got, want)
		}
	}
}
