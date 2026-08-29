package council

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// operatorFigure matches the operator's own clock in either spelling, and only
// when it carries a NUMBER.
//
// A bare `strings.Contains(got, "you ")` used to be enough. It stopped being
// enough when the state word became `needs you` (§9.45's amendment), because the
// word ends in the same two syllables the figure opens with — so a test asking
// "did an unmeasured span draw anything" would answer yes off the word alone. Both
// spellings operatorCell renders weld `you ` to a digit, which is the thing an
// unmeasured span must never produce.
var operatorFigure = regexp.MustCompile(`you \d`)

// gateClockRoom is one frame carrying all three states of the operator's clock
// (§9.45), because the three are only meaningful against each other:
//
//   - seat 1 is STOPPED right now, on the second card of its turn. The first
//     card cost 48s and was answered; the one on screen went up four minutes
//     ago. Five minutes of wall clock, twelve seconds of vendor.
//   - seat 2 took a turn that raised NO card. It draws no operator figure at
//     all — absent, never `0s`.
//   - seat 3 took a turn whose one card was answered inside a second. It draws
//     `you 0s` — a measured zero, which is a different claim from seat 2's
//     blank and must not render alike (§4a.1).
func gateClockRoom() State {
	st := room()
	st.Turn = 1
	st.Now = time.Date(2026, 8, 15, 12, 5, 0, 0, time.UTC)

	c := &st.Columns[0]
	c.Phase, c.TurnN = PhaseStreaming, 1
	c.Prompt = "split the operator's wait out of the turn clock"
	c.Started = st.Now.Add(-5 * time.Minute)
	c.GateWait = runner.Span{D: 48 * time.Second, Measured: true}
	c.Body = "I need to write the new file."
	st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r2", ToolUseID: "t2",
		Text:      "Write: internal/council/clock.go",
		StoppedAt: st.Now.Add(-4 * time.Minute),
	}}

	c = &st.Columns[1]
	c.Phase, c.TurnN = PhaseDone, 1
	c.Prompt = "split the operator's wait out of the turn clock"
	c.Elapsed = 96 * time.Second
	c.Body = "Nothing here needed asking about."

	c = &st.Columns[2]
	c.Phase, c.TurnN = PhaseDone, 1
	c.Prompt = "split the operator's wait out of the turn clock"
	c.Elapsed = 96*time.Second + 400*time.Millisecond
	c.GateWait = runner.Span{D: 400 * time.Millisecond, Measured: true}
	c.Body = "Asked once and was answered at once."
	return st
}

// TestGateClockSplitsTheOperatorsWait is the render this section exists for.
//
// It is the clock half of TestWaitingIsNotStreaming's claim (view_test.go): that
// test guards the WORD, and this one guards the NUMBER under it. A seat held
// five minutes behind an approval card used to read `⋮ streaming 5m0s`, which is
// a stopped seat wearing a moving seat's figure — the same failure through the
// clock instead of through the phase.
func TestGateClockSplitsTheOperatorsWait(t *testing.T) {
	got := render(gateClockRoom())

	if strings.Contains(got, "streaming 5m0s") {
		t.Error("the header still charges the operator's wait to the vendor")
	}
	// And it does not charge the operator's wait to a vendor WORD either, which
	// is the amendment: while the card is up the header says `needs you` and
	// states no clock, because neither figure is time spent in that state. The
	// vendor's twelve seconds are asserted the moment the card is answered —
	// TestWaitingOnYouIsNotStreaming (view_test.go) carries that arm.
	if strings.Contains(got, "streaming") {
		t.Error("a seat stopped behind a card still claims output is arriving")
	}
	if !strings.Contains(got, needsYouWord) {
		t.Error("the header does not say the seat is stopped on the operator")
	}
	// 48s answered plus four minutes still open, measured against State.Now.
	if !strings.Contains(got, "you 4m48s") {
		t.Error("the room does not say how long it has been waiting on the operator")
	}
	// The card is two rows under the figure and spells the phrase the short form
	// sheds, which is what makes `you` a word this room has taught rather than
	// one it invented for a cell.
	if !strings.Contains(got, "waiting on you:") {
		t.Error("the card that raised the wait is not on screen with it")
	}

	// The finished seats: the vendor's own time in both, and the operator figure
	// in exactly one of them.
	if !strings.Contains(got, "done 1m36s") {
		t.Error("a finished seat lost the vendor's own time")
	}
	if !strings.Contains(got, "you 0s") {
		t.Error("a card answered inside a second rendered as no card at all")
	}
	if strings.Count(got, "you 0s") != 1 {
		t.Error("a turn that raised no card grew an operator figure")
	}

	golden(t, "gate-clock", got)
}

