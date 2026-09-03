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
