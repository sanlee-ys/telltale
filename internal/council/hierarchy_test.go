package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

// Tests for the room's visual hierarchy: the marks that say what a seat is
// doing, the shape that binds a seat to its state, the card grammar the
// degraded columns render in, and the weight the posture badges and the mode
// line carry. A separate file for the same reason gate_view_test.go and
// conversation_test.go are separate — one surface's argument per file.
//
// Every assertion here is the house pattern: two different facts must never
// render alike, and no distinction may depend on colour.

// headerRow is the column-header line of a rendered frame, at the expanded tier
// where exactly one column is drawn and nothing under test wraps.
//
// Row 3: header, rule, tab bar, column header.
func headerRow(st State, g Glyphs) string {
	st.Expanded = true
	return strings.Split(Render(st, PlainStyles(), g), "\n")[3]
}

// TestPhasesRenderAsDistinctMarks is the honest-gauge rule applied to the fact a
// reader scans four columns for.
//
// The six states a seat can be in were separated by the word alone and by a
// colour behind it — and "done", "idle" and "failed" at the far right of a
// 37-cell column, in a room with three of them, is a distinction you have to
// stop and READ. Each now carries a mark, so the state is a shape before it is
// a word and a word before it is a colour: --ascii and a monochrome terminal
// lose nothing, which is the only reason a glyph may be added at all.
//
// Every mark except the idle one is a meaning this room already owns — the
// spinner, the trace's own tick and cross, the warning that opens a note. What
// this test pins is that no two states arrive at the same rendering by any
// route.
func TestPhasesRenderAsDistinctMarks(t *testing.T) {
	g := UnicodeGlyphs()
	seat := func(p Phase, avail Availability) State {
		st := room()
		st.Turn = 1
		st.Seats = Seats{All: true} // an unseated column must still be drawn
		st.Columns[0].Phase = p
		st.Columns[0].Avail = avail
		return st
	}

	seen := map[string]string{}
	for _, tc := range []struct {
		name string
		st   State
		mark string
		word string
	}{
		{"idle", seat(PhaseIdle, AvailInstalled), g.Idle, "idle"},
		{"waiting", seat(PhaseWaiting, AvailInstalled), g.Spinner[0], "waiting"},
		{"streaming", seat(PhaseStreaming, AvailInstalled), g.Spinner[0], "streaming"},
		{"done", seat(PhaseDone, AvailInstalled), g.ActOK, "done"},
		{"failed", seat(PhaseFailed, AvailInstalled), g.ActFail, "failed"},
		{"cancelled", seat(PhaseCancelled, AvailInstalled), g.Warn, "cancelled"},
		{"unavailable", seat(PhaseIdle, AvailNotInstalled), g.Warn, "unavailable"},
	} {
		got := headerRow(tc.st, g)
		if !strings.Contains(got, tc.mark+" "+tc.word) {
			t.Errorf("%s: header %q does not carry %q", tc.name, got, tc.mark+" "+tc.word)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s renders identically to %s: %q", tc.name, prev, got)
		}
		seen[got] = tc.name
	}

	// The word is what carries the two states that share a mark, and it is never
	// traded away: the warning opens both "cancelled" and "unavailable" because
	// both mean "this did not complete normally", and only the word says which.
	if g.Warn == g.ActOK || g.Warn == g.ActFail || g.Idle == g.ActOK || g.Idle == g.ActFail {
		t.Error("two phase marks that mean different things collide")
	}
}

