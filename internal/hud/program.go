package hud

import (
	"context"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/quotacache"
	"github.com/sanlee-ys/telltale/internal/usagecache"
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

	// Hide drops these vendors from every body for the whole run (§7.20). It
	// is applied to the snapshot as each scan lands — not in Render — so the
	// grid, the vendor lines, the quota strip, the u page and the w page all
	// agree without each checking the list. The footer states the hide for as
	// long as it is in force.
	Hide []model.VendorID

	// Root is the substitute store root (`--root`): the Adapters above were
	// rooted beneath this directory instead of this machine's own stores.
	// The HUD does not act on it — the adapters are already rooted — it
	// exists so the footer can STATE the substitution for the whole run.
	// A frame whose every row comes from a corpus must say so on screen;
	// anything else is the invented-gauge lie with extra steps.
	Root string
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

	// selectedKey is the Vendor/ID of the row under the cursor. State.Cursor
	// is an INDEX because that is what Render needs; this is the identity that
	// index is supposed to mean, kept here so a re-sort between scans cannot
	// move the selection to a different session behind the user's back.
	selectedKey string
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
	st.Hidden = opts.Hide
	st.Root = opts.Root
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
		snap := Scan(ctx, adapters, time.Now())
		// The statusline's relayed quota rides the same scan cadence as the
		// vendor stores (design.md §7.15). Read here, off the Update goroutine
		// and outside Render, so the render path stays pure over State; a
		// missing dir or unreadable entry contributes nothing, same as an
		// absent vendor.
		if dir, err := quotacache.Dir(); err == nil {
			snap.Account = quotacache.ReadAll(dir, snap.At)
		}
		// The hook-relayed token totals ride the same cadence and are read the
		// same way, for the same reason (design.md §7.16). Two caches rather
		// than one field because they are two measurements: quota has a
		// ceiling the vendor published, and this has none at all.
		if dir, err := usagecache.Dir(); err == nil {
			snap.Spend = usagecache.ReadAll(dir, snap.At)
		}
		return scanResultMsg{snap: dropHidden(snap, m.opts.Hide)}
	}
}

