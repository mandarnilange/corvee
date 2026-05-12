package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newRecoverCmd builds `corvee recover`.
// Reads the operation journal and rolls forward any interrupted operations.
func newRecoverCmd(deps Deps) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "recover",
		Short: "Roll forward any interrupted multi-file operations",
		Long: `Reads the operation journal (.tasks/operations/) and re-executes any
steps that were not completed before a crash. Each recovered operation is
reported in the output. Failed recoveries surface in task validate.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Recover(cmd.Context(), deps.Usecase, usecase.RecoverInput{
				DryRun: dryRun,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be recovered without making changes")
	return cmd
}