// TestPhaseMarksSurviveASCII. Every distinction in this product has to survive
// --ascii, and the ASCII budget here is unusually tight — glyphs.go argues at
// length that a character already spoken for is not a mark. Exactly one new
// character was needed, and it is the HUD's own ASCII form for the dot it
// borrows (design.md §7.5).
func TestPhaseMarksSurviveASCII(t *testing.T) {
	a := ASCIIGlyphs()
	g := GlyphsFor(true)
	for _, tc := range []struct {
		phase Phase
		word  string
		mark  string
	}{
		{PhaseIdle, "idle", a.Idle},
		{PhaseDone, "done", a.ActOK},
		{PhaseFailed, "failed", a.ActFail},
		{PhaseCancelled, "cancelled", a.Warn},
	} {
		st := room()
		st.Turn, st.ASCII = 1, true
		st.Columns[0].Phase = tc.phase
		if got := headerRow(st, g); !strings.Contains(got, tc.mark+" "+tc.word) {
			t.Errorf("ascii mode lost the %s mark: %q", tc.word, got)
		}
	}

	// The one new character must not already mean something else here.
	for name, taken := range map[string]string{
		"act": a.Act, "ellipsis": a.Ellipsis, "focus": a.Focus, "warn": a.Warn,
		"sep": a.Sep, "rule": a.Rule, "prompt": a.Prompt, "caret": a.Caret,
		"up": a.Up, "down": a.Down, "ok": a.ActOK, "fail": a.ActFail,
	} {
		if a.Idle == taken {
			t.Errorf("the ascii idle mark %q is already the %s glyph", a.Idle, name)
		}
	}

	// The ascii spinner's first frame is "-" and so is the rule, which is the
	// whole reason the column header keeps two cells of air around its leader.
	// One cell rendered "------------ - streaming", where the mark that says
	// what the seat is doing vanished into the rule pointing at it.
	st := room()
	st.Turn, st.ASCII = 1, true
	st.Columns[0].Phase = PhaseStreaming
	if got := headerRow(st, g); strings.Contains(got, a.Rule+" "+a.Spinner[0]+" ") {
		t.Errorf("the ascii spinner is flush against the leader rule: %q", got)
	}
}

// TestTheSeatNameAndItsStateAreOneLine. The header used to be a name at the far
// left and a bare word at the far right with twenty-five dead cells between,
// which reads as two unrelated labels rather than as a seat with a state. The
// rule between them is this room's existing grammar for "a label and the
// numbers that belong to it" — the same shape turnRule draws for every turn in
// the transcript underneath, so a reader learns one line form instead of two.
func TestTheSeatNameAndItsStateAreOneLine(t *testing.T) {
	g := UnicodeGlyphs()
	got := headerRow(room(), g)
	if !strings.Contains(got, "Claude Code  "+g.Rule) {
		t.Errorf("the seat name is not bound to its state by a rule: %q", got)
	}
	if !strings.Contains(got, g.Rule+"  "+g.Idle+" idle") {
		t.Errorf("the state does not close the header's rule: %q", got)
	}

	// The transcript separator is the same shape, which is the point of using it.
	if !strings.Contains(turnRule(2, "8s", 40, g), "turn 2  "+g.Rule) {
		t.Error("the turn separator and the column header no longer share a grammar")
	}

	// A column too narrow for a rule keeps the STATE and truncates the name: a
	// clipped seat name is still recognisable and a clipped state word is not.
	st := room()
	narrow := columnHeader(st, st.Columns[0], seatFocused, 18, PlainStyles(), g)
	if !strings.Contains(narrow, "idle") {
		t.Errorf("a narrow header dropped the state rather than the name: %q", narrow)
	}
	if w := lipgloss.Width(narrow); w > 18 {
		t.Errorf("a narrow header is %d cells, want at most 18: %q", w, narrow)
	}
}

