# Architecture

The codebase follows clean architecture (Hexagonal / Ports &
Adapters). Five top-level packages, with a strict dependency rule
enforced by `golangci-lint`'s `depguard`.

```text
                   ┌──────────────────┐
                   │  cmd/corvee/main   │  ~30 lines wiring
                   └─────────┬────────┘
                             ↓
                   ┌──────────────────┐
                   │  internal/cli/   │  cobra, flag parsing, JSON envelope
                   └─────────┬────────┘
                             ↓
                   ┌──────────────────┐
                   │internal/usecase/ │  application orchestration
                   └─────────┬────────┘
                  interfaces ↓
                   ┌──────────────────┐
                   │internal/domain/  │  pure types & rules
                   └─────────┬────────┘
              implements ↑    │
                   ┌──────────────────┐
                   │internal/adapter/ │  fs, git, ULIDs, fsnotify, render
                   └──────────────────┘
```

## The dependency rule

| Layer | May import | May not import |
|---|---|---|
| `domain/` | stdlib | anything from this codebase |
| `usecase/` | `domain/` | `adapter/`, `cli/` |
| `adapter/` | `domain/` | `usecase/`, `cli/` |
| `cli/` | `domain/`, `usecase/` | `adapter/` (except indirectly via `cmd/`) |
| `cmd/corvee/` | everything | — (it's the wiring point) |

`golangci-lint depguard` rules in `.golangci.yml` enforce this. PRs
that violate the rule fail lint before they reach review.

## Why these layers?

### `domain/`: pure types

`domain/` defines the data the system reasons about (`Item`,
`Status`, `Claim`, `Event`) plus the **ports** — Go interfaces that
abstract every external dependency. No file I/O, no `time.Now()`, no
goroutines, no syscalls. The tests for `domain/` run in
microseconds because they touch no external system.

This is the layer everything else converges on. When you read a
domain file, you don't have to understand any other layer — the file
fully describes its responsibilities.

### `usecase/`: orchestration

Every CLI verb (`add`, `claim`, `done`, `sync`, …) is an exported
function in `usecase/` with the signature:

```go
func Verb(ctx context.Context, d Deps, in VerbInput) (VerbOutput, error)
```

`Deps` is the bundle of port interfaces a usecase needs (Store,
Clock, IDGen, EventLog, …). The usecase composes calls against
those ports, applies invariants from `domain/`, and returns. It
never instantiates concrete adapters.

This makes usecases testable with in-memory fakes (`testfakes/`).
Every usecase has a sub-2-second unit test.

### `adapter/`: implementations

`adapter/fsstore/` implements `domain.Store` against the
filesystem. `adapter/git/` implements `domain.GitClient` by
shelling out to `git`. `adapter/idgen/` implements `domain.IDGen`
with a real ULID source.

Adapters can fail (disk full, git not installed, network down).
That's where the `*_test.go` files use `t.TempDir()` and exercise
real I/O.

### `cli/`: presentation

Cobra commands. Each verb has a `new<Verb>Cmd(deps Deps) *cobra.Command`
that parses flags, calls the usecase, and prints the JSON envelope.
The CLI never performs business logic.

The single source of truth for **error → exit code** lives in
`cli/root.go:exitCodeFor`. Domain sentinels feed it; adding a new
sentinel requires updating the table in `root.go` and the matching
test in `root_test.go` so they cannot drift.

### `cmd/corvee/main.go`: composition root

The only place concrete adapters are instantiated and wired into
`Deps`. About 30 lines that resolve the workspace path, build the
adapter graph, and hand control to `cli.ExecuteWithDeps`.

## Concurrency model (recap)

The full story lives in `.spec/corvee-spec.md` § 6. Layers in
brief:

| Layer | Mechanism | Where |
|---|---|---|
| 1 | Atomic file writes (tmp + rename) | `adapter/fsstore` |
| 2 | Optimistic concurrency via `version` | `usecase/` (Put with expectVersion) |
| 3 | Claim semantics + lease IDs | `usecase/claim`, `usecase/heartbeat`, … |
| 4 | Git as distribution & merge | `adapter/git`, `usecase/sync` |
| 5 | Sharded events log | `adapter/eventlog` |
| 6 | Operation journal (mini WAL) | `adapter/oplog`, `usecase/recover` |
| 7 | Idempotency via `operation_id` | `adapter/oplog`, `usecase/claim` |
| 8 | Distributed claim resolution (lower lease wins) | `usecase/merge_3way` |

Layer 8 is the keystone. Two executors on different VMs can both
issue `corvee claim X` before either syncs. When the second machine
pulls, the merge picks the lower lease ID (ULIDs are sortable). The
loser sees `details.claim_lost: true` on its next operation and
reroutes via `corvee next`. No human intervention required.

## File-system layout (`.tasks/`)

```text
.tasks/
  workspace.json             # committed config (claim TTL, critical path, …)
  local.json                 # gitignored per-VM identity
  .gitignore                 # encodes the gitignored paths
  items/                     # one JSON per item (committed)
    PROJ.json
    PROJ-E01.json
    PROJ-E01-S01.json
  events/                    # daily-sharded JSONL audit log (committed)
    2026-05-06.jsonl
    archive/                 # `corvee compact` archives old shards here
  hooks/                     # your shell hooks (committed)
    on-status-change.sh
  plugins/                   # external binary plugins (committed)
    auto-tagger/plugin.json
    auto-tagger/run.py
  operations/                # multi-file-op WAL (gitignored)
  locks/                     # per-item file locks (gitignored)
  index.json                 # cache; safe to delete (gitignored)
  dist/                      # `corvee render` output (gitignored)
```

The committed parts give you the workspace. The gitignored parts are
local cache + per-VM ephemera. Cloning a fresh repo + running any
`corvee <verb>` re-creates the gitignored bits automatically.

## Why one giant Makefile, not multiple shells / npm scripts?

`make` is the lowest-common-denominator authoring surface that runs
identically on contributor laptops and in CI. The pre-push hook calls
`make ci`. CI (`.github/workflows/ci.yml`) calls the same targets
broken into separate jobs. There is exactly one shell command for
each gate; ad-hoc invocation is the bug.

## Why no daemon?

Two reasons:

1. **Simplicity.** A daemon adds a lifecycle, a process supervisor
   issue, a port to bind, and a cross-process protocol. We have
   none of those.
2. **Correctness.** Coordination across machines goes through git.
   A daemon would need its own state-replication story; with no
   daemon, git is the replication, and the merge rule (Layer 8)
   handles divergence deterministically.

The trade-off: `corvee` re-reads `.tasks/items/*.json` on every CLI
invocation. The `index.json` cache absorbs most of that cost (one
`stat` per dir to invalidate). For backlogs up to ~10k items —
beyond what any single project actually has — invocation latency
stays under 50 ms.

## Why `cmd/tasks-validate` and `cmd/tasks-mark`?

Two tiny side-binaries that operate on the planning doc
(`.spec/tasks.json`) rather than the live workspace (`.tasks/`).
They predate the live workspace and are kept because:

- `tasks-validate` is what `make ci`'s `tasks-validate` target runs;
  it ensures the planning doc stays schema-valid.
- `tasks-mark` is a convenient one-shot for "this story just landed,
  flip its status in the planning doc."

Neither knows anything about `.tasks/` or the binary; they're pure
JSON tooling. Long-term, both should retire in favour of using
`corvee export-plan` to regenerate `.spec/tasks.json` from the live
workspace, but that migration is post-v1.0.

## Testing strategy

| Tier | Where | Goal | Time budget |
|---|---|---|---|
| Unit | `internal/domain/`, `internal/usecase/` | invariants & branches | < 2 s total |
| Adapter integration | `internal/adapter/*/` (each adapter's tests) | real I/O against `t.TempDir()` | < 10 s total |
| Concurrency | `*_concurrent_test.go` | `-race -count=10` | per-test < 1 s |
| E2E | `test/e2e/` | full binary against real workspace | per-test < 5 s |
| Property | `test/property/` | randomized inputs | bounded |

Coverage thresholds are enforced (`make test-coverage`):
- `internal/domain/` ≥ 90%
- `internal/usecase/` ≥ 90% (excluding `testfakes/`, see SQ-005)

## See also

- [.spec/corvee-spec.md](../.spec/corvee-spec.md) — the
  full behavioral spec (long, but read § 2, § 6, § 14 for
  architecture detail).
- [docs/spec-questions.md](spec-questions.md) — every place we
  deviated from the spec, with rationale.
- [CLAUDE.md](../CLAUDE.md) — the contributor contract (TDD,
  naming, layering rules, commit format).
