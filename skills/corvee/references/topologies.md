# Topologies

Three patterns the system supports. Most teams use the first one.

## 1. Planner → Executors (canonical)

```text
                git remote
                    │
   ┌────────────────┼────────────────┐
   │                │                │
┌──▼─────┐    ┌─────▼──────┐   ┌─────▼──────┐
│Planner │    │Executor A  │   │Executor B  │
│(VM-0)  │    │(VM-1)      │   │(VM-2)      │
│role=plan│   │role=exec   │   │role=exec   │
└────────┘    │caps=[py,ml]│   │caps=[fe,ts]│
              └────────────┘   └────────────┘
```

When to use:

- One agent (or human) decomposes work; many agents execute it.
- Most multi-agent AI workflows.

Sync cadence:

- Planner: sync immediately after each batch creation.
- Executors: sync every 30s–5min, and after every `done`/`update`.
- All: opportunistic sync every ~60s on idle.

Failure modes:

- Two executors claim the same item → merge picks lower lease;
  loser sees `claim_lost`.
- Executor crashes mid-task → claim TTL expires (default 60 min),
  reaper releases it, another executor picks it up.
- Planner can't reach remote → can still create items locally;
  syncs when network returns.

## 2. Peer executors (no planner)

Same as topology 1 but everyone runs `role=executor`. Useful when:

- A small team self-organizes.
- Each agent both creates and completes work.
- No central planning agent is required.

Caveat: without a planner, the `critical_path` config tends to drift.
Designate one agent (or a recurring CI job) to keep it current.

## 3. Reviewer-in-the-loop

```text
       git remote
           │
  ┌────────┼────────┬────────┐
  │        │        │        │
┌─▼─┐   ┌──▼─┐   ┌──▼──┐   ┌─▼──┐
│Pln│   │ExA │   │ExB  │   │Rev │
└───┘   └────┘   └─────┘   └────┘
                              ↑
                  reviewer flips items
                  in status=review back
                  to in_progress (with
                  feedback) or to done.
```

When to use:

- Production codebases with mandatory review.
- Compliance-sensitive workflows.

Implementation: executors mark items `review` instead of `done` on
completion (`corvee update <id> --status review`). Reviewer agent
picks them up via `corvee list --status review`, validates against
`acceptance_criteria`, and either flips to `done` or back to
`in_progress` with a journal note explaining what to fix.

## Choosing a topology

| Need | Topology |
|---|---|
| One AI plans, many execute | Planner → Executors |
| Agents are interchangeable | Peer executors |
| Mandatory human or AI review gate | Reviewer-in-the-loop |
| Mixed AI + human collaboration | Any of the above; humans use `role=human` |

All three respect the same `lease_id` semantics, so an executor in
one topology can move to another without changing the protocol.
