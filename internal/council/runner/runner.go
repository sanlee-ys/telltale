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
	// KindActivity is news about what the vendor is DOING: a tool call, a shell
	// command, a file edit — and, later in the same stream, how that call
	// turned out. Distinct from KindText because it is not the vendor's opinion
	// and must never be concatenated into its prose — a column that ran three
	// commands and then answered should show both, and show which is which.
	//
	// One kind rather than two, deliberately. An announcement and its result
	// are the same fact at two moments, the consumer correlates them by id, and
	// a second kind would only give the two paths somewhere to diverge.
	KindActivity
	// KindSession carries the vendor's own session id, which is what makes the
	// next turn a resume rather than a re-send.
	KindSession
	// KindMeta carries a reported cost. Only ever set from a number the vendor
	// stated; council never derives one.
	KindMeta
	// KindGate is a vendor asking permission for one tool call and BLOCKING
	// until it is answered. Gate is set.
	//
	// Only a persistent session can carry one: the request arrives on the
	// process's stdout and its answer goes back on the same process's stdin, so
	// a spawn-per-turn child — whose stdin was written and closed before the
	// first token arrived — has no channel to answer on. That is not a Claude
	// limitation, it is the shape of the batch CLIs, and it is why the gate is
	// Claude-only.
	KindGate
	// KindDone: the process exited. ExitCode is set.
	KindDone
	// KindError: the turn failed. Err and Note are set.
	KindError
)

// ActStatus is what is known about the outcome of one tool call.
//
// Four values, and Unknown is the one that earns the type. A vendor that
// reports a step FINISHED without saying whether it worked is a different fact
// from a vendor that reports success, and the two must not render alike — the
// same rule that keeps "no data" and "zero" apart on every gauge in this
// product (design.md §4a.1). Antigravity is exactly that case: its steps flip
// ACTIVE then DONE and no captured line has ever carried a success signal.
type ActStatus uint8

const (
	// ActPending: announced, not resolved. The call may still be running, or
	// the vendor may simply never say. It renders as the bare trace entry,
	// because that is all that is known.
	ActPending ActStatus = iota
	// ActOK: the vendor reported the call succeeded.
	ActOK
	// ActFailed: the vendor reported the call failed.
	ActFailed
	// ActUnknown: the vendor reported the call ENDED and said nothing about
	// whether it worked. Neither a success nor a failure, and it must not be
	// rendered as either.
	ActUnknown
	// ActDenied: the USER refused it at the gate. The call never ran.
	//
	// A fifth value rather than reusing ActFailed, and the distinction is the
	// whole point of the gate. The vendor reports a denial as an is_error
	// tool_result carrying council's own refusal text back — so read off the
	// stream alone it is indistinguishable from a tool that broke, and the
	// trace would say the command failed when what happened is that it was not
	// allowed to run. This is the only outcome council knows FIRST HAND: it is
	// the record of a keystroke, not a reading of a vendor's words.
	ActDenied
)

// ActCall is one tool call, as a vendor reported it.
//
// The same type carries an announcement and its later result. An announcement
// sets Text and leaves Outcome at ActPending; a result sets Outcome and, where
// the vendor offered one, Detail. Both may set Text, so a vendor that reports
// only completions still names what it did.
type ActCall struct {
	// ID is the vendor's own id for this call: Claude's tool_use_id, codex's
	// item id, agy's step index. It is what the consumer correlates on, and it
	// is never rendered.
	//
	// Empty is legal and means the call can never be resolved — it stays
	// pending forever, which is the honest outcome rather than a guess. No
	// adapter here is currently in that position.
	ID string
	// Text is what the vendor did, already shortened for a narrow column:
	// "Bash: go test ./...".
	Text string
	// Outcome is what the vendor said about how it went.
	Outcome ActStatus
	// Detail is the vendor's OWN words about a failure, first line only.
	// Never composed here: a diagnosis council wrote itself would be
	// indistinguishable on screen from one the vendor reported.
	Detail string
}

