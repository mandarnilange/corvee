package domain

import (
	"testing"
	"time"
)

func TestIsLeaseLive_NilClaim_NotLive(t *testing.T) {
	t.Parallel()
	if IsLeaseLive(nil, time.Now()) {
		t.Error("nil claim should not be live")
	}
}

func TestIsLeaseLive_ZeroExpiry_IsLive(t *testing.T) {
	t.Parallel()
	c := &Claim{Agent: "alice"}
	if !IsLeaseLive(c, time.Now()) {
		t.Error("claim without expiry should default to live")
	}
}

func TestIsLeaseLive_BeforeExpiry_IsLive(t *testing.T) {
	t.Parallel()
	now := time.Now()
	c := &Claim{Agent: "alice", ExpiresAt: now.Add(time.Hour)}
	if !IsLeaseLive(c, now) {
		t.Error("claim with future expiry should be live")
	}
}

func TestIsLeaseLive_PastExpiryWithinSkew_IsStillLive(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	now := expires.Add(LeaseSkewTolerance / 2)
	c := &Claim{Agent: "alice", ExpiresAt: expires}
	if !IsLeaseLive(c, now) {
		t.Error("claim within skew tolerance should still be live")
	}
}

func TestIsLeaseLive_BeyondSkew_IsExpired(t *testing.T) {
	t.Parallel()
	expires := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	now := expires.Add(LeaseSkewTolerance + time.Second)
	c := &Claim{Agent: "alice", ExpiresAt: expires}
	if IsLeaseLive(c, now) {
		t.Error("claim beyond skew tolerance should be expired")
	}
	if !IsLeaseExpired(c, now) {
		t.Error("IsLeaseExpired should be the inverse")
	}
}

func TestLeaseMatches_NilClaim_NoMatch(t *testing.T) {
	t.Parallel()
	if LeaseMatches(nil, "id") {
		t.Error("nil claim never matches")
	}
}

func TestLeaseMatches_EmptyLeaseID_NoMatch(t *testing.T) {
	t.Parallel()
	if LeaseMatches(&Claim{LeaseID: "x"}, "") {
		t.Error("empty leaseID never matches")
	}
}

func TestLeaseMatches_ExactMatch(t *testing.T) {
	t.Parallel()
	if !LeaseMatches(&Claim{LeaseID: "abc"}, "abc") {
		t.Error("matching leaseID should match")
	}
	if LeaseMatches(&Claim{LeaseID: "abc"}, "xyz") {
		t.Error("differing leaseID should not match")
	}
}
