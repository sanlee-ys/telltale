package council

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

// talking is a room three turns into a conversation, with the seats having
// taken different numbers of them: Claude answered all three, Codex was
// addressed once and left out since.
func talking() State {
	st := room()
	st.Turn = 3

	c := &st.Columns[0]
	c.startTurn(1, "should council resume sessions or re-send the transcript?", false)
	c.Body = "Resume. Re-sending grows input quadratically."
	c.Phase, c.Elapsed = PhaseDone, 12*time.Second
	cost := 0.0123
	c.CostUSD = &cost

	c.startTurn(2, "what does that cost by turn five?", false)
	c.Body = "About 30K redundant input tokens per vendor."
	c.Acts = []Act{{ID: "toolu_a", Text: "Read: dispatch.go", Status: runner.ActOK}}
	c.Phase, c.Elapsed = PhaseDone, 8*time.Second

	c.startTurn(3, "@all is the scrollback worth the memory?", false)
	c.Body = "Fifty turns is cheap against forgetting."
	c.Phase = PhaseStreaming

	d := &st.Columns[1]
	d.startTurn(3, "@all is the scrollback worth the memory?", false)
	d.Phase = PhaseWaiting

	return st
}

// TestTheRoomRemembersTheTurnBefore is the whole complaint in one assertion.
// Dispatching turn N used to erase turn N-1 off the screen, and the user's own
// words were never on it at all.
func TestTheRoomRemembersTheTurnBefore(t *testing.T) {
	st := talking()
	st.Expanded = true // one column at full width, so nothing under test wraps
	got := render(st)

	for _, want := range []string{
		"turn 1", "turn 2", "turn 3",
		"should council resume sessions",   // the user's words, turn 1
		"Resume. Re-sending grows input",   // the vendor's, turn 1
		"what does that cost by turn five", // the user's words, turn 2
		"About 30K redundant input tokens", // the vendor's, turn 2
		"Fifty turns is cheap",             // the turn in flight
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the transcript is missing %q", want)
		}
	}
}

// TestTheEchoIsThePrincipalsWordsAndIsMarkedAsSuch. The prompt has to be
// legible as the user's rather than as something a vendor said, and the marker
// is a glyph before it is a colour so it survives --ascii.
func TestTheEchoIsThePrincipalsWordsAndIsMarkedAsSuch(t *testing.T) {
	st := talking()
	st.Expanded = true
	g := UnicodeGlyphs()
	if !strings.Contains(render(st), g.Prompt+" should council resume") {
		t.Error("the echoed brief does not carry the compose glyph")
	}

	st.ASCII = true
	a := ASCIIGlyphs()
	if !strings.Contains(Render(st, PlainStyles(), GlyphsFor(true)), a.Prompt+" should council resume") {
		t.Error("ascii mode lost the marker on the echoed brief")
	}
}

// TestTheEchoedBriefIsNotRedacted.
//
// Every other string that reaches a column came out of a vendor process and is
// redacted on the way in. This one is the user's own typing, echoed to the user
// on the user's own screen: hiding it would conceal a secret from the one person
// who already has it, do nothing about the copy just sent to four vendors, and
// make the echo disagree with what was dispatched — which is the single thing
// this line exists to show.
func TestTheEchoedBriefIsNotRedacted(t *testing.T) {
	st := room()
	st.Expanded = true
	st.Turn = 1
	c := &st.Columns[0]
	c.startTurn(1, "is sk-ant-abcdefghijklmnop0123 still in the env file?", false)
	c.Body = "Checked; it is not."
	c.Phase = PhaseDone

	got := render(st)
	if !strings.Contains(got, "sk-ant-abcdefghijklmnop0123") {
		t.Error("the room redacted the user's own brief back at the user")
	}
	if strings.Contains(got, redacted) {
		t.Error("the echoed brief carries a redaction marker")
	}
}

