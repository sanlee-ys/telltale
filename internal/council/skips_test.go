package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// A seat that sits a turn out is the room working. Since the default route
// became one seat (#99), three columns do it every ordinary turn — so the
// question these tests answer is what a QUIET column costs: how many rows, and
// how loud (§9.19).

// skipColumn is a seat that answered `took` and sat out everything since.
//
// Built through startTurn rather than by assigning History directly, because the
// bug §9.19 fixes lived in startTurn: the note on a skipped column is about a
// LATER turn than the record being filed, and carrying it put one turn's success
// under another turn's absence.
func skipColumn(took []int, now int, skipping bool) Column {
	c := Column{Vendor: model.VendorCodex, Label: "Codex", Avail: AvailInstalled}
	for _, n := range took {
		c.startTurn(n, "brief for turn "+itoa(n), false)
		c.Body, c.Phase = "answer to turn "+itoa(n), PhaseDone
	}
	if skipping {
		c.Note, c.Skipped = "not addressed in turn "+itoa(now), true
	}
	return c
}

func skipRender(c Column, now int) string {
	st := room()
	st.Turn = now
	st.Columns = []Column{c}
	return strings.Join(columnText(st, c, 40, PlainStyles(), GlyphsFor(false)), "\n")
}

// TestConsecutiveSkipsCostOneLine is the complaint, at the size that produced
// it: one seat, one answer, and then six turns it was not part of.
func TestConsecutiveSkipsCostOneLine(t *testing.T) {
	got := skipRender(skipColumn([]int{1, 8}, 8, false), 8)
	if !strings.Contains(got, "not addressed in turns 2–7") {
		t.Errorf("a run of six skipped turns did not coalesce:\n%s", got)
	}
	if n := strings.Count(got, "not addressed"); n != 1 {
		t.Errorf("the run cost %d lines, want 1:\n%s", n, got)
	}
}

// TestASingleSkippedTurnIsSingular. "turns 4–4" is a sentence nobody writes.
func TestASingleSkippedTurnIsSingular(t *testing.T) {
	got := skipRender(skipColumn([]int{3, 5}, 5, false), 5)
	if !strings.Contains(got, "not addressed in turn 4") {
		t.Errorf("a one-turn gap did not render singular:\n%s", got)
	}
	if strings.Contains(got, "turns 4") {
		t.Errorf("a one-turn gap rendered as a range:\n%s", got)
	}
}

// TestABrokenRunStartsANewLineInPlace. A turn the seat TOOK ends a run, and the
// next run opens where the break is — the transcript still reads in order,
// which is the whole reason the coalescing happens on the way out rather than
// on the way in.
func TestABrokenRunStartsANewLineInPlace(t *testing.T) {
	got := skipRender(skipColumn([]int{1, 5, 9}, 9, false), 9)
	first := strings.Index(got, "not addressed in turns 2–4")
	second := strings.Index(got, "not addressed in turns 6–8")
	took5 := strings.Index(got, "answer to turn 5")
	switch {
	case first < 0 || second < 0:
		t.Fatalf("the two runs did not both coalesce:\n%s", got)
	case !(first < took5 && took5 < second):
		t.Errorf("the runs did not bracket the turn that broke them:\n%s", got)
	}
}

// TestTheLiveSkipKeepsItsOwnLine. The run above it is history; this one is the
// turn happening now, and it is the one a reader is deciding whether to act on.
func TestTheLiveSkipKeepsItsOwnLine(t *testing.T) {
	got := skipRender(skipColumn([]int{1}, 7, true), 7)
	if !strings.Contains(got, "not addressed in turns 2–6") {
		t.Errorf("the finished skips did not coalesce:\n%s", got)
	}
	if !strings.Contains(got, "not addressed in turn 7") {
		t.Errorf("the live skip lost its own line:\n%s", got)
	}
	// And the coalesced run stops one short of it rather than saying it twice.
	if strings.Contains(got, "turns 2–7") {
		t.Errorf("the run swallowed the live turn:\n%s", got)
	}
}

// TestASkipNeverLandsOnATurnThatHappened is the record-level half, and it is the
// bug §9.19 found underneath the rendering one: the note is written on the live
// column and the live column is what startTurn files, so a seat that answered
// turn 1 and sat out through 7 filed turn 1's record wearing "not addressed in
// turn 7".
func TestASkipNeverLandsOnATurnThatHappened(t *testing.T) {
	c := skipColumn([]int{1}, 7, true)
	c.startTurn(8, "back in the room", false)
	if len(c.History) != 1 {
		t.Fatalf("History = %d entries, want the one turn this seat took", len(c.History))
	}
	if h := c.History[0]; h.Note != "" {
		t.Errorf("turn %d's record carries a skip note: %q", h.N, h.Note)
	}
	if c.Skipped {
		t.Error("the skip flag survived the turn that ended it")
	}
}

