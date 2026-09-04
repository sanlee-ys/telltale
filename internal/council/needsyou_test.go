package council

import (
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The needs-you strip (§9.40). What these tests pin is not a layout but the two
// rules the line is only worth having if it keeps:
//
//   - it says a seat is waiting ONLY when the gate queue says so, and
//   - it stops saying it only when the reader goes to that seat or answers it.
//
// Everything else about the strip — where it sits, how it sheds, what it looks
// like — is a golden and a width sweep. Those two are properties, because the
// failure they guard is silent: a strip that could be talked out of a name by
// silence, by output, or by a clock would be an anti-stall that goes quiet at the
// exact moment a vendor is stuck.

// needsYouRoom is gatedRoom with two more seats blocked, so the strip has
// somebody to name.
//
// It keeps Claude's gate deliberately. The reader is focused on Claude (Focus is
// 0 in every fixture here), so that seat is the one the strip must NOT name — the
// card in its own column is already asking the question — and a fixture with only
// the two unfocused gates could not show the difference.
func needsYouRoom() State {
	st := gatedRoom()
	st.Gates = append(st.Gates,
		PendingGate{Vendor: model.VendorCodex, RequestID: "r2", ToolUseID: "t2",
			Text: "Bash: go test ./..."},
		PendingGate{Vendor: model.VendorAntigravity, RequestID: "r3", ToolUseID: "t3",
			Text: "Write: docs/design.md"})
	st.Columns[1].Sandbox = SandboxClaim{Level: SandboxGated, Detail: "asks before every tool call"}
	st.Columns[2].Sandbox = SandboxClaim{Level: SandboxGated, Detail: "asks before every tool call"}
	return st
}

// TestNeedsYouStrip is the golden: what the line looks like in a room where three
// seats are blocked and the reader is looking at one of them.
func TestNeedsYouStrip(t *testing.T) {
	golden(t, "needs-you", render(needsYouRoom()))
}

// TestNeedsYouStripASCII is the same frame in the reduced glyph set. The strip's
// whole signal is words — the mark in front of them is the room's existing
// warning glyph and carries nothing the phrase does not — so --ascii has to leave
// it saying the same thing.
func TestNeedsYouStripASCII(t *testing.T) {
	st := needsYouRoom()
	st.ASCII = true
	golden(t, "needs-you-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestTheStripNamesEverySeatButTheOneYouAreLookingAt.
//
// The two halves of the rule, asserted on one frame: the seats blocked in columns
// the reader is not in are named, and the seat under the reader's own focus is
// not. The second half is the one worth an assertion — a strip that named it
// anyway would be the room stating, above the frame, a fact the card three rows
// down is already stating in more detail (§9.30's duplication, one level up).
func TestTheStripNamesEverySeatButTheOneYouAreLookingAt(t *testing.T) {
	line := needsYouRowOf(t, render(needsYouRoom()))
	for _, want := range []string{needsYouLead, "Codex", "Antigravity"} {
		if !strings.Contains(line, want) {
			t.Errorf("the strip does not name %q: %q", want, line)
		}
	}
	if strings.Contains(line, "Claude Code") {
		t.Errorf("the strip names the seat the reader is focused on: %q", line)
	}
}

// TestTheStripSaysNothingWithoutAPendingGate is the honesty rule stated as the
// absence it produces.
//
// Three seats are given every shape that LOOKS like a seat wanting something —
// one waiting with nothing to show, one streaming a reply that ends in a
// question, one that failed outright — and the gate queue is empty. Not one of
// them is a measurement that a vendor is blocked (§4a.1), so the room draws no
// strip at all and spends no row on one.
func TestTheStripSaysNothingWithoutAPendingGate(t *testing.T) {
	st := room()
	st.Turn = 3
	st.Columns[0].Phase = PhaseWaiting
	st.Columns[1].Phase, st.Columns[1].Body = PhaseStreaming, "shall I write the file?"
	st.Columns[2].Phase, st.Columns[2].Note = PhaseFailed, "the vendor exited: exit 2"

	got := render(st)
	if strings.Contains(got, needsYouLead) {
		t.Errorf("a room with no pending gate drew a needs-you strip:\n%s", got)
	}
	// Nor an inbox: the failed seat has no Ended stamp, so nothing LANDED.
	if strings.Contains(got, unreadLead) {
		t.Errorf("a room with no landing drew an inbox:\n%s", got)
	}
	if n := needsYouRows(st); n != 0 {
		t.Errorf("needsYouRows = %d with an empty gate queue, want 0", n)
	}
}

// TestGoingToASeatIsWhatClearsIt, and the three things that do not.
//
// Focus is the ONLY act besides answering that takes a name off this line, and
// the alternatives it is being kept away from are exactly the ones that would
// make it a guess: a seat going quiet, a seat producing output, and time passing.
// Each is applied to a seat that is still blocked, and the name has to stay.
func TestGoingToASeatIsWhatClearsIt(t *testing.T) {
	base := needsYouRoom()

	if line := needsYouRowOf(t, render(base)); !strings.Contains(line, "Codex") {
		t.Fatalf("the fixture does not name Codex to begin with: %q", line)
	}

	// Focus moves to Codex (seat 2). It leaves; Antigravity, which the reader has
	// not been to, stays.
	moved := needsYouRoom()
	moved.Focus = 1
	line := needsYouRowOf(t, render(moved))
	if strings.Contains(line, "Codex") {
		t.Errorf("focusing a blocked seat did not clear it from the strip: %q", line)
	}
	if !strings.Contains(line, "Antigravity") {
		t.Errorf("focusing one seat cleared another the reader never visited: %q", line)
	}
	// Claude's gate is still pending and the reader has left that column, so it
	// comes BACK. That is the derived rule being honest rather than a bug: the
	// seat is still stopped and nobody is looking at it any more.
	if !strings.Contains(line, "Claude Code") {
		t.Errorf("a still-blocked seat the reader moved away from vanished from the strip: %q", line)
	}

	for _, tc := range []struct {
		name string
		mut  func(*State)
	}{
		{"the seat goes quiet", func(s *State) { s.Columns[1].Phase = PhaseIdle; s.Columns[1].Body = "" }},
		{"the seat produces output", func(s *State) {
			s.Columns[1].Phase = PhaseStreaming
			s.Columns[1].Body = "still working on it, and here is a lot more text"
		}},
		{"time passes", func(s *State) { s.Now = s.Now.Add(600e9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := needsYouRoom()
			tc.mut(&st)
			if line := needsYouRowOf(t, render(st)); !strings.Contains(line, "Codex") {
				t.Errorf("%s took a still-blocked seat off the strip: %q", tc.name, line)
			}
		})
	}

	// Answering is the other way off, and it is the queue's own doing rather than
	// the strip's: drop the entry and the name goes with it.
	answered := needsYouRoom()
	answered.Gates = answered.Gates[:2] // Antigravity's request, decided
	if line := needsYouRowOf(t, render(answered)); strings.Contains(line, "Antigravity") {
		t.Errorf("a seat whose gate was answered is still on the strip: %q", line)
	}
}

// TestTheStripCostsOneRowAndTakesItFromTheBody.
//
// It is chrome and chrome is paid for out of the reading area, the same trade the
// collapsed-seat notice and the band make. Asserted against the same room with
// and without the gates so nothing else can account for the difference — and on
// Body rather than on the rendered height, because the frame always fills the
// terminal and a row taken from the wrong place would still total 24.
func TestTheStripCostsOneRowAndTakesItFromTheBody(t *testing.T) {
	with := needsYouRoom()
	without := needsYouRoom()
	without.Gates = nil

	g := GlyphsFor(false)
	a, b := layoutFor(with, g), layoutFor(without, g)
	if a.NeedsYou != 1 {
		t.Errorf("Layout.NeedsYou = %d with three seats blocked, want 1", a.NeedsYou)
	}
	if b.NeedsYou != 0 {
		t.Errorf("Layout.NeedsYou = %d with an empty queue, want 0", b.NeedsYou)
	}
	if a.Body != b.Body-1 {
		t.Errorf("body is %d rows with the strip and %d without; the strip must cost exactly one",
			a.Body, b.Body)
	}
}

// TestTheStripDoesNotYieldWhereTheBandDoes.
//
// The two chrome lines are spent out of one budget and they rank differently on
// purpose (§9.40): the band exists to remove a duplication, so a short terminal
// drops it and loses nothing that was not already on screen, while this line is
// the only place the room says WHICH seat is stopped. So a frame short enough to
// retire the band must still be drawing the strip.
func TestTheStripDoesNotYieldWhereTheBandDoes(t *testing.T) {
	st := needsYouRoom()
	st.Height = MinHeight
	for i := range st.Columns {
		c := &st.Columns[i]
		c.startTurn(st.Turn, "one brief, every seat — the band's own condition", false)
		c.Phase = PhaseStreaming
	}

	lay := layoutFor(st, GlyphsFor(false))
	if lay.Band != 0 {
		t.Fatalf("the fixture is not short enough to retire the band: Band=%d", lay.Band)
	}
	if lay.NeedsYou != 1 {
		t.Error("the needs-you strip yielded on a short terminal alongside the band")
	}
	if !strings.Contains(render(st), needsYouLead) {
		t.Error("the strip is budgeted a row it does not draw")
	}
}

// needsYouLadderWidths is every width at which the strip can still say its own
// claim whole, swept rather than sampled.
//
// It starts at the lead's own width and runs well past the widest terminal, for
// stripHeader's reason: the ladder is a pure function of the width, so a constant
// that moved would otherwise change the behaviour with nothing noticing. Below
// the lead there is nothing left to shed and the honest render is a clip that
// says so — a width the frame cannot produce, since MinWidth leaves the strip 56
// cells and the lead costs eleven.
func needsYouLadderWidths(g Glyphs) (from, to int) {
	return lipgloss.Width(g.Warn + " " + needsYouLead), 200
}

// TestTheStripShedsWholeSeats sweeps a full five-seat roster across the ladder's
// whole domain, in both glyph sets, and asks the same question strips_test asks
// of a narrow column: did anything leave as a fragment?
//
// The vocabulary is taken from the roster under test rather than from a fixed
// list, so a seat added to the room is covered the day it is added. `+N more` is
// in it too — the marker is chrome and a clipped `mor` is exactly the failure
// §9.10 shipped once already.
func TestTheStripShedsWholeSeats(t *testing.T) {
	st := fiveSeatNeedsYou()
	vocabulary := append(strings.Fields(needsYouLead), "more")
	for _, c := range st.Columns {
		vocabulary = append(vocabulary, strings.Fields(c.Label)...)
	}

	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		from, to := needsYouLadderWidths(g)
		for w := from; w <= to; w++ {
			line := needsYouLine(st, w, PlainStyles(), g)
			if line == "" {
				t.Fatalf("w=%d ascii=%v: five blocked seats and no strip at all", w, ascii)
			}
			if n := lipgloss.Width(line); n > w {
				t.Errorf("w=%d ascii=%v: the strip is %d cells: %q", w, ascii, n, line)
			}
			// The words are the signal and they are the last thing to go.
			if !strings.Contains(line, needsYouLead) {
				t.Errorf("w=%d ascii=%v: the strip shed its own claim: %q", w, ascii, line)
			}
			assertWholeWords(t, []string{line}, vocabulary, "w=%d ascii=%v", w, ascii)
		}
	}
}

// TestTheStripNamesEverySeatBeforeItNamesFewer is the ladder's ordering, stated
// as the outcome it produces rather than as the branch that produces it.
//
// Identity yields before a seat does (§9.18): at a width where four names will
// not fit, four TAGS are the better answer, because the reader is looking for who
// is stopped and four abbreviations they already know answer it completely while
// two names and a count answer half of it. So wherever the strip is naming fewer
// seats than are blocked, it must already have spent the tag rung.
//
// The sweep has to reach below MinWidth to find that width at all — at sixty
// columns four tags cost 39 cells of 56 — which is the point: the rung exists for
// a roster this room has not grown yet, and a test that only walked today's
// terminals would pass whether or not it worked.
func TestTheStripNamesEverySeatBeforeItNamesFewer(t *testing.T) {
	st := fiveSeatNeedsYou()
	blocked := len(needsYou(st))
	if blocked < 4 {
		t.Fatalf("the fixture blocks %d seats, too few to shed", blocked)
	}
	g := GlyphsFor(false)
	from, to := needsYouLadderWidths(g)
	shed := 0
	for w := from; w <= to; w++ {
		line := needsYouLine(st, w, PlainStyles(), g)
		if !strings.Contains(line, "more") {
			continue
		}
		shed++
		for _, c := range st.Columns {
			if strings.Contains(line, c.Label) {
				t.Errorf("w=%d: the strip dropped a seat while still spelling %q out in full — "+
					"the tag rung was skipped: %q", w, c.Label, line)
			}
		}
	}
	if shed == 0 {
		t.Error("no width in the sweep sheds a seat, so this test asserted nothing")
	}
}

// TestTheStripDoesNotHardcodeTheRoster. Five seats landed in this room in two
// months and the numbering is positional, so a line that knew how many seats
// there were would be wrong by the next vendor.
func TestTheStripDoesNotHardcodeTheRoster(t *testing.T) {
	st := fiveSeatNeedsYou()
	st.Width = 200
	line := needsYouLine(st, st.Width-2*framePad, PlainStyles(), GlyphsFor(false))
	for _, c := range st.Columns {
		if c.Vendor == st.Columns[st.Focus].Vendor {
			continue
		}
		if !strings.Contains(line, c.Label) {
			t.Errorf("a five-seat room's strip does not name %q: %q", c.Label, line)
		}
	}
	// And the numbers are the positions, so the fifth seat is reachable by the
	// key printed beside it (§9.29).
	if !strings.Contains(line, "5 "+st.Columns[4].Label) {
		t.Errorf("the fifth seat carries no seat number on the strip: %q", line)
	}
}

// TestTheStripPrintsNoSeatNumberOnATurnPage.
//
// Digits focus a seat through viewKey, and gateKey falls through to it, so while
// the room is gating the number is a live key in both modes — except on a page,
// where focusSeat refuses outright because there are no columns to move between
// (§9.22). Printing it there would be the room naming a key that does nothing,
// which is §7.8's surprise; the NAMES stay, because who is stopped is still true.
func TestTheStripPrintsNoSeatNumberOnATurnPage(t *testing.T) {
	st := needsYouRoom()
	st.Page = TurnView{Open: true, Turn: st.Turn}

	line := needsYouRowOf(t, render(st))
	if !strings.Contains(line, "Codex") || !strings.Contains(line, "Antigravity") {
		t.Errorf("the page's strip stopped naming the blocked seats: %q", line)
	}
	if strings.Contains(line, "2 Codex") || strings.Contains(line, "3 Antigravity") {
		t.Errorf("the strip offers a seat key that does nothing on a page: %q", line)
	}
}

// TestTheStripNamesASeatFoldedOutOfTheGrid.
//
// A seat left out by --vendor still holds an unanswered request — its vendor is
// blocked whether or not the room drew it a column — and a blocked vendor with no
// card, no column and no line anywhere is the disappearance §4a.1 forbids. It
// gets no number, because no key in this room reaches it.
func TestTheStripNamesASeatFoldedOutOfTheGrid(t *testing.T) {
	st := needsYouRoom()
	st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}}

	line := needsYouRowOf(t, render(st))
	if !strings.Contains(line, "Antigravity") {
		t.Errorf("a blocked seat that is not on screen is on no line at all: %q", line)
	}
	if strings.Contains(line, "3 Antigravity") {
		t.Errorf("an unseated column carries a seat key that cannot reach it: %q", line)
	}
}

// TestTheStripNeverTearsTheFrame is the frame matrix's two assertions, applied to
// the one room shape it does not build: a five-seat roster with four of them
// blocked, at every width and height the sweep uses.
//
// The strip is chrome added ABOVE the body, so the failure it could cause is not
// a long line — it sheds — but a frame one row taller than the terminal, which
// scrolls the header off the top.
func TestTheStripNeverTearsTheFrame(t *testing.T) {
	for _, w := range matrixWidths {
		for _, h := range matrixHeights {
			for _, ascii := range []bool{false, true} {
				for _, expanded := range []bool{false, true} {
					st := fiveSeatNeedsYou()
					st.Width, st.Height, st.Expanded = w, h, expanded
					out := Render(st, PlainStyles(), GlyphsFor(ascii))

					lines := strings.Split(out, "\n")
					for i, l := range lines {
						if got := lipgloss.Width(l); got > w {
							t.Errorf("w=%d h=%d ascii=%v expanded=%v: line %d is %d cells: %q",
								w, h, ascii, expanded, i, got, l)
						}
					}
					if len(lines) > h {
						t.Errorf("w=%d h=%d ascii=%v expanded=%v: frame is %d lines, terminal is %d",
							w, h, ascii, expanded, len(lines), h)
					}
				}
			}
		}
	}
}

// fiveSeatNeedsYou is the full roster with every seat but the focused one blocked
// — the widest this line can ever get, and the shape the shedding ladder exists
// for.
func fiveSeatNeedsYou() State {
	st := needsYouRoom()
	st.Columns = append(st.Columns,
		Column{
			Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxRequested}, Gran: GranEvents, Phase: PhaseWaiting,
		},
		Column{
			Vendor: model.VendorGrok, Label: "Grok", Avail: AvailInstalled,
			Sandbox: SandboxClaim{Level: SandboxNone}, Gran: GranFinalOnly, Phase: PhaseWaiting,
		})
	st.Gates = append(st.Gates,
		PendingGate{Vendor: model.VendorCursor, RequestID: "r4", ToolUseID: "t4",
			Text: "Write: internal/council/needsyou.go"},
		PendingGate{Vendor: model.VendorGrok, RequestID: "r5", ToolUseID: "t5",
			Text: "Bash: go vet ./..."})
	return st
}

