package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newInitCmd builds the `corvee init` subcommand. Bootstraps a workspace
// at the resolved TasksDir, writing workspace.json + local.json + the
// directory tree per spec §3.
func newInitCmd(deps Deps) *cobra.Command {
	var (
		workspaceName  string
		defaultProject string
		agentID        string
		force          bool
	)
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Bootstrap a workspace in the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Init(cmd.Context(), deps.Usecase, usecase.InitInput{
				TasksDir:       deps.TasksDir,
				WorkspaceName:  workspaceName,
				DefaultProject: defaultProject,
				AgentID:        agentID,
				Force:          force,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&workspaceName, "name", "", "Workspace name (required)")
	cmd.Flags().StringVar(&defaultProject, "project", "", "Default project code")
	cmd.Flags().StringVar(&agentID, "agent-id", "", "Override default_agent for new local.json")
	cmd.Flags().BoolVar(&force, "force", false, "Re-init existing workspace (preserves local.json)")
	return cmd
}
