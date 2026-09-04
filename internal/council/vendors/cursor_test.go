package vendors

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// The Cursor seat's tests, rewritten wholesale with the seat.
//
// Everything here replays SHAPES captured live on 2026-08-08 against
// cursor-agent 2026.08.04-aaa8809 (design.md §9.36) with the ids, paths and
// prose synthesized — this repository is public and its fixtures are never real
// session content.
//
// The tests this file replaced were all about print mode: the `--` separator
// that kept a dash-leading brief from being read as a flag, the `--sandbox`
// branch, the whole-message repeat and the `model_call_id` that discriminated
// it. None of those surfaces exist on this seat any more. git history is the
// record of what they asserted.

// drive replays a stream through one protocol, collecting what came out in both
// directions.
//
// It is the #62 fixture-replay shape adapted to a two-way protocol: a test can
// assert on the room's events AND on the answers that would have gone back down
// the pipe, with no process anywhere near it.
type driven struct {
	events  []runner.Event
	replies [][]byte
	body    string
	acts    []runner.ActCall
}

func drive(p runner.Protocol, lines ...string) *driven {
	d := &driven{}
	d.replies = append(d.replies, p.Opening()...)
	for _, line := range lines {
		evs, out := p.Inbound([]byte(line))
		d.events = append(d.events, evs...)
		d.replies = append(d.replies, out...)
		for _, ev := range evs {
			if ev.Kind == runner.KindText {
				d.body += ev.Text
			}
			d.acts = append(d.acts, ev.Acts...)
		}
	}
	return d
}

// sent reports whether any line written back names this method.
func (d *driven) sent(method string) bool { return d.find(method) != nil }

func (d *driven) find(method string) map[string]any {
	for _, r := range d.replies {
		var m map[string]any
		if json.Unmarshal(r, &m) != nil {
			continue
		}
		if m["method"] == method {
			return m
		}
	}
	return nil
}

func (d *driven) kinds() []runner.EventKind {
	var out []runner.EventKind
	for _, ev := range d.events {
		out = append(out, ev.Kind)
	}
	return out
}

