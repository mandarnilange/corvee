# Getting started

This guide walks you from "no `corvee` binary on disk" to "I claimed
my first item, marked it done, and synced to a remote." About 10
minutes end-to-end.

---

## 1. Install the binary

### Option A — `go install` (Go ≥1.26)

```sh
go install github.com/mandarnilange/corvee/cmd/corvee@latest
# (uses $GOBIN, falls back to $GOPATH/bin — typically ~/go/bin)
```

To pin a specific version: `…/cmd/corvee@v1.0.0`.

### Option B — pre-built release binary

```sh
PLATFORM=linux-amd64                    # or linux-arm64, darwin-arm64
VERSION=v1.0.0
curl -LO "https://github.com/mandarnilange/corvee/releases/download/${VERSION}/corvee-${PLATFORM}"
curl -LO "https://github.com/mandarnilange/corvee/releases/download/${VERSION}/corvee-${PLATFORM}.sha256"
shasum -a 256 -c "corvee-${PLATFORM}.sha256"
chmod +x "corvee-${PLATFORM}"
sudo install "corvee-${PLATFORM}" /usr/local/bin/corvee
```

### Option C — build from source

Prerequisites: Go ≥1.26, `make`, a POSIX shell, `git`.

```sh
git clone https://github.com/mandarnilange/corvee
cd corvee
make build                    # produces bin/corvee
sudo install bin/corvee /usr/local/bin/corvee
```

### Verify

```sh
corvee version --pretty
```

### (Optional) install the agent skills

Two skills ship with the repo:

| Skill | When the agent reaches for it |
|---|---|
| `corvee` | Day-to-day agent loop: claim, heartbeat, journal, done, sync. |
| `corvee-install` | Onboarding: install the binary, run `corvee init`, version-check, in-place upgrade, append a "Using corvee" block to `CLAUDE.md` / `AGENTS.md` so future agents pick the tool up automatically. |

