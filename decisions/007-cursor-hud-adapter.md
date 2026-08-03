# 007 — Cursor (Composer) becomes the fifth built-in adapter, behind a read allowlist

Status: Accepted (2026-08-02)

## Context

A read-only survey of Cursor 3.14.7 (design.md §3.9) found an open disk seam: one SQLite
store, `%APPDATA%\Cursor\User\globalStorage\state.vscdb`, with a `composerHeaders` table
of one row per Composer session and a `cursorDiskKV` key/value table whose
`composerData:<uuid>` rows carry the model and — a first for this project — **a context
percentage the vendor computed and wrote down itself**.

It also found something no previous vendor's store had: **live credentials in the same
file**. `ItemTable` holds `cursorAuth/accessToken`, `cursorAuth/refreshToken`,
`mcpOAuth.secret.*` and git-IPC auth tokens; the per-session blobs hold encryption keys.
"Read Cursor's store" and "read the user's tokens" are the same sentence unless an
adapter is explicit about what it will not touch.

§8's adoption track named the next lane; San greenlit the build.

## Decision

`internal/adapter/cursor` ships built-in: wired into the HUD's adapter slice, the vendor
filter cycle, the `--vendor` flag, the header counts and the empty-state vendor table.
`VendorCursor` is `cursor` — the product's name rather than the surface's, because
`composer` names a pane inside an editor most people call Cursor, and the HUD's job is to
say which *tool* a row came from. The grid tag is `CU`.

**Capabilities.**

| Field | Verdict | Source |
|---|---|---|
| `name` | reported | `composerHeaders.value.name`, the vendor's own generated title; the composerId's first eight characters when it wrote none |
| `model` | reported | `composerData.modelConfig.modelName` — one string, used as both id and display name |
| `workspace` | reported | `composerHeaders.workspaceId` → `workspaceStorage/<id>/workspace.json` → `.folder`, a `file:///` URI converted to a native path |
| `context_pct` | **derived (declared); reported in practice** | `composerData.contextUsagePercent` verbatim, unmarked. Computed from `contextTokensUsed ÷ contextTokenLimit` and MARKED only when the vendor's own percentage is missing. See ruling 3. |
| `last_activity` | reported | the newest of `lastUpdatedAt` / `recency` / `checkpointAt`, each through the future-skew guard. See ruling 4. |
| `cost` | **CapNone** | see ruling 2 |
| `quota` | **CapNone** | see ruling 2 |
| `liveness` | **CapNone** | deferred, ruling 5 |
| `subagents` | **CapNone** | deferred, ruling 5 |

Six rulings hold it up.

### 1. A read allowlist, asserted by a test rather than promised in a comment

The adapter reads exactly this and nothing else:

- `composerHeaders` — the columns `composerId`, `workspaceId`, `lastUpdatedAt`,
  `isArchived`, `isSubagent`, `recency`, `checkpointAt`, and two fields out of the
  `value` JSON (`name`, `isDraft`);
- `cursorDiskKV` rows whose key has the exact prefix `composerData:` **and** whose id
  belongs to a session that survived filtering, and within those rows four named JSON
  fields (`modelConfig.modelName`, `contextUsagePercent`, `contextTokensUsed`,
  `contextTokenLimit`);
- `workspaceStorage/<id>/workspace.json`.

It never walks `ItemTable`. It never reads `bubbleId:*` or `ofsContent:*` — the message
payloads. Nothing from `value.subtitle` (a list of the files a turn touched), message
text, todos or tool-call payloads reaches a Session field, an Extra, a Diagnostic or a
log line. The Go structs the blobs decode into contain only the named fields, on the
Antigravity precedent: **a field that is never decoded cannot be accidentally rendered.**

The honest caveat, stated rather than buried: a b-tree walk visits the rows of the table
it walks, so filtering `cursorDiskKV` by key prefix happens *after* a row is decoded —
there is no index to seek with. `sqlite.Rows` (ruling 6) exists so that walk retains
nothing: a row that is not on the allowlist is looked at and dropped, never copied out.
`ItemTable` is not walked at all.

