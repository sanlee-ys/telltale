package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The other half of the arena's trace: the council labels the spec, and this
// package has to carry that label all the way onto the emitted line.
//
// Split across two packages because the seam is: a council test never spawns a
// vendor (CLAUDE.md), so it can witness the spec and nothing past it. The clock
// only runs where a process really starts, which is here.

// TestSpawnedRaceReachesTheRecord: a one-shot racer is the shape /arena
// actually dispatches for the batch seats, and the race has to survive the
// process it was handed to. This is the assertion the live gap failed —
// STATE.md, OBSERVED 2026-08-15/16: a trace armed before a race held no line
// anyone could attribute to it.
func TestSpawnedRaceReachesTheRecord(t *testing.T) {
	read := traceSink(t, model.VendorGemini)

	spec := clockSpec(t, model.VendorGemini, "quick")
	spec.Race = "arena/t9"

	ch := make(chan Event, 16)
	h, err := Start(context.Background(), spec, ch, parseT)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(30 * time.Second):
		t.Fatal("the racer never exited")
	}

	recs := read()
	if len(recs) != 1 {
		t.Fatalf("got %d records for one racer, want exactly one", len(recs))
	}
	if recs[0].Race != "arena/t9" {
		t.Errorf("Race = %q, want the race the room handed to the spec", recs[0].Race)
	}
	// The line is the surface, so the line is what is asserted — a field set on
	// a struct nobody printed would be the same gap one layer in.
	if line := recs[0].String(); !strings.Contains(line, "race=arena/t9") {
		t.Errorf("trace line %q does not name the race", line)
	}
	// The split itself still has to be there. A labelled line with nothing
	// measured on it would answer "which race" and lose the question the trace
	// exists for.
	if !recs[0].Spawn.Measured || !recs[0].Wait.Measured {
		t.Errorf("a one-shot racer reported spawn=%v wait=%v — the split it was raced to measure is missing",
			recs[0].Spawn, recs[0].Wait)
	}
}

// TestOrdinaryTurnLineHasNoRaceField: the ordinary line keeps the exact shape
// every existing reader knows. An ordinary turn is not a race with a missing
// id — it is not a race — so there is no field to render absent, which is why
// nothing is appended rather than a dash being printed (§4a.1).
func TestOrdinaryTurnLineHasNoRaceField(t *testing.T) {
	c := TurnClock{
		Vendor: model.VendorCodex,
		At:     time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC),
		Spawn:  Span{D: 30 * time.Millisecond, Measured: true},
		Wait:   Span{D: 3 * time.Second, Measured: true},
		Stream: Span{D: 5 * time.Second, Measured: true},
	}
	line := c.String()
	if strings.Contains(line, "race=") {
		t.Errorf("an ordinary turn's line %q carries a race field", line)
	}
	if !strings.HasSuffix(line, "total=8.03s") {
		t.Errorf("line %q no longer ends on total — the field order every reader parses moved", line)
	}
}

// TestRaceIsAppendedAfterTheTimings: the trace is a log a person greps and a
// script splits. Adding a column in the MIDDLE would silently move every
// timing one field to the right, so the position is pinned rather than left to
// whoever edits String next.
func TestRaceIsAppendedAfterTheTimings(t *testing.T) {
	c := TurnClock{
		Vendor: model.VendorGrok,
		At:     time.Date(2026, 8, 16, 9, 0, 0, 0, time.UTC),
		Spawn:  Span{D: 5 * time.Millisecond, Measured: true},
		Wait:   Span{D: time.Second, Measured: true},
		Stream: Span{D: time.Second, Measured: true},
		Race:   "arena/t9",
	}
	fields := strings.Fields(c.String())
	if got := fields[len(fields)-1]; got != "race=arena/t9" {
		t.Errorf("last field = %q, want the race appended after the timings", got)
	}
	if got := fields[len(fields)-2]; !strings.HasPrefix(got, "total=") {
		t.Errorf("second-to-last field = %q, want total — the race displaced a timing", got)
	}
}
