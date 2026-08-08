package council

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// pageBrief is the turn-3 question every fixture below asks. Held as a constant
// because the assertion that matters most about it is a COUNT: the grid echoes a
// brief once per column and a page must echo it exactly once (§9.22).
const pageBrief = "what does resume cost by turn five?"

// paged is a finished turn 3 with all three shapes a page has to draw at once: a
// seat that answered, a seat that failed, and a seat that sat the turn out and
// therefore must not appear at all.
func paged() State {
	st := room()
	st.Turn = 3

	c := &st.Columns[0]
	c.startTurn(1, "the first question", false)
	c.Body, c.Phase, c.Elapsed = "the first answer", PhaseDone, 2*time.Second
	c.startTurn(2, "an older question", false)
	c.Body, c.Phase, c.Elapsed = "an older answer from Claude Code", PhaseDone, 4*time.Second
	c.startTurn(3, pageBrief, false)
	c.Acts = []Act{{ID: "a1", Text: "Bash: go test ./...", Status: runner.ActOK}}
	c.Body, c.Phase, c.Elapsed = "About 30K redundant input tokens per vendor.", PhaseDone, 41*time.Second
	cost := 0.0123
	c.CostUSD = &cost

	x := &st.Columns[1]
	x.startTurn(3, pageBrief, false)
	x.Phase, x.Elapsed = PhaseFailed, 3*time.Second
	x.Note = "the vendor exited before answering"

	// Sat turn 3 out: it still holds turn 2's reply, which is exactly what must
	// not be filed under turn 3 (§9.15).
	a := &st.Columns[2]
	a.startTurn(2, "an older question", false)
	a.Body, a.Phase, a.Elapsed = "an older answer from Antigravity", PhaseDone, 6*time.Second
	a.Note, a.Skipped = "not addressed in turn 3", true

	st.Page = TurnView{Open: true, Turn: 3}
	return st
}

// pagedLive is the turn in flight: one seat streaming, one waiting with nothing
// yet, one waiting behind a trace it has already produced.
func pagedLive() State {
	st := room()
	st.Turn = 4
	start := time.Date(2026, 8, 7, 9, 0, 0, 0, time.UTC)
	st.Now = start.Add(12 * time.Second)

	for i := range st.Columns {
		c := &st.Columns[i]
		c.startTurn(4, pageBrief, false)
		c.Started = start
	}
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "Considering the tradeoffs."
	st.Columns[1].Phase, st.Columns[1].Gran = PhaseWaiting, GranFinalOnly
	st.Columns[2].Phase = PhaseWaiting
	st.Columns[2].Acts = []Act{{ID: "a1", Text: "Read: docs/design.md", Status: runner.ActOK}}

	st.Page = TurnView{Open: true, Turn: 4, Follow: true}
	return st
}

// TestTheTurnPageReadsOneTurnAcrossEverySeat is the feature.
//
// The grid answers "what did each seat say"; this answers "what happened in turn
// 3", which is the reading `Y` has been assembling into a clipboard since §9.15
// with no surface able to show it.
func TestTheTurnPageReadsOneTurnAcrossEverySeat(t *testing.T) {
	st := paged()
	got := render(st)
	golden(t, "turn-page", got)

	// The brief ONCE, under the composer's own mark. Four copies of the user's
	// own question is what a grid has to do (each seat's prompt is a fact about
	// that seat) and what a page must not.
	if n := strings.Count(got, pageBrief); n != 1 {
		t.Errorf("the brief appears %d times on the page, want exactly once", n)
	}
	if !strings.Contains(got, UnicodeGlyphs().Prompt+" "+pageBrief) {
		t.Errorf("the brief is not echoed under the prompt glyph:\n%s", got)
	}

	// Both seats that took the turn, each under its own labelled rule, with the
	// outcome that seat actually reported.
	for _, want := range []string{
		"Claude Code", "About 30K redundant input tokens per vendor.",
		"Bash: go test ./...", "Codex", "the vendor exited before answering",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the page dropped %q:\n%s", want, got)
		}
	}

	// THE SAT-OUT SEAT IS ABSENT. §9.15's rule, and the reason the page and the
	// yank take their participants from one call: a seat that sat turn 3 out
	// still holds turn 2's reply, and putting it under this heading would be the
	// room inventing a conversation on a surface built for comparing them.
	if strings.Contains(got, "Antigravity") {
		t.Errorf("a seat that sat the turn out has a heading on its page:\n%s", got)
	}
	if strings.Contains(got, "an older answer") {
		t.Errorf("the page reached back past the turn it is showing:\n%s", got)
	}
	// And nothing from the seat's own skip line either — a page is about the
	// turn, and "not addressed" is a fact about a column's transcript.
	if strings.Contains(got, "not addressed") {
		t.Errorf("the page reports an absence it has no room to explain:\n%s", got)
	}
}

