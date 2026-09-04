// Package probe drives one installed vendor seat through the live shape the
// room uses — the handshake, one turn, and the stop — and writes what it
// measured under ~/.telltale/probe.
//
// # Why this may run a vendor, when almost nothing else may
//
// `internal/council` never runs a vendor to find out whether it works, and its
// own doc says why: a probe turn costs real quota, and "is it authenticated?"
// is a question the first real dispatch answers for free (ADR-008 §6).
// `internal/doctor` widened that boundary once, and drew the new line at COST
// AND SIDE EFFECT rather than at "running a vendor is forbidden": it spawns
// `<binary> --version`, which parses argv, prints a string and exits, with no
// model and no billing anywhere in it (design.md §9.42).
//
// This mode is on the far side of that line and says so on every surface it
// has. It SPENDS a turn on the operator's own account, one per seat. So it is
// its own foreground subcommand nobody else calls, it names the cost before it
// starts, it refuses to run non-interactively without `--yes`, and no gauge,
// no room and no scheduled path reaches it. The operator asks for it, watches
// it, and pays for it.
//
// What the spend buys is the one thing this repository could not otherwise
// have. Every claim about a live vendor shape here was measured by hand and
// written into prose, and STATE.md carries a list of the ones nobody has paid.
// A reader of `telltale doctor` today is told that telltale's survey is older
// than the binary on this disk, and nothing on the machine can do anything
// about it. After a probe, `doctor` can say the seat was driven HERE, at THIS
// build, on THIS day — a machine-paid fact rather than a maintainer's note.
//
// # What it drives, and what it deliberately does not
//
// Three checks, in order, stopping at the first failure on that seat:
//
//   - handshake — the process comes up and the seat names a session.
//   - turn — a brief of ONE WORD goes down the same pipe, the reply arrives,
//     and the turn ends the way the adapter says a turn ends.
//   - stop — the seat's own closing lines go out, stdin closes, and the
//     process exits inside the grace the adapter states.
//
// Those are the three things every dispatch in the room depends on, and they
// are the three the repository already calls owed: STATE.md's crew checklist
// and design.md §9.57 both name a handshake, a turn and a timed stop per seat.
// Nothing else is driven. No write is asked for, no approval flow is
// exercised, no second turn tests a resume — each of those is a separate item
// on that checklist, each needs a person to read what came back, and a probe
// that quietly did them would be spending more of the operator's money than the
// sentence on screen said it would.
//
// # The seat runs in a throwaway directory
//
// Every probe points its seat at a fresh empty temporary directory, removed
// when the seat is done. Three of the room's seats act unasked on a write and
// the room says so (docs/council.md), so the containment that actually holds is
// the directory — and a preflight that pointed a live agent at the operator's
// own repository to ask it one word would be spending that containment for
// nothing. The posture asked for is the read posture, which is the most
// read-only invocation each vendor honours; the directory is what makes the
// claim safe rather than the flag.
package probe

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The three check names, in the order they run. One lower-case word each, the
// vocabulary `doctor`'s own rows use.
const (
	CheckHandshake = "handshake"
	CheckTurn      = "turn"
	CheckStop      = "stop"
)

// Brief is the one word every probe sends, and it is a constant rather than a
// flag.
//
// A flag here would be a way to spend a real turn through a mode that promises
// a trivial one — the operator was told "one word" and the sentence has to stay
// true. One word is also what makes the turn check honest about what it
// measures: that the seat took a brief and ended a turn, not that a model
// answered anything well.
const Brief = "ping"

// defaultGrace bounds the stop for a seat whose adapter states no grace of its
// own.
//
// Only the stream-json Claude seat is in that position today, and it is there
// because a closed stdin was MEASURED sufficient for it (vendors.GracefulStop's
// doc: Claude Code exits 0 on a closed stdin), so the adapter has nothing to
// say. A bound is still owed: the check is "the process exits", and a check
// that can wait forever is not a check. This number is this package's own and
// says so — it is not a claim about the vendor, and nothing renders it as one.
const defaultGrace = 5 * time.Second

