package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newTreeCmd builds `corvee tree [--root ID] [--max-depth N]`.
func newTreeCmd(deps Deps) *cobra.Command {
	var (
		root     string
		maxDepth int
	)
	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Render the workspace hierarchy as nested JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Tree(cmd.Context(), deps.Usecase, usecase.TreeInput{
				Root: root, MaxDepth: maxDepth,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&root, "root", "", "Render only the subtree rooted at this ID")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 0, "Cap recursion depth (0 = unlimited)")
	return cmd
}
