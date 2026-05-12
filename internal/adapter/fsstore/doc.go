// Package fsstore is the production filesystem-backed implementation
// of domain.Store. Each item is persisted as <id>.json under a single
// flat directory (typically .tasks/items/) using the §6 Layer 1
// atomic-write primitive: write to a tmp file, fsync, POSIX rename to
// the canonical name. Phase 1 ships single-writer; cross-process
// per-item locking arrives in Phase 3 (TTR-E03-S04).
package fsstore
