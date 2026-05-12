# corvee-install (skill)

A drop-in skill that walks an AI agent (or a human) through three
lifecycle operations for the `corvee` CLI:

1. **Install** the binary on a fresh machine (with OS/arch detection,
   `go install` preferred, pre-built release binary fallback, source
   build as a last resort).
2. **Verify + bootstrap** — confirm `corvee version` works and create
   a `.tasks/` workspace via `corvee init`.
3. **Maintain** — version-check against the latest GitHub release,
   upgrade in place preserving the original install method, and
   append a "Using corvee in this repo" block to `CLAUDE.md` /
   `AGENTS.md` so future agents pick the tool up automatically.

Companion to the [`corvee`](../corvee/SKILL.md) skill — that one
teaches the claim/heartbeat/done loop and assumes the binary is
already installed.

## Layout

```text
corvee-install/
  SKILL.md              # the manifest (frontmatter + step-by-step prose)
  README.md             # this file
```

No binaries. No install scripts. The skill is documentation an agent
reads and acts on — exact shell snippets included, but the agent
runs them after asking the user for consent.

## Installing the skill

Use [skills.sh](https://skills.sh/) — one command, auto-detects your
harness (Claude Code, Cursor, Windsurf, …):

```sh
npx skills add mandarnilange/corvee
```

This installs both `corvee-install` (this skill) and the day-to-day
`corvee` skill.

After install, the agent reads `SKILL.md` and triggers automatically
on phrases like "install corvee", "set up corvee", "upgrade corvee",
or "add corvee to CLAUDE.md".

### Offline / air-gapped install

When `npx` can't reach the registry, fall back to the bundled
tarball: `make skill-tarball` produces `dist/corvee-skill.tar.gz`
containing both `corvee/` and `corvee-install/`; extract it directly
into your harness's skills dir.

## Relationship to the `corvee` skill

| Skill | Purpose | When the agent reaches for it |
|---|---|---|
| `corvee-install` (this one) | Install, verify, upgrade, agent-doc bootstrap | "install corvee", "upgrade", "add corvee to CLAUDE.md" |
| `corvee` (sibling) | Day-to-day claim/heartbeat/done loop | "what should I work on?", "mark this done", "what's blocked" |

The install skill always finishes by pointing the agent at the
`corvee` skill for the actual workflow.

## Versioning

This skill tracks the corvee binary's release cadence — when a new
binary version ships, this skill's references to the latest release
URL still work via
`https://api.github.com/repos/mandarnilange/corvee/releases/latest`.
The agent-doc block (`<!-- BEGIN corvee:agent-block -->`) is
intentionally version-agnostic; it points at exit codes and the
canonical loop, both of which are stable across the v1.x line.

## Pointers

- **Project source / issues**:
  https://github.com/mandarnilange/corvee
- **Sibling skill (day-to-day agent loop)**:
  [`../corvee/SKILL.md`](../corvee/SKILL.md)
- **CLI reference (every verb's flags + output + exit codes)**:
  https://github.com/mandarnilange/corvee/blob/main/docs/cli-reference.md
- **Spec questions log (SQ-008 — init vs skill for agent-doc
  bootstrap)**:
  https://github.com/mandarnilange/corvee/blob/main/docs/spec-questions.md
