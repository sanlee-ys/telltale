package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// §9.28: the ratified exception. Council spends one hue per seat, and only where
// seat names are what the eye is sorting.
//
// Every assertion here is over a RENDERED string with NewStyles, because the
// whole feature is invisible to PlainStyles by construction — which is also the
// property TestSeatHuesAreInvisibleToPlainStyles pins directly, and the reason
// this pass regolds nothing.

// seatVendors is every VendorID that can reach a seat hue, seatable or not — the
// five the room seats plus Gemini, which is in the normalized model and takes
// the documented fallback. Kept beside the tests so the fallback has a witness
// as well as a doc comment.
var seatVendors = []model.VendorID{
	model.VendorClaude, model.VendorCodex, model.VendorGemini,
	model.VendorAntigravity, model.VendorCursor, model.VendorGrok,
}

// TestEverySeatWearsItsOwnHue. A vendor the room can seat holds a hue nobody
// else holds; one it cannot falls back to the identity hue every seat name used
// to have, which is a seat that looks as it always did rather than broken.
func TestEverySeatWearsItsOwnHue(t *testing.T) {
	if h := seatHue(model.VendorGemini); h != "6" {
		// Asserted by name so that giving Gemini a hue of its own is a decision
		// somebody makes here, on purpose, rather than a side effect.
		t.Errorf("gemini's fallback hue is %q, want the identity hue", h)
	}

	// Each seatable vendor's NAME actually renders in its own hue, on the surface
	// with the highest payoff — a turn page, where the seats stack in one column
	// and position answers nothing.
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	for _, v := range addressableVendors() {
		line := seatRule(v, "Some Seat", "✓ done  1s", 60, sty, g)
		if !strings.Contains(line, sty.SeatStrong(v).Render("Some Seat")) {
			t.Errorf("%s's seat rule does not render its name in its own hue: %q",
				v, stripANSI(line))
		}
		// Two different seats must not produce the same styled name.
		if v != model.VendorCodex &&
			strings.Contains(line, sty.SeatStrong(model.VendorCodex).Render("Some Seat")) {
			t.Errorf("%s's seat rule renders in codex's hue", v)
		}
	}
}

// TestNoSeatHueIsASeverity is the boundary that makes this exception safe to
// grant at all.
//
// The green/yellow/red ramp — 1/2/3 and their bright twins 9/10/11 — is
// severity, on every surface this product draws. A seat that happened to wear
// red would be a seat that reads as failed, on a row where `✗ failed` is the
// thing beside it. The chrome family is fenced off for the same kind of reason:
// 0/7/8/15 are the gauge track and the terminal's own fore/background.
func TestNoSeatHueIsASeverity(t *testing.T) {
	severity := map[string]bool{"1": true, "2": true, "3": true,
		"9": true, "10": true, "11": true}
	chrome := map[string]bool{"0": true, "7": true, "8": true, "15": true}

	for _, v := range seatVendors {
		h := seatHue(v)
		if severity[h] {
			t.Errorf("%s's seat hue %q is in the severity family; a seat would read "+
				"as an outcome", v, h)
		}
		if chrome[h] {
			t.Errorf("%s's seat hue %q is in the chrome family", v, h)
		}
	}
}

// TestSeatHuesAreExhaustive fails the build when a VendorID is added without
// anybody deciding what colour that seat is.
//
// A default branch is the right BEHAVIOUR — an unknown seat renders in the
// identity hue rather than breaking — and it is exactly what makes the decision
// skippable, because nothing goes wrong on screen. So the guard is here instead:
// the list above has to be updated in the same change that adds a vendor, and
// updating it is what puts the hue question in front of whoever is doing it.
func TestSeatHuesAreExhaustive(t *testing.T) {
	seats := addressableVendors()
	if len(seats) != 5 {
		t.Fatalf("the room seats %d vendors; §9.28 plus the Grok amendment decided a hue "+
			"for 5. A sixth seat needs a hue decision, and it is now the HARD one: the "+
			"legal set (4,5,6,12,13,14) has exactly 13 left, after which a new seat "+
			"cannot have its own hue without taking a severity or abandoning 4-bit "+
			"indices — see seatHue's note. Decide it before this number moves.", len(seats))
	}
	seen := map[string]model.VendorID{}
	for _, v := range seats {
		h := seatHue(v)
		if h == "" {
			t.Errorf("%s has no seat hue at all", v)
			continue
		}
		if other, dup := seen[h]; dup {
			t.Errorf("%s and %s share hue %q; a seat hue that is not a seat's own is a "+
				"hue spent for nothing", v, other, h)
		}
		seen[h] = v
	}
}