// eventBuffer is how many events may sit between the runner and this drive.
//
// Generous, because a one-word turn on a streaming seat still arrives as
// hundreds of text deltas — the grok handshake capture of 2026-09-04 counted
// 853 chunks on one brief — and a full channel BLOCKS the runner's reader
// goroutine. That would not lose events, it would slow the drive and charge the
// delay to a vendor, which is the one thing a measurement must not do.
const eventBuffer = 1024

// session is the slice of runner.Session this drive uses.
//
// An interface for internal/council's stated reason and one of its own: the
// tests script a handshake that never lands, a turn that never ends and a
// process that outlives its grace, and every one of those is a PROCESS
// behaviour rather than a vendor's words. Scripting them over a real child
// would mean shipping three fake vendor binaries; scripting them here means the
// suite spawns nothing at all.
type session interface {
	SendTurn(lines [][]byte) error
	SendAside(lines [][]byte) error
	CloseInput()
	Kill()
	Done() <-chan struct{}
}

// startSession and startRPCSession are this package's vendor spawns, behind
// vars for the reason internal/council puts its four behind vars: the property
// under test is "the suite started no vendor", and the cheapest honest way to
// assert it is to make the real call site countable. main_test.go wraps both
// fail-closed. Production never replaces them.
var startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
	parse runner.ParseFunc) (session, error) {
	return runner.StartSession(ctx, spec, out, parse)
}

var startRPCSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
	proto runner.Protocol) (session, error) {
	return runner.StartRPCSession(ctx, spec, out, proto)
}

// Seat is one vendor the probe can drive, flattened into what the drive needs.
//
// It is filled by internal/council, which is where detection, the binary
// resolution and the adapter registry already live — the same seam
// `doctor.Seat` is filled through, and for the same reason. Two copies of
// "where does cursor-agent live" is the agreement that silently stops holding.
type Seat struct {
	Vendor model.VendorID
	Label  string
	// Binary is the resolved path detection will actually run.
	Binary string
	// Adapter is the registered seat. A seat with none cannot be driven and is
	// reported as such rather than skipped silently.
	Adapter vendors.Vendor
	// VersionArgs is the argv after Binary that asks this vendor its version —
	// per-seat because of the Cursor bundle (council's versionArgs).
	VersionArgs []string
}

