# AI-Native Task Tracking System — Specification

## 1. Overview

A local-first, file-based task tracking system designed for orchestrating multiple AI agents working concurrently on a shared backlog. Distributed via git, queryable via CLI, viewable via a generated HTML dashboard. Packaged as a portable skill for agent systems.

**What it does.** Coordinates agent work: claim, lease, heartbeat, journal, dependency, status, audit, sync. Agents and humans put tasks into the system; agents and humans take work out; the tool ensures the middle is safe under concurrent multi-agent execution.

**What it does not do.** It does not generate task content, run code, deploy artifacts, query the web, or call LLMs. PRD parsing, task breakdown, complexity ranking, auto-tagging, code execution, and worktree management all happen *outside* the tool — by humans, by planner agents, or by sibling skills. The tool's responsibility starts at `corvee add` / `corvee import` and ends at `corvee done`.

**Design principles**

- **Pure coordination**: We coordinate work; we don't invent it. Anything that creates the *content* of a task is out of scope.
- **Local-first**: All state lives in plain files in a project directory. No server, no daemon.
- **Git-native**: Distribution, history, and cross-machine sync come from the host repo's git.
- **Agent-friendly**: Every operation is a script with deterministic JSON output and stable exit codes. The CLI is the agent interface.
- **Optimistic concurrency**: A `version` field + atomic file rename handle multi-agent races without locks.
- **Zero LLM in core**: The CLI never makes network calls or invokes language models. Enrichment that needs an LLM is a user-built plugin or a sibling skill.
- **Minimal dependencies**: Go stdlib + a small set of audited packages (cobra for CLI, gojsonschema for validation). MIT-licensed end-to-end.

### 1.1 Scope and non-goals

In scope:

