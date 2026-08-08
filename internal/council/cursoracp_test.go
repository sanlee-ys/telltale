package council

import (
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The Cursor seat's lifecycle, witnessed by PROCESS COUNTS rather than by
// anything the adapter says about itself.
//
// That is the whole point of these four. §9.33 measured the seat's cost as ~8.1s
// of process, paid on every turn, and §9.36 replaced it with a handshake paid
// once — so "one process across many turns" is not an implementation detail
// here, it IS the feature, and a spawn that quietly crept back onto the per-turn
// path would undo the change while every other test still passed.

// exitedSession is a process that has gone. Distinct from deadSession, whose
// name is about spawning nothing rather than about being dead: that one reports
// Alive, which is what lets the stale-exit guard be tested at all.
type exitedSession struct{}

func (exitedSession) SendTurn([][]byte) error  { return runner.ErrSessionClosed }
func (exitedSession) SendAside([][]byte) error { return runner.ErrSessionClosed }
func (exitedSession) Kill()                    {}
func (exitedSession) Alive() bool              { return false }

// endCursorTurn retires the seat's column the way the protocol does: with the
// vendor's own end-of-turn signal, which on this seat is the room's own
// session/prompt request being answered.
func endCursorTurn(m *Model) {
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true,
	}})
}

// TestTheCursorSeatKeepsOneProcessAcrossTurns is §9.36's headline as an
// assertion: three briefs, one process.
//
// Under print mode this was three children and roughly 24 seconds of pure
// startup. The measured replacement is one handshake and three requests.
func TestTheCursorSeatKeepsOneProcessAcrossTurns(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	for i := 0; i < 3; i++ {
		m.st.Draft = "@cursor say something"
		m.dispatch()
		endCursorTurn(m)
	}

	if log.n() != 1 {
		t.Fatalf("%d spawns for three turns, want 1 — the seat is back on the per-turn path: %+v",
			log.n(), log.specs)
	}
	if p := m.procs[model.VendorCursor]; p == nil {
		t.Fatal("the seat has no process after three turns")
	} else if p.sent != 3 {
		t.Errorf("the process was handed %d turns, want 3", p.sent)
	}
}

// TestTheCursorSeatIsBriefedOncePerProcess, not once per turn.
//
// Per PROCESS rather than per room, which is the rule seatProc.sent already
// carries for the stream-json seat: a seat whose process was replaced is a
// stranger again and has to be briefed like the original was, and a per-vendor
// flag would remember a briefing that happened in a session that no longer
// exists.
func TestTheCursorSeatIsBriefedOncePerProcess(t *testing.T) {
	countSpawns(t)
	m := flowRoom(t, true)
	m.brief = Brief{Path: "brief.md", Text: "the operating context"}

	m.st.Draft = "@cursor first"
	m.dispatch()
	endCursorTurn(m)
	m.st.Draft = "@cursor second"
	m.dispatch()
	endCursorTurn(m)

	if p := m.procs[model.VendorCursor]; p == nil || p.sent != 2 {
		t.Fatalf("proc = %+v, want two turns on one process", p)
	}
}

// TestAMovedRoomReplacesTheCursorSeatToo.
//
// The ACP seat COULD have followed a /cd without a respawn — measured, one
// process ran two sessions in two different directories, each reading its own
// file — and it deliberately does not. seatProc's own comment carries the
// argument: what a move costs the user is a new conversation either way, and one
// rule across four seats is worth more than the three seconds.
//
// This test is what would fail if that choice were ever reversed by accident
// rather than on purpose, which is the only way it should be reversible.
func TestAMovedRoomReplacesTheCursorSeatToo(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	m.st.Draft = "@cursor first"
	m.dispatch()
	endCursorTurn(m)

	first := m.procs[model.VendorCursor]
	if first == nil {
		t.Fatal("no process after the first turn")
	}
	m.st.Workspace = t.TempDir()

	m.st.Draft = "@cursor after the move"
	m.dispatch()
	endCursorTurn(m)

	if log.n() != 2 {
		t.Fatalf("%d spawns, want 2 — the moved seat kept a process pinned to the old directory", log.n())
	}
	if m.procs[model.VendorCursor] == first {
		t.Error("the seat is still the process that was launched in the previous workspace")
	}
	if got := m.column(model.VendorCursor).Note; got == "" {
		t.Error("a seat whose history restarted under the user said nothing about it")
	}
}