// TestTheTurnRuleStatesWhereTheTurnWentAndWhatItCost. §9.21 retires the live
// route the instant the turn lands, so a page for a finished turn has to read
// its destination off what the seats recorded — which is the measurement that
// outlives the header's copy.
func TestTheTurnRuleStatesWhereTheTurnWentAndWhatItCost(t *testing.T) {
	got := render(paged())
	if !strings.Contains(got, "turn 3") {
		t.Errorf("the page's own rule does not name the turn:\n%s", got)
	}
	// Two of three seats took it, so the route names them rather than claiming
	// everyone — in Route.label()'s vocabulary, so what is shown is what would
	// have to be typed to reproduce it.
	if !strings.Contains(got, "→ claude, codex") {
		t.Errorf("the turn rule does not name the seats the turn reached:\n%s", got)
	}
	if strings.Contains(got, "→ everyone") {
		t.Errorf("a turn that reached two of three seats is billed as everyone:\n%s", got)
	}
	// The clock is the LONGEST seat's own measured elapsed: the turn is over when
	// its slowest seat lands. Never a sum (44s) and never a mean (22s), which are
	// numbers no clock in this room ever read.
	if !strings.Contains(got, "41s") {
		t.Errorf("the turn rule does not carry the wait:\n%s", got)
	}
	for _, invented := range []string{"44s", "22s"} {
		if strings.Contains(got, invented) {
			t.Errorf("the turn rule derived %q from the seats' clocks:\n%s", invented, got)
		}
	}

	// A turn every seat took is "everyone", which is what @all parses to — the
	// same word, from the same label(), rather than a second spelling.
	all := paged()
	all.Columns[2].startTurn(3, pageBrief, false)
	all.Columns[2].Body, all.Columns[2].Phase = "Native resume avoids it entirely.", PhaseDone
	all.Columns[2].Note, all.Columns[2].Skipped = "", false
	if got := render(all); !strings.Contains(got, "→ everyone") {
		t.Errorf("a turn that reached every seat is not called everyone:\n%s", got)
	}
}

// TestTheTurnPageRendersTheLiveTurn. The projection is not a history viewer: the
// turn in flight has a page too, streaming bodies fill it as events land, and a
// waiting seat keeps the §9.14 one-liner it has in the grid.
func TestTheTurnPageRendersTheLiveTurn(t *testing.T) {
	st := pagedLive()
	got := render(st)
	golden(t, "turn-page-live", got)

	for _, want := range []string{
		"streaming", "Considering the tradeoffs.",
		"waiting", "working — the reply arrives whole.",
		"working — the steps above are what it has done so far.",
		"Read: docs/design.md",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the live page dropped %q:\n%s", want, got)
		}
	}
	// The seat rules carry the running clock from State.Now, the same source the
	// column header reads — never a clock inside Render.
	if !strings.Contains(got, "12s") {
		t.Errorf("a running seat's rule has no clock:\n%s", got)
	}
	// And the vendor-internals vocabulary stays off the reading area here too,
	// which is the assertion §9.14 added to stop it creeping back.
	if strings.Contains(got, "incremental") {
		t.Errorf("the live page is explaining council's plumbing:\n%s", got)
	}
}

