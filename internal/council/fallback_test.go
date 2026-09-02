package council

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The room's side of vendors.LiveFallback (fallback.go) and vendors.GracefulStop
// (stopProc), through countSpawns and stubbed sessions. No vendor runs here,
// which is the whole point of both: the three live shapes have never been
// driven, and what these tests pin is what the ROOM does when one of them
// cannot be brought up, not what the vendor does.

// liveSeatsRoom is flowRoom with the DEFAULT registry: codex, antigravity and grok
// seated as their live shapes, which is the state a retreat starts from.
// flowRoom pins the batch registry for its own tests' reasons; this undoes
// that for the test's life (flowRoom's cleanup restores the same original).
func liveSeatsRoom(t *testing.T) *Model {
	t.Helper()
	live := vendors.Registry
	m := flowRoom(t, true)
	vendors.Registry = live
	m.st.Columns = append(m.st.Columns, Column{
		Vendor: model.VendorGrok, Label: "grok", Binary: "grok", Avail: AvailInstalled,
	})
	for i := range m.st.Columns {
		m.restampSeat(&m.st.Columns[i])
	}
	m.st.Width, m.st.Height = 120, 24
	m.brief = Brief{Path: "BRIEF.md", Text: "the operating brief"}
	return m
}

// refuseHandshake feeds spawn i's protocol the answer a refused initialize
// produces and hands the resulting events to the room, exactly as the runner
// would: the protocol marks itself Dead and reports a failed turn.
func refuseHandshake(t *testing.T, m *Model, log *spawnLog, i int, v model.VendorID) {
	t.Helper()
	proto, ok := log.protos[i]
	if !ok {
		t.Fatalf("spawn %d was not an RPC session", i)
	}
	proto.Opening()
	evs, _ := proto.Inbound([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"refused"}}`))
	if len(evs) == 0 {
		t.Fatal("a refused initialize produced no event")
	}
	for j := range evs {
		evs[j].Vendor = v
	}
	m.applyEvents(evs)
}

func batchSpec(t *testing.T, log *spawnLog, i int, want string) runner.Spec {
	t.Helper()
	if i >= len(log.specs) {
		t.Fatalf("spawn %d never happened (%d spawns)", i, len(log.specs))
	}
	spec := log.specs[i]
	if !strings.Contains(strings.Join(spec.Args, " "), want) {
		t.Fatalf("spawn %d is %s %v, want the batch invocation (%s)", i, spec.Binary, spec.Args, want)
	}
	return spec
}

// TestARefusedHandshakeFallsBackWithinTheSameTurn: the protocol refuses after
// the brief was handed over (it queues the turn behind the handshake), and
// the same brief reaches `codex exec` on the same dispatch — with the
// operating brief applied, since the batch seat's first turn is its first.
func TestARefusedHandshakeFallsBackWithinTheSameTurn(t *testing.T) {
	log := countSpawns(t)
	m := liveSeatsRoom(t)
	send(t, m, "@codex list the files")
	if len(log.specs) != 1 || len(log.protos) != 1 {
		t.Fatalf("spawns = %d (rpc %d), want one app-server spawn", len(log.specs), len(log.protos))
	}
	turn := m.st.Turn

	refuseHandshake(t, m, log, 0, model.VendorCodex)

	if !m.fellBack[model.VendorCodex] {
		t.Fatal("the seat did not record its retreat")
	}
	spec := batchSpec(t, log, 1, "exec")
	if p := specPrompt(spec); !strings.Contains(p, "list the files") || !strings.Contains(p, "the operating brief") {
		t.Errorf("the batch adapter did not get the brief and the operating brief:\n%s", p)
	}
	c := m.column(model.VendorCodex)
	if c.Phase != PhaseWaiting && c.Phase != PhaseStreaming {
		t.Errorf("phase = %v after the retreat; the column should still be live on the same turn", c.Phase)
	}
	if c.TurnN != turn || m.st.Turn != turn {
		t.Errorf("the retreat moved the turn: column %d, room %d, want %d", c.TurnN, m.st.Turn, turn)
	}
	ts := m.turnOf(model.VendorCodex)
	if ts == nil {
		t.Fatal("the seat left its dispatch")
	}
	if ts.persistent[model.VendorCodex] {
		t.Error("the seat is still booked as persistent after retreating to a one-shot process")
	}
	if _, ok := ts.seatHandles[model.VendorCodex]; !ok {
		t.Error("the batch process has no handle on the turn; x and ctrl+c could not reach it")
	}
	if _, ok := m.procs[model.VendorCodex]; ok {
		t.Error("the refused process is still registered as the seat's process")
	}
	if !strings.Contains(c.Note, "fell back to `codex exec --json`") {
		t.Errorf("note = %q, want the fallback named", c.Note)
	}
	if !strings.HasPrefix(c.Sandbox.Detail, seatShape(model.VendorCodex, true)+": ") {
		t.Errorf("badge detail = %q, want the fallback spelling first", c.Sandbox.Detail)
	}
	if strings.Contains(c.Sandbox.Detail, "unmeasured") {
		t.Errorf("the fallback is the measured seat and its badge says unmeasured: %q", c.Sandbox.Detail)
	}

	// The batch process lands the way any one-shot seat does, and the seat
	// stays batch on the next brief: no second app-server spawn.
	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindDone}})
	if c.Phase != PhaseDone {
		t.Errorf("phase = %v after the batch exit, want done", c.Phase)
	}
	send(t, m, "@codex and again")
	batchSpec(t, log, 2, "exec")
	if len(log.protos) != 1 {
		t.Errorf("rpc spawns = %d after the retreat, want the one that was refused", len(log.protos))
	}
}

// TestARetreatLeavesTheOtherSeatsInFlight: the retreat is per seat. A claude
// turn open on its own dispatch keeps its process and its turn.
func TestARetreatLeavesTheOtherSeatsInFlight(t *testing.T) {
	log := countSpawns(t)
	m := liveSeatsRoom(t)
	send(t, m, "@claude keep going")
	send(t, m, "@codex list the files")
	claudeTurn := m.turnOf(model.VendorClaude)
	if claudeTurn == nil {
		t.Fatal("claude is not in flight")
	}
	claudeBadge := m.column(model.VendorClaude).Sandbox
	claudePhase := m.column(model.VendorClaude).Phase
	spawns := len(log.specs)

	refuseHandshake(t, m, log, 1, model.VendorCodex)

	if m.turnOf(model.VendorClaude) != claudeTurn {
		t.Error("claude's dispatch changed under it")
	}
	if _, ok := m.procs[model.VendorClaude]; !ok {
		t.Error("claude's process was dropped by codex's retreat")
	}
	if got := m.column(model.VendorClaude).Phase; got != claudePhase {
		t.Errorf("claude phase = %v, was %v before codex retreated", got, claudePhase)
	}
	if m.fellBack[model.VendorClaude] {
		t.Error("claude was marked as fallen back")
	}
	if len(log.specs) != spawns+1 {
		t.Errorf("spawns = %d, want exactly one more (codex's batch process)", len(log.specs))
	}
	if got := m.column(model.VendorClaude).Sandbox; got != claudeBadge {
		t.Errorf("claude's badge moved with codex's retreat: %+v", got)
	}
}

// TestADeadWireFallsBackBeforeTheBriefIsSpent: the refusal arrived earlier (a
// room-open rebuild, say), so the process the seat holds is up and useless.
// The brief goes to the batch adapter on this press of enter, and the useless
// process is stopped.
func TestADeadWireFallsBackBeforeTheBriefIsSpent(t *testing.T) {
	log := countSpawns(t)
	m := liveSeatsRoom(t)
	rec := newStopRecorder(true)
	// Pinned to the directory and posture this dispatch wants, so seatProcess
	// hands the turn to THIS process rather than replacing it.
	m.procs[model.VendorCodex] = &seatProc{
		sess: rec, wire: refusedWire{}, sent: 1,
		dir:     m.seatDir(model.VendorCodex),
		posture: spawnPosture(vendors.Registry()[model.VendorCodex], m.seatPosture()),
	}

	send(t, m, "@codex list the files")

	if !m.fellBack[model.VendorCodex] {
		t.Fatal("a dead wire did not trigger the retreat")
	}
	if len(log.specs) != 1 {
		t.Fatalf("spawns = %d, want the one batch process", len(log.specs))
	}
	spec := batchSpec(t, log, 0, "exec")
	if p := specPrompt(spec); !strings.Contains(p, "list the files") || !strings.Contains(p, "the operating brief") {
		t.Errorf("the batch adapter did not get the brief:\n%s", p)
	}
	if got := rec.order(); !strings.HasSuffix(got, "kill") {
		t.Errorf("the useless process was not ended: %q", got)
	}
	c := m.column(model.VendorCodex)
	if c.Phase == PhaseFailed {
		t.Errorf("the column failed instead of retreating: %q", c.Note)
	}
	if !strings.Contains(c.Note, "fell back") {
		t.Errorf("note = %q", c.Note)
	}
}

// TestAPersistentSeatThatDiesBeforeNamingASessionFallsBack: the agy stream
// shape's refusal is a process death at argument parsing, before any `init`
// names a conversation. First turn, no session, no cancel: a retreat to
// `agy -p` on the same dispatch.
func TestAPersistentSeatThatDiesBeforeNamingASessionFallsBack(t *testing.T) {
	log := countSpawns(t)
	m := liveSeatsRoom(t)
	send(t, m, "@agy say hello")
	if len(log.specs) != 1 {
		t.Fatalf("spawns = %d, want one stream-json spawn", len(log.specs))
	}
	// The stub reports alive; the exit branch's stale-process guard reads
	// that as "this seat is fine", so the death has to be visible to it.
	m.procs[model.VendorAntigravity].sess = exitedSession{}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorAntigravity, Kind: runner.KindError, ExitCode: 2,
		Err: errors.New("exit status 2"), Note: "flag needs an argument: --input-format",
	}})

	if !m.fellBack[model.VendorAntigravity] {
		t.Fatal("the seat did not retreat")
	}
	spec := batchSpec(t, log, 1, "-p")
	if p := specPrompt(spec); !strings.Contains(p, "say hello") || !strings.Contains(p, "the operating brief") {
		t.Errorf("the batch adapter did not get the brief:\n%s", p)
	}
	c := m.column(model.VendorAntigravity)
	if c.Phase == PhaseFailed {
		t.Errorf("the column failed instead of retreating: %q", c.Note)
	}
	if !strings.HasPrefix(c.Sandbox.Detail, seatShape(model.VendorAntigravity, true)+": ") {
		t.Errorf("badge detail = %q, want the fallback spelling first", c.Sandbox.Detail)
	}
}

// TestADeathWithASessionNamedIsNotARetreat: the same exit on a seat that had
// answered once is a real death, and the column says what it always said.
func TestADeathWithASessionNamedIsNotARetreat(t *testing.T) {
	log := countSpawns(t)
	m := liveSeatsRoom(t)
	send(t, m, "@agy say hello")
	m.applyEvents([]runner.Event{{Vendor: model.VendorAntigravity, Kind: runner.KindSession, SessionID: "conv-1"}})
	m.procs[model.VendorAntigravity].sess = exitedSession{}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorAntigravity, Kind: runner.KindError, ExitCode: 1, Err: errors.New("exit status 1"),
	}})

	if m.fellBack[model.VendorAntigravity] {
		t.Error("a process that had named a session was treated as never up")
	}
	if len(log.specs) != 1 {
		t.Errorf("spawns = %d, want no batch respawn", len(log.specs))
	}
	if c := m.column(model.VendorAntigravity); c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed", c.Phase)
	}
}

// TestHelpPosturesAfterAFallback pins the badge on the postures page for a
// seat that retreated: the fallback spelling, the measured level, and no
// `unmeasured` anywhere on that column.
func TestHelpPosturesAfterAFallback(t *testing.T) {
	st := postureRoom()
	for i := range st.Columns {
		if st.Columns[i].Vendor == model.VendorCodex {
			st.Columns[i].Sandbox = postureClaimFor(model.VendorCodex, true, false, false, false, true)
		}
	}
	golden(t, "help-postures-fallback", render(st))
}

// TestTheFallbackBadgeOffWindowsIsTheMeasuredLevel: the one level the seat
// move lowered (seatshape_test.go) comes back with the seat that was measured.
func TestTheFallbackBadgeOffWindowsIsTheMeasuredLevel(t *testing.T) {
	if got := postureClaimFor(model.VendorCodex, false, false, false, false, true).Level; got != SandboxEnforced {
		t.Errorf("codex fallback off Windows = %v, want SandboxEnforced — `codex exec -s read-only` was measured on macOS", got)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCursor} {
		if postureClaimFor(v, true, false, false, false, true) != postureClaim(v, true, false, false, false) {
			t.Errorf("%s has no fallback and its claim moved with the flag", v)
		}
	}
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorGrok, model.VendorAntigravity} {
		w := postureClaimFor(v, true, true, false, false, true)
		if w.Level != SandboxWrite || !strings.HasPrefix(w.Detail, seatShape(v, true)+": ") || !strings.Contains(w.Detail, fallbackInvocation(v)) {
			t.Errorf("%s write fallback claim = %+v", v, w)
		}
	}
}

// --- the graceful stop ---

// stopRecorder is a session that records every verb the stop calls on it, in
// order, and whose exit the test controls.
type stopRecorder struct {
	mu    sync.Mutex
	calls []string
	done  chan struct{}
}

func newStopRecorder(exited bool) *stopRecorder {
	r := &stopRecorder{done: make(chan struct{})}
	if exited {
		close(r.done)
	}
	return r
}

func (r *stopRecorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}
func (r *stopRecorder) order() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.calls, " ")
}
func (r *stopRecorder) SendTurn([][]byte) error { r.add("turn"); return nil }
func (r *stopRecorder) SendAside(lines [][]byte) error {
	var s []string
	for _, l := range lines {
		s = append(s, string(l))
	}
	r.add("aside(" + strings.Join(s, ",") + ")")
	return nil
}
func (r *stopRecorder) Kill()                 { r.add("kill") }
func (r *stopRecorder) Alive() bool           { return true }
func (r *stopRecorder) CloseInput()           { r.add("close") }
func (r *stopRecorder) Done() <-chan struct{} { return r.done }

// stopWire is a protocol with a last word and a short grace.
type stopWire struct {
	closing [][]byte
	grace   time.Duration
}

func (w stopWire) Turn(string) ([][]byte, error)                                 { return nil, nil }
func (w stopWire) Interrupt(string) ([][]byte, error)                            { return [][]byte{[]byte("interrupt")}, nil }
func (w stopWire) Decide(string, bool, string, map[string]any) ([][]byte, error) { return nil, nil }
func (w stopWire) Closing() [][]byte                                             { return w.closing }
func (w stopWire) Grace() time.Duration                                          { return w.grace }

// refusedWire is a protocol whose handshake has failed: Turn refuses and Dead
// reports it, which is the shape both RPC protocols take (acp.go,
// codexappserver.go).
type refusedWire struct{}

func (refusedWire) Turn(string) ([][]byte, error)      { return nil, vendors.ErrAppServerHandshakeFailed }
func (refusedWire) Interrupt(string) ([][]byte, error) { return nil, nil }
func (refusedWire) Decide(string, bool, string, map[string]any) ([][]byte, error) {
	return nil, nil
}
func (refusedWire) Dead() bool { return true }

func stopRoom(t *testing.T, rec seatSession, wire seatWire) *Model {
	t.Helper()
	m, _, _, _ := teardownRoom(t)
	m.procs = map[model.VendorID]*seatProc{model.VendorCodex: {sess: rec, wire: wire}}
	return m
}

// TestTeardownSaysGoodbyeBeforeTheKill: Closing, CloseInput, the exit, then
// Kill — and a process that exits inside the grace is killed at once rather
// than after it.
func TestTeardownSaysGoodbyeBeforeTheKill(t *testing.T) {
	rec := newStopRecorder(true)
	m := stopRoom(t, rec, stopWire{closing: [][]byte{[]byte("interrupt"), []byte("refuse")}, grace: 10 * time.Second})
	start := time.Now()
	m.teardown()
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("teardown waited %v on a process that had already exited", took)
	}
	if got, want := rec.order(), "aside(interrupt,refuse) close kill"; got != want {
		t.Errorf("stop order = %q, want %q", got, want)
	}
	if m.ended != 1 {
		t.Errorf("ended = %d, want the one seat this teardown stopped", m.ended)
	}
}

// TestTheGraceIsBoundedWhenTheProcessNeverExits: the kill still follows, and
// it follows within the grace plus a little, not whenever the vendor feels
// like leaving (§9.50's measured 15 s straggler).
func TestTheGraceIsBoundedWhenTheProcessNeverExits(t *testing.T) {
	rec := newStopRecorder(false)
	m := stopRoom(t, rec, stopWire{closing: nil, grace: 50 * time.Millisecond})
	start := time.Now()
	m.teardown()
	took := time.Since(start)
	if took < 50*time.Millisecond {
		t.Errorf("teardown returned in %v, before the grace", took)
	}
	if took > 5*time.Second {
		t.Errorf("teardown waited %v on a process that never exits; the grace is the bound", took)
	}
	// Nothing to say for an idle seat: no aside at all, straight to the close.
	if got, want := rec.order(), "close kill"; got != want {
		t.Errorf("stop order = %q, want %q", got, want)
	}
}

// TestASeatWithNoLastWordIsKilledAtOnce: the stream-json seat has no
// GracefulStop and keeps the one-call teardown it was measured needing
// (Claude Code exits 0 on a closed stdin).
func TestASeatWithNoLastWordIsKilledAtOnce(t *testing.T) {
	rec := newStopRecorder(false)
	m := stopRoom(t, rec, claudeWire())
	m.teardown()
	if got := rec.order(); got != "kill" {
		t.Errorf("stop order = %q, want a bare kill", got)
	}
}

// TestACancelThatBecomesAKillStillClosesFirst: the per-seat cancel keeps a
// persistent process by interrupting it; when the interrupt cannot be
// queued the kill it falls back to is the graceful one.
func TestACancelThatBecomesAKillStillClosesFirst(t *testing.T) {
	rec := &refusingRecorder{stopRecorder: newStopRecorder(true)}
	m := stopRoom(t, rec, stopWire{closing: [][]byte{[]byte("bye")}, grace: 50 * time.Millisecond})
	m.holdTurn(&turnState{
		cancel:     func() {},
		seatCancel: map[model.VendorID]context.CancelFunc{},
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{model.VendorCodex: true},
	})
	if !m.cancelSeat(model.VendorCodex) {
		t.Fatal("nothing to cancel")
	}
	deadline := time.After(5 * time.Second)
	for !strings.HasSuffix(rec.order(), "kill") {
		select {
		case <-deadline:
			t.Fatalf("no kill followed the refused interrupt: %q", rec.order())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got, want := rec.order(), "aside(interrupt) aside(bye) close kill"; got != want {
		t.Errorf("stop order = %q, want %q", got, want)
	}
	if _, ok := m.procs[model.VendorCodex]; ok {
		t.Error("the killed process is still registered")
	}
}

// refusingRecorder records an aside and then refuses it, the way a full send
// queue does, so the interrupt cannot be delivered.
type refusingRecorder struct{ *stopRecorder }

func (r *refusingRecorder) SendAside(lines [][]byte) error {
	_ = r.stopRecorder.SendAside(lines)
	if len(lines) == 1 && strings.Contains(string(lines[0]), "interrupt") {
		return runner.ErrSendBacklog
	}
	return nil
}
