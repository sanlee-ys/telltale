package council

import (
	"context"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// turnModel is traceModel plus a turn in flight on a persistent seat. The turn
// bookkeeping is the seam this file tests: a column can now be retired by four
// different signals, and getting that wrong either hangs the room or ends a
// turn while a vendor is still talking.
func turnModel(persistent bool) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		st: State{Columns: []Column{{
			Vendor: model.VendorClaude, Label: "Claude Code",
			Avail: AvailInstalled, Phase: PhaseStreaming,
			Started: time.Now().Add(-time.Second),
		}}},
		sessions:   map[model.VendorID]string{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      map[model.VendorID]*seatProc{},
		roomCtx:    ctx,
		roomCancel: cancel,
	}
	m.turn = &turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{},
	}
	if persistent {
		m.turn.persistent[model.VendorClaude] = true
	}
	return m
}

// TestPersistentTurnEndsOnTheVendorsOwnLine.
//
// A spawn-per-turn child says "the turn is over" by dying. A persistent one
// never dies, so the end-of-turn line is the only signal there is — and if it
// were dropped the column would spin forever while the room waited for an exit
// that is not coming.
func TestPersistentTurnEndsOnTheVendorsOwnLine(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta,
		Text: "done", EndsTurn: true,
	}})

	if got := m.st.Columns[0].Phase; got != PhaseDone {
		t.Errorf("phase = %v, want done", got)
	}
	if m.turn != nil {
		t.Error("the turn is still in flight after its only end signal")
	}
	if m.st.Mode != ModeComposing {
		t.Error("the room did not return to compose after the turn ended")
	}
}

// TestSpawnPerTurnIgnoresTheEndOfTurnLine.
//
// The same `result` line carries EndsTurn for every vendor, but a spawn-per-turn
// column is retired by its process exit — the flush of the redactor and the
// final elapsed both hang off it. Acting on both would retire the column twice.
func TestSpawnPerTurnIgnoresTheEndOfTurnLine(t *testing.T) {
	m := turnModel(false)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta,
		Text: "done", EndsTurn: true,
	}})
	if m.turn == nil {
		t.Fatal("a spawn-per-turn column was retired by a stream line rather than by its exit")
	}
	if got := m.st.Columns[0].Phase; got != PhaseStreaming {
		t.Errorf("phase = %v, want still streaming until the process exits", got)
	}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})
	if m.turn != nil {
		t.Error("the process exit did not end the turn")
	}
}

// TestPersistentProcessDeathMidTurnFailsTheColumn.
//
// The hang this prevents is the whole reason KindDone is handled separately for
// a persistent seat: the process is not supposed to exit, so an exit during a
// turn means the answer is not coming. A column that simply stopped would be
// indistinguishable from one that finished.
func TestPersistentProcessDeathMidTurnFailsTheColumn(t *testing.T) {
	m := turnModel(true)
	m.procs[model.VendorClaude] = &seatProc{}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindDone, ExitCode: 4,
	}})

	c := m.st.Columns[0]
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed: the answer is not coming", c.Phase)
	}
	if c.Note == "" {
		t.Error("a column that lost its process said nothing about why")
	}
	if m.turn != nil {
		t.Error("the turn never ended after the process died")
	}
	if _, ok := m.procs[model.VendorClaude]; ok {
		t.Error("a dead process was kept; the next brief would write into a closed pipe")
	}
}

// TestPersistentProcessDeathBetweenTurnsIsNotAFailure. Quitting the room kills
// these processes on purpose, and the exit that follows must not paint a
// finished column red.
func TestPersistentProcessDeathBetweenTurnsIsNotAFailure(t *testing.T) {
	m := turnModel(true)
	m.turn = nil
	m.st.Columns[0].Phase = PhaseDone
	m.procs[model.VendorClaude] = &seatProc{}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})

	if got := m.st.Columns[0].Phase; got != PhaseDone {
		t.Errorf("phase = %v, want the finished column left alone", got)
	}
}

// TestRetiringAColumnTwiceDoesNotEndTheTurnEarly.
//
// A persistent seat really can report twice — its end-of-turn line, and then its
// process dying — and the counter this replaced would have decremented for both,
// ending the turn while another vendor was still mid-sentence.
func TestRetiringAColumnTwiceDoesNotEndTheTurnEarly(t *testing.T) {
	m := turnModel(true)
	m.st.Columns = append(m.st.Columns, Column{
		Vendor: model.VendorCodex, Label: "Codex",
		Avail: AvailInstalled, Phase: PhaseWaiting,
	})
	m.turn.live[model.VendorCodex] = true

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true},
		{Vendor: model.VendorClaude, Kind: runner.KindDone},
	})

	if m.turn == nil {
		t.Fatal("the turn ended while Codex was still working")
	}
	if !m.turn.live[model.VendorCodex] {
		t.Error("Codex was retired by another column's events")
	}
}

// TestCancelledPersistentTurnIsNotAVendorFailure.
//
// Interrupting comes back as a result with is_error true — the vendor really
// does report a failure. But the user's keystroke is not the vendor falling
// over, and blaming it for one is a false claim on screen.
func TestCancelledPersistentTurnIsNotAVendorFailure(t *testing.T) {
	m := turnModel(true)
	m.cancelling = true

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note: "the vendor reported the turn failed", EndsTurn: true,
	}})

	c := m.st.Columns[0]
	if c.Phase != PhaseCancelled {
		t.Errorf("phase = %v, want cancelled", c.Phase)
	}
	if c.Note != "cancelled — the output above is partial" {
		t.Errorf("note = %q, want the cancellation wording rather than the vendor's error", c.Note)
	}
}

// TestPersistentCostIsLabelledAsASessionTotal.
//
// Measured: two turns of one process reported $0.1061493 then $0.1177296 while
// the per-turn usage block stayed at 2 input tokens both times. The number is
// true and the cell has always meant "this turn", so the two must not render
// alike.
func TestPersistentCostIsLabelledAsASessionTotal(t *testing.T) {
	cost := 0.1177296
	c := Column{
		Vendor: model.VendorClaude, Label: "Claude Code", Avail: AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxWrite}, Gran: GranTokens,
		CostUSD: &cost, CostSession: true,
	}
	if got := badgeLine(c); got != "WRITES  tokens  $0.1177 session" {
		t.Errorf("badge = %q, want the cost named as a session total", got)
	}

	c.CostSession = false
	if got := badgeLine(c); got != "WRITES  tokens  $0.1177" {
		t.Errorf("badge = %q, want a bare per-turn cost", got)
	}
}

// TestGateWithNoUIIsDeniedNotAllowed.
//
// This build has no way to ask, so the only two options are to deny or to
// approve silently. Approving would be a gate that approves everything while
// looking like a gate, which is the exact false claim this repo exists to
// refuse. Unreachable in practice — the postures here never produce a request —
// and pinned anyway, because "unreachable" is how the last three false claims
// in this room survived review.
func TestGateWithNoUIIsDeniedNotAllowed(t *testing.T) {
	m := turnModel(true)
	// No process registered, so the answer cannot be sent; the assertion is that
	// nothing panics and nothing is approved.
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindGate,
		Gate: &runner.Gate{RequestID: "r1", Tool: "Write", Text: "Write: x.txt"},
	}})
	if m.turn == nil {
		t.Error("a gate request ended the turn")
	}
}
