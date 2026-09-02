package council

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sanlee-ys/telltale/internal/model"
)

// This file holds the paste feature to its one promise: a paste lands in the
// draft WHOLE and never sends, whatever it carries. Every test drives the model
// through Update with the message bubbletea v2.0.8 actually delivers for a
// bracketed paste — one tea.PasteMsg with the full content, newlines inside —
// and asserts the observables this repo's security tests already trust: the
// spawn count, the draft, and which questions are still pending. None of them
// assert that a helper returned a value.
//
// What no test here can reach: whether Windows Terminal actually brackets the
// paste. That is the terminal's half of the contract, and the honest check is
// a live one — paste a three-line snippet into the room and count insertions
// (one) and dispatches (zero). See design.md §9.38.

// paste drives the model exactly as the runtime does: through Update, so these
// tests also pin that the PasteMsg case is wired into the switch at all — the
// original defect was precisely that it was not.
func paste(m *Model, content string) {
	m.Update(tea.PasteMsg{Content: content})
}

// TestAPasteLandsWholeAndSendsNothing is the headline property. A multiline
// paste is ONE insertion — its newlines land as newlines in the draft, no
// dispatch fires, and the room is still composing when it is over. Enter, and
// only enter, sends.
func TestAPasteLandsWholeAndSendsNothing(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing

	paste(m, "line one\nline two\nline three")

	if got, want := m.st.Draft, "line one\nline two\nline three"; got != want {
		t.Errorf("draft = %q, want %q", got, want)
	}
	if log.n() != 0 {
		t.Fatalf("a paste spawned %d process(es): %+v", log.n(), log.specs)
	}
	if m.anyInFlight() {
		t.Error("a paste put a turn in flight")
	}
	if m.st.Mode != ModeComposing {
		t.Error("a paste changed the mode away from compose")
	}
}

// TestAWindowsPasteKeepsItsLinesAndLosesItsCRs. The Windows clipboard ends
// every line \r\n; sanitize()'s per-rune treatment would turn that into a
// trailing space on every pasted line. The paste filter collapses the pair —
// and a bare \r — to one \n, so the draft holds the lines the operator copied
// and nothing they did not write.
func TestAWindowsPasteKeepsItsLinesAndLosesItsCRs(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing

	paste(m, "first\r\nsecond\rthird\r\n")

	if got, want := m.st.Draft, "first\nsecond\nthird\n"; got != want {
		t.Errorf("draft = %q, want %q", got, want)
	}
}

// TestPasteAppendsAtTheCaret. The composer's caret is always the end of the
// draft — backspace deletes there, typed text lands there — so a paste into a
// draft already holding text continues it rather than replacing it.
func TestPasteAppendsAtTheCaret(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.setDraft("explain ")

	paste(m, "this stack trace")

	if got, want := m.st.Draft, "explain this stack trace"; got != want {
		t.Errorf("draft = %q, want %q", got, want)
	}
}

// TestControlCharactersInAPasteNeverAct. Pasted bytes that LOOK like keys must
// not behave like keys: a \x03 is not ctrl+c, an ESC is not the escape key, a
// pasted q is the letter q. The paste arrives in view mode — where every one of
// those characters has a meaning as a keystroke — and the only thing any of
// them may do is be text or be dropped.
func TestControlCharactersInAPasteNeverAct(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Mode = ModeViewing

	paste(m, "y\x03n\x1b[2Jq\x7f done\ta.go")

	if m.roomCtx.Err() != nil {
		t.Fatal("a pasted control character tore the room down")
	}
	if log.n() != 0 {
		t.Fatalf("a pasted control character spawned %d process(es)", log.n())
	}
	// \x03, \x1b and \x7f drop (no width, and possibly a control); \t is one
	// space; the letters — including the ones that are commands when typed —
	// are just letters.
	if got, want := m.st.Draft, "yn[2Jq done a.go"; got != want {
		t.Errorf("draft = %q, want %q", got, want)
	}
}

// TestPasteFromViewModeOpensTheComposer. A paste is material, not a keystroke,
// and the only place material goes is the draft — so a paste offered to a room
// in view mode lands there and the room switches to compose, stating the switch
// on the mode line rather than making the operator press i and paste twice.
func TestPasteFromViewModeOpensTheComposer(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeViewing
	m.st.Help = HelpKeys

	paste(m, "review the diff")

	if m.st.Mode != ModeComposing {
		t.Error("the room did not switch to compose for the pasted draft")
	}
	if m.st.Help != HelpClosed {
		t.Error("the help panel stayed open over the composer, unlike the i key's path")
	}
	if got, want := m.st.Draft, "review the diff"; got != want {
		t.Errorf("draft = %q, want %q", got, want)
	}
}

