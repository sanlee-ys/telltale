package council

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The replay (design.md §9.56): a room fed from a --record file instead of
// from vendors, drawn by the same Render over the same State.
//
// # What a replay is
//
// The recording is the event stream the live room applied, and a replay
// applies it again through the same path: each dispatch line puts the same
// seats on the same turn, each event line goes through applyEvents — the one
// choke point every vendor byte crosses — and each gate line takes the card
// down the way the operator's key did. The redactor runs, the phase words
// land, the elapsed counters count, because none of them is replay code;
// what the replay owns is the FEED and the CLOCK, and the three refusals.
//
// # The clock
//
// Render is pure over State and State.Now is stamped on the tick (CLAUDE.md).
// A replay keeps that contract and changes what the tick stamps: Now is the
// RECORDING's clock — its start plus the offset the tick has reached, at the
// speed --replay-speed asked for — so a column that took forty seconds shows
// forty seconds counting up, and shows them in twenty at 2x with the figure
// still reading forty at the end. Every record lands on that same clock, so
// the frame at a record and the frame between records agree.
//
// Two things in the live path read the wall clock as a column retires —
// finishColumn's Elapsed and the gate's charged wait — and the replay stamps
// both from the recording BEFORE the event lands, using the guards those
// paths already have (Elapsed is filled only when zero). Nothing in the
// replay reads time.Now inside Render, and a fixture played through Update
// renders the same bytes every time, which is what the golden pins.
//
// # The three refusals
//
// Enter says `this room is a replay; nothing here is live`. The card's y, n
// and a say the same, because the recording answers its own cards. ctrl+c
// and q leave, in every mode, because there is nothing to cancel. Every
// READING key is untouched: scroll, focus, the digits, the by-turn page, the
// act ledger, the help panel. A replay is a room to be read.
//
// # What it never touches
//
// room.json is neither read nor written: Run's replay branch returns before
// LoadRoom, and saveRoom returns on the replay flag. The quota relay is not
// read: Init skips readQuotaCmd and Update skips the per-dispatch re-read.
// No spawn var is reached, which the package's own guard would otherwise
// panic on (main_test.go): Init starts no rebuild and no live seat, and the
// dispatch path is behind the enter refusal.

// replayRun is one recording being played back.
type replayRun struct {
	rec  *recording
	path string
	// i is the next record to apply. A replayMsg for any other index is
	// stale — a tick that outlived a restart — and is dropped.
	i int
	// speed is the playback multiplier, already normalized to a positive
	// number.
	speed float64
	// began is the wall time the replay opened; start is the recording's own
	// wall start. clock maps the one to the other.
	began, start time.Time
}

// replayMsg is one record's turn to land, by index.
type replayMsg struct{ i int }

// runReplay is Run's replay branch: read the file, build the room over it,
// and run the program. Nothing here loads a saved room, opens a trace, reads
// a brief or consults a host, and the closing line says what was played
// rather than what was saved, because nothing was.
func runReplay(opts Options) error {
	rec, err := readRecording(opts.ReplayPath)
	if err != nil {
		return err
	}
	if opts.ReplaySpeed < 0 {
		return fmt.Errorf("--replay-speed %v: a speed is positive", opts.ReplaySpeed)
	}
	mdl := newReplayModel(opts, rec, opts.ReplayPath)
	p := tea.NewProgram(mdl)
	stopSignals := watchExitSignals(mdl, p)
	defer stopSignals()
	if _, err := p.Run(); err != nil {
		return err
	}
	fmt.Printf("replayed %d %s from %s over %s — no vendor ran, nothing was saved\n",
		len(rec.lines), plural(len(rec.lines), "record"), opts.ReplayPath, dur(rec.span()))
	return nil
}

