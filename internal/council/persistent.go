package council

import (
	"strconv"
	"strings"

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
	// resumed reports that this process was launched on a session id restored
	// from a saved room, rather than opening a new conversation.
	//
	// Its only job is the brief: a resumed process already has the operating
	// context in the history it is replaying, so re-sending it would spend the
	// whole brief again for nothing. Whether the resume WORKED is not tracked
	// here — that is settleRestoredThread's question, and it is asked the same
	// way for all four seats rather than twice in two places.
	resumed bool
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
	//
	// A RESUMED process is the exception, and it is the same rule rather than a
	// carve-out from it: the brief is already in the history it is replaying, so
	// re-sending it would spend the whole thing again per turn against a metered
	// quota for a vendor that has already read it (brief.Apply's own reasoning).
	if p.sent == 0 && !p.resumed {
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

	spec, err := v.Session(m.st.Workspace, c.Binary, m.seatPosture())
	if err != nil {
		return nil, "", err
	}
	// A thread restored from a saved room is spent HERE, on the first process
	// this seat opens, and forgotten whether or not it works.
	//
	// One attempt, never a loop, and that is the load-bearing part. A stale id
	// makes the vendor exit immediately (measured: `No conversation found with
	// session ID`, exit 1, no model turn spent), so a seat that retried it would
	// refuse every brief for the rest of the session with the same error. Spent
	// once, the next dispatch opens a new conversation and briefs it, which is
	// the behaviour seatProcess already had for a seat whose process died.
	resumed := false
	if id := m.resumeIDs[c.Vendor]; id != "" {
		delete(m.resumeIDs, c.Vendor)
		if rs, rerr := v.SessionResume(m.st.Workspace, c.Binary, id, m.seatPosture()); rerr == nil {
			spec = rs
			resumed = true
		}
	}
	// The ROOM's context, never the turn's. A turn that is cancelled must not
	// take this process with it — that is the entire point of keeping it — so
	// only quitting the room cancels this.
	sess, err := runner.StartSession(m.roomCtx, spec, m.events, v.ParseEvent)
	if err != nil {
		return nil, "", err
	}
	p := &seatProc{sess: sess, resumed: resumed}
	m.procs[c.Vendor] = p

	if resumed {
		// Deliberately NOT "reattached to the saved thread". Nothing has come
		// back yet — the process has been launched and that is all — and a
		// column claiming a resume it has not seen evidence of is this repo's own
		// failure mode. The card the room opened with already says a thread was
		// restored; if the vendor refuses it, resumeFailed replaces that with the
		// truth.
		return p, "", nil
	}
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

// queueGate puts one blocked tool call in front of the user.
//
// The vendor is stopped until this is answered, which is the property the whole
// feature rests on and also the reason nothing here may quietly drop a request:
// a gate that vanished would leave a column waiting forever with no card to
// explain it.
func (m *Model) queueGate(c *Column, g *runner.Gate) {
	if g == nil || g.RequestID == "" {
		return
	}
	// Redacted whole, like every other complete string a vendor produced. It
	// matters more here than in prose: the argument line of a tool call is one
	// of the likeliest places for a token to appear on screen, and this one is
	// rendered in chrome that does not scroll away.
	text := strings.TrimSpace(m.redactWhole(g.Text))
	if text == "" {
		text = g.Tool
	}
	if m.gateInputs == nil {
		m.gateInputs = map[string]map[string]any{}
	}
	m.st.Gates = append(m.st.Gates, PendingGate{
		Vendor:    c.Vendor,
		RequestID: g.RequestID,
		ToolUseID: g.ToolUseID,
		Text:      text,
	})
	m.gateInputs[g.RequestID] = g.Input
}

// decideGate answers the OLDEST pending request.
//
// Oldest first because that is the one the card is showing. Answering anything
// else would decide a call the user was not looking at, which on an approval
// gate is not a UI wrinkle — it is approving the wrong thing.
func (m *Model) decideGate(allow bool) {
	if len(m.st.Gates) == 0 {
		return
	}
	pending := m.st.Gates[0]
	m.st.Gates = m.st.Gates[1:]
	delete(m.gateInputs, pending.RequestID)

	c := m.column(pending.Vendor)
	if !allow && c != nil {
		// Recorded NOW, from the keystroke, rather than later from the vendor's
		// echo of it. The vendor reports a denial as an is_error tool_result
		// carrying this refusal text back, which read off the stream alone is
		// indistinguishable from a tool that broke.
		c.recordAct(runner.ActCall{
			ID:      pending.ToolUseID,
			Text:    pending.Text,
			Outcome: runner.ActDenied,
		}, m.redactWhole)
	}

	m.sendDecision(pending, allow)

	if len(m.st.Gates) == 0 {
		m.st.Notice = ""
	}
}

// denialText is what the model reads back when a call is refused.
//
// It names WHO refused. "Denied" alone reads to a model as an obstacle to route
// around, and the observed behaviour of a vendor told only that much is to try
// a slightly different spelling of the same call — which produces a second
// request for a user who has already said no once.
const denialText = "denied by the person running this council room. " +
	"Do not retry this call or a variation of it; say what you wanted to do and why."

func (m *Model) sendDecision(pending PendingGate, allow bool) {
	p, ok := m.procs[pending.Vendor]
	if !ok {
		return
	}
	pv, ok := vendors.Registry()[pending.Vendor].(vendors.Persistent)
	if !ok {
		return
	}
	line, err := pv.Decide(pending.RequestID, allow, denialText,
		m.gateInputs[pending.RequestID])
	if err != nil {
		return
	}
	if err := p.sess.Send(line); err != nil && m.column(pending.Vendor) != nil {
		m.column(pending.Vendor).Note = "the decision could not be delivered: " + err.Error()
	}
}

// dropGates discards a seat's pending requests.
//
// Called when the turn they belong to ends by any route — cancelled,
// interrupted, or the process dying. A card left on screen for a vendor that is
// no longer waiting would invite a keystroke that decides nothing, and the room
// would keep saying it was gating.
func (m *Model) dropGates(v model.VendorID) {
	kept := m.st.Gates[:0]
	for _, g := range m.st.Gates {
		if g.Vendor == v {
			delete(m.gateInputs, g.RequestID)
			continue
		}
		kept = append(kept, g)
	}
	m.st.Gates = kept
}