func fixture(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

const initOK = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`
const newOK = `{"jsonrpc":"2.0","id":2,"result":{"sessionId":"sess-fixture-1"}}`

// TestCursorIsDrivenAsALiveProcessAndNothingElse is the wholesale ruling as an
// assertion.
//
// The batch entry points survive only because Vendor requires them. If either
// ever returns a Spec again, something has quietly rebuilt the spawn-per-turn
// path that §9.33 measured at ~13s a turn and §9.36 replaced.
func TestCursorIsDrivenAsALiveProcessAndNothingElse(t *testing.T) {
	if _, err := (Cursor{}).FirstTurn("brief", "/ws", "cursor-agent", PostureRead); !errors.Is(err, ErrCursorIsLiveOnly) {
		t.Errorf("FirstTurn err = %v, want ErrCursorIsLiveOnly — a batch invocation of this seat is gone", err)
	}
	if _, err := (Cursor{}).NextTurn("brief", "/ws", "cursor-agent", "sess-1", PostureRead); !errors.Is(err, ErrCursorIsLiveOnly) {
		t.Errorf("NextTurn err = %v, want ErrCursorIsLiveOnly", err)
	}
	if _, ok := (Cursor{}).ParseEvent([]byte(`{"jsonrpc":"2.0"}`)); ok {
		t.Error("ParseEvent claimed a line; the ACP stream is only meaningful on the per-process protocol")
	}
	if _, ok := any(Cursor{}).(Persistent); ok {
		t.Error("Cursor satisfies Persistent, so the room will drive it with Turn()/Send() and never open a session")
	}
}

// TestCursorInvokesTheHiddenACPSubcommandAndNothingElse.
//
// One argument. Every flag the print-mode invocation carried belonged to a
// surface this seat no longer uses, and each one that reappeared here would be
// rejected by a subcommand that does not take it.
func TestCursorInvokesTheHiddenACPSubcommandAndNothingElse(t *testing.T) {
	for _, p := range []Posture{PostureRead, PostureWrite, PostureWriteGated} {
		spec, proto, err := Cursor{}.Open("/ws", "/usr/local/bin/cursor-agent", "", p)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(spec.Args, []string{"acp"}) {
			t.Errorf("posture %v args = %v, want exactly [acp]", p, spec.Args)
		}
		if spec.StdinPrompt != "" {
			t.Error("a stdin prompt appeared; every turn is a JSON-RPC request now")
		}
		if spec.Dir != "/ws" {
			t.Errorf("Dir = %q, want the workspace", spec.Dir)
		}
		if proto == nil {
			t.Fatal("Open returned no protocol; the seat would have no way to speak")
		}
	}
}

// TestCursorNeverPassesTheSkipPermissionsFlags is the safety rule for this
// vendor, kept across the rewrite rather than retired with the flags it names.
//
// -f/--force and --yolo are cursor-agent's "run everything" flags, --trust
// accepts a workspace trust prompt on the user's behalf, and --approve-mcps
// auto-approves servers that reach OUTSIDE the directory council was pointed at.
// None of them is reachable from an argv that is one word — which is exactly why
// the assertion stays: it is now cheap, and the day somebody adds a flag here is
// the day it is worth having.
func TestCursorNeverPassesTheSkipPermissionsFlags(t *testing.T) {
	for _, p := range []Posture{PostureRead, PostureWrite, PostureWriteGated} {
		spec, _, err := Cursor{}.Open("/ws", "/usr/local/bin/cursor-agent", "sess-1", p)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"-f", "--force", "--yolo", "--trust", "--approve-mcps"} {
			if slices.Contains(spec.Args, banned) {
				t.Errorf("posture %v passes %q; that is a consent decision this adapter does not get to make", p, banned)
			}
		}
	}
}

// TestCursorRunsTheBundleThroughNodeDirectly is the seat's whole existence on
// Windows.
//
// Detection resolves this vendor to the node.exe that cursor-agent.cmd would
// have run; the adapter has to hand that node its JavaScript entry point as the
// FIRST argument, or it starts a REPL and the room waits forever on a handshake
// nobody is reading. The bundle is derived from the binary rather than passed
// alongside it, so the two can never disagree about which install is driven.
func TestCursorRunsTheBundleThroughNodeDirectly(t *testing.T) {
	node := filepath.Join(`C:\Users\dev\AppData\Local\cursor-agent\versions\2026.08.04-aaa8809`, "node.exe")
	spec, _, err := Cursor{}.Open(`C:\ws`, node, "", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(node), "index.js")
	if !slices.Equal(spec.Args, []string{want, "acp"}) {
		t.Fatalf("Args = %v, want [%q acp]", spec.Args, want)
	}
}

// TestCursorLeavesANativeEntryPointAlone: on macOS and Linux the resolved binary
// IS cursor-agent, and prepending a JavaScript path to its argv would make the
// first thing it sees a file it was never asked to read.
func TestCursorLeavesANativeEntryPointAlone(t *testing.T) {
	spec, _, err := Cursor{}.Open("/ws", "/usr/local/bin/cursor-agent", "", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Args, []string{"acp"}) {
		t.Errorf("Args = %v — nothing should be prepended to a native entry point", spec.Args)
	}
}

// TestACPHandshakeIsSequencedByResponsesAndReleasesTheHeldTurn is the property
// that could not be expressed through Persistent at all, and the reason runner
// grew a Protocol.
//
// A turn taken before there is a session is HELD, not refused and not dropped:
// the room has already told the user this seat is working. It goes out the
// moment session/new answers, carrying the id that only that answer could
// supply.
func TestACPHandshakeIsSequencedByResponsesAndReleasesTheHeldTurn(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)

	opening := p.Opening()
	if len(opening) != 1 || !bytes.Contains(opening[0], []byte(`"initialize"`)) {
		t.Fatalf("opening = %s, want one initialize request", opening)
	}

	lines, err := p.Turn("the brief")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("a turn went out before there was a session to put it in: %s", lines)
	}

	_, out := p.Inbound([]byte(initOK))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"session/new"`)) {
		t.Fatalf("initialize did not lead to session/new: %s", out)
	}
	// Nothing about session/new needs initialize's result, so a pipelined client
	// would have sent both at once. It is sequenced deliberately: a failed
	// handshake must not be followed by a session request to a server that has
	// already said no.
	if bytes.Contains(out[0], []byte(`"session/prompt"`)) {
		t.Error("the turn went out beside session/new, before there was a session id")
	}

	evs, out := p.Inbound([]byte(newOK))
	if len(evs) != 1 || evs[0].Kind != runner.KindSession || evs[0].SessionID != "sess-fixture-1" {
		t.Fatalf("session/new did not announce the thread: %+v", evs)
	}
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"session/prompt"`)) {
		t.Fatalf("the held turn was not released: %s", out)
	}
	if !bytes.Contains(out[0], []byte("sess-fixture-1")) {
		t.Error("the released turn does not carry the session id it was waiting for")
	}
	if !bytes.Contains(out[0], []byte("the brief")) {
		t.Error("the released turn lost its prompt")
	}
}

// TestACPReadPostureSetsPlanModeBeforeTheTurnRuns.
//
// MEASURED: session/set_mode with modeId `plan` is accepted, and a seat in that
// mode declined to create a file. The ORDER is the assertion here — a brief
// dispatched beside the mode change would race the posture it is supposed to run
// under, and the race would be invisible, because a reply from the wrong mode
// looks exactly like a reply from the right one.
func TestACPReadPostureSetsPlanModeBeforeTheTurnRuns(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureRead)
	p.Opening()
	if _, err := p.Turn("the brief"); err != nil {
		t.Fatal(err)
	}
	p.Inbound([]byte(initOK))

	_, out := p.Inbound([]byte(newOK))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"session/set_mode"`)) {
		t.Fatalf("read posture did not ask for a mode: %s", out)
	}
	if !bytes.Contains(out[0], []byte(`"plan"`)) {
		t.Errorf("read posture asked for the wrong mode: %s", out[0])
	}
	for _, l := range out {
		if bytes.Contains(l, []byte(`"session/prompt"`)) {
			t.Fatal("the turn was dispatched beside the mode change rather than after it")
		}
	}

	_, out = p.Inbound([]byte(`{"jsonrpc":"2.0","id":3,"result":{}}`))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"session/prompt"`)) {
		t.Fatalf("the turn was not released once the mode landed: %s", out)
	}
}

// TestACPRefusedModeFailsTheTurnRatherThanRunningItAnyway.
//
// The alternative — dispatch anyway — is the silent upgrade dispatch.go refuses
// one layer up: the column's badge would claim a posture the seat is not in.
func TestACPRefusedModeFailsTheTurnRatherThanRunningItAnyway(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureRead)
	p.Opening()
	p.Turn("the brief")
	p.Inbound([]byte(initOK))
	p.Inbound([]byte(newOK))

	evs, out := p.Inbound([]byte(
		`{"jsonrpc":"2.0","id":3,"error":{"code":-32602,"message":"Invalid params","data":{"message":"no such mode"}}}`))
	for _, l := range out {
		if bytes.Contains(l, []byte(`"session/prompt"`)) {
			t.Fatal("the brief ran under a posture the seat could not be put in")
		}
	}
	if len(evs) != 1 || evs[0].Kind != runner.KindError || !evs[0].EndsTurn {
		t.Fatalf("a refused mode did not end the turn visibly: %+v", evs)
	}
	if !strings.Contains(evs[0].Note, "no such mode") {
		t.Errorf("the note drops the vendor's own words: %q", evs[0].Note)
	}
}

// TestACPWritePostureAssertsNoMode: `agent` is the server's own default on every
// captured session/new, so sending a set_mode to reassert it would be a request
// the room then has to defend, for no change in behaviour.
func TestACPWritePostureAssertsNoMode(t *testing.T) {
	for _, posture := range []Posture{PostureWrite, PostureWriteGated} {
		p := newACPProtocol("/ws", "", posture)
		p.Opening()
		p.Turn("the brief")
		p.Inbound([]byte(initOK))
		_, out := p.Inbound([]byte(newOK))
		for _, l := range out {
			if bytes.Contains(l, []byte(`"session/set_mode"`)) {
				t.Errorf("posture %v asks for a mode it already has: %s", posture, l)
			}
		}
	}
}

// TestACPTurnReplaysAsOneReadingOfEachPassage is the §9.6c property, re-asked
// against the protocol that replaced the one it was written for.
//
// Print mode sent a model call's deltas and then that call's COMPLETE message,
// so appending both rendered the passage twice, and the parser needed
// `model_call_id` to tell them apart. §9.33 saw no repeat across two ACP turns
// and said outright that two turns is a hypothesis rather than a rule. This
// fixture is a turn with two tool calls and three message segments in it — the
// shape §9.6c says the thin capture could not have ruled on — and the assertion
// is that the streamed body is the passages in order, once each.
func TestACPTurnReplaysAsOneReadingOfEachPassage(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Turn("count the lines")
	d := drive(p, fixture(t, "cursor-acp-turn.jsonl")...)

	const want = "Looking it up: second line is bravo, 2 lines total."
	if d.body != want {
		t.Errorf("streamed body =\n%q\nwant\n%q", d.body, want)
	}
	// The thought chunks are the vendor reasoning out loud: not its answer and
	// not a thing it did, so they belong in neither the body nor the trace.
	if strings.Contains(d.body, "Reading the notes file") {
		t.Error("reasoning leaked into the column body")
	}
	// Chrome for an interactive client. available_commands_update alone is
	// kilobytes on every session and would bury the turn beside it.
	if strings.Contains(d.body, "Fixture Turn") || strings.Contains(d.body, "rename-chat") {
		t.Error("client chrome was rendered as though the vendor had said it")
	}
}

// TestACPTurnEndsOnTheResponseAndCarriesNoCost.
//
// The end-of-turn signal is the ROOM'S OWN REQUEST being answered — there is no
// line on the stream that means "done" — which is the plainest statement of why
// a ParseFunc could not have driven this seat.
//
// And what the answer does not carry is the other half. Print mode's `result`
// held the whole reply and a token usage block; this holds a stop reason. Cost
// was already absent for this vendor forever; the tokens are gone now too, and
// so is the fallback a column that streamed nothing used to have.
func TestACPTurnEndsOnTheResponseAndCarriesNoCost(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Turn("count the lines")
	d := drive(p, fixture(t, "cursor-acp-turn.jsonl")...)

	var end *runner.Event
	for i := range d.events {
		if d.events[i].EndsTurn {
			end = &d.events[i]
		}
	}
	if end == nil {
		t.Fatal("no event ended the turn; on a persistent seat the column would wait forever")
	}
	if end.Kind != runner.KindMeta {
		t.Errorf("a clean end_turn was reported as %v", end.Kind)
	}
	if end.CostUSD != nil {
		t.Error("a cost appeared; this vendor publishes no monetary figure anywhere")
	}
	if end.Tokens != nil {
		t.Errorf("tokens = %+v; this vendor's prompt response carries no _meta and no usage, so the count is absent", *end.Tokens)
	}
	if end.Text != "" {
		t.Error("the turn's end carries reply text; ACP has no final whole reply and inventing one would be a fabrication")
	}
}

// TestACPToolCallsCarryTheirOutcomes, including the one that says nothing.
//
// The last row is the important one and it is not a hypothetical: a call the
// user REJECTED at the permission prompt arrives as `completed` with no
// rawOutput at all. On the wire a denial is indistinguishable from a completion
// that said nothing — so it must not become ActOK, and the room's own ActDenied,
// recorded from the keystroke, is what actually names it.
func TestACPToolCallsCarryTheirOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rawOut  string
		want    runner.ActStatus
		wantDet string
	}{
		{"a file was read", `"rawOutput":{"content":"alpha\n"}`, runner.ActOK, ""},
		{"a shell command ran", `"rawOutput":{"exitCode":0,"stdout":"ok\n"}`, runner.ActOK, ""},
		{"an edit landed", `"content":[{"type":"diff","path":"notes.txt"}]`, runner.ActOK, ""},
		{"it failed", `"rawOutput":{"error":"Hook blocked with message: nope"}`, runner.ActFailed, "Hook blocked with message: nope"},
		{"it said nothing at all", `"status":"completed"`, runner.ActUnknown, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line := `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{` +
				`"sessionUpdate":"tool_call_update","toolCallId":"call-1","status":"completed",` + tc.rawOut + `}}}`
			p := newACPProtocol("/ws", "", PostureWrite)
			d := drive(p, line)
			if len(d.acts) != 1 {
				t.Fatalf("acts = %+v, want one", d.acts)
			}
			if d.acts[0].Outcome != tc.want {
				t.Errorf("outcome = %v, want %v", d.acts[0].Outcome, tc.want)
			}
			if tc.wantDet != "" && d.acts[0].Detail != tc.wantDet {
				t.Errorf("detail = %q, want %q", d.acts[0].Detail, tc.wantDet)
			}
		})
	}
}

// TestACPNamesACallByTheVendorsOwnTitle.
//
// `title` is the only naming field always populated — rawInput came back EMPTY
// for Read, Find and grep on the live announcements — and for a shell call the
// title IS the command, which the vendor writes in backticks for a chat client.
// The backticks come off: the trace renders plain text beside three vendors that
// send none.
func TestACPNamesACallByTheVendorsOwnTitle(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	d := drive(p, fixture(t, "cursor-acp-turn.jsonl")...)

	var names []string
	for _, a := range d.acts {
		if a.Text != "" {
			names = append(names, a.Text)
		}
	}
	if !slices.Contains(names, "Read File") {
		t.Errorf("a tool whose rawInput was empty lost its name: %v", names)
	}
	if !slices.Contains(names, "wc -l notes.txt") {
		t.Errorf("a shell call is not named by its command, or kept its markdown backticks: %v", names)
	}
}

// TestACPCallIDsSurviveTheirEmbeddedNewline.
//
// These ids contain a literal newline in the middle —
// "call-aaaaaaaa-0\nfc_bbbbbbbb_0" — exactly as print mode's call_id did. It
// looks like corruption and is not: both halves of a call carry the identical
// string, so correlation works, and the id is never rendered.
func TestACPCallIDsSurviveTheirEmbeddedNewline(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	d := drive(p, fixture(t, "cursor-acp-turn.jsonl")...)

	var read []runner.ActCall
	for _, a := range d.acts {
		if strings.HasPrefix(a.ID, "call-aaaaaaaa") {
			read = append(read, a)
		}
	}
	if len(read) < 2 {
		t.Fatalf("the announcement and its outcome did not share an id: %+v", d.acts)
	}
	if !strings.Contains(read[0].ID, "\n") {
		t.Error("the embedded newline was stripped; the halves would no longer match")
	}
	if read[len(read)-1].Outcome != runner.ActOK {
		t.Errorf("the outcome did not land on the announced call: %+v", read)
	}
}

// TestACPDropsTheHistoryALoadedSessionReplays is the trap this protocol has that
// print mode did not, and it is the one that would have been most visible.
//
// MEASURED, twice: `session/load` streams the ENTIRE prior conversation back as
// ordinary session/update notifications BEFORE it answers — the old user
// prompts, the old tool calls with their real output, the old replies. A parser
// that appended them would refill a reattached column with the whole previous
// room and then answer the new brief underneath it.
func TestACPDropsTheHistoryALoadedSessionReplays(t *testing.T) {
	p := newACPProtocol("/ws", "old-thread-1", PostureWrite)
	p.Turn("what did you say before?")

	d := drive(p,
		initOK,
		// Everything from here to the load's own response is history.
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old-thread-1","update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"the previous brief"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old-thread-1","update":{"sessionUpdate":"tool_call","toolCallId":"replay-0-1","title":"Read File","kind":"read","status":"pending"}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old-thread-1","update":{"sessionUpdate":"tool_call_update","toolCallId":"replay-0-1","status":"completed","rawOutput":{"content":"old file body"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old-thread-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"the previous answer"}}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"modes":{"currentModeId":"agent"}}}`,
		// Live again.
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"old-thread-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"the new answer"}}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`,
	)

	if d.body != "the new answer" {
		t.Errorf("body = %q — the reattached column was refilled with the previous room", d.body)
	}
	if len(d.acts) != 0 {
		t.Errorf("replayed tool calls landed in the trace: %+v", d.acts)
	}
	// The loaded session keeps the id it was loaded with; there is no new one in
	// the response, which is what keeps the saved-room file valid across repeated
	// reattaches.
	if len(d.events) == 0 || d.events[0].Kind != runner.KindSession || d.events[0].SessionID != "old-thread-1" {
		t.Errorf("a loaded thread did not report its own id: %+v", d.events)
	}
	if !d.sent("session/load") {
		t.Error("a restored thread was not loaded")
	}
}

