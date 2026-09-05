package vendors

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The codex app-server seat's tests.
//
// Everything here replays SHAPES captured live on 2026-08-29 against codex-cli
// 0.149.1 (design.md §9.50) with the ids, paths and prose synthesized — this
// repository is public and its fixtures are never real session content. The
// version-pinned REAL capture is `testdata/wire/codex-app-server-0.149.1-turn.jsonl`
// and `wire_test.go` replays it.
//
// `drive` lives in cursor_test.go. It is the #62 fixture-replay shape adapted
// to a two-way protocol, and it is reused here rather than copied because a
// second copy would drift from the first the moment either protocol grew a
// direction.

func appServerDriver(p Posture) *appServerProtocol {
	return newAppServerProtocol("C:\\Users\\dev\\code\\example-app", "", p)
}

// The handshake lines, exactly as the server sent them, abridged where a thread
// record sat. Kept as consts so a test reads as the transcript it mirrors.
const (
	appServerInitOK = `{"id":1,"result":{"userAgent":"telltale-council/0.149.1","codexHome":"C:\\Users\\dev\\.codex","platformFamily":"windows","platformOs":"windows"}}`
	appServerThread = `{"id":2,"result":{"thread":{"id":"22222222-2222-7222-8222-222222222222","sessionId":"22222222-2222-7222-8222-222222222222","cwd":"C:\\Users\\dev\\code\\example-app"}}}`
	appServerTurnOK = `{"id":3,"result":{}}`
	appServerDone   = `{"method":"turn/completed","params":{"threadId":"22222222-2222-7222-8222-222222222222","turn":{"id":"44444444-4444-7444-a444-444444444444","status":"completed","error":null,"durationMs":32622}}}`
)

func TestAppServerHandshakeIsSequencedByResponsesAndReleasesTheHeldTurn(t *testing.T) {
	p := appServerDriver(PostureRead)

	// A brief arriving before the thread exists is HELD, never dropped: the room
	// has already told the user this seat is working, and a turn dropped here
	// would be a column that sat out a brief it was addressed in.
	lines, err := p.Turn("the brief")
	if err != nil || len(lines) != 0 {
		t.Fatalf("a turn taken before the thread exists must be queued, got %v %v", lines, err)
	}

	// drive issues Opening itself, so this is the seat's first byte.
	d := drive(p, appServerInitOK, appServerThread)
	if len(d.replies) == 0 || !strings.Contains(string(d.replies[0]), `"initialize"`) {
		t.Fatalf("the opening must be initialize and nothing else, got %s", d.replies[0])
	}

	// initialize's answer releases `initialized` AND thread/start, in that
	// order. The notification has no response to wait for, so pipelining the two
	// is not the pipelining the sequencing rule forbids.
	if !d.sent("initialized") {
		t.Fatal("the initialized notification must follow the initialize response")
	}
	if !d.sent("thread/start") {
		t.Fatal("thread/start must follow the initialize response")
	}
	// The thread's answer is what makes the id knowable and what releases the
	// held brief.
	var gotSession string
	for _, ev := range d.events {
		if ev.Kind == runner.KindSession {
			gotSession = ev.SessionID
		}
	}
	if gotSession != "22222222-2222-7222-8222-222222222222" {
		t.Fatalf("the thread id must reach the room as a session, got %q", gotSession)
	}
	if !d.sent("turn/start") {
		t.Fatal("the held brief must go out once the thread exists")
	}
}

func TestAppServerCarriesThePostureOnTheThreadAndNotOnArgv(t *testing.T) {
	// The posture is a `thread/start` parameter here, where `codex exec` takes
	// it as a first-turn-only `-s` flag that resume rejects outright. Pinning it
	// on the request is what keeps a future change from quietly moving it back
	// onto argv, where the resume trap lives.
	for _, tc := range []struct {
		posture Posture
		want    string
	}{
		{PostureRead, "read-only"},
		{PostureWrite, "workspace-write"},
		{PostureWriteGated, "workspace-write"},
	} {
		p := appServerDriver(tc.posture)
		d := drive(p, appServerInitOK)
		start := d.find("thread/start")
		if start == nil {
			t.Fatalf("posture %v sent no thread/start", tc.posture)
		}
		params, _ := start["params"].(map[string]any)
		if got := params["sandbox"]; got != tc.want {
			t.Fatalf("posture %v asked for sandbox %v, want %q", tc.posture, got, tc.want)
		}
		if got := params["cwd"]; got != "C:\\Users\\dev\\code\\example-app" {
			t.Fatalf("the workspace must ride thread/start's cwd, got %v", got)
		}
	}
}

