package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/usecase"
)

// newMigrateCmd builds `corvee migrate`.
// Upgrades items whose schema_version is below the current binary's version.
func newMigrateCmd(deps Deps) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Upgrade items to the current schema version",
		Long: `Scans the workspace for items whose schema_version is below
CurrentSchemaVersion and upgrades them. Items written by a newer binary
(schema_version > current) are reported as skipped with reason="binary too old"
and are NOT modified. Idempotent: running migrate on an up-to-date workspace
is a no-op.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := usecase.Migrate(cmd.Context(), deps.Usecase, usecase.MigrateInput{
				DryRun: dryRun,
			})
			if err != nil {
				return err
			}
			return printJSON(deps.Stdout, deps.format(), out)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Report what would be migrated without making changes")
	return cmd
}
