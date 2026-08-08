package council

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheZeroStateAsks is the polarity, pinned.
//
// The obvious field was `Asking bool`, whose zero value is "does not ask" — so
// every State built as a literal would have silently been an ungated room. That
// is not a style preference: it is a safety property whose default was off, and
// it was caught only because five existing gate tests build their State by hand
// and went green while asserting nothing. Stored negated, the zero value is the
// guarded room.
func TestTheZeroStateAsks(t *testing.T) {
	if !(State{}).Asking() {
		t.Fatal("a zero State does not ask before a change; the gate defaults off")
	}
	if !room().Asking() {
		t.Error("a room built by the test helper does not ask")
	}
}

// askModel is a write room with one gate already waiting, which is the state
// the `a` key is actually pressed in.
func askModel(queued int) *Model {
	m := &Model{
		st:         room(),
		glyphs:     GlyphsFor(false),
		opts:       Options{Write: true},
		gateInputs: map[string]map[string]any{},
		procs:      map[model.VendorID]*seatProc{},
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
	}
	m.st.Write = true
	m.procs[model.VendorClaude] = &seatProc{sess: &killSession{}}
	for i := 0; i < queued; i++ {
		m.st.Gates = append(m.st.Gates, PendingGate{
			Vendor:    model.VendorClaude,
			RequestID: "r" + itoa(i),
			ToolUseID: "t" + itoa(i),
			Text:      "Write: internal/council/gate.go",
		})
		m.gateInputs["r"+itoa(i)] = map[string]any{}
	}
	return m
}

// TestStopAskingClearsTheWholeQueue. The cards behind the current one are the
// same question asked again; leaving them standing would make `a` mean "stop
// asking after these four", which is not what anyone presses it for.
func TestStopAskingClearsTheWholeQueue(t *testing.T) {
	m := askModel(4)
	m.key(tea.KeyPressMsg{Code: 'a'})

	if m.st.Asking() {
		t.Error("the room is still asking after a")
	}
	if len(m.st.Gates) != 0 {
		t.Errorf("%d cards left standing after a", len(m.st.Gates))
	}
	if m.st.Gating() {
		t.Error("the room still reports itself as gating")
	}
	if !strings.Contains(m.st.Notice, "4") {
		t.Errorf("the notice did not say how many calls it approved: %q", m.st.Notice)
	}
}

// TestStopAskingApprovesRatherThanDiscards. A pending gate is a vendor STOPPED
// mid-call — queueGate's own rule is that nothing may quietly drop a request —
// so a dropped queue would leave columns waiting forever with no card left to
// explain why.
func TestStopAskingApprovesRatherThanDiscards(t *testing.T) {
	m := askModel(3)
	sess := m.procs[model.VendorClaude].sess.(*killSession)

	m.key(tea.KeyPressMsg{Code: 'a'})

	if len(sess.sent) != 3 {
		t.Fatalf("sent %d decisions for 3 blocked calls; a request was dropped", len(sess.sent))
	}
	for _, c := range m.st.Columns {
		for _, act := range c.Acts {
			if act.Status == runner.ActDenied {
				t.Error("a recorded a denial; it approves")
			}
		}
	}
}

// TestStopAskingRebuildsTheBadge. The gated badge claims "this column asks
// before every tool call". The moment it stops, that claim is false — the same
// honesty property /read and /write have, reached by a different key.
func TestStopAskingRebuildsTheBadge(t *testing.T) {
	m := askModel(1)
	m.applyPosture(true)

	gatedBefore := false
	for _, c := range m.st.Columns {
		if c.Sandbox.Level == SandboxGated {
			gatedBefore = true
		}
	}
	if !gatedBefore {
		t.Fatal("no column badged gated in an asking write room")
	}

	m.key(tea.KeyPressMsg{Code: 'a'})

	for _, c := range m.st.Columns {
		if c.Sandbox.Level == SandboxGated {
			t.Errorf("%s still badges gated in a room that stopped asking", c.Label)
		}
	}
}

// TestAskingIsNotAOneWayDoor. A control that could only ever be turned off would
// be the §9.17 defect rebuilt one key later: a decision you make once, in one
// direction, and then relaunch to undo.
func TestAskingIsNotAOneWayDoor(t *testing.T) {
	m := askModel(1)
	m.applyPosture(true)
	m.key(tea.KeyPressMsg{Code: 'a'}) // card is up: stop asking
	if m.st.Asking() {
		t.Fatal("a did not stop the asking")
	}

	m.key(tea.KeyPressMsg{Code: 'a'}) // no card now: toggle back
	if !m.st.Asking() {
		t.Fatal("a in view mode did not turn asking back on")
	}
	gated := false
	for _, c := range m.st.Columns {
		if c.Sandbox.Level == SandboxGated {
			gated = true
		}
	}
	if !gated {
		t.Error("asking came back but no column badges gated again")
	}
}

// TestTheFooterSaysWhenNothingIsAsking. `a` on the card is announced twice while
// a card is up, and then the card is gone. Without this cell the room would sit
// permanently ungated with the way back documented nowhere on screen.
func TestTheFooterSaysWhenNothingIsAsking(t *testing.T) {
	st := room()
	st.Write = true
	st.Mode = ModeViewing
	if strings.Contains(render(st), "not asking") {
		t.Fatal("an asking room advertises the cell that says it is not")
	}

	st.GateOff = true
	if !strings.Contains(render(st), "not asking") {
		t.Error("a room that stopped asking says nothing about it on the footer")
	}
}

// TestAnUngatedRoomSpawnsUngated. seatPosture must read the room's state and not
// the launch flag, or `a` would leave the seat spawning with gate flags and
// raising cards nobody is answering — a gate whose only effect is to block.
func TestAnUngatedRoomSpawnsUngated(t *testing.T) {
	m := askModel(0)
	m.applyPosture(true)
	if got := m.seatPosture(); got != vendors.PostureWriteGated {
		t.Fatalf("an asking write room spawns %v, want write-gated", got)
	}

	m.st.GateOff = true
	if got := m.seatPosture(); got == vendors.PostureWriteGated {
		t.Error("a room that stopped asking still spawns the gated invocation")
	}
}

// TestQueueGateStopsAskingImmediately. `a` pressed mid-turn has to hold for the
// REST of that turn: the running process keeps the gate flags it was spawned
// with, so it goes on sending requests. If those queued, "stop asking" would
// keep asking until the turn ended — the promise broken at the moment it was
// made.
func TestQueueGateStopsAskingImmediately(t *testing.T) {
	m := askModel(0)
	m.st.GateOff = true
	c := m.column(model.VendorClaude)

	m.queueGate(c, &runner.Gate{
		RequestID: "live", ToolUseID: "t-live",
		Tool: "Write", Text: "Write: some/file.go",
		Input: map[string]any{},
	})

	if len(m.st.Gates) != 0 {
		t.Error("a request queued a card in a room that had stopped asking")
	}
	if sent := m.procs[model.VendorClaude].sess.(*killSession).sent; len(sent) != 1 {
		t.Errorf("the request was not answered: %d decisions sent", len(sent))
	}
}

// TestAskingBackOnInAReadRoomPromisesNothing. There is nothing to ask about in a
// room that cannot write, so reporting "the seat asks again" would promise a
// card that cannot arrive.
func TestAskingBackOnInAReadRoomPromisesNothing(t *testing.T) {
	m := askModel(0)
	m.applyPosture(false)
	m.st.GateOff = true

	m.toggleAsking()

	if !strings.Contains(m.st.Notice, "/write") {
		t.Errorf("a read-only room did not say why no card will arrive: %q", m.st.Notice)
	}
}
