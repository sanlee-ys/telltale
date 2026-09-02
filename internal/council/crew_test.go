package council

import (
	"context"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The fixtures for a room with per-seat turns (design.md §9.54).
//
// Before §9.54 a test put the room mid-turn with `m.turn = &turnState{}`: the
// pointer's existence was the fact, and its contents did not matter. A turn is
// now a fact about a SEAT, so the same fixture has to say which seat is busy —
// which is also what the refusals it exercises now name.

// occupy puts the given seats (the focused seat, when none is named) on a
// dispatch in flight without spawning anything, numbered as the room's next
// turn, and returns the record so a test can reach its maps.
func occupy(m *Model, seats ...model.VendorID) *turnState {
	if len(seats) == 0 {
		switch c := m.focused(); {
		case c != nil:
			seats = []model.VendorID{c.Vendor}
		case len(m.st.Columns) > 0:
			seats = []model.VendorID{m.st.Columns[0].Vendor}
		default:
			// A room typed out with no columns at all still has to be able
			// to be "mid-turn" for the room-wide refusals, which read the map
			// and not the grid.
			seats = []model.VendorID{model.VendorClaude}
		}
	}
	ts := &turnState{
		n:          m.st.Turn + 1,
		cancel:     func() {},
		seatCancel: map[model.VendorID]context.CancelFunc{},
		live:       map[model.VendorID]bool{},
		persistent: map[model.VendorID]bool{},
	}
	for _, v := range seats {
		ts.live[v] = true
	}
	m.holdTurn(ts)
	return ts
}

// idle takes every seat off its turn — the fixture that used to be `m.turn =
// nil`. The dispatch records are dropped, not cancelled: nothing here was ever
// spawned.
func idle(m *Model) { m.turns = map[model.VendorID]*turnState{} }

// markCancelling is the flag ctrl+c sets on one seat (cancelSeat), for the
// tests that emulate a cancel without going through the key. It tolerates a
// Model typed out as a literal, which has no map.
func markCancelling(m *Model, v model.VendorID) {
	if m.cancelling == nil {
		m.cancelling = map[model.VendorID]bool{}
	}
	m.cancelling[v] = true
}

// crewRoom is flowRoom in the shape the crew tests need: the same four
// installed seats, a workspace, and the room in view mode as a dispatch leaves
// it. Every spawn is stubbed by the caller's countSpawns, so the persistent
// seats get deadSession (alive, inert) and the one-shot seats get an empty
// runner.Handle — whose Kill needs a process that was never spawned, so the
// tests that stop a one-shot seat swap its handle for a recordedKill first.
func crewRoom(t *testing.T) *Model {
	t.Helper()
	m := flowRoom(t, true)
	m.st.Width, m.st.Height = 120, 24
	m.st.Mode = ModeComposing
	return m
}

// send types a brief and presses enter, the way an operator dispatches.
func send(t *testing.T, m *Model, brief string) {
	t.Helper()
	m.st.Mode = ModeComposing
	m.setDraft(brief)
	m.key(key("enter"))
}

// fakeHandles replaces every one-shot handle on the seat's dispatch with an
// observable fake and returns them by seat.
func fakeHandles(m *Model) map[model.VendorID]*recordedKill {
	out := map[model.VendorID]*recordedKill{}
	for _, ts := range m.dispatches() {
		for v := range ts.seatHandles {
			k := &recordedKill{}
			ts.seatHandles[v] = k
			out[v] = k
		}
	}
	return out
}

// TestASeatTakesABriefWhileAnotherIsStillAnswering is the product change
// (§9.54): two briefs to two seats, the second sent while the first is still
// open, and both in flight on their own turn numbers.
func TestASeatTakesABriefWhileAnotherIsStillAnswering(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)

	send(t, m, "@codex refactor the poller")
	if log.n() != 1 || m.turnOf(model.VendorCodex) == nil {
		t.Fatalf("the first brief did not put codex on a turn (%d spawns)", log.n())
	}
	send(t, m, "@antigravity write the docs")
	if log.n() != 2 {
		t.Fatalf("the second brief did not spawn while the first was open: %d spawns, notice %q", log.n(), m.st.Notice)
	}
	if m.st.Notice != "" {
		t.Errorf("a clean second dispatch left a notice: %q", m.st.Notice)
	}
	cx, ag := m.turnOf(model.VendorCodex), m.turnOf(model.VendorAntigravity)
	if cx == nil || ag == nil || cx == ag {
		t.Fatalf("the two seats are not on two dispatches: codex=%p agy=%p", cx, ag)
	}
	if cx.n != 1 || ag.n != 2 || m.st.Turn != 2 {
		t.Errorf("turn numbers: codex on %d, agy on %d, room at %d — want 1, 2, 2", cx.n, ag.n, m.st.Turn)
	}
	if got := m.column(model.VendorCodex).TurnN; got != 1 {
		t.Errorf("codex's column moved to turn %d when antigravity was dispatched", got)
	}
	if m.st.TurnRoute == nil || m.st.TurnRoute.label() != "agy" {
		t.Errorf("the header names %v, want the most recent dispatch's route", m.st.TurnRoute)
	}
	// One pump, however many dispatches: a second reader would reorder events.
	if !m.eventsArmed {
		t.Error("no event reader is armed with two seats in flight")
	}
	if cmd := m.waitEvents(); cmd != nil {
		t.Error("a second event reader was armed beside the first")
	}
}