The fixtures plant three marker strings — prompt text, credential-shaped `ItemTable`
values, and the plan-entitlement constants — and a test asserts none of the three reaches
any field, extra or diagnostic of any session the adapter produces. In the real store two
of those markers are live access tokens.

### 2. Cost and quota are CapNone, and the entitlement constants are not read at all

**Cost.** `usageData` was `{}` in every session surveyed, and per-message
`tokenCount.inputTokens`/`outputTokens` read **0 in all 310 message rows**. The schema is
present and never populated. A zero that really means "unpopulated" must not render as
`$0.00` or as `0 tokens` — that is the honest-gauge rule's exact failure mode, and it is
worse than an em dash because it looks like information. No cost field, and no
cost-shaped extra either.

**Quota.** No consumption record exists on disk anywhere. What *is* there is plan
**entitlement**: `credit_dollars: 25`, `included_usage_dollars: 40`. Those are what the
plan grants, not what has been spent, and rendering an entitlement in a gauge labelled
usage would be a lie with a number on it. The adapter does not read them — not "reads
them and discards them", does not read them. They live in `ItemTable`, so ruling 1
forbids them independently, and that redundancy is deliberate.

### 3. `context_pct` is declared derived and marked per read

Cursor persists `contextUsagePercent` and telltale reads it verbatim, unmarked: it is the
number the vendor itself shows, computed by whoever knows what counts against that
window. When it is missing and both raw token fields are present, the adapter computes
`used ÷ limit` instead — and *marks* it, because that arithmetic is this program's and
not the vendor's reading.

`Capabilities` is a static per-vendor declaration and the reported/derived sets are
disjoint by construction, so there is no way to say "reported when the vendor wrote it
down, derived when it did not". Declaring the **weaker** of the two claims is the safe
direction: the HUD never promises more than the adapter can guarantee, and `Session`'s
per-read `Derived` set tells the truth row by row. In the `v1-capabilities` render the
Cursor row carries a bar with no `~` beside the Codex row's `~69.8%`, and that contrast
is exactly the distinction the schema exists to preserve.

### 4. `last_activity` drops the file mtime, and the reason is that the file is shared

§6 Q8 pins `last_activity` per adapter as `max(file mtime, newest record timestamp)`.
Cursor is the first vendor where the first half of that is wrong. Every other vendor
gives a session its own file, so that file's mtime dates that session. Cursor gives every
session **one** file: its mtime says when Cursor last wrote *anything*, which is
continuously while the editor is open. Folding it in would mark every Cursor row live
forever, including the ones abandoned last week — a number that is always fresh and never
true.

So the fold runs over the three per-row timestamps only, each through the future-skew
guard, and degrades to absent when none of them is readable. The Q8 *rule* is unchanged;
what changed is that one of its two inputs does not exist for this vendor.

The timestamps are read with a decoder that accepts epoch milliseconds **and** ISO-8601,
because the columns are integers today and the schema is undocumented and unversioned
(§3.9 finds ISO-8601 elsewhere in the same store). The finer-grained per-message
timestamps at `composerData.fullConversationHeadersOnly[].createdAt` are deliberately not
read: they are outside the allowlist, and widening the allowlist to buy precision is
exactly the trade this seam must not make.

### 5. Liveness and sub-agents are deferred; Hooks is the seam, and it is documented

`composerData.status`, `generatingBubbleIds` and `hasBlockingPendingActions` look like
liveness and needs-input signals. Every one of them read terminal or empty across the
corpus and **no session was ever sampled mid-generation**, so the mapping from a status
code to "working now" is untested — and §4a.4 sets a high bar for a `LivenessHint`
precisely because it is the one place an adapter can lie undetected. The HUD classifies
age from `last_activity`, same as every other vendor.

The watch item is **not** "sample an in-flight session and map `status`". Cursor
documents Hooks (cursor.com/docs/hooks): a supported, versioned payload carrying
`conversation_id`, `model`, `workspace_roots` and `transcript_path`, with context numbers
on `preCompact`. That is where a needs-input signal for this vendor should come from, and
reverse-engineering an undocumented field when a documented contract exists would be
building the fragile version of something the vendor already supports.

