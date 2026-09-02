package council

import (
	"context"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The cursor seat's race, witnessed the way cursoracp_test.go witnesses the
// seat itself: by which spawn ran, what spec it was handed, and what happened
// to the process — never by what the adapter says about itself.
//
// The property under all of these is §9.37's follow-up built whole: an arena
// turn drives this vendor as an EPHEMERAL ACP session — spawned in the racer's
// worktree, one session, one prompt, killed when the column lands — while the
// room's own conversation (its live process, its saved ids, room.json) is
// untouchable by construction. cursor-agent is not installed where these run;
// the live half of the verification is owed on the Windows box and the design
// doc says so.

// arenaCursorRace is a four-seat write room in a real git workspace with
// /arena dispatched, the cursor racer's session captured for kill assertions.
//
// It layers over countSpawns rather than replacing it, so the spec log and the
// proto capture keep working; only the returned session changes, because the
// property most of these tests end on IS the kill, and deadSession's no-op
// Kill would assert the call instead of the effect (clear_test.go's argument).
func arenaCursorRace(t *testing.T) (*Model, *spawnLog, *killSession) {
	t.Helper()
	log := countSpawns(t)
	racer := &killSession{}
	prev := startRPCSession
	startRPCSession = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, proto runner.Protocol) (seatSession, error) {
		log.protos[len(log.specs)] = proto
		log.specs = append(log.specs, spec)
		return racer, nil
	}
	t.Cleanup(func() { startRPCSession = prev })

	m := flowRoom(t, true)
	m.st.Workspace = gitRepo(t)
	m.sessions[model.VendorCursor] = "cursor-room-thread"
	m.st.Draft = "/arena add a marker file"
	// raceNow rather than dispatch: the worktree setup runs off the loop now, so
	// the turn is born after its messages land (arenasetup_test.go).
	raceNow(t, m)
	if m.turn == nil {
		t.Fatal("the race did not dispatch")
	}
	return m, log, racer
}

// TestArenaRacesTheCursorSeatOnAThrowawayACPSession is the spawn-path choice:
// the seat whose FirstTurn refuses by design (ErrCursorIsLiveOnly, §9.36) is
// raced through its own live protocol instead — rooted in ITS worktree, on the
// turn's books rather than the room's.
func TestArenaRacesTheCursorSeatOnAThrowawayACPSession(t *testing.T) {
	m, log, _ := arenaCursorRace(t)

	c := m.column(model.VendorCursor)
	if c.Phase == PhaseFailed {
		t.Fatalf("the cursor seat could not race: %q — the FirstTurn refusal is back on the column", c.Note)
	}
	if m.ephemeralRacer(model.VendorCursor) == nil {
		t.Fatal("no ephemeral session is registered on the turn")
	}
	if _, inProcs := m.procs[model.VendorCursor]; inProcs {
		t.Error("the racer landed in m.procs — the next ordinary brief would be handed to a worktree session")
	}

	tree := m.turn.arenaTrees[model.VendorCursor]
	var cursorSpecs int
	for i, spec := range log.specs {
		if spec.Vendor != model.VendorCursor {
			continue
		}
		cursorSpecs++
		// The §9.36 invocation, unchanged: `acp` and nothing else. A flag
		// appearing here would mean a second, unmeasured invocation of this
		// vendor was invented for the race.
		if len(spec.Args) != 1 || spec.Args[0] != "acp" {
			t.Errorf("racer argv = %v, want the one-word acp invocation", spec.Args)
		}
		if spec.Dir != tree {
			t.Errorf("racer Dir = %q, want its own worktree %q", spec.Dir, tree)
		}
		if log.protos[i] == nil {
			t.Error("the racer was spawned without a protocol — nothing would drive its session")
		}
	}
	if cursorSpecs != 1 {
		t.Errorf("%d cursor spawns, want exactly 1", cursorSpecs)
	}
	// The other three racers still take the FirstTurn one-shot: four seats,
	// four spawns, no seat silently dropped to make room for the new path.
	if log.n() != 4 {
		t.Errorf("%d spawns for a four-seat race, want 4: %+v", log.n(), log.specs)
	}
}

// TestARaceNeverTouchesTheCursorRoomThread: the throwaway session id is
// refused before it can reach m.sessions, and the room's own live process is
// neither killed nor forgotten by a race running beside it. This is
// TestArenaTurnNeverTouchesSavedThreads re-asked for the one seat whose race
// runs on a live protocol — the seat with the most conversation to lose.
func TestARaceNeverTouchesTheCursorRoomThread(t *testing.T) {
	m, _, _ := arenaCursorRace(t)
	roomProc := &killSession{}
	m.procs[model.VendorCursor] = &seatProc{sess: roomProc, dir: m.st.Workspace}

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorCursor, Kind: runner.KindSession, SessionID: "race-throwaway-id"},
		{Vendor: model.VendorCursor, Kind: runner.KindText, Text: "racing. "},
		{Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true},
	})

	if got := m.sessions[model.VendorCursor]; got != "cursor-room-thread" {
		t.Errorf("the race replaced the room's saved thread: %q", got)
	}
	if roomProc.killed {
		t.Error("finishing the race killed the ROOM's process")
	}
	if _, ok := m.procs[model.VendorCursor]; !ok {
		t.Error("the room's process was dropped by a race that never used it")
	}
}

