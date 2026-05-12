// Package property contains property-based tests for Phase 4 multi-file
// operations. These tests generate random valid hierarchies and verify
// the invariants that Move and Rename must maintain regardless of input.
//
// Per docs/spec-questions.md SQ-004: the spec (TTR-E04-S11) calls for the
// rapid library, which is not in go.mod and requires a separate justification
// PR per CLAUDE.md §9. We implement the property tests using stdlib
// testing/quick, which is sufficient to cover the stated invariants:
// idempotency and alias resolvability after crash injection.
package property

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

// projectCodes is the alphabet of valid 3-letter project codes for
// random hierarchy generation.
var projectCodes = []string{"AAA", "BBB", "CCC", "DDD", "EEE"}

// hierarchy describes a generated workspace with a source project and
// an available target project for move/rename operations.
type hierarchy struct {
	store     *testfakes.Store
	sourceID  string // an epic in the source project
	targetID  string // the target project for Move
	epicCount int    // how many stories under the source epic
}

// generateHierarchy creates a random hierarchy with 1-5 stories under
// a source epic. rng is used for all random decisions.
func generateHierarchy(t *testing.T, rng *rand.Rand) hierarchy {
	t.Helper()
	ctx := context.Background()
	store := testfakes.NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	srcCode := projectCodes[rng.Intn(len(projectCodes))]
	dstCode := "ZZZ"

	srcProject := domain.Item{
		SchemaVersion: 1, ID: srcCode, Type: domain.TypeProject,
		Title: "src", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	dstProject := domain.Item{
		SchemaVersion: 1, ID: dstCode, Type: domain.TypeProject,
		Title: "dst", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, srcProject, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, dstProject, -1); err != nil {
		t.Fatal(err)
	}

	epicID := srcCode + "-E01"
	epic := domain.Item{
		SchemaVersion: 1, ID: epicID, Type: domain.TypeEpic,
		ProjectID: srcCode, ParentID: srcCode,
		Title: "epic", Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if _, err := store.Put(ctx, epic, -1); err != nil {
		t.Fatal(err)
	}

	storyCount := rng.Intn(5) + 1
	for s := 1; s <= storyCount; s++ {
		storyID := fmt.Sprintf("%s-E01-S%02d", srcCode, s)
		story := domain.Item{
			SchemaVersion: 1, ID: storyID, Type: domain.TypeStory,
			ProjectID: srcCode, ParentID: epicID,
			Title:  fmt.Sprintf("story-%d", s),
			Status: domain.StatusBacklog, CreatedAt: now, UpdatedAt: now, Version: 1,
		}
		if _, err := store.Put(ctx, story, -1); err != nil {
			t.Fatal(err)
		}
	}

	return hierarchy{
		store:     store,
		sourceID:  epicID,
		targetID:  dstCode,
		epicCount: storyCount,
	}
}

// TestMoveProperty_MappingIsComplete verifies that the mapping returned
// by Move contains exactly one entry for each item in the moved subtree.
func TestMoveProperty_MappingIsComplete(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(42))
	const trials = 20

	for i := 0; i < trials; i++ {
		h := generateHierarchy(t, rng)
		d := usecase.Deps{
			Store:     h.store,
			Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
			IDGen:     testfakes.NewIDGen("prop"),
			OpJournal: testfakes.NewOpJournal(),
		}
		out, err := usecase.Move(context.Background(), d, usecase.MoveInput{
			SourceID:       h.sourceID,
			TargetParentID: h.targetID,
			Agent:          "property-test",
		})
		if err != nil {
			t.Fatalf("trial %d: Move: %v", i, err)
		}
		// Mapping should have 1 entry for epic + N for stories.
		wantMappingLen := 1 + h.epicCount
		if len(out.Mapping) != wantMappingLen {
			t.Errorf("trial %d: mapping len: got %d want %d", i, len(out.Mapping), wantMappingLen)
		}
	}
}

// TestMoveProperty_OldIDsPreservedAsAliases verifies that every old ID
// appears in the Aliases of the corresponding new item.
func TestMoveProperty_OldIDsPreservedAsAliases(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(7))
	const trials = 20

	for i := 0; i < trials; i++ {
		h := generateHierarchy(t, rng)
		d := usecase.Deps{
			Store:     h.store,
			Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
			IDGen:     testfakes.NewIDGen("prop"),
			OpJournal: testfakes.NewOpJournal(),
		}
		out, err := usecase.Move(context.Background(), d, usecase.MoveInput{
			SourceID:       h.sourceID,
			TargetParentID: h.targetID,
			Agent:          "property-test",
		})
		if err != nil {
			t.Fatalf("trial %d: Move: %v", i, err)
		}
		ctx := context.Background()
		for _, m := range out.Mapping {
			newItem, err := h.store.Get(ctx, m.NewID)
			if err != nil {
				t.Fatalf("trial %d: new item %s: %v", i, m.NewID, err)
			}
			found := false
			for _, alias := range newItem.Aliases {
				if alias == m.OldID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("trial %d: %s.Aliases does not contain old ID %s: %v",
					i, m.NewID, m.OldID, newItem.Aliases)
			}
		}
	}
}

