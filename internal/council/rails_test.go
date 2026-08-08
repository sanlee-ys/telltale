package council

import (
	"strings"
	"testing"
)

// frameBody is the rows between the header rule and the footer rule — the part
// columnsBody draws.
//
// Found by the two full-width rules rather than by arithmetic over Layout,
// because the point of these assertions is what a reader SEES: a test that
// re-derived the body's row range from the same numbers the renderer used would
// agree with a renderer that had gone wrong. A body row is never all rule glyphs
// (it carries at least the frame pad and a gutter), so the two lines that are
// bracket the body exactly.
func frameBody(t *testing.T, frame string, rule string) []string {
	t.Helper()
	lines := strings.Split(frame, "\n")
	var rules []int
	for i, ln := range lines {
		if ln != "" && strings.Trim(ln, rule) == "" {
			rules = append(rules, i)
		}
	}
	if len(rules) < 2 {
		t.Fatalf("frame has %d full-width rules, want at least 2:\n%s", len(rules), frame)
	}
	return lines[rules[0]+1 : rules[len(rules)-1]]
}

// TestTheRailNeverDashes is the frame the room draws around its columns: it does
// not blink out for a row and come back.
//
// The rule under test is railRows' second half — a LONE blank row between two
// content rows carries the rail, because every deliberate blank on this surface
// is exactly one row (§9.11: chrome to content, speaker change, content-kind
// change) and a frame that breaks at the exact rows the design put air in reads
// as damage. Before this the separator was decided per row on a content
// predicate, so transcript.txt broke at rows 11 and 13, skips-coalesced.txt at
// 5, 10 and 13, and unavailable.txt at 19.
//
// Stated as "no railed row is followed by a bare row that is followed by another
// railed row" rather than as a span, because a span is exactly what this rule
// declines to draw — see railRows on why the literal reading loses.
func TestTheRailNeverDashes(t *testing.T) {
	g := UnicodeGlyphs()
	for _, tc := range []struct {
		name string
		st   func() State
	}{
		// A transcript with interior blank rows and three columns of very
		// different lengths — the shape that stuttered worst.
		{"transcript", talking},
		// Two dead seats whose cards sit at the BOTTOM of their columns.
		{"unavailable", func() State {
			st := room()
			st.Seats = Seats{All: true} // --vendor all, where the full cards live
			st.Columns[1].Avail = AvailNotInstalled
			st.Columns[1].Note = "not found on PATH (looked for codex)"
			st.Columns[1].Sandbox = SandboxClaim{}
			st.Columns[2].Avail = AvailUnusable
			st.Columns[2].Note = "resolves to a shell shim (agy.cmd) and takes its " +
				"prompt as an argument; set TELLTALE_COUNCIL_AGY_BIN to the real executable"
			st.Columns[2].Sandbox = SandboxClaim{}
			return st
		}},
		// A seat that sat several turns out: coalesced skip lines with air
		// between them and the reply above.
		{"skips", func() State { return skipRoom(false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := frameBody(t, render(tc.st()), g.Rule)
			railed := make([]bool, len(body))
			any := false
			for i, ln := range body {
				railed[i] = strings.Contains(ln, g.Sep)
				any = any || railed[i]
			}
			if !any {
				t.Fatal("no rail anywhere in the body; the grid has no frame")
			}
			for i := 1; i < len(body)-1; i++ {
				if !railed[i] && railed[i-1] && railed[i+1] {
					t.Errorf("the rail dashes: body row %d is bare between two railed "+
						"rows\n%s", i, strings.Join(body, "\n"))
				}
			}
		})
	}
}

// TestRailsDoNotSpearAVoid is the half of the rule that predates this change and
// must survive it: a room where nobody has said anything has nothing to
// separate, and bars down an empty screen is the shape Phase 2 removed.
//
// The literal "first content row to last" band would have broken exactly here —
// an idle frame has chrome at the top and `no turn dispatched yet.` anchored at
// the bottom, so one span is fifty-five rows of bar through nothing. This asserts
// the middle stays bare while the two ends keep their rails.
func TestRailsDoNotSpearAVoid(t *testing.T) {
	g := UnicodeGlyphs()
	st := room()
	st.Width, st.Height = 120, 60
	body := frameBody(t, render(st), g.Rule)

	bare := 0
	for _, ln := range body {
		if !strings.Contains(ln, g.Sep) {
			bare++
		}
	}
	if bare == 0 {
		t.Fatalf("a tall idle room draws a rail on every one of its %d body rows — "+
			"four spears through a void:\n%s", len(body), strings.Join(body, "\n"))
	}
	if bare < len(body)/2 {
		t.Errorf("only %d of %d body rows are rail-free in a tall idle room; "+
			"most of it is empty", bare, len(body))
	}
}

// TestPageTurnRuleOutranksItsSeats: on a turn page the turn's own rule is the
// heading every seat below it belongs to, so it takes the weight and the seats
// keep theirs.
//
// It used to render wholly Muted while seatRule gave each seat name Strong — the
// parent whispering while its children shouted, which is the room's hierarchy
// upside down. Asserted here rather than in a golden because weight is an
// attribute rather than a cell: PlainStyles renders Strong and Muted alike, so
// every layout golden is blind to this by design (§9.5, §9.11).
func TestPageTurnRuleOutranksItsSeats(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	frame := Render(paged(), sty, g)

	for _, want := range []string{
		sty.Strong.Render("turn 3"),      // the page's outline
		sty.Strong.Render("Claude Code"), // a seat under it, unchanged
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("the turn page does not render %q at weight", stripANSI(want))
		}
	}

	// And the grid's copy of the same line is deliberately NOT flipped: there a
	// turn separator sits inside a column already headed by a seat name, so it is
	// the child and muted is its correct rank (see strongLabelRule).
	grid := paged()
	grid.Page = TurnView{}
	if strings.Contains(Render(grid, sty, g), sty.Strong.Render("turn 3")) {
		t.Error("the grid's turn separator took weight; in a column it is the child, not the parent")
	}
}

// stripANSI makes a failure message readable. Test-only.
func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			esc = true
		case esc && r == 'm':
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestTheRoomSpellsItsSeparatorOneWay: every │ this product draws has two cells
// of air each side — the room header, the mode line, the gutters between columns
// (§9.11 argues the number from --ascii, where the rule glyph and the spinner's
// first frame collide at one cell). The collapsed-seat notice was spelling it
// with one, which is a second grammar for the room's only separator.
func TestTheRoomSpellsItsSeparatorOneWay(t *testing.T) {
	st := room()
	st.Columns[1].Avail = AvailNotInstalled
	st.Columns[1].Note = "not found on PATH"
	g := UnicodeGlyphs()

	notice := collapsedNotice(st, g)
	if notice == "" {
		t.Fatal("no collapsed-seat notice to check")
	}
	pad := strings.Repeat(" ", gutter)
	if !strings.Contains(notice, pad+g.Sep+pad) {
		t.Errorf("the notice spells its separator %q, want %q air each side:\n%s",
			g.Sep, pad, notice)
	}
	if strings.Contains(notice, " "+g.Sep+" ") &&
		!strings.Contains(notice, pad+g.Sep+pad) {
		t.Errorf("the notice still uses one cell of air around %q", g.Sep)
	}
}
