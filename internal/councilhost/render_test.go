package councilhost

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestRenderIsPureOverTheRoom is council's and the HUD's house rule, carried
// here because the client half depends on it.
//
// Repaint at any width is free only if Render reads nothing but its argument.
// The moment it reads a clock, a file or an environment variable, a reattaching
// client's redraw stops being reproducible — and the same call would answer
// differently twice, which is what makes a golden flaky in a way that only
// shows up in CI.
func TestRenderIsPureOverTheRoom(t *testing.T) {
	r := Room{
		Version: RoomVersion, Workspace: `C:\src`, Turn: 2, Posture: "read",
		Seats: []Seat{{Vendor: model.VendorClaude, Phase: PhaseDone, Body: "hello", Acts: []Act{{Text: "Bash: ls", Status: ActOK}}}},
	}
	first := Render(r, 80)
	for i := 0; i < 5; i++ {
		if got := Render(r, 80); got != first {
			t.Fatalf("Render answered differently on call %d:\n%s\n---\n%s", i, first, got)
		}
	}
}

// TestRenderNeverOverrunsItsWidth.
//
// A client hands its own width to a pure function, so the width is the whole
// contract. A line that overran it would wrap in the terminal and put the room
// out of alignment on exactly the narrow terminals a person is most likely to
// have detached from.
func TestRenderNeverOverrunsItsWidth(t *testing.T) {
	r := Room{
		Version: RoomVersion, Turn: 1, Posture: "write",
		Workspace: `C:\a\very\long\workspace\path\that\keeps\going\and\going\telltale`,
		Seats: []Seat{{
			Vendor: model.VendorClaude, Phase: PhaseStreaming,
			Body: strings.Repeat("a long streamed sentence with plenty of words in it ", 6) +
				"\n" + strings.Repeat("z", 300),
			Acts: []Act{{Text: "Bash: " + strings.Repeat("x", 200), Status: ActOK}},
			Note: strings.Repeat("a note about why this seat is unhappy ", 6),
		}},
	}
	for _, w := range []int{24, 40, 60, 80, 100, 200} {
		for _, line := range strings.Split(Render(r, w), "\n") {
			if len([]rune(line)) > w {
				t.Errorf("at width %d a line ran to %d columns: %q", w, len([]rune(line)), line)
			}
		}
	}
}

// TestAnAbsentExitCodeDrawsNothing is §4a.1 at the render.
//
// The fold keeps "exited 0" and "nothing exited" apart as a pointer; this is
// the other half, where the distinction has to survive onto the screen. A
// renderer that printed `(exit 0)` for an absent code would put the fold's
// honesty back in the bin at the last step.
func TestAnAbsentExitCodeDrawsNothing(t *testing.T) {
	zero := 0
	absent := Room{Seats: []Seat{{Vendor: model.VendorClaude, Phase: PhaseDone}}}
	measured := Room{Seats: []Seat{{Vendor: model.VendorClaude, Phase: PhaseDone, ExitCode: &zero}}}
	if strings.Contains(Render(absent, 80), "exit") {
		t.Fatalf("a seat with no exit code drew one:\n%s", Render(absent, 80))
	}
	if !strings.Contains(Render(measured, 80), "exit 0") {
		t.Fatalf("a measured exit 0 did not draw:\n%s", Render(measured, 80))
	}
}

// TestTheHostExitNoticeSaysTheSeatsWentToo is §7.28's first crash mitigation.
//
// The operator cannot see the host die, because it has no terminal. So the
// client is the only thing that can say it happened, and it must say something
// different from an ordinary quiet room. The notice also has to point at the
// floor that survived — room.json — because that is what makes this change
// strictly additive rather than a new way to lose a conversation.
func TestTheHostExitNoticeSaysTheSeatsWentToo(t *testing.T) {
	n := RenderHostExit()
	for _, want := range []string{"exited", "seats went with it", "room.json"} {
		if !strings.Contains(n, want) {
			t.Errorf("the host-exit notice does not mention %q:\n%s", want, n)
		}
	}
	if n == Render(Room{}, 80) {
		t.Fatal("a dead host and an empty room render the same")
	}
	if !strings.Contains(ErrHostExited.Error(), "seats went with it") {
		t.Fatalf("the error and the notice drifted apart: %v", ErrHostExited)
	}
}

// TestAnUndrivableSeatDrawsItsReason.
//
// A seat the host will not drive must not read as a seat that is merely quiet.
func TestAnUndrivableSeatDrawsItsReason(t *testing.T) {
	r := Room{Seats: []Seat{{
		Vendor: model.VendorCursor, Phase: PhaseUndrivable, Drivable: false,
		Note: "this seat speaks a request/response protocol the host does not drive yet",
	}}}
	out := Render(r, 100)
	if !strings.Contains(out, string(PhaseUndrivable)) {
		t.Fatalf("the phase did not draw:\n%s", out)
	}
	if !strings.Contains(out, "does not drive yet") {
		t.Fatalf("the reason did not draw:\n%s", out)
	}
}
