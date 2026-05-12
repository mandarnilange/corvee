package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newDeleteCmd builds `corvee delete <id>`. Phase 1 only supports
// soft-delete (sets Status=abandoned). --hard / --cascade / --force
// route through to the usecase, which rejects them with ErrUsage.
func newDeleteCmd(deps Deps) *cobra.Command {
	var (
		expectVersion int
		hard          bool
		cascade       bool
		force         bool
	)
	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Soft-delete an item (sets status=abandoned)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Delete(cmd.Context(), deps.Usecase, usecase.DeleteInput{
				ID:            args[0],
				ExpectVersion: expectVersion,
				Hard:          hard,
				Cascade:       cascade,
				Force:         force,
				Agent:         resolveAgent(deps),
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().IntVar(&expectVersion, "expect-version", -1, "Optimistic concurrency: expected version (-1 = no check)")
	cmd.Flags().BoolVar(&hard, "hard", false, "Phase 4+: hard delete the file (rejected in Phase 1)")
	cmd.Flags().BoolVar(&cascade, "cascade", false, "Phase 4+: also delete descendants (rejected in Phase 1)")
	cmd.Flags().BoolVar(&force, "force", false, "Phase 4+: bypass dependent guard (rejected in Phase 1)")
	return cmd
}
