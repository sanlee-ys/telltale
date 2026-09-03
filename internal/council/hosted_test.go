package council

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The hosted room's client half (hosted.go, design.md §7.31), over a fake
// link: what enter sends, what ctrl+c stops, what /detach waits for, what is
// refused, and what the frames draw. Nothing here touches a pipe; the real one
// is in hosted_e2e_test.go.

// fakeLink records every frame the Model sends and hands back the frames a
// test queues. NextFrame blocks on the queue, so a test that wants a frame
// folded calls applyHostFrame directly with it rather than going through
// waitHost.
type fakeLink struct {
	pid        int
	dispatches []fakeDispatch
	interrupts [][]model.VendorID
	detaches   int
	closed     bool
	detached   bool
	frames     chan councilhost.Frame
	dead       error
}

type fakeDispatch struct {
	prompt string
	seats  []model.VendorID
}

func (f *fakeLink) HostPID() int { return f.pid }
func (f *fakeLink) Dispatch(prompt string, seats ...model.VendorID) error {
	if f.dead != nil {
		return f.dead
	}
	f.dispatches = append(f.dispatches, fakeDispatch{prompt: prompt, seats: seats})
	return nil
}
func (f *fakeLink) Interrupt(seats ...model.VendorID) error {
	if f.dead != nil {
		return f.dead
	}
	f.interrupts = append(f.interrupts, seats)
	return nil
}
func (f *fakeLink) RequestDetach() error {
	if f.dead != nil {
		return f.dead
	}
	f.detaches++
	return nil
}
func (f *fakeLink) NextFrame() (councilhost.Frame, error) {
	if f.dead != nil {
		return councilhost.Frame{}, f.dead
	}
	return <-f.frames, nil
}
func (f *fakeLink) CloseDetached() error { f.detached = true; return nil }
func (f *fakeLink) Close() error         { f.closed = true; return nil }

// hostedFixture is the room a five-seat reference machine's host holds after
// it opened: four drivable seats, two of them on their measured batch adapter,
// and cursor refused in the host's own words. Read posture, no turn yet.
func hostedFixture() councilhost.Room {
	return councilhost.Room{
		Version:   councilhost.RoomVersion,
		Workspace: "/home/dev/code/telltale",
		Posture:   "read",
		Seats: []councilhost.Seat{
			{Vendor: model.VendorClaude, Binary: "/usr/bin/claude", Phase: councilhost.PhaseIdle, Drivable: true, Persistent: true},
			{Vendor: model.VendorCodex, Binary: "/usr/bin/codex", Phase: councilhost.PhaseIdle, Drivable: true, FellBack: true},
			{Vendor: model.VendorAntigravity, Binary: "/usr/bin/agy", Phase: councilhost.PhaseIdle, Drivable: true, Persistent: true},
			{Vendor: model.VendorCursor, Binary: "/usr/bin/cursor-agent", Phase: councilhost.PhaseUndrivable, Drivable: false,
				Note: "this seat speaks a request/response protocol the host does not drive yet"},
			{Vendor: model.VendorGrok, Binary: "/usr/bin/grok", Phase: councilhost.PhaseIdle, Drivable: true, FellBack: true},
		},
	}
}