// TestABusySeatIsRefusedAndTheIdleOnesStillGo: a brief that names a seat
// mid-answer is refused for that seat, by name and turn, and the seats it also
// names are dispatched.
func TestABusySeatIsRefusedAndTheIdleOnesStillGo(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)

	send(t, m, "@codex refactor the poller")
	before := m.column(model.VendorCodex).Prompt
	send(t, m, "@all is the poller worth keeping?")

	if log.n() != 1+3 {
		t.Fatalf("%d spawns, want the first brief plus the three idle seats", log.n())
	}
	if m.turnOf(model.VendorCodex).n != 1 {
		t.Error("codex was moved onto the second brief while still answering the first")
	}
	if got := m.column(model.VendorCodex).Prompt; got != before {
		t.Errorf("the busy seat's prompt was overwritten: %q", got)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorAntigravity, model.VendorCursor} {
		if ts := m.turnOf(v); ts == nil || ts.n != 2 {
			t.Errorf("%s did not take turn 2: %v", v, ts)
		}
	}
	for _, want := range []string{"sent to claude, agy, cursor", "skipped: codex (turn 1)", "ctrl+c"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the notice does not say %q: %q", want, m.st.Notice)
		}
	}
}

// TestABriefOnlyToBusySeatsKeepsTheDraft: nothing to send, so nothing moves.
func TestABriefOnlyToBusySeatsKeepsTheDraft(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)

	send(t, m, "@codex refactor the poller")
	send(t, m, "@codex and add tests")

	if log.n() != 1 {
		t.Fatalf("a brief to a busy seat spawned: %d spawns", log.n())
	}
	if m.st.Draft != "@codex and add tests" {
		t.Errorf("the refused brief was not kept in the composer: %q", m.st.Draft)
	}
	if m.st.Turn != 1 {
		t.Errorf("a refused brief counted as a dispatch: turn %d", m.st.Turn)
	}
	for _, want := range []string{"codex (turn 1)", "in flight", "ctrl+c"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the refusal does not say %q: %q", want, m.st.Notice)
		}
	}
}

// TestARaceNeedsTheRoomIdleAndOwnsItWhileItRuns: the two room-wide refusals
// that survive §9.54, each in its own direction.
func TestARaceNeedsTheRoomIdleAndOwnsItWhileItRuns(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)

	send(t, m, "@codex refactor the poller")
	send(t, m, "/arena race it")
	if m.arenaPrep != nil || log.n() != 1 {
		t.Fatal("a race started over a busy seat")
	}
	if !strings.Contains(m.st.Notice, "codex (turn 1)") || !strings.Contains(m.st.Notice, "race") {
		t.Errorf("the race refusal does not name the busy seat: %q", m.st.Notice)
	}

	// And the other way: a race in flight refuses every brief.
	idle(m)
	occupy(m, model.VendorClaude, model.VendorCodex).arena = true
	send(t, m, "@antigravity write the docs")
	if log.n() != 1 {
		t.Fatal("an ordinary brief dispatched under a race")
	}
	if !strings.Contains(m.st.Notice, "race") || !strings.Contains(m.st.Notice, "ctrl+c") {
		t.Errorf("the refusal does not say a race owns the seats: %q", m.st.Notice)
	}
}

