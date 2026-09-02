package councilhost

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Room is the host's own state of the conversation, and it is what travels on
// the wire.
//
// It is a PROJECTION and deliberately not council.State (§7.28's last
// limitation). State carries pointers and rendered projections that a wire
// format cannot take without a parallel type, and building that type is the
// work that makes council's own Model the client's renderer. Naming the
// projection here keeps this rung from being read as having paid for it.
//
// Everything on this struct is either a number, a key, or vendor output the
// host is holding in memory to draw. NOTHING here is ever written to disk — see
// the package doc's third rule and design.md §7.28's read/write boundary.
type Room struct {
	// Version is the shape's own number, so a stored or logged Room can be
	// told apart from a later one. It is not the protocol version: a frame
	// carries that.
	Version int `json:"version"`
	// Workspace is the directory turns are dispatched against.
	Workspace string `json:"workspace"`
	// Turn counts dispatches this host has made. Zero means no turn yet, which
	// is a different fact from a turn that produced nothing (§4a.1).
	Turn int `json:"turn"`
	// Posture is the room's own word for what the seats may do, carried so a
	// client can draw it without asking a second question.
	Posture string `json:"posture"`
	// Notice is one line about the room itself rather than about a seat: what
	// the host refused, what it could not drive. Empty is the ordinary case.
	Notice string `json:"notice,omitempty"`
	// Seats is the roster, in the order the host was given it. Fixed order,
	// because position is the navigation on every other council surface.
	Seats []Seat `json:"seats"`
}

// Seat is one vendor's column, as the host holds it.
type Seat struct {
	Vendor model.VendorID `json:"vendor"`
	// Binary is the resolved path the host would spawn. Held so a client can
	// say which program a seat is, and so a refusal can name it.
	Binary string `json:"binary,omitempty"`
	// Phase is the seat's own word for what it is doing. The strings are the
	// vocabulary council already uses, and Waiting and Streaming stay apart for
	// the reason council.Phase states: they look the same on screen and they
	// are different claims.
	Phase Phase `json:"phase"`
	// SessionID is the vendor's own id for its conversation, which is what
	// makes the next turn a resume. A key, never content.
	SessionID string `json:"session_id,omitempty"`
	// Body is what the vendor has said this room, accumulated. IN MEMORY ONLY.
	// This is the field the transport's security descriptor exists for.
	Body string `json:"body,omitempty"`
	// Acts is the tool-call trace, one already-shortened line per call, in the
	// order the vendor announced them.
	Acts []string `json:"acts,omitempty"`
	// Note is the seat's own card line: a failure the vendor reported, or a
	// refusal the host made. Never composed from a guess — a note the host
	// wrote says so in its own words.
	Note string `json:"note,omitempty"`
	// ExitCode is set when a process exited and reported one. A pointer, so
	// "exited 0" and "no process has exited" stay different facts (§4a.1).
	ExitCode *int `json:"exit_code,omitempty"`
	// Drivable is false when the host will not drive this seat at all, and Note
	// then says why. A seat the host cannot drive is NOT rendered as a seat
	// that failed: those are two states and they render two ways.
	Drivable bool `json:"drivable"`
	// Persistent is true when this seat is ONE process that takes many turns
	// (vendors.Persistent). It decides which event ends a turn on this seat.
	//
	// A spawn-per-turn seat ends its turn by dying, and the runner reports
	// that as KindDone. A persistent process does not exit between turns, so
	// its turn ends with a line in its own stream — the vendor's `result`,
	// which the adapter reports as KindMeta with EndsTurn set — and no KindDone
	// is coming. The fold used to drop every KindMeta, so a persistent seat
	// whose answer was complete and on screen stayed `streaming` for the rest
	// of the room's life: it was still `streaming` after a detach, a closed
	// window and a rejoin, with the identical text. That is a false claim
	// about what an agent is doing (§4a.1), on the surface the whole split
	// exists to draw.
	//
	// The flag is set by the host from the adapter's interface, never guessed
	// from output, and it is a key on the wire so a client can say which seats
	// keep a process between turns.
	Persistent bool `json:"persistent,omitempty"`
}

// Phase is what a seat is doing right now.
//
// A string rather than council.Phase's integer, because this value crosses a
// process boundary and an integer that shifted between two builds would
// silently re-label every column. The words are council's own.
type Phase string

