package usecase

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/mandarnilange/corvee/internal/domain"
)

// RenderInput is the request payload for Render.
type RenderInput struct {
	// OutDir is the destination directory for HTML and asset files.
	// Empty means the caller wants the spec's default ".tasks/dist/"
	// — the CLI fills in the absolute path before invoking.
	OutDir string
	// Theme is one of the embedded theme names. Empty defaults to
	// "default".
	Theme string
	// AvailableThemes is the closed set of valid theme names. Passed
	// from the adapter (which owns the embedded theme list) so the
	// usecase can validate without importing the adapter.
	AvailableThemes []string
}

// RenderOutput is the response payload for Render. The shape mirrors
// spec §15.2: out_dir, files, manifest, stats.
type RenderOutput struct {
	// OutDir is the absolute output directory.
	OutDir string `json:"out_dir"`
	// Files is the list of OutDir-relative paths written.
	Files []string `json:"files"`
	// Manifest is the deploy-handoff descriptor written to manifest.json.
	Manifest domain.Manifest `json:"manifest"`
	// Stats is the per-type item count rollup.
	Stats domain.RenderStats `json:"stats"`
}

// Render orchestrates an HTML dashboard build. It loads items and
// events through the domain ports, validates the requested theme,
// builds the deploy manifest, and delegates every filesystem write to
// the Renderer port. usecase.Render itself touches no files on disk
// — that is the adapter's job per spec §S04.
func Render(ctx context.Context, d Deps, in RenderInput) (RenderOutput, error) {
	if d.Renderer == nil {
		return RenderOutput{}, fmt.Errorf("render: renderer not wired: %w", domain.ErrUsage)
	}
	if in.OutDir == "" {
		return RenderOutput{}, fmt.Errorf("render: out_dir is required: %w", domain.ErrUsage)
	}
	theme := in.Theme
	if theme == "" {
		theme = "default"
	}
	if !themeAvailable(theme, in.AvailableThemes) {
		return RenderOutput{}, fmt.Errorf("render: unknown theme %q: %w", theme, domain.ErrUsage)
	}

	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return RenderOutput{}, fmt.Errorf("render: list items: %w", err)
	}

	stats, activity, err := scanEvents(d)
	if err != nil {
		return RenderOutput{}, err
	}

	name, err := workspaceName(d)
	if err != nil {
		return RenderOutput{}, err
	}

	now := d.Clock.Now()
	manifest, err := BuildManifest(name, gitSHA(d), now)
	if err != nil {
		return RenderOutput{}, err
	}
	if err := ValidateManifest(manifest); err != nil {
		return RenderOutput{}, err
	}

	absOut, err := filepath.Abs(in.OutDir)
	if err != nil {
		return RenderOutput{}, fmt.Errorf("render: resolve out_dir: %w", err)
	}

	critical := buildCriticalPath(d, items)

	rin := domain.RenderInput{
		WorkspaceName: name,
		Items:         items,
		EventStats:    stats,
		Activity:      activity,
		Theme:         theme,
		OutDir:        absOut,
		Manifest:      manifest,
		CriticalPath:  critical,
	}
	rout, err := d.Renderer.Render(ctx, rin)
	if err != nil {
		return RenderOutput{}, fmt.Errorf("render: %w", err)
	}

	return RenderOutput{
		OutDir:   absOut,
		Files:    rout.Files,
		Manifest: manifest,
		Stats:    rout.Stats,
	}, nil
}

// themeAvailable reports whether name is in available. An empty
// available list short-circuits to true so unit tests that don't wire
// a Renderer adapter can still exercise Render's orchestration.
func themeAvailable(name string, available []string) bool {
	if len(available) == 0 {
		return true
	}
	for _, a := range available {
		if a == name {
			return true
		}
	}
	return false
}

