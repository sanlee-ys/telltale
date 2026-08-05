# telltale — STATE

Shared fleet state for the council. Every seat reads this before answering
about current work.

**Staleness rule:** any PR that changes what's in flight updates this file in
the same commit. A ledger nobody updates is worse than none.

Last updated: 2026-08-05 · Author: Cursor · Rev: ADRs archived; no-ledger closed

---

## Current objective

Make `telltale council` the daily operating room — one terminal, four seats —
so San is not juggling vendor windows. Glass must earn that.

## In flight

| Work | Owners | Status | Branch / PR |
|---|---|---|---|
| Council TUI upgrade (density, focus hierarchy, `isDark` chrome, footer) | Cursor + Antigravity | Partly overtaken by #49/#52/#54/#59 — re-cut against the current room before painting | — |
| Fable council session (screenshots handed for TUI) | Fable | In progress (outside room) | unknown |

## Awaiting San's ruling

- **v1 cut.** Gauges-only (statusline + HUD, declared vendor version pins) vs hold v1 until council settles. COO recommends gauges-only.
- **Front-end scope.** Browser HUD vs docs/marketing vs TUI-only. Today's guarantees: one binary, no network calls, no credential reads — a served UI retires some of them.

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

- ADRs moved to [career/decisions/telltale](https://github.com/sanlee-ys/career/tree/main/decisions/telltale) — telltale adds no more ADRs
- Ordinary turns stay in-room; yank + `/flow` are the export seams (no ambient ledger)
- #62 — Cursor's mid-turn whole-message repeat no longer renders twice
- #61 — `/flow` seat-to-seat handoffs, opt-in redacted artifacts, explicit `write:<path>` authority
- #60 — `y` copies this seat's reply, `Y` copies the whole turn
- #59 — the waiting card says what to expect, not how it works
- #58 — stopped asking agy for a restriction that only ever killed turns
- #57 — a hiccup no longer costs the whole conversation
- #55 — `STATE.md` committee pickup doc
- #54 — council posture badges explain themselves

## Known gaps, not yet owned

- **Adapter schema drift.** Pins are private on-disk formats (Cursor 3.14.7, agy 1.1.9, gemini-cli v0.53.1); no detector marks a vendor unverified when corpus stops matching.
- **No repo-local contract.** No `CLAUDE.md`, no `PARITY.md`. Windows-shaped paths; Mac verification not queued.
- **No negative routing** in council (`@all -claude`). Feature request.

## Next

1. Antigravity: visual punch list → append under Council TUI detail.
2. Cursor: re-cut the v1 TUI list against the post-#61/#62 room (four of the five items moved under it), then branch; PR; merge on green; refresh this file in the same commit.

## How to re-enter

1. Read this file.
2. Check `telltale hud` and `gh pr list`.
3. Update **In flight** / **Next** / **Last updated** when you land or hand off.
