package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestParseRoomCommand is the PARSER's contract, which is narrower than the
// room's: leading whitespace is still trimmed here, and roomCommand no longer
// reaches this function when a draft carries any — a leading space is the escape
// hatch that sends a slash-leading brief to the vendors (§9.31,
// TestALeadingSpaceSendsASlashBriefToTheVendors). The trimming that still
// matters is the trailing kind.
func TestParseRoomCommand(t *testing.T) {
	cases := []struct {
		draft string
		arg   string
		ok    bool
	}{
		{"/cd", "", true},
		{"/cd kb-agent", "kb-agent", true},
		{"  /cd   ../elsewhere  ", "../elsewhere", true},
		// Not commands: the vocabulary stolen from the conversation is exactly
		// one verb, and nothing that merely resembles it.
		{"/cdx", "", false},
		{"/CD kb-agent", "", false},
		{"tell me about /cd", "", false},
		{"@claude /cd kb-agent", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		arg, ok := parseCommand(c.draft, "/cd")
		if ok != c.ok || arg != c.arg {
			t.Errorf("parseCommand(%q, \"/cd\") = %q, %v — want %q, %v", c.draft, arg, ok, c.arg, c.ok)
		}
	}
}

// cdRoom builds a model whose workspace is a real directory with a real
// sibling, because /cd resolution answers with os.Stat and a test against
// invented paths would pass on any string logic at all.
func cdRoom(t *testing.T) (*Model, string, string) {
	t.Helper()
	parent := t.TempDir()
	a := filepath.Join(parent, "telltale")
	b := filepath.Join(parent, "kb-agent")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	m := newWithBrief(Options{Dir: a}, Brief{}, GateHook{}, Reattachment{})
	return m, a, b
}

func TestCdMovesTheRoomToASibling(t *testing.T) {
	m, _, b := cdRoom(t)
	m.setDraft("/cd kb-agent")
	if !m.roomCommand() {
		t.Fatal("/cd was not recognised as a room command")
	}
	if !sameDir(m.st.Workspace, b) {
		t.Errorf("workspace = %q, want the sibling %q", m.st.Workspace, b)
	}
	if m.st.Draft != "" {
		t.Errorf("the draft survived a successful /cd: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "next turn") {
		t.Errorf("the notice does not say when seats move: %q", m.st.Notice)
	}
}

func TestCdPrefersAChildOverASibling(t *testing.T) {
	m, a, _ := cdRoom(t)
	// A directory that exists BOTH under the workspace and beside it: the
	// shell-like reading (relative to here) has to win, or /cd would jump
	// somewhere a shell would not.
	if err := os.Mkdir(filepath.Join(a, "kb-agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.setDraft("/cd kb-agent")
	m.roomCommand()
	if want := filepath.Join(a, "kb-agent"); !sameDir(m.st.Workspace, want) {
		t.Errorf("workspace = %q, want the child %q", m.st.Workspace, want)
	}
}

func TestCdUnknownDirectoryKeepsTheDraft(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.setDraft("/cd no-such-repo")
	m.roomCommand()
	if !sameDir(m.st.Workspace, a) {
		t.Errorf("a failed /cd moved the room anyway: %q", m.st.Workspace)
	}
	if m.st.Draft != "/cd no-such-repo" {
		t.Errorf("the draft was discarded on a failed /cd: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "no-such-repo") {
		t.Errorf("the notice does not name what was not found: %q", m.st.Notice)
	}
}

func TestCdRefusedMidTurn(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.turn = &turnState{cancel: func() {}, live: map[model.VendorID]bool{}}
	m.setDraft("/cd kb-agent")
	m.roomCommand()
	if !sameDir(m.st.Workspace, a) {
		t.Error("/cd moved the room under a turn in flight")
	}
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("the refusal does not say why: %q", m.st.Notice)
	}
}

func TestCdAloneNamesTheCurrentWorkspace(t *testing.T) {
	m, _, _ := cdRoom(t)
	m.setDraft("/cd")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "/cd <dir>") {
		t.Errorf("bare /cd does not explain itself: %q", m.st.Notice)
	}
}

func TestCdExpandsALeadingTilde(t *testing.T) {
	m, _, b := cdRoom(t)
	m.st.Home = filepath.Dir(b)
	m.setDraft("/cd ~/kb-agent")
	m.roomCommand()
	if !sameDir(m.st.Workspace, b) {
		t.Errorf("workspace = %q, want %q via ~", m.st.Workspace, b)
	}
}

func TestCdToTheSameDirectoryIsANoOp(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.setDraft("/cd " + a)
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "already") {
		t.Errorf("a no-op /cd was not called one: %q", m.st.Notice)
	}
}

// --- the move reaches the file when it happens ----------------------------

// dispatchedCdRoom is cdRoom for the PERSISTENCE half of /cd. It adds the two
// things a write needs and cdRoom deliberately has not got: a redirected home,
// so the save lands in a temp directory rather than the operator's own
// ~/.telltale/council, and one completed turn, because saveRoom writes nothing
// at turn 0 and a room that never dispatched cannot witness a write either way.
func dispatchedCdRoom(t *testing.T) (*Model, string, string) {
	t.Helper()
	tempHome(t)
	m, a, b := cdRoom(t)
	m.st.Turn = 1
	m.sessions[model.VendorClaude] = "claude-sess-1"
	return m, a, b
}

// nothingWasSaved asserts the room file does not describe a room. Used by the
// refusal cases, where the claim is an ABSENCE of a write — a save that
// happened would be visible here as a usable room.
func nothingWasSaved(t *testing.T) {
	t.Helper()
	re, err := LoadRoom()
	if err != nil {
		return
	}
	if re.Active() {
		t.Fatalf("a refused /cd wrote the room file anyway: workspace %q", re.Room.Workspace)
	}
}

// TestCdIsPersistedWhenItHappens is the crash the old behaviour lost.
//
// A `/cd` is a deliberate statement about where the room is, and it used to
// reach room.json only when something ELSE wrote: the next completed turn, or
// teardown. So the move survived a clean quit and did not survive a closed
// terminal or a machine that went down — the same failure the per-turn save
// exists to prevent for session ids, on the other half of the room's SHAPE.
//
// Read off disk rather than off m.st, for roster_test.go's reason: a workspace
// held in memory is exactly the bug.
func TestCdIsPersistedWhenItHappens(t *testing.T) {
	m, _, b := dispatchedCdRoom(t)
	m.setDraft("/cd " + b)
	if !m.roomCommand() {
		t.Fatal("/cd was not recognised as a room command")
	}
	if got := savedNow(t).Workspace; !sameDir(got, b) {
		t.Errorf("the file says %q, the room moved to %q", got, b)
	}
}

// TestARefusedCdWritesNothing keeps the refusal semantics the write is layered
// over. Each case below leaves the room where it was, so each must leave the
// file where it was too — a write on a refusal would refresh SavedAt, the age a
// reattach shows, for a room that moved nowhere.
//
// The unknown path is the one worth naming: it is refused by resolveCD BEFORE
// the workspace is assigned, so there is no window in which a bad path is
// persisted and then corrected.
func TestARefusedCdWritesNothing(t *testing.T) {
	t.Run("an unknown directory", func(t *testing.T) {
		m, _, _ := dispatchedCdRoom(t)
		m.setDraft("/cd no-such-repo")
		m.roomCommand()
		nothingWasSaved(t)
	})
	t.Run("a turn in flight", func(t *testing.T) {
		m, _, b := dispatchedCdRoom(t)
		m.turn = &turnState{cancel: func() {}, live: map[model.VendorID]bool{}}
		m.setDraft("/cd " + b)
		m.roomCommand()
		nothingWasSaved(t)
	})
	t.Run("the directory the room is already in", func(t *testing.T) {
		m, a, _ := dispatchedCdRoom(t)
		m.setDraft("/cd " + a)
		m.roomCommand()
		nothingWasSaved(t)
	})
	t.Run("bare /cd, which only reports", func(t *testing.T) {
		m, _, _ := dispatchedCdRoom(t)
		m.setDraft("/cd")
		m.roomCommand()
		nothingWasSaved(t)
	})
}

// TestCdBeforeTheFirstTurnWritesNothing is the roster's rule reaching /cd
// unchanged, and it is stated as a test rather than left to be discovered:
// saveRoom returns at turn 0, so a `/cd` typed before the first brief rides out
// on that brief's own save. A room opened in the wrong directory and quit still
// drops no file into ~/.telltale/council.
func TestCdBeforeTheFirstTurnWritesNothing(t *testing.T) {
	m, _, b := dispatchedCdRoom(t)
	m.st.Turn = 0
	m.setDraft("/cd " + b)
	m.roomCommand()
	nothingWasSaved(t)
}

// fakeSession stands in for a live vendor process. See seatSession: the /cd
// respawn kills a RUNNING process, and this is how the branch is watched
// without spawning one.
type fakeSession struct {
	alive  bool
	killed bool
}

func (f *fakeSession) SendTurn([][]byte) error  { return nil }
func (f *fakeSession) SendAside([][]byte) error { return nil }
func (f *fakeSession) Kill()                    { f.killed = true; f.alive = false }
func (f *fakeSession) Alive() bool              { return f.alive }

// TestAMovedRoomRespawnsThePersistentSeatOnItsOwnThread is the Claude-seat
// half of /cd. cwd is fixed at spawn — the stream-json envelope has no cwd
// field, and the documented mid-conversation move is respawn with --resume —
// so the live process pinned to the old directory is killed and the earned
// session id is spent on the replacement, through the SAME one-attempt
// probation rule the restored ids use.
func TestAMovedRoomRespawnsThePersistentSeatOnItsOwnThread(t *testing.T) {
	m, _, b := cdRoom(t)
	m.sessions[model.VendorClaude] = "claude-sess-1"
	old := &fakeSession{alive: true}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: old, sent: 2, dir: m.st.Workspace}

	m.setDraft("/cd " + b)
	if !m.roomCommand() {
		t.Fatal("/cd was not handled")
	}
	if old.killed {
		t.Error("/cd killed the seat eagerly — the respawn is lazy, on the next dispatch")
	}

	seat := &recordingSeat{}
	c := &Column{
		Vendor: model.VendorClaude, Avail: AvailInstalled,
		Binary: filepath.Join(t.TempDir(), "telltale-no-such-binary"),
	}
	// The launch fails on a missing binary; what is being watched is what the
	// attempt DID: killed the old process, spent the id on a resume in the new
	// directory, and put the thread on probation.
	if _, _, err := m.seatProcess(seat, c); err == nil {
		t.Fatal("a launch against a missing binary unexpectedly succeeded")
	}
	if !old.killed {
		t.Error("the old process survived the move")
	}
	if seat.resumeCalls != 1 || seat.lastID != "claude-sess-1" {
		t.Errorf("resume calls = %d id %q, want 1 spend of the earned id",
			seat.resumeCalls, seat.lastID)
	}
	if !m.unproven[model.VendorClaude] {
		t.Error("the moved thread is not on probation")
	}
	if id := m.resumeIDs[model.VendorClaude]; id != "" {
		t.Errorf("the id survived its one attempt: %q", id)
	}
}

// TestAStaleExitDoesNotFailTheLiveSeat is the review finding that would have
// shipped: a killed predecessor emits one terminal KindDone naming only the
// VENDOR, into the room-lifetime channel, and the turn that follows a /cd
// would have drained it and attributed it to the replacement — failing the
// live turn, dropping the live process from procs (leaving it running,
// invisibly), and discarding the earned thread through the probation rule.
// The guard: a process that exited cannot be Alive, so a terminal event
// arriving while the seat's current process IS alive belongs to a
// predecessor and is ignored. The same applies to a seat that died while
// the room was idle, whose exit sat queued until the next turn.
func TestAStaleExitDoesNotFailTheLiveSeat(t *testing.T) {
	m, _, _ := cdRoom(t)
	live := &fakeSession{alive: true}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: live, sent: 1, dir: m.st.Workspace}
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.st.Columns = []Column{{
		Vendor: model.VendorClaude, Label: "Claude Code",
		Avail: AvailInstalled, Phase: PhaseStreaming, Body: "half an answer",
	}}
	m.turn = &turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{model.VendorClaude: true},
	}

	// The predecessor's exit, and a stale process-level error for good measure.
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindDone},
		{Vendor: model.VendorClaude, Kind: runner.KindError,
			Note: "exit status 1", ExitCode: 1},
	})

	c := m.st.Columns[0]
	if c.Phase != PhaseStreaming {
		t.Errorf("phase = %v — a stale exit retired the live turn", c.Phase)
	}
	if _, ok := m.procs[model.VendorClaude]; !ok {
		t.Error("a stale exit dropped the LIVE process from procs")
	}
	if m.sessions[model.VendorClaude] != "claude-sess-1" {
		t.Error("a stale exit cost the seat its earned thread")
	}
	if m.turn == nil {
		t.Error("a stale exit ended the turn")
	}

	// And the genuine death still lands: once the current process is not
	// alive, the same event retires the column the way it always did.
	live.alive = false
	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})
	if m.st.Columns[0].Phase != PhaseFailed {
		t.Errorf("phase = %v — a real mid-turn death was ignored", m.st.Columns[0].Phase)
	}
}