// TestTheRacerIsKilledAtItsOwnFinishLine, not the turn's: the end-of-turn
// response retires the column, and the process — which §9.33 measured
// lingering ~2.5s after answering — is killed in the same breath, before the
// diff is read. The column's spend gauge stays ABSENT, because ACP reports no
// token usage and no cost (§9.36): nil is "reported nothing", and a zero here
// would be an invented number.
func TestTheRacerIsKilledAtItsOwnFinishLine(t *testing.T) {
	m, _, racer := arenaCursorRace(t)

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorCursor, Kind: runner.KindText, Text: "done. "},
		{Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true},
	})

	if !racer.killed {
		t.Fatal("the racer's process outlived its column — the orphan §9.37 forbids")
	}
	if m.ephemeralRacer(model.VendorCursor) != nil {
		t.Error("the reaped racer is still registered on the turn")
	}
	c := m.column(model.VendorCursor)
	if c.Phase != PhaseDone {
		t.Errorf("phase = %v, want done", c.Phase)
	}
	if c.Arena == nil {
		t.Fatal("the racer landed without an arena result — no diff was collected")
	}
	if c.Arena.Rank == 0 {
		t.Error("the racer landed unranked")
	}
	if c.CostUSD != nil {
		t.Errorf("CostUSD = %v, want nil — this seat reports no usage, and absent must stay absent", *c.CostUSD)
	}

	// A racer driven by a live protocol retires twice: the response above, then
	// the exit of the process that response got killed. The echo must not
	// re-rank the race — collection is once-only.
	rank := c.Arena.Rank
	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindDone}})
	if c.Arena.Rank != rank {
		t.Errorf("the kill's own exit re-ranked the race: %d -> %d", rank, c.Arena.Rank)
	}
	if m.turn == nil {
		t.Error("the exit echo tore down a turn three seats are still racing")
	}
}

// TestTheRacerIsKilledOnAProtocolReportedFailure: a refused handshake leaves
// an ACP server up and useless (§9.36) — no exit event is ever coming, so the
// failure itself must kill the process, or the racer becomes the orphan and
// the column streams forever.
func TestTheRacerIsKilledOnAProtocolReportedFailure(t *testing.T) {
	m, _, racer := arenaCursorRace(t)

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorCursor, Kind: runner.KindError, EndsTurn: true,
		Note: "the vendor refused the ACP handshake: not signed in",
	}})

	if !racer.killed {
		t.Fatal("a racer whose protocol failed was left running")
	}
	c := m.column(model.VendorCursor)
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed", c.Phase)
	}
	if !strings.Contains(c.Note, "handshake") {
		t.Errorf("the failure lost its reason: %q", c.Note)
	}
	if m.turn.live[model.VendorCursor] {
		t.Error("the failed racer never left the turn — the race cannot end")
	}
}

// TestAnEmptyStreamIsNamedNotRenderedAsAnEmptySuccess: ACP's turn resolves
// with a stop reason and no reply, so a racer that streamed nothing and a
// chunk parser that broke look identical on the wire (§9.36's stated loss).
// The column may not pick a story; it must say the fact.
func TestAnEmptyStreamIsNamedNotRenderedAsAnEmptySuccess(t *testing.T) {
	m, _, _ := arenaCursorRace(t)

	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindMeta, EndsTurn: true}})

	c := m.column(model.VendorCursor)
	if c.Phase != PhaseDone {
		t.Errorf("phase = %v — a clean end is still a clean end", c.Phase)
	}
	if c.Note == "" {
		t.Fatal("an empty-stream turn landed as a bare success, with nothing naming the ambiguity")
	}
	if !strings.Contains(c.Note, "nothing streamed") {
		t.Errorf("the note does not name the empty stream: %q", c.Note)
	}
}

// TestARacerDeathWithoutATurnEndFailsTheColumn: on this seat the turn's end is
// a RESPONSE, so a bare exit — even a zero one — means no answer ever arrived.
// And it must reach the column past the stale-exit guard: a live room process
// sitting in m.procs is exactly what that guard reads as "this seat is fine",
// which during a race would eat the racer's own death and hang the turn.
func TestARacerDeathWithoutATurnEndFailsTheColumn(t *testing.T) {
	m, _, racer := arenaCursorRace(t)
	m.procs[model.VendorCursor] = &seatProc{sess: deadSession{}, dir: m.st.Workspace}

	// The racer dies externally — crash, OOM, someone's task manager.
	racer.killed = true
	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindDone}})

	c := m.column(model.VendorCursor)
	if c.Phase != PhaseFailed {
		t.Fatalf("phase = %v — a racer that never answered landed as %v", c.Phase, c.Phase)
	}
	if !strings.Contains(c.Note, "before its turn") {
		t.Errorf("the death is not named: %q", c.Note)
	}
	if m.turn.live[model.VendorCursor] {
		t.Error("the dead racer never left the turn")
	}
	if _, ok := m.procs[model.VendorCursor]; !ok {
		t.Error("the racer's death took the ROOM's live process with it")
	}
}