// dropHidden strips the hide list's vendors from a snapshot: sessions, vendor
// lines, relayed account quota and relayed spend alike.
//
// It runs here, once per scan, rather than as a check in each render path,
// because the snapshot is the one place all four bodies read from — a hide
// applied to the grid but forgotten by the week page would show a vendor the
// footer claims is hidden, which is the exact disagreement §7.20 forbids. The
// header's session count and per-vendor census come from the same slices, so
// the numbers stay consistent with the rows for free.
func dropHidden(snap Snapshot, hide []model.VendorID) Snapshot {
	if len(hide) == 0 {
		return snap
	}
	hidden := func(v model.VendorID) bool {
		for _, h := range hide {
			if h == v {
				return true
			}
		}
		return false
	}
	sessions := snap.Sessions[:0:0]
	for _, s := range snap.Sessions {
		if !hidden(s.Vendor) {
			sessions = append(sessions, s)
		}
	}
	snap.Sessions = sessions
	vendors := snap.Vendors[:0:0]
	for _, v := range snap.Vendors {
		if !hidden(v.Vendor) {
			vendors = append(vendors, v)
		}
	}
	snap.Vendors = vendors
	account := snap.Account[:0:0]
	for _, a := range snap.Account {
		if !hidden(a.Vendor) {
			account = append(account, a)
		}
	}
	snap.Account = account
	spend := snap.Spend[:0:0]
	for _, s := range snap.Spend {
		if !hidden(s.Vendor) {
			spend = append(spend, s)
		}
	}
	snap.Spend = spend
	return snap
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
		// The visible set is a function of Now (idle cutoff, clamped ages), so
		// a tick alone can change it. Re-resolving the selection here keeps the
		// cursor pointing at the same session rather than the same index
		// (review finding: a row crossing the cutoff shifted the selection).
		m.resyncSelection()
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
		// Sample the account quota AFTER the snapshot lands, using the same
		// windows the header renders, so the forecast can never describe a
		// window that is not on screen. One sample per completed scan: a
		// failed scan contributes nothing rather than a repeat of the last
		// reading, which would flatten the measured slope with data we did not
		// measure. The source identity resets the series when the sampled
		// session changes.
		windows, source := accountQuotaSource(m.st)
		m.st.Burn.Observe(windows, source, m.st.Now)
		m.resyncSelection()
		return m, nil
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Find mode swallows the keyboard. This is the whole reason it announces
	// itself in the footer: while it is on, "q" is a letter, not a command,
	// and a mode that changes what an unmodified key means without saying so
	// is how a read-only monitor surprises someone.
	if m.st.Finding {
		return m.findKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		// esc unwinds one layer at a time and only quits from the bottom one.
		// v1's esc quit unconditionally; with a pane and a query to back out
		// of, an esc that closed the program instead of the pane would punish
		// the reflex it trained.
		//
		// The week page takes the first step. Order among the bodies is not
		// arbitrary: they are mutually exclusive, so at most one of the first
		// four cases can be true, and listing the newest first keeps the
		// chain reading as "close whatever is open, then unwind the state
		// underneath it".
		switch {
		case m.st.Week:
			m.st.Week = false
			m.st.Scroll = 0
		case m.st.Usage:
			m.st.Usage = false
			m.st.Scroll = 0
		case m.st.Detail:
			m.st.Detail = false
		case m.st.Help:
			m.st.Help = false
		case m.st.Query != "":
			m.setQuery("")
		default:
			return m, tea.Quit
		}
	case "enter":
		if !m.st.Detail && len(visibleSessions(m.st)) == 0 {
			// enter over an empty row area has no session to detail. Opening
			// the pane anyway would replace the empty-state message (which
			// carries the vendor status the user needs) with a claim about a
			// session that was never selected (review finding).
			break
		}
		m.st.Detail = !m.st.Detail
		if m.st.Detail {
			m.st.Help = false
			m.st.Usage = false
			m.st.Week = false
			m.st.Scroll = 0
			if m.st.Cursor < 0 {
				// enter with no selection opens the top row: the sort already
				// put the most interesting session there.
				m.moveCursor(0)
			}
		}
	case "/":
		m.st.Finding = true
		m.st.Detail = false
		m.st.Help = false
		m.st.Usage = false
		m.st.Week = false
		m.st.Scroll = 0
	case "u":
		// One body at a time (§7.17). The usage view replaces the row area the
		// same way the detail pane and the help overlay do, and two of them
		// open at once is not a layout to resolve — it is a state that must
		// not exist, which is why every door closes the others rather than
		// Render picking a winner.
		m.st.Usage = !m.st.Usage
		m.st.Detail = false
		m.st.Help = false
		m.st.Week = false
		m.st.Scroll = 0
	case "w":
		// The week page (§7.19) is a sibling door to `u`, not a mode inside
		// it: one key, one body, and the same one-body-at-a-time rule.
		m.st.Week = !m.st.Week
		m.st.Detail = false
		m.st.Help = false
		m.st.Usage = false
		m.st.Scroll = 0
	case "v":
		// The cycle skips hidden vendors: a filter that can only ever select
		// an empty grid is a dead stop on a one-key cycle. FilterAll has no
		// vendor and is never skipped, so the loop always terminates there.
		f := m.st.Filter.Next()
		for {
			v, ok := f.VendorID()
			if !ok || !m.st.hiddenHas(v) {
				break
			}
			f = f.Next()
		}
		m.st.Filter = f
		m.resetSelection()
	case "s":
		m.st.Sort = m.st.Sort.Next()
		m.resetSelection()
	case "a":
		m.st.ShowAll = !m.st.ShowAll
		m.resetSelection()
	case "r":
		if !m.inFlight {
			m.inFlight = true
			return m, m.scanCmd()
		}
	case "?":
		m.st.Help = !m.st.Help
		m.st.Detail = false
		m.st.Usage = false
		m.st.Week = false
		m.st.Scroll = 0
	case "up", "k":
		// Over the help overlay, the usage view and the week page the arrows
		// scroll the body; everywhere else they move the selection. The rule
		// is "the arrows move whatever the body is": none of those bodies has
		// rows to select, and the row area has nothing to scroll independently
		// of the cursor.
		if m.st.Help || m.st.Usage || m.st.Week {
			if m.st.Scroll > 0 {
				m.st.Scroll--
			}
			break
		}
		m.moveCursor(-1)
	case "down", "j":
		if m.st.Help || m.st.Usage || m.st.Week {
			// Bounded against the body's own length rather than left to grow:
			// Render clamps the viewport anyway, but an offset that keeps
			// counting past the end takes as many keypresses to come back as
			// it took to leave.
			if m.st.Scroll < len(m.scrollBody())-1 {
				m.st.Scroll++
			}
			break
		}
		m.moveCursor(+1)
	}
	return m, nil
}