`isSubagent`, `numSubComposers` and `subComposerIds` were zero and empty on every row.
The structure exists; the observation does not, so the field is `CapNone` rather than a
declared zero. The fields are still *used* — a row marked `isSubagent` is dropped from
top-level discovery, which costs nothing and is right whenever the observation changes.

Also out of scope and recorded as such: the `cursor-agent` CLI keeps its own separate
store. It is not installed on the survey machine, so it is an unverified surface and
nothing is claimed about it.

### 6. `internal/sqlite` was extended, not worked around

Two additions, each with its own tests:

- **`Rows(name, fn)`** streams a table to a callback that may stop the walk, retaining
  nothing. `Table` is now written in terms of it. This is what makes ruling 1's allowlist
  cheap *and* narrow: reading eight keys out of a key/value table without materializing
  the thousands of rows around them.
- **`Columns(name)`** splits the column list out of the CREATE statement `sqlite_master`
  already stores, so the adapter addresses `composerHeaders` columns by **name**.
  Positional indices were the alternative and they are the wrong contract for an
  unversioned schema: a store that gains a column keeps parsing and starts meaning
  something else. This is not the SQL parser the package doc rules out — no types are
  interpreted and no expressions evaluated — and a statement it cannot read confidently
  yields nothing rather than a guess.

An unrecognized schema (no `composerHeaders`, or a missing column) is reported as a typed
`ErrSchemaMismatch` from `Discover`, which renders the vendor line `unreadable` with the
reason. **It is deliberately an error and not an empty result:** the format is
undocumented and unversioned, so the day Cursor renames a table this adapter must say "I
cannot read this". Reporting zero sessions instead would tell the user their agents are
idle, which is a wrong answer rather than a missing one.

## Consequences

- **"Cross-vendor" now means five vendors and six HUD lanes**, and Cursor is the first
  IDE-resident agent among them — the previous four are CLIs.
- **The vendor status word `unreadable` widens** from "the OS refused" to "the vendor's
  data is there and the adapter cannot read it", which now includes an unrecognized
  schema. §7.7's definition is updated in place.
- **The help overlay's vendor cycle wraps onto a second line.** ADR-006 bought room by
  changing the separator from `->` to `>`; the sixth vendor exhausted that, and
  `TestNoLineExceedsTheTerminalWidth` failed the build at 60 columns rather than shipping
  a sheared overlay. Shortening the vendor names was rejected: an overlay that teaches a
  name the footer does not print is worse than a two-line list.
- **The README hero grew a seventh row** and its caption gained one sentence, because the
  `CU` row sitting directly above the `CX` row is the clearest statement of the reported/
  derived distinction the project has managed — same bar, one marker.
- **Discover is not stat-only for this vendor**, and cannot be: there is no directory of
  sessions, only rows in one database. The parse is cached per store, keyed on the size
  and mtime of the database and its sidecar, and shared with the `Read` calls that follow
  — a poll tick on which Cursor has not written costs two stats. The main file's bytes
  are cached across ticks separately from the sidecar's, because the main file only moves
  on a checkpoint.
- **Binary fixtures gain the `.vscdb` extension** in `.gitattributes`. The `-wal` entry
  matters more here than anywhere else in the repo: one fixture's main file is a single
  empty page and every byte of its content is in the sidecar, so a rewritten sidecar is
  not a degraded fixture, it is an empty one.
- Verification posture: the adapter is **live-verified**, not source-verified — Cursor is
  closed source, so this follows the Claude Code and Antigravity precedent. Five sessions
  were read at merge time against the real store *with Cursor running* and a 4.6 MB live
  sidecar, all passing `Validate` with an empty degraded set. What that does **not**
  cover is itemized in §3.9: no in-flight session, no fan-out, no live exercise of the
  derived-percentage path, and one machine, one day, one Cursor version.

## Downstream surfaces

design.md (§3.3 matrix gains a Cursor column + a paragraph, §3.9 new survey section, §5
eval table ×2 rows, §7.3 renders G/H/I re-spliced, §7.7 status-word definition, §7.8
keyboard cycle + `--vendor` line, §8 adoption item 5), README.md (status, hero frame +
caption, adapter list, flags), decisions/README.md index, HUD goldens ×5,
`.gitattributes`, `internal/sqlite` (`Rows`, `Columns`), memory: telltale-project.