// TestTheFocusedSeatIsWeightedApartFromItsNeighbours.
//
// §9.11 gave every seat name full weight, correctly — a name is the anchor a
// reader scans for. The cost was invisible until the room was driven: with all
// four names at the loudest level the surface has, the only thing separating the
// column the keys move from the three they do not was a single `▸`, and it was
// reported as not being there at all.
//
// Weight now says which seat has the keys. Nothing else changes: the marker
// still carries the whole distinction on its own, which is why PlainStyles
// renders the two headers identically and every layout golden is blind to this.
func TestTheFocusedSeatIsWeightedApartFromItsNeighbours(t *testing.T) {
	sty, g := NewStyles(true), UnicodeGlyphs()
	st := room()

	focused := columnHeader(st, st.Columns[0], seatFocused, 37, sty, g)
	unfocused := columnHeader(st, st.Columns[0], seatUnfocused, 37, sty, g)
	if focused == unfocused {
		t.Error("a focused seat header renders exactly like an unfocused one")
	}
	if !strings.Contains(focused, sty.Strong.Render(g.Focus+" Claude Code")) {
		t.Errorf("the focused seat name is not at full weight: %q", focused)
	}
	if !strings.Contains(unfocused, sty.Identity.Render("  Claude Code")) {
		t.Errorf("an unfocused seat name lost its identity hue rather than its weight: %q", unfocused)
	}

	// The tabbed and expanded tiers address a column the tab bar has already
	// marked. It keeps the weight, because the weight answers "do the keys move
	// this" and the marker answers "is this selected" — the conflation of those
	// two is what seatFocus exists to undo.
	if addressed := columnHeader(st, st.Columns[0], seatAddressed, 37, sty, g); addressed == unfocused {
		t.Error("the addressed column in the tabbed tier is drawn as if the keys were elsewhere")
	}

	// The identity set is a true no-op for all three, which is the property that
	// makes weight safe to spend here at all.
	plain := PlainStyles()
	a := columnHeader(st, st.Columns[0], seatUnfocused, 37, plain, g)
	b := columnHeader(st, st.Columns[0], seatAddressed, 37, plain, g)
	if a != b {
		t.Errorf("the identity style set distinguishes the two:\n %q\n %q", a, b)
	}
}

// TestTheChromeIsMeasuredRatherThanCounted. MaxScroll used to subtract a literal
// 3 for the header, the badge line and the rule — which was already wrong for a
// column with no badges at all, and would have gone wrong again the moment the
// chrome changed shape. Both callers now draw the same function.
func TestTheChromeIsMeasuredRatherThanCounted(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Body = longBody(80)
	st.Columns[0].Follow = false

	lay := layoutFor(st, GlyphsFor(false))
	chrome := columnChrome(st, st.Columns[0], seatFocused, lay.ColWidth+lay.extraFor(0),
		PlainStyles(), GlyphsFor(false))
	body := columnText(st, st.Columns[0], lay.ColWidth+lay.extraFor(0),
		PlainStyles(), GlyphsFor(false))
	if want := len(body) - (lay.Body - len(chrome)); MaxScroll(st, 0) != want {
		t.Errorf("MaxScroll = %d, want %d — the scroll model and the renderer disagree about the chrome",
			MaxScroll(st, 0), want)
	}

	// Scrolled all the way down, the last line of the reply is on screen and
	// nothing claims there is more below it.
	st.Columns[0].Scroll = MaxScroll(st, 0)
	got := render(st)
	if !strings.Contains(got, "line 80 of the reply") {
		t.Error("the maximum scroll offset does not reach the end of the reply")
	}
	if strings.Contains(got, "more below") {
		t.Error("a column scrolled to its maximum still claims content below it")
	}
}

// TestACardsBodyHangsUnderItsTitle. The degraded card was a warning line, a
// blank, a reason paragraph at the same indent, a blank and a closing sentence
// at the same indent — three fragments floating in a column, with nothing on
// screen saying the reason belonged to the title above it.
func TestACardsBodyHangsUnderItsTitle(t *testing.T) {
	g := UnicodeGlyphs()
	c := Column{
		Label: "Codex", Avail: AvailNotInstalled,
		Note: "not found on PATH, and nothing else on this machine answers to that name either",
	}
	lines := unavailableCard(c, 30, PlainStyles(), g)
	if !strings.HasPrefix(lines[0], g.Warn+" Codex is not seated") {
		t.Fatalf("the card does not open with its title: %q", lines[0])
	}
	for i, l := range lines[1:] {
		if l == "" {
			continue
		}
		if !strings.HasPrefix(l, "  ") {
			t.Errorf("card line %d does not hang under the title: %q", i+1, l)
		}
	}

	// The same grammar on a note, whose second line used to start hard against
	// the column edge and read as a new statement rather than as a continuation.
	n := noteCard("exit 1: not signed in, and the login command is the one named in the card", "", false, 30, PlainStyles(), g)
	if len(n) < 2 {
		t.Fatal("the note under test did not wrap")
	}
	if !strings.HasPrefix(n[1], "  ") {
		t.Errorf("a wrapped note does not hang under its mark: %q", n[1])
	}
	// The mark still opens it: the glyph carries the fact before the colour does.
	if !strings.HasPrefix(n[0], g.Warn+" ") {
		t.Errorf("the note lost its mark: %q", n[0])
	}
}

