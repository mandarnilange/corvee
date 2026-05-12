# Contributing to corvee

Thank you for considering a contribution. This document captures the
working agreements that govern *how* changes get made. The full
behavioural specification lives in
[`.spec/corvee-spec.md`](.spec/corvee-spec.md); the rules
that govern day-to-day collaboration live in
[`CLAUDE.md`](CLAUDE.md) — read both before opening a non-trivial PR.

If you are an AI agent, also load `CLAUDE.md` at the start of every
session — it is the codebase's contract for any agent (human or
machine) touching the source.

---

## Quick start

```sh
git clone https://github.com/mandarnilange/corvee
cd corvee
make hooks-install   # one-time: installs the pre-push gate
make build           # produces ./bin/corvee
make ci              # everything CI runs (lint, race tests, e2e, coverage)
```

Go ≥1.26 is required. The build does not depend on anything outside
the standard library and the modules listed in `go.mod` (see the
banned-dependency list in [CLAUDE.md §9](CLAUDE.md)).

---

## The TDD loop is non-negotiable

Spec §11 says it plainly: **TDD is a hard requirement, not a
recommendation.** Every functional change is delivered as:

1. Write a failing test in the package being modified. The test names
   a behaviour, not an implementation
   (`TestClaim_RejectsWhenAlreadyClaimed`, not
   `TestClaimReturnsError`).
2. Run the test, confirm it fails for the expected reason — not a
   compile error or a typo.
3. Write the minimum code to make it pass. No extra fields, no extra
   helpers, no extra branches the test doesn't require.
4. Run **all** tests, not just the new one. Catch regressions
   immediately.
5. Refactor with the safety net of green tests.
6. Commit the test + implementation as a single atomic change.

A change that doesn't fit the loop (pure refactor, config tweak, doc
edit) is exempt. Anything that changes runtime behaviour follows it.

Coverage gates: `internal/domain/` ≥90 %, `internal/usecase/` ≥90 %.
CI fails the build when either falls below.

---

## The `make ci` gate

`make ci` is the single source of truth for "is my change ready?".
Locally it runs the same steps GitHub Actions runs, and the pre-push
hook runs it on every push.

| Need | Command |
|---|---|
| Compile the binary | `make build` |
| Fast inner loop | `make test-unit` (sub-2s) |
| Pre-commit gate | `make ci` (everything) |
| Concurrency confidence | `make test-race` (`-race -count=10`) |
| End-to-end | `make test-e2e` |
| Coverage report | `make test-coverage` |

Do **not** invoke `go test`, `gofmt`, `golangci-lint`, or `go vet`
ad-hoc. Those bypass the gates CI depends on.

---

## Clean architecture (enforced by `golangci-lint depguard`)

```
cmd/corvee/                ← CLI entrypoint (~30 lines of wiring)
  ↓
internal/cli/              ← Cobra wiring, flag parsing, JSON envelope
  ↓
internal/usecase/          ← Application orchestration (one verb per file)
  ↓ (interfaces only)
internal/domain/           ← Pure types and rules (no I/O, no time)
  ↑ (implements ports)
internal/adapter/          ← Filesystem, git, ULIDs, fsnotify, render
```

- `domain/` imports nothing from this codebase.
- `usecase/` imports only `domain/`. Never imports a concrete adapter.
- `adapter/*` implements the `domain` ports. Never imports `usecase`
  or `cli`.
- `cli/` imports `usecase`. No business logic in flag parsing.

`depguard` rejects violating imports at lint time, but reviewers also
flag them.

---

## Conventional commits

Commit messages follow the conventional-commits format (spec §11.6):

```
feat(domain/item): add status transition validation
test(usecase/claim): cover lease mismatch
fix(adapter/fsstore): retry rename on EXDEV
docs(readme): document --auto-claim flag
chore(ci): bump coverage floor
```

Rules:

- Subject ≤72 characters.
- Reference the relevant story ID in the body where applicable, e.g.
  `TTR-E09-S08`. The tracker can then cross-reference.
- **No co-author tags** and no Claude-specific markers — see
  `~/.claude/CLAUDE.md` if you are running with Anthropic tooling.
- Test + implementation in one commit (TDD step 6).
- One concern per PR. A spec PR (`.spec/`) is separate from an
  implementation PR.

---

## Branch naming

| Type of work | Prefix |
|---|---|
| New feature | `feat/<short-name>` |
| Bug fix | `fix/<short-name>` |
| Documentation | `docs/<short-name>` |
| Maintenance / chore | `chore/<short-name>` |
| Spec change | `spec/<short-name>` |

Open PRs against `main`. Long-running branches should rebase rather
than merge.

---

## Sign-off

We accept changes under the [Developer Certificate of Origin
(DCO)](https://developercertificate.org/). Add a `Signed-off-by:`
trailer to every commit:

```sh
git commit --signoff -m "feat(...): ..."
```

By signing off you are asserting that you have the right to submit the
change under the project's MIT licence.

---

## Reporting bugs / asking for features

Use the GitHub issue templates:

- **Bug report** — repro steps, expected vs actual, `corvee version`
  output, OS.
- **Feature request** — what problem you are trying to solve before
  jumping to a proposed solution.
- **Spec mismatch** — when the running CLI disagrees with
  `.spec/corvee-spec.md`. Link to the spec section. Resolution
  often ends up in [`docs/spec-questions.md`](docs/spec-questions.md)
  per the §14.8 ambiguity protocol.

Security vulnerabilities go through [SECURITY.md](SECURITY.md) — do
not file them as public issues.

---

## CHANGELOG

Update [`CHANGELOG.md`](CHANGELOG.md) under the `[Unreleased]`
section in the same PR that changes user-visible behaviour. The format
is [keep-a-changelog](https://keepachangelog.com/en/1.1.0/).

---

## Code of conduct

This project follows the [Contributor Covenant 2.1](CODE_OF_CONDUCT.md).
By participating you agree to abide by its terms.