// TestCdTildeWithNoHomeIsRefused: expanding ~ against an empty home would
// resolve to the current workspace and report a move that never happened.
func TestCdTildeWithNoHomeIsRefused(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.st.Home = ""
	m.setDraft("/cd ~/kb-agent")
	m.roomCommand()
	if !sameDir(m.st.Workspace, a) {
		t.Error("a homeless ~ still moved the room")
	}
	if !strings.Contains(m.st.Notice, "home directory is unknown") {
		t.Errorf("the refusal does not say why: %q", m.st.Notice)
	}
}

// TestRunRejectsAMissingCdDirectory is the LoadBrief discipline on --cd: a
// typed path that is not a directory is a plain error before the alternate
// screen, not four seats failing their first turn.
//
// The saved room is PLANTED, and pointed at a workspace that does not exist.
// Two reasons, and the second is why this test exists in the shape it does.
// The first is coverage: --cd has to win over a restored workspace, and a room
// whose own directory is also gone is the case where a bug could answer with
// the saved-directory sentence instead of the typed-path refusal. The second is
// hermeticity — the earlier version read whatever the developer's real
// ~/.telltale/council held, so its verdict moved with the machine (see
// tempHome). Planting the state makes the stale-workspace case the one that is
// always exercised, rather than the one that shows up on a bad day.
func TestRunRejectsAMissingCdDirectory(t *testing.T) {
	home := tempHome(t)
	if err := SaveRoom(savedRoom(filepath.Join(home, "renamed-away"))); err != nil {
		t.Fatal(err)
	}
	err := Run(Options{Dir: filepath.Join(t.TempDir(), "gone")})
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err = %v, want the --cd refusal", err)
	}
}