// hostedTurnFixture is the same room two turns in: claude streaming with a
// trace, codex done with turn 1 filed behind it, agy skipped this turn, grok
// failed on its exit.
func hostedTurnFixture(now time.Time) councilhost.Room {
	r := hostedFixture()
	r.Turn = 2
	cost, one := 0.0123, 1
	for i := range r.Seats {
		s := &r.Seats[i]
		switch s.Vendor {
		case model.VendorClaude:
			s.Phase, s.Turn, s.Prompt = councilhost.PhaseStreaming, 2, "tighten the parser's error path"
			s.Body = "Reading the parser first.\n\nThe error path drops the line number;"
			s.Acts = []councilhost.Act{
				{ID: "t1", Text: "Read: internal/parser.go", Status: councilhost.ActOK},
				{ID: "t2", Text: "Bash: go test ./internal/parser", Status: councilhost.ActFailed, Detail: "exit status 1"},
				{ID: "t3", Text: "Edit: internal/parser.go"},
			}
			s.Started = now.Add(-30 * time.Second)
			s.CostUSD, s.CostSession = &cost, true
			s.History = []councilhost.TurnRecord{{N: 1, Prompt: "say your seat name", Body: "Claude Code.",
				Phase: councilhost.PhaseDone, Elapsed: 4 * time.Second}}
		case model.VendorCodex:
			s.Phase, s.Turn, s.Prompt = councilhost.PhaseDone, 2, "tighten the parser's error path"
			s.Body = "Done: the line number is carried on the error now."
			s.Started, s.Ended, s.Elapsed = now.Add(-40*time.Second), now.Add(-28*time.Second), 12*time.Second
			zero := 0
			s.ExitCode = &zero
			s.History = []councilhost.TurnRecord{{N: 1, Prompt: "say your seat name", Body: "Codex.",
				Phase: councilhost.PhaseDone, Elapsed: 9 * time.Second, ExitCode: &zero}}
		case model.VendorAntigravity:
			s.Turn, s.Prompt, s.Body = 1, "say your seat name", "Antigravity."
			s.Phase = councilhost.PhaseDone
			s.Elapsed = 6 * time.Second
			s.Note, s.Skipped = "not addressed in turn 2", true
		case model.VendorGrok:
			s.Phase, s.Turn, s.Prompt = councilhost.PhaseFailed, 2, "tighten the parser's error path"
			s.Note = "You've hit your usage limit."
			s.NoteDetail = "exit status 1"
			s.ExitCode = &one
			s.Started, s.Ended, s.Elapsed = now.Add(-40*time.Second), now.Add(-38*time.Second), 2*time.Second
		}
	}
	return r
}

// hostedState projects a room the way a hosted client on Windows would, over
// the goldens' own geometry. windows is fixed rather than read from the
// machine, so the posture badges — which differ per platform — draw the same
// bytes on every CI job.
func hostedState(r councilhost.Room) State {
	st := NewState()
	st.Width, st.Height = 120, 24
	st.Home = "/home/dev"
	st.Mode = ModeViewing
	st.Hosted = HostedRoom{PID: 4242}
	st.Now = time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)
	st = stateFromRoom(r, st, true)
	if vis := st.VisibleColumns(); len(vis) > 0 {
		st.Focus = vis[0]
	}
	return st
}

// hostedModel builds a Model over the fake link, at the goldens' geometry.
func hostedModel(t *testing.T, r councilhost.Room, notice string) (*Model, *fakeLink) {
	t.Helper()
	link := &fakeLink{pid: 4242, frames: make(chan councilhost.Frame, 8)}
	m := newHostedModel(Options{}, r, link, notice)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.st.Now = time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)
	return m, link
}

func hostedRoomFrame(r councilhost.Room) hostFrameMsg {
	room := r
	return hostFrameMsg{frame: councilhost.Frame{Kind: councilhost.KindRoom, Room: &room}}
}

// TestHostedStateIsPureOverTheRoom is the property the whole client rests on:
// the same frame over the same previous State builds the same State, twice,
// and renders the same bytes. A rejoining client draws the whole current room
// from its first frame only because this holds.
func TestHostedStateIsPureOverTheRoom(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)
	prev := hostedState(hostedFixture())
	a := stateFromRoom(hostedTurnFixture(now), prev, true)
	time.Sleep(5 * time.Millisecond)
	b := stateFromRoom(hostedTurnFixture(now), prev, true)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("stateFromRoom answered differently for the same room and the same previous State")
	}
	if render(a) != render(b) {
		t.Fatal("the same projected State rendered differently")
	}
}

