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

	for _, want := range []Filter{
		FilterClaude, FilterCodex, FilterGemini, FilterAntigravity, FilterCursor, FilterAll,
	} {
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

// ------------------------------------------------------ v1.1 interaction

// loaded is a model with the v1.1 fixture snapshot already in place.
func loaded(t *testing.T) *Model {
	t.Helper()
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 20})
	m.st.Snap = v11State(120, 20).Snap
	m.st.Now = pinned
	return m
}

func TestArrowsPlantTheSelectionAndStayInsideTheList(t *testing.T) {
	m := loaded(t)
	if m.st.Cursor != -1 {
		t.Fatalf("Cursor = %d before any keypress, want -1", m.st.Cursor)
	}
	m = send(t, m, key("down"))
	if m.st.Cursor != 0 {
		t.Fatalf("the first arrow selected row %d, want the top row", m.st.Cursor)
	}
	for i := 0; i < 10; i++ {
		m = send(t, m, key("j"))
	}
	if want := len(visibleSessions(m.st)) - 1; m.st.Cursor != want {
		t.Errorf("Cursor ran to %d, want a clamp at %d", m.st.Cursor, want)
	}
	for i := 0; i < 10; i++ {
		m = send(t, m, key("k"))
	}
	if m.st.Cursor != 0 {
		t.Errorf("Cursor ran to %d, want a clamp at 0", m.st.Cursor)
	}
}

func TestEnterOpensAndClosesTheDetailPane(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("enter"))
	if !m.st.Detail {
		t.Fatal("enter did not open the pane")
	}
	if m.st.Cursor != 0 {
		t.Errorf("enter with no selection opened row %d, want the top row", m.st.Cursor)
	}
	m = send(t, m, key("enter"))
	if m.st.Detail {
		t.Fatal("enter did not close the pane")
	}
}

// v1's esc quit unconditionally. With a pane and a query to back out of, an
// esc that closed the program instead of the pane would punish the reflex the
// product itself trained — so it unwinds one layer at a time and only quits
// from the bottom.
func TestEscUnwindsOneLayerAtATime(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("u"))
	m, cmd := updateWith(t, m, key("esc"))
	if cmd != nil {
		t.Fatal("esc quit with the usage view open")
	}
	if m.st.Usage {
		t.Fatal("esc did not close the usage view")
	}

	m = send(t, m, key("enter"))
	m, cmd = updateWith(t, m, key("esc"))
	if cmd != nil {
		t.Fatal("esc quit with the detail pane open")
	}
	if m.st.Detail {
		t.Fatal("esc did not close the detail pane")
	}

	m = send(t, m, key("?"))
	m, cmd = updateWith(t, m, key("esc"))
	if cmd != nil || m.st.Help {
		t.Fatal("esc did not close the help overlay without quitting")
	}

	m.st.Query = "api"
	m, cmd = updateWith(t, m, key("esc"))
	if cmd != nil {
		t.Fatal("esc quit while a query was applied")
	}
	if m.st.Query != "" {
		t.Fatalf("esc left the query %q applied", m.st.Query)
	}

	_, cmd = updateWith(t, m, key("esc"))
	if cmd == nil {
		t.Fatal("esc from the bottom layer did not quit")
	}
}

