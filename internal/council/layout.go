package council

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Tier is a responsive breakpoint, on width only, with a fixed shedding order —
// same contract as the HUD's, so the layout at any width is a pure function of
// that width and every golden is reproducible.
type Tier uint8

const (
	// TierFloor: too narrow to draw the room at all.
	TierFloor Tier = iota
	// TierTabs: one column at a time, with a tab bar.
	TierTabs
	// TierColumns: every seated column side by side.
	TierColumns
)

const (
	// MinWidth is the floor. Below it a column would be too narrow to read a
	// sentence in, and three of them would be shredded prose.
	MinWidth = 60
	// MinHeight leaves room for header, one body line, and the prompt footer.
	MinHeight = 10

	// columnsBreak is where three columns stop being readable. At 96 each
	// column gets ~30 usable cells, which is about six words — the point below
	// which side-by-side comparison stops beating tabs.
	columnsBreak = 96

	// gutter is the space each side of the vertical separator between columns.
	//
	// Two, not one: a single cell left prose welded to the │ on Windows
	// Terminal (the reference host), which is the "rigid / cramped" read the
	// screenshot pass named. One extra cell each side costs ~1–2 cells of
	// wrap width per column at four-up and buys the air the frame was missing.
	// columnsBody must use the same constant — ColWidth math and the painted
	// sep have to agree or the row overflows the terminal.
	gutter = 2

	// minColumn is the narrowest a PRIMARY column may be before the tier drops.
	minColumn = 24

	// stripColumn is the width of a seat that is on screen but not owning the
	// frame this turn (unaddressed under a narrow route). Wide enough for a
	// two-letter vendor tag + phase mark + the phase word; narrow enough that
	// the addressed seats get real reading width. Intent-controlled — see
	// State.FrameOwners.
	stripColumn = 14

	// stripWidth is the width at or below which a column stops rendering as a
	// seat and renders as a STRIP: identity collapses to its two-letter vendor
	// tag, the clock and the cost leave, and the badge row keeps only a posture
	// word that fits whole (view.go, stripHeader / stripBadges).
	//
	// It is stripColumn itself, and the arithmetic is why it cannot be anything
	// else. The widest thing a header must still say WHOLE is its phase word —
	// `streaming` and `cancelled` are nine cells — and the mark in front of it
	// costs two more. Fourteen minus eleven is three: exactly a two-letter tag
	// and the space after it. One more cell of indent and the tag would not fit;
	// one fewer cell of column and the phase word would clip, which §9.11 rules
	// is not a word at all.
	//
	// Nothing else in this package can land here. A PRIMARY column never falls
	// below minColumn (24) — the tier drops to tabs first — and a tabbed column
	// is the frame minus two pads, so MinWidth already floors it at 58. So a
	// column at or under this width is a strip, and no second predicate is
	// needed to say so.
	stripWidth = stripColumn

	// promptChrome is the fixed part of the footer: the rule above the
	// composer, and the mode line below it. The composer itself is variable —
	// see Layout.Prompt.
	promptChrome = 2
	// headerRows is the title line plus its rule.
	headerRows = 2

	// maxComposerRows is how tall the compose area may grow.
	//
	// Six, because a brief worth sending to four agents is a paragraph and one
	// elided line was not somewhere anyone could think. It is a ceiling rather
	// than the height: the composer is as tall as the draft needs and no
	// taller, so a room nobody is typing in looks exactly as it always did.
	// Body pays for the difference, which is why the ceiling exists at all.
	maxComposerRows = 6
)

// Layout is the resolved plan for one frame.
type Layout struct {
	Tier  Tier
	Width int
	// Cols is how many columns are drawn side by side (1 in TierTabs).
	Cols int
	// ColWidth is the usable text width inside one column when the frame is
	// equal (or the tabbed single column). Weighted frames use ColWidths.
	ColWidth int
	// ColWidths is per drawn column when FrameOwners narrows the turn. Nil
	// means every column uses ColWidth (+ extraFor on the leftmost).
	ColWidths []int
	// Body is how many rows the column bodies get.
	Body int
	// Tabs reports that a tab bar is drawn above the body.
	//
	// Not the same as TierTabs. A room with ONE seat on screen is the tabs tier
	// — there is nothing to put side by side — but a bar holding a single tab
	// selects nothing and names the column the header underneath already names.
	// That used to be a rarity; collapsing the seats that cannot be driven made
	// it the ordinary room on a machine with one vendor installed.
	Tabs bool
	// Prompt is how many rows the compose area gets, at least 1.
	Prompt int
	// Notice is 1 when a row under the header names the seats that were
	// collapsed out of the grid, 0 otherwise.
	Notice int
}

