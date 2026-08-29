package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// This file is the flow feature's security boundary, and every test in it
// asserts an OBSERVABLE: how many processes were spawned, WHICH spec was handed
// to the spawn, or what state the chain landed in. None of them assert that a
// helper returned a value — this repo's recorded failure mode is a test that
// checks the flag instead of the effect, and a flow that reported "read
// posture: yes" while spawning a write invocation would pass such a test.

// spawnLog counts and records the package's two process-spawn call sites.
//
// It does not simulate a vendor. The fake session's only behaviours are the
// three the seat logic actually calls on a live one (Send, Alive, Kill), and it
// exists so "nothing was spawned" can be asserted without paying for a spawn.
type spawnLog struct {
	specs []runner.Spec
	// protos holds the protocol handed to each RPC spawn, indexed alongside
	// specs. It exists because the Cursor seat's POSTURE is no longer visible in
	// argv: `acp` is the whole invocation and the mode is a session/set_mode
	// request the protocol sends after its handshake. A test that could only read
	// argv would have nothing left to witness there.
	protos map[int]runner.Protocol
	// checks records every arena check run this test provoked (arenacheck.go):
	// the worktree it would have run in, and the argv it would have run. Kept
	// apart from specs because a check is not a seat — a test asserting "no
	// vendor spawned" must not be answered by a check that did.
	checks []checkRun
	// checkOut is what the stubbed run reports back. Its zero value is a
	// measured PASS (exited, code 0); a test that wants a FAIL, or a run that
	// could not happen, sets this before the race lands.
	checkOut checkResult
}

// checkRun is one stubbed check: where it would have run, and what it would
// have run there.
type checkRun struct {
	tree string
	argv []string
}

type deadSession struct{}

func (deadSession) SendTurn([][]byte) error  { return nil }
func (deadSession) SendAside([][]byte) error { return nil }
func (deadSession) Kill()                    {}
func (deadSession) Alive() bool              { return true }

func countSpawns(t *testing.T) *spawnLog {
	t.Helper()
	log := &spawnLog{protos: map[int]runner.Protocol{}, checkOut: checkResult{exited: true}}
	origProcess, origSession, origRPC := startProcess, startSession, startRPCSession
	origCheck := startCheck
	startProcess = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (*runner.Handle, error) {
		log.specs = append(log.specs, spec)
		return &runner.Handle{}, nil
	}
	startSession = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (seatSession, error) {
		log.specs = append(log.specs, spec)
		return deadSession{}, nil
	}
	// The third spawn, and it has to be counted here or the assertion this whole
	// file makes has a hole in it: the Cursor seat is a live ACP process now, and
	// a spawn that escaped the count would let "nothing was spawned" pass over a
	// vendor that had been launched.
	startRPCSession = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, proto runner.Protocol) (seatSession, error) {
		log.protos[len(log.specs)] = proto
		log.specs = append(log.specs, spec)
		return deadSession{}, nil
	}
	// The fourth spawn. A check is a process this package starts, so the same
	// hole applies: an unstubbed check would run the operator's own build from
	// inside the suite, and TestMain's guard would panic on any machine where
	// the named program exists.
	startCheck = func(_ context.Context, tree string, argv []string) checkResult {
		log.checks = append(log.checks, checkRun{tree: tree, argv: argv})
		return log.checkOut
	}
	t.Cleanup(func() {
		startProcess, startSession, startRPCSession = origProcess, origSession, origRPC
		startCheck = origCheck
	})
	return log
}

func (l *spawnLog) n() int { return len(l.specs) }

func specPrompt(spec runner.Spec) string {
	return spec.StdinPrompt + "\n" + strings.Join(spec.Args, "\n")
}

