package council

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// holdCardRecording is the synthesized recording that carries a write
// acknowledgement card (ack.go). One write brief, one card that names two
// seats as `write unasked` and one as `asking unmeasured`, the `send` that
// answered it, the dispatch, and the four seats' end-of-turn events.
//
// It exists because `examples/demo.jsonl` predates the card. That capture
// (2026-09-03) holds room, dispatch, event and gate records and no `ack`
// record, so the two goldens it feeds can never show the card. This file is
// the test-only stand-in until an owner records a room with the card in it.
// Fake ids, fake paths, synthesized words, realistic shape only (CLAUDE.md's
// fixture rule).
const holdCardRecording = "testdata/replay/hold-card.jsonl"

// holdCardRaisedAt is the index of the record that raises the card, and
// holdCardLen the record count. Pinned so the golden is taken at one moment:
// the card up, before the recording answers it.
const (
	holdCardRaisedAt = 0
	holdCardLen      = 14
)

// TestTheHoldCardReplaysToAGolden pins the frame a replay draws while the
// recorded card is up, at the demo geometry (180x50, replaydemo_test.go).
//
// The frame has to say three things at once: REPLAY in the header and on
// every seat's posture rail, HOLD on the composer border, and the card's own
// sentence with each class counted and named. The posture rail is the row
// that states what each seat may do, and the card is the row that says the
// operator saw those seats named before the brief left the room; a golden
// that pins both on one frame is what proves the replay draws the card over
// the same room the live operator answered it in.
func TestTheHoldCardReplaysToAGolden(t *testing.T) {
	log := countSpawns(t)
	rec, err := readRecording(holdCardRecording)
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.lines) != holdCardLen {
		t.Fatalf("the fixture has %d records; the constants above expect %d", len(rec.lines), holdCardLen)
	}
	m := newReplayModel(Options{}, rec, holdCardRecording)
	m.st.Width, m.st.Height = 180, 50

	play(m, 0, holdCardRaisedAt+1)
	if m.st.Ack == nil {
		t.Fatal("the ack record did not raise a card")
	}
	frame := render(m.st)
	golden(t, "demo-ack", frame)

	lines := strings.Split(frame, "\n")
	if !strings.Contains(lines[0], "REPLAY") {
		t.Errorf("the header does not say REPLAY: %q", lines[0])
	}
	if strings.Contains(lines[0], "WRITE") || strings.Contains(lines[0], "READ") {
		t.Errorf("the header claims a posture on a replay: %q", lines[0])
	}
	for _, want := range []string{
		"2 seats write unasked: Antigravity, Grok",
		"1 seat asking unmeasured: Codex",
		"y send",
		"n drop them",
		"HOLD",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame does not say %q:\n%s", want, frame)
		}
	}
	// The posture rail: one REPLAY per seat, ahead of the recorded posture.
	rail := ""
	for _, l := range lines {
		if strings.Contains(l, "gated") {
			rail = l
			break
		}
	}
	if n := strings.Count(rail, "REPLAY"); n != 4 {
		t.Errorf("the posture rail carries REPLAY %d times, want one per seat: %q", n, rail)
	}
	// No vendor is stopped while the card is up, because none has started
	// (LEDGER.md 2026-09-03), so the strip must not say so.
	if strings.Contains(frame, "NEEDS YOU") {
		t.Errorf("the strip claims a blocked vendor over a room that has dispatched nothing:\n%s", frame)
	}

	// The rest of the file: the decision takes the card down, the dispatch
	// lands, and every seat ends its turn.
	play(m, holdCardRaisedAt+1, holdCardLen)
	if m.st.Ack != nil {
		t.Error("the decision record did not take the card down")
	}
	if m.st.Turn != 1 {
		t.Errorf("turn %d after the file, want 1", m.st.Turn)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity, model.VendorGrok} {
		if c := m.column(v); c == nil || c.Phase != PhaseDone {
			t.Errorf("%s did not finish its turn", v)
		}
	}
	if log.n() != 0 {
		t.Fatalf("replaying the fixture spawned %d processes: %v", log.n(), log.specs)
	}
}

// TestTheHoldCardFixtureIsDeterministic: two plays of the file are one frame,
// so the golden above pins the renderer rather than the ordering of a map.
func TestTheHoldCardFixtureIsDeterministic(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(holdCardRecording)
	if err != nil {
		t.Fatal(err)
	}
	a := newReplayModel(Options{}, rec, holdCardRecording)
	b := newReplayModel(Options{}, rec, holdCardRecording)
	a.st.Width, a.st.Height = 180, 50
	b.st.Width, b.st.Height = 180, 50
	play(a, 0, holdCardRaisedAt+1)
	play(b, 0, holdCardRaisedAt+1)
	if render(a.st) != render(b.st) {
		t.Error("two plays of the fixture drew different frames with the card up")
	}
}