// needsYouRowOf pulls the strip out of a rendered frame by the one of its two
// leads that is on it, and fails when neither is there.
//
// By content rather than by row index: the strip's position is a decision §9.40
// argues for, and a helper that hard-coded it would go on passing if the line
// moved under the notice — asserting the wrong row's text under the right name.
func needsYouRowOf(t *testing.T, frame string) string {
	t.Helper()
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, needsYouLead) || strings.Contains(l, unreadLead) {
			return l
		}
	}
	t.Fatalf("no needs-you strip in the frame:\n%s", frame)
	return ""
}

// allLandedRoom is the frame that was misread (unreadLead's doc): a WRITE room
// of five gated seats at the end of an `@all` brief, every seat done, the reader
// still on seat 1. Nothing is pending. Four replies landed unread.
func allLandedRoom() State {
	st := room()
	st.Write = true
	st.Turn = 3
	// Wide enough for five columns: the misread frame was the columns tier, with
	// the strip's lead sitting above seat 1's own `✓ done`.
	st.Width = 150
	st.Columns = append(st.Columns,
		Column{Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailInstalled, Gran: GranEvents},
		Column{Vendor: model.VendorGrok, Label: "Grok", Avail: AvailInstalled, Gran: GranFinalOnly})
	base := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	st.Now = base.Add(20 * time.Second)
	for i := range st.Columns {
		c := &st.Columns[i]
		c.Sandbox = SandboxClaim{Level: SandboxGated, Detail: "asks before every tool call"}
		c.TurnN, c.Elapsed = 3, 3*time.Second
		c.Body = "Hello. Standing by."
		c.LastFocus = base.Add(-time.Minute)
		landed(c, PhaseDone, base.Add(time.Duration(i+1)*time.Second))
	}
	st.Columns[0].Body = "Hello. COO seat, standing by."
	st.Focus = 0
	return st
}

