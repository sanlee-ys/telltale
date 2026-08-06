package runner

import (
	"fmt"
	"sync"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// A turn's clock, split where the time can actually be attributed.
//
// It exists because a 44-second reply to a greeting could not be explained. One
// elapsed figure per column is the sum of three unrelated stretches — launching
// a process, waiting on a model, and reading its output — so every account of
// that number was a guess. Splitting it measures which one it was.
//
// Nothing here makes a turn faster, and nothing here may. Measurement first is
// the whole point: a fast path added now would be aimed at whichever phase
// seemed likeliest, which is the guess this file replaces.

// Span is one stretch of a turn's clock, or the honest absence of one.
//
// Measured is what earns the type. A turn that launched nothing — every turn
// after the first on a seat whose process outlives it — did not spend zero
// milliseconds spawning; it did not spawn. Rendering the two alike is the same
// error as a gauge showing "no data" as zero (design.md §4a.1).
type Span struct {
	D        time.Duration
	Measured bool
}

// String renders a span for the trace line, and an unmeasured one as "-".
func (s Span) String() string {
	if !s.Measured {
		return "-"
	}
	return s.D.Round(time.Millisecond).String()
}

// TurnClock is one turn on one seat, measured at the process boundary.
type TurnClock struct {
	Vendor model.VendorID
	// At is when the turn ended.
	//
	// There is no turn NUMBER here. The runner has no access to the room's
	// counter, and a second count kept on this side would disagree with the
	// header the first time a seat sat a turn out — so a record is keyed by the
	// seat and the moment, both of which are facts this package holds.
	At time.Time

	// Spawn is process launch: Start's own entry to exec.Start returning. It
	// includes the pipes and the job object, because those are work the turn
	// pays for before the vendor exists.
	Spawn Span
	// Wait is from launch — or, on a seat whose process was already running,
	// from the moment the turn was handed to it — to the FIRST line off the
	// vendor's stdout.
	//
	// Stamped before that line is put on the event channel, so a slow consumer
	// is never charged to the vendor.
	Wait Span
	// Stream is the first line to the turn's last. Unmeasured on a turn that
	// produced no output at all, where Wait carries the whole thing.
	//
	// Wall clock and nothing else. A seat blocked on an approval card is inside
	// this figure, because from the process's side that is exactly what it is:
	// the vendor is stopped and no line is arriving.
	Stream Span
}

// Total is the measured stretches added up. Derived rather than stored, so it
// cannot disagree with them.
func (c TurnClock) Total() time.Duration {
	var d time.Duration
	for _, s := range []Span{c.Spawn, c.Wait, c.Stream} {
		if s.Measured {
			d += s.D
		}
	}
	return d
}

// String is the trace line: one turn, one seat, one row.
func (c TurnClock) String() string {
	return fmt.Sprintf("%s %-6s spawn=%s wait=%s stream=%s total=%s",
		c.At.UTC().Format("2006-01-02T15:04:05.000Z"), c.Vendor,
		c.Spawn, c.Wait, c.Stream, c.Total().Round(time.Millisecond))
}

// Trace receives one record per finished turn per seat.
type Trace func(TurnClock)

var (
	traceMu sync.RWMutex
	traceFn Trace
)

// SetTrace installs the sink every turn clock is written to. Nil — the default
// — writes nothing.
//
// A package-level sink rather than a field on Spec, and that is a seam rather
// than a preference: a Spec is built by the vendor adapters, which have no idea
// whether this run was asked to trace, and the room hands its specs to Start
// without looking at them. So it is installed once, before anything is
// dispatched, by whoever parsed the flag.
func SetTrace(t Trace) {
	traceMu.Lock()
	traceFn = t
	traceMu.Unlock()
}

func emitTurnClock(c TurnClock) {
	traceMu.RLock()
	fn := traceFn
	traceMu.RUnlock()
	if fn == nil {
		return
	}
	fn(c)
}

// clock accumulates one process's turn boundaries.
//
// One per process, not one per turn. A spawn-per-turn child sees exactly one
// turn through it; a long-lived one sees a turn per Send, and only the first of
// those carries a spawn — which is the fact this whole file was written to make
// visible.
type clock struct {
	vendor model.VendorID
	birth  time.Time

	mu sync.Mutex
	// pending holds the launch until a turn claims it. A session's spawn belongs
	// to the first turn sent, not to the process's own idle time before it.
	pending Span
	spawn   Span
	// open is set while a turn is being measured. A long-lived process emits
	// lines before its first turn is sent, and stamping a first token off one of
	// those would time the vendor's own startup as somebody's wait.
	open  bool
	begun time.Time
	first time.Time
}

// newClock starts the stopwatch. It runs from here rather than from exec.Start
// so the pipes and the job object land in the spawn figure with the launch.
func newClock(v model.VendorID) *clock {
	return &clock{vendor: v, birth: time.Now()}
}

// launched closes the spawn stretch. Called once, after the child is running.
func (c *clock) launched() {
	now := time.Now()
	c.mu.Lock()
	c.pending = Span{D: now.Sub(c.birth), Measured: true}
	c.mu.Unlock()
}

// begin opens a turn, claiming the launch if one is still unspent.
//
// A turn already open is left alone rather than restarted. On a persistent seat
// the same stdin carries turns, interrupts and gate decisions, and the runner
// cannot tell them apart without parsing a vendor's protocol — so the rule is
// positional instead: the first write after a turn ends starts the next one,
// and anything sent mid-turn belongs to the turn in progress.
func (c *clock) begin(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open {
		return
	}
	c.open, c.begun, c.first = true, at, time.Time{}
	c.spawn, c.pending = c.pending, Span{}
}

// observe takes one event on its way to the channel.
func (c *clock) observe(ev Event) {
	c.sawOutput(time.Now())
	if ev.EndsTurn {
		// A process that will take another turn says the turn is over with a
		// line, and only the adapter knows which line that is. A spawn-per-turn
		// child never sets this and ends on its exit instead.
		c.end(time.Now())
	}
}

func (c *clock) sawOutput(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open && c.first.IsZero() {
		c.first = at
	}
}

// end closes the open turn and emits its record. A no-op when no turn is open,
// which is what keeps a process dying at room teardown from filing a turn
// nobody took.
func (c *clock) end(at time.Time) {
	c.mu.Lock()
	if !c.open {
		c.mu.Unlock()
		return
	}
	rec := TurnClock{Vendor: c.vendor, At: at, Spawn: c.spawn}
	if c.first.IsZero() {
		// Nothing ever arrived, so there is no boundary to split on and the
		// whole turn was wait. Stream stays unmeasured rather than zero: a turn
		// that streamed nothing did not stream instantly.
		rec.Wait = Span{D: at.Sub(c.begun), Measured: true}
	} else {
		rec.Wait = Span{D: c.first.Sub(c.begun), Measured: true}
		rec.Stream = Span{D: at.Sub(c.first), Measured: true}
	}
	c.open, c.spawn, c.first = false, Span{}, time.Time{}
	c.mu.Unlock()

	// Emitted outside the lock: the sink writes to a file, and a slow disk must
	// not be able to stall the goroutine reading a vendor's stdout.
	emitTurnClock(rec)
}
