package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mandarnilange/corvee/internal/domain"
)

// CompactInput is the request payload for Compact.
type CompactInput struct {
	// Before is the ISO date cutoff. Empty defaults to 90 days ago.
	// Shards and operations dated before this are archived/pruned.
	Before string
	// TasksDir overrides Deps.TasksDir for the duration of the call.
	// Empty falls back to Deps.TasksDir; required if neither is set.
	// Most callers leave this empty and let the wired Deps take over;
	// tests use it to point at t.TempDir() without rebuilding Deps.
	TasksDir string
}

// CompactOutput is the response payload for Compact (per spec §15.2).
type CompactOutput struct {
	// ShardsArchived counts events shards moved or removed.
	ShardsArchived int `json:"shards_archived"`
	// OpsPruned counts operation files deleted.
	OpsPruned int `json:"ops_pruned"`
	// AliasesPruned counts items whose alias[] was trimmed.
	AliasesPruned int `json:"aliases_pruned"`
	// BytesSaved is the cumulative size of archived/deleted files.
	BytesSaved int64 `json:"bytes_saved"`
}

const defaultCompactWindow = 90 * 24 * time.Hour

// Compact runs the maintenance pass described in spec §15.2:
// archives events shards older than the cutoff, prunes completed
// operations, and gc's aliases that no longer reference live items.
//
// Idempotent — re-running on a workspace already-compacted returns a
// zero-output result.
func Compact(ctx context.Context, d Deps, in CompactInput) (CompactOutput, error) {
	tasksDir := in.TasksDir
	if tasksDir == "" {
		tasksDir = d.TasksDir
	}
	if tasksDir == "" {
		return CompactOutput{}, fmt.Errorf("compact: tasks_dir is required: %w", domain.ErrUsage)
	}
	cutoff, err := resolveCutoff(d, in.Before)
	if err != nil {
		return CompactOutput{}, err
	}
	out := CompactOutput{}

	// Archive events shards.
	eventsDir := filepath.Join(tasksDir, "events")
	archive := filepath.Join(eventsDir, "archive")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		return CompactOutput{}, fmt.Errorf("compact: mkdir archive: %w", err)
	}
	entries, err := os.ReadDir(eventsDir)
	if err != nil && !os.IsNotExist(err) {
		return CompactOutput{}, fmt.Errorf("compact: read events: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		date, ok := parseShardDate(e.Name())
		if !ok || !date.Before(cutoff) {
			continue
		}
		src := filepath.Join(eventsDir, e.Name())
		info, err := os.Stat(src)
		if err != nil {
			continue
		}
		dst := filepath.Join(archive, e.Name())
		if err := os.Rename(src, dst); err != nil {
			return CompactOutput{}, fmt.Errorf("compact: archive %s: %w", e.Name(), err)
		}
		out.ShardsArchived++
		out.BytesSaved += info.Size()
	}

	// Prune resolved operations.
	opsDir := filepath.Join(tasksDir, "operations")
	opEntries, err := os.ReadDir(opsDir)
	if err != nil && !os.IsNotExist(err) {
		return CompactOutput{}, fmt.Errorf("compact: read operations: %w", err)
	}
	for _, e := range opEntries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(opsDir, e.Name())
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoff) {
			continue
		}
		// Best-effort: read the file to confirm status=completed.
		body, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if !strings.Contains(string(body), `"status":"completed"`) {
			continue
		}
		if err := os.Remove(path); err != nil {
			return CompactOutput{}, fmt.Errorf("compact: remove %s: %w", e.Name(), err)
		}
		out.OpsPruned++
		out.BytesSaved += info.Size()
	}

	// Garbage-collect aliases. Alias entries whose target ID no
	// longer exists in the store can be dropped — they exist only as
	// historical breadcrumbs and add noise once the original is gone.
	if d.Store != nil {
		aliasesPruned, err := pruneStaleAliases(ctx, d, cutoff)
		if err != nil {
			return CompactOutput{}, err
		}
		out.AliasesPruned = aliasesPruned
	}

	return out, nil
}

// resolveCutoff parses in.Before or falls back to clock.Now()-90 days.
func resolveCutoff(d Deps, before string) (time.Time, error) {
	if before == "" {
		return clockNow(d).Add(-defaultCompactWindow), nil
	}
	t, err := time.Parse("2006-01-02", before)
	if err != nil {
		t, err = time.Parse(time.RFC3339, before)
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("compact: parse before %q: %w", before, domain.ErrUsage)
	}
	return t.UTC(), nil
}

// parseShardDate extracts the date encoded in a "YYYY-MM-DD.jsonl"
// shard filename. Returns (zero, false) on any other shape.
func parseShardDate(name string) (time.Time, bool) {
	stem := strings.TrimSuffix(name, ".jsonl")
	t, err := time.Parse("2006-01-02", stem)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// pruneStaleAliases drops alias entries on each item whose target ID
// is no longer present in the store. Mutated items are written back
// via Store.Put with optimistic concurrency.
func pruneStaleAliases(ctx context.Context, d Deps, cutoff time.Time) (int, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return 0, fmt.Errorf("compact: list: %w", err)
	}
	live := map[string]struct{}{}
	for _, it := range items {
		live[it.ID] = struct{}{}
	}
	count := 0
	for _, it := range items {
		if len(it.Aliases) == 0 {
			continue
		}
		if it.UpdatedAt.After(cutoff) {
			// Recently touched — leave aliases for safety; compaction
			// is intentionally conservative.
			continue
		}
		kept := it.Aliases[:0]
		dropped := false
		for _, a := range it.Aliases {
			if _, ok := live[a]; ok {
				kept = append(kept, a)
			} else {
				dropped = true
			}
		}
		if !dropped {
			continue
		}
		if len(kept) == 0 {
			it.Aliases = nil
		} else {
			it.Aliases = kept
		}
		it.Version++
		it.UpdatedAt = clockNow(d)
		if _, err := d.Store.Put(ctx, it, it.Version-1); err != nil {
			return count, fmt.Errorf("compact: prune aliases %s: %w", it.ID, err)
		}
		count++
	}
	return count, nil
}
