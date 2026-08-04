# telltale — STATE

Pickup doc for the operating committee. Read this cold before acting.
Last updated: 2026-08-04 · Author: Cursor (Frontend)

## Direction

Council is the operating room so San is not juggling Claude / Codex / Cursor / Antigravity as four separate windows. The glass has to earn that: readable four-seat room, honest gauges, no fake completeness.

## Owners

| Seat | Role on current work |
|---|---|
| **Cursor** | Frontend & UI — builds the TUI upgrade; owns this file |
| **Antigravity** | Visual / CPO — punch list and design critique on the glass |
| **Claude Code** | Control plane — out of the paint this week (quota); integrate only if seams cross adapters |
| **Codex** | Challenge / pre-merge audit — on call when San asks; not the painter |

## Active work: Council TUI upgrade

**Status:** Pickup doc landing. Paint not started.

**Cut (v1):**
1. Four-column density — retune `internal/council/layout.go` breakpoints / chrome shedding
2. Focus / read hierarchy — `Strong` / muted unfocused columns, honest mode line (`internal/council/style.go`, view)
3. Adaptive chrome — wire unused `isDark` like the HUD
4. Composer / footer clutter — body wins when idle
5. Antigravity punch-list deltas that fit those seams

**Bar:** One Windows Terminal window, four seats, readable and steerable without opening the four vendor apps.

**Out of scope:** Adapter / runner / sandbox rewrites; per-vendor cancel; mouse wheel; full HUD redesign; Claude as painter.

**Key paths:** `internal/council/{view,layout,style,glyphs}.go` · goldens under `internal/council/testdata/` · honesty rules in `docs/design.md` §7 / §9

## How to re-enter

1. Read this file.
2. Check `telltale hud` for live sessions and `gh pr list` for open work.
3. Update **Status** and **Next** here when you land or hand off.

## Next

1. Merge this `STATE.md` to `main`.
2. Antigravity: visual punch list → append under Active work.
3. Cursor: implement v1 cut on a branch; PR; merge on green; refresh Status.
