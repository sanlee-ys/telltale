# telltale — design doc

Status: v1 surface built; v1 itself is held until council settles (§1). The honest-gauge
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

**v1 is held until council settles.** The gauges are finished and unreleased. The standing
alternative — cut v1 as gauges only, statusline and HUD with declared vendor version pins
— is rejected, because a v1 that named the gauges would name the wrong product. Council's
surface is still moving week to week, so the version waits on §9 rather than on §2, §3
and §7.

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
statusline path does no I/O beyond stdin — revisit only with a measured budget),
permission mode (not in the stdin payload; same call the predecessor script made).

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

| Field | Claude (disk) | Codex (disk) | Gemini (disk, §3.7) | Antigravity (disk, §3.8) | Cursor (disk, §3.9) |
|---|---|---|---|---|---|
| session id, cwd, git branch | yes | yes | id yes; cwd via `projects.json`; branch no | id yes; cwd via the trajectory blob's `file:///` URI; branch no | id yes; cwd via `workspaceStorage/<id>/workspace.json`; branch no |
| model | yes | yes | yes (per message) | yes (per generation, id + display name) | yes (`modelConfig.modelName`, one string; sometimes the literal `default`) |
| token counts | yes | yes | yes (per message) | yes (per generation, self-checking) | context totals yes; per-message counts present and **always 0** |
| context window size | **no** | yes | **no** (static table in CLI source only) | **no** (statusline payload only) | yes (`contextTokenLimit`) |
| context % | **not derivable** | **derived** | **not derivable** | **not derivable** | **reported** (the vendor persists its own; derived from raw counts only if it is missing) |
| quota / rate limits | **no** (statusline stdin only) | yes | **no** (runtime 429 handling only) | **no** (statusline stdin only; never persisted) | **no** — plan *entitlements* on disk, no consumption record |
| cost USD | no (stdin only) | no | no | no | **no** — `usageData` `{}`, token counts unpopulated zeros |
| process liveness | registry exists, deliberately unread (§3.1) | none | none | `steps.status` exists, structural only (never observed in-flight) | `status`/`generatingBubbleIds` exist, structural only (never observed in-flight); Hooks is the real seam |
| session title | yes | no | yes (`summary` metadata) | **no** — the only free text on disk is prompt content | yes (`value.name`, vendor-generated) |
| sub-agent count | **derived** (`subagents/` sidecar, §3.1) | **no** | **derived** (`chats/<parent-id>/` nest) | **no** — `parent_references` observed empty | **no** — `isSubagent`/`numSubComposers` observed zero throughout |

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
not read (§3.9, decisions/007).

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

- **Took:** Name (conversation id, shortened for the grid — the only label on disk that
  is not somebody's prompt), Model (`#1.#21` display name, `#1.#19` id fallback),
  Workspace (the trajectory blob's URI, converted to a native path), LastActivity (the
  Q8 fold over the transcript's newest `created_at` and the mtimes of the transcript,
  the database and its sidecar — the sidecar because on a live conversation that is the
  file being written). All four REPORTED; nothing is derived.
- **Left:** context % (the numerator is measured and the denominator is not — the token
  totals are display-only extras instead), cost, quota, liveness and subagents, all
  `CapNone` for the reasons itemized above. The liveness and subagent deferrals are
  pending live observation, not pending effort.
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
| Doc/code sync | `internal/hud` | every render pasted into `docs/design.md` §7.3/§7.11–§7.14 and into `README.md` still matches its golden, and every golden is either embedded or explicitly exempted |

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
3. HUD refresh model — **PROVISIONALLY ANSWERED, revisit with measurement:** a 1 s
   `tea.Tick` poll, not a file watcher. The survey's inputs argue for it: 837 sessions
   across 33 directories, transcripts up to 7.7 MB, and a projects tree that provably
   mutates mid-sweep. `Discover` is stat-only and `Read` is head+tail bounded, which is
   what makes polling affordable; a watcher over a mutating tree on Windows is a larger
   correctness surface for a smaller win. Not yet measured on a cold cache — that is the
   open part.
4. ~~Exact Claude/Codex on-disk data sources~~ — **ANSWERED, §3.1–3.3**, with Claude
   verified live and Codex's first live pass run 2026-08-01 (§3.4; short remainder
   itemized there).
