# CLI reference

Every verb the `corvee` binary supports. Each entry: synopsis →
flags → output payload → exit codes.

The full normative spec lives in `.spec/corvee-spec.md` § 15.
This document is the agent-facing distillation — same data, easier
to scan.

## Global conventions

```sh
task <verb> [args] [flags]
```

| Global flag | Purpose |
|---|---|
| `--pretty` | Indented JSON output (default is compact single-line) |
| `--text` | Human-readable text output instead of JSON |
| `--workspace <path>` | Override workspace auto-discovery |
| `--agent <id>` | Override agent ID (else `CORVEE_AGENT_ID`, else `local.json`) |
| `--operation-id <ULID>` | Idempotency key (auto-minted if absent) |
| `--quiet` | Suppress non-essential output |
| `--help` | Print help and exit |

**Output schema** — every command emits one JSON object on stdout:

```json
// success
{ "ok": true, "data": <command-specific payload> }

// failure
{ "ok": false, "error": { "code": "STRING_CODE", "message": "...", "details": {} } }
```

Errors go to **stdout** (parseable by agents); stderr is reserved for
diagnostic logs.

**Exit codes** — agents branch on these without parsing JSON:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error |
| 2 | Usage / argument error |
| 3 | Not found (item or workspace) |
| 4 | Version conflict (concurrent modification) |
| 5 | Claim conflict (already claimed, wrong lease, claim lost) |
| 6 | Schema validation failure |
| 7 | Dependency / integrity violation |
| 8 | Lock acquisition timeout |
| 9 | Incomplete operation — run `corvee recover` |

For a full incident-response guide per code, see
[references/error-codes.md](references/error-codes.md).

## Workspace lifecycle

### `corvee init [--name <NAME>] [--project <CODE>] [--agent-id <ID>]`

Bootstrap `.tasks/` in cwd. Optionally seeds an initial project.

- **Output**: `{ workspace_path, local_path }`
- **Errors**: 1 (workspace already exists; pass `--force` to re-init)

### `corvee validate`

Workspace-wide integrity check: schema conformance per item, parent
existence and type-correctness, no dependency cycles, no orphaned
aliases, index consistency, no orphaned operation files.

- **Output**: `{ ok: bool, issues: [{ item_id, code, message }, …] }`
- **Errors**: exit 0 even if `ok: false` — issues surface in payload

### `corvee reindex`

Rebuild the local `.tasks/index.json` cache from `items/` and event
shards. Useful after a manual edit or fresh clone.

- **Output**: `{ items_indexed, events_replayed, shards_read, was_stale }`

### `corvee summary [--project <CODE>] [--by <FIELD>]`

Pre-computed roll-up across the workspace. Cheap (reads index).

- **Flags**: `--by <type|status|kind|priority|assignee|project|epic>`
- **Output**: `{ counts, total, critical_path: [...], deliverables: [...] }`

### `corvee migrate`

Detect items with `schema_version` lower than the current spec
version and apply lazy migrations. Idempotent.

- **Output**: `{ migrated: [{ id, from_version, to_version }], skipped }`

## Item CRUD

### `corvee add <type> --title <S> [opts]`

Create a new item. `type` ∈ `project | epic | story | subtask`.
For non-project types, `--parent` is required.

- **Flags**: `--parent <ID>`, `--description <S>`, `--priority`,
  `--tags <CSV>`, `--depends-on <CSV>`, `--impact-files <CSV>`,
  `--impact-modules <CSV>`, `--estimated-loc <N>`, `--risk`,
  `--acceptance <S>` (repeatable), `--due <ISO>`,
  `--required-capability <S>` (repeatable)
- **Output**: full item JSON
- **Errors**: 2, 3, 6, 7

### `corvee list [filters]`

Query items. All filters AND together.

- **Flags**: `--status <CSV>`, `--type <CSV>`, `--project <CODE>`,
  `--parent <ID>`, `--assignee <ID>`, `--tag <CSV>`,
  `--unblocked`, `--unassigned`, `--limit <N>`, `--sort <FIELD>`,
  `--order <asc|desc>`
- **Output**: `{ count, items: [...] }`

