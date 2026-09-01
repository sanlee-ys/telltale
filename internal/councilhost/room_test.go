package councilhost

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

func oneSeatRoom() Room {
	return Room{
		Version: RoomVersion,
		Seats:   []Seat{{Vendor: model.VendorClaude, Phase: PhaseIdle, Drivable: true}},
	}
}

// TestWaitingAndStreamingStayApart is council.Phase's own rule, carried across
// the process boundary.
//
// The two look almost identical on screen — nothing has been rendered yet — and
// they are different CLAIMS. Streaming means output is arriving and you are
// seeing it as it lands. Waiting means this vendor does not report incremental
// output, so there is nothing to show until it finishes. Collapsing them would
// be this product's own failure mode: a gauge implying knowledge it does not
// have.
//
// The promotion is read off an event that ACTUALLY carried text, never off a
// vendor's declared capability. A vendor that says it streams and then does not
// must not draw as though it did.
func TestWaitingAndStreamingStayApart(t *testing.T) {
	r := oneSeatRoom()
	r.beginTurn()
	if r.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("a dispatched seat with no output drew as %q", r.Seats[0].Phase)
	}
	// An empty text event changes nothing: no output arrived, so nothing is
	// streaming.
	if r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: ""}) {
		t.Fatal("an empty text event reported a change")
	}
	if r.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("an empty text event promoted the seat to %q", r.Seats[0].Phase)
	}
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "hi"})
	if r.Seats[0].Phase != PhaseStreaming {
		t.Fatalf("text arrived and the seat stayed %q", r.Seats[0].Phase)
	}
}

// TestAnExitCodeIsAbsentUntilAProcessReportsOne is §4a.1's zero-versus-absent
// rule, applied to the one number this projection carries.
//
// ExitCode is a POINTER for exactly this reason. "This process exited 0" and
// "no process has exited" are different facts, and a value type would render
// them the same — which is the one regression this repo exists to prevent.
func TestAnExitCodeIsAbsentUntilAProcessReportsOne(t *testing.T) {
	r := oneSeatRoom()
	if r.Seats[0].ExitCode != nil {
		t.Fatal("a fresh seat already carried an exit code")
	}
	r.beginTurn()
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "working"})
	if r.Seats[0].ExitCode != nil {
		t.Fatal("a streaming seat carried an exit code before anything exited")
	}
	// A vendor's own end-of-turn line ends the TURN, not the process. A
	// persistent seat takes another turn from the same pid, so reporting an
	// exit there would be inventing a process death.
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindDone, EndsTurn: true})
	if r.Seats[0].ExitCode != nil {
		t.Fatalf("an end-of-turn line produced exit code %d — the process did not exit",
			*r.Seats[0].ExitCode)
	}
	if r.Seats[0].Phase != PhaseDone {
		t.Fatalf("the seat did not finish: %q", r.Seats[0].Phase)
	}
	// A process that really exited nonzero reports its code.
	r.Apply(runner.Event{Vendor: model.VendorClaude, Kind: runner.KindError, ExitCode: 2, Note: "it broke"})
	if r.Seats[0].ExitCode == nil || *r.Seats[0].ExitCode != 2 {
		t.Fatalf("a failed process did not report its exit code: %+v", r.Seats[0])
	}
}

// TestAToolCallSaysWhatTheVendorSaidAboutIt pins the four outcome words apart.
//
// ActUnknown is the value that earns the type: a vendor reporting a step ENDED
// without saying whether it worked is a different fact from a vendor reporting
// success, and rendering them alike is the failure §4a.1 exists to forbid.
// ActDenied is the other one that must not collapse — a call the USER refused
// never ran, and a vendor reports that denial as an error result, so reading
// the stream alone would say the command failed.
func TestAToolCallSaysWhatTheVendorSaidAboutIt(t *testing.T) {
	cases := []struct {
		outcome runner.ActStatus
		detail  string
		want    string
	}{
		{runner.ActPending, "", "Bash: go test"},
		{runner.ActOK, "", "Bash: go test — ok"},
		{runner.ActFailed, "exit 1", "Bash: go test — failed: exit 1"},
		{runner.ActFailed, "", "Bash: go test — failed"},
		{runner.ActUnknown, "", "Bash: go test — ended, outcome not reported"},
		{runner.ActDenied, "", "Bash: go test — denied"},
	}
	seen := map[string]bool{}
	for _, c := range cases {
		got := actLine(runner.ActCall{Text: "Bash: go test", Outcome: c.outcome, Detail: c.detail})
		if got != c.want {
			t.Errorf("outcome %v rendered %q, want %q", c.outcome, got, c.want)
		}
		seen[got] = true
	}
	if len(seen) != len(cases) {
		t.Fatalf("two outcomes rendered identically; %d distinct lines from %d cases", len(seen), len(cases))
	}
}