func TestAppServerWritePostureDoesNotInheritTheExecSeatsWindowsFlag(t *testing.T) {
	// codex.go's write posture passes danger-full-access on Windows, because
	// `exec`'s .git override was MEASURED failing there (#311). The equivalent
	// override on THIS path is unmeasured, so copying the conclusion would be
	// inheriting a finding across surfaces — the move STATE.md rules against and
	// the whole reason this seat was re-measured rather than ported.
	if got := appServerSandbox(PostureWrite); got == windowsWriteSandboxMode {
		t.Fatalf("the app-server write posture must not inherit %q from the exec seat", got)
	}
}

func TestAppServerReadsAWholeMessageOnceDespiteTheRepeat(t *testing.T) {
	// MEASURED on this wire: the concatenated deltas equal the completed item's
	// text byte for byte, on every agentMessage of every arm. A parser that read
	// both would print every answer twice.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"item/started","params":{"item":{"type":"agentMessage","id":"msg_1","text":"","phase":"final_answer"}}}`,
		`{"method":"item/agentMessage/delta","params":{"itemId":"msg_1","delta":"wrote-"}}`,
		`{"method":"item/agentMessage/delta","params":{"itemId":"msg_1","delta":"ro.txt"}}`,
		`{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg_1","text":"wrote-ro.txt","phase":"final_answer"}}}`,
	)
	if d.body != "wrote-ro.txt\n" {
		t.Fatalf("the answer must be read exactly once, got %q", d.body)
	}
}

func TestAppServerSeparatesTwoMessagesInOneTurn(t *testing.T) {
	// A turn carries several COMPLETE messages — the sandbox arm produced a
	// commentary line and then a final answer. Appending the next message's
	// first delta to this one's last would run two sentences together, which is
	// the trap codex.go's trailing newline exists for.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"item/agentMessage/delta","params":{"itemId":"msg_1","delta":"first"}}`,
		`{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg_1","text":"first","phase":"commentary"}}}`,
		`{"method":"item/agentMessage/delta","params":{"itemId":"msg_2","delta":"second"}}`,
		`{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg_2","text":"second","phase":"final_answer"}}}`,
	)
	if d.body != "first\nsecond\n" {
		t.Fatalf("two complete messages must not run together, got %q", d.body)
	}
}

func TestAppServerPrintsACompletedMessageThatNeverStreamed(t *testing.T) {
	// The safety net the ACP seat explicitly does NOT have: if a build stops
	// streaming, the completed item is the only copy of the answer and the
	// column must fill from it rather than stay empty.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"msg_9","text":"no deltas arrived","phase":"final_answer"}}}`,
	)
	if d.body != "no deltas arrived\n" {
		t.Fatalf("a message that never streamed must still land, got %q", d.body)
	}
}

func TestAppServerCommandsCarryTheirOutcomes(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"item/started","params":{"item":{"type":"commandExecution","id":"exec-1","command":"cmd.exe /c dir","status":"inProgress","exitCode":null,"aggregatedOutput":null}}}`,
		`{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"exec-1","command":"cmd.exe /c dir","status":"completed","exitCode":0,"aggregatedOutput":"a\nb\n"}}}`,
		`{"method":"item/completed","params":{"item":{"type":"commandExecution","id":"exec-2","command":"cmd.exe /c >x.txt echo probe","status":"failed","exitCode":1,"aggregatedOutput":"Access is denied.\r\n"}}}`,
	)
	if len(d.acts) != 3 {
		t.Fatalf("want an announcement and two results, got %d: %+v", len(d.acts), d.acts)
	}
	if d.acts[0].Outcome != runner.ActPending {
		t.Fatalf("an announced command must be pending, got %v", d.acts[0].Outcome)
	}
	if d.acts[1].Outcome != runner.ActOK {
		t.Fatalf("exit 0 is a success, got %v", d.acts[1].Outcome)
	}
	if d.acts[2].Outcome != runner.ActFailed {
		t.Fatalf("exit 1 is a failure, got %v", d.acts[2].Outcome)
	}
	// The vendor's OWN words, never a diagnosis council composed. This is the
	// sandbox denial as it actually arrived.
	if d.acts[2].Detail != "Access is denied." {
		t.Fatalf("the failure detail must be the vendor's own line, got %q", d.acts[2].Detail)
	}
}

func TestAppServerCommandWithNoExitCodeIsNotCalledASuccess(t *testing.T) {
	// `status: "failed"` is load-bearing on this wire — it was captured on a
	// command that never started, where there is no exit code to read. A
	// `completed` with no exit code has never been seen and must not be
	// promoted: that is the move that put a read-only badge on a session that
	// could write (§9.2).
	if got := appServerOutcome("failed", nil); got != runner.ActFailed {
		t.Fatalf("a failed status with no exit code is a failure, got %v", got)
	}
	if got := appServerOutcome("completed", nil); got != runner.ActUnknown {
		t.Fatalf("a completed status with no exit code is unknown, got %v", got)
	}
}

func TestAppServerDropsTheBriefComingBackAndTheModelThinking(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"item/completed","params":{"item":{"type":"userMessage","id":"u1","content":[{"type":"text","text":"the brief"}]}}}`,
		`{"method":"item/completed","params":{"item":{"type":"reasoning","id":"rs_1","summary":[],"content":[]}}}`,
		`{"method":"item/completed","params":{"item":{"type":"somethingNobodyHasSeen","id":"z1","text":"invented"}}}`,
	)
	if d.body != "" || len(d.acts) != 0 {
		t.Fatalf("the brief, the thinking and an unknown item must all be dropped, got body %q acts %+v", d.body, d.acts)
	}
}

