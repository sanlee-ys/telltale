package councilhost

import (
	"strconv"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Room is the host's own state of the conversation, and it is what travels on
// the wire.
//
// It is a PROJECTION and deliberately not council.State. State carries facts
// that belong to the client's machine and to the reader — the posture claim
// for each vendor, the focus, the scroll, the pane sizes, the help page, the
// draft — and a host that owned those would own the reader's eyes. What this
// carries is the CONVERSATION: every fact council's Render reads about a seat,
// widened in design.md §7.31 from the words-only projection §7.28 shipped, so
// that package council can build a State from it with one pure function
// (stateFromRoom) and draw a hosted room with its own columns.
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
//
// The per-turn fields — Turn, Prompt, Body, Acts, Note, NoteDetail, Started,
// Ended, Elapsed, CostUSD, CostSession, Settling, Skipped, ExitCode — describe
// the seat's CURRENT turn, and startTurn files them into History when the next
// one begins. That is council's own Column.startTurn, mirrored here because
// the TUI's transcript, turn page and act ledger all read History (§7.31).
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
	// Turn is which dispatch Body, Acts and Prompt belong to. Zero means this
	// seat has never been dispatched to, which is what keeps an unaddressed
	// column's history truthful: it records nothing for a turn it sat out.
	Turn int `json:"turn,omitempty"`
	// Prompt is the brief this seat received on its current turn, as the
	// operator typed it. IN MEMORY ONLY, like Body.
	Prompt string `json:"prompt,omitempty"`
	// Body is what the vendor has said on its current turn. IN MEMORY ONLY.
	// This is the field the transport's security descriptor exists for.
	Body string `json:"body,omitempty"`
	// Acts is the tool-call trace for the current turn, in the order the
	// vendor announced the calls, each with what the vendor said about how it
	// went. Structured rather than rendered (§7.31): a string had already
	// folded the outcome into words, and the TUI draws the outcome as a mark.
	Acts []Act `json:"acts,omitempty"`
	// History is the turns this seat has finished, OLDEST FIRST, capped at
	// maxHistory the way council's own transcript is.
	History []TurnRecord `json:"history,omitempty"`
	// Note is the seat's own card line: a failure the vendor reported, or a
	// refusal the host made. Never composed from a guess — a note the host
	// wrote says so in its own words.
	Note string `json:"note,omitempty"`
	// NoteDetail is the note's body: the mechanics, demoted below the title.
	// Empty for most notes, which are one sentence and the whole story.
	NoteDetail string `json:"note_detail,omitempty"`
	// Skipped reports that Note describes a turn this seat SAT OUT rather than
	// a turn it took, so the client draws it as a skip and not as a warning,
	// and startTurn does not file it into the record it pushes.
	Skipped bool `json:"skipped,omitempty"`
	// ExitCode is set when a process exited and reported one. A pointer, so
	// "exited 0" and "no process has exited" stay different facts (§4a.1).
	ExitCode *int `json:"exit_code,omitempty"`
	// Started is when the current turn was dispatched to this seat, on the
	// host's clock. Zero on a seat that has never taken one. The client's
	// tick measures the live clock against it; both processes read the same
	// machine's clock.
	Started time.Time `json:"started,omitempty"`
	// Ended is when the current turn was retired, on the host's clock. Zero
	// while the turn is live. The client's inbox strip reads it (§9.54).
	Ended time.Time `json:"ended,omitempty"`
	// Elapsed is how long the current turn took once it settled, stamped once
	// and kept, so a finished column can still say how long it made the
	// operator wait. Wall clock; a hosted room raises no gate, so there is no
	// operator share to take back out.
	Elapsed time.Duration `json:"elapsed,omitempty"`
	// CostUSD is the spend AS REPORTED BY THE VENDOR, a pointer so "reported
	// zero" and "reported nothing" stay apart. CostSession reports that the
	// figure is a persistent process's running total rather than this turn's
	// spend, which is what the badge says beside it.
	CostUSD     *float64 `json:"cost_usd,omitempty"`
	CostSession bool     `json:"cost_session,omitempty"`
	// Tokens is this turn's token count AS REPORTED BY THE VENDOR, on
	// CostUSD's terms: a pointer, so a seat that counted zero and a seat that
	// sent no count stay apart on the wire, and two integers under two keys
	// (numbers and keys, never content). Per-turn by construction — the one
	// wire that reports it says so beside its prompt id (council's
	// vendors/acp.go) — so it carries no `session` word.
	Tokens *TokenCounts `json:"tokens,omitempty"`
	// Settling reports that this seat has ANSWERED and its process has not
	// exited yet: the column is terminal, the turn is not (§9.33). A batch seat
	// that says `turn.completed` seconds before it exits spends those seconds
	// here, and the dispatch guard reads it, so a re-dispatch cannot kill a
	// child that is still winding down.
	Settling bool `json:"settling,omitempty"`
	// Drivable is false when the host will not drive this seat at all, and Note
	// then says why. A seat the host cannot drive is NOT rendered as a seat
	// that failed: those are two states and they render two ways.
	Drivable bool `json:"drivable"`
	// FellBack is true when the host drives this seat through its measured
	// batch adapter instead of the live request/response shape the registry
	// names for it (vendors.LiveFallback; design.md §7.31). A key on the wire
	// so the client's posture badge can say which shape the seat is in, the
	// way the room's own badge does after a retreat (fallback.go).
	FellBack bool `json:"fell_back,omitempty"`
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

// Act is one thing a vendor did this turn, and what is known about how it went.
//
// Status is a WORD on the wire rather than runner.ActStatus's integer, for the
// reason Phase is: this value crosses a process boundary, and an integer that
// shifted between two builds would silently re-label every call.
type Act struct {
	// ID is the vendor's own id for the call, held so a result arriving later
	// finds the entry it belongs to. A key, never rendered.
	ID string `json:"id,omitempty"`
	// Text is the call as it will be shown: "Bash: go test ./...".
	Text string `json:"text"`
	// Status is what the vendor said about the outcome: one of the ActStatus
	// words below.
	Status string `json:"status,omitempty"`
	// Detail is the vendor's own first line about a failure. Empty whenever
	// the vendor gave none, which is most of the time.
	Detail string `json:"detail,omitempty"`
}

// The act status words. ActPending is the empty string, so an announced call
// with no result yet marshals with no status key at all.
const (
	ActPending = ""
	ActOK      = "ok"
	ActFailed  = "failed"
	ActUnknown = "unknown"
	ActDenied  = "denied"
)

// actStatusWord maps the runner's outcome onto the wire word.
func actStatusWord(s runner.ActStatus) string {
	switch s {
	case runner.ActOK:
		return ActOK
	case runner.ActFailed:
		return ActFailed
	case runner.ActUnknown:
		return ActUnknown
	case runner.ActDenied:
		return ActDenied
	default:
		return ActPending
	}
}

// TurnRecord is one finished turn on one seat: the per-turn fields as they
// stood when the next turn began.
type TurnRecord struct {
	N           int           `json:"n"`
	Prompt      string        `json:"prompt,omitempty"`
	Body        string        `json:"body,omitempty"`
	Acts        []Act         `json:"acts,omitempty"`
	Note        string        `json:"note,omitempty"`
	NoteDetail  string        `json:"note_detail,omitempty"`
	Phase       Phase         `json:"phase"`
	ExitCode    *int          `json:"exit_code,omitempty"`
	Elapsed     time.Duration `json:"elapsed,omitempty"`
	CostUSD     *float64      `json:"cost_usd,omitempty"`
	CostSession bool          `json:"cost_session,omitempty"`
	Tokens      *TokenCounts  `json:"tokens,omitempty"`
}

// TokenCounts is a reported count on the wire: the vendor's two integers, and
// nothing derived from them.
type TokenCounts struct {
	In  int64 `json:"in"`
	Out int64 `json:"out"`
}

// maxHistory is how many finished turns one seat keeps, matched to council's
// own transcript cap for the same reason: the room remembers a conversation,
// and a cap reachable in an afternoon is a room that forgets. Every frame
// carries the whole history, and §7.31 names that cost rather than hiding it.
const maxHistory = 50

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

// RoomVersion is Room.Version's current value. Bumped by §7.31, which widened
// Seat and changed Acts from strings to structs.
const RoomVersion = 2

// cancelledNote is council's own sentence for a turn the operator stopped
// (dispatch.go, finishColumn), so the two rooms say the same thing.
const cancelledNote = "cancelled — the output above is partial"

// busy reports that this seat cannot take a brief right now: its turn is open,
// or its process is still exiting after its answer (Settling). The same test
// council.Column.inFlight makes, and the dispatch guard reads it under the
// room's lock.
func (s Seat) busy() bool {
	return s.Phase == PhaseWaiting || s.Phase == PhaseStreaming || s.Settling
}

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

// Apply folds one runner event into the room, on the wall clock.
func (r *Room) Apply(ev runner.Event) bool { return r.applyAt(ev, time.Now()) }

// applyAt folds one runner event into the room, stamping now where a turn
// settles.
//
// This is the whole of what makes the host a parser rather than a babysitter.
// It runs on the host's own goroutine, draining the same bounded channel
// council's Model drains, so a client that stops reading stalls the WIRE and
// never the vendors — which is the inversion this package exists for.
//
// Returns whether anything changed, so the coalescing tick can stay quiet when
// a line produced nothing a reader would see.
func (r *Room) applyAt(ev runner.Event, now time.Time) bool {
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
		changed := false
		for _, a := range ev.Acts {
			if s.recordAct(a) {
				changed = true
			}
		}
		return changed
	case runner.KindSession:
		if ev.SessionID == "" || ev.SessionID == s.SessionID {
			return false
		}
		s.SessionID = ev.SessionID
		return true
	case runner.KindMeta:
		changed := false
		if ev.SessionID != "" && ev.SessionID != s.SessionID {
			s.SessionID = ev.SessionID
			changed = true
		}
		// A reported cost is the vendor's own figure, and since §7.31 the TUI
		// draws it, so it is kept. A persistent process reports a RUNNING
		// TOTAL rather than this turn's spend (council measured $0.1061493 then
		// $0.1177296 across two turns of one process), and CostSession is how
		// the badge says which figure it is instead of council inventing one.
		if ev.CostUSD != nil {
			c := *ev.CostUSD
			s.CostUSD = &c
			s.CostSession = s.Persistent
			changed = true
		}
		// A reported count lands beside a reported cost, copied rather than
		// shared for clone's reason: the writer marshals this seat while the
		// fold goroutine moves on.
		if ev.Tokens != nil {
			s.Tokens = &TokenCounts{In: ev.Tokens.Input, Out: ev.Tokens.Output}
			changed = true
		}
		if !ev.EndsTurn {
			return changed
		}
		if s.Phase != PhaseWaiting && s.Phase != PhaseStreaming {
			// A turn that was already interrupted or failed keeps saying so. A
			// `result` that lands after the operator cancelled is the vendor
			// confirming the abandonment, not a completion.
			return changed
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
		s.stampElapsed(now)
		if s.Persistent {
			// On a persistent seat this is the ONLY end-of-turn signal there is
			// (Seat.Persistent's doc has the measurement), so the turn retires
			// here.
			s.Ended = now
			return true
		}
		// A SPAWN-PER-TURN seat that names its own end of turn: codex says
		// `turn.completed` seconds before it exits (4.06s and 4.25s measured on
		// codex-cli 0.147.0; 7.94s in §9.33). The PHASE settles here and the
		// turn does not: the seat stays busy until the exit, because the exit
		// is what frees the handle, and a dispatch that landed in the gap would
		// kill a child still winding down (dispatchBatch). Settling is the
		// linger made visible, which is §9.33's own rule carried across the
		// process boundary; before §7.31 this host left the batch seat
		// `streaming` for those seconds instead.
		s.Settling = true
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
		// The process is gone, so the linger is over by definition, and a
		// column that was still live settles here. A column that already
		// settled — or was cancelled, or failed — keeps its word: the exit of
		// a cancelled process is not a completion (§4a.1).
		s.Settling = false
		if s.Phase == PhaseWaiting || s.Phase == PhaseStreaming {
			if strings.TrimSpace(s.Body) == "" {
				s.Body = "[Turn completed with 0 text chunks streamed]"
			}
			s.Phase = PhaseDone
		}
		s.stampElapsed(now)
		s.Ended = now
		return true
	case runner.KindError:
		// A process exit landing on a seat the vendor already failed keeps the
		// vendor's sentence. Only the runner's exit event carries Err, and a
		// seat that is already failed with a note when it lands got that note
		// from the vendor's own stream (codex's `turn.failed` at 0.151.0, agy's
		// `status: "ERROR"`). The exit goes to the detail line rather than
		// over the title, which is council's dispatch.go KindError rule and
		// the same measurement: before it, a hosted room showed `codex —
		// failed (exit 1)` under a stream that had said "You've hit your
		// usage limit".
		spoke := !ev.EndsTurn && ev.Err != nil && s.Phase == PhaseFailed && s.Note != ""
		s.Phase = PhaseFailed
		switch {
		case spoke:
			if s.NoteDetail == "" {
				s.NoteDetail = ev.Note
				if s.NoteDetail == "" {
					s.NoteDetail = ev.Err.Error()
				}
			}
		case ev.Note != "":
			s.Note = ev.Note
		case ev.Err != nil:
			s.Note = ev.Err.Error()
		}
		if ev.ExitCode != 0 {
			code := ev.ExitCode
			s.ExitCode = &code
		}
		s.Settling = false
		s.stampElapsed(now)
		s.Ended = now
		return true
	}
	return false
}

// stampElapsed records the turn's wall time once. A seat that answered and
// then failed on its way out keeps the time to its ANSWER: restamping would
// hand the column the process's whole lifetime, the figure §9.33's settle
// exists to stop billing.
func (s *Seat) stampElapsed(now time.Time) {
	if s.Elapsed == 0 && !s.Started.IsZero() {
		s.Elapsed = now.Sub(s.Started)
	}
}

// recordAct folds one tool call or result into the trace, correlated by the
// vendor's own id rather than by arrival order. council's Column.recordAct,
// mirrored: the first live Claude probe returned the second call's failure
// ahead of the first call's success, so a trace zipped by position would have
// blamed the wrong command.
//
// A result whose id matches nothing still lands as an already-resolved entry,
// which keeps the trace honest for a vendor that reports only completions.
func (s *Seat) recordAct(a runner.ActCall) bool {
	text := strings.TrimSpace(a.Text)
	if a.ID != "" {
		for i := range s.Acts {
			if s.Acts[i].ID != a.ID {
				continue
			}
			if s.Acts[i].Status == ActDenied {
				// A denial is a record of a keystroke, and the vendor echoes
				// it back as an error result. Letting that overwrite the
				// entry would turn "not allowed" into "failed".
				return false
			}
			changed := false
			if text != "" && s.Acts[i].Text != text {
				s.Acts[i].Text = text
				changed = true
			}
			// A second announcement never un-resolves an entry: a known
			// outcome downgraded back to pending would make a finished call
			// look like a running one.
			if a.Outcome != runner.ActPending {
				s.Acts[i].Status = actStatusWord(a.Outcome)
				s.Acts[i].Detail = strings.TrimSpace(a.Detail)
				changed = true
			}
			return changed
		}
	}
	if text == "" {
		// A result for a call never announced, carrying no text of its own.
		// There is nothing to name it by, so there is nothing worth drawing.
		return false
	}
	s.Acts = append(s.Acts, Act{
		ID:     a.ID,
		Text:   text,
		Status: actStatusWord(a.Outcome),
		Detail: strings.TrimSpace(a.Detail),
	})
	return true
}

// beginTurn opens turn n on the seats that will take it, and marks the
// drivable seats that will not.
//
// The caller has already decided which seats are accepted, under the lock, by
// reading busy(); this only moves the state. Waiting rather than Streaming,
// always: a seat that has not produced a byte is waiting whether or not its
// vendor is capable of streaming, and applyAt promotes it the moment text
// actually arrives. A seat the dispatch did not name and that is not busy is
// told so, in council's own words and with Skipped set, so a stale answer
// beside two fresh ones cannot be read as a third opinion on the new brief.
func (r *Room) beginTurn(n int, prompt string, accepted map[model.VendorID]bool, now time.Time) {
	r.Turn = n
	for i := range r.Seats {
		s := &r.Seats[i]
		if !s.Drivable {
			continue
		}
		if accepted[s.Vendor] {
			s.startTurn(n, prompt, now)
			continue
		}
		if s.busy() {
			// Still answering an earlier brief. Untouched: every per-turn
			// field on it describes the turn it is on.
			continue
		}
		s.Note = "not addressed in turn " + itoa(n)
		s.NoteDetail = ""
		s.Skipped = true
	}
}

// startTurn moves a seat onto a new turn, filing the finished one into History.
//
// Only a seat that has actually taken a turn records one. A seat dispatched to
// for the first time has nothing behind it.
func (s *Seat) startTurn(n int, prompt string, now time.Time) {
	if s.Turn > 0 {
		rec := TurnRecord{
			N: s.Turn, Prompt: s.Prompt, Body: s.Body, Acts: s.Acts,
			Note: s.Note, NoteDetail: s.NoteDetail, Phase: s.Phase,
			ExitCode: s.ExitCode, Elapsed: s.Elapsed,
			CostUSD: s.CostUSD, CostSession: s.CostSession,
			Tokens: s.Tokens,
		}
		if s.Skipped {
			// The note on the seat is about a turn it sat out, which is a
			// LATER turn than the one being filed. Carrying it would put "not
			// addressed in turn 7" under turn 1's separator.
			rec.Note, rec.NoteDetail = "", ""
		}
		s.History = append(s.History, rec)
		if len(s.History) > maxHistory {
			s.History = s.History[len(s.History)-maxHistory:]
		}
	}
	s.Turn = n
	s.Prompt = prompt
	s.Body = ""
	// Nil rather than truncated: the record above now owns that slice.
	s.Acts = nil
	s.Note = ""
	s.NoteDetail = ""
	s.Skipped = false
	s.Settling = false
	s.ExitCode = nil
	s.CostUSD = nil
	s.CostSession = false
	s.Tokens = nil
	s.Started = now
	s.Ended = time.Time{}
	s.Elapsed = 0
	s.Phase = PhaseWaiting
}

// cancel marks a running seat's turn as stopped by the operator, at now.
//
// Cancelled and not Failed: output already on screen was really produced, and
// blaming the vendor for the operator's keystroke is the distinction
// council.PhaseCancelled exists for. A seat that is not running is left alone,
// so a late interrupt cannot re-label a finished turn.
func (s *Seat) cancel(now time.Time) bool {
	if s.Phase != PhaseWaiting && s.Phase != PhaseStreaming {
		return false
	}
	s.Phase = PhaseCancelled
	s.Note = cancelledNote
	s.stampElapsed(now)
	s.Ended = now
	return true
}

// clone deep-copies the room so a frame can be marshalled without holding the
// host's lock across a write.
//
// The slices and the pointers are copied rather than shared. A shared Acts
// slice would be appended to by the fold goroutine while the writer marshalled
// it, which is the kind of race that shows up as a corrupted frame once a week.
func (r *Room) clone() *Room {
	out := *r
	out.Seats = make([]Seat, len(r.Seats))
	copy(out.Seats, r.Seats)
	for i := range out.Seats {
		s := &out.Seats[i]
		s.Acts = cloneActs(s.Acts)
		s.ExitCode = cloneInt(s.ExitCode)
		s.CostUSD = cloneFloat(s.CostUSD)
		s.Tokens = cloneTokens(s.Tokens)
		if n := len(r.Seats[i].History); n > 0 {
			s.History = make([]TurnRecord, n)
			copy(s.History, r.Seats[i].History)
			for j := range s.History {
				h := &s.History[j]
				h.Acts = cloneActs(h.Acts)
				h.ExitCode = cloneInt(h.ExitCode)
				h.CostUSD = cloneFloat(h.CostUSD)
				h.Tokens = cloneTokens(h.Tokens)
			}
		}
	}
	return &out
}

func cloneActs(in []Act) []Act {
	if len(in) == 0 {
		return nil
	}
	out := make([]Act, len(in))
	copy(out, in)
	return out
}

func cloneInt(p *int) *int {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneFloat(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cloneTokens(p *TokenCounts) *TokenCounts {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func itoa(n int) string { return strconv.Itoa(n) }
