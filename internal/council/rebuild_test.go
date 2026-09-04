package council

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The room-open rebuild (design.md §9.52, rung 2). Every test here stubs the
// spawn vars through countSpawns, which is the package's standing rule: a
// council test never starts a vendor.

// rebuiltRoom is a reattached room with two seats holding a saved thread.
func rebuiltRoom(t *testing.T) *Model {
	t.Helper()
	m := flowRoom(t, true)
	m.st.Reattached = Reattach{Turn: 7, SavedAt: time.Now().Add(-2 * time.Hour)}
	m.st.Turn = 7
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		m.sessions[v] = "sess-" + string(v)
		m.resumeIDs[v] = "sess-" + string(v)
		m.unproven[v] = true
		m.column(v).Restored = true
	}
	return m
}

func TestRebuildLaunchesEveryRestorableSeat(t *testing.T) {
	log := countSpawns(t)
	m := rebuiltRoom(t)

	m.startRebuild()

	if log.n() != 2 {
		t.Fatalf("%d seats launched, want 2: %+v", log.n(), log.specs)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		if _, ok := m.procs[v]; !ok {
			t.Errorf("%s has no process after the rebuild", v)
		}
		if got := m.column(v).Note; got != "rebuilding" {
			t.Errorf("%s does not report that it is rebuilding: %q", v, got)
		}
	}
	// The room fact and its explanation go in the notice, ONCE. The columns say
	// the one word that is true of each seat on its own; the sentence behind it
	// was identical on all of them, so four copies of it left the frame with the
	// density pass. The COUNT leads the notice so it survives a truncation the
	// explanation does not.
	if !strings.HasPrefix(m.st.Notice, "rebuilding 2 seats") {
		t.Errorf("the notice does not count the seats: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "loads each saved thread") {
		t.Errorf("the notice does not explain what a rebuild is: %q", m.st.Notice)
	}
}

// The rebuild changes WHEN a seat is launched, never WHETHER. A seat with no
// saved id, a vendor this machine cannot run, and a seat that was never
// restored are all left alone — and none of the three is a failure.
func TestRebuildSkipsWhatItHasNoIdFor(t *testing.T) {
	log := countSpawns(t)
	m := rebuiltRoom(t)
	// Restored, with an id, and no binary here.
	m.column(model.VendorCodex).Restored = true
	m.resumeIDs[model.VendorCodex] = "sess-codex"
	m.column(model.VendorCodex).Avail = AvailNotInstalled
	// Restored, installed, and nothing saved for it.
	m.column(model.VendorAntigravity).Restored = true

	m.startRebuild()

	if log.n() != 2 {
		t.Fatalf("%d seats launched, want 2: %+v", log.n(), log.specs)
	}
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity} {
		if _, ok := m.procs[v]; ok {
			t.Errorf("%s was launched with nothing to rebuild", v)
		}
		if got := m.column(v).Note; got != "" {
			t.Errorf("%s was reported as a rebuild failure: %q", v, got)
		}
	}
}

// A room with nothing saved must launch nothing at all. Opening a conversation
// the operator has not asked for is the one way this rung could cost money it
// was not given.
func TestRebuildDoesNothingInAColdRoom(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	if cmd := m.rebuildCmd(); cmd != nil {
		t.Error("a cold room armed a rebuild")
	}
	m.startRebuild()

	if log.n() != 0 {
		t.Fatalf("%d seats launched in a cold room: %+v", log.n(), log.specs)
	}
	if m.st.Notice != "" {
		t.Errorf("a cold room says something about a rebuild: %q", m.st.Notice)
	}
}

// THE RUNG'S WHOLE POINT. A launched process is not a proven thread, and the
// two states must not share a sentence. `rebuilding` may not claim the thread
// came back; only the vendor's own announcement promotes it.
func TestRebuildingAndRebuiltDoNotShareASentence(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()

	launching := m.column(model.VendorClaude).Note
	if strings.Contains(launching, "came back") {
		t.Errorf("a launched process claims the thread came back: %q", launching)
	}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude",
	}})
	settled := m.column(model.VendorClaude).Note

	if launching == settled {
		t.Fatalf("rebuilding and rebuilt render identically: %q", settled)
	}
	if !strings.Contains(settled, "came back") {
		t.Errorf("a settled seat does not say its thread came back: %q", settled)
	}
	// And it says what did NOT come back, which is the half the existing
	// reattach card cannot say.
	if !strings.Contains(settled, "NEW process") {
		t.Errorf("a rebuilt seat does not say the process is new: %q", settled)
	}
	if !strings.Contains(settled, "ended when the room closed") {
		t.Errorf("a rebuilt seat does not say what happened to the old process: %q", settled)
	}
}

