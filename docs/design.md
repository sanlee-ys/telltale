# telltale — design doc

Status: skeleton (v1 design in progress). The honest-gauge rule requires every segment's
data source to be named here before that segment ships; the tables below are the
authority the eval harness tests against.

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

## 2. Statusline segments (v1)

| Segment | Source (exact field) | Empty/degraded state | Status |
|---|---|---|---|
| Model | stdin `model.display_name` (falls back to `model.id`) | hide if both empty | **built** |
| Context % | stdin `context_window.used_percentage` (input-token based per docs) | hide segment | **built** |
| Session cost | stdin `cost.total_cost_usd` | hide segment | **built** |
| Quota pacing (5h) | stdin `rate_limits.five_hour.used_percentage` + `resets_at` (unix s) | rate_limits absent on API-key logins; each window independently absent → hide, never zero; countdown hides without `resets_at` | **built** |
| Quota pacing (7d) | stdin `rate_limits.seven_day.*` | same rule | **built** |
| Worktree | stdin `worktree.name` (present only in `--worktree` sessions) | hide segment | **built** |

Threshold colors (applies to any percentage segment): green < 60, yellow ≥ 60, red ≥ 85.
`NO_COLOR` env strips styling. Derived displays (reset countdown `↻2h13m`) are arithmetic
on `resets_at` only.

Schema verification record: full stdin JSON schema captured from
code.claude.com/docs/en/statusline on 2026-08-01, including per-field absence semantics
(`rate_limits` Pro/Max-only and only after first API response; each window independently
absent). Statusline updates are debounced at 300ms and in-flight scripts are cancelled —
which is the empirical backing for the fast-exit budget. Parsing ignores unknown fields
by design (vendor adds fields between versions).

## 3. HUD (v1, minimal)

- One row per live session, both vendors; per-row: vendor, session identity, model,
  context/quota gauges where the vendor provides them, last-activity age.
- **Claude Code adapter sources:** TBD at build time (candidates: statusline JSON relay,
  transcript JSONL). To be filled in with exact paths/fields before build.
- **Codex adapter sources:** TBD at build time (candidates: `~/.codex/sessions` JSONL,
  hooks/notify events). Codex has no statusline hook (ADR-001); the HUD is its surface.
  **Machine note (2026-08-01): Codex CLI is not installed on the dev PC** (`~/.codex`
  absent) — the adapter starts fixture-driven; ADR-001's live-session verification
  happens on the Pi (`goguma`, where Codex-family tooling runs) or after a local
  install, BEFORE the adapter is called done.
- Degradation rule: a vendor field the adapter can't read renders as `—` (absent), never
  as a zero or a stale value presented as fresh.

## 4. Adapter contract (v1)

One module per vendor implementing:

- `discover()` — find live/recent sessions from vendor-native data on disk
- `read(session)` — return the normalized session model (schema TBD, documented here)
- `capabilities()` — which normalized fields this vendor can actually source

The contract, the normalized schema, and a worked third-party example (how you'd add
Gemini CLI) are documentation deliverables of v1, not afterthoughts.

## 5. Eval harness

- Fixture-driven: recorded real inputs (statusline stdin JSON, Codex session files) per
  vendor, per state (healthy, empty, degraded, API-key-login without rate_limits).
- Asserts the exact render for every segment/row against each fixture.
- CI-gating: a failing render assertion fails the build.
- No number appears in README/launch material unless this harness generated it.

## 6. Open design questions (to resolve before build, each gets an ADR if consequential)

1. ~~Language/stack~~ — **ANSWERED, ADR-002:** Go + Bubble Tea/Lipgloss, one binary,
   two modes. Windows-first hardened; macOS/Linux deferred post-v1.
2. Normalized session schema — the one contract everything hangs on.
3. HUD refresh model (poll cadence vs file-watch; Windows file-watching behavior).
4. Exact Claude/Codex on-disk data sources (verify live, per ADR-001's contract).
5. Distribution naming (`telltale-hud` on any registry; winget/scoop manifests) — at
   packaging time. Go binary means npm is optional, not required.
6. HUD UI design section (layout, color/threshold system, motion, degraded states) —
   written in this doc before HUD build starts (ADR-002 consequence).
