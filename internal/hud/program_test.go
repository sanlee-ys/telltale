package hud

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// These drive the Bubble Tea model directly. There is no terminal and no
// program loop: Update is a pure state transition and View is pure over State,
// which is the property that makes the whole HUD testable.

func newTestModel(adapters ...model.Adapter) *Model {
	m := New(Options{Adapters: adapters})
	m.st.Now = pinned
	return m
}

func send(t *testing.T, m *Model, msg tea.Msg) *Model {
	t.Helper()
	next, _ := m.Update(msg)
	got, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", next)
	}
	return got
}

// One blank frame beats one frame of wrong layout.
func TestNothingRendersBeforeTheFirstWindowSize(t *testing.T) {
	m := newTestModel()
	if got := m.View().Content; got != "" {
		t.Fatalf("rendered %q before knowing the terminal size", got)
	}
	m = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 10})
	if got := m.View().Content; got == "" {
		t.Fatal("rendered nothing after the size arrived")
	}
}

func TestViewIsAltScreenAndCursorless(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})
	v := m.View()
	if !v.AltScreen {
		t.Error("the HUD must own the alternate screen buffer")
	}
	if v.Cursor != nil {
		t.Error("a monitor has nothing to type into; the cursor must stay hidden")
	}
	if v.WindowTitle != "telltale" {
		t.Errorf("window title = %q", v.WindowTitle)
	}
}

func TestNoTitleSuppressesTheWindowTitle(t *testing.T) {
	m := New(Options{NoTitle: true})
	m = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 10})
	if got := m.View().WindowTitle; got != "" {
		t.Errorf("window title = %q, want none", got)
	}
}

func TestKeysCycleFilterAndSortAndToggleHelp(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})

	for _, want := range []Filter{FilterClaude, FilterCodex, FilterAll} {
		m = send(t, m, key("v"))
		if m.st.Filter != want {
			t.Fatalf("filter = %v, want %v", m.st.Filter, want)
		}
	}
	for _, want := range []SortKey{SortContext, SortCost, SortActivity} {
		m = send(t, m, key("s"))
		if m.st.Sort != want {
			t.Fatalf("sort = %v, want %v", m.st.Sort, want)
		}
	}
	m = send(t, m, key("?"))
	if !m.st.Help {
		t.Fatal("? did not open the help overlay")
	}
	m = send(t, m, key("?"))
	if m.st.Help {
		t.Fatal("? did not close the help overlay")
	}
	m = send(t, m, key("a"))
	if !m.st.ShowAll {
		t.Fatal("a did not toggle show-all")
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})
		_, cmd := m.Update(key(k))
		if cmd == nil {
			t.Fatalf("%q produced no command, expected quit", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%q did not quit", k)
		}
	}
}

// The HUD is strictly read-only. No key may mutate vendor state or send
// anything to a running agent, so every key either changes view state or
// requests a rescan — and nothing else exists to call.
func TestNoKeyMutatesVendorState(t *testing.T) {
	a := fakeVendor(model.VendorClaude, "a")
	m := send(t, newTestModel(a), tea.WindowSizeMsg{Width: 120, Height: 10})
	before := *a
	for _, k := range []string{"v", "s", "a", "?", "up", "down", "j", "k", "r"} {
		m = send(t, m, key(k))
	}
	if len(a.refs) != len(before.refs) || len(a.sessions) != len(before.sessions) {
		t.Fatal("a keystroke changed the adapter's state")
	}
}

// A tick arriving while a scan is in flight is dropped: at most one scan runs
// at a time, so a slow filesystem cannot pile up work behind the poll.
func TestTicksAreDroppedWhileAScanIsInFlight(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})
	m.inFlight = true
	m = send(t, m, tickMsg(pinned))
	if !m.inFlight {
		t.Fatal("a second scan was started while one was already running")
	}

	m = send(t, m, scanResultMsg{snap: Snapshot{At: pinned}})
	if m.inFlight {
		t.Fatal("the scan result did not clear the in-flight flag")
	}
}

// telltale may animate its own work, and only its own: after the first
// completed scan the spinner never returns. Later slow scans surface as
// staleness in the footer, not as motion.
func TestSpinnerStopsForeverAfterTheFirstScan(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})
	m.started = pinned.Add(-time.Second)
	m.inFlight = true
	m = send(t, m, spinMsg(pinned))
	if !m.st.Scanning {
		t.Fatal("the spinner did not start for a first scan already past 250ms")
	}

	m = send(t, m, scanResultMsg{snap: Snapshot{At: pinned}})
	m.inFlight = true
	m = send(t, m, spinMsg(pinned))
	if m.st.Scanning {
		t.Fatal("the spinner came back after the first scan completed")
	}
}

func TestBackgroundColorRebuildsTheStyleSetWithoutMovingTheLayout(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 10})
	m.st.Snap = Snapshot{Sessions: healthy(), At: pinned}
	m.st.Now = pinned

	dark := stripANSI(m.View().Content)
	m.styles = NewStyles(false)
	light := stripANSI(m.View().Content)
	if dark != light {
		t.Fatal("the background-colour answer moved the layout; only the gauge track may depend on it")
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	default:
		r := []rune(s)[0]
		return tea.KeyPressMsg{Code: r, Text: s}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case esc:
			if r == 'm' {
				esc = false
			}
		case r == '\x1b':
			esc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
