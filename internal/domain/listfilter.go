package domain

import "sort"

// Apply returns the subset of items that match f, sorted and limited
// per f. The unblocked computation builds a temporary id→item index
// from items so callers don't have to. Apply is a pure function over
// the provided snapshot — Store implementations call this after
// loading items from their backing store.
func (f ListFilter) Apply(items []Item) []Item {
	index := make(map[string]Item, len(items))
	for _, it := range items {
		index[it.ID] = it
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		if f.matches(it, index) {
			out = append(out, it)
		}
	}
	sortItems(out, f.Sort, f.Order)
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}

func (f ListFilter) matches(it Item, all map[string]Item) bool {
	if len(f.Statuses) > 0 && !containsStatus(f.Statuses, it.Status) {
		return false
	}
	if len(f.Types) > 0 && !containsType(f.Types, it.Type) {
		return false
	}
	if len(f.Kinds) > 0 && !containsKind(f.Kinds, it.Kind) {
		return false
	}
	if f.ProjectID != "" && it.ProjectID != f.ProjectID {
		return false
	}
	if f.ParentID != "" && it.ParentID != f.ParentID {
		return false
	}
	if f.Assignee != "" {
		if it.Claim == nil || it.Claim.Agent != f.Assignee {
			return false
		}
	}
	if f.Unassigned && it.Claim != nil {
		return false
	}
	if len(f.Tags) > 0 && !containsAllStrings(it.Tags, f.Tags) {
		return false
	}
	if len(f.Capabilities) > 0 && !subsetOfStrings(it.RequiredCapabilities, f.Capabilities) {
		return false
	}
	if f.Unblocked && !isUnblocked(it, all) {
		return false
	}
	return true
}

func containsStatus(haystack []Status, needle Status) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsType(haystack []ItemType, needle ItemType) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsKind(haystack []Kind, needle Kind) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// containsAllStrings reports whether every element of needles is
// present in haystack. Tag matching: an item must carry every
// requested tag.
func containsAllStrings(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, h := range haystack {
		set[h] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}

// subsetOfStrings reports whether every element of sub is present in
// super. --match-capabilities: an agent's capability set (super) must
// cover the item's required capabilities (sub).
func subsetOfStrings(sub, super []string) bool {
	set := make(map[string]struct{}, len(super))
	for _, h := range super {
		set[h] = struct{}{}
	}
	for _, n := range sub {
		if _, ok := set[n]; !ok {
			return false
		}
	}
	return true
}

// isUnblocked reports whether all of it's Dependencies have status=done.
// An item with no dependencies is unblocked. Missing dependency targets
// (refs to ids not present in the snapshot) count as not-done — the
// usecase author can decide whether to surface that as an integrity
// warning, but for query purposes the safe default is "still blocked".
func isUnblocked(it Item, all map[string]Item) bool {
	for _, depID := range it.Dependencies {
		dep, ok := all[depID]
		if !ok || dep.Status != StatusDone {
			return false
		}
	}
	return true
}

func sortItems(items []Item, key, order string) {
	less := pickLess(key)
	sort.SliceStable(items, func(i, j int) bool {
		if order == "desc" {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
}

func pickLess(key string) func(a, b Item) bool {
	switch key {
	case "priority":
		return byPriority
	case "created_at":
		return byCreatedAt
	case "updated_at":
		return byUpdatedAt
	case "due_date":
		return byDueDate
	}
	return byID
}

func priorityRank(p Priority) int {
	switch p {
	case PriorityCritical:
		return 0
	case PriorityHigh:
		return 1
	case PriorityMedium:
		return 2
	case PriorityLow:
		return 3
	}
	return 4
}

func byPriority(a, b Item) bool {
	ra, rb := priorityRank(a.Priority), priorityRank(b.Priority)
	if ra != rb {
		return ra < rb
	}
	return a.ID < b.ID
}

func byCreatedAt(a, b Item) bool {
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.Before(b.CreatedAt)
	}
	return a.ID < b.ID
}

func byUpdatedAt(a, b Item) bool {
	if !a.UpdatedAt.Equal(b.UpdatedAt) {
		return a.UpdatedAt.Before(b.UpdatedAt)
	}
	return a.ID < b.ID
}

func byDueDate(a, b Item) bool {
	switch {
	case a.DueDate == nil && b.DueDate == nil:
		return a.ID < b.ID
	case a.DueDate == nil:
		return false
	case b.DueDate == nil:
		return true
	default:
		if !a.DueDate.Equal(*b.DueDate) {
			return a.DueDate.Before(*b.DueDate)
		}
		return a.ID < b.ID
	}
}

func byID(a, b Item) bool {
	return a.ID < b.ID
}
