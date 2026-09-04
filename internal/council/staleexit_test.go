package council

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The stale exit (design.md §9.56, measured 2026-09-03). A persistent seat's
// replaced process ends after the seat's new process has taken the next
// brief. The live room attributes that exit to the old process by a liveness
// test on the current one; the recording carries only the vendor name, so
// the first real replay marked every persistent seat failed at every
// dispatch. Two fixes, both pinned here: the recorder drops the exit at the
// guard the room already trusts, and the reader recovers the attribution for
// a file recorded before that guard, by the order a process's events land in.

// fixtureStaleExit carries one stale exit on turn 1 (a `done` before the
// seat's own session line, text and end of turn), a nameless tool result,
// and a REAL death on turn 2 (a `done` nothing follows).
const fixtureStaleExit = "testdata/replay/stale-exit.jsonl"

// TestTheRecorderDropsAnExitTheRoomDiscards is the recorder's half: the exit
// the stale-exit guard ignores never reaches the file, and the events the
// room did apply are all there in order.
func TestTheRecorderDropsAnExitTheRoomDiscards(t *testing.T) {
	countSpawns(t)
	path := filepath.Join(t.TempDir(), "run.jsonl")
	rec, err := openRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	m := crewRoom(t)
	m.rec = rec
	rec.room(m.st)

	send(t, m, "@claude go")
	// The seat's current process is alive; the exit that lands next is a
	// predecessor's, which is exactly what the room's guard reads.
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: &fakeSession{alive: true}, sent: 1}
	m.Update(eventBatchMsg{events: []runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindDone, ExitCode: 1},
	}})
	c := m.column(model.VendorClaude)
	if c.Phase != PhaseStreaming && c.Phase != PhaseWaiting {
		t.Fatalf("the live room failed the turn on a stale exit: %v %q", c.Phase, c.Note)
	}
	m.Update(eventBatchMsg{events: []runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "one risk\n"},
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true},
	}})
	if c.Phase != PhaseDone {
		t.Fatalf("the turn did not end on the vendor's own line: %v", c.Phase)
	}
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}

	got, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, l := range got.lines {
		kinds = append(kinds, l.Kind+":"+l.Event)
	}
	want := []string{"dispatch:", "event:text", "event:meta"}
	if strings.Join(kinds, " ") != strings.Join(want, " ") {
		t.Fatalf("records = %v, want %v — the stale exit must not be in the file", kinds, want)
	}
	if got.staleExits() != 0 {
		t.Errorf("the reader found %d stale exits in a file the guarded recorder wrote", got.staleExits())
	}
}

// TestAReplaySkipsAStaleExitAndAppliesARealOne is the reader's half, over a
// file recorded the way the first real one was: the seat is done at the
// vendor's own end of turn, with the elapsed time the vendor took, and a
// death nothing follows is still a death.
func TestAReplaySkipsAStaleExitAndAppliesARealOne(t *testing.T) {
	countSpawns(t)
	rec, err := readRecording(fixtureStaleExit)
	if err != nil {
		t.Fatal(err)
	}
	if rec.staleExits() != 1 || !rec.lines[1].stale {
		t.Fatalf("the reader marked %d stale exits, want exactly the one on turn 1", rec.staleExits())
	}
	m := newReplayModel(Options{}, rec, fixtureStaleExit)
	m.st.Width, m.st.Height = 120, 24

	// Turn 1, through the stale exit and up to the end of the turn: records
	// 0..8 are the first dispatch and everything before the second.
	if rec.lines[9].Kind != "dispatch" || rec.lines[9].Turn != 2 {
		t.Fatalf("record 9 is %s turn %d; the bounds below expect the second dispatch", rec.lines[9].Kind, rec.lines[9].Turn)
	}
	play(m, 0, 9)
	claude := m.column(model.VendorClaude)
	if claude.Phase != PhaseDone || claude.Elapsed != 2*time.Second {
		t.Errorf("claude = %v after %v, note %q; want done after 2s (dispatch to the vendor's own end of turn)", claude.Phase, claude.Elapsed, claude.Note)
	}
	if strings.Contains(claude.Note, "ended mid-turn") {
		t.Errorf("the stale exit was applied: %q", claude.Note)
	}
	if claude.CostUSD == nil || *claude.CostUSD != 0.0456 {
		t.Errorf("claude cost = %v, want the recorded figure", claude.CostUSD)
	}
	if !strings.Contains(claude.Body, "Two agents pushing") {
		t.Errorf("claude's reply was not drawn: %q", claude.Body)
	}
	codex := m.column(model.VendorCodex)
	if codex.Phase != PhaseDone || codex.Elapsed != 3*time.Second {
		t.Errorf("codex = %v after %v, want done after 3s", codex.Phase, codex.Elapsed)
	}

	// Turn 2: the same seat's process dies for real, and nothing follows.
	play(m, 9, len(rec.lines))
	if claude.Phase != PhaseFailed || !strings.Contains(claude.Note, "ended mid-turn") {
		t.Errorf("a real death replays as %v %q, want failed with the mid-turn note", claude.Phase, claude.Note)
	}
	if claude.Elapsed != 2*time.Second {
		t.Errorf("the death's elapsed = %v, want 2s", claude.Elapsed)
	}
}

