package council

import (
	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The live pane's keyboard (design.md §9.53, amended 2026-10-28).
//
// §9.53's first cut was a WINDOW: the operator watched the agent's own screen
// and answered it in the structured column beside it. This file is what makes
// it a terminal. One key hands the keyboard to the pane, every keystroke after
// it is encoded as the bytes a terminal would have sent and written to the
// pseudoconsole's input pipe, and the same key hands the keyboard back.
//
// Three rules, each the answer to a question §9.53 named as owed.
//
// A KEY ENCODING. encodeKey maps a Bubble Tea key press to what an xterm-style
// terminal sends for it, in NORMAL cursor mode. That last clause is the limit
// to read before anything else: a guest that sets DECCKM (ESC[?1h) expects
// ESC O A for an arrow and gets ESC [ A here, and a guest that asks for the
// kitty keyboard protocol or win32-input-mode is answered in neither. The
// emulator consumes those mode sets, so the pane knows nothing of them, and no
// live drive has measured whether claude.exe sets any. Owed, and named on the
// pane's own section rather than left for a reader to discover at the keyboard.
//
// A FOCUS RULE. The pane has the keyboard or it does not, and the whole room
// can see which (State.Live.Typing). While it has it, EVERY key but the one
// that gives it back goes to the pane — the room's `q`, `?`, `i`, the digits,
// the scroll keys, ctrl+c, all of them. Half a keymap leaking through would be
// the worst of both: a `q` that quit the room from inside a guest that uses
// `q`, or a `?` the guest never received. So the mode is total, it is entered
// only from the room's own view mode with the live seat already focused, and
// entering it moves nothing — no focus change, no scroll, no panel — because a
// surface that repositioned the room under the operator's hands would be the
// figure taking the jump it should only offer.
//
// WHAT `q` MEANS. The letter q. So does ctrl+c: it is the guest's interrupt,
// and an operator typing into claude's own prompt expects it to stop claude,
// not to cancel a structured turn two columns over. The room's own cancel and
// quit are one ctrl+] away, and the footer names that key on every frame the
// pane holds the keyboard.
//
// WHY ctrl+]. It is the one key a guest is least likely to want: telnet took it
// as its escape in 1983 and no terminal program since has bound it. It survives
// every terminal this room runs under — 0x1D is a C0 byte, decoded as ctrl+]
// by the pinned ultraviolet (key_table.go, GS) with no protocol negotiation —
// and it is the ONE key encodeKey refuses to forward, so a guest can never be
// sent the byte the room reserves.

// liveLeaveKey is the key that hands the keyboard to the pane and takes it
// back. One spelling, read by key() and modeHints and the help panel alike, so
// the three cannot name different keys.
const liveLeaveKey = "ctrl+]"

// liveKey is the typing mode's whole key handler.
//
// Reached from key() before every gate and every mode, which is the rule above
// in code: a gate raised by the structured seat while the pane has the keyboard
// is still named on the needs-you strip, and it is answered after ctrl+], not by
// a `y` meant for the guest.
func (m *Model) liveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == liveLeaveKey {
		m.st.Live.Typing = false
		m.st.Notice = ""
		return m, nil
	}
	b, ok := encodeKey(msg)
	if !ok {
		// A key the encoder has no bytes for is DROPPED, not re-dispatched. It
		// must not reach the room's keymap — that is the leak this mode exists
		// to close — and it must not reach the guest as a guess.
		return m, nil
	}
	m.liveWrite(b)
	return m, nil
}

// liveWrite sends bytes to the pseudoconsole, and says so if it cannot.
//
// A failed write ends the typing mode rather than leaving the operator typing
// into a pipe that drops every byte: the pane's marker goes back to a window's,
// and the notice says why. The child's own exit reaches the same place through
// applyPTY, which clears Typing when the phase moves to LiveEnded.
func (m *Model) liveWrite(b []byte) {
	if m.live == nil {
		m.st.Live.Typing = false
		m.st.Notice = "the live pane has no process to type into"
		return
	}
	if err := m.live.Write(b); err != nil {
		m.st.Live.Typing = false
		m.st.Notice = "the live pane stopped taking keys: " + err.Error()
	}
}

// enterLive hands the keyboard to the live pane, or says why it cannot.
//
// Every refusal is a sentence rather than a silent no-op, on §7.8's rule: a key
// the footer names must do something or say why not. The conditions are the
// ones livePaneRect already checks for the resize, plus focus — the pane must
// be the column the operator is looking at, so the keys go where their eyes
// are. Nothing else changes: not Focus, not Help, not Mode, not a scroll.
func (m *Model) enterLive() {
	l := m.st.Live
	if l.Seat == "" || l.Phase == LiveOff {
		m.st.Notice = "no live pane in this room — open one with --live claude"
		return
	}
	if l.Phase == LiveUnavailable || l.Phase == LiveEnded {
		m.st.Notice = "the live pane has no process to type into"
		return
	}
	c := m.focused()
	if c == nil || c.Vendor != l.Seat {
		m.st.Notice = "the live pane is on " + seatLabel(m.st, l.Seat) + " — focus it first, then " + liveLeaveKey
		return
	}
	if _, _, ok := livePaneRect(m.st); !ok {
		m.st.Notice = "the live pane is not on screen"
		return
	}
	m.st.Live.Typing = true
	m.st.Notice = ""
}

// seatLabel is a seat's column label, or its vendor id when it has no column.
func seatLabel(st State, v model.VendorID) string {
	for _, c := range st.Columns {
		if c.Vendor == v {
			return c.Label
		}
	}
	return string(v)
}

