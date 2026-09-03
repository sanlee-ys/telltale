package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The MONOGRAPH set retires §9.28's five seat hues and keeps its closed list.
// Council spends ONE identity ink on seat names, and what separates the seat the
// keys move from the rest is WEIGHT — see style.go's seatInk for the argument.
//
// Every assertion here is over a RENDERED string with NewStyles, because the
// whole feature is invisible to PlainStyles by construction — which is also the
// property TestSeatInkIsInvisibleToPlainStyles pins directly, and the reason
// this pass regolds nothing.

// seatVendors is every VendorID that can reach a seat's ink, seatable or not —
// the five the room seats plus Gemini, which is in the normalized model. It is
// kept now that the hue is gone because the CALL SITES are still a closed list
// and these tests walk it.
var seatVendors = []model.VendorID{
	model.VendorClaude, model.VendorCodex, model.VendorGemini,
	model.VendorAntigravity, model.VendorCursor, model.VendorGrok,
}

// TestEverySeatWearsTheRoomsOneInk. No seat has an ink of its own, and no seat
// is missing one either: a vendor the room can seat and a vendor it cannot both
// render in the identity ink, and a sixth vendor therefore needs no decision.
//
// This is the inverse of the assertion it replaces. TestEverySeatWearsItsOwnHue
// pinned uniqueness across a set that style.go admitted was one index from full;
// the ruling that lifted §9.28 is what let the answer be "one ink, and the tag
// is what scales."
func TestEverySeatWearsTheRoomsOneInk(t *testing.T) {
	sty := NewStyles(true)
	for _, v := range seatVendors {
		if got, want := sty.SeatIdentity(v).Render("x"), sty.Identity.Render("x"); got != want {
			t.Errorf("%s's seat ink is %q, want the room's identity ink %q", v, got, want)
		}
		if got, want := sty.SeatStrong(v).Render("x"), sty.Strong.Render("x"); got != want {
			t.Errorf("%s's seat ink at weight is %q, want %q", v, got, want)
		}
	}

	// Weight, not hue, is what says which seat the keys move — asserted on the
	// surface with the highest payoff, a turn page, where the seats stack in one
	// column and position answers nothing about who is speaking.
	g := UnicodeGlyphs()
	for _, v := range addressableVendors() {
		line := seatRule(v, "Some Seat", "✓ done  1s", 60, sty, g)
		if !strings.Contains(line, sty.SeatStrong(v).Render("Some Seat")) {
			t.Errorf("%s's seat rule does not render its name at the seat weight: %q",
				v, stripANSI(line))
		}
	}
	if sty.SeatStrong(model.VendorClaude).Render("x") == sty.SeatIdentity(model.VendorClaude).Render("x") {
		t.Error("a seat at weight renders identically to one without it; the focus signal is gone")
	}
}

// TestTheIdentityInkIsNotAnAccent is the boundary that survives the retirement,
// restated for a palette of hex inks rather than of 4-bit indices.
//
// A seat that wore the withdrawn ink would read as cancelled and one that wore
// the broke ink would read as failed, on a row where `✗ failed` is the thing
// beside it. The measured ink is fenced off for the neighbouring reason: it
// means a value somebody read, and a NAME is not a reading.
func TestTheIdentityInkIsNotAnAccent(t *testing.T) {
	for _, p := range []struct {
		name string
		pal  Palette
	}{{"night", NightPalette()}, {"paper", PaperPalette()}} {
		for _, bad := range []struct{ what, hex string }{
			{"the withdrawn ink", p.pal.Withdrawn},
			{"the broke ink", p.pal.Broke},
			{"the measured ink", p.pal.Measured},
		} {
			if p.pal.Identity == bad.hex {
				t.Errorf("%s: the identity ink is %s; a seat name would read as one",
					p.name, bad.what)
			}
		}
	}
}

// TestEveryInkIsDistinct fails the build when two tokens collapse onto one
// pigment, which is how a palette silently loses a level.
//
// The MONOGRAPH hierarchy is carried by VALUE — Measured above Text above Muted
// above Dim above RuleInk above Hair — so two tokens sharing a hex is not a tidy
// palette, it is a distinction that stopped being drawn.
func TestEveryInkIsDistinct(t *testing.T) {
	for _, p := range []struct {
		name string
		pal  Palette
	}{{"night", NightPalette()}, {"paper", PaperPalette()}} {
		// Text is deliberately empty — vendor prose renders in the terminal's own
		// foreground, which is what keeps the bare body lines in the palette (see
		// Palette.Text). It is asserted by name rather than walked with the rest.
		if p.pal.Text != "" {
			t.Errorf("%s: Text is %q; prose renders in the terminal's own ink",
				p.name, p.pal.Text)
		}
		seen := map[string]string{}
		for _, tok := range []struct{ name, hex string }{
			{"Measured", p.pal.Measured},
			{"Muted", p.pal.Muted}, {"Dim", p.pal.Dim},
			{"RuleInk", p.pal.RuleInk}, {"Hair", p.pal.Hair},
			{"Identity", p.pal.Identity}, {"Withdrawn", p.pal.Withdrawn},
			{"Broke", p.pal.Broke},
		} {
			if tok.hex == "" {
				t.Errorf("%s: %s has no value", p.name, tok.name)
				continue
			}
			if other, dup := seen[tok.hex]; dup {
				t.Errorf("%s: %s and %s are both %s; a level stopped being drawn",
					p.name, tok.name, other, tok.hex)
			}
			seen[tok.hex] = tok.name
		}
	}
}