// TestTheScrubKeepsTheHoldCardAndReplacesTheWords runs the scrub over the
// fixture and reads the result back the way a replay would.
//
// An `ack` record is structure end to end: seat ids, a bool and a decision
// word, all of them telltale's own vocabulary (scrub.go). So the scrub has to
// keep both lines whole, with their seat names, their clause counts and
// their answer, while every brief and every reply around them changes. The
// scrubbed lines then have to parse as a recording, because a scrubbed file
// that the reader refused would be a fixture nobody can play.
func TestTheScrubKeepsTheHoldCardAndReplacesTheWords(t *testing.T) {
	rec, err := readRecording(holdCardRecording)
	if err != nil {
		t.Fatal(err)
	}
	out := scrubRecording(rec)
	if len(out) != len(rec.lines)+1 {
		t.Fatalf("scrub wrote %d lines for %d records plus a room line", len(out), len(rec.lines))
	}
	if !out[0].Scrubbed {
		t.Error("the room line does not say scrubbed")
	}

	var acks []recordLine
	for _, l := range out[1:] {
		if l.Kind == "ack" {
			acks = append(acks, l)
		}
	}
	if len(acks) != 2 {
		t.Fatalf("%d ack lines survived the scrub, want 2", len(acks))
	}
	raised, decided := acks[0], acks[1]
	if strings.Join(raised.Unasked, ",") != "agy,grok" {
		t.Errorf("the scrub changed the unasked seats: %v", raised.Unasked)
	}
	if strings.Join(raised.Unmeasured, ",") != "codex" {
		t.Errorf("the scrub changed the unmeasured seats: %v", raised.Unmeasured)
	}
	if len(raised.Unasked) != 2 || len(raised.Unmeasured) != 1 {
		t.Errorf("the scrub changed a clause count: %d unasked, %d unmeasured", len(raised.Unasked), len(raised.Unmeasured))
	}
	if !raised.Rest || raised.MS != 1200 {
		t.Errorf("the scrub changed the raised card: %+v", raised)
	}
	if decided.Decision != ackDecisionSend || decided.MS != 4800 {
		t.Errorf("the scrub changed the card's answer: %+v", decided)
	}

	// The words around the card are gone, and the shape is kept.
	for i, l := range out[1:] {
		was := rec.lines[i]
		if l.Kind != was.Kind || l.MS != was.MS || l.Vendor != was.Vendor || l.Event != was.Event {
			t.Errorf("record %d lost a structural fact: %+v -> %+v", i, was, l)
		}
		for j, s := range l.Sent {
			if s.Prompt == was.Sent[j].Prompt {
				t.Errorf("record %d seat %d kept its brief", i, j)
			}
		}
		if was.Text != "" && l.Text == was.Text {
			t.Errorf("record %d kept its text", i)
		}
		if was.SessionID != "" && l.SessionID == was.SessionID {
			t.Errorf("record %d kept its session id", i)
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{
		"add a marker file",
		"Marker written",
		"marker-grok.md",
		"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
		"ffffffff-1111-4222-8333-444444444444",
	} {
		if bytes.Contains(raw, []byte(gone)) {
			t.Errorf("the scrub kept %q", gone)
		}
	}
	for _, kept := range []string{`"unasked":["agy","grok"]`, `"unmeasured":["codex"]`, `"rest":true`, `"decision":"send"`} {
		if !bytes.Contains(raw, []byte(kept)) {
			t.Errorf("the scrub dropped %s", kept)
		}
	}

	// What the scrub writes, the reader reads, and the replay draws the card
	// from it.
	var buf bytes.Buffer
	for _, line := range out {
		b, merr := json.Marshal(line)
		if merr != nil {
			t.Fatal(merr)
		}
		buf.Write(append(b, '\n'))
	}
	back, err := parseRecording(&buf, "hold-card-scrubbed.jsonl")
	if err != nil {
		t.Fatalf("the scrub wrote a file the reader refuses: %v", err)
	}
	countSpawns(t)
	m := newReplayModel(Options{}, back, "hold-card-scrubbed.jsonl")
	m.st.Width, m.st.Height = 180, 50
	play(m, 0, holdCardRaisedAt+1)
	if m.st.Ack == nil {
		t.Fatal("the scrubbed file did not raise the card on replay")
	}
	if got := render(m.st); !strings.Contains(got, "2 seats write unasked: Antigravity, Grok") {
		t.Errorf("the scrubbed replay does not name the seats on the card:\n%s", got)
	}
}