// TestHostedRoomDrawsTheRoomsOwnColumns pins the idle hosted room: five
// columns as the room draws them, the badge on each, the word `hosted` on the
// header and the pid on the border — and the four seats' postures as this
// machine claims them, with the two fallen-back seats wearing their measured
// adapters' spelling.
func TestHostedRoomDrawsTheRoomsOwnColumns(t *testing.T) {
	st := hostedState(hostedFixture())
	got := render(st)
	golden(t, "hosted-idle", got)
	for _, want := range []string{"hosted", "hosted pid 4242", "READ", "Claude Code", "Codex", "Antigravity", "Grok"} {
		if !strings.Contains(got, want) {
			t.Errorf("the hosted room does not draw %q:\n%s", want, got)
		}
	}
	// The undrivable seat folds out with the host's own reason reachable, and
	// is not drawn as a seat that failed.
	if strings.Contains(got, "failed") {
		t.Errorf("an undrivable seat drew as a failure:\n%s", got)
	}
	golden(t, "hosted-idle-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestHostedTurnDrawsTheTraceTheHistoryAndTheClock pins a hosted room two
// turns in: the streaming seat's clock from State.Now against the host's
// Started, the trace's three outcome marks, a filed turn behind a finished
// seat, a skip line, and a failed seat's two-line card.
func TestHostedTurnDrawsTheTraceTheHistoryAndTheClock(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)
	st := hostedState(hostedTurnFixture(now))
	got := render(st)
	golden(t, "hosted-turn", got)
	// The card wraps at the column's width, so the note is asserted by a
	// fragment that sits inside one row.
	for _, want := range []string{"turn 2", "streaming 30s", "done 12s", "not addressed in turn 2",
		"hit your usage", "exit status 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("the hosted turn does not draw %q:\n%s", want, got)
		}
	}
	// A 25-cell column cannot hold `$0.0123 session` whole, so it draws no
	// figure at all (badgeRow: shown whole or not shown). This frame used to
	// pin `$0.012`, a cut that lost a digit and the word that made it a
	// running total; the figure and its word are asserted whole on the
	// expanded column below, where there is room for them.
	if strings.Contains(got, "$") {
		t.Errorf("the narrow hosted column drew a figure it cannot show whole:\n%s", got)
	}
	wide := st
	wide.Expanded = true
	if got := render(wide); !strings.Contains(got, "$0.0123 session") {
		t.Errorf("the expanded column does not draw the persistent seat's running total with its word:\n%s", got)
	}
	// The turn page draws the filed turn from History, so the page exists.
	if turns := st.PageTurns(); len(turns) != 2 || turns[0] != 1 || turns[1] != 2 {
		t.Errorf("the turn page sees %v; turns 1 and 2 were filed", turns)
	}
	st.Page = TurnView{Open: true, Turn: 1, Follow: true}
	golden(t, "hosted-turn-page", render(st))
}

// TestHostedRejoinedAndRefusedRenderApart is §4a.1 on the two notices the TUI
// carries on its own line: the rejoin's denial and the host's refusal. Each
// is its own golden, and neither sentence is composed here — the rejoin is
// councilhost's one-line form and the refusal is the host's ruled sentence.
func TestHostedRejoinedAndRefusedRenderApart(t *testing.T) {
	joined := hostedState(hostedFixture())
	joined.Notice = councilhost.RejoinedNotice(councilhost.HostFile{PID: 4242})
	golden(t, "hosted-rejoined", render(joined))

	write := hostedFixture()
	write.Posture = "write"
	refused := hostedState(write)
	refused.Notice = councilhost.UnwatchedWriteRefusal
	got := render(refused)
	golden(t, "hosted-refused", got)
	if !strings.Contains(got, "WRITE") || !strings.Contains(got, "not asking") {
		t.Errorf("a hosted write room does not say it writes without asking:\n%s", got)
	}
	if strings.Contains(got, "a not asking") {
		t.Errorf("a hosted room promises the `a` key, which cannot turn the asking on here:\n%s", got)
	}
	if render(joined) == got {
		t.Fatal("a rejoin and a refusal rendered alike")
	}
}

