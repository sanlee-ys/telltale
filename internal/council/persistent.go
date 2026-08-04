package council

import (
	"strconv"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// seatProc is one seat's long-lived vendor process.
//
// It outlives a turn on purpose and outlives the room never: teardown kills it,
// and the job object kills it again if telltale itself dies first.
type seatProc struct {
	sess *runner.Session
	// sent counts turns handed to THIS process. Zero means the next one is its
	// first, which is what decides whether the shared brief goes with it.
	//
	// Counted per PROCESS rather than per vendor, and that is the whole reason
	// it is not a boolean on the model: a seat whose process died and was
	// replaced is a stranger again, and the replacement has to be briefed like
	// the original was. A per-vendor flag would have remembered a briefing that
	// happened in a session that no longer exists.
	sent int
}

// sendPersistentTurn hands one turn to a seat's process, starting one if there
// is none. The returned string is a note for the column, empty when nothing
// worth saying happened.
func (m *Model) sendPersistentTurn(v vendors.Persistent, c *Column, prompt string) (string, error) {
	p, note, err := m.seatProcess(v, c)
	if err != nil {
		return "", err
	}

	// First turn for THIS process, so it gets the operating context. Per
	// process rather than per room: a seat that respawned is unbriefed again,
	// and would otherwise be the only one guessing.
	if p.sent == 0 {
		prompt = m.brief.Apply(prompt)
	}

	line, err := v.Turn(prompt)
	if err != nil {
		return "", err
	}
	if err := p.sess.Send(line); err != nil {
		// The process is there but will not take the turn. Dropping the seat
		// means the next brief starts a fresh one rather than writing into a
		// pipe nobody reads.
		m.dropProcess(c.Vendor)
		return "", err
	}
	p.sent++
	return note, nil
}

// seatProcess returns the seat's process, launching one if it has none or if
// the one it had has gone.
func (m *Model) seatProcess(v vendors.Persistent, c *Column) (*seatProc, string, error) {
	existing, had := m.procs[c.Vendor]
	if had && existing.sess.Alive() {
		return existing, "", nil
	}

	spec, err := v.Session(m.st.Workspace, c.Binary, m.posture())
	if err != nil {
		return nil, "", err
	}
	// The ROOM's context, never the turn's. A turn that is cancelled must not
	// take this process with it — that is the entire point of keeping it — so
	// only quitting the room cancels this.
	sess, err := runner.StartSession(m.roomCtx, spec, m.events, v.ParseEvent)
	if err != nil {
		return nil, "", err
	}
	p := &seatProc{sess: sess}
	m.procs[c.Vendor] = p

	if had {
		// Said out loud. The thread really was lost, and a seat that quietly
		// forgot the conversation while its neighbours remembered theirs is the
		// kind of silent divergence this product exists to refuse.
		return p, "the previous process ended — this seat is starting a new session", nil
	}
	return p, "", nil
}

// dropProcess forgets a seat's process. The kill is separate on purpose: a
// process that died on its own does not need killing, and one the room is
// tearing down is killed by teardown.
func (m *Model) dropProcess(v model.VendorID) {
	delete(m.procs, v)
}

// interruptSeat abandons the turn in flight on a persistent seat without
// killing the process.
//
// Falls back to a kill if the message cannot be queued, because a cancel that
// silently did nothing would leave the user watching a column they believe they
// stopped — the one outcome worse than paying for a new session.
func (m *Model) interruptSeat(v model.VendorID) {
	p, ok := m.procs[v]
	if !ok {
		return
	}
	pv, ok := vendors.Registry()[v].(vendors.Persistent)
	if !ok {
		return
	}
	m.interrupts++
	line, err := pv.Interrupt("telltale-interrupt-" + strconv.Itoa(m.interrupts))
	if err == nil && p.sess.Send(line) == nil {
		return
	}
	p.sess.Kill()
	m.dropProcess(v)
}

// answerGate decides one permission request.
//
// This build has no way to ask: the approval UI is the next change, and until
// it lands the room must not be able to leave a vendor blocked forever on a
// question nobody can see. So the answer is a denial, and it says why in the
// words the vendor will read back.
//
// Denying rather than allowing is not caution for its own sake. An allow here
// would be a gate that approves everything while looking like a gate, which is
// the exact shape of false claim this repo exists to refuse — and it is
// unreachable in practice, because the postures this build passes never produce
// a request at all.
func (m *Model) answerGate(c *Column, g *runner.Gate) {
	if g == nil {
		return
	}
	p, ok := m.procs[c.Vendor]
	if !ok {
		return
	}
	pv, ok := vendors.Registry()[c.Vendor].(vendors.Persistent)
	if !ok {
		return
	}
	line, err := pv.Decide(g.RequestID, false,
		"denied: this build of telltale council has no approval gate, so there "+
			"is nobody to ask", nil)
	if err != nil {
		return
	}
	_ = p.sess.Send(line)
}