// TestCtrlCCancelsTheFocusedSeatAndLeavesItsNeighbourWorking is the key's new
// first meaning; the whole-room act is still there when the focused seat is
// idle, and quitting waits for everyone.
func TestCtrlCCancelsTheFocusedSeatAndLeavesItsNeighbourWorking(t *testing.T) {
	countSpawns(t)
	m := crewRoom(t)
	send(t, m, "@codex refactor the poller")
	send(t, m, "@antigravity write the docs")
	handles := fakeHandles(m)

	m.st.Mode = ModeViewing
	focusSeatOn(t, m, model.VendorCodex)
	m.key(key("ctrl+c"))
	if !handles[model.VendorCodex].killed {
		t.Error("ctrl+c on the focused seat did not kill its process")
	}
	if handles[model.VendorAntigravity].killed {
		t.Error("ctrl+c on codex killed antigravity's process too")
	}
	if !m.cancelling[model.VendorCodex] || m.cancelling[model.VendorAntigravity] {
		t.Errorf("cancelling = %v, want codex alone", m.cancelling)
	}
	// The exit lands and the column says it was cancelled, not that it failed.
	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindDone}})
	if c := m.column(model.VendorCodex); c.Phase != PhaseCancelled {
		t.Errorf("the cancelled seat landed %v", c.Phase)
	}
	if m.turnOf(model.VendorAntigravity) == nil {
		t.Fatal("cancelling codex ended antigravity's turn")
	}

	// `q` waits for the seat still working, and names it. The cancelled
	// dispatch's end dropped the room into compose, as any landing does, so
	// the key is pressed from view mode again.
	m.st.Mode = ModeViewing
	m.key(key("q"))
	if !strings.Contains(m.st.Notice, "agy (turn 2)") {
		t.Errorf("q did not name the busy seat: %q", m.st.Notice)
	}

	// ctrl+c on an idle seat while another works is the whole-room act.
	focusSeatOn(t, m, model.VendorCodex)
	m.key(key("ctrl+c"))
	if !handles[model.VendorAntigravity].killed {
		t.Error("ctrl+c on an idle seat did not cancel the seat still working")
	}
}

// TestAFlowHopDispatchesWhenItsOwnSeatLands: the chain waits on its seat, not
// on the room, and a hop whose seat is busy stops the chain by name.
func TestAFlowHopDispatchesWhenItsOwnSeatLands(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)
	send(t, m, "@antigravity write the docs")
	send(t, m, "/flow @codex plan -> @claude review")
	if log.n() != 2 || m.turnOf(model.VendorCodex) == nil {
		t.Fatalf("hop 1 did not dispatch beside the busy seat: %d spawns, %q", log.n(), m.st.Notice)
	}

	// Hop 1 lands while antigravity is still working: hop 2 goes anyway.
	m.column(model.VendorCodex).Body = "the plan"
	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindDone}})
	m.Update(eventBatchMsg{})
	if log.n() != 3 || m.turnOf(model.VendorClaude) == nil {
		t.Fatalf("hop 2 waited on the room instead of on its seat: %d spawns, %q", log.n(), m.st.Notice)
	}
	if m.turnOf(model.VendorAntigravity) == nil {
		t.Error("the chain's hop ended an unrelated seat's turn")
	}
	if m.st.FlowHop != 2 {
		t.Errorf("the header says hop %d, want 2", m.st.FlowHop)
	}

	// A chain whose next seat is busy stops, and says which seat and why.
	idle(m)
	m.endFlowChain()
	send(t, m, "@codex refactor the poller")
	send(t, m, "/flow @codex audit -> @claude review")
	if m.flowChain != nil || m.st.FlowSteps != 0 {
		t.Error("a chain whose first hop's seat is busy was left standing")
	}
	if !strings.Contains(m.st.Notice, "flow stopped") || !strings.Contains(m.st.Notice, "@codex is still on turn") {
		t.Errorf("the chain refusal does not name the seat and its turn: %q", m.st.Notice)
	}
}