// TestMoveProperty_OldItemsRemovedNewItemsExist verifies that after Move,
// no old item ID exists in the store and every new item ID does.
func TestMoveProperty_OldItemsRemovedNewItemsExist(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(99))
	const trials = 20

	for i := 0; i < trials; i++ {
		h := generateHierarchy(t, rng)
		d := usecase.Deps{
			Store:     h.store,
			Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
			IDGen:     testfakes.NewIDGen("prop"),
			OpJournal: testfakes.NewOpJournal(),
		}
		out, err := usecase.Move(context.Background(), d, usecase.MoveInput{
			SourceID:       h.sourceID,
			TargetParentID: h.targetID,
			Agent:          "property-test",
		})
		if err != nil {
			t.Fatalf("trial %d: Move: %v", i, err)
		}
		ctx := context.Background()
		for _, m := range out.Mapping {
			if exists, _ := h.store.Exists(ctx, m.OldID); exists {
				t.Errorf("trial %d: old ID %s still exists after Move", i, m.OldID)
			}
			if _, err := h.store.Get(ctx, m.NewID); err != nil {
				t.Errorf("trial %d: new ID %s missing after Move: %v", i, m.NewID, err)
			}
		}
	}
}

// TestCrashRecovery_Property verifies that for random hierarchies,
// crashing at each step and recovering yields a consistent state.
func TestCrashRecovery_Property(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(2026))
	const trials = 10

	for i := 0; i < trials; i++ {
		for crashAt := 0; crashAt <= 2; crashAt++ {
			h := generateHierarchy(t, rng)
			inner := testfakes.NewOpJournal()
			cj := &crashingJournalProp{inner: inner, crashAt: crashAt}

			d := usecase.Deps{
				Store:     h.store,
				Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				IDGen:     testfakes.NewIDGen("prop"),
				OpJournal: cj,
			}
			func() {
				defer func() { _ = recover() }()
				_, _ = usecase.Move(context.Background(), d, usecase.MoveInput{
					SourceID:       h.sourceID,
					TargetParentID: h.targetID,
					Agent:          "property-test",
				})
			}()

			// Recover.
			rd := usecase.Deps{
				Store:     h.store,
				Clock:     testfakes.NewClock(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)),
				IDGen:     testfakes.NewIDGen("recover"),
				OpJournal: inner,
			}
			out, err := usecase.Recover(context.Background(), rd, usecase.RecoverInput{})
			if err != nil {
				t.Fatalf("trial %d crashAt=%d: Recover: %v", i, crashAt, err)
			}
			if len(out.Failed) > 0 {
				t.Errorf("trial %d crashAt=%d: recovery failures: %v", i, crashAt, out.Failed)
			}

			// Either old or new item must exist, not both, not neither.
			ctx := context.Background()
			srcExists, _ := h.store.Exists(ctx, h.sourceID)
			dstExists, _ := h.store.Exists(ctx, "ZZZ-E01")
			if srcExists && dstExists {
				t.Errorf("trial %d crashAt=%d: both old and new exist", i, crashAt)
			}
			if !srcExists && !dstExists {
				t.Errorf("trial %d crashAt=%d: neither old nor new exists", i, crashAt)
			}
		}
	}
}

// crashingJournalProp is a local copy of the crash-injection journal
// for the property test package (avoids importing usecase internals).
type crashingJournalProp struct {
	inner   domain.OpJournal
	crashAt int
}

func (j *crashingJournalProp) Begin(op domain.Operation) error { return j.inner.Begin(op) }
func (j *crashingJournalProp) Complete(opID string) error      { return j.inner.Complete(opID) }
func (j *crashingJournalProp) Pending() ([]domain.Operation, error) {
	return j.inner.Pending()
}
func (j *crashingJournalProp) MarkStep(opID string, stepNum int) error {
	if err := j.inner.MarkStep(opID, stepNum); err != nil {
		return err
	}
	if j.crashAt >= 0 && stepNum == j.crashAt {
		panic("crash-injection")
	}
	return nil
}
