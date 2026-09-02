# telltale council — the dispatch room

`telltale council` is one brief, typed once and answered by five vendors side by side —
**Claude Code**, **Codex**, **Antigravity**, **Cursor** and **Grok**, each in its own
column, in your terminal. It exists because the alternative is five terminals and a
clipboard.

This file is the room's own guide: what each mark on the frame means, how a turn is routed,
how to read five answers at once, how to get one back out — and how to race one brief
across every seat and keep the attempt you like. [README.md](../README.md) is
the front door and states the project's claims; [design.md §9](design.md) is the record
behind this one — what was measured on each vendor, what each seam cost, and what is still
unverified.

```
telltale.exe council
```

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="../images/telltale-council-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="../images/telltale-council-light.svg">
    <img alt="telltale council dispatch room showing multi-agent panel" src="../images/telltale-council-dark.svg">
  </picture>
</p>

That picture is emitted from the council test suite's `hero` golden
(`internal/council/testdata/golden/hero.txt`) with its empty rows dropped, and nothing
else changed — including the `READ` in its header, because the fixture opens a `--read`
room. A plain `telltale council` says `⚠ WRITE` there instead. The five columns are the
five addressable seats (Claude Code, Codex, Antigravity, Cursor, Grok); the three-column
`activity` golden stays a unit fixture and is not the public picture.

## The seat header

**A seat header names the key that reaches it, the tag it wears everywhere, and the state
it is in.** `▸ 1 CC Claude Code ──────── ✓ done`: `▸` is focus, `1` is the key that jumps
straight to this seat, `CC` is the two-letter tag — the HUD's own, character for character,
so a reader who learned `CX` is Codex from the grid does not meet a second abbreviation
here — and the name is bound to its state by a leader rule, the same `label ─── value`
shape the turn separators use further down the transcript. The state leads with a mark:
`✓ done`, `✗ failed`, `○ idle`, `⚠ cancelled`, a spinner while a turn is in flight. The
mark is always a second signal; the word carries the distinction on its own, so `--ascii`
and a monochrome terminal lose nothing (`design.md` §9.11). Each seat's name also carries
its own hue — the one place council spends a colour it did not inherit, ratified as an
exception because "which of five agents is speaking" is a distinction the statusline and
the HUD have no seats to make (§9.28); the tags carry it first, so a terminal scheme that
renders two of them alike loses nothing that was load-bearing.

Every claim in the two header lines is made per vendor, never as a blanket:

- **The sandbox badge.** `ro:tools` is Claude under `--disallowedTools` plus
  `--strict-mcp-config`, and it claims only that *these named tools are absent, verified* —
  a deny list cannot cover a tool a later release adds. `ro:requested` is a flag that was
  accepted but whose effect is unestablished. `unsandboxed` is a column with no read-only
  posture at all, and it deliberately does not open with `ro:`, because a reader scanning
  three headers takes in the prefix before the qualifier. Antigravity wears it because it
  was asked to write a file under both of its own read-only flags and wrote it: refuted,
  not unverified.

  **On Windows, Codex wore it too until 2026-08-29**, and the frame above is a rendering
  fixture rather than a capture from any one machine. At codex-cli 0.146.0, `-s read-only`
  and `-s workspace-write` were both measured failing *every* process spawn there — including
  one asked merely to list a directory — so council passed `-s danger-full-access`, the only
  mode that ran, and said so. Re-measured at codex-cli 0.149.1, the sandbox enforces: a
  shell write under `-s read-only` was denied with no file on disk, so the read posture
  passes `-s read-only` again and the seat is `ro:enforced` on every OS. The write posture
  still passes `danger-full-access` on Windows only, because that build's sandbox denies
  `.git` and refuses the override that unlocks it elsewhere — a seat that cannot commit
  builds and never lands (design.md §9.2's 2026-08-29 amendment).
- **The streaming granularity.** Only Claude streams (`tokens`, verified live). Codex and
  Antigravity were measured to emit nothing at all until the turn ends, so they are
  labelled `final only` and open on a waiting card that says so, rather than on an empty
  column that reads as slow streaming.
- **The clock and the cost.** Each column times its own turn. Cost renders only when the
  vendor reported one — a turn that reported `$0.0000` shows it, a turn that reported
  nothing shows no cost cell at all, and telltale never derives a cost from token counts.

## Reading the badges

The badge is the one thing on a column header where misreading it has consequences, so
here is the whole vocabulary in one place. **`?` twice inside the room** shows the same
table, followed by the full claim each of your own seats is making.

| badge | what it means for you |
|---|---|
| `ro:tools` | The write and shell tools are **absent** from that session, so it cannot edit your files. Verified by reading what the session reported about itself, not by trusting a flag. Residual: a deny list cannot cover a tool a future release adds. |
| `ro:enforced` | The vendor's own **OS-level sandbox** is applying it — `codex -s read-only`, on every OS since codex-cli 0.149.1. The one posture here that an operating system rather than a flag is behind. |
| `ro:requested` | A read-only flag was passed and accepted, and **what it actually enforces was never observed**. Weaker than the two above, and it says so rather than borrowing their word. |
| `unsandboxed` | **Nothing restricts this vendor at the OS level** — measured, not assumed. Treat the column as able to change your files. It deliberately does not open with `ro:`, because a reader scanning four headers takes in the prefix before the qualifier. |
| `WRITES` | The room can write — the default. This column may edit and run things in the workspace. |
| `gated` | The room can write, and this seat **asks first**: `y` approves, `n` denies, and nothing runs until you answer. Only the seat driven as a live process can be asked; the others say `WRITES` rather than implying they can. |

Two of those need the same answer to the obvious follow-up — *must they stay that way?*

**No badge is what keeps this room out of your files. The workspace is.** `unsandboxed` is
not a setting anyone chose to leave on: on Windows both of Codex's sandboxed modes were
measured failing *every* process spawn, reads included, so `-s read-only` was not a
restriction but a seat that could not read its own repo. Antigravity was asked to write a
file under both of its own read-only flags and wrote it. Grok wrote its file under
`--permission-mode plan` too — and its `--sandbox` flag silently ACCEPTS a profile name
that does not exist, so there was nothing there council could even observe, let alone
claim. Those are measurements, and the badge reports them. The control that actually holds is the directory council was pointed
at — so if a room should not be able to touch something, point it somewhere else:

```
git worktree add ../telltale-council
telltale council --cd ../telltale-council
```

That is also the fleet's own ruling rather than a local convenience: `agent-ops` ADR-012
rules capability parity — every vendor reads and writes, and guard wiring rather than lane
shape is the control. A column that looked read-only because of a broken sandbox was never
a safety property; it was a defect wearing one's clothes. To fulfill the ADR-012 guard
obligation for Grok, Grok's `PreToolUse` fleet guard is configured via `[compat.claude] hooks = true`
in `~/.grok/config.toml` (or by registering the `PreToolUse` hook via `grok hooks-add`). This routes
Grok tool invocations through the standard `PreToolUse` credential-guard screen (denying secret leaks
and dangerous mutations) without restricting Grok's execution capability.

The `⚙` lines are the activity trace: what a vendor is *doing* — the tool call, and the
command it ran — interleaved with what it says.

## Routing: who a turn actually reaches

The room is an operating committee, but routing defaults to the **control plane**, not a
broadcast. An unaddressed brief goes to **Claude alone**; `@codex`, `@agy`, `@cursor` and
`@grok` (and `@claude`) name seats for one turn; **`@all`** (also `@everyone` / `@council`)
convenes every seated vendor. Mentions used to narrow from an everyone-default; that
inverted after measured council burn billed scarce seats on every casual turn. Only
leading mentions route, so "ask @claude about it" stays prose.

**`-@claude` goes the other way: everyone seated *except* that seat.** Same position, same
aliases, same case-insensitivity — it is the mention grammar with a minus in front, not a
second vocabulary. `-@codex -@agy` subtracts two. The footer names the result before you
press enter: `→ everyone but claude`.

The two forms may not be **mixed**. `@claude -@codex` is refused with a notice rather than
reconciled, because it is over-specified rather than under-specified: `@` starts from
nobody and adds, `-@` starts from everyone and subtracts, and a line that does both states
two contradictory theories of who is in the room. Picking one silently is exactly the
hidden decision the live routing indicator exists to prevent, so the room does not.
`@all -@claude` is *not* that case and is accepted — `@all` names everyone, so it names
the set the exclusion subtracts from. An exclusion that leaves
nobody (`-@all`, or naming every seat you have) gets the same "none of the vendors you
addressed are seated" notice a mention of an unseated vendor already gets.

**The routing cell states the bill before you press enter.** `→ everyone (3 seats)`,
`→ everyone but codex (2 seats)` — because how much `everyone` is depends on what is
installed and on what `--vendor` left in the room, which is the part a reader cannot get
from the word. One seat states no count: `→ claude` already names everything it reaches.
The number counts seated ∩ addressed through the same call the dispatch gate uses, so it
never prices a seat that will not be spawned, and a refused route prices nothing at all.
Once the turn is away the composer's cell resets to the next draft, so the header carries
the live one instead — `turn 10 → everyone` — and drops back to plain `turn 10` when the
last column lands, because at that instant the route is history and the transcript is
where history goes.

**A committee brief is drawn once, not once per seat.** From two seats up, the live turn's
brief sits full width under the room chrome and the addressed columns stop echoing it —
one question above five answers instead of the same paragraph across the top of every
reading area. It spends at most four rows, and when the brief is longer the fourth row
says how many rows are missing and where the whole of it is, because a reader must never
have to wonder whether they are looking at their own question or a truncated copy of it.
Nothing new is stored: this is a rendering rule over the echo each column already held,
history keeps its per-column copies, and if spending the band would leave the columns
fewer than eight rows of body it is not spent at all.

**A seat takes a brief while another seat is still answering.** The room is a crew, not a
committee: a turn is a fact about a *seat*, and seats are busy or idle one at a time. Hand
Codex a refactor, and while it runs hand Grok the docs — `@codex <brief>`, enter, `@grok
<brief>`, enter — and both columns stream at once, each on its own turn number and its own
clock. The room refuses a brief only for a seat that is *still answering*: `@codex` while
Codex is mid-answer gets `a turn is in flight on codex (turn 4) — ctrl+c on its column cancels
that turn, or address another seat`, and the draft stays put. A brief that names several seats
goes to the idle ones and says who was skipped and why: `sent to grok, agy — skipped: codex
(turn 4), still on a turn; ctrl+c on its column cancels it`. That is the rule that keeps the
persistent seats honest too: a stream-json or ACP process holds one turn open at a time, and
the refusal is what stands between it and a second prompt written into a process mid-turn.
A turn number is a *dispatch* number — turn 5 is the fifth brief the room sent, whoever it
went to — so the separators, the by-turn page, `/retry` and the saved room all keep reading
one coordinate; a seat's own history is the subset of those numbers it took part in. The
header names the most recent dispatch's route and, when more than one seat is answering,
counts them: `turn 5 → codex · 3 in flight`, measured over the columns. The one turn that
still needs the whole room is a race: `/arena` refuses while any seat is busy, and every
ordinary brief is refused while a race runs, because a race owns every worktree and every
seat until its last racer lands.

**The routing cell says which seat has headroom, before enter.** On a crew the question the
seat readings exist to answer is "which seat has room for *this* brief", so it is answered
where the route is, while the route can still be changed. When the draft addresses a seat
whose relayed quota window is at or above the threshold (`--headroom-warn N`, default 90
percent used), the cell says so: `→ codex · 5h 94% used`, or on a route that names a set,
`→ everyone · codex 5h 94% used`. The figure is the vendor's own, copied from the same relay
the badge row reads (the statusline's `~/.telltale/quota/<vendor>.json`), and it is the only
thing the cell adds. A seat with no reading gets no number and no word: Cursor and Grok have
no quota anywhere telltale can read, and an unrelayed Claude is the same absence. A reading
whose window has reset since the relay wrote it is absent too, not a stale number, and a
reading past the five-hour age mark is the alarm's (`⚠ claude stale 19h ago`), not the
hint's. A window at 100% is the alarm's as well. The hint stops one short of it.

**`@auto` hands the choice to the readings.** As a route word it picks, among the seats that
are seated and idle and have a measured reading, the one with the most headroom in its
shortest window (the window that resets soonest), and the cell states the pick before enter:
`→ auto: grok (5h 12% used)`. Ties go to seating order. Enter sends the brief to that seat and
the notice repeats the choice, `@auto → grok (5h 12% used)`; the header, the transcript and
the saved room record a turn to grok, because that is what happened. A seat with no reading
is never ranked, a busy seat is never picked, and when no seated seat has a reading the cell
reads `→ auto: no measured reading` and enter refuses with `@auto needs a measured reading;
none of the seated seats has one`, keeping the draft. `@auto` beside a named seat or an
exclusion is refused like `@` beside `-@`: two theories of who chooses.

Turn 1 is blind. Later turns ride each vendor's own native session resume rather than
re-sending the transcript, which keeps that guarantee structural: each session holds only
its own history. `ctrl+r` arms a rebuttal turn — off by default — in which each vendor
sees the others' last answers, fenced and labelled as untrusted material. The snapshot is
taken per seat, at the moment that seat's brief is sent: a neighbour that is still
answering contributes its last *finished* reply rather than the half it has streamed so
far, and nothing at all if it has never finished one.

## Reading five answers at once

**Reading is a first-class mode.** `↑`/`↓` scroll the focused column, `pgup`/`pgdn` move by
a screenful, `g` and `G` reach the first turn or the newest, `tab` moves between columns,
`1`–`N` go straight to the Nth seat on screen, and `f` expands one column to the full width
— which is what you want when an answer is long rather than when five of them are being
compared. `[` and `]` walk the focused column **a turn at a time**, landing the turn's
separator on the top row where the brief and the answer to it read together. The scrollback
had only ever moved in lines, so `↑ 509 more above` was measured, correct, and counted in a
unit nobody reads a conversation in — while *how far back is what I asked* was the one
question a reader actually had, and the only two answers were both ends. All of that works
**while composing** as well as in view mode, which is the point: a finished turn drops the
room back into compose, so the mode you are in when four long answers land is the mode you
need to read in. Keys that can be text stay text there — `j`, `k`, `g`, `G`, `q`, `t`, `c`,
`d`, `D`, `o`, `u`, the digits and the brackets are all just characters in the composer — and the mode
line says which set is live on every frame. `?` lists all of them and says when it is holding
more than fits (`↓ 5 more below`); `?` again explains what the posture badge on each column
means, with your own seats' full claims underneath; `?` a third time closes the panel.

**Two projections, one transcript.** The grid reads by seat. **`t` reads by turn**: one
page, the brief once at the top, then every seat that took *that* turn under its own
labelled rule, in seating order, with the turn's own clock — the longest seat's elapsed,
because a turn is over when its slowest seat lands, and never a sum or a mean council would
have had to derive. It is the document `Y` already pasted, rendered; both come from the same
call, because a page and a paste that disagreed about who was in a turn would be two
honest-looking records with nothing on screen to say which was the room's. A seat that sat
the turn out does not appear on its page — it still holds an older reply, and filing that
under this turn's heading would be the room inventing a conversation on the one surface
built to compare them. `[` and `]` move the page the same way they move the grid, `t` goes
back, and the mode word says `TURN 10/11` when a newer turn has landed behind you, because
a turn arriving never moves the page out from under a reader.

**The keys move one column, and the frame says which.** `▸` marks it, its name is the only
one at full weight, the gutter to its left thickens to `▌` for as far as the column's own
text runs — the one mark on this surface as tall as the thing it describes — and the
unfocused columns' prose steps back one contrast level. Its overflow
marker names the keys that would move it and where in the conversation the fold falls
(`↑ 25 more above  │  turn 3  │  ↑↓ scroll  │  f expand`). A column those keys do *not*
reach says so instead — `↑ 36 more above  │  tab to focus` — rather than repeating a hint
that would be false on that seat, and it gets no coordinate, because putting the question
in front of the answer is how the original bug worked. That distinction is the whole of
`design.md` §9.12: everything scrolled correctly, and a reader looking at the third
seat had no way to tell that the arrows were moving the first. Under `--ascii` the rail is
`[` and the focus mark `]`; under `NO_COLOR` the contrast step is what goes, and every
other carrier is still there.

**The strip under the header is the crew's inbox.** `⚠ NEEDS YOU` used to name only the
seats stopped on an approval card. It now also names the seats whose turn *ended* while you
were looking somewhere else — `⚠ NEEDS YOU   2 Codex   3 Grok done   4 Cursor failed` — with
the phase word saying how, in the same word the column header uses. Both kinds of entry are
measurements: a card the gate queue holds, or a landing the room stamped against the last
time your keys were on that column. Going to the seat is what takes it off; a seat that
lands again later comes back. `.` jumps to the next seat on the strip, wrapping, and the
footer names that key only while the strip has somebody on it; the digits still reach any
seat by position. Nothing is stored about what you acknowledged — the strip is a comparison
between two timestamps the room took itself, which is why it cannot drift.

**A backgrounded seat says where it left off, once.** Since the default route became one
seat, the other three sit out most turns — and a column of `⚠ not addressed in turn 2` /
`turn 3` / `turn 4` buried the answer that seat actually gave. Consecutive skips now
coalesce at render time into one muted `○ not addressed in turns 2–7`; the mark is `○`
rather than `⚠` because sitting a turn out is not a failure, and the live turn's skip keeps
its own line because that one is a decision you are still making. Nothing is written down
for a turn a seat did not take, so `[` and `]` still hop between real turns, and a run is
never claimed back past the oldest record the room still holds. Narrowed to a strip, the
seat carries `last: turn 8 ✓` — the turn it last took and that turn's own mark — above the
run.

Mouse wheel scrolling is deliberately absent, and the measurement is written down
(`design.md` §9.10): the terminal protocol has no wheel-only mode, so enabling the
wheel means claiming the left button too — and that costs native click-drag selection of
the very answers this room exists to produce.

## Taking an answer out of the room

`y` copies the focused column's reply; `Y` copies the whole current turn — every seat that
took part, each under its own heading, with the brief that produced it at the top, so it
pastes into a document as a readable record rather than as four anonymous blocks. Both take
the same sanitized text the screen is showing, never the raw stream, and a notice says what
was copied because the mechanism cannot report back: `y` while a vendor is waiting on a gate
still means **approve**, and yank simply does not exist until you have answered. On a turn
page the two keys produce the same document, because a per-seat `y` needs a per-seat focus
and a projection whose whole unit is the turn deliberately has none. One exception, ruled
with the race itself: **on a seat that raced this turn, `y` copies its diff**, not its
prose — the diff is that seat's deliverable, and the notice says so (`copied Codex's arena
diff (turn 7)`), naming the 1 MB truncation when it applied. The reply is still on screen;
an attempt that changed nothing has no diff, so `y` copies the reply as usual.

The mechanism is OSC 52 — an escape sequence handed to your terminal — which needs no
clipboard library and no temp file. Its honest limit: the sequence carries no
acknowledgement, so council cannot know whether your terminal honoured it. Windows Terminal
accepts OSC 52 in current builds; over `ssh` or `tmux` it may be disabled by the terminal's
own configuration. One keystroke settles it on your machine — press `y`, then paste.

## The room remembers

Each column keeps its whole conversation, oldest turn first: a separator naming the turn,
the brief *you* typed as that seat received it, then what it answered, with the time and
cost that turn reported. Dispatching a new turn files the finished one rather than erasing
it, so `↑`/`↓` and `g` scroll back through the whole argument instead of through one reply
— and the overflow marker names those keys where the eye already is, next to the count of
what is hidden. The compose area grows with the draft — `ctrl+j` puts a newline in it,
`enter` still dispatches — up to six rows.

A seat that is not installed, or that is installed and cannot be driven, folds out of the
grid so the seats that answer get the width; one line under the header names what was
folded and why.

There is **one room**, and a bare `telltale council` reattaches to it by default. Each
vendor is holding a conversation several turns deep — that is what the resume mechanism
buys — and reopening the room is how you get back to it: the turn counter continues at
N+1 and each seat picks up its own thread; a seat whose first turn on a restored thread
fails lets that thread go and starts fresh, briefed. `--fresh` starts over, and a usable
saved room is named once before the first dispatch replaces it. The workspace is a
property of the room, not of the launch: `/cd <dir>` typed in the composer moves the room
— absolute, relative to the current workspace, or a sibling of it — and every seat
follows on the next dispatch. **Posture is never restored** — it comes from this launch's
flags or from the default, never from the saved file, because a posture that can arrive
from a file is not one anyone typed. And one room shared by every terminal means two
councils open at once share one state file — last save wins.

## Pasting into the composer

**A paste lands in the draft whole, and never sends.** Enter — a keystroke, from a person
— remains the only way a brief leaves the room: a paste never dispatches, never answers a
gate, never quits, and a pasted control character cannot act (a pasted ctrl+c is not a
cancel, a pasted `q` is the letter q). Newlines are kept exactly as pasted — the composer
has been a block since `ctrl+j` existed, so a pasted paragraph renders as the rows it is
and dispatches with its real newlines. The one lossy rewrite is stated rather than hidden:
a tab becomes one space, because a cell grid cannot budget a tab; a snippet whose
indentation is load-bearing belongs in a file the brief names.

**Pasting from view mode inserts and opens compose.** View mode's letters are commands
because they are keys; pasted text can only be material, and the only place material goes
is the draft — the mode line states the switch on the next frame. The exception is a
pending y/n: the paste is refused by name (`the paste was not inserted`) and the question
stays exactly where it was, because nothing about a pending request happens implicitly.

**A paste that would put the draft past 8,192 characters is refused whole**, not
truncated — a composer that kept the first half of a paste would send the vendors a brief
you never wrote. The notice carries both numbers and the remedy: `paste refused: 20481
chars against the composer's 8192 — put long text in a file and name the path in the
brief`. The cap is anchored to the narrowest pipe a brief must fit through (one seat's
prompt rides a Windows command line); typing can still pass it, as it always could.

One honest limit, carried from the record: all of this is measured against the runtime's
own pinned source, and it holds in any terminal that brackets its pastes — Windows
Terminal, the reference environment, does. **A terminal with no bracketed paste replays a
paste as keystrokes**, and there each pasted newline is an Enter and dispatches; council
cannot tell that replay from typing without guessing, so on such a terminal the fix is
the terminal ([design.md §9.38](design.md)).

## Write posture, and what actually contains the room

**A plain `telltale council` can write.** That is the default, and `--read` is the
opt-out — a room that only talks, where no seat may touch the workspace. It reads
backwards until you notice what guards it: the *gate* does, per call, not the flag. An
opt-in posture made sense while nothing could ask before a write, so "this room can
write" meant "this room writes without you". Once the gated seat started raising an
approval card, all the flag still did was make a room you opened to get work done unable
to do any until you remembered a word — the same reason the workspace stopped being an
invocation input.

**The containment is the workspace, not the flag** — so the room states its posture where
it cannot be missed: `⚠ WRITE` in the header for the whole session, and the same `WRITES`
badge on every column, uniform on purpose because grading them would imply a safety
difference that does not exist. A `--read` room says `READ` in the same place, because
absence of a badge is not a claim.

Only the seat driven as a live process can be asked. The other three are batch CLIs with
no channel a question could arrive on, so they act unasked — which is exactly why the
directory matters more than the posture, and why the throwaway worktree above is the
control that actually holds.

**`a` on the approval card stops the asking.** `y` approves, `n` denies, `a` approves this
call and every one after it — on the card rather than in the composer, because that is
where you decide you have been asked enough. It drains the queue behind the card rather
than discarding it, since a pending request is a vendor stopped mid-call. `a` alone in view
mode turns the asking back on, and while it is off the footer carries an `a not asking`
cell so the way back is never off screen.

## Controls you reach from inside the room

**`/seat <list>` changes who is in the room, and `/unseat <list>` names who leaves**, both
taking the same argument as `--vendor` and reading it through the same alias table
`@mentions` use. An unseated seat **keeps its thread and its process** — it just stops being
drawn and stops being dispatched to, so its width goes to the seats that are answering.
`/seat all` puts everyone back mid-conversation, with nothing to resume and nothing that can
fail. That is a different control from sitting out: a seat nobody addresses is already quiet
and already free.

**`--read` is the door; `/read` and `/write` are the room.** Typed into the composer,
`/read` makes the room read-only at once and `/write` asks `y`/`n` before letting it write
again — the confirmation is on the loosening direction only, because that one hands editing
and command authority to every seat. Both refuse while a turn is in flight, and neither
kills anything: seats move on their next turn. Only the bare word is a command, so
`/write a test for this` never changes the posture.

**`/retry` sends the last brief again, to the seats that owe an answer.** A turn where four
seats replied and one failed, was cut with `x`, or fell over leaves you retyping the brief and
the mentions to finish it. Type `/retry` instead: the brief comes back into the composer
addressed to exactly the seats that did not answer — `@codex @agy <brief>` — and **enter is
still what sends it**, so the footer prices the re-send before you pay for it and you can edit
the draft first. A seat that answered is never re-sent to, and neither is one that sat the turn
out, so the bill never grows past the turn you are finishing. A seat that finished and streamed
nothing counts as having answered: that is a measured zero, not a missing reply. It refuses
while a turn is in flight, when nothing has been dispatched yet, and when every seat answered.
The text goes out unchanged — `/retry` re-sends, it does not re-brief.

**A slash the room does not know is refused, not sent.** A draft that opens with `/` and
names no room command used to go to the vendors as a brief, so a mistyped verb cost every
seated seat a turn. Now nothing spawns, the draft stays in the composer, and the notice names
the word that failed and lists the room's commands. A brief that genuinely begins with a
slash — a path, a regex — is sent by typing **one space in front of it**, which the notice
also says.

**`ctrl+c` stops the seat you are looking at.** With several seats answering, the key
cancels the *focused* seat's turn and leaves its neighbours working; when the focused seat
is idle and something else is running it cancels everything in flight, as it always did;
when nothing is running it quits. The footer says which of the three is live on every
frame — `ctrl+c cancel codex`, `ctrl+c cancel all`, or plain `ctrl+c cancel` when only one
seat is answering. `x` is still the per-seat give-up with its `y`/`n` card, and it still
names what the cut costs that kind of seat. `q` refuses while any seat is busy and names
which: `a turn is in flight on codex (turn 4) — ctrl+c cancels a seat's turn first`. The
room-wide commands — `/cd`, `/seat`, `/unseat`, `/read`, `/write`, `/retry`, `/adopt`,
`/arena drop` — refuse the same way and name the same seats, because each of them changes
something a busy seat was dispatched against.

**`--fresh` is room-wide; `c` is one seat.** A seat whose context has filled up does not
need the other three restarted with it, so `c` in view mode clears the **focused** seat's
thread — `y` confirms, `n` keeps it, any other key cancels — and its next brief opens a new
session with the brief re-applied. The turns already on screen stay: what is cleared is the
thread the next brief would have continued, not the record of what was said. It refuses
while *that* seat is on a turn — `a turn is in flight on Claude Code (turn 4) — c clears a
seat between its turns` — and a busy neighbour does not stop it, because the thread being
dropped is this seat's alone; `u` follows the same per-seat rule. The rule it is the first control
built to — anything that changes while the room is open is reachable from inside it, and a
flag is for what is true at launch — is [design.md §9.17](design.md).

**`/trace <file>` records turn timings, including the ones already behind you.** The turn
clock runs on every turn whether or not anything is writing it down, so the room holds the
last 200 records and hands them to the file the moment you open one — type it straight after
a turn you cannot explain and you get *that* turn, not the next one. `/trace off` stops and
the room keeps measuring; bare `/trace` reports. Council never picks the path: that is what
keeps `room.json` the only file it writes on its own initiative.

## The race: /arena

**`/arena <brief>` races one brief across every seated vendor, each attempt in its own git
worktree, compared by diff instead of by prose.** Five writers in one shared tree are not
five answers; the worktree is what makes five *write* attempts comparable at all. Every
attempt is a **fresh session** — the room's own conversations are untouched, and a race's
throwaway session ids can never replace the threads the room reattaches to. Even the seat
driven as a live process races on a throwaway session of its own, killed at its own finish
line, its room thread untouched by construction. A race is not routable — `@codex`-only
arenas deliberately do not exist, because the value is the comparison, and a one-seat race
is an ordinary turn in a worktree, which `/cd` already provides.

Every racer writes — that is the point — so a `--read` room refuses the race and names the
way in: `/write lets it, between turns`. The containment is the worktree, stated on the
column rather than implied: a one-shot process has no channel to be asked on, so the gate
structurally cannot exist here.

**The worktrees are kept until you delete them, and they are siblings you can see.** Each
one is named `<repo>-arena-t<N>-<vendor>` beside the workspace, on branch
`arena/t<N>/<vendor>` — so `/cd telltale-arena-t7-codex` walks into one by name, and a room
reopened after a quit can finish the lifecycle by hand with plain git. Every diff answers
against **one base commit recorded before any seat spawned**, and files a racer *created*
count too — an attempt whose whole answer is a new file can never read as "no changes". A
workspace that is not a git repo refuses the whole race with git's own sentence; one seat
whose worktree could not be added is skipped and told why, and the others race on. One
thing to know about the block itself: it belongs to the seat's *current* turn, so the next
brief you dispatch to that seat clears it from the screen. The worktree, the branch and
the commit are the record that outlives it — which is exactly what the two lifecycle verbs
below act on.

**Cutting those worktrees takes a moment, and the room stays a room while it does.** The
footer names the step it is on — `arena: preparing worktree for codex…` — with a spinner
beside it and nothing else: no percentage and no count, because how long a checkout takes is
not something council can measure. The seats are prepared one at a time, on purpose. Keys
keep working throughout, and **ctrl+c stops the setup** rather than the room — which is what
you want when another session is holding the repository's lock. The whole setup is bounded at
90 seconds; a deadline or a git refusal names the step it stopped on, quotes git, and puts
your brief back in the composer so the same enter tries again. Any worktree already added is
kept, and `git worktree remove` clears one.

**While the race runs, each racing column shows the diff growing.** Stream activity on a
seat arms a re-read of its worktree (at most one every two seconds), and the block is
labelled **`arena · so far`** — the "so far" is the honesty marker, because a mid-race read
is a measurement of a moment already past. It keeps zero, absent and broken apart: no read
yet draws nothing at all; a read that found nothing says `no changes yet against <base>.`;
a failed read carries git's own first line, and after three consecutive failures the block
says it stopped re-reading — the finish-time diff still runs either way.

**Every finisher carries a finish line: `2nd of 4 · done · 25s`.** The rank is the order
the *room* saw seats land — a vendor's own claim about when it finished is never consulted
— and it is welded to the phase word on purpose: `2nd · failed` and `2nd · done` are
different facts, and a bare number would let a fast crash read as a podium. The elapsed is
the column's own clock. Below it, the settled block replaces the interim one outright: a
diff stat, a measured `no changes against <base>.`, or `diff unavailable: <why>` — three
outcomes, never collapsed.

**Every attempt that changed something survives as a commit.** The moment a racer lands,
its whole tree is committed onto its arena branch and the column shows the receipt —
`committed <sha>.`, exactly what git reported — so the attempt stays diffable, adoptable
and rollbackable even after its worktree is deleted. An attempt that changed nothing
commits nothing and shows no commit line either: `no changes against <base>.` is the whole
story, and an empty commit would be a receipt claiming work that did not happen. A commit
that could not land degrades that one seat's receipt — `not committed: <git's reason>` —
and touches nothing else.

**`d` flips the focused seat's arena block from the stat to the full patch**, and back —
per column, because reading one seat's stat against another's whole diff is a legitimate
way to compare. The frame shows at most 400 patch lines, and the cutoff names what it
dropped and both routes to the rest: `y` copies the whole diff (capped at 1 MB, truncation
stated), and the worktree holds all of it. `d` refuses with the reason named when there is
nothing to flip to — no race this turn, an attempt that changed nothing, or a diff that
could not be read.

**Opening the patch puts a cursor `▸` on one hunk, and `[` `]` step it.** Those are the
same two keys that step a turn in the grid and a page in the by-turn view: in an open
patch the unit is a hunk, and the footer's cell says `[ ] hunk` so the key and the line
that names it never disagree. The cursor stays **inside the drawn 400 lines** — it does
not scroll the frame — and both ends refuse by name rather than wrapping.

**`D` quotes the hunk under the cursor into the composer draft.** It is a review comment
without a round trip the room could not make honest: a race attempt is one-shot, so there
is no session for a reply to resume, and the quote therefore lands in your **live draft**
— visible, editable, and sent by the same `enter` as any other brief. Nothing is queued and
nothing is auto-sent. The whole hunk crosses even when the 400-line frame cut it off, fenced
as data with the branch, the base and the **worktree path** named, so the seat reading it
knows the code is not in the room's own workspace. An empty draft is seeded with that
racer's `@mention` — silence routes to claude, and a comment about codex's attempt should
not reach claude — and a draft you have already started is left exactly as you typed it. A
hunk too big for the composer's 8,192-character cap is refused whole rather than truncated;
`y` still copies the diff.

**`o` hands you the worktree: `y` opens it in your editor, `c` copies the path.** The card
names the program before it starts one — `$VISUAL` first, then `$EDITOR`, and **nothing is
guessed** if neither is set, which the card says while still offering the copy. `c` puts the
full absolute path on the clipboard through the same mechanism `y` uses. The room keeps the
screen, so a terminal editor opens where you cannot see it; the notice says so rather than
letting you find out.

Both `D` and `o` address a **column**, so both refuse while a turn page, an act ledger or an
arena record is filling the frame — those have no hunk and no worktree, and the cursor is not
on screen to point at. `t` gives the columns back.

**`u` takes the focused seat's attempt back — y/n-gated, like `c`.** A stray keystroke
must cost a `y` before it costs an attempt: the card names exactly what happens (`y resets
its worktree and branch to <base>`), and the reset runs inside that racer's worktree only,
behind a mechanical path check that refuses anything that is not a tree this room made
this turn. After an undo the stat stays on the column — the measured record of what the
attempt changed — under a line saying the tree and branch no longer hold it. `u` refuses
while a turn is in flight, when no race is on the seat's current turn, when the attempt
changed nothing, and when it is already undone — pressing again is not more undone.

**A `.worktreeinclude` at the repo root seeds each racer with the files git ignores.**
A fresh worktree is a clean checkout, which is also the trap: the `.env` a project needs
to run is exactly what git does not carry. Name such files in `.worktreeinclude` —
gitignore-style patterns, one per line — and every racer's tree gets a copy before it
races; the column says `seeded 3 files`, a count of what actually landed in *that* tree.
Only untracked files are copied (a tracked file already arrives with the checkout, and
seeding the room's possibly-dirty copy would plant your own edits in every seat's diff),
symlinks are never followed, and the total is capped at 64 MiB per seat so one over-broad
pattern fails loud instead of copying a dependency tree four times. A pattern that matches
nothing is named on the column rather than silently skipped; a copy that fails skips that
seat with the reason, and its half-seeded worktree stays on disk with the other receipts.
Copy only, never execute: the repo cannot run code on your machine by containing a file.

**Every racer's tree also gets the brief as an `AGENTS.md`.** The prompt is unchanged — this
is a second copy of the same words at the path the vendors already look at, so a seat that
re-reads its instructions mid-turn finds them. The file is identical in every tree (marker,
the conduct line, then your brief), it never reaches the attempt's stat, its patch or its
commit, and `/adopt` therefore merges a branch that never held it. `/arena drop` takes the file
back before it removes the tree, so a drop needs no `!` over it. A repo that ships its own
root `AGENTS.md` keeps it: council writes nothing there, on any seat. **The room never says a
seat was briefed this way**, because only some vendors were measured reading the file and the
room cannot tell per race which did — the file is offered, exactly like the worktree is.

**`/adopt <seat>` takes the winner — onto a fresh branch, behind a y/n card that names the
exact commands.** `y` cuts `adopt/t<N>-<vendor>` from where the room is standing, checks it
out, and runs `git merge --no-ff arena/t<N>/<seat>` there — so the branch you were on does
not move at all, and the hand-off is one `gh pr create`, which the notice names. The name is
taken as spelled unless something already holds it, in which case the next free suffix
(`-2`, `-3`, …) is used and the card says so before you press `y`. When the attempt's
worktree still holds uncommitted work, the card says the commit comes first, and names every
command. Adopt refuses while a turn is in flight, refuses a racer that changed
nothing (an empty merge commit would claim work that does not exist), and refuses — with
the path count — while the room's own tree is dirty, because a merge must never eat your
uncommitted work. A merge that conflicts is aborted, your tree restored, the attempt
intact on its branch, and the notice hands the merge to a human; one that failed before
starting says the tree is untouched instead — two different endings, kept apart. Either way
the branch cut for the merge is deleted and the room goes back where it was, so a failed
adoption leaves nothing behind.

**`/adopt <seat> +<seat> <path...>` takes the winner plus the parts of the runner-up you
point at.** `/adopt claude +codex internal/helper.go` merges claude's attempt whole and then
takes exactly that one file from codex's, on a branch named for both of them —
`adopt/t<N>-claude+codex`. The card names both sources and every path before you press `y`,
and the commit it writes names them again, so nothing reading the history later has to guess
where a file came from. The paths are yours to name: council picks nothing. It refuses, by
name and before the card arms, a path **both racers wrote** (taking one would discard the
other's answer with no merge and no marker — drop it, or merge it by hand afterwards), a
path **the room itself wrote** since the race was cut, a path **the runner-up never touched**,
and a path **it deleted** — a hybrid takes files a racer wrote, never a deletion. A hybrid
whose merge conflicts restores your tree and deletes its branch exactly as the plain form
does. Only one runner-up per adopt, and the seat must be a different one.

**`/arena drop <seat>` deletes a racer's worktree and branch, and the force is a spelling,
not a keystroke.** Drop refuses while a worktree holds uncommitted changes (counted), and
while the arena branch holds commits the room has not merged (counted, with
`/adopt <seat>` offered beside the alternative). The force form is a trailing bang —
`/arena drop codex!` — retyped by you, chosen over a second y/n on purpose: the bang
travels in the command and records that destruction was asked for, and a stray key can
never produce it. `/arena drop all` degrades per seat — clean trees go, survivors are
named with their reasons. Only the exact two-word form is the verb; anything longer after
`/arena` is a brief and races as prose. Both lifecycle verbs run on your own keystroke,
not a vendor's, so they work in a `--read` room too — and both answer with usage when
typed bare.

**`/arena record` says which seat you actually take.** One line per seat, read from the
`arena/` and `adopt/` branches this repository still holds — nothing is stored, and no
file is written. A seat that has never raced says `never raced`; a seat you decided
against says `0 of 4 adopted  0%`, because a measured zero and an absence are different
facts. The rate never appears without the two counts it divides, and races **nobody was
adopted from** are reported beside it as undecided rather than inside it — a race you
walked away from is not a verdict on anyone. A race you settled with a **hybrid** adopt is
its own state and credits nobody: the race counts as decided, both seats count as having
entered it, and neither rate moves — the page says `part of 2 hybrid adopts` beside the rate,
and the line under the heading says how many decided races no seat's column claims. The refs
can say which two seats a hybrid was cut from; they cannot say which paths came from where,
because that lives in a commit message this page does not read. The word is `adopted` and never `won`, and
that is deliberate: a seat you cut with `x` still counts as one you did not adopt, because
the branches record who entered and whom you took and nothing else. The page states its own
bound — a race whose branches were dropped is no longer in the record — and `y` copies the
whole table with that sentence attached. `t` gives the grid back. There is no rank and no
phase word in it, because those live on the turn and the turn is gone.

**`/arena check <command>` says whether each attempt WORKS.** Name one command — `/arena
check go test ./...` — and every racer runs it in its own worktree when that seat lands.
The block then says `check PASS` or `check FAIL · exit 2`, from the command's real exit
code and from nothing else: there is no judge, no opinion, and no verdict inferred from the
diff. A command that could not run at all — a missing program, a run the ten-minute bound
stopped — says `check unavailable: <why>` instead, because that is not a failing attempt,
it is an unmeasured one. Name nothing and no race mentions a check at all. The command is
resolved when you name it, so a draft whose first word is not a program is refused and
handed back rather than raced — which is what keeps a brief opening with the word "check"
from becoming a command. `/arena check` alone says what is named; `/arena check off` stops
it. The check runs AFTER the diff is read and the attempt is committed, so nothing it
writes is in either — and if it leaves the tree dirty, the column says so, because
`/adopt` would otherwise carry that build output into your merge. It never gates anything:
it reports, and you adopt.

The record behind all of this — the rulings, which live race verified what, and what is
still owed — is [design.md §9.37](design.md). Read it before trusting the edges, because
they are not all at the same standard. **Live-verified** as of race t9 (2026-08-09): the
core race and its ranks, the honest zeros, every attempt surviving as a commit,
`/arena drop`, `x` on a stuck racer, and the conduct line's one observed compliance.
**Live-verified** as of races t13/t14 (2026-08-14): `u` — the branch really reset to its
base, confirmed against git rather than the room's own claim, and a second press refused
as already undone; `.worktreeinclude` seeding — `seeded 1 file` plus the named no-match
notice, the seed line's zero-vs-absent rule on a live column; and the fresh-branch adopt
in the shape ruled 2026-08-11 — `adopt/t14-claude` cut behind the card, a `--no-ff` merge
landed there, and the room's own branch never moved. **Live-verified** as of the
5-of-5 race (2026-08-15/16): all five vendors racing one brief at once — three clean
finishes with ranked receipts and two seats given up mid-stall, both keeping their
commits — and the `arena · so far` stat watched growing from a non-empty read on two
columns before the settled block replaced it. **Offline only**: the arena record
([design.md §9.47](design.md)) — its tally, its three renders and its frame are pinned by
tests, and no live open against a real pile of leftover branches is recorded yet — and the
arena check ([design.md §9.48](design.md)), whose exit-code reading is pinned against a real
process but never yet against a real race.
**Live in part**: the cursor seat's
throwaway racer has been spawned, streamed and killed live — twice now — but never
watched finish on its own; that half still stands on its offline tests, and
[design.md §9.37](design.md) says so beside its payment blocks.

## Record and replay

The room cannot be seen without five paid CLI logins, and a frame worth showing is a frame
somebody spent quota on. `telltale council --record <file>` keeps a real run: every event the
room applied (each seat's output, tool lines, session ids, costs, gate cards), each dispatch,
and each answer you gave a card, with the clock each one landed on. `telltale council --replay
<file>` opens a room fed from that file instead of from vendors, at the original pace
(`--replay-speed N` multiplies it), and draws it with the same renderer: the columns stream,
the card goes up and comes down when you answered it, the elapsed figures count the seconds
the vendor actually took.

**A replay is labelled on every frame.** `⚠ REPLAY` sits in the header where `WRITE` or `READ`
sits, `REPLAY` leads every column's badge row ahead of the recorded posture, and the compose
footer reads `⚠ REPLAY nothing here is live` where the routing cell and `enter dispatch` would
be. Enter says `this room is a replay; nothing here is live`; so do the card's `y`, `n` and
`a`, and the per-seat verbs. `ctrl+c` and `q` leave. Reading is untouched: scroll, focus, the
digits, the by-turn page, the ledger, the help panel. A replay starts no vendor, dispatches
nothing, reads no quota relay, and neither reads nor writes the saved room; its seats, their
postures and its workspace all come from the file, so it draws the room that was recorded on
a machine with nothing installed.

**The recording is yours, at a path you name.** Everything else council writes is numbers and
keys; this file carries the conversation verbatim, unredacted (a redacting recorder would be
a second truth; the replay runs the same redactor over the same bytes, so the screen matches),
and so it is never anything under `~/.telltale`: a `--record` path there is refused, an
existing file is refused rather than overwritten or extended, and no key inside the room
starts one. Before a capture goes anywhere, `telltale council replay-check <file>` prints what
a review needs to see: the workspace, the seats, every session id, every tool line and gate
card (each may name a path), how much prose is in it, and a reminder that it did not read the
prose. That is the README's frame review, given a tool. What a recording does not hold: your
cancels and give-ups (a column you cancelled live replays as the vendor's own exit), focus
and scrolling, and the `--brief` file's text.

## Flags

`telltale council` flags: `--fresh` (start over instead of reattaching), `--cd <dir>`
(launch-time override of the room's workspace — the daily path never needs it),
`--vendor <list>`, `--brief <file>`, `--read`, `--auto`, `--trace <file>`, `--resume`
(accepted, and redundant — reattaching is the default), `--write` (accepted, and does
nothing — writing is the default), `--headroom-warn N` (the routing cell's threshold, default
90), `--record <file>` and `--replay <file>` with `--replay-speed N` (above), `--ascii`,
`--no-title`. `telltale council replay-check <file>` reviews a recording without opening it.

`--vendor <list>` decides who is in the room: `all` keeps every detected seat on screen
including the ones that cannot be driven, and a comma list (`--vendor claude,codex`) seats
exactly those and dispatches to nobody else. That is a different control from an
`@mention`, which routes a single turn.

`--brief <file>` (or `TELLTALE_COUNCIL_BRIEF`) hands one file of shared operating context
to every vendor on its first turn. Without it, the room's default state is five vendors
guessing separately at a convention you already wrote down. The flag takes a **path**,
never content: telltale is public and a briefing is not, so no default location inside a
repo is searched, and the file is never logged, never rendered and never stored by
telltale. The header says `briefed` or `no brief` on every frame.

---

The rest of the account — why execution is argv and never a shell, the silent invocation
trap each vendor hid, and what is still unverified — is in [design.md §9](design.md).
