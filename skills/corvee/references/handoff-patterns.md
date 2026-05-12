# Handoff patterns

The `--metadata` payload on `corvee done`, `corvee journal`, and any
mutating verb is intentionally schema-less — agents attach whatever
they need. To make handoffs interoperable, this skill records the
conventions teams have settled on. None are enforced by the schema;
following them lets downstream agents reliably parse the trail.

## Common keys

| Key | Type | Purpose | Example |
|---|---|---|---|
| `changed_files` | string[] | Files modified in this step. | `["auth.go","auth_test.go"]` |
| `tests_run` | int / object | Test counts (or per-suite breakdown). | `{"unit":18,"integration":4}` |
| `tests_passed` | int | Subset that passed. | `18` |
| `tests_failed` | int | Subset that failed. | `0` |
| `decisions` | string[] | Notable choices made; the next agent should respect or revisit. | `["use HMAC SHA256 over RSA"]` |
| `artifacts` | object[] | Produced artifacts and their locations. | `[{"kind":"dashboard","path":".tasks/dist"}]` |
| `metrics` | object | Domain-specific KPIs. | `{"latency_p95_ms":42}` |
| `risks` | object[] | Risks discovered, with severity. | `[{"text":"missing rate limit","severity":"medium"}]` |
| `next_step` | string | Suggested next action when handing off mid-task. | `"add tests for refresh path"` |
| `files_touched` | string[] | (handoff variant) Files in flight, possibly uncommitted. | `["auth.go"]` |
| `time_spent_minutes` | int | Coarse effort tracking. | `45` |
| `external_refs` | object[] | Links to PRs, issues, dashboards, traces. | `[{"kind":"pr","url":"..."}]` |

## When marking done

```sh
corvee done <id> --lease-id <LEASE> --note "Handler ships clean" \
  --metadata '{
    "changed_files": ["auth.go", "auth_test.go", "router.go"],
    "tests_run": 22,
    "tests_passed": 22,
    "tests_failed": 0,
    "decisions": [
      "use HMAC SHA256 (matches existing token helper)",
      "rate-limit defers to middleware in router.go"
    ],
    "artifacts": [
      {"kind": "coverage", "path": "coverage.out", "value_pct": 91.2}
    ],
    "external_refs": [
      {"kind": "pr", "url": "https://example.com/pr/142"}
    ]
  }'
```

## When recording progress

```sh
corvee journal <id> --note "Implemented refresh path" \
  --event progress \
  --metadata '{
    "step": "refresh",
    "files_touched": ["auth.go"],
    "tests_run": {"unit": 6}
  }' \
  --lease-id <LEASE>
```

## When handing off mid-task

The new owner picks this up via `corvee next` or by inspecting the
journal directly. Be specific about state — uncommitted files, half-
finished refactors, pending decisions.

```sh
corvee journal <id> --note "Stopping at step 3 of 5" \
  --event handoff \
  --metadata '{
    "next_step": "wire HMAC verification into middleware",
    "files_touched": ["auth.go", "middleware/auth.go"],
    "decisions": ["use HMAC SHA256"],
    "risks": [
      {"text": "no rate limit yet", "severity": "medium"}
    ]
  }' \
  --lease-id <LEASE>
corvee release <id> --lease-id <LEASE>
corvee sync
```

## When recording a blocker

```sh
corvee update <id> --status blocked --lease-id <LEASE>
corvee journal <id> --note "Waiting on schema review" \
  --event blocker \
  --metadata '{
    "blocked_on": ["DEMO-E01-S07"],
    "external_refs": [{"kind": "issue", "url": "..."}]
  }' \
  --lease-id <LEASE>
corvee release <id> --lease-id <LEASE>
corvee sync
```

The planner can then `corvee watch --filter event=blocker` and react
in real time.

## When recording a decision

For irreversible choices (architecture, vendor selection, schema
migrations) document the rationale so the next agent doesn't second-
guess.

```sh
corvee journal <id> --note "Chose Postgres over SQLite for sharding" \
  --event decision \
  --metadata '{
    "alternatives_considered": ["SQLite", "BoltDB"],
    "rationale": "shard-by-tenant requires server-side query planning",
    "reversibility": "expensive"
  }' \
  --lease-id <LEASE>
```

## What downstream skills assume

The `vercel` deployment skill (and any other skills consuming the
journal) read these conventions, not arbitrary keys. Stick to the
table above when you can; only invent new keys when the existing
ones genuinely don't fit.
