package council

import (
	"strings"
	"testing"
	"time"
)

// railRoom is three seats that all answered the same turn, so every column has
// prose of its own and the frame reads the same whichever seat holds the keys.
//
// Purpose-built rather than reusing `talking`, whose seats deliberately have
// transcripts of very different lengths: this file's assertions are about which
// column a mark is beside, and a fixture where only one column has content
// cannot tell "the mark moved" from "the other columns are empty".
func railRoom() State {
	st := room()
	st.Height = 30
	st.Turn = 1
	for i := range st.Columns {
		c := &st.Columns[i]
		c.startTurn(1, "which way should the room read?", false)
		c.Phase, c.Elapsed = PhaseDone, time.Second
	}
	st.Columns[0].Body = "the leftmost seat answered."
	st.Columns[1].Body = "the middle seat answered."
	st.Columns[2].Body = "the rightmost seat answered."
	return st
}

// §9.27: focus is legible from across the room. The thick rail marks the
// focused column's left edge for the full height of the band, and every other
// column's prose steps down one level of contrast.
//
// The rail is a CHARACTER and the contrast is an ATTRIBUTE, which is why the
// two halves are asserted differently: the rail is in the goldens and in the
// rendered string, the demotion is asserted where colour is asserted (§9.5).

// railRow is the body row of a rendered frame that carries this text, split into
// its cells at the gutter separators — with the leading frame pad kept, because
// the leftmost column's mark lives there.
func railLine(t *testing.T, frame, containing string) string {
	t.Helper()
	for _, ln := range strings.Split(frame, "\n") {
		if strings.Contains(ln, containing) {
			return ln
		}
	}
	t.Fatalf("no body row containing %q:\n%s", containing, frame)
	return ""
}

// TestTheThickRailFollowsFocus is the device itself: tab moves the keys, and the
// heavy mark moves with them, leaving a thin rail behind.
//
// Two presses rather than one, because one press cannot tell "the mark moved"
// from "the mark is always at position 1" — and the second press is what puts a
// focused column in the MIDDLE of the frame, which is the only position where
// the mark rides a gutter rather than the frame's own pad.
func TestTheThickRailFollowsFocus(t *testing.T) {
	g := UnicodeGlyphs()
	m := &Model{st: railRoom(), glyphs: g}
	sepPad := strings.Repeat(" ", gutter)

	// Position 0: no gutter to its left, so the mark is in the frame's left pad.
	frame := render(m.st)
	row := railLine(t, frame, "the leftmost seat answered.")
	if !strings.HasPrefix(row, g.FocusRail) {
		t.Errorf("the leftmost focused column has no rail in the frame pad: %q", row)
	}
	if strings.Contains(row, sepPad+g.FocusRail+sepPad) {
		t.Errorf("a gutter carries the thick rail while position 0 is focused: %q", row)
	}

	m.focusBy(1)
	if m.st.Focus != 1 {
		t.Fatalf("tab left focus at %d, want 1", m.st.Focus)
	}
	frame = render(m.st)
	row = railLine(t, frame, "the leftmost seat answered.")
	if strings.HasPrefix(row, g.FocusRail) {
		t.Errorf("the frame pad still carries the rail after focus moved off position 0: %q", row)
	}
	// Exactly one gutter is heavy, and it is the one left of column 1.
	if n := strings.Count(row, g.FocusRail); n != 1 {
		t.Errorf("row carries %d thick rails, want exactly 1: %q", n, row)
	}
	cells := strings.Split(row, sepPad+g.FocusRail+sepPad)
	if len(cells) != 2 || !strings.Contains(cells[0], "Claude") && !strings.Contains(row, g.Sep) {
		t.Errorf("the thick rail is not spelled as a gutter (%q air each side): %q", sepPad, row)
	}
	// And the gutter it vacated went back to the thin rail.
	if !strings.Contains(row, sepPad+g.Sep+sepPad) {
		t.Errorf("no thin rail left anywhere on the row; every gutter went heavy: %q", row)
	}

	m.focusBy(1)
	frame = render(m.st)
	row = railLine(t, frame, "the leftmost seat answered.")
	if n := strings.Count(row, g.FocusRail); n != 1 {
		t.Errorf("row carries %d thick rails after a second tab, want 1: %q", n, row)
	}
	// The mark is now on the LAST gutter, so everything before it is two columns.
	if i := strings.Index(row, g.FocusRail); i <= strings.LastIndex(row, g.Sep) {
		t.Errorf("the rail did not move to the last gutter: %q", row)
	}
}

