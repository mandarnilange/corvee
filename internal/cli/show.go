package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newShowCmd builds `corvee show <id>`.
func newShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a single item including its journal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Show(cmd.Context(), deps.Usecase, usecase.ShowInput{ID: args[0]})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
}