// TestHostedHelpTeachesDetachAndWhatQCosts pins the hosted help page: /detach
// where the ordinary room's /cd row was, and q named as ending every seat.
func TestHostedHelpTeachesDetachAndWhatQCosts(t *testing.T) {
	st := hostedState(hostedFixture())
	st.Help = HelpKeys
	got := render(st)
	golden(t, "hosted-help", got)
	if !strings.Contains(got, "/detach") {
		t.Errorf("the hosted help page does not teach /detach:\n%s", got)
	}
	if strings.Contains(got, "/cd <dir>") {
		t.Errorf("the hosted help page teaches /cd, which a hosted room refuses:\n%s", got)
	}
	if !strings.Contains(got, "q ENDS the room") {
		t.Errorf("the hosted help page does not say what q costs:\n%s", got)
	}
	plain := hostedState(hostedFixture())
	plain.Help = HelpKeys
	plain.Hosted = HostedRoom{}
	if strings.Contains(render(plain), "/detach") {
		t.Error("the ordinary help page teaches /detach, which the ordinary room refuses")
	}
	ordinary, hosted := helpKeys(layoutFor(st, GlyphsFor(false)), PlainStyles(), GlyphsFor(false)), helpKeysHosted(helpKeys(layoutFor(st, GlyphsFor(false)), PlainStyles(), GlyphsFor(false)))
	if len(ordinary) != len(hosted) {
		t.Fatalf("the hosted page has %d rows and the ordinary one %d; the budget is the same 16", len(hosted), len(ordinary))
	}
}

// TestHostedEnterSendsTheRouteAndSpawnsNothing is the crew over the wire: the
// route resolved here, the seats named to the host, no process started in
// this one, and a busy seat refused before anything is sent.
func TestHostedEnterSendsTheRouteAndSpawnsNothing(t *testing.T) {
	log := countSpawns(t)
	m, link := hostedModel(t, hostedFixture(), "")

	m.setDraft("@codex fix the parser")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(link.dispatches) != 1 || link.dispatches[0].prompt != "fix the parser" ||
		!reflect.DeepEqual(link.dispatches[0].seats, []model.VendorID{model.VendorCodex}) {
		t.Fatalf("enter did not send the named seat: %+v", link.dispatches)
	}
	if m.st.Draft != "" || m.st.Mode != ModeViewing || m.st.TurnRoute == nil {
		t.Fatalf("the composer did not hand off: draft=%q mode=%v route=%v", m.st.Draft, m.st.Mode, m.st.TurnRoute)
	}
	if log.n() != 0 {
		t.Fatalf("a hosted dispatch spawned %d process(es) in this process: %+v", log.n(), log.specs)
	}

	// The default route is claude alone, and @all is everyone seated and
	// drivable — the undrivable seat is never named.
	m.st.Mode = ModeComposing
	m.setDraft("plain brief")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	m.st.Mode = ModeComposing
	m.setDraft("@all everyone")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := link.dispatches[1].seats; !reflect.DeepEqual(got, []model.VendorID{model.VendorClaude}) {
		t.Fatalf("an unaddressed brief went to %v", got)
	}
	if got := link.dispatches[2].seats; len(got) != 4 {
		t.Fatalf("@all named %v; four seats are drivable", got)
	}
	for _, v := range link.dispatches[2].seats {
		if v == model.VendorCursor {
			t.Fatal("@all named the undrivable seat")
		}
	}

	// A frame lands with codex busy. The next brief to codex is refused
	// HERE, in the room's own words, and nothing crosses the wire.
	busy := hostedFixture()
	busy.Turn = 1
	busy.Seats[1].Phase, busy.Seats[1].Turn = councilhost.PhaseStreaming, 1
	m.applyHostFrame(hostedRoomFrame(busy))
	m.st.Mode = ModeComposing
	m.setDraft("@codex again")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(link.dispatches) != 3 {
		t.Fatalf("a brief to a busy seat crossed the wire: %+v", link.dispatches[3:])
	}
	if !strings.HasPrefix(m.st.Notice, "a turn is in flight on codex (turn 1)") || m.st.Draft != "@codex again" {
		t.Fatalf("the busy refusal is wrong or the draft was lost: notice=%q draft=%q", m.st.Notice, m.st.Draft)
	}
	// The same brief to an idle seat still goes.
	m.setDraft("@agy docs")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(link.dispatches) != 4 || link.dispatches[3].seats[0] != model.VendorAntigravity {
		t.Fatalf("a brief to an idle seat beside a busy one did not go: %+v", link.dispatches)
	}
}

