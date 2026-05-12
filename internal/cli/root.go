package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

// Globals captures the global flags from spec §15.1 that apply to every
// command. They are populated on the root command and passed to verb
// builders via the Deps struct.
type Globals struct {
	Pretty      bool
	Text        bool
	Workspace   string
	Agent       string
	OperationID string
	Quiet       bool
}

// Deps is the dependency bundle wired by cmd/corvee/main.go.
//
// Globals + Stdout/Stderr are always populated. Usecase is the wired
// usecase.Deps for the verbs that need adapters (Store, Clock, IDGen,
// Workspace, Local). For the special init verb, Usecase.Store may be
// nil (the workspace doesn't exist yet) — init only consults the
// loaders.
type Deps struct {
	// Globals captures the per-invocation flag settings.
	Globals *Globals
	// Stdout is where verbs write success-envelope JSON.
	Stdout io.Writer
	// Stderr is where the exit-code mapper writes diagnostics.
	Stderr io.Writer
	// Usecase is the wired domain-port bundle.
	Usecase usecase.Deps
	// TasksDir is the resolved path to the .tasks/ directory for
	// this invocation. For most verbs it points at a discovered
	// workspace; for `init` it points at where one will be created.
	TasksDir string
}

// format returns the resolved output mode for the current invocation.
func (d Deps) format() outputFormat {
	switch {
	case d.Globals.Pretty:
		return outputPretty
	case d.Globals.Text:
		return outputText
	default:
		return outputCompact
	}
}

// verbsAllowedWithoutWorkspace lists every command that can run when
// no .tasks/ workspace exists. Anything else needs a workspace and
// will be rejected with ErrIntegrityViolated by the
// PersistentPreRunE guard below — instead of dereferencing a nil
// Store and panicking deep inside a usecase.
//
// Notably absent: `migrate` (lazily upgrades items inside an existing
// workspace) and `recover` (reads the ops journal of an existing
// workspace) both legitimately require the workspace and stay
// gated.
var verbsAllowedWithoutWorkspace = map[string]struct{}{
	"corvee":  {}, // root help
	"version": {},
	"init":    {},
	"help":    {},
}

// NewRootCmd builds the root cobra command, registering all subcommands.
// The returned command is suitable for cmd.ExecuteContext or for tests
// that drive the CLI via cobra directly.
func NewRootCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "corvee",
		Short:         "Local-first, multi-agent task tracker",
		Long:          "corvee coordinates work across humans and AI agents through a file-based, git-distributed backlog.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(c *cobra.Command, _ []string) error {
			// Test wiring leaves both TasksDir and Store empty; the
			// guard only applies in production (main.go always sets
			// TasksDir, leaving Store nil only when no workspace
			// exists yet).
			if deps.TasksDir == "" {
				return nil
			}
			if deps.Usecase.Store != nil {
				return nil
			}
			if _, ok := verbsAllowedWithoutWorkspace[c.Name()]; ok {
				return nil
			}
			return fmt.Errorf("no .tasks/ workspace found at %s; run `corvee init` first: %w",
				deps.TasksDir, domain.ErrIntegrityViolated)
		},
	}

	cmd.PersistentFlags().BoolVar(&deps.Globals.Pretty, "pretty", false, "Indented JSON output")
	cmd.PersistentFlags().BoolVar(&deps.Globals.Text, "text", false, "Human-readable text output instead of JSON")
	cmd.PersistentFlags().StringVar(&deps.Globals.Workspace, "workspace", "", "Override workspace auto-discovery")
	cmd.PersistentFlags().StringVar(&deps.Globals.Agent, "agent", "", "Override agent ID for this invocation")
	cmd.PersistentFlags().StringVar(&deps.Globals.OperationID, "operation-id", "", "Idempotency key (auto-minted if absent)")
	cmd.PersistentFlags().BoolVar(&deps.Globals.Quiet, "quiet", false, "Suppress non-essential output")

	cmd.AddCommand(newVersionCmd(deps))
	cmd.AddCommand(newInitCmd(deps))
	cmd.AddCommand(newAddCmd(deps))
	cmd.AddCommand(newListCmd(deps))
	cmd.AddCommand(newShowCmd(deps))
	cmd.AddCommand(newUpdateCmd(deps))
	cmd.AddCommand(newDoneCmd(deps))
	cmd.AddCommand(newDeleteCmd(deps))
	cmd.AddCommand(newTreeCmd(deps))
	cmd.AddCommand(newValidateCmd(deps))
	cmd.AddCommand(newReindexCmd(deps))
	cmd.AddCommand(newSummaryCmd(deps))
	cmd.AddCommand(newCloneCmd(deps))
	cmd.AddCommand(newClaimCmd(deps))
	cmd.AddCommand(newReleaseCmd(deps))
	cmd.AddCommand(newHeartbeatCmd(deps))
	cmd.AddCommand(newReapCmd(deps))
	cmd.AddCommand(newNextCmd(deps))
	cmd.AddCommand(newMoveCmd(deps))
	cmd.AddCommand(newRenameCmd(deps))
	cmd.AddCommand(newRecoverCmd(deps))
	cmd.AddCommand(newMigrateCmd(deps))
	cmd.AddCommand(newWatchCmd(deps))
	cmd.AddCommand(newRenderCmd(deps))
	cmd.AddCommand(newSyncCmd(deps))
	cmd.AddCommand(newImportCmd(deps))
	cmd.AddCommand(newExportPlanCmd(deps))
	cmd.AddCommand(newExportCmd(deps))
	cmd.AddCommand(newCompactCmd(deps))

	return cmd
}

