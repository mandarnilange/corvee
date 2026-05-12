package testfakes

import (
	"context"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// Tailer is an in-memory fake of domain.EventTailer. Tests Send events
// and the active Tail consumer receives them; channel closes on ctx
// cancel or Close().
type Tailer struct {
	mu      sync.Mutex
	out     chan domain.Event
	pending []domain.Event
	closed  bool
}

// NewTailer returns an empty Tailer fake.
func NewTailer() *Tailer { return &Tailer{} }

// Tail returns the live channel. A second call before ctx cancellation
// of the first replaces the channel — tests should only call Tail once
// per fixture.
func (t *Tailer) Tail(ctx context.Context) (<-chan domain.Event, error) {
	t.mu.Lock()
	t.out = make(chan domain.Event, 64)
	pending := t.pending
	t.pending = nil
	out := t.out
	t.mu.Unlock()

	// One goroutine owns the channel write side: it drains buffered
	// events first (so callers pre-loading large bursts don't deadlock
	// on the channel cap), then waits for ctx cancellation before
	// closing. Splitting the drain and the close into separate
	// goroutines previously raced under the data-race detector when
	// cancellation interleaved with the final pending send.
	go func() {
		defer func() {
			t.mu.Lock()
			if !t.closed {
				t.closed = true
				close(out)
			}
			t.mu.Unlock()
		}()
		for _, ev := range pending {
			select {
			case <-ctx.Done():
				return
			case out <- ev:
			}
		}
		<-ctx.Done()
	}()
	return out, nil
}

// Send delivers ev to the active Tail consumer. If no Tail is active
// the event is buffered for the next Tail call.
func (t *Tailer) Send(ev domain.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.out != nil && !t.closed {
		t.out <- ev
		return
	}
	t.pending = append(t.pending, ev)
}

// Close terminates the active Tail (if any) by closing its channel.
func (t *Tailer) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.out != nil && !t.closed {
		t.closed = true
		close(t.out)
	}
}