// --- /retry: the last brief, sent again to the seats that owe an answer -----
//
// Every assertion below is about a COST: what the composer is about to bill,
// and what actually spawned. The verb's whole shape is that it arms and does
// not dispatch, so "nothing spawned" is the claim on the arming half and "only
// these seats spawned" is the claim on the sending half.

// retryRoom is a four-seat room whose last turn is OVER, carrying one column
// for each of the four ways a seat ends a turn (§9.37's 2026-08-17 amendment):
// answered, failed, given up, and sat out.
//
// The sat-out seat keeps the turn number of the last turn it TOOK, because that
// is what dispatch leaves behind for a seat it never started — startTurn is not
// called on it. A fixture that stamped turn 3 on it would be testing a state
// the room cannot produce.
func retryRoom(t *testing.T) *Model {
	t.Helper()
	m := flowRoom(t, true)
	m.st.Turn = 3
	const brief = "the brief that half landed"
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		switch c.Vendor {
		case model.VendorClaude:
			c.TurnN, c.Phase, c.Prompt, c.Body = 3, PhaseDone, brief, "an answer"
		case model.VendorCodex:
			c.TurnN, c.Phase, c.Prompt = 3, PhaseFailed, brief
			c.Note = "exit status 1"
		case model.VendorAntigravity:
			c.TurnN, c.Phase, c.Prompt = 3, PhaseCancelled, brief
			c.Note = "given up after 4m12s — nothing had arrived, and its process is dead"
		case model.VendorCursor:
			c.TurnN, c.Phase, c.Prompt, c.Skipped = 2, PhaseDone, "an earlier brief", true
			c.Note = "not addressed in turn 3"
		}
	}
	return m
}

