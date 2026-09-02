package council

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The rebuild at room open (design.md §9.52, rung 2).
//
// THE DEFECT IS A SENTENCE, NOT A MECHANISM. Quitting the room kills every seat
// (teardown), so the agents a reattached room shows are not the agents the
// operator left — they are new processes started on the ids the old ones wrote
// down. The room used to say `2/3 seats restored` and `this seat's thread came
// back`, which are both true about the THREAD and say nothing about the
// PROCESS, and the process is the half that died. A room drawn that way reads
// as a room that was never closed.
//
// So this file does two things, and the second is the reason for the first:
//
//  1. It moves the seat launch from the first dispatch to room open, so the
//     startup is already paid when the operator's first brief arrives.
//  2. It says, per seat and in the room's own notice, that what came back is a
//     REBUILD and not a survival.
//
// WHAT THE MOVE ACTUALLY BUYS, split honestly. runner/session.go records the
// measurement: a one-word turn cost about 25 seconds and about $0.23, NEARLY
// ALL OF IT STARTUP. Moving the launch earlier moves the seconds. It does not
// move the dollars — a process that has started has run no model turn, so
// nothing is billed until the first brief. The rebuilt seat states both halves,
// because a room that implied it had just spent a dollar per reopen would be
// inventing a charge, and a room that implied the first brief was now free
// would be hiding one. Both figures carry a leading `~`: one measurement, of
// one one-word turn, extrapolated across seats. Which surface carries which
// half is decided further down, at rebuildCostDetail.
//
// WHAT IT DOES NOT DO. It starts no process the first brief was not going to
// start anyway — the rebuild changes WHEN, never WHETHER — and it persists
// nothing. room.json is unchanged by any of this: session ids, a workspace and
// numbers, exactly as resume.go rules.
//
// THE VOCABULARY IS §9.52's. `reattach` is a fact about the FILE and keeps that
// meaning. `rebuild` is a fact about the PROCESS and is new here. `rejoin` is
// reserved for a client reaching a process that was already running, which
// nothing in this product does, so nothing here may spend the word.

// rebuildState is where one seat has got to. Every value is MEASURED — a
// returned error, an event the vendor sent, or a process that is no longer
// alive. None of them is a timer.
type rebuildState uint8

const (
	// rebuildRunning: the spawn returned with no error. The process is up and
	// has said nothing yet.
	//
	// It is emphatically NOT "the thread came back". persistent.go already
	// refuses that claim on this exact path — "Deliberately NOT reattached to
	// the saved thread. Nothing has come back yet" — and a launched process is
	// not a proven thread until the vendor says so.
	rebuildRunning rebuildState = iota
	// rebuildDone: the vendor announced a session id and it is the one that was
	// asked for. This is the only state that may say a thread came back.
	rebuildDone
	// rebuildFailed: the spawn failed, or the process died before it announced
	// anything. why carries the reason in whatever words produced it.
	rebuildFailed
)

// rebuildSeat is one seat's half of a run.
type rebuildSeat struct {
	// want is the session id this seat was asked to resume, kept so a vendor
	// answering somewhere else can be recognised rather than assumed.
	want  string
	state rebuildState
	why   string
}

// rebuildRun is one room-open rebuild.
//
// On Model and never on State, the boundary the brief and the reattachment
// already keep: only what the room can honestly SAY crosses over, as a notice
// and as per-seat notes. Nothing here is a field the renderer reads, so nothing
// here can drift behind what is drawn.
type rebuildRun struct {
	// started is when the launches were made, so the settled notice can report
	// how long the rebuild actually took instead of quoting the measurement
	// again. Read only when the run settles, off the update loop, so Render
	// never sees a clock.
	started time.Time
	seats   map[model.VendorID]*rebuildSeat
	// settled reports that the closing notice has already been written.
	//
	// A one-shot, for the reason teardownDone is one: settleRebuild is reached
	// from two places — every event batch that empties the running set, and the
	// spinner's backstop — and both can fire again afterwards. Without this,
	// the closing sentence would overwrite whatever the operator's next action
	// put in the notice, on every tick, forever.
	settled bool
}

// rebuildMsg starts the run. It is a message rather than a direct call so the
// launches happen ON the update loop, where every other mutation of m.procs
// happens — the model is lock-free by that convention, and a rebuild goroutine
// writing the seat registry would be the first thing to break it.
type rebuildMsg struct{}