// gatedVsStreamingRoom puts the two states the amendment separates side by side,
// on ONE frame and on the SAME wall clock.
//
// Both seats started five minutes ago. Seat 1 has been stopped on an approval
// card for four minutes and forty-eight seconds of them; seat 2 has been working
// for all five. A frame that could not tell them apart is the whole defect, so a
// fixture that shows one without the other cannot pin the fix — the reader's
// question is never "what does a blocked seat look like", it is "which of these
// two is actually running".
//
// Seat 3 is idle and stays idle: a room where every column is doing something is
// not the room, and the third seat is what shows that the word only lands where a
// card is up.
func gatedVsStreamingRoom() State {
	st := room()
	st.Turn = 1
	st.Now = time.Date(2026, 8, 15, 12, 5, 0, 0, time.UTC)

	c := &st.Columns[0]
	c.Phase, c.TurnN = PhaseStreaming, 1
	c.Sandbox = SandboxClaim{Level: SandboxGated, Detail: "asks before every tool call"}
	c.Prompt = "say which seat is working"
	c.Started = st.Now.Add(-5 * time.Minute)
	c.Body = "I need to write the new file."
	st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text:      "Write: internal/council/clock.go",
		StoppedAt: st.Now.Add(-4*time.Minute - 48*time.Second),
	}}

	c = &st.Columns[1]
	c.Phase, c.TurnN = PhaseStreaming, 1
	c.Prompt = "say which seat is working"
	c.Started = st.Now.Add(-5 * time.Minute)
	c.Body = "Nobody has stopped me."
	return st
}

// TestGatedIsNotStreaming is the golden for the WORD, and it is deliberately the
// twin of waiting-vs-streaming.txt rather than an extension of it.
//
// That golden pins a vendor that reports nothing until it finishes against one
// whose text is arriving — two claims about the VENDOR. This one pins a vendor
// blocked on the operator against one nobody blocked — the same shape of claim
// about a different agent, which is why it is its own file: a golden's name is
// what it pins, and neither case is an edge of the other.
func TestGatedIsNotStreaming(t *testing.T) {
	got := render(gatedVsStreamingRoom())

	// The two headers on one row, and they must not say the same thing. The
	// blocked seat names the operator and states no clock; the working seat keeps
	// the word and the whole five minutes, because nobody took any of them.
	if !strings.Contains(got, needsYouWord) {
		t.Errorf("the blocked seat does not say it is stopped on the operator:\n%s", got)
	}
	if !strings.Contains(got, "streaming 5m0s") {
		t.Errorf("the seat nobody blocked lost its own five minutes:\n%s", got)
	}
	if strings.Count(got, "streaming") != 1 {
		t.Errorf("both seats claim output is arriving; only one of them has any:\n%s", got)
	}
	// The operator's figure stays where §9.45 put it, on the blocked seat's own
	// turn separator, and the seat nobody asked anything draws none at all —
	// absent, never a zero.
	if !strings.Contains(got, "you 4m48s") {
		t.Errorf("nothing says how long the room has waited on the operator:\n%s", got)
	}
	if n := len(operatorFigure.FindAllString(got, -1)); n != 1 {
		t.Errorf("want one operator figure on this frame, got %d:\n%s", n, got)
	}

	golden(t, "gated-vs-streaming", got)
}

// TestGatedIsNotStreamingASCII: the distinction is a WORD, so the reduced glyph
// set has to carry it whole. The mark in front of it is the room's existing
// warning glyph and says nothing the phrase does not (§7.1 rule 2).
func TestGatedIsNotStreamingASCII(t *testing.T) {
	st := gatedVsStreamingRoom()
	st.ASCII = true
	got := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(got, needsYouWord) {
		t.Errorf("--ascii lost the word that says the seat is stopped on you:\n%s", got)
	}
	golden(t, "gated-vs-streaming-ascii", got)
}

// TestGateClockIsPureOverState is TestElapsedIsPureOverState for the second
// figure this room now derives from a timestamp.
//
// The open stretch is State.Now minus PendingGate.StoppedAt, on the Reattach
// .SavedAt precedent: the room stamps, the renderer subtracts. A clock read
// inside Render would make every golden that carries an open card flaky in CI
// and nowhere else.
func TestGateClockIsPureOverState(t *testing.T) {
	st := gateClockRoom()
	first := render(st)
	time.Sleep(15 * time.Millisecond)
	if render(st) != first {
		t.Fatal("Render read a clock; the operator's figure must come from State.Now")
	}
}

