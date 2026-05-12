package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/mandarnilange/corvee/internal/domain"
)

// SyncInput is the request payload for Sync.
type SyncInput struct {
	// AutoCommit, when true, runs `git add .tasks/` + `git commit`
	// before the pull/push. Mirrors workspace.git.auto_commit so the
	// CLI can override per-invocation.
	AutoCommit bool
	// CommitMessage is the message to use when AutoCommit triggers a
	// commit. Empty defaults to "task: sync".
	CommitMessage string
	// Agent is the agent ID stamped into merge events.
	Agent string
}

// SyncOutput is the response payload for Sync.
type SyncOutput struct {
	// Pulled is true when PullRebase succeeded (no conflicts).
	Pulled bool `json:"pulled"`
	// Pushed is true when Push succeeded.
	Pushed bool `json:"pushed"`
	// Merged is the per-task merge summary when conflicts surfaced.
	Merged *MergeOutput `json:"merged,omitempty"`
}

// Sync implements `corvee sync`: pull --rebase, then push. On conflict,
// dispatches to Merge for per-task resolution and continues the
// rebase.
//
// auto_sync_seconds is intentionally ignored here — that knob drives
// `corvee sync --watch` (TTR-E07-S08), not the one-shot path.
func Sync(ctx context.Context, d Deps, in SyncInput) (SyncOutput, error) {
	if d.GitClient == nil {
		return SyncOutput{}, fmt.Errorf("sync: git client not wired: %w", domain.ErrUsage)
	}

	if in.AutoCommit {
		stagePath, err := workspaceStagePath(d)
		if err != nil {
			return SyncOutput{}, err
		}
		if err := d.GitClient.Add(stagePath); err != nil {
			return SyncOutput{}, fmt.Errorf("sync: stage %s: %w", stagePath, err)
		}
		msg := in.CommitMessage
		if msg == "" {
			msg = "task: sync"
		}
		if err := d.GitClient.Commit(msg); err != nil {
			return SyncOutput{}, fmt.Errorf("sync: commit: %w", err)
		}
	}

	out := SyncOutput{}

	pullErr := d.GitClient.PullRebase()
	if pullErr == nil {
		out.Pulled = true
	} else if errors.Is(pullErr, domain.ErrGitConflict) {
		mergeOut, mergeErr := Merge(ctx, d, MergeInput{Agent: in.Agent})
		if mergeErr != nil {
			abortAfterFailure(d, "merge", mergeErr)
			return SyncOutput{}, fmt.Errorf("sync: merge: %w", mergeErr)
		}
		out.Merged = &mergeOut
		// Stage merged files and continue the rebase.
		if len(mergeOut.ResolvedPaths) > 0 {
			if err := d.GitClient.Add(mergeOut.ResolvedPaths...); err != nil {
				abortAfterFailure(d, "stage merged", err)
				return SyncOutput{}, fmt.Errorf("sync: stage merged: %w", err)
			}
		}
		if err := d.GitClient.RebaseContinue(); err != nil {
			abortAfterFailure(d, "rebase --continue", err)
			return SyncOutput{}, fmt.Errorf("sync: rebase --continue: %w", err)
		}
		out.Pulled = true
	} else {
		return SyncOutput{}, fmt.Errorf("sync: pull --rebase: %w", pullErr)
	}

	if err := d.GitClient.Push(); err != nil {
		return out, fmt.Errorf("sync: push: %w", err)
	}
	out.Pushed = true
	return out, nil
}

// abortAfterFailure runs `git rebase --abort` and surfaces the abort
// error via slog rather than swallowing it. The original failure is
// the user-visible error; the abort failure (if any) is a secondary
// concern that still deserves to be visible because it leaves the
// working tree in `.git/rebase-apply/` and the next sync will fail
// with a confusing "rebase already in progress" message.
func abortAfterFailure(d Deps, phase string, primary error) {
	if abortErr := d.GitClient.RebaseAbort(); abortErr != nil {
		slog.Warn("sync: rebase --abort failed after error",
			slog.String("phase", phase),
			slog.String("primary_error", primary.Error()),
			slog.String("abort_error", abortErr.Error()))
	}
}

// workspaceStagePath returns the repository-relative path of the
// .tasks/ workspace, suitable for `git add`. Falls back to ".tasks"
// when neither d.TasksDir nor d.GitClient.RepoRoot is wired (the
// older single-test paths still work).
func workspaceStagePath(d Deps) (string, error) {
	if d.TasksDir == "" || d.GitClient == nil {
		return ".tasks", nil
	}
	root, err := d.GitClient.RepoRoot()
	if err != nil {
		// Outside a repo, the workspace path doesn't matter — Push
		// will fail upstream and the user gets a meaningful error.
		return ".tasks", nil //nolint:nilerr // intentional fallback
	}
	rel, err := filepath.Rel(root, d.TasksDir)
	if err != nil || rel == "" {
		return ".tasks", nil //nolint:nilerr // safe fallback
	}
	return rel, nil
}