Install both via [skills.sh](https://skills.sh/) — one command,
auto-detects your harness (Claude Code, Cursor, Windsurf, …):

```sh
npx skills add mandarnilange/corvee
```

Both skills are pure documentation — the agent reads `SKILL.md` on
each invocation that matches a trigger phrase (e.g. "what should I
work on?", "install corvee") and shells out to the `corvee` binary
you installed above.

For local development on the skills themselves (so edits to
`skills/*` show up immediately in the harness), symlink instead:

```sh
ln -s "$(pwd)/skills/corvee"         ~/.claude/skills/corvee
ln -s "$(pwd)/skills/corvee-install" ~/.claude/skills/corvee-install
```

---

## 2. Bootstrap a workspace

Pick the directory that should own the workspace. For a code
repository, this is usually the repo root. For a planning project, a
dedicated directory works fine.

```sh
cd ~/projects/my-app
corvee init --project MYAPP --agent-id alice
```

Output (single-line JSON; pass `--pretty` for indented):

```json
{
  "ok": true,
  "data": {
    "workspace_path": ".../my-app/.tasks/workspace.json",
    "local_path":     ".../my-app/.tasks/local.json"
  }
}
```

`corvee init` creates:

- `.tasks/items/` — one JSON per work item (committed)
- `.tasks/events/` — daily-sharded JSONL audit log (committed)
- `.tasks/hooks/` — your shell hooks live here (committed)
- `.tasks/operations/` — multi-file-op WAL (gitignored)
- `.tasks/locks/` — per-item file locks (gitignored)
- `.tasks/workspace.json` — committed workspace config
- `.tasks/local.json` — per-VM agent identity (gitignored — your
  alice doesn't merge-conflict with bob's bob)
- `.tasks/.gitignore` — encodes the gitignored bits above

Verify:

```sh
corvee list                     # exit 0, empty list
```

> **Tip**: in a git repo, the workspace's content lives at
> `.tasks/items/*.json` and `.tasks/events/*.jsonl`. The first
> `git add .tasks && git commit && git push` distributes it.

---

## 3. Plan a tiny project

Items have four hierarchical types — `project → epic → story →
subtask`. IDs encode the position: `MYAPP`, `MYAPP-E01`,
`MYAPP-E01-S03`, `MYAPP-E01-S03-T02`.

```sh
# Create the project (ID = the project code).
corvee add project --project MYAPP --title "My App"

# Add an epic (auto-numbered E01).
corvee add epic --parent MYAPP --title "Authentication"

# Add three stories under the epic (auto-numbered S01..S03).
corvee add story --parent MYAPP-E01 --title "Login handler" \
  --priority high --acceptance "POST /login returns JWT" \
  --acceptance "rejects bad credentials with 401"

corvee add story --parent MYAPP-E01 --title "Refresh tokens" \
  --priority medium --depends-on MYAPP-E01-S01

corvee add story --parent MYAPP-E01 --title "Logout / revoke" \
  --priority low --depends-on MYAPP-E01-S02
```

Confirm:

```sh
corvee tree --pretty            # see the hierarchy
corvee list --status backlog --pretty
```

---

## 4. Promote to ready and pick the next item

Items start as `backlog`. Promote each story to `ready` so executors
can claim them:

```sh
corvee update MYAPP-E01-S01 --status ready
corvee update MYAPP-E01-S02 --status ready
corvee update MYAPP-E01-S03 --status ready
```

Now pick the next-best item for the current agent:

```sh
corvee next --type story --auto-claim --pretty
```

Output (truncated):

```json
{
  "ok": true,
  "data": {
    "item": {
      "id": "MYAPP-E01-S01",
      "status": "claimed",
      "claim": {
        "agent": "alice",
        "lease_id": "01HX9KP3...",
        "claimed_at": "2026-05-06T12:00:00Z",
        "expires_at": "2026-05-06T13:00:00Z"
      },
      "version": 2
    },
    "lease_id": "01HX9KP3..."
  }
}
```

**Save the `lease_id`** — every claim-scoped operation
(`heartbeat`, `journal`, `done`, `release`) requires it. The CLI
rejects with exit 5 if the on-disk lease differs from yours.

`corvee next` enforces:

- `status=ready`
- unmet dependencies are dropped
- `--match-capabilities` filters items whose `required_capabilities`
  exceed your `agent_capabilities` (from `local.json` or
  `CORVEE_AGENT_CAPABILITIES`)
- items in `workspace.json:critical_path` jump above non-critical
  items at the same priority

---

## 5. Work the item

Move it from `claimed` → `in_progress`:

```sh
corvee update MYAPP-E01-S01 --status in_progress
```

Heartbeat every 30 minutes (workspace default
`heartbeat_interval_minutes`):

```sh
corvee heartbeat MYAPP-E01-S01 --lease-id "$LEASE"
```

Record progress as you go. The `--metadata` payload is structured
handoff data — see `references/handoff-patterns.md` for conventions.

```sh
corvee journal MYAPP-E01-S01 \
  --note "implemented happy path" --event progress \
  --metadata '{"files":["auth/login.go"],"tests_passed":4}' \
  --lease-id "$LEASE"
```

When the work is done:

```sh
corvee done MYAPP-E01-S01 \
  --lease-id "$LEASE" \
  --note "ships" \
  --metadata '{"changed_files":["auth/login.go","auth/login_test.go"],"tests_passed":12}'
```

The Claim is cleared on done. The journal entry plus metadata is
preserved forever.

---

## 6. Sync with the team

Stage, commit, push:

```sh
corvee sync --auto-commit --message "claim+complete login handler"
```

`corvee sync` runs `git pull --rebase` then `git push`. On conflict it
dispatches to the deterministic 3-way merge (lower lease wins for
divergent claims; status=latest version; tags+journal=union; prose
fields surface a `.conflict` file for human review).

For a long-lived loop:

```sh
corvee sync --watch --interval 60
```

Exits 130 on Ctrl+C, 0 on clean shutdown.

---

## 7. Build a static dashboard

Generate a hostable HTML view of the workspace plus a deploy
manifest:

```sh
corvee render --out .tasks/dist
```

Hand the directory to a deployment skill (Vercel, Netlify, GitHub
Pages, S3) — `manifest.json` follows their expected schema.

---

## What next?

- **[docs/recipes.md](recipes.md)** — multi-VM topologies, GitHub
  Actions integration, Claude Code / Cursor / MCP setups, plugin
  recipes.
- **[docs/cli-reference.md](cli-reference.md)** — every verb's
  flags, output, and exit codes.
- **[docs/architecture.md](architecture.md)** — how the layers work
  together and why they're divided that way.
- **[references/error-codes.md](../references/error-codes.md)** — what
  each exit code means and how to recover.
- **[CLAUDE.md](../CLAUDE.md)** — if you're going to contribute code,
  this is the contributor contract.
