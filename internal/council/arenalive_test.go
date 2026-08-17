package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// arenaLiveTurn is a one-racer arena turn a test can drive by hand: codex
// streaming on turn 5, its live-stat slot empty, its tree a synthesized path —
// dueArenaRefreshes is only ever asked whether it WOULD read, so no real git
// repository is needed until a test executes the returned Cmd.
func arenaLiveTurn(m *Model) *arenaLiveState {
	m.st.Columns[1].Phase = PhaseStreaming
	m.st.Columns[1].TurnN = 5
	ls := &arenaLiveState{}
	m.turn = &turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true,
		arenaBase:  "abcdef1234",
		arenaTrees: map[model.VendorID]string{model.VendorCodex: "/x/repo-arena-t5-codex"},
		arenaLive:  map[model.VendorID]*arenaLiveState{model.VendorCodex: ls},
	}
	return ls
}

// TestArenaInterimThreeStatesRenderDifferently: no read yet (nil) renders
// NOTHING, a read that returned empty is a measured "no changes yet", and a
// failed read carries its reason — absent, zero and degraded stay three facts
// (§4a.1), mid-race exactly as they do at the finish line.
func TestArenaInterimThreeStatesRenderDifferently(t *testing.T) {
	st := room()
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].ArenaInterim = &ArenaInterim{Stat: " a.txt | 2 +-", Base: "abcdef1234"}
	st.Columns[1].Phase = PhaseStreaming // interim nil: no read has returned
	st.Columns[2].Phase = PhaseStreaming
	st.Columns[2].ArenaInterim = &ArenaInterim{Base: "abcdef1234"} // measured zero

	got := render(st)
	if !strings.Contains(got, " a.txt | 2 +-") {
		t.Error("the interim stat is not on screen")
	}
	if !strings.Contains(got, "no changes yet against abcdef1.") {
		t.Error("a measured-empty read does not say 'no changes yet' against the named base")
	}
	if n := strings.Count(got, "arena · so far"); n != 2 {
		t.Errorf("%d live blocks on screen, want 2 — a seat with no read yet must render nothing", n)
	}

	st.Columns[2].ArenaInterim = &ArenaInterim{Err: "boom", Base: "abcdef1234"}
	got = render(st)
	if !strings.Contains(got, "live stat unavailable: boom") {
		t.Error("a failed read does not carry its reason")
	}
	if strings.Contains(got, "no changes yet against abcdef1.") &&
		!strings.Contains(got, " a.txt | 2 +-") {
		t.Error("a failed read rendered as a zero — degraded dressed as nothing-changed")
	}
}

// TestArenaInterimMarksItselfAndNeverTheFinal: the live block says "so far"
// and withholds the finish line's receipt (branch, rank), so a mid-race read
// cannot masquerade as the settled result.
func TestArenaInterimMarksItselfAndNeverTheFinal(t *testing.T) {
	st := room()
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].TurnN = 5
	st.Columns[0].ArenaInterim = &ArenaInterim{Stat: " a.txt | 1 +", Base: "abcdef1234"}

	got := render(st)
	if !strings.Contains(got, "arena · so far") {
		t.Error("the interim stat renders without its 'so far' marker")
	}
	if strings.Contains(got, "arena/t5/") {
		t.Error("the interim block prints a branch — the finish line's receipt, worn early")
	}
}

