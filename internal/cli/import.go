package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newImportCmd wires `corvee import <file> [--dry-run] [--prefix <CODE>]`
// per spec §15.2. A file path of "-" reads from stdin so plans can be
// piped in.
func newImportCmd(deps Deps) *cobra.Command {
	var (
		dryRun bool
		prefix string
	)
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Bulk-create items from a planning JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			body, err := readImportBody(args[0])
			if err != nil {
				return fmt.Errorf("import: %w", err)
			}
			role := resolveAgentRole(deps)
			out, err := usecase.Import(cmd.Context(), deps.Usecase, usecase.ImportInput{
				Body:      body,
				Prefix:    prefix,
				DryRun:    dryRun,
				Agent:     resolveAgent(deps),
				AgentRole: role,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Validate and report without writing")
	cmd.Flags().StringVar(&prefix, "prefix", "", "Override project_id from input doc")
	return cmd
}

func readImportBody(path string) ([]byte, error) {
	if path == "-" {
		return io.ReadAll(os.Stdin)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, domain.ErrNotFound)
	}
	return body, nil
}
