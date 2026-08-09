package council

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// claudeWire is the production wire for the stream-json seat.
//
// Tests that drive a live seat build a seatProc by hand, and the wire is part of
// what a spawn produces — so it is the REAL one rather than a stub. A stub here
// would assert that a decision was routed rather than that the right bytes were
// built, which is the substitution this repo's own CLAUDE.md names as its
// recorded failure mode.
func claudeWire() seatWire { return streamWire{vendors.Claude{}} }

type decisionSession struct{ sent [][]byte }

func (s *decisionSession) SendTurn(lines [][]byte) error  { return s.record(lines) }
func (s *decisionSession) SendAside(lines [][]byte) error { return s.record(lines) }
func (s *decisionSession) record(lines [][]byte) error {
	for _, l := range lines {
		s.sent = append(s.sent, append([]byte(nil), l...))
	}
	return nil
}
func (*decisionSession) Kill()       {}
func (*decisionSession) Alive() bool { return true }

// turnModel is traceModel plus a turn in flight on a persistent seat. The turn
// bookkeeping is the seam this file tests: a column can now be retired by four
// different signals, and getting that wrong either hangs the room or ends a
// turn while a vendor is still talking.
func turnModel(persistent bool) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		st: State{Columns: []Column{{
			Vendor: model.VendorClaude, Label: "Claude Code",
			Avail: AvailInstalled, Phase: PhaseStreaming,
			Started: time.Now().Add(-time.Second),
		}}},
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		failure:    map[model.VendorID]runner.FailureClass{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      map[model.VendorID]*seatProc{},
		gateInputs: map[string]map[string]any{},
		roomCtx:    ctx,
		roomCancel: cancel,
	}
	m.turn = &turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{},
	}
	if persistent {
		m.turn.persistent[model.VendorClaude] = true
	}
	return m
}

// TestPersistentTurnEndsOnTheVendorsOwnLine.
//
// A spawn-per-turn child says "the turn is over" by dying. A persistent one
// never dies, so the end-of-turn line is the only signal there is — and if it
// were dropped the column would spin forever while the room waited for an exit
// that is not coming.
func TestPersistentTurnEndsOnTheVendorsOwnLine(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta,
		Text: "done", EndsTurn: true,
	}})

	if got := m.st.Columns[0].Phase; got != PhaseDone {
		t.Errorf("phase = %v, want done", got)
	}
	if m.turn != nil {
		t.Error("the turn is still in flight after its only end signal")
	}
	if m.st.Mode != ModeComposing {
		t.Error("the room did not return to compose after the turn ended")
	}
}

// TestPersistentEndOfTurnFlushesTheStreamTail is a live defect: the redactor
// holds everything after the last word boundary (so a secret split across two
// chunks cannot straddle the match), and the persistent seat's end-of-turn is
// a `result` line rather than a process exit — so the KindDone/KindError
// flushes never ran and the reply's final word was silently eaten. Observed by
// the owner on a real turn: "Afternoon, San. Seat".
func TestPersistentEndOfTurnFlushesTheStreamTail(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "Afternoon, San. Seat"},
		{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "ed."},
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true},
	})

	if got := m.st.Columns[0].Body; got != "Afternoon, San. Seated." {
		t.Errorf("body = %q — the stream tail was dropped at end of turn", got)
	}
	if got := m.st.Columns[0].Phase; got != PhaseDone {
		t.Errorf("phase = %v, want done", got)
	}
}

// TestSpawnPerTurnIgnoresTheEndOfTurnLine.
//
// The same `result` line carries EndsTurn for every vendor, but a spawn-per-turn
// column is retired by its process exit — the flush of the redactor and the
// final elapsed both hang off it. Acting on both would retire the column twice.
func TestSpawnPerTurnIgnoresTheEndOfTurnLine(t *testing.T) {
	m := turnModel(false)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta,
		Text: "done", EndsTurn: true,
	}})
	if m.turn == nil {
		t.Fatal("a spawn-per-turn column was retired by a stream line rather than by its exit")
	}
	if got := m.st.Columns[0].Phase; got != PhaseStreaming {
		t.Errorf("phase = %v, want still streaming until the process exits", got)
	}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})
	if m.turn != nil {
		t.Error("the process exit did not end the turn")
	}
}

