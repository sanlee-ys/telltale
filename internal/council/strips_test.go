package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The strip is the narrowest thing this room draws — 14 cells for a seat the
// current turn was not addressed to (§9.18) — and it is where every "it fits"
// assumption in the renderer goes to die. What these tests pin is not a layout
// but a RULE: at strip width the room sheds whole words and never parts of
// them, because a clipped state word is not a word (§9.11) and a clipped word
// that is also the prefix of another word in the same vocabulary is not damage
// — it is a different claim. `fina` is not a broken `final only`; it is a thing
// this room does not say.

// stripPhases is every state a column can be in, including the availability
// case that is not a Phase at all.
var stripPhases = []Phase{
	PhaseIdle, PhaseWaiting, PhaseStreaming, PhaseDone, PhaseFailed, PhaseCancelled,
}

// stripVendors is every seat this product knows how to spell, not only the
// three the fixture room seats. A tag map with a hole in it renders an empty
// identity, which is the one failure a three-seat golden could not show.
var stripVendors = []model.VendorID{
	model.VendorClaude, model.VendorCodex, model.VendorGemini,
	model.VendorAntigravity, model.VendorCursor,
}

// stripSandboxes is every posture badge, so the row that carries the safety
// claim is walked at strip width rather than sampled.
var stripSandboxes = []SandboxLevel{
	SandboxUnknown, SandboxTools, SandboxEnforced, SandboxRequested,
	SandboxNone, SandboxWrite, SandboxGated,
}

var stripGrans = []Granularity{GranUnknown, GranTokens, GranEvents, GranFinalOnly}

// TestStripsShedWholeWordsOrNothing is the "fina" / "toke" regression, pinned
// generically rather than by the two strings that were reported.
//
// It walks every vendor against every phase, posture and granularity at strip
// width and asks one question of each rendered strip: for every word this
// column's own state spells, is that word either present WHOLE or absent
// entirely? A token that is a proper prefix of one of them, with the word
// itself nowhere on the strip, is a clip — and the vocabulary is taken from the
// state under test rather than from a fixed list, so a word added to Phase,
// Granularity or SandboxClaim.Badge is covered the day it is added.
//
// Tokens shorter than three cells are skipped. This room spells single letters
// on purpose — `f expand`, `y approve` — and every word in the vocabulary is at
// least five cells long, so a clip short enough to be mistaken for a key name
// is not a shape any of this can produce.
func TestStripsShedWholeWordsOrNothing(t *testing.T) {
	for _, v := range stripVendors {
		for _, p := range stripPhases {
			for _, sb := range stripSandboxes {
				for _, gr := range stripGrans {
					c := Column{
						Vendor: v, Label: string(v), Avail: AvailInstalled,
						Sandbox: SandboxClaim{Level: sb}, Gran: gr, Phase: p,
					}
					lines := stripCell(t, c)
					want := []string{p.String()}
					want = append(want, strings.Fields(gr.String())...)
					want = append(want, strings.Fields(c.Sandbox.Badge())...)
					assertWholeWords(t, lines, want,
						"vendor=%s phase=%s sandbox=%q gran=%q", v, p, c.Sandbox.Badge(), gr)
				}
			}
		}
	}
}

// TestStripsNeverEllipse is the same rule stated the other way, and it catches
// the case the prefix walk above cannot: a clip that KEPT its ellipsis, like
// the `Anti…` that started this. That form is honest — a clipped string in this
// room says it was clipped — and at strip width it is still the wrong trade,
// because two letters of vendor tag say the whole thing (§9.18).
func TestStripsNeverEllipse(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		for _, v := range stripVendors {
			for _, p := range stripPhases {
				for _, av := range []Availability{AvailInstalled, AvailNotInstalled, AvailUnusable} {
					c := Column{
						Vendor: v, Label: string(v), Avail: av, Phase: p,
						Sandbox: SandboxClaim{Level: SandboxRequested}, Gran: GranFinalOnly,
					}
					st := room()
					st.ASCII = ascii
					head := columnHeader(st, c, seatFocused, stripWidth, PlainStyles(), g)
					badges := badgeRow(c, stripWidth, PlainStyles(), g)
					for _, l := range []string{head, badges} {
						if strings.Contains(l, g.Ellipsis) {
							t.Errorf("ascii=%v vendor=%s phase=%s avail=%v: strip clipped rather than shed: %q",
								ascii, v, p, av, l)
						}
					}
				}
			}
		}
	}
}

