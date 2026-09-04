package council

import (
	"os"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The hosted room, drawn by the room's own Model (design.md §7.31).
//
// # What this file is, and what it is not
//
// It is the CLIENT half of a hosted room: a Model whose State is projected from
// the host's Room on every frame (stateFromRoom), whose enter sends a dispatch
// frame instead of spawning, and whose ctrl+c sends an interrupt frame instead
// of killing. Everything the room already knows how to draw — the columns, the
// badges, the trace, the turn page, the panes, the help panel — draws over that
// State unchanged, because Render is pure over State and always was.
//
// It is NOT a second fold. The host owns the conversation and this process
// owns the view. stateFromRoom joins them: the conversation comes from the
// seat, the view comes from the previous column with the same vendor. The
// three shapes §7.31 weighed and the reason this one won are in that section;
// the short version is that councilhost cannot import council (boundary_test.go
// pins the direction), a State on the wire would let the host own the reader's
// focus, and an event stream would need a replay the host refuses to hold.
//
// # What a hosted room refuses, and why each refusal is a sentence
//
// Every control that changes a seat's process, tree, thread or gate is refused
// here with a notice naming the room it works in, because the client holds
// none of those: /cd, /seat, /unseat, /read, /write, /arena, /flow, /hand,
// /adopt, /retry, /trace, c, x, a, s and ctrl+r. A key that did nothing would
// be §7.8's surprise; a key that pretended would be worse.

// hostLink is the client half of a hosted room as the Model drives it.
//
// An interface for seatSession's reason: *councilhost.Client wraps a real pipe
// handle and has no way to be faked, and every test of the hosted room's KEYS
// — what enter sends, what ctrl+c stops, what /detach waits for — is a test
// about frames and not about pipes. The stubbed end-to-end (hosted_e2e_test.go)
// hands a real Client over a real pipe to the same code.
type hostLink interface {
	HostPID() int
	Dispatch(prompt string, seats ...model.VendorID) error
	Interrupt(seats ...model.VendorID) error
	RequestDetach() error
	NextFrame() (councilhost.Frame, error)
	CloseDetached() error
	Close() error
}

// hostedRun is the Model's side of a hosted room: the link, and how the run
// ended. On Model and never on State, the boundary gateInputs and the brief
// keep — Render reads the pid off State.Hosted and nothing else.
type hostedRun struct {
	link hostLink
	// windows decides the posture claim's platform branch, resolved once at
	// room open so stateFromRoom reads no environment.
	windows bool
	// outcome and err are how the run ended, read by runHostedTUI after the
	// program returns. outcome is meaningful only once the program has quit.
	outcome councilhost.Outcome
	err     error
	// closed reports that the link was closed (a shutdown or a detach), so a
	// second path out — a signal after q — cannot close it twice.
	closed bool
	// lastNotice is the host's own notice as of the last frame, so a notice
	// the host repeats on every frame lands once and a newer local notice is
	// not overwritten by an old host one.
	lastNotice string
}

// hostFrameMsg is one frame off the wire, or the error that ended the stream.
type hostFrameMsg struct {
	frame councilhost.Frame
	err   error
}

// newHostedModel builds the room over the host's first frame.
//
// The State comes from the HOST for the conversation and from this machine for
// the view: the posture claim for each vendor, the granularity, the label, the
// home directory the header abbreviates against. No detection runs and no
// saved room is read — the host was handed both when it opened, and a second
// reading here could disagree with the room it is drawing.
func newHostedModel(opts Options, first councilhost.Room, link hostLink, notice string) *Model {
	st := NewState()
	st.ASCII = opts.ASCII
	st.Seats = opts.Seats
	st.HeadroomWarn = opts.Headroom
	st.Now = time.Now()
	if home, err := os.UserHomeDir(); err == nil {
		st.Home = home
	}
	st.Hosted = HostedRoom{PID: link.HostPID()}
	windows := runtime.GOOS == "windows"
	st = stateFromRoom(first, st, windows)
	if vis := st.VisibleColumns(); len(vis) > 0 {
		st.Focus = vis[0]
	}
	st.Notice = notice
	m := newModel(opts, st)
	m.hosted = &hostedRun{link: link, windows: windows, lastNotice: first.Notice}
	if first.Notice != "" && notice == "" {
		m.st.Notice = first.Notice
	}
	return m
}

// stateFromRoom projects the host's room onto a State, keeping the view.
//
// PURE over its arguments, and TestHostedStateIsPureOverTheRoom holds it there:
// no clock, no file, no environment. That is what lets a rejoining client draw
// the whole current room from its first frame — the frame IS the state, and a
// second client and a client that has been watching both build the same State
// from it.
//
// What comes from the SEAT is the conversation: phase, body, prompt, acts,
// history, note, clock, cost, the linger, the skip. What comes from the
// PREVIOUS COLUMN with the same vendor is the view: scroll, follow, when the
// reader last looked, the quota reading this client read from its own relay.
// A seat on a new turn re-arms its tail, the way startTurn does.
//
// Every body, act and note passes through Redact and sanitize on the way in.
// The host does not redact; this is the room's one choke point, applied to a
// whole string instead of a stream (applyEvents' rule).
func stateFromRoom(r councilhost.Room, prev State, windows bool) State {
	st := prev
	st.Workspace = r.Workspace
	st.Turn = r.Turn
	st.Write = r.Posture == "write"
	// A hosted room asks nobody: §7.28 refuses to host a gated room, so a
	// hosted write room runs every tool call as though --auto were typed. The
	// border says `not asking` for exactly that reason (composerLabel).
	st.GateOff = st.Write

	prevCols := make(map[model.VendorID]Column, len(prev.Columns))
	for _, c := range prev.Columns {
		prevCols[c.Vendor] = c
	}
	cols := make([]Column, 0, len(r.Seats))
	for _, s := range r.Seats {
		cols = append(cols, columnFromSeat(s, prevCols[s.Vendor], st.Write, windows))
	}
	st.Columns = cols

	// Focus follows the VENDOR, not the index: the roster's order is the
	// host's and does not move, but a reader's keys must never land on a
	// different seat because a frame arrived.
	if prev.Focus >= 0 && prev.Focus < len(prev.Columns) {
		want := prev.Columns[prev.Focus].Vendor
		for i, c := range cols {
			if c.Vendor == want {
				st.Focus = i
			}
		}
	}
	if st.Focus < 0 || st.Focus >= len(cols) {
		st.Focus = 0
	}

	// The live route retires the moment no seat on the latest turn is still
	// answering — turnColumnFinished's rule, read off the columns because
	// the client holds no turn of its own (§9.21, §9.54).
	if st.TurnRoute != nil {
		live := false
		for _, c := range cols {
			if c.TurnN == st.Turn && c.inFlight() {
				live = true
			}
		}
		if !live {
			st.TurnRoute = nil
		}
	}
	return st
}

// columnFromSeat is one seat onto one column, over the previous column's view.
func columnFromSeat(s councilhost.Seat, prev Column, write, windows bool) Column {
	c := prev
	fresh := prev.Vendor == ""
	c.Vendor = s.Vendor
	c.Label = vendorLabel(s.Vendor)
	c.Binary = s.Binary
	c.Gran = granularityFor(s.Vendor)
	// The claim this machine makes for this vendor in this posture. A hosted
	// room is never gated, so gated and hooked are false; a seat the host
	// drives through its batch adapter wears that adapter's measured claim
	// (fallback.go's own spelling).
	c.Sandbox = postureClaimFor(s.Vendor, windows, write, false, false, s.FellBack)
	switch {
	case s.Drivable:
		c.Avail = AvailInstalled
	case s.Binary == "":
		c.Avail = AvailNotInstalled
	default:
		// A binary the host resolved and will not drive: cursor's
		// request/response protocol. The card carries the host's own reason.
		c.Avail = AvailUnusable
	}
	c.Phase = phaseFromWire(s.Phase)
	c.Body = cleanWire(s.Body)
	c.Prompt = sanitize(s.Prompt)
	// A hosted room carries no rebuttal (§7.31 names it as owed), so nothing
	// rode along with any brief.
	c.Quoted = false
	if fresh || s.Turn != prev.TurnN {
		// A new turn re-arms the tail, startTurn's rule: what arrives now is
		// what the reader is waiting for. The history is still scrollable.
		c.Follow = true
		c.Scroll = 0
	}
	c.TurnN = s.Turn
	c.Acts = actsFromWire(s.Acts)
	c.History = historyFromWire(s.History)
	c.Note = cleanWire(s.Note)
	c.NoteDetail = cleanWire(s.NoteDetail)
	c.Skipped = s.Skipped
	c.NoteCalm = false
	if s.ExitCode != nil && *s.ExitCode != 0 && c.Note == "" {
		// The exit named itself and nothing else did. The single-process room
		// gets this sentence from the runner's own error; here the code is
		// what crossed the wire, so the sentence is composed from it and from
		// nothing else.
		c.Note = "exit status " + itoa(*s.ExitCode)
	}
	c.Settling = s.Settling
	c.Started = s.Started
	c.Ended = s.Ended
	c.Elapsed = s.Elapsed
	// A hosted room raises no gate, so no operator share was ever measured:
	// UNMEASURED, not zero (§9.45).
	c.GateWait = runner.Span{}
	c.CostUSD = s.CostUSD
	c.CostSession = s.CostSession
	c.Tokens = tokensFromWire(s.Tokens)
	// None of these happen in a hosted room, and a value carried across from
	// a previous column would be a claim about a seat the host never made.
	c.Restored = false
	c.Cleared = false
	c.Arena = nil
	c.ArenaInterim = nil
	c.ArenaShowDiff = false
	c.ArenaHunk = 0
	// The host runs every seat in the workspace and stamps no containment,
	// so the badge row carries no claim rather than a guessed one (§9.55).
	c.Containment = ContainClaim{}
	return c
}

// cleanWire is the room's choke point for vendor text arriving whole.
func cleanWire(s string) string { return sanitize(Redact(s)) }

func phaseFromWire(p councilhost.Phase) Phase {
	switch p {
	case councilhost.PhaseWaiting:
		return PhaseWaiting
	case councilhost.PhaseStreaming:
		return PhaseStreaming
	case councilhost.PhaseDone:
		return PhaseDone
	case councilhost.PhaseFailed:
		return PhaseFailed
	case councilhost.PhaseCancelled:
		return PhaseCancelled
	default:
		// Idle, and undrivable: the availability carries the second one.
		return PhaseIdle
	}
}

func actStatusFromWire(s string) runner.ActStatus {
	switch s {
	case councilhost.ActOK:
		return runner.ActOK
	case councilhost.ActFailed:
		return runner.ActFailed
	case councilhost.ActUnknown:
		return runner.ActUnknown
	case councilhost.ActDenied:
		return runner.ActDenied
	default:
		return runner.ActPending
	}
}

func actsFromWire(in []councilhost.Act) []Act {
	if len(in) == 0 {
		return nil
	}
	out := make([]Act, 0, len(in))
	for _, a := range in {
		out = append(out, Act{
			ID:     a.ID,
			Text:   cleanWire(a.Text),
			Status: actStatusFromWire(a.Status),
			Detail: cleanWire(a.Detail),
		})
	}
	return out
}

func historyFromWire(in []councilhost.TurnRecord) []TurnRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]TurnRecord, 0, len(in))
	for _, h := range in {
		rec := TurnRecord{
			N:           h.N,
			Prompt:      sanitize(h.Prompt),
			Body:        cleanWire(h.Body),
			Acts:        actsFromWire(h.Acts),
			Note:        cleanWire(h.Note),
			NoteDetail:  cleanWire(h.NoteDetail),
			Elapsed:     h.Elapsed,
			CostUSD:     h.CostUSD,
			CostSession: h.CostSession,
			Tokens:      tokensFromWire(h.Tokens),
			Phase:       phaseFromWire(h.Phase),
		}
		if h.ExitCode != nil && *h.ExitCode != 0 && rec.Note == "" {
			rec.Note = "exit status " + itoa(*h.ExitCode)
		}
		out = append(out, rec)
	}
	return out
}

