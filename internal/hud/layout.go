package hud

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Tier is a responsive breakpoint. Breakpoints are on width only and the
// shedding order is fixed, so the layout at any width is a pure function of
// that width — which is what makes every golden reproducible.
type Tier uint8

const (
	TierFloor Tier = iota
	TierNarrow
	TierCompact
	TierWide
)

// Grid constants. Widths are fixed; SESSION is the only flexible column and
// absorbs all slack, which right-anchors the numeric block at every width.
const (
	MinWidth  = 60
	MinHeight = 6

	// fullChromeHeight is where the rules and the column-header row survive.
	// Below it the HUD keeps the header, the rows and the footer only.
	fullChromeHeight = 9

	// wideBreak is also where the quota block stops fitting beside the
	// identity block and wraps to its own header line.
	wideBreak    = 100
	compactBreak = 80

	// modelWidth is never narrowed. "gpt-5.1-codex" is exactly 13 columns, and
	// truncating a model name to "gpt-5.1-c…" destroys the one field a user
	// scans to answer "which of my agents is this?".
	modelWidth = 13

	// pctWidth is 6, not the 5 that "99.9%" needs, because a DERIVED value
	// renders with a leading estimate marker ("~69.8%"). ADR-001 requires
	// inferred values be visibly marked rather than mixed in with reported
	// ones, and the marker needs a column to live in.
	pctWidth = 6

	costWidth = 7
	ageWidth  = 4

	wideGauge    = 12
	compactGauge = 8

	// quotaGauge is the header block's bar. Narrower than a row's because the
	// header holds two windows plus their countdowns.
	quotaGauge = 8

	minSession = 8
)

// Layout is the resolved column plan for one frame.
type Layout struct {
	Tier     Tier
	Width    int
	Session  int
	Gauge    int // 0 when the tier has shed the gauge
	ShowCtx  bool
	ShowCost bool
}

// tierFor maps a width to its breakpoint.
func tierFor(width int) Tier {
	switch {
	case width < MinWidth:
		return TierFloor
	case width < compactBreak:
		return TierNarrow
	case width < wideBreak:
		return TierCompact
	default:
		return TierWide
	}
}

// resolveLayout plans the columns.
//
// The shedding order is cost, then the gauge, then nothing else: the gauge is
// a redundant encoding of a number that stays on screen, and the model is
// identity, which nothing else supplies.
//
// hasCtx and hasCost come from the visible rows, not from the vendor's
// capabilities. A column that would render an em dash for EVERY visible row is
// dropped and its width returned to SESSION — a full column of dashes is
// noise, not information. It is computed per frame and is therefore
// deterministic.
func resolveLayout(width int, hasCtx, hasCost bool) Layout {
	l := Layout{Tier: tierFor(width), Width: width}
	if l.Tier == TierFloor {
		return l
	}

	switch l.Tier {
	case TierWide:
		l.Gauge = wideGauge
		l.ShowCost = hasCost
	case TierCompact:
		l.Gauge = compactGauge
	case TierNarrow:
		l.Gauge = 0
	}
	l.ShowCtx = hasCtx

	l.Session = width - l.overhead()
	if l.Session < minSession {
		l.Session = minSession
	}
	return l
}

// overhead is every column except SESSION, including the gaps between them.
//
//	pad dot _ vendor _ sep _ [SESSION] __ model __ gauge _ pct __ cost _ sep _ age pad
func (l Layout) overhead() int {
	n := 1 + 1 + 1 + 2 + 1 + 1 + 1 // pad, dot, gap, vendor, gap, sep, gap
	n += 2 + modelWidth
	if l.ShowCtx {
		if l.Gauge > 0 {
			n += 2 + l.Gauge
		}
		n += 1 + pctWidth
	}
	if l.ShowCost {
		n += 2 + costWidth
	}
	n += 1 + 1 + 1 + ageWidth // gap, sep, gap, age
	n++                       // trailing pad
	return n
}

// padRight left-aligns plain text in a fixed cell, truncating with the
// ellipsis glyph when it does not fit.
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

// padLeft right-aligns plain text in a fixed cell.
func padLeft(s string, w int, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w, g.Ellipsis)
	if d := w - lipgloss.Width(s); d > 0 {
		s = strings.Repeat(" ", d) + s
	}
	return s
}

// truncate cuts a string to a display width, appending the ellipsis glyph.
//
// Width is measured with lipgloss.Width and never len(): the label column
// carries arbitrary project names.
func truncate(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ell)
	if ew >= w {
		// No room for content plus a marker; show as much of the marker as
		// fits rather than nothing.
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

// elideLeft trims a path from the LEFT, which is where the uninformative part
// of a filesystem path lives.
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
