# 005 — Adoption direction: multi-harness attention routing as product strategy

Status: Proposed (2026-08-02) — design checkpoint; merges only after San's review

## Context

Direction brief from the cross-agent channel (2026-08-02, Codex → Claude, San agreed in
conversation): telltale should pursue meaningful external adoption, not only serve as
portfolio proof + personal utility. Proposed positioning: **"One local HUD for every
coding agent you use"** — category analogy **"btop for coding agents"** — with the
concrete job being attention routing: *which of my running agent sessions is working,
blocked, finished, stale, nearing a limit, or waiting for me?*

Relationship to ADR-001: this is a re-weighting, not a reversal. 001's bar already
includes "real users"; the 500★ drop reset the *metric*, not the ambition to be used.
This ADR adds a pre-registered external-adoption bar and promotes adoptability from
lagging indicator to design input. Everything in 001 stands, including the honest-gauge
contract and the done-for-v1 sequence.

An evidence pass ran 2026-08-02 before this ADR was drafted (per the brief's request for
the smallest evidence-backed plan). Findings below; all claims verified against live
GitHub state on that date.

## Evidence

### Demand for multi-harness coverage is real and observed, not projected

- **ccusage (~17.7k★), the largest usage-monitor incumbent, went multi-harness by
  shipping it**: official `@ccusage/codex`, `@ccusage/opencode`, and `@ccusage/copilot`
  sub-packages exist; 103 issues in its tracker match codex/gemini/opencode queries.
  Top asks by reactions include "Is it possible to support codex?" (#626) and "Support
  for opencode" (#757).
- **Both single-vendor incumbents wanted cross-harness coverage and were blocked by the
  missing vendor hook.** claude-hud (~27k★) issue #215 "Are we planning to create a
  codex-hud?" — the maintainer: "I originally planned to make one but IIRC is not
  supported by them currently, maybe when Codex plugins are officially released I can
  revisit." ccstatusline (~12k★) issue #511 "Codex support" — the maintainer: "Sadly
  they don't have an interface to call a custom command to render the statusline yet."
  The blocker that stops statusline-shaped tools is precisely the seam telltale's HUD
  does not need: it reads vendor session files instead of waiting for a hook (ADR-001's
  Codex fork).
- **Antigravity coverage is asked for and unserved.** ccusage issues #1402, #1488, #1194
  request agy support; #1402 was auto-closed with users asking maintainers to reopen.
  No tool in the lane ships it.
- **Honest accounting is a live pain point, not a theoretical differentiator.** ccusage's
  Codex tracker includes "Massive token overcounting for Codex subagent sessions (91x
  inflation)" (#950) and fork-replay double-counting (#897, #1460). Measured-only values
  with visible degradation is a defensible trust position in this lane.

### The lane is contested but not won

- **abtop** (graykode, created 2026-03-29) is the leader at **3,411★**: Rust/Ratatui,
  monitors Claude Code + Codex CLI + OpenCode, native Windows support, prebuilt
  binaries. The rest is a long tail: agtop 195★, lazyagent 175★, ccboard 88★,
  sidekick-agent-hub 76★, tokentop 64★, marmonitor 27★, aitop 13★, and more.
- This mirrors the statusline lane's structure (one leader + fragmentation), and the
  leader is at 3.4k, not claude-hud's 27k — while the best-claude-hud precedent (2.3k★
  in ~5 weeks against a 27k★ incumbent) shows late entrants get adopted in this
  ecosystem when they differentiate.
- **abtop confirms the honest-gauge gap**: its session summaries are generated via
  `claude --print` — the monitor spends the user's own tokens and quota to describe the
  user's sessions. telltale is passive read-only by contract, makes zero API calls, and
  distinguishes absent from zero. No tool in the lane covers the Gemini family.
- **Vendor consolidation pressure is real but single-vendor.** Claude Code Agent View
  (official, 2026-05) orchestrates Claude sessions only. Orchestrator products
  (JetBrains Air, Superset, agentgrid, Claude Command Center) *own* the sessions they
  display; telltale observes sessions the user runs natively, in the tools they already
  chose. The brief's rule stands: do not compete as an orchestrator.

### New seam evidence: the agy disk seam may have reopened

ccusage #1402 documents that agy ≥ 1.0.4 stores sessions in SQLite `.db` files whose
`gen_metadata` table exposes input / cache-read / output / thinking token counts, a
response model per generation, and per-turn rows dedupable by `responseId`; a community
contributor reports a working local parser. ADR-004 ruled the disk seam closed based on
the 1.1.9 survey's verdict that the protobuf blobs were opaque — this evidence says they
are parseable. If a re-survey against the local corpus verifies it, telltale can ship
the lane's only Antigravity HUD adapter.

## Decision

1. **External adoption becomes an explicit product goal** alongside 001's
   portfolio-evidence bar. Positioning: "One local HUD for every coding agent you use";
   category: btop for coding agents. Differentiation, all evidence-backed: the honest
   gauge (passive, zero API calls, absent ≠ zero, visible degradation), the broadest
   honest harness coverage (only tool with Gemini-family support), statusline + HUD in
   one binary, and the attention-routing job as the center.
2. **Pre-registered adoption bar: 10 evidenced external users within 30 days of the
   launch post.** Evidence = an issue, discussion, PR, or unsolicited mention/screenshot
   from a distinct external user; a star does not count. Recorded here, before launch,
   so the outcome is falsifiable either way. Stars remain weather (001).
3. **Sequencing: 001's order stands** — dogfood → eval + design doc → launch post. The
   activation slice runs in parallel during the dogfood window: prebuilt binaries for
   Windows/macOS/Linux (goreleaser), one-command install (scoop/winget first, per
   Windows-first), a README hero visual, and a useful zero-config first frame. The
   launch post doubles as the hypothesis experiment: it leads with the multi-harness
   positioning, and the adoption bar measures whether that is what resonates.
4. **First post-validation feature investment is needs-input/blocked/done state** — the
   attention-routing job — where vendor seams support it (Claude Code hooks, Codex
   notify events, agy `agent_state`). Not before the launch experiment reads out.
5. **The agy disk-seam re-survey is scheduled as the next adapter work item**, upgrading
   ADR-004's passive watch item to active on the #1402 evidence. Verification runs
   against the local 1.1.9 corpus before any coverage claim is made.

## Rejected

- **Competing as an orchestrator or session manager** (Agent View / JetBrains Air /
  Superset lane): those products own the sessions they show; telltale's value is being
  the read-only layer above tools the user already chose.
- **Repositioning around cost accounting**: ccusage owns that lane at 17.7k★. telltale's
  job is attention, not bookkeeping; cost stays one segment among several.
- **A machine-readable `snapshot --json` router surface**: still parked. The cross-tool
  strategy routes work by type, not by quota; no routing rule consults a gauge today.

## Downstream surfaces

- `docs/design.md` §8 roadmap: activation slice, needs-input feature, agy re-survey —
  updated after this ADR merges, not in this PR.
- README positioning line — lands with the activation slice, not before.
- Launch post — carries the pre-registered bar and the honest-gauge differentiation.
- `decisions/README.md` index — this PR.
- Operating memory + cross-tool strategy doc — telltale remains the capacity-friction
  observation instrument; unaffected.
