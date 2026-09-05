# The room's visual identity

Decided 2026-09-03. One page, because `LEDGER.md` carries the ruling in one line
and `docs/design.md` takes no new sections through 2026-09-30.

> **Telltale spends colour on evidence, not personalities: the figures are
> readings, the posture rail says what each seat may do, and an empty gauge means
> we did not measure it.**

That is the sentence this identity exists to make true on a stage. Everything
below is the argument for it and the list of what was grafted from where.

## How it was decided

Three identities were built as full branches, each rendered to a labelled contact
sheet, and audited by a second model against three questions: does it read as a
measuring instrument, is it unmistakably this product, and would it survive a
projector at the back of a room. The prototypes were `explore/room-monograph`,
`explore/room-instrument` and `explore/room-broadcast`. MONOGRAPH won; the other
two were harvested for the surfaces they won on. The audit is
`desk/research/telltale-room-identity-audit-2026-09-03.md` (private).

## The identity, in three sentences

The room draws in one ink at six values and spends colour three times, so the
hierarchy lives in how much ink is on the page rather than in how many hues are on
it. A value telltale actually MEASURED — a cost, a clock, a quota reading, a
diffstat, an exit code's PASS — is the brightest thing on screen, and the several
hundred lines a vendor wrote sit below it in the terminal's own ink. Ink blue says
who and what has focus, copper says withdrawn, oxide says broken, and nothing else
gets a colour at all.

The pigments are the owner's own, from the portfolio's MONOGRAPH landing, where
the taste is already written down and already measured for contrast. The room
needs one value the site does not have — the site has no failure state and an
instrument does — so oxide and copper are the same pigment at two strengths.
`internal/council/style.go` carries the palette and the whole argument.

## The three surfaces

**Columns for answers.** Five seats side by side. The focus rail takes the focus
ink; a column the keys do not move recedes; and a column header's CLOCK renders in
the measured ink whatever the phase word beside it wears — so a reader scanning
five columns for "which of these is slow" compares five numbers instead of reading
five words.

**A board for the race.** `/arena` is drawn as a race: a fixed eight-cell lane
filled to the racer's host-observed rank, an EMPTY track for a racer the host has
not ranked, a leading verdict mark in front of the PASS / FAIL / unavailable
sentence, and `/adopt` printed on the block's own rule beside the branch it would
merge. `internal/council/arenalane.go`.

**A ledger for posture.** The differentiator, and now the frame's governing
object. The badge row is one continuous rail that runs the whole frame, so an
absent reading is a gap in a printed line rather than a blank that could be a row
that failed to draw. The badges are a ladder ordered by EVIDENCE rather than by
risk: `ro:enforced`, `ro:tools`, `ro:requested`, `gated`, `WRITES` /
`unsandboxed`, each rendering distinctly, so a containment this repo measured
cannot look like one it merely asked for.

## The graft list, and where each item landed

Base: MONOGRAPH, whole.

| grafted | from | landed in |
|---|---|---|
| fixed eight-cell race track, with an EMPTY track for "no rank yet" | BROADCAST | `arenalane.go` `laneLines` / `runningLaneLines`, `glyphs.go` `Fill` |
| leading verdict marks, explicit PASS / FAIL / unavailable wording | BROADCAST | `arenalane.go` `verdictLines` / `verdictMark` |
| the `/adopt` affordance on each arena rule | BROADCAST | `arenalane.go` `arenaRule` |
| posture badges ordered and rendered by EVIDENCE | BROADCAST | `style.go` `ForSandbox`, `view.go` `helpBadgeGloss` |
| the continuous posture rail, with a visible gap for an absent reading | INSTRUMENT | `style.go` `RailGround` / `onBand` / `bandFill` / `onRail`, `layout.go` `fitOn`, `view.go` `ledgerRow` |
| the recorder-strip hierarchy for the tool trace | INSTRUMENT | `view.go` `recorderHead` |

Repainted, not copied: the lane wears the measured ink rather than a seat's hue,
the verdicts carry no coloured background, and the rail's ground is the bottom of
this palette's own scale rather than the prototype's warm band.

