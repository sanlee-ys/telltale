package council

import (
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/vt"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The live seat: a pane that draws a vendor's REAL terminal screen, beside the
// measured seats and contributing nothing to them (design.md §9.53).
//
// The whole file is organised around one boundary. The emulator holds the raw
// stream and lives on Model; State gets decoded rows and nothing else. Render
// draws those rows. No gauge, badge, cost or posture is ever read from here,
// because a screen of ANSI is a picture of a program and a number taken off a
// picture is inferred rather than measured — the one thing ADR-001 refuses.
//
// The room's OTHER Claude seat is untouched by all of this. It keeps running
// `claude --input-format stream-json --output-format stream-json`, it keeps
// supplying every figure the column renders, and the live child is a second
// process in its own interactive mode. That doubling is real spend and the
// pane says so in a word.

// ptyBuffer bounds the chunk channel, and blocking on it is the point.
//
// The same decision dispatch.go argues for the event channel: a full channel
// stalls the child rather than dropping its output, and a stalled pane is
// honest where a lagging one is not. 64 chunks of 16 KiB is about a megabyte of
// slack, and a 5000-line flood measured 369 KB in six and a half seconds.
const ptyBuffer = 64

// ptyDrainMax caps how many chunks one Update applies before it re-arms.
//
// The emulator holds the screen, so draining more chunks costs one Write each
// and still produces ONE snapshot — which is exactly what makes a flood cheap
// here and is the same argument drainMax makes for text.
const ptyDrainMax = 64

// liveScrollback is how much history the emulator keeps behind the pane.
//
// Kept at zero deliberately. The pane draws the emulator's current screen and
// has no scroll key, so a scrollback buffer would be a copy of a private
// conversation held in memory that nothing can read — which resume.go already
// ruled against for the room's own transcript. The guest's scrollback stays
// inside the guest.
const liveScrollback = 0

// ptyBatchMsg carries the chunks one wait drained.
type ptyBatchMsg struct{ chunks []runner.PTYChunk }

// startPTYSession is declared in persistent.go beside the other spawn vars.
// It is named here only so the reader of this file knows where the process
// actually starts.

// liveVendor is the one seat that can take a live pane, and the check is a
// property of the ADAPTER rather than a name typed into a list.
//
// vendors.Persistent has exactly one implementer. Codex, Antigravity and Grok
// are batch programs that exit every turn, so a pseudoconsole on one of them is
// a pane that is empty between turns; Cursor speaks ACP JSON-RPC and draws no
// terminal UI at all. Asking the registry rather than hard-coding `claude` is
// what makes this claim keep up with the registry: a vendor that becomes
// persistent later becomes eligible here without a second edit, and one that
// stops being persistent stops being offered.
func liveVendor(v model.VendorID) bool {
	drv, ok := vendors.Registry()[v]
	if !ok {
		return false
	}
	_, persistent := drv.(vendors.Persistent)
	return persistent
}

// ErrNoLiveSeat is what ParseLive says about a seat that cannot be seated live.
var ErrNoLiveSeat = errors.New(
	"only a vendor that keeps one process across turns can take a live seat")

// ParseLive turns the --live flag's value into a seat.
//
// It lives here rather than in cmd/telltale for the reason ParseSeats does: the
// command line hands over a string and learns nothing about vendors, so which
// seats exist and which of them can be live is answered in one place.
//
// An empty value is not an error. It is the ordinary room, with no live pane.
func ParseLive(s string) (model.VendorID, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", nil
	}
	v := model.VendorID(s)
	if _, ok := vendors.Registry()[v]; !ok {
		return "", errors.New("unknown seat for --live: " + s)
	}
	if !liveVendor(v) {
		return "", ErrNoLiveSeat
	}
	return v, nil
}

