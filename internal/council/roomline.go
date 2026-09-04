package council

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// The room line (from the LEDGER lane of the density pass, 2026-09-03).
//
// One rule governs this file: a fact about the ROOM prints once, here, and a
// column prints only what is true of that seat and no other. Before this pass
// three room facts printed once per column. The seat that is not on screen was
// already correct and it is the model the other two follow. The seats that sat
// the live turn out printed one muted line each, in three columns, saying the
// same sentence about the same dispatch. The rebuild's measured cost printed
// under every rebuilt column, word for word.
//
// The line costs NO new row in the room that already had a collapsed seat. It
// is the row `collapsedNotice` owned, and the collapsed-seat sentence is now its
// first segment rather than its whole content.
//
// WHAT THIS FILE REFUSES. The LEDGER lane also put a `waiting on N seats —
// nothing has arrived yet` fact here. The audit refused that sentence: it
// reports absence without naming the act the room waits for. The cue row
// answers the same finding per seat and with a measurement (`no acts yet`, then
// the quiet clock), so the room line states no waiting fact at all.
//
// The segments shed from the END, longest-first, on `needsYouLine`'s idiom. The
// order is subject size and then urgency, which is the order `Render` already
// stacks its chrome in: a seat the room cannot draw at all outranks the seats
// that are sitting the live turn out.

// roomFacts is the room's own facts, in priority order, at no width.
//
// Width-free on `needsYouRows`' own argument: `layoutFor` asks only whether the
// row is spent, and a row count that depended on the glyph set or on the frame's
// width would make the room's HEIGHT vary with `--ascii`. The shedding happens
// in roomLine, at the width the frame actually has.
func roomFacts(st State, g Glyphs) []string {
	var out []string
	if n := collapsedNotice(st, g); n != "" {
		out = append(out, n)
	}
	if s := satOutFact(st); s != "" {
		out = append(out, s)
	}
	if st.RoomNote != "" {
		// LAST, and the first thing a full line pushes to the second row: it is
		// the only segment here that reports something that already happened,
		// and the two above it report the state of the room now.
		out = append(out, st.RoomNote)
	}
	return out
}

// satOutFact names the on-screen seats the live turn did not reach.
//
// The finding it answers: `not addressed in turn N` printed in every idle
// column, which is one dispatch described three times. Which seats a turn
// reached is a fact about the DISPATCH, so it belongs to the room, and the
// column's own copy is deleted in columnLines.
//
// The LIVE turn only. A run of turns a seat sat out BEFORE this one is a gap in
// that seat's own reading order, it differs from seat to seat, and no room line
// can supply it — so `skipSpan` keeps every historical run exactly as it was.
//
// The seats are NAMED rather than counted. A count would tell a reader that
// somebody sat the turn out and leave them to find who, one column at a time,
// which is the stall the needs-you strip was written to delete.
func satOutFact(st State) string {
	// The GRID only, which is the band's own rule one row up (layoutFor). This
	// fact names the LIVE turn, and the by-turn page, the arena record and the
	// act ledger each show a turn the reader chose — so `1 seat sat turn 3 out`
	// above a page reading turn 2 would be chrome about content that is not on
	// screen. The help panel replaces the grid outright.
	if st.Page.Open || st.Record != nil || st.Help != HelpClosed {
		return ""
	}
	var names []string
	for _, idx := range st.VisibleColumns() {
		c := st.Columns[idx]
		if c.Skipped && c.Note != "" {
			names = append(names, c.Label)
		}
	}
	if len(names) == 0 {
		return ""
	}
	n := len(names)
	return strconv.Itoa(n) + " " + plural(n, "seat") + " sat turn " +
		strconv.Itoa(st.Turn) + " out: " + strings.Join(names, ", ")
}

// maxRoomRows is the most rows the room line may take.
//
// Two, and the reason is the same trade `bandRows` makes one line down: the room
// line's whole value is deleting rows from four columns at once, so a room line
// that grew without a bound would give back more than it took. Two rows hold
// every fact this file can produce at any width the room draws a grid at.
const maxRoomRows = 2

// roomLines packs the room's facts into at most maxRoomRows lines at width w.
//
// Greedy, in priority order, and NOTHING IS DROPPED. The first shape of this
// function shed the lowest-priority fact when the line was full, and a room with
// a collapsed seat lost `1 seat sat turn 2 out: Antigravity` — a fact that is
// nowhere else on the frame now that the column no longer prints it. A room fact
// that prints once and then does not print at all is worse than the duplication
// this pass exists to delete.
//
// So the facts overflow to a second row, and a second row that is still full is
// truncated by the caller with the ellipsis noticeLine has always used. A clipped
// sentence says it was clipped; a dropped one says nothing.
func roomLines(st State, w int, g Glyphs) []string {
	facts := roomFacts(st, g)
	if len(facts) == 0 {
		return nil
	}
	sep := strings.Repeat(" ", gutter) + g.Sep + strings.Repeat(" ", gutter)
	out := []string{facts[0]}
	for _, f := range facts[1:] {
		last := len(out) - 1
		joined := out[last] + sep + f
		if lipgloss.Width(joined) <= w || len(out) >= maxRoomRows {
			out[last] = joined
			continue
		}
		out = append(out, f)
	}
	return out
}
