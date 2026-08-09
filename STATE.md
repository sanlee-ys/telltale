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

**The direction is recorded rather than remembered** (2026-08-06; v1 gate
re-cut 2026-08-08): council is the product, the gauges are the infrastructure
under it, and **v1 is a snapshot that cuts when three checkable gates hold —
not when development goes quiet**. Cutting v1 as gauges only was the standing
alternative and it is rejected. `README.md` and
[docs/design.md §1](docs/design.md) hold the binding copies, the gates, and
the argument; this file does not restate either.

## In flight

- **The arena family is landed and owes one live session.** The whole §9.37
  deferred list is now built — the live stat, the cursor racer, commit-per-turn
  with `u` undo, `.worktreeinclude` seeding, `/adopt` and `/arena drop` — plus
  §9.38's composer paste (what landed: `gh pr list --state merged`; the rulings:
  the dated 2026-08-09 amendments in [docs/design.md §9.37/§9.38](docs/design.md)).
  Every one of those amendments states a live-verification debt rather than
  implying it paid, and **one real session on the Windows box covers nearly all
  of them at once**: a `/arena` run with a brief that changes files (4-of-4 now —
  cursor races on a throwaway ACP session), watching the "so far" stat grow,
  the `committed <sha>` receipts land, then `/adopt` the winner, `u`-undo a
  loser, `/arena drop` the rest, and paste a three-line snippet (expect one
  insertion, zero sends). Nothing here has run against a live vendor —
  offline suites only, same debt shape the core carried before turn 4 paid it.

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
- **The write seat's `git -C` allowlist gap** — **measured closed, 2026-08-09**,
  by a four-arm probe on the reference box after two races tripped it. Claude
  Code's `--allowedTools` matcher is prefix-only and no rule spelling scopes a
  verb behind `-C` (the mid-wildcard `Bash(git -C * status:*)` does not match);
  in a trusted workspace the operator's own settings cover `-C` shapes, which
  is why it only ever bit in never-trusted arena worktrees. There is no
  rule-shaped fix; `Bash(git -C:*)` stays rejected (it would pre-approve every
  `-C` verb, destructive ones included), and the residue is friction the room
  already handles — the blocked call surfaces as an approval on the column.
  The full record lives on `autoAllowedTools` in
  `internal/council/vendors/claude.go`; do not re-open without a new
  measurement.
- **Negative routing** (`@all` minus a seat) — **shipped 2026-08-04**, and this
  file went on listing it as an unowned gap for two days afterwards. `-@vendor`,
  the expansion of `-@all` into a list of exclusions, and the refusal to mix the
  positive and negative forms are all in `mentions.go`, with
  `TestEveryAddressableVendorIsExcludable` pinning the vocabulary so a fifth
  seat cannot be added while `-@all` quietly goes on meaning four.

## Open questions

Nothing open. The last one here was the 44 seconds, and it was measured
2026-08-08; the finding and the unowned work left over from it are below.

## Known gaps, not yet owned

