package council

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The committee's live band (design.md §9.30).
//
// The property under test everywhere here is a COUNT: how many times the user's
// own words are on screen. Once when a committee turn is live, once per column
// otherwise, and never zero — a band that suppressed the echoes without drawing
// itself would take the brief off the screen entirely, which is the failure mode
// worth more of this file than the feature is.

// brief is the one string every fixture below is asked, long enough to wrap in a
// 37-cell column and short enough to sit on one row of a 116-cell band.
const bandBrief = "@all does the room read better with one brief or four?"

// committee is a live turn addressed to every seat on screen — the shape that
// drew the brief once per column.
func committee() State {
	st := room()
	st.Turn = 4
	r := Route{}
	st.TurnRoute = &r
	for i := range st.Columns {
		c := &st.Columns[i]
		c.startTurn(4, bandBrief, false)
		c.Phase = PhaseStreaming
		c.Body = c.Label + " is answering."
	}
	return st
}

// countBrief is how many times the echoed brief appears in a frame. The prompt
// glyph is included so a mention of the words inside a reply could not be
// mistaken for an echo of them.
func countBrief(frame string, g Glyphs) int {
	return strings.Count(frame, g.Prompt+" "+bandBrief[:20])
}

// TestTheCommitteeHearsTheBriefOnce is the whole complaint in one number.
func TestTheCommitteeHearsTheBriefOnce(t *testing.T) {
	st := committee()
	g := UnicodeGlyphs()
	got := render(st)

	if n := countBrief(got, g); n != 1 {
		t.Errorf("a three-seat committee turn echoes the brief %d times, want 1:\n%s", n, got)
	}
	// Above the columns, not inside one: the band's row is full width, so it
	// carries no column separator.
	rows := strings.Split(got, "\n")
	band := -1
	for i, r := range rows {
		if strings.Contains(r, g.Prompt+" "+bandBrief[:20]) {
			band = i
			break
		}
	}
	if band < 0 {
		t.Fatalf("the brief is nowhere on screen:\n%s", got)
	}
	if strings.Contains(rows[band], g.Sep) {
		t.Errorf("the band row carries a column separator, so it is drawn inside the grid: %q", rows[band])
	}
	// A blank row under it, which is §9.11's boundary where the speaker changes.
	if strings.TrimSpace(rows[band+1]) != "" {
		t.Errorf("the band is not separated from the columns: %q", rows[band+1])
	}
	// Every column still says which turn its lines belong to. That separator is
	// the column's own statement and is not what the band replaced.
	if n := strings.Count(got, "turn 4  "); n < 3 {
		t.Errorf("only %d of 3 columns carry the live turn's separator:\n%s", n, got)
	}
}

// TestTheBandDoesNotAppearOnASingleColumnRoute.
//
// One seat addressed is one echo on screen, and there is nothing to hoist. The
// brief stays in the column that was asked it, beside the answer to it.
func TestTheBandDoesNotAppearOnASingleColumnRoute(t *testing.T) {
	st := committee()
	// Turn 4 reached Claude alone; the other two sat it out, which is the room
	// the default route produces on every ordinary turn.
	for i := 1; i < len(st.Columns); i++ {
		st.Columns[i] = room().Columns[i]
	}
	st.FrameOwners = []model.VendorID{model.VendorClaude}

	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Errorf("a one-seat route spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times on a one-seat route, want 1 (in its own column)", n)
	}
}

// TestTheBandDoesNotAppearInAOneSeatRoom. A room with one seat on screen is the
// tabs tier, where there is one column and therefore one echo.
func TestTheBandDoesNotAppearInAOneSeatRoom(t *testing.T) {
	st := committee()
	st.Seats = Seats{Only: []model.VendorID{model.VendorClaude}}
	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Errorf("a one-seat room spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times in a one-seat room, want 1", n)
	}
}

// TestTheBandDoesNotAppearAtTheTabsTier. Three seats, too narrow to sit side by
// side: one column is drawn at a time, so the brief is on screen once already.
func TestTheBandDoesNotAppearAtTheTabsTier(t *testing.T) {
	st := committee()
	st.Width = 80
	if lay := layoutFor(st, GlyphsFor(false)); lay.Tier != TierTabs {
		t.Fatalf("80 cells is tier %v, want tabs", lay.Tier)
	} else if lay.Band != 0 {
		t.Errorf("the tabs tier spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times at the tabs tier, want 1", n)
	}
}

