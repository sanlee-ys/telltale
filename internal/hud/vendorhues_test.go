package hud

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/theme"
)

// §7.17's per-vendor hues: the HUD half of council's ratified exception
// (§9.28), extended to the fleet usage view on 2026-08-09.
//
// Every assertion here is over a string rendered with NewStyles, because the
// whole feature is invisible to PlainStyles by construction — which is also
// what TestVendorHuesAreInvisibleToPlainStyles pins directly, and the reason
// this change regolds nothing.

// hueOpen is the opening escape a style emits. Lipgloss v2 always writes the
// sequence and Bubble Tea downsamples it later (§7.5), so this is stable in a
// test with no terminal attached.
func hueOpen(t *testing.T, st lipgloss.Style) string {
	t.Helper()
	r := st.Render("x")
	i := strings.Index(r, "x")
	if i <= 0 {
		t.Fatalf("a coloured style rendered no escape: %q — this whole file would be vacuous", r)
	}
	return r[:i]
}

// ownHueVendors are the vendors §7.17 gives a hue of their OWN. Gemini is
// deliberately absent: it takes the identity hue as a documented fallback, so
// nothing here can tell its heading from an un-retinted one, and asserting
// otherwise would be asserting a decision nobody made.
var ownHueVendors = []model.VendorID{
	model.VendorClaude, model.VendorCodex,
	model.VendorAntigravity, model.VendorCursor, model.VendorGrok,
	model.VendorPi,
}

// TestEveryVendorWearsItsOwnHueOnTheUsagePage. Six blocks stack in one column
// on this surface, so position answers nothing about which vendor a paragraph
// belongs to — the condition council named for when a hue earns its place.
func TestEveryVendorWearsItsOwnHueOnTheUsagePage(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	frame := Render(usageFleetState(120, 28), sty, g)

	for _, v := range fleetOrder {
		if v == model.VendorPi {
			// usageFleetState has no Pi session and Pi has no quota, so
			// usageBlocks omits the heading. vendorHue(pi) is asserted
			// in TestVendorHuesMatchCouncilsSeats.
			continue
		}
		if v == model.VendorSelfReported {
			// Same omission as Pi — usageFleetState has no drop-file row — and
			// on top of it this id gets no hue of its own, deliberately. A hue
			// is a vendor IDENTITY (§9.28), and these rows have none to give:
			// what they share is a provenance, which the word "self-reported"
			// and the "SR" tag already carry without spending a colour. It
			// takes the identity-hue fallback exactly as Gemini does.
			continue
		}
		name := string(v)
		if !strings.Contains(frame, sty.VendorIdentity(v).Render(name)) {
			t.Errorf("%s's usage heading does not render its name in its own hue:\n%s",
				v, stripANSI(frame))
		}
	}

	// And the retint actually happened: a vendor with a hue of its own no longer
	// renders its name in the identity hue every vendor heading used to wear.
	// Codex is excluded because its hue IS the identity hue — kept, per §9.28,
	// by the vendor that already had it — so the two strings are equal there by
	// design rather than by regression.
	for _, v := range ownHueVendors {
		if v == model.VendorCodex {
			continue
		}
		if strings.Contains(frame, sty.Identity.Render(string(v))) {
			t.Errorf("%s's usage heading still renders in the shared identity hue", v)
		}
	}
}

// TestNoVendorHueIsASeverityOrChrome is the fence that makes this exception
// safe to grant on THIS surface in particular.
//
// The green/yellow/red ramp — 1/2/3 and their bright twins 9/10/11 — is
// severity everywhere in this product, and on the fleet usage view it is
// carrying the account percentages a few cells to the right of the vendor name.
// A vendor heading that happened to wear red would read as an account in
// trouble. The chrome family 0/7/8/15 is the gauge track and the terminal's own
// fore/background.
func TestNoVendorHueIsASeverityOrChrome(t *testing.T) {
	severity := map[string]bool{"1": true, "2": true, "3": true,
		"9": true, "10": true, "11": true}
	chrome := map[string]bool{"0": true, "7": true, "8": true, "15": true}

	for _, v := range append(append([]model.VendorID{}, fleetOrder...), model.VendorID("newvendor")) {
		h := vendorHue(v)
		if h == "" {
			t.Errorf("%s has no hue at all; the fallback exists so this cannot happen", v)
		}
		if severity[h] {
			t.Errorf("%s's hue %q is in the severity family; the vendor would read as an outcome", v, h)
		}
		if chrome[h] {
			t.Errorf("%s's hue %q is in the chrome family", v, h)
		}
	}

	// Distinct, or the hue is spent for nothing. Gemini is expected to collide
	// with Codex — that is the documented fallback, not a clash — so it is the
	// vendors with a decision behind them that must not share.
	seen := map[string]model.VendorID{}
	for _, v := range ownHueVendors {
		h := vendorHue(v)
		if other, dup := seen[h]; dup {
			t.Errorf("%s and %s share hue %q", v, other, h)
		}
		seen[h] = v
	}
	if got := vendorHue(model.VendorGemini); got != theme.ColorIdentity {
		t.Errorf("gemini's fallback hue is %q, want the identity hue — giving it one of its "+
			"own is a decision somebody makes here, on purpose", got)
	}
}

