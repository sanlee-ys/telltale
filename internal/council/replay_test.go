package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// replayFixture opens the synthesized recording as a replay, at the reference
// width. Nothing here reads a clock: the model's Now is the recording's
// start, and every record it plays stamps the recording's own time.
func replayFixture(t *testing.T) *Model {
	t.Helper()
	rec, err := readRecording(fixtureRecording)
	if err != nil {
		t.Fatal(err)
	}
	m := newReplayModel(Options{}, rec, fixtureRecording)
	m.st.Width, m.st.Height = 120, 24
	return m
}

// play feeds records [from, to) through Update, the messages the replay's
// own Cmd would deliver.
func play(m *Model, from, to int) {
	for i := from; i < to; i++ {
		m.Update(replayMsg{i})
	}
}

// fixtureGateAt is the index of the gate event in the fixture, and
// fixtureLen the record count. Pinned so the goldens below are taken at the
// same two moments: the card up, and the end.
const (
	fixtureGateAt = 5
	fixtureLen    = 11
)

// TestAReplayPlaysTheFixtureToAGolden is the product: the same room, drawn
// from a file, with REPLAY on every frame — in the header, on every column's
// badge row — and nothing spawned to draw it.
func TestAReplayPlaysTheFixtureToAGolden(t *testing.T) {
	log := countSpawns(t)
	m := replayFixture(t)
	if len(m.replay.rec.lines) != fixtureLen {
		t.Fatalf("the fixture has %d records; the constants above expect %d", len(m.replay.rec.lines), fixtureLen)
	}

	play(m, 0, fixtureGateAt+1)
	if !m.st.Gating() {
		t.Fatal("the gate record did not raise a card")
	}
	atGate := render(m.st)
	golden(t, "replay-gate", atGate)
	// The footer names the key's live meaning (§7.8): with two seats still
	// answering, ctrl+c would be `cancel all` in a live room and is `quit`
	// here. Read on this frame, where the hints are up; the last frame's
	// footer carries the end-of-replay notice instead.
	if !strings.Contains(atGate, "ctrl+c quit") || strings.Contains(atGate, "cancel") {
		t.Errorf("the footer promises a cancel on a replay:\n%s", atGate)
	}

	play(m, fixtureGateAt+1, fixtureLen)
	if m.st.Gating() {
		t.Error("the gate decision did not take the card down")
	}
	got := render(m.st)
	golden(t, "replay", got)

	if log.n() != 0 {
		t.Fatalf("a replay spawned %d processes: %v", log.n(), log.specs)
	}
	for _, frame := range []string{atGate, got} {
		lines := strings.Split(frame, "\n")
		if !strings.Contains(lines[0], "REPLAY") {
			t.Errorf("the header does not say REPLAY: %q", lines[0])
		}
		if strings.Contains(lines[0], "WRITE") || strings.Contains(lines[0], "READ") {
			t.Errorf("the header claims a posture on a replay: %q", lines[0])
		}
		// Every visible column's badge row: the row that carries the
		// recorded postures carries REPLAY ahead of each of them.
		badges := ""
		for _, l := range lines {
			if strings.Contains(l, "gated") {
				badges = l
				break
			}
		}
		if n := strings.Count(badges, "REPLAY"); n != 3 {
			t.Errorf("badge row carries REPLAY %d times, want one per column: %q", n, badges)
		}
	}
	// What the recording said happened, drawn: codex finished at its own
	// time, claude and agy are still on the turn.
	codex := m.column(model.VendorCodex)
	if codex.Phase != PhaseDone || codex.Elapsed != 3100*time.Millisecond {
		t.Errorf("codex = %v after %v, want done after 3.1s", codex.Phase, codex.Elapsed)
	}
	if codex.CostUSD == nil || *codex.CostUSD != 0.0123 {
		t.Errorf("codex cost = %v, want the recorded figure", codex.CostUSD)
	}
	claude := m.column(model.VendorClaude)
	if claude.Phase != PhaseStreaming || !claude.GateWait.Measured || claude.GateWait.D != 2*time.Second {
		t.Errorf("claude = %v, gate wait %+v; want streaming with a measured 2s wait", claude.Phase, claude.GateWait)
	}
	if m.st.Turn != 1 || m.st.TurnRoute == nil {
		t.Errorf("turn %d, route %v", m.st.Turn, m.st.TurnRoute)
	}
}