// TestExpandingACommitteeTurnRestoresTheEcho. `f` is the tabs tier's own
// arithmetic, so the same rule reaches it — and the one column on screen has to
// keep saying what it was asked.
func TestExpandingACommitteeTurnRestoresTheEcho(t *testing.T) {
	st := committee()
	st.Expanded = true
	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Errorf("an expanded column spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times in an expanded column, want 1", n)
	}
}

// TestTheEchoesComeBackWhenTheTurnIsFiled is the retirement moment.
//
// The band describes the LIVE turn. Once the next brief is dispatched, turn 4 is
// a record in each seat's history — that seat's own conversation — and §9.9's
// per-column echo is the correct rendering of it again.
func TestTheEchoesComeBackWhenTheTurnIsFiled(t *testing.T) {
	st := committee()
	st.Turn = 5
	// Turn 5 goes to Claude alone, which files turn 4 on that column and leaves
	// the other two showing it as their last.
	st.Columns[0].startTurn(5, "and now the narrow one", false)
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "answering."
	st.Height = 60 // tall enough that no column's transcript is scrolled away

	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Errorf("a filed turn still spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 3 {
		t.Errorf("turn 4's brief appears %d times after it was filed, want 3 — "+
			"one per seat's own transcript (§9.9):\n%s", n, render(st))
	}
}

// TestASeatThatSatTheTurnOutKeepsItsOwnEcho.
//
// A column's Prompt block outlives its turn: a seat that answered turn 4 and sat
// out 5 is still showing turn 4's brief. That is that seat's conversation, and a
// band for turn 5 must not silence it.
func TestASeatThatSatTheTurnOutKeepsItsOwnEcho(t *testing.T) {
	st := committee()
	st.Turn = 5
	// Turn 5 reaches Claude and Codex; Antigravity keeps turn 4 on screen.
	for i := 0; i < 2; i++ {
		st.Columns[i].startTurn(5, "the second brief entirely", false)
		st.Columns[i].Phase = PhaseStreaming
	}
	st.Height = 60
	got := render(st)

	if lay := layoutFor(st, GlyphsFor(false)); lay.Band == 0 {
		t.Fatal("a two-seat live turn draws no band")
	}
	if n := strings.Count(got, "› the second brief entirely"); n != 1 {
		t.Errorf("turn 5's brief appears %d times, want 1 (the band)", n)
	}
	if n := countBrief(got, UnicodeGlyphs()); n != 3 {
		t.Errorf("turn 4's brief appears %d times, want 3 — two filed records and "+
			"the seat that has not moved on:\n%s", n, got)
	}
}

// TestTheBandStandsWhileAColumnIsScrolledBack.
//
// The band is a fact about the live TURN, not about any column's viewport. A
// reader who scrolled a seat back into history is still in the turn they
// dispatched, and the brief that produced it does not stop being on screen
// because they went looking for an older answer.
func TestTheBandStandsWhileAColumnIsScrolledBack(t *testing.T) {
	st := committee()
	for i := range st.Columns {
		c := &st.Columns[i]
		// Give every seat a history to scroll back into, then detach all of them.
		hist := c.History
		c.History = append([]TurnRecord{{N: 3, Prompt: "an older question",
			Body: strings.Repeat("an older answer. ", 20), Phase: PhaseDone,
			Elapsed: time.Second}}, hist...)
		c.Follow, c.Scroll = false, 0
	}
	if lay := layoutFor(st, GlyphsFor(false)); lay.Band == 0 {
		t.Error("the band retires when every addressed column is scrolled away — " +
			"it describes the live turn, not the viewport")
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times with every column detached, want 1", n)
	}
}

// TestTheQuotedNoticeIsSaidOnceUnderTheBand. What rode along with a rebuttal
// brief is a fact about the dispatch, so it is hoisted with the words it
// qualifies rather than left repeating per column.
func TestTheQuotedNoticeIsSaidOnceUnderTheBand(t *testing.T) {
	st := committee()
	for i := range st.Columns {
		st.Columns[i].Quoted = true
	}
	got := render(st)
	if n := strings.Count(got, "the other seats' last answers were quoted"); n != 1 {
		t.Errorf("the rebuttal notice appears %d times, want 1:\n%s", n, got)
	}
}

