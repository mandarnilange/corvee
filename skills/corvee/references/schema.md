# Schema reference

Full per-Item schema as it lives on disk in `.tasks/items/<id>.json`.
Source of truth is spec §5; this is the agent-facing distillation.

## Item

| Field | Type | Required | Notes |
|---|---|---|---|
| `schema_version` | int | ✅ | Pinned per item; lazy-migrated on read. |
| `id` | string | ✅ | `PROJ-Eee[-Sss[-Ttt]]` (3–6 letter project, 2-digit numeric segments 01–99). |
| `type` | enum | ✅ | `project`, `epic`, `story`, `subtask`. |
| `kind` | enum |  | `feature`, `bug`, `chore`, `spike`, `docs`, `refactor`. |
| `project_id` | string |  | Root project ID (matches `id` for projects). |
| `parent_id` | string |  | Empty for projects. |
| `title` | string | ✅ | One-line summary. |
| `description` | string |  | Long-form prose. |
| `status` | enum | ✅ | See lifecycle below. |
| `priority` | enum |  | `critical`, `high`, `medium`, `low`. |
| `tags` | string[] |  | Free-form labels. |
| `deliverable` | string |  | Epic-only outcome statement. |
| `claim` | object |  | `{agent, lease_id, claimed_at, expires_at}`. |
| `required_capabilities` | string[] |  | Filter for `corvee next --match-capabilities`. |
| `created_at` | RFC3339 | ✅ | |
| `created_by` | string |  | Agent ID. |
| `created_by_role` | enum |  | `planner`, `executor`, `reviewer`, `human`. |
| `updated_at` | RFC3339 | ✅ | Bumped on every mutation. |
| `completed_at` | RFC3339 |  | Set when status flips to `done`. |
| `due_date` | RFC3339 |  | Optional deadline. |
| `version` | int | ✅ | Optimistic-concurrency counter; `--expect-version` checks it. |
| `impact` | object |  | `{files, modules, estimated_hours, estimated_loc, risk}`. |
| `dependencies` | string[] |  | IDs that must be `done` first. |
| `blocks` | string[] |  | Inverse of `dependencies`. |
| `aliases` | string[] |  | Prior IDs (after move/rename). |
| `acceptance_criteria` | string[] |  | Observable conditions for done. |
| `journal` | object[] |  | Append-only history. |

## Status lifecycle

```text
backlog → ready → claimed → in_progress → review → done
                       ↓          ↓           ↑
                       ↓        blocked → ready
                       ↓          ↑
                     ready ←──────┘
                       ↓
                  abandoned ← any status
```

Legal transitions:

- `backlog → ready`
- `ready → claimed`
- `claimed → ready` (release)
- `claimed → in_progress`
- `in_progress → review | blocked | done`
- `review → done | in_progress`
- `blocked → ready | in_progress`
- `* → abandoned`

Anything else is `ErrInvalidStatus`.

## Optimistic concurrency: `version` and `--expect-version`

Every mutation bumps `version` by 1. To guard against concurrent
writers, pass `--expect-version <N>` on `corvee update`:

```sh
corvee update DEMO-E01-S03 --status review --expect-version 5
# → exit 4 if on-disk version != 5; exit 0 + new_version=6 on success
```

Agents in a long-running loop should always pass `--expect-version`
to surface lost updates instead of silently overwriting them.

## Aliases resolution

`corvee move` / `corvee rename` append the old ID to `aliases[]` so
references in events, journals, and external systems still resolve.
The CLI looks up by `id`, then by any item with `<input> ∈ aliases`.

Pruning: stale aliases (whose target ID no longer exists in the
workspace) are gc'd by `corvee compact`. To preserve an alias forever,
keep referencing it from another item's `dependencies` or `blocks`.

## `agent_role` matrix

| Role | Reads | Writes |
|---|---|---|
| `planner` | All items | Create items, update priorities, set `critical_path`. |
| `executor` | Claimed items, dependencies | Status, journal, claim/release, subtasks of own claimed items. |
| `reviewer` | Items in `review` | Status, journal. |
| `human` | All | All. |

Roles are advisory — they communicate intent rather than enforce
permissions. Every CREATED or CLAIMED action is stamped with the
role at that moment so downstream consumers know provenance.

## Workspace metadata (`.tasks/workspace.json`)

Committed to git; safe to sync.

```json
{
  "schema_version": 1,
  "workspace_name": "demo",
  "default_project": "DEMO",
  "claim_ttl_minutes": 60,
  "heartbeat_interval_minutes": 30,
  "critical_path": ["DEMO-E01-S01", "DEMO-E01-S05"],
  "git": {
    "auto_commit": false,
    "auto_sync_seconds": null
  }
}
```

## Per-VM identity (`.tasks/local.json`)

Gitignored; never synced.

```json
{
  "default_agent": "executor-vm-1",
  "agent_role": "executor",
  "agent_capabilities": ["go", "sql", "fe"]
}
```

Resolution order: env (`CORVEE_AGENT_*`) → `local.json` → built-in
default.
