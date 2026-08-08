package council

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// postureModel is a three-seat room opened in WRITE, the posture a plain
// council actually starts in.
//
// Built through stateWith rather than by hand so the badges under test are the
// ones New would have produced. A hand-set Sandbox would let applyPosture pass
// against a fixture nobody ships.
func postureModel(auto bool) *Model {
	opts := Options{Write: true, Auto: auto}
	m := &Model{
		opts:   opts,
		st:     stateWith(opts, false),
		glyphs: GlyphsFor(false),
	}
	return m
}

func press(m *Model, k string) {
	m.key(tea.KeyPressMsg{Code: rune(k[0])})
}

// TestReadMakesTheRoomReadOnlyAtOnce. The tightening direction spends no
// keystroke: /read is the safe one and asking would be friction charged for
// nothing, which is the complaint §9.17 exists to answer.
func TestReadMakesTheRoomReadOnlyAtOnce(t *testing.T) {
	m := postureModel(false)
	m.setDraft("/read")

	if !m.roomCommand() {
		t.Fatal("/read dispatched to the vendors instead of being intercepted")
	}
	if m.st.Write {
		t.Error("the room still writes after /read")
	}
	if m.writePending {
		t.Error("/read armed a confirmation; only /write asks")
	}
	if m.st.Draft != "" {
		t.Errorf("the draft survived the command: %q", m.st.Draft)
	}
}

// TestPostureFlipRebuildsEveryBadge is the honesty property of this whole
// control, and the reason applyPosture exists rather than a bare assignment.
//
// Sandbox is computed once in stateWith from opts.Write. A posture that moved
// without rebuilding the claims would leave four columns advertising authority
// the room had just taken away — a displayed value that no longer comes from
// what is true (§4a.1), and the one class of bug this repo is built to prevent.
// Asserted on the BADGE, the thing a user actually reads, not on the field.
func TestPostureFlipRebuildsEveryBadge(t *testing.T) {
	m := postureModel(true)

	var before []string
	for _, c := range m.st.Columns {
		before = append(before, c.Sandbox.Badge())
	}
	writes := 0
	for _, b := range before {
		if b == "WRITES" {
			writes++
		}
	}
	if writes == 0 {
		t.Fatalf("no column claimed WRITES in a write room; badges were %v", before)
	}

	m.setDraft("/read")
	m.roomCommand()

	for i, c := range m.st.Columns {
		if c.Sandbox.Badge() == "WRITES" {
			t.Errorf("%s still badges WRITES in a read-only room", c.Label)
		}
		if c.Sandbox.Badge() == before[i] && before[i] == "WRITES" {
			t.Errorf("%s's badge did not move at all", c.Label)
		}
	}
}

// TestWriteDoesNotLoosenUntilConfirmed. The asymmetry with /read is the point:
// loosening hands editing and command authority to every seat in the room, so
// the room may not be armed by a draft alone.
func TestWriteDoesNotLoosenUntilConfirmed(t *testing.T) {
	m := postureModel(false)
	m.applyPosture(false)

	m.setDraft("/write")
	if !m.roomCommand() {
		t.Fatal("/write dispatched instead of being intercepted")
	}
	if m.st.Write {
		t.Fatal("/write loosened the room before anyone confirmed")
	}
	if !m.writePending {
		t.Fatal("/write did not arm a confirmation")
	}

	press(m, "y")
	if !m.st.Write {
		t.Error("y did not let the room write")
	}
	if m.writePending {
		t.Error("the confirmation is still pending after y")
	}
	for _, c := range m.st.Columns {
		if b := c.Sandbox.Badge(); b == "" {
			t.Errorf("%s lost its badge entirely on the way back to write", c.Label)
		}
	}
}

// TestOnlyYLoosensTheRoom. n is a decision and anything else is an accident,
// and both land in the same safe place — clearGateKey's rule, for clearGateKey's
// reason: this gate interrupts nothing, so a key nobody meant to press must not
// be able to arm four seats.
func TestOnlyYLoosensTheRoom(t *testing.T) {
	for _, k := range []string{"n", "q", "j", "Y"} {
		m := postureModel(false)
		m.applyPosture(false)
		m.setDraft("/write")
		m.roomCommand()

		press(m, k)

		if m.st.Write {
			t.Errorf("%q loosened the room; only y may", k)
		}
		if m.writePending {
			t.Errorf("%q left the confirmation armed", k)
		}
	}
}

// TestBareWordOnly is the vocabulary rule, and it is the reason these two
// commands parse differently from /cd and /trace.
//
// The rule this pins is that NONE of these runs a setting. "/write a test for
// this" and "/read the design doc first" are ordinary briefs a person addresses
// a room with, and intercepting them as the posture command would silently
// swallow a turn and change the room instead — the user watching their brief
// vanish rather than being told anything.
//
// What §9.31 changed is where they go instead, and it is deliberately not the
// vendors: a draft opening with a slash is refused with the space escape named,
// so a slip costs a notice rather than three seats' quota. Both halves are
// asserted here, in one test, because the failure to avoid is either of them
// alone — a "/read the design doc" that ran the setting, or one that was billed.
func TestBareWordOnly(t *testing.T) {
	for _, draft := range []string{
		"/write a test for this",
		"/read the design doc first",
		"/writes",
		"/reading list",
	} {
		m := postureModel(false)
		was := m.st.Write
		m.setDraft(draft)

		if !m.roomCommand() {
			t.Errorf("%q was dispatched to the vendors; a slash-leading slip is refused", draft)
		}
		if m.writePending {
			t.Errorf("%q armed the write gate", draft)
		}
		if m.st.Write != was {
			t.Errorf("%q moved the room's posture", draft)
		}
		if m.st.Draft != draft {
			t.Errorf("%q was altered rather than handed back: %q", draft, m.st.Draft)
		}
		if !strings.Contains(m.st.Notice, "leading space") {
			t.Errorf("%q was refused without naming the way to send it: %q", draft, m.st.Notice)
		}
	}
}

