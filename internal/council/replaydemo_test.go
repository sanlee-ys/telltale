package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// demoRecording is the scrubbed room in examples/, and it is the only fixture
// in this package whose SHAPE nobody wrote.
//
// Every other golden here renders a State a test built, which means every
// other golden pins the room a test author thought of. This one is a real
// evening: five seats with one of them off the dispatch, seven briefs over
// forty minutes, a gate card raised on a write and answered, two turns routed
// to two seats and one to a single seat, ten stale exits from replaced
// processes, 314 tool calls, and 1,412 streamed text events arriving one and
// two runes at a time. CLAUDE.md's fixture rule is kept and not bent -- every
// word in the file is synthesized (scrub.go) -- and what is real is the event
// shape, which is the thing a renderer regression breaks.
//
// What it does NOT carry, measured over all 1,863 records rather than assumed:
// no column ever reaches PhaseCancelled, and no race board is drawn. A
// recording does not hold the operator's cancels (recording.go), and it has no
// record kind for a race, so a claim about either would be a claim about a
// frame this file cannot produce.
//
// It is read from the repository root rather than from testdata because it is
// a PRODUCT artifact first: the README's sixty-second path plays this file, so
// a visitor with no vendor installed has something to run. A copy under
// testdata would be a second file to keep in step with it.
const demoRecording = "../../examples/demo.jsonl"

// TestTheDemoRoomReplaysToAGolden pins the real room's event shape at two
// moments: the card up, and the end.
func TestTheDemoRoomReplaysToAGolden(t *testing.T) {
	log := countSpawns(t)
	rec, err := readRecording(demoRecording)
	if err != nil {
		t.Fatal(err)
	}
	if !rec.room.Scrubbed {
		t.Fatal("examples/demo.jsonl does not say it is scrubbed; only a scrubbed room belongs in this repository")
	}
	m := newReplayModel(Options{}, rec, demoRecording)
	// The projector width, and a tall terminal: the point of this golden is
	// the geometry a wide room actually draws, which the 120x24 goldens
	// beside it never reach.
	m.st.Width, m.st.Height = 180, 50

	// Found rather than pinned by index. A regenerated fixture may move every
	// number in the file, and a constant here would send the next author
	// hunting for a card that had shifted by one record.
	gate := -1
	for i, l := range rec.lines {
		if l.Kind == "event" && l.Gate != nil {
			gate = i
			break
		}
	}
	if gate < 0 {
		t.Fatal("the demo recording carries no gate card, so the second golden has no moment to pin")
	}

	play(m, 0, gate+1)
	if !m.st.Gating() {
		t.Fatal("the gate record did not raise a card")
	}
	atGate := render(m.st)
	golden(t, "demo-gate", atGate)

	play(m, gate+1, len(rec.lines))
	if m.st.Gating() {
		t.Error("the gate decision did not take the card down")
	}
	atEnd := render(m.st)
	golden(t, "demo-final", atEnd)

	if log.n() != 0 {
		t.Fatalf("replaying the demo spawned %d processes: %v", log.n(), log.specs)
	}
	for name, frame := range map[string]string{"demo-gate": atGate, "demo-final": atEnd} {
		lines := strings.Split(frame, "\n")
		if !strings.Contains(lines[0], "REPLAY") {
			t.Errorf("%s: the header does not say REPLAY: %q", name, lines[0])
		}
		if strings.Contains(lines[0], "WRITE") || strings.Contains(lines[0], "READ") {
			t.Errorf("%s: the header claims a posture on a replay: %q", name, lines[0])
		}
	}
	// The second claim a scrubbed room has to make, on the frame a reader is
	// left looking at.
	if !strings.Contains(atEnd, "scrubbed") {
		t.Errorf("the last frame of a scrubbed replay does not say so:\n%s", atEnd)
	}
	// The provenance, on the room line of every frame (roomline.go,
	// replayFact): the file's stamp, and that the stamp is synthesized with
	// the words. The stamp is scrub.go's own constant.
	for name, frame := range map[string]string{"demo-gate": atGate, "demo-final": atEnd} {
		for _, want := range []string{"recorded 2026-01-01 09:00 UTC", "the date and every word are synthesized"} {
			if !strings.Contains(frame, want) {
				t.Errorf("%s: the frame does not say %q:\n%s", name, want, frame)
			}
		}
	}
}

