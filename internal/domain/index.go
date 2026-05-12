package domain

import "time"

// IndexEntry is a single row of the workspace index cache. It records
// just enough metadata for `corvee list`/`corvee next` to filter and
// rank without reading every per-item JSON file.
type IndexEntry struct {
	// ID is the canonical item ID.
	ID string `json:"id"`
	// Type is the structural type (project|epic|story|subtask).
	Type ItemType `json:"type"`
	// ProjectID is the root project for this item.
	ProjectID string `json:"project_id,omitempty"`
	// ParentID is the immediate parent ID (empty for projects).
	ParentID string `json:"parent_id,omitempty"`
	// Status is the current lifecycle status.
	Status Status `json:"status"`
	// Priority is the urgency enum.
	Priority Priority `json:"priority,omitempty"`
	// Kind is the orthogonal nature-of-work enum.
	Kind Kind `json:"kind,omitempty"`
	// RequiredCapabilities mirrors Item.RequiredCapabilities.
	RequiredCapabilities []string `json:"required_capabilities,omitempty"`
	// Dependencies is the set of IDs this item depends on.
	Dependencies []string `json:"dependencies,omitempty"`
	// HasClaim indicates whether the item is currently leased.
	HasClaim bool `json:"has_claim,omitempty"`
	// Version is the persisted item version, used for staleness checks.
	Version int `json:"version"`
	// UpdatedAt is the per-item updated_at timestamp; the index
	// max(updated_at) is compared with the on-disk max(updated_at) to
	// detect staleness.
	UpdatedAt time.Time `json:"updated_at"`
	// Title is the display label, mirrored for `corvee list`.
	Title string `json:"title,omitempty"`
}

// Index is the workspace cache persisted at .tasks/index.json. It is
// fully rebuildable from the per-item files via `corvee reindex` and is
// gitignored — never source of truth.
type Index struct {
	// SchemaVersion pins the cache layout for forward-compat.
	SchemaVersion int `json:"schema_version"`
	// BuiltAt is the wall-clock time the index was last rebuilt.
	BuiltAt time.Time `json:"built_at"`
	// MaxItemUpdatedAt is the max(item.updated_at) seen at build time;
	// retained for backward compat with older caches but no longer
	// load-bearing for staleness — see MaxFileModTime.
	MaxItemUpdatedAt time.Time `json:"max_item_updated_at"`
	// MaxFileModTime is the max(os.Stat().ModTime()) across the
	// items/ directory at build time. Staleness is checked against
	// this so the freshness probe does N stat calls instead of N
	// full-file reads + JSON parses (review-finding-8).
	MaxFileModTime time.Time `json:"max_file_mod_time"`
	// ItemCount is the number of entries in Items at build time.
	ItemCount int `json:"item_count"`
	// Items is the index payload, sorted by ID for determinism.
	Items []IndexEntry `json:"items"`
}

// IndexEntryFromItem projects an Item into an IndexEntry. Used by the
// reindex usecase and by adapter writers that emit an updated entry on
// every mutation.
func IndexEntryFromItem(it Item) IndexEntry {
	return IndexEntry{
		ID:                   it.ID,
		Type:                 it.Type,
		ProjectID:            it.ProjectID,
		ParentID:             it.ParentID,
		Status:               it.Status,
		Priority:             it.Priority,
		Kind:                 it.Kind,
		RequiredCapabilities: append([]string(nil), it.RequiredCapabilities...),
		Dependencies:         append([]string(nil), it.Dependencies...),
		HasClaim:             it.Claim != nil,
		Version:              it.Version,
		UpdatedAt:            it.UpdatedAt,
		Title:                it.Title,
	}
}
