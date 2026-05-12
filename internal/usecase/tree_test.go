package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func mustPut(t *testing.T, store *testfakes.Store, items ...domain.Item) {
	t.Helper()
	for _, it := range items {
		if _, err := store.Put(context.Background(), it, -1); err != nil {
			t.Fatalf("put %s: %v", it.ID, err)
		}
	}
}

func TestTree_AllProjectsAsRoots(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, Title: "rkn"},
		domain.Item{ID: "AAA", Type: domain.TypeProject, Title: "aaa"},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN", Title: "epic"},
	)
	out, err := Tree(context.Background(), Deps{Store: store}, TreeInput{})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if len(out.Roots) != 2 {
		t.Fatalf("want 2 roots, got %d: %+v", len(out.Roots), out.Roots)
	}
	if out.Roots[0].Item.ID != "AAA" || out.Roots[1].Item.ID != "RKN" {
		t.Errorf("roots not sorted: %+v", out.Roots)
	}
	if len(out.Roots[1].Children) != 1 || out.Roots[1].Children[0].Item.ID != "RKN-E01" {
		t.Errorf("RKN children: %+v", out.Roots[1].Children)
	}
}

func TestTree_SubtreeFromRoot(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject, Title: "rkn"},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01"},
	)
	out, err := Tree(context.Background(), Deps{Store: store}, TreeInput{Root: "RKN-E01"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Roots) != 1 || out.Roots[0].Item.ID != "RKN-E01" {
		t.Fatalf("subtree: %+v", out.Roots)
	}
	if len(out.Roots[0].Children) != 1 || out.Roots[0].Children[0].Item.ID != "RKN-E01-S01" {
		t.Errorf("child: %+v", out.Roots[0].Children)
	}
}

func TestTree_MissingRoot_NotFound(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	_, err := Tree(context.Background(), Deps{Store: store}, TreeInput{Root: "ghost"})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("want ErrNotFound, got %v", err)
	}
}

func TestTree_RespectsMaxDepth(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN"},
		domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "RKN-E01"},
	)
	out, err := Tree(context.Background(), Deps{Store: store}, TreeInput{Root: "RKN", MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Roots[0].Children) != 0 {
		t.Errorf("max_depth=1 should hide children: %+v", out.Roots[0].Children)
	}
}
