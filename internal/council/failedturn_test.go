package council

import (
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// agyFailedResult is agy's own end-of-turn line for a turn that FAILED, in the
// shape vendors/agy_test.go pins verbatim off the 2026-08-04 capture (agy
// 1.1.10, Windows) — status ERROR, an empty response, and the vendor's sentence
// in `error`. Only the conversation id is synthesized, per CLAUDE.md.
//
// The line is parsed by the real adapter rather than hand-built into an Event.
// What this file is about is the SHAPE agy puts on the wire for a failure, so an
// Event written here would assert the room's handling of a belief about the
// vendor instead of of the vendor's own bytes.
const agyFailedResult = `{"event":"result","result":{"conversation_id":"11111111-2222-3333-4444-555555555555",` +
	`"status":"ERROR","response":"","error":"Agent execution terminated due to error.",` +
	`"duration_seconds":5.1746031,"num_turns":1}}`

// agyTurnModel is turnModel's spawn-per-turn room with the agy seat in it. Built
// from turnModel rather than beside it so the two cannot drift on the turn
// bookkeeping, which is the seam both files test.
func agyTurnModel(t *testing.T) *Model {
	t.Helper()
	m := turnModel(false)
	// The mode a dispatched turn puts the room in (dispatch.go). It is what the
	// footer branch this file asserts on is drawn for: compose names neither `q`
	// nor `ctrl+c`, so a test left in the zero mode would check nothing.
	m.st.Mode = ModeViewing
	m.st.Columns[0].Vendor = model.VendorAntigravity
	m.st.Columns[0].Label = "Antigravity"
	ts := m.turnOf(model.VendorClaude)
	delete(ts.live, model.VendorClaude)
	delete(m.turns, model.VendorClaude)
	ts.live[model.VendorAntigravity] = true
	m.turns[model.VendorAntigravity] = ts
	return m
}

// agyFailureEvent parses the failing line and asserts it still has the four
// properties this file's case rests on. A vendor bump that changed any of them
// must fail here, loudly, rather than quietly turning these tests into a check
// of a case that no longer exists.
func agyFailureEvent(t *testing.T) runner.Event {
	t.Helper()
	ev, ok := vendors.Antigravity{}.ParseEvent([]byte(agyFailedResult))
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%+v, %v), want a KindError", ev, ok)
	}
	if ev.EndsTurn {
		t.Fatal("agy now names its own end of turn; this case is about the failure that does NOT")
	}
	if ev.ExitCode != 0 || ev.Err != nil {
		t.Fatalf("exit=%d err=%v — the case is a failure the PROCESS has not reported", ev.ExitCode, ev.Err)
	}
	ev.Vendor = model.VendorAntigravity
	return ev
}

