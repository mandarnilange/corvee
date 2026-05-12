package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestRelease_HappyPath_ResetsToReady(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Release(context.Background(), d, ReleaseInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, Agent: "alice", ResetToReady: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.Claim != nil {
		t.Errorf("claim not cleared: %+v", out.Item.Claim)
	}
	if out.Item.Status != domain.StatusReady {
		t.Errorf("status = %q, want ready", out.Item.Status)
	}
}

func TestRelease_WrongLease_Rejected(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"}); err != nil {
		t.Fatal(err)
	}
	_, err := Release(context.Background(), d, ReleaseInput{
		ID: "RKN-E01-S01", LeaseID: "wrong", Agent: "bob",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("want ErrLeaseMismatch, got %v", err)
	}
}

func TestRelease_NoClaim_Rejected(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	_, err := Release(context.Background(), d, ReleaseInput{
		ID: "RKN-E01-S01", LeaseID: "x",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("want ErrLeaseMismatch, got %v", err)
	}
}

func TestRelease_NotResetGoesBlocked(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, _ := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	out, err := Release(context.Background(), d, ReleaseInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, ResetToReady: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.Status != domain.StatusBlocked {
		t.Errorf("status = %q, want blocked", out.Item.Status)
	}
}