// TestRetryAddressesOnlyTheSeatsThatDidNotAnswer is the verb's contract in one
// test: the brief comes back unchanged, the mentions name the failed and the
// given-up seat, and the two that have nothing to re-send — the one that
// answered and the one that sat the turn out — are not billed.
func TestRetryAddressesOnlyTheSeatsThatDidNotAnswer(t *testing.T) {
	log := countSpawns(t)
	m := retryRoom(t)
	m.setDraft("/retry")

	if !m.roomCommand() {
		t.Fatal("/retry was not recognised as a room command")
	}
	if log.n() != 0 {
		t.Fatalf("/retry spawned %d process(es) — it arms the composer, it does not dispatch", log.n())
	}
	if m.st.Turn != 3 {
		t.Errorf("/retry counted itself as a turn: %d", m.st.Turn)
	}
	if want := "@codex @agy the brief that half landed"; m.st.Draft != want {
		t.Errorf("draft = %q, want %q", m.st.Draft, want)
	}
	// The bill the footer is about to print, read through the arithmetic
	// dispatch itself gates on rather than by counting the words in the draft.
	if n := m.st.SeatsIn(m.st.Route); n != 2 {
		t.Errorf("the route prices %d seats, want the 2 that did not answer", n)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		if m.st.Route.addresses(v) {
			t.Errorf("%s has no answer owing and the re-send addresses it anyway", v)
		}
	}
	if !strings.Contains(m.st.Notice, "codex, agy") || !strings.Contains(m.st.Notice, "turn 3") {
		t.Errorf("the notice does not say who owes an answer, and for which turn: %q", m.st.Notice)
	}
}

