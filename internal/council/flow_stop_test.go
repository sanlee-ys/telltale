package council

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sanlee-ys/telltale/internal/model"
)

// This file pins §9.35: a running /flow chain can be told to stop after the
// hop it is on (`s`), a cancelled or failed chain dies whole, and no chain —
// however it ended — goes on claiming a hop or eating the enter that follows
// it. Like flow_security_test.go, every test here asserts an observable: the
// spawn count, the rendered marker, or the notice the user was actually shown.

// pressS reaches viewKey the way a user does — through key(), so the test
// fails if some gate starts swallowing the keystroke.
func pressS(m *Model) {
	m.key(tea.KeyPressMsg{Code: 's', Text: "s"})
}

// `s` during hop 1 of 3: hop 1 finishes on its own terms — artifact saved,
// Returned recorded — and hops 2 and 3 are never dispatched. The chain is
// GONE afterwards, marker included, and the notice says stopped, not finished.
func TestStopAfterCurrentHopEndsTheChainWhenTheHopReturns(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build widget -> @codex audit -> @agy summarize"
	m.dispatch()

	pressS(m)
	if !m.st.FlowStop {
		t.Fatal("s during a running hop did not arm the stop")
	}
	if !strings.Contains(m.st.Notice, "stops after hop 1/3") {
		t.Errorf("arming does not say what will happen: %q", m.st.Notice)
	}

	cursor := m.column(model.VendorCursor)
	cursor.Body = "cursor built the widget"
	m.finishColumn(cursor, PhaseDone)
	m.Update(eventBatchMsg{})

	if log.n() != 1 {
		t.Fatalf("spawns = %d — a stopped chain dispatched a successor", log.n())
	}
	if m.flowChain != nil || m.flowDraft != "" {
		t.Error("the chain survived its own stop and would claim the next enter")
	}
	if m.st.FlowSteps != 0 || m.st.FlowStop {
		t.Errorf("marker still up after the stop: hop=%d/%d stop=%v",
			m.st.FlowHop, m.st.FlowSteps, m.st.FlowStop)
	}
	// The hop itself finished on its own terms: its artifact was still saved.
	if !strings.Contains(m.st.Notice, "artifact saved") {
		t.Errorf("stopping cost the finished hop its artifact: %q", m.st.Notice)
	}
	// And the record says stopped — a stopped chain must not read as a finished
	// one (§4a.1).
	if !strings.Contains(m.st.Notice, "flow stopped after hop 1/3") ||
		!strings.Contains(m.st.Notice, "2 later hops not dispatched") {
		t.Errorf("the stop is not stated as a stop: %q", m.st.Notice)
	}

	// The enter after a stopped chain is the user's again.
	m.st.Draft = "@claude an ordinary brief"
	m.dispatch()
	if log.n() != 2 {
		t.Fatalf("the brief after a stopped chain did not dispatch: spawns=%d notice=%q", log.n(), m.st.Notice)
	}
}

// `s` is a toggle, and the armed state is on the room's chrome, not only in a
// notice: the hop cell says the chain stops here, and the busy mode line's `s`
// cell flips to name the reversal.
func TestFlowStopIsAToggleAndTheArmedStateRenders(t *testing.T) {
	countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit"
	m.dispatch()

	pressS(m)
	if !m.st.FlowStop {
		t.Fatal("first press did not arm")
	}
	pressS(m)
	if m.st.FlowStop {
		t.Fatal("second press did not disarm")
	}
	if !strings.Contains(m.st.Notice, "the chain continues") {
		t.Errorf("disarming does not say the handoff is back: %q", m.st.Notice)
	}

	// Rendered, on a State built like view_test's: armed shows on the hop cell
	// and the mode line; disarmed shows on neither.
	st := room()
	st.Turn = 1
	st.FlowHop, st.FlowSteps, st.FlowVendor = 1, 2, model.VendorCursor
	st.Columns[0].Phase = PhaseStreaming // busy, so the mode line is the busy line
	got := render(st)
	if strings.Contains(got, "stops here") {
		t.Error("an unarmed chain renders as stopping")
	}
	if !strings.Contains(got, "stop after hop") {
		t.Error("the busy mode line does not offer s while a chain runs")
	}
	st.FlowStop = true
	got = render(st)
	if !strings.Contains(got, "(stops here)") {
		t.Error("the armed stop is not on the hop marker — the promise lives only in a notice that scrolls away")
	}
	if !strings.Contains(got, "continue chain") {
		t.Error("the armed mode line does not name the way back")
	}

	// And a room with no chain never offers the key (§7.8: a promised key that
	// does nothing).
	plain := room()
	plain.Columns[0].Phase = PhaseStreaming
	if strings.Contains(render(plain), "stop after hop") {
		t.Error("the mode line offers s in a room with no chain")
	}
}

// The last hop refuses to arm: the chain ends there whether or not `s` is
// pressed, and a key that "worked" would claim credit for an outcome it did
// not cause.
func TestFlowStopOnTheLastHopRefuses(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit"
	m.dispatch()

	cursor := m.column(model.VendorCursor)
	cursor.Body = "built"
	m.finishColumn(cursor, PhaseDone)
	m.Update(eventBatchMsg{}) // hop 2/2 dispatched

	pressS(m)
	if m.st.FlowStop {
		t.Fatal("the last hop armed a stop it cannot deliver")
	}
	if !strings.Contains(m.st.Notice, "ends here anyway") {
		t.Errorf("the refusal does not explain itself: %q", m.st.Notice)
	}
}

