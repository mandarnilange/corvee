package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newValidateCmd builds `corvee validate [--strict]`.
func newValidateCmd(deps Deps) *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Run workspace integrity checks",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Validate(cmd.Context(), deps.Usecase, usecase.ValidateInput{
				FailOnIssues: strict,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "Exit non-zero when issues are found")
	return cmd
}
