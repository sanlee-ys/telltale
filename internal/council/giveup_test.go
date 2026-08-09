package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The give-up key (`x`, §9.37 amended 2026-08-09), witnessed the way the race
// itself is: real temp repositories, the fake-session machinery, and
// assertions on which process died and what landed on the column — never on a
// helper's return value. The measurement behind every test here is the second
// live /arena: three seats landed, the fourth streamed for 26m40s, and the
// operator sat ~20 minutes because ctrl+c was the only exit and it cancels
// everything. The property under test is the act that measurement was
// missing: ONE racer dies, its column lands cancelled with its receipt, and
// the race — and the room seat behind the same vendor id — run on.

// recordedKill is the racerHandle fake: the property most of these tests end
// on IS the kill, and dispatch's real handles here are countSpawns' empty
// shells whose Kill needs a process that was never spawned. Planted over the
// map entries dispatch minted, which is the seam turnState.arenaHandles
// exists to be (racerHandle's own doc).
type recordedKill struct{ killed bool }

func (k *recordedKill) Kill() { k.killed = true }

// giveUpRace is arenaCursorRace with every one-shot racer's handle replaced by
// an observable fake, so a test can say not only that the right process died
// but that the wrong ones did not.
func giveUpRace(t *testing.T) (*Model, map[model.VendorID]*recordedKill, *killSession) {
	t.Helper()
	m, _, racer := arenaCursorRace(t)
	oneShots := map[model.VendorID]*recordedKill{}
	for v := range m.turn.arenaHandles {
		k := &recordedKill{}
		oneShots[v] = k
		m.turn.arenaHandles[v] = k
	}
	return m, oneShots, racer
}

// focusSeatOn points the room's focus at one vendor's column.
func focusSeatOn(t *testing.T, m *Model, v model.VendorID) {
	t.Helper()
	for i := range m.st.Columns {
		if m.st.Columns[i].Vendor == v {
			m.st.Focus = i
			return
		}
	}
	t.Fatalf("no column for %s", v)
}