// TestAPastTurnCarriesItsOwnClockAndCost. The header and badge line are chrome
// describing the CURRENT turn, so a turn in the transcript that did not carry
// its own numbers would sit under someone else's.
func TestAPastTurnCarriesItsOwnClockAndCost(t *testing.T) {
	st := talking()
	st.Expanded = true
	got := render(st)
	if !strings.Contains(got, "12s") {
		t.Error("turn 1 lost the time it took")
	}
	if !strings.Contains(got, "$0.0123") {
		t.Error("turn 1 lost the cost it reported")
	}
	if !strings.Contains(got, "8s") {
		t.Error("turn 2 lost the time it took")
	}

	// A turn that ended badly says so on its own separator: the phase word is
	// chrome for the live turn only, and "done" would be noise on every other.
	st2 := room()
	st2.Expanded = true
	st2.Turn = 2
	c := &st2.Columns[0]
	c.startTurn(1, "run the suite", false)
	c.Phase, c.Note = PhaseFailed, "exit 1: not signed in"
	c.startTurn(2, "try again", false)
	c.Phase = PhaseStreaming
	got2 := render(st2)
	if !strings.Contains(got2, "failed") {
		t.Error("a past turn that failed does not say so")
	}
	if !strings.Contains(got2, "not signed in") {
		t.Error("a past turn lost the card that said why it failed")
	}
}

// TestASessionTotalKeepsItsWordInHistory: $0.1177 means one thing on a
// spawn-per-turn seat and another on a persistent one, and the word is the only
// thing keeping them apart. Losing it on the way into the transcript would turn
// a true running total into a false turn cost.
func TestASessionTotalKeepsItsWordInHistory(t *testing.T) {
	st := room()
	st.Expanded = true
	st.Turn = 2
	total := 0.1177
	c := &st.Columns[0]
	c.startTurn(1, "gm", false)
	c.Body, c.Phase, c.CostUSD, c.CostSession = "Morning.", PhaseDone, &total, true
	c.startTurn(2, "and again", false)
	c.Phase = PhaseStreaming

	if !strings.Contains(render(st), "$0.1177 session") {
		t.Error("a running total lost its label on the way into the transcript")
	}
}

// TestAnUnaddressedSeatRecordsNothing. Turn 2 went to Claude alone, so Codex's
// transcript must skip from 1 to 3 rather than hold an entry for a turn it
// never saw.
func TestAnUnaddressedSeatRecordsNothing(t *testing.T) {
	c := &Column{Vendor: model.VendorCodex, Avail: AvailInstalled}
	c.startTurn(1, "first", false)
	c.Body, c.Phase = "an answer", PhaseDone
	// Turn 2: not addressed. dispatch() sets a note and continues, and startTurn
	// is exactly what it does not reach.
	c.Note = "not addressed in turn 2"
	c.startTurn(3, "third", false)

	if len(c.History) != 1 {
		t.Fatalf("History = %d entries, want the one turn this seat took", len(c.History))
	}
	if c.History[0].N != 1 {
		t.Errorf("recorded turn %d, want turn 1", c.History[0].N)
	}
	if c.TurnN != 3 {
		t.Errorf("current turn is %d, want 3", c.TurnN)
	}
}

// TestHistoryIsCapped keeps a long room from growing without bound. The cap is
// generous on purpose — a room you can exhaust in an afternoon is a room that
// forgets again — but it is a cap.
func TestHistoryIsCapped(t *testing.T) {
	c := &Column{Vendor: model.VendorClaude, Avail: AvailInstalled}
	for i := 1; i <= maxHistory+20; i++ {
		c.startTurn(i, "brief", false)
		c.Body, c.Phase = "answer", PhaseDone
	}
	if len(c.History) != maxHistory {
		t.Fatalf("History = %d entries, want the %d cap", len(c.History), maxHistory)
	}
	// The OLDEST go. Dropping the newest would make the cap look like the
	// transcript had stopped recording.
	if c.History[len(c.History)-1].N != maxHistory+19 {
		t.Errorf("the newest retained turn is %d, want %d",
			c.History[len(c.History)-1].N, maxHistory+19)
	}
}

// TestATurnsTraceDoesNotBleedIntoTheNext. Act ids are scoped to a turn — agy's
// are bare step indices — so the new turn must not share the old turn's slice.
func TestATurnsTraceDoesNotBleedIntoTheNext(t *testing.T) {
	c := &Column{Vendor: model.VendorClaude, Avail: AvailInstalled}
	c.startTurn(1, "first", false)
	c.Acts = []Act{{ID: "step-3", Text: "tool", Status: runner.ActOK}}
	c.Body, c.Phase = "done", PhaseDone

	c.startTurn(2, "second", false)
	if len(c.Acts) != 0 {
		t.Fatalf("Acts = %+v, want the new turn to start empty", c.Acts)
	}
	c.Acts = append(c.Acts, Act{ID: "step-3", Text: "another tool"})
	if c.History[0].Acts[0].Text != "tool" {
		t.Errorf("the new turn's trace overwrote the old one in place: %q",
			c.History[0].Acts[0].Text)
	}
}

