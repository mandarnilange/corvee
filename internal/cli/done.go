package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newDoneCmd builds `corvee done <id>`. Optional --note appends a
// completion journal entry. --lease-id is REQUIRED in Phase 3+ when
// the item is currently claimed; the usecase enforces the rule
// (empty/wrong lease returns ErrLeaseMismatch via exit code 5).
func newDoneCmd(deps Deps) *cobra.Command {
	var (
		expectVersion int
		note          string
		leaseID       string
		lockTimeout   time.Duration
	)
	cmd := &cobra.Command{
		Use:   "done <id>",
		Short: "Mark an item as done",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := usecase.Done(cmd.Context(), deps.Usecase, usecase.DoneInput{
				ID:            args[0],
				ExpectVersion: expectVersion,
				Note:          note,
				Agent:         resolveAgent(deps),
				LeaseID:       leaseID,
				LockTimeout:   lockTimeout,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().IntVar(&expectVersion, "expect-version", -1, "Optimistic concurrency: expected version (-1 = no check)")
	cmd.Flags().StringVar(&note, "note", "", "Annotation appended to the completion journal entry")
	cmd.Flags().StringVar(&leaseID, "lease-id", "", "Required when the item is currently claimed (Phase 3+)")
	cmd.Flags().DurationVar(&lockTimeout, "lock-timeout", time.Second, "Per-item lock acquisition timeout")
	return cmd
}