5. Distribution naming (`telltale-hud` on any registry; winget/scoop manifests) — at
   packaging time. Go binary means npm is optional, not required.
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
quota appears once, in the header, never per row.** `rate_limits` is a property of the
account, not the session; repeating it on every row would assert per-session quota, which
is false. If no adapter can source it, the block is absent — not zeroed. *(v1 limitation:
the block shows the windows from the most recently active session that has any. With one
quota-bearing vendor that is exact; a second one needs a per-vendor block.)*

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
 telltale  │  4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
```

Row 3 is the honest-gauge case in its normal habitat: a session whose adapter can source
a model and an age but not context or cost. Blank gauge field, `—` in both numeric
columns. Nothing about it looks like zero.

**B — compact (80 cols).** Cost gone, gauge halved, quota wrapped to its own line.

```
 telltale  │  4 sessions  │  claude 3  codex 1
               claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────
        SESSION                           MODEL          CONTEXT            AGE
 ● CC │ telltale  C:\src\code             Opus 5         █████▉──  84.2% │  12s
 ● CC │ acme-api  C:\src\work             Sonnet 4.5     ██▉─────    41% │  48s
 ◐ CX │ notes-api  C:\src\code            gpt-5.1-codex                — │   4m
 ○ CC │ learning-notes  C:\src\code       Haiku 4.5      ██████▌─  92.6% │  22m
 ──────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   ? keys
```

**C — narrow (72 cols).** Gauge gone; the number it encoded stays. Vendor names shorten.

```
 telltale  │  4 sessions  │  cc 3  cx 1
       claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
```

`0%` is a full track of `────────────`; absent is whitespace. If these two rows ever
render the same, the build fails.

**E — stale scan (120 cols).** The scan has been failing for 47 seconds. Values are the
last ones actually measured; the whole row area renders `Muted` (invisible in a plain
golden, asserted separately), and the footer's right slot carries the notice. The header
is never used for notices — it holds identity and quota only, which keeps it from
overflowing at any width.

```
 telltale  │  4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys            ⚠ last scan 47s ago   Access is denied.
```

**F — filter and sort active (120 cols).** The header count reads `3 of 4` so it cannot
contradict the per-vendor totals beside it. Non-default filter/sort is stated in the
footer, because a monitor that silently hides rows is a liar.

```
 telltale  │  3 of 4 sessions  │  claude 3  codex 1    claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys                       filter claude   sort context
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys                                    ⚠ codex drifted
```

**H — help overlay (120 cols).** Replaces the row area rather than floating over it; a
floating panel on a monitor obscures the thing being monitored.

```
 telltale  │  4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

        q      quit  (also ctrl+c)
        ↑/↓    move the selection  (also j / k)
        enter  open the detail pane for the selected session
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
column auto-hides and its width returns to `SESSION`. The `AG` row is labelled by the
head of its conversation id because the only free text on agy's disk is prompt content
(§3.8), and its `MODEL` cell truncates because the vendor's display string is 23
characters against a 13-column cell — both are what the HUD really shows. The `CU` row
is the one to read next to the `CX` row: both carry a context bar, and only one of them
carries a `~`, because Cursor persists its own `contextUsagePercent` and telltale reads
it rather than computing one (§3.9).

```
 telltale  │  5 sessions  │  claude 1  codex 1  gemini 1  agy 1  cursor 1               codex 5h ██████▎─ 88.4% ↻ 3h02m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
 ● CC │ telltale  C:\src\code                                                 Opus 5                           — │  12s
 ● CU │ multi-vendor orchestration  C:\src\code                               composer-2.5   ████▏───────    37% │   1m
 ● CX │ example-app  C:\src\code                                              gpt-5.1-codex  ███████▋──── ~69.8% │   1m
 ● AG │ 4c8b21a7  C:\src\code                                                 Gemini 3.6 F…                    — │   2m
 ◐ GE │ glossary tooltips ⑂~2  c:\src\code                                    gemini-3-pro                     — │   3m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
```