Deliberately NOT grafted: BROADCAST's seat chips and per-seat rails, and
INSTRUMENT's hard-coded hex semantics. See the two rulings below.

## The two rulings

**Seat hues stay retired.** §9.28 spent one 4-bit hue per seat and its own note
admitted the legal set was full at five, with two pairs already twins some schemes
render close. The room answers "which seat" four times before colour is reached —
column position, the seat number that is also its key, the two-letter tag, and the
spelled-out name — and the fifth answer in hue is the one that turns a monograph
into a sticker sheet. Seat identity is one ink and what separates the focused seat
is weight. A sixth vendor needs no hue decision at all.
`internal/council/seathues_test.go` is that law.

**Truecolour may enhance; it may never define.** An identity may spend a hex
triple. No distinction may DEPEND on one. So the hierarchy is carried by value,
by weight, and by words that survive `--ascii`, `NO_COLOR` and a 16-colour
console, and the one ground this room paints draws nothing at all when it is
empty. `TestPlainStylesPaintsNoRail` holds that at the type level rather than by
inspection, and the 118 layout goldens are the standing proof: they render
`PlainStyles`, where the whole identity is the identity function.

## The accessibility floor, which is not taste

`--ascii` and `NO_COLOR` must still render a usable room, with every distinction
carried by a word or a mark. `LEDGER.md` says outright that this was not lifted
with the three taste rulings. Two mechanical consequences:

- every treatment this identity adds is a colour, a weight or a ground, so it is
  invisible to a golden by construction;
- the projector floor is asserted rather than eyeballed. Every ink clears WCAG AA
  against the ground the room is drawn on, every rule clears the 3:1 floor a
  non-text component needs, and every ink printed on the posture rail clears AA
  against the RAIL. `TestTheInkScaleClearsTheProjectorFloor` and
  `TestTheRailIsLegibleOnBothGrounds`.

## The font facts

**Cascadia Mono**, which ships with Windows 10 and 11 and is Windows Terminal's
own face. **No Nerd Font is required and none is used.** Every glyph is standard
Unicode from blocks the product already depends on: Box Drawing, Block Elements,
Braille Patterns, Dingbats, Geometric Shapes and Miscellaneous Symbols.

One honest finding, measured while rendering the sheets: Cascadia Mono does not
itself carry `U+2699` GEAR (the trace mark), `U+26A0` WARNING SIGN or `U+2717`
BALLOT X (the failure mark). Windows Terminal falls back to Segoe UI Symbol for
all three. That is pre-existing and this identity did not introduce it, but it
means the room's three loudest marks are drawn by a fallback face on the primary
target. If it matters at demo width, the fix is a font choice and not a glyph
choice.

A second measured finding, for whoever next renders a picture of this room:
Charm's `freeze` ignores `--font.family` and `--font.file` on this machine and
renders with its bundled JetBrains Mono, which carries neither the gear nor the
braille spinner — so a `freeze` picture of this room shows tofu where two marks
should be. The contact sheets are rendered through `internal/svgframe` instead.

## What is asserted, and what is only claimed

Asserted by a test: the ink floors, the rail's legibility, the rail's invisibility
to `PlainStyles`, the empty-track law, the distinctness of the verdict marks and
of the five posture renders, `/adopt` shedding rather than clipping, the singular
gate card, and the evidence order of the legend.

Not asserted by anything, and said here so nobody reads "done" as "verified": how
these inks resolve on a terminal that downsamples truecolour to 256 or 16 colours.
Lipgloss degrades a hex to the writer's profile; nobody has measured what a
16-colour console produces from this palette. On such a terminal the ink scale
collapses toward the words and marks that were already carrying every distinction,
which is the reason the accessibility floor is a floor.

## Density and repetition: what a column owes the reader

The identity of 2026-09-03 was designed against synthesized fixtures at 180x50.
It says nothing about DENSITY or about REPETITION. A pass on 2026-09-03 answered
both, against a real recorded room at the owner's own desk geometry. The identity
does not move: seat hues stay retired, truecolour may enhance and never define,
and no ink, weight or ground changed.

