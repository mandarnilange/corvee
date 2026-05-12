package testfakes

import (
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestOpResults_GetMissingReturnsFalse(t *testing.T) {
	t.Parallel()
	o := NewOpResults()
	_, ok, err := o.Get("op-1")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Error("missing op should not be present")
	}
}

func TestOpResults_PutThenGetRoundTrips(t *testing.T) {
	t.Parallel()
	o := NewOpResults()
	if err := o.Put("op-1", "result"); err != nil {
		t.Fatal(err)
	}
	got, ok, _ := o.Get("op-1")
	if !ok || got != "result" {
		t.Errorf("got=%q ok=%v", got, ok)
	}
}

func TestOpResults_DuplicatePutRejected(t *testing.T) {
	t.Parallel()
	o := NewOpResults()
	if err := o.Put("op-1", "first"); err != nil {
		t.Fatal(err)
	}
	err := o.Put("op-1", "second")
	if !errors.Is(err, domain.ErrOpAlreadyRecorded) {
		t.Errorf("want ErrOpAlreadyRecorded, got %v", err)
	}
	got, _, _ := o.Get("op-1")
	if got != "first" {
		t.Errorf("first record overwritten: got %q", got)
	}
}