// TestScrollbackSpansTheWholeTranscript is what makes the memory usable. `g`
// has to reach the first thing this seat was ever asked, not the top of the
// current turn.
func TestScrollbackSpansTheWholeTranscript(t *testing.T) {
	st := talking()
	short := room()
	short.Turn = 3
	short.Columns[0].Body = "Fifty turns is cheap against forgetting."
	short.Columns[0].Phase = PhaseStreaming

	if MaxScroll(st, 0) <= MaxScroll(short, 0) {
		t.Fatal("the transcript did not extend the scrollable range")
	}

	// Scrolled to the very top: the first turn's brief is on screen and the
	// column says there is more below it.
	st.Columns[0].Follow = false
	st.Columns[0].Scroll = 0
	top := render(st)
	if !strings.Contains(top, "turn 1") {
		t.Error("scrolling to the top does not reach the first turn")
	}
	if !strings.Contains(top, "more below") {
		t.Error("a transcript scrolled to the top does not say there is more below")
	}
	golden(t, "transcript-top", top)

	// Following pins to the newest line, which is still the turn in flight.
	st.Columns[0].Follow = true
	if !strings.Contains(render(st), "Fifty turns is cheap") {
		t.Error("a following column is not showing the newest turn")
	}
}

// TestTheTranscriptDoesNotDescribeAPastTurnInThePresentTense. "working…" and
// "reports no incremental output" are claims about a turn in flight.
func TestTheTranscriptDoesNotDescribeAPastTurnInThePresentTense(t *testing.T) {
	c := &Column{Vendor: model.VendorClaude, Label: "Claude Code", Avail: AvailInstalled, Gran: GranFinalOnly}
	c.startTurn(1, "first", false)
	c.Body, c.Phase = "an answer", PhaseDone
	c.startTurn(2, "second", false)
	c.Phase = PhaseWaiting

	st := room()
	st.Turn = 2
	st.Expanded = true
	st.Columns[0] = *c

	got := render(st)
	if n := strings.Count(got, "no incremental output"); n != 1 {
		t.Errorf("the waiting card appears %d times, want only on the live turn", n)
	}
}