// resolveAgent picks the agent ID for this invocation. The priority is
// --agent flag > Local.DefaultAgent (which already encodes env-var
// overrides via adapter/config.LoadLocalEffective at startup).
func resolveAgent(d Deps) string {
	if d.Globals != nil && d.Globals.Agent != "" {
		return d.Globals.Agent
	}
	if d.Usecase.Local == nil {
		return ""
	}
	local, err := d.Usecase.Local.Load()
	if err != nil {
		return ""
	}
	return local.DefaultAgent
}

// resolveAgentRole picks the agent role for stamping CreatedByRole.
// Falls through to Local.AgentRole (already env-resolved at startup);
// defaults to RoleHuman if unset.
func resolveAgentRole(d Deps) domain.Role {
	if d.Usecase.Local == nil {
		return domain.RoleHuman
	}
	local, err := d.Usecase.Local.Load()
	if err != nil || local.AgentRole == "" {
		return domain.RoleHuman
	}
	return local.AgentRole
}

// Execute parses args, dispatches the chosen verb, and translates domain
// errors into stable exit codes per §15.1. It is the only place exit
// codes are mapped, so verbs can return wrapped sentinels and trust the
// translation happens here.
//
// Deprecated for production paths — cmd/corvee/main.go uses
// ExecuteWithDeps so it can wire concrete adapters. Kept for tests
// that only exercise format/exit-code behavior.
func Execute(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	deps := Deps{
		Globals: &Globals{},
		Stdout:  stdout,
		Stderr:  stderr,
	}
	return ExecuteWithDeps(ctx, args, deps)
}

// ExecuteWithDeps runs the CLI with a pre-built Deps bundle. Used by
// cmd/corvee/main.go after wiring concrete adapters.
func ExecuteWithDeps(ctx context.Context, args []string, deps Deps) int {
	if deps.Globals == nil {
		deps.Globals = &Globals{}
	}
	root := NewRootCmd(deps)
	root.SetArgs(args)
	root.SetOut(deps.Stdout)
	root.SetErr(deps.Stderr)

	err := root.ExecuteContext(ctx)
	return exitCodeFor(err, deps.Stderr)
}

// exitCodeFor maps a returned error to the spec's exit-code table.
// errors.Is is used in preference to type assertions so wrapped errors
// (the usecase pattern) translate correctly.
func exitCodeFor(err error, stderr io.Writer) int {
	if err == nil {
		return 0
	}

	// Print the error envelope for the agent to consume on stdout — but
	// since agents see exit codes too, the human-readable diagnostic
	// goes to stderr.
	fmt.Fprintln(stderr, err.Error())

	switch {
	case errors.Is(err, domain.ErrUsage):
		return 2
	case errors.Is(err, domain.ErrNotFound):
		return 3
	case errors.Is(err, domain.ErrVersionConflict):
		return 4
	case errors.Is(err, domain.ErrAlreadyClaimed),
		errors.Is(err, domain.ErrLeaseMismatch),
		errors.Is(err, domain.ErrClaimLost),
		errors.Is(err, domain.ErrOpAlreadyRecorded):
		return 5
	case errors.Is(err, domain.ErrSchemaInvalid):
		return 6
	case errors.Is(err, domain.ErrIntegrityViolated):
		return 7
	case errors.Is(err, domain.ErrLockTimeout):
		return 8
	case errors.Is(err, domain.ErrPendingOperation):
		return 9
	}

	// Cobra reports unknown flags and missing args as plain errors with
	// no sentinel. Map those to exit 2 (usage error) per §15.1.
	if isCobraUsageError(err) {
		return 2
	}

	return 1
}

// isCobraUsageError detects cobra's flag/argument errors via their text
// since cobra does not expose typed sentinels.
func isCobraUsageError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "unknown flag") ||
		contains(msg, "unknown command") ||
		contains(msg, "unknown shorthand flag") ||
		contains(msg, "requires") ||
		contains(msg, "accepts")
}

func contains(s, substr string) bool {
	if len(substr) == 0 || len(s) < len(substr) {
		return len(substr) == 0
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// Discard is a no-op writer; used when callers want to silence output.
var Discard io.Writer = io.Discard

var _ = os.Stdin // keep os import meaningful; future verbs read stdin
