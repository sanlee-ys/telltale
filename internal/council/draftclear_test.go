package council

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sanlee-ys/telltale/internal/model"
)

// This file holds ctrl+u to its one promise: it clears the DRAFT and nothing
// else — not a gate, not the mode, not a thread — and it states the loss with
// a measured count. Every test drives the model through Update with the real
// chord (key("ctrl+u") builds Code:'u' Mod:ModCtrl, what the decoder actually
// delivers), so the routing under test is key()'s own: gates first, then mode.
//
// What no test here can reach: a terminal that swallows ctrl+u before council
// sees it (some shells bind it at the line-discipline layer, which does not
// apply inside a bubbletea alt-screen program but is the honest caveat). The
// live check is one keystroke at the real machine.

func ctrlU(m *Model) {
	m.Update(key("ctrl+u"))
}

// TestCtrlUClearsAPastedDraftAndSaysHowMuch is the headline: the multiline
// draft a paste built goes in one keystroke, the notice carries the rune count
// of exactly the string that was dropped, and nothing is dispatched, torn down
// or switched by the act.
func TestCtrlUClearsAPastedDraftAndSaysHowMuch(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing

	paste(m, "@codex review this trace:\r\n  step two failed\r\nsay why")
	dropped := utf8.RuneCountInString(m.st.Draft)
	if dropped == 0 {
		t.Fatal("the fixture paste never landed")
	}

	ctrlU(m)

	if m.st.Draft != "" {
		t.Errorf("the draft survived ctrl+u: %q", m.st.Draft)
	}
	for _, want := range []string{"draft cleared", itoa(dropped) + " chars"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the notice does not say %q: %q", want, m.st.Notice)
		}
	}
	if m.st.Mode != ModeComposing {
		t.Error("ctrl+u changed the mode away from compose")
	}
	if log.n() != 0 || m.anyInFlight() {
		t.Errorf("ctrl+u put work in flight: %d spawn(s)", log.n())
	}
	// The routing indicator falls with the draft it describes: "@codex" is
	// gone, so a footer still reading "→ codex" would promise a dispatch the
	// draft can no longer make.
	if want, _ := ParseRoute(""); !reflect.DeepEqual(m.st.Route, want) {
		t.Errorf("the route did not reset with the draft: %+v", m.st.Route)
	}
}

// TestCtrlUCountsRunesNotBytes. The count is measured off the string dropped,
// in pasteRefusal's own unit — runes said as "chars" — so a draft of multibyte
// text reports what the operator lost, not what UTF-8 spent encoding it.
func TestCtrlUCountsRunesNotBytes(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.setDraft("κ→λ") // 3 runes, 7 bytes

	ctrlU(m)

	if !strings.Contains(m.st.Notice, "3 chars") {
		t.Errorf("the notice does not carry the rune count: %q", m.st.Notice)
	}
}

// TestCtrlUSaysCharForOneRune pins the plural the same way the room's other
// counted notices do: "1 char", not "1 chars".
func TestCtrlUSaysCharForOneRune(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.setDraft("x")

	ctrlU(m)

	if !strings.Contains(m.st.Notice, "1 char") || strings.Contains(m.st.Notice, "1 chars") {
		t.Errorf("the one-rune notice reads wrong: %q", m.st.Notice)
	}
}

// TestCtrlUOnAnEmptyDraftIsSilent is backspace's precedent applied: the key
// deletes nothing, clears any stale notice, and does not manufacture a
// "nothing to clear" sentence — after the press, the state ctrl+u promises is
// already the state on screen.
func TestCtrlUOnAnEmptyDraftIsSilent(t *testing.T) {
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.st.Notice = "a stale notice from the last act"

	ctrlU(m)

	if m.st.Draft != "" {
		t.Errorf("an empty clear grew a draft: %q", m.st.Draft)
	}
	if m.st.Notice != "" {
		t.Errorf("an empty clear left a sentence: %q", m.st.Notice)
	}
	if m.st.Mode != ModeComposing {
		t.Error("an empty clear changed the mode")
	}
}