### `corvee show <id>`

Full item JSON including journal.

- **Output**: full item
- **Errors**: 3

### `corvee update <id> [mutators] [--expect-version <N>]`

Generic mutation. `--expect-version` enables optimistic concurrency:
if the on-disk version differs, fail with exit 4.

- **Mutators**: `--status`, `--priority`, `--title`, `--description`,
  `--assignee`, `--due`, `--add-tag`, `--remove-tag`,
  `--add-impact-file`, `--remove-impact-file`, `--add-dep`,
  `--remove-dep`, `--add-acceptance`, `--remove-acceptance <INDEX>`,
  `--risk`
- **Output**: `{ item, prev_version, new_version }`
- **Errors**: 3, 4, 6

### `corvee delete <id> [--hard] [--cascade] [--force]`

Soft-delete (status=abandoned) by default. `--hard` removes the
JSON file (git history preserves it). `--cascade` includes children.

- **Output**: `{ deleted: ["<id>", ...], soft: bool }`
- **Errors**: 3, 7

### `corvee move <id> --to <parent-id>`

Re-parent. Computes new ID under target; rewrites all references;
appends old IDs to `aliases[]`. Atomic at workspace level.

- **Output**: `{ mapping: {"<old>": "<new>", ...}, items_updated }`
- **Errors**: 2, 3

### `corvee rename <id> --to <new-id>`

Same machinery as move with explicit target ID.

- **Output**: `{ old, new, aliases_added }`
- **Errors**: 1, 2, 3

### `corvee clone <id> [--to <parent>] [--with-children]`

Duplicate with fresh ID; resets status/journal/version. Children
cloned only with `--with-children`.

- **Output**: full new item (or `{ root, descendants }` with cascade)
- **Errors**: 3

### `corvee tree [<id>] [--max-depth <N>] [--status <CSV>]`

Hierarchical view. With no ID, shows all projects.

- **Output**: `{ roots: [<tree-node>, ...] }`
- **Errors**: 3

## Claim lifecycle (multi-agent coordination)

### `corvee claim <id> [--ttl-minutes <N>]`

Atomic claim by current agent. Writes a structured `claim` object
(`agent`, `lease_id`, `claimed_at`, `expires_at`) and sets
`status=claimed`. Succeeds only if currently unclaimed and
`status ∈ {backlog, ready}`. TTL default 60 min.

- **Output**: `{ id, agent, lease_id, claimed_at, expires_at, version }`
- **Agents must retain the `lease_id`** for subsequent operations.
- **Errors**: 3, 5, 1

### `corvee release <id> --lease-id <ID>`

Release current agent's claim. Reverts status to `ready`.

- **Output**: `{ id, version }`
- **Errors**: 3, 5

### `corvee heartbeat <id> --lease-id <ID>`

Refresh `expires_at` to `now + TTL`. Required periodically (default
30 min) to prevent reaping.

- **Output**: `{ id, claimed_at, expires_at }`
- **Errors**: 3, 5

### `corvee done <id> --lease-id <ID> [--note <S>] [--metadata <JSON>]`

Sets `status=done`, `completed_at=now`, clears the claim, optionally
appends a journal entry. `--metadata` accepts an arbitrary JSON
object (handoff payload).

- **Output**: full updated item
- **Errors**: 2, 3, 5

### `corvee journal <id> --note <S> [--event <T>] [--metadata <JSON>] [--lease-id <ID>]`

Append a journal entry. `--event` defaults to `note`. Other event
types: `progress`, `blocker`, `decision`, `handoff`. `--lease-id`
required if the item is currently claimed and `--event` is one of
`progress | blocker | decision`.

- **Output**: `{ id, journal_length }`
- **Errors**: 2, 3, 5

### `corvee next [filters] [--auto-claim]`

Pick the next item for the current agent. Selection order:

1. Filter by flags (status defaults to `ready`)
2. Drop items with unmet dependencies
3. Apply selection bonuses: items in
   `workspace.json:critical_path` jump above non-critical items at
   the same priority
4. Sort by priority desc → critical-path order asc → due_date asc →
   updated_at asc → id asc

