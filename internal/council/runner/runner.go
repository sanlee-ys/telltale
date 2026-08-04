// Package runner spawns vendor CLIs and turns their stdout into events.
//
// Three rules shape everything here, and all three are safety rather than
// style:
//
//   - A prompt is arbitrary text and never crosses a shell. Specs carry argv as
//     a slice and, where the vendor supports it, the prompt on stdin.
//   - A child that spawns its own children must die with them. On Windows that
//     means a Job Object, because the vendor we most need to kill (Codex) is
//     reached through an npm shim and is therefore a grandchild.
//   - Nothing is dropped silently. A full event channel blocks the reader,
//     which fills the OS pipe, which stalls the vendor — backpressure the user
//     can see as a slow column, rather than output that quietly disappears.
package runner

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sanlee-ys/telltale/internal/model"
)

// EventKind is what a stream event says.
type EventKind uint8

const (
	// KindText is incremental output to append to a column.
	KindText EventKind = iota
	// KindActivity is what the vendor is DOING: a tool call, a shell command,
	// a file edit. Distinct from KindText because it is not the vendor's
	// opinion and must never be concatenated into its prose — a column that ran
	// three commands and then answered should show both, and show which is
	// which.
	KindActivity
	// KindSession carries the vendor's own session id, which is what makes the
	// next turn a resume rather than a re-send.
	KindSession
	// KindMeta carries a reported cost. Only ever set from a number the vendor
	// stated; council never derives one.
	KindMeta
	// KindDone: the process exited. ExitCode is set.
	KindDone
	// KindError: the turn failed. Err and Note are set.
	KindError
)

// Event is one thing that happened to one vendor.
type Event struct {
	Vendor    model.VendorID
	Kind      EventKind
	Text      string
	SessionID string
	// CostUSD is a pointer so "the vendor reported zero" and "the vendor
	// reported nothing" stay distinguishable — the same rule the HUD's schema
	// follows (design.md §4a.1).
	CostUSD  *float64
	ExitCode int
	Err      error
	// Note is a human-readable reason, already assembled, for a column card.
	Note string
}

// Spec is one invocation, fully resolved. Nothing in it is interpolated into a
// string: Binary and Args go to exec.Command as separate arguments.
type Spec struct {
	Vendor model.VendorID
	Binary string
	Args   []string
	// StdinPrompt is written to the child's stdin, which is then closed. Empty
	// means the prompt is already in Args (only safe for a native binary).
	StdinPrompt string
	Dir         string
}

// ParseFunc converts one line of a vendor's stdout into an event. Returning
// false drops the line, which is how a parser ignores event types it does not
// model without failing the turn.
type ParseFunc func(line []byte) (Event, bool)

// ErrShellShimWithArgvPrompt is the refusal that keeps prompt text away from
// cmd.exe.
//
// Go's os/exec runs .cmd and .bat through cmd.exe, whose argument parsing
// cannot be safely quoted for arbitrary text — a prompt containing a quote or
// an ampersand would either break or, worse, execute as something else. A
// vendor that resolves to a shim must therefore take its prompt on stdin.
var ErrShellShimWithArgvPrompt = errors.New(
	"runner: refusing to pass prompt text as an argument to a shell shim")

// maxLine caps one stdout line. Vendor stream-json lines carry whole message
// deltas and can be long; bufio.Scanner's 64K default would silently truncate
// one, so the reader uses ReadBytes and this is the explicit ceiling.
const maxLine = 8 << 20

// stderrTail is how much of a failed child's stderr is kept for its card.
const stderrTail = 4 << 10

// Handle is a running child.
type Handle struct {
	cmd   *exec.Cmd
	group procGroup

	mu     sync.Mutex
	killed bool

	done chan struct{}
}