// TestATurnGoldens is the feature in one frame, in both glyph sets.
func TestTranscriptGoldens(t *testing.T) {
	golden(t, "transcript", render(talking()))

	st := talking()
	st.ASCII = true
	golden(t, "transcript-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestARebuttalTurnReportsTheQuotesRatherThanEchoingThem.
//
// On a rebuttal turn the seat really does receive the other seats' answers in
// front of the brief. Pasting those into the echo would put another vendor's
// words on the line marked as the principal's — the same category error as
// running a tool trace into prose — so what rides along is reported instead.
func TestARebuttalTurnReportsTheQuotesRatherThanEchoingThem(t *testing.T) {
	st := room()
	st.Expanded = true
	st.Turn = 2
	c := &st.Columns[0]
	c.startTurn(2, "given those answers, which is right?", true)
	c.Phase = PhaseStreaming

	got := render(st)
	if !strings.Contains(got, "given those answers") {
		t.Error("the brief itself is missing")
	}
	if !strings.Contains(got, "quoted to this one") {
		t.Error("a rebuttal turn does not say the other seats' answers rode along")
	}
}

// ---------------------------------------------------------------- composer

// TestTheComposerGrowsWithTheDraft: the body pays for it, and the frame stays
// exactly as tall as the terminal.
func TestTheComposerGrowsWithTheDraft(t *testing.T) {
	one := room()
	one.Mode = ModeComposing
	one.Draft = "short"

	many := room()
	many.Mode = ModeComposing
	many.Draft = "first line\nsecond line\nthird line\nfourth line"

	g := GlyphsFor(false)
	lo, hi := layoutFor(one, g), layoutFor(many, g)
	if hi.Prompt <= lo.Prompt {
		t.Fatalf("composer rows: %d for a paragraph, %d for a word", hi.Prompt, lo.Prompt)
	}
	if hi.Body >= lo.Body {
		t.Error("the composer grew without the body paying for it")
	}
	if hi.Prompt+hi.Body != lo.Prompt+lo.Body {
		t.Error("the frame gained or lost rows")
	}

	got := render(many)
	for _, want := range []string{"first line", "second line", "third line", "fourth line"} {
		if !strings.Contains(got, want) {
			t.Errorf("the composer is not showing %q", want)
		}
	}
	golden(t, "composer-multirow", got)
}

// TestTheComposerYieldsToTheFloor. At the minimum height a six-row draft would
// leave the columns nothing, and a room you can type in but not read is not the
// trade anyone asked for.
func TestTheComposerYieldsToTheFloor(t *testing.T) {
	for _, h := range []int{MinHeight, 11, 12, 16, 24, 40} {
		for _, w := range []int{60, 80, 120} {
			st := room()
			st.Width, st.Height = w, h
			st.Mode = ModeComposing
			st.Draft = strings.Repeat("a long brief that will certainly wrap ", 12)
			out := Render(st, PlainStyles(), GlyphsFor(false))
			if n := len(strings.Split(out, "\n")); n > h {
				t.Errorf("w=%d h=%d: frame is %d lines, terminal is %d", w, h, n, h)
			}
			if layoutFor(st, GlyphsFor(false)).Body < 1 {
				t.Errorf("w=%d h=%d: the composer ate the last body row", w, h)
			}
		}
	}
}

// TestAnOverlongDraftSaysWhatItIsHiding. The ceiling is six rows; past it the
// tail is what stays, because that is where the cursor is — and the row that
// says how much is above it is the same trade the column overflow markers make.
func TestAnOverlongDraftSaysWhatItIsHiding(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Draft = strings.Repeat("line\n", 20) + "the last line"

	got := render(st)
	if !strings.Contains(got, "more above") {
		t.Error("a draft taller than the compose area does not say it is clipped")
	}
	if !strings.Contains(got, "the last line") {
		t.Error("the tail of the draft, where the cursor is, is not on screen")
	}
	golden(t, "composer-overflow", got)
}

// TestCtrlJInsertsANewlineAndEnterStillDispatches is the keymap half. The
// distinction that matters is against a PASTED newline, which still dies: a
// paste is not a keystroke, and flattening it is what keeps a pasted log from
// tearing the footer apart.
func TestCtrlJInsertsANewlineAndEnterStillDispatches(t *testing.T) {
	m := &Model{st: State{Mode: ModeComposing, Width: 120, Height: 24}}

	m.composeKey(key("a"))
	m.composeKey(key("ctrl+j"))
	m.composeKey(key("b"))
	if m.st.Draft != "a\nb" {
		t.Fatalf("Draft = %q, want a deliberate newline between the letters", m.st.Draft)
	}

	// A paste arrives as text through the same path and is still flattened.
	m.st.Draft = ""
	m.composeKey(tea.KeyPressMsg{Code: 'x', Text: "one\ntwo"})
	if strings.Contains(m.st.Draft, "\n") {
		t.Errorf("Draft = %q, want a pasted newline flattened", m.st.Draft)
	}

	// backspace still walks off the end of a newline rather than sticking on it.
	m.st.Draft = "a\n"
	m.composeKey(key("backspace"))
	if m.st.Draft != "a" {
		t.Errorf("Draft = %q, want the newline deleted", m.st.Draft)
	}

	// enter is unchanged: it dispatches. With no seats it says so rather than
	// putting a newline in the draft.
	m.st.Draft = "go"
	m.composeKey(key("enter"))
	if strings.Contains(m.st.Draft, "\n") {
		t.Error("enter inserted a newline instead of dispatching")
	}
}

// TestADeliberateNewlineSurvivesIntoTheDispatchedBrief. sanitize keeps
// newlines — they are paragraphing — and every transport this repo drives is
// safe with them: Claude and Codex take the prompt on stdin, Claude's turn is
// JSON-marshalled so the newline is escaped, and agy takes it as one argv
// element on a native binary with no shell in the path (§9.3).
func TestADeliberateNewlineSurvivesIntoTheDispatchedBrief(t *testing.T) {
	draft := "compare these two:\n\n1. resume\n2. re-send"
	route, prompt := ParseRoute(draft)
	if len(route) != 1 || route[0] != model.VendorClaude {
		t.Fatalf("route = %v, want the default lane", route)
	}
	if prompt != draft {
		t.Errorf("prompt = %q, want the draft unchanged", prompt)
	}
	if got := sanitize(prompt); got != draft {
		t.Errorf("sanitize(%q) = %q; a deliberate newline was flattened", draft, got)
	}
}

// ------------------------------------------------------------------- seats

// TestDeadSeatsFoldOut is the third complaint: a seat that cannot be driven
// held a quarter of the width for the whole session to repeat one sentence.
func TestDeadSeatsFoldOut(t *testing.T) {
	st := deadSeats()
	got := render(st)

	if strings.Contains(got, "the other columns dispatch normally") {
		t.Error("a seat that cannot be driven is still holding a column")
	}
	// The one that CAN answer got the width. At 120 cells a single column is
	// the tabs tier, so the test is that its body is wider than a third.
	if lay := layoutFor(st, GlyphsFor(false)); lay.ColWidth < 100 {
		t.Errorf("the surviving seat got %d cells, want most of the terminal", lay.ColWidth)
	}
	golden(t, "collapsed-seats", got)
}

// TestTheCollapsedSeatSaysWhichFailure. Folding a seat away must not fold away
// what it was saying: absence and unusability are different facts with
// different fixes, and a seat nobody can see is one a user has no reason to go
// looking for (§4a.1).
func TestTheCollapsedSeatSaysWhichFailure(t *testing.T) {
	got := render(deadSeats())
	for _, want := range []string{"Codex", "not installed", "Antigravity", "installed but not drivable"} {
		if !strings.Contains(got, want) {
			t.Errorf("the collapsed-seat notice does not say %q", want)
		}
	}
	if !strings.Contains(got, "not on screen") {
		t.Error("the room does not say the seats are missing from the grid")
	}
}

// TestVendorAllKeepsEverySeat is the escape hatch: the full cards, on request.
func TestVendorAllKeepsEverySeat(t *testing.T) {
	st := deadSeats()
	st.Seats = Seats{All: true}
	got := render(st)
	if !strings.Contains(got, "not found on PATH") {
		t.Error("--vendor all did not bring the unavailable card back")
	}
	if strings.Contains(got, "not on screen") {
		t.Error("--vendor all still claims a seat was folded away")
	}
}

// TestVendorListSeatsExactlyThose — including one that is not installed. A user
// who named a seat is owed the card that says why it is not there; a seat they
// did not name is out of the room, so it is neither drawn nor dispatched to.
func TestVendorListSeatsExactlyThose(t *testing.T) {
	st := deadSeats()
	// Antigravity is installed here and simply not asked for, which is the case
	// this flag adds: a working seat the user decided is not in the room.
	st.Columns[2].Avail = AvailInstalled
	st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}}

	got := render(st)
	if !strings.Contains(got, "not found on PATH") {
		t.Error("a seat named on --vendor was not forced on screen")
	}
	if !strings.Contains(got, "Antigravity") {
		t.Error("the seat left out by --vendor is not named anywhere")
	}
	if !strings.Contains(got, "left out by --vendor") {
		t.Error("the notice does not say why the unnamed seat is missing")
	}
	// Claude is installed and named; Codex is named but absent; Antigravity is
	// installed and NOT named, so the room holds one seat that takes turns.
	if n := st.Seated(); n != 1 {
		t.Errorf("Seated() = %d, want only the seat that is both named and drivable", n)
	}
}