// TestAnUnstampedCardIsNotAWait.
//
// Every State typed out by hand — which is most of the ones in this package —
// carries no StoppedAt. An unstamped card must add nothing rather than count from
// the epoch, or a fixture would render a fifty-year wait: a figure arrived at by
// arithmetic over an absence, which is the top item on §4a.1's rejected list.
//
// The card is still a CARD, and the amendment is where the two questions come
// apart. Whether the seat is stopped is answered by the card existing
// (gateStopped), so the header says so on this fixture like any other; how LONG it
// has been stopped is answered by a stamp, and there is none, so no figure is
// printed. Nothing is charged to the vendor either — the arm below the render
// takes the card away and finds the whole thirty seconds still there.
func TestAnUnstampedCardIsNotAWait(t *testing.T) {
	st := room()
	st.Turn = 1
	st.Now = time.Date(2026, 8, 15, 12, 5, 0, 0, time.UTC)
	st.Columns[0].Phase, st.Columns[0].TurnN = PhaseStreaming, 1
	st.Columns[0].Started = st.Now.Add(-30 * time.Second)
	st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", Text: "Write: a.txt",
	}}

	got := render(st)
	if !strings.Contains(got, needsYouWord) {
		t.Error("an unstamped card left the header claiming the vendor was working")
	}
	// `you ` alone is no longer the tell — the state word above ends in it. What
	// an unmeasured span must never draw is a FIGURE, and every one operatorCell
	// renders is `you ` welded to a digit.
	if operatorFigure.MatchString(got) {
		t.Error("an unstamped card rendered a wait nothing measured")
	}

	st.Gates = nil
	if got := render(st); !strings.Contains(got, "streaming 30s") {
		t.Errorf("an unstamped card took time off the vendor's clock:\n%s", got)
	}
}

// TestTheSeatsStopwatchStartsAtTheOldestCard.
//
// One assistant message can raise a parallel batch, and the seat is stopped from
// the first of them. Reading the newest stamp would forgive every minute before
// it — the seat did not resume when the second card arrived, because it never
// resumed at all.
func TestTheSeatsStopwatchStartsAtTheOldestCard(t *testing.T) {
	base := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	st := room()
	st.Gates = []PendingGate{
		{Vendor: model.VendorClaude, RequestID: "r1", StoppedAt: base.Add(4 * time.Minute)},
		{Vendor: model.VendorClaude, RequestID: "r2", StoppedAt: base.Add(time.Minute)},
		{Vendor: model.VendorCodex, RequestID: "r3", StoppedAt: base},
	}

	at, stopped := st.gateStoppedAt(model.VendorClaude)
	if !stopped {
		t.Fatal("a seat with two cards up is not reported stopped")
	}
	if !at.Equal(base.Add(time.Minute)) {
		t.Errorf("stopwatch starts at %v, want the oldest card's stamp", at)
	}
	// Per seat, never room-wide: Codex's older card is not Claude's wait.
	if at, _ := st.gateStoppedAt(model.VendorCodex); !at.Equal(base) {
		t.Error("one seat's stopwatch read another seat's card")
	}
	if _, stopped := st.gateStoppedAt(model.VendorAntigravity); stopped {
		t.Error("a seat with no card up is reported stopped")
	}
}

// gateClockModel is a live persistent seat with a decision sink, ready to be
// handed cards.
func gateClockModel(t *testing.T) *Model {
	t.Helper()
	m := turnModel(true)
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: &decisionSession{}}
	return m
}

// card is one already-open request on a seat that stopped `ago` ago.
//
// Stamped by hand rather than by queueGate so the assertions below are about the
// arithmetic rather than about how fast the test ran.
func card(id string, ago time.Duration) PendingGate {
	return PendingGate{
		Vendor: model.VendorClaude, RequestID: id, ToolUseID: id,
		Text: "Write: " + id + ".go", StoppedAt: time.Now().Add(-ago),
	}
}