// TestArenaFinalReplacesTheInterim drives the real finish path against a real
// repository: the last mid-race stat must be REPLACED by the finish-time
// collectArena read — cleared, never merged — and the frame swaps "so far"
// for the settled block in the same land.
func TestArenaFinalReplacesTheInterim(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 5, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	ls := arenaLiveTurn(m)
	m.turn.arenaRaceN = raceN
	m.turn.arenaBase = base
	m.turn.arenaTrees = trees
	_ = ls

	// A mid-race read lands: the tree is untouched, so it is a measured zero.
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5})
	c := &m.st.Columns[1]
	if c.ArenaInterim == nil {
		t.Fatal("the interim read did not land")
	}
	if !strings.Contains(render(m.st), "arena · so far") {
		t.Fatal("the live block is not on screen before the finish")
	}

	// The seat then changes something and finishes.
	if err := os.WriteFile(filepath.Join(trees[model.VendorCodex], "late.go"), []byte("package late\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.finishColumn(c, PhaseDone)

	if c.Arena == nil {
		t.Fatal("the racer landed without a final result")
	}
	if c.ArenaInterim != nil {
		t.Error("the interim survived the finish — the final must replace it, never merge")
	}
	got := render(m.st)
	if strings.Contains(got, "so far") {
		t.Error("'so far' still on screen after the finish — an interim marker on a settled result")
	}
	if !strings.Contains(got, "arena arena/t5/codex") || !strings.Contains(got, "late.go") {
		t.Error("the settled block is missing after the swap")
	}
}

// TestArenaRefreshOneInFlightAndThrottled pins the two spend bounds: a due
// check while a read is running SKIPS (never queues), and a completed read
// starts the interval over — at most one launch per arenaRefreshInterval per
// seat, timed off State.Now so the whole property is testable.
func TestArenaRefreshOneInFlightAndThrottled(t *testing.T) {
	m := clearModel()
	ls := arenaLiveTurn(m)
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	m.st.Now = now

	if m.dueArenaRefreshes() != nil {
		t.Fatal("an UNARMED seat was read — an idle seat re-reads nothing")
	}

	ls.armed = true
	if m.dueArenaRefreshes() == nil {
		t.Fatal("an armed seat with no prior read was not read")
	}
	if !ls.inFlight || ls.armed {
		t.Fatalf("launch bookkeeping: inFlight=%v armed=%v, want true/false", ls.inFlight, ls.armed)
	}

	// Armed again while the read runs: skip, don't queue.
	ls.armed = true
	if m.dueArenaRefreshes() != nil {
		t.Fatal("a second read launched while one is in flight")
	}

	// The read returns; the seat is armed but the interval has not elapsed.
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5, stat: " a.txt | 1 +"})
	if ls.inFlight {
		t.Fatal("the landed read still counts as in flight")
	}
	if got := m.st.Columns[1].ArenaInterim; got == nil || got.Stat != " a.txt | 1 +" {
		t.Fatalf("the interim stat did not land: %+v", got)
	}
	ls.armed = true
	if m.dueArenaRefreshes() != nil {
		t.Fatal("a read launched inside the throttle interval")
	}

	// The interval elapses on the tick clock: now it fires.
	m.st.Now = now.Add(arenaRefreshInterval)
	if m.dueArenaRefreshes() == nil {
		t.Fatal("an armed seat was not read once the interval elapsed")
	}
}

// TestArenaRefreshStopsWithTheTurnAndTheSeat: a landed seat is never read
// again, a stale message (old turn, or arriving after the final) writes
// nothing, and a message landing on a torn-down turn is dropped without
// touching anything.
func TestArenaRefreshStopsWithTheTurnAndTheSeat(t *testing.T) {
	m := clearModel()
	ls := arenaLiveTurn(m)
	c := &m.st.Columns[1]

	// The seat lands: even armed, nothing more is launched for it.
	delete(m.turn.live, model.VendorCodex)
	ls.armed = true
	if m.dueArenaRefreshes() != nil {
		t.Error("a landed seat's tree was read again")
	}

	// A read that raced the finish arrives AFTER the final: dropped, and the
	// in-flight slot still frees.
	ls.inFlight = true
	c.Arena = &ArenaResult{Stat: "final"}
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5, stat: "stale"})
	if c.ArenaInterim != nil {
		t.Error("a stale read wrote an interim over the settled result")
	}
	if ls.inFlight {
		t.Error("a dropped read left its seat marked in flight")
	}

	// A read from a previous turn: the turn number is the guard.
	c.Arena = nil
	c.TurnN = 6
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5, stat: "stale"})
	if c.ArenaInterim != nil {
		t.Error("an old turn's read landed on the new turn")
	}

	// The turn tears down entirely: the late message is a no-op, not a panic.
	m.turn = nil
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5, stat: "stale"})
	if c.ArenaInterim != nil {
		t.Error("a read landed on a room with no turn")
	}
	if m.dueArenaRefreshes() != nil {
		t.Error("a room with no turn launched a read")
	}
}

