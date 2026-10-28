package council

import (
	"bytes"
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The live pane's keyboard (livekeys.go), tested offline.
//
// No test here starts a pseudoconsole. The seam is runner.PTYSession, the same
// interface the Windows implementation satisfies, and fakePTY is a buffer
// behind it: every byte the room would have written to a child's stdin lands
// in fakePTY.in instead, where the test reads it back. The spawn guard in
// main_test.go is untouched and never reached.

// fakePTY is a PTYSession that records what was written and never runs.
type fakePTY struct {
	in     bytes.Buffer
	err    error
	killed bool
	cols   int
	rows   int
}

func (f *fakePTY) Resize(cols, rows int) error { f.cols, f.rows = cols, rows; return nil }
func (f *fakePTY) Write(p []byte) error {
	if f.err != nil {
		return f.err
	}
	f.in.Write(p)
	return nil
}
func (f *fakePTY) Kill()       { f.killed = true }
func (f *fakePTY) Alive() bool { return !f.killed }
func (f *fakePTY) Pid() int    { return 0 }

var _ runner.PTYSession = (*fakePTY)(nil)

// liveModel is a room with a live Claude seat showing a screen, a fake pty
// behind it, and the live seat focused — the state ctrl+] is allowed from.
func liveModel(t *testing.T) (*Model, *fakePTY) {
	t.Helper()
	st := liveRoom(t)
	st.Mode = ModeViewing
	pty := &fakePTY{}
	m := &Model{st: st, styles: PlainStyles(), glyphs: GlyphsFor(false), live: pty}
	m.emu = vt.NewEmulator(st.Live.Cols, st.Live.Rows)
	m.emu.SetScrollbackSize(liveScrollback)
	if c := m.focused(); c == nil || c.Vendor != model.VendorClaude {
		t.Fatal("fixture drift: the live seat is not the focused column")
	}
	return m, pty
}

func pk(code rune, mods ...tea.KeyMod) tea.KeyPressMsg {
	k := tea.KeyPressMsg{Code: code}
	for _, m := range mods {
		k.Mod |= m
	}
	return k
}

func text(s string, mods ...tea.KeyMod) tea.KeyPressMsg {
	k := pk([]rune(s)[0], mods...)
	k.Text = s
	return k
}

// TestLiveKeyEncoder pins every key the encoder maps to the bytes a terminal
// sends for it, and the two kinds it refuses.
func TestLiveKeyEncoder(t *testing.T) {
	cases := []struct {
		name string
		k    tea.KeyPressMsg
		want string
	}{
		// Printable text goes through as typed; alt prefixes ESC.
		{"letter", text("a"), "a"},
		{"shifted letter", text("A", tea.ModShift), "A"},
		{"digit", text("7"), "7"},
		{"punctuation", text("?"), "?"},
		{"the letter q is the letter q", text("q"), "q"},
		{"multi-rune text", tea.KeyPressMsg{Code: tea.KeyExtended, Text: "ab"}, "ab"},
		{"non-ascii", text("é"), "é"},
		{"alt+letter", text("x", tea.ModAlt), "\x1bx"},
		{"space", pk(tea.KeySpace), " "},
		{"space as text", text(" "), " "},
		// The C0 names.
		{"enter", pk(tea.KeyEnter), "\r"},
		{"keypad enter", pk(tea.KeyKpEnter), "\r"},
		{"tab", pk(tea.KeyTab), "\t"},
		{"shift+tab", pk(tea.KeyTab, tea.ModShift), "\x1b[Z"},
		{"backspace", pk(tea.KeyBackspace), "\x7f"},
		{"alt+backspace", pk(tea.KeyBackspace, tea.ModAlt), "\x1b\x7f"},
		{"esc", pk(tea.KeyEscape), "\x1b"},
		// Control chords.
		{"ctrl+c", pk('c', tea.ModCtrl), "\x03"},
		{"ctrl+d", pk('d', tea.ModCtrl), "\x04"},
		{"ctrl+a", pk('a', tea.ModCtrl), "\x01"},
		{"ctrl+z", pk('z', tea.ModCtrl), "\x1a"},
		{"ctrl+l", pk('l', tea.ModCtrl), "\x0c"},
		{"ctrl+u", pk('u', tea.ModCtrl), "\x15"},
		{"ctrl+w", pk('w', tea.ModCtrl), "\x17"},
		{"ctrl+shift letter folds", pk('A', tea.ModCtrl|tea.ModShift), "\x01"},
		{"ctrl+alt+letter", pk('b', tea.ModCtrl|tea.ModAlt), "\x1b\x02"},
		{"ctrl+space", pk(tea.KeySpace, tea.ModCtrl), "\x00"},
		{"ctrl+@", pk('@', tea.ModCtrl), "\x00"},
		{"ctrl+[", pk('[', tea.ModCtrl), "\x1b"},
		{"ctrl+\\", pk('\\', tea.ModCtrl), "\x1c"},
		{"ctrl+^", pk('^', tea.ModCtrl), "\x1e"},
		{"ctrl+_", pk('_', tea.ModCtrl), "\x1f"},
		{"ctrl+backspace", pk(tea.KeyBackspace, tea.ModCtrl), "\x7f"},
		// Cursor keys, plain and modified.
		{"up", pk(tea.KeyUp), "\x1b[A"},
		{"down", pk(tea.KeyDown), "\x1b[B"},
		{"right", pk(tea.KeyRight), "\x1b[C"},
		{"left", pk(tea.KeyLeft), "\x1b[D"},
		{"home", pk(tea.KeyHome), "\x1b[H"},
		{"end", pk(tea.KeyEnd), "\x1b[F"},
		{"shift+up", pk(tea.KeyUp, tea.ModShift), "\x1b[1;2A"},
		{"alt+left", pk(tea.KeyLeft, tea.ModAlt), "\x1b[1;3D"},
		{"ctrl+right", pk(tea.KeyRight, tea.ModCtrl), "\x1b[1;5C"},
		{"ctrl+shift+down", pk(tea.KeyDown, tea.ModCtrl|tea.ModShift), "\x1b[1;6B"},
		{"ctrl+alt+shift+home", pk(tea.KeyHome, tea.ModCtrl|tea.ModAlt|tea.ModShift), "\x1b[1;8H"},
		// The editing keypad.
		{"insert", pk(tea.KeyInsert), "\x1b[2~"},
		{"delete", pk(tea.KeyDelete), "\x1b[3~"},
		{"pgup", pk(tea.KeyPgUp), "\x1b[5~"},
		{"pgdown", pk(tea.KeyPgDown), "\x1b[6~"},
		{"shift+pgup", pk(tea.KeyPgUp, tea.ModShift), "\x1b[5;2~"},
		{"ctrl+delete", pk(tea.KeyDelete, tea.ModCtrl), "\x1b[3;5~"},
		// Function keys: SS3 for F1-F4, CSI ~ above.
		{"f1", pk(tea.KeyF1), "\x1bOP"},
		{"f2", pk(tea.KeyF2), "\x1bOQ"},
		{"f3", pk(tea.KeyF3), "\x1bOR"},
		{"f4", pk(tea.KeyF4), "\x1bOS"},
		{"shift+f1", pk(tea.KeyF1, tea.ModShift), "\x1b[1;2P"},
		{"f5", pk(tea.KeyF5), "\x1b[15~"},
		{"f6", pk(tea.KeyF6), "\x1b[17~"},
		{"f7", pk(tea.KeyF7), "\x1b[18~"},
		{"f8", pk(tea.KeyF8), "\x1b[19~"},
		{"f9", pk(tea.KeyF9), "\x1b[20~"},
		{"f10", pk(tea.KeyF10), "\x1b[21~"},
		{"f11", pk(tea.KeyF11), "\x1b[23~"},
		{"f12", pk(tea.KeyF12), "\x1b[24~"},
		{"ctrl+f5", pk(tea.KeyF5, tea.ModCtrl), "\x1b[15;5~"},
	}
	for _, tc := range cases {
		got, ok := encodeKey(tc.k)
		if !ok {
			t.Errorf("%s: %q was refused, want %q", tc.name, tc.k.String(), tc.want)
			continue
		}
		if string(got) != tc.want {
			t.Errorf("%s: %q encoded as %q, want %q", tc.name, tc.k.String(), got, tc.want)
		}
	}

	refused := []struct {
		name string
		k    tea.KeyPressMsg
	}{
		// The reserved key is never forwarded, so a guest cannot be sent the
		// byte the room keeps for itself.
		{"ctrl+] is the room's", pk(']', tea.ModCtrl)},
		// Keys with no normal-mode encoding send nothing rather than a guess.
		{"caps lock", pk(tea.KeyCapsLock)},
		{"a bare modifier", pk(tea.KeyLeftShift)},
		{"media key", pk(tea.KeyMediaPlay)},
		{"f13", pk(tea.KeyF13)},
		{"ctrl on a non-latin code", pk('é', tea.ModCtrl)},
	}
	for _, tc := range refused {
		if got, ok := encodeKey(tc.k); ok {
			t.Errorf("%s: %q encoded as %q, want refused", tc.name, tc.k.String(), got)
		}
	}
	if pk(']', tea.ModCtrl).String() != liveLeaveKey {
		t.Fatalf("the reserved key spells %q, the handler matches %q", pk(']', tea.ModCtrl).String(), liveLeaveKey)
	}
}

// TestLivePaneTakesTheKeyboardWhole is the focus rule: ctrl+] hands every key
// to the pane, the room's own bindings stop meaning anything, and ctrl+] gives
// them back. Entering moves nothing.
func TestLivePaneTakesTheKeyboardWhole(t *testing.T) {
	m, pty := liveModel(t)
	before := m.st

	m.key(pk(']', tea.ModCtrl))
	if !m.st.Live.Typing {
		t.Fatalf("ctrl+] on a focused live seat did not hand it the keyboard: %q", m.st.Notice)
	}
	// Entering is the one change. Focus, help, mode, scroll, draft — all as
	// they were, because a figure offers the jump and does not take it.
	after := m.st
	after.Live.Typing = false
	if after.Focus != before.Focus || after.Help != before.Help || after.Mode != before.Mode ||
		after.Draft != before.Draft || after.Expanded != before.Expanded {
		t.Errorf("entering the pane moved the room:\nbefore %+v\nafter  %+v", before, after)
	}
	if pty.in.Len() != 0 {
		t.Errorf("entering wrote %q to the child", pty.in.Bytes())
	}

	// The room's keys are letters now.
	for _, k := range []tea.KeyPressMsg{text("q"), text("?"), text("i"), text("1"), text("."), text("f")} {
		if _, cmd := m.key(k); cmd != nil {
			t.Errorf("%q produced a command while the pane held the keyboard", k.String())
		}
	}
	if m.st.Help != HelpClosed || m.st.Mode != ModeViewing || m.st.Expanded || m.st.Focus != before.Focus {
		t.Errorf("a room key leaked past the pane: help=%v mode=%v expanded=%v focus=%d",
			m.st.Help, m.st.Mode, m.st.Expanded, m.st.Focus)
	}
	if got := pty.in.String(); got != "q?i1.f" {
		t.Errorf("the pane received %q, want %q", got, "q?i1.f")
	}
	pty.in.Reset()

	// ctrl+c is the guest's interrupt, not the room's cancel or quit.
	if _, cmd := m.key(pk('c', tea.ModCtrl)); cmd != nil {
		t.Error("ctrl+c quit the room from inside the pane")
	}
	m.key(pk(tea.KeyEnter))
	m.key(pk(tea.KeyUp))
	m.key(pk(tea.KeyBackspace))
	m.key(pk(tea.KeyTab))
	if got := pty.in.String(); got != "\x03\r\x1b[A\x7f\t" {
		t.Errorf("the pane received %q", got)
	}
	pty.in.Reset()

	// A key with no encoding is dropped, and dropped means it reaches neither
	// side.
	m.key(pk(tea.KeyCapsLock))
	if pty.in.Len() != 0 {
		t.Errorf("an unmapped key reached the pane as %q", pty.in.Bytes())
	}

	// ctrl+] hands the keyboard back, writes nothing, and the next key is the
	// room's again.
	m.key(pk(']', tea.ModCtrl))
	if m.st.Live.Typing {
		t.Fatal("ctrl+] did not hand the keyboard back")
	}
	if pty.in.Len() != 0 {
		t.Errorf("leaving wrote %q to the child", pty.in.Bytes())
	}
	m.key(text("i"))
	if m.st.Mode != ModeComposing {
		t.Error("after leaving the pane, i did not open the composer")
	}
	if pty.in.Len() != 0 {
		t.Errorf("a room key reached the pane after ctrl+]: %q", pty.in.Bytes())
	}
}

// TestLivePaneKeysNeverReachTheComposer: entered from compose with a draft,
// the keys go to the pane and the draft comes back untouched.
func TestLivePaneKeysNeverReachTheComposer(t *testing.T) {
	m, pty := liveModel(t)
	m.st.Mode = ModeComposing
	m.setDraft("half a brief")

	m.key(pk(']', tea.ModCtrl))
	if !m.st.Live.Typing {
		t.Fatalf("ctrl+] from compose was refused: %q", m.st.Notice)
	}
	m.key(text("x"))
	m.key(pk(tea.KeyBackspace))
	m.key(pk(tea.KeyEnter))
	if m.st.Draft != "half a brief" {
		t.Errorf("pane keys changed the draft: %q", m.st.Draft)
	}
	if got := pty.in.String(); got != "x\x7f\r" {
		t.Errorf("the pane received %q", got)
	}
	// A paste goes to the pane too, raw, and never into the draft.
	m.paste(tea.PasteMsg{Content: "pasted\ntext"})
	if m.st.Draft != "half a brief" {
		t.Errorf("a paste reached the draft while the pane held the keyboard: %q", m.st.Draft)
	}
	if got := pty.in.String(); got != "x\x7f\rpasted\ntext" {
		t.Errorf("the pane received %q", got)
	}
	m.key(pk(']', tea.ModCtrl))
	if m.st.Mode != ModeComposing || m.st.Draft != "half a brief" {
		t.Errorf("leaving the pane did not return the composer as it was: mode=%v draft=%q", m.st.Mode, m.st.Draft)
	}
}

// TestLivePaneRefusesTheKeyboardInWords: every state ctrl+] cannot act in
// says why, and the keyboard stays with the room.
func TestLivePaneRefusesTheKeyboardInWords(t *testing.T) {
	t.Run("no live seat", func(t *testing.T) {
		st := room()
		st.Mode = ModeViewing
		m := &Model{st: st, styles: PlainStyles(), glyphs: GlyphsFor(false)}
		m.key(pk(']', tea.ModCtrl))
		if m.st.Live.Typing || m.st.Notice == "" {
			t.Errorf("typing=%v notice=%q", m.st.Live.Typing, m.st.Notice)
		}
	})
	t.Run("another seat focused", func(t *testing.T) {
		m, pty := liveModel(t)
		m.st.Focus = 1
		m.key(pk(']', tea.ModCtrl))
		if m.st.Live.Typing {
			t.Fatal("the pane took the keyboard while another seat was focused")
		}
		if m.st.Notice == "" || m.st.Focus != 1 {
			t.Errorf("notice=%q focus=%d — the refusal must say so and move nothing", m.st.Notice, m.st.Focus)
		}
		m.key(text("q"))
		if pty.in.Len() != 0 {
			t.Errorf("a refused entry still forwarded keys: %q", pty.in.Bytes())
		}
	})
	t.Run("ended child", func(t *testing.T) {
		m, _ := liveModel(t)
		m.st.Live.Phase = LiveEnded
		m.key(pk(']', tea.ModCtrl))
		if m.st.Live.Typing || m.st.Notice == "" {
			t.Errorf("typing=%v notice=%q", m.st.Live.Typing, m.st.Notice)
		}
	})
	t.Run("pane off screen", func(t *testing.T) {
		m, _ := liveModel(t)
		m.st.Help = HelpKeys
		m.key(pk(']', tea.ModCtrl))
		if m.st.Live.Typing || m.st.Notice == "" {
			t.Errorf("typing=%v notice=%q", m.st.Live.Typing, m.st.Notice)
		}
	})
}

// TestLivePaneGivesTheKeyboardBackWhenItCannotTake: the child ending, or a
// write failing, returns the keys to the room rather than leaving the operator
// typing into nothing.
func TestLivePaneGivesTheKeyboardBackWhenItCannotTake(t *testing.T) {
	t.Run("child exits", func(t *testing.T) {
		m, _ := liveModel(t)
		m.st.Live.Typing = true
		m.applyPTY([]runner.PTYChunk{{Vendor: model.VendorClaude, Done: true}})
		if m.st.Live.Typing {
			t.Error("a pane whose child exited still holds the keyboard")
		}
	})
	t.Run("write fails", func(t *testing.T) {
		m, pty := liveModel(t)
		m.st.Live.Typing = true
		pty.err = errors.New("pipe closed")
		m.key(text("a"))
		if m.st.Live.Typing {
			t.Error("a pane that cannot take a byte still holds the keyboard")
		}
		if m.st.Notice == "" {
			t.Error("the failed write was silent")
		}
	})
	t.Run("killed", func(t *testing.T) {
		m, pty := liveModel(t)
		m.st.Live.Typing = true
		m.killLive()
		if m.st.Live.Typing || !pty.killed {
			t.Errorf("typing=%v killed=%v", m.st.Live.Typing, pty.killed)
		}
	})
}

// TestLivePaneResizeStillReachesThePty: the keyboard changes nothing about the
// resize path. A window change still reaches Resize with the pane's rectangle.
func TestLivePaneResizeStillReachesThePty(t *testing.T) {
	m, pty := liveModel(t)
	m.liveCols, m.liveRows = 80, 24
	m.st.Live.Typing = true
	m.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	cols, rows, ok := livePaneRect(m.st)
	if !ok {
		t.Fatal("no pane after the resize")
	}
	if pty.cols != cols || pty.rows != rows {
		t.Errorf("the pty was resized to %dx%d, the pane is %dx%d", pty.cols, pty.rows, cols, rows)
	}
	if m.emu.Width() != cols || m.emu.Height() != rows {
		t.Errorf("the emulator is %dx%d, the pane is %dx%d", m.emu.Width(), m.emu.Height(), cols, rows)
	}
	if !m.st.Live.Typing {
		t.Error("a resize took the keyboard away")
	}
}

// TestLiveKeysGolden pins the mode word where it is drawn: the marker row says
// KEYS, the border says KEYS, and the footer names ctrl+] — all as words, with
// PlainStyles and the ASCII glyph set, so the distinction is proven to survive
// NO_COLOR and --ascii together.
func TestLiveKeysGolden(t *testing.T) {
	st := liveRoom(t)
	st.Mode = ModeViewing
	st.Live.Typing = true
	st.ASCII = true
	golden(t, "live-keys-go-to-the-pane-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}
