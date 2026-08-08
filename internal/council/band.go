package council

import (
	"strconv"

	"charm.land/lipgloss/v2"
)

// The committee's live band (design.md §9.30).
//
// §9.9 made each column's echo a fact about that COLUMN — a turn can reach two
// seats and not a third, and a seat that sat one out records nothing for it — and
// that argument is still exactly right about history. What it does not survive is
// the live turn on a committee route: when one brief reaches two, three or four
// on-screen seats, the grid draws the user's own words once per column, side by
// side, across the surface the room exists to compare answers on. Four copies of
// the question, and the four answers pushed a row further down for each of them.
//
// So the live turn's brief is drawn ONCE, full width, directly under the room
// chrome, and the addressed columns stop echoing it while that band is up. The
// band is a RENDERING RULE and not a content model: nothing new reaches State,
// nothing is stored, and the turn each column records is unchanged — which is why
// the moment the turn is filed the per-column echoes are back, in history, where
// §9.9's argument holds.

// liveBrief is the brief of the turn IN FLIGHT and how many on-screen seats
// received it.
//
// Anchored on State.Turn rather than on "this column has a prompt", and the
// difference is the whole of §9.9's rule kept intact. A column's Prompt block
// survives until its NEXT dispatch, so a seat that answered turn 3 and has sat
// out 4 and 5 is still displaying turn 3's brief as its current block — that is
// that seat's own conversation, not the live turn, and the band must not speak
// for it.
//
// The brief is taken from the first participant rather than merged, on
// pageLines' own precedent: dispatch echoes ONE sanitized string to every seat a
// route addresses, so there is one string here, and a merge would be code
// defending against a state this room cannot be in.
func liveBrief(st State) (prompt string, quoted bool, seats int) {
	if st.Turn <= 0 {
		return "", false, 0
	}
	for _, idx := range st.VisibleColumns() {
		c := st.Columns[idx]
		// A seat that cannot be driven was never dispatched to, so it is not a
		// participant that happens to be quiet — it was never in the turn, which
		// is §4a.1's distinction and turnEntries' own test.
		if c.Avail != AvailInstalled || c.TurnN != st.Turn || c.Prompt == "" {
			continue
		}
		if seats == 0 {
			prompt, quoted = c.Prompt, c.Quoted
		}
		seats++
	}
	return prompt, quoted, seats
}

// bandLines is the band itself: the brief at the composer's own `›` and at full
// weight, the rebuttal notice under it when there is one, and a blank row.
//
// **The anatomy is §9.9's echo unchanged, hoisted.** Same glyph, same weight,
// same words, and the same sanitized-but-unredacted string — this is the user's
// own typing echoed to the user on the user's own screen, and there is no new
// content path here to redact along. What is NOT here is the route: the header
// already carries `turn 10 → everyone` on the cell that names the turn (§9.21),
// and a band that repeated it would be the second copy this whole section exists
// to delete.
//
// **The boundary is a blank row, which is §9.11's middle strength.** The three
// this room ranks are a labelled rule where the turn changes, a blank where the
// speaker changes, a blank where the kind of content changes. Under the band the
// speaker changes — the user stops and four vendors start — and that is the
// blank's own meaning, drawn here at full width for exactly the reason turnHead
// draws it inside a column. A rule was considered and refused: the frame's
// full-bleed rule sits two rows above, and a second horizontal line under it
// would be §9.11's "one rule per column instead of two three rows apart" rebuilt
// at the room's scale, with the heavy/light distinction (§9.26) blurred as well.
//
// Returns nil when the live turn reaches fewer than minBandSeats on-screen seats.
// The tier and the height floor are NOT tested here — resolveLayoutIn owns those,
// so there is one place that decides whether a band survives and one number
// (Layout.Band) that everything else reads.
func bandLines(st State, w int, sty Styles, g Glyphs) []string {
	if w < 1 {
		return nil
	}
	prompt, quoted, seats := liveBrief(st)
	if seats < minBandSeats {
		return nil
	}

	brief := hangWrap(g.Prompt+" ", prompt, w)
	var marker string
	if len(brief) > maxBandBrief {
		// The last row of the budget goes to the marker rather than to a fourth
		// row of brief, so what is on screen is three rows and an explicit
		// statement instead of four rows that stop.
		marker = bandMore(st, len(brief)-(maxBandBrief-1), w, g)
		brief = brief[:maxBandBrief-1]
	}
	// Full weight, promptEcho's own reason: in a room of vendor prose the user's
	// words are the anchor a reader navigates by. Wrapped first and styled second,
	// because wrap splits on spaces and would break an escape sequence in half.
	out := styleAll(brief, sty.Strong)
	if marker != "" {
		out = append(out, sty.Muted.Render(marker))
	}
	if quoted {
		// Said ONCE, under the words it qualifies, in promptEcho's own sentence —
		// the constant is shared rather than copied, because two spellings of one
		// fact is how the band and the column would start disagreeing about what
		// rode along with a brief.
		out = append(out, styleAll(indentWrap("  ", quotedNotice, w), sty.Muted)...)
	}
	return append(out, "")
}

