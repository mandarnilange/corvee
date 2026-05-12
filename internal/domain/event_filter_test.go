package domain

import (
	"testing"
	"time"
)

func mkEvent(typ, id, actor string, opts ...func(*Event)) Event {
	e := Event{
		EventID:   "01J000000000000000000000",
		Timestamp: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		Type:      typ,
		ItemID:    id,
		Actor:     actor,
	}
	for _, o := range opts {
		o(&e)
	}
	return e
}

func TestEventFilter_Match_EmptyMatchesAll(t *testing.T) {
	t.Parallel()
	f := EventFilter{}
	if !f.Match(mkEvent("item_added", "RKN-E01", "alice")) {
		t.Fatalf("empty filter should match any event")
	}
}

func TestEventFilter_Match_TypeFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		filter []string
		ev     string
		want   bool
	}{
		{"single match", []string{"item_added"}, "item_added", true},
		{"single mismatch", []string{"item_added"}, "claimed", false},
		{"csv or match-first", []string{"item_added", "claimed"}, "item_added", true},
		{"csv or match-last", []string{"item_added", "claimed"}, "claimed", true},
		{"csv or none", []string{"item_added", "claimed"}, "released", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := EventFilter{Types: tc.filter}
			got := f.Match(mkEvent(tc.ev, "RKN-E01", "alice"))
			if got != tc.want {
				t.Fatalf("Types=%v ev=%q got=%v want=%v", tc.filter, tc.ev, got, tc.want)
			}
		})
	}
}

func TestEventFilter_Match_ActorFilter(t *testing.T) {
	t.Parallel()
	f := EventFilter{Actors: []string{"alice", "bob"}}
	if !f.Match(mkEvent("item_added", "RKN-E01", "alice")) {
		t.Fatalf("alice should match")
	}
	if !f.Match(mkEvent("item_added", "RKN-E01", "bob")) {
		t.Fatalf("bob should match")
	}
	if f.Match(mkEvent("item_added", "RKN-E01", "carol")) {
		t.Fatalf("carol should not match")
	}
}

func TestEventFilter_Match_IDPrefixIncludesDescendants(t *testing.T) {
	t.Parallel()
	f := EventFilter{IDs: []string{"RKN-E01"}}
	cases := []struct {
		id   string
		want bool
	}{
		{"RKN-E01", true},
		{"RKN-E01-S03", true},
		{"RKN-E01-S03-T02", true},
		{"RKN-E02", false},
		{"RKN-E02-S01", false},
		{"OTHER-E01", false},
		{"", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			got := f.Match(mkEvent("item_added", tc.id, "alice"))
			if got != tc.want {
				t.Fatalf("id=%q got=%v want=%v", tc.id, got, tc.want)
			}
		})
	}
}

func TestEventFilter_Match_IDExactMatchesProjectOnly(t *testing.T) {
	t.Parallel()
	// "RKN" alone (project root) should match anything starting with "RKN-".
	f := EventFilter{IDs: []string{"RKN"}}
	if !f.Match(mkEvent("item_added", "RKN-E01", "alice")) {
		t.Fatalf("RKN-E01 should match prefix RKN")
	}
	if f.Match(mkEvent("item_added", "RKNX-E01", "alice")) {
		t.Fatalf("RKNX-E01 must not match prefix RKN — boundary required")
	}
}

func TestEventFilter_Match_FromAndTo(t *testing.T) {
	t.Parallel()
	ev := mkEvent("status_changed", "RKN-E01-S03", "alice", func(e *Event) {
		e.Metadata = map[string]any{"from": "ready", "to": "in_progress"}
	})
	if !(EventFilter{From: []string{"ready"}}).Match(ev) {
		t.Fatalf("from=ready should match")
	}
	if !(EventFilter{To: []string{"in_progress"}}).Match(ev) {
		t.Fatalf("to=in_progress should match")
	}
	if (EventFilter{From: []string{"backlog"}}).Match(ev) {
		t.Fatalf("from=backlog should not match")
	}
	if (EventFilter{To: []string{"done"}}).Match(ev) {
		t.Fatalf("to=done should not match")
	}
}

func TestEventFilter_Match_FromTo_MissingMetadata(t *testing.T) {
	t.Parallel()
	ev := mkEvent("item_added", "RKN-E01", "alice")
	if (EventFilter{From: []string{"ready"}}).Match(ev) {
		t.Fatalf("event with no from metadata should not match from=ready")
	}
	if (EventFilter{To: []string{"in_progress"}}).Match(ev) {
		t.Fatalf("event with no to metadata should not match to=in_progress")
	}
}

func TestEventFilter_Match_OperationID(t *testing.T) {
	t.Parallel()
	ev := mkEvent("item_added", "RKN-E01", "alice", func(e *Event) {
		e.OperationID = "01J0OP000000000000000000"
	})
	if !(EventFilter{OperationIDs: []string{"01J0OP000000000000000000"}}).Match(ev) {
		t.Fatalf("op id should match")
	}
	if (EventFilter{OperationIDs: []string{"other"}}).Match(ev) {
		t.Fatalf("op id should not match")
	}
}