// TestAWriteCapableBadgeDoesNotRenderLikeAReadOnlyOne.
//
// §9.2 makes the badge a safety claim, and the room then drew every one of them
// at the same faint volume — so "unsandboxed" whispered exactly as loudly as
// "ro:tools" beside it. Colour stays redundant: the words are what carry the
// distinction, which is why the plain set below still says the same thing.
func TestAWriteCapableBadgeDoesNotRenderLikeAReadOnlyOne(t *testing.T) {
	sty := NewStyles(true)
	for _, l := range []SandboxLevel{SandboxWrite, SandboxNone} {
		if sty.ForSandbox(l).Render("x") == sty.ForSandbox(SandboxTools).Render("x") {
			t.Errorf("level %v renders like a read-only posture", l)
		}
		if sty.ForSandbox(l).Render("x") != sty.Alert.Render("x") {
			t.Errorf("level %v is not rendered as a claim that has to be findable", l)
		}
	}
	// A gated seat may do everything a writing one may; what differs is that it
	// has to be told yes first, so it takes weight without taking the severity.
	if sty.ForSandbox(SandboxGated).Render("x") == sty.Alert.Render("x") {
		t.Error("a gated seat is coloured like an ungated one")
	}
	for _, l := range []SandboxLevel{SandboxTools, SandboxEnforced, SandboxRequested} {
		if sty.ForSandbox(l).Render("x") != sty.Muted.Render("x") {
			t.Errorf("level %v is not chrome; only the badges that can change your files are loud", l)
		}
	}

	// With styling off every badge is still exactly its word. That is what makes
	// the weight safe to add: nothing above is the sole carrier of anything, so
	// NO_COLOR and --ascii are untouched.
	plain := PlainStyles()
	for _, l := range []SandboxLevel{SandboxWrite, SandboxNone, SandboxGated, SandboxTools} {
		if got := plain.ForSandbox(l).Render("badge"); got != "badge" {
			t.Errorf("the identity style set is not a no-op for level %v: %q", l, got)
		}
	}
}

// TestTheBadgeRowRightAnchorsTheCostWithoutDroppingTheClaim. The posture is this
// line's reason for existing and the cost is a number; a number gives way to a
// claim, never the other way round.
func TestTheBadgeRowRightAnchorsTheCostWithoutDroppingTheClaim(t *testing.T) {
	cost := 0.0123
	c := room().Columns[2] // unsandboxed, final only
	c.CostUSD = &cost

	wide := badgeRow(c, 60, PlainStyles(), UnicodeGlyphs())
	if !strings.HasPrefix(wide, "  unsandboxed  final only") {
		t.Errorf("the badge row does not lead with the posture claim: %q", wide)
	}
	if !strings.HasSuffix(wide, "$0.0123") {
		t.Errorf("the cost is not right-anchored: %q", wide)
	}

	// Too narrow to anchor: the claim stays and the number tucks in behind it.
	// The cell's own fit is what clips — never this function dropping a badge.
	if tight := badgeRow(c, 28, PlainStyles(), UnicodeGlyphs()); !strings.Contains(tight, "unsandboxed") {
		t.Errorf("a narrow badge row dropped the posture claim: %q", tight)
	}
}