// TestTheOperatorsWaitAccumulatesAcrossOneTurnsCards.
//
// A turn asks more than once. Each answered card closes a stretch and the
// stretches add up, because what the figure claims is the operator's share of
// THIS turn and not of the last question in it.
func TestTheOperatorsWaitAccumulatesAcrossOneTurnsCards(t *testing.T) {
	m := gateClockModel(t)

	m.st.Gates = []PendingGate{card("r1", 3*time.Second)}
	m.decideGate(true)
	first := m.st.Columns[0].GateWait
	if !first.Measured {
		t.Fatal("an answered card left the operator's wait unmeasured")
	}
	if first.D < 3*time.Second {
		t.Errorf("first stretch = %v, want at least the three seconds it was up", first.D)
	}

	m.st.Gates = []PendingGate{card("r2", 2*time.Second)}
	m.decideGate(false)
	total := m.st.Columns[0].GateWait
	if total.D < first.D+2*time.Second {
		t.Errorf("total = %v, want the second stretch added to %v", total.D, first.D)
	}

	// A denial is a decision, so it closes a stretch exactly as an approval
	// does: the seat was stopped for the same reason either way.
	if total.D <= first.D {
		t.Error("a denied card cost the operator nothing")
	}
}

// TestOverlappingCardsAreOneStretch is the double-count this measurement is most
// exposed to, and it is where the first cut of it was wrong.
//
// Two cards up at once means one stopped seat, not two. The stopwatch therefore
// runs over the SEAT: answering the first charges nothing at all, because the
// vendor has not resumed, and the whole stretch lands when the last card goes.
//
// The second card carries the FIRST one's stamp (queueGate, and the property
// below pins it), which is what makes that possible. Stamped with its own
// moment, the stretch's start would leave the queue with the card that owned it
// — the figure on screen would jump backwards mid-stretch, and the charge would
// lose every second before the newest card.
func TestOverlappingCardsAreOneStretch(t *testing.T) {
	m := gateClockModel(t)
	m.queueGate(&m.st.Columns[0], &runner.Gate{
		RequestID: "r1", ToolUseID: "t1", Tool: "Write", Text: "Write: a.go"})
	time.Sleep(5 * time.Millisecond)
	m.queueGate(&m.st.Columns[0], &runner.Gate{
		RequestID: "r2", ToolUseID: "t2", Tool: "Write", Text: "Write: b.go"})

	if len(m.st.Gates) != 2 {
		t.Fatalf("queue = %d, want both cards up", len(m.st.Gates))
	}
	if !m.st.Gates[1].StoppedAt.Equal(m.st.Gates[0].StoppedAt) {
		t.Fatal("the second card restamped a seat that was already stopped")
	}

	// Hand-stamped back four seconds, so the assertion below is about the
	// arithmetic rather than about how fast the test ran.
	stopped := time.Now().Add(-4 * time.Second)
	m.st.Gates[0].StoppedAt, m.st.Gates[1].StoppedAt = stopped, stopped

	m.decideGate(true)
	if m.st.Columns[0].GateWait.Measured {
		t.Fatal("a stretch was closed while the seat still had a card up — the wait is being counted twice")
	}

	m.decideGate(true)
	got := m.st.Columns[0].GateWait
	if !got.Measured {
		t.Fatal("the last card went and nothing was charged")
	}
	if got.D < 4*time.Second {
		t.Errorf("stretch = %v, want it measured from the moment the seat stopped", got.D)
	}
}

// TestAnAutoApprovedCardCostsTheOperatorNothing.
//
// A routine command is answered inside queueGate and no card is ever drawn. The
// operator read nothing and decided nothing, so charging them for it would be
// the room reporting a wait that did not happen (§4a.1).
func TestAnAutoApprovedCardCostsTheOperatorNothing(t *testing.T) {
	m := gateClockModel(t)
	m.queueGate(&m.st.Columns[0], &runner.Gate{
		RequestID: "r1", ToolUseID: "t1", Tool: "Bash",
		Text:  "Bash: go test ./...",
		Input: map[string]any{"command": "go test ./..."},
	})

	if m.st.Gating() {
		t.Fatal("a routine command was carded")
	}
	if m.st.Columns[0].GateWait.Measured {
		t.Error("a call nobody was asked about was charged to the operator")
	}
}

// TestACardStampsWhenItGoesUp pins the other half of the boundary: the carded
// path is stamped where the card is raised, outside Render.
func TestACardStampsWhenItGoesUp(t *testing.T) {
	m := gateClockModel(t)
	before := time.Now()
	m.queueGate(&m.st.Columns[0], &runner.Gate{
		RequestID: "r1", ToolUseID: "t1", Tool: "Write",
		Text: "Write: internal/council/clock.go",
	})

	if len(m.st.Gates) != 1 {
		t.Fatalf("queue = %d, want the carded request", len(m.st.Gates))
	}
	if at := m.st.Gates[0].StoppedAt; at.Before(before) || at.IsZero() {
		t.Errorf("card stamped %v, want the moment it was raised", at)
	}
}