// TestHostedCtrlCStopsTheFocusedSeatThenEveryoneThenEndsTheRoom is viewKey's
// three meanings, each sent to the host, with the footer's cancel cell the
// contract for which one is live.
func TestHostedCtrlCStopsTheFocusedSeatThenEveryoneThenEndsTheRoom(t *testing.T) {
	busy := hostedFixture()
	busy.Turn = 1
	busy.Seats[0].Phase, busy.Seats[0].Turn = councilhost.PhaseStreaming, 1
	busy.Seats[1].Phase, busy.Seats[1].Turn = councilhost.PhaseWaiting, 1
	m, link := hostedModel(t, busy, "")
	m.st.Mode = ModeViewing
	m.setFocus(0)

	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if len(link.interrupts) != 1 || !reflect.DeepEqual(link.interrupts[0], []model.VendorID{model.VendorClaude}) {
		t.Fatalf("ctrl+c on a busy focused seat sent %v", link.interrupts)
	}
	if !strings.Contains(m.st.Notice, "cancelling Claude Code") {
		t.Fatalf("the cancel was not named: %q", m.st.Notice)
	}
	// The footer's contract, read with the notice out of the way: a notice
	// takes the mode line for itself (modeLine), and the cancel cell under it
	// names the focused seat while two are in flight (cancelLabel).
	m.st.Notice = ""
	if !strings.Contains(render(m.st), "cancel claude") {
		t.Fatalf("the footer's cancel cell does not name the focused seat:\n%s", render(m.st))
	}

	m.setFocus(2) // agy, idle
	m.st.Notice = ""
	m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if len(link.interrupts) != 2 || link.interrupts[1] != nil {
		t.Fatalf("ctrl+c on an idle seat with others busy sent %v; everyone was expected", link.interrupts)
	}
	if link.closed {
		t.Fatal("ctrl+c ended the room while seats were busy")
	}

	idle := hostedFixture()
	m.applyHostFrame(hostedRoomFrame(idle))
	_, cmd := m.key(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !link.closed || m.hosted.outcome != councilhost.OutcomeEnded {
		t.Fatalf("ctrl+c on an idle room did not end it: closed=%v outcome=%v", link.closed, m.hosted.outcome)
	}
	if cmd == nil {
		t.Fatal("ending the room did not quit the program")
	}
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Fatal("ending the room returned something other than a quit")
	}
}

// TestHostedQRefusesWhileASeatIsBusyAndNamesDetach: q ends every seat, so a
// busy room refuses it with the per-seat remedy and the way to leave instead.
func TestHostedQRefusesWhileASeatIsBusyAndNamesDetach(t *testing.T) {
	busy := hostedFixture()
	busy.Turn = 3
	busy.Seats[4].Phase, busy.Seats[4].Turn = councilhost.PhaseWaiting, 3
	m, link := hostedModel(t, busy, "")
	m.st.Mode = ModeViewing
	_, cmd := m.key(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil || link.closed {
		t.Fatal("q ended a room with a seat in flight")
	}
	for _, want := range []string{"grok (turn 3)", "ctrl+c", "/detach"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the q refusal lacks %q: %q", want, m.st.Notice)
		}
	}
}

