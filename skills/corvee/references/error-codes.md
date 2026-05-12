# Error codes

Every exit code, with cause, recovery recipe, and a worked example.

| Code | Sentinel | Meaning |
|---|---|---|
| 0 | — | Success |
| 1 | (other) | Generic error |
| 2 | `ErrUsage` | Argument or flag error |
| 3 | `ErrNotFound` | Item or workspace missing |
| 4 | `ErrVersionConflict` | Concurrent modification — version mismatched |
| 5 | `ErrAlreadyClaimed`, `ErrLeaseMismatch`, `ErrClaimLost` | Claim conflict |
| 6 | `ErrSchemaInvalid` | JSON Schema validation failure |
| 7 | `ErrIntegrityViolated` | Dependency / orphan / dependent guard |
| 8 | `ErrLockTimeout` | Per-item lock acquisition timeout |
| 9 | `ErrPendingOperation` | Multi-file op crashed; run `corvee recover` |

## Code 2 — usage error

You passed an unknown flag, missing required arg, or malformed input.

```sh
$ corvee done                     # missing <id>
exit 2
```

**Recovery**: read `corvee <verb> --help`, fix the invocation.

## Code 3 — not found

The workspace doesn't exist, or the requested item doesn't exist
(after alias resolution).

```sh
$ corvee list                     # in a directory with no .tasks/
exit 3
```

**Recovery**:

- Workspace missing: `corvee init` (ask the user first).
- Item missing: re-fetch IDs via `corvee list`. If you suspect a
  rename, search by alias: `corvee show <suspected-old-id>` (the CLI
  resolves aliases automatically).

## Code 4 — version conflict

Optimistic concurrency rejected your write. Another agent or your own
prior process bumped `version` between your read and your write.

```sh
$ corvee update DEMO-E01-S03 --status review --expect-version 5
# on-disk version is 7
exit 4
```

**Recovery**: re-read with `corvee show <id>`, re-apply your changes,
retry with the new `--expect-version`. If the change conflicts with
the new on-disk state, surface to the user.

## Code 5 — claim conflict

Three flavours, all map to 5:

- `ErrAlreadyClaimed` — `corvee claim` lost the race; another agent
  has the lease.
- `ErrLeaseMismatch` — your `--lease-id` doesn't match the on-disk
  claim. Either you have a stale lease or you never had one.
- `ErrClaimLost` — distributed merge resolved against you (lower
  lease wins per spec §6 Layer 8). The response includes
  `details.claim_lost: true`.

```sh
$ corvee heartbeat DEMO-E01-S03 --lease-id 01HX...
exit 5
# stderr: claim lost in merge
```

**Recovery**: do **not** retry on the same item — your claim is
gone. Drop the work and call `corvee next` to pick a different item.
If you have local work-in-progress, journal it for the new owner via
`corvee journal --event handoff` before moving on (the new owner will
see your handoff in the item's history).

## Code 6 — schema invalid

JSON Schema rejected an input or stored item. Common causes:

- A field has the wrong type (e.g. `priority: 5` instead of `"high"`).
- A required field is missing.
- An enum value isn't in the allowed set.
- An ID doesn't match the `PROJ-Eee[-Sss[-Ttt]]` grammar.

**Recovery**: read the JSON error message — it names the field. Fix
and retry. For `corvee import`, the whole batch is rejected atomically;
fix the offending item and re-run.

## Code 7 — integrity violation

Workspace-level rule broken:

- Dependency cycle (A depends on B which depends on A).
- Orphaned reference (parent ID doesn't exist).
- Dependent guard (deleting an item that other items list in
  `dependencies` or `blocks`).
- Schema version mismatch (item written by a newer binary).

**Recovery**:

- Cycles: read the error, restructure the graph.
- Orphans: `corvee validate` lists every orphan; either re-create the
  parent or remove the orphans.
- Dependent guard: pass `--force` on `corvee delete` to override (with
  a warning) or re-target the dependents first.

## Code 8 — lock timeout

Per-item file lock contention. Another invocation is mid-write.

**Recovery**: retry with backoff. Locks are short-lived (sub-second
in practice), so a single retry usually works.

## Code 9 — incomplete operation

A multi-file operation (move, rename, delete --cascade,
clone --with-children) crashed mid-flight. The orphaned operation
file is in `.tasks/operations/`.

```sh
$ corvee list
exit 9
# stderr: incomplete operation; run corvee recover
```

**Recovery**: `corvee recover`. It rolls forward from the next undone
step. If a step is genuinely unsafe to roll forward, the op is
marked `failed` and surfaced in `corvee validate`.
