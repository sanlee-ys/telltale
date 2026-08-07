package council

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// Render draws one frame.
//
// Pure over State: no clock, no filesystem, no environment. Tests construct a
// State by hand and diff the result against internal/council/testdata/golden —
// the same contract internal/hud renders under (design.md §7.9), and the reason
// council's layout can be verified without a terminal or a vendor process.
func Render(st State, sty Styles, g Glyphs) string {
	if st.Width < MinWidth || st.Height < MinHeight {
		return floorMessage(st, sty)
	}

	lay := layoutFor(st, g)

	// Every assembled line goes through fit at its final width. The rule that no
	// rendered line may exceed the terminal is enforced here, once, rather than
	// trusted to the gap arithmetic in each builder below.
	var b strings.Builder
	b.WriteString(fit(header(st, lay, sty, g), st.Width))
	b.WriteString("\n")
	b.WriteString(rule(st.Width, sty, g))
	b.WriteString("\n")
	if lay.Notice > 0 {
		b.WriteString(fit(" "+noticeLine(st, sty, g, st.Width-2), st.Width))
		b.WriteString("\n")
	}

	if st.Help != HelpClosed {
		b.WriteString(helpBody(st, lay, sty, g))
	} else if lay.Tier == TierTabs {
		if lay.Tabs {
			b.WriteString(tabBar(st, lay, sty, g))
			b.WriteString("\n")
		}
		b.WriteString(tabBody(st, lay, sty, g))
	} else {
		b.WriteString(columnsBody(st, lay, sty, g))
	}

	b.WriteString("\n")
	b.WriteString(rule(st.Width, sty, g))
	b.WriteString("\n")
	for _, l := range composerLines(st, lay, sty, g) {
		b.WriteString(fit(l, st.Width))
		b.WriteString("\n")
	}
	b.WriteString(fit(modeLine(st, lay, sty, g), st.Width))
	return b.String()
}

// layoutFor resolves the frame for one State: the widths, plus the two things
// that now vary with content — how many rows the draft needs and whether a
// collapsed-seat notice is on screen.
//
// Every caller that measures the body goes through here rather than through
// resolveLayout, so a scroll key and the renderer can never disagree about how
// many rows there are.
func layoutFor(st State, g Glyphs) Layout {
	vis := st.VisibleColumns()
	return resolveLayoutIn(layoutInput{
		Width:    st.Width,
		Height:   st.Height,
		Cols:     len(vis),
		Expanded: st.Expanded,
		Composer: composerRows(st, g),
		Notice:   collapsedNotice(st, g) != "",
		Primary:  framePrimary(st, vis),
	})
}

// framePrimary marks which visible seats own the frame this turn.
//
// Nil means equal columns. A partial set means those seats share the wide
// region and the rest sit at stripColumn until the next dispatch.
func framePrimary(st State, vis []int) []bool {
	if st.Expanded || len(st.FrameOwners) == 0 || len(vis) < 2 {
		return nil
	}
	out := make([]bool, len(vis))
	n := 0
	for j, idx := range vis {
		for _, o := range st.FrameOwners {
			if st.Columns[idx].Vendor == o {
				out[j] = true
				n++
				break
			}
		}
	}
	if n == 0 || n == len(vis) {
		return nil
	}
	return out
}

// floorMessage is what a terminal too small to draw the room gets. It names the
// number needed rather than just refusing, so the user knows how far to drag.
func floorMessage(st State, sty Styles) string {
	want := "council needs " + strconv.Itoa(MinWidth) + "x" + strconv.Itoa(MinHeight)
	got := " (have " + strconv.Itoa(st.Width) + "x" + strconv.Itoa(st.Height) + ")"
	return sty.Muted.Render(want + got)
}

func rule(w int, sty Styles, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	return sty.Rule().Render(strings.Repeat(g.Rule, w))
}

// header names the workspace and the round.
//
// The workspace is on screen at all times and is not decoration: every turn is
// dispatched with it as the working directory, so which directory this is
// changes what the agents can see. A dispatch room that hid its own cwd would
// be the same class of omission as a gauge that hid its units.
func header(st State, lay Layout, sty Styles, g Glyphs) string {
	// Full weight, because this is the one word on screen that says which
	// telltale surface you are looking at, and the HUD's header opens the same
	// way. The two headers now share a shape as well as a palette: product name,
	// separator, subject, then the counts right-anchored.
	left := sty.Strong.Render("council")
	if st.Write {
		// Persistent, not a one-off notice. A notice scrolls away and a badge
		// can be missed while reading a column; the state it describes lasts
		// the whole session, so its marker does too.
		left += " " + sty.SevCrit.Render(g.Warn+" WRITE")
	} else {
		// Stated rather than left blank, now that write is the default. An
		// absent badge used to mean read; it would now mean either read or a
		// marker that failed to render, and a reader cannot tell those apart.
		// Both postures name themselves so neither is inferred from a gap.
		left += " " + sty.Muted.Render("READ")
	}

	round := "no turn yet"
	if st.Turn > 0 {
		round = "turn " + strconv.Itoa(st.Turn)
	}
	// A chain dispatches its own next hop, so the room names the hop it is on
	// and the seat holding it. This is the only line here that explains a turn
	// the user did not press enter on: three idle columns and a brief nobody
	// typed is otherwise indistinguishable from the room acting on its own.
	if st.FlowSteps > 0 {
		round += "  " + g.Sep + "  hop " + strconv.Itoa(st.FlowHop) + "/" + strconv.Itoa(st.FlowSteps) + " @" + string(st.FlowVendor)
	}
	seated := strconv.Itoa(st.Seated()) + "/" + strconv.Itoa(len(st.Columns)) + " seated"
	// "no brief" is stated rather than left blank, and that asymmetry is
	// deliberate. An unbriefed room LOOKS identical to a briefed one until a
	// vendor guesses at a convention out loud — which is exactly how this was
	// discovered. Absence of shared context is a fact about the room, so the
	// room says it.
	brief := "no brief"
	if st.Briefed {
		brief = "briefed"
	}
	right := sty.Muted.Render(round + "  " + g.Sep + "  " + seated + "  " + g.Sep + "  " + brief)

	// The path takes whatever is left, elided from the left because the
	// uninformative part of a path is its prefix. It is introduced by the same
	// "  │  " the HUD's header uses between its own zones, so the room's name
	// and the directory it dispatches into read as two facts rather than as one
	// run-on label — which is what a bare space made them.
	sep := "  " + sty.Rule().Render(g.Sep) + "  "
	used := lipgloss.Width(left) + lipgloss.Width(right) + 2 + lipgloss.Width(sep)
	pathw := lay.Width - used
	mid := ""
	if pathw > 3 {
		mid = sep + sty.Muted.Render(elideLeft(displayPath(st), pathw, g.Ellipsis))
	}

	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + left + mid + strings.Repeat(" ", gap) + right + " "
}

// noticeLine is the collapsed-seat notice, truncated honestly.
//
// It used to be handed to fit, which cuts without saying so: at 120 columns the
// reference machine's notice lost the last word of "--vendor all seats them
// anyway" and looked like a sentence that simply stopped. Everywhere else in
// this room a clipped string says it was clipped, and a line about seats you
// cannot see is a poor place to start making exceptions.
//
// The warning mark carries the hue and the words carry the fact, the same split
// the activity trace's outcome marks use.
func noticeLine(st State, sty Styles, g Glyphs, w int) string {
	n := truncate(collapsedNotice(st, g), w, g.Ellipsis)
	if rest, ok := strings.CutPrefix(n, g.Warn); ok {
		return sty.SevWarn.Render(g.Warn) + sty.Muted.Render(rest)
	}
	return sty.Muted.Render(n)
}

// displayPath abbreviates the home prefix. Display only — the dispatched
// working directory is always the resolved absolute path.
func displayPath(st State) string {
	p := st.Workspace
	if st.Home != "" && strings.HasPrefix(p, st.Home) {
		return "~" + p[len(st.Home):]
	}
	return p
}

// collapsedNotice names the seats folded out of the grid, and why.
//
// One muted line under the header rather than a column each, and the trade is
// the point: a seat that cannot be driven was holding a quarter of the width to
// repeat one sentence for the whole session, while the replies it was crowding
// are what the room exists to compare. What must NOT change is that the failure
// stays visible — a seat that vanished with no line anywhere would be worse
// than the column it replaced, because a user who never saw it has no reason to
// go looking (§4a.1).
//
// Empty when nothing is collapsed, which is the common case and costs no row.
func collapsedNotice(st State, g Glyphs) string {
	gone := st.CollapsedColumns()
	if len(gone) == 0 {
		return ""
	}
	parts := make([]string, 0, len(gone))
	for _, i := range gone {
		c := st.Columns[i]
		parts = append(parts, c.Label+" ("+st.CollapseReason(c)+")")
	}
	lead := "1 seat is not on screen: "
	if len(gone) > 1 {
		lead = strconv.Itoa(len(gone)) + " seats are not on screen: "
	}
	// The remedy is last because it is the least urgent part of the sentence and
	// the first thing a narrow terminal should drop. It is also in --help and in
	// the help panel, so truncating here loses a convenience rather than the
	// only copy of it.
	return g.Warn + " " + lead + strings.Join(parts, ", ") +
		" " + g.Sep + " --vendor all seats them anyway"
}

