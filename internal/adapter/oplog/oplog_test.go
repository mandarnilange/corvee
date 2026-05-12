package oplog

import (
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestStore_GetMissingReturnsFalse(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := s.Get("op-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("missing op should report false")
	}
}

func TestStore_PutThenGetRoundTrips(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if putErr := s.Put("op-1", `{"item":"RKN-E01-S01"}`); putErr != nil {
		t.Fatal(putErr)
	}
	got, ok, err := s.Get("op-1")
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got != `{"item":"RKN-E01-S01"}` {
		t.Errorf("got %q", got)
	}
}

func TestStore_RejectsTraversalOpID(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.Get("../escape")
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
	if err := s.Put("../escape", "x"); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("Put want ErrUsage, got %v", err)
	}
}

// TestStore_PutTwice_RejectsSecondWriteWithSentinel is the
// post-review-finding-4 hardening: oplog uses link-style O_EXCL on
// the destination so caller misuse of an op-id (or a torn replay
// path) lands a loud ErrOpAlreadyRecorded instead of silently
// overwriting the prior record.
func TestStore_PutTwice_RejectsSecondWriteWithSentinel(t *testing.T) {
	t.Parallel()
	s, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if firstErr := s.Put("op-1", `first`); firstErr != nil {
		t.Fatal(firstErr)
	}
	err = s.Put("op-1", `second`)
	if !errors.Is(err, domain.ErrOpAlreadyRecorded) {
		t.Errorf("want ErrOpAlreadyRecorded, got %v", err)
	}
	got, ok, _ := s.Get("op-1")
	if !ok || got != "first" {
		t.Errorf("first record overwritten: got=%q ok=%v", got, ok)
	}
}
