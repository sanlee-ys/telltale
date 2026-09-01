package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// paneRoom is room() with the keys on the first seat, which is the state every
// pane key is pressed from below.
func paneRoom() State {
	st := room()
	st.Focus = 0
	return st
}

// widths is what the renderer would give each drawn pane, which is the only
// measurement any of these tests may make: it goes through layoutFor, the same
// pure function Render calls, so a test cannot pass against arithmetic the frame
// does not use.
func widths(st State) []int {
	lay := layoutFor(st, GlyphsFor(false))
	out := make([]int, lay.Cols)
	for i := range out {
		out[i] = lay.widthAt(i)
	}
	return out
}

// TestThePanePrefixArmsAndCancels. `^w` is a mode that lasts exactly one
// keystroke, and the room may never be left waiting on a second key the operator
// has stopped expecting to give — so every branch clears it, the default
// included.
func TestThePanePrefixArmsAndCancels(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	m.key(key("ctrl+w"))
	if !m.st.PanePrefix {
		t.Fatal("ctrl+w did not arm the pane prefix")
	}
	for _, k := range []string{"s", "e", ">", "<", ".", ",", "z", "enter"} {
		m.st.PanePrefix = true
		m.key(key(k))
		if m.st.PanePrefix {
			t.Errorf("%q left the pane prefix armed", k)
		}
	}
}

// TestAnUnknownPaneKeyIsSwallowed. Falling through would read as tolerant and
// would be dangerous: `^w q` would quit the room and `^w ctrl+c` would cancel a
// turn, two irreversible acts reached by a chord the operator did not finish.
func TestAnUnknownPaneKeyIsSwallowed(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	m.st.Focus = 1
	m.key(key("ctrl+w"))
	if _, cmd := m.key(key("q")); cmd != nil {
		t.Error("^w q returned a command — q fell through to the quit key")
	}
	if m.st.Focus != 1 {
		t.Errorf("^w q moved focus to %d", m.st.Focus)
	}
	m.key(key("ctrl+w"))
	m.key(key("i"))
	if m.st.Mode == ModeComposing {
		t.Error("^w i fell through to the composer")
	}
}

// TestThePanePrefixNeverReachesTheDraft. In compose every printable character is
// draft text, the contract `q`, `f`, `c` and `t` already keep. A prefix armed
// there would change what the next letter does while a brief is being typed, and
// the operator would learn it by losing a character.
func TestThePanePrefixNeverReachesTheDraft(t *testing.T) {
	st := paneRoom()
	st.Mode = ModeComposing
	st.Draft = "ship it"
	m := &Model{st: st, glyphs: GlyphsFor(false)}

	m.key(key("ctrl+w"))
	if m.st.PanePrefix {
		t.Error("ctrl+w armed the pane prefix in compose mode")
	}
	m.key(key("s"))
	if m.st.Draft != "ship its" {
		t.Errorf("draft is %q — s did not stay the letter s in compose", m.st.Draft)
	}
	if m.st.PaneOwner != "" {
		t.Errorf("a keystroke in compose split the room to %q", m.st.PaneOwner)
	}
}

// TestSplitGivesTheFocusedPaneTheReadingWidth. The other panes hold at
// stripColumn, which is the width §9.18 measured a backgrounded seat needs — so
// `^w s` reuses the degradation the room already had rather than inventing a
// second narrow rendering.
func TestSplitGivesTheFocusedPaneTheReadingWidth(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	even := widths(m.st)

	m.key(key("ctrl+w"))
	m.key(key("s"))
	if m.st.PaneOwner != model.VendorClaude {
		t.Fatalf("PaneOwner is %q, want the focused seat", m.st.PaneOwner)
	}
	split := widths(m.st)
	if split[0] <= even[0] {
		t.Errorf("the split pane is %d cells, no wider than the even %d", split[0], even[0])
	}
	for i := 1; i < len(split); i++ {
		if split[i] != stripColumn {
			t.Errorf("pane %d is %d cells, want stripColumn %d", i, split[i], stripColumn)
		}
	}

	// It SETS rather than toggles, and pressing it on a second pane re-points the
	// split rather than adding an owner: the question "which seat am I reading"
	// has one answer.
	m.st.Focus = 2
	m.key(key("ctrl+w"))
	m.key(key("s"))
	if m.st.PaneOwner != model.VendorAntigravity {
		t.Errorf("PaneOwner is %q after a second split, want the newly focused seat", m.st.PaneOwner)
	}
	again := widths(m.st)
	if again[0] != stripColumn || again[2] <= again[0] {
		t.Errorf("re-pointing the split gave %v, want the third pane wide and the first a strip", again)
	}
}