// TestACPDeadThreadOpensAFreshSessionInTheSameProcess.
//
// MEASURED: a fabricated id comes back `-32602 … Session "…" not found` in
// 0.45s, and — the part that decides the design — THE PROCESS SURVIVES. A fresh
// session opened in the same process 0.45s later and answered.
//
// So the one-attempt rule the ninth amendment established still holds and is now
// cheap: the id is spent, a new conversation opens immediately, and the brief
// still runs. The print-mode path paid a whole process to learn the same thing
// by exiting.
func TestACPDeadThreadOpensAFreshSessionInTheSameProcess(t *testing.T) {
	p := newACPProtocol("/ws", "dead-thread", PostureWrite)
	p.Turn("the brief")
	d := drive(p,
		initOK,
		`{"jsonrpc":"2.0","id":2,"error":{"code":-32602,"message":"Invalid params","data":{"message":"Session \"dead-thread\" not found"}}}`,
		`{"jsonrpc":"2.0","id":3,"result":{"sessionId":"fresh-thread"}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fresh-thread","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"FRESH"}}}}`,
	)
	if !d.sent("session/new") {
		t.Fatal("a refused load did not open a new conversation; the brief would be lost")
	}
	if d.body != "FRESH" {
		t.Errorf("body = %q — the turn did not run on the replacement session", d.body)
	}
	var ids []string
	for _, ev := range d.events {
		if ev.Kind == runner.KindSession {
			ids = append(ids, ev.SessionID)
		}
	}
	if !slices.Contains(ids, "fresh-thread") {
		t.Errorf("the replacement thread was never announced: %v", ids)
	}
	if slices.Contains(ids, "dead-thread") {
		t.Error("the room was told it had restored a thread the vendor refused")
	}
	// A second load would rebuild the same doomed request on every turn for the
	// life of the room, which is the wedge the one-attempt rule exists to stop.
	var loads int
	for _, r := range d.replies {
		if bytes.Contains(r, []byte(`"session/load"`)) {
			loads++
		}
	}
	if loads != 1 {
		t.Errorf("session/load was sent %d times; a dead id must be spent exactly once", loads)
	}
}