// TestALongBriefIsCutWithAMarkerThatSaysWhereTheRestIs.
//
// Silent clipping is the ambiguity §4a.1 forbids: a reader cannot tell a brief
// that ended from one that was cut. The marker states how much is missing AND
// where the whole thing is.
func TestALongBriefIsCutWithAMarkerThatSaysWhereTheRestIs(t *testing.T) {
	st := committee()
	long := strings.TrimSpace(strings.Repeat("a considered brief worth sending to three agents at once. ", 8))
	for i := range st.Columns {
		st.Columns[i].Prompt = long
	}
	lay := layoutFor(st, GlyphsFor(false))
	if lay.Band == 0 {
		t.Fatal("a long committee brief draws no band")
	}
	// Four rows of brief at most, plus the blank boundary. Nothing here may grow
	// with the length of what was typed.
	if lay.Band > maxBandBrief+1 {
		t.Errorf("the band is %d rows for a long brief, cap is %d", lay.Band, maxBandBrief+1)
	}

	got := render(st)
	if !strings.Contains(got, "the turn page has this brief whole") {
		t.Errorf("a truncated brief does not say it was truncated:\n%s", got)
	}
	if !strings.Contains(got, "t opens it") {
		t.Errorf("the marker does not say where the whole brief is:\n%s", got)
	}
	// And it really is there in full: the turn page renders the brief once,
	// uncapped, which is what the marker points at.
	page := st
	page.Page = TurnView{Open: true, Turn: 4, Follow: true}
	if !strings.Contains(strings.Join(strings.Fields(render(page)), " "),
		strings.Join(strings.Fields(long[len(long)-40:]), " ")) {
		t.Error("the turn the marker points at does not carry the end of the brief")
	}
}

// TestTheBandNamesNoKeyItCannotHonour. `t` is the letter t while composing, so
// the marker sheds the keystroke there and keeps the count — scrollHint's rule
// for `f` (§7.8).
func TestTheBandNamesNoKeyItCannotHonour(t *testing.T) {
	st := committee()
	long := strings.TrimSpace(strings.Repeat("a considered brief worth sending to three agents at once. ", 8))
	for i := range st.Columns {
		st.Columns[i].Prompt = long
	}
	st.Mode = ModeComposing
	got := render(st)
	if strings.Contains(got, "t opens it") {
		t.Errorf("the band advertises `t` while composing, where it types a letter:\n%s", got)
	}
	if !strings.Contains(got, "the turn page has this brief whole") {
		t.Errorf("compose mode lost the truncation marker entirely:\n%s", got)
	}
}

// TestTheBandYieldsWholeOnAShortTerminal.
//
// The fallback is a pure function of height, and it is all-or-nothing: below the
// floor the columns echo the brief themselves, which is the frame exactly as it
// was before the band existed. A band shedding a row instead would leave a cut
// brief above columns that no longer say what they were asked.
func TestTheBandYieldsWholeOnAShortTerminal(t *testing.T) {
	g := GlyphsFor(false)
	first := 0
	for h := MinHeight; h <= 30; h++ {
		st := committee()
		st.Height = h
		lay := layoutFor(st, g)
		frame := render(st)
		n := countBrief(frame, UnicodeGlyphs())

		switch {
		case lay.Band > 0 && n != 1:
			t.Errorf("h=%d: band up and the brief appears %d times, want 1", h, n)
		case lay.Band == 0 && first > 0:
			t.Errorf("h=%d: the band retired at a height taller than %d, where it stood",
				h, first)
		case lay.Band == 0:
			// The columns took the brief back. In a short window it may be
			// scrolled above the fold — that is the scrollback working, and the
			// marker says so — so the TRANSCRIPT is what is asserted here rather
			// than the visible frame.
			text := strings.Join(columnText(st, st.Columns[0], lay.widthAt(0), PlainStyles(), g), "\n")
			if !strings.Contains(text, UnicodeGlyphs().Prompt+" "+bandBrief[:20]) {
				t.Fatalf("h=%d: no band and no echo — the brief is nowhere at all:\n%s", h, frame)
			}
		}
		if lay.Band > 0 && first == 0 {
			first = h
		}
	}
	if first == 0 {
		t.Fatal("the band never appears at any height up to 30")
	}
	if first <= MinHeight {
		t.Errorf("the band survives the minimum height (%d), so nothing tests the fallback", first)
	}
}

// TestTheBandDoesNotDependOnTheDraft.
//
// A band that retired because the composer grew would be a layout jump on a
// keystroke, mid-turn, and it would jump back on backspace (§7.1 rule 4).
func TestTheBandDoesNotDependOnTheDraft(t *testing.T) {
	g := GlyphsFor(false)
	for _, h := range []int{16, 20, 24, 40} {
		bare := committee()
		bare.Height = h
		typing := committee()
		typing.Height, typing.Mode = h, ModeComposing
		typing.Draft = strings.TrimSpace(strings.Repeat("a six row draft. ", 60))

		if a, b := layoutFor(bare, g).Band, layoutFor(typing, g).Band; a != b {
			t.Errorf("h=%d: the band is %d rows with an empty composer and %d with a full one",
				h, a, b)
		}
	}
}

