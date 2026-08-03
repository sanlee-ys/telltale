# 003 — Gemini CLI becomes the third built-in adapter

Status: Accepted (2026-08-02)

## Context

ADR-001 scoped v1 to two built-in adapters (Claude Code, Codex CLI) with Gemini CLI as
the *worked documentation example* (design.md §4a.7), not built. San's direction on
2026-08-02: **"add in gemini … i want telltale to capture gemini today"** — in the same
breath as renaming his operating-layer repo claude-ops → agent-ops, i.e. the
multi-vendor framing is hardening across the whole system, and telltale is its
observation instrument. The market picture backs the pick: among terminal-native agent
CLIs that leave inspectable session artifacts on disk, Claude Code / Codex CLI /
Gemini CLI are the three vendor-native seats, and Gemini CLI was already this repo's
own "obvious third" (it is why §4a.7 used it as the example).

## Decision

- `internal/adapter/gemini` ships built-in, wired into the HUD's adapter list, the
  vendor filter cycle, and the header counts. Package docs carry the CapNone
  inventory; design.md §3.7 carries the seam verification.
- **The honest-gauge bar is unchanged.** The adapter is source-verified (gemini-cli
  v0.53.1, the writer's own persistence code read at tag) with synthesized fixtures,
  following the Codex precedent: surface lands source-verified, and the **first
  live-corpus pass is itemized in §3.7 and stays owed** until a real corpus exists on
  the dev machine. The launch post does not claim the Gemini adapter as verified until
  §3.7's live items are discharged.
- Capability outcome worth recording: the §4a.7 sketch guessed `context_pct: derived`;
  the source read falsified it (no window size on disk; the CLI's own percentage uses
  a static in-binary table — the assumed denominator decisions/001 forbids). Shipped:
  `context_pct: CapNone`, token reading as a display-only extra, `subagents:
  CapDerived` via the structural `chats/<parent-id>/` nest that Codex lacks.

## Consequences

- The §4a.7 worked example's subject is now a built-in. The sketch is kept as written,
  wrong guess included, with a postscript naming what Step 0 changed — the example is
  stronger as evidence than it was as hypothesis.
- "Cross-vendor" in public wording now means three vendors on the disk seam;
  the statusline remains Claude-only (the only vendor with a statusline hook).
- The empty-state vendor table, README hero frame, and capability goldens all show
  three vendors; doc-sync tests pin them.

## Addendum (2026-08-02, same day, hours later)

Post-merge fact check overturned part of the Context: **Gemini CLI stopped serving
free/Pro/Ultra consumer requests on 2026-06-18** — Google folded the consumer terminal
product into **Antigravity CLI (`agy`)**, closed-source, docs-only GitHub presence
(google-antigravity/antigravity-cli). Gemini CLI continues only for Gemini Code Assist
Standard/Enterprise licenses and paid API keys. So the "third vendor-native CLI seat"
this ADR cited is, for consumers, agy — and this adapter covers Gemini CLI as the
enterprise/API-key flavour of that seat.

Consequences of the correction:

- The §3.7 live pass now requires a **paid Gemini API key** (there is no consumer login
  to capture). It stays owed; the verified-claim gate is unchanged.
- **San's ruling (2026-08-02): telltale's consumer-facing Gemini-family capture target
  is Antigravity CLI.** agy is closed-source, so its verification follows the Claude
  Code precedent (documented contracts + live-corpus survey, no source read). Its
  documented seams are promising and materially different: transcripts at
  `~/.gemini/antigravity/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`,
  and a **statusline hook** whose stdin payload is a superset of Claude's (including
  `context_window.context_window_size`, a `quota` object, and `agent_state` — the first
  vendor-reported liveness signal in this product's universe). That work is a future
  ADR once the live corpus exists; nothing about it is promised here.

## Live-pass note (2026-08-03)

§3.7's first live pass ran against a real gemini-cli 0.53.1 session and **passed**
(design.md §3.7 carries the results: upserts and `$set` checkpoints observed live,
registry mapping confirmed, one fixture assumption corrected — main sessions carry
`kind:"main"`). **The verification hold above is released: the launch post may claim
this adapter live-verified.** The addendum's "requires a paid API key" expectation was
overtaken by an observed session on this consumer machine; the auth flavour behind it
was not investigated.

## Downstream surfaces

design.md (§3.3 matrix, §3.7 new, §4 intro, §4a.7 postscript, §5 eval table, §8
roadmap), README.md (status, hero + caption, flags, adapter list), decisions/README.md
index, HUD goldens ×5, memory: telltale-project.