// TestSkipsAreNotRecordedAsTurns. The coalescing is render-time and the data
// model is untouched: a run of six skips adds no TurnRecord, so `[` and `]`
// still hop between turns this seat really took (§9.20) and the transcript
// still skips from 1 to 8.
func TestSkipsAreNotRecordedAsTurns(t *testing.T) {
	c := skipColumn([]int{1, 8}, 8, false)
	if len(c.History) != 1 || c.History[0].N != 1 {
		t.Fatalf("History = %+v, want one record for turn 1", c.History)
	}
	if c.TurnN != 8 {
		t.Errorf("current turn is %d, want 8", c.TurnN)
	}
	st := room()
	st.Turn, st.Columns = 8, []Column{c}
	_, anchors := columnLines(st, c, 40, PlainStyles(), GlyphsFor(false))
	if len(anchors) != 2 {
		t.Fatalf("anchors = %+v, want turns 1 and 8 and nothing between", anchors)
	}
	if anchors[0].N != 1 || anchors[1].N != 8 {
		t.Errorf("anchors name turns %d and %d, want 1 and 8", anchors[0].N, anchors[1].N)
	}
}

// TestSkipsAreNeverInventedBeforeTheOldestRecord. History is capped at fifty and
// drops the oldest first, so the turns before the first record may well have
// been ANSWERED by this seat. Claiming them as skips would be the room inventing
// an absence — the same error as §9.9's inventing a conversation, in the other
// direction.
func TestSkipsAreNeverInventedBeforeTheOldestRecord(t *testing.T) {
	c := skipColumn([]int{30, 31}, 31, false)
	got := skipRender(c, 31)
	for _, claim := range []string{"turns 1", "turn 1 ", "turns 2"} {
		if strings.Contains(got, "not addressed in "+claim) {
			t.Errorf("claimed a skip before the oldest record (%q):\n%s", claim, got)
		}
	}
	if _, _, ok := trailingSkip(State{Turn: 5}, Column{TurnN: 0}); ok {
		t.Error("a seat that has never taken a turn reported a run of skips")
	}
}

// TestSkipsWearTheIdleMarkNotTheWarning, in both glyph sets.
//
// ⚠ / `!` opens a note because a note reports something that did not complete
// normally. Sitting a turn out is the room working, and a warning drawn on the
// common case is a warning the eye learns to skip. The WORD is unchanged and
// carries the fact either way; this pins the demotion of the mark, which is the
// part colour could otherwise have hidden.
func TestSkipsWearTheIdleMarkNotTheWarning(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		g := GlyphsFor(ascii)
		st := room()
		st.Turn = 7
		c := skipColumn([]int{1}, 7, true)
		st.Columns = []Column{c}
		lines := columnText(st, c, 40, PlainStyles(), g)
		var seen int
		for _, l := range lines {
			if !strings.Contains(l, "not addressed") {
				continue
			}
			seen++
			if !strings.HasPrefix(strings.TrimSpace(l), g.Idle) {
				t.Errorf("ascii=%v: a skip did not lead with %q: %q", ascii, g.Idle, l)
			}
			if strings.Contains(l, g.Warn) {
				t.Errorf("ascii=%v: a skip wore the warning mark %q: %q", ascii, g.Warn, l)
			}
		}
		if seen != 2 {
			t.Errorf("ascii=%v: %d skip lines, want the coalesced run and the live one", ascii, seen)
		}
	}
}

// TestARealNoteKeepsItsWarning guards the other side of that demotion: ⚠ still
// means something ended badly, and a cancellation is still one.
func TestARealNoteKeepsItsWarning(t *testing.T) {
	st := room()
	c := Column{
		Vendor: model.VendorCodex, Label: "Codex", Avail: AvailInstalled,
		Phase: PhaseCancelled, TurnN: 2, Prompt: "go", Note: "cancelled by you",
	}
	st.Turn, st.Columns = 2, []Column{c}
	got := strings.Join(columnText(st, c, 40, PlainStyles(), GlyphsFor(false)), "\n")
	if !strings.Contains(got, UnicodeGlyphs().Warn+" cancelled by you") {
		t.Errorf("a cancellation lost its warning mark:\n%s", got)
	}
}