- **Flags**: `--type <CSV>`, `--kind <CSV>`, `--project <CODE>`,
  `--tag <CSV>`, `--match-capabilities`, `--max-hours <N>`,
  `--affinity <AGENT-ID>`, `--auto-claim`
- **Output**: full item JSON (with `lease_id` if `--auto-claim`),
  or `null` if nothing available (still exit 0)
- **Errors**: 5

### `corvee reap`

Release expired claims (past `expires_at` plus 5-min skew tolerance).
Idempotent.

- **Output**: `{ reaped: [{ id, was_assigned_to, lease_id, expired_at }, ...] }`

### `corvee recover`

Detect orphaned operation files and roll forward. Auto-invoked by
`corvee list` / `corvee next` when an in-flight op is detected.

- **Output**: `{ recovered: [...], failed: [...] }`
- **Errors**: 1

## Distribution

### `corvee sync [--watch] [--interval <SEC>] [--auto-commit] [--message <S>]`

One-shot: `git pull --rebase` then `git push`. On conflict,
dispatches to deterministic 3-way merge.

`--watch` makes it long-running; loops every `--interval` seconds
(or `workspace.json:git.auto_sync_seconds`) until ctx is cancelled.

`--auto-commit` stages `.tasks/` and commits with `--message` (or a
default) before pulling.

- **Output**: `{ pulled, pushed, merged: { ... } | null }`
- **Errors**: 1, 2

### `corvee import <file> [--dry-run] [--prefix <CODE>]`

Bulk-create items from a planning JSON. Two formats:

1. **Native**: array of partial items matching the spec's schema.
2. **Phases-and-tasks**: AgentForge-style nested
   `{ phases: [{ id, name, tasks: [...] }] }`.

`--dry-run` validates without writing. Atomic: nothing written if
any item fails validation.

- **Output**: `{ created: [<id>...], skipped: [...], format }`
- **Errors**: 2, 6, 7

### `corvee export-plan [--project <CODE>] [--format <FORMAT>] [--out <PATH>]`

Inverse of `corvee import`. Three formats: `native`, `phases`,
`markdown`. Default `--out` is stdout.

- **Output**: writes to `--out`; returns `{ format, items, out }`
- **Errors**: 2, 3

### `corvee export --format <FORMAT> [--out <PATH>] [--include <CSV>]`

Export the workspace as a portable graph (D3, Cytoscape, Neo4j).
Three formats: `graph` (JSON Graph Format), `cypher`, `dot`.
`--include` filters edge types: `parent`, `dependency`, `blocks`,
`alias`.

- **Output**: writes to `--out`; returns `{ format, nodes, edges, out }`
- **Errors**: 2

## Observability

### `corvee watch [filters] [--since <TS>] [--format <FORMAT>]`

Stream events from the daily-sharded events log as they happen.
Local-only — to see remote events, run `corvee sync` periodically.

- **Flags**: `--filter <K=V>` (repeatable), `--id <ID-OR-PREFIX>`,
  `--since <ISO-OR-RELATIVE>`, `--format <jsonl|text|compact>`,
  `--limit <N>`, `--exit-on <EVENT-TYPE>`
- **Output**: streams matching events to stdout (one JSON per line)
- **Exit**: 0 on `--limit` / `--exit-on`; 130 on Ctrl+C
- **Errors**: 1, 2

### `corvee render [--out <DIR>] [--theme <NAME>]`

Generate the static HTML dashboard. Default `--out` is
`.tasks/dist/`. Produces `index.html`, `tree.html`, `summary.html`,
`assets/styles.css`, `assets/app.js`, `manifest.json`.

The manifest is shaped for Vercel/Netlify/etc.; hand the directory
to a deployment skill.

- **Output**: `{ out_dir, files: [...], manifest, stats }`
- **Errors**: 1

## Maintenance

### `corvee compact [--before <DATE>]`

Squash events into item state, archive old daily shards (default 90
days), prune resolved operation files, garbage-collect stale
aliases. Idempotent.

- **Output**: `{ shards_archived, ops_pruned, aliases_pruned, bytes_saved }`

### `corvee version`

Print binary version + build info.

- **Output**: `{ version, commit, built_at }`