// TestPersistentProcessDeathMidTurnFailsTheColumn.
//
// The hang this prevents is the whole reason KindDone is handled separately for
// a persistent seat: the process is not supposed to exit, so an exit during a
// turn means the answer is not coming. A column that simply stopped would be
// indistinguishable from one that finished.
func TestPersistentProcessDeathMidTurnFailsTheColumn(t *testing.T) {
	m := turnModel(true)
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire()}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindDone, ExitCode: 4,
	}})

	c := m.st.Columns[0]
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed: the answer is not coming", c.Phase)
	}
	if c.Note == "" {
		t.Error("a column that lost its process said nothing about why")
	}
	if m.turn != nil {
		t.Error("the turn never ended after the process died")
	}
	if _, ok := m.procs[model.VendorClaude]; ok {
		t.Error("a dead process was kept; the next brief would write into a closed pipe")
	}
}

// TestPersistentProcessDeathBetweenTurnsIsNotAFailure. Quitting the room kills
// these processes on purpose, and the exit that follows must not paint a
// finished column red.
func TestPersistentProcessDeathBetweenTurnsIsNotAFailure(t *testing.T) {
	m := turnModel(true)
	m.turn = nil
	m.st.Columns[0].Phase = PhaseDone
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire()}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})

	if got := m.st.Columns[0].Phase; got != PhaseDone {
		t.Errorf("phase = %v, want the finished column left alone", got)
	}
}

// TestRetiringAColumnTwiceDoesNotEndTheTurnEarly.
//
// A persistent seat really can report twice — its end-of-turn line, and then its
// process dying — and the counter this replaced would have decremented for both,
// ending the turn while another vendor was still mid-sentence.
func TestRetiringAColumnTwiceDoesNotEndTheTurnEarly(t *testing.T) {
	m := turnModel(true)
	m.st.Columns = append(m.st.Columns, Column{
		Vendor: model.VendorCodex, Label: "Codex",
		Avail: AvailInstalled, Phase: PhaseWaiting,
	})
	m.turn.live[model.VendorCodex] = true

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true},
		{Vendor: model.VendorClaude, Kind: runner.KindDone},
	})

	if m.turn == nil {
		t.Fatal("the turn ended while Codex was still working")
	}
	if !m.turn.live[model.VendorCodex] {
		t.Error("Codex was retired by another column's events")
	}
}

// TestCancelledPersistentTurnIsNotAVendorFailure.
//
// Interrupting comes back as a result with is_error true — the vendor really
// does report a failure. But the user's keystroke is not the vendor falling
// over, and blaming it for one is a false claim on screen.
func TestCancelledPersistentTurnIsNotAVendorFailure(t *testing.T) {
	m := turnModel(true)
	m.cancelling = true

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note: "the vendor reported the turn failed", EndsTurn: true,
	}})

	c := m.st.Columns[0]
	if c.Phase != PhaseCancelled {
		t.Errorf("phase = %v, want cancelled", c.Phase)
	}
	if c.Note != "cancelled — the output above is partial" {
		t.Errorf("note = %q, want the cancellation wording rather than the vendor's error", c.Note)
	}
}

// TestPersistentCostIsLabelledAsASessionTotal.
//
// Measured: two turns of one process reported $0.1061493 then $0.1177296 while
// the per-turn usage block stayed at 2 input tokens both times. The number is
// true and the cell has always meant "this turn", so the two must not render
// alike.
func TestPersistentCostIsLabelledAsASessionTotal(t *testing.T) {
	cost := 0.1177296
	c := Column{
		Vendor: model.VendorClaude, Label: "Claude Code", Avail: AvailInstalled,
		Sandbox: SandboxClaim{Level: SandboxWrite}, Gran: GranTokens,
		CostUSD: &cost, CostSession: true,
	}
	// Asserted through the rendered row rather than through a cost helper alone,
	// so the word cannot be lost between the two: the badges are left-anchored
	// and the figure is right-anchored, and this is the fact that has to survive
	// that gap.
	row := badgeRow(c, 48, PlainStyles(), UnicodeGlyphs())
	if !strings.HasPrefix(row, "  WRITES  tokens") {
		t.Errorf("badge row = %q, want the posture claim first", row)
	}
	if !strings.HasSuffix(row, "$0.1177 session") {
		t.Errorf("badge row = %q, want the cost named as a session total", row)
	}

	c.CostSession = false
	if got := badgeRow(c, 48, PlainStyles(), UnicodeGlyphs()); !strings.HasSuffix(got, "$0.1177") {
		t.Errorf("badge row = %q, want a bare per-turn cost", got)
	}
}

