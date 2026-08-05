# 005 — Adoption direction: multi-harness attention routing as product strategy

Status: Accepted (2026-08-02) — reviewed at the design checkpoint; adoption bar ratified as proposed

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
2. **Pre-registered adoption bar: 10 run-evidenced external users within 30 days of the
   launch post.** Evidence must show the person actually ran telltale: a version-bearing
   bug report, a real-session screenshot, a PR grounded in running it, package-manager
   feedback, or an explicit unsolicited statement of use. Engagement without run-evidence
   (a comment, a question, a hot take) does not count; a star does not count. Recorded
   here, before launch, so the outcome is falsifiable either way. Stars remain weather
   (001). *(Tightened from "evidenced" to "run-evidenced" at review — the bar measures
   usage, not engagement.)*
3. **Sequencing: 001's order stands** — dogfood → eval + design doc → launch post. The
   activation slice runs in parallel during the dogfood window: prebuilt binaries via
   goreleaser, one-command install (scoop/winget first, per Windows-first), a README
   hero visual, and a useful zero-config first frame. macOS binaries are cross-compiled
   and shipped labeled **"smoke-verified on macOS — Windows is the continuously verified
   target"**; Linux binaries remain **"built, not verified"** (this amends ADR-002's "no
   macOS/Linux work until v1" for *distribution only*; the no-porting/no-verification-effort
   rule stands, and both labels are 001's flagged-limitation pattern). *(Label amended
   2026-08-03 for macOS only — see the second amendment below.)* The launch post doubles
   as the hypothesis experiment,
   and **the hypothesis it tests is cross-harness visibility** — do multi-harness power
   users want one honest local HUD across the agents they already run? That is what the
   launched product contains. The launch does not claim to test attention routing
   (see 4).
4. **First post-validation feature investment is needs-input/blocked/done state** — the
   attention-routing job — where vendor seams support it (Claude Code hooks, Codex
   notify events, agy `agent_state`). Not before the launch experiment reads out.
   Attention routing then gets its own experiment once that slice ships; the launch
   experiment's result is read as evidence about cross-harness visibility only, and
   neither validates nor falsifies a capability the launched product did not contain.
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

## Amendment — 2026-08-02: external review reconciliation

A cross-agent review (Codex, same day, delivered after merge) returned three findings;
all three were accepted. San ruled the two product forks:

1. *The launch experiment cannot validate a feature deferred until afterward.* Ruled:
   narrow the launch hypothesis to cross-harness visibility (decision 3 as amended);
   attention routing gets its own experiment after the needs-input slice ships
   (decision 4 as amended). Rejected alternative: building an attention-state slice
   pre-launch, which would stall the running dogfood clock and stack two variables into
   one experiment.
2. *The activation slice conflicted with ADR-002's "no macOS/Linux work until v1."*
   Ruled: amend ADR-002 for distribution only — cross-compiled binaries may ship
   pre-v1, labeled built-not-verified; the no-porting/no-verification-effort rule
   stands. Rationale: near-zero marginal work, and a Windows-only launch would
   handicap the adoption bar in a macOS-heavy audience. ADR-002 carries the matching
   amendment note.
3. *"Evidenced external users" measured engagement, not usage.* Accepted outright:
   bar tightened to run-evidenced (decision 2 as amended); 10 users / 30 days
   unchanged.

## Amendment — 2026-08-03: macOS label raised to smoke-verified

The "built, not verified" label was written when nothing had ever been run on macOS.
That is no longer true, so the label is raised to match the evidence — and no further.

**What was run** (2026-08-03, macOS 26.5.2 build 25F84 / Darwin 25.5.0, **Intel x86_64**
(Core i7-9750H), Go 1.26.5, at `052a9d6`):

- `go vet ./...` clean; `go test ./...` green across all 13 packages, including all
  five vendor adapters against their committed fixtures
- `go build` succeeds; both CI statusline smokes pass through the real binary
  (honest-gauge assertions included: no quota without `rate_limits`, no cost for a
  vendor with no cost field)
- the Claude Code adapter read a **live macOS corpus**: 53 sessions discovered, all 53
  read, zero errors, zero degraded fields, zero diagnostics. POSIX workspace-path
  decoding was exercised for the first time, including worktree dirs
  (`-Users-…-dotfiles--claude-worktrees-…` → `/Users/…/dotfiles/.claude/worktrees/…`),
  a shape no committed fixture covers — every fixture project dir is Windows-shaped
- the HUD renders in a real terminal, and the absent-vendor path is confirmed on a
  machine where four of five vendors genuinely are absent: Codex, Cursor, Gemini and
  Antigravity hid themselves via `ErrVendorAbsent` with no rows and no error banner

**What was NOT verified, and why the label says smoke and not verified:**

- the Codex, Cursor, Gemini and Antigravity adapters have never met a live macOS
  corpus — no vendor but Claude Code is installed on that machine. Their macOS
  evidence is fixture-only, and fixtures cannot falsify a path assumption
- this was one manual run at one commit. **CI still runs `windows-latest` only**, so
  the macOS claim is point-in-time and decays with every subsequent commit. It is dated
  and SHA-bearing above for exactly that reason
- Linux remains untouched, hence the split label — Linux keeps "built, not verified"
- **Apple Silicon is unverified.** The run above was Intel x86_64. Nothing here was
  executed on arm64, which is the majority of the macOS audience the label exists to
  serve, and `os.UserConfigDir`/path behaviour is the same on both — but "smoke-verified
  on macOS" should be read as smoke-verified on Intel macOS until an arm64 run says
  otherwise. *(This bullet and the environment line above were corrected 2026-08-05: the
  amendment as first merged recorded Apple Silicon and macOS 15, both wrong, from an
  assumption rather than a `uname`/`sw_vers` read.)*

The no-porting/no-verification-effort rule of ADR-002 is **unchanged**. This amendment
records evidence that already existed; it does not authorize macOS work, and it does
not make macOS a target. Whether to add a `macos-latest` CI lane — which is what would
stop the claim from decaying — is deliberately left open here rather than assumed.

## Downstream surfaces

- `docs/design.md` §8 roadmap: activation slice, needs-input feature, agy re-survey —
  updated after this ADR merges, not in this PR.
- README positioning line — lands with the activation slice, not before.
- Launch post — carries the pre-registered bar and the honest-gauge differentiation.
- `decisions/README.md` index — this PR.
- Operating memory + cross-tool strategy doc — telltale remains the capacity-friction
  observation instrument; unaffected.