// rebuildCmd is what Init adds when there is something to rebuild, and nothing
// otherwise.
//
// Fired from Init deliberately. No test calls Init, so a Model a test builds
// directly launches nothing at all, and the package's spawn guard
// (main_test.go) keeps its meaning without this path needing an exception.
func (m *Model) rebuildCmd() tea.Cmd {
	if len(m.rebuildable()) == 0 {
		return nil
	}
	return func() tea.Msg { return rebuildMsg{} }
}

// rebuildable is the seats this room may rebuild, in seating order.
//
// FOUR CONDITIONS, and each excluded seat is excluded for a measured reason
// rather than reported as a failure:
//
//   - The room actually reattached. With no saved room there is no id to
//     rebuild onto, and launching a seat anyway would open a conversation the
//     operator has not asked for.
//   - This seat's thread was restored (Column.Restored) and there is a resume
//     id still unspent. A seat that saved nothing has nothing to resume.
//   - The vendor is installed here. An id for a vendor this machine cannot run
//     is not a failure, it is a fact about the box (§7.27's middle row).
//   - The seat is driven as a long-lived process. A spawn-per-turn seat has no
//     process to start early; its turn IS its process.
func (m *Model) rebuildable() []model.VendorID {
	if !m.st.Reattached.Active() {
		return nil
	}
	reg := vendors.Registry()
	var out []model.VendorID
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if !c.Restored || c.Avail != AvailInstalled {
			continue
		}
		if m.resumeIDs[c.Vendor] == "" {
			continue
		}
		v, ok := reg[c.Vendor]
		if !ok || !liveSeat(v) {
			continue
		}
		out = append(out, c.Vendor)
	}
	return out
}

// startRebuild launches every restorable seat and reports what happened.
//
// The launches are made back to back on one pass of the update loop, which is
// what makes them parallel where the time actually goes: exec.Start returns in
// milliseconds and the vendor's own session load then runs concurrently in each
// child. Nothing here waits for a vendor.
//
// seatProcess is reused rather than reimplemented, and that is the point. It
// already spends a restored id exactly once, records the workspace and posture
// the process was spawned with, marks the process `resumed` so the brief is not
// re-sent, and registers it in m.procs so the first dispatch finds it instead of
// starting a second one. A rebuild that opened its own processes would be a
// second seat-launch path for the same seats, and the two would drift.
func (m *Model) startRebuild() tea.Cmd {
	seats := m.rebuildable()
	if len(seats) == 0 {
		return nil
	}
	reg := vendors.Registry()
	run := &rebuildRun{started: time.Now(), seats: map[model.VendorID]*rebuildSeat{}}
	m.rebuild = run

	for _, id := range seats {
		c := m.column(id)
		v := reg[id]
		// Read BEFORE the launch: seatProcess spends the id, so afterwards
		// there is nothing left to compare a vendor's answer against.
		want := m.resumeIDs[id]
		rs := &rebuildSeat{want: want}
		run.seats[id] = rs

		if _, _, err := m.seatProcess(v, c); err != nil {
			// A launch that failed is a measured failure and carries the
			// vendor's own words. The seat is not wedged by it: the next brief
			// takes the ordinary path and opens a new session.
			rs.state, rs.why = rebuildFailed, err.Error()
			c.Note = "this seat could not be rebuilt: " + err.Error() +
				" — its next brief opens a new session, with the brief re-applied."
			c.NoteCalm = true
			continue
		}
		rs.state = rebuildRunning
		// Cleared explicitly: a rebuild that follows a failed one on the same
		// seat must not keep the earlier failure's detail under a new title.
		c.NoteDetail = ""
		// Armed with the id that was asked for, exactly as a dispatch arms it
		// (§9.43). A vendor that answers in a different conversation is then
		// caught by adoptSession's existing check and reported in its existing
		// words, so a fork at room open and a fork at turn 5 read alike.
		if want != "" {
			m.forkWatch[id] = want
		}
		c.Note = "rebuilding this seat — a new process is loading the saved thread."
		c.NoteCalm = true
	}

	m.st.Notice = joinNotice(m.st.Notice, m.rebuildStartNotice())
	if m.rebuildInFlight() {
		// The event pump has to run with no turn behind it, because the only
		// thing that can settle a seat is an event from the process just
		// started.
		return m.waitEvents()
	}
	m.settleRebuild()
	return nil
}

// rebuildInFlight reports that at least one seat is still only launched.
func (m *Model) rebuildInFlight() bool {
	if m.rebuild == nil {
		return false
	}
	for _, rs := range m.rebuild.seats {
		if rs.state == rebuildRunning {
			return true
		}
	}
	return false
}

