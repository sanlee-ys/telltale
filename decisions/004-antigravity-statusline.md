# 004 — Antigravity CLI: statusline vendor, not (yet) a HUD vendor

Status: Accepted (2026-08-02)

## Context

ADR-003's addendum recorded the market shift: Gemini CLI went enterprise/API-key-only
on 2026-06-18 and consumers moved to Antigravity CLI (`agy`), and San ruled agy the
consumer-facing Gemini-family capture target. agy 1.1.9 was installed and surveyed the
same day (design.md §3.8). The survey split cleanly:

- **Disk seam: closed.** Conversations are SQLite databases of protobuf blobs; the
  transcript.jsonl that both the docs and the live payload advertise is never written.
  Parsing undocumented protobuf is guessing; a SQLite dependency was already rejected
  once (§3.2). Building HUD rows from this would violate decisions/001 twice over.
- **Statusline seam: open, documented, and richer than Claude's.** Verified against a
  six-payload live capture: named weekly quota buckets with remaining fractions and
  reset times, full context accounting with the window size, and `agent_state` — the
  first vendor-reported liveness signal available on any seam this product reads.

San chose to build the statusline support immediately.

## Decision

- `telltale statusline` serves two vendors from one subcommand. Routing is the
  documented `product` field ("antigravity" stamped on every observed payload; absent
  from Claude's payload) — an affirmative marker, no flag, no heuristic.
- New `internal/antigravity` parse package (mirror of `internal/claude`) and a
  parallel `RenderAntigravity` path in `internal/statusline` sharing the helpers. A
  normalized common input was rejected: the payloads disagree about what exists
  (named buckets vs fixed windows, reported state, in-payload vcs), and flattening
  them would invent fields or drop vendor truth.
- Segments: model, vendor-reported context %, one segment per quota bucket (ids
  verbatim, sorted; used% = (1−remaining)×100, a unit conversion), agent state with
  `tool_confirmation_pending` outranking it as `confirm?`, in-payload vcs branch, and
  folder. `cost` is nowhere in the payload and renders nowhere.
- **No HUD adapter, no VendorID, no golden churn.** agy does not appear in the HUD at
  all — absence, not a row of em dashes for a vendor whose disk cannot be read
  honestly.
- **Watch item (standing):** the payload's `transcript_path` says the vendor intends
  the transcript file. When agy starts writing it, a HUD adapter becomes buildable via
  the Claude-precedent method, and §3.8 already records where to look.

## Consequences

- The public claim upgrades honestly: "vendor-native statusline where the seam exists
  — and it exists twice." Codex stays the no-statusline counterexample; Antigravity is
  the no-disk counterexample. Every vendor in the README now demonstrates a different
  honest boundary.
- The statusline is no longer Claude-coupled in `cmd/telltale`: stdin is read once and
  routed. The single-digit-ms budget is unchanged (one extra Unmarshal of a ~2 KB
  payload).
- Live captures of the agy payload contain the signed-in email and plan tier: real
  payloads are PII and never enter `testdata/`; fixtures are synthesized to shape.

## Downstream surfaces

design.md (§2.1 new, §3.8 new, §5 eval row, restored §4 heading), README.md (status,
statusline wiring, honest claim), decisions/README.md index, memory: telltale-project.