- **ATTRIBUTED, 2026-08-08. Spawning was never the cost; `wait` is, and only on
  the three seats that are not persistent.** One traced `@all` turn, all four
  seats, out of a live room's ring:

  ```
  claude spawn=-     wait=38ms     stream=6.443s   total=6.481s
  agy    spawn=18ms  wait=6.436s   stream=2.671s   total=9.125s
  cursor spawn=13ms  wait=10.864s  stream=14.137s  total=25.014s
  codex  spawn=30ms  wait=3.688s   stream=34.662s  total=38.38s
  ```

  **The standing diagnosis was right about the mechanism and wrong about the
  number.** "`cursor-agent` is spawned fresh per turn" is true — and its spawn is
  **13 ms, the cheapest of all four seats.** Process creation was never where the
  time went. What costs is `wait`: launch until the first line comes back.
  `cursor` pays **10.9 s** there, against a warm `claude`'s 38 ms — 286×. So the
  fix is not "make spawning cheaper"; there is nothing left in spawning to make
  cheaper.

  **~6 s looks like a CLI cold-start floor**, not a cursor problem alone: a cold
  `claude` measured 6.1–6.3 s (previous trace) and `agy` measures 6.4 s. `cursor`
  sits ~4.5 s ABOVE that floor and `codex` 2.4 s below it, so cursor is the one
  outlier worth a fix and codex is the fastest of the three to first byte.

  **What `wait` could not tell us has now been measured separately, and the
  split is in [design.md §9.33](docs/design.md).** `wait` bundled the vendor's
  own startup with the model's time-to-first-token; stamping raw stdout lines
  separates them, because `system/init` lands before the model is called.
  On `cursor-agent` **2026.08.04-aaa8809**, two trials per arm:

  - **~5.6 s** launch → `system/init` — pure CLI startup, no model involved. Of
    it, node is 0.08 s and loading the bundle ~1.13 s; the remaining ~4.4 s is
    the vendor resolving auth, config, trust and workspace.
  - **4.3–5.8 s** `init` → `result` — the model, confirmed by the vendor's own
    `duration_ms` on every trial. Persistence cannot touch this.
  - **~2.5 s** `result` → exit — the process lingers after answering.

  So **~8.1 s per turn is process cost and none of it is the model**, and it is
  paid again on every turn. `--resume` is NOT the expensive half: resumed
  startup (5.196 s, 5.551 s) is no larger than cold (5.666 s, 5.617 s), so
  restoring a conversation is free and the standing diagnosis was right.
  Proportion stated honestly rather than at its most flattering: 8.1 s is ~60%
  of a trivial turn but ~32% of the 25.0 s real turn traced above.

  **`cursor` was the outlier and is not one any more.** It has been re-founded on
  the hidden `cursor-agent acp` server ([design.md §9.36](docs/design.md)):
  one live process, a handshake paid once, and a warm turn measured at
  **1.12 s through the merged seat** against the ~13 s above. What remains on
  this bullet is `codex` and `agy`, which are batch programs with no seam anyone
  has found — their `wait` is a CLI cold-start floor nothing in this repo can
  move.

  **A fan-out turn costs the SLOWEST seat, not the sum**: 38.4 s here, set by
  codex's 34.7 s of `stream` — which is the model working and not something to
  optimise. `claude` finished in 6.5 s and the room waited ~32 s for the rest.

- **The ACP seat reports no token usage, and has no fallback for a turn that
  streams nothing.** Both are losses the switch paid for and neither has a fix in
  sight, so they are gaps rather than todos. Print mode's `result` line carried a
  usage block and the whole final reply; an ACP turn resolves with a stop reason
  and nothing else. The second one is the sharper: §9.6c leaned on that reply by
  name as the safety net for a column that streamed nothing, so a chunk parser
  that broke here would give an EMPTY column rather than a late one. (Cost was
  never available on this vendor and still is not — it publishes no monetary
  figure anywhere.)

- **The Cursor seat's `plan` posture rests on ONE trial, and its workspace-trust
  screen is gone.** Asked to create a file in plan mode the seat declined and
  nothing landed — better evidence than print mode ever produced — but one trial
  of a mode the model obeys is not a layer that stops it, which is why the badge
  still says `ro:requested`. Separately and worse: print mode refused to run in an
  untrusted directory and the ACP server wrote a file into the same one. There is
  no trust parameter in the protocol. Both facts are on the column's own badge
  detail; neither is fixable from here.

  Method note, because it cost a round trip: `/trace` resolves a relative path
  against the ROOM's workspace, so a trace has to be named relative to the repo
  for anything confined to it to read the result.

Cross-platform and cross-machine status has its own file: [PARITY.md](PARITY.md).

The conventions a fresh session would otherwise re-derive — golden-test traps,
commit voice, the honesty rules, the read/write boundary — are in
[CLAUDE.md](CLAUDE.md).

## How to re-enter

1. Read this file, then `PARITY.md` if you changed machines.
2. `gh pr list` for open work; `telltale hud` for live sessions.
3. Update *In flight* and *Open questions* in the same PR that changes them.
