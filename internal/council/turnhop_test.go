package council

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// walking is a column four turns deep, with the last turn taller than the
// viewport.
//
// The last turn's height is the load-bearing part of the fixture rather than
// decoration: a turn shorter than the window has its head clamped away by the
// tail, so a transcript ending in a short turn can never park the reader ABOVE
// the final separator — and that is exactly the position the "] past the last
// turn" rule is about.
func walking() State {
	st := room()
	st.Turn = 4

	c := &st.Columns[0]
	for i := 1; i <= 3; i++ {
		c.startTurn(i, "brief "+strconv.Itoa(i), false)
		c.Body = longBody(8)
		c.Phase, c.Elapsed = PhaseDone, 5*time.Second
	}
	c.startTurn(4, "brief 4", false)
	c.Body = longBody(30)
	c.Phase = PhaseStreaming
	return st
}

// anchorsOf is the fixture's own view of where the turns start, taken from the
// same function the renderer and the hop keys take it from. A test that
// hard-coded line numbers would be asserting one arithmetic against another
// copy of itself.
func anchorsOf(t *testing.T, st State, idx int) []turnAnchor {
	t.Helper()
	_, anchors, _, ok := columnViewport(st, idx)
	if !ok {
		t.Fatalf("column %d is not on screen", idx)
	}
	return anchors
}

// TestBracketsWalkTheTranscriptATurnAtATime is the complaint this feature
// answers: the columns scroll a line at a time and the conversation happens a
// turn at a time, so at "↑ 509 more above" the count is honest and unusable.
func TestBracketsWalkTheTranscriptATurnAtATime(t *testing.T) {
	m := &Model{st: walking(), glyphs: GlyphsFor(false)}
	a := anchorsOf(t, m.st, 0)
	if len(a) != 4 {
		t.Fatalf("fixture has %d turns, want 4", len(a))
	}
	max := MaxScroll(m.st, 0)
	if a[3].Off >= max {
		t.Fatalf("fixture's last turn is shorter than the viewport: head %d, max %d", a[3].Off, max)
	}

	m.st.Columns[0].Follow = false
	m.st.Columns[0].Scroll = 0

	// Forward, one turn per press, each landing the separator on the top row.
	for _, want := range []int{a[1].Off, a[2].Off, a[3].Off} {
		m.viewKey(key("]"))
		if got := m.st.Columns[0].Scroll; got != want {
			t.Fatalf("] landed at %d, want the next turn's head at %d", got, want)
		}
		if m.st.Columns[0].Follow {
			t.Error("a hop left the column following the tail while showing an older turn")
		}
	}

	// And back, the same way.
	for _, want := range []int{a[2].Off, a[1].Off, a[0].Off} {
		m.viewKey(key("["))
		if got := m.st.Columns[0].Scroll; got != want {
			t.Fatalf("[ landed at %d, want the previous turn's head at %d", got, want)
		}
	}
	if a[0].Off != 0 {
		t.Errorf("the first turn starts at line %d, want the top of the transcript", a[0].Off)
	}
}

// TestAHopBackFromMidTurnLandsOnItsOwnHeadFirst is the audio player's rule, and
// it is the one people already have in their hands: previous-track from the
// middle of a track restarts the track.
func TestAHopBackFromMidTurnLandsOnItsOwnHeadFirst(t *testing.T) {
	m := &Model{st: walking(), glyphs: GlyphsFor(false)}
	a := anchorsOf(t, m.st, 0)

	m.st.Columns[0].Follow = false
	m.st.Columns[0].Scroll = a[2].Off + 2

	m.viewKey(key("["))
	if got := m.st.Columns[0].Scroll; got != a[2].Off {
		t.Errorf("[ from mid-turn landed at %d, want this turn's own head at %d", got, a[2].Off)
	}
	m.viewKey(key("["))
	if got := m.st.Columns[0].Scroll; got != a[1].Off {
		t.Errorf("the second [ landed at %d, want the turn before at %d", got, a[1].Off)
	}
}

// TestTheEndsOfTheTranscriptDoNotWrap. A transcript has a first turn, and a key
// pressed one time too many must not answer by jumping a whole conversation.
func TestTheEndsOfTheTranscriptDoNotWrap(t *testing.T) {
	m := &Model{st: walking(), glyphs: GlyphsFor(false)}
	a := anchorsOf(t, m.st, 0)

	m.st.Columns[0].Follow = false
	m.st.Columns[0].Scroll = 0
	m.viewKey(key("["))
	if got := m.st.Columns[0].Scroll; got != 0 {
		t.Errorf("[ at the first turn moved to %d, want it to do nothing", got)
	}
	if m.st.Columns[0].Follow {
		t.Error("[ at the top wrapped to the tail")
	}

	// Forward past the last turn is the ONE asymmetry, and it is G's answer
	// rather than a second one: what comes after the last turn is the live
	// output, which is a place the transcript really does go.
	m.st.Columns[0].Scroll = a[3].Off
	m.viewKey(key("]"))
	if !m.st.Columns[0].Follow {
		t.Error("] past the last turn did not restore the tail")
	}
	if got, max := m.st.Columns[0].Scroll, MaxScroll(m.st, 0); got != max {
		t.Errorf("] past the last turn parked at %d, want the tail at %d", got, max)
	}
}