**L — ASCII glyph mode (120 cols).** `--ascii`, `TELLTALE_ASCII=1`, or a non-terminal
output target. Absent renders `n/a`; the gauge loses its eighth-cell partials, which is a
real precision loss in the bar and acceptable only because the number beside it carries
the precision.

```
 telltale  |  4 sessions  |  claude 3  codex 1         claude 5h ###-----   42% ~ 2h13m   |   7d #-------   18% ~ 5d02h
 ----------------------------------------------------------------------------------------------------------------------
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 * CC | telltale  C:\src\code                                        Opus 5         #########---  84.2%    $2.41 |  12s
 * CC | acme-api  C:\src\work                                        Sonnet 4.5     #####-------    41%    $0.18 |  48s
 o CX | notes-api  C:\src\code                                       gpt-5.1-codex                  n/a      n/a |   4m
 . CC | learning-notes  C:\src\code                                  Haiku 4.5      ##########--  92.6%   $11.07 |  22m
 ----------------------------------------------------------------------------------------------------------------------
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 telltale  │  4 sessions  │  claude 3  codex 1         claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ 00000000-bbbb-4ccc-8ddd-000000000001                                                          —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys                                    ⚠ codex drifted
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
| `●` `◐` `○` | `*` `o` `.` | | `─` (track) | `-` |
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
- The account quota block is sourced from one session (§7.1). A second quota-bearing
  vendor needs a per-vendor block.
- The 1 s poll has not been measured on a cold cache over an 837-session tree (§6 Q3).
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
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
                                   claude 5h ███─────   42% ↻ 2h13m  ~13:27 · 18m basis   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys
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
 telltale  │  2 of 4 sessions  │  claude 3  codex 1    claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 /api_                                                                                          esc clear   enter apply
```

and once applied, with the mode left:

