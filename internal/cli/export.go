package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newExportCmd wires `corvee export --format <FORMAT> [--out <PATH>]
// [--include <CSV>]` per spec §15.2.
func newExportCmd(deps Deps) *cobra.Command {
	var (
		format  string
		out     string
		include string
	)
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the workspace as a portable graph (graph|cypher|dot)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var includes []string
			if include != "" {
				includes = strings.Split(include, ",")
			}
			res, err := usecase.Export(cmd.Context(), deps.Usecase, usecase.ExportInput{
				Format:  format,
				Include: includes,
			})
			if err != nil {
				return err
			}
			if out != "" {
				if writeErr := os.WriteFile(out, res.Body, 0o600); writeErr != nil {
					return fmt.Errorf("export: write %s: %w", out, writeErr)
				}
			} else {
				if _, writeErr := deps.Stdout.Write(res.Body); writeErr != nil {
					return fmt.Errorf("export: write stdout: %w", writeErr)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "graph", "graph | cypher | dot")
	cmd.Flags().StringVar(&out, "out", "", "Output path (default stdout)")
	cmd.Flags().StringVar(&include, "include", "", "Edge types CSV: parent,dependency,blocks,alias")
	return cmd
}