// TestARebuttalQuotesABusySeatsLastFiledAnswer: the snapshot is taken at this
// seat's dispatch, and a neighbour mid-answer is quoted by what it last
// FINISHED saying, never by the half it has streamed.
func TestARebuttalQuotesABusySeatsLastFiledAnswer(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)
	send(t, m, "@codex first opinion")
	cx := m.column(model.VendorCodex)
	cx.Body = "FILED-ANSWER"
	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindDone}})
	send(t, m, "@codex second opinion")
	cx.Body = "HALF-STREAMED"

	m.st.Quote = true
	send(t, m, "@antigravity rebut")
	spec := log.specs[len(log.specs)-1]
	if got := specPrompt(spec); !strings.Contains(got, "FILED-ANSWER") || strings.Contains(got, "HALF-STREAMED") {
		t.Errorf("the rebuttal quoted the wrong codex reply:\n%s", got)
	}
}

// TestTeardownReapsEveryDispatch: two dispatches in flight, one teardown, and
// every process of both is killed once.
func TestTeardownReapsEveryDispatch(t *testing.T) {
	countSpawns(t)
	m := crewRoom(t)
	send(t, m, "@codex refactor the poller")
	send(t, m, "@antigravity write the docs")
	handles := fakeHandles(m)
	if len(m.dispatches()) != 2 {
		t.Fatalf("%d dispatches in flight, want 2", len(m.dispatches()))
	}

	m.teardown()
	for v, h := range handles {
		if !h.killed {
			t.Errorf("%s survived teardown", v)
		}
	}
	if m.anyInFlight() {
		t.Error("a dispatch survived teardown")
	}
}

// TestTheHeaderCountsSeatsBeyondTheNamedDispatch is the header golden: the
// cell names the newest dispatch and counts the seats an older one still
// holds. Two seats in flight, two turn numbers.
func twoInFlight() State {
	st := room()
	st.Turn = 5
	sent := Route{Vendors: []model.VendorID{model.VendorCodex}}
	st.TurnRoute = &sent
	st.Columns[0].Phase, st.Columns[0].TurnN = PhaseStreaming, 4
	st.Columns[0].Prompt = "refactor the poller"
	st.Columns[0].Body = "Reading the retry loop first."
	st.Columns[1].Phase, st.Columns[1].TurnN = PhaseWaiting, 5
	st.Columns[1].Prompt = "write the docs"
	return st
}

func TestTheHeaderCountsSeatsBeyondTheNamedDispatch(t *testing.T) {
	golden(t, "two-in-flight", render(twoInFlight()))
	line := strings.SplitN(render(twoInFlight()), "\n", 2)[0]
	if !strings.Contains(line, "turn 5 → codex · 2 in flight") {
		t.Errorf("the header does not name the newest route and count the rest: %q", line)
	}
	// The count is a claim about OTHER dispatches: one @all turn with all its
	// seats working states no count, and a live column with no turn number
	// cannot be counted as another dispatch.
	st := twoInFlight()
	st.Columns[0].TurnN = 5
	if strings.Contains(render(st), "in flight") {
		t.Error("seats all on the named dispatch were counted as if they were elsewhere")
	}
	st.Columns[0].TurnN = 0
	if strings.Contains(render(st), "in flight") {
		t.Error("a live column with no turn number was counted as another dispatch")
	}
}

// TestABusySeatRefusalIsOnScreen is the golden for the refusal a reader sees:
// the draft kept, the notice naming the seat and its turn.
func TestABusySeatRefusalIsOnScreen(t *testing.T) {
	st := twoInFlight()
	st.Mode = ModeComposing
	st.Draft = "@codex and add tests"
	st.Route = Route{Vendors: []model.VendorID{model.VendorCodex}}
	st.Notice = "a turn is in flight on codex (turn 5) — ctrl+c on its column cancels that turn, or address another seat"
	golden(t, "busy-seat-refused", render(st))
}