// TestAnAbandonedCardStillCostsWhatItCost.
//
// The turn was cancelled, or its process died, and dropGates takes the card down
// unanswered. The operator really did hold the seat for that stretch, and a turn
// that ended badly is the one whose numbers a reader goes back to.
func TestAnAbandonedCardStillCostsWhatItCost(t *testing.T) {
	m := gateClockModel(t)
	m.st.Gates = []PendingGate{card("r1", 2*time.Second)}

	m.dropGates(model.VendorClaude)
	got := m.st.Columns[0].GateWait
	if !got.Measured || got.D < 2*time.Second {
		t.Errorf("abandoned card charged %v (measured=%v), want the stretch it was up",
			got.D, got.Measured)
	}
}

// TestANewTurnForgetsTheOperatorsWait.
//
// The figure is a fact about ONE turn. It travels into the record with the rest
// of that turn's chrome and the column goes back to unmeasured — not to zero,
// which would claim a new turn had asked and been answered instantly.
func TestANewTurnForgetsTheOperatorsWait(t *testing.T) {
	c := Column{Vendor: model.VendorClaude, TurnN: 1, Prompt: "first"}
	c.Elapsed = 5 * time.Minute
	c.GateWait = runner.Span{D: 4 * time.Minute, Measured: true}

	c.startTurn(2, "second", false)

	if len(c.History) != 1 {
		t.Fatalf("history = %d records, want the finished turn filed", len(c.History))
	}
	if got := c.History[0].GateWait; !got.Measured || got.D != 4*time.Minute {
		t.Errorf("filed record carries %+v, want the turn's own operator wait", got)
	}
	if c.GateWait.Measured {
		t.Error("the new turn opened with the old turn's wait still measured")
	}
}

// TestTheTranscriptKeepsTheSplit.
//
// A finished turn has no chrome of its own, so its separator is where the two
// numbers live. Both of them: the vendor's time with the operator's share out of
// it, and the share itself beside it (historyMeta).
func TestTheTranscriptKeepsTheSplit(t *testing.T) {
	cost := 0.0123
	h := TurnRecord{
		N: 3, Phase: PhaseDone,
		Elapsed:  5 * time.Minute,
		GateWait: runner.Span{D: 4*time.Minute + 48*time.Second, Measured: true},
		CostUSD:  &cost,
	}
	got := historyMeta(h)
	if !strings.Contains(got, "12s") {
		t.Errorf("meta = %q, want the vendor's own time", got)
	}
	if !strings.Contains(got, "you 4m48s") {
		t.Errorf("meta = %q, want the operator's share beside it", got)
	}
	if !strings.Contains(got, "$0.0123") {
		t.Errorf("meta = %q, want the cost kept", got)
	}
	// Thirty-six cells is a column in a three-up room at 120, and the separator
	// carries a `turn 3` label plus two cells of air each side of its rule.
	if len(got)+len("turn 3")+4 > 36 {
		t.Errorf("meta = %q is too wide for a column's turn separator", got)
	}

	// And the turn that raised no card says nothing about the operator.
	h.GateWait = runner.Span{}
	if strings.Contains(historyMeta(h), "you ") {
		t.Error("a turn with no card grew an operator figure in the transcript")
	}
}

// TestThePageSaysItWhole.
//
// The grid sheds the label because a column is thirty-six cells wide. A page
// rule is the whole frame, so it prints the room's own phrase — the one on the
// card and on the NEEDS YOU strip — rather than the abbreviation width forced.
//
// The page takes the amendment's word too, and for the reason the amendment
// gives: two surfaces reading one queue must not disagree about what it says. So
// the blocked seat's rule states `needs you` and the operator's whole figure, and
// the vendor's own twelve seconds arrive when the card is answered — which is the
// second arm here, because a word that hid a number would be no better than a
// number under the wrong word.
func TestThePageSaysItWhole(t *testing.T) {
	st := gateClockRoom()
	st.Page = TurnView{Open: true, Turn: 1}
	got := render(st)

	if !strings.Contains(got, "waiting on you 4m48s") {
		t.Error("the page abbreviates a figure it has the width to spell")
	}
	if !strings.Contains(got, needsYouWord) {
		t.Error("the page says the vendor is working while the room is stopped on you")
	}
	if strings.Contains(got, "streaming") {
		t.Error("the page still claims output is arriving from a stopped process")
	}

	st.Gates = nil
	st.Columns[0].GateWait = runner.Span{D: 4*time.Minute + 48*time.Second, Measured: true}
	if got := render(st); !strings.Contains(got, "12s") {
		t.Errorf("the answered page lost the vendor's own time:\n%s", got)
	}
}
