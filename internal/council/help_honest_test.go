package council

import (
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// helpFrameRows is the help panel's own rows out of a rendered frame: the region
// between the two full-width rules, trimmed of the trailing pad.
func helpFrameRows(t *testing.T, st State) []string {
	t.Helper()
	g := GlyphsFor(false)
	var rows []string
	start := -1
	for i, l := range strings.Split(render(st), "\n") {
		if !frameEdge(l, st.Width, g) {
			continue
		}
		if start < 0 {
			start = i
			continue
		}
		rows = strings.Split(render(st), "\n")[start+1 : i]
		break
	}
	if rows == nil {
		t.Fatalf("could not find the help body between the room's rules at %dx%d",
			st.Width, st.Height)
	}
	return rows
}

// helpGeometries is every room the panel actually has to survive: the reference
// machine's own (a seat that will not run costs a notice row, the narrow tier
// costs a tab bar) plus the plain wide one.
func helpGeometries() []struct {
	name string
	st   func() State
	w, h int
} {
	withDeadSeat := func() State {
		st := room()
		st.Columns = append(st.Columns, Column{
			Vendor: model.VendorCursor, Label: "Cursor",
			Avail: AvailNotInstalled, Note: "not found on PATH",
		})
		return st
	}
	return []struct {
		name string
		st   func() State
		w, h int
	}{
		{"wide", room, 120, 24},
		{"wide+notice", withDeadSeat, 120, 24},
		{"tabs+notice", withDeadSeat, 80, 24},
		{"tall", room, 120, 60},
	}
}

// TestTheHelpPanelSaysWhenItIsHoldingMore is the §4a.1 defect this pass exists
// to close.
//
// Every other surface in this room spends a body row on `↓ N more below` when
// content does not fit, on the explicit argument that silent clipping is
// indistinguishable from there being nothing more to say. The help panel — 24
// rows on page one and 33 on page two against a hard 17-row budget — was the one
// place that just stopped, mid-sentence and mid-word (`…the containment, not a`).
//
// The assertion is the honest one rather than a row count: if the panel is
// holding lines it is not showing, the last row it shows has to say so.
func TestTheHelpPanelSaysWhenItIsHoldingMore(t *testing.T) {
	g := GlyphsFor(false)
	for _, tc := range helpGeometries() {
		for _, page := range []HelpPage{HelpKeys, HelpPostures} {
			st := tc.st()
			st.Width, st.Height, st.Help = tc.w, tc.h, page
			lay := layoutFor(st, g)

			all := helpKeys(lay, PlainStyles(), g)
			if page == HelpPostures {
				all = helpPostures(st, lay, PlainStyles(), g)
			}
			rows := helpFrameRows(t, st)
			hidden := len(all) > lay.Body
			marked := false
			for _, r := range rows {
				if strings.Contains(r, g.Down) && strings.Contains(r, "below") {
					marked = true
				}
			}
			if hidden && !marked {
				t.Errorf("%s page %v: the panel holds %d lines in %d rows and says nothing about it\n%s",
					tc.name, page, len(all), lay.Body, strings.Join(rows, "\n"))
			}
			if !hidden && marked {
				t.Errorf("%s page %v: the panel claims there is more below and there is not",
					tc.name, page)
			}
		}
	}
}

// TestTheHelpPanelNeverLosesItsWayOut. `?` is the only documented way back out
// of this panel, and on both pages it sits at exactly row 17 of a 17-row budget
// — so a marker taking the last row the ordinary way would have bought honesty
// with the exit. It is pinned instead, which turns a lucky row count into a
// structural guarantee.
func TestTheHelpPanelNeverLosesItsWayOut(t *testing.T) {
	for _, tc := range helpGeometries() {
		for _, pg := range []struct {
			page HelpPage
			want string
		}{
			{HelpKeys, "?            next page"},
			{HelpPostures, "?            close"},
		} {
			st := tc.st()
			st.Width, st.Height, st.Help = tc.w, tc.h, pg.page
			rows := helpFrameRows(t, st)
			if !strings.Contains(strings.Join(rows, "\n"), pg.want) {
				t.Errorf("%s (%dx%d) page %v: the panel lost %q — no way back to the room\n%s",
					tc.name, tc.w, tc.h, pg.page, pg.want, strings.Join(rows, "\n"))
			}
		}
	}
}

// TestTheHelpPanelNamesEverySeatItCanReach is the panel's version of
// cmd/telltale's TestUsageNamesEverySeat, and it exists for the same measured
// failure.
//
// grok became the fifth seat (§9.39) and eight surfaces went on describing the
// four the room had before it. SeatNames() was extracted to derive all eight —
// and this panel was not among them, so it kept its hand-typed roster: `--help`
// named the seat and `?`, the surface a user inside the room actually reaches
// for, did not. Two rows carried it — the routing row listed four lanes, and
// the focus row offered `1-4` over a key handler that has bound 1-9 against
// VisibleColumns() the whole time.
//
// Walked rather than pinned to a string, so the assertion is "the roster is
// named" and not "it is punctuated this way" — rewrapping the panel stays free,
// and a sixth seat fails this test until both rows carry it.
func TestTheHelpPanelNamesEverySeatItCanReach(t *testing.T) {
	g := GlyphsFor(false)
	st := room()
	st.Width, st.Height = 120, 60
	page := strings.Join(helpKeys(layoutFor(st, g), PlainStyles(), g), "\n")

	for _, seat := range SeatNames() {
		if !strings.Contains(page, "@"+seat) {
			t.Errorf("the help panel never names the %q lane — @%s routes a turn, and the "+
				"one page that teaches routing says the seat does not exist\n%s", seat, seat, page)
		}
	}

	// The range's top is the roster's size. A room that seats five and offers
	// `1-4` hides a seat behind a key that already works.
	if want := "1-" + strconv.Itoa(len(SeatNames())); !strings.Contains(page, want) {
		t.Errorf("the focus row does not offer %q for a %d-seat roster\n%s",
			want, len(SeatNames()), page)
	}
}

// TestHelpKeyColHoldsTheProseColumn. helpKeyCol pads a DERIVED key, and a
// derived key is the one thing this panel's hand-counted leading spaces could
// never survive: the focus row's key grows a cell the day the roster reaches ten
// seats, and a misaligned prose column is the exact defect helpIndent was
// extracted to end.
//
// Where the rendered row lands is pinned by TestTheHelpPanelStillFitsItsBudget
// (seatnum_test.go), which already reads that row's prose offset against
// helpIndent. This is the unit underneath it, exercised at the key widths a
// five-seat roster cannot produce yet.
func TestHelpKeyColHoldsTheProseColumn(t *testing.T) {
	for _, key := range []string{"tab / 1-5", "tab / 1-12", "i / enter", ""} {
		if got := len(helpKeyCol(key)); got != helpIndent {
			t.Errorf("helpKeyCol(%q) is %d cells, not helpIndent (%d) — the prose column shears",
				key, got, helpIndent)
		}
	}
	// An over-long key keeps a separating space rather than colliding with the
	// prose or being truncated into a different key.
	long := strings.Repeat("x", helpIndent)
	if got := helpKeyCol(long); !strings.HasSuffix(got, " ") || !strings.Contains(got, long) {
		t.Errorf("helpKeyCol(%q) = %q — an over-long key must keep its whole text and a space", long, got)
	}
}

// TestTheHelpPanelPromisesNoKeyItDoesNotHave. `↑↓` do nothing over the help
// panel — key() routes no scroll to it — so naming them in the mode line while
// it is open is §7.8's surprise pointing the other way, and the overflow marker
// must not offer them either. The panel says there is more; it does not pretend
// you can reach it.
func TestTheHelpPanelPromisesNoKeyItDoesNotHave(t *testing.T) {
	g := GlyphsFor(false)
	for _, page := range []HelpPage{HelpKeys, HelpPostures} {
		st := room()
		st.Width, st.Height, st.Help = 120, 24, page
		frame := render(st)
		mode := lastLine(frame)
		if strings.Contains(mode, g.Up+g.Down+" scroll") {
			t.Errorf("page %v: the mode line offers scroll over a panel no scroll key reaches: %q",
				page, mode)
		}
		for _, r := range helpFrameRows(t, st) {
			if strings.Contains(r, "below") && strings.Contains(r, "scroll") {
				t.Errorf("page %v: the overflow marker names a key that does nothing here: %q", page, r)
			}
		}
	}
	// And the hint comes back the moment the panel closes — this removes a false
	// promise, it does not retire a real key.
	st := room()
	st.Width, st.Height = 120, 24
	if !strings.Contains(lastLine(render(st)), g.Up+g.Down+" scroll") {
		t.Error("closing the help panel did not restore the scroll hint")
	}
}

// TestTheHelpPanelHangsItsBodyUnderItsLabel. Every card in this room has had one
// grammar since §9.11 — a title at weight, its body hanging under it — and the
// per-seat posture detail was the last place still drawing the opposite: its
// label started at column 15 and its body at column 6, so the child hung ten
// cells LEFT of its parent and read as a new statement rather than the reason
// for the one above it.
func TestTheHelpPanelHangsItsBodyUnderItsLabel(t *testing.T) {
	g := GlyphsFor(false)
	st := room()
	st.Width, st.Height, st.Help = 120, 60, HelpPostures // tall enough to reach below the fold

	indent := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	var label, body int = -1, -1
	for _, r := range helpFrameRows(t, st) {
		switch {
		case strings.Contains(r, "Claude Code"):
			label = indent(r)
		case label >= 0 && body < 0 && strings.TrimSpace(r) != "":
			body = indent(r)
		}
	}
	if label < 0 || body < 0 {
		t.Fatal("the per-seat posture section did not render")
	}
	if body <= label {
		t.Errorf("a seat's detail hangs at column %d under a label at column %d — "+
			"the child is left of its parent", body, label)
	}
	if want := framePad + helpIndent; body != want {
		t.Errorf("the detail hangs at %d, not the legend's own %d — two lists of one "+
			"vocabulary that do not share a left edge read as two lists", body, want)
	}
	_ = g
}

// TestASeatNameCarriesItsTagEverywhereItHeadsAColumn.
//
// §9.18 introduced `CC` / `CX` / `AG` / `CU` as what identity degrades TO at
// strip width — so the abbreviation appeared exactly where a reader had the
// least context to learn it, and vanished at every width where the room had
// space to teach it. Drawn always, the wide column is the legend for the narrow
// one.
//
// Prose is deliberately excluded: a turn page's seat rule and the collapsed-seat
// notice keep bare names, because the tag earns its place where columns are
// scanned, not inside a sentence.
func TestASeatNameCarriesItsTagEverywhereItHeadsAColumn(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    int
	}{{"columns", 120}, {"tabs", 80}} {
		st := room()
		st.Width = tc.w
		frame := render(st)
		for _, want := range []string{"CC Claude Code", "CX Codex", "AG Antigravity"} {
			if !strings.Contains(frame, want) {
				t.Errorf("%s: a seat heads its column without its tag — %q is missing:\n%s",
					tc.name, want, frame)
			}
		}
	}

	// A turn page's seat rules are prose headings, not column heads.
	page := paged()
	page.Width = 120
	if got := render(page); strings.Contains(got, "CC Claude Code") {
		t.Errorf("a turn page's seat rule wears a column tag:\n%s", got)
	}

	// So is the collapsed-seat notice.
	col := room()
	col.Columns[1].Avail = AvailNotInstalled
	col.Columns[1].Note = "not found on PATH"
	if n := collapsedNotice(col, UnicodeGlyphs()); strings.Contains(n, "CX Codex") {
		t.Errorf("the collapsed-seat notice wears a column tag: %q", n)
	}
}