// tokensFromWire is a reported count as the host sent it, or nil. Two
// integers cross as two integers; nil stays nil, so a hosted seat that
// reported no count draws no cell, exactly as the single-process room does.
func tokensFromWire(t *councilhost.TokenCounts) *model.TokenCounts {
	if t == nil {
		return nil
	}
	return &model.TokenCounts{Input: t.In, Output: t.Out}
}

// waitHost parks one reader on the wire and delivers the next frame.
//
// One reader at a time, re-armed by applyHostFrame after each delivery, which
// is waitEvents' own discipline: two goroutines reading one stream would hand
// frames to Update out of order, and on this stream the answer to a detach
// travels between room frames.
func (m *Model) waitHost() tea.Cmd {
	link := m.hosted.link
	return func() tea.Msg {
		f, err := link.NextFrame()
		return hostFrameMsg{frame: f, err: err}
	}
}

// applyHostFrame lands one frame and says what to wait for next.
//
// Four kinds reach a client after the handshake, and §4a.1's discipline is
// that each ends differently:
//
//   - a room: projected onto State, and the next frame is awaited;
//   - a detach answer: the link is given up and the program quits, with the
//     leaving sentence printed once the alternate screen is gone;
//   - a refusal: the host's own sentence lands on the notice line and the room
//     carries on, because the host carries on too — the unwatched-write ruling
//     is the one this exists for, and the sentence is the host's verbatim;
//   - a broken stream: the host is gone and so are the seats, and the program
//     quits with that sentence, which is never the sentence for a quiet room.
func (m *Model) applyHostFrame(msg hostFrameMsg) tea.Cmd {
	h := m.hosted
	if msg.err != nil {
		h.outcome, h.err = councilhost.OutcomeHostExited, msg.err
		// Nothing to close: the pipe is what broke. The host process, if it
		// somehow lives, is not this client's to kill from here — Close would
		// wait on it, and a wait on a process that is not exiting is a hang
		// behind a TUI that has already gone.
		h.closed = true
		return tea.Quit
	}
	f := msg.frame
	switch f.Kind {
	case councilhost.KindRoom:
		if f.Room != nil {
			m.applyRoom(*f.Room)
		}
	case councilhost.KindDetached:
		h.outcome = councilhost.OutcomeDetached
		_ = h.link.CloseDetached()
		h.closed = true
		return tea.Quit
	case councilhost.KindRefused:
		// The FIRST line is the ruled sentence (UnwatchedWriteRefusal); the
		// second is its remedy, which the help panel also carries. The notice
		// row is one line, and the sentence outranks the remedy on it.
		m.st.Notice = hostReasonLine(f.Reason)
	}
	return m.waitHost()
}