// TestSeatHuesAreInvisibleToPlainStyles is the golden contract, and on this pass
// it is the whole verification story: every site the hue reaches renders through
// PlainStyles as the identity function, so a golden that moved on this change is
// a bug rather than a regold.
func TestSeatHuesAreInvisibleToPlainStyles(t *testing.T) {
	p := PlainStyles()
	for _, v := range seatVendors {
		for _, s := range []string{"", "Claude Code", "  padded  "} {
			if got := p.SeatStrong(v).Render(s); got != s {
				t.Errorf("PlainStyles().SeatStrong(%s).Render(%q) = %q, want it unchanged",
					v, s, got)
			}
			if got := p.SeatIdentity(v).Render(s); got != s {
				t.Errorf("PlainStyles().SeatIdentity(%s).Render(%q) = %q, want it unchanged",
					v, s, got)
			}
		}
	}
}

// TestTheHueIsSpentOnlyOnSeatNames is the closed list, from the other side.
//
// Position already answers "which seat" in the grid, so a column header wearing
// a per-seat hue would be a circus row spending the room's newest signal on the
// one question the layout had already settled. Severity owns the phase words and
// the marks beside them; chrome owns the rules and leaders; a posture badge is a
// claim that must not compete with a name.
func TestTheHueIsSpentOnlyOnSeatNames(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := talking()
	frame := Render(st, sty, g)

	// The grid's column header keeps the room's identity hue, not the seat's.
	if strings.Contains(frame, sty.SeatStrong(model.VendorClaude).Render("Claude Code")) {
		t.Error("a grid column header wears a seat hue; position already answers which seat it is")
	}
	if !strings.Contains(frame, sty.Strong.Render("Claude Code")) {
		t.Error("the grid's focused column header lost the room's identity hue")
	}

	// Phase words stay severity, on a seat with a hue of its own.
	done := talking()
	done.Columns[1].Phase = PhaseDone
	if !strings.Contains(Render(done, sty, g), sty.SevOK.Render(g.ActOK+" done")) {
		t.Error("a phase word stopped rendering as a severity")
	}

	// A posture badge stays a claim.
	if !strings.Contains(frame, sty.Alert.Render("unsandboxed")) {
		t.Error("a posture badge lost its own style")
	}
}

// TestTheTabBarSortsBySeat: the tab bar is the other place a seat NAME heads a
// reading area, and the tier where a reader picks one by name.
func TestTheTabBarSortsBySeat(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := talking()
	st.Width = 80 // tabs tier
	st.Focus = 0
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.SeatStrong(model.VendorClaude).Render("Claude Code")) {
		t.Error("the selected tab does not carry its seat's hue at weight")
	}
	if !strings.Contains(frame, sty.SeatIdentity(model.VendorCodex).Render("Codex")) {
		t.Error("an unselected tab does not carry its seat's hue")
	}
	// The selected tab still outranks the others by WEIGHT and by the mark, which
	// is what survives NO_COLOR — the hue is the second signal here as everywhere.
	if !strings.Contains(stripANSI(frame), g.Focus+" 1 CC Claude Code") {
		t.Error("the selected tab lost the focus mark; the hue would be carrying it alone")
	}
	if strings.Contains(frame, sty.SeatStrong(model.VendorCodex).Render("Codex")) {
		t.Error("an unselected tab took weight")
	}
}

// TestACollapsedSeatIsNamedInItsOwnHue. The notice is the one place a seat's
// name appears inside PROSE, and §9.25 kept the two-letter tag out of prose on
// the argument that an abbreviation introduced mid-sentence is one nobody can
// learn there. A hue is not an abbreviation: it costs no cell and teaches
// nothing new.
func TestACollapsedSeatIsNamedInItsOwnHue(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := room()
	st.Columns[1].Avail = AvailNotInstalled
	st.Columns[1].Note = "not found on PATH"
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.SeatIdentity(model.VendorCodex).Render("Codex")) {
		t.Errorf("the collapsed-seat notice does not name Codex in its own hue:\n%s",
			stripANSI(frame))
	}
	// The mark stays a warning and the prose stays chrome.
	if !strings.Contains(frame, sty.SevWarn.Render(g.Warn)) {
		t.Error("the notice lost its warning mark's hue")
	}
	if !strings.Contains(frame, sty.Muted.Render(" 1 seat is not on screen: ")) {
		t.Error("the notice's prose is no longer chrome")
	}
	// And nothing on that line took weight — it is a sentence.
	if strings.Contains(frame, sty.SeatStrong(model.VendorCodex).Render("Codex")) {
		t.Error("a seat name inside the notice took weight")
	}
}
