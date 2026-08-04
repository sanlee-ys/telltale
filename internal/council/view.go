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
		b.WriteString(fit(" "+sty.Muted.Render(collapsedNotice(st, g)), st.Width))
		b.WriteString("\n")
	}

	if st.Help {
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
	return resolveLayoutIn(layoutInput{
		Width:    st.Width,
		Height:   st.Height,
		Cols:     len(st.VisibleColumns()),
		Expanded: st.Expanded,
		Composer: composerRows(st, g),
		Notice:   collapsedNotice(st, g) != "",
	})
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
	left := sty.Identity.Render("council")
	if st.Write {
		// Persistent, not a one-off notice. A notice scrolls away and a badge
		// can be missed while reading a column; the state it describes lasts
		// the whole session, so its marker does too.
		left += " " + sty.SevCrit.Render(g.Warn+" WRITE")
	}

	round := "no turn yet"
	if st.Turn > 0 {
		round = "turn " + strconv.Itoa(st.Turn)
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
	// uninformative part of a path is its prefix.
	used := lipgloss.Width(left) + lipgloss.Width(right) + 4
	pathw := lay.Width - used
	mid := ""
	if pathw > 3 {
		mid = sty.Muted.Render(elideLeft(displayPath(st), pathw, g.Ellipsis))
	}

	gap := lay.Width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}
	return " " + left + " " + mid + strings.Repeat(" ", gap) + right + " "
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
func columnsBody(st State, lay Layout, sty Styles, g Glyphs) string {
	vis := st.VisibleColumns()
	cells := make([][]string, len(vis))
	for j, idx := range vis {
		// extraFor is indexed by POSITION in the row, not by seat: the leftover
		// cells go to the leftmost drawn column, and a collapsed seat has no
		// position to give them to.
		w := lay.ColWidth + lay.extraFor(j)
		cells[j] = columnCell(st, st.Columns[idx], idx == st.Focus, w, lay.Body, sty, g)
	}

	sep := " " + sty.Rule().Render(g.Sep) + " "
	var b strings.Builder
	for row := 0; row < lay.Body; row++ {
		b.WriteString(" ")
		for j := range vis {
			if j > 0 {
				b.WriteString(sep)
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

// columnCell renders one column to exactly h lines of exactly w cells.
//
// Returning a fixed rectangle is what keeps the side-by-side join honest: a
// short column pads rather than collapsing, so a vendor that has said nothing
// yet occupies its seat instead of letting its neighbours slide left.
func columnCell(st State, c Column, focused bool, w, h int, sty Styles, g Glyphs) []string {
	lines := make([]string, 0, h)

	lines = append(lines, fit(columnHeader(st, c, focused, w, sty, g), w))
	// The badge line is CHROME, not body, and that is a safety decision rather
	// than a layout one. It carries the sandbox claim, and the first version of
	// this scrolled it away with the text — so a user reading the middle of a
	// long reply from the unsandboxed column had nothing on screen telling them
	// that column can write to their tree. A claim that disappears when you
	// read is not a claim.
	if len(lines) < h {
		if b := badgeLine(c); b != "" {
			lines = append(lines, sty.Muted.Render(padRight(b, w, g)))
		}
	}
	// The approval card is CHROME too, and for a stronger version of the badge
	// line's reason. The badge must not scroll away because a claim you cannot
	// see is not a claim; this must not scroll away because a vendor is STOPPED
	// behind it. During a turn every column is following its own tail, so a card
	// in the body would be pushed off screen by the output of the very call it
	// is asking about.
	for _, l := range gateCard(st, c, w, sty, g) {
		if len(lines) >= h {
			break
		}
		lines = append(lines, fit(l, w))
	}
	if len(lines) < h {
		lines = append(lines, sty.Muted.Render(strings.Repeat(g.Rule, w)))
	}

	body := columnText(st, c, w, sty, g)
	avail := h - len(lines)
	win, above, below := scrollWindow(c, body, avail)

	for i, l := range win {
		// The overflow markers replace the first and last visible lines rather
		// than sitting outside the body, because the body area is the whole
		// budget. Spending a line to say "there is more" is worth it: silent
		// clipping is indistinguishable from a vendor that stopped talking,
		// which is exactly the ambiguity §4a.1 forbids.
		switch {
		case i == 0 && above > 0:
			lines = append(lines, sty.Muted.Render(padRight(
				g.Up+" "+strconv.Itoa(above)+" more above", w, g)))
		case i == len(win)-1 && below > 0:
			lines = append(lines, sty.Muted.Render(padRight(
				g.Down+" "+strconv.Itoa(below)+" more below", w, g)))
		default:
			// fit, not padRight, and this is the ANSI trap §9.5 records rather
			// than a stylistic choice. Body lines can now carry style — the
			// outcome mark on a trace entry is coloured — and padRight
			// truncates rune by rune, so it would cut through an escape
			// sequence and count escape bytes as width. Goldens render with
			// PlainStyles and are blind to that, which is exactly why the rule
			// is enforced by the function used rather than by review.
			lines = append(lines, fit(l, w))
		}
	}

	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
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
	w := lay.ColWidth
	if lay.Tier == TierColumns {
		w += lay.extraFor(pos)
	}
	// PlainStyles because only the line COUNT is wanted here, and styling
	// cannot change it — every style in this package is a wrapper, never a
	// re-wrap.
	sty, gl := PlainStyles(), GlyphsFor(st.ASCII)
	// Three lines of the cell are chrome — header, badge, rule — plus the
	// approval card when one is up. Counted from the same function that draws
	// it rather than from a constant: a card of a different height would
	// otherwise let the tail scroll past the end of the content.
	avail := lay.Body - 3 - len(gateCard(st, st.Columns[idx], w, sty, gl))
	n := len(columnText(st, st.Columns[idx], w, sty, gl))
	if m := n - avail; m > 0 {
		return m
	}
	return 0
}

// columnHeader is the vendor name plus the two claims this product refuses to
// leave implicit: what its sandbox actually is, and how finely it reports.
func columnHeader(st State, c Column, focused bool, w int, sty Styles, g Glyphs) string {
	name := c.Label
	if focused {
		name = g.Focus + name
	} else {
		name = " " + name
	}

	status := c.Phase.String()
	if c.Avail != AvailInstalled {
		status = "unavailable"
	} else if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		// The clock is the answer to "why is this one taking so long".
		// Without it a final-only vendor is a blank column and a spinner,
		// which reads as broken rather than slow — and two of the three
		// vendors here are final-only, so that ambiguity is the common case
		// rather than an edge one.
		status = status + " " + elapsed(st, c)
		if len(g.Spinner) > 0 {
			status = g.Spinner[st.Spinner%len(g.Spinner)] + " " + status
		}
	} else if c.Elapsed > 0 {
		// Kept after the turn ends. A finished column should still be able to
		// say how long it made you wait, which is the only way the asymmetry
		// between a streaming vendor and a final-only one is ever legible.
		status = status + " " + dur(c.Elapsed)
	}

	left := sty.Identity.Render(padRight(name, maxInt(1, w-lipgloss.Width(status)-1), g))
	right := sty.ForPhase(c.Phase).Render(status)
	if c.Avail != AvailInstalled {
		right = sty.SevWarn.Render(status)
	}
	head := left + " " + right

	// The badges get their own line when there is room; below that they are the
	// first thing dropped, because a claim nobody can read is not a claim.
	if w < 20 {
		return truncate(head, w, g.Ellipsis)
	}
	return head
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
		return unavailableCard(c, w, g)
	}

	var out []string
	for _, h := range c.History {
		out = append(out, turnHead(h.N, historyMeta(h), h.Prompt, h.Quoted, w, sty, g)...)
		out = append(out, pastTurn(h, w, sty, g)...)
		out = append(out, "")
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
		out = append(out, reattachCard(st, c, w)...)
	case c.Phase == PhaseIdle && c.Body == "":
		out = append(out, wrap("no turn dispatched yet.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "" && len(c.Acts) > 0:
		// It has acted but not spoken. Saying "no incremental output" here
		// would contradict the trace directly above it.
		out = append(out, wrap("working — the steps above are what it has done so far.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "" && c.Gran == GranUnknown:
		// A separate sentence from the one below, because they are different
		// claims. "Reports no incremental output" is a measurement two vendors
		// earned; a column whose granularity was never established must not
		// borrow it. This one says only what is true: it is running, and
		// whether anything will appear before the end is not known.
		out = append(out, wrap("working. whether this vendor reports incremental output has not been established, so output may not appear until the turn finishes.", w)...)
	case c.Phase == PhaseWaiting && c.Body == "":
		// The honest version of an empty streaming column. This vendor is
		// working; it just does not report anything until it is done, and
		// pretending otherwise would be a fabricated progress signal.
		out = append(out, wrap("working. this vendor reports no incremental output, so nothing appears until the turn finishes.", w)...)
	default:
		out = append(out, wrap(c.Body, w)...)
	}

	if c.Note != "" {
		out = append(out, "")
		out = append(out, wrap(g.Warn+" "+c.Note, w)...)
	}
	return out
}

// turnHead opens one turn: the separator naming it, then the brief that
// produced it.
func turnHead(n int, meta, prompt string, quoted bool, w int, sty Styles, g Glyphs) []string {
	out := []string{sty.Muted.Render(padRight(turnRule(n, meta, w, g), w, g))}
	return append(out, promptEcho(prompt, quoted, w, sty, g)...)
}

// turnRule is the separator line: "turn 3 ───────────  12s  $0.0123".
//
// The meta is dropped before the label when the column is too narrow for both.
// Which turn this is outranks how long it took: without the number the reply
// above and the reply below are one undifferentiated wall, which is the state
// this whole feature exists to leave.
func turnRule(n int, meta string, w int, g Glyphs) string {
	label := "turn " + strconv.Itoa(n)
	// The rule takes whatever the label and the meta leave: one space after the
	// label always, one before the meta when there is one.
	fill := func(m string) int {
		if m == "" {
			return w - lipgloss.Width(label) - 1
		}
		return w - lipgloss.Width(label) - lipgloss.Width(m) - 2
	}
	n2 := fill(meta)
	if n2 < 1 && meta != "" {
		meta = ""
		n2 = fill(meta)
	}
	if n2 < 1 {
		return label
	}
	s := label + " " + strings.Repeat(g.Rule, n2)
	if meta != "" {
		s += " " + meta
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
	body := wrap(prompt, maxInt(1, w-2))
	out := make([]string, 0, len(body)+1)
	for i, l := range body {
		prefix := "  "
		if i == 0 {
			prefix = g.Prompt + " "
		}
		out = append(out, sty.Identity.Render(padRight(prefix+l, w, g)))
	}
	if quoted {
		// What the seat ACTUALLY received on a rebuttal turn is this brief with
		// the other seats' answers fenced in front of it. Those are not the
		// principal's words, so they are reported rather than echoed — the line
		// above stays the user's, and this one says what rode along with it.
		for _, l := range wrap("+ the other seats' last answers were quoted to this one", maxInt(1, w-2)) {
			out = append(out, sty.Muted.Render(padRight("  "+l, w, g)))
		}
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
		out = append(out, wrap(g.Warn+" "+h.Note, w)...)
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
	text := g.Act + " " + a.Text
	if mark != "" {
		text += " " + mark
	}

	// Wrapped as PLAIN text and styled afterwards, never the other way round:
	// wrap measures with lipgloss.Width but splits on spaces, and an escape
	// sequence pushed through it would be broken across two lines.
	lines := wrap(text, w)
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
		for _, l := range wrap(a.Detail, maxInt(1, w-2)) {
			lines = append(lines, sty.Muted.Render("  "+l))
		}
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

	out := wrap(g.Warn+" waiting on you: "+mine[0].Text, w)
	for i := range out {
		out[i] = sty.SevWarn.Render(padRight(out[i], w, g))
	}

	keys := "y approve   n deny"
	if n := len(mine) - 1; n > 0 {
		keys += "   +" + strconv.Itoa(n) + " queued"
	}
	// The keys are repeated here AND in the mode line on purpose. The mode line
	// is the contract — it announces what every key means on every frame — and
	// this is the copy that sits next to the thing being decided, where the eye
	// already is.
	for _, l := range wrap(keys, w) {
		out = append(out, sty.Identity.Render(padRight(l, w, g)))
	}
	return out
}

// badgeLine is the sandbox claim, the streaming granularity, and the cost.
//
// Cost renders only when the vendor REPORTED one. A turn that reported zero
// shows $0.0000; a turn that reported nothing shows no cost cell at all. Those
// are different facts, and deriving a figure from token counts is on this
// repo's deliberately-rejected list (design.md §8) — council does not get to
// invent dollars either.
func badgeLine(c Column) string {
	parts := []string{}
	if b := c.Sandbox.Badge(); b != "" {
		parts = append(parts, b)
	}
	if s := c.Gran.String(); s != "" {
		parts = append(parts, s)
	}
	if c.CostUSD != nil {
		cost := "$" + strconv.FormatFloat(*c.CostUSD, 'f', 4, 64)
		if c.CostSession {
			// A word, not a symbol, and not a colour. A seat kept alive across
			// turns reports its running total; the cell has always meant "this
			// turn" everywhere else in this room, and two different quantities
			// sharing one rendering is the ambiguity §4a.1 forbids.
			cost += " session"
		}
		parts = append(parts, cost)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "  ")
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
// Two versions, one per seat, and the split is the honest part. A room can
// reattach with only some of its seats restored: a vendor that never answered
// left no id, and a vendor installed since the room was saved was never in it.
// Both open beside seats that DO continue, and one shared sentence would let
// either be read as continuing something.
//
// No warning glyph. A reattach is the feature working, not a problem, and
// spending the ⚠ on it would blunt the mark that carries real failures — the
// same argument ActDenied makes for SevWarn over SevCrit.
func reattachCard(st State, c Column, w int) []string {
	// The age comes off State.Now, never a clock, so this stays pure and the
	// goldens stay reproducible — the same contract elapsed() renders under.
	when := ""
	if !st.Now.IsZero() {
		when = ", saved " + age(st.Now.Sub(st.Reattached.SavedAt))
	}
	out := wrap("reattached — turn "+strconv.Itoa(st.Reattached.Turn)+
		" was the last"+when+".", w)
	out = append(out, "")
	if c.Restored {
		// "continues it" rather than "resumes it": the resume is the vendor's
		// own mechanism and it has not been asked yet. What the room can promise
		// is where the next brief is addressed.
		out = append(out, wrap("this seat's thread came back. the next brief continues it.", w)...)
		return out
	}
	out = append(out, wrap("no thread came back for this seat. its next brief opens a new session, with the brief re-applied.", w)...)
	return out
}

// unavailableCard says which failure this is and what would fix it. Absence and
// unusability are different facts and get different words — the HUD's rule that
// a dropped column and an em dash must not read alike (§4a.1), applied here.
func unavailableCard(c Column, w int, g Glyphs) []string {
	out := wrap(g.Warn+" "+c.Label+" is not seated", w)
	if c.Note != "" {
		out = append(out, "")
		out = append(out, wrap(c.Note, w)...)
	}
	out = append(out, "")
	out = append(out, wrap("the other columns dispatch normally.", w)...)
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
		if idx == st.Focus {
			parts = append(parts, sty.Identity.Render(g.Focus+label))
		} else {
			parts = append(parts, sty.Muted.Render(" "+label))
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
	// focused=false: the tab bar directly above already carries the marker, and
	// a second one on the only visible column is noise.
	cell := columnCell(st, st.Columns[idx], false, lay.ColWidth, lay.Body, sty, g)
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
		return padRows([]string{
			" " + sty.Muted.Render(prefix) +
				sty.Muted.Render(padRight("type a brief — goes to claude; @codex, @agy or @all to widen"+g.Caret, w, g)) + " ",
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
func modeLine(st State, lay Layout, sty Styles, g Glyphs) string {
	var left, right string
	switch {
	case st.Gating():
		// Outranks both other modes, because it is the only state in this room
		// where something is STOPPED until a key is pressed. The notice is not
		// allowed to overwrite this line the way it overwrites the others: a
		// transient message displacing the two keys that unblock a vendor is
		// exactly the surprise §7.8 exists to forbid.
		left = "GATE"
		right = gateLabel(st) + "  " + g.Sep + "  y approve  " + g.Sep +
			"  n deny  " + g.Sep + "  ctrl+c cancel the turn"
		l := sty.SevWarn.Render(left)
		r := sty.Muted.Render(right)
		gap := lay.Width - lipgloss.Width(l) - lipgloss.Width(r) - 2
		if gap < 1 {
			gap = 1
			r = sty.Muted.Render(truncate(right,
				maxInt(1, lay.Width-lipgloss.Width(l)-3), g.Ellipsis))
		}
		return " " + l + strings.Repeat(" ", gap) + r + " "
	}

	switch st.Mode {
	case ModeComposing:
		left = "COMPOSE"
		// The routing is stated before the keybindings because it is the one
		// thing on this line that changes what enter DOES. An @typo has to read
		// as "this is going to everyone" while there is still time to fix it;
		// discovering it afterwards means a wasted turn against three quotas.
		// ^j is stated beside enter because the two are one decision: a key that
		// adds a line and a key that spends four quotas sit next to each other on
		// the keyboard, and a composer you can write a paragraph in is useless if
		// nobody can find out how.
		right = "→ " + routeLabel(st) + quoteTag(st) + "  " + g.Sep +
			"  enter dispatch  " + g.Sep + "  ^j newline  " + g.Sep + "  ^r rebut"
	default:
		left = "VIEW"
		scroll := g.Up + g.Down + " scroll  " + g.Sep + "  f expand  " + g.Sep + "  "
		if st.Busy() {
			right = scroll + "tab focus  " + g.Sep + "  ctrl+c cancel  " + g.Sep + "  ? help"
		} else {
			right = scroll + "tab focus  " + g.Sep + "  i compose  " + g.Sep + "  ? help  " + g.Sep + "  q quit"
		}
	}
	if st.Notice != "" {
		right = g.Warn + " " + st.Notice
	}

	l := sty.Identity.Render(left)
	r := sty.Muted.Render(right)
	gap := lay.Width - lipgloss.Width(l) - lipgloss.Width(r) - 2
	if gap < 1 {
		gap = 1
		r = truncate(right, maxInt(1, lay.Width-lipgloss.Width(l)-3), g.Ellipsis)
		r = sty.Muted.Render(r)
	}
	return " " + l + strings.Repeat(" ", gap) + r + " "
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
func routeLabel(st State) string {
	if len(st.Route) == 0 {
		return "everyone"
	}
	return strings.Join(st.Route.labels(), ", ")
}

// helpBody replaces the column area, rather than floating over it, for the same
// reason the HUD's overlay does: a panel that covers live output hides the
// thing the user is watching.
func helpBody(st State, lay Layout, sty Styles, g Glyphs) string {
	lines := []string{
		sty.Identity.Render("council") + sty.Muted.Render(" — one brief, several agents, side by side"),
		"",
		"  i / enter    compose a brief",
		"  enter        dispatch — to claude, or to whoever is @mentioned",
		"  ctrl+j       newline in the brief — the compose area grows to six rows",
		"  @codex       address a lane: @claude, @codex, @agy, @cursor, @all",
		"               unaddressed goes to claude alone; the others are review,",
		"               IDE and tiebreak lanes. Leading mentions only: \"ask @claude\" is prose",
		"  y / n        approve or deny a tool call a vendor is blocked on (--write)",
		"  esc          leave compose (the draft is kept)",
		"  tab          move focus between columns",
		"  ↑ ↓ / j k    scroll the focused column's whole transcript",
		// Two keys on one line, because this panel has to fit a 24-row terminal
		// and the line it was competing with is "? this help" — which toggles,
		// and is therefore the only documented way back out of here.
		"  pgup/pgdn    scroll by a screenful (space = pgdn); g / G first turn or newest",
		"  f            expand the focused column to the full width",
		"  ctrl+r       arm rebuttal: each vendor sees the others' last answers,",
		"               fenced and labelled as untrusted. Turn 1 is always blind.",
		"  ctrl+c       cancel the turn in flight, or quit when idle",
		"  q            quit (in view mode only — in compose it is the letter q)",
		"  ?            this help",
		"",
		"  a seat that is not installed folds out of the grid and is named in one",
		"  line under the header. --vendor all keeps every seat on screen;",
		"  --vendor claude,codex seats exactly those.",
		"",
		// Below the fold at the minimum height, and left in for the same reason
		// the help lists keys it expects you to already know: at a normal
		// terminal size it is the paragraph that explains why the badges differ.
		sty.Muted.Render("  council dispatches to vendor CLIs. It is the one telltale mode that"),
		sty.Muted.Render("  does, and each column states its own posture rather than the room"),
		sty.Muted.Render("  claiming one on behalf of every seat. Only one seat can be asked"),
		sty.Muted.Render("  before it acts; the rest say so instead of implying otherwise."),
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
