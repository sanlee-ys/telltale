package council

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The race's worktree setup, off the render loop (§9.37, amended 2026-08-17).
//
// The defect these pin down is the operator's: parallel sessions against one
// repository, a lock held by another of them, and a room that drew nothing and
// read no keys for as long as git blocked — with ctrl+c, the only act that
// could have ended it, sitting unread in a queue nobody was draining because
// the drainer was inside `git worktree add`.
//
// What is asserted here is therefore three properties and not one: the room is
// still alive DURING the setup, the frame says what is happening without
// claiming how far along it is, and every ending — a deadline, a git refusal, a
// ctrl+c — hands the room back rather than keeping it.

// pumpArenaSetup drives a running setup to completion the way Update would:
// run the command, hand the message back, repeat until the prep is gone.
func pumpArenaSetup(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	for m.arenaPrep != nil {
		if cmd == nil {
			t.Fatal("the setup stopped handing back a command with a prep still running")
		}
		raw := cmd()
		msg, ok := raw.(arenaSetupMsg)
		if !ok {
			t.Fatalf("the setup produced %T, not a setup message", raw)
		}
		cmd = m.applyArenaSetup(msg)
	}
}

// raceNow dispatches an /arena draft and drives its setup to completion, so a
// test can assert against a room whose seats have actually spawned.
//
// It exists because dispatch no longer finishes the job: it starts the setup
// and returns, and the turn is born several messages later in applyArenaSetup.
// A test that called dispatch alone would be asserting against a room that has
// prepared nothing — a real state, just not the one those tests are about.
func raceNow(t *testing.T, m *Model) {
	t.Helper()
	pumpArenaSetup(t, m, m.dispatch())
}

// arenaRoom is a write room seated in a real git repository, ready to race.
func arenaRoom(t *testing.T) *Model {
	t.Helper()
	m := flowRoom(t, true)
	m.st.Workspace = gitRepo(t)
	return m
}

// TestARaceLeavesTheRoomDrawingAndReadingKeys is the whole point of the change:
// between the enter and the first spawn, the room is a room.
func TestARaceLeavesTheRoomDrawingAndReadingKeys(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	m.st.Width, m.st.Height = 120, 24
	m.st.Draft = "/arena add a marker file"

	cmd := m.dispatch()
	if cmd == nil {
		t.Fatal("dispatch returned no command — nothing would ever read the setup")
	}
	if m.turn != nil {
		t.Fatal("a seat spawned before its worktree existed")
	}
	if m.arenaPrep == nil {
		t.Fatal("no setup is running and no turn started — the race went nowhere")
	}
	// The frame says what is happening. This is the assertion the frozen room
	// could not have passed: it had no state to draw and no chance to draw it.
	if m.st.ArenaSetup == "" {
		t.Error("the room is preparing a race and says nothing about it")
	}
	if got := render(m.st); !strings.Contains(got, m.st.ArenaSetup) {
		t.Errorf("the frame does not name the step %q", m.st.ArenaSetup)
	}
	// And it still reads keys. `?` is the cheapest proof there is: it belongs to
	// no gate, needs no turn, and changes state the test can see.
	if _, _ = m.key(key("?")); m.st.Help == HelpClosed {
		t.Error("a keystroke went unread while the setup ran — the freeze, back")
	}
	// The brief was accepted: the composer is empty and the room is watching.
	if m.st.Draft != "" {
		t.Errorf("the draft survived the enter that sent it: %q", m.st.Draft)
	}
	if m.st.Mode != ModeViewing {
		t.Errorf("mode = %v, want viewing while the race is prepared", m.st.Mode)
	}

	pumpArenaSetup(t, m, cmd)
	if m.turn == nil {
		t.Fatal("the setup finished and no turn started")
	}
	if !m.turn.arena {
		t.Error("the turn that started is not a race")
	}
	if m.st.ArenaSetup != "" {
		t.Errorf("the step line outlived the setup: %q", m.st.ArenaSetup)
	}
	if len(m.turn.arenaTrees) == 0 {
		t.Error("the race started with no worktree — nothing was prepared")
	}
}