// TestEverySeatCollapsedStillDrawsTheCards. On a machine with nothing
// installed, the cards ARE the content — an empty grid would say less than the
// four columns it folded away.
func TestEverySeatCollapsedStillDrawsTheCards(t *testing.T) {
	st := room()
	for i := range st.Columns {
		st.Columns[i].Avail = AvailNotInstalled
		st.Columns[i].Note = "not found on PATH"
	}
	got := render(st)
	if !strings.Contains(got, "is not seated") {
		t.Error("a room with no drivable seat drew nothing at all")
	}
	if strings.Contains(got, "not on screen") {
		t.Error("the room claims it folded seats away and then drew them anyway")
	}
}

// TestFocusStartsAndStaysOnADrawnSeat. Focus is an index into Columns, and a
// collapsed seat holding it would leave the marker nowhere on screen while the
// scroll keys addressed a column nobody can see.
func TestFocusStartsAndStaysOnADrawnSeat(t *testing.T) {
	st := deadSeats()
	// Claude is index 0 and is the only drawn seat here, so widen the room:
	// two drawn seats out of three is what actually exercises the wrap.
	st.Columns[2].Avail = AvailInstalled
	m := &Model{st: st, glyphs: GlyphsFor(false)}

	seen := map[int]bool{}
	for i := 0; i < 4; i++ {
		m.focusBy(1)
		seen[m.st.Focus] = true
	}
	if seen[1] {
		t.Error("tab landed on a seat that is not on screen")
	}
	if !seen[0] || !seen[2] {
		t.Errorf("tab did not reach every drawn seat: %v", seen)
	}
	if MaxScroll(m.st, 1) != 0 {
		t.Error("a collapsed seat reports a scrollable window")
	}
}