**Two classes of finding, and they need different cures.** The first class is
REPETITION: a fact about the room printed once per column. The quoted-material
sentence, the live skip, the rebuild notice and the unseated seat were four
sentences the grid said two, three or four times each. The second class is
DENSITY: a column that has twenty cells cannot carry a transcript. A Windows path
wrapped to ten rows there, and the trace this page calls the flight recorder was
the least readable thing on screen exactly where the reader had least room. A
row given back to a starved column does not fix the second class, and a shorter
path does not fix the first.

**Three lanes argued it, and an independent audit chose.** LEDGER held that a
room fact prints once on the room's own line. STRIP held that a narrow column is
a strip and not a shrunken column. CLOCK held that the frame between two briefs
must read as time passing. The audit ranked CLOCK strongest as a base, STRIP
strongest on density, and LEDGER strongest on repetition, and it ruled the graft.

**What landed, and where.** CLOCK is the base, whole: the cue row on the chrome
row the column already reserved, the quiet clock measured from the seat's last
word or act, the act count, the overflow cue off the first content row, and the
turn coordinate in every column. From LEDGER: `roomline.go`, the room line above
the grid, which takes the unseated seat, the seats that sat the LIVE turn out
(named, never counted), and a room fact too long for the footer; and the rebuild
note off the columns, so the column says one word and the room states the
sentence and the measured cost once. From STRIP: `strip.go` and a hard width
threshold, below which a column states the turn and its clock, one ordered row
per tool act with the tool name and no path, the last substantive sentence, and
how to widen it.

**What was refused, and why.** The LEDGER lane's narrow-width transcript
geometry: a full column made thinner is not a usable column. The STRIP lane's
history-navigation row: CLOCK's cue row is the better home, so a strip draws no
scroll cue of its own. The sentence `nothing has arrived yet` as the description
of a quiet seat: it reports absence without naming the act the room waits for,
and the cue row answers with `no acts yet` and then a measurement. The strip form
above the threshold: at a usable width the transcript is EVIDENCE and it stays a
transcript. And the audit's own last item, removing the seat names from the
UNREAD strip: the strip says a reply is UNREAD, which no header says, and the
names are that fact. The phase word had already gone.

**What is asserted, and what is only claimed.** Asserted by a test: that the live
skip leaves the column and the room line names the seat; that a settled rebuild
carries no per-seat note while the room states both halves; that the strip's turn
line sheds the clock before the mark and never clips the number; that the strip
keeps the tool and drops the path; and that the whole strip form survives
`GlyphsFor(true)` with no Unicode mark left in it. Claimed and not asserted: that
these frames read better to a person. Every frame this pass was judged on is a
replay of one recording through the same `Render` the goldens use, which is the
strongest evidence this repository holds short of a demo. Nobody has driven this
room on a live vendor since the pass.

## The Zoom viewer, measured (2026-09-04)

An independent audit failed three prototypes on a projector test at 180x50 on
2026-09-03. It judged the quiet clocks, the act counts, the abbreviated tool rows
and the fine rules too slight for the back of a room. Nobody changed anything for
that finding, and nobody measured it. This section replaces the judgement with a
measurement, and it changes two inks.

**The viewing condition.** The demo of 2026-09-30 runs on the owner's Windows PC.
The display is 3840 by 1600. The room takes a window of about 181 by 71 cells,
which is about 1900 by 1500 pixels at the owner's present font. That gives a cell
of 10.5 by 21 pixels. Zoom carries that surface to people who watch on laptops of
about 1440 by 900. The reader is therefore not on the owner's screen, and the
share is resampled on the way.

**The method.** The frames come from the public scrubbed room,
`examples/demo.jsonl`, played through the same `Render` the goldens use
(`internal/council/frames_emit_test.go`). Four moments were taken at 181x71 and
at 180x50: a dispatch, a gate with the card still open, a turn end, and the final
frame. Two notes on the moments. The scrubbed room holds no arena, so the turn
end stands in for the arena end. The scrubbed room also answers its one gate in
the same record that raises it, so the emitter's gate moment shows an answered
gate; the gate frame here was taken one record earlier, by hand.
Each dark SVG was rendered to PNG at the owner's own pixel size with Chrome,
then scaled the way a viewer's client scales it. Every judgement below was made
on the viewer's own pixels at 1:1, with no further resampling.