// scrollBody renders the currently open scrollable body off-screen purely to
// measure it. Cheap, and it cannot disagree with what Render draws — which is
// the whole reason the bound is taken this way rather than from a length
// someone has to remember to update.
func (m *Model) scrollBody() []string {
	if m.st.Usage {
		// Room zero, which asks for the TIGHT page — one blank row between
		// blocks — and that is the right bound rather than a convenient one.
		// usageAir only widens a gap when the widened page still fits its
		// region, so an aired page can never overflow and can never be
		// scrollable; the only body scrolling ever applies to is this one.
		// Measuring the aired page here would hand `j` a bound that describes a
		// layout the reader is not looking at.
		return usageLines(m.st, 0, m.styles, m.glyphs)
	}
	if m.st.Week {
		return weekLines(m.st, m.styles, m.glyphs)
	}
	rows := visibleSessions(m.st)
	hasCtx, hasCost := columnsInUse(rows)
	lay := resolveLayout(m.st.Width, hasCtx, hasCost)
	return helpLines(m.st, lay, hasCtx, hasCost, m.styles, m.glyphs)
}

// findKey handles the one mode. Only four keys are commands; everything else
// that carries text is text.
func (m *Model) findKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// The one key that always means the same thing.
		return m, tea.Quit
	case "esc":
		// esc CLEARS. Leaving the query applied on the way out would hide rows
		// behind a mode the user just cancelled.
		m.st.Finding = false
		m.setQuery("")
	case "enter":
		m.st.Finding = false
	case "backspace":
		if q := []rune(m.st.Query); len(q) > 0 {
			m.setQuery(string(q[:len(q)-1]))
		}
	default:
		if t := msg.Text; t != "" {
			m.setQuery(m.st.Query + t)
		}
	}
	return m, nil
}

// setQuery changes the find query and drops the selection with it: the cursor
// is an index into the visible rows, and a different row set makes the old
// index point at a different session.
func (m *Model) setQuery(q string) {
	if q == m.st.Query {
		return
	}
	m.st.Query = q
	m.resetSelection()
}

// resetSelection clears the cursor and the viewport after any change to which
// rows are visible or in what order.
func (m *Model) resetSelection() {
	m.st.Cursor = -1
	m.selectedKey = ""
	m.st.Scroll = 0
	if m.st.Detail {
		// The pane's subject just stopped being well defined. Closing it is
		// the honest move; silently repointing it at whatever now sits at the
		// old index would relabel one session's diagnostics with another's.
		m.st.Detail = false
	}
}

// moveCursor steps the selection, or plants it if there is none yet.
//
// delta 0 means "select the first row". The cursor starts at -1 and the mark
// appears only once the user asks for it, so nothing on the steady-state
// screen implies a row was chosen for them.
func (m *Model) moveCursor(delta int) {
	rows := visibleSessions(m.st)
	if len(rows) == 0 {
		m.st.Cursor = -1
		m.selectedKey = ""
		return
	}
	next := 0
	if m.st.Cursor >= 0 {
		next = m.st.Cursor + delta
	} else if delta > 0 {
		next = 0
	} else if delta < 0 {
		next = 0
	}
	if next < 0 {
		next = 0
	}
	if next > len(rows)-1 {
		next = len(rows) - 1
	}
	m.st.Cursor = next
	m.selectedKey = rows[next].Key()
}

// resyncSelection re-points the cursor at the session it was on before the
// scan, by KEY rather than by index.
//
// Sorting is by activity, so a session that just wrote a record can jump to
// the top and shift every row under it. Holding the index would silently move
// the selection — and with the detail pane open, would relabel one session's
// diagnostics with another's.
func (m *Model) resyncSelection() {
	if m.selectedKey == "" {
		return
	}
	rows := visibleSessions(m.st)
	for i, s := range rows {
		if s.Key() == m.selectedKey {
			m.st.Cursor = i
			return
		}
	}
	// The selected session is gone. Say nothing new: drop the selection and
	// close the pane rather than retarget it.
	m.st.Cursor = -1
	m.selectedKey = ""
	m.st.Detail = false
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
