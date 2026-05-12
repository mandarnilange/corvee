package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newReleaseCmd builds `corvee release <id> --lease-id <lease>`.
func newReleaseCmd(deps Deps) *cobra.Command {
	var (
		leaseID      string
		resetToReady bool
		lockTimeout  time.Duration
	)
	cmd := &cobra.Command{
		Use:   "release <id>",
		Short: "Release a held lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Release(cmd.Context(), deps.Usecase, usecase.ReleaseInput{
				ID:           args[0],
				LeaseID:      leaseID,
				Agent:        resolveAgent(deps),
				ResetToReady: resetToReady,
				LockTimeout:  lockTimeout,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&leaseID, "lease-id", "", "Lease ID to release (required)")
	cmd.Flags().BoolVar(&resetToReady, "reset-to-ready", false, "Reset Status to ready (otherwise drops to blocked)")
	cmd.Flags().DurationVar(&lockTimeout, "lock-timeout", time.Second, "Per-item lock acquisition timeout")
	return cmd
}