// u opens and closes the fleet usage view, and it is a body rather than an
// overlay — so it may never be on screen beside another one.
func TestUsageViewIsOneBodyAtATime(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("u"))
	if !m.st.Usage {
		t.Fatal("u did not open the usage view")
	}
	m = send(t, m, key("u"))
	if m.st.Usage {
		t.Fatal("u did not close the usage view")
	}

	// Every other door closes it, and it closes every other door. Rendering
	// would otherwise have to pick a winner, and a pane that appears only
	// because it won an ordering is a pane nobody can predict.
	for _, open := range []struct {
		key  string
		want func(*Model) bool
	}{
		{"?", func(m *Model) bool { return m.st.Help }},
		{"enter", func(m *Model) bool { return m.st.Detail }},
		// Find is a MODE rather than a body, and it closes the view for the
		// same reason the bodies do — but only in this direction. Once find
		// mode has the keyboard, "u" is a letter, which is the whole point of
		// the mode announcing itself in the footer.
		{"/", func(m *Model) bool { return m.st.Finding }},
	} {
		m = send(t, loaded(t), key("u"))
		m = send(t, m, key(open.key))
		if m.st.Usage {
			t.Errorf("%q left the usage view open beside it", open.key)
		}
		if !open.want(m) {
			t.Errorf("%q did not open its own body from the usage view", open.key)
		}

		// And the other direction, for the two that are bodies.
		if open.key == "/" {
			continue
		}
		m = send(t, loaded(t), key(open.key))
		m = send(t, m, key("u"))
		if !m.st.Usage {
			t.Errorf("u did not open from the %q body", open.key)
		}
		if m.st.Help || m.st.Detail {
			t.Errorf("u left the %q body open beside it", open.key)
		}
	}
}

// The arrows move whatever the body is. Over the usage view that is the view,
// not the row selection underneath it — and the offset is bounded against the
// body's own length so it takes no more keypresses to come back than it took to
// leave.
func TestUsageViewScrollIsBoundedAndLeavesTheSelectionAlone(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 8})
	m = send(t, m, key("u"))
	for i := 0; i < 50; i++ {
		m = send(t, m, key("down"))
	}
	if max := len(m.scrollBody()) - 1; m.st.Scroll > max {
		t.Errorf("usage scroll ran to %d, bound is %d", m.st.Scroll, max)
	}
	if m.st.Cursor != -1 {
		t.Error("arrows over the usage view moved the row selection")
	}
}

// Find mode swallows the keyboard, which is exactly why it announces itself in
// the footer: while it is on, "q" is a letter.
func TestFindModeSwallowsTheKeyboard(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("/"))
	if !m.st.Finding {
		t.Fatal("/ did not enter find mode")
	}
	for _, k := range []string{"q", "v", "s", "a"} {
		next, cmd := m.Update(key(k))
		if cmd != nil {
			t.Fatalf("%q issued a command in find mode", k)
		}
		m = next.(*Model)
	}
	if m.st.Query != "qvsa" {
		t.Fatalf("query = %q, want the typed letters", m.st.Query)
	}
	if m.st.Filter != FilterAll || m.st.Sort != SortActivity || m.st.ShowAll {
		t.Error("a keystroke in find mode also acted as a command")
	}
	m = send(t, m, key("backspace"))
	if m.st.Query != "qvs" {
		t.Errorf("backspace left %q", m.st.Query)
	}
	// ctrl+c is the one key that always means the same thing.
	if _, cmd := m.Update(key("ctrl+c")); cmd == nil {
		t.Error("ctrl+c did not quit from find mode")
	}
}

// enter keeps the query and hands the keyboard back; esc clears it. Leaving a
// cancelled query applied would hide rows behind a mode the user just backed
// out of.
func TestFindEnterKeepsAndEscClears(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("/"))
	for _, k := range []string{"a", "p", "i"} {
		m = send(t, m, key(k))
	}
	m = send(t, m, key("enter"))
	if m.st.Finding || m.st.Query != "api" {
		t.Fatalf("enter left finding=%v query=%q", m.st.Finding, m.st.Query)
	}
	if got := len(visibleSessions(m.st)); got != 1 {
		t.Errorf("the applied query shows %d rows, want 1", got)
	}

	m = send(t, m, key("/"))
	m = send(t, m, key("esc"))
	if m.st.Finding || m.st.Query != "" {
		t.Fatalf("esc left finding=%v query=%q", m.st.Finding, m.st.Query)
	}
}

// The cursor is an index into the visible rows; anything that changes which
// rows are visible makes the old index point at a different session.
func TestChangingTheVisibleSetDropsTheSelection(t *testing.T) {
	for _, k := range []string{"v", "s", "a"} {
		t.Run(k, func(t *testing.T) {
			m := loaded(t)
			m = send(t, m, key("enter"))
			if m.st.Cursor < 0 || !m.st.Detail {
				t.Fatal("setup: nothing selected")
			}
			m = send(t, m, key(k))
			if m.st.Cursor != -1 {
				t.Errorf("%q kept a stale selection at index %d", k, m.st.Cursor)
			}
			if m.st.Detail {
				t.Errorf("%q left the pane open on an undefined subject", k)
			}
		})
	}
}

