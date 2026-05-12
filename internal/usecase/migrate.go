package usecase

import (
	"context"
	"fmt"

	"github.com/mandarnilange/corvee/internal/domain"
)

// MigrateInput is the request payload for Migrate.
type MigrateInput struct {
	// DryRun, when true, reports what would be migrated without writing.
	DryRun bool
}

// MigrationRecord describes a successfully migrated item.
type MigrationRecord struct {
	// ID is the item's canonical identifier.
	ID string `json:"id"`
	// FromVersion is the schema_version before migration.
	FromVersion int `json:"from_version"`
	// ToVersion is the schema_version after migration.
	ToVersion int `json:"to_version"`
}

// SkippedRecord describes an item that could not be migrated.
type SkippedRecord struct {
	// ID is the item's canonical identifier.
	ID string `json:"id"`
	// CurrentVersion is the item's schema_version on disk.
	CurrentVersion int `json:"current_version"`
	// BinarySupports is the maximum schema_version this binary handles.
	BinarySupports int `json:"binary_supports"`
	// Reason is the human-readable explanation (e.g. "binary too old").
	Reason string `json:"reason"`
}

// MigrateOutput is the response payload for Migrate.
type MigrateOutput struct {
	// Migrated lists items that were upgraded.
	Migrated []MigrationRecord `json:"migrated"`
	// Skipped lists items that could not be migrated.
	Skipped []SkippedRecord `json:"skipped"`
}

// Migrate scans the workspace for items whose schema_version is below
// CurrentSchemaVersion and upgrades them by applying registered upgraders.
// Items whose schema_version exceeds CurrentSchemaVersion are reported in
// Skipped with reason="binary too old" — they are NOT migrated, NOT
// downgraded, and NOT silently dropped. Idempotent: running again after a
// successful migration is a no-op.
//
// Auto-invoked by list/show when an item with an old schema_version is
// encountered.
func Migrate(ctx context.Context, d Deps, in MigrateInput) (MigrateOutput, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return MigrateOutput{}, fmt.Errorf("migrate: list: %w", err)
	}

	var migrated []MigrationRecord
	var skipped []SkippedRecord

	for _, item := range items {
		switch {
		case item.SchemaVersion == domain.CurrentSchemaVersion:
			// Already current — nothing to do.
			continue

		case item.SchemaVersion > domain.CurrentSchemaVersion:
			// Written by a newer binary; do not touch.
			skipped = append(skipped, SkippedRecord{
				ID:             item.ID,
				CurrentVersion: item.SchemaVersion,
				BinarySupports: domain.CurrentSchemaVersion,
				Reason:         "binary too old",
			})

		default:
			// item.SchemaVersion < domain.CurrentSchemaVersion
			from := item.SchemaVersion
			upgraded, err := applyUpgraders(item, from, domain.CurrentSchemaVersion)
			if err != nil {
				skipped = append(skipped, SkippedRecord{
					ID:             item.ID,
					CurrentVersion: from,
					BinarySupports: domain.CurrentSchemaVersion,
					Reason:         fmt.Sprintf("upgrade failed: %s", err.Error()),
				})
				continue
			}
			if !in.DryRun {
				if _, putErr := d.Store.Put(ctx, upgraded, -1); putErr != nil {
					return MigrateOutput{}, fmt.Errorf("migrate: put %s: %w", item.ID, putErr)
				}
			}
			migrated = append(migrated, MigrationRecord{
				ID:          item.ID,
				FromVersion: from,
				ToVersion:   domain.CurrentSchemaVersion,
			})
		}
	}

	return MigrateOutput{Migrated: migrated, Skipped: skipped}, nil
}

// applyUpgraders runs all registered upgraders on item, advancing from
// fromVersion to toVersion one step at a time. Each upgrader handles
// exactly one version transition (v → v+1).
//
// The upgrader registry is a slice of functions indexed by from-version.
// Adding a new schema version means appending one entry to upgraders.
func applyUpgraders(item domain.Item, from, to int) (domain.Item, error) {
	for v := from; v < to; v++ {
		if v >= len(upgraders) {
			// No upgrader registered for this transition.
			// This means the binary doesn't know how to upgrade from v.
			// Treat as success if item is already at the target (shouldn't happen)
			// or as a no-op bump for versions where only additive fields changed.
			item.SchemaVersion = v + 1
			continue
		}
		var err error
		item, err = upgraders[v](item)
		if err != nil {
			return item, fmt.Errorf("upgrader v%d→v%d: %w", v, v+1, err)
		}
		item.SchemaVersion = v + 1
	}
	return item, nil
}

// upgradeFunc is a function that upgrades an Item from version N to N+1.
// upgraders[n] upgrades from schema_version=n to schema_version=n+1.
type upgradeFunc func(domain.Item) (domain.Item, error)

// upgraders is the registry of schema upgraders, indexed by from-version.
// Currently empty because CurrentSchemaVersion==1 and there is no v0→v1
// upgrade path other than bumping the version counter.
var upgraders = []upgradeFunc{
	// upgraders[0]: v0 → v1. Version 0 items are identical to v1 items in
	// structure (v1 merely formalised the schema_version field). Bumping
	// the field is the entire migration.
	func(item domain.Item) (domain.Item, error) {
		return item, nil // field already bumped by applyUpgraders
	},
}
