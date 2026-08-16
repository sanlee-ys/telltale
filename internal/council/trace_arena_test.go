package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The race reaches the trace, and says which race it was.
//
// The defect these pin down (STATE.md, OBSERVED 2026-08-15/16): a trace armed
// before a race held no line anyone could attribute to it. The runner measured
// every racer — that half was verified against a live one-shot spawn — but a
// TurnClock carried only a vendor and a moment, so a racer's line was
// byte-identical in SHAPE to an ordinary turn's line for the same seat. The
// room knew the race number and never told the runner, which is the one place
// the record is written.
//
// Witnessed at the SPEC, not at the clock, and that is forced rather than
// preferred: a council test never spawns a vendor (CLAUDE.md), so the runner's
// clock cannot run here at all. The spec is the whole of what this package
// contributes to the record — the runner's own test carries the other half,
// that a spec carrying a race lands it on the line.

// TestArenaSpecsCarryTheRaceIntoTheTrace: every racer's invocation is labelled
// with the race it belongs to, on BOTH spawn paths.
//
// Both paths matter and neither can stand for the other: the one-shot seats go
// through FirstTurn and startProcess, while the cursor seat races as an
// ephemeral ACP session through startRPCSession (§9.37's follow-up). A label
// applied to one path only would leave the merged seat unattributable in
// exactly the trace the race was run to read.
func TestArenaSpecsCarryTheRaceIntoTheTrace(t *testing.T) {
	m, log, _ := arenaCursorRace(t)

	want := arenaRaceTag(m.turn.arenaRaceN)
	if want == "" {
		t.Fatal("the race minted no tag")
	}
	// The racers are exactly the seats the race built a worktree for. A seat
	// whose worktree failed setup never spawned, so demanding a label from it
	// would assert against a process that does not exist.
	raced := 0
	for _, spec := range log.specs {
		if _, isRacer := m.turn.arenaTrees[spec.Vendor]; !isRacer {
			continue
		}
		raced++
		if spec.Race != want {
			t.Errorf("%s raced with Race=%q, want %q — this seat's turn is unattributable in the trace",
				spec.Vendor, spec.Race, want)
		}
	}
	if raced == 0 {
		t.Fatal("no racer spawned — the assertion above proved nothing")
	}
	if _, cursorRaced := m.turn.arenaTrees[model.VendorCursor]; cursorRaced {
		// Named explicitly: the ephemeral path builds its spec inside
		// startEphemeralRacer, not in dispatch's arena branch, so it is the
		// half a fix applied at the obvious call site would miss.
		var seen bool
		for _, spec := range log.specs {
			if spec.Vendor == model.VendorCursor && spec.Race == want {
				seen = true
			}
		}
		if !seen {
			t.Error("the cursor seat raced on an ephemeral session with no race on its spec")
		}
	}
}

// TestOrdinaryTurnsCarryNoRace: an ordinary turn is not a race with a missing
// id, it is not a race at all — so its spec carries nothing rather than a
// placeholder. The same distinction §4a.1 draws between absent and zero, and
// the reason the trace line omits the field instead of printing a dash.
func TestOrdinaryTurnsCarryNoRace(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "an ordinary brief"
	m.dispatch()
	if m.turn == nil {
		t.Fatal("the turn did not dispatch")
	}
	if m.turn.arena {
		t.Fatal("an ordinary brief dispatched as a race")
	}
	if log.n() == 0 {
		t.Fatal("nothing spawned — the assertion below would prove nothing")
	}
	for _, spec := range log.specs {
		if spec.Race != "" {
			t.Errorf("%s took an ordinary turn with Race=%q, want empty", spec.Vendor, spec.Race)
		}
	}
}

// TestArenaRaceTagMatchesTheBranch: the tag and the branch are minted from one
// number, so a trace line and the worktree it belongs to can never disagree.
// The vendor is already a column on the trace line, so the tag carries the race
// alone and the two together spell the branch (arenaBranch).
func TestArenaRaceTagMatchesTheBranch(t *testing.T) {
	const raceN = 9
	tag := arenaRaceTag(raceN)
	branch := arenaBranch(raceN, model.VendorGrok)
	if !strings.HasPrefix(branch, tag+"/") {
		t.Errorf("tag %q and branch %q disagree — a trace line could not be traced to its worktree", tag, branch)
	}
}