// TestHostedDetachWaitsForTheHostsAnswer is §7.29's rule in the TUI: /detach
// ASKS, a refusal lands as the host's own sentence and the room carries on,
// and agreement is what ends the client.
func TestHostedDetachWaitsForTheHostsAnswer(t *testing.T) {
	m, link := hostedModel(t, hostedFixture(), "")
	m.setDraft("/detach")
	_, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if link.detaches != 1 || cmd != nil || link.detached || link.closed {
		t.Fatalf("/detach did not ask, or did not wait: detaches=%d detached=%v closed=%v", link.detaches, link.detached, link.closed)
	}
	if m.st.Draft != "" {
		t.Fatalf("the draft survived the command: %q", m.st.Draft)
	}

	// Refused, with the ruled sentence verbatim and nothing else on the line.
	refusal := councilhost.Frame{Kind: councilhost.KindRefused,
		Reason: councilhost.UnwatchedWriteRefusal + "\n" + councilhost.UnwatchedWriteRemedy}
	// The Cmd a refusal returns is the re-armed reader (waitHost), which would
	// block on the fake's queue if executed; that it is not a quit is asserted
	// on the outcome and the link instead.
	if next := m.applyHostFrame(hostFrameMsg{frame: refusal}); next == nil {
		t.Fatal("a refusal stopped the reader; the room carries on and so must the frames")
	}
	if m.hosted.outcome != councilhost.OutcomeEnded {
		t.Fatalf("a refused detach set an outcome: %v", m.hosted.outcome)
	}
	if m.st.Notice != councilhost.UnwatchedWriteRefusal {
		t.Fatalf("the refusal on screen is not the host's sentence:\n%q", m.st.Notice)
	}
	if link.detached || link.closed {
		t.Fatal("a refused detach closed the link")
	}

	// Agreed: the link is given up, never closed, and the program quits with
	// the detached outcome.
	quit := m.applyHostFrame(hostFrameMsg{frame: councilhost.Frame{Kind: councilhost.KindDetached, HostPID: 4242}})
	if quit == nil {
		t.Fatal("a granted detach did not quit")
	}
	if _, isQuit := quit().(tea.QuitMsg); !isQuit {
		t.Fatal("a granted detach returned something other than a quit")
	}
	if !link.detached || link.closed || m.hosted.outcome != councilhost.OutcomeDetached {
		t.Fatalf("the detach did not give the host up cleanly: detached=%v closed=%v outcome=%v",
			link.detached, link.closed, m.hosted.outcome)
	}
	// A later teardown — a signal after the detach — must not reach a host
	// this client no longer owns.
	m.teardown()
	if link.closed {
		t.Fatal("teardown after a detach sent a shutdown to the host the client had left")
	}
}

// TestHostedHostDeathEndsTheRoomAsItsOwnState: a broken stream is the exited
// outcome and a quit, never a quiet room and never a detach.
func TestHostedHostDeathEndsTheRoomAsItsOwnState(t *testing.T) {
	m, link := hostedModel(t, hostedFixture(), "")
	quit := m.applyHostFrame(hostFrameMsg{err: councilhost.ErrHostExited})
	if quit == nil || m.hosted.outcome != councilhost.OutcomeHostExited {
		t.Fatalf("a dead host did not end the room as exited: outcome=%v", m.hosted.outcome)
	}
	if link.closed || link.detached {
		t.Fatal("a dead host was closed or detached from; the pipe is what broke")
	}
	// A write down the dead pipe from a key ends the same way.
	m2, link2 := hostedModel(t, hostedFixture(), "")
	link2.dead = errors.New("councilhost: the host process exited")
	m2.setDraft("hello")
	_, cmd := m2.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil || m2.hosted.outcome != councilhost.OutcomeHostExited {
		t.Fatalf("a dispatch down a dead pipe did not end the room: outcome=%v", m2.hosted.outcome)
	}
}

