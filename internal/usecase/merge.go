package usecase

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mandarnilange/corvee/internal/domain"
)

// MergeInput is the request payload for Merge.
type MergeInput struct {
	// Agent stamps merge events.
	Agent string
}

// MergeOutput is the response payload for Merge.
type MergeOutput struct {
	// Merged is the count of items resolved automatically by the
	// 3-way merge.
	Merged int `json:"merged"`
	// ClaimLost lists items where a divergent claim was resolved by
	// the lower-lease-id rule. The losing agent will see ErrClaimLost
	// on its next operation against the item.
	ClaimLost []string `json:"claim_lost,omitempty"`
	// ProseConflicts lists items where description / acceptance
	// criteria diverged and a .conflict file was written for human
	// review.
	ProseConflicts []string `json:"prose_conflicts,omitempty"`
	// ResolvedPaths is the repository-relative path list the caller
	// must `git add` before continuing the rebase. Includes both
	// auto-merged JSON files and the .conflict markers.
	ResolvedPaths []string `json:"resolved_paths,omitempty"`
}

// Merge resolves the per-task JSON conflicts surfaced by Sync's
// PullRebase. For each conflicted file:
//
//   - Items: parse three sides (BASE/HEAD/MERGE_HEAD) and apply the
//     deterministic 3-way merge from spec §6 Layer 8 (claims) and
//     §6 Layer 4 (status/journal/tags). Prose conflicts surface a
//     .conflict file; the merged item is still written.
//   - Events shards (.tasks/events/*.jsonl): union all lines from
//     both sides, dedupe by event_id, sort by (UnixNano, event_id).
//   - Anything else: fail (callers don't expect non-data conflicts).
//
// The Merge usecase reads conflict-stage blobs through GitClient
// (a domain port) and writes resolved bytes back via os.WriteFile
// rooted at GitClient.RepoRoot(). This is the one usecase that
// touches the filesystem outside the Store — necessary because the
// conflicted artefacts are events shards and merge-only .conflict
// files that no other adapter knows how to address.
func Merge(ctx context.Context, d Deps, in MergeInput) (MergeOutput, error) {
	if d.GitClient == nil {
		return MergeOutput{}, fmt.Errorf("merge: git client not wired: %w", domain.ErrUsage)
	}
	conflicts, err := d.GitClient.ConflictedFiles()
	if err != nil {
		return MergeOutput{}, fmt.Errorf("merge: list conflicts: %w", err)
	}
	out := MergeOutput{}
	for _, p := range conflicts {
		switch {
		case strings.HasSuffix(p, ".json") && strings.Contains(p, "/items/"):
			res, mergeErr := mergeItemFile(ctx, d, in, p)
			if mergeErr != nil {
				return MergeOutput{}, mergeErr
			}
			out.Merged++
			if res.claimLostID != "" {
				out.ClaimLost = append(out.ClaimLost, res.claimLostID)
			}
			if res.proseConflict {
				out.ProseConflicts = append(out.ProseConflicts, res.itemID)
				out.ResolvedPaths = append(out.ResolvedPaths, p+".conflict")
			}
			out.ResolvedPaths = append(out.ResolvedPaths, p)
		case strings.HasSuffix(p, ".jsonl") && strings.Contains(p, "/events/"):
			if mergeErr := mergeEventsShard(d, p); mergeErr != nil {
				return MergeOutput{}, mergeErr
			}
			out.ResolvedPaths = append(out.ResolvedPaths, p)
		default:
			return MergeOutput{}, fmt.Errorf("merge: unsupported conflict path %q: %w", p, domain.ErrIntegrityViolated)
		}
	}
	return out, nil
}

// itemMergeResult captures the per-file outcome surfaced by Merge.
type itemMergeResult struct {
	itemID        string
	claimLostID   string
	proseConflict bool
}

// mergeItemFile resolves a conflicted item JSON via 3-way merge,
// writes the merged item to the working tree, and emits a claim_lost
// event when claims diverged.
func mergeItemFile(ctx context.Context, d Deps, in MergeInput, path string) (itemMergeResult, error) {
	if err := ctx.Err(); err != nil {
		return itemMergeResult{}, err
	}
	res := itemMergeResult{}
	base, baseErr := readSide(d.GitClient, ":1", path)
	ours, oursErr := readSide(d.GitClient, ":2", path)
	theirs, theirsErr := readSide(d.GitClient, ":3", path)

	// Base may legitimately not exist (added on both sides). Ours/theirs must.
	if oursErr != nil || theirsErr != nil {
		return res, fmt.Errorf("merge %s: read sides: ours=%v theirs=%v: %w",
			path, oursErr, theirsErr, domain.ErrIntegrityViolated)
	}

	var baseItem *domain.Item
	if baseErr == nil {
		bi, err := parseItem(base)
		if err != nil {
			return res, fmt.Errorf("merge %s: parse base: %w", path, err)
		}
		baseItem = &bi
	}
	oursItem, err := parseItem(ours)
	if err != nil {
		return res, fmt.Errorf("merge %s: parse ours: %w", path, err)
	}
	theirsItem, err := parseItem(theirs)
	if err != nil {
		return res, fmt.Errorf("merge %s: parse theirs: %w", path, err)
	}

	merged, claimLostAgent, proseConflict := MergeItem3Way(baseItem, oursItem, theirsItem)
	res.itemID = merged.ID
	res.proseConflict = proseConflict

	// Write merged item back to the file's absolute path on disk.
	abs, err := absPath(d, path)
	if err != nil {
		return res, err
	}
	body, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return res, fmt.Errorf("merge %s: marshal: %w", path, err)
	}
	body = append(body, '\n')
	if writeErr := os.WriteFile(abs, body, 0o600); writeErr != nil {
		return res, fmt.Errorf("merge %s: write: %w", path, writeErr)
	}

	if proseConflict {
		conflictBody := buildConflictBlob(oursItem, theirsItem)
		if writeErr := os.WriteFile(abs+".conflict", conflictBody, 0o600); writeErr != nil {
			return res, fmt.Errorf("merge %s: write conflict marker: %w", path, writeErr)
		}
	}

	if claimLostAgent != "" {
		res.claimLostID = merged.ID
		now := d.Clock.Now()
		auditAppend(d, domain.Event{
			EventID:   nextEventID(d),
			Timestamp: now,
			Type:      "claim_lost",
			ItemID:    merged.ID,
			Actor:     in.Agent,
			Metadata: map[string]any{
				"loser_agent": claimLostAgent,
			},
		})
	}

	return res, nil
}

