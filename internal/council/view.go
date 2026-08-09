package council

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
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
		b.WriteString(fit(framePadStr+noticeLine(st, sty, g, st.Width-2*framePad), st.Width))
		b.WriteString("\n")
	}
	// The live turn's brief, once, above the columns that were asked it (§9.30).
	// It sits under the notice for the same reason the notice sits under the rule:
	// the collapsed-seat line is a fact about the ROOM and this is a fact about
	// the turn, and the room is the larger subject. Both are chrome, and
	// resolveLayoutIn has already spent both out of one budget.
	if lay.Band > 0 {
		for _, l := range bandLines(st, st.Width-2*framePad, sty, g) {
			b.WriteString(fit(framePadStr+l, st.Width))
			b.WriteString("\n")
		}
	}

	if st.Help != HelpClosed {
		b.WriteString(helpBody(st, lay, sty, g))
	} else if st.Page.Open {
		// The by-turn page outranks the tier branch rather than living inside
		// one, because it is a PROJECTION of the transcript and not a width
		// breakpoint (§9.22). What it does share with the tabs tier is the
		// geometry — one reading area at the full frame — which layoutFor
		// resolves for it rather than a second layout path inventing one.
		b.WriteString(pageBody(st, lay, sty, g))
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
	cols, primary := len(vis), framePrimary(st, vis)
	if st.Page.Open {
		// A turn page is ONE reading area at the full frame, so it plans as one
		// column — which is the tabs tier's own arithmetic, already written and
		// already swept by the frame matrix. Reusing it is what keeps the height
		// budget, the 60-column floor, the composer's growth and the
		// collapsed-seat notice identical in both projections; a second layout
		// path for a surface that IS a column at full width would be a second
		// place for the frame to tear (§9.22).
		//
		// The frame owners go with it. FrameOwners apportions width between
		// seats, and there are no seats side by side here for it to apportion.
		cols, primary = 1, nil
	}
	// The band is asked for only when the body is the GRID. A turn page already
	// renders the brief once — that is half of what it is for (§9.22) — and the
	// help panel replaces the column area outright, so a band above it would be
	// chrome describing content that is not on screen. Both are answered here
	// rather than inside bandLines, so the content rule and the "which body is
	// this" rule stay in the two places that own them.
	band := 0
	if st.Help == HelpClosed && !st.Page.Open {
		band = bandRows(st, g)
	}
	return resolveLayoutIn(layoutInput{
		Width:    st.Width,
		Height:   st.Height,
		Cols:     cols,
		Expanded: st.Expanded,
		Composer: composerRows(st, g),
		Notice:   collapsedNotice(st, g) != "",
		Primary:  primary,
		Band:     band,
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

// rule is the frame's own edge: the full-bleed line under the header and the
// one above the composer. Together they are the only closed shape on screen, so
// they draw at the HEAVY weight and everything inside them stays light (§9.26).
//
// It is still `Rule()`, i.e. muted, and that is deliberate. Weight says which
// line is the outline; intensity would say the outline matters more than the
// content it bounds, which is the trade §9.23 refused when it declined to let
// the rails' hue mean anything.
func rule(w int, sty Styles, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	return sty.Rule().Render(strings.Repeat(g.RuleHeavy, w))
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
	hop := ""
	if st.FlowSteps > 0 {
		hop = "  " + g.Sep + "  hop " + strconv.Itoa(st.FlowHop) + "/" + strconv.Itoa(st.FlowSteps) + " @" + string(st.FlowVendor)
		// `s` armed: the chain ends after this hop, and the promise lives on the
		// marker rather than only in the notice that announced it — a notice
		// scrolls away while the armed state persists, the WRITE badge's own
		// argument one line up. Words, not a glyph, so it survives --ascii, and
		// on the hop cell itself because it is a fact ABOUT the hop: this one
		// runs, its successors do not (§9.35).
		if st.FlowStop {
			hop += " (stops here)"
		}
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
	// The live turn's destination, on the cell that already names the turn:
	// "turn 10 → everyone". It is the room's one moving fact that has no other
	// home while it is true — the composer's routing cell has already been
	// cleared and refilled with the next draft's default, and each column's
	// transcript does not record participation until the turn lands (§9.21).
	//
	// The route's OWN label(), never a second vocabulary: what the header prints
	// is what would have to be typed to produce it, so a reader can read the
	// header and then reproduce the turn. The arrow is likewise the literal one
	// the composer's routing cell uses rather than a Glyphs entry — the two are
	// the same statement about the same turn a keystroke apart, and giving the
	// header its own glyph would let one fact drift into two spellings.
	//
	// A /flow hop states no route, and that is the same rule the shedding below
	// runs on rather than an exception to it: a hop goes to exactly one named
	// seat (§9.16) and the cell immediately to the right already says which.
	// The route is attached to the turn number and BEFORE the hop cell, so the
	// arrow can never read as pointing at the hop.
	rightZone := func(withRoute bool) string {
		r := round
		if withRoute && st.TurnRoute != nil && st.FlowSteps == 0 {
			r += " → " + st.TurnRoute.label()
		}
		return sty.Muted.Render(r + hop + "  " + g.Sep + "  " + seated + "  " + g.Sep + "  " + brief)
	}

	// The path takes whatever is left, elided from the left because the
	// uninformative part of a path is its prefix. It is introduced by the same
	// "  │  " the HUD's header uses between its own zones, so the room's name
	// and the directory it dispatches into read as two facts rather than as one
	// run-on label — which is what a bare space made them.
	sep := "  " + sty.Rule().Render(g.Sep) + "  "
	pathWidth := func(r string) int {
		return lay.Width - lipgloss.Width(left) - lipgloss.Width(r) - 2*framePad - lipgloss.Width(sep)
	}

	// The route sheds BEFORE the workspace and before the seated/briefed counts,
	// and the ordering is a rule rather than a preference: a fact with a home
	// elsewhere yields to facts that have none. The route is on screen in the
	// composer a keystroke earlier and in the transcript a moment later; the
	// path is nowhere else at all, and it is the one that changes what the
	// agents can see. So the route is added only when it costs nothing that was
	// already here — the path keeps its cells if it had them, and the line
	// keeps its gap if it did not.
	right := rightZone(true)
	if right != rightZone(false) {
		bare := rightZone(false)
		// The path was on screen without the route, so it has to still be on
		// screen with it. Where there was no room for a path either way, the
		// only question left is whether the counts still fit beside the room's
		// own name.
		affordable := pathWidth(right) > 3
		if pathWidth(bare) <= 3 {
			affordable = lay.Width-lipgloss.Width(left)-lipgloss.Width(right)-2*framePad >= 1
		}
		if !affordable {
			right = bare
		}
	}

	pathw := pathWidth(right)
	mid := ""
	if pathw > 3 {
		mid = sep + sty.Muted.Render(elideLeft(displayPath(st), pathw, g.Ellipsis))
	}

	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right) - 2*framePad
	if gap < 1 {
		gap = 1
	}
	return framePadStr + left + mid + strings.Repeat(" ", gap) + right + framePadStr
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
		return sty.SevWarn.Render(g.Warn) + noticeProse(st, rest, sty)
	}
	return noticeProse(st, n, sty)
}

// noticeProse renders the collapsed-seat sentence Muted, with each named seat in
// that seat's own hue (§9.28).
//
// The NAMES only. The mark keeps SevWarn because the notice is a warning, the
// reason in parentheses and the remedy after the bar stay chrome, and nothing
// here gains weight — this is a sentence, and a sentence with four bold words in
// it is not one. §9.25's boundary is untouched: the two-letter TAG stays off
// prose, because an abbreviation introduced mid-sentence is where nothing is
// being compared. A hue is not an abbreviation; it costs no cell and asks the
// reader to learn nothing they have not already learned from the tab bar.
//
// Scanned in CollapsedColumns' order, which is the order collapsedNotice writes
// them in, so one forward walk finds each name once. A name that did not survive
// truncation is simply not found and the text stays muted — the honest outcome,
// and the reason this cannot resurrect a clipped word.
func noticeProse(st State, text string, sty Styles) string {
	var b strings.Builder
	rest := text
	for _, i := range st.CollapsedColumns() {
		c := st.Columns[i]
		k := strings.Index(rest, c.Label)
		if k < 0 {
			continue
		}
		b.WriteString(sty.Muted.Render(rest[:k]))
		b.WriteString(sty.SeatIdentity(c.Vendor).Render(c.Label))
		rest = rest[k+len(c.Label):]
	}
	b.WriteString(sty.Muted.Render(rest))
	return b.String()
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
	// Two cells of air each side of the bar, not one. Every other │ in this
	// product — the room header, the mode line, the gutters between columns —
	// is spaced that way (§9.11 argues the number from --ascii, where the rule
	// glyph and the spinner's first frame collide at one cell), and this was the
	// single place spelling the room's one separator a second way.
	return g.Warn + " " + lead + strings.Join(parts, ", ") +
		strings.Repeat(" ", gutter) + g.Sep + strings.Repeat(" ", gutter) +
		"--vendor all seats them anyway"
}

// columnsBody draws the seats side by side.
//
// The vertical separators run in BANDS rather than row by row — see railRows,
// which owns the rule and the argument for it. Gutters stay blank-width either
// way so the grid does not shear.
//
// One rail in that band is heavier than the rest: the one immediately LEFT of
// the focused column (§9.27). It is a columns-tier device only — the tabs tier
// has one column on screen and a tab bar already carrying the marker, so a rail
// there would mark the only thing there is.
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
	// The focused column's LEFT rail thickens (§9.27). Same cell, same width, one
	// glyph heavier — so the mark is as tall as the column it describes, which is
	// the one thing `▸` and the name's weight cannot be: both of those sit on the
	// header row, and a reader forty rows into a transcript has scrolled past
	// them. The mark is Muted like every other rail, because a rail that also
	// changed hue would be chrome competing with the content it bounds (§9.23).
	focusSep := sepPad + sty.Rule().Render(g.FocusRail) + sepPad
	blankSep := strings.Repeat(" ", lipgloss.Width(plainSep))
	// The LEFTMOST column has no gutter to its left, so the frame's own left pad
	// carries the mark for it — cell one of framePad's two, which puts one cell of
	// air between the mark and the column exactly as the gutter's own arithmetic
	// would if there were room for two. Without this a focused seat in position
	// zero would be the one seat the device could not mark, which is the kind of
	// hole that teaches a reader to stop trusting a signal.
	focusPad := sty.Rule().Render(g.FocusRail) + strings.Repeat(" ", framePad-1)
	rails := railRows(cells, lay.Body)
	var b strings.Builder
	for row := 0; row < lay.Body; row++ {
		// The rail rides §9.23's band and nothing more: it marks the rows the thin
		// rail would have marked, so focus never asserts a grid over emptiness and
		// the frame's edge never changes shape when the keys move.
		lead := framePadStr
		if rails[row] && len(vis) > 0 && vis[0] == st.Focus {
			lead = focusPad
		}
		b.WriteString(lead)
		div := blankSep
		if rails[row] {
			div = sep
		}
		for j := range vis {
			if j > 0 {
				d := div
				if rails[row] && vis[j] == st.Focus {
					d = focusSep
				}
				b.WriteString(d)
			}
			b.WriteString(cells[j][row])
		}
		b.WriteString(framePadStr)
		if row < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// railRows decides, for the whole frame at once, which body rows carry the │
// between columns.
//
// **A row carries a rail when some column has content on it, or when it is a
// LONE blank row with content above and below.** Two consecutive blank rows end
// the band; the next word starts a new one.
//
// The rule this replaces tested each row on its own — a rail wherever some
// column had ink on that line, nothing anywhere else. It was answering a real
// question, one row at a time. The question was the tall idle window that drew
// │ down through every empty row to the footer, four spears through a void, and
// that half is unchanged here: corpus-idle-120x60 still has a bare middle, and
// TestRailsStopThroughEmptyBody still fails the build if it does not.
//
// What the per-row form ALSO did, unasked, was punch a hole in the frame every
// time three transcripts of different lengths happened to be blank on the same
// line. transcript.txt broke at rows 11 and 13, skips-coalesced.txt at 5, 10 and
// 13, unavailable.txt at 19. Those are not voids — they are the air §9.11
// deliberately spends, and every one of them is a single row. A frame whose edge
// dashes in and out at the exact rows the design put air in reads as damage, and
// it made the rail look like a property of the prose rather than of the grid.
//
// **One row is the whole threshold, and it is the room's own number rather than
// a tuned one.** Every deliberate blank this surface draws is exactly one row,
// three times over (§9.11): between a seat's chrome and its content, where the
// speaker changes, where the kind of content changes. A one-row gap is therefore
// a boundary the design placed BETWEEN two things it means to keep together, and
// bridging it is drawing what was meant. Two rows is nothing the design asked
// for — the bottom-anchor pad, an idle room, a column that ran out of transcript
// long before its neighbour did — and a separator has nothing to separate there.
//
// The alternative considered and rejected was the literal reading: rails on
// every row from the frame's first word to its last. It is a simpler sentence
// and it produces a worse room — an idle 120x60 frame has chrome at the top and
// `no turn dispatched yet.` at the bottom, so a single span would run fifty-five
// rows of bar through nothing at all, which is precisely the shape Phase 2
// removed. Contiguity is worth having up to the point where it starts asserting
// a grid over emptiness.
//
// TrimSpace, not len: every cell is padded to its column width, so a blank row
// is a run of spaces rather than an empty string.
func railRows(cells [][]string, rows int) []bool {
	ink := make([]bool, rows)
	for _, col := range cells {
		for row := 0; row < rows && row < len(col); row++ {
			if strings.TrimSpace(col[row]) != "" {
				ink[row] = true
			}
		}
	}
	out := make([]bool, rows)
	copy(out, ink)
	// A lone blank between two content rows is air, not a void. Bounded on both
	// sides on purpose: a single blank row hanging off either end of the frame
	// has content on one side only and stays bare, so the band never overshoots
	// the last word by a row.
	for row := 1; row < rows-1; row++ {
		if !ink[row] && ink[row-1] && ink[row+1] {
			out[row] = true
		}
	}
	return out
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

	// The CHROME above renders with the room's own set and the body with the
	// seat's, which is the whole shape of §9.27's demotion. A posture badge, a
	// gate card and a seat's name are claims about the seat rather than reading
	// material, and a claim that faded because the reader was looking at the next
	// column is the failure §9.2 wrote the badge row to prevent.
	body, anchors := columnLines(st, c, w, sty.forSeat(f), g)
	avail := h - len(chrome)
	win, above, below := scrollWindow(c, body, avail)

	// The turn coordinate rides on the column the keys move and on no other,
	// which is §9.12's rule about hints applied to a fact: an unfocused marker
	// is already spending its cells on `tab to focus`, the one thing a reader
	// looking at that column can act on, and a turn number in front of it would
	// crowd the answer to make room for the address.
	turnUp, turnDown := "", ""
	if f.hasKeys() {
		turnUp = turnLabel(anchors, above-1)
		turnDown = turnLabel(anchors, above+len(win))
	}

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
				overflowMarker(g.Up, above, "above", turnUp, hint, w, g), w, g)))
		case i == len(win)-1 && below > 0:
			// The hint is named once per column. On a column with content hidden
			// both ways it has already been said above, and saying it twice in
			// one cell is the kind of noise that makes the count harder to find.
			//
			// The turn coordinate is NOT the same case and is repeated: the two
			// markers name two different turns, so the second one is a fact this
			// cell is the only place to learn rather than a second copy of the
			// first.
			h := hint
			if above > 0 {
				h = nil
			}
			bodyLines = append(bodyLines, sty.Muted.Render(padRight(
				overflowMarker(g.Down, below, "below", turnDown, h, w, g), w, g)))
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

// overflowMarker is the "there is more" line, plus which turn is behind it and
// the keys that would reach it.
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
//
// `turn` is the transcript coordinate the count needs to be useful (§9.20): at
// "↑ 509 more above" nobody counts lines, and the number a reader can act on is
// the one `[` and `]` move by. It is the LOWEST-priority element on this line
// and therefore the first to shed — below even `f expand`, because it says
// where you are while the hints say what you can do about it, and a marker that
// dropped a key to keep a coordinate would be §9.10's trade run backwards.
//
// The marker's OWN WORDS shed last, and they shed whole (§9.18). `↑ 12 more
// above` is fifteen cells against a strip's fourteen, so it used to reach fit()
// and come back as `↑ 12 more abov` — a line telling a reader something is
// hidden, in a word that is not one. `more` goes first, because the count in
// front of it already says how much; `above` and `below` go last, because they
// are the only part that says which way to press. The count never goes at all.
func overflowMarker(mark string, n int, where, turn string, hints []string, w int, g Glyphs) string {
	count := mark + " " + strconv.Itoa(n)
	// Longest first, like every shedding list in this file. A width that fits
	// the full form never reaches the shorter ones, so nothing above strip width
	// renders differently than it did.
	forms := []string{count + " more " + where, count + " " + where, count}
	sep := "  " + g.Sep + "  "
	for _, s := range forms {
		if lipgloss.Width(s) > w {
			continue
		}
		fits := func(tail string) bool {
			return lipgloss.Width(s)+lipgloss.Width(sep)+lipgloss.Width(tail) <= w
		}
		// Widest first, and the turn coordinate only ever rides on the widest
		// hint form. Pairing it with a SHORTER hint would let it survive a width
		// that cost the room a key, which is the shedding order this comment
		// forbids.
		if turn != "" {
			tail := turn
			if len(hints) > 0 {
				tail = turn + sep + hints[0]
			}
			if fits(tail) {
				return s + sep + tail
			}
		}
		for _, h := range hints {
			if fits(h) {
				return s + sep + h
			}
		}
		return s
	}
	// Narrower than the count itself. padRight clips it and says so, which is
	// the only honest answer left at a width that cannot hold a number.
	return forms[len(forms)-1]
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
	return scrollWindowAt(c.Scroll, c.Follow, body, avail)
}

// scrollWindowAt is scrollWindow with the two fields named rather than carried
// on a Column.
//
// Split out when the by-turn page needed the same window over a list that is not
// a column's (§9.22). One implementation, because "which slice is visible" is
// the contract the overflow markers, MaxScroll and every hop key are clamped
// against — and a second copy would agree with the first until the day one of
// them learned to clamp differently.
func scrollWindowAt(scroll int, follow bool, body []string, avail int) (win []string, above, below int) {
	if avail <= 0 {
		return nil, 0, 0
	}
	if len(body) <= avail {
		return body, 0, 0
	}

	max := len(body) - avail
	off := scroll
	if follow {
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
	lines, _, avail, ok := columnViewport(st, idx)
	if !ok {
		return 0
	}
	if m := len(lines) - avail; m > 0 {
		return m
	}
	return 0
}

// columnViewport resolves what one column is actually being drawn into: its
// transcript, where each turn starts in it, and how many body rows survive the
// chrome. ok is false for a seat that is not on screen.
//
// Factored out when the hop keys needed the same four numbers MaxScroll needed.
// Two derivations of "how tall is this column's content at this width" is
// exactly the drift the columnLines comment refuses, one level up: a hop that
// measured the transcript differently from the clamp applied to it would land
// off the end of the content only in the geometries nobody tests.
func columnViewport(st State, idx int) (lines []string, anchors []turnAnchor, avail int, ok bool) {
	if idx < 0 || idx >= len(st.Columns) {
		return nil, nil, 0, false
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
		return nil, nil, 0, false
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
	avail = lay.Body - len(columnChrome(st, st.Columns[idx], seatUnfocused, w, sty, gl))
	lines, anchors = columnLines(st, st.Columns[idx], w, sty, gl)
	return lines, anchors, avail, true
}

// TurnHop is where `[` and `]` land the focused column: the scroll offset that
// puts the neighbouring turn's separator on the viewport's top row.
//
// Exported for the program loop beside MaxScroll, and for the same reason —
// the keystroke is clamped against the geometry the renderer resolved, never
// against one the loop kept its own copy of.
//
// `cur` is the offset the column is reading at now. Backwards is the audio
// player's previous-track rule: from the middle of a turn it lands on THAT
// turn's head, and only a second press reaches the one before it. Written as
// "the last head strictly above where we are", which produces both cases
// without a special one — and never wraps, because a transcript has a first
// turn and pretending otherwise would make `[` at the top a jump to the end.
//
// ok is false when there is nothing in that direction. The caller decides what
// that means: backwards it is a no-op, forwards it is the tail (§9.20), which
// is `G`'s answer to the same question rather than a second one.
func TurnHop(st State, idx, cur, dir int) (int, bool) {
	_, anchors, _, ok := columnViewport(st, idx)
	if !ok {
		return 0, false
	}
	if dir < 0 {
		for i := len(anchors) - 1; i >= 0; i-- {
			if anchors[i].Off < cur {
				return anchors[i].Off, true
			}
		}
		return 0, false
	}
	for _, a := range anchors {
		if a.Off > cur {
			return a.Off, true
		}
	}
	return 0, false
}

// turnAt names the turn a line belongs to: the last turn that started at or
// before it, which is 0 when nothing has started yet.
//
// "At or before" is the whole of the semantics, and it is chosen so the
// coordinate on an overflow marker cannot lie about a turn that is only half
// hidden (§9.20). The marker asks about the line immediately outside the fold,
// so what it gets back is the turn that line is part of — which is the turn the
// user is reading when a long reply runs off the top, not the turn number of
// the topmost hidden separator several screens above it.
// turnLabel is the coordinate an overflow marker prints, or empty when there is
// no turn to name.
//
// The words are turnRule's — "turn 4", the same two the separator draws — so
// the marker and the line it points at agree without a reader having to
// translate. A column with no turns at all (an unavailable card, a seat that
// has never been asked anything) prints nothing rather than "turn 0": a
// coordinate the room does not have is omitted, never invented (§4a.1).
func turnLabel(anchors []turnAnchor, line int) string {
	if n := turnAt(anchors, line); n > 0 {
		return "turn " + strconv.Itoa(n)
	}
	return ""
}

func turnAt(anchors []turnAnchor, line int) int {
	n := 0
	for _, a := range anchors {
		if a.Off > line {
			break
		}
		n = a.N
	}
	return n
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
	if w <= stripWidth {
		return stripHeader(st, c, f, w, sty, g)
	}
	// A space after the focus mark, and two cells of indent without it, so the
	// names still line up across the row. "▸Claude Code" saved a cell and spent
	// it looking like a typo.
	//
	// The two-letter vendor tag rides in front of the name PERMANENTLY, and this
	// is where §9.18's collapse stops being a narrow-width special case. `CC` and
	// `CX` were introduced as what identity degrades TO when a strip has no room
	// for a name — so the abbreviation a reader has to know appeared exactly
	// where they had the least context to learn it, and vanished at every width
	// where the room could have taught it. Drawn always, the wide column becomes
	// the legend for the narrow one: `CC Claude Code` at 37 cells is the sentence
	// that makes `CC ✓ done` at eighteen readable, and it is the same pairing the
	// HUD's own grid makes.
	//
	// It is chrome and the name is the anchor, so the tag is MUTED while the name
	// keeps the weight that says which column the keys move. Three cells, spent
	// on the header row only.
	lead := "  "
	if f.marked() {
		lead = g.Focus + " "
	}
	tag := vendorTag(c.Vendor)
	if tag != "" {
		tag += " "
	}
	// The seat's number, which is both a label and a KEY (§9.29). It goes in
	// front of the tag rather than after the name, because it is the thing a
	// reader's eye runs down the row of headers looking for — and because a
	// number at the far right would sit beside the state word, where every other
	// number on this line is a duration.
	//
	// Muted, on the tag's own argument: it is chrome and the name is the anchor.
	// Two cells, and the header row is the only place they are spent.
	num := ""
	if n := st.SeatNumber(c); n > 0 {
		num = strconv.Itoa(n) + " "
	}
	name := lead + num + tag + c.Label

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
	// The tag and the mark in front of it stay at the NAME's weight; only the
	// tag recedes. Split here rather than at the call site because the width
	// arithmetic above is over the plain string and must stay that way (§9.5's
	// ANSI trap).
	styledName := func(s string) string {
		pre := lead + num + tag
		if pre == lead || !strings.HasPrefix(s, pre) {
			return label.Render(s)
		}
		return label.Render(lead) + sty.Muted.Render(num+tag) +
			label.Render(strings.TrimPrefix(s, pre))
	}

	gap := w - lipgloss.Width(name) - lipgloss.Width(status) - 4
	if gap < 1 {
		// Identity yields first, and the TAG is the last of it to go — §9.18's
		// order, now reachable at more widths than the strip. Truncating from the
		// right takes the spelled-out name and leaves the two letters, which is
		// exactly the degradation the strip performs in one step. The NUMBER is in
		// front of both and therefore sheds last of all, which is the order §9.29
		// wanted anyway: a key nobody can see is a key nobody presses.
		keep := maxInt(1, w-lipgloss.Width(status)-1)
		return styledName(truncate(name, keep, g.Ellipsis)) + " " + right
	}
	mid := strings.Repeat(" ", gap)
	if headerUsesLeader(c) {
		mid = sty.Rule().Render(strings.Repeat(g.Rule, gap))
	}
	return styledName(name) + "  " + mid + "  " + right
}

// stripHeader is a seat's header at strip width: who it is, what state it is
// in, and nothing else that would push a state word off the line.
//
// §9.11 settled the degradation order — a clipped seat name is still
// recognisable and a clipped state word is not, so identity yields first — and
// then never enforced it at the one width where it bites. A 14-cell strip drew
// `Anti… ○ idle`: four fifths of a name the room could have said WHOLE in two
// letters, next to the only fact the strip exists to carry. So identity
// collapses to the two-letter vendor tag the HUD's grid already prints for the
// same vendor (vendorTag), and what it gives back is spent on the state:
//
//	CC ✓ done      CX ○ idle      AG ⠋ streaming      ⚠ unavailable
//
// Three things shed, in this order, and each is a pure function of the width so
// the frame sweep pins the whole ladder rather than one golden per state:
//
//   - the CLOCK, first and always. `8s` is the meta on this line, and turnRule
//     already ranks a label above the numbers that belong to it. It is not lost:
//     every finished turn carries its own elapsed on its separator (historyMeta).
//   - the FOCUS MARK. Two cells of `▸ ` at fourteen is the difference between a
//     tag and no tag for every nine-letter phase word. §9.12 had already moved
//     the load-bearing half of that signal off the glyph and onto WEIGHT and
//     onto the overflow marker's own words (`↑↓ scroll` against `tab to focus`);
//     both cost no cells, both survive here, and both are what a reader actually
//     used. A strip is by construction the seat this turn was NOT addressed to,
//     so spending a seventh of its width marking it — at the price of its
//     identity — inverts the priority §9.11 set.
//   - the TAG, last, and only for `unavailable`: the one state word long enough
//     that two letters and a mark cannot both sit beside it. The column's
//     position says which seat it is; the word says the thing nothing else can.
//
// The phase word itself never truncates. That is the whole rule this function
// exists to hold.
func stripHeader(st State, c Column, f seatFocus, w int, sty Styles, g Glyphs) string {
	word, mark := c.Phase.String(), phaseMark(c.Phase, st, g)
	style := sty.ForPhase(c.Phase)
	if c.Avail != AvailInstalled {
		// The same substitution columnStatus makes, and for the same reason: the
		// phase of a seat that is not there is not a fact about a turn.
		word, mark, style = "unavailable", g.Warn, sty.SevWarn
	}
	// Weight, not a marker, is what says the keys move this one — see above.
	label := sty.Identity
	if f.hasKeys() {
		label = sty.Strong
	}

	tag := vendorTag(c.Vendor)
	num := ""
	if n := st.SeatNumber(c); n > 0 {
		num = strconv.Itoa(n)
	}
	state := mark + " " + word
	// Longest first, widest that fits wins. Same idiom as the overflow marker's
	// hint list, so a reader of this file meets one shedding shape rather than
	// three.
	//
	// The NUMBER outranks the tag, which is a new rung in §9.18's ladder and the
	// one place its ordering had to be reasoned about again. §9.18 shed the focus
	// mark on the finding that the load-bearing half of that signal had moved
	// somewhere free (weight, and the overflow marker's own words); the tag is
	// likewise a second spelling of an identity the column's POSITION already
	// gives. The number is not a second anything — it is the key that reaches
	// this seat, and at strip width, where a room has four seats and one narrow
	// column each, it is the fastest way to any of them. A key nobody can see is
	// a key nobody presses (§9.10).
	//
	// At stripColumn the full form fits every phase word — `1 CC ⚠ unavailable`
	// is exactly eighteen cells — so this ladder only bites below a width §9.24
	// says the tier ladder does not produce. It is still written out, because the
	// last time a floor was assumed rather than enforced it was wrong by four.
	switch {
	case num != "" && tag != "" &&
		lipgloss.Width(num)+1+lipgloss.Width(tag)+1+lipgloss.Width(state) <= w:
		return sty.Muted.Render(num) + " " + label.Render(tag) + " " + style.Render(state)
	case num != "" && lipgloss.Width(num)+1+lipgloss.Width(state) <= w:
		return sty.Muted.Render(num) + " " + style.Render(state)
	case tag != "" && lipgloss.Width(tag)+1+lipgloss.Width(state) <= w:
		return label.Render(tag) + " " + style.Render(state)
	case lipgloss.Width(state) <= w:
		return style.Render(state)
	case lipgloss.Width(word) <= w:
		return style.Render(word)
	}
	// Unreachable at stripColumn — the longest word is eleven cells and the
	// narrowest strip is fourteen — and still honest if some future width breaks
	// that: a clipped string in this room says out loud that it was clipped.
	return style.Render(truncate(word, w, g.Ellipsis))
}

// vendorTag is a seat's identity in two cells, for a column too narrow to say
// its name.
//
// The spellings are the HUD's, character for character (internal/hud's
// vendorTag), and they are COPIED rather than imported. One product, one
// vocabulary: a reader who learned `CX` is Codex from the HUD's grid must not
// meet a second abbreviation in the room. What the copy protects is the seam
// this repo keeps between the two surfaces — internal/hud and internal/council
// share the normalized session model and internal/theme's numbers and nothing
// else (see padRight's note in layout.go) — and reaching across it for a
// rendering detail is the coupling that seam exists to prevent.
// TestStripTagsMatchTheHUDSpelling asserts the strings by literal so the copy
// cannot drift in silence.
func vendorTag(v model.VendorID) string {
	switch v {
	case model.VendorClaude:
		return "CC"
	case model.VendorCodex:
		return "CX"
	case model.VendorGemini:
		return "GE"
	case model.VendorAntigravity:
		return "AG"
	case model.VendorCursor:
		return "CU"
	default:
		// A vendor this map has not met yet still gets two cells rather than
		// none, on the HUD's own fallback: a seat added to one surface should not
		// read differently on the other in the window before anyone updates both.
		s := strings.ToUpper(string(v))
		if len(s) > 2 {
			s = s[:2]
		}
		return s
	}
}

// headerUsesLeader reports whether a column header fills the gap between the
// seat's name and its state with the rule glyph. It always does.
//
// It used to be false for an idle seat, on an argument that was correct at ONE
// rule weight: a long ──── between `Claude Code` and `○ idle` was *filling*
// rather than separating, whitespace does that job for free, and a room with a
// single rule weight cannot spend ink on nothing.
//
// §9.26 is what retires it. The frame now closes on the heavy rule and every
// line inside it is the lighter of two, so the header leader is no longer "the
// rule" — it is the interior weight, and its job on this row is to say that the
// name at the left and the state at the right belong to one seat. That claim is
// as true of an idle seat as of a streaming one.
//
// The observable defect the old form produced is the sharper argument. A room
// where one seat is answering and its neighbour is not drew the seats' header
// band as one continuous ruled line across half the frame and blank across the
// other half — one row, two grammars, re-texturing itself the moment a turn
// started and again when it ended. §7.1 rule 4 keeps this room still by default;
// a header band that changes shape on every dispatch is the loudest still-frame
// change on screen, spent on a fact the state word beside it already states.
//
// The air the old comment wanted is not lost: labelRule keeps two cells each
// side of its rule, which is the gap that keeps an ascii spinner ("-") legible
// against an ascii leader ("-").
func headerUsesLeader(c Column) bool {
	_ = c
	return true
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
func elapsed(st State, c Column) string { return elapsedSince(st, c.Started) }

// elapsedSince is elapsed with the start time named rather than carried on a
// Column, for the by-turn page's own seat rules (§9.22). Same purity contract:
// the answer comes from State.Now, which a tick stamps, and never from a clock
// inside Render.
func elapsedSince(st State, started time.Time) string {
	if started.IsZero() || st.Now.IsZero() {
		return ""
	}
	d := st.Now.Sub(started)
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
	lines, _ := columnLines(st, c, w, sty, g)
	return lines
}

// turnAnchor is where one turn begins in a column's flat line list: the row
// turnHead drew its separator on, and the number that separator names.
//
// It exists because the transcript's unit is the turn and its scroll model's
// unit is the line (§9.20). Every consumer — the hop keys, the overflow
// marker's coordinate — takes its offsets from the SAME render pass that
// produced the lines, rather than recomputing where a turn starts from
// History: a second derivation would drift from the first the day a card grows
// a row, and it would drift silently, since both would still be plausible
// numbers.
type turnAnchor struct {
	// N is the turn number as turnRule prints it, not an index.
	N int
	// Off is the index of that turn's separator line in columnLines' output.
	Off int
}

// columnLines is columnText plus where each turn starts.
//
// One function rather than two because the offsets are only meaningful against
// the exact list of lines they index into: the trace, the cards and the wrap
// width all decide how tall a turn is, and a caller that asked for "the lines"
// and "the turn starts" separately could be handed two answers from two
// geometries without either one being wrong on its own.
func columnLines(st State, c Column, w int, sty Styles, g Glyphs) ([]string, []turnAnchor) {
	if c.Avail != AvailInstalled {
		// No turns, and that is the honest answer rather than an empty slice
		// standing in for one: a seat that cannot be driven has never been asked
		// anything, so the hop keys have nowhere to go and say so by doing
		// nothing (§9.20).
		return unavailableCard(c, w, sty, g), nil
	}

	var out []string
	var anchors []turnAnchor
	for i, h := range c.History {
		if i > 0 {
			// The turns between this record and the one before it are turns this
			// seat sat out, and they are coalesced IN PLACE — a run broken by a
			// turn the seat took starts a new line where the break is, so the
			// transcript still reads in order (§9.19).
			out = append(out, skipSpan(c.History[i-1].N+1, h.N-1, w, sty, g)...)
		}
		anchors = append(anchors, turnAnchor{N: h.N, Off: len(out)})
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
		if n := len(c.History); n > 0 {
			out = append(out, skipSpan(c.History[n-1].N+1, c.TurnN-1, w, sty, g)...)
		}
		// The current turn's separator carries the number and nothing else. Its
		// clock and its cost are in the column header and the badge line, which
		// is chrome that describes exactly this turn — repeating them a row
		// later would be the room saying the same thing twice. A past turn has
		// no chrome of its own, which is why the record carries them.
		anchors = append(anchors, turnAnchor{N: c.TurnN, Off: len(out)})
		// The echo yields to the band, and only the LIVE turn's does (§9.30).
		// While the band is up the user's words are on screen once, full width,
		// above every column that was asked them, so repeating them here is the
		// duplication the band exists to delete. What survives is the separator:
		// it is this column's own statement of which turn the lines under it
		// belong to, and it is one line rather than the same paragraph three
		// times over.
		//
		// The turn number is tested rather than assumed. A column's Prompt block
		// outlives its turn — a seat that answered turn 3 and sat out 4 and 5 is
		// still showing turn 3's brief here — and that block is that seat's own
		// conversation, which §9.9 is emphatic belongs to the column.
		prompt, quoted := c.Prompt, c.Quoted
		if c.TurnN == st.Turn && bandUp(st, g) {
			prompt, quoted = "", false
		}
		out = append(out, turnHead(c.TurnN, "", prompt, quoted, w, sty, g)...)
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
	// The two IDLE cards first, because they are the only cases here that are
	// about a column with no turn at all — and they are therefore the only ones
	// the by-turn page never reaches, since a seat on a page took the turn by
	// definition. Everything else is inFlightBody, shared (§9.22).
	case c.Phase == PhaseIdle && c.Body == "" && st.Reattached.Active():
		out = append(out, reattachCard(st, c, w, sty)...)
	case c.Phase == PhaseIdle && c.Body == "":
		out = append(out, dimmable(wrap("no turn dispatched yet.", w), sty)...)
	default:
		out = append(out, dimmable(
			inFlightBody(c.Phase, c.Gran, c.Body, len(c.Acts) > 0, w), sty)...)
	}

	// Everything this seat has sat out SINCE its last turn, which is where the
	// post-#99 room spends most of its rows: the default route is one seat, so
	// three columns skip every ordinary turn.
	//
	// Anchored on a turn the transcript actually shows, and never extended
	// backwards past the oldest record. History is capped at fifty and drops the
	// oldest first, so "not addressed in turns 1–29" on a column whose record of
	// those turns was evicted would be the room inventing an absence — the same
	// error in the other direction as inventing a conversation (§9.9).
	from, to, run := trailingSkip(st, c)
	if last := lastTurnLine(c, st, w, sty, g); len(last) > 0 {
		out = append(out, "")
		out = append(out, last...)
	} else if run {
		out = append(out, "")
	}
	if run {
		out = append(out, skipSpan(from, to, w, sty, g)...)
	}
	if c.Note != "" {
		out = append(out, "")
		if c.Skipped {
			// The LIVE skip keeps a line of its own — the coalesced run above is
			// history, this is the turn happening now — and it is drawn as a skip
			// rather than as a note, for the reason Column.Skipped records.
			out = append(out, skipLine(c.Note, w, sty, g)...)
		} else {
			out = append(out, noteCard(c.Note, c.NoteDetail, c.NoteCalm, w, sty, g)...)
		}
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
	if c.Arena != nil {
		// Below the answer, in the transcript's boundary grammar (labelRule, like
		// turn separators and the cleared marker): the seat spoke, and THEN its
		// tree was read. Three outcomes get three different renders, because
		// collapsing them is the §4a.1 bug — a diff (shown), a measured
		// nothing-changed (said in words, against the named base), and a diff
		// that could not be read (the error, never dressed up as "no changes").
		out = append(out, "")
		out = append(out, sty.Muted.Render(padRight(labelRule("arena "+c.Arena.Branch, "", w, g), w, g)))
		out = append(out, styleAll(wrap(abbreviate(c.Arena.Tree, st.Home), w), sty.Muted)...)
		// The finish line: host-observed rank, the phase word, the measured
		// elapsed. The rank NEVER stands alone — "2nd · failed" is a different
		// fact from "2nd · done", and printing the number without the word
		// would let a fast crash read as a podium. Rank zero (a fixture, or a
		// pre-rank frame) renders nothing rather than "1st of 0".
		if c.Arena.Rank > 0 {
			finish := ordinal(c.Arena.Rank) + " of " + itoa(c.Arena.Of) + " · " + phaseWord(c.Phase)
			if c.Elapsed > 0 {
				finish += " · " + dur(c.Elapsed)
			}
			out = append(out, styleAll(wrap(finish, w), sty.bold(sty.Text))...)
		}
		switch {
		case c.Arena.Err != "":
			out = append(out, wrap(c.Arena.Err, w)...)
		case c.ArenaShowDiff && c.Arena.Diff != "":
			// The full patch, capped for the frame: Render runs per keystroke
			// and a megabyte diff is tens of thousands of styled lines. The cap
			// names what it dropped and where the whole thing is — two exits,
			// both stated (y, and the worktree itself).
			// TrimRight before splitting: a patch ends in a newline, and the
			// empty string after it is not a line the cutoff should count.
			lines := strings.Split(strings.TrimRight(c.Arena.Diff, "\n"), "\n")
			shown := lines
			if len(lines) > arenaDiffScreenLines {
				shown = lines[:arenaDiffScreenLines]
			}
			for _, line := range shown {
				out = append(out, fit(strings.TrimRight(line, " "), w))
			}
			if n := len(lines) - len(shown); n > 0 {
				out = append(out, wrap("(… "+itoa(n)+" more lines — y copies the whole diff, d returns to the stat)", w)...)
			}
		case strings.TrimSpace(c.Arena.Stat) == "":
			out = append(out, wrap("no changes against "+shortSHA(c.Arena.Base)+".", w)...)
		default:
			for _, line := range strings.Split(c.Arena.Stat, "\n") {
				// fit, not padRight — a body line, and the ANSI trap is about
				// what a line CAN carry, not what it happens to today.
				out = append(out, fit(strings.TrimRight(line, " "), w))
			}
			if c.Arena.DiffTruncated {
				out = append(out, wrap("(the yankable diff is truncated at 1 MB — the worktree holds the whole of it)", w)...)
			}
		}
	} else if c.ArenaInterim != nil {
		// The MID-RACE stat (§9.37's live refresh). An else-branch of the
		// final on purpose: the two must never share a frame, because an
		// interim line beside a settled result is two answers with nothing to
		// say which one stands — finishColumn clears the interim when the
		// final lands, and this branch is the render-side half of that rule.
		//
		// The rule says "so far", and that is the whole honesty marker: this
		// is a measured read of the tree at a moment already past, not the
		// settled result, and the label is the word that keeps a mid-race
		// stat from masquerading as a finish line (§4a.1 — the same reason an
		// estimate wears its ~). No branch, no worktree path, no rank: those
		// are the finish line's receipt, and printing them early would dress
		// an interim block in the final's clothes.
		//
		// Three states, three renders, matching the final block's own rule: a
		// stat (shown), a measured nothing-YET (said against the named base —
		// "yet" because the seat is still running, unlike the final's settled
		// "no changes"), and a failed read (the error, never dressed as
		// zero). The fourth state — no read has returned — is the nil pointer
		// and renders nothing at all: absence, not a zero.
		out = append(out, "")
		out = append(out, sty.Muted.Render(padRight(labelRule("arena · so far", "", w, g), w, g)))
		switch {
		case c.ArenaInterim.Err != "":
			out = append(out, wrap("live stat unavailable: "+c.ArenaInterim.Err, w)...)
			if c.ArenaInterim.Stopped {
				// The give-up is said, not silent: a stat that quietly froze
				// would go on reading as live. The race itself is untouched
				// and the sentence says where the real answer still arrives.
				out = append(out, wrap("stopped re-reading after "+itoa(arenaRefreshMaxFails)+
					" failed reads — the finish-time diff still runs.", w)...)
			}
		case strings.TrimSpace(c.ArenaInterim.Stat) == "":
			out = append(out, wrap("no changes yet against "+shortSHA(c.ArenaInterim.Base)+".", w)...)
		default:
			for _, line := range strings.Split(c.ArenaInterim.Stat, "\n") {
				// fit, not padRight — the ANSI trap, same as the final stat.
				out = append(out, fit(strings.TrimRight(line, " "), w))
			}
		}
	}
	return out, anchors
}

// shortSHA is the seven-character convention, guarded for the synthetic bases
// tests construct.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// arenaDiffScreenLines caps how much of a patch one frame renders. The whole
// diff stays yankable; this bounds only the per-keystroke render cost, and the
// cutoff line says what was dropped and both ways to the rest.
const arenaDiffScreenLines = 400

// ordinal spells a rank. Four seats today, but the teens rule is three lines
// and a "21st" bug would outlive the assumption that broke it.
func ordinal(n int) string {
	s := itoa(n)
	if n%100 >= 11 && n%100 <= 13 {
		return s + "th"
	}
	switch n % 10 {
	case 1:
		return s + "st"
	case 2:
		return s + "nd"
	case 3:
		return s + "rd"
	default:
		return s + "th"
	}
}

// phaseWord is the finish-line vocabulary: the phase as one word, because a
// rank must never print without the word that says what KIND of finish it
// ranks — "2nd · failed" and "2nd · done" are different facts.
func phaseWord(p Phase) string {
	switch p {
	case PhaseDone:
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseCancelled:
		return "cancelled"
	default:
		return "running"
	}
}

// inFlightBody is what a seat's reading area says for the turn it is on: the
// reply as it lands, or the one line standing in for one that has not (§9.14).
//
// The three waiting lines are one row each, on purpose, and what they used to be
// is the point. This card exists because PhaseWaiting must never be mistaken for
// streaming — a genuine honesty distinction (§9.2), and it is kept. What did not
// belong is the ARGUMENT for it, restated in full in the body of every waiting
// turn: "this vendor reports no incremental output, so nothing appears until the
// turn finishes" is a sentence about council's plumbing, written in council's
// vocabulary, in the space where a user came to read an answer. Two thirds of
// the room renders this card, so on a normal turn it was most of what was on
// screen.
//
// What carries the distinction now is the word already in the column header —
// `waiting` against `streaming`, always drawn, in both glyph sets, beside the
// granularity badge that says WHY. The body says only that the seat is working
// and what to expect, and the wiring moved to the help panel's posture page,
// where a reader who wants it can go and get it. That is the same trade §9.13
// made for the sandbox badges: the claim stays on the column, the argument moves
// somewhere it can be read properly.
//
// Extracted from columnLines when the by-turn page needed the same answer
// (§9.22). It takes the four facts it actually reads rather than a Column,
// because the page's participants are turnEntry values — and the extraction is
// the point: two projections of one transcript that disagreed about what a
// waiting seat says would be the room telling a reader two things about one
// seat, in two places, with nothing on screen to say which was true.
func inFlightBody(phase Phase, gran Granularity, body string, acted bool, w int) []string {
	switch {
	case phase == PhaseStreaming && body == "" && acted:
		return wrap("working…", w)
	case phase == PhaseWaiting && body == "" && acted:
		// It has acted but not spoken. This one keeps its own sentence because
		// it is a different claim from the two below — there IS something on
		// screen, and pointing at it beats describing the seat.
		return wrap("working — the steps above are what it has done so far.", w)
	case phase == PhaseWaiting && body == "" && gran == GranUnknown:
		// Deliberately NOT the line below. "The reply arrives whole" is a
		// measurement two vendors earned; a column whose granularity was never
		// established must not borrow it. This says only what is observed —
		// nothing has arrived — and claims nothing about whether anything will
		// before the end. The header carries the rest: this is the one seat that
		// prints no granularity word at all.
		return wrap("working — nothing has arrived yet.", w)
	case phase == PhaseWaiting && body == "":
		// The honest version of an empty streaming column, in one line. This
		// vendor is working; it just does not report anything until it is done.
		return wrap("working — the reply arrives whole.", w)
	default:
		return wrap(body, w)
	}
}

// trailingSkip is the run of turns this seat has sat out since its last one,
// as an inclusive range, minus the turn its own live note already speaks for.
//
// Reported rather than stored, and derived from two numbers the room already
// keeps: the newest turn this column took (TurnN) and the turn the room is on
// (State.Turn). Recording a skip would mean a TurnRecord per turn a seat did not
// take, which is the room writing down a conversation that did not happen —
// §9.9's rule, and the reason the transcript skips from 3 to 5 in the first
// place.
//
// ok is false for a seat that has never taken a turn. A run needs a turn to hang
// off: "not addressed in turns 1–6" under a column whose card already says "no
// turn dispatched yet." is a second way of saying nothing happened, and on a
// column whose early records were evicted by the history cap it would be a
// claim about turns this seat may well have answered.
func trailingSkip(st State, c Column) (from, to int, ok bool) {
	if c.TurnN <= 0 {
		return 0, 0, false
	}
	to = st.Turn
	if c.Skipped {
		// The live note names this turn on a line of its own — it is the current
		// fact, the one the user is deciding whether to act on — so the coalesced
		// run stops one short of it rather than saying it twice.
		to--
	}
	from = c.TurnN + 1
	return from, to, from <= to
}

// skipSpan is the one muted line a whole run of sat-out turns costs.
//
// Post-#99 the default route is one seat, so three columns sit out every
// ordinary turn — and a line each turned the transcript of a quiet seat into a
// column of identical warnings, one per turn, with the answer it actually gave
// scrolled off the top. The run is the fact; the turns inside it are not
// separately interesting, and a reader who wants one has the turn numbers.
//
// The data model is untouched: nothing is coalesced on the way in, only on the
// way out, so `[` and `]` still hop between the turns this seat really took and
// the record still says exactly what it said.
//
// Singular for a run of one, because "turns 4–4" is a sentence no one writes.
func skipSpan(from, to, w int, sty Styles, g Glyphs) []string {
	switch {
	case from > to:
		return nil
	case from == to:
		return skipLine("not addressed in turn "+strconv.Itoa(from), w, sty, g)
	default:
		return skipLine("not addressed in turns "+
			strconv.Itoa(from)+g.Range+strconv.Itoa(to), w, sty, g)
	}
}

// skipLine draws a sat-out turn: muted, and led by the IDLE mark rather than by
// the warning one.
//
// ⚠ opens a note because a note reports something that did not complete
// normally — a cancellation, a seat that is not there. Sitting a turn out is
// neither. It was a fair mark when a narrow route was the exception; since the
// default route became one seat it is the ordinary shape of every turn, and a
// warning drawn on the common case is a warning the eye learns to skip — the
// same argument ActDenied makes for SevWarn over SevCrit, and reattachCard makes
// for no mark at all.
//
// ○ is the mark this room already spends on "nothing has been asked of this
// seat", which is exactly what a skipped turn is, said about one turn instead of
// about the whole session. It survives --ascii as "." against the warning's "!",
// so the demotion is legible with colour switched off — the word carries it
// first either way.
func skipLine(text string, w int, sty Styles, g Glyphs) []string {
	return styleAll(hangWrap(g.Idle+" ", text, w), sty.Muted)
}

// lastTurnLine is what an idle strip says instead of nothing: which turn it last
// took, and how that turn ended.
//
// Strip-width only. A wide column already answers this — the turn separators are
// on screen with their own numbers and their own outcomes — so the line would be
// the room repeating itself at the width that has rows to spare, and silent at
// the width that does not. At fourteen cells a seat sitting out had a header, a
// posture word and then a run of skips, and the one thing a reader wants from a
// backgrounded seat is where it left off.
//
// Every part of it is MEASURED: the turn number is the one this column recorded,
// the mark is that turn's own phase. A seat with no turns behind it renders
// nothing here rather than a placeholder — absent is absent (§4a.1), and this
// room does not draw "last: —". Nor does a seat that is IN the current turn: the
// line answers "where did this one leave off", which is not a question about a
// column that is answering right now.
func lastTurnLine(c Column, st State, w int, sty Styles, g Glyphs) []string {
	if w > stripWidth || c.TurnN <= 0 {
		return nil
	}
	if !c.Skipped && c.TurnN >= st.Turn {
		return nil
	}
	if c.Phase == PhaseWaiting || c.Phase == PhaseStreaming {
		// A turn that is still running is not one this seat "last took", and its
		// mark is the spinner — a moving cell on a line about something finished.
		// Unreachable in a real frame, since a column only reaches here by having
		// sat the current turn out, and kept as a guard rather than an assumption.
		return nil
	}
	mark := phaseMark(c.Phase, st, g)
	// Longest first, same shedding idiom as the header above it: `last:` is the
	// label and the turn number is the fact, so the label is what goes when a
	// three-digit turn arrives.
	for _, s := range []string{
		"last: turn " + strconv.Itoa(c.TurnN) + " " + mark,
		"turn " + strconv.Itoa(c.TurnN) + " " + mark,
	} {
		if lipgloss.Width(s) <= w {
			return []string{sty.Muted.Render(s)}
		}
	}
	return nil
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
	return labelRuleIn(label, meta, w, g.Rule)
}

// labelRuleIn is labelRule with the fill glyph named by the caller, so the one
// line in the room that draws this grammar at the heavy weight (§9.26, the turn
// page's own rule) shares the arithmetic rather than reimplementing it.
//
// The weight is a PARAMETER rather than a flag on Glyphs because the set of
// lines that carry it is closed and small: a caller that wants the heavy rule
// has to say so at the call site, which is what makes "exactly three lines"
// checkable by reading rather than by grepping a bool.
func labelRuleIn(label, meta string, w int, ruleGlyph string) string {
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
	s := label + "  " + strings.Repeat(ruleGlyph, n2)
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
		out = append(out, styleAll(indentWrap("  ", quotedNotice, w), sty.Muted)...)
	}
	return out
}

// quotedNotice is what a rebuttal turn reports rode along with the brief.
//
// Named rather than spelled twice, because the live band says it too (§9.30) and
// two literals would be two spellings of one fact — the drift labelRule's own doc
// comment refuses one grammar down. Whichever surface a reader sees it on, they
// are reading the same sentence about the same dispatch.
const quotedNotice = "+ the other seats' last answers were quoted to this one"

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
		out = append(out, dimmable(wrap(h.Body, w), sty)...)
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
	// No seat name: in the grid the card's POSITION is the seat, and naming it
	// again inside its own column would be the room saying one thing twice. The
	// by-turn page has no position left to carry it and passes one (§9.22).
	return gateCardLines(mine, "", w, sty, g)
}

// gateCardLines is the card itself, with the queue and the subject supplied.
//
// who is empty wherever the surface already says which seat is asking, and is
// the seat's label wherever it does not. The two callers are the grid's column
// and the by-turn page; splitting the card in two instead would have given the
// one line in this room that guards a write two spellings to drift between.
func gateCardLines(q []PendingGate, who string, w int, sty Styles, g Glyphs) []string {
	if len(q) == 0 {
		return nil
	}
	subject := q[0].Text
	if who != "" {
		subject = who + " — " + subject
	}
	// Same card grammar as every other card in this column: a title at weight,
	// its body hanging under it. The call being decided used to wrap back to the
	// column edge, so the second line of a long path sat flush against the frame
	// and read as a new statement rather than as the rest of the question.
	out := styleAll(hangWrap(g.Warn+" ", "waiting on you: "+subject, w), sty.Alert)

	// `a` is on the card and not only in the mode line, because it is the one key
	// here nobody arrives already knowing. y and n are the two answers anyone
	// expects from a prompt; "stop asking me" is a third thing the card has to
	// offer before a user can know it exists — and the moment they want it is
	// the moment they are staring at this.
	keys := "y approve   n deny   a stop asking"
	if n := len(q) - 1; n > 0 {
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
	// A seat that is not there makes no claims, and the row stays RESERVED but
	// empty rather than being dropped (see columnChrome: the grid's rows have to
	// line up).
	//
	// unavailable.txt used to draw `final only` under `⚠ Codex is not seated` —
	// a granularity badge is a claim about how this vendor behaves DURING a turn,
	// stated about a vendor that cannot take one. Codex was not found on PATH;
	// nothing about its streaming was measured, and §4a.1's rule is that a field
	// nothing sourced is absent rather than filled with a plausible value. It was
	// plausible — it is what the binary would do if it were installed — which is
	// precisely the kind of claim this repo exists to refuse. The cost cell goes
	// with it, for the same reason: a seat that never ran cost nothing, and a
	// blank is the honest way to say so.
	//
	// The card in the column below already says what IS known: which failure it
	// was, and what to do about it.
	if c.Avail != AvailInstalled {
		return ""
	}
	if w <= stripWidth {
		return stripBadges(c, w, sty)
	}
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

// stripBadges is the badge row at strip width: the posture word, or nothing at
// all.
//
// The row says three things at full width, and at fourteen cells it used to say
// all three and clip two: `  ro:tools  tokens` arrived as `  ro:tools  to`, and
// `gated  final only` as `gated  fina`. §9.11 rules that a clipped state word is
// not a word — and a clipped one that is also a PREFIX of another word in the
// same vocabulary is worse than damage, because it reads as a different claim.
// `fina` is not a broken `final only`; it is a word this room does not have.
//
// So whole words leave instead, in the order of what a reader loses by not
// seeing them:
//
//   - COST first. It is a number, the transcript records it on every turn's own
//     separator (historyMeta), and §9.11 already ruled that the posture claim
//     never gives way to it.
//   - GRANULARITY with it. That word exists so `waiting` cannot be read as a slow
//     `streaming` — and both of those words are on the header one row above,
//     where at strip width they are now the only thing on the line.
//   - POSTURE stays, if it fits WHOLE. Every badge this room spells does at
//     fourteen (`ro:requested` is the longest, at twelve), which is not luck:
//     §9.2 is emphatic that a claim you cannot see is not a claim, so the safety
//     word is the last thing on this row to go. A badge too long for the strip
//     drops rather than clips, and stays reachable at full length on the `?`
//     postures page and in the room header's own READ / WRITE marker.
//
// No indent, unlike the full-width row. Those two cells exist to bind the badges
// to a seat NAME above them, and at strip width there is no name — the header
// starts at column zero, so this does too, and the strip reads as one flush-left
// block rather than as a column with its margins still on.
func stripBadges(c Column, w int, sty Styles) string {
	b := c.Sandbox.Badge()
	if b == "" || lipgloss.Width(b) > w {
		return ""
	}
	return sty.ForSandbox(c.Sandbox.Level).Render(b)
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

// dimmable applies the reading area's contrast step (§9.27) to a block of
// already-wrapped prose, and is a no-op for the focused column and for
// PlainStyles alike.
//
// BLANK rows are left bare, and that is load-bearing rather than tidiness.
// railRows decides which body rows carry a gutter by testing
// `strings.TrimSpace(cell) != ""` over the RENDERED cell, so a blank row wearing
// an escape sequence would read as ink and the frame's rails would differ
// between a colour terminal and the goldens — a divergence PlainStyles is by
// construction blind to, which is the same class of trap as §9.5's padRight.
func dimmable(lines []string, sty Styles) []string {
	s := sty.Body()
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines[i] = s.Render(l)
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
		// The seat's number (§9.29). The tab bar is the other place the key acts,
		// and at this tier it is where it earns most: `tab` walks one seat at a
		// time through a full-frame redraw each press, while the number goes
		// straight there. Chrome, like the tag beside it, for the same reason —
		// the name is the anchor.
		num := ""
		if n := st.SeatNumber(c); n > 0 {
			num = strconv.Itoa(n) + " "
		}
		// The tag rides here too, for columnHeader's reason: a tab bar is the
		// other place a seat NAME heads a reading area, so it is the other place
		// that has to teach the two letters a strip will later use alone.
		tag := vendorTag(c.Vendor)
		if tag != "" {
			tag += " "
		}
		// Same two-cell prefix and the same weight the column header gives a seat
		// name, so the tab bar and the header underneath it agree about how a
		// selected seat is drawn rather than each having its own spelling. The
		// tag is chrome on the selected tab as it is on the header.
		//
		// Both names now carry the SEAT's own hue (§9.28), and the unselected one
		// stops being wholly Muted to get it. That is a promotion, and it is the
		// opposite of what §9.27 does to an unfocused column's prose — deliberately:
		// prose in a column you are not reading is content you are not reading,
		// while an unselected tab is a DESTINATION. It is the one row on this tier
		// whose entire job is "here are the other seats, pick one", and a menu
		// whose entries are faint is a menu that makes you read it twice. The
		// selected tab still outranks it by weight and by the mark in front of it,
		// which is the distinction that survives NO_COLOR.
		if idx == st.Focus {
			parts = append(parts, sty.SeatStrong(c.Vendor).Render(g.Focus+" ")+
				sty.Muted.Render(num+tag)+sty.SeatStrong(c.Vendor).Render(label))
		} else {
			parts = append(parts, sty.Muted.Render("  "+num+tag)+
				sty.SeatIdentity(c.Vendor).Render(label))
		}
	}
	// fit, not padRight: parts are already styled per tab.
	return framePadStr + fit(strings.Join(parts, "  "), lay.Width-2*framePad) + framePadStr
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
		b.WriteString(framePadStr + l + framePadStr)
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
	w := width - 2*framePad - lipgloss.Width(g.Prompt+" ")
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
			framePadStr + sty.Muted.Render(prefix) +
				sty.Muted.Render(padRight("type a brief"+g.Caret, w, g)) + framePadStr,
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
			framePadStr + sty.Muted.Render(prefix) + sty.Text.Render(padRight(text, w, g)) + framePadStr,
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
		out = append(out, framePadStr+sty.Muted.Render(padRight(
			g.Up+" "+strconv.Itoa(elided)+" more above", w+2, g))+framePadStr)
	}
	for i, r := range rows {
		// Continuation rows are indented to the prefix, so a wrapped brief reads
		// as one thing rather than as several.
		p := "  "
		if i == 0 && elided == 0 {
			p = prefix
		}
		out = append(out, framePadStr+sty.Muted.Render(p)+sty.Text.Render(padRight(r, w, g))+framePadStr)
	}
	return padRows(out, lay, w, sty, g)
}

// padRows makes the compose area exactly the height the layout budgeted, so the
// frame is the number of lines the terminal has whatever the draft is doing.
func padRows(rows []string, lay Layout, w int, sty Styles, g Glyphs) []string {
	for len(rows) < lay.Prompt {
		rows = append(rows, framePadStr+sty.Muted.Render("  ")+sty.Text.Render(padRight("", w, g))+framePadStr)
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
	// shed marks a hint that may be dropped whole rather than let the line be
	// truncated (§9.20).
	//
	// The default is false and stays false for everything that was here before:
	// this is not a licence to hide keys, it is a choice about WHICH cell goes
	// when the footer runs out of width. Truncation cuts the tail, so the
	// alternative to shedding is `q quit` and `? help` losing their last letters
	// to make room for a key added in front of them — the room's way out of the
	// room, spent on a motion key. A shed hint is one the ellipsis would
	// otherwise have chosen at random.
	//
	// **Shed order is list order**, so where a hint sits in modeHints' slice is
	// its rank as well as its position — the leftmost sheddable cell is the
	// first to go. That used to be a backwards walk, which read as "newest
	// first" and was not: `[ ]` is the second cell on the view line and `f` the
	// third, and neither position tracks when it was introduced. Once a second
	// rung existed the walk direction stopped being incidental and started
	// deciding which key a narrow room keeps, so it is stated rather than
	// inherited.
	shed bool
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

// flowStopHint appends the `s` cell wherever a busy line is being built, and
// only while a chain is live (§9.35).
//
// Gated on FlowSteps rather than on Busy alone: a mode line that promises a
// key which does nothing is §7.8's failure, and `s` without a chain only
// explains itself. This cell is also the control's whole documentation — it is
// not in helpKeys, deliberately: the key exists only while a chain runs, the
// mode line names it on every frame of exactly those moments, and the help
// panel's 17-row budget has no row for a key that is dead in every room the
// panel is usually read in. The label flips with the armed state so the line
// always says what pressing it does NOW — the same rule that renames `a`.
func flowStopHint(hs []hint, st State) []hint {
	if st.FlowSteps == 0 {
		return hs
	}
	if st.FlowStop {
		return append(hs, hint{key: "s", label: "continue chain"})
	}
	return append(hs, hint{key: "s", label: "stop after hop"})
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
	// `tab` and `f` address COLUMNS, and the by-turn page has none: there is one
	// reading area, so focus has nothing to move between and expanding it grows
	// it to the width it already has. Dropped in both modes for that reason —
	// including compose, where the page stays open while a brief is typed
	// (§9.22).
	several := len(st.VisibleColumns()) > 1 && !st.Page.Open
	if st.Mode == ModeComposing {
		// The routing is stated before the keybindings because it is the one
		// thing on this line that changes what enter DOES. An @typo has to read
		// as "this is going to claude" while there is still time to fix it;
		// discovering it afterwards means a turn went to the wrong seat.
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
		// The routing cell states the BILL as well as the destination, and the
		// two are one hint rather than two because the count is a property of
		// the route rather than a fact beside it: key and label is the same
		// figure/ground split every other cell on this line makes, so the seat
		// names keep their intensity and the number recedes to chrome without
		// costing a cell or a colour (§9.21).
		//
		// The rebuttal tag moves to its own cell so the count can sit against
		// the route it prices. It still answers the same question — what is
		// actually about to be sent — one separator further along.
		hs := []hint{{key: "→ " + routeLabel(st), label: seatBill(st)}}
		if q := quoteTag(st); q != "" {
			hs = append(hs, hint{key: q})
		}
		hs = append(hs,
			hint{key: "enter", label: "dispatch"},
			hint{key: g.Up + g.Down, label: "scroll"},
		)
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

	if st.Page.Open {
		// The by-turn line, and what is NOT on it is the argument.
		//
		// `[ ]` keeps the words it has in the grid, because it is the same motion
		// at the same unit — §9.20's vocabulary, moved one projection over rather
		// than re-spelled, so there is one thing to learn. `t grid` is the way
		// BACK, and it is the one cell here that may never shed: a projection you
		// cannot leave is the help panel's missing `?` with a whole surface behind
		// it. `y yank` is named here and not in the grid because on a page the key
		// takes the document the page is showing — a fact a reader can check
		// against what is in front of them, which is what makes it worth a cell.
		//
		// `f` and `tab` are absent because they do nothing here (see several,
		// above). `i compose` is the deliberate omission: the six cells below are
		// what this page's own motions need, the composer is one `t` away in a
		// mode line that names it, and it is on the help panel's first row —
		// while a footer that ran out of width would otherwise start cutting into
		// the way out of the room.
		hs := []hint{
			{key: g.Up + g.Down, label: "scroll"},
			{key: "[ ]", label: "turn", shed: true},
			{key: "t", label: "grid"},
			{key: "y", label: "yank", shed: true},
		}
		if st.Busy() {
			return append(flowStopHint(hs, st), hint{key: "ctrl+c", label: "cancel"}, hint{key: "?", label: "help"})
		}
		return append(hs, hint{key: "?", label: "help"}, hint{key: "q", label: "quit"})
	}

	// The turn hop sits immediately after the line-wise keys, because it is the
	// same motion at the transcript's own scale and a reader hunting for "how do
	// I get back to what I asked" should not have to find it three cells later
	// (§9.20).
	//
	// View mode only: `[` and `]` are the letters they type in compose, the same
	// rule that keeps `q` the letter q there. And offered unconditionally rather
	// than gated on how many turns this seat has — `↑↓ scroll` is already named
	// in a room where nothing has been said yet, and a footer cell that appeared
	// at the first dispatch would be chrome changing under a reader mid-turn,
	// which §7.1 rule 4 does not budget for.
	//
	// It is the one cell on this line marked sheddable: at the tabbed tier the
	// footer fit its keys exactly, and the honest place to take the cells from
	// is the key that was added last, not the tail the ellipsis would have eaten.
	if st.Help != HelpClosed {
		// The panel replaces the column area, so every motion key on this line
		// addresses something that is not on screen — and `↑↓` addresses nothing
		// at all: key() routes no scroll to the help panel, so the room was
		// advertising a key that does literally nothing in the mode a reader is
		// in when they went looking for what the keys do. That is §7.8's
		// surprise pointing the other way, and §9.11 already dropped `f` and
		// `tab` outright on the same argument in a one-seat room.
		//
		// What is left is what actually works here: `?` cycles the pages and
		// closes the panel, `i` and `enter` leave it for the composer, `q` quits.
		// The overflow marker inside the panel names no key either, for the same
		// reason (helpRows) — the honest thing to tell a reader is that there is
		// more and this terminal is not tall enough, not to point at an arrow
		// that will not move it.
		hs := []hint{{key: "?", label: "next page"}, {key: "i", label: "compose"}}
		if st.Help == HelpPostures {
			hs[0].label = "close"
		}
		if st.Busy() {
			return append(flowStopHint(hs, st), hint{key: "ctrl+c", label: "cancel"})
		}
		return append(hs, hint{key: "q", label: "quit"})
	}

	hs := []hint{
		{key: g.Up + g.Down, label: "scroll"},
		{key: "[ ]", label: "turn", shed: true},
	}
	// A room that has stopped asking says so, on every frame, next to the key
	// that reverses it.
	//
	// This is a hint about STATE rather than about the mode, which no other cell
	// on this line is — and that is the justification rather than an exception to
	// it. `a` on the card is announced twice while a card is up and then the card
	// is gone; without this the room would be permanently ungated with the only
	// way back undocumented anywhere on screen, which is the §9.17 defect rebuilt
	// one key later. It is not marked sheddable for the same reason `t grid` is
	// not: shedding it would drop the way out of a state, not a convenience.
	//
	// Only when the gate is OFF. The asking room is the default and needs no cell
	// to say a card will arrive — the card says that itself.
	if !st.Asking() && st.Write {
		hs = append(hs, hint{key: "a", label: "not asking"})
	}
	if several {
		// `f` is the SECOND rung of the shed ladder, after `[ ]`, and the two
		// cells framePad now spends on the room's margins are what made a second
		// rung necessary: at 80 columns the view line comes out one cell over
		// with `[ ]` already gone, and this room sheds whole cells rather than
		// clipping words (§9.18).
		//
		// `f` rather than `tab`, and rather than the tail: `tab` is how you reach
		// the other seats at the tabbed tier, which is the tier this only bites
		// at, so shedding it would strand a reader on one column. `f` is the cell
		// §9.11 already ranked lowest — it is the first thing dropped outright in
		// a room with one seat on screen, on the argument that it expands a
		// column to a width it already has, and at the tabbed tier the drawn
		// column is likewise already the whole frame. What it buys there is the
		// expanded mode's persistence across a resize, which is the least any
		// cell on this line offers.
		hs = append(hs, hint{key: "f", label: "expand", shed: true},
			hint{key: "tab", label: "focus"})
		// The seat numbers, next to the key that does the same job one step at a
		// time (§9.29). The range is however many seats are on screen rather than
		// a literal `1-4`: a room with three seats has no `4`, and a footer that
		// named one would be promising a key that does nothing — §7.8's surprise,
		// which this line already refuses in the other direction for `tab` and `f`.
		//
		// It is the THIRD rung of the shed ladder, after `[ ]` and `f`, so §9.24's
		// order is untouched and this appends rather than reorders. Last to go of
		// the three, and that is the argument rather than the default: shedding
		// only bites at the tabbed tier, which is precisely where one seat is on
		// screen and reaching the fourth costs three `tab` presses through three
		// full-frame redraws. `[ ]` sheds first because `g` and `G` still reach the
		// ends of the transcript; nothing else reaches seat 4 in one keystroke.
		hs = append(hs, hint{key: "1-" + strconv.Itoa(len(st.VisibleColumns())),
			label: "seat", shed: true})
	}
	if st.Busy() {
		return append(flowStopHint(hs, st), hint{key: "ctrl+c", label: "cancel"}, hint{key: "?", label: "help"})
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
				// The mode line is the contract: it announces what every key means
				// on every frame, and this mode gives `a` a meaning it does not
				// have anywhere else (§7.8). Leaving it to the card alone would put
				// a key that silences the room's only guard in the one place a
				// scrolled column can hide.
				{key: "a", label: "stop asking"},
				{key: "ctrl+c", label: "cancel the turn"},
			},
			lay, sty, g)
	}

	left = "VIEW"
	leftStyle := sty.Strong
	switch {
	case st.Mode == ModeComposing:
		left = "COMPOSE"
		// Empty compose is the post-turn resting state. Full weight on COMPOSE
		// competed with the routing cell for the same attention the screenshot
		// said the footer was stealing from the body — demote until there is a
		// draft. Gate and notices still outrank this path above.
		if strings.TrimSpace(st.Draft) == "" {
			leftStyle = sty.Muted
		}
	case st.Page.Open:
		// The mode word says which PROJECTION is live, and carries the turn the
		// body is deliberately not allowed to move to (§9.22, pageLabel).
		left = pageLabel(st)
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
//
// It sheds before it truncates, and the order is: everything, then the
// sheddable cells newest-first, then the ellipsis. §9.20 added a key to a line
// that fit its narrowest tier exactly, and truncation answers that by cutting
// the RIGHT-hand end — which is where `? help` and `q quit` live. A footer that
// buys a scroll hint with the panel's only documented way out is the trade
// §9.11 spent this line's whole redesign refusing.
func statusLine(left string, hs []hint, lay Layout, sty Styles, g Glyphs) string {
	styled, plain := hints(sty, g, hs)
	fits := func(p string) bool {
		return lay.Width-lipgloss.Width(left)-lipgloss.Width(p)-2*framePad >= 1
	}
	// Forwards, because shed order is list order — see hint.shed. Re-scanning
	// from the front each time is what lets the slice shrink under the index.
	for !fits(plain) {
		i := -1
		for j, h := range hs {
			if h.shed {
				i = j
				break
			}
		}
		if i < 0 {
			break
		}
		hs = append(append([]hint{}, hs[:i]...), hs[i+1:]...)
		styled, plain = hints(sty, g, hs)
	}
	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(plain) - 2*framePad
	if gap < 1 {
		gap = 1
		styled = sty.Muted.Render(truncate(plain,
			maxInt(1, lay.Width-lipgloss.Width(left)-2*framePad-1), g.Ellipsis))
	}
	return framePadStr + left + strings.Repeat(" ", gap) + styled + framePadStr
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
//
// Its own cell rather than a suffix glued to the routing text, since the
// routing cell's label is now the seat count and the count has to sit against
// the route it prices (§9.21). It keeps its intensity by keeping the key half
// of a hint: this is a fact about the dispatch, not chrome describing one.
func quoteTag(st State) string {
	if !st.Quote {
		return ""
	}
	if st.Turn == 0 {
		return "+ rebuttal (turn 1 is blind)"
	}
	return "+ rebuttal"
}

// seatBill is what the draft would actually cost in seats, or empty when the
// number would be noise.
//
// Empty below two, and that is the whole rule: "→ claude · 1 seat" prices a
// route whose own text already names every seat it reaches, and a cell that
// restates its neighbour is how the footer became the wall §9.11 had to take
// apart. From two upward the route names a SET — "everyone", "everyone but
// codex" — and how many seats that is depends on what is installed and what
// --vendor left in the room, which is a fact the user cannot read off the
// words.
//
// It counts seated ∩ addressed, through the same State.SeatsIn that dispatch
// gates on, so a route naming an unseated vendor is not billed for it. A
// refused route addresses nobody and prices nothing: its cell keeps the
// refusal label unchanged, which is the one thing a reader needs from it.
func seatBill(st State) string {
	if n := st.SeatsIn(st.Route); n > 1 {
		// Parenthesised, which is this room's existing grammar for a qualifier
		// on the thing in front of it — the gate's "(+2 queued)", the rebuttal's
		// "(turn 1 is blind)". Weight already separates the count from the route
		// in a colour terminal; the brackets are what keep them apart under
		// NO_COLOR and in every PlainStyles golden, where a bare "→ codex, agy 2
		// seats" runs the price into the list it is pricing.
		return "(" + strconv.Itoa(n) + " seats)"
	}
	return ""
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
	lines := helpKeys(lay, sty, g)
	if st.Help == HelpPostures {
		lines = helpPostures(st, lay, sty, g)
	}
	w := lay.Width - 2*framePad
	rows := helpRows(lines, lay.Body, w, sty, g)

	var b strings.Builder
	for i := 0; i < lay.Body; i++ {
		if i < len(rows) {
			// fit, not padRight: some help lines are pre-styled.
			b.WriteString(framePadStr + fit(rows[i], w) + framePadStr)
		} else {
			b.WriteString(strings.Repeat(" ", lay.Width))
		}
		if i < lay.Body-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// helpRows fits a help page into h rows, saying so when it cannot.
//
// **This panel used to clip in silence**, and it was the only surface in the
// room that did. Everywhere else — every column, every turn page — content that
// does not fit spends a body row on `↓ N more below`, on the explicit argument
// that silent clipping is indistinguishable from there being nothing more to
// say (§4a.1, columnCell). The help panel is 24 rows on page one and 33 on page
// two against a hard budget of 17, so at the reference machine's own geometry it
// was dropping seven lines and sixteen lines respectively, with nothing on
// screen to say so — and it dropped them mid-word, `…the containment, not a`.
// A panel whose job is to enumerate what the room can do, quietly not
// enumerating it, is the sharpest version of the defect the marker exists for.
//
// **The way out is pinned to the last row**, and that is what makes the marker
// affordable rather than dangerous. `?` is the only documented way back out of
// this panel, and on both pages it sits at exactly row 17 of a 17-row budget —
// so a marker taking the last row the ordinary way would have bought honesty
// with the exit, which is the trade §9.11's footer pass and helpKeys' own budget
// comment both refuse by name. Pinning makes the guarantee structural instead of
// a lucky row count: the exit is chrome, like `columnChrome` above a transcript,
// and the marker lives inside the scroll below it.
//
// The marker costs a row, and page one paid for it the way this panel always
// pays — by merging two lines that were one category (`ctrl+j` and `esc`, the
// two compose keys that are not `enter`). Nothing is dropped to make room.
//
// **The marker names no key, deliberately.** `↑↓` do nothing over the help panel
// — `key()` routes no scroll to it — so a hint here would be the false promise
// §7.8 forbids, and the same reasoning removes `↑↓ scroll` from the mode line
// while the panel is open (modeHints). What the reader is told is the true
// thing: there is more, and this terminal is not tall enough for it.
func helpRows(lines []string, h, w int, sty Styles, g Glyphs) []string {
	if h <= 0 {
		return nil
	}
	if len(lines) <= h {
		return lines
	}
	exit := helpExit(lines)
	// Two rows is the floor for the pinned shape: one marker and one exit. Below
	// it the panel is unusable either way, and the marker alone is still more
	// honest than a silent cut.
	if exit < 0 || h < 3 {
		out := append([]string{}, lines[:h-1]...)
		return append(out, sty.Muted.Render(overflowMarker(
			g.Down, len(lines)-(h-1), "below", "", nil, w, g)))
	}
	body := append(append([]string{}, lines[:exit]...), lines[exit+1:]...)
	content := h - 2 // one row for the marker, one for the pinned exit
	out := append([]string{}, body[:content]...)
	out = append(out, sty.Muted.Render(overflowMarker(
		g.Down, len(body)-content, "below", "", nil, w, g)))
	return append(out, lines[exit])
}

// helpExit is the index of the page's way out — the entry whose key column is
// `?`. Both pages carry exactly one, on purpose: `?` cycles, so three presses
// always return the room, and it is the one row helpKeys' budget comment says
// may never be spent.
//
// Matched on the rendered line rather than declared alongside it because the two
// page builders are plain []string and three tests already read them that way,
// looking for this same line as the fold. One spelling of "which line is the way
// out", read the same way by the renderer and by the tests that guard it.
func helpExit(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "? ") {
			return i
		}
	}
	return -1
}

const (
	// helpIndent is where a help page's PROSE column starts: two cells of
	// margin, then thirteen for the key or badge in front of it.
	//
	// One number because the panel has three things that must line up on it —
	// the key column on page one, the badge legend on page two, and the
	// per-seat detail under that legend — and the third was hanging at six while
	// the first two sat at fifteen. A hard-coded 13 and a hard-coded 15 and a
	// hard-coded 6 cannot disagree visibly until someone reads the panel, which
	// is how the misalignment survived.
	helpIndent = 15
	// helpHang is helpIndent as the string a continuation row is written with.
	helpHang = "               "
)

func init() {
	// The two spellings of one number, checked once at startup rather than by
	// eye. A panel whose continuation rows drift a cell from its key column is
	// the exact defect helpIndent was extracted to end, and it is invisible in
	// a diff.
	if len(helpHang) != helpIndent {
		panic("helpHang must be helpIndent cells wide")
	}
}

// helpTitle is a help page's heading, in the grammar every other heading in this
// room already uses: the name at weight, a rule, and what this page is about
// anchored at the right.
//
// It was `council — one brief, several agents, side by side`, an em dash and a
// subtitle, and it was **the only heading in the product with no rule on it**.
// The column header, every turn separator and every seat rule on a turn page all
// draw labelRule; the panel that teaches the room's vocabulary was the one
// surface not speaking it.
//
// A rule UNDER the title is what a reader might expect and it is not what this
// room does. §9.11 spent a whole item removing exactly that shape — a heading
// followed by a horizontal rule three rows later — on the finding that the lower
// rule said nothing the heading had not, and ruled that a heading carries its own
// rule instead. So this costs no row, which is what makes it affordable against a
// budget with none to spare.
//
// The LIGHT rule. The help panel is drawn INSIDE the frame's two heavy rules,
// so its own title is interior by construction (§9.26) — a heading that matched
// the frame would claim to bound the room rather than to head a page in it.
func helpTitle(about string, lay Layout, sty Styles, g Glyphs) string {
	return strongLabelRule("council", about, lay.Width-2*framePad, g.Rule, sty)
}

// helpKeys is page one: what every key does.
func helpKeys(lay Layout, sty Styles, g Glyphs) []string {
	// The budget is HARD, and it is 17 rows rather than the 19 this panel used
	// to spend. Body at a 24-row terminal is 19 with nothing else on screen, but
	// the collapsed-seat notice costs a row and the narrow tier's tab bar costs
	// another — and a machine with a seat that will not run is the ordinary
	// machine, not the edge case. At 19 entries the `?` line, the only
	// documented way back out of this panel, fell off the bottom in exactly that
	// room. Anything added here has to be merged into a line that is already
	// present; two were, to buy the two rows back.
	return []string{
		helpTitle("one brief, several agents, side by side", lay, sty, g),
		"",
		// Merged with the `enter` line it used to sit above. The two described one
		// key in two rows, which is the cheapest row in the panel to buy back.
		"  i / enter    compose a brief; enter dispatches — to claude, or whoever is @mentioned (@all = everyone)",
		// `esc` merged in from its own row, and the row it frees is what pays for
		// the overflow marker helpRows now spends (§4a.1: this panel used to clip
		// in silence). The merge is a category rather than a saving, which is the
		// bar helpKeys' budget comment sets: these are the two compose keys that
		// are NOT enter — one extends the draft, one leaves it alone — and a
		// reader hunting for either is in compose looking for the way out of it.
		"  ctrl+j/esc   ctrl+j puts a newline in the brief (it grows to six rows); esc leaves compose, keeping the draft",
		// Down from three rows to two. What went is "the others are review, IDE
		// and tiebreak lanes" — which explains why the fleet is shaped this way
		// rather than what a key does, and it is in docs/design.md §9.11 and
		// ADR-010 where that argument belongs.
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
		"  @codex       name a lane: @claude @codex @agy @cursor; @all convenes everyone",
		"               -@codex excludes one. Unaddressed goes to claude. Leading only: \"ask @claude\" is prose",
		// One line, like pgup/pgdn below and for the same reason: the panel has
		// to fit a 24-row terminal with q and ? still on screen.
		//
		// Three controls on one row, and the merge is the honest shape rather than
		// only a saving: these are the ones that change the ROOM from inside it
		// instead of addressing the vendors, which is the distinction design.md
		// §9.17 turned into a rule. Grouped, a reader learns the category; split
		// across three rows they would read as three unrelated keys.
		//
		// It has to be INSIDE the budget. helpBody clips at the body height and
		// does not scroll, so a row past the fold is invisible on a 24-row
		// terminal — which is exactly where the posture explanation was standing
		// when a user asked what "unsandboxed" meant, the failure that split this
		// panel into two pages. Below the fold is not a cheaper row, it is no row.
		// `/unseat` merged onto the row `/seat` already holds, as `/seat /unseat
		// <list>`, and the merge is the honest shape rather than a saving: they
		// take one argument in one vocabulary and differ only in direction, so a
		// reader who finds either has found both. "times" paid for it — the row is
		// a list of controls, and `/trace <file>` is unambiguous without the verb.
		// A row of its own was never affordable: the budget is hard (17 rows,
		// above) and a control documented below the fold is a control nobody finds
		// (§9.20), which is exactly how the room came to be missing this one.
		"  /cd <dir>    move the room; /read /write (y); /seat /unseat; /arena races worktrees; c clears (y); /trace <file>",
		// One row for three keys, and the merge is the honest shape rather than
		// a saving. The panel's budget is hard (17 rows, above) and yank had to
		// land inside it — a copy key documented below the fold is a copy key
		// nobody finds — but the real reason these belong together is that they
		// COLLIDE. `y` means two things depending on whether a vendor is
		// blocked, gateKey resolves it, and the one place a reader could learn
		// that is the line that names both. Splitting them into two rows would
		// have spent a row to make the collision harder to see.
		"  y / Y        copy this seat's reply, or the whole turn (in turn view, both) — while a gate waits, y/n answer it",
		// The "in compose too" clauses are the whole of this change on this
		// panel. These keys always worked; what no one could find out is that
		// they now work in the mode a finished turn drops you into — which is
		// the mode you are in when there is finally something long to read.
		// The seat numbers land on the row that already names the other way to
		// change focus, rather than on one of their own: the budget is hard (17
		// rows, above) and these are one question asked two ways — step to the next
		// seat, or go straight to one. "move" paid for it. The numbers are
		// POSITIONS, left to right, so they renumber when a seat folds out; the
		// line says "by position" rather than pretending a seat owns a number.
		"  tab / 1-4    focus between columns — in compose too; 1-4 goes straight to a seat, by position",
		"  ↑ ↓ / j k    scroll the focused column's whole transcript — ↑ ↓ in compose too",
		"  pgup/pgdn    scroll by a screenful, in compose too (space = pgdn in view mode);",
		// The turn keys land on the row that already holds the other jumps rather
		// than on one of their own, because the budget is hard (17 rows, above)
		// and this is the row a reader is already on when they are looking for a
		// way to move by something bigger than a line. "jump to the" went to pay
		// for it: the line above says `scroll`, so what g / G do is unambiguous
		// without the verb, and a key documented below the fold is a key nobody
		// finds (§9.20).
		"               g / G first turn or newest; [ ] step one turn at a time",
		// `t` lands on the row that already holds `f` rather than on one of its
		// own, because the budget is hard (17 rows, above) and these two are the
		// same question asked twice: how much of the room is the reading area.
		// `f` gives one seat the width; `t` gives one turn the room. A reader
		// looking for either is looking for the other, and the merge is what §9.15
		// made of y/Y and §9.20 made of g/G and [ ] — a category, not a saving.
		// "expand"/"the focused column" paid for it: the words above already say
		// column, and a key documented below the fold is a key nobody finds.
		"  f / t        f gives one column the full width; t gives one turn the whole room (in compose, text)",
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
		// Both of these named --write alone until /write existed. The flag was
		// the only way to reach either badge and now it is not, so a legend
		// still crediting it would send a reader looking for a relaunch — the
		// §9.17 defect, reappearing in the glossary that explains the thing.
		{SandboxWrite, []string{
			"--write or /write: this column may edit and run in the workspace",
		}},
		{SandboxGated, []string{
			"--write or /write, and this seat asks first — y approves, n denies",
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
func helpPostures(st State, lay Layout, sty Styles, g Glyphs) []string {
	lines := []string{
		helpTitle("what the badge on each column means", lay, sty, g),
		"",
		"  Each column states its own posture; there is no room-wide claim.",
		"",
	}

	for _, e := range helpBadgeGloss() {
		b := SandboxClaim{Level: e.level}.Badge()
		// The badge renders in the SAME style it wears on a column, from the
		// same function, so the legend cannot teach one weight and the room
		// show another. `unsandboxed` and `WRITES` are loud here too.
		head := sty.ForSandbox(e.level).Render(b) +
			strings.Repeat(" ", maxInt(1, helpIndent-2-len(b)))
		lines = append(lines, "  "+head+sty.Text.Render(e.gloss[0]))
		for _, l := range e.gloss[1:] {
			lines = append(lines, helpHang+sty.Text.Render(l))
		}
	}

	lines = append(lines,
		"",
		// The load-bearing sentence on this page, and the reason the page is not
		// just a glossary. Every badge above is a claim about a FLAG; none of
		// them is what keeps this room from touching something it should not.
		"  What contains this room is the WORKSPACE above, not any of these words.",
		"  Point council at a throwaway worktree when that matters.",
		// No blank row above the way out, and that is a trade rather than an
		// oversight. It is wedged against the sentence before it and it should
		// not be — but `?` sits at exactly row 17 of a 17-row budget, so a blank
		// here is a row that comes straight out of the legend this page exists
		// for. §9.11's ranking settles it: a rule outranks a blank, the title now
		// carries one, and air is the boundary strength this panel can afford to
		// go without. If the budget ever loosens, this is the first row to spend.
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
		// Hung at the legend's own continuation indent, so a seat's detail sits
		// UNDER the name it belongs to. It used to hang at six cells while its
		// own label started at fifteen — the child ten cells LEFT of its parent,
		// which reads as a new statement rather than as the reason for the one
		// above it. Every card in this room has had one grammar since §9.11 (a
		// title at weight, its body hanging under it) and this was the last place
		// still drawing the shape that rule was written to remove.
		body := maxInt(20, lay.Width-2*framePad-helpIndent)
		for _, l := range wrap(c.Sandbox.Detail, body) {
			seats = append(seats, sty.Muted.Render(helpHang+l))
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
		if gloss, ok := helpGranGloss()[c.Gran]; ok {
			for _, l := range wrap(gloss, body) {
				seats = append(seats, sty.Muted.Render(helpHang+l))
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