// rebuildOwns reports that this vendor's events belong to the rebuild rather
// than to the turn machinery.
//
// TRUE ONLY WHILE THE SEAT IS STILL LAUNCHING AND NO TURN IS RUNNING. Both
// halves matter. The turn path assumes a turn — it reads the seat's turn on
// several branches — so an init line arriving at an idle room must not be
// walked through it. And the moment a turn starts, the turn owns the seat
// again. Room-wide (anyInFlight, where this read m.turn) rather than per seat,
// and the difference cannot be observed: the first brief ends the rebuild
// outright (sendTurn calls endRebuild), so no seat is ever launching while
// another is on a turn.
func (m *Model) rebuildOwns(v model.VendorID) bool {
	if m.rebuild == nil || m.anyInFlight() {
		return false
	}
	rs, ok := m.rebuild.seats[v]
	return ok && rs.state == rebuildRunning
}

// applyRebuildEvent folds one event into a seat the rebuild owns.
//
// EVERY event for such a seat is consumed here, including the ones this
// function has nothing to do with. A rebuild sends no prompt, so text arriving
// before the first brief is vendor chatter with no turn to attach it to, and
// appending it to a column body would draw it as an answer to a question nobody
// asked. Swallowing it is the honest render, not a shortcut.
func (m *Model) applyRebuildEvent(c *Column, ev runner.Event) {
	rs := m.rebuild.seats[ev.Vendor]
	switch ev.Kind {
	case runner.KindSession, runner.KindMeta:
		if ev.SessionID == "" {
			return
		}
		// adoptSession is the one place a session id is recorded, and it is
		// also §9.43's fork check. Called rather than duplicated: a rebuild
		// that recorded the id itself would be a second writer of m.sessions
		// and would skip the fork correction entirely.
		asked := rs.want
		m.adoptSession(c, ev.SessionID)
		if ev.SessionID == asked {
			rs.state = rebuildDone
			// "on a NEW process" is the whole point of the sentence. The
			// reattach card above it says the thread came back, which is true;
			// this says what did NOT come back. Together they state both halves,
			// and neither alone would.
			c.Note = "this seat was rebuilt — the saved thread came back, on a NEW process. " +
				"the one you left was ended when the room closed."
			c.NoteDetail = rebuildCostDetail
			c.NoteCalm = true
			return
		}
		// The vendor answered somewhere else. adoptSession has already written
		// the correction into the note and cleared Restored (§9.43); this only
		// records that the seat has stopped launching, so nothing here
		// overwrites the sentence that says what happened to the history.
		rs.state = rebuildDone

	case runner.KindDone, runner.KindError:
		// The process is gone before it said anything. Measured, and reported
		// with whatever the vendor left behind.
		why := ev.Note
		if why == "" && ev.Err != nil {
			why = ev.Err.Error()
		}
		if why == "" {
			why = "the process exited before it reported a thread"
		}
		rs.state, rs.why = rebuildFailed, why
		m.dropProcess(ev.Vendor)
		c.Note = "this seat could not be rebuilt: " + why +
			" — its next brief opens a new session, with the brief re-applied."
		c.NoteCalm = true
	}
}

// settleDeadRebuilds catches a seat whose process died without an event.
//
// The spinner tick is the backstop, exactly as it is for the arena's throttled
// reads: a process that exited cannot be Alive, so this is a read of the same
// fact KindDone reports and not a timeout. Nothing here gives up on a seat that
// is merely slow — a live process that has said nothing is still rebuilding,
// and saying otherwise would be a guess wearing a measurement's clothes.
func (m *Model) settleDeadRebuilds() {
	if !m.rebuildInFlight() {
		return
	}
	for v, rs := range m.rebuild.seats {
		if rs.state != rebuildRunning {
			continue
		}
		p, ok := m.procs[v]
		if ok && p.sess != nil && p.sess.Alive() {
			continue
		}
		rs.state, rs.why = rebuildFailed, "the process exited before it reported a thread"
		m.dropProcess(v)
		if c := m.column(v); c != nil {
			c.Note = "this seat could not be rebuilt: " + rs.why +
				" — its next brief opens a new session, with the brief re-applied."
			c.NoteCalm = true
		}
	}
	if !m.rebuildInFlight() {
		m.settleRebuild()
	}
}

