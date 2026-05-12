# Plugin protocol

Plugins extend `corvee` without touching core code. They are external
binaries (any language) installed under `<workspace>/.tasks/plugins/<name>/`
or `<repo>/plugins/<name>/`. The CLI invokes them via `os/exec`,
sends a JSON event on stdin, and reads an optional JSON response on
stdout.

## Layout

```text
plugins/
  <plugin-name>/
    plugin.json            # manifest (required)
    <command>              # executable referenced by manifest.command
    ... any other files ...
```

## `plugin.json` schema

```json
{
  "name": "auto-tagger",
  "description": "Adds tags based on file path patterns",
  "command": "run.py",
  "events": ["status_changed", "completed"],
  "agent_safe": true
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `name` | string | ✅ | Human-readable name. |
| `description` | string |  | One-liner; surfaced in plugin lists. |
| `command` | string | ✅ | Executable name (resolved relative to plugin dir). |
| `events` | string[] |  | Events to subscribe to. Empty = all. |
| `agent_safe` | bool |  | Defaults to `false`. See agent mode below. |

Malformed manifests are logged at `WARN` and skipped. They never
break the CLI.

## Event payload

The CLI writes one JSON object on the plugin's stdin per event:

```json
{
  "event": "completed",
  "item_id": "DEMO-E01-S03",
  "actor": "executor-1",
  "from": "in_progress",
  "to": "done",
  "ts": "2026-05-06T10:00:00Z",
  "metadata": {
    "changed_files": ["a.go"],
    "tests_passed": 12
  }
}
```

The plugin should read stdin to EOF before responding.

## Plugin response (optional)

The plugin may write a single JSON object to stdout:

```json
{ "veto": false }
```

Or to refuse the operation:

```json
{ "veto": true, "reason": "code freeze in effect" }
```

When `veto: true`, the core operation fails with exit code 1. Use
sparingly — vetos hide failures behind plugin logic.

A plugin that writes nothing (or anything other than valid JSON) is
treated as "ok, no veto." Errors from the plugin (non-zero exit, IO
failure) are logged at `WARN` and do **not** fail the core operation
unless they explicitly veto.

## Agent mode (`CORVEE_AGENT_MODE=1`)

Some plugins are interactive (open a browser, prompt the user, page
slack on a long-running operation). These are dangerous in agent
mode where the calling LLM expects a deterministic JSON response on
stdout.

`CORVEE_AGENT_MODE=1` (or the global `--agent` flag) skips plugins that
do **not** declare `"agent_safe": true`. Set this whenever the CLI is
invoked from an LLM harness, a CI runner, or any non-interactive
context.

## Sample: Python auto-tagger

`plugins/auto-tagger/plugin.json`:

```json
{
  "name": "auto-tagger",
  "command": "run.py",
  "events": ["completed"],
  "agent_safe": true
}
```

`plugins/auto-tagger/run.py`:

```python
#!/usr/bin/env python3
"""Adds a tag based on the changed_files metadata."""
import json
import sys

ev = json.load(sys.stdin)
changed = ev.get("metadata", {}).get("changed_files") or []
tags = set()
for f in changed:
    if f.endswith("_test.go"):
        tags.add("tests")
    elif f.startswith("internal/domain/"):
        tags.add("domain")
    elif f.startswith("internal/usecase/"):
        tags.add("usecase")

# This plugin is informational; it doesn't veto. A real tagger would
# call `corvee update <id> --add-tag X` from here. Network/CLI calls
# are fine — keep them quick (<10s) so the parent doesn't time out.
print(json.dumps({}))
```

Make it executable: `chmod +x plugins/auto-tagger/run.py`.

## Discovery & ordering

`corvee` walks `<workspace>/.tasks/plugins/` (and a future repo-level
`<repo>/plugins/`) at every invocation. Plugins are dispatched in
filesystem order; if you need ordering guarantees, prefix plugin
folder names with two-digit numbers (`10-auto-tagger`,
`20-deploy-notifier`).

## Shell hooks (lightweight alternative)

For one-off automation, use `.tasks/hooks/<event>.sh` instead of a
full plugin. Same stdin payload, no manifest required. See
`references/workflows.md` for an example.