// TestAnInboxWithNoPendingGateSaysUnread is the fix for the frame allLandedRoom
// rebuilds. The strip on it may not say `NEEDS YOU`: no vendor is stopped on a
// keystroke, and the word that means that is not available for a reply that can
// wait. It says `UNREAD`, with no warning mark, over the same four entries — so
// the inbox keeps everything §9.54 gave it except the alarm. The footer's key
// follows the lead, and --ascii keeps the word.
//
// The entries carry NO phase word for an ordinary landing since the density
// pass (landedWord): the column header two rows below already says `done`, and
// the strip's own claim is the lead. The names are what it lists.
func TestAnInboxWithNoPendingGateSaysUnread(t *testing.T) {
	st := allLandedRoom()
	if len(st.Gates) != 0 {
		t.Fatal("fixture: a gate is pending")
	}
	got := render(st)
	golden(t, "inbox-unread", got)

	line := needsYouRowOf(t, got)
	if strings.Contains(line, needsYouLead) {
		t.Errorf("a strip with no blocked seat says %q: %q", needsYouLead, line)
	}
	if !strings.HasPrefix(strings.TrimSpace(line), unreadLead) {
		t.Errorf("the strip does not open with %q and no mark: %q", unreadLead, line)
	}
	for _, want := range []string{"2 Codex", "3 Antigravity", "4 Cursor", "5 Grok"} {
		if !strings.Contains(line, want) {
			t.Errorf("the inbox lost an entry %q: %q", want, line)
		}
	}
	if strings.Contains(line, "done") {
		t.Errorf("the strip repeats the word every column header already says: %q", line)
	}
	if strings.Contains(line, "Claude") {
		t.Errorf("the seat the reader is on is listed: %q", line)
	}
	if !strings.Contains(got, ". unread") || strings.Contains(got, ". needs you") {
		t.Errorf("the footer's key is not called what the strip is called:\n%s", got)
	}

	ascii := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(needsYouRowOf(t, ascii), unreadLead) {
		t.Errorf("--ascii lost the inbox's word:\n%s", ascii)
	}

	// The ink follows the word: an inbox is anchors, not an alarm. Under real
	// styles the lead and the names wear Strong and nothing on the line wears
	// Alert — which is what keeps the 2026-09-03 audit's "too many yellow
	// warnings" from returning on every @all brief.
	sty := NewStyles(true)
	styled := needsYouLine(st, st.Width-2*framePad, sty, UnicodeGlyphs())
	if !strings.Contains(styled, sty.Strong.Render(unreadLead)) {
		t.Errorf("the inbox lead is not at Strong: %q", styled)
	}
	if strings.Contains(styled, sty.Alert.Render(unreadLead)) || strings.Contains(styled, sty.Alert.Render("Codex")) {
		t.Errorf("an inbox with nothing blocked wears the gate's ink: %q", styled)
	}
}