// TestSplitDoesNotFollowFocus. A split that followed focus would reflow the
// whole grid on every `tab` press, which today moves a marker — the moving cell
// §7.1 rule 4 does not budget for.
func TestSplitDoesNotFollowFocus(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	m.key(key("ctrl+w"))
	m.key(key("s"))
	before := widths(m.st)

	m.key(key("tab"))
	if m.st.Focus == 0 {
		t.Fatal("tab did not move focus")
	}
	after := widths(m.st)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("tab reflowed the grid: %v became %v", before, after)
		}
	}
}

// TestTheOperatorOutranksTheRoute. FrameOwners is an inference from where a turn
// went; PaneOwner is a request the operator typed. When the two disagree the
// request wins outright, and a dispatch replacing FrameOwners leaves it alone —
// which is the difference between a layout control and a side effect of routing.
func TestTheOperatorOutranksTheRoute(t *testing.T) {
	st := paneRoom()
	st.FrameOwners = []model.VendorID{model.VendorCodex}
	routed := widths(st)
	if routed[1] <= routed[0] {
		t.Fatalf("fixture is wrong: the route did not widen the codex pane (%v)", routed)
	}

	st.PaneOwner = model.VendorClaude
	got := widths(st)
	if got[0] <= got[1] {
		t.Errorf("widths %v — the route outranked the operator's split", got)
	}
	if got[1] != stripColumn {
		t.Errorf("pane 1 is %d cells, want stripColumn %d: the two owner sets merged", got[1], stripColumn)
	}
}

// TestResizeMovesOneBoundary. One press gives the focused pane a step and takes
// the same step off ONE neighbour, so PaneGrow sums to zero by construction and
// the row still fills the terminal exactly.
func TestResizeMovesOneBoundary(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	before := widths(m.st)

	m.key(key("ctrl+w"))
	m.key(key(">"))
	after := widths(m.st)

	if after[0]-before[0] != paneStep {
		t.Errorf("the focused pane moved %d cells, want %d", after[0]-before[0], paneStep)
	}
	if before[1]-after[1] != paneStep {
		t.Errorf("the neighbour paid %d cells, want %d", before[1]-after[1], paneStep)
	}
	if after[2] != before[2] {
		t.Errorf("pane 2 moved from %d to %d — more than one boundary shifted", before[2], after[2])
	}
	sum := 0
	for _, v := range m.st.PaneGrow {
		sum += v
	}
	if sum != 0 {
		t.Errorf("PaneGrow is %v, sums to %d rather than zero", m.st.PaneGrow, sum)
	}

	// `<` is the same boundary, back.
	m.key(key("ctrl+w"))
	m.key(key("<"))
	if got := widths(m.st); got[0] != before[0] || got[1] != before[1] {
		t.Errorf("shrink gave %v, want the even %v back", got, before)
	}
}

// TestTheRightmostPaneTradesLeftward. It has no right neighbour, so it takes its
// step from the pane on its left — which is what a reader expects from the only
// pane whose right edge is the frame.
func TestTheRightmostPaneTradesLeftward(t *testing.T) {
	st := paneRoom()
	st.Focus = len(st.Columns) - 1
	m := &Model{st: st, glyphs: GlyphsFor(false)}
	before := widths(m.st)

	m.key(key("ctrl+w"))
	m.key(key(">"))
	after := widths(m.st)

	last := len(after) - 1
	if after[last]-before[last] != paneStep {
		t.Errorf("the rightmost pane moved %d cells, want %d", after[last]-before[last], paneStep)
	}
	if before[last-1]-after[last-1] != paneStep {
		t.Errorf("the pane to its left paid %d cells, want %d", before[last-1]-after[last-1], paneStep)
	}
}

