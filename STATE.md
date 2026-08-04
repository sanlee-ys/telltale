# telltale — STATE

Shared fleet state for the council. Every seat reads this before answering
about current work.

**Staleness rule:** any PR that changes what's in flight updates this file in
the same commit. A ledger nobody updates is worse than none.

Last updated: 2026-08-04 · Author: Cursor (Frontend) · Rev: post-#55 refresh

---

## Current objective

Make `telltale council` the daily operating room — one terminal, four seats —
so San is not juggling vendor windows. Glass must earn that.

## In flight

| Work | Owners | Status | Branch / PR |
|---|---|---|---|
| Council TUI upgrade (density, focus hierarchy, `isDark` chrome, footer) | Cursor + Antigravity | Planned; paint not started | — |
| Fable council session (screenshots handed for TUI) | Fable | In progress (outside room) | unknown |

## Awaiting San's ruling

- **v1 cut.** Gauges-only (statusline + HUD, declared vendor version pins) vs hold v1 until council settles. COO recommends gauges-only.
- **Front-end scope / ADR-009.** Browser HUD vs docs/marketing vs TUI-only. Today's guarantees: one binary, no network calls, no credential reads — a served UI retires some of them.
- **Council decision records.** Emit a per-turn decision file vs build no ledger. COO recommends emit-only. Related: **yank key** (`y` / `Y` clipboard, or write `~/.telltale/council/last-turn.md`) — deliberate→execute seam; likely same feature family as decision records, not a second track. Proposed for Fable / later paint; not started.

## Owners

| Seat | Role |
|---|---|
| **Cursor** | Frontend & UI — owns this file; builds the council TUI upgrade |
| **Antigravity** | Visual / CPO — punch list and design critique on the glass |
| **Claude Code** | COO & integrator of record for everything. Quota is scheduling, not a role change — out of the paint this week on capacity |
| **Codex** | CRO / CISO — design challenge and independent pre-merge audit when San calls it |

## Council TUI upgrade (detail)

**Cut (v1):**
1. Four-column density — `internal/council/layout.go`
2. Focus / read hierarchy — `internal/council/style.go`, view
3. Adaptive chrome — wire unused `isDark` like the HUD
4. Composer / footer clutter — body wins when idle
5. Antigravity punch-list deltas that fit those seams

**Bar:** One Windows Terminal window, four seats, readable and steerable without opening the four vendor apps.

**Out of scope:** Adapter / runner / sandbox rewrites; per-vendor cancel; mouse wheel; full HUD redesign.

**Key paths:** `internal/council/{view,layout,style,glyphs}.go` · goldens under `internal/council/testdata/` · honesty rules in `docs/design.md` §7 / §9

## Landed recently

- #55 — `STATE.md` committee pickup doc
- #54 — council posture badges explain themselves
- #52 — council names which column the scroll keys move
- #51 — a turn's last word survives the end of the stream
- #50 — council frame and graphics refreshed

## Known gaps, not yet owned

- **Adapter schema drift.** Pins are private on-disk formats (Cursor 3.14.7, agy 1.1.9, gemini-cli v0.53.1); no detector marks a vendor unverified when corpus stops matching.
- **No repo-local contract.** No `CLAUDE.md`, no `PARITY.md`. Windows-shaped paths; Mac verification not queued.
- **No negative routing** in council (`@all -claude`). Feature request.

## Next

1. Antigravity: visual punch list → append under Council TUI detail.
2. Cursor: implement v1 cut on a branch; PR; merge on green; refresh this file in the same commit.
3. Codex: review this STATE contract after this refresh lands (standing offer).

## How to re-enter

1. Read this file.
2. Check `telltale hud` and `gh pr list`.
3. Update **In flight** / **Next** / **Last updated** when you land or hand off.
