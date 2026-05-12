package testfakes

import (
	"testing"
	"time"
)

func TestClock_NowReturnsPinnedTime(t *testing.T) {
	t.Parallel()
	pin := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	c := NewClock(pin)
	if !c.Now().Equal(pin) {
		t.Errorf("Now=%v, want %v", c.Now(), pin)
	}
}

func TestClock_SetReplaces(t *testing.T) {
	t.Parallel()
	c := NewClock(time.Now())
	pin := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Set(pin)
	if !c.Now().Equal(pin) {
		t.Errorf("after Set: Now=%v", c.Now())
	}
}

func TestClock_AdvanceAndStep(t *testing.T) {
	t.Parallel()
	c := NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	c.Advance(time.Hour)
	want := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	if !c.Now().Equal(want) {
		t.Errorf("after Advance: Now=%v, want %v", c.Now(), want)
	}

	c.SetStep(time.Minute)
	first := c.Now()
	second := c.Now()
	if !second.Equal(first.Add(time.Minute)) {
		t.Errorf("step: first=%v second=%v", first, second)
	}
}
