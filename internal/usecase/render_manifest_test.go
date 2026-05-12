package usecase

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestBuildManifest_PopulatesAllFields(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 10, 30, 0, 0, time.UTC)
	m, err := BuildManifest("rkn", "abc1234", now)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "rkn" {
		t.Errorf("name = %q", m.Name)
	}
	if !strings.HasPrefix(m.Version, "abc1234-") {
		t.Errorf("version = %q, want prefix abc1234-", m.Version)
	}
	if m.GeneratedAt != "2026-05-06T10:30:00Z" {
		t.Errorf("generated_at = %q", m.GeneratedAt)
	}
	if m.Entrypoint != "index.html" {
		t.Errorf("entrypoint = %q", m.Entrypoint)
	}
	if !m.Static {
		t.Error("static must be true")
	}
	if m.Framework != nil || m.BuildCommand != nil {
		t.Errorf("framework=%v build_command=%v", m.Framework, m.BuildCommand)
	}
	if m.OutputDirectory != "." {
		t.Errorf("output_directory = %q", m.OutputDirectory)
	}
	if len(m.Routes) != 3 {
		t.Fatalf("routes: %+v", m.Routes)
	}
	wantRoutes := map[string]string{"/": "/index.html", "/tree": "/tree.html", "/summary": "/summary.html"}
	for _, r := range m.Routes {
		if wantRoutes[r.Src] != r.Dest {
			t.Errorf("route %q -> %q (want %q)", r.Src, r.Dest, wantRoutes[r.Src])
		}
	}
}

func TestBuildManifest_FallsBackWhenSHAEmpty(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	m, err := BuildManifest("rkn", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(m.Version, "0.0.0-dev-") {
		t.Errorf("dev fallback expected, got %q", m.Version)
	}
}

func TestBuildManifest_RequiresName(t *testing.T) {
	t.Parallel()
	_, err := BuildManifest("", "abc", time.Now())
	if !errors.Is(err, domain.ErrUsage) {
		t.Fatalf("err = %v, want ErrUsage", err)
	}
}

func TestValidateManifest_AcceptsBuilt(t *testing.T) {
	t.Parallel()
	m, _ := BuildManifest("rkn", "abc", time.Now().UTC())
	if err := ValidateManifest(m); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateManifest_RejectsBrokenFields(t *testing.T) {
	t.Parallel()
	good, _ := BuildManifest("rkn", "abc", time.Now().UTC())
	cases := []struct {
		name   string
		mutate func(*domain.Manifest)
	}{
		{"missing-name", func(m *domain.Manifest) { m.Name = "" }},
		{"missing-version", func(m *domain.Manifest) { m.Version = "" }},
		{"missing-generated-at", func(m *domain.Manifest) { m.GeneratedAt = "" }},
		{"wrong-entrypoint", func(m *domain.Manifest) { m.Entrypoint = "main.html" }},
		{"static-false", func(m *domain.Manifest) { m.Static = false }},
		{"framework-set", func(m *domain.Manifest) { s := "next"; m.Framework = &s }},
		{"build-cmd-set", func(m *domain.Manifest) { s := "make"; m.BuildCommand = &s }},
		{"output-dir-wrong", func(m *domain.Manifest) { m.OutputDirectory = "dist" }},
		{"no-routes", func(m *domain.Manifest) { m.Routes = nil }},
		{"empty-route-src", func(m *domain.Manifest) { m.Routes[0].Src = "" }},
		{"empty-route-dest", func(m *domain.Manifest) { m.Routes[0].Dest = "" }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := good
			m.Routes = append([]domain.ManifestRoute{}, good.Routes...)
			tc.mutate(&m)
			if err := ValidateManifest(m); !errors.Is(err, domain.ErrSchemaInvalid) {
				t.Errorf("err = %v, want ErrSchemaInvalid", err)
			}
		})
	}
}