// THE COST IS STATED PER SEAT AND BOTH HALVES ARE STATED. The ~25s moved; the
// ~$0.23 did not. Naming only the seconds would read as though the reopen were
// free; naming only the dollars would read as though the room had just spent
// them. It lives on the column rather than in the notice because the notice is
// one truncated line and a cost that vanishes at a hundred columns is not a
// stated cost — and because "$0.23 a seat" is a per-seat fact to begin with.
func TestRebuiltSeatStatesBothHalvesOfTheMeasuredCost(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude"},
		{Vendor: model.VendorCursor, Kind: runner.KindSession, SessionID: "sess-cursor"},
	})

	detail := m.column(model.VendorClaude).NoteDetail
	for _, want := range []string{
		"~25s of startup is spent now instead of on your first brief",
		"still bills its ~$0.23",
		"measured once, on a one-word turn",
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("the rebuilt seat does not say %q:\n%s", want, detail)
		}
	}
	// Neither figure may appear as a counted value: one measurement of one
	// one-word turn, extrapolated, is an estimate and wears a ~.
	if strings.Contains(detail, "$0.46") || strings.Contains(detail, "$0.23 spent") {
		t.Errorf("the detail reports a spend nobody measured:\n%s", detail)
	}
	if strings.Contains(detail, "$0.23)") || !strings.Contains(detail, "~$0.23") {
		t.Errorf("the dollar figure is not marked as an estimate:\n%s", detail)
	}

	// The room fact is the notice's, and it is short enough to survive the line.
	notice := m.st.Notice
	if !strings.Contains(notice, "2/4 seats rebuilt in") {
		t.Errorf("the settled notice does not count the seats:\n%s", notice)
	}
	if !strings.Contains(notice, "NEW processes, not the ones you left") {
		t.Errorf("the settled notice does not say the processes are new:\n%s", notice)
	}
}

// The settled sentence REPLACES the reattach sentence rather than joining it,
// because joined it lands past a hundred columns and the clause the rung exists
// to say is the half that gets cut. The reattach sentence was the whole notice
// for the length of the rebuild, so nothing it carries goes unread.
func TestSettledRebuildKeepsItsOwnClauseWhole(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.st.Notice = "reattached from ~/.telltale/council/room.json — turn 7 was the last, saved 2h ago"
	m.startRebuild()

	// While it runs, the reattach sentence is still the notice and the rebuild
	// adds only a short count after it.
	if !strings.Contains(m.st.Notice, "turn 7 was the last") {
		t.Errorf("the running rebuild dropped the reattach sentence:\n%s", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "rebuilding 2 seats") {
		t.Errorf("the running rebuild does not count its seats:\n%s", m.st.Notice)
	}

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude"},
		{Vendor: model.VendorCursor, Kind: runner.KindSession, SessionID: "sess-cursor"},
	})

	if strings.Contains(m.st.Notice, "reattached from") {
		t.Errorf("the settled notice still carries the reattach sentence:\n%s", m.st.Notice)
	}
	if !strings.HasPrefix(m.st.Notice, "2/4 seats rebuilt in") {
		t.Errorf("the settled notice does not lead with its own fact:\n%s", m.st.Notice)
	}
	// It has to survive a 120-column line with the notice chrome around it.
	if len([]rune(m.st.Notice)) > 100 {
		t.Errorf("the settled notice is %d runes and will be truncated:\n%s",
			len([]rune(m.st.Notice)), m.st.Notice)
	}
}

// A vendor asked to resume that answers somewhere else is §9.43's finding, and
// the rebuild must reach its existing words rather than inventing a second
// sentence for one fact.
func TestRebuildReportsAForkInTheExistingWords(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "a-different-thread",
	}})

	c := m.column(model.VendorClaude)
	if !strings.Contains(c.Note, "thread not restored") {
		t.Errorf("a forked resume does not report as one: %q", c.Note)
	}
	if c.Restored {
		t.Error("a seat that answered in a new conversation is still marked restored")
	}
	if m.sessions[model.VendorClaude] != "a-different-thread" {
		t.Errorf("the new thread was not recorded: %q", m.sessions[model.VendorClaude])
	}
}

// A spawn that failed is a measured failure carrying the vendor's own words,
// and it must not wedge the seat: the next brief takes the ordinary path.
func TestRebuildReportsALaunchThatFailed(t *testing.T) {
	countSpawns(t)
	orig := startSession
	startSession = func(_ context.Context, _ runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (seatSession, error) {
		return nil, errors.New("codex-agent: not authenticated")
	}
	t.Cleanup(func() { startSession = orig })

	m := rebuiltRoom(t)
	m.startRebuild()

	c := m.column(model.VendorClaude)
	if !strings.Contains(c.Note, "could not be rebuilt") {
		t.Errorf("a failed launch is not reported: %q", c.Note)
	}
	if !strings.Contains(c.Note, "not authenticated") {
		t.Errorf("a failed launch drops the vendor's own line: %q", c.Note)
	}
	if !strings.Contains(c.Note, "opens a new session") {
		t.Errorf("a failed launch does not say what happens next: %q", c.Note)
	}
	if _, ok := m.procs[model.VendorClaude]; ok {
		t.Error("a failed launch left a process registered")
	}
}

// A process that dies before it announces anything is a failure, measured off
// the exit rather than off a clock.
func TestRebuildReportsAProcessThatDiedBeforeAnnouncing(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindDone, ExitCode: 1,
		Note: "No conversation found with session ID",
	}})

	c := m.column(model.VendorClaude)
	if !strings.Contains(c.Note, "could not be rebuilt") {
		t.Errorf("a dead process is not reported as a failed rebuild: %q", c.Note)
	}
	if !strings.Contains(c.Note, "No conversation found") {
		t.Errorf("a dead process drops the vendor's own line: %q", c.Note)
	}
}