// TestSetupStepsNameTheWorkAndNeverTheProgress is the honesty rule in its own
// test: every step is a sentence about what is happening, and none of them is a
// number about how far along it is.
//
// The digit check is the sharp end. council cannot measure how long a checkout
// takes, so a percentage, a "2 of 4" or an elapsed figure would all be values it
// invented (§4a.1) — and each of them is the obvious thing for a later change to
// add to a line that already looks like a progress line.
func TestSetupStepsNameTheWorkAndNeverTheProgress(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex}

	var steps []string
	_, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 1, seats,
		func(s string) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("seat errors on a clean repo: %v", seatErr)
	}
	if len(trees) != len(seats) {
		t.Fatalf("%d trees for %d seats", len(trees), len(seats))
	}

	want := []string{
		arenaStepBase, arenaStepNumber, arenaStepPlan,
		arenaStepTree(model.VendorClaude), arenaStepTree(model.VendorCodex),
	}
	if len(steps) != len(want) {
		t.Fatalf("steps = %v, want %v", steps, want)
	}
	for i, w := range want {
		if steps[i] != w {
			t.Errorf("step %d = %q, want %q", i, steps[i], w)
		}
	}
	for _, s := range steps {
		if strings.ContainsAny(s, "0123456789%") {
			t.Errorf("step %q carries a number — council cannot measure how far along a checkout is", s)
		}
		if strings.TrimSpace(s) == "" {
			t.Error("a step reported no words at all")
		}
	}
	if !strings.Contains(steps[4], "codex") {
		t.Errorf("the per-seat step does not name its seat: %q", steps[4])
	}
}

