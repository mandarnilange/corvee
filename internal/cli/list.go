package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newListCmd builds `corvee list`. Filters are AND-combined per §14.2.
func newListCmd(deps Deps) *cobra.Command {
	var (
		statuses   []string
		types      []string
		kinds      []string
		project    string
		parent     string
		assignee   string
		tags       []string
		caps       []string
		unblocked  bool
		unassigned bool
		limit      int
		sort       string
		order      string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List items matching filters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			filter := domain.ListFilter{
				ProjectID:    project,
				ParentID:     parent,
				Assignee:     assignee,
				Tags:         tags,
				Capabilities: caps,
				Unblocked:    unblocked,
				Unassigned:   unassigned,
				Limit:        limit,
				Sort:         sort,
				Order:        order,
			}
			for _, s := range statuses {
				filter.Statuses = append(filter.Statuses, domain.Status(s))
			}
			for _, t := range types {
				filter.Types = append(filter.Types, domain.ItemType(t))
			}
			for _, k := range kinds {
				filter.Kinds = append(filter.Kinds, domain.Kind(k))
			}
			out, err := usecase.List(cmd.Context(), deps.Usecase, usecase.ListInput{Filter: filter})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().StringSliceVar(&statuses, "status", nil, "Status filter (repeatable)")
	cmd.Flags().StringSliceVar(&types, "type", nil, "Type filter (repeatable)")
	cmd.Flags().StringSliceVar(&kinds, "kind", nil, "Kind filter (repeatable)")
	cmd.Flags().StringVar(&project, "project", "", "Restrict to a project ID")
	cmd.Flags().StringVar(&parent, "parent", "", "Restrict to direct children of this parent")
	cmd.Flags().StringVar(&assignee, "assignee", "", "Restrict to items currently claimed by this agent")
	cmd.Flags().StringSliceVar(&tags, "tag", nil, "Required tag (all must be present; repeatable)")
	cmd.Flags().StringSliceVar(&caps, "match-capabilities", nil, "Match items whose required_capabilities are a subset of these")
	cmd.Flags().BoolVar(&unblocked, "unblocked", false, "Only items whose dependencies are all done")
	cmd.Flags().BoolVar(&unassigned, "unassigned", false, "Only items with no active claim")
	cmd.Flags().IntVar(&limit, "limit", 0, "Cap result count (0 = unlimited)")
	cmd.Flags().StringVar(&sort, "sort", "", "Sort key: priority|created_at|updated_at|due_date")
	cmd.Flags().StringVar(&order, "order", "", "Order: asc (default) | desc")
	return cmd
}
