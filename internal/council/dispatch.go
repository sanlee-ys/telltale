package council

import (
	"context"
	"strconv"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// eventBuffer bounds the shared channel every vendor writes into.
//
// Bounded on purpose. When it fills, the reading goroutine blocks, the OS pipe
// fills behind it, and the vendor stalls — which the user sees as a column that
// has paused. The alternative, an unbounded channel, trades that visible pause
// for unbounded memory and an event backlog that renders minutes after the
// vendor produced it. A stalled column is honest; a lagging one is not.
const eventBuffer = 512

// drainMax is how many events one Update may consume.
//
// Token-level streaming produces events far faster than a terminal can usefully
// redraw. Batching caps redraws at one per Update regardless of token rate,
// while the bound keeps a fast vendor from starving input handling — the key
// that cancels the turn has to stay responsive while three agents talk.
const drainMax = 64

type turnState struct {
	cancel  context.CancelFunc
	handles []*runner.Handle
	// live counts columns that have not reached a terminal phase yet, so the
	// turn knows when it is over without polling.
	live int
}

type eventBatchMsg struct{ events []runner.Event }

type dispatchFailedMsg struct {
	vendor model.VendorID
	note   string
}

// dispatch sends the draft to every seated vendor.
//
// Turn 1 is blind: each vendor receives the brief alone, with no other vendor's
// answer attached. Later turns resume each vendor's OWN session, so a vendor
// still only ever sees its own history — the independence guarantee is
// structural rather than a formatting convention (ADR-008 §4).
func (m *Model) dispatch() tea.Cmd {
	if m.turn != nil {
		m.st.Notice = "a turn is already in flight — ctrl+c cancels it"
		return nil
	}
	if m.st.Draft == "" {
		m.st.Notice = "nothing to dispatch: the brief is empty"
		return nil
	}

	reg := vendors.Registry()
	// The mentions are stripped from what the vendors receive. A brief that
	// opens "@codex @claude compare these" should reach them as "compare
	// these" — the routing is addressing, not content, and leaving it in makes
	// every vendor read a header about who else is in the room.
	route, prompt := ParseRoute(m.st.Draft)
	if prompt == "" {
		m.st.Notice = "that is a mention with no brief after it"
		return nil
	}
	if n := m.seatedIn(route); n == 0 {
		// Addressed only to vendors that are not seated. Dispatching to nobody
		// while the columns sat idle would look like the key did nothing.
		m.st.Notice = "none of the vendors you addressed are seated"
		return nil
	}

	// Turn 1 is blind no matter what is armed: the whole value of the room is
	// three opinions formed without sight of each other, and a first round that
	// quoted anything would have nothing to quote but would still establish the
	// wrong precedent in the code.
	quoting := m.st.Quote && m.st.Turn > 0
	// Snapshotted BEFORE any column is reset, because the loop below clears the
	// bodies it would otherwise be quoting.
	priorReplies := append([]Column(nil), m.st.Columns...)

	ctx, cancel := context.WithCancel(context.Background())
	ts := &turnState{cancel: cancel}
	var failures []dispatchFailedMsg

	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if c.Avail != AvailInstalled {
			continue
		}
		if !route.addresses(c.Vendor) {
			// Not in this turn. Its previous reply stays on screen, because
			// that is still the last thing this vendor said — but the note
			// makes clear it is not participating, so a stale answer beside two
			// fresh ones cannot be mistaken for a third opinion on the new
			// brief.
			c.Note = "not addressed in turn " + itoa(m.st.Turn+1)
			continue
		}
		v, ok := reg[c.Vendor]
		if !ok {
			// A seat detected but not yet drivable. Said out loud rather than
			// left looking idle: this is the honest shape of a half-built
			// feature, and it disappears as each adapter lands.
			c.Phase = PhaseFailed
			c.Note = "no adapter yet — this column arrives in a later build"
			continue
		}

		// Each vendor may receive a DIFFERENT prompt on a quoting turn, since
		// each one is shown the others' answers and not its own.
		vendorPrompt := prompt
		if quoting {
			vendorPrompt = BuildRebuttalPrompt(prompt, *c, priorReplies)
		}

		spec, err := m.specFor(v, c, vendorPrompt)
		if err != nil {
			failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
			continue
		}

		h, err := runner.Start(ctx, spec, m.events, v.ParseEvent)
		if err != nil {
			failures = append(failures, dispatchFailedMsg{c.Vendor, err.Error()})
			continue
		}

		ts.handles = append(ts.handles, h)
		ts.live++
		c.Phase = PhaseStreaming
		if c.Gran == GranFinalOnly {
			c.Phase = PhaseWaiting
		}
		c.Body = ""
		c.Note = ""
		c.CostUSD = nil
		// Re-arm the tail for the new turn. Whatever the user was reading
		// belonged to the previous answer, which this column just cleared.
		c.Follow = true
		c.Scroll = 0
		m.redactors[c.Vendor] = &Redactor{}
	}

	for _, f := range failures {
		if c := m.column(f.vendor); c != nil {
			c.Phase = PhaseFailed
			c.Note = f.note
		}
	}

	if ts.live == 0 {
		cancel()
		m.st.Notice = "no vendor could be dispatched to — see the columns"
		return nil
	}

	m.turn = ts
	m.st.Turn++
	m.st.Mode = ModeViewing
	m.setDraft("")
	m.st.Notice = ""
	return m.waitEvents()
}