// TestHostedRefusesEveryControlItCannotCarry walks the verbs and keys a hosted
// room refuses, and asserts each refusal names the room the control works in
// and sends nothing.
func TestHostedRefusesEveryControlItCannotCarry(t *testing.T) {
	log := countSpawns(t)
	m, link := hostedModel(t, hostedFixture(), "")
	for _, draft := range []string{"/cd ..", "/seat codex", "/unseat grok", "/read", "/write",
		"/arena race it", "/flow @codex a -> @claude b", "/hand claude codex", "/adopt codex", "/retry", "/trace out.jsonl"} {
		m.st.Mode = ModeComposing
		m.st.Notice = ""
		m.setDraft(draft)
		m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
		if !strings.Contains(m.st.Notice, "not available in a hosted room") {
			t.Errorf("%q was not refused as a hosted-room control: %q", draft, m.st.Notice)
		}
		if !strings.Contains(m.st.Notice, "/detach") {
			t.Errorf("the refusal of %q does not name the way out: %q", draft, m.st.Notice)
		}
	}
	m.st.Mode = ModeComposing
	m.setDraft("x")
	m.key(tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if !strings.Contains(m.st.Notice, "rebuttal") || m.st.Quote {
		t.Errorf("ctrl+r armed a rebuttal a hosted room cannot carry: notice=%q quote=%v", m.st.Notice, m.st.Quote)
	}
	m.st.Mode = ModeViewing
	for _, k := range []string{"c", "x", "a", "s", "r"} {
		m.st.Notice = ""
		m.key(tea.KeyPressMsg{Code: rune(k[0]), Text: k})
		if !strings.Contains(m.st.Notice, "not available in a hosted room") {
			t.Errorf("key %q was not refused: %q", k, m.st.Notice)
		}
	}
	if len(link.dispatches) != 0 || len(link.interrupts) != 0 || log.n() != 0 {
		t.Fatalf("a refused control reached the host or a spawn: %+v %+v %d", link.dispatches, link.interrupts, log.n())
	}
	if m.st.GateOff {
		t.Error("`a` turned the gate off in a room that has none")
	}
}

// TestDetachRefusesInASingleProcessRoom: the verb exists in both rooms and
// lies in neither. The ordinary room has no host to leave and says so, with
// the way to get one, and spawns nothing.
func TestDetachRefusesInASingleProcessRoom(t *testing.T) {
	log := countSpawns(t)
	m := newModel(Options{}, room())
	m.st.Mode = ModeComposing
	m.setDraft("/detach")
	_, cmd := m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("/detach in a single-process room returned a command")
	}
	if !strings.Contains(m.st.Notice, "no host to leave") || !strings.Contains(m.st.Notice, "--host") {
		t.Fatalf("the ordinary room's /detach refusal lacks the remedy: %q", m.st.Notice)
	}
	if log.n() != 0 {
		t.Fatalf("/detach spawned %d process(es)", log.n())
	}
	// And a sentence typed with the verb is prose, /read's rule.
	m.setDraft("/detach the lexer from the parser")
	m.key(tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Contains(m.st.Notice, "no host to leave") {
		t.Fatal("a sentence opening with /detach ran the command")
	}
}

// TestHostedViewSurvivesAFrame: scroll, focus and the quota reading are the
// reader's and this client's, and a frame from the host must not move them.
// A new turn on a seat re-arms its tail, startTurn's own rule.
func TestHostedViewSurvivesAFrame(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 1, 0, 0, time.UTC)
	m, _ := hostedModel(t, hostedTurnFixture(now), "")
	m.st.Mode = ModeViewing
	m.setFocus(1) // codex
	m.st.Columns[1].Follow, m.st.Columns[1].Scroll = false, 3
	m.st.Columns[0].Quota = &SeatQuota{WrittenAt: now}
	m.st.Expanded = true
	m.st.Help = HelpKeys

	next := hostedTurnFixture(now)
	next.Seats[0].Body += " and the column number too."
	m.applyHostFrame(hostedRoomFrame(next))

	if m.st.Focus != 1 || m.st.Columns[1].Scroll != 3 || m.st.Columns[1].Follow {
		t.Fatalf("a frame moved the reader: focus=%d scroll=%d follow=%v", m.st.Focus, m.st.Columns[1].Scroll, m.st.Columns[1].Follow)
	}
	if m.st.Columns[0].Quota == nil {
		t.Fatal("a frame dropped the quota reading this client read from its own relay")
	}
	if !m.st.Expanded || m.st.Help != HelpKeys {
		t.Fatal("a frame changed the view's own toggles")
	}
	if !strings.HasSuffix(m.st.Columns[0].Body, "column number too.") {
		t.Fatal("the frame's new text did not land")
	}

	// codex takes a new turn: its tail re-arms.
	next.Turn = 3
	next.Seats[1].Turn, next.Seats[1].Phase = 3, councilhost.PhaseWaiting
	m.applyHostFrame(hostedRoomFrame(next))
	if !m.st.Columns[1].Follow || m.st.Columns[1].Scroll != 0 {
		t.Fatalf("a new turn did not re-arm the seat's tail: follow=%v scroll=%d", m.st.Columns[1].Follow, m.st.Columns[1].Scroll)
	}
	if len(m.st.Columns[1].History) != 1 {
		t.Fatalf("the filed turn did not project: %d records", len(m.st.Columns[1].History))
	}
}

