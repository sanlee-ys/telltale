package runner

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// traceSink installs a trace for one test and hands back a reader for what
// landed on it.
//
// Filtered by vendor, because the sink is package-level and a child started by
// an earlier test can still be dying while this one runs. Every other test in
// this package drives model.VendorClaude, so a clock test asks for a different
// seat and reads only its own turns.
func traceSink(t *testing.T, v model.VendorID) func() []TurnClock {
	t.Helper()
	var (
		mu   sync.Mutex
		recs []TurnClock
	)
	SetTrace(func(c TurnClock) {
		if c.Vendor != v {
			return
		}
		mu.Lock()
		recs = append(recs, c)
		mu.Unlock()
	})
	t.Cleanup(func() { SetTrace(nil) })
	return func() []TurnClock {
		mu.Lock()
		defer mu.Unlock()
		return append([]TurnClock(nil), recs...)
	}
}

// clockSpec is a helper invocation attributed to a seat of its own, so the trace
// this test reads cannot contain another test's process.
func clockSpec(t *testing.T, v model.VendorID, mode string, args ...string) Spec {
	t.Helper()
	spec := helperSpec(t, mode, args...)
	spec.Vendor = v
	return spec
}

// TestTurnClockSplitsASpawnedTurn is the whole point of the file: a turn's
// elapsed figure stops being one number and becomes three that say where it
// went.
func TestTurnClockSplitsASpawnedTurn(t *testing.T) {
	read := traceSink(t, model.VendorCursor)

	ch := make(chan Event, 16)
	h, err := Start(context.Background(), clockSpec(t, model.VendorCursor, "slow"), ch, parseT)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	<-h.Done()

	recs := read()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want exactly one turn: %v", len(recs), recs)
	}
	c := recs[0]
	if !c.Spawn.Measured {
		t.Error("spawn is unmeasured on a turn that launched a process")
	}
	// The helper waits 300ms before its first line and 300ms between its two,
	// so both stretches must be attributed and neither may swallow the other.
	// Asserted loosely: the claim is that the split works, not that a CI runner
	// keeps time to the millisecond.
	if !c.Wait.Measured || c.Wait.D < 100*time.Millisecond {
		t.Errorf("wait = %v, want the vendor's 300ms pause before its first line", c.Wait)
	}
	if !c.Stream.Measured || c.Stream.D < 100*time.Millisecond {
		t.Errorf("stream = %v, want the 300ms between the vendor's two lines", c.Stream)
	}
	if c.Total() < c.Wait.D+c.Stream.D {
		t.Errorf("total %v is less than its own parts (%v + %v)", c.Total(), c.Wait.D, c.Stream.D)
	}
	if c.At.IsZero() {
		t.Error("the record does not say when the turn ended")
	}
}

// TestTurnThatSaidNothingHasNoStream: a turn that produced no output did not
// stream instantly, it did not stream. Reporting stream=0 would be this
// product's own failure mode — a figure that reads as a measurement and is not.
func TestTurnThatSaidNothingHasNoStream(t *testing.T) {
	read := traceSink(t, model.VendorGemini)

	ch := make(chan Event, 16)
	h, err := Start(context.Background(), clockSpec(t, model.VendorGemini, "silent"), ch, parseT)
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	<-h.Done()

	recs := read()
	if len(recs) != 1 {
		t.Fatalf("got %d records, want exactly one turn: %v", len(recs), recs)
	}
	c := recs[0]
	if c.Stream.Measured {
		t.Errorf("stream = %v on a turn that never said anything; it must be unmeasured", c.Stream)
	}
	if !c.Wait.Measured || c.Wait.D < 50*time.Millisecond {
		t.Errorf("wait = %v, want the whole turn attributed to waiting", c.Wait)
	}
	if got := c.Stream.String(); got != "-" {
		t.Errorf("an unmeasured stream renders %q, want %q", got, "-")
	}
}

