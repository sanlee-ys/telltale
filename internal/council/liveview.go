package council

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// The live pane's own render path (design.md §9.53).
//
// It exists because the ordinary one cannot be reused. sanitize (glyphs.go)
// drops every byte below 0x20, ESC included, so a pty stream routed through
// Column.Body would arrive with every escape stripped and the payload
// characters left as literal garbage. That is not a bug to fix in sanitize: the
// rule there is right for text, and a screen is not text. The two paths are
// separate all the way down, which is also what keeps the display-only contract
// checkable — a grid can only reach the frame through this file.

// liveMarkerRows is how many rows the pane spends saying what it is.
//
// One, and it is not optional. The live child is a SECOND process on the same
// account as the seat beside it, so the pane doubles that seat's spend, and a
// surface that quietly billed twice would be the most expensive thing this
// feature could ship. It is also the row that says the screen measures nothing,
// which is the claim a reader most needs while looking at a picture full of
// numbers.
const liveMarkerRows = 1

// liveCell draws a live pane: the seat's ordinary chrome, one marker row, then
// the emulator's screen.
//
// The chrome above it is the SAME chrome every other column gets — the seat's
// name, its posture badges, its gate card — and that is the display-only
// contract seen from the render side. Every claim on this column still comes
// from the structured adapter path or renders absent. Nothing in the grid below
// can reach it.
func liveCell(st State, chrome []string, w, h int, sty Styles, g Glyphs) []string {
	blank := strings.Repeat(" ", w)
	lines := make([]string, 0, h)
	lines = append(lines, chrome...)

	avail := h - len(chrome)
	if avail >= liveMarkerRows {
		lines = append(lines, fit(sty.Muted.Render(liveMarker(st.Live, w, g)), w))
		avail -= liveMarkerRows
	}
	for _, l := range liveRows(st.Live, w, avail, sty, g) {
		lines = append(lines, fit(l, w))
	}
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// liveMarker is the one row that says what this pane is.
//
// Words, never colour and never a lone glyph. Every distinction this UI makes
// is carried first by something that survives --ascii and NO_COLOR (CLAUDE.md),
// and this one carries three facts in five words: the pane is live, it is a
// second process, and nothing on it is measured.
func liveMarker(l LiveSeat, w int, g Glyphs) string {
	state := "live"
	switch l.Phase {
	case LiveOpening:
		state = "live · starting"
	case LiveEnded:
		state = "live · ended"
	case LiveUnavailable:
		return truncate(g.Warn+" live seat unavailable", w, g.Ellipsis)
	}
	full := state + " · second process · display only"
	// The pane sheds the explanation before it sheds the word, because a narrow
	// pane that dropped `live` would be a screen with no label at all — which is
	// the one state this row exists to prevent.
	for _, s := range []string{full, state + " · display only", state} {
		if lipgloss.Width(s) <= w {
			return s
		}
	}
	return truncate(state, w, g.Ellipsis)
}

// liveRows is the body under the marker: the screen, or the sentence explaining
// why there is no screen.
//
// The two are kept apart for §4a.1's reason. A pane that was refused, a pane
// that is starting and a pane whose child has exited would all draw blank if
// this returned nothing, and three different states rendering alike is the
// zero-vs-absent collapse this repo exists to prevent.
func liveRows(l LiveSeat, w, avail int, sty Styles, g Glyphs) []string {
	if avail < 1 {
		return nil
	}
	switch l.Phase {
	case LiveUnavailable:
		note := l.Note
		if note == "" {
			// Never reached by any path in this package, and written anyway:
			// "unavailable" with no reason is exactly the shape §4a.1 spends the
			// room avoiding, and a future caller that forgets the sentence
			// should get a worse-but-honest one rather than a blank pane.
			note = "no reason was recorded, which is itself the defect"
		}
		return clipRows(styleEach(wrap(note, w), sty.Muted), avail)
	case LiveOpening:
		if len(l.Grid) == 0 {
			return clipRows(styleEach(
				wrap("the live seat is starting, and has not painted yet", w), sty.Muted), avail)
		}
	case LiveEnded:
		// A child that ended BADLY says so above its last screen, and it says so
		// in the reading area rather than only in the marker row: the marker has
		// room for a word, and "why" is a sentence. Empty on a clean exit and on
		// a pane the room closed itself, because a process we ended did not
		// fail — runner.go's rule for a killed Handle, applied unchanged.
		if l.Note != "" {
			note := clipRows(styleEach(wrap(l.Note, w), sty.Muted), avail)
			return append(note, clipRows(l.Grid, avail-len(note))...)
		}
	}
	if len(l.Grid) == 0 {
		return nil
	}
	// TOP-anchored, unlike a transcript. Row 0 of an emulator is the top of the
	// screen, and bottom-anchoring a terminal viewport would put its title bar
	// wherever the content happened to end.
	//
	// A grid taller than the pane is clipped from the TOP, keeping the newest
	// rows. That only happens between a resize and the next chunk — the emulator
	// is sized to the pane — so it is a one-frame condition rather than a
	// scrolling model, and there is no overflow marker for it: a marker that
	// flickered for one frame per resize would say less than the rows it cost.
	grid := l.Grid
	if len(grid) > avail {
		grid = grid[len(grid)-avail:]
	}
	return grid
}

// clipRows cuts a slice to at most n entries.
func clipRows(lines []string, n int) []string {
	if len(lines) > n {
		return lines[:n]
	}
	return lines
}

// styleEach wraps every line in one style.
//
// Wrap and measure as PLAIN, style afterwards — the rule view.go states at
// actLinesMarked and the reason these lines go through fit at the call site
// rather than padRight.
func styleEach(lines []string, s lipgloss.Style) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = s.Render(l)
	}
	return out
}
