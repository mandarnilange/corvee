# Spec Questions Log

Per the spec's §14.8 ambiguity protocol: when an implementer hits a place
where the spec underspecifies a concrete decision, they record the question
and the resolution chosen here, then proceed. The resolution is reviewable;
the assumption is not buried.

Format per entry:

- **ID** — short slug
- **Section** — spec section the ambiguity lives in
- **Question** — what was unclear
- **Resolution** — what we chose and why
- **Date** — when the resolution was made

---

## SQ-001 — §16.1: SKILL folder layout language mismatch

- **Section**: §16.1 (SKILL.md folder layout) vs §2 (architecture) and §12 (tech stack)
- **Question**: §16.1 shows a Node-based skill folder (`bin/corvee` with a Node shebang, `package.json`, `lib/`, `commands/`, `node --test`), but §2 ("The implementation language is **Go (≥1.22)**") and §12.1/§12.6 are unambiguously Go. Which is authoritative?
- **Resolution**: Go is authoritative. §16.1 reads as residue from a pre-Go draft. The §2 language decision is explicit and dated; §12 specifies Go modules, `golangci-lint`, `gofmt`, `go test -race`, and a `Makefile`-driven invocation surface; §11 (TDD) references `go test` and `t.Cleanup()`; §14 pins Go interface signatures. Implementing §16.1 verbatim would invalidate every other section. **Action**: §16.1's folder layout will be patched in a follow-up doc PR to reflect the Go layout (`cmd/corvee/`, embedded templates via `embed.FS`, `go test` for the skill's tests). Tracker entry: deferred to Phase 8 docs work in `.spec/tasks.json`. The skill itself ships as a Go binary plus the `references/*.md` files, distributed as a release asset rather than a Node package.
- **Date**: 2026-05-05

---

## SQ-002 — §12.1: Go 1.22 vs 1.23 minimum