// TestPostureRefusesMidTurn. Posture is argv, fixed at spawn, so seats already
// running hold the flags they were launched with whatever this sets. Letting the
// flip land mid-turn would put a read-only badge over a live process still
// holding write flags — the exact disagreement between claim and process that
// persistent.go's respawn exists to prevent.
func TestPostureRefusesMidTurn(t *testing.T) {
	for _, draft := range []string{"/read", "/write"} {
		m := postureModel(false)
		m.turn = &turnState{}

		m.setDraft(draft)
		if !m.roomCommand() {
			t.Fatalf("%s dispatched during a turn", draft)
		}
		if !m.st.Write {
			t.Errorf("%s moved the posture while a turn was in flight", draft)
		}
		if m.writePending {
			t.Errorf("%s armed its gate while a turn was in flight", draft)
		}
		if m.st.Draft != draft {
			t.Errorf("the refused draft was discarded: %q", m.st.Draft)
		}
		if !strings.Contains(m.st.Notice, "in flight") {
			t.Errorf("the refusal did not say why: %q", m.st.Notice)
		}
	}
}

// TestTheCardNamesWhichWriteYouGet. An ungated room and a gated one reach the
// same badge-bearing posture by different routes, and only one of them asks
// before acting. A card promising "claude asks first" in an ungated room would
// be a promise that room cannot keep — the honesty rule applied to a
// confirmation prompt rather than to a gauge.
func TestTheCardNamesWhichWriteYouGet(t *testing.T) {
	gated := postureModel(false)
	gated.applyPosture(false)
	gated.setDraft("/write")
	gated.roomCommand()

	auto := postureModel(true)
	auto.applyPosture(false)
	auto.setDraft("/write")
	auto.roomCommand()

	if gated.st.Notice == auto.st.Notice {
		t.Fatal("a gated room and an ungated room offer the same card; only one of them asks")
	}
	if !strings.Contains(gated.st.Notice, "asks before") {
		t.Errorf("the gated card does not say the seat asks: %q", gated.st.Notice)
	}
	if !strings.Contains(auto.st.Notice, "nothing will ask") {
		t.Errorf("the ungated card does not say nothing will ask: %q", auto.st.Notice)
	}
	// The wording no longer credits the flag, because the flag is no longer the
	// only way into this state — `a` is. A card blaming --auto in a room that
	// was gated at launch names a cause that is not there.
	if strings.Contains(auto.st.Notice, "--auto") {
		t.Errorf("the card still credits --auto for a state `a` can also reach: %q", auto.st.Notice)
	}
}

// TestTheCardReadsTheRoomAndNotTheFlag. The regression this pins: /write's card
// read m.opts.Auto, which is only the LAUNCH seed for the gate (stateWith), while
// `a` has moved m.st.GateOff ever since §9.17's last control landed. So a room
// opened gated, told to stop asking, then flipped /read → /write handed back a
// card promising "claude asks before each change" with nothing left to ask —
// the promise the sibling test exists to forbid, made in the direction that
// costs the user rather than merely misinforming them.
//
// dispatch.go already states the rule for the REQUEST path ("m.st.Asking, not
// m.opts.Auto: the flag only SEEDS this at launch"); this is the same rule on the
// confirmation path, and it is a test rather than a comment because the two
// fields agree in every room nobody pressed `a` in — which is every fixture.
func TestTheCardReadsTheRoomAndNotTheFlag(t *testing.T) {
	// Opened GATED: the flag says the seat asks.
	m := postureModel(false)
	if !m.st.Asking() {
		t.Fatal("a room built without --auto did not start asking")
	}

	// `a` in view mode is the whole point — the gate moves without the flag.
	m.toggleAsking()
	if m.st.Asking() {
		t.Fatal("`a` did not stop the room asking")
	}

	m.applyPosture(false)
	m.setDraft("/write")
	m.roomCommand()

	if strings.Contains(m.st.Notice, "asks before") {
		t.Errorf("the card promises a seat will ask in a room that has stopped asking: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "nothing will ask") {
		t.Errorf("the card does not say nothing will ask: %q", m.st.Notice)
	}

	// And the other direction, which is the same bug wearing the safer face: a
	// room opened --auto and then told to ask again must not go on advertising
	// that nothing will.
	back := postureModel(true)
	back.toggleAsking()
	if !back.st.Asking() {
		t.Fatal("`a` did not turn asking back on in an --auto room")
	}
	back.applyPosture(false)
	back.setDraft("/write")
	back.roomCommand()

	if !strings.Contains(back.st.Notice, "asks before") {
		t.Errorf("an --auto room that was told to ask again still offers the ungated card: %q", back.st.Notice)
	}
}

// TestFlippingToThePostureYouAreInChangesNothing. Told, rather than handed a
// confirmation whose y does nothing — askClearSeat's rule for an empty seat, and
// the same reasoning: a card that no-ops teaches that the key is unreliable.
func TestFlippingToThePostureYouAreInChangesNothing(t *testing.T) {
	m := postureModel(false)
	m.setDraft("/write")
	m.roomCommand()

	if m.writePending {
		t.Error("/write in a writing room armed a confirmation for nothing")
	}
	if !m.st.Write {
		t.Error("/write in a writing room took writing away")
	}
	if !strings.Contains(m.st.Notice, "already") {
		t.Errorf("the notice did not say the room was already there: %q", m.st.Notice)
	}
}
