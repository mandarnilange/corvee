# CLAUDE.md — Working Conventions for corvee

This file is the codebase's contract for any agent (human or AI) modifying
it. The full behavioral specification lives in
[`.spec/corvee-spec.md`](.spec/corvee-spec.md); this document
captures the working rules that govern *how* changes get made.

If you are an AI agent: read this file before touching any code. Read it
again if a session has been compacted. The rules here are not optional.

---

## 1. Project

A local-first, file-based task tracker that coordinates work across humans
and AI agents through a git-distributed backlog. Implemented in **Go ≥1.26**.
The `corvee` binary is the only runtime artifact; there is no daemon and no
server. State lives entirely in the project's `.tasks/` directory.

Open-sourced under MIT — see [`LICENSE`](LICENSE). Public repo:
[`github.com/mandarnilange/corvee`](https://github.com/mandarnilange/corvee).

The historical work breakdown — every story across every phase — is in
[`.spec/tasks.json`](.spec/tasks.json). All 100 stories shipped; new work
is appended there via `corvee add`. When you need to know "what's next,"
read that file.

---

## 2. Build, Test, Lint

The `Makefile` is the only invocation surface. Do **not** run `go test`,
`gofmt`, `golangci-lint`, or `go vet` ad-hoc — those bypass the gates CI
relies on.

| Need | Command |
|---|---|
| Compile the binary | `make build` |
| Fast inner loop | `make test-unit` (sub-2s) |
| Pre-commit gate | `make ci` (everything) |
| Concurrency confidence | `make test-race` (`-race -count=10`) |
| End-to-end | `make test-e2e` |
| Coverage report | `make test-coverage` |

`make ci` is what GitHub Actions runs and what the pre-push hook runs. If
it passes locally it passes in CI.

---

## 3. TDD is Mandatory

The spec's §11 says it plainly: **TDD is a hard requirement, not a
recommendation.** Every functional change is delivered as:

1. **Write a failing test** in the package being modified. The test names
   a behavior, not an implementation:
   `TestClaim_RejectsWhenAlreadyClaimed`, not `TestClaimReturnsError`.
2. **Run the test, confirm it fails** for the expected reason — not a
   compile error or a typo.
3. **Write the minimum code** to make it pass. No extra fields, no extra
   helpers, no extra branches the current test doesn't require.
4. **Run all tests**, not just the new one. Catch regressions immediately.
5. **Refactor** with the safety net of green tests.
6. **Commit** the test + implementation as a single atomic change.

A change that doesn't fit this loop (a pure refactor, a config tweak, a
doc edit) is exempt. Anything that changes runtime behavior follows the
loop.

---

## 4. Clean Architecture (enforced by `golangci-lint depguard`)

```
  cmd/corvee/                  ← CLI entrypoint (main)
   ↓
  internal/cli/              ← Cobra wiring, flag parsing, JSON output
   ↓
  internal/usecase/          ← Application orchestration
   ↓ (interfaces only)       │
  internal/domain/           ← Pure types and rules
   ↑ (implements ports)
  internal/adapter/          ← Filesystem, git, ULIDs, fsnotify
```

The dependency rule:

- `domain/` imports nothing from this codebase. No file I/O, no syscalls,
  no `time`, no goroutines.
- `usecase/` imports only `domain/`. Never imports a concrete adapter.
- `adapter/*` implements the `domain` ports. Never imports `usecase` or
  `cli`.
- `cli/` imports `usecase`. No business logic in flag parsing.
- `cmd/corvee/main.go` is ~30 lines wiring the dependency graph.

`golangci-lint`'s `depguard` rule rejects violating imports. Reviewers
also flag them, but the linter catches them first.

---

## 5. Naming and Conventions (§11.2 + §14.7)

- **Test names**: `Test<Behavior>_<Condition>` — what behavior, under
  what condition. Sub-tests (`t.Run`) describe variations.
- **Table-driven tests** for any function with >2 variants. Use
  `t.Parallel()` aggressively in unit tests.