// A dead key says why it did nothing (§9.12): `s` with no chain running is
// answered, not swallowed.
func TestFlowStopWithoutAChainExplainsItself(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeViewing
	pressS(m)
	if m.st.FlowStop {
		t.Fatal("a chainless room armed a flow stop")
	}
	if !strings.Contains(m.st.Notice, "no flow chain is running") {
		t.Errorf("s in a chainless room said nothing: %q", m.st.Notice)
	}
}

// ctrl+c mid-chain is the hard abort, and the chain now dies WHOLE. Measured
// before this change (2026-08-08): the chain survived as a corpse — header
// still claiming "hop 1/3", current step still Running — and the user's next
// brief was eaten by "flow start error: cannot start step in state running".
func TestCancelMidChainEndsTheChainAndTheNextBriefDispatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit -> @agy summarize"
	m.dispatch()

	// persistent_test.go's cancel emulation: the flag ctrl+c sets, then the
	// column retiring as its killed process would.
	m.cancelling = true
	cursor := m.column(model.VendorCursor)
	cursor.Body = "partial output"
	m.finishColumn(cursor, PhaseFailed)
	m.Update(eventBatchMsg{})

	if m.flowChain != nil || m.flowDraft != "" {
		t.Error("the chain survived the cancel and would claim the next enter")
	}
	if m.st.FlowSteps != 0 {
		t.Errorf("the header still claims hop %d/%d over a cancelled chain", m.st.FlowHop, m.st.FlowSteps)
	}
	// Cancelled, by name — not "stopped", and never readable as finished.
	if !strings.Contains(m.st.Notice, "flow cancelled at hop 1/3") ||
		!strings.Contains(m.st.Notice, "2 later hops not run") {
		t.Errorf("the cancel is not stated as one: %q", m.st.Notice)
	}
	if cursor.Phase != PhaseCancelled {
		t.Errorf("the cancelled hop's column phase = %v, want cancelled", cursor.Phase)
	}

	m.st.Draft = "@claude an ordinary brief"
	m.dispatch()
	if log.n() != 2 {
		t.Fatalf("the brief after a cancelled chain did not dispatch: spawns=%d notice=%q", log.n(), m.st.Notice)
	}
}

// A hop whose vendor FAILED ends the chain the same way — before this change
// it left the same corpse the cancel did, with the same eaten enter.
func TestAFailedHopEndsTheChainAndTheNextBriefDispatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit"
	m.dispatch()

	cursor := m.column(model.VendorCursor)
	cursor.Body = "partial output"
	m.finishColumn(cursor, PhaseFailed)
	m.Update(eventBatchMsg{})

	if log.n() != 1 {
		t.Fatalf("a failed hop dispatched a successor: %d spawns", log.n())
	}
	if m.flowChain != nil || m.st.FlowSteps != 0 {
		t.Error("the failed chain left a corpse or a marker behind")
	}
	if !strings.Contains(m.st.Notice, "flow stopped at hop 1/2") ||
		!strings.Contains(m.st.Notice, "1 later hop not run") {
		t.Errorf("the failure's reach is not stated: %q", m.st.Notice)
	}

	m.st.Draft = "@claude an ordinary brief"
	m.dispatch()
	if log.n() != 2 {
		t.Fatalf("the brief after a failed chain did not dispatch: spawns=%d notice=%q", log.n(), m.st.Notice)
	}
}

// A COMPLETED chain releases the room too. Measured before this change: the
// happy path itself left flowChain and flowDraft behind, and the first brief
// typed after a finished chain was eaten by "flow start error: cannot start
// step in state returned".
func TestACompletedChainDoesNotEatTheNextBrief(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit"
	m.dispatch()

	first := m.column(model.VendorCursor)
	first.Body = "built"
	m.finishColumn(first, PhaseDone)
	m.Update(eventBatchMsg{})
	second := m.column(model.VendorCodex)
	second.Body = "audited"
	m.finishColumn(second, PhaseDone)
	m.Update(eventBatchMsg{})

	if m.flowChain != nil || m.flowDraft != "" {
		t.Error("a finished chain is still holding the room")
	}
	m.st.Draft = "@claude an ordinary brief"
	m.dispatch()
	if log.n() != 3 {
		t.Fatalf("the brief after a finished chain did not dispatch: spawns=%d notice=%q", log.n(), m.st.Notice)
	}
}

// n at the write gate takes the marker down with the chain. The gate's own
// test already pins that the chain dies; this pins that the header stops
// claiming a hop the user just refused.
func TestGateNTakesTheHopMarkerDown(t *testing.T) {
	countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "/flow @codex publish write:docs/out.md -> @claude review it"
	m.dispatch()
	if m.st.FlowSteps == 0 {
		t.Fatal("setup: the gated hop is not on the marker")
	}

	m.key(tea.KeyPressMsg{Code: 'n', Text: "n"})
	if m.st.FlowSteps != 0 {
		t.Errorf("the header still claims hop %d/%d after n cancelled the chain", m.st.FlowHop, m.st.FlowSteps)
	}
}
