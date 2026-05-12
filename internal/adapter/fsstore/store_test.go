package fsstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func mkItem(id string) domain.Item {
	t := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	return domain.Item{
		SchemaVersion: 1,
		ID:            id,
		Type:          domain.TypeProject,
		Title:         id + " title",
		Status:        domain.StatusBacklog,
		CreatedAt:     t,
		UpdatedAt:     t,
		Version:       1,
	}
}

func newStoreT(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s, dir
}

func TestFsStore_New_AutoCreatesMissingDir(t *testing.T) {
	t.Parallel()
	// Phase 7 multi-VM clones may not have items/ yet; New now
	// mkdirs it instead of erroring.
	dir := filepath.Join(t.TempDir(), "fresh")
	s, err := New(dir)
	if err != nil {
		t.Fatalf("New(missing): %v", err)
	}
	if s == nil {
		t.Fatal("expected non-nil store")
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Errorf("New did not create dir: %v", statErr)
	}
}

func TestFsStore_New_RejectsFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "regular-file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(f)
	if err == nil {
		t.Fatal("New(file) returned nil err")
	}
}

func TestFsStore_PutGet_RoundTrip(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	in := mkItem("RKN")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(context.Background(), "RKN")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "RKN" || got.Title != in.Title || got.Version != in.Version {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestFsStore_Get_MissingReturnsErrNotFound(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	_, err := s.Get(context.Background(), "RKN")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err=%v, want ErrNotFound", err)
	}
}

func TestFsStore_NoTmpFilesAfterPut(t *testing.T) {
	t.Parallel()
	s, dir := newStoreT(t)
	if _, err := s.Put(context.Background(), mkItem("RKN"), -1); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("leftover tmp file: %s", e.Name())
		}
	}
}

// TestFsStore_TmpFilesAreInvisible simulates a crash mid-Put: a stray
// <id>.json.tmp.<pid>.<ts> file remains, but Get/List must not surface
// it as a real item.
func TestFsStore_TmpFilesAreInvisible(t *testing.T) {
	t.Parallel()
	s, dir := newStoreT(t)
	tmp := filepath.Join(dir, "RKN.json.tmp.99999.123456789")
	if err := os.WriteFile(tmp, []byte(`{"id":"RKN","schema_version":1,"type":"project","title":"x","status":"backlog","created_at":"2026-05-01T00:00:00Z","updated_at":"2026-05-01T00:00:00Z","version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// Get sees nothing — RKN.json doesn't exist.
	_, err := s.Get(context.Background(), "RKN")
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get found tmp file: %v", err)
	}

	// List sees nothing — tmp suffix is filtered.
	items, err := s.List(context.Background(), domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("List returned tmp file as item: %v", items)
	}
}

func TestFsStore_PathTraversalRejected(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	bogus := []string{
		"../../etc/passwd",
		"..",
		"foo/bar",
		"foo bar",
		"lowercase",
		"",
	}
	for _, id := range bogus {
		id := id
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			_, err := s.Get(context.Background(), id)
			if !errors.Is(err, domain.ErrUsage) {
				t.Errorf("Get(%q): err=%v, want ErrUsage", id, err)
			}
			_, err = s.Put(context.Background(), mkItem(id), -1)
			if !errors.Is(err, domain.ErrUsage) {
				t.Errorf("Put(%q): err=%v, want ErrUsage", id, err)
			}
		})
	}
}

func TestFsStore_Delete(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	if err := s.Delete(context.Background(), "RKN"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Delete missing: err=%v, want ErrNotFound", err)
	}
	if _, err := s.Put(context.Background(), mkItem("RKN"), -1); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(context.Background(), "RKN"); err != nil {
		t.Fatalf("Delete existing: %v", err)
	}
	if exists, _ := s.Exists(context.Background(), "RKN"); exists {
		t.Errorf("Exists returned true after Delete")
	}
}

func TestFsStore_Exists(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	if exists, err := s.Exists(context.Background(), "RKN"); err != nil || exists {
		t.Errorf("Exists(missing) = (%v, %v); want (false, nil)", exists, err)
	}
	if _, err := s.Put(context.Background(), mkItem("RKN"), -1); err != nil {
		t.Fatal(err)
	}
	if exists, err := s.Exists(context.Background(), "RKN"); err != nil || !exists {
		t.Errorf("Exists(present) = (%v, %v); want (true, nil)", exists, err)
	}
}

func TestFsStore_List_ReturnsAllValidItems(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	for _, id := range []string{"AAA", "BBB", "CCC"} {
		if _, err := s.Put(context.Background(), mkItem(id), -1); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.List(context.Background(), domain.ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d, want 3", len(got))
	}
}

func TestFsStore_List_AppliesFilter(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	a := mkItem("AAA")
	a.Status = domain.StatusReady
	b := mkItem("BBB")
	b.Status = domain.StatusDone
	if _, err := s.Put(context.Background(), a, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(context.Background(), b, -1); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(context.Background(), domain.ListFilter{
		Statuses: []domain.Status{domain.StatusReady},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "AAA" {
		t.Fatalf("filter mismatch: %v", got)
	}
}

func TestFsStore_List_SkipsNonJSONAndCorrupt(t *testing.T) {
	t.Parallel()
	s, dir := newStoreT(t)
	// Drop a non-json file
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Drop a corrupt json
	if err := os.WriteFile(filepath.Join(dir, "BBB.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Drop a json file with an id that doesn't parse (e.g., lowercase)
	if err := os.WriteFile(filepath.Join(dir, "junk.json"), []byte(`{"id":"junk"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// And a valid item
	if _, err := s.Put(context.Background(), mkItem("AAA"), -1); err != nil {
		t.Fatal(err)
	}
	got, err := s.List(context.Background(), domain.ListFilter{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].ID != "AAA" {
		t.Fatalf("got=%v, want only AAA", got)
	}
}

func TestFsStore_Put_ExpectVersionMatchesSucceeds(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	in := mkItem("RKN")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatal(err)
	}
	updated := in
	updated.Version = 2
	updated.Title = "updated"
	if _, err := s.Put(context.Background(), updated, 1); err != nil {
		t.Fatalf("Put with matching expectVersion: %v", err)
	}
	got, _ := s.Get(context.Background(), "RKN")
	if got.Version != 2 || got.Title != "updated" {
		t.Fatalf("update did not land: %+v", got)
	}
}

func TestFsStore_Put_ExpectVersionMismatchReturnsErrVersionConflict(t *testing.T) {
	t.Parallel()
	s, dir := newStoreT(t)
	in := mkItem("RKN")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatal(err)
	}
	stale := in
	stale.Title = "should not land"
	stale.Version = 99
	_, err := s.Put(context.Background(), stale, 99) // disk has version 1
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("err=%v, want ErrVersionConflict", err)
	}

	// On-disk file unchanged.
	got, _ := s.Get(context.Background(), "RKN")
	if got.Title == "should not land" {
		t.Errorf("disk was modified despite conflict: %+v", got)
	}

	// No leftover tmp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp.") {
			t.Errorf("tmp leftover after conflict: %s", e.Name())
		}
	}
}

