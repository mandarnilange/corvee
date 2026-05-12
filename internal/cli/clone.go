package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newCloneCmd builds `corvee clone <source> --parent <parent>`.
func newCloneCmd(deps Deps) *cobra.Command {
	var (
		parent string
		title  string
	)
	cmd := &cobra.Command{
		Use:   "clone <source-id>",
		Short: "Duplicate an item under a new parent (no children)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Clone(cmd.Context(), deps.Usecase, usecase.CloneInput{
				SourceID:      args[0],
				ParentID:      parent,
				TitleOverride: title,
				Agent:         resolveAgent(deps),
				AgentRole:     resolveAgentRole(deps),
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "Parent ID to attach the clone under (required)")
	cmd.Flags().StringVar(&title, "title", "", "Override the cloned title")
	return cmd
}