// Start launches a spec and streams its stdout into out as events.
//
// out is shared by every vendor in a turn and is expected to be bounded; a slow
// consumer therefore stalls the vendors rather than losing their output.
// Start returns as soon as the child is running: parsing happens on its own
// goroutine, and the caller learns about completion through a KindDone or
// KindError event.
func Start(ctx context.Context, spec Spec, out chan<- Event, parse ParseFunc) (*Handle, error) {
	if spec.StdinPrompt == "" && isShim(spec.Binary) {
		return nil, ErrShellShimWithArgvPrompt
	}

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
	var stdin io.WriteCloser
	if spec.StdinPrompt != "" {
		if stdin, err = cmd.StdinPipe(); err != nil {
			return nil, err
		}
	}

	group := newProcGroup()
	group.prepare(cmd)

	if err := cmd.Start(); err != nil {
		group.close()
		return nil, err
	}
	// Assigned immediately after Start. There is a small race here: a child
	// that spawns a grandchild in the microseconds before assignment escapes
	// the group. Closing it properly needs the child created suspended and
	// resumed after assignment, and Go's os/exec does not expose the thread
	// handle that would take. Documented rather than hidden.
	if err := group.attach(cmd); err != nil {
		// Non-fatal: the turn still runs, it is only the tree-kill guarantee
		// that weakens, and killing the direct child remains possible.
		_ = err
	}

	h := &Handle{cmd: cmd, group: group, done: make(chan struct{})}

	if stdin != nil {
		// Written on its own goroutine and closed straight after: a vendor that
		// never reads stdin would otherwise deadlock a synchronous write once
		// the pipe buffer filled.
		go func() {
			_, _ = io.WriteString(stdin, spec.StdinPrompt)
			_ = stdin.Close()
		}()
	}

	errTail := &ringBuffer{limit: stderrTail}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); pumpStdout(stdout, spec.Vendor, out, parse) }()
	go func() { defer wg.Done(); errTail.consume(stderr) }()

	// One goroutine owns the lifecycle: it waits for the readers to drain, then
	// for the process, then emits exactly one terminal event. Emitting on exit
	// before the readers drain would race the last lines of output off screen.
	go func() {
		defer close(h.done)
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
		if h.wasKilled() {
			// A process we killed did not fail; the user cancelled it. Saying
			// "exit 1" here would blame the vendor for the user's keystroke.
			ev.Kind = KindDone
			ev.Err = nil
			ev.Note = ""
		}
		select {
		case out <- ev:
		case <-ctx.Done():
		}
		h.group.close()
	}()

	// Cancellation kills the whole tree, not just the direct child.
	// exec.CommandContext would kill only the latter, which on Windows leaves
	// the real vendor process running behind its shim.
	go func() {
		select {
		case <-ctx.Done():
			h.Kill()
		case <-h.done:
		}
	}()

	return h, nil
}

// Kill terminates the child and everything it spawned.
func (h *Handle) Kill() {
	h.mu.Lock()
	h.killed = true
	h.mu.Unlock()
	h.group.kill()
}

func (h *Handle) wasKilled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.killed
}

// Done closes once the child has exited and its terminal event has been sent.
func (h *Handle) Done() <-chan struct{} { return h.done }

// pumpStdout reads whole lines and hands each to the vendor's parser.
//
// ReadBytes rather than bufio.Scanner: Scanner caps a token at 64K by default
// and reports a line longer than that as an error, which for a stream of JSON
// message deltas means silently losing exactly the largest replies.
func pumpStdout(r io.Reader, vendor model.VendorID, out chan<- Event, parse ParseFunc) {
	br := bufio.NewReaderSize(r, 64<<10)
	var acc []byte
	for {
		chunk, err := br.ReadBytes('\n')
		if len(chunk) > 0 {
			acc = append(acc, chunk...)
			if acc[len(acc)-1] == '\n' || len(acc) >= maxLine {
				line := trimEOL(acc)
				acc = nil
				if ev, ok := parse(line); ok {
					ev.Vendor = vendor
					out <- ev
				}
			}
		}
		if err != nil {
			// A final line with no trailing newline is still a line. Dropping it
			// would lose the last thing a vendor said on a clean exit.
			if len(acc) > 0 {
				if ev, ok := parse(trimEOL(acc)); ok {
					ev.Vendor = vendor
					out <- ev
				}
			}
			return
		}
	}
}

func trimEOL(b []byte) []byte {
	b = trimSuffixByte(b, '\n')
	return trimSuffixByte(b, '\r')
}

func trimSuffixByte(b []byte, c byte) []byte {
	if len(b) > 0 && b[len(b)-1] == c {
		return b[:len(b)-1]
	}
	return b
}

// failureNote turns an exit error plus stderr into one line for a column card.
//
// It classifies the two failures a user can actually act on — not signed in,
// and binary missing — and otherwise quotes the vendor. Guessing beyond that
// would be inventing a diagnosis.
func failureNote(err error, stderrText string) string {
	s := strings.TrimSpace(stderrText)
	low := strings.ToLower(s)
	switch {
	case strings.Contains(low, "not logged in"),
		strings.Contains(low, "not signed in"),
		strings.Contains(low, "unauthorized"),
		strings.Contains(low, "authentication"),
		strings.Contains(low, "please run") && strings.Contains(low, "login"):
		return "not signed in — authenticate this vendor in your own terminal, then dispatch again"
	case strings.Contains(low, "command not found"),
		strings.Contains(low, "is not recognized"):
		return "the vendor binary vanished between detection and dispatch"
	}
	if s == "" {
		return err.Error()
	}
	return err.Error() + ": " + firstLine(s)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// isShim reports whether a path will be run through cmd.exe.
func isShim(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	default:
		return false
	}
}

// ringBuffer keeps the last N bytes of a stream.
//
// Bounded on purpose: a vendor that fails in a loop can produce unbounded
// stderr, and the only part of it a card can show is the end.
type ringBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (r *ringBuffer) consume(src io.Reader) {
	b := make([]byte, 4096)
	for {
		n, err := src.Read(b)
		if n > 0 {
			r.mu.Lock()
			r.buf = append(r.buf, b[:n]...)
			if len(r.buf) > r.limit {
				r.buf = r.buf[len(r.buf)-r.limit:]
			}
			r.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (r *ringBuffer) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(r.buf)
}