const permRequest = `{"jsonrpc":"2.0","id":0,"method":"session/request_permission","params":{` +
	`"sessionId":"s","toolCall":{"toolCallId":"call-9","title":"` + "`mkdir zzz`" + `","kind":"execute",` +
	`"content":[{"type":"content","content":{"type":"text","text":"Not in allowlist: mkdir"}}]},` +
	`"options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},` +
	`{"optionId":"allow-always","name":"Allow always","kind":"allow_always"},` +
	`{"optionId":"reject-once","name":"Reject","kind":"reject_once"}]}}`

// TestACPPermissionRequestIsHandedToTheRoomAndNotAnsweredBehindIt.
//
// The vendor is BLOCKED until this is answered — measured, both branches, with a
// rejection leaving the command unrun — and that block is the entire value of
// the card. So the protocol emits the question and writes NOTHING: an adapter
// that answered here would be deciding on the user's behalf while their card was
// still on screen.
func TestACPPermissionRequestIsHandedToTheRoomAndNotAnsweredBehindIt(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	evs, out := p.Inbound([]byte(permRequest))
	if len(out) != 0 {
		t.Fatalf("the protocol answered a question the user is being shown: %s", out)
	}
	if len(evs) != 1 || evs[0].Kind != runner.KindGate || evs[0].Gate == nil {
		t.Fatalf("no gate was raised: %+v", evs)
	}
	g := evs[0].Gate
	if g.RequestID == "" {
		t.Error("the gate has no id to answer with")
	}
	if g.ToolUseID != "call-9" {
		t.Errorf("ToolUseID = %q — the card and its trace entry are two things on screen", g.ToolUseID)
	}
	if g.Text != "mkdir zzz" {
		t.Errorf("Text = %q, want the command with its markdown backticks off", g.Text)
	}
	// Claude's Input exists because ITS protocol requires the tool's whole
	// argument blob echoed back on an approval. ACP's answer is an option id, so
	// carrying the blob would be a copy of a Write's entire file content held for
	// no purpose.
	if g.Input != nil {
		t.Error("the gate carries an argument blob nothing will ever send")
	}

	lines, err := p.Decide(g.RequestID, true, denialTextForTest, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !bytes.Contains(lines[0], []byte("allow-once")) {
		t.Fatalf("the approval did not go back: %s", lines)
	}
	if !bytes.Contains(lines[0], []byte(`"id":0`)) {
		t.Errorf("the answer does not carry the vendor's own request id: %s", lines[0])
	}
}

const denialTextForTest = "denied by the person running this council room"

// TestACPNeverSelectsAllowAlways is a rule rather than a default.
//
// `allow-always` writes a PERMANENT rule into the user's own
// ~/.cursor/cli-config.json. Council reaching into somebody's config to widen
// what an agent may do without being asked again is the same line this adapter
// already declines to cross by never passing --trust.
func TestACPNeverSelectsAllowAlways(t *testing.T) {
	for _, allow := range []bool{true, false} {
		p := newACPProtocol("/ws", "", PostureWrite)
		evs, _ := p.Inbound([]byte(permRequest))
		lines, err := p.Decide(evs[0].Gate.RequestID, allow, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(lines[0], []byte("allow-always")) {
			t.Errorf("allow=%v selected allow-always, which edits the user's own config: %s", allow, lines[0])
		}
		want := "reject-once"
		if allow {
			want = "allow-once"
		}
		if !bytes.Contains(lines[0], []byte(want)) {
			t.Errorf("allow=%v did not select %s: %s", allow, want, lines[0])
		}
	}
}

// TestACPReadPostureAnswersAPermissionRequestItself.
//
// A read-posture seat asking to change something is not a question for the user;
// it is already answered. Raising a card would offer authority this posture
// withheld — the same silent upgrade dispatch.go refuses when a write hop lands
// in a read room.
//
// It is still REPORTED. A seat that tried and was stopped is a thing that
// happened, and a column that hid it would read as one that never tried.
func TestACPReadPostureAnswersAPermissionRequestItself(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureRead)
	evs, out := p.Inbound([]byte(permRequest))
	if len(out) != 1 || !bytes.Contains(out[0], []byte("reject-once")) {
		t.Fatalf("a read-posture seat's request was not refused: %s", out)
	}
	if len(evs) != 1 || evs[0].Kind != runner.KindActivity {
		t.Fatalf("the refusal was hidden from the trace: %+v", evs)
	}
	if evs[0].Acts[0].Outcome != runner.ActFailed {
		t.Errorf("outcome = %v, want the call recorded as stopped", evs[0].Acts[0].Outcome)
	}
	if !strings.Contains(evs[0].Acts[0].Detail, "read-only") {
		t.Errorf("the trace does not say why: %q", evs[0].Acts[0].Detail)
	}
	for _, ev := range evs {
		if ev.Kind == runner.KindGate {
			t.Error("a read-only room offered the user write authority it never granted")
		}
	}
}

// TestACPUnknownServerRequestIsAnsweredRatherThanIgnored.
//
// `cursor/create_plan` was captured in plan mode and is a vendor extension. A
// request left unanswered blocks the vendor FOREVER, which on a persistent seat
// is a column that never finishes and a room that never lets go of the turn. An
// empty object is the smallest well-formed thing that unblocks it — and the
// vendor accepted it, with the call it belonged to completing immediately after.
// Guessing at a payload would be council inventing a side of a protocol it has
// not read.
func TestACPUnknownServerRequestIsAnsweredRatherThanIgnored(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	evs, out := p.Inbound([]byte(`{"jsonrpc":"2.0","id":7,"method":"cursor/create_plan","params":{"plan":"…"}}`))
	if len(out) != 1 {
		t.Fatalf("an unknown request was left hanging: %s", out)
	}
	if !bytes.Contains(out[0], []byte(`"id":7`)) {
		t.Errorf("the answer does not name the request: %s", out[0])
	}
	if bytes.Contains(out[0], []byte("optionId")) {
		t.Errorf("an unknown request was answered as though it were a permission prompt: %s", out[0])
	}
	if len(evs) != 0 {
		t.Errorf("a protocol detail was rendered to the user: %+v", evs)
	}
}

// TestACPCancelIsANotificationAndLeavesTheProcessAlive.
//
// VERIFIED LIVE: `session/cancel` carries no id and gets no response; what
// confirms it is the outstanding session/prompt resolving with
// `{"stopReason":"cancelled"}` 23ms later. The process took a further turn 1.1s
// after that.
//
// And `cancelled` is not a failure — it is the user's own keystroke coming back
// — so it must not reach the column as one. finishColumn's cancellation check is
// what words it.
func TestACPCancelIsANotificationAndLeavesTheProcessAlive(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Opening()
	p.Turn("the brief")
	p.Inbound([]byte(initOK))
	p.Inbound([]byte(newOK))

	lines, err := p.Interrupt("telltale-interrupt-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || !bytes.Contains(lines[0], []byte(`"session/cancel"`)) {
		t.Fatalf("cancel = %s", lines)
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatal(err)
	}
	if _, has := m["id"]; has {
		t.Error("cancel was sent as a request; it is a notification and nothing answers it")
	}

	evs, _ := p.Inbound([]byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"cancelled"}}`))
	if len(evs) != 1 || !evs[0].EndsTurn {
		t.Fatalf("a cancelled turn did not end: %+v", evs)
	}
	if evs[0].Kind == runner.KindError {
		t.Error("the user's own keystroke was reported as a vendor failure")
	}

	// The process is still there, so the next brief is a turn and not a respawn.
	next, err := p.Turn("another brief")
	if err != nil {
		t.Fatal(err)
	}
	if len(next) != 1 || !bytes.Contains(next[0], []byte(`"session/prompt"`)) {
		t.Fatalf("the seat would not take a turn after a cancel: %s", next)
	}
}

// TestACPInterruptOfAHeldTurnDropsItAndAsksToBeKilled covers BOTH windows in
// which a turn is held, and the second one is the one that was wrong.
//
// A brief the user cancelled must not be delivered to a session that opens
// moments later. The first guard keyed that on "is there a session yet", which
// missed the read-posture seat's OTHER wait — the session/set_mode round trip,
// which happens after the session id exists — so a cancelled brief in that
// window was dispatched anyway.
//
// And the interrupt must report an ERROR rather than a clean nothing. There is
// no outstanding session/prompt here, so no response is coming, so nothing would
// ever end the turn: the room would refuse every later brief and refuse to quit.
// The error is what makes interruptSeat fall through to its kill, which is the
// documented fallback for a cancel that could not be delivered — and nothing is
// lost by taking it, because no conversation has started.
func TestACPInterruptOfAHeldTurnDropsItAndAsksToBeKilled(t *testing.T) {
	for _, tc := range []struct {
		name    string
		posture Posture
		before  []string
	}{
		{"before the session exists", PostureWrite, []string{}},
		{"inside the set_mode window", PostureRead, []string{initOK, newOK}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newACPProtocol("/ws", "", tc.posture)
			p.Opening()
			p.Turn("the brief")
			for _, l := range tc.before {
				p.Inbound([]byte(l))
			}

			if _, err := p.Interrupt("x"); !errors.Is(err, ErrACPTurnNotStarted) {
				t.Fatalf("Interrupt err = %v, want ErrACPTurnNotStarted — nothing else would end this turn", err)
			}

			// Whatever is still owed by the handshake must not carry the brief.
			var out [][]byte
			for _, l := range []string{initOK, newOK, `{"jsonrpc":"2.0","id":3,"result":{}}`} {
				_, o := p.Inbound([]byte(l))
				out = append(out, o...)
			}
			for _, l := range out {
				if bytes.Contains(l, []byte(`"session/prompt"`)) {
					t.Fatalf("a cancelled brief was dispatched anyway: %s", l)
				}
			}
		})
	}
}

// TestACPTurnTakenInsideTheModeWindowStillWaitsForIt.
//
// The sequencing comment on session/set_mode claims a brief can never race the
// posture it is supposed to run under. Queueing on "is there a session yet" did
// not deliver that: once session/new answered, a turn arriving before the mode
// landed went straight out — under the server's default `agent`, while the
// column's badge said `ro:requested`. Invisible, because a reply from the wrong
// mode looks exactly like a reply from the right one.
func TestACPTurnTakenInsideTheModeWindowStillWaitsForIt(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureRead)
	p.Opening()
	p.Inbound([]byte(initOK))
	p.Inbound([]byte(newOK)) // session open, set_mode in flight

	lines, err := p.Turn("the brief")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("a brief went out under an unconfirmed posture: %s", lines)
	}

	_, out := p.Inbound([]byte(`{"jsonrpc":"2.0","id":3,"result":{}}`))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"session/prompt"`)) {
		t.Fatalf("the brief was not released once the mode landed: %s", out)
	}
}