```
 telltale  │  2 of 4 sessions  │  claude 3  codex 1    claude 5h ███─────   42% ↻ 2h13m   │   7d █▎──────   18% ↻ 5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   / find   enter detail   v vendor   s sort   a all   ? keys                                         find "api"
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

## 8. Roadmap (decided 2026-08-01; adoption track added 2026-08-02, ADR-005)

Rigor stays the floor; features and front-end craft are the priority axis from here.
Each item names its incumbent inspiration and the honest-gauge twist that makes it ours.
Sources rule unchanged: a segment ships only when this doc names its source.

Read the version numbers below against §1: v1 is held until council settles, so an item
marked for v1 is not an item waiting on the gauges. They are done.

ADR-005 adds a second axis: external adoption is now an explicit product goal alongside
ADR-001's portfolio-evidence bar, and adoptability is a design input rather than a
lagging indicator. That does not reorder the feature track below — it adds the adoption
track that runs beside it, and one of its items lands *before* v1 is done, which is why
this section is no longer titled "after v1".

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
   (ADR-005, second amendment) — while CI still runs `windows-latest` only, and four
   of the five adapters have still never met a live macOS corpus.
   The README positioning line — *"one local HUD for every coding agent you use"* —
   lands **with** this slice and deliberately not before it: a positioning claim that
   arrives ahead of a one-command install is a promise the reader has no way to act on.
2. **The launch post is experiment #1, and the hypothesis it tests is cross-harness
   visibility** — do multi-harness power users want one honest local HUD across the
   agents they already run? That, and only that, is what the launched product contains.
   Its bar is pre-registered here before launch so the outcome is falsifiable either
   way: **10 run-evidenced external users within 30 days of the post.** Run-evidence
   means the person demonstrably ran telltale — a version-bearing bug report, a
   real-session screenshot, a PR grounded in running it, package-manager feedback, or an
   unsolicited statement of use. Engagement without run-evidence (a comment, a question,
   a hot take) does not count, and a star does not count; stars stay weather (ADR-001).
3. **Needs-input / blocked / done state is the first post-validation feature** — the
   attention-routing job, and the reason the product is positioned the way it is. It is
   built where the vendor seams already support it: Claude Code hooks, Codex notify
   events, agy's `agent_state` (observed live transitioning `tool_use` → `idle`, §3.8),
   and Cursor Hooks (documented and versioned — §3.9, and the reason the Cursor adapter's
   `status` field is deferred rather than mapped). Not before experiment #1 reads out,
   and it then gets **its own experiment
   (#2)**. The launch experiment explicitly does not claim this ground: its result is
   evidence about cross-harness visibility only, and neither validates nor falsifies a
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

It is the one subcommand that is not a gauge, and the boundary is worth stating precisely
rather than hand-waving. §7.8's invariant — no keybinding may mutate vendor state or send
anything to a running agent — is **unchanged and unweakened**; it is a rule about the
observation surfaces, and council is not one of them. Nothing in the HUD reaches council.
The only way in is typing the subcommand. What moved is the *scope* of the sentence in
`README.md`, from "telltale never writes" to "the gauges never write", because the old
phrasing had become false the moment ADR-008 was accepted and an accepted decision the
docs contradict is worse than either option on its own.

### 9.1 What v1 seats, and what it does not

Four columns: **Claude Code**, **Codex**, **Antigravity**, **Cursor**.

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
own session id (ADR-008, ninth and eleventh amendments). Multi-turn is native resume for the
batch seats and one live process for Claude (§9.8).

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

The third is the honesty of the whole feature, and it is the one nobody would have thought to
test. Permission *allow rules* in settings files are consulted **before** the callback, so a
call they cover never reaches the gate at all. Without that flag, "nothing writes without your
keystroke" is simply false — and false quietly, on a machine whose owner wrote those rules
years ago for a different purpose.

**One limit is stated on the badge rather than buried here.** Shell commands the CLI itself
classifies as read-only are approved without asking — `git status` was ungated under both
setting-source configurations, and so is `echo` — so the claim is about calls that *change*
things and is worded that way everywhere.

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

`--write --auto` restores the old behaviour for the times nobody is watching: `acceptEdits`,
the `WRITES` badge, the user's settings left alone — and therefore no injected hooks file
either, since a room that loads those settings natively would otherwise run every hook twice.
Gating is the default because the room the user opened is the one they are looking at;
unattended is the exception and has to be typed.

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
they remain that way?* The README's new badge table answers it where a first-time reader
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
| the terminal honours it | Windows Terminal accepts OSC 52 writes in current builds | **INFERRED** from documented behaviour, not run |

That last row cannot be closed from inside this repo: the only observer that could settle it is
the terminal, and it sends nothing back. So the notice claims what council **did** — "copied
claude code's turn-3 reply" — and never what the machine now holds, and the honest check is a
person pressing `y` and then `ctrl+v`. The notice is not decoration for the same reason: with
no acknowledgement available, it is the *only* feedback that the key did anything, and a silent
copy would be indistinguishable from a terminal that ignored the sequence — the ambiguity
§4a.1 forbids everywhere else here.

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
notice says the room is read-only and names the flag that would change it.

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
| `--read` (posture) | **violates** | see the refusal above. Note what is *not* an objection: posture is deliberately never restored from the saved room, because "a posture that can arrive from a file is not one anyone typed." A posture typed into the composer is typed |
| `--auto` | **violates** | whether the gated seat asks before each tool call is a preference you form partway through a batch, not before it |
| `--vendor` | **violates** | who is *seated* is launch-only. `-@seat` routes one turn and is explicitly "a different control from an @mention" — routing is not reseating |
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

#### Not decided here

Whether the other three violating controls — `--read`, `--auto`, `--vendor` — earn their own
surfaces or should be judged one at a time against this rule. Each is its own change.

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
