---
name: corvee-install
description: Install, verify, upgrade, and bootstrap the `corvee` CLI in a project. Use this skill whenever the user says "install corvee", "set up corvee", "add corvee to this project", "bootstrap corvee", "upgrade corvee", "update corvee", "check corvee version", or asks how to get the binary on PATH. Also use it when an agent lands in a repo with `.tasks/` references but no working `corvee` on PATH, or when `corvee version` is behind the latest release, or when the user wants the standard "Using corvee in this repo" block appended to CLAUDE.md / AGENTS.md so future agents pick the tool up automatically. Companion to the `corvee` skill (which assumes the binary is already installed and teaches the claim/heartbeat/done loop).
---

# corvee-install

This skill walks an AI agent (or a human) through three lifecycle
operations for the `corvee` CLI:

1. **Install** — get the binary on `PATH` on a fresh machine.
2. **Verify + bootstrap** — confirm `corvee version` works and create
   a `.tasks/` workspace via `corvee init`.
3. **Maintain** — version-check against the latest release, upgrade in
   place preserving the original install method, and append a
   "Using corvee in this repo" block to `CLAUDE.md` / `AGENTS.md` so
   future agents pick the tool up without being told.

Once the binary is installed, hand off to the `corvee` skill for the
day-to-day claim/heartbeat/done loop.

## When to trigger this skill

| User says / context | Action |
|---|---|
| "install corvee" / "set up corvee" / "add corvee to this project" | Run **Step 1 — Install**, then **Step 2 — Verify**, then **Step 3 — Init**. |
| repo has `.tasks/` but `command -v corvee` is empty | Run **Step 1 — Install**, skip Step 3 (workspace already exists). |
| "upgrade corvee" / "update corvee to latest" | Run **Step 4 — Version check** then **Step 5 — Upgrade**. |
| "make sure agents in this repo know about corvee" / "add corvee to CLAUDE.md" | Run **Step 6 — Bootstrap agent docs**. |
| user wants the full onboarding | Run all six steps in order. |

Never write to the user's filesystem (install a binary, run `corvee
init`, edit `CLAUDE.md`) without confirming with the user first. The
skill is opinionated about steps; consent is non-negotiable.

## Constants used below

The variables below appear in every snippet. Substitute the right
values once at the top of the conversation:

```text
REPO          = mandarnilange/corvee
GO_MODULE     = github.com/mandarnilange/corvee
BINARY        = corvee
MIN_GO        = 1.26
```

---

## Step 1 — Install

### 1a. Detect OS and architecture

```sh
OS=$(uname -s | tr '[:upper:]' '[:lower:]')   # linux | darwin
ARCH_RAW=$(uname -m)                          # x86_64 | aarch64 | arm64
case "$ARCH_RAW" in
  x86_64|amd64)   ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  *) echo "unsupported arch: $ARCH_RAW" >&2; exit 1 ;;
esac
PLATFORM="${OS}-${ARCH}"
echo "platform: $PLATFORM"
```

Supported targets (per `make build-all`):
`linux-amd64`, `linux-arm64`, `darwin-arm64`. Anything else aborts —
do not silently fall back. Windows is explicitly out of scope.

### 1b. Pick an install method

Try them in order. Stop at the first one that works.

#### Method A — `go install` (preferred when Go ≥ 1.26 is present)

```sh
if command -v go >/dev/null && go version | awk '{print $3}' | sed 's/^go//' | \
    awk -F. -v min=1.26 '{if ($1"."$2 >= min) exit 0; exit 1}'; then
  go install github.com/mandarnilange/corvee/cmd/corvee@latest
  INSTALLED_VIA=go
  INSTALL_DIR=$(go env GOBIN); [ -z "$INSTALL_DIR" ] && INSTALL_DIR="$(go env GOPATH)/bin"
fi
```

Record `INSTALLED_VIA=go` and `INSTALL_DIR` — Step 5 (upgrade) reads
both to choose the same path next time.

#### Method B — pre-built release binary

```sh
VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
          awk -F\" '/"tag_name":/ {print $4; exit}')
ASSET="corvee-${PLATFORM}"
SHA_ASSET="${ASSET}.sha256"
BASE="https://github.com/${REPO}/releases/download/${VERSION}"

curl -fsSLO "${BASE}/${ASSET}"
curl -fsSLO "${BASE}/${SHA_ASSET}"
shasum -a 256 -c "${SHA_ASSET}"      # MUST exit 0
chmod +x "${ASSET}"

# Choose an install location the user controls.
if [ -w /usr/local/bin ] || sudo -n true 2>/dev/null; then
  sudo install "${ASSET}" /usr/local/bin/corvee
  INSTALL_DIR=/usr/local/bin
else
  mkdir -p "$HOME/.local/bin"
  install "${ASSET}" "$HOME/.local/bin/corvee"
  INSTALL_DIR="$HOME/.local/bin"
  # Remind the user to add ~/.local/bin to PATH if it's not there.
