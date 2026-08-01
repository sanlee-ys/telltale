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

- **Statusline** (Claude Code only in v1): a single line rendered from the JSON Claude
  Code passes on stdin. Zero I/O beyond stdin; budget-conscious output (every character
  renders on every prompt).
- **HUD** (cross-vendor): a minimal watch-mode TUI listing live sessions across vendors
  with per-session gauges. Reads vendor-native data via adapters; refresh cadence and
  degradation behavior specified below before build.

## 2. Statusline segments (v1)

| Segment | Source (exact field) | Empty/degraded state | Status |
|---|---|---|---|
| Model | stdin JSON: model id | — (always present) | planned |
| Context % | stdin JSON: context used_percentage (input-token based — display labeled accordingly) | hide segment | planned |
| Session cost | stdin JSON: `total_cost_usd` | hide segment | planned |
| Quota pacing (5h) | stdin JSON: `rate_limits.five_hour.used_percentage` + `resets_at` | API-key logins have no rate_limits → segment hidden, never zeroed | planned |
| Quota pacing (7d) | stdin JSON: `rate_limits.seven_day.*` | same rule | planned |

Rules: no segment computes a number the source didn't provide; derived displays (e.g.
time-to-reset countdown from `resets_at`) are arithmetic on a sourced value and are
labeled by their source field here. Field names above are from the live docs as of
2026-08-01 and get re-verified against stdin fixtures at build time.

## 3. HUD (v1, minimal)

- One row per live session, both vendors; per-row: vendor, session identity, model,
  context/quota gauges where the vendor provides them, last-activity age.
- **Claude Code adapter sources:** TBD at build time (candidates: statusline JSON relay,
  transcript JSONL). To be filled in with exact paths/fields before build.
- **Codex adapter sources:** TBD at build time (candidates: `~/.codex/sessions` JSONL,
  hooks/notify events). Codex has no statusline hook (ADR-001); the HUD is its surface.
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

1. Language/stack for statusline + TUI (constraint: Windows first-class, minimal deps,
   near-zero install friction).
2. Normalized session schema — the one contract everything hangs on.
3. HUD refresh model (poll cadence vs file-watch; Windows file-watching behavior).
4. Exact Claude/Codex on-disk data sources (verify live, per ADR-001's contract).
5. npm package name (`telltale-hud` vs scoped) — at packaging time.