// TestAGateIsRefusedInWordsRatherThanLeftBlocked is the honest half of a
// capability this host does not have.
//
// A gated seat BLOCKS on its question. This host cannot carry the question or
// the answer, so a seat that reached one would sit there forever and draw as a
// slow column. A blocked seat and a slow seat must not render alike, so the
// refusal is a sentence on the card.
func TestAGateIsRefusedInWordsRatherThanLeftBlocked(t *testing.T) {
	r := oneSeatRoom()
	r.beginTurn()
	if !r.Apply(runner.Event{
		Vendor: model.VendorClaude, Kind: runner.KindGate,
		Gate: &runner.Gate{Tool: "Write", Text: "Write: x.txt"},
	}) {
		t.Fatal("a gate request changed nothing on the room")
	}
	if !strings.Contains(r.Seats[0].Note, "cannot carry the question") {
		t.Fatalf("a gated seat said %q", r.Seats[0].Note)
	}
}

// TestAnEventForAnUnseatedVendorIsDropped keeps a stray event from inventing a
// column.
func TestAnEventForAnUnseatedVendorIsDropped(t *testing.T) {
	r := oneSeatRoom()
	if r.Apply(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "hello"}) {
		t.Fatal("an event for a vendor nobody seated reported a change")
	}
	if len(r.Seats) != 1 {
		t.Fatalf("the roster grew to %d seats", len(r.Seats))
	}
}

// TestAnUndrivableSeatIsNotDispatchedTo keeps two states apart that both look
// like "this column did nothing".
//
// A seat the host will not drive is NOT a seat that failed. beginTurn leaves it
// alone, so it keeps its undrivable phase and the sentence explaining it rather
// than being moved to waiting for a turn it will never take.
func TestAnUndrivableSeatIsNotDispatchedTo(t *testing.T) {
	r := Room{Seats: []Seat{
		{Vendor: model.VendorClaude, Phase: PhaseIdle, Drivable: true},
		{Vendor: model.VendorCursor, Phase: PhaseUndrivable, Drivable: false, Note: "protocol not driven here"},
	}}
	r.beginTurn()
	if r.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("the drivable seat drew as %q", r.Seats[0].Phase)
	}
	if r.Seats[1].Phase != PhaseUndrivable {
		t.Fatalf("an undrivable seat was moved to %q by a dispatch", r.Seats[1].Phase)
	}
	if r.Seats[1].Note == "" {
		t.Fatal("the undrivable seat lost the sentence explaining why")
	}
}

// TestACloneSharesNoSliceWithTheRoom pins the copy the wire depends on.
//
// The fold goroutine appends to Acts while the writer marshals a frame. A
// shared slice would be appended to mid-marshal, which shows up as a corrupted
// frame roughly once a week and is unfindable from the symptom.
func TestACloneSharesNoSliceWithTheRoom(t *testing.T) {
	r := oneSeatRoom()
	r.Apply(runner.Event{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{Text: "Bash: ls", Outcome: runner.ActOK}},
	})
	code := 7
	r.Seats[0].ExitCode = &code

	c := r.clone()
	r.Seats[0].Acts[0] = "MUTATED"
	*r.Seats[0].ExitCode = 9
	r.Apply(runner.Event{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{Text: "Bash: rm"}},
	})

	if c.Seats[0].Acts[0] == "MUTATED" {
		t.Fatal("the clone shares its Acts backing array with the room")
	}
	if len(c.Seats[0].Acts) != 1 {
		t.Fatalf("the clone grew with the room: %v", c.Seats[0].Acts)
	}
	if *c.Seats[0].ExitCode != 7 {
		t.Fatalf("the clone shares its ExitCode pointer with the room: %d", *c.Seats[0].ExitCode)
	}
}
