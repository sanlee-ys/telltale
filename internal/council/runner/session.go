package runner

import (
	"context"
	"errors"
	"os/exec"
	"sync"
	"time"
)

// Session is a vendor process that outlives a single turn.
//
// The reason it exists is a measurement, not a preference. Every turn used to
// pay a full session init — a one-word "gm" cost about 25 seconds and $0.23,
// nearly all of it startup — because each turn was a fresh `claude -p
// --resume`. Feeding turns to ONE process instead spends that once per room.
//
// The second reason is the one that could not be bought any other way: a batch
// process cannot ask permission. Its stdin was written and closed before the
// first token arrived, so there is no channel for it to ask on and none for an
// answer to come back on. A session keeps stdin open in both directions, which
// is what makes a per-action gate possible at all (see Gate).
//
// Everything Start guarantees still holds here and for the same reasons: the
// child lives in the same kill-on-close job object, its output crosses the same
// bounded channel, and no prompt text ever reaches argv.
type Session struct {
	cmd   *exec.Cmd
	group procGroup

	// sendQ carries lines to the writer goroutine. Bounded, and Send never
	// blocks on it: a pipe write can stall indefinitely if the child has stopped
	// reading, and the caller here is the Bubble Tea update loop — a stalled
	// write would freeze the whole room, including the key that cancels it.
	sendQ chan []byte

	// clock splits each turn this process takes. One clock for the process, not
	// one per turn: the launch it records is spent by the FIRST turn and by no
	// other, which is the difference between a persistent seat and a fresh spawn
	// stated as a measurement.
	clock *clock

	mu     sync.Mutex
	killed bool
	closed bool

	done chan struct{}
}

// ErrSessionClosed is returned by Send when the process is gone. The turn then
// fails visibly rather than being silently swallowed by a dead pipe.
var ErrSessionClosed = errors.New("runner: the vendor process is no longer accepting input")

// ErrSendBacklog is returned when the outbound queue is full.
//
// A vendor that has stopped reading its stdin is stuck, and queueing more turns
// at it would hide that behind a growing buffer. Refusing surfaces it in the
// column, which is the same bargain the bounded event channel makes in the
// other direction.
var ErrSendBacklog = errors.New("runner: the vendor is not reading its input")

// sendQueue is how many turns may be in flight to one child. Small on purpose:
// council dispatches one turn at a time per seat, so anything above one is
// already an anomaly and the ceiling only exists to keep Send non-blocking.
const sendQueue = 8

// StartSession launches a spec as a long-lived child and streams its stdout
// into out as events.
//
// The context here is the ROOM's, never a turn's. A turn that is cancelled must
// not take the process with it — that is the whole point of keeping it — so
// cancellation of a turn is an interrupt sent through Send, and only quitting
// the room cancels this context.
//
// Unlike Start, no prompt is placed anywhere at launch: the spec carries flags
// only, and every turn arrives later as a line on stdin. The shim refusal that
// guards Start therefore has nothing to guard here, because there is no path by
// which prompt text could reach argv.
func StartSession(ctx context.Context, spec Spec, out chan<- Event, parse ParseFunc) (*Session, error) {
	// A one-way parser adapted to the two-way shape below. It never replies,
	// which is the whole difference between this path and StartRPCSession, and
	// stating it as a nil return rather than as a second code path is what keeps
	// the stream-json seats running through byte-identical machinery.
	return startSession(ctx, spec, out, func(line []byte) ([]Event, [][]byte) {
		if ev, ok := parse(line); ok {
			return []Event{ev}, nil
		}
		return nil, nil
	}, nil)
}

// StartRPCSession launches a spec as a long-lived child that speaks a
// request/response protocol on stdin/stdout.
//
// Identical to StartSession in every process guarantee — same job object, same
// bounded channel, same turn clock, no prompt anywhere in argv — and different
// in exactly one way: the parser may answer. See Protocol for why that
// difference cannot be papered over, and design.md §9.36 for the measurement
// that forced it.
func StartRPCSession(ctx context.Context, spec Spec, out chan<- Event, proto Protocol) (*Session, error) {
	return startSession(ctx, spec, out, proto.Inbound, proto.Opening())
}

