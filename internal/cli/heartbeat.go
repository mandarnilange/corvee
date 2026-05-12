package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newHeartbeatCmd builds `corvee heartbeat <id>`.
func newHeartbeatCmd(deps Deps) *cobra.Command {
	var (
		leaseID    string
		ttlMinutes int
		advance    bool
		lockTO     time.Duration
	)
	cmd := &cobra.Command{
		Use:   "heartbeat <id>",
		Short: "Extend a held lease's expiry",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Heartbeat(cmd.Context(), deps.Usecase, usecase.HeartbeatInput{
				ID:                  args[0],
				LeaseID:             leaseID,
				Agent:               resolveAgent(deps),
				TTL:                 time.Duration(ttlMinutes) * time.Minute,
				LockTimeout:         lockTO,
				AdvanceToInProgress: advance,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&leaseID, "lease-id", "", "Lease ID being refreshed (required)")
	cmd.Flags().IntVar(&ttlMinutes, "ttl-minutes", 0, "Extend by this many minutes (0 = workspace default)")
	cmd.Flags().BoolVar(&advance, "advance-to-in-progress", false, "Also advance status from claimed to in_progress")
	cmd.Flags().DurationVar(&lockTO, "lock-timeout", time.Second, "Per-item lock acquisition timeout")
	return cmd
}