| case | share | room | viewer cell | result |
|---|---|---|---|---|
| a | whole screen, 0.375 | 181x71 | 3.94 x 7.88 px | fails |
| b2 | window, fit to 1440x900, 0.60 | 181x71 | 6.30 x 12.60 px | marginal |
| b | window, 0.75 | 181x71 | 7.88 x 15.75 px | reads, one item under its floor |
| c | window, 0.75, larger font | 180x50 | 11.25 x 22.50 px | reads with margin |

**Item by item.** At case a the column header words, the posture rail badges, the
cue row's quiet clock and act count, the strip's one-act rows and the room line
all arrive as texture. Only the loudest anchors survive: the seat names, the
`✓ done` marks and the measured figures. That is a geometry failure and no ink
can fix it. At case b every named item reads. The header words, the badges, the
`quiet 1m18s  1 act` cue, the strip rows `⚙ Read ✓` and `turn 11 ✓  2m1s`, the
room line and the composer are all legible. The hairline leader is the faintest
thing on the frame. At case c every item reads with margin, and the hairline
leader and the column separators are plainly present.

**The demo rule, as one sentence.** Share the terminal WINDOW and never the whole
screen, and set the terminal font so the room is 180 by 50 cells rather than 181
by 71: that gives the viewer a cell of 11.25 by 22.50 pixels, which is the
smallest cell size measured at which every named item reads and every rule still
clears its 3:1 floor.

**What failed at the recommended mode, and what changed.** One thing failed, and
it was an ink. A rule is one device pixel of a 21-pixel row, so the resample
blends it with the ground while the prose beside it keeps almost all of its ink.
The hairline ink was authored at 3.11:1. Read back off the rendered pixels, the
hairline leader arrived at 2.8:1 at the owner's own pixel size, at 2.9:1 at case
c, and at 2.3:1 at case b. The last two are under the 3:1 floor a non-text
component needs, which is the floor this identity adopted on 2026-09-03. So the
two rule inks were raised to carry the resample's own cost as headroom.

| ink | ground | before | after | at the viewer, case c |
|---|---|---|---|---|
| Hair | night `#0c0c0c` | `#675f53` 3.11:1 | `#766c5f` 3.80:1 | 2.85:1 to 3.44:1 |
| RuleInk | night `#0c0c0c` | `#736a5d` 3.68:1 | `#827869` 4.51:1 | thick glyph, no loss |
| Hair | paper `#ffffff` | `#969288` 3.10:1 | `#86837a` 3.79:1 | not measured |

RuleInk moved because the two rule weights must differ by ink as well as by
stroke, and a raised Hair alone would have inverted them. Paper's RuleInk did not
move, because 10.5:1 already carries the headroom twice over. Dim did not move,
because prose is many pixels thick and loses almost nothing to the resample. The
scale keeps its order and it is tighter now: Muted 6.2, Dim 5.1, RuleInk 4.5,
Hair 3.8. Nothing here is a glyph or a layout, so the 118 layout goldens did not
move; the two hero pictures did, because they draw the coloured set.

**What is asserted, and what was only looked at.** Asserted by a test: that Hair
and RuleInk clear 3.75:1 on both grounds, that RuleInk stays above Hair, and that
the night scale keeps the order Muted, Dim, RuleInk, Hair
(`TestTheRuleInksCarryTheZoomHeadroom`). Measured but not asserted: every
contrast figure read back off a rendered pixel, because the number depends on the
renderer and on the resampler, and a test that pinned it would be pinning Chrome
and Pillow rather than this room. Looked at and not measured at all: whether a
person reads these frames faster. One honest limit on the figures above. They
come from an SVG rendered by Chrome, which antialiases a one-pixel rule that
Windows Terminal draws crisp, so the figures are a floor and the owner's own
screen is better than they say. Case b after the raise still puts the hairline
leader at 2.7:1 through that path, which is why the rule above asks for the
larger font and not for the ink alone.
