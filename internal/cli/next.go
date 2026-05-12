package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newNextCmd builds `corvee next` — picks the highest-ranked claimable
// item for the agent.
func newNextCmd(deps Deps) *cobra.Command {
	var (
		project   string
		kinds     []string
		types     []string
		autoClaim bool
	)
	cmd := &cobra.Command{
		Use:   "next",
		Short: "Pick the next-best claimable item for this agent",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			in := usecase.NextInput{
				Agent:             resolveAgent(deps),
				AgentRole:         resolveAgentRole(deps),
				AgentCapabilities: agentCapabilities(deps),
				ProjectID:         project,
				AutoClaim:         autoClaim,
			}
			for _, k := range kinds {
				in.Kinds = append(in.Kinds, domain.Kind(k))
			}
			for _, ty := range types {
				in.Types = append(in.Types, domain.ItemType(ty))
			}
			out, err := usecase.Next(cmd.Context(), deps.Usecase, in)
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Restrict to a single project")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "Filter by Kind (repeatable)")
	cmd.Flags().StringSliceVar(&types, "type", nil, "Filter by ItemType (repeatable)")
	cmd.Flags().BoolVar(&autoClaim, "auto-claim", false, "Claim the chosen item before returning")
	return cmd
}