- **One usecase = one exported function = one file = one test file**.
  Input struct (`<Verb>Input`) and output struct (`<Verb>Output`)
  defined alongside.
- **Function signatures**: every usecase is
  `func Verb(ctx context.Context, d Deps, in VerbInput) (VerbOutput, error)`.
- **No global state**. No package-level vars except constants and error
  sentinels.
- **No `init()`** except for embed.FS loaders.
- **No goroutines in `domain` or `usecase`**. Concurrency lives in
  adapters.
- **No `panic`** except for "this is impossible" assertions in
  exhaustive switches over closed enums.
- **`errors.Is` / `errors.As`** only. Never compare error strings.
- **Imports** in three blocks: stdlib, third-party, local. Enforced by
  goimports.
- **Every exported identifier has a godoc comment** starting with the
  identifier's name. Enforced by `revive`.
- **No magic numbers**. Constants get names. `60 * time.Minute` becomes
  `defaultClaimTTL`.

---

## 6. Errors → Exit Codes (single-source mapping)

The mapping lives in `internal/cli/root.go:exitCodeFor`. Usecases return
wrapped sentinels; the CLI translates once.

| Sentinel (`internal/domain/errors.go`) | Exit | Meaning |
|---|---|---|
| `ErrUsage` | 2 | Argument or flag error |
| `ErrNotFound` | 3 | Item or workspace missing |
| `ErrVersionConflict` | 4 | Optimistic-concurrency mismatch |
| `ErrAlreadyClaimed`, `ErrLeaseMismatch`, `ErrClaimLost` | 5 | Claim conflict |
| `ErrSchemaInvalid` | 6 | Item failed JSON Schema validation |
| `ErrIntegrityViolated` | 7 | Dependency / orphan / dependent guard |
| `ErrLockTimeout` | 8 | Per-item lock acquisition timeout |
| `ErrPendingOperation` | 9 | Run `corvee recover` |
| (anything else) | 1 | Generic error |
| (cobra usage error text) | 2 | Mapped via prefix matching |

Add a sentinel by editing `errors.go` *and* the table in `root.go` *and*
the test table in `root_test.go`. The trio drifts apart silently if you
only update one.

---

## 7. Spec Ambiguity Protocol (§14.8)

When the spec underspecifies a concrete decision:

1. Write the test that codifies your best guess.
2. Implement to the test.
3. Add an entry to [`docs/spec-questions.md`](docs/spec-questions.md)
   describing the ambiguity and the resolution chosen.
4. Proceed.

The protocol surfaces assumptions instead of burying them. See
[`docs/spec-questions.md`](docs/spec-questions.md) for every recorded
deviation (SQ-001 through SQ-008 at the time of v1.0.0).

---

## 8. Phases & Current State

The implementation was phased per spec §13. Each phase shipped a
working, tested, releasable binary; the phase's exit gate was `make ci`
plus the spec's §11.5 thresholds. **All 9 phases are done (100 / 100
stories).**

| Phase | Outcome |
|---|---|
| 0 | `corvee version` green; CI green; `.spec/tasks.json` populated |
| 1 | Single-user backlog (init/add/list/show/update/done/delete-soft) |
| 2 | Hierarchy + validation + sharded events log + index cache |
| 3 | Distributed-safe concurrency (claims, lease IDs, capability match) |
| 4 | Multi-file ops + crash recovery |
| 5 | Real-time observability (`corvee watch`, journal metadata) |
| 6 | HTML render + deploy handoff manifest |
| 7 | Git sync + distributed merge (the multi-VM gate) |
| 8 | Plugins, hooks, compaction, SKILL.md |

The next milestone is the **`v1.0.0` tag** — the first public release
(GitHub Release with cross-compiled binaries + skill tarball). Follow-on
work (plugin extensions, SKILL.md polish, spec follow-ups) gets appended
to `.spec/tasks.json` as new stories, not new phases.