// TestABlockedSeatOutranksTheInboxOnAMixedStrip: one seat stopped on a gate and
// one that landed share the line, and the line says `NEEDS YOU` — a vendor
// waiting on a key is the claim that cannot wait — while a landing that ended
// BADLY keeps its word, so three outcomes cannot read alike (§4a.1).
func TestABlockedSeatOutranksTheInboxOnAMixedStrip(t *testing.T) {
	st := inboxRoom()
	st.Gates = []PendingGate{{Vendor: model.VendorCursor, RequestID: "r4", ToolUseID: "t4",
		Text: "Write: docs/README.md"}}
	st.Columns[3].Phase, st.Columns[3].Ended = PhaseStreaming, time.Time{}

	line := needsYouRowOf(t, render(st))
	if !strings.Contains(line, needsYouLead) || strings.Contains(line, unreadLead) {
		t.Errorf("a strip with a blocked seat on it does not say %q: %q", needsYouLead, line)
	}
	for _, want := range []string{"2 Codex", "3 Antigravity failed", "4 Cursor"} {
		if !strings.Contains(line, want) {
			t.Errorf("the mixed strip lost %q: %q", want, line)
		}
	}
	if strings.Contains(line, "Cursor done") || strings.Contains(line, "Cursor streaming") {
		t.Errorf("the blocked seat carries a phase word: %q", line)
	}
	// The footer is the gate's own key list while a card is up, so the strip's
	// key is read off stripKeyLabel directly: it follows the lead.
	if got := stripKeyLabel(st); got != "needs you" {
		t.Errorf("stripKeyLabel = %q on a mixed strip, want %q", got, "needs you")
	}
}