// seatedIn counts how many installed columns a route actually reaches.
func (m *Model) seatedIn(route Route) int {
	n := 0
	for _, c := range m.st.Columns {
		if c.Avail == AvailInstalled && route.addresses(c.Vendor) {
			n++
		}
	}
	return n
}

// itoa is strconv.Itoa under a shorter name, kept local so the dispatch path
// reads as prose.
func itoa(i int) string { return strconv.Itoa(i) }

// specFor builds this vendor's invocation for the current turn.
//
// A vendor with a session id resumes it; one without starts fresh. A resume
// that the vendor refuses is not silently downgraded — ErrNoResume falls back
// to a first turn, and the column says the thread was lost.
func (m *Model) specFor(v vendors.Vendor, c *Column, prompt string) (runner.Spec, error) {
	if id := m.sessions[c.Vendor]; id != "" {
		spec, err := v.NextTurn(prompt, m.st.Workspace, c.Binary, id)
		if err == nil {
			return spec, nil
		}
	}
	return v.FirstTurn(prompt, m.st.Workspace, c.Binary)
}

// waitEvents blocks on one event, then drains what is already queued into a
// single batch. One redraw per batch instead of one per token.
func (m *Model) waitEvents() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		batch := []runner.Event{ev}
		for len(batch) < drainMax {
			select {
			case next, ok := <-ch:
				if !ok {
					return eventBatchMsg{batch}
				}
				batch = append(batch, next)
			default:
				return eventBatchMsg{batch}
			}
		}
		return eventBatchMsg{batch}
	}
}

func (m *Model) applyEvents(batch []runner.Event) {
	for _, ev := range batch {
		c := m.column(ev.Vendor)
		if c == nil {
			continue
		}
		switch ev.Kind {
		case runner.KindText:
			// Every byte of vendor output reaches state through the redactor,
			// which is the single choke point Render can be reasoned about from.
			c.Body += m.redact(ev.Vendor, ev.Text)
			if c.Phase == PhaseWaiting {
				// It streamed after all. Upgrading the phase is honest in this
				// direction only: the column now IS showing incremental output.
				c.Phase = PhaseStreaming
			}

		case runner.KindSession:
			if ev.SessionID != "" {
				m.sessions[ev.Vendor] = ev.SessionID
			}

		case runner.KindMeta:
			if ev.SessionID != "" {
				m.sessions[ev.Vendor] = ev.SessionID
			}
			if ev.CostUSD != nil {
				c.CostUSD = ev.CostUSD
			}
			// The final-result fallback: used ONLY when the turn streamed
			// nothing, so a vendor whose partial events did not arrive still
			// shows its reply instead of an empty column.
			if c.Body == "" && ev.Text != "" {
				c.Body = m.redact(ev.Vendor, ev.Text) + m.flush(ev.Vendor)
			}

		case runner.KindDone:
			c.Body += m.flush(ev.Vendor)
			if c.Phase == PhaseStreaming || c.Phase == PhaseWaiting {
				c.Phase = PhaseDone
				if m.cancelling {
					c.Phase = PhaseCancelled
					c.Note = "cancelled — the output above is partial"
				}
			}
			m.turnColumnFinished()

		case runner.KindError:
			c.Body += m.flush(ev.Vendor)
			c.Phase = PhaseFailed
			if ev.Note != "" {
				c.Note = ev.Note
			} else if ev.Err != nil {
				c.Note = ev.Err.Error()
			}
			// A vendor-reported failure arrives BEFORE the process exits, so
			// this is not the end of the column's life; KindDone still follows
			// and is what decrements the live count.
			if ev.ExitCode != 0 || ev.Err != nil {
				m.turnColumnFinished()
			}
		}
	}
}

// turnColumnFinished retires one column from the turn and tears the turn down
// when the last one lands.
func (m *Model) turnColumnFinished() {
	if m.turn == nil {
		return
	}
	m.turn.live--
	if m.turn.live > 0 {
		return
	}
	m.turn.cancel()
	m.turn = nil
	m.cancelling = false
	m.st.Mode = ModeComposing
}

// cancelTurn stops everything in flight. The columns keep whatever they already
// received: that output was really produced, and the card says it is partial
// rather than implying the turn completed.
func (m *Model) cancelTurn() {
	if m.turn == nil {
		return
	}
	m.cancelling = true
	m.st.Notice = "cancelling…"
	for _, h := range m.turn.handles {
		h.Kill()
	}
	m.turn.cancel()
}

// teardown kills every child on the way out.
//
// Without this, quitting the room would leave agents running, holding sessions
// and spending quota, with nothing on screen to say so — the exact invisible
// state this product exists to refuse.
func (m *Model) teardown() {
	if m.turn == nil {
		return
	}
	for _, h := range m.turn.handles {
		h.Kill()
	}
	m.turn.cancel()
	m.turn = nil
}

func (m *Model) column(v model.VendorID) *Column {
	for i := range m.st.Columns {
		if m.st.Columns[i].Vendor == v {
			return &m.st.Columns[i]
		}
	}
	return nil
}

func (m *Model) redact(v model.VendorID, s string) string {
	r, ok := m.redactors[v]
	if !ok {
		r = &Redactor{}
		m.redactors[v] = r
	}
	return sanitize(r.Feed(s))
}

func (m *Model) flush(v model.VendorID) string {
	r, ok := m.redactors[v]
	if !ok {
		return ""
	}
	return sanitize(r.Flush())
}