// newReplayModel builds the room the recording's first line describes.
//
// The State comes from the FILE, not from this machine: the seats, their
// labels, postures and granularities are the recorded room's, so a replay on
// a laptop with nothing installed draws the five-seat room that was recorded.
// The workspace is the string the live header drew (recording.go's rule), so
// Home is left empty and nothing is re-abbreviated against this machine.
func newReplayModel(opts Options, rec *recording, path string) *Model {
	h := rec.room
	st := NewState()
	st.Replay = true
	st.ASCII = opts.ASCII
	st.Workspace = h.Workspace
	st.Write = h.Write
	st.GateOff = h.GateOff
	st.Briefed = h.Briefed
	st.Seats = Seats{All: h.SeatsAll}
	for _, v := range h.SeatsOnly {
		st.Seats.Only = append(st.Seats.Only, model.VendorID(v))
	}
	for _, s := range h.Seats {
		st.Columns = append(st.Columns, Column{
			Vendor:  model.VendorID(s.Vendor),
			Label:   s.Label,
			Avail:   Availability(s.Avail),
			Sandbox: SandboxClaim{Level: SandboxLevel(s.Sandbox), Detail: s.Detail},
			Gran:    Granularity(s.Gran),
			Note:    s.Note,
			Phase:   PhaseIdle,
			Follow:  true,
		})
	}
	if vis := st.VisibleColumns(); len(vis) > 0 {
		st.Focus = vis[0]
	}
	speed := opts.ReplaySpeed
	if speed <= 0 {
		speed = 1
	}
	r := &replayRun{rec: rec, path: path, speed: speed, began: time.Now(), start: rec.started()}
	st.Now = r.start
	m := newModel(opts, st)
	m.replay = r
	m.st.Notice = "replay of " + path + " — " + itoa(len(rec.lines)) + " " + plural(len(rec.lines), "record") +
		" over " + dur(rec.span()) + "; nothing here is live"
	return m
}

// clock maps a wall instant onto the recording's clock: the recording's start
// plus the wall time elapsed since the replay opened, scaled by the speed.
// A record delivered at began + ms/speed maps back to start + ms, which is
// the time the record itself stamps (at), so the two never disagree.
func (r *replayRun) clock(wall time.Time) time.Time {
	return r.start.Add(time.Duration(float64(wall.Sub(r.began)) * r.speed))
}

// at is the recording's clock at one record.
func (r *replayRun) at(ms int64) time.Time {
	return r.start.Add(time.Duration(ms) * time.Millisecond)
}

// delay is the wall time to wait before record i lands: the gap from the
// record before it, at speed. The first record waits its own offset from the
// room line, which is the pause the operator took before the first key.
func (r *replayRun) delay(i int) time.Duration {
	var prev int64
	if i > 0 {
		prev = r.rec.lines[i-1].MS
	}
	gap := time.Duration(r.rec.lines[i].MS-prev) * time.Millisecond
	if gap <= 0 {
		return 0
	}
	return time.Duration(float64(gap) / r.speed)
}

// replayNext is the Cmd that delivers the next record on time, or nil with
// the closing notice when the file is done. Delivered by index, so a test
// can feed Update the same messages the Cmd would and get the same frames.
func (m *Model) replayNext() tea.Cmd {
	r := m.replay
	if r == nil {
		return nil
	}
	if r.i >= len(r.rec.lines) {
		m.st.Notice = "replay ended — " + itoa(len(r.rec.lines)) + " " + plural(len(r.rec.lines), "record") +
			" over " + dur(r.rec.span()) + " · q quits"
		return nil
	}
	i := r.i
	return tea.Tick(r.delay(i), func(time.Time) tea.Msg { return replayMsg{i} })
}

// applyReplay lands one record and arms the next.
//
// The clock moves to the record's own stamp first, so everything the record
// changes is measured against the moment it happened — and never backwards,
// because a tick between records may already have carried Now past it.
func (m *Model) applyReplay(msg replayMsg) tea.Cmd {
	r := m.replay
	if r == nil || msg.i != r.i || msg.i >= len(r.rec.lines) {
		return nil
	}
	line := r.rec.lines[msg.i]
	r.i++
	if at := r.at(line.MS); at.After(m.st.Now) {
		m.st.Now = at
	}
	switch line.Kind {
	case "dispatch":
		m.replayDispatch(line)
	case "event":
		// A stale exit is consumed and not applied (markStaleExits): the live
		// room dropped it on a liveness test a replay cannot run, and landing
		// it here would fail the new turn and stamp an Elapsed the vendor
		// never took.
		if !line.stale {
			m.replayEvent(line)
		}
	case "gate":
		m.replayGate(line)
	}
	return m.replayNext()
}

