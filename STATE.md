# telltale — STATE

Shared fleet state for the council. Every seat reads this before answering
about current work.

**Staleness rule:** any PR that changes what's in flight updates this file in
the same commit. A ledger nobody updates is worse than none.

Last updated: 2026-08-05 · Author: Claude Code (COO) · Rev: post-#62 refresh

---

## Current objective

Make `telltale council` the daily operating room — one terminal, four seats —
so San is not juggling vendor windows. Glass must earn that.

## In flight

| Work | Owners | Status | Branch / PR |
|---|---|---|---|
| Council flow execution engine + artifact transport seam | — | Open PR, not merged | [#61](https://github.com/sanlee-ys/telltale/pull/61) `feat/council-flow-seam` |
| Council TUI upgrade (density, focus hierarchy, `isDark` chrome, footer) | Cursor + Antigravity | Partly overtaken by #49/#52/#54/#59 — re-cut against the current room before painting | — |
| Fable council session (screenshots handed for TUI) | Fable | In progress (outside room) | unknown |

## Awaiting San's ruling

- **v1 cut.** Gauges-only (statusline + HUD, declared vendor version pins) vs hold v1 until council settles. COO recommends gauges-only.
- **Front-end scope / ADR-009.** Browser HUD vs docs/marketing vs TUI-only. Today's guarantees: one binary, no network calls, no credential reads — a served UI retires some of them.
- **Council decision records.** Emit a per-turn decision file vs build no ledger. COO recommends emit-only. The **yank key** half of this shipped in #60 (`y` copies this seat's reply, `Y` the whole turn) — clipboard only, no file on disk. What is still unruled is whether a turn also gets written somewhere durable (`~/.telltale/council/last-turn.md` or a per-turn decision file); #61's artifact transport seam is the natural place for it if the answer is yes.

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

- #62 — Cursor's mid-turn whole-message repeat no longer renders twice
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

1. **#61 is the one open PR** — land it or say why not; it blocks the durable-turn half of the decision-records question above.
2. Antigravity: visual punch list → append under Council TUI detail.
3. Cursor: re-cut the v1 TUI list against the post-#62 room (four of the five items moved under it), then branch; PR; merge on green; refresh this file in the same commit.
4. Codex: review this STATE contract after this refresh lands (standing offer).

## How to re-enter

1. Read this file.
2. Check `telltale hud` and `gh pr list`.
3. Update **In flight** / **Next** / **Last updated** when you land or hand off.