// Sorting is by activity, so a session that just wrote a record jumps to the
// top and shifts every row under it. The selection follows the SESSION, not
// the index — otherwise the pane silently relabels one session's diagnostics
// with another's.
func TestSelectionFollowsTheSessionNotTheIndex(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("down"))
	m = send(t, m, key("down")) // second row: acme-api
	want := visibleSessions(m.st)[1].Key()
	if m.selectedKey != want {
		t.Fatalf("selectedKey = %q, want %q", m.selectedKey, want)
	}

	// The bottom session becomes the most recent one, re-sorting the list.
	next := v11State(120, 20).Snap
	next.At = pinned
	next.Sessions[3].LastActivity = model.TimePtr(pinned.Add(-time.Second))
	m = send(t, m, scanResultMsg{snap: next})

	rows := visibleSessions(m.st)
	if m.st.Cursor < 0 || rows[m.st.Cursor].Key() != want {
		t.Fatalf("after the re-sort the cursor points at %v, want %q",
			func() string {
				if m.st.Cursor < 0 {
					return "nothing"
				}
				return rows[m.st.Cursor].Key()
			}(), want)
	}
}

// A selected session that disappears takes the pane with it. Retargeting the
// pane at whatever now occupies the index would attribute one session's
// diagnostics to another.
func TestASelectedSessionThatVanishesClosesThePane(t *testing.T) {
	m := loaded(t)
	m = send(t, m, key("enter"))
	if !m.st.Detail {
		t.Fatal("setup: pane not open")
	}
	m = send(t, m, scanResultMsg{snap: Snapshot{At: pinned}})
	if m.st.Detail {
		t.Error("the pane survived its session")
	}
	if m.st.Cursor != -1 || m.selectedKey != "" {
		t.Errorf("a stale selection survived: cursor=%d key=%q", m.st.Cursor, m.selectedKey)
	}
}

// One sample per COMPLETED scan, from the same windows the header renders.
func TestEachScanContributesOneQuotaSample(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 12})
	at := pinned
	for i := 0; i < 4; i++ {
		snap := healthyState(120, 12).Snap
		snap.At = at
		snap.Sessions[0].Quota = []model.QuotaWindow{
			{ID: "five_hour", Label: "5h", UsedPercent: model.PercentPtr(float64(20 + i*5))},
		}
		m = send(t, m, scanResultMsg{snap: snap})
		at = at.Add(5 * time.Minute)
		m.st.Now = at
	}
	if len(m.st.Burn.Series) != 1 {
		t.Fatalf("tracked %d windows, want 1", len(m.st.Burn.Series))
	}
	if n := len(m.st.Burn.Series[0].Samples); n != 4 {
		t.Errorf("collected %d samples from 4 scans, want 4", n)
	}
}

// The scroll offset is bounded by the overlay's own length: an offset that
// keeps counting past the end takes as many keypresses to come back as it took
// to leave.
func TestHelpOverlayScrollIsBounded(t *testing.T) {
	m := send(t, newTestModel(), tea.WindowSizeMsg{Width: 120, Height: 8})
	m = send(t, m, key("?"))
	for i := 0; i < 50; i++ {
		m = send(t, m, key("down"))
	}
	if max := len(m.scrollBody()) - 1; m.st.Scroll > max {
		t.Errorf("help scroll ran to %d, bound is %d", m.st.Scroll, max)
	}
	if m.st.Cursor != -1 {
		t.Error("arrows over the help overlay moved the row selection")
	}
}

func updateWith(t *testing.T, m *Model, msg tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	got, ok := next.(*Model)
	if !ok {
		t.Fatalf("Update returned %T, want *Model", next)
	}
	return got, cmd
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
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