const (
	// PhaseIdle: no turn has been dispatched to this seat yet.
	PhaseIdle Phase = "idle"
	// PhaseWaiting: dispatched and running, with no incremental output
	// available from this vendor.
	PhaseWaiting Phase = "waiting"
	// PhaseStreaming: dispatched and running, with output arriving.
	PhaseStreaming Phase = "streaming"
	// PhaseDone: the seat finished this turn cleanly.
	PhaseDone Phase = "done"
	// PhaseFailed: the process died, or the vendor reported an error.
	PhaseFailed Phase = "failed"
	// PhaseCancelled: the turn was interrupted. Output already accumulated was
	// really produced, so the seat says the turn is partial rather than
	// pretending it completed.
	PhaseCancelled Phase = "cancelled"
	// PhaseUndrivable: the host will not drive this seat. Note says why.
	PhaseUndrivable Phase = "undrivable"
)

// RoomVersion is Room.Version's current value.
const RoomVersion = 1

// seatIndex finds a seat by vendor id. Returns -1 when the roster has none,
// which is the case an event for a vendor nobody seated arrives in.
func (r *Room) seatIndex(v model.VendorID) int {
	for i := range r.Seats {
		if r.Seats[i].Vendor == v {
			return i
		}
	}
	return -1
}

// Apply folds one runner event into the room.
//
// This is the whole of what makes the host a parser rather than a babysitter.
// It runs on the host's own goroutine, draining the same bounded channel
// council's Model drains, so a client that stops reading stalls the WIRE and
// never the vendors — which is the inversion this package exists for.
//
// Returns whether anything changed, so the coalescing tick can stay quiet when
// a line produced nothing a reader would see.
func (r *Room) Apply(ev runner.Event) bool {
	i := r.seatIndex(ev.Vendor)
	if i < 0 {
		return false
	}
	s := &r.Seats[i]
	switch ev.Kind {
	case runner.KindText:
		if ev.Text == "" {
			return false
		}
		s.Body += ev.Text
		// Text ARRIVING is what distinguishes streaming from waiting, and it is
		// read off the event rather than off the vendor's declared capability:
		// a vendor that says it streams and then does not must not draw as
		// though it did.
		if s.Phase == PhaseWaiting {
			s.Phase = PhaseStreaming
		}
		return true
	case runner.KindActivity:
		if len(ev.Acts) == 0 {
			return false
		}
		for _, a := range ev.Acts {
			line := actLine(a)
			if line == "" {
				continue
			}
			s.Acts = append(s.Acts, line)
		}
		return true
	case runner.KindSession:
		if ev.SessionID == "" || ev.SessionID == s.SessionID {
			return false
		}
		s.SessionID = ev.SessionID
		return true
	case runner.KindMeta:
		// A reported cost is a number this projection does not draw yet, so it
		// is DROPPED rather than stored unrendered. Carrying a value no surface
		// reads would invite a later session to render it without checking
		// where it came from, and §4a.1's rule is that a displayed value names
		// its source.
		//
		// The END-OF-TURN half of the same line is not dropped. On a persistent
		// seat this is the ONLY end-of-turn signal there is (Seat.Persistent's
		// doc has the measurement), so it is what moves the seat to done.
		//
		// On a spawn-per-turn seat the same flag arrives too — codex says
		// `turn.completed` seconds before it exits — and here it deliberately
		// changes NOTHING. council's own Model settles that column's phase on
		// this line and keeps the process live until the exit, because the
		// vendor's capture covers no turn that ran tools (vendors/codex.go).
		// This host's turn guard reads phases alone (watchTurn), so settling a
		// batch seat here would clear the guard while the child is still
		// exiting, and the next dispatch would kill it (dispatchBatch). The
		// exit still arrives as KindDone and settles the seat there, as it did
		// before; the linger it draws is the pre-existing state of this rung,
		// not a change made by this branch.
		if !ev.EndsTurn || !s.Persistent {
			return false
		}
		if s.Phase != PhaseWaiting && s.Phase != PhaseStreaming {
			// A turn that was already interrupted or failed keeps saying so. A
			// `result` that lands after the operator cancelled is the vendor
			// confirming the abandonment, not a completion.
			return false
		}
		if ev.SessionID != "" {
			s.SessionID = ev.SessionID
		}
		// The final reply is carried on this line as a fallback for a turn that
		// streamed nothing, and it is used ONLY when the body is empty, so the
		// ordinary streaming path never draws the reply twice. A clean end with
		// nothing at all says so in council's own words rather than drawing an
		// empty done column, which §4a.1 would read as a false zero.
		if strings.TrimSpace(s.Body) == "" {
			if ev.Text != "" {
				s.Body = ev.Text
			} else {
				s.Body = "[Turn completed with 0 text chunks streamed]"
			}
		}
		s.Phase = PhaseDone
		return true
	case runner.KindGate:
		// The gate is a card, a keystroke and an answer written back down the
		// same pipe. None of that is built here, so the seat says the host
		// refused rather than leaving the vendor blocked with nothing on
		// screen: a blocked seat and a slow seat must not render alike.
		s.Note = "this seat asked permission and the host cannot carry the question yet — " +
			"run this room with `telltale council` instead"
		return true
	case runner.KindDone:
		if ev.ExitCode != 0 {
			code := ev.ExitCode
			s.ExitCode = &code
		} else if !ev.EndsTurn {
			zero := 0
			s.ExitCode = &zero
		}
		s.Phase = PhaseDone
		return true
	case runner.KindError:
		// A process exit landing on a seat the vendor already failed keeps the
		// vendor's sentence. Only the runner's exit event carries Err, and a
		// seat that is already failed with a note when it lands got that note
		// from the vendor's own stream (codex's `turn.failed` at 0.151.0, agy's
		// `status: "ERROR"`). The exit code is drawn on the head line by
		// Render, so nothing the exit said is lost. This is the same rule as
		// council's dispatch.go KindError branch, and the same measurement:
		// before it, a hosted room showed `codex — failed (exit 1)` under a
		// stream that had said "You've hit your usage limit".
		spoke := !ev.EndsTurn && ev.Err != nil && s.Phase == PhaseFailed && s.Note != ""
		s.Phase = PhaseFailed
		if spoke {
			// keep s.Note
		} else if ev.Note != "" {
			s.Note = ev.Note
		} else if ev.Err != nil {
			s.Note = ev.Err.Error()
		}
		if ev.ExitCode != 0 {
			code := ev.ExitCode
			s.ExitCode = &code
		}
		return true
	}
	return false
}