func TestParseSeats(t *testing.T) {
	cases := []struct {
		in   string
		all  bool
		only []model.VendorID
		err  bool
	}{
		{in: "", all: false, only: nil},
		{in: "all", all: true},
		{in: "everyone", all: true},
		{in: "claude", only: []model.VendorID{model.VendorClaude}},
		// The @mention vocabulary, including its aliases and its tolerance for
		// the way people actually type a list.
		{in: "claude, antigravity", only: []model.VendorID{model.VendorClaude, model.VendorAntigravity}},
		{in: "agy,agy", only: []model.VendorID{model.VendorAntigravity}},
		// all is the wider request and wins over a list.
		{in: "claude,all", all: true, only: []model.VendorID{model.VendorClaude}},
		{in: "cluade", err: true},
	}
	for _, c := range cases {
		got, err := ParseSeats(c.in)
		if c.err {
			if err == nil {
				t.Errorf("ParseSeats(%q) = %+v, want an error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSeats(%q): %v", c.in, err)
			continue
		}
		if got.All != c.all || len(got.Only) != len(c.only) {
			t.Errorf("ParseSeats(%q) = %+v, want all=%v only=%v", c.in, got, c.all, c.only)
			continue
		}
		for i, v := range c.only {
			if got.Only[i] != v {
				t.Errorf("ParseSeats(%q).Only[%d] = %v, want %v", c.in, i, got.Only[i], v)
			}
		}
	}
}

// TestTheNewSurfacesNeverExceedTheTerminal re-runs the width sweep over the
// three states this change adds. Each of them assembles lines at render time —
// a separator with a right-hand meta cell, a prompt echo with a prefix, a
// notice naming several seats — which is exactly the shape that overflows a
// narrow column by a cell or two.
func TestTheNewSurfacesNeverExceedTheTerminal(t *testing.T) {
	states := map[string]func() State{
		"transcript": talking,
		"transcript-long": func() State {
			st := talking()
			st.Columns[0].History[0].Prompt = strings.Repeat("unbreakabletokenofdoom", 6)
			st.Columns[0].History[0].Body = strings.Repeat("a long streamed reply ", 20)
			st.Columns[0].Prompt = strings.Repeat("verylongtokenwithnobreaks", 4)
			return st
		},
		"composer": func() State {
			st := room()
			st.Mode = ModeComposing
			st.Draft = "one\n" + strings.Repeat("a wrapping brief ", 20) + "\n" + strings.Repeat("x", 300)
			return st
		},
		"collapsed": deadSeats,
		"collapsed-long": func() State {
			st := deadSeats()
			st.Columns[1].Label = strings.Repeat("Codex", 20)
			return st
		},
	}
	for name, mk := range states {
		for _, w := range []int{60, 72, 80, 95, 96, 120, 160, 201} {
			for _, h := range []int{10, 14, 24, 40} {
				for _, ascii := range []bool{false, true} {
					st := mk()
					st.Width, st.Height = w, h
					out := Render(st, PlainStyles(), GlyphsFor(ascii))
					for i, line := range strings.Split(out, "\n") {
						if got := lipgloss.Width(line); got > w {
							t.Errorf("%s w=%d h=%d ascii=%v: line %d is %d cells: %q",
								name, w, h, ascii, i, got, line)
						}
					}
					if n := len(strings.Split(out, "\n")); n > h {
						t.Errorf("%s w=%d h=%d ascii=%v: frame is %d lines", name, w, h, ascii, n)
					}
				}
			}
		}
	}
}