// TestABackgroundRoomSeatDeathDoesNotFailTheRace is the attribution rule's
// other half: while the racer is ALIVE, an exit wearing this vendor's id can
// only be the room's own idle process dying in the background. The race column
// is not its to end.
func TestABackgroundRoomSeatDeathDoesNotFailTheRace(t *testing.T) {
	m, _, racer := arenaCursorRace(t)
	m.procs[model.VendorCursor] = &seatProc{sess: exitedSession{}, dir: m.st.Workspace}

	m.applyEvents([]runner.Event{{Vendor: model.VendorCursor, Kind: runner.KindDone}})

	if racer.killed {
		t.Error("a background death killed the racer")
	}
	c := m.column(model.VendorCursor)
	if c.Phase != PhaseWaiting && c.Phase != PhaseStreaming {
		t.Errorf("phase = %v — the room seat's death retired a race column it was not driving", c.Phase)
	}
	if _, ok := m.procs[model.VendorCursor]; ok {
		t.Error("the dead room process is still registered; the next ordinary brief would write into it")
	}
	if m.turn == nil {
		t.Error("a background death ended the turn")
	}
}

// TestCancelAndTeardownKillTheRacer: the two paths that end a race without the
// racer's own column landing first. Both kill outright — a throwaway session
// has no conversation for an interrupt to spare, and quitting mid-race must
// not leave an ACP server running in a worktree with nothing on screen to say
// so. (A seat cannot be CLEARED mid-race at all: askClearSeat refuses while a
// turn is in flight, so that path is closed structurally rather than handled.)
func TestCancelAndTeardownKillTheRacer(t *testing.T) {
	// The one-shot racers' handles are countSpawns' empty shells, whose Kill
	// needs a process that was never spawned. They are not the property here —
	// the runner's own tests pin Handle.Kill — so they are dropped, leaving
	// exactly the racer for these two paths to reap or orphan.
	t.Run("cancel", func(t *testing.T) {
		m, _, racer := arenaCursorRace(t)
		m.turn.handles = nil
		m.cancelTurn()
		if !racer.killed {
			t.Error("ctrl+c left the racer running")
		}
	})
	t.Run("teardown", func(t *testing.T) {
		m, _, racer := arenaCursorRace(t)
		m.turn.handles = nil
		m.teardown()
		if !racer.killed {
			t.Error("quitting the room left the racer running")
		}
	})
}

// TestARaceBriefCarriesTheConductLineAndAnOrdinaryBriefDoesNot pins the one
// place the room adds words to a brief (arenaConduct, §9.37 amended
// 2026-08-09): every one-shot racer's prompt opens with the same constant
// line ahead of the operator's own words — prepended, not appended, so a long
// brief cannot bury it — and an ordinary turn's prompt carries none of it.
// A Conversational racer's prompt rides its protocol rather than its spec
// (§9.36: `acp` is the whole argv; since §9.54 codex's `app-server` and grok's
// `agent stdio` are the same shape), so the one-shot racers are the witnesses
// here — and which seats those are is read off the registry rather than
// counted by hand, because the count moved once already.
func TestARaceBriefCarriesTheConductLineAndAnOrdinaryBriefDoesNot(t *testing.T) {
	_, log, _ := arenaCursorRace(t)

	reg := vendors.Registry()
	want := 0
	for _, v := range reg {
		if _, conversational := v.(vendors.Conversational); !conversational {
			want++
		}
	}
	oneShots := 0
	for _, spec := range log.specs {
		if _, conversational := reg[spec.Vendor].(vendors.Conversational); conversational {
			continue
		}
		oneShots++
		p := specPrompt(spec)
		at, brief := strings.Index(p, arenaConduct), strings.Index(p, "add a marker file")
		if at < 0 {
			t.Errorf("%s raced without the conduct line:\n%s", spec.Vendor, p)
			continue
		}
		if brief < 0 {
			t.Errorf("%s lost the operator's own brief:\n%s", spec.Vendor, p)
			continue
		}
		if at > brief {
			t.Errorf("%s carries the conduct line AFTER the brief — a long brief would bury it", spec.Vendor)
		}
	}
	if oneShots != want || want == 0 {
		t.Fatalf("%d one-shot racers, want %d (every non-Conversational seat in the registry)", oneShots, want)
	}
}

// TestAnOrdinaryBriefStaysVerbatim is the boundary of the bend: the conduct
// line is a race fact, and a turn that is not a race sends exactly what the
// operator typed.
func TestAnOrdinaryBriefStaysVerbatim(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "@all add a marker file"
	m.dispatch()
	if m.turn == nil {
		t.Fatal("the turn did not dispatch")
	}
	if log.n() == 0 {
		t.Fatal("nothing spawned")
	}
	for _, spec := range log.specs {
		if strings.Contains(specPrompt(spec), arenaConduct) {
			t.Errorf("%s's ordinary brief carries the race conduct line", spec.Vendor)
		}
	}
}
