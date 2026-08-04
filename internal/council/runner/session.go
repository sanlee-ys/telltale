package runner

import (
	"context"
	"errors"
	"os/exec"
	"sync"
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

	s := &Session{
		cmd:   cmd,
		group: group,
		sendQ: make(chan []byte, sendQueue),
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

	errTail := &ringBuffer{limit: stderrTail}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pumpStdout(stdout, spec.Vendor, out, parse) }()
	go func() { defer wg.Done(); errTail.consume(stderr) }()

	// The lifecycle goroutine, identical in shape to Start's: drain the readers,
	// reap the process, emit exactly one terminal event. What differs is when it
	// runs — at room teardown or an unexpected death, not at the end of a turn.
	go func() {
		defer close(s.done)
		wg.Wait()
		waitErr := cmd.Wait()

		ev := Event{Vendor: spec.Vendor, Kind: KindDone}
		if waitErr != nil {
			ev.Kind = KindError
			ev.Err = waitErr
			ev.Note = failureNote(waitErr, errTail.String())
		}
		if code := cmd.ProcessState.ExitCode(); code >= 0 {
			ev.ExitCode = code
		}
		if s.wasKilled() {
			ev.Kind = KindDone
			ev.Err = nil
			ev.Note = ""
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