// TestTheFocusRailGolden is the picture, and it pins the case no other golden in
// this package reaches: the focused column in the MIDDLE of the frame, where the
// mark rides a gutter rather than the frame's own left pad.
//
// Every pre-existing golden was rendered with focus at position 0, so regolding
// the package would have moved 44 files and covered exactly one of the two
// shapes this device has. Its own file, on the CLAUDE.md rule: the name says the
// property it pins down.
func TestTheFocusRailGolden(t *testing.T) {
	st := railRoom()
	st.Focus = 1
	golden(t, "focus-rail", render(st))
}

// TestTheRailRidesTheSameBandTheThinOneDoes. §9.23 decided which rows carry a
// separator at all — content, or a lone blank bridging two content rows — and
// focus does not get its own answer to that question. A rail that ran the full
// body height on the focused column would be the "spear through a void" §9.23
// declined, drawn once instead of four times.
func TestTheRailRidesTheSameBandTheThinOneDoes(t *testing.T) {
	g := UnicodeGlyphs()
	st := room() // idle: chrome at the top, one line anchored at the bottom
	st.Width, st.Height = 120, 60

	thick, thin, bare := 0, 0, 0
	for _, ln := range frameBody(t, render(st), g) {
		switch {
		case strings.Contains(ln, g.FocusRail):
			thick++
		case strings.Contains(ln, g.Sep):
			thin++
		default:
			bare++
		}
	}
	if thick == 0 {
		t.Fatal("the focused column has no rail at all in an idle room")
	}
	if bare == 0 {
		t.Errorf("every body row of a tall idle room carries a rail; focus speared the void "+
			"(%d thick, %d thin, %d bare)", thick, thin, bare)
	}
	// Every row that carries the thin rail between the other two columns carries
	// the thick one too, and vice versa: one band, two weights.
	if thin != 0 {
		t.Errorf("%d rows carry a gutter but no thick rail; the two rails disagree about the band", thin)
	}
}

// TestTheFocusRailSurvivesASCII. The rail is the tallest carrier of focus in the
// room, so a glyph set that dropped it would leave `▸` — which §9.18's ladder
// shows the strip already sheds — as the only one.
func TestTheFocusRailSurvivesASCII(t *testing.T) {
	a := GlyphsFor(true)
	st := talking()
	st.ASCII = true
	frame := Render(st, PlainStyles(), a)

	if !strings.Contains(frame, a.FocusRail) {
		t.Errorf("ascii mode draws no focus rail:\n%s", frame)
	}
	// And it is not the separator wearing a different name.
	if a.FocusRail == a.Sep {
		t.Errorf("the ascii focus rail %q is the ascii separator", a.FocusRail)
	}
	claimed := map[string]string{
		a.Sep: "sep", a.Rule: "rule", a.RuleHeavy: "heavy rule", a.Ellipsis: "ellipsis",
		a.Caret: "caret", a.Warn: "warn", a.Focus: "focus mark", a.Prompt: "prompt",
		a.Up: "up", a.Down: "down", a.Act: "act", a.Range: "range", a.Idle: "idle",
		a.ActOK: "ok", a.ActFail: "fail", a.ActUnknown: "unknown",
		"#": "the HUD's gauge fill",
	}
	for _, f := range a.Spinner {
		if _, ok := claimed[f]; !ok {
			claimed[f] = "a spinner frame"
		}
	}
	if owner, ok := claimed[a.FocusRail]; ok {
		t.Errorf("the ascii focus rail %q is already the %s glyph", a.FocusRail, owner)
	}
}

// TestAnUnfocusedColumnsProseStepsBack is B3, asserted where colour is asserted.
//
// PlainStyles renders Dim as the identity function by design, so no golden can
// see this — the same property that makes weight safe to spend (§9.11). The
// focused column's prose renders through `Text`, which is the empty style, so
// "not demoted" is checkable as "the dimmed form of this sentence is not on
// screen" rather than by comparing two escape sequences.
func TestAnUnfocusedColumnsProseStepsBack(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := railRoom()
	st.Focus = 0
	frame := Render(st, sty, g)

	if !strings.Contains(frame, sty.Dim.Render("the middle seat answered.")) {
		t.Error("an unfocused column's prose is not dimmed")
	}
	if !strings.Contains(frame, sty.Dim.Render("the rightmost seat answered.")) {
		t.Error("the second unfocused column's prose is not dimmed")
	}
	if strings.Contains(frame, sty.Dim.Render("the leftmost seat answered.")) {
		t.Error("the focused column's prose was dimmed; the demotion is for the OTHER seats")
	}

	// It follows the keys rather than the column index.
	st.Focus = 1
	frame = Render(st, sty, g)
	if strings.Contains(frame, sty.Dim.Render("the middle seat answered.")) {
		t.Error("a column kept its demotion after the keys moved to it")
	}
	if !strings.Contains(frame, sty.Dim.Render("the leftmost seat answered.")) {
		t.Error("the column the keys left was not demoted")
	}
}

