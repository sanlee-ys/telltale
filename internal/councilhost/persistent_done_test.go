package councilhost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestAPersistentSeatThatFinishedIsDrawnDone pins the defect the owner's first
// live drive of a hosted room found (2026-09-02).
//
// In a hosted read room, after a one-line brief, the claude seat's answer was
// complete and on screen and the seat STAYED `streaming`. It was still
// `streaming` after `/detach`, a closed window and a rejoin, with the identical
// text. The two batch seats moved to `done (exit 0)`. The same claude seat in a
// single-process room read `done`, so the seat finished and the HOST did not
// notice.
//
// The mechanism: the claude seat is the one vendors.Persistent seat, and its
// process does not exit between turns. Its turn ends with a `result` line,
// which the adapter reports as KindMeta with EndsTurn set — and the fold
// dropped every KindMeta. A finished seat drawn as `streaming` is a false claim
// about what an agent is doing (§4a.1), on the surface this split exists to
// draw.
//
// The stream is SYNTHESIZED and parsed by the real claude adapter, so the event
// shapes under test are the ones a live process produces, not shapes a test
// author guessed. No vendor is spawned: countSpawns stubs the session, and a
// real spawn would panic the whole test binary (TestMain).
func TestAPersistentSeatThatFinishedIsDrawnDone(t *testing.T) {
	countSpawns(t)
	stubRoomJob(t)

	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  "telltale-test-persistent-done",
		Posture:   vendors.PostureRead,
		Roster:    []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Snapshot().Seats[0].Persistent {
		t.Fatal("the host did not mark the claude seat persistent; the adapter implements vendors.Persistent")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	go h.fold()

	h.dispatch("say your seat name, one line", nil)

	// Exactly the lines a `--input-format stream-json` process emits for one
	// turn, in order: init, one text delta, and the result that ends the turn.
	parse := vendors.Registry()[model.VendorClaude].ParseEvent
	for _, line := range []string{
		`{"type":"system","subtype":"init","session_id":"sess-synth-1","model":"claude-opus-5"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Claude Code — the control plane."}}}`,
		`{"type":"result","session_id":"sess-synth-1","result":"Claude Code — the control plane.","total_cost_usd":0.01}`,
	} {
		ev, ok := parse([]byte(line))
		if !ok {
			t.Fatalf("the claude adapter dropped a line this test relies on: %s", line)
		}
		ev.Vendor = model.VendorClaude
		h.events <- ev
	}

	seat := awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseDone })
	if seat.Body != "Claude Code — the control plane." {
		t.Fatalf("the streamed text was not kept, or the result was drawn twice: %q", seat.Body)
	}
	if seat.SessionID != "sess-synth-1" {
		t.Fatalf("the session id did not land: %+v", seat)
	}
	if seat.ExitCode != nil {
		t.Fatalf("an end-of-turn line reported exit code %d — the process did not exit", *seat.ExitCode)
	}

	// The render is what the operator reads, so the claim is checked there
	// too: the word `done` on the seat's own line, and `streaming` nowhere.
	out := Render(h.Snapshot(), 80)
	if !strings.Contains(out, "claude — done") {
		t.Fatalf("the render did not draw the seat done:\n%s", out)
	}
	if strings.Contains(out, "streaming") {
		t.Fatalf("a finished seat still drew as streaming:\n%s", out)
	}

	// The turn guard reads the same phase the operator sees, under the same
	// lock (§7.31). Before this fix the seat never settled, so every later
	// brief was refused as "a turn is already running" — the room was one turn
	// long. A second dispatch must be accepted and counted.
	h.dispatch("and again", nil)
	r := awaitRoom2(t, h, func(r Room) bool { return r.Turn == 2 })
	if r.Notice != "" {
		t.Fatalf("the second brief was refused: %q", r.Notice)
	}
	if r.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("a redispatched seat with no output yet drew as %q", r.Seats[0].Phase)
	}
}

