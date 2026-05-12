// Package eventlog is the daily-sharded JSONL append-only audit log.
// Each line is one event. Sharding by day reduces git-merge contention
// and makes compaction trivial.
package eventlog