// replayDispatch puts the recorded seats on the recorded turn, the way
// sendTurn does once every refusal is behind it: the columns start their
// turn, the dispatch is held on Model.turns so the header, the spinner and
// the retirement path all see a seat in flight, and the geometry is decided
// from the route. No process, no handle, no context to cancel — the record's
// cancel is a no-op, and its seat contexts are children of nothing.
func (m *Model) replayDispatch(line recordLine) {
	now := m.replay.at(line.MS)
	route := Route{}
	if line.Route != nil {
		route.Negated = line.Route.Negated
		for _, v := range line.Route.Vendors {
			route.Vendors = append(route.Vendors, model.VendorID(v))
		}
	}
	ts := &turnState{
		n:          line.Turn,
		route:      route,
		cancel:     func() {},
		seatCancel: map[model.VendorID]context.CancelFunc{},
		live:       map[model.VendorID]bool{},
		persistent: map[model.VendorID]bool{},
	}
	sent := map[model.VendorID]recordSent{}
	for _, s := range line.Sent {
		sent[model.VendorID(s.Vendor)] = s
	}
	m.st.FrameOwners = frameOwnersFor(route, m.st)
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if !m.st.seats(*c) {
			continue
		}
		s, ok := sent[c.Vendor]
		if m.turnOf(c.Vendor) != nil {
			if !ok {
				// Still answering an earlier brief, and this brief did not
				// name it: sendTurn's own rule, untouched.
				continue
			}
			// The record says this seat TOOK the brief, and sendTurn refuses
			// a busy seat, so the live room had already released it. The
			// recording does not carry the release: a one-shot seat that
			// named its own end of turn settles without retiring until its
			// exit lands, and that exit was the next dispatch's stale one, or
			// the operator's ctrl+c on a lingering process, neither of which
			// is written down. Left held, the seat would never start another
			// turn, every later reply would land in the old turn's body, and
			// the column would report itself "not addressed" on turns it
			// answered (measured 2026-09-04 on a real recording: grok stuck
			// on turn 9 through turns 10 and 11 it took). The dispatch line
			// is the evidence; the seat is released the way the room's own
			// retirement path does it.
			m.replayRelease(c, now)
		}
		if !ok {
			// sendTurn's own words for a seat the brief did not reach.
			c.Note = "not addressed in turn " + itoa(line.Turn)
			c.Skipped = true
			continue
		}
		delete(m.givenUp, c.Vendor)
		delete(m.cancelling, c.Vendor)
		ts.live[c.Vendor] = true
		ts.persistent[c.Vendor] = s.Persistent
		_, cancel := context.WithCancel(context.Background())
		ts.seatCancel[c.Vendor] = cancel
		c.startTurn(line.Turn, sanitize(s.Prompt), s.Quoted)
		c.Phase = PhaseStreaming
		if c.Gran == GranFinalOnly || c.Gran == GranUnknown {
			c.Phase = PhaseWaiting
		}
		c.Note = ""
		c.CostSession = s.Persistent
		c.Started = now
		m.redactors[c.Vendor] = &Redactor{}
	}
	if len(ts.live) == 0 {
		return
	}
	m.holdTurn(ts)
	routed := route
	m.st.TurnRoute = &routed
	m.st.Turn = ts.n
	if m.st.Page.Open {
		m.openPage(m.st.Turn)
	}
	m.st.Mode = ModeViewing
	m.setDraft("")
	m.st.Notice = ""
}

// replayRelease retires a seat the recording shows taking a new brief while
// the replay still holds it on an older turn (replayDispatch).
//
// A seat still in flight is closed as CANCELLED, with the elapsed it had at
// the record's own instant: the only way a live seat mid-answer becomes free
// for the next brief is the operator's ctrl+c, and a column that read `done`
// over an answer that never finished would be the replay inventing an ending
// (§4a.1). A seat whose phase already settled keeps its phase and its clock.
// Retirement then runs the room's own path, so the turn tears down when its
// last seat leaves, exactly as it would have live.
func (m *Model) replayRelease(c *Column, at time.Time) {
	if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
		if c.Elapsed == 0 && !c.Started.IsZero() {
			if d := at.Sub(c.Started); d > 0 {
				c.Elapsed = d
			}
		}
		c.Phase = PhaseCancelled
		c.Note = "cancelled — the output above is partial"
	}
	c.Settling = false
	m.turnColumnFinished(c.Vendor)
}

