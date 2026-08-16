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

v1 is cut: **v0.2.0, 2026-08-14**. The room is built, tagged and downloadable,
and every claim it makes was checked by the person who wrote it.

**The next minor is the OUTWARD CHAIN** (owner's ruling, 2026-08-15). Three
links, in this order, and each one is the input to the next:

1. **The demo path is defined.** One named route through the room, written
   down rather than improvised on the day. A separate entry owns what that
   path is; this one owns only that it must exist first, because both links
   below consume it.
2. **The first 5-of-5 arena race runs.** Every seat racing one brief at once.
   The grok-seat entry below has said "the next race is 5-of-5" since the
   fifth seat landed, and it has not run — so a five-seat room is still a
   claim this project has never driven under load.
3. **The launch post fires.** The post, the install it points at, and a reader
   who arrives at the room the post described.

**What the chain buys is a room that has been witnessed** — driven end to end
by someone who did not build it. That is the one thing a tag cannot supply and
the one thing this project has never had. It is also the standard the v1 gates
were written against ([docs/design.md §1](docs/design.md)): a stranger who
reads the post and installs must find the room the post described. The tag
asserted that; the chain tests it.

Nothing here pauses development. Work merged after the tag is the next minor,
and the tag itself stays one command (design.md §8).

**The direction is recorded rather than remembered** (2026-08-06; v1 gate
re-cut 2026-08-08): council is the product, the gauges are the infrastructure
under it. Cutting v1 as gauges only was the standing alternative and it was
rejected. `README.md` and [docs/design.md §1](docs/design.md) hold the binding
copies, the gates and the argument; this file does not restate any of them.

## In flight

- **The 3-minute demo path is DEFINED (owner's ruling, 2026-08-15).** It is link
  1 of the outward chain above, and both later links consume it. Eight beats, in
  order. Every beat names a surface that exists today. Two beats carry an honest
  marker, because they rest on a configuration nobody has run yet.

  1. **`telltale doctor`, 15 seconds.** The preflight names each seat it
     resolved, where the binary was found, and the version it reports. One
     sentence explains `not checked`: nothing here probes a login or calls the
     network, so auth and network never read as a pass.
  2. **`telltale council`.** The bare command opens the one room and reattaches
     to the standing conversations. Each seat picks up its own thread, and the
     turn counter continues at N+1.
  3. **One brief, addressed `@all`.** Five columns answer side by side. Before
     enter, the composer footer states the resolved ROUTE: the seat names, plus
     the seat count from two seats up. The footer says which seats the turn
     reaches. It shows no money, and this beat must never promise a price.
     **Awaiting its first live run.** The demo pastes a multi-line brief, and
     [design.md §9.38](docs/design.md) records that a pasted draft has never
     been sent with enter. Typing the brief is the fallback.
  4. **One honesty beat.** A column whose vendor reported no cost renders no
     cost cell at all. A turn that reported zero renders `$0.0000`. The two
     states stay apart on screen. The `~` estimate marker is NOT on this
     surface: council renders no `~`, so beat 7 carries that half.
  5. **`y`.** The focused column's reply goes to the clipboard over OSC 52, and
     a notice names what was copied. The sequence returns no acknowledgement,
     so the paste is the proof.
  6. **`/arena <brief>`, then `/adopt <seat>`.** Each seat races the same brief
     in its own git worktree, on branch `arena/t<N>/<vendor>`. A racing column
     shows `arena · so far` while it works. A finisher carries its rank, its
     phase word and its own clock. An attempt that changed something shows
     `committed <sha>.`. `/adopt` cuts `adopt/t<N>-<vendor>` behind a y/n card
     and merges the attempt there.
     **Awaiting its first live run, twice over.** No 5-of-5 race has ever run:
     grok landed after the last full race, so the next race is the first with
     five seats. No live race has watched the `arena · so far` stat grow from a
     non-empty read either ([design.md §9.37](docs/design.md)).
  7. **`telltale hud`.** The grid shows the fleet at a glance. `enter` opens the
     detail pane, whose `not sourced` line names the fields that vendor can
     never source. A `~` on a context cell marks a percentage telltale computed
     rather than read.
  8. **`telltale snapshot --compact`, piped to a parser, 10 seconds.** One line
     of JSON carries the same truth as the grid, for a reader that is a program.
     Absent is `null`, a measured zero is `0`, and no optional key is omitted.

  **The path is recorded, not frozen.** Beats 3 and 6 keep their markers until
  the owner's next drive pays them. Do not strike a marker without a live run
  behind it, and do not write down a result neither beat has produced.

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

- **The arena family's live debt is PAID IN FULL** (2026-08-14). Everything
  below this paragraph is the history of how it got there; nothing in it is
  owed. Races t13 and t14 on the reference box closed the last three items in
  one sitting — `seeded 1 file` with its named `.env` no-match notice, a `u`
  undo whose branch really went back to its base and whose second press refused
  as already undone, and a live `/adopt` that cut `adopt/t14-claude`, merged
  `arena/t14/claude` with `--no-ff`, left `main` where it stood and named
  `gh pr create` as the next command. The same sitting was the first live race
  under the finish fix (design.md §9.37): 18 seconds to retire, against the 21
  minutes that found the bug. §9.38's composer paste is paid too, on Windows
  Terminal 1.24.11911.0 — one insertion, three composer rows, zero dispatches.
  **The one thing still unexercised is named in design.md §9.38** and is not an
  arena debt: a pasted multi-line draft has never been sent with enter.

- **How the family got here** (2026-08-09, race t9 on the
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

  **The symlink-refusal item that led the old unpaid list is paid** — Developer
  Mode went on 2026-08-09 and the test PASSES on this box; the measurement and
  its two mechanics live in [PARITY.md](PARITY.md). The three items under it
  are paid as of 2026-08-14 and their recipe is deliberately gone: a
  step-by-step for work nobody owes is the derived-fact copy this file's
  opening warns about, and the measurements it produced belong in design.md
  §9.37 rather than here.

  **One thing that drive established is worth keeping, because it is not
  written anywhere else.** All three were UNPAYABLE beforehand and nobody knew
  it: a race against a seat the room had already dispatched to never retired
  its column, so the settled arena block — the seed receipt, the commit
  receipt, `u`'s base and `/adopt`'s candidate all come out of it — never
  arrived. Two earlier drives hit it and both wrote it off as a quit. **A debt
  that will not pay may be a broken feature rather than an unfinished errand**,
  and the tell was that its residue looked identical every time: an uncommitted
  racer edit on a branch still sitting at its base.

  **Housekeeping, not owned:** every race leaves an `arena/t<N>/<vendor>`
  branch behind, `/adopt` leaves an `adopt/*` branch, and some of them still
  hold worktrees. Kept-until-deleted is the ruling, so leftovers are a decision
  rather than a bug.

  **This file no longer carries the count.** It carried one twice and it was
  wrong both times, by wide margins — a hand-maintained copy of a derived fact
  is precisely what this file's own opening warns against, and git already
  holds the answer. Ask git:

  ```
  git branch --list "arena/*" "adopt/*"
  git worktree list
  ```

  Two things a future session should know before clearing anything: `/arena
  drop` only reaches the CURRENT room's race, so anything from a closed room is
  `git worktree remove` + `git branch -D` by hand, and a worktree must go WITH
  its branch — deleting branches alone drops the highest ref and the next race
  re-mints a number a leftover worktree still holds.

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
     **Unblocked 2026-08-15, and now waiting on the drive rather than on a
     decision.** The demo path is defined above, so there is a route to
     record. Two of its beats still carry an awaiting-first-live-run marker,
     and a tape cut before that drive would script a claim nobody has watched.
     Record it after the drive pays those two beats.

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

  **Amended 2026-08-10: "friction the room already handles" holds only while
  someone is answering cards.** Press `a`, or walk away, and a `-C` call stops
  being a card and becomes the operator running git in a second terminal —
  which is the one thing a command surface exists to remove. Measured the same
  day, in a live room: `git -C <abs> status --short` was refused while a plain
  `git status --short` from the same working directory ran untouched. The seat's
  cwd is already the workspace, so **`-C` buys nothing here and costs the
  allowlist**. The conflict worth naming is that the global command-shapes
  convention mandates `git -C` precisely to avoid `cd &&` chains: right in a
  terminal, inverted inside a seat. `CLAUDE.md` carries the instruction; this
  entry carries why.

  **Amended 2026-08-14, after PR #223: still true, but only in the posture
  that was never PR #223's subject.** #223 (`230ba54`, merged 2026-08-12)
  rewired the GATED posture only — it dropped `--setting-sources ""` as the
  default and made council inject its own `PreToolUse` hook
  (`internal/council/gatehook.go`) — and never touched `PostureWrite` or the
  `autoAllowedTools` constant this entry is about. That constant, and the
  `--allowedTools` prefix-only matcher it feeds, still cannot see through
  `-C`; nothing in the code measured this changing, so the original finding
  stands unmodified for the ungated write posture.

  What #223 makes worth saying explicitly is that the GATED posture
  (`PostureWriteGated`) never had this problem, on a *different* mechanism
  than the one this entry measures: council answers its own gate with
  `autoApproveRoutine`, which calls `safeGitArgs`
  (`internal/council/persistent.go`), and that function strips a leading
  `-C <path>` pair before classifying the subcommand — `git -C <path> status`
  and `git status` land on the identical branch. `gate_git_test.go` pins
  `"git -C /tmp/ws status"` in the same allow-list as `"git status"`. That
  stripping shipped 2026-08-05 in PR #69, a week before this entry's own
  2026-08-09 measurement and eight days before #223 — so the gated posture was
  never the bug, and #223 could not have fixed something that was never
  broken there.

  Reconciled live, same day: the owner's own write-seat session ran
  `git -C C:/Users/sanle/code/telltale check-ignore -v dist/gate-drive.txt`
  and it drew a card the seat answered — but `check-ignore` is not one of
  `safeGitArgs`'s recognized subcommands (`status`, `log`, `diff`, `show`,
  `fetch`, `add`, `commit`, `pull`, `push`, `switch`, `checkout`, `branch`),
  so that call would have carded with or without `-C`. It confirms the
  instruction was followed, not the size of the `-C` penalty in whichever
  posture that session was running.

  **Net: the instruction in `CLAUDE.md` stays, unconditionally, for a
  narrower reason than either amendment states alone.** A seat cannot see its
  own posture from inside, plain `git` costs nothing extra in the posture
  where `-C` was never a problem, and it avoids the approval in the posture
  where it still is. `CLAUDE.md` now names both mechanisms and both
  postures.
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

- **UNMEASURED, 2026-08-12. Council now adds a `PreToolUse` hook to a file the
  operator also populates, and the two have never been run together.** This is
  the one item left over from the gate-hook build that shipped the same day
  ([design.md §9.8](docs/design.md), second dated block — the build itself is
  done, and the four other open items with it). The docs rank `deny` over
  `defer` over `ask` over `allow` when several hooks answer, which makes a
  weakening unlikely: a `deny` from the operator's own hook beats council's
  `ask`. "Unlikely by documentation" is the standard of evidence that section
  exists to distrust, and the credential guard is exactly the hook that must not
  come back weaker for council having added one beside it. What would settle it
  is one arm of the same rig: a probe settings file carrying council's gate hook,
  run on a machine whose own `PreToolUse` hook denies a known shape, asserting
  the denial still holds.

  **Narrowed by a live drive, 2026-08-14, and NOT closed.** The built seat was
  driven for real with the operator's settings loaded: the postures page showed
  the wired sentence, a `Read` drew no card, a `Write` drew one and landed on
  `y`, and a shell command carded — **for its redirection, not for its pipe.**
  The command was `ls -la dist/ 2>&1 | head -30`, and the reason first written
  down here was the wrong one. `routineSegments`
  (`internal/council/persistent.go`) has split `|` and `&&` since PR #73
  (2026-08-05) and classifies each segment on its own; `ls` and `head` are both
  in `readOnlyCommands`, so `ls -la dist/ | head -30` passes untouched. What
  refused this command is that function's FIRST guard, which rejects any
  command containing `<` or `>` before it splits anything — redirection writes
  to a path the classifier never inspects, and `2>&1` is real redirection
  rather than a spelling worth carving out by hand. `gate_git_test.go` already
  pins `go test ./... 2>&1 | tail -2` as refused for that exact reason. The
  card was right; only the sentence explaining it was not.

  So the two hook sets demonstrably COEXIST without
  breaking the gate — which is worth knowing and is not what this gap asks.
  **No operator `deny` was exercised**, so whether their credential guard still
  holds with council's `ask` beside it is exactly as unmeasured as before. The
  rig above is still the thing that would settle it.

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