// Options is what the caller sets for a run.
type Options struct {
	// Timeout bounds each check on its own, never the run: a wedged seat costs
	// its own deadline and not the report, which is `doctor --timeout`'s
	// bargain one mode out.
	Timeout time.Duration
	// TelltaleVersion is the build doing the driving, written into the record.
	TelltaleVersion string
	// Now is injected so a test can pin the stamp. Nil takes time.Now.
	Now func() time.Time
	// Version is the version probe. Nil takes doctor.ExecProbe, which is the
	// one place in this binary that asks a vendor its own version.
	Version doctor.Probe
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// Check is one of the three checks and what came back.
type Check struct {
	Name string
	// Status carries doctor's three states and no fourth. Reused rather than
	// redefined: this report is read beside that one, and two vocabularies for
	// one distinction is how a reader ends up believing a `not run` here means
	// something other than a `not checked` there.
	Status doctor.Status
	// Took is measured, and it is zero on a check that did not run — which is
	// why nothing renders it without its own status beside it.
	Took time.Duration
	// Detail is the vendor's own first line of stderr, or the sentence the
	// failing event carried. It is printed in the operator's terminal and it is
	// NEVER written to the record; see file.go for the argument.
	Detail string
}

// Result is one seat's whole probe.
type Result struct {
	Vendor model.VendorID
	Label  string
	// Version is what the binary printed, unchanged, or empty when this machine
	// printed none.
	Version  string
	Checks   []Check
	ProbedAt time.Time
	// Skipped says why this seat was never driven — no binary, no adapter, no
	// live shape. A skipped seat carries no checks and writes no file: an
	// absent file is what "nobody probed this here" already says, and a file
	// full of `not_run` would be the probe claiming a visit it did not make.
	Skipped string
}

// Drove reports that this seat was actually driven.
func (r Result) Drove() bool { return r.Skipped == "" && len(r.Checks) > 0 }

// Record flattens a Result into the file's shape. It is the one place that
// decides what reaches disk, and it drops the Detail on every branch.
func (r Result) Record(telltaleVersion string) Record {
	rec := Record{
		Vendor:          string(r.Vendor),
		Version:         r.Version,
		ProbedAt:        r.ProbedAt,
		TelltaleVersion: telltaleVersion,
	}
	for _, c := range r.Checks {
		cr := CheckRecord{Name: c.Name, Status: statusWord(c.Status)}
		if c.Status != doctor.NotChecked {
			ms := c.Took.Milliseconds()
			cr.Millis = &ms
		}
		rec.Checks = append(rec.Checks, cr)
	}
	return rec
}

func statusWord(s doctor.Status) string {
	switch s {
	case doctor.Passed:
		return StatusOK
	case doctor.Failed:
		return StatusFailed
	default:
		return StatusNotRun
	}
}

// Run drives every seat in order and returns one Result each. Seats are driven
// one at a time on purpose: two live agents answering at once would make the
// durations this mode exists to measure a reading of the machine's load rather
// than of the vendor.
func Run(ctx context.Context, seats []Seat, o Options) []Result {
	out := make([]Result, 0, len(seats))
	for _, s := range seats {
		out = append(out, RunSeat(ctx, s, o))
	}
	return out
}

// RunSeat drives one seat.
func RunSeat(ctx context.Context, s Seat, o Options) Result {
	res := Result{Vendor: s.Vendor, Label: s.Label, ProbedAt: o.now()}
	if s.Binary == "" {
		res.Skipped = "detection resolved no binary to drive on this machine"
		return res
	}
	if s.Adapter == nil {
		res.Skipped = "council has no adapter for this seat, so there is no shape to drive"
		return res
	}

	res.Version = readVersion(s, o)

	dir, err := os.MkdirTemp("", "telltale-probe-")
	if err != nil {
		res.Skipped = "no throwaway directory could be made to run the seat in: " + err.Error()
		return res
	}
	defer os.RemoveAll(dir)

	res.Checks = drive(ctx, s, dir, o)
	return res
}

// readVersion asks the binary its own version, through the same probe `doctor`
// uses. A version this machine did not print stays empty: the record's own
// field is omitted, and every surface says the version was not read rather than
// showing a blank where one goes.
func readVersion(s Seat, o Options) string {
	p := o.Version
	if p == nil {
		p = doctor.ExecProbe(o.Timeout)
	}
	r := p(s.Binary, s.VersionArgs)
	if r.Err != nil {
		return ""
	}
	return r.Out
}

// drive is the three checks over one live process.
func drive(ctx context.Context, s Seat, dir string, o Options) []Check {
	events := make(chan runner.Event, eventBuffer)
	// The drive's OWN context, cancelled on the way out. It is what guarantees
	// the process cannot outlive this function even on a path that returns
	// early: the runner kills its child when this is cancelled, on top of the
	// explicit Kill below.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	sess, wire, err := spawn(ctx, s, dir, events)
	if err != nil {
		return []Check{
			{Name: CheckHandshake, Status: doctor.Failed, Detail: err.Error()},
			{Name: CheckTurn, Status: doctor.NotChecked},
			{Name: CheckStop, Status: doctor.NotChecked},
		}
	}
	defer sess.Kill()

	started := time.Now()
	// The brief goes down the pipe BEFORE the handshake is scored, and that
	// ordering is forced by the vendors rather than chosen.
	//
	// A request/response seat names its session in the answer to the room's own
	// opening, so its id arrives whether or not a brief was sent. A stream-json
	// seat names nothing until it has something to answer: Claude Code's
	// `system` init line and Antigravity's `init` event both arrive as the
	// first line of the first turn's output. So a drive that waited for a
	// session before sending anything would hang on two of the four seats and
	// report a handshake failure that is an artefact of its own ordering.
	//
	// Handing the turn over first costs nothing on the other two: an ACP or
	// app-server protocol QUEUES a turn it cannot yet encode and flushes it the
	// moment the session opens, which is the case runner.Session.SendTurn
	// exists for.
	if err := sendBrief(sess, wire); err != nil {
		return []Check{
			{Name: CheckHandshake, Status: doctor.Failed,
				Detail: "the brief could not be written to the seat: " + err.Error()},
			{Name: CheckTurn, Status: doctor.NotChecked},
			{Name: CheckStop, Status: doctor.NotChecked},
		}
	}

	hs := waitForSession(ctx, events, started, o.Timeout)
	if hs.Status != doctor.Passed {
		return []Check{hs,
			{Name: CheckTurn, Status: doctor.NotChecked},
			{Name: CheckStop, Status: doctor.NotChecked},
		}
	}

	turn := waitForTurn(ctx, events, o.Timeout)
	if turn.Status != doctor.Passed {
		return []Check{hs, turn, {Name: CheckStop, Status: doctor.NotChecked}}
	}

	return []Check{hs, turn, stop(ctx, sess, wire, events)}
}

// spawn brings the seat's live process up, through the same two call shapes the
// room's own spawnSeat uses.
func spawn(ctx context.Context, s Seat, dir string, events chan runner.Event) (session, any, error) {
	if cv, ok := s.Adapter.(vendors.Conversational); ok {
		spec, proto, err := cv.Open(dir, s.Binary, "", vendors.PostureRead)
		if err != nil {
			return nil, nil, err
		}
		if proto == nil {
			return nil, nil, errors.New("this seat opened no protocol, so it has no live shape to drive")
		}
		sess, err := startRPCSession(ctx, spec, events, proto)
		if err != nil {
			return nil, nil, err
		}
		return sess, proto, nil
	}
	pv, ok := s.Adapter.(vendors.Persistent)
	if !ok {
		return nil, nil, errors.New("this seat is a batch program, so it has no live shape to drive")
	}
	// No hooks file. The room carries the operator's own hooks into a gated
	// seat; a probe has no room, no gate and nothing to carry, and handing over
	// a path here would put the operator's hook commands in front of a process
	// they did not open.
	spec, err := pv.Session(dir, s.Binary, "", vendors.PostureRead)
	if err != nil {
		return nil, nil, err
	}
	sess, err := startSession(ctx, spec, events, pv.ParseEvent)
	if err != nil {
		return nil, nil, err
	}
	return sess, pv, nil
}

// sendBrief encodes the one word for whichever shape this seat is and hands it
// over. Both shapes may legally return no lines — an RPC protocol takes a turn
// it cannot encode yet — so an empty result is passed through rather than
// treated as a refusal.
func sendBrief(sess session, wire any) error {
	var lines [][]byte
	var err error
	switch w := wire.(type) {
	case runner.Protocol:
		lines, err = w.Turn(Brief)
	case vendors.Persistent:
		var line []byte
		line, err = w.Turn(Brief)
		if line != nil {
			lines = [][]byte{line}
		}
	default:
		return errors.New("this seat has no wire to send a brief on")
	}
	if err != nil {
		return err
	}
	return sess.SendTurn(lines)
}

// waitForSession is the handshake check: the process is up and the seat names a
// session.
//
// A session id is the right thing to wait for rather than "the process did not
// die", and the difference is the whole value of this check. Every seat's next
// turn is a resume keyed on that id, so a process that comes up and never names
// one is a seat the room can dispatch to exactly once — a fault that looks like
// a working column until the second brief.
func waitForSession(ctx context.Context, events <-chan runner.Event, started time.Time,
	timeout time.Duration) Check {
	c := Check{Name: CheckHandshake}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev := <-events:
			switch {
			case ev.Kind == runner.KindSession && ev.SessionID != "":
				c.Status = doctor.Passed
				c.Took = time.Since(started)
				return c
			case ev.Kind == runner.KindError:
				c.Status = doctor.Failed
				c.Took = time.Since(started)
				c.Detail = failureWords(ev, "the seat failed before it named a session")
				return c
			case ev.Kind == runner.KindDone:
				c.Status = doctor.Failed
				c.Took = time.Since(started)
				c.Detail = "the process exited before it named a session"
				return c
			}
		case <-deadline.C:
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "no session was named within " + timeout.String()
			return c
		case <-ctx.Done():
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "the run was cancelled before the seat named a session"
			return c
		}
	}
}