// TestAMeasuredZeroCountsAsAnAnswer. A seat that finished and streamed nothing
// says so — "[Turn completed with 0 text chunks streamed]" under `done` — and
// that is a MEASURED zero, not a missing reply (§4a.1). Re-sending on it would
// be the room overruling a vendor's honest empty answer, and it would bill a
// seat that already did the work.
func TestAMeasuredZeroCountsAsAnAnswer(t *testing.T) {
	m := retryRoom(t)
	for i := range m.st.Columns {
		if m.st.Columns[i].Vendor == model.VendorClaude {
			m.st.Columns[i].Body = "[Turn completed with 0 text chunks streamed]"
		}
	}
	m.setDraft("/retry")
	m.roomCommand()

	if m.st.Route.addresses(model.VendorClaude) {
		t.Errorf("a measured empty answer was re-sent as though the seat had not answered: %q", m.st.Draft)
	}
}

// TestRetryRefusesWithNoBriefOnRecord: turn 0. Nothing has been dispatched, so
// there is nothing to send again, and the refusal says which of the two it is
// rather than arming an empty composer.
func TestRetryRefusesWithNoBriefOnRecord(t *testing.T) {
	m := flowRoom(t, true)
	m.setDraft("/retry")
	m.roomCommand()

	if !strings.Contains(m.st.Notice, "no brief to re-send") {
		t.Errorf("the refusal does not say what is missing: %q", m.st.Notice)
	}
	if m.st.Draft != "" {
		t.Errorf("a refusal that only reports left the verb in the composer: %q", m.st.Draft)
	}
	if m.st.Route.addresses(model.VendorCodex) {
		t.Error("a refused /retry routed a draft anyway")
	}
}