// applyRoom projects one room frame onto State and lands the host's notice
// when it is new.
func (m *Model) applyRoom(r councilhost.Room) {
	h := m.hosted
	m.st = stateFromRoom(r, m.st, h.windows)
	if r.Notice != h.lastNotice {
		h.lastNotice = r.Notice
		if r.Notice != "" {
			m.st.Notice = r.Notice
		}
	}
}

func hostReasonLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// hostedDispatch is enter in a hosted room: the route resolved here, against
// this State, and the seats named to the host.
//
// The refusals are the room's own, in the room's own words, and they run
// BEFORE anything crosses the wire: a brief that mixes @ and -@, a mention
// with nothing after it, a route that reaches nobody seated, and a brief that
// reaches only busy seats all leave the draft where it was typed and send
// nothing. What the host then refuses on its own — a seat that went busy in
// the 50 ms between this frame and the last — comes back as its notice.
//
// `@auto` is resolved here too, against the quota readings this client read
// from its own relay, because the pick depends on State and the host holds
// none of it.
func (m *Model) hostedDispatch() tea.Cmd {
	h := m.hosted
	draft := m.st.Draft
	if strings.TrimSpace(draft) == "" {
		m.st.Notice = "nothing to dispatch: the brief is empty"
		return nil
	}
	if isFlowCommand(draft) {
		m.st.Notice = hostedRefusal("/flow")
		return nil
	}
	if _, isArena := parseCommand(draft, "/arena"); isArena {
		m.st.Notice = hostedRefusal("/arena")
		return nil
	}
	route, prompt := ParseRoute(draft)
	autoWord := ""
	if route.Auto {
		if route.Mixed {
			m.st.Notice = "@auto picks the seat itself — drop the other mentions"
			return nil
		}
		v, w, ok := autoPick(m.st)
		if !ok {
			m.st.Notice = autoRefusal
			return nil
		}
		route, autoWord = Route{Vendors: []model.VendorID{v}}, "@auto → "+string(v)+" ("+w+")"
	}
	if route.Mixed {
		m.st.Notice = "@ narrows and -@ excludes — use one form, not both"
		return nil
	}
	if prompt == "" {
		m.st.Notice = "that is a mention with no brief after it"
		return nil
	}
	if m.st.SeatsIn(route) == 0 {
		m.st.Notice = "none of the vendors you addressed are seated"
		return nil
	}
	var seats []model.VendorID
	var busy []string
	for _, c := range m.st.Columns {
		if !m.st.seats(c) || !route.addresses(c.Vendor) {
			continue
		}
		if c.inFlight() {
			busy = append(busy, string(c.Vendor)+" (turn "+itoa(c.TurnN)+")")
			continue
		}
		seats = append(seats, c.Vendor)
	}
	if len(seats) == 0 {
		m.st.Notice = "a turn is in flight on " + strings.Join(busy, ", ") +
			" — ctrl+c on its column cancels that turn, or address another seat"
		return nil
	}
	if err := h.link.Dispatch(prompt, seats...); err != nil {
		h.outcome, h.err, h.closed = councilhost.OutcomeHostExited, err, true
		return tea.Quit
	}
	// The turn's geometry and its route, as sendTurn stamps them: the
	// operator's own act may reflow the frame, and the header names the
	// dispatch until its last seat lands. The turn number itself arrives on
	// the host's next frame, which is the one that counted it.
	m.st.FrameOwners = frameOwnersFor(route, m.st)
	sent := route
	m.st.TurnRoute = &sent
	m.st.Mode = ModeViewing
	m.setDraft("")
	m.st.Notice = autoWord
	return nil
}