// TestAReplaySurvivesASCII: the label is a word.
func TestAReplaySurvivesASCII(t *testing.T) {
	countSpawns(t)
	m := replayFixture(t)
	m.st.ASCII = true
	play(m, 0, fixtureLen)
	got := Render(m.st, PlainStyles(), GlyphsFor(true))
	golden(t, "replay-ascii", got)
	if strings.Count(got, "REPLAY") < 4 {
		t.Errorf("--ascii lost the label:\n%s", got)
	}
}

// TestAReplayIsDeterministic: two plays of one file are one frame, and two
// renders of one State are one frame. Nothing in the replay reads a clock
// into anything Render draws.
func TestAReplayIsDeterministic(t *testing.T) {
	countSpawns(t)
	a, b := replayFixture(t), replayFixture(t)
	play(a, 0, fixtureLen)
	play(b, 0, fixtureLen)
	if render(a.st) != render(b.st) {
		t.Error("two plays of the fixture drew different frames")
	}
	if render(a.st) != render(a.st) {
		t.Error("Render is not pure over a replayed State")
	}
	// A stale message — an index already played — changes nothing.
	before := render(a.st)
	a.Update(replayMsg{3})
	if render(a.st) != before {
		t.Error("a stale replay message moved the room")
	}
}

// TestAReplayRefusesDispatch: enter says so, the card's keys say so, and no
// process starts. Reading keys still work.
func TestAReplayRefusesDispatch(t *testing.T) {
	log := countSpawns(t)
	m := replayFixture(t)
	play(m, 0, fixtureGateAt+1)

	// The card is up; y, n and a are the recording's to answer.
	for _, k := range []string{"y", "n", "a"} {
		m.key(key(k))
		if m.st.Notice != replayNotice || len(m.st.Gates) != 1 {
			t.Errorf("%s on a replayed card: notice %q, %d cards", k, m.st.Notice, len(m.st.Gates))
		}
	}
	play(m, fixtureGateAt+1, fixtureLen)

	m.st.Mode = ModeComposing
	m.setDraft("@codex another brief")
	m.key(key("enter"))
	if m.st.Notice != replayNotice {
		t.Errorf("enter: notice %q, want %q", m.st.Notice, replayNotice)
	}
	if m.st.Turn != 1 || log.n() != 0 {
		t.Errorf("enter dispatched: turn %d, %d spawns", m.st.Turn, log.n())
	}
	if m.st.Draft != "@codex another brief" {
		t.Errorf("the draft was consumed: %q", m.st.Draft)
	}
	_, plain := hints(PlainStyles(), UnicodeGlyphs(), modeHints(m.st, UnicodeGlyphs()))
	if !strings.Contains(plain, "nothing here is live") || strings.Contains(plain, "enter dispatch") {
		t.Errorf("the compose footer promises a dispatch: %q", plain)
	}

	// The per-seat verbs, in view mode.
	m.key(key("esc"))
	for _, k := range []string{"c", "u", "x", "o", "a"} {
		m.st.Notice = ""
		m.key(key(k))
		if m.st.Notice != replayNotice {
			t.Errorf("%s: notice %q", k, m.st.Notice)
		}
	}
	// Reading is untouched.
	m.key(key("tab"))
	if m.st.Focus != 1 {
		t.Errorf("tab did not move focus: %d", m.st.Focus)
	}
	if log.n() != 0 {
		t.Errorf("%d spawns", log.n())
	}
}

// TestAReplayQuitsOnCtrlCAndQ: nothing to cancel, so both keys leave, and
// leaving writes nothing.
func TestAReplayQuitsOnCtrlCAndQ(t *testing.T) {
	countSpawns(t)
	for _, k := range []string{"ctrl+c", "q"} {
		m := replayFixture(t)
		play(m, 0, fixtureLen)
		if !m.st.Busy() {
			t.Fatal("the fixture should end with seats in flight")
		}
		_, cmd := m.key(key(k))
		if cmd == nil || !m.closingDone() {
			t.Errorf("%s did not quit the replay", k)
		}
	}
}