// TestASingleTurnColumnHasNowhereToHop. One turn that fits its column is a
// column where both keys are honest no-ops — nothing is hidden, so nothing is
// reached by uncovering it.
func TestASingleTurnColumnHasNowhereToHop(t *testing.T) {
	st := room()
	st.Turn = 1
	c := &st.Columns[0]
	c.startTurn(1, "one brief", false)
	c.Body, c.Phase = "a short answer.", PhaseDone

	if max := MaxScroll(st, 0); max != 0 {
		t.Fatalf("fixture scrolls (max %d); this case is about a turn that fits", max)
	}
	m := &Model{st: st, glyphs: GlyphsFor(false)}
	for _, k := range []string{"[", "]"} {
		m.viewKey(key(k))
		if got := m.st.Columns[0].Scroll; got != 0 {
			t.Errorf("%s moved a column with nothing to hop to, to %d", k, got)
		}
	}
}

// TestBracketsAreTextWhileComposing is §9.10's rule, not a new one: a key that
// carries text IS text in the composer, which is what keeps `q` the letter q
// there. Brackets carry text.
func TestBracketsAreTextWhileComposing(t *testing.T) {
	m := &Model{st: walking(), glyphs: GlyphsFor(false)}
	m.st.Mode = ModeComposing
	m.st.Columns[0].Follow = false
	m.st.Columns[0].Scroll = 4

	m.composeKey(key("["))
	m.composeKey(key("]"))
	if m.st.Draft != "[]" {
		t.Errorf("Draft = %q, want the brackets typed as text", m.st.Draft)
	}
	if got := m.st.Columns[0].Scroll; got != 4 {
		t.Errorf("a bracket scrolled the column to %d while composing", got)
	}

	// And the mode line must not offer them there, which would be the room
	// promising a key that does something else entirely.
	if strings.Contains(lastLine(render(m.st)), "[ ] turn") {
		t.Error("the compose mode line advertises [ ], which is text in that mode")
	}
	if !strings.Contains(lastLine(render(room())), "[ ] turn") {
		t.Error("the view mode line does not name the turn keys")
	}
}

// TestTheOverflowMarkerNamesWhichTurnIsHidden. The count says how much is
// hidden; the coordinate says what it IS, in the transcript's own word.
//
// The semantics are "the turn the line immediately outside the fold belongs
// to", chosen because it is the reading that cannot lie about a turn only half
// on screen: a long reply running off the top is still the turn you are in, and
// naming the topmost hidden separator several screens above would answer a
// question nobody asked.
func TestTheOverflowMarkerNamesWhichTurnIsHidden(t *testing.T) {
	g := UnicodeGlyphs()
	st := walking()
	st.Expanded = true // one column at full width, where the marker has cells to spend
	a := anchorsOf(t, st, 0)
	st.Columns[0].Follow = false
	st.Columns[0].Scroll = a[2].Off + 3 // three lines into turn 3

	got := render(st)
	if !strings.Contains(got, "more above  "+g.Sep+"  turn 3") {
		t.Errorf("the marker does not name the turn above the fold\n%s", got)
	}
	// The two markers name two different turns, so the second is a fact this
	// cell is the only place to learn rather than a repeat of the first.
	if !strings.Contains(got, "more below  "+g.Sep+"  turn 4") {
		t.Errorf("the marker below does not name the turn it is holding back\n%s", got)
	}
	golden(t, "turn-hop-marker", got)

	// A column the keys do not move keeps its address instead: §9.12 rules that
	// a marker states the key for THIS column, and a coordinate in front of the
	// only thing a reader can act on there would crowd the answer to make room
	// for the question.
	side := walking()
	side.Columns[1].Phase = PhaseDone
	side.Columns[1].Body = longBody(60)
	side.Columns[1].Follow, side.Columns[1].Scroll = false, 20
	if line := columnMarkerLine(render(side), "tab to focus"); strings.Contains(line, "turn ") {
		t.Errorf("an unfocused column spends its marker on a coordinate: %q", line)
	}
}

