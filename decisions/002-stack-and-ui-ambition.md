# 002 — Windows-first hardened, HUD UI as a first-class surface, Go + Charm stack

Status: Accepted (2026-08-01)

## Context

Two amendments from San, same session as ADR-001:

1. **"Windows first. We can worry macOS later."** ADR-001 made Windows the primary
   *verified* target; this hardens it into build priority — macOS/Linux support is a
   later milestone, not a v1 acceptance criterion. (The Charm stack below cross-compiles
   trivially, so this costs little to honor later.)
2. **"Max effort on the UI/front-end of this cross-vendor HUD."** ADR-001 called the HUD
   a *minimal* watch-mode companion. That word is struck: the HUD's interface is a
   first-class design investment — layout, color system, motion, degraded-state
   presentation all designed deliberately, not defaulted. The research record backs
   this: in the statusline/HUD lane, packaging and polish are what earn adoption, not
   novel capability.

The medium question (terminal vs local-web) was put to San explicitly with the
tradeoffs; he chose terminal-native.

## Decision

- **Stack: Go + Bubble Tea + Lipgloss** (the Charm stack) for the HUD.
  - Highest polish ceiling among terminal UI stacks (styling, layout, animation are
    first-class primitives, not escape-code craft).
  - Single static `.exe` — near-zero install friction on Windows, no runtime
    dependency, winget/scoop-able later. Cross-compiles to macOS/Linux when that
    milestone arrives.
  - Go 1.26.5 is already installed on the dev machine; no new toolchain (the
    alternative, Rust + MSVC, is the exact toolchain gap that left quota-widget's Tauri
    backend permanently uncompiled).
- **One binary, two modes:** `telltale.exe` ships both surfaces —
  `telltale statusline` (reads Claude Code's JSON on stdin, prints one line, exits;
  startup must stay in single-digit milliseconds) and `telltale hud` (the Bubble Tea
  watch-mode app). Shared adapter + normalized-model code, one artifact to install,
  one version to reason about.
- **Rejected:** Rust/Ratatui (toolchain cost + language cliff, no UI-ceiling win over
  Charm for this scope); Node/Ink (runtime dependency, weaker polish ceiling — the
  React familiarity was not worth the install friction); local web dashboard (highest
  pixel ceiling, but San explicitly re-chose terminal-native after the tradeoff was
  laid out — the HUD lives where the agents live).

## Consequences

- Windows Terminal is the reference rendering environment; anything that renders worse
  there than on other terminals is a bug, not a footnote.
- "Max effort UI" gets teeth via the eval + design doc: the design doc gains a UI
  section (layout grid, color/threshold system, motion rules, empty/degraded-state
  designs) BEFORE the HUD is built, and degraded-state renders are eval fixtures like
  everything else. Polish is specified, then verified — not vibes.
- The statusline mode's latency budget, restated after measurement (2026-08-01):
  telltale's own parse+render is sub-millisecond (benchmarked: ~0.9ms/op); end-to-end
  invocation is bound by the Windows process-spawn floor (~15–30ms), well inside Claude
  Code's 300ms debounce. Bubble Tea is never initialized on the statusline path. The
  original "single-digit ms" phrasing described the end-to-end number, which no external
  process can hit on Windows — the enforceable constraint is the benchmark plus
  spawn-floor awareness.
- macOS/Linux: no work until v1 ships; the only discipline is not writing
  Windows-only code where a portable call exists cheaply.

## Downstream surfaces

- `docs/design.md` — §1 shape (one binary, two modes), §6 Q1 answered, "minimal" struck.
- `README.md` — HUD description updated.
- `decisions/README.md` — this row.