// TestRetryRefusesWhenEverySeatAnswered. The other end of the same question:
// the turn is on record and every seat in it replied, so a re-send would bill
// the room for answers it already has.
func TestRetryRefusesWhenEverySeatAnswered(t *testing.T) {
	m := retryRoom(t)
	for i := range m.st.Columns {
		if m.st.Columns[i].TurnN == 3 {
			m.st.Columns[i].Phase = PhaseDone
		}
	}
	m.setDraft("/retry")
	m.roomCommand()

	if !strings.Contains(m.st.Notice, "every seat answered turn 3") {
		t.Errorf("the refusal does not say why there is nothing to send: %q", m.st.Notice)
	}
	if m.st.Draft != "" {
		t.Errorf("a refusal that only reports left the verb in the composer: %q", m.st.Draft)
	}
}

// TestRetryRefusedMidTurnKeepsTheDraft. The phases this verb reads are not
// settled while a turn runs, so it refuses — and it KEEPS the draft, which is
// postureCommand's rule: the command is still what the operator wants, one turn
// later.
func TestRetryRefusedMidTurnKeepsTheDraft(t *testing.T) {
	m := retryRoom(t)
	m.turn = &turnState{cancel: func() {}, live: map[model.VendorID]bool{}}
	m.setDraft("/retry")
	m.roomCommand()

	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("the refusal does not say why: %q", m.st.Notice)
	}
	if m.st.Draft != "/retry" {
		t.Errorf("the draft was thrown away on a refusal the next turn undoes: %q", m.st.Draft)
	}
}

// TestRetryIsOnlyABareCommand is §9.17's bare-word rule reaching this verb:
// "/retry the failing test" is a brief, and a verb that took it as an argument
// would run a re-send and discard the sentence the operator typed. It is
// refused instead, which costs nothing and names the escape.
func TestRetryIsOnlyABareCommand(t *testing.T) {
	log := countSpawns(t)
	m := retryRoom(t)
	m.setDraft("/retry the failing test")

	if !m.roomCommand() {
		t.Fatal("a slash-leading draft was neither run nor refused")
	}
	if !strings.Contains(m.st.Notice, "no room command") {
		t.Errorf("/retry took an argument it does not have: %q", m.st.Notice)
	}
	if m.st.Draft != "/retry the failing test" {
		t.Errorf("the refused draft was rewritten: %q", m.st.Draft)
	}
	if log.n() != 0 {
		t.Errorf("a refused draft spawned %d process(es)", log.n())
	}
}

// TestRetryIsNamedInTheWalkedCommandTable. The refusal reads roomVerbs and
// §9.31's rule is that the table is walked rather than copied into a string —
// so the claim here is registration: a verb missing from that table is a verb
// the room refuses, and one present in it is taught by the refusal for free.
func TestRetryIsNamedInTheWalkedCommandTable(t *testing.T) {
	registered := false
	for _, rc := range roomVerbs() {
		if rc.verb == "/retry" {
			registered = true
		}
	}
	if !registered {
		t.Fatal("/retry is not in roomVerbs, so the room refuses its own word")
	}

	m := flowRoom(t, true)
	m.setDraft("/nosuchverb")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "/retry") {
		t.Errorf("the refusal does not teach /retry: %q", m.st.Notice)
	}
}