// TestACPInterruptRejectsWhateverTheVendorIsBlockedOn.
//
// A pending session/request_permission holds the vendor still until it is
// answered. Whether session/cancel releases one has never been measured;
// rejection has — reject-once resolves the call and the command does not run. So
// the refusals go FIRST and the cancel reaches a vendor that is listening rather
// than one waiting on a question nobody will now answer.
//
// Rejecting is also what ctrl+c means for a call the user was being asked about.
// Approving on the way out would run the thing they just stopped.
func TestACPInterruptRejectsWhateverTheVendorIsBlockedOn(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Opening()
	p.Inbound([]byte(initOK))
	p.Inbound([]byte(newOK))
	p.Turn("the brief")
	p.Inbound([]byte(permRequest))

	lines, err := p.Interrupt("x")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %s, want a refusal and then the cancel", lines)
	}
	if !bytes.Contains(lines[0], []byte("reject-once")) {
		t.Errorf("the blocked call was not refused first: %s", lines[0])
	}
	if bytes.Contains(lines[0], []byte("allow")) {
		t.Errorf("cancelling ran the call the user was being asked about: %s", lines[0])
	}
	if !bytes.Contains(lines[1], []byte(`"session/cancel"`)) {
		t.Errorf("the turn was not cancelled after the vendor was unblocked: %s", lines[1])
	}
	// And the request is forgotten, so a later decision cannot answer it twice.
	if _, err := p.Decide("acp-perm-1", true, "", nil); !errors.Is(err, ErrACPUnknownRequest) {
		t.Errorf("the refused request is still answerable: %v", err)
	}
}