func TestAppServerTurnEndsOnTheNotificationAndCarriesNoCost(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread, appServerTurnOK, appServerDone)
	var ends int
	for _, ev := range d.events {
		if !ev.EndsTurn {
			continue
		}
		ends++
		if ev.Kind != runner.KindMeta {
			t.Fatalf("a completed turn ends as meta, got %v", ev.Kind)
		}
		// codex publishes no monetary figure anywhere, on either surface, so
		// CostUSD stays nil for this vendor forever. Deriving one from tokens is
		// on the rejected list.
		if ev.CostUSD != nil {
			t.Fatalf("this vendor reports no cost; got %v", *ev.CostUSD)
		}
	}
	if ends != 1 {
		t.Fatalf("a turn ends exactly once, got %d", ends)
	}
}

func TestAppServerUnknownStopStatusIsNotRenderedAsAnAnswer(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"turn/completed","params":{"turn":{"id":"t1","status":"somethingNobodyHasSeen","error":null}}}`)
	last := d.events[len(d.events)-1]
	if last.Kind != runner.KindError || !last.EndsTurn {
		t.Fatalf("an unseen stop status must end the turn as a failure, got %+v", last)
	}
	// Quoted rather than translated: this adapter has never seen one and has no
	// business paraphrasing it.
	if !strings.Contains(last.Note, "somethingNobodyHasSeen") {
		t.Fatalf("the vendor's own word must survive, got %q", last.Note)
	}
}

func TestAppServerInterruptedTurnIsTheUsersOwnKeystrokeComingBack(t *testing.T) {
	// DOC READ (app-server README, 2026-09-02): after `turn/interrupt` "the
	// turn finishes with status: "interrupted"". Until this landed the word
	// was the unseen-status example above and rendered as a failure — which
	// on a column the user just cancelled is the wrong word in the direction
	// this repo minds most. It ends the turn as meta, on the ACP seat's
	// `cancelled` precedent, and finishColumn's cancellation check words it.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"turn/completed","params":{"turn":{"id":"t1","status":"interrupted","error":null}}}`)
	last := d.events[len(d.events)-1]
	if last.Kind != runner.KindMeta || !last.EndsTurn {
		t.Fatalf("an interrupted turn is not a failure, got %+v", last)
	}
}

