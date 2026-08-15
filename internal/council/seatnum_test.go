package council

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// §9.29: a seat answers to its number. `1`-`4` in VIEW mode focus the Nth
// visible seat, and the number is drawn where the key acts — the seat header and
// the tab bar — so the binding and its label are the same cell.

// TestANumberFocusesTheNthVisibleSeat, at every tier.
//
// The tier matters because the three draw the seats three ways — side by side,
// one at a time under a tab bar, and one at full width — and the number is
// positional in all three. A binding that worked in the grid and not under the
// tabs would be a key that stops working when the terminal narrows, which is the
// one thing a positional shortcut must not do.
func TestANumberFocusesTheNthVisibleSeat(t *testing.T) {
	for _, tc := range []struct {
		tier string
		prep func(*State)
	}{
		{"columns", func(*State) {}},
		{"tabs", func(s *State) { s.Width = 80 }},
		{"expanded", func(s *State) { s.Expanded = true }},
	} {
		t.Run(tc.tier, func(t *testing.T) {
			m := &Model{st: room(), glyphs: UnicodeGlyphs()}
			tc.prep(&m.st)
			for n, want := range map[int]int{1: 0, 2: 1, 3: 2} {
				m.st.Focus = -1
				m.key(key(strconv.Itoa(n)))
				if m.st.Focus != want {
					t.Errorf("%q focused column %d, want %d", strconv.Itoa(n), m.st.Focus, want)
				}
			}
		})
	}
}

// TestANumberPastTheSeatCountDoesNothing. A room with two seats has keys 1 and
// 2; `3` is a no-op rather than a wrap or a clamp, because a key that quietly
// lands somewhere else is §7.8's surprise and a wrap would make the number stop
// meaning the position it is printed at.
func TestANumberPastTheSeatCountDoesNothing(t *testing.T) {
	st := room()
	st.Columns = st.Columns[:2]
	m := &Model{st: st, glyphs: UnicodeGlyphs()}
	m.st.Focus = 1

	for _, k := range []string{"3", "4", "9"} {
		m.key(key(k))
		if m.st.Focus != 1 {
			t.Errorf("%q moved focus to %d in a two-seat room", k, m.st.Focus)
		}
	}
	// And the two that exist still work, so the no-op is about the range rather
	// than about digits being inert.
	m.key(key("1"))
	if m.st.Focus != 0 {
		t.Errorf("`1` did not focus the first seat of two: %d", m.st.Focus)
	}

	// A page has one reading area and no seat to focus, so the key does nothing
	// there — the same answer `tab` gives, from the same gate.
	paged := &Model{st: railRoom(), glyphs: UnicodeGlyphs()}
	paged.st.Page = TurnView{Open: true, Turn: 1}
	paged.st.Focus = 0
	paged.key(key("3"))
	if paged.st.Focus != 0 {
		t.Error("a number moved focus while a turn page was open")
	}
}

// TestDigitsAreTextInCompose. The same contract `q`, `f`, `c` and `[` already
// keep: composeKey routes any key carrying text to the draft, so a digit typed
// into a brief is a digit.
func TestDigitsAreTextInCompose(t *testing.T) {
	m := &Model{st: room(), glyphs: UnicodeGlyphs()}
	m.st.Mode = ModeComposing
	m.st.Focus = 2

	for _, k := range []string{"1", "2", "0"} {
		m.key(key(k))
	}
	if m.st.Draft != "120" {
		t.Errorf("compose swallowed the digits as seat keys; draft is %q, want %q",
			m.st.Draft, "120")
	}
	if m.st.Focus != 2 {
		t.Errorf("typing digits in compose moved focus to %d", m.st.Focus)
	}
}

// TestTheSeatHeaderCarriesItsNumber: the binding and its label are the same
// cell, which is the whole reason this is worth two cells of header row.
func TestTheSeatHeaderCarriesItsNumber(t *testing.T) {
	g := UnicodeGlyphs()
	st := talking()
	frame := stripANSI(render(st))

	for i, want := range []string{
		g.Focus + " 1 CC Claude Code",
		"  2 CX Codex",
		"  3 AG Antigravity",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("seat %d's header does not carry its number: want %q in\n%s",
				i+1, want, frame)
		}
	}

	// The tab bar too — the other place the key acts.
	tabs := talking()
	tabs.Width = 80
	bar := stripANSI(render(tabs))
	for _, want := range []string{g.Focus + " 1 CC Claude Code", "  2 CX Codex"} {
		if !strings.Contains(bar, want) {
			t.Errorf("the tab bar does not carry seat numbers: want %q", want)
		}
	}
}