// TestTheTagSurvivesToTheNarrowestColumn. §9.11 settled that identity yields
// before a state word and §9.18 settled that it yields to the TAG rather than to
// a clipped name. Making the tag permanent must not have reversed either: at
// every width the room draws a column at, the two letters are still there.
func TestTheTagSurvivesToTheNarrowestColumn(t *testing.T) {
	sty, g := PlainStyles(), GlyphsFor(false)
	c := room().Columns[1] // Codex
	st := room()
	for w := 60; w >= minColumn; w -= 4 {
		got := columnHeader(st, c, seatFocused, w, sty, g)
		if !strings.Contains(got, "CX") {
			t.Errorf("w=%d: the column header lost its tag: %q", w, got)
		}
		if lipgloss.Width(got) > w {
			t.Errorf("w=%d: the header is %d cells: %q", w, lipgloss.Width(got), got)
		}
	}
	// And at strip width, where the tag is all the identity there is.
	if got := stripHeader(st, c, seatUnfocused, stripWidth, sty, g); !strings.Contains(got, "CX") {
		t.Errorf("the strip lost its tag: %q", got)
	}
}

// TestAnUnavailableSeatMakesNoClaims is the stray fact this pass removes.
//
// unavailable.txt drew `final only` under `⚠ Codex is not seated` — a claim
// about how a vendor behaves DURING a turn, stated about a vendor that cannot
// take one. Codex was not found on PATH; nothing about its streaming was
// measured, and §4a.1's rule is that a field nothing sourced renders absent
// rather than plausible. Plausible is exactly what it was.
func TestAnUnavailableSeatMakesNoClaims(t *testing.T) {
	sty, g := PlainStyles(), GlyphsFor(false)
	c := Column{
		Vendor: model.VendorCodex, Label: "Codex",
		Avail: AvailNotInstalled, Note: "not found on PATH (looked for codex)",
		// Both set, and both must stay off screen: a granularity word is static
		// per vendor, so detection fills it in whether or not the binary exists.
		Gran:    GranFinalOnly,
		Sandbox: SandboxClaim{Level: SandboxRequested},
	}
	if got := strings.TrimSpace(badgeRow(room(), c, 37, sty, g)); got != "" {
		t.Errorf("a seat that is not installed states %q — a claim about a vendor that is not there", got)
	}

	// The row is still RESERVED, or the grid's rows stop lining up (§9.11).
	st := room()
	st.Seats = Seats{All: true}
	st.Columns[1] = c
	chrome := columnChrome(st, c, seatUnfocused, 37, sty, g)
	other := columnChrome(st, st.Columns[0], seatUnfocused, 37, sty, g)
	if len(chrome) != len(other) {
		t.Errorf("an unavailable column's chrome is %d rows against %d — the grid shears",
			len(chrome), len(other))
	}

	// And what IS known still renders: the card below says which failure it was.
	if !strings.Contains(render(st), "not found on PATH") {
		t.Error("the unavailable card stopped saying why the seat is not there")
	}
}
