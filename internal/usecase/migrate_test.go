package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func migrateDeps(store *testfakes.Store) Deps {
	return Deps{
		Store: store,
		Clock: testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
	}
}

func TestMigrate_NoItemsReturnsEmpty(t *testing.T) {
	t.Parallel()
	d := migrateDeps(testfakes.NewStore())
	out, err := Migrate(context.Background(), d, MigrateInput{})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(out.Migrated) != 0 || len(out.Skipped) != 0 {
		t.Errorf("empty workspace: got migrated=%d skipped=%d", len(out.Migrated), len(out.Skipped))
	}
}

func TestMigrate_CurrentVersionItemsNotMigrated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Item already at CurrentSchemaVersion — should not be in migrated list.
	it := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "RKN", Type: domain.TypeProject, Title: "p",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, it, -1); err != nil {
		t.Fatal(err)
	}
	d := migrateDeps(store)
	out, err := Migrate(ctx, d, MigrateInput{})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(out.Migrated) != 0 {
		t.Errorf("current-version item should not be migrated, got %v", out.Migrated)
	}
}

func TestMigrate_FutureVersionItemSkipped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Item at a version newer than the binary supports.
	it := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion + 1,
		ID:            "RKN", Type: domain.TypeProject, Title: "p",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	// Bypass schema validation by writing directly to the fake store
	// with -1 (no version check). In production fsstore does the same.
	if _, err := store.Put(ctx, it, -1); err != nil {
		t.Fatal(err)
	}
	d := migrateDeps(store)
	out, err := Migrate(ctx, d, MigrateInput{})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(out.Skipped) != 1 {
		t.Fatalf("want 1 skipped, got %d", len(out.Skipped))
	}
	if out.Skipped[0].ID != "RKN" {
		t.Errorf("skipped ID: got %q", out.Skipped[0].ID)
	}
	if out.Skipped[0].Reason != "binary too old" {
		t.Errorf("skipped reason: got %q want %q", out.Skipped[0].Reason, "binary too old")
	}
	// Skipped item must NOT be modified.
	got, _ := store.Get(ctx, "RKN")
	if got.SchemaVersion != domain.CurrentSchemaVersion+1 {
		t.Error("future-version item should not be downgraded")
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	it := domain.Item{
		SchemaVersion: domain.CurrentSchemaVersion,
		ID:            "AAA", Type: domain.TypeProject, Title: "p",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, it, -1); err != nil {
		t.Fatal(err)
	}
	d := migrateDeps(store)
	// Run twice — should be idempotent.
	for i := 0; i < 2; i++ {
		out, err := Migrate(ctx, d, MigrateInput{})
		if err != nil {
			t.Fatalf("run %d: %v", i+1, err)
		}
		if len(out.Migrated) != 0 {
			t.Errorf("run %d: should not migrate up-to-date items", i+1)
		}
	}
}

func TestMigrate_Output_HasVersionFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Simulate a v0 item (before current schema).
	old := domain.Item{
		SchemaVersion: 0,
		ID:            "ZZZ", Type: domain.TypeProject, Title: "old",
		Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, old, -1); err != nil {
		t.Fatal(err)
	}
	d := migrateDeps(store)
	out, err := Migrate(ctx, d, MigrateInput{})
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if len(out.Migrated) != 1 {
		t.Fatalf("want 1 migrated, got %d", len(out.Migrated))
	}
	m := out.Migrated[0]
	if m.ID != "ZZZ" {
		t.Errorf("migrated ID: %q", m.ID)
	}
	if m.FromVersion != 0 {
		t.Errorf("from_version: got %d want 0", m.FromVersion)
	}
	if m.ToVersion != domain.CurrentSchemaVersion {
		t.Errorf("to_version: got %d want %d", m.ToVersion, domain.CurrentSchemaVersion)
	}
	// Item in store should now be at current version.
	got, _ := store.Get(ctx, "ZZZ")
	if got.SchemaVersion != domain.CurrentSchemaVersion {
		t.Errorf("store item schema_version: got %d want %d", got.SchemaVersion, domain.CurrentSchemaVersion)
	}
}