// hostedKey answers the keys whose meaning a hosted room changes, before the
// ordinary keymap sees them, and reports whether it did.
//
// Reading is untouched — scroll, focus, the digits, the pages, the panes, the
// help, the yanks — because every one of those is pure over State and State is
// what the frames fill. What changes is every key that would have spawned,
// killed, or altered a seat the client does not hold.
func (m *Model) hostedKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if m.st.PanePrefix {
		return false, nil
	}
	if m.st.Mode == ModeComposing {
		switch k {
		case "ctrl+c":
			// The one key that always means the same thing: the room ends,
			// and every seat with it. Leaving is a WORD (/detach), never a
			// key, so that walking away from five agents cannot happen by
			// accident (§7.31).
			return true, m.hostedQuit()
		case "ctrl+r":
			m.st.Notice = hostedRefusal("rebuttal")
			return true, nil
		case "enter":
			if m.roomCommand() {
				return true, nil
			}
			return true, m.hostedDispatch()
		}
		return false, nil
	}
	switch k {
	case "ctrl+c":
		// viewKey's own three meanings, sent to the host instead of acted on
		// here: the focused seat when it is busy, everyone when the focused
		// seat is idle and something else runs, the room when nothing does.
		// The footer's cancel cell names which on every frame (cancelLabel).
		if c := m.focused(); c != nil && c.inFlight() {
			if err := m.hosted.link.Interrupt(c.Vendor); err != nil {
				return true, m.hostGone(err)
			}
			m.st.Notice = "cancelling " + c.Label + "…"
			return true, nil
		}
		if m.st.InFlight() {
			if err := m.hosted.link.Interrupt(); err != nil {
				return true, m.hostGone(err)
			}
			m.st.Notice = "cancelling…"
			return true, nil
		}
		return true, m.hostedQuit()
	case "q":
		if m.st.InFlight() {
			m.st.Notice = m.hostedBusySeats() + " — ctrl+c cancels a seat's turn first; /detach leaves the seats working"
			return true, nil
		}
		return true, m.hostedQuit()
	case "c", "x", "a", "s", "r", "ctrl+r":
		m.st.Notice = hostedRefusal(k)
		return true, nil
	}
	return false, nil
}

