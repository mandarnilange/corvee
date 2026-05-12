package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newClaimCmd builds `corvee claim <id>`. Mints a ULID lease and
// transitions Status to claimed.
func newClaimCmd(deps Deps) *cobra.Command {
	var (
		ttlMinutes  int
		lockTimeout time.Duration
	)
	cmd := &cobra.Command{
		Use:   "claim <id>",
		Short: "Acquire a lease on an item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			caps := agentCapabilities(deps)
			out, err := usecase.Claim(cmd.Context(), deps.Usecase, usecase.ClaimInput{
				ID:                args[0],
				Agent:             resolveAgent(deps),
				AgentRole:         resolveAgentRole(deps),
				AgentCapabilities: caps,
				TTL:               time.Duration(ttlMinutes) * time.Minute,
				LockTimeout:       lockTimeout,
				OperationID:       deps.Globals.OperationID,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().IntVar(&ttlMinutes, "ttl-minutes", 0, "Lease TTL in minutes (0 = workspace default)")
	cmd.Flags().DurationVar(&lockTimeout, "lock-timeout", time.Second, "Per-item lock acquisition timeout")
	return cmd
}

// agentCapabilities resolves the per-VM capability list, used by
// claim/next for capability-match filtering.
func agentCapabilities(deps Deps) []string {
	if deps.Usecase.Local == nil {
		return nil
	}
	local, err := deps.Usecase.Local.Load()
	if err != nil {
		return nil
	}
	return local.AgentCapabilities
}