// demoDispatch is the index of the demo recording's dispatch line for one
// turn, found rather than pinned for the reason the gate is.
func demoDispatch(t *testing.T, rec *recording, turn int) int {
	t.Helper()
	for i, l := range rec.lines {
		if l.Kind == "dispatch" && l.Turn == turn {
			return i
		}
	}
	t.Fatalf("the demo recording has no dispatch for turn %d", turn)
	return -1
}

// TestTheDemoRoomComparesTwoSeatsToAGolden pins the two-seat compare at the
// share geometry (docs/room-identity.md, 2026-09-16), by both roads to it.
//
// The operator's road: after turn 9, which went to everyone, the grid is four
// equal columns, and `^w s` on Claude then `^w c` on Codex gives those two the
// reading width. The route's road: turn 11 went to Codex and Grok, so
// FrameOwners already holds the pair and no key is pressed. Both frames are
// two wide columns beside two strips, with the UNREAD strip naming the rest.
func TestTheDemoRoomComparesTwoSeatsToAGolden(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(demoRecording)
	if err != nil {
		t.Fatal(err)
	}
	twoWide := func(name string, st State) {
		t.Helper()
		got := widths(st)
		if len(got) != 4 {
			t.Fatalf("%s: %d panes drawn, want 4", name, len(got))
		}
		wide, strips := 0, 0
		var w []int
		for _, v := range got {
			switch {
			case v == stripColumn:
				strips++
			case v >= minColumn:
				wide++
				w = append(w, v)
			}
		}
		if wide != 2 || strips != 2 {
			t.Errorf("%s: widths %v, want two wide panes and two strips", name, got)
		}
		if len(w) == 2 && (w[0]-w[1] < 0 || w[0]-w[1] > 1) {
			t.Errorf("%s: the pair is %v, want equal to within the remainder", name, w)
		}
	}

	m := newReplayModel(Options{}, rec, demoRecording)
	m.st.Width, m.st.Height = 180, 50
	play(m, 0, demoDispatch(t, rec, 10))
	if m.st.FrameOwners != nil {
		t.Fatalf("turn 9 went to everyone, yet the frame has owners %v", m.st.FrameOwners)
	}
	m.st.PaneOwner, m.st.PanePeer = model.VendorClaude, model.VendorCodex
	keys := render(m.st)
	golden(t, "demo-compare", keys)
	twoWide("demo-compare", m.st)
	if !strings.Contains(keys, "panes compared") {
		t.Errorf("the compared frame does not say so:\n%s", keys)
	}

	r := newReplayModel(Options{}, rec, demoRecording)
	r.st.Width, r.st.Height = 180, 50
	play(r, 0, demoDispatch(t, rec, 12))
	if len(r.st.FrameOwners) != 2 {
		t.Fatalf("turn 11 went to two seats, yet the frame has owners %v", r.st.FrameOwners)
	}
	route := render(r.st)
	golden(t, "demo-compare-route", route)
	twoWide("demo-compare-route", r.st)
	if strings.Contains(route, "panes") {
		t.Errorf("the routed frame claims an arrangement no key made:\n%s", route)
	}
}

// TestTheDemoRoomIsDeterministic. Two plays of the file are one frame, so the
// golden above pins the renderer rather than the ordering of a map.
func TestTheDemoRoomIsDeterministic(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(demoRecording)
	if err != nil {
		t.Fatal(err)
	}
	a := newReplayModel(Options{}, rec, demoRecording)
	b := newReplayModel(Options{}, rec, demoRecording)
	a.st.Width, a.st.Height = 180, 50
	b.st.Width, b.st.Height = 180, 50
	play(a, 0, len(rec.lines))
	play(b, 0, len(rec.lines))
	if render(a.st) != render(b.st) {
		t.Error("two plays of the demo drew different frames")
	}
}