// columnsBody draws the seats side by side.
//
// Vertical separators appear only on rows that carry content in any column.
// A tall idle window used to draw │ through every empty body row down to the
// footer — four spears through a void. Bottom-anchor adds a second void
// between chrome and transcript; the same per-row rule keeps that pad
// sep-free. Gutters stay blank-width so the grid does not shear.
func columnsBody(st State, lay Layout, sty Styles, g Glyphs) string {
	vis := st.VisibleColumns()
	cells := make([][]string, len(vis))
	for j, idx := range vis {
		// extraFor is indexed by POSITION in the row, not by seat: the leftover
		// cells go to the leftmost drawn column, and a collapsed seat has no
		// position to give them to.
		w := lay.widthAt(j)
		f := seatUnfocused
		if idx == st.Focus {
			f = seatFocused
		}
		// The SCROLL hint rides only on the column those keys actually move.
		// Repeating it on all four would be three false claims. A column the
		// keys do not reach gets the key that would reach it instead — see
		// focusHint, and the report that made this necessary.
		hint := focusHint(st, g)
		if f == seatFocused {
			hint = scrollHint(st, g)
		}
		cells[j] = columnCell(st, st.Columns[idx], f, hint, w, lay.Body, sty, g)
	}

	sepPad := strings.Repeat(" ", gutter)
	plainSep := sepPad + g.Sep + sepPad
	sep := sepPad + sty.Rule().Render(g.Sep) + sepPad
	blankSep := strings.Repeat(" ", lipgloss.Width(plainSep))
	var b strings.Builder
	for row := 0; row < lay.Body; row++ {
		b.WriteString(" ")
		// Rails only on rows that carry content in any column. Bottom-anchor
		// puts blank pad between chrome and transcript; drawing │ through that
		// pad would recreate the void spears Phase 2 removed below the content.
		div := blankSep
		if rowHasContent(cells, row) {
			div = sep
		}
		for j := range vis {
			if j > 0 {
				b.WriteString(div)
			}
			b.WriteString(cells[j][row])
		}
		b.WriteString(" ")
		if row < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// rowHasContent reports that any drawn column has non-blank text on this row.
func rowHasContent(cells [][]string, row int) bool {
	for _, col := range cells {
		if row >= 0 && row < len(col) && strings.TrimSpace(col[row]) != "" {
			return true
		}
	}
	return false
}

// seatFocus is how a column stands in relation to the keys, which is two
// questions rather than one — and collapsing them into a single bool is what
// made the focus marker so easy to miss.
//
// The MARKER answers "which column is selected" and the WEIGHT answers "which
// column do the scroll keys move". They agree in the side-by-side tier and part
// company in the tabbed and expanded ones, where the tab bar above already
// carries a marker and a second one under it would be noise — while the column
// beneath it is still very much the one the keys address.
type seatFocus uint8

const (
	// seatUnfocused: another column has the keys.
	seatUnfocused seatFocus = iota
	// seatFocused: this column has them, and says so with the marker.
	seatFocused
	// seatAddressed: this column has them; the tab bar above carries the marker.
	seatAddressed
)

// marked reports whether this column draws the focus glyph.
func (f seatFocus) marked() bool { return f == seatFocused }

// hasKeys reports whether the scroll keys move this column, which is what the
// seat name's weight now says.
func (f seatFocus) hasKeys() bool { return f != seatUnfocused }

// columnCell renders one column to exactly h lines of exactly w cells.
//
// Returning a fixed rectangle is what keeps the side-by-side join honest: a
// short column pads rather than collapsing, so a vendor that has said nothing
// yet occupies its seat instead of letting its neighbours slide left.
// hint is the key names appended to this column's overflow marker: the scroll
// keys on the column they move, and `tab` on a column they do not.
//
// Short content is BOTTOM-ANCHORED under the chrome: spare rows sit between
// the badge/gate block and the transcript, so the latest output sits next to
// the composer in a tall window. Long content fills avail and behaves as
// before. The pad depends only on (avail − window height) — viewport geometry,
// not phase or activity — and shrinks as the reply grows, so completion never
// jumps. Scroll-up still freezes via Follow; G restores the tail.
func columnCell(st State, c Column, f seatFocus, hint []string, w, h int, sty Styles, g Glyphs) []string {
	chrome := columnChrome(st, c, f, w, sty, g)
	if len(chrome) > h {
		chrome = chrome[:h]
	}

	body := columnText(st, c, w, sty, g)
	avail := h - len(chrome)
	win, above, below := scrollWindow(c, body, avail)

	bodyLines := make([]string, 0, len(win))
	for i, l := range win {
		// The overflow markers replace the first and last visible lines rather
		// than sitting outside the body, because the body area is the whole
		// budget. Spending a line to say "there is more" is worth it: silent
		// clipping is indistinguishable from a vendor that stopped talking,
		// which is exactly the ambiguity §4a.1 forbids.
		switch {
		case i == 0 && above > 0:
			bodyLines = append(bodyLines, sty.Muted.Render(padRight(
				overflowMarker(g.Up, above, "above", hint, w, g), w, g)))
		case i == len(win)-1 && below > 0:
			// The hint is named once per column. On a column with content hidden
			// both ways it has already been said above, and saying it twice in
			// one cell is the kind of noise that makes the count harder to find.
			h := hint
			if above > 0 {
				h = nil
			}
			bodyLines = append(bodyLines, sty.Muted.Render(padRight(
				overflowMarker(g.Down, below, "below", h, w, g), w, g)))
		default:
			// fit, not padRight, and this is the ANSI trap §9.5 records rather
			// than a stylistic choice. Body lines can now carry style — the
			// outcome mark on a trace entry is coloured — and padRight
			// truncates rune by rune, so it would cut through an escape
			// sequence and count escape bytes as width. Goldens render with
			// PlainStyles and are blind to that, which is exactly why the rule
			// is enforced by the function used rather than by review.
			bodyLines = append(bodyLines, fit(l, w))
		}
	}

	blank := strings.Repeat(" ", w)
	lines := make([]string, 0, h)
	lines = append(lines, chrome...)
	// Spare rows ABOVE the transcript, not below: that is the bottom-anchor.
	pad := avail - len(bodyLines)
	if pad < 0 {
		pad = 0
	}
	for i := 0; i < pad; i++ {
		lines = append(lines, blank)
	}
	lines = append(lines, bodyLines...)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// columnChrome is the fixed top of a column: who this seat is, what it may do,
// anything it is blocked on, and one empty row before the reading starts.
//
// Factored out of columnCell because MaxScroll has to subtract exactly this
// many rows, and it used to subtract the literal 3 — which was wrong for a
// column with no badges at all and would have gone wrong again the moment the
// chrome changed shape. One function, two callers, no constant to keep in step.
//
// Everything here is CHROME, not body, and that is a safety decision rather
// than a layout one. The badge line carries the sandbox claim, and the first
// version of this scrolled it away with the text — so a user reading the middle
// of a long reply from the unsandboxed column had nothing on screen telling
// them that column can write to their tree. A claim that disappears when you
// read is not a claim. The approval card earns its place for a stronger version
// of the same reason: a vendor is STOPPED behind it, and during a turn every
// column is following its own tail, so a card in the body would be pushed off
// screen by the output of the very call it is asking about.
func columnChrome(st State, c Column, f seatFocus, w int, sty Styles, g Glyphs) []string {
	lines := []string{
		fit(columnHeader(st, c, f, w, sty, g), w),
		// The badge row is RESERVED rather than conditional. A seat with no
		// posture to state is rare and its neighbours are not: dropping the row
		// on one column would start its body a line above every other column's,
		// and a grid whose rows do not line up is a worse trade than one empty
		// claim slot.
		fit(badgeRow(c, w, sty, g), w),
	}
	for _, l := range gateCard(st, c, w, sty, g) {
		lines = append(lines, fit(l, w))
	}
	// One blank row, where a full-width per-column rule used to be.
	//
	// The rule was the second horizontal line in three rows — one under the room
	// header, another under every column's badges — and the lower one was doing
	// almost nothing: the header above it already reads as a heading, and by the
	// time a reader reaches the rule they have not been told anything the two
	// lines above did not say. The column header now carries a rule of its own
	// (see columnHeader), which separates the seat from its content in the same
	// gesture that binds the seat's name to its state. What the body actually
	// needed here was air, so that is what the row is spent on.
	return append(lines, strings.Repeat(" ", maxInt(0, w)))
}

// overflowMarker is the "there is more" line, plus the keys that would reach it.
//
// The count alone was a marker that told a reader something was hidden and
// nothing at all about how to see it — which is how a room with working
// scrollback, page keys, `g`, `G` and a full-width expand got reported as
// having "no way to scroll". The keys are named where the eye already is,
// rather than only in a footer that is scanned once and then ignored.
//
// The hints arrive longest-first and the widest one that fits is used, because
// a three-seat room at 120 columns gives each column 37 cells and the full hint
// wants 42 — an all-or-nothing test would have dropped the whole thing in
// exactly the room this was written for. The count itself is never traded away:
// how much is hidden outranks how to reach it, the same order turnRule keeps.
func overflowMarker(mark string, n int, where string, hints []string, w int, g Glyphs) string {
	s := mark + " " + strconv.Itoa(n) + " more " + where
	sep := "  " + g.Sep + "  "
	for _, h := range hints {
		if lipgloss.Width(s)+lipgloss.Width(sep)+lipgloss.Width(h) <= w {
			return s + sep + h
		}
	}
	return s
}

// scrollHint is the keys that move the focused column, as the CURRENT mode has
// them, widest first.
//
// Mode-aware because `f` is the letter f while composing, and a marker that
// advertised it there would be the room telling the user a key does something
// it does not — the precise failure the always-on mode line exists to prevent
// (design.md §7.8). The arrows are unqualified because they now mean the same
// thing in both modes, which is the change this whole branch is about.
//
// `f` is the part that gets dropped on a narrow column, and that is the right
// way round twice over: the arrows are what the user was reaching for, and `f`
// is already named in the view mode line at every width. Compose has one form
// only, which is also the short one — so the mode that needed this most is the
// mode where it always fits.
func scrollHint(st State, g Glyphs) []string {
	base := g.Up + g.Down + " scroll"
	// `f` expands the focused column to the full width, which is the width the
	// only column already has. In a room with one seat on screen the key does
	// nothing, so the marker does not offer it — the same reason the mode line
	// drops it there.
	if st.Mode == ModeComposing || len(st.VisibleColumns()) < 2 {
		return []string{base}
	}
	return []string{base + "  " + g.Sep + "  f expand", base}
}

// focusHint is what an UNFOCUSED column's overflow marker offers instead.
//
// The room was reported as unable to scroll a second time, and this one was not
// a dead key either: "scrolling works for your window. i tried scrolling up/down
// in agy and cursor. could not." Both halves of that sentence are accurate.
// The keys address the focused column, they had always addressed the focused
// column, and every column with content hidden said `↑ 36 more above` in exactly
// the same words — so three seats each advertised that they were holding
// something back and only one of them named a key. A reader who presses ↑ at
// that point moves a DIFFERENT column, sees nothing happen in the one they are
// looking at, and correctly concludes the feature does not work.
//
// So a column the keys do not reach names the key that would reach it. This is
// the same rule the scroll hint already follows — a marker states the key for
// THIS column and never a neighbour's — applied to the case that was left blank
// rather than to a new one.
//
// Honest in both modes: `tab` moves focus while composing too, which is what
// §9.10 landed, so this hint does not need the mode-awareness `f` needs. Empty
// in a room with one seat on screen, where there is nothing to tab to.
func focusHint(st State, g Glyphs) []string {
	if len(st.VisibleColumns()) < 2 {
		return nil
	}
	// Longest first, like every hint list here: the widest form that fits wins,
	// and the count is never traded for either of them.
	return []string{"tab to focus", "tab"}
}

// scrollWindow picks the visible slice of a column's body.
//
// Pure, and derived from Column rather than mutating it, so Render stays a
// function of State: a column that is following computes its offset from the
// content it currently has, which means the tail cannot drift out of sync with
// what arrived. A column the user has scrolled uses its stored offset, clamped
// here so a resize or a shorter reply can never strand the view past the end.
func scrollWindow(c Column, body []string, avail int) (win []string, above, below int) {
	if avail <= 0 {
		return nil, 0, 0
	}
	if len(body) <= avail {
		return body, 0, 0
	}

	max := len(body) - avail
	off := c.Scroll
	if c.Follow {
		off = max
	}
	if off < 0 {
		off = 0
	}
	if off > max {
		off = max
	}
	return body[off : off+avail], off, max - off
}

// MaxScroll is the largest useful offset for a column at the given geometry.
// Exported for the program loop, which clamps a keystroke against it.
//
// It spans the WHOLE transcript, because columnText does: the history, the
// prompts and the current turn are one list of lines, so `g` reaches the first
// thing this seat was ever asked and there is no second scroll model to keep in
// step with the first.
func MaxScroll(st State, idx int) int {
	if idx < 0 || idx >= len(st.Columns) {
		return 0
	}
	pos := -1
	for j, v := range st.VisibleColumns() {
		if v == idx {
			pos = j
			break
		}
	}
	if pos < 0 {
		// A collapsed seat has no window to scroll.
		return 0
	}
	lay := layoutFor(st, GlyphsFor(st.ASCII))
	w := lay.widthAt(pos)
	// PlainStyles because only the line COUNT is wanted here, and styling
	// cannot change it — every style in this package is a wrapper, never a
	// re-wrap.
	sty, gl := PlainStyles(), GlyphsFor(st.ASCII)
	// The chrome is measured by drawing it, never by a constant. A card of a
	// different height, or a column with nothing to claim, would otherwise let
	// the tail scroll past the end of the content — and the constant that used
	// to sit here was already wrong for the second case.
	avail := lay.Body - len(columnChrome(st, st.Columns[idx], seatUnfocused, w, sty, gl))
	n := len(columnText(st, st.Columns[idx], w, sty, gl))
	if m := n - avail; m > 0 {
		return m
	}
	return 0
}

// columnHeader is one seat and what it is doing: "▸ Claude Code ──── ✓ done 8s".
//
// It used to be a name at the far left and a bare word at the far right with
// twenty-five dead cells between them, which reads as two unrelated labels
// rather than as a seat with a state. Three things changed and they are one
// idea: the name takes full weight because it is the anchor, the state takes a
// mark before its word (see phaseMark) because a shape is legible at a glance
// where a five-letter word is not, and the gap is filled with a rule.
//
// The rule is not decoration — it is this room's EXISTING grammar for "a label
// and the numbers that belong to it", which is exactly what turnRule draws for
// every turn in the transcript below. Using the same shape for the live turn's
// header and for a finished turn's separator means a reader learns one line
// form instead of two, and the header stops being able to read as two things.
func columnHeader(st State, c Column, f seatFocus, w int, sty Styles, g Glyphs) string {
	// A space after the focus mark, and two cells of indent without it, so the
	// names still line up across the row. "▸Claude Code" saved a cell and spent
	// it looking like a typo.
	name := "  " + c.Label
	if f.marked() {
		name = g.Focus + " " + c.Label
	}

	// The weight now says which seat the keys move, and that is a correction
	// rather than an addition. §9.11 gave EVERY seat name full weight because a
	// name is the anchor a reader scans for — which is true, and it spent the
	// room's loudest typographic signal on the one fact that is the same in all
	// four columns. The focus marker was then a single glyph in a frame where
	// nothing else varied, and it was reported as invisible. Unfocused names keep
	// the identity hue and give up the weight: still names, still legible, no
	// longer competing with the one that answers "which column do these keys
	// move".
	//
	// Colour and weight are both second signals here. The `▸` still carries the
	// distinction on its own, so --ascii, NO_COLOR and every PlainStyles golden
	// are untouched — which is exactly the property that makes weight safe to
	// spend (§9.11).
	label := sty.Identity
	if f.hasKeys() {
		label = sty.Strong
	}

	status := columnStatus(st, c, g)
	style := sty.ForPhase(c.Phase)
	if c.Avail != AvailInstalled {
		style = sty.SevWarn
	}
	right := style.Render(status)

	// The gap between name and state is filled with a rule when the seat is
	// doing something — same grammar as turnRule, and the two-cell air each
	// side keeps an ascii spinner ("-") from vanishing into the leader ("-").
	// Idle seats used to get the same ink for nothing to divide: a long ────
	// between a name and "○ idle" was filling, not separating. Whitespace does
	// that job and costs no chrome; the name and state still share one line.
	gap := w - lipgloss.Width(name) - lipgloss.Width(status) - 4
	if gap < 1 {
		keep := maxInt(1, w-lipgloss.Width(status)-1)
		return label.Render(truncate(name, keep, g.Ellipsis)) + " " + right
	}
	mid := strings.Repeat(" ", gap)
	if headerUsesLeader(c) {
		mid = sty.Rule().Render(strings.Repeat(g.Rule, gap))
	}
	return label.Render(name) + "  " + mid + "  " + right
}

// headerUsesLeader reports whether the column header fills its gap with the
// turn-rule glyph. Idle has nothing to bind; every other phase does (and
// streaming/waiting need the air around a rule so an ascii spinner stays
// distinct).
func headerUsesLeader(c Column) bool {
	return c.Phase != PhaseIdle || c.Avail != AvailInstalled
}

// columnStatus is the state word with its mark and, where there is one, the
// clock that says how long it took or has taken.
func columnStatus(st State, c Column, g Glyphs) string {
	if c.Avail != AvailInstalled {
		return g.Warn + " unavailable"
	}
	status := c.Phase.String()
	if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		// The clock is the answer to "why is this one taking so long".
		// Without it a final-only vendor is a blank column and a spinner,
		// which reads as broken rather than slow — and two of the three
		// vendors here are final-only, so that ambiguity is the common case
		// rather than an edge one.
		//
		// Appended only when there IS one: the old code always added the space
		// and left a trailing cell on every column that had not started, which
		// pushed the state word one cell off the right edge it was aligned to.
		if e := elapsed(st, c); e != "" {
			status += " " + e
		}
		return phaseMark(c.Phase, st, g) + " " + status
	}
	if c.Elapsed > 0 {
		// Kept after the turn ends. A finished column should still be able to
		// say how long it made you wait, which is the only way the asymmetry
		// between a streaming vendor and a final-only one is ever legible.
		status += " " + dur(c.Elapsed)
	}
	return phaseMark(c.Phase, st, g) + " " + status
}

// phaseMark is the glyph in front of a column's state word.
//
// The five phase words were distinguished by the word and by colour and by
// nothing else, which is a thin way to render the single fact a reader scans
// four columns for. The marks fix that, and every one of them is a meaning this
// room already owns rather than a new alphabet:
//
//   - a turn IN FLIGHT is the spinner, which already sat in this slot. Making
//     it the in-flight member of one vocabulary rather than a special case is
//     what makes the rest coherent — and it stays the room's ONLY moving cell
//     (§7.1 rule 4), because none of the others move.
//   - a turn that FINISHED is ActOK, and one that BROKE is ActFail. Those are
//     the same claims the activity trace makes about a single step, made about
//     the whole turn; reusing the mark is reusing the meaning, which is the
//     opposite of the collision glyphs.go argues against.
//   - a turn that did NOT COMPLETE NORMALLY — cancelled by the user, or a seat
//     that is not there at all — is Warn, which is already what the note and
//     the unavailable card open with. The word after the mark is what separates
//     those two, and it always renders.
//   - a seat NOTHING HAS BEEN ASKED OF is Idle, the one new glyph.
//
// Colour stays redundant throughout: every phase still renders its own word, so
// --ascii and a monochrome terminal lose nothing (§7.1 rule 2).
func phaseMark(p Phase, st State, g Glyphs) string {
	switch p {
	case PhaseStreaming, PhaseWaiting:
		if len(g.Spinner) == 0 {
			return g.Idle
		}
		return g.Spinner[st.Spinner%len(g.Spinner)]
	case PhaseDone:
		return g.ActOK
	case PhaseFailed:
		return g.ActFail
	case PhaseCancelled:
		return g.Warn
	default:
		return g.Idle
	}
}

// elapsed is how long the current turn has been running, from State.Now rather
// than the clock, so Render stays pure.
func elapsed(st State, c Column) string {
	if c.Started.IsZero() || st.Now.IsZero() {
		return ""
	}
	d := st.Now.Sub(c.Started)
	if d < 0 {
		return ""
	}
	return dur(d)
}

// dur renders a duration at one-second resolution.
//
// Seconds, not milliseconds: this measures how long a language model took to
// think, where a hundred milliseconds is noise and pretending otherwise would
// be precision the number does not carry.
func dur(d time.Duration) string {
	s := int(d.Seconds())
	if s < 60 {
		return strconv.Itoa(s) + "s"
	}
	return strconv.Itoa(s/60) + "m" + strconv.Itoa(s%60) + "s"
}

// columnText is a column's whole transcript: every turn this seat has taken,
// oldest first, then the one in flight — or the card explaining why there is
// none.
//
// One flat list of lines rather than a scroll model per turn, which is what
// makes the scrollback work at all: the window, the overflow markers, the tail
// and MaxScroll are the code that was already here, and the transcript is just
// more lines for them to move through.
func columnText(st State, c Column, w int, sty Styles, g Glyphs) []string {
	if c.Avail != AvailInstalled {
		return unavailableCard(c, w, sty, g)
	}

	var out []string
	for _, h := range c.History {
		// No blank BETWEEN turns any more, and that is a swap rather than a cut:
		// the row it used to spend now sits between the brief and the answer
		// (see turnHead). A turn boundary already has the loudest divider this
		// column owns — a labelled full-width rule — while the moment the
		// speaker changes had nothing at all, and one blank per turn buys more
		// where nothing was than where a rule already is. The transcript is
		// therefore exactly as tall as it was before.
		out = append(out, turnHead(h.N, historyMeta(h), h.Prompt, h.Quoted, w, sty, g)...)
		out = append(out, pastTurn(h, w, sty, g)...)
	}
	if c.Prompt != "" {
		// The current turn's separator carries the number and nothing else. Its
		// clock and its cost are in the column header and the badge line, which
		// is chrome that describes exactly this turn — repeating them a row
		// later would be the room saying the same thing twice. A past turn has
		// no chrome of its own, which is why the record carries them.
		out = append(out, turnHead(c.TurnN, "", c.Prompt, c.Quoted, w, sty, g)...)
	}

	// The activity trace comes FIRST and is visually distinct, because it is
	// chronologically first — the vendor acted, then answered. Prefixed rather
	// than merely dimmed: colour is the second signal in this product, never
	// the only one, so the trace survives --ascii and a monochrome terminal.
	for _, a := range c.Acts {
		out = append(out, actLines(a, w, sty, g)...)
	}
	if len(c.Acts) > 0 && (c.Body != "" || c.Phase == PhaseDone) {
		out = append(out, "")
	}

	switch {
	case c.Phase == PhaseStreaming && c.Body == "" && len(c.Acts) > 0:
		out = append(out, wrap("working…", w)...)
	case c.Phase == PhaseIdle && c.Body == "" && st.Reattached.Active():
		out = append(out, reattachCard(st, c, w, sty)...)
	case c.Phase == PhaseIdle && c.Body == "":
		out = append(out, wrap("no turn dispatched yet.", w)...)
	// The three waiting lines below are one row each, on purpose, and what they
	// used to be is the point.
	//
	// This card exists because PhaseWaiting must never be mistaken for streaming
	// — a genuine honesty distinction (§9.2), and it is kept. What did not
	// belong is the ARGUMENT for it, restated in full in the body of every
	// waiting turn: "this vendor reports no incremental output, so nothing
	// appears until the turn finishes" is a sentence about council's plumbing,
	// written in council's vocabulary, in the space where a user came to read an
	// answer. Two thirds of the room renders this card, so on a normal turn it
	// was most of what was on screen.
	//
	// What carries the distinction now is the word already in the column header
	// — `waiting` against `streaming`, always drawn, in both glyph sets, beside
	// the granularity badge that says WHY. The body says only that the seat is
	// working and what to expect, and the wiring moved to the help panel's
	// posture page, where a reader who wants it can go and get it. That is the
	// same trade §9.13 made for the sandbox badges: the claim stays on the
	// column, the argument moves somewhere it can be read properly.
	case c.Phase == PhaseWaiting && c.Body == "" && len(c.Acts) > 0:
		// It has acted but not spoken. This one keeps its own sentence because
		// it is a different claim from the two below — there IS something on
		// screen, and pointing at it beats describing the seat.
		out = append(out, wrap("working — the steps above are what it has done so far.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "" && c.Gran == GranUnknown:
		// Deliberately NOT the line below. "The reply arrives whole" is a
		// measurement two vendors earned; a column whose granularity was never
		// established must not borrow it. This says only what is observed —
		// nothing has arrived — and claims nothing about whether anything will
		// before the end. The header carries the rest: this is the one seat that
		// prints no granularity word at all.
		out = append(out, wrap("working — nothing has arrived yet.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "":
		// The honest version of an empty streaming column, in one line. This
		// vendor is working; it just does not report anything until it is done.
		out = append(out, wrap("working — the reply arrives whole.", w)...)
	default:
		out = append(out, wrap(c.Body, w)...)
	}

	if c.Note != "" {
		out = append(out, "")
		out = append(out, noteCard(c.Note, c.NoteDetail, c.NoteCalm, w, sty, g)...)
	}
	if c.Cleared {
		// LAST, below everything, because that is when it happened: the turns
		// above were said, and then the thread behind them was ended. Drawn as a
		// labelled rule rather than as a card for the same reason turnHead is —
		// it marks a boundary in the transcript, and the transcript's existing
		// grammar for "a boundary you scroll past" is a rule with a word on it.
		//
		// The turns are deliberately still there. What was cleared is the thread
		// the next brief would have continued, not the record of what was said,
		// and erasing the reading surface to report a vendor-side change would be
		// the room destroying the thing it exists to show.
		out = append(out, "")
		out = append(out, sty.Muted.Render(padRight(labelRule("thread cleared", "", w, g), w, g)))
		// The same sentence the no-thread reattach card uses, on purpose: one
		// vocabulary for one fact. A seat that never had a thread and a seat
		// whose thread was just ended arrive at the same next brief, and the two
		// are told apart by the marker above rather than by two descriptions of
		// one outcome.
		out = append(out, wrap("its next brief opens a new session, with the brief re-applied.", w)...)
	}
	return out
}

// turnHead opens one turn: the separator naming it, then the brief that
// produced it, then a blank row before the seat answers.
//
// The blank is what turns a log into a conversation. Without it the user's
// question and the vendor's reply arrive as consecutive lines at the same
// indent and are told apart only by a "›" at the start of one of them — which
// is a distinction you have to READ, on a surface whose whole purpose is
// putting four answers where they can be compared at a glance. It costs one row
// per turn, paid out of the scrollback rather than out of the live turn, and
// the scrollback is the part with rows to spare.
func turnHead(n int, meta, prompt string, quoted bool, w int, sty Styles, g Glyphs) []string {
	out := []string{sty.Muted.Render(padRight(turnRule(n, meta, w, g), w, g))}
	echo := promptEcho(prompt, quoted, w, sty, g)
	out = append(out, echo...)
	if len(echo) > 0 {
		out = append(out, "")
	}
	return out
}

// turnRule is the separator line: "turn 3 ───────────  12s  $0.0123".
//
// The meta is dropped before the label when the column is too narrow for both.
// Which turn this is outranks how long it took: without the number the reply
// above and the reply below are one undifferentiated wall, which is the state
// this whole feature exists to leave.
//
// Two cells of air each side of the rule, matching the column header — the two
// lines are the same grammar (a label, a rule, the numbers that belong to it)
// applied to the turn in flight and to a turn in the transcript, and a reader
// should not have to notice they are different lines.
func turnRule(n int, meta string, w int, g Glyphs) string {
	return labelRule("turn "+strconv.Itoa(n), meta, w, g)
}

// labelRule is turnRule's construction with the label supplied.
//
// Extracted when the cleared marker needed the same line with a different word.
// One implementation rather than two, because the thing being kept in step is
// the GRAMMAR — a label, a rule, optional numbers, two cells of air each side —
// and a second copy would drift from it one narrow-terminal fix at a time.
func labelRule(label, meta string, w int, g Glyphs) string {
	fill := func(m string) int {
		if m == "" {
			return w - lipgloss.Width(label) - 2
		}
		return w - lipgloss.Width(label) - lipgloss.Width(m) - 4
	}
	n2 := fill(meta)
	if n2 < 1 && meta != "" {
		meta = ""
		n2 = fill(meta)
	}
	if n2 < 1 {
		return label
	}
	s := label + "  " + strings.Repeat(g.Rule, n2)
	if meta != "" {
		s += "  " + meta
	}
	return s
}

// historyMeta is what a past turn reported: how it ended, how long it took, and
// what it cost — on exactly the terms the live chrome states them.
func historyMeta(h TurnRecord) string {
	var parts []string
	// Only a turn that ended badly names its phase. "done" on every separator
	// would be noise on the ordinary case and would make the two that matter
	// harder to spot, not easier.
	if h.Phase == PhaseFailed || h.Phase == PhaseCancelled {
		parts = append(parts, h.Phase.String())
	}
	if h.Elapsed > 0 {
		parts = append(parts, dur(h.Elapsed))
	}
	if h.CostUSD != nil {
		cost := "$" + strconv.FormatFloat(*h.CostUSD, 'f', 4, 64)
		if h.CostSession {
			cost += " session"
		}
		parts = append(parts, cost)
	}
	return strings.Join(parts, "  ")
}

// promptEcho renders the user's own brief inside the column.
//
// It is marked with the SAME glyph the composer uses, because that is where
// these words were typed: the eye that learned "› means you" in the footer
// reads it the same way in the transcript. The glyph carries the distinction
// before the colour does, so it survives --ascii and a monochrome terminal like
// every other distinction in this product.
//
// Sanitized on the way in and NOT redacted, deliberately. Everything else that
// reaches a column is vendor-authored, arriving from a process this room
// spawned, and redaction is the guard on that path. This is the user's own
// typing, echoed back to the user on the user's own screen: covering it would
// hide a secret from the one person who already knows it, while doing nothing
// at all about the copy that was just sent to four vendors. A «redacted» here
// would be theatre — and worse, it would make the echo disagree with what was
// dispatched, which is the one thing this line exists to show.
func promptEcho(prompt string, quoted bool, w int, sty Styles, g Glyphs) []string {
	if prompt == "" {
		return nil
	}
	// Full weight, because in a column of vendor prose the user's own words are
	// the anchor a reader navigates by — the thing you scroll looking for when
	// you want to know what a seat was actually asked. Same treatment as a seat
	// name, for the same reason: both are identity rather than content.
	out := styleAll(hangWrap(g.Prompt+" ", prompt, w), sty.Strong)
	if quoted {
		// What the seat ACTUALLY received on a rebuttal turn is this brief with
		// the other seats' answers fenced in front of it. Those are not the
		// principal's words, so they are reported rather than echoed — the line
		// above stays the user's, and this one says what rode along with it.
		out = append(out, styleAll(
			indentWrap("  ", "+ the other seats' last answers were quoted to this one", w),
			sty.Muted)...)
	}
	return out
}

// pastTurn renders a finished turn: what the seat did, what it said, and how it
// ended.
//
// None of the live cards appear here. "working…" and "this vendor reports no
// incremental output" are claims about a turn in flight, and a turn in the
// transcript is over — rendering either would be the room describing the
// present tense of something that finished ten minutes ago.
func pastTurn(h TurnRecord, w int, sty Styles, g Glyphs) []string {
	var out []string
	for _, a := range h.Acts {
		out = append(out, actLines(a, w, sty, g)...)
	}
	switch {
	case h.Body != "":
		if len(h.Acts) > 0 {
			out = append(out, "")
		}
		out = append(out, wrap(h.Body, w)...)
	case len(h.Acts) == 0 && h.Note == "":
		// It was dispatched to and it said nothing, which is a fact rather than
		// a gap. An empty run of lines here would read as the transcript
		// dropping a turn.
		out = append(out, sty.Muted.Render(padRight("(no reply)", w, g)))
	}
	if h.Note != "" {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, noteCard(h.Note, h.NoteDetail, h.NoteCalm, w, sty, g)...)
	}
	return out
}

// actLines renders one trace entry: what the vendor did, how it went, and — on
// a failure only — the vendor's own first line about why.
//
// The mark goes at the END of the command rather than in front of it, because
// the command is what the eye is scanning for; a leading status column would
// push three different commands to three different indents and make the trace
// harder to read than the thing it replaced.
func actLines(a Act, w int, sty Styles, g Glyphs) []string {
	mark, style := actMark(a.Status, sty, g)
	text := a.Text
	if mark != "" {
		text += " " + mark
	}

	// Wrapped as PLAIN text and styled afterwards, never the other way round:
	// wrap measures with lipgloss.Width but splits on spaces, and an escape
	// sequence pushed through it would be broken across two lines.
	//
	// hangWrap rather than wrap, which is the card grammar §9.11 gave every
	// other card in a column and never gave this one. A `run_command` carrying
	// a Windows path is longer than 37 cells more often than not, and its
	// continuation used to start hard against the column edge — so a wrapped
	// command read as a second, nameless entry, and the outcome mark ended up
	// on a line with nothing on it to say what it was the outcome OF. Hanging
	// it under the ⚙ costs no rows and makes one call look like one call.
	lines := hangWrap(g.Act+" ", text, w)
	if mark != "" && len(lines) > 0 {
		// The mark is the last thing on the last line it landed on. Matched by
		// suffix rather than searched for, so a command that itself contains a
		// "?" or an "x" cannot have part of its own text coloured.
		last := len(lines) - 1
		if strings.HasSuffix(lines[last], mark) {
			lines[last] = strings.TrimSuffix(lines[last], mark) + style.Render(mark)
		}
	}

	// Failure detail only. A successful call's output is not shown at all: the
	// trace is a record of what was done, and pasting every command's stdout
	// into a 37-cell column would bury the answer the room exists to compare.
	if a.Status == runner.ActFailed && a.Detail != "" {
		for _, l := range actDetail(a.Detail, w, g) {
			lines = append(lines, sty.Muted.Render(l))
		}
	}
	return lines
}

// actDetailMaxRows is how much of a vendor's failure reason one trace entry may
// spend.
//
// Three, and the number is argued rather than picked. One is not enough for the
// shape these actually take — "error executing cascade step: …: granting access
// to C:\: Access is denied." is the whole diagnosis and its useful half is at
// the END. Unbounded is what produced the complaint: a raw stderr tail is
// whatever the vendor felt like writing, and one failed call could take the
// column the room exists to read.
const actDetailMaxRows = 3

// actDetail formats a vendor's failure reason for one trace entry.
//
// Two things happen here and both are about not letting raw vendor output
// dictate the shape of the room:
//
// It is FLATTENED. sanitize deliberately preserves newlines, because a vendor's
// prose reply is prose and paragraphs are content. A tool failure's detail is
// not prose — it is a diagnosis line, and multi-line stderr pushed through wrap
// arrives as ragged fragments at random widths, which is the "raw wreckage"
// reading rather than a card. Nothing is lost: the words are all still here, on
// one flowing line the wrapper can measure.
//
// It is BOUNDED, with the ellipsis this product already uses for a clipped
// string, so a clipped detail can never be mistaken for a short one. The clip is
// a real cost and it has a real answer: `f` expands the focused column to the
// full frame, where the same detail wraps into far fewer rows and typically
// survives whole. What this refuses to be is a log viewer — the trace answers
// *what did this agent do and did it work*, and the turn-level failure still
// arrives in the column's own note with the vendor's sentence in it.
// The indent is FOUR, not the two every other card body uses, and that is
// forced by the line above it. Hanging a wrapped command under its ⚙ spends the
// first two cells, so a reason indented two would land at exactly the same
// column as the tail of the command it explains — telling them apart by colour
// alone, which this product does not do. Two cells further is the cheapest thing
// that keeps *what was run* and *why it broke* legible as different claims with
// the styles switched off, and a golden rendered with PlainStyles is precisely
// the case that proves it.
func actDetail(detail string, w int, g Glyphs) []string {
	const indent = "    "
	inner := maxInt(1, w-len(indent))
	lines := wrap(strings.Join(strings.Fields(detail), " "), inner)
	if len(lines) > actDetailMaxRows {
		lines = lines[:actDetailMaxRows]
		last := len(lines) - 1
		// Built with the ellipsis already attached and then truncated, so the
		// mark lands whether or not the last line had room for it. Handed the
		// glyph set rather than a literal: the ASCII ellipsis is ">" and a
		// hardcoded "…" would be the one cell in this entry that did not
		// survive --ascii.
		lines[last] = truncate(lines[last]+" "+g.Ellipsis, inner, g.Ellipsis)
	}
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return lines
}

// actMark is the outcome glyph for one trace entry, and the style it carries.
//
// Pending renders NOTHING, which is the honest render of a call that has not
// come back: any mark at all would be a claim about a result nobody has. The
// three that do render are distinct glyphs BEFORE they are distinct colours, so
// the whole distinction survives --ascii and a monochrome terminal — colour is
// this product's second signal and never its only one.
func actMark(s runner.ActStatus, sty Styles, g Glyphs) (string, lipgloss.Style) {
	switch s {
	case runner.ActOK:
		return g.ActOK, sty.SevOK
	case runner.ActFailed:
		return g.ActFail, sty.SevCrit
	case runner.ActUnknown:
		// Muted, not a severity. Not knowing how a step went is not an alarm,
		// and colouring it as one would train the eye to ignore the real ones.
		return g.ActUnknown, sty.Muted
	case runner.ActDenied:
		// The cross, plus the WORDS — and the words are the distinction, not
		// decoration on it. The vendor echoes a denial back as an is_error
		// tool_result, so a bare cross here would be identical to a tool that
		// broke, and the trace would report the command failing when what
		// happened is that it was refused. "by you" is carried because this is
		// the one line in the trace that is not a reading of a vendor's words:
		// it is the record of a keystroke.
		//
		// SevWarn rather than SevCrit: a refusal is the room working, not
		// something going wrong, and colouring it like a failure would teach the
		// eye to skip the real ones.
		return g.ActFail + " denied by you", sty.SevWarn
	default:
		return "", sty.Text
	}
}

// gateCard is the approval prompt for one column: what is about to happen, and
// the two keys that decide it.
//
// It names the call rather than the tool alone. "Approve Bash?" is not a
// question anybody can answer — the argument line is the entire content of the
// decision, and it is formatted exactly as the activity trace formats it so the
// entry that appears afterwards is recognisably the same call.
//
// Only the OLDEST pending request is shown, with a count of what is behind it.
// Rendering the queue would put several decisions under one pair of keys.
func gateCard(st State, c Column, w int, sty Styles, g Glyphs) []string {
	if w < 12 {
		// Below this the keys and the call cannot both be read, and a card that
		// asks a question nobody can make out is worse than the mode line
		// carrying the whole burden — which it does at every width.
		return nil
	}
	var mine []PendingGate
	for _, p := range st.Gates {
		if p.Vendor == c.Vendor {
			mine = append(mine, p)
		}
	}
	if len(mine) == 0 {
		return nil
	}

	// Same card grammar as every other card in this column: a title at weight,
	// its body hanging under it. The call being decided used to wrap back to the
	// column edge, so the second line of a long path sat flush against the frame
	// and read as a new statement rather than as the rest of the question.
	out := styleAll(hangWrap(g.Warn+" ", "waiting on you: "+mine[0].Text, w), sty.Alert)

	keys := "y approve   n deny"
	if n := len(mine) - 1; n > 0 {
		keys += "   +" + strconv.Itoa(n) + " queued"
	}
	// The keys are repeated here AND in the mode line on purpose. The mode line
	// is the contract — it announces what every key means on every frame — and
	// this is the copy that sits next to the thing being decided, where the eye
	// already is. Indented with the rest of the card's body, because they belong
	// to the question above rather than to the reply below.
	return append(out, styleAll(indentWrap("  ", keys, w), sty.Identity)...)
}

// badgeRow is the seat's claim about itself: what its sandbox actually is, how
// finely it reports, and what it has cost.
//
// Three things about how it is SHAPED, none of which touch what it says. It is
// indented to the seat name above it, so it reads as a property of that seat
// rather than as the first line of the reply — the complaint that started this
// was that these badges looked like debug output, and an unindented row of bare
// lowercase tokens at the top of a column is exactly what debug output looks
// like. The posture badge carries weight when it says this seat can change your
// files (see Styles.ForSandbox). And the cost is right-anchored, because it is
// a number and every other number in this product is right-anchored — which
// also gives the two chrome rows one shape, label on the left and value on the
// right, twice.
//
// Cost renders only when the vendor REPORTED one. A turn that reported zero
// shows $0.0000; a turn that reported nothing shows no cost cell at all. Those
// are different facts, and deriving a figure from token counts is on this
// repo's deliberately-rejected list (design.md §8) — council does not get to
// invent dollars either.
func badgeRow(c Column, w int, sty Styles, g Glyphs) string {
	var plain, styled []string
	if b := c.Sandbox.Badge(); b != "" {
		plain = append(plain, b)
		styled = append(styled, sty.ForSandbox(c.Sandbox.Level).Render(b))
	}
	if s := c.Gran.String(); s != "" {
		plain = append(plain, s)
		styled = append(styled, sty.Muted.Render(s))
	}

	left := strings.Join(plain, "  ")
	leftS := strings.Join(styled, "  ")
	if left != "" {
		left, leftS = "  "+left, "  "+leftS
	}

	cost := costCell(c)
	if cost == "" {
		return leftS
	}
	// Right-anchored when it fits; back to trailing the badges when it does
	// not. What must never happen is the posture claim giving way to the
	// number: §9.2 is emphatic that a claim you cannot see is not a claim, and
	// the cost is the one thing on this line the transcript also records.
	if gap := w - lipgloss.Width(left) - lipgloss.Width(cost); gap >= 1 {
		return leftS + strings.Repeat(" ", gap) + sty.Muted.Render(cost)
	}
	return leftS + sty.Muted.Render("  "+cost)
}

// costCell is the vendor's own figure, and the word that says what it counted.
func costCell(c Column) string {
	if c.CostUSD == nil {
		return ""
	}
	cost := "$" + strconv.FormatFloat(*c.CostUSD, 'f', 4, 64)
	if c.CostSession {
		// A word, not a symbol, and not a colour. A seat kept alive across
		// turns reports its running total; the cell has always meant "this
		// turn" everywhere else in this room, and two different quantities
		// sharing one rendering is the ambiguity §4a.1 forbids.
		cost += " session"
	}
	return cost
}

// reattachCard is what a restored seat says before its first brief.
//
// It replaces "no turn dispatched yet." rather than joining it, because for a
// reattached room that sentence is simply false in the way that matters: no
// turn has been dispatched BY THIS PROCESS, and the vendor on the other end is
// holding a conversation several turns long. A column that opened with the
// same words as a cold room would make the whole feature invisible — the user
// would have no way to tell a successful reattach from a --resume that quietly
// did nothing.
//
// The ROOM half of the news — which turn was last, how stale the save is, where
// it was loaded from — lives once in Notice. Columns used to repeat that
// sentence four times beside each other, which is what made an idle reattach
// read as a spreadsheet of identical paragraphs. What remains here is only
// the per-seat fact: whether THIS seat's thread came back. A room can reattach
// with only some seats restored (no id left, or a vendor installed since), and
// one shared sentence would let either be read as continuing something.
//
// No warning glyph. A reattach is the feature working, not a problem, and
// spending the ⚠ on it would blunt the mark that carries real failures — the
// same argument ActDenied makes for SevWarn over SevCrit.
func reattachCard(_ State, c Column, w int, sty Styles) []string {
	// Plain weight rather than warning colour: same argument as above. Bold so
	// the one line that remains still reads as a card title, not body chrome.
	if c.Restored {
		// "continues it" rather than "resumes it": the resume is the vendor's
		// own mechanism and it has not been asked yet. What the room can promise
		// is where the next brief is addressed.
		return styleAll(wrap(
			"this seat's thread came back. the next brief continues it.", w),
			sty.bold(sty.Text))
	}
	return styleAll(wrap(
		"no thread came back for this seat. its next brief opens a new session, with the brief re-applied.", w),
		sty.bold(sty.Text))
}

// unavailableCard says which failure this is and what would fix it. Absence and
// unusability are different facts and get different words — the HUD's rule that
// a dropped column and an em dash must not read alike (§4a.1), applied here.
//
// It is a CARD now rather than three fragments floating in a column. What was
// there — a warning line, a blank, a wrapped reason at the same indent, a
// blank, a closing sentence at the same indent — had no shape at all: nothing
// on screen said the reason belonged to the title, so a reader scanning a
// three-seat room saw three unrelated paragraphs where one seat had failed. A
// title at full weight with its body hanging under it is the cheapest structure
// a fixed-width column can carry, and it costs no rows.
func unavailableCard(c Column, w int, sty Styles, g Glyphs) []string {
	out := styleAll(hangWrap(g.Warn+" ", c.Label+" is not seated", w), sty.Alert)
	if c.Note != "" {
		out = append(out, styleAll(indentWrap("  ", c.Note, w), sty.Text)...)
	}
	out = append(out, "")
	// Muted, because this is reassurance rather than news: what the reader came
	// to this card for is the line above it.
	out = append(out, styleAll(indentWrap("  ", "the other columns dispatch normally.", w), sty.Muted)...)
	return out
}

// hangWrap wraps text under a leading mark, indenting every continuation line to
// the mark's width so the block reads as one thing hanging off its opener.
//
// The room already did this in two places — the prompt echo indents under its
// "›", a failed call's detail indents under the call — and did not do it in the
// two places a reader most needs it: the warning notes and the unavailable
// card, where a wrapped second line started hard against the column edge and
// looked like a new statement.
func hangWrap(lead, text string, w int) []string {
	inner := maxInt(1, w-lipgloss.Width(lead))
	lines := wrap(text, inner)
	pad := strings.Repeat(" ", lipgloss.Width(lead))
	for i := range lines {
		if i == 0 {
			lines[i] = lead + lines[i]
			continue
		}
		lines[i] = pad + lines[i]
	}
	return lines
}

// indentWrap wraps text at a fixed indent — a card's body under its title.
func indentWrap(indent, text string, w int) []string {
	lines := wrap(text, maxInt(1, w-lipgloss.Width(indent)))
	for i := range lines {
		lines[i] = indent + lines[i]
	}
	return lines
}

// styleAll applies one style to a block of already-wrapped PLAIN lines.
//
// Wrapping first and styling second is the rule everywhere in this file: wrap
// measures with lipgloss.Width but splits on spaces, so an escape sequence
// pushed through it would be broken across two lines.
func styleAll(lines []string, s lipgloss.Style) []string {
	for i := range lines {
		lines[i] = s.Render(lines[i])
	}
	return lines
}

// noteCard is a column's note: the failure reason, the cancellation, the "not
// addressed in turn 2" — and, where there is one, the machinery behind it on a
// quieter line underneath.
//
// One card grammar, the same one every other card in a column uses: a title
// carrying the outcome, its body hanging under it. What a note may not be is a
// paragraph — a single sentence that opens with an outcome and runs on into the
// mechanism wraps to three lines of uniform weight in a 37-cell column, and a
// reader scanning four seats has to read all three to learn which one matters.
//
// The mark carries the hue and the words carry the fact — the same split the
// activity trace's outcome marks make, and the reason a note is legible with
// colour switched off. A CALM note drops the mark entirely rather than
// substituting a quieter one: ⚠ means something went wrong, an outcome that is
// merely news is not that, and a warning glyph spent on news is a warning glyph
// the eye stops trusting. Nothing is lost by dropping it, because the words are
// what carry the note in every glyph set already.
func noteCard(note, detail string, calm bool, w int, sty Styles, g Glyphs) []string {
	var out []string
	if calm {
		out = styleAll(wrap(note, w), sty.bold(sty.Text))
	} else {
		out = hangWrap(g.Warn+" ", note, w)
		if len(out) > 0 {
			if rest, ok := strings.CutPrefix(out[0], g.Warn); ok {
				out[0] = sty.SevWarn.Render(g.Warn) + rest
			}
		}
	}
	if detail != "" {
		// Muted and indented, on the unavailable card's reasoning: the reader
		// came to this card for the line above, and the body is what they read
		// only if the title made them want to.
		out = append(out, styleAll(indentWrap("  ", detail, w), sty.Muted)...)
	}
	return out
}

// tabBar is the narrow-terminal alternative to side-by-side columns.
func tabBar(st State, lay Layout, sty Styles, g Glyphs) string {
	var parts []string
	for _, idx := range st.VisibleColumns() {
		c := st.Columns[idx]
		label := c.Label
		if c.Avail != AvailInstalled {
			label += " " + g.Warn
		}
		// Same two-cell prefix and the same weight the column header gives a seat
		// name, so the tab bar and the header underneath it agree about how a
		// selected seat is drawn rather than each having its own spelling.
		if idx == st.Focus {
			parts = append(parts, sty.Strong.Render(g.Focus+" "+label))
		} else {
			parts = append(parts, sty.Muted.Render("  "+label))
		}
	}
	// fit, not padRight: parts are already styled per tab.
	return " " + fit(strings.Join(parts, "  "), lay.Width-2) + " "
}

func tabBody(st State, lay Layout, sty Styles, g Glyphs) string {
	if len(st.Columns) == 0 {
		return strings.Repeat("\n", maxInt(0, lay.Body-1))
	}
	vis := st.VisibleColumns()
	idx := vis[0]
	for _, v := range vis {
		// Focus is an index into Columns, and a collapsed seat cannot hold it —
		// the tab bar would have no tab to mark and the body would show a column
		// the room says is not on screen.
		if v == st.Focus {
			idx = v
			break
		}
	}
	// seatAddressed, not seatFocused: the tab bar directly above already carries
	// the marker, and a second one on the only visible column is noise. What is
	// NOT suppressed with it is the scroll hint or the name's weight — this is
	// the column the keys address, and this tier is where `f` puts a user who
	// came here specifically to read a long reply. Keeping those two on the same
	// value as the marker is precisely the conflation seatFocus exists to undo.
	cell := columnCell(st, st.Columns[idx], seatAddressed, scrollHint(st, g), lay.ColWidth, lay.Body, sty, g)
	var b strings.Builder
	for i, l := range cell {
		b.WriteString(" " + l + " ")
		if i < len(cell)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// promptWidth is the usable text width inside the compose area.
func promptWidth(width int, g Glyphs) int {
	// The prompt glyph plus its space. One cell in both glyph sets, but measured
	// rather than assumed, because a set that changed it would silently shift
	// every wrap in the composer.
	w := width - 2 - lipgloss.Width(g.Prompt+" ")
	if w < 1 {
		w = 1
	}
	return w
}

// composerRows is how many rows the draft wants, before the height floor.
//
// Derived from the draft in BOTH modes. `esc` keeps the draft — a half-typed
// brief is expensive to retype — so a four-row draft that collapsed to one
// elided line the moment focus left compose would be the room hiding work the
// user has not finished.
func composerRows(st State, g Glyphs) int {
	if st.Draft == "" {
		return 1
	}
	text := st.Draft
	if st.Mode == ModeComposing {
		text += g.Caret
	}
	n := len(wrap(text, promptWidth(st.Width, g)))
	if n < 1 {
		n = 1
	}
	if n > maxComposerRows {
		n = maxComposerRows
	}
	return n
}

// composerLines is the draft, with the caret at the insertion point, as exactly
// lay.Prompt rows.
//
// The caret is static, never blinking: the HUD budgets exactly one moving cell
// on screen and council keeps the same budget — here it is spent on the
// spinner of a column that is actually working.
func composerLines(st State, lay Layout, sty Styles, g Glyphs) []string {
	prefix := g.Prompt + " "
	w := promptWidth(lay.Width, g)

	text := st.Draft
	if st.Mode == ModeComposing {
		text += g.Caret
	}

	if st.Draft == "" && st.Mode == ModeComposing {
		// Short placeholder: the mode line already states routing (`→ everyone`),
		// so repeating "goes to everyone" here was footer noise on an empty draft
		// — the exact chrome the Windows screenshot spent body on.
		return padRows([]string{
			" " + sty.Muted.Render(prefix) +
				sty.Muted.Render(padRight("type a brief"+g.Caret, w, g)) + " ",
		}, lay, w, sty, g)
	}

	// One row is the old behaviour exactly: elide from the LEFT, because the
	// tail is where the cursor is and a prompt that hides the characters just
	// typed would be unusable. It is also what every room that is not mid-
	// paragraph gets, which is why this frame is byte-identical to the one
	// before the composer could grow.
	if lay.Prompt == 1 {
		// A paragraph cannot go in one row, so it is flattened rather than left
		// to put a raw newline into a fixed grid. Unreachable for a real draft —
		// any newline wraps to at least two rows, and the height floor always
		// leaves room for two — and kept as the cheap half of that argument.
		text = strings.ReplaceAll(text, "\n", " ")
		if lipgloss.Width(text) > w {
			text = elideLeft(text, w, g.Ellipsis)
		}
		return padRows([]string{
			" " + sty.Muted.Render(prefix) + sty.Text.Render(padRight(text, w, g)) + " ",
		}, lay, w, sty, g)
	}

	rows := wrap(text, w)
	elided := 0
	if len(rows) > lay.Prompt {
		// The draft is taller than the ceiling. Keep the TAIL, where the cursor
		// is, and spend one row saying how much is above it — the same
		// vocabulary and the same trade the column overflow markers make, for
		// the same reason: silently clipping is indistinguishable from having
		// typed less than you did.
		elided = len(rows) - (lay.Prompt - 1)
		rows = rows[len(rows)-(lay.Prompt-1):]
	}

	out := make([]string, 0, lay.Prompt)
	if elided > 0 {
		// The same words the column overflow marker uses, deliberately: one
		// vocabulary for "there is content you cannot see", wherever it appears.
		out = append(out, " "+sty.Muted.Render(padRight(
			g.Up+" "+strconv.Itoa(elided)+" more above", w+2, g))+" ")
	}
	for i, r := range rows {
		// Continuation rows are indented to the prefix, so a wrapped brief reads
		// as one thing rather than as several.
		p := "  "
		if i == 0 && elided == 0 {
			p = prefix
		}
		out = append(out, " "+sty.Muted.Render(p)+sty.Text.Render(padRight(r, w, g))+" ")
	}
	return padRows(out, lay, w, sty, g)
}

// padRows makes the compose area exactly the height the layout budgeted, so the
// frame is the number of lines the terminal has whatever the draft is doing.
func padRows(rows []string, lay Layout, w int, sty Styles, g Glyphs) []string {
	for len(rows) < lay.Prompt {
		rows = append(rows, " "+sty.Muted.Render("  ")+sty.Text.Render(padRight("", w, g))+" ")
	}
	return rows[:lay.Prompt]
}

// modeLine announces which mode the room is in and what the keys mean in it.
//
// Always visible, never inferred: a mode that changes what an unmodified key
// means without saying so is the failure design.md §7.8 names by name, and
// council has a mode where `q` is the letter q.
// hint is one item on the mode line: a key, and what pressing it does.
//
// Split into two fields so the two can be weighted differently. The footer used
// to be six items of identical weight separated by identical bars — a wall the
// eye slides off, which is why the scroll keys could sit in it for a whole
// release and still be reported as missing. The KEY is what a reader is hunting
// for, so it renders at full intensity and its label recedes; that is the same
// figure/ground split the column header now makes between a seat's name and its
// state, and it costs no cells at all.
//
// A hint with no label is a whole statement rather than a binding — the compose
// line's routing is one — and renders undimmed in full.
type hint struct {
	key, label string
	// alarm puts the key at severity rather than at plain intensity. Exactly one
	// thing uses it: the warning mark in front of a transient notice, which is
	// the same mark-carries-the-hue split the notes and the trace marks make.
	alarm bool
}

// hints renders the mode line's right-hand side twice: once styled, once plain.
//
// The plain copy is not a fallback for want of effort — it is what truncation
// needs. Cutting a string that already carries escapes would cut through one
// (§9.5's trap), and the ellipsis is not optional here: a key list that silently
// lost its tail is a mode line making a promise about keys it no longer names.
func hints(sty Styles, g Glyphs, hs []hint) (styled, plain string) {
	sep := "  " + g.Sep + "  "
	var s, p []string
	for _, h := range hs {
		key := sty.Text
		if h.alarm {
			key = sty.SevWarn
		}
		if h.label == "" {
			s = append(s, key.Render(h.key))
			p = append(p, h.key)
			continue
		}
		s = append(s, key.Render(h.key)+" "+sty.Muted.Render(h.label))
		p = append(p, h.key+" "+h.label)
	}
	return strings.Join(s, sty.Muted.Render(sep)), strings.Join(p, sep)
}

// modeHints is what the keys mean in the mode the room is currently in.
//
// Two of them are dropped in a room with a single seat on screen, and that is
// honesty rather than tidying: `tab` cycles focus between columns and `f`
// expands one column to the width the only column already has, so on a machine
// where three of four seats folded away both keys do exactly nothing. A mode
// line that promises a key which does nothing is the same failure as one that
// hides a key that does — §7.8 forbids the surprise, in both directions.
func modeHints(st State, g Glyphs) []hint {
	several := len(st.VisibleColumns()) > 1
	if st.Mode == ModeComposing {
		// The routing is stated before the keybindings because it is the one
		// thing on this line that changes what enter DOES. An @typo has to read
		// as "this is going to everyone" while there is still time to fix it;
		// discovering it afterwards means a wasted turn against three quotas.
		//
		// Scrolling is named next: a finished turn drops the room into this
		// mode, so this is the line on screen at the moment four long answers
		// land — the one moment the user is certain to want to scroll. Bare
		// arrows because that is the subset compose has: `f`, `g`, `G`, `j`
		// and `k` are letters here.
		//
		// Empty-draft compose keeps the line short on purpose. After a turn
		// lands you are reading, not typing: ^j and ^r are real bindings but
		// naming them beside an empty placeholder was the footer wall the
		// Windows screenshot spent body on. They reappear the moment the draft
		// has any text — §7.8 still forbids hiding a key that does something
		// when you need it; an empty draft does not need newline or rebut yet.
		// enter stays always: it is what the mode is for.
		hs := []hint{
			{key: "→ " + routeLabel(st) + quoteTag(st)},
			{key: "enter", label: "dispatch"},
			{key: g.Up + g.Down, label: "scroll"},
		}
		// `tab` sits immediately after the scroll keys, and its absence here was
		// the concrete half of "i tried scrolling up/down in agy and cursor.
		// could not." §9.10 wired tab into compose so the scroll keys would have
		// something to aim, then named the arrows on this line and not the key
		// that aims them — leaving a mode line that promises scrolling and, in
		// the mode a finished turn drops you into, never says the arrows move ONE
		// column of four.
		//
		// It is offered whenever there is more than one seat on screen, and
		// deliberately NOT gated on whether some column currently overflows. A
		// hint that appeared the moment a reply grew past its column would be a
		// footer cell that changes while output arrives, which §7.1 rule 4 does
		// not budget for — and the promise this line makes is about what the mode
		// can do, not about what the vendors happen to have said.
		if several {
			hs = append(hs, hint{key: "tab", label: "focus"})
		}
		if strings.TrimSpace(st.Draft) != "" {
			// ^j beside enter once there is something to extend: a key that adds
			// a line and a key that spends four quotas sit next to each other on
			// the keyboard. ^r is the other content-changing control.
			hs = append(hs, hint{key: "^j", label: "newline"}, hint{key: "^r", label: "rebut"})
		}
		return hs
	}

	hs := []hint{{key: g.Up + g.Down, label: "scroll"}}
	if several {
		hs = append(hs, hint{key: "f", label: "expand"}, hint{key: "tab", label: "focus"})
	}
	if st.Busy() {
		return append(hs, hint{key: "ctrl+c", label: "cancel"}, hint{key: "?", label: "help"})
	}
	return append(hs, hint{key: "i", label: "compose"},
		hint{key: "?", label: "help"}, hint{key: "q", label: "quit"})
}

func modeLine(st State, lay Layout, sty Styles, g Glyphs) string {
	var left string
	switch {
	case st.Gating():
		// Outranks both other modes, because it is the only state in this room
		// where something is STOPPED until a key is pressed. The notice is not
		// allowed to overwrite this line the way it overwrites the others: a
		// transient message displacing the two keys that unblock a vendor is
		// exactly the surprise §7.8 exists to forbid.
		// The call being decided is the CONTENT of this line, not chrome on it,
		// so it renders at full intensity while the keys that answer it recede
		// to their labels' weight. Everything on this line used to be equally
		// faint, including the path a vendor is about to write to.
		return statusLine(sty.Alert.Render("GATE"),
			[]hint{
				{key: gateLabel(st)},
				{key: "y", label: "approve"},
				{key: "n", label: "deny"},
				{key: "ctrl+c", label: "cancel the turn"},
			},
			lay, sty, g)
	}

	left = "VIEW"
	leftStyle := sty.Strong
	if st.Mode == ModeComposing {
		left = "COMPOSE"
		// Empty compose is the post-turn resting state. Full weight on COMPOSE
		// competed with the routing cell for the same attention the screenshot
		// said the footer was stealing from the body — demote until there is a
		// draft. Gate and notices still outrank this path above.
		if strings.TrimSpace(st.Draft) == "" {
			leftStyle = sty.Muted
		}
	}
	if st.Notice != "" {
		// A notice replaces the keys rather than joining them, and keeps the
		// warning mark at severity while its words stay plain — the same split
		// every other note in this room makes. The mode label keeps leftStyle
		// (muted on empty compose) so COMPOSE does not compete with the notice
		// for the same attention.
		return statusLine(leftStyle.Render(left),
			[]hint{{key: g.Warn, label: st.Notice, alarm: true}}, lay, sty, g)
	}
	return statusLine(leftStyle.Render(left), modeHints(st, g), lay, sty, g)
}

// statusLine lays the mode name against its right-anchored hints, and is the one
// place the two-copy truncation rule lives.
func statusLine(left string, hs []hint, lay Layout, sty Styles, g Glyphs) string {
	styled, plain := hints(sty, g, hs)
	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(plain) - 2
	if gap < 1 {
		gap = 1
		styled = sty.Muted.Render(truncate(plain,
			maxInt(1, lay.Width-lipgloss.Width(left)-3), g.Ellipsis))
	}
	return " " + left + strings.Repeat(" ", gap) + styled + " "
}

// gateLabel names what is waiting, and how much else is behind it.
//
// The count is stated rather than left to the card, because at narrow widths the
// card is the only place a queue is visible and the card lives in one column —
// a user tabbed to a different one would otherwise see "GATE" with no idea how
// many decisions are stacked up.
func gateLabel(st State) string {
	if len(st.Gates) == 0 {
		return ""
	}
	s := st.Gates[0].Text
	if n := len(st.Gates) - 1; n > 0 {
		s += " (+" + strconv.Itoa(n) + " queued)"
	}
	return s
}

// quoteTag marks an armed rebuttal turn in the footer.
//
// It sits beside the routing because both answer the same question — what is
// actually about to be sent — and this one changes the content rather than the
// destination. "(blind)" is shown rather than nothing when armed on turn 1, so
// the user learns the rule at the moment it applies to them instead of
// wondering why the toggle did nothing.
func quoteTag(st State) string {
	if !st.Quote {
		return ""
	}
	if st.Turn == 0 {
		return "  + rebuttal (turn 1 is blind)"
	}
	return "  + rebuttal"
}

// routeLabel names who the current draft is addressed to.
//
// "everyone" rather than a blank or an em dash: this cell always has an answer,
// because a brief with no mention is not an absent routing, it is a routing to
// the whole room. The em dash is reserved for facts the product does not have.
//
// An exclusion renders as "everyone but claude" — the negative form said in the
// positive direction, because what the user needs to check before enter is who
// is about to be BILLED, and "-claude" states the one seat that is not.
func routeLabel(st State) string { return st.Route.label() }

// helpBody replaces the column area, rather than floating over it, for the same
// reason the HUD's overlay does: a panel that covers live output hides the
// thing the user is watching.
//
// It has two pages, and the split is by KIND rather than by length: page one is
// what the keys do, page two is what the words on each column mean. They were
// one panel, and the second one lost — it was four muted lines under the fold,
// which is where the posture explanation was standing when a user asked "why do
// I care that codex and agy are 'unsandboxed'?". Both pages spend the same hard
// 17-row budget, and each ends with the `?` line that leaves it.
func helpBody(st State, lay Layout, sty Styles, g Glyphs) string {
	lines := helpKeys(sty)
	if st.Help == HelpPostures {
		lines = helpPostures(st, lay, sty)
	}

	var b strings.Builder
	for i := 0; i < lay.Body; i++ {
		if i < len(lines) {
			// fit, not padRight: some help lines are pre-styled.
			b.WriteString(" " + fit(lines[i], lay.Width-2) + " ")
		} else {
			b.WriteString(strings.Repeat(" ", lay.Width))
		}
		if i < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// helpKeys is page one: what every key does.
func helpKeys(sty Styles) []string {
	// The budget is HARD, and it is 17 rows rather than the 19 this panel used
	// to spend. Body at a 24-row terminal is 19 with nothing else on screen, but
	// the collapsed-seat notice costs a row and the narrow tier's tab bar costs
	// another — and a machine with a seat that will not run is the ordinary
	// machine, not the edge case. At 19 entries the `?` line, the only
	// documented way back out of this panel, fell off the bottom in exactly that
	// room. Anything added here has to be merged into a line that is already
	// present; two were, to buy the two rows back.
	return []string{
		sty.Identity.Render("council") + sty.Muted.Render(" — one brief, several agents, side by side"),
		"",
		// Merged with the `enter` line it used to sit above. The two described one
		// key in two rows, which is the cheapest row in the panel to buy back.
		"  i / enter    compose a brief; enter dispatches — to everyone, or whoever is @mentioned",
		"  ctrl+j       newline in the brief — the compose area grows to six rows",
		// Down from three rows to two. What went is "the others are review, IDE
		// and tiebreak lanes" — which explains why the fleet is shaped this way
		// rather than what a key does, and it is in the README and ADR-010 where
		// that argument belongs.
		//
		// The exclusion form was folded into the SAME two rows rather than given
		// a third: this panel's budget is hard (17 rows, above), and a feature
		// that pushed the `?` line off a 24-row terminal would have bought
		// discoverability for one thing by taking away the way out of the panel.
		// The commas between the aliases went to pay for it — they were never
		// typed anyway. What is deliberately NOT here is the mixing refusal:
		// that one announces itself in the footer while the line is still being
		// typed, and again as a notice on enter, so it is the one rule on this
		// list that does not need a row to be discovered.
		"  @codex       narrow to a lane: @claude @codex @agy @cursor; -@codex excludes",
		"               one. Unaddressed goes to every seat. Leading only: \"ask @claude\" is prose",
		// One line, like pgup/pgdn below and for the same reason: the panel has
		// to fit a 24-row terminal with q and ? still on screen.
		//
		// `c` shares the row rather than taking a new one, and the merge is the
		// honest shape rather than only a saving: these are the two controls that
		// change the ROOM from inside it instead of addressing the vendors, which
		// is the distinction design.md §9.17 turned into a rule. The budget is
		// hard (17 rows, above) and a row added here pushes the `?` line — the
		// only documented way out of this panel — off a 24-row terminal.
		"  /cd <dir>    move the room to another repo; c clears the focused seat's thread (y confirms)",
		// One row for three keys, and the merge is the honest shape rather than
		// a saving. The panel's budget is hard (17 rows, above) and yank had to
		// land inside it — a copy key documented below the fold is a copy key
		// nobody finds — but the real reason these belong together is that they
		// COLLIDE. `y` means two things depending on whether a vendor is
		// blocked, gateKey resolves it, and the one place a reader could learn
		// that is the line that names both. Splitting them into two rows would
		// have spent a row to make the collision harder to see.
		"  y / Y        copy this seat's reply, or the whole turn — while a gate waits, y/n answer it",
		"  esc          leave compose (the draft is kept)",
		// The "in compose too" clauses are the whole of this change on this
		// panel. These keys always worked; what no one could find out is that
		// they now work in the mode a finished turn drops you into — which is
		// the mode you are in when there is finally something long to read.
		"  tab          move focus between columns — in compose too",
		"  ↑ ↓ / j k    scroll the focused column's whole transcript — ↑ ↓ in compose too",
		"  pgup/pgdn    scroll by a screenful, in compose too (space = pgdn in view mode);",
		"               g / G jump to the first turn or the newest",
		"  f            expand the focused column to the full width (in compose, f is text)",
		"  ctrl+r       arm rebuttal: vendors see the others' answers, quoted as untrusted",
		// Two keys on one line, because this panel has to fit a 24-row terminal
		// and the line they were competing with is "? this help" — which toggles,
		// and is therefore the only documented way back out of here.
		"  ctrl+c / q   ctrl+c cancels the turn, or quits when idle; q quits (in compose it is text)",
		// `?` no longer toggles, it CYCLES, so this line has to say where the
		// next press goes or the second page is a feature nobody finds. It is
		// still the only documented way out of the panel — three presses always
		// return the room — which is why it keeps this row on both pages.
		"  ?            next page: what the badge on each column means",
		"",
		// One complete sentence per line, because the two rows freed above land
		// exactly here at a 24-row terminal: a paragraph that wrapped would show
		// its first half and cut mid-clause, which reads as the panel being
		// broken rather than as it having more to say further down.
		"  a seat that cannot be driven folds out of the grid, named in one line above.",
		"  --vendor all keeps every seat on screen; --vendor claude,codex seats those.",
		"",
		// What used to be here was four muted lines trying to explain the posture
		// badges below the fold, and it was the wrong shape for the job: a
		// paragraph nobody scrolls to, on the one subject in this room where a
		// misreading has consequences. It moved to its own page, which had the
		// room to say it in plain English, and this is the pointer that makes
		// that page findable from the panel people actually open.
		sty.Muted.Render("  council dispatches to vendor CLIs — the one telltale mode that does, and"),
		sty.Muted.Render("  each column states its OWN posture rather than the room claiming one for"),
		sty.Muted.Render("  every seat. Press ? for what those posture badges mean."),
	}
}

// helpBadgeGloss is one plain-English sentence per badge, keyed by the level
// that renders it.
//
// It is a table rather than prose so that TestEveryBadgeIsExplained can walk
// every SandboxLevel and fail the build when a badge exists with nothing here
// to say what it means — which is the failure this page was added to fix, and
// the kind that comes back the day a sixth posture lands.
//
// NOTHING HERE MAY WEAKEN A CLAIM. These are glosses on the badge words, not
// replacements for them: `unsandboxed` still says nothing restricts the vendor,
// `ro:requested` still admits it was never observed, and the gloss's job is to
// answer "so what?" rather than to make either sit more comfortably. The
// detailed, per-seat, measured version is below on the same page.
func helpBadgeGloss() []struct {
	level SandboxLevel
	gloss []string
} {
	return []struct {
		level SandboxLevel
		gloss []string
	}{
		{SandboxTools, []string{
			"the write tools are ABSENT from that session — checked",
			"against what the session reported about itself, not a flag",
		}},
		{SandboxEnforced, []string{
			"the vendor's own OS-level sandbox does it — codex, mac/linux",
		}},
		{SandboxRequested, []string{
			"a flag was passed and accepted; what it actually enforces",
			"was never observed. Weaker than the two above, and says so",
		}},
		{SandboxNone, []string{
			"nothing restricts this vendor at the OS level. MEASURED,",
			"not assumed — treat this column as able to change your files",
		}},
		{SandboxWrite, []string{
			"--write: this column may edit and run things in the workspace",
		}},
		{SandboxGated, []string{
			"--write, and this seat asks first — y approves, n denies",
		}},
	}
}

// helpGranGloss is the other half of a column's badge line, in plain English.
//
// The sandbox badge got a legend in §9.13 and the granularity word beside it did
// not, which left the room saying `final only` on two of three columns with
// nothing anywhere to say what that means. It was covered for a while by the
// waiting card reciting the whole explanation in the body of every waiting turn
// — which is exactly the wiring §9.14 pulled out of the reading area, and pulling
// it out is what made this legend owed rather than merely nice.
//
// Keyed by Granularity, so TestEveryGranularityIsExplained can walk the type and
// fail the build when a value renders a word with nothing here to define it —
// the same guard TestEveryBadgeIsExplained gives the sandbox levels, for the
// same reason: the gap it closes is the kind that comes back the day a fifth
// value lands.
//
// It renders inside each SEAT's block rather than as a room-independent legend,
// and that is a deliberate departure from how the sandbox words are presented
// one section above. §9.13's argument for a legend covering badges this room
// does not show is that a user who has never typed --write should be able to
// learn what WRITES means BEFORE they type it. There is no equivalent here:
// nobody chooses a granularity. It is a property of whichever vendors are
// installed, so the only granularity words a reader can ever meet are the ones
// their own room is already displaying — and putting the sentence beside the
// word it defines beats making them match two lists.
//
// GranUnknown is included and prints NO badge word, which is not an oversight —
// it is the fifth amendment's ruling that a column whose granularity was never
// established must not borrow the word two vendors earned by measurement. It
// needs an entry precisely because of that: "this column has a blank where the
// others have a word" is the one case a reader cannot look up by reading the
// word.
func helpGranGloss() map[Granularity]string {
	return map[Granularity]string{
		GranTokens: `"tokens" — text arrives as the vendor writes it, so you are watching ` +
			`it land rather than waiting for it`,
		GranEvents: `"events" — progress arrives as whole messages or steps rather than as ` +
			`text, so this column moves in jumps`,
		GranFinalOnly: `"final only" — MEASURED: this vendor sends nothing at all until its ` +
			`turn is done, so a quiet column here is a working one and not a stalled one`,
		GranUnknown: `no granularity word — whether this vendor reports anything before it ` +
			`finishes has never been established, so the column opens waiting and the first ` +
			`real output promotes it. A claim earned rather than assumed`,
	}
}

// helpPostures is page two: what the badge on each column means.
//
// The top of the page is a legend of every badge word this product can render,
// not only the ones this room happens to show. A user who has never typed
// --write should be able to find out what WRITES means BEFORE they type it, and
// a room-specific legend could only ever explain the room you are already in.
//
// Below the legend, and below the fold at a 24-row terminal, is this room's own
// seats with the full claim each one is making — the first time SandboxClaim
// .Detail has been rendered anywhere. That ordering is deliberate: the detail is
// unreadable without the vocabulary, and the vocabulary fits the budget while
// four paragraphs of measured prose never could. It is the same trade page one
// makes with its closing paragraph, and the honest residual is the same: at the
// shortest terminal this room will draw in, the per-seat half is scrolled past
// rather than absent.
func helpPostures(st State, lay Layout, sty Styles) []string {
	lines := []string{
		sty.Identity.Render("council") + sty.Muted.Render(" — what the badge on each column means"),
		"",
		"  Each column states its own posture; there is no room-wide claim.",
		"",
	}

	for _, e := range helpBadgeGloss() {
		b := SandboxClaim{Level: e.level}.Badge()
		// The badge renders in the SAME style it wears on a column, from the
		// same function, so the legend cannot teach one weight and the room
		// show another. `unsandboxed` and `WRITES` are loud here too.
		head := sty.ForSandbox(e.level).Render(b) + strings.Repeat(" ", maxInt(1, 13-len(b)))
		lines = append(lines, "  "+head+sty.Text.Render(e.gloss[0]))
		for _, g := range e.gloss[1:] {
			lines = append(lines, "               "+sty.Text.Render(g))
		}
	}

	lines = append(lines,
		"",
		// The load-bearing sentence on this page, and the reason the page is not
		// just a glossary. Every badge above is a claim about a FLAG; none of
		// them is what keeps this room from touching something it should not.
		"  What contains this room is the WORKSPACE above, not any of these words.",
		"  Point council at a throwaway worktree when that matters.",
		"  ?            close",
	)

	// Below the fold at the minimum height. Same reasoning as page one's closing
	// paragraph, and it buys something page one's did not: this is the measured,
	// per-seat argument behind each badge, which is what answers "why is THIS
	// column unsandboxed" rather than "what does the word mean".
	var seats []string
	for _, c := range st.Columns {
		b := c.Sandbox.Badge()
		if !st.seats(c) || b == "" || c.Sandbox.Detail == "" {
			continue
		}
		// Padded to the legend's own column so the seat names line up under one
		// another and the badges line up with the words they were just defined
		// by. Two lists of the same vocabulary that do not share a left edge
		// read as two unrelated lists.
		seats = append(seats, "",
			"  "+sty.ForSandbox(c.Sandbox.Level).Render(b)+
				strings.Repeat(" ", maxInt(1, 13-len(b)))+sty.Muted.Render(c.Label))
		body := maxInt(20, lay.Width-8)
		for _, l := range wrap(c.Sandbox.Detail, body) {
			seats = append(seats, sty.Muted.Render("      "+l))
		}
		// The other half of that seat's badge line, and the reason this section
		// grew: §9.14 took the granularity explanation out of the body of every
		// waiting turn, where it had been standing in for a legend that did not
		// exist. The claim stays on the column — `waiting` and `final only` are
		// drawn on every frame — and the argument for it moved here.
		//
		// Under the posture rather than beside it, because they answer different
		// questions about the same seat and the posture is the one with
		// consequences. A seat is never skipped for having no granularity word:
		// the blank IS the claim (fifth amendment), and it is the one a reader
		// cannot decode by reading the header.
		if g, ok := helpGranGloss()[c.Gran]; ok {
			for _, l := range wrap(g, body) {
				seats = append(seats, sty.Muted.Render("      "+l))
			}
		}
	}
	if len(seats) > 0 {
		lines = append(lines, "", sty.Text.Render("  this room, seat by seat:"))
		lines = append(lines, seats...)
	}
	return lines
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
