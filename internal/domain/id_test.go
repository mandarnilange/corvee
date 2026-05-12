package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestParseID_AllFormsFromSection4(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in       string
		project  string
		epic     int
		story    int
		subtask  int
		wantType ItemType
	}{
		{"RKN", "RKN", 0, 0, 0, TypeProject},
		{"KAHU", "KAHU", 0, 0, 0, TypeProject},
		{"FOOBAR", "FOOBAR", 0, 0, 0, TypeProject},
		{"RKN-E01", "RKN", 1, 0, 0, TypeEpic},
		{"RKN-E12", "RKN", 12, 0, 0, TypeEpic},
		{"RKN-E01-S03", "RKN", 1, 3, 0, TypeStory},
		{"RKN-E01-S03-T02", "RKN", 1, 3, 2, TypeSubtask},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			id, err := ParseID(c.in)
			if err != nil {
				t.Fatalf("ParseID(%q) err=%v", c.in, err)
			}
			if id.Project != c.project || id.Epic != c.epic || id.Story != c.story || id.Subtask != c.subtask {
				t.Fatalf("ParseID(%q) = %+v, want {%q %d %d %d}", c.in, id, c.project, c.epic, c.story, c.subtask)
			}
			if id.Type() != c.wantType {
				t.Fatalf("ParseID(%q).Type() = %q, want %q", c.in, id.Type(), c.wantType)
			}
			if id.String() != c.in {
				t.Fatalf("ID.String() = %q, want %q", id.String(), c.in)
			}
		})
	}
}

func TestParseID_RejectsMalformed(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",                    // empty
		"rkn",                 // lowercase project
		"RK",                  // too short (must be 3-6)
		"TOOLONG7",            // too long (>6)
		"RKN-",                // trailing dash
		"RKN-E1",              // single digit epic
		"RKN-E001",            // three digits epic
		"RKN-E00",             // zero is not allowed
		"RKN-E01-S",           // empty story segment
		"RKN-E01-S00",         // zero story
		"RKN-E01-S03-T02-X01", // extra segment past subtask
		"RKN-X01",             // wrong segment letter at epic position
		"RKN-E01-X02",         // wrong letter at story position
		"RKN-E01-S03-X02",     // wrong letter at subtask position
		"rkn-E01",             // lowercase project with epic
		"RKN-e01",             // lowercase epic letter
	}
	for _, in := range cases {
		in := in
		name := in
		if name == "" {
			name = "<empty>"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseID(in)
			if !errors.Is(err, ErrUsage) {
				t.Fatalf("ParseID(%q) err=%v, want errors.Is(ErrUsage)", in, err)
			}
		})
	}
}

func TestParentOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{"RKN", ""},
		{"RKN-E01", "RKN"},
		{"RKN-E01-S03", "RKN-E01"},
		{"RKN-E01-S03-T02", "RKN-E01-S03"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.in, func(t *testing.T) {
			t.Parallel()
			got, err := ParentOf(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("ParentOf(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParentOf_RejectsMalformed(t *testing.T) {
	t.Parallel()
	if _, err := ParentOf("not-a-valid-id"); !errors.Is(err, ErrUsage) {
		t.Fatalf("ParentOf invalid err=%v, want ErrUsage", err)
	}
}

func TestChildID_AppendsNextSegmentByDepth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		parent string
		n      int
		want   string
	}{
		{"RKN", 1, "RKN-E01"},
		{"RKN", 12, "RKN-E12"},
		{"RKN", 99, "RKN-E99"},
		{"RKN-E01", 3, "RKN-E01-S03"},
		{"RKN-E01-S03", 2, "RKN-E01-S03-T02"},
	}
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%s+%d", c.parent, c.n), func(t *testing.T) {
			t.Parallel()
			got, err := ChildID(c.parent, c.n)
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Fatalf("ChildID(%q,%d) = %q, want %q", c.parent, c.n, got, c.want)
			}
		})
	}
}

func TestChildID_RejectsBadInputs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		parent string
		n      int
	}{
		{"rkn", 1},             // bad project
		{"RKN-E01-S03-T02", 1}, // can't nest deeper than subtask
		{"RKN", 0},             // n must be >=1
		{"RKN", 100},           // n must fit in 2 digits
		{"RKN", -1},            // n must be positive
		{"", 1},                // empty parent
	}
	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("%s+%d", c.parent, c.n), func(t *testing.T) {
			t.Parallel()
			_, err := ChildID(c.parent, c.n)
			if !errors.Is(err, ErrUsage) {
				t.Fatalf("ChildID(%q,%d) err=%v, want ErrUsage", c.parent, c.n, err)
			}
		})
	}
}

// TestProperty_ParseStringRoundTrip enumerates a small but exhaustive
// space of valid IDs and asserts ParseID(id.String())==id for each. This
// is the §S03 property-test acceptance criterion.
func TestProperty_ParseStringRoundTrip(t *testing.T) {
	t.Parallel()
	projects := []string{"RKN", "ABC", "KAHU", "FOOBAR"}
	for _, p := range projects {
		for e := 0; e <= 4; e++ {
			for s := 0; s <= 4; s++ {
				for st := 0; st <= 4; st++ {
					if e == 0 && (s != 0 || st != 0) {
						continue
					}
					if s == 0 && st != 0 {
						continue
					}
					id := ID{Project: p, Epic: e, Story: s, Subtask: st}
					encoded := id.String()
					if encoded == "" {
						t.Fatalf("empty String for %+v", id)
					}
					parsed, err := ParseID(encoded)
					if err != nil {
						t.Fatalf("ParseID(%q): %v", encoded, err)
					}
					if parsed != id {
						t.Fatalf("round-trip: %+v -> %q -> %+v", id, encoded, parsed)
					}
				}
			}
		}
	}
}

// TestProperty_ParentOfChildIDIsParent asserts ParentOf(ChildID(p,n))==p
// for representative parents at every depth. The §S03 property test
// acceptance criterion.
func TestProperty_ParentOfChildIDIsParent(t *testing.T) {
	t.Parallel()
	parents := []string{"RKN", "ABC", "KAHU-E01", "FOOBAR-E01-S03"}
	for _, p := range parents {
		for n := 1; n <= 5; n++ {
			child, err := ChildID(p, n)
			if err != nil {
				t.Fatalf("ChildID(%q,%d): %v", p, n, err)
			}
			got, err := ParentOf(child)
			if err != nil {
				t.Fatal(err)
			}
			if got != p {
				t.Fatalf("ParentOf(%q) = %q, want %q", child, got, p)
			}
		}
	}
}

func TestID_TypeAndStringZeroValues(t *testing.T) {
	t.Parallel()
	var zero ID
	if zero.Type() != "" {
		t.Errorf("zero ID Type() = %q, want empty", zero.Type())
	}
	if zero.String() != "" {
		t.Errorf("zero ID String() = %q, want empty", zero.String())
	}
	if zero.Parent() != (ID{}) {
		t.Errorf("zero ID Parent() = %+v, want zero", zero.Parent())
	}
}
