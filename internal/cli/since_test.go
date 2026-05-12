package cli

import (
	"errors"
	"testing"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

func TestParseSince_RFC3339(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	got, err := ParseSince("2026-04-15T08:30:00Z", now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 4, 15, 8, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSince_Duration(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
	}{
		{"5m", now.Add(-5 * time.Minute)},
		{"1h", now.Add(-time.Hour)},
		{"24h", now.Add(-24 * time.Hour)},
		{"30s", now.Add(-30 * time.Second)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParseSince(tc.in, now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestParseSince_Today(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 14, 30, 45, 123, time.UTC)
	got, err := ParseSince("today", now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := time.Date(2026, 5, 6, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSince_Yesterday(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 14, 30, 45, 123, time.UTC)
	got, err := ParseSince("yesterday", now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := time.Date(2026, 5, 5, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSince_NonUTCBoundary(t *testing.T) {
	t.Parallel()
	// `today` always anchors to UTC midnight regardless of the now timezone
	// — the audit log shards by UTC date, so this matches the file layout.
	loc, _ := time.LoadLocation("America/Los_Angeles")
	now := time.Date(2026, 5, 6, 18, 0, 0, 0, loc) // 2026-05-07 01:00 UTC
	got, err := ParseSince("today", now)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	want := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseSince_Invalid(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	cases := []string{
		"",
		"-5m",
		"5",
		"tomorrow",
		"five minutes ago",
		"2026-13-01T00:00:00Z",
		"abc",
	}
	for _, in := range cases {
		in := in
		t.Run(in, func(t *testing.T) {
			t.Parallel()
			_, err := ParseSince(in, now)
			if err == nil {
				t.Fatalf("expected error for %q", in)
			}
			if !errors.Is(err, domain.ErrUsage) {
				t.Fatalf("expected ErrUsage, got %v", err)
			}
		})
	}
}

func TestParseSince_EmptyReturnsZeroTime(t *testing.T) {
	t.Parallel()
	// Distinct from invalid: callers may pre-check `if since == ""` to mean
	// "no --since"; ParseSince must reject the empty literal explicitly so
	// downstream code never silently treats unset as "now".
	now := time.Date(2026, 5, 6, 12, 0, 0, 0, time.UTC)
	if _, err := ParseSince("", now); err == nil {
		t.Fatalf("empty input must be an explicit error")
	}
}