// hostedBusySeats is busySeats read off the columns, which is the only record
// of a turn this client has.
func (m *Model) hostedBusySeats() string {
	var parts []string
	for _, c := range m.st.Columns {
		if c.inFlight() {
			parts = append(parts, string(c.Vendor)+" (turn "+itoa(c.TurnN)+")")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "a turn is in flight on " + strings.Join(parts, ", ")
}

// hostedQuit ends the room: a shutdown frame, and a wait for the host to
// finish killing the seats, then the program quits.
func (m *Model) hostedQuit() tea.Cmd {
	m.hostedClose()
	return tea.Quit
}

// hostedClose closes the link once. Reached by q, ctrl+c and a signal
// (teardown), and only the first of them closes anything.
func (m *Model) hostedClose() {
	h := m.hosted
	if h.closed {
		return
	}
	h.closed = true
	h.outcome = councilhost.OutcomeEnded
	_ = h.link.Close()
}

// hostGone is what a write down a dead pipe turns into: the exited outcome
// and a quit, never a retry into a story.
func (m *Model) hostGone(err error) tea.Cmd {
	h := m.hosted
	h.outcome, h.err, h.closed = councilhost.OutcomeHostExited, err, true
	return tea.Quit
}

// detachCommand is /detach.
//
// In a hosted room it ASKS: the frame goes out, and the answer — agreement or
// the unwatched-write refusal — arrives on the wire and is handled where every
// other frame is (applyHostFrame). The client waits for that answer rather
// than assuming it, because a client that assumed agreement would walk away
// from a refusal it had provoked (design.md §7.29).
//
// In the single-process room it refuses, with the way to get a host: a verb
// that did nothing would be the honest-gauge failure spent on the room's own
// surface, and a verb that quit would be worse.
func (m *Model) detachCommand() bool {
	if m.hosted == nil {
		m.st.Notice = "this room has no host to leave. open one with `telltale council --host`"
		return true
	}
	if err := m.hosted.link.RequestDetach(); err != nil {
		// The frame reader sees the same broken pipe and quits the program
		// with the exited outcome; here the sentence is enough.
		m.st.Notice = "the host could not be asked: " + err.Error()
		return true
	}
	m.setDraft("")
	m.st.Notice = "asking the host to keep the seats and let you leave…"
	return true
}

// hostedRefusal is the one sentence every refused control gets: what was
// refused, and where it works.
func hostedRefusal(what string) string {
	return what + " is not available in a hosted room: the seats live in the host and this room only draws them. " +
		"/detach leaves it running, q ends it, and `telltale council` without --host has every control"
}