func TestAppServerTurnErrorCarriesTheVendorsOwnSentence(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"turn/completed","params":{"turn":{"id":"t1","status":"failed","error":{"message":"the model refused"}}}}`)
	last := d.events[len(d.events)-1]
	if last.Kind != runner.KindError || !strings.Contains(last.Note, "the model refused") {
		t.Fatalf("the vendor's sentence must reach the column, got %+v", last)
	}
}

func TestAppServerFailedHandshakeIsTerminalRatherThanASilentQueue(t *testing.T) {
	// Without this the seat wedges the whole room, and it wedges harder than the
	// ACP seat does: this process was measured surviving a stdin close by 15s,
	// so nothing else would stop it.
	p := appServerDriver(PostureRead)
	d := drive(p, `{"id":1,"error":{"code":-32600,"message":"not authenticated"}}`)
	if len(d.events) == 0 || d.events[0].Kind != runner.KindError || !d.events[0].EndsTurn {
		t.Fatalf("a refused handshake must end the turn, got %+v", d.events)
	}
	if !strings.Contains(d.events[0].Note, "not authenticated") {
		t.Fatalf("the vendor's own reason must survive, got %q", d.events[0].Note)
	}
	if _, err := p.Turn("another brief"); !errors.Is(err, ErrAppServerHandshakeFailed) {
		t.Fatalf("a dead handshake must refuse later turns, got %v", err)
	}
}

func TestAppServerNamelessThreadIsAFailureRatherThanAnEmptyID(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, `{"id":2,"result":{"thread":{"id":""}}}`)
	last := d.events[len(d.events)-1]
	if last.Kind != runner.KindError || !last.EndsTurn {
		t.Fatalf("a thread with no id must fail rather than seat an empty session, got %+v", last)
	}
}

func TestAppServerDeadThreadOpensAFreshOneInTheSameProcess(t *testing.T) {
	// UNMEASURED on this path and the code says so; this pins the one-attempt
	// rule as APPLIED rather than as a captured behaviour. The id is spent, a
	// new thread opens in the same process, and the brief still runs.
	p := newAppServerProtocol("C:\\Users\\dev\\code\\example-app", "99999999-9999-7999-8999-999999999999", PostureRead)
	d := drive(p, appServerInitOK, `{"id":2,"error":{"code":-32602,"message":"no rollout found"}}`)
	if !d.sent("thread/resume") {
		t.Fatal("a saved id must be resumed rather than ignored")
	}
	var starts int
	for _, r := range d.replies {
		var m map[string]any
		if json.Unmarshal(r, &m) == nil && m["method"] == "thread/start" {
			starts++
		}
	}
	if starts != 1 {
		t.Fatalf("a refused resume opens exactly one fresh thread, got %d", starts)
	}
	for _, ev := range d.events {
		if ev.Kind == runner.KindError {
			t.Fatalf("a refused resume costs two round trips, not the turn: %+v", ev)
		}
	}
}

func TestAppServerInterruptNamesTheThreadAndTheTurn(t *testing.T) {
	p := appServerDriver(PostureRead)
	drive(p, appServerInitOK, appServerThread,
		`{"method":"turn/started","params":{"threadId":"22222222-2222-7222-8222-222222222222","turn":{"id":"44444444-4444-7444-a444-444444444444","status":"inProgress"}}}`)

	lines, err := p.Interrupt("ignored")
	if err != nil || len(lines) != 1 {
		t.Fatalf("an interrupt with a turn in flight sends one request, got %v %v", lines, err)
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatal(err)
	}
	if m["method"] != "turn/interrupt" {
		t.Fatalf("want turn/interrupt, got %v", m["method"])
	}
	params, _ := m["params"].(map[string]any)
	// Both ids are REQUIRED by the schema, and the turn id is the reason
	// turn/started is parsed at all: the room's own bookkeeping cannot name the
	// turn the vendor is running.
	if params["threadId"] == "" || params["turnId"] != "44444444-4444-7444-a444-444444444444" {
		t.Fatalf("the interrupt must name both ids, got %v", params)
	}
}

func TestAppServerInterruptOfAHeldTurnDropsItAndAsksToBeKilled(t *testing.T) {
	// A cancelled brief that arrives anyway is the room doing something the user
	// just stopped. And nothing would end this turn — no turn/start was ever
	// sent — so the caller must fall through to the kill.
	p := appServerDriver(PostureRead)
	if _, err := p.Turn("the brief"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Interrupt("x"); !errors.Is(err, ErrAppServerTurnNotStarted) {
		t.Fatalf("a held turn must ask to be killed, got %v", err)
	}
	d := drive(p, appServerInitOK, appServerThread)
	if d.sent("turn/start") {
		t.Fatal("a cancelled brief must not go out behind the user's back")
	}
}

// The approval lines, synthesized in the v2 shape the app-server README and
// v2/item.rs describe (read 2026-09-02) and the v1 shape the 0.149.1 schema
// declared. None has been captured live.
const (
	appServerCmdApproval  = `{"jsonrpc":"2.0","id":0,"method":"item/commandExecution/requestApproval","params":{"threadId":"22222222-2222-7222-8222-222222222222","turnId":"44444444-4444-7444-a444-444444444444","itemId":"exec-7","kind":"command","command":"rm -rf build","cwd":"C:\\Users\\dev\\code\\example-app","reason":"clean the tree","commandActions":[{"type":"unknown"}],"additionalPermissions":{"network":{"enabled":true}}}}`
	appServerFileApproval = `{"jsonrpc":"2.0","id":1,"method":"item/fileChange/requestApproval","params":{"threadId":"22222222-2222-7222-8222-222222222222","turnId":"44444444-4444-7444-a444-444444444444","itemId":"patch-3","reason":"write outside the workspace","grantRoot":"C:\\Users\\dev"}}`
	appServerV1Approval   = `{"jsonrpc":"2.0","id":2,"method":"execCommandApproval","params":{"conversationId":"c","callId":"k","command":["cmd.exe","/c","dir"],"cwd":"C:\\ws"}}`
)

// decisions reads every `{"decision": …}` the driver wrote back, in order.
func decisions(d *driven) []string {
	var out []string
	for _, r := range d.replies {
		var m map[string]any
		if json.Unmarshal(r, &m) != nil || m["method"] != nil {
			continue
		}
		if res, ok := m["result"].(map[string]any); ok {
			if v, ok := res["decision"].(string); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

func TestAppServerAnswersEveryServerRequestItCannotLeaveBlocked(t *testing.T) {
	// The ACP seat's hardest-won lesson, carried across: a request left
	// unanswered blocks the vendor forever, which on a persistent seat is a
	// column that never finishes and a room that cannot even be quit.
	//
	// READ posture: the approval is declined on arrival — it asked for
	// `never` and a read seat asking to change something is already answered
	// — and the unknown request gets the smallest well-formed thing.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		appServerCmdApproval,
		`{"jsonrpc":"2.0","id":1,"method":"mcpServer/elicitation/request","params":{}}`,
	)
	var answered int
	for _, r := range d.replies {
		var m map[string]any
		if json.Unmarshal(r, &m) == nil && m["method"] == nil {
			answered++
		}
	}
	if answered != 2 {
		t.Fatalf("both server requests must be answered, got %d", answered)
	}
	if got := decisions(d); len(got) != 1 || got[0] != "decline" {
		t.Fatalf("a read-posture approval is declined in the v2 vocabulary, got %v", got)
	}
	// The attempt is still reported: a seat that tried and was stopped must not
	// read as one that never tried — and the trace names the command.
	if len(d.acts) != 1 || d.acts[0].Outcome != runner.ActFailed {
		t.Fatalf("the refusal must land in the trace, got %+v", d.acts)
	}
	if d.acts[0].Text != "commandExecution: rm -rf build" {
		t.Fatalf("the trace must name the call, got %q", d.acts[0].Text)
	}
	if _, err := p.Decide("app-server-approval-1", true, "", nil); !errors.Is(err, ErrAppServerUnknownRequest) {
		t.Fatalf("a read-posture refusal is not held open for the room, got %v", err)
	}
}

func TestAppServerWritePostureHandsAnApprovalToTheRoomAndWritesNothingBack(t *testing.T) {
	// The vendor is BLOCKED until the room answers, which is the whole value
	// of the card — so the request produces a Gate and no reply. UNMEASURED:
	// no arm has produced an approval on this path; what this pins is that
	// the room is offered the decision rather than the seat making it.
	p := appServerDriver(PostureWrite)
	d := drive(p, appServerInitOK, appServerThread, appServerCmdApproval, appServerFileApproval)
	if got := decisions(d); len(got) != 0 {
		t.Fatalf("a write-posture approval must wait for the room, but the seat answered %v", got)
	}
	var gates []*runner.Gate
	for _, ev := range d.events {
		if ev.Kind == runner.KindGate {
			gates = append(gates, ev.Gate)
		}
	}
	if len(gates) != 2 {
		t.Fatalf("want a card per approval, got %d", len(gates))
	}
	cmd, file := gates[0], gates[1]
	if cmd.Tool != "commandExecution" || cmd.Text != "commandExecution: rm -rf build" || cmd.ToolUseID != "exec-7" {
		t.Fatalf("the command card is mis-named: %+v", cmd)
	}
	if file.Tool != "fileChange" || file.Text != "fileChange: write outside the workspace" || file.ToolUseID != "patch-3" {
		t.Fatalf("the file card is mis-named: %+v", file)
	}
	// The ALLOWLIST: nothing the request carried beyond the command and the
	// reason reaches the card. `additionalPermissions`, `commandActions` and
	// `grantRoot` are on the wire above and have no destination.
	for _, g := range gates {
		if g.Input != nil || g.OldContent != "" || g.NewContent != "" {
			t.Fatalf("a codex card carries no argument blob and no preview, got %+v", g)
		}
	}
	if cmd.RequestID == file.RequestID {
		t.Fatal("two approvals must be decidable separately")
	}
}

func TestAppServerDecideAnswersEachWayInTheVendorsOwnWords(t *testing.T) {
	for _, tc := range []struct {
		name    string
		request string
		allow   bool
		want    string
	}{
		{"v2 command accepted", appServerCmdApproval, true, "accept"},
		{"v2 command declined", appServerCmdApproval, false, "decline"},
		{"v2 file change accepted", appServerFileApproval, true, "accept"},
		{"v2 file change declined", appServerFileApproval, false, "decline"},
		// The legacy pair keeps the 0.149.1 schema's spelling.
		{"v1 exec approved", appServerV1Approval, true, "approved"},
		{"v1 exec denied", appServerV1Approval, false, "denied"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := appServerDriver(PostureWrite)
			d := drive(p, appServerInitOK, appServerThread, tc.request)
			var g *runner.Gate
			for _, ev := range d.events {
				if ev.Kind == runner.KindGate {
					g = ev.Gate
				}
			}
			if g == nil {
				t.Fatal("no card was raised")
			}
			lines, err := p.Decide(g.RequestID, tc.allow, "denied by the person running this room", nil)
			if err != nil || len(lines) != 1 {
				t.Fatalf("Decide = %v, %v", lines, err)
			}
			var m map[string]any
			if err := json.Unmarshal(lines[0], &m); err != nil {
				t.Fatal(err)
			}
			res, _ := m["result"].(map[string]any)
			if res["decision"] != tc.want {
				t.Fatalf("decision = %v, want %q", res["decision"], tc.want)
			}
			// The vendor's own id, echoed raw. acceptForSession is never sent:
			// it widens what the agent may do without being asked again.
			if strings.Contains(string(lines[0]), "acceptForSession") {
				t.Fatal("the room approves per call, never for the session")
			}
			// Answered once. A second keystroke on the same card decides nothing.
			if _, err := p.Decide(g.RequestID, tc.allow, "", nil); !errors.Is(err, ErrAppServerUnknownRequest) {
				t.Fatalf("an answered request is still answerable: %v", err)
			}
		})
	}
}

func TestAppServerDecideRefusesARequestItIsNotHolding(t *testing.T) {
	// An error rather than a silent success, because a caller reading a clean
	// return as "the vendor was told yes" would leave a card on screen over a
	// vendor that had already moved on.
	p := appServerDriver(PostureWrite)
	if _, err := p.Decide("app-server-approval-1", true, "", nil); !errors.Is(err, ErrAppServerUnknownRequest) {
		t.Fatalf("Decide must refuse, got %v", err)
	}
}

func TestAppServerInterruptCancelsWhatTheVendorIsBlockedOnFirst(t *testing.T) {
	// acpProtocol's ordering: a pending approval holds the vendor still, so
	// it is answered — with the v2 `cancel`, the decision the schema says also
	// interrupts — before the interrupt is sent to a server that is listening.
	p := appServerDriver(PostureWrite)
	drive(p, appServerInitOK, appServerThread,
		`{"method":"turn/started","params":{"threadId":"22222222-2222-7222-8222-222222222222","turn":{"id":"44444444-4444-7444-a444-444444444444","status":"inProgress"}}}`,
		appServerCmdApproval)
	lines, err := p.Interrupt("x")
	if err != nil || len(lines) != 2 {
		t.Fatalf("want a cancel and then the interrupt, got %s %v", lines, err)
	}
	if !strings.Contains(string(lines[0]), `"decision":"cancel"`) {
		t.Fatalf("the blocked call was not cancelled first: %s", lines[0])
	}
	if !strings.Contains(string(lines[1]), `"turn/interrupt"`) {
		t.Fatalf("the turn was not interrupted after the vendor was unblocked: %s", lines[1])
	}
	if _, err := p.Decide("app-server-approval-1", true, "", nil); !errors.Is(err, ErrAppServerUnknownRequest) {
		t.Fatalf("the cancelled request is still answerable: %v", err)
	}
}

func TestAppServerClosingOwnsTheKillInOrder(t *testing.T) {
	// §9.50 measured stdin close not reliably stopping this server, so the
	// seat says goodbye in order: cancel anything held, interrupt a turn still
	// open, and then the room closes the pipe, waits Grace, and kills.
	t.Run("a turn in flight with an approval held", func(t *testing.T) {
		p := appServerDriver(PostureWrite)
		drive(p, appServerInitOK, appServerThread,
			`{"method":"turn/started","params":{"threadId":"22222222-2222-7222-8222-222222222222","turn":{"id":"44444444-4444-7444-a444-444444444444","status":"inProgress"}}}`,
			appServerFileApproval)
		lines := p.Closing()
		if len(lines) != 2 {
			t.Fatalf("want a cancel then an interrupt, got %s", lines)
		}
		if !strings.Contains(string(lines[0]), `"decision":"cancel"`) || !strings.Contains(string(lines[1]), `"turn/interrupt"`) {
			t.Fatalf("closing is out of order: %s", lines)
		}
	})
	t.Run("an idle thread says nothing", func(t *testing.T) {
		p := appServerDriver(PostureRead)
		drive(p, appServerInitOK, appServerThread, appServerTurnOK, appServerDone)
		if lines := p.Closing(); len(lines) != 0 {
			t.Fatalf("an idle thread has no turn to interrupt, got %s", lines)
		}
	})
	t.Run("a held brief is dropped rather than sent behind the user's back", func(t *testing.T) {
		p := appServerDriver(PostureRead)
		if _, err := p.Turn("the brief"); err != nil {
			t.Fatal(err)
		}
		if lines := p.Closing(); len(lines) != 0 {
			t.Fatalf("nothing to say before the thread exists, got %s", lines)
		}
		d := drive(p, appServerInitOK, appServerThread)
		if d.sent("turn/start") {
			t.Fatal("a brief held at teardown went out anyway")
		}
	})
	// The bound sits past every measured exit. The longest of the forty runs
	// at 0.151.0 was 14.73 s, and every one of them was the operator's
	// SessionEnd hooks running (see Grace). It is still a bound: a quit ends.
	if g := appServerDriver(PostureRead).Grace(); g < 15*time.Second || g > 30*time.Second {
		t.Fatalf("grace = %v, want a bound past the measured exits and short of a quit nobody waits for", g)
	}
}

func TestAppServerNamesItsFallbackTriggers(t *testing.T) {
	// Every shape that means "this protocol cannot be brought up" is terminal
	// and reported by Dead(), so the room can retreat to the measured exec
	// seat (vendors.LiveFallback) before spending a brief on it.
	for _, tc := range []struct {
		name  string
		lines []string
	}{
		{"initialize refused", []string{`{"id":1,"error":{"code":-32600,"message":"not authenticated"}}`}},
		{"initialize refused for this client's version", []string{`{"id":1,"error":{"code":-32602,"message":"unsupported client version"}}`}},
		{"thread/start is not a method this build knows", []string{appServerInitOK, `{"id":2,"error":{"code":-32601,"message":"Method not found"}}`}},
		{"a thread with no id", []string{appServerInitOK, `{"id":2,"result":{"thread":{"id":""}}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := appServerDriver(PostureRead)
			if p.Dead() {
				t.Fatal("a fresh protocol is not dead")
			}
			d := drive(p, tc.lines...)
			if !p.Dead() {
				t.Fatal("the failure must be terminal and visible to the room")
			}
			last := d.events[len(d.events)-1]
			if last.Kind != runner.KindError || !last.EndsTurn {
				t.Fatalf("the failure must end the turn it was dispatched for, got %+v", last)
			}
			if _, err := p.Turn("another brief"); !errors.Is(err, ErrAppServerHandshakeFailed) {
				t.Fatalf("a dead protocol must refuse a turn, got %v", err)
			}
		})
	}
	// And a healthy handshake is not.
	p := appServerDriver(PostureRead)
	drive(p, appServerInitOK, appServerThread)
	if p.Dead() {
		t.Fatal("a seated thread reported dead")
	}
	if _, ok := (CodexAppServer{}).Fallback().(Codex); !ok {
		t.Fatalf("the fallback must be the measured exec seat, got %T", CodexAppServer{}.Fallback())
	}
}