fi
INSTALLED_VIA=binary
```

Always verify the SHA-256 before chmod/install. A failed checksum
means an interrupted download or a tampered asset — re-download, do
not "just chmod and hope".

#### Method C — build from source

Use this when both A and B fail (corporate proxy blocks
`api.github.com`, air-gapped host, Go missing on a darwin box, etc.):

```sh
git clone "https://github.com/${REPO}.git"
cd corvee
make build
sudo install bin/corvee /usr/local/bin/corvee
INSTALLED_VIA=source
INSTALL_DIR=/usr/local/bin
SOURCE_DIR=$(pwd)        # remember for upgrades
```

### 1c. Persist the install method

Stash a single-line record so Step 5 (upgrade) can be deterministic:

```sh
mkdir -p "$HOME/.config/corvee"
printf 'installed_via=%s\ninstall_dir=%s\nsource_dir=%s\n' \
  "$INSTALLED_VIA" "$INSTALL_DIR" "${SOURCE_DIR:-}" \
  > "$HOME/.config/corvee/install.env"
```

This file is **per-user, not per-project**, and never committed.

---

## Step 2 — Verify

```sh
hash -r 2>/dev/null || true     # refresh shell's command cache
command -v corvee >/dev/null || { echo "corvee not on PATH" >&2; exit 1; }
corvee version --pretty
```

The expected output is a JSON envelope with `ok: true` and a
`data.version` string. If the command exits non-zero or `ok` is
`false`, stop — do **not** proceed to Step 3.

Common failure modes:

- **`corvee: command not found`** — the install dir is not on `$PATH`.
  Print: `add "$INSTALL_DIR" to PATH and re-open the shell`.
- **`exec format error`** — wrong arch downloaded. Re-run Step 1a, fix
  `PLATFORM`, retry Method B.
- **Mach-O / dylib gatekeeper prompt on macOS** —
  `xattr -d com.apple.quarantine /usr/local/bin/corvee`.

---

## Step 3 — Bootstrap `.tasks/` (skip if it already exists)

```sh
corvee list --limit 1 >/dev/null 2>&1
case $? in
  0) echo "workspace already initialized" ;;
  3) NEED_INIT=1 ;;
  *) echo "unexpected exit $?" >&2; exit 1 ;;