// layoutInput is everything the frame plan is computed from.
//
// A struct rather than a longer parameter list because the last two arguments
// are both small integers with no natural order, and a caller that swapped them
// would produce a plausible frame rather than a compile error.
type layoutInput struct {
	Width, Height int
	// Cols is how many columns are DRAWN — the visible seats, not every
	// detected one.
	Cols     int
	Expanded bool
	// Composer is how many rows the draft wants, before the height floor.
	Composer int
	// Notice reports that the collapsed-seat line is on screen.
	Notice bool
	// Primary marks which of the Cols drawn columns own the frame this turn.
	// Nil, empty, or all-true means equal widths. When set, length must equal
	// Cols; false entries get stripColumn and the rest share what remains.
	Primary []bool
}

func tierFor(width, cols int, expanded bool) Tier {
	switch {
	case width < MinWidth:
		return TierFloor
	// Expanded is a deliberate request for one column at full width, so it
	// outranks the width breakpoint rather than competing with it.
	case expanded || cols <= 1 || width < columnsBreak:
		return TierTabs
	default:
		return TierColumns
	}
}

// resolveLayout plans a frame with a one-row composer and no notice row.
//
// The narrow entry point, kept because most of what asks about layout is asking
// about widths, which the composer cannot change.
func resolveLayout(width, height, n int, expanded bool) Layout {
	return resolveLayoutIn(layoutInput{Width: width, Height: height, Cols: n, Expanded: expanded})
}

// resolveLayoutIn plans the frame.
//
// Cols is the number of columns to seat. The separators cost (n-1) cells plus a
// gutter each side; whatever is left divides evenly, and the remainder goes to
// the LEFTMOST drawn column rather than being scattered — see extraFor, which
// this comment used to describe as giving it to the focused one. It does not,
// and must not: a remainder that followed focus would re-wrap two columns'
// worth of prose on every tab press, which is both a moving cell §7.1 does not
// budget for and a worse way to compare two answers than a stable grid.
//
// The tier is settled BEFORE any row is budgeted, because the tab bar costs a
// row and the fallback from columns to tabs happens on a width test. Budgeting
// first and dropping the tier afterwards is how the old shape worked, and it
// only survived because the composer was a constant: a taller one would have
// overflowed the terminal by exactly the tab bar.
func resolveLayoutIn(in layoutInput) Layout {
	l := Layout{Tier: tierFor(in.Width, in.Cols, in.Expanded), Width: in.Width, Prompt: 1}
	if l.Tier == TierFloor {
		return l
	}

	chrome := 2 + (in.Cols-1)*(1+2*gutter)
	if l.Tier == TierColumns && (in.Width-chrome)/in.Cols < minColumn {
		// Every column would fall under the readability floor. Tabs.
		l.Tier = TierTabs
	}

	l.Tabs = l.Tier == TierTabs && in.Cols > 1
	rows := headerRows + promptChrome
	if l.Tabs {
		rows++
	}
	if in.Notice {
		rows++
	}

	// The composer takes what it wants, then yields to the floor: at the
	// minimum height a six-row draft would leave the columns nothing, and a
	// room where you can type but not read is not the trade anyone asked for.
	l.Prompt = in.Composer
	if l.Prompt > maxComposerRows {
		l.Prompt = maxComposerRows
	}
	if m := in.Height - rows - 1; l.Prompt > m {
		l.Prompt = m
	}
	if l.Prompt < 1 {
		l.Prompt = 1
	}
	if in.Notice {
		l.Notice = 1
	}

	l.Body = in.Height - rows - l.Prompt
	if l.Body < 1 {
		l.Body = 1
	}

	if l.Tier == TierTabs {
		l.Cols, l.ColWidth = 1, in.Width-2 // one pad each side
		if l.ColWidth < 1 {
			l.ColWidth = 1
		}
		return l
	}
	l.Cols = in.Cols
	if widths, ok := weightedWidths(in.Width, in.Cols, in.Primary); ok {
		l.ColWidths = widths
		l.ColWidth = widths[0] // callers that ignore ColWidths stay sane
		return l
	}
	l.ColWidth = (in.Width - chrome) / in.Cols
	return l
}

// weightedWidths apportions usable width when some seats own the frame.
//
// Returns ok=false when the split would leave a primary under minColumn or the
// strips would consume the row — callers fall back to equal columns rather than
// ship an unreadable frame.
func weightedWidths(width, cols int, primary []bool) ([]int, bool) {
	if cols < 2 || len(primary) != cols {
		return nil, false
	}
	nPrim := 0
	for _, p := range primary {
		if p {
			nPrim++
		}
	}
	if nPrim == 0 || nPrim == cols {
		return nil, false
	}
	chrome := 2 + (cols-1)*(1+2*gutter)
	usable := width - chrome
	nStrip := cols - nPrim
	if nStrip*stripColumn >= usable {
		return nil, false
	}
	rem := usable - nStrip*stripColumn
	if rem/nPrim < minColumn {
		return nil, false
	}
	base, leftover := rem/nPrim, rem%nPrim
	out := make([]int, cols)
	firstPrim := -1
	for i, p := range primary {
		if p {
			out[i] = base
			if firstPrim < 0 {
				firstPrim = i
			}
			continue
		}
		out[i] = stripColumn
	}
	if firstPrim >= 0 {
		out[firstPrim] += leftover
	}
	return out, true
}