func TestAppServerAsksForAnApprovalPolicyPerPosture(t *testing.T) {
	// SCHEMA READ (v2/shared.rs, 2026-09-02): `never`, `on-request`,
	// `untrusted`. The read posture asks for nobody; the write postures ask
	// for the narrower of the two asking policies, and the gated posture
	// never reaches the seat as itself (spawnPosture collapses it).
	for _, tc := range []struct {
		posture Posture
		want    string
	}{
		{PostureRead, "never"},
		{PostureWrite, "on-request"},
		{PostureWriteGated, "on-request"},
	} {
		p := appServerDriver(tc.posture)
		d := drive(p, appServerInitOK)
		start := d.find("thread/start")
		if start == nil {
			t.Fatalf("posture %v sent no thread/start", tc.posture)
		}
		params, _ := start["params"].(map[string]any)
		if got := params["approvalPolicy"]; got != tc.want {
			t.Fatalf("posture %v asked for approvalPolicy %v, want %q", tc.posture, got, tc.want)
		}
		if got := params["approvalPolicy"]; got == "untrusted" {
			t.Fatal("untrusted is not adopted unmeasured; see appServerApprovalPolicy")
		}
	}
}

func TestAppServerRecordsUsageAndLimitsAndRendersNeither(t *testing.T) {
	// PARSED AND NOTHING ELSE, which is the line acpUsage stops at too. The
	// point of capturing them is that the follow-up is a read rather than a
	// re-measurement; rendering a quota or a token total is §7.15/§7.16
	// vocabulary and those seams have owners.
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		`{"method":"thread/tokenUsage/updated","params":{"threadId":"t","turnId":"u","tokenUsage":{"total":{"totalTokens":23760,"inputTokens":23595,"cachedInputTokens":6912,"cacheWriteInputTokens":0,"outputTokens":165,"reasoningOutputTokens":0},"last":{"totalTokens":23760,"inputTokens":23595,"cachedInputTokens":6912,"cacheWriteInputTokens":0,"outputTokens":165,"reasoningOutputTokens":0},"modelContextWindow":258400}}}`,
		`{"method":"account/rateLimits/updated","params":{"rateLimits":{"limitId":"codex","primary":{"usedPercent":10,"windowDurationMins":300,"resetsAt":1700000000},"secondary":{"usedPercent":2,"windowDurationMins":10080,"resetsAt":1700000000},"credits":{"hasCredits":false,"unlimited":false,"balance":"0"},"planType":"plus"}}}`,
	)
	if len(d.events) != 1 {
		// Only the KindSession from the thread opening. Neither figure produces
		// an event, and that is the assertion.
		t.Fatalf("usage and limits must produce no events, got %+v", d.events)
	}
	if p.usage == nil || p.usage.ModelContextWindow != 258400 || p.usage.Total.TotalTokens != 23760 {
		t.Fatalf("the token figures must be captured, got %+v", p.usage)
	}
	// The window PAIR is kept as the vendor sent it. "10% of a 5-hour window"
	// and "2% of a weekly window" are two different facts, and a room that
	// reduced them to one number would be answering a question nobody asked.
	if p.limits == nil || p.limits.Primary == nil || p.limits.Secondary == nil {
		t.Fatalf("both quota windows must be captured, got %+v", p.limits)
	}
	if p.limits.Primary.WindowDurationMins != 300 || p.limits.Secondary.WindowDurationMins != 10080 {
		t.Fatalf("the windows must survive as sent, got %+v %+v", p.limits.Primary, p.limits.Secondary)
	}
}

