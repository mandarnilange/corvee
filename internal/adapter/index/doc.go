// Package index maintains the local cache (.tasks/index.json).
// The index is gitignored, always rebuildable, and treated as a cache
// — never as the source of truth. Staleness is detected by comparing
// item count and max mtime against the items/ directory.
package index