// TestAVendorReportedFailureLeavesTheRoomInFlight is the hole PR #254 named in
// InFlight's own doc comment, written as the room a user would be sitting in
// front of.
//
// agy reports a failed turn IN ITS STREAM: a `result` with status ERROR, which
// the adapter turns into a KindError carrying exit code 0 and no error, because
// the process has not failed and has not exited. `applyEvents` took that as the
// column's phase change and nothing else, so the seat went to `failed` while its
// process was still winding down and its vendor was still in `m.turn.live`.
//
// The room that produced is the wedge: every seat reads terminal, no spinner
// moves, and `q` is still refused with "a turn is in flight" — because it is.
// The footer offers that key on InFlight(), so the room advertised a key that
// answers with a notice, which is §7.8's surprise and the same defect §9.33's
// settle branch was written to close for codex.
//
// This is the SHAPE rather than one vendor's bug: any spawn-per-turn seat whose
// adapter reports a turn failure in-stream reaches the same branch. agy is the
// only seat that does so today — codex and grok have no structured error frame
// at all (vendors/testdata/wire/README.md), so their failure IS the exit.
func TestAVendorReportedFailureLeavesTheRoomInFlight(t *testing.T) {
	m := agyTurnModel(t)
	m.applyEvents([]runner.Event{agyFailureEvent(t)})

	// The turn MUST survive. turnColumnFinished cancels the turn's context and
	// runner.Start kills the child on it, so retiring here would kill a process
	// that is still winding down — the same refusal §9.33 made for codex, on the
	// same reasoning, and the reason this is a settle rather than a retirement.
	if !m.anyInFlight() {
		t.Fatal("the vendor's failure line retired the turn; the turn's cancel would kill a process that is still alive")
	}

	c := m.st.Columns[0]
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed: the vendor said the turn failed", c.Phase)
	}
	if m.st.Busy() {
		t.Error("Busy stayed true over a seat that has stopped working; the spinner would run on a failed column")
	}
	if !c.Settling {
		t.Error("the failed column does not say its process is still exiting — the room goes silent with the composer locked")
	}
	if !m.st.InFlight() {
		t.Error("InFlight went false while the turn was live: every seat renders idle and the footer offers `q`, which key() refuses")
	}
	// The footer is where the user meets this, so it is asserted on the footer
	// and not only on the predicate behind it.
	if keys := hintKeys(modeHints(m.st, GlyphsFor(true))); keys["q"] {
		t.Error("the footer offered `q` while the turn was in flight; pressing it answers with a notice")
	} else if !keys["ctrl+c"] {
		t.Error("the footer named no way to stop a turn that is still running")
	}
	// The vendor's own sentence still reaches the card. A settle must not cost
	// the user the diagnosis.
	if c.Note != "Agent execution terminated due to error." {
		t.Errorf("note = %q, want the vendor's own words", c.Note)
	}
}

// TestAVendorReportedFailureRetiresOnTheExit is the other half: the settle is a
// waiting room, not a new resting state. The process exit still retires the
// column, still ends the turn, and takes the linger word with it.
func TestAVendorReportedFailureRetiresOnTheExit(t *testing.T) {
	m := agyTurnModel(t)
	m.applyEvents([]runner.Event{agyFailureEvent(t)})
	failed := m.st.Columns[0].Elapsed
	if failed == 0 {
		t.Fatal("the failure stamped no elapsed")
	}

	m.applyEvents([]runner.Event{{Vendor: model.VendorAntigravity, Kind: runner.KindDone}})

	if m.anyInFlight() {
		t.Error("the process exit did not end the turn; `q` would stay refused forever")
	}
	c := m.st.Columns[0]
	if c.Settling {
		t.Error("a retired column still claims to be exiting")
	}
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v after the exit, want the vendor's failure kept", c.Phase)
	}
	if c.Elapsed != failed {
		t.Error("the exit overwrote the turn's own figure with the process's lifetime")
	}
	if m.st.InFlight() {
		t.Error("the room is still in flight after the turn ended")
	}
}

// TestALateVendorReportedFailureCannotResurrectTheRoom is the guard the settle
// needs, and it is the same one §9.33's review found for the end-of-turn line: a
// killed process drains its buffered stdout, so this line can arrive after its
// column is already terminal or after the turn boundary entirely. Marking a
// retired column as exiting would hold InFlight true with nothing running —
// a room wedged the other way, where the footer never offers `q` again.
func TestALateVendorReportedFailureCannotResurrectTheRoom(t *testing.T) {
	for _, phase := range []Phase{PhaseCancelled, PhaseFailed, PhaseDone} {
		m := agyTurnModel(t)
		m.st.Columns[0].Phase = phase
		m.applyEvents([]runner.Event{agyFailureEvent(t)})
		if m.st.Columns[0].Settling {
			t.Errorf("%v: a terminal column was marked as still exiting by a late failure line", phase)
		}
	}

	m := agyTurnModel(t)
	idle(m)
	m.applyEvents([]runner.Event{agyFailureEvent(t)})
	if m.st.Columns[0].Settling {
		t.Error("a failure from a dead turn settled a column, so the footer would never offer `q` again")
	}
	if m.st.InFlight() {
		t.Error("a failure from a dead turn put the room back in flight")
	}
}

// hintKeys is the footer's keys as a set, so a test can ask what it offered
// without depending on the order the cells are laid out in.
func hintKeys(hs []hint) map[string]bool {
	keys := map[string]bool{}
	for _, h := range hs {
		keys[h.key] = true
	}
	return keys
}