func TestAppServerSurvivesGarbageOnTheStream(t *testing.T) {
	p := appServerDriver(PostureRead)
	d := drive(p, appServerInitOK, appServerThread,
		"", "not json at all", `{"jsonrpc":"2.0"}`, `{`,
		`{"method":"item/completed","params":{"item":{"type":"agentMessage","id":"m","text":"still here","phase":"final_answer"}}}`,
	)
	if d.body != "still here\n" {
		t.Fatalf("garbage must be dropped without losing the turn, got %q", d.body)
	}
}

func TestCodexAppServerInvokesTheSubcommandAndNothingElse(t *testing.T) {
	spec, proto, err := (CodexAppServer{}).Open("C:\\ws", "codex.cmd", "", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Args, []string{"app-server"}) {
		t.Fatalf("the invocation is `app-server` and nothing else, got %v", spec.Args)
	}
	// No prompt can reach argv on this seat at all, which retires the whole
	// cmd.exe shim question codex.go has to route around.
	if spec.StdinPrompt != "" {
		t.Fatalf("no prompt belongs in the spec, got %q", spec.StdinPrompt)
	}
	if spec.Vendor != model.VendorCodex {
		t.Fatalf("this is the codex seat, got %v", spec.Vendor)
	}
	if proto == nil {
		t.Fatal("Open must return a protocol driver")
	}
}