// TestResizeStopsAtTheFloor. The key refuses a move a floor would swallow rather
// than writing it and letting repairPaneFloors put it back — the second would
// leave State claiming a size the frame does not have, and the composer border
// would then say `sized` about a room that is not.
func TestResizeStopsAtTheFloor(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	for i := 0; i < 40; i++ {
		m.key(key("ctrl+w"))
		m.key(key(">"))
		for j, w := range widths(m.st) {
			if w < stripColumn {
				t.Fatalf("press %d put pane %d at %d cells, below the %d floor", i, j, w, stripColumn)
			}
		}
	}
	// It stopped, rather than running away: the last press changed nothing.
	before := widths(m.st)
	m.key(key("ctrl+w"))
	m.key(key(">"))
	after := widths(m.st)
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("the boundary was still moving at the floor: %v became %v", before, after)
		}
	}
}

// TestEvenPutsEverythingBack. `^w e` clears BOTH facts, and that is what makes
// it the single way back: an operator who split the room and then grew the owner
// has two pieces of state they never named separately.
func TestEvenPutsEverythingBack(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	want := widths(m.st)

	m.key(key("ctrl+w"))
	m.key(key("s"))
	m.key(key("ctrl+w"))
	m.key(key(">"))
	if !m.st.PanesArranged() {
		t.Fatal("the room reports no arrangement after a split and a resize")
	}

	m.key(key("ctrl+w"))
	m.key(key("e"))
	if m.st.PanesArranged() {
		t.Errorf("^w e left PaneOwner=%q PaneGrow=%v", m.st.PaneOwner, m.st.PaneGrow)
	}
	if got := widths(m.st); !equalInts(got, want) {
		t.Errorf("^w e gave %v, want the even %v", got, want)
	}
}

// TestThePrefixRefusesWhereThereIsNoBoundary. An armed prefix draws a footer
// naming four keys, so arming it where all four do nothing would be §7.8's
// surprise delivered by the one line that exists to prevent it.
func TestThePrefixRefusesWhereThereIsNoBoundary(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(*State)
	}{
		{"turn page", func(s *State) { s.Page.Open = true }},
		{"arena record", func(s *State) { s.Record = &ArenaRecord{} }},
		{"expanded", func(s *State) { s.Expanded = true }},
		{"tabs tier", func(s *State) { s.Width = 80 }},
		{"one seat", func(s *State) { s.Columns = s.Columns[:1] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := paneRoom()
			tc.prep(&st)
			m := &Model{st: st, glyphs: GlyphsFor(false)}
			m.key(key("ctrl+w"))
			if m.st.PanePrefix {
				t.Error("the pane prefix armed where no boundary exists")
			}
		})
	}
}

// TestAnArrangementSurvivesANarrowTerminal. The stored split and bias paint
// nothing below the columns tier, and they come back when the terminal is
// widened — exactly as Expanded does. A control that forgot on a resize would
// punish the operator for dragging a window.
func TestAnArrangementSurvivesANarrowTerminal(t *testing.T) {
	m := &Model{st: paneRoom(), glyphs: GlyphsFor(false)}
	m.key(key("ctrl+w"))
	m.key(key("s"))
	wide := widths(m.st)

	m.st.Width = 80
	if lay := layoutFor(m.st, m.glyphs); lay.Tier != TierTabs {
		t.Fatalf("fixture is wrong: 80 cells resolved to %v", lay.Tier)
	}
	if m.st.PaneOwner == "" {
		t.Fatal("the narrow tier cleared the split")
	}

	m.st.Width = 120
	if got := widths(m.st); !equalInts(got, wide) {
		t.Errorf("widening gave %v, want the split %v back", got, wide)
	}
}