// TestTheNumbersFollowThePositionsAfterACollapse. The number is positional, so a
// seat folding out RENUMBERS the seats after it. That is a still-by-default
// wrinkle and it is bounded: a collapse already reflows the whole frame, so the
// relabelling never happens on a frame that was otherwise going to look the same.
func TestTheNumbersFollowThePositionsAfterACollapse(t *testing.T) {
	st := room()
	st.Columns[0].Avail = AvailNotInstalled // Claude folds out
	st.Columns[0].Note = "not found on PATH"

	frame := stripANSI(render(st))
	if !strings.Contains(frame, "1 CX Codex") {
		t.Errorf("after the first seat folded out, Codex is not seat 1:\n%s", frame)
	}
	if !strings.Contains(frame, "2 AG Antigravity") {
		t.Errorf("after the first seat folded out, Antigravity is not seat 2:\n%s", frame)
	}
	// A collapsed seat has no number, because no key reaches it.
	if n := st.SeatNumber(st.Columns[0]); n != 0 {
		t.Errorf("a collapsed seat has number %d; no key reaches it, so it has none", n)
	}
	// And the key follows the drawing rather than the seat table: `1` goes to
	// whoever is drawn first.
	m := &Model{st: st, glyphs: UnicodeGlyphs()}
	m.key(key("1"))
	if m.st.Focus != 1 {
		t.Errorf("`1` focused column %d, want the first VISIBLE seat (1)", m.st.Focus)
	}
}

// TestTheNumberSurvivesTheStripLadder is §9.18's shedding order with its new
// rung asserted: the number outranks the tag, and the phase word still outranks
// everything.
//
// Swept over widths rather than asserted at one, for the reason
// TestStripPhaseWordSurvivesEveryWidth gives: the ladder is a pure function of
// the width, so a constant that moved would otherwise change behaviour with no
// test noticing.
func TestTheNumberSurvivesTheStripLadder(t *testing.T) {
	g := GlyphsFor(false)
	st := room()
	for _, p := range []Phase{PhaseIdle, PhaseDone, PhaseStreaming, PhaseFailed} {
		word := p.String()
		for w := lipgloss.Width(word); w <= stripWidth; w++ {
			c := st.Columns[1] // Codex: seat 2, tag CX
			c.Phase = p
			got := columnHeader(st, c, seatUnfocused, w, PlainStyles(), g)
			if n := lipgloss.Width(got); n > w {
				t.Fatalf("phase=%s w=%d: header is %d cells: %q", p, w, n, got)
			}
			// Wherever the TAG is on screen the number is too — the number never
			// sheds while a lower-ranked cell survives.
			if strings.Contains(got, "CX") && !strings.HasPrefix(strings.TrimSpace(got), "2") {
				t.Errorf("phase=%s w=%d: the tag survived and the number did not: %q",
					p, w, got)
			}
			// And the phase word is still untouchable.
			if !hasToken(got, word) {
				t.Errorf("phase=%s w=%d: phase word missing or clipped: %q", p, w, got)
			}
		}
	}

	// At the room's own strip width the full form fits every phase word,
	// `unavailable` included — eighteen cells exactly.
	c := st.Columns[1]
	c.Avail, c.Phase = AvailNotInstalled, PhaseIdle
	full := columnHeader(st, c, seatUnfocused, stripWidth, PlainStyles(), g)
	if !strings.Contains(full, "2 CX "+g.Warn+" unavailable") {
		t.Errorf("the widest strip state does not fit the full form: %q", full)
	}
}

