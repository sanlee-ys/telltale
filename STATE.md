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
one terminal, five seats, readable and steerable without opening five vendor
apps.

**The direction is recorded rather than remembered** (2026-08-06; v1 gate
re-cut 2026-08-08): council is the product, the gauges are the infrastructure
under it, and **v1 is a snapshot that cuts when three checkable gates hold —
not when development goes quiet**. Cutting v1 as gauges only was the standing
alternative and it is rejected. `README.md` and
[docs/design.md §1](docs/design.md) hold the binding copies, the gates, and
the argument; this file does not restate either.

## In flight

- **The Grok seat is landed, was broken on its first live turn, and is fixed.**
  The fifth seat (§9.39) is built, registered, and verified end to end against
  the real binary — `go test ./internal/council/vendors -tags=live -run
  TestLiveGrok` exercises the actual argv and asserts a real resume, and it
  passes here. **It passed before the fix too, and that is the part worth
  remembering**: the seat shipped with `-p` separated from its value, which
  clap refuses for a brief beginning with `---`, so every briefed turn died at
  exit 2 with an empty column while the live test stayed green over a prompt
  that began with a letter (§9.39's dated amendment). The prompt is now
  attached (`--single=…`) and every argv test uses a fenced, brief-shaped
  prompt. The rule that generalises: **probe a vendor with a prompt shaped
  like a brief, not like a greeting.** One thing is deliberately NOT done and should not be mistaken
  for an oversight. **`internal/adapter/grok` now exists** (§3.9a), so grok
  sessions render as HUD rows with name, model, workspace, a vendor-reported
  context percentage and last activity — the gauges observe this vendor as well
  as drive it. The COST column still stays dropped on those rows: grok writes a
  per-turn dollar figure and no session total, so the money reaches the detail
  pane as a labeled extra and never the grid. **The seat has no fleet guards wired** — grok's own config carries
  `[compat.claude] hooks = false` and its native hook system
  (`grok hooks-add`/`hooks-trust`) has nothing installed into it — which under
  agent-ops ADR-012 is an open obligation on the fleet rather than a reason to
  avoid the seat. That work belongs in agent-ops, not here.

  It also arrived AFTER race t9 below, which is why that entry still says four
  seats: t9 raced the room as it was that morning, and a record of what ran is
  not a place to write down what would run today. The next race is 5-of-5.

- **The arena family's live debt is mostly paid** (2026-08-09, race t9 on the
  reference box, after three earlier races each exposed and funded a fix —
  swallowed worktree errors, turn-number collisions, the agy print-timeout,
  a racer pushing mid-race, a stalled seat holding the turn). t9 was the clean
  run: all four seats raced, agy finished live for the first time, codex ended
  with "nothing pushed" under the conduct preamble, a stalled cursor was cut
  loose live with `x`, the colored patch toggle read cleanly, and the winner
  went through `/adopt` → `/arena drop` → a merged PR. **The design doc's own
  per-amendment verification notes were reconciled against t9 on 2026-08-09**
  (six of eight had gone stale in the other direction, still claiming debts t9
  had paid — including §9.37 telling you no live `/adopt` had run three
  paragraphs above an open question filed *from* the first live `/adopt`).

  **What is genuinely still unpaid, and how to pay it.** Three items, in
  ascending cost. Nothing here needs a session of its own; item 1 costs no API
  spend at all. **The symlink-refusal item that led this list is paid** —
  Developer Mode went on 2026-08-09 and the test PASSES on this box; the
  measurement and its two mechanics live in [PARITY.md](PARITY.md).

  1. **§9.38's composer paste** (design.md §9.38's own "live verification
     owed"). No vendors, no race, no spend: open `telltale council` in Windows
     Terminal, copy a three-line snippet, paste. Expect ONE insertion, three
     composer rows, ZERO dispatches; then enter sends it as one brief. A paste
     that lands as separate turns means the terminal did not bracket it —
     record the Windows Terminal build in `PARITY.md`, because that is a
     vendor fact, not a council bug.
  2. **`.worktreeinclude` seeding** and 3. **`u` undo on a real racer commit**
     — both need one live race, and **one race pays both**. The design doc
     frames the seeding debt as needing "a repo that actually needs a `.env`";
     it does not. This repo can pay it with a throwaway git-ignored file:

     ```
     mkdir dist && echo probe > dist/arena-seed-probe.txt   # /dist/ is ignored
     printf 'dist/arena-seed-probe.txt\n.env\n' > .worktreeinclude
     ```

     Two lines on purpose: the first matches, the second matches nothing, so
     one race shows both `seeded 1 files` and the named no-match notice — the
     seed line's zero-vs-absent rule, live. (Verified 2026-08-09 that
     `git ls-files --others` in this repo does enumerate that path, which is
     the candidate set `loadSeedPlan` reads.) Then, in the room:

     - `telltale council --fresh --vendor claude` — **seat one vendor.** A race
       is not routable, so the roster IS the cost control; the four-seat
       default is roughly 4× the spend for nothing this debt needs.
     - `/arena add a one-line comment to the top of internal/council/glyphs.go
       saying nothing but the date` — a brief that reliably CHANGES a file.
       Watch the `arena · so far` block appear and **grow** (that pays the
       remaining half of §9.37's live-stat note), and check the column says
       `seeded 1 files` plus the no-match notice.
     - When it lands: expect a diff stat and `committed <sha>.`
     - **Between turns** (`u` refuses while a turn is in flight): `esc` to view
       mode, focus that seat, press `u`, then `y`. Expect the card to name the
       base, then the stat to STAY on the column under an "undone" line, and
       `git -C ../telltale-arena-t9-claude log --oneline -1` to be back at the
       base. That pays item 3. Press `u` again to see the already-undone
       refusal, which costs nothing.
     - Clean up: `/arena drop claude!`, then `rm .worktreeinclude` and
       `rm -r dist`. **Then put the roster back** — a typed `--vendor` list is
       saved as the room's roster, so the next launch is still one seat until
       `/seat all` or `--vendor all`.

     Cost: one short brief to one seat — cents, and a few minutes. Note the
     race will mint **t9** again: t9's branches were dropped but t8's survive,
     and the number comes from the refs.

  **Housekeeping this turned up, not owned:** 27 `arena/t<N>/<vendor>` branches
  and 28 sibling `telltale-arena-t*` worktrees from races t2–t8 are still on
  the reference box (`telltale-arena-t5-codex` is at a detached HEAD — the
  residue of the racer that pushed its own PR). Kept-until-deleted is the
  ruling, so this is a decision to make rather than a bug: `/arena drop` only
  reaches the current room's race, so clearing older ones is
  `git worktree remove` + `git branch -D` by hand.

- **QUEUED, unowned — the post-audit build list (2026-08-09; shrunk same
  day).** The steal sweep's survivors, reconciled against an independent
  read-only second-model audit and then against the live repo. The audit's
  sharpest catch: subset @-mention routing was on the steal list and had been
  shipped in `mentions.go` since 2026-08-04 — so verify any steal against the
  repo before building it. Items 1–4 (needs-you strip, gate before/after
  preview, `telltale doctor`, version-pinned wire fixtures) landed 2026-08-09
  and are gone from this list per its own keep-it-true rule; what landed is
  git's record, not this file's. One pre-demo item remains; the 2026-09-30
  demo is the deadline.

  1. **A scripted fallback demo tape** (optional, taste work): a checked-in
     recording script (e.g. VHS) that captures the demo path end-to-end, so
     a vendor or network failure on the day cannot erase the presentation.
     Deliberately not built with the rest: there is no defined demo path yet
     to record, and scripting one before it exists would invent the demo
     rather than capture it.

  Post-demo shelf — decided, deliberately not queued: a measured-silence
  advisory ("no event for 5m", never "stalled"); rendering an explicit
  zero-output turn distinct from absent; one visible, editable queued room
  draft (never auto-send); an import verb for adopting work from outside the
  room (a new verb with its own measurements, not an overload of `/adopt`);
  per-racer port allocation, when an arena brief first needs live servers.

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
  this bullet is `codex` and `agy` — and now `grok`, which is a batch program of
  the same shape (§9.39) and has not been timed on this axis. All three have no
  seam anyone has found; their `wait` is a CLI cold-start floor nothing in this
  repo can move.

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
