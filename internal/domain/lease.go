package domain

import "time"

// LeaseSkewTolerance is the wall-clock allowance for cross-VM clock
// skew when checking lease expiry. A claim is treated as live until
// ExpiresAt + LeaseSkewTolerance has passed; this prevents an executor
// with a slightly-fast clock from reaping its peer's still-valid
// lease.
const LeaseSkewTolerance = 30 * time.Second

// IsLeaseLive reports whether the claim is still valid at now.
// A nil claim is never live; a claim with a zero ExpiresAt counts as
// live (defensive default — usecases set the field at issuance time,
// so a missing value means "no TTL configured").
func IsLeaseLive(c *Claim, now time.Time) bool {
	if c == nil {
		return false
	}
	if c.ExpiresAt.IsZero() {
		return true
	}
	return now.Before(c.ExpiresAt.Add(LeaseSkewTolerance))
}

// IsLeaseExpired is the negation of IsLeaseLive.
func IsLeaseExpired(c *Claim, now time.Time) bool {
	return !IsLeaseLive(c, now)
}

// LeaseMatches reports whether the supplied leaseID matches the claim
// on the item. nil claim or empty leaseID always returns false — the
// usecase is responsible for distinguishing "no claim" from "claim
// belongs to someone else" via separate sentinel translation.
func LeaseMatches(c *Claim, leaseID string) bool {
	if c == nil || leaseID == "" {
		return false
	}
	return c.LeaseID == leaseID
}