func (m *Model) closingDone() bool {
	_, closed := m.closingFacts()
	return closed
}

// TestAReplayNeverTouchesRoomJSON. The suite's HOME is a sandbox (TestMain),
// so the file is this package's own to plant: a sentinel room is written
// first, the replay is played through a finished turn and a teardown — the
// two places a live room saves — and the sentinel is unchanged.
func TestAReplayNeverTouchesRoomJSON(t *testing.T) {
	countSpawns(t)
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, sandboxHome) {
		t.Fatalf("RoomPath %s is not under the sandbox home %s", path, sandboxHome)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	sentinel := []byte("sentinel: a replay must not touch this\n")
	if err := os.WriteFile(path, sentinel, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(path) })

	m := replayFixture(t)
	play(m, 0, fixtureLen)
	if m.column(model.VendorCodex).Phase != PhaseDone {
		t.Fatal("the fixture's finished seat did not finish, so no save path was exercised")
	}
	m.key(key("ctrl+c"))
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(sentinel) {
		t.Errorf("room.json changed under a replay: %q (%v)", got, err)
	}
	if m.saveErr != nil {
		t.Errorf("a replay tried to save: %v", m.saveErr)
	}
}

// TestReplaySpeedMapsTheClock: the tick's wall time maps onto the recording's
// clock at the speed asked for, and the gap before each record shrinks by
// the same factor.
func TestReplaySpeedMapsTheClock(t *testing.T) {
	rec, err := readRecording(fixtureRecording)
	if err != nil {
		t.Fatal(err)
	}
	began := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	start := rec.started()
	if start.IsZero() {
		t.Fatal("the fixture's start did not parse")
	}
	r := &replayRun{rec: rec, speed: 2, began: began, start: start}
	if got := r.clock(began.Add(10 * time.Second)); !got.Equal(start.Add(20 * time.Second)) {
		t.Errorf("clock at 2x = %v, want %v", got, start.Add(20*time.Second))
	}
	if got := r.delay(0); got != 750*time.Millisecond {
		t.Errorf("first delay at 2x = %v, want 750ms (1500ms recorded)", got)
	}
	if got := r.delay(1); got != 300*time.Millisecond {
		t.Errorf("second delay at 2x = %v, want 300ms (600ms recorded)", got)
	}
	r.speed = 1
	if got := r.delay(0); got != 1500*time.Millisecond {
		t.Errorf("first delay at 1x = %v", got)
	}
	// A zero speed on the model is the original pace, never a stall.
	m := newReplayModel(Options{ReplaySpeed: 0}, rec, fixtureRecording)
	if m.replay.speed != 1 {
		t.Errorf("speed 0 became %v", m.replay.speed)
	}
	if m.replayNext() == nil {
		t.Error("a fresh replay armed nothing")
	}
	m.replay.i = fixtureLen
	if m.replayNext() != nil || !strings.Contains(m.st.Notice, "replay ended") {
		t.Errorf("the end of the file did not close: %q", m.st.Notice)
	}
}

// TestAReplayDrawsTheRecordedRoomNotThisMachine: the seats, their postures
// and the workspace come from the file. A replay on a machine with nothing
// installed draws the room that was recorded.
func TestAReplayDrawsTheRecordedRoomNotThisMachine(t *testing.T) {
	m := replayFixture(t)
	if len(m.st.Columns) != 3 || m.st.Columns[0].Label != "Claude Code" || m.st.Columns[0].Sandbox.Level != SandboxGated {
		t.Errorf("columns = %+v", m.st.Columns)
	}
	if m.st.Workspace != "~/code/example" || m.st.Home != "" {
		t.Errorf("workspace %q home %q", m.st.Workspace, m.st.Home)
	}
	if !m.st.Replay || !m.st.Write || m.st.GateOff {
		t.Errorf("state = replay %v write %v gateoff %v", m.st.Replay, m.st.Write, m.st.GateOff)
	}
	if !strings.Contains(m.st.Notice, "nothing here is live") {
		t.Errorf("opening notice = %q", m.st.Notice)
	}
}
