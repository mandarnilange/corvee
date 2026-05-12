package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newMoveCmd builds `corvee move <source-id> <target-parent-id>`.
// Relocates the item and its entire subtree under the new parent,
// computing new IDs, updating aliases, and rewriting cross-references.
// The operation is journalled via OpJournal for crash-safety.
func newMoveCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <source-id> <target-parent-id>",
		Short: "Move an item (and its subtree) to a new parent",
		Long: `Moves <source-id> and all of its descendants under <target-parent-id>.
New IDs are computed by the standard sibling-numbering rule. Old IDs are
preserved in each item's aliases[] field. Cross-references (dependencies,
blocks) are rewritten across the workspace. The operation is crash-safe via
the operation journal.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Move(cmd.Context(), deps.Usecase, usecase.MoveInput{
				SourceID:       args[0],
				TargetParentID: args[1],
				Agent:          resolveAgent(deps),
				AgentRole:      resolveAgentRole(deps),
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	return cmd
}
