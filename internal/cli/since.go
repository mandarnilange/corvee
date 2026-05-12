package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ParseSince parses the --since flag accepted by `corvee watch` and
// `corvee audit`. It supports four forms (per spec §15.2):
//
//   - ISO timestamp (RFC 3339): "2026-04-15T08:30:00Z"
//   - positive Go duration: "5m", "1h", "24h", "30s" — interpreted as
//     "now minus the duration"
//   - the literal "today" — UTC-midnight of the current calendar day
//   - the literal "yesterday" — UTC-midnight of the previous day
//
// `now` is the reference instant for relative forms; production callers
// pass time.Now().UTC() (or the injected Clock's value). The "today" /
// "yesterday" literals always anchor to UTC because the audit log
// shards by UTC date.
//
// Returns ErrUsage on empty input, negative durations, unknown literals,
// or any other unparseable form.
func ParseSince(s string, now time.Time) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("--since: empty value: %w", domain.ErrUsage)
	}

	switch strings.ToLower(s) {
	case "today":
		return utcMidnight(now, 0), nil
	case "yesterday":
		return utcMidnight(now, -1), nil
	}

	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}

	d, err := time.ParseDuration(s)
	if err == nil {
		if d < 0 {
			return time.Time{}, fmt.Errorf("--since=%q: negative duration not allowed: %w", s, domain.ErrUsage)
		}
		return now.Add(-d).UTC(), nil
	}

	return time.Time{}, fmt.Errorf("--since=%q: not an RFC3339 timestamp, duration, or 'today'/'yesterday': %w", s, domain.ErrUsage)
}

func utcMidnight(now time.Time, dayOffset int) time.Time {
	u := now.UTC()
	return time.Date(u.Year(), u.Month(), u.Day()+dayOffset, 0, 0, 0, 0, time.UTC)
}
