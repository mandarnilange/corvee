package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// ValidateInput is the request payload for Validate.
type ValidateInput struct {
	// FailOnIssues, when true, returns ErrIntegrityViolated when at
	// least one issue is found. When false, the issues are returned in
	// the output without an error so CLI users can inspect a partial
	// repair.
	FailOnIssues bool
}

// ValidateOutput is the response payload for Validate.
type ValidateOutput struct {
	// Issues is the deterministic list of structural problems.
	Issues []domain.IntegrityIssue `json:"issues"`
	// OK is true when len(Issues) == 0.
	OK bool `json:"ok"`
}

// Validate runs the workspace integrity checks per spec §6:
// orphans, parent-type mismatches, dependency references, self-deps,
// and cycles.
func Validate(ctx context.Context, d Deps, in ValidateInput) (ValidateOutput, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return ValidateOutput{}, fmt.Errorf("validate: list: %w", err)
	}
	g := domain.NewGraph(items)
	issues := g.ValidateIntegrity()
	out := ValidateOutput{Issues: issues, OK: len(issues) == 0}
	if !out.OK && in.FailOnIssues {
		return out, fmt.Errorf("validate: %d integrity issue(s): %w",
			len(issues), domain.ErrIntegrityViolated)
	}
	return out, nil
}