// TestABatchSeatSettlesOnItsEndOfTurnLineAndStaysBusyUntilItExits is the
// other half of the decision Room.applyAt's KindMeta branch records, as §7.31
// re-cut it.
//
// codex says `turn.completed` seconds before it exits, and that line is the
// same KindMeta-with-EndsTurn shape. The PHASE settles on it, so the column
// reads `done · exiting` instead of `streaming` for those seconds (§9.33), and
// the SEAT stays busy, so a dispatch landing in the gap is refused rather than
// killing a child that is still winding down (dispatchBatch). The exit clears
// the linger and lands the exit code.
func TestABatchSeatSettlesOnItsEndOfTurnLineAndStaysBusyUntilItExits(t *testing.T) {
	r := Room{Seats: []Seat{{Vendor: model.VendorCodex, Phase: PhaseIdle, Drivable: true}}}
	r.beginAll()
	r.Apply(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "codex here"})
	if !r.Apply(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindMeta, EndsTurn: true}) {
		t.Fatal("a batch seat's end-of-turn line reported no change")
	}
	s := r.Seats[0]
	if s.Phase != PhaseDone {
		t.Fatalf("a batch seat did not settle on its end-of-turn line: %q", s.Phase)
	}
	if !s.Settling {
		t.Fatal("a settled batch seat whose process is still up did not say it was exiting")
	}
	if !s.busy() {
		t.Fatal("a seat still winding down was free to take a brief; the next dispatch would kill its child")
	}
	if s.ExitCode != nil {
		t.Fatalf("an end-of-turn line reported exit code %d — the process did not exit", *s.ExitCode)
	}
	r.Apply(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindDone})
	s = r.Seats[0]
	if s.Phase != PhaseDone || s.ExitCode == nil || *s.ExitCode != 0 {
		t.Fatalf("the exit did not settle the batch seat: %+v", s)
	}
	if s.Settling || s.busy() {
		t.Fatalf("the exit did not free the seat: %+v", s)
	}
}

// TestALateResultDoesNotUnCancelASeat keeps an interrupted seat honest.
//
// The interrupt moves a running seat to cancelled at once. The vendor then
// answers the abandonment with a `result` of its own, and that line must not
// turn a seat the operator stopped into a seat that completed.
func TestALateResultDoesNotUnCancelASeat(t *testing.T) {
	r := oneSeatRoom()
	r.beginAll()
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "partial"})
	r.Seats[0].Phase = PhaseCancelled
	if r.Apply(turnEnded("partial answer")) {
		t.Fatal("a result after a cancel reported a change")
	}
	if r.Seats[0].Phase != PhaseCancelled {
		t.Fatalf("a late result re-labelled a cancelled seat as %q", r.Seats[0].Phase)
	}
}

// TestATurnThatStreamedNothingSaysSo is §4a.1's false-zero rule on the fold.
//
// A clean end with no text at all draws council's own sentence rather than an
// empty done column, and the result's own text is used only when nothing was
// streamed, so a normal turn never draws its reply twice.
func TestATurnThatStreamedNothingSaysSo(t *testing.T) {
	r := oneSeatRoom()
	r.beginAll()
	r.Apply(turnEnded(""))
	if r.Seats[0].Body != "[Turn completed with 0 text chunks streamed]" {
		t.Fatalf("an empty turn drew %q", r.Seats[0].Body)
	}

	r = oneSeatRoom()
	r.beginAll()
	r.Apply(turnEnded("the whole answer"))
	if r.Seats[0].Body != "the whole answer" {
		t.Fatalf("the result's text was not used as the fallback: %q", r.Seats[0].Body)
	}

	r = oneSeatRoom()
	r.beginAll()
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "streamed"})
	r.Apply(turnEnded("streamed"))
	if r.Seats[0].Body != "streamed" {
		t.Fatalf("the reply was drawn twice: %q", r.Seats[0].Body)
	}
}

// awaitSeat polls the host's own memory for the first seat to satisfy want.
//
// Off Snapshot rather than the wire, because these tests are about the FOLD:
// the wire's half is pinned by TestOneClientDrivesAHostedRoomEndToEnd, which
// now ends its turn with the same real event shape.
func awaitSeat(t *testing.T, h *Host, want func(Seat) bool) Seat {
	t.Helper()
	r := awaitRoom2(t, h, func(r Room) bool { return want(r.Seats[0]) })
	return r.Seats[0]
}

// awaitRoom2 polls Snapshot until want holds, or fails the test.
func awaitRoom2(t *testing.T, h *Host, want func(Room) bool) Room {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var last Room
	for time.Now().Before(deadline) {
		last = h.Snapshot()
		if want(last) {
			return last
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the room never satisfied the condition; the last snapshot was %+v", last)
	return Room{}
}
