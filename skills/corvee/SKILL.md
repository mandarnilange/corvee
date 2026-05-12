---
name: corvee
description: Manage hierarchical, multi-agent task backlogs through a local CLI. Use this skill whenever the user mentions tasks, todos, backlog, work items, sprints, epics, stories, subtasks, or asks "what should I work on next." Also use it whenever an agent needs to claim work in a coordinated multi-agent setup, record progress on an in-flight task, mark something done, or answer questions about project status. Even if the user doesn't say "task tracker" explicitly, reach for this skill any time work is being created, picked up, updated, completed, or reviewed across a project.
---

# corvee

Coordinate hierarchical, multi-agent backlogs through a local-first
CLI. State lives in `.tasks/` in the user's project; sync between
machines is plain `git push` / `git pull`. The skill teaches you how
to invoke it; the actual binary is `corvee`, installed separately by
the user — see "Setup check" below.

## When to trigger this skill

Reach for this skill on any of these cues, even when the phrase
"task tracker" isn't used:

| User says / context | Action |
|---|---|
| "track this todo" / "add a task for…" / "remind me to…" | `corvee add story --parent <epic> --title "…"` |
| "what should I work on next?" | `corvee next --type story --auto-claim` |
| pasting a list of TODOs from a meeting | `corvee import` (after building a plan JSON) |
| "what's the status of project X?" | `corvee summary --project X --pretty` |
| "what's blocked?" | `corvee list --status blocked` |
| "mark this done" / "I finished X" | `corvee done <id> --lease-id <LEASE>` |
| "write up a deploy board" | `corvee render` then hand to a deployment skill |
| user works in a `.tasks/` repo without saying anything | check workspace via `corvee list --limit 1` |
| any multi-agent coordination scenario | use the lease-id discipline below |

If the user is new to the tool, run the **Setup check** before
issuing any other command.

## Setup check

Before any other operation, confirm the binary and workspace exist:

1. **Is `corvee` on PATH?**

   ```sh
   command -v corvee >/dev/null && corvee version --pretty
   ```

   If absent, the sibling **`corvee-install`** skill walks through
   install + verify + `corvee init` end-to-end (and also handles
   upgrades and the `CLAUDE.md` / `AGENTS.md` bootstrap block). If
   you're operating without that skill, install via one of:

   - **`go install`** (preferred, Go ≥1.26):
     `go install github.com/mandarnilange/corvee/cmd/corvee@latest`
   - **Pre-built binary**: download from
     [GitHub Releases](https://github.com/mandarnilange/corvee/releases)
     for `linux/amd64`, `linux/arm64`, or `darwin/arm64`. Verify
     SHA-256, `chmod +x`, place on PATH.
   - **From source**: clone
     `https://github.com/mandarnilange/corvee`,
     run `make build`, install `bin/corvee`.

2. **Does a `.tasks/` workspace exist here?**

   ```sh
   corvee list --limit 1
   ```

   - Exit 3 → no workspace. Ask the user before running
     `corvee init --project <CODE> --agent-id <NAME>`.
   - Exit 0 → workspace is good; proceed.

The user is responsible for the install. The skill walks them
through it but never auto-installs without confirmation.

## Standard agent workflow (executor loop)

Most common case for AI agents:

```text
0. corvee sync                                      # pull peer changes
1. corvee list --limit 1                            # confirm workspace exists
2. corvee next --type story --auto-claim --match-capabilities
                                                  # → returns {item, lease_id}
3. corvee heartbeat <id> --lease-id <LEASE>         # every 30 min
4. corvee journal <id> --note "…" --event progress \
            --metadata '{"step":"design","files":["x.go"]}' \
            --lease-id <LEASE>
5. corvee sync                                      # ~every 60s during long tasks
6. corvee done <id> --lease-id <LEASE> --note "summary" \
            --metadata '{"changed_files":[…],"tests_passed":18}'
7. corvee sync
8. Loop back to step 0
```

**Always retain the `lease_id`** returned by `corvee claim` /
`corvee next --auto-claim`. Every claim-scoped operation
(`heartbeat`, `journal`, `done`, `release`) requires it. The CLI
rejects with exit 5 if your lease doesn't match the on-disk lease.

The planner loop is symmetric:

```text
1. corvee summary                                   # read state
   corvee list --status blocked                     # surface stuck work
   (optional) corvee watch --filter event=blocker & # background reactor
2. corvee add … (or corvee import plan.json)          # add work
3. (optional) edit critical_path in workspace.json
4. corvee sync                                      # publish — executors see it
5. Observe via summary + list, replan, loop.
```

See `references/workflows.md` for both loops with worked examples.

## Quick reference

| Verb | Purpose |
|---|---|
| `corvee init --project <CODE>` | Bootstrap `.tasks/` and seed a project |
| `corvee add <type> --title <S> [--parent <ID>]` | Create item |
| `corvee list [--status …] [--type …]` | Filter/query items |
| `corvee next [--auto-claim]` | Pick next-best ready item |
| `corvee claim <id> [--ttl-minutes N]` | Atomic claim; returns `lease_id` |
| `corvee heartbeat <id> --lease-id <ID>` | Refresh lease |
| `corvee done <id> --lease-id <ID> --note "…"` | Complete |
| `corvee journal <id> --note "…" --event <type>` | Append history |
| `corvee sync` | `git pull --rebase` + `git push` |
| `corvee summary` | Pre-computed roll-up (cheap, reads index) |
| `corvee render --out .tasks/dist` | Generate static HTML board |
| `corvee watch [--filter …]` | Real-time event stream |

Full verb reference: see the project's
[docs/cli-reference.md](https://github.com/mandarnilange/corvee/blob/main/docs/cli-reference.md)
or the spec's §15.2.

## Error handling guide

Exit codes are stable; branch on them without parsing JSON:

- **2 (usage)**: bad flag or arg — re-read the help text.
- **3 (not found)**: item or workspace missing. For workspace, ask
  the user about `corvee init`. For item, check the ID (alias
  resolution may help).
- **4 (version conflict)**: another writer mutated between read and
  write. Reload via `corvee show <id>`, re-apply, retry.
- **5 (claim conflict)**: another agent has the lease. **Do not
  retry** — call `corvee next` to pick a different item. If
  `details.claim_lost: true` is set, your claim was deterministically
  resolved against (lower lease wins).
- **6 (schema invalid)**: malformed input. Fix and retry. For
  `corvee import`, the whole batch is rejected atomically.
- **7 (integrity)**: dependency cycle, orphaned reference, or guard
  refused. Often resolvable with `--force` or `--cascade`.
- **8 (lock timeout)**: per-item lock contention. Retry shortly.
- **9 (incomplete operation)**: a multi-file op crashed. Run
  `corvee recover`.

## Multi-agent coordination notes

- **Heartbeat cadence**: every 30 min by default
  (`workspace.heartbeat_interval_minutes`). Skip and your claim gets
  reaped.
- **Sync cadence**: planner syncs immediately after a batch;
  executors sync every 30s–5min and after every `done`/`update`.
- **Claim conflict resolution**: when two agents claim the same item
  on divergent branches, the merge picks the **lower `lease_id`**
  (ULIDs are sortable; earlier-issued lease wins). The loser sees
  exit 5 with `details.claim_lost: true` on its next operation.
- **Plugins under agent mode**: when invoked from an LLM harness,
  set `CORVEE_AGENT_MODE=1` (or pass `--agent`). Only plugins with
  `agent_safe: true` then run; interactive plugins (browsers,
  prompts) are skipped.

## Deployment via vercel-style skill

`corvee render --out .tasks/dist/` produces a static-hostable
directory plus `manifest.json`. To publish:

1. `corvee render --out .tasks/dist/`
2. Read `.tasks/dist/manifest.json`.
3. Invoke a deployment skill (vercel, netlify, etc.) with
   `path: .tasks/dist/`, passing the manifest verbatim.
4. Capture the returned URL.
5. `corvee journal <project-id> --note "Board deployed: <URL>"
   --event decision`.
6. Report the URL.

This skill never deploys directly — handing off to a deployment
skill is the contract.

## Pointers to references/

When you need depth on a specific topic, read the matching reference
file on demand (don't preload all of them):

- `references/workflows.md` — multi-step recipes (planner-executor,
  sprint planning, mid-task handoff, crash recovery, executor
  onboarding).
- `references/schema.md` — every Item field with semantics, allowed
  values, examples; alias resolution; `version` + `--expect-version`
  semantics; `agent_role` matrix.
- `references/error-codes.md` — every exit code expanded with
  cause, recovery recipe, example.
- `references/topologies.md` — planner-executor, peer executors,
  reviewer-in-the-loop; when to use which, sync cadences, failure
  modes.
- `references/handoff-patterns.md` — common conventions for
  `journal[].metadata` payloads (changed_files, tests_run,
  decisions, artifact paths, metrics).

## What this skill is NOT

- **Not a binary distribution.** Install via `go install` or
  GitHub Releases.
- **Not a daemon or server.** Every `corvee <verb>` reads/writes
  `.tasks/` and exits.
- **Not a deployment provider.** `corvee render` produces a static
  artifact; deployment is a different skill's job.
- **Not network-aware** beyond `git pull` / `git push`. No HTTP
  clients, no LLM calls in core.