// startSession is the shared body. handle sees every line and may return lines
// to write back; opening is written once the child is up, before any turn.
func startSession(ctx context.Context, spec Spec, out chan<- Event, handle handlerFunc, opening [][]byte) (*Session, error) {
	ck := newClock(spec.Vendor, spec.Race)
	cmd := exec.Command(spec.Binary, spec.Args...)
	cmd.Dir = spec.Dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	group := newProcGroup()
	group.prepare(cmd)

	if err := cmd.Start(); err != nil {
		group.close()
		return nil, err
	}
	// Same small race as Start documents: a grandchild spawned in the
	// microseconds before assignment escapes the job. Non-fatal — the tree-kill
	// guarantee weakens, killing the direct child does not.
	_ = group.attach(cmd)
	// Held unspent until the first turn is sent. The process is up, but nobody
	// is waiting on it yet, and charging its idle time to a turn that has not
	// been typed would be the clock inventing a wait.
	ck.launched()

	s := &Session{
		cmd:   cmd,
		group: group,
		sendQ: make(chan []byte, sendQueue),
		clock: ck,
		done:  make(chan struct{}),
	}

	// One goroutine owns stdin. Writes are serialised here rather than under the
	// mutex so that a blocked pipe stalls only this goroutine.
	go func() {
		defer stdin.Close()
		for {
			select {
			case line, ok := <-s.sendQ:
				if !ok {
					return
				}
				if _, err := stdin.Write(line); err != nil {
					// The child stopped reading. Nothing useful can be said to it
					// any more; the terminal event from its exit is what the
					// column will render.
					s.markClosed()
					return
				}
			case <-s.done:
				return
			}
		}
	}()

	// The handshake goes out before the first turn and WITHOUT touching the
	// clock: queued through write rather than Send, so the room's own opening
	// cannot be billed to a turn nobody has typed yet.
	//
	// A failure here is FATAL to the session rather than shrugged off. An opening
	// that did not go out leaves a process nobody has spoken to, which will
	// answer nothing forever — and reporting that as a launch error puts it in
	// the column as a dispatch failure instead of as a turn that never ends.
	for _, line := range opening {
		if err := s.write(line); err != nil {
			s.Kill()
			return nil, err
		}
	}

	errTail := &ringBuffer{limit: stderrTail}
	var wg sync.WaitGroup
	wg.Add(2)
	// A reply produced by the parser goes back down the SAME queue a turn uses,
	// and through write rather than Send: an answer to a question the vendor
	// asked mid-turn belongs to the turn already in progress, and starting a new
	// one on it would time the room's own keystroke as a fresh wait.
	//
	// A failure is REPORTED, and that is the difference between a visible fault
	// and a hang. write is non-blocking by construction — a vendor that stopped
	// reading its stdin must not be able to stall the room's input handling — so
	// its failure mode is a refusal, and the lines it refuses are the ones a
	// blocked vendor is waiting on: a permission answer, a handshake step. Dropped
	// silently, each of those is a column that never finishes.
	go func() {
		defer wg.Done()
		pumpStdout(stdout, spec.Vendor, out, handle, ck, func(lines [][]byte) {
			for _, l := range lines {
				err := s.write(l)
				if err == nil {
					continue
				}
				select {
				case out <- Event{
					Vendor: spec.Vendor, Kind: KindError, EndsTurn: true, Err: err,
					Note: "this seat could not be answered: " + err.Error(),
				}:
				case <-ctx.Done():
				}
				return
			}
		})
	}()
	go func() { defer wg.Done(); errTail.consume(stderr) }()

	// The lifecycle goroutine, identical in shape to Start's: drain the readers,
	// reap the process, emit exactly one terminal event. What differs is when it
	// runs — at room teardown or an unexpected death, not at the end of a turn.
	go func() {
		defer close(s.done)
		wg.Wait()
		waitErr := cmd.Wait()
		// A turn still open here died with the process. It is recorded rather
		// than dropped: how long the seat waited before it fell over is the same
		// question this clock answers for a turn that finished.
		ck.end(time.Now())

		ev := Event{Vendor: spec.Vendor, Kind: KindDone}
		if waitErr != nil {
			ev.Kind = KindError
			ev.Err = waitErr
			ev.Note, ev.Failure = failureNote(waitErr, errTail.String())
		}
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			ev.ExitCode = code
		}
		if s.wasKilled() {
			ev.Kind = KindDone
			ev.Err = nil
			ev.Note = ""
			ev.Failure = FailureUnclassified
		}
		s.markClosed()
		select {
		case out <- ev:
		case <-ctx.Done():
		}
		s.group.close()
	}()

	go func() {
		select {
		case <-ctx.Done():
			s.Kill()
		case <-s.done:
		}
	}()

	return s, nil
}

