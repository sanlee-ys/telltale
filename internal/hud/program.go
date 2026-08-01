package hud

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// pollInterval is the tick cadence. 1 s is affordable because the poll is
// stat-first — Discover does directory listing only, and each adapter reads a
// bounded head and tail rather than whole transcripts.
const pollInterval = time.Second

// spinnerInterval is 10 fps, and drives the ONLY animation in the product.
const spinnerInterval = 100 * time.Millisecond

// Options configure a HUD run.
type Options struct {
	Adapters []model.Adapter
	Filter   Filter
	ASCII    bool
	NoTitle  bool
}

// Model is the Bubble Tea model. It owns State plus the things Render must not
// see: the adapter list, the style set, and whether a scan is in flight.
type Model struct {
	opts     Options
	st       State
	styles   Styles
	glyphs   Glyphs
	inFlight bool
	started  time.Time
}

// New builds the model. Nothing is rendered until the first WindowSizeMsg
// arrives: one blank frame beats one frame of wrong layout.
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
	st.Filter = opts.Filter
	st.Now = time.Now()
	// Resolved once here so the render path never reads the environment.
	if home, err := os.UserHomeDir(); err == nil {
		st.Home = home
	}
	return st
}

type scanResultMsg struct {
	snap Snapshot
}

type tickMsg time.Time

type spinMsg time.Time

func (m *Model) Init() tea.Cmd {
	m.started = time.Now()
	m.inFlight = true
	return tea.Batch(
		// RequestBackgroundColor is a Msg in v2, not a Cmd: it is a request
		// the runtime turns into an OSC query, so it has to be lifted into a
		// Cmd rather than handed to Batch directly.
		func() tea.Msg { return tea.RequestBackgroundColor() },
		m.scanCmd(),
		tick(),
		spin(),
	)
}

func tick() tea.Cmd {
	return tea.Tick(pollInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func spin() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinMsg(t) })
}

// scanCmd runs the scan OFF the Update goroutine.
//
// This is a Windows correctness requirement, not tidiness: a stat against a
// disconnected network path blocks, and a blocked Update freezes input —
// including the key that quits.
func (m *Model) scanCmd() tea.Cmd {
	adapters := m.opts.Adapters
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return scanResultMsg{snap: Scan(ctx, adapters, time.Now())}
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.st.Width, m.st.Height = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		// Lipgloss v2 has no AdaptiveColor and no global renderer, so the
		// style set is rebuilt here. Exactly one token depends on it and no
		// layout does, which is why goldens are unaffected by the answer.
		m.styles = NewStyles(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg)

	case tickMsg:
		m.st.Now = time.Time(msg)
		if m.inFlight {
			// Ticks arriving during a scan are dropped: at most one scan is
			// ever in flight.
			return m, tick()
		}
		m.inFlight = true
		return m, tea.Batch(m.scanCmd(), tick())

	case spinMsg:
		m.st.Now = time.Time(msg)
		if m.st.Snap.At.IsZero() && m.inFlight {
			m.st.Scanning = time.Since(m.started) > spinAfter
			if m.st.Scanning {
				m.st.Spinner++
			}
			return m, spin()
		}
		// After the first successful scan the spinner never comes back. Later
		// slow scans surface as staleness in the footer, not as motion.
		m.st.Scanning = false
		return m, nil

	case scanResultMsg:
		m.inFlight = false
		m.st.Scanning = false
		m.st.Snap = msg.snap
		// The display clock only moves forward. snap.At is the scan's START
		// time, so adopting it here would jump every AGE cell backwards by the
		// scan duration and zero the staleness notice for a frame.
		if msg.snap.At.After(m.st.Now) {
			m.st.Now = msg.snap.At
		}
		m.clampScroll()
		return m, nil
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "v":
		m.st.Filter = m.st.Filter.Next()
		m.st.Scroll = 0
	case "s":
		m.st.Sort = m.st.Sort.Next()
		m.st.Scroll = 0
	case "a":
		m.st.ShowAll = !m.st.ShowAll
		m.st.Scroll = 0
	case "r":
		if !m.inFlight {
			m.inFlight = true
			return m, m.scanCmd()
		}
	case "?":
		m.st.Help = !m.st.Help
		m.st.Scroll = 0
	case "up", "k":
		if m.st.Scroll > 0 {
			m.st.Scroll--
		}
	case "down", "j":
		m.st.Scroll++
		m.clampScroll()
	}
	return m, nil
}

func (m *Model) clampScroll() {
	n := len(visibleSessions(m.st))
	if m.st.Scroll > n-1 {
		m.st.Scroll = n - 1
	}
	if m.st.Scroll < 0 {
		m.st.Scroll = 0
	}
}

// View is a thin wrapper over the pure renderer. It calls no clock: State.Now
// was stamped when the tick arrived.
func (m *Model) View() tea.View {
	if m.st.Width == 0 || m.st.Height == 0 {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	v := tea.NewView(Render(m.st, m.styles, m.glyphs))
	v.AltScreen = true
	// The cursor stays nil: a monitor has nothing to type into, and a blinking
	// block parked on a row reads as a selection that does not exist.
	v.Cursor = nil
	if !m.opts.NoTitle {
		v.WindowTitle = "telltale"
	}
	return v
}

// Run starts the program. The HUD is strictly read-only: no keybinding mutates
// vendor state or sends anything to a running agent.
func Run(opts Options) error {
	p := tea.NewProgram(New(opts))
	_, err := p.Run()
	return err
}

var _ tea.Model = (*Model)(nil)