// waitForTurn is the turn check: the reply arrives and the turn ends cleanly.
//
// "Cleanly" is the adapter's word, not this package's. A live seat ends a turn
// with a line its own parser marks `EndsTurn`, because a process that will take
// another brief has no exit to end it with; a seat that ended up as a batch
// program ends it by exiting 0. Both are accepted, and nothing else is: an exit
// with a non-zero code, or an error event, is a turn that did not end the way
// the room needs it to.
func waitForTurn(ctx context.Context, events <-chan runner.Event, timeout time.Duration) Check {
	c := Check{Name: CheckTurn}
	started := time.Now()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for {
		select {
		case ev := <-events:
			switch {
			case ev.EndsTurn && ev.Kind != runner.KindError:
				c.Status = doctor.Passed
				c.Took = time.Since(started)
				return c
			case ev.Kind == runner.KindError:
				c.Status = doctor.Failed
				c.Took = time.Since(started)
				c.Detail = failureWords(ev, "the turn failed")
				return c
			case ev.Kind == runner.KindDone:
				c.Took = time.Since(started)
				if ev.ExitCode == 0 {
					c.Status = doctor.Passed
					return c
				}
				c.Status = doctor.Failed
				c.Detail = "the process exited " + strconv.Itoa(ev.ExitCode) + " during the turn"
				return c
			}
		case <-deadline.C:
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "the turn did not end within " + timeout.String()
			return c
		case <-ctx.Done():
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "the run was cancelled during the turn"
			return c
		}
	}
}

