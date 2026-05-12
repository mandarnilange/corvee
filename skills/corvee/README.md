# corvee (skill)

A drop-in skill that teaches an AI agent how to use the `corvee` CLI —
a local-first, file-based task tracker for hierarchical, multi-agent
backlogs. The skill ships **only documentation** (`SKILL.md` plus
`references/`); the binary is installed separately by the user.

This layout matches the
[skills.sh](https://skills.sh/) / Anthropic Skills convention: drop
the folder into your harness's skills directory and the agent picks
it up.

## Layout

```text
corvee/
  SKILL.md              # the manifest (frontmatter + body)
  README.md             # this file
  references/           # loaded by agents on demand
    workflows.md
    schema.md
    error-codes.md
    topologies.md
    handoff-patterns.md
```

No binaries. No install scripts. The skill is a static documentation
bundle — the same five `references/` files specified in the project's
[spec §16.4](https://github.com/mandarnilange/corvee/blob/main/.spec/corvee-spec.md).

## Installing the skill

Use [skills.sh](https://skills.sh/) — one command, auto-detects your
harness (Claude Code, Cursor, Windsurf, …) and drops the folder in
the right place:

```sh
npx skills add mandarnilange/corvee
```

This installs both `corvee` (this skill) and the companion
`corvee-install` skill that walks an agent through binary install,
upgrade, and `CLAUDE.md` / `AGENTS.md` bootstrap.

After install, the agent reads `SKILL.md` and triggers automatically
on the phrases listed in its frontmatter.

### Local development

If you're editing the skill itself, symlink your working copy into
the harness's skills dir so changes show up immediately:

```sh
ln -s "$(pwd)/skills/corvee" ~/.claude/skills/corvee
```

### Offline / air-gapped install

When `npx` can't reach the registry, fall back to the bundled
tarball: `make skill-tarball` produces `dist/corvee-skill.tar.gz`
containing both `corvee/` and `corvee-install/`; extract it directly
into your harness's skills dir.

## Installing the `corvee` binary

The skill says "shell out to `corvee`" — that means the binary must
be on the agent's `$PATH` first. Three install paths:

### Option A — `go install` (Go ≥1.26)

```sh
go install github.com/mandarnilange/corvee/cmd/corvee@latest
```

This compiles the latest tag's source and drops `corvee` into
`$GOPATH/bin` (typically `~/go/bin`). Add that to `$PATH`.

To pin a specific version:

```sh
go install github.com/mandarnilange/corvee/cmd/corvee@v1.0.0
```

### Option B — pre-built release binary

Pre-built per-platform binaries are uploaded to
[GitHub Releases](https://github.com/mandarnilange/corvee/releases)
on every `v*` tag.

```sh
# Pick the platform matching your host (linux/amd64, linux/arm64, darwin/arm64).
PLATFORM=linux-amd64
VERSION=v1.0.0
curl -LO "https://github.com/mandarnilange/corvee/releases/download/${VERSION}/corvee-${PLATFORM}"
curl -LO "https://github.com/mandarnilange/corvee/releases/download/${VERSION}/corvee-${PLATFORM}.sha256"

# Verify integrity.
shasum -a 256 -c "corvee-${PLATFORM}.sha256"

chmod +x "corvee-${PLATFORM}"
sudo install "corvee-${PLATFORM}" /usr/local/bin/corvee
corvee version --pretty
```

### Option C — build from source

```sh
git clone https://github.com/mandarnilange/corvee
cd corvee
make build
sudo install bin/corvee /usr/local/bin/corvee
```

## Verifying

```sh
corvee version --pretty
# {"ok":true,"data":{"version":"v1.0.0","commit":"…","built_at":"…"}}
```

## Workspace bootstrap

The first time the agent runs against a project, it'll suggest:

```sh
corvee init --project <CODE> --agent-id <YOUR-NAME>
```

…to create `.tasks/`. From there, `git push` distributes the
workspace; collaborators clone and run their own
`corvee init --agent-id <DIFFERENT-NAME>` to register a per-VM
identity (gitignored, doesn't conflict).

## Versioning

This skill targets `corvee` binary's `v1.0`+ API. If the binary's API
changes incompatibly, the skill bumps a major version and the
matching release notes call out the migration.

## Pointers

- **Project source / issues**:
  https://github.com/mandarnilange/corvee
- **CLI reference (every verb's flags + output + exit codes)**:
  https://github.com/mandarnilange/corvee/blob/main/docs/cli-reference.md
- **Recipes (multi-VM setups, GitHub Actions, plugin samples)**:
  https://github.com/mandarnilange/corvee/blob/main/docs/recipes.md
- **Full behavioral spec**:
  https://github.com/mandarnilange/corvee/blob/main/.spec/corvee-spec.md