// settleRebuild writes the closing notice once every seat has stopped
// launching.
func (m *Model) settleRebuild() {
	if m.rebuild == nil || m.rebuild.settled {
		return
	}
	notice := m.rebuildSettledNotice()
	if notice == "" {
		// Nothing settled into a reportable state, so there is nothing to say
		// and the notice that is up stays up. Blanking it would let a rebuild
		// that reached no conclusion erase a sentence that had one.
		return
	}
	m.rebuild.settled = true
	// REPLACES the reattach sentence rather than joining it, and that is the
	// one place this file spends something.
	//
	// Joined, the settled sentence lands past a hundred columns and renders as
	// `… 2/4 seats rebuilt in 24s — NE…`, which cuts the exact clause the rung
	// exists to say. The reattach sentence is not lost by the swap: it was the
	// whole notice from room open until this moment — the length of the rebuild
	// — so its once-only clauses (a workspace that no longer exists, a posture
	// the saved room ran under) have been on screen the entire time, and they
	// are also readable at any moment from `telltale council ls` (§7.27).
	m.st.Notice = notice
	// The run is kept rather than cleared, so a later reader can still tell a
	// rebuilt room from a cold one. What is cleared is the ownership: every
	// seat has left rebuildRunning, so rebuildOwns is already false for all of
	// them and the turn machinery has the seats back.
}

// endRebuild retires the run when a turn is dispatched.
//
// The first brief is the moment the rebuild stops being the thing on screen:
// startTurn clears every per-turn field including the note, and a run left
// standing would go on owning events for a seat the turn is now driving.
func (m *Model) endRebuild() { m.rebuild = nil }

// WHERE EACH HALF OF THE NEWS LIVES, and this placement is the decision the
// first draft got wrong.
//
// The notice is ONE LINE and it is truncated, not wrapped: at 120 columns the
// reattach sentence already fills most of it, and a cost clause appended after
// that one renders as `… — NE…`. A sentence that disappears at a hundred
// columns is not a stated cost. The columns are the opposite shape — every note
// wraps, every seat has one, and noteCard already carries a muted detail block
// under its title.
//
// So the split follows the shape:
//
//   - The COLUMN carries the sentence that must never be lost — this seat was
//     rebuilt, on a NEW process — and the measured cost under it as detail. The
//     cost is a PER-SEAT fact ("$0.23 a seat"), so a per-seat home is also the
//     correct one rather than merely the roomy one. That is the distinction
//     reattachCard draws: the room fact goes in the notice once, the seat fact
//     goes in the seat.
//   - The NOTICE carries the room fact — how many seats, how long it took — and
//     it is JOINED to the reattach sentence rather than replacing it, because
//     that sentence holds clauses shown exactly once anywhere (a workspace that
//     no longer exists, a posture the saved room ran under).

// rebuildCostDetail is the measured cost, stated per seat, under the note that
// says the seat was rebuilt.
//
// BOTH HALVES OR NEITHER. runner/session.go measured one one-word turn at about
// 25 seconds and about $0.23, nearly all of it startup. The rebuild moves the
// seconds and does not move the dollars — a process that has started has run no
// model turn — so a line naming only the seconds would read as though the
// reopen were free, and one naming only the dollars would read as though the
// room had just spent them.
//
// Both figures carry a leading `~` and the sentence names what was measured,
// because one turn on one seat extrapolated across a room is an estimate and
// this repository marks estimates rather than rounding them into facts.
const rebuildCostDetail = "its ~25s of startup is spent now instead of on your first brief, " +
	"which still bills its ~$0.23 (measured once, on a one-word turn)."

// rebuildStartNotice is the room fact while the seats are coming back.
//
// Deliberately short. The honest clause is not here — it is in every column,
// where it cannot be cut — and a longer sentence would push the reattach
// sentence it is joined to off the end of the line.
func (m *Model) rebuildStartNotice() string {
	n := 0
	for _, rs := range m.rebuild.seats {
		if rs.state == rebuildRunning {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return "rebuilding " + itoa(n) + " " + plural(n, "seat")
}

// rebuildSettledNotice is the room fact once they have.
//
// THE ELAPSED FIGURE IS THIS RUN'S OWN, not the measurement quoted back. It is
// what this rebuild actually took, read once here, off the update loop, so
// Render never sees a clock.
func (m *Model) rebuildSettledNotice() string {
	done, failed := 0, 0
	for _, rs := range m.rebuild.seats {
		switch rs.state {
		case rebuildDone:
			done++
		case rebuildFailed:
			failed++
		}
	}
	if done+failed == 0 {
		return ""
	}
	out := itoa(done) + "/" + itoa(m.st.Seated()) + " seats rebuilt in " +
		dur(time.Since(m.rebuild.started))
	if failed > 0 {
		// A measured partial is reported as a partial. Rounding it up to the
		// attempted count would claim seats that are not there.
		out += ", " + itoa(failed) + " could not be"
	}
	return out + " — NEW processes, not the ones you left"
}
