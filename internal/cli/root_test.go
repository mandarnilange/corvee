package cli

import (
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/mandarnilange/corvee/internal/domain"
)

// TestExitCodeFor_MapsDomainSentinels asserts the §15.1 exit-code table.
// One regression here would silently break agent error handling, so the
// table is exhaustive.
func TestExitCodeFor_MapsDomainSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil yields 0", nil, 0},
		{"usage error yields 2", domain.ErrUsage, 2},
		{"not found yields 3", domain.ErrNotFound, 3},
		{"version conflict yields 4", domain.ErrVersionConflict, 4},
		{"already claimed yields 5", domain.ErrAlreadyClaimed, 5},
		{"lease mismatch yields 5", domain.ErrLeaseMismatch, 5},
		{"claim lost yields 5", domain.ErrClaimLost, 5},
		{"op already recorded yields 5", domain.ErrOpAlreadyRecorded, 5},
		{"schema invalid yields 6", domain.ErrSchemaInvalid, 6},
		{"integrity violation yields 7", domain.ErrIntegrityViolated, 7},
		{"lock timeout yields 8", domain.ErrLockTimeout, 8},
		{"pending operation yields 9", domain.ErrPendingOperation, 9},
		{"wrapped sentinel still maps", fmt.Errorf("ctx: %w", domain.ErrNotFound), 3},
		{"unknown error yields 1", errors.New("something else"), 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeFor(tc.err, io.Discard); got != tc.want {
				t.Errorf("exitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

// TestExitCodeFor_DetectsCobraUsageErrors asserts that cobra's stringly-typed
// errors map to exit 2. If cobra ever exposes typed sentinels we should
// switch to errors.Is — until then, prefix matching is the contract.
func TestExitCodeFor_DetectsCobraUsageErrors(t *testing.T) {
	t.Parallel()

	cases := []string{
		"unknown flag: --bogus",
		"unknown command \"frob\" for \"corvee\"",
		"requires at least 1 arg(s), only received 0",
		"accepts 1 arg(s), received 2",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			t.Parallel()
			if got := exitCodeFor(errors.New(msg), io.Discard); got != 2 {
				t.Errorf("exitCodeFor(%q) = %d, want 2", msg, got)
			}
		})
	}
}

// TestExitCodeFor_PrintsErrorToStderr ensures diagnostic text reaches stderr.
func TestExitCodeFor_PrintsErrorToStderr(t *testing.T) {
	t.Parallel()

	var stderr stringSink
	_ = exitCodeFor(domain.ErrNotFound, &stderr)
	if !contains(stderr.String(), domain.ErrNotFound.Error()) {
		t.Errorf("stderr=%q, want it to contain %q", stderr.String(), domain.ErrNotFound.Error())
	}
}

type stringSink struct{ buf []byte }

func (s *stringSink) Write(p []byte) (int, error) { s.buf = append(s.buf, p...); return len(p), nil }
func (s *stringSink) String() string              { return string(s.buf) }
