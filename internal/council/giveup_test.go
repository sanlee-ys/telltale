package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	race := m.race()
	for v := range race.arenaHandles {
		k := &recordedKill{}
		oneShots[v] = k
		race.arenaHandles[v] = k
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
	tree := m.race().arenaTrees[model.VendorCursor]
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
	if !m.anyInFlight() {
		t.Fatal("giving up on one seat ended a turn three seats are still racing")
	}
	if m.turnOf(model.VendorCursor) != nil {
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
	if !m.anyInFlight() {
		t.Fatal("the give-up itself ended the turn")
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity} {
		m.applyEvents([]runner.Event{{Vendor: v, Kind: runner.KindDone}})
	}

	if m.anyInFlight() {
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
	// Derived rather than named: which seats race as one-shot processes is
	// the registry's claim, not this test's. Codex was the witness until
	// 2026-09-02, when it became a Conversational seat that races on a
	// throwaway session like cursor (§9.54); the property is the same for
	// whichever one-shot racer the room still has.
	cut := oneOf(t, oneShots, "one-shot racer")
	focusSeatOn(t, m, cut)

	m.key(key("x"))
	m.key(key("y"))

	if !oneShots[cut].killed {
		t.Fatalf("%s's give-up did not kill %s's racer", cut, cut)
	}
	for v, k := range oneShots {
		if v == cut {
			continue
		}
		if k.killed {
			t.Errorf("%s's give-up killed %s's racer", cut, v)
		}
		if c := m.column(v); c.Phase != PhaseStreaming && c.Phase != PhaseWaiting {
			t.Errorf("%s's column stopped racing: %v", v, c.Phase)
		}
	}
	if racer.killed {
		t.Errorf("%s's give-up killed an ephemeral racer", cut)
	}
	if c := m.column(cut); c.Phase != PhaseCancelled {
		t.Errorf("%s's column = %v, want cancelled", cut, c.Phase)
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
	if !m.anyInFlight() {
		t.Error("the exit echo tore down a turn three seats are still racing")
	}
	if roomProc.killed {
		t.Error("the exit echo killed the room's process")
	}
}

// TestGiveUpRefusalsEachNameTheirReason: three different facts, three
// different sentences (askUndoSeat's rule).
//
// The set changed on 2026-08-17. "This turn is not a race" is gone, because
// the key is no longer arena-only, and the refusal it left behind is the one
// that took its place: a turn ctrl+c is already cancelling has no per-seat act
// left to offer, since every seat is going anyway.
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
	t.Run("ctrl+c is already stopping every seat", func(t *testing.T) {
		m, _, _ := giveUpRace(t)
		focusSeatOn(t, m, model.VendorCursor)
		markCancelling(m, model.VendorCursor)
		m.key(key("x"))
		if m.giveUpPending != "" {
			t.Fatal("x armed a per-seat act over a whole-turn cancel already in progress")
		}
		if !strings.Contains(m.st.Notice, "ctrl+c") || !strings.Contains(m.st.Notice, "stopping") {
			t.Errorf("the refusal does not name the act already running: %q", m.st.Notice)
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
			if m.turnOf(model.VendorCursor) == nil {
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

// ── the ordinary turn (§9.37, amended 2026-08-17) ─────────────────────────
//
// The owner reversed the "an ordinary turn's seats share one fate by design"
// line on 2026-08-17: it was a four-seat-era position, and the five-seat room's
// most probable live failure is one stalled vendor on an @all turn. Everything
// below witnesses the same properties the race tests above do — which process
// was stopped, what landed on the column, what the OTHER seats kept — with the
// one new axis the ordinary turn adds: a batch seat is killed and a persistent
// seat is interrupted, and the two must not be confused for each other.
//
// No test here spawns a vendor. countSpawns stubs all three spawn vars and the
// handles are planted fakes, per CLAUDE.md's council-test rule.

// ordinaryTurn is a four-seat room with an ORDINARY brief in flight, every
// one-shot handle replaced by an observable fake, and the persistent seats'
// sessions replaced by killSession so an interrupt can be told from a kill.
//
// `@all`, because that is the turn the reversal was ruled for: the room's
// default route is Claude alone, and a one-seat turn has no seat to leave
// running — the whole property under test. It also produces both seat kinds at
// once, which is what makes "the wrong one was not stopped" assertable.
//
// HOME is redirected because a turn that ends writes room.json, and a test
// must never touch the operator's own.
func ordinaryTurn(t *testing.T) (*Model, map[model.VendorID]*recordedKill, map[model.VendorID]*killSession) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	// The batch room. Since 2026-09-02 (§9.54) every registered seat keeps a
	// process, so "an ordinary one-shot seat" has to be constructed: this is
	// the registry after the three live seats have fallen back to their
	// measured batch adapters, which is a production state and the one whose
	// kill path these tests exist to witness.
	seatFallbacks(t)
	m := flowRoom(t, true)
	m.st.Draft = "@all an ordinary brief"
	m.dispatch()
	if !m.anyInFlight() {
		t.Fatalf("fixture: no turn in flight (%d spawns)", log.n())
	}
	if m.race() != nil {
		t.Fatal("fixture: the brief raced — this file's ordinary half needs an ordinary turn")
	}
	ts := m.dispatches()[0]
	oneShots := map[model.VendorID]*recordedKill{}
	for v := range ts.seatHandles {
		k := &recordedKill{}
		oneShots[v] = k
		ts.seatHandles[v] = k
	}
	if len(oneShots) == 0 {
		t.Fatal("fixture: no seat handle was keyed — x has nothing to address on an ordinary turn")
	}
	live := map[model.VendorID]*killSession{}
	for v := range ts.persistent {
		s := &killSession{}
		live[v] = s
		m.procs[v].sess = s
	}
	if len(live) == 0 {
		t.Fatal("fixture: no persistent seat took the turn — the interrupt arm is untested")
	}
	return m, oneShots, live
}

// oneOf returns a vendor from a fixture map, so a test can name "a batch seat"
// without hard-coding which vendors the registry currently drives that way.
func oneOf[T any](t *testing.T, m map[model.VendorID]T, what string) model.VendorID {
	t.Helper()
	best := model.VendorID("")
	for v := range m {
		if best == "" || v < best {
			best = v
		}
	}
	if best == "" {
		t.Fatalf("no %s in this room", what)
	}
	return best
}

// TestGiveUpOnAnOrdinaryBatchSeatKillsItAndLeavesTheTurnRunning is the reversal
// itself: `x` on an ordinary turn's spawn-per-turn seat kills exactly that
// seat's process, lands its column cancelled, and the brief's other seats go
// on answering. Before 2026-08-17 this refused with "this turn is not a race".
func TestGiveUpOnAnOrdinaryBatchSeatKillsItAndLeavesTheTurnRunning(t *testing.T) {
	m, oneShots, live := ordinaryTurn(t)
	cut := oneOf(t, oneShots, "batch seat")
	c := m.column(cut)
	c.Body = "half a thought"
	focusSeatOn(t, m, cut)

	m.key(key("x"))
	if m.giveUpPending != cut {
		t.Fatalf("x did not arm the focused ordinary seat: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "kills its process") {
		t.Errorf("the card does not say what y costs a batch seat: %q", m.st.Notice)
	}
	m.key(key("y"))

	if !oneShots[cut].killed {
		t.Fatal("y did not kill the seat's process")
	}
	if c.Phase != PhaseCancelled {
		t.Fatalf("phase = %v, want cancelled", c.Phase)
	}
	if !strings.Contains(c.Note, "given up after") || !strings.Contains(c.Note, "its process is dead") {
		t.Errorf("the note does not name the give-up and the seat's fate: %q", c.Note)
	}
	if c.Body != "half a thought" {
		t.Errorf("the cut column lost what it had streamed: %q", c.Body)
	}
	if c.Arena != nil {
		t.Error("an ordinary give-up produced an arena receipt")
	}
	if !m.anyInFlight() {
		t.Fatal("giving up on one seat ended a turn the others are still taking")
	}
	if m.turnOf(cut) != nil {
		t.Error("the cut seat never left the turn's live set — the turn cannot end")
	}
	for v, k := range oneShots {
		if v != cut && k.killed {
			t.Errorf("%s's give-up killed %s's process too", cut, v)
		}
	}
	for v, s := range live {
		if s.killed {
			t.Errorf("%s's give-up killed the persistent seat %s", cut, v)
		}
		if c := m.column(v); c.Phase != PhaseStreaming && c.Phase != PhaseWaiting {
			t.Errorf("%s stopped working: %v", v, c.Phase)
		}
	}
}

// TestGiveUpOnThePersistentSeatInterruptsItAndKeepsTheConversation.
//
// The seat kinds are not interchangeable here, and this is the difference:
// killing the persistent seat would work and would also throw away the
// conversation and the session-init cost that bought it, so cutting one turn
// would silently make the next one expensive (cancelTurn's argument, applied
// per seat). The process stays, the interrupt goes down its own channel, and
// the column's note says the next brief resumes it.
func TestGiveUpOnThePersistentSeatInterruptsItAndKeepsTheConversation(t *testing.T) {
	m, _, live := ordinaryTurn(t)
	cut := oneOf(t, live, "persistent seat")
	focusSeatOn(t, m, cut)

	m.key(key("x"))
	if !strings.Contains(m.st.Notice, "interrupts it") || !strings.Contains(m.st.Notice, "conversation survives") {
		t.Errorf("the card does not say what y costs a persistent seat: %q", m.st.Notice)
	}
	m.key(key("y"))

	sess := live[cut]
	if sess.killed {
		t.Fatal("the persistent seat was KILLED — the conversation and its session-init cost went with it")
	}
	if len(sess.sent) == 0 {
		t.Fatal("nothing was sent to the seat — the vendor was never told to abandon the turn")
	}
	if _, ok := m.procs[cut]; !ok {
		t.Error("the seat's process was forgotten; the next brief would start a new session")
	}
	c := m.column(cut)
	if c.Phase != PhaseCancelled {
		t.Fatalf("phase = %v, want cancelled", c.Phase)
	}
	if !strings.Contains(c.Note, "the next brief resumes it") {
		t.Errorf("the note does not say the conversation survived: %q", c.Note)
	}
	if !strings.Contains(m.st.Notice, "interrupted") || !strings.Contains(m.st.Notice, "conversation is intact") {
		t.Errorf("the footer does not name the mechanism, which is the part the user cannot see: %q", m.st.Notice)
	}
}

// TestAnInterruptedSeatsOwnErrorDoesNotOverwriteTheGiveUp.
//
// Measured behaviour this guards: interrupting a persistent turn comes back as
// a result with is_error true and terminal_reason "aborted_tools" — the vendor
// really does report a failure. The user's keystroke is not the vendor falling
// over, so that event must not replace "given up after …" with the vendor's
// error, must not re-label the column, and must not record a failure class
// against the seat.
func TestAnInterruptedSeatsOwnErrorDoesNotOverwriteTheGiveUp(t *testing.T) {
	m, _, live := ordinaryTurn(t)
	cut := oneOf(t, live, "persistent seat")
	focusSeatOn(t, m, cut)
	m.key(key("x"))
	m.key(key("y"))
	note := m.column(cut).Note

	m.applyEvents([]runner.Event{{
		Vendor: cut, Kind: runner.KindError, EndsTurn: true,
		Note: "the vendor reported the turn failed", Failure: runner.FailureUnclassified,
	}})

	c := m.column(cut)
	if c.Note != note {
		t.Errorf("the vendor's abort error replaced the give-up's own sentence: %q", c.Note)
	}
	if c.Phase != PhaseCancelled {
		t.Errorf("phase = %v — the abort re-labelled a seat the operator stopped", c.Phase)
	}
	if _, blamed := m.failure[cut]; blamed {
		t.Error("a keystroke was recorded as a vendor failure")
	}
}

// TestACutSeatThatStreamedNothingIsNotAMeasuredZero.
//
// The §4a.1 case, in the shape this feature makes possible. A killed child
// drains its buffered stdout, so its exit lands on a column that is already
// terminal — and the ordinary KindDone path stamps "[Turn completed with 0
// text chunks streamed]" onto an empty body. On a cut seat that sentence is a
// lie twice over: the turn did not complete, and a seat that never spoke is
// not a seat that measured nothing.
func TestACutSeatThatStreamedNothingIsNotAMeasuredZero(t *testing.T) {
	m, oneShots, _ := ordinaryTurn(t)
	cut := oneOf(t, oneShots, "batch seat")
	focusSeatOn(t, m, cut)

	m.key(key("x"))
	m.key(key("y"))
	c := m.column(cut)
	if !strings.Contains(c.Note, "nothing had arrived when it was cut") {
		t.Errorf("the note does not say the seat never spoke: %q", c.Note)
	}

	m.applyEvents([]runner.Event{{Vendor: cut, Kind: runner.KindDone}})

	if c.Body != "" {
		t.Errorf("the cut seat's exit claimed a measured zero: body = %q", c.Body)
	}
	if c.Phase != PhaseCancelled {
		t.Errorf("phase = %v — the exit echo re-labelled a cut column", c.Phase)
	}
	if !strings.Contains(c.Note, "given up after") {
		t.Errorf("the exit echo took the give-up's own sentence: %q", c.Note)
	}

	// The same exit arriving AFTER the turn boundary, which is the case
	// turnState.givenUp cannot cover — the turn is gone, and with it the fact
	// that this seat was cut. The placeholder's own guard is what holds here:
	// it is a claim about a turn that completed, so only a column still in a
	// live phase may acquire it.
	idle(m)
	m.applyEvents([]runner.Event{{Vendor: cut, Kind: runner.KindDone}})
	if c.Body != "" {
		t.Errorf("an exit past the turn boundary claimed a measured zero on a cut column: body = %q", c.Body)
	}
	if c.Phase != PhaseCancelled {
		t.Errorf("phase = %v — an exit past the turn boundary re-labelled a cut column", c.Phase)
	}
}

// TestTheOrdinaryTurnEndsWhenTheRemainingSeatsLand is the whole point of the
// reversal, in the same shape the race version of it takes: the cut seat drains
// through the same finish every other landing uses, so nobody waits on a seat
// they have already given up on.
func TestTheOrdinaryTurnEndsWhenTheRemainingSeatsLand(t *testing.T) {
	m, oneShots, _ := ordinaryTurn(t)
	cut := oneOf(t, oneShots, "batch seat")
	focusSeatOn(t, m, cut)

	m.key(key("x"))
	m.key(key("y"))
	if !m.anyInFlight() {
		t.Fatal("the give-up itself ended the turn")
	}
	var rest []model.VendorID
	for v := range m.turns {
		rest = append(rest, v)
	}
	for _, v := range rest {
		m.applyEvents([]runner.Event{{Vendor: v, Kind: runner.KindMeta, EndsTurn: true}})
		m.applyEvents([]runner.Event{{Vendor: v, Kind: runner.KindDone}})
	}

	if m.anyInFlight() {
		t.Fatal("every remaining seat has landed and the turn is still in flight — the hostage the key exists to free")
	}
	if m.st.Mode != ModeComposing {
		t.Error("the ended turn did not hand the composer back")
	}
}

// TestTheCutColumnStaysDistinctFromEveryOtherWayAColumnEnds.
//
// Four endings, four sentences, and the property is that no two of them read
// alike: given up, not addressed, cancelled by ctrl+c, and a turn that
// completed having said nothing. A note that collapsed any pair would make the
// room's own transcript unreadable at exactly the moment the reader is trying
// to work out who answered.
func TestTheCutColumnStaysDistinctFromEveryOtherWayAColumnEnds(t *testing.T) {
	m, oneShots, live := ordinaryTurn(t)
	batch := oneOf(t, oneShots, "batch seat")
	seat := oneOf(t, live, "persistent seat")
	focusSeatOn(t, m, batch)
	m.key(key("x"))
	m.key(key("y"))
	focusSeatOn(t, m, seat)
	m.key(key("x"))
	m.key(key("y"))

	notes := map[string]string{
		"given up (batch)":      m.column(batch).Note,
		"given up (persistent)": m.column(seat).Note,
		"not addressed":         "not addressed in turn 2",
		"ctrl+c":                "cancelled — the output above is partial",
	}
	seen := map[string]string{}
	for name, note := range notes {
		if note == "" {
			t.Fatalf("%s has no note at all", name)
		}
		if prior, dup := seen[note]; dup {
			t.Errorf("%s and %s both read %q", prior, name, note)
		}
		seen[note] = name
	}
	for _, name := range []string{"given up (batch)", "given up (persistent)"} {
		if !strings.Contains(notes[name], "given up") {
			t.Errorf("%s does not say it was given up: %q", name, notes[name])
		}
		if strings.Contains(notes[name], "not addressed") || strings.Contains(notes[name], "partial") {
			t.Errorf("%s borrowed another ending's words: %q", name, notes[name])
		}
	}
}

// givenUpRoom is the rendered form of the reversal: one seat cut while it was
// mid-sentence, one cut before it said anything, and one that finished having
// measured nothing. The third column is the control — it is the render a cut
// seat must never be mistaken for.
func givenUpRoom() State {
	st := room()
	st.Turn = 2
	st.Columns[0].Phase = PhaseCancelled
	st.Columns[0].Body = "The resume path is the one to take, because re-sending the"
	st.Columns[0].Note = "given up after 4m12s — what arrived before the cut is above; its conversation survives, so the next brief resumes it"
	st.Columns[0].Elapsed = 4*time.Minute + 12*time.Second
	st.Columns[1].Phase = PhaseCancelled
	st.Columns[1].Body = ""
	st.Columns[1].Note = "given up after 11m3s — nothing had arrived when it was cut; its process is dead"
	st.Columns[1].Elapsed = 11*time.Minute + 3*time.Second
	st.Columns[2].Phase = PhaseDone
	st.Columns[2].Body = "[Turn completed with 0 text chunks streamed]"
	st.Columns[2].Elapsed = 9 * time.Second
	return st
}

// TestGivenUpColumnsAgainstAMeasuredZero pins the frame the amendment claims:
// three endings, three renders, and in particular the cut seat that never
// spoke drawing NOTHING where the seat that answered nothing draws the
// placeholder sentence. Collapsing those two is §4a.1's own regression.
func TestGivenUpColumnsAgainstAMeasuredZero(t *testing.T) {
	golden(t, "given-up-vs-zero", render(givenUpRoom()))
}

// TestTheGiveUpReadsTheSameInASCIIAndWithoutColour. Every distinction this room
// makes is carried by a word first (§9.11), so the cut seat's whole signal —
// the phase word and the note that says which kind of cut it was — has to
// survive --ascii and NO_COLOR. The colour is only ever the second signal, and
// PlainStyles is the identity set that stands in for a monochrome terminal.
func TestTheGiveUpReadsTheSameInASCIIAndWithoutColour(t *testing.T) {
	st := givenUpRoom()
	st.ASCII = true
	got := Render(st, PlainStyles(), GlyphsFor(true))

	// Fragments, not whole sentences: a body wraps to the column width, so a
	// check on the full string would be testing the wrap rather than the words.
	// Each fragment below is short enough to land on one line at 120 cells.
	for _, want := range []string{
		"cancelled",
		"given up after 4m12s",
		"given up after 11m3s",
		"had arrived when it was cut",
		"process is dead",
		"its conversation survives",
		"[Turn completed with 0 text chunks",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--ascii dropped %q\n%s", want, got)
		}
	}
	// The one that would pass every substring check above and still be the bug:
	// a cut seat and a measured zero rendering the same body.
	if strings.Count(got, "[Turn completed with 0 text chunks") != 1 {
		t.Error("more than one column claims a measured zero — a cut seat borrowed the placeholder")
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