// Send queues one line for the child's stdin, appending the terminator.
//
// Never blocks. A full queue is reported rather than waited on, because the
// caller is the UI loop and a vendor that has stopped reading must not be able
// to take the room's input handling down with it.
func (s *Session) Send(line []byte) error {
	if err := s.write(line); err != nil {
		return err
	}
	// The turn's clock starts where the room let go of it, not where the writer
	// goroutine gets to it — the queue is a detail of this type, and time spent
	// in it is still time the user is waiting. A write that lands mid-turn (a
	// gate decision, an interrupt) finds a turn already open and leaves it
	// alone; see clock.begin.
	s.clock.begin(time.Now())
	return nil
}

// SendTurn hands one turn over, as however many lines the protocol makes of it.
//
// ZERO lines is legal and is the case this method exists for. An RPC protocol
// may TAKE a turn it cannot yet encode — cursor-agent's ACP server has no
// `sessionId` to put in a `session/prompt` until it has answered `session/new`
// — and the turn clock still has to start, because the person who pressed enter
// is waiting from that moment whether or not a byte has moved. Routing that
// through Send with an empty line would put a blank line on the vendor's stdin;
// routing it through write would lose the clock.
func (s *Session) SendTurn(lines [][]byte) error {
	s.clock.begin(time.Now())
	for _, line := range lines {
		if err := s.write(line); err != nil {
			return err
		}
	}
	return nil
}

// SendAside writes lines that are NOT a turn: an interrupt, a gate decision, a
// protocol reply. The clock is left alone, so an answer typed mid-turn stays
// inside the turn it answers.
func (s *Session) SendAside(lines [][]byte) error {
	for _, line := range lines {
		if err := s.write(line); err != nil {
			return err
		}
	}
	return nil
}

// write queues one line without touching the turn clock.
func (s *Session) write(line []byte) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return ErrSessionClosed
	}

	// Copied before queueing: the caller owns the slice it passed and may reuse
	// it, and the write happens on another goroutine at an unknown later moment.
	buf := make([]byte, 0, len(line)+1)
	buf = append(buf, line...)
	buf = append(buf, '\n')

	select {
	case s.sendQ <- buf:
		return nil
	default:
		return ErrSendBacklog
	}
}

// Kill terminates the child and everything it spawned. Same job-object teardown
// as a spawn-per-turn child: quitting the room must never leave an agent
// running, holding a session and spending quota, with nothing on screen to say
// so.
func (s *Session) Kill() {
	s.mu.Lock()
	s.killed = true
	s.closed = true
	s.mu.Unlock()
	s.group.kill()
}

// Alive reports whether the process is still able to take a turn.
func (s *Session) Alive() bool {
	select {
	case <-s.done:
		return false
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// Done closes once the child has exited and its terminal event has been sent.
func (s *Session) Done() <-chan struct{} { return s.done }

func (s *Session) markClosed() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
}

func (s *Session) wasKilled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.killed
}
