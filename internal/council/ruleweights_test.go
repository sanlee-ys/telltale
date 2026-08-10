package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// §9.26: the room draws horizontal lines at two weights, and the heavy one is
// spent on exactly three lines. These assertions are about which lines those are
// — every one of them is a fact about the rendered STRING, because the whole
// distinction is a character rather than a style and PlainStyles cannot hide it.

// TestTheHeavyRuleHasAnUnclaimedASCIIPartner is glyphs.go's collision rule
// applied to the one character §9.26 adds.
//
// A second rule weight is only a distinction if it survives --ascii, and in the
// reduced set every mark is one printable character competing with a dozen
// others that already mean something. `=` has to be unclaimed, and it has to
// stay unclaimed as the set grows — which is why this enumerates the whole set
// rather than the four glyphs that happened to be nearby when it was written.
func TestTheHeavyRuleHasAnUnclaimedASCIIPartner(t *testing.T) {
	a := ASCIIGlyphs()

	claimed := map[string]string{
		a.Sep: "sep", a.Rule: "rule", a.Ellipsis: "ellipsis", a.Caret: "caret",
		a.Warn: "warn", a.Focus: "focus", a.Prompt: "prompt", a.Up: "up",
		a.Down: "down", a.Act: "act", a.Range: "range", a.Idle: "idle",
		a.ActOK: "ok", a.ActFail: "fail", a.ActUnknown: "unknown",
		// The HUD's ascii gauge fill. Not council's glyph and deliberately
		// listed anyway: one product, one vocabulary (vendorTag's own argument),
		// so a character that means "gauge" on the other surface is not free
		// here either.
		"#": "the HUD's gauge fill",
	}
	for i, f := range a.Spinner {
		if _, ok := claimed[f]; !ok {
			claimed[f] = "spinner frame " + string(rune('0'+i))
		}
	}

	if owner, ok := claimed[a.RuleHeavy]; ok {
		t.Errorf("the ascii heavy rule %q is already the %s glyph", a.RuleHeavy, owner)
	}
	if a.RuleHeavy == "" {
		t.Error("the ascii set has no heavy rule; --ascii lost a distinction the unicode set makes")
	}
	// And the same in the reference set: two weights that render as one character
	// are one weight.
	if u := UnicodeGlyphs(); u.RuleHeavy == u.Rule || u.RuleHeavy == "" {
		t.Errorf("the unicode heavy rule %q does not differ from the light one %q",
			u.RuleHeavy, u.Rule)
	}
}

// TestOnlyTheFrameAndTheTurnPageDrawTheHeavyRule is the closed list, asserted.
//
// The value of a second weight is entirely in its scarcity: it says "this line
// is the OUTLINE" and it can only say that while the interior lines are all the
// other weight. So the useful assertion is not that the frame is heavy — it is
// that nothing else is, at every tier and on both projections.
//
// **One full-bleed rule, not two, since §9.44.** The lower one is gone: the
// composer is a bordered box now and closes the frame by shape rather than by
// ink. The scarcity argument is unchanged and one line cheaper — what the weight
// says is "the chrome stops here and the seats begin", and there is exactly one
// place in a grid where that is true.
func TestOnlyTheFrameAndTheTurnPageDrawTheHeavyRule(t *testing.T) {
	g := UnicodeGlyphs()

	heavyLines := func(frame string) (full, other int) {
		for _, ln := range strings.Split(frame, "\n") {
			switch {
			case ln == "":
			case strings.Trim(ln, g.RuleHeavy) == "":
				full++
			case strings.Contains(ln, g.RuleHeavy):
				other++
			}
		}
		return full, other
	}

	// The grid: one full-bleed rule and nothing else. Seat headers, turn
	// separators, skip lines and cards are all interior, and the composer's box
	// draws light.
	full, other := heavyLines(render(talking()))
	if full != 1 {
		t.Errorf("the grid draws %d full-bleed heavy rules, want exactly 1", full)
	}
	if other != 0 {
		t.Errorf("%d interior grid lines carry the heavy rule; the frame is no longer the only outline", other)
	}

	// The turn page: the same one, plus the turn's own rule — which is not
	// full-bleed (it sits inside the frame pad and carries a label and meta).
	full, other = heavyLines(render(paged()))
	if full != 1 {
		t.Errorf("the turn page draws %d full-bleed heavy rules, want exactly 1", full)
	}
	if other != 1 {
		t.Errorf("the turn page carries the heavy rule on %d interior lines, want exactly 1 "+
			"(its own turn rule)", other)
	}

	// The help panel is drawn inside the frame, so its title is interior.
	help := room()
	help.Help = HelpKeys
	full, other = heavyLines(render(help))
	if full != 1 || other != 0 {
		t.Errorf("the help panel frame is %d heavy rules and %d heavy interior lines, want 1 and 0",
			full, other)
	}
}

// TestTheGridsInteriorRulesStayLight names the three lines that were the obvious
// candidates for the heavy weight and did not get it.
//
// Each is a heading of something INSIDE the outline: a turn inside a column, a
// seat inside a page, a page inside the frame. Promoting any of them would
// restate §9.23's hierarchy defect one level down, so they are pinned by
// construction rather than left to a reviewer noticing.
func TestTheGridsInteriorRulesStayLight(t *testing.T) {
	g := UnicodeGlyphs()
	sty := PlainStyles()

	for _, tc := range []struct {
		name string
		line string
	}{
		{"the grid's turn separator", turnRule(2, "8s", 40, g)},
		{"a seat rule on a turn page",
			seatRule(model.VendorClaude, "Claude Code", "✓ done  8s", 60, sty, g)},
		{"the help panel's title", helpTitle("keys", resolveLayout(120, 40, 3, false), sty, g)},
		{"a column header's leader", headerRow(talking(), g)},
	} {
		if strings.Contains(tc.line, g.RuleHeavy) {
			t.Errorf("%s draws the heavy rule; only the frame and a turn page's own "+
				"rule may: %q", tc.name, tc.line)
		}
		if !strings.Contains(tc.line, g.Rule) {
			t.Errorf("%s lost its rule entirely: %q", tc.name, tc.line)
		}
	}
}

