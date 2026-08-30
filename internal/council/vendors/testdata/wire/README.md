# Wire fixtures — each vendor's real stream, sanitized and version-pinned

Captured **2026-08-09** on the Windows 11 reference box, one short real turn per
vendor, through the exact argv the seat in `internal/council/vendors` builds. The
prompt was `reply with the word ok` in every case — the cheapest thing that still
produces a whole turn. `wire_test.go` replays each file through that seat's own
parser.

**grok's file was re-captured 2026-08-14 at 1.0.4**, and its prompt is FENCED
rather than bare: the same `---` opening that a briefed room sends. See "The
alarm fired once" below.

These exist for one reason: **a vendor can change its frames between a rehearsal
and demo day, and nothing else in this repository would notice.** Every other
fixture in this package is a shape somebody wrote down, including
`../cursor-acp-turn.jsonl`, which design.md §9.36 correctly calls synthesized.
A synthesized line proves the parser handles the shape its author believed in;
it cannot prove the vendor still sends it.

## The version is in the filename, and that is the mechanism

A fixture is a claim about **one build of one CLI**. When a vendor is upgraded,
capture a NEW file under the new version's name — never edit an old one, which
would silently restate a measurement nobody re-ran. Each version below was read
from the vendor's own `--version` at capture time.

| File | Vendor build |
|---|---|
| `claude-2.1.226-turn.jsonl` | `claude` 2.1.226 |
| `claude-2.1.226-resume-not-found.jsonl` | `claude` 2.1.226 |
| `codex-0.147.0-turn.jsonl` | `codex-cli` 0.147.0 |
| `agy-1.1.11-turn.jsonl` | `agy` 1.1.11 |
| `grok-1.0.4-turn.jsonl` | `grok` 1.0.4 (d846eb93d9) |
| `cursor-agent-2026.08.04-aaa8809-turn.jsonl` | `cursor-agent` 2026.08.04-aaa8809 |
| `cursor-agent-2026.08.04-aaa8809-load-not-found.jsonl` | `cursor-agent` 2026.08.04-aaa8809 |
| `codex-app-server-0.149.1-turn.jsonl` | `codex-cli` 0.149.1 |

**Two files pin one CLI, and that is not a stale entry.** `codex-0.147.0-turn.jsonl`
is `codex exec --json` — the invocation the room seats — and
`codex-app-server-0.149.1-turn.jsonl` is `codex app-server`, a second protocol
that ships parsed and unseated (design.md §9.50). They are different surfaces of
one binary, so a bump retires them independently; when the seat moves, the exec
pin goes with it.

Three of the five bumped past the version their adapter's doc comments were
written against — Claude Code 2.1.220 → 2.1.226, codex-cli 0.146.0 → 0.147.0,
agy 1.1.10 → 1.1.11 — which is the drift these files exist to make visible.

## The alarm fired once, and this is what it found (2026-08-14)

grok reached **1.0.4 (d846eb93d9)** — four patch versions past the pin — with
nothing in this repository noticing. That is the case these files were added for,
so the seat was re-measured against the installed binary rather than re-pinned by
editing a string.

**The wire is unchanged.** The 1.0.4 capture was diffed against the 1.0.0 one by
shape rather than by eye: frame types, key names, nesting and ordering are
identical. The only shape difference anywhere in the two files is the KEY of the
`modelUsage` map, `grok-4.5-build` → `grok-4.6-build` — a model id, not a schema
key, and one no adapter here reads. `grok models` now offers `grok-4.6` (default)
and `grok-4.5`, where the 1.0.0 survey found one model.

**The old file is replaced rather than kept, and that is not the same as editing
one.** The rule above forbids editing an old capture, because that restates a
measurement nobody re-ran; it does not ask for one file per build forever. 1.0.0
is gone from every box this project runs on, so its fixture pins a claim nobody
can re-check, and its shape is a subset of the new one's. Git history holds it.
What must never happen is the third option — keeping `grok-1.0.0-turn.jsonl` as
the name while putting 1.0.4 bytes in it.

**The prompt changed on purpose.** This capture is FENCED — it opens with the
`---` a briefed room sends — where the other four are the bare `reply with the
word ok`. design.md §9.39 records a seat that shipped broken for a day behind a
green live test whose prompt began with a letter, and a capture is a probe like
any other. The reply is still `ok`, so nothing downstream moved.

## How they were sanitized

The repository is public and `CLAUDE.md` is unambiguous: fixtures are
synthesized, never real. So the SHAPE here is real and every VALUE that could
identify anything is not.

Substitution was **textual, never a re-marshal**, so keys, nesting, ordering and
types are byte-identical to the capture — a JSON round trip would have reordered
keys, and Claude Code's `result` frame carries its `"type"` key last, which is
exactly the kind of detail a fixture is supposed to preserve.

What was replaced, each by an obviously-fake value of the same type and format:

- **Session, thread, conversation, request and hook ids**, and every other UUID —
  remapped consistently per file to repeated-digit UUIDs that keep the original's
  version and variant nibbles (`22222222-2222-7222-9222-222222222222` is a v7 id).
