package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newExportPlanCmd wires `corvee export-plan [--project <CODE>]
// [--format <FORMAT>] [--out <PATH>]` per spec §15.2. Default format
// is "native"; default --out is stdout.
func newExportPlanCmd(deps Deps) *cobra.Command {
	var (
		project string
		format  string
		out     string
	)
	cmd := &cobra.Command{
		Use:   "export-plan",
		Short: "Export the workspace as a planning JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			res, err := usecase.ExportPlan(cmd.Context(), deps.Usecase, usecase.ExportPlanInput{
				Format:      format,
				ProjectCode: project,
			})
			if err != nil {
				return err
			}
			if out != "" {
				if writeErr := os.WriteFile(out, res.Body, 0o600); writeErr != nil {
					return fmt.Errorf("export-plan: write %s: %w", out, writeErr)
				}
			} else {
				if _, writeErr := deps.Stdout.Write(res.Body); writeErr != nil {
					return fmt.Errorf("export-plan: write stdout: %w", writeErr)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "Restrict to one project code")
	cmd.Flags().StringVar(&format, "format", "native", "native | phases | markdown")
	cmd.Flags().StringVar(&out, "out", "", "Output path (default stdout)")
	return cmd
}