func TestFsStore_Put_ExpectVersionAgainstMissingItemConflicts(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	_, err := s.Put(context.Background(), mkItem("RKN"), 0) // expects version 0; no file yet
	if !errors.Is(err, domain.ErrVersionConflict) {
		t.Fatalf("err=%v, want ErrVersionConflict (no on-disk version to honor)", err)
	}
	if exists, _ := s.Exists(context.Background(), "RKN"); exists {
		t.Errorf("item created despite conflict")
	}
}

func TestFsStore_Put_NoExpectationIgnoresExistingVersion(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	in := mkItem("RKN")
	if _, err := s.Put(context.Background(), in, -1); err != nil {
		t.Fatal(err)
	}
	overwrite := in
	overwrite.Title = "overwritten"
	overwrite.Version = 7
	// expectVersion = -1: blind overwrite, no conflict check.
	if _, err := s.Put(context.Background(), overwrite, -1); err != nil {
		t.Fatalf("Put with -1: %v", err)
	}
	got, _ := s.Get(context.Background(), "RKN")
	if got.Title != "overwritten" || got.Version != 7 {
		t.Fatalf("blind overwrite did not land: %+v", got)
	}
}

func TestFsStore_Put_DoesNotMutateVersionOrUpdatedAt(t *testing.T) {
	t.Parallel()
	s, _ := newStoreT(t)
	stamp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	in := mkItem("RKN")
	in.Version = 42
	in.UpdatedAt = stamp

	out, err := s.Put(context.Background(), in, -1)
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != 42 || !out.UpdatedAt.Equal(stamp) {
		t.Errorf("returned mutated: Version=%d UpdatedAt=%v", out.Version, out.UpdatedAt)
	}
	got, _ := s.Get(context.Background(), "RKN")
	if got.Version != 42 || !got.UpdatedAt.Equal(stamp) {
		t.Errorf("disk mutated: Version=%d UpdatedAt=%v", got.Version, got.UpdatedAt)
	}
}

func TestFsStore_Get_RejectsCorruptJSON(t *testing.T) {
	t.Parallel()
	s, dir := newStoreT(t)
	if err := os.WriteFile(filepath.Join(dir, "RKN.json"), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Get(context.Background(), "RKN")
	if !errors.Is(err, domain.ErrMalformedJSON) {
		t.Fatalf("err=%v, want ErrMalformedJSON", err)
	}
}