// TestAFailedHandshakeIsTerminalRatherThanASilentQueue is the wedge this state
// exists to prevent, and it is worth spelling out because the failure is a room
// nobody can quit.
//
// An ACP server that refuses `initialize` does NOT exit. So the room's stale-exit
// guard correctly reads a live process, keeps the seat, and hands it the next
// brief — which would be queued against a handshake that has already finished
// failing. Nothing answers, the turn never ends, no further brief can be
// dispatched, and `q` refuses to quit. The likeliest trigger is an
// unauthenticated CLI: somebody's first run.
func TestAFailedHandshakeIsTerminalRatherThanASilentQueue(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Opening()
	p.Turn("the first brief")

	evs, _ := p.Inbound([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"not signed in"}}`))
	if len(evs) != 1 || !evs[0].EndsTurn {
		t.Fatalf("the failed handshake did not end the turn it was dispatched for: %+v", evs)
	}

	lines, err := p.Turn("the next brief")
	if !errors.Is(err, ErrACPHandshakeFailed) {
		t.Fatalf("Turn err = %v, want ErrACPHandshakeFailed — a queued brief here never ends", err)
	}
	if len(lines) != 0 {
		t.Errorf("a dead protocol produced lines: %s", lines)
	}
}

// TestACPUnknownStopReasonIsNotRenderedAsAnAnswer.
//
// `refusal`, `max_tokens` and `max_turn_requests` are in the schema and none was
// observed here. The word is QUOTED rather than paraphrased: this adapter has
// never seen one and has no business translating it.
func TestACPUnknownStopReasonIsNotRenderedAsAnAnswer(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	p.Opening()
	p.Turn("the brief")
	p.Inbound([]byte(initOK))
	p.Inbound([]byte(newOK))
	evs, _ := p.Inbound([]byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"max_tokens"}}`))
	if len(evs) != 1 || evs[0].Kind != runner.KindError || !evs[0].EndsTurn {
		t.Fatalf("an unobserved stop reason passed as a clean turn: %+v", evs)
	}
	if !strings.Contains(evs[0].Note, "max_tokens") {
		t.Errorf("the vendor's own word was dropped: %q", evs[0].Note)
	}
}

