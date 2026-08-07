package council

import (
	"fmt"
	"os"
	"sync"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// The turn trace, and the thing about it that decided this design: the clock is
// ALWAYS running.
//
// runner/clock.go measures every turn unconditionally — newClock, begin and end
// are on the ordinary path — and `--trace` only decided whether emitTurnClock
// had a sink to hand the record to. So every slow turn anyone ever watched was
// measured, and the number was dropped on the floor because nobody had predicted
// that turn at launch. That is the §9.17 defect in its purest form: a control
// answering, at the one moment you cannot yet have the question, something you
// only learn by working.
//
// So the sink is installed for the whole life of the room and always keeps the
// last few turns in memory. Turning the trace on is opening a FILE, and the
// first thing that file receives is what the room already held — which is what
// makes `/trace <file>` typed straight after a 44-second turn capture that turn
// rather than the next one.

// maxTraceRing is how many turn clocks the room holds against a trace that has
// not been opened yet.
//
// 200 is maxHistory's 50 turns at four seats: the transcript's own window, so
// the trace can reach back exactly as far as the thing on screen that made you
// want it. A record is five words and a timestamp, so the whole ring is smaller
// than one turn of transcript.
const maxTraceRing = 200

// traceSink is the room's single trace destination: a bounded ring of recent
// records, and optionally a file receiving them as they arrive.
//
// It owns its own mutex and is reached through a POINTER held by the Model
// rather than being Model state, because the runner calls it from the goroutines
// reading each vendor's stdout. Touching Bubble Tea model fields from there
// would be a data race against Update; nothing in here reads or writes the
// Model, and nothing in the Model reads this during Render.
//
// The writes are serialised here rather than relied on from the runner — carried
// over from the sink this replaced, and still the reason for the lock: seats
// finish independently, each on its own goroutine, and interleaved lines would
// be exactly as useless as no lines.
type traceSink struct {
	mu   sync.Mutex
	ring []runner.TurnClock
	f    *os.File
	path string
}

func newTraceSink() *traceSink { return &traceSink{} }

// record is the runner.Trace installed for the life of the room.
func (t *traceSink) record(c runner.TurnClock) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f != nil {
		// Best effort, and silent. A full disk must not be able to take a room
		// down; the trace is a diagnostic and the room is the product.
		fmt.Fprintln(t.f, c)
	}
	t.ring = append(t.ring, c)
	if len(t.ring) > maxTraceRing {
		// Oldest first out, the same way a column's history is capped: a room
		// this long-lived has scrolled past them, and dropping the NEWEST would
		// make the cap look like the clock had stopped.
		t.ring = t.ring[len(t.ring)-maxTraceRing:]
	}
}

// open starts writing to path and flushes everything the ring is holding.
//
// Append rather than truncate, matching the flag: a path named twice in one
// session, or across two sessions, is a log rather than a snapshot. The flush is
// the whole point of the ring — see this file's opening note.
//
// Returns how many held records were written, which is what the notice reports.
// A count rather than a bare "on": a trace opened mid-conversation can see only
// as far back as the ring, and a number is the honest way to say so.
func (t *traceSink) open(path string) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return 0, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f != nil {
		_ = t.f.Close()
	}
	t.f, t.path = f, path
	for _, c := range t.ring {
		fmt.Fprintln(f, c)
	}
	return len(t.ring), nil
}

// close stops writing. The ring keeps filling, so a trace stopped and started
// again does not lose the turns in between.
func (t *traceSink) close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.f != nil {
		_ = t.f.Close()
		t.f, t.path = nil, ""
	}
}

// target is the file being written, or "" when the trace is off.
func (t *traceSink) target() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.path
}

// held is how many records the ring is carrying.
func (t *traceSink) held() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.ring)
}