// TestReplayCheckCountsStaleExitsAndUnnamedResults: the review tool says how
// many exits the replay will skip, and folds the nameless tool results into
// one count per seat instead of one bare vendor word per line.
func TestReplayCheckCountsStaleExitsAndUnnamedResults(t *testing.T) {
	var out bytes.Buffer
	if err := ReplayCheck(fixtureStaleExit, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"seats: claude (Claude Code), codex (Codex)",
		"stale exits: 1",
		"claude  Read: README.md",
		"claude  1 unnamed tool result",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("replay-check does not say %q:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimSpace(line) == "claude" {
			t.Errorf("replay-check printed a bare vendor word:\n%s", got)
		}
	}
}

// TestAnUnseatedSeatIsNeitherRestoredNorRebuilt (measured 2026-09-03: a room
// opened with four --vendor seats rebuilt five and recorded five). A seat
// --vendor left out is still a column, but it takes no turn, so a saved
// thread for it is not a restored seat and the rebuild does not launch it.
func TestAnUnseatedSeatIsNeitherRestoredNorRebuilt(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}}
	room := SavedRoom{
		Workspace: m.st.Workspace,
		Posture:   "write",
		Turn:      7,
		Sessions: map[model.VendorID]string{
			model.VendorClaude: "sess-claude",
			model.VendorCursor: "sess-cursor",
		},
		SavedAt: time.Now().Add(-time.Hour),
	}
	m.reattach(Reattachment{Path: "/home/dev/.telltale/council/abc.json", Room: room})

	if !m.column(model.VendorClaude).Restored {
		t.Error("the seated seat with a saved thread is not marked restored")
	}
	if m.column(model.VendorCursor).Restored {
		t.Error("a seat --vendor left out is marked restored")
	}
	if !strings.Contains(m.st.Notice, "1/2 seats restored") {
		t.Errorf("the notice counts an unseated seat: %q", m.st.Notice)
	}
	if got := m.rebuildable(); len(got) != 1 || got[0] != model.VendorClaude {
		t.Errorf("rebuildable = %v, want claude alone", got)
	}
	m.startRebuild()
	if log.n() != 1 {
		t.Errorf("%d seats launched, want 1: %+v", log.n(), log.specs)
	}
	if _, ok := m.procs[model.VendorCursor]; ok {
		t.Error("the unseated seat got a process it can never take a turn on")
	}
}

// TestTheRoomLineListsOnlyTheSeatsTheRoomDraws: the recording's first line
// is the room the operator had, not every column the binary holds.
func TestTheRoomLineListsOnlyTheSeatsTheRoomDraws(t *testing.T) {
	countSpawns(t)
	path := filepath.Join(t.TempDir(), "run.jsonl")
	rec, err := openRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	m := crewRoom(t)
	m.st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}}
	rec.room(m.st)
	if err := rec.close(); err != nil {
		t.Fatal(err)
	}
	got, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	var seats []string
	for _, s := range got.room.Seats {
		seats = append(seats, s.Vendor)
	}
	if strings.Join(seats, ",") != "claude,codex" {
		t.Errorf("room line seats = %v, want the two --vendor named", seats)
	}
	if strings.Join(got.room.SeatsOnly, ",") != "claude,codex" {
		t.Errorf("seats_only = %v", got.room.SeatsOnly)
	}
}
