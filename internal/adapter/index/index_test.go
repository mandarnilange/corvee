package index

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestIndexer_Load_MissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := New(filepath.Join(dir, "index.json"), filepath.Join(dir, "items"))
	_, err := idx.Load()
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestIndexer_SaveLoad_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx := New(filepath.Join(dir, "index.json"), itemsDir)
	now := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	want := domain.Index{
		SchemaVersion:    1,
		BuiltAt:          now,
		MaxItemUpdatedAt: now,
		ItemCount:        1,
		Items:            []domain.IndexEntry{{ID: "RKN-E01-S01", Type: domain.TypeStory, UpdatedAt: now, Version: 1}},
	}
	if err := idx.Save(want); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := idx.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ItemCount != 1 || got.Items[0].ID != "RKN-E01-S01" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestIndexer_Stale_NoCacheIsStale(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	idx := New(filepath.Join(dir, "index.json"), filepath.Join(dir, "items"))
	stale, err := idx.Stale()
	if err != nil {
		t.Fatalf("stale: %v", err)
	}
	if !stale {
		t.Error("missing cache should be stale")
	}
}

func TestIndexer_Stale_FreshCacheReportsFalse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	writeItem(t, itemsDir, "RKN-E01-S01", now)

	idx := New(filepath.Join(dir, "index.json"), itemsDir)
	if err := idx.Save(domain.Index{
		SchemaVersion:    1,
		BuiltAt:          now,
		MaxItemUpdatedAt: now,
		ItemCount:        1,
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := idx.Stale()
	if err != nil {
		t.Fatal(err)
	}
	if stale {
		t.Error("freshly-built cache should not be stale")
	}
}

func TestIndexer_Stale_DetectsNewerItem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	writeItem(t, itemsDir, "RKN-E01-S01", now)

	idx := New(filepath.Join(dir, "index.json"), itemsDir)
	if err := idx.Save(domain.Index{
		SchemaVersion: 1,
		BuiltAt:       now,
		ItemCount:     1,
	}); err != nil {
		t.Fatal(err)
	}

	// Touch the item so its file ModTime advances. Stale() should
	// notice via the stat comparison without re-reading payloads.
	time.Sleep(10 * time.Millisecond)
	writeItem(t, itemsDir, "RKN-E01-S01", now)

	stale, err := idx.Stale()
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("cache lagging an item file ModTime should be stale")
	}
}

func TestIndexer_Stale_DetectsItemCountChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	itemsDir := filepath.Join(dir, "items")
	if err := os.MkdirAll(itemsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	writeItem(t, itemsDir, "RKN-E01-S01", now)
	writeItem(t, itemsDir, "RKN-E01-S02", now)

	idx := New(filepath.Join(dir, "index.json"), itemsDir)
	if err := idx.Save(domain.Index{
		SchemaVersion:    1,
		BuiltAt:          now,
		MaxItemUpdatedAt: now,
		ItemCount:        1,
	}); err != nil {
		t.Fatal(err)
	}
	stale, err := idx.Stale()
	if err != nil {
		t.Fatal(err)
	}
	if !stale {
		t.Error("cache count mismatch should be stale")
	}
}

func writeItem(t *testing.T, dir, id string, updated time.Time) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"id":         id,
		"updated_at": updated,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}
