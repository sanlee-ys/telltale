package council

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Two defects measured on one real recording (2026-09-04, the final frame of
// a twelve-turn room at the owner's desk geometry), pinned here on a
// synthesized fixture with the same shape.
//
// The fixture: turn 1 goes to every seat. claude is persistent and ends its
// turn on its own line. codex is a one-shot seat that names its own end of
// turn and whose exit never lands before the next brief, so the replay holds
// it. agy finishes and exits. Turn 2 goes to claude and codex, with codex
// now persistent and one stale exit ahead of its session line. Turns 3 and 4
// go to claude alone.
const fixtureHeldSeat = "testdata/replay/held-seat.jsonl"

// TestAReplayReleasesASeatTheRecordSaysTookTheBrief: a dispatch line that
// names a seat is the recording's own statement that the seat was free, so
// the replay must not leave it on its old turn. Before the fix the seat never
// started turn 2, its turn 2 reply landed in turn 1's body, and its column
// ended the room saying `not addressed in turns 2–4` about turns it answered.
func TestAReplayReleasesASeatTheRecordSaysTookTheBrief(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(fixtureHeldSeat)
	if err != nil {
		t.Fatal(err)
	}
	m := newReplayModel(Options{}, rec, fixtureHeldSeat)
	m.st.Width, m.st.Height = 120, 24
	if rec.lines[7].Kind != "dispatch" || rec.lines[7].Turn != 2 {
		t.Fatalf("record 7 is %s turn %d; the bounds below expect the second dispatch", rec.lines[7].Kind, rec.lines[7].Turn)
	}

	// Turn 1 whole. codex settled on its own end-of-turn line and, with no
	// exit behind it, is still held on the turn.
	play(m, 0, 7)
	codex := m.column(model.VendorCodex)
	if codex.Phase != PhaseDone || codex.Elapsed != 2*time.Second {
		t.Fatalf("codex = %v after %v, want done after 2s", codex.Phase, codex.Elapsed)
	}
	if m.turnOf(model.VendorCodex) == nil {
		t.Fatal("the fixture no longer holds codex past its end of turn; the test below pins nothing")
	}

	// The second dispatch names codex. The held turn goes, and the new one
	// starts on the seat.
	play(m, 7, 8)
	if m.turnOf(model.VendorCodex) == nil || m.turnOf(model.VendorCodex).n != 2 {
		t.Fatalf("codex is not on turn 2 after the dispatch that named it: %+v", m.turnOf(model.VendorCodex))
	}
	if codex.TurnN != 2 || len(codex.History) != 1 || codex.History[0].N != 1 {
		t.Errorf("codex TurnN=%d history=%d; want turn 2 with turn 1 filed", codex.TurnN, len(codex.History))
	}
	if codex.Body != "" {
		t.Errorf("turn 1's body carried into turn 2: %q", codex.Body)
	}

	// The rest of the room.
	play(m, 8, len(rec.lines))
	if codex.Phase != PhaseDone || codex.Elapsed != 2*time.Second {
		t.Errorf("codex on turn 2 = %v after %v, want done after 2s (dispatch to its own end of turn)", codex.Phase, codex.Elapsed)
	}
	if !strings.Contains(codex.Body, "Refuse force-pushes") || strings.Contains(codex.Body, "erasing") {
		t.Errorf("turn 2's body is not turn 2's reply alone: %q", codex.Body)
	}
	if !codex.Skipped || codex.Note != "not addressed in turn 4" {
		t.Errorf("codex live note = %q skipped=%v; want the turn 4 note", codex.Note, codex.Skipped)
	}
	// The coalesced run under the column names turn 3 alone: turn 2 was
	// taken, and turn 4 is the live note's.
	from, to, run := trailingSkip(m.st, *codex)
	if !run || from != 3 || to != 3 {
		t.Errorf("trailing skip = %d..%d (%v), want turn 3 alone", from, to, run)
	}
	// The drawn line. At the reference width the idle seats are strips, and
	// the strip's sat-out line is the same run (stripSatOut). Before the fix
	// codex read `sat out 2–4`; agy's `sat out 2–3` is correct and stays.
	frame := render(m.st)
	if !strings.Contains(frame, "sat out 3 ") || strings.Contains(frame, "sat out 2–4") {
		t.Errorf("codex's sat-out line does not name turn 3 alone:\n%s", frame)
	}
}

// TestTheStripClockIsTheHeaderClock: a strip's turn line and the wide
// column's header describe one turn with one figure. Before the fix the strip
// read the running clock on a turn that had ended, so a seat that finished in
// 3s and then sat out three turns wore `turn 1 ✓ 3m` at strip width.
func TestTheStripClockIsTheHeaderClock(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(fixtureHeldSeat)
	if err != nil {
		t.Fatal(err)
	}
	m := newReplayModel(Options{}, rec, fixtureHeldSeat)
	m.st.Width, m.st.Height = 120, 24
	play(m, 0, len(rec.lines))

	span := rec.span()
	if span < 3*time.Minute {
		t.Fatalf("the fixture spans %v; the clocks below need minutes of silence to drift in", span)
	}
	g := GlyphsFor(false)
	for _, v := range []model.VendorID{model.VendorAntigravity, model.VendorCodex} {
		c := m.column(v)
		if c.Phase != PhaseDone || c.Elapsed == 0 {
			t.Fatalf("%s = %v after %v; the pin below needs a finished turn with a stamp", v, c.Phase, c.Elapsed)
		}
		_, header, _ := columnStatus(m.st, *c, g)
		header = strings.TrimSpace(header)
		_, _, _, _, strip, ok := stripTurn(m.st, *c)
		if !ok {
			t.Fatalf("%s has no strip turn", v)
		}
		if strip != header {
			t.Errorf("%s: strip clock %q, header clock %q; one turn, one figure", v, strip, header)
		}
		if want := dur(c.Elapsed); strip != want {
			t.Errorf("%s: strip clock %q, want the turn's own %q", v, strip, want)
		}
		if strip == dur(m.st.Now.Sub(c.Started)) {
			t.Errorf("%s: strip clock %q is the running clock, not the turn's", v, strip)
		}
	}
}
