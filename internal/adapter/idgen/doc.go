// Package idgen generates ULIDs for lease IDs and operation IDs.
// ULIDs are sortable and timestamped, which the deterministic claim-
// conflict resolution rule (lower lease ID wins) depends on.
package idgen
