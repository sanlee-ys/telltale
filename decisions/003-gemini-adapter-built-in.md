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

## Downstream surfaces

design.md (§3.3 matrix, §3.7 new, §4 intro, §4a.7 postscript, §5 eval table, §8
roadmap), README.md (status, hero + caption, flags, adapter list), decisions/README.md
index, HUD goldens ×5, memory: telltale-project.
