package council

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// gatedRoom is a write-mode room where the Claude seat is blocked on a call.
//
// State is typed out by hand, as every renderer test here is: no process, no
// protocol, no terminal. The gate is a plain queue on State precisely so this
// is possible.
func gatedRoom() State {
	st := room()
	st.Write = true
	st.Turn = 1
	for i := range st.Columns {
		st.Columns[i].Sandbox = SandboxClaim{Level: SandboxWrite, Detail: "started with --write"}
	}
	// Only the seat that can be ASKED carries the gated badge. The other two are
	// batch CLIs; giving them the same word would claim a control they do not
	// have.
	st.Columns[0].Sandbox = SandboxClaim{Level: SandboxGated, Detail: "asks before every tool call"}
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Acts = []Act{
		{ID: "t0", Text: "Read: internal/council/view.go", Status: runner.ActOK},
		{ID: "t1", Text: "Write: internal/council/gate.go"},
	}
	st.Columns[0].Body = "I need to add the card renderer."
	st.Columns[1].Phase = PhaseWaiting
	st.Columns[2].Phase = PhaseWaiting
	st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Write: internal/council/gate.go",
	}}
	return st
}

func TestGateCard(t *testing.T) {
	golden(t, "gate-card", render(gatedRoom()))
}

func TestGateCardASCII(t *testing.T) {
	st := gatedRoom()
	st.ASCII = true
	golden(t, "gate-card-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

func TestGateQueue(t *testing.T) {
	st := gatedRoom()
	st.Gates = append(st.Gates,
		PendingGate{Vendor: model.VendorClaude, RequestID: "r2", ToolUseID: "t2",
			Text: "Bash: go test ./..."},
		PendingGate{Vendor: model.VendorClaude, RequestID: "r3", ToolUseID: "t3",
			Text: "Bash: git commit -m wip"})
	golden(t, "gate-queue", render(st))
}

// TestGateModeLineAnnouncesBothKeys is the house rule as an assertion: a mode
// that changes what an unmodified key means without saying so is the failure
// design.md §7.8 names, and this mode gives `y` and `n` meanings they do not
// have anywhere else in the room.
func TestGateModeLineAnnouncesBothKeys(t *testing.T) {
	got := render(gatedRoom())
	if !strings.Contains(got, "GATE") {
		t.Error("the mode line does not say the room is gating")
	}
	for _, want := range []string{"y approve", "n deny"} {
		if !strings.Contains(got, want) {
			t.Errorf("the mode line never spells out %q", want)
		}
	}
}

// TestGateModeLineSurvivesANotice.
//
// Every other mode lets a transient notice take the whole right side of the
// footer. This one must not: displacing the two keys that unblock a vendor with
// a message that scrolls away is the exact surprise the mode line exists to
// prevent.
func TestGateModeLineSurvivesANotice(t *testing.T) {
	st := gatedRoom()
	st.Notice = "rebuttal armed — each vendor will see the others' last answers"
	got := render(st)
	if !strings.Contains(got, "y approve") || !strings.Contains(got, "n deny") {
		t.Error("a notice displaced the gate keys from the mode line")
	}
}

// TestGateCardIsChromeNotBody. The badge line must not scroll away because a
// claim you cannot see is not a claim; this must not scroll away because a
// vendor is STOPPED behind it — and during a turn every column is following its
// own tail, so a card in the body would be pushed off by the output of the very
// call it is asking about.
func TestGateCardIsChromeNotBody(t *testing.T) {
	st := gatedRoom()
	st.Columns[0].Body = strings.Repeat("a long streamed reply that pushes everything up ", 40)
	st.Columns[0].Follow = true

	got := render(st)
	if !strings.Contains(got, "waiting on you") {
		t.Error("the approval card scrolled away under the vendor's own output")
	}
}

// TestOnlyTheBlockedColumnShowsTheCard. The gate belongs to one seat. A card in
// a column that is not waiting would ask the user to approve something that
// vendor never requested.
func TestOnlyTheBlockedColumnShowsTheCard(t *testing.T) {
	st := gatedRoom()
	if n := strings.Count(render(st), "waiting on you"); n != 1 {
		t.Errorf("the card appears %d times, want exactly once", n)
	}
}

// TestGatedBadgeIsOnlyOnTheSeatThatCanAsk.
func TestGatedBadgeIsOnlyOnTheSeatThatCanAsk(t *testing.T) {
	got := render(gatedRoom())
	if n := strings.Count(got, "gated"); n != 1 {
		t.Errorf("the gated badge appears %d times, want once", n)
	}
	if n := strings.Count(got, "WRITES"); n != 2 {
		t.Errorf("the plain write badge appears %d times, want twice", n)
	}
	// The room still says WRITE in the header. "gated" is only unambiguous
	// because that marker has already established what kind of room this is.
	if !strings.Contains(got, "WRITE") {
		t.Error("the header stopped announcing write mode")
	}
}

// TestDenialReadsAsARefusalNotAFailure.
//
// The vendor echoes a denial back as an is_error tool_result carrying council's
// own refusal text, so on the stream alone it is indistinguishable from a tool
// that broke. The trace has to say which one happened, in words, before it says
// it in colour.
func TestDenialReadsAsARefusalNotAFailure(t *testing.T) {
	st := gatedRoom()
	st.Gates = nil
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Acts = []Act{
		{ID: "t1", Text: "Write: internal/council/gate.go", Status: runner.ActDenied},
		{ID: "t2", Text: "Bash: go build ./...", Status: runner.ActFailed,
			Detail: "exit 2: undefined: gateCard"},
	}
	got := render(st)
	if !strings.Contains(got, "denied by you") {
		t.Error("a refused call does not say it was refused")
	}
	golden(t, "gate-denied", got)
}

// TestDeniedAndFailedAreDifferentInColourToo. Word first, colour second — but
// the colours must not collide either, or a monochrome reader and a colour
// reader would be told different things.
func TestDeniedAndFailedAreDifferentInColourToo(t *testing.T) {
	sty := NewStyles(true)
	denied, _ := actMark(runner.ActDenied, sty, GlyphsFor(false))
	failed, _ := actMark(runner.ActFailed, sty, GlyphsFor(false))
	if denied == failed {
		t.Fatalf("denied and failed render the same mark %q", denied)
	}

	_, deniedStyle := actMark(runner.ActDenied, sty, GlyphsFor(false))
	_, failedStyle := actMark(runner.ActFailed, sty, GlyphsFor(false))
	if deniedStyle.Render("x") == failedStyle.Render("x") {
		t.Error("a refusal is styled identically to a vendor failure")
	}
}

// TestGateCardNeverExceedsTheWidth sweeps the card across every width and both
// glyph sets, including the narrow tiers where it is dropped entirely rather
// than rendered unreadably.
func TestGateCardNeverExceedsTheWidth(t *testing.T) {
	long := PendingGate{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Bash: " + strings.Repeat("verylongtokenwithnobreaks", 6),
	}
	for _, w := range []int{60, 72, 80, 95, 96, 100, 120, 160, 201} {
		for _, ascii := range []bool{false, true} {
			for _, expanded := range []bool{false, true} {
				st := gatedRoom()
				st.Width, st.Height = w, 24
				st.Expanded = expanded
				st.Gates = []PendingGate{long, long, long}
				out := Render(st, PlainStyles(), GlyphsFor(ascii))
				for i, line := range strings.Split(out, "\n") {
					if got := lipgloss.Width(line); got > w {
						t.Errorf("w=%d ascii=%v expanded=%v: line %d is %d cells: %q",
							w, ascii, expanded, i, got, line)
					}
				}
				if n := len(strings.Split(out, "\n")); n > 24 {
					t.Errorf("w=%d ascii=%v expanded=%v: frame is %d lines, terminal is 24",
						w, ascii, expanded, n)
				}
			}
		}
	}
}

// TestScrollCeilingAccountsForTheCard. The card is chrome, so it costs body
// lines. A ceiling computed from a constant would let the tail scroll past the
// end of the content while a card was up, which shows a column of blank cells
// where the newest output should be.
func TestScrollCeilingAccountsForTheCard(t *testing.T) {
	st := gatedRoom()
	st.Columns[0].Body = strings.Repeat("line of the reply\n", 40)

	with := MaxScroll(st, 0)
	st.Gates = nil
	without := MaxScroll(st, 0)
	if with <= without {
		t.Errorf("scroll ceiling with a card = %d, without = %d; the card must cost body lines",
			with, without)
	}
}
