package usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase/testfakes"
)

func TestHeartbeat_ExtendsExpiry(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	originalExpiry := c.Item.Claim.ExpiresAt
	d.Clock.(*testfakes.Clock).Advance(30 * time.Minute)
	out, err := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, Agent: "alice", TTL: time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !out.ExpiresAt.After(originalExpiry) {
		t.Errorf("expiry not extended: %v -> %v", originalExpiry, out.ExpiresAt)
	}
}

func TestHeartbeat_AdvancesToInProgress(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	c, _ := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"})
	out, err := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: c.LeaseID, Agent: "alice", AdvanceToInProgress: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Item.Status != domain.StatusInProgress {
		t.Errorf("status = %q, want in_progress", out.Item.Status)
	}
}

func TestHeartbeat_LeaseMismatch(t *testing.T) {
	t.Parallel()
	d := mkClaimDeps(t)
	if _, err := Claim(context.Background(), d, ClaimInput{ID: "RKN-E01-S01", Agent: "alice"}); err != nil {
		t.Fatal(err)
	}
	_, err := Heartbeat(context.Background(), d, HeartbeatInput{
		ID: "RKN-E01-S01", LeaseID: "wrong", Agent: "alice",
	})
	if !errors.Is(err, domain.ErrLeaseMismatch) {
		t.Errorf("want ErrLeaseMismatch, got %v", err)
	}
}
