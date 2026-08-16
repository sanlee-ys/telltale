# telltale — design doc

Status: v1 is cut — **v0.2.0, released 2026-08-14**, the snapshot gates held (§1). The honest-gauge
rule requires every segment's data source to be named here before that segment ships; the
tables below are the authority the eval harness tests against.

## 1. Product shape

**`telltale council` is the product. The gauges — `telltale statusline` and `telltale
hud` — are the infrastructure under it.** That ranking had been stated out loud and
recorded nowhere, which meant it bound nothing and every argument about what to build
next started over from memory. It is written here so it stops depending on who was in the
room. It does not demote the gauges: they are where each vendor's on-disk seam was
surveyed and written down (§3), they are what the honest-gauge rule was built and tested
against (§5), and council inherits both — it renders through the same `internal/model`
vocabulary and `internal/theme` palette the two gauge paths share. They are finished, they
are load-bearing, and they are not the thing this is for.

**v1 is a snapshot, not a freeze — it cuts when three gates hold, not when the room goes
quiet (re-cut 2026-08-08, owner's ruling).** The original hold said "until council
settles," and the first attempt to operationalize that — five consecutive days with no
merged PR touching council's visible surface — was falsified within a day: this project is
driven daily by its owner, so a quietness clock measures abandonment, not stability. What
the hold was actually protecting is narrower and checkable: a stranger who reads the
launch post and installs must find the room the post described. So v1 cuts when, on the
day of the tag:

1. **Nothing on the surface is half-finished or landed-but-never-driven** — every
   recently re-founded piece (a seat's protocol, a new command) has been used by the
   owner for real work for a few days;
2. **The README is verified against tip** — every claim, keybinding and badge checked
   against the code, with the frame-freshness tests holding the renders;
3. **No breaking change to the routing grammar, room commands or keymap is planned** —
   churn after the tag is welcome; a *known upcoming* contract break is not.

The owner's dogfood bar (two weeks of daily use, clock from 2026-08-01) still applies and
closes no earlier than 2026-08-15. Development never pauses for any of this: work merged
after the tag becomes the next minor version, and the tag itself is one command (§8).

The standing alternative — cut v1 as gauges only, statusline and HUD with declared vendor
version pins — remains rejected, because a v1 that named the gauges would name the wrong
product.

Two gauge surfaces over one data layer:

```
vendor adapters  ──►  normalized session model  ──►  renderers
(claude, codex)       (one schema, documented)      (statusline / HUD)
```

One Go module, one binary (`telltale.exe`). ADR-002 specified two modes; council (ADR-008)
is the third, and it does not sit on the pipeline above:

- **`telltale statusline`** (Claude Code and, since ADR-004, Antigravity CLI — routed
  on the payload's documented `product` field, §2.1): reads the vendor's JSON on
  stdin, prints one line, exits. Latency budget: single-digit milliseconds; Bubble
  Tea is never initialized on this path. Budget-conscious output (every character
  renders on every prompt).
- **`telltale hud`** (cross-vendor): a Bubble Tea/Lipgloss watch-mode TUI listing live
  sessions across vendors with per-session gauges. **First-class UI surface** — a UI
  design section (layout grid, color/threshold system, motion rules, empty/degraded
  state designs) is written here BEFORE the HUD is built, and degraded-state renders
  are eval fixtures. Windows Terminal is the reference rendering environment.
- **`telltale council`** (ADR-008, §9): the dispatch room — one brief typed once,
  answered by the seated vendor CLIs side by side, each column claiming only what was
  measured about that vendor. It spawns vendor CLIs instead of reading their session
  files, which is why it is off the data layer above and specified separately in §9.
- **`telltale hook <vendor>`** (§7.16): the vendor-hook relay — a per-turn payload on
  stdin, token counts to `~/.telltale/usage/`, and **nothing on stdout**, because a
  hook's stdout is parsed by the vendor as a hook result. Not a gauge and not a room; it
  renders nothing and is never run by a human.

**The gauges never write, with three bounded exceptions — all under `~/.telltale/`, all
numbers and keys only, never content.** `telltale council` keeps `council/room.json` (the
session ids reattaching needs); the statusline relays `quota/<vendor>.json` after its line
is on stdout (§7.15); and `usage/<vendor>.json` accumulates per-turn token counts from two
writers, `telltale hook` (§7.16) and the `telltale otel` collector (§7.16a). Each store
is atomic (temp+rename), best-effort, self-expiring on read, and pinned by a test that
walks the serialized form field by field. No transcript, prompt, reply, path or address
reaches any of the three. Anything else that wants to write from `internal/hud` or
`internal/statusline` is in the wrong package.

**Amended 2026-08-11: a FOURTH store exists, it carries content, and the paragraph above
was never corrected for it.** The event sink (`telltale events`, §7.21) writes each hook
payload VERBATIM under `~/.telltale/events/`. It does not widen the rule above, because
what contains it is scope rather than redaction: it is its own foreground mode the
operator starts, its server binds loopback only and refuses any other host, and nothing
in the gauges reads or renders those files. So the three counted above stay
numbers-and-keys, and the fourth is named as an exception instead of being folded into
them. §7.21 carries the record and CLAUDE.md's boundary section carries the same
exception.

The two gauge paths share exactly two packages: `internal/model` (the schema) and
`internal/theme` (thresholds and value formatters). `internal/theme` is stdlib-only and
holds no style type, which is what lets both surfaces share the numbers while the
statusline links no TUI framework.

## 2. Statusline segments (v1)

| Segment | Source (exact field) | Empty/degraded state | Status |
|---|---|---|---|
| Model | stdin `model.display_name` (falls back to `model.id`) | hide if both empty | **built** |
| Context % | stdin `context_window.used_percentage` (input-token based per docs) | hide segment | **built** |
| Session cost | stdin `cost.total_cost_usd` | hide segment | **built** |
| Quota pacing (5h) | stdin `rate_limits.five_hour.used_percentage` + `resets_at` (unix s) | rate_limits absent on API-key logins; each window independently absent → hide, never zero; countdown hides without `resets_at` | **built** |
| Quota pacing (7d) | stdin `rate_limits.seven_day.*` | same rule | **built** |
| Worktree | stdin `worktree.name` (present only in `--worktree` sessions) | hide segment | **built** |
| Folder | stdin `workspace.current_dir` (fallback `cwd`), basename only — no filesystem/git calls | hide segment | **built** |

Deliberately not shown **on this path**: git branch (would require an exec; the
statusline path reads nothing beyond stdin — revisit only with a measured budget),
permission mode (not in the stdin payload; same call the predecessor script made).
The path's one write is the quota relay (§7.15): after the line is on stdout, the
payload's rate-limit windows go to `~/.telltale/quota/` for the HUD — numbers only,
best-effort, never ahead of the render.

> Both exclusions are properties of the **stdin seam**, not of Claude Code. The
> transcript carries `gitBranch` and `permissionMode` directly, so the HUD's disk path
> gets both for free (§3.1). The statusline's exclusions are unaffected.

Threshold colors (applies to any percentage segment): green < 60, yellow ≥ 60, red ≥ 85,
from `theme.WarnPct` / `theme.CritPct`. `NO_COLOR` env strips styling. Derived displays
(reset countdown `↻2h13m`) are arithmetic on `resets_at` only.

Schema verification record: full stdin JSON schema captured from
code.claude.com/docs/en/statusline on 2026-08-01, including per-field absence semantics
(`rate_limits` Pro/Max-only and only after first API response; each window independently
absent). Statusline updates are debounced at 300ms and in-flight scripts are cancelled —
which is the empirical backing for the fast-exit budget. Parsing ignores unknown fields
by design (vendor adds fields between versions).

**Known divergence from `theme`, deliberate and unresolved:** the statusline's local
`pct` uses `%.1f`, which rounds, and its local `shortDur` has no days branch. The shared
helpers `theme.Percent` and `theme.Countdown` floor and carry days respectively, and the
HUD uses them. Unifying the statusline onto them changes its rendered output (99.96 would
stop reading as `100.0%`, a 7-day window would stop reading as `↻120h00m`), which is a
behaviour change to a shipped surface and is therefore a separate change with its own
fixture updates. The thresholds are already unified; the formatters are not.

### 2.1 Antigravity CLI statusline (added 2026-08-02, ADR-004)

`telltale statusline` serves a second vendor: Antigravity CLI (`agy`) hands statusline
commands a JSON payload on stdin, same seam shape as Claude Code. **Routing is the
documented `product` field** — agy stamps `"product": "antigravity"` on every payload
(observed on all six live captures) and Claude's payload has no product field; one
binary, one subcommand, no flag. Stdin is read once and handed to whichever parser the
marker selects (`internal/antigravity`).

| Segment | Source (exact field) | Empty/degraded state | Status |
|---|---|---|---|
| Model | stdin `model.display_name` (falls back to `model.id`) | hide if both empty | **built** |
| Context % | stdin `context_window.used_percentage` (vendor-reported; the payload also carries `context_window_size`) | hide segment; 0 is a reading and renders `ctx 0%` | **built** |
| Quota buckets | stdin `quota.<id>.remaining_fraction` + `reset_in_seconds` (fallback `reset_time`) — one segment per NAMED bucket, ids rendered verbatim, sorted for stability; used% = (1−remaining)×100, a unit conversion | bucket without `remaining_fraction` hides; absent map hides all | **built** |
| Agent state | stdin `agent_state` — the first vendor-REPORTED liveness signal on any seam; `tool_confirmation_pending: true` outranks it and renders `confirm?` | hide if empty; unknown vocabulary renders verbatim in dim | **built** |
| Branch | stdin `vcs.branch` (+`*` when `vcs.dirty`) — in the payload, so no exec; the no-I/O-beyond-stdin rule holds | hide segment | **built** (documented; not yet observed live — §3.8) |
| Folder | stdin `workspace.current_dir` (fallback `cwd`), basename only | hide segment | **built** |

Not rendered, deliberately: `cost` does not exist anywhere in the payload (nothing is
priced); `email` and `plan_tier` are identity, not gauges; `transcript_path` is
advertised but never written by agy 1.1.9 (§3.8) and displaying a path to a
nonexistent file would be narrating.

Schema verification record: documented contract (antigravity.google/docs/cli/statusline)
cross-checked against a six-payload live capture from a real interactive session on agy
1.1.9, 2026-08-02 (§3.8). Fixtures are synthesized to the observed shapes.

## 3. HUD (v1)

One row per live session, both vendors; per-row: vendor, session identity, model,
context/quota gauges **where the vendor provides them**, last-activity age. The rendered
grid, its responsive tiers and every degraded state are specified in §7.

### 3.1 Claude Code adapter sources — VERIFIED LIVE 2026-08-01, Claude Code 2.1.219

Read-only survey of `%USERPROFILE%\.claude\` on the dev PC: 33 project dirs, 837
sessions, 13,211 records walked. Nothing from that survey is reproduced here or in the
fixtures; the fixtures are synthesized to shape only.

**Discovery glob — `~/.claude/projects/*/*.jsonl`, non-recursive, UUID basename.**
Measured: non-recursive = 837 files, recursive = 2021. The extra 1,184 are subagent
transcripts under `<sessionId>/subagents/**`, plus `tool-results/` and `workflows/`
sidecars; a `**/*.jsonl` glob inflates the session list 2.4x and double-counts every
token. Non-`.jsonl` neighbours share the directory (`.memory-sync-manifest.json`), so
the basename is checked as a UUID, not just the extension.

The project-directory slug is **lossy and must never be decoded to a path**: `\` and a
literal `-` both encode as `-` (`C--Users-dev-code-my-app` could be `code\my-app` or
`code-my\app`), and the drive-letter case is not stable — the same tree can hold
`C--Users-…-app` and `c--Users-…-app` as siblings. `cwd` is read from the record; the slug is an
opaque grouping key. Because those sibling directories can hold the same session id,
`Discover` also de-duplicates by id, newest mtime winning: a duplicate id would break
the HUD's row matching. The tree also mutates during a sweep (a project dir vanished
between enumeration and open during the survey), so `Discover` swallows ENOENT on dirs
and files and continues rather than aborting.

**Record fields the adapter reads** (all on `assistant` records unless noted):

| Normalized field | Exact source | Absent when |
|---|---|---|
| Session id | `sessionId` (present on every record type) | never |
| Working dir | `cwd` | metadata-only record types |
| Git branch | `gitBranch` (carried as a display-only extra) | outside a repo |
| Model | `message.model` | non-`assistant` records; `"<synthetic>"` is rejected |
| Tokens in context | `message.usage.input_tokens + .cache_read_input_tokens + .cache_creation_input_tokens` | no `usage` |
| Last activity | file mtime | never (but see clock skew below) |
| CLI version | `version` (display-only extra) | never |
| Title | `custom-title.customTitle`, else an `ai-title` record | untitled sessions |
| **Sub-agent count** (v1.1) | **stat only:** entries matching `<uuid>.jsonl` under `<sessionId>/subagents/`, mtime within 15 min | never — an absent directory is a measured **zero**; only an unreadable one is absent |

**The sub-agent count is `CapDerived`, and the reason is worth stating precisely.** The
files are counted *exactly* — one `ReadDir`, one `Info` per entry, no file opened and no
byte parsed, which is what makes it affordable on the 1 s poll. What is inferred is the
15-minute recency boundary that turns "written lately" into "a fan-out is running now".
That inference is the thing the estimate marker exists to expose, so the chip renders
`⑂~2` rather than `⑂2` (§7.13). The boundary is `model.DefaultLivenessThresholds.Idle`
rather than a second constant: the chip sits on a row whose state dot already classifies
"recent" at that boundary, and two definitions of recent on one line is how a display
starts contradicting itself.

Two absences are distinguished, per §4a.1: the directory **not existing** means the
session never fanned out, which is a countable zero; the directory existing and the OS
**refusing** is nil plus a diagnostic, because we do not know. A sub-agent transcript
whose mtime is ahead of the local clock is not counted at all — the same rule the
session's own mtime gets, for the same reason: a timestamp ahead of the clock is not a
readable time, so it cannot be evidence of recency.

Only `assistant` and `user` records carry `message`. `custom-title`, `last-prompt`,
`mode` and `ai-title` carry `{type, sessionId, <one key>}` and have **no `timestamp`
and no `cwd`** — a parser that assumes those fields exist will nil-deref. Full observed
`type` set: `assistant, user, attachment, last-prompt, queue-operation, custom-title,
pr-link, mode, system, ai-title, permission-mode, file-history-snapshot`.

The survey verified the **shape** of an `ai-title` record but not the name of its payload
key. The adapter therefore matches on the verified structure — exactly one key beyond
`type` and `sessionId`, holding a string — rather than guessing a field name from memory.
A record that does not match yields no title and the row falls back to its workspace
name, which is absence rather than a wrong label.

**Claude capability gaps (grepped, zero matches across the corpus):**

- **No cost.** `cost.total_cost_usd` exists only on the statusline stdin payload.
- **No quota.** `rate_limits.*` likewise stdin-only.
- **No context window size**, so **context % is not derivable** — the denominator varies
  by model and by the `[1m]` variant. Token counts are sourced and carried as a
  display-only extra; the CONTEXT cell for a Claude row is absent (§6 Q7).

**Honest-gauge traps pinned by fixtures:**

- `input_tokens` alone is not context usage. Measured live: `input_tokens=2,
  cache_read=213388, cache_creation=2464`. Reading `input_tokens` renders **2 tokens**
  for a ~216k-token context.
- `message.model` can be `"<synthetic>"` (locally generated notices, zeroed usage). It
  must never reach the model cell and its zeros must not overwrite a real reading.
- An mtime ahead of the local clock has **no readable age**. It is left nil and marked
  degraded rather than clamped, because "0s" claims the session was active this instant.
  The HUD renders `—`.

**Liveness — and why the PID registry is not read in v1.** The honest primitive is
**mtime = last activity**, rendered as an age. The survey found an undocumented registry
at `~/.claude/sessions/<PID>.json`
(`{pid, sessionId, cwd, startedAt (unix ms), version, kind, entrypoint, name,
nameSource}`); at survey time all 7 entries mapped to live PIDs and to top-level
transcripts. **The adapter does not read it.** Every use of it reduces to "a process
with this id exists", which §4a.4 names explicitly as evidence a process exists rather
than evidence the session is doing anything — and a liveness hint is the one value
`Validate` cannot check, so the bar for emitting one is a signal that actually separates
working-now from process-exists. `liveness` is therefore `CapNone` for Claude and the
HUD classifies every vendor identically from `last_activity`. The registry stays recorded
here as a verified observation on 2.1.219 (not a vendor contract) in case a later version
adds a turn-start/turn-end signal worth reading.

**Read strategy.** Transcripts routinely reach 7.7 MB. The adapter reads a bounded
**head** (64 KiB — session id / cwd / git branch / title, verified present on the first
record of 60/60 files sampled) plus a bounded **tail** (256 KiB, first fragment discarded
as partial), scanning for the newest `assistant` record with `message.usage` and a
non-synthetic model. A file smaller than the tail window is read once, not twice.
Records with `isSidechain == true` are skipped defensively — 0 of 837 top-level
transcripts contain one on 2.1.219, but the filter is free.

### 3.2 Codex CLI adapter sources — RESEARCHED FROM SOURCE, **NOT LIVE-VERIFIED**

Codex is now installed on the dev PC (2026-08-01): **Codex Desktop** (VS Code app
26.727.51351, bundling `codex-cli 0.146.0-alpha.9.2` under `%LOCALAPPDATA%\OpenAI\Codex`)
plus the **npm CLI** (`codex-cli 0.146.0`). The claims below were first read from
`github.com/openai/codex` at commit `1e85ca09` (2026-08-01): `codex-rs/utils/home-dir/src/lib.rs`,
`codex-rs/rollout/src/{lib,recorder,compression,policy,metadata}.rs`,
`codex-rs/protocol/src/{protocol,models}.rs`,
`codex-rs/login/src/auth/default_client.rs`, `codex-rs/thread-store/README.md` — and then
checked against the live corpus. **§3.4 carries the verified results and the itemized
remainder**; the adapter is not "done" until the remainder is discharged.

**Layout.** `$CODEX_HOME` (default `~/.codex`) `/sessions/<YYYY>/<MM>/<DD>/rollout-<YYYY-MM-DDThh-mm-ss>-<uuid>.jsonl`,
fixed depth, no recursion. The date directory is **local** time, not UTC — deriving
today's directory from a UTC clock silently loses sessions across midnight and DST, so
the adapter walks the tree instead of computing a path. Files older than 7 days are
compressed in place to `.jsonl.zst` (zstd level 3); the adapter reads `.jsonl` only — a
`.zst` file is by construction ≥7 days cold and cannot be a live row, so skipping it
avoids a zstd dependency. `rollout-compression.lock` and `*.tmp` in the same tree are not
sessions. `archived_sessions/` is deliberately ignored.

**Envelope.** `RolloutLine { timestamp, ordinal?, #[serde(flatten)] item }` with
`RolloutItem` tagged `#[serde(tag="type", content="payload")]`:

```json
{"timestamp":"…","ordinal":42,"type":"session_meta|turn_context|response_item|event_msg|compacted|world_state|…","payload":{…}}
```

`EventMsg` is **internally** tagged, so its discriminator sits *inside* `payload`
alongside its fields (`payload.type == "token_count"`, with `info` / `rate_limits` as
siblings). This differs from the outer envelope and is the easiest thing to get wrong.

**Field mapping:**

| Normalized field | Exact source |
|---|---|
| Session id | filename uuid, cross-checked against `session_meta.payload.id` / `.session_id` |
| Working dir | `session_meta.payload.cwd`, then the last `turn_context.payload.cwd` |
| Git branch | `session_meta.payload.git.branch` (display-only extra) |
| Model | **last** `turn_context.payload.model` (not on `session_meta`) |
| **Context %** | **derived**: last `token_count` → `info.last_token_usage.total_tokens ÷ info.model_context_window` |
| **Quota** | `payload.rate_limits.primary` / `.secondary` → `{used_percent (0–100), window_minutes, resets_at (unix s)}` |
| Plan / CLI version / history mode | `rate_limits.plan_type`, `session_meta.payload.cli_version`, `.history_mode` (display-only extras) |
| Sub-agent thread | `session_meta.payload.agent_nickname` / `agent_role` non-null → not a session |
| Last activity | file mtime (matches Codex's own `updated_at`/`recency_at` derivation) |

`session_meta.payload.history_mode` is `legacy` (default) or `paginated` and changes
which *message* records exist. The adapter does not branch on it: `policy.rs` persists
`SessionMeta`, `TurnContext` and `EventMsg::TokenCount` under both modes, and those are
the only records it reads. The mode is carried as a display-only extra so a live
verification pass can see which one produced a given fixture.

`TokenCountEvent.info` and `.rate_limits` are both `Option`. **Judgement call, UNVERIFIED
(§3.4):** a `token_count` whose `info` or `rate_limits` is null is treated as *clearing*
that datum rather than leaving the previous value standing. `protocol.rs` annotates the
neighbouring field with *"`None` is unavailable, not a sparse-update recovery"*, which
reads as "we do not have it" rather than "unchanged"; it is also the conservative side of
the honest-gauge rule, since it never shows a number the vendor's most recent statement
did not contain. The 2026-08-01 live pass could not settle it — no session in the corpus
emitted a mid-stream null after a populated event — so the conservative reading stands
unfalsified rather than confirmed (§3.4 "still owed").

**Codex capability gaps:** no cost in USD anywhere; **no process-liveness registry** (mtime
is the only signal); no session title, so rows fall back to the workspace basename; cold
`.zst` sessions unreadable under minimal deps. Reading the SQLite state DB
(`codex-rs/rollout/src/state_db.rs`) is a **rejected** path — it would add a sqlite
dependency for metadata the JSONL already carries, and `thread-store/README.md` confirms
JSONL stays canonical and readable without SQLite.

### 3.3 Cross-vendor capability matrix — the asymmetry is a design fact, not a bug

| Field | Claude (disk) | Codex (disk) | Gemini (disk, §3.7) | Antigravity (disk, §3.8) | Cursor (disk, §3.9) | Grok (disk, §3.9a) |
|---|---|---|---|---|---|---|
| session id, cwd, git branch | yes | yes | id yes; cwd via `projects.json`; branch no | id yes; cwd via the trajectory blob's `file:///` URI; branch no | id yes; cwd via `workspaceStorage/<id>/workspace.json`; branch no | id yes; cwd verbatim in `summary.json`; branch **yes, unused** (`head_branch`, only when the cwd is a repo) |
| model | yes | yes | yes (per message) | yes (per generation, id + display name) | yes (`modelConfig.modelName`, one string; sometimes the literal `default`) | yes (`current_model_id`, one string) |
| token counts | yes | yes | yes (per message) | yes (per generation, self-checking) | context totals yes; per-message counts present and **always 0** | yes (per turn in `updates.jsonl`, plus a context total in `signals.json`) |
| context window size | **no** | yes | **no** (static table in CLI source only) | **no** (statusline payload only) | yes (`contextTokenLimit`) | yes (`contextWindowTokens`) |
| context % | **not derivable** | **derived** | **not derivable** | **not derivable** | **reported** (the vendor persists its own; derived from raw counts only if it is missing) | **reported** (`contextWindowUsage`, an integer the vendor truncates) |
| quota / rate limits | **no** (statusline stdin only) | yes | **no** (runtime 429 handling only) | **no** (statusline stdin only; never persisted) | **no** — plan *entitlements* on disk, no consumption record | **no** — nothing account-level anywhere in the store |
| cost USD | no (stdin only) | no | no | no | **no** — `usageData` `{}`, token counts unpopulated zeros | **per turn yes, session total no** — `costUsdTicks`, unit measured; no cumulative figure exists |
| process liveness | registry exists, deliberately unread (§3.1) | none | none | `steps.status` exists, structural only (never observed in-flight) | `status`/`generatingBubbleIds` exist, structural only (never observed in-flight); Hooks is the real seam | `active_sessions.json` exists and was **measured empty during a live turn**; `events.jsonl` phases outlive the process |
| session title | yes | no | yes (`summary` metadata) | **no** — the only free text on disk is prompt content | yes (`value.name`, vendor-generated) | yes (`generated_title`, vendor-generated; absent on headless runs) |
| sub-agent count | **derived** (`subagents/` sidecar, §3.1) | **no** | **derived** (`chats/<parent-id>/` nest) | **no** — `parent_references` observed empty | **no** — `isSubagent`/`numSubComposers` observed zero throughout | **no** — a `spawn_subagent` tool exists, nothing about it reaches disk |

Codex is `CapNone` for the sub-agent count and not merely empty. Sub-agent *threads* do
exist in the Codex format — `session_meta.payload.agent_nickname` marks one, and the
adapter rejects those rollouts with `ErrSubAgentThread` — but they are whole top-level
rollout files carrying no link back to a parent session, so there is nothing to attribute
a chip to. Declaring the field and always emitting zero would assert "this Codex session
is running no sub-agents", which is not something the format lets us check.

Claude's quota lives on the statusline seam; Codex's lives on the disk seam. So the HUD's
quota block is Codex-sourced today, and the CONTEXT column carries a Codex number beside
a Claude em dash. **Nothing sources cost**, so the COST column auto-hides in every real
v1 frame — see the `v1-capabilities` render in §7.3.

Cursor is the first vendor to put a context percentage on disk as a number *it* computed,
which makes it the only unmarked bar in that frame. Everything else about it is the
asymmetry again from the other side: it is also the first vendor whose store holds live
credentials, so the adapter's most load-bearing property is the list of things it does
not read (§3.9, decisions/007). Grok is the second to report a percentage, and the first
to write a **dollar figure** to disk at all — and the COST column still auto-hides on its
rows, because what it writes is one turn's cost and never the session's (§3.9a). "Nothing
sources cost" became "nothing sources a session cost", which is a narrower sentence and
the same column.

**Percentage comparability.** Codex's own
`TokenUsage::percent_of_context_window_remaining` subtracts `BASELINE_TOKENS = 12000`
from both numerator and denominator; Claude's `context_window.used_percentage` is raw
input-token-based. **They are not the same statistic.** The adapter therefore does *not*
reproduce Codex's baseline-normalized figure: it computes a plain
`last_token_usage.total_tokens ÷ model_context_window`, declares it `CapDerived`, and the
HUD marks it with an estimate marker. See §6 Q7 for the resolution and its alternatives.

### 3.4 Live verification (ADR-001) — first pass run 2026-08-01; remainder itemized

**Environment:** Codex Desktop app 26.727.51351 bundling `codex-cli 0.146.0-alpha.9.2`
(every live rollout in the corpus was written by it, `originator: "Codex Desktop"`,
`source: "vscode"`), with npm `codex-cli 0.146.0` installed alongside. The source read
above was taken at CLI `2.1.219`; no contradiction between the two surfaced except where
noted below.

**Confirmed:**

- `sessions/<YYYY>/<MM>/<DD>/` is the **local** date: events stamped `2026-08-02T00:12Z`
  (UTC) sit under `08/01`. Walking the tree instead of computing today's path was right.
- `session_meta` writes **both** `id` and `session_id`, identical values.
- `history_mode` is `"legacy"` on every fresh thread.
- `model_context_window` is populated (`258400` for `gpt-5.6-terra`), so the derived
  context percentage works as designed.
- `ordinal` is **not emitted** — the envelope is `{timestamp, type, payload}` only.
  Fixtures 0002/0003 keep their `ordinal` deliberately (the field must stay tolerated);
  fixtures 0006/0007 pin the observed no-`ordinal` shape.
- `rate_limits`, live values: **free plan** = `primary` only with `window_minutes: 43200`
  (a 30-day window), `secondary: null`; **plus plan** = `primary` with
  `window_minutes: 10080` (7 days), `secondary: null` so far, `plan_type: "plus"`. The
  "record real values instead of hard-coding 5h/7d" instinct was right — neither plan
  matches the guessed pair, and labels derive from `window_minutes` alone. Newer fields
  (`limit_id`, `credits{}`, `plan_type`, `rate_limit_reached_type`) are parsed loosely;
  `credits.balance` has been observed as both `null` and the string `"0"`, so nothing in
  it is typed strictly.
- Go can `os.Open`, head-read, and tail-read a rollout **while a live codex process holds
  it** (verified against an active session; Windows sharing mode is permissive).

**Learned, not on the checklist:**

1. **Imported external-agent transcripts.** Desktop onboarding imported 35 Claude
   sessions into `sessions/<date>/` as rollout files. Markers: `session_meta` lacks
   `thread_source` (native threads carry `"user"`); every turn's `task_started.turn_id`
   is `external-import-turn-<n>` (inside the head window in all 35 observed files); the
   single `token_count` is synthetic (zero components, non-zero `total_tokens`, null
   window, null `rate_limits`). The adapter rejects these with `ErrImportedTranscript`
   on the affirmative `turn_id` marker only — absence of `thread_source` is not used, so
   pre-`thread_source` CLI rollouts are unaffected. Rendering an imported Claude
   transcript as a Codex row is a cross-vendor double count; the filter is not optional.
2. **`archived_sessions/` semantics confirmed the hard way.** The first inspection pass
   found every real session in flat `archived_sessions/` and only imports in
   `sessions/<date>/`, which read as "Desktop sessions are invisible to the adapter."
   A later live session disproved that: Desktop writes live rollouts under
   `sessions/<date>/` and threads move to `archived_sessions/` when archived — the
   Desktop auto-archives its onboarding threads, which is what emptied the first hour.
   Ignoring `archived_sessions/` remains correct.
3. **Windows mtime does not reliably advance mid-session.** On an active session the
   newest records were stamped ~100 s *after* the file's mtime: NTFS defers the mtime
   update while the writer holds the handle. `LastActivity` from mtime therefore
   under-reports on live sessions (never over-reports). ~~The ruling is §6 Q8~~ —
   **ruled and implemented 2026-08-01**: `LastActivity = max(mtime, newest record
   timestamp)`, both adapters; see §6 Q8 for the rules.
4. Desktop threads run in per-thread scratch workspaces
   (`Documents\Codex\<date>\<slug>`), so the workspace-basename fallback shows the
   thread slug, not a repo name. Cosmetic, vendor-truthful, unchanged.

**Discharged 2026-08-01, same evening:**

- ~~A rollout written by the **standalone CLI**~~ — observed live: two sessions from
  the npm CLI at 0.146.0 write `session_meta` with `originator:"codex-tui"`,
  `source:"cli"`, `thread_source:"user"` — same record shape and tree layout as the
  Desktop writer, so no adapter change (nothing keys on originator). The observation
  also reproduced §6 Q8 a second time: the first CLI rollout's mtime settled roughly
  twenty minutes after the session ended, when the writer released the file.

**Still owed** (re-scannable on demand: `tools/scan-passive-tail.py`):

- Null `info`/`rate_limits` mid-stream, "cleared" vs "unchanged" (§3.2): the corpus
  contained **no mid-stream nulls**, so the conservative "clearing" reading stands
  unfalsified rather than confirmed.
- An **API-key login** capture (rate_limits expected absent), and whether a paid plan
  ever populates `secondary`. Capture path when wanted: `codex login --with-api-key`
  (reads the key from stdin), run one short session, re-scan, then plain
  `codex login` to return to the ChatGPT plan.
- The 7-day `.zst` compression pass — unobservable until the corpus is a week old.

*Re-scan 2026-08-02:* 5 native rollouts (35 imports filtered), including one new
Desktop session — still zero mid-stream nulls; plus-plan `secondary` still null
across all 32 populated `rate_limits`; no API-key-signature session; no `.zst`
anywhere under `sessions/`. Oldest native rollout is 2026-08-01, so the `.zst`
pass stays unobservable before ~2026-08-08.

*Re-scan 2026-08-11:* 316 native rollouts, 1,251 populated `rate_limits`. All three
owed items stay negative. Two of them now rest on 316 rollouts rather than the 5 of
2026-08-02, and a fourth observation changes what the API-key capture must look for.

- **Mid-stream nulls: still zero**, now across 316 rollouts rather than 5. The
  conservative "clearing" reading stays unfalsified rather than confirmed.
- **`secondary`: still null** in all 1,251 populated `rate_limits`. A plus plan does
  not populate it.
- **`.zst`: still zero files** under `sessions/` — and this item is no longer blocked
  on time. The oldest native rollout is 2026-08-01, so it was 10 days old at the scan,
  and Codex wrote new rollouts on 9 later days. The pass is **measured absent on this
  box, not unobservable**. The prediction above expired on ~2026-08-08.
- **A `rate_limits` object can report "no windows" without being absent.** 21
  `token_count` records across 4 native `codex_exec` sessions (`cli_version`
  0.146.0, 2026-08-07 and 2026-08-08) carry a `rate_limits` OBJECT whose `primary`,
  `secondary`, `plan_type`, `limit_name`, `individual_limit` and
  `spend_control_reached` are all null, beside `limit_id:"premium"` and a `credits`
  block. Each of those sessions is null from its FIRST `token_count`, so this is not
  a mid-stream clear and it does not settle §3.2.

  **What it costs the owed capture.** `tools/scan-passive-tail.py` detects the
  API-key signature as `rate_limits is None` alone, so a session of this shape
  passes it unseen and reports as negative. Whoever runs the capture must check
  both signatures: an absent `rate_limits`, and a present one whose windows are all
  null. **The cause is deliberately not stated here** — nobody recorded which auth
  mode those four sessions ran under, so a claim that they are API-key sessions
  would be an inference, and §4a.1 forbids one dressed as a reading.

### 3.5 Framing rule — now measured, not assumed (see §4)

The §4 hazards were quantified against the live Claude corpus:

- **64 KiB cap: firing.** 107 of 13,211 records exceed 64 KiB; the longest single line
  is **1,004,230 characters**. `bufio.Scanner` at its default cap returns
  `bufio.ErrTooLong` on ~0.8% of records, and an unchecked `Err()` silently truncates the
  file — reading as "no more sessions". Adapters use `bufio.Reader.ReadBytes('\n')` via
  `internal/jsonl`, which is the one tested implementation of this rule.
- **U+2028/U+2029: not observed** (0 raw `E2 80 A8`/`E2 80 A9` bytes across the 40 newest
  transcripts). That is absence of evidence, not absence of hazard — the records carry
  model-authored text and both characters are legal unescaped inside a JSON string. The
  §4 byte-level rule is unchanged and both fixtures embed the character to pin it, with
  a `.gitattributes` entry plus a byte assertion in each adapter's tests so a checkout
  rewrite fails the build instead of silently disarming the test.
- **Trailing partial line:** all 7 live transcripts ended on `0x0A` at survey time, so
  writes look line-atomic — a sampled observation, not a guarantee. The hold-until-`\n`
  rule stands, and both fixtures end in a deliberate truncated record with no trailing
  newline.
- **Windows concurrent read:** all 7 live transcripts opened with share-read/write while
  their processes were running. Go's `os.Open` already requests
  `FILE_SHARE_READ|WRITE|DELETE`, so no special handling is needed — recorded here so
  nobody "fixes" it later.

### 3.6 Degradation rule

A vendor field the adapter cannot read renders as `—` (absent), never as a zero or a
stale value presented as fresh. A record that parses only partially degrades the fields
it could not source to `—` and keeps the rest. A truncated trailing line is not a record.
The exact renders are §7.7.

### 3.7 Gemini CLI seam — source-verified 2026-08-02; first live pass itemized

**Environment:** gemini-cli 0.53.1 installed via npm 2026-08-02; the persistence layer
read at tag v0.53.1 (`packages/core/src/services/chatRecordingService.ts` +
`chatRecordingTypes.ts` for the writer and record shapes, `config/storage.ts` for the
tree, `config/projectRegistry.ts` for the slug registry). This is the writer's own
source, not its docs — the same standard as the Codex `rollout` read (§3.2).

**Layout (from source):**

- Sessions: `~/.gemini/tmp/<project-slug>/chats/session-<YYYY-MM-DDTHH-MM>-<id8>.jsonl`.
  The filename embeds only the session id's first 8 characters; the full id is on the
  first record. `~/.gemini/projects.json` maps absolute project paths (lowercased on
  Windows) to slugs; the slug scheme replaced sha256-hash directory names in 0.5x, and
  the registry self-heals from `.project_root` markers.
- Sub-agent transcripts nest at `chats/<parent-session-id>/<id>.jsonl` — a structural
  parent link, which is why Gemini declares `subagents` (derived) where Codex cannot
  (§3.3): Codex's sub-agent threads are top-level files with no path back to a parent.
- Legacy pre-JSONL sessions are single-document `*.json`; the adapter skips them.

**Record shapes (from source):** the first line is metadata (`sessionId`,
`projectHash`, `startTime`, `lastUpdated`, optional `kind`/`directories`); message
records carry a string `id`, `timestamp`, `type`, and on `type:"gemini"` a `model` and
a per-message `tokens` summary (`input` = promptTokenCount, `cached` a subset of it,
`output`, `total`); `{"$set":{...}}` records patch metadata (including `summary`, the
session title, and whole-array `messages` checkpoints that can put megabytes on one
line — the §4 framing rule is earning its keep here); `{"$rewindTo":id}` truncates.
**Messages are upserts**: the writer re-appends the full record under the same id when
tokens or tool calls settle, so a linear last-wins pass needs no dedup map.

**Traps encoded in the adapter:**

- The writer **deletes** a session file on exit when it holds no resumable content, so
  a file vanishing between Discover and Read is normal operation (`ErrSessionGone`,
  row dropped silently).
- Nothing quota-shaped is persisted — rate limiting exists only as runtime 429
  handling (`googleQuotaErrors.ts`, `retry.ts`). `quota` is CapNone, not empty.
- No context-window size reaches disk; the CLI's own percentage divides by a static
  per-model table compiled into its source. An assumed denominator is an invented
  gauge, so `context_pct` is CapNone — the §4a.7 sketch guessed
  "derived" here, and the source read falsified the guess.
- `workspace` is read verbatim from the vendor's registry entry (REPORTED, a lookup
  not a computation), with a fidelity caveat: the vendor lowercases the recorded path
  on Windows.
- The adapter replays the writer's grammar, not just its records: `$rewindTo`
  truncates the ordered message log (a rewind to an id outside the read windows
  conservatively clears it), and a `$set` messages checkpoint clears and rebuilds it —
  both mirroring the vendor's own loader. Independent review (2026-08-02) caught the
  first cut ignoring both; a rewound-away 215k-token reading would have kept rendering.
- **Bounded-read limitation, stated:** the head/tail windows share the seam behaviour
  of every adapter — a record crossing the boundary is read by neither window, and a
  single line larger than the tail budget (256 KiB) is outside the read entirely. On
  Gemini that line can be a whole-conversation checkpoint, so a giant checkpoint's
  values are invisible until the next ordinary record re-establishes them. Accepted as
  the same tradeoff the other adapters carry; the live pass below sizes real
  checkpoints to check whether the budget needs raising.

**Market note (2026-08-02, post-merge):** Gemini CLI stopped serving consumer tiers
(free/Pro/Ultra) on 2026-06-18; it remains live for Gemini Code Assist
Standard/Enterprise licenses and paid API keys, with Antigravity CLI (`agy`) as the
consumer successor (ADR-003 addendum). This adapter therefore covers the
enterprise/API-key flavour. (A live session was nonetheless produced on this machine
2026-08-03 — the auth flavour behind it was not investigated; recorded as an observed
fact only.)

**First live pass — RUN 2026-08-03 and PASSED** (gemini-cli 0.53.1, the same version
the source read pinned; one real session, ~1.6 MB, 50 records, written live during
the check). Adapter output against it: discovered 1; model `gemini-3.5-flash`;
workspace `c:\users\sanle` via `projects.json` (lowercased-path registry confirmed);
LastActivity rode the mtime side of the Q8 fold (the file was being touched after its
newest record timestamp — the live-write pattern the fold exists for); subagents
derived 0; zero diagnostics; Validate green. Name is absent and honestly so — the
header carries no title field; the HUD label falls back to the workspace basename.
The itemized checks resolve as follows (original list kept below for the record):

- Metadata line is the first line: **confirmed.** Main sessions **carry
  `kind:"main"`** — the fixture's omitted-field assumption was falsified; the adapter
  is unaffected (only `"subagent"` branches) and the healthy fixture now carries
  `kind:"main"` to match reality.
- Filename shape and prompt registry entry: **confirmed** (entry present, lowercased).
- Upserts against real traffic: **confirmed** — 27 message records, 20 distinct ids
  (7 in-place updates). Per-message `tokens` **confirmed live** with shape
  `{cached, input, output, thoughts, tool, total}` — unused by the adapter (context
  stays CapNone per §4a.7's falsification), recorded for fidelity. Delete-on-exit for
  non-resumable sessions: not observable (this session persisted).
- Checkpoint sizes vs the read budgets: the two live `$set` messages checkpoints were
  **7.3 KiB and 14.5 KiB — neither approached the 64 KiB scanner cap**; the "expected:
  yes, on any long session" guess did not materialize (checkpoints snapshot compactly).
  The budget pressure came from elsewhere: **single message lines up to 746 KiB** were
  observed, dwarfing both budgets. The newest checkpoint sat wholly inside the 256 KiB
  tail and the bounded read produced correct output — a tail that starts mid-line
  resyncs at the next newline, exactly the framing design.
- `$rewindTo`: **not exercised by this session** — remains fixture-verified only.

**ADR-003's verification hold is RELEASED**: the launch post may claim the Gemini
adapter live-verified.

Original itemized list (as written before the pass):

- Confirm the metadata line is the FIRST line in every live file, and whether main
  sessions carry `kind:"main"` or omit the field (the fixture assumes omitted).
- Confirm filename shape and that `projects.json` gains the entry promptly (the
  registry can lag a fresh project; the adapter treats a missing entry as absence).
- Confirm the upsert pattern and per-message `tokens` against real traffic, and the
  non-resumable delete-on-exit behaviour.
- Observe whether a live `$set` messages checkpoint exceeds the 64 KiB scanner cap in
  practice (expected: yes, on any long session), and whether any exceeds the 256 KiB
  tail budget (which would put whole checkpoints outside the bounded read — see the
  limitation above).
- Observe a real `$rewindTo` and confirm the truncate-or-clear replay against the
  loader's behaviour on the same file.

### 3.8 Antigravity CLI seam — surveyed live 2026-08-02; statusline is the seam

**Environment:** agy 1.1.9, installed 2026-08-02. Closed source (the GitHub repo,
google-antigravity/antigravity-cli, is docs and examples only), so this survey is the
Claude Code method: documented contracts cross-checked against live observation, no
source read possible.

**Disk verdict (first survey — superseded the same day by the re-survey below).**
Interactive and headless sessions both write
`~/.gemini/antigravity-cli/conversations/<conversation-id>.db`: SQLite, protobuf blobs
in every payload column (`steps.step_payload`, `gen_metadata.data` — inspected
read-only). The first survey judged the blobs unparseable without guessing (and the
repo had already rejected a SQLite dependency once, §3.2), and found no transcript: the
docs and the live payload both advertise
`~/.gemini/antigravity/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`,
that path did not exist, and `antigravity-cli/brain/` held only empty `scratch/` dirs
at survey time. Verdict then: no honest HUD adapter; agy shipped as a statusline-only
vendor (ADR-004), with a standing watch item on the transcript.

**Re-survey (2026-08-02, later the same day; ADR-005 decision 5, prompted by ccusage
issue #1402): the disk seam is OPEN, and the watch item is RESOLVED — the transcript is
real.** Corpus: four conversations, all agy **1.1.9** — the same version the first
survey ruled on; this is a correction, not a version change.

- `brain/<id>/.system_generated/logs/transcript.jsonl` **exists for all four
  conversations**, non-empty, plus an undocumented untruncated sibling
  `transcript_full.jsonl` (`transcript.jsonl` marks its cuts with `truncated_fields`).
  Written default-on, with `enableTelemetry: false`, under `antigravity-cli/` — the
  docs' advertised `antigravity/` tree still does not exist. Why the first survey saw
  only empty dirs is unresolved (a flush-timing artifact vs. a missed `.system_generated`
  subdir); both observations stand as recorded. The transcript is plain JSONL, one line
  per `steps` row (verified exact, 38/38 across the corpus): step_index, source, type,
  status, created_at (RFC3339 UTC, second resolution), content, thinking, tool_calls,
  exit_code.
- `gen_metadata.data` **decodes with a stdlib protobuf wire walk** (no schema, no
  deps). Per-generation token counts live at `#1.#4`: uncached input (`#2`), total
  output (`#3`), cache-read (`#5` — inferred from position and magnitude, lower
  confidence), thinking (`#9`), answer (`#10`), and the per-generation response id
  (`#11`, the dedup key — the *top-level* `#4` UUID is constant per conversation and
  must not be used). Model id at `#1.#19` (`gemini-3.6-flash`) and display name at
  `#1.#21`, matching the live statusline string verbatim. **Self-check: `thinking +
  answer == output` held in 15/15 decoded generations** — the arithmetic identity that
  promotes this from field-guessing to a schema. An adapter must assert it at read time
  and degrade to absent rather than render a number that fails it.
- What an adapter could report as **measured**: Name, Model, Workspace (trajectory-blob
  URI), LastActivity (max transcript `created_at`; the trajectory-blob timestamp is
  session *start* — do not use it). **Partial**: context used-tokens (numerator
  measured; the 1,048,576 window denominator appears nowhere on disk — it must come
  from the statusline payload or a constant with stated provenance). **Absent**: cost
  (consumer auth; no pricing on disk), quota (server-refreshed in memory, never
  persisted). **Structural only, never observed live**: liveness (`steps.status` — all
  38 observed rows `DONE`; no in-flight session sampled yet) and subagents
  (`parent_references` empty, `has_subtrajectory` all zero, `define_subagent`/
  `invoke_subagent` present in the tool registry).
- Build cautions, recorded before any adapter work: **WAL sidecars are load-bearing**
  (a `-wal` larger than the `.db` was observed — copy `.db`+`-wal`+`-shm` together, or
  open the live file strictly read-only and let SQLite replay; never open it for
  write). `conversation_summaries.db` is a stale index (1 row for 4 conversations) —
  enumerate `conversations/*.db`, never trust it. `cache/last_conversations.json` is
  written at session start and points at the *previous* session for a reused
  workspace. Transcript `content`/`thinking` and the request blobs are PII (full
  prompt text, file contents; the account email appears in `cli.log`) — an adapter
  reads structural fields only and never surfaces those into the HUD or any log. All
  protobuf field numbers are reverse-engineered and unversioned; the corpus is one
  day, one model — field stability across models is untested.

**Statusline seam — verified live.** Capture method: a temporary statusline command
(`telltale-capture.cmd`) appending each stdin payload to a file; six payloads captured
across one interactive session. Confirmed against the documented schema: `product:
"antigravity"` on every payload (the routing marker); model with display_name/effort;
full context accounting (`context_window_size` 1,048,576 observed, `used_percentage`,
per-request `current_usage` incl. cache reads); **named weekly quota buckets**
(`gemini-weekly`, `3p-weekly` on the Starter tier) each carrying `remaining_fraction`,
`reset_time`, `reset_in_seconds`; `agent_state` observed transitioning `tool_use` →
`idle`; `tool_confirmation_pending` observed `true` while a permission prompt was on
screen. Documented but not yet observed live (the capture session ran outside a repo):
`vcs`, `artifact_count`, `task_count`, `execution_mode` — the parser carries them as
optional; the branch segment's live confirmation is the itemized remainder here.

**Adapter built, 2026-08-02 (`internal/adapter/antigravity`).** What the
adapter took from this survey and what it left:

- **Took:** Model (`#1.#21` display name, `#1.#19` id fallback), Workspace (the
  trajectory blob's URI, converted to a native path), LastActivity (the Q8 fold over the
  transcript's newest `created_at` and the mtimes of the transcript, the database and its
  sidecar — the sidecar because on a live conversation that is the file being written).
  All three REPORTED; nothing is derived. Name was sourced from the conversation id at
  first shipping (shortened for the grid, the only label on disk that is not somebody's
  prompt) — **revised 2026-08-12 (ruled):** the HUD row's Name showed the id prefix
  instead of the workspace basename every other vendor with no on-disk title falls back
  to, so Name moved from REPORTED to `CapNone` and the adapter no longer writes it; the
  HUD sources the row's label itself, the same fallback a Gemini row takes with no
  summary of its own.
- **Left:** context % (the numerator is measured and the denominator is not — the token
  totals are display-only extras instead), cost, quota, liveness, subagents and (as of
  the 2026-08-12 revision) name, all `CapNone` for the reasons itemized above. The
  liveness and subagent deferrals are pending live observation, not pending effort.
- **The identity is asserted, not assumed:** every generation must satisfy `thinking +
  answer == output` before its numbers count. A generation that fails contributes
  nothing and the row says a self-check failed. Across the live corpus on the day the
  adapter landed the identity held **16/16** (one more generation than this survey's 15,
  a conversation having advanced in between).
- **Zero dependencies added.** The `.db` and `-wal` bytes are read by `internal/sqlite`,
  a read-only reader written for this seam: header, `sqlite_master`, table b-trees,
  overflow chains, and a WAL overlay applying SQLite's own recovery semantics. The
  rejected alternative and the reasoning are decisions/006.
- **Live verification, same day:** all five local conversations discovered and read;
  model `Gemini 3.6 Flash (High)` on every one; three workspaces resolved to
  `C:\Users\sanle` and two absent (those conversations genuinely carry no URI — absence,
  not degradation); a real 284 KiB `-wal` parsed and accepted with no diagnostic; every
  session passed `Validate` with an empty degraded set.

**Fidelity notes:** the docs' storage path (`antigravity/`) and the real one
(`antigravity-cli/`) disagree — the re-survey confirmed the real tree (the docs path
has never existed on this machine); `model.id` equals the display
string ("Gemini 3.6 Flash (High)"), not a machine id; the payload carries the signed-in
email and plan tier, so real captures are PII and never enter `testdata/`.

### 3.9 Cursor (Composer) seam — surveyed live 2026-08-02; the store is open, and it holds credentials

**Environment:** Cursor 3.14.7, Windows. Closed source, and the store format is
**undocumented and unversioned** — there is no changelog for the 3.12–3.14 line and no
schema anywhere. So this is the Claude Code / Antigravity method again: a read-only live
survey, every field cross-checked, nothing claimed that was not observed.

**Store inventory.** One SQLite database backs every Composer session:

```
%APPDATA%\Cursor\User\
  globalStorage\state.vscdb          9.3 MB at survey time
  globalStorage\state.vscdb-wal      4.6 MB — LIVE, Cursor was running
  workspaceStorage\<workspace-id>\workspace.json
  workspaceStorage\<workspace-id>\state.vscdb        4096 bytes
  workspaceStorage\<workspace-id>\state.vscdb-wal    300–540 KB
```

Three tables. `composerHeaders(composerId, workspaceId, createdAt, lastUpdatedAt,
isArchived, isSubagent, recency, checkpointAt, value)` is one row per session, `value`
being a JSON blob. `cursorDiskKV(key, value)` is key/value: `composerData:<uuid>` holds
per-session state, `bubbleId:*` and `ofsContent:*` hold the message payloads.
**`ItemTable(key, value)` holds the credentials** — see below.

**Field map.**

| Field | Verdict | Source, and what was measured |
|---|---|---|
| name | **MEASURED** | `composerHeaders.value.name` — a vendor-GENERATED session title ("Multi-vendor orchestration"), same class as the Claude summaries the HUD already shows. Absent on a session the vendor has not titled yet; the composerId's first eight characters then. |
| model | **MEASURED** | `composerData:<id>` → `modelConfig.modelName`. Observed `composer-2.5`, `grok-4.5`, `gpt-5.6-sol`, and the literal `default`. One string, no display name beside it. |
| workspace | **MEASURED** | `composerHeaders.workspaceId` → `workspaceStorage/<id>/workspace.json` → `.folder`, a `file:///c%3A/...` URI (lower-case drive letter, percent-encoded colon). `value.workspaceIdentifier.uri.fsPath` carries the same path and confirms it. |
| context % | **MEASURED** | `composerData.contextUsagePercent`, a float the vendor persists: 37.05, 29.008, 12.38, 10.99 observed, alongside raw `contextTokensUsed`/`contextTokenLimit` (94854/256000, 24763/200000, 44k/1M). The header row's `value.contextUsagePercent` mirrors it and agreed **exactly** on all four rows carrying both. |
| cost | **ABSENT** | `usageData` was `{}` in all 8 blobs, and `tokenCount.inputTokens`/`outputTokens` read **0 in all 310 message rows**. The schema is present and never populated. |
| quota | **ABSENT** | No consumption record on disk anywhere. What IS there is plan ENTITLEMENT (`credit_dollars: 25`, `included_usage_dollars: 40`) — what the plan grants, not what has been spent. |
| last_activity | **MEASURED** | `lastUpdatedAt` / `recency` / `checkpointAt`, epoch **milliseconds**. Not every row has all three (`lastUpdatedAt` was NULL on 4 of 9). |
| liveness | **PARTIAL, never observed in flight** | `composerData.status` (`completed`, `aborted`, `none`), `generatingBubbleIds`, `hasBlockingPendingActions`. All read terminal or empty across the corpus; no session was ever sampled mid-generation. |
| subagents | **ABSENT (structural only)** | `isSubagent` 0, `numSubComposers` 0, `subComposerIds` `[]` on every row. The fields exist; the observation does not. |

**Amended 2026-08-11: a larger survey HARDENED the `cost` row and CORRECTED the reason
behind the `quota` row. The corrected reading lived only in §7.16 until now.** The two
rows above rest on the 2026-08-02 corpus — 8 `composerData` blobs and 310 message rows.
A re-verification on 2026-08-08 (§7.16) read a bigger one and found the same thing
harder: `usageData` was `{}` in **19 of 19** blobs, `tokenCount` was zero in **1,622 of
1,622** message rows, and 78 `turn_ended` records and 51 transcripts carried status and
no numbers. `cost` **ABSENT** therefore gets stronger, not weaker, and the original
counts above stay as the smaller measurement they were.

The `quota` row's VERDICT stands and its REASON does not. The row reads `credit_dollars:
25` and `included_usage_dollars: 40` as plan ENTITLEMENT — what the plan grants. The
2026-08-08 survey measured those constants as **Statsig experiment values stamped
`is_user_in_experiment:false`**, and they were the only account figures anywhere on that
disk. So they describe an experiment this account is not in, not what this account was
granted. §7.17's absence table already renders Cursor as `no quota anywhere · its store
holds experiment values, not usage` on exactly that measurement.

What follows from both rows is the same: nothing about consumption reaches the store as
a byproduct of a turn, so the number is FETCHED rather than found. **Cursor Hooks is the
real token seam** — the vendor's own documented `afterAgentResponse` step hands a command
hook the turn's token counts on stdin, and §7.16 is the record of it, including why print
mode's derived `inputTokens` was refused.

**Most rows are not sessions.** 9 header rows, of which **5** were: the empty-state
draft (`composerId` literally `empty-state-draft`, `isDraft` true), two pre-created
composers a new window makes before anyone types (no title, no `lastUpdatedAt`), and two
archived threads. Filtering on `isDraft`, `isArchived`, `isSubagent`,
`workspaceId == "empty-window"` and the draft sentinel left 4 real sessions, which is
what the HUD shows.

**Build cautions, recorded before any adapter work:**

- **The WAL is where the data is.** This is stronger than the usual "read the sidecar
  too" (§3.8): every workspace-level `state.vscdb` was **4096 bytes — one empty page —
  with 300–540 KB in its `-wal`**. A reader that opens only the `.db` there does not get
  stale data, it gets an empty database. The global store was 9.3 MB with a 4.6 MB live
  sidecar. Read both as bytes; never open or lock the file Cursor owns.
- **THE STORE HOLDS LIVE CREDENTIALS.** `ItemTable` carries `cursorAuth/accessToken`,
  `cursorAuth/refreshToken`, `mcpOAuth.secret.*` and git-IPC auth tokens;
  `composerData` blobs carry `blobEncryptionKey` and
  `speculativeSummarizationEncryptionKey`. This is the first vendor where "read the
  store" and "read the user's tokens" are the same sentence unless an adapter is
  explicit about what it will not touch. The allowlist is decisions/007 and it is
  asserted by a test, not promised in a comment.
- **`ItemTable['composer.composerHeaders']` is a legacy JSON mirror and it is STALE.**
  At survey time it named **3** composers to the table's **9**, and all three were rows
  the filter drops — so an adapter reading the mirror would report *zero* sessions on a
  machine running four. Read the table. (The mirror is in `ItemTable` anyway, so the
  credential rule forbids it independently.)
- **Timestamps are mixed.** The header columns are epoch milliseconds; ISO-8601 UTC
  strings live in the same store at `composerData.fullConversationHeadersOnly[].createdAt`
  (per-message, structural). That path is a finer-grained activity signal than
  `lastUpdatedAt` and the adapter deliberately does **not** read it: it is outside the
  allowlist, and widening the allowlist for precision is exactly the trade this seam
  should not make. The timestamp reader accepts both encodings anyway, because an
  unversioned INTEGER column is not promised to stay one.
- **One store, many sessions**, so the store's file mtime dates the STORE. Folding it
  into `last_activity` — which is what §6 Q8 prescribes for every other vendor — would
  mark every Cursor row live whenever Cursor wrote anything, forever. The Q8 fold runs
  over the per-row timestamps only, and degrades when none is readable. This is the one
  deliberate departure from the Q8 shape, and the reason is that the shape assumes one
  file per session.
- **Version fragility.** No changelog, no schema version in the file, no documentation.
  The adapter addresses columns by NAME (read out of the CREATE statement `sqlite_master`
  stores) rather than by position, and a store missing `composerHeaders` or one of the
  columns the field map needs is reported **unreadable with the reason** rather than as
  zero sessions — a wrong "your agents are idle" is worse than a visible "I cannot read
  this".

**The needs-input seam, for later.** Cursor's *documented* surface is Hooks
(cursor.com/docs/hooks): the base payload carries `conversation_id`, `model`,
`workspace_roots` and `transcript_path`, and `preCompact` carries context numbers. That
is a supported, versioned contract and it is where a liveness/needs-input signal should
come from — not from reverse-engineering `status` out of the store. Recorded as the
watch item; §8 carries it. Separately, the `cursor-agent` CLI keeps its own store, which
is **not installed on this machine** and therefore an unverified surface, out of scope.

**Adapter built, 2026-08-02 (`internal/adapter/cursor`).**
Name, model, workspace and last_activity REPORTED; context % declared DERIVED and marked
per read only when the adapter computed it; cost, quota, liveness and subagents
`CapNone`. `internal/sqlite` gained two additions rather than being worked around:
`Columns` (split the column list out of the stored CREATE statement, so columns are
addressed by name) and `Rows` (stream a table to a callback that can stop, so filtering a
key/value table by prefix retains nothing).

**Live verification, same day:** 5 sessions discovered and read against the real store
**with Cursor running** and a 4.6 MB live sidecar; workspaces resolved to `agent-ops` and
`faithfulness-judge`; models `grok-4.5`, `composer-2.5`, `gpt-5.6-sol`; context
percentages 4.42 / 12.93 / 37.05 all vendor-REPORTED, none marked derived; one session
with no `composerData` row rendering an absent model rather than an empty one; every
session passed `Validate` with an empty degraded set; no cost, no quota, no sub-agent
count anywhere. First `Discover` 78 ms, second 0 ms (the store had not moved). What this
does **not** cover, itemized: no in-flight session was sampled, no fan-out was observed,
the derived-percentage path did not fire on live data (no real session was missing
`contextUsagePercent` while carrying raw counts), and the corpus is one machine, one day,
one Cursor version.

### 3.9a Grok CLI seam — surveyed live 2026-08-09; the first vendor that writes money down

**Environment:** grok 1.0.0 (3cd0d0cbce), Windows 11, signed in against grok.com, model
`grok-4.5`. Closed source and undocumented — `~/.grok/README.md` is user-facing product
prose, not a format spec — so this is the Claude Code / Cursor method again: a read-only
live survey over the real store, every claim measured, nothing carried over from
`--help`. **Numbered against the sections above rather than after §3.10** so the survey
sits with the surveys and the inventory keeps summarizing everything before it.

The council seat (§9.39) already drives this vendor, and the HUD could not see it at all.
This closes that; the seat's parser and this adapter now read the same wire from two
sides, which is what let the cost unit below be pinned rather than assumed.

**Store inventory.** Sessions are DIRECTORIES, not files:

```
~/.grok/
  sessions/
    session_search.sqlite                     a FILE at the root — full-text index
    <percent-encoded-cwd>/
      prompt_history.jsonl                    a FILE at workspace level
      <uuid>/                                 ONE SESSION
        summary.json      signals.json        chat_history.jsonl   events.jsonl
        updates.jsonl     prompt_context.json system_prompt.txt    resources_state.json
        rewind_points.jsonl  announcement_state.json  terminal/    *.lock
  active_sessions.json   auth.json   worktrees.db   models_cache.json   logs/   memtrace/
```

**The variance is a finding, not noise.** Across 30 session directories in 8 workspaces,
only five files were present in all 30: `summary.json`, `chat_history.jsonl`,
`events.jsonl`, `prompt_context.json`, `system_prompt.txt`. `updates.jsonl` was in 29,
`signals.json` in 23, `resources_state.json` in 13, a `terminal/` subdirectory in 5. The
adapter therefore sources every required field from `summary.json` — the invariant — and
treats a field that lives anywhere else as *absent now* when its file is missing, which is
a first-class state here and not a failure.

**Field map.**

| Field | Verdict | Source, and what was measured |
|---|---|---|
| name | **MEASURED** | `summary.json` → `generated_title` (17 of 30), falling back to `session_summary`, which held the identical string on every session carrying both. A headless `--single` run has `session_summary: ""` and **no `generated_title` key at all** — verified by running one — so an unnamed grok row is absence, not a failed read. |
| model | **MEASURED** | `summary.json` → `current_model_id`. `grok-4.5` on 30 of 30. Note the per-model usage blocks key off `grok-4.5-build` instead; the adapter reports the id the vendor puts in the session's own model field, not the billing variant. |
| workspace | **MEASURED** | `summary.json` → `info.cwd`, absolute and native (`C:\Users\sanle\code\telltale`), on 30 of 30. The parent directory name carries the same path percent-encoded and is the **fallback** — see the round-trip note below. |
| context % | **MEASURED** | `signals.json` → `contextWindowUsage`, an integer percentage the vendor computes, beside the raw `contextTokensUsed` / `contextWindowTokens` (500000 on every session). It TRUNCATES: 39656/500000 = 7.93 is written `7`, 22675/500000 = 4.535 is written `4`. The adapter reports the vendor's integer and does not recompute — grok is the second vendor after Cursor with no assumed denominator, and the more precise float would be a number the vendor never said. |
| cost | **PER-TURN ONLY, so the field stays `CapNone`** | `updates.jsonl` → each `turn_completed` record's `usage.costUsdTicks`. The unit is measured twice over (below). It is **not cumulative**: one session's three turns read 455412000, 820464000, 747416000 ticks, the third smaller than the second. `"[a-z_]*cost[a-z_]*"` over every `.json`/`.jsonl` in the store matched `costUsdTicks` and **nothing else** — no session total exists anywhere. Summing needs every turn record and `updates.jsonl` reached **818 KB** in one session, past any bounded tail; a tail-window sum is a lower bound, and a lower bound in a column headed COST is a derived number wearing a read one's clothes. The **last turn's** cost is carried as a labeled Extra instead. |
| quota | **ABSENT** | `"[a-z_]*(rate\|limit\|quota)[a-z_]*"` over the whole store returned only tool-configuration keys (`output_byte_limit`, `head_limit`). No window, no ordinal, no reset time reaches disk. That is a statement about the *disk*; the network half of the same question is measured and closed separately below. |
| last_activity | **MEASURED** | `summary.json` → `last_active_at` (29 of 30) then `updated_at` (30 of 30) then `created_at`, folded with the file's mtime per §6 Q8. `summary.json` is rewritten every turn, which is also why it — not the session DIRECTORY, whose mtime moves only when an entry is added — is the freshness hint `Discover` returns. |
| liveness | **ABSENT, and this one was probed rather than reasoned about** | See below. |
| sub-agent count | **ABSENT** | grok ships a `spawn_subagent` tool (it is in the tool list every headless run prints), and `"subagent[A-Za-z_]*"` matched nothing on disk outside the system prompt's own description of it. No nest, no count, no parent link. Same ruling as Codex (§3.3): declaring the field and emitting zero would assert something the format cannot check. |

**The cost unit, pinned twice.** `costUsdTicks` is fixed-point USD at **1e10 ticks to the
dollar**, and neither half of that was inferred from the name. First, grok's headless wire
prints both forms of the same number on its `end` event, and three live runs on this box
on 2026-08-09 gave `0.0306488 / 306488000`, `0.0315248 / 315248000` and
`0.0382104 / 382104000` — exactly 1e10, three times. Second, the disk field is spelled
differently (`costUsdTicks` vs the wire's `total_cost_usd_ticks`), so it had to be shown to
be the same quantity and not merely a similarly named one: those three runs' on-disk values
were read back and matched the wire's tick counts **value for value**. Without the second
step this would be a plausible unit rather than a measured one.

**`active_sessions.json` claims liveness and does not deliver it — measured.** With 30
sessions in the store the file held the two bytes `[]`. It still held `[]` **while a
headless turn was mid-flight**, sampled with `grok.exe` confirmed running by PID and with
the file's own mtime freshly stamped by that run — so the vendor had written the file and
written nothing in it. A registry that is empty during a live session cannot tell "nothing
is running" from "the thing running is not the kind it tracks", and §4a.4 already names
process-existence as the one case where an adapter can lie to the HUD undetectably. Not
read.

`events.jsonl` carries the other tempting signal — `phase_changed` (1765 of them in one
session, spelling `waiting_for_model`, `streaming_reasoning`, `streaming_text`,
`tool_execution`, `permission_prompt`), `turn_started` / `turn_ended` with an `outcome`,
and a `permission_requested` that is a genuine needs-input state. It is left unread in v1
for the reason the corpus itself demonstrates: the newest session ends on an **unresolved
`permission_requested`** written minutes before grok exited, so "the last event is a
prompt" and "a dead session was killed at a prompt" are the same bytes. A hint that stays
true forever after the process is gone is worse than no hint. This is the strongest
needs-input seam any vendor has offered so far and it is recorded here as the watch item,
not spent.

**The percent-encoding round-trips, and that is why this adapter may decode it.**
`C%3A%5CUsers%5Csanle%5Ccode%5Ctelltale` is `C:\Users\sanle\code\telltale`: `:` as `%3A`,
`\` as `%5C`, with letters, digits and a literal `-` passing through unescaped. Every one
of the 8 workspace directory names decoded to exactly the `info.cwd` its sessions recorded,
drive-letter case included. That is the opposite of §3.1's ruling for Claude Code, whose
project slug maps both `\` and a literal `-` onto `-` and is therefore lossy — decoding
*that* would invent a path. Grok's encoding is injective, so decoding it invents nothing,
and the adapter uses it as the workspace fallback. It stays a fallback: the vendor's own
record of its cwd outranks a key we reconstructed.

**What is deliberately not opened, and why it is stated rather than assumed:**

- **`~/.grok/auth.json`** holds the OAuth token. The adapter resolves no path outside the
  sessions tree at all.
- **`sessions/session_search.sqlite`** is an FTS5 index whose `session_docs` table carries
  `(session_id, cwd, updated_at, title, content, content_hash)` — **`content` is transcript
  text**. It would answer "what is this session about" in one query, and that is exactly the
  trade this repo does not make: `summary.json` already has the vendor's own label.
- **`prompt_context.json`** inlines the user's `CLAUDE.md`/`AGENTS.md` verbatim (39 KB on
  one session). Same rule. A test plants a marker in both files and asserts nothing carrying
  it reaches any displayable field, the way §3.9's credential allowlist is enforced.
- **The `.lock` sidecars** beside `summary.json`, `chat_history.jsonl`, `updates.jsonl` and
  `rewind_points.jsonl` are never opened and never created. The gauges read.

**The quota question has a network half, and it is closed three times over — probed
2026-08-09**, the same local day as the survey above (the HTTP `Date` headers quoted below
read 2026-08-10 UTC; this box runs UTC−4). The table's `quota` row says nothing reaches
*disk*, which invites the obvious follow-up: the CLI talks to a server, so ask the server. `~/.grok/README.md` ("Using
auth.json for API Access") even documents the call. This block exists so the next person to
ask stops here instead of re-deriving it. Three findings, then three rules.

*The documented recipe does not run on this build.* Its `jq` path
`."https://accounts.x.ai/sign-in".key` matches nothing in this box's `auth.json`, whose one
entry is keyed `https://auth.x.ai::<uuid>` (`auth_mode: "oidc"`, a six-hour `expires_at`);
sent as written it returns **401**, `www-authenticate: … reason=no auth context`. And
`POST /v1/chat/completions` gates on a header the recipe never mentions — omit it and the
proxy answers **426 Upgrade Required**, body `"Your Grok CLI version (none) is outdated"`.
The header is `x-grok-client-version`, read out of `grok.exe`'s string table beside
`x-grok-client-identifier`, `x-grok-client-mode`, `x-grok-session-id` and the documented
`x-grok-model-override`. The README is product prose here too, exactly as this section's
header warns.

*The free half of the question is answered, and the answer is no.* `GET /v1/models` — the
same request whose result the CLI already caches to `models_cache.json`, so it bills
nothing — returns **200** carrying `etag`, `strict-transport-security`, `cf-cache-status`,
`CF-RAY`, `alt-svc` and `Server: cloudflare`, and **not one `x-ratelimit-*` header of any
kind**. The one proxy response this vendor already persists has no quota in it.

*The billed half is `not checked`, which per §9.42 carries a reason and never a value.*
Whether `POST /v1/chat/completions` returns those headers was **not measured**: it is the
only probe here that spends a turn, and it stopped at this machine's own tool-permission
boundary rather than being run. Two attempts to get it from the vendor's own logging failed
for a reason worth recording, because it looks like evidence and is not: `~/.grok/logs/
unified.jsonl` is **not an HTTP log** — every record's `src` is `shell` or `grok-pager` — so
its silence about rate-limit headers was never evidence about the wire, and a live
`grok -p` run driven with `--debug-file` produced no file at all.

*The disk sweep was re-run with a case gap closed.* The original survey's
`"[a-z_]*(rate|limit|quota)[a-z_]*"` is **lowercase-only** and would have missed a camelCase
`rateLimit` — which matters precisely here, because this is the vendor that writes
`contextWindowUsage` and `contextTokensUsed`, so camelCase is its house style and the
original regex had a live blind spot. Re-swept as
`"[A-Za-z_-]*([Rr]ate[Ll]imit|[Qq]uota|[Rr]emaining)[A-Za-z_-]*"` over the whole sessions
store, including a session directory created by a fresh live turn: the sole hit is
`agents_remaining` (17 occurrences), a **sub-agent budget**, not an account one. The
`quota: ABSENT` verdict survives the stronger sweep.

*And the vendor's own monitoring surface settles it.* `docs/user-guide/24-monitoring-usage.md`
documents an external OpenTelemetry stream — the one place grok is *designed* to report what
an account is doing — and its attribute keys are **a closed enum**, with an export-time
validator that drops any record carrying a key outside it. What it carries is
`grok_code.token.usage` (`input` / `output` / `reasoning` / `cache_read`, by model) and a
`grok_code.api_request` event with the same four counts. What it carries **nowhere** is a
window, a reset, a remaining percentage or a limit of any kind; `subscription tier` sits on
its explicit never-exported list, and the doc states outright that there is no cost metric
("join `grok_code.token.usage` with your own price sheet"). So the absence is not an
oversight in a session file. The vendor built a schema for exactly this question, put
**spend** in it, and put **no quota in it at all** — which is §7.15 versus §7.16 in the
vendor's own hand: a count with none, never a reading against a limit.

That stream is also the honest answer to "then what *could* be measured". If a grok **spend**
row is ever wanted it is the seam — exported to a local collector whose output telltale would
read as a *file*, leaving §4a.5's no-network-calls contract intact, since the push is grok's
and not ours. It is the cursor relay's shape (§7.16), and it is emphatically not a quota
window. Cost: a double opt-in, an endpoint, and a collector to run; the `console` exporter is
no shortcut because the doc says it is suppressed in the `agent` and `headless` entrypoints.
Recorded as the seam, not spent.

**The seam was spent on 2026-08-10 — `telltale otel grok` is the collector (§7.16a).**
Before it was built, the doc's schema table was checked against the wire the way this
section's header demands: a dump collector on 127.0.0.1:4318, and a live headless
`grok -p "hi"` from grok 1.0.0 (3cd0d0cbce) with the double opt-in set. What arrived,
versus what the doc says:

- Transport as documented: OTLP http/protobuf POSTs to `/v1/logs` and `/v1/metrics`,
  `Content-Type: application/x-protobuf`, uncompressed, from `OTel-OTLP-Exporter-Rust/0.32.0`.
  The batches flushed before the headless process exited, at the default export intervals —
  a short `-p` run loses nothing. The fleet-policy startup suppression the doc warns about
  was not observed to delay anything on this signed-in box.
- `grok_code.api_request` events carry `input_tokens`, `output_tokens`, `reasoning_tokens`
  and `cache_read_tokens` as int attributes — all four on one record — beside `model`,
  `duration_ms`, `stop_reason`, `session.id`, a per-session monotonic `event.sequence`,
  `user.id` and `team.id`. The event name arrives in the LogRecord's `event_name` field,
  not as an attribute.
- The `grok_code.token.usage` metric (delta temporality, by `model` and `type`) carried
  **the same four counts value-for-value** as the same turn's api_request event:
  20323/56/42/2560 on both sides of one capture. One number, two envelopes.
- `turn_completed` carries outcome and duration and **no token counts**, as the table says.
- One departure from the doc's letter: `OTEL_METRICS_INCLUDE_SESSION_ID` defaults on, but
  the token.usage data points carried no `session.id` — only `session.count`'s did. Nothing
  here reads metrics, so nothing turns on it; recorded because the doc says otherwise.

The quota verdict above is unchanged by any of this: nothing on the stream carries a
window, a reset or a limit. What was added is a **spend count** (§7.16's vocabulary — a
count with no denominator), accumulated per api_request event into
`~/.telltale/usage/grok.json`, display held. §7.16a is the design record.

A measurement of the completions headers would not move the verdict, because three separate
rules already close this and none of them turns on whether the number exists:

1. it needs `auth.json`, and this adapter resolves no path outside the sessions tree (above);
2. it needs a network call, and §4a.5's adapter contract is explicit that implementations
   "must not write to vendor state, and must not make network calls or read credentials";
3. §9.42 draws the probing line at **cost and side effect** — `doctor` may run `--version`
   precisely because it starts no turn and bills nothing. A quota probe against the
   completions endpoint would spend from the very pool it is trying to read.

And the quantity would be the wrong one regardless. `x-ratelimit-remaining-requests`,
`-remaining-tokens` and `x-ratelimit-reset-requests` are documented for **`api.x.ai`**, the
metered developer API. This CLI rides `cli-chat-proxy.grok.com` on an OIDC session token
against a **SuperGrok subscription**, whose binding limit is a shared weekly pool surfaced
only in grok.com's own Settings → Usage. An RPS/TPM header is not the window the usage pane
means by quota: §4a.3's window carries a *length* and a `ResetsAt`, and a per-second request
cap has neither. Relabelling one as the other would be a duration claim with no source —
the exact move §4a.3 already forbids — and would land a real number on screen answering a
question nobody asked.

**Two smaller findings worth writing down.** One session's `summary.json` carries
`"sandbox_profile": "bogus-profile-xyz"` — the invalid profile §9.39 fed the CLI to prove
`--sandbox` validates nothing. grok not only accepted it, it **persisted it**, which is why
that key is not rendered as an Extra: it would put an unvalidated word on screen. And
`summary.json` carries a git block (`git_root_dir`, `git_remotes`, `head_commit`,
`head_branch`) on exactly the one session whose cwd was a git repo — a branch column is
available for later, and is out of scope here.

**The frame, generated by the build** (`internal/hud/testdata/golden/grok-row.txt`, at
120 columns; both rows are synthesized). The COST column is not narrow here, it is
ABSENT — the vendor writes dollars and no row claims a session total. The first row's
bar carries no estimate marker because the percentage was read rather than derived; the
second is a headless run with no title and no `signals.json`, so its label falls back to
the workspace and its CONTEXT is an em dash rather than a zero:

```
 telltale  │  2 sessions  │  grok 2
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
 ● GR │ Adapter Field Map Review  C:\src\code                                 grok-4.5       ▊───────────     7% │  20s
 ◐ GR │ example-app  C:\src\code                                              grok-4.5                         — │   6m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

**Adapter built, same day (`internal/adapter/grok`).** Name, model, workspace, context %
and last_activity REPORTED; cost, quota, liveness and sub-agent count `CapNone`; the
derived set deliberately EMPTY. `summary.json` and `signals.json` are read whole under a
64 KB cap each (largest observed: 918 and 1538 bytes) and a file past its cap degrades the
fields it feeds rather than being slurped; `updates.jsonl` is tail-read like every other
JSONL here.

**Live verification, same day** (`go test ./internal/adapter/grok -tags=live -run
TestLiveGrokStore`, which drives the HUD's own `Scan` and `Render` over the real store and
is excluded from CI because CI has no store to read): **31 sessions discovered and read**,
**31 of 31** sourcing both a model and a workspace, 18 titled, 24 carrying a context
reading, 24 carrying a turn cost. Every session passed `Validate`; not one produced a cost,
a quota window, a sub-agent count or a liveness hint. The frame showed the zero-vs-absent
distinction doing real work: the 7 sessions with no `signals.json` rendered an em dash in
CONTEXT while their neighbours rendered an unmarked bar, on the same screen. First
`Discover` plus 31 `Read`s completed in 80 ms.

What this does **not** cover, itemized: no session was sampled while a turn was streaming
(the `active_sessions.json` probe ran against a headless turn, not against a HUD scan); no
interactive TUI session was running during a scan, so nothing exercised the NTFS-deferred
mtime the Q8 fold exists for; the summary-past-its-cap path has never fired on real data;
the corpus is one machine, one day, one grok version; and `active_sessions.json` has never
been observed non-empty **at all**, so "it does not track headless sessions" is the most
this survey can say about what it would otherwise contain.

### 3.10 The canary set — what each adapter actually watches

Every survey above pins an adapter to a private, unversioned on-disk format. §7 records how
drift *renders*; this is the other half, and without it the next person re-verifying a vendor
cannot know what was being watched. `grep -n canary docs/design.md` used to return nothing,
which was the whole of the gap.

A **canary** is a structural fact the survey established is present on *every* well-formed unit
of that vendor's corpus. A read that examined units and found no canary is reading a corpus that
has moved, and says so. `internal/adapter/drift` holds the mechanism; this is the inventory.

| adapter | verified against | canary | fields it feeds |
|---|---|---|---|
| Claude Code | `Claude Code 2.1.219` | `sessionId` — on every JSONL record | name, model, workspace |
| Codex CLI | `codex-cli 0.146.0` | `envelope type` — on every rollout record | model, workspace, quota, context % |
| | | `session_meta record` — the FIRST record of every rollout | workspace |
| Gemini CLI | `gemini-cli v0.53.1` | `metadata record` | name, subagents |
| Antigravity | `agy 1.1.9` | `gen_metadata table` | model |
| | | `trajectory_metadata_blob table` | workspace |
| Cursor | `Cursor 3.14.7` | `composerHeaders timestamp columns` | last activity |
| Grok CLI | `grok 1.0.4 (d846eb93d9)` | `summary.json info.id` — the identity envelope, on 30 of 30 sessions | name, model, workspace, last activity |

The middle column quotes each canary by the **name the adapter gives it**, not a paraphrase, so
the string in this table is the string in the code — which is what makes the guard tests below
able to check it at all.

**Grok's row moved to 1.0.4 on 2026-08-14, and the row's two halves were re-checked to different
depths.** The version came from re-measuring the seat after four patch bumps went unnoticed
(§9.39's 2026-08-14 amendment). What was re-read on disk is one session directory that 1.0.4
itself wrote: `info.id` is there, so is every other key `internal/adapter/grok` names, and a
rate/limit/quota sweep over it still matches nothing account-level. What was NOT re-run is the
30-session census the canary's own phrase quotes — that number is still the 2026-08-09 survey's,
and it is left saying so rather than quietly re-attributed to a build it was never counted on.

**One table rather than a paragraph in each of §3.1–3.9**, deliberately, and against the first
instinct that a canary is a survey finding belonging beside its own survey. It is — but the
question this answers is asked *across* adapters ("what is being watched, and where is the gap"),
and five copies of the same claim in five subsections is five places for it to drift out of step
with `internal/adapter`. The survey sections keep the evidence; this keeps the inventory.

**What the columns are not.** `verified against` is CONTEXT, never a trigger — a version
comparison would fire on every vendor release that did not move a byte, and a report nobody
reads is worse than none. `fields it feeds` is what degrades when that canary goes missing, which
is why a canary is the load-bearing subset of a schema fingerprint rather than the fingerprint:
a vendor ADDING a column costs this program nothing, because every reader here addresses columns
by name.

## 4. Adapter contract (v1)

One module per vendor implementing:

- `discover()` — find live/recent sessions from vendor-native data on disk
- `read(session)` — return the normalized session model (schema TBD, documented here)
- `capabilities()` — which normalized fields this vendor can actually source

The contract, the normalized schema, and a worked third-party example are documentation
deliverables of v1, not afterthoughts. The Go form is §4a.5 and the worked example is
§4a.7 — whose subject, Gemini CLI, became a real built-in adapter on 2026-08-02; the
example keeps its original sketch precisely because live verification overturned part
of it (see the §4a.7 postscript).

### JSONL framing rule (binding on every adapter)

Both vendors' on-disk sources are JSONL (Claude transcripts, `~/.codex/sessions`), and
both carry model-authored text. **A JSONL record is framed by the `\n` byte (0x0A) and
nothing else.** U+2028 (LINE SEPARATOR) and U+2029 (PARAGRAPH SEPARATOR) are legal
*unescaped* inside a JSON string value, so a reader that splits on "lines" in the
Unicode sense tears one record in two and both halves fail to parse — a HUD row that
silently loses sessions. Node's `readline` has exactly this bug; pi's
`packages/coding-agent/src/modes/rpc/jsonl.ts` hand-rolls a `\n`-only splitter to avoid
it, with the reasoning written down.

Go is structurally safer: `bufio.ScanLines`, `bufio.Reader.ReadBytes('\n')` and
`bytes`/`strings.Split` all match the 0x0A byte exactly, and the UTF-8 encodings of
U+2028 (`E2 80 A8`) and U+2029 (`E2 80 A9`) contain no 0x0A byte. So the rule for
adapters is: **split at the byte level, never adopt a dependency that does Unicode
line-breaking.** The property is pinned by tests in `internal/claude/stdin_test.go`
rather than assumed.

Two adjacent traps to avoid when the adapters land:

- `bufio.Scanner` caps a token at 64 KiB by default and then returns
  `bufio.ErrTooLong`. Transcript records routinely exceed that, and an ignored scanner
  error truncates the rest of the file — reading as "no more sessions". Use
  `bufio.Reader.ReadBytes('\n')`, or `Scanner` with an enlarged buffer, and **check
  `Err()`**.
- A trailing partial line (the vendor is still writing) is not a record. Hold it until
  its `\n` arrives; a half-record must degrade to `—`, never to a parsed-looking value.

**Audit record (2026-08-01):** the repo was swept for this hazard at the point the pi
harness surfaced it. At that time the only parse site was `claude.Parse`
(`internal/claude/stdin.go`), a streaming decode of a single JSON value from stdin with
no line splitting anywhere — not exposed, nothing to fix. This section exists so the
question is answered before the HUD adapters are written, not re-audited after.

**Implementation (added with the adapters):** the rule now has one tested home,
`internal/jsonl` — `Split`, `Scan`, `Head` and `Tail`. `Tail` additionally discards the
first fragment after a backward seek, because a bounded tail read lands mid-record and
parsing that fragment invents a record the vendor never wrote. Both adapters use it; no
adapter reads bytes on its own.

## 4a. The normalized session model

*(Answers §6 Q2. Implementation: `internal/model/session.go`, package `model`, stdlib
only — the statusline path must never link a TUI framework.)*

Adapters produce `model.Session`; the statusline and the HUD only read it. Nothing
downstream of an adapter knows a vendor's field names, units, or file formats.

### 4a.1 Absence is two different things

The honest-gauge rule (ADR-001) says a displayed value must come from vendor data. That
forces a distinction most schemas skip, because the HUD renders the two cases
differently:

| | what it is | how it is encoded | how it renders |
|---|---|---|---|
| **absent now** | the adapter can source this field, but there is no value for this session right now (Claude's `rate_limits` on an API-key login) | nil pointer **+** capability declared | `—` in the cell |
| **can't know** | the vendor exposes no such thing, ever | nil pointer **+** capability not declared | the column is dropped for that vendor |

So presence lives in two places on purpose: **the value** (a nil pointer, per session)
and **the capability** (a static declaration, per adapter). Neither alone is enough — a
column of dashes for a vendor that could never fill it is itself a small lie about what
was measured.

Every optional field is a pointer. There is no "unset" sentinel number anywhere in the
package: `0` always means the vendor said zero, and the zero `time.Time` is invalid input
(`Validate` rejects it) rather than a stand-in for "no timestamp".

### 4a.2 Fields

Required — an adapter that cannot produce these has no row to render:

| Go field | Type | Notes |
|---|---|---|
| `Vendor` | `VendorID` | stable lowercase id, matches the adapter package name; appears in config keys and fixtures |
| `ID` | `string` | opaque, unique within the vendor, stable for the session's life — the HUD matches rows across polls with `Vendor/ID` |
| `ObservedAt` | `time.Time` | when **this snapshot was read**, not when the session last did anything. The HUD marks rows whose snapshot has aged out because polling failed; showing an old snapshot as current is precisely the failure the honest-gauge rule exists to prevent |

Optional — each has a stable **field id** used by `Capabilities`, fixtures, and this doc:

| Field id | Go field | Type | Meaning |
|---|---|---|---|
| `name` | `Name` | `*string` | human label. Model-authored text: may contain U+2028/U+2029, so renderers must not assume one line (§4) |
| `model` | `Model` | `*Model` | `{ID, DisplayName}`; `Name()` falls back to the id, same rule as the statusline |
| `workspace` | `WorkspaceDir` | `*string` | absolute native-format path. `WorkspaceName()` gives the basename for display |
| `context_pct` | `ContextPercent` | `*Percent` | 0–100, as vendors report percentages. Convert once at the edge; nothing downstream rescales |
| `cost` | `Cost` | `*USD` | USD only. A vendor reporting another currency declares `CapNone` rather than converting at an unsourced rate |
| `quota` | `Quota` | `[]QuotaWindow` | labeled usage windows, see below |
| `last_activity` | `LastActivity` | `*time.Time` | last observable activity. An **input** to liveness, not a claim about it |
| `liveness` | `LivenessHint` | `*Liveness` | the adapter's own verdict; see 4a.4 for when you are allowed to set it |
| `subagents` | `Subagents` | `*int` | count of the session's recently-written sub-agent transcripts. **Zero is a measurement** (we looked and found none) and must survive as one; nil means the count could not be taken |

Three more per-snapshot annotations, all optional:

- `Derived FieldSet` — fields whose value in *this* snapshot the adapter computed rather
  than read (summing transcript token counts into a context percentage, say). Must be a
  subset of the adapter's declared `Capabilities.Derived`, and every marked field must
  actually carry a value. The HUD renders these with an estimate marker; ADR-001 requires
  inferred values be visibly marked, not silently mixed in with reported ones.
- `Degraded FieldSet` — fields the adapter tried to read and failed (a truncated JSONL
  record, an unparseable number). Degraded fields must be absent. **Degraded and
  plain-absent render identically as `—`**; the difference is diagnostic only, shown in
  the detail pane. If "we failed to read it" got its own gauge glyph it would start to
  read as data.
- `Diagnostics []string` — operator-facing notes explaining degradation. Never rendered
  as values, and (public repo) they describe structure — `"record 41 truncated"` — never
  transcript content.

`Extras []Extra` is the escape hatch for vendor-specific labeled strings, so an adapter
with something extra to show does not stuff it into a field that means something else.
Extras are display-only: no thresholds, no colors, no sorting, detail pane only. If an
extra deserves a gauge, it deserves a `Field` — propose one. *(v1.1: the detail pane
(§7.11) is that surface, and it is now the only place extras appear —
`TestDetailPaneIsTheOnlyPlaceExtrasAppear` asserts both halves. Both adapters populate
them: git branch, CLI version, Claude's context token count, Codex's plan and history
mode.)*

**Why `subagents` is a `Field` and not an Extra.** It fails the Extra test in both
directions: it is a *number* with an absent-versus-zero distinction the Extra type cannot
carry (an Extra is a string, and `""` would collapse "none running" into "could not
count"), and it renders as a gauge-adjacent mark in the grid rather than as a labelled
line in the pane. That is exactly the "if an extra deserves a gauge, it deserves a Field"
case, taken rather than dodged.

### 4a.3 Quota windows

Windows are a slice, not named fields, because the set is vendor-defined. Emit only the
windows your vendor actually has, in display order, shortest first. Presence works at two
levels and both are load-bearing:

- a window the vendor does not have is **absent from the slice**;
- a window that exists but has no usage figure yet is **present with a nil
  `UsedPercent`** and renders `—`. Never `0%`.

Each window carries `ID` (stable snake_case key, e.g. `five_hour`), `Label` (short
display string, ≤ 4 cells — the statusline is character-budgeted), `UsedPercent`, and
`ResetsAt`. A nil `ResetsAt` hides the countdown rather than guessing one. A window whose
*length* the vendor did not report gets a positional label (`1st`, `2nd`) from the
adapter: calling it "5h" on a guess would be a duration claim with no source.

### 4a.4 Liveness: who decides

**The HUD decides.** It classifies from `LastActivity` against one shared
`LivenessThresholds`, so every vendor's rows are judged by the same rule and are
comparable side by side. Defaults (`DefaultLivenessThresholds`):

| age since last activity | class |
|---|---|
| ≤ 2 min | `live` |
| ≤ 15 min | `idle` |
| > 15 min | `stale` |
| no timestamp, no hint | `unknown` → renders absent |

2 minutes because a working agent can go a long single turn without writing anything to
disk, and a boundary shorter than the longest quiet stretch of real work flaps mid-task.
15 minutes because past that a session is nearly always one the user walked away from, so
it sorts to the bottom instead of competing for attention. Both are defaults, not
constants: the HUD may expose them and the eval harness pins renders against explicit
values. A `LastActivity` in the future (file mtime vs. local clock skew) clamps to age
zero rather than going negative — and the adapters degrade it to absent before it gets
that far, so the clamp is a floor rather than a render path.

`unknown` is a real state and renders as absent — **never as `stale`**. "Stale" is a
claim; "we have no activity signal" is not.

**The adapter's only input is `LivenessHint`, and it wins when set.** Set it *only* from a
positive vendor signal the HUD cannot see:

- ✅ a turn-started / turn-ended event from a hook or notify stream;
- ✅ the vendor has recorded the session as ended → `LivenessStale`, even though
  `LastActivity` is seconds old (this is the strongest legitimate hint);
- ❌ anything computed from the age of `LastActivity` — that is the HUD's job, and
  duplicating it per-adapter makes vendors incomparable at different boundaries;
- ❌ "a process with that name is running" — that is evidence a process exists, not that
  the session is doing anything.

A hint the model cannot check is the one place an adapter can lie undetected, which is
why the bar for emitting one is a signal that actually separates working-now from
process-exists. **Neither v1 adapter emits one** (§3.1, §3.2).

### 4a.5 The adapter interface

```go
type Adapter interface {
	Vendor() VendorID
	Capabilities() Capabilities
	Discover(ctx context.Context) ([]SessionRef, error)
	Read(ctx context.Context, ref SessionRef) (*Session, error)
}
```

- **`Capabilities()`** is static: which normalized fields this vendor can source, and how.
  It must not vary with what a particular session happens to contain — that is what nil
  pointers are for. Callers may cache it.

  ```go
  type Capabilities struct {
      Reported FieldSet // read from vendor output verbatim (modulo unit conversion)
      Derived  FieldSet // computed by the adapter from something that isn't the value
  }
  ```

  The two sets are disjoint; a field in neither is `CapNone` — "can't know". Declaring a
  capability is a promise about the *source*, not about any given session.

- **`Discover()`** must stay cheap: directory listing and `stat`, no parsing. The HUD
  calls it every poll tick. It returns `SessionRef{Vendor, ID, Locator, LastActivity}` —
  `Locator` is vendor-private (a path, a pipe name), opaque to the HUD, never rendered
  (on a shared machine it can name another user's paths), and handed back to `Read`
  unchanged. `SessionRef.LastActivity` is a scheduling hint (typically an mtime) so the
  poll loop can skip unchanged sessions; the value the HUD *displays* comes from `Read`.

- **`Read()`** parses one session. **Partial failure is not an error**: a field you cannot
  parse is left nil, added to `Degraded`, and explained in `Diagnostics` — the row still
  renders with `—` in that cell. Return an error only when there is no session to report
  at all.

- Errors the HUD handles by showing *less*, not by showing a banner:
  `ErrVendorAbsent` (vendor not installed — the vendor disappears from the HUD entirely;
  a user without Codex should not stare at a Codex error forever) and `ErrSessionGone`
  (the session vanished between `Discover` and `Read` — the row drops silently). Any
  other `Read` error drops that one row and nothing else; the Codex adapter uses this for
  `ErrSubAgentThread`, a rollout that is a sub-agent's thread rather than a session and
  cannot be identified before parsing.

- Implementations must be safe for concurrent use (the HUD polls vendors in parallel),
  must not write to vendor state, and must not make network calls or read credentials.

- **JSONL adapters:** §4's framing rule is binding. Split on the `0x0A` byte only, use
  `bufio.Reader.ReadBytes('\n')` (or `Scanner` with an enlarged buffer) and **check
  `Err()`**, and treat a trailing partial line as not-yet-a-record. `internal/jsonl` is
  the shared implementation; use it rather than re-deriving it.

### 4a.6 The validation gate

`(*Session).Validate(caps)` is the machine-checkable form of the honest-gauge rule. It
rejects a session that:

- carries a value for a field the adapter declared unsupported;
- marks a field derived that was not declared derived, or marks a field derived that
  carries no value;
- reports a field as both present and degraded;
- has a percentage outside 0–100 (drop it and mark it degraded — a clamped value is
  invented data), a negative cost, or a zero `time.Time` used to mean "absent";
- has no `Vendor`, `ID`, or `ObservedAt`, or duplicate/unlabeled quota windows.

The eval harness runs it over every fixture; run it in your adapter's own tests too. It is
not on the render path.

### 4a.7 Worked example: adding a Gemini CLI adapter

*(2026-08-02: this example became real — `internal/adapter/gemini`, seam verification
in §3.7. The sketch below is kept AS WRITTEN, wrong guess included, because the gap
between it and the shipped adapter is the section's whole lesson; see the postscript.)*

**Step 0 — verify the seam before writing a line of code.** ADR-001's live-doc rule
applies to third-party adapters too: check the vendor's *current* docs for what it
actually writes to disk. This repo's own Codex plan was falsified on first check (no
statusline hook), and the sketch below is deliberately a *shape*, not a claim — the file
locations and field names are placeholders until you have verified them. A capability you
cannot point at a documented source for is `CapNone`.

**Step 1 — write the capability table first**, in your ADR or PR description. It is the
honest inventory of what your vendor can actually answer, and it is the thing reviewers
argue with. Illustrative shape:

| field id | capability | source |
|---|---|---|
| `name` | reported | session file header |
| `model` | reported | session file header |
| `workspace` | reported | session file header |
| `context_pct` | **derived** | token counts summed from records — not a vendor-reported percentage |
| `cost` | none | vendor exposes no cost |
| `quota` | none | vendor exposes no quota window |
| `last_activity` | reported | timestamp of the last record |
| `liveness` | none | no turn-start/turn-end signal to read |

**Step 2 — implement.**

```go
// Package gemini adapts Gemini CLI's on-disk session data to model.Session.
// Source paths and field names verified against <live doc URL> on <date>.
package gemini

const Vendor = model.VendorID("gemini")

type Adapter struct{ root string } // e.g. filepath.Join(home, ".gemini", "sessions")

func (a *Adapter) Vendor() model.VendorID { return Vendor }

// Capabilities: context_pct is DERIVED — we sum token counts ourselves, so the
// HUD marks it as an estimate. Cost, quota and liveness are absent from the
// vendor's data entirely and are declared nowhere: the HUD drops those columns
// for gemini rather than printing dashes it can never fill.
func (a *Adapter) Capabilities() model.Capabilities {
	return model.Capabilities{
		Reported: model.NewFieldSet(
			model.FieldName, model.FieldModel, model.FieldWorkspace,
			model.FieldLastActivity,
		),
		Derived: model.NewFieldSet(model.FieldContextPercent),
	}
}

// Discover stats the session directory only — no parsing on the poll path.
func (a *Adapter) Discover(ctx context.Context) ([]model.SessionRef, error) {
	entries, err := os.ReadDir(a.root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrVendorAbsent // not installed: hide the vendor
	}
	if err != nil {
		return nil, err
	}
	var refs []model.SessionRef
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue // racing the vendor's writer is normal, not fatal
		}
		refs = append(refs, model.SessionRef{
			Vendor:       Vendor,
			ID:           strings.TrimSuffix(e.Name(), ".jsonl"),
			Locator:      filepath.Join(a.root, e.Name()),
			LastActivity: model.TimePtr(info.ModTime()),
		})
	}
	return refs, nil
}

func (a *Adapter) Read(ctx context.Context, ref model.SessionRef) (*model.Session, error) {
	f, err := os.Open(ref.Locator)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, model.ErrSessionGone
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	s := &model.Session{Vendor: Vendor, ID: ref.ID, ObservedAt: time.Now()}

	// §4 framing rule, via the shared implementation: records are framed by
	// 0x0A and nothing else, there is no 64 KiB cap, and a trailing partial
	// line is not a record.
	err = jsonl.Scan(f, func(line []byte) error {
		var rec record
		if json.Unmarshal(line, &rec) != nil {
			// One bad record degrades the fields it fed, not the whole row.
			s.Degraded = s.Degraded.With(model.FieldContextPercent)
			s.Diagnostics = append(s.Diagnostics, "unparseable record skipped")
			return nil
		}
		apply(s, rec)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Derived, and declared as such: this is a computed estimate, and the HUD
	// renders it with an estimate marker rather than as a vendor-reported figure.
	if pct, ok := estimateContext(s); ok && !s.Degraded.Has(model.FieldContextPercent) {
		s.ContextPercent = model.PercentPtr(pct)
		s.Derived = s.Derived.With(model.FieldContextPercent)
	}
	// Cost and quota are never set: not declared, so not knowable.
	// LivenessHint is never set: no positive signal, so the HUD classifies by age.
	return s, nil
}
```

**Step 3 — fixtures and the gate.** Add synthesized fixtures per state — healthy, empty,
degraded (a truncated final record), and whatever your vendor's equivalent of "logged in
a way that hides quota" is — and assert `Validate` passes plus the exact render for each.
Fixtures are **synthesized**: fake session ids, fake text, fake paths, realistic in shape
only. This repo is public and real transcripts carry private material; no real session
content enters `testdata/`, ever.

**Step 4 — write down what you could not source**, in your adapter's package doc. A field
declared `CapNone` with a one-line reason is a finished answer. A field quietly filled
with a plausible number is the bug this whole schema exists to make hard.

**Postscript — what Step 0 did to this very sketch.** The sketch above, written before
anyone read the vendor's source, guessed `context_pct: derived` ("token counts summed
from records"). The source read (§3.7) falsified it: Gemini's per-message token counts
are real, but no context-window size reaches disk — the CLI's own percentage divides by
a static table compiled into its binary, which is exactly the assumed denominator
decisions/001 forbids. The shipped adapter declares `context_pct: CapNone` and carries
the token reading as a display-only extra instead. The hypothetical also missed the two
things only the source could reveal: message records are upserts (same id re-appended),
and the writer deletes non-resumable sessions on exit. If you skip Step 0, those two
become bugs; the wrong capability guess becomes a fabricated gauge.

## 5. Eval harness

Fixture-driven, in-repo, CI-gating (`.github/workflows/ci.yml` runs `go vet ./...` and
`go test ./...` on `windows-latest`, then smoke-tests the built binary against a
statusline fixture). What it asserts today:

| Layer | Package | What is pinned |
|---|---|---|
| Statusline renders | `internal/statusline` | every segment against five stdin fixtures, including the API-key login that must render no quota |
| Statusline (agy) | `internal/statusline` + `internal/antigravity` | four agy fixtures + inline cases: full render exact, confirm? outranking the state word, ctx 0% as a reading beside hidden quota, bucket-without-reading hides, unknown state verbatim, reset_time fallback, U+2028 in a string value, the product routing marker |
| Framing rule | `internal/jsonl` | 0x0A-only framing, a 300 KiB record surviving the `bufio.Scanner` cap, read errors surfacing, torn tails held back, a seek fragment discarded |
| Schema gate | `internal/model` | `Validate` over every rejection case, liveness boundaries, presence semantics |
| Claude adapter | `internal/adapter/claudecode` | discovery filters, the `<synthetic>` trap, the `input_tokens` trap, torn tail invisibility, torn-only session, future mtime, capability table |
| Codex adapter | `internal/adapter/codex` | envelope + internally-tagged event parsing, derived context, quota window presence, null `rate_limits`, sub-agent rejection, capability table |
| SQLite reader | `internal/sqlite` | record decoding across every storage class, the zero-width serial types, the overflow-page chain (25 KiB blob against a 4 KiB page), missing-table-is-absence, and the WAL overlay: a committed sidecar value winning, a corrupt frame ignored with a note, a bad header rejected whole, mismatched salts ignored, a torn tail never assembled |
| SQLite reader (v1.2) | `internal/sqlite` | `Rows` streaming the same rows as `Table` and stopping when the callback says so; `Columns` splitting a CREATE statement across every quoting style, a parenthesized type, a comma inside a default and a table-level constraint, and yielding nothing rather than a guess on a statement it cannot read |
| Antigravity adapter | `internal/adapter/antigravity` | discovery that ignores the stale summary index and the sidecars, WAL overlay changing the reported model, overflow-spanning generation blob, dedup on the response id rather than the constant conversation UUID, invariant violation dropping the tokens with a diagnostic, zero tokens as data, absent workspace as absence, missing transcript as a typed sentinel, unreadable database degrading rather than dropping the row, Q8 fold, future mtime, transcript content never reaching a field, capability table |
| Cursor adapter | `internal/adapter/cursor` | discovery dropping the five row shapes that are not sessions (draft sentinel, `value.isDraft`, archived, sub-agent, empty-window), a store whose main file is one empty page reading entirely out of its sidecar, the vendor's own percentage reported unmarked, a computed one marked, neither-present as absence, `default` rendered literally, unpopulated zeros never becoming a cost or a token reading, missing workspace mapping as absence and an unparseable one as degradation, mixed epoch-ms/ISO-8601 timestamps, future-skew, all-timestamps-unreadable degrading, the stale `ItemTable` mirror losing to the table, an unrecognized schema erroring rather than reporting zero, capability table, and the allowlist: three planted markers (prompt text, credentials, plan entitlements) reaching no field, extra or diagnostic |
| Gemini adapter | `internal/adapter/gemini` | fixed-depth discovery (legacy `.json` and nested sub-agent files excluded), upsert last-wins, `$set` summary/lastUpdated, registry workspace lookup (verbatim, corrupt-degrades, absent-is-absent), sub-agent nest counting, `kind:"subagent"` rejection, torn tail invisibility, future mtime, Q8 fold, capability table |
| Claude adapter (v1.1) | `internal/adapter/claudecode` | the sub-agent count: recency boundary, future-mtime exclusion, non-transcript neighbours ignored, absent sidecar as a measured zero |
| HUD renders | `internal/hud` | every golden frame byte-for-byte at 52/72/80/120 columns (count enforced by TestEveryGoldenIsClassified, not restated here — a literal drifted once already), the §7.4 gauge table, the estimate marker, threshold colours, frame width/height invariants |
| HUD behaviour | `internal/hud` | vendor status words from adapter errors, key handling, one-scan-in-flight, spinner lifecycle |
| HUD behaviour (v1.1) | `internal/hud` | esc unwinding one layer at a time, find mode swallowing the keyboard, selection carried by session key across a re-sort, the pane closing when its session ends |
| Burn arithmetic | `internal/hud` | the minimum basis, the four refusals, least-squares slope against injected series, rollover detection vs. `resets_at` jitter, sample throttling and eviction |
| Fixture legality | `internal/hud` | every session behind every golden passes `model.Validate` against its vendor's declared capabilities — a golden may not pin a render of a state the schema forbids |
| Doc/code sync | `internal/hud` | every render pasted into `docs/design.md` §7.3/§7.11–§7.14 still matches its golden, and every golden is either embedded or explicitly exempted |
| Picture/code sync | `internal/hud` | `README.md`'s hero picture is re-emitted from the `readme` golden and byte-equal to the committed file, with the characters read back out of the emitted markup and diffed against the render — plus no dollar sign anywhere, and the estimate marker surviving into the picture |
| Picture/code sync | `internal/council` | the hero picture `README.md` and `docs/council.md` both show is re-emitted from the `activity` golden with its all-blank rows dropped, byte-equal and read back the same way, with exactly one seat wearing the focus mark |

Rules that outrank convenience:

- Fixtures are **synthesized**. No real session content enters `testdata/`, ever.
- Golden renders use a pinned clock, an explicit terminal size and a plain style set, so
  they never depend on the CI terminal.
- A failing render assertion fails the build.
- No number appears in README/launch material unless this harness generated it.

## 6. Open design questions

1. ~~Language/stack~~ — **ANSWERED, ADR-002:** Go + Bubble Tea/Lipgloss, one binary,
   two modes. Windows-first hardened; macOS/Linux deferred post-v1.
2. ~~Normalized session schema~~ — **ANSWERED, §4a:** `model.Session` + `Capabilities`,
   pointers for absent-now vs. undeclared capability for can't-know, liveness classified
   by the HUD from `LastActivity` with an adapter override only on positive vendor
   evidence, and `Validate` as the machine-checked honest-gauge gate.
3. ~~HUD refresh model~~ — **ANSWERED, and the measurement it was waiting for has been
   taken.** A 1 s `tea.Tick` poll, not a file watcher. The survey's inputs argued for it:
   837 sessions across 33 directories, transcripts up to 7.7 MB, and a projects tree that
   provably mutates mid-sweep. `Discover` is stat-only and `Read` is head+tail bounded,
   which is what made polling affordable; a watcher over a mutating tree on Windows is a
   larger correctness surface for a smaller win.

   The open part was the cold cache, and `BenchmarkScan` closed it over a synthesized
   1,400-session corpus: the warm scan went 798 ms → **82 ms** with the `(size, mtime)`
   cache, and on the live corpus 1.84–3.37 s → **181–204 ms**. **The cold scan did not
   move** (896 ms → 994 ms, unchanged within noise), and that is the answer rather than a
   remaining gap: the first frame must genuinely read everything, which is what the
   spinner exists for. The poll was never the cost; re-reading unchanged files was.
4. ~~Exact Claude/Codex on-disk data sources~~ — **ANSWERED, §3.1–3.3**, with Claude
   verified live and Codex's first live pass run 2026-08-01 (§3.4; short remainder
   itemized there).
5. ~~Distribution naming (`telltale-hud` on any registry; winget/scoop manifests) — at
   packaging time. Go binary means npm is optional, not required.~~ — **ANSWERED
   2026-08-08, §8 "Packaging decisions":** the bare name `telltale` was free on scoop
   and winget, so the `telltale-hud` fallback goes unused; winget takes the
   publisher-qualified `sanlee-ys.telltale`; npm stays skipped rather than renamed.
6. ~~HUD UI design section~~ — **ANSWERED, §7:** layout grid, colour/threshold tokens
   shared with the statusline, motion rules, degraded-state renders. Written before HUD
   build per ADR-002; every render in §7.3 and every row in §7.7 is a golden/fixture.
7. **Cross-vendor context percentage — ANSWERED PROVISIONALLY, wants a ruling.** Claude
   and Codex context percentages are different statistics (§3.3), and Claude's is not
   derivable from disk at all. Three options were on the table:

   1. show raw token counts only, cross-vendor, no percentage anywhere;
   2. show a percentage only where a vendor ships a denominator, accepting a ragged
      column;
   3. show each vendor's own formula, labelled as vendor-native and non-comparable.

   **v1 ships (2).** Codex's `context_pct` is `CapDerived` and renders with an estimate
   marker; Claude's is `CapNone` and renders absent; and when no visible row can fill the
   column it is dropped entirely. (3) is rejected outright: two numbers in one column
   that are not the same statistic is exactly the lie the honest gauge forbids. (1) is
   the more conservative answer and remains the better one if the ragged column reads
   badly in daily use — it needs a `context_tokens` field in the schema, which is an
   additive change, and both adapters already carry the token count as an extra so
   nothing has to be re-derived. **Decide after two weeks of dogfood, not before.**

8. ~~LastActivity source on Windows~~ — **RULED 2026-08-01: option (iii),
   `max(mtime, newest record timestamp)`, implemented in both adapters.** NTFS defers
   mtime while the writer holds the file (~100 s observed on a hot rollout; ~20 min on
   a closing one, seen twice), so mtime alone under-reports on exactly the rows the HUD
   exists to watch. The newest record `timestamp` (RFC3339, vendor-written on records
   in both formats — verified live; some Claude housekeeping records omit it) comes out
   of the tail window the adapters already read, so the fold is zero extra I/O. Rules:
   each signal independently passes the future-skew guard or is excluded (a wrong
   vendor clock cannot fresh-wash a row); the fresher valid signal wins; the field
   degrades only when BOTH are unreadable. Still `CapReported` — the max of two
   vendor-written stamps invents nothing. Pinned by
   `TestLastActivityUsesNewestRecordTimestampOverStaleMtime` in each adapter; no HUD
   golden changed (fixtures inject `LastActivity` directly).

## 7. HUD UI design

Written before the HUD was built, per ADR-002. This section is binding: every render
below is a golden-test target in `internal/hud/testdata/golden/`, and the degraded states
in §7.7 are eval fixtures like the statusline's. Library facts were verified against live
docs on 2026-08-01 and then against the compiled API: `charm.land/bubbletea/v2` **v2.0.8**
and `charm.land/lipgloss/v2` **v2.0.5**. Both are v2: `AdaptiveColor` and the global
`Renderer` are gone, `Style` is a plain value type, and `Model.View()` returns a
`tea.View` struct rather than a string.

> **The renders in §7.3 are generated by the build, not drawn by hand.** They are pasted
> from `internal/hud/testdata/golden/*.txt`, which `go test ./internal/hud -update`
> regenerates. If a render here and the code disagree, the code is right and this section
> is stale — fix it in the same change.

### 7.1 Principles

Five rules, in priority order. Where polish and a rule conflict, the rule wins.

1. **The honest gauge extends to pixels.** Absent data renders as absent. Specifically:
   an *empty gauge track* means zero, so an absent gauge draws **no track at all** — the
   field is blank and the number beside it is `—`. `0%` and "no data" must never produce
   the same row of glyphs. This is the load-bearing render assertion of the whole HUD.
2. **Colour is always redundant.** Every distinction is carried by a glyph or a number
   first; colour only reinforces it. This makes `NO_COLOR` degradation correct by
   construction rather than by a second code path, and makes the HUD readable to
   colour-blind users without a mode.
3. **telltale may animate its own work; it must never animate the vendor's.** See §7.6.
4. **Still by default.** This is a glanceable monitor. In steady state the only cell
   permitted to change each second is the `AGE` of a session younger than one minute.
   If more than that moves, it is a bug, not a flourish.
5. **Same product as the statusline.** Identical thresholds, identical palette, identical
   `│` separator, identical `↻` countdown. The two surfaces share numbers through
   `internal/theme` (§7.5), not by coincidence.

A sixth rule that is a consequence of #1 and worth stating on its own: **account-level
quota appears once per vendor, in the header, never per row.** `rate_limits` is a
property of the account, not the session; repeating it on every row would assert
per-session quota, which is false. If no source can honestly supply it, that vendor's
block is absent — not zeroed — so the block count is itself a measurement: the header
shows exactly as many vendors as telltale can speak for. Where the readings come from
and how the line fits them is §7.15.

### 7.2 Anatomy

```
  header      identity, session counts, account quota           1 line (2 below 100 cols)
  rule        ─────────────────────────────────────────         1 line
  col header  SESSION  MODEL  CONTEXT  COST  AGE                1 line
  rows        one per session, sorted, scrollable               n lines
  rule        ─────────────────────────────────────────         1 line
  footer      key hints (left)   ·   state notices (right)      1 line
```

Chrome is 5 lines at wide, 6 below 100 cols (the quota block wraps to its own line). The
column-header row is dropped when the body is not the grid — over the help overlay or the
empty state it would label columns that are not on screen.

**Column grid.** Widths are fixed; the `SESSION` column is the only flexible one and
absorbs all slack, which right-anchors the numeric block at every terminal width.
Offsets below are 1-based for the wide tier at 120 columns.

| Cols | Field | Width | Align | Notes |
|---|---|---|---|---|
| 1 | pad / **selection** | 1 | | blank, or `▸` on the selected row (§7.11) |
| 2 | state dot | 1 | | `●` live / `◐` idle / `○` stale / blank unknown |
| 4–5 | vendor | 2 | left | `CC` / `CX` |
| 7 | separator | 1 | | dim `│` |
| 9–67 | **session** | **W−61** | left | flexes; `…` truncation |
| 70–82 | model | 13 | left | normalized display name |
| 85–96 | context gauge | 12 | | see §7.4 |
| 98–103 | context % | 6 | right | 6, not 5: a derived value carries a `~` marker |
| 106–112 | cost | 7 | right | |
| 114 | separator | 1 | | dim `│` |
| 116–119 | age | 4 | right | |
| 120 | pad | 1 | | |

Only two `│` separators per row, deliberately. They cut the row into three zones —
**identity** (dot, vendor), **measurement** (name, model, gauges, cost), **time** (age).
A pipe between every column reads as a spreadsheet; two pipes read as structure.

**Session label content.** The session's own `name` if the vendor has one, else the
workspace basename, else the vendor session id. Then the sub-agent chip if the session is
fanning out (§7.13). Then, only if ≥14 cells remain free, two spaces and the parent
directory (left-elided with `…`). The parent path disambiguates same-named projects under
different roots and stops the wide tier from opening a dead gulf between the name and the
model. It drops out automatically as the terminal narrows.

The chip's width is reserved **before** the name is truncated, and the name loses the
character. A chip that vanished on a long project name would make the same session look
like a different kind of session at a different terminal width — a lie by omission — and
the name is the field that can afford to lose a character, because the parent path and
the detail pane both still carry the identity.

> **Deviation from the original spec, deliberate:** the `⌥worktree` mark is not rendered.
> `worktree.name` exists only on the statusline's stdin payload, which the HUD does not
> consume, so **no adapter can source it** — a cell no adapter can fill is a cell that
> should not be in the grid. The glyph and its ASCII form stay in `internal/hud/glyphs.go`
> for the day a vendor writes it to disk.

**Responsive tiers.** Breakpoints are on width only; the shedding order is fixed, so the
layout at any width is a pure function of the width.

| Tier | Width | Model | Gauge | Cost | `SESSION` width | At 120 / 80 / 72 |
|---|---|---|---|---|---|---|
| wide | ≥ 100 | 13 | 12 cells | shown | W − 61 | 59 |
| compact | 80–99 | 13 | 8 cells | **dropped** | W − 48 | 32 |
| narrow | 60–79 | 13 | **dropped** | dropped | W − 38 | 34 |
| floor | < 60 | — | — | — | — | one-line notice |

Cost sheds before the gauge, and the gauge before the model, because the gauge is a
redundant encoding of a number that stays on screen, and the model is identity — the
answer to "which of my agents is this?" — which nothing else supplies. `MODEL` is never
narrowed below 13: `gpt-5.1-codex` is exactly 13 columns, and truncating a model name to
`gpt-5.1-c…` destroys the one field a user scans for.

Height tiers: **H ≥ 9** full chrome; **6 ≤ H < 9** drops both rules and the column-header
row (header + rows + footer only); **H < 6** shows the floor notice. Row overflow is not
paginated — the footer gains `+3 more`, and the viewport **follows the selection**: `↑`/`↓`
move the cursor and the visible window slides to keep it on screen. That arithmetic lives
in `Render` rather than in `Update`, because `Update` does not know how tall the chrome is
this frame and a second copy of the height maths is a second thing to get wrong.

Floor renders, exactly:

```
 telltale needs 60 columns (have 52)
```
```
 telltale needs 6 rows (have 4)
```

**Column auto-hide.** A column that would render `—` for *every* visible row is dropped
entirely and its width returned to `SESSION`; a full column of dashes is noise, not
information. This applies to `CONTEXT` and `COST` only (never to `MODEL` or `AGE`), is
computed per frame from the visible rows, and is therefore deterministic. The help
overlay lists any column hidden this way and why.

### 7.3 Target renders

These are the golden-test targets, pasted from the generated files. All data is
synthesized.

**A — wide, healthy (120 cols).** The reference render. Rendered with a *synthetic*
vendor that declares every capability, so the whole grid is exercised; render I below
shows what the real v1 capability mix produces.

```
 telltale  │  4 sessions  │  claude 3  codex 1              claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

Row 3 is the honest-gauge case in its normal habitat: a session whose adapter can source
a model and an age but not context or cost. Blank gauge field, `—` in both numeric
columns. Nothing about it looks like zero.

**B — compact (80 cols).** Cost gone, gauge halved, quota wrapped to its own line.

```
 telltale  │  4 sessions  │  claude 3  codex 1
                    claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────
        SESSION                           MODEL          CONTEXT            AGE
 ● CC │ telltale  C:\src\code             Opus 5         █████▉──  84.2% │  12s
 ● CC │ acme-api  C:\src\work             Sonnet 4.5     ██▉─────    41% │  48s
 ◐ CX │ notes-api  C:\src\code            gpt-5.1-codex                — │   4m
 ○ CC │ learning-notes  C:\src\code       Haiku 4.5      ██████▌─  92.6% │  22m
 ──────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   ? keys
```

**C — narrow (72 cols).** Gauge gone; the number it encoded stays. Vendor names shorten.

```
 telltale  │  4 sessions  │  cc 3  cx 1
            claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────
        SESSION                             MODEL            CTX    AGE
 ● CC │ telltale  C:\src\code               Opus 5         84.2% │  12s
 ● CC │ acme-api  C:\src\work               Sonnet 4.5       41% │  48s
 ◐ CX │ notes-api  C:\src\code              gpt-5.1-codex      — │   4m
 ○ CC │ learning-notes  C:\src\code         Haiku 4.5      92.6% │  22m
 ──────────────────────────────────────────────────────────────────────
 q quit   / find   ? keys
```

**D — degraded rows (120 cols).** Four distinct failure shapes in one frame. Rows are
sorted by activity, so they do not appear in the order they are described.

```
 telltale  │  4 sessions  │  claude 3  codex 1                                         claude 5h ███─────   42% ↻ 2h13m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         ────────────     0%    $0.04 │   3s
 ● CC │ a-really-long-project-name-that-overflows-the-label-column…  Opus 5         ███████████─  99.9%  $340.50 │   9s
 ◐ CX │ 4f2a9c81-1d3e-4a77-9b02-000000000000                                                          —        — │   7m
   CC │ acme-api  C:\src\work                                        Sonnet 4.5                       —    $1.02 │    —
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

Row 1 is at exactly 0% and draws a full track. Row 2 is the label-overflow case,
truncated at `…` with the grid intact. Row 3 is a session discovered by filename whose
only record was torn, so nothing parsed — the label falls back to the session id, the
model cell is blank and every sourced field is `—`. Row 4 has a record timestamp in the
future (clock skew): its `AGE` is `—`, never a negative or a zero, and because its
liveness is `unknown` **its state dot is blank, not `○`** — unknown is not a claim of
staleness.

**J — zero versus absent (120 cols).** The single assertion the build exists to protect.

```
 telltale  │  2 sessions  │  claude 2
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ at-zero  C:\src\code                                         Opus 5         ────────────     0%    $0.00 │   5s
 ● CC │ no-source  C:\src\code                                       Opus 5                           —        — │   6s


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

`0%` is a full track of `────────────`; absent is whitespace. If these two rows ever
render the same, the build fails.

**E — stale scan (120 cols).** The scan has been failing for 47 seconds. Values are the
last ones actually measured; the whole row area renders `Muted` (invisible in a plain
golden, asserted separately), and the footer's right slot carries the notice. The header
is never used for notices — it holds identity and quota only, which keeps it from
overflowing at any width.

```
 telltale  │  4 sessions  │  claude 3  codex 1              claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                                                ⚠ last scan 47s ago   Access is denied.
```

**F — filter and sort active (120 cols).** The header count reads `3 of 4` so it cannot
contradict the per-vendor totals beside it. Non-default filter/sort is stated in the
footer, because a monitor that silently hides rows is a liar.

```
 telltale  │  3 of 4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys    filter claude   sort context
```

**G — empty (120 cols).** Distinguishes "watching, found nothing" from "vendor not
installed". Two different facts; two different words; never a fake row and never an
error dialog.

```
 telltale  │  0 sessions
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                   no active sessions

                             agy      not detected   %USERPROFILE%\.gemini\antigravity-cli
                             claude   watching       %USERPROFILE%\.claude\projects
                             codex    not detected   %USERPROFILE%\.codex
                             cursor   not detected   %APPDATA%\Cursor\User
                             gemini   not detected   %USERPROFILE%\.gemini\tmp
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

The vendor status word is one of exactly four: `watching` (directory exists and is
readable), `not detected` (directory absent), `unreadable` (the vendor's data is there and
the adapter cannot read it — an OS refusal, or a store whose schema the adapter does not
recognize (§3.9); rendered `SevWarn` with the reason appended), `drifted` (the store
opened and read, and at least one session's read could not find the structure the adapter
was verified against — `internal/adapter/drift`; also `SevWarn`). On the dev machine today
the Codex line reads `not detected`, since `~/.codex` is absent (§3.2). The third word:

```
 telltale  │  0 sessions
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                   no active sessions

                       agy      not detected   %USERPROFILE%\.gemini\antigravity-cli
                       claude   unreadable     %USERPROFILE%\.claude\projects  Access is denied.
                       codex    not detected   %USERPROFILE%\.codex
                       cursor   not detected   %APPDATA%\Cursor\User
                       gemini   not detected   %USERPROFILE%\.gemini\tmp
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

`drifted` is the word the other three cannot say. Those three are all answers from
`Discover`, known before a single session is read. Drift is knowable only *after* the
read: it is the finding that a store which opened, listed and parsed no longer carries the
structure the adapter's readings hang off. Calling that `unreadable` would borrow the word
for "the OS refused" to describe a store the OS handed over intact — the same collapse
`zero` and `absent` are kept apart to avoid (§4a.1).

It renders with its **scope** appended: how many of the vendor's sessions reported drift,
out of how many this scan read. One of forty-one is a vendor mid-rollout; forty-one of
forty-one is a format that moved under the whole store, and the word alone cannot tell
those apart. The scope deliberately does **not** reuse the header's `n of m sessions`
sentence — the header counts visible-of-total across every vendor, this counts
drifted-of-read for one vendor, and the two land on the same screen. In the borrowed
grammar the vendor line would read as a claim about how many sessions are *showing*, which
the header directly contradicts; naming what the numerator counts is what keeps them
apart. The scope is the only part of the line that gives way when the width runs out, the
same way the grid sheds `COST`; the word never does.

This state needs sessions to exist and every one of them to be hidden — below, by the
8-hour idle cutoff — because a vendor cannot drift without having produced the sessions
that revealed it. The ordinary case is render M. The fourth word:

```
 telltale  │  0 of 2 sessions  │  codex 2
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                   no active sessions

                           agy      not detected   %USERPROFILE%\.gemini\antigravity-cli
                           claude   not detected   %USERPROFILE%\.claude\projects
                           codex    drifted        %USERPROFILE%\.codex  1 drifted of 2 read
                           cursor   not detected   %APPDATA%\Cursor\User
                           gemini   not detected   %USERPROFILE%\.gemini\tmp
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys                 ⚠ codex drifted
```

**H — help overlay (120 cols).** Replaces the row area rather than floating over it; a
floating panel on a monitor obscures the thing being monitored.

```
 telltale  │  4 sessions  │  claude 3  codex 1              claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

        q      quit  (also ctrl+c)
        ↑/↓    move the selection  (also j / k)
        enter  open the detail pane for the selected session
        u      what each vendor has left, and what it spent
        w      this week: the fleet's slow windows only
        /      find: narrow rows by name or path
        esc    close the pane, or cancel the find, or quit
        v      vendor: all > claude > codex >
                       gemini > agy > cursor
        s      sort: activity > context > cost
        a      show all (include sessions idle > 8h)
        r      rescan now
        ?      close this help
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ? close
```

**I — the v1 capability mix (120 cols).** What the real adapters actually render today:
Claude sources neither context nor cost from disk, Codex sources a **derived** context
percentage (marked `~`) and real quota windows. Nothing sources cost, so the `COST`
column auto-hides and its width returns to `SESSION`. The `AG` row carries no `Name`
at all — the only free text on agy's disk is prompt content (§3.8), so this field is
`CapNone` and the HUD falls back to the workspace basename, the same fallback a Gemini
row takes when it has no summary of its own (ruled 2026-08-12). Its `MODEL` cell
truncates because the vendor's display string is 23 characters against a 13-column
cell — both are what the HUD really shows. The `CU` row is the one to read next to the
`CX` row: both carry a context bar, and only one of them carries a `~`, because Cursor
persists its own `contextUsagePercent` and telltale reads it rather than computing one
(§3.9).

```
 telltale  │  5 sessions  │  claude 1  codex 1  gemini 1  agy 1  cursor 1               codex 5h ██████▎─ 88.4% ↻ 3h02m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
 ● CC │ telltale  C:\src\code                                                 Opus 5                           — │  12s
 ● CU │ multi-vendor orchestration  C:\src\code                               composer-2.5   ████▏───────    37% │   1m
 ● CX │ example-app  C:\src\code                                              gpt-5.1-codex  ███████▋──── ~69.8% │   1m
 ● AG │ example-app  C:\src\code                                              Gemini 3.6 F…                    — │   2m
 ◐ GE │ glossary tooltips ⑂~2  c:\src\code                                    gemini-3-pro                     — │   3m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

This render is the one to look at when judging §6 Q7. The ragged CONTEXT column is the
cost of option (2); it is honest, and whether it is *legible* is a dogfood question.

**K — every column hidden (120 cols).** No visible row reports context or cost.

```
 telltale  │  3 sessions  │  claude 2  codex 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                                                    MODEL            AGE
 ● CC │ telltale  C:\src\code                                                                      Opus 5        │  12s
 ◐ CX │ notes-api  C:\src\code                                                                     gpt-5.1-codex │   4m
 ○ CC │ learning-notes  C:\src\code                                                                Haiku 4.5     │  22m

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

**L — ASCII glyph mode (120 cols).** `--ascii`, `TELLTALE_ASCII=1`, or a non-terminal
output target. Absent renders `n/a`; the gauge loses its eighth-cell partials, which is a
real precision loss in the bar and acceptable only because the number beside it carries
the precision.

```
 telltale  |  4 sessions  |  claude 3  codex 1              claude 5h ###-----   42% ~ 2h13m  7d #-------   18% ~ 5d02h
 ----------------------------------------------------------------------------------------------------------------------
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 * CC | telltale  C:\src\code                                        Opus 5         #########---  84.2%    $2.41 |  12s
 * CC | acme-api  C:\src\work                                        Sonnet 4.5     #####-------    41%    $0.18 |  48s
 o CX | notes-api  C:\src\code                                       gpt-5.1-codex                  n/a      n/a |   4m
 . CC | learning-notes  C:\src\code                                  Haiku 4.5      ##########--  92.6%   $11.07 |  22m
 ----------------------------------------------------------------------------------------------------------------------
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

**M — shape drift (120 cols).** A store that reads fine and no longer matches. Every row
here is render A's except the Codex one, whose read found no `session_meta` record — so
everything that record feeds is absent, and the row renders *exactly* as it would if Codex
simply had nothing to say. That is the failure: nothing in the grid can tell those two
apart, and the footer notice is the only thing on screen that knows.

It is also why the fourth vendor word needs a second home. The vendor line renders in the
empty state only, and a vendor cannot drift without having produced sessions — so the
screen drift actually happens on is this one, where the vendor line is not present at all.
`driftNotice` therefore renders under **every** body: grid, empty state, help overlay and
detail pane alike. A warning that came and went with whichever pane was open would be one
a reader could not trust to be there.

```
 telltale  │  4 sessions  │  claude 3  codex 1              claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ 00000000-bbbb-4ccc-8ddd-000000000001                                                          —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys                 ⚠ codex drifted
```

### 7.4 The gauge

Glyphs: fill `█` (U+2588) with eighth-block partials `▏▎▍▌▋▊▉` (U+258F–U+2589), track
`─` (U+2500). Full-height fill over a mid-height rule reads as a level above a baseline
and keeps the row visually quiet; a shaded `░` track reads as texture and fights the text.

Three rules, all of which exist to stop the gauge from lying:

1. **The last cell is reserved below 100%.** Fill is computed over `cells−1`, so a value
   of 99.9% always leaves one visible track cell and only an exact 100% fills the bar. A
   92.6% bar that renders as solid is a gauge claiming "full" when it is not.
2. **Any nonzero value draws at least one eighth.** 0.4% must not be pixel-identical to
   0%.
3. **Absent draws nothing.** Not an empty track — nothing. (Principle #1.)

Verified scale at 12 cells (`TestGaugeScale` pins every row):

```
    0%  ────────────      0%        84.2%  █████████▎──   84.2%
  0.4%  ▏───────────    0.4%        92.6%  ██████████▏─   92.6%
    5%  ▌───────────      5%        99.9%  ███████████─   99.9%
   25%  ██▊─────────     25%         100%  ████████████    100%
   50%  █████▌──────     50%       absent                     —
```

**Number formatting**, shared with the statusline via `internal/theme`:

- Percent: floored to one decimal, never rounded up (a usage gauge must not overstate);
  whole numbers drop the decimal; `100%` has no decimal. Guarantees a 5-column field, and
  the cell is 6 so a derived value can carry its `~`.
- Cost: `$0.00` under $1000, `$1234` at or above.
- Age: `12s` / `47m` / `2h` / `3d`, capped at 4 columns. Sub-hour precision is where a
  monitor's value is; `2h13m` precision belongs to the quota countdown, not to row age.
- Countdown: `↻2h13m`, `↻47m`, `↻5d02h`. The days branch matters: without it a seven-day
  window renders `↻120h00m`.

The statusline still uses its own `pct` and `shortDur`; see the divergence note in §2.

### 7.5 Colour and threshold tokens

Thresholds are the statusline's, unchanged and now literally shared:
**green < 60, yellow ≥ 60, red ≥ 85** from `theme.WarnPct` / `theme.CritPct`.

The palette is deliberately the terminal's own 4-bit ANSI palette rather than hex
truecolor. Reason: telltale then inherits whatever theme the user already chose, looks
native in Windows Terminal's default scheme and in a light-background scheme without a
second palette, and matches the statusline byte-for-byte in intent. Total palette: four
hues, one attribute, and the default foreground.

| Token | Meaning | ANSI | Statusline (raw) | HUD (lipgloss v2) |
|---|---|---|---|---|
| `Text` | primary values | default | *(unstyled)* | `NewStyle()` |
| `Muted` | chrome, labels, rules, de-emphasis | — | `\x1b[2m` | `NewStyle().Faint(true)` |
| `Identity` | model name, vendor tag | 6 | `\x1b[36m` | `Foreground(Color("6"))` |
| `SevOK` | value < 60; healthy notices | 2 | `\x1b[32m` | `Foreground(Color("2"))` |
| `SevWarn` | value ≥ 60; warning notices | 3 | `\x1b[33m` | `Foreground(Color("3"))` |
| `SevCrit` | value ≥ 85; error notices | 1 | `\x1b[31m` | `Foreground(Color("1"))` |
| `Track` | unfilled gauge cells | 7 / 8 | n/a | `Foreground(lightDark(Color("7"), Color("8")))` |

Semantic aliases, so intent is greppable rather than inferred: `Absent() = Muted`,
`Rule() = Muted`.

Hue owns exactly one meaning: **cyan is identity, the green/yellow/red ramp is severity,
faint is de-emphasis.** Nothing else gets a colour. In particular the state dot encodes
liveness by *glyph and intensity* (`●` Text / `◐` Text / `○` Muted), never by hue — green
already means "under 60%", and one hue meaning two things is how a colour system rots.

**Shared code, without dragging Lipgloss onto the fast path.** ADR-002 requires the
statusline to stay stdlib-only and never initialize Bubble Tea. So `internal/theme`
holds only numbers and names — `WarnPct`, `CritPct`, the ANSI indices, and the shared
format helpers — and no `Style` type at all. `internal/statusline` maps those indices to
escape codes as it does today; `internal/hud/style.go` maps them to `lipgloss.Style`
values. One source of truth for the thresholds, zero coupling of the statusline binary to
the TUI stack.

**Light and dark backgrounds.** Lipgloss v2 removed `AdaptiveColor` and the global
renderer, so adaptation is explicit: `Init()` lifts `tea.RequestBackgroundColor()` into a
`Cmd`, `Update` handles `tea.BackgroundColorMsg` and calls `msg.IsDark()`, and the style
set is rebuilt with `lipgloss.LightDark(isDark)`. Only `Track` consumes it (light gray on
light backgrounds, dark gray on dark). Terminals that never answer the OSC query leave
the default: assume dark. Because exactly one token depends on it and no layout does,
golden layout tests are unaffected by which branch is taken — and
`TestBackgroundColorRebuildsTheStyleSetWithoutMovingTheLayout` enforces that.

**NO_COLOR.** `colorprofile` caps the profile at `Ascii` when `NO_COLOR` is set; Bubble
Tea v2 downsamples internally, so no telltale code path is involved. Under `Ascii` every
`Foreground` disappears while `Faint` survives — chrome still recedes. Nothing is lost,
because by principle #2 colour was never the sole carrier of any distinction. No
`--no-color` flag of our own: one mechanism, the standard one.

**ASCII glyph mode** is a *separate* switch from colour — `--ascii`, or
`TELLTALE_ASCII=1`. For legacy consoles and non-UTF-8 code pages:

| Unicode | ASCII | | Unicode | ASCII |
|---|---|---|---|---|
| `●` `◐` `○` | `*` `o` `.` | | `─` (light rule / track) | `-` |
| `━` (heavy rule) | `=` | | | |
| `█` + eighths | `#` (no partials) | | `│` | `\|` |
| `—` (absent) | `n/a` | | `…` | `>` |
| `↻` | `~` | | `⌥name` | `(name)` |
| `⚠` | `!` | | spinner | `-\|/` rotation |

### 7.6 Motion

**The rule: telltale may animate its own work; it must never animate the vendor's.**

Everything follows from it. A spinner on a session row would assert "this agent is
working right now" — a claim telltale cannot source, since the adapters read files on
disk and know a last-write timestamp, not liveness. That is a narrated animation, which
is the honest-gauge violation in motion form. A tweened gauge is worse: every
intermediate frame displays a value no vendor ever reported.

**Animates — one thing.** A braille spinner `⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏` at 10 fps in the header, only
while the *first* scan is in flight, and only once that scan exceeds 250 ms. It reports
telltale's own I/O, which telltale is entitled to describe. After the first successful
scan it never appears again; later slow scans surface as staleness (§7.7), not motion.
`TestSpinnerStopsForeverAfterTheFirstScan` pins that.

**Never animates.** Numbers. Gauge fills. Colour pulses or flashes. Row insertion and
removal. Sort transitions. Per-row spinners or activity indicators of any kind.

**Cadence.** Poll every 1000 ms via `tea.Tick`. Rows re-sort only when the underlying
values change, and never as a tie-break wobble — the sort comparator falls back to
session id so equal keys hold a stable order frame to frame. Steady-state churn budget is
principle #4: the `AGE` cell of sessions younger than 60 s, and nothing else. The
footer's scan-freshness notice appears *only when abnormal* (> 3 s), precisely so the
healthy screen has no ticking element in it.

The 1 s cadence is affordable because the poll is stat-first: `Discover` lists and stats
only, and `Read` touches a bounded head and tail rather than a whole transcript. Any
further tail-read optimization must honour §4 — a backward seek can land mid-record, so
the first partial record after a seek is discarded, not parsed. `internal/jsonl.Tail`
already does this.

**Bubble Tea v2 implications** (verified against v2.0.8):

- `Model` is `Init() Cmd`, `Update(Msg) (Model, Cmd)`, `View() View`. `View` is a struct,
  so alt-screen and cursor are view state, not program options: return a `tea.View` with
  `AltScreen: true`, `Cursor: nil`, `WindowTitle: "telltale"` (`--no-title` suppresses
  the title). There is no `WithAltScreen` in v2.
- `tea.RequestBackgroundColor()` is a `Msg`, not a `Cmd`; it has to be lifted into a
  `func() tea.Msg` before `tea.Batch` will take it.
- **Scanning never happens inside `Update`.** The tick dispatches a `tea.Cmd`; the scan
  returns a `scanResultMsg`. At most one scan is in flight and ticks arriving during a
  scan are dropped. This is a Windows correctness requirement, not tidiness: a `stat`
  against a disconnected network path blocks, and a blocked `Update` freezes input
  including `q`.
- **`View()` is pure and never calls `time.Now()`.** The model carries `now`, stamped when
  the tick arrives — the same discipline as `statusline.Options.Now`. This is what makes
  the renders in §7.3 testable at all.
- Do not raise the renderer FPS. Frames are byte-identical between data changes, so the
  framerate cap is not the thing limiting redraws — the data is.
- Render nothing until the first `tea.WindowSizeMsg` arrives. One blank frame beats one
  frame of wrong layout.

### 7.7 Degraded and empty states

Each row below is an eval fixture. Fixtures are synthesized — fake session ids, fake
paths, fake text — never copied from real transcripts.

| Fixture | Condition | Where it is asserted | Assertion |
|---|---|---|---|
| `zero-vs-absent` | one session at 0%, one with no context source | golden J + `TestAbsentGaugeIsNotAnEmptyTrack` | The two rows differ. The build fails if a gauge cannot tell "no data" from "zero". |
| `degraded` row 3 | adapter sources model + age only | golden D | Blank gauge field, `—` in `CONTEXT` and `COST`; no `0`, no stale carry-over. |
| torn tail | file ends mid-record, no trailing `\n` | `TestTornTailChangesNothing` (both adapters) | The partial line is held, never parsed (§4). A torn tail changes nothing on screen. |
| torn-only | the file's *only* record is torn | `TestTornOnlyRecordStillListsWithEverythingAbsent`, golden D row 3 | Session still listed (discovered by filename), label falls back to session id, every sourced field `—`, age from file mtime. |
| clock skew | record timestamp in the future | `TestFutureMtimeDegradesRatherThanClampingToZero`, golden D row 4 | `AGE` = `—`. Never a negative duration, never `0s`. Liveness is `unknown`, so the dot is blank. |
| `stale-scan-47s` | scan older than 3 s | golden E + `TestStaleScanDimsTheRowArea` | Values are retained (they were true at the displayed `AGE`) but nothing renders at full intensity. |
| `stale-scan-90s` | scan failing over 60 s | golden | Notice escalates to `SevCrit` and the header quota goes `Muted` too. Quota is as stale as everything else and must not look fresh. |
| `empty-watching` | vendor dirs readable, no sessions | golden G | `watching` + the path actually checked, home-redacted. |
| vendor missing | `~/.codex` absent | golden G, `not detected` + `TestVendorAbsentBecomesNotDetected` | No fake row, no error state; the other vendor still renders. |
| `empty-unreadable` | dir exists, OS refuses | golden + `TestUnreadableVendorKeepsTheOSMessage` | Third word, distinct from the other two, with the OS message in `SevWarn`. |
| `quota-absent` | API-key login, no `rate_limits` | golden + `TestNullRateLimitsYieldNoQuotaWindows` | Header quota block absent. Mirrors the statusline's load-bearing test — never `5h 0%`. |
| `degraded` row 2 | label longer than the column | golden D | Truncation at `…`, grid intact. |
| `column-hidden` | `CONTEXT` and `COST` absent for every visible row | golden K | Columns dropped, width returned to `SESSION`; help overlay names them. |
| `floor-width` / `-height` | 52 cols / 4 rows | goldens | One line, no partial grid. |
| gauge scale | the §7.4 table | `TestGaugeScale` | Exact glyph string per value: the reserve-last-cell and min-eighth rules. |
| separator injection | a session name containing U+2028/U+2029 | `TestSessionNameSeparatorsCannotTearTheGrid`, `TestDetailPaneSanitizesModelAuthoredText`, `TestFindQueryCannotTearTheFooter` | The character never reaches the frame — grid, pane or footer — and no line exceeds the terminal width. |

Added in v1.1:

| Fixture | Condition | Where it is asserted | Assertion |
|---|---|---|---|
| `detail-pane` | pane over a Claude row, real capability table | golden + `TestDetailPaneSeparatesCantKnowFromAbsentNow` | Fields Claude declares `CapNone` get **no line**; they are named once on `not sourced`. |
| `detail-degraded` | pane over a session whose records did not parse | golden + `TestDetailPaneShowsDegradedFieldsAndDiagnostics` | Degraded field names and every diagnostic are on screen; a declared-but-empty quota is `—`, never `0%`. |
| clean session | no degraded fields, no diagnostics | `TestDetailPaneStatesTheAbsenceOfProblems` | The honesty block says `—` rather than going blank; a blank block is indistinguishable from a pane that forgot to render it. |
| measured zero fan-out | `Subagents = 0` | `TestDetailPaneStatesAMeasuredZeroFanOut`, `TestSubagentChipOnlyAppearsForANonzeroCount` | Grid draws **no chip**; the pane says `~0 recent`. |
| uncountable fan-out | sidecar unreadable | `TestDetailPaneRendersAnUncountableFanOutAsAbsent` | `—`, never `0`. |
| selection vanishes | the selected session ends mid-poll | `TestASelectedSessionThatVanishesClosesThePane`, `TestDetailPaneSaysSoWhenItsSessionIsGone` | The pane closes rather than retargeting; an out-of-range cursor says "no longer listed". |
| re-sort under the cursor | a bottom row becomes the newest | `TestSelectionFollowsTheSessionNotTheIndex` | The selection follows the **session key**, not the index. |
| `row-grammar` | selection mark + fan-out chips | golden + `TestSelectionIsAGlyphNotAHighlight` | Selection is a glyph in the pad column, not reverse video. |
| chip vs. truncation | a 73-character session name | `TestSubagentChipSurvivesLabelTruncation` | The chip survives at every width; the name gives way. |
| `burn-forecast` | 7 samples over 18 min on one window, a near-flat second window | golden + `TestForecastArithmeticIsPinned` | Exact projected time and basis; the slow window renders **nothing**. |
| below basis | < 3 samples, or a span < 5 min | `TestForecastRefusesToProjectBelowTheMinimumBasis`, `TestNoForecastRendersWithoutABasis` | Nothing renders. Not a placeholder, not a dash — the header cell simply ends. |
| window rollover | usage drops, or `resets_at` jumps a window forward | `TestUsageDropClearsTheSamples`, `TestResetsAtJumpClearsTheSamplesButJitterDoesNot` | The buffer clears; three seconds of `resets_at` jitter does not clear it. |
| `find-active` | find mode with a query typed | golden | The footer becomes the query line and says how to leave. |
| `find-applied` | query applied, mode left | golden + `TestAnAppliedQueryAlwaysAnnouncesItself` | Header reads `2 of 4`; footer keeps naming the query. |
| query hides everything | a query matching no row | `TestAnEmptyResultNamesTheQuery` | The empty state names the query rather than saying "no active sessions". |
| over-long query | 156 characters at 60–120 cols | `TestALongQueryIsTruncatedNotDropped` | Truncated with `…`, never pushed off the footer — a query that vanished while still filtering is the silent row-hiding the footer exists to prevent. |
| query with a trailing space | `"acme "` | `TestTheDisplayedQueryIsTheQueryBeingMatched` | The string on screen is the string being matched; the display is not trimmed. |

Added with shape-drift reporting:

| Fixture | Condition | Where it is asserted | Assertion |
|---|---|---|---|
| `shape-drift` | a read reports drift; every row still renders | golden M + `TestDriftIsVisibleOnTheGridNotOnlyInTheDetailPane` | The grid is unchanged and the footer carries `⚠ <vendor> drifted`. A healthy frame never mentions drift, and the notice survives `--ascii` and `NO_COLOR` as a word. |
| `empty-drifted` | sessions exist, all past the idle cutoff | golden + `TestTheDriftScopeCannotBeReadAsTheHeaderCount` | The fourth vendor word, with its scope in the slot `unreadable` gives to the OS message — and in a grammar the header's own count cannot be mistaken for. |
| partial drift | 1 of 41 sessions reports drift | `TestOneDriftedSessionDriftsTheVendor`, `TestTheVendorLineStatesHowMuchOfTheStoreDrifted` | **Any** drifted session drifts the vendor, and the counts travel with the word. Every row still renders. |
| drift under a failed `Discover` | vendor absent, or the OS refuses | `TestTheDiscoverTierStillWinsOverDrift` | `not detected` and `unreadable` are untouched: the roll-up only runs where `Discover` succeeded, so the ordering is structural rather than a comparison. |
| reworded drift note | `drift.Watch` changes its wording | `TestDriftIsRecognizedFromTheNoteTheAdapterLayerActuallyWrites` | The HUD reads drift off `Diagnostics` text, which the compiler cannot check. The test folds a real `drift.Watch` so a rewording fails the build instead of silencing the vendor line. |
| notice pile-up | 60 cols with a 24-char query, a filter, a sort, a stale scan **and** drift | `TestADriftedFrameStillFitsEveryTier`, `TestTheFooterGivesUpItsCheapestNoticesFirst` | Every line fits the terminal. Whole notices are dropped, cheapest first, and `…` says so. |

Freshness escalation, stated once: **≤ 3 s** normal; **> 3 s** row area `Muted` + footer
notice in `SevWarn`; **> 60 s** notice in `SevCrit` and the header quota goes `Muted` too.
Retained values are not "presented as fresh" in any of these, because the age of the
measurement is on screen next to them — that is the condition the honest-gauge rule
actually imposes.

Notice priority, stated once: the footer cannot always hold every notice — `joinEnds` has
no truncation path, so a block that does not fit runs off the end of the terminal. The
block is therefore fitted first, by dropping **whole** notices cheapest-first and
prefixing what survives with `…`. The order is `sort`, `+N more`, `filter`, `find`, the
stale-scan warning, drift. That is the rule `joinEnds` already applies between the key
hints and the notice block, asked one level down: what survives is what the reader cannot
find out anywhere else on this screen. `sort` hides nothing at all; `+N more` sits above a
row area the reader can see is full; a filter and a query hide rows silently, but the
header's `N of M sessions` still declares *that* rows are hidden, so only the cause is
lost; a stale scan re-announces itself every tick and clears the moment a scan succeeds;
drift does neither, and is the last to go. A single notice wider than the whole line is
truncated rather than dropped — an ellipsis on a warning still says a warning is there,
and a footer that dropped its last one would quietly claim nothing is wrong.

### 7.8 Keyboard

Minimal, and every key earns its place.

| Key | Action |
|---|---|
| `q`, `ctrl+c` | quit |
| `esc` | close the detail pane → close help → clear the find query → **then** quit |
| `↑`/`↓`, `j`/`k` | move the selection (scrolls the help overlay while it is open) |
| `enter` | open the detail pane for the selected session; close it if open |
| `/` | find: type-to-filter on name or path |
| `v` | vendor filter cycle: all → claude → codex → gemini → agy → cursor → all |
| `s` | sort cycle: activity → context → cost → activity |
| `a` | toggle show-all (default hides sessions idle > 8 h) |
| `r` | rescan now |
| `?` | toggle help |

In **find mode** the keyboard belongs to the query: only `esc` (clear and leave), `enter`
(keep and leave), `backspace` and `ctrl+c` are commands, and everything else is text.
That is why the mode takes over the whole footer — a mode that silently changes what `q`
means without saying so is how a read-only monitor surprises someone.

`--vendor all|claude|codex|gemini|agy|cursor` sets the starting filter; the cycle takes
over from there. `antigravity` is accepted as a synonym for `agy` and `composer` for
`cursor`; the short forms are the ids the footer and the header counts print.

Cycles, not multi-select menus: with six vendors and three sorts, a cycle is one keystroke
and no mode. Non-default filter, sort or query is always visible in the footer. The help
overlay writes the cycle with `>` rather than `->` for one reason worth recording: the
fourth vendor pushed that line past the 60-column floor, and a golden test at that width
is what caught it. The **sixth** vendor exhausted that trick, and the cycle now wraps onto
a continuation line indented under the first hop — shortening the vendor names instead
would have made the overlay teach a name the footer does not print.

> **Reversed in v1.1, deliberately.** v1 said: *"There is no selection cursor — the
> default sort puts the interesting sessions on top, and a cursor invites drill-down,
> which is a different product."* The roadmap (§8) then decided drill-down **is** the
> product: the schema already carried `Diagnostics`, `Degraded` and every `Extra` with no
> surface to show them on, and that machinery is the thing this project is actually
> about. The original objection is answered rather than ignored — the cursor starts at
> **no selection** and the mark appears the first time the user asks for it, so the
> steady-state monitor frame is byte-identical to v1's.

Anything that changes *which rows are visible or in what order* (`v`, `s`, `a`, a new
query) **drops the selection** and closes the pane. The cursor is an index into the
visible rows, so a different row set makes the old index point at a different session.
Between polls the selection is carried by **session key**, not by index, because the
activity sort re-orders rows as sessions write — holding the index would silently move
the selection, and with the pane open would relabel one session's diagnostics with
another's.

Show-all deliberately does **not** hide a session with no activity timestamp: "we have no
signal" is not evidence that a session is old.

Deliberately absent: mouse support, fuzzy/regex/embedding search (the query is displayed
literally, and a syntax that can mean something other than what it looks like is a filter
that hides rows without saying so), and configuration UI. And one invariant that outranks
all future feature requests: **the HUD is strictly read-only. No keybinding may ever
mutate vendor state or send anything to a running agent.** The HUD is a telltale.

That invariant is scoped to the observation surfaces — `hud` and `statusline` — and it does
not weaken. `telltale council` (ADR-008, §9) is a separate subcommand that *does* dispatch to
vendor CLIs; it is a dispatch room, not a gauge, it is entered deliberately, and it says so on
screen. Nothing in §7 may reach for it.

### 7.9 Golden tests

- `Render` is pure over `State` — `(sessions, vendors, now, width, height, filter, sort,
  showAll, help, scroll, scanning, thresholds)`. Tests construct the state directly and
  compare against `internal/hud/testdata/golden/*.txt` — no terminal, no program loop.
  `go test ./internal/hud -update` regenerates them.
- Two families. **Layout goldens** render with `PlainStyles()`, a style set in which every
  `Render` is the identity, at widths 120 / 80 / 72 / 52 and at the height floor — so they
  never depend on the CI terminal's colour profile. **Style assertions** render with
  `NewStyles(true)` and check one escape code per severity band, mirroring
  `TestThresholdColors` in `internal/statusline/render_test.go`.
- Width is measured with `lipgloss.Width`, never `len()` — the label column carries
  arbitrary project names. `TestNoLineExceedsTheTerminalWidth` sweeps seven widths with
  and without the help overlay.

### 7.10 Known limitations

- Every glyph in the visual language — `● ◐ ○ ─ │ █ ▏▎▍▌▋▊▉ … — ↻ ⚠ ▸ ⑂ ·` — is
  East-Asian-**Ambiguous** width. Windows Terminal, the reference environment, renders
  ambiguous as narrow, which is what the grid assumes. A terminal configured to render
  ambiguous glyphs double-width will shear the layout; `--ascii` is the escape hatch.
  Stated here rather than discovered later.
- `⑂` (U+2482 OCR FORK) is the least-common glyph in the set and the most likely to miss
  from a font. It appears **only** on a session that is fanning out, so a font gap costs
  a tofu box on a minority of rows rather than a broken grid — and `--ascii` renders it
  `Y`. It was chosen over a second `│` or a bracket because both already mean something
  here (`│` separates zones; `]` is the ASCII selection mark).
- The detail pane does not scroll. A pane taller than the row area is clipped, and on a
  terminal shorter than about 16 rows a long extras list can run off the bottom. The
  arrows are spent on moving between sessions, which is the more valuable binding while
  the pane is open; a scrollable pane needs a second axis and is deferred.
- The `█` fill and `─` track differ in glyph height by design. Verified legible in
  Cascadia Mono; other fonts may render the step more harshly.
- Fill resolution is one eighth of a cell (1.04% at 12 cells). The number beside the bar
  carries the precision; the bar carries the glance.
- ~~The account quota block is sourced from one session (§7.1). A second quota-bearing
  vendor needs a per-vendor block.~~ **Closed in two steps.** §7.15 (2026-08-07) gave the
  *header* a block per vendor, from the statusline relay alongside the transcript reading.
  §7.17 (2026-08-09) gave the per-vendor block its own surface: `u` opens a body with one
  block per vendor, the gauge at 20 cells instead of the header's 8, and — the part the
  header has no room for at all — a stated reason wherever a vendor has nothing to say.
  What remains is not this limitation but §7.17's own: an aged-out relay reading and one
  that never arrived render alike.
- ~~The 1 s poll has not been measured on a cold cache over an 837-session tree (§6 Q3).~~
  **Measured** — see §6 Q3 and the `BenchmarkScan` table. The cold scan is the half that
  did NOT improve, and that is the ruling rather than the residue: the first frame has to
  read everything, and the spinner is what covers it.
- The burn forecast's sampling history lives in the process and dies with it. Restarting
  the HUD restarts the basis at zero, and for the first five minutes of every run there
  is no forecast at all. Persisting samples would mean writing to disk, which "telltale
  never writes" forbids, so this limitation is load-bearing rather than an oversight.

### 7.11 The detail pane

**The problem it solves.** v1 carried `Diagnostics`, the `Degraded` field set and every
`Extra` from adapter to renderer and displayed **none of them**. The grid can only draw
one kind of nothing: a dropped column and an em dash both read as "no value here", and
§4a.1 insists there are two different facts underneath. The pane is where the difference
gets said in words. It is the honesty machinery becoming product rather than plumbing.

`enter` opens it on the selected row; `enter` or `esc` closes it. It **replaces** the row
area rather than floating over it, for the same reason the help overlay does — a panel
covering the thing being monitored is a monitor you have to move to read.

**Layout.** Line one is literally the selected row's identity zone (dot, vendor, `│`,
label), so the pane opens where the row was. Everything below hangs off the `SESSION`
column at offset 8: a 12-column muted label, two spaces, then the value. Field order
mirrors the row's three zones — identity, measurement, time — then extras, then the
honesty block, separated by one blank line because it is a different kind of statement.

```
 telltale  │  4 sessions  │  claude 3  codex 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ● CC │ telltale ⑂~2  C:\src\code
        session       00000000-aaaa-4bbb-8ccc-000000000001
        workspace     C:\src\code\telltale
        model         Opus 5
        subagents     ~2 recent
        activity      live · 12s ago
        branch        main
        cli           2.1.219
        ctx tokens    215k

        degraded      —
        diagnostics   —
        not sourced   context_pct, cost, quota

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 esc close   ↑/↓ session
```

Read the last three lines together, because they are the whole point:

- **`not sourced`** is "can't know" — the fields this vendor declared `CapNone`. They get
  no line of their own at all, exactly as the grid drops a column no visible row can
  fill. This line is the answer to "why is this row's CONTEXT cell empty?", and it is the
  first surface in the product that answers it.
- **`degraded`** is "we tried and failed", named field by field. §4a.2 requires degraded
  and plain-absent to render identically in the grid — otherwise "we failed to read it"
  starts to look like data — and this is the one place that difference is legible.
- **`diagnostics`** is why. One line per note, structure only, never transcript content.

A clean session prints `—` on both rather than going blank: a blank honesty block is
indistinguishable from a pane that forgot to render one.

Degraded and absent under real failure:

```
 telltale  │  4 sessions  │  claude 3  codex 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ◐ CX │ 4f2a9c81-1d3e-4a77-9b02-000000000000
        session       4f2a9c81-1d3e-4a77-9b02-000000000000
        workspace     —
        model         —
        context       —
        quota         —
        activity      idle · 7m ago

        degraded      workspace, context_pct
        diagnostics   2 unparseable records skipped
                      no turn_context record in the read window
        not sourced   name, cost, subagents

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 esc close   ↑/↓ session
```

Every `—` above is a field Codex **can** source and has no value for right now; every
name on `not sourced` is one it never could. Same glyph in the grid, two different facts,
and this is where they separate.

**`activity` is the one line that never renders `—`.** It reports the liveness *class*,
which the HUD can always produce, with the age only as the evidence behind it. A session
with no timestamp reads `unknown`, not `—`: the em dash would say "no value" where the
truthful statement is "no basis for a claim" (§4a.4).

**Selection.** `▸` in the row's leading pad column — the column that was already blank,
so selection costs the grid no width. A glyph rather than reverse video, because §7.1
rule 2 says every distinction is carried by a glyph or a number first and a highlight-only
cursor disappears under `NO_COLOR`. The mark is jammed against the state dot (`▸●`) on
purpose: it reads as a pointer at the row's state, and the alternative is a dedicated
column on every row forever to serve a mark that is off most of the time.

```
 telltale  │  4 sessions  │  claude 3  codex 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                                                    MODEL            AGE
▸● CC │ telltale ⑂~2  C:\src\code                                                                  Opus 5        │  12s
 ● CC │ acme-api  C:\src\work                                                                      Sonnet 4.5    │  48s
 ◐ CX │ 4f2a9c81-1d3e-4a77-9b02-000000000000                                                                     │   7m
 ○ CC │ learning-notes ⑂~5  C:\src\code                                                            Haiku 4.5     │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

That frame is also §7.13's: row 2 measured **zero** sub-agents and therefore draws no
chip, and the CONTEXT and COST columns are auto-hidden because these are real Claude rows.

### 7.12 The burn-rate forecast

**What makes this ours.** The incumbents in this lane project a burn line against a plan
budget nobody publishes. That is the exact fabrication decisions/001 exists to forbid, and
§8's "deliberately rejected" list names it. telltale instead samples the vendor's own
`used_percentage` **over its own runtime**, reports the slope it measured, marks it
derived, and states the sampling window beside it. The number is telltale's measurement of
telltale's own observations, which is the one kind of computed figure this product is
entitled to show.

Rendered in the header beside the window it describes, never per row (the §7.1 corollary:
quota is a property of the account):

```
 telltale  │  4 sessions  │  claude 3  codex 1
                                        claude 5h ███─────   42% ↻ 2h13m  ~13:27 · 18m basis  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

Both windows in that frame have the same 7 samples over the same 18 minutes. The 5h window
is moving fast enough to project; the 7d window renders **nothing at all** — not a dash,
not a placeholder, the cell simply ends. That contrast is the feature.

**The four refusals.** A forecast renders only when all of these hold, and each one exists
to prevent a specific lie:

| Condition | The lie it prevents |
|---|---|
| ≥ **3 samples** spanning ≥ **5 minutes** | Two samples fit a line through themselves and cannot disagree; the third is the first one that can. Five minutes because a stepped percentage sampled over ninety seconds measures the step, not the rate. |
| **positive slope** | "You will never run out" is not a time. A flat or falling window renders nothing rather than an infinity. |
| exhaustion **before the window resets**, when `resets_at` is known | Projecting past the reset describes a window that will not exist. |
| exhaustion **within 24 h** | The render is a wall clock with no date on it. `~04:12` sixteen hours out is misleading, not informative. |

**The arithmetic**, pinned by `TestForecastArithmeticIsPinned`: least-squares slope over
the retained samples, projected from the **last observed value**. Least squares rather
than a first-to-last difference because vendor usage percentages move in steps and a
two-point slope is dominated by whichever endpoints straddle a step. Anchored to the last
observed reading rather than to the fitted line so the projection starts from the number
printed next to it — a forecast that quietly starts from 44% while the cell says 42% is a
small lie in the place this product is least allowed one.

**Sampling.** One sample per *completed* scan (a failed scan contributes nothing rather
than a repeat of the last reading, which would flatten the slope with data we did not
measure), throttled to one every 15 s, bounded to 30 minutes and 128 entries. A window
with a nil `UsedPercent` this scan is a **gap, not a reset** — the history stands.

**Rollover clears the buffer**, on either of two signals: usage dropping (monotonic within
a window, so a drop is a rollover), or `resets_at` jumping forward by more than a minute
(a rollover moves it a whole window; jitter does not). Fitting a line across a rollover
reports a negative rate or a wild one, and every sample before it describes a window that
no longer exists.

**Amendment to §7.1 rule 4** ("still by default"), stated rather than quietly taken: the
forecast cell may change when a new sample lands, which is at most once every 15 s and
only when the measurement itself moved. That is a measurement changing, not an animation —
the §7.6 rule is about telltale never animating the *vendor's* state, and this is telltale
reporting its own arithmetic on a new reading.

### 7.13 The sub-agent chip

`⑂~2` after the session label on any row whose adapter counted recently-written
transcripts in that session's `subagents/` sidecar. Sourced by a stat pass (§3.1), Claude
only.

**Why the `~`.** The count is exact — telltale listed the directory. What is *inferred* is
the 15-minute recency boundary that turns "written lately" into "a fan-out is running
now", and ADR-001 requires the inferred part be visible. So the chip carries the same
estimate marker the CONTEXT column does, and it means the same thing: this number was
computed by telltale, not reported by the vendor.

**Zero draws nothing.** The absence of a chip is not a claim, and a `⑂0` on every Claude
row would be noise asserting a fact nobody asked for — the same reasoning as an absent
gauge drawing no track. The measured zero is not discarded, though: the detail pane says
`~0 recent`, where there is room to distinguish "we counted none" from "we could not
count". A sidecar the OS refuses renders `—` there, never `0`.

Styling: the chip renders in `Text`, not `Muted`. `Muted` is this palette's "chrome or
absent" (§7.5, `Absent() = Muted`), and rendering real measured data in it would put a
sourced number in the same visual class as a missing one.

### 7.14 Type-to-filter

`/` opens the query; typing narrows rows by case-insensitive substring; `enter` keeps the
query and hands the keyboard back; `esc` clears it.

```
 telltale  │  2 of 4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 /api_                                                                                          esc clear   enter apply
```

and once applied, with the mode left:

```
 telltale  │  2 of 4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m  7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys                      find "api"
```

Four rules, all of them the same rule — **a monitor that hides rows must say so**:

1. The header count reads `2 of 4`, so the headline can never contradict the per-vendor
   totals beside it. (Same mechanism as the vendor filter; they compose.)
2. An applied query keeps announcing itself in the footer after the mode is gone. A filter
   the user has forgotten about hides rows just as silently as one they cannot see.
3. If nothing matches, the empty state says `no sessions matching "zzz"` rather than
   `no active sessions` — naming the thing that emptied the list.
4. The match is a literal substring, displayed literally. No globs, no regex, no fuzzy or
   embedding search: a syntax that can silently mean something other than what it looks
   like is a filter that hides rows without saying so.

**What it matches:** the vendor's session name, the workspace path, and the session id.
The id is in the set because a torn-record row is *labelled* by its id (§7.3 render D row
3), and matching only the "title" would make the one piece of text on that line unable to
find it.

`/` is a mode, and it is the product's only one — which is why it takes over the whole
footer instead of quietly changing what an unmodified key does.

### 7.15 The quota relay — every vendor the header can honestly speak for

Added 2026-08-07, San's ruling. The header's quota block was one vendor (Codex) because
Codex is the only vendor whose quota exists on disk where a passive reader can see it:
Claude's `rate_limits` arrive **only** on its statusline stdin payload (§3.1 — the live
corpus was grepped, nothing quota-shaped reaches the transcripts), and agy's named
buckets exist only in its statusline payload the same way (§3.8). Cursor's store holds
plan-entitlement constants that must never render as usage (§3.9), and Gemini has
nothing — so those two vendors have no quota **anywhere**, relay or not.

**The mechanism: the statusline relays what it just rendered.** After the line is on
stdout, `telltale statusline` writes the payload's quota windows to
`~/.telltale/quota/<vendor>.json` (`internal/quotacache`), and the HUD's scan reads
every surviving entry alongside the vendor stores. This is a deliberate, scoped
amendment to "the gauges never write" (§1, CLAUDE.md):

- **numbers only, never content** — vendor id, timestamp, window ids/labels,
  percentages, reset instants. The same keys-not-content standard as council's
  `room.json`, pinned by a test that walks the serialized form field by field.
- **atomic and best-effort** — temp + rename in the same directory, error ignored
  after the render is delivered; the cache can never cost a statusline frame or a
  torn read.
- **self-expiring** — the reader drops a window whose reset has passed (its
  percentage is not stale, it is *false*), and whole entries past 24h or stamped
  from the future beyond clock-jitter tolerance.
- **age travels with the reading** — past 5 minutes a relayed block carries
  `· 2h ago` at every dress level, the §7.12 basis rule applied to time: shedding
  the age would re-present a stale number as fresh. Past `quotaAgeWarn` it stops
  being muted chrome and escalates to `· ⚠ stale 19h ago`; §7.17 as amended
  argues the threshold and owns both surfaces' wording.

**One block per vendor, transcript outranks relay.** A vendor sourced from its own
store (Codex) is re-measured every scan; its relay entry, if one ever exists, is as old
as the last statusline render. The scan-fresh reading wins and the vendor renders once.
Only the transcript-sourced block may carry a burn forecast — window ids collide across
vendors (Claude and Codex both have a `seven_day`), and re-reading an unchanged cache
file is not a new observation, so a forecast on a relayed block would be one vendor's
slope pinned to another's account.

**The line fits by shedding decoration, never fact.** Dress levels, tried in order
until one fits: full (names, gauges, countdowns, forecasts) → drop forecasts → names
to two-letter tags → drop gauges (the percentage beside each bar says the same thing)
→ drop countdowns. Vendor, window label, reading, and a stale reading's age survive
every level. If even the barest level overflows, whole trailing blocks are dropped and
an ellipsis says so — the footer's dropping-is-never-silent rule. The generated render
(`quota-fleet` golden):

```
 telltale  │  1 session  │  codex 1
                      ag gemini-weekly 38% ↻ 3h00m  │  cc 5h 42% ↻ 2h13m  7d 6% ↻ 5d00h · 2h ago  │  cx 7d 79% ↻ 22h48m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                                                    MODEL            AGE
 ◐ CX │ notes-api  C:\src\code                                                                     gpt-5.1-codex │   4m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

At 120 columns three vendors and four windows already shed to tags-without-gauges;
the full dress needs ~145. That is the honest trade as measured, not a bug: the bars
went first precisely so every fact could stay.

What the relay does **not** change: the statusline's own display (it renders from
stdin as before, the write happens after), the HUD's read-only posture toward *vendor*
files, and the absence rule — a vendor whose statusline never fires simply never
appears, and one that stops firing ages out.

### 7.16 The token relay — what Cursor cost, from a seam with no network call

Added 2026-08-08. §3.9 declared Cursor's `cost` and `quota` **ABSENT**, and re-verifying
it that day made the verdict harder rather than softer: `usageData` was `{}` in 19 of 19
blobs, `tokenCount` was zero in 1,622 of 1,622 message rows, 78 `turn_ended` records and
51 transcripts carried status and no numbers, and the only account figures anywhere on
disk were Statsig experiment values stamped `is_user_in_experiment:false`. Nothing about
consumption reaches the store as a byproduct of a turn.

So the number was fetched rather than found: **Cursor Hooks**, the vendor's own
documented and versioned contract (cursor.com/docs/hooks), whose `afterAgentResponse`
step hands a command hook the turn's token counts on stdin.

**Why the hook and not print mode — the derived-`inputTokens` trap.** `cursor-agent -p
--output-format json` prints a `usage` block, and reaching for it would have been the
obvious move. It is the wrong one: its `inputTokens` is **not** the raw count. The CLI
publishes `max(raw − cacheRead − cacheWrite, 0)`, measured printing **24,076 where the
un-derived input was 48,012**. Rendering that under the label "input tokens" would be
telltale repeating a vendor's arithmetic as if it were a reading — the ADR-001 violation
this project exists to refuse, and a *quieter* one than usual, because the number looks
perfectly plausible. The hook payload carries the vendor's own `tokenUsage` fields
untouched, which is the entire reason it wins.

Source-read at **cursor-agent 2026.08.04-aaa8809** (`8674.index.js`,
`./src/after-agent-hooks.ts`), where the payload is assembled as
`{conversation_id, generation_id, model, text, input_tokens: tokenUsage?.inputTokens,
output_tokens: …, cache_read_tokens: …, cache_write_tokens: …}` and then enriched by the
executor (`190.index.js`) with `hook_event_name`, `cursor_version`, `workspace_roots`,
`session_id`, `transcript_path` and **`user_email`** before it reaches stdin. Both
Windows transports (`argv_heredoc`, the default, and `windows_temp_file`) deliver that
JSON on the command's **stdin**, under PowerShell.

**The payload is the reason the allowlist is a struct.** This is the first telltale seam
where the numbers arrive in the same object as the model's full reply *and* the user's
email address. `internal/cursorhook` decodes into a four-field struct of integer
pointers; `encoding/json` discards everything with no destination, so no content field
can reach the cache unless someone adds a field on purpose — the technique
`internal/adapter/cursor` already uses against a store that keeps OAuth tokens beside
session state (decisions/007), pointed at a payload that keeps PII beside numbers. A test
plants markers in every content-bearing field of a real payload shape and asserts none of
them survives, at the parser AND again on the serialized cache file.

**Three fields were left out on purpose, and they are not content.** `model` and
`generation_id` are per-turn facts and the entry is a TOTAL — naming one turn's model
beside a sum invites reading the sum as that model's. `conversation_id` names a
cursor-agent **CLI** conversation, and the HUD's Cursor rows come from the **IDE's**
Composer store (§3.9); the CLI keeps a separate one. Storing it would dangle a join that
does not exist, which is also why this reading is not rendered on a session row.

#### The accumulation ruling: a total, and never without its window

A hook fires once per agent response, so the file is either the last turn's numbers or a
running total. It is a **running total**, and the price of that choice is that the window
is not optional:

- a single turn's counts answer a question nobody asks — the turn you just watched
  finish — and go stale the instant the next one starts. A *counter* is the thing a token
  figure wants to be.
- but a sum over an unbounded window is a different and much weaker claim than a reading.
  So the entry carries `since` and `turns`, both travel to the screen, and the renderer
  may never print the sum without them. "48k" is a number pretending to be a state;
  "in 48k · out 1.2k · 14 turns over 12m" is a measurement with its scope attached, the
  same §7.12 basis rule the burn forecast and the relayed quota block already follow.
- the window's boundaries are mechanical rather than chosen. Accumulation continues onto
  any entry a *reader* would still accept, and opens a fresh window otherwise — first
  turn ever, first turn after a day of silence, first turn after a corrupted or
  clock-skewed file. `internal/usagecache.readEntry` is shared by `Add` and `ReadAll`
  precisely so those two can never disagree; without that, a sum could silently span a
  week-long gap and still call itself a total.
- **a partial turn is refused, not part-counted.** A payload missing any of the four
  counts is not accumulated at all. Summing the three that arrived and treating the
  fourth as zero would leave the total wrong by an amount nothing on screen could name,
  while it kept looking like a total; refusing makes the counter go quiet, and a visible
  absence is the failure mode §7.7 prefers every time. Every count in the file is
  therefore a sum of complete readings, and `turns` says exactly how many.

Everything else is §7.15's mechanism copied deliberately, function for function: one file
per vendor under `~/.telltale/usage/`, atomic temp+rename in the same directory,
best-effort, self-expiring at 24h and on future-skew, and the reading's age travelling
with it past five minutes. `internal/usagecache` is a **sibling package** rather than a
second store inside `internal/quotacache` because the two share their mechanism exactly
and their schema not at all — quota is windows with percentages, resets and a
"reset has passed, so this window no longer exists" rule that means nothing to a counter
— and folding them together would put one keys-not-content test in charge of two
unrelated formats.

#### What it rendered, and what stayed absent

*This subsection describes the display as built on 2026-08-08. It was retired on
2026-08-09 — see the amendment below — and is kept in the past tense because the rules it
worked out still bind the one spend line that remains (§7.17).*

**Tokens spent are not quota, and the render may never blur that.** There is no
denominator anywhere in this reading, so no percentage, no gauge, no countdown, no bar —
any of them would invent a ceiling out of nothing, the same class of error as filling a
`CapNone` field with a plausible guess. The spend block therefore got **its own header
line, never shared with quota at any width**, and carried a verb: `cursor spent`. The
verb is a word, not a glyph or a colour, so `--ascii` and `NO_COLOR` lose none of the
claim. It rendered as:

```
 telltale  │  2 sessions  │  codex 1  cursor 1                                         codex 7d █████▌──   79% ↻ 22h48m
                               cursor spent  in 48k · out 1.2k · cache read 1.9M · cache write 62k  · 14 turns over 10m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
```

Codex's quota was on the quota line; Cursor's was nowhere, and Cursor's spend was on a
line of its own. The shed cascade followed §7.15's grammar — cache pair first, then the
turn count, then full names down to the two-letter tags — and vendor, verb, `in`, `out`
and the window survived every level, with the ellipsis saying so whenever anything went.
`theme.Tokens` floors at every step for the same reason `theme.Percent` does: this is
what a machine *spent*, and rounding 47,950 up to "48.0k" invents fifty tokens nobody was
billed for.

#### The display is retired; the relay is not (owner's ruling, 2026-08-09)

**Ruled by the owner: the Cursor spend line comes off every surface. The seam, the hook,
the cache and the HUD's read of it all stay.** The reason was not that the number was
wrong — it was measured end to end and it was right. It was that *it bought no decision*.
A running token count for a vendor with no ceiling anywhere answers a question nobody was
asking, and it was answering it from a header line the header does not have to spare:
§7.15's whole design is a shed cascade fighting for one or two rows, and this was
permanently occupying a third.

That is a product judgement, not an honesty one, and it is worth naming which because the
two have different consequences. Nothing here was retracted. §7.16's measurements, its
vocabulary rules and its accumulation ruling all still stand and all still bind — the
fleet usage view's remaining spend line is held to them (§7.17), and the amendment below
is the first place they were applied to a sum of a different shape.

What changed, exactly:

- **removed:** the header's spend line, and the usage view's Cursor spend row. Nothing
  renders a `usagecache.Total` anywhere.
- **kept, deliberately untouched:** `telltale hook cursor`, `internal/cursorhook`,
  `internal/usagecache` and every test either owns; `~/.cursor/hooks.json` on this
  machine; and the HUD's own read — `Snapshot.Spend` is still filled by every scan and is
  read by nothing. `internal/hud/state.go` says so on the field, because a reader who
  finds an unused field will otherwise correctly conclude it is dead and delete it.
  Reinstating the display is a call site, not a re-plumb, and the accumulating file means
  the day it comes back it has history in it rather than starting from this minute.
- **pinned:** `TestTheCursorSpendDisplayIsRetiredEverywhere` renders the same fixture that
  produced the old block — relayed total still in the snapshot — at five widths, in both
  glyph sets, with the usage view open and closed, and fails on the verb or on either of
  the counts appearing anywhere in the frame.
  `TestTheRetiredDisplayStillHasItsRelayUnderneath` is the other half, and it is the one
  that catches "retired" being implemented as "deleted".
  `TestCursorIsStillGivenNoQuotaBlock` survives from the old pair: losing its spend line
  is exactly the moment a renderer would be tempted to find Cursor a home on the quota
  one.

The same fixture now renders (`cursor-without-spend` golden) — a two-line header where
there were three, Cursor's row still on the grid, and its quota still visibly nowhere:

```
 telltale  │  2 sessions  │  codex 1  cursor 1                                         codex 7d █████▌──   79% ↻ 22h48m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
 ● CU │ Multi-vendor orchestration  C:\src\code                               composer-2.5   ████▏───────    37% │   1m
 ◐ CX │ notes-api  C:\src\code                                                gpt-5.1-codex                    — │   4m



 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   u usage   w week   v vendor   s sort   a all   ? keys
```

#### The boundary amendment

This is the **third** bounded write on a gauge path, and §1 and `CLAUDE.md` name it
alongside council's `room.json` and the statusline's quota relay. It meets the same bar:
under `~/.telltale/`, numbers and keys only, atomic, best-effort, and pinned by a test
that walks the serialized form field by field. It is also the first one written by
neither gauge — `telltale hook cursor` is its own mode, because a hook's stdout is parsed
by the vendor as a hook result, so this is the one path in the binary where printing
nothing is the contract and every exit is clean.

#### Live verification, 2026-08-08 — and the half of it that did not hold

Measured, on Windows 11 at cursor-agent 2026.08.04-aaa8809:

- the relay path end to end, driven by the real binary from a real turn's real counts
  (`input 23,941 / output 33 / cache 0 / 0`, from a print-mode `result` whose zero cache
  figures make its derived `inputTokens` equal to the raw one). Two turns relayed;
  `~/.telltale/usage/cursor.json` read back
  `{"vendor":"cursor","turns":2,"input_tokens":47882,"output_tokens":66,…}` — accumulated,
  with no `text`, no `user_email`, no `conversation_id` and no `model` anywhere in it —
  and the HUD rendered `cursor spent  in 47.8k · out 66 · cache read 0 · cache write 0 ·
  2 turns over 6s` from that file.
- **the vendor never invoked the hook, on any surface reachable from a script.** Marker
  hooks were installed at `~/.cursor/hooks.json` on both `beforeSubmitPrompt` and
  `afterAgentResponse` and neither fired for: `-p --output-format json`, a non-TTY run
  without `-p`, or an **ACP** turn driven over JSON-RPC. The ACP result is corroborated by
  source rather than resting on one capture — the ACP chunk (`8096.index.js`) contains
  **zero** references to `hookExecutor` — and the only call site of the
  `afterAgentResponse` helper sits in the React/ink agent-app module, beside the protobuf
  agent-server dispatcher the IDE talks to. The config itself was not the problem: `type`
  defaults to `"command"` in the bundle's own validator, and an invalid config produced no
  `[hooks]` warning on those paths either, which is consistent with the subsystem not
  being initialized there at all.

So the seam is real and its payload is verified by source read at a pinned version, and
the **invocation is verified for no surface yet**. What is unverified, itemized: that a
true-TTY interactive `cursor-agent` session fires the hook; that the Cursor IDE's agent
server does; and therefore that any figure ever appears without being fed in by hand. The
hook is left wired at `~/.cursor/hooks.json` so the first session that does fire it
captures a total. Worth stating plainly because it bears on the roadmap: **council's
Cursor seat runs on ACP (§9.36), and ACP does not carry hooks** — so the seat that most
wanted per-turn cost is, at this version, the one surface that provably cannot supply it.

#### Known limitations

- **Accumulation is a read-modify-write and takes no lock.** Two hook processes finishing
  in the same instant can lose one turn. Accepted rather than locked: the loss is bounded
  and self-consistent (the `turns` count drops with the counts it names, so the total
  never disagrees with its own window), and the alternative is a lock file on a path a
  vendor's turn is waiting on — a gauge that can hang a turn is strictly worse than one
  that undercounts.
- **`~/.cursor/hooks.json` is un-versioned machine state.** It names an absolute path to
  the binary, so it does not travel; versioning it belongs in the dotfiles repo, not here.
- The counter says nothing about *which* conversation or model spent the tokens, by the
  design ruling above. If a future seam makes a CLI conversation joinable to a HUD row,
  that is a new claim and needs its own section.

### 7.16a The OTLP collector — what grok spent, pushed rather than hooked (2026-08-10)

§3.9a closed grok's quota question three times over and ended on a seam: the vendor's one
designed-for-reporting surface is an external OpenTelemetry stream that carries **spend and
no quota at all**. This section is that seam, spent. `telltale otel grok` is a loopback-only
OTLP/HTTP listener; grok's own exporter pushes to it, and each `grok_code.api_request`
event's four token counts are folded into `~/.telltale/usage/grok.json` — the same cache,
accumulation ruling and refusal gates as §7.16, fed by a push instead of a hook. The
measured export shapes, capture environment and grok version are pinned in §3.9a's export
addendum; `internal/grokotel`'s package doc carries the same facts beside the code.

**Why a listening socket does not breach §4a.5.** The adapter contract's "no network
calls" protects two things: a gauge that can stall on a wire, and a gauge that can *reach
out* — toward an endpoint, with credentials, spending from the pool it reads. The
collector does neither, and the direction of the arrow is the whole argument: telltale
opens a socket on 127.0.0.1 and **the push is grok's**, exactly as §3.9a recorded when it
named the seam. The gauges still make no network calls and read no credentials; they read
the FILE this mode writes, exactly as they read the hook relay's. It is its own mode for
the same reason `telltale hook cursor` is (§7.16's boundary amendment): its I/O contract —
a foreground server holding a port — belongs to neither gauge. The bind refuses any
non-loopback address at startup, mechanically: a collector reachable off-box would be an
open door wearing a gauge's name.

**One source, chosen over a redundant second.** The stream carries the same counts twice —
per-request on `api_request` events, aggregated on the `token.usage` metric — and §3.9a's
capture measured them value-for-value equal. The collector reads the EVENTS and
acknowledges `/v1/metrics` without reading it: one record is one claim, an event carries
all four counts atomically (so §7.16's complete-or-refused gate maps onto it unchanged),
and reading both envelopes would be two chances to count one number. The 200 on the
unread path matters — an unacknowledged export is retried, and making the exporter loop
on a signal nobody reads would spend grok's batches on nothing.

**The window unit is the api request, and the entry says so.** grok's counts arrive per
API call, not per turn (`turn_completed` carries no counts — measured), so the cache
entry's window count is `requests`, a new sibling of `turns` in the §7.16 schema. The
same amendment gave the schema `reasoning_tokens` and made two fields *optional with
their absence meaning something*: a cursor entry carries `cache_write_tokens` and no
`reasoning_tokens`, a grok entry the reverse, because each vendor's file may only claim
the counts its vendor keeps — §4a.1's zero-versus-absent rule, applied to the serialized
form. `TestTheGrokShapedEntryCarriesItsOwnKeysOnly` and the cursor keys test pin both
shapes field by field.

**What the wire carries and what survives.** Every record arrives with `session.id`,
`user.id`, `team.id`, `model` and timing beside the counts; with a content gate open it
would carry prompt text. Four counts survive. The extraction is an allowlist the same way
`internal/cursorhook`'s struct is — an attribute key with no case in the parser falls
through unread — and `TestNothingFromTheWireReachesDisk` plants content markers on a real
record shape (plus a gate-open `user_prompt` event) and asserts nothing but the numbers
reaches the file. `session.id` and `event.sequence` are read into collector *memory* for
one purpose: the exporter retries unacknowledged batches, and a total that counts a
retried batch twice is overstated by an amount nothing can name. A replayed
(session, sequence) pair is refused; the guard is never written to disk.

**The display is held, and it is the owner's own ruling applied.** §7.16's amendment
retired the cursor spend line because a running count for a vendor with no ceiling
anywhere buys no decision — and grok is *more* ceiling-less than Cursor, not less: §3.9a
swept its disk twice, probed the free network half and read the vendor's own monitoring
schema, and no quota exists anywhere. §7.17's Declined already refused grok a spend line
sourced from disk ticks. So this relay ships exactly as the cursor one now stands: write,
cache and the HUD's read of it wired (`Snapshot.Spend` carries the entry; nothing renders
it), display a call site away, and the accumulating file means the day a display is ever
ruled in it has history rather than starting from that minute.
`TestTheGrokSpendRelayRendersNowhere` pins the hold at every width, in both glyph sets,
with the usage view open and closed.

**Wiring it on a machine** (the enable is machine-local config, deliberately not in this
repo):

```toml
# ~/.grok/config.toml — grok's double opt-in, pointed at the default local endpoint
[telemetry]
otel_enabled = true
otel_logs_exporter = "otlp"
```

then leave the collector running while grok runs:

```
telltale otel grok
```

It listens on 127.0.0.1:4318 (OTLP's http default, so the zero-flag pairing finds
itself; `--addr` moves it, loopback only) and prints one line per counted request. The
content gates (`otel_log_user_prompts`, `otel_log_tool_details`) stay off; the collector
keeps nothing they would add, and the planted-marker test is the proof, but a
content-free wire is strictly better than a filtered one. Verified end to end on
2026-08-10: a config-driven `grok -p "hi"` (grok 1.0.0 (3cd0d0cbce), no env overrides,
default batch intervals) against the running collector produced
`{"vendor":"grok","requests":1,"input_tokens":23767,"output_tokens":96,
"cache_read_tokens":1408,"reasoning_tokens":81,…}` — real numbers, keys only.

#### Known limitations

- **The collector must be running to hear the push.** grok's exporter retries briefly and
  then drops a batch; spend accrued while the collector is down is not counted later. The
  counter goes quiet rather than drifting — the §7.7-preferred failure — but "quiet"
  here can also look like "nothing spent", and only the window's `since` says how long
  the file has been accumulating.
- **The replay guard is memory-only.** A batch retried across a collector restart is
  counted twice; bounded by one batch, and accepted for the same reason §7.16 accepted
  its write race — a guard file would be a second store keyed on session ids.
- **A record without `session.id` or `event.sequence` is counted unguarded** rather than
  refused: both ids exist on every measured record, and if a later grok drops them the
  honest failure is a counter exposed to duplicate retries, not one that silently stops.
- **The schema is the vendor's alpha (`grok_code.schema.version = v1`)** and the
  collector does not read the version attribute. A rename lands as quiet non-counting —
  visible as a counter that stops moving, and §3.9a's capture is the shape to re-measure
  against.
- The capture behind every claim here is one machine, one day, one grok version, one
  signed-in account. The §3.4 discipline applies: re-measure before extending any claim.

### 7.17 `u`: the fleet usage view — two claims, and never one

Added 2026-08-09; amended the same day by the reading pass below (the models census, the
title's rule weight, and the age escalation the 19-hour incident forced).

§7.15 gave the header a block per vendor and §7.16 gave it a spend line,
and between them they filled the one or two lines the header has. §7.10's last open
limitation said the quiet part: *"the account quota block is sourced from one session. A
second quota-bearing vendor needs a per-vendor block."* It has one now — but not in the
header, because the header is answering a different question.

**Glance and read are different jobs.** The header answers *am I about to run out?* in the
time it takes to look up from an editor, and its whole design is a shed cascade that spends
decoration to keep facts on one line (§7.15). It cannot also answer *what can telltale
actually say about each of my five vendors, and where it says nothing, why?* — that answer
is a paragraph per vendor, and a paragraph per vendor is a body, not a header. So `u` opens
a third body over the row area, on the detail pane's precedent (§7.11): it replaces the
grid rather than floating over it, because a panel covering the thing being monitored is a
monitor you have to move to read.

**The header was left unchanged, and that was wrong for this one body** — see *the header
stops repeating the page* below, which reverses it. Over the GRID the two surfaces render
the same readings from the same assembly and the duplication is the point, one for glancing
at and one for reading. Over this body there is nothing left to glance at: the reader is
already looking at the read surface.

#### The organizing insight, and it came from the measurement

"Usage" is **two different claims**, and the view may never blur them.

| | **Quota** | **Spend** |
|---|---|---|
| what it is | a reading against a limit the vendor published | a count of tokens with no denominator anywhere |
| sources today | Codex (its own store, scan-fresh); Claude and agy (statusline relay) | agy (summed from the conversations this scan read, §3.8) |
| may render | gauge, percentage, reset countdown, severity hue | a verb, the counts, and the accumulation window |
| may never render | — | a gauge, a percentage, a countdown, a bar, or the sum without its window |

The right-hand column is not a style preference. There is no ceiling anywhere in a token
count — no vendor here publishes an account limit a passive reader can see (§3.8, §3.9,
§3.9a) — so a bar or a percentage would **invent** one, which is the same class of error
as filling a `CapNone` field with a plausible guess.
`TestUsageSpendBorrowsNoneOfQuotasVocabulary` pins it, and since the header's spend line
was retired (§7.16's amendment) that test is the *whole* of the guarantee rather than the
second half of a pair. It matters most here anyway: this surface puts the two measurements
four lines apart under one vendor name instead of on separate header rows, and
**proximity is what makes this the riskier render of the two**.

#### The layout

One block per vendor, in **fixed fleet order** — claude, codex, gemini, agy, cursor, grok —
never sorted by usage. Position is the navigation: a vendor moving must mean a vendor was added
or removed, not that another vendor's percentage crossed it. `fleetOrder` is now one
variable, walked by both the header's per-vendor counts and this view, so a vendor sits in
the same place on both surfaces.

Each block is a heading that states the **quota seam** — where the reading came from, or
why there is none — and then one line per fact under it, hanging off the detail pane's
label column so the two bodies read as one product. The generated render (`usage-fleet`
golden, and the shape it took after the 2026-08-09 amendment below):

```
 telltale  │  7 sessions  │  codex 1  gemini 1  agy 3  cursor 1  grok 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 fleet usage  quota is a reading against a limit; spend is a count with none  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

 claude  quota relayed by the statusline · 2h ago
        5h             ████████────────────    42%  ↻ 2h13m
        7d             █▏──────────────────     6%  ↻ 5d00h

 codex  quota read from its own store, this scan
        models         gpt-5.1-codex
        7d             ███████████████─────    79%  ↻ 22h48m

 gemini  no quota reaches disk anywhere telltale can read
        models         gemini-3-pro

 agy  quota relayed by the statusline
        models         Gemini 3.6 Flash (High)
        gemini-weekly  ███████▎────────────    38%  ↻ 3h00m
        spent          uncached in 1.2M · out 13.1k  · summed across 2 sessions on disk, this scan

 cursor  no quota anywhere · its store holds experiment values, not usage
        models         composer-2.5

 grok  no quota anywhere · no window, no ordinal, no reset time on its disk
        models         grok-4.5
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

 esc close   ↑/↓ scroll
```

Five things in that frame are doing work:

- **The gauge is 20 cells, not the header's 8.** That is the concrete reason this view
  exists rather than a wider header: one window per line buys the bar room to be *read*
  rather than merely glanced at. Fill resolution goes from 1.6% to 0.66%. It sheds to 12
  cells at the compact tier and disappears below 80 columns, on §7.2's own breakpoints
  rather than on new ones — and it is allowed to shed only because the number beside it
  stays.
- **The heading names the source, and §7.15 makes that load-bearing.** A transcript-sourced
  block is re-measured every scan; a relayed one is exactly as old as the last statusline
  render, and only the first may ever carry a burn forecast. A reader deciding how much to
  trust a percentage needs to know which they are looking at. The relayed reading's **age
  survives every dress level** — the phrase around it gives way instead — because shedding
  the age would re-present a stale number as fresh.
- **`agy` carries both kinds of claim, and they arrive from two different seams.** Its
  percentage is relayed by the statusline; its token counts are summed out of the
  conversations this scan read. The heading always speaks about quota and never about
  spend, deliberately: the spend line explains itself in its own vocabulary (a verb and a
  window), while an absent or relayed reading explains nothing at all unless something says
  it out loud.
- **`gemini` is one line, and it is a line rather than a row of dashes.**
- **`cursor` and `grok` have a sentence each and no numbers, and the sentences differ**
  because the measurements behind them do.
- **The header above it is identity only.** It carried the quota strip when this view
  shipped; the amendment below took it off, because every figure on it is restated
  underneath with room.

#### Three kinds of nothing, and the one that had to collapse

§4a.1's rule is that the kinds of absence stay distinct. On this surface there are three,
and they are the whole reason the absence line carries a reason rather than an em dash:

| | Vendors | What it renders | Why that wording |
|---|---|---|---|
| **structurally absent** | gemini, cursor, grok | `no quota reaches disk anywhere telltale can read` / `no quota anywhere · its store holds experiment values, not usage` / `no quota anywhere · no window, no ordinal, no reset time on its disk` | There is no seam to fire. Cursor's verdict was re-measured 2026-08-08 and came back harder: the only account figures on its disk are Statsig experiment values stamped `is_user_in_experiment:false`, never consumption (§7.16). grok's was measured the same way on 2026-08-09: a rate/limit/quota sweep of the whole store matched tool-configuration keys and nothing else (§3.9a). Naming an action here would send someone to enable a thing that does not exist. **Each vendor gets its own sentence rather than sharing one** — the verdicts are the same shape and different measurements, and lending grok Cursor's wording would claim something about grok's disk that nobody looked for there. |
| **seam exists, never seen** | claude, agy | `no quota relayed yet · the telltale statusline writes it` | This is the one absence a user can act on, so it **names the statusline**. The reading turns up as soon as the gauge runs in that vendor. An absence with an action behind it that does not say the action is just a shrug. |
| **aged out** | any relayed vendor | *renders as never-seen* | `quotacache`'s reader drops a window whose reset has passed and any entry over 24h old before the HUD ever sees it (§7.15's self-expiry). Telling the two apart would mean **holding numbers §7.15 calls not stale but FALSE** so this view could display them. Losing one distinction is the cheaper of those two trades — and it is recorded as a limitation below rather than left to be discovered. |

Codex is in none of the three: its quota comes from its own store, so an absence there is a
statement about what this scan read (`no quota in the sessions read this scan`) rather than
about a relay that never fired. Borrowing either of the other two sentences for it would
name the wrong seam.

**A vendor with no sessions, no quota and no total does not appear at all.** The view is a
report on what is running, not a checklist of every adapter that was compiled in — and an
absence line is for a vendor that is *here* and silent, not for one that is not here. When
nothing anywhere has anything to say, the body is a sentence rather than five blocks of
dashes, because a table of nothing is a table asserting it measured five things
(`usage-empty` golden):

```
 telltale  │  0 sessions
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 fleet usage  quota is a reading against a limit; spend is a count with none  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

        no vendor on this machine has reported a quota reading or a token count
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────



 esc close   ↑/↓ scroll
```

#### The reading pass (amended 2026-08-09): it worked, and it did not read like a page

The view above shipped the same day it was designed and it was *correct* — every claim in
it is measured, every absence names its seam. Asked whether it was the best the product
could do, the answer was no, and for three separable reasons: it did not say **which
models did the work** (half of the original ask, dropped in the first cut), it had **no
visual nesting** (a body title at the same weight and the same column as its own entries),
and an **old reading looked exactly like a fresh one** apart from four muted characters.
The third one had already cost something real, which is why it is the longest item here.

**The census: which models actually did the work.** Each vendor block now carries a
`models` row naming the model display names this scan saw under that vendor.

```
 telltale  │  6 sessions  │  claude 4  codex 1  gemini 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 fleet usage  quota is a reading against a limit; spend is a count with none  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

 claude  no quota relayed yet · the telltale statusline writes it
        models         Haiku 4.5, Opus 5, Sonnet 4.5

 codex  quota read from its own store, this scan
        models         gpt-5.1-codex
        7d             ███████████████─────    79%  ↻ 22h48m

 gemini  no quota reaches disk anywhere telltale can read
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 esc close   ↑/↓ scroll
```

It is a **third kind of line**, and it belongs to neither of the two claims this view is
built around. A census has no limit and no total — nothing to compare it against — so it
borrows neither vocabulary and simply lists what was there. What governs it is the same
rule everything else here obeys:

| Rule | Why it is the honest-gauge rule again |
|---|---|
| **Only this snapshot** | Never a remembered list and never the vendor's catalogue. A name surviving from a previous scan would be a claim about the past presented as the present — the defect the relay's age exists to prevent, arriving through a different door. Four Claude sessions above; take them away and the row goes, even though the vendor still has a quota reading. |
| **The grid's own normalization** | Through `DisplayModel`, so `claude-opus-5` reads `Opus 5` here and in the MODEL column, and a reader never has to work out that two spellings are one model. |
| **Deduped, sorted** | Four sessions, three names. Alphabetical rather than by recency or by session count: ranked ordering reshuffles every time a turn lands, and §7.1 rule 4 budgets the movement on this screen at one cell. |
| **Absent renders absent** | A vendor whose sessions carry no model gets **no row** — `gemini` above has a session and no census. Not an em dash: the dash would claim telltale looked at a model and could not name it, when what happened is that the adapter sources no model at all. |
| **Overflow is announced** | `+3 more`, never a clipped list. The cap is the *room* rather than a magic number, and names are dropped whole rather than cut, with the marker's own width reserved before the last name is accepted. At the 60-column floor: `        models         Haiku 4.5, Opus 5  +3 more`. An ellipsis there would leave the reader unable to tell whether one name went missing or nine. |

**It is its own row rather than part of the heading**, and that was the one real layout
fork in this pass. The obvious placement is beside the vendor name —
`claude · Opus 5, Sonnet 5   quota relayed by the statusline · 2h ago` — and it is wrong
for two reasons that point the same way. First, the heading has one job and §7.17 already
made it load-bearing: it states the **quota seam**, and the relayed reading's age may
never shed from it. A second variable-length vendor-supplied fact on that line puts a
census in competition with the one thing on the surface that is not allowed to give way,
and at 60 columns the census wins by being at the front. Second, the census is a *session*
fact aggregated per vendor while the heading speaks about the *account* — putting them on
one line blurs exactly the distinction the block's shape exists to draw. In the label
column it instead joins `5h`, `7d` and `spent` as a fourth labelled fact about one vendor,
which is what the column is for.

**Within the block it comes first**, before the quota windows and before spend: it names
who did the work and the rows under it say what that work cost, subject before predicate.
It does not tear the heading from its evidence, because the heading states a provenance
("relayed by the statusline") rather than a number.

#### The title carries the room's second rule weight

`fleet usage` and ` claude` started in the same column at the same weight, so the body's
**title read as a peer of one of its entries** — §9.23's finding one surface over, where a
turn page's outline whispered while its entries shouted. The HUD had one rule weight and
was asking it to be both the frame's edge and a gauge's empty track.

`RuleHeavy` is `━` and `=`, **council's own pair** rather than a second one: §7.1 principle
5 is that these are one product, and a second heavy-rule character would be a second
alphabet. `=` is the one unclaimed mark left in the HUD's reduced set — `-` is the light
rule and the gauge track and the fact separator and a spinner frame, `#` the gauge fill,
`|` the separator, `>` the ellipsis, `~` the reset, `!` the warning, `]` the cursor, `Y`
the fan-out, `*`/`o`/`.` the state dots, `_` the caret — and
`TestTheHeavyRuleHasAnUnclaimedASCIIPartner` enumerates that list so the next glyph cannot
be added without meeting it.

**The rule goes ON the title, not under it**, and that is council's ruling rather than a
preference: §9.11 spent a whole item removing a heading followed by a horizontal rule, on
the finding that such a rule says nothing the heading had not, and ruled that a heading
carries its own. Here it also avoids a specific defect — the frame's own light full-bleed
rule sits one row above, and a *heavier* line three rows inside a lighter outline is
§9.26's hierarchy argument inverted. It costs **zero rows**, which is what makes it
affordable on a body with a line budget, and it yields to the legend rather than the other
way round: the note is fitted first and the rule takes what is left, because a legend is a
statement and a rule is chrome. At the 60-column floor that leaves ten cells; below
`usageRuleMin` the line simply has no rule.

**The vendor headings deliberately get no rule, not even the light one**, and
`TestOnlyTheUsageTitleDrawsTheHeavyRule` asserts the weight as a *count* on the rendered
frame — one line, one run, and zero on every other body. §9.26's argument is that a second
weight is worth exactly what it is scarce; a rule on every block would spend it five times
a screen to restate what an indent, a blank row and the identity hue already say.

#### Air and alignment: what was already right, and what is now pinned

The columns were already shared — one `usageLabel` cell and one `usageGap` for every fact
row, so `claude`'s `5h` gauge starts where `codex`'s `7d` gauge and `agy`'s
`gemini-weekly` gauge start — and the blocks were already one blank row apart. This pass
**pinned that rather than built it**, which is the honest description:
`TestUsageFactsShareOneColumnGridAcrossVendors` now walks the rendered body at four widths
and fails if the label column, the gauge, the percentage or the reset countdown lands in
two different columns across vendors, and if any label runs into the value column. Before
it, the models row could have been added with a layout of its own and nothing would have
noticed.

Two things were considered and **declined**. A second blank between blocks: §9.11's
threshold is that every deliberate blank this product draws is exactly one row, and a
one-row gap is a boundary placed between two things meant to be kept together — two rows
is nothing the design asked for. And a light rule under each vendor heading, for the
scarcity reason above; air is the boundary strength this body can afford, and it already
has it.

> **The first of those was reversed on 2026-08-09** — narrowly, and only where the rows are
> otherwise going to waste. See *the page stops trailing off*. The rule under each vendor
> heading stands declined.

#### An old reading has to look old (the 19-hour incident)

**What happened, 2026-08-09.** A Claude relay entry written nineteen hours earlier
reported 15% of the seven-day window. It rendered at full confidence beside a live gauge
and was read as current. The account was at 44%.

Nothing in it was dishonest. The age was on screen — `· 19h ago`, exactly as §7.15
requires — and every part of the state is one the product genuinely reaches: the five-hour
window was gone because `quotacache` drops a window whose reset has passed, the entry
survived because it was inside the 24h ceiling, and the reset it reported was still four
days out so nothing upstream had any reason to touch it. **The age was present and it was
not loud.** A muted four-character suffix is the same weight as every other piece of
chrome on that line.

```
 telltale  │  7 sessions  │  codex 1  gemini 1  agy 3  cursor 1  grok 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 fleet usage  quota is a reading against a limit; spend is a count with none  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

 claude  quota relayed by the statusline · ⚠ 19h ago · older than the fleet's shortest quota window
        7d             ██▉─────────────────    15%  ↻ 4d07h

 codex  quota read from its own store, this scan
        models         gpt-5.1-codex
        7d             ███████████████─────    79%  ↻ 22h48m

 gemini  no quota reaches disk anywhere telltale can read
        models         gemini-3-pro

 agy  quota relayed by the statusline
        models         Gemini 3.6 Flash (High)
        gemini-weekly  ███████▎────────────    38%  ↻ 3h00m
        spent          uncached in 1.2M · out 13.1k  · summed across 2 sessions on disk, this scan

 cursor  no quota anywhere · its store holds experiment values, not usage
        models         composer-2.5

 grok  no quota anywhere · no window, no ordinal, no reset time on its disk
        models         grok-4.5
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

 esc close   ↑/↓ scroll
```

**Five hours, and the number is argued rather than tuned.** `quotaAgeWarn` is the shortest
quota window telltale has measured anywhere in the fleet — Claude's `five_hour` (§3.1) —
and therefore the shortest span over which a vendor is known to reset a limit *wholesale*.
A relayed reading older than that has outlived the fastest-moving quota this product knows
about: whatever window it reports, an entire window of the shortest kind could have opened
and closed since it was taken, so the reader may no longer assume the number describes
now. Below five hours the reading is old and still bounded by something; above it there is
no window short enough to bound it. It pairs with `quotaAgeShown` (5m, where the age starts
rendering at all) as the two boundaries of a relayed reading's life on screen.

Three thresholds it deliberately is **not**:

- **Not the per-block shortest window**, which was the first candidate. `model.QuotaWindow`
  carries no duration — a label, a percentage and a reset time — so a per-block rule would
  have to infer a length by parsing `"5h"` or `"seven_day"`, and a threshold derived from a
  display string is the class of guess §4a.1 rejects. It would also have failed on the very
  reading that prompted this: the expired 5h window was already gone, so the surviving
  block reported only `7d` and a per-block rule would have stayed silent at nineteen hours.
- **Not a second reason to drop a reading.** `quotacache` owns expiry (§7.15) and this view
  renders everything it is handed, changing only how loudly. Dropping earlier than the
  reader does would hide a measurement telltale holds.
- **Not a freshness gauge.** There is no denominator for "how fresh", which is the spend
  line's argument in a different costume.

**Word first, glyph second, hue third**, in that order, because §7.1 rule 2 is that colour
never carries a distinction alone. Past the threshold the heading's dress gains the warning
glyph beside the age *and* the reason in words — `· ⚠ 19h ago · older than the fleet's
shortest quota window` — and the whole statement renders in `SevWarn`, which §7.5 already
defines as the token for warning notices and which the footer's own `⚠ last scan 1m ago`
already uses. In the reduced set that line reads
`claude  quota relayed by the statusline - ! 19h ago - older than the fleet's shortest quota window`.

It says "the fleet's" rather than "its" on purpose: five hours is the shortest window in
the fleet, not necessarily in this block, and an agy weekly reading six hours old has not
outlived its own window. Trading one over-confident render for one over-stated warning is
not a trade.

The shed cascade is unchanged in grammar — the age is the fact and never sheds, the
sentence explaining it is decoration and does, so the barest level is `⚠ 19h ago`. What a
narrow terminal loses is the argument, not the alarm. And the **reading itself is
untouched**: the 15% keeps its own severity hue, because that is a statement about the
account and this is a statement about the measurement.

**There is exactly one step.** §9.26's lesson is that a second level is worth what it is
scarce, and the only boundary above this one that is not invented is `quotacache`'s 24h
drop — which is a disappearance rather than a louder warning.

#### Amended 2026-08-09: the header escalates too

The pass above deliberately left the header alone, and recorded that as unfinished work
rather than as a boundary. It was the wrong half to fix first: the reading that was acted
on was read off the **glance** line, so the product ended up loud on the surface a reader
opens on purpose and quiet on the surface they merely look at. The header now escalates on
the same threshold, in the same order, off the same constants — `quotaAgeWarn` and the
reason string are read from §7.17's declarations rather than restated, because two copies
of `5 * time.Hour` is how the two surfaces would come to disagree about one reading.

Past the threshold the header's age suffix becomes `· ⚠ stale 19h ago`, in `SevWarn`, with
`· older than the fleet's shortest quota window` beside it at the most dressed level. The
reduced set renders `- ! stale 19h ago`. The reading itself is untouched — 15% keeps its
own severity hue, for §7.17's reason: that is a statement about the account and this is a
statement about the measurement.

**The header keeps a WORD where the view's barest level does not**, and that is the one
place the two surfaces deliberately differ. §7.17's shed bottoms out at `⚠ 19h ago`,
because by then the reader has opened a body on purpose and the sentence above it is still
on screen. The header has no sentence anywhere and is read at a glance, so the level that
survives every shed has to state the verdict without the reader knowing what `⚠` is
supposed to cost them. `stale` is that word: it names a state rather than restating the
duration (`19h old` would be the number a second time), and it is the word this codebase
already spends on a reading that has outlived its currency — `DotStale`, and the footer's
own `⚠ last scan 1m ago`.

**What sheds is the argument and only the argument.** `ageReason` joins the dress ladder as
a sixth level above the existing five and is the first thing dropped, ahead of forecasts:
it is by a wide margin the longest clause on the line, and it is the only part of the
escalation whose absence costs a reader something they cannot otherwise see. Everything
below it — glyph, word, age — survives to the barest level, so a narrow terminal loses why
the reading is distrusted, never that it is. Same grammar as the view, one surface over.

#### Everything else is inherited, not invented

- **`u` opens and closes it; `esc` closes it.** The esc chain gains a step and now reads
  usage → detail → help → clear the query → quit. One body at a time: opening any of the
  three closes the others, enforced in `Update` rather than in `Render`, because a pane
  that appears only because it won an ordering is a pane nobody can predict. Find mode
  closes it too — but only in that direction, since once the mode has the keyboard `u` is
  a letter (§7.8).
- **`u usage` joins the footer hints** beside `enter detail`, and sheds on the same tier
  boundary for the same reason: below 80 columns the footer keeps only the keys nothing
  else can teach. It is on the help overlay's keys page at every width.
- **The drift notice renders under this body too** (§7.3), like every other. A warning that
  comes and goes depending on which pane is open is one a reader cannot trust to be there.
- **Overflow scrolls** with the help overlay's vocabulary — `↑`/`↓` move the body, bounded
  against its own rendered length — not the grid's `+N more`, which counts sessions.
- **The 60-column floor and the height tiers are unchanged**, and nothing here animates
  (§7.1 rule 4). At the floor the bars are gone and every fact survives, including the
  relayed reading's age and the spend total's window.
- **The view is not narrowed by `v` or by the find query.** Those narrow the *session*
  list, and nothing on this surface is a session fact — filtering an account reading by a
  session filter would be the per-row quota §7.1 forbids, arriving by the back door.

#### Declined

- **Trend sparklines.** The caches hold one reading per vendor, so a trend line would be
  drawn from data that does not exist. The burn forecast (§7.12) is the honest version of
  this and it is confined to the one scan-fresh block that can support it.
- **Sorting by usage.** Position is the navigation; see the fleet-order note above.
- **Per-row quota.** An account fact is not a session fact — §7.1's sixth rule, and the
  reason this view exists at all.
- **A fabricated fleet total.** Different units (percentages of unrelated windows, and raw
  token counts), different accounts, different vendors. Any single number across them would
  be arithmetic telltale invented, which is the ADR-001 violation this whole product is
  built to refuse.
- **A spend line for grok**, added 2026-08-09 and the closest call in this section. grok
  is the only vendor here that writes real money to disk, so it is the one block where a
  reader might expect a figure — and it gets none. The only cost on its disk is
  `usage.costUsdTicks` on each `turn_completed` record: per-turn, not cumulative,
  in an append-only file that reached 818 KB in one session (§3.9a). A tail-window sum is
  a **lower bound**, and a lower bound rendered next to the word "spent" is a derived
  number wearing a read one's clothes. The last turn's cost is already a labelled Extra in
  the detail pane, where its label says which turn it belongs to. Nothing on grok's block
  says any of this: the heading speaks about quota and never about spend, and buying one
  vendor an exception would cost every other block its meaning.

#### Amendment, 2026-08-09: the owner's ruling on which vendors this speaks for

Ruled by the owner: **Cursor's spend display goes; agy and grok come on.** The retirement
half is §7.16's amendment. This is the half that adds.

**agy's spend line, and why its window reads differently.** The source is the agy
adapter's measured per-conversation token counts — `gen_metadata`'s `#1.#4.#2` (uncached
input) and `#1.#4.#3` (output), guarded by the `thinking + answer == output` identity §3.8
requires and the adapter asserts. Those were already on screen per row as display-only
extras; what is new is `model.Session.Tokens`, the same two numbers as integers, summed
per vendor by the view. The integers exist because a sum of pre-rounded display strings is
a sum of roundings; the adapter sets both in the same branch from the same variables, so a
row and the fleet total cannot disagree about what was counted.

**This is a scan, not a meter, and the wording has to carry that.** Cursor's total was a
file that only ever went up. agy's is a sum over the conversations that are on disk at
this moment, so *deleting a conversation makes it smaller*. §7.16's rule — the sum never
prints without its window — therefore binds against a different window:

- the wording is `summed across 2 sessions on disk, this scan`, and it never says "since
  <date>". A "since" is a meter's claim and this is not a meter.
- the shed cascade drops words and never facts: `summed across N sessions on disk, this
  scan` → `summed across N sessions on disk` → `across N sessions on disk` → `N sessions
  on disk`. The **count** survives every level because it *is* the window, and **"on
  disk"** survives every level because it is the difference between this sum and a
  monotonic one. "summed" and "this scan" are what give way, in that order.
- the count is of the sessions that **contributed a measured reading**, not of the vendor's
  sessions. The fleet fixture's agy has three conversations and the line says two: the
  third has not called a model yet, carries no counts, and is in neither the sum nor its
  window. Folding it in as a zero would put a session in the denominator that contributed
  nothing to the numerator — §4a.1's rule applied to a window instead of to a cell.
- a generation that **failed its self-check** is dropped by the adapter and named in that
  row's Diagnostics, which is where a reader finds out a total is over fewer generations
  than the conversation ran. That is inherited behaviour, not new, and it is the one place
  this surface is quieter than the row it summed: the count says how many sessions, not how
  many generations inside them were refused. Recorded as a limitation below.
- the label is **`uncached in`**, not `in`. §3.8 marks the cache-read component's field
  number lower-confidence and the adapter refuses to fold it into a rounder total;
  labelling the number `in` would quietly promote a partial figure to a whole one, and the
  fleet line is the one place a reader could not catch that.

**And it wraps rather than sheds, which nothing else on this surface does.** Every other
line here sheds decoration — a gauge that re-states a number still on screen, a phrase
around an age. This line is facts only: two counts, a label that has to say "uncached",
and a window that may not go. At 60 columns those do not fit on one row and there is
nothing left to spend. The header solved that class of problem by dropping whole vendor
blocks, because a header has a hard one-or-two-line budget; **this is a body and it
scrolls**, so it can pay a second row, and a second row costs a reader nothing next to a
sum that has stopped saying what it summed. The window hangs under the counts in the same
indent and re-runs its own cascade there — a row to itself buys back dress the shared row
could not afford, so the *narrow* render says more about the window than a cramped
single-line one would have. It carries no leading glyph: the mid dot separates facts on a
line everywhere else in this product, and giving it a second job as a continuation mark is
the failure §9.26 is a whole section about. `usage-floor` (60 columns):

```
 agy  quota relayed by the statusline
        models         Gemini 3.6 Flash (High)
        gemini-weekly     38%  ↻ 3h00m
        spent          uncached in 1.2M · out 13.1k
                       summed across 2 sessions on disk
```

**grok's block** is the absence table's third structural row, above. It qualifies for a
block on sessions alone, and it would have rendered the un-surveyed fallback
(`no quota telltale can read`) — honest, and a step down from a sentence that names what
was measured. It now says `no quota anywhere · no window, no ordinal, no reset time on its
disk`, from §3.9a's sweep. It has no spend line, for the reason in Declined above.

Added by the reading pass:

- **A freshness gauge.** No denominator for "how fresh", so a bar would invent one — the
  spend line's argument, one field over.
- **A second escalation step for the relay's age**, and a per-block threshold parsed out of
  a window label. Both above.
- **A light rule under each vendor heading.** The air-and-alignment note above. (A second
  blank row between blocks was on this list too, and came off it — see below.)

#### Amended 2026-08-09: the header stops repeating the page

**The defect, from driving the real thing.** With the usage body open the header still drew
the full quota strip — two cramped rows of
`ag 3p-5h 0% ↻2h10m 3p-weekly 0% … cc 5h 40% ↻2h17m 7d 62% … cx 7d 37% … cursor spent in
47.8k out 66 …`. Every fact on them is stated properly in the blocks four rows below, with
a label column, a 20-cell gauge and the provenance sentence the strip has no room for. It
was the same measurement twice, once cramped and once legible, with the cramped copy on
top, and it was the ugliest thing on the screen.

**Over the `u` body the header collapses to identity and session counts** —
`telltale │ 144 of 1380 sessions │ claude 961 codex 299 …` — and the quota strip does not
render. The grid keeps today's header exactly. Two rows come back to the body.

This **reverses #163**, which ruled the header untouched when this view landed. #163 was
right for the grid and wrong for this page, and the principle worth keeping is the one that
distinguishes them:

> A **glance** surface may not repeat the **read** surface it sits above.

The glance line earns its rows by being the only statement of a fact. Over the grid it is:
account quota appears nowhere else, so the strip buys a fact that is otherwise off screen.
Over the page built to state those same facts at length, it buys nothing and costs two of
the rows that page is short of. Identity survives the collapse for exactly that test — the
session census is not restated anywhere below, because the usage blocks speak about
accounts and never about sessions.

One consequence worth naming: at the 60-column floor the recovered row is enough for
`grok`'s census row to fit, which it did not before. The fix pays for itself in content on
the narrowest terminal telltale renders on.

#### Amended 2026-08-09: the page stops trailing off

**The defect, same session.** On a tall terminal the content stopped around 40% of the way
down and the rest was blank. Nothing was wrong with any line; the page simply had no bottom.
A surface built to be *read* trailed off like a truncated file.

Council solved this exact problem twice (§9.23's contiguous rails, §9.11's boundary-strength
grammar) and neither answer ports: the HUD has no rails, and importing them would give this
one body a vocabulary no other surface in the product speaks. The fix is in the HUD's own
language, and it is two moves that only work together.

**1. The closing rule hugs the content.** The frame's bottom rule was already being drawn —
sixty rows below the last vendor block, where it reads as the terminal's edge rather than as
the page's. Moved to where the content stops, the same rule makes the body a bounded region
with a visible bottom edge, and the leftover rows fall *outside* it: unused terminal, not
unfinished page. This is §9.11's boundary-strength grammar with the weight the frame already
owns, moved — no new glyph, no new hue, no second rule. The grid is untouched, because its
body is a list and a list that ends early has ended; a hard edge under a row area still open
for more rows would claim something false.

**2. The blocks breathe into two-row gaps.** This is the reversal of the air-and-alignment
ruling above, and it is narrow. That ruling was made under a **line budget**, on the
assumption that a row spent on air is a row taken from a fact. On a tall terminal the
assumption is false — the rows are there, unspent — and the real choice is air versus void.
So the second row is never a constant and never a fiat: it appears only when the widened
gaps consume fewer rows than the page would otherwise leave blank at the bottom, which makes
it a **redistribution of air the page already has**. Short terminal, tight page, exactly as
before. The gap before the *first* block never grows, so the page still starts where the
page starts — the §7.x anchor rulings hold and this is not vertical centring.

**It stops at two, and the cap is the argument.** Distributing all the surplus — justifying
the blocks down to the closing rule — was the obvious alternative and is declined twice
over. It makes gap height a function of terminal height and block count, so the distance
between two vendors would encode nothing while looking like it encoded something. And it
puts every gap on a variable, so one grok session appearing reshuffles the vertical position
of every block below it, against §7.1 rule 4's one-cell churn budget. Air that says "these
are separate things" has to be a constant to say it.

Also declined: **inventing content to fill the space** (a fleet total is §7.17's own
rejected list; anything else would be a number nobody measured), and **centring the block
vertically**.

The `usage-tall` golden pins both halves at 52 rows:

```
 telltale  │  7 sessions  │  codex 1  gemini 1  agy 3  cursor 1  grok 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 fleet usage  quota is a reading against a limit; spend is a count with none  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

 claude  quota relayed by the statusline · 2h ago
        5h             ████████────────────    42%  ↻ 2h13m
        7d             █▏──────────────────     6%  ↻ 5d00h


 codex  quota read from its own store, this scan
        models         gpt-5.1-codex
        7d             ███████████████─────    79%  ↻ 22h48m


 gemini  no quota reaches disk anywhere telltale can read
        models         gemini-3-pro


 agy  quota relayed by the statusline
        models         Gemini 3.6 Flash (High)
        gemini-weekly  ███████▎────────────    38%  ↻ 3h00m
        spent          uncached in 1.2M · out 13.1k  · summed across 2 sessions on disk, this scan


 cursor  no quota anywhere · its store holds experiment values, not usage
        models         composer-2.5


 grok  no quota anywhere · no window, no ordinal, no reset time on its disk
        models         grok-4.5
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────




















 esc close   ↑/↓ scroll
```

#### Per-vendor hues: ratified, and exactly as far as council's went

Answered 2026-08-09 (San), and it is a **yes**. The question §7.17 left open — whether
council's seat-hue exception (§9.28) extends to this surface's vendor names — turned on
whether the HUD has the concept the exception was granted for. It does, on this body and
nowhere else: **six vendor blocks stack in one column**, each a heading with a paragraph
under it, so position answers nothing about which vendor a reader is looking at. That is
the exact condition §9.28 named. The grid does not qualify and does not get it — a row's
vendor is already answered by a two-letter tag in a fixed column.

It arrives on the terms the open question itself set, and every one of them is council's
rather than a new argument:

- **Names only.** The vendor NAME in each usage-block heading, and nothing else. Not the
  seam sentence beside it (chrome, and past `quotaAgeWarn` a warning that must keep
  `SevWarn`), not the gauges, percentages or countdowns (severity owns those), not the
  spend line, not the models census (theme's identity hue, the same token the grid's
  `MODEL` column spends), not the grid rows, and not the header's quota block — the glance
  surface names its vendors by tag in a fixed order, so the question the hue answers is not
  the one it is asking. `TestTheVendorHueIsSpentOnlyOnTheVendorName` walks the rendered
  frame and fails if a hue reaches the header, a fact row or the footer.
- **The assignments are council's, matched by literal value** — claude `5`, codex `6`, agy
  `4`, cursor `12`, grok `14`, gemini falling back to the identity hue. A reader who learned
  in the room that magenta is Claude must not meet a second colour for Claude one keypress
  away. `TestVendorHuesMatchCouncilsSeats` writes council's numbers out and fails when one
  copy moves without the other — the shape `TestStripTagsMatchTheHUDSpelling` already uses
  for the two-letter tags, and for the same reason: the seam between the two surfaces is the
  normalized session model and `internal/theme`'s numbers, and reaching across it for a
  rendering detail is the coupling that seam exists to prevent.
- **The map does NOT go into `internal/theme`**, and the stdlib rule is not why. These are
  plain strings and would compile there. The reason is theme's own contract — one hue, one
  meaning, across every surface that imports it — and `internal/statusline` has no vendor
  blocks. Two packages holding the same map is the honest cost of keeping that contract
  intact, and the parity test is what makes the cost bounded.
- **4-bit indices, severity and chrome off limits.** `1`/`2`/`3` and their bright twins
  `9`/`10`/`11` are the ramp this very surface draws its percentages in — a vendor heading
  wearing red would read as an account in trouble — and `0`/`7`/`8`/`15` are the gauge track
  and the terminal's own fore/background. `TestNoVendorHueIsASeverityOrChrome` fences it.
- **`PlainStyles`-identity by construction, and zero golden churn.** One `retint` helper
  returns the base style untouched when `Plain` is set, so no golden on this surface can see
  the feature exist; **any golden diff on this change is a bug**, and none was produced.
  `NO_COLOR` needs nothing new — `colorprofile` downsamples inside Bubble Tea exactly as
  §7.5 describes.

The **honest weakness is council's too**: `4`/`12` and `6`/`14` are two pairs of one hue at
two intensities, and some schemes render each pair close. Council carries the distinction on
the two-letter tags; here the thing being tinted is the vendor's full name, spelled out at
the head of its own block, so this surface has more carrying it than the room does.

Declined inside the ratification, and the list is closed:

- **The hue on the grid rows and the header.** Position and the two-letter tag already
  answer "which vendor" on both, so it would be the circus row §9.28 refused for council's
  column headers, spent on a question the layout had already settled.
- **A hue on the vendor tag as well as the name**, wherever the two appear together — double
  the ink for a distinction the name already carries. Council's own declined list.
- **Gemini given a hue of its own.** The legal set has one index left (`13`), and spending
  it to complete a set is a palette entry with no argument behind it. Gemini takes the
  identity-hue fallback and looks exactly as it always did.

#### Known limitations

- **An aged-out relay reading is indistinguishable from one that never arrived**, by the
  trade in the table above. Both render as "no quota relayed yet".
- **The header and this view still say some of the same things twice — just never at the
  same moment.** The 2026-08-09 amendment above removed the on-screen overlap by collapsing
  the header while this body is open, so no reading appears in two places in one frame any
  more. What survives is the *maintenance* half of the limitation: two renderers still speak
  for the same measurement, and a future change to one has to be made in both.
  `quotaVendors` being shared by both surfaces is what keeps them from disagreeing about
  *which* source speaks for a vendor; nothing yet keeps them from diverging in tone.
- **The void fix is decided per frame, so a resize can change the gaps.** `usageAir` widens
  the blocks apart only when the taller layout still fits, which means dragging a terminal
  across the threshold moves every gap by a row at once. It is a resize, not a tick — §7.1
  rule 4 budgets *frame-to-frame* churn on a still screen and this cannot fire on one — but
  it is a visible jump at the boundary rather than a smooth one, and smoothing it would mean
  the variable-height gaps the amendment declines.
- **The absence sentences are per-vendor literals**, so a seventh vendor arrives with the
  fallback wording (`no quota telltale can read`) until someone measures its seam and gives
  it a sentence. That is the honest default — it claims nothing about a seam nobody has
  looked at — but it is a step down from the five that name theirs. **`grok` was that case
  live** for one day: it landed as the fleet's sixth vendor (#183), flowed into the block
  layout and the shared column grid with no change to either, and took the fallback
  sentence because nobody had measured what its store says about an account. §3.9a's sweep
  supplied one on 2026-08-09 and it now names its own seam; the fallback is back to being
  a path nothing currently takes.
- **The spend line's count is of sessions, not of generations.** A conversation whose read
  dropped some generations for failing the `thinking + answer == output` self-check still
  counts as one contributing session, and its partial sum is in the total. The drop is
  named in that row's Diagnostics and nowhere on this surface, so a reader looking only at
  the fleet line cannot tell a whole conversation from a partly-refused one. The
  alternative — excluding the whole conversation — would discard generations that passed
  their own check and undercount by an amount nothing on screen could name, which is the
  worse of the two silences.
- **The spend line's window shrinks silently when a conversation is deleted.** "on disk"
  is the only thing saying so. There is no honest alternative from a passive read: the
  scan cannot know what was on disk yesterday without keeping its own history, and a cache
  of previous scans would be telltale asserting a past it did not measure at the time.
- **One over-age block costs the whole header line its gauges.** The escalation is decided
  per block, but the dress cascade is decided per LINE — one level has to fit every block
  on it — so the eight columns `⚠ stale` adds to a single vendor can push the line down a
  level that every vendor pays for. That is exactly what the `usage-stale-relay` frame
  above shows: at 120 columns with three vendors the header was already flush against its
  budget, so escalating Claude's reading dropped the bars from agy's and Codex's fresh
  blocks too. The trade is deliberate and it is the cascade's existing grammar rather than
  a new rule — the percentage beside each bar carries the reading, and a quietly stale
  number is a worse failure than a missing bar — but it does mean the frame that
  demonstrates the fix is also the frame that pays the most for it. A per-block dress
  would fix it and is declined: blocks of different heights and vocabularies on one line
  is a grid a reader has to reconstruct, and §7.2's whole argument is that they should
  not have to.
- **The models census counts sessions, not turns.** A model that ran once six hours ago and
  a model that has been running all morning are the same entry in the row. There is no
  per-model activity anywhere the HUD reads, so weighting the list would be an invention —
  but a reader who takes the order for importance will be wrong, which is part of why it is
  alphabetical rather than ranked.

### 7.18 The scan keeps up: what a poll actually costs, measured (2026-08-09)

The footer's `⚠ last scan Ns ago` (`view.go` `staleAfter = 3s`) was on permanently on the
owner's machine. That notice was **correct** — the scan really was taking longer than three
seconds against a 1 s `pollInterval` — which is the worst version of the problem: a true
signal that fires constantly stops being read, and the one field the HUD has for saying
"do not trust what you are looking at" becomes wallpaper. Raising `staleAfter` was
considered and rejected outright; it hides the measurement instead of fixing what it
measures.

**Measure first.** Profiled against the live corpus on 2026-08-09 (Windows 11, i7-7700K,
1,404 sessions across six vendors — 967 claude, 346 codex, 65 agy, 53 grok, 7 cursor,
1 gemini). Nothing from that corpus is in this repository; only the timings are.

| vendor | refs | discover | read (all refs, gate 8) | per-read p50 / p95 |
|---|---|---|---|---|
| claude | 967 | 29 ms | **2.612 s** | 12.8 ms / 63.7 ms |
| codex | 346 | 27 ms | 138 ms | 1.0 ms / 13.2 ms |
| agy | 65 | 1 ms | 42 ms | 2.5 ms / 14.4 ms |
| grok | 53 | 5 ms | 11 ms | 1.0 ms / 3.3 ms |
| gemini | 1 | 0 ms | 3 ms | 3.2 ms |
| cursor | 7 | 0 ms | 2 ms | 1.0 ms |

Whole `Scan`, warm: **1.84 s / 1.91 s / 3.37 s** over three consecutive runs.

Four of the five things worth suspecting were already fine, and saying so is the point of
recording this:

- **The vendors are already concurrent.** `Scan` fans out one goroutine per adapter and
  `readAll` fans out per ref behind a semaphore of 8. Serialization was not the finding.
- **The reads are already bounded.** Head 64 KB + tail 128 KB held at this corpus size —
  164 MB read against 693 MB on disk. Cursor's SQLite store was already snapshot-cached on
  a two-stat check, and grok's `updates.jsonl` never showed up in the profile at all.
- **The scan is already off the UI goroutine** (`scanCmd`), so a slow scan lagged the
  display; it never froze input.
- **The 8-hour idle filter is genuinely expensive** — 1,235 of 1,404 sessions were read and
  then hidden — but see the rejected optimization below.

The finding was **JSON parsing, and specifically re-parsing work that had not changed**.
Broken down over claude's 967 files: open 73 ms, stat 14 ms, head read 185 ms, tail read
364 ms, subagent stat pass 321 ms, and **`json.Unmarshal` 2.65 s** — 46,727 records, every
second, of which on a typical tick approximately none had been written since the last one.
A pure `os.Stat` pass over the same 967 files costs 50 ms.

**The fix** is a per-transcript parse cache in `internal/adapter/claudecode`, keyed on the
file's `(size, mtime)` — the same shape `internal/adapter/cursor` already uses for its
store snapshot. The honesty constraints decided its boundaries, not convenience:

- **`last_activity` is not cached.** The §6 Q8 ruling makes it `max(mtime, newest record
  timestamp)` and *both* inputs move, so only the record-timestamp half — a pure function
  of the bytes the parse read — is stored. The mtime half is re-stat'ed and the max
  re-folded on every read. This is the field the display's whole staleness story hangs
  off; freezing it would have been the exact self-defeating fix.
- **The sub-agent count is not cached.** It is a function of `now` as much as of the disk
  (§7.13's recency horizon): a fan-out expires with no file changing. It runs on every
  read, cache hit or not. It was always a stat pass and stays one.
- **Diagnostics and degradation replay verbatim.** A hit reports the same torn records and
  the same drift verdict a fresh read would. `drift.Watch.Fold` only reads its watch, which
  is what makes replaying a stored one sound.
- **Absence stays absence.** The stat happens *before* the cache lookup, so a deleted
  transcript is `ErrSessionGone` and never a replay; and a field the parse did not source
  is stored as empty and rebuilt as nil.
- Entries are pruned in `Discover` against the live set, so a HUD left running for a day
  does not accumulate one per session that ever existed.

The residual risk is every mtime cache's: a rewrite landing on byte-identical size *and*
identical mtime is invisible. NTFS timestamps are 100 ns, the vendor appends rather than
rewrites, and the cursor adapter already accepts this trade.

**Rejected: skipping the read for sessions the idle filter will hide.** It is the biggest
apparent win on the table — 1,235 of 1,404 rows — and it is a lie. Q8 exists precisely
because NTFS defers mtime while a writer holds the file (observed lags of ~100 s hot,
~20 min closing), so a stat-only recency prefilter would hide the hot sessions the HUD
exists to watch. The scan also cannot know the filter: `a` toggles `ShowAll` and the rows
must already be there. The cache buys the same speed with none of that.

**Result**, `BenchmarkScan` in `internal/hud` over a synthesized 1,400-session corpus
(generated in the test, never committed; five runs each, same machine):

| | before | after |
|---|---|---|
| warm scan (steady state) | 798 ms median (533–931) | **82 ms median (64–174)** |
| cold scan (first, after launch) | 896 ms median (708–1156) | 994 ms median (655–1200) — unchanged within noise |

On the live corpus the warm whole-`Scan` went from 1.84–3.37 s to **181–204 ms**. The cold
scan is untouched by design: the first frame must genuinely read everything, and that is
what the spinner is for.

The benchmark's corpus is Claude-shaped only, which is a deliberate narrowing recorded here
so nobody reads it as a whole-fleet figure: claude was 967 of 1,404 sessions and 2.6 s of
the 2.9 s of read time, and it is the only adapter carrying this cache. The other five
together were under a fifth of the budget. It runs at full scale in CI (~30 MB of temp
files) and drops to 50 sessions under `-short`.

Codex's 138 ms is the next-largest item and is now co-dominant with everything else put
together. It is left alone: the scan is an order of magnitude inside its budget, and the
same cache would need its own correctness argument against its own read path.

### 7.19 `w`: the week page — the slow windows, one line per vendor (2026-08-09)

The owner's question, verbatim: "one view of the weekly usage for these models — it would
help with scoping work." §7.17 already holds every reading that view needs and spends a
block per vendor to say it, with the census, the spend lines and the five-hour windows in
between. A scoping glance wants none of those; it wants the slow pools, one line each. So
`w` opens a LENS over §7.17's data — the same `quotaVendors`/`usageBlocks` assembly, so the
two surfaces cannot disagree about an account — rendered as a tight table.

**Which windows, and why the rule is honest.** The page shows every window the vendor
itself names weekly, plus the vendor's longest. Neither leg infers a duration, which is the
constraint that shaped this section (§4a.1; quotaAgeWarn's ruling that a length parsed out
of "5h" or "seven_day" is a guess wearing a fact's clothes):

- the `-weekly` suffix is vendor vocabulary read verbatim — agy names its buckets
  ("3p-weekly", "gemini-weekly" observed, §3.8) and quotacache carries the names as ids
  unchanged. Reading the vendor's own suffix is reading, not translating.
- the LAST window rides `model.QuotaWindow`'s ordering contract — "display order, shortest
  first" — so it is the vendor's longest pool by structure rather than by arithmetic.
  Claude's slice ends on `seven_day`, Codex's on `secondary`. No id is parsed for a length,
  and each row renders the vendor's own label beside the reading, so the page never states
  a duration the vendor did not.

One edge is deliberate: a vendor whose only surviving window is short — Claude relayed
after quotacache dropped an expired `seven_day` — shows that window under its own label.
It is the longest reading telltale holds, and the label says how long it is.

**What is kept off.** SPEND does not appear, and not because it is unimportant: a spend
total's accumulation window is "sessions on disk, this scan" (§7.16) — not a week, not any
calendar span — and rendering it under a page titled "this week" would claim a window the
number does not have. The u page renders spend correctly, one key away. A vendor with no
reading keeps §7.17's absence sentence rather than an em dash, because a dash would say
"no reading now" about vendors that are structurally unreadable (§4a.1's three kinds of
nothing). Sorting by remaining headroom was considered and rejected: readings, absences
and two-window vendors do not order on one axis, and a page that reshuffles when a
percentage moves spends §7.1 rule 4's churn budget to encode nothing the percentages do
not already say.

**The relayed age rides every row.** This page has no vendor headings to carry §7.15's
age, so it rides each row as a suffix, and past quotaAgeWarn it escalates in the header's
own grammar — the word (`stale`), the glyph, then the hue as the second signal. The REASON
sentence does not travel here; §7.17 carries the argument, this page carries the alarm:

```
 claude 7d             ██▉─────────────────    15%  ↻ 4d07h  · ⚠ stale 19h ago
```

**The frame, generated by the build** (`internal/hud/testdata/golden/week.txt`; the
week-stale variant is the golden quoted above). Both agy weekly pools under one vendor
name, the second on a continuation row; the four five-hour buckets in the fixture render
nowhere; 3p-weekly's measured 0% draws a full empty track while the three absence vendors
draw sentences — zero and absent, still different states on the page built for a glance:

```
 telltale  │  7 sessions  │  codex 1  gemini 1  agy 3  cursor 1  grok 1
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 this week  each vendor's longest window, and every window it names weekly  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

 claude 7d             █▏──────────────────     6%  ↻ 5d00h  · 2h ago
 codex  7d             ███████████████─────    79%  ↻ 22h48m
 gemini   no quota reaches disk anywhere telltale can read
 agy    3p-weekly      ────────────────────     0%  ↻ 6d23h
        gemini-weekly  ███████▎────────────    38%  ↻ 6d23h
 cursor   no quota anywhere · its store holds experiment values, not usage
 grok     no quota anywhere · no window, no ordinal, no reset time on its disk
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────





 esc close   ↑/↓ scroll
```

**Known limitations, named:**

- **"This week" is the page's name, not each row's claim.** Codex's `secondary` window is
  selected for being the longest, not for being seven days; if the vendor ever reports a
  different length there, the label beside the reading says so and the page needs no code
  change. The title states the question the page answers, the labels state the facts.
- **A new agy bucket that is weekly but not named `-weekly` would miss the suffix leg.**
  It would still appear if it is the slice's last window; otherwise it waits for the
  registry to learn the vendor's new word, which is the same posture every verbatim-
  vocabulary surface here takes (§7.15's convert rule).

### 7.20 `--hide`: the standing hide list (2026-08-10)

The owner's request, near-verbatim: hide gemini and cursor, because only the CLI vendors
are in use these days. The `v` filter cannot answer it — a filter narrows one launch to
one vendor, and this is the opposite ask: every launch, every vendor EXCEPT two. So the
HUD takes a hide list: `--hide gemini,cursor`, with the env var `TELLTALE_HUD_HIDE` as the
flag's default. The env var is the standing per-machine preference (TELLTALE_ASCII's
precedent) and the flag always wins, including `--hide ""` to see everything for one
launch without unsetting the variable.

**Where the hide is applied.** To the snapshot, as each scan lands — not in Render. The
grid, the vendor lines, the fleet quota strip, the `u` page and the `w` page all read the
same snapshot, so stripping it once is what keeps five surfaces from ever disagreeing
about who is hidden. The header census comes from the same slices, so its counts match the
rows for free.

**How this survives the honesty rule.** A monitor that silently hides rows is a liar
(§7.1), and a hidden vendor has no backstop at all: it leaves the "N of M" census
entirely, and no keypress re-reads the choice. So the footer states `hidden gemini cursor`
for the whole run, and the notice outranks the filter and the query in the drop order —
only the two ⚠ facts sit above it. The `v` cycle skips hidden vendors, because a filter
that can only ever select an empty grid is a dead stop on a one-key cycle; a `--vendor`
naming a hidden vendor at startup is refused loudly rather than opened onto a
contradiction.

**What this is not.** Not an uninstall: the adapters stay registered, and the vocabulary
(`agy`/`antigravity`, `cursor`/`composer`) is parseFilter's own, so the two flags cannot
disagree about what a vendor is called. The list is deduplicated and sorted at parse time
so the footer's wording is stable no matter how it was typed. `--hide all` is refused: a
HUD told to hide every vendor is a request to not run it.

### 7.21 The event sink — every hook, one durable stream (2026-08-11)

**What it is.** `telltale events` is a loopback-only HTTP server plus a durable log. An
emitter POSTs one hook event to `/events`; the sink appends it to a JSONL day file under
`~/.telltale/events/`, and rebroadcasts it to every WebSocket client on `/stream`. Two read
endpoints serve a future viewer: `GET /events/recent?limit=N` (newest first) and
`GET /events/filter-options` (the DISTINCT of the three tag axes: `source_app`,
`session_id`, `hook_event_type`). The design is a re-implementation from a published
observability shape (an emitter, one events table, a live stream); no code was taken from
the reference, which carries no license.

**Why it exists.** The fleet runs four vendors and each one's hooks fire into that vendor's
own log, in that vendor's own shape. The sink is the one place a hook event from any vendor
can land in one shape: any process that can pipe JSON is a source. `tools/emit-event.py` is
the reference emitter (stdlib-only Python, no dependency to install): it reads the hook payload
on stdin, promotes the fields a reader filters on (`tool_name`, `tool_use_id`, `error`,
`agent_id`, `agent_type`, `stop_hook_active`), stamps epoch-millisecond time, and POSTs.
Its hard rules are the hook contract: a 5 second timeout, no retry, and exit 0 on every
path — a sink that is down costs the agent at most 5 seconds and one stderr line, never a
failed turn. There is no summarization pass anywhere: the payload travels and is stored
verbatim.

**Distribution is one edit per repo.** The wiring pattern is a hook command of the shape
`python3 <path>/tools/emit-event.py --source-app <repo-name>` — `--source-app` is the only
per-repo change. Claude Code payloads carry `hook_event_name` and `session_id`, so no other
flag is needed there; a wrapper for a vendor whose payload lacks the name passes
`--event-type`.

**Why the store is JSONL, not SQLite.** The reference keeps an `events` table with indexes
on the three tag axes and the timestamp. This repo takes no dependency for a storage path
(decisions/001), and `internal/sqlite` is a byte-level READER with no write path — so the
contract the indexes serve is met with stdlib parts: one JSONL file per UTC day, the
retention window held in memory, distinct-value sets kept beside it. The two queries the
endpoints need — last N by arrival, DISTINCT of three columns — are exactly what that shape
answers. The WebSocket is hand-rolled for the same reason (`internal/eventsink/ws.go`):
the sink speaks one direction of one frame type, and that is a page of checked stdlib code,
not a dependency.

**Retention, which the reference does not have.** `--retain <days>` (default 30). The sweep
runs at startup and then hourly: memory drops events past the window, and a day file is
deleted only when its whole day is past it. The file-name pattern is affirmative
(`YYYY-MM-DD.jsonl`), so the sweep can never delete a file it did not write.

**The boundary this moves, said out loud.** This is the first content-bearing store under
`~/.telltale/` — rows carry the hook payload verbatim, not numbers-and-keys. Three facts
keep it inside the read/write contract: it is its own foreground mode the operator starts
(the `otel` precedent — the gauges gain no write), it binds loopback only and refuses any
other host at startup, and nothing in the gauges reads or renders these files. CLAUDE.md's
boundary section names the exception.

**Dark by design, and the v1 gate.** v1 is gate-held (§1) and this subsystem touches no
gate surface: no council code, no HUD or statusline render, no new seat. The sink runs dark
— events accrue and stream, and nothing in telltale displays them yet. A viewer is a later
call site, not a re-plumb; §7.16's held-display precedent is the model.

**Verified live, 2026-08-11, Windows 11.** A fake PreToolUse payload piped into
`tools/emit-event.py` against a running `telltale events`: the POST returned the stored
row, the row landed in `~/.telltale/events/<day>.jsonl`, `GET /events/recent` returned it,
`GET /events/filter-options` listed its three axes, and a WebSocket client connected to
`/stream` received `{type:"initial"}` on connect and `{type:"event"}` on the insert. The
sink-down path was exercised the same day: with no server listening, the emitter printed
one stderr line and exited 0.

**Retention verified 2026-08-11, same box.** The startup sweep deleted a synthetic
`2026-06-30.jsonl` staged beside the live day file and logged `retention sweep deleted 1
day files`, and the same run left `2026-08-11.jsonl` byte-identical by SHA-256 — so the
sweep drops a day past the 30-day window and keeps a day inside it.

**Live drive, 2026-08-11/12.** The runs above used a piped fake payload. A real
vendor-invoked firing is now recorded. On the Windows reference box (Claude Code
2.1.226→2.1.228), an interactive Claude Code session (v2.1.228) in the repo directory
ran one Bash tool call. The project-local PostToolUse hook posted to the sink, and the
sink stored row id 4 with `source_app` `"telltale"`, the real session id, and the
verbatim PostToolUse payload (`tool_input`, `tool_response`, `transcript_path`, `cwd`).
The sink had run since about 11:58 local; the row landed 2026-08-12T02:38Z (timestamp
1786502282870). The drive found two traps, and both are measurements, not readings of a
document.

**Trap 1 — headless print mode does not run PostToolUse hooks.** In `claude -p` print
mode, PostToolUse hooks NEVER ran: not from the project's `.claude/settings.local.json`
with workspace trust accepted, not passed through `--settings`, with and without a
`matcher` key. The measurement is a breadcrumb test. Two hooks wrote a file by two
different mechanisms (`cmd /c echo` to a file, and `python -c` writing a file), and zero
files appeared. So the hooks were not invoked at all — this is not an invoked-and-failed
hook. **Do not generalize this to the council gate's stream-json surface.** §9.8's probe
measured PreToolUse firing over `--input-format stream-json` the same night. The two
runtimes differ; measure each one.

**Trap 2 — `uv run` in a hook command exits before the script runs.** A hook command of
the form `uv run <path>/tools/emit-event.py` exits 2 with the error ``No environment file
found at: `.env` `` on any machine where `UV_ENV_FILE` is set globally. A PostToolUse hook failure is
silent in normal use, so the hook looks wired and stores nothing. The working shape is a
direct interpreter invocation. `tools/emit-event.py` is stdlib-only and needs no `uv`.
The `telltale events` usage text now recommends the direct form.

### 7.22 `telltale snapshot` — the read mode whose reader is a program (2026-08-11)

**What it is.** `telltale snapshot` runs one scan and prints the fleet's current gauge
state as a single JSON document on stdout, then exits 0. It is the same scan the HUD
runs — `hud.Scan` plus the account relay — reshaped by `internal/snapshot` instead of
rendered into a frame. Three flags: `--vendor` (one vendor only, the HUD's vocabulary),
`--compact` (one line instead of indented), `--timeout` (default 10s).

**Why it exists.** The fleet's own agents are now a reader of this data, and neither
existing read surface serves them. The statusline answers one vendor in one line of styled
text. The HUD is a full-screen TUI that runs until you quit it, and its output is a frame
of box-drawing characters an agent would have to scrape. Both are built for eyes. An agent
that wants to know whether anything is close to its context window, what the fleet has
spent, or which vendor stopped reading, had no answer that was not a screen-scrape — so it
either did not ask, or it read `~/.telltale/` directly and coupled itself to a cache
format that is nobody's API.

**A separate mode, for `doctor`'s reason.** What it prints goes somewhere else. The HUD
owns the alternate screen; this writes to a pipe and returns. Nothing on this path enters
the TUI, renders a gauge, or touches council — which is what keeps it clear of the v1
gate (§1). It is additive: no existing surface changes.

**The schema, and the four rules that shape it.** The document is
`{schema_version, generated_at, scan_error, fleet, vendors[]}`.

- **Zero and absent stay different** (§4a.1). A measured zero is the number `0`. An
  absent value is `null`. No sentinel numbers, and **no `omitempty` anywhere**: an
  optional key is always present, carrying `null`. That last part is the sharper half —
  a key that vanishes when its value is absent makes "no reading" and "this schema moved
  under me" the same observation for the consumer, which is the zero-vs-absent collapse
  one level up. `internal/snapshot/testdata/golden/zero-vs-absent.json` pins it beside
  the HUD's golden of the same name, and `TestZeroIsANumberAndAbsentIsNull` asserts the
  two states differ in JSON *type*, not merely in bytes.
- **A derived value says so.** Each vendor block carries `estimated`, the sorted list of
  `model.Field` names whose value here an adapter computed rather than read. It is the
  JSON form of the render layer's `~`.
- **"Can't know" is not "absent now".** Each vendor block also carries `unsupported`, the
  fields that vendor exposes nothing for, ever. A `null` on a field named there is a
  capability statement; a `null` anywhere else is this moment's reading. The HUD spends a
  whole column-drop rule on this distinction; JSON gets it for two lists. Both lists cover
  the SESSION-sourced fields only, and the first live run is what settled that: an
  adapter's quota capability describes what a session exposes, while the `quota` array
  comes from the account relay, so listing quota in both put `agy` in the document with
  two relayed windows and the word `quota` under `unsupported` — two true statements that
  read as one contradiction.
- **Definitive empty states.** A list with nothing in it is `[]`, never `null`. A reader
  must never handle two spellings of "nothing".

**Pre-computed aggregates, because the alternative is every consumer doing the same
arithmetic.** `fleet` carries the session count, the liveness census (`live`, `idle`,
`stale`, and `unknown` as its own count rather than folded into `stale`, which is an
age claim those rows cannot support), the vendor census by status, the highest context
percentage anywhere, and the total cost. `context_pct_max` is a max and not a mean: the
fleet question is "is anything close to its window", which an average over idle sessions
hides.

**Two things are deliberately absent.** There are **no per-session rows**. Partly that is
the rollup being the product — an agent wants one answer per vendor, not a list to fold —
and partly it is the read/write boundary: a session's honest identity is its name and its
workspace path, and this surface renders numbers and keys, never content.
`TestNoSessionContentReachesTheDocument` plants markers in every content-bearing field of
a session and requires that none survives, including the session id.
And there are **no token counts**. The relay is wired and the HUD reads it, but the
DISPLAY is held by the owner (§7.16's amendment, applied to grok in §7.16a). A JSON field
is a rendering; adding one here would end that hold as a side effect of a different
feature. `TestSpendIsNotRendered` pins the omission so the day the hold lifts is a
decision, not a drift.

**Quota comes from the account relay and never from a row** (§7.15, §7.1's sixth rule).
Hanging a window off a session would assert a per-session limit no vendor publishes.
`quota_read_at` travels with it, because a quota figure without the age of its reading is
a number the consumer cannot judge.

**It writes nothing.** The gauges' contract holds on this path with one item spare: it
reads vendor stores and the quota relay, calls no network, reads no credential, and does
not even write the quota relay — it renders no quota of its own to relay.

**Verified live, 2026-08-11, Windows 11.** `telltale snapshot` against the reference box's
real stores returned a document carrying all six adapters, 1423 sessions with a liveness
census that summed to that count, `agy`'s two relayed quota windows with their reading
time, and `estimated: ["subagents"]` on claude against `estimated: []` on cursor. The
zero-vs-absent pair appeared in that real document without being staged: `agy`'s
`3p-weekly` window carried `"used_pct": 0` — a measured zero — beside five vendors whose
`quota_read_at` was `null`.
`--compact` returned the same document on one line and `--vendor codex` returned that
vendor alone. `--json`, a positional argument, `--vendor chatgpt` and `--timeout 0` each
printed a corrective error, no document, and exit 1.

That run is also what found the quota-capability contradiction described above; the
contradiction was fixed and the run repeated.

**The stated reader has now consumed it, 2026-08-12.** At 2026-08-12T01:59Z an agent
session ran `telltale snapshot --compact` (binary built at main `01770ec`) and answered
real fleet questions from the parsed JSON: 6 vendors watching, 1,443 sessions, 1 live,
`context_pct_max` 75.8 on codex, and `agy` quota with 4 windows of which `gemini-weekly`
carried 11.9 `used_pct`. Zero-vs-absent held in the document the agent read: `cost` was
`null` everywhere, and a `used_pct` of 0 was the number 0.

## 8. Roadmap (decided 2026-08-01; adoption track added 2026-08-02, ADR-005)

Rigor stays the floor; features and front-end craft are the priority axis from here.
Each item names its incumbent inspiration and the honest-gauge twist that makes it ours.
Sources rule unchanged: a segment ships only when this doc names its source.

Read the version numbers below against §1: v1 cuts on the snapshot gates, so an item
marked for v1 is not an item waiting on the gauges. They are done.

ADR-005 adds a second axis: external adoption is an explicit product goal, and
adoptability is a design input rather than a lagging indicator. That does not reorder the
feature track below — it adds the adoption track that runs beside it, and one of its
items lands *before* v1 is done, which is why this section is no longer titled
"after v1".

### Adoption track (ADR-005)

ADR-001's sequence stands unchanged — dogfood → eval + design doc → launch post — so
these items are ordered by that sequence, not by version number.

1. **Activation slice — runs in parallel with the dogfood window**, i.e. now, not after
   v1. Four pieces, all packaging rather than capability: prebuilt binaries via
   goreleaser; one-command install with **scoop/winget first**, per Windows-first
   (ADR-002); a README hero visual; and a useful **zero-config first frame** — the
   binary's first run has to show something true without being configured, because an
   install that lands on an empty screen has spent its only attempt. macOS/Linux binaries
   are cross-compiled; macOS ships labeled **"smoke-verified on macOS — Windows is the
   continuously verified target"** and Linux keeps **"built, not verified"**: ADR-002's
   "no macOS/Linux work until v1" is amended for *distribution only*, the
   no-porting/no-verification-effort rule stands, and both labels are ADR-001's
   flagged-limitation pattern applied to a platform instead of a segment. The macOS
   label is point-in-time and SHA-bearing — the suite, build, statusline smokes, a
   53-session live Claude Code read and the HUD all ran on macOS at `052a9d6`
   (ADR-005, second amendment) — while CI still runs `windows-latest` only, and five
   of the six adapters have still never met a live macOS corpus.
   The README positioning line — *"one local HUD for every coding agent you use"* —
   lands **with** this slice and deliberately not before it: a positioning claim that
   arrives ahead of a one-command install is a promise the reader has no way to act on.
2. **The launch post tests one hypothesis: cross-harness visibility** — do multi-harness
   power users want one honest local HUD across the agents they already run? That, and
   only that, is what the launched product contains. The signal that answers it is
   evidence someone actually *ran* telltale — a version-bearing bug report, a real-session
   screenshot, a PR grounded in running it, package-manager feedback, or an unsolicited
   statement of use. Engagement without that (a comment, a question, a hot take) answers
   a different question, and is read as such.

   **Amended 2026-08-15: the hypothesis is room-led, because the product is.** The wording
   above was written 2026-08-02 and names the HUD, four days before the
   council-is-the-product ruling (§1); the post that goes out leads with the room, so the
   signal above is read as evidence about cross-harness visibility of the ROOM — one brief
   answered by five vendor CLIs side by side, with the gauges as the infrastructure under
   it. The signal test itself is unchanged, and so is item 3's exclusion.
3. **Needs-input / blocked / done state is the first post-validation feature** — the
   attention-routing job, and the reason the product is positioned the way it is. It is
   built where the vendor seams already support it: Claude Code hooks, Codex notify
   events, agy's `agent_state` (observed live transitioning `tool_use` → `idle`, §3.8),
   and Cursor Hooks (documented and versioned — §3.9, and the reason the Cursor adapter's
   `status` field is deferred rather than mapped). It is judged on its own terms rather
   than on the launch's: the launch explicitly does not claim this ground, so its result
   is evidence about cross-harness visibility only, and neither validates nor falsifies a
   capability the launched product did not contain.
4. **The agy disk-seam re-survey RAN the same day this track landed — verdict: OPEN**
   (§3.8 re-survey block; prompted by ccusage issue #1402). Both claimed surfaces
   verified against the local 1.1.9 corpus: the advertised transcript.jsonl is real
   (ADR-004's watch item resolved — the first survey's "never written" was wrong at the
   same version), and `gen_metadata` token counts decode with a self-checking
   arithmetic identity. **The next adapter work item is therefore the agy HUD adapter
   itself** — transcript-first (Name/Workspace/LastActivity/liveness scaffolding from
   plain JSONL), `gen_metadata` only for Model + token counts, honoring §3.8's build
   cautions (WAL sidecars, stale summary index, PII fields, assert-the-identity). agy
   stops being a statusline-only vendor when that ships, and telltale ships the lane's
   only Antigravity HUD adapter. Liveness and subagents stay structural-only until
   observed live. — **BUILT the same day** (decisions/006,
   `internal/adapter/antigravity` + `internal/sqlite`, §3.8 "Adapter built"): four
   reported fields, zero new dependencies, the token identity asserted at read time and
   holding 16/16 on the live corpus. agy is now a HUD vendor; the two deferrals stand.
5. **Cursor (Composer) is the fifth vendor and the SIXTH HUD lane** — surveyed and
   **BUILT the same day** (§3.9, decisions/007, `internal/adapter/cursor`). It is the
   first vendor to persist its own context percentage, and the first whose store holds
   live credentials, so the adapter's most load-bearing property is its read allowlist.
   Two watch items follow it and neither is blocked on effort: **Cursor Hooks**
   (cursor.com/docs/hooks — a documented, versioned payload carrying `conversation_id`,
   `model`, `workspace_roots`, `transcript_path`, and context numbers on `preCompact`) is
   where item 3's needs-input signal should come from for this vendor, rather than
   reverse-engineering `status` out of the store; and the `cursor-agent` CLI keeps a
   separate store that is not installed on the survey machine and stays unverified and
   out of scope until it is.

#### Packaging decisions (settled 2026-08-08; §6.5 closed here)

Adoption item 1's first piece — prebuilt binaries and a one-command install — is
**built and not yet fired**. `.goreleaser.yaml` and `.github/workflows/release.yml`
exist; no tag does. §6.5 deferred distribution naming "to packaging time", and this is
it, so the rulings are here rather than in the open-questions list.

**Tag day is one command.** `git tag vX.Y.Z && git push origin vX.Y.Z`. A `v*` tag is
the release workflow's only trigger; merging to main releases nothing. The workflow
then runs the repo's own gate, builds, and stages a **draft** release. The runbook
lives in [packaging/README.md](../packaging/README.md) — this section is the argument,
that file is the procedure, and neither restates the other.

1. **Targets, and each one's label.** Four: `windows/amd64`, `darwin/amd64`,
   `darwin/arm64`, `linux/amd64`, CGO off. The labels above are binding on the release
   notes and are printed per download in the release body: Windows **continuously
   verified**, `darwin/amd64` **smoke-verified on Intel macOS** (point-in-time,
   `052a9d6` — the Mac that ran it is Intel, and the label says which arch was under
   the smoke rather than "macOS" flat), `linux/amd64` **built, not verified**.
   `darwin/arm64` is the one addition to the ADR-005 list and it takes the Linux label
   verbatim: **built, not verified**. Shipping it was weighed against withholding it —
   most Macs are Apple Silicon, so refusing to build it replaces a labelled binary with
   no binary at all, which serves nobody and teaches nobody anything. The label is the
   whole claim. `windows/arm64` and `linux/arm64` are not built: no verification story
   and no known user, and an unlabelled binary nobody has run is the packaging form of
   a rendered guess.
2. **Names: `telltale` everywhere.** §6.5 named `telltale-hud` as the fallback if the
   bare name collided. It does not — checked at packaging time against the scoop
   `Main` and `Extras` buckets and against `microsoft/winget-pkgs`, both clean — so the
   fallback stays unused and the scoop app is `telltale`. winget needs a
   publisher-qualified id and gets `sanlee-ys.telltale`, the GitHub-handle convention
   `junegunn.fzf` and `ajeetdsouza.zoxide` already use. npm remains skipped, not
   renamed: the bare name there IS taken (an unrelated option parser), and §6.5 already
   ruled npm optional for a Go binary.
3. **The scoop bucket is in this repo, `bucket/`.** Rejected: a second repo,
   `sanlee-ys/scoop-telltale`. The deciding cost is a credential rather than
   convenience — goreleaser pushing a manifest into a *different* repo needs a
   cross-repo PAT held as a release secret, while pushing into its own needs only the
   workflow's built-in `GITHUB_TOKEN`, which the release already holds to upload
   artifacts. A project whose stated posture is that it reads no credentials should not
   mint a long-lived write token for one JSON file. `scoop bucket add` accepts any git
   repo carrying a `bucket/` directory, so the user's command is the same length under
   either choice, and the second repo buys nothing but a second thing to keep alive.
4. **The release is a DRAFT and stays one.** goreleaser's job ends at "artifacts and
   notes are staged"; publishing is outward-facing and is a human action. The honest
   consequence, recorded rather than glossed: the scoop manifest is committed in the
   same run, so between that commit and the publish click, `scoop install telltale`
   points at a URL that 404s. It fails cleanly and installs nothing — but the window is
   real, and the answer is to publish promptly rather than to pretend it isn't there.
   `skip_upload: auto` keeps snapshots and prereleases out of the bucket entirely.
5. **The release runs the repo's own gate, called and not copied.** `release.yml`'s
   first job is `uses: ./.github/workflows/ci.yml` — vet, the suite, the build and the
   three binary-level smokes, on `windows-latest`. A release that skips the gate is a
   false green; a release running a hand-maintained second transcription of it is a
   subtler one, and that is the only change `ci.yml` took (a `workflow_call` trigger).
   goreleaser itself runs on `ubuntu-latest`: with CGO off the cross-compile is
   host-independent, so the build host is a cost question, and what makes the Windows
   artifact trustworthy is the Windows gate that already passed.
6. **Changelog: plain `git log`, ascending, ungrouped** — not goreleaser's default
   Conventional-Commits grouping, and explicitly not `use: github-native`. This repo's
   commit voice is a lowercase sentence describing the behavior change (CLAUDE.md), not
   a `feat(x):` label, so CC grouping would file every commit under "Others" beneath
   three empty headings; squash-merge already makes those subjects the PR titles, which
   is the changelog anyone would write by hand. `github-native` is rejected for a
   harder reason: it replaces the entire release body, which would delete the
   platform-label table — the part of the release that has to be true.
7. **Not packaged, and why.** No Homebrew tap: macOS is smoke-verified on Intel only,
   and a tap resolves just as happily on Apple Silicon, which would launder "built, not
   verified" into "supported". No `.deb`/`.rpm`: a distro package is a support claim
   Linux has not earned here, and the tarball carries the label a package would drop.
   No npm, per (2). winget is packaged but **not automated** — submission is a pull
   request against a Microsoft-owned repository, and a bot opening PRs on someone
   else's repo every tag is a different thing from cutting a release. The manifest
   draft (schema 1.12.0, `zip` + nested `portable`, `windows_amd64` only) and the
   submission flow are in [packaging/winget/](../packaging/winget/).

What this does **not** discharge: the README hero visual and the zero-config first
frame are the other two pieces of adoption item 1 and are untouched here, and the
positioning line still lands with the slice, not ahead of it.

Neither track discharges what verification already owes: §3.4's remaining passive-tail
items stay open (§3.7's first live Gemini pass ran and passed 2026-08-03), and
adoption work does not buy an exemption from them.

### v1.1 — the flagship trio (**BUILT**)

1. **Detail pane** (inspiration: abtop / CASS drill-ins) — **BUILT, §7.11.** Select a row,
   get an expanded view: quota windows, extras (branch, CLI version, ctx tokens), session
   id, and — crucially — the session's Diagnostics and degraded-field marks, which v1
   carried with no surface. The honesty machinery becomes visible product. Shipped with
   one thing the spec did not ask for and the design demanded: a `not sourced` line, which
   makes §4a.1's "can't know" versus "absent now" legible for the first time.
2. **Burn-rate forecast** (inspiration: Claude-Code-Usage-Monitor / codeburn) — **BUILT,
   §7.12.** The HUD samples the account window's `used_percentage` over its own runtime;
   the slope is a telltale-measured value, rendered with the `~` derived marker AND its
   sampling window (`~13:27 · 18m basis`). Never extrapolated from a guessed budget, and
   below the minimum basis it renders nothing at all. Shipped with two refusals beyond the
   spec's "minimum sample count/age": no projection past the window's own reset, and none
   beyond 24 h, because the render is a wall clock with no date on it.
3. **Sub-agent chips** (inspiration: claude-hud's active-subagent display) — **BUILT,
   §7.13.** Counts recently-written transcripts in a session's `subagents/` sidecar tree
   (already discovered and excluded from rows in §3.1): a `⑂~2` chip on rows running
   fan-outs. Corrected against the spec: the roadmap called it "pure sourced data" and it
   is not — the files are counted exactly, but the recency boundary is an inference, so
   the field is `CapDerived` and the chip carries the estimate marker.

Also in v1.1: **`/` type-to-filter** on name/path substring (CASS's kernel without the
embedding search) — **BUILT, §7.14.**

New schema field in v1.1: `subagents` (§4a.2), declared `CapDerived` by the Claude adapter
and `CapNone` by Codex. `model.Validate` gained a non-negative check for it; nothing else
in the schema moved.

### v1.2 — the Windows-native leap

- **`telltale notify`**: a third mode on the same binary, fed by Claude Code hook events
  on stdin, raising a Windows toast when a session needs input or ends a long turn.
  (agenttray had the idea; nobody has executed it well on Windows.) Read-only posture
  holds: notify consumes hook payloads, sends nothing anywhere but the OS notification
  API.
- **Statusline context-breakdown bar** (two-line statusline): stacked mini-bar of
  `current_usage` components (input / cache-read / cache-creation / output) — fully
  sourced from stdin.

### Later / unscheduled

- Themes + segment config file (ccstatusline's adoption driver).
- `telltale snap`: one-shot frame render to stdout (pipeable, screenshot-able; already
  prototyped as a throwaway during v1 verification).
- ~~Gemini CLI adapter, once its seam is verified live (§4a.7 example becomes real)~~ —
  **landed 2026-08-02** (`internal/adapter/gemini`, §3.7; the §4a.7 example is real,
  with its original sketch kept as the postscript's evidence).

### Deliberately rejected

- Cross-device pairing/sync (codeburn): network egress breaks "telltale never writes".
- Plan-budget "% of plan" spend meters: the budget is a guess — the exact fabrication
  this product exists to refuse.
- On-disk cost estimation via price tables: inventing dollars from token counts.

## 9. Council (ADR-008)

`telltale council` is the dispatch room: one brief typed once, routed to Claude's control
plane by default or to the explicitly `@mentioned` seats, replies streaming side by side.
`@all` convenes every seated vendor when independent answers are the point. It exists
because the alternative is four terminals and a clipboard.

[council.md](council.md) is the room's user-facing guide — the badge vocabulary, the
routing grammar, the reading keys, every flag. This section is the record underneath it:
what was measured on each vendor, what each finding cost, and what is still unverified.

It is the one subcommand that is not a gauge, and the boundary is worth stating precisely
rather than hand-waving. §7.8's invariant — no keybinding may mutate vendor state or send
anything to a running agent — is **unchanged and unweakened**; it is a rule about the
observation surfaces, and council is not one of them. Nothing in the HUD reaches council.
The only way in is typing the subcommand. What moved is the *scope* of the sentence in
`README.md`, from "telltale never writes" to "the gauges never write", because the old
phrasing had become false the moment ADR-008 was accepted and an accepted decision the
docs contradict is worse than either option on its own.

### 9.1 What v1 seats, and what it does not

Five columns: **Claude Code**, **Codex**, **Antigravity**, **Cursor**, **Grok** (the
fifth seat arrived 2026-08-09, §9.39 — this line said "Four columns" for six days after
it landed, which is its own small lesson about headline counts in a doc whose sections
are the record).

Cursor was originally written up here as deliberately absent, on the grounds that
`cursor-agent` was not installed. **That was not a judgement call, it was untrue** — the
binary was at `%LOCALAPPDATA%\cursor-agent\cursor-agent.cmd` and had been for a month, and a
test pinned the false claim in place so the day the world changed underneath it, it went on
passing. It is seated now (ADR-008, fifth amendment). What remains true from the original
paragraph: the `cursor` binary on PATH is the editor launcher, and council never drives it.
The seat is driven on Windows too — the `.cmd` shim turned out to be a nine-line wrapper
around a bundled `node.exe`, which detection resolves directly, so no prompt ever meets a
shell and §9.3's refusal has nothing left to fire on (ADR-008, tenth amendment). Its posture
badge stays `ro:requested`, now contradicted-by-capture rather than merely unobserved: under
`--mode plan` the agent was seen dispatching shell tool calls, and `--sandbox enabled` kills
the turn outright on Windows, so it is not passed there.

This is the inverse of the HUD, where Cursor *is* a built-in adapter (ADR-007) because its seam
is on disk rather than behind a CLI.

#### The vendors this room does not seat, and the class of evidence behind each (2026-08-15)

This list existed only in the owner's private notes, which meant it bound nothing and was one
lost file away from being re-derived vendor by vendor. It is here so a future session can tell
**"we looked and said no"** from **"nobody has looked"** — two states this project has already
ruled must never render alike (§4a.1), applied to a decision rather than to a value.

Every entry names the CLASS of its evidence, because they are not the same strength. An
unmeasured rejection stated as a measured one is the honest-gauge defect committed in prose,
and this doc's own §9.2 is the record of what that costs: the first draft of ADR-008 claimed
enforcement for four seats when one had a mechanism.

- **Warp** — **REJECTED, measured.** There is no blocking hook anywhere in the product: no
  surface at which a tool call is held while something else decides. So the fleet's guard
  requirement cannot be satisfied here, and that requirement is not council's own — council
  gates the seats it spawns (§9.8), while agent-ops ADR-012 requires each vendor to be
  guard-wired mechanically, per vendor. A vendor with nothing to wire cannot be brought inside
  it, and routing work away from an unwired vendor is explicitly not how that gap closes.
- **Amazon Q** — **REJECTED on vendor status**, which is a weaker class than a measurement and
  is named as one: the product is dead / end-of-life. There is nothing left here to measure.
- **Amp** — **REJECTED on platform.** WSL-only on Windows, and its threads live server-side.
  Windows is this product's primary target (ADR-002), so a WSL-only binary is not a seat here;
  and a vendor whose conversation state sits on someone else's server gives §9.4's native
  resume nothing on this machine to resume from.
- **Goose** — **REJECTED on platform.** No Windows path at all. The same ADR-002 reason as
  Amp, without the second problem.
- **Every BYO-harness re-host** — **REJECTED as a class**, rather than one vendor at a time.
  Each of them re-hosts a model family that already has a seat here. §9.4's whole argument for
  turn 1 being blind is that the answers are *independent*; a harness wrapped around a model
  already in the room buys a column that agrees with an existing one for the reason a copy
  agrees with its original. That is a surface, not an opinion, and the room is priced per seat
  (§9.21).
- **Kimi Code 0.34.0** — **INSTALLED, WAITLISTED, UNVERIFIED. This is not a rejection**, and
  it must not be read as one. The binary is on the reference box; its hook surface and its
  session surface have never been observed, so there is no measurement to seat it on and none
  to refuse it on either. One trap is recorded because it is the obvious wrong shortcut: the
  legacy `kimi-cli` documentation is **VOID** for this binary — it describes a different
  program — and any claim read out of it would be exactly the "read off `--help`" evidence
  §9.2 refuses.
- **Qwen Code** — **RUNNER-UP: measured, and it fails on one specific thing.** It has a real
  statusline hook, which is more than several seated vendors offer. What its payload does not
  carry is any rate-limit window, so the quota relay (§7.15) would have nothing to write and
  the header could speak for this vendor only by inventing the numbers it renders. Under
  §4a.1 that is `CapNone` rather than a plausible fill, which leaves a seat whose gauge is
  permanently blank. This is the one entry worth re-checking when a vendor payload changes;
  the rejections above do not move until the vendor does.

### 9.2 Two claims the room refuses to leave implicit

Every column header carries its own **sandbox badge** and its own **streaming
granularity**, because vendors across the 4-vendor fleet differ on both and the first draft of ADR-008 got
this wrong in the direction that matters — it claimed "enforced read-only sandboxing" for
all seated vendors when only one had a mechanism named.

| | mechanism | badge |
|---|---|---|
| Claude Code | `--disallowedTools <write/exec list>` + `--strict-mcp-config` | `ro:tools` |
| Codex (macOS/Linux) | `-s read-only`, enforced by the OS sandbox | `ro:enforced` |
| Codex (Windows) | **no sandbox**: `-s danger-full-access`, the only mode that can spawn a process there | `unsandboxed` |
| Antigravity | **no posture flag at all**: `--mode plan --sandbox` were measured not to restrict writes, and were dropped once their only observed effect turned out to be a dead turn (§9.6b) | `unsandboxed` |

There is no level that renders as an unqualified "read-only", and after the live spike there is
one that renders as the opposite. Antigravity was asked to write a file under both of its
read-only flags and wrote it — file confirmed on disk, reported permission mode and tool list
byte-identical to a run without the flags. That is refuted, not unverified, so it gets a fourth
level badged `unsandboxed`. Deliberately not `ro:none`: every other badge opens with `ro:`, a
reader scanning column headers takes in the prefix before the qualifier, and a vendor that
can edit your working tree must not read as read-only at a glance.

Council **stopped passing those two flags** on 2026-08-04 (ADR-008, seventeenth amendment).
The badge is unchanged and so is every word behind it — it never rested on the flags being
sent, it rested on the write having landed — and what moved is one clause of the detail, which
used to end *"the flags are still passed; they do not restrict it"* and could not go on saying
so. This is the second seat where a posture flag came off because it was doing nothing useful
and one measured harm; the Codex row above is the first, and the two were decided on the same
ledger. §9.6b carries the argument.

**Codex is the seat where the OS changes the answer, and on Windows it wears that same
`unsandboxed` badge.** Re-probed 2026-08-04 against codex-cli 0.146.0: `-s read-only` *and*
`-s workspace-write` both fail every process spawn there with `CreateProcessAsUserW failed: 5
(Access is denied.)`, including a control asked merely to list a directory. So "read-only" on
Windows was never a read/write distinction — it was a seat that could not read, which is
exactly how this surfaced: a live council turn answered a "thoughts on this repo" brief with
*"I could not inspect the repository."* Council now passes `-s danger-full-access` on Windows
in **both** postures, because it is the only mode that runs, and the badge tells the truth
about that rather than keeping a comfortable word. The containment is the workspace, not the
flag (ADR-008, third and twelfth amendments). macOS and Linux are untouched and still
`ro:enforced`.

`TestSandboxBadgesAreNeverBlanket` fails the build if a bare claim reappears, and asserts the
badges stay distinct — convergence on one string is how a per-vendor claim quietly becomes
a blanket one again.

What these badges *say* has not changed since; how loudly they say it has. §9.11 gives the
two that mean "this seat can change your files" weight and the warning hue, because the room
had been drawing `unsandboxed` at exactly the volume it drew `ro:tools` beside it. The words
still carry the whole distinction — that is why they break the `ro:` prefix — so nothing
here depends on colour; the weight only makes the word findable in a frame with four columns
of prose in it.

**The Claude row cost three attempts to get right, and the failure mode is worth recording.**
The original ADR claimed enforcement with no mechanism named. The first correction named
`--allowedTools "Read,Glob,Grep"`, which *sounds* exactly right and is not: it pre-approves
tools for permission prompts, it does not remove them from the session. Running the real
invocation and reading the `system/init` event's own `tools` array showed `Edit`, `Write` and
`Bash` still there. Every test in the package passed at that point, because every test asserted
the **flag** and none asserted the **effect** — which is this repo's own False Green failure,
committed inside the feature whose entire premise is refusing it.

What works is `--disallowedTools` plus `--strict-mcp-config`, and two parts of that are easy to
miss. Deny **PowerShell**, not just Bash — denying only Bash leaves a working shell on the
platform this product targets. And drop MCP servers, because without `--strict-mcp-config` the
session inherits whatever the user has connected; the verification run surfaced Gmail write
tools in a session with every built-in write tool denied, and no fixed deny list can name those
in advance.

The residual limitation is stated in the badge's own detail text rather than hidden: a deny list
cannot cover a tool that does not exist yet. The claim is *these named tools are absent,
verified*, not *this session cannot write*. The general rule this leaves behind: **a flag's name
is not evidence of its effect**, and the check that matters is what the session reports about
itself afterwards.

Granularity is the same discipline applied to streaming, and the spike made the answer worse
than the guess. Claude streams token-level deltas, verified live. The other two were
provisionally labelled `events`, on the reasoning that a coarse stream is still a stream;
neither streams at all. Codex emits one `item.completed` per complete agent message and has no
message-delta feature even under development. Antigravity delivers an entire response as a
single `text_delta` — a one-word reply left its column blank for 73 seconds and then painted at
once. Both are `GranFinalOnly`. A vendor that emits nothing until it finishes renders `PhaseWaiting`
— a first-class phase, named as such in its column header — rather than an empty column that
looks like slow streaming. `TestWaitingIsNotStreaming` asserts the two never render alike. That
distinction was added on the theory that some vendor might not stream; it turns out to describe
two thirds of the room. This is §4a.1's rule (a dropped column and an em dash must not read the
same) applied to a surface where the ambiguity would otherwise be invisible.

The card's WORDING is not what carries it, and §9.14 is why that matters: the body used to
recite *"this vendor reports no incremental output, so nothing appears until the turn
finishes"* on every waiting turn, which is council's plumbing described in council's vocabulary
in the space a user came to read an answer. The distinction is carried by the header's own
phase word and the granularity badge beside it; the body says only `working — the reply
arrives whole.`

### 9.3 Execution: argv, never a shell

Prompts are arbitrary text — quotes, ampersands, whatever was typed — so no prompt is ever
interpolated into a command string. Specs are `{Binary, Args []string, StdinPrompt, Dir}`
through `exec.CommandContext`.

This is load-bearing on Windows specifically. `LookPath("codex")` resolves to `codex.cmd`,
an npm shim, and Go's `os/exec` runs `.cmd` and `.bat` through `cmd.exe`, whose argument
parsing cannot be safely quoted for arbitrary text. So `detect.go` classifies every
resolved path as `KindNative` or `KindShim`, Codex and Claude take their prompt on **stdin**
(Codex via its verified `-` sentinel), and a vendor that is *both* a shim and argv-only is
marked `AvailUnusable` and not driven at all. The refusal is the feature; the card tells the
user which env override fixes it.

### 9.4 Multi-turn is native resume, not transcript re-send

Turn 1 is blind: no vendor sees another's answer, which is what makes opinions across the 4-vendor fleet
independent rather than anchored. Later turns ride each vendor's own session-resume
(`claude --resume`, `codex exec resume`, `agy --conversation`). Re-sending the transcript
would grow input quadratically against metered quotas and flatten native turn structure into
quoted prose; resume sends only the new turn and makes the blind-round guarantee
*structural*, since each session holds only its own history. Cross-agent rebuttal is an
explicit opt-in toggle that quotes the previous turn's finals as labelled untrusted material.

### 9.5 Layout and testing

Same contract as §7.9, for the same reason: `Render` is pure over `State`, tests construct
state by hand, goldens live in `internal/council/testdata/golden/*.txt` and render with
`PlainStyles()`. Three columns at ≥96 cells; below that, or when a column would fall under
24 cells, the tier drops to a tab bar rather than shredding prose into unreadable ribbons.
Width is measured with `lipgloss.Width`, never `len()`.

Two of the frame's rows are no longer constants, and the ordering that makes that safe is
worth stating: the **tier is settled before any row is budgeted**. The tab bar costs a row,
and the fallback from columns to tabs is a width test — budgeting first and dropping the
tier afterwards worked only while the footer was a fixed three rows, and a taller composer
would have overflowed the terminal by exactly the tab bar. `resolveLayoutIn` therefore
finishes deciding the tier, then spends rows: header, footer chrome, tab bar if any,
collapsed-seat notice if any, then the composer up to its ceiling, and **the composer
yields before the body does** — at the minimum height a six-row draft would leave the
columns nothing, and a room you can type in but not read is not the trade anyone asked
for. A tab bar holding a single tab is not drawn at all: it selects nothing and repeats
the column header underneath it, which stopped being a rarity the moment dead seats began
folding away (§9.9).

One trap worth recording, because the golden tests could not have caught it: `padRight`
truncates rune by rune, so on text that already carries ANSI escapes it cuts through an
escape sequence and counts escape bytes as content. Goldens render with the identity style
set by design, so they are blind to it. Anywhere a line is assembled from differently-styled
pieces — the tab bar, the help body — padding goes through `fit`, which is ANSI-aware.
`TestFitIsANSIAware` is the regression guard.

### 9.6 Invocation traps, one per vendor

Each adapter hit a failure that is silent rather than loud, which is the kind worth writing down.

- **Claude**: `--allowedTools` pre-approves, it does not restrict (§9.2). Enforcement is
  `--disallowedTools` + `--strict-mcp-config`.
- **Codex**: `codex exec` and `codex exec resume` **do not take the same flags**. `-s` and `--cd`
  are rejected by `resume` with an argument-parsing error, and a parse error means *empty
  stdout* — a naive resume would blank the column on every follow-up turn with no card able to
  explain it. Resume carries the posture as `-c sandbox_mode="<mode>"`, derived from the same
  function as the spawn path so the two cannot drift, and takes its workspace from `Spec.Dir`
  alone. The session id is **positional**, not a flag value.
- **Antigravity**: `-p` is a **string flag whose value is the prompt**, not a boolean. Written in
  the natural order, `agy -p --output-format stream-json "<brief>"` exits 0 and cheerfully
  answers a question about the flag it just swallowed. `-p` must be last, brief immediately
  after, every other flag before it. agy also rejects a prompt on stdin, so its brief goes in
  argv and is bounded by the ~32K Windows command-line limit — a real ceiling on a long brief,
  with no workaround short of upstream support.

- **Cursor**: the prompt is a variadic positional and needs a bare `--` in front of it, or a brief
  that happens to open with `-` is read as an unknown option and the turn dies. And the stream
  sends every passage twice — deltas, then the whole message again — which §9.6c covers, because
  it is a parsing trap rather than an invocation one and it took two captures to state correctly.

The shared shape: all four failures produce a *plausible* result rather than an error. That is
why each one is pinned by a test asserting the argv this repo actually builds.

### 9.6a The activity trace carries outcomes — and says when it cannot

The trace answers *what did this agent do*. Until this landed it could not answer *did it
work*, which made it the same half-built gauge §4a.1 exists to forbid: `⚙ Bash: go test ./...`
renders identically whether the suite passed or the build never compiled. The results were not
missing, either — they were arriving in the same stream the commands came from and being
dropped on the floor. A room that discards knowledge it has is the mirror image of one that
invents knowledge it lacks, and both are the same failure.

Four statuses, and **Unknown is the one that earns the type**. Pending renders as the bare
entry, OK as `✓`, Failed as `✗` plus the vendor's own first line about why, Unknown as `?`.
ASCII gets `+`, `x`, `?` — chosen around everything already spoken for, since `*` is the
activity prefix, `>` the ellipsis, `]` focus and `#` the HUD's gauge fill. Every distinction is
a glyph before it is a colour, so all four survive `--ascii` and a monochrome terminal.

**Where the outcome comes from, per vendor, and how strong the claim is.**

| | signal | verified |
|---|---|---|
| Claude Code | `user` messages carrying `tool_result` blocks with `tool_use_id` + `is_error` | **live**, 2026-08-04, Claude Code 2.1.220 |
| Codex | `item.started` → pending; `item.completed` with `exit_code` / `status` | captured fixtures; `exit_code` and `status:"failed"` observed, `status:"completed"` **never** |
| Antigravity | `step_update` ACTIVE → pending, DONE → **Unknown** | live capture shows DONE carries no success signal at all |

Three things the Claude probe settled that a docs-first parser would have got wrong. Field
**order** differs between captured lines, so nothing may be read from position. `is_error` is
**absent** on some successes — a `Read` result carried only `tool_use_id`, `type` and `content`
— so absence is success rather than unknown; Claude Code marks failure and stays quiet about
the rest. And the results came back **out of order**, the second call's failure landing ahead
of the first call's success, on the very first probe. That last one is why correlation is by
id and never by arrival order: a trace zipped by position would have blamed the wrong command
on its first real run.

Antigravity is the case the Unknown status exists for. Its steps flip ACTIVE then DONE, and
every captured DONE line carries `duration_seconds`, sometimes a `tool_info` with the call's
parameters, and nothing whatsoever about whether the step achieved anything. agy reports
success or failure exactly once per turn, in the final `result` event, and that verdict is
about the *turn*. So a finished agy step renders `?` — not `✓`, and the code comment says why.
Reusing the success mark would be council inventing a result on a vendor's behalf, which is the
`--allowedTools` mistake (§9.2) wearing different clothes.

Codex carries the same discipline in a smaller way. `exit_code` is a **pointer**, because codex
spells "still running" as `"exit_code":null` and a plain int would flatten that to 0 — the
spelling of success, and the most expensive confusion available on that field. And an item that
completes with neither an exit code nor `status:"failed"` resolves **Unknown**, not OK: no
captured line has ever carried `status:"completed"`, and guessing the success spelling from the
observed failure one would be a success claim built on a string nobody has seen. That
deliberately weak mapping tightens the moment a live run shows the spelling.

`TestActOutcomesRenderDistinctly` fails the build if any two statuses ever render alike;
`TestOverlappingToolCallsResolveToTheRightEntries` replays the real out-of-order probe.

**A failed entry is a card now, and it was the one card §9.11 missed.** That pass gave every
card in a column one grammar — a title with its body hanging under it — and cited the trace as
somewhere the room *already* did it, on the strength of the failure detail's indent. The entry
itself never got it, and a live room showed why that matters: `run_command: pwsh -Command
"Get-ChildItem"` does not fit 37 cells, so the command wrapped to a continuation starting hard
against the column edge, reading as a second nameless entry with the outcome mark stranded on
it. It now hangs under its own `⚙`, which costs no rows and makes one call look like one call.

Two things then had to change with it, and both are the kind of detail that only shows up
against a real capture:

- **The reason indents FOUR, not two.** Once the command hangs at two, a reason at two lands in
  the same column as the tail of the command it explains — telling them apart by colour alone,
  which this product does not do. Goldens render with `PlainStyles`, so that golden is exactly
  the artifact that proves it.
- **The reason is flattened and bounded.** `sanitize` preserves newlines on purpose, because a
  vendor's prose reply is prose; a tool failure's detail is not prose, and multi-line stderr
  pushed through the wrapper arrived as ragged fragments at random widths. It is now collapsed
  to one flowing line and capped at three rows with the room's own ellipsis, so a clipped reason
  can never read as a complete one. The clip has an answer — `f` expands the column to the full
  frame, where the same reason typically survives whole — and a refusal behind it: the trace
  answers *what did this agent do and did it work*, not *show me the log*, and the turn-level
  failure still arrives in the column's note carrying the vendor's own sentence.

### 9.6b The agy trace was showing its message-passing and hiding its work

Driven live, the Antigravity column's trace read `user_input ?`, `system_message ?`,
`checkpoint ?`, `unknown ?` — and, for every real thing the agent did, a bare `tool ?`. Three
separate defects wearing one symptom, all found by reading captured stdout (agy 1.1.10,
Windows, 2026-08-04) rather than the adapter.

**1. The plumbing is suppressed, and the line that decides what counts as plumbing is not
"noisy".** Hiding a vendor's ACTIONS would be a false gauge — a quiet column for an agent busy
editing the workspace, which is §4a.1's failure with the sign flipped. Hiding its PLUMBING is
noise reduction. So the suppression is an allowlist defended per kind against a captured line,
never a filter on what looks like chatter, and it lives in the adapter (`ParseEvent` returns
`false`) rather than in the view, because `Render` is pure over `State` and a step that is not
an action must never become one.

| kind | why it is plumbing, from the capture |
|---|---|
| `user_input` | step 0 of every turn, `DONE`, nothing else on the line — the brief council itself just sent, echoed back |
| `system_message` | same empty shape; agy placing its own message into the conversation |
| `checkpoint` | `duration_seconds` and a ~120-token usage block, nothing else — a thread bookmark, never the workspace |
| `error_message` | an empty marker on a failing turn: no message, no error field, no duration |
| `unknown` | one per turn at a fixed preamble slot (step 1, right after `user_input`), 0.0005s and 0.0045s across two turns, no tool name, no parameters |

`error_message` needed the most care, because dropping the only visible sign that a turn went
wrong is the opposite mistake. It is safe for a checked reason: both captured failing turns end
`result` with `status:"ERROR"` and `error:"Agent execution terminated due to error."`, and that
path already produces a `KindError` carrying the vendor's sentence. The turn-level failure IS
reported, with words. A rendered `error_message ?` is strictly *less* than that — an ominous
name with a shrug attached. The result path now prefers `result.error` over the composed status
line precisely so that argument keeps holding.

`unknown` had to be argued rather than listed, since suppressing a step whose type the adapter
merely does not RECOGNISE is the same class of mistake as inventing an outcome for it. The
capture says this is agy's own label and not our ignorance: fixed position, half a millisecond,
and no tool name — while every step in every capture that did something carried one. What
would reverse the decision is written as code, not as a promise: an `unknown` step that names a
tool is **not** suppressed and renders under that name, so if agy ever starts acting through
this label the trace shows it that same turn.

**2. agy's real tool names were on the wire the whole time and were not being read.** A tool
step carries `tool_name` at the top level *and* `tool_info.name` with `tool_info.parameters`
beside it. The adapter rendered `step_update.step_type` — the literal string `"tool"` — so every
call, whatever it was, produced one indistinguishable entry. This is ADR-008's tenth amendment
repeating itself in a second costume: Cursor's `tool_call.tool.case` lookup matched nothing
because the oneof arrives flattened to a key on the wire, and every Cursor trace entry read
`tool call`. Same cause both times — the fields the vendor sends were never compared against
the fields the parser reads — and the same fix, which is to **parse what arrives**. Observed
names: `list_dir`, `run_command`, `write_to_file`, `list_permissions`.

The entry now follows the grammar the other three adapters already use — `Glob: **/*.go`,
`Bash: go test ./...` — so `⚙ tool ?` becomes `⚙ list_dir: C:\Users\…\antigravity-cli\scratch ?`.
The argument rule is deliberately small, because agy's parameter keys are vendor-specific
(`DirectoryPath`, `CommandLine`, `TargetFile`) and an arbitrary object is not a trace line: only
string values are candidates, one such value renders (which is every captured shape), several
resolve to the lowest key name by byte order, none degrades to the bare tool name. Rule three is
not a claim about which key matters — it is a refusal to let Go's randomised map iteration reach
a rendered line or a golden, pinned by `TestAgyToolArgIsDeterministic`.

**3. A failed agy tool call used to render as permanently pending.** There is a fifth state,
`ERROR`, and the switch handled `ACTIVE` and `DONE` only, so the line matched nothing and the
entry its `ACTIVE` twin had opened stayed pending for the rest of the room's life — the trace
claiming a command was running after the vendor had given up on it. It carries its own reason in
`tool_info.error.message`, so it maps to Failed with the vendor's own first line, exactly as
§9.6a specifies. Failed and not Denied: `ActDenied` is council's first-hand record of its own
gate keystroke, and a refusal read off someone's stream is not that. The `DONE → Unknown` rule
above is unchanged and narrows in one direction only — agy does report per-step failure, and it
still reports no per-step success.

**The resume note was a misdiagnosis, and the fix is to the claim rather than to the
mechanism.** A seat whose restored thread failed its first turn used to say *"the saved thread
was refused — this seat's history is gone."* **Measured**, single trial, 2026-08-04: `agy
--conversation <id>` **does** resume in 1.1.10. The same `conversation_id` came back,
`step_index` **continued** (10 → 11) rather than restarting at 0, and `result.num_turns` was 2.
That demonstrably live thread's turn nevertheless ended `status:"ERROR"` /
`"Agent execution terminated due to error."`, and a separate attempt died before any thread was
involved at all — a bare `result` with an **empty** `conversation_id` and *"Eligibility check
failed: UNAVAILABLE (code 503): The service is currently unavailable."* So agy turns fail
transiently for reasons that have nothing to do with the conversation, and "the history is
gone" is a claim the evidence does not support.

The behaviour was deliberately untouched at the time: one failed turn still dropped the id, for
the reasons ADR-008's ninth amendment gives at length, and no new signal was invented to tell
the two cases apart because none had been observed. Only the sentence was narrowed, to the
three things known — the first turn on the restored thread failed, the seat has let the saved
thread go, and the next brief starts a new session with the brief re-applied.

**That is now out of date, and the evidence above is what dated it** (ADR-008, sixteenth
amendment). The paragraph declined to change the rule on the ground that a record is not a fix,
which was right — and left a measurement sitting beside a rule it contradicted. The rule's
default is unchanged: a restored id whose first turn fails is dropped, because a seat retrying
a genuinely dead id rebuilds the same doomed invocation on every turn for the life of the room.
What changed is that a failure which is **identifiably transient** is now treated exactly as a
cancellation already is — nothing was learned about the thread, so nothing is forfeited, and
the seat stays on probation so the next unclassified failure still costs it the id.

Two classes qualify, and both are positive evidence that the vendor never reached the
conversation. **Pre-flight**: the failures `failureNote` already classifies off captured stderr
— not signed in, an untrusted workspace, a sandbox the vendor's own config demands and its own
help refuses, a binary that vanished — each documented at its case as exiting before any model
call; and, one step earlier, a dispatch that never started a process at all. **Vendor-reported
outage**: the 503 quoted above, matched on agy's own sentence, with the capture's empty
`conversation_id` as the corroboration that it died before a thread was involved.

Everything else still drops, and the asymmetry is deliberate — a lost conversation costs one
conversation, a wedged seat costs every turn of the room. Claude and Codex get nothing
vendor-specific here: neither has a measured transient signal, only measured *dead-thread*
strings pointing the other way, and their behaviour is unchanged. agy's commonest failure
sentence — *"Agent execution terminated due to error."* — is deliberately **not** classified,
because it was captured on a demonstrably live thread and is also what a dead one would
plausibly produce, and a string on both sides of a distinction is evidence for neither.

The classification is produced where the evidence is (the runner's stderr classifier, the
adapters' result parsers) and travels on the event as a small enum. It is never re-derived by
matching the rendered note: that note is prose written for a narrow column, and keying a
mechanism off it would make every wording change a silent behaviour change. It lives on `Model`
and never on `State` — a decision input the renderer has no business reaching.

**The card that says this changed shape too.** One ⚠ plus a single sentence carrying an outcome
and a mechanism wraps to three lines of uniform weight in a 37-cell column, and three of those
side by side reads as a room on fire over a seat that will simply start a new session. It is
now §9.11's card grammar: a short title — *thread not restored — starting fresh* — with the
mechanics hanging under it, quieter, and **no warning mark**. That is the same fact
`reattachCard` already states calmly at idle when no thread came back, learned a turn later;
the ⚠ has to go on meaning *something went wrong* for the notes where something did. The words
carry the card in every glyph set, so `--ascii` loses nothing.

**A side measurement, separately labelled, with its confound stated.** Under `--mode plan
--sandbox`, agy's `run_command` was refused with *"granting access to C:\: Access is denied."*,
the agent gave up, and the whole turn died `status:"ERROR"` with an empty response. The control
run with both flags **dropped** ran a shell command and returned `status:"SUCCESS"`. ADR-008 and
the `baseArgs` comment previously said `--sandbox`'s effect on the shell "was NOT tested and is
not claimed"; this is the first evidence on it, and that comment no longer says so. **It is one
trial per arm with an uncontrolled difference: the two turns issued different command lines
(`pwsh -Command "Get-Location; Get-ChildItem"` versus `Get-ChildItem`), so it is not a clean
A/B**, and the refusal's mention of `C:\` may be about a drive root rather than about the flag.
What it does establish is the flag's observed cost — a dead turn, with nothing rendered. The
posture flags were **not** changed on the strength of it; that is a decision to make
deliberately and separately, and this was a record, not a fix.

**That decision is now made: the flags come off, in both postures** (ADR-008, seventeenth
amendment). The open question this section carried — *should council keep asking agy for
`--mode plan --sandbox` when both are measured to do nothing?* — is closed the way the evidence
points, and the ledger is one-sided rather than a close call:

- On the **write** side, the flags were measured restricting nothing. Asked to write a file
  under both, agy wrote it; reported permission mode and tool list were byte-identical to a run
  without them, and `write_to_file` was still in the list. Refuted, not unproven.
- On the **shell** side, the one and only effect either flag has ever been observed to have is
  the paragraph above: a refused `run_command`, an agent that gave up, and a turn that ended
  `status:"ERROR"` with an empty response. The user sees a blank column.
- So the flags bought **no restriction that was ever observed**, at the price of turns that die
  with nothing rendered. That is not caution; it is the appearance of caution paid for in the
  vendor's actual answers. The confound above is unresolved and does not need to be: it concerns
  *why* the turn died, and the decision only needs *that* it did, set against a benefit measured
  at zero.

**No honesty claim moves with them, and that separation is the point of having waited.** §9.13
deliberately changed what the room *says* about this posture and nothing about the posture,
because a documentation pass that quietly retunes a safety flag is exactly what this file exists
to prevent. This is the other half, made on its own, by the owner. The badge stays
`unsandboxed`; the detail loses the clause claiming the flags are passed, because they are not,
and a detail describing council's own behaviour inaccurately is the one class of false claim
this repo has no excuse for. The containment was never these flags — it is the workspace (§9.2,
ADR-008 third and twelfth amendments), and agent-ops ADR-012 rules the same way independently.

Deliberately **not** part of this: `--dangerously-skip-permissions`. Dropping a flag that
restricted nothing and adding one that approves everything are different acts, and the second
stays refused on both seats that offer it.

### 9.6c The Cursor stream says everything twice, and the second time does not always look alike

cursor-agent under `--stream-partial-output` sends a model call's text deltas and then that
call's **complete message** as one more assistant event. Appending both renders the passage
twice, which is the whole of this defect in both of its appearances.

The first capture (2026-08-04, a turn asked to reply `PONG`) showed deltas `"P"`, `"ONG"` each
carrying `timestamp_ms` and the repeat `"PONG"` carrying none, so the adapter dropped the event
whose `timestamp_ms` was absent. That rule was derived from turns with no tool call in them, and
**every such turn is one model call** — so it was a rule about the end of a *turn* being used as
a rule about the end of a *message*.

A turn that runs a tool is several model calls, each ending in a repeat of its own segment, and
those mid-turn repeats carry `timestamp_ms` like any delta. The column rendered the segment, the
segment again, then the next one — `X X Y` — which is what the owner saw on a long Cursor reply
and what replaying the captured turn through the old parser reproduces exactly.

What separates them is **`model_call_id`**: present on every whole-message repeat that ends a
mid-turn model call, absent from every one of 108 captured deltas, and carrying the vendor's own
per-segment numbering (`…-0-x7su`, `…-1-15l2`) that also appears on the `tool_call` events
between the segments. The adapter now drops an assistant event when `model_call_id` is present
**or** `timestamp_ms` is absent — the second is kept because the *turn-final* repeat still
carries neither, so dropping it would trade this bug for the first one.

Presence rather than absence is the point, and it generalises past this vendor: a missing field
cannot distinguish "the vendor is telling me this is a complete message" from "the vendor stopped
sending that field". `internal/council/vendors/testdata/cursor-segmented-turn.jsonl` is the whole
turn, redacted, replayed by `TestCursorSegmentedTurnRendersEachPassageOnce`, which asserts the
streamed body equals the reply the vendor itself put in its `result` event. That `result` remains
the safety net if both fields ever go: the room uses it whenever a column streamed nothing, so
the failure mode is a column that fills at the end, never one that is wrong. ADR-008's twentieth
amendment carries the argument.

### 9.7 Status

The room opens, detects the four seats, renders both layouts and every degraded state, takes a
brief, and dispatches it. Claude streams incrementally; Codex and Antigravity render the waiting
card and fill at once. Quitting the room kills every child, including the persistent one —
and no longer strands the conversation: a bare `telltale council` reopens the one saved room
by default, `--fresh` starts over, and `/cd <dir>` typed in the composer moves the room to
another workspace between turns, with the persistent Claude seat following by respawn on its
own session id (ADR-008, ninth and eleventh amendments). Multi-turn is one live process for
Claude (§9.8) and for Cursor (§9.36), and native resume for the two seats that are still batch
programs.

Cross-agent rebuttal (§9.4) and per-column scrollback are **built and shipped**, and the
scrollback now spans the whole conversation rather than one turn (§9.9): the room keeps a
per-column transcript, echoes the brief that produced each turn, composes in up to six rows,
and folds the seats it cannot drive out of the grid. Not built: per-vendor cancel — `ctrl+c`
still ends the whole turn.

Known gaps, stated rather than buried. Codex's non-shell write path is untested — asked to
create a file with its own patch tool it declined, but that was a model choice and says nothing
about enforcement. Neither vendor was observed producing a failure event on stdout, so the error
branches are modelled on exit code plus stderr rather than an observed schema. Antigravity's
`--print-timeout` is left at its 5-minute default, which is a hard ceiling on a long council
turn and a policy choice worth making deliberately later.

Unverified and scheduled as a live spike before the Codex and Antigravity columns ship: the
Codex `--json` event schema and delta granularity, whether `codex -s read-only` engages on
Windows, and Antigravity's stream-json schema, conversation-id location, stdin support and
`--sandbox` semantics. Those columns render honest *requested* badges until it says
otherwise.

**That spike ran, and the Windows sandbox question is closed the other way.** `-s read-only`
does not engage on Windows in any useful sense: it fails every process spawn, reads included,
and so does `-s workspace-write`. Codex on Windows is invoked `danger-full-access` and badged
`unsandboxed` — see the §9.2 table above and ADR-008's twelfth amendment. What remains open on
this seat is narrower: whether the `-c sandbox_mode=` override actually changes behaviour on
the *resume* path. The key is accepted; its effect has never been separately observed, and
until this change every mode failed identically so there was nothing to observe.

One claim in this section is looser than its measurement and is flagged rather than quietly
corrected, because fixing it is a separate change to a separate surface. The Claude column's
granularity word is `tokens`. Measured over a 250-word reply the deltas are **~80 characters
each, about three a second** — genuinely incremental, and not tokens. Measured identically
under the persistent invocation and under a spawn-per-turn control, so it is a pre-existing
overstatement rather than something §9.8 introduced.

### 9.8 One live process, and the gate it makes possible

Every Claude turn used to be a fresh `claude -p --resume`. A one-word "gm" cost about 25
seconds and $0.23, nearly all of it session init, paid again on every turn. That was the
visible cost. The structural one is what forced the change: **a batch process cannot ask
permission.** Its stdin is written and closed before the first token arrives, so it has no
channel to ask on and none for an answer to come back on.

`--input-format stream-json` keeps one process alive taking one JSONL message per turn on an
open stdin. Verified live against Claude Code 2.1.220 rather than read from documentation: two
turns down one stdin came back under the same `session_id` with the same pid, `system/init` is
re-emitted at the *start of every turn* (a parser reading it as "a new session" would reset the
seat once per turn), and one `result` per turn is the only end-of-turn signal there is, because
there is no exit to infer one from.

Cancelling a turn now **interrupts** rather than kills — `{"subtype":"interrupt"}` on the
control channel, measured to end the turn and leave the process answering a further one.
Killing would also work, and would throw away the session init the room just paid for, so
cancelling one turn would quietly make the next one expensive.

**The reported cost changed meaning and the badge changed with it.** `total_cost_usd` is a
running total for the process: $0.1061493 → $0.1177296 across two turns while the per-turn
`usage` block stayed at 2 input tokens both times. That cell has meant "this turn" everywhere
else in the room, so it now reads `$0.1177 session`. Rendering a session total unlabelled would
be a false reading of a true number; subtracting to recover the turn would be council inventing
a figure, which §8 rejects.

#### The gate

With `--write`, the seat that can ask **does** ask. Every tool call raises an approval card in
its column — the tool and its argument line, formatted exactly as the activity trace formats it
— and the room enters a gate state: `y` approves, `n` denies, and the vendor is stopped until
one of them is pressed. Blocking was measured, not assumed: the answer was withheld for twenty
seconds and nothing else arrived on stdout in that window.

Three flags turn it on, none is optional, and **two of them do nothing alone**:

| flag | what it does | what happens without it |
|---|---|---|
| `--permission-prompt-tool stdio` | routes the request onto the stream | **absent from `--help`** and real; alone, the session runs in auto mode, no request is ever emitted and the file is written |
| `--permission-mode manual` | makes the call ask rather than assume | alone, there is nobody to ask, so the call short-circuits to *"you haven't granted it yet"* and the vendor gives up |
| `--setting-sources ""` | stops the user's own permission rules pre-approving the call | measured on a machine allowing `Bash(mkdir:*)`: `mkdir zzz` **ran ungated** and the directory was created |

**The third flag became a FALLBACK on 2026-08-12** and the table row is the record of why it
was ever needed. Council injects its own `PreToolUse` hook instead, which runs at step one and
beats an allow rule at step five, so the operator's settings stay loaded. The dated block at
the end of this section carries the measurements and the build.

The third is the honesty of the whole feature, and it is the one nobody would have thought to
test. Permission *allow rules* in settings files are consulted **before** the callback, so a
call they cover never reaches the gate at all. Without that flag, "nothing writes without your
keystroke" is simply false — and false quietly, on a machine whose owner wrote those rules
years ago for a different purpose.

**Amended 2026-08-11, at the end of this section.** That paragraph is still true and it is
still the record of 2026-08-04. It reads one step of the evaluation, not the step above it: an
`ask` rule is consulted BEFORE an `allow` rule, and it reaches the callback the allow rule
would have skipped. So the third flag is no longer the only way to be honest. The measurement
and the build it implies are below.

**One limit is stated on the badge rather than buried here.** Shell commands the CLI itself
classifies as read-only are approved without asking — `git status` was ungated under both
setting-source configurations, and so is `echo` — so the claim is about calls that *change*
things and is worded that way everywhere.

**RETIRED 2026-08-12, and kept because it is the record of a hole that existed for eight days.**
Everything in the next four paragraphs describes council copying the USER's hooks into the
ephemeral file. The seat no longer drops their settings, so there is nothing to copy and a copy
would run every one of their hooks twice. The ephemeral file survives, built the same way and
for the same reason, carrying council's own gate hook instead.

**The second limit was a hole, and it is now closed.** Dropping the setting sources also
dropped the user's own hooks and user-level commands from that seat. Half of that is the
feature working: the allow rules are what the gate replaces. The other half was collateral — a
`PreToolUse` hook is a screen the user built, nothing was replacing it, and the calls it
covered are disproportionately the ones the gate never sees. Measured: in the gated posture,
`echo <marker>` raised no request and simply ran.

`--settings <file>` composes with `--setting-sources ""` — the sources stay dropped and the
named file is still read — so council copies the user's `hooks` section into an ephemeral file
of its own and points the gated seat at it. Two properties of that file are load-bearing:

- **It is built by naming one key, never by deleting others.** The same spike showed a
  `permissions` block inside a `--settings` file re-admits the allow rules: an allowlisted
  `mkdir` ran with no request and the directory landed on disk. An allowlist of exactly `hooks`
  cannot rot as Claude Code adds settings keys; a denylist would.
- **The badge is derived from whether the file exists**, not from whether council tried. An
  unreadable settings file, an empty hooks section and a temp directory that could not be
  created all end in the same place, and the column says the guard is absent rather than
  claiming one.

The file is absolute (a relative `--settings` path resolves against the *child's* working
directory, which is the workspace, and fails), 0600, removed on teardown, never logged and
never rendered — the same privacy discipline `--brief` carries, for the same reason: only a
boolean crosses onto `State`.

The honest residual: hooks fire as that file described them at spawn time. Editing the real
settings mid-session does not propagate until the next room.

**A denial is not a failure, and the difference took a fifth outcome value to keep.** The vendor
reports a refusal as an `is_error` tool_result carrying council's own refusal text back — so
read off the stream alone it is indistinguishable from a tool that broke, and the trace would
say the command *failed* when what happened is that it was *not allowed to run*. `ActDenied` is
recorded from the keystroke, before the echo arrives, and the echo cannot overwrite it. It
renders `✗ denied by you`: the words carry the distinction, colour only seconds it, and it is
the one line in the trace that is not a reading of a vendor's words.

**The gate is Claude-only, and that is a fact about the other CLIs.** `codex exec` and `agy -p`
are batch programs — read a prompt, answer, exit. Neither has a channel a question could arrive
on. Their columns keep `WRITES`; only the seat that asks carries `gated`. Giving all four the
same badge would be the blanket claim §9.2 exists to refuse, one level up.

**Amended by §9.36, and the amendment is narrower than it looks.** The Cursor seat is now a live
ACP process and it *can* be asked: `session/request_permission` blocks it until answered, measured
on both branches. It still does not carry `gated`, because it does not ask about EDITS — measured
twice, it wrote a file and raised nothing. So the last sentence above holds with one word changed:
only the seat that asks about **everything that changes anything** carries `gated`. Council answers
Cursor's requests all the same, because an unanswered one blocks the vendor forever.

`--write --auto` restores the old behaviour for the times nobody is watching: `acceptEdits`,
the `WRITES` badge, the user's settings left alone — and therefore no injected hooks file
either, since a room that loads those settings natively would otherwise run every hook twice.
Gating is the default because the room the user opened is the one they are looking at;
unattended is the exception and has to be typed.

#### The gate can keep the user's settings, measured 2026-08-11 — and the build is not authorised

**Superseded by the block below it, 2026-08-12, and kept whole.** Nothing changed in the
product on this date; this is the record of the probe and of the ruling it waited for. The
ruling came the next day — measure the two open unknowns, build only if both support it — and
both did.

**The claim.** Claude Code evaluates a tool call in six steps, in this order: hooks, deny
rules, ask rules, permission mode, allow rules, then the `canUseTool` callback. That is the
live documentation of 2026-08-11 (`code.claude.com/docs/en/agent-sdk/permissions`), not
memory. `ask` is step three and `allow` is step five, so an `ask` rule reaches the callback
that an `allow` rule would have skipped. The same docs say a `PreToolUse` hook may return
`permissionDecision` `"allow"`, `"deny"`, `"ask"` or `"defer"`, and that `ask` beats `allow`
when both apply. If either holds in the binary, the gate can keep `--setting-sources` and
gate anyway — and keeping them carries the user's own hooks, deny rules and user-level
commands back in natively, which is the whole of what this seat gives up today.

**The rig, because this repo does not read a claim off a doc.** A probe replicated this
seat's own argv — `baseArgs` plus `Session`, gated posture, `--model haiku` added to keep the
turns cheap — spawned it against a throwaway directory, wrote one turn on stdin as `Turn()`
builds it, and answered any `can_use_tool` request with `behavior: "deny"` as `Decide()`
builds it. `--setting-sources ""` was dropped on every arm except the one that reproduces
what council ships. **The decisive observable is the filesystem, never the stream**: the
brief asked for one command that creates a marker, and the arm is read by whether the marker
is on disk. Claude Code 2.1.226, Windows 11, `claude-haiku-4-5` on every turn, two trials
each.

| arm | what it changed | requests | marker created | trials |
|---|---|---|---|---|
| **A** adopter | zero-rule `CLAUDE_CONFIG_DIR`, no `--setting-sources` | — | — | **blocked** |
| **A2** | user settings live, `touch probe-marker` | 0 | **yes** | 2/2 |
| **A3** | user settings live, `install -d probe-marker` | 1 | no | 2/2 |
| **B1** | user settings live, `mkdir probe-marker` | 0 | **yes** | 2/2 |
| **B2** | B1 plus an injected `ask` rule for `Bash(mkdir:*)` | 1 | no | 2/2 |
| **C** | user settings live, injected `PreToolUse` hook returns `"ask"` | 1 | no | 2/2 |
| **C control** | same hook wiring, hook returns no decision | 0 | **yes** | 2/2 |
| **shipped** | council's argv today, `--setting-sources ""` | 1 | no | 2/2 |

**B1 says the 2026-08-04 finding still reproduces.** An allow rule covers `mkdir`, the call
ran, and the directory is on disk. Nothing here retires the original measurement.

**B2 is the decisive arm.** One rule was added — `{"permissions":{"ask":["Bash(mkdir:*)"]}}`
in a `--settings` file — over settings that already allow the same shape. The call raised a
request, the denial was honoured, and nothing was created. The request named its own cause:
`"decision_reason_type":"rule"`.

**C says a hook can do the same job, and says more on the way through.** A hooks-only
`--settings` file whose `PreToolUse` hook returns `permissionDecision: "ask"` gated the same
allow-covered call, and the request arrived carrying `"decision_reason_type":"hook"` with the
hook's own sentence in `decision_reason`. The hook wrote a breadcrumb on every trial, so its
run is provable off the stream. **The control matters as much as the arm**: C changed two
things at once, a `--settings` file and an "ask" behind it, so the same file was run again
with a hook that returns nothing. The call went ungated and the directory landed. The
decision causes the gate, not the file.

**Two facts fell out that were not the question.** `--settings` composes with the user's
settings rather than replacing them — every sources-live arm ran the user's own `SessionEnd`
hooks, including the arms passing `--settings`, and the shipped arm ran none. And a write
shape no rule covers already reaches the gate with sources live (A3), so today's flag is not
what makes the gate fire; it is what makes it fire *uniformly*.

**What build this implies, and it is the hook rather than the rule.** A2 is why. `touch`
creates a file and it ran ungated on both trials, so the user's rules cover more shapes than
anyone would enumerate, and an `ask` list built shape by shape leaks exactly the way an allow
list leaks. That is the same defect `hookset.go` (now `gatehook.go`) already refuses by naming one key instead of
deleting many. A matcherless `PreToolUse` hook has no list to leak: the documentation's own
advice for a check that must run on every tool call is a hook, for this reason. So the shape
to build is council injecting its own `PreToolUse` hook that answers `"ask"`, into the same
ephemeral `--settings` file it already writes, and **dropping `--setting-sources ""`** — which
returns the user's deny rules, their user-level commands and their hooks to the gated seat,
and retires the hooks copy in `hookset.go` (the file became `gatehook.go`) along with the badge that reports whether it
worked.

**What is NOT settled, and each item is a reason the build waits.**

- **The adopter arm did not run.** A temporary `CLAUDE_CONFIG_DIR` holds no credentials, so
  the turn died at `"apiKeySource":"none"` and `Not logged in · Please run /login` before any
  tool call. Copying a credential store into a probe directory is a redline, so the arm stays
  unrun. The shipped arm is the nearest evidence for the same question: with no rules in
  force at all, the call reached the prompt on both trials.
- **A matcherless hook was never measured.** This rig measured a `Bash` matcher. The claim
  that one hook sees every tool call is documentation, and documentation is what this section
  exists to distrust.
- **Composition with the user's own `PreToolUse` hooks is unmeasured.** The docs rank `deny`
  over `defer` over `ask` over `allow` when several apply. Council would be adding a second
  hook to a file the user also populates, and the credential guard is exactly the hook that
  must not be weakened by the addition.
- **A hook is a process per tool call.** The seat that was re-founded to stop paying process
  cost per turn (§9.33, §9.36) would take on a spawn per call. Nothing here timed it.
- **The badge's sentence would have to change.** Today it claims a guard because a hooks file
  exists. Under this build the guard IS the gate, and "the user's hooks are carried" stops
  being a separate claim — a badge that kept saying it would be reporting a file that no
  longer does that job.

#### The two deciding measurements, and the build, 2026-08-12

The owner ruled: measure the two items above that decide the build, then build only if both
measurements support it. Both did, and the build is in. Claude Code **2.1.228** (the box moved
on from 2.1.226 between the two dates), Windows 11, `claude-haiku-4-5` on every turn, two
trials per arm, throwaway directories, the same rig as the block above — a probe replicating
this seat's own argv, one turn written on stdin as `Turn()` builds it, `can_use_tool` answered
as `Decide()` builds it, and **the decisive observable is the filesystem, never the stream**.
The adopter arm stays unrun for the same reason: copying a credential store into a probe
directory is a redline.

**(a) A matcherless hook fires for every tool shape, and the ask reaches the card.** The arm
kept the operator's settings live — no `--setting-sources ""` — and injected one `PreToolUse`
entry with no `matcher` field, returning `permissionDecision: "ask"`.

| arm | `mkdir gate-a` (an allow rule covers it) | `install -d gate-b` (no rule covers it) | `Write gate-c.txt` (not a shell command) | on disk | trials |
|---|---|---|---|---|---|
| **M** matcherless hook returns `"ask"` | request, `hook` | request, `hook` | request, `hook` | nothing | 2/2 |
| **M control** same file, hook returns no decision | **no request, directory created** | request | request | `gate-a` | 2/2 |
| **shipped binary** the file council now writes, `telltale hook gate` | request, `hook` | request, `hook` | request, `hook` | nothing | 2/2 |

Every request carried `"decision_reason_type":"hook"` and the hook's own sentence in
`decision_reason`, forwarded verbatim to the `can_use_tool` card. **The control is what makes
this a finding**: the same file, the same hook process running — its breadcrumbs prove it — and
only the decision removed. `mkdir` went ungated and the directory landed, which also
re-reproduces the 2026-08-04 bypass on 2.1.228. The decision causes the gate, not the file.

**The matcher forms were measured against each other** in one turn, four entries side by side
writing to four breadcrumb files: **matcherless, `"*"` and `""` each saw both the `Bash` call
and the `Write` call; `"Bash"` saw only the `Bash` call.** That one hook sees every tool call
was documentation until this turn. The absent field is what ships, of the three equivalent
forms, because it is the only one that cannot later be read as a pattern somebody should widen.

**A Windows trap, and it is the worst failure this feature has.** Claude Code hands the hook
command to **`/usr/bin/bash`** — Git Bash, on the platform this product primarily targets. The
first three arms measured nothing because bash ate every backslash of a native Windows path:

```
/usr/bin/bash: line 1: C:UserssanleAppDataLocalTempclaudeC--…askhook.exe: command not found
```

`exit_code: 127`, `outcome: "error"` — and **a hook that fails to run makes no decision, so
every call ran ungated while the badge went on claiming a gate**. It was found only because a
`SessionStart` hook was planted in the same file to prove the file was read at all, which costs
no model turn. Council quotes the command and swaps the separators;
`TestTheHookCommandSurvivesGitBash` pins both.

**The first fix for it was wrong, and the Linux CI job is what said so.** `filepath.ToSlash` is
a **no-op on Linux**, where a backslash is a legal filename character, so it made the
conversion depend on the host Go compiled for. The string is not read by the host — it is read
by bash, on every platform, where a backslash is the escape character and cannot survive as
itself. The swap is now unconditional, and the test feeds a Windows path on every runner.

**(b) The hook costs tens of milliseconds, and the operator's own settings cost more.** The
measure is the interval between the assistant's `tool_use` block landing on stdout and the
`can_use_tool` request landing — the window Claude Code evaluates permissions and runs hooks in.

| arm | per-call gap, all samples (ms) | median | trials |
|---|---|---|---|
| **shipped today** — `--setting-sources ""`, no hooks at all | 23.0, 12.3, 21.2, 10.5, 6.8 | **12.3** | 2 |
| operator's settings live, **no** council hook | 284.6, 280.5, 291.0, 234.6 | **282** | 2 |
| live + council's hook, a 3 MB probe binary | 358.9, 351.7, 249.5, 348.4, 298.4, 258.4 | **323** | 2 |
| live + council's hook, **the shipped 14 MB `telltale.exe`** | 523.6, 491.2, 461.2, 451.8, 439.7, 362.7 | **456** | 2 |

Read the rows against each other rather than against zero, because **most of the delta is not
the hook**. Loading the operator's settings at all costs ~270 ms per call — that is their own
`PreToolUse` hooks running, and it is the thing this build BUYS, not a price it adds. Council's
own hook adds **~41 ms** as a small binary and **~174 ms** as the shipped one. Measured
directly, outside the CLI, 20 spawns through the same Git Bash: the small binary is
**36.2 ms median**, `telltale.exe` is **54.4 ms median** — the 14 MB single binary links the TUI
framework on a path that runs once per tool call, which is ADR-002's statusline argument
arriving at a second door. Against a warm Claude turn of 6.4 s (`STATE.md`'s traced `@all`), three
gated calls add ~0.5 s. That is the owner's "tens of milliseconds is fine" band at the binary's
own cost and inside it at the process's; it is nowhere near the "a second per call" that fails.

**What shipped.** Council writes an ephemeral `--settings` file containing exactly one key,
`hooks`, holding one matcherless `PreToolUse` entry that runs `telltale hook gate` — a new mode
beside `telltale hook cursor`, which drains stdin and prints one decision object and nothing
else. `--setting-sources ""` is **dropped**, so the gated seat loads the operator's deny rules,
their user-level commands and their own hooks again. `--permission-mode manual` is **kept**:
the documentation makes it an alias for `default` on 2.1.200+, which would make it decoration,
but every arm of both probes carried it and nothing has measured the seat without it — the same
rule that kept `--permission-prompt-tool stdio` when it was absent from `--help`.

**The fallback is the old build, not a hole.** A room whose hook file cannot be written — no
temp directory, a binary that cannot locate itself — passes `--setting-sources ""` and gates the
2026-08-04 way. It gives up the operator's settings and the column says so. Weaker in what it
keeps, never weaker at the gate.

**One cost was not on the list of five, and it changed the room.** The hook asks about
EVERYTHING, which is the point — and `Read`, `Glob` and `git status` raised **no request at all**
under the old flag, because Claude Code approves what it classifies read-only before the
callback. Under the hook all three raise one. Shipping only the hook would have tripled the
cards, and this room already knows what that costs: the first session with the gate carded the
user thirty-four times, which is why `autoApproveRoutine` exists. So council answers them
itself — `autoApproveRoutine` for shell commands, and a new positive list of tool names that
change nothing (`Read`, `Glob`, `Grep`, `NotebookRead`) for the calls that are not shell
commands. Positive, so a tool Claude Code adds next month draws a card rather than being waved
through; `TodoWrite` is deliberately absent, because nothing here measured what it writes.

**The badge's sentence changed, as the fifth item predicted.** It no longer claims the
operator's permission rules are dropped, because they are not. The wired branch says their
settings stay loaded and that council's own hook asks first; the fallback branch says the
settings were dropped and why. Both branches still say nothing runs until you answer.

**Of the five unsettled items, two are settled, two are retired by the build, and one remains.**
The matcherless hook and the per-call cost are measured above. The badge sentence and the
`--setting-sources ""` question are decided by what shipped. **Composition with the operator's
own `PreToolUse` hooks is still unmeasured** — council now adds a second hook to a file the
operator also populates, the docs rank `deny` over `defer` over `ask` over `allow` when several
apply, and the credential guard is exactly the hook that must not be weakened by the addition.
The ranking makes a weakening unlikely (a `deny` from their hook beats council's `ask`), and
"unlikely by documentation" is the standard of evidence this section exists to distrust.

**The per-call cost tolerance is HELD — owner ruling, 2026-08-15.** The measurement above
stands: the shipped 14 MB binary costs ~54 ms per gated call because the one binary links
the TUI framework, and a sibling hook binary would save ~133 ms per gated call. The owner
ruled the saving does not buy an ADR-002 amendment: roughly half a second across three
gated calls on a 6.4 s turn sits inside the "tens of milliseconds is fine" band the build
was accepted under, and the split's real price is not the binary — it is the packaging
(the scoop manifest ships one exe), `hookCommand`'s self-location, room/hook version skew,
and a missing-sibling fallback whose only honest shape is `--setting-sources ""`, which
trades away the operator's settings to save milliseconds. The one-binary shape stands
unamended. Reopen this only with a measurement that changes the arithmetic: more gated
calls per turn than the ~3 assumed, or a vendor change that raises the per-call floor.

### 9.9 The room remembers — a conversation, not a ticker

Everything above builds a very good way to **send one turn**. What it did not build is
somewhere to have a conversation, and the gap was structural rather than cosmetic:
dispatching turn N cleared turn N-1's body, trace, clock and cost off the screen, and the
user's own words were never rendered anywhere at all. So the room could show you four
answers to a question it could not show you, and then throw them away when you asked the
next one. This was PR 4 of council's original plan, ratified and then skipped.

**The transcript is per column and per turn.** A finished turn is *pushed* to
`Column.History` rather than erased: `TurnRecord` carries the brief, the reply, the trace,
the note, the elapsed and the cost, and `columnText` renders the whole list oldest-first,
each turn opened by a separator naming it. Three consequences fall out of it being one flat
list of lines:

- **The scrollback needed no second mechanism.** The window, the overflow markers, the tail
  and `MaxScroll` were already the code that moved through a column's lines; a transcript is
  just more of them. `g` reaches the first thing that seat was ever asked.
- **Each past turn carries its own numbers.** The column header and the badge line are
  chrome describing the turn *in flight*, so a turn scrolled back to would otherwise sit
  under someone else's clock. A turn that ended badly names its phase on its separator; a
  turn that ended normally does not, because "done" on every one of them is noise on the
  common case and makes the two that matter harder to find. A running total keeps its
  `session` word (§9.8) — losing it in history would turn a true figure into a false one.
- **A seat that sat out a turn records nothing for it.** Routing means turn 4 can go to
  Claude alone, and Codex's transcript then skips from 3 to 5. Filling that gap would be the
  room inventing a conversation.

**The echo is the principal's words, and that is a boundary rather than a phrasing.** What a
seat literally receives is not what is echoed: a first turn is sent with the `--brief` file
prepended, whose content is deliberately held off `State` (§9.8's privacy discipline), and a
rebuttal turn is sent with the other seats' answers fenced in front of it, which are other
vendors' words. Echoing "exactly what was sent" would have put a private file on screen and
labelled another model's output as the user's. So the brief is echoed, marked with the same
`›` the composer uses — the glyph carries it before the colour does — and what rode along
with it is *reported* on its own muted line. It goes through `sanitize` like everything else
that reaches `State` and is **not** redacted: this is the user's own typing echoed to the
user on the user's own screen, so covering it would hide a secret from the one person who
already has it, do nothing about the copy just sent to four vendors, and make the echo
disagree with what was dispatched — which is the one thing the line exists to show.

**Memory is capped at 50 turns per column**, oldest out first, and nothing is written to
disk. The room file (`~/.telltale/council/room.json` — one global room, the workspace a
mutable field inside it; ADR-008, ninth and eleventh amendments) stays keys-only: it holds
session ids and no content, and scrollback is not state worth persisting.

**The composer grows to six rows.** One elided line was not somewhere anyone could compose a
brief worth sending to four agents. `ctrl+j` inserts a newline; `enter` still dispatches, and
the mode line says both. The newline goes into the draft **raw**, deliberately bypassing
`sanitizeKeepingSpace` — that filter exists so a *pasted* newline cannot tear the footer
apart, and it still does exactly that; what it must not do is flatten the one the user asked
for by name. A deliberate newline survives to every transport this repo drives: Claude and
Codex take the prompt on stdin, Claude's persistent turn is JSON-marshalled so the newline is
escaped in the envelope, and agy takes it as a single argv element on a native binary with no
shell anywhere in the path (§9.3) — Go quotes an argument containing a newline, and the
~32K Windows command-line ceiling is unchanged. A draft taller than the ceiling keeps its
tail, where the cursor is, and spends one row saying how much is above it, in the same words
the column overflow markers use.

**A dead seat stops eating the width.** A column whose availability is `NotInstalled` or
`Unusable` held a quarter of the terminal for the whole session to display one card that
never changes — on the reference machine that is Cursor, permanently. Those seats now fold
out of the grid and the survivors take the width. What must not fold away is the *fact*:
one muted line under the header names each collapsed seat and which failure it is, keeping
§4a.1's distinction between "not installed" and "installed but not drivable" intact at one
line instead of one column. A seat nobody can see is one a user has no reason to go looking
for, which makes silent collapse worse than the column it replaced.

`--vendor` is the explicit control, and it mirrors the HUD's flag while doing more than
filter: `all` keeps every detected seat on screen, and a comma list seats exactly those —
drawn **and** dispatched to, since drawing a seat you cannot see while spending its quota is
the same class of hidden state this product exists to refuse. Naming a seat forces it on
screen even when it is absent, because a user who asked for it is owed the card explaining
why it is not there. It parses the `@mention` vocabulary rather than a second one, so
`--vendor agy` and `@agy` are the same word, and `Seated()` counts only seats that are both
drivable and in the room so the header's `3/4 seated` keeps meaning what it says.

### 9.10 A mode that could not scroll, and the mouse it did not get

The room shipped with a per-column scrollback, page keys, `g`, `G`, overflow markers that
count what is hidden, and a full-width expand — and was reported as having **"no way to
scroll up or down if the output that each agent provides is long."** The report was
correct about the experience and wrong about the cause, which is the interesting part:
none of that machinery was missing, and all of it was unreachable.

`turnColumnFinished` puts the room in **compose** when the last column lands, so the mode
the user is in at the moment four long answers arrive is the mode that reads keys as text.
`composeKey` forwarded the six keys it recognised and dropped everything else into a
branch that appends `msg.Text` — and an arrow key carries no text, so every scroll key did
nothing at all, silently, from the one moment they were wanted until the user guessed at
`esc`. The keys were not absent; they were being swallowed by a rule written for letters.

**The rule is now the test rather than a list.** A key that carries no text cannot *be*
composer text, so it keeps the meaning it has in view mode: `↑`, `↓`, `pgup`, `pgdown`,
`tab` and `shift+tab` are shared between the modes through one function, which is what
lets the mode line promise them without a second implementation to keep in step. The
letter aliases stay view-only, because in compose `j`, `k`, `g`, `G` and space are the
letters j, k, g, G and a space — the same rule that keeps `q` the letter q here.

`tab` had to come with the scroll keys rather than after them. They address the *focused*
column, so a mode that can scroll and cannot change which column it scrolls can only ever
read whichever seat happened to be focused when the turn ended.

`left`, `right`, `home` and `end` are deliberately still dead in compose. They are where
an in-draft cursor goes if the composer ever grows one, and spending them on focus now
would make that a change to muscle memory rather than an addition.

**The overflow marker names its keys, on the focused column only.** `↑ 53 more above` told
a reader that something was hidden and nothing about how to see it. It now carries the
keys that would move it — but only on the column those keys address, because the same
hint on the three seats beside it would be three false claims. The hint is mode-aware:
`f expand` is dropped while composing, where `f` is the letter f. A marker that advertised
a key the current mode does not have is precisely the dishonesty §7.8's always-on mode
line exists to prevent. It is also dropped, `f` first, when the cell cannot hold both —
the count is never traded for the hint, and at a three-seat room's 37 cells the short form
is what fits.

The same honesty now runs the other way as well, in §9.11: `f` and `tab` are dropped from
the mode line *and* the marker in a room with a single seat on screen, because expanding the
only column to the width it already has and cycling focus around one seat are both nothing
happening. A key that does nothing is as much a false promise as a key that goes unnamed.

#### Mouse wheel scrolling: rejected, with the measurement

**Measured**, against the compiled `charm.land/bubbletea/v2` v2.0.8 by running a program
per mode and reading the bytes it wrote:

| `View.MouseMode` | emitted on enter |
|---|---|
| `MouseModeNone` | nothing |
| `MouseModeCellMotion` | `ESC[?1002h` `ESC[?1006h` |
| `MouseModeAllMotion` | `ESC[?1003h` `ESC[?1006h` |

Those three are the whole enum. **There is no wheel-only mode**, and there is no DEC mode
that would provide one: under 1000, 1002 and 1003 alike the wheel is reported *as buttons
4 and 5 inside button reporting*, so a program cannot ask for the wheel without also
claiming the left button. 1002 is button-event tracking — press, release, and motion while
a button is held.

**Inferred** from that, and from Windows Terminal's documented behaviour rather than from a
run: while 1002 is set, a left-press and drag belongs to the application, so the terminal's
own text selection is suppressed unless the user holds the bypass modifier (shift).

That is the trade, and it is a bad one **for this room specifically**. Council exists to
put four vendors' answers side by side so they can be read and taken away; making the
answers harder to select with a mouse in order to make them easier to scroll with a mouse
spends the product's output to buy a convenience for its input. The keyboard path is
complete — it is what the rest of this section fixed — so the wheel would add no capability
at all. §7.8 already records "deliberately absent: mouse support" for the gauges; council
is a different surface and got the question asked again on its own terms, and the answer
came back the same.

Recorded rather than left as a gap, because "nobody tried" and "it was measured and
refused" are different facts, and this repo does not let them render alike.

### 9.11 The room was correct and it was flat

§9.10 fixed the last thing that did not *work*, and the room was driven live the same day.
The report back was three words — *"where are the UI updates?"* — and it was right. Every
sentence in this section so far is about what the room says; none of them is about how it
looks, and the accumulated answer was a surface with one typographic level in it. A seat's
name, a safety claim, a key you can press and four hundred lines of vendor prose all
arrived at the eye with exactly the same emphasis, separated by nothing but two horizontal
rules three rows apart. Everything was true and nothing was findable.

The rule this section is written under is §7.1's second: **every distinction is carried by a
glyph, a word or a number FIRST, and colour only reinforces it.** That is what makes
`--ascii` and `NO_COLOR` correct by construction, and it is also, read the other way, a
budget: a surface that may not lean on hue has to earn hierarchy from shape, position,
weight and air. Council had spent almost none of that budget. What follows is the pass that
spent it, and the constraint every item was checked against.

**No colour was added, and that was not a close call.** The palette is still exactly
`Text / Muted / Identity / SevOK / SevWarn / SevCrit` from `internal/theme` (§7.5), and
`internal/theme` was not touched — it is shared with the stdlib-only statusline binary
(ADR-002), so a token added for a TUI would be a coupling paid for by a binary that cannot
use it. What *was* added is **weight**, which is an attribute rather than a hue: `Strong` is
Identity at full weight and `Alert` is SevWarn at full weight, and `PlainStyles` renders
both as the identity function. That last property is the whole reason weight is safe here —
it changes no cell's width and no line's content, so every layout golden is blind to it and
nothing it marks is the sole carrier of anything.

**The state a seat is in is now a shape.** `done` / `failed` / `cancelled` / `idle` /
`unavailable` were five words at the far right of a 37-cell column, told apart by the word
and by a colour behind it, in a room that holds three of them side by side. Each now leads
with a mark, and the vocabulary is deliberately a **reuse of meanings this room already
owns** rather than a second alphabet:

| phase | mark | ascii | where the meaning comes from |
|---|---|---|---|
| idle | `○` | `.` | the only new glyph — the HUD's own weakest-state dot, with the HUD's own ASCII form (§7.5) |
| waiting / streaming | spinner | `-\|/` | already sat in this slot; it is now the in-flight member of one vocabulary rather than a special case |
| done | `✓` | `+` | the trace's own "the vendor reported this worked", said about the whole turn |
| failed | `✗` | `x` | the trace's own "the vendor reported this broke" |
| cancelled | `⚠` | `!` | what a note and the unavailable card already open with: *this did not complete normally* |
| unavailable | `⚠` | `!` | same claim, and the **word** is what separates it from cancelled |

Two of those share a mark, and that is the design rather than a collision. glyphs.go argues
at length that a character already spoken for is not a mark — but that argument is about a
character meaning two *different* things, and here it means the same thing twice. The
distinction between "cancelled" and "unavailable" is carried by the word, which always
renders, in both glyph sets and with colour off. `TestPhasesRenderAsDistinctMarks` fails the
build if any two states ever render alike; `TestPhaseMarksSurviveASCII` fails it if the one
new character collides with anything already claimed. **Rule 4 is untouched**: the spinner
is still the room's only moving cell, because none of the other four marks move.

**The column header is one line instead of two labels.** `▸Claude Code` at the far left and
`idle` at the far right with twenty-five dead cells between them reads as two unrelated
things, which is what it was. The name now takes full weight — it is the anchor a reader
scans for — the state leads with its mark, and the gap between them is filled with a rule.
The rule is not decoration: it is **this room's existing grammar for "a label and the
numbers that belong to it"**, which is exactly what `turnRule` has always drawn for every
turn in the transcript underneath. The live turn's header and a finished turn's separator
are now the same line form, so a reader learns one shape rather than two, and the header
loses the ability to read as two things at once. It degrades in the right order: below the
width where a rule fits, the **name** is truncated and the state is kept, because a clipped
seat name is still recognisable and a clipped state word is not.

Two cells of air each side of that rule, not one, and the reason is `--ascii`. The ASCII
rule is `-` and the ASCII spinner's first frame is also `-`, so a streaming column at one
cell rendered `------------ - streaming` and the mark disappeared into the rule pointing at
it. Two cells is also what this product already puts around the `│` that separates zones in
the header and the mode line, so the fix and the convention are the same number.

**One rule per column instead of two three rows apart.** The full-width rule under the room
header stays — it is the HUD's anatomy (§7.2) and council is meant to be the same product —
and the per-column rule under the badges is gone. It was the weaker of the two and it was
doing almost nothing: by the time the eye reached it, it had been told nothing the two lines
above had not already said. The header now carries a rule of its own, which separates the
seat from its content in the same gesture that binds its name to its state. **The row was
not reclaimed for the body; it was spent on a blank one**, because what the reading area
needed was air between chrome and content, and a blank line separates two blocks more
quietly than a second horizontal line does.

That row is *reserved* even for a seat with no posture to state, so the bodies of three
columns start on the same screen row. A grid whose rows do not line up is a worse trade than
one empty claim slot — and `MaxScroll` no longer subtracts a literal 3 for the chrome. It
measures the chrome by drawing it, from the same function the renderer uses, which fixes an
off-by-one that was already live on a column with no badges.

**The badge line looks like a claim instead of like debug output.** It is unchanged in what
it says — `TestSandboxBadgesAreNeverBlanket` still guards that, and §9.2's argument is
untouched — and changed in three ways in how it is shaped. It is indented to the seat name
above it, so it reads as a property of that seat rather than as the first line of the reply;
an unindented row of bare lowercase tokens at the top of a column is precisely what debug
output looks like. Its cost is right-anchored, giving the two chrome rows one shape twice
over: label on the left, value on the right. And the posture badge takes weight and the
warning hue **when, and only when, it says this seat can change your files** — `WRITES` and
`unsandboxed` are loud, `ro:*` stays chrome, `gated` takes the weight without the severity
because a gated seat is the room working rather than a risk.

That last one is the pass's only change with a safety argument behind it. §9.2 is emphatic
that a claim you cannot see is not a claim, and then the room drew `unsandboxed` at exactly
the volume it drew `ro:tools` beside it. Colour is still redundant — the words break the
`ro:` prefix on purpose and are what actually carry it, which is why the plain style set
renders every badge as its own bare word — but a claim a hurried reader skims past is doing
half its job. `TestAWriteCapableBadgeDoesNotRenderLikeAReadOnlyOne` pins it.

**The degraded columns are cards.** `⚠ Codex is not seated`, a blank, a reason paragraph at
the same indent, a blank, and a closing sentence at the same indent is three fragments
floating in a column: nothing on screen said the reason belonged to the title, so a
three-seat room with one dead seat read as unrelated paragraphs. Every card in a column now
has one grammar — **a title at weight, its body hanging under it** — and it costs no rows.
The room was already doing this in the two places it needed it least (the prompt echo
indents under its `›`, a failed call's detail indents under the call) and in none of the
places it needed it most: the notes, the unavailable card and the approval card, where a
wrapped second line started hard against the column edge and read as a new statement. On
notes the mark carries the hue and the words stay plain, which is the same split the trace's
outcome marks make.

**The transcript reads as a conversation.** A brief and the answer to it arrived as
consecutive lines at the same indent, told apart only by a glyph at the start of one of
them — a distinction you have to *read*, on a surface built for comparing four answers at a
glance. There is now a blank row between them, and the echoed brief takes full weight,
because in a column of vendor prose the user's own words are the thing you scroll looking
for. **The row is a swap, not a cost**: it came from between the turns, where a labelled
full-width rule was already doing the separating. The transcript is exactly as tall as it
was. What that leaves is three boundaries with three strengths, ranked: a labelled rule
where the turn changes, a blank row where the speaker changes, a blank row where the kind of
content changes (what the seat *did*, then what it *said*).

**The footer has a figure and a ground.** Six items of identical weight separated by
identical bars is a wall the eye slides off — which is, concretely, how a room with working
scroll keys, page keys, `g`, `G` and a full-width expand got reported as having no way to
scroll at all (§9.10). The key renders at full intensity and its label recedes, the same
figure/ground split the column header makes between a seat and its state, and it costs no
cells. Two items are dropped outright in a room with one seat on screen: `f` expands the
only column to the width it already has and `tab` cycles focus around a single seat, and a
mode line that promises a key which does nothing is §7.8's surprise pointing the other way.
The gate line got the same treatment, where it matters most — the call about to be run was
being drawn at the same faint volume as the keys that answer for it.

Two smaller repairs fell out of the same reading. The room header now separates its own name
from the workspace with the `  │  ` the HUD's header uses, instead of a bare space that made
`council ~/code/telltale` read as one run-on label — and fixing that surfaced an off-by-one
in the header's gap arithmetic, which had been overrunning the frame by exactly one cell and
having `fit` quietly eat it. And the collapsed-seat notice is truncated with an ellipsis
rather than handed to `fit`, which cuts silently: at 120 columns on the reference machine it
had been losing the last word of its own remedy and looking like a sentence that stopped.

**What was declined.** Hoisting the badges into the column header's gap at the single-column
tiers, which would have bought a body row and killed the widest dead gulf: it makes the
chrome height depend on content, so the body would grow a row at the moment a cost arrived
mid-turn — a layout jump, which §7.1 rule 4 does not budget for. Giving `cancelled` a glyph
of its own: nothing unclaimed in the ASCII set reads as "stopped", and the rule that a
distinction may be carried by a **word** is there precisely so a glyph does not have to be
invented for every case. A "role" line under each seat naming what that vendor is for
(review, IDE, tiebreak): council has no such field, ADR-010's allocation is a fleet fact
rather than something this room measured, and a room that stated it would be asserting
something no adapter sourced.

### 9.12 The scroll keys worked; which column they moved was the thing nobody could see

§9.10 fixed a room whose scroll keys were dead in the mode a finished turn drops you into.
The room was then driven again, and reported as unable to scroll a **second** time:

> "scrolling works for your window. i tried scrolling up/down in agy and cursor. could not."

Every word of that is accurate, and none of it is a bug. The keys address the **focused**
column, they have always addressed the focused column, `tab` moves focus in both modes since
§9.10, and the second and third seats scroll exactly as the first does once the keys are
pointed at them. `TestFocusThenScrollMovesThatColumn` says so in the product's own terms —
two tabs, one `↑`, the third column leaves its tail and the first does not move — and it is
kept as a test precisely so the changes below are never mistaken for a mechanism fix.

**What failed is the affordance, and it failed in three places at once.** Each of them is
individually defensible, which is why reading the code did not surface it:

- **Three columns each said they were hiding something; one of them said how to look.**
  `↑ 36 more above` appeared verbatim on every column with content off screen, and the key
  hint rode only on the focused one — correctly, since naming `↑↓ scroll` on a seat those
  keys do not move would be three false claims (§9.10). The result is that the *unfocused*
  markers were the ones a reader was most likely to be staring at, and they named nothing.
  Pressing `↑` then moves a column the user is not looking at, and a scroll key that
  visibly does nothing is a scroll key that does not work.
- **The focus marker was competing with three identical anchors.** §9.11 gave every seat
  name full weight, on the correct argument that a name is what a reader scans for. The
  cost only shows up live: with all four names at the loudest level this surface has, the
  entire distinction between the column the keys move and the three they do not was one
  `▸` glyph in a frame carrying four columns of prose.
- **The compose mode line named the arrows and not the key that aims them.** §9.10 wired
  `tab` into compose *because* the scroll keys address one column — it says so in as many
  words — and then listed `↑↓ scroll` on that line without `tab focus` beside it. The one
  moment the user is certain to want both is the moment four long answers land, which is
  exactly when this line is on screen.

**Three fixes, all of them words and weight, no new colour and no new key.**

A marker on a column the keys do not move now names the key that would move them there:
`↑ 36 more above  │  tab to focus`, against the focused column's `↑ 51 more above  │  ↑↓
scroll  │  f expand`. This is the same rule the existing hint follows — *a marker states the
key for THIS column and never a neighbour's* — applied to the case that had been left blank
rather than a new rule bolted beside it. It needs none of `f`'s mode-awareness, because
`tab` really does move focus in both modes; and it is empty in a room with one seat on
screen, where there is nothing to tab to, for the same reason the mode line drops `f` there.

The seat name's weight now says **which column the keys move**. Unfocused names keep the
identity hue and give up the weight; they are still names and still legible, and they have
stopped competing with the one fact that varies across the row. This needed a small type
rather than a bool: `seatFocus` separates *is this column marked* from *do the keys move
it*, because the two agree in the side-by-side tier and part company in the tabbed and
expanded ones, where the tab bar above already carries a marker and the column beneath it is
still the one being scrolled. Conflating them is what made a single call site pass
`focused=false` for a column that had the keys.

`tab focus` joins the compose mode line, immediately after the arrows it aims. It is offered
whenever more than one seat is on screen and deliberately **not** gated on whether some
column currently overflows: a hint that appeared the moment a reply grew past its column
would be a footer cell that changes while output arrives, which §7.1 rule 4 does not budget
for — and this line's promise is about what the mode can do, not about what the vendors
happen to have said this turn.

**What did not change**, because the rules that produced the original design are still the
right ones. The count is never traded for a hint, in either form. No badge, no help row and
no keybinding moved — the help panel already documented `tab … in compose too`, and its
17-row budget (§9.11, `TestHelpFitsTheSmallestRoom`) is untouched. Every distinction added
here is a word or an attribute: `PlainStyles` renders the focused and unfocused headers
identically, so every layout golden is blind to the weight, and `tab to focus` is the same
string under `--ascii`.

The general lesson, in this file's own terms: §9.10 recorded a mechanism that was complete
and unreachable. This is the same shape one level up — a mechanism that was complete,
reachable, and **unattributed**. The room said *something is hidden here* three times and
*here is how to see it* once, and a user reading the two-thirds of the room that named no
key concluded, reasonably, that the feature was missing. Nothing measurable was wrong;
what was wrong is that the honest thing and the actionable thing were on different columns.

### 9.13 The badges were honest and nobody knew what they meant

§9.2 argues that every column states its own posture, §9.11 gave the two that mean "this
seat can change your files" weight and the warning hue, and twelve amendments of ADR-008
made each word behind them defensible. The room was then driven, and the report back was
a question:

> *"why do i care codex and agy are 'unsandboxed'? what does this mean, why are they
> sandboxed, and must they remain that way? i'm really confused here."*

Every previous complaint in this section was about something being wrong. This one is not.
The badges are correct, they are the most carefully-argued strings in the product, and to
their primary user they were **three lowercase tokens with no reachable explanation.**
`unsandboxed` reads as jargon-with-a-negation, which invites exactly the two wrong
readings the question contains: that the sandbox is something council switched off, and
that a sandbox is what was keeping the room safe.

**Two things were missing, and only one of them is a legend.**

The first is the vocabulary. There was no plain-English gloss of the badge words anywhere
a user could reach without reading an ADR. What existed was four muted lines at the bottom
of the help panel, below the fold at a 24-row terminal, saying that each column states its
own posture — a sentence about the *policy* rather than about any of the words.

The second is worse and was found by grep. **`SandboxClaim.Detail` rendered nowhere at
all.** It is the full argument behind each badge — what was passed, what was measured,
what is therefore claimed — it is written per vendor per OS, it is asserted by tests, it is
quoted into ADR-008, and no surface read it. The field's own doc comment said it was "shown
in the degraded/help text". It was not shown anywhere. §9.2's rule is that a claim you
cannot see is not a claim; **the argument for a claim is under the same rule**, and this
one had been invisible since the badges landed.

**The fix is a second help page, and the split is by kind rather than by length.** `?` now
cycles: keys, postures, closed. Both pages spend the same hard 17-row budget (§9.11), both
end with the `?` line that leaves them, and three presses always return the room from
anywhere — the panel's one non-negotiable property, since `?` is the only documented way
out of it. Page one's closing paragraph became the pointer to page two, which is what makes
a second page a feature rather than a place.

Page two is a legend of **every** badge this product can render, not only the ones the
current room shows. A user who has never typed `--write` should be able to find out what
`WRITES` means before they type it, and a room-specific legend could only ever explain the
room you are already in. Each entry renders its badge through `Styles.ForSandbox`, the same
function the column header uses, so the legend cannot teach one weight and the room show
another; `TestEveryBadgeIsExplained` walks every `SandboxLevel` and fails the build when a
badge exists with nothing here to say what it means.

**Nothing was softened, and that is asserted rather than promised.** These are glosses on
the badge words, never replacements for them.
`TestThePostureLegendDoesNotSoftenAnyClaim` pins the load-bearing phrases — `unsandboxed`
still says *nothing restricts*, *measured*, *change your files*; `ro:requested` still says
*never observed* — and forbids "read-only", "safe" and "cannot write" from appearing in the
gloss for any level that can write. The badges break the `ro:` prefix on purpose; a legend
that put the word back would undo that in the one place a reader goes to have it explained.

Below the legend, and below the fold at the 24-row floor, is this room's own seats with
each one's `Detail` in full — the first time that field has rendered. The ordering is
deliberate: the detail is unreadable without the vocabulary, and the vocabulary fits the
budget where four paragraphs of measured prose never could. It is the same trade page one
already makes with its closing paragraph, and the residual is stated rather than discovered:
at the shortest terminal this room will draw in, the per-seat half is scrolled past rather
than absent.

**Every `Detail` was reordered so its first clause answers "so what?".** Not one factual
clause was removed, weakened or added; what changed is which end of the sentence the
consequence sits at. `"named write/exec tools denied and MCP servers dropped; verified
against..."` opens on a mechanism a user has to decode before they learn anything, and now
opens *"this seat has no write or shell tools in its session, so it cannot edit your
files"* with the verification and the deny-list residual behind it. Codex on Windows opens
on *"nothing at the OS level stops this column reading or writing here"* rather than on the
flag that produced it. This is §7.1's rule about glyph-word-number ordering applied to
prose: the distinction goes first and the evidence reinforces it.

**The question's third clause got an answer too, and it is the one that mattered.** *Must
they remain that way?* The badge table answers it where a first-time reader
is, and the answer is not about flags: **no badge is what keeps this room out of your
files — the workspace is.** `unsandboxed` on Codex is not a setting anyone chose to leave
off; both sandboxed modes were measured failing every process spawn there, so read-only was
a seat that could not read (ADR-008, twelfth amendment). The control that holds is `--cd`
into a throwaway worktree, and the fleet contract rules the same way independently:
`agent-ops` ADR-012 rules capability parity, with guard wiring rather than lane shape as the
control. A column that looked read-only because of a broken sandbox was never a safety
property; it was a defect wearing one's clothes.

**No posture flag moved.** Whether council should keep asking agy for `--mode plan
--sandbox` when both are measured to do nothing is an open decision (§9.6b) and belongs to
the owner, not to a documentation pass. This section changed what the room *says* about the
posture and nothing about the posture.

*(That decision was made the same day, separately and by the owner: the flags come off. §9.6b
carries the ruling and ADR-008's seventeenth amendment records it. The split held — the pass
that changed the words and the ruling that changed the behaviour are two changes with two
arguments, which is what let each be judged on its own.)*

The general lesson, in this file's own terms: §9.10 found a mechanism that was complete and
unreachable, §9.12 found one that was complete, reachable and unattributed. This one is a
claim that was complete, visible, attributed — and **untranslated**. Twelve amendments of
adversarial care went into making three words defensible to a reviewer, and none of them
asked whether the person the words are *for* could read them. Honesty that only survives
an expert audit is a claim made to the wrong audience.

### 9.14 the honest sentence was in the wrong room

§9.2 rules that `PhaseWaiting` must never be mistaken for streaming, and it is right. The
card that enforced it was reported from a live room in the bluntest terms this project has
had yet:

> *"'working. this vendor reports no incremental output..' looks ugly as fuck. i get why you
> put it there but yuck — you can hide the wiring underneath the floor of our council room."*

**Both halves of that are correct, and they are about different things.** The distinction is
load-bearing and stays. What does not belong in the body of every waiting turn is the
*argument* for it. Read it as a user rather than as its author: "this vendor reports no
incremental output" is a sentence about council's own plumbing, in council's own vocabulary,
occupying the space someone opened this room to read an answer in. And because two thirds of
the seats are `GranFinalOnly`, it was not an occasional card — on an ordinary turn it was most
of what was on screen, three columns wide, until the vendors came back.

**What carries the distinction now was already there, and had been all along.** The column
header names the phase: `waiting` against `streaming`, on every frame, in both glyph sets,
above the scroll where it cannot be read past. Beside it the granularity badge says why —
`final only`, or a deliberate blank. That word is the claim; the body sentence was never the
claim, it was a *paraphrase of the badge*, printed where the badge could already be seen.

So the body is one line. Three of them, because they are three different claims and collapsing
them would be the failure §9.2 exists to prevent one level down:

| when | line | why not the others |
|---|---|---|
| final-only, nothing yet | `working — the reply arrives whole.` | states what to expect, from a measurement two vendors earned |
| granularity never established | `working — nothing has arrived yet.` | must NOT borrow the sentence above — the fifth amendment's rule that an unestablished claim may not wear a measured one's words |
| it has acted but not spoken | `working — the steps above are what it has done so far.` | there IS something on screen; pointing at it beats describing the seat |

None of them uses a word about incremental output, deltas or granularity. `TestWaitingIsNotStreaming`
now asserts that in both directions: the body says what to expect, the frame carries the word
`waiting`, a streaming frame does not, and the vendor-internals vocabulary is **absent** — which
is the assertion that stops the explanation creeping back in one clause at a time.

**The wiring went under the floor, and the floor is the help panel's posture page.** That page
already exists (§9.13) and already had the shape for this: a claim on the column, its argument
somewhere it can be read properly. What it did not have is any gloss of the granularity word at
all — §9.13 gave the sandbox badges a legend and left the badge beside them undefined, which was
survivable only because the waiting card was reciting the explanation in the reading area.
Taking that out is what turned the gap into a debt.

**The gloss sits inside each seat's own block rather than in a room-independent legend, and that
is a deliberate departure from how the sandbox words above it are presented.** §9.13's argument
for a legend covering badges this room does not show is that a user who has never typed
`--write` should learn what `WRITES` means *before* they type it. There is no equivalent here:
**nobody chooses a granularity.** It is a property of whichever vendors are installed, so the
only granularity words a reader can ever meet are the ones their own room is already displaying
— and a sentence beside the word it defines beats making someone match two lists. It goes under
the posture rather than beside it, because the two answer different questions about one seat and
only one of them has consequences.

`TestEveryGranularityIsExplained` walks the type and fails the build for a value that can render
on a column with nothing to say what it means — the guard `TestEveryBadgeIsExplained` gives the
sandbox levels, for the same reason. `GranUnknown` gets an entry precisely because it prints no
word: the blank is the claim, and it is the one case a reader cannot decode by reading the
header.

**The residual, stated rather than discovered.** Each seat's block grew a line, so at the 24-row
floor the last seat's paragraph is cut a little earlier than it was. That is §9.13's own stated
trade, one line deeper — the per-seat half is what a taller terminal gets, and nothing above the
fold moved. The panel's hard 17-row budget is untouched and `TestHelpFitsTheSmallestRoom` still
holds it.

The general lesson, in this file's own terms: §9.13 found a claim that was true and
untranslated, and translated it. This is the same audit run once more on the *result* — because
the translation was correct, and it was put in the wrong room. A sentence can be honest, legible,
and still wrong to print, if the place it prints is the place someone came to read something
else. Every earlier section here asked whether the room says the truth. This one is the first to
ask **how much of the room the truth is allowed to take up.**

### 9.15 getting an answer out of the room

Council exists to put several vendors' answers where they can be compared. What it never had
is a way to take one *away*. §9.10 already noticed the gap from the other side and refused the
obvious fix: mouse support was rejected partly because enabling the wheel claims the left
button too, which would cost the native click-drag selection this room's output depends on.
That refusal protected a workaround. It did not build a feature.

> *"go with your yank key suggestion"*

**`y` copies the focused column's reply. `Y` copies the whole turn.** Two keys rather than one
with a modifier meaning, because they produce different documents — and `shift` for the wider
version of a motion is what this room already does with `g` and `G`.

**What `y` takes is the sanitized `Body` the renderer is showing, and the three things it is
not are each a rule this file already holds.** Not the raw stream: everything on `State` has
been through the redaction and sanitize choke point, and a clipboard is a *worse* place for a
credential than a screen because it outlives the room. Not the trace: what a seat did and what
it said are different kinds of claim (§4a.1), and that does not stop being true because the
destination is a document. Not a neighbour's: it addresses the **focused** column, the same
column every scroll key addresses, because a copy key that took from somewhere other than
where the eye is would be §9.12's failure with a clipboard attached.

It falls back to the newest finished turn when the current one has produced nothing yet — "the
last answer" is what a user means by this key, and the notice names the turn the text actually
came from rather than the one on screen.

**`Y`'s format has one job: be readable a week later.** Seat headings, and the brief at the
top, because four answers to a question the file does not contain are unreadable. The brief is
the user's own words, which §9.9 already echoes un-redacted on the user's own screen for the
same reason — and what rode *along* with it does not go in, which is the same boundary §9.9
draws: a first turn carries the `--brief` file whose content is deliberately kept off `State`,
and a rebuttal turn carries other vendors' words. Only seats that took **this** turn are
included; a seat that sat out still holds an older reply, and filing that under this turn's
heading would be the room inventing a conversation into a document, where it outlives every
chance to notice.

**The key collision is the interesting part, and it was already resolved.** `y` approves a
tool call a vendor is blocked on (§9.8). `key()` routes a pending gate to `gateKey` first and
`gateKey` answers `y` itself rather than falling through, so the approve key keeps the letter
it has always had and yank does not exist while a vendor is stopped. That was true before this
landed; what is new is that it is now **asserted**, because losing that race would mean a
keystroke the user believes approved a write quietly copying text instead — and their next
move would be to press it again. In compose mode `y` is the letter y, the same rule that keeps
`q` the letter q there (§9.10).

**The mechanism is OSC 52, and its limit is stated rather than glossed.** Verified by reading
the installed module rather than the internet, because v1 answers for this are wrong:
`charm.land/bubbletea/v2@v2.0.8`'s `clipboard.go` returns a `Cmd` whose message `tea.go` turns
into `ansi.SetSystemClipboard`, emitting `ESC ] 52 ; c ; <base64> BEL` **unconditionally** — no
capability probe, no terminal query, nothing that can decline in a way this program could
observe.

| | claim | strength |
|---|---|---|
| the key produces the command, carrying the right text | asserted by a test that calls the `Cmd` | measured |
| the sequence reaches the terminal | bubbletea writes it on the next message pump | read from the module source |
| the terminal honours it | Windows Terminal accepts OSC 52 writes in current builds | ~~**INFERRED** from documented behaviour, not run~~ — **FALSIFIED on macOS, 2026-08-10** |

That last row cannot be closed from inside this repo: the only observer that could settle it is
the terminal, and it sends nothing back. So the notice claims what council **did** — "copied
claude code's turn-3 reply" — and never what the machine now holds, and the honest check is a
person pressing `y` and then `ctrl+v`. The notice is not decoration for the same reason: with
no acknowledgement available, it is the *only* feedback that the key did anything, and a silent
copy would be indistinguishable from a terminal that ignored the sequence — the ambiguity
§4a.1 forbids everywhere else here.

**Amended 2026-08-10: the inference was wrong, and it took a second machine to
see it.** On the macOS box `y` reported "copied …" and the clipboard was
untouched, in the same build where the key works on the Windows one. Terminal.app
does not implement OSC 52 clipboard writes at all; iTerm2 does but ships the
permission off. Nothing was broken in council — the gauge was reporting an action
it had structurally no way to observe, which is the failure §4a.1 exists to
prevent, wearing the costume of a limitation everyone had already agreed to.

**A native helper is now tried FIRST wherever one exists** (`clipboard.go`):
`pbcopy` on darwin, `wl-copy` then `xclip` on linux, and nothing on Windows,
where OSC 52 is measured working and a process spawn per keystroke would buy
nothing. The reason is not that the helper is better plumbing — it is that it is
**checkable**. `pbcopy`'s exit status is a fact about the clipboard; the escape
sequence has none, and never will. OSC 52 stays as the fallback because it is the
only mechanism that survives SSH.

The two paths never both fire. Sending the sequence as well would put the text on
the clipboard twice where both work — harmless — and would also hide the failure
of one behind the success of the other, which is not.

The test story changed with it, and this is the part worth carrying: the old test
asserted the `Cmd` was produced, and **passed for two days while the key did
nothing on macOS**. It was asserting the artifact rather than the effect, the
mistake this file records four other times. The native path is now round-tripped
through the OS (`pbcopy` in, `pbpaste` out) and the fallback is driven by a stub,
so the mechanism a machine happens to have no longer decides which half of the
feature its suite covers.

**An empty yank issues no command at all.** Writing `""` through OSC 52 is the documented way
to *clear* a clipboard, so a copy key that found nothing to copy would silently destroy
whatever the user had. "Nothing happened" and "your clipboard is now empty" are different
outcomes and this key must not spell them the same way.

**The help row is a merge, and the merge is the honest shape rather than a saving.** The
panel's budget is hard (17 rows, §9.11) and a copy key documented below the fold is a copy key
nobody finds — but the reason `y`, `Y` and the gate's `y`/`n` share one line is that they
*collide*, and the one place a reader could learn that is the line naming all of them.
Splitting them would have spent a row to make the collision harder to see.

**Declined: writing the turn to a file as a fallback.** A `~/.telltale/council/last-turn.md`
would work in any terminal and needs no escape sequence at all, which makes refusing it worth
an argument rather than a sentence. ADR-008's ninth amendment ratified council writing exactly
one file and ruled what may be in it: **keys, not content** — session ids and no transcript,
because each vendor already stores its own history and anything copied there would be a second
copy of a private conversation in a location the user never chose. A file of four vendors'
answers in the state directory is precisely that, and it would break the rule in the same
release that a mechanism needing **no disk at all** was available. The terminal-support
residual is real and is paid in a notice, not in a contract.

The general lesson, in this file's own terms: §9.10 measured a fix, refused it for a good
reason, and recorded the refusal — which is the process working. What it did not do is ask
what the user had actually been trying to *do* when they reached for the mouse. The answer was
never "scroll"; it was "take this answer with me", and that want went unnamed for two sections
because the request arrived wearing the costume of a mechanism.

### 9.16 `/flow`: the hop holds the authority, and it has to say so out loud

A `/flow` chain is `@seat verb [task] [write:<path>]`, arrows between hops, dispatched **one hop
at a time to exactly one seat**. Ordinary dispatch can address Claude by default, one or more
explicitly mentioned seats, or the whole committee via `@all`; a flow hop has no such choice.
It is an instruction to its one named seat, and a chain that fanned out would hand each hop's
authority to seats the chain never mentioned.

Nothing becomes a flow without the literal `/flow` prefix. A bare `->` in prose stays prose —
"compare approach A -> approach B" is a question — because the alternative is ordinary briefs
silently acquiring orchestration semantics, write gates and all.

**Write authority is declared, never inferred.** The parser as shipped decided a hop could mutate
the workspace by checking whether its last token contained `.`, `/` or `\`. Spelled out, that is:
a sentence that ended in a period was a write hop; so was a task naming a file it was only meant
to *read*; so was a Windows path quoted inside a question. English does not grant permissions and
neither does punctuation. **Only `write:<path>` does.** The verb is a label — `publish` confers
nothing.

The target is checked at **parse** time, which is the load-bearing part rather than a tidiness
preference: once parsed, the seat is spawned holding authority and pointed at the path, so parse
is the last moment the answer is still free. Refused there — absolute paths in *either* platform's
spelling (`filepath.IsAbs` alone does not consider `/etc/shadow` absolute on Windows), `..` in any
segment under either separator, an empty target, two targets on one hop, and a `write:` token
occupying the verb slot. The last is refused loudly rather than read as a read hop: silently
demoting a declared write is the same class of lie as silently promoting a read, and it is the
more dangerous one, because the user believes they authorized something and the log agrees.

**Posture belongs to the step, and it only ever moves down.**

| the hop | the room | what happens |
|---|---|---|
| no `write:` target | `--write` | **read** posture. The room's authority is not the hop's. |
| no `write:` target | read-only | read posture, unchanged |
| `write:<path>` | `--write` | gate first (`y`), **then** spawn, at the room's write posture |
| `write:<path>` | read-only | **blocked**, and nothing is spawned |

The bottom row is where the two dishonest options live and both are refused. Running it read-only
would let the seat return, the hop report `returned`, and the chain advance past a publish that
never happened — §4a.1's ambiguity with a receipt attached. Upgrading the room would mean a
program granting itself authority the person who started it withheld. So there is no `y`/`n` card
here at all: a gate implies a keystroke exists that makes the action legal, and none does. The
notice says the room is read-only and names **`/write`**, the control that would change it.

That last sentence used to name the *flag*, and §9.17 quotes the older version of it as the
tell that a control was trapped at launch. It stayed true for exactly as long as a relaunch was
the only remedy; `/read` and `/write` made it false and nothing was asserting on it, so the
room went on telling a user with a chain half-typed to quit and start over. Corrected, and
pinned by `TestAWriteHopIntoAReadRoomNamesTheControlNotARelaunch` — see §9.17's closing note on
why a fix that lands everywhere except the sentence that motivated it is the shape to look for.

**The gate is before the spawn, and that is now pinned by a test that counts processes.** On a
write hop, zero vendor processes exist until `y`; `n` cancels with zero. A gate drawn after the
spawn is a notification.

**The persistent seat forced a real choice.** Claude's seat is a long-lived process (§9.8) and its
posture is argv — fixed at spawn, with nothing in the stream-json envelope able to change it
mid-session. This is the same measured constraint as `cwd`, which is why `/cd` respawns rather
than redirects. So a hop needing a posture the live process was not launched with can either be
sent anyway or trigger a respawn. **It respawns**, on `/cd`'s own `--resume` composition and under
the same one-attempt probation, and the column says so. Sending it would have been the silent
downgrade this whole section exists to forbid, and the tell would have been invisible in exactly
the way that matters: the badge would read READ while the process still held write flags.

**Retention was running backwards.** The artifact prune sorted filenames as strings, so
`turn-10-*.md` sorted before `turn-2-*.md` and the cap deleted the *newest* ten and kept the
oldest — reachable only at turn 10, which is the first moment retention does anything at all. It
sorts by the parsed turn number now. A name that does not parse sorts first and is pruned first,
because an unrecognised file in that directory is not a receipt this store wrote and must never
displace one that is; and nothing there panics, since it reads a real directory that can hold
anything.

**How these are tested, and why it is worth a paragraph.** Every one of the six security
properties is asserted on something observable — the number of processes spawned, the exact argv
handed to the spawn, or the chain's state — and never on a helper returning `true`. This repo's
recorded failure mode is a test that checks the flag instead of the effect, and it would be
perfectly at home here: a flow that computed "read posture" correctly and then spawned a write
invocation passes any test that asks the posture function what it thinks. The posture assertion
witnesses `@cursor`'s argv rather than `@codex`'s for a measured reason — on Windows, codex's read
and write sandbox flags collapse to the same value, so codex's command line cannot testify to a
posture on this machine.

### 9.17 a control you need mid-session cannot live in a flag

**The rule: state that changes while the room is open is reachable from inside the room.
A flag is for what is true at launch and stays true.** A control that only exists as a flag
answers, at the one moment you cannot yet have the question, something you will only learn by
working — so the remedy for noticing it is to quit the room and lose the conversation.

This is not a new principle here. It is the one council has already applied twice, case by
case, without ever writing it down.

- **The workspace stopped being an invocation input.** `--cd` still exists, but `/cd <dir>`
  moves the room between turns and every seat follows on its next dispatch (§9.16, `roomcmd.go`).
- **Posture stopped being opt-in.** The room writes by default and `--read` is the opt-out,
  because once the gated seat could raise an approval card, "all the flag still did was make a
  room you opened to get work done unable to do any until you remembered a word" — and that
  demotion cites the workspace one as its precedent.

Two demotions with the same argument is a rule. `roomcmd.go` even states the scope it was
decided under — "the workspace is **the one piece** of room state the P0 demands be movable
from inside" — and that claim is what has now failed. It was true when the room could not run
long enough for anything else to drift. A room used as a daily driver drifts in several
places at once.

**The tell is a refusal that names a flag.** §9.16 has one already: a `/flow` write hop into a
read-only room is blocked, and "the notice says the room is read-only and names the flag that
would change it." That sentence is the defect in miniature — the room knows exactly what you
want, knows exactly what would grant it, and can only tell you to quit and start over. Any
notice whose remedy is a relaunch is this bug.

#### The sweep

Every council control, classified. This is a **source read**, not a live run — the claims below
are about where a control is reachable from, which argv and `roomcmd.go` settle, not about
vendor behaviour, which would need measuring.

| control | verdict | why |
|---|---|---|
| workspace (`--cd` / `/cd`) | **compliant** | the launch flag has an inside-the-room twin; the flag's own help says so |
| `--fresh` | **violates** | a conversation fills up *by being used*. The only reset is room-wide and launch-time, so clearing one seat costs the other three their threads |
| `--trace` | **retired by `/trace`** | its own doc said it answers "a question that is asked on the days a turn is inexplicably slow" — a day you identify from inside a slow turn. The flag remains for a run you already intend to measure; see below for what the sweep found underneath it |
| `--read` (posture) | **retired by `/read` and `/write`** | see the refusal above. Note what is *not* an objection: posture is deliberately never restored from the saved room, because "a posture that can arrive from a file is not one anyone typed." A posture typed into the composer is typed. The flag stays: opening a room that only talks is a real thing to want at launch |
| `--auto` | **retired by `a`** | whether the gated seat asks before each tool call is a preference you form partway through a batch, not before it — so the surface is a third key on the card, not a room command. The flag stays: opening a room you already know you will not be watching is a real thing to want at launch |
| `--vendor` | **retired by `/seat`** | who is *seated* was launch-only. `-@seat` routes one turn and is explicitly "a different control from an @mention" — routing is not reseating. The flag stays: opening a room with a chosen set is a real thing to want at launch |
| `--brief` | **arguable, not filed** | it is defined as first-turn context, so re-briefing is a different feature rather than a missing surface for this one. Left out deliberately; do not fold it in without deciding that question on its own |
| `--ascii`, `--no-title` | **legitimately launch-only** | properties of the terminal, not of the room. They do not change while it is open |
| `--resume`, `--write` | **vestigial** | accepted and ignored; kept for muscle memory |

#### What satisfying the rule costs

Not every control can simply be flipped in place. Posture and `cwd` are **argv** — fixed at
spawn, with nothing in the stream-json envelope able to change them mid-session — which is why
`/cd` respawns the persistent seat rather than redirecting it. That is the pattern, not an
obstacle: respawn lazily on the next dispatch, under `--resume` composition and the same
one-attempt probation, and let the column say so. A mid-session control may cost a respawn; it
may not cost the room.

#### The surface, and why it is not a slash command

**Ruled: a key on the focused seat.** Focus already ships (`▸` + `Strong`,
`hierarchy_test.go`), so the seat is already named without anyone typing its name.

The alternative was a room command with the mention grammar (`/clear @codex`), and the argument
against it is vocabulary. `roomcmd.go` intercepts only a draft that *is* the command,
"so no vocabulary is quietly stolen from the conversation" — but two words are already spoken
for, `/cd` and `/flow`, and `/clear` is a word people mean for a vendor, since it is a real
Claude Code command. A key takes nothing from the composer.

**It must not be automatic.** The obvious version — the room notices a seat is near its ceiling
and clears it — reads `context_pct`, which for the codex adapter is declared `Derived`, not
`Reported`: Codex ships a denominator, telltale computes the percentage, and the HUD marks it
with a leading `~` "rather than passing it off as a vendor figure." ADR-005 settled this class
for the fleet — status is advisory, never a gate, and nothing irreversible branches on it. A
dropped thread is irreversible.

#### What `c` does, and the two things it gets right by construction

The first control built to this rule. `c` in view mode arms a confirmation for the focused seat;
`y` drops that seat's thread, `n` keeps it, and **any other key cancels**. The flow gate falls
through to `viewKey` and this one does not, because they ask different questions: that one blocks
a chain already running, so reading the columns is part of deciding, while this one interrupts
nothing and the safe reading of a key nobody meant to press is to put the thread back out of
reach. It refuses while a turn is in flight — `/cd`'s rule, for `/cd`'s reason — and a seat with
nothing to clear is told so rather than handed a card whose `y` does nothing.

**The ordering is load-bearing and it fails silently.** `seatProcess` re-arms `resumeIDs` from
`m.sessions` whenever it replaces a live process; that is what carries a thread across a `/cd`.
So the deletes come *before* the kill. Reversed, the id is handed straight back, the next brief
resumes the conversation the user just ended, and every word on screen still says cleared —
which is why it is a named test (`TestClearSeatKillsThePersistentProcessAndDoesNotRearmTheThread`)
rather than a comment.

**The drop is saved immediately, not at the next dispatch.** The room file is what a reattach
reads, so a clear held only in memory would be undone by quitting: the user ends a thread and
finds it waiting for them, which is the failure the control was built to remove.

**`Cleared` is its own field, not `!Restored`.** "This seat never had a thread" and "you ended
this seat's thread" reach the same next brief, and collapsing them is zero-vs-absent (§4a.1)
applied to a conversation. The marker is a labelled rule in the transcript's own grammar, drawn
last because that is when it happened — and **the turns above it stay**. What was cleared is the
thread the next brief would have continued, not the record of what was said; blanking the
reading surface to report a vendor-side change would be the room destroying the thing it exists
to show. It retires in `startTurn`: once the brief is sent the seat has a thread again, and a
marker outliving that would describe a break the room has already healed.

#### `/trace`, and the thing the sweep found underneath it

**The clock was always running.** `runner/clock.go` measures every turn unconditionally —
`newClock`, `begin` and `end` sit on the ordinary path — and `--trace` only decided whether
`emitTurnClock` had a sink to hand the record to. So every slow turn anyone ever watched *was*
measured, at full spawn/wait/stream resolution, and the numbers were dropped on the floor
because nobody had predicted that turn before the room opened.

That reframes the fix. A `/trace` that merely installed the sink from here on would move the
prediction from launch to the previous turn rather than retiring it — you would still be waiting
for the slow turn to happen *again*. So the sink is installed for the life of the room and keeps
the last **200** records (`maxTraceRing`: `maxHistory`'s 50 turns at four seats, so the trace
reaches back exactly as far as the transcript that made you want it). Turning the trace on is
opening a *file*, and the first thing that file receives is what the room already held.

`/trace <file>` enables and reports how many held turns it wrote; `/trace off` stops; bare
`/trace` reports where it is going, or how many turns are held if it is off. The ring keeps
filling while the trace is off, so stopping costs nothing and starting again reaches back over
the gap.

**Three deliberate refusals, each with a reason that is not "consistency":**

- **No council-chosen path.** Bare `/trace` reports and never enables. A no-argument form that
  picked a file would make council write a second file on its own initiative, and the sentence
  in `README.md` and `CLAUDE.md` — the only mode that writes anything to disk, one file,
  `room.json` — would become false. A `--trace`/`/trace` path is one the user named.
- **It does not refuse mid-turn**, unlike `/cd` and `c`. Those change state the seats are
  actively using; this opens a file on the room's side and changes nothing any vendor can
  observe. The turn you cannot explain is usually the one still running, so refusing here would
  refuse at the only moment that matters — and because the clock emits at `end()`, a trace
  opened mid-turn still catches that turn.
- **A relative path resolves against the ROOM's workspace**, not the process's cwd, matching
  `/cd`. The room is the frame of reference for everything else typed into it.

**The help panel taught the sweep something too.** `/trace` first went in below the panel's hard
17-row budget, on the theory that a diagnostic can be demoted. It cannot: `helpBody` clips at the
body height and does not scroll, so a row past the fold is not a cheaper row, it is **no row** —
the same failure that put the posture explanation out of reach and split this panel into two
pages. All three room controls now share one row inside the budget, and
`TestHelpNamesEveryRoomControlAboveTheFold` pins both the fold and the controls, which until now
were asserted only by a comment.

#### `/read` and `/write`, and the two asymmetries in them

The third control built to this rule, and the one the rule was written for: §9.16's refusal of a
`/flow` write hop into a read-only room "names the flag that would change it", which is this
defect stated as a feature. The room knew what was wanted, knew what would grant it, and could
only say *quit and start over*.

**The confirmation is asymmetric, on purpose.** `/read` applies at once; `/write` asks `y`/`n`.
They are not the same act. Tightening takes authority away from four seats, and the worst case
of a stray `/read` is a turn re-run. Loosening hands editing and command authority to every seat
in the room — and in an `--auto` room, hands it with nothing left asking. `c` spends a keystroke
on its irreversible direction for exactly this reason, and anything that is not `y` cancels here
for `clearGateKey`'s reason: this gate interrupts nothing, so a key nobody meant to press must
not be able to arm the room.

**The card names which write you are getting.** Gated write and `--auto` write reach the same
badge-bearing posture by different routes and only one of them asks first, so the confirmation
says which — a card promising "claude asks before each change" in an `--auto` room is a promise
that room cannot keep. §4a.1 applied to a prompt rather than to a gauge.

**Neither direction is offered mid-turn**, which is `/cd`'s refusal rather than a house style.
Posture is argv, fixed at spawn, so seats already running hold the flags they were launched with
whatever the room now says. Landing the flip under them would put a read-only badge over a live
process still holding write flags — the disagreement between claim and process that the
per-step posture rule exists to forbid. Nothing is killed: `seatProcess` already respawns on a
posture mismatch under the same measured `--resume` composition it uses for `/cd`, so a `/read`
that is `/write`d back before anyone dispatches costs nothing at all.

**The badges are rebuilt, not just the flag.** `Sandbox` is computed once in `stateWith` from
`opts.Write`, so a posture that moved without `applyPosture`'s loop would leave four columns
advertising authority the room had just taken away — a displayed value no longer coming from
what is true. `TestPostureFlipRebuildsEveryBadge` asserts on the rendered badge rather than the
field. The `WRITES` and `gated` glosses were updated in the same change for the same reason:
both credited `--write` as the only way to reach them, which would send a reader looking for a
relaunch out of the glossary that explains the thing.

**Only the bare word is a command**, unlike `/cd` and `/trace`. Those take an argument, so
`/cd ` and `/trace ` are unmistakable; these take none, and both are words a person addresses a
room with. `/write a test for this` and `/read the design doc first` are ordinary briefs, and
intercepting them would swallow a turn and run a setting instead — worse than stealing a word,
because the user watches their brief vanish rather than being told it was a command.

Posture is still never restored from the saved room. `TestReattachDoesNotRestoreWritePosture` is
unchanged and still holds: "a posture that can arrive from a file is not one anyone typed" —
and a posture typed into the composer is typed.

#### `/seat`, and the control it is deliberately NOT

`--vendor`'s twin, taking the same argument for the same reason `/cd` takes `--cd`'s:
`/seat claude,codex` and `--vendor claude,codex` are one grammar, read through the same alias
table `@mentions` use. Two tables would let `/seat agy` work and `@agy` not.

**What it does not do is the design.** An unseated seat keeps its thread, keeps its process, and
keeps every id that would resume it. Only two things change: it is not drawn, and it is not
dispatched to. Killing the process to reclaim it was considered and rejected on the ruling that
a returning seat picks up its own thread where it left off:

- **The thread is the thing being protected.** A seat with a live process and no reported
  session id yet holds its whole conversation *in that process* (§9.8). Killing it there
  destroys a thread `seatHasThread` calls real — silently, on a command nobody reads as
  destructive. Dropping a thread is `c`'s job, and `c` asks first.
- **Nothing is being spent.** An unseated seat is never dispatched to, so an idle process costs
  a process and no quota. Trading a guaranteed-correct return for a resource nobody is short of
  is the wrong trade.

So reversibility is by construction rather than by a resume that could fail: `/seat all` puts
everyone back mid-conversation with nothing to go wrong. What it buys is what the fold-out
already buys an uninstalled seat — the **width** goes to the seats answering.

**Sitting out is a different control and already exists.** A seat nobody addresses does not
answer and is not billed; §9.19 renders a long absence as one line rather than ten. `/seat` is
for the seat you want off the *screen*, not merely quiet — which is why it was worth building
even though the quota problem it looks like it solves was already solved by the default route.

**It warns when it unseats the default route.** Silence goes to claude, so a room without claude
answers nothing until every brief is `@mentioned`. Dispatch already refuses a zero-seat route
per turn; saying it once at `/seat` time is the difference between a rule learned now and one
discovered on the next enter.

#### `a`, and the field that was nearly a landmine

The last control on the sweep, and the only one whose surface is **not** a room command. The
preference forms while a card is on screen — you decide to stop being asked at the eleventh
identical card, not at a shell prompt — so `a` sits beside `y` and `n`, where the question is.
It approves the card in front of you as well as the ones after it: an `a` that turned asking off
and left the current request pending would answer the general question and not the one on
screen.

**The queue is drained, not discarded.** A pending gate is a vendor *stopped* mid-call, and
`queueGate`'s own rule is that nothing may quietly drop a request — a dropped queue leaves
columns waiting forever with no card left to explain why. So every card behind the current one
is approved, and the notice says how many.

**It takes effect on the REQUEST, not on the next spawn.** A process already running keeps the
gate flags it was launched with, so it goes on sending requests after `a` is pressed. If those
queued, "stop asking" would keep asking until the turn ended — the promise broken at the moment
it was made. `queueGate` reads the room's state per request and answers immediately; the respawn
that drops the flags happens later, through `seatPosture`, on the next dispatch.

**It is not a one-way door.** `a` alone in view mode turns asking back on, and the footer carries
a permanent `a not asking` cell whenever the gate is off. Without that cell the room would sit
ungated with the way back documented nowhere on screen — the §9.17 defect rebuilt one key later.
The cell is not sheddable, for `t grid`'s reason: shedding it would drop the way out of a state
rather than a convenience.

**And the field is stored negated, which is the finding worth keeping.** The obvious shape is
`Asking bool` — whose zero value is *does not ask*. Every `State` built as a literal would have
been a silently ungated room, and the reason that was caught at all is that five existing gate
tests build their State by hand and went green while asserting nothing. A safety property whose
default is off is the wrong way round however carefully the constructor sets it. `GateOff bool`
read through `Asking()` makes the zero value the guarded room and turning the gate off an act.
`TestTheZeroStateAsks` pins it.

#### Nothing left on this list

Every control the §9.17 sweep found in violation now has an in-room surface: `--fresh`→`c`,
`--trace`→`/trace`, `--read`→`/read`/`/write`, `--vendor`→`/seat`, `--auto`→`a`. Every flag
stays, because each names a room you may genuinely want at the door; none of them is any longer
the only way to get one. `--brief` remains deliberately unfiled — it is first-turn context by
definition, so re-briefing is a separate feature rather than a missing surface for this one, and
folding it in without deciding that question on its own is still the thing not to do.

#### The sweep built the controls and left two sentences describing the old room

Every control landed and two strings went on describing council as it was before them. Neither
was cosmetic, and the shape they share is worth more than either fix.

**The refusal that motivated the whole sweep was the last thing to be fixed by it.** §9.16's
`/flow` write-hop block still said the room "was opened with `--read` — reopen it without that
flag", which is the §9.17 defect quoted verbatim *as the specification of the bug* and then left
running. The remedy is `/write`, it was two PRs old, and the notice sent a user with a half-typed
chain out of the room to fetch a flag they no longer needed. **A refusal is the surface least
likely to be re-read after the thing it refuses becomes possible**, because it is written once,
by the person who knows it is correct, and then only ever seen by someone who is already stuck.
It also now reports the *posture* rather than the launch argv, since `/read` reaches this state
too and "opened with `--read`" would be false as well as useless.

**And `/write`'s confirmation card was reading the flag instead of the room, which is the one
that could cost something.** The card exists to say which write you are getting — §4a.1 applied
to a prompt — and it chose its wording from `m.opts.Auto`. That field is only the *seed*:
`stateWith` copies it into `GateOff` at launch and `a` has moved it independently ever since.
So a room opened gated, told to stop asking, then `/read` → `/write`, offered a card promising
"claude asks before each change" with nothing left to ask. The user reads a promise of a
checkpoint and gets none — the failure this card was built to prevent, arriving through the
control that was supposed to prevent it. The `--auto` wording went with the field, because the
flag is no longer the only route into an ungated room and naming it asserts a cause that may not
be there.

**The rule, stated once so the next control inherits it: a flag that gains an in-room twin stops
being the answer to "what is the room doing" and becomes only the seed.** `dispatch.go` already
says this for the request path — "`m.st.Asking`, not `m.opts.Auto`: the flag only SEEDS this at
launch" — and the two misses were both places that had not heard. Every `opts.*` read on a
demoted control is now either a launch-time decision (`wantsGateHook`, the `savedPosture` record) or
a bug, and the way to tell them apart is to ask whether an in-room control can move the state
underneath it. Both fixes are pinned by tests rather than comments for the same reason: in every
room nobody typed a control into, the flag and the state agree, so the fixtures cannot tell them
apart and neither could review.

### 9.18 a strip said four fifths of a name it could have said whole in two letters

Since the default route stopped being everyone, the ordinary turn narrows the frame to one
seat and leaves the rest at `stripColumn` — fourteen cells. Every layout rule in §9.11 was
written for a column three times that, and at fourteen the room did the opposite of what
§9.11 ruled in both halves of the chrome at once. `Antigravity` rendered `Anti…`. The badge
row rendered `ro:tools  to` and `gated  fina`, and the overflow marker rendered `↑ 12 more
abov`.

The ruling those violate is §9.11's own: **a clipped seat name is still recognisable and a
clipped state word is not**, so identity yields first. A clipped state word that is also the
prefix of another word in the same vocabulary is worse than damage — it reads as a different
claim. `fina` is not a broken `final only`; it is a thing this room does not say.

So at strip width the room **sheds whole words** rather than cutting them, in a fixed order
that is a pure function of the width — which is what lets the frame sweep pin the whole
ladder instead of a golden per state:

- **Identity collapses to two letters.** `CC ✓ done`, `CX ○ idle`, `AG ⠋ streaming`. The tags
  are the HUD's own, character for character, because a reader who learned `CX` is Codex from
  the HUD's grid must not meet a second abbreviation in the room. They are *copied*, not
  imported: the seam between the two surfaces is the normalized session model and
  `internal/theme`'s numbers and nothing else, and a test asserts the strings by literal so
  the copy cannot drift in silence.
- **The clock goes, then the focus mark, then — for `unavailable` alone — the tag itself.**
  `8s` is the meta on that line and `turnRule` already ranks a label above the numbers that
  belong to it; every finished turn still carries its elapsed on its own separator. The
  arithmetic behind the rest: nine cells of `streaming` plus its mark leaves exactly three,
  which is a two-letter tag and the space after it, and `unavailable` at eleven leaves room
  for a mark or a tag but not both.
- **The badge row keeps the posture word and drops the cost and the granularity.** §9.2 is
  emphatic that a claim you cannot see is not a claim, so the safety word is the last thing on
  that row to go; the cost is a number the transcript records on every turn separator, and the
  granularity word exists to keep `waiting` from reading as a slow `streaming` — both of which
  are now the only thing on the header one row above. A badge too long for a strip would drop
  rather than clip, and stays readable at full length on the `?` postures page.
- **The overflow marker sheds `more`, then `above` / `below`.** The count is never traded: how
  much is hidden outranks which way to press, which outranks the filler between them.

**The focus mark is the one deliberate loss, and §9.12 is why it is affordable.** Two cells of
`▸ ` at fourteen is the difference between a tag and no tag for every nine-letter phase word.
§9.12 had already found that the glyph was the *weakest* part of that signal — "one `▸` in a
frame carrying four columns of prose" — and moved the load-bearing half onto weight and onto
the overflow marker's own words, `↑↓ scroll` against `tab to focus`. Both cost no cells and
both survive here. A strip is by construction the seat this turn was **not** addressed to, so
spending a seventh of its width marking it, at the price of its identity, inverts the priority
§9.11 set.

**What was declined.** Keeping the two-cell indent on a strip so the focused and unfocused
forms line up: it is chrome that exists to align a *name*, and at strip width there is no name
— the header starts at column zero and the badges start under it, so the strip reads as one
flush-left block rather than as a column with its margins still on. Shortening a phase word to
fit (`cancel` for `cancelled`, `stream` for `streaming`): a different word is a different
claim, and the vocabulary is shared with the help panel and the transcript. Giving the strip a
narrower vocabulary of its own — a second alphabet is exactly what §9.11's phase marks were
built to avoid.

### 9.19 sitting a turn out cost a line a turn, and wore the wrong mark doing it

Since the default route became one seat (#99), three columns sit out every ordinary turn. The
room said so, correctly, once per turn — and a quiet seat's transcript became a column of
identical warnings with the answer it actually gave scrolled off the top:

> `⚠ not addressed in turn 2` / `⚠ not addressed in turn 3` / `⚠ not addressed in turn 4` / …

Two things are wrong there and they are separate. One is the arithmetic. The other is the mark.

**Consecutive skips coalesce, at render time only.** A run of turns this seat was not part of
is one muted line — `not addressed in turns 2–7`, singular for a run of one. The run is the fact;
the turns inside it are not separately interesting, and a reader who wants one has the numbers.
The **data model is untouched**: nothing is written down for a turn a seat did not take, which
is §9.9's rule and the reason a transcript skips from 3 to 5 in the first place. The runs are
*derived* from the gaps between the turns that ARE recorded, so `[` and `]` still hop between
real turns (§9.20) and no record says anything it did not say before. A run broken by a turn
the seat took starts a new line in place, so the transcript still reads in order, and the LIVE
turn's skip keeps a line of its own — the run above it is history, that one is the turn the user
is deciding whether to act on.

A run is never claimed **before the oldest record**. History is capped at fifty and drops the
oldest first, so a column whose early turns were evicted would otherwise report "not addressed
in turns 1–29" about turns it may well have answered. Inventing an absence is the same error as
§9.9's inventing a conversation, run the other way.

Underneath the rendering bug was a data one, and it is the reason `Column.Skipped` exists at
all: the note is written on the LIVE column, and the live column is what `startTurn` files into
history. A seat that answered turn 1 and then sat out through 7 filed turn 1's record wearing
`not addressed in turn 7` — a turn that succeeded, with someone else's absence stapled under it.
A skip is not a fact about any turn this column recorded, so it does not travel with one.

**The mark is demoted to `○`.** `⚠` opens a note because a note reports something that did not
complete normally — a cancellation, a seat that is not there. Sitting a turn out is neither, and
it was a fair mark only while a narrow route was the exception. Drawn on the ordinary case it
is a warning the eye learns to skip, which is the same argument `ActDenied` makes for `SevWarn`
over `SevCrit` and the reattach card makes for no mark at all. `○` is what this room already
spends on *nothing has been asked of this seat*, which is exactly what a skipped turn is, said
about one turn instead of a session. It survives `--ascii` as `.` against the warning's `!`, so
the demotion is legible with colour switched off — and the word carries it first either way.

**An idle strip says where it left off.** At fourteen cells (§9.18) a backgrounded seat had a
header, a posture word and a run of skips, and the one thing a reader wants from it is which
turn it last took: `last: turn 8 ✓`, above the coalesced line. Every part of it is measured —
the number is the turn this column recorded, the mark is that turn's own phase — and a seat
with nothing behind it renders nothing rather than a placeholder, because absent is absent
(§4a.1) and this room does not draw `last: —`. Strip width only: a wide column already answers
the question with the turn separators themselves, and repeating it there would be the room
being loudest where it has the least to add.

**What was declined.** Recording a `TurnRecord` per skipped turn so the coalescing could read
one list: that is the room writing down a conversation that did not happen, and it would put
the skips in `[`/`]`'s path. Dropping the live skip into the coalesced run to save a row: the
run is history and that line is now, and a reader deciding whether to re-address a seat should
not have to read a range to find out. And giving the skip a mark of its own — `○` already means
this, and a second glyph for one meaning is the collision `glyphs.go` argues against.

### 9.20 the transcript is turn-wise and the only way through it was line-wise

§9.9 gave every column a real conversation and §9.10 and §9.12 made it reachable and
attributed. What none of the three changed is the *unit*. The scrollback moves a line at
a time, a page at a time, or all the way to either end — and the thing being scrolled
through is a list of turns, each one a labelled rule, a brief, and however much prose a
vendor felt like producing. So the room could tell you, honestly and precisely:

> `↑ 509 more above`

and nobody has ever counted lines. The number is measured, it is correct, and the only
question a reader actually has — *how far back is what I asked?* — is one it cannot
answer. `g` goes to the beginning and `G` goes to the end, which are the two positions
in a transcript that need no help finding.

**`[` and `]` walk the focused column one turn at a time.** They land the turn's
separator on the viewport's top row, which is the position that makes the brief and the
answer to it readable in one screen, and they take their offsets from `columnLines` —
the *same* pass that produced the lines — rather than recomputing where a turn starts
from `History`. A second derivation of "how tall is this turn at this width" would agree
with the first until the day a card grows a row, and would then disagree silently, since
both answers would still be plausible line numbers.

**Backwards is the audio player's rule, and it is the one people already have in their
hands.** `[` from the middle of a turn lands on *that* turn's head; only a second press
reaches the one before it. That falls out of the definition rather than being a special
case — "the last head strictly above where we are" produces both — and it means the key
answers "start this again" and "go back one" with the same press, in the order a reader
wants them.

**The two ends are deliberately not symmetric.** `[` at the first turn does nothing:
there is no turn 0, and a wrap would make a key pressed one time too many jump an entire
conversation. `]` past the last turn restores the tail and `Follow`, because what comes
after the last turn is the live output — that is `G`'s answer to the same question, not a
second one. Every landing goes through `applyScroll`, so `Follow` drops exactly as it does
for `↑`; a column pinned to the tail while displaying turn 3 would be lying about which of
the two it is doing.

**In compose they are the characters `[` and `]`.** No rule was added for that: §9.10
replaced the composer's list of exceptions with a test — a key that carries text *is*
text — and brackets carry text. This is the same contract that keeps `q` the letter q
there, and it is asserted rather than assumed, because a bracket that scrolled instead of
typing would corrupt a draft in a way the user would only find after pressing enter.

#### The marker states the coordinate, and §9.12's rules decide what it costs

The overflow marker is where the count lives, so it is where the coordinate belongs:
`↑ 25 more above  │  turn 3  │  ↑↓ scroll  │  f expand`. Three constraints from §9.12 and
§9.10 bound the whole design, and each one closed a question:

- **The count is never traded away**, in any form, at any width. It was §9.10's rule about
  the key hint and it is unchanged: how much is hidden outranks both how to reach it and
  what it is.
- **The coordinate sheds FIRST** — below even `f expand`. It says *where you are* while the
  hints say *what you can do about it*, and a marker that dropped a key to keep a coordinate
  would be §9.10's trade run backwards. Concretely it rides only on the widest hint form, so
  at the three-up tier's 37 cells the keys win and the coordinate is simply absent; `f`, the
  reading tier, is where it appears. That is the graceful degradation, not a gap in it.
- **A marker states the key for THIS column and never a neighbour's** — §9.12's rule, applied
  to a fact rather than a key. An unfocused column keeps `tab to focus`, the one thing a
  reader looking at it can act on, and gets no coordinate at all: putting the question in
  front of the answer is how §9.12's bug worked in the first place.

**Which turn it names is the part that could have lied.** The choice was between the topmost
hidden separator and *the turn the line immediately outside the fold belongs to*, and only
the second is honest when a turn is half on screen: a long reply running off the top is still
the turn you are reading, while the topmost hidden separator can be several screens further
back and answers a question nobody asked. So `turnAt` takes "the last turn that started at or
before this line", the two markers on one column name two *different* turns, and a column with
no turns at all — an unavailable card, a seat never asked anything — prints nothing rather
than `turn 0`, because a coordinate the room does not have is omitted and never invented
(§4a.1).

#### The footer learned to shed a cell instead of losing its way out

`[ ] turn` joins the view mode line immediately after the arrows, offered unconditionally for
§9.12's reason — the promise is about what the mode can do, not about how many turns a vendor
happens to have taken, and a footer cell that appeared at the first dispatch is chrome moving
while output arrives.

That exposed something the line had been getting away with. At the tabbed tier the six hints
fit **exactly**, and `statusLine`'s only answer to running out of width is to truncate — from
the right, which is where `? help` and `q quit` live. A motion key bought with the panel's
documented way out and the room's only quit key is precisely the trade §9.11's footer pass
existed to refuse. So a hint may now be marked *sheddable*: when the line does not fit, the
sheddable cells are dropped whole, newest-first, before the ellipsis is allowed to choose.
Exactly one hint carries the mark, and it is the one this section added — this is a rule about
which cell goes, not a licence to hide keys.

The help panel took it inside the hard 17-row budget by merging onto the row that already
holds the other jumps, the way §9.15 merged `y`/`Y` onto the gate's row: `g / G first turn or
newest; [ ] step one turn at a time`. "jump to the" paid for it — the line above already says
`scroll`, so the verb was never carrying anything.

**What was declined.** A turn coordinate on unfocused columns, which the width would have paid
for out of `tab to focus` (above). Numbering the hop in the notice line — "turn 3 of 7" is a
progress bar for a conversation, and the marker already says how much is left in the unit the
scroll keys use. And a `[`/`]` that moved *focus* between columns when a column has one turn:
two motions on one key, resolved by content, is the kind of binding that is only ever right for
the person who wrote it.

### 9.21 the room knew what the turn would cost and did not say

#99 restored the cheap default: silence goes to Claude alone, and the committee is
convened by typing `@all` or naming the seats. That settled *which* route is expensive and
made every expensive route explicit — and it left the footer stating the route in the same
words whether it reaches one vendor or four. `→ everyone` is accurate, and how much
`everyone` is depends on what is installed and on what `--vendor` left in the room, which
is exactly the part a user cannot read off the word.

**The routing cell states the bill when the draft would reach more than one seat.**
`→ everyone  (3 seats)`, `→ everyone but codex  (2 seats)`. The room already computes this
number — `dispatch` refuses a turn that reaches nobody by counting it — and the moment it
is worth knowing is the moment before `enter`, on the cell that is already answering the
same question.

- **One seat states no count.** `→ claude` names every seat it reaches in its own text; a
  cell that restates its neighbour is how this footer became the wall §9.11 had to take
  apart. From two upward the route names a *set*, and the size of a set is not in the word.
- **It counts seated ∩ addressed**, through the same `State.SeatsIn` the dispatch gate now
  calls. A route may name a vendor that is not installed or that `--vendor` left out; that
  seat is never spawned, so billing for it would quote a price for a turn that does not
  happen. `Model.seatedIn` became one line delegating to it rather than a second copy —
  a bill derived from different arithmetic than the dispatch is a bill for a different turn.
- **A refused route prices nothing.** `mixed @ and -@` addresses nobody, and the one thing
  that cell owes a reader mid-typing is what is wrong with the line they are still holding.
- **No colour, no cell, no new glyph.** The count is the *label* half of a hint and the
  route is the *key* half, which is the figure/ground split every other item on this line
  already makes (§9.11) — so the seat names keep their intensity and the number recedes to
  chrome for free. It is parenthesised because that is this room's existing grammar for a
  qualifier on the thing in front of it (`(+2 queued)`, `(turn 1 is blind)`), and because
  weight is invisible under `NO_COLOR`, where `→ codex, agy 2 seats` runs the price into
  the list it is pricing. The rebuttal tag moved to its own cell so the count could sit
  against the route it prices; it kept its intensity by keeping the key half of a hint.

#### The header carries the live turn's route

Once `enter` is pressed the composer clears and its routing cell resets to the *next*
draft's default, while the columns take anything from seconds to minutes. For that whole
window the room has nowhere at all that says where this turn went — and each column's
transcript does not record participation until it lands. So the header's turn cell carries
it while it is live: `turn 10 → everyone`, `turn 10 → codex, agy`, reverting to plain
`turn 10` when the last column finishes. **The route becomes history at that instant**, and
the transcript is where history goes; a header still naming it would be describing the past
in the one cell that describes the present.

`State.TurnRoute` is a **pointer**, and that is §4a.1's zero-vs-absent rule rather than a
style choice: `Route{}` is a real and extremely common route — it is what `@all` parses to
— so a value field could not tell "this turn went to everyone" from "no turn is running".
The same distinction `Column.CostUSD` draws with the same mechanism. It is set where the
turn actually starts rather than beside `FrameOwners`, because everything above that line
can still refuse the dispatch and a route on the header of a turn that never began would
report a spend that never happened. The two have opposite lifetimes on purpose: the
geometry outlives the turn so nothing reflows under a reader (§9.11), the route is retired
with it.

**It prints the route's own `label()`**, never a second vocabulary — what the header shows
is what would have to be typed to produce it — and the arrow is the literal one the
composer's cell uses rather than a `Glyphs` entry, so one fact cannot drift into two
spellings.

**A `/flow` hop states no route at all.** A hop is dispatched to exactly one named seat
(§9.16) and the cell immediately to its right already says which, so the route would be the
header saying the same thing twice — and the arrow, appended after the hop, would read as
pointing at it. This is the same rule as the shedding below rather than an exception to it.

#### Shedding order: a fact with a home elsewhere yields to facts that have none

The header already elides the workspace path from the left, and the new cell had to be
ranked against it. **The route sheds first** — before the path, before `3/4 seated`, before
`briefed`. The route is on screen in the composer a keystroke earlier and in the transcript
a moment later; the workspace is nowhere else at all, and it is the one fact here that
changes *what the agents can see*, which is why it has been on screen at all times since
this header was written. So the route is added only when it costs nothing that was already
there: the path keeps its cells if it had them, and where there was no room for a path
either way, the counts keep their gap.

**What was declined.** A dollar figure beside the seat count: cost is reported per seat per
turn where a vendor reports it, and multiplying a seat count by anything would be council
deriving a number and presenting it as read — the top item on this repo's rejected list
(§4a.1). Billing the *route's* vendors rather than the seated ones, which would have been
one line shorter and would have priced seats that are never spawned. And a count on the
one-seat case, which is a number whose only reading is "yes, one".

### 9.22 four answers to one question, and no way to read them as one

Council exists to put several vendors' answers side by side. Everything from §9.9 onward
built the surface that does it — a real transcript, per seat, scrollable, attributed,
navigable a turn at a time — and every one of those sections improved a **column**. The
room therefore had a comparison surface with no way to read a comparison. To see what four
seats made of one brief you scrolled Claude to turn 10, remembered it, tabbed, scrolled
Codex to turn 10, remembered that, and tabbed again; §9.20's `[` and `]` made each of those
one keystroke and did not change what the exercise was.

**The document already existed.** §9.15's `Y` assembles exactly this: the brief once at the
top, then every seat that took *this* turn, labelled, in seating order — and it was ruled,
argued and tested a release ago. What it could be read in was a clipboard. So this section
adds no content model at all; it renders the one `Y` already had, and the two now come from
the same call (`turnEntries`), which is the point rather than a tidiness. A page and a
paste that disagreed about who was in a turn would be two honest-looking documents with
nothing on screen to say which was the room's answer.

**`t` swaps the body between the by-seat grid and one turn's page.** One key and a toggle,
because the two are one transcript read two ways rather than two places — and it opens on
the turn the grid was already following, so the projection changes and the subject does
not.

#### What the page is, line by line, and why none of it is new

- **The turn's own rule**, carrying the number, where the turn went, and how long it took:
  `turn 10 ──────── → claude, codex  41s`. Same `labelRule` grammar, same shedding, meta
  before number, as every separator since §9.11.
- **The brief once**, under the composer's own `›` at full weight. Four copies is what a
  *grid* has to do — each seat's prompt is a fact about that seat (§9.9), since a turn can
  reach two seats and not a third — and it is precisely what a page must not.
- **Each participating seat under its own labelled rule**, name at weight, then its
  activity trace, then what it said, with §9.11's boundary strengths unchanged. The only
  thing that differs from a column is what the strongest boundary is *about*: a turn there,
  a seat here. That is what swapping the projection means.
- **A seat that sat the turn out does not appear.** §9.15's rule, for §9.15's reason: it
  still holds an older reply, and filing that under this turn's heading would be the room
  inventing a conversation — on the surface built to compare them, where it would be
  believed.
- **Failed and cancelled seats keep their note cards.** A turn's page shows what actually
  happened; the two turns anyone scrolls back for are the ones that went wrong.

**The route is read off participation, not off `State.TurnRoute`.** §9.21 retires the live
route the instant the last column lands, because the header describes the present. What
outlives it is the measurement — a `TurnRecord` exists for exactly the seats the brief
reached — so the page states who took the turn, through `Route.label()` so what is
displayed is still what would have to be typed to reproduce it. **The clock is the longest
seat's own elapsed**, because a turn is over when its slowest seat lands. A sum would be
the wall time of a room that dispatched serially and a mean is a duration no seat ever
took; both would be council deriving a number and printing it as read, which is the top
item on §4a.1's rejected list, and being in seconds does not exempt them. A turn still
running carries no turn-level clock at all — how long it took is not a fact yet — while
each seat's own rule carries its running one, from `State.Now`.

#### The two rules that shaped it more than the layout did

**Gate precedence, in both projections.** A pending approval renders on the page as
*chrome* — above the scroll, like `columnChrome` — and the argument is stronger here than
in the grid: a vendor is stopped, the live page follows its own tail, and a card inside the
body would be pushed off screen by the output of the very call it is asking about. It also
**names the seat**, which the grid's card never had to: there the card's position *is* the
seat, and one page has no position left to carry it. And `y`/`n` still answer the gate
before they mean anything else, because `key()` routes to `gateKey` first in either view.
That was already true; §9.15 made it asserted, and it is asserted again here — a keystroke
the user believes approved a write must never quietly copy text instead, since their next
move is to press it again.

**§7.1 rule 4 decided what the footer says.** A turn arriving while an older page is open
**never moves the view**: content jumping out from under a reader because a vendor finished
is the thing the bottom-anchor and the frozen-geometry rules exist to prevent. But a reader
on turn 10 of a room now on turn 11 is looking at something stale, and silence about that
is its own dishonesty — so the drift goes where a reader already looks to learn what the
keys mean. The mode word is `TURN 10/11`. §9.20 declined "turn 3 of 7" and this is not that
reversed: that was a progress bar offered in the *notice* line, describing a hop that had
already happened. This is §7.8's always-on mode label answering which projection is live,
which is the one thing the body has been ruled out of saying.

Pressing **enter** is the exception that proves the rule: dispatching from a page lands on
the turn just sent, because that move is the user's, not a vendor's, and a projection that
answered a new brief by staying on turn 7 would show an old conversation while spending
quota on a new one.

#### What it does not get, and the two keys that say so

There is **no column focus** on a page, so `tab` and `f` do nothing — and both are dropped
from the mode line rather than left promising something, which is §7.8's surprise pointing
the other way (§9.11's footer rule). The overflow markers follow: the focused-column form,
no `tab to focus` and no turn coordinate, since every line on a page belongs to the same
turn and the mode word already names it.

For the same reason **`y` and `Y` produce the same document here**. A per-seat `y` needs a
per-seat focus, and a projection whose whole unit is the turn deliberately has none — so
the narrower key takes the wider document rather than guessing which seat was meant. `y
yank` is named on this mode line and not on the grid's, because here the key takes the
thing in front of the reader, which is what makes it worth a cell.

`i` is the deliberate omission from that line. The six cells the page needs are its own
motions and its two ways out; the composer is one `t` away in a mode line that names it,
and it is the first row of the help panel. A footer short of width starts cutting into `?`
and `q` (§9.20), and that is the trade this line was designed never to make.

`t` joins the help panel by merging onto `f`'s row, inside the hard 17-row budget: `f gives
one column the full width; t gives one turn the whole room`. Not a saving — the same
category, the way §9.15 merged `y`/`Y` and §9.20 merged `[ ]` onto `g`/`G`. Both keys
answer one question, *how much of the room is the reading area*, and a reader looking for
either is looking for the other.

Everything else is reuse rather than resemblance. The page plans as **one column at the
full frame**, which is the tabs tier's own arithmetic, so the height budget, the 60-column
floor, the composer's growth and the collapsed-seat notice are identical in both
projections — a second layout path for a surface that *is* a column at full width would
just be a second place for the frame to tear. The scroll window, the overflow markers, the
tail and the clamp are §9.9's own argument applied once more: a page is a flat list of
lines, and this room already knows how to move through one. `[` and `]` keep the words they
have in the grid, at the same unit, so there is one motion to learn; `g` and `G` reach the
same two positions — the oldest turn still in memory and the live end — in the projection's
own unit. A turn the fifty-turn cap has evicted has no page, and says so rather than
drawing an empty one: "nobody answered" and "the room no longer remembers" are different
facts (§4a.1).

#### Declined

- **Cross-seat diff or agreement marks** — "these two agree", "this one dissents". A page
  puts the answers where a person can judge them; a mark would be council judging them,
  which no adapter sourced and no vendor reported (§4a.1). It is the same refusal as the
  "role" line §9.11 declined, with a harder consequence: a wrong agreement mark is one a
  reader would act on.
- **Persisting the projection.** `room.json` stays keys-only (ADR-008, ninth amendment) and
  which turn someone was looking at is not state the next session should inherit — §9.9's
  argument for not persisting the scrollback, one surface up.
- **Per-seat focus inside the page**, with `tab` cycling seats and `y` taking one of them.
  It would import the grid's whole focus apparatus — a marker, a weight, a hint on every
  marker — into a view whose entire claim is that the turn is the unit, and it would buy
  one thing the grid already does better. v1 lacks it deliberately, and `y`'s behaviour
  here is what falls out of that rather than a limitation worked around.

### 9.23 the frame dashed, and the outline whispered while its entries shouted

§9.11 through §9.22 spent the room's typographic budget on *columns* — a seat's name, its
state, its cards, its transcript — and every one of them was measured against what a reader
could find. What none of them looked at is the thing holding the columns apart. Read the
repository's own goldens as pictures rather than as assertions and the frame is the first
thing wrong with them.

**The rails were a property of the prose, not of the grid.** The `│` between two columns was
drawn per row, on the test *does any column have ink on this line*. That predicate exists for
a real reason: a tall idle window used to draw four bars straight down through an empty screen
to the footer, and Phase 2 removed them. But the room seats three transcripts of different
lengths beside each other, and §9.11 spends a blank row as a boundary in three separate
places — between a seat's chrome and its content, where the speaker changes, where the kind of
content changes. So the ordinary case is that all three columns are blank on the same line
several times per screen, and the frame blinked out on every one of them. `transcript.txt`
broke at rows 11 and 13, `skips-coalesced.txt` at 5, 10 and 13, `unavailable.txt` at 19. The
per-row rule solved the void and created a stutter, and a stutter is worse: an edge that dashes
in and out at irregular intervals reads as damage, and it read as damage at precisely the rows
where the design had placed air on purpose.

**A row carries a rail when some column has content on it, or when it is a lone blank row with
content above and below.** Two consecutive blanks end the band; the next word starts a new one.
A separator is *structural* — it says these are different columns — and that claim is as true on
a quiet row inside a conversation as on a loud one.

**One row is the whole threshold, and it is the room's own number rather than a tuned one.**
Every deliberate blank this surface draws is exactly one row, and §9.11 names all three of them.
A one-row gap is therefore a boundary the design placed *between two things it means to keep
together*, and drawing the rail through it is drawing what was meant. Two rows is nothing the
design asked for — the bottom-anchor pad, an idle room, a column that ran out of transcript long
before its neighbour did — and there a separator has nothing to separate.

The **literal** reading was tried first and rejected on the evidence: rails on every row from the
frame's first word to its last. It is a simpler sentence and it produces a worse room. An idle
frame at 120×60 has chrome at the top and `no turn dispatched yet.` anchored at the bottom, so
one span runs fifty-five rows of bar through nothing at all — exactly the shape Phase 2 removed,
re-derived from a nicer-sounding rule. Contiguity is worth having up to the point where it starts
asserting a grid over emptiness. `TestTheRailNeverDashes` and `TestRailsDoNotSpearAVoid` hold the
two ends apart, and the older `TestRailsStopThroughEmptyBody` is kept unchanged as the third
witness that this pass did not quietly trade one for the other.

**The turn page's outline takes the weight its entries already had.** §9.22 gave a page two levels
of heading — the turn's own rule at the top, then one labelled rule per participating seat — and
drew the parent wholly `Muted` while `seatRule` gave every child `Strong`. The room's hierarchy
upside down: the eye landed on four vendor names and had to hunt *upward* to find out which turn
it was reading, on the one surface whose entire claim is that the turn is the unit. The turn rule
now takes the same split every heading in this room takes — the label at weight, the rule and the
numbers hanging off its end receding — which is the figure/ground rule the column header and the
mode line already make, applied to a heading instead of to a key.

**The grid's copy of that line is deliberately untouched, and the asymmetry is the argument.**
Inside a column a turn separator sits under a seat name already at weight; there it is the child,
and muted is its correct rank. On a page it is the root. The same line changes weight because it
changed what it is the parent of, which is what swapping the projection means. `strongLabelRule`
is one implementation for both callers, extracted for `labelRule`'s own reason: the thing being
kept in step is the grammar, and a second copy would drift from it one narrow-terminal fix at a
time. Weight costs no cells and `PlainStyles` renders it as the identity function, so this half
moved no golden — `TestPageTurnRuleOutranksItsSeats` asserts it where colour is asserted (§9.5),
and asserts the grid's separator did *not* move in the same breath.

**One separator, spelled one way.** The collapsed-seat notice joined its remedy with `" │ "` —
one cell of air — while the room header, the mode line and the column gutters all use two. §9.11
argues that number from `--ascii`, where the rule glyph and the spinner's first frame collide at
one cell, and the notice was the single place in the product spelling the room's only separator a
second way. It now reads from `gutter`, so it cannot drift again.

**What was declined.** Making the rail's weight or hue say anything — it is chrome, and a frame
that varied would be competing with the content it exists to bound. Drawing the rail through the
bottom-anchor pad so every frame has one unbroken edge: that pad is the void, and it is the case
Phase 2 was written about. And a per-column rail extent, so a short column's gutter stops early:
the gutter belongs to the boundary between two columns rather than to either of them, and one of
the two ending sooner is not a fact about the line between them.

### 9.24 the middle of the grid breathed and its edges did not

§9.23 fixed the frame's continuity. This section is about the space inside it, and about a
number that was never chosen — it was assumed, in about eighteen places, and the two halves of
it had to agree by hand.

**The pad was a literal, and so was its twin.** The margin between the terminal's edge and
anything council draws was a bare `" "` in roughly ten builders — the header, the notice, the
column grid, the tab bar, the single-column and turn-page bodies, five row shapes in the
composer, the mode line, the help panel — with its arithmetic twin, a literal `2` meaning
*pad×2*, in eight more places that subtract it back out to get a usable width. Those two
families have to agree exactly. A builder that paints more than its arithmetic subtracts pushes
the row past the terminal edge and `fit` eats the overflow in silence, which is precisely the
off-by-one §9.11 found in the header's gap.

`framePad` names it and `framePadStr` derives the string from it, so the paint cannot drift
from the sums. **The extraction shipped as its own commit with the value still 1** — every
frame byte-identical, not one golden moved — because a refactor that also changes behaviour is
a refactor nobody can check. A `- 2` that is *not* the frame pad, like `labelRule`'s two cells
of air around its rule, is deliberately left as a literal; the constant is not a licence to
unify every 2 in the package.

The extraction turned out to be **incomplete on the first pass**, and the value change is what
found it: `header`'s `pathWidth` and its affordability test were still subtracting a literal 2.
At `framePad = 1` that is indistinguishable from correct, which is exactly why it survived —
the bug is invisible until the constant moves, and it surfaced as the header clipping `no brief`
to `no brie` at 68 columns. That is the argument for the constant restated as evidence.

**One to two, because a margin narrower than the gutters inside it is the wrong way round.**
The interior of the grid gave two cells each side of every rail; the frame's own edge gave one.
So the outermost boundary was the tightest thing on screen, the room read as crowded against
the terminal, and the middle read loose — the inverse of what a grid wants. The screenshot pass
that set `gutter` to 2 named that feeling exactly ("rigid / cramped") and fixed it in the one
place it happened to be looking. `framePad` is now the same two, for the same reason, and the
room has one number for *air between things* rather than two that disagree.

It costs two cells of total width, and one of them landed somewhere worth recording: **at 80
columns the view-mode footer came out one cell over.** This room sheds whole cells rather than
clipping words (§9.18), so `f expand` becomes the second rung of the shed ladder after `[ ]`.
`f` and not `tab`: `tab` is how a reader reaches the other seats at the tabbed tier, which is
the only tier this bites at, so shedding it would strand them on one column — while `f` is the
cell §9.11 already ranked lowest, on the argument that it expands a column to a width it
already has. Adding a second rung also made the shed *order* load-bearing for the first time,
so it is now stated — **shed order is list order** — rather than left to a backwards walk that
read as "newest first" and was not.

**stripColumn goes 14 → 18, from an arithmetic floor to a reading width.** Fourteen was
derived, and derived correctly: the widest phase word is nine cells, its mark costs two, and
the remaining three are exactly a two-letter vendor tag and its space (§9.18). That answers
what a strip's *header* cannot go below. It says nothing about the prose underneath, and prose
is most of what a strip draws.

At fourteen the prose shredded. §9.19's coalesced skip line — on most turns the **only** content
a backgrounded seat has — came out three rows deep as `○ not` / `addressed in` / `turn 4`, with
the phrase that carries the meaning split across two of them. `last: turn 8 ✓`, which §9.19
introduced with "room" as its stated goal, wrapped in a long room. A column whose every line
breaks mid-phrase is not narrow, it is unreadable, and the entire point of keeping these seats
on screen (§9.18) is that a reader takes them in at a glance.

Eighteen is the smallest width that puts `○ not addressed` and `last: turn 137 ✓` each on one
line. The header floor still holds — fourteen is still where the header itself would break, so
eighteen clears it by four and §9.18's shedding ladder is untouched. The four cells come out of
the primary column, and `weightedWidths` refuses the weighted split outright rather than ship a
primary under `minColumn`, so at a frame narrow enough for four cells to matter the room falls
back to equal columns instead of trading a readable strip for an unreadable seat.

**The change paid for itself in rows.** `skips-coalesced.txt` is the clearest reading: with each
block a row or two shorter, the same body height now holds seven more turns of transcript, and
the overflow marker went from `↑ 8 more above` to `↑ 1 more above`. Wider columns showing *more*
content is not the trade anyone expected from spending cells, and it is what happens when the
alternative was spending three rows to say four words.

**What was declined.** A width-dependent pad, so narrow terminals keep one cell and wide ones
get two: the tier ladder already varies what is *said* by width, and varying the frame's own
geometry as well would make two different rooms out of one resize. Trimming the footer by
clipping instead of shedding, which is the trade §9.11's whole footer pass exists to refuse.
And unifying every literal 2 in the package behind the new constant — `labelRule`'s air around
its rule is the same number for an unrelated reason, and tying them together would mean a
future change to one silently moving the other.

### 9.25 the panel that lists what the room can do was not listing it

Three of the four items here are the same defect wearing different clothes: a surface that
knew something and did not say it. The fourth is a surface that said something it did not know.

**The help panel clipped in silence, and it was the only place in the room that did.** Every
other surface spends a body row on `↓ N more below` when content does not fit, on the explicit
argument (§9.11, columnCell) that silent clipping is indistinguishable from there being nothing
more to say. The help panel is 24 rows on page one and 33 on page two against a hard budget of
17, so at the reference machine's own geometry it was dropping seven lines and sixteen — with
nothing on screen to say so, and dropping them mid-word: `…the containment, not a`. A panel
whose whole job is to enumerate what the room can do, quietly not enumerating it, is the
sharpest available version of §4a.1's rule.

**The marker's row is paid for, and the way out is pinned.** `?` is the only documented way back
out of this panel, and on both pages it sat at exactly row 17 of a 17-row budget — so a marker
taking the last row the ordinary way would have bought honesty with the exit, which is the trade
§9.11's footer pass and helpKeys' own budget comment both refuse by name. The exit is now
**chrome**, pinned to the last row the way `columnChrome` sits above a transcript, with the
marker inside the scroll below it. That makes the guarantee structural instead of a lucky row
count. The marker's own row is paid for the way this panel has always paid — by merging two
lines that were one category: `ctrl+j` and `esc`, the two compose keys that are not `enter`, one
extending the draft and one leaving it alone. Nothing was dropped to make room.

**The marker names no key, and neither does the mode line.** `↑↓` do nothing over the help panel
— `key()` routes no scroll to it — so the room was advertising an arrow that does literally
nothing in the mode a reader is in *when they went looking for what the keys do*. Wiring a help
scroll offset was the alternative and it was declined: it buys reachability for a page whose
overflow is a paragraph of prose, at the cost of new state, new key routing and a new §7.1 rule-4
surface, when the honest sentence — *there is more, and this terminal is not tall enough* — costs
one row and no mechanism. So the panel's mode line names only what works there (`?`, `i`, `q`),
which is §9.11's own footer rule applied to a mode it had not been applied to.

**The title got the room's grammar.** `council — one brief, several agents, side by side` was the
only heading in the product with no rule on it, while the column header, every turn separator and
every seat rule on a turn page all draw `labelRule`. A rule *under* the title is what one might
expect and it is not what this room does: §9.11 spent a whole item removing exactly that shape on
the finding that a heading followed by a horizontal rule says nothing the heading had not, and
ruled that a heading carries its own rule. So the title becomes a `labelRule` and costs no row —
which is what made it affordable against a budget with none to spare.

**The blank above `? close` did not happen, and that is recorded rather than fixed.** It is wedged
against the sentence before it and it should not be, but the exit sits at row 17 of 17 and a blank
there comes straight out of the legend the page exists for. §9.11's ranking settles it: a rule
outranks a blank, the title now carries one, and air is the boundary strength this panel can
afford to go without. If the budget ever loosens, that is the first row to spend.

**A seat's detail hung ten cells left of its own label.** The per-seat posture section put a seat's
name at column 15, under a badge legend at column 15, and then hung the seat's measured detail at
column 6 — the child left of its parent, reading as a new statement rather than as the reason for
the one above it. Every card in this room has had one grammar since §9.11 (a title at weight, its
body hanging under it) and this was the last place still drawing the shape that rule was written to
remove. The three hard-coded numbers that had to agree — 13 for the badge column, 15 for the
legend's continuation, 6 for the body — are now one `helpIndent`, checked against its own string
form at init, because a panel whose continuation rows drift a cell from its key column is invisible
in a diff and obvious on screen.

**The vendor tag is permanent, and the wide column is now the legend for the narrow one.** §9.18
introduced `CC` / `CX` / `AG` / `CU` as what identity degrades *to* when a strip has no room for a
name. Read as a whole product that is backwards: the abbreviation a reader has to know appeared
exactly where they had the least context to learn it, and vanished at every width where the room
had space to teach it. Drawn always, `CC Claude Code` at 37 cells is the sentence that makes
`CC ✓ done` at eighteen readable, and it is the same pairing the HUD's own grid already makes.

The tag is **chrome and the name is the anchor**, so the tag is muted while the name keeps the
weight that says which column the keys move — asserted, because a tag at the name's weight would
put a two-letter abbreviation in competition with the thing a reader is scanning for. It costs
three cells of the header row and nothing else, and §9.18's degradation order is unchanged: at
widths where the header must truncate, the spelled-out name goes and the two letters stay, which
is the strip's one-step collapse performed gradually.

**Turn pages and the collapsed-seat notice keep bare names**, and that boundary is the rule rather
than an omission: the tag earns its place where columns are *scanned*, and a turn page's seat rule
and a notice sentence are prose. `CX Codex (not installed)` inside a sentence is an abbreviation
introduced where nothing is being compared.

**One stray fact.** `unavailable.txt` drew `final only` under `⚠ Codex is not seated` — a claim
about how a vendor behaves *during* a turn, stated about a vendor that cannot take one. Codex was
not found on PATH; nothing about its streaming was measured. It was *plausible* — it is what the
binary would do if it were installed — which is precisely the class of claim §4a.1 puts at the top
of its rejected list. The badge row goes empty for an unavailable seat, and the cost cell with it
(a seat that never ran cost nothing). The row stays **reserved**, because §9.11's argument for
reserving it is about the grid's rows lining up and is untouched; what changes is that a reserved
row now holds nothing rather than something invented.

**What was declined.** Wiring `↑↓` to a help scroll offset (above). Giving the help panel its own
narrower vocabulary of markers — `↓ 7 more below` is the room's existing sentence and a second one
would be the second alphabet §9.11's phase marks were built to avoid. Dropping the badge row
entirely for an unavailable seat, which shears the grid for the sake of a row that costs nothing
to keep. And putting the tag on turn pages "for consistency": consistency across surfaces that are
doing different jobs is how a room ends up with an abbreviation in the middle of a sentence.

### 9.26 one rule glyph was doing four jobs, and the header band re-textured on every dispatch

§9.23 made the frame continuous and §9.24 made its margins breathe. What neither looked at is
that the room draws horizontal lines at **one weight**, and asks that one weight to be four
different things: the frame's own edge, a column header's leader, a turn separator inside a
transcript, a seat's heading on a turn page. Every one of them is `─`, so a reader scanning for
*where does the room end and the content start* gets the same ink as a reader scanning for
*where does turn 3 begin*. A grid with no outline is a grid you have to reconstruct from its
contents.

**Two weights, one distinction: outline against interior.** `RuleHeavy` is `━` (U+2501) and `=`
in the reduced set, and it is spent on **exactly three lines** — the two full-bleed rules that
close the frame above and below the reading area, and the turn separator at the top of a turn
page. Everything else keeps `─`. Three weights would be a hierarchy nobody can hold in their
head; the value of the second one is entirely in its scarcity, which is why the list is closed
and `TestOnlyTheFrameAndTheTurnPageDrawTheHeavyRule` asserts it as a *count* on the rendered
frame rather than as a property of the three call sites.

> **Amended 2026-08-09 (§9.44).** Two of those three lines are now one. The composer is a
> bordered box, so the lower full-bleed rule is gone and the frame's closed shape is the header
> rule plus the box — closure carried by corners rather than by ink. The scarcity argument here
> is unchanged and one line cheaper; what the heavy weight says is now *the chrome stops here and
> the seats begin*. The test still asserts a count, and the count is 1.

**Why the turn page's rule is the third.** It is the only line inside the frame that bounds a
whole document rather than a part of one. §9.23 gave it the *weight* of a root — the label at
full intensity while its seat rules recede — on the finding that the page's outline whispered
while its entries shouted; this gives it the *form* of one. The grid's copy of that same line is
untouched, for §9.23's own reason: there a turn separator sits inside a column already headed by
a seat name, so it is the child. The seat rules on a page and the help panel's title stay light
for the same test — a heading *inside* the outline that matched the outline would restate §9.23's
hierarchy defect one level down.

**The weight is a parameter, not a flag.** `labelRuleIn` takes the fill glyph and `labelRule`
passes `g.Rule`; a caller that wants the heavy rule has to name it at the call site. That is what
makes "exactly three lines" checkable by *reading* the three call sites rather than by grepping
for a bool, and it keeps one implementation of the grammar — a label, a rule, optional numbers,
two cells of air each side — which is `labelRule`'s own extraction argument.

**It is a character before it is a style.** `--ascii` gets `=`, not a fallback to `-`, so the
outline survives on exactly the terminals least able to infer it; `NO_COLOR` never touched it,
because weight of this kind is a glyph rather than an attribute. `=` is the one unclaimed mark
left in the reduced set — `-` is the light rule, the `Range` joiner and the first spinner frame,
`|` the separator, `>` the ellipsis, `]` focus, `!` the warning prefix, `^`/`v` the overflow
markers, `*` Act, `.` Idle, `:` the prompt, `_` the caret, `+`/`x`/`?` the outcome marks, `/`
and `\` the remaining spinner frames, and `#` the HUD's gauge fill. It is also the only
unclaimed character that reads as a *doubled* `-` rather than as a different symbol, which is
the one property a second rule weight needs.
`TestTheHeavyRuleHasAnUnclaimedASCIIPartner` enumerates that whole list so the next glyph cannot
be added without meeting it.

**The header leader stops depending on phase.** `headerUsesLeader` was false for an idle seat, on
an argument that was true at one rule weight: a long `────` between `Claude Code` and `○ idle`
was *filling* rather than separating, whitespace does that job for free, and a room with a single
rule weight cannot afford ink on nothing. With two weights the leader is no longer "the rule" —
it is the interior weight, and its claim on that row is *this name and this state belong to one
seat*, which is as true of an idle seat as of a streaming one.

The observable defect is the sharper half of the argument. A room where one seat is answering
drew the seats' header band as one continuous ruled line across part of the frame and blank
across the rest — **one row, two grammars** — and re-textured itself the moment a turn started
and again when it ended. §7.1 rule 4 keeps this room still by default, and a band that changes
shape on every dispatch is the loudest still-frame change on screen, spent on a fact the state
word beside it already states. The air the old comment wanted is not lost: `labelRule` keeps two
cells each side of its rule, which is the gap that keeps an ascii spinner (`-`) legible against
an ascii leader (`-`).

**Golden churn is the whole visible change, and it is two lines per frame plus one.** Every
frame's two rules, and every idle seat's header row. Nothing else moved — `PlainStyles` renders
both weights as themselves because they are characters, so unlike §9.23's weight half this pass
*is* visible in the goldens and had to be read frame by frame. The three test helpers that found
the frame by searching for a run of `─` (`fullWidthRule`, `frameBody`) now search for `━`, which
makes them stricter rather than merely different: a column header's leader can no longer be
mistaken for a frame edge at any width.

**What was declined.** A third weight, or a double rule (`═`), for the turn page — the page's
rule is already distinguished from its seat rules by its label, its position and its meta, and
the frame is the only thing it needs to *match*. Making the frame's rule brighter as well as
heavier: §9.23 declined to let the rails' hue mean anything on the argument that chrome competing
with content is the wrong trade, and an outline is chrome. And keeping the idle leader off "for
quiet": the quiet was bought by making the room's most stable row the one that changed most.

### 9.27 focus was a mark on one row, in a frame the reader had scrolled past

§9.12 fixed the focus signal by adding the `▸` and moving the load-bearing half onto the seat
name's *weight*. Both of those live on the column header — row one of a body that is twenty rows
tall — so a reader forty lines into a transcript, comparing two answers, had nothing on screen at
all telling them which column `↑↓` would move. The signal was correct and it was in the wrong
place: it described a column and was as tall as a line.

**The focused column's LEFT rail thickens.** The gutter cell immediately left of the focused
column draws `▌` (U+258C) instead of `│` — same cell, same width, one glyph heavier — for the
full height of the band. It is the only mark on this surface that is as tall as the thing it
describes, which is the whole reason it is worth a glyph. The `▸` and the name's weight stay:
word/glyph-first means two carriers on two rows, not one carrier moved.

**The leftmost column has no gutter, so the frame's left pad carries its mark.** `framePad` is
two cells since §9.24, and the mark takes cell one — which leaves exactly one cell of air between
it and the column, the closest the geometry gets to the gutter's two. Without this, position zero
would be the one seat the device could not mark, and a signal with a hole in it is a signal a
reader stops trusting.

**It rides §9.23's band exactly.** The thick rail spans the rows the thin one would and no
others, so focus cannot spear a void either — an idle 120×60 room still has a bare middle, and
`TestTheRailRidesTheSameBandTheThinOneDoes` asserts it against the same `bare > 0` test §9.23
wrote. Focus does not get its own answer to a question the frame already settled.

**Unfocused columns' prose steps back one contrast level.** `Dim` is `Text` + `Faint`, applied to
the *reading area* of a column the keys do not move: the vendor's reply, the §9.14 stand-in for a
reply that has not arrived, and the `no turn dispatched yet.` line. That is crush's
`Focused`/`Blurred` pair applied to prose rather than to a border, and it is the half of this
pass that costs no cell at all.

**The faint collapse is accepted, and here is the accounting.** Council has two intensities —
`Text` and `Muted` — so a demoted body renders identically to chrome, and inside an unfocused
column prose and chrome do arrive at one intensity. What is lost is the *second* signal, on a
column the reader is not reading: every distinction between them is carried by shape first (a
turn separator is a labelled rule, a trace entry opens `⚙`, a skip line `○`, a note `⚠`), which is
§7.1 rule 2 doing exactly the job it was written for. The alternative — a third intensity in
`internal/theme` — would spend a shared palette token, on a surface the statusline does not have,
for a distinction only the unread column needs.

**What the demotion does NOT reach, and each exclusion is a rule rather than a taste.**
- **The chrome above the body.** `columnCell` renders the header, the badge row and the gate card
  with the room's set and only the body with the seat's. A posture badge is a safety claim, and a
  claim that faded because the reader was looking at the next column is precisely the defect §9.2
  wrote the reserved badge row to prevent.
- **The prompt echo.** The user's own words stay `Strong` in every column. What a seat was *asked*
  is the thing a reader scrolls looking for (§9.9), and it is not the vendor's prose to demote.
- **Notes and cards.** A failure note, a reattach card, an unavailable card and the thread-cleared
  sentence under its rule all keep their own styles. This is the one place the ratified shape was
  **narrowed** during implementation: the thread-cleared sentence is prose in the reading area by
  position, but it is the body of a card in the room's grammar and it says what the *next* brief
  will do — an actionable claim about the seat, in the same category as the reattach card whose
  wording it shares. Leaving one of that pair full-contrast and demoting the other would be two
  spellings of one fact.

**The rail is a columns-tier device, and says so.** The tabs tier has one column on screen with a
tab bar above it already carrying `▸` and the selected tab's weight; a rail there would mark the
only thing there is. Expanded is the tabs tier by `tierFor`'s own rule, so it inherits that
answer rather than needing its own. A turn page is one reading area and has no unfocused seat to
demote.

**Under `NO_COLOR` and `--ascii` the whole distinction still lands**, and that is the test the
demotion had to pass to be allowed at all: `▌`/`[` in the gutter, `▸`/`]` before the name, and the
name's own weight all survive both, so a monochrome terminal loses the contrast step and keeps
every carrier that was doing the work. `[` is the ascii rail — `#`, the obvious candidate, is
refused for the reason `ActOK` refused it (it is the HUD's ascii gauge fill, and one product means
one vocabulary), and of what is left `[` is the squarest vertical stroke in the set, faces the
column it marks, and mirrors `]`, which is already this room's ascii focus mark. The `[` and `]`
in the mode line are key *names* in the footer's prose, never marks in the grid — the same slot
argument `Range`'s doc makes for the hyphen.

**Golden churn: the rail only.** `▌` is a character, so every columns-tier golden moved by exactly
one cell per railed row; `Dim` is an attribute rendered by `PlainStyles` as the identity function,
so it moved nothing. The whole diff was verified mechanically — every added line with the rail
glyph mapped back to a space is byte-identical to the line it replaced. One golden is **new**:
`focus-rail.txt` pins the focused column in the *middle* of the frame, the shape no pre-existing
golden reached because all of them render with focus at position zero.

**What was declined.** A rail on both sides of the focused column, which is a box and turns a
gutter shared between two seats into a property of one of them (§9.23's own last item). Colouring
the rail: chrome that competes with content is the trade §9.23 refused. Running the thick rail the
full body height so focus always has an unbroken edge: that is the void again. And demoting
`Muted` chrome a further step in unfocused columns, which would need the third intensity this
section just declined to buy.

### 9.28 the room's one hue exception, and exactly how far it goes

`internal/council` has said "adds no hues of its own" since §9.11, and the rule was right: a
dispatch room that invented a sixth colour drifts from the visual language the statusline and the
HUD share, so council spent WEIGHT (§9.11) and CONTRAST (§9.27) instead, both attributes rather
than hues. **This is the one ratified exception (San, 2026-08-07), and it is an exception to the
rule rather than a repeal of it.**

**The concept the other two surfaces do not have is the SEAT.** Everything council renders that
theme already has a token for — severity, identity, chrome — keeps that token. What has no token
is *which of four agents is speaking*, because `telltale statusline` and `telltale hud` have no
seats to distinguish. `seatHue` returns one ANSI index per vendor: claude `5` (magenta), codex `6`
(cyan — theme's identity hue, kept by the seat that already had it), agy `4` (blue), cursor `12`
(bright blue), and `theme.ColorIdentity` for anything else.

**Why it lives in `internal/council` and not in `internal/theme` — and the stdlib rule is NOT the
reason.** These are plain strings; they would compile in theme perfectly well, and citing ADR-002
here would send the next reader to fix the wrong thing. The reason is theme's *own* contract: one
hue, one meaning, across every surface that imports it. A per-vendor hue promoted to theme is a
token that means nothing on two of the three surfaces, which is how a shared palette stops being
shared.

**Why 4-bit indices.** theme.go's own argument, unchanged and reused rather than restated: the
terminal resolves an index against the scheme the user already chose, so the room looks native in
Windows Terminal's default and in a light scheme with no second palette and **no `isDark` fork**.
A hex triple would be council asserting a colour over the user's own.

**What is off limits, and it is a fence rather than a guideline.** The severity family — `1`/`2`/`3`
and their bright twins `9`/`10`/`11` — is the green/yellow/red ramp on every surface, and a seat
wearing red would read as a seat that failed, on a row where `✗ failed` is the thing beside it.
The chrome family — `0`/`7`/`8`/`15` — is the gauge track and the terminal's own fore/background.
That leaves 4, 5, 6, 12, 13, 14; this spends four of them, and `TestNoSeatHueIsASeverity` fails
the build if that stops being true.

**The honest weakness: 4 and 12 are one hue at two intensities.** agy and cursor are blue and
bright blue, which some terminal schemes render close together and a reader can miss. That is
acceptable **here and only here**, because §9.25 made the two-letter tags permanent — `AG` and
`CU` appear beside every seat name the room scans — so the hue is the second signal it is supposed
to be and the tag is carrying the distinction. If a fifth seat arrives wanting blue, the tag is
what still works and the hue is what has to be argued for.
`TestSeatHuesAreExhaustive` asserts the room seats exactly four vendors, so a fifth cannot be added
without somebody reading this paragraph.

**Three sites, and the list is closed.**
1. **A turn page's seat rules** (`seatRule`). The highest payoff by a distance: a page stacks every
   participating seat in one column, one block after another, so position answers *nothing* about
   who is speaking — which is the exact condition under which a hue earns its place.
2. **The tab bar.** `SeatStrong` selected, `SeatIdentity` unselected, replacing the wholly-muted
   unselected tab. That is a *promotion*, and the opposite of what §9.27 does to an unfocused
   column's prose, deliberately: prose in a column you are not reading is content you are not
   reading, while an unselected tab is a **destination**. It is the one row on that tier whose job
   is "here are the other seats, pick one", and a menu whose entries are faint makes you read it
   twice. The selected tab still outranks the rest by weight and by the `▸` in front of it, which
   is what survives NO_COLOR.
3. **The collapsed-seat notice**, names only. The `⚠` keeps `SevWarn`, the reason in parentheses
   and the remedy after the bar stay chrome, and nothing there gains weight — it is a sentence, and
   a sentence with four bold words in it is not one. §9.25's boundary is untouched: the two-letter
   *tag* stays out of prose, because an abbreviation introduced mid-sentence is one nobody can
   learn there. A hue is not an abbreviation — it costs no cell and teaches nothing new.

**Where it is deliberately NOT spent.**
- **Grid column headers.** Position already answers which seat this is, and four coloured names
  across one row is the circus row this rule exists to prevent — the room's newest signal spent on
  the one question the layout had already settled.
- **Phase marks and status words.** Severity owns those cells (§9.7).
- **Rules, leaders, badge rows and every other piece of chrome.** A posture badge is a safety claim
  (§9.2) and must not compete with a name for the eye.

**Constructed to be invisible to the goldens, rather than checked to be.** `SeatIdentity` and
`SeatStrong` are `Identity` and `Strong` *retinted*, through one `retint` helper that returns the
base style untouched when `Plain` is set. A second pair of literal constructors would have to
remember that and would forget it the first time one grew a second attribute. So **golden churn on
this pass is zero, and any golden diff on it is a bug** — which is also the whole verification
story, since colour is asserted where colour is asserted (§9.5) and never in a golden.

**What was declined.** A hue on the grid's column headers (above). Hue on the vendor tag as well
as the name, which doubles the ink for a distinction the name already carries. Truecolor, which
would override the user's scheme. And a fifth hue held in reserve for "the next vendor": a palette
entry with no seat behind it is a decision nobody has made, recorded as if somebody had.

### 9.29 the seats had positions and no way to address one

`tab` cycles focus, and at the columns tier that is fine: three seats, at most two presses. At
the **tabbed** tier — the narrow terminal, the one a laptop actually runs — one column is on
screen and reaching the fourth seat costs three presses, each of which redraws the whole frame
and shows you a seat you did not want. The room had four seats sitting in a fixed order, drawn in
that order on every surface, and no way to say *that one*.

**`1`–`4` focus the Nth VISIBLE seat, in seating order.** Positional, exactly like the columns
are. A room with two seats has keys 1 and 2 and nothing else: `3` there is a **no-op**, not a wrap
and not a clamp, because a key that quietly lands somewhere else is §7.8's surprise and a wrap
would make the number stop meaning the position it is printed at. In **compose** a digit is a
digit — the same contract `q`, `f`, `c` and `[` already keep, and it needs no second list: the
handler tests whether the key carries text, which is what makes it text in the composer.

**The number is drawn where the key acts.** `▸ 1 CC Claude Code ──────── ✓ done 8s` in the seat
header, and `▸ 1 CC Claude Code   2 CX Codex` on the tab bar — the two places a seat name heads a
reading area, which are the two places §9.25 already put the vendor tag for the same reason. The
number is **muted**, on the tag's own argument: it is chrome and the name is the anchor. It sits
in FRONT of the tag rather than after the name, because it is what a reader's eye runs down the
row of headers looking for, and because a number at the far right would sit beside the state word
where every other number on that line is a duration.

**It sheds last, and that is a new rung reasoned about rather than an appended default.** §9.18's
ladder drops the clock first, then the focus mark, then the tag. The number goes below all three:
`1 CC ⚠ unavailable` is exactly eighteen cells, so at `stripColumn` the full form fits every phase
word, and below that the tag goes before the number does. The argument is the one §9.18 itself
used — it shed the focus mark because the load-bearing half of that signal had moved somewhere
free, and it kept the tag because position alone was a weak identity. The number is not a second
spelling of anything: it is the key that reaches this seat, at the width where reaching seats is
hardest, and **a key nobody can see is a key nobody presses** (§9.10, which is the whole reason
this room names keys on its overflow markers at all).

**The footer names `1-N`, not `1-4`.** The range is however many seats are on screen. A
three-seat room naming a `4` would promise a key that does nothing — §7.8's surprise, which this
line already refuses in the other direction for `tab` and `f`. It is the **third rung of the shed
ladder**, appended after `[ ]` and `f` so §9.24's order is untouched, and it is the last of the
three to go: shedding only bites at the tabbed tier, which is precisely where the number is worth
most. `[ ]` sheds first because `g` and `G` still reach the ends of the transcript; nothing else
reaches seat 4 in one keystroke. `? help` and `q quit` remain unsheddable, asserted.

**A room with ONE seat on screen has no numbers at all**, and that is §9.11's rule applied to a
third key rather than a special case: `f` and `tab` are dropped there because they address a
choice that does not exist, and a number labelling the only column there is spends a cell on the
same nothing. `State.SeatNumber` and the footer's cell run off the same predicate, so the key's
label and its advertisement appear and vanish together — a footer naming a key the header did not
would be one surprise split across two rows.

**Renumbering, and the still-by-default wrinkle it is.** Because the number is a position, a seat
folding out **renumbers** every seat after it: with Claude uninstalled, Codex is seat 1. That is a
label changing under a reader, which §7.1 rule 4 does not hand out lightly — and it is bounded by
*when* it can happen. A seat collapses, or `--vendor`/`/seat` reseats the room, and both already
reflow the entire frame: the column widths change, the notice row appears, the grid is visibly a
different room. There is no path where the numbers move on a frame that was otherwise going to
look the same, and in particular none mid-turn. The help panel says "by position" rather than
implying a seat owns its number.

**The help panel merged, not grown.** `tab / 1-4` on the row that already named `tab`, because the
budget is hard at 17 rows and these are one question asked two ways — step to the next seat, or go
straight to one. "move" paid for the characters. The `?` row, the panel's only documented way out,
is exactly where it was, and the `↓ 5 more below` marker's count is unchanged.

**What was declined.** `alt+1`–`4`, so digits could stay digits in both modes: it buys nothing —
compose already routes text keys to the draft — at the price of a chord nobody discovers and that
several terminals eat. Numbers on a turn page, which has one reading area and no focus to move.
Stable per-vendor numbers that never renumber, which would leave gaps (`1`, `3`, `4` on screen)
and make the printed number disagree with the position it is printed at — the number would then
be an identity, and identity is what the tag and the hue are for. And a fifth key for a fifth
seat: `1-N` already says how many there are.

### 9.30 one question, asked once, instead of four times across the comparison surface

Council exists to put several answers side by side, and §9.22 built the page that reads a turn
as one document. What neither of them fixed is what the **grid** does with the question itself.
§9.9 ruled that the echoed brief is a fact about the COLUMN — a turn can reach two seats and not
a third, seats skip turns, and a transcript that filled the gaps would be the room inventing a
conversation — so every addressed column echoes it. On a one-seat route, which is the ordinary
turn since the default stopped being everyone, that is exactly right. On a committee route it is
the same paragraph two, three or four times across the top of the reading area, each copy pushing
the answer it belongs to a row further down, with "+ the other seats' last answers were quoted to
this one" repeated underneath every one of them on a rebuttal turn. The surface built to compare
four answers spent its widest rows agreeing with itself about the question.

**So the live turn's brief is drawn once, full width, as a band under the room chrome, and the
addressed columns stop echoing it while it is up.** Nothing new reaches `State`, nothing is
stored, and no column records anything different: this is a rendering rule over the same echo
§9.9 already holds, sanitized and deliberately unredacted for §9.9's own reason — it is the user's
own typing shown back to the user, and covering it would hide a secret from the one person who
already has it while doing nothing about the copy just sent to three vendors.

**History is untouched, and that is the boundary rather than a scope limit.** A finished turn's
echo stays inside the column that took it, because §9.9's argument is about a *record*: turn 4
is filed on the two seats it reached and absent from the one it did not, and each column's
transcript is that seat's own conversation read top to bottom. The band speaks for one turn — the
live one — and it identifies that turn by number (`Column.TurnN == State.Turn`), never by "this
column has a prompt". A column's prompt block outlives its turn: a seat that answered turn 4 and
sat out 5 and 6 is still displaying turn 4's brief as its current block, and that is its own
conversation, not the live turn.

**It appears at dispatch and retires at the next one.** Both are keystrokes, and §7.1 rule 4's
still-by-default frame is about what moves *without* one. The retirement moment is deliberately
the push to history and **not** the instant the last column lands: §9.21 retires the live route
there because the header describes the present, but that landing is a vendor finishing, and a band
that vanished on it — restoring three per-column echoes and reflowing every column — would be
precisely the mid-turn layout jump the rule forbids. Tying the band's life to the same block the
per-column echo already had means there is one reflow point, it is the user's own enter, and the
frame either side of it is one the reader asked for.

**Two seats is the threshold.** One echo on screen is not duplication, and hoisting it would move
the user's words away from the answer to them and buy a row of chrome for nothing. The band is
therefore a columns-tier device: the tabs tier draws one column at a time, `f` resolves to that
tier, and a one-seat room has nothing to compare — in all three the column keeps its own echo. A
turn page is excluded for the opposite reason: it already prints the brief once, which is half of
what it is for. The help panel is excluded because it replaces the column area outright.

**The anatomy is §9.9's echo hoisted, and §9.11's middle boundary under it.** The composer's own
`›` at full weight, because the glyph carries "you said this" before the colour does and the user's
words are the anchor a reader navigates by; the rebuttal notice once, muted, underneath, only when
true, in the same sentence the column prints (one constant, not two spellings); then a **blank
row**. Of the three boundary strengths this room ranks — a labelled rule where the turn changes, a
blank where the speaker changes, a blank where the kind of content changes — the band's is the
second: the user stops and the seats start. A rule was refused. The frame's own full-bleed heavy
rule sits two rows above it, and a second horizontal line under that would rebuild §9.11's "one
rule per column instead of two three rows apart" at the room's own scale, with §9.26's
heavy/light distinction blurred as well. The band also states **no route**: the header already
carries `turn 10 → everyone` on the cell that names the turn, and repeating it would be the second
copy this whole section exists to delete. What each column keeps is its own turn separator, which
is one line saying which turn the lines under it belong to rather than the same paragraph again.

**Four rows, and the fourth is the marker.** A brief worth sending to three agents can be a
paragraph — the composer grows to six rows for that reason — and a band as tall as the draft would
eat the reading area it was written to protect. So the band spends at most four rows on the brief,
and when it needs more the fourth row is a **truncation marker** rather than a fourth row of text:
how many rows are missing, and that the turn page has the brief whole. Silent clipping is the
ambiguity §4a.1 forbids, and it is worse here than anywhere else on screen — a reader cannot tell
their own question from a truncated copy of it. The `t` that opens that page is named on the
marker in **view mode only**, because `t` is the letter t while composing and a marker advertising
it there would promise a keystroke that does something else (§7.8, scrollHint's rule for `f`). The
count and the destination survive in both modes; only the keystroke sheds.

**The band's rows are room chrome, spent where the notice line is spent.** `resolveLayoutIn`
settles the tier first — §9.5's ordering, unchanged, and the band depends on the tier so it could
not be spent any earlier — then header, footer chrome, tab bar, collapsed-seat notice, **band**,
then the composer, which still yields before the body. The band is budgeted before the composer
and tested against the composer's *floor* rather than its current height, deliberately: a band
that retired because the draft grew a row would be a layout jump on a keystroke mid-turn, and it
would jump back on backspace.

**Below a floor the band yields ENTIRELY, and the fallback is a pure function of height.** If
spending the band would leave the columns fewer than eight body rows, it is not spent at all and
the columns echo the brief themselves — the pre-band frame, byte for byte. Eight is measured from
what a column draws before a word of the reply: three rows of `columnChrome` (name, posture claim,
one blank) and the live turn's own separator, leaving four rows of answer. All-or-nothing rather
than shedding a row or two off the band, because a half-band is the worst of both: a cut question
above columns that no longer say what they were asked. One number decides it — `Layout.Band` — and
the renderer and the columns both read that one number, so a band without suppression (the brief
three times *and* at the top) and suppression without a band (the brief nowhere on screen at all,
which is a §4a.1 failure with the user's own words as the missing content) are not states this
code can reach.

**Scroll detaches from the band, on purpose.** A column scrolled back into history draws its
history under the band exactly as it always did, and the band stays — including when *every*
addressed column has been scrolled away. It describes the live turn, which is a fact about the
room, not about any viewport; a reader who went looking for an older answer is still in the turn
they dispatched, and the question that produced it does not stop being true because they scrolled.

**What was declined.** Hoisting past turns' briefs the same way, which would flatten §9.9's
per-column record into a room-level one and lose which seats a turn actually reached. Keeping a
one-line stub in each column to mark where the brief would have been: it costs the row the band
was spent to save, once per column, and the turn separator already marks that boundary. Shedding
the band down to one row on a short terminal, covered above. And giving the band a rule glyph or a
hue of its own — council adds no hues, and the boundary vocabulary this room already has is what a
reader has already learned to read.

### 9.31 a word the room did not know was billed to three vendors

**The rule: a draft that opens with `/` and names no room command is refused, not dispatched.**
Refusing is free — nothing spawns, nothing is billed, the draft stays in the composer — and the
alternative is not free at all.

**The field report.** Turn 53 of a real room: `/unseat codex` was typed. There was no `/unseat`;
§9.17 shipped `/seat <list>` and nothing else. `roomcmd.go` recognised no command, so the draft
fell through **as a brief**, and the committee was billed to discuss the string `/unseat codex`
until the user cancelled the turn. Nothing malfunctioned. Every line of that was the documented
behaviour: "only a draft that IS a command is intercepted; anything else, including text that
merely starts with a slash, dispatches to the vendors as typed."

That fall-through was the right call for the *vocabulary* question and the wrong call for the
*typo* question, and the two had never been separated. The vocabulary rule exists so the room does
not steal words out of the conversation — the argument that kept `/clear` out of `roomcmd.go`,
since `/clear` is a real Claude Code command a person means for a vendor. It says what must not be
**executed**. It says nothing about what should happen to a draft that executes nothing, and
dispatching was only ever the default that was already sitting there.

**A leading slash is almost never prose.** It is a command the room does not have, a command a
*vendor* has, or a typo for one of ours. Against that, dispatching costs a turn on every seated
vendor — on the scarce independence pool as readily as on the cheap lane — for a line the user will
retype in five seconds. `addressesRoom` is the whole test: a slash in **column one**.

#### The escape hatch is one space, and it had to be

A brief that legitimately opens with a slash is a real thing to type — a POSIX path, a regex,
`/etc/hosts is wrong`. Prefixing one space sends it, unmodified, to the vendors.

It is the cheapest honest escape available, and it is honest because nothing between the composer
and the spawn trims it. `sanitizeKeepingSpace` deliberately does not trim ("trimming would make the
string on screen disagree with the string about to be dispatched", §7.14's rule applied to the
composer), `ParseRoute` returns an unconsumed draft unchanged, and `dispatch` echoes what it sends.
The space the user typed is the space the seat receives, and
`TestALeadingSpaceSendsASlashBriefToTheVendors` asserts that at the seat rather than at the parser.

**The refusal has to say so, in few words.** §9.17's own defect shape is a refusal whose remedy is
undiscoverable — the `/flow` write-hop notice that went on naming a flag for two releases after
`/write` made it wrong. So the notice carries three clauses **in this order**: what failed, how to
send it anyway, then the vocabulary.

> `no room command /unseet — a leading space dispatches it · /cd /flow /read /seat /trace /unseat /write`

The order is load-bearing. This notice replaces the entire hint stack on the mode line, which
truncates from the *right*, so the clause a narrow room loses has to be the one a reader can get
elsewhere. `?` lists the room controls; nothing else on screen teaches the space. The quoted word is
capped (`unknownVerbEcho`) for the same reason — a pasted 200-character path is one word, and an
uncapped echo would push both the remedy and the vocabulary off the end, leaving a refusal that
names only the mistake.

**The vocabulary in that notice is walked, never written twice.** `roomVerbs` is the one table; the
notice reads it and `TestTheRefusalListsTheLiveCommandTable` walks it. A hardcoded list in either
place is the copy that goes stale on the next command — and this feature would have been its first
victim, shipping `/unseat` with a refusal that did not mention `/unseat`.

**What this does to the bare-word rule, which is the one deliberate consequence.** §9.17 made
`/read` and `/write` bare-only so that "/read the design doc first" could not silently swallow a
turn and run a setting. That rule is untouched: those drafts still do not reach `postureCommand`.
What changes is where they go instead — refused with the space named, rather than billed. Both
halves are now one test (`TestBareWordOnly`), because either alone is a defect: a "/read the design
doc" that ran the setting, and a "/read the design doc" that cost three seats a turn.

**`/flow` came along with it.** `dispatch.go` matched `strings.HasPrefix(TrimSpace(draft), "/flow")`
— any draft whose first non-space characters were those five letters. So `/flowchart the auth path`
was an orchestration, and, worse, `" /flow/gate.log is the file I mean"` would have been swallowed
*after* being escaped, making the hatch a lie for exactly one prefix. An escape hatch with an
invisible exception is not one. `isFlowCommand` applies the room's single vocabulary rule there too.

#### `/unseat <list>`: `/seat` spelled the other way round

The typo that started this was reaching for a control that should have existed. `/seat` names who
**stays**; `/unseat` names who **leaves**, and it is the argument `-@` makes one control up: the
correction a user reaches for mid-session is "not that seat" — one vendor is answering badly or
expensively and the other three are fine — and making them retype the complement is arithmetic done
at the keyboard, on the one line where getting it wrong quietly reseats the room around seats they
did not mean.

It is `parseSeatList`, literally: same aliases, same `@` tolerance, same trailing punctuation, same
dedupe. A second list parser is how `/seat agy` would work and `/unseat agy` would not.

Everything §9.17 ruled for `/seat` holds unchanged, and mostly by sharing the code rather than by
sharing the intention:

- **It kills nothing.** An unseated seat keeps its thread, its process and every id that would
  resume it. `/seat all` puts it back mid-conversation, with no resume to fail.
- **It refuses mid-turn.** The roster is dispatch state — `frameOwnersFor` decided this turn's grid
  — so reseating under a live turn would redraw the room around columns that are mid-answer.
- **It warns when it removes the default route**, because unaddressed briefs go to claude. The
  warning lives in `applySeats`, shared with `/seat`, precisely so the subtractive spelling cannot
  be the one that says nothing.
- **Bare `/unseat` reports**, the way bare `/cd`, `/trace` and `/seat` do: a command that half-asks
  a question answers it rather than doing something.

Three refusals are its own. **The last seat**: a room with no seats can answer nothing, so the
subtraction that would empty it is refused in `/seat`'s words for `/seat`'s reason. **`/unseat
all`**: a sentence someone will type, answered as the empty room it names rather than left to
`parseSeatList`, whose honest report would be "no seat called all" — a spelling complaint about a
word the room understands perfectly well. **A seat that is not in the room**: distinguished from a
typo, and both change nothing, on `/seat`'s argument that a command which quietly did less than it
was asked is discovered several turns later as a seat still answering.

**Membership is what the room SHOWS, not what it can drive**, and that line took a CI failure to
find. `/seat cursor` *forces* an uninstalled seat on screen — "a user who asked for it is owed the
card explaining why it is not there" — so that seat is in the room in every sense a subtraction
cares about, and the first spelling, which tested membership with `seatsVendor`, could not remove
the one card a user is most likely to want gone. On a machine where nothing is installed it could
not remove anything at all: every `/unseat` was answered "not in the room" and the roster never
moved. Local runs passed because the developer's machine has four vendors on `PATH`; CI, which has
none, is the one that reads the rule as written.

**So the two questions are separated, and only the second guards the last seat.** *Is it in the
room?* is `shows`. *Can it answer?* is `Avail == AvailInstalled`, and the refusal built on it is
**conditional on the room having had one**: a room with nothing installed could not answer before
this was typed either, and refusing there would blame `/unseat` for a state it did not cause. An
empty roster is refused unconditionally, because that is `/seat`'s own "at least one seat" reached
by subtraction.

#### The focus bug underneath it

`/seat` has been able to unseat the **focused** column since §9.17, and nothing moved focus when it
did. `State.Focus` indexes `Columns`, so it went on pointing at a seat the grid no longer draws: the
focus mark vanished from the room, and `f`, the scroll keys and `y` went on addressing the hidden
column. Keys that still work over a transcript nobody can see are worse than keys that stop, because
nothing on screen says anything is wrong.

`stateWith` already does this once, at launch — "focus lands on a column that is actually drawn" —
and the fix is that same rule applied wherever the roster moves (`rehomeFocus`), not a rule of
`/unseat`'s own. It is called from `applySeats`, so `/seat` gets it too; a helper that fixed only
the new command would have left the older one holding the bug that made the new one worth writing a
test for.

#### The help row

`/unseat` merged onto the row `/seat` already holds, as `/seat /unseat <list>`. The panel's budget is
hard — 17 rows to the `?` line on a 24-row terminal — and `helpBody` clips without scrolling, so a
control named past the fold is not a demoted control, it is an absent one (§9.20). The merge is also
the honest shape rather than only a saving: the two take one argument in one vocabulary and differ
only in direction, so a reader who finds either has found both. "times" paid for the width — the row
is a list of controls, and `/trace <file>` is unambiguous without the verb.

### 9.32 the room remembered where it was and forgot who was in it

**The ruling, San's, 2026-08-08, and it is the line every field in `room.json` is now cut
along:**

> `room.json` records the room's **SHAPE** — workspace and roster — and restores it.
> **AUTHORITY** — write posture, gate cadence — is never restored; it must be typed. The saved
> posture field exists only for the reattach-mismatch notice, so it records the room **as it
> stood** (live write + live asking, both sides at once so the notice can't fire spuriously).

Two defects, one on each side of that line, and they are opposite failures of the same file.

**Shape was half-saved.** `/cd` moves the room and the file follows; `/seat` moves the room and
the file never heard. So `/seat claude,agy,cursor` — evicting a Codex that was dark on quota —
died with a restart, and Codex walked back into the room and started billing the next
unaddressed turn. That is the expensive-default defect returning through a reboot, on a control
built specifically to kill it, and the room said nothing while it happened: the header drew four
seats and the user had typed three.

**Authority was half-recorded.** `savedPosture(m.st.Write, m.opts.Auto)` reads one live field
and one launch flag. Press `a` in a gated write room and the file goes on saying `write-gated`
about a room with nothing left asking. §9.17's own closing rule is the one that was broken —
*a flag with an in-room twin stops being the answer to "what is the room doing" and becomes
only the seed* — and §9.17 named this exact call site as a legitimate launch-time read. **This
section amends that.** `savedPosture` is not a launch-time decision; it is a description of a
running room, and it is the third miss of the same shape after `/write`'s confirmation card and
the request path.

#### The roster is keys, not content — which is why it may be saved at all

ADR-008's ninth amendment ratified council writing exactly one file and ruled what may be in
it: **keys, not content.** A roster passes that test rather than being excused from it. It is
at most four vendor ids out of the closed set `addressableVendors()` — the same words `--vendor`
takes on the command line and the footer prints on every frame. It says *who was in the room*
and not one syllable of what was said in it. If the file leaked, the roster discloses which of
four public CLI tools the user had on screen, which is strictly less than the workspace path
sitting beside it already discloses.

`TestTheSavedRoomHoldsKeysAndNeverContent` is the guard, and it fails closed by pinning the
exact key set — so adding `seats` had to be a deliberate act that broke a test and got read.
It now also asserts the roster's *content* is names, so a field added to `Seats` later that
carried a note or a reason reaches this file through the same tag and gets caught there.

#### Saved when it moves, not at the next dispatch

`c`'s rule, in `clearSeat`'s own words: the room file is what a reattach reads, so a change held
only in memory is undone by quitting — the user ends a thread and finds it waiting for them.
A roster is the other thing a user deliberately takes out of the room, and it earns the same
treatment for the same reason.

**The save is an observation on `roomCommand`, not a call inside `seatCommand`.** `c` could put
its `saveRoom` inside itself because there is exactly one way to clear a seat. The roster has
`/seat`, has `/unseat`, and will have whatever narrows it next — and a save per command is a
save the third one forgets. So `roomCommand` snapshots the roster, runs the command, and saves
if it moved. Any command reachable from there inherits persistence without knowing the wrapper
exists, which is what let `/unseat` be written in a parallel lane and compose with this without
either side being told about the other.

Two consequences worth stating rather than discovering:

- **Only a change writes.** Bare `/seat`, a typo, and a `/seat` refused mid-turn all report
  without reseating. Rewriting the file on those would refresh `saved_at` — the age a reattach
  shows — for a room that answered a question and did nothing.
- **A room that has never dispatched still writes nothing.** `saveRoom` returns at turn 0 and
  `readRoom` refuses a turn-0 file, both unchanged. A `/seat` typed before the first brief rides
  out on that brief's own save, which is the only save there was ever going to be.

#### `--vendor` overrides the saved roster, and then rewrites it

`--cd`'s rule and `--cd`'s reasoning: **an explicit launch control someone typed today outranks
a file from yesterday.** `seatsFor` mirrors `Run`'s workspace switch line for line, down to
sharing the same `re.Active() && !re.Offered` — a room `--fresh` declined restores neither half
of the shape.

The rewrite needs no code, and that is worth saying because it reads like a missing branch:
`stateWith` copies the answer into `State.Seats` and `saveRoom` writes `m.st.Seats`, so the
first completed turn records the room the user actually got. The same one line is what makes
`/seat` persist. Leaving it out would be worse than not overriding at all — the file would go
on describing a room that is not on screen, and the *next* launch would restore it.

**Restoring is unconditional on the roster's own content**, including the zero value: the
default room saved as the default room. A saved roster that could only ever *widen* would be a
`/seat` you could not undo by quitting.

**Back-compat is the absence of a field, and it is exact.** A `room.json` written before this
section has no `seats` key; that decodes to the zero `Seats`; the zero `Seats` is the full
detected table. So an old file opens the room it has always opened, and no version bump is owed
— `roomVersion` is bumped when a field *changes meaning*, and additive fields are handled by
the zero value, which is the rule `roomVersion`'s own comment already states. Pinned by a
hand-written v2 fixture rather than a round-trip, since a file this build saved would carry the
field and prove nothing.

**An unknown seat name is dropped, not obeyed.** The roster is the one restored field whose
value is a *name*, so it is the one a hand-edit or a downgrade can fill with a word this build
has no seat for. Obeying it would seat nobody, fall through the everything-collapsed fallback in
`VisibleColumns`, and hand the user the default room while the file claimed a narrowed one —
§4a.1's collapse in the surface this section exists to make trustworthy. Dropped rather than
refused, because a roster is shape: the sessions are still perfectly reattachable and refusing
the whole file over the seating plan would cost four conversations to fix a screen.

#### The posture field records the room, and still never restores it

Both arguments are live now — `m.st.Write` and `m.st.Asking()` — and **the writer and every
reader moved in one change**, because the field has exactly one consumer. A writer reading the
state while a reader read the flags would compare a description of the live room against a
description of the launch argv and report a change to a user who made none. That is the
spurious fire the ruling names, and it would have been *introduced by the fix* had either side
moved alone.

Recording the room accurately is the opposite of restoring it, and nothing about the restore
changed. `TestReattachRestoresNoPostureAndNoGate` extends the old
`TestReattachDoesNotRestoreWritePosture` to the gate as well: a room saved `write` reopens read,
a room saved with the gate off reopens asking, and the WRITE marker is asserted absent on the
rendered frame rather than on a field. Both halves are witnessed, so it cannot pass by the room
being read-only for some reason of its own — the same fixture reopened with `--write --auto`
gets exactly what was typed. *A posture that can arrive from a file is not one anyone typed*
survives this section unchanged; what it never said is that the room may not write down what it
did.

#### Declined

**Restoring the gate.** It is authority, it is on the far side of the ruling's line, and `a`'s
own section already argues that a safety property whose default is off is the wrong way round
however carefully the constructor sets it. A gate that can arrive from a file is that mistake
with a longer fuse.

**Recording *why* the roster is what it is** — flag, command, or file. It is the room's own
history rather than its shape, `saved_at` already dates it, and a `reason` string is the first
thing in this file that would be prose.

**Bumping `roomVersion`.** A bump costs every user their reattach, and it is reserved for a
field that changes meaning. Nothing here changes what an existing key means.

### 9.33 the cursor seat's per-turn cost, split at last — and the seam that was hidden from `--help`

§9.8 gave the Claude seat one live process and measured what it bought. The obvious next
question was whether the Cursor seat could have the same thing, and the standing instruction in
`STATE.md` was to read a trace before optimising anything. This is that reading.

**Version pinned first, because the last capture's lesson was that a rule is only as general as
the capture it came from (§9.6c).** Everything below is `cursor-agent` **2026.08.04-aaa8809**, the
bundle's own `--version`, on Windows 11. That is **not** the version the rest of this seat was
measured against — `vendors/cursor.go` cites 2026.07.23-e383d2b throughout — and one of the
findings is a direct consequence of the gap.

**Instrument:** the vendor's own `node.exe` against `index.js`, argv identical to the seat's read
posture, with every stdout line stamped against the moment of launch. Two trials per arm. The
`result` event carries the vendor's own `duration_ms`, which is the cross-check: it agrees with
`system/init` → `result` on every trial, so the split below is the vendor's arithmetic as much as
this instrument's.

#### What the 44 seconds actually decomposes into

`STATE.md` already established that spawn is 13 ms and that `wait` is where the time goes, and
said outright what it could not do: `wait` bundles the vendor's startup with the model's
time-to-first-token, and nothing then in the room could separate them. Stamping raw lines
separates them, because `system/init` lands *before* the model is called.

Print mode, no `--resume` — trivial prompt, `reply with exactly: OK`:

| trial | launch → `system/init` | `init` → `result` (vendor `duration_ms`) | `result` → exit | total |
|---|---|---|---|---|
| 1 | 5.666s | 5.779s | 2.299s | 13.742s |
| 2 | 5.617s | 5.361s | 1.818s | 12.792s |

Print mode, `--resume` against a real prior session (created by the trials above, so nothing of
anyone's real work is in this record):

| trial | launch → `system/init` | `init` → `result` | `result` → exit | total |
|---|---|---|---|---|
| 1 | 5.196s | 5.042s | 3.104s | 13.337s |
| 2 | 5.551s | 4.298s | 3.080s | 12.928s |

And the startup itself, taken apart with progressively less work asked of the same bundle:

| what ran | to first output |
|---|---|
| `node.exe -e "console.log('x')"` — interpreter only | 0.078s |
| `node.exe index.js --version` — interpreter + bundle load + arg parse | 1.204s |
| a turn in an **untrusted** directory (aborts at the trust check, before any model call) | 2.139s |
| a real turn, to `system/init` | ~5.6s |

**Three things follow, and only the first was already known.**

**The standing diagnosis was right, and `--resume` is not the expensive half.** "`--resume`
restores context, not process warmth" is confirmed and now has a number against it: resumed
startup (5.196s, 5.551s) is *no larger* than cold startup (5.666s, 5.617s). Restoring a
conversation is free. What costs is the fixed startup underneath it, paid identically either way.

**Process cost is ~8.1s per turn and none of it is the model.** ~5.6s before `system/init` plus
~2.5s after `result` — the process lingers after answering — against a model turn the vendor
itself clocks at 4.3–5.8s. Of the ~5.6s startup, node is 0.08s and loading the bundle is ~1.13s;
the remaining ~4.4s is the vendor resolving auth, config, trust and workspace, and it is the
largest single item in the seat's budget.

**The honest proportion, stated so the number is not oversold.** On these trivial prompts the
8.1s is ~60% of the turn, but a trivial prompt is the arm that flatters the finding most. Against
the real room traced in `STATE.md`, where `cursor` totalled 25.014s, the same fixed 8.1s is ~32%.
The *absolute* figure is what is load-bearing: it does not shrink as the question gets harder, and
it is paid again on every single turn.

#### The seam: what print mode cannot do, and what the hidden subcommand can

Persistence needs two halves. The output half the seat already has — `--output-format stream-json`
is what §9.6c parses. The input half is the one that decides it: a way to hand turn N+1 to a
process that is already running.

**Print mode cannot be that channel, and the measurement is unambiguous.** Turn one was written to
an open stdin and then the pipe was *held*. Nothing happened for sixty seconds. Only when stdin was
closed did `system/init` appear, 3.6s later, and the turn ran — one turn, on the joined contents of
stdin, then exit. **Print mode drains stdin to EOF and treats the whole of it as one prompt, so the
EOF that starts the turn is the same EOF that destroys the channel for the next one.** There is no
`--input-format` in `--help`, and none in the bundle either: enumerating every flag the bundle
defines turns up hidden development flags (`--ian-dev`, `--sb-debug`, `--tool-gallery`), which is
what makes that absence evidence rather than an unsearched corner.

**One correction to this repo's own record falls out of the same probe.** `vendors/cursor.go` said
no code path in the bundle reads the prompt from stdin, and that there is no `-` sentinel and no
`--prompt-file`. That was true when it was measured; at 2026.08.04-aaa8809 the first clause is
**false** — a prompt piped in with no positional argument produced a normal turn. Nothing in the
seat depends on it (council always passes the prompt in argv), so this changed no code; the comment
is corrected because a stale measurement left standing is how the next reader inherits a wrong
premise.

**The channel exists, and `--help` does not mention it.** The bundle registers a subcommand marked
hidden:

```
Ce.command("acp",{hidden:!0}).description("Start the Cursor Agent as an ACP (Agent Client Protocol) server")
```

This is the `--permission-prompt-tool stdio` situation from §9.8 exactly — absent from the help
text and real — so it was driven live rather than believed. **Two turns, one process, one session:**

| trial | `initialize` | `session/new` | turn 1 | turn 2 |
|---|---|---|---|---|
| 1 | 1.944s | +0.994s | 5.285s | 5.335s (this turn ran a tool call) |
| 2 | 1.701s | +1.040s | 5.365s | **1.177s** |

The shape, recorded rather than the content: JSON-RPC 2.0, newline-delimited, on stdin/stdout.
`initialize` returns `agentCapabilities` — including `loadSession: true`, the resume equivalent.
`session/new` takes a `cwd` and returns a `sessionId` plus `configOptions`, among them a `mode`
select whose values are `agent`, `plan` and `ask`. Turns are `session/prompt` requests carrying
that `sessionId`; output arrives as `session/update` notifications (`agent_message_chunk`,
`agent_thought_chunk`, `tool_call`, `tool_call_update`) and the request resolves with a
`stopReason`. The second turn correctly answered a question about the first, from the same pid, so
this is one conversation in one process and not two conversations that happened to share a parent.

**The prize, stated as measured:** a follow-up turn costs **1.18s** where a print-mode turn costs
~13s, because the ~8.1s of process cost is paid once at `initialize` and never again.

#### What this section does NOT authorise, and why it stops here

The gate this work was run against was "build persistence only if the cost is process warmth *and*
a live-verified seam exists." Both are now true, so the finding is **build**, and it is worth
building. What the measurement also established is that the build is **not** the change it was
expected to be — mirroring §9.8's shape onto this seat — because ACP is a *different protocol*, not
the same protocol with an open stdin. Three forks come out of that, each a design decision rather
than a detail, and each one is recorded here instead of guessed at:

- **`Persistent` as written cannot express ACP.** `Turn(prompt) ([]byte, error)` is stateless: it
  returns the line for a turn. ACP needs `initialize`, then `session/new`, then a `sessionId`
  captured out of a *response* before any turn can be encoded at all — and `runner.Session` pipes
  lines and correlates nothing. Server→client requests (ACP's `session/request_permission`, the
  natural home for §9.8's gate) have no channel back at all today. That is a change to shared
  runner plumbing, not to one adapter.
- **Posture and cwd stop being argv-bound, which un-founds the respawn rules.** `persistent.go`
  respawns a seat when the room moves or a `/flow` hop needs a different posture, and the comments
  there rest on both being fixed at spawn. In ACP, `cwd` is a `session/new` parameter and `mode` is
  a session `configOption` — so a `/cd` could open a new *session* in the same live process, and a
  posture change might not need a respawn either. Whether it *should* is a product question about
  what the badge is allowed to promise, not a mechanical one.
- **Every measured claim on this seat was measured against print mode.** The §9.6c dedup rule, the
  `tool_call` oneof wire shape, the `--mode plan` badge and the Windows sandbox finding are all
  facts about a surface ACP does not use. Worth noting precisely because it is *not* yet a finding:
  across these two ACP turns, `agent_message_chunk` arrived once per turn with **no whole-message
  repeat** — which would mean the dedup rule is unnecessary here. That is a two-turn capture with
  one tool call in it, and §9.6c is the standing warning against generalising exactly that. It is
  a hypothesis for whoever builds this, not a rule.

So the seat keeps its print-mode invocation for now, and the next lane starts with a number, a
verified seam, and three named decisions instead of a guess.

#### 2026-08-15: the same rig, pointed at the codex seat, and what a warm thread saves

The rig above measured one vendor. This block runs it against a second one, and answers a
question `STATE.md`'s 2026-08-08 trace could not. That trace shows the codex seat paying
`wait=3.688s` before its first byte, while a cold binary start measures 190ms. Nothing said where
the other ~3.5s went. **This is measurement only. It authorises no seat change.**

**Version pinned first, and the subcommands were driven before they were believed.** Everything
below is `codex-cli 0.147.0` on Windows 11 (`codex --version`). That is a NEWER build than the one
`vendors/codex.go` cites. `codex app-server` and `codex app-server generate-json-schema` both
exist on this build and both ran: the schema command wrote 46 files to a directory, and the server
answered a live `initialize`. A subcommand named in `--help` is not evidence of a subcommand that
runs, which is this repo's twice-earned lesson, so both were executed rather than read.

**Instrument:** the installed `codex` binary, argv identical to the seat's first turn in
`vendors/codex.go` (`-s danger-full-access --skip-git-repo-check --cd <ws> -`), with every stdout
line stamped against the moment of launch. The prompt is **brief-shaped**: it opens with
`brief.go`'s own `--- operating context ...---` fence and carries the request under it, because a
greeting-shaped probe measures a transport the product never uses. One trial per arm, which is
half of what §9.33 spent. Treat every figure below as one observation.

#### The three-way capture, one identical turn

`codex exec` (human), one trial. The seat does not use this renderer; it is here because it is the
only arm that shows what the `--json` arm drops.

| stamped line | at |
|---|---|
| spawn returned | 0.030s |
| banner (`OpenAI Codex v0.147.0`) | 0.928s |
| `hook: SessionStart` | 3.709s |
| `hook: SessionStart Completed` | 4.201s |
| the model's answer (`OK`) | 6.800s |
| `tokens used` / `13,543` | 9.036s |
| process exit | 15.519s |

`codex exec --json`, one trial. This is the seat's own invocation.

| stamped line | at |
|---|---|
| spawn returned | 0.014s |
| `{"type":"thread.started",...}` | 1.153s |
| `{"type":"turn.started"}` | 1.554s |
| `{"type":"item.completed",...,"text":"OK"}` | 5.250s |
| `{"type":"turn.completed","usage":{...}}` | 5.327s |
| process exit | 13.266s |

`codex app-server`, one process, one thread, two turns. The fixed half is paid once:

| stamped line | at |
|---|---|
| spawn returned | 0.016s |
| `initialize` response | 0.246s |
| `thread/start` response, and the `thread/started` notification | 0.572s / 0.573s |

Then the two turns, both on that one open thread:

| turn | `turn/start` sent | `turn/started` | first `item/agentMessage/delta` | `turn/completed` | wait | stream | total |
|---|---|---|---|---|---|---|---|
| 1 | 0.576s | 0.836s | 5.085s | 5.259s | **4.509s** | 0.174s | 4.683s |
| 2 | 5.263s | 5.303s | 6.467s | 6.705s | **1.204s** | 0.238s | **1.442s** |

**A warm turn costs 1.44s, and 1.20s of that is the model.** Turn 2 asked what turn 1 had answered
and got it right from the same pid, so this is one conversation in one process. Against the same
prompt through `codex exec --json` the comparison is 1.442s against 5.327s to the last line, or
against 13.266s to exit.

**Four separate items make up the difference, and only one of them is process start.**

1. **Process and thread start is 0.573s, not 3.5s.** `initialize` answers in 246ms and
   `thread/start` in a further 326ms. Spawn itself is 16ms, which agrees with the 190ms class of
   figure and confirms again that spawning was never the cost.
2. **A `sessionStart` hook runs before the model does, and it is the operator's own.**
   `hook/started` at 3.151s and `hook/completed` at 3.840s, and the notification names its source:
   `"sourcePath":"C:\\Users\\sanle\\.codex\\hooks.json"`, `"source":"user"`, `"durationMs":838`.
   The human arm shows the same hook as `hook: SessionStart` at 3.709s. **This item is
   machine-specific.** A box with no `hooks.json` would not pay it, so it must never be quoted as
   a property of the vendor.
3. **Five MCP servers start on the same path.** `mcpServer/startupStatus/updated` fires for
   `node_repl`, `context7`, `github`, `kb-agent` and `codex_apps`, and two of them go
   `starting` to `cancelled` to `ready` across the turn. This is also operator config, and the
   same caution applies.
4. **The process lingers after it answers.** `exec --json` printed its last line at 5.327s and
   exited at 13.266s, which is **7.94s** of linger. The human arm shows 6.48s of the same. §9.36's
   "kill, never wait" rule was written for a different vendor and a ~2.5s linger. **This vendor's
   linger is larger, and nothing here checked whether council waits on it.**

**One comparison this block does NOT make.** The `exec --json` arm reached its first line at
1.153s, well below `STATE.md`'s `wait=3.688s`. That trace ran a real brief through a real room
with four seats starting at once, and this trial ran a trivial prompt alone. The 3.688s is not
reproduced here and must not be treated as refuted.

#### The hook question, answered by the captures

**`codex exec --json` is the only one of the three surfaces that hides hook activity.** The human
renderer prints `hook: SessionStart` and `hook: SessionStart Completed`. The protocol emits
`hook/started` and `hook/completed`, each carrying the hook's id, event name, source path, source
and `durationMs`. The `--json` stream emitted **four lines in total** for the whole turn
(`thread.started`, `turn.started`, `item.completed`, `turn.completed`) and not one of them mentions
a hook, an MCP server, or a rate limit.

The protocol also carries two things the seat currently reads off disk instead:

```
{"method":"thread/tokenUsage/updated","params":{...,"tokenUsage":{"total":{"totalTokens":21130,...},"modelContextWindow":258400}}}
{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":2,"windowDurationMins":10080,"resetsAt":1787369304},"secondary":null,"planType":"plus",...}}}
```

Those are the same fields §3.4 verified in the rollout files, arriving live on a socket. **Nothing
is built on that here.**

#### Seat-move viability, recorded and not acted on

1. **Thread continuity exists.** `thread/started` arrived live at 0.573s. `thread/resume` is a
   real request and its schema documents three routes (`thread_id`, `history`, `path`), plus
   `thread/fork`. The `thread/start` result carries `thread.id`, an identical `sessionId`, and the
   rollout `path` under `~/.codex/sessions/...`, which is the same id the adapter already reads.
2. **The sandbox channel exists on this path, and it is wider than `-s`.** `thread/start` takes a
   `sandbox` parameter, and the live response echoed `"sandbox":{"type":"dangerFullAccess"}`.
   `turn/start` takes a per-turn `sandboxPolicy`. That is strictly more than `codex exec` offers,
   where `-s` is first-turn-only and `codex exec resume` rejects it outright. Only
   `danger-full-access` was driven here.
3. **The Windows `danger-full-access` finding does NOT carry over, and needs its own re-check.**
   The evidence is direct rather than inferential: this protocol has a Windows sandbox surface
   that `codex exec` has no equivalent for. `windowsSandbox/setupStart` and
   `windowsSandbox/readiness` are client requests, and `windowsSandbox/setupCompleted` and
   `windows/worldWritableWarning` are server notifications. `vendors/codex.go`'s finding rests on
   `-s read-only` failing every process spawn, and `read-only` was never sent on this path. Until
   somebody sends it, the seat's badge rule stands unchanged.

**Spend:** four billed turns. Two arms of one turn each, plus two turns on the app-server thread,
because a single app-server turn would have reported turn 1's 4.509s as if it were the warm number
and oversold nothing or undersold everything depending on which row was quoted.

### 9.34 the rebuttal stopped naming its authors

A `ctrl+r` turn used to quote each seat's answer under its vendor's name: *"quoted reply from
Codex"*. The fence's security framing was right and is untouched (quote.go); what was wrong is
subtler — **the receiving model was told who wrote what**, and models weigh an argument
differently when it arrives under a name they recognise. That is the self-preference /
identity-bias class that peer-review setups blind for, and the reason llm-council anonymises its
review stage before models rank each other. A rebuttal exists to test the argument, so the
argument is now what crosses: *"quoted reply from participant A"*.

The mechanics, because each carries a decision:

- **Labels are positional per receiver** — seat order with self skipped — and a seat with
  nothing to quote this turn keeps its letter reserved, so a quiet seat does not shuffle every
  neighbour's identity. The letters exist so a multi-turn argument stays attached to a
  consistent speaker; letters that agreed BETWEEN receivers would need a shared assignment
  written somewhere the models could correlate, and nothing downstream may join on them anyway.
- **The blinding is label-deep, and says so.** A reply whose content self-identifies ("as
  Claude Code, I…") has identified itself, and editing another participant's words to hide it
  would be the censorship the fence refuses — the room shows what was said. Best-effort
  blinding, stated as such, over silent redaction.
- **The user is not blinded.** Columns stay labelled by vendor; the blind applies to what the
  models read, never to what the person sees. Nothing in the room's rendering changed at all.
- **One test inverted, on purpose.** `TestQuotedMaterialIsFencedAsUntrusted` used to fail with
  "quoted material is not attributed to its author"; attribution to the model is now the
  defect. The name-absence checks in the nothing-quotable test were re-grounded on the fence
  itself at the same time, because a vendor-name check passes vacuously against a prompt that
  never contains vendor names — a guard that cannot fail is not a guard (the same one-level-up
  rule the architecture repo's test policy states).

What this deliberately does not add: a ranking stage, a chairman, or any synthesis hop.
llm-council's stage 3 collapses the answers into one; §9.2's position is that independent
answers ARE the product, and a synthesis is available today as an explicit `/flow` hop the user
types. Blinding sharpens the comparison; it does not delegate the verdict.

### 9.35 a running chain can be told to stop after this hop

`/flow` shipped with one control over a chain already moving: ctrl+c, the turn-cancel key. That
is a hard abort — it interrupts the hop mid-sentence — and it was also a lie in three parts,
which is where this section starts, because the honest baseline had to be measured before a
gentler control could be designed against it (the flow-autoadvance plan named this gap and
nothing tracked it).

**What cancelling mid-chain actually did, measured 2026-08-08.** Ctrl+c during hop 1 of 3
killed the hop and the chain never advanced — that much was right, and stays. Everything around
it was wrong: the current step stayed `running` forever, the header went on claiming `hop 1/3`
over a room doing nothing, and the next brief the user typed was **eaten** — dispatch saw a
live chain, tried to continue it, failed with `flow start error: cannot start step in state
running`, and threw the user's words away. The second enter worked. And the corpse was not the
cancel path's alone: **a chain that COMPLETED left the same corpse**, and the first brief after
a successful flow died as `cannot start step in state returned`. The happy path was charging
the same tax.

**The fix under everything else: a chain ends whole, or it has not ended.** `endFlowChain`
retires the chain, its draft, its carry, its pending flags and its header marker in one move,
and every ending — finished, failed, cancelled, stopped, refused at the write gate, start
error — goes through it. Half of these paths used to clear only the marker and half only the
chain; each half-state was a room asserting something false about the other half. The
teardown in `turnColumnFinished` is the backstop for the endings `finishFlowHop` never sees
(a cancelled hop, a vendor failure, a hop that returned nothing), because teardown is the one
place every turn's death already passes through — and it says which death it was: `flow
cancelled at hop 1/3 — 2 later hops not run` against `flow stopped at hop 1/2 — 1 later hop
not run`. Cancelled and failed are different facts, and a stopped chain must never render as a
finished one (§4a.1).

**The control: `s`, stop after the hop that is running now.** The middle ground ctrl+c cannot
offer: the current hop finishes on its own terms — artifact saved, receipt verified, Returned
or Published exactly as if nothing had been pressed — and the chain ends there instead of
handing off. Pressed while the hop streams, because that is when the decision forms: you are
reading hop 2's output when you learn hops 3 and 4 are no longer worth their quota. Reading
the columns is part of deciding — §9.16's own argument for its gate — so the key lives in view
mode where the columns stay scrollable, and interrupts nothing.

The grammar decisions, each against a precedent:

- **A key, not a room command.** `c`'s reason (§9.17): a key takes no vocabulary from the
  composer, and `/stop` is a word people address vendors with. Not `c`'s confirmation though —
  `c` spends a `y` because a dropped thread is irreversible, and this destroys nothing: the
  hop completes, every artifact lands, and the cost of a stray press is one keystroke to undo.
  So `s` is a toggle, `a`'s shape, and pressing it again re-arms the handoff.
- **The armed state lives on the chrome, not in the notice.** The WRITE badge's argument: a
  notice scrolls away and the promise persists. The hop cell reads `hop 2/4 @codex (stops
  here)` — words, so it survives `--ascii`, and on the hop cell because it is a fact about the
  hop: this one runs, its successors do not. The busy mode line offers `s stop after hop`
  while a chain is live and flips to `s continue chain` when armed, the label-renames-itself
  rule `a` set. `TestFlowStopIsAToggleAndTheArmedStateRenders` pins all four states.
- **The last hop refuses to arm.** The chain ends there whether or not `s` is pressed; a key
  that "worked" would claim credit for an outcome it did not cause. The refusal says so —
  `hop 2/2 is the last — the chain ends here anyway` — and a press with no chain running is
  answered rather than swallowed, §9.12's attribution rule for a key that did nothing.
- **Not in helpKeys, deliberately.** The key exists only while a chain runs, and the mode line
  names it on every frame of exactly those moments — the contextual-control surface `y`/`n`
  and the card's `a` already use. The help panel's 17-row budget has no row for a key that is
  dead in every room the panel is usually read in; the §9.17 sweep's rule was about controls
  reachable from inside the room, and this one is announced there.

**What was declined.** Pausing — a stopped chain that could resume where it left off — is a
real feature and not this one: the carry artifact is consumed at dispatch, the draft would need
re-parsing against a chain whose position moved, and stop-then-retype is the honest v1. A stop
that also killed the current hop was declined because ctrl+c already is that, and two keys with
one meaning apart is how a keymap grows synonyms. And ctrl+c stays exactly as hard as it was:
the interrupt semantics did not move, only the lying state it left behind.

The general lesson, in this file's own terms: §9.16 built the chain's authority grammar and
§9.17 built the room's mid-session controls, and the seam between them — a chain that is
neither obeying nor gone — belonged to nobody, so nothing asserted on it. The corpse survived
every ending, including the successful one, because the tests all stopped at "did not
advance" and none typed the next brief. The regression tests here end by dispatching one.
### 9.36 the cursor seat re-founded on ACP: what the wider capture said, and what it cost

§9.33 ended with a build verdict, a verified seam and three named decisions. This is the build.
The ruling that shaped it was **wholesale**: ACP replaces the spawn-per-turn path rather than
sitting beside it, there is no fallback, and git history is the record of what went. A fallback
would be a second protocol to keep honest, and the numbers below are the reason nobody would
want to fall back to it.

**Version pinned, and it is the same one.** `cursor-agent` **2026.08.04-aaa8809**, the version
§9.33 measured, on Windows 11 — so nothing here is confounded by a bundle change. Instrument: a
throwaway JSON-RPC client driving `node.exe index.js acp`, every line stamped against launch,
across **thirteen arms**. Then the finished seat re-verified through the room's own code.

**One environment note, because it cost an arm and will cost the next reader's.** The first
capture had every tool call blocked by this machine's own `PreToolUse` credential guard, whose
wrapper fails closed when cursor-agent is launched from a Git Bash parent on Windows (a known
upstream wrapper bug, agent-ops ADR-012). Nothing was wrong with ACP. **Drive cursor-agent from
a PowerShell or cmd parent on Windows**, or every tool in the capture will read as failing.

#### Phase 1: what two trivial turns could not have told us

§9.6c's lesson is that a rule is only as general as the capture it came from, and §9.33 flagged
its own two-turn no-repeat finding as a hypothesis for exactly that reason. So the capture was
widened first, and the widening changed three conclusions.

| what was asked | trials | what came back |
|---|---|---|
| a turn that runs a TOOL | 3 | `tool_call` then `tool_call_update`(in_progress) then `tool_call_update`(completed). `title` always populated; `rawInput` **empty** for Read/Find/grep and populated for shell |
| several model calls in one turn | 2 | four tool calls and three message segments in one turn, interleaved, with no envelope around a "call" |
| a long streamed reply | 1 (300 words) | 24 `agent_message_chunk` in 2.6 s — ~95 chars each, ~9 a second |
| an interrupted turn | 1 | `session/cancel` (a notification) and the open `session/prompt` resolves `{"stopReason":"cancelled"}` **23 ms** later; the process took a further turn 1.1 s after that |
| resume in a NEW process | 2 | `session/load` works, and **replays the whole prior conversation** onto the update stream before it answers |
| a dead thread | 2 | `-32602 … Session "…" not found` in **0.45 s**, and the process survives — a fresh `session/new` answered 0.45 s later |
| cwd binding | 1 | **per SESSION, not per process.** One server ran a session in `ws1` reading ws1's file and another in `ws3` reading ws3's |
| workspace trust | 2 dirs | **does not apply.** Print mode refused the same directory with "⚠ Workspace Trust Required"; the ACP server wrote a file into it |
| a permission prompt | 2 | `session/request_permission` **blocks**; `allow-once` ran the command, `reject-once` did not and nothing was created |
| an edit | 2 | ran ungated — **no permission request at all**, in a never-trusted directory, under the user's own `approvalMode: allowlist` |
| plan mode | 1 | `session/set_mode {"modeId":"plan"}` accepted; asked to create a file the seat declined and **no file landed** |
| the dedup hypothesis | every arm | **no whole-message repeat anywhere, and no `model_call_id` field in ACP traffic at all** |

Timings across the twelve arms that ran a handshake: `initialize` **1.43–4.30 s**, `session/new`
a further **0.85–2.55 s**, `session/load` a further **0.89–1.37 s** — so resume is once again no
more expensive than a fresh conversation, which is the same shape §9.33 measured for `--resume`.
Warm turns: **1.12 s, 1.79 s, 1.82 s**, against §9.33's print-mode ~13 s.

**Three of these overturn something.**

**The dedup rule is not carried over, and it is now a measurement rather than a hypothesis.**
§9.6c's rule exists because print mode sent a model call's deltas and then that call's complete
message, so appending both rendered the passage twice. Across a turn with four tool calls and
several model segments, ACP repeated nothing and carries no `model_call_id` at all. §9.33 was
right to refuse to generalise from two turns; the wider capture is what earns the conclusion.

**The safety net that rule leaned on is gone with it.** §9.6c named the fallback explicitly —
"the failure mode is a column that fills at the end, never one that is wrong" — because print
mode's `result` carried the whole reply. An ACP turn resolves with `{"stopReason":…}` and
nothing else: no reply, and no token usage either. So a broken chunk parser here gives an
**empty** column, not a late one. There is no mitigation that would not be invented, so it is
stated instead — in `cursor.go`, in `dispatch.go` where the old special case was, and in
`STATE.md`.

**Workspace trust does not apply on this path.** This is the one finding that makes a claim
*worse*, and it is on the badge for that reason. The tightest form of it: the directory print
mode had just refused was written to over ACP, with no prompt, minutes later.

#### Phase 2: what was built

**runner grew a second protocol shape, and the stream-json path did not move.** `ParseFunc` sees
a line and has nowhere to reply to, which is enough for a monologue and cannot express ACP: a
turn cannot be *encoded* until `session/new` answers, the child asks questions that block it,
and ids come in two independent namespaces. So `runner.Protocol` is a stateful per-process
driver that owns both directions and returns lines rather than writing them — which keeps it
replay-testable exactly as a `ParseFunc` is. `StartSession` and `StartRPCSession` are two
wrappers over one body; the Claude adapter was not touched and its tests did not change.

`Session` grew `SendTurn` and `SendAside` in place of a bare `Send`, and the split is the turn
clock: an ACP protocol may **take** a turn it cannot yet encode, and the person who pressed
enter is waiting from that moment whether or not a byte has moved. An answer to a question the
vendor asked mid-turn goes the other way — it belongs to the turn it is holding up, so it must
not start a new one.

**The seat.** `vendors.Conversational` is a sibling of `Persistent`, not a subtype: `Open`
returns a spec plus the protocol. The invocation is now the single word `acp`. Posture arrives
as `session/set_mode`, the workspace as `session/new`'s `cwd`, and the brief as a JSON string —
so no prompt text can reach argv by any path, which retires the shell-shim question this seat
used to have to reason about.

**Re-measured, not inherited.**

| claim | verdict |
|---|---|
| granularity `tokens` | **re-earned, with a caveat.** ACP chunks are ~95 chars at ~9/s — coarser than print mode's real tokens ("P", "ONG") and *finer in time* than the Claude seat that already carries this word (§9.7: ~80 chars, ~3/s, flagged there as an overstatement). The word stays with its existing looseness and no new looseness; fixing it is one change to both seats at once, which is why §9.7 left it as a separate change to a separate surface |
| `ro:requested` | **level unchanged, reasons replaced.** Plan mode did better than print mode's ever did, and it is one trial of a mode the model obeys |
| `--sandbox enabled` | **gone.** ACP takes no sandbox parameter on any OS, so the badge is no longer split by platform. On Windows nothing was lost — the flag was measured killing the turn. On macOS and Linux what was lost is a *request* whose enforcement was never observed |
| `gated` | **withheld, deliberately.** `canGate` used to read "is this a live process", off the registry. That was right only while those two questions had one answer. This seat can ask *and does not ask about edits*, and `gated` promises that nothing which changes anything runs without a keystroke — so it keeps `WRITES`, and its detail says what the cards cover and what they do not |
| the §9.6c dedup rule | **retired**, above |
| the `result` fallback | **retired**, above |

**The gate, such as it is.** Council answers every `session/request_permission` — an unanswered
one blocks the vendor forever, which is a column that never finishes. In a write posture the
request becomes the room's ordinary approval card; in a read posture the adapter refuses it
itself and records the attempt in the trace, because a read-only seat asking to change something
is not a question for the user, it is already answered. `allow-always` is never selected in any
posture: it writes a permanent rule into the user's own `~/.cursor/cli-config.json`, which is
the line this adapter already declines to cross by never passing `--trust`.

Two shapes fall out of the capture and both are in the code beside the lines that produced them.
A **rejected** call arrives as `completed` with no output at all — indistinguishable on the wire
from a completion that said nothing — which is §9.8's `ActDenied` argument in a sharper form: the
room records the refusal from its own keystroke and `recordAct` refuses to let the echo overwrite
it. And ACP's rejection carries **no message field**, so unlike the Claude seat this one cannot
ask the model not to retry; it was measured saying "DONE" afterwards as though nothing had
happened.

**`session/load` replays history, and dropping it is load-bearing.** A loaded session streams the
entire prior conversation back — old prompts, old tool calls with their real output, old replies
— *before* it answers. A parser that appended it would refill a reattached column with the whole
previous room. The gate is the pending response rather than the `replay-` prefix those ids happen
to carry: a prefix is a spelling, the pending request is the protocol.

**Two hazards this protocol has that a one-way stream does not, both found in review and both
ending in a room nobody can quit.** They are recorded because neither is visible from the wire
format alone.

- **A turn's end is a RESPONSE, so a turn that was never sent can never end.** Anything that
  holds a brief — the handshake, the `session/set_mode` round trip — is a window in which there
  is no outstanding `session/prompt` for the vendor to answer or for a cancel to abandon. So the
  protocol refuses an interrupt in that window rather than reporting a quiet success, which is
  what makes the room fall through to killing the seat; the alternative was a turn that never
  ends, a room that then refuses every further brief, and a `q` that will not quit.
- **A failed handshake is TERMINAL, because the server does not exit on one.** An ACP server that
  refuses `initialize` answers and stays up — and a live process is exactly what §9.8's stale-exit
  guard correctly reads as a healthy seat. Without a terminal state the room would keep handing
  that process briefs forever. The protocol refuses instead, the seat is killed and forgotten, and
  one retry inside the same dispatch gives the user a working column rather than an error naming a
  handshake they cannot see. The likeliest trigger is an unauthenticated CLI: somebody's first run.

The same class of care applies to the `set_mode` window in the other direction: a turn *taken*
there must wait too, or it would go out under the server's default `agent` mode while the badge
said `ro:requested` — invisibly, because a reply from the wrong mode looks exactly like a reply
from the right one.

**A refused thread now costs two round trips instead of a process.** The one-attempt rule the
ninth amendment established is unchanged and is simply cheaper here: the id is spent, the same
process opens a new conversation 0.45 s later, and the brief still runs. Reattachment therefore
never fakes a restored thread — if the load is refused the seat honestly starts fresh and
`settleRestoredThread` says so, exactly as it does for the other three seats.

#### The forks, and the one that was decided rather than measured

§9.33 named three. The first (Persistent cannot express ACP) and the third (every claim was
measured against the wrong surface) are settled above. The second is a **choice**, and it is
called out here because a reader would otherwise find a measurement in the code and wonder why
it was not acted on:

**`cwd` and posture are no longer argv-bound, and the seat is respawned anyway.** Measured: one
process really did run two sessions in two directories. So a `/cd` *could* cost a new session
(~1 s) instead of a new process (~3 s). It costs a process — because what a move actually costs
the user is a new conversation either way; because one rule across four seats is worth more than
three seconds; and because re-opening a session inside a live process has failure modes (a
half-moved session, a queued turn addressed to the old one) that nothing has measured. The
argument is on `seatProc`, the behaviour is pinned by `TestAMovedRoomReplacesTheCursorSeatToo`,
and it is revisitable with a measurement rather than with a preference.

The **stale-exit guard** (eleventh amendment) applies to this seat unchanged and is re-asserted
for it: a terminal event names a vendor, not a process, and acting on a predecessor's exit would
fail the live turn and leave a real process running and invisible.

#### Verification

Fixture replay in the #62 style over synthesized shapes
(`vendors/testdata/cursor-acp-turn.jsonl`), lifecycle pinned by **process counts** rather than by
anything the adapter says about itself, and one live multi-turn conversation through the merged
seat (`-tags=live`):

```
turn 1  phase=done  elapsed=9.744s  act "Read File" → ok   body: github.com/sanlee-ys/telltale
turn 2  phase=done  elapsed=1.120s  same process           body: github.com/sanlee-ys/telltale
```

Turn one read a file it could only have read by running a tool in the workspace; turn two
answered a question only turn one's history could answer, from the same process, in 1.12 s. That
is the whole of §9.33's prize, measured through the room rather than through an instrument
standing beside it.

**Not verified here: macOS.** Every arm ran on Windows 11, and the Mac's ACP seat is unmeasured.
That belongs in `PARITY.md` rather than in this section.

### 9.37 /arena: the seats race in worktrees, and the human picks the winner

`/arena <brief>` is one brief raced across every seated vendor, each attempt in its own git
worktree, compared by diff instead of by prose. It is §9.2's thesis — independent answers ARE
the product — applied to code, where "independent" stops being free: four writers in one shared
tree are not four answers, they are one trampled tree. The isolation the manager lane built its
whole category on (Crystal's same-prompt sessions, claudexor's best-of-N envelopes,
parallel-code's AI Arena) is what makes four *write* attempts comparable at all.

Ruled 2026-08-08, four decisions and their reasons:

- **Per-turn, typed at the room** — not a launch posture. §9.17's rule; a race is something you
  want *about a brief*, not about a session.
- **Every attempt is a FRESH session.** All three comparable products race fresh, a continued
  thread would anchor each seat on its own prior answers, and whether resume even survives a cwd
  change is measured for none of the spawn-per-turn seats — so fresh is also the only option
  that costs no new vendor measurements. Mechanically: every seat goes through the `FirstTurn`
  one-shot it already implements, the persistent seat included. The room's live process, saved
  ids and conversations are untouched — dispatch guards the session-id capture so a race's
  throwaway ids can never replace the room's saved threads (the reattach-swap bug, killed in a
  test before it could exist).
- **Worktrees are KEPT until the user deletes them**, named `<repo>-arena-t<N>-<vendor>` as
  SIBLINGS of the workspace with branches `arena/t<N>/<vendor>`. Siblings, not a state
  directory: kept-until-deleted means the user must SEE what is kept, it matches the README's
  own worktree convention, and /cd's sibling resolution makes `/cd repo-arena-t7-codex` work
  with zero new code.
- **Comparison lands in-column** — `git diff --stat` against a base SHA recorded once before any
  seat spawned, rendered in the transcript's boundary grammar; `y` yanks the full diff (capped
  at 1 MB, truncation stated). Three outcomes, three renders: a diff, a measured "no changes
  against <base>", and "diff unavailable: <why>" — zero, absent and degraded stay three
  different facts (§4a.1).

Two mechanics carried in from the deep-read of claude-squad's `session/git/diff.go`, because
they are the difference between a diff surface and a lying one: the diff anchors on the
**recorded base SHA**, never HEAD, so an attempt that commits mid-turn cannot show an empty
diff; and `git add -N .` runs before diffing so an attempt whose whole answer is a NEW file
cannot read as "no changes" — the false zero, again.

Posture is `PostureWrite` for every racing seat, stated rather than hidden: a one-shot process
has no channel to be asked on, so the gate structurally cannot exist here, and the containment
is the worktree — which is the whole reason the worktree exists. A read room refuses `/arena`
with the in-room remedy named (`/write lets it`), per §9.17's tell.

**What council deliberately does not do, having read the competition:** claudexor AUTO-ADOPTS
the winning patch into the live tree. This room offers the diffs and the human picks — adoption
is a git command the user runs against a kept branch, never an action taken for them. And a
race is not routable in v1 (`@codex`-only arenas): the value is the comparison, and a one-seat
race is an ordinary turn in a worktree, which `/cd` already provides.

Deliberately deferred, each its own change judged against this section — and every one has now
landed, each as its own change (the 2026-08-09 amendments below): ~~commit-per-turn inside arena
worktrees~~ (with its undo), ~~`.worktreeinclude` seeding (the first real arena run on a repo
needing `.env` will surface it)~~ (landed on exactly that argument, ahead of that repo showing
up), and ~~a deletion guard stronger than git's own refusal to remove a dirty worktree~~ (landed
as `/arena drop`'s counted refusals, beside `/adopt`). This paragraph briefly existed as two
half-struck copies of itself — two same-day changes each struck their own item and a text merge
kept both variants — collapsed back to one on the same day.

Verification note: the git mechanics (worktree creation from one base, add -N, the three
collection outcomes, the session-id guard, the renders, the yank) are all pinned by offline
tests against a real temp repository. ~~No live vendor has raced yet.~~ **The first live race
ran 2026-08-09** — turn 4 of a real room on the Windows box, four seats dispatched, `/arena`
against this repo at 422b1c3 — and it paid the debt this note carried while measuring exactly
where the predicted risk lived:

- **The core is verified live.** Worktrees created as named siblings, three seats raced fresh,
  ranks rendered in host-observed order (agy 1st · 7s, codex 2nd · 15s, claude 3rd · 19s), and
  the zero-render said "no changes against 422b1c3." on every finisher — honest zeros, since
  the brief was a harness check that asked for no changes. The room's threads survived intact.
- **The cursor seat cannot race, by its own design.** The ACP refounding (§9.36) gives
  `Cursor.FirstTurn` a deliberate refusal — "driven as a live ACP process, not as one child per
  turn" — which arena's uniform one-shot path duly surfaced on the column. The fix is a
  follow-up with its own shape: an EPHEMERAL ACP session per race (spawn in the worktree, one
  turn, kill), which is §9.36's machinery pointed at a throwaway session. ~~On the deferred list
  below until someone builds it; until then a race is honestly 3-of-4 on Windows.~~ **Built
  2026-08-09, in exactly that shape — second amendment below.**
- **The write seat hit the allowlist-prefix trap.** claude's one probe — `git -C <worktree>
  status --short --branch` — met `autoAllowedTools`' `Bash(git status:*)` rule and failed the
  prefix match, so an ungated print-mode seat had an approval request and nobody to ask (act
  rendered ✗, correctly). The fix is NOT a blind `Bash(git -C:*)` — that constant also serves
  `--auto` in real workspaces, where pre-approving every `-C` form is a wider grant than the
  verbs it lists — and the vendor file already warns its rule grammar has not been driven.
  ~~What this needs first is one measured probe of whether the rule syntax can scope a verb
  behind `-C` at all; the finding is filed, the measurement is the next step.~~ **The probe
  ran, 2026-08-09, and closed this.** A four-arm probe on the reference box measured the
  matcher as prefix-only, with no rule spelling that scopes a verb behind `-C`, so
  `Bash(git -C:*)` stays rejected and a seat runs plain `git` — its cwd is already the
  workspace. The record is `STATE.md`'s "Closed without code" entry with its 2026-08-10
  amendment, plus `autoAllowedTools` in `internal/council/vendors/claude.go`. Do not re-open
  it without a new measurement.

**Amendment, 2026-08-08: the finish line and the `d` key.** Two deferrals came off the list:

- **Every racer's arena block now carries a finish line** — *"2nd of 4 · done · 25.0s"* — and
  each part keeps its own epistemics. The rank is the order the ROOM saw seats land
  (finishColumn call order, host-stamped; event batching bounds the resolution, which is the
  honest limit of what was measured — a vendor's own claim about when it finished is an
  inferred value wearing measured clothes and is not consulted). The phase word is welded to
  the rank on purpose: "2nd · failed" and "2nd · done" are different facts, and a bare number
  would let a fast crash read as a podium. A DNF ranks — it landed, just not well. The elapsed
  is the column's own measured clock. parallel-code's results screen is the pattern source,
  minus its star rating, which is a judgment no gauge here is allowed to render.
- **`d` flips the focused seat's arena block from stat to the full patch** and back. Per
  column, because reading A's stat against B's whole diff is a legitimate way to compare. The
  frame renders at most 400 patch lines (`arenaDiffScreenLines`) — a render cost bound, not a
  data bound — and the cutoff names how many lines it dropped and both routes to the rest
  (`y`, and the worktree itself). Three refusals with three sentences: no race this turn, a
  measured nothing-to-show, and an unreadable diff carrying its reason. ~~Plain text, no diff
  colouring yet: +/- prefixes are the first signal and survive `--ascii`; colour through the
  existing palette is a later, separate change under style.go's no-new-hues rule.~~ **Coloured
  2026-08-09, through the existing palette and nothing else** (`Styles.ForDiffLine`): added
  lines wear `SevOK`, removed lines `SevCrit`, headers (`diff --git`, `index `, `---`/`+++`,
  `@@`) the muted chrome style — no new hue, per style.go's rule. Classification reads the raw
  prefix with headers matched first, so `+++` never wears the addition's green. The `+`/`-`
  prefixes stay the first signal: `PlainStyles` renders the same bytes as before, which is why
  no golden moved, and `--ascii`/`NO_COLOR` see exactly the frame they always did. The stat
  blocks (interim and final) stay unstyled — a stat is a summary, not a patch line.

**Amendment, 2026-08-09: the live stat — the race shows the diff growing.** Until now a
racer's stat appeared only when its column finished; the audience of a 20-second race
watched three spinners and then a scoreboard. The pattern is the manager lane's
event-triggered diff refresh (codeg's), rebuilt under this room's honesty rules
(`internal/council/arenalive.go`):

- **Event-triggered, throttled, off the loop.** Stream activity on a racing column (text or a
  tool call — a session id arriving is not evidence the tree moved) ARMS a re-read of that
  seat's worktree — `git add -N . && git diff <base> --stat`, the same two claude-squad
  mechanics the finish-time read carries, stat only (the full patch stays a finish-line
  deliverable). An armed seat is read at most once per `arenaRefreshInterval` (2 s: the read
  is a subprocess pair, the audience is human, and the first live podium ran 7 s/15 s/19 s —
  faster buys frames nobody can distinguish), timed off the tick-stamped `State.Now` so the
  throttle is testable and Render stays pure. The read runs as a Bubble Tea command
  (goroutine → `arenaStatMsg`), never inline in Update, never in Render; one read in flight
  per seat, a due refresh that finds one running SKIPS rather than queues. An idle seat never
  arms, so an idle seat is never read; a seat whose worktree failed setup has no refresh slot
  at all, by construction.
- **The interim marker ruling.** A mid-race read is a measured value at a moment that is
  already past, so the block's label is `arena · so far` — the "so far" is the whole marker,
  the same honesty spend as an estimate's `~` — and it withholds the finish line's receipt
  (branch, tree path, rank), which would dress an interim block in the final's clothes. Three
  states stay three renders (§4a.1, mid-race edition): no read yet is the nil pointer and
  renders NOTHING (absence, not a zero); a read that returned empty says "no changes yet
  against <base>" (the "yet" is what separates a running seat's measured zero from the
  final's settled one); a failed read carries git's first stderr line, never dressed as
  no-changes. A failed read degrades only the live stat — the race runs on — and
  `arenaRefreshMaxFails` (3) consecutive failures end the seat's live stat WITH THE STOP
  NAMED on the column ("stopped re-reading … the finish-time diff still runs"), because a
  gauge that quietly freezes goes on reading as live. A success resets the count: the
  likeliest failure is the refresh contending with the vendor for the worktree's own index,
  and one contended read is not evidence the tree is unreadable.
- **The finish-time `collectArena` read stays the authoritative final and REPLACES the last
  interim — cleared, never merged.** The refresh state lives on the turn, so teardown ends
  all refreshing with no cleanup path to forget; a read that outlives its turn or its seat
  arrives as a stale message and is dropped by comparison (turn number, final-already-landed),
  not by hoping the timing worked out. The one collision the feature introduces is named in
  the code: an interim `add -N` holds `index.lock` for milliseconds, so a final read that
  fails while a refresh is in flight is retried once — reporting "diff unavailable" for a
  lock this feature itself held would be the refresh degrading the read it exists to
  complement.

Verification note: the mechanics — arming, the throttle, single-flight, the three interim
renders, replacement by the final, stop-on-turn-end and stop-on-repeated-failure, the
`add -N` false-zero property of the interim read — are pinned by offline tests
(`arenalive_test.go`), the git ones against a real temp repository. ~~No live race has
watched the stat move yet~~ **Half paid, 2026-08-09, and the halves are worth keeping
apart.** The block APPEARING mid-race and reading honestly is live-verified by the give-up
amendment's own race below: the stuck cursor racer's `arena · so far` read "no changes yet
against \<base\>" for 26m40s, which is the interim empty state observed live for longer
than anyone wanted. What is still owed is the other half — a "so far" block that GROWS and
then swaps for the settled block at the finish — because no live race is recorded as having
watched a NON-empty interim stat change. One `/arena` against a brief that changes files
pays it, and it is stated here rather than implied paid.
**Paid, 2026-08-15/16, race t9 — the first 5-of-5, and the growing half both.** The owner
raced a file-changing brief across all five seats. The Antigravity and Cursor columns both
drew a non-empty `arena · so far` that CHANGED on a later refresh (one file, then two) and
was replaced by the settled block at landing — the growing half, watched twice over. The
race's full record: three clean finishes with ranks and receipts (claude `1st of 5 · 50s ·
committed 2770c0c`, grok `2nd · 1m8s · 4aba168`, codex `3rd · 1m10s · 6874ff2`), and two
seats given up with `x` after ~11 minutes (agy `4th · cancelled · committed 91fc53e`,
cursor `5th · cancelled · committed 6b94b55`) — the give-up's second and third live
exercises, and both cut seats kept their commit receipts exactly as the finish-line design
promised. The cause of the two stalls was measured from OUTSIDE the room before the cuts:
both racer processes were alive with ~zero CPU over an 8-second sample and no go toolchain
process existed anywhere, so the work was done and the vendors' own turns had stalled —
agy inside a `manage_task`/`schedule` poll loop that stopped polling, cursor after its
final tool step. `/adopt claude` then exercised the dirty-room refusal live (the probe
turn's two throwaway edits held the tree; the card named them; the operator restored and
re-ran) before cutting `adopt/t9-claude` and landing the `--no-ff` merge cleanly — the
refusal path's first live run. **One new gap, found by the same race:** `/trace` was armed
before the turn and recorded NOTHING for it — the trace file holds only the preceding
ordinary turn's line — so an arena turn produces no per-seat spawn/wait/stream split, and
grok's timing on that axis stays unmeasured. Recorded as an unowned gap in STATE.md.
**Amendment, 2026-08-09: the cursor seat races too, on a throwaway session.** The deferred
follow-up the verification note filed is built, in the shape it predicted. For an arena turn —
and only there — dispatch recognises the Conversational seat and, instead of the `FirstTurn`
one-shot it deliberately refuses, launches a throwaway `cursor-agent acp` server rooted in that
racer's worktree, runs exactly one `session/new` and one `session/prompt` through §9.36's own
protocol driver, and kills the process when the column lands. It is `startEphemeralRacer`
(persistent.go), a sibling of `spawnSeat` reusing `Cursor.Open`, `acpProtocol` and the counted
`startRPCSession` spawn — no second ACP implementation exists to drift, and where the client
was welded to the room seat's lifecycle the seam extracted was placement, not protocol.

- **The room's conversation is untouchable by construction, not by discipline.** The race
  session opens with an empty resume id (never persisted, never resumed), registers on the
  TURN (`turnState.arenaEphemeral`) rather than in the seat-process registry — so a live
  persistent cursor seat and its racer coexist without either being mistaken for the other —
  and the throwaway session id it reports is refused by the existing arena guard before it
  can reach the saved threads or room.json.
- **Kill, never wait.** §9.33 measured this vendor's process lingering ~2.5 s after answering,
  so the racer is killed at its own finish line — before the diff is read, making the receipt
  a snapshot of a stopped attempt — and on a protocol-reported failure (an ACP server survives
  its own refusals, so no exit event would ever have come), on ctrl+c, and at room teardown.
  Its context is the turn's rather than the room's, which is the backstop on every one of
  those paths; a seat cannot be cleared mid-race at all, because `askClearSeat` refuses while
  a turn is in flight.
- **Two processes now wear one vendor id during a race**, so the eleventh amendment's
  stale-exit guard grew an attribution rule on the same liveness test it already trusts: an
  exit that arrives while the racer is alive can only be the room's idle seat dying in the
  background (forgotten, race untouched); one that arrives after the racer is dead is the
  racer's own, and must not be eaten by the guard reading a live room process as "this seat
  is fine" — that would hang the race column forever.
- **The exits keep their epistemics.** A racer that dies without its end-of-turn response
  FAILED — on this seat the turn's end is a response, so a bare exit means no answer arrived,
  and rendering it done would be the empty-success this seat's missing result line makes
  possible. A turn that ends cleanly having streamed nothing lands done with a note naming
  the ambiguity, because a silently-working racer and a broken chunk parser are identical on
  this wire (§9.36's stated loss). Token usage stays what ACP makes it: absent, never zero.
  And the containment phrase every racer carries — write posture, contained by the worktree —
  is stated at its weakest here: §9.36 measured workspace trust not applying over ACP, and an
  arena worktree is a freshly created, never-trusted directory, so nothing but the session's
  cwd scopes the attempt. The posture detail already says so; the worktree gives it more
  force, not less.

Verified offline only: fixture-driven tests (arena_cursor_test.go) pin the spawn choice, the
untouched room thread, the kill on finish / protocol failure / cancel / teardown, both
degraded exits, and the exit-echo not re-ranking the race. cursor-agent was not installed
where this was built, so ~~this amendment owes a live race on the Windows box~~ **the
amendment owed a live race; two have now run it, and what they paid is narrower than the
word "verified" would suggest.** The throwaway racer has been spawned live and rooted in
its own worktree — it streamed for 26m40s on the race the give-up amendment below records,
and was cut loose mid-race with `x` on race t9 — so the spawn, the live interim read
against its tree, and the kill path are measured. **A clean completion is still owed**: no
live race is recorded in which this seat's own `session/prompt` resolved and the racer was
killed at its own finish line with its diff read. Until then the *finishing* half of this
amendment stands on `arena_cursor_test.go` alone, and that is the honest split. *(The
2026-08-15/16 5-of-5 race cut this racer a second time — alive at ~zero CPU with its edits
complete and committed on the cut, ~11 minutes in — so the debt stands and gained a second
data point: two live races, two stalls, zero self-finishes.)*
**Amendment, 2026-08-09: every attempt survives as a commit, and `u` takes one back.** The
commit-per-turn deferral came off the list, and it brought the rollback it makes possible
(mechanics stolen with attribution: Crystal's commit-per-turn checkpoint, cc-haha's turn-level
undo). Two halves that stack:

- **Commit-per-turn.** The moment a racer lands and `collectArena` has read its diff, the
  worktree's whole state is staged and committed onto `arena/t<N>/<vendor>` — subject
  `arena t<N>: <first line of the brief>` (64-byte cap) — so every attempt is durable on its
  branch: diffable, adoptable, rollbackable, and the worktree itself becomes deletable without
  losing anything. Staging everything is correct *there and only there*: the tree contains
  nothing but the racer's own output, so the reason blanket staging is wrong in a real
  workspace does not exist in that one. The sha the column renders is exactly what
  `git rev-parse HEAD` returned, shortened for display only. On a machine with no git identity
  anywhere (CI runners, fresh boxes) the commit carries a fallback identity via per-command
  `-c` flags — never a config write, which on a worktree would land in the shared repo config,
  i.e. in the room's repo. A commit that cannot land (a stale ref lock, a signer that cannot
  run) degrades **that seat's receipt only**, as `not committed: <git's first stderr line>` —
  the diff was still read, the race and the other racers are untouched. A racer that committed
  for itself mid-turn already parked its attempt; its own tip is reported rather than papered
  over with an empty commit — and the diff still answers against the recorded base, so the
  mid-turn commit cannot hide the work.
- **The empty-commit ruling.** A zero-diff attempt commits **nothing**, and that is a ruling,
  not an omission: an empty commit would be a receipt claiming work that did not happen —
  §4a.1's false zero, mirrored into the write path. The seat renders no commit line and no
  failure either (nothing was owed); "no changes against \<base\>." stays the whole story, and
  the branch tip staying at base is the machine-readable form of the same fact.
- **Undo-the-whole-turn.** `u` on a focused arena seat, y/n-gated exactly like `c` (a stray
  keystroke must cost a `y` before it costs an attempt), runs `git reset --hard <base>` inside
  the racer worktree **only**. Branch and tree agree by construction rather than by a second
  command: the worktree has its arena branch checked out, so `--hard` moves that ref itself.
  The safety argument is an explicit path guard, not trust in recorded state: the reset runs
  only on a path equal to the arena-tree name recomputed from the room's *current* workspace,
  turn and vendor — a name that structurally cannot be the workspace itself — and anything
  else refuses before git runs. Refusals are four sentences for four facts: no race this turn;
  the attempt changed nothing (nothing to take back); already undone (pressing again is not
  more undone); and the reset itself failed, carrying git's own first stderr line. After an
  undo the stat stays on the column — the measured record of what the attempt changed — under
  an "undone" line saying the tree and branch no longer hold it.
- `u` landed on the help panel's room-controls row at its exact 114-cell budget by trading the
  word "worktrees" for it: the arena block prints the worktree path on every race, so that
  clause restated something the screen already teaches, while an undo key documented nowhere
  is a control nobody finds.

Verification note, on the same terms as the section's own: the git mechanics — the commit
landing on the branch with the racer's files, the per-seat degrade, the zero-diff skip, the
self-committed tip, the undo round-trip (files restored, created files gone, branch back at
base), the path guard, and every refusal sentence — are pinned by offline tests against real
temp repositories. ~~No live race has exercised either half yet.~~ **Commit-per-turn is
paid; the undo is not, and it is now the last unpaid item in this section.** Race t9
(Windows box, 2026-08-09) landed the claude racer's attempt as `cf69634` — subject
`arena t9: write table-driven tests for the small render helpers …`, parent `e1bf983`,
which is the base the race recorded — onto `arena/t9/claude`; that commit outlived
`/adopt`, `/arena drop` of its worktree, and a merged PR (#164), with the branch
`adopt/t9-claude-helpers` left as the receipt. **`u` has still never run against a real
racer commit.** The debt is one `/arena` against a brief that changes files, then `u` on
the finisher between turns.

**Amendment, 2026-08-09: `.worktreeinclude` — a race carries the files git ignores, when the
repo names them.** The seeding deferral came off the list, on the schedule the original note
predicted (a real race on a repo needing `.env` fails falsely on every seat at once, so the fix
is worth landing before that repo shows up). What was built, and the rulings inside it:

- **The file and the copy.** A `.worktreeinclude` at the room repo's root — gitignore-style
  patterns, one per line, `#` comments and blank lines ignored — and during `arenaSetup`, after
  each racer's worktree is added, every matching file is copied from the room repo into that
  tree, relative paths preserved, parent directories created. The grammar is a documented
  subset of gitignore's (bare names match at any depth, anchored patterns from the root, `*`
  within a segment, `**` across segments, a directory pattern takes its subtree; no negation).
- **Copy only, never execute — the half of agent-deck deliberately not taken.** agent-deck (the
  pattern source) pairs seeding with repo-carried setup scripts that run after the copy. That
  half crosses a trust boundary this project has explicitly parked: byte-level trust gating is
  on the parked list pending an audit, and a repo that can run code on the machine by merely
  containing a file is a different product with a different threat model. Copying bytes into a
  tree the room already owns is containable; execution is not.
- **Candidates are untracked files only** (`git ls-files --others`, ignored files included —
  exactly the set a fresh worktree lacks). A tracked file already arrives with the checkout,
  and seeding the room's possibly-dirty copy of one would plant the room's own edits in every
  seat's diff — a lying diff, §4a.1's class. Known limit, stated: a seeded file that is
  untracked but *not* git-ignored still surfaces through collection's `git add -N .`; name
  git-ignored files and it cannot.
- **Containment.** Patterns resolve from the repo root and matches come from git's own
  enumeration, so they structurally cannot leave it; absolute and `..`-carrying patterns are
  refused by name anyway, per pattern, so one bad line disables only itself. Symlinks are never
  followed — Windows is primary and symlink semantics differ per platform — a symlink match
  copies nothing and says so.
- **The budget: 64 MiB per seat** (`seedBudgetBytes`). Exists for the node_modules pattern — an
  over-broad line must fail loud and named, not hang the room copying a dependency tree into
  four worktrees. Over-budget is refused wholesale (copying *some* of the file's list would
  hand every seat a tree that half-works), with the measured total in the sentence, and the
  budget is enforced again on actual bytes during the copy, because files grow between stat and
  copy.
- **Honesty in the column.** "seeded 3 files" is the count actually copied into that seat's
  tree; no `.worktreeinclude` means no line at all (zero and absent stay two facts). A pattern
  that matches nothing is a named notice, not silence — an allowlist-shaped file fails both
  ways — and not a failure either. A copy error degrades that one seat with the path and the
  error's first line, through the same per-seat lane a failed worktree add uses; the race runs
  on, and the half-seeded worktree stays on disk (kept-until-deleted receipts include the
  broken ones).

Verification note, same shape as this section's original one: the mechanics — the copy into
each racer tree, nested parents, every named refusal, the budget, the per-seat degrade channel,
zero-vs-absent on the seed line — are pinned by offline tests against real temp repositories.
**No live race on a repo that actually needs a `.env` has run yet; that run is the debt this
amendment carries**, and it is the same debt the original note carried for the core, paid the
same way.

**Amendment, 2026-08-09: the end of a worktree's life — `/adopt` and `/arena drop`.** The
deferred deletion guard came off the list, and adoption moved from a printed suggestion to a
typed room command; both are §9.17 verbs, reachable mid-session with no flag and no relaunch
(`lifecycle.go`). The shapes borrow from the two products that had already worked this seam —
claude-squad's adopt, Pane's deletion guard — with one deliberate fork each:

- **`/adopt <seat>` merges; it does not check ~~out~~ the racer's branch out.** claude-squad's
  adopt is a checkout of the
  attempt's branch over the user's — which moves HEAD, rewrites the tree wholesale, and leaves
  the user's own branch behind. That is more state than the act requires, and §9.37's whole
  posture is offer-never-take, so council does the least-magic git operation that lands the
  work: `git merge --no-ff arena/t<N>/<seat>` in the room's repo, ~~on the branch the user is
  already standing on~~ **on a fresh branch council cuts for it — ruled 2026-08-11, the last
  block in this section; the merge itself is unchanged**. `--no-ff` keeps the adoption a visible event in history — the merge
  commit is the receipt saying where the work came from. Because arena seats leave their work
  uncommitted (commit-per-turn is still deferred), a dirty attempt is first committed in its
  OWN worktree, on its OWN arena branch, under the user's own git config — and the y/n card
  says so, naming the exact command(s) y will run, the flow write gate's contract. Hard
  precondition, refused by name with the path count: the room tree must be CLEAN
  (`git status --porcelain` empty) — a merge writes into that tree, and adopt must never eat
  the user's uncommitted work. A racer that changed nothing refuses (an empty merge commit
  would claim work that does not exist); a merge that conflicts is `git merge --abort`ed —
  tree restored, attempt intact on its branch — and the notice hands the merge to a human. A
  merge that failed before starting reports the tree as untouched instead: the two endings
  are different facts (§4a.1). Posture is not consulted: read/write governs the seats, and an
  adopt runs on the user's own y, the same footing as /cd.
- **`/arena drop <seat>` (or `all`) deletes tree + branch, guarded; the force is a spelling,
  not a keystroke.** Two guards, each refusing with exactly what would be lost and the way
  forward: a worktree holding uncommitted changes (counted), and an arena branch holding
  commits the room's HEAD cannot reach (counted, with `/adopt <seat>` offered beside the
  force). The force form is a trailing bang — `/arena drop codex!` — re-run by the user,
  chosen over a second y/n on purpose: y is one keystroke answered against a notice half-read,
  while the bang travels in the command, records that destruction was asked for, and cannot be
  produced by a stray key. (`/adopt` keeps y/n because its act is additive and revertible;
  drop orphans work.) Mechanics are `git worktree remove` (git's own `--force` only when the
  user spelled it) then `branch -D`, argv via gitOut — and the path check is mechanical: a
  tree is only ever removed if it re-derives, from the recorded race's own workspace/turn/seat
  through the same `arenaTree` that minted it, to exactly the recorded path. A receipt entry
  that fails that check is refused even under force. `drop all` degrades per seat rather than
  refusing wholesale: clean trees go, survivors are named with their reasons.
- **The target is the RACE'S receipt, not the column.** `Column.Arena` is a per-turn fact the
  next dispatch clears; the worktrees are kept until deleted. So dispatch records the race —
  workspace, turn, base, each racer's tree — on the model (`arenaRace`), in memory only:
  room.json stays keys-and-numbers, and a room reopened after a quit finishes the lifecycle by
  hand with the same git commands, against worktrees that are visible siblings precisely so no
  session state is needed to find them. Grammar note: only the exact two-word form
  `/arena drop <seat>[!]` is the verb; anything longer after `/arena` is a brief and races as
  prose, the roomcmd vocabulary rule applied inside the one command that takes free text.

The help panel's room-commands row is at its width budget and does not name the two verbs;
they are taught by the slash refusal (which lists `/adopt` in the live table), by bare
`/adopt` and bare `/arena drop` answering with usage, and by every guard refusal naming its
remedy. Verification, ~~owed~~ **paid 2026-08-09**: the git
mechanics — merge, commit-then-merge, conflict abort, both guards, the force, the path check,
`drop all`'s partial degrade — are pinned by offline tests against real temp repositories
(`lifecycle_test.go`), and ~~no live adopt has run on the Windows box~~ **race t9's winner
went through `/adopt` and then `/arena drop` on the Windows box**, which is why no
`arena/t9/*` branch and no `telltale-arena-t9-*` sibling exist on it while every earlier
race's leftovers sit exactly where they were left. What that adoption then cost in hand-run
git is the open question at the end of this section, filed rather than ruled. Two guard
paths a SUCCESSFUL adopt cannot reach still rest on `lifecycle_test.go` alone: a merge that
conflicts, and a drop refused for unmerged commits.

**Amendment, 2026-08-09: the race numbers itself off the refs, and a failed race says why.**
A live `/arena` at turn 3 (Windows box, real room) failed on all four seats, and each column's
whole explanation was `arena: Preparing worktree (new branch 'arena/t3/<vendor>')`. Two
measured defects, one incident — the second is what made the first expensive:

- **The collision.** Kept-until-deleted cuts both ways: arena branches and worktrees outlive
  the room, but the turn counter — and the in-memory race receipt `/arena drop` needs
  (`Model.lastRace`) — reset with every launch. So a fresh room's turn 3 minted the exact
  names an older room's turn 3 had already parked, `git worktree add -b` refused every seat,
  and drop could not reach the old trees because their receipt had died with the old room. The
  only remedy was hand-run git. The fix reads instead of guessing: at setup the race number is
  `arenaRaceNumber` — one past the highest N among the repo's existing `arena/t<N>/...`
  branches (`git for-each-ref` over `refs/heads/arena/`, argv via gitOut), floored at the turn
  number, so a repo with no leftovers keeps racing as `t<turn>`. The refs are the one record
  that shares the leftovers' lifetime, which is what qualifies them to number the race; a scan
  that cannot run degrades to the turn-number floor with the race running, because a broken
  for-each-ref must not brick `/arena`. The number is recorded once
  (`turnState.arenaRaceN`, `arenaRace.raceN`, `ArenaResult.RaceN`) and EVERYTHING that mints
  or re-derives a name reads it — the branch on the receipt, the `arena t<N>:` commit subject,
  `/adopt` and `/arena drop`'s re-derivations, undo's path guard — because the turn and the
  race now legitimately disagree, and one call site still reading `Column.TurnN` would aim a
  verb at names the race never created. The arena block's render is untouched: it already
  shows the branch name, which carries the (now honest) `t<N>`.
- **The lie about the collision.** gitOut surfaced the FIRST stderr line of a failed command,
  and `worktree add` prints progress chatter ("Preparing worktree ...") before its
  `fatal: a branch named '...' already exists` — so the column showed the narration and
  swallowed the diagnosis. gitOut now prefers the first line git itself marks as the problem
  (`fatal:` / `error:`), falling back to the first non-empty line only when no marked line
  exists (some refusals print bare prose). git's own prefixes are the measured marker of
  which line is the error; the old rule displayed the nearest string to the failure instead of
  the failure. If a residual collision still happens despite the scan — a sibling directory an
  old room left with no branch to be scanned, a ref minted between scan and add — the seat's
  error now carries the fatal line plus the named remedy (`git worktree remove` /
  `git branch -D`), since those leftovers are exactly the state no receipt can reach.

Both fixes are pinned by offline tests against real temp repositories: the two-line-stderr
collision fixture (the live transcript, replayed), renumbering past stale `t3` branches with
every seat racing clean, the turn-number floor on a failed scan, the residual-collision
sentence, and adopt/drop/undo driven end to end against a race whose number outran its turn —
leftovers untouched throughout. ~~The live re-race is owed~~ **The live re-race ran, and has
kept running ever since**: every race after the fix has been numbered over a growing pile of
leftovers — 27 `arena/t<N>/<vendor>` branches and 28 sibling worktrees, t2 through t8, are
still on the reference box — and race t9 raced all four seats clean over them, which is the
claim. One consequence worth knowing before the next race: t9's branches were dropped, so
the highest surviving `arena/t<N>` is t8 and the scan will mint `t9` again.

**Amendment, 2026-08-09: `x` gives up on one racing seat, and the race runs on.** The second
live `/arena` (same day, Windows box) measured the gap: four seats raced, three landed
(7m51s / 26m28s / 5m07s), and the fourth — the cursor throwaway ACP racer — streamed for
**26m40s** with the live stat honestly reading "no changes yet against \<base\>" the whole
time. The operator sat ~20 minutes after the race was effectively decided, because one stuck
racer holds the WHOLE turn hostage: ctrl+c is the only exit and it cancels everything. The
room displayed the truth and offered no per-seat act on it. The act built:

- **`x` on a focused, still-racing arena seat, mid-turn** — the one per-seat key that runs
  while a turn is in flight, because mid-flight is the only time it means anything.
  y/n-gated exactly like `c` and `u` (a stray keystroke must cost a y before it costs a
  process), and the question names the vendor and what y does. On y, the room kills THAT
  racer only — the ephemeral ACP session when one is racing, else that vendor's one-shot
  process through `turnState.arenaHandles`, the per-vendor record dispatch's arena branch now
  keeps beside the flat `handles` list (the flat list stays: cancel and teardown are
  all-or-nothing acts and never address a single process; the give-up is the first act that
  does). The kill lands on the racer's side of the two-processes-one-vendor-id split — the
  room's idle seat behind the same id survives, per applyEvents' existing attribution rule.
- **A given-up seat lands like any other finisher, wearing the honest phase.** The stream
  tail is flushed, the elapsed stamped, the note says "given up after \<elapsed\> — anything
  it wrote is in the diff", and the column retires through `finishColumn` with the CANCELLED
  phase — the same phase and render ctrl+c's cancel produces ("cancelled — the output above
  is partial" is that path's wording; this one names the give-up instead). Everything the
  finish line already does happens unchanged: the racer dies before the diff is read (the
  receipt is a snapshot of a stopped attempt), a dirty tree commits its receipt onto the
  arena branch, a clean tree stays a measured zero with no commit, the rank is stamped in
  host-observed landing order — a DNF finished too, and the render welds the rank to the
  phase word so "4th · cancelled" cannot read as a result — and the interim stat clears. The
  seat leaves the turn's live set through the same drain every landing uses, so **the turn
  ends when the remaining seats land** — which is the whole point.
- **Three refusals, three sentences** (the undo key's rule): no turn in flight; an ordinary
  turn — its seats share one fate by design, this key is arena-only and **ctrl+c remains the
  whole-turn act**, said in the refusal; and a seat that already landed (its result is
  settled — a y arriving after the seat lands under the question refuses the same way,
  killing nothing and re-ranking nothing). The help panel's room-controls row is at its
  exact 114-cell budget and does not name the key; it is taught by these refusals and by
  this amendment, the way `/adopt` and `/arena drop` are taught by theirs.

Verified offline only, the section's standing debt shape: real-temp-repo plus fake-session
tests (giveup_test.go) pin the ephemeral kill and the cancelled landing with rank and
committed receipt, the keyed one-shot kill with every other racer's handle surviving, the
room process surviving its racer's give-up and the exit echo landing inert, the turn ending
when the remaining seats land, all three refusals, the y/n/stray gate, and compose leaving
`x` a letter. ~~A live give-up on the Windows box is owed~~ **Paid on race t9** (Windows box,
2026-08-09): the cursor throwaway racer stalled, was cut loose mid-race with `x`, and the
turn ended when the remaining seats landed — which is the whole claim, measured. The three
refusals and the y/n/stray gate stay offline-pinned, and always will be: a live race has no
way to exercise a refusal it never trips.

**Amendment, 2026-08-09: the brief carries the conduct line — the one place the room adds
words.** A write-posture racer's confinement is its worktree, but the machine's git and gh
credentials are ambient, so a racer can reach GitHub — and one did: the codex seat took the
t5 gofmt brief, pushed its arena branch, opened a PR, waited out CI, and merged it into main,
then announced the same plan on the very next race. This section's founding ruling — the
room offers the diffs and the human adopts, never an auto-adoption — binds this codebase and
cannot bind a vendor that runs `gh pr merge` on its own initiative. The operator ruled the
same day: every `/arena` dispatch now prepends `arenaConduct` (arena.go) to the brief —
*"This tree is a race attempt in its own git worktree. Do not push, open pull requests, or
merge — the operator compares the attempts and adopts the winner. Do the work, verify it
locally, and stop."*

The bend to the brief-verbatim promise is bounded three ways, each pinned by test: races
only (an ordinary turn's prompt is byte-verbatim), a constant — the same published line for
every racer on every race, prepended so a long brief cannot bury it, leaving the cross-seat
comparison undisturbed — and recorded here rather than discoverable only in a wire capture.
Stated honestly for what it is: an instruction, not a control. A vendor can ignore it, and
the mechanical version of this boundary — credentials a racer cannot reach — is a different,
harder change that this amendment deliberately does not claim. ~~The live measurement owed:
the next race showing the codex seat stopping at its commit.~~ **Measured on race t9**: the
codex seat raced under the preamble and ended with "nothing pushed." One race is one race —
an instruction a vendor obeyed once is still an instruction, so the sentence above stands
exactly as written and this measurement does not promote it to a control.

**Open question, 2026-08-09 — RULED 2026-08-11, option (b): where should an adoption land?**
Filed from the first live
`/adopt` rather than decided, because the answer depends on a convention the room cannot see.
`/adopt` merges into the room repo's current branch — for most workspaces, local `main` — and
that is the smallest honest act the verb can perform. But an operator whose repos are run
branch→PR (this project's own convention, and this operator's standing rule across every
machine) then holds a merge commit on a local `main` that must never be pushed as-is; the
first live adoption ended with four hand-run git commands turning the merge into a PR branch
and resetting `main` back to origin. Options, ~~none ruled on~~ **(b) ruled, 2026-08-11 — the
block below**: (a) keep the current shape and
document the hand-off (an adoption is a local act; publishing is the operator's, as it
already is for every other commit); (b) `/adopt` onto a NEW branch cut from the room's HEAD
(`adopt/t<N>-<vendor>`?), never touching the current branch — closer to branch→PR, but the
room minting branch names in the operator's repo is a bigger footprint than one merge; (c) a
flag or second verb for each. The founding posture — offer, never take — leans (b) no further
than it leans (a): both are one revertible act on the operator's own y. Whoever picks this up
starts from the measured friction: four commands, once per adoption, only on branch→PR repos.

**Ruling, 2026-08-11: an adoption lands on a fresh branch, never on the branch the workspace
has checked out.** The owner ruled option (b), and the reason is the convention the room
could not see: the owner's workflow is branch-then-PR on every repo and every machine, and a
commit straight to `main` is never made. The room's own posture does not decide this — both
options are one revertible act on the operator's y — so the measured friction decides it, and
the friction is four hand-run git commands per adoption on every branch→PR repo. A fresh
branch turns that hand-off into one `gh pr create`. What was built:

- **The name is `adopt/t<N>-<vendor>`, cut from the room's current HEAD and checked out.** The
  race number, never the turn, like every other arena name (the renumbering amendment above).
  The seat is joined by a dash rather than a slash so `arena/t9/claude` and `adopt/t9-claude`
  differ in the last segment, which is where a reader of `git branch` looks. `--no-ff` and the
  commit-then-merge order are untouched: what moved is WHERE the merge lands, not what it does.
- **A collision takes the next free suffix** (`-2`, `-3`, … to 50, then a named refusal). The
  collision is ordinary rather than exotic: race numbers repeat once a race's branches are
  dropped, since the scan that numbers a race reads `refs/heads/arena/` alone, and an operator
  can adopt, revert and adopt again. Reusing the name would either fail the checkout or land
  the merge on an older adoption's branch, where the PR would carry work nobody asked about.
  The free name is resolved WHEN THE CARD ARMS and carried to the `y`, because a card that
  named `adopt/t9-claude` and then cut `adopt/t9-claude-2` would be the card describing
  something other than what it ran — the one contract this gate exists to keep. One
  `for-each-ref` over the adopt namespace answers every candidate at once and separates "no
  such branch" from "git could not answer"; a scan that cannot run degrades to the plain name
  with the adoption still running, and `git checkout -b` reports the collision with git's own
  fatal line.
- **A failed adoption leaves nothing behind.** The branch is cut for a merge, so a merge that
  conflicts or refuses ends with the branch deleted and the room back on the branch it came
  from — an empty branch handed over as the receipt of a failure would be the verb charging
  for its own failure. The two failure endings stay two facts (§4a.1): a conflict aborts and
  says a human merge is needed, a merge that never started says the tree is untouched, and
  both now also say where the room stands. A restore that cannot finish says THAT instead,
  naming the branch the room is left on and the command that puts it back.
- **The alternative reading, recorded rather than taken**: leave the room standing on the
  fresh branch after a conflict, so the human merge happens there. It is defensible — that is
  where the resolution belongs — and it was not chosen, because the ruling covers where a
  SUCCESSFUL adoption lands and a failure quietly moving the operator to a new branch is a
  state change nobody asked for. If a live conflict makes the restore feel wrong, this is the
  line to amend.
- **Existing refusals are untouched**, and so are their tests: the dirty-room gate with its
  untracked-bystander rule, the zero-change refusal, the mid-turn refusal, and `/arena drop`'s
  unmerged-commit guard, which reads the room's HEAD and therefore counts zero as soon as the
  adopt branch holds the merge.

Verification, on this section's own terms: the mechanics — the branch cut and checked out, the
room's own branch not moving, the suffix on a taken name, the conflict restore, and the notice
naming the branch and `gh pr create` — are pinned by offline tests against real temp
repositories (`lifecycle_test.go`). **Paid 2026-08-14** by race t14 on the reference box: the
card named the branch and the merge before the `y`, `adopt/t14-claude` was cut and checked
out, `git merge --no-ff arena/t14/claude` landed as `91c5f3e`, `main` did not move, and the
notice named `gh pr create`. The push half was deliberately not run — the racer's payload was
a throwaway date comment, and opening a PR to prove a verb works would put noise in the
repository to record that the repository is fine.

#### A warm seat's racer could not finish, 2026-08-13 — measured, then fixed

**A race against a seat the room had already used never ended.** Race t10 on the reference
box: one Claude racer, a one-line edit, and the room rendered `streaming` for **21 minutes**
after the racer had exited. The vendor was not slow and did not fail. Its transcript ends at
52 seconds with a complete reply, and by then no process wearing that vendor id was left alive
except the room's own persistent seat. Nothing downstream ran — no diff, no commit, no rank,
no seed receipt — because all of them live inside `finishColumn`, and `finishColumn` was never
called.

**Both of the column's exits were closed at once, which is why nothing caught it.** A one-shot
racer ends its turn by exiting, so `KindDone` is its only retirement signal; the two earlier
paths do not apply to it, and each declines for its own correct reason. `arenaEphemeral` is
populated only for a `Conversational` seat, so `ephemeralRacer` is nil for this vendor. The
arena spawn never sets `turnState.persistent`, correctly — the racer *is* a spawn — so
`isPersistent` is false. That leaves `KindDone`, and `KindDone` reaches the stale-exit guard
first: a terminal event names a **vendor**, not a process, so a live entry in `m.procs` reads
as "this seat is fine" and the exit is discarded as a predecessor's. The room's persistent
Claude seat is exactly such an entry.

**The failure mode was already written down, one path over.** `KindDone`'s own attribution
comment names it for the ACP racer — a guard "reading a live ROOM process as *this seat is
fine* would leave the race column streaming forever and the turn unable to end" — and fixes it
with the `ephemeralRacer` check. `giveUpSeat`'s comment then observes that the guard eats a
racer's exit when a room process wears the id, and judges it harmless. For `giveUpSeat` that
judgement is right: a given-up column is already terminal when the exit lands. On the ordinary
path the column is not, and the same swallow is the difference between a race that finishes
and one that cannot. Two processes wear one vendor id for every racing seat, not only for the
one whose racer happens to be a live session.

**The trigger is a warm seat, and it is why this survived several clean races.** `m.procs`
must already hold a live process for that vendor when the race dispatches. Race before the
room's first ordinary brief and the guard never fires, which is what t9-on-2026-08-09 did when
all four seats raced and a winner was adopted. The drive that found this sent two ordinary
briefs first, so the seat was warm. It also means codex, agy and grok racers were never
affected: none of those vendors holds a persistent process, so their exits pass the guard
untouched. The bug reached exactly one seat, and it is the seat the room dispatches to by
default.

**The fix is attribution, not a hole in the guard.** `arenaRacing` is the one-shot sibling of
`ephemeralRacer` — keyed presence in `arenaHandles`, because a handle is not a session and
cannot be asked whether it is alive, which is precisely the case at hand: the process has
already exited and the map is the only record of whose exit it was. A racing vendor's
`KindDone` retires its column; a vendor that is not racing keeps the stale-exit guard exactly
as it was. `dropProcess` is deliberately not called on that path — the exit belongs to the
racer, the room's own seat is still running, and forgetting a live process would leave it
running and invisible, which is the state this product refuses. `TestAWarmSeatsRacerRetires-
OnItsOwnExit` was verified to fail without the change, reproducing the hang as
`phase = streaming`; its sibling pins that a non-racing vendor's predecessor exit is still
discarded.

**Verified live the same day.** Race t13, one Claude seat, first dispatch of a cold room:
the column retired in **18 seconds** and drew the whole settled block — `arena
arena/t13/claude`, `seeded 1 file`, `no untracked file matches ".env"`, `1st of 1 · done ·
18s`, `committed 1ef0f00.` and the diff stat. Eighteen seconds against twenty-one minutes is
the measurement. Race t14 then landed the same way with a warm seat behind it, which is the
arm that matters: it is the state the bug needed.

**And it paid the three debts that had been stuck behind it.** `u` on t13 reset the branch to
`ba2d00b` with the stat kept above an `undone` line, and a second press refused as already
undone; the reset was confirmed against git rather than against the room's own claim.
`/adopt claude` on t14 cut `adopt/t14-claude`, ran the `--no-ff` merge, left `main` at
`ba2d00b` and named `gh pr create` — the first live adoption under the shape ruled
2026-08-11, and the debt this section states three paragraphs above its own dated block is
now paid.

### 9.38 paste lands whole, and never sends (2026-08-09)

The ask, in the operator's words: *"how i can paste things into the area i can type in."* The
answer required measuring what a paste even was in this room, because the two obvious guesses —
it works, or it fires a send per pasted line — were both wrong.

**What was measured about today's behaviour.** All of it read off the pinned module source, not
vendor docs. bubbletea v2.0.8 enables bracketed paste unless a view opts out
(`cursed_renderer.go` writes `SetModeBracketedPaste`; council's `View()` never sets
`DisableBracketedPasteMode`), and ultraviolet's terminal reader buffers everything between the
paste markers into ONE `PasteEvent` — a newline inside the paste lands as `\n` in its content,
never as an Enter keypress, and the win32-input-mode encoding Windows Terminal uses is decoded
into the same buffer (`terminal_reader.go`, ultraviolet pinned at v0.0.0-20260703014108). So in
a bracketed-paste terminal a paste could never have fired a send. What it did instead was
NOTHING: council's `Update` had no `PasteMsg` case, the message fell through the type switch,
and the clipboard's offer was silently discarded. The composer never learned a paste happened.

The fires-sends failure is real on exactly one path: a terminal with NO bracketed paste replays
a paste as keystrokes, each pasted newline arrives as an Enter keypress, and compose mode's
enter dispatches — a five-line paste is up to five turns, each to live vendor CLIs. Council
cannot distinguish that replay from typing without a timing heuristic, which would be inferred
behaviour, and this product does not ship inferred behaviour (§4a.1). So that path is left as
it is and named here instead: the text chunks are flattened safely by `sanitizeKeepingSpace`,
the enters are enters, and the fix on such a terminal is the terminal. Windows Terminal — the
reference environment — brackets its pastes.

**What was built** (`paste.go`): the room's half of the contract the runtime already offers.

- **One `PasteMsg` case in `Update`.** The content goes into the draft and nowhere else — a
  paste never dispatches, never answers a gate, never quits. Enter, a keystroke from a person,
  remains the only way a brief leaves the room. Pasted control characters cannot act: a pasted
  `\x03` is not ctrl+c, a pasted `q` is the letter q; controls without width are dropped.
- **The multiline ruling: newlines are PRESERVED, raw.** The composer has been a block since
  ctrl+j existed (`State.Draft` may hold newlines; `wrap()` honours them; the compose area
  grows to `maxComposerRows` and elides with "N more above"), so there is no single-line
  prompt to protect and no need for a `⏎` display glyph — a pasted paragraph renders as the
  rows it is, in both glyph sets, and dispatch hands the vendors the draft with its real
  newlines. The string on screen is the string sent (§7.14). CRLF collapses to `\n` (the
  Windows clipboard's line ending; splitting it would gift every line a trailing space). The
  one lossy rewrite is stated rather than hidden: a tab becomes one space, because a cell grid
  cannot budget a tab and a guessed tab stop would be fidelity theatre.
- **A cap with a named refusal.** `maxPasteRunes` (8,192, over draft-plus-paste) refuses
  atomically — nothing lands, not a truncated prefix — and the notice carries both numbers and
  the remedy: *"paste refused: 20481 chars against the composer's 8192 — put long text in a
  file and name the path in the brief."* The number is anchored to the narrowest pipe a brief
  must fit through (the Antigravity seat's prompt rides argv; Windows caps a command line at
  32,767 UTF-16 units; 8,192 runes is at most half that even all-surrogate-pair) and to the
  point where a footer composer that deletes rune-by-rune stops being an editor.
- **A paste from view mode inserts and opens compose.** A paste is not a keystroke — view
  mode's letters are commands because they are keys; pasted text can only be material, and the
  only place material goes is the draft. The mode line states the switch on the next frame.
  The exception is a pending y/n (tool gate, `c`, `/write`, a flow write hop): the paste is
  refused by name and the question stays exactly where it was — nothing about a pending
  request happens implicitly.

No golden changed and none was added: a pasted draft produces the same `State` shape ctrl+j
already produces, and a golden that did not change is the claim that the room's appearance did
not either. The tests (`paste_test.go`) drive `Update` with the real message shape and assert
the observables the flow security tests trust — spawn count, draft, pending flags — end to end
through enter, which must deliver the pasted newlines to the seat intact.

**The live verification owed.** No test in this container can observe Windows Terminal
bracketing a paste — that is the terminal's half of the contract. The check, one minute at the
real machine: open `telltale council` in Windows Terminal, copy a three-line snippet, paste
into the room. Expected: one insertion, three rows in the composer, zero dispatches; then enter
sends it as one brief. If the paste instead lands as separate turns, the terminal did not
bracket it — record the terminal build in PARITY.md, because that is a measured vendor fact,
not a council bug.

**Paid, 2026-08-13, on Windows Terminal 1.24.11911.0.** A three-line snippet pasted into a
live room landed as ONE insertion, three composer rows, and **zero dispatches**; `ctrl+u` then
cleared it and reported the count, which is the second half of the same gesture and is why the
clear is evidence too — a paste that had dispatched would have left nothing to clear. The
terminal build is named because the bracketing is the terminal's half of the contract and a
version is the only thing that claim can be pinned to; PARITY.md stays out of it, since that
file records a machine BEHAVING DIFFERENTLY and this machine behaved as specified. **One half
of the check is still unexercised**, stated rather than rounded up: the draft was cleared
instead of sent, so "enter sends it as one brief" has not been observed on a pasted multi-line
draft. The property that was owed — a paste never sends — is the one that was measured.

**The other half paid, 2026-08-15/16, and it found a render bug on the way.** A three-line
brief was pasted and SENT with enter in a live 5/5 room: ONE dispatch, the turn counter
moved by one, and the newlines reached the seat as bytes — verified in the vendor's own
session transcript (`now.\nThen add a second one…`), not read off the wrapped column echo,
because the echo's row breaks are ambiguous between newlines and word wrap. **What did not
hold was the composer render: the three-line draft drew as ONE row before enter.** The
2026-08-13 check drew three rows in a one-seat room on the same terminal build
(1.24.11911.0), so the suspect is the composer's height behavior under a full seat strip,
not the paste path — the wire is proven right and the drawing is proven wrong, which is
the exact split this section exists to keep. The render defect is recorded as an unowned
gap in STATE.md; this section's claim is amended to say the paste property holds ON THE
WIRE, with the row rendering owned separately.

**Amendment, 2026-08-09 — the ergonomic other half: ctrl+u clears the draft.** Paste changed
the arithmetic on regret. A draft used to cost at most a typed sentence, so backspace's
rune-at-a-time delete was proportionate to any mistake the composer could hold; one wrong
paste is now up to 8,192 runes in a single gesture, and 8,192 backspaces is not an editor.
`ctrl+u` — readline's own kill-line — empties the composer in one keystroke, in compose mode
only. A chord on purpose: `sanitizePaste` drops every control character, so no paste can
carry the key into the room, and no stray letter can fire it — the one gesture that can empty
a draft is a deliberate hand on ctrl, the same argument that keeps a pasted `\x03` from
cancelling. No y/n gate, unlike `c` and `u`, whose confirms price drops nothing can reverse:
a cleared draft's ways back are ordinary (the clipboard still holds a paste; a sentence
re-types), so the loss is *stated* instead of priced — the notice carries the measured rune
count of the string just dropped ("draft cleared — 1204 chars"), in the paste refusal's own
unit and spelling, never an estimate (§4a.1). An empty draft clears silently — backspace's
own empty-draft behaviour applied at size: after the press the state the key promises is
already on screen, and an every-press "nothing to clear" would put noise where dispatch
answers land. The key sits below every pending gate in `key()`'s routing, so a stray ctrl+u
under a y/n gets that gate's standing stray-key answer (cancel, or the question restated) and
never reaches the draft; in view mode it does nothing at all, because esc parked the draft
there under the promise "keeping the draft", and a chord that revoked it from the other mode
would make esc unsafe in hindsight. Nothing but the draft moves — the routing indicator falls
with it only because it describes it. Taught on the help panel's compose-keys row
(`ctrl+j/u/esc`, landed inside that row's 114-cell budget; the words "compose" and "the
draft" paid), deliberately not on the compose mode line, which is at its own width budget.
Tests: `draftclear_test.go` drives `Update` with the real chord through every gate and both
modes.

### 9.39 a fifth seat, and the first that reports money (2026-08-09)

The ask, in the operator's words: *"grok 30 dollar subscription paid for. create a seat for
grok in council."* The seat is built and the invocation is verified end to end; what follows is
what had to be measured to earn each claim on the column, including two flags that were refuted
and one hazard the seat ships with because nothing in the CLI can close it.

**The vendor.** grok 1.0.0 (3cd0d0cbce), signed in against grok.com — the subscription, not an
API key — with `grok models` reporting one model, `grok-4.5`. It is a spawn-per-turn batch
program like Codex and Antigravity, not a live process like the Cursor seat: `-p/--single`
takes a prompt, answers, and exits. So it implements `Vendor` and neither `Persistent` nor
`Conversational`, and its column takes the same spawn-per-turn shape those two already have.

**The invocation, and what it deliberately does not carry.** `--output-format streaming-json`
and nothing else, with the prompt as the value of a trailing `-p`. argv rather than stdin, and
unlike Codex that is not a preference — there is no `-` sentinel and no stdin channel for a
prompt at all. It is safe for the reason the Antigravity seat's argv transport is safe: the
installer drops a native `grok.exe`, not a `.cmd`, so `classify()` never reaches the shim
refusal and no `cmd.exe` ever sees a brief.

Two flags whose names promise containment were probed, and NEITHER is passed:

- **`--permission-mode plan` was refuted, with the write landing.** Asked to create a file under
  it, the seat called its `write` tool, reported the call `completed`, said so in prose, and the
  file was on disk afterwards. The control run without the flag wrote its file too, via
  `search_replace`. The only difference observed between the arms was which write tool the model
  picked. This is the Antigravity ledger a second time (ADR-008, seventeenth amendment) and it
  lands the same way.
- **`--sandbox` is worse than refuted — it is unobservable.** `grok --sandbox bogus-profile-xyz
  -p "hi"` does not error, does not warn, and answers normally with exit 0. A flag that silently
  accepts a profile name that cannot exist gives council no way to tell a real profile from a
  typo, so asking for one would put a word in the badge backed by a value the CLI may never have
  read. Feeding a vendor a deliberately INVALID value, rather than a plausible one, is what
  turned "unverified" into "unobservable" here, and it is the cheapest probe in this document.

So the badge is `unsandboxed`, and its detail says both of those things in the vendor's own
terms. Also deliberately absent: `--always-approve` and `--permission-mode dontAsk /
bypassPermissions`. The default headless mode already writes without asking — measured, above —
so an approve-everything flag would buy nothing in exchange for the badge cost ADR-008's fifth
and seventh amendments attach to that whole class.

**The first seat that reports money, and why that is the honesty rule working rather than
bending.** grok's `end` event carries a `total_cost_usd` it computed itself (`0.0407676` on the
first captured turn, with a `modelUsage` breakdown beside it). Codex and Antigravity report
token counts and no dollar figure, which is why both adapters leave `CostUSD` nil forever — a
cost derived from tokens and a remembered price is exactly the invented number section 4a.1
forbids. The constraint was never "council does not show cost"; it was "council does not INVENT
cost". This figure is read, so it passes through untouched, and
`TestGrokEndCarriesThreadAndTheVendorsOwnCost` asserts the exact captured value so that any
future rounding or unit conversion fails there.

The pointer matters and is not defensive: a captured turn ends with no `usage` and no cost keys
at all (see the slash hazard below), so "reported nothing" and "reported zero" are both real
states of this field on this vendor. `TestGrokAbsentCostStaysAbsent` pins it.

**Streaming, and the one judgement call.** The deltas are genuinely token-level — `"I'll"`,
`" read"`, `"notes"`, `".txt"`. That is finer than the ~80-character chunks section 9.7 flagged
as overstating "tokens" on the Claude seat, and finer than the ~95-character ACP chunks section
9.36 measured on the Cursor seat, so this column carries `GranTokens` on the strongest evidence
in the room.

The judgement call is `thought`, which is dropped. It is the model reasoning rather than
answering — 46 lines against 14 of `text` on the first capture, opening "The user wants me to
read notes.txt" — and routing it to the column would put private deliberation where the answer
goes, in a room built to compare answers. It is the line `codex.go` already draws when it
excludes `reasoning` items. The cost is stated rather than hidden: a turn that thinks for a long
time before speaking shows an empty column while it thinks.

**A hazard that ships, because no invocation closes it.** A brief whose first non-space
character is `/` is eaten by grok's own slash-command parser and never reaches the model. The
turn is not an error — `available_commands`, then an `end` with no usage, no cost and no text,
exit 0. On screen: a column that finishes instantly with nothing in it.

Three channels were tried and all three were eaten: `-p "/context"`, `--verbatim -p "/context"`
(whose help text reads "Send the prompt exactly as given"), and `--prompt-json` with the text as
a content block. The third had a CONTROL — the same `--prompt-json` invocation with a non-slash
prompt answered normally — which is what makes this a property of the parser rather than a guess
about a flag that might not work.

The room mostly protects this seat already, and by accident rather than by design: section 9.31
refuses to dispatch any draft whose first character is a slash, so nothing spawns and nothing is
billed. What does NOT hold is that refusal's documented escape hatch. A user who genuinely means
a leading slash types one leading SPACE, and the space survives the composer, the parse and the
dispatch untouched — and then grok trims it and eats the slash anyway (measured). So the escape
hatch reaches four seats and not the fifth.

Nothing is rewritten to compensate, and that is the decision rather than an omission. Editing a
brief on the way to ONE vendor would make five columns answer different questions while the room
claims they answered one, and the room's whole premise is that the seats got the same brief. A
blank column is the lesser failure, and it is documented in the file whose column shows the
symptom.

**The hue argument section 9.28 said would be owed.** That section closed by saying a fifth
vendor would have to argue for its colour, and here is the argument. Grok is `14`, bright cyan —
the twin of Codex's `6`, so section 9.28's honest weakness is now TWO twinned pairs rather than
one. That is forced, not chosen: after 4/5/6/12 the legal set holds only 13 and 14, both twins
of a seat already seated. The only real decision was which seat to pair with, and it went to
Codex because silence routes to Claude alone — the Claude column is on screen in nearly every
room, so keeping magenta unshared protects the seat a reader sees most. `CX` versus `GR` carries
the distinction when a scheme renders 6 and 14 close, which is what the two-letter tags are for.

Worth stating plainly for whoever adds a sixth: **the legal set is now full.** 13 is the last
free index, and after it a new seat cannot have a hue of its own without taking a severity
(making a seat read as failed) or abandoning 4-bit indices (council asserting a colour over the
user's own scheme). `TestSeatHuesAreExhaustive` fails with that sentence in it. The tag is what
scales; the hue was always going to run out.

**What is verified, and what is not.** The parser is pinned against captured lines, and the
INVOCATION is verified separately by `grok_live_test.go` (`-tags=live`) — because unit tests
over captures would still pass if the argv this adapter builds were rejected outright by the
CLI, which is the ADR-008 failure mode in its purest form. That test ran: the first turn
returned the exact expected text, a session id and a positive cost; the resume turn came back on
the SAME session id and recalled its own first answer, which is what distinguishes a real resume
from a re-send. Not verified: anything on macOS — this is a Windows measurement, and the Mac's
grok is untouched (`PARITY.md`). ~~Not built: an `internal/adapter/grok`, so no HUD row carries
this vendor. Council can drive this seat; the gauges cannot yet see it.~~

**Amended 2026-08-11: it was built, and this paragraph went on saying otherwise.**
`internal/adapter/grok` landed in PR #183, on the live survey §3.9a records (2026-08-09),
so grok sessions render as
HUD rows with name, model, workspace, a vendor-REPORTED context percentage and last
activity. The gauges now observe this vendor as well as drive it, and the seat's parser
and the adapter read the same wire from two sides. One thing stays dropped and it is not
an oversight: grok writes a per-turn dollar figure and no session total anywhere, so the
last turn's cost reaches the detail pane as a labeled Extra and never the `COST` column
(§3.9a).

**Fleet guard wiring for Grok under ADR-012.** agent-ops ADR-012 rules that guard
wiring, not lane shape, is the control on every vendor — and grok is the fifth vendor seat in the room.
~~To fulfill the ADR-012 guard obligation for Grok, Grok's `PreToolUse` fleet guard is configured by setting
`[compat.claude] hooks = true` in `~/.grok/config.toml` (which routes Grok tool calls through the fleet's
Claude-compatible `PreToolUse` credential guard wrapper `pre_tool_use_credential_guard.py` / `hooks.json`), or by
registering the `PreToolUse` hook script via `grok hooks-add`. This ensures secret stores, published history,
and dangerous mutations are screened by the `PreToolUse` guard across all five seated vendors (Claude, Codex,
Antigravity, Cursor, and Grok) without restricting lane capabilities.~~

**Corrected 2026-08-11: the struck sentences were instructions, and none of it is wired.**
Two things were wrong at once. First the reading: `[compat.claude] hooks` is `false` on
this box, and grok's native hook system (`grok hooks-add` / `grok hooks-trust`) has
nothing installed into it, so **the seat has no fleet guards wired today** — the struck
text described a configuration that does not exist and stated the screening as a fact.
`STATE.md` carries the same measurement. Second the shape: this file records what was
measured about a vendor, and those sentences told a reader how to configure a machine. A
design record is not a runbook, and a runbook here would go stale silently on a box
nobody re-measured.

What replaces them is the obligation, recorded and routed rather than acted on. Under
agent-ops ADR-012 an unwired vendor is an **open obligation on the FLEET**, never a
reason to avoid the seat or to route work away from it — the gap closes by building the
guard. **That work belongs to agent-ops and not to this repository.** telltale seats the
vendor and states each seat's own posture on screen; it does not own the fleet's guard
layer, and nothing here wires one. This paragraph exists so the next reader of §9.39
learns the obligation is open, and learns where it is owned.

**Amended 2026-08-09, same day, from a live room: the seat could not take a briefed turn at
all.** It was merged green and failed on its first real dispatch — `✗ failed 0s`, every seat
answering but this one. The whole of it:

```
error: unexpected argument '--- operating context ---
  You are in a room...' found
  tip: to pass '...' as a value, use '-- ...'
```

`-p` was passed SEPARATED from its value. `Brief.Apply` prepends a fence to every first turn,
so council's real prompt begins with `---`, and clap will not accept a hyphen-leading token as
a flag's value unless that flag opts into `allow_hyphen_values` — which this one does not. So
grok read the entire brief as an unknown flag and exited 2 before emitting a single event.
Exit 2 with an empty stdout is the failure shape this adapter already documents for a bad
resume id, so the room reported it correctly; there was simply nothing to report but a dead
turn.

The fix is one token: `--single=<prompt>`, attached. Everything after the first `=` is the
value, hyphens and newlines included. Verified against the exact failing shape on the first
turn, and composed with `--resume`, where the resumed turn recalled a codeword only the first
turn carried. The long spelling is deliberate — clap's attached form for a SHORT flag is
`-pVALUE`, not `-p=VALUE`, so `--single=` is the unambiguous one. `--resume` keeps the
separated form, because a session id is a UUID and cannot begin with a hyphen.

**The lesson is about the live test, not about clap.** This seat shipped WITH an end-to-end
live test that ran the real argv, and that test passed — because its prompt was
`"Reply with exactly: LIVEOK"`, which begins with a letter. The test exercised the transport
and never the shape the product actually sends. That is a narrower version of the same
mistake §9.39 was written to avoid: a claim verified against a case nobody ships is not
verified. `grok_live_test.go` now sends a fenced prompt on both turns, and
`TestGrokAttachesThePromptToItsFlag` pins the property offline — no argv element may be a bare
`-p`/`--single`, and none may be the naked prompt.

Worth stating for the next adapter, because it generalises past this vendor: **a probe prompt
should be shaped like a brief, not like a greeting.** Three of this file's captures used
friendly one-liners, and the one hazard they could never have surfaced is the one that took
the seat down on its first real turn.

**Amended 2026-08-14: the drift alarm fired, and the seat was re-measured rather than re-dated.**
grok reached **1.0.4 (d846eb93d9)**, four patch versions past the pin every claim above rests on,
and nothing in the repository had noticed. PR #174 added version-pinned wire fixtures for exactly
this, so this is the mechanism working, not a surprise. The re-measure cost three billed turns and
one free one, and it is written up as a measurement because a version bump is not evidence that
anything changed — nor evidence that nothing did.

**The wire is unchanged, and that is a checked claim rather than an impression.** The 1.0.4
capture and the 1.0.0 one were diffed by SHAPE — frame types, key names, nesting, value types —
and they are identical. The single difference in the two files is the KEY of the `modelUsage`
map, `grok-4.5-build` → `grok-4.6-build`, which is a model id rather than a schema key, and one
this seat's parser never reads. `testdata/wire/grok-1.0.4-turn.jsonl` replaces the 1.0.0 file.

**Both containment flags are still dead.**

- `--permission-mode plan` is refuted again, with the write landing again: the `write` tool was
  called, the update reported `completed`, the process exited 0, and `probe-plan.txt` held
  `WROTE` on disk. `--help` still offers `plan` among six permission modes, which is the whole
  point — the help text has said the same thing across five builds while the flag has never once
  been observed to stop a write.
- `--sandbox` is still unobservable on Windows: `bogus-profile-xyz` drew no error, no warning and
  exit 0. **This one was re-probed for free, and the technique generalises.** It was passed
  alongside `--single=/context` — a prompt this vendor is already known to eat — because profile
  validation happens at startup, so a refusal surfaces before any model turn. A turn that is
  never billed still answers the question. macOS still diverges and fails closed (`PARITY.md`).

**The flag that earned its re-run is `--resume`.** 1.0.4's help spells it
`-r, --resume [<SESSION_ID_OR_TITLE>]` — an OPTIONAL value that also matches session titles,
where the pinned build took a required id. An optional-value flag is precisely the clap shape
whose SEPARATED form can stop binding, and this seat passes the id separated. Had it stopped
binding, every follow-up turn would have quietly become a fresh conversation — the room's worst
failure mode, because it looks like success. It did not: the resumed turn echoed the same
`sessionId`, recalled the first turn's own word, and reported `input_tokens` 454 against
`cache_read_input_tokens` 21504. The conversation was on the vendor's side.

**The slash hazard survives, and so does the absent-cost shape.** `--single=/context` still
produces `available_commands`, then an `end` with no usage, no cost and no text, at exit 0. So
`TotalCostUSD`'s pointer-ness is measured on the CURRENT build rather than inherited from the
pinned one.

**What grok gained, and what it did not.** `grok models` now offers `grok-4.6` (default) and
`grok-4.5`, where the 2026-08-09 survey found one model. The CLI has a `grok trace` subcommand
that exports or uploads a session's trace data. **Neither changes a verdict here, and nothing is
built on either.** Quota is still structurally absent (§7.16a, #195): a rate/limit/quota sweep
over a session directory 1.0.4 itself wrote matches nothing account-level, and `grok trace` moves
a transcript rather than reporting a ceiling. The telemetry seam §7.16a already spends is the
OTLP push, and it is untouched.

**One shape was measured that nobody had looked at before, and it is recorded as new rather than
as drift.** A WRITE tool call's `content` element is a diff —
`{"type":"diff","path":…,"oldText":"","newText":"WROTE\n"}` — with no nested `content` object, so
`grokDetail` reads nothing from it. No write call was captured at 1.0.0, so this is a gap in the
original measurement rather than a change under us, and `grok.go` says so at the function. It is
left alone: a detail renders only on a FAILED outcome, and composing a sentence out of
`oldText`/`newText` would be this package writing the vendor's line for it (§9.6a).

**Not re-verified at 1.0.4:** the bad-`--resume`-id error shape, the `--verbatim` and
`--prompt-json` slash channels, and anything on macOS.

### 9.40 the room said something was stopped and never said which seat (2026-08-09)

The gate (§9.8) blocks a vendor until a key is pressed, and the room already announced that in
two places. Neither of them names the seat.

- The **card** sits in the blocked column and does not have to name it: its position *is* the
  seat, which is exactly why `gateCard` passes an empty subject there.
- The **mode line** cannot. `gateLabel` prints the oldest request's own text —
  `GATE Write: internal/council/gate.go (+2 queued)` — which says what is blocked and never who.

So in a five-seat room the footer says something is stopped, and the reader finds out which
column by going through them one at a time while it stays stopped. On a projector, driving four
seats live, that is the stall. **The needs-you strip is one line of room chrome that answers the
question the other two cannot:** `⚠ NEEDS YOU   2 Codex   3 Antigravity`.

**It is driven by the gate queue and by nothing else, and that is the whole safety property.**
`State.Gates` is a structured record of vendors that asked for permission and have not been
answered. Every name on this line comes from one of those entries. A seat that has gone quiet,
a seat streaming nothing, a seat whose prose happens to end in a question mark — none of them
reach it, because none of them is a *measurement* that anyone is blocked (§4a.1). "Needs you"
is a claim about a vendor waiting on a keystroke, and the queue is the only thing in this room
that knows. `TestTheStripSaysNothingWithoutAPendingGate` builds all three of those look-alikes
at once and asserts no strip is drawn.

**A seat leaves when the reader goes to it — and that is the only thing besides answering that
takes it off.** Derived from `State.Focus` rather than stored as an acknowledged set, on
`Gating()`'s own argument: a stored set is a second place for the same fact to live, and the two
drift the first time a seat's gate is answered and a *new* one arrives while the old
acknowledgement is still in the map. At that point the anti-stall silently omits a seat that is
waiting, which is the one failure it exists to prevent. Derived, the worst case is the strip
re-listing a seat the reader visited and left — and that is true: it is still stopped and nobody
is looking at it any more. `TestGoingToASeatIsWhatClearsIt` pins the three clearings that are
refused (the seat going quiet, the seat producing output, time passing) alongside the one that
is allowed.

**The default focus is a hole in that rule, and it is deliberately left open.** `NewState` seats
the keys on column 0 without anyone pressing anything, so a gate on whichever seat happens to be
focused never appears here at all. That is the correct outcome rather than a gap: the reader is
looking at the column whose card is already spelling the question out in full, and a room-level
line naming the seat under their own cursor is the duplication §9.30 spent a section removing.

#### Where it sits, and what it does not yield to

Directly under the frame's heavy rule — **above** the collapsed-seat notice and above the band.
The other two are ordered by subject size (the room outranks the turn); this one is ordered by
**urgency**, on `modeLine`'s own precedent: a gate is the only state in this room where
something is STOPPED until a key is pressed, which is why `GATE` outranks every other mode word
on the footer. Seats that are not on screen and a brief that was just sent are facts a reader
can come back to. A blocked vendor is not.

It costs a row and, unlike the band, **it does not yield to a short terminal.** The band's whole
value is removing a duplication, so retiring it falls back to the frame as it was and loses
nothing that was not already on screen. This line is the only place the room says *which* seat
is stopped, so a row spent on it is the last row the height budget should reclaim. It is spent
in **every tier** for the same reason — at the tabs tier the blocked seat may be the one column
not on screen, which is precisely where a reader has no other way to learn it exists.

#### The ladder, and one ordering that had to be reasoned about again

Longest-first, widest-that-fits-wins — `stripHeader`'s idiom, so this package has one shedding
shape rather than three. Three rungs:

1. every seat, by name;
2. every seat, by the two-letter tag §9.25 made permanent;
3. as many tagged seats as fit, and a count of the rest (`+2 more`).

**Identity yields before a SEAT does**, which is §9.18's order and the rung boundary worth
writing down. A four-seat strip at sixty columns can hold `2 Codex   3 Antigravity   +2 more` or
it can hold `2 CX   3 AG   4 CU   5 GR`, and the second is better by the only measure this line
has: the reader is asking who is stopped, four abbreviations they already learned from the
column headers answer it completely, and two names plus a count answer half of it. So the tag
rung is tried at *full roster* before any seat is dropped.

Nothing is ever clipped at any rung — an entry survives whole or leaves and is counted, because
`Ant` is not a shortened `Antigravity`, it is a seat this room does not have (§9.18). The count
is never traded away either, on `overflowMarker`'s rule: "there is more" without a number is the
marker §9.10 shipped and got reported as a room that could not scroll. Below the width where
even one tagged seat fits, `⚠ NEEDS YOU` survives alone — still true, still the signal, and
honest about being unable to say who. That floor is unreachable in a real frame (MinWidth leaves
the strip 56 cells and the lead costs eleven) and is written out anyway, because the last time a
floor was assumed rather than enforced it was wrong by four.

#### Two smaller rulings

**The seat number is printed only where the key is live.** Digits focus a seat through
`viewKey`, and `gateKey` falls through to it, so while the room is gating the number works in
both modes — except on a turn page, where `focusSeat` refuses outright because there are no
columns to move between (§9.22). A number printed there would be the room naming a key that does
nothing, which is §7.8's surprise. The names stay, because *who is stopped* is still true on a
page.

**A seat folded out of the grid is still named.** Its vendor is blocked whether or not the room
drew it a column, and a blocked vendor with no card, no column and no line anywhere is the
disappearance §4a.1 forbids. It carries no number, because no key in this room reaches it, and
it is therefore the one entry that focus cannot clear — which is honest: nothing the reader can
press from here will unblock it.

**No new hue, and no new site for the old one.** The line is `Alert` — SevWarn at weight, the
gate card's own title style — with the seat numbers `Muted`, which is the chrome/anchor split
the column header and the tab bar already use for those same two things. §9.28's list of three
places a seat hue is spent stays closed; it was ratified as closed, and a fourth site is a
decision for whoever wants to reopen it rather than a side effect of this line. Under `--ascii`
and `NO_COLOR` the strip reads exactly the same, which is the property every distinction this UI
makes has to have: `NEEDS YOU` is the signal, and the mark and the weight only make it findable.

### 9.41 the gate asked about an edit and would not show it (2026-08-09)

The approval card (§9.8) names the call — `⚠ waiting on you: Edit: internal/council/gate.go` —
and that is the whole of what a user has to decide on. For a `Bash` it is enough: the command
*is* the action. For an edit it is not even close. The path says which file is about to change
and says nothing about *what changes*, so the only two answers available are "yes, because I
trust it" and "no, because I don't" — which is the gate reduced to a mood. **The card now shows
the edit itself, as a red/green before/after, when the vendor's payload carried one.**

```
⚠ waiting on you: Edit:
  internal/council/gate.go
  - func gateCard() {
  -  return nil
  - }
  + func gateCard() []string {
  +  return lines
  + }
  y approve   n deny   a stop asking
```

#### What it renders is what was measured, and nothing else

This is §4a.1 on the one card in the product that guards a write, so the rule is stricter here
than anywhere: **council never opens the file, never reconstructs a before from an after, and
never shows one half as if it were two.** A preview is drawn only when the vendor's own
permission request carried *both* halves. Everything else — every `Bash`, every `Read`, every
`Write`, every request from the Cursor seat — renders the card exactly as it rendered before
this section existed, and `TestAPayloadWithoutABeforeShowsNoPreview` pins that by comparing
against the *existing* `gate-card` golden rather than a new one of its own.

Which payloads carry both was measured on 2026-08-09 against **Claude Code 2.1.226** on Windows,
driving the gated invocation (`--permission-prompt-tool stdio`, `--permission-mode manual`,
`--setting-sources ""`) in a throwaway directory. Two requests from that session, quoted whole
on `runner.Gate` and replayed verbatim as tests:

| tool | what the payload carries | what the card draws |
|---|---|---|
| **Edit** | `old_string` **and** `new_string` (plus a `replace_all` the room has no use for) | the before/after |
| **Write** | `file_path` and `content` — the after, and no before at all | nothing |
| **Cursor / ACP** (`session/request_permission`) | a title, a kind, and an options list (§9.36's capture) | nothing |

`content` is deliberately *not* read as a new half. A Write knows what the file will say and
says nothing about what it says now, so treating it as an addition would paint a green block
against a before council never saw — a plausible value where §4a.1 requires an absent one. The
Cursor seat is the same refusal one level up, and a smaller hole than it sounds: that seat does
not ask about edits at all (§9.36), so the card it does raise is about a command, where the
command is already the whole decision.

**The two halves are a pair or neither.** `editHalves` fills both or returns nothing, which
makes the renderer's test simply *do they differ* — and that one question folds in three "show
nothing" cases honestly: no halves at all, an edit that changes nothing, and an edit whose only
difference was a redacted secret. An empty `new_string` beside a non-empty `old_string` is a
legal, measured **deletion** and draws all removals and no added-side count; "0 more added
lines" would be the card filling a slot rather than answering a question.

#### The one thing that crosses the Input boundary, and why it is two strings

`runner.Gate.Input` is the vendor's whole argument blob, held only to be echoed back on an
approval, and it has always been kept **off** `State` on purpose: for a Write it is the entire
file content, one careless line away from the screen. That rule is not repealed here. What
crosses is a **projection** — two named strings, `PendingGate.Old` and `.New` — because two
fields with one purpose cannot be reached for by accident the way a map can. They are read by
the *adapter*, not by council, for the same reason the card's `Text` is composed there: the key
names are that vendor's, and the room renders a preview without knowing whose spelling produced
it.

They are redacted on the way through, and that matters more here than on the argument line the
existing redaction was written for. A command is a *likely* place for a token to appear; the
body of a file being edited is where one actually lives — and this lands in chrome that does not
scroll away. Each half is redacted separately because each is rendered separately. Only trailing
newlines are trimmed, never leading whitespace: an indent is content, and a preview that
silently unindented the code it is asking about would be showing an edit nobody requested.

#### The prefixes carry it; the colour only seconds it

`-` and `+` are the entire signal, exactly as they are on §9.37's raw patch lines — which is why
the preview reads identically under `--ascii` and `NO_COLOR`, and why the goldens, which render
`PlainStyles`, are the *proof* of that rather than an approximation. The styling reuses
`Styles.ForDiffLine`, the same classifier those patch lines already go through, so **council adds
no hue for this**: green-for-added and red-for-removed is one convention spent twice, not a
second vocabulary, and §9.28's closed list is untouched.

The marks are patch punctuation and **not** entries in the `Glyphs` alphabet, which is worth
saying because the ASCII set already spends `+` on `ActOK` and `-` on the light rule. They do not
collide, on `Glyphs.Range`'s own slot argument: those are marks that stand alone in a slot, and
these only ever open a line inside this block.

#### Bounded, per half, and it says what it dropped

The card is **chrome** — it costs body lines, `MaxScroll` derives the ceiling from it, and every
row spent here is a row of the reply the user cannot read while deciding. So each half shows at
most **three** lines and then counts: `2 more removed lines not shown`.

The count is **per half**, not one total, because a long removal would otherwise spend the whole
budget and take the additions with it, with no line admitting the additions had ever been there.
It carries no glyph of its own: `…` has an ASCII partner of `>`, and `> 2 more removed lines`
reads as a comparison rather than as a truncation — a marker that can be misread as a number is
worse than a longer sentence. Long *lines* are cut with the ellipsis glyph on the plain text
**before** the style is applied — classify, truncate, style, and only then let `fit` pad — because
`fit` alone clips silently and would be clipping a string that now genuinely carries ANSI, which
is §9.5's trap read backwards.

#### What is deliberately not here

**No intra-line diff.** The unit is the line, because that is the unit the payload arrives in;
highlighting *which words* changed inside a line would be council computing a diff rather than
displaying one, and the first thing it would get wrong is the case it was added for.

**No scrolling the preview.** A card with its own viewport is a second scroll surface in a room
that already has one per column plus a turn page, and the keys to move it would have to be taken
from a mode whose whole contract is that `y` and `n` mean one thing each (§7.8). The bound plus
an honest count is the trade; the whole edit is in the file the moment it is approved.

**No preview on a denial or after the fact.** The trace still says `✗ denied by you` and nothing
more. What the seat *would* have written is not what happened, and a room that showed it
afterwards would be displaying a file state that never existed.

### 9.42 `telltale doctor`: the one moment probing is allowed, and the three answers it may give (2026-08-09)

Council's detection has never run a vendor. `detect.go` says why in its own doc — "council
never runs a vendor to find out whether it works: a probe turn costs real quota, and 'is it
authenticated?' is a question the first real dispatch answers for free" (ADR-008 §6) — and that
rule is correct and stays. What it is a rule *about* is a **turn**: the room may not spend the
user's money to answer a question it does not have to ask, and it may not spend it silently,
mid-conversation, on a schedule nobody asked for.

So the boundary is drawn at **cost and side effect**, not at "running a vendor is forbidden".
`telltale doctor` runs `<binary> --version` — a flag that parses argv, prints a string and
exits, with no model, no session and no billing anywhere in it. It starts no turn, reads no
credential store, makes no network call, and writes nothing at all: it adds no fourth exception
to the three writes `CLAUDE.md` lists, because it has none. §9.17 is the frame that makes this
the right place for it — *a fact that is true at launch and stays true belongs at launch*, and
what binary is on this disk and what it calls itself is exactly that shape. The inverse of the
same rule is why there is no `/doctor`: nothing this reports changes while the room is open.

**Three states, and the whole design is keeping them three.**

| | what it means | what it may carry |
|---|---|---|
| `ok` | the check ran and passed | the measured text — the path that was stat'd, the line the vendor printed |
| `FAILED` | the check ran and did not pass | the reason |
| `not checked` | the check did not run | **why** it did not, and never a value |

**Auth and network are `not checked` on every seat, always.** A binary that exists and answers
`--version` establishes nothing whatever about a login or about reachability, and a report that
let those two ride along on the good news would be believed on the one day it was wrong — the
§4a.1 collapse wearing a preflight's clothes. The reason is printed rather than shrugged: the
only thing that establishes a login is a real turn, and a turn costs quota. A seat that is
installed and signed out reports its own auth failure on its column the first time you dispatch
to it, which is where that fact was always going to come from.

**`NotChecked` is the zero value**, for §9.17's `GateOff` reason exactly: a safety property
whose default is the reassuring answer is the wrong way round however carefully the constructor
sets it. Every `Check` a test types by hand, and every field a future change forgets to fill in,
reads as an honest blank instead of a silent pass. `TestNotCheckedIsTheZeroStatus` pins it.

**The measurement that would have been a plausible lie.** One seat's resolved binary is not the
program it names. Detection steps over `cursor-agent.cmd` to the bundled `node.exe` its launcher
would have run (§9.33), so the obvious `--version` answers **`v24.5.0`** — node's version,
printed under a row labelled `cursor`, real and about the wrong program. Handed the bundle first,
`node.exe index.js --version`, the same install answers **`2026.08.04-aaa8809`**. Both measured
here, one after the other. The version argv is therefore per-seat data taken through
`vendors.CursorNodeBundle`, not a `--version` constant and not a second `filepath.Join` — that
function's own doc names two copies of one join as the agreement that silently stops holding.
The other four take the bare flag, verified by running them: claude `2.1.226 (Claude Code)`,
codex `codex-cli 0.147.0`, agy `1.1.11`, grok `grok 1.0.0 (3cd0d0cbce) [stable]`.

**Found and undrivable is a third thing, and it stays one.** `detect.go` already refuses to
collapse "not installed" with "installed somewhere council will not drive from", because the fix
differs. The preflight carries that through as two checks rather than one verdict: `binary`
passes and `drivable` fails, with the shim note attached. The version probe still runs on such a
seat — what is installed is worth knowing where the room cannot use it, and a fixed `--version`
carries none of the prompt text that made it undrivable.

**A fifth mode, not a council flag, and no colour in it.** What it prints goes somewhere else:
council's output is a full-screen room, so a preflight rendered inside one is unreadable at
exactly the moment it is wanted — before the room opens, piped into a file, pasted into an issue.
Every distinction it makes is carried by a **word** (`ok`, `FAILED`, `not checked`), so
`--ascii` and `NO_COLOR` have nothing to switch off and neither is a flag here; a flag that does
nothing is a promise that something was configurable. `Render` is pure over its `Report` for
council's reason, with the probe durations measured in `Run` and arriving as data. Each seat gets
its own `--timeout` (15s default) rather than the run sharing one: a wedged vendor must cost its
own deadline and a failed check, never the report. And the command exits **0** whatever it finds
— a failed check is this mode working, and exiting non-zero would make "I have four of five
seats" indistinguishable from "doctor itself broke".

**Capabilities are printed and are explicitly not checks.** How a seat streams and whether it can
be asked to ask first were measured once, against live runs (§9.7, §9.33, §9.36, §9.39, and
`canGate`), and written down; nothing re-measures them on this machine now. They render on their
own labelled line — *"council declares, and did not check here"* — outside the status column,
because putting them in it would give them a fourth state and imply a check that never happened.
The words are read off `granularityFor` and `canGate` rather than restated, so the preflight
cannot drift from the room's own badges. Worth the line at all because "installed" and "will
stream to you" are different promises, and the seat a user is most likely to think is broken is
the one that is working and silent until the end of the turn (§9.14).

### 9.43 the agy seat stops pretending a lost thread resumed (2026-08-09)

`STATE.md` carried this as an unowned gap for as long as it took to write the entry. **The agy
seat could not tell a lost thread from a resumed one, and the stream would not say.** Measured
2026-08-09 against agy 1.1.11 during the wire-fixture capture (PR #174; the record is
`internal/council/vendors/testdata/wire/README.md`, under *what could NOT be captured, and why*):
handed a `--conversation` id it does not hold, that CLI **does not error**. It opens a NEW
conversation, answers the brief normally, and reports `status: "SUCCESS"`, exit 0.

That is the whole difficulty in one sentence. Every other seat resolves the question for the
room: Claude Code returns a `result` frame whose `errors` array says *"No conversation found with
session ID: …"* (PR #178); codex writes `no rollout found` to stderr and exits 1; grok
does the same with a 404; the Cursor seat answers `session/load` with -32602 and opens a fresh
one in the same process (§9.36). agy claims success either way — so a room reading status and
exit code, which is every honest thing the seat did before this, rendered **a continued
conversation over a reply that had no history behind it.** Not a crash and not an empty column:
a plausible answer under a `restored` mark that was no longer true.

**The tell, and it is the only one the capture surfaced: the `conversation_id` that comes back is
not the one that was asked for.** Nothing read it. Reading it is this change.

**The comparison lives in council, not in the adapter, because neither half is where the other
is.** `ParseEvent` sees one line at a time and never learns which id the turn requested; dispatch
knows the request and never sees the stream. So `specFor` now *returns* the id it asked to
resume — the one fact a caller cannot re-derive, since it is buried in a vendor-specific argv
position — dispatch records it, and `adoptSession` compares it against the id the vendor names
on its own session event.

**The vendor gate is structural, and that is the honesty argument rather than a taste in
plumbing.** The arithmetic (`asked != returned`) is vendor-neutral; the *conclusion* is not. A
CLI that re-keys a resumed thread while keeping its history would look identical on the wire, so
a room that compared ids for everybody would announce lost threads it had never measured — §4a.1's
inference, wearing a comparison's clothes. The claim is therefore made by the seat, through
`vendors.SilentResumeFork`, whose one method returns **the build the fork was measured against**
rather than being a bare marker: a seat cannot make the claim without naming its evidence, and a
vendor bump that fixes the behaviour leaves a version string that no longer matches the fixture
beside it. Only agy implements it. `TestOnlyAMeasuredVendorArmsTheForkComparison` pins that it
stays alone until somebody captures a second case, and the gate is applied at *dispatch* — a seat
that never enters `forkWatch` cannot raise the card at all.

**Three rulings on what the room then does, each of which had a plausible alternative.**

| | what happens | the alternative, and why not |
|---|---|---|
| the reply | **renders, untouched** | failing the column would throw away an answer the user paid for, to punish a bookkeeping mismatch. The turn succeeded; what was false was only the claim that it was informed by everything before it |
| the new id | **adopted as this seat's thread** | discarding it orphans a real turn — the reply happened *inside* that conversation — and leaves the room rebuilding the same forking invocation on every later turn |
| the card | **the calm lost-thread card already in use** | a second card would say the same fact in different words. The outcome is identical to a refused reattach — this seat is starting fresh — and only the body differs, because there the turn failed and the id was let go, here it succeeded in a thread nobody asked for |

So the column reads *"thread not restored — starting fresh"*, quietly, with the mechanics
demoted underneath it: the seat asked to resume its saved thread, the vendor answered in a new
conversation instead and reported success, the reply below is real and the history behind it is
not, and the next brief continues from this turn. **No warning mark**, for the reason
`settleRestoredThread` states: this is the same fact `reattachCard` says calmly at idle when no
thread came back, discovered a turn later, and spending the ⚠ on it blunts the mark that carries
real failures. The seat's probation ends here too — the restored id is gone by evidence rather
than by a turn's outcome, so `settleRestoredThread` has nothing left to decide and a later
failure on the *new* thread cannot be blamed on a reattach that was already reported.

**The fixture is derived, and it is labelled derived.** `testdata/agy-forked-conversation.jsonl`
is the real 1.1.11 capture with one textual substitution — every `conversation_id` value moved
from `2222…` to `3333…`, nothing else, not a key and not a token count. It sits **outside**
`testdata/wire/`, whose contract is real captures only: the forked turn itself was measured, but
that probe's stream was not kept, and a hand-edited file among the captures would silently
restate a measurement nobody re-ran. What it proves is the narrow thing it can: a turn that looks
entirely successful still delivers the mismatched id to the room.

**What was not done, and is not claimed: no live agy turn was driven for this change.** The
behaviour rests on the 2026-08-09 capture, and the code rests on that capture's fixture. A
re-measurement against a later build is what would retire `SilentResumeForkMeasuredAt`, and
until somebody runs one, this seat's claim names 1.1.11 and no other build.

### 9.44 the composer was a gap under a rule, and the room's state floated below it (2026-08-09)

**Inspiration is named because it should be: Grok's CLI.** Its input is a rounded, clearly
bordered box, and the bottom border carries a right-anchored legend — `Grok 4.5 (high) ·
always-approve` — laid *on* the line rather than under it, with the remaining key hint
(`Shift+Tab:mode`) on a muted line below. The thing that reads well there is not the corners. It
is that the box says **where you act**, and its own frame says **what you are acting under**,
which leaves the line below free to be nothing but keys.

Council had neither. §9.26 closed the frame with two full-bleed heavy rules, and the composer sat
in the gap between the lower one and the mode line — a prompt glyph on an unbounded row, with no
mark anywhere saying that this strip of the screen is the one place typing does anything. Every
other region in the room is a reading area. The one region that is an *input* was the only region
with no shape of its own.

**So the composer is a box, and it is the only bordered element on screen.** Rounded corners
(`╭ ─ ╮ │ ╰ ╯`, and `+ - |` in the reduced set), a side and a cell of air on each row, and the
bottom border carrying the legend. Bordering exactly one thing is the whole design: a second box
anywhere would make this one a decoration instead of a signal, the same scarcity argument §9.26
made for a second rule weight and §9.28 made for the seat hue.

**The lower heavy rule is gone, and that is a correction to §9.26 rather than a cost of this
change.** §9.26's claim was that the two full-bleed rules were the only *closed shape* on screen
and that a closed shape earns the second weight. They were never closed — two horizontal lines
with nothing joining their ends is a pair of lines, and the weight was doing the work a shape
should have done. The box actually closes, by corners and sides, so it draws **light**: closure
moved from ink to geometry, which is a carrier that survives `NO_COLOR` outright. What the heavy
rule now says is narrower and true — *the chrome stops here and the seats begin* — and there is
exactly one line in a grid where that holds.

**The legend is split by lifetime, not by topic.** On the border go the facts that stay true until
a key changes them: the mode word (`VIEW` / `COMPOSE` / `GATE` / the page label) and, when the
guard is off, `a not asking`. Under the box go the keys, which change with the mode, the draft and
the turn. Nothing appears in both places — `statusLine`'s left-hand slot is now empty and is
deliberately not backfilled, because a slot that survives its content is how a footer becomes the
wall §9.11 spent a whole pass taking apart.

The cadence cell keeps its **key** on the way up, not just its words. `a not asking` was added
(§9.24-era footer work) to close the §9.17 defect where a permanently ungated room documented the
way back nowhere on screen; moving the words without the key would have reopened it one release
later. It is on the border, whole, and unsheddable.

**What it costs, stated plainly.** One row of body — `promptChrome` goes from 2 to 3, since the
box replaces the rule with a top border and adds a bottom one — and six cells of composer width,
which is `boxChrome`: a side glyph plus `gutter` cells of air, on each side. The air is `gutter`
rather than one cell because **the room spells its separator one way** — two cells each side of
every `│` it draws, which `TestTheRoomSpellsItsSeparatorOneWay` already holds for the header, the
key line and the column rails. A box welding its prose to its own sides would be a second grammar
for the room's only vertical mark, on the element the eye is meant to read as the frame.

**The help panel paid the row, the way it always pays.** Its budget was 17 lines to the pinned `?`
and is now 16, which pushed `ctrl+c / q` off page one and the WORKSPACE sentence — the
load-bearing line — off page two at the reference machine's 24-row room. Neither was allowed to
fall: both pages spend the blank row directly under their title instead, on §9.11's own ranking
that **a rule outranks a blank**. The title *is* a labelled rule, so the blank beneath it was the
one row on each page restating a boundary the row above already drew.

`MinWidth` (60) and `MinHeight` (10) are unchanged and still
resolve: at the floor the room is 2 header rows, 3 of footer chrome, a one-row composer and four
rows of reading area. When the frame is too narrow to lay the legend on the border with air each
side and a rule cell outboard of it, the border **closes bare** rather than truncating — a legend
cut in half is a claim cut in half.

**One golden moved further than the box did, and it is a gain rather than a surprise.** Emptying
`statusLine`'s left slot gives the key line back four cells and its gap, and at the tabbed tier
that is enough for `1-N seat` to stop shedding — `empty-tabs.txt` now names a key that always
worked and had been dropped for width since §9.29. Nothing was un-shed by hand; the ladder in
`hint.shed` is unchanged and simply has more room to not use.

**No new hues.** The border and the sides are `Rule()`, i.e. muted chrome; the legend keeps the
exact styles the mode word already had on the mode line, gate included. This change spends
*shape*, which the palette does not pay for.
