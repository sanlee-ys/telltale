# telltale — design doc

Status: v1 surface built. The honest-gauge rule requires every segment's data source to
be named here before that segment ships; the tables below are the authority the eval
harness tests against.

## 1. Product shape

Two surfaces over one data layer:

```
vendor adapters  ──►  normalized session model  ──►  renderers
(claude, codex)       (one schema, documented)      (statusline / HUD)
```

One Go module, one binary (`telltale.exe`), two modes (ADR-002):

- **`telltale statusline`** (Claude Code only in v1): reads the JSON Claude Code passes
  on stdin, prints one line, exits. Latency budget: single-digit milliseconds; Bubble
  Tea is never initialized on this path. Budget-conscious output (every character
  renders on every prompt).
- **`telltale hud`** (cross-vendor): a Bubble Tea/Lipgloss watch-mode TUI listing live
  sessions across vendors with per-session gauges. **First-class UI surface** — a UI
  design section (layout grid, color/threshold system, motion rules, empty/degraded
  state designs) is written here BEFORE the HUD is built, and degraded-state renders
  are eval fixtures. Windows Terminal is the reference rendering environment.

The two paths share exactly two packages: `internal/model` (the schema) and
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

Codex CLI is **not installed on the dev PC** (`~/.codex` absent, `codex` not on PATH,
re-confirmed 2026-08-01). Everything below is read from `github.com/openai/codex` at
commit `1e85ca09` (2026-08-01): `codex-rs/utils/home-dir/src/lib.rs`,
`codex-rs/rollout/src/{lib,recorder,compression,policy,metadata}.rs`,
`codex-rs/protocol/src/{protocol,models}.rs`,
`codex-rs/login/src/auth/default_client.rs`, `codex-rs/thread-store/README.md`.
**ADR-001's live-session verification is still owed** and the adapter is not "done"
until §3.4 is discharged.

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
did not contain. Confirm against a live session before the adapter is called done.

**Codex capability gaps:** no cost in USD anywhere; **no process-liveness registry** (mtime
is the only signal); no session title, so rows fall back to the workspace basename; cold
`.zst` sessions unreadable under minimal deps. Reading the SQLite state DB
(`codex-rs/rollout/src/state_db.rs`) is a **rejected** path — it would add a sqlite
dependency for metadata the JSONL already carries, and `thread-store/README.md` confirms
JSONL stays canonical and readable without SQLite.

### 3.3 Cross-vendor capability matrix — the asymmetry is a design fact, not a bug

| Field | Claude (disk) | Codex (disk) |
|---|---|---|
| session id, cwd, git branch | yes | yes |
| model | yes | yes |
| token counts | yes | yes |
| context window size | **no** | yes |
| context % | **not derivable** | **derived** |
| quota / rate limits | **no** (statusline stdin only) | yes |
| cost USD | no (stdin only) | no |
| process liveness | registry exists, deliberately unread (§3.1) | none |
| session title | yes | no |

Claude's quota lives on the statusline seam; Codex's lives on the disk seam. So the HUD's
quota block is Codex-sourced today, and the CONTEXT column carries a Codex number beside
a Claude em dash. **Nothing sources cost**, so the COST column auto-hides in every real
v1 frame — see the `v1-capabilities` render in §7.3.

**Percentage comparability.** Codex's own
`TokenUsage::percent_of_context_window_remaining` subtracts `BASELINE_TOKENS = 12000`
from both numerator and denominator; Claude's `context_window.used_percentage` is raw
input-token-based. **They are not the same statistic.** The adapter therefore does *not*
reproduce Codex's baseline-normalized figure: it computes a plain
`last_token_usage.total_tokens ÷ model_context_window`, declares it `CapDerived`, and the
HUD marks it with an estimate marker. See §6 Q7 for the resolution and its alternatives.

### 3.4 Live-verification checklist owed for Codex (ADR-001)

