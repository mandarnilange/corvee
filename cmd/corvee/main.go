// Command corvee is the CLI entrypoint for the local-first task tracker.
//
// Per the clean-architecture layering (spec §2), this binary is the
// only place that wires concrete adapters. It builds the dependency
// graph, hands it to internal/cli, and forwards the resulting exit
// code. All behavior lives elsewhere.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/mandarnilange/corvee/internal/adapter/clock"
	"github.com/mandarnilange/corvee/internal/adapter/config"
	"github.com/mandarnilange/corvee/internal/adapter/eventlog"
	"github.com/mandarnilange/corvee/internal/adapter/fsstore"
	"github.com/mandarnilange/corvee/internal/adapter/git"
	"github.com/mandarnilange/corvee/internal/adapter/hooks"
	"github.com/mandarnilange/corvee/internal/adapter/idgen"
	"github.com/mandarnilange/corvee/internal/adapter/index"
	"github.com/mandarnilange/corvee/internal/adapter/oplog"
	"github.com/mandarnilange/corvee/internal/adapter/render"
	"github.com/mandarnilange/corvee/internal/adapter/watch"
	"github.com/mandarnilange/corvee/internal/cli"
	"github.com/mandarnilange/corvee/internal/domain"
	"github.com/mandarnilange/corvee/internal/usecase"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	deps, err := buildDeps(os.Args[1:], os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	exitCode := cli.ExecuteWithDeps(ctx, os.Args[1:], deps)
	os.Exit(exitCode)
}

// buildDeps resolves the workspace location, constructs concrete
// adapter implementations, and returns the cli.Deps bundle. The
// `init` verb is special-cased: when no .tasks/ is found via auto-
// discovery, init operates against cwd/.tasks (not yet existing) and
// the Store is left nil since the usecase only consults the loaders.
func buildDeps(args []string, stdout, stderr *os.File) (cli.Deps, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return cli.Deps{}, fmt.Errorf("cwd: %w", err)
	}

	// Honor an explicit --workspace flag before falling back to auto-discovery.
	// Pre-parse it here so the filesystem adapters are wired to the right dir
	// before cobra runs its PersistentPreRun hooks.
	tasksDir, found := discoverTasksDir(cwd)
	if ws := parseWorkspaceFlag(args); ws != "" {
		abs, absErr := filepath.Abs(ws)
		if absErr == nil {
			tasksDir = abs
			found = dirExists(abs)
		}
	}
	if !found {
		// Either we're running `init` (which will create it) or any
		// other verb (which will fail with ErrIntegrityViolated when
		// the workspace loader probes). Either way, default the path
		// to cwd/.tasks so init has somewhere to bootstrap.
		if tasksDir == "" {
			tasksDir = filepath.Join(cwd, ".tasks")
		}
	}

	workspaceLoader := config.NewWorkspaceLoader(filepath.Join(tasksDir, "workspace.json"))
	localLoader := config.NewLocalLoader(filepath.Join(tasksDir, "local.json"))

	// Apply env-var overrides on the local config for the rest of
	// the invocation (the loader passed to verbs returns the file as
	// stored; LoadLocalEffective is consulted up-front so the agent
	// helpers in cli see the merged result).
	effective, err := config.LoadLocalEffective(localLoader)
	if err != nil {
		return cli.Deps{}, fmt.Errorf("load local config: %w", err)
	}
	mergedLocal := newMergedLocalLoader(localLoader, effective)

	var store domain.Store
	var evlog domain.EventLog
	var indexer domain.Indexer
	var locker domain.Locker
	var opResults domain.OpResultStore
	var opJournal domain.OpJournal
	var tailer domain.EventTailer
	if found {
		s, err := fsstore.New(filepath.Join(tasksDir, "items"))
		if err != nil {
			return cli.Deps{}, fmt.Errorf("open store: %w", err)
		}
		store = s
		ev, err := eventlog.New(filepath.Join(tasksDir, "events"))
		if err != nil {
			return cli.Deps{}, fmt.Errorf("open eventlog: %w", err)
		}
		evlog = ev
		indexer = index.New(filepath.Join(tasksDir, "index.json"), filepath.Join(tasksDir, "items"))
		l, err := fsstore.NewLocker(filepath.Join(tasksDir, "locks"))
		if err != nil {
			return cli.Deps{}, fmt.Errorf("open locker: %w", err)
		}
		locker = l
		ops, err := oplog.New(filepath.Join(tasksDir, "operations"))
		if err != nil {
			return cli.Deps{}, fmt.Errorf("open oplog: %w", err)
		}
		opResults = ops
		j, err := oplog.NewJournal(filepath.Join(tasksDir, "operations"))
		if err != nil {
			return cli.Deps{}, fmt.Errorf("open op journal: %w", err)
		}
		opJournal = j
		tailer = watch.New(filepath.Join(tasksDir, "events"), clock.New())
	}
	renderer := render.New()
	gitClient := git.New(filepath.Dir(tasksDir))
	gitInfo := gitClient
	// For init or no-workspace cases store stays nil; verbs that need
	// it surface ErrIntegrityViolated via the workspace loader probe.

	deps := cli.Deps{
		Globals: &cli.Globals{},
		Stdout:  stdout,
		Stderr:  stderr,
		Usecase: usecase.Deps{
			Store:          store,
			Clock:          clock.New(),
			IDGen:          idgen.New(),
			Workspace:      workspaceLoader,
			Local:          mergedLocal,
			EventLog:       evlog,
			Indexer:        indexer,
			Locker:         locker,
			OpResults:      opResults,
			OpJournal:      opJournal,
			EventTailer:    tailer,
			Renderer:       renderer,
			Git:            gitInfo,
			GitClient:      gitClient,
			TasksDir:       tasksDir,
			PluginRegistry: hooks.NewRegistry(filepath.Join(tasksDir, "plugins")),
			HookDispatcher: hooks.NewDispatcher(),
			AgentMode:      hooks.IsAgentMode(false),
		},
		TasksDir: tasksDir,
	}
	_ = args // reserved for future per-verb routing
	return deps, nil
}

