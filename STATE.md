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

## In flight

| Work | Owner | Status | Branch / PR |
|---|---|---|---|
| Council TUI upgrade — column density, focus hierarchy, adaptive chrome, footer | Cursor | Needs re-cutting: four of its five items were overtaken by #49/#52/#54/#59 | — |
| Council TUI implementation (parallel session, outside this room) | — | unknown | unknown |

Nothing else is open. No open PRs, no open issues at the time of writing.

## Open questions

- **v1 cut.** Ship v1 as gauges only — statusline and HUD, with declared vendor
  version pins — or hold v1 until council settles? The gauges are finished and
  unreleased; council's surface still moves weekly.
- **Product direction.** Council has been described as the primary product with
  the gauges as supporting infrastructure. That direction has been *stated in
  the room and never recorded*, so it currently binds nobody. Until it is
  written down, treat the framing as unsettled.
- **Turn latency has no provenance.** A 44-second reply to a greeting was
  observed and could not be attributed. Splitting a turn's clock into measured
  segments — spawn, wait, stream — would answer it without inferring anything.
  Diagnosis so far: `cursor-agent` is spawned fresh per turn and `--resume`
  restores context, not process warmth. Unowned; measure before optimising.

## Known gaps, not yet owned

- **Adapter schema drift.** Adapters are pinned to vendors' private on-disk
  formats (Cursor 3.14.7, Antigravity 1.1.9, gemini-cli v0.53.1). Nothing
  detects that a corpus no longer matches what the adapter was verified
  against, so drift would degrade silently — the one failure mode this project
  exists to forbid.
- **This file's own staleness is unmeasured.** Council could compare
  `git log -1 STATE.md` against `HEAD` and show the gap on open. It does not,
  so a stale pickup doc reads exactly like a current one.
- **No repo-local contributor contract.** No `CLAUDE.md`.
- **No negative routing in council** (`@all` minus a seat). Feature request.

Cross-platform and cross-machine status has its own file: [PARITY.md](PARITY.md).

## How to re-enter

1. Read this file, then `PARITY.md` if you changed machines.
2. `gh pr list` for open work; `telltale hud` for live sessions.
3. Update *In flight* and *Open questions* in the same PR that changes them.
