package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// AddInput is the request payload for Add.
type AddInput struct {
	// Type is the structural type of the new item.
	Type domain.ItemType
	// ProjectCode is required for Type=project; ignored otherwise.
	// Must be 3-6 uppercase letters per §4.
	ProjectCode string
	// ParentID is required for non-project types; must be empty for
	// projects.
	ParentID string
	// Title is the human-readable summary (required).
	Title string
	// Description is optional long-form prose.
	Description string
	// Kind is the orthogonal nature-of-work enum (defaults to feature
	// when empty for non-project items).
	Kind domain.Kind
	// Priority is the urgency enum (optional).
	Priority domain.Priority
	// Tags is an optional set of free-form labels.
	Tags []string
	// Deliverable is the epic-only one-sentence outcome statement.
	Deliverable string
	// AcceptanceCriteria is the deduplicated, ordered list of
	// observable conditions for done.
	AcceptanceCriteria []string
	// Dependencies is the set of item IDs that must complete first.
	Dependencies []string
	// RequiredCapabilities is the set of agent capabilities needed
	// to claim this item.
	RequiredCapabilities []string
	// EstimatedHours feeds Impact.EstimatedHours.
	EstimatedHours float64
	// DueDate is an optional deadline.
	DueDate *time.Time
	// Agent is the creator's agent ID (CreatedBy).
	Agent string
	// AgentRole is the creator's role at creation time
	// (CreatedByRole).
	AgentRole domain.Role
	// OperationID is an optional idempotency key (Phase 7).
	OperationID string
}

// AddOutput is the response payload for Add.
type AddOutput struct {
	// Item is the newly persisted item, with ID, version, and
	// timestamps assigned.
	Item domain.Item `json:"item"`
}

// Add creates a new item under the given parent, assigning the next
// free sibling number and stamping creation metadata. Returns
// ErrUsage on invalid type/parent combinations or malformed inputs;
// ErrNotFound when ParentID does not resolve to an existing item;
// ErrIntegrityViolated wraps anything else surprising.
//
// Phase 3 concurrency rule (closing post-review-finding-2): the
// "list siblings → compute next ID → Put" sequence is wrapped in
// Locker.Acquire keyed by the parent (or by the project code for
// project creation) so two concurrent planners cannot both allocate
// the same child ID and silently overwrite each other via
// fsstore.Put(..., -1)'s blind-overwrite semantics.
func Add(ctx context.Context, d Deps, in AddInput) (AddOutput, error) {
	if in.Title == "" {
		return AddOutput{}, fmt.Errorf("add: title is required: %w", domain.ErrUsage)
	}

	lockKey, err := addLockKey(in)
	if err != nil {
		return AddOutput{}, err
	}
	if d.Locker != nil {
		handle, lockErr := d.Locker.Acquire(lockKey, time.Second)
		if lockErr != nil {
			return AddOutput{}, fmt.Errorf("add: acquire %q: %w", lockKey, lockErr)
		}
		defer func() { _ = handle.Release() }()
	}

	id, projectID, parentID, err := resolveNewID(ctx, d, in)
	if err != nil {
		return AddOutput{}, err
	}

	now := d.Clock.Now()
	item := domain.Item{
		SchemaVersion:        domain.CurrentSchemaVersion,
		ID:                   id,
		Type:                 in.Type,
		Kind:                 in.Kind,
		ProjectID:            projectID,
		ParentID:             parentID,
		Title:                in.Title,
		Description:          in.Description,
		Status:               domain.StatusBacklog,
		Priority:             in.Priority,
		Tags:                 in.Tags,
		Deliverable:          in.Deliverable,
		RequiredCapabilities: in.RequiredCapabilities,
		CreatedAt:            now,
		CreatedBy:            in.Agent,
		CreatedByRole:        in.AgentRole,
		UpdatedAt:            now,
		Version:              1,
		Dependencies:         in.Dependencies,
		AcceptanceCriteria:   in.AcceptanceCriteria,
	}
	if in.EstimatedHours > 0 {
		item.Impact.EstimatedHours = in.EstimatedHours
	}
	if in.DueDate != nil {
		due := *in.DueDate
		item.DueDate = &due
	}

	stored, err := d.Store.Put(ctx, item, -1)
	if err != nil {
		return AddOutput{}, fmt.Errorf("add %s: %w", id, err)
	}

	auditAppend(d, domain.Event{
		EventID:   nextEventID(d),
		Timestamp: now,
		Type:      "item_added",
		ItemID:    stored.ID,
		Actor:     in.Agent,
		ActorRole: in.AgentRole,
	})

	// Re-open any ancestor that was previously marked `done`. Adding a
	// child invalidates the parent's "no open work" claim. The walk
	// stops at the first non-done ancestor (everything above that is
	// already in an open state). Failures here are best-effort —
	// logged but not propagated, since the new item itself was
	// persisted successfully.
	if parentID != "" {
		if rerr := reopenAncestorsIfDone(ctx, d, parentID, in.Agent, in.AgentRole, now); rerr != nil {
			slog.Warn("add: failed to auto-reopen ancestors",
				slog.String("child_id", stored.ID),
				slog.String("error", rerr.Error()))
		}
	}

	return AddOutput{Item: stored}, nil
}