// columnMarkerLine finds the rendered row carrying a given hint.
func columnMarkerLine(frame, hint string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, hint) && strings.Contains(l, "more ") {
			return l
		}
	}
	return ""
}

// TestTheMarkerShedsTheTurnBeforeItShedsAKey pins the order §9.12 fixed and
// §9.20 had to fit inside: the count is never traded, the keys outrank the
// coordinate, and the coordinate is what a narrow column loses first.
func TestTheMarkerShedsTheTurnBeforeItShedsAKey(t *testing.T) {
	g := UnicodeGlyphs()
	hints := []string{g.Up + g.Down + " scroll  " + g.Sep + "  f expand", g.Up + g.Down + " scroll"}

	var sawKeysWithoutTurn bool
	for w := 16; w <= 90; w++ {
		s := overflowMarker(g.Up, 509, "above", "turn 4", hints, w, g)
		if !strings.Contains(s, "509 more above") {
			t.Fatalf("w=%d: the count was traded away: %q", w, s)
		}
		if got := len([]rune(s)); got > w && !strings.HasPrefix(s, g.Up+" 509 more above") {
			t.Fatalf("w=%d: marker overruns its cell: %q", w, s)
		}
		hasTurn := strings.Contains(s, "turn 4")
		hasKeys := strings.Contains(s, "scroll")
		if hasTurn && !strings.Contains(s, "f expand") {
			t.Fatalf("w=%d: the coordinate survived a width that cost the room a key: %q", w, s)
		}
		if hasKeys && !hasTurn {
			sawKeysWithoutTurn = true
		}
	}
	if !sawKeysWithoutTurn {
		t.Error("no width sheds the coordinate and keeps the keys — the order is untested")
	}

	// With nothing to compete against, the coordinate takes the cells. This is
	// the second marker on a column hidden both ways, where the hint has already
	// been said once above.
	if s := overflowMarker(g.Down, 12, "below", "turn 5", nil, 40, g); !strings.Contains(s, "turn 5") {
		t.Errorf("a hintless marker dropped the coordinate anyway: %q", s)
	}
}

// TestTheTurnKeysSurviveASCII. Every distinction this room makes is carried by
// a word first, so --ascii and NO_COLOR must read identically (§9.11).
func TestTheTurnKeysSurviveASCII(t *testing.T) {
	st := walking()
	st.ASCII = true
	st.Expanded = true
	a := anchorsOf(t, st, 0)
	st.Columns[0].Follow = false
	st.Columns[0].Scroll = a[2].Off + 3

	got := Render(st, PlainStyles(), GlyphsFor(true))
	for _, want := range []string{"turn 3", "turn 4", "[ ] turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("--ascii dropped %q\n%s", want, got)
		}
	}
}

// TestTheHelpPanelNamesTheTurnKeysAboveTheFold. helpBody clips at the body
// height and does not scroll, so a row past the fold is not a demoted row, it
// is an absent one — and the panel's budget is hard at 17.
func TestTheHelpPanelNamesTheTurnKeysAboveTheFold(t *testing.T) {
	lines := helpKeys(layoutFor(room(), GlyphsFor(false)), PlainStyles(), GlyphsFor(false))
	fold := -1
	for i, l := range lines {
		if strings.Contains(l, "? ") && strings.Contains(l, "next page") {
			fold = i
			break
		}
	}
	if fold < 0 {
		t.Fatal("the `?` line is gone — the panel has no documented way out")
	}
	if !strings.Contains(strings.Join(lines[:fold+1], "\n"), "[ ]") {
		t.Error("the turn keys are not named above the fold — they cannot be discovered in the UI")
	}
}

// TestTheModeLineShedsTheTurnKeysRatherThanItsWayOut. The footer fit its keys
// exactly at the tabbed tier, and truncation cuts the RIGHT-hand end — where
// `? help` and `q quit` live. A motion key bought with the way out of the room
// is the trade §9.11's footer pass exists to refuse.
func TestTheModeLineShedsTheTurnKeysRatherThanItsWayOut(t *testing.T) {
	st := room()
	st.Width = 80 // the tabbed tier, where the line already fit exactly
	line := lastLine(render(st))
	for _, want := range []string{"? help", "q quit"} {
		if !strings.Contains(line, want) {
			t.Errorf("the narrow mode line lost %q: %q", want, line)
		}
	}
	if strings.Contains(line, "[ ] turn") {
		t.Errorf("the narrow mode line kept the sheddable cell instead: %q", line)
	}
	if strings.Contains(line, UnicodeGlyphs().Ellipsis) {
		t.Errorf("the mode line was truncated where a whole cell could have gone: %q", line)
	}
}
