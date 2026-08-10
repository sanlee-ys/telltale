package council

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// replied builds a room mid-conversation: turn 3, three seats that answered,
// one older turn behind them.
func replied() State {
	st := room()
	st.Turn = 3
	for i := range st.Columns {
		c := &st.Columns[i]
		c.History = []TurnRecord{{N: 2, Prompt: "an older question", Body: "an older answer from " + c.Label}}
		c.TurnN = 3
		c.Prompt = "what does resume cost by turn five?"
		c.Phase = PhaseDone
	}
	st.Columns[0].Body = "About 30K redundant input tokens per vendor."
	st.Columns[1].Body = "Roughly quadratic in the transcript."
	st.Columns[2].Body = "Native resume avoids it entirely."
	return st
}

// TestYankTakesTheFocusedSeatsReply. The key addresses the focused column — the
// same column every scroll key addresses — because a copy key that took from
// somewhere other than where the eye is would be §9.12's failure with a
// clipboard attached.
func TestYankTakesTheFocusedSeatsReply(t *testing.T) {
	st := replied()
	st.Focus = 1

	y := st.YankColumn(st.Focus)
	if y.Text != "Roughly quadratic in the transcript." {
		t.Errorf("Text = %q, want the focused seat's reply", y.Text)
	}
	// Never a neighbour's. Three answers side by side is the whole product, and
	// silently copying the wrong one is unrecoverable once it is in a document.
	for _, other := range []string{"30K redundant", "Native resume"} {
		if strings.Contains(y.Text, other) {
			t.Errorf("the yank picked up another seat's reply: %q", y.Text)
		}
	}
	// And not the older turn sitting in this seat's history.
	if strings.Contains(y.Text, "older answer") {
		t.Errorf("the yank reached back past the current turn: %q", y.Text)
	}
	if !strings.Contains(y.Notice, "Codex") || !strings.Contains(y.Notice, "turn-3") {
		t.Errorf("Notice = %q, want it to name the seat and the turn", y.Notice)
	}
}

// TestYankFallsBackToTheLastThingThisSeatSaid.
//
// "The last answer" is what a user means by this key. A seat that has just been
// asked a new question has not stopped having answered the old one, and a copy
// key that went empty for the whole of a slow turn would be useless in exactly
// the window where someone reaches for it.
func TestYankFallsBackToTheLastThingThisSeatSaid(t *testing.T) {
	st := replied()
	st.Turn = 4
	st.Columns[0].TurnN = 4
	st.Columns[0].Body = ""
	st.Columns[0].Phase = PhaseWaiting

	y := st.YankColumn(0)
	if !strings.Contains(y.Text, "an older answer from Claude Code") {
		t.Errorf("Text = %q, want the newest turn that actually said something", y.Text)
	}
	// And the notice names the turn it CAME from, not the one on screen. A line
	// saying "turn-4 reply" over turn 2's text is the room misreporting what it
	// just put on someone's clipboard.
	if !strings.Contains(y.Notice, "turn-2") {
		t.Errorf("Notice = %q, want the turn the text is actually from", y.Notice)
	}
}

// TestYankOnASilentSeatCopiesNothingAtAll.
//
// Writing "" through OSC 52 is the documented way to CLEAR a clipboard, so a
// copy key that found nothing must produce no command at all. "Nothing
// happened" and "your clipboard is now empty" are different outcomes and this
// key must never spell them the same way.
func TestYankOnASilentSeatCopiesNothingAtAll(t *testing.T) {
	st := room()
	y := st.YankColumn(0)
	if !y.Empty() {
		t.Errorf("Text = %q, want nothing for a seat that never answered", y.Text)
	}
	if y.Notice == "" {
		t.Error("a key that did nothing said nothing — the user cannot tell it was pressed")
	}

	m := &Model{st: st}
	if cmd := m.yank(y); cmd != nil {
		t.Error("an empty yank still issued a clipboard write, which would CLEAR the clipboard")
	}
}

// TestYankTurnLabelsEverySeatAndCarriesTheBrief.
//
// The format has one job: be pasteable into a document a week later. Four
// answers to a question the file does not contain are unreadable, so the brief
// goes in — the user's own words, which §9.9 already echoes un-redacted on the
// user's own screen for the same reason.
func TestYankTurnLabelsEverySeatAndCarriesTheBrief(t *testing.T) {
	st := replied()
	y := st.YankTurn()

	if !strings.Contains(y.Text, "what does resume cost by turn five?") {
		t.Errorf("the turn yank dropped the brief:\n%s", y.Text)
	}
	for _, c := range st.Columns {
		if !strings.Contains(y.Text, "## "+c.Label) {
			t.Errorf("no header for %s:\n%s", c.Label, y.Text)
		}
	}
	for _, want := range []string{"30K redundant", "Roughly quadratic", "Native resume"} {
		if !strings.Contains(y.Text, want) {
			t.Errorf("the turn yank dropped a reply containing %q", want)
		}
	}
	// Every seat's answer, and the OLDER turn's answers are not among them.
	if strings.Contains(y.Text, "older answer") {
		t.Errorf("the turn yank swept in a previous turn:\n%s", y.Text)
	}
	if !strings.Contains(y.Notice, "turn 3") || !strings.Contains(y.Notice, "3 seats") {
		t.Errorf("Notice = %q, want the turn and how many seats", y.Notice)
	}
}

