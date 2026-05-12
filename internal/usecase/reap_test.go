package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestReap_ClearsExpiredClaims(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, _ := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice", TTL: time.Minute})
	_ = c
	d.Clock.(*testfakes.Clock).Advance(time.Hour)
	out, err := Reap(context.Background(), d, ReapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Reaped) != 1 || out.Reaped[0].ItemID != "RKN-E01-S01" {
		t.Errorf("reaped: %+v", out.Reaped)
	}
	item, _ := d.Store.Get(context.Background(), "RKN-E01-S01")
	if item.Claim != nil {
		t.Errorf("claim still present after reap: %+v", item.Claim)
	}
	if item.Status != domain.StatusReady {
		t.Errorf("status = %q, want ready", item.Status)
	}
}

func TestReap_LeavesLiveClaimsAlone(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	out, err := Reap(context.Background(), d, ReapInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Reaped) != 0 {
		t.Errorf("should reap nothing, got %+v", out.Reaped)
	}
}
