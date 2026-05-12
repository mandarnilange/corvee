package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestMerge_NoConflictsReturnsEmpty(t *testing.T) {
	t.Parallel()
	g := testfakes.NewGitClient()
	g.ConflictedFilesQueue = [][]string{nil}
	d := Deps{
		Clock:     testfakes.NewClock(time.Now()),
		IDGen:     testfakes.NewIDGen("ULID"),
		EventLog:  testfakes.NewEventLog(),
		GitClient: g,
	}
	out, err := Merge(context.Background(), d, MergeInput{})
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if out.Merged != 0 {
		t.Errorf("merged = %d, want 0", out.Merged)
	}
}

func TestMerge_NoGitClientReturnsErrUsage(t *testing.T) {
	t.Parallel()
	_, err := Merge(context.Background(), Deps{}, MergeInput{})
	if !errors.Is(err, domain.ErrUsage) {
		t.Errorf("err = %v, want ErrUsage", err)
	}
}