// TestAFailedCursorSeatDoesNotFallBackToSpawnPerTurn is San's wholesale ruling
// as a test.
//
// A dead or failing ACP seat fails like any seat, under the room's existing
// transient/probation rules. It does NOT quietly reopen the print-mode path,
// because that path is gone: the adapter's batch entry points return
// ErrCursorIsLiveOnly, and a caller that reached for one would surface the error
// in the column rather than silently paying ~13s a turn to route around a bug.
func TestAFailedCursorSeatDoesNotFallBackToSpawnPerTurn(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	m.st.Draft = "@cursor say something"
	m.dispatch()

	// The process dies mid-turn. Reported as dead as well as failing, because
	// the eleventh amendment's guard exists precisely to ignore a terminal event
	// while the seat's current process is still alive — see the test below.
	m.procs[model.VendorCursor].sess = exitedSession{}
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorCursor, Kind: runner.KindError,
		Note: "the ACP server exited", ExitCode: 1,
	}})

	if _, alive := m.procs[model.VendorCursor]; alive {
		t.Error("a dead process is still registered; the next turn would write into it")
	}
	c := m.column(model.VendorCursor)
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want the failure on screen rather than a silent retry", c.Phase)
	}
	// Exactly one spawn: nothing rebuilt the seat behind the failure, and in
	// particular nothing reached for a per-turn child.
	if log.n() != 1 {
		t.Errorf("%d spawns, want 1 — something fell back after the seat failed: %+v", log.n(), log.specs)
	}
	for _, spec := range log.specs {
		if len(spec.Args) != 1 || spec.Args[0] != "acp" {
			t.Errorf("a non-ACP cursor invocation was built: %v", spec.Args)
		}
	}
}

// TestASeatWhoseWireRefusesIsKilledRatherThanKeptAndRetried is the room half of
// the terminal-handshake fix, and the property is a room that can still be used.
//
// An ACP server that refuses `initialize` does not exit, so the stale-exit guard
// reads a live process and the seat is kept. Keeping it is the trap: every later
// brief would be handed to a protocol that has stopped answering, and each turn
// would hang forever. The protocol refuses instead — and a seat whose wire
// refuses is killed and forgotten, so the next brief starts a fresh process,
// which is the only recovery there is when the cause is an auth problem the user
// may have since fixed.
func TestASeatWhoseWireRefusesIsKilledRatherThanKeptAndRetried(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	m.st.Draft = "@cursor first"
	m.dispatch()
	proc := m.procs[model.VendorCursor]
	if proc == nil {
		t.Fatal("no process to fail")
	}
	// The handshake fails: the protocol ends the turn and goes terminal.
	proto := log.protos[0]
	proto.Opening()
	evs, _ := proto.Inbound([]byte(
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"not signed in"}}`))
	m.applyEvents(prefix(evs, model.VendorCursor))

	if m.turn != nil {
		t.Error("the turn survived a handshake that will never answer it")
	}

	// The very next brief gets a WORKING column, not a second error naming a
	// handshake the user cannot see: the dead process is killed and one retry
	// spawns its replacement inside the same dispatch.
	m.st.Draft = "@cursor second"
	m.dispatch()

	if log.n() != 2 {
		t.Fatalf("%d spawns, want 2 — the room kept handing briefs to a dead protocol", log.n())
	}
	if m.procs[model.VendorCursor] == proc {
		t.Error("the refusing process is still the seat's")
	}
	if c := m.column(model.VendorCursor); c.Phase == PhaseFailed {
		t.Errorf("the retry did not happen; the second brief failed too: %q", c.Note)
	}
}

// prefix stamps the vendor onto events a protocol produced, which is what the
// runner's pump does on the way to the channel.
func prefix(evs []runner.Event, v model.VendorID) []runner.Event {
	out := make([]runner.Event, 0, len(evs))
	for _, ev := range evs {
		ev.Vendor = v
		out = append(out, ev)
	}
	return out
}

// TestAStaleExitDoesNotFailTheLiveCursorSeat is the eleventh amendment's guard,
// re-asked for a seat that did not exist when it was written.
//
// A terminal event names a VENDOR, not a process. The room-lifetime channel
// carries a killed predecessor's exit into whatever turn drains it next — and
// acting on it would fail the LIVE turn, drop the live process from procs
// (leaving it running and invisible, which is the exact state this product
// refuses), and discard the earned thread through the probation rule.
func TestAStaleExitDoesNotFailTheLiveCursorSeat(t *testing.T) {
	countSpawns(t)
	m := flowRoom(t, true)

	m.st.Draft = "@cursor say something"
	m.dispatch()
	live := m.procs[model.VendorCursor]
	if live == nil {
		t.Fatal("no live process to protect")
	}

	// The predecessor's exit, arriving late.
	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindDone}})

	if m.procs[model.VendorCursor] != live {
		t.Error("a stale exit dropped the LIVE process from procs")
	}
	if c := m.column(model.VendorCursor); c.Phase == PhaseFailed || c.Phase == PhaseDone {
		t.Errorf("phase = %v — a stale exit retired the live turn", c.Phase)
	}
	if m.turn == nil {
		t.Error("a stale exit ended the turn")
	}
}
