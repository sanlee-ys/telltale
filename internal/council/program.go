package council

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
)

const spinnerInterval = 100 * time.Millisecond

// Options configure a council run.
type Options struct {
	// Dir is the workspace turns are dispatched against. Empty means the
	// process working directory.
	Dir     string
	ASCII   bool
	NoTitle bool
}

// Model is the Bubble Tea model. It owns State plus the things Render must not
// see: the style set, the glyph set, and (from PR 2) the running child
// processes.
type Model struct {
	opts   Options
	st     State
	styles Styles
	glyphs Glyphs
}

// New builds the model. Nothing renders until the first WindowSizeMsg arrives:
// one blank frame beats one frame of wrong layout.
func New(opts Options) *Model {
	return &Model{
		opts:   opts,
		st:     stateWith(opts),
		styles: NewStyles(true), // assume dark until the terminal answers
		glyphs: GlyphsFor(opts.ASCII),
	}
}

func stateWith(opts Options) State {
	st := NewState()
	st.ASCII = opts.ASCII

	// Resolved once, here, so the render path never reads the environment.
	dir := opts.Dir
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	st.Workspace = dir
	if home, err := os.UserHomeDir(); err == nil {
		st.Home = home
	}

	windows := runtime.GOOS == "windows"
	for _, info := range Detect() {
		st.Columns = append(st.Columns, Column{
			Vendor:  info.Vendor,
			Label:   info.Label,
			Avail:   info.Avail,
			Binary:  info.Binary,
			Note:    info.Note,
			Sandbox: sandboxFor(info.Vendor, windows),
			Gran:    granularityFor(info.Vendor),
			Phase:   PhaseIdle,
		})
	}
	return st
}

type spinMsg time.Time

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		// RequestBackgroundColor is a Msg in v2, not a Cmd: it is a request the
		// runtime turns into an OSC query, so it has to be lifted into a Cmd
		// rather than handed to Batch directly.
		func() tea.Msg { return tea.RequestBackgroundColor() },
		spin(),
	)
}

func spin() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.st.Width, m.st.Height = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		m.styles = NewStyles(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg)

	case spinMsg:
		// The spinner only advances while a column is genuinely working. A
		// motionless room is the honest render of a room where nothing is
		// happening, and it keeps §7.1's budget of one moving cell.
		if m.st.Busy() {
			m.st.Spinner++
			return m, spin()
		}
		return m, spin()
	}
	return m, nil
}

// key routes by mode. Compose mode is checked FIRST and swallows everything
// that carries text, because in it `q` is the letter q — the footer says which
// mode is active on every frame precisely so this is never a surprise.
func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.st.Mode == ModeComposing {
		return m.composeKey(msg)
	}
	return m.viewKey(msg)
}

func (m *Model) composeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// The one key that always means the same thing.
		return m, tea.Quit
	case "esc":
		// esc leaves compose but KEEPS the draft. Unlike the HUD's find query,
		// a half-typed brief is expensive to retype and discarding it is not a
		// safety property — nothing has been dispatched.
		m.st.Mode = ModeViewing
		m.st.Notice = ""
	case "enter":
		return m, m.dispatch()
	case "backspace":
		if d := []rune(m.st.Draft); len(d) > 0 {
			m.st.Draft = string(d[:len(d)-1])
		}
		m.st.Notice = ""
	default:
		if t := msg.Text; t != "" {
			m.st.Draft += sanitizeKeepingSpace(t)
			m.st.Notice = ""
		}
	}
	return m, nil
}

func (m *Model) viewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Cancels the turn in flight if there is one, quits if there is not.
		// Wiring the cancel half is PR 2's job; until dispatch exists there is
		// never a turn in flight, so this is quit today and the help text does
		// not promise otherwise.
		return m, tea.Quit
	case "q":
		return m, tea.Quit
	case "?":
		m.st.Help = !m.st.Help
	case "i", "enter":
		m.st.Mode = ModeComposing
		m.st.Help = false
		m.st.Notice = ""
	case "tab", "right", "l":
		m.focusBy(1)
	case "shift+tab", "left", "h":
		m.focusBy(-1)
	}
	return m, nil
}

// focusBy moves the focused column, wrapping. Focus is an index into Columns,
// which is a fixed set resolved once at startup — unlike the HUD's cursor, it
// cannot be invalidated by a re-sort, so there is no selection-by-key
// machinery here.
func (m *Model) focusBy(d int) {
	n := len(m.st.Columns)
	if n == 0 {
		return
	}
	m.st.Focus = ((m.st.Focus+d)%n + n) % n
}

// dispatch is the seam PR 2 fills in.
//
// It is a stub on purpose rather than an absent keybinding: pressing enter and
// having nothing at all happen would read as a bug. The room says what it can
// and cannot do yet, in the same footer that carries every other notice.
func (m *Model) dispatch() tea.Cmd {
	if m.st.Seated() == 0 {
		m.st.Notice = "no vendor is seated — nothing to dispatch to"
		return nil
	}
	if m.st.Draft == "" {
		m.st.Notice = "nothing to dispatch: the brief is empty"
		return nil
	}
	m.st.Notice = "dispatch is not wired yet (council v0) — the brief is kept"
	return nil
}

// View is a thin wrapper over the pure renderer.
//
// Unlike the HUD's, this view DOES place a cursor: council has something to
// type into, and the one moving cell §7.1 budgets is spent here while
// composing and on a working column's spinner otherwise.
func (m *Model) View() tea.View {
	if m.st.Width == 0 || m.st.Height == 0 {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	v := tea.NewView(Render(m.st, m.styles, m.glyphs))
	v.AltScreen = true
	v.Cursor = nil
	if !m.opts.NoTitle {
		// The title says which room this is. Someone with a HUD and a council
		// open in two tabs should not have to guess from the taskbar.
		v.WindowTitle = "telltale council"
	}
	return v
}

// Run starts the room.
//
// Council is the one telltale mode that dispatches to vendor CLIs. The
// observation surfaces — statusline and hud — keep their read-only guarantee
// unchanged, and nothing here is reachable from either of them (ADR-008).
func Run(opts Options) error {
	p := tea.NewProgram(New(opts))
	_, err := p.Run()
	return err
}

var _ tea.Model = (*Model)(nil)