// discoverTasksDir walks up from start looking for a .tasks/ directory
// per spec §18. Returns the absolute path and true on success. Stops
// at the filesystem root.
func discoverTasksDir(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, ".tasks")
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate, true
		}
		// Other Stat errors (permission denied, etc.) are treated as
		// "not found here" — keep walking up; the caller may have
		// access higher in the tree.
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// mergedLocalLoader wraps a real LocalLoader so Load returns the
// already-env-merged effective config. Save passes through to the
// underlying loader so on-disk state isn't polluted with env values.
type mergedLocalLoader struct {
	underlying domain.LocalLoader
	effective  domain.Local
}

func newMergedLocalLoader(underlying domain.LocalLoader, effective domain.Local) *mergedLocalLoader {
	return &mergedLocalLoader{underlying: underlying, effective: effective}
}

func (m *mergedLocalLoader) Load() (domain.Local, error) { return m.effective, nil }

func (m *mergedLocalLoader) Save(local domain.Local) error { return m.underlying.Save(local) }

func (m *mergedLocalLoader) Exists() (bool, error) { return m.underlying.Exists() }

// parseWorkspaceFlag extracts the value of --workspace from raw CLI args
// before cobra processes them. Returns "" if the flag is absent.
func parseWorkspaceFlag(args []string) string {
	for i, arg := range args {
		if arg == "--workspace" && i+1 < len(args) {
			return args[i+1]
		}
		if len(arg) > len("--workspace=") && arg[:len("--workspace=")] == "--workspace=" {
			return arg[len("--workspace="):]
		}
	}
	return ""
}

// dirExists reports whether path is an existing directory.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