// TestTheStripNeverSaysNeedsYouWithoutAPendingGate is the property the two
// leads exist to hold, swept over every room shape this file and crew_test
// build with an empty gate queue: `NEEDS YOU` is a claim that a vendor is
// stopped on a keystroke, and State.Gates is the only thing that knows.
func TestTheStripNeverSaysNeedsYouWithoutAPendingGate(t *testing.T) {
	landedFive := fiveSeatNeedsYou()
	landedFive.Gates = nil
	base := time.Date(2026, 9, 3, 9, 0, 0, 0, time.UTC)
	for i := range landedFive.Columns {
		landedFive.Columns[i].LastFocus = base
		landed(&landedFive.Columns[i], PhaseFailed, base.Add(time.Second))
	}
	for _, tc := range []struct {
		name string
		st   State
	}{
		{"every seat landed", allLandedRoom()},
		{"two landed, one working", inboxRoom()},
		{"five seats failed", landedFive},
		{"nothing landed, nothing pending", room()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.st.Gates) != 0 {
				t.Fatal("fixture: a gate is pending")
			}
			for _, ascii := range []bool{false, true} {
				got := Render(tc.st, PlainStyles(), GlyphsFor(ascii))
				if strings.Contains(got, needsYouLead) {
					t.Errorf("ascii=%v: %q with an empty gate queue:\n%s", ascii, needsYouLead, got)
				}
				if listed := len(needsYou(tc.st)) > 0; listed != strings.Contains(got, unreadLead) {
					t.Errorf("ascii=%v: strip has %d entries and the frame says %q=%v",
						ascii, len(needsYou(tc.st)), unreadLead, !listed)
				}
			}
		})
	}
	// And the inbox sheds like the gate strip does: whole seats, never a
	// fragment, with its own word the last thing to go.
	vocabulary := []string{unreadLead, "more"}
	for _, c := range landedFive.Columns {
		vocabulary = append(vocabulary, strings.Fields(c.Label)...)
	}
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		for w := lipgloss.Width(unreadLead); w <= 200; w++ {
			line := needsYouLine(landedFive, w, PlainStyles(), g)
			if line == "" {
				t.Fatalf("w=%d ascii=%v: four landed seats and no strip at all", w, ascii)
			}
			if n := lipgloss.Width(line); n > w {
				t.Errorf("w=%d ascii=%v: the strip is %d cells: %q", w, ascii, n, line)
			}
			if !strings.Contains(line, unreadLead) {
				t.Errorf("w=%d ascii=%v: the inbox shed its own word: %q", w, ascii, line)
			}
			assertWholeWords(t, []string{line}, vocabulary, "w=%d ascii=%v", w, ascii)
		}
	}
}
