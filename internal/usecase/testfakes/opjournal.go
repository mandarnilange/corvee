package testfakes

import (
	"fmt"
	"sync"

	"github.com/mandarnilange/corvee/internal/domain"
)

// OpJournal is an in-memory fake of domain.OpJournal. It stores
// operations in a map keyed by op_id and supports the full lifecycle
// (Begin/MarkStep/Complete/Pending) for unit tests.
type OpJournal struct {
	mu  sync.Mutex
	ops map[string]domain.Operation
}

// NewOpJournal returns an empty fake journal.
func NewOpJournal() *OpJournal {
	return &OpJournal{ops: map[string]domain.Operation{}}
}

// Begin writes op with status=executing.
func (j *OpJournal) Begin(op domain.Operation) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	op.Status = domain.OpStatusExecuting
	j.ops[op.OpID] = copyOp(op)
	return nil
}

// MarkStep marks stepNum done within opID.
func (j *OpJournal) MarkStep(opID string, stepNum int) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.ops[opID]
	if !ok {
		return fmt.Errorf("opjournal(fake): %s not found: %w", opID, domain.ErrNotFound)
	}
	if stepNum < 0 || stepNum >= len(op.Plan) {
		return fmt.Errorf("opjournal(fake): step %d out of range: %w", stepNum, domain.ErrUsage)
	}
	op.Plan[stepNum].Done = true
	j.ops[opID] = op
	return nil
}

// Complete flips status to completed.
func (j *OpJournal) Complete(opID string) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	op, ok := j.ops[opID]
	if !ok {
		return fmt.Errorf("opjournal(fake): %s not found: %w", opID, domain.ErrNotFound)
	}
	op.Status = domain.OpStatusCompleted
	j.ops[opID] = op
	return nil
}

// Pending returns all operations in pending or executing state.
func (j *OpJournal) Pending() ([]domain.Operation, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var out []domain.Operation
	for _, op := range j.ops {
		if op.Status == domain.OpStatusPending || op.Status == domain.OpStatusExecuting {
			out = append(out, copyOp(op))
		}
	}
	return out, nil
}

// All returns all operations (for test assertions).
func (j *OpJournal) All() []domain.Operation {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]domain.Operation, 0, len(j.ops))
	for _, op := range j.ops {
		out = append(out, copyOp(op))
	}
	return out
}

func copyOp(op domain.Operation) domain.Operation {
	plan := make([]domain.OpStep, len(op.Plan))
	for i, step := range op.Plan {
		args := map[string]any{}
		for k, v := range step.Args {
			args[k] = v
		}
		plan[i] = domain.OpStep{
			Step: step.Step,
			Kind: step.Kind,
			Args: args,
			Done: step.Done,
		}
	}
	return domain.Operation{
		OpID:      op.OpID,
		Kind:      op.Kind,
		Agent:     op.Agent,
		StartedAt: op.StartedAt,
		Status:    op.Status,
		Plan:      plan,
	}
}
