package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newUpdateCmd builds `corvee update <id> [flags]`.
func newUpdateCmd(deps Deps) *cobra.Command {
	var (
		expectVersion int
		status        string
		priority      string
		title         string
		description   string
		dueDate       string
		risk          string
		addTags       []string
		removeTags    []string
		addImpact     []string
		removeImpact  []string
		addDeps       []string
		removeDeps    []string
		addAccept     []string
		removeAccept  []string
		note          string
	)
	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Apply field mutations to an existing item",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := usecase.UpdateInput{
				ID:                args[0],
				ExpectVersion:     expectVersion,
				AddTags:           addTags,
				RemoveTags:        removeTags,
				AddImpactFiles:    addImpact,
				RemoveImpactFiles: removeImpact,
				AddDeps:           addDeps,
				RemoveDeps:        removeDeps,
				AddAcceptance:     addAccept,
				RemoveAcceptance:  removeAccept,
				Note:              note,
				Agent:             resolveAgent(deps),
			}
			if cmd.Flags().Changed("status") {
				s := domain.Status(status)
				input.Status = &s
			}
			if cmd.Flags().Changed("priority") {
				p := domain.Priority(priority)
				input.Priority = &p
			}
			if cmd.Flags().Changed("title") {
				input.Title = &title
			}
			if cmd.Flags().Changed("description") {
				input.Description = &description
			}
			if cmd.Flags().Changed("risk") {
				r := domain.Risk(risk)
				input.Risk = &r
			}
			if dueDate != "" {
				ts, err := time.Parse(time.RFC3339, dueDate)
				if err != nil {
					return fmt.Errorf("update: --due %q must be RFC3339: %w", dueDate, domain.ErrUsage)
				}
				input.DueDate = &ts
			}
			out, err := usecase.Update(cmd.Context(), deps.Usecase, input)
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().IntVar(&expectVersion, "expect-version", -1, "Optimistic concurrency: expected version (-1 = no check)")
	cmd.Flags().StringVar(&status, "status", "", "New status (validated against §15.2 transitions)")
	cmd.Flags().StringVar(&priority, "priority", "", "New priority")
	cmd.Flags().StringVar(&title, "title", "", "Replace title")
	cmd.Flags().StringVar(&description, "description", "", "Replace description")
	cmd.Flags().StringVar(&dueDate, "due", "", "Set due date (RFC3339)")
	cmd.Flags().StringVar(&risk, "risk", "", "Set Impact.risk")
	cmd.Flags().StringSliceVar(&addTags, "add-tag", nil, "Add tag (repeatable)")
	cmd.Flags().StringSliceVar(&removeTags, "remove-tag", nil, "Remove tag (repeatable)")
	cmd.Flags().StringSliceVar(&addImpact, "add-impact-file", nil, "Add Impact.file (repeatable)")
	cmd.Flags().StringSliceVar(&removeImpact, "remove-impact-file", nil, "Remove Impact.file (repeatable)")
	cmd.Flags().StringSliceVar(&addDeps, "add-dep", nil, "Add dependency (repeatable)")
	cmd.Flags().StringSliceVar(&removeDeps, "remove-dep", nil, "Remove dependency (repeatable)")
	cmd.Flags().StringSliceVar(&addAccept, "add-acceptance", nil, "Add acceptance criterion (repeatable)")
	cmd.Flags().StringSliceVar(&removeAccept, "remove-acceptance", nil, "Remove acceptance criterion (repeatable)")
	cmd.Flags().StringVar(&note, "note", "", "Append a journal entry with this note")
	return cmd
}
