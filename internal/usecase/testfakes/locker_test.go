package testfakes

import (
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestLocker_AcquireRelease(t *testing.T) {
	t.Parallel()
	l := NewLocker()
	h, err := l.Acquire("X", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Release(); err != nil {
		t.Errorf("release: %v", err)
	}
}

func TestLocker_DoubleReleaseRejected(t *testing.T) {
	t.Parallel()
	l := NewLocker()
	h, _ := l.Acquire("X", 0)
	_ = h.Release()
	if err := h.Release(); !errors.Is(err, domain.ErrUsage) {
		t.Errorf("want ErrUsage, got %v", err)
	}
}

func TestLocker_TimesOutWhenHeld(t *testing.T) {
	t.Parallel()
	l := NewLocker()
	h, _ := l.Acquire("X", 0)
	defer func() { _ = h.Release() }()
	_, err := l.Acquire("X", 5*time.Millisecond)
	if !errors.Is(err, domain.ErrLockTimeout) {
		t.Errorf("want timeout, got %v", err)
	}
}

func TestLocker_AcquireWithBudgetSucceedsAfterRelease(t *testing.T) {
	t.Parallel()
	l := NewLocker()
	h, _ := l.Acquire("Y", 0)
	go func() {
		time.Sleep(5 * time.Millisecond)
		_ = h.Release()
	}()
	h2, err := l.Acquire("Y", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = h2.Release()
}
