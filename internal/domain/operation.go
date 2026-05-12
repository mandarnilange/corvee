package domain

import "time"

// OpKind identifies the type of multi-file operation recorded in the
// WAL journal. Used by Recover to dispatch the correct step handlers.
type OpKind string

// OpKind values for the supported Phase 4 operation types.
const (
	// OpKindMove represents a subtree move (new parent, cascading re-ID).
	OpKindMove OpKind = "move"
	// OpKindRename represents an explicit ID rename with cascading child re-ID.
	OpKindRename OpKind = "rename"
	// OpKindDeleteCascade represents a hard delete of an item and its descendants.
	OpKindDeleteCascade OpKind = "delete_cascade"
	// OpKindCloneWithChildren represents a deep clone (item + all descendants).
	OpKindCloneWithChildren OpKind = "clone_with_children"
)

// OpStatus is the lifecycle state of a journal'd operation.
type OpStatus string

// OpStatus values per §14.2.
const (
	// OpStatusPending indicates the operation intent was written but execution
	// has not yet started.
	OpStatusPending OpStatus = "pending"
	// OpStatusExecuting indicates the operation is actively executing steps.
	OpStatusExecuting OpStatus = "executing"
	// OpStatusCompleted indicates all steps finished successfully.
	OpStatusCompleted OpStatus = "completed"
	// OpStatusFailed indicates recovery was attempted but could not complete.
	OpStatusFailed OpStatus = "failed"
)

// OpStep is one atomic step in an Operation's execution plan. Each step
// is applied and marked done sequentially; Recover resumes from the first
// step whose Done == false.
type OpStep struct {
	// Step is the zero-based sequential index within Plan.
	Step int `json:"step"`
	// Kind describes what this step does (e.g. "create_items",
	// "delete_items", "rewrite_refs", "append_event").
	Kind string `json:"kind"`
	// Args carries the step-specific parameters needed to execute or
	// re-execute the step during recovery. Stored verbatim as JSON.
	Args map[string]any `json:"args,omitempty"`
	// Done is true when the step has been applied and the MarkStep call
	// has been persisted.
	Done bool `json:"done"`
}

// Operation is the WAL record for a multi-file operation. Written by
// OpJournal.Begin before any file is mutated; updated step-by-step via
// MarkStep; closed via Complete. Operations in executing state that
// survive a process restart are re-driven by usecase.Recover.
type Operation struct {
	// OpID is the ULID identifying this operation (from IDGen.ULID).
	OpID string `json:"op_id"`
	// Kind is the operation type enum.
	Kind OpKind `json:"kind"`
	// Agent is the agent ID that initiated the operation.
	Agent string `json:"agent"`
	// StartedAt is the wall-clock time Begin was called.
	StartedAt time.Time `json:"started_at"`
	// Status is the lifecycle state.
	Status OpStatus `json:"status"`
	// Plan is the ordered list of steps; each is executed exactly once.
	Plan []OpStep `json:"plan"`
}