// stop is the stop check, and it is the room's own teardown run once and timed:
// the seat's closing lines, then stdin closed, then a bounded wait
// (internal/council's stopProc).
//
// The kill still follows on every branch, exactly as it does in the room, and
// it is NOT what this check measures. §9.50 measured a closed stdin failing to
// end `codex app-server` — four runs exited in 1.5–3.3 s and one was alive at
// 15 s — which is why the room kills unconditionally and why a check that
// accepted the kill as an exit would report every seat passing.
func stop(ctx context.Context, sess session, wire any, events <-chan runner.Event) Check {
	c := Check{Name: CheckStop}
	grace := defaultGrace
	if g, ok := wire.(vendors.GracefulStop); ok {
		if lines := g.Closing(); len(lines) > 0 {
			// A closing line that cannot be queued is not a reason to skip the
			// close: the pipe is still there to shut. stopProc makes the same
			// call and for the same reason.
			_ = sess.SendAside(lines)
		}
		grace = g.Grace()
	}
	started := time.Now()
	sess.CloseInput()

	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	for {
		select {
		case <-sess.Done():
			c.Status = doctor.Passed
			c.Took = time.Since(started)
			return c
		case <-deadline.C:
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "the process was still running " + grace.String() +
				" after its stdin was closed, so the room's kill is what ends this seat"
			return c
		case <-events:
			// Drained rather than ignored. The runner writes its terminal event
			// to this channel before it closes Done, so a drive that stopped
			// reading here would block the goroutine it is waiting on and turn
			// every stop into a timeout.
		case <-ctx.Done():
			c.Status = doctor.Failed
			c.Took = time.Since(started)
			c.Detail = "the run was cancelled before the process exited"
			return c
		}
	}
}

// failureWords is the vendor's own words on a failure, and it prefers them to
// this package's.
//
// The runner has already assembled a note from the child's stderr tail
// (`failureNote`), which is the vendor's own first line where there is one. That
// is the sentence a reader can act on; the fallback exists so a failure with no
// words still gets a row rather than a blank.
func failureWords(ev runner.Event, fallback string) string {
	if n := strings.TrimSpace(ev.Note); n != "" {
		return n
	}
	if ev.Err != nil {
		return ev.Err.Error()
	}
	return fallback
}