// bandMore is the marker a brief too tall for the band is cut with.
//
// It states the COUNT and where the whole thing is, because a reader who cannot
// see the end of their own question has to be told both — "there is more" without
// a destination is the overflow marker §9.10 shipped, and that got the room
// reported as unable to scroll.
//
// The destination is named as the PAGE rather than as the key, and the key is
// added after it in VIEW mode only. `t` is the letter t while composing, so a
// marker that advertised it there would be the room promising a keystroke that
// does something else — scrollHint's rule for `f`, applied to the same problem
// (§7.8). What sheds is the keystroke; the count and the destination survive in
// both modes, which is the shedding order every list in this package keeps.
//
// Indented to the brief's own hang, so it reads as a property of those words
// rather than as a new statement — the card grammar §9.11 gave every wrapped
// second line in this room.
func bandMore(st State, n, w int, g Glyphs) string {
	base := "  " + g.Ellipsis + " " + strconv.Itoa(n) +
		" more lines — the turn page has this brief whole"
	if st.Mode == ModeViewing {
		full := base + "  " + g.Sep + "  t opens it"
		if lipgloss.Width(full) <= w {
			return full
		}
	}
	return truncate(base, w, g.Ellipsis)
}

// bandRows is how many rows the band wants at this terminal's width.
//
// Measured by DRAWING it, never by arithmetic over the brief — columnViewport's
// own rule, for its own reason: two derivations of "how tall is this content at
// this width" drift the day the marker grows a line, and they drift silently
// because both answers are still plausible row counts.
//
// PlainStyles because only the count is wanted, and no style in this package can
// change one: every style here is a wrapper, never a re-wrap.
func bandRows(st State, g Glyphs) int {
	return len(bandLines(st, st.Width-2*framePad, PlainStyles(), g))
}

// bandUp reports that the live turn's brief is on the band, which is the same
// question as "must this column stop echoing it".
//
// It resolves the layout rather than taking a parameter threaded down through
// columnCell, and that is the point rather than a convenience. The two readers of
// this fact are the renderer, which draws the band, and columnLines, which stops
// drawing the echo — and the two failure modes of letting them disagree are a
// brief on screen four times plus a band, or a brief nowhere on screen at all.
// The second is a §4a.1 violation with the user's own words as the missing
// content. One resolution, one answer, no way to hold two.
//
// The cost is that a frame resolves its layout once per column instead of once.
// It is pure integer work over a State — the same call MaxScroll and the hop keys
// already make per keystroke — and it buys a fact that cannot be got wrong.
func bandUp(st State, g Glyphs) bool { return layoutFor(st, g).Band > 0 }