// encodeKey turns one key press into the bytes a terminal sends for it.
//
// xterm's normal-mode encoding, which is what a guest gets from Windows
// Terminal and from every VT-style terminal with no extended protocol
// negotiated. The second return is false for a key the encoder does not map,
// and for the reserved ctrl+], and a false means "send nothing" — never "send
// something close".
//
// Modifier arithmetic is xterm's: the parameter after `;` is 1 plus shift(1),
// alt(2), ctrl(4). A plain arrow is ESC [ A; shift+arrow is ESC [ 1 ; 2 A;
// a plain pgup is ESC [ 5 ~; ctrl+pgup is ESC [ 5 ; 5 ~.
func encodeKey(k tea.KeyPressMsg) ([]byte, bool) {
	if k.String() == liveLeaveKey {
		return nil, false
	}
	ctrl := k.Mod.Contains(tea.ModCtrl)
	alt := k.Mod.Contains(tea.ModAlt)
	shift := k.Mod.Contains(tea.ModShift)

	// Printable text, the common case. Alt prefixes ESC, which is the meta
	// convention every readline-style program understands; ctrl is handled
	// below on the code rather than the text, because a ctrl chord carries no
	// text in the decoder's own contract (bubbletea key.go: Text is populated
	// only for printable characters).
	if k.Text != "" && !ctrl {
		if alt {
			return append([]byte{0x1b}, k.Text...), true
		}
		return []byte(k.Text), true
	}

	// Control chords on the G0 range: letters fold to their C0 byte, and the
	// five punctuation chords xterm defines take theirs. ctrl+] is excluded
	// above; ctrl+space is NUL.
	if ctrl {
		var b byte
		switch code := k.Code; {
		case code >= 'a' && code <= 'z':
			b = byte(code) & 0x1f
		case code >= 'A' && code <= 'Z':
			b = byte(code-'A'+'a') & 0x1f
		case code == tea.KeySpace, code == '@', code == '2':
			b = 0x00
		case code == '[', code == '3', code == tea.KeyEscape:
			b = 0x1b
		case code == '\\', code == '4':
			b = 0x1c
		case code == '^', code == '6':
			b = 0x1e
		case code == '_', code == '7', code == '-':
			b = 0x1f
		case code == '8', code == '?', code == tea.KeyBackspace:
			b = 0x7f
		default:
			// A ctrl chord on a special key falls through to the CSI forms
			// below, which carry the modifier as a parameter.
			if seq, ok := encodeSpecial(k.Code, mod(shift, alt, ctrl)); ok {
				return seq, true
			}
			return nil, false
		}
		if alt {
			return []byte{0x1b, b}, true
		}
		return []byte{b}, true
	}

	seq, ok := encodeSpecial(k.Code, mod(shift, alt, ctrl))
	if !ok {
		return nil, false
	}
	if alt && len(seq) == 1 {
		// A one-byte key under alt takes the ESC prefix; a CSI form already
		// carries alt in its parameter.
		return append([]byte{0x1b}, seq...), true
	}
	return seq, true
}

// mod is xterm's modifier parameter: 0 when nothing is held, else 1 + bits.
func mod(shift, alt, ctrl bool) int {
	n := 0
	if shift {
		n |= 1
	}
	if alt {
		n |= 2
	}
	if ctrl {
		n |= 4
	}
	if n == 0 {
		return 0
	}
	return n + 1
}

// encodeSpecial is the non-text keys: the C0 names, the cursor keys, the
// editing keypad and the function keys.
func encodeSpecial(code rune, m int) ([]byte, bool) {
	switch code {
	case tea.KeyEnter, tea.KeyKpEnter:
		return []byte{'\r'}, true
	case tea.KeyTab:
		if m == 2 { // shift+tab is a back-tab, and it is CSI Z rather than a modified HT
			return []byte("\x1b[Z"), true
		}
		return []byte{'\t'}, true
	case tea.KeyBackspace:
		// DEL, not BS. Windows Terminal and xterm both send 0x7f for the key
		// above enter, and 0x08 is ctrl+h — a distinction readline keeps.
		return []byte{0x7f}, true
	case tea.KeyEscape:
		return []byte{0x1b}, true
	case tea.KeySpace:
		return []byte{' '}, true
	}
	// Cursor and home/end: ESC [ <final>, or ESC [ 1 ; <mod> <final>.
	if final, ok := map[rune]byte{
		tea.KeyUp: 'A', tea.KeyDown: 'B', tea.KeyRight: 'C', tea.KeyLeft: 'D',
		tea.KeyHome: 'H', tea.KeyEnd: 'F',
		tea.KeyF1: 'P', tea.KeyF2: 'Q', tea.KeyF3: 'R', tea.KeyF4: 'S',
	}[code]; ok {
		isF := code >= tea.KeyF1 && code <= tea.KeyF4
		if m == 0 {
			if isF {
				// F1-F4 are SS3 in normal mode: ESC O P. Modified, they take the
				// CSI 1 ; m form like the cursor keys.
				return []byte{0x1b, 'O', final}, true
			}
			return []byte{0x1b, '[', final}, true
		}
		return []byte("\x1b[1;" + itoa(m) + string(final)), true
	}
	// The editing keypad and F5+: ESC [ <n> ~, or ESC [ <n> ; <mod> ~.
	if n, ok := map[rune]string{
		tea.KeyInsert: "2", tea.KeyDelete: "3", tea.KeyPgUp: "5", tea.KeyPgDown: "6",
		tea.KeyF5: "15", tea.KeyF6: "17", tea.KeyF7: "18", tea.KeyF8: "19",
		tea.KeyF9: "20", tea.KeyF10: "21", tea.KeyF11: "23", tea.KeyF12: "24",
	}[code]; ok {
		if m == 0 {
			return []byte("\x1b[" + n + "~"), true
		}
		return []byte("\x1b[" + n + ";" + itoa(m) + "~"), true
	}
	return nil, false
}