- Hierarchical task storage (project → epic → story → subtask)
- Atomic claim with lease IDs, heartbeats, TTL-based reaping
- Distributed multi-writer coordination via git (deterministic claim-conflict merge)
- Idempotent operations, operation journal (WAL), sharded events log
- Querying, filtering, dependency resolution, capability matching
- Bulk import/export of plans (interchange format only — we don't generate plans)
- HTML dashboard generation (read-only artifact for handoff to a deploy skill)
- Real-time event streaming for local consumers (`corvee watch`)

Explicit non-goals:

- **PRD parsing or task generation from prose.** Tasks arrive already specified.
- **Complexity scoring, auto-tagging, dependency suggestion, summarization.** These need an LLM; not our job.
- **Live web research / "what are best practices for X" lookups.** Out of scope.
- **Code execution, test running, build orchestration.** Tasks describe work; agents do work elsewhere.
- **Worktree lifecycle** (create/cd/destroy git worktrees per task). A coding-agent harness can wrap our claim verb if it wants this.
- **Deployment.** `corvee render` produces a static `dist/`; a separate skill (e.g. vercel skill) deploys it.
- **Real-time push across machines** (websockets, pub/sub). Git is eventually consistent; that's by design.
- **Hosted SaaS layer.** Local-first only.
- **Authentication beyond what git provides.** Repo permissions are the auth boundary.
- **MCP server in v1.** The CLI is already the agent interface. MCP can be added later as an optional adapter (`bin/corvee-mcp`) that's a thin wrapper over the existing verbs — no special status, no different code paths.

If a feature would require us to call an LLM, generate prose, run user code, or reach the network beyond `git push`/`git pull`, it is out of scope. The right home for it is a sibling skill that calls our CLI.

---

## 2. Architecture

The implementation language is **Go (≥1.22)**. Rationale: single static binary distribution, fast cold start (~5ms), POSIX file primitives in stdlib, trivial cross-compilation for the multi-VM topology. The full language decision is recorded in §13 (Tech Stack & Dependencies); the rest of this section assumes Go.

The codebase follows **clean architecture** (Uncle Bob's variant): dependencies point inward only, the domain is independent of frameworks and I/O, and each layer is independently testable. Concretely:

```
  ┌─────────────────────────────────────────────────────────┐
  │  cmd/corvee/                  ← CLI entrypoint (main)     │
  │   ↓ depends on                                          │
  │  internal/cli/              ← CLI verbs (cobra wiring)  │
  │   ↓ depends on                                          │
  │  internal/usecase/          ← Application logic         │
  │   ↓ depends on (interfaces) │  (orchestrates domain)    │
  │  internal/domain/           ← Pure business logic       │
  │   ↑ implemented by                                      │
  │  internal/adapter/          ← I/O, file system, git     │
  │   ↑ used by usecase via interfaces                      │
  └─────────────────────────────────────────────────────────┘
```

**The dependency rule.** Inner layers never import outer layers. `domain` imports nothing from this codebase. `usecase` imports only `domain`. `adapter` imports `domain` (to satisfy interfaces) but not `usecase`. `cli` imports `usecase`. `cmd` imports everything to wire it together.

### 2.1 Layers in detail

**`internal/domain/`** — pure types and rules. No file I/O, no syscalls, no time, no randomness. Just structs, validation, business invariants. Examples:
- `Item` struct (the schema from §5)
- ID parsing and validation (`ParseID`, `ChildID`, `ParentOf`)
- Status transition rules (which transitions are legal)
- Lease validity logic (given a lease and a clock, is it expired?)
- Dependency cycle detection over an in-memory graph
- Schema validation

This layer is 100% unit-testable with no fixtures, no temp directories, no goroutines. Every function is `f(input) → output` or `f(input) → error`.

**`internal/usecase/`** — application orchestration. One file per CLI verb, exposing one exported function. Examples:
- `usecase.Claim(ctx, deps, id, agent) → (Item, LeaseID, error)`
- `usecase.Next(ctx, deps, filters) → (Item, error)`
- `usecase.Done(ctx, deps, id, leaseID, note, metadata) → (Item, error)`

Each takes a `Deps` struct of interfaces (a `Store`, a `Clock`, an `IDGen`, an `EventLog`, a `Locker`). Calls `domain` for business rules. Returns domain types or errors. **Never imports a concrete adapter** — only the interfaces defined in `domain` (or `usecase` itself).

This layer is unit-testable using fakes/mocks of the interfaces. No real filesystem.

**`internal/adapter/`** — concrete implementations of the interfaces `usecase` depends on. Subdivided:
- `adapter/fsstore/` — file-based item store, atomic writes, lock files
- `adapter/eventlog/` — sharded JSONL append + tail
- `adapter/oplog/` — operation journal (WAL) for multi-file ops
- `adapter/index/` — local cache builder/reader
- `adapter/git/` — wraps `os/exec` for `git pull`/`push`/`add`/`commit`
- `adapter/clock/` — wraps `time.Now()` (so tests inject a fake clock)
- `adapter/idgen/` — wraps ULID generation (tests inject deterministic IDs)
- `adapter/render/` — HTML generation from items + events
- `adapter/watch/` — `fsnotify`-based event tail

Each adapter is integration-tested against a temp directory or a stub binary. Only this layer touches the real filesystem.

**`internal/cli/`** — cobra command tree. Parses flags, calls `usecase`, formats output. Knows about JSON-vs-text rendering, exit codes, the global flags from §15.1. Does not contain business logic.

**`cmd/corvee/main.go`** — 30-50 lines. Wires the dependency graph (constructs concrete adapters, injects them into the cli tree), handles top-level errors, exits with the correct code.

### 2.2 Why this matters

Without clean architecture, the standard CLI rewrite trap kicks in: business rules leak into adapter code, adapters leak into business rules, and you can't test the concurrency model without spinning up a temp git repo. With it:

- **Unit tests for `domain` and `usecase` run in <1s** with no fixtures. A `make test-fast` target should always be sub-second.
- **Concurrency tests can use fake clocks and fake stores** to deterministically reproduce race conditions that would be flaky against a real filesystem.
- **The Phase 6 git-sync work doesn't require touching `domain` or `usecase`** — it's all in `adapter/git/`.
- **The future MCP adapter (deferred per §16.6) doesn't require touching `domain` or `usecase`** — it would be a new entrypoint at `cmd/corvee-mcp/main.go` reusing the same usecase layer.

### 2.3 Extension surfaces

Three places where users plug in:

- **Plugins** (`plugins/`) — Go plugins are awkward; we use a different mechanism. Plugins are external binaries the CLI invokes via `os/exec` for hook events, communicating over JSON on stdin/stdout. This keeps plugins language-agnostic (a Python plugin works fine) and avoids Go's plugin-loading limitations.
- **Shell hooks** (`.tasks/hooks/`) — shell scripts invoked on lifecycle events. Receive event JSON on stdin. Useful for Slack/Telegram pings, CI triggers, custom logging, or invoking external LLM-powered enrichers without coupling them to the core.
- **HTML themes** — Go templates + CSS files in `adapter/render/themes/`. Selectable via `corvee render --theme <name>`.

Note: there is **no LLM adapter** in the architecture. Per §1.1, content generation is out of scope. If a user wants LLM-powered enrichment, they write an external plugin that calls their preferred provider directly. The core CLI never makes network calls beyond `git push`/`git pull`.

---

## 3. Directory Layout

```
<repo-root>/
  .tasks/
    config.json              # workspace config (project codes, defaults, agent id, capabilities)
    .gitignore               # excludes index.json, locks/, operations/ from git
    index.json               # LOCAL CACHE — gitignored, rebuildable, never source of truth
    items/
      RKN.json               # project
      RKN-E01.json            # epic
      RKN-E01-S01.json        # story
      RKN-E01-S01-T01.json    # optional subtask
    events/
      2026-05-02.jsonl       # daily-sharded append-only audit log
      2026-05-03.jsonl
    operations/              # in-flight multi-file operation journals (WAL) — gitignored
      01HX9KP3...json        # cleaned up after completion
    locks/                   # transient per-item lock files — gitignored
    hooks/
      on-status-change.sh    # optional user shell hooks
```

A single flat `items/` directory keeps lookups O(1) by ID. Hierarchy is encoded in the ID and the `parent_id` field, not in the filesystem. Scales cleanly to thousands of items without nested directory traversal.

---

## 4. ID Scheme

Format: `<PROJECT>-E<NN>-S<NN>-T<NN>`

- `PROJECT` — short uppercase code, 3–6 chars (e.g. `RKN`, `KAHU`)
- `E<NN>` — epic number, zero-padded to 2 digits, optional
- `S<NN>` — story number, optional
- `T<NN>` — subtask number, optional

| ID | Type | Meaning |
|---|---|---|
| `RKN` | project | The project itself |
| `RKN-E01` | epic | First epic in RKN |
| `RKN-E01-S03` | story | Third story in that epic |
| `RKN-E01-S03-T02` | subtask | Second subtask under that story |

Numbering is per-parent and monotonic: the system reads existing siblings and assigns the next free number. The ID alone reveals the full ancestry — useful for grep, tab-completion, and UI grouping.

---

## 5. Task Schema

A single unified schema discriminated by `type`. Fields irrelevant to a type are omitted, not nulled.

```json
{
  "schema_version": 1,
  "id": "RKN-E01-S03",
  "type": "story",
  "kind": "feature",
  "project_id": "RKN",
  "parent_id": "RKN-E01",
  "title": "Build emotion classifier pipeline",
  "description": "...",
  "status": "in_progress",
  "priority": "high",
  "tags": ["ml", "nlp"],

  "deliverable": null,

  "claim": {
    "agent": "agent-claude-1",
    "lease_id": "01HX9K3QT8FSXR2M5J7N",
    "claimed_at": "2026-05-02T10:00:00Z",
    "expires_at": "2026-05-02T11:00:00Z"
  },
  "required_capabilities": ["python", "ml"],

  "created_at": "2026-05-01T08:00:00Z",
  "created_by": "agent-planner-1",
  "created_by_role": "planner",
  "updated_at": "2026-05-02T10:15:00Z",
  "completed_at": null,
  "due_date": null,

  "version": 7,

  "impact": {
    "files": ["src/ml/classifier.py", "tests/test_classifier.py"],
    "modules": ["ml.classifier"],
    "estimated_hours": 6,
    "estimated_loc": 200,
    "risk": "medium"
  },

  "dependencies": ["RKN-E01-S01"],
  "blocks": [],
  "aliases": [],

  "acceptance_criteria": [
    "Classifier achieves >85% accuracy on validation set",
    "Unit tests cover happy path + 3 failure modes",
    "Inference latency <100ms p95"
  ],

  "journal": [
    { "ts": "2026-05-01T08:00:00Z", "actor": "mandar", "event": "created" },
    { "ts": "2026-05-02T10:00:00Z", "actor": "agent-claude-1", "event": "claimed" },
    {
      "ts": "2026-05-02T14:30:00Z",
      "actor": "agent-claude-1",
      "event": "completed",
      "note": "shipped classifier; 92% accuracy on val set",
      "metadata": {
        "changed_files": ["src/ml/classifier.py", "tests/test_classifier.py"],
        "tests_run": 18,
        "tests_passed": 18,
        "decisions": ["used distilbert-base over xlm-r for latency"],
        "metrics": { "accuracy": 0.921, "p95_latency_ms": 87 }
      }
    }
  ]
}
```

### Enumerations

- `type`: `project | epic | story | subtask` (structural hierarchy)
- `kind`: `feature | bug | chore | spike | docs | refactor` (nature of work, orthogonal to type)
- `status`: `backlog | ready | claimed | in_progress | review | blocked | done | abandoned`
- `priority`: `critical | high | medium | low`
- `risk`: `low | medium | high`

### Field notes

- **`schema_version`**: integer, currently `1`. Pinned per-item so future migrations can detect old records and upgrade them lazily. Workspace-level version lives in `config.json`.
- **`kind`** (orthogonal to `type`): captures what kind of work, not the structural level. A story can be a feature, bug, or refactor. Defaults to `feature`. Useful for filtering, reporting, and routing (bug fixes get different agent capabilities than features).
- **`deliverable`** (epic-only): one-sentence machine-readable summary of what working product the epic produces when complete. Read by `corvee summary` and the HTML render. Distinct from `acceptance_criteria` which lives at the story/subtask level. `null` for non-epic types.
- **`estimated_hours`** (number, optional): primary capacity unit. Replaces nothing — `estimated_loc` stays for code-impact reasoning. Hours feed into `corvee summary` rollups and can bias `corvee next` (prefer shorter tasks for end-of-day claims, etc.).
- **`created_by`** / **`created_by_role`**: agent ID and role of whoever created the item. Roles: `planner | executor | reviewer | human`. Used by executors to weight trust in tasks (planner-created tasks come with a fuller spec; executor-created tasks are usually subtasks discovered mid-execution). Set automatically from `agent_role` in `config.json` on the creating machine.
- **`acceptance_criteria`** is a Set semantically — duplicates are removed on write. Order is preserved.
- **`aliases`** (array of past IDs from move/rename ops) — already documented in §8.2; lifted into the schema example here for completeness.
- **`journal[].metadata`** (object, optional): arbitrary JSON attached to journal entries by `corvee done`, `corvee journal`, and progress events. **Schema is intentionally unconstrained** — agents and skills choose their own conventions. The tool stores and surfaces the metadata; it does not validate keys. Common patterns (changed_files, tests_run, decisions, artifact paths, metrics) are documented in `references/handoff-patterns.md` but are guidance, not enforcement. Downstream agents read this via `corvee show <id>` or by tailing `corvee watch` to receive structured handoff data without LLM parsing.

### Required fields

`schema_version`, `id`, `type`, `title`, `status`, `created_at`, `updated_at`, `version`. Everything else is optional. Schema is enforced via JSON Schema (ajv) and validated on every write.

### Workspace-level metadata (`config.json`)

```json
{
  "schema_version": 1,
  "workspace_name": "rikonect-backlog",
  "default_project": "RKN",
  "default_agent": "mandar",
  "agent_role": "executor",
  "agent_capabilities": ["python", "ml", "frontend"],
  "claim_ttl_minutes": 60,
  "heartbeat_interval_minutes": 30,
  "critical_path": ["RKN-E01-S01", "RKN-E01-S02", "RKN-E02-S01"],
  "git": { "auto_commit": false, "auto_sync_seconds": null }
}
```

`critical_path` is an ordered list of item IDs that must complete in sequence before the project is unblocked. `corvee next` gives critical-path items a selection bonus (after priority, before due_date). Computed by humans or `corvee plan` (future); not derived automatically — true critical paths require human judgment about which dependencies *matter*.

---

## 6. Concurrency Model

The most important section. Multiple agents will hit this concurrently — same machine and across machines via git.

### Layer 1: Atomic file writes

Every write follows: write to `<id>.json.tmp.<pid>.<ts>`, fsync, atomically rename to `<id>.json`. POSIX rename is atomic; no partial writes, no torn JSON parsable mid-flight.

### Layer 2: Optimistic concurrency via `version`

Every mutation follows this loop:

1. Read `<id>.json` → record `version = N`
2. Apply mutation in memory; increment `version` to `N+1`, set `updated_at`
3. Re-read on disk inside the write transaction; abort if version changed
4. Write atomically (Layer 1)
5. On version mismatch: reload, re-apply, retry — bounded to ~5 attempts

This handles same-machine concurrent writers without any lock daemon.

### Layer 3: Claim semantics for agents

To prevent two agents from working the same task:

- `corvee claim <id>` — atomically writes a structured `claim` object (`agent`, `lease_id`, `claimed_at`, `expires_at`) and sets `status=claimed`. Succeeds only if currently unclaimed. Returns the lease_id; the agent must retain it.
- **Every claim-scoped operation (`heartbeat`, `release`, `done`, `journal`) requires `--lease-id <ID>`.** The CLI rejects with exit 5 if the on-disk `claim.lease_id` differs. This closes the post-TTL hazard: agent A's expired claim cannot resurrect itself after agent B has taken over, because A's lease_id no longer matches.
- Claims have a TTL (default 60 min, configurable). `corvee heartbeat` extends `expires_at`. `corvee reap` releases claims past `expires_at` (runs as cron or implicitly inside `corvee next`).
- TTL checks tolerate up to 5 minutes of clock skew between machines; skew beyond that is flagged by `corvee validate`.
- This is **cooperative** locking — finest-grained protection comes from lease_id checks, not the kernel.

### Layer 4: Git as the distribution & merge layer

- Mutations optionally trigger `git add` + `git commit` (configurable per workspace).
- `corvee sync` runs `git pull --rebase` then `git push`.
- Per-task JSON conflicts are resolved by `corvee merge <id>`: a 3-way merge that's automatic for status/journal/tags (mergeable fields) and prompts for prose fields (description, acceptance_criteria). Claim conflicts use the deterministic rule in Layer 8.
- Daily events shards merge trivially: union the lines, dedupe by `op_id`, sort by `ts`.

### Layer 5: Sharded events log

Events are append-only and the source of truth for audit. To avoid git-merge hot-spots and append contention, events are sharded by day:

```
.tasks/events/
  2026-05-02.jsonl
  2026-05-03.jsonl
```

Each line:

```jsonl
{"ts":"2026-05-02T10:00:00Z","actor":"agent-claude-1","id":"RKN-E01-S03","event":"claim","from":"ready","to":"claimed","op_id":"01HX9..."}
```

Daily shards reduce git conflicts (different days = different files), make compaction trivial (archive old shards), and preserve total order within a shard via `ts`. Cross-shard queries are O(N shards) — small for any realistic workload. `index.json` is fully rebuildable from items + events via `corvee reindex`.

### Layer 6: Operation journal (mini WAL) for multi-file ops

`move`, `rename`, `delete --cascade`, and `clone --with-children` mutate many files. A crash mid-operation otherwise corrupts the graph (some files renamed, references not yet rewritten).

Each multi-file operation writes an intent record before any mutation:

```
.tasks/operations/<op-id>.json
```

Intent schema:

```json
{
  "op_id": "01HX9KP3...",
  "kind": "move | rename | delete_cascade | clone_with_children",
  "agent": "agent-claude-1",
  "started_at": "...",
  "status": "pending | executing | completed | failed",
  "plan": [
    { "step": 1, "kind": "rename_file", "from": "RKN-E01-S03.json", "to": "RKN-E02-S04.json", "done": false },
    { "step": 2, "kind": "rewrite_refs", "in_items": ["RKN-E03-S01"], "old_id": "RKN-E01-S03", "new_id": "RKN-E02-S04", "done": false }
  ]
}
```

Lifecycle:
1. Write intent with `status=pending`
2. Flip to `executing`, run each step, mark `done: true` after each
3. On success, flip to `completed`, then atomic-delete after a short grace period
4. On crash, the file remains with some steps incomplete

`corvee recover` (run automatically by `corvee list`/`corvee next` if any orphaned op file is detected) rolls forward (resumes from the next undone step) when steps are individually idempotent, or rolls back when not. In v1, all step kinds are designed to be roll-forwardable.

### Layer 7: Idempotency via operation_id

Every mutating command accepts `--operation-id <ULID>`. The events log records it. Re-running with the same operation_id is a no-op that returns the prior result.

Agents should always pass their own operation_ids (cross-process retry safety). If absent, the CLI mints one — fine for one-shot human invocations.

### Layer 8: Distributed claim-conflict resolution

When `corvee sync` (git merge) detects two agents claimed the same item from divergent branches, resolution is deterministic, no human required:

1. **Lower `lease_id` wins** (ULIDs are sortable; the earlier-issued lease prevails).
2. The losing claim is recorded as a `claim_lost` event in that day's events shard.
3. The losing agent, on its next operation against that item, sees exit 5 with `details.claim_lost: true` and re-routes via `corvee next`.

This logic lives inside `corvee merge`, which handles per-task JSON conflicts during `corvee sync`.

### Index & in-memory graph (cache, not state)

The index is a **local cache**, not state. The source of truth is always the per-item JSON files in `items/` and the daily events shards. Treating the index as state invites three problems that real systems hit: merge conflicts when committed to git, staleness when concurrent agents rebuild, and unclear ownership.

**Rules:**

1. **`index.json` is gitignored by default.** `corvee init` writes `.tasks/.gitignore` excluding `index.json`, `operations/`, and `locks/`. Each machine maintains its own cache; each clone rebuilds on first use.
2. **Always rebuildable, safe to delete.** `corvee reindex` rebuilds from `items/` and events shards. Deleting the file is harmless — the next command rebuilds.
3. **No graph database.** Neo4j, ArangoDB, etc. are explicit non-goals — they break local-first, add runtime dependencies, complicate git distribution, and hurt determinism. The in-memory graph plus the cached index covers every realistic query for backlogs up to ~10k items, which is well past anything a single project will need. If the system ever needs deep graph analytics, that's a different product.

**Staleness detection (cheap):** the index header records `item_count` and `max_mtime` from `items/` at build time. On read, the CLI does one `readdir + stat` and compares; if either differs, the index is invalidated and rebuilt before the query proceeds. Two stat calls; no checksums needed.

**What's in the index** — small, cheap, addresses the queries that actually matter:

```json
{
  "built_at": "2026-05-02T10:15:00Z",
  "item_count": 142,
  "max_mtime": "2026-05-02T10:15:00Z",
  "by_id":            { "RKN-E01-S03": {...full item...} },
  "by_status":        { "ready": [...], "claimed": [...] },
  "by_assignee":      { "agent-claude-1": [...] },
  "by_parent":        { "RKN-E01": ["RKN-E01-S01", ...] },
  "unblocked":        ["..."],
  "dependency_graph": { "RKN-E01-S03": ["RKN-E01-S01"] }
}
```

**In-memory graph per CLI invocation.** Each `corvee <verb>` invocation builds a process-local graph at startup from the cached index (or rebuilds from items if stale). The graph is three Maps:

```
Map<string, Item>           // id → item (O(1) lookup)
Map<string, string[]>       // parent_id → child_ids (tree traversal)
Map<string, string[]>       // id → dependencies (unblocked computation)
```

This gives O(1) lookup and fast traversal without any external system. The CLI exits and the graph is gone — no shared mutable state, no race conditions, no cross-process coordination on the in-memory structures.

**Future-proofing without commitment.** `corvee export --format graph` emits the workspace as nodes-and-edges (JSON Graph Format, optionally Cypher or DOT) so downstream tools — D3 visualizations, Neo4j analysis, ML pipelines — can consume the graph without the core system depending on any of them.

---

## 7. Multi-Agent Topologies

The system supports several agent collaboration patterns. The most important — and the one this spec optimizes for — is **planner-executor with distributed executors across VMs**. This section documents how the primitives compose into that topology.

### 7.1 Roles

The `agent_role` field in each VM's `config.json` (and `created_by_role` on items) declares what the agent is doing in the system. Roles are advisory — they don't enforce permissions, they communicate intent.

| Role | Typical responsibilities | Reads | Writes |
|---|---|---|---|
| `planner` | Decompose goals into epics/stories/subtasks. Set priorities, dependencies, capabilities, deliverables. Doesn't execute work. | All items | Creates items, updates priorities, sets `critical_path` in config |
| `executor` | Pull `ready` work via `corvee next`, do the work, update status, journal progress, mark `done`. Can create subtasks discovered mid-execution. | Claimed items, dependencies | Status, journal, claim/release, subtasks of own claimed items |
| `reviewer` | Pick items in `status=review`, validate against `acceptance_criteria`, flip to `done` or back to `in_progress` with feedback. | Items in review | Status, journal |
| `human` | Anything. Default for interactive CLI use. | All | All |

A single agent process can take on multiple roles over its lifetime — the role is per-invocation, set by `--agent-role <ROLE>` flag or `CORVEE_AGENT_ROLE` env var. What matters is that every CREATED or CLAIMED action is stamped with the role at that moment, so downstream consumers know provenance.

### 7.2 Planner → Executors Pattern (Canonical)

**Topology:**
```
                                git remote
                                    │
               ┌────────────────────┼────────────────────┐
               │                    │                    │
         ┌─────▼─────┐       ┌──────▼──────┐      ┌──────▼──────┐
         │  Planner  │       │ Executor A  │      │ Executor B  │
         │  (VM-0)   │       │   (VM-1)    │      │   (VM-2)    │
         │ role=plan │       │ role=exec   │      │ role=exec   │
         └───────────┘       │ caps=[py,ml]│      │ caps=[fe,ts]│
                             └─────────────┘      └─────────────┘
```

Each VM has its own clone of the workspace repo. Tasks flow:

1. **Planner creates work** — `corvee import plan.json` or a sequence of `corvee add`. Items are created with `status=ready`, `created_by_role=planner`, populated `required_capabilities`, `dependencies`, `acceptance_criteria`, `estimated_hours`. **No claim yet.**
2. **Planner publishes** — `corvee sync` (commits items, pushes to remote). This is the explicit "release the plan" moment. Without sync, executors can't see the work.
3. **Executors poll** — periodic `corvee sync && corvee next --auto-claim --match-capabilities` on each executor VM. The `--match-capabilities` flag filters to items whose `required_capabilities` are a subset of the executor's `agent_capabilities`. The atomic claim + lease_id ensures two executors on different VMs can't both claim the same task even with concurrent polls (Layer 8 deterministic merge resolves the race during the next `sync`).
4. **Executor heartbeats and journals** — `corvee heartbeat <id> --lease-id <ID>` every `heartbeat_interval_minutes` (default 30). Journal entries on milestones. These writes also need `corvee sync` to be visible to others.
5. **Executor completes** — `corvee done <id> --lease-id <ID> --note "..."`. Status flips to `review` if a reviewer role exists in the workspace, else direct to `done`. Sync.
6. **Planner observes** — `corvee summary` shows progress; `corvee list --status blocked` surfaces stuck work; planner replans (creates new items, updates priorities) and syncs again.

### 7.3 Sync Cadence

The single most important operational parameter for this topology. Recommended defaults:

| Action | Cadence | Rationale |
|---|---|---|
| Planner: `corvee sync` after creating a batch | Immediate (after `corvee import`) | Executors can't pick up unsynced work |
| Executor: `corvee sync` before `corvee next` | Every poll (default 30s–5min) | Prevents stale claims; surfaces newly-planned work |
| Executor: `corvee sync` after every `done`/`update`/`heartbeat` | Immediate | Other executors see freed claims; planner sees progress |
| All: `corvee sync` opportunistically on idle | Every 60s | Catches peer changes; resolves merge conflicts before they pile up |

`config.json:git.auto_sync_seconds` enables a background sync loop inside long-running agents. `null` (default) means manual only. For the planner-executor topology, **set this to 30–60 seconds on executors**.

The skew tolerance (5 min on TTL checks) tolerates moderate clock drift across VMs. Workspaces with VMs in different time zones or with unsynced clocks should set `claim_ttl_minutes` higher (e.g., 120) to add safety margin.

### 7.4 Failure Modes & How They Resolve

| Failure | What happens | Recovery |
|---|---|---|
| Executor VM crashes mid-task | Claim's `expires_at` passes; no heartbeats | Any executor's `corvee next` invokes `corvee reap` first; expired claim released; another executor picks it up. The `journal` from the crashed executor is preserved (last-known progress). |
| Two executors race on same task across VMs | Both claim locally; both push to remote; `corvee sync` triggers `corvee merge` | Layer 8: lower lease_id wins, loser sees `claim_lost: true` on next operation, calls `corvee next` again |
| Planner crashes mid-`corvee import` | Some items created, sync didn't happen | Items stay in local `.tasks/items/` — planner can resume `corvee import --dry-run` to see what's already there, or just re-run (idempotent if `--operation-id` was used) |
| Network partition (executor can't `git push`) | Executor keeps working locally; commits accumulate | When connectivity returns, `corvee sync` rebases and pushes. If two executors worked the same task during partition, Layer 8 resolves on merge. |
| Clock skew on executor exceeds tolerance | `corvee validate` flags it; `corvee heartbeat` may unexpectedly fail | Operator action: NTP-sync the VM, or raise `claim_ttl_minutes` |

### 7.5 What This Topology Doesn't Provide

Honest limits, so nobody plans around capabilities that aren't there:

- **Real-time coordination**: there's no pub/sub. Executors learn about new work on their next `sync`. Latency = sync interval. Adequate for SDLC-style workloads where tasks take minutes-to-hours; inadequate for sub-second dispatch.
- **Strong consistency across VMs**: git is eventually consistent. Two executors might briefly believe they each hold a claim until the next sync resolves it. This is by design — the alternative requires a coordination server, which the spec rules out.
- **Cross-workspace dependencies**: an executor in workspace A can't depend on a task in workspace B. Each workspace is its own coordination domain. If you need this, run a meta-planner that maintains a third workspace tracking cross-workspace work.
- **Authentication**: any agent with repo access can claim any task and impersonate any role. `agent_role` is advisory metadata, not a permission boundary. Auth lives at the git layer (SSH keys, repo permissions), not in corvee.

---

## 8. CLI Surface

All commands emit JSON to stdout by default (machine-readable). `--pretty` for human-friendly output. Non-zero exit code on error, with structured error JSON to stderr.

| Command | Purpose |
|---|---|
| `corvee init` | Bootstrap `.tasks/` in cwd |
| `corvee add <type> --title "..." [--parent <id>] [--priority ...]` | Create item, returns assigned ID |
| `corvee list [--status ...] [--project ...] [--assignee ...] [--parent ...]` | Query with filters |
| `corvee show <id>` | Full item JSON |
| `corvee tree [<id>]` | Hierarchical view (project → epic → story → subtask) |
| `corvee update <id> [--status ...] [--add-tag ...] ...` | Generic mutation, version-aware |
| `corvee claim <id> --as <agent-id>` | Atomic claim |
| `corvee release <id>` | Release a claim |
| `corvee heartbeat <id>` | Refresh claim TTL |
| `corvee done <id>` | Convenience: status=done, completed_at=now |
| `corvee delete <id> [--hard] [--cascade] [--force]` | Delete item (soft by default; `--hard` removes file) |
| `corvee move <id> --to <new-parent-id>` | Re-parent item under different epic/project; cascades to children |
| `corvee rename <id> --to <new-id>` | Rename ID directly (e.g. fix project code typo) |
| `corvee clone <id> [--to <parent>]` | Duplicate item with fresh ID and reset status |
| `corvee journal <id> --note "..." [--actor ...]` | Append journal entry |
| `corvee next [--assignee ...]` | Pick highest-priority unblocked, unassigned task — primary agent entry point |
| `corvee sync` | git pull/push wrapper |
| `corvee reindex` | Rebuild `index.json` from `items/` |
| `corvee validate` | Schema check across all items |
| `corvee render` | Generate HTML dashboard |

Each verb lives in its own file under `bin/`, dispatched via a single `corvee` entrypoint.

### 8.1 Delete semantics

- **Soft by default**: `corvee delete <id>` sets `status=abandoned`. File remains. Reversible via `corvee update`.
- **Hard**: `--hard` removes the JSON file. Git history retains it for recovery.
- **Cascade**: `--cascade` also deletes children. Without it, items with children block the operation.
- **Dependents**: refuses if any other item lists this ID in `dependencies` or `blocks`. `--force` overrides but logs a warning. This guard prevents silent orphaning of dependency chains.

### 8.2 Move and rename semantics

Because IDs encode hierarchy, `move` and `rename` issue new IDs and rewrite references. The operation is transactional at the workspace level — either every change applies or none do.

Procedure for `corvee move <id> --to <new-parent-id>`:

1. Compute the new ID — next free slot of the same `type` under the target parent.
2. Compute cascading new IDs for all descendants (subtasks come along).
3. For each affected item: `git mv` the file, update `parent_id` and `project_id`, append the old ID to `aliases[]`.
4. Scan all other items for the old IDs in `dependencies` and `blocks`; rewrite to new IDs.
5. Append a `moved` event to `events.jsonl` with the full old→new ID map.
6. Bump `version` on every touched item.

The `aliases[]` field on each moved item preserves resolvability of old references in external systems — commit messages, agent context, journal entries written before the move. `corvee show <old-id>` follows aliases.

`corvee rename <id> --to <new-id>` uses the same machinery but accepts an explicit target ID instead of computing one under a new parent. Useful for fixing project-code typos or renumbering.

### 8.3 Clone semantics

`corvee clone <id> [--to <parent>]` creates a new item with:
- Fresh ID under the original parent (or `--to` if specified)
- Same `title`, `description`, `acceptance_criteria`, `tags`, `impact`, `dependencies`, `priority`
- Reset: `status=backlog`, `assignee=null`, `claimed_at=null`, `completed_at=null`, `journal=[{created}]`, `version=1`

Children are not cloned by default. `--with-children` cascades the clone.

---

## 9. HTML Generator

`corvee render [--out <path>] [--theme <name>]` produces a single self-contained HTML file (no build step, no external assets at runtime).

Three views, toggled in-page:

1. **Board view** — kanban columns by status, cards grouped by epic, color-coded by priority. Read-only; the file regenerates on demand.
2. **Tree view** — collapsible project → epic → story → subtask hierarchy with status pills, completion percentages, and assignee badges.
3. **Summary view** — per-project progress bars, completion velocity from `events.jsonl`, blocked-task callouts, agent activity feed.

Implementation: vanilla HTML/CSS + minimal vanilla JS for view toggling. Themes are CSS files swappable via `--theme`. Generator reads `items/` and `events.jsonl` only — never mutates.

---

## 10. Extensibility

### Plugins

A plugin is a folder under `plugins/<name>/` exposing a `plugin.js`:

```js
module.exports = {
  name: "auto-tagger",
  agent_safe: true,   // declares deterministic behavior; safe to run during agent invocations
  hooks: {
    afterCreate: async (item, ctx) => { /* enrich newly-created items */ },
    beforeStatusChange: async (item, newStatus, ctx) => { /* return false to veto */ },
    afterStatusChange: async (item, oldStatus, ctx) => { /* notify, log, etc. */ },
  }
};
```

Plugins run in-process. Errors are logged but don't fail the core operation unless the plugin explicitly vetoes via `beforeStatusChange`.

**Determinism for agent contexts**: plugins must declare `agent_safe: true` to run when the CLI is invoked by an agent (detected via `CORVEE_AGENT_MODE=1` env var or `--agent` flag). Non-agent-safe plugins are skipped silently in agent mode. This prevents two agents from observing different workspace behavior — critical for reproducibility in distributed setups.

**No LLM adapter ships in core.** Earlier drafts included an `adapters/llm/` interface for future enrichers (auto-tagging, complexity scoring, summarization). That idea is dropped per §1.1 — content generation is out of scope. If a user wants LLM-powered enrichment, they write a plugin that calls their preferred provider directly. The core CLI never makes network calls beyond `git push`/`git pull`.

### Shell hooks

Files in `.tasks/hooks/` execute if present, named by event (`on-create.sh`, `on-status-change.sh`, etc.). Receive event JSON on stdin. Useful for Slack/Telegram pings, CI triggers, custom logging, or invoking external LLM-powered enrichers without coupling them to the core.

---

## 11. Test-Driven Development (Mandatory)

**TDD is a hard requirement, not a recommendation.** Every functional change is delivered as: failing test → minimum implementation → green test → refactor. No code is committed without tests that cover its behavior. CI rejects PRs that decrease coverage in `domain/` or `usecase/`.

This is not bureaucratic. The system is a coordination protocol with concurrency-sensitive invariants (lease IDs, version monotonicity, claim conflict resolution). The cost of a subtle bug is a corrupted backlog across multiple agents on multiple VMs. TDD is how we prevent that — by forcing every invariant to be expressed as an executable assertion before the code that satisfies it exists.

### 11.1 The TDD loop (per change)

For each task in the phased plan (§13), the agent or developer follows:

1. **Write a failing test** in the package being modified. The test names a behavior, not an implementation. Example: `TestClaim_RejectsWhenAlreadyClaimed`, not `TestClaimReturnsError`.
2. **Run the test, confirm it fails** for the expected reason (not a compile error or a typo). `go test ./internal/usecase/ -run TestClaim_RejectsWhenAlreadyClaimed` should print a meaningful failure.
3. **Write the minimum code** to make the test pass. Resist adding fields, helpers, or branches the current test doesn't require.
4. **Run all tests**, not just the new one. Catch regressions immediately.
5. **Refactor** with the safety net of green tests. Remove duplication, extract names, simplify.
6. **Commit** the test + implementation as a single atomic change. The commit message states what behavior was added.

A change that doesn't fit this loop (a pure refactor, a config tweak, a doc edit) is exempt — but anything that changes runtime behavior follows the loop.

### 11.2 Test categories and directory layout

Each test category lives in a known place with known naming. AI agents implementing this should not invent new categories or directory conventions.

```
internal/domain/
  item.go
  item_test.go              ← unit tests for Item (schema, validation, transitions)
  id.go
  id_test.go                ← unit tests for ID parsing/generation
  status.go
  status_test.go            ← unit tests for legal status transitions
  ...

internal/usecase/
  claim.go
  claim_test.go             ← unit tests with fakes for store/clock/idgen
  next.go
  next_test.go
  done.go
  done_test.go
  ...
  testfakes/                ← shared fakes (FakeStore, FakeClock, FakeIDGen)
    store.go
    clock.go
    idgen.go

internal/adapter/fsstore/
  store.go
  store_test.go             ← integration tests against t.TempDir()
  store_concurrent_test.go  ← N-goroutine race tests with -race flag

internal/adapter/eventlog/
  log.go
  log_test.go
  log_tail_test.go          ← fsnotify integration

test/
  e2e/                      ← end-to-end tests: build the binary, run real commands
    init_test.go
    multi_agent_test.go     ← spawn N processes, assert no lost updates
    git_sync_test.go        ← two repos, push/pull, verify merge resolution
  fixtures/                 ← golden files for snapshot tests
    workspaces/
    plans/
    rendered/
  property/                 ← property tests using gopter or rapid
    id_property_test.go
    schema_property_test.go
```

**Naming conventions** (enforced by `go vet` and PR review):

- Unit test functions: `Test<Behavior>_<Condition>`. Example: `TestClaim_FailsWithExit5_WhenAlreadyClaimedByAnother`.
- Subtests for variations: `t.Run("when status is backlog", func(t *testing.T) { ... })`. Use `t.Parallel()` aggressively in unit tests.
- Table-driven tests for any function with >2 variants: declare a `tests := []struct{ name string; in X; want Y; wantErr bool }{...}`.
- Fakes live in `testfakes/`; mocks (with assertions) live in `testmocks/` if needed. Prefer fakes over mocks — they read more like real code.

### 11.3 Test categories — what each must cover

**Unit tests (`*_test.go` in `domain/` and `usecase/`)** — fastest tier. Run on every save in the editor. `make test-unit` must complete in <2 seconds for the full suite even at thousands of tests.

Coverage requirement: **≥90% line coverage in `domain/`, ≥90% in `usecase/`**. Enforced via `go test -coverprofile=coverage.out ./internal/domain/... ./internal/usecase/...` in CI.

For every exported function, at minimum:
- Happy path (typical valid input)
- Each documented error condition (one test per)
- Boundary conditions (empty input, max-length input, unicode, etc., where relevant)

**Adapter integration tests (`*_test.go` in `adapter/*/`)** — exercise real I/O against `t.TempDir()`. Each test creates a fresh workspace, performs operations, asserts state. Cleanup via `t.Cleanup()`.

Coverage requirement: ≥75% line coverage. Lower bar than `domain/` because some I/O error paths are hard to trigger reliably.

**Concurrency tests (`*_concurrent_test.go`)** — the tier that earns its keep. Required for every adapter that handles concurrent writes (`fsstore`, `eventlog`, `oplog`). All run with `-race`.

Required scenarios per phase:

- **Phase 1** (basic store): N goroutines update the same item; assert version is strictly monotonic and the final on-disk state matches one of the writes (no torn writes, no lost updates beyond optimistic-concurrency retries).
- **Phase 3** (claim semantics): N goroutines call `Claim` on the same item; exactly one succeeds, others receive `ErrAlreadyClaimed`. Repeat 1000 times under `-race` with `-count=10`.
- **Phase 3** (lease validation): goroutine A holds a lease, B does heartbeat with a wrong lease ID; B is rejected with `ErrLeaseMismatch` and A's lease is unchanged.
- **Phase 4** (operation journal): inject crash mid-multi-file-op (panic after writing intent but before completing all steps); assert `recover()` rolls forward to a consistent state.
- **Phase 6** (git merge): two repos diverge with conflicting claims; merge applies the deterministic rule (lower lease_id wins) and produces identical state on both sides.

**Property tests (`test/property/`)** — generative tests using `pgregory.net/rapid`. Required for:

- ID parsing/generation: `parse(generate(x)) == x` for any valid x; `generate` never produces an invalid ID; `parentOf(childId(p, n)) == p`.
- Schema validation: a randomly-generated valid item validates; mutation of any required field invalidates it.
- Dependency graph: a randomly-generated DAG has no cycles; a randomly-generated graph with one inserted back-edge does have a cycle.

**End-to-end tests (`test/e2e/`)** — slowest tier. Build the binary, run real `corvee` commands, assert on stdout/stderr/exit code. One test per CLI verb, plus the canonical workflows from §7.5.

Required E2E scenarios:

- Init → add project → add epic → add story → list → show: assert all output is well-formed JSON, exit codes are 0, IDs follow the §4 scheme.
- Multi-process: spawn 10 child processes each calling `corvee next --auto-claim` against a workspace with 5 items; assert exactly 5 succeed and 5 receive exit code 5.
- Multi-VM (simulated via two temp dirs + a bare git repo): planner creates 20 tasks in repo A, syncs; executor in repo B syncs, claims, completes; planner syncs and sees the completions.
- Crash recovery: kill the process during `corvee move`; restart; `corvee recover` rolls forward; `corvee validate` reports clean.

**HTML snapshot tests (`test/e2e/render_test.go`)** — render a known fixture workspace, diff against `test/fixtures/rendered/*.html`. CI fails on diff. Updates are explicit (`go test -update` regenerates fixtures, requires manual review).

### 11.4 What "done" means for a test

A test is complete when:

- It runs with `go test ./...` from a fresh checkout with no setup beyond `go mod download`.
- It passes deterministically. A test that passes 99/100 runs is broken; either the production code is racy or the test is. Fix it.
- It cleans up after itself. `t.TempDir()` for files; `t.Cleanup()` for everything else. No test writes to `/tmp` directly, no test depends on cwd.
- It runs in <100ms for unit tests, <1s for adapter integration, <5s for E2E. Slower tests get tagged with `//go:build slow` and excluded from `make test`.

### 11.5 CI gates

A change merges only if all of these pass:

| Gate | Command | Required threshold |
|---|---|---|
| Build all platforms | `make build-all` | exit 0 on linux/amd64, linux/arm64, darwin/arm64 |
| Vet | `go vet ./...` | no findings |
| Lint | `golangci-lint run` | no findings (config in `.golangci.yml`) |
| Format | `gofmt -l . \| wc -l` | output is `0` |
| Unit tests | `go test -race ./internal/domain/... ./internal/usecase/...` | exit 0 |
| Integration tests | `go test -race ./internal/adapter/...` | exit 0 |
| Concurrency tests | `go test -race -count=10 ./...` | exit 0 (10x repetition catches flakiness) |
| E2E tests | `go test ./test/e2e/...` | exit 0 |
| Coverage (domain) | `go test -coverprofile=... ./internal/domain/...` | ≥90% |
| Coverage (usecase) | `go test -coverprofile=... ./internal/usecase/...` | ≥90% |

CI is a single `make ci` target. Local pre-push hook runs the same target so failures are caught before push.

### 11.6 What an AI agent implementing this needs to do

A coding agent picking up a task from this spec follows the loop in §11.1. Concretely, for each subtask:

1. Read the relevant section of this spec.
2. Identify the package and file the change belongs in (per §2.1 layering).
3. Write the test(s) covering the behavior described in the spec. Run them, confirm they fail for the right reason.
4. Implement the minimum code. Run all tests. Iterate until green.
5. Refactor.
6. Commit with a message matching the conventional commits format: `feat(domain/item): add status transition validation` or `test(usecase/claim): cover lease mismatch case`.
7. Update relevant docs (godoc comments, `references/*.md` if user-facing) in the same commit.

If the agent finds the spec ambiguous, it does **not** invent behavior. It writes a TODO test (`t.Skip("spec ambiguous on X")`), opens an issue, and proceeds with what's clear. This makes spec gaps visible instead of silently filled with the agent's guesses.

---

## 12. Tech Stack & Dependencies

### 12.1 Language and runtime

**Go ≥1.23** (uses range-over-function iterators introduced in 1.23, which simplify event-log streaming). Single static binary distribution. No runtime needed on target machines. Resolves SQ-002 in `docs/spec-questions.md`.

### 12.2 Direct dependencies

Hard rule: every direct dependency must be MIT/Apache/BSD/ISC, ≥1k GitHub stars or otherwise demonstrably maintained, and justified against a stdlib alternative.

| Module | Version | License | Purpose | Why not stdlib |
|---|---|---|---|---|
| `github.com/spf13/cobra` | ≥1.8 | Apache-2.0 | CLI verbs, subcommands, flags | `flag` stdlib lacks subcommands; cobra is the de facto standard (kubectl, gh, helm) |
| `github.com/fsnotify/fsnotify` | ≥1.7 | BSD-3 | File watching for `corvee watch` | No stdlib equivalent for cross-platform inotify/FSEvents |
| `github.com/oklog/ulid/v2` | ≥2.1 | Apache-2.0 | Lease IDs and operation IDs (sortable, unique) | Stdlib `crypto/rand` exists but ULID = sortable+timestamped, important for §6 Layer 8 |
| `github.com/xeipuuv/gojsonschema` | ≥1.2 | Apache-2.0 | JSON Schema draft-7 validation | Stdlib `encoding/json` doesn't validate schemas |

### 12.3 Test-only dependencies

| Module | License | Purpose |
|---|---|---|
| `pgregory.net/rapid` | MPL-2.0 | Property-based testing |
| `github.com/google/go-cmp` | BSD-3 | Better struct diffs in test failures |

That's the entire dependency list. Six modules total. No ORM, no logging framework (stdlib `log/slog` is sufficient), no HTTP framework (we don't run a server in core), no testing framework (stdlib `testing` is sufficient).

### 12.4 Stdlib reliance

Where another project might pull in a dependency, we use stdlib:

- `encoding/json` for JSON read/write
- `os` and `path/filepath` for file I/O
- `os/exec` for invoking `git`
- `sync` for in-process locking
- `syscall` for `O_EXCL` open and atomic rename (already POSIX in `os.Rename`)
- `log/slog` for structured logging (introduced in Go 1.21)
- `testing` and `testing/quick` for tests
- `text/template` and `html/template` for HTML rendering
- `embed` for embedding default themes and JSON Schema files into the binary

### 12.5 Banned dependencies

- **ORMs.** Files are the source of truth.
- **SQL drivers.** No database in core.
- **HTTP clients.** No network calls beyond `git push`/`git pull`.
- **YAML libraries.** Config and data are JSON-only. (Hooks are shell scripts; agents are JSON.)
- **Logging frameworks (logrus, zap, etc.).** `log/slog` covers it.
- **Testing frameworks (testify, ginkgo, etc.).** Stdlib `testing` is enough; testify's `assert` is convenient but adds a dep we don't need.

### 12.6 Repository layout

```
corvee/
├── cmd/
│   └── corvee/
│       └── main.go                  # entrypoint; wires deps and exits
├── internal/
│   ├── domain/
│   │   ├── item.go                  # Item struct, validation
│   │   ├── item_test.go
│   │   ├── id.go                    # ID parsing/generation
│   │   ├── id_test.go
│   │   ├── status.go                # status transitions
│   │   ├── status_test.go
│   │   ├── lease.go                 # lease validity
│   │   ├── lease_test.go
│   │   ├── graph.go                 # in-memory graph; cycle detection
│   │   ├── graph_test.go
│   │   ├── schema.go                # JSON Schema (embed.FS)
│   │   └── ports.go                 # interfaces: Store, Clock, IDGen, EventLog, Locker
│   ├── usecase/
│   │   ├── add.go
│   │   ├── add_test.go
│   │   ├── claim.go
│   │   ├── claim_test.go
│   │   ├── done.go
│   │   ├── done_test.go
│   │   ├── ... (one file per CLI verb)
│   │   └── testfakes/
│   │       ├── store.go             # FakeStore implementing domain.Store
│   │       ├── clock.go
│   │       ├── idgen.go
│   │       └── eventlog.go
│   ├── adapter/
│   │   ├── fsstore/                 # file-based item store
│   │   ├── eventlog/                # sharded JSONL log + tail
│   │   ├── oplog/                   # operation journal (WAL)
│   │   ├── index/                   # local cache
│   │   ├── git/                     # os/exec wrapper for git
│   │   ├── clock/                   # time.Now() wrapper
│   │   ├── idgen/                   # ULID wrapper
│   │   ├── render/                  # HTML generator + themes
│   │   │   ├── render.go
│   │   │   ├── themes/
│   │   │   │   ├── default.css
│   │   │   │   └── dark.css
│   │   │   └── templates/
│   │   │       ├── board.html.tmpl
│   │   │       ├── tree.html.tmpl
│   │   │       └── summary.html.tmpl
│   │   └── watch/                   # fsnotify-based event tail
│   └── cli/
│       ├── root.go                  # cobra root command
│       ├── add.go                   # one file per verb (mirrors usecase/)
│       ├── claim.go
│       ├── done.go
│       └── ...
├── pkg/
│   └── api/                         # public types if anyone embeds the lib
│       └── types.go
├── test/
│   ├── e2e/
│   │   ├── helpers_test.go          # build binary, run command, capture output
│   │   ├── init_test.go
│   │   ├── multi_agent_test.go
│   │   ├── git_sync_test.go
│   │   └── render_test.go
│   ├── fixtures/
│   │   ├── workspaces/              # known-good .tasks/ dirs for tests
│   │   ├── plans/                   # sample import JSONs
│   │   └── rendered/                # snapshot HTML for diff
│   └── property/
│       ├── id_property_test.go
│       └── schema_property_test.go
├── docs/
│   └── (this spec, plus references/)
├── SKILL.md
├── README.md
├── LICENSE                          # MIT
├── Makefile
├── .golangci.yml
├── .github/
│   └── workflows/
│       └── ci.yml                   # runs `make ci`
├── go.mod
└── go.sum
```

### 12.7 Makefile targets

The `Makefile` is the authoritative entrypoint. AI agents implementing the system invoke `make <target>`, never invent ad-hoc commands.

| Target | What it does |
|---|---|
| `make build` | `go build -o bin/corvee ./cmd/corvee` for current platform |
| `make build-all` | Cross-compile for linux/{amd64,arm64} and darwin/arm64 |
| `make test` | Run unit + adapter integration tests (excluding slow tests). Must complete in <30s. |
| `make test-unit` | Domain + usecase only. Must complete in <2s. |
| `make test-race` | All tests with `-race -count=10`. Used in CI for concurrency. |
| `make test-e2e` | E2E tests (build binary first). |
| `make test-coverage` | Generate coverage report; fail if domain <90% or usecase <90%. |
| `make lint` | `gofmt -l`, `go vet`, `golangci-lint run`. |
| `make ci` | `lint && test-race && test-e2e && test-coverage`. The CI gate. |
| `make clean` | Remove `bin/`, `coverage.out`, `dist/`. |
| `make release` | Build all platforms, write SHA256 sums, produce `dist/` for upload. |

Project license: **MIT**.

---

## 13. Phased Implementation

Each phase ends with a working, tested, releasable binary. **No phase is "done" until §11 CI gates pass for the phase's deliverables.** Phases must ship in order — the layering depends on it (Phase 3 concurrency assumes Phase 1 storage; Phase 4 WAL assumes Phase 3 leases).

For each phase below, the `Tests required` column lists the specific test categories from §11.3 that must exist and pass before the phase is considered complete. AI agents implementing a phase use this column as the gate, not the `Scope` column.

| Phase | Scope | Go packages touched | Tests required | Outcome |
|---|---|---|---|---|
| **0. Project skeleton** | Repo layout per §12.6, `Makefile`, `go.mod`, CI workflow, `.golangci.yml`. `cmd/corvee/main.go` with a single `corvee version` command that prints a version string. | `cmd/corvee`, `internal/cli`, `internal/domain` (empty) | E2E test for `corvee version`. CI green. | `corvee version` prints, CI passes. Foundation for TDD. |
| **1. Core MVP** | Item domain (struct, validation, status enum), ID parsing/generation, `Store` interface, fsstore adapter, usecase + cli for `init`, `add`, `list`, `show`, `update`, `done`, `delete` (soft only). | `domain/{item,id,status}`, `usecase/{add,list,show,update,done,delete}`, `adapter/fsstore`, `cli/*` | Unit tests for all domain + usecase (≥90% cov per layer). Integration tests for fsstore. E2E for happy-path workflow. | Single-user task tracker. Versioning works. No claims yet. |
| **2. Hierarchy & validation** | `tree`, `validate`, `reindex`, `summary`, `clone`, parent/child integrity rules, dependency cycle detection in graph. Sharded events log with `EventLog` interface and `eventlog` adapter. Index/cache builder with staleness detection. | `domain/graph`, `usecase/{tree,validate,reindex,summary,clone}`, `adapter/{eventlog,index}` | Unit tests for graph cycle detection (incl. property tests). Integration tests for eventlog + index staleness. | Project/epic/story/subtask navigation, dependency validation, fast `list` via index. |
| **3. Distributed-safe concurrency** | Lease IDs (ULIDs), `claim`/`release`/`heartbeat`/`done` with `--lease-id`, `reap`, `next` with tie-breakers and capability matching. Idempotency via `--operation-id`. `Locker` interface for per-item lock files. | `domain/lease`, `usecase/{claim,release,heartbeat,reap,next}`, `adapter/{idgen,clock}` (real impls), `adapter/fsstore` (lock support) | **Concurrency test suite** (§11.3): N-goroutine claim contention, lease validation, idempotency replay. All under `-race -count=10`. | Multiple agents can safely share a backlog on one machine. |
| **4. Multi-file ops & recovery** | Operation journal (WAL) for `move`/`rename`/`delete --cascade`/`clone --with-children`. `corvee recover`. Crash-injection harness. `corvee migrate` (schema_version upgrades). | `usecase/{move,rename,deleteCascade,recover,migrate}`, `adapter/oplog` | Crash-injection tests: panic mid-op, restart, assert recover() yields consistent state. Property tests on rename idempotency. | Multi-file ops are crash-safe. Schema migrations supported. |
| **5. Real-time observability** | `corvee watch` event stream with filtering. Structured `--metadata` JSON on `corvee done` and `corvee journal`. `journal[].metadata` schema. | `usecase/{watch,journal}` (extension), `adapter/watch` (fsnotify) | Integration tests: write event in goroutine A, watch picks it up in goroutine B within timeout. Filter correctness. | Reactive planner agents possible. Structured handoff data. |
| **6. HTML render & deploy handoff** | `render` with board/tree/summary views, default + dark themes, deploy manifest for handoff to vercel skill (§16). Embed templates and CSS via `embed.FS`. | `adapter/render`, `usecase/render` | HTML snapshot tests against fixture workspaces. Manifest schema validation. | Visual progress dashboard, deployable artifact. |
| **7. Git sync & distributed merge** | `sync`, `merge` with deterministic claim-conflict resolution (Layer 8). `auto_sync_seconds` background loop. Cross-VM tests. `corvee export` (JSON Graph / Cypher / DOT) and `corvee export-plan`. | `adapter/git`, `usecase/{sync,merge,export,exportPlan,import}` | E2E with two repos and a bare middle: planner pushes, executor pulls, claims, completes, planner sees update. Conflict scenarios resolve deterministically. | Cross-machine distribution. Truly multi-VM. |
| **8. Plugins, hooks, compaction** | Plugin protocol (external binaries via `os/exec` + JSON over stdio). Shell hooks. `corvee compact`. `agent_safe` enforcement. | `usecase/{compact,hooks}`, plugin protocol docs | Plugin contract tests with a sample plugin. Hook execution tests. Compaction round-trip preserves state. | Extensibility surfaces live. Maintenance command shipped. |

**Releasable checkpoints**: Phases 1, 3, 5, 7 produce shippable binaries. v0.1 = Phase 1 done. v0.3 = Phase 3 done (multi-agent local). v0.5 = Phase 5 done (with watch + metadata). v1.0 = Phase 7 done (multi-VM). Phase 8 is v1.x maintenance/extension work.

**Phase ordering rationale:**
- Phase 0 establishes TDD discipline before any feature code; CI gates exist from day one.
- Phase 1 ships a single-user tracker so the schema and storage are pinned before concurrency complicates them.
- Phase 2 layers hierarchy and validation on stable storage, again before concurrency.
- Phase 3 introduces all concurrency primitives in one focused phase. The concurrency test suite is the gate.
- Phase 4 hardens against crashes — must come before exposing the system to real workloads.
- Phase 5 adds observability before the visual layer (render). An agent loop wants `watch` more than HTML.
- Phase 6 (HTML) before Phase 7 (git sync) because render is read-only and lower-risk; sync is the most failure-prone phase and benefits from a working render to debug with.
- Phase 8 last — extensibility is for users, not the core team.

Each phase's first commit must be a `make ci`-green skeleton with failing tests for the phase's scope. Implementation fills in the tests one at a time. This forces TDD compliance at the structural level.

---

## 14. Implementation Contracts (For AI Agents)

This section pins down the Go interfaces, error types, and naming conventions that the implementation must follow. Earlier sections describe **what** the system does; this section describes **how to express it in Go** so an AI agent can write the code without inventing signatures.

If something in earlier sections conflicts with this section, this section wins for code shape; the earlier sections win for behavior.

### 14.1 Domain ports (interfaces in `internal/domain/ports.go`)

These are the interfaces `usecase` depends on. `adapter/*` implements them. Tests use `testfakes/`.

```go
package domain

import (
    "context"
    "io"
    "time"
)

// Store is the persistence boundary. fsstore implements it for production;
// testfakes.Store implements it for unit tests.
type Store interface {
    // Get returns the item with the given ID. Returns ErrNotFound if missing.
    Get(ctx context.Context, id string) (Item, error)

    // List returns items matching the filter. Filter fields are AND-combined.
    // Empty filter returns all items.
    List(ctx context.Context, filter ListFilter) ([]Item, error)

    // Put writes the item. The item's Version field is incremented and
    // UpdatedAt is set to clock.Now(). If expectVersion >= 0 and the on-disk
    // version differs, returns ErrVersionConflict without writing.
    Put(ctx context.Context, item Item, expectVersion int) (Item, error)

    // Delete removes the item file. Returns ErrNotFound if missing.
    // Soft-delete (status=abandoned) is done via Put, not Delete.
    Delete(ctx context.Context, id string) error

    // Exists is a cheap existence check that doesn't read the file.
    Exists(ctx context.Context, id string) (bool, error)
}

// EventLog is the append-only audit log boundary.
type EventLog interface {
    // Append writes one event line to today's shard. Atomic per line.
    Append(ctx context.Context, e Event) error

    // Read returns events matching the filter, in timestamp order.
    Read(ctx context.Context, filter EventFilter) ([]Event, error)

    // Tail streams matching events to the channel as they're appended.
    // Closes the channel when ctx is cancelled.
    Tail(ctx context.Context, filter EventFilter, out chan<- Event) error
}

// OpLog is the operation journal (WAL) boundary for multi-file ops.
type OpLog interface {
    // Begin records an operation intent. Returns the op_id.
    Begin(ctx context.Context, op Operation) (string, error)

    // MarkStep records that a step within the operation completed.
    MarkStep(ctx context.Context, opID string, stepID int) error

    // Complete marks the operation as fully done. Eligible for cleanup.
    Complete(ctx context.Context, opID string) error

    // Pending returns operations in pending or executing state — used by
    // recover() to detect orphaned operations.
    Pending(ctx context.Context) ([]Operation, error)
}

// Locker provides per-item exclusive locks for the duration of a write.
// Production impl uses .tasks/locks/<id>.lock with O_EXCL; fake uses
// an in-memory mutex map.
type Locker interface {
    // Acquire blocks until the lock is obtained or ctx is cancelled.
    // Returns a release function. The caller MUST call release.
    Acquire(ctx context.Context, id string) (release func(), err error)
}

// Clock abstracts time.Now() for testability. Production impl returns
// time.Now().UTC(); fake returns whatever the test sets.
type Clock interface {
    Now() time.Time
}

// IDGen abstracts ULID generation for testability.
type IDGen interface {
    // ULID returns a sortable, monotonically-increasing identifier
    // suitable for lease_id and op_id.
    ULID() string
}

// Indexer maintains the local cache (.tasks/index.json).
type Indexer interface {
    // Read returns the index, rebuilding it if stale.
    Read(ctx context.Context) (Index, error)

    // Rebuild forces a full rebuild from items + events.
    Rebuild(ctx context.Context) (Index, error)

    // Invalidate marks the index dirty (next Read will rebuild).
    Invalidate(ctx context.Context) error
}
```

### 14.2 Domain types (in `internal/domain/types.go`)

```go
package domain

import "time"

type ItemType string

const (
    TypeProject ItemType = "project"
    TypeEpic    ItemType = "epic"
    TypeStory   ItemType = "story"
    TypeSubtask ItemType = "subtask"
)

type Status string

const (
    StatusBacklog    Status = "backlog"
    StatusReady      Status = "ready"
    StatusClaimed    Status = "claimed"
    StatusInProgress Status = "in_progress"
    StatusReview     Status = "review"
    StatusBlocked    Status = "blocked"
    StatusDone       Status = "done"
    StatusAbandoned  Status = "abandoned"
)

type Priority string

const (
    PriorityCritical Priority = "critical"
    PriorityHigh     Priority = "high"
    PriorityMedium   Priority = "medium"
    PriorityLow      Priority = "low"
)

type Kind string

const (
    KindFeature  Kind = "feature"
    KindBug      Kind = "bug"
    KindChore    Kind = "chore"
    KindSpike    Kind = "spike"
    KindDocs     Kind = "docs"
    KindRefactor Kind = "refactor"
)

type Risk string

const (
    RiskLow    Risk = "low"
    RiskMedium Risk = "medium"
    RiskHigh   Risk = "high"
)

type Role string

const (
    RolePlanner  Role = "planner"
    RoleExecutor Role = "executor"
    RoleReviewer Role = "reviewer"
    RoleHuman    Role = "human"
)

type Claim struct {
    Agent     string    `json:"agent"`
    LeaseID   string    `json:"lease_id"`
    ClaimedAt time.Time `json:"claimed_at"`
    ExpiresAt time.Time `json:"expires_at"`
}

type Impact struct {
    Files          []string `json:"files,omitempty"`
    Modules        []string `json:"modules,omitempty"`
    EstimatedHours float64  `json:"estimated_hours,omitempty"`
    EstimatedLOC   int      `json:"estimated_loc,omitempty"`
    Risk           Risk     `json:"risk,omitempty"`
}

type JournalEntry struct {
    Timestamp time.Time              `json:"ts"`
    Actor     string                 `json:"actor"`
    Event     string                 `json:"event"`
    Note      string                 `json:"note,omitempty"`
    Metadata  map[string]any         `json:"metadata,omitempty"`
}

type Item struct {
    SchemaVersion        int            `json:"schema_version"`
    ID                   string         `json:"id"`
    Type                 ItemType       `json:"type"`
    Kind                 Kind           `json:"kind,omitempty"`
    ProjectID            string         `json:"project_id,omitempty"`
    ParentID             string         `json:"parent_id,omitempty"`
    Title                string         `json:"title"`
    Description          string         `json:"description,omitempty"`
    Status               Status         `json:"status"`
    Priority             Priority       `json:"priority,omitempty"`
    Tags                 []string       `json:"tags,omitempty"`
    Deliverable          string         `json:"deliverable,omitempty"`
    Claim                *Claim         `json:"claim,omitempty"`
    RequiredCapabilities []string       `json:"required_capabilities,omitempty"`
    CreatedAt            time.Time      `json:"created_at"`
    CreatedBy            string         `json:"created_by,omitempty"`
    CreatedByRole        Role           `json:"created_by_role,omitempty"`
    UpdatedAt            time.Time      `json:"updated_at"`
    CompletedAt          *time.Time     `json:"completed_at,omitempty"`
    DueDate              *time.Time     `json:"due_date,omitempty"`
    Version              int            `json:"version"`
    Impact               Impact         `json:"impact,omitempty"`
    Dependencies         []string       `json:"dependencies,omitempty"`
    Blocks               []string       `json:"blocks,omitempty"`
    Aliases              []string       `json:"aliases,omitempty"`
    AcceptanceCriteria   []string       `json:"acceptance_criteria,omitempty"`
    Journal              []JournalEntry `json:"journal,omitempty"`
}

type ListFilter struct {
    Statuses     []Status
    Types        []ItemType
    Kinds        []Kind
    ProjectID    string
    ParentID     string
    Assignee     string
    Tags         []string
    Capabilities []string // for --match-capabilities
    Unblocked    bool
    Unassigned   bool
    Limit        int
    Sort         string // "priority" | "created_at" | "updated_at" | "due_date"
    Order        string // "asc" | "desc"
}

type Event struct {
    Timestamp time.Time      `json:"ts"`
    Actor     string         `json:"actor"`
    ID        string         `json:"id"`
    Event     string         `json:"event"`
    From      string         `json:"from,omitempty"`
    To        string         `json:"to,omitempty"`
    OpID      string         `json:"op_id,omitempty"`
    LeaseID   string         `json:"lease_id,omitempty"`
    Metadata  map[string]any `json:"metadata,omitempty"`
}

type EventFilter struct {
    EventTypes []string
    IDPrefix   string // matches item ID and all descendants
    Actor      string
    Since      time.Time
    Until      time.Time
}

type Operation struct {
    OpID      string    `json:"op_id"`
    Kind      string    `json:"kind"` // "move" | "rename" | "delete_cascade" | "clone_with_children"
    Agent     string    `json:"agent"`
    StartedAt time.Time `json:"started_at"`
    Status    string    `json:"status"` // "pending" | "executing" | "completed" | "failed"
    Steps     []OpStep  `json:"plan"`
}

type OpStep struct {
    Step int            `json:"step"`
    Kind string         `json:"kind"`
    Args map[string]any `json:"args"`
    Done bool           `json:"done"`
}

type Index struct {
    BuiltAt          time.Time                  `json:"built_at"`
    ItemCount        int                        `json:"item_count"`
    MaxMtime         time.Time                  `json:"max_mtime"`
    ByID             map[string]Item            `json:"by_id"`
    ByStatus         map[Status][]string        `json:"by_status"`
    ByAssignee       map[string][]string        `json:"by_assignee"`
    ByParent         map[string][]string        `json:"by_parent"`
    Unblocked        []string                   `json:"unblocked"`
    DependencyGraph  map[string][]string        `json:"dependency_graph"`
}
```

### 14.3 Error sentinels (in `internal/domain/errors.go`)

Production code returns these via `errors.Is`/`errors.As`. The CLI maps them to exit codes (§15.1).

```go
package domain

import "errors"

var (
    ErrNotFound          = errors.New("not found")              // exit 3
    ErrVersionConflict   = errors.New("version conflict")       // exit 4
    ErrAlreadyClaimed    = errors.New("already claimed")        // exit 5
    ErrLeaseMismatch     = errors.New("lease mismatch")         // exit 5
    ErrClaimLost         = errors.New("claim lost in merge")    // exit 5
    ErrSchemaInvalid     = errors.New("schema validation failed") // exit 6
    ErrIntegrityViolated = errors.New("integrity violation")    // exit 7
    ErrLockTimeout       = errors.New("lock acquisition timeout") // exit 8
    ErrPendingOperation  = errors.New("incomplete operation; run corvee recover") // exit 9
    ErrUsage             = errors.New("usage error")            // exit 2
    ErrInvalidStatus     = errors.New("invalid status transition")
    ErrCycleDetected     = errors.New("dependency cycle detected")
    ErrMalformedJSON     = errors.New("malformed JSON")
)
```

Usecase functions wrap these with context. Example:

```go
return fmt.Errorf("claim %s: %w", id, ErrAlreadyClaimed)
```

CLI maps via:

```go
switch {
case errors.Is(err, domain.ErrNotFound):       os.Exit(3)
case errors.Is(err, domain.ErrVersionConflict): os.Exit(4)
case errors.Is(err, domain.ErrAlreadyClaimed),
     errors.Is(err, domain.ErrLeaseMismatch),
     errors.Is(err, domain.ErrClaimLost):       os.Exit(5)
// ...
}
```

### 14.4 Usecase function signature pattern

Every usecase function follows this shape exactly:

```go
package usecase

import (
    "context"
    "github.com/mandarnilange/corvee/internal/domain"
)

// Deps groups all interface dependencies a usecase might need.
// Individual usecases use only the subset they require.
type Deps struct {
    Store    domain.Store
    EventLog domain.EventLog
    OpLog    domain.OpLog
    Locker   domain.Locker
    Clock    domain.Clock
    IDGen    domain.IDGen
    Indexer  domain.Indexer
}

// ClaimInput is the request payload.
type ClaimInput struct {
    ID           string
    Agent        string
    TTL          time.Duration
    OperationID  string // for idempotency
}

// ClaimOutput is the response payload.
type ClaimOutput struct {
    Item    domain.Item
    LeaseID string
}

// Claim atomically claims an item for the given agent.
// Returns ErrNotFound, ErrAlreadyClaimed, or ErrInvalidStatus.
func Claim(ctx context.Context, d Deps, in ClaimInput) (ClaimOutput, error) {
    // ... implementation
}
```

Conventions, all enforced:
- One usecase = one exported function = one file = one test file.
- Input struct (`<Verb>Input`) and Output struct (`<Verb>Output`) defined alongside.
- First arg always `context.Context`. Second arg always `Deps`. Third arg always the input struct.
- Return either `(<Verb>Output, error)` or just `error`.
- Function body: validate input → acquire lock if needed → read state → apply domain rules → write state → emit event → return.

### 14.5 CLI command pattern

Every CLI verb follows this shape (in `internal/cli/`):

```go
package cli

import (
    "github.com/spf13/cobra"
    "github.com/mandarnilange/corvee/internal/usecase"
)

func newClaimCmd(deps Deps) *cobra.Command {
    var (
        agent       string
        ttlMinutes  int
        operationID string
    )
    cmd := &cobra.Command{
        Use:   "claim <id>",
        Short: "Atomically claim an item for the current agent",
        Args:  cobra.ExactArgs(1),
        RunE: func(cmd *cobra.Command, args []string) error {
            out, err := usecase.Claim(cmd.Context(), deps.Usecase, usecase.ClaimInput{
                ID:          args[0],
                Agent:       resolveAgent(agent, deps),
                TTL:         time.Duration(ttlMinutes) * time.Minute,
                OperationID: operationID,
            })
            if err != nil {
                return err // root handler maps to exit code
            }
            return printJSON(cmd.OutOrStdout(), out)
        },
    }
    cmd.Flags().StringVar(&agent, "agent", "", "Agent ID (overrides env/config)")
    cmd.Flags().IntVar(&ttlMinutes, "ttl-minutes", 0, "Override claim TTL")
    cmd.Flags().StringVar(&operationID, "operation-id", "", "Idempotency key")
    return cmd
}
```

Conventions:
- One command = one file = one `new<Verb>Cmd(deps)` function returning `*cobra.Command`.
- All flags defined in the same function via `cmd.Flags()`. No package-level globals.
- `RunE` (not `Run`) so errors propagate. Exit-code mapping happens once in `cli.Execute()`.
- JSON output via shared `printJSON` helper (handles `--pretty` and `--text`).
- No business logic in `cli/` — only flag parsing, output formatting, and one usecase call.

### 14.6 Test fakes pattern

Every fake in `internal/usecase/testfakes/` implements the corresponding `domain` interface and exposes exported fields for inspection:

```go
package testfakes

import (
    "context"
    "errors"
    "sync"

    "github.com/mandarnilange/corvee/internal/domain"
)

type Store struct {
    mu    sync.Mutex
    Items map[string]domain.Item // exported for test assertions
    PutErr error                  // tests inject errors
}

func NewStore() *Store {
    return &Store{Items: map[string]domain.Item{}}
}

func (s *Store) Get(ctx context.Context, id string) (domain.Item, error) {
    s.mu.Lock()
    defer s.mu.Unlock()
    item, ok := s.Items[id]
    if !ok {
        return domain.Item{}, domain.ErrNotFound
    }
    return item, nil
}

// ... etc
```

Tests use them like:

```go
func TestClaim_FailsWhenAlreadyClaimed(t *testing.T) {
    store := testfakes.NewStore()
    store.Items["RKN-E01-S01"] = domain.Item{
        ID:     "RKN-E01-S01",
        Status: domain.StatusClaimed,
        Claim:  &domain.Claim{Agent: "agent-A", LeaseID: "01HX..."},
    }
    deps := usecase.Deps{
        Store: store,
        Clock: testfakes.FixedClock(t),
        IDGen: testfakes.SeqIDGen("01HY..."),
    }

    _, err := usecase.Claim(t.Context(), deps, usecase.ClaimInput{
        ID:    "RKN-E01-S01",
        Agent: "agent-B",
    })

    if !errors.Is(err, domain.ErrAlreadyClaimed) {
        t.Fatalf("want ErrAlreadyClaimed, got %v", err)
    }
}
```

### 14.7 Conventions agents must follow

When implementing, AI agents adhere to:

- **No global state.** No package-level vars except constants and error sentinels. Everything flows through `Deps`.
- **No `init()` functions** except for embed.FS loaders.
- **No goroutines in `domain` or `usecase`.** Concurrency lives in `adapter/`.
- **No `panic` except for "this is impossible" assertions** (e.g., default case in an exhaustive switch over a closed enum). User-facing errors return through the normal error path.
- **Context propagation.** Every function that does I/O or could block takes `ctx context.Context` as the first argument.
- **`errors.Is` and `errors.As`** for error checks. Never compare error strings.
- **Every exported identifier has a godoc comment** starting with the identifier's name. CI runs `go vet` which checks this.
- **Imports grouped** in three blocks: stdlib, third-party, local. Enforced by `goimports`.
- **No magic numbers.** Constants get names. `60 * time.Minute` becomes `defaultClaimTTL`.

### 14.8 What an agent does when it hits ambiguity

If an agent implementing this section finds the spec underspecified for a concrete decision (e.g., "what should `corvee list` do when given an empty workspace?"), it:

1. Writes the test that codifies its best guess at the answer, with a comment marking the assumption.
2. Implements the code to satisfy the test.
3. Adds an entry to `docs/spec-questions.md` describing the ambiguity and the resolution chosen.
4. Proceeds.

This makes assumptions visible and reviewable rather than buried.

---

## 15. CLI Reference (Detailed)

### 15.1 Global Conventions

**Invocation**: `corvee <verb> [args] [flags]`

**Global flags** (apply to every command):

| Flag | Purpose |
|---|---|
| `--pretty` | Indented JSON output (default is compact single-line JSON) |
| `--text` | Human-readable text output instead of JSON |
| `--workspace <path>` | Override workspace auto-discovery |
| `--agent <id>` | Override agent ID for this invocation (else uses `CORVEE_AGENT_ID` env var, else config default) |
| `--operation-id <ULID>` | Idempotency key. Re-running with the same ID is a no-op that returns the prior result. Auto-minted if absent; agents should always pass their own. |
| `--quiet` | Suppress non-essential output |
| `--help` | Print help for the verb and exit |

**Output schema** — every command emits one JSON object on stdout:

```json
// success
{ "ok": true, "data": <command-specific payload> }

// failure
{ "ok": false, "error": { "code": "STRING_CODE", "message": "...", "details": {} } }
```

Errors go to **stdout** (not stderr) so agents can parse them with the same path as success. Stderr is reserved for diagnostic logs (visible only with `--verbose`).

**Exit codes** — agents can branch on these without parsing JSON:

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error |
| 2 | Usage / argument error |
| 3 | Not found (item or workspace) |
| 4 | Version conflict (concurrent modification) |
| 5 | Claim conflict (already claimed, or wrong lease_id) |
| 6 | Schema validation failure |
| 7 | Dependency or integrity violation |
| 8 | Lock acquisition timeout |
| 9 | Incomplete operation detected — run `corvee recover` |

### 15.2 Command Reference

Each entry: synopsis → flags → output payload → relevant exit codes.

---

**`corvee init [--project <CODE>] [--agent <ID>]`**
Bootstrap `.tasks/` in cwd. Optionally seeds an initial project.
- Output: `{ workspace: "<abs path>", project: "<CODE>" | null, agent: "<ID>" }`
- Errors: 1 (workspace already exists)

---

**`corvee add <type> --title <STRING> [opts]`**
Create a new item. `type` ∈ `project | epic | story | subtask`. For non-project types, `--parent` is required and must point to a parent of the legal type (epic→project, story→epic, subtask→story).
- Flags: `--parent <ID>`, `--description <STRING>`, `--priority <critical|high|medium|low>`, `--tags <CSV>`, `--depends-on <CSV>`, `--impact-files <CSV>`, `--impact-modules <CSV>`, `--estimated-loc <N>`, `--risk <low|medium|high>`, `--acceptance <STRING>` (repeatable), `--due <ISO-DATE>`
- Output: full item JSON
- Errors: 2 (invalid type/args), 3 (parent missing), 6 (validation), 7 (parent type mismatch)

---

**`corvee list [filters]`**
Query items. All filters AND together. Multi-value filters use CSV.
- Flags: `--status <CSV>`, `--type <CSV>`, `--project <CODE>`, `--parent <ID>`, `--assignee <ID>`, `--tag <CSV>`, `--unblocked` (no unmet dependencies), `--unassigned`, `--limit <N>`, `--sort <FIELD>` (priority|created_at|updated_at|due_date), `--order <asc|desc>`
- Output: `{ count: <N>, items: [<item>, ...] }`
- Errors: 2 (invalid filter)

---

**`corvee show <id>`**
Full item JSON including journal.
- Output: full item
- Errors: 3 (not found)

---

**`corvee update <id> [mutators] [--expect-version <N>]`**
Generic mutation. `--expect-version` enables agent-side optimistic concurrency: if the on-disk version doesn't match, fail with exit 4 instead of merging.
- Mutators: `--status <S>`, `--priority <P>`, `--title <S>`, `--description <S>`, `--assignee <ID>`, `--due <ISO>`, `--add-tag <T>`, `--remove-tag <T>`, `--add-impact-file <F>`, `--remove-impact-file <F>`, `--add-dep <ID>`, `--remove-dep <ID>`, `--add-acceptance <S>`, `--remove-acceptance <INDEX>`, `--risk <LEVEL>`
- Output: `{ item: <updated>, prev_version: <N>, new_version: <N+1> }`
- Errors: 3 (not found), 4 (version conflict), 6 (validation)

---

**`corvee delete <id> [--hard] [--cascade] [--force]`**
Soft-delete (status=abandoned) by default. `--hard` removes the JSON file (git history preserves it). `--cascade` includes children. Refuses by default if any other item lists this ID in `dependencies` or `blocks`; `--force` overrides with a warning.
- Output: `{ deleted: ["<id>", ...], soft: <bool> }`
- Errors: 3 (not found), 7 (has dependents w/o force, or has children w/o cascade)

---

**`corvee move <id> --to <parent-id>`**
Re-parent. Computes new ID under target parent; cascades through descendants; rewrites references in `dependencies`/`blocks` across all items; appends old IDs to each moved item's `aliases[]`. Atomic at workspace level.
- Output: `{ mapping: { "<old-id>": "<new-id>", ... }, items_updated: <N> }`
- Errors: 2 (target invalid, would create cycle), 3 (not found)

---

**`corvee rename <id> --to <new-id>`**
Same machinery as move but takes an explicit target ID. Old ID added to `aliases[]`.
- Output: `{ old: "<id>", new: "<new-id>", aliases_added: [...] }`
- Errors: 1 (target already exists), 2 (target invalid), 3 (not found)

---

**`corvee clone <id> [--to <parent>] [--with-children]`**
Duplicate with fresh ID; resets `status=backlog`, `assignee=null`, `claimed_at=null`, `completed_at=null`, `journal=[{created}]`, `version=1`. Children cloned only with `--with-children`.
- Output: full new item (or `{ root: <item>, descendants: [...] }` with cascade)
- Errors: 3 (not found)

---

**`corvee claim <id> [--ttl-minutes <N>]`**
Atomic claim by current agent. Writes a structured `claim` object (`agent`, `lease_id`, `claimed_at`, `expires_at`) and sets `status=claimed`. Succeeds only if currently unclaimed and status ∈ `{backlog, ready}`. TTL default from config (60 min).
- Output: `{ id, agent, lease_id, claimed_at, expires_at, version }` — **agents must retain the `lease_id` for subsequent operations**
- Errors: 3 (not found), 5 (already claimed), 1 (invalid current status)

---

**`corvee release <id> --lease-id <ID>`**
Release current agent's claim. `--lease-id` required and must match on-disk lease. Reverts status to `ready` unless explicitly changed.
- Output: `{ id, version }`
- Errors: 3 (not found), 5 (lease_id mismatch — claim was lost or already released)

---

**`corvee heartbeat <id> --lease-id <ID>`**
Refresh `expires_at` to `now + TTL`. Required periodically (default every 30 min) to prevent reaping. `--lease-id` required and must match.
- Output: `{ id, claimed_at, expires_at }`
- Errors: 3 (not found), 5 (lease_id mismatch — your claim was lost; do not retry, call `corvee next`)

---

**`corvee done <id> --lease-id <ID> [--note <STRING>] [--metadata <JSON>]`**
Convenience: sets `status=done`, `completed_at=now`, clears the claim, optionally appends a journal entry. `--lease-id` required. `--metadata` accepts an arbitrary JSON object that is attached to the journal entry — used for structured handoff to downstream agents (changed files, tests run, decisions made, artifact paths). The schema is intentionally unconstrained; common patterns are documented in `references/handoff-patterns.md` but no keys are enforced. Pass either as a JSON string (`--metadata '{"changed_files":["a.py"]}'`) or via `--metadata-file <path>` for larger payloads.
- Output: full updated item
- Errors: 2 (malformed JSON), 3 (not found), 5 (lease_id mismatch — use `--force` to override and steal the close)

---

**`corvee journal <id> --note <STRING> [--event <TYPE>] [--metadata <JSON>] [--lease-id <ID>]`**
Append a journal entry. `--event` defaults to `note`. Other event types: `progress`, `blocker`, `decision`, `handoff`. `--metadata` accepts an arbitrary JSON object attached to the entry (same shape as `corvee done`'s metadata). `--lease-id` is required if the item is currently claimed and `--event` is one of `progress | blocker | decision`.
- Output: `{ id, journal_length: <N> }`
- Errors: 2 (malformed JSON), 3 (not found), 5 (lease_id required and mismatched)

---

**`corvee next [filters] [--auto-claim]`**
Pick the next item for the current agent. Selection order:
1. Filter by flags (status defaults to `ready`; type, project, tag, kind, capabilities)
2. Drop items with unmet dependencies
3. Apply selection bonuses: items in `config.json:critical_path` (in declared order) jump above non-critical items at the same priority level
4. Sort by: `priority` desc → critical-path order asc → `due_date` asc (nulls last) → `updated_at` asc (least recently touched first) → `id` asc (stable tie-break)
5. Optional `--affinity <agent-id>`: prefer items where `<agent-id>` appears in past journal `actor` fields (returning agents pick up where they left off)

With `--auto-claim`, atomically claim and return the lease_id; otherwise just return the candidate item.

- Flags: `--type <CSV>`, `--kind <CSV>`, `--project <CODE>`, `--tag <CSV>`, `--match-capabilities` (filter to items whose `required_capabilities` are a subset of the agent's `capabilities` from config), `--max-hours <N>` (filter by `estimated_hours`), `--affinity <AGENT-ID>`, `--auto-claim`
- Output: full item JSON (with `lease_id` if `--auto-claim`), or `null` if nothing available (still exit 0)
- Errors: 5 (auto-claim race lost — caller can retry; `corvee next` will pick a different item)

---

**`corvee summary [--project <CODE>] [--by <FIELD>]`**
Pre-computed roll-up across the workspace. Cheap (reads index, no item-by-item scan). Primary command for status reports and dashboards.
- Flags: `--by <type|status|kind|priority|assignee|project|epic>` (group-by dimension, default `status`)
- Output:
  ```json
  {
    "totals": {
      "items": 326,
      "done": 248,
      "in_progress": 12,
      "blocked": 3,
      "estimated_hours_total": 1840,
      "estimated_hours_remaining": 540,
      "completion_pct": 76.1
    },
    "by_<field>": { "<value>": { "count": N, "done": N, "hours_remaining": N }, ... },
    "critical_path": {
      "ids": ["RKN-E01-S01", "RKN-E01-S02", "RKN-E02-S01"],
      "complete": 1,
      "total": 3,
      "next": "RKN-E01-S02"
    },
    "deliverables": [
      { "epic": "RKN-E01", "deliverable": "...", "status": "in_progress", "completion_pct": 60 }
    ]
  }
  ```
- Errors: 3 (project not found if `--project` given)

---

**`corvee watch [filters] [--since <TS>] [--format <FORMAT>]`**
Stream events from the daily-sharded events log as they happen. The events log is the source of truth for audit (§6 Layer 5); `watch` exposes it as a real-time feed for local consumers. Tails the current day's shard, follows into tomorrow's at midnight, and filters lines matching the given criteria. Local-only — there is no cross-machine push. To see remote events, run `corvee sync` periodically and `watch` will pick up the appended lines.

Primary use cases:
- **Long-lived planner agent** waiting for executor events (`corvee watch --filter event=blocked` to react to discovered blockers, `--filter event=completed` to plan follow-up work).
- **Human in a terminal pane** watching the swarm — gives the live-board feeling without needing a UI server.
- **Debugging concurrent runs** — `corvee watch --filter event=claim_lost` shows Layer 8 merge resolutions as they happen.
- **Hook trigger source** — pipe `corvee watch` into a shell loop that invokes `.tasks/hooks/*` for matching events.

Flags:
- `--filter <KEY=VALUE>` (repeatable): match on any event field (`event`, `actor`, `id`, `from`, `to`, `op_id`, etc.). Multi-flag = AND; CSV in value = OR.
- `--id <ID-OR-PREFIX>`: shorthand for filtering by item ID, including all descendants (e.g., `--id RKN-E01` matches `RKN-E01-S03` and `RKN-E01-S03-T02`).
- `--since <ISO-TS-OR-RELATIVE>`: replay events from a starting point before going live. Supports ISO timestamps and relative (`5m`, `1h`, `today`, `yesterday`).
- `--format <FORMAT>`: `jsonl` (default — one event per line, suitable for piping), `text` (human-readable), `compact` (short single-line summary).
- `--limit <N>`: exit after N matching events (default: stream forever).
- `--exit-on <EVENT-TYPE>`: exit cleanly when a matching event of this type appears (useful for "wait until task X completes" scripts).

Output: streams matching events to stdout, one per line. Does not return a top-level JSON object — each line *is* the event JSON, so consumers can pipe straight to `jq`. Exit 0 on `--limit` or `--exit-on`; exit 130 (SIGINT) on Ctrl+C.

Example agent loop:
```bash
# Planner reacts to blockers
corvee watch --filter event=blocked --format jsonl | while read event; do
  task_id=$(echo "$event" | jq -r '.id')
  # Planner agent invocation here, e.g., add a remediation subtask
done
```

Errors: 2 (invalid filter), 1 (events directory missing — run `corvee init` or `corvee reindex`).

---

**`corvee import <file> [--dry-run] [--prefix <CODE>]`**
Bulk-create items from a single planning JSON. Designed for the up-front-planning case (e.g., handing the spec doc to an LLM that returns a full backlog) and for migrating from formats like the AgentForge `phases[].tasks[]` shape.

Accepts two input formats:
1. **Native**: an array of partial items matching this spec's schema; missing IDs are auto-assigned.
2. **Phases-and-tasks** (AgentForge-style): `{ phases: [{ id, name, description, deliverable, tasks: [{ id, title, description, type, priority, estimatedHours, files, acceptanceCriteria }] }] }`. Each phase becomes an epic; each task becomes a story. The phase ID prefix maps to the project code (`--prefix` overrides). Phase `deliverable` populates the epic's `deliverable` field.

`--dry-run` validates and reports what would be created without writing.
- Output: `{ created: [<id>, ...], skipped: [{ id, reason }], conflicts: [...], plan: {...} }` (full plan only on `--dry-run`)
- Errors: 2 (malformed input), 6 (validation failure on any item — atomic; nothing written if any item invalid), 7 (parent reference unresolvable)

---

**`corvee export-plan [--project <CODE>] [--format <FORMAT>] [--out <PATH>]`**
Inverse of `corvee import`. Exports a project (or whole workspace) as a single planning JSON, suitable for sharing, archival, or feeding into LLM planners.
- Formats: `native` (this spec's schema, array of full items), `phases` (AgentForge-style nested phases-and-tasks), `markdown` (human-readable backlog brief)
- Output: writes to `--out` (default stdout), returns `{ format, items: <N>, out: "<path>" }`
- Errors: 2 (unknown format), 3 (project not found)

---

**`corvee tree [<id>] [--max-depth <N>] [--status <CSV>]`**
Hierarchical view. With no ID, shows all projects. Each node carries its full item plus `children: [...]`.
- Output: `{ roots: [<tree-node>, ...] }`
- Errors: 3 (not found)

---

**`corvee migrate`**
Detect items with `schema_version` lower than the current spec version and apply lazy migrations. Idempotent. Auto-invoked when `corvee list`/`corvee show` encounters an old item; can be run manually for a workspace-wide pass.
- Output: `{ migrated: [{ id, from_version, to_version }], skipped: [...] }`
- Errors: 1 (unmigratable item — manual intervention)

---

**`corvee validate`**
Workspace-wide integrity check: schema conformance per item, parent existence and type-correctness, no dependency cycles, no orphaned aliases, index consistency, no orphaned operation files, clock skew within tolerance, schema_version current.
- Output: `{ valid: <bool>, item_count: <N>, errors: [{ id, code, message }, ...] }`
- Exit 0 even if `valid: false` — surface errors in payload, don't fail the call

---

**`corvee reindex`**
Rebuild the local `index.json` cache from `items/` and the daily events shards. Useful after manual edits, git merges, or a fresh clone. The index is gitignored — every machine maintains its own.
- Output: `{ items_indexed: <N>, events_replayed: <N>, shards_read: <N>, was_stale: <bool> }`

---

**`corvee export --format <FORMAT> [--out <PATH>] [--include <CSV>]`**
Read-only export of the workspace as a portable graph for external tools (D3, Cytoscape, Neo4j, ML pipelines). Does not modify state.
- Formats: `graph` (JSON Graph Format — nodes + edges with metadata), `cypher` (Neo4j load script), `dot` (Graphviz)
- `--include` defaults to all edge types: `parent`, `dependency`, `blocks`, `alias`. Restrict for narrower views.
- Output: writes to `--out` (default stdout), returns `{ format, nodes: <N>, edges: <N>, out: "<path>" }`
- Errors: 2 (unknown format)

---

**`corvee reap`**
Release expired claims (those past `expires_at` plus 5-min skew tolerance). Idempotent.
- Output: `{ reaped: [{ id, was_assigned_to, lease_id, expired_at }, ...] }`

---

**`corvee recover`**
Detect orphaned operation files in `.tasks/operations/` and roll forward (resume from next undone step). Auto-invoked by `corvee list`/`corvee next` when an in-flight op is detected; can also be run manually. If a step is unsafe to roll forward, the op is marked `failed` and surfaced in `corvee validate`.
- Output: `{ recovered: [{ op_id, kind, steps_completed, steps_skipped }], failed: [...] }`
- Errors: 1 (unrecoverable op — manual intervention required, see `corvee show-op <op-id>`)

---

**`corvee compact [--before <DATE>]`**
Maintenance command (Phase 6). Squashes events into item state, archives or deletes old daily event shards, prunes resolved operation files, garbage-collects stale aliases. Default cutoff: 90 days. Idempotent.
- Output: `{ shards_archived: <N>, ops_pruned: <N>, aliases_pruned: <N>, bytes_saved: <N> }`

---

**`corvee render [--out <dir>] [--theme <name>]`**
Generate the HTML dashboard. Default `--out` is `.tasks/dist/`. Produces a static-hostable directory with a manifest (see Section 17).
- Output: `{ out_dir: "<abs>", files: ["index.html", ...], manifest: {...}, stats: {projects, epics, stories, subtasks} }`
- Errors: 1 (write failure)

---

## 16. SKILL.md Specification

The skill is a self-contained folder agents can drop into their skill directory. The CLI lives inside the skill as a pre-built static Go binary (one per supported `GOOS/GOARCH` triple); there is nothing external to install at runtime beyond a POSIX shell.

### 16.1 Skill Folder Layout

```
corvee/
  SKILL.md              # the skill manifest — agents read this first
  README.md             # human docs (skill consumers can ignore)
  bin/                  # pre-built CLI binaries (one per platform)
    corvee-linux-amd64
    corvee-linux-arm64
    corvee-darwin-arm64
  references/           # loaded by agents on demand
    workflows.md
    schema.md
    error-codes.md
    topologies.md
    handoff-patterns.md
```

The skill is published as a release asset of the upstream Go module. End users pin a release tag and unpack the asset into their skills directory; their agent harness selects the appropriate `bin/corvee-<platform>` binary. (Resolves SQ-001 in `docs/spec-questions.md` — the original §16.1 layout was Node-based residue from an earlier draft.)

### 16.2 Frontmatter

```yaml
---
name: corvee
description: Manage hierarchical, multi-agent task backlogs through a local CLI. Use this skill whenever the user mentions tasks, todos, backlog, work items, sprints, epics, stories, subtasks, or asks "what should I work on next." Also use it whenever an agent needs to claim work in a coordinated multi-agent setup, record progress on an in-flight task, mark something done, or answer questions about project status. Even if the user doesn't say "task tracker" explicitly, reach for this skill any time work is being created, picked up, updated, completed, or reviewed across a project.
---
```

The description is intentionally pushy and enumerates concrete trigger phrases — this combats Claude's tendency to undertrigger skills (per skill-creator guidance).

### 16.3 SKILL.md Body Structure

Required sections, in order:

1. **When to use** — 1-2 paragraphs of concrete trigger conditions, including ones beyond the description (e.g., "user pasting a list of TODOs from a meeting").
2. **Setup check** — first thing the agent should do: confirm a `.tasks/` workspace exists; if not, ask whether to `corvee init`.
3. **Standard agent workflow** — the discover → next → claim → heartbeat → journal → done loop, in prose with example invocations.
4. **Quick reference** — compact table of the 8-10 most-used commands with one-line descriptions. Pointer to Section 15 of this spec for the full reference.
5. **Error handling guide** — key exit codes (especially 4 and 5) with the right reaction for each. E.g., "Exit 5 from `claim` means another agent got there first — call `corvee next` to pick a different item."
6. **Deployment via vercel skill** — when the user asks to publish the board, render to `.tasks/dist/` and hand off to the vercel skill (Section 17).
7. **Multi-agent coordination notes** — heartbeat cadence, when to release vs let TTL expire, how to handoff between agents.
8. **Pointers to references/** — three deeper docs the agent can `view` on demand without bloating context.

Body length target: <400 lines. The progressive-disclosure pattern keeps SKILL.md scannable; deep material lives in `references/`.

### 16.4 Reference Files

- **`references/workflows.md`** — multi-step recipes: **planner-executor distributed setup** (the canonical multi-VM topology), planning a sprint, breaking down an epic, handing off mid-task, recovering from a crashed agent, executor onboarding (cloning a workspace and joining the pool). Each workflow is a numbered step list with example commands.
- **`references/schema.md`** — full task schema, every field with semantics, allowed values, examples. Also documents `aliases[]` resolution and how `version` interacts with `--expect-version`. Includes the `agent_role` matrix from §7.1.
- **`references/error-codes.md`** — every exit code expanded: cause, recovery recipe, example. Especially detailed for codes 4 (version conflict) and 5 (claim conflict) since those are the multi-agent failure modes agents will hit most.
- **`references/topologies.md`** — the patterns from §7: planner-executor, peer executors, reviewer-in-the-loop. When to use each, sync cadences, failure modes.
- **`references/handoff-patterns.md`** — common conventions for `journal[].metadata` payloads (changed_files, tests_run, decisions, artifact paths, metrics). Guidance for agents on what to attach when completing or progressing on a task. Not enforced by the schema; downstream agents and skills can rely on conventions documented here.

### 16.5 Standard Agent Workflow (Canonical)

The loop differs by role. SKILL.md documents both verbatim.

**Executor loop** (the most common case):

```
0. Sync first:                   corvee sync           (pull peer changes)
1. Confirm workspace:            corvee list --limit 1 (exit 3 → init or ask)
2. Pick work:                    corvee next --auto-claim --match-capabilities
                                 → returns {item, lease_id}, or null
3. Heartbeat every 30 min:       corvee heartbeat <id> --lease-id <LEASE>
4. Record progress:              corvee journal <id> --note "..." --event progress \
                                              --metadata '{"step":"design","files":["x.py"]}' \
                                              --lease-id <LEASE>
5. Sync periodically:            corvee sync           (every ~60s during long tasks)
6. Block discovered:             corvee update <id> --status blocked --lease-id <LEASE>
                                 corvee journal <id> --note "..." --event blocker --lease-id <LEASE>
                                 corvee release <id> --lease-id <LEASE>
                                 corvee sync           (let planner see it)
7. Complete:                     corvee done <id> --lease-id <LEASE> --note "summary" \
                                          --metadata '{"changed_files":[...],"tests_passed":18}'
                                 corvee sync
8. Loop back to step 0
```

**Planner loop**:

```
1. Read state:                   corvee summary       (overall progress)
                                 corvee list --status blocked
                                 corvee list --created-after <last-sync>
                                 (optional) corvee watch --filter event=blocked,completed &
                                            ↑ background event stream for reactive planning
2. Plan work:                    corvee add ...       (or corvee import plan.json)
3. Set ordering:                 update config.json:critical_path if needed
4. Publish:                      corvee sync          (executors can now see it)
5. Observe:                      corvee summary --by status
                                 corvee list --status done --completed-after <ts>
6. Replan blocked items, loop.
```

Both loops respect lease_id semantics. Agents must retain the lease_id returned by `claim` / `next --auto-claim` and pass it on every subsequent operation against that item.

### 16.6 Optional Adapters (Not in v1 Core)

The CLI is the agent interface. Agents shell out to `corvee <verb>` the same way they shell out to `git`, `gh`, or `npm`. Nothing else is required for any agent in any harness — Claude Code, Cursor, a cron job, a CI runner, a custom Python loop — to fully use this tool.

That said, certain harnesses prefer specific transports. Adapters that wrap the CLI without changing it are sensible v2 extensions if real demand emerges. **None of these ship in v1.** They are documented here so the design stays open to them, not as commitments.

**MCP adapter (`bin/corvee-mcp`).** A stdio MCP server that exposes the CLI verbs as MCP tools. Implementation is a thin wrapper — it shells out to `corvee <verb>` and reformats stdout into MCP responses. The agent in an MCP-aware IDE (Claude Code, Cursor, Windsurf, etc.) gets tool discovery and inline schema validation; the underlying behavior is identical to the CLI. Not added to v1 because (a) the CLI already works for every agent that can shell out, and (b) shipping two interfaces means keeping them in sync. Add when a concrete user need surfaces, not preemptively.

**Coding-agent worktree wrapper (`bin/corvee-worktree`).** A wrapper that pairs `corvee claim` with `git worktree add`, returns the worktree path, and pairs `corvee done` with `git worktree remove`. Useful for coding-agent harnesses that run each task in an isolated worktree (Bernstein, agent-kanban, parallel-code patterns). Not core because worktrees are an *execution* concern, not a *coordination* concern; the wrapper conflates layers if forced into the core CLI.

**PRD-to-tasks sibling skill.** A separate skill that takes a PRD (or any prose brief), calls an LLM, produces a JSON plan in our native or phases format, and pipes it to `corvee import`. This is exactly the pattern §1.1 describes: content generation happens outside; we ingest the result. This skill is **not part of this project** — it's a downstream consumer that any LLM-using team can build (or already has, if they're using Taskmaster's `parse-prd` and want to migrate).

The principle behind all three: the core stays focused on coordination. Anything that bridges to a different transport, execution model, or content-generation pipeline lives outside, calling our CLI.

---

## 17. Vercel Deployment Integration

### 17.1 Decoupling Principle

The corvee skill **never deploys**. It produces a static-hostable artifact. Deployment is delegated to a separate skill (Vercel, Netlify, GitHub Pages, S3 — whichever is connected). This keeps corvee free of deployment auth, hosting concerns, and provider lock-in, and keeps the deployment skill reusable across other artifact-producing skills.

### 17.2 Output Contract

`corvee render --out .tasks/dist/` produces:

```
.tasks/dist/
  index.html         # board view, default landing
  tree.html          # hierarchical view
  summary.html       # progress + velocity
  assets/
    styles.css
    app.js
  manifest.json      # metadata for the deployment skill
```

`manifest.json` schema:

```json
{
  "name": "<project-or-workspace-name>",
  "version": "<git-sha-if-available>-<unix-ts>",
  "generated_at": "2026-05-02T10:00:00Z",
  "entrypoint": "index.html",
  "static": true,
  "framework": null,
  "build_command": null,
  "output_directory": ".",
  "routes": [
    { "src": "/", "dest": "/index.html" },
    { "src": "/tree", "dest": "/tree.html" },
    { "src": "/summary", "dest": "/summary.html" }
  ]
}
```

The manifest fields (`entrypoint`, `static`, `framework`, `build_command`, `output_directory`, `routes`) match what most static-deployment skills expect, so the handoff requires no adapter logic on either side.

### 17.3 Agent Handoff Pattern

The published-the-board flow:

1. Agent runs `corvee render --out .tasks/dist/`
2. Agent reads `.tasks/dist/manifest.json`
3. Agent invokes the **vercel skill** with `path: .tasks/dist/`, passing the manifest
4. Vercel skill returns a deployment URL
5. Agent runs `corvee journal <project-id> --note "Board deployed: <URL>" --event decision` to record the deploy in the project's journal
6. Agent reports the URL to the user

The corvee SKILL.md's "Deployment" section codifies steps 1, 2, and 5; steps 3-4 are the vercel skill's contract.

### 17.4 What Stays Out of corvee

These are deliberate non-features — they would couple corvee to a specific deployment provider:

- A `corvee deploy` command
- Vercel API tokens, project configs, or auth handling
- HTTP upload logic
- Webhook handlers for deploy notifications
- Custom domain configuration

If a user asks for `corvee deploy`, the SKILL.md instructs the agent to: render, then invoke the vercel skill. No exceptions.

### 17.5 Re-deploy Cadence

`corvee render` is cheap (read-only, ~ms for hundreds of items). The recommended pattern:
- On manual user request: render + redeploy
- Optionally: a shell hook on `on-status-change.sh` that triggers render + vercel skill invocation, batched (debounced) to avoid redeploying on every keystroke

The hook approach keeps corvee clean — the integration is a 5-line shell script in `.tasks/hooks/`, not core code.

---

## 18. Open Questions to Resolve During Build

- **Workspace discovery**: walk up from cwd to find `.tasks/` (git-style), or require an env var? Recommend walk-up.
- **Per-agent identity**: how is `agent-claude-1` minted and persisted? Recommend `CORVEE_AGENT_ID` env var, falling back to `.tasks/config.json` default.
- **Index regeneration**: on every write (eager) or on next read after dirty mark (lazy)? Recommend lazy — dirty flag in `index.json`, regen on read if stale.
- **Auto-commit cadence**: per-mutation, batched per session, or manual via `corvee sync`? Recommend manual for v1; per-mutation as opt-in flag in config.
- **HTML theme system**: hardcoded variants vs true plugin? Recommend start with `default.css` + `dark.css`, formalize a theme plugin contract only if needed.
- **Lock implementation**: per-item `.lock` files via `O_EXCL`, or a single workspace-level lock? Recommend per-item — finer granularity, less contention with multiple agents.
- **`render` cadence in CI**: should `corvee render` run in a git pre-commit hook? Recommend no — too noisy. Run on push or on-demand only.
- **Logical clock (deferred)**: a Lamport or vector clock would give true causal ordering across machines, removing reliance on wall-clock timestamps. v1 uses sortable ULIDs (lease_id) plus 5-min skew tolerance — sufficient for cooperative agents in the same time zone. Revisit if the system extends to weakly-coordinated, geographically distributed agents.
- **Roll-back vs roll-forward in `corvee recover`**: v1 designs every operation step to be roll-forwardable (idempotent or naturally resumable), which avoids the complexity of reverse operations. If a future operation kind genuinely cannot be roll-forwarded, the design adds an explicit `compensate` field per step in the operation journal.