// TestHostedRoomWritesNoRoomFile: the client holds no session id and owns no
// conversation, so the one file council writes is not written from here.
func TestHostedRoomWritesNoRoomFile(t *testing.T) {
	// A home of this test's own: the suite's sandbox home is shared, and a
	// room another test saved there would be read as this client's write.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	m, _ := hostedModel(t, hostedFixture(), "")
	m.st.Turn = 3
	m.saveRoom()
	m.teardown()
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a hosted client wrote %s (%v)", path, err)
	}
}

// TestHostedNoticeFromTheHostLandsOnceAndTheRefusalOutranksNothing: the host's
// own notice — a busy-seat refusal it made — lands on the line when it is
// new, and is not re-landed over a newer local notice on every frame.
func TestHostedNoticeFromTheHostLandsOnce(t *testing.T) {
	m, _ := hostedModel(t, hostedFixture(), "")
	r := hostedFixture()
	r.Notice = "a turn is in flight on codex (turn 1) — ctrl+c on its column cancels that turn, or address another seat"
	m.applyHostFrame(hostedRoomFrame(r))
	if m.st.Notice != r.Notice {
		t.Fatalf("the host's notice did not land: %q", m.st.Notice)
	}
	m.st.Notice = "cancelling Codex…"
	m.applyHostFrame(hostedRoomFrame(r))
	if m.st.Notice != "cancelling Codex…" {
		t.Fatalf("an unchanged host notice overwrote a newer local one: %q", m.st.Notice)
	}
}

// TestHostedFlagsAreRefusedBeforeTheRoomOpens: the flags a hosted room cannot
// honour are a line on stderr, each with its reason, and every other flag
// passes.
func TestHostedFlagsAreRefusedBeforeTheRoomOpens(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts Options
	}{
		{"live", Options{Live: model.VendorClaude}},
		{"brief", Options{BriefPath: "brief.md"}},
		{"record", Options{RecordPath: "demo.jsonl"}},
		{"trace", Options{TracePath: "trace.jsonl"}},
	} {
		if err := refuseHostedFlags(tc.opts); !errors.Is(err, ErrHostedFlag) {
			t.Errorf("--%s was not refused with --host: %v", tc.name, err)
		}
	}
	if err := refuseHostedFlags(Options{Write: true, Auto: true, SharedTree: true, Fresh: true, ASCII: true}); err != nil {
		t.Errorf("a flag a hosted room can honour was refused: %v", err)
	}
}