// TestGiveUpKillsTheEphemeralRacerAndLandsTheColumnCancelled is the headline
// act end to end: x, y, and the stuck seat's racer is dead while its column
// lands with everything a finished seat gets — the cancelled phase paired
// with a rank, the note naming what happened, and the diff of a DIRTY tree
// committed onto the arena branch as the attempt's durable receipt.
func TestGiveUpKillsTheEphemeralRacerAndLandsTheColumnCancelled(t *testing.T) {
	m, _, racer := giveUpRace(t)
	tree := m.turn.arenaTrees[model.VendorCursor]
	if err := os.WriteFile(filepath.Join(tree, "half.txt"), []byte("partial\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	focusSeatOn(t, m, model.VendorCursor)

	m.key(key("x"))
	if m.giveUpPending != model.VendorCursor {
		t.Fatalf("x did not arm the focused racer: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "cursor") || !strings.Contains(m.st.Notice, "kills its racer") {
		t.Errorf("the question does not name the vendor and what y does: %q", m.st.Notice)
	}
	m.key(key("y"))

	if !racer.killed {
		t.Fatal("y did not kill the racer")
	}
	c := m.column(model.VendorCursor)
	if c.Phase != PhaseCancelled {
		t.Fatalf("phase = %v, want cancelled", c.Phase)
	}
	if !strings.Contains(c.Note, "given up after") || !strings.Contains(c.Note, "in the diff") {
		t.Errorf("the note does not name the give-up: %q", c.Note)
	}
	r := c.Arena
	if r == nil {
		t.Fatal("the given-up seat landed without an arena result — no diff was collected")
	}
	if r.Rank != 1 || r.Of != 4 {
		t.Errorf("rank = %d of %d — a give-up finished too, just not well; want 1 of 4", r.Rank, r.Of)
	}
	if !strings.Contains(r.Stat, "half.txt") {
		t.Errorf("the stopped attempt's work is not in the stat: %q", r.Stat)
	}
	if r.Commit == "" || r.CommitErr != "" {
		t.Fatalf("a dirty tree did not commit its receipt: commit %q, err %q", r.Commit, r.CommitErr)
	}
	if tip, _ := gitOut(m.st.Workspace, "rev-parse", r.Branch); tip != r.Commit {
		t.Errorf("%s = %q, want the receipt's %q", r.Branch, tip, r.Commit)
	}
	if c.ArenaInterim != nil {
		t.Error("the interim stat survived the landing — two answers on one column")
	}
	if m.turn == nil {
		t.Fatal("giving up on one seat ended a turn three seats are still racing")
	}
	if m.turn.live[model.VendorCursor] {
		t.Error("the given-up seat never left the turn's live set — the turn cannot end")
	}
}

// TestGiveUpOnACleanTreeLandsAMeasuredZero: a racer that wrote nothing lands
// with an empty stat and NO commit — the zero-diff ruling unchanged by who
// ended the attempt. Zero and absent stay different facts (§4a.1): the result
// exists, its stat is a measured nothing.
func TestGiveUpOnACleanTreeLandsAMeasuredZero(t *testing.T) {
	m, _, _ := giveUpRace(t)
	focusSeatOn(t, m, model.VendorCursor)

	m.key(key("x"))
	m.key(key("y"))

	r := m.column(model.VendorCursor).Arena
	if r == nil {
		t.Fatal("no arena result")
	}
	if r.Err != "" {
		t.Fatalf("collection failed: %q", r.Err)
	}
	if strings.TrimSpace(r.Stat) != "" {
		t.Errorf("a clean tree measured a diff: %q", r.Stat)
	}
	if r.Commit != "" || r.CommitErr != "" {
		t.Errorf("a zero-diff give-up produced a commit receipt: %q %q", r.Commit, r.CommitErr)
	}
}

// TestTheTurnEndsWhenTheOthersLandAfterAGiveUp is the whole point of the key:
// the live set drains through the same finish every other landing uses, so
// the turn is free to end the moment the remaining seats land — nobody sits
// twenty minutes behind a seat that was already given up on.
func TestTheTurnEndsWhenTheOthersLandAfterAGiveUp(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m, _, _ := giveUpRace(t)
	focusSeatOn(t, m, model.VendorCursor)

	m.key(key("x"))
	m.key(key("y"))
	if m.turn == nil {
		t.Fatal("the give-up itself ended the turn")
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity} {
		m.applyEvents([]runner.Event{{Vendor: v, Kind: runner.KindDone}})
	}

	if m.turn != nil {
		t.Fatal("every seat has landed and the turn is still in flight — the hostage the key exists to free")
	}
	if m.st.Mode != ModeComposing {
		t.Error("the ended turn did not hand the composer back")
	}
}

// TestGiveUpKillsTheRightOneShotHandle: the kill is keyed by vendor, so one
// seat's give-up reaches exactly one process — the other racers' handles and
// the ephemeral racer all survive, and their columns keep racing.
func TestGiveUpKillsTheRightOneShotHandle(t *testing.T) {
	m, oneShots, racer := giveUpRace(t)
	focusSeatOn(t, m, model.VendorCodex)

	m.key(key("x"))
	m.key(key("y"))

	if !oneShots[model.VendorCodex].killed {
		t.Fatal("codex's give-up did not kill codex's racer")
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorAntigravity} {
		if oneShots[v].killed {
			t.Errorf("codex's give-up killed %s's racer", v)
		}
		if c := m.column(v); c.Phase != PhaseStreaming && c.Phase != PhaseWaiting {
			t.Errorf("%s's column stopped racing: %v", v, c.Phase)
		}
	}
	if racer.killed {
		t.Error("codex's give-up killed the cursor seat's ephemeral racer")
	}
	if c := m.column(model.VendorCodex); c.Phase != PhaseCancelled {
		t.Errorf("codex's column = %v, want cancelled", c.Phase)
	}
}

// TestARacerGiveUpLeavesTheRoomProcessAlive: two processes wear one vendor id
// during a race (applyEvents' attribution rule), and the give-up must land on
// the racer's side of that split — the room's idle seat survives, stays
// registered, and the racer's own exit echo neither re-ranks the race nor
// touches it.
func TestARacerGiveUpLeavesTheRoomProcessAlive(t *testing.T) {
	m, _, racer := giveUpRace(t)
	roomProc := &killSession{}
	m.procs[model.VendorCursor] = &seatProc{sess: roomProc, dir: m.st.Workspace}
	focusSeatOn(t, m, model.VendorCursor)

	m.key(key("x"))
	m.key(key("y"))

	if !racer.killed {
		t.Fatal("the racer survived its own give-up")
	}
	if roomProc.killed {
		t.Fatal("the give-up killed the ROOM's process — the conversation died with the racer")
	}
	if _, ok := m.procs[model.VendorCursor]; !ok {
		t.Error("the room's process was dropped by a give-up that never addressed it")
	}

	// The killed racer's exit arrives later, wearing the shared vendor id.
	rank := m.column(model.VendorCursor).Arena.Rank
	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindDone}})
	if m.column(model.VendorCursor).Arena.Rank != rank {
		t.Error("the kill's own exit re-ranked the race")
	}
	if m.turn == nil {
		t.Error("the exit echo tore down a turn three seats are still racing")
	}
	if roomProc.killed {
		t.Error("the exit echo killed the room's process")
	}
}