// Gate is one tool call a vendor is BLOCKED on, waiting to be told yes or no.
//
// Captured live on 2026-08-04 against Claude Code 2.1.220, driving the process
// with --input-format stream-json. The request, whole, minus nothing:
//
//	{"type":"control_request","request_id":"179ce36e-c5d1-4b95-a761-ec7aa1fd5494","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"...\\ping.txt","content":"PONG"},"description":"ping.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01MagQh7Ep8kzC1edrDr17jL"}}
//
// That it BLOCKS is the part that had to be measured rather than read: the
// answer was withheld for twenty seconds and nothing else arrived on stdout in
// that window, then the tool_result landed 0.25s after it was sent.
type Gate struct {
	// RequestID is the vendor's own id for this request. The answer carries it
	// back; nothing else identifies which of several pending calls was decided.
	RequestID string
	// ToolUseID is the id of the tool_use block this request is about, which is
	// the SAME id the activity trace already keys on. That correspondence is
	// what lets an approval card and the trace entry it approves be one thing on
	// screen rather than two.
	//
	// Verified: the captured request's tool_use_id matched the assistant
	// message's tool_use id exactly.
	ToolUseID string
	// Tool is the tool name as the vendor named it ("Write", "Bash").
	Tool string
	// Text is the tool and its argument line, shortened for a narrow column and
	// formatted like a trace entry: "Write: ...\\ping.txt". Composed by the
	// adapter from the vendor's own fields; nothing here is invented.
	Text string
	// Input is the tool's arguments exactly as the vendor sent them, held only
	// to be handed straight back on an approval — the protocol requires the
	// input to be echoed in the answer.
	//
	// NEVER rendered, and deliberately not carried onto State. It is the whole
	// argument blob, which for a Write is the entire file content; the card
	// shows Text. Keeping it off the renderer's side of the boundary means
	// there is no way for it to reach the screen by accident.
	Input map[string]any
}

// Event is one thing that happened to one vendor.
type Event struct {
	Vendor    model.VendorID
	Kind      EventKind
	Text      string
	SessionID string
	// Gate is set on KindGate and nowhere else.
	Gate *Gate
	// EndsTurn marks the vendor's own end-of-turn line.
	//
	// It exists because a persistent process has no process exit to end a turn
	// with. A spawn-per-turn child says "the turn is over" by dying, which the
	// runner reports as KindDone; a process that will take another turn says it
	// with a line in the stream, and only the adapter can recognise which line
	// that is. Set on Claude's `result` — one per turn, verified across two
	// turns of one process.
	EndsTurn bool
	// Acts carries the tool-call news of a KindActivity: one entry per call
	// announced or resolved on this line.
	//
	// A SLICE rather than a string, because one assistant message really can
	// carry a parallel batch of calls and collapsing them onto one line would
	// under-report the work. It replaced a newline-joined Text that the
	// consumer split back apart — once each call has to carry its own id, that
	// split-and-zip becomes positional correlation across a redaction
	// boundary, which is exactly the kind of thing that goes wrong silently.
	Acts []ActCall
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
// It classifies the failures a user can actually act on and otherwise quotes
// the vendor. Guessing beyond that would be inventing a diagnosis, so a case is
// only added here once a real run has produced the text it matches on — every
// pattern below was copied off a captured stderr rather than imagined.
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
	case strings.Contains(low, "workspace trust"),
		strings.Contains(low, "do you trust the contents of this directory"):
		// Captured 2026-08-04 from cursor-agent, which refuses a print-mode turn
		// in a directory it has not been told to trust and exits 1 before any
		// model call. It offers `--trust` as the fix; council does not pass it,
		// in either posture, because accepting a trust prompt is a consent
		// decision and this tool does not make those on someone's behalf. So the
		// card says what the user can do instead, which is the same thing in
		// their own terminal where they can see what they are agreeing to.
		return "this vendor has not been told to trust this workspace — it refuses to run " +
			"until you approve the directory in your own terminal; council will not accept " +
			"that prompt for you"
	case strings.Contains(low, "sandbox requires macos or linux"),
		strings.Contains(low, "sandbox mode is enabled but not available"):
		// Also captured 2026-08-04. Council itself no longer asks for a sandbox
		// on Windows — the adapter drops the flag there — so reaching this means
		// the user's OWN vendor config enables it, and every turn will die the
		// same way until that config changes. Left classified rather than
		// removed as unreachable for exactly that reason.
		return "this vendor's own config asks for a sandbox its help says needs macOS or " +
			"Linux, and it refuses to run without one — disable it in the vendor's config"
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
