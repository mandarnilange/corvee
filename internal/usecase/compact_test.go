package usecase

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestCompact_ArchivesOldShards(t *testing.T) {
	t.Parallel()
	tasksDir := t.TempDir()
	eventsDir := filepath.Join(tasksDir, "events")
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(eventsDir, "2025-01-01.jsonl")
	recent := filepath.Join(eventsDir, "2026-05-05.jsonl")
	if err := os.WriteFile(old, []byte(`{"event_id":"e1"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recent, []byte(`{"event_id":"e2"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := Deps{
		Clock: testfakes.NewClock(time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)),
	}
	out, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if out.ShardsArchived != 1 {
		t.Errorf("shards archived = %d, want 1", out.ShardsArchived)
	}
	// Old shard moved to archive subdir.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("old shard still in events dir")
	}
	if _, err := os.Stat(filepath.Join(eventsDir, "archive", "2025-01-01.jsonl")); err != nil {
		t.Errorf("archive missing: %v", err)
	}
}

func TestCompact_PrunesCompletedOps(t *testing.T) {
	t.Parallel()
	tasksDir := t.TempDir()
	opsDir := filepath.Join(tasksDir, "operations")
	if err := os.MkdirAll(opsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(opsDir, "old.json")
	if err := os.WriteFile(old, []byte(`{"status":"completed"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Backdate the file so the cutoff considers it.
	past := time.Now().Add(-200 * 24 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}

	d := Deps{Clock: testfakes.NewClock(time.Now())}
	out, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if out.OpsPruned != 1 {
		t.Errorf("ops pruned = %d, want 1", out.OpsPruned)
	}
}

func TestCompact_Idempotent(t *testing.T) {
	t.Parallel()
	tasksDir := t.TempDir()
	d := Deps{Clock: testfakes.NewClock(time.Now())}
	out1, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatal(err)
	}
	out2, err := Compact(context.Background(), d, CompactInput{TasksDir: tasksDir})
	if err != nil {
		t.Fatal(err)
	}
	if out2.ShardsArchived != 0 || out2.OpsPruned != 0 {
		t.Errorf("second pass should be no-op; got %+v", out2)
	}
	_ = out1
}
