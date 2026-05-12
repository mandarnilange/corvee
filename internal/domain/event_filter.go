package domain

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// EventFilter is the AND-combined filter for the audit-log query
// surfaces (`corvee watch`, `corvee audit`) per spec §15.2. Every populated
// field constrains the result: events must satisfy every flag (AND
// across flags), and within a single flag any of the listed values
// matches (OR within CSV).
//
// Zero-value EventFilter matches every event.
//
// Metadata filtering: From and To are convenience fields for the
// status-transition keys named in §15.2. MetadataMatch is the generic
// key→values map exposed via --meta key=value at the CLI; it covers
// arbitrary keys attached by `corvee done --metadata` or
// `corvee journal --metadata`. Both surfaces canonicalize the on-disk
// value (string / bool / numeric / json.Number) to its string form
// before comparing, so a CLI string like "42" matches a stored
// float64(42) — the natural shape after a JSON round-trip.
type EventFilter struct {
	// Types restricts to events whose Type appears in the list.
	Types []string
	// Actors restricts to events whose Actor appears in the list.
	Actors []string
	// IDs restricts to events whose ItemID equals or is a descendant of
	// any listed id (so --id RKN-E01 captures RKN-E01-S03 as well).
	// Matching is canonical-prefix with a "-" boundary so RKN never
	// matches RKNX.
	IDs []string
	// From restricts to events whose Metadata["from"] (any scalar)
	// stringifies to one of the listed values.
	From []string
	// To restricts to events whose Metadata["to"] (any scalar)
	// stringifies to one of the listed values.
	To []string
	// OperationIDs restricts to events whose OperationID appears in the
	// list — used to gather every event emitted by one multi-file op.
	OperationIDs []string
	// MetadataMatch is the generic per-key filter. Each map entry is
	// one constraint (AND across entries); the value slice is OR. Used
	// to filter on arbitrary keys attached by `corvee done --metadata`
	// and `corvee journal --metadata`.
	MetadataMatch map[string][]string
}

// Match reports whether ev satisfies every populated field of f.
func (f EventFilter) Match(ev Event) bool {
	if len(f.Types) > 0 && !containsString(f.Types, ev.Type) {
		return false
	}
	if len(f.Actors) > 0 && !containsString(f.Actors, ev.Actor) {
		return false
	}
	if len(f.IDs) > 0 && !matchAnyIDPrefix(f.IDs, ev.ItemID) {
		return false
	}
	if len(f.From) > 0 && !matchMetadataValue(ev.Metadata, "from", f.From) {
		return false
	}
	if len(f.To) > 0 && !matchMetadataValue(ev.Metadata, "to", f.To) {
		return false
	}
	if len(f.OperationIDs) > 0 && !containsString(f.OperationIDs, ev.OperationID) {
		return false
	}
	for key, allowed := range f.MetadataMatch {
		if len(allowed) == 0 {
			continue
		}
		if !matchMetadataValue(ev.Metadata, key, allowed) {
			return false
		}
	}
	return true
}

func containsString(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// matchAnyIDPrefix returns true when itemID equals one of the prefixes
// or is a descendant: prefix is followed by "-" in itemID. Empty itemID
// never matches; empty prefix is ignored.
func matchAnyIDPrefix(prefixes []string, itemID string) bool {
	if itemID == "" {
		return false
	}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if itemID == p || strings.HasPrefix(itemID, p+"-") {
			return true
		}
	}
	return false
}

// matchMetadataValue stringifies the metadata value at key and tests
// it against the allowed set. Unsupported types (nested objects,
// arrays, nil) never match — the filter contract is single-value
// equality, not structural querying.
func matchMetadataValue(md map[string]any, key string, allowed []string) bool {
	if md == nil {
		return false
	}
	raw, ok := md[key]
	if !ok {
		return false
	}
	s, ok := metadataValueToString(raw)
	if !ok {
		return false
	}
	return containsString(allowed, s)
}

// metadataValueToString converts a JSON-shaped scalar to the canonical
// CLI string form. Returns false for nil, objects, arrays, or any
// other non-scalar — those are not filterable.
func metadataValueToString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		return strconv.FormatBool(x), true
	case float64:
		// JSON numbers unmarshal to float64. Render integers without
		// the trailing ".0" so CLI "--to=42" matches stored 42.
		if !math.IsNaN(x) && !math.IsInf(x, 0) && x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10), true
		}
		return strconv.FormatFloat(x, 'g', -1, 64), true
	case float32:
		return metadataValueToString(float64(x))
	case int:
		return strconv.Itoa(x), true
	case int32:
		return strconv.FormatInt(int64(x), 10), true
	case int64:
		return strconv.FormatInt(x, 10), true
	case uint:
		return strconv.FormatUint(uint64(x), 10), true
	case uint32:
		return strconv.FormatUint(uint64(x), 10), true
	case uint64:
		return strconv.FormatUint(x, 10), true
	case json.Number:
		return string(x), true
	}
	return "", false
}