// TestTheReSendSpawnsOnlyForTheSeatsThatOweAnAnswer is the dispatch-level half,
// asserted on what SPAWNED rather than on the draft the verb built.
//
// The fixture derives the two sets rather than naming vendors: which seats the
// registry drives as one-shot processes and which as long-lived ones is not this
// test's claim, and hardcoding it would make a registry change look like a
// /retry bug. One batch seat is cut with `x`, the rest fail, and every
// persistent seat answers — so the seats owing an answer are exactly the batch
// seats, and exactly they should spawn.
//
// No vendor is started: countSpawns stubs all three spawn vars, per CLAUDE.md's
// council-test rule.
func TestTheReSendSpawnsOnlyForTheSeatsThatOweAnAnswer(t *testing.T) {
	m, oneShots, live := ordinaryTurn(t)

	cut := oneOf(t, oneShots, "batch seat")
	focusSeatOn(t, m, cut)
	m.key(key("x"))
	m.key(key("y"))
	for v := range oneShots {
		if v == cut {
			continue
		}
		m.applyEvents([]runner.Event{{
			Vendor: v, Kind: runner.KindError, Note: "exit status 1", ExitCode: 1,
		}})
	}
	for v := range live {
		m.applyEvents([]runner.Event{{
			Vendor: v, Kind: runner.KindMeta, EndsTurn: true, Text: "an answer",
		}})
	}
	if m.turn != nil {
		t.Fatal("fixture: the turn never ended, so there is no finished turn to re-send")
	}

	// Counted from here, so the first turn's own spawns are not in the number.
	log := countSpawns(t)
	m.setDraft("/retry")
	enter(m)
	if log.n() != 0 {
		t.Fatalf("/retry spawned %d process(es) before enter was pressed", log.n())
	}
	enter(m)

	if log.n() != len(oneShots) {
		t.Fatalf("the re-send spawned %d process(es), want %d — one per seat owing an answer: %+v",
			log.n(), len(oneShots), log.specs)
	}
	for _, spec := range log.specs {
		if _, owed := oneShots[spec.Vendor]; !owed {
			t.Errorf("the re-send spawned %s, which already answered turn 1", spec.Vendor)
		}
	}
	for v := range live {
		c := m.column(v)
		if c.TurnN != 1 || !c.Skipped {
			t.Errorf("%s answered turn 1 and was billed again: turn %d, skipped %v", v, c.TurnN, c.Skipped)
		}
	}
	for v := range oneShots {
		if c := m.column(v); c.TurnN != 2 {
			t.Errorf("%s owed an answer and the re-send missed it: turn %d", v, c.TurnN)
		}
	}
	// The text is the same brief, unchanged — the property that keeps this verb
	// clear of §9.17's unfiled re-briefing question.
	for v := range oneShots {
		if got := m.column(v).Prompt; got != "an ordinary brief" {
			t.Errorf("%s was re-sent %q, not the brief it was given", v, got)
		}
	}
}

// TestAnUnmovedRoomKeepsItsProcess is the other side: seatProcess must not
// respawn a seat whose directory still matches — that would pay a session
// init per turn and undo the sixth amendment.
func TestAnUnmovedRoomKeepsItsProcess(t *testing.T) {
	m, _, _ := cdRoom(t)
	sess := &fakeSession{alive: true}
	p := &seatProc{wire: claudeWire(), sess: sess, sent: 1, dir: m.st.Workspace}
	m.procs[model.VendorClaude] = p

	seat := &recordingSeat{}
	c := &Column{Vendor: model.VendorClaude, Avail: AvailInstalled, Binary: "claude"}
	got, note, err := m.seatProcess(seat, c)
	if err != nil || got != p || note != "" {
		t.Fatalf("seatProcess = %v, %q, %v — want the existing process untouched", got, note, err)
	}
	if sess.killed {
		t.Error("an unmoved seat's process was killed")
	}
	if seat.sessionCalls+seat.resumeCalls != 0 {
		t.Error("an unmoved seat respawned anyway")
	}
}