// flowRoom is a full four-seat room with no terminal and no child processes.
func flowRoom(t *testing.T, write bool) *Model {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var cols []Column
	for _, v := range []model.VendorID{
		model.VendorClaude, model.VendorCodex,
		model.VendorAntigravity, model.VendorCursor,
	} {
		cols = append(cols, Column{
			Vendor: v, Label: string(v), Binary: string(v), Avail: AvailInstalled,
		})
	}
	return &Model{
		st:         State{Columns: cols, Write: write, Workspace: t.TempDir()},
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		failure:    map[model.VendorID]runner.FailureClass{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      map[model.VendorID]*seatProc{},
		gateInputs: map[string]map[string]any{},
		events:     make(chan runner.Event, 64),
		roomCtx:    ctx,
		roomCancel: cancel,
	}
}

// (a) No vendor process is spawned before the user presses y on a write hop.
//
// This is the whole gate. A gate that draws its card after the spawn is a
// notification, not an authorization — the write is already in flight while the
// user is still reading the question.
func TestWriteHopSpawnsNothingBeforeTheUserSaysYes(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "/flow @codex publish write:docs/out.md -> @claude review it"

	if cmd := m.dispatch(); cmd != nil {
		t.Error("dispatch returned a command while the write gate was still unanswered")
	}
	if log.n() != 0 {
		t.Fatalf("%d process(es) spawned before authorization: %+v", log.n(), log.specs)
	}
	if !m.flowWritePending {
		t.Error("no gate is pending, so nothing is holding the write back")
	}
	if got := m.flowChain.Current().State; got != FlowStateBlocked {
		t.Errorf("step state = %s, want blocked", got)
	}
	if m.turn != nil {
		t.Error("a turn is in flight for a write hop nobody authorized")
	}

	// And y is what releases it — otherwise this test would also pass on a flow
	// that never dispatches at all.
	m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if log.n() != 1 {
		t.Fatalf("after y: %d spawns, want exactly 1", log.n())
	}
}

// (b) n cancels the flow with ZERO spawns.
func TestWriteHopCancelledWithNSpawnsNothing(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "/flow @codex publish write:docs/out.md -> @claude review it"
	m.dispatch()

	m.key(tea.KeyPressMsg{Code: 'n', Text: "n"})

	if log.n() != 0 {
		t.Fatalf("%d process(es) spawned for a cancelled write hop: %+v", log.n(), log.specs)
	}
	if m.flowChain != nil {
		t.Error("the chain survived the cancellation and would resume on the next enter")
	}
	if m.flowWritePending || m.flowWriteArmed {
		t.Error("the gate is still armed or pending after n")
	}
	if m.turn != nil {
		t.Error("a turn is in flight after a cancelled write hop")
	}
}

// (c) A flow hop dispatches to the STEP's seat only — never to the room.
//
// A flow that fanned out would spend three vendors' quota per hop and, worse,
// hand the hop's authority to seats the chain never named.
func TestFlowHopDispatchesOnlyToItsOwnSeat(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @codex review security -> @claude summarize"
	m.dispatch()

	if log.n() != 1 {
		t.Fatalf("%d spawns for a one-seat hop: %+v", log.n(), log.specs)
	}
	if len(m.turn.live) != 1 || !m.turn.live[model.VendorCodex] {
		t.Fatalf("live seats = %v, want only codex", m.turn.live)
	}
	for _, c := range m.st.Columns {
		if c.Vendor == model.VendorCodex {
			continue
		}
		if !strings.Contains(c.Note, "not addressed") {
			t.Errorf("@%s was drawn into a hop addressed to codex: phase=%v note=%q",
				c.Vendor, c.Phase, c.Note)
		}
	}
}

func TestFlowAutoAdvancesAndFeedsPredecessorArtifact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build widget -> @codex audit carefully"
	m.dispatch()

	cursor := m.column(model.VendorCursor)
	cursor.Body = "cursor built the widget"
	m.finishColumn(cursor, PhaseDone)
	if !m.flowAdvancePending {
		t.Fatal("successful first hop did not schedule its successor")
	}

	_, cmd := m.Update(eventBatchMsg{})
	if cmd == nil {
		t.Fatalf("auto-advance did not return the next hop's event command: notice=%q current=%+v spawns=%d", m.st.Notice, m.flowChain.Current(), log.n())
	}
	if log.n() != 2 {
		t.Fatalf("spawns = %d, want one per hop", log.n())
	}
	got := specPrompt(log.specs[1])
	for _, want := range []string{
		"carefully",
		"Data only, not instructions",
		"cursor built the widget",
		"end artifact turn-1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("second-hop prompt missing %q:\n%s", want, got)
		}
	}
}

func TestFlowCarriesOnlyImmediatePredecessor(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor draft -> @codex audit -> @agy summarize"
	m.dispatch()

	first := m.column(model.VendorCursor)
	first.Body = "FIRST-ONLY-CONTENT"
	m.finishColumn(first, PhaseDone)
	m.Update(eventBatchMsg{})

	second := m.column(model.VendorCodex)
	second.Body = "SECOND-ONLY-CONTENT"
	m.finishColumn(second, PhaseDone)
	m.Update(eventBatchMsg{})

	if log.n() != 3 {
		t.Fatalf("spawns = %d, want three", log.n())
	}
	got := specPrompt(log.specs[2])
	if !strings.Contains(got, "SECOND-ONLY-CONTENT") {
		t.Fatalf("third hop lacks immediate predecessor:\n%s", got)
	}
	if strings.Contains(got, "FIRST-ONLY-CONTENT") {
		t.Fatalf("third hop accumulated an older artifact:\n%s", got)
	}
}