// TestVendorHuesMatchCouncilsSeats asserts the HUD's map against council's by
// LITERAL string, the same shape as TestStripTagsMatchTheHUDSpelling one
// package over and for the same reason.
//
// Deliberately not a call into internal/council. One product, one vocabulary —
// a reader who learned in the room that magenta is Claude must not meet a
// second colour for Claude one keypress away — but the seam between the two
// surfaces is the normalized session model and internal/theme's numbers and
// nothing else, and reaching across it for a rendering detail is the coupling
// that seam exists to prevent. (The hue map is not IN internal/theme for the
// reason vendorHue's doc comment gives: theme's contract is one hue, one
// meaning, across every surface, and internal/statusline has no vendor blocks.)
//
// So council's numbers are written out here, and this test is the thing that
// fails when one copy moves without the other.
func TestVendorHuesMatchCouncilsSeats(t *testing.T) {
	// internal/council/style.go, seatHue — §9.28 plus its Grok amendment.
	want := map[model.VendorID]string{
		model.VendorClaude:      "5",  // magenta
		model.VendorCodex:       "6",  // cyan, theme's identity hue
		model.VendorAntigravity: "4",  // blue
		model.VendorCursor:      "12", // bright blue
		model.VendorGrok:        "14", // bright cyan
	}
	if got := vendorHue(model.VendorPi); got != "13" {
		t.Errorf("vendorHue(pi) = %q, want 13 — Pi is HUD-only and has no council seat hue to match", got)
	}
	for v, hue := range want {
		if got := vendorHue(v); got != hue {
			t.Errorf("vendorHue(%s) = %q, want %q — internal/council seats it at %q, and two "+
				"surfaces teaching different colours for one vendor is the defect this asserts against",
				v, got, hue, hue)
		}
	}
	// Gemini has no seat hue in the room either; both surfaces fall back.
	if got := vendorHue(model.VendorGemini); got != "6" {
		t.Errorf("vendorHue(gemini) = %q; council falls back to the identity hue %q", got, "6")
	}
}

// TestVendorHuesAreInvisibleToPlainStyles is the golden contract, and on this
// change it is the whole verification story: the one site the hue reaches
// renders through PlainStyles as the identity function, so a golden that moved
// is a bug rather than a regold.
func TestVendorHuesAreInvisibleToPlainStyles(t *testing.T) {
	p := PlainStyles()
	for _, v := range append(append([]model.VendorID{}, fleetOrder...), model.VendorID("newvendor")) {
		for _, s := range []string{"", "claude", "  padded  "} {
			if got := p.VendorIdentity(v).Render(s); got != s {
				t.Errorf("PlainStyles().VendorIdentity(%s).Render(%q) = %q, want it unchanged",
					v, s, got)
			}
		}
	}
	// And from the frame's side: the plain render of the surface the hue lives
	// on carries no escape at all.
	if frame := Render(usageFleetState(120, 28), p, UnicodeGlyphs()); strings.Contains(frame, "\x1b[") {
		t.Error("the plain usage render emitted an ANSI escape; every golden on this surface is now wrong")
	}
}

// TestTheVendorHueIsSpentOnlyOnTheVendorName is the closed list, from the other
// side. Everything else on this surface already has an owner: severity owns the
// percentages and the gauges, chrome owns the labels, the seam sentence and the
// countdowns, and theme's identity hue owns a model name in the census exactly
// as it does in the grid's MODEL column.
//
// The check reads the four hues that are NOT theme's identity — 5, 4, 12, 14.
// Codex's 6 is unfalsifiable here by construction, because a leak of it is
// indistinguishable from the identity hue this surface legitimately spends in
// several places; that is the price of §9.28's decision to keep cyan with the
// vendor that already had it, and the other four cover the same code path.
func TestTheVendorHueIsSpentOnlyOnTheVendorName(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := usageFleetState(120, 28)
	frame := Render(st, sty, g)

	var marks []string
	for _, v := range ownHueVendors {
		if vendorHue(v) == theme.ColorIdentity {
			continue
		}
		marks = append(marks, hueOpen(t, sty.VendorIdentity(v)))
	}

	sep := rule(st.Width, sty, g)
	lines := strings.Split(frame, "\n")
	rules := 0
	for _, l := range lines {
		if l == sep {
			rules++
			continue
		}
		for _, m := range marks {
			if !strings.Contains(l, m) {
				continue
			}
			switch {
			case rules == 0:
				// The header — including the quota block and the spend line —
				// is untouched by this change. It is a GLANCE surface whose
				// vendors are named by two-letter tag in a fixed order, so the
				// question the hue answers is not the one it is asking.
				t.Errorf("a vendor hue reached the header:\n%s", stripANSI(l))
			case rules == 1 && strings.HasPrefix(l, strings.Repeat(" ", usageIndent)):
				t.Errorf("a vendor hue reached a fact row; the heading is the whole list:\n%s",
					stripANSI(l))
			case rules >= 2:
				t.Errorf("a vendor hue reached the footer:\n%s", stripANSI(l))
			}
		}
	}
	if rules != 2 {
		t.Fatalf("expected a body between two rules, found %d", rules)
	}

	// The facts the heading sits above keep the owners they already had.
	if !strings.Contains(frame, sty.Muted.Render("quota read from its own store, this scan")) {
		t.Error("the quota seam sentence stopped being chrome")
	}
	if !strings.Contains(frame, sty.Identity.Render("gpt-5.1-codex")) {
		t.Error("the models census stopped using theme's identity hue for a model name")
	}
	if !strings.Contains(frame, sty.SevWarn.Render(padLeft(theme.Percent(79), usagePct, g))) {
		t.Error("a quota percentage stopped being a severity")
	}
	if !strings.Contains(frame, sty.Muted.Render(padRight("spent", usageLabel, g))) {
		t.Error("the spend line's verb stopped being a chrome label")
	}
}
