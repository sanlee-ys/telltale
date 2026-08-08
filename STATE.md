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

Nothing claimed.

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
  The launch-only excuse is now gone: `/trace <file>` records from inside the
  room and writes the turns already held, so catching a slow turn no longer
  needs one predicted before the room opened. **That removes the obstacle, not
  the question** — the measurement still has not been taken, and whether the
  flag's shape was ever the real reason is untested either way.

## Known gaps, not yet owned

- **The turn clock's concurrency is argued, not race-verified.** `-race` needs
  cgo, is unavailable on the machine the clock was written on, and is not in the
  CI gate either. The locking in `runner/clock.go` was reasoned through and
  reviewed; it has not been run under the detector.
- **CI actions are pinned to a deprecated runtime.** `actions/checkout@v4` and
  `actions/setup-go@v5` target Node 20, which now force-runs on Node 24.
  Cosmetic today, a broken gate eventually.

Cross-platform and cross-machine status has its own file: [PARITY.md](PARITY.md).

The conventions a fresh session would otherwise re-derive — golden-test traps,
commit voice, the honesty rules, the read/write boundary — are in
[CLAUDE.md](CLAUDE.md).

## How to re-enter

1. Read this file, then `PARITY.md` if you changed machines.
2. `gh pr list` for open work; `telltale hud` for live sessions.
3. Update *In flight* and *Open questions* in the same PR that changes them.
