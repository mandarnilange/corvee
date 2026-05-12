package testfakes

import (
	"sync"
	"time"
)

// Clock is a deterministic Clock fake. Now() returns the current
// pinned time; each call advances by Step (zero by default — the
// clock is frozen until Set or Advance is called).
type Clock struct {
	mu   sync.Mutex
	now  time.Time
	step time.Duration
}

// NewClock returns a Clock pinned at t.
func NewClock(t time.Time) *Clock {
	return &Clock{now: t}
}

// Now returns the pinned time, then advances by Step (zero-step
// clocks return the same time on every call).
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := c.now
	c.now = c.now.Add(c.step)
	return t
}

// Set replaces the pinned time.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Advance moves the pinned time forward by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// SetStep configures auto-advance per Now() call.
func (c *Clock) SetStep(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.step = d
}