// TestCtrlUDuringEachPendingGateDoesNotClear. key() routes every pending y/n
// ahead of compose, so ctrl+u must reach each gate's OWN stray-key rule rather
// than the draft — and those rules differ, measured off the handlers rather
// than invented: clear/undo/adopt/write cancel their question (the safe
// reading of a key nobody meant to press is to put the act back out of reach),
// the flow write gate restates its question and stays pending, and the tool
// gate falls through to viewKey, where ctrl+u means nothing, leaving the card
// up. In every case the draft is untouched.
func TestCtrlUDuringEachPendingGateDoesNotClear(t *testing.T) {
	cases := map[string]struct {
		arm          func(m *Model) func() bool // returns "still pending?"
		stillPending bool                       // the gate's own stray-key rule
		notice       string
	}{
		"tool gate": {
			arm: func(m *Model) func() bool {
				m.st.Gates = []PendingGate{{Vendor: model.VendorClaude, RequestID: "req-1", Text: "Write: a.go"}}
				return func() bool { return m.st.Gating() }
			},
			stillPending: true, // gateKey falls through to viewKey; the card stays
			notice:       "",
		},
		"clear seat": {
			arm: func(m *Model) func() bool {
				m.sessions[model.VendorCodex] = "codex-thread"
				m.clearPending = model.VendorCodex
				return func() bool { return m.clearPending != "" }
			},
			stillPending: false, // a stray key cancels (clearGateKey)
			notice:       "clear cancelled",
		},
		"undo race attempt": {
			arm: func(m *Model) func() bool {
				m.undoPending = model.VendorCodex
				return func() bool { return m.undoPending != "" }
			},
			stillPending: false,
			notice:       "undo cancelled",
		},
		"adopt": {
			arm: func(m *Model) func() bool {
				m.adoptPending = model.VendorCodex
				return func() bool { return m.adoptPending != "" }
			},
			stillPending: false,
			notice:       "adopt cancelled",
		},
		"/write": {
			arm: func(m *Model) func() bool {
				m.writePending = true
				return func() bool { return m.writePending }
			},
			stillPending: false,
			notice:       "still read-only",
		},
		"flow write hop": {
			arm: func(m *Model) func() bool {
				m.flowWritePending = true
				return func() bool { return m.flowWritePending }
			},
			stillPending: true, // flowWriteGateKey restates the question
			notice:       "y authorizes, n cancels",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := flowRoom(t, true)
			m.st.Mode = ModeComposing
			m.setDraft("half a brief")
			stillPending := tc.arm(m)

			ctrlU(m)

			if got, want := m.st.Draft, "half a brief"; got != want {
				t.Errorf("ctrl+u reached the draft under a pending question: %q", got)
			}
			if stillPending() != tc.stillPending {
				t.Errorf("pending = %v after a stray ctrl+u, want %v", stillPending(), tc.stillPending)
			}
			if tc.notice != "" && !strings.Contains(m.st.Notice, tc.notice) {
				t.Errorf("the gate's stray-key answer does not say %q: %q", tc.notice, m.st.Notice)
			}
			if strings.Contains(m.st.Notice, "draft cleared") {
				t.Errorf("the room claims a clear that must not have happened: %q", m.st.Notice)
			}
		})
	}
	// And the clear-seat cancel cost nothing but the question: a stray ctrl+u
	// under that card must not have dropped the thread either.
	m := flowRoom(t, true)
	m.st.Mode = ModeComposing
	m.sessions[model.VendorCodex] = "codex-thread"
	m.clearPending = model.VendorCodex
	ctrlU(m)
	if m.sessions[model.VendorCodex] != "codex-thread" {
		t.Error("a stray ctrl+u under the clear card dropped the thread")
	}
}

// TestCtrlUInViewModeLeavesEverythingAlone. esc parks the draft under the
// promise "keeping the draft"; a chord that emptied it from view mode would
// revoke that promise from the mode it was made to. ctrl+u there matches
// nothing, silently — no notice, because the key is not offered in view mode's
// vocabulary anywhere on screen.
func TestCtrlUInViewModeLeavesEverythingAlone(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Mode = ModeComposing
	m.setDraft("a draft esc promised to keep")
	m.key(key("esc"))
	if m.st.Mode != ModeViewing {
		t.Fatal("esc did not return the room to view mode")
	}

	ctrlU(m)

	if got, want := m.st.Draft, "a draft esc promised to keep"; got != want {
		t.Errorf("view-mode ctrl+u touched the parked draft: %q", got)
	}
	if m.st.Mode != ModeViewing {
		t.Error("view-mode ctrl+u changed the mode")
	}
	if m.st.Notice != "" {
		t.Errorf("view-mode ctrl+u left a sentence: %q", m.st.Notice)
	}
	if log.n() != 0 || m.roomCtx.Err() != nil {
		t.Error("view-mode ctrl+u spawned or tore something down")
	}
}