// TestStripPhaseWordSurvivesEveryWidth pins the half of the shedding order that
// is not negotiable: identity yields, the clock yields, the mark yields — the
// state word never does.
//
// Swept over widths rather than asserted at fourteen, because the ladder is a
// pure function of the width and a constant that changed would otherwise move
// the behaviour with no test noticing. Below the word's own length there is
// nothing left to shed, and the honest render is a clip that says so.
func TestStripPhaseWordSurvivesEveryWidth(t *testing.T) {
	st := room()
	for _, v := range stripVendors {
		for _, p := range stripPhases {
			word := p.String()
			for w := lipgloss.Width(word); w <= stripWidth; w++ {
				c := Column{Vendor: v, Label: string(v), Avail: AvailInstalled, Phase: p}
				got := columnHeader(st, c, seatUnfocused, w, PlainStyles(), GlyphsFor(false))
				if !hasToken(got, word) {
					t.Errorf("vendor=%s phase=%s w=%d: phase word missing or clipped: %q",
						v, p, w, got)
				}
				if n := lipgloss.Width(got); n > w {
					t.Errorf("vendor=%s phase=%s w=%d: header is %d cells: %q", v, p, w, n, got)
				}
			}
		}
	}
}

// TestStripTagsMatchTheHUDSpelling asserts council's tag map against the HUD's
// by LITERAL string.
//
// Deliberately not a call into internal/hud. One product, one vocabulary — a
// reader who learned `CX` is Codex from the HUD's grid must not meet a second
// abbreviation in the room — but the seam between the two surfaces is the
// normalized session model and internal/theme's numbers and nothing else, and
// reaching across it for a rendering detail is the coupling that seam exists to
// prevent (layout.go, padRight). So the strings are written out here, and this
// test is the thing that fails when one copy moves without the other.
func TestStripTagsMatchTheHUDSpelling(t *testing.T) {
	want := map[model.VendorID]string{
		model.VendorClaude:      "CC",
		model.VendorCodex:       "CX",
		model.VendorGemini:      "GE",
		model.VendorAntigravity: "AG",
		model.VendorCursor:      "CU",
	}
	for v, tag := range want {
		if got := vendorTag(v); got != tag {
			t.Errorf("vendorTag(%s) = %q, want %q — internal/hud spells it %q",
				v, got, tag, tag)
		}
		if lipgloss.Width(tag) != 2 {
			t.Errorf("tag %q is %d cells; the strip's arithmetic budgets two", tag, lipgloss.Width(tag))
		}
	}
	// An unmapped id still gets two cells rather than none: a seat added to one
	// surface must not read as a nameless column on the other in the window
	// before both maps are updated.
	if got := vendorTag(model.VendorID("newvendor")); got != "NE" {
		t.Errorf("unmapped vendorTag = %q, want the HUD's two-letter fallback %q", got, "NE")
	}
}

// TestStripBadgeKeepsThePostureOrDropsIt pins the one badge that may not be
// traded away and the two that are. §9.2 is emphatic that a claim you cannot
// see is not a claim, so the posture word outlives the cost and the
// granularity on this row — and if it cannot fit whole it leaves whole, which
// is the difference between saying nothing and saying `unsandbox`.
func TestStripBadgeKeepsThePostureOrDropsIt(t *testing.T) {
	cost := 1.25
	for _, sb := range stripSandboxes {
		c := Column{
			Vendor: model.VendorCodex, Label: "Codex", Avail: AvailInstalled,
			Sandbox: SandboxClaim{Level: sb}, Gran: GranFinalOnly,
			CostUSD: &cost, CostSession: true,
		}
		got := badgeRow(c, stripWidth, PlainStyles(), GlyphsFor(false))
		badge := c.Sandbox.Badge()
		if badge != "" && lipgloss.Width(badge) <= stripWidth && !hasToken(got, badge) {
			t.Errorf("sandbox=%v: posture %q left the strip: %q", sb, badge, got)
		}
		if strings.Contains(got, "$") || strings.Contains(got, "session") {
			t.Errorf("sandbox=%v: the cost stayed on a strip: %q", sb, got)
		}
		for _, word := range strings.Fields(c.Gran.String()) {
			if hasToken(got, word) {
				t.Errorf("sandbox=%v: granularity %q stayed on a strip: %q", sb, word, got)
			}
		}
	}
}

