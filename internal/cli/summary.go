package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newSummaryCmd builds `corvee summary [--project P]`.
func newSummaryCmd(deps Deps) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Roll up status counts, deliverables, and critical-path progress",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Summary(cmd.Context(), deps.Usecase, usecase.SummaryInput{
				ProjectID: project,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Restrict to a single project")
	return cmd
}
