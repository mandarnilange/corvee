package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestList_DelegatesToStore(t *testing.T) {
	t.Parallel()
	store := testfakes.NewStore()
	clock := testfakes.NewClock(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))
	d := usecase.Deps{Store: store, Clock: clock}

	for _, code := range []string{"AAA", "BBB"} {
		if _, err := usecase.Add(context.Background(), d, usecase.AddInput{
			Type:        domain.TypeProject,
			ProjectCode: code,
			Title:       code,
		}); err != nil {
			t.Fatal(err)
		}
	}

	out, err := usecase.List(context.Background(), d, usecase.ListInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("len=%d, want 2", len(out.Items))
	}

	out, err = usecase.List(context.Background(), d, usecase.ListInput{
		Filter: domain.ListFilter{Statuses: []domain.Status{domain.StatusReady}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 0 {
		t.Errorf("filter mismatch: %+v", out.Items)
	}
}