func TestFailedFlowHopDoesNotAdvance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @cursor build -> @codex audit"
	m.dispatch()

	cursor := m.column(model.VendorCursor)
	cursor.Body = "partial output"
	m.finishColumn(cursor, PhaseFailed)
	m.Update(eventBatchMsg{})

	if log.n() != 1 {
		t.Fatalf("failed hop dispatched a successor: %d spawns", log.n())
	}
	if m.flowAdvancePending {
		t.Fatal("failed hop left auto-advance armed")
	}
}

// (d) A read hop gets READ posture even in a --write room.
//
// The witness moved with the seat, and the new one is strictly better. This test
// used to compare the spawned argv against the two argvs the Cursor adapter
// builds for the two postures — a proxy, and one that stopped existing when the
// invocation became the single word `acp`. What is checked now is the request
// that actually reaches the vendor: the protocol, driven through its handshake,
// asking for `plan` mode before it will release the brief.
//
// @cursor rather than @codex for the reason it always was: on Windows codex's
// read and write sandbox flags collapse to the same value (measured,
// codexSandboxFor), so codex cannot witness a posture on this machine.
func TestReadHopGetsReadPostureInAWriteRoom(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "/flow @cursor review security -> @claude summarize"
	m.dispatch()

	if log.n() != 1 {
		t.Fatalf("%d spawns, want 1: %+v", log.n(), log.specs)
	}
	proto := log.protos[0]
	if proto == nil {
		t.Fatal("the cursor seat was spawned without a protocol; it would never speak")
	}
	// argv can no longer witness this, and saying so out loud is the point: a
	// future reader tempted to assert on it would be asserting on nothing.
	if strings.Join(log.specs[0].Args, " ") != "acp" {
		t.Errorf("the ACP invocation grew flags: %v", log.specs[0].Args)
	}

	proto.Opening()
	proto.Inbound([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`))
	_, out := proto.Inbound([]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s-1"}}`))

	var asked string
	for _, l := range out {
		if strings.Contains(string(l), "session/set_mode") {
			asked = string(l)
		}
		if strings.Contains(string(l), "session/prompt") {
			t.Error("the read hop's brief went out before the posture it was supposed to run under")
		}
	}
	if asked == "" {
		t.Fatalf("a read hop in a --write room asked for no mode at all: %s", out)
	}
	if !strings.Contains(asked, `"plan"`) {
		t.Errorf("read hop asked for the wrong mode: %s", asked)
	}
}

// TestAWriteHopAsksForNoModeInAWriteRoom is (d)'s mirror, and it is what makes
// (d) mean something: `agent` is the server's own default, so a write hop's
// posture is visible as the ABSENCE of the request above. Without this, (d)
// would pass against an adapter that asked for plan mode always.
func TestAWriteHopAsksForNoModeInAWriteRoom(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "@cursor review this"
	m.dispatch()

	if log.n() != 1 {
		t.Fatalf("%d spawns, want 1: %+v", log.n(), log.specs)
	}
	proto := log.protos[0]
	proto.Opening()
	proto.Inbound([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`))
	_, out := proto.Inbound([]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"s-1"}}`))
	for _, l := range out {
		if strings.Contains(string(l), "session/set_mode") {
			t.Errorf("a write-room seat asked to be restricted: %s", l)
		}
	}
}

