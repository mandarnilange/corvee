package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newCompactCmd wires `corvee compact [--before <DATE>]` per spec §15.2.
// Default cutoff is 90 days; --before lets the operator pin a specific
// ISO date.
func newCompactCmd(deps Deps) *cobra.Command {
	var before string
	cmd := &cobra.Command{
		Use:   "compact",
		Short: "Squash events, archive shards, prune ops, gc aliases",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Compact(cmd.Context(), deps.Usecase, usecase.CompactInput{
				Before: before,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&before, "before", "", "ISO date cutoff (default: 90 days ago)")
	return cmd
}
