package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newSyncCmd wires `corvee sync [--watch] [--interval <seconds>]
// [--auto-commit] [--message <msg>]` per spec §15.2 and TTR-E07-S08.
//
// One-shot: pull --rebase + push, exit 0 on success.
// Watch: long-running loop, exit 0 on clean cancellation, 130 on SIGINT.
func newSyncCmd(deps Deps) *cobra.Command {
	var (
		watch      bool
		interval   int
		autoCommit bool
		message    string
	)
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Pull --rebase + push the workspace; --watch loops",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			in := usecase.SyncInput{
				AutoCommit:    autoCommit,
				CommitMessage: message,
				Agent:         resolveAgent(deps),
			}
			if !watch {
				out, err := usecase.Sync(ctx, deps.Usecase, in)
				if err != nil {
					return err
				}
				return printJSON(deps.Stdout, deps.format(), out)
			}
			return runSyncWatch(ctx, deps, in, interval)
		},
	}
	cmd.Flags().BoolVar(&watch, "watch", false, "Long-running sync loop (Ctrl+C to exit)")
	cmd.Flags().IntVar(&interval, "interval", 0, "Override workspace.json:git.auto_sync_seconds (in seconds)")
	cmd.Flags().BoolVar(&autoCommit, "auto-commit", false, "Stage and commit .tasks/ before pulling")
	cmd.Flags().StringVar(&message, "message", "", "Commit message (with --auto-commit)")
	return cmd
}

// runSyncWatch reads the loop cadence from --interval (overrides
// config) or from workspace.git.auto_sync_seconds. ErrUsage if
// neither is set. Exits 0 on context cancellation; the parent
// signal.NotifyContext maps SIGINT to ctx.Done so the process exits
// 130 from the OS layer.
func runSyncWatch(ctx context.Context, deps Deps, in usecase.SyncInput, intervalFlag int) error {
	seconds, err := resolveSyncInterval(deps, intervalFlag)
	if err != nil {
		return err
	}
	tick := time.Duration(seconds) * time.Second
	t := time.NewTicker(tick)
	defer t.Stop()

	// Run once immediately so a short-lived watch invocation is still
	// useful (matches the user's mental model of `--watch` = "keep
	// going" rather than "wait first").
	if err := runOneTick(ctx, deps, in); err != nil {
		fmt.Fprintln(deps.Stderr, err)
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := runOneTick(ctx, deps, in); err != nil {
				fmt.Fprintln(deps.Stderr, err)
			}
		}
	}
}

func runOneTick(ctx context.Context, deps Deps, in usecase.SyncInput) error {
	out, err := usecase.Sync(ctx, deps.Usecase, in)
	if err != nil {
		return fmt.Errorf("sync tick: %w", err)
	}
	return printJSON(deps.Stdout, deps.format(), out)
}

// resolveSyncInterval picks the loop cadence per the priority chain
// from TTR-E07-S08: --interval flag wins, else workspace.git.auto_sync_seconds,
// else ErrUsage.
func resolveSyncInterval(deps Deps, intervalFlag int) (int, error) {
	if intervalFlag > 0 {
		return intervalFlag, nil
	}
	if deps.Usecase.Workspace == nil {
		return 0, fmt.Errorf("sync --watch: workspace not wired and --interval missing: %w", domain.ErrUsage)
	}
	ws, err := deps.Usecase.Workspace.Load()
	if err != nil {
		return 0, fmt.Errorf("sync --watch: load workspace: %w", err)
	}
	if ws.Git.AutoSyncSeconds == nil || *ws.Git.AutoSyncSeconds <= 0 {
		return 0, fmt.Errorf("sync --watch: workspace.json:git.auto_sync_seconds is null and --interval not given: %w", domain.ErrUsage)
	}
	return *ws.Git.AutoSyncSeconds, nil
}