// TestTheDemotionStopsAtTheReadingArea names, one by one, what it must not
// reach. Each of these is a claim rather than reading material, and a claim that
// faded because the reader was looking at the next column is exactly the defect
// §9.2 wrote the badge row to prevent.
//
// Every item here is asserted POSITIVELY — the string must still be on screen in
// the style that outranks Dim — and that is forced rather than stylistic. Dim is
// `Faint`, which is Muted's own attribute, so `Dim.Render(x)` and
// `Muted.Render(x)` are the same bytes and a negative assertion over anything
// already muted would be vacuous. What can be distinguished is everything the
// demotion must NOT flatten into faintness: weight, identity, severity.
func TestTheDemotionStopsAtTheReadingArea(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	st := railRoom()
	st.Focus = 0 // seats 1 and 2 are the unfocused ones
	st.Columns[1].Note = "the vendor exited early"
	frame := Render(st, sty, g)

	for _, tc := range []struct {
		what string
		want string
	}{
		// The user's own words, echoed inside an unfocused column, at weight.
		{"the prompt echo", sty.Strong.Render(g.Prompt + " which way should the room read?")},
		// The posture badge that says a seat can change your files. Alert, not
		// faint, on a column nobody is reading — and ON THE RAIL since
		// 2026-09-03, because the badge row is the posture ledger's own ground
		// (style.go's RailGround). onBand rather than a literal, so this keeps
		// asserting the DEMOTION rather than the ground.
		{"an unsandboxed posture badge", sty.onBand(sty.Alert).Render("unsandboxed")},
		// A failure note's mark keeps the warning hue.
		{"a note card's mark", sty.SevWarn.Render(g.Warn)},
		// And the seat's own name keeps its hue.
		{"an unfocused seat's name", sty.Identity.Render("Codex")},
	} {
		if !strings.Contains(frame, tc.want) {
			t.Errorf("%s did not survive the demotion in an unfocused column: %q",
				tc.what, stripANSI(tc.want))
		}
	}
}

// TestPlainStylesRendersDimAsIdentity is the golden contract, stated directly.
// Every layout golden in this package depends on it.
func TestPlainStylesRendersDimAsIdentity(t *testing.T) {
	p := PlainStyles()
	for _, s := range []string{"", "a reply", "  padded  ", "with a ⚙ mark"} {
		if got := p.Dim.Render(s); got != s {
			t.Errorf("PlainStyles().Dim.Render(%q) = %q, want the input unchanged", s, got)
		}
	}
	// And forSeat cannot turn the identity set into a styling one.
	blur := p.forSeat(seatUnfocused)
	if got := blur.Body().Render("a reply"); got != "a reply" {
		t.Errorf("an unfocused PlainStyles body renders %q, want the input unchanged", got)
	}
	if !blur.Blurred {
		t.Error("forSeat(seatUnfocused) did not mark the set blurred")
	}
	if PlainStyles().forSeat(seatFocused).Blurred ||
		PlainStyles().forSeat(seatAddressed).Blurred {
		t.Error("a seat the keys move was marked blurred")
	}
}

// TestTheRailDoesNotMoveTheGateOrTheScroll is §9.15's collision check applied to
// this pass: a device that only changes which character sits in a gutter must
// not change what any key does or how far it goes.
func TestTheRailDoesNotMoveTheGateOrTheScroll(t *testing.T) {
	st := talking()
	for _, idx := range []int{0, 1, 2} {
		st.Focus = 0
		wantMax := MaxScroll(st, idx)
		lines0, _, avail0, ok0 := columnViewport(st, idx)

		st.Focus = 1
		if got := MaxScroll(st, idx); got != wantMax {
			t.Errorf("column %d's MaxScroll changed with focus: %d then %d", idx, wantMax, got)
		}
		lines1, _, avail1, ok1 := columnViewport(st, idx)
		if ok0 != ok1 || avail0 != avail1 || len(lines0) != len(lines1) {
			t.Errorf("column %d's viewport changed with focus: ok %v/%v avail %d/%d lines %d/%d",
				idx, ok0, ok1, avail0, avail1, len(lines0), len(lines1))
		}
	}

	// A pending gate still owns the frame, and the rail is drawn around it rather
	// than instead of it.
	gated := talking()
	gated.Gates = []PendingGate{{Vendor: gated.Columns[0].Vendor,
		RequestID: "r1", ToolUseID: "t1", Text: "Bash: go test ./..."}}
	if !strings.Contains(render(gated), "go test ./...") {
		t.Error("the gate card is no longer on screen")
	}
}