Run on a machine with a real Codex install, before the adapter is called done: confirm the
`sessions/<YYYY>/<MM>/<DD>/` local-date path; confirm the literal `session_meta`
envelope and whether `id` or `session_id` is written; confirm `history_mode` on a fresh
session; capture one real `token_count` and check `model_context_window` is populated
rather than `null`; **confirm whether a null `info`/`rate_limits` on a later event means
"cleared" or "unchanged"** (§3.2); confirm `rate_limits.primary`/`.secondary` on a
ChatGPT-plan login and their **absence** on an API-key login (the Codex analogue of
Claude's degraded fixture) and record real `window_minutes` values instead of hard-coding
"5h/7d"; confirm whether `ordinal` is emitted; confirm mtime advances mid-turn and that
Go can `os.Open` the file while `codex` holds it; pin the exact `codex --version` here
next to these claims, as `2.1.219` is pinned above; then reconcile
`internal/adapter/codex/testdata/` against the real file.

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

## 4. Adapter contract (v1)

One module per vendor implementing:

- `discover()` — find live/recent sessions from vendor-native data on disk
- `read(session)` — return the normalized session model (schema TBD, documented here)
- `capabilities()` — which normalized fields this vendor can actually source

The contract, the normalized schema, and a worked third-party example (how you'd add
Gemini CLI) are documentation deliverables of v1, not afterthoughts. The Go form is §4a.5
and the worked example is §4a.7.

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
extra deserves a gauge, it deserves a `Field` — propose one. *(v1 note: there is no
detail pane yet, so extras are carried and not rendered. Both adapters populate them —
git branch, CLI version, Claude's context token count, Codex's plan and history mode —
so the measurements are not discarded while the fork in §6 Q7 is open.)*

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

## 5. Eval harness

Fixture-driven, in-repo, CI-gating (`.github/workflows/ci.yml` runs `go vet ./...` and
`go test ./...` on `windows-latest`, then smoke-tests the built binary against a
statusline fixture). What it asserts today:

| Layer | Package | What is pinned |
|---|---|---|
| Statusline renders | `internal/statusline` | every segment against five stdin fixtures, including the API-key login that must render no quota |
| Framing rule | `internal/jsonl` | 0x0A-only framing, a 300 KiB record surviving the `bufio.Scanner` cap, read errors surfacing, torn tails held back, a seek fragment discarded |
| Schema gate | `internal/model` | `Validate` over every rejection case, liveness boundaries, presence semantics |
| Claude adapter | `internal/adapter/claudecode` | discovery filters, the `<synthetic>` trap, the `input_tokens` trap, torn tail invisibility, torn-only session, future mtime, capability table |
| Codex adapter | `internal/adapter/codex` | envelope + internally-tagged event parsing, derived context, quota window presence, null `rate_limits`, sub-agent rejection, capability table |
| HUD renders | `internal/hud` | 18 golden frames byte-for-byte at 52/72/80/120 columns, the §7.4 gauge table, the estimate marker, threshold colours, frame width/height invariants |
| HUD behaviour | `internal/hud` | vendor status words from adapter errors, key handling, one-scan-in-flight, spinner lifecycle |
| Doc/code sync | `internal/hud` | every render pasted into `docs/design.md` §7.3 and into `README.md` still matches its golden, and every golden is either embedded or explicitly exempted |

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
   verified live and Codex still owing §3.4.
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
| 1 | pad | 1 | | |
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
workspace basename, else the vendor session id. Then, only if ≥14 cells remain free, two
spaces and the parent directory (left-elided with `…`). The parent path disambiguates
same-named projects under different roots and stops the wide tier from opening a dead
gulf between the name and the model. It drops out automatically as the terminal narrows.

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
paginated — the footer gains `+3 more` and `↑/↓` scrolls the viewport.

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
 telltale  │  4 sessions  │  claude 3  codex 1                  5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys
```

Row 3 is the honest-gauge case in its normal habitat: a session whose adapter can source
a model and an age but not context or cost. Blank gauge field, `—` in both numeric
columns. Nothing about it looks like zero.

**B — compact (80 cols).** Cost gone, gauge halved, quota wrapped to its own line.

```
 telltale  │  4 sessions  │  claude 3  codex 1
                        5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────────────
        SESSION                           MODEL          CONTEXT            AGE
 ● CC │ telltale  C:\src\code             Opus 5         █████▉──  84.2% │  12s
 ● CC │ acme-api  C:\src\work             Sonnet 4.5     ██▉─────    41% │  48s
 ◐ CX │ notes-api  C:\src\code            gpt-5.1-codex                — │   4m
 ○ CC │ learning-notes  C:\src\code       Haiku 4.5      ██████▌─  92.6% │  22m
 ──────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   ? keys
```

**C — narrow (72 cols).** Gauge gone; the number it encoded stays. Vendor names shorten.

```
 telltale  │  4 sessions  │  cc 3  cx 1
                5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────
        SESSION                             MODEL            CTX    AGE
 ● CC │ telltale  C:\src\code               Opus 5         84.2% │  12s
 ● CC │ acme-api  C:\src\work               Sonnet 4.5       41% │  48s
 ◐ CX │ notes-api  C:\src\code              gpt-5.1-codex      — │   4m
 ○ CC │ learning-notes  C:\src\code         Haiku 4.5      92.6% │  22m
 ──────────────────────────────────────────────────────────────────────
 q quit   v vendor   ? keys
```

**D — degraded rows (120 cols).** Four distinct failure shapes in one frame. Rows are
sorted by activity, so they do not appear in the order they are described.

```
 telltale  │  4 sessions  │  claude 3  codex 1                                                 5h ███─────   42% ↻2h13m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         ────────────     0%    $0.04 │   3s
 ● CC │ a-really-long-project-name-that-overflows-the-label-column…  Opus 5         ███████████─  99.9%  $340.50 │   9s
 ◐ CX │ 4f2a9c81-1d3e-4a77-9b02-000000000000                                                          —        — │   7m
   CC │ acme-api  C:\src\work                                        Sonnet 4.5                       —    $1.02 │    —
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys
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
 q quit   v vendor   s sort   a all   r refresh   ? keys
```

`0%` is a full track of `────────────`; absent is whitespace. If these two rows ever
render the same, the build fails.

**E — stale scan (120 cols).** The scan has been failing for 47 seconds. Values are the
last ones actually measured; the whole row area renders `Muted` (invisible in a plain
golden, asserted separately), and the footer's right slot carries the notice. The header
is never used for notices — it holds identity and quota only, which keeps it from
overflowing at any width.

```
 telltale  │  4 sessions  │  claude 3  codex 1                  5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s
 ◐ CX │ notes-api  C:\src\code                                       gpt-5.1-codex                    —        — │   4m
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys                        ⚠ last scan 47s ago   Access is denied.
```

**F — filter and sort active (120 cols).** The header count reads `3 of 4` so it cannot
contradict the per-vendor totals beside it. Non-default filter/sort is stated in the
footer, because a monitor that silently hides rows is a liar.

```
 telltale  │  3 of 4 sessions  │  claude 3  codex 1             5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 ○ CC │ learning-notes  C:\src\code                                  Haiku 4.5      ██████████▏─  92.6%   $11.07 │  22m
 ● CC │ telltale  C:\src\code                                        Opus 5         █████████▎──  84.2%    $2.41 │  12s
 ● CC │ acme-api  C:\src\work                                        Sonnet 4.5     ████▌───────    41%    $0.18 │  48s

 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys                                   filter claude   sort context
```

**G — empty (120 cols).** Distinguishes "watching, found nothing" from "vendor not
installed". Two different facts; two different words; never a fake row and never an
error dialog.

```
 telltale  │  0 sessions
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                   no active sessions

                                 claude   watching       %USERPROFILE%\.claude\projects
                                 codex    not detected   %USERPROFILE%\.codex


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys
```

The vendor status word is one of exactly three: `watching` (directory exists and is
readable), `not detected` (directory absent), `unreadable` (directory exists, the OS
refused — rendered `SevWarn` with the OS error appended). On the dev machine today the
Codex line reads `not detected`, since `~/.codex` is absent (§3.2). The third word:

```
 telltale  │  0 sessions
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
                                                   no active sessions

                       claude   unreadable     %USERPROFILE%\.claude\projects  Access is denied.
                       codex    not detected   %USERPROFILE%\.codex


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys
```

**H — help overlay (120 cols).** Replaces the row area rather than floating over it; a
floating panel on a monitor obscures the thing being monitored.

```
 telltale  │  4 sessions  │  claude 3  codex 1                  5h ███─────   42% ↻2h13m   │   7d █▎──────   18% ↻5d02h
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────

        q  quit  (also esc, ctrl+c)
        v  vendor filter: all -> claude -> codex
        s  sort: activity -> context -> cost
        a  show all (include sessions idle > 8h)
        r  rescan now
        ?  close this help
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 ? close
```

**I — the v1 capability mix (120 cols).** What the real adapters actually render today:
Claude sources neither context nor cost from disk, Codex sources a **derived** context
percentage (marked `~`) and real quota windows. Nothing sources cost, so the `COST`
column auto-hides and its width returns to `SESSION`.

```
 telltale  │  2 sessions  │  claude 1  codex 1                                                 5h ██████▎─ 88.4% ↻3h02m
 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
        SESSION                                                               MODEL          CONTEXT                AGE
 ● CC │ telltale  C:\src\code                                                 Opus 5                           — │  12s
 ● CX │ example-app  C:\src\code                                              gpt-5.1-codex  ███████▋──── ~69.8% │   1m


 ──────────────────────────────────────────────────────────────────────────────────────────────────────────────────────
 q quit   v vendor   s sort   a all   r refresh   ? keys
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
 q quit   v vendor   s sort   a all   r refresh   ? keys
```

**L — ASCII glyph mode (120 cols).** `--ascii`, `TELLTALE_ASCII=1`, or a non-terminal
output target. Absent renders `n/a`; the gauge loses its eighth-cell partials, which is a
real precision loss in the bar and acceptable only because the number beside it carries
the precision.

```
 telltale  |  4 sessions  |  claude 3  codex 1                  5h ###-----   42% ~2h13m   |   7d #-------   18% ~5d02h
 ----------------------------------------------------------------------------------------------------------------------
        SESSION                                                      MODEL          CONTEXT                 COST    AGE
 * CC | telltale  C:\src\code                                        Opus 5         #########---  84.2%    $2.41 |  12s
 * CC | acme-api  C:\src\work                                        Sonnet 4.5     #####-------    41%    $0.18 |  48s
 o CX | notes-api  C:\src\code                                       gpt-5.1-codex                  n/a      n/a |   4m
 . CC | learning-notes  C:\src\code                                  Haiku 4.5      ##########--  92.6%   $11.07 |  22m
 ----------------------------------------------------------------------------------------------------------------------
 q quit   v vendor   s sort   a all   r refresh   ? keys
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
| separator injection | a session name containing U+2028/U+2029 | `TestSessionNameSeparatorsCannotTearTheGrid` | The character never reaches the frame and no line exceeds the terminal width. |

Freshness escalation, stated once: **≤ 3 s** normal; **> 3 s** row area `Muted` + footer
notice in `SevWarn`; **> 60 s** notice in `SevCrit` and the header quota goes `Muted` too.
Retained values are not "presented as fresh" in any of these, because the age of the
measurement is on screen next to them — that is the condition the honest-gauge rule
actually imposes.

### 7.8 Keyboard

Minimal, and every key earns its place.

| Key | Action |
|---|---|
| `q`, `esc`, `ctrl+c` | quit |
| `v` | vendor filter cycle: all → claude → codex → all |
| `s` | sort cycle: activity → context → cost → activity |
| `a` | toggle show-all (default hides sessions idle > 8 h) |
| `r` | rescan now |
| `?` | toggle help |
| `↑`/`↓`, `j`/`k` | scroll the row viewport when it overflows |

`--vendor all|claude|codex` sets the starting filter; the cycle takes over from there.

Cycles, not multi-select menus: with two vendors and three sorts, a cycle is one keystroke
and no mode. Non-default filter or sort is always visible in the footer. There is no
selection cursor — the default sort puts the interesting sessions on top, and a cursor
invites drill-down, which is a different product.

Show-all deliberately does **not** hide a session with no activity timestamp: "we have no
signal" is not evidence that a session is old.

Deliberately absent: mouse support, search, per-session detail panes, and configuration
UI. And one invariant that outranks all future feature requests: **the HUD is strictly
read-only. No keybinding may ever mutate vendor state or send anything to a running
agent.** telltale is a telltale.

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

- Every glyph in the visual language — `● ◐ ○ ─ │ █ ▏▎▍▌▋▊▉ … — ↻ ⚠` — is
  East-Asian-**Ambiguous** width. Windows Terminal, the reference environment, renders
  ambiguous as narrow, which is what the grid assumes. A terminal configured to render
  ambiguous glyphs double-width will shear the layout; `--ascii` is the escape hatch.
  Stated here rather than discovered later.
- The `█` fill and `─` track differ in glyph height by design. Verified legible in
  Cascadia Mono; other fonts may render the step more harshly.
- Fill resolution is one eighth of a cell (1.04% at 12 cells). The number beside the bar
  carries the precision; the bar carries the glance.
- The account quota block is sourced from one session (§7.1). A second quota-bearing
  vendor needs a per-vendor block.
- The 1 s poll has not been measured on a cold cache over an 837-session tree (§6 Q3).
