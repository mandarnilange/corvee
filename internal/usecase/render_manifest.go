package usecase

import (
	"fmt"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// defaultManifestRoutes returns the canonical /, /tree, /summary route
// table from spec §17.2. The slice is constructed fresh per call so
// callers can mutate without affecting future invocations.
func defaultManifestRoutes() []domain.ManifestRoute {
	return []domain.ManifestRoute{
		{Src: "/", Dest: "/index.html"},
		{Src: "/tree", Dest: "/tree.html"},
		{Src: "/summary", Dest: "/summary.html"},
	}
}

// BuildManifest assembles the deploy-handoff descriptor written to
// manifest.json per spec §17.2. sha may be empty — in that case the
// version field falls back to the "0.0.0-dev-<unix-ts>" form. now
// supplies both the version timestamp and the generated_at field, so
// callers (and snapshot tests) can pin the rendering to a fixed clock.
func BuildManifest(name, sha string, now time.Time) (domain.Manifest, error) {
	if name == "" {
		return domain.Manifest{}, fmt.Errorf("render manifest: name is required: %w", domain.ErrUsage)
	}
	ts := now.UTC().Unix()
	version := manifestVersion(sha, ts)
	return domain.Manifest{
		Name:            name,
		Version:         version,
		GeneratedAt:     now.UTC().Format(time.RFC3339),
		Entrypoint:      "index.html",
		Static:          true,
		Framework:       nil,
		BuildCommand:    nil,
		OutputDirectory: ".",
		Routes:          defaultManifestRoutes(),
	}, nil
}

// manifestVersion returns "<sha>-<ts>" when a git SHA is supplied,
// otherwise "0.0.0-dev-<ts>" per spec §17.2.
func manifestVersion(sha string, ts int64) string {
	sha = strings.TrimSpace(sha)
	if sha == "" {
		return fmt.Sprintf("0.0.0-dev-%d", ts)
	}
	return fmt.Sprintf("%s-%d", sha, ts)
}

// ValidateManifest enforces the §17.2 schema invariants: required
// fields are non-empty, framework / build_command are nil, the route
// table is well-formed. Used both in tests and at the end of Render
// before handing the manifest to the renderer.
func ValidateManifest(m domain.Manifest) error {
	if m.Name == "" {
		return fmt.Errorf("manifest: name required: %w", domain.ErrSchemaInvalid)
	}
	if m.Version == "" {
		return fmt.Errorf("manifest: version required: %w", domain.ErrSchemaInvalid)
	}
	if m.GeneratedAt == "" {
		return fmt.Errorf("manifest: generated_at required: %w", domain.ErrSchemaInvalid)
	}
	if m.Entrypoint != "index.html" {
		return fmt.Errorf("manifest: entrypoint must be index.html: %w", domain.ErrSchemaInvalid)
	}
	if !m.Static {
		return fmt.Errorf("manifest: static must be true: %w", domain.ErrSchemaInvalid)
	}
	if m.Framework != nil {
		return fmt.Errorf("manifest: framework must be null: %w", domain.ErrSchemaInvalid)
	}
	if m.BuildCommand != nil {
		return fmt.Errorf("manifest: build_command must be null: %w", domain.ErrSchemaInvalid)
	}
	if m.OutputDirectory != "." {
		return fmt.Errorf("manifest: output_directory must be \".\": %w", domain.ErrSchemaInvalid)
	}
	if len(m.Routes) == 0 {
		return fmt.Errorf("manifest: routes required: %w", domain.ErrSchemaInvalid)
	}
	for i, r := range m.Routes {
		if r.Src == "" || r.Dest == "" {
			return fmt.Errorf("manifest: route[%d] src/dest required: %w", i, domain.ErrSchemaInvalid)
		}
	}
	return nil
}