esac
```

If `NEED_INIT=1`, ask the user for a **project code** — 2–4 uppercase
letters, no digits, e.g. `TTR`, `APP`, `WEB`. Then run:

```sh
corvee init --project "${PROJECT_CODE}" --agent-id "${AGENT_NAME}"
```

`AGENT_NAME` is the human or LLM identity that will appear in
`created_by` fields. Common values: `mandar`, `claude`, `gpt-codex`.

After init, point the user at the day-to-day skill:

> Workspace ready. From here on, the `corvee` skill (next to this
> one) covers the agent loop:
>
> ```text
> corvee sync → corvee next --type story --auto-claim → heartbeat → done → sync
> ```

---

## Step 4 — Version check

Run this whenever the user asks "is my corvee up to date?" or before
starting a long session:

```sh
LOCAL=$(corvee version 2>/dev/null | awk -F\" '/"version":/ {print $4; exit}')
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | \
         awk -F\" '/"tag_name":/ {print $4; exit}')
echo "installed: ${LOCAL}  latest: ${LATEST}"
[ "$LOCAL" = "$LATEST" ] && echo "up to date" || echo "upgrade available"
```

If `LATEST` is empty (rate-limited, offline, repo still private to
the agent's network identity), report that to the user — do not
silently report "up to date".

The skill never auto-upgrades on a version-check. Verification is
read-only; mutation requires Step 5.

---

## Step 5 — Upgrade in place (preserves install method)

Confirm with the user first unless `--yes` was passed by the host:

```text
corvee X.Y.Z installed, A.B.C available — upgrade? [y/N]
```

Read `$HOME/.config/corvee/install.env` to recover `installed_via`,
`install_dir`, and `source_dir`. If the file is missing, fall back to
sniffing `command -v corvee` against `go env GOPATH/bin`, `/usr/local/bin`,
and `~/.local/bin` to guess.

Then dispatch:

| installed_via | Upgrade command |
|---|---|
| `go` | `go install github.com/mandarnilange/corvee/cmd/corvee@latest` |
| `binary` | Re-run **Method B** from Step 1 but write into the recorded `install_dir`. Refuse if `install_dir` is empty. |
| `source` | `cd "$SOURCE_DIR" && git pull --ff-only && make build && sudo install bin/corvee "$INSTALL_DIR/corvee"` |

After the upgrade, re-run **Step 2 — Verify** and confirm the new
version matches `LATEST`. If it doesn't, stop and surface the gap —
the user might be running multiple `corvee` binaries on different
`PATH` entries.

Never clobber a binary at a different path than the one recorded.
"Upgrade" means same install location, newer bits.

### `--yes` (unattended)

When the host harness signals unattended (`CORVEE_INSTALL_YES=1` env,
or the skill is invoked with the literal token `--yes`), skip the
prompt. Everything else stays the same. Treat a SHA-256 mismatch or a
non-fast-forward `git pull` in source mode as fatal — never force
through them.

---

## Step 6 — Bootstrap agent docs (`CLAUDE.md` / `AGENTS.md`)

Many projects keep a top-level `CLAUDE.md` or `AGENTS.md` that AI
agents read on session start. Append (or create) a short, opinionated
"Using corvee in this repo" block so the next agent finds the tool
without being told.

### Detection order

1. `CLAUDE.md` at repo root → append there.
2. Else `AGENTS.md` at repo root → append there.
3. Else **ask the user** which one to create. Never write a new file
   without consent.

### Idempotent block

Use fenced HTML markers so re-running this step doesn't duplicate.
The marker text is fixed; do not paraphrase it.

```md
<!-- BEGIN corvee:agent-block (managed by corvee-install skill) -->
## Using corvee in this repo

This project tracks work in `.tasks/` via the `corvee` CLI
(a local-first, file-based task tracker). The full agent contract
lives in the `corvee` skill (`skills/corvee/SKILL.md`); the summary
below is enough to get unstuck.

**Canonical agent loop**:

```text
corvee sync
corvee next --type story --auto-claim     # → returns {item, lease_id}
corvee heartbeat <id> --lease-id <LEASE>  # every 30 min
corvee done <id> --lease-id <LEASE> --note "…"
corvee sync
```

**Output contract**: every command emits a JSON envelope on stdout.
Branch on the **exit code**, not on string parsing of stderr:

| Exit | Meaning |
|---|---|
| 0 | OK |
| 2 | Usage error |
| 3 | Not found (item or workspace) |
| 4 | Optimistic-concurrency version mismatch |
| 5 | Claim conflict — call `corvee next` for a different item, do **not** retry |
| 6 | Schema-invalid input |
| 7 | Integrity violation (dependency cycle, orphan, guard) |
| 8 | Per-item lock timeout — short retry |
| 9 | Crashed multi-file op — run `corvee recover` |

**House rules when you write code in this repo**:

- TDD is mandatory (CLAUDE.md §3 / spec §11). Failing test → minimum
  implementation → green → refactor → commit.
- No new third-party dependencies without a spec-style justification
  (CLAUDE.md §9, enforced by `golangci-lint depguard`).
- `usecase/` line-coverage floor is ≥90% (SQ-007). The pre-push hook
  runs `make ci`, which enforces it.

**Pointers**: `skills/corvee/SKILL.md` (agent loop, error codes,
multi-agent coordination), `docs/cli-reference.md` (every verb's
flags and output), `docs/recipes.md` (multi-VM topologies).
<!-- END corvee:agent-block -->
```

### Append safely

```sh
TARGET=         # CLAUDE.md or AGENTS.md, chosen above
MARKER_BEGIN="<!-- BEGIN corvee:agent-block"
if [ -f "$TARGET" ] && grep -qF "$MARKER_BEGIN" "$TARGET"; then
  echo "corvee block already present in $TARGET — skipping"
else
  # Use a HEREDOC sourced from this skill, exact text above.
  printf '\n%s\n' "$BLOCK_TEXT" >> "$TARGET"
  echo "appended corvee block to $TARGET"
fi
```

The marker scan must use `grep -F` (literal) so the comparison is not
fooled by trailing whitespace in the surrounding file.

### Updating a stale block

If the agent loop or exit-code table changes upstream, the **correct
fix is to replace the entire block** — delete everything between
`BEGIN corvee:agent-block` and `END corvee:agent-block` inclusive,
then re-append. Never edit the block in place; that's the path back
to drift.

---

## End-to-end smoke (use this to self-verify after install/upgrade)

```sh
set -e
corvee version --pretty                                  # exit 0
[ -d .tasks ] || corvee init --project SMK --agent-id "$USER"
corvee list --limit 1 >/dev/null                         # exit 0
corvee summary --pretty | jq -e '.ok == true' >/dev/null # JSON envelope sane
echo "smoke OK"
```

If every line succeeds, the install is good and the agent can start
the loop documented in `skills/corvee/SKILL.md`.

---

## What this skill is NOT

- **Not a package manager.** No tap, no formula, no apt source.
  Method A / B / C are the three supported paths.
- **Not a workspace migrator.** If the user already has `.tasks/`
  from an older corvee version, this skill verifies the new binary
  reads it — it does not rewrite on-disk schema.
- **Not the day-to-day skill.** Once installed, the `corvee` skill
  is what the agent uses every session. This one is for setup,
  upgrade, and getting CLAUDE.md/AGENTS.md to point at the tool.