// TestACPHandshakeFailureEndsTheTurnRatherThanHanging.
//
// EndsTurn is set even though no turn ever reached the vendor. The room
// dispatched to this seat; a column left streaming would wait forever on a
// process that is up and useless.
func TestACPHandshakeFailureEndsTheTurnRatherThanHanging(t *testing.T) {
	for _, tc := range []struct{ name, line string }{
		{"initialize refused", `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"not signed in"}}`},
		{"session refused", `{"jsonrpc":"2.0","id":2,"error":{"code":-32603,"message":"internal"}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newACPProtocol("/ws", "", PostureWrite)
			p.Opening()
			p.Turn("the brief")
			if strings.Contains(tc.line, `"id":2`) {
				p.Inbound([]byte(initOK))
			}
			evs, _ := p.Inbound([]byte(tc.line))
			if len(evs) != 1 || evs[0].Kind != runner.KindError || !evs[0].EndsTurn {
				t.Fatalf("a dead handshake left the column waiting: %+v", evs)
			}
		})
	}
}

// TestACPSurvivesGarbageOnTheStream: a wrapper's log line, a truncated object, a
// response to an id we never sent. None of them may fail the turn — the same
// rule every parser in this package follows, so that an upstream addition
// cannot break a column.
func TestACPSurvivesGarbageOnTheStream(t *testing.T) {
	p := newACPProtocol("/ws", "", PostureWrite)
	d := drive(p,
		"some wrapper wrote a plain line",
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","upda`,
		`{"jsonrpc":"2.0","id":998,"result":{"stopReason":"end_turn"}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"something_new_upstream","content":{"type":"text","text":"x"}}}}`,
		`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"s","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"still here"}}}}`,
	)
	if d.body != "still here" {
		t.Errorf("body = %q — noise on the stream cost the column its reply", d.body)
	}
	// An id we never issued must not end a turn: on a persistent seat that would
	// retire a column while the vendor was still talking.
	for _, k := range d.kinds() {
		if k == runner.KindMeta {
			t.Error("a response to an unknown id was read as this turn's end")
		}
	}
}