// TestWorktreesAreAddedOneAtATime measures the serial rule rather than assuming
// it: when the setup announces seat N's add, seat N-1's tree is already on disk.
//
// That is only true if the adds do not overlap. It is pinned because
// parallelising them is the obvious next thought and is wrong for a specific
// reason: `git worktree add` writes the repository's own refs and administrative
// files, so N at once contend for exactly the lock this whole change exists to
// survive.
func TestWorktreesAreAddedOneAtATime(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity}

	var done []model.VendorID
	_, _, _, _, _, err := arenaSetup(context.Background(), ws, 1, seats, func(s string) {
		for _, v := range seats {
			if s != arenaStepTree(v) {
				continue
			}
			for _, earlier := range done {
				if _, serr := os.Stat(arenaTree(ws, 1, earlier)); serr != nil {
					t.Errorf("%s's add began while %s's tree did not exist yet — the adds are overlapping", v, earlier)
				}
			}
			done = append(done, v)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(done) != len(seats) {
		t.Fatalf("%d seats announced, want %d", len(done), len(seats))
	}
}

// TestOnlySetupGitCallsCarryADeadline pins the split the two helpers exist to
// make.
//
// The structural half is the signature: gitOut takes no context and can never
// be handed one, so every git call outside the setup path is un-deadlined by
// construction. The behavioural half is here — an ended context really does
// stop gitOutCtx, and the very same command through gitOut really does run.
func TestOnlySetupGitCallsCarryADeadline(t *testing.T) {
	ws := gitRepo(t)

	dead, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if out, err := gitOutCtx(dead, ws, "rev-parse", "HEAD"); err == nil {
		t.Errorf("an expired deadline still ran git and returned %q", out)
	}
	if _, err := gitOut(ws, "rev-parse", "HEAD"); err != nil {
		t.Errorf("the un-deadlined helper failed on the same command: %v", err)
	}
}

// TestADeadlineStopsTheWholeSetupAndNamesTheStep: a clock that ran out is a
// fact about the room's patience, not about a seat — so it ends the setup
// wholesale and the sentence says where it was.
func TestADeadlineStopsTheWholeSetupAndNamesTheStep(t *testing.T) {
	ws := gitRepo(t)
	dead, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, _, trees, _, seatErr, err := arenaSetup(dead, ws, 1, []model.VendorID{model.VendorCodex}, nil)
	if err == nil {
		t.Fatal("an expired setup reported success")
	}
	if len(trees) != 0 || len(seatErr) != 0 {
		t.Errorf("the deadline was recorded per seat (trees=%v seatErr=%v), want a wholesale stop", trees, seatErr)
	}
	if !strings.Contains(err.Error(), arenaStepBase) {
		t.Errorf("the failure does not name the step it stopped on: %q", err)
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Errorf("the failure does not say the deadline expired: %q", err)
	}
	if !strings.Contains(err.Error(), dur(arenaSetupDeadline)) {
		t.Errorf("the failure does not say how long the room waited: %q", err)
	}
}

// TestAFailedSetupHandsTheRoomBack: the notice carries the step and git's own
// sentence, the brief returns to the composer, and nothing is left in flight.
//
// The last clause is the one that matters most. A setup that failed and left
// arenaPrep set would wedge the room exactly as the freeze did — every dispatch
// refused, the footer promising a ctrl+c that has nothing to stop.
func TestAFailedSetupHandsTheRoomBack(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	// A workspace that is not a repository: the failure arrives on the first
	// step, wholesale, exactly as a deadline would.
	m.st.Workspace = t.TempDir()
	m.st.Draft = "/arena add a marker file"

	pumpArenaSetup(t, m, m.dispatch())

	if m.turn != nil {
		t.Fatal("a seat spawned for a setup that failed")
	}
	if m.arenaPrep != nil {
		t.Fatal("the failed setup is still marked as running — the room is wedged")
	}
	if m.st.ArenaSetup != "" {
		t.Errorf("the step line outlived the failure: %q", m.st.ArenaSetup)
	}
	if !strings.HasPrefix(m.st.Notice, "arena: ") {
		t.Errorf("the failure did not land on the room's arena notice: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, arenaStepBase) {
		t.Errorf("the notice does not name the step: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "fatal:") {
		t.Errorf("the notice does not carry git's own sentence: %q", m.st.Notice)
	}
	if m.st.Draft != "/arena add a marker file" {
		t.Errorf("the brief did not come back: %q", m.st.Draft)
	}
	if m.st.Mode != ModeComposing {
		t.Errorf("mode = %v, want composing — the operator has to be able to act", m.st.Mode)
	}
	// And the room really can act: the same brief dispatches again.
	if cmd := m.dispatch(); cmd == nil && m.arenaPrep == nil && m.turn == nil {
		t.Error("the room refused the retry — it was handed back in name only")
	}
}

// TestASetupNobodyCouldRaceStillReturnsTheBrief: the setup itself succeeded and
// every seat's worktree failed on its own, so the dispatch spawned nothing.
//
// The columns carry each seat's reason; what this pins is the composer. Inline,
// this path never lost the brief — dispatch returned before it reached the line
// that clears it — and the split put the clear an entire setup earlier, so the
// text has to be handed back deliberately.
func TestASetupNobodyCouldRaceStillReturnsTheBrief(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	m.st.Draft = "/arena add a marker file"
	// Every seat's tree name is squatted by a non-empty directory, which is the
	// residual collision `git worktree add` refuses per seat.
	for i := range m.st.Columns {
		squat := arenaTree(m.st.Workspace, 1, m.st.Columns[i].Vendor)
		if err := os.MkdirAll(squat, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(squat+"/stale.txt", []byte("old room\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	pumpArenaSetup(t, m, m.dispatch())

	if m.turn != nil {
		t.Fatal("a turn started with no racer")
	}
	if m.st.Draft != "/arena add a marker file" {
		t.Errorf("the brief did not come back: %q", m.st.Draft)
	}
	if m.st.Mode != ModeComposing {
		t.Errorf("mode = %v, want composing", m.st.Mode)
	}
}

// TestCtrlCStopsASetupRatherThanTheRoom is the key the freeze ate.
//
// It stops the setup in whichever mode the room is in, and it must NOT quit:
// view mode's ctrl+c quits when no turn is running, and a setup is not a turn.
// The leftovers are named rather than removed — §9.37 keeps worktrees until the
// user deletes them, and a cancel is the worst moment to start deciding for
// them.
func TestCtrlCStopsASetupRatherThanTheRoom(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	m.st.Draft = "/arena add a marker file"
	if cmd := m.dispatch(); cmd == nil {
		t.Fatal("the setup did not start")
	}
	if m.arenaPrep == nil {
		t.Fatal("no setup to stop")
	}

	_, cmd := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Error("ctrl+c during a setup returned a command — the room was quitting, not stopping")
	}
	if m.arenaPrep != nil {
		t.Fatal("the setup is still running after ctrl+c")
	}
	if m.st.ArenaSetup != "" {
		t.Errorf("the step line outlived the stop: %q", m.st.ArenaSetup)
	}
	if m.st.Draft != "/arena add a marker file" {
		t.Errorf("the brief did not come back: %q", m.st.Draft)
	}
	if m.st.Mode != ModeComposing {
		t.Errorf("mode = %v, want composing", m.st.Mode)
	}
	if !strings.Contains(m.st.Notice, "kept") {
		t.Errorf("the stop does not say what was left on disk: %q", m.st.Notice)
	}
}

// TestAStoppedSetupsMessagesAreDropped: the worker keeps writing after a stop —
// it is a goroutine, and it finishes what it was doing — so its messages have to
// be droppable by comparison rather than by hoping the timing worked out.
func TestAStoppedSetupsMessagesAreDropped(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	m.st.Draft = "/arena add a marker file"
	m.dispatch()
	stale := m.arenaPrep.id
	m.stopArenaSetup()

	if cmd := m.applyArenaSetup(arenaSetupMsg{prep: stale, step: "preparing worktree for codex"}); cmd != nil {
		t.Error("a stopped setup's step re-armed the reader")
	}
	if m.st.ArenaSetup != "" {
		t.Errorf("a stopped setup's step was drawn: %q", m.st.ArenaSetup)
	}
	if cmd := m.applyArenaSetup(arenaSetupMsg{prep: stale, done: &arenaSetupResult{}}); cmd != nil {
		t.Error("a stopped setup's result started a turn")
	}
	if m.turn != nil {
		t.Fatal("a turn started from a setup the operator stopped")
	}
}

// TestTheSetupFrameSaysWhatAndNotHowFar renders the line a person actually
// looks at, and asserts the honesty rule on the rendered string rather than on
// the state behind it — this repo tests the frame (CLAUDE.md).
func TestTheSetupFrameSaysWhatAndNotHowFar(t *testing.T) {
	st := room()
	st.ArenaSetup = arenaStepTree(model.VendorCodex)
	golden(t, "arena-setup", render(st))

	got := render(st)
	if !strings.Contains(got, "preparing worktree for codex") {
		t.Error("the frame does not name the step")
	}
	if !strings.Contains(got, "ctrl+c") {
		t.Error("the frame does not offer the key that stops it")
	}
	if strings.Contains(got, "%") {
		t.Error("the frame drew a percentage — council did not measure one")
	}
	// The words survive --ascii, because the words are the signal and the
	// spinner beside them is only the second one.
	if a := Render(st, PlainStyles(), GlyphsFor(true)); !strings.Contains(a, "preparing worktree for codex") {
		t.Error("the ascii frame lost the step")
	}
}

// TestTheSetupSpinnerMovesWhileGitWorks: the words alone cannot separate a room
// waiting on a slow checkout from a room that has stopped, because a step that
// takes a minute prints the same sentence throughout. The moving cell is that
// difference, and it advances on the tick like every other one.
func TestTheSetupSpinnerMovesWhileGitWorks(t *testing.T) {
	m := arenaRoom(t)
	m.st.ArenaSetup = arenaStepTree(model.VendorCodex)
	before := m.st.Spinner
	m.Update(spinMsg(time.Now()))
	if m.st.Spinner == before {
		t.Error("the spinner stood still while a setup ran — a working room draws as a frozen one")
	}

	// And it stops when the setup does: a motionless room is the honest render
	// of a room where nothing is happening.
	m.st.ArenaSetup = ""
	still := m.st.Spinner
	m.Update(spinMsg(time.Now()))
	if m.st.Spinner != still {
		t.Error("the spinner kept moving with nothing running")
	}
}

// TestASecondRaceIsRefusedWhileOneIsBeingPrepared: two setups against one
// repository would cut names from the same ref scan, and the second would
// collide with trees the first had not finished adding.
func TestASecondRaceIsRefusedWhileOneIsBeingPrepared(t *testing.T) {
	countSpawns(t)
	m := arenaRoom(t)
	m.st.Draft = "/arena add a marker file"
	m.dispatch()
	first := m.arenaPrep
	if first == nil {
		t.Fatal("the first setup did not start")
	}

	m.st.Draft = "/arena and another one"
	if cmd := m.dispatch(); cmd != nil {
		t.Error("a second race dispatched while the first was being prepared")
	}
	if m.arenaPrep != first {
		t.Error("the second dispatch replaced the running setup")
	}
	if !strings.Contains(m.st.Notice, "ctrl+c") {
		t.Errorf("the refusal does not name the key that ends the wait: %q", m.st.Notice)
	}

	// And the refusal is ON SCREEN. A notice raised while a setup runs is the
	// room answering a key the operator just pressed, so the step line carries
	// it rather than swallowing it — silence in reply to a keystroke is the
	// exact fault the setup moved off the loop to fix.
	st := room()
	st.ArenaSetup, st.Notice = arenaStepTree(model.VendorCodex), m.st.Notice
	got := render(st)
	if !strings.Contains(got, "being prepared") {
		t.Error("the refusal never reached the frame")
	}
	if !strings.Contains(got, "preparing worktree for codex") {
		t.Error("the notice hid the step the room is on")
	}
}