- **Section**: §12.1 (Tech Stack & Dependencies)
- **Question**: §12.1 says "Go ≥1.22" in the version line and "uses range-over-function iterators introduced in 1.23, which simplify event-log streaming" in the rationale. The two are contradictory — if 1.23-only iterators are used, the minimum is 1.23, not 1.22.
- **Resolution**: Initially pinned to **Go 1.23**. Subsequently bumped to **Go 1.26** during Phase 7+8 development (matches `go.mod`'s actual `go 1.26.0` line and the `toolchain go1.26.2` directive). Rationale: 1.26 is what the project actually compiles against; documentation pretending otherwise is just a foot-gun for end users running `go install`. All install instructions (README, getting-started, skill README, SKILL.md) say "Go ≥1.26".
- **Date**: 2026-05-05 (revised 2026-05-06)

---

## SQ-003 — §5.1: single config.json mixes synced and per-VM state

- **Section**: §5.1 (Workspace-level metadata) and §3 (directory layout)
- **Question**: §5.1 specifies a single `.tasks/config.json` containing both synced workspace state (`workspace_name`, `default_project`, `claim_ttl_minutes`, `heartbeat_interval_minutes`, `critical_path`, `git.*`) and per-VM agent state (`default_agent`, `agent_role`, `agent_capabilities`). §3's directory layout does not list `config.json` as gitignored. In a multi-VM topology, every executor would commit its own `default_agent` to the same file, causing a constant `corvee sync` merge conflict over each agent's own name. §18 hints at `CORVEE_AGENT_ID` env-var fallback, but env-only is a footgun (one missed export and the agent identity gets committed).
- **Resolution**: Split the config into two files.
  - `.tasks/workspace.json` (committed): `schema_version`, `workspace_name`, `default_project`, `claim_ttl_minutes`, `heartbeat_interval_minutes`, `critical_path`, `git.{auto_commit, auto_sync_seconds}`. Stable across VMs, safe to sync.
  - `.tasks/local.json` (gitignored): `default_agent`, `agent_role`, `agent_capabilities`. Per-VM; never committed.
  - Env-var overrides per §7.1 / §18 still apply: `CORVEE_AGENT_ID`, `CORVEE_AGENT_ROLE`, `CORVEE_AGENT_CAPABILITIES`. Resolution order: env → `local.json` → built-in default.

  This intentionally diverges from §5.1's single-file design. The §5.1 wording will be patched in TTR-E08-S06 alongside SQ-001/SQ-002. Affected stories in `.spec/tasks.json`: TTR-E00-S09 (gitignore covers `.tasks/local.json`), TTR-E01-S04 (two structs + two loaders + env override), TTR-E01-S10 (init writes both files; preserves existing `local.json` on re-init).
- **Date**: 2026-05-05

---

## SQ-004 — §4: ID grammar 1-99 vs dogfood TTR-E00

- **Section**: §4 (ID Scheme) and `.spec/tasks.json`
- **Question**: §4 says numeric segments are zero-padded **1-99**, so `ParseID` rejects `E00`. The dogfood file uses `TTR-E00` for "Phase 0 — Project Skeleton" because numbering started at 0 in early planning. The two are inconsistent.
- **Resolution**: Keep `ParseID` strict (1-99 per spec). The Phase 0 stories are already done and will not be re-imported in production; the import self-test (TTR-E07-S05) strips Phase 0 from `.spec/tasks.json` before validating the round-trip. If a future planning doc needs a "phase 0" semantic, use `E01` for the bootstrap phase and re-number.
- **Date**: 2026-05-06

---

## SQ-005 — §11.5: usecase coverage threshold counts testfakes

- **Section**: §11.5 (CI gates) and `scripts/check-coverage.sh`
- **Question**: §11.5 mandates ≥85% line coverage in `usecase/` (since bumped to ≥90% — see SQ-007), enforced via `go test -coverprofile=...`. The script averages every function whose path begins with `internal/usecase`, which includes `internal/usecase/testfakes/*` — production support code with intentionally low self-coverage (it's exercised by the consumers, not its own tests). Phase 7+8 added ~1800 lines of new usecase code, dragging the testfakes-included average below 85% even though the production code itself sits at ~89%.
- **Resolution**: Exclude `internal/usecase/testfakes/` and `internal/usecase/testdata/` from the `usecase` coverage average. testfakes is test-only support; counting its self-coverage punishes adding fakes (which is the right thing to do in TDD). Production code in `internal/usecase/*.go` continues to be measured against the active threshold (≥90% post SQ-007). `scripts/check-coverage.sh` updated accordingly.
- **Date**: 2026-05-06

---

## SQ-006 — §6 Layer 4: journal dedup key includes Note field

- **Section**: §6 Layer 4 (3-way merge for journal/tags) and `internal/usecase/merge_3way.go:unionJournal`
- **Question**: §6 Layer 4 says the journal-dedup key is the (ts, actor, event) triple. The implementation uses (ts, actor, event, **note**) — a quad. Strictly different from spec.
- **Resolution**: Keep the quad. Rationale: two agents on different VMs can produce events that share (ts, actor, event) but carry distinct `note` strings (e.g. parallel "progress" entries with different per-step descriptions). Dropping one would lose information. The cost is that an idempotent retry of the *exact* same journal entry (same note text, same timestamp, same actor) deduplicates correctly, while a re-emit with whitespace drift in `note` keeps both. We accept the latter as the lesser evil.
  - **Future revisit**: if duplicate-note-with-whitespace becomes a real complaint, normalise notes (trim, collapse whitespace) inside the dedup key while keeping the original prose in the stored entry.
- **Date**: 2026-05-06

---

## SQ-007 — §11.5: bump usecase coverage threshold from ≥85% to ≥90%

- **Section**: §11.5 (CI gates)
- **Question**: Spec §11.5 set the `usecase/` line-coverage floor at ≥85%, 5 points lower than `domain/`'s ≥90%. The asymmetry was justified at adoption time by usecase code paths involving I/O-shaped seams (filesystem, git, fsnotify) that were harder to cover. Post-v1.0, with testfakes fully fleshed out (SQ-005) and the codebase stabilized, is the 5-point gap still earning its keep?
- **Resolution**: Bump `usecase/` to ≥90%, matching `domain/`. Two reasons: (1) the 87.2% baseline at decision time was close enough that tightening the bar costs only a handful of targeted unit tests, not a refactor; (2) future drift is easier to spot when both layers track to the same number. New `coverage_extra3_test.go` adds the focused tests that lift the actual figure to 90.1% — chiefly defensive error paths in `sync.go` and the close-rollup helper from SQ-006's symmetric counterpart. Threshold updated in `Makefile`, `scripts/check-coverage.sh`, README, architecture doc, and this spec (§11.5 + §11.5 table + appendix).
- **Date**: 2026-05-12

---

## SQ-008 — §16: CLAUDE.md/AGENTS.md bootstrap belongs in the skill, not in `corvee init`

- **Section**: §16 (SKILL.md) and §15.2 (`init` verb)
- **Question**: TTR-E09-S10 introduces a "Using corvee in this repo" block appended to `CLAUDE.md` / `AGENTS.md` so AI agents in a new project find the tool without being told. The block could ship two ways: (a) as part of `corvee init --with-agent-docs` (one command, but `init` writes outside `.tasks/` and grows tightly coupled to a specific agent-runtime convention), or (b) as a separate `corvee-install` skill the user invokes ("install corvee", "add corvee to CLAUDE.md") which negotiates consent in prose before touching repo-root files. Which is authoritative?
- **Resolution**: Skill-only. `corvee init` continues to write only to `.tasks/` (workspace.json, local.json, gitignore entries) — it never edits `CLAUDE.md` or `AGENTS.md`. The `corvee-install` skill is the single supported path for bootstrapping agent docs. Three reasons:
  1. `init`'s scope is the workspace contract, and that boundary is load-bearing. Once `init` starts writing outside `.tasks/` it becomes an opinionated installer of one particular agent-runtime convention, and the next harness (Cursor, Windsurf, Codex) will demand a flag of its own.
  2. Skills already negotiate consent prose-style, which is the right interface for "may I append ~50 lines to your CLAUDE.md?" — a CLI flag has nowhere to put that conversation.
  3. Keeping the skill the single source of truth means the block's content can evolve without a binary release, which matters because the exit-code table and the canonical agent loop are the parts most likely to grow.

  The skill writes a fenced HTML-comment marker block (`<!-- BEGIN corvee:agent-block -->` … `<!-- END corvee:agent-block -->`) so re-running is idempotent and stale-block replacement is mechanical (delete between markers, re-append). Detection prefers `CLAUDE.md`, then `AGENTS.md`; if neither exists the skill asks before creating one.
- **Date**: 2026-05-12

---

<!-- New entries are appended below. -->