// TestTheTurnPageSurvivesASCII. Every distinction this room makes is carried by a
// word or a glyph first, so --ascii and NO_COLOR must read identically (§9.11).
func TestTheTurnPageSurvivesASCII(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   State
		want []string
	}{
		{"turn-page-ascii", paged(), []string{
			"turn 3", "→ claude, codex", "41s", pageBrief,
			"Claude Code", "Codex", "the vendor exited before answering",
		}},
		{"turn-page-live-ascii", pagedLive(), []string{
			"turn 4", "→ everyone", "streaming", "waiting",
			"working — the reply arrives whole.",
		}},
	} {
		st := tc.st
		st.ASCII = true
		got := Render(st, PlainStyles(), GlyphsFor(true))
		golden(t, tc.name, got)
		for _, want := range tc.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s: --ascii dropped %q\n%s", tc.name, want, got)
			}
		}
		if strings.Contains(got, "Antigravity") && tc.name == "turn-page-ascii" {
			t.Errorf("%s: the sat-out seat appears under --ascii\n%s", tc.name, got)
		}
	}
}

// TestTheTurnPageHopsDoNotWrap is §9.20's asymmetry, kept: `[` at the first turn
// does nothing because there is no turn 0, and `]` past the newest restores that
// page's tail rather than jumping a whole conversation.
func TestTheTurnPageHopsDoNotWrap(t *testing.T) {
	m := &Model{st: paged(), glyphs: GlyphsFor(false)}
	turns := m.st.PageTurns()
	if len(turns) != 3 {
		t.Fatalf("fixture holds turns %v, want three", turns)
	}

	m.viewKey(key("["))
	if got := m.st.Page.Turn; got != 2 {
		t.Fatalf("[ landed on turn %d, want the turn before", got)
	}
	m.viewKey(key("["))
	if got := m.st.Page.Turn; got != 1 {
		t.Fatalf("the second [ landed on turn %d, want turn 1", got)
	}
	m.viewKey(key("["))
	if got := m.st.Page.Turn; got != 1 {
		t.Errorf("[ at the first turn moved to %d, want it to do nothing", got)
	}

	m.viewKey(key("]"))
	m.viewKey(key("]"))
	if got := m.st.Page.Turn; got != 3 {
		t.Fatalf("] walked to turn %d, want the newest at 3", got)
	}
	m.viewKey(key("]"))
	if got := m.st.Page.Turn; got != 3 {
		t.Errorf("] past the newest turn wrapped to %d", got)
	}
	if !m.st.Page.Follow {
		t.Error("] past the newest turn did not restore the page's tail")
	}

	// g and G are the same two positions in the projection's own unit.
	m.viewKey(key("g"))
	if got := m.st.Page.Turn; got != 1 {
		t.Errorf("g landed on turn %d, want the first turn still in memory", got)
	}
	m.viewKey(key("G"))
	if got := m.st.Page.Turn; got != 3 {
		t.Errorf("G landed on turn %d, want the newest", got)
	}
}