// readSide reads one of the three rebase stages for a conflicted
// path. Stage 1 = common ancestor (base), 2 = ours (current branch),
// 3 = theirs (incoming). Wraps ErrNotFound when the side is absent.
func readSide(g domain.GitClient, stage, path string) ([]byte, error) {
	return g.ShowAtRev(stage, path)
}

// parseItem deserializes an Item JSON blob. Wraps the unmarshal
// error with ErrMalformedJSON via errors.Join so callers can branch
// on errors.Is(err, ErrMalformedJSON) without losing the underlying
// detail.
func parseItem(body []byte) (domain.Item, error) {
	var it domain.Item
	if err := json.Unmarshal(body, &it); err != nil {
		return domain.Item{}, fmt.Errorf("parse item: %w", errors.Join(domain.ErrMalformedJSON, err))
	}
	return it, nil
}

// absPath converts a repository-relative path (as returned by `git
// diff --name-only`) to an absolute one rooted at the git toplevel.
// We deliberately do NOT use os.Getwd() — `corvee` may be invoked from
// a subdirectory, in which case cwd ≠ repo root and the merged file
// would land in the wrong place.
func absPath(d Deps, rel string) (string, error) {
	if d.GitClient == nil {
		return "", fmt.Errorf("merge: git client not wired: %w", domain.ErrUsage)
	}
	root, err := d.GitClient.RepoRoot()
	if err != nil {
		return "", fmt.Errorf("merge: resolve repo root: %w", err)
	}
	return filepath.Join(root, rel), nil
}

// mergeEventsShard performs the trivial union-by-event_id-sort-by-ts
// merge for an events shard, then writes the result back. This works
// even with conflict markers (`<<<<<<<`) in the file because we only
// keep lines that parse as a JSON event with a non-empty event_id.
//
// Memory: streams the file via bufio.Scanner so a high-activity
// shard with millions of lines doesn't load all-at-once.
func mergeEventsShard(d Deps, path string) error {
	abs, err := absPath(d, path)
	if err != nil {
		return err
	}
	f, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("merge %s: open: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	type evLine struct {
		// nano is the full timestamp resolution (UnixNano) so two
		// events less than a millisecond apart still order
		// deterministically.
		nano int64
		// id is the secondary sort key, ensuring a total order even
		// when nano collides (same instant on the same machine).
		id string
		ln string
	}
	seen := map[string]struct{}{}
	lines := []evLine{}
	scanner := bufio.NewScanner(f)
	// Allow long lines — handoff metadata payloads can be hefty.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		l := strings.TrimSpace(scanner.Text())
		if l == "" {
			continue
		}
		// Skip git conflict markers.
		if strings.HasPrefix(l, "<<<<<<<") || strings.HasPrefix(l, "=======") ||
			strings.HasPrefix(l, ">>>>>>>") || strings.HasPrefix(l, "|||||||") {
			continue
		}
		var ev domain.Event
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			continue
		}
		if ev.EventID == "" {
			continue
		}
		if _, ok := seen[ev.EventID]; ok {
			continue
		}
		seen[ev.EventID] = struct{}{}
		lines = append(lines, evLine{
			nano: ev.Timestamp.UnixNano(),
			id:   ev.EventID,
			ln:   l,
		})
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return fmt.Errorf("merge %s: scan: %w", path, scanErr)
	}
	sort.Slice(lines, func(i, j int) bool {
		if lines[i].nano != lines[j].nano {
			return lines[i].nano < lines[j].nano
		}
		return lines[i].id < lines[j].id
	})
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(l.ln)
		b.WriteByte('\n')
	}
	if writeErr := os.WriteFile(abs, []byte(b.String()), 0o600); writeErr != nil {
		return fmt.Errorf("merge %s: write: %w", path, writeErr)
	}
	return nil
}

// buildConflictBlob renders a human-readable side-by-side payload for
// the prose fields that diverged. Format is intentionally simple
// (key: ours / theirs) so reviewers can paste it into PR comments.
func buildConflictBlob(ours, theirs domain.Item) []byte {
	var b strings.Builder
	b.WriteString("# Prose conflict — resolve manually\n\n")
	if ours.Description != theirs.Description {
		b.WriteString("## description\n")
		b.WriteString("--- ours\n")
		b.WriteString(ours.Description)
		b.WriteString("\n--- theirs\n")
		b.WriteString(theirs.Description)
		b.WriteString("\n")
	}
	if !equalStrings(ours.AcceptanceCriteria, theirs.AcceptanceCriteria) {
		b.WriteString("## acceptance_criteria\n")
		b.WriteString("--- ours\n")
		for _, a := range ours.AcceptanceCriteria {
			b.WriteString("- ")
			b.WriteString(a)
			b.WriteString("\n")
		}
		b.WriteString("--- theirs\n")
		for _, a := range theirs.AcceptanceCriteria {
			b.WriteString("- ")
			b.WriteString(a)
			b.WriteString("\n")
		}
	}
	return []byte(b.String())
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