// TestTheArrangementIsSaidInWords is §9.51's second-signal test, and it is the
// one that would catch a pane feature legible only in colour.
//
// It renders with PlainStyles — the identity set, where every Render is a no-op
// — and with the ASCII glyphs, so nothing a hue, a weight or a Unicode glyph
// could carry is available to it. What must survive is the sentence.
func TestTheArrangementIsSaidInWords(t *testing.T) {
	for _, tc := range []struct {
		name string
		prep func(*State)
		want string
	}{
		{"split", func(s *State) { s.PaneOwner = model.VendorClaude }, "^w e panes split"},
		{"sized", func(s *State) {
			s.PaneGrow = map[model.VendorID]int{model.VendorClaude: paneStep, model.VendorCodex: -paneStep}
		}, "^w e panes sized"},
		{"both", func(s *State) {
			s.PaneOwner = model.VendorClaude
			s.PaneGrow = map[model.VendorID]int{model.VendorClaude: paneStep, model.VendorCodex: -paneStep}
		}, "^w e panes split, sized"},
		{"prefix armed", func(s *State) { s.PanePrefix = true }, "PANES"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, ascii := range []bool{false, true} {
				st := paneRoom()
				tc.prep(&st)
				got := Render(st, PlainStyles(), GlyphsFor(ascii))
				if !strings.Contains(got, tc.want) {
					t.Errorf("ascii=%v: the frame never says %q\n%s", ascii, tc.want, got)
				}
			}
		})
	}
}

// TestAnUntouchedRoomSaysNothingAboutPanes. The legend appears only once the
// operator has arranged something, which is what keeps every golden taken before
// §9.51 a byte-for-byte correct claim about the frame.
func TestAnUntouchedRoomSaysNothingAboutPanes(t *testing.T) {
	if got := render(paneRoom()); strings.Contains(got, "panes") || strings.Contains(got, "PANES") {
		t.Errorf("an untouched room mentions panes:\n%s", got)
	}
	// And it says nothing at a tier where the arrangement paints nothing, even
	// with the state set: a legend describing a frame the reader is not looking
	// at is the room describing someone else's room.
	st := paneRoom()
	st.Width, st.PaneOwner = 80, model.VendorClaude
	if got := render(st); strings.Contains(got, "panes split") {
		t.Errorf("the tabs tier claims a split:\n%s", got)
	}
}

// TestThePanelTeachesThePrefix. The four pane keys are documented by the mode
// line, on every frame of the one moment they are live (flowStopHint's
// precedent) — but a reader has to know the prefix before they can get there, so
// the panel names it.
func TestThePanelTeachesThePrefix(t *testing.T) {
	st := paneRoom()
	st.Help = HelpKeys
	if got := render(st); !strings.Contains(got, "^w sizes the panes") {
		t.Errorf("the help panel does not name the pane prefix:\n%s", got)
	}
}

// TestTheFooterNamesThePaneKeysWhileArmed. §7.8 forbids a mode that changes what
// an unmodified key means without saying so, and for one keystroke `s` is not
// the letter s.
func TestTheFooterNamesThePaneKeysWhileArmed(t *testing.T) {
	st := paneRoom()
	st.PanePrefix = true
	got := render(st)
	for _, want := range []string{"s split", "< > resize", "e even", "any cancel"} {
		if !strings.Contains(got, want) {
			t.Errorf("the footer does not name %q while the prefix is armed:\n%s", want, got)
		}
	}
}

// The goldens. One file per named scenario, and every one of them is a NEW name
// rather than a case bolted onto an existing file: the name is what says which
// property the bytes pin down.
func TestPaneGoldens(t *testing.T) {
	split := paneRoom()
	split.PaneOwner = model.VendorClaude
	golden(t, "panes-split", render(split))
	golden(t, "panes-split-ascii", Render(split, PlainStyles(), GlyphsFor(true)))

	sized := paneRoom()
	sized.PaneGrow = map[model.VendorID]int{
		model.VendorClaude: 2 * paneStep,
		model.VendorCodex:  -2 * paneStep,
	}
	golden(t, "panes-sized", render(sized))

	armed := paneRoom()
	armed.PanePrefix = true
	golden(t, "panes-keys", render(armed))
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
