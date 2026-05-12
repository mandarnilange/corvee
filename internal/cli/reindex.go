package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newReindexCmd builds `corvee reindex` to rebuild .tasks/index.json.
func newReindexCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "reindex",
		Short: "Rebuild the workspace index cache",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Reindex(cmd.Context(), deps.Usecase, usecase.ReindexInput{})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
}