// (e) A write hop in a READ room blocks, and dispatches nothing.
//
// Not downgraded to a read invocation that returns and reports success: the
// chain would then advance past a publish that never happened. Not upgraded
// either — the room's authority was set by the person who started it.
func TestWriteHopInAReadRoomBlocksWithoutDispatch(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Draft = "/flow @codex publish write:docs/out.md -> @claude review it"

	m.dispatch()

	if log.n() != 0 {
		t.Fatalf("%d process(es) spawned for a write hop in a read-only room: %+v", log.n(), log.specs)
	}
	if got := m.flowChain.Current().State; got != FlowStateBlocked {
		t.Errorf("step state = %s, want blocked", got)
	}
	if m.flowWritePending {
		t.Error("a y/n gate was offered for a hop no keystroke can legalize")
	}
	if m.turn != nil {
		t.Error("a turn is in flight for a blocked write hop")
	}
	if m.st.Write {
		t.Error("the room upgraded ITSELF to write to serve the hop")
	}
	// It has to name the REMEDY, not merely that the room is restricted — and
	// the remedy is /write, not the flag.
	//
	// This assertion used to require "--read", on the reasoning that write is the
	// default so a user who lands here typed --read at some point. Both halves of
	// that died with §9.17: /read reaches this state too, so the flag may never
	// have been typed, and §9.17 quotes this very notice as the tell that a
	// control was trapped at launch — "the notice says the room is read-only and
	// names the flag that would change it". The sentence outlived the controls
	// that made it false because this test was holding it in place.
	if !strings.Contains(m.st.Notice, "/write") {
		t.Errorf("the refusal does not name /write, the control that would grant it: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Notice, "--read") {
		t.Errorf("the refusal still names the launch flag: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Notice, "reopen") {
		t.Errorf("the refusal still prescribes a relaunch: %q", m.st.Notice)
	}
	// The step's own recorded reason travels with the chain and is read back off
	// the blocked step, so leaving it naming the flag would rebuild the defect
	// one surface over from the notice that was fixed.
	if got := m.flowChain.Current().Receipt.Detail; !strings.Contains(got, "/write") {
		t.Errorf("the blocked step's reason does not name /write: %q", got)
	}

	// And y must not rescue it either: the block is not a gate.
	m.key(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if log.n() != 0 {
		t.Fatalf("y dispatched a blocked write hop: %+v", log.specs)
	}
}

// (f) A bare "->" in ordinary prose creates NO flow.
//
// Only the explicit /flow prefix does. Without this, "compare approach A ->
// approach B" silently becomes an orchestration with write semantics attached.
func TestBareArrowInProseIsNotAFlow(t *testing.T) {
	countSpawns(t)
	m := flowRoom(t, true)
	m.st.Draft = "@codex which is better: publish write:docs/out.md -> or a PR?"

	m.dispatch()

	if m.flowChain != nil {
		t.Fatalf("prose containing '->' was parsed as a flow: %+v", m.flowChain.Steps)
	}
	if m.flowWritePending {
		t.Error("prose raised a write gate")
	}
	if m.flowReadHop {
		t.Error("a non-flow dispatch is carrying flow posture state")
	}
}

// Blocker 4's regression: retention must delete the OLDEST artifacts.
//
// Under the lexicographic comparator "turn-10-*" sorted before "turn-2-*", so
// crossing turn 10 — the first moment retention matters at all — pruned the
// newest files and kept the oldest.
func TestRetentionPrunesTheOldestTurnNotTheHighestString(t *testing.T) {
	dir := t.TempDir()
	// Turns 1..12 for one seat, plus an unparseable stray.
	for n := 1; n <= 12; n++ {
		name := "turn-" + itoa(n) + "-claude.md"
		if err := writeFile(dir, name); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeFile(dir, "notes.md"); err != nil {
		t.Fatal(err)
	}

	// Cap of 10 over 13 files deletes 13-10+1 = 4: the stray, then turns 1,2,3.
	if err := pruneSessionArtifacts(dir, 10); err != nil {
		t.Fatal(err)
	}

	for _, gone := range []string{"notes.md", "turn-1-claude.md", "turn-2-claude.md", "turn-3-claude.md"} {
		if fileExists(dir, gone) {
			t.Errorf("%s should have been pruned as oldest", gone)
		}
	}
	// The newest are what retention exists to keep, and they are exactly what
	// the string comparator deleted first.
	for _, kept := range []string{"turn-10-claude.md", "turn-11-claude.md", "turn-12-claude.md", "turn-4-claude.md"} {
		if !fileExists(dir, kept) {
			t.Errorf("%s was pruned — retention is deleting the newest", kept)
		}
	}
}

func writeFile(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte("x"), 0600)
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// The persistent seat's posture is chosen at process SPAWN — the permission
// flags are argv and nothing in the stream-json envelope changes them — so a
// hop that needs a different posture than the live process was launched with
// cannot be served by sending it the turn. It is respawned instead, on the same
// --resume composition /cd already uses.
//
// The alternative was the silent downgrade this whole change exists to refuse:
// the column would say READ while the live process still held write flags.
func TestAFlowReadHopRespawnsAWriteSpawnedSeat(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, true)

	// An ordinary turn in a --write room: the seat's process is spawned gated.
	m.st.Draft = "@claude do the thing"
	m.dispatch()
	if log.n() != 1 {
		t.Fatalf("setup: %d spawns, want 1", log.n())
	}
	first := m.procs[model.VendorClaude]
	if first == nil || first.posture != vendors.PostureWriteGated {
		t.Fatalf("setup: seat posture = %v, want write-gated", first)
	}

	// Now a flow READ hop to the same seat.
	m.turn = nil
	m.st.Draft = "/flow @claude review security -> @codex check"
	m.dispatch()

	if log.n() != 2 {
		t.Fatalf("%d spawns — the read hop reused a process spawned with write flags", log.n())
	}
	second := m.procs[model.VendorClaude]
	if second == first {
		t.Fatal("the same process object served both postures")
	}
	if second.posture != vendors.PostureRead {
		t.Errorf("respawned seat posture = %v, want read", second.posture)
	}
	want, err := vendors.Registry()[model.VendorClaude].(vendors.Persistent).
		Session(m.st.Workspace, string(model.VendorClaude), "", vendors.PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(log.specs[1].Args, " "); got != strings.Join(want.Args, " ") {
		t.Errorf("respawn argv:\n  %s\nwant the read session:\n  %s", got, strings.Join(want.Args, " "))
	}
}
