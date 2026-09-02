package council

import (
	"context"
	"runtime"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// A live seat that cannot be brought up retreats to its measured batch adapter
// for the rest of the room (vendors.LiveFallback, design.md §9.57).
//
// Three seats moved to long-lived processes on 2026-09-02 on the strength of
// vendor documentation rather than a live run, and each has a batch invocation
// beside it that WAS measured. The interface names that adapter; this file is
// the room's side of the bargain — WHEN to retreat, read from what the room can
// see — and it is deliberately narrow. Two triggers, no more:
//
//   - the seat's protocol reports its handshake Dead: `initialize` or the
//     thread/session open answered with an error (retreatOnRefusal, and the
//     same test in handTurnToSeat for a process whose refusal arrived before
//     this brief);
//   - a persistent process died on its FIRST turn before it ever named a
//     session (retreatOnDeath): an `agy` that does not know `--input-format`
//     exits at argument parsing, and a build without a subcommand exits before
//     answering anything.
//
// A death on a later turn, a death with a session already named, a cancel, a
// give-up and a resumed process that refused its thread are all NOT a retreat:
// each is a seat that was up, and the seat's existing sentences say what
// happened to it. The retreat is per SEAT and per ROOM — the other seats in
// flight are untouched, and the room's next open starts every seat live again,
// because a refusal is a fact about a build on this machine today and the
// badge does not outlive the room that measured it.

// fallbackFor returns the measured batch adapter behind a seat, or nil for a
// seat that has none (Claude, Cursor) or that the registry does not know.
func (m *Model) fallbackFor(v model.VendorID) vendors.Vendor {
	lf, ok := vendors.Registry()[v].(vendors.LiveFallback)
	if !ok {
		return nil
	}
	return lf.Fallback()
}

// registry is vendors.Registry with every seat that has retreated in this
// room replaced by its batch adapter — the shape dispatch reads, so a
// fallen-back seat is a one-shot seat on every later brief rather than a live
// seat that respawns the same refusal each turn.
func (m *Model) registry() map[model.VendorID]vendors.Vendor {
	reg := vendors.Registry()
	for id, back := range m.fellBack {
		if !back {
			continue
		}
		if lf, ok := reg[id].(vendors.LiveFallback); ok {
			reg[id] = lf.Fallback()
		}
	}
	return reg
}

// retreat records that a seat has fallen back and re-stamps its badge, so the
// posture page reads the fallback spelling seatShape carries for it (`exec ·
// unasked · fallback, measured at 0.149.1`) rather than the live shape's
// `unmeasured`. The fallback IS the measured seat, and the badge may say so.
func (m *Model) retreat(c *Column) {
	if m.fellBack == nil {
		m.fellBack = map[model.VendorID]bool{}
	}
	m.fellBack[c.Vendor] = true
	m.restampSeat(c)
}

// restampSeat recomputes one column's posture claim from the room's current
// facts, the way applyPosture does for every column on /read and /write.
func (m *Model) restampSeat(c *Column) {
	c.Sandbox = postureClaimFor(c.Vendor, runtime.GOOS == "windows", m.st.Write,
		m.st.Asking(), m.hooks.Wired(), m.fellBack[c.Vendor])
}

// retreatOnRefusal is the KindError branch's question for a persistent seat
// whose protocol just reported a failed turn: was that the handshake? If the
// wire is Dead and the seat has a fallback, the up-and-useless process is
// stopped and the same brief goes to the batch adapter on this dispatch. It
// reports whether it took the column, so the caller's ordinary retirement
// runs only when it did not.
func (m *Model) retreatOnRefusal(c *Column) bool {
	p, ok := m.procs[c.Vendor]
	if !ok || !deadWire(p.wire) || m.fallbackFor(c.Vendor) == nil {
		return false
	}
	stopProc(p)
	m.dropProcess(c.Vendor)
	return m.retreatSeat(c)
}

// retreatOnDeath is the process-exit branches' question for a persistent seat
// whose process has just died: did it die before it was ever up? Only a
// process on its first turn, with no session yet named, that was not resumed,
// cancelled or given up counts — every other death is a seat that was
// running, and the existing sentences describe it. The process is already
// gone, so nothing is killed here; it is only forgotten.
func (m *Model) retreatOnDeath(c *Column) bool {
	p, ok := m.procs[c.Vendor]
	if !ok || p.sent != 1 || p.resumed || m.sessions[c.Vendor] != "" {
		return false
	}
	if m.cancelling[c.Vendor] || m.wasGivenUp(c.Vendor) || m.fallbackFor(c.Vendor) == nil {
		return false
	}
	m.dropProcess(c.Vendor)
	return m.retreatSeat(c)
}

// retreatSeat sends the seat's dispatched brief to its batch adapter on the
// dispatch it is already on. The column stays in the turn's live set — it
// never landed — and moves from the persistent bookkeeping to the one-shot
// bookkeeping (turnState.seatHandles), so its exit retires it the way any
// one-shot seat's does and `x`, ctrl+c and teardown reach the new process.
// The batch child hangs from the dispatch's own context, as its siblings do.
func (m *Model) retreatSeat(c *Column) bool {
	ts := m.turnOf(c.Vendor)
	if ts == nil {
		return false
	}
	m.retreat(c)
	v := m.fallbackFor(c.Vendor)
	delete(ts.persistent, c.Vendor)
	// A one-shot process reports THIS turn's spend, not a running total; the
	// badge that says which one it is follows the process (sendTurn's rule).
	c.CostSession = false
	// The redactor held nothing worth keeping: a refused handshake produces
	// no text, and a process that died at argument parsing produced stderr,
	// which the failure note already carried. Flushed rather than dropped so
	// nothing is eaten, on the same rule every retirement follows.
	c.Body += m.flush(c.Vendor)
	parent := ts.ctx
	if parent == nil {
		parent = context.Background()
	}
	if old := ts.seatCancel[c.Vendor]; old != nil {
		old()
	}
	sctx, scancel := context.WithCancel(parent)
	if ts.seatCancel == nil {
		ts.seatCancel = map[model.VendorID]context.CancelFunc{}
	}
	ts.seatCancel[c.Vendor] = scancel
	if err := m.startBatchSeat(ts, sctx, v, c, ts.prompts[c.Vendor]); err != nil {
		// The fallback could not be started either. That is a dispatch
		// failure in sendTurn's own terms — pre-flight, no process, the
		// thread never asked about — reported on the column with both facts.
		c.Note = fallbackNote(c.Vendor) + " — and it could not be started: " + err.Error()
		c.Elapsed = time.Since(c.Started)
		m.failure[c.Vendor] = runner.FailurePreflight
		m.finishColumn(c, PhaseFailed)
		return true
	}
	c.Note = fallbackNote(c.Vendor)
	// The batch seat starts in the phase sendTurn gives a fresh spawn of its
	// granularity: a final-only vendor says nothing until it is done, and
	// `streaming` on a column that is silent by design is a claim it has not
	// earned (sendTurn's own reasoning). The clock is NOT restarted — the
	// operator has been waiting since enter, and the retreat is part of what
	// this turn cost.
	c.Phase = PhaseStreaming
	if c.Gran == GranFinalOnly || c.Gran == GranUnknown {
		c.Phase = PhaseWaiting
	}
	return true
}

// fallbackNote is the column's sentence for a retreat: what was refused, what
// the seat runs instead, and that it stays that way for the rest of the room.
func fallbackNote(v model.VendorID) string {
	return "the live handshake was refused — this seat fell back to " +
		fallbackInvocation(v) + ", one process per turn, for the rest of the room"
}