func TestCodexAppServerHasNoBatchInvocation(t *testing.T) {
	if _, err := (CodexAppServer{}).FirstTurn("p", "w", "b", PostureRead); !errors.Is(err, ErrCodexAppServerIsLiveOnly) {
		t.Fatalf("want ErrCodexAppServerIsLiveOnly, got %v", err)
	}
	if _, err := (CodexAppServer{}).NextTurn("p", "w", "b", "s", PostureRead); !errors.Is(err, ErrCodexAppServerIsLiveOnly) {
		t.Fatalf("want ErrCodexAppServerIsLiveOnly, got %v", err)
	}
	if _, ok := (CodexAppServer{}).ParseEvent([]byte(`{"method":"turn/completed"}`)); ok {
		t.Fatal("this stream is a conversation, not a sequence of independent lines")
	}
}

func TestTheRoomSeatsTheAppServerAndKeepsExecAsTheFallback(t *testing.T) {
	// This test's predecessor pinned the ABSENCE: from 2026-08-29 the seat
	// stayed on `codex exec` because §9.50 measured a read-posture app-server
	// turn failing to inspect on two of three arms, and the follow-up was to
	// re-measure that first. On 2026-09-02 the seat moved WITHOUT that
	// re-measurement (design.md §9.57 says why and what it owes), so what is
	// pinned now is the shape of the honesty around the move: the measured
	// exec adapter is one step away as the fallback, and the badge says
	// unmeasured (seatshape_test.go in package council pins the words).
	seat, ok := Registry()[model.VendorCodex].(CodexAppServer)
	if !ok {
		t.Fatalf("the codex seat must be the app-server adapter, got %T", Registry()[model.VendorCodex])
	}
	if _, ok := seat.Fallback().(Codex); !ok {
		t.Fatalf("the fallback must be the measured exec adapter, got %T", seat.Fallback())
	}
	if _, ok := FallbackRegistry()[model.VendorCodex].(Codex); !ok {
		t.Fatalf("the fallback registry must seat exec, got %T", FallbackRegistry()[model.VendorCodex])
	}
}
