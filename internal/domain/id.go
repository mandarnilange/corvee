package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// projectCodeRE enforces §4: 3-6 uppercase ASCII letters.
var projectCodeRE = regexp.MustCompile(`^[A-Z]{3,6}$`)

// segmentRE enforces a leading letter (E/S/T) followed by exactly two
// decimal digits per §4. Numeric range is checked separately so we
// can reject "00".
var segmentRE = map[byte]*regexp.Regexp{
	'E': regexp.MustCompile(`^E\d{2}$`),
	'S': regexp.MustCompile(`^S\d{2}$`),
	'T': regexp.MustCompile(`^T\d{2}$`),
}

// ID is the structural decomposition of an item identifier per §4.
// Numeric fields are 1-99 when present; 0 means "no segment at this
// depth". The zero value is an empty (invalid) ID.
type ID struct {
	// Project is the 3-6 uppercase project code (e.g. "RKN").
	Project string
	// Epic is the 1-99 epic number, or 0 if not present.
	Epic int
	// Story is the 1-99 story number, or 0 if not present.
	Story int
	// Subtask is the 1-99 subtask number, or 0 if not present.
	Subtask int
}

// Type returns the ItemType implied by the depth of populated segments.
// An empty ID returns the empty ItemType.
func (id ID) Type() ItemType {
	switch {
	case id.Subtask > 0:
		return TypeSubtask
	case id.Story > 0:
		return TypeStory
	case id.Epic > 0:
		return TypeEpic
	case id.Project != "":
		return TypeProject
	}
	return ""
}

// String renders id in canonical "PROJECT-E01-S03-T02" form. Returns
// the empty string if Project is empty.
func (id ID) String() string {
	if id.Project == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(id.Project)
	if id.Epic > 0 {
		fmt.Fprintf(&b, "-E%02d", id.Epic)
		if id.Story > 0 {
			fmt.Fprintf(&b, "-S%02d", id.Story)
			if id.Subtask > 0 {
				fmt.Fprintf(&b, "-T%02d", id.Subtask)
			}
		}
	}
	return b.String()
}

// Parent returns the parent ID. Projects (or the zero ID) return the
// zero ID — callers rendering with String() get an empty result, which
// mirrors §4 ("project has no parent").
func (id ID) Parent() ID {
	switch {
	case id.Subtask > 0:
		id.Subtask = 0
	case id.Story > 0:
		id.Story = 0
	case id.Epic > 0:
		id.Epic = 0
	default:
		return ID{}
	}
	return id
}

// ParseID parses s into an ID. Wraps ErrUsage on any malformation
// (bad project code, wrong segment letter, non-2-digit number, zero
// number, trailing junk).
func ParseID(s string) (ID, error) {
	if s == "" {
		return ID{}, fmt.Errorf("parse id: empty input: %w", ErrUsage)
	}
	parts := strings.Split(s, "-")
	if len(parts) > 4 {
		return ID{}, fmt.Errorf("parse id %q: too many segments (max 4): %w", s, ErrUsage)
	}
	if !projectCodeRE.MatchString(parts[0]) {
		return ID{}, fmt.Errorf("parse id %q: project code must be 3-6 uppercase letters: %w", s, ErrUsage)
	}
	id := ID{Project: parts[0]}
	depth := []struct {
		letter byte
		assign func(int)
	}{
		{'E', func(n int) { id.Epic = n }},
		{'S', func(n int) { id.Story = n }},
		{'T', func(n int) { id.Subtask = n }},
	}
	for i, seg := range parts[1:] {
		expected := depth[i]
		if !segmentRE[expected.letter].MatchString(seg) {
			return ID{}, fmt.Errorf("parse id %q: segment %q must match %c\\d{2}: %w", s, seg, expected.letter, ErrUsage)
		}
		n, err := strconv.Atoi(seg[1:])
		if err != nil || n < 1 {
			return ID{}, fmt.Errorf("parse id %q: segment %q has zero or non-numeric index: %w", s, seg, ErrUsage)
		}
		expected.assign(n)
	}
	return id, nil
}

// ParentOf returns the canonical parent ID string of s. Returns "" for
// projects, or wraps ErrUsage if s is malformed.
func ParentOf(s string) (string, error) {
	id, err := ParseID(s)
	if err != nil {
		return "", err
	}
	return id.Parent().String(), nil
}

// ChildID returns the canonical ID of the n-th child of parent. n must
// be in [1,99] so it fits the §4 two-digit zero-padded format. Returns
// ErrUsage if parent is malformed, n is out of range, or parent is
// already at subtask depth.
//
// ChildID is a pure formatter — discovering "next free n" given a set
// of existing siblings is the caller's responsibility (typically the
// usecase that enumerates Store contents).
func ChildID(parent string, n int) (string, error) {
	if n < 1 || n > 99 {
		return "", fmt.Errorf("child id: n=%d outside [1,99]: %w", n, ErrUsage)
	}
	id, err := ParseID(parent)
	if err != nil {
		return "", err
	}
	switch id.Type() {
	case TypeProject:
		id.Epic = n
	case TypeEpic:
		id.Story = n
	case TypeStory:
		id.Subtask = n
	case TypeSubtask:
		return "", fmt.Errorf("child id: parent %q is already a subtask; cannot nest deeper: %w", parent, ErrUsage)
	default:
		return "", fmt.Errorf("child id: parent %q has no resolvable type: %w", parent, ErrUsage)
	}
	return id.String(), nil
}