// TestGiveUpRefusalsEachNameTheirReason: three different facts, three
// different sentences (askUndoSeat's rule) — and the ordinary-turn refusal
// says out loud that ctrl+c remains the whole-turn act, because a key that is
// arena-only must name the act that is not.
func TestGiveUpRefusalsEachNameTheirReason(t *testing.T) {
	t.Run("no turn in flight", func(t *testing.T) {
		m := flowRoom(t, true)
		m.st.Mode = ModeViewing
		m.key(key("x"))
		if m.giveUpPending != "" {
			t.Fatal("x armed with no turn in flight")
		}
		if !strings.Contains(m.st.Notice, "no turn is in flight") {
			t.Errorf("refusal: %q", m.st.Notice)
		}
	})
	t.Run("an ordinary turn is not a race", func(t *testing.T) {
		log := countSpawns(t)
		m := flowRoom(t, true)
		m.st.Draft = "an ordinary brief"
		m.dispatch()
		if m.turn == nil || m.turn.arena {
			t.Fatalf("fixture: want an ordinary turn in flight (%d spawns)", log.n())
		}
		m.key(key("x"))
		if m.giveUpPending != "" {
			t.Fatal("x armed on an ordinary turn's seat")
		}
		if !strings.Contains(m.st.Notice, "not a race") || !strings.Contains(m.st.Notice, "ctrl+c") {
			t.Errorf("the refusal does not say the key is arena-only and name the whole-turn act: %q", m.st.Notice)
		}
	})
	t.Run("the seat already landed", func(t *testing.T) {
		m, _, _ := giveUpRace(t)
		m.applyEvents([]runner.Event{
			{Vendor: model.VendorCursor, Kind: runner.KindText, Text: "done. "},
			{Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true},
		})
		focusSeatOn(t, m, model.VendorCursor)
		m.key(key("x"))
		if m.giveUpPending != "" {
			t.Fatal("x armed on a seat that already landed")
		}
		if !strings.Contains(m.st.Notice, "already landed") {
			t.Errorf("refusal: %q", m.st.Notice)
		}
	})
}

// TestGiveUpGateKeepsAndCancels: n is a decision, a stray key is not, and
// neither kills anything — clearGateKey's rule, priced against a process.
func TestGiveUpGateKeepsAndCancels(t *testing.T) {
	for _, tc := range []struct {
		name, press, notice string
	}{
		{"n keeps the racer", "n", "kept"},
		{"a stray key cancels", "j", "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, racer := giveUpRace(t)
			focusSeatOn(t, m, model.VendorCursor)
			m.key(key("x"))
			if m.giveUpPending == "" {
				t.Fatalf("x did not arm: %q", m.st.Notice)
			}
			m.key(key(tc.press))
			if m.giveUpPending != "" {
				t.Error("the gate is still pending after an answer")
			}
			if racer.killed {
				t.Error("a declined give-up killed the racer anyway")
			}
			if !m.turn.live[model.VendorCursor] {
				t.Error("a declined give-up retired the seat")
			}
			if !strings.Contains(m.st.Notice, tc.notice) {
				t.Errorf("notice %q does not contain %q", m.st.Notice, tc.notice)
			}
		})
	}
}

// TestGiveUpRefusesWhenTheSeatLandsUnderTheQuestion: events drain between the
// card arming and the y, so the seat can land while the operator decides. The
// y then kills nothing and re-finishes nothing — the settled result stands.
func TestGiveUpRefusesWhenTheSeatLandsUnderTheQuestion(t *testing.T) {
	m, _, _ := giveUpRace(t)
	focusSeatOn(t, m, model.VendorCursor)

	m.key(key("x"))
	if m.giveUpPending == "" {
		t.Fatal("x did not arm")
	}
	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true}})
	c := m.column(model.VendorCursor)
	rank := c.Arena.Rank
	m.key(key("y"))

	if c.Phase != PhaseDone {
		t.Errorf("phase = %v — the stale y re-labelled a seat that landed on its own", c.Phase)
	}
	if c.Arena.Rank != rank {
		t.Error("the stale y re-ranked the race")
	}
	if !strings.Contains(m.st.Notice, "landed while the question was up") {
		t.Errorf("the refusal does not name what happened: %q", m.st.Notice)
	}
}

// TestGiveUpIsViewModeOnly keeps the contract q, f, c and u already keep: in
// compose, x is the letter x.
func TestGiveUpIsViewModeOnly(t *testing.T) {
	m := flowRoom(t, true)
	m.st.Mode = ModeComposing

	m.key(key("x"))

	if m.giveUpPending != "" {
		t.Errorf("x armed a give-up while composing: %s", m.giveUpPending)
	}
	if !strings.Contains(m.st.Draft, "x") {
		t.Errorf("x was swallowed instead of typed: draft = %q", m.st.Draft)
	}
}
