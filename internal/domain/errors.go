package domain

import "errors"

// Sentinel errors as specified in §14.3 of the spec. CLI maps each to a
// stable exit code in cli/root.go so agents can branch on numeric codes
// without parsing JSON. Usecase functions wrap these via fmt.Errorf("%w").
var (
	// ErrNotFound is returned when an item or workspace cannot be located.
	// CLI exit code: 3.
	ErrNotFound = errors.New("not found")

	// ErrVersionConflict is returned when an optimistic-concurrency write
	// finds the on-disk version differs from the expected version.
	// CLI exit code: 4.
	ErrVersionConflict = errors.New("version conflict")

	// ErrAlreadyClaimed is returned when claim is attempted on an item
	// that is currently claimed by another agent.
	// CLI exit code: 5.
	ErrAlreadyClaimed = errors.New("already claimed")

	// ErrLeaseMismatch is returned when a claim-scoped operation supplies
	// a lease ID that does not match the on-disk lease.
	// CLI exit code: 5.
	ErrLeaseMismatch = errors.New("lease mismatch")

	// ErrClaimLost is returned to a losing party after deterministic
	// claim-conflict resolution during a sync merge.
	// CLI exit code: 5.
	ErrClaimLost = errors.New("claim lost in merge")

	// ErrSchemaInvalid is returned when an item fails JSON Schema
	// validation. CLI exit code: 6.
	ErrSchemaInvalid = errors.New("schema validation failed")

	// ErrIntegrityViolated is returned when a workspace-level integrity
	// rule is broken (orphaned reference, dependency on non-existent
	// item, dependent guard, etc.). CLI exit code: 7.
	ErrIntegrityViolated = errors.New("integrity violation")

	// ErrLockTimeout is returned when a per-item lock cannot be acquired
	// within the configured timeout. CLI exit code: 8.
	ErrLockTimeout = errors.New("lock acquisition timeout")

	// ErrPendingOperation is returned when an unfinished multi-file
	// operation is detected and the user is asked to run task recover.
	// CLI exit code: 9.
	ErrPendingOperation = errors.New("incomplete operation; run task recover")

	// ErrUsage is returned for argument or flag errors.
	// CLI exit code: 2.
	ErrUsage = errors.New("usage error")

	// ErrInvalidStatus is returned for illegal status transitions.
	ErrInvalidStatus = errors.New("invalid status transition")

	// ErrCycleDetected is returned when a dependency cycle is found.
	ErrCycleDetected = errors.New("dependency cycle detected")

	// ErrMalformedJSON is returned when input or stored JSON cannot be
	// parsed.
	ErrMalformedJSON = errors.New("malformed JSON")

	// ErrOpAlreadyRecorded is returned by OpResultStore.Put when an
	// entry for the same operation_id already exists. Surfaces
	// caller misuse (re-using an op-id for a different operation) and
	// is also the post-lock-loser path in Claim's idempotency replay.
	// Maps to CLI exit code 5 (treated as a claim-family conflict).
	ErrOpAlreadyRecorded = errors.New("operation_id already recorded")

	// ErrGitConflict is returned by GitClient when a pull/rebase or
	// merge surfaces conflicts in the working tree. usecase.Sync
	// branches on this to invoke usecase.Merge.
	ErrGitConflict = errors.New("git conflict")

	// ErrPluginVeto is returned by HookDispatcher when a plugin
	// returns veto: true on its response. CLI maps this to exit 1
	// (generic error) since the spec reserves the other codes for
	// core operations.
	ErrPluginVeto = errors.New("plugin vetoed operation")
)
