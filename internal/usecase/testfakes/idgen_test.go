package testfakes

import "testing"

func TestIDGen_SequencesMonotonically(t *testing.T) {
	t.Parallel()
	g := NewIDGen("ULID")
	first, second, third := g.ULID(), g.ULID(), g.ULID()
	if first != "ULID-1" || second != "ULID-2" || third != "ULID-3" {
		t.Errorf("got %q %q %q", first, second, third)
	}
}