// TestTheTurnPageOpensOnTheTurnTheGridWasShowing. `t` turns the transcript
// ninety degrees; it does not navigate. A key that also moved the subject would
// make the two views two places rather than one transcript read two ways.
func TestTheTurnPageOpensOnTheTurnTheGridWasShowing(t *testing.T) {
	st := paged()
	st.Page = TurnView{}
	m := &Model{st: st, glyphs: GlyphsFor(false)}

	m.viewKey(key("t"))
	if !m.st.Page.Open {
		t.Fatal("t did not open the by-turn page")
	}
	if got := m.st.Page.Turn; got != 3 {
		t.Errorf("t opened turn %d, want the newest at 3", got)
	}
	m.viewKey(key("t"))
	if m.st.Page.Open {
		t.Error("t did not return the grid")
	}

	// A room with nothing behind it is told so rather than handed a blank page.
	empty := &Model{st: room(), glyphs: GlyphsFor(false)}
	empty.viewKey(key("t"))
	if empty.st.Page.Open {
		t.Error("t opened a page for a room with no turns")
	}
	if !strings.Contains(empty.st.Notice, "no turn has been taken yet") {
		t.Errorf("Notice = %q, want it to say why nothing opened", empty.st.Notice)
	}

	// And in compose it is the letter t, the same rule that keeps q the letter q
	// there (§9.10).
	typing := &Model{st: paged()}
	typing.st.Page = TurnView{}
	typing.st.Mode = ModeComposing
	typing.composeKey(key("t"))
	if typing.st.Page.Open {
		t.Error("t opened the page while a brief was being typed")
	}
	if typing.st.Draft != "t" {
		t.Errorf("Draft = %q, want the letter t", typing.st.Draft)
	}
}

// TestANewTurnDoesNotMoveTheOpenPage is §7.1 rule 4 on this surface: content
// must not jump out from under a reader because a vendor did something. The
// drift is carried by the mode word instead, which is where a reader already
// looks to learn what the keys mean.
func TestANewTurnDoesNotMoveTheOpenPage(t *testing.T) {
	st := paged()
	st.Page.Turn = 2

	before := render(st)
	if !strings.Contains(before, "TURN 2/3") {
		t.Fatalf("the mode word does not state the page and the newest turn:\n%s", lastLine(before))
	}

	// Turn 4 arrives on every seat while the reader is on turn 2's page.
	st.Turn = 4
	for i := range st.Columns {
		c := &st.Columns[i]
		c.startTurn(4, "a question nobody on this page asked", false)
		c.Body, c.Phase = "a brand new answer", PhaseStreaming
	}

	after := render(st)
	if !strings.Contains(after, "an older question") {
		t.Errorf("the open page moved when a new turn arrived:\n%s", after)
	}
	if strings.Contains(after, "a brand new answer") {
		t.Errorf("the newest turn's output landed on an older turn's page:\n%s", after)
	}
	if !strings.Contains(after, "TURN 2/4") {
		t.Errorf("the mode word does not carry the drift:\n%s", lastLine(after))
	}
}

// TestAGateOutranksTheTurnPage is §9.15's collision, asserted in the second
// projection.
//
// `y` approves a tool call a vendor is blocked on, and on a page it is also the
// copy key. If yank ever won that race, a keystroke the user believes approved a
// write would quietly copy text while the vendor sat there — and their next move
// is to press it again.
func TestAGateOutranksTheTurnPage(t *testing.T) {
	m := &Model{st: paged(), gateInputs: map[string]map[string]any{}}
	m.st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Write: internal/council/turnview.go",
	}}

	// The card renders ON the page, and it names the seat — the grid's card never
	// had to, because there the card's position is the seat.
	got := render(m.st)
	for _, want := range []string{"waiting on you:", "Claude Code — Write: internal/council/turnview.go",
		"y approve", "n deny", "GATE"} {
		if !strings.Contains(got, want) {
			t.Errorf("the page's gate card dropped %q:\n%s", want, got)
		}
	}

	_, cmd := m.key(key("y"))
	if len(m.st.Gates) != 0 {
		t.Fatal("y did not answer the pending gate — the approve key was stolen by the page's yank")
	}
	if cmd != nil {
		t.Error("y issued a clipboard write while a vendor was blocked on it")
	}
	if strings.Contains(m.st.Notice, "copied") {
		t.Errorf("the room reported a copy for a keystroke that approved a tool call: %q", m.st.Notice)
	}

	// Drained, the same key copies the page again: the precedence is a routing
	// rule, not a disabled feature.
	_, cmd = m.key(key("y"))
	if cmd == nil {
		t.Error("y stopped copying the page after the gate cleared")
	}
}