// closeAncestorsIfAllChildrenDone walks the parent chain starting at
// startID and promotes every ancestor that was previously auto-reopened
// (carries an `auto_reopened` journal entry, currently `in_progress`,
// and all direct children are settled in {done, abandoned}) back to
// `done`. Each promotion bumps the version, appends an `auto_closed`
// journal entry, stamps CompletedAt, and emits an `auto_closed` audit
// event. Stops at the first ancestor that's not eligible.
//
// Symmetric counterpart to reopenAncestorsIfDone: when the last open
// child of an auto-reopened parent moves away, completes, or is
// abandoned, the parent's "no open work" claim is restored. The
// auto_reopened-marker precondition protects parents that are
// `in_progress` for unrelated reasons (e.g. user-driven status set)
// from being silently flipped to `done`.
func closeAncestorsIfAllChildrenDone(ctx context.Context, d Deps, startID, agent string, role domain.Role, now time.Time) error {
	current := startID
	for current != "" {
		anc, err := d.Store.Get(ctx, current)
		if err != nil {
			return fmt.Errorf("load ancestor %s: %w", current, err)
		}
		if anc.Status != domain.StatusInProgress {
			return nil
		}
		if !journalHas(anc.Journal, "auto_reopened") {
			return nil
		}
		children, err := d.Store.List(ctx, domain.ListFilter{ParentID: anc.ID})
		if err != nil {
			return fmt.Errorf("list children of %s: %w", anc.ID, err)
		}
		if !allChildrenSettled(children) {
			return nil
		}
		completedAt := now
		anc.Status = domain.StatusDone
		anc.CompletedAt = &completedAt
		anc.UpdatedAt = now
		anc.Version++
		anc.Journal = append(anc.Journal, domain.JournalEntry{
			Timestamp: now,
			Actor:     agent,
			Event:     "auto_closed",
			Note:      "ancestor closed: all children settled after re-open",
		})
		if _, err := d.Store.Put(ctx, anc, anc.Version-1); err != nil {
			return fmt.Errorf("put closed ancestor %s: %w", current, err)
		}
		auditAppend(d, domain.Event{
			EventID:   nextEventID(d),
			Timestamp: now,
			Type:      "auto_closed",
			ItemID:    anc.ID,
			Actor:     agent,
			ActorRole: role,
		})
		current = anc.ParentID
	}
	return nil
}

// journalHas reports whether journal contains an entry with the given event name.
func journalHas(journal []domain.JournalEntry, event string) bool {
	for _, je := range journal {
		if je.Event == event {
			return true
		}
	}
	return false
}

// allChildrenSettled reports whether every child in the slice has a
// terminal status (done or abandoned). An empty slice is settled by
// vacuous truth — an ancestor that lost its only open child via Move
// or Delete legitimately has no remaining open work.
func allChildrenSettled(children []domain.Item) bool {
	for _, c := range children {
		if c.Status != domain.StatusDone && c.Status != domain.StatusAbandoned {
			return false
		}
	}
	return true
}

// reopenAncestorsIfDone walks the parent chain starting at startID
// and demotes every `done` ancestor to `in_progress`, clearing
// CompletedAt. Each demotion bumps the version, appends an
// `auto_reopened` journal entry, and emits an `auto_reopened` audit
// event. Stops at the first non-done ancestor.
//
// This is a system-driven status transition; the closed-graph
// `done → in_progress` edge is not legal user-input transition (per
// status.go's validTransitions), but a re-open after the addition of
// open work is the principle of least surprise: the parent had open
// work; it is no longer done.
func reopenAncestorsIfDone(ctx context.Context, d Deps, startID, agent string, role domain.Role, now time.Time) error {
	current := startID
	for current != "" {
		anc, err := d.Store.Get(ctx, current)
		if err != nil {
			return fmt.Errorf("load ancestor %s: %w", current, err)
		}
		if anc.Status != domain.StatusDone {
			return nil
		}
		anc.Status = domain.StatusInProgress
		anc.CompletedAt = nil
		anc.UpdatedAt = now
		anc.Version++
		anc.Journal = append(anc.Journal, domain.JournalEntry{
			Timestamp: now,
			Actor:     agent,
			Event:     "auto_reopened",
			Note:      "ancestor reopened: new child added under done parent",
		})
		if _, err := d.Store.Put(ctx, anc, anc.Version-1); err != nil {
			return fmt.Errorf("put reopened ancestor %s: %w", current, err)
		}
		auditAppend(d, domain.Event{
			EventID:   nextEventID(d),
			Timestamp: now,
			Type:      "auto_reopened",
			ItemID:    anc.ID,
			Actor:     agent,
			ActorRole: role,
		})
		current = anc.ParentID
	}
	return nil
}