// TestPersistentSeatSpawnsOnceAndSaysSo is the measurement the open question
// actually asked for: a seat whose process outlives the turn pays for a launch
// on its FIRST turn and on no other, and the trace has to make that legible
// rather than printing a zero that reads like a fast spawn.
func TestPersistentSeatSpawnsOnceAndSaysSo(t *testing.T) {
	read := traceSink(t, model.VendorCodex)

	ch := make(chan Event, 64)
	s, err := StartSession(context.Background(),
		clockSpec(t, model.VendorCodex, "session"), ch, parseSession)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Kill()

	for _, want := range []string{"one", "two", "three"} {
		if err := s.Send(turnLine(t, want)); err != nil {
			t.Fatalf("send %q: %v", want, err)
		}
		if _, last := awaitTurn(t, ch); !last.EndsTurn {
			t.Fatalf("turn %q did not end on the vendor's own line", want)
		}
	}

	recs := read()
	if len(recs) != 3 {
		t.Fatalf("got %d records for 3 turns: %v", len(recs), recs)
	}
	if !recs[0].Spawn.Measured {
		t.Error("the first turn on a new process reports no spawn; it paid for one")
	}
	for _, c := range recs[1:] {
		if c.Spawn.Measured {
			t.Errorf("a later turn reports spawn %v; nothing was launched for it", c.Spawn)
		}
	}
	for i, c := range recs {
		if !c.Wait.Measured {
			t.Errorf("turn %d has no wait; every turn waits for its first line", i+1)
		}
	}
}

// TestGateDecisionDoesNotRestartTheTurnClock. On a persistent seat the same
// stdin carries the turn, the interrupt and the answer to an approval card, and
// the runner cannot tell them apart without parsing a vendor's protocol. So a
// write arriving mid-turn must leave the clock alone — otherwise a turn held up
// by a card would report the time AFTER the keystroke and hide the wait that
// was the reason to look.
func TestGateDecisionDoesNotRestartTheTurnClock(t *testing.T) {
	ck := newClock(model.VendorClaude)
	ck.launched()

	begun := time.Now()
	ck.begin(begun)
	ck.begin(begun.Add(5 * time.Second)) // the mid-turn write

	ck.sawOutput(begun.Add(6 * time.Second))

	var got TurnClock
	SetTrace(func(c TurnClock) { got = c })
	t.Cleanup(func() { SetTrace(nil) })
	ck.end(begun.Add(7 * time.Second))

	if got.Wait.D != 6*time.Second {
		t.Errorf("wait = %v, want 6s measured from the turn's own start", got.Wait.D)
	}
	if got.Stream.D != time.Second {
		t.Errorf("stream = %v, want 1s", got.Stream.D)
	}
}

// TestTraceIsSilentByDefault: a room nobody asked to trace measures nothing
// anyone can see. The clock still runs — it is three timestamps — but no sink
// means no file, no line and no cost.
func TestTraceIsSilentByDefault(t *testing.T) {
	SetTrace(nil)
	ck := newClock(model.VendorClaude)
	ck.launched()
	ck.begin(time.Now())
	ck.sawOutput(time.Now())
	ck.end(time.Now()) // must not panic, must not block
}

// TestClockEndsOnlyAnOpenTurn: a process that dies at room teardown with no turn
// in flight has no turn to file. A record there would be a turn nobody took.
func TestClockEndsOnlyAnOpenTurn(t *testing.T) {
	n := 0
	SetTrace(func(TurnClock) { n++ })
	t.Cleanup(func() { SetTrace(nil) })

	ck := newClock(model.VendorClaude)
	ck.launched()
	ck.end(time.Now())
	ck.end(time.Now())
	if n != 0 {
		t.Errorf("%d records filed for a process that never took a turn", n)
	}

	ck.begin(time.Now())
	ck.end(time.Now())
	ck.end(time.Now()) // the process exit, after the vendor already ended the turn
	if n != 1 {
		t.Errorf("%d records for one turn, want exactly one", n)
	}
}

// TestTraceLineNamesEverySegment pins the line a person actually reads. It is
// the artifact of this whole change, so it is asserted rather than assumed.
func TestTraceLineNamesEverySegment(t *testing.T) {
	c := TurnClock{
		Vendor: model.VendorCursor,
		At:     time.Date(2026, 8, 6, 11, 22, 33, 0, time.UTC),
		Wait:   Span{D: 41 * time.Second, Measured: true},
		Stream: Span{D: 2100 * time.Millisecond, Measured: true},
	}
	line := c.String()
	for _, want := range []string{
		"2026-08-06T11:22:33.000Z", "cursor",
		"spawn=-", "wait=41s", "stream=2.1s", "total=43.1s",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("trace line %q is missing %q", line, want)
		}
	}
	if strings.Contains(line, "spawn=0s") {
		t.Error("a turn that launched nothing reported a zero-length launch")
	}
}