// TestGatesQueueInArrivalOrder. One assistant message can ask for a parallel
// batch, and each call blocks separately. Arrival order is the only order a
// person can follow: the card names the call it is asking about, and a queue
// that reordered itself would move the card under the keystroke.
func TestGatesQueueInArrivalOrder(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindGate,
			Gate: &runner.Gate{RequestID: "r1", ToolUseID: "t1", Tool: "Write", Text: "Write: a.txt"}},
		{Vendor: model.VendorClaude, Kind: runner.KindGate,
			Gate: &runner.Gate{RequestID: "r2", ToolUseID: "t2", Tool: "Bash", Text: "Bash: rm -rf b"}},
	})

	if !m.st.Gating() {
		t.Fatal("two blocked calls and the room is not gating")
	}
	if len(m.st.Gates) != 2 {
		t.Fatalf("queue = %d, want both requests kept", len(m.st.Gates))
	}
	if m.st.Gates[0].Text != "Write: a.txt" {
		t.Errorf("head = %q, want the first to arrive", m.st.Gates[0].Text)
	}

	m.decideGate(true)
	if len(m.st.Gates) != 1 || m.st.Gates[0].RequestID != "r2" {
		t.Errorf("after one decision the queue is %+v, want only r2", m.st.Gates)
	}
	m.decideGate(false)
	if m.st.Gating() {
		t.Error("the room is still gating with an empty queue")
	}
}

func TestBasicGitOpsProceedWithoutAGate(t *testing.T) {
	m := turnModel(true)
	sess := &decisionSession{}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: sess}
	g := &runner.Gate{
		RequestID: "r1", ToolUseID: "t1", Tool: "Bash",
		Text:  "Bash: git push -u origin feat/handoff",
		Input: map[string]any{"command": "git push -u origin feat/handoff"},
	}
	m.queueGate(&m.st.Columns[0], g)

	if m.st.Gating() {
		t.Fatal("routine git push was put behind a user gate")
	}
	if len(sess.sent) != 1 {
		t.Fatalf("decision messages = %d, want one automatic approval", len(sess.sent))
	}
	got := string(sess.sent[0])
	if !strings.Contains(got, `"behavior":"allow"`) || !strings.Contains(got, "feat/handoff") {
		t.Fatalf("automatic decision did not approve and echo the command input: %s", got)
	}
}

func TestDestructiveGitOpsStillGate(t *testing.T) {
	for _, command := range []string{
		"git reset --hard HEAD~1",
		"git clean -fd",
		"git push --force origin main",
		"git branch -D work",
		"git checkout -- changed.go",
		"git commit --amend --no-edit",
		"gh pr merge 68 --squash",
		"git status && rm -rf .",
	} {
		t.Run(command, func(t *testing.T) {
			g := &runner.Gate{Tool: "Bash", Input: map[string]any{"command": command}}
			if autoApproveRoutine(g) {
				t.Fatalf("destructive or composed command was automatically approved: %s", command)
			}
		})
	}
}

func TestRoutineGitOpsAreRecognized(t *testing.T) {
	for _, command := range []string{
		"git status --short",
		"git add internal/council/dispatch.go",
		`git commit -m "fix handoff"`,
		"git switch -c feat/handoff",
		"git checkout -b feat/handoff",
		"git pull --ff-only",
		"git push -u origin feat/handoff",
		"gh pr create --fill",
		"gh run watch 123",
	} {
		t.Run(command, func(t *testing.T) {
			g := &runner.Gate{Tool: "Bash", Input: map[string]any{"command": command}}
			if !autoApproveRoutine(g) {
				t.Fatalf("routine command still requires approval: %s", command)
			}
		})
	}
}

