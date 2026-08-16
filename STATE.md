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
   **RAN, 2026-08-15/16, race t9** — three clean finishes with commit receipts,
   two seats given up with `x` after vendor-side stalls, both keeping their
   receipts, and `/adopt` exercised through its dirty-room refusal and then
   cleanly. The full record is design.md §9.37's dated payment block.
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
     **Paid, 2026-08-15/16.** A pasted three-line brief was sent with enter:
     one dispatch, and the newlines verified as bytes in the seat's own
     transcript ([design.md §9.38](docs/design.md)'s dated payment). One
     caveat rides with it: the composer DREW the three-line draft as one row
     (the render gap below), so the demo should type or paste the brief and
     trust the wire, not the row count.
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
     **Paid, 2026-08-15/16, race t9 — both halves.** The first 5-of-5 ran, and
     two columns were watched drawing a non-empty `arena · so far` that GREW
     before the settled block replaced it ([design.md §9.37](docs/design.md)'s
     dated payment carries the full record, including the two give-ups).
  7. **`telltale hud`.** The grid shows the fleet at a glance. `enter` opens the
     detail pane, whose `not sourced` line names the fields that vendor can
     never source. A `~` on a context cell marks a percentage telltale computed
     rather than read.
  8. **`telltale snapshot --compact`, piped to a parser, 10 seconds.** One line
     of JSON carries the same truth as the grid, for a reader that is a program.
     Absent is `null`, a measured zero is `0`, and no optional key is omitted.

  **The path is frozen as recorded.** Both markers were paid by the owner's
  drive on 2026-08-15/16 (race t9), each with a live run behind it. The one
  residue is beat 3's render caveat, owned in the gaps below.

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

  It also arrived AFTER the first race t9 below (2026-08-09), which is why that
  entry says four seats: a record of what ran is not a place to write down what
  would run today. The first 5-of-5 has since run — the SECOND race numbered t9
  (2026-08-15/16; the first t9's branches were cleared, and race numbers come
  from the refs, so the number was honestly re-minted — lifecycle.go rules that
  intended). Design.md §9.37's dated payment block is that race's record.

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
     **Recordable as of 2026-08-15/16.** The demo path is defined above and
     both of its markers are paid, so a tape now captures claims somebody has
     watched. The recording chain is its own decision (the research sweep's
     roadmap names PowerSession-rs + agg and the reasons VHS cannot record on
     this machine); nothing here picks it.

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

- **OBSERVED 2026-08-15/16, the drive that paid the demo path found three.
  The 2026-08-16 lane batch measured all three, and what is left is smaller.**
  1. **The composer one-row report is unexplained, and its recorded suspect
     is refuted.** The render is measured correct at every geometry the room
     draws (the 588-combination sweep in §9.38's second dated amendment): the
     compose area wraps against the terminal width, so the seat count has no
     path to the composer's row count, and the one silent-collapse branch is
     unreachable above the 60x10 floor. The wire half was already paid. What
     remains is one decisive live measurement: reproduce the paste in a live
     5/5 room and record the terminal's rows and columns with it.
  2. **Arena turns were always traced; the record could not name the race.**
     The "records nothing" premise did not survive measurement — racers'
     records were emitted all along, and a racer's line now ends
     `race=arena/t<N>` on both spawn paths. The t9 empty trace file therefore
     has another cause, still open; the leading candidate was gap 3's
     workspace-`~` reattach resolving `/trace dist/…` against the home
     directory, and gap 3 is now fixed. The live half is owed: no race has
     run against this build.
  3. **The workspace restore was measured correct; the silent fallback beside
     it was the bug, and it is fixed.** A saved workspace that no longer
     exists was replaced with the current directory without a word, and the
     next turn persisted the substitute. The reattach notice now names the
     vanished path (§9.32's dated amendment). The next-turn overwrite itself
     stays, by design: `room.json` never describes a room nobody is in.

- **MEASURED and CLOSED, 2026-08-15/16. The operator's `deny` still stops the
  call with council's `ask` beside it — and council is never asked at all.**
  This was the last open item from the gate-hook build of 2026-08-12
  ([design.md §9.8](docs/design.md), third dated block, which carries every arm
  and its verdict; shipped as PR #241). Claude Code 2.1.228, Windows 11, two
  trials per arm, throwaway directories, the filesystem as the observable. A
  probe hook with the credential guard's own SHAPE — matcher `"*"`, a reason on
  stderr, exit 2 — went in the throwaway workspace's own `.claude/settings.json`;
  the operator's `~/.claude/settings.json` was never edited and no credential
  store was copied anywhere.

  **The deciding arm answered the card `allow`, and that is the design.** A
  denial pressed at the card leaves nothing on disk whatever the hooks do, so it
  cannot tell a holding `deny` from a displaced one. With council's gate hook
  and the probe `deny` in front of the same call, and `allow` at the callback,
  no request was emitted and nothing was created, 2/2. The control changed one
  thing — the probe hook exits 0 — and the marker landed 2/2, which proves the
  pipeline and proves council's `ask` still fires when nothing denies.

  **Both hooks run, and the `deny` ends the evaluation.** `--include-hook-events`
  showed two `PreToolUse` hooks starting before either answered: council's
  returned `permissionDecision: "ask"` at exit 0, the probe returned exit 2 with
  its stderr. The model read the OPERATOR's sentence, not council's. So the
  guard is not merely ranked above the gate; the gate is never reached. One
  catch worth its line: on a turn where nothing was created, the CLI's own
  `post_turn_summary` said the blocked command "executed" — a stream-reading rig
  would have recorded the opposite of the truth.

  **The 2026-08-14 narrowing stands as history**: the built seat was driven with
  the operator's settings loaded, the two hook sets coexisted, and the carded
  shell command was refused for its redirection, not its pipe —
  `routineSegments`' first guard rejects `<` and `>` before it splits anything,
  and `2>&1` is real redirection. The card was right; only the first sentence
  explaining it was not.

  **Two changelog claims were treated as hypotheses and both are confirmed at
  2.1.228**: exit code 2 blocks (2.1.222) and a hook's `ask` is not overridden by
  auto mode (2.1.221). The version floor for this measurement is 2.1.221, and
  any earlier reading of it is void.

  **What stays unmeasured is narrow, and named so nobody reads the finding wider
  than it is:** an operator hook that denies by printing
  `permissionDecision: "deny"` as JSON rather than by exit code 2, and `defer`.
  The real guard uses exit 2, so the rig measured the shape that ships here. The
  adopter arm stays unrun for the reason it always has: copying a credential
  store into a probe directory is a redline.

- **RESIDUES of the 2026-08-16 roadmap batch (PRs #237–#247), each small and
  unowned.** The gate clock split (#243) is proven by unit tests and goldens
  only — no live gated turn has rendered `you 4m48s` yet; the owner's next real
  gated turn exercises it for free. The codex seat's **post-answer linger is
  OWNED as of the same day's lane batch**: the column settles at the measured
  `turn.completed` marker and renders `done Ns exiting`, the process lifetime
  untouched (§9.33's dated payment block). What that lane left open, stated
  there: the linger's CAUSE is unmeasured, the tool-using turn's tail is
  unmeasured (probing it needs `danger-full-access`, a redline), and the other
  spawn-per-turn vendors never set `EndsTurn`, so their tails are untouched —
  for agy this now has a sharper edge, because a failed agy turn settles at the
  vendor's own `status: "ERROR"` line while the turn clock keeps billing until
  exit, and no capture of the agy process exit after a failed turn exists.
  The two adjacent council holes that lane named are closed by the same day's
  second wave: a failed agy turn settles its column instead of leaving the
  room idle-but-locked, and a `/cd` persists when it happens (both carried in
  dated design.md amendments; `gh pr list` has the numbers). Two small
  unowned residues from that wave: `telltale events` still greets a taken
  127.0.0.1:4519 with the raw Go bind error (`telltale otel grok` got the
  named-error treatment; the event sink did not), and the OTLP collision has
  no binary smoke in CI. The
  **Windows `danger-full-access` finding does not port to `codex app-server`**
  — that path has its own `windowsSandbox/*` surface nobody has probed; any
  seat move re-measures rather than inherits. The **agy statusline payload is still pinned at 1.1.9** and
  needs an interactive re-capture — expect FOUR quota buckets now, not two
  (§3.8); the **multi-chunk transcript** is the case that would break that
  adapter and has never been observed (all chunked conversations hold one
  chunk). One capture is owed on the cursor statusline seam (§7.16's
  amendment carries the one-minute manual step for a populated
  `context_window`). The Claude payload capture landed 2026-08-16: seven live
  fires at the pinned 2.1.233 agreed with the source read on every count, and
  §7.16b's amended limitation carries the record.

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
