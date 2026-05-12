# Workflows

Concrete, multi-step recipes for the most common corvee
scenarios. Each is a numbered command list with explanations.

---

## Planner-executor distributed setup (canonical multi-VM topology)

The pattern this skill optimizes for: one planner VM creates work,
multiple executor VMs claim and complete it. All coordination
happens through git.

### Bootstrap (one-time)

1. Pick a remote git repo (GitHub, Gitea, or a bare repo over SSH).
2. Planner VM:

   ```sh
   git clone <remote> work
   cd work
   corvee init --project DEMO --agent-id planner-1
   git add .tasks && git commit -m "init" && git push -u origin main
   ```

3. Each executor VM:

   ```sh
   git clone <remote> work
   cd work
   # Local-only identity; gitignored, never synced.
   corvee init --agent-id executor-$(hostname)
   ```

   `corvee init` on an existing workspace preserves the committed
   `workspace.json` and writes a fresh `local.json`.

### Planner: define a sprint

```sh
# Author the plan as JSON (or hand it to an LLM).
corvee import sprint-1.json
corvee sync                    # publish — executors can now see it
```

Or interactively:

```sh
corvee add story --parent DEMO-E01 --title "Wire auth handler" \
               --priority high --depends-on DEMO-E01-S03
corvee sync
```

### Executor: pick up work

```sh
corvee sync                                    # pull latest
corvee next --type story --auto-claim --match-capabilities
# → returns {item, lease_id}
```

### Executor: heartbeat & journal during work

```sh
corvee heartbeat <id> --lease-id <LEASE>       # every 30 min
corvee journal <id> --note "wrote handler" \
              --event progress \
              --metadata '{"step":"impl","files":["auth.go"]}' \
              --lease-id <LEASE>
```

### Executor: complete

```sh
corvee done <id> --lease-id <LEASE> --note "ships clean" \
          --metadata '{"changed_files":["auth.go","auth_test.go"],"tests_passed":12}'
corvee sync
```

### Planner: observe

```sh
corvee summary                          # roll-up
corvee list --status blocked            # surfaces stuck work
corvee watch --filter event=blocked &   # live stream
```

### When two executors race for the same item

The atomic claim + lease_id prevents same-machine races. Across VMs,
two executors may both claim the same item before sync. On the next
`corvee sync`, the merge picks the lower `lease_id` (ULIDs are
sortable; earlier-issued lease wins). The loser sees exit 5 with
`details.claim_lost: true` on their next operation against the item.
Action: drop the work and call `corvee next` to pick a different item.

---

## Plan a sprint from a PRD

1. Hand the PRD to an LLM with the prompt: "Decompose this PRD into
   a planning JSON for corvee, native format. Include
   acceptance criteria, dependencies, and capability tags."
2. Save the output as `plan.json`.
3. Validate without writing: `corvee import plan.json --dry-run`.
4. Apply: `corvee import plan.json`.
5. Set the critical path: edit `.tasks/workspace.json` `critical_path`
   array (or pass `critical_path` in the plan doc).
6. Publish: `corvee sync`.

---

## Break down an epic

```sh
# Read the epic and its existing children.
corvee tree DEMO-E01

# Add stories.
corvee add story --parent DEMO-E01 --title "Schema migration" \
               --estimated-loc 80 --risk medium
corvee add story --parent DEMO-E01 --title "Migration tests" \
               --estimated-loc 50 --depends-on DEMO-E01-S04

# Promote them to ready when planning is complete.
corvee update DEMO-E01-S04 --status ready
corvee update DEMO-E01-S05 --status ready
corvee sync
```

---

## Hand off mid-task

Sometimes an executor needs to release ownership before completing
(blocked on review, end of session, machine going offline). The
journal handoff metadata is the contract:

```sh
corvee journal <id> --note "Stopping at step 3 of 5; auth.go done." \
              --event handoff \
              --metadata '{
                "next_step":"add tests for refresh path",
                "files_touched":["auth.go"],
                "decisions":["use HMAC SHA256"]
              }' \
              --lease-id <LEASE>
corvee release <id> --lease-id <LEASE>
corvee sync
```

Any executor that picks up the item via `corvee next` will see the
handoff entry in the journal and can pick up where the previous one
left off.

---

## Recover from a crashed agent

If a `corvee <verb>` invocation crashed mid-multi-file-operation
(rename, move, delete --cascade, clone --with-children), the next
`corvee list` or `corvee next` auto-detects the orphaned operation file
and rolls forward. To run recovery manually:

```sh
corvee recover
```

If a step is genuinely unsafe to roll forward, the operation is
marked `failed`. Inspect with `corvee validate` and resolve manually.

---

## Onboard a new executor (clone a workspace and join the pool)

```sh
git clone <remote> work
cd work
corvee init --agent-id executor-bob       # writes local.json only
corvee list --limit 1                     # smoke test
export CORVEE_AGENT_CAPABILITIES=go,sql   # optional
corvee next --auto-claim --match-capabilities
```
