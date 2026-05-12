package cli

import (
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/adapter/render"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// newRenderCmd builds `corvee render [--out <dir>] [--theme <name>]` per
// spec §15.2. The default --out is .tasks/dist/ relative to the
// resolved workspace; --theme is validated against the closed set
// returned by adapter/render.AvailableThemes (unknown name returns
// ErrUsage → exit 2).
func newRenderCmd(deps Deps) *cobra.Command {
	var (
		out   string
		theme string
	)
	cmd := &cobra.Command{
		Use:   "render",
		Short: "Generate the static HTML dashboard and deploy manifest",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			outDir := out
			if outDir == "" {
				outDir = filepath.Join(deps.TasksDir, "dist")
			}
			result, err := usecase.Render(cmd.Context(), deps.Usecase, usecase.RenderInput{
				OutDir:          outDir,
				Theme:           theme,
				AvailableThemes: render.AvailableThemes(),
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), result)
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "Output directory (default: <workspace>/dist)")
	cmd.Flags().StringVar(&theme, "theme", "", "Theme name (default: default)")
	return cmd
}