// The rebuild starts a process and sends nothing. The whole claim about cost
// rests on this: a process that has run no turn has billed nothing.
func TestRebuildSendsNoTurn(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()

	for v, p := range m.procs {
		if p.sent != 0 {
			t.Errorf("%s was handed %d turn(s) by the rebuild", v, p.sent)
		}
		if !p.resumed {
			t.Errorf("%s was not launched on its saved id", v)
		}
	}
}

// The first brief retires the run. startTurn is about to clear the note the
// rebuild wrote, and a run left standing would go on owning events for a seat
// the turn is now driving.
func TestRebuildRetiresOnTheFirstBrief(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()
	if m.rebuild == nil {
		t.Fatal("no rebuild is in flight to retire")
	}

	m.st.Draft = "carry on"
	m.dispatch()

	if m.rebuild != nil {
		t.Error("the rebuild outlived the first brief")
	}
	if m.rebuildOwns(model.VendorClaude) {
		t.Error("the rebuild still owns a seat the turn is driving")
	}
}

// A seat the rebuild owns hands over the moment a turn starts, and hands over
// permanently once it has settled.
func TestRebuildOwnershipIsNarrow(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()

	if !m.rebuildOwns(model.VendorClaude) {
		t.Error("a launching seat is not owned by the rebuild")
	}
	if m.rebuildOwns(model.VendorCodex) {
		t.Error("a seat the rebuild never launched is owned by it")
	}
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude",
	}})
	if m.rebuildOwns(model.VendorClaude) {
		t.Error("a settled seat is still owned by the rebuild")
	}
}

// --- what a rebuilding and a rebuilt room look like ----------------------

// The two goldens exist because `rebuilt` and `survived` rendering alike is the
// regression §9.52 was written to prevent. The strings come from the model
// itself rather than being typed here, so a wording change moves the golden
// instead of silently passing an out-of-date assertion.
func TestRebuildGoldens(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.st.Notice = "reattached from ~/.telltale/council/room.json — turn 7 was the last, saved 2h ago"
	m.startRebuild()

	st := room()
	st.Now = time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC)
	st.Turn = 7
	st.Reattached = Reattach{Turn: 7, SavedAt: st.Now.Add(-2 * time.Hour)}
	st.Columns[0].Restored = true
	st.Columns[1].Restored = true
	st.Notice = m.st.Notice
	st.Columns[0].Note = m.column(model.VendorClaude).Note
	st.Columns[0].NoteCalm = true
	st.Columns[1].Note = m.column(model.VendorCursor).Note
	st.Columns[1].NoteCalm = true
	golden(t, "rebuilding", render(st))

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude"},
		{Vendor: model.VendorCursor, Kind: runner.KindSession, SessionID: "sess-cursor"},
	})
	st.Notice = m.st.Notice
	st.Columns[0].Note = m.column(model.VendorClaude).Note
	st.Columns[0].NoteDetail = m.column(model.VendorClaude).NoteDetail
	st.Columns[1].Note = m.column(model.VendorCursor).Note
	st.Columns[1].NoteDetail = m.column(model.VendorCursor).NoteDetail
	got := render(st)
	golden(t, "rebuilt", got)

	// The distinction the goldens exist for, asserted in words as well as bytes
	// so a careless -update cannot quietly retire it.
	if !strings.Contains(got, "NEW process") {
		t.Error("a rebuilt room does not say its processes are new")
	}
	if strings.Contains(got, "rebuilding this seat") {
		t.Error("a settled room still says it is rebuilding")
	}
	// The measured cost is on screen, not merely in a field.
	if !strings.Contains(got, "~$0.23") {
		t.Error("a rebuilt room does not show the measured cost")
	}
	if !strings.Contains(got, "2/4 seats rebuilt in") {
		t.Error("the settled notice is cut off before it names the seats")
	}
}

// The closing sentence is written ONCE. settleRebuild is reached from two
// places — an event batch that empties the running set, and the spinner's
// backstop — and both fire again afterwards. A run that re-settled would
// overwrite whatever the operator's next action put in the notice, on every
// tick, for the life of the room.
func TestSettledRebuildDoesNotKeepReclaimingTheNotice(t *testing.T) {
	countSpawns(t)
	m := rebuiltRoom(t)
	m.startRebuild()
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-claude"},
		{Vendor: model.VendorCursor, Kind: runner.KindSession, SessionID: "sess-cursor"},
	})
	if !strings.Contains(m.st.Notice, "seats rebuilt") {
		t.Fatalf("the rebuild never settled: %q", m.st.Notice)
	}

	// Whatever the operator does next owns the notice from here.
	m.st.Notice = "a later sentence"
	m.settleRebuild()
	m.settleDeadRebuilds()

	if m.st.Notice != "a later sentence" {
		t.Errorf("the settled rebuild reclaimed the notice: %q", m.st.Notice)
	}
}
