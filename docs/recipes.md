# Recipes

End-to-end setups for common scenarios. Each recipe is a numbered
shell session — copy-paste into a terminal and it works.

| Recipe | What it gives you |
|---|---|
| [Solo developer](#1-solo-developer) | One-person backlog, no remote |
| [Solo + GitHub backup](#2-solo--github-backup) | Same plus daily git push |
| [Planner-executor (multi-VM)](#3-planner--executors-canonical-multi-vm) | One planner VM, N executor VMs |
| [Peer executors](#4-peer-executors) | A small team that self-organizes |
| [Reviewer-in-the-loop](#5-reviewer-in-the-loop) | Mandatory review gate |
| [Claude Code skill](#6-claude-code--cursor-skill) | Drop-in agent skill |
| [GitHub Actions integration](#7-github-actions-integration) | Tasks tracked from PR titles |
| [Plugin: auto-tagger](#8-plugin-auto-tagger-python) | A working sample plugin |
| [Hook: deploy on done](#9-hook-deploy-on-status-change) | Render + deploy on corvee done |
| [Migration from Markdown TODOs](#10-migration-from-markdown-todos) | Pipe a Markdown file in |

---

## 1. Solo developer

You want a structured backlog for your own work, no team.

```sh
cd ~/projects/blog
corvee init --project BLOG --agent-id me

# Plan
corvee add project --project BLOG --title "Blog migration"
corvee add epic --parent BLOG --title "Move from WordPress to Astro"
corvee add story --parent BLOG-E01 --title "Export old posts" \
  --priority high --estimated-loc 50
corvee add story --parent BLOG-E01 --title "Astro layouts" \
  --priority medium --depends-on BLOG-E01-S01
corvee update BLOG-E01-S01 --status ready
corvee update BLOG-E01-S02 --status ready

# Loop
corvee next --type story --auto-claim
# … work …
corvee done BLOG-E01-S01 --lease-id $LEASE --note "ships"
```

No `corvee sync` needed — everything stays local. Periodic
`git commit -am "task: snapshot"` if you want history.

---

## 2. Solo + GitHub backup

Same as recipe 1, but daily git push for backup.

```sh
cd ~/projects/blog
git init && git remote add origin git@github.com:you/blog.git
corvee init --project BLOG --agent-id me
git add .tasks && git commit -m "init tracker" && git push -u origin main

# Configure auto-commit
jq '.git.auto_commit = true' .tasks/workspace.json > .tasks/workspace.json.tmp \
  && mv .tasks/workspace.json.tmp .tasks/workspace.json

# Now `corvee sync` stages, commits, and pushes in one go
corvee sync --auto-commit --message "task: snapshot"
```

Add this to your shell profile or a cron job for hands-off backup.

---

## 3. Planner → executors (canonical multi-VM)

The pattern this tool optimizes for. One agent (or human) plans;
many agents execute; all coordination via git.

### Setup

```sh
# On the git host: create a bare repo (or use an existing one).
ssh git-host
mkdir -p ~/repos/work.git && cd ~/repos/work.git
git init --bare
exit
```

### Planner VM

```sh
git clone git@git-host:~/repos/work.git
cd work
corvee init --project DEMO --agent-id planner-1

# Bootstrap a project + plan
corvee add project --project DEMO --title "Demo project"

# Or import a pre-built plan from an LLM:
cat <<'EOF' > plan.json
{
  "schema_version": 1,
  "project_id": "DEMO",
  "project_name": "Demo project",
  "phases": [
    { "id": "DEMO-E01", "name": "Phase 1",
      "tasks": [
        { "id": "DEMO-E01-S01", "title": "Schema design", "type": "story",
          "status": "ready", "priority": "high",
          "acceptanceCriteria": ["ER diagram in repo"] },
        { "id": "DEMO-E01-S02", "title": "Migration script", "type": "story",
          "status": "ready", "dependencies": ["DEMO-E01-S01"] }
      ]
    }
  ]
}
EOF
corvee import plan.json

# Publish
corvee sync --auto-commit --message "seed plan"
```

### Each executor VM

```sh
git clone git@git-host:~/repos/work.git
cd work
corvee init --agent-id executor-$(hostname)
echo 'CORVEE_AGENT_CAPABILITIES=go,sql' >> ~/.profile && source ~/.profile

# Loop forever
while true; do
  corvee sync                                        # pull peer changes
  out=$(corvee next --type story --auto-claim --match-capabilities)
  id=$(echo "$out" | jq -r '.data.item.id')
  lease=$(echo "$out" | jq -r '.data.lease_id')
  if [ -z "$id" ] || [ "$id" = "null" ]; then
    sleep 60
    continue
  fi

  # Move to in_progress
  corvee update "$id" --status in_progress

  # … do the actual work here …

  corvee done "$id" --lease-id "$lease" --note "shipped"
  corvee sync --auto-commit --message "complete $id"
done
```

### Planner observes

```sh
corvee sync                          # pull executor progress
corvee summary --pretty              # roll-up
corvee list --status blocked         # surface stuck work
corvee watch --filter event=blocker  # react in real time
```

### What if two executors race?

Both can claim before either pushes. On the next `corvee sync`, the
3-way merge picks the lower lease ID (ULIDs are sortable). The
loser sees `details.claim_lost: true` on its next operation against
the item and reroutes via `corvee next`.

---

## 4. Peer executors

Same as recipe 3, but no dedicated planner — every agent has both
roles.

```sh
# Each agent
corvee init --agent-id $(whoami)
echo 'CORVEE_AGENT_ROLE=executor' >> ~/.profile

# Whoever sees a need creates work
corvee add story --parent DEMO-E01 --title "Bug found in login"
corvee sync --auto-commit --message "report login bug"

# Whoever picks it up does it
corvee next --type story --auto-claim
```

The trade-off: without a planner, the `critical_path` config drifts.
Designate one agent (or a recurring cron job) to keep it current:

```sh
# In a cron job
git pull --rebase
jq '.critical_path = ["DEMO-E01-S03","DEMO-E01-S07","DEMO-E02-S01"]' \
  .tasks/workspace.json > .tasks/workspace.json.tmp \
  && mv .tasks/workspace.json.tmp .tasks/workspace.json
git commit -am "update critical path" && git push
```

---

## 5. Reviewer-in-the-loop

Mandatory review gate. Executors mark `review` instead of `done`; a
reviewer agent picks up `review`-status items and either flips to
`done` or back to `in_progress` with feedback.

### Executor

```sh
# Same as recipe 3 except the closing step:
corvee update "$id" --status review
corvee journal "$id" --note "ready for review" --event handoff \
  --metadata '{"changed_files":["auth.go"]}' --lease-id "$lease"
corvee release "$id" --lease-id "$lease"
```

### Reviewer

```sh
corvee init --agent-id reviewer-1
echo 'CORVEE_AGENT_ROLE=reviewer' >> ~/.profile

while true; do
  corvee sync
  out=$(corvee next --status review --auto-claim)
  id=$(echo "$out" | jq -r '.data.item.id')
  lease=$(echo "$out" | jq -r '.data.lease_id')
  [ -z "$id" ] && sleep 60 && continue

  # Inspect the item
  corvee show "$id" --pretty

  # If approved
  corvee done "$id" --lease-id "$lease" --note "LGTM"

  # If changes needed
  corvee journal "$id" --note "Needs error handling for nil input" \
    --event decision --lease-id "$lease"
  corvee update "$id" --status in_progress
  corvee release "$id" --lease-id "$lease"

  corvee sync --auto-commit
done
```

---

## 6. Claude Code / Cursor skills

Two skills ship with the repo:

| Skill | Triggers on (examples) |
|---|---|
| `corvee` | "what should I work on?", "mark this done", "what's blocked?" — the day-to-day agent loop. |
| `corvee-install` | "install corvee", "set up corvee", "upgrade corvee", "add corvee to CLAUDE.md" — install, verify, init, version-check, in-place upgrade, and idempotent `CLAUDE.md` / `AGENTS.md` bootstrap. |

```sh
# Step 1 — install the `corvee` binary (one of):
go install github.com/mandarnilange/corvee/cmd/corvee@latest
# or download a pre-built release:
#   curl -LO https://github.com/mandarnilange/corvee/releases/download/v1.0.0/corvee-linux-amd64
# (or invoke the `corvee-install` skill once installed; it walks the
# agent through every option.)

# Step 2 — install both skills via skills.sh (auto-detects your
# harness — Claude Code, Cursor, Windsurf, …):
npx skills add mandarnilange/corvee
```

Now the agent reads `SKILL.md` on every matching invocation and
shells out to the `corvee` binary you installed in step 1. Triggers
automatically — no extra config.

For local development on the skills themselves, symlink instead so
edits to `skills/*` are visible to the harness immediately. The
target depends on your harness — common defaults:

| Harness | Skills dir |
|---|---|
| Claude Code | `~/.claude/skills/` |
| Cursor | `~/.cursor/skills/` |
| Windsurf | `~/.codeium/windsurf/skills/` |

Substitute your harness's path for `$SKILLS_DIR` below:

```sh
SKILLS_DIR=~/.claude/skills          # change for your harness
mkdir -p "$SKILLS_DIR"
ln -s "$(pwd)/skills/corvee"         "$SKILLS_DIR/corvee"
ln -s "$(pwd)/skills/corvee-install" "$SKILLS_DIR/corvee-install"
```

> Offline / air-gapped distribution still has `make skill-tarball`
> (produces `dist/corvee-skill.tar.gz` bundling both folders) — use
> it when `npx` can't reach the registry, then extract directly into
> the harness's skills dir.

---

## 7. GitHub Actions integration

Mirror PR titles into the tracker, automatically.

```yaml
# .github/workflows/track-prs.yml
name: Track PRs in tracker
on:
  pull_request:
    types: [opened, closed]

permissions:
  contents: write

jobs:
  track:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Install task
        run: |
          curl -L https://github.com/.../releases/latest/download/corvee-linux-amd64 \
            -o /usr/local/bin/corvee && chmod +x /usr/local/bin/corvee
      - name: Configure git
        run: |
          git config user.email "ci@example.com"
          git config user.name  "ci"
      - name: Track new PR
        if: github.event.action == 'opened'
        env:
          PR_TITLE: ${{ github.event.pull_request.title }}
          PR_NUMBER: ${{ github.event.pull_request.number }}
        run: |
          corvee add story --parent DEMO-E01 --title "$PR_TITLE" --tags "pr-$PR_NUMBER"
          corvee sync --auto-commit --message "track PR #$PR_NUMBER"
      - name: Mark PR done
        if: github.event.action == 'closed' && github.event.pull_request.merged
        env:
          PR_NUMBER: ${{ github.event.pull_request.number }}
        run: |
          item=$(corvee list --tag "pr-$PR_NUMBER" | jq -r '.data.items[0].id')
          [ -n "$item" ] && corvee done "$item" --note "Merged via PR #$PR_NUMBER"
          corvee sync --auto-commit --message "close PR #$PR_NUMBER"
```

---

## 8. Plugin: auto-tagger (Python)

A plugin that adds tags based on changed files.

```sh
mkdir -p .tasks/plugins/auto-tagger
cat <<'EOF' > .tasks/plugins/auto-tagger/plugin.json
{
  "name": "auto-tagger",
  "command": "run.py",
  "events": ["completed"],
  "agent_safe": true
}
EOF
cat <<'EOF' > .tasks/plugins/auto-tagger/run.py
#!/usr/bin/env python3
import json
import sys
import subprocess

ev = json.load(sys.stdin)
changed = ev.get("metadata", {}).get("changed_files") or []
tags = set()
for f in changed:
    if f.endswith("_test.go"): tags.add("tests")
    elif f.startswith("internal/domain/"): tags.add("domain")
    elif f.startswith("internal/usecase/"): tags.add("usecase")

for tag in tags:
    subprocess.run(["task", "update", ev["item_id"], "--add-tag", tag], check=False)
print(json.dumps({}))
EOF
chmod +x .tasks/plugins/auto-tagger/run.py
git add .tasks/plugins && git commit -m "add auto-tagger plugin"
```

Now every `corvee done <id> --metadata '{"changed_files":[...]}` fires
the plugin and tags the item.

See [docs/plugin-protocol.md](plugin-protocol.md) for the full
contract.

---

## 9. Hook: deploy on status change

A shell hook that re-renders the dashboard and fires the deploy
skill whenever a status changes.

```sh
mkdir -p .tasks/hooks
cat <<'EOF' > .tasks/hooks/all.sh
#!/bin/sh
# Triggered on every event. Read the event from stdin.
event=$(cat)
type=$(echo "$event" | jq -r '.event')
case "$type" in
  status_changed|completed)
    corvee render --out .tasks/dist/ > /dev/null
    # If you have a vercel skill or similar, invoke it here:
    # vercel deploy --path .tasks/dist/ --prebuilt
    ;;
esac
EOF
chmod +x .tasks/hooks/all.sh
git add .tasks/hooks && git commit -m "add deploy hook"
```

Errors from the hook are logged to stderr but never fail the core
operation.

---

## 10. Migration from Markdown TODOs

A small script to bulk-import a Markdown TODO list.

```sh
cat <<'EOF' > /tmp/todos.md
- [x] Wire up auth
- [ ] Add password reset
- [ ] Email verification
EOF

cat <<'EOF' > /tmp/convert.py
#!/usr/bin/env python3
import re, json, sys
plan = {"schema_version": 1, "project_id": "TODO", "project_name": "TODOs",
        "phases": [{"id": "TODO-E01", "name": "TODOs", "tasks": []}]}
for i, line in enumerate(sys.stdin, 1):
    m = re.match(r'-\s+\[(.)\]\s+(.+)', line.strip())
    if not m: continue
    done, title = m.groups()
    plan["phases"][0]["tasks"].append({
        "id": f"TODO-E01-S{i:02d}",
        "title": title,
        "type": "story",
        "status": "done" if done == "x" else "ready",
    })
print(json.dumps(plan, indent=2))
EOF
chmod +x /tmp/convert.py

cd /path/to/project
corvee init --project TODO
python3 /tmp/convert.py < /tmp/todos.md | corvee import -
```

---

## More

- [docs/getting-started.md](getting-started.md) — first-run walkthrough
- [docs/cli-reference.md](cli-reference.md) — every verb's flags
- [docs/architecture.md](architecture.md) — how the layers fit together
- [references/topologies.md](../references/topologies.md) — when to use which pattern
- [references/handoff-patterns.md](../references/handoff-patterns.md) — `--metadata` conventions
