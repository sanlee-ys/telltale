package council

import (
	"strings"
	"testing"
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