// TestStripOverflowMarkerShedsWholeWords. The marker is chrome too, and it was
// fifteen cells wide in a fourteen-cell column — so `↑ 12 more above` reached
// fit() and came back as `↑ 12 more abov`, a line telling a reader something is
// hidden in a word that is not one. The COUNT is never traded: how much is
// hidden outranks which way to press, which outranks the filler between them.
func TestStripOverflowMarkerShedsWholeWords(t *testing.T) {
	g := GlyphsFor(false)
	for _, n := range []int{1, 12, 480, 12345} {
		for w := 4; w <= 24; w++ {
			got := overflowMarker(g.Up, n, "above", "", nil, w, g)
			if lipgloss.Width(got) > w && w >= 4 {
				// Only the bare count may overrun, and only below its own width.
				if lipgloss.Width(g.Up+" "+itoa(n)) <= w {
					t.Errorf("n=%d w=%d: marker is %d cells: %q", n, w, lipgloss.Width(got), got)
				}
			}
			if !strings.Contains(got, itoa(n)) {
				t.Errorf("n=%d w=%d: the count was traded away: %q", n, w, got)
			}
			assertWholeWords(t, []string{got}, []string{"above", "more"}, "n=%d w=%d", n, w)
		}
	}
}

// TestStripFrame is the golden: a real room narrowed to one seat, with the
// other three at stripColumn. One file per named scenario — this one pins what
// a strip LOOKS like, where the tests above pin what it may never do.
func TestStripFrame(t *testing.T) {
	golden(t, "strips", render(stripRoom(false)))
}

// TestStripFrameASCII is the same frame in the reduced glyph set, because every
// distinction the strip makes is a word or a mark and both have to survive it.
func TestStripFrameASCII(t *testing.T) {
	st := stripRoom(true)
	golden(t, "strips-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// stripRoom is a four-seat room mid-turn with the frame narrowed to Claude, so
// three seats sit at stripColumn in three different states.
func stripRoom(ascii bool) State {
	st := room()
	st.Width, st.Height = 120, 20
	st.Turn, st.ASCII = 4, ascii
	st.FrameOwners = []model.VendorID{model.VendorClaude}
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].TurnN, st.Columns[0].Prompt = 4, "where does the resume path break?"
	st.Columns[0].Body = "Reading resume.go now."
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].TurnN, st.Columns[1].Elapsed = 3, 8e9
	st.Columns[2].Phase = PhaseCancelled
	st.Columns[2].TurnN = 3
	st.Columns = append(st.Columns, Column{
		Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxGated, Detail: "asks before every write"},
		Gran:    GranEvents, Phase: PhaseIdle,
	})
	return st
}

// stripCell renders one column at strip width and returns its chrome rows.
//
// Chrome only, and deliberately: the body is vendor prose, which wrap() breaks
// mid-word by design (a URL has to break somewhere), so a prefix walk over it
// would be asserting a rule this room does not have.
func stripCell(t *testing.T, c Column) []string {
	t.Helper()
	st := room()
	sty, g := PlainStyles(), GlyphsFor(false)
	return []string{
		columnHeader(st, c, seatFocused, stripWidth, sty, g),
		badgeRow(c, stripWidth, sty, g),
	}
}

// assertWholeWords fails when a line carries a proper prefix of a word it was
// supposed to say — the generic form of `fina` and `toke`.
func assertWholeWords(t *testing.T, lines, vocabulary []string, format string, args ...any) {
	t.Helper()
	for _, l := range lines {
		for _, tok := range strings.Fields(l) {
			tok = strings.Trim(tok, ".,")
			if len(tok) < 3 {
				// A one- or two-cell token is a key name in this room, never a
				// clipped word: every word in the vocabulary is at least five.
				continue
			}
			for _, word := range vocabulary {
				if tok == word || !strings.HasPrefix(word, tok) {
					continue
				}
				if hasToken(l, word) {
					continue
				}
				t.Errorf(format+": %q is a clipped %q — the word had to leave whole or stay whole: %q",
					append(args, tok, word, l)...)
			}
		}
	}
}

// hasToken reports that s contains word as a whole space-delimited token.
func hasToken(s, word string) bool {
	for _, tok := range strings.Fields(s) {
		if strings.Trim(tok, ".,") == word {
			return true
		}
	}
	return false
}