// TestDenialIsRecordedAsADenialNotAFailure.
//
// The substitution this prevents is the whole point of the gate. The vendor
// reports a denial as an is_error tool_result carrying council's own refusal
// text back, so read off the stream alone it is indistinguishable from a tool
// that broke — and the trace would say the command failed when what happened is
// that it was not allowed to run.
func TestDenialIsRecordedAsADenialNotAFailure(t *testing.T) {
	m := turnModel(true)
	// The call is announced first, exactly as the live stream does it: the
	// assistant tool_use block arrived 0.05s before the permission request.
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindActivity,
			Acts: []runner.ActCall{{ID: "toolu_1", Text: "Bash: rm -rf ."}}},
		{Vendor: model.VendorClaude, Kind: runner.KindGate,
			Gate: &runner.Gate{RequestID: "r1", ToolUseID: "toolu_1",
				Tool: "Bash", Text: "Bash: rm -rf ."}},
	})
	m.decideGate(false)

	acts := m.st.Columns[0].Acts
	if len(acts) != 1 {
		t.Fatalf("acts = %+v, want the announced call carrying the decision", acts)
	}
	if acts[0].Status != runner.ActDenied {
		t.Errorf("status = %v, want ActDenied", acts[0].Status)
	}

	// Now the vendor echoes our refusal back as a failed tool_result. It must
	// not overwrite the record of the keystroke.
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{
			ID: "toolu_1", Outcome: runner.ActFailed, Detail: denialText,
		}},
	}})
	if got := m.st.Columns[0].Acts[0].Status; got != runner.ActDenied {
		t.Errorf("status = %v after the vendor echoed the denial back, want ActDenied still", got)
	}
}

// TestApprovalLeavesTheTraceToTheVendor. A call the user allowed is one the
// vendor then runs, and how it went is the vendor's fact to report — council
// must not pre-empt it with a mark of its own.
func TestApprovalLeavesTheTraceToTheVendor(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorClaude, Kind: runner.KindActivity,
			Acts: []runner.ActCall{{ID: "toolu_1", Text: "Write: a.txt"}}},
		{Vendor: model.VendorClaude, Kind: runner.KindGate,
			Gate: &runner.Gate{RequestID: "r1", ToolUseID: "toolu_1",
				Tool: "Write", Text: "Write: a.txt"}},
	})
	m.decideGate(true)

	if got := m.st.Columns[0].Acts[0].Status; got != runner.ActPending {
		t.Errorf("status = %v, want pending: the call has not come back yet", got)
	}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: "toolu_1", Outcome: runner.ActOK}},
	}})
	if got := m.st.Columns[0].Acts[0].Status; got != runner.ActOK {
		t.Errorf("status = %v, want the vendor's own outcome", got)
	}
}

// TestEndingATurnClearsItsGates.
//
// A card left up for a vendor that has stopped waiting invites a keystroke that
// decides nothing, and the footer would go on announcing keys that no longer do
// anything. Cancelling is the common way in: the interrupt ends the turn while
// a request is still on screen.
func TestEndingATurnClearsItsGates(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindGate,
		Gate: &runner.Gate{RequestID: "r1", ToolUseID: "t1", Tool: "Write", Text: "Write: a.txt"},
	}})
	if !m.st.Gating() {
		t.Fatal("the gate was not queued")
	}

	m.cancelling = true
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError, EndsTurn: true,
	}})

	if m.st.Gating() {
		t.Error("a cancelled turn left its approval card on screen")
	}
	if len(m.gateInputs) != 0 {
		t.Error("the tool arguments of a discarded request were kept")
	}
}

// TestGateTextIsRedacted. The argument line of a tool call is one of the
// likeliest places for a credential to appear, and this one is rendered in
// chrome that does not scroll away.
func TestGateTextIsRedacted(t *testing.T) {
	m := turnModel(true)
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindGate,
		Gate: &runner.Gate{
			RequestID: "r1", ToolUseID: "t1", Tool: "Bash",
			Text: "Bash: curl -H 'Authorization: Bearer sk-ant-api03-" +
				"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'",
		},
	}})
	if len(m.st.Gates) != 1 {
		t.Fatal("the gate was not queued")
	}
	if strings.Contains(m.st.Gates[0].Text, "sk-ant-api03-AAAA") {
		t.Errorf("the approval card carries an unredacted secret: %q", m.st.Gates[0].Text)
	}
}