// addLockKey derives the Locker key for an Add call. For projects the
// key is the project code (which is also the future ID); for children
// it's the parent ID. Both are valid §4 IDs so they pass the Locker's
// id-grammar guard.
func addLockKey(in AddInput) (string, error) {
	switch in.Type {
	case domain.TypeProject:
		if in.ProjectCode == "" {
			return "", fmt.Errorf("add: project_code is required for projects: %w", domain.ErrUsage)
		}
		return in.ProjectCode, nil
	case domain.TypeEpic, domain.TypeStory, domain.TypeSubtask:
		if in.ParentID == "" {
			return "", fmt.Errorf("add: %s requires parent_id: %w", in.Type, domain.ErrUsage)
		}
		return in.ParentID, nil
	default:
		return "", fmt.Errorf("add: unknown type %q: %w", in.Type, domain.ErrUsage)
	}
}

// resolveNewID validates the type/parent combination, derives the
// new item's ID, and returns the project_id and parent_id fields to
// stamp on the item.
func resolveNewID(ctx context.Context, d Deps, in AddInput) (id, projectID, parentID string, err error) {
	switch in.Type {
	case domain.TypeProject:
		return resolveProjectID(ctx, d, in)
	case domain.TypeEpic, domain.TypeStory, domain.TypeSubtask:
		return resolveChildID(ctx, d, in)
	default:
		return "", "", "", fmt.Errorf("add: unknown type %q: %w", in.Type, domain.ErrUsage)
	}
}

func resolveProjectID(ctx context.Context, d Deps, in AddInput) (string, string, string, error) {
	if in.ParentID != "" {
		return "", "", "", fmt.Errorf("add: project must not have parent_id (%q): %w", in.ParentID, domain.ErrUsage)
	}
	parsed, err := domain.ParseID(in.ProjectCode)
	if err != nil || parsed.Type() != domain.TypeProject {
		return "", "", "", fmt.Errorf("add: project_code %q is not a 3-6 uppercase code: %w", in.ProjectCode, domain.ErrUsage)
	}
	exists, err := d.Store.Exists(ctx, in.ProjectCode)
	if err != nil {
		return "", "", "", fmt.Errorf("add: probe project %q: %w", in.ProjectCode, err)
	}
	if exists {
		return "", "", "", fmt.Errorf("add: project %q already exists: %w", in.ProjectCode, domain.ErrUsage)
	}
	return in.ProjectCode, in.ProjectCode, "", nil
}

func resolveChildID(ctx context.Context, d Deps, in AddInput) (string, string, string, error) {
	if in.ParentID == "" {
		return "", "", "", fmt.Errorf("add: %s requires parent_id: %w", in.Type, domain.ErrUsage)
	}
	parent, err := d.Store.Get(ctx, in.ParentID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return "", "", "", fmt.Errorf("add: parent %q: %w", in.ParentID, domain.ErrNotFound)
		}
		return "", "", "", fmt.Errorf("add: load parent %q: %w", in.ParentID, err)
	}
	expected := expectedParentType(in.Type)
	if parent.Type != expected {
		return "", "", "", fmt.Errorf("add: %s requires parent of type %q, got %q (parent=%s): %w",
			in.Type, expected, parent.Type, in.ParentID, domain.ErrUsage)
	}

	siblings, err := d.Store.List(ctx, domain.ListFilter{ParentID: in.ParentID})
	if err != nil {
		return "", "", "", fmt.Errorf("add: list siblings of %q: %w", in.ParentID, err)
	}
	n, err := nextSiblingNum(siblings, parent.Type)
	if err != nil {
		return "", "", "", err
	}
	id, err := domain.ChildID(in.ParentID, n)
	if err != nil {
		return "", "", "", err
	}
	projectID := parent.ProjectID
	if projectID == "" {
		projectID = parent.ID // parent is the project itself
	}
	return id, projectID, in.ParentID, nil
}

// expectedParentType maps each non-project ItemType to the type its
// parent must have, per spec §15.2. Projects have no parent so the
// case returns the empty type.
func expectedParentType(t domain.ItemType) domain.ItemType {
	switch t {
	case domain.TypeEpic:
		return domain.TypeProject
	case domain.TypeStory:
		return domain.TypeEpic
	case domain.TypeSubtask:
		return domain.TypeStory
	case domain.TypeProject:
		return ""
	}
	return ""
}

// nextSiblingNum returns max(existing sibling segment) + 1. Numbering
// is monotonic: a deleted-sibling gap is not reused, so events-log
// references stay unambiguous. parentType is one of project/epic/
// story — subtasks cannot have children (rejected upstream), so the
// TypeSubtask branch returns 0 to make the exhaustive linter happy
// even though it's unreachable.
func nextSiblingNum(siblings []domain.Item, parentType domain.ItemType) (int, error) {
	top := 0
	for _, s := range siblings {
		parsed, err := domain.ParseID(s.ID)
		if err != nil {
			return 0, fmt.Errorf("add: sibling %q: %w", s.ID, err)
		}
		var n int
		switch parentType {
		case domain.TypeProject:
			n = parsed.Epic
		case domain.TypeEpic:
			n = parsed.Story
		case domain.TypeStory:
			n = parsed.Subtask
		case domain.TypeSubtask:
			return 0, fmt.Errorf("add: subtasks cannot have children: %w", domain.ErrUsage)
		}
		if n > top {
			top = n
		}
	}
	return top + 1, nil
}