`.spec/tasks.json` carries every story with a `status` field from spec
§5's enum (`backlog`, `ready`, `claimed`, `in_progress`, `review`,
`blocked`, `done`, `abandoned`), plus a `completed_at` timestamp on
done rows. `cmd/tasks-validate` enforces the schema as part of
`make ci`, so drift is impossible.

**Updating status**: when a story lands, run

```sh
go run ./cmd/tasks-mark .spec/tasks.json TTR-E01-S03 done
```

`tasks-mark` defaults `completed_at` to the current UTC time when
`status=done`. Pass `--completed-at <iso>` to override (useful when
back-filling a commit timestamp). Use `--init` once at bootstrap to
apply default status across the whole document.

What's pending? Open `.spec/tasks.json` — any `ready` story is fair
game; pull one, claim via `corvee claim`, and start TDD. If you're
opening new work, add a story (`corvee add story --parent <epic>`)
before you start.

---

## 9. Banned Dependencies (§12.5)

Anything outside the allow-list in `go.mod` requires a separate PR with
spec §12.2-style justification. Specifically banned:

- ORMs, SQL drivers (files are the source of truth)
- HTTP clients (no network calls beyond `git push`/`git pull`)
- YAML libraries (config and data are JSON)
- Logging frameworks: logrus, zap, etc. — use stdlib `log/slog`
- Testing frameworks: testify, ginkgo, etc. — use stdlib `testing`

`golangci-lint depguard` enforces this list at lint time.

---

## 10. Commit & PR Format

- **Conventional commits** (§11.6): `feat(domain/item): add status
  transition validation`, `test(usecase/claim): cover lease mismatch`.
- **Test + implementation in one commit** (§11.1 step 6).
- **No AI co-author tags.** No `Co-Authored-By: Claude ...` lines, no
  `🤖 Generated with ...` footers, no AI-tool-specific comments in
  commit messages. Authorship is the human pushing the commit.
- **One concern per PR.** A spec PR (`.spec/`) is separate from an
  implementation PR.
- **PR descriptions reference the story ID** (`TTR-E01-S08`) so the
  tracker can be cross-referenced.

---

## 11. Pre-Commit / Pre-Push Hooks

The `.githooks/pre-push` hook runs `make ci`. Install via
`make hooks-install` (one-time per clone).

Human contributors: `make ci` locally before pushing is sufficient —
the same gates run in GitHub Actions (`.github/workflows/ci.yml`).

AI agents working through Claude Code (or similar harnesses) should
spawn the four checks in parallel and wait for all four before
committing:

- `Task(subagent_type="test-runner", run_in_background=true)`
- `Bash("make lint", run_in_background=true)`
- `Task(subagent_type="clean-architecture-validator", run_in_background=true)`
- `/coderabbit:review` skill (if available)

Address any CodeRabbit findings (presented as a summary table per the
agent protocol) before committing.

---

## 12. Workspace Cache Policy

The `.tasks/` workspace is created by `corvee init`. In this repo it
already exists (committed) — corvee tracks its own development.

- `.tasks/items/`, `.tasks/events/*.jsonl`, and `.tasks/workspace.json`
  (project-level config) **are committed to git**.
- `.tasks/index.json`, `.tasks/locks/`, `.tasks/operations/`, and
  `.tasks/local.json` (per-VM agent identity) **are gitignored**.
  The index is a cache, never source of truth, and is fully
  rebuildable via `corvee reindex`. `local.json` holds `default_agent`,
  `agent_role`, and `agent_capabilities` so executors on different
  VMs don't merge-conflict over their own names — see SQ-003.

## 13. Workspace Filesystem Restriction

The `.tasks/` workspace **must live on a local POSIX filesystem**
(linux/{amd64,arm64}, darwin/arm64). NFS, SMB, and other network
filesystems are explicitly unsupported because Phase 3's per-item
`O_EXCL` lock files race on networked storage (well-known kernel
bug). `corvee validate` checks the filesystem type via `statfs` and
emits an integrity warning if the workspace is hosted on a network
share. Windows is out of scope by §12.7 (`make build-all` does not
target it).
