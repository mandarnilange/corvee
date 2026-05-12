package render

import "embed"

// templatesFS embeds the Go html/template sources used by the
// renderer. Embedding keeps the binary self-contained per spec §12.4 —
// no runtime asset directory is ever consulted.
//
//go:embed templates/board.html.tmpl templates/tree.html.tmpl templates/summary.html.tmpl templates/item.html.tmpl templates/_header.html.tmpl templates/_item_panel.html.tmpl templates/_drawer.html.tmpl
var templatesFS embed.FS

// themesFS embeds the single unified CSS bundle. The `--theme` flag
// continues to validate against this list, but the file itself handles
// every palette via [data-theme] selectors and prefers-color-scheme,
// so the runtime stylesheet is the same across all theme choices.
//
//go:embed themes/default.css
var themesFS embed.FS

// assetsFS embeds the static client-side script bundle copied verbatim
// to <out>/assets/app.js. The script wires the theme toggle and the
// tree expand/collapse shortcuts; it has no third-party dependencies.
//
//go:embed assets/app.js
var assetsFS embed.FS

// themeNames is the closed set of valid --theme values. Each name maps
// to an initial data-theme attribute on the rendered HTML; the toggle
// button in the topbar lets viewers override at runtime.
//
//	auto  — defer to the system's prefers-color-scheme (default)
//	light — force light palette
//	dark  — force dark palette
//
// The legacy name "default" is preserved as an alias for "auto" so
// older `--theme default` invocations keep working.
var themeNames = []string{"auto", "light", "dark", "default"}

// AvailableThemes returns the names a caller may pass as --theme. The
// CLI consults this list to validate user input before invoking the
// renderer.
func AvailableThemes() []string {
	out := make([]string, len(themeNames))
	copy(out, themeNames)
	return out
}

// resolveTheme maps a --theme input to the value that lands in the
// rendered HTML's data-theme attribute. "default" is an alias for
// "auto" so existing invocations keep working.
func resolveTheme(name string) string {
	if name == "default" || name == "" {
		return "auto"
	}
	return name
}