// TestTheBandDoesNotAppearOverATurnPage. The page already draws the brief once
// — that is half of what it is for (§9.22) — and a band above it would be a
// second copy on the one surface that cannot have one.
func TestTheBandDoesNotAppearOverATurnPage(t *testing.T) {
	st := committee()
	st.Page = TurnView{Open: true, Turn: 4, Follow: true}
	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Errorf("a turn page spends %d band rows", lay.Band)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times on a turn page, want 1", n)
	}
}

// TestTheBandDoesNotAppearOverTheHelpPanel. The panel replaces the column area,
// so a band above it would be chrome describing content that is not on screen.
func TestTheBandDoesNotAppearOverTheHelpPanel(t *testing.T) {
	st := committee()
	for _, page := range []HelpPage{HelpKeys, HelpPostures} {
		st.Help = page
		if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
			t.Errorf("help page %v spends %d band rows", page, lay.Band)
		}
	}
}

// TestTheBandSurvivesASCII. Every distinction this room makes is carried by a
// character before it is carried by a style, so the band has to read the same
// way on a console that cannot draw `›`.
func TestTheBandSurvivesASCII(t *testing.T) {
	st := committee()
	st.ASCII = true
	a := GlyphsFor(true)
	got := Render(st, PlainStyles(), a)
	if n := countBrief(got, a); n != 1 {
		t.Errorf("ascii mode draws the brief %d times, want 1:\n%s", n, got)
	}
	if strings.Contains(got, UnicodeGlyphs().Prompt) {
		t.Error("the band drew a unicode prompt glyph in ascii mode")
	}
}

// TestTheBandCoexistsWithNarrowedFrameOwners. A route that narrows the frame to
// two of three seats still addresses two, so the band applies — and the seat it
// left out keeps its strip.
func TestTheBandCoexistsWithNarrowedFrameOwners(t *testing.T) {
	st := committee()
	st.Columns[2] = room().Columns[2] // Antigravity sat this turn out
	st.FrameOwners = []model.VendorID{model.VendorClaude, model.VendorCodex}
	lay := layoutFor(st, GlyphsFor(false))
	if lay.Band == 0 {
		t.Fatal("a two-seat committee turn under a narrowed frame draws no band")
	}
	if len(lay.ColWidths) != 3 || lay.ColWidths[2] != stripColumn {
		t.Errorf("the unaddressed seat is not a strip: %v", lay.ColWidths)
	}
	if n := countBrief(render(st), UnicodeGlyphs()); n != 1 {
		t.Errorf("the brief appears %d times, want 1", n)
	}
}

// TestTheBandRowsMatchWhatIsDrawn.
//
// Layout.Band is what the row budget spent and what the columns read to decide
// whether to echo. If the renderer drew a different number of rows the frame
// would be one row over the terminal, which is the tear the frame matrix exists
// to catch — asserted here at the seam rather than only through its symptom.
func TestTheBandRowsMatchWhatIsDrawn(t *testing.T) {
	for _, quoted := range []bool{false, true} {
		for _, long := range []bool{false, true} {
			st := committee()
			for i := range st.Columns {
				st.Columns[i].Quoted = quoted
				if long {
					st.Columns[i].Prompt = strings.TrimSpace(
						strings.Repeat("a considered brief for three agents. ", 8))
				}
			}
			lay := layoutFor(st, GlyphsFor(false))
			drawn := len(bandLines(st, st.Width-2*framePad, PlainStyles(), GlyphsFor(false)))
			if lay.Band != drawn {
				t.Errorf("quoted=%v long=%v: the budget spent %d rows and the renderer drew %d",
					quoted, long, lay.Band, drawn)
			}
		}
	}
}

// TestTheCommitteeBandGoldens pins the two frames this feature has: the band up,
// and the short terminal where it yields and the columns take the brief back.
func TestTheCommitteeBandGoldens(t *testing.T) {
	golden(t, "committee-band", render(committee()))

	st := committee()
	st.Height = 14
	if lay := layoutFor(st, GlyphsFor(false)); lay.Band != 0 {
		t.Fatalf("the fallback golden is not the fallback: %d band rows at h=14", lay.Band)
	}
	golden(t, "committee-band-fallback", render(st))
}