// liveCmd starts the live child, or records why it could not be started.
//
// Called from Init and from nowhere else, which is the placement the rebuild
// uses for the same reason (§9.52): no test calls Init, so a Model a test
// builds directly starts no pseudoconsole, and the package's spawn guard needs
// no exception for this path.
//
// The size here is PROVISIONAL. Init runs before the terminal has said how big
// it is, so the child opens at a conventional 80x24 and the first
// tea.WindowSizeMsg resizes it to the pane it actually got. A guessed size that
// is never corrected would be the dishonest version; a guessed size that is
// corrected within one frame is just a start.
func (m *Model) liveCmd() tea.Cmd {
	seat := m.opts.Live
	if seat == "" {
		return nil
	}
	m.st.Live.Seat = seat
	if !liveVendor(seat) {
		m.st.Live.Phase = LiveUnavailable
		m.st.Live.Note = string(seat) + " cannot take a live seat: it does not keep one process across turns"
		return nil
	}
	col := m.columnFor(seat)
	if col == nil || col.Avail != AvailInstalled {
		m.st.Live.Phase = LiveUnavailable
		m.st.Live.Note = "this machine cannot run " + string(seat) + ", so there is no screen to show"
		return nil
	}

	const provisionalCols, provisionalRows = 80, 24
	m.ptyChunks = make(chan runner.PTYChunk, ptyBuffer)
	// The live invocation is NOT council's invocation. Council drives this
	// vendor with --input-format stream-json; the pane wants the program a
	// person would get by typing its name, which is a different mode with a
	// different contract. Args are empty on purpose, and that emptiness is the
	// whole difference between the two children.
	spec := runner.Spec{Vendor: seat, Binary: col.Binary, Dir: m.st.Workspace}
	sess, err := startPTYSession(m.roomCtx, spec, provisionalCols, provisionalRows, m.ptyChunks)
	if err != nil {
		m.ptyChunks = nil
		m.st.Live.Phase = LiveUnavailable
		m.st.Live.Note = err.Error()
		return nil
	}
	m.live = sess
	m.emu = vt.NewEmulator(provisionalCols, provisionalRows)
	m.emu.SetScrollbackSize(liveScrollback)
	m.liveCols, m.liveRows = provisionalCols, provisionalRows
	m.st.Live.Phase = LiveOpening
	m.st.Live.Cols, m.st.Live.Rows = provisionalCols, provisionalRows
	return m.waitPTY()
}

// columnFor finds a seat's column, or nil.
func (m *Model) columnFor(v model.VendorID) *Column {
	for i := range m.st.Columns {
		if m.st.Columns[i].Vendor == v {
			return &m.st.Columns[i]
		}
	}
	return nil
}

// waitPTY blocks on one chunk, then drains what is already queued.
//
// The shape of waitEvents (dispatch.go), and it has to be: a Cmd that polled
// would burn a goroutine on an idle pane, and a measured idle agent CLI costs
// about 36 bytes a second. Blocking is what makes an idle pane free.
func (m *Model) waitPTY() tea.Cmd {
	ch := m.ptyChunks
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		first, ok := <-ch
		if !ok {
			return ptyBatchMsg{}
		}
		batch := []runner.PTYChunk{first}
		for len(batch) < ptyDrainMax {
			select {
			case c, ok := <-ch:
				if !ok {
					return ptyBatchMsg{chunks: batch}
				}
				batch = append(batch, c)
			default:
				return ptyBatchMsg{chunks: batch}
			}
		}
		return ptyBatchMsg{chunks: batch}
	}
}

// applyPTY feeds the batch to the emulator and copies ONE snapshot onto State.
//
// This function is the display-only contract in code, and it is written to be
// checkable rather than merely intended: the only field of State it assigns is
// st.Live. TestLiveSeatMeasuresNothing compares the whole State across a call
// and demands that is the only difference, so a later edit that reached for a
// cost or a phase here fails a test rather than a review.
//
// One snapshot per batch, not per chunk. The emulator already holds every byte
// the batch carried, so the coalescing costs nothing and turns a flood into one
// grid copy per frame.
func (m *Model) applyPTY(chunks []runner.PTYChunk) {
	if m.emu == nil {
		return
	}
	ended, note := false, ""
	for _, c := range chunks {
		if len(c.Data) > 0 {
			// The raw bytes go HERE and nowhere else. Every escape in them —
			// the window resize, the terminal-version query, the win32 input
			// mode set — is consumed by this call and never reaches a rendered
			// line (§9.53).
			_, _ = m.emu.Write(c.Data)
		}
		if c.Done {
			ended = true
			if note == "" {
				note = c.Note
			}
		}
	}
	m.st.Live.Grid = m.liveGrid()
	switch {
	case ended:
		m.st.Live.Phase = LiveEnded
		m.st.Live.Note = note
		m.ptyChunks = nil
		m.live = nil
	case m.st.Live.Phase == LiveOpening:
		m.st.Live.Phase = LiveShowing
	}
}