// TestYankTurnSkipsASeatThatSatOut.
//
// Routing means turn 3 can go to two seats while the third still holds turn 2's
// reply (§9.9). Pasting that under a turn-3 heading would be the room inventing
// a conversation into a document, where it outlives every chance to notice.
func TestYankTurnSkipsASeatThatSatOut(t *testing.T) {
	st := replied()
	st.Columns[2].TurnN = 2
	st.Columns[2].Body = "an answer to something else"

	y := st.YankTurn()
	if strings.Contains(y.Text, "an answer to something else") {
		t.Errorf("a seat that sat out turn 3 was filed under it:\n%s", y.Text)
	}
	if strings.Contains(y.Text, "## Antigravity") {
		t.Errorf("a seat that sat out got a heading with nothing under it:\n%s", y.Text)
	}
	if !strings.Contains(y.Notice, "2 seats") {
		t.Errorf("Notice = %q, want the count of seats that actually took the turn", y.Notice)
	}
}

// TestAPendingGateStillGetsTheY is the collision, pinned.
//
// `y` approves a tool call a vendor is BLOCKED on, and it is now also the copy
// key. If yank ever won that race, a keystroke the user believes approved a
// write would quietly copy text while the vendor sat there — the most expensive
// misfire available in this keymap, because the user's next move is to press it
// again.
func TestAPendingGateStillGetsTheY(t *testing.T) {
	// The second half asserts a clipboard COMMAND, so it needs the fallback
	// mechanism — see stubNoNativeClipboard. What is under test is the routing
	// (gate first, copy after), not which helper the copy lands in.
	stubNoNativeClipboard(t)
	m := &Model{st: replied(), gateInputs: map[string]map[string]any{}}
	m.st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Write: internal/council/gate.go",
	}}

	_, cmd := m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if len(m.st.Gates) != 0 {
		t.Fatal("y did not answer the pending gate — the approve key was stolen by yank")
	}
	if cmd != nil {
		t.Error("y issued a clipboard write while a vendor was blocked on it")
	}
	if strings.Contains(m.st.Notice, "copied") {
		t.Errorf("the room reported a copy for a keystroke that approved a tool call: %q", m.st.Notice)
	}

	// With the queue drained, the same key is the copy key again. The precedence
	// is a routing rule, not a disabled feature.
	_, cmd = m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Error("y stopped copying after the gate cleared")
	}
	if !strings.Contains(m.st.Notice, "copied") {
		t.Errorf("Notice = %q, want the copy confirmation", m.st.Notice)
	}
}

// TestYankIsTextInCompose. In compose mode y is the letter y, the same rule
// that keeps q the letter q there (§9.10).
func TestYankIsTextInCompose(t *testing.T) {
	m := &Model{st: replied()}
	m.st.Mode = ModeComposing

	_, cmd := m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Error("y copied to the clipboard while the user was typing a brief")
	}
	if m.st.Draft != "y" {
		t.Errorf("Draft = %q, want the letter y", m.st.Draft)
	}
}

// TestYankEmitsAClipboardCommand is as far as a test can reach ON THE FALLBACK
// PATH, and it now says which path it is testing.
//
// tea.SetClipboard returns a Cmd whose message the program turns into an OSC 52
// write; whether the TERMINAL honours that sequence is unobservable from here,
// and is labelled as inferred wherever it is claimed. What is checkable is that
// the key produces the command carrying the right text.
//
// The stub is what keeps this honest on a machine that has a native helper. On
// macOS `y` now copies through pbcopy and emits NO command at all — this test
// passed for two days on Windows while the key did nothing on the Mac, because
// it asserted the artifact rather than the effect. Forcing the fallback keeps
// the OSC 52 path covered everywhere instead of only where it is the default.
func TestYankEmitsAClipboardCommand(t *testing.T) {
	stubNoNativeClipboard(t)
	m := &Model{st: replied()}
	m.st.Focus = 2

	_, cmd := m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil {
		t.Fatal("y produced no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("the clipboard command produced no message")
	}
	if !strings.Contains(m.st.Notice, "Antigravity") {
		t.Errorf("Notice = %q, want the focused seat", m.st.Notice)
	}
}