// actLine renders one tool call the way the trace does: the vendor's own text,
// with its outcome as a word rather than as a colour.
//
// The outcome words are separate values on purpose, and ActUnknown is the one
// that earns the type: a vendor that reports a step ENDED without saying
// whether it worked is a different fact from a vendor reporting success, and
// collapsing them is the failure §4a.1 exists to forbid.
func actLine(a runner.ActCall) string {
	text := strings.TrimSpace(a.Text)
	if text == "" {
		return ""
	}
	switch a.Outcome {
	case runner.ActOK:
		return text + " — ok"
	case runner.ActFailed:
		if d := strings.TrimSpace(a.Detail); d != "" {
			return text + " — failed: " + d
		}
		return text + " — failed"
	case runner.ActUnknown:
		return text + " — ended, outcome not reported"
	case runner.ActDenied:
		return text + " — denied"
	default:
		return text
	}
}

// beginTurn moves every drivable seat to its starting phase for a new turn.
//
// Waiting rather than Streaming, always. A seat that has not produced a byte is
// waiting whether or not its vendor is capable of streaming, and Apply promotes
// it the moment text actually arrives.
func (r *Room) beginTurn() {
	r.Turn++
	for i := range r.Seats {
		if !r.Seats[i].Drivable {
			continue
		}
		r.Seats[i].Phase = PhaseWaiting
		r.Seats[i].Note = ""
		r.Seats[i].ExitCode = nil
	}
}

// clone deep-copies the room so a frame can be marshalled without holding the
// host's lock across a write.
//
// The slices are copied rather than shared. A shared Acts slice would be
// appended to by the fold goroutine while the writer marshalled it, which is
// the kind of race that shows up as a corrupted frame once a week.
func (r *Room) clone() *Room {
	out := *r
	out.Seats = make([]Seat, len(r.Seats))
	copy(out.Seats, r.Seats)
	for i := range out.Seats {
		if n := len(r.Seats[i].Acts); n > 0 {
			out.Seats[i].Acts = make([]string, n)
			copy(out.Seats[i].Acts, r.Seats[i].Acts)
		}
		if c := r.Seats[i].ExitCode; c != nil {
			v := *c
			out.Seats[i].ExitCode = &v
		}
	}
	return &out
}