// TestTheModeLineNamesTheSeatKeys, and never at the cost of the way out.
func TestTheModeLineNamesTheSeatKeys(t *testing.T) {
	g := UnicodeGlyphs()
	st := room()
	if !strings.Contains(stripANSI(render(st)), "1-3 seat") {
		t.Errorf("the view mode line does not name the seat keys:\n%s", stripANSI(render(st)))
	}

	// The range is however many seats are on screen. A footer naming a `4` in a
	// three-seat room would promise a key that does nothing — §7.8's surprise,
	// which this line already refuses for `tab` and `f`.
	two := room()
	two.Columns = two.Columns[:2]
	if got := stripANSI(render(two)); !strings.Contains(got, "1-2 seat") ||
		strings.Contains(got, "1-3 seat") {
		t.Errorf("a two-seat room does not name 1-2:\n%s", got)
	}

	// One seat on screen: no `tab`, no `f`, and no numbers either — §9.11's rule
	// applied to a third key, since all three address a choice that does not
	// exist there. The header drops its number in the same breath, so the key and
	// its label appear and vanish together.
	one := room()
	one.Columns = one.Columns[:1]
	got := stripANSI(render(one))
	if strings.Contains(got, "1-1 seat") {
		t.Errorf("a one-seat room names a seat key:\n%s", got)
	}
	if strings.Contains(got, "1 CC Claude Code") {
		t.Errorf("a one-seat room numbers its only column:\n%s", got)
	}
	if one.SeatNumber(one.Columns[0]) != 0 {
		t.Error("the only seat on screen still has a number")
	}

	// Shed order is list order and this is the third rung: `[ ]` goes first, `f`
	// second, the numbers last of the three — and `? help` / `q quit` never yield.
	hs := modeHints(room(), g)
	var shed []string
	for _, h := range hs {
		if h.shed {
			shed = append(shed, h.key)
		}
		if (h.key == "?" || h.key == "q") && h.shed {
			t.Errorf("%q is marked sheddable; it is the way out of the room", h.key)
		}
	}
	want := []string{"[ ]", "f", "1-3"}
	if len(shed) != len(want) {
		t.Fatalf("shed ladder is %v, want %v", shed, want)
	}
	for i := range want {
		if shed[i] != want[i] {
			t.Errorf("shed ladder is %v, want %v (order is rank)", shed, want)
		}
	}
}

// TestTheSeatKeysSurviveASCII. Digits are digits in both glyph sets, which is
// the point of choosing them — but the header and the tab bar have to still draw
// them, and the mode line has to still name them.
func TestTheSeatKeysSurviveASCII(t *testing.T) {
	a := GlyphsFor(true)
	st := talking()
	st.ASCII = true
	frame := stripANSI(Render(st, PlainStyles(), a))

	for _, want := range []string{a.Focus + " 1 CC Claude Code", "  2 CX Codex", "1-3 seat"} {
		if !strings.Contains(frame, want) {
			t.Errorf("ascii mode lost %q:\n%s", want, frame)
		}
	}
}

// TestTheHelpPanelStillFitsItsBudget. The seat keys were merged onto the row
// that already names `tab` rather than given one of their own — the panel's
// budget is 17 rows and the `?` line is the only documented way out of it.
//
// The range is read from SeatNames() rather than spelled `1-4`, because it was
// spelled `1-4` and went on saying so through the whole life of the fifth seat
// (§9.39). A test that pins the stale number is how the stale number survives.
func TestTheHelpPanelStillFitsItsBudget(t *testing.T) {
	lay := resolveLayout(120, 40, 3, false)
	sty, g := PlainStyles(), UnicodeGlyphs()

	page := helpKeys(lay, sty, g)
	if helpExit(page) < 0 {
		t.Fatal("the help page lost its way out")
	}
	seatKeys := "1-" + strconv.Itoa(len(SeatNames()))
	row := ""
	for _, l := range page {
		if strings.HasPrefix(strings.TrimSpace(l), "tab") {
			row = l
		}
		if strings.HasPrefix(strings.TrimSpace(l), seatKeys+" ") {
			t.Errorf("the seat keys took a row of their own: %q", l)
		}
	}
	if !strings.Contains(row, seatKeys) {
		t.Errorf("the focus row does not name the seat keys (%s): %q", seatKeys, row)
	}
	// The key column has to line up with every other row's.
	if i := strings.Index(row, "focus"); i != helpIndent {
		t.Errorf("the focus row's prose starts at column %d, want helpIndent (%d): %q",
			i, helpIndent, row)
	}
}

// key(digit) has to be the keypress a terminal actually delivers, or this whole
// file tests a keyboard nobody has: a digit carries Text, which is exactly what
// composeKey reads to decide it is text.
func TestADigitKeypressCarriesItsText(t *testing.T) {
	k := key("1")
	if k.Text != "1" || k.Code != tea.KeyPressMsg(k).Code {
		t.Fatalf("a digit keypress is %+v", k)
	}
	if k.Text == "" {
		t.Error("a digit with no Text would be swallowed by compose as a command")
	}
}