// replayEvent hands one recorded event to applyEvents, after stamping the
// two clocks the live path would read off the wall as the column retires.
//
// Elapsed is stamped for a terminal event on a live column, from the
// recording's own two instants, so the figure the column keeps is the time
// the vendor actually took — finishColumn fills it only when zero. The gate
// card's StoppedAt is re-stamped after queueGate raised it, because
// queueGate reads time.Now for a card that has just gone up (§9.45), and on
// a replay "just" is the record's own moment.
func (m *Model) replayEvent(line recordLine) {
	ev, ok := line.event()
	if !ok {
		return
	}
	at := m.replay.at(line.MS)
	if c := m.column(ev.Vendor); c != nil && (c.Phase == PhaseStreaming || c.Phase == PhaseWaiting) &&
		c.Elapsed == 0 && !c.Started.IsZero() {
		if ev.Kind == runner.KindDone || ev.Kind == runner.KindError || ev.EndsTurn {
			if d := at.Sub(c.Started); d > 0 {
				c.Elapsed = d
			}
		}
	}
	m.applyEvents([]runner.Event{ev})
	if ev.Gate != nil {
		for i := range m.st.Gates {
			if m.st.Gates[i].RequestID == ev.Gate.RequestID {
				m.st.Gates[i].StoppedAt = at
			}
		}
	}
}

// replayGate takes one card down the way decideGate does, with the wait
// charged from the recording rather than from the wall: the stretch between
// the seat's first card and this answer, on the recording's clock, added to
// the column only once the seat has no card left (chargeGateWait's rule).
func (m *Model) replayGate(line recordLine) {
	v := model.VendorID(line.Vendor)
	at := m.replay.at(line.MS)
	stoppedAt, _ := m.st.gateStoppedAt(v)
	kept := m.st.Gates[:0]
	var decided *PendingGate
	for i := range m.st.Gates {
		g := m.st.Gates[i]
		if decided == nil && g.Vendor == v && g.RequestID == line.RequestID {
			d := g
			decided = &d
			delete(m.gateInputs, g.RequestID)
			continue
		}
		kept = append(kept, g)
	}
	m.st.Gates = kept
	if decided == nil {
		return
	}
	c := m.column(v)
	if !line.Allow && c != nil {
		// Recorded from the keystroke, as decideGate records it: the vendor's
		// echo of a denial is indistinguishable from a tool that broke.
		c.recordAct(runner.ActCall{ID: decided.ToolUseID, Text: decided.Text, Outcome: runner.ActDenied}, m.redactWhole)
	}
	if _, stopped := m.st.gateStoppedAt(v); !stopped && c != nil && !stoppedAt.IsZero() {
		if d := at.Sub(stoppedAt); d > 0 {
			c.GateWait.D += d
		}
		c.GateWait.Measured = true
	}
	if len(m.st.Gates) == 0 {
		m.st.Notice = ""
	}
}

// replayNotice is what every refused key says. One sentence for every
// refusal, so the reader learns one fact about the room rather than one
// per key.
const replayNotice = "this room is a replay; nothing here is live"

// replayKey answers the keys that would act on a live room. It reports
// whether it took the key; a key it did not take goes on to the ordinary
// keymap, which is how reading stays untouched.
//
// ctrl+c is quit in every mode: a replay has no turn to cancel and no
// process to end, and teardown on a replay kills nothing and saves nothing
// (saveRoom's replay guard). q quits from view mode as it always did, minus
// the busy-seat refusal, which is about seats that are answering.
func (m *Model) replayKey(msg tea.KeyPressMsg) (bool, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" {
		m.teardown()
		return true, tea.Quit
	}
	if m.st.Gating() {
		switch k {
		case "y", "n", "a":
			m.st.Notice = replayNotice
			return true, nil
		}
	}
	if m.st.PanePrefix {
		return false, nil
	}
	if m.st.Mode == ModeComposing {
		if k == "enter" {
			m.st.Notice = replayNotice
			return true, nil
		}
		return false, nil
	}
	switch k {
	case "q":
		m.teardown()
		return true, tea.Quit
	case "c", "u", "x", "o", "a":
		// The per-seat verbs and the asking toggle: each changes a seat's
		// thread, tree, racer or gate, and a replay has none of those to
		// change.
		m.st.Notice = replayNotice
		return true, nil
	}
	return false, nil
}