// liveGrid reads the emulator's current screen as plain rows.
//
// Cell text only. The guest's colours are dropped, and §9.53 argues the three
// reasons: a golden may not embed vendor ANSI, colour in this room is always a
// second signal for a claim telltale is making, and a styled row clipped at the
// pane edge leaves an open colour run bleeding into its neighbour.
//
// Trailing blanks are trimmed per row because the renderer pads every line to
// the pane width anyway, and a row of spaces that survives into a golden is a
// row whose real content nobody can see in a diff.
func (m *Model) liveGrid() []string {
	if m.emu == nil {
		return nil
	}
	w, h := m.emu.Width(), m.emu.Height()
	rows := make([]string, 0, h)
	var b strings.Builder
	for y := 0; y < h; y++ {
		b.Reset()
		for x := 0; x < w; x++ {
			c := m.emu.CellAt(x, y)
			if c == nil || c.String() == "" {
				b.WriteString(" ")
				continue
			}
			b.WriteString(c.String())
		}
		rows = append(rows, strings.TrimRight(b.String(), " "))
	}
	return rows
}

// syncLiveSize resizes the pseudoconsole and the emulator TOGETHER.
//
// Together, because doing one without the other leaves the guest painting to a
// width the grid does not have — which shows up as a torn pane and reads like a
// rendering bug rather than a missed call.
//
// Called from Update, never from Render. livePaneRect is pure over State, so
// the update loop can ask the same question the renderer will ask without
// giving the renderer anything to do but draw.
func (m *Model) syncLiveSize() {
	if m.live == nil || m.emu == nil {
		return
	}
	cols, rows, ok := livePaneRect(m.st)
	if !ok || (cols == m.liveCols && rows == m.liveRows) {
		return
	}
	m.liveCols, m.liveRows = cols, rows
	_ = m.live.Resize(cols, rows)
	m.emu.Resize(cols, rows)
	m.st.Live.Cols, m.st.Live.Rows = cols, rows
	m.st.Live.Grid = m.liveGrid()
}

// killLive ends the live child.
//
// Separate from the roomCtx cancellation that would also reach it, because
// teardown counts what it ended and a seat that died from a cancelled context
// somewhere below would not be counted. The room says how many agents it ended
// (§9.52), and the live child is an agent.
func (m *Model) killLive() bool {
	if m.live == nil {
		return false
	}
	m.live.Kill()
	m.live = nil
	return true
}

// livePaneRect is the cell rectangle the live pane will be drawn into.
//
// Pure over State, and that is what lets Update ask it. It answers false
// whenever the pane is not on screen — a body that replaced the grid, a seat
// that folded out, a tabs tier showing a different seat — and a false answer
// means "do not resize", not "resize to nothing": a pane the operator cannot
// see must not make the guest repaint its whole screen.
func livePaneRect(st State) (cols, rows int, ok bool) {
	if st.Live.Seat == "" || st.Live.Phase == LiveOff {
		return 0, 0, false
	}
	// The help panel, the arena record and the turn page each replace the
	// column area outright. layoutFor already plans those as one full-width
	// reading area, so there is no pane to measure.
	if st.Help != HelpClosed || st.Page.Open || st.Record != nil {
		return 0, 0, false
	}
	idx := -1
	for i, c := range st.Columns {
		if c.Vendor == st.Live.Seat {
			idx = i
			break
		}
	}
	if idx < 0 {
		return 0, 0, false
	}
	pos := -1
	for j, v := range st.VisibleColumns() {
		if v == idx {
			pos = j
			break
		}
	}
	if pos < 0 {
		return 0, 0, false
	}
	if st.Width < MinWidth || st.Height < MinHeight {
		return 0, 0, false
	}
	g := GlyphsFor(st.ASCII)
	lay := layoutFor(st, g)
	w := lay.widthAt(pos)
	if lay.Tier == TierTabs {
		// One column is drawn on this tier and it is the focused one, so a live
		// seat that is not focused is not on screen at all.
		if st.Focus != idx {
			return 0, 0, false
		}
		w = lay.ColWidth
	}
	// The chrome is measured by drawing it, never by a constant — columnViewport
	// makes the same call for the same reason, and the constant that used to sit
	// in its place was already wrong for a column with nothing to claim.
	// PlainStyles because only the line COUNT is wanted, and every style in this
	// package is a wrapper that cannot change it.
	h := lay.Body - len(columnChrome(st, st.Columns[idx], seatUnfocused, w, PlainStyles(), g)) - liveMarkerRows
	if w < 1 || h < 1 {
		return 0, 0, false
	}
	return w, h, true
}
