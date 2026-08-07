# telltale — STATE

Where the project is right now. Read this before picking up work.

This file holds only what git and GitHub cannot tell you: intent, open
questions, and known gaps. For what landed, run `gh pr list --state merged`.
For when this file last changed, run `git log -1 STATE.md`. Neither is copied
here, because a hand-maintained copy of a derived fact is stale by the next
merge — this file has gone stale that way three times.

**Keep it true:** update *In flight* and *Open questions* when you land or hand
off work. `unknown` is a legal value; an honest blank beats a plausible guess.

## Current objective

Pre-v1. The active push is making `telltale council` usable as a daily driver:
one terminal, four seats, readable and steerable without opening four vendor
apps.

**The direction is recorded rather than remembered** (2026-08-06): council is
the product, the gauges are the infrastructure under it, and **v1 is held until
council settles**. Cutting v1 as gauges only was the standing alternative and it
is rejected. `README.md` and [docs/design.md §1](docs/design.md) hold the
binding copies and the argument; this file does not restate either.

## In flight

The mid-session control invariant is on this branch (ships with this PR): a rule
plus an inventory, no behaviour change. [docs/design.md §9.17](docs/design.md)
is the binding copy — state that changes while the room is open is reachable
from inside it, flags are for what is true at launch. Six controls were swept;
five violate it. Surface for the first one is **ruled** (a key on the focused
seat, not a slash command); the keybinding itself is not built.

## Closed without code (do not re-open)

- **Tall-window content anchor** — **bottom**, ruled 2026-08-05. Short content
  sits above the composer and grows upward. Top-anchor is rejected; do not
  re-litigate. Contract: viewport geometry only; scroll-up freezes Follow; `G`
  restores; no jump on Done; long content keeps full-viewport scroll.
- **Focus hierarchy** — already shipped (`▸` + `Strong`, `hierarchy_test.go`).
- **`isDark` adaptive chrome** — won't-do. Council uses ANSI palette indices from
  `internal/theme`; the terminal resolves them against its own theme. The only
  background-dependent tokens in the repo are the HUD's gauge track
  (`ColorTrackLite` / `ColorTrackDark`), and council has no gauge. Wiring
  `lipgloss.LightDark` here would invent hues and break "Council adds no hues
  of its own" in `style.go`. The `_ = isDark` placeholder stays documented.
- **Negative routing** (`@all` minus a seat) — **shipped 2026-08-04**, and this
  file went on listing it as an unowned gap for two days afterwards. `-@vendor`,
  the expansion of `-@all` into a list of exclusions, and the refusal to mix the
  positive and negative forms are all in `mentions.go`, with
  `TestEveryAddressableVendorIsExcludable` pinning the vocabulary so a fifth
  seat cannot be added while `-@all` quietly goes on meaning four.

## Open questions

- **The 44 seconds is still unattributed.** `telltale council --trace <file>`
  now splits every turn into spawn / wait / stream per seat, so the question is
  answerable — but nobody has run it against a slow turn. The instrument exists;
  the measurement does not, and the two are not the same claim. Diagnosis so far
  is unchanged: `cursor-agent` is spawned fresh per turn and `--resume` restores
  context, not process warmth. **Read a trace before optimising anything.**
  §9.17 offers a candidate explanation for why the measurement never happened:
  `--trace` is launch-only, so catching a slow turn means having predicted one
  before the room opened. Unconfirmed — it is a reading of the control surface,
  not of anyone's actual attempt.

## Known gaps, not yet owned

- **Five controls violate the §9.17 mid-session rule**, each its own change:
  `--fresh` (the one that raised it — clearing one seat's thread costs the other
  three), `--trace`, `--read`, `--auto`, `--vendor`. The inventory and the
  per-control reasoning are in [docs/design.md §9.17](docs/design.md); it is not
  restated here. `--brief` was examined and deliberately **not** filed — it is
  first-turn context by definition, so re-briefing is a separate question.

- **`docs/design.md` §3 does not name the canary set.** §7 now records how drift
  *renders*; §3 still does not say what each adapter actually watches. A canary
  is a survey finding and belongs beside the rest of that adapter's survey, or
  the next person to re-verify a vendor will not know what was being watched.
  `grep -n canary docs/design.md` returns nothing, which is the whole gap.
- **This file's own staleness is unmeasured**, and 2026-08-06 is the evidence
  rather than the theory: a single pickup found *two* entries wrong — a shipped
  feature still listed as an unowned gap, and an in-flight row naming work that
  had merged. Council could compare `git log -1 STATE.md` against `HEAD` and
  show the gap on open. It does not, so a stale pickup doc reads exactly like a
  current one — which is the same failure this project refuses to tolerate in a
  gauge, tolerated in the file that describes the project.
- **The turn clock's concurrency is argued, not race-verified.** `-race` needs
  cgo, is unavailable on the machine the clock was written on, and is not in the
  CI gate either. The locking in `runner/clock.go` was reasoned through and
  reviewed; it has not been run under the detector.
- **CI actions are pinned to a deprecated runtime.** `actions/checkout@v4` and
  `actions/setup-go@v5` target Node 20, which now force-runs on Node 24.
  Cosmetic today, a broken gate eventually.
- **The empty state can still draw past the terminal width.** `emptyLines` builds
  each vendor row and hands it to `centerBlock`, which pads and never truncates,
  so a long `v.Err` overflows — `empty-unreadable` at 60 columns renders 74. The
  footer's own overflow on this path was fixed when drift reached the grid; this
  one was found in the same review and deliberately left, because it is older
  than that change and fixing it there would have hidden a regression inside an
  unrelated repair. A frame that tears is the honest-gauge rule failing at the
  layer below the numbers.

Cross-platform and cross-machine status has its own file: [PARITY.md](PARITY.md).

The conventions a fresh session would otherwise re-derive — golden-test traps,
commit voice, the honesty rules, the read/write boundary — are in
[CLAUDE.md](CLAUDE.md).

## How to re-enter

1. Read this file, then `PARITY.md` if you changed machines.
2. `gh pr list` for open work; `telltale hud` for live sessions.
3. Update *In flight* and *Open questions* in the same PR that changes them.