// TestAnIdleStripSaysWhereItLeftOff. At fourteen cells a backgrounded seat had
// a header, a posture word and a run of skips, and the one thing a reader wants
// from it is which turn it last took.
func TestAnIdleStripSaysWhereItLeftOff(t *testing.T) {
	st := room()
	st.Turn = 9
	c := skipColumn([]int{8}, 9, true)
	st.Columns = []Column{c}
	got := strings.Join(columnText(st, c, stripWidth, PlainStyles(), GlyphsFor(false)), "\n")
	if !strings.Contains(got, "last: turn 8 "+UnicodeGlyphs().ActOK) {
		t.Errorf("the strip did not say where it left off:\n%s", got)
	}
	// And a wide column does not repeat itself: the turn separators are already
	// on screen with their own numbers and their own outcomes.
	wide := strings.Join(columnText(st, c, 40, PlainStyles(), GlyphsFor(false)), "\n")
	if strings.Contains(wide, "last: turn") {
		t.Errorf("a wide column repeated what its separators already say:\n%s", wide)
	}
}

// TestAStripWithNoTurnsBehindItSaysNothing. Absent renders absent (§4a.1) —
// there is no "last: —" in this room, and a seat that has never been asked
// anything has no turn to name.
func TestAStripWithNoTurnsBehindItSaysNothing(t *testing.T) {
	st := room()
	st.Turn = 4
	c := Column{
		Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailInstalled,
		Phase: PhaseIdle, Note: "not addressed in turn 4", Skipped: true,
	}
	st.Columns = []Column{c}
	got := strings.Join(columnText(st, c, stripWidth, PlainStyles(), GlyphsFor(false)), "\n")
	if strings.Contains(got, "last:") {
		t.Errorf("a seat with nothing behind it drew a placeholder:\n%s", got)
	}
	if !strings.Contains(got, "no turn") {
		t.Errorf("a never-dispatched seat stopped saying so:\n%s", got)
	}
}

// TestLastTurnLineShedsItsLabel. The number is the fact and `last:` is the
// label, so the label is what goes when the line will not fit.
//
// The width where that bites moved when stripColumn went from an arithmetic
// floor to a reading width: at fourteen a three-digit turn already overflowed,
// at eighteen it fits whole. Both halves are asserted, because the second is
// what the wider strip was FOR — §9.19 gave this line "room" as its stated goal
// and then drew it at a width where a long room wrapped it.
func TestLastTurnLineShedsItsLabel(t *testing.T) {
	g := GlyphsFor(false)
	c := Column{Vendor: model.VendorCodex, Phase: PhaseDone, TurnN: 137}

	// At strip width the whole line fits, label and all, on one row.
	got := lastTurnLine(c, State{Turn: 140}, stripWidth, PlainStyles(), g)
	if len(got) != 1 || strings.TrimSpace(got[0]) != "last: turn 137 "+g.ActOK {
		t.Errorf("lastTurnLine at strip width = %q, want the whole line on one row", got)
	}

	// Squeezed below it, the label is what yields — never the number and never
	// the mark, and never by clipping.
	got = lastTurnLine(c, State{Turn: 140}, 14, PlainStyles(), g)
	if len(got) != 1 || strings.TrimSpace(got[0]) != "turn 137 "+g.ActOK {
		t.Errorf("lastTurnLine squeezed = %q, want the label shed whole", got)
	}
}

// TestSkippedTurnsGolden pins the whole shape at strip width, which is where
// this change is actually read.
func TestSkippedTurnsGolden(t *testing.T) {
	golden(t, "skips-coalesced", render(skipRoom(false)))
}

// TestSkippedTurnsGoldenASCII: the ○ / ⚠ split has to survive the reduced set,
// where it is `.` against `!`.
func TestSkippedTurnsGoldenASCII(t *testing.T) {
	golden(t, "skips-coalesced-ascii", Render(skipRoom(true), PlainStyles(), GlyphsFor(true)))
}

// skipRoom is the post-#99 room several turns in: Claude has the frame, two
// seats have been sitting out since turn 1, and one of them was cancelled when
// it last spoke.
func skipRoom(ascii bool) State {
	st := room()
	st.Width, st.Height = 120, 24
	st.Turn, st.ASCII = 8, ascii
	st.FrameOwners = []model.VendorID{model.VendorClaude}
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].TurnN, st.Columns[0].Prompt = 8, "and now the resume path"
	st.Columns[0].Body = "Reading resume.go."

	codex := skipColumn([]int{1, 3}, 8, true)
	codex.Vendor, codex.Label = model.VendorCodex, "Codex"
	codex.Sandbox = SandboxClaim{Level: SandboxRequested}
	codex.Gran = GranFinalOnly
	st.Columns[1] = codex

	agy := skipColumn([]int{2}, 8, true)
	agy.Vendor, agy.Label = model.VendorAntigravity, "Antigravity"
	agy.Phase = PhaseCancelled
	agy.Sandbox = SandboxClaim{Level: SandboxNone}
	agy.Gran = GranFinalOnly
	st.Columns[2] = agy
	return st
}