func TestEventFilter_Match_FromTo_NumericValueRendered(t *testing.T) {
	t.Parallel()
	// JSON-round-tripped numbers come through as float64; the matcher
	// must render them so a CLI filter --to=42 matches Metadata{"to":42}.
	ev := mkEvent("status_changed", "RKN-E01-S03", "alice", func(e *Event) {
		e.Metadata = map[string]any{"from": float64(7), "to": float64(42)}
	})
	if !(EventFilter{From: []string{"7"}}).Match(ev) {
		t.Fatalf("from=7 (string) should match metadata.from=float64(7)")
	}
	if !(EventFilter{To: []string{"42"}}).Match(ev) {
		t.Fatalf("to=42 should match")
	}
	if (EventFilter{To: []string{"43"}}).Match(ev) {
		t.Fatalf("to=43 must not match metadata.to=42")
	}
}

func TestEventFilter_Match_FromTo_BoolValueRendered(t *testing.T) {
	t.Parallel()
	ev := mkEvent("custom", "RKN-E01", "alice", func(e *Event) {
		e.Metadata = map[string]any{"from": false, "to": true}
	})
	if !(EventFilter{From: []string{"false"}}).Match(ev) {
		t.Fatalf("from=false should match metadata.from=bool(false)")
	}
	if !(EventFilter{To: []string{"true"}}).Match(ev) {
		t.Fatalf("to=true should match metadata.to=bool(true)")
	}
}

func TestEventFilter_Match_FromTo_FloatPreservesPrecision(t *testing.T) {
	t.Parallel()
	ev := mkEvent("custom", "RKN-E01", "alice", func(e *Event) {
		e.Metadata = map[string]any{"to": 3.14}
	})
	if !(EventFilter{To: []string{"3.14"}}).Match(ev) {
		t.Fatalf("to=3.14 should match metadata.to=float64(3.14)")
	}
}

func TestEventFilter_Match_GenericMetadataMatch(t *testing.T) {
	t.Parallel()
	ev := mkEvent("completed", "RKN-E01-S03", "alice", func(e *Event) {
		e.Metadata = map[string]any{
			"deploy_url": "https://example.com/r/42",
			"sha":        "abc123",
			"priority":   float64(2),
			"approved":   true,
		}
	})

	cases := []struct {
		name string
		f    EventFilter
		want bool
	}{
		{
			name: "single key string match",
			f:    EventFilter{MetadataMatch: map[string][]string{"sha": {"abc123"}}},
			want: true,
		},
		{
			name: "single key string mismatch",
			f:    EventFilter{MetadataMatch: map[string][]string{"sha": {"def456"}}},
			want: false,
		},
		{
			name: "single key OR within values",
			f:    EventFilter{MetadataMatch: map[string][]string{"sha": {"def456", "abc123"}}},
			want: true,
		},
		{
			name: "numeric value rendered",
			f:    EventFilter{MetadataMatch: map[string][]string{"priority": {"2"}}},
			want: true,
		},
		{
			name: "boolean value rendered",
			f:    EventFilter{MetadataMatch: map[string][]string{"approved": {"true"}}},
			want: true,
		},
		{
			name: "two keys ANDed",
			f: EventFilter{MetadataMatch: map[string][]string{
				"sha":      {"abc123"},
				"priority": {"2"},
			}},
			want: true,
		},
		{
			name: "two keys: one mismatches → fail",
			f: EventFilter{MetadataMatch: map[string][]string{
				"sha":      {"abc123"},
				"priority": {"99"},
			}},
			want: false,
		},
		{
			name: "missing key → fail",
			f:    EventFilter{MetadataMatch: map[string][]string{"absent": {"x"}}},
			want: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.f.Match(ev); got != tc.want {
				t.Fatalf("got=%v want=%v", got, tc.want)
			}
		})
	}
}

func TestEventFilter_Match_AllFlagsAndedTogether(t *testing.T) {
	t.Parallel()
	ev := mkEvent("status_changed", "RKN-E01-S03", "alice", func(e *Event) {
		e.OperationID = "op123"
		e.Metadata = map[string]any{"from": "ready", "to": "in_progress"}
	})
	f := EventFilter{
		Types:        []string{"status_changed"},
		Actors:       []string{"alice"},
		IDs:          []string{"RKN-E01"},
		From:         []string{"ready"},
		To:           []string{"in_progress"},
		OperationIDs: []string{"op123"},
	}
	if !f.Match(ev) {
		t.Fatalf("all-flags should AND-match")
	}
	// Flip one field to mismatch — overall should fail.
	f2 := f
	f2.Actors = []string{"bob"}
	if f2.Match(ev) {
		t.Fatalf("AND must fail when any field mismatches")
	}
}
