package cli

import (
	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
)

// versionPayload is the data block of the success envelope for `corvee version`.
type versionPayload struct {
	Version string `json:"version"`
}

// newVersionCmd builds the `corvee version` subcommand. Phase 0's only verb;
// it exists to prove the CLI plumbing (cobra, exit-code mapping, JSON
// envelope) before any business logic ships.
func newVersionCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the binary's version string",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return printJSON(deps.Stdout, deps.format(), versionPayload{Version: domain.Version})
		},
	}
}