// widthAt is the usable text width of drawn column idx.
func (l Layout) widthAt(idx int) int {
	if len(l.ColWidths) == l.Cols && idx >= 0 && idx < len(l.ColWidths) {
		return l.ColWidths[idx]
	}
	return l.ColWidth + l.extraFor(idx)
}

// extraFor returns leftover cells for equal frames only. Weighted frames fold
// the remainder into the first primary column inside weightedWidths.
func (l Layout) extraFor(idx int) int {
	if len(l.ColWidths) == l.Cols {
		return 0
	}
	if l.Tier != TierColumns || l.Cols == 0 {
		return 0
	}
	chrome := 2 + (l.Cols-1)*(1+2*gutter)
	rem := (l.Width - chrome) - l.Cols*l.ColWidth
	if idx == 0 {
		return rem
	}
	return 0
}

// wrap breaks text to a display width, measured with lipgloss.Width rather than
// len() because vendor output is arbitrary text.
//
// It breaks on spaces where it can and mid-word where it must — a URL or a Go
// identifier longer than the column has to break somewhere, and dropping it or
// letting it run past the edge are both worse than a hard break. Existing
// newlines are honoured: they are the vendor's paragraphing, and reflowing them
// away would misrepresent the reply.
//
// Contract: every returned line is at most w cells wide, for w >= 2. At w == 1
// a double-width rune cannot be represented at all — there is no correct
// answer, only a choice between dropping content and overflowing — so wrap
// keeps the rune and the caller's padRight truncates it. That path is
// unreachable in a real frame: minColumn is 24, and the tier drops to tabs
// rather than seat a column below it. The frame-level guarantee is asserted
// where it actually matters, over whole rendered frames, in
// TestNoLineExceedsTheTerminalWidth.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(para, w)...)
	}
	return out
}

func wrapLine(s string, w int) []string {
	var out []string
	cur := ""
	curw := 0
	flush := func() {
		out = append(out, cur)
		cur, curw = "", 0
	}
	for _, word := range strings.Split(s, " ") {
		ww := lipgloss.Width(word)
		switch {
		case ww > w:
			// Longer than the column on its own. Emit what is buffered, then
			// hard-break the word across as many lines as it needs.
			if curw > 0 {
				flush()
			}
			for _, chunk := range hardBreak(word, w) {
				out = append(out, chunk)
			}
			// The last chunk stays open so a following short word can join it.
			if len(out) > 0 {
				cur = out[len(out)-1]
				curw = lipgloss.Width(cur)
				out = out[:len(out)-1]
			}
		case curw == 0:
			cur, curw = word, ww
		case curw+1+ww <= w:
			cur += " " + word
			curw += 1 + ww
		default:
			flush()
			cur, curw = word, ww
		}
	}
	if curw > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

func hardBreak(s string, w int) []string {
	var out []string
	cur := strings.Builder{}
	curw := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curw+rw > w {
			out = append(out, cur.String())
			cur.Reset()
			curw = 0
		}
		cur.WriteRune(r)
		curw += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// padRight left-aligns plain text in a fixed cell.
//
// This and its neighbours are duplicated from internal/hud rather than shared.
// The seam between the two surfaces is the normalized session model and
// internal/theme's numbers, and nothing else (cmd/telltale/main.go); exporting
// the HUD's layout internals to reach them here would create exactly the
// coupling that seam exists to prevent, for eighty lines.
func padRight(s string, w int, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w, g.Ellipsis)
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// fit pads or truncates a possibly-STYLED string to exactly w cells.
//
// padRight below may only be used on plain text: it truncates rune by rune, so
// on a string that already carries ANSI escapes it would cut through an escape
// sequence, and its width arithmetic would count the escape bytes as content.
// That failure is invisible to the golden tests, which render with PlainStyles
// by design — so anywhere a line is assembled from differently-styled pieces
// (the tab bar, the help body), the padding has to be ANSI-aware, and this is
// it. lipgloss.Width and MaxWidth both skip escapes.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = lipgloss.NewStyle().MaxWidth(w).Render(s)
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// truncate cuts a string to a display width, appending the ellipsis glyph.
func truncate(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ell)
	if ew >= w {
		return string([]rune(ell)[:1])
	}
	budget := w - ew
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > budget {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + ell
}

// elideLeft trims a path from the LEFT, where the uninformative part lives.
func elideLeft(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ell)
	if ew >= w {
		return string([]rune(ell)[:1])
	}
	budget := w - ew
	runes := []rune(s)
	used := 0
	i := len(runes)
	for i > 0 {
		rw := lipgloss.Width(string(runes[i-1]))
		if used+rw > budget {
			break
		}
		used += rw
		i--
	}
	return ell + string(runes[i:])
}
