package usecase

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/mandarnilange/corvee/internal/domain"
)

// NextInput is the request payload for Next.
type NextInput struct {
	// Agent is the consulting agent's ID; populates auto-claim when
	// AutoClaim is true.
	Agent string
	// AgentRole is the consulting agent's role.
	AgentRole domain.Role
	// AgentCapabilities is the consulting agent's capability set;
	// items whose RequiredCapabilities are not a subset are filtered.
	AgentCapabilities []string
	// ProjectID, when non-empty, restricts to one project.
	ProjectID string
	// Kinds, when non-empty, filters by Kind.
	Kinds []domain.Kind
	// Types, when non-empty, restricts to those ItemTypes (project,
	// epic, story, subtask). Defaults to all four when empty so the
	// existing call sites (which never set this) preserve behavior.
	Types []domain.ItemType
	// AutoClaim, when true, attempts to claim the chosen item before
	// returning it.
	AutoClaim bool
}

// NextOutput is the response payload for Next.
type NextOutput struct {
	// Item is the next-best candidate (zero-value when none found).
	Item domain.Item `json:"item,omitempty"`
	// LeaseID is non-empty when AutoClaim succeeded.
	LeaseID string `json:"lease_id,omitempty"`
	// Found is true when a candidate was selected.
	Found bool `json:"found"`
}

// Next picks the highest-ranked claimable item for the agent, applying
// the spec's filters (status=ready, unblocked deps, capability match,
// optional project/kind constraints) and selection bonuses
// (workspace.critical_path membership, then priority, then ID).
//
// AutoClaim runs Claim with the chosen item's ID to remove the
// classic "two-call" race; on AlreadyClaimed it falls back to the
// next candidate.
func Next(ctx context.Context, d Deps, in NextInput) (NextOutput, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{
		Statuses:     []domain.Status{domain.StatusReady},
		Types:        in.Types,
		Kinds:        in.Kinds,
		ProjectID:    in.ProjectID,
		Capabilities: in.AgentCapabilities,
		Unblocked:    true,
	})
	if err != nil {
		return NextOutput{}, fmt.Errorf("next: list: %w", err)
	}

	criticalSet := map[string]int{}
	if d.Workspace != nil {
		if ws, wsErr := d.Workspace.Load(); wsErr == nil {
			for i, id := range ws.CriticalPath {
				criticalSet[id] = i
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return rankLess(items[i], items[j], criticalSet)
	})

	for _, candidate := range items {
		if !in.AutoClaim {
			return NextOutput{Item: candidate, Found: true}, nil
		}
		out, err := Claim(ctx, d, ClaimInput{
			ID:                candidate.ID,
			Agent:             in.Agent,
			AgentRole:         in.AgentRole,
			AgentCapabilities: in.AgentCapabilities,
		})
		if err == nil {
			return NextOutput{Item: out.Item, LeaseID: out.LeaseID, Found: true}, nil
		}
		// On contention, try the next candidate. Other errors bubble.
		if errors.Is(err, domain.ErrAlreadyClaimed) || errors.Is(err, domain.ErrLockTimeout) {
			continue
		}
		return NextOutput{}, err
	}
	return NextOutput{Found: false}, nil
}

// rankLess implements the §spec selection ordering: critical-path
// membership first (lower index = higher priority), then Priority
// (critical < high < medium < low), then ID for determinism.
func rankLess(a, b domain.Item, critical map[string]int) bool {
	ai, aOk := critical[a.ID]
	bi, bOk := critical[b.ID]
	if aOk && !bOk {
		return true
	}
	if !aOk && bOk {
		return false
	}
	if aOk && bOk && ai != bi {
		return ai < bi
	}
	if a.Priority != b.Priority {
		return priorityRank(a.Priority) < priorityRank(b.Priority)
	}
	return a.ID < b.ID
}

func priorityRank(p domain.Priority) int {
	switch p {
	case domain.PriorityCritical:
		return 0
	case domain.PriorityHigh:
		return 1
	case domain.PriorityMedium:
		return 2
	case domain.PriorityLow:
		return 3
	}
	return 4
}