// TestAnOversizePasteRefusesByName. The refusal is atomic — nothing lands, not
// a truncated prefix — and it carries both numbers, because "too big" without
// the sizes cannot tell a near miss from a 2 MB accident.
func TestAnOversizePasteRefusesByName(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing

	paste(m, strings.Repeat("x", maxPasteRunes+1))

	if m.st.Draft != "" {
		t.Fatalf("a refused paste left %d chars behind", len(m.st.Draft))
	}
	for _, want := range []string{"paste refused", itoa(maxPasteRunes + 1), itoa(maxPasteRunes)} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the refusal does not say %q: %q", want, m.st.Notice)
		}
	}
}

// TestTheCapCountsTheDraftTooAndSaysSo. The cap is over draft-plus-paste, or
// two half-cap pastes would land where one full-cap paste refuses — and when
// the draft is part of the answer, the refusal names its share.
func TestTheCapCountsTheDraftTooAndSaysSo(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.setDraft("context: ")

	paste(m, strings.Repeat("x", maxPasteRunes-5))

	if got, want := m.st.Draft, "context: "; got != want {
		t.Fatalf("a refused paste changed the draft: %q", got)
	}
	for _, want := range []string{"paste refused", itoa(maxPasteRunes - 5), "9 already in the draft"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the refusal does not say %q: %q", want, m.st.Notice)
		}
	}
}

// TestPasteWhileAQuestionIsPendingIsRefused. A pending y/n outranks everything
// (key()'s own routing rule), and a paste must neither answer the question nor
// slip text in underneath it. The question stays pending, the draft stays as it
// was, and the refusal says the paste was not inserted.
func TestPasteWhileAQuestionIsPendingIsRefused(t *testing.T) {
	cases := map[string]func(m *Model) func() bool{
		"tool gate": func(m *Model) func() bool {
			m.st.Gates = []PendingGate{{Vendor: model.VendorClaude, RequestID: "req-1", Text: "Write: a.go"}}
			return func() bool { return m.st.Gating() }
		},
		"clear seat": func(m *Model) func() bool {
			m.clearPending = model.VendorClaude
			return func() bool { return m.clearPending != "" }
		},
		"/write": func(m *Model) func() bool {
			m.writePending = true
			return func() bool { return m.writePending }
		},
		"flow write hop": func(m *Model) func() bool {
			m.flowWritePending = true
			return func() bool { return m.flowWritePending }
		},
	}
	for name, arm := range cases {
		m := flowRoom(t, true)
		m.st.Mode = ModeComposing
		m.setDraft("half a brief")
		stillPending := arm(m)

		paste(m, "stray context")

		if got, want := m.st.Draft, "half a brief"; got != want {
			t.Errorf("%s: the paste reached the draft: %q", name, got)
		}
		if !stillPending() {
			t.Errorf("%s: the paste answered or dropped the pending question", name)
		}
		if !strings.Contains(m.st.Notice, "not inserted") {
			t.Errorf("%s: the refusal does not say the paste was not inserted: %q", name, m.st.Notice)
		}
	}
}

// TestAnEmptyPasteChangesNothing. An empty bracketed paste is terminal noise;
// it must not switch modes, set a notice, or disturb the draft.
func TestAnEmptyPasteChangesNothing(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeViewing

	paste(m, "")

	if m.st.Mode != ModeViewing {
		t.Error("an empty paste opened the composer")
	}
	if m.st.Draft != "" || m.st.Notice != "" {
		t.Errorf("an empty paste left a mark: draft=%q notice=%q", m.st.Draft, m.st.Notice)
	}
}

// TestEnterAfterAPasteSendsTheRealNewlines closes the loop end to end: the
// brief the seat is handed is the pasted text with its line structure intact —
// asserted off the column's recorded prompt, the same string the vendor was
// handed, because the default route is a persistent seat and takes its prompt
// on stdin.
func TestEnterAfterAPasteSendsTheRealNewlines(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing

	paste(m, "check this trace:\r\n  step two failed\r\nsay why")
	enter(m)

	if log.n() == 0 {
		t.Fatalf("the pasted brief was not dispatched: %q", m.st.Notice)
	}
	c := m.column(model.VendorClaude)
	if c == nil || c.TurnN == 0 {
		t.Fatal("the pasted brief reached no seat")
	}
	if got, want := c.Prompt, "check this trace:\n  step two failed\nsay why"; got != want {
		t.Errorf("the seat was handed %q, want %q — the paste was rewritten on the way", got, want)
	}
}