// workspaceName picks the human-facing label rendered in page headers
// and stamped into the manifest. WorkspaceLoader is consulted when
// available; absent or erroring loaders fall back to "workspace" — a
// missing workspace.json should not block the read-only render path.
func workspaceName(d Deps) (string, error) {
	if d.Workspace == nil {
		return "workspace", nil
	}
	ws, err := d.Workspace.Load()
	if err != nil {
		return "workspace", nil //nolint:nilerr // intentional fallback per spec §17.2
	}
	if ws.WorkspaceName != "" {
		return ws.WorkspaceName, nil
	}
	return "workspace", nil
}

// activityWindow caps how many events the dashboard's "Recent
// Activity" feed surfaces. The cap also bounds memory during the
// streaming scan: only the latest N events are retained, never the
// full log.
const activityWindow = 20

// scanEvents streams the audit log through EventLog.Scan, accumulating
// the velocity panel's aggregates and the dashboard's activity feed
// without ever materializing the full history. Memory cost is O(N)
// where N == activityWindow + |distinct actors|.
//
// The activity slice is returned newest-first so the renderer can echo
// it as-is.
func scanEvents(d Deps) (domain.EventStats, []domain.ActivityEvent, error) {
	if d.EventLog == nil {
		return domain.EventStats{}, nil, nil
	}
	actors := map[string]struct{}{}
	stats := domain.EventStats{}

	// Ring buffer holds the last activityWindow events in arrival
	// order; head is the next write slot. We accept up to N events;
	// older ones are overwritten.
	ring := make([]domain.ActivityEvent, activityWindow)
	var head, count int

	scanErr := d.EventLog.Scan(func(ev domain.Event) error {
		if ev.Type == "completed" {
			stats.CompletedCount++
		}
		if ev.Actor != "" {
			actors[ev.Actor] = struct{}{}
		}
		ring[head] = domain.ActivityEvent{Actor: ev.Actor, Type: ev.Type, ItemID: ev.ItemID, Timestamp: ev.Timestamp}
		head = (head + 1) % activityWindow
		count++
		return nil
	})
	if scanErr != nil {
		return domain.EventStats{}, nil, fmt.Errorf("render: scan events: %w", scanErr)
	}
	stats.DistinctActors = len(actors)

	// Materialize the ring buffer in newest-first order. When count <
	// activityWindow the buffer is partially filled starting at index
	// 0; when it has wrapped, the oldest sits at `head`.
	size := count
	if size > activityWindow {
		size = activityWindow
	}
	out := make([]domain.ActivityEvent, size)
	for i := range size {
		// The element written most recently lives at (head-1); walk
		// backwards from there.
		idx := (head - 1 - i + activityWindow) % activityWindow
		out[i] = ring[idx]
	}
	return stats, out, nil
}

// buildCriticalPath resolves the workspace's configured critical_path
// IDs against the current item set. A nil/missing workspace loader or
// empty critical_path returns nil — the renderer hides the panel.
func buildCriticalPath(d Deps, items []domain.Item) []domain.RenderCriticalPathStep {
	if d.Workspace == nil {
		return nil
	}
	ws, err := d.Workspace.Load()
	if err != nil || len(ws.CriticalPath) == 0 {
		return nil
	}
	byID := make(map[string]domain.Item, len(items))
	for _, it := range items {
		byID[it.ID] = it
	}
	steps := make([]domain.RenderCriticalPathStep, 0, len(ws.CriticalPath))
	for _, id := range ws.CriticalPath {
		step := domain.RenderCriticalPathStep{ID: id}
		if it, ok := byID[id]; ok {
			step.Title = it.Title
			step.Status = it.Status
			step.Found = true
		}
		steps = append(steps, step)
	}
	return steps
}

// gitSHA returns the short HEAD hash if a Git port is wired, else "".
// All errors fall through to "" so the manifest's dev-version fallback
// kicks in — the deploy handoff must succeed even outside a repo.
func gitSHA(d Deps) string {
	if d.Git == nil {
		return ""
	}
	sha, err := d.Git.SHA()
	if err != nil {
		return ""
	}
	return sha
}
