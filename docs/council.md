# telltale council — the dispatch room

`telltale council` is one brief, typed once and answered by the 4-vendor fleet side by
side — **Claude Code**, **Codex**, **Antigravity**, and **Cursor**, each in its own column,
in your terminal. It exists because the alternative is four terminals and a clipboard.

This file is the room's own guide: what each mark on the frame means, how a turn is routed,
how to read four answers at once, and how to get one back out. [README.md](../README.md) is
the front door and states the project's claims; [design.md §9](design.md) is the record
behind this one — what was measured on each vendor, what each seam cost, and what is still
unverified.

```
telltale.exe council
```

```
  council READ  │  ~/code/telltale                                                  turn 1  │  3/3 seated  │  no brief  
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
▌ ▸ 1 CC Claude Code  ────────  ✓ done  │    2 CX Codex  ─────────────  ○ idle  │    3 AG Antigravity  ───────  ○ idle  
▌   ro:tools  tokens                    │    ro:requested  final only           │    unsandboxed  final only            
▌ ⚙ Glob                                │                                       │                                       
▌ ⚙ Read                                │                                       │                                       
▌ ⚙ Bash: go test ./...                 │                                       │                                       
▌                                       │                                       │                                       
▌ Tests pass.                           │  no turn dispatched yet.              │  no turn dispatched yet.              
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  ›                                                                                                                     
  VIEW              ↑↓ scroll  │  [ ] turn  │  f expand  │  tab focus  │  1-3 seat  │  i compose  │  ? help  │  q quit  
```

That frame is the council test suite's `activity` golden
(`internal/council/testdata/golden/activity.txt`) with its empty rows dropped, and nothing
else changed — including the `READ` in its header, because the fixture opens a `--read`
room. A plain `telltale council` says `⚠ WRITE` there instead.

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
exception because "which of four agents is speaking" is a distinction the statusline and
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

  **On Windows, Codex wears it too**, and the frame above is a rendering fixture rather than
  a capture from any one machine. `-s read-only` and `-s workspace-write` were both measured
  failing *every* process spawn there — including one asked merely to list a directory — so
  council passes `-s danger-full-access`, the only mode that runs, and says so. A read-only
  badge on that seat would be the one false claim in this room that someone would actually
  rely on. On macOS and Linux the same seat is `ro:enforced` by the OS sandbox.
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
| `ro:enforced` | The vendor's own **OS-level sandbox** is applying it — `codex -s read-only` on macOS and Linux. The one posture here that an operating system rather than a flag is behind. |
| `ro:requested` | A read-only flag was passed and accepted, and **what it actually enforces was never observed**. Weaker than the two above, and it says so rather than borrowing their word. |
| `unsandboxed` | **Nothing restricts this vendor at the OS level** — measured, not assumed. Treat the column as able to change your files. It deliberately does not open with `ro:`, because a reader scanning four headers takes in the prefix before the qualifier. |
| `WRITES` | The room can write — the default. This column may edit and run things in the workspace. |
| `gated` | The room can write, and this seat **asks first**: `y` approves, `n` denies, and nothing runs until you answer. Only the seat driven as a live process can be asked; the others say `WRITES` rather than implying they can. |

Two of those need the same answer to the obvious follow-up — *must they stay that way?*

**No badge is what keeps this room out of your files. The workspace is.** `unsandboxed` is
not a setting anyone chose to leave on: on Windows both of Codex's sandboxed modes were
measured failing *every* process spawn, reads included, so `-s read-only` was not a
restriction but a seat that could not read its own repo. Antigravity was asked to write a
file under both of its own read-only flags and wrote it. Those are measurements, and the
badge reports them. The control that actually holds is the directory council was pointed
at — so if a room should not be able to touch something, point it somewhere else:

```
git worktree add ../telltale-council
telltale council --cd ../telltale-council
```

That is also the fleet's own ruling rather than a local convenience: `agent-ops` ADR-012
rules capability parity — every vendor reads and writes, and guard wiring rather than lane
shape is the control. A column that looked read-only because of a broken sandbox was never
a safety property; it was a defect wearing one's clothes.

The `⚙` lines are the activity trace: what a vendor is *doing* — the tool call, and the
command it ran — interleaved with what it says.

## Routing: who a turn actually reaches

The room is an operating committee, but routing defaults to the **control plane**, not a
broadcast. An unaddressed brief goes to **Claude alone**; `@codex`, `@agy` and `@cursor`
(and `@claude`) name seats for one turn; **`@all`** (also `@everyone` / `@council`)
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
one question above four answers instead of the same paragraph across the top of every
reading area. It spends at most four rows, and when the brief is longer the fourth row
says how many rows are missing and where the whole of it is, because a reader must never
have to wonder whether they are looking at their own question or a truncated copy of it.
Nothing new is stored: this is a rendering rule over the echo each column already held,
history keeps its per-column copies, and if spending the band would leave the columns
fewer than eight rows of body it is not spent at all.