- **Paths and the username** — the capture directory becomes
  `C:\Users\dev\code\example-app`.
- **Anthropic message and request ids** (`msg_01…`, `req_01…`) and tool-use ids
  (`toolu_…`).
- **MCP tool names**, which are namespaced `server__tool` and so name what the
  operator has connected. They become `example-mcp__tool_NN`. The *count* is
  left alone deliberately, because it is vendor shape: grok's third
  `available_commands` frame of the turn grows from 26 builtin tools to 54 as
  the MCP servers finish loading, mid-turn, which is a thing this seat's parser
  has to keep ignoring.
- **The operator's own inventories** — Claude Code's `slash_commands`, `skills`
  and `agents` arrays, grok's `commands` array, and Cursor's
  `availableCommands`. These are personal configuration, not vendor shape.
- **Two account-state values** on Claude Code's `rate_limit_event`: `resetsAt`
  is a fixed epoch and `overageDisabledReason` reads `sanitized`. The keys and
  types are the shape; those two values described a real billing state.
- **grok's opaque `signature` blob**, replaced with a same-length placeholder.
- **The app-server capture's own extras.** Its `userAgent` echoes the client
  name and the OS build back, its `installationId` and `serverName` name this
  machine, and its `hook/*` frames name the operator's own hook scripts by path.
  All are replaced. Every `resetsAt`, `emittedAtMs`, `createdAt` and friend is
  fixed to one epoch, for the same reason Claude's `resetsAt` was.

**One deliberate exception in the app-server capture, stated because it is a
real judgement call.** The `account/rateLimits/updated` frames keep their
`usedPercent`, `windowDurationMins` and `planType` VERBATIM. Those describe a
real account's quota state, which is the kind of value the rule above replaces —
but they are also the measurement the file exists to pin, and a substituted
percentage would make the fixture assert a number nobody read. The reset epochs,
which are the part that would let a reader date the capture against an account,
are fixed. What is left is "some account was 15% into a 300-minute window",
which is shape.

What was deliberately **left verbatim**, because it is the measurement:

- Claude Code's `system/init` `tools` array. The turn ran under the seat's read
  posture, so that array holding only read tools is the standing evidence for
  `claude.go`'s argument that `--disallowedTools` removes tools where
  `--allowedTools` does not.
- Every token count, duration and cost figure, and every vendor tool inventory.

## What could NOT be captured, and why

**Nothing in this directory is written from documentation.** A frame that no run
produced is recorded here as absent, not invented.

- **Codex has no structured error frame.** Re-measured at 0.147.0: a resume
  against a thread id it does not hold writes `Error: thread/resume: … no
  rollout found … (code -32600)` to **stderr**, exits 1, and puts **zero bytes**
  on stdout. `codex.go` says this and it is still true — the exit code and the
  stderr tail are the whole of the failure signal, so there is no stdout shape to
  pin.
- **Grok has no structured error frame either.** Same probe, same result at
  1.0.0: a bad `--resume` id exits 1 with an empty stdout and
  `Error: Failed to restore session from remote: … 404 Not Found` on stderr.
- **Antigravity has no error frame reachable this way, and the reason is a
  finding rather than a gap.** `agy --conversation <a uuid it has never seen>`
  does not fail. It **silently opens a new conversation**, answers normally,
  reports a DIFFERENT `conversation_id`, `status: "SUCCESS"` and exit 0. So the
  room cannot tell a resumed thread from a lost one on this vendor by reading the
  stream — there is no error to capture because the vendor does not raise one.
  The one tell that IS on the wire — the returned id is not the requested one —
  is now read by the room rather than left as a note (design.md §9.43). The
  fixture that exercises it is `../agy-forked-conversation.jsonl`, deliberately
  outside this directory: it is this capture with only its `conversation_id`
  values changed, and a hand-edited file among these would restate a measurement
  nobody re-ran.
- **No zero-token or empty-usage turn was observed on any vendor**, so none is
  fixtured. The one measured ZERO shape in this directory is Claude Code's
  resume-not-found frame, which carries `total_cost_usd: 0`, an all-zero usage
  block, `modelUsage: {}` and `iterations: []` — a measured zero rather than an
  absence, which is the distinction §4a.1 exists to keep.
- **Nothing here is from macOS.** Every capture is Windows 11. Whether a Mac's
  frames match is unmeasured; `PARITY.md` is where that would be recorded.

## One thing these fixtures surfaced

Claude Code's resume-not-found frame carries the vendor's own sentence in an
`errors` array — `["No conversation found with session ID: …"]` — which
`claude.go`'s `streamLine` did not model when the capture landed. The turn was
still reported as failed, correctly, but with the generic note "the vendor
reported the turn failed" rather than the vendor's own words. The capture PR
recorded that here rather than fixing it, because reading a new field is a
behaviour change and this directory is a measurement; a follow-up change then
modelled the field, so the column now shows the vendor's sentence, and the
wire test asserts it.