// TestTheTurnPageOffersNoFocusKey. There is one page and no columns to move
// between, so tab does nothing — and a mode line that promised it would be
// §7.8's surprise pointing the other way (§9.11's footer rule).
func TestTheTurnPageOffersNoFocusKey(t *testing.T) {
	m := &Model{st: paged(), glyphs: GlyphsFor(false)}
	m.st.Focus = 0

	m.viewKey(key("tab"))
	if m.st.Focus != 0 {
		t.Errorf("tab moved focus to %d while the by-turn page was open", m.st.Focus)
	}

	line := lastLine(render(m.st))
	for _, gone := range []string{"tab focus", "f expand"} {
		if strings.Contains(line, gone) {
			t.Errorf("the turn-view mode line promises %q, which does nothing there: %q", gone, line)
		}
	}
	for _, want := range []string{"t grid", "[ ] turn", "scroll", "y yank", "? help", "q quit"} {
		if !strings.Contains(line, want) {
			t.Errorf("the turn-view mode line does not name %q: %q", want, line)
		}
	}

	// The overflow markers make the same promise, so they carry the same keys —
	// and never `tab to focus`, which has nothing to point at here.
	tall := paged()
	tall.Columns[0].Body = longBody(60)
	tall.Page.Scroll, tall.Page.Follow = 5, false
	got := render(tall)
	if !strings.Contains(got, "more above") {
		t.Fatalf("the fixture does not overflow:\n%s", got)
	}
	if strings.Contains(got, "tab to focus") {
		t.Errorf("a page's overflow marker offers a focus key:\n%s", got)
	}
	if !strings.Contains(got, "scroll") {
		t.Errorf("a page's overflow marker names no key at all:\n%s", got)
	}
}

// TestYankOnTheTurnPageTakesThePage. The page IS `Y`'s document (§9.15), so both
// keys produce it: a per-seat `y` would need a per-seat focus, and a projection
// whose unit is the turn deliberately has none.
func TestYankOnTheTurnPageTakesThePage(t *testing.T) {
	for _, k := range []string{"y", "Y"} {
		m := &Model{st: paged()}
		m.st.Focus = 1 // Codex, whose turn-3 reply is empty
		_, cmd := m.viewKey(key(k))
		if cmd == nil {
			t.Fatalf("%s produced no clipboard command on the page", k)
		}
		if !strings.Contains(m.st.Notice, "copied turn 3") {
			t.Errorf("%s: Notice = %q, want the page's own turn", k, m.st.Notice)
		}
		if !strings.Contains(m.st.Notice, "2 seats") {
			t.Errorf("%s: Notice = %q, want the seats that took the turn", k, m.st.Notice)
		}
	}

	// An OLDER page yanks that page, not the newest turn — the document follows
	// what is on screen, which is the whole reason the key is worth a footer cell
	// here and not in the grid.
	st := paged()
	st.Page.Turn = 2
	y := st.YankTurnN(st.Page.Turn)
	if !strings.Contains(y.Text, "an older answer from Claude Code") {
		t.Errorf("the page yank took a turn other than the one on screen:\n%s", y.Text)
	}
	if strings.Contains(y.Text, "30K redundant") {
		t.Errorf("the page yank swept in a later turn:\n%s", y.Text)
	}
	if !strings.Contains(y.Text, "# turn 2") || !strings.Contains(y.Text, "> an older question") {
		t.Errorf("the page yank lost the brief-at-top format:\n%s", y.Text)
	}

	// And an empty page still issues no command: writing "" through OSC 52 is the
	// documented way to CLEAR a clipboard (§9.15).
	m := &Model{st: room()}
	m.st.Page = TurnView{Open: true, Turn: 1}
	if cmd := m.yank(m.st.YankTurnN(1)); cmd != nil {
		t.Error("an empty page yank issued a clipboard write, which would CLEAR the clipboard")
	}
}

