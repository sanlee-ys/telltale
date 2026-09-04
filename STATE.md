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

**Amended 2026-09-02: the room is a crew, built offline, and the live runs it
owes are one list.** Between the tag and the next minor the room moved from a
committee to a crew — per-seat turns and an inbox (design.md §9.54), a worktree
per writing seat with the room as integrator (§9.55), routing by measured
headroom and a record/replay pair (§9.56), three long-lived seats with a
measured fallback behind each (§9.57), a host that outlives the terminal on
macOS and Linux (§7.30), and a Homebrew tap with an Apple Silicon CI job. Six
lanes built it apart and it was integrated on one branch; every behaviour is
pinned over stubbed processes and no lane and no integration ran a vendor. So
the next minor's precondition is not more code: it is the checklist at the head
of *In flight*, run on the reference box and the Mac, each line retiring one
`unmeasured` badge, one "owed" sentence, or one "has not run" claim. The outward
chain above is unchanged by this; the demo path's eight beats still stand, and
a demo of the crew is a demo of measured claims only after that list is paid.

## In flight

- **The crew's live measurements, ONE checklist (2026-09-02).** Consolidated
  from the six lane memos so nobody reassembles it from five design sections.
  Each line is one action on the reference box (the Mac where it says so) and
  the badge, sentence or claim it retires. Record the result in the design.md
  section the line names, then delete the line here; the section's own
  checklist is the durable copy.

  1. `@codex <brief>`, then `@grok <brief>` while codex streams; `ctrl+c` on
     codex's column; then `@codex` again — retires §9.54's "a persistent seat
     taking its next brief cleanly after a per-seat cancel".
  2. `@all` in a five-seat room at 170x54: the header `turn N → … · K in
     flight` and the footer's `cancel <seat>` / `cancel all` at real widths —
     retires §9.54's width note.
  3. `/flow @codex … -> @claude …` while `@grok` is mid-answer — retires
     §9.54's "a hop landing beside an unrelated seat, artifact intact".
  4. A write brief to `@claude` and `@codex` at once in a git workspace: two
     `wt: seat/<v>` badges, the persistent seat's "now works in its own
     worktree" note with its thread resumed there, then `/adopt codex` and
     `/adopt claude` — retires §9.55's tree, respawn and adopt items.
  5. `/flow @codex write:a.md … & @grok write:b.md … -> @claude …` — retires
     §9.55's fanned stage and join.
  6. A write brief in a directory that is not a git repo: `⚠ shared tree ·
     not a git repo` at real widths — retires §9.55's badge item.
  7. `--replay demo.jsonl` of the real recording made 2026-09-03 (`--record`
     and `replay-check` are PAID; §9.56 carries the measurement) — retires
     §9.56's synthesized-fixture note and the README's "tested offline".
     **Replay half MEASURED 2026-09-03, and it found a defect:** every
     persistent seat replayed as `failed` at each dispatch, on the replaced
     process's exit; fixed at the recorder and repaired at the reader (§9.56's
     dated paragraph). The rendering of that recording is the density pass's.
  8. The routing cell against the live quota relay, and `@auto` with Claude's
     5h window beside agy's weekly bucket — retires §9.56's routing items.
  10. Codex approvals: a write room, a brief that writes `%TEMP%\outside.txt`
      and runs `curl example.com`, `y` and `n` on separate trials, the disk
      checked — retires `app-server · asks · unmeasured` (§9.57 item 2).
  11. Codex interrupt and stop: `ctrl+c` mid-turn, then `q`; `turn/completed`'s
      status, and seconds from stdin close to exit over five runs against the
      4 s grace — retires §9.57 item 3 and the integration's stop timing.
  12. Codex fallback: the seat against a build without `app-server`, or logged
      out; the column's "fell back to `codex exec --json`" note and the
      postures page's `exec · unasked · fallback` — retires §9.57 item 5 and
      `fallback.go`'s offline-only standing.
  13. Item 9 on the Mac, into PARITY.md — retires the off-Windows
      `ro:requested` (§9.57 item 4).
  16. Grok `costUsdTicks` unit: one turn checked against grok.com's billing —
      retires §9.57's cost half (token counts are on the wire since
      2026-09-04; the tick is not). The permissions and `session/load` halves
      were PAID 2026-09-04 (§9.57's grok items). One design choice came out
      of them and is open: the server asks about nothing until a
      `/always-approve off` prompt is sent, after which it asks before a file
      write and not before a shell command. The seat sends no such prompt, so
      the gate cannot card its writes; sending one is a new frame on the
      brief channel, and whether the room should is the owner's call.
  17. Antigravity stream: two `{"event":"user",…}` lines down one stdin; same
      pid, same `conversation_id`, the second turn recalling the first; the
      exit timed after stdin close against 3 s — retires `stream-json ·
      unasked · unmeasured` (§9.57 item 10).
  18. Antigravity `--conversation <id>` and `--print-timeout 30m` under stream
      input — retires §9.57 items 11 and 12.
  19. Antigravity without `--input-format`: the exit code and stderr line —
      confirms the retreat's trigger (§9.57 item 13).
  20. On the Mac: `telltale council --host --read`, `/detach`, `telltale
      council ls`, rejoin, `/quit`; then a host left up, `telltale council
      kill`; then `kill -9` on a host and `ls` — retires §7.30's "macOS is
      NOT measured" and PARITY's "expected to match".
  21. Tag a release and publish the draft; on the MacBook `brew tap
      sanlee-ys/telltale https://github.com/sanlee-ys/telltale`, `brew install
      telltale`, `brew test telltale`; record it in PARITY — retires "not yet
      exercised by a `brew install`".
  22. Unpack the `darwin_arm64` archive by hand and run `telltale doctor` —
      retires PARITY's "not walked at all".


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
     transcript ([design.md §9.38](docs/design.md)'s dated payment).
     **The render caveat this beat carried is RETIRED, 2026-08-17.** It read
     that the composer drew the three-line draft as one row, so the demo should
     trust the wire and not the row count. A live 5/5 room at a recorded 170x54
     drew three rows, matching the sweep, so wire AND render are both verified
     and the beat can be demonstrated as it reads.
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
  drive on 2026-08-15/16 (race t9), each with a live run behind it. Beat 3's
  render caveat was the one residue, and it is retired as of 2026-08-17 — the
  path now carries no unpaid marker.

  **Amended 2026-08-17: the beat ORDER is reopened, by the owner's ruling.**
  A re-cut is planned for the final two weeks before the demo: open on the
  room, fold `doctor` into a later credibility moment, and add `t` and `Y` as
  the payoff after the `@all` beat. The eight surfaces stay; only the order
  moves. The re-cut lands as its own edit to this entry before the final
  rehearsal, and until it lands the order above is the one to rehearse.

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
  **That §9.38 thread is now closed end to end**, and this line said otherwise
  until 2026-08-17: a pasted multi-line draft WAS sent with enter on 2026-08-15/16
  (one dispatch, the newlines verified as bytes in the seat's own transcript), and
  the composer's row count — the last half in doubt — reproduced as three rows in
  a live 5/5 room at 170x54 on 2026-08-17. Nothing on the paste path is owed.

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

     **Ruled 2026-08-17:** a committed capture is a publication, and the owner
     approved its control — every frame gets a review for workspace paths,
     session names and seat identity before any capture (the README hero GIF
     included) is committed to the repo.

  Post-demo shelf — decided, deliberately not queued: a measured-silence
  advisory ("no event for 5m", never "stalled"); rendering an explicit
  zero-output turn distinct from absent; one visible, editable queued room
  draft (never auto-send); an import verb for adopting work from outside the
  room (a new verb with its own measurements, not an overload of `/adopt`);
  per-racer port allocation, when an arena brief first needs live servers.

- **`/arena` writes the brief as `AGENTS.md` too — built 2026-08-29, one live
  half owed.** The competitor sweep's candidate rested on agents.md's own claim
  that 20+ tools read the file natively. That claim was MEASURED first, one
  headless probe per vendor CLI on this box, and the build was conditional on
  the result: codex 0.149.1 and grok 1.0.5 both answered a codename only that
  file carried, with no tool call, so both ingest it unprompted; Claude Code
  2.1.251 answered by running `ls -la` and `cat` on it, which is a read but not
  the same fact; agy 1.1.20 and cursor are UNMEASURED. The full table and the
  rulings it forced are [design.md §9.37](docs/design.md)'s 2026-08-29
  amendment.

  What a future session needs from this entry rather than from git: the room
  writes the file for every racer and **claims it for none** — no column, no
  notice and no snapshot field says a seat was briefed this way, because two of
  five seats are unmeasured and the room cannot tell per race which ingested
  it. Do not add such a render without measuring the missing seats first.

  **Owed:** the probes ran in a scratch directory, not inside a racer worktree
  during a real `/arena`. No live race has yet watched a seat act on the file.
  One race against a brief whose answer depends on something only the file says
  pays it.

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

- **The codex sandbox claims are pinned at 0.149.1 and the installed build
  is 0.151.0 (2026-09-01).** The chip that traced the seat's `failed (exit 1)`
  re-ran the three seat argv shapes at 0.151.0 and all parsed, so no flag
  moved. It could NOT re-run the sandbox probes of [design.md
  §9.2](docs/design.md#s9-2)'s 2026-08-29 amendment: every turn that day
  died at the account's usage limit before a tool ran. The `ro:enforced`
  badge on Windows therefore rests on a build two minors behind the one
  installed. Re-run the four probes (read-only read, read-only write,
  workspace-write `.git`, resume override) once the account has quota; the
  commands are on the constants in `internal/council/vendors/codex.go`. The
  0.147.0 resume-not-found stderr path is unmeasured at 0.151.0 for the same
  reason (`vendors/testdata/wire/README.md`).

- **The launch playbook's outward half is OWNER WORK and is unowned
  (2026-08-18).** [design.md §8](docs/design.md#s8)'s 2026-08-18 amendment
  records the cadence; recording it is all a session may do. Three pieces wait
  on the owner. **Directory listings** (`awesome-claude-code` and its
  neighbours) are pull requests to other people's repositories, which takes
  winget's ruling: a human action, never automated, never opened by a
  contributor session — and the README badge slot fills only after a listing
  merges. **The Show HN cadence** is sequenced behind chain link 3 and is
  pinned to one hypothesis, so a second post tests its own feature's question
  and says so. **The run-evidence bar's threshold is undecided**: §8 item 2
  already fixes the KIND of evidence, and the count, the window, and what a
  miss means are the owner's to name. The sweep's "10 runs in 30 days" is a
  proposal with no measurement behind it and was deliberately not adopted.

- **No third-party MCP client has connected to `telltale mcp`** (2026-08-18,
  design.md §7.25). The mode is verified against the built binary by a scripted
  stdio client — seven messages in, six responses out, exit 0, the tool's
  document validated against `docs/snapshot.schema.json`, and `~/.telltale`
  byte-identical before and after — and CI drives the same sequence on every run.
  What that proves is a correct server. It says nothing about how a shipped
  client negotiates a version, orders its requests, or renders the result,
  because wiring one up writes an entry into the operator's own client
  configuration and that entry is his to make. One `claude mcp add telltale --
  <path>\telltale.exe mcp` followed by one tool call pays this in a minute. The
  command's shape is read off `claude mcp add --help` at Claude Code 2.1.233,
  not assumed.

- **A live ordinary-turn give-up is owed on the reference box before
  2026-09-30.** `x` on an ordinary turn shipped 2026-08-17 with offline tests
  only. Whether a real vendor's interrupt lands mid-turn, and whether the
  persistent seat's next brief resumes the interrupted conversation, has no
  live payment yet. design.md §9.37's dated amendment carries the debt.

- **`telltale history` reads one vendor of seven, and the other six are surveyed rather than
  owned (2026-08-29).** design.md §7.26 carries the per-vendor verdicts and the mode prints
  them on every run, so this entry does not restate them. What it records is the INTENT: codex
  is the next slice and it is unowned, and it is blocked on a ruling rather than on work —
  a codex rollout carries `info.last_token_usage` (this turn) beside a cumulative
  `info.total_token_usage`, and which of the two a day may sum is a decision nobody has made.
  agy is refused for want of a per-turn TIMESTAMP, which is a survey finding and not
  a todo: if that vendor ever dates its counts, that is a new measurement and the verdict is
  re-taken, not assumed. **grok was refused on the same ground and that was wrong, corrected
  the same day** (#316): a live re-measure at grok 1.0.5 read a `turn_completed` record off
  disk carrying a full input/output/cache split beside the envelope's own timestamp. The
  survey had read `internal/adapter/grok`'s struct, which parses `totalTokens` alone, and a
  record struct is an allowlist — what it omits is a decision, not an absence. grok is still
  uncovered and is now unowned rather than refused, behind one named unit trap: its
  `inputTokens` INCLUDES the cache read where claude's `input_tokens` excludes it. The
  general rule that bought is in §7.26 — before a vendor is built there, re-read its
  records, not its struct — and it applies to every remaining row.

- **The arena record has never been opened against a real pile of leftovers
  (2026-08-29).** `/arena record` (design.md §9.47) tallies every seat's adopted-of-decided
  standing from the repository's own `arena/` and `adopt/` branches. It stores nothing, so
  there is no state to be wrong — but every count it prints is a claim about what a real ref
  pile produces, and the only refs it has ever read are the ones its tests build. This box
  holds 27 `arena/t<N>` branches and the `adopt/*` refs beside them, which is exactly the
  input the feature was written for. One `/arena record` in a live room pays this: read the
  counts, check them against `git branch --list "arena/*" "adopt/*"`, and confirm the page
  reads at the room's own geometry. No vendor is spawned and nothing is written, so the debt
  costs a keystroke.

- **The attempt review surface has never been driven live (2026-08-29).** `D` and `o`
  (design.md §9.49) put a hunk of a racer's patch in the composer draft and start the
  operator's editor on the worktree. Both are offline-tested only, and both make a claim a
  suite cannot settle. `D`'s is end to end: the quoted hunk has to survive the composer, the
  seeded `@mention` has to route the turn to the seat under review, and the seat has to be
  able to act on a worktree path that is not the room's workspace — one live race, one `D`,
  one dispatched turn pays it. `o`'s is narrower and shares `y`'s known limit: council can
  measure that the process STARTED and nothing more, so whether a real `$EDITOR` draws a
  window, and what a terminal editor does when the room owns the screen, needs one press
  against a real setting. Neither costs a vendor turn to check the second half.

- **No live race has run under the arena check (2026-08-29).** `/arena check <command>`
  (design.md §9.48) runs one operator-named command in each racer's worktree and renders PASS
  or FAIL from its real exit code. Every mechanic is pinned offline, and one test runs a real
  process to prove the exit code is READ rather than assumed — but that process is the test
  binary, in a temp repository, exiting on demand. What no test can witness is the case the
  feature exists for: a real vendor's attempt meeting a real suite in a real worktree. Two
  things are unmeasured there and only there. Whether a check that runs for MINUTES reads well
  on a column whose turn has already ended, since the run outlives the turn by design and the
  spinner does not. And whether a real racer's tree leaves the check something to do — a
  worktree seeded by `.worktreeinclude`, with the vendor's own build state in it, is not the
  clean fixture the tests build. One `/arena check go test ./...` followed by one `/arena`
  against a brief that changes files pays both.

- **The `/adopt` divergence preview has never been armed in a live room
  (2026-08-29).** The card now leads with measured git state — the racer's
  ahead/behind counts against the room's own HEAD, and the paths both sides
  wrote — before it names the merge (design.md §9.37's dated amendment). Every
  sentence it can print is pinned by offline tests against real temp
  repositories, and none of them has been read on a real pile of leftovers. The
  same keystroke pays it as the arena record above: race, then `/adopt <seat>`,
  read the card and press `n`. Nothing is merged by arming the gate, so the debt
  costs one race.

- **The HYBRID adopt has never run against a live race (2026-08-29).**
  `/adopt <seat> +<seat> <path...>` merges one attempt whole and takes named
  paths from another, on `adopt/t<N>-<base>+<donor>` (design.md §9.37's dated
  hybrid amendment). The git mechanics and all four refusals are pinned by
  offline tests against real temp repositories (`hybrid_test.go`), and the
  arena record's hybrid state is pinned against ref lists with its own golden.
  Neither has met a real race. **The same race pays this and the two debts
  above**: race, `/adopt <seat>` and press `n` for the preview, then
  `/adopt <a> +<b> <path>` and press `n` for the hybrid card, then `/arena
  record`. Arming a card merges nothing, so all three cost one race and no
  commit. Paying it fully — pressing `y` — additionally wants a look at the
  receipt commit, which is the sentence the feature stands on.

- **Per-HUNK adoption is deferred, not rejected (2026-08-29).** The hybrid ships
  per-PATH because a hunk picker is a new full-frame body with its own scroll,
  keys and mode word. The grammar does not block it: a picker would narrow what
  `+<seat>` contributes and leave `/adopt <seat> +<seat> <path...>` alone. Its
  own change, judged against §9.37 like every other item on that list.

- **OBSERVED 2026-08-15/16, the drive that paid the demo path found three.
  The 2026-08-16 lane batch measured all three, and what is left is smaller.**
  1. **CLOSED 2026-08-17. The composer one-row report did not reproduce.** The
     decisive live measurement named here was taken: a three-line brief into a
     live 5/5 room in Windows Terminal at a recorded 170x54 drew **three rows**.
     That agrees with the 588-combination sweep, so the render is now verified
     both synthetically and live, and the wire half was already paid. The
     original one-row sighting stands unreproduced and is recorded as a probable
     observation error — §9.38 documents the trap that produces one, the echo's
     ambiguous row breaks, and the composer's row count was the one figure still
     read by eye. Named honestly in §9.38's dated amendment: 170x54 is a
     generous geometry, so it closes the reported defect and says nothing about
     the tight cells near the 60x10 floor.
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
  gated turn exercises it for free. **The same debt now covers the WORD beside
  that figure**: §9.45's 2026-08-29 amendment makes a blocked column say
  `needs you` instead of `streaming`, and it too is pinned by goldens alone. One
  live gated turn pays both at once. The codex seat's **post-answer linger is
  OWNED as of the same day's lane batch**: the column settles at the measured
  `turn.completed` marker and renders `done Ns exiting`, the process lifetime
  untouched (§9.33's dated payment block). What that lane left open, stated
  there: the linger's CAUSE is unmeasured and the tool-using turn's tail is
  unmeasured (probing it needed `danger-full-access`, a redline, when every
  sandboxed spawn failed on Windows; since the 2026-08-29 re-measurement at
  codex-cli 0.149.1, `-s read-only` runs commands there — §9.2's dated
  amendment — so the probe is possible now and still unrun). The agy tail
  is now MEASURED and ruled (§9.43's dated amendment): 0.049–0.314s behind a
  reliable `result` marker, too small to justify `EndsTurn` on a whole-second
  clock, and a test pins the refusal — what stays unknown there is the FAILED
  agy turn's tail (all three trials ended SUCCESS), which is the path that
  settles at the vendor's `status: "ERROR"` line while the clock bills to
  exit. The two adjacent council holes that lane named are closed by the same
  day's second wave: a failed agy turn settles its column instead of leaving
  the room idle-but-locked, and a `/cd` persists when it happens (both carried
  in dated design.md amendments; `gh pr list` has the numbers). That wave's
  own two residues are closed by the third: both listeners now greet a taken
  port with a named error (§7.16a, §7.21), and the OTLP collision has a binary
  smoke in CI, and the smoke's eventsink twin landed with the fourth wave's
  schema gate — that queue is empty. The fourth wave also retired two
  completeness misses by ruling: the drift loop lives in `doctor`, and snapshot's
  first measured consumers exist (CI's schema gate, and a driven example in
  `tools/fleet-prompt.ps1`). The drift loop found the claude and codex field-map
  surveys stale on its first run and both were RE-MEASURED the same day — claude
  at 2.1.233 over 179,614 live records, codex at 0.147.0 over 330 rollouts, every
  CapNone gap re-confirmed and all four comparable pins now match (§3.1/§3.2
  re-measure blocks). The **demand gate is LIFTED** (owner, 2026-08-16): a
  measurement wave then surveyed the three needs-input hook seams and all three
  REFUSED — claude's Notification fires on a cancellable 6s timer with no tool id
  (§9.40), agy never waits in print mode (§9.43), cursor maps Claude's
  needs-input events to null and an awaiting-human moment is byte-identical to a
  silent success (§9.46); the affirmative signal is ACP's `session/request_permission`,
  which only the driving seat sees, so council's gate cards stay the strip's
  source by measurement rather than default. The same wave shipped the drop-file
  relay (a self-reported row that cannot impersonate a measured one, §7.23),
  govulncheck/CodeQL/SBOM/provenance (a `go 1.26.6` bump cleared 3 reachable
  stdlib CVEs), and closed a measured browser exfiltration of both listeners
  (§7.24). The
  **`codex app-server` re-measurement is PAID (2026-08-29)** and the caution it
  answers was right: the `exec` findings do not port, and the new path is worse
  where it counts. At 0.149.1 its `read-only` sandbox DENIES a write when a
  process starts, but its tool router goes through `pwsh.exe`, pwsh cannot start
  under the Windows sandbox on this box, and in two of three read-posture arms
  the model abandoned the turn rather than inspecting — the 0.146.0-class defect
  #311 had just cleared off `codex exec` the same day. So the protocol ships as a
  second `runner.Protocol` and **the seat does not move** (§9.50). **AMENDED
  2026-09-02: the seat MOVED without that re-measurement** (§9.57), on the crew
  ledger rather than the sandbox one, with `codex exec` kept as the fallback,
  the seat owning its kill, and every codex badge reading `unmeasured at
  0.152.1`; the off-Windows read badge dropped to `ro:requested`. The three
  things below are still open and are now the head of §9.57's checklist
  rather than a precondition. Three things
  stay open and gate the flip: the read posture's LIVENESS (a turn that lists a
  directory and reads a file under the sandbox the badge claims), whether a write
  outside the workspace is denied (the one arm ran under `%TEMP%`, which
  `workspaceWrite` permits by default, so it measured nothing), and whether a
  per-turn `sandboxPolicy.writableRoots` can buy `.git` back where `exec` on
  Windows cannot. **The agy statusline re-capture is
  PAID (2026-08-17)** and the pin moves to 1.1.13: fifteen live payloads
  confirmed FOUR quota buckets rather than two, `agent_state` was observed live
  (including one value the documented vocabulary omits), and the payload has
  grown to carry the §7.16b context block. It also FALSIFIED `transcript_path` —
  the payload names a directory that does not exist, so §2.1's refusal to display
  it is now a measurement rather than caution; no code ever followed the path,
  which was re-checked across the repo (§3.8's re-capture block). The
  **multi-chunk transcript** is now PINNED synthetically (§3.8's
  2026-08-16 amendment: the adapter reads the flat file by decision and proved
  correct under its own contract, and the head/tail overlap guard was shown
  load-bearing) — the live multi-chunk capture remains the missing instrument,
  since a synthetic fixture cannot say which behavior agy actually has. **The
  cursor statusline capture is PAID (2026-08-17)**: a live interactive session at
  cursor-agent `2026.08.11-e8db854` rendered `ctx 12.7%` after its first reply,
  so `used_percentage` is observed populated at one-decimal precision and the
  synthesized fixture's assumed shape was correct (§7.16's amendment, which also
  names the build caveat — that capture ran one build ahead of the section's own
  pin). The Claude payload capture landed 2026-08-16: seven live
  fires at the pinned 2.1.233 agreed with the source read on every count, and
  §7.16b's amended limitation carries the record. Two more items from that
  batch's tail: the seat's advertised **capabilities are parsed for the record
  and gate nothing** (#285) — `subagentStatusLine` was REFUSED as a source and
  `FieldSubagents` stays `CapDerived` — and the event sink's **reader exists as
  its own foreground mode** (#286, `telltale events view`), reading the day files
  rather than the sink's endpoints so it still answers after the sink exits.

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

- **Whether telltale should draw HUD rows for its own council seats is UNDECIDED
  (2026-08-29).** `internal/adapter/cursor` now reads the `cursor-agent` CLI's
  session manifests ([design.md §3.9](docs/design.md#s3-9)'s 2026-08-29
  addendum), and `~/.cursor/acp-sessions/<uuid>/meta.json` carries the same
  shape — 49 manifests, same three key sets, measured the same day. It is
  deliberately NOT read. Those are `cursor-agent acp` sessions, which is the
  server council's own Cursor seat runs (§9.36), so reading them would put the
  room's seats on the grid beside the operator's own work. That may be right; it
  is a product question nobody has answered, and the reader is one root away
  from it either way.

- **The PTY "live seat" is BUILT, and it has never been driven (2026-09-01).**
  [design.md §9.53](docs/design.md#s9-53) holds the whole design and the
  measurements the spike made; the paragraph below is kept because it records
  what was measured and where, and because every trap in it is still a trap.
  What changed is that the seat exists: `runner.StartPTY`, an `x/vt` emulator on
  `Model`, decoded rows on `State.Live`, and a sixth spawn var in the guard.
  What is still owed is the drive. **No `claude` interactive session has ever
  been run through a pane.** The spawn guard makes that impossible from inside
  the suite by design, so it is the same class of debt as the host's first live
  turn two entries down: an operator-driven check. Also unbuilt: no key sends a
  keystroke into the pane, so the pane is a window rather than a terminal.

- **The PTY spike's own measurements (2026-08-31).** The
  spike ran on this machine — Windows 11 build 10.0.26200.9168, go1.26.6 — and
  its findings are recorded here because nothing else in the repo holds them.
  **ConPTY needs no new dependency**: `golang.org/x/sys/windows` v0.47.0 already
  exposes `CreatePseudoConsole`, `ResizePseudoConsole`, `ClosePseudoConsole` and
  `PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE`, and a working host is about 160 lines.
  **`CREATE_NO_WINDOW` is the trap.** `internal/council/runner/proc_windows.go`
  sets it on every child today, and a ConPTY child created with it emits ZERO
  bytes, accepts no input, and exits 0 with a nil error — a silent total failure
  an implementer reaches by copying the existing spawn helper.
  `DETACHED_PROCESS` fails too; `CREATE_NEW_PROCESS_GROUP` and `HideWindow` are
  compatible. **There is no console flash**, measured with a differential
  `EnumWindows` scan carrying a positive control; ConPTY's own conhost reports a
  `PseudoConsoleWindow` that never paints, so a future guard test must match on
  `ConsoleWindowClass` instead. **The repaint reputation is stale on this
  build**: a 5000-line flood was 97.3% printable payload, scrollback replayed
  losslessly at 5000 lines, and an idle static TUI costs 0 bytes/sec. **`os/exec`
  cannot spawn a ConPTY child** — Go's `SysProcAttr` has no attribute-list field
  — so the spawn half must call `windows.CreateProcess` directly; the existing
  job object then contains it unchanged. **`fit` is not sufficient**:
  `lipgloss.Width` counts cursor-move and erase escapes as zero cells, so an
  emulator that consumes them is mandatory. **Only Claude Code can take a PTY
  seat** — it is the one `vendors.Persistent` seat, via `--input-format
  stream-json`; Codex, Antigravity and Grok exit every turn, and Cursor speaks
  ACP with no TUI. **UNVERIFIED**: only build 26200 was tested, the Windows 10
  1809 floor is documentation-only, alt-screen guests were not exercised, no
  streaming agent turn was run, and the longest run was 20 seconds.

- **Host memory is unbounded, and nobody has measured how fast it grows
  (2026-09-01).** [design.md §7.28](docs/design.md#s7-28) named a turn ceiling
  as owed before detach shipped, and [§7.29](docs/design.md#s7-29) shipped
  detach without it and says so. A number picked now would be a guess presented
  as a limit: nothing has measured what a room accumulates per turn, so the
  measurement comes first. What the ceiling must then do is settled — the drop
  is stated in the header rather than applied silently, on the retention
  discipline `telltale events` already has (§7.21).

- **The hosted room draws with the plain client, not council's own `Model`
  (2026-09-01). PAID 2026-09-02, [design.md §7.31](docs/design.md#s7-31).**
  `telltale council --host` and a rejoin now draw with the room's own columns,
  badges, trace, turn page, panes and help panel, and `/detach` is typed into
  the composer. The host's projection carries the whole turn, the client
  builds a `State` from it with one pure function, and the host takes a brief
  per seat. The plain client is the path when stdin is not a terminal. Still
  owed, in order:
  1. **The owner's live drive on the built binary.** No lane can dispatch a
     vendor through a host. One brief through `telltale council --host --read`
     on this PR's binary, then `/detach`, a closed window and a rejoin, must
     show the columns and the badges survive the rejoin. That drive is the
     closer for this entry. **Attempted 2026-09-03 and stopped before the
     rejoin**: the hosted room drew the columns, the rail and the badges, and
     a claude seat answered through the host; then another session installed
     a fresh `telltale.exe` while the host ran, Windows renamed the host's
     file to `telltale.exe~`, and the liveness probe called that a reused pid.
     `telltale council` removed `host.json` and rebuilt five seats over a host
     that was alive. The probe now forgives that one rename
     (`sameImage` in `internal/councilhost/process_windows.go`, with the
     measurement). **The rejoin half ran 2026-09-04 on `cc851af`, with no
     reinstall in flight, through the plain client (stdin piped):** one read
     brief to `claude,codex,agy,grok` in this repo; all four answered and the
     persistent seat settled `done`; `/detach` left pid 6980; `ls` reported
     RUNNING; `telltale council` printed *rejoined the host that was already
     running … nothing was rebuilt, and no session was resumed* with all four
     answers intact; a second `/detach`, then `kill`. What that run cannot
     show is the TUI half of the claim — columns, rail and badges surviving a
     rejoin on a real terminal — because a piped stdin takes the plain client
     by design. That observation is the owner's, one command while a host is
     up, and it is the only piece of this line still open.
  2. **The live seat in a hosted room.** `--live` with `--host` is refused
     with one sentence. A pseudoconsole child in the host and its cell grid on
     the wire is a second wire format and a second spawn guard.
  3. **The rebuttal in a hosted room.** `ctrl+r` is refused with one
     sentence. A quoting turn hands each seat a different prompt, and the
     dispatch frame carries one.
  4. **A hosted room starts every seat fresh.** The host is handed a roster
     and never a saved session id, so no thread is resumed and no `Restored`
     card is drawn. This predates §7.31 and is named there.
  5. **The host does not write `room.json`.** §7.28 said it would, on the
     room's own schedule; no code in `internal/councilhost` reaches `SaveRoom`,
     so a hosted room's session ids never reach disk. Named in §7.31.
  6. **The frame cost is unmeasured.** Every frame carries every seat's
     history, bounded by the 50 ms tick and the 8 MiB line ceiling. Nothing
     has measured a long room against that ceiling, and the host's memory
     ceiling above is still owed with it.

- **The host refused two of five seats after the crew merge, and §7.31 took
  the fallback (2026-09-02).** §9.57 made codex's and grok's registry entries
  request/response live shapes, and the host marks a conversational seat
  undrivable, so from that merge until §7.31 a hosted room could drive only
  claude and agy. The host now drives each such seat through its measured
  batch adapter (`vendors.LiveFallback`) and says so on the badge, the way the
  room retreats on a refused handshake. The agy seat is the exception: its
  stream shape is `Persistent`, so the host drives the live shape, which
  §9.57 lists as unmeasured. No hosted room has dispatched to it.

- **No vendor has ever been dispatched to THROUGH A HOST (2026-09-01).** Every
  seat spawn in `internal/councilhost`'s suite is stubbed, by design — the spawn
  guard exists to stop a test spending a real turn, so neither CI nor a session
  can close this. The first live turn through a host is an operator-driven
  check, and it is the same class of debt as the demo path's live-drive items.
  **Detach did not change this and could not.** §7.29's suite measures the
  process facts with real processes — a real client detaches and exits, the host
  outlives it, a second client rejoins it, and a `taskkill /F` on the detached
  host reaps a real seat — but every one of those seats is this test binary
  re-executed, and the host's roster is empty by construction.

  **MEASURED 2026-09-01, on the built binary, at Windows 11 Pro 10.0.26200:**
  the whole operator path ran against a sandbox `HOME` and a real five-seat
  roster — `telltale council --host --read`, `/detach` (pid named), `telltale
  council ls` reporting the live host, `telltale council` rejoining it, a second
  `/detach`, then `telltale council kill` ending it and `ls` reporting none. The
  write refusal ran too: `telltale council --host` warned before it opened and
  refused the `/detach` in the ruled sentence. **NO VENDOR WAS SPAWNED IN ANY OF
  IT**, and that is what the run does not pay for rather than a caveat on it: a
  host starts a seat's process on the FIRST DISPATCH and this drive never
  dispatched, so five columns drew `idle` (and `cursor` drew `undrivable`, the
  §9.36 refusal) without a vendor existing. What is still owed is one brief
  through a hosted room — a real vendor answering, streaming into a detached
  host, and still being there on the rejoin.

  **MEASURED 2026-09-02, by the owner, on the built binary:** one brief ran
  through `telltale council --host --read`, then `/detach`, a closed window,
  and a `telltale council` rejoin. The two batch seats settled to `done (exit
  0)`. The claude seat answered in full and STAYED `streaming`, through the
  detach and the rejoin, with the identical text. The host dropped the
  persistent seat's own end-of-turn line, so the seat never settled and the
  turn guard refused every later brief. The fix and the test that pins it are
  in `internal/councilhost/room.go` and `persistent_done_test.go`. The drive
  paid the first half of the debt above: a vendor answered through a host and
  was still there on the rejoin. A second live brief on the fixed binary is
  the check that closes it.

Cross-platform and cross-machine status has its own file: [PARITY.md](PARITY.md).

The conventions a fresh session would otherwise re-derive — golden-test traps,
commit voice, the honesty rules, the read/write boundary — are in
[CLAUDE.md](CLAUDE.md).

## How to re-enter

1. Read this file, then `PARITY.md` if you changed machines.
2. `gh pr list` for open work; `telltale hud` for live sessions.
3. Update *In flight* and *Open questions* in the same PR that changes them.