Turn 1 is blind. Later turns ride each vendor's own native session resume rather than
re-sending the transcript, which keeps that guarantee structural: each session holds only
its own history. `ctrl+r` arms a rebuttal turn — off by default — in which each vendor
sees the others' last answers, fenced and labelled as untrusted material.

## Reading four answers at once

**Reading is a first-class mode.** `↑`/`↓` scroll the focused column, `pgup`/`pgdn` move by
a screenful, `g` and `G` reach the first turn or the newest, `tab` moves between columns,
`1`–`N` go straight to the Nth seat on screen, and `f` expands one column to the full width
— which is what you want when an answer is long rather than when four of them are being
compared. `[` and `]` walk the focused column **a turn at a time**, landing the turn's
separator on the top row where the brief and the answer to it read together. The scrollback
had only ever moved in lines, so `↑ 509 more above` was measured, correct, and counted in a
unit nobody reads a conversation in — while *how far back is what I asked* was the one
question a reader actually had, and the only two answers were both ends. All of that works
**while composing** as well as in view mode, which is the point: a finished turn drops the
room back into compose, so the mode you are in when four long answers land is the mode you
need to read in. Keys that can be text stay text there — `j`, `k`, `g`, `G`, `q`, `t`, `c`,
the digits and the brackets are all just characters in the composer — and the mode line
says which set is live on every frame. `?` lists all of them and says when it is holding
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
and a projection whose whole unit is the turn deliberately has none.

The mechanism is OSC 52 — an escape sequence handed to your terminal — which needs no
clipboard library and no temp file. Its honest limit: the sequence carries no
acknowledgement, so council cannot know whether your terminal honoured it. Windows Terminal
accepts OSC 52 in current builds; over `ssh` or `tmux` it may be disabled by the terminal's
own configuration. One keystroke settles it on your machine — press `y`, then paste.

## The room remembers

Each column keeps its whole conversation, oldest turn first: a
separator naming the turn, the brief *you* typed as that seat received it, then what it
answered, with the time and cost that turn reported. Dispatching a new turn files the
finished one rather than erasing it, so `↑`/`↓` and `g` scroll back through the whole
argument instead of through one reply — and the overflow marker names those keys where
the eye already is, next to the count of what is hidden. The compose area grows with the draft — `ctrl+j` puts a
newline in it, `enter` still dispatches — up to six rows.

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
from a file is not one anyone typed. And one
room shared by every terminal means two councils open at once share one state file —
last save wins.

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

**A slash the room does not know is refused, not sent.** A draft that opens with `/` and
names no room command used to go to the vendors as a brief, so a mistyped verb cost every
seated seat a turn. Now nothing spawns, the draft stays in the composer, and the notice names
the word that failed and lists the room's commands. A brief that genuinely begins with a
slash — a path, a regex — is sent by typing **one space in front of it**, which the notice
also says.

**`--fresh` is room-wide; `c` is one seat.** A seat whose context has filled up does not
need the other three restarted with it, so `c` in view mode clears the **focused** seat's
thread — `y` confirms, `n` keeps it, any other key cancels — and its next brief opens a new
session with the brief re-applied. The turns already on screen stay: what is cleared is the
thread the next brief would have continued, not the record of what was said. It refuses
while a turn is in flight, for the same reason `/cd` does. The rule it is the first control
built to — anything that changes while the room is open is reachable from inside it, and a
flag is for what is true at launch — is [design.md §9.17](design.md).

**`/trace <file>` records turn timings, including the ones already behind you.** The turn
clock runs on every turn whether or not anything is writing it down, so the room holds the
last 200 records and hands them to the file the moment you open one — type it straight after
a turn you cannot explain and you get *that* turn, not the next one. `/trace off` stops and
the room keeps measuring; bare `/trace` reports. Council never picks the path: that is what
keeps `room.json` the only file it writes on its own initiative.

## Flags

`telltale council` flags: `--fresh` (start over instead of reattaching), `--cd <dir>`
(launch-time override of the room's workspace — the daily path never needs it),
`--vendor <list>`, `--brief <file>`, `--read`, `--auto`, `--trace <file>`, `--resume`
(accepted, and redundant — reattaching is the default), `--write` (accepted, and does
nothing — writing is the default), `--ascii`, `--no-title`.

`--vendor <list>` decides who is in the room: `all` keeps every detected seat on screen
including the ones that cannot be driven, and a comma list (`--vendor claude,codex`) seats
exactly those and dispatches to nobody else. That is a different control from an
`@mention`, which routes a single turn.

`--brief <file>` (or `TELLTALE_COUNCIL_BRIEF`) hands one file of shared operating context
to every vendor on its first turn. Without it, the room's default state is four vendors
guessing separately at a convention you already wrote down. The flag takes a **path**,
never content: telltale is public and a briefing is not, so no default location inside a
repo is searched, and the file is never logged, never rendered and never stored by
telltale. The header says `briefed` or `no brief` on every frame.

---

The rest of the account — why execution is argv and never a shell, the silent invocation
trap each vendor hid, and what is still unverified — is in [design.md §9](design.md).
