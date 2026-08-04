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
	gutter = 1

	// minColumn is the narrowest a column may be before the tier drops.
	minColumn = 24

	// promptRows is the footer: the rule, the prompt line, the mode line.
	promptRows = 3
	// headerRows is the title line plus its rule.
	headerRows = 2
)

// Layout is the resolved plan for one frame.
type Layout struct {
	Tier  Tier
	Width int
	// Cols is how many columns are drawn side by side (1 in TierTabs).
	Cols int
	// ColWidth is the usable text width inside one column.
	ColWidth int
	// Body is how many rows the column bodies get.
	Body int
}

func tierFor(width, cols int) Tier {
	switch {
	case width < MinWidth:
		return TierFloor
	case cols <= 1 || width < columnsBreak:
		return TierTabs
	default:
		return TierColumns
	}
}

// resolveLayout plans the frame.
//
// n is the number of columns to seat. The separators cost (n-1) cells plus a
// gutter each side; whatever is left divides evenly, and the remainder goes to
// the focused column rather than being scattered, so the widths are stable
// between frames instead of shimmering by one cell as focus moves.
func resolveLayout(width, height, n int) Layout {
	l := Layout{Tier: tierFor(width, n), Width: width}
	if l.Tier == TierFloor {
		return l
	}

	l.Body = height - headerRows - promptRows
	if l.Tier == TierTabs {
		l.Body-- // the tab bar
	}
	if l.Body < 1 {
		l.Body = 1
	}

	if l.Tier == TierTabs {
		l.Cols, l.ColWidth = 1, width-2 // one pad each side
		if l.ColWidth < 1 {
			l.ColWidth = 1
		}
		return l
	}

	// chrome: a pad each side, plus per interior seam a separator and its two
	// gutters.
	chrome := 2 + (n-1)*(1+2*gutter)
	avail := width - chrome
	if avail/n < minColumn {
		// Three columns would each fall under the readability floor. Tabs.
		l.Tier = TierTabs
		l.Cols, l.ColWidth = 1, width-2
		l.Body-- // the tab bar
		if l.Body < 1 {
			l.Body = 1
		}
		return l
	}
	l.Cols, l.ColWidth = n, avail/n
	return l
}

// extraFor returns the leftover cells given to the focused column, so the row
// always fills the terminal exactly rather than leaving a ragged right edge.
func (l Layout) extraFor(idx int) int {
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