// TestTheHeaderLeaderDoesNotDependOnPhase.
//
// The leader used to be drawn only for a seat that was doing something, so a
// room with one seat answering rendered its header band as a ruled line across
// part of the frame and blank across the rest — and re-textured that band the
// moment a turn started or ended. §7.1 rule 4 keeps this room still by default;
// this is the assertion that the band no longer moves.
func TestTheHeaderLeaderDoesNotDependOnPhase(t *testing.T) {
	g := UnicodeGlyphs()
	for _, p := range []Phase{PhaseIdle, PhaseWaiting, PhaseStreaming, PhaseDone,
		PhaseFailed, PhaseCancelled} {
		st := room()
		st.Columns[0].Phase = p
		if got := headerRow(st, g); !strings.Contains(got, g.Rule) {
			t.Errorf("a %s seat's header draws no leader: %q", p, got)
		}
		if !headerUsesLeader(st.Columns[0]) {
			t.Errorf("headerUsesLeader is false for %s", p)
		}
	}

	// A seat that is not there at all keeps it too — the header still names a
	// seat and still names a state.
	st := room()
	st.Columns[0].Avail = AvailNotInstalled
	if !headerUsesLeader(st.Columns[0]) {
		t.Error("an unavailable seat's header draws no leader")
	}

	// One row, one grammar: a frame where one seat is streaming and its
	// neighbours are idle draws the leader on every seat's header, not on some.
	st = talking()
	st.Expanded = false
	// FOUND rather than indexed. talking()'s turn 3 reaches two seats, so the
	// live band sits between the frame rule and the column headers (§9.30) and a
	// literal row number would be asserting where the chrome ends rather than
	// what this row's grammar is.
	row := ""
	for _, ln := range strings.Split(render(st), "\n") {
		if strings.Contains(ln, "Claude Code") {
			row = ln
			break
		}
	}
	if !strings.Contains(row, "Claude Code") || !strings.Contains(row, g.Sep) {
		t.Fatalf("row %q is not the column-header row", row)
	}
	for i, cell := range strings.Split(row, g.Sep) {
		if !strings.Contains(cell, g.Rule) {
			t.Errorf("seat %d's header cell has no leader while its neighbours do: %q", i, cell)
		}
	}
}

// TestTheHeavyRuleSurvivesASCII. Every distinction this room makes is carried by
// a character before it is carried by a style, and --ascii is where that claim is
// actually tested: a mode that fell back to one rule glyph would silently lose
// the outline on exactly the terminals least able to infer it.
func TestTheHeavyRuleSurvivesASCII(t *testing.T) {
	a := GlyphsFor(true)
	st := talking()
	st.ASCII = true
	frame := Render(st, PlainStyles(), a)

	full := 0
	for _, ln := range strings.Split(frame, "\n") {
		if ln != "" && strings.Trim(ln, a.RuleHeavy) == "" {
			full++
		}
	}
	if full != 1 {
		t.Errorf("ascii mode draws %d full-bleed heavy rules, want 1:\n%s", full, frame)
	}
	if !strings.Contains(frame, a.Rule) {
		t.Error("ascii mode lost the light rule; both weights have to be on screen")
	}
}

// TestTheComposerIsAClosedBox is §9.44's whole claim, in both glyph modes.
//
// The claim is not "there are some box characters on screen" — it is that the
// composer has four corners and that every row between them has a side on each
// end. A box missing one side is worse than the bare rule it replaced: it reads
// as a rendering failure rather than as a frame, which is exactly what --ascii
// would have produced if the corners had been left to a Unicode-only fallback.
func TestTheComposerIsAClosedBox(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ascii bool
	}{{"unicode", false}, {"ascii", true}} {
		g := GlyphsFor(tc.ascii)
		st := talking()
		st.ASCII = tc.ascii
		lines := strings.Split(Render(st, PlainStyles(), g), "\n")

		top, bottom := -1, -1
		for i, ln := range lines {
			trimmed := strings.TrimSpace(ln)
			if strings.HasPrefix(trimmed, g.BoxTL) && strings.HasSuffix(trimmed, g.BoxTR) && top < 0 {
				top = i
			}
			if strings.HasPrefix(trimmed, g.BoxBL) && strings.HasSuffix(trimmed, g.BoxBR) {
				bottom = i
			}
		}
		if top < 0 || bottom <= top {
			t.Fatalf("%s: the composer box is not closed (top=%d bottom=%d):\n%s",
				tc.name, top, bottom, strings.Join(lines, "\n"))
		}
		for i := top + 1; i < bottom; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if !strings.HasPrefix(trimmed, g.Sep) || !strings.HasSuffix(trimmed, g.Sep) {
				t.Errorf("%s: composer row %d has no side: %q", tc.name, i-top, lines[i])
			}
		}
		// The legend is ON the bottom border, and it is not repeated under it.
		if !strings.Contains(lines[bottom], "VIEW") {
			t.Errorf("%s: the box's bottom border carries no mode word: %q", tc.name, lines[bottom])
		}
		if strings.Contains(lines[len(lines)-1], "VIEW") {
			t.Errorf("%s: the mode word is on the border AND under it: %q", tc.name, lines[len(lines)-1])
		}
	}
}