// TestSeatInkIsInvisibleToPlainStyles is the golden contract, and on this pass
// it is the whole verification story: every site the ink reaches renders through
// PlainStyles as the identity function, so a golden that moved on this change is
// a bug rather than a regold.
func TestSeatInkIsInvisibleToPlainStyles(t *testing.T) {
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
	// The MONOGRAPH tokens are on the same contract, and it is what makes the
	// whole identity free: each one is a colour or a weight, so every layout
	// golden is blind to it.
	for _, tok := range []struct {
		name  string
		style func(Styles) interface{ Render(...string) string }
	}{
		{"Measured", func(s Styles) interface{ Render(...string) string } { return s.Measured }},
		{"Hair", func(s Styles) interface{ Render(...string) string } { return s.Hair }},
		{"RuleInk", func(s Styles) interface{ Render(...string) string } { return s.RuleInk }},
		{"Focus", func(s Styles) interface{ Render(...string) string } { return s.Focus }},
	} {
		if got := tok.style(p).Render("x"); got != "x" {
			t.Errorf("PlainStyles().%s.Render(%q) = %q, want it unchanged", tok.name, "x", got)
		}
	}
}

// TestTheInkIsSpentOnlyOnSeatNames is the closed list, from the other side.
//
// Position already answers "which seat" in the grid, so a column header renders
// in the room's own Strong rather than through the seat accessors. Severity owns
// the phase words and the marks beside them; the two rule inks own the rules and
// leaders; a posture badge is a claim that must not compete with a name.
func TestTheInkIsSpentOnlyOnSeatNames(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := talking()
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.Strong.Render("Claude Code")) {
		t.Error("the grid's focused column header lost the room's identity ink at weight")
	}

	// Phase words stay severity, and `done` now wears the MEASURED ink: a turn
	// that ended is a reading, not a hue of its own.
	done := talking()
	done.Columns[1].Phase = PhaseDone
	if !strings.Contains(Render(done, sty, g), sty.SevOK.Render(g.ActOK+" done")) {
		t.Error("a phase word stopped rendering as a severity")
	}
	if sty.SevOK.Render("x") != sty.Measured.Render("x") {
		t.Error("a finished turn no longer reports in the measured ink")
	}

	// A posture badge stays a claim.
	if !strings.Contains(frame, sty.Alert.Render("unsandboxed")) {
		t.Error("a posture badge lost its own style")
	}
}

// TestTheTabBarSortsBySeat: the tab bar is the other place a seat NAME heads a
// reading area, and the tier where a reader picks one by name. The selected tab
// is the one at weight.
func TestTheTabBarSortsBySeat(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := talking()
	st.Width = 80 // tabs tier
	st.Focus = 0
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.SeatStrong(model.VendorClaude).Render("Claude Code")) {
		t.Error("the selected tab does not carry the seat ink at weight")
	}
	if !strings.Contains(frame, sty.SeatIdentity(model.VendorCodex).Render("Codex")) {
		t.Error("an unselected tab does not carry the seat ink")
	}
	// The selected tab still outranks the others by WEIGHT and by the mark, which
	// is what survives NO_COLOR — the ink is the second signal here as everywhere.
	if !strings.Contains(stripANSI(frame), g.Focus+" 1 CC Claude Code") {
		t.Error("the selected tab lost the focus mark; the ink would be carrying it alone")
	}
	if strings.Contains(frame, sty.SeatStrong(model.VendorCodex).Render("Codex")) {
		t.Error("an unselected tab took weight")
	}
}

// TestACollapsedSeatIsNamedInTheSeatInk. The notice is the one place a seat's
// name appears inside PROSE, and §9.25 kept the two-letter tag out of prose on
// the argument that an abbreviation introduced mid-sentence is one nobody can
// learn there. An ink is not an abbreviation: it costs no cell and teaches
// nothing new.
func TestACollapsedSeatIsNamedInTheSeatInk(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := room()
	st.Columns[1].Avail = AvailNotInstalled
	st.Columns[1].Note = "not found on PATH"
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.SeatIdentity(model.VendorCodex).Render("Codex")) {
		t.Errorf("the collapsed-seat notice does not name Codex in the seat ink:\n%s",
			stripANSI(frame))
	}
	// The mark stays a warning and the prose stays chrome.
	if !strings.Contains(frame, sty.SevWarn.Render(g.Warn)) {
		t.Error("the notice lost its warning mark's ink")
	}
	if !strings.Contains(frame, sty.Muted.Render(" 1 seat is not on screen: ")) {
		t.Error("the notice's prose is no longer chrome")
	}
	// And nothing on that line took weight — it is a sentence.
	if strings.Contains(frame, sty.SeatStrong(model.VendorCodex).Render("Codex")) {
		t.Error("a seat name inside the notice took weight")
	}
}