// TestTheModeLineDropsKeysThatDoNothing. `tab` moves focus between columns and
// `f` expands one column to the width the only column already has, so in a room
// with a single seat on screen both do nothing at all — and a mode line that
// promises a key which does nothing is the same failure as one that hides a key
// that does (design.md §7.8), pointing the other way.
func TestTheModeLineDropsKeysThatDoNothing(t *testing.T) {
	one := render(deadSeats()) // two seats folded out, one column drawn
	for _, k := range []string{"tab focus", "f expand"} {
		if strings.Contains(one, k) {
			t.Errorf("a one-seat room advertises %q, which does nothing there", k)
		}
	}
	for _, k := range []string{"i compose", "? help", "q quit", "scroll"} {
		if !strings.Contains(one, k) {
			t.Errorf("a one-seat room dropped %q, which still works", k)
		}
	}

	several := render(room())
	for _, k := range []string{"tab focus", "f expand"} {
		if !strings.Contains(several, k) {
			t.Errorf("a room with several seats stopped naming %q", k)
		}
	}
}

// TestTheCollapsedNoticeSaysWhenItIsClipped. Everywhere else in this room a
// truncated string says it was truncated. The notice was handed to fit, which
// cuts silently, and at 120 columns on the reference machine it lost the last
// word of its own remedy and looked like a sentence that simply stopped.
func TestTheCollapsedNoticeSaysWhenItIsClipped(t *testing.T) {
	g := UnicodeGlyphs()
	st := deadSeats()
	line := strings.Split(render(st), "\n")[2]
	if strings.Contains(line, "seats them anyway") {
		return // it fit, and there is nothing to say
	}
	if !strings.Contains(line, g.Ellipsis) {
		t.Errorf("the notice was clipped without saying so:\n got:  %q\n full: %q",
			line, collapsedNotice(st, g))
	}
}

// TestTheModeLineKeysAreNotDrawnLikeTheirLabels is the style half of the footer
// change. Six items of identical weight is a wall the eye slides off, which is
// how a room with working scroll keys got reported as having none of them.
func TestTheModeLineKeysAreNotDrawnLikeTheirLabels(t *testing.T) {
	sty := NewStyles(true)
	g := UnicodeGlyphs()
	styled, plain := hints(sty, g, []hint{{key: "q", label: "quit"}})
	if styled == plain {
		t.Error("the key and its label render at the same weight")
	}
	if !strings.Contains(styled, sty.Text.Render("q")) {
		t.Error("the key is not at full intensity")
	}
	if !strings.Contains(styled, sty.Muted.Render("quit")) {
		t.Error("the label does not recede")
	}
	// The plain copy is the exact text, which is what truncation cuts and what
	// every golden compares — so the styling above can never move a cell.
	if plain != "q quit" {
		t.Errorf("plain copy = %q, want %q", plain, "q quit")
	}
	if got, _ := hints(PlainStyles(), g, []hint{{key: "q", label: "quit"}}); got != plain {
		t.Errorf("the identity style set does not reproduce the plain copy: %q", got)
	}
}

// TestTheBriefAndTheReplyAreSeparated. A question and the answer to it used to
// arrive as consecutive lines at the same indent, told apart only by a glyph at
// the start of one of them — a distinction you have to read, on a surface whose
// whole purpose is comparing four answers at a glance.
//
// The row it costs was taken from BETWEEN the turns, where a labelled full-width
// rule was already doing the separating, so the transcript is no taller than it
// was.
func TestTheBriefAndTheReplyAreSeparated(t *testing.T) {
	g := UnicodeGlyphs()
	lines := turnHead(2, "8s", "what does that cost by turn five?", false, 40, PlainStyles(), g)
	if len(lines) < 3 {
		t.Fatalf("turn head is %d lines, want a rule, the brief and air", len(lines))
	}
	if !strings.HasPrefix(lines[1], g.Prompt+" ") {
		t.Errorf("the brief is not marked as the user's: %q", lines[1])
	}
	if strings.TrimSpace(lines[len(lines)-1]) != "" {
		t.Errorf("the brief is not separated from the reply beneath it: %q", lines[len(lines)-1])
	}

	// A turn with no brief to echo spends no row on air.
	if got := turnHead(2, "", "", false, 40, PlainStyles(), g); len(got) != 1 {
		t.Errorf("a turn with no echo is %d lines, want just its separator", len(got))
	}
}
