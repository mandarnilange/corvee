package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newAddCmd builds the `corvee add` subcommand. Usage:
//
//	task add <type> [flags]
//
// where <type> is one of project|epic|story|subtask. The required
// flags vary by type (--project for projects, --parent for others).
func newAddCmd(deps Deps) *cobra.Command {
	var (
		projectCode    string
		parentID       string
		title          string
		description    string
		kind           string
		priority       string
		tags           []string
		tagsAlias      []string
		acceptance     []string
		dependencies   []string
		capabilities   []string
		estimatedHours float64
		dueDate        string
	)
	cmd := &cobra.Command{
		Use:   "add <type>",
		Short: "Add a new project/epic/story/subtask",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			itype := domain.ItemType(args[0])
			if !itype.IsValid() {
				return fmt.Errorf("add: unknown type %q (want project|epic|story|subtask): %w", args[0], domain.ErrUsage)
			}
			// Merge --tag and --tags into one slice. Both flags
			// accept CSV (cobra's StringSliceVar splits commas).
			merged := append([]string{}, tags...)
			merged = append(merged, tagsAlias...)
			input := usecase.AddInput{
				Type:                 itype,
				ProjectCode:          projectCode,
				ParentID:             parentID,
				Title:                title,
				Description:          description,
				Kind:                 domain.Kind(kind),
				Priority:             domain.Priority(priority),
				Tags:                 merged,
				AcceptanceCriteria:   acceptance,
				Dependencies:         dependencies,
				RequiredCapabilities: capabilities,
				EstimatedHours:       estimatedHours,
				Agent:                resolveAgent(deps),
				AgentRole:            resolveAgentRole(deps),
			}
			if dueDate != "" {
				ts, err := time.Parse(time.RFC3339, dueDate)
				if err != nil {
					return fmt.Errorf("add: --due %q must be RFC3339: %w", dueDate, domain.ErrUsage)
				}
				input.DueDate = &ts
			}
			out, err := usecase.Add(cmd.Context(), deps.Usecase, input)
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringVar(&projectCode, "project", "", "Project code (3-6 uppercase letters; required for type=project)")
	cmd.Flags().StringVar(&parentID, "parent", "", "Parent ID (required for non-project types)")
	cmd.Flags().StringVar(&title, "title", "", "Item title (required)")
	cmd.Flags().StringVar(&description, "description", "", "Long-form description")
	cmd.Flags().StringVar(&kind, "kind", "", "Kind: feature|bug|chore|spike|docs|refactor")
	cmd.Flags().StringVar(&priority, "priority", "", "Priority: critical|high|medium|low")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Tag (repeatable; also accepts CSV: --tag a,b,c)")
	cmd.Flags().StringSliceVar(&tagsAlias, "tags", nil, "Alias for --tag (CSV or repeatable)")
	cmd.Flags().StringSliceVar(&acceptance, "acceptance", nil, "Acceptance criterion (repeatable)")
	cmd.Flags().StringSliceVar(&dependencies, "depends-on", nil, "Dependency item ID (repeatable)")
	cmd.Flags().StringSliceVar(&capabilities, "requires", nil, "Required capability (repeatable)")
	cmd.Flags().Float64Var(&estimatedHours, "hours", 0, "Estimated hours")
	cmd.Flags().StringVar(&dueDate, "due", "", "Due date (RFC3339, e.g. 2026-12-31T00:00:00Z)")
	return cmd
}