// TestDispatchingFromTheTurnPageLandsOnTheLiveTurn. §7.1 rule 4 forbids the view
// moving because a VENDOR did something; pressing enter is the user saying what
// they want to read next, and a page that answered it by staying on turn 2 would
// show an old conversation while spending quota on a new one.
func TestDispatchingFromTheTurnPageLandsOnTheLiveTurn(t *testing.T) {
	st := paged()
	st.Page.Turn = 1
	m := &Model{st: st}
	m.openPage(m.st.Turn)
	if got := m.st.Page.Turn; got != 3 {
		t.Fatalf("openPage landed on turn %d, want the live turn", got)
	}
	if !m.st.Page.Follow {
		t.Error("the live turn's page did not open following its tail")
	}
	if !m.st.Page.Open {
		t.Error("openPage closed the projection it was asked to move")
	}

	// A finished turn opens at its HEAD instead: it is a document whose top is
	// the brief that produced it, and nothing more is arriving at the bottom.
	m.openPage(2)
	if m.st.Page.Follow || m.st.Page.Scroll != 0 {
		t.Errorf("a finished turn's page opened at its tail (follow=%v scroll=%d)",
			m.st.Page.Follow, m.st.Page.Scroll)
	}
}

// TestPageTurnsNeverClaimsAnEvictedTurn. History is capped at fifty and drops the
// oldest first (§9.9), so a room deep into a session has turns it can no longer
// draw — and offering a page for one would be §9.19's invented absence with a
// whole document behind it.
func TestPageTurnsNeverClaimsAnEvictedTurn(t *testing.T) {
	st := room()
	st.Turn = 214
	c := &st.Columns[0]
	for i := 1; i <= 214; i++ {
		c.startTurn(i, "brief", false)
		c.Body, c.Phase = "an answer", PhaseDone
	}

	turns := st.PageTurns()
	if len(turns) != maxHistory+1 {
		t.Fatalf("PageTurns holds %d turns, want the %d capped records plus the live one",
			len(turns), maxHistory)
	}
	if turns[0] != 214-maxHistory {
		t.Errorf("the oldest page is turn %d, want %d — the cap evicted the rest",
			turns[0], 214-maxHistory)
	}
	if got := st.turnEntries(3); len(got) != 0 {
		t.Errorf("turn 3 has %d entries after eviction, want none", len(got))
	}

	// The page for an evicted turn says the record is gone rather than drawing an
	// empty turn: "nobody answered" and "the room no longer remembers" are
	// different facts (§4a.1).
	st.Page = TurnView{Open: true, Turn: 3}
	if got := render(st); !strings.Contains(got, "no longer in memory") {
		t.Errorf("an evicted page renders as an empty turn:\n%s", got)
	}
}

// TestTheHelpPanelNamesTurnViewAboveTheFold. helpBody clips at the body height
// and does not scroll, so a row past the fold is not a demoted row, it is no row
// — and a projection nobody can find is a projection that does not exist.
func TestTheHelpPanelNamesTurnViewAboveTheFold(t *testing.T) {
	lines := helpKeys(layoutFor(room(), GlyphsFor(false)), PlainStyles(), GlyphsFor(false))
	fold := -1
	for i, l := range lines {
		if strings.Contains(l, "? ") && strings.Contains(l, "next page") {
			fold = i
			break
		}
	}
	if fold < 0 {
		t.Fatal("the `?` line is gone — the panel has no documented way out")
	}
	above := strings.Join(lines[:fold+1], "\n")
	if !strings.Contains(above, "f / t") {
		t.Error("`t` is not named above the fold — the projection cannot be discovered in the UI")
	}
	if !strings.Contains(above, "turn view") {
		t.Error("the yank row does not say what y takes in turn view")
	}
}
