package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestValidate_CleanWorkspace(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store,
		domain.Item{ID: "RKN", Type: domain.TypeProject},
		domain.Item{ID: "RKN-E01", Type: domain.TypeEpic, ParentID: "RKN"},
	)
	out, err := Validate(context.Background(), Deps{Store: store}, ValidateInput{})
	if err != nil || !out.OK {
		t.Errorf("clean validate: ok=%v err=%v issues=%+v", out.OK, err, out.Issues)
	}
}

func TestValidate_DetectsOrphan(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{ID: "RKN-E01-S01", Type: domain.TypeStory, ParentID: "ghost"})
	out, err := Validate(context.Background(), Deps{Store: store}, ValidateInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.OK || len(out.Issues) != 1 || out.Issues[0].Code != domain.IntegrityOrphan {
		t.Errorf("expected orphan issue, got %+v", out)
	}
}

func TestValidate_FailOnIssuesReturnsError(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	mustPut(t, store, domain.Item{ID: "A", Type: domain.TypeStory, Dependencies: []string{"missing"}})
	_, err := Validate(context.Background(), Deps{Store: store}, ValidateInput{FailOnIssues: true})
	if !errors.Is(err, domain.ErrIntegrityViolated) {
		t.Errorf("want ErrIntegrityViolated, got %v", err)
	}
}
