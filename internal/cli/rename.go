package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newRenameCmd builds `corvee rename <source-id> <target-id>`.
// Gives the item an explicit new ID, cascading to all descendants.
// Refuses if the target ID already exists.
func newRenameCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <source-id> <target-id>",
		Short: "Rename an item (and its subtree) to an explicit new ID",
		Long: `Renames <source-id> to <target-id>, cascading child IDs via the
same prefix-swap approach as move. The item's parent and project are unchanged.
Fails with an integrity error if <target-id> already exists. Old IDs are
preserved in each item's aliases[] field.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Rename(cmd.Context(), deps.Usecase, usecase.RenameInput{
				SourceID:  args[0],
				TargetID:  args[1],
				Agent:     resolveAgent(deps),
				AgentRole: resolveAgentRole(deps),
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	return cmd
}
