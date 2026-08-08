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

- **The 44 seconds is half-attributed: measured for `claude`, still open for
  `cursor`.** First trace taken 2026-08-08, five turns from a live room, written
  out of the ring by `/trace` rather than predicted in advance:

  ```
  claude spawn=40ms   wait=6.295s  stream=29.917s   total=36.252s
  claude spawn=172ms  wait=6.121s  stream=9m8.975s  total=9m15.268s
  claude spawn=-      wait=77ms    stream=1m49.261s total=1m49.338s
  claude spawn=-      wait=97ms    stream=28.855s   total=28.951s
  claude spawn=-      wait=37ms    stream=4.974s    total=5.011s
  ```

  **What is now measured, for this seat:** spawn is noise (40–172 ms). A COLD
  process costs ~6.1–6.3 s of `wait`; a warm one costs 37–97 ms, a ~65× gap. The
  persistent seat is doing exactly what it was built to do — the last three turns
  spawned nothing at all (`spawn=-`) and reused the process. Everything else is
  `stream`, which is the model working, not telltale waiting. **There is no
  overhead left to optimise on this seat.**

  **What is NOT measured, and it is the half the entry was originally about:**
  every row here is `claude`. Since #99 made silence route to the control plane,
  an unaddressed turn never reaches the other seats, so a session's worth of
  ordinary turns produces a trace with no `cursor` or `agy` rows in it at all.
  The standing diagnosis — `cursor-agent` spawned fresh per turn, `--resume`
  restoring context rather than process warmth — is **still untested**. Closing
  it needs one traced `@all` turn, which is now a keystroke rather than a
  relaunch.

  Method note, because it cost a round trip: `/trace` resolves a relative path
  against the ROOM's workspace, so the file has to be named relative to the repo
  for anything confined to it to read the result.

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
