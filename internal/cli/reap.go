package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newReapCmd builds `corvee reap` to release expired leases.
func newReapCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "reap",
		Short: "Release every expired lease",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Reap(cmd.Context(), deps.Usecase, usecase.ReapInput{})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
}