// TestArenaRepeatedFailureStopsRefreshingAndSaysSo: each failed read is shown
// degraded (never as a zero), arenaRefreshMaxFails consecutive failures end
// the live stat for that seat with the stop NAMED on the column, and a
// success in between resets the count — one contended read is not evidence
// the tree is unreadable.
func TestArenaRepeatedFailureStopsRefreshingAndSaysSo(t *testing.T) {
	m := clearModel()
	ls := arenaLiveTurn(m)
	c := &m.st.Columns[1]
	fail := arenaStatMsg{vendor: model.VendorCodex, turnN: 5, err: "index.lock exists"}

	m.applyArenaStat(fail)
	m.applyArenaStat(fail)
	if ls.stopped || c.ArenaInterim == nil || c.ArenaInterim.Stopped {
		t.Fatal("two failures already ended the live stat")
	}
	if c.ArenaInterim.Err == "" {
		t.Fatal("a failed read rendered without its reason")
	}

	// A success between failures resets the count.
	m.applyArenaStat(arenaStatMsg{vendor: model.VendorCodex, turnN: 5, stat: " a.txt | 1 +"})
	if ls.fails != 0 {
		t.Fatalf("a successful read did not reset the failure count: %d", ls.fails)
	}

	m.applyArenaStat(fail)
	m.applyArenaStat(fail)
	m.applyArenaStat(fail)
	if !ls.stopped || c.ArenaInterim == nil || !c.ArenaInterim.Stopped {
		t.Fatalf("%d consecutive failures did not stop the live stat", arenaRefreshMaxFails)
	}
	// Asserted in two pieces because the column wraps the sentence at word
	// boundaries — the property is that the give-up is named, not where the
	// line breaks fall.
	got := render(m.st)
	if !strings.Contains(got, "stopped re-reading") || !strings.Contains(got, "finish-time diff") {
		t.Error("the give-up is not named on the column — a stat that froze silently")
	}
	ls.armed = true
	if m.dueArenaRefreshes() != nil {
		t.Error("a stopped seat's tree was read again")
	}
}

// TestArenaStreamActivityArmsTheRefresh: text and tool-call events arm the
// seat they belong to and no other, and an ordinary (non-arena) turn arms
// nothing — the live stat is a race feature, not a per-turn tax.
func TestArenaStreamActivityArmsTheRefresh(t *testing.T) {
	m := clearModel()
	m.redactors = map[model.VendorID]*Redactor{}
	ls := arenaLiveTurn(m)
	claudeLS := &arenaLiveState{}
	m.turn.live[model.VendorClaude] = true
	m.turn.arenaTrees[model.VendorClaude] = "/x/repo-arena-t5-claude"
	m.turn.arenaLive[model.VendorClaude] = claudeLS
	m.st.Columns[0].Phase = PhaseStreaming
	m.st.Columns[0].TurnN = 5

	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "working "}})
	if !ls.armed {
		t.Error("streamed text did not arm its seat")
	}
	if claudeLS.armed {
		t.Error("one seat's activity armed its neighbour")
	}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: "t1", Text: "Bash: go test ./..."}}}})
	if !claudeLS.armed {
		t.Error("a tool call did not arm its seat")
	}

	// An ordinary turn has no live-stat state to arm; the same events must
	// pass through untouched.
	m2 := clearModel()
	m2.redactors = map[model.VendorID]*Redactor{}
	m2.st.Columns[1].Phase = PhaseStreaming
	m2.turn = &turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
	}
	m2.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "hi"}})
	if m2.dueArenaRefreshes() != nil {
		t.Error("an ordinary turn launched a live stat read")
	}
}

// TestCollectArenaStatSeesNewFilesZeroAndError pins the interim read's own
// mechanics against a real repository: `add -N` keeps a new-file-only attempt
// from reading as a false zero mid-race, an untouched tree is a clean
// measured zero, and an unreadable tree reports rather than pretends.
func TestCollectArenaStatSeesNewFilesZeroAndError(t *testing.T) {
	ws := gitRepo(t)
	_, base, trees, _, _, err := arenaSetup(context.Background(), ws, 9, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]

	stat, errLine := collectArenaStat(tree, base)
	if errLine != "" || strings.TrimSpace(stat) != "" {
		t.Errorf("an untouched worktree should read as a clean zero: stat=%q err=%q", stat, errLine)
	}

	if err := os.WriteFile(filepath.Join(tree, "mid-race.go"), []byte("package mid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stat, errLine = collectArenaStat(tree, base)
	if errLine != "" {
		t.Fatalf("read failed: %s", errLine)
	}
	if !strings.Contains(stat, "mid-race.go") {
		t.Errorf("a created file is missing from the live stat — the false-zero bug, mid-race:\n%s", stat)
	}

	if _, errLine = collectArenaStat(filepath.Join(ws, "no-such-tree"), base); errLine == "" {
		t.Error("an unreadable tree reported no error — degraded dressed as zero")
	}
}
