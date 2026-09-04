package vendors

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The Grok ACP seat's tests.
//
// Every line here is SYNTHESIZED from the ACP schema
// (agentclientprotocol.com/protocol/schema, read 2026-09-02) and from the
// shapes cursor-agent was measured sending under the same protocol
// (design.md §9.36), with ONE exception: grokPromptMeta reproduces the SHAPE
// of the prompt response `grok agent stdio` was measured sending at 1.0.13
// on 2026-09-04 (acp.go's header quotes it), with every id and every figure
// replaced by a fake of the same type. The option ids below are invented so
// that a test can prove the seat reads them off the request rather than
// knowing them. §9.57 lists the runs that would replace the rest with a
// version-pinned capture.

func grokDriver(p Posture) *acpProtocol {
	return newACPProtocolWith(grokDialect, "C:\\Users\\dev\\code\\example-app", "", p)
}

const (
	grokInitCanLoad = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`
	grokInitNoLoad  = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{"loadSession":false}}}`
	grokInitBare    = `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1}}`
	grokSession     = `{"jsonrpc":"2.0","id":2,"result":{"sessionId":"55555555-5555-4555-8555-555555555555"}}`
	grokChunk       = `{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"55555555-5555-4555-8555-555555555555","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"ok"}}}}`
	grokPromptDone  = `{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`
	// The 1.0.13 shape: a per-prompt count at the top of `_meta`, and the
	// session's running total under `usage` with a `costUsdTicks` in it. The
	// figures are fakes chosen so the two accountings are visibly different
	// and neither is the other's multiple: a test that lands the wrong one
	// cannot pass by coincidence.
	grokPromptMeta = `{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","_meta":{"sessionId":"55555555-5555-4555-8555-555555555555","requestId":"66666666-6666-4666-8666-666666666666","promptId":"66666666-6666-4666-8666-666666666666","modelId":"grok-9.9","inputTokens":3210,"outputTokens":45,"totalTokens":3255,"cachedReadTokens":2000,"reasoningTokens":12,"usage":{"inputTokens":7777,"outputTokens":111,"totalTokens":7888,"cachedReadTokens":4000,"cacheCreationTokens":0,"reasoningTokens":30,"modelCalls":2,"apiDurationMs":1500,"costUsdTicks":123456789,"numTurns":2,"modelUsage":{"grok-9.9-build":{"inputTokens":7777,"outputTokens":111,"totalTokens":7888,"cachedReadTokens":4000,"cacheCreationTokens":0,"reasoningTokens":30,"modelCalls":2,"apiDurationMs":1500,"costUsdTicks":123456789}}}}}}`
	// A measured ZERO, which is a different fact from no count at all.
	grokPromptMetaZero = `{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","_meta":{"inputTokens":0,"outputTokens":0,"totalTokens":0}}}`
	// Half a count: one side present, the other absent. Never seen; guarded.
	grokPromptMetaHalf = `{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn","_meta":{"inputTokens":3210}}}`
	// A permission request whose option ids are NOT cursor's spelling, so a
	// seat that answered with "allow-once" would be caught echoing an id this
	// vendor never offered.
	grokPermission = `{"jsonrpc":"2.0","id":0,"method":"session/request_permission","params":{"sessionId":"55555555-5555-4555-8555-555555555555","toolCall":{"toolCallId":"call-9","title":"` + "`mkdir zzz`" + `","kind":"execute","status":"pending"},"options":[{"optionId":"proceed_once","name":"Allow once","kind":"allow_once"},{"optionId":"proceed_always","name":"Allow always","kind":"allow_always"},{"optionId":"refuse","name":"Reject","kind":"reject_once"}]}}`
	// The same request offering nothing the room could honestly select.
	grokPermissionAlwaysOnly = `{"jsonrpc":"2.0","id":0,"method":"session/request_permission","params":{"sessionId":"55555555-5555-4555-8555-555555555555","toolCall":{"toolCallId":"call-9","title":"` + "`mkdir zzz`" + `","kind":"execute"},"options":[{"optionId":"proceed_always","name":"Allow always","kind":"allow_always"}]}}`
)

func TestGrokAgentInvokesAgentStdioAndNothingElse(t *testing.T) {
	spec, proto, err := (GrokAgent{}).Open("C:\\ws", "grok.exe", "", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Args, []string{"agent", "stdio"}) {
		t.Fatalf("the invocation is `agent stdio` and nothing else, got %v", spec.Args)
	}
	// The workspace rides session/new's cwd, not --cwd; a saved thread rides
	// session/load, not --resume. One channel each.
	for _, a := range spec.Args {
		if strings.HasPrefix(a, "--") {
			t.Fatalf("no flag belongs on this seat's argv, got %q", a)
		}
	}
	if spec.StdinPrompt != "" || spec.Vendor != model.VendorGrok || spec.Dir != "C:\\ws" {
		t.Fatalf("spec = %+v", spec)
	}
	if proto == nil {
		t.Fatal("Open must return a protocol driver")
	}
}

func TestGrokACPHandshakeOpensASessionInTheWorkspaceAndReleasesTheHeldBrief(t *testing.T) {
	p := grokDriver(PostureRead)
	if lines, err := p.Turn("the brief"); err != nil || len(lines) != 0 {
		t.Fatalf("a turn before the session exists is held, got %v %v", lines, err)
	}
	d := drive(p, grokInitBare, grokSession)
	if !strings.Contains(string(d.replies[0]), `"initialize"`) {
		t.Fatalf("the opening must be initialize, got %s", d.replies[0])
	}
	// The client declares no fs capability it cannot honour, on the cursor
	// seat's rule, and the dialect adds nothing to the handshake.
	if !bytes.Contains(d.replies[0], []byte(`"readTextFile":false`)) {
		t.Fatalf("the initialize must decline the fs capability: %s", d.replies[0])
	}
	sn := d.find("session/new")
	if sn == nil {
		t.Fatal("session/new must follow the initialize response")
	}
	params, _ := sn["params"].(map[string]any)
	if params["cwd"] != "C:\\Users\\dev\\code\\example-app" {
		t.Fatalf("the workspace must ride session/new's cwd, got %v", params["cwd"])
	}
	if _, ok := params["mcpServers"]; !ok {
		t.Fatal("mcpServers is required by the schema and must be present, empty")
	}
	// No mode is requested in the read posture: nothing has named one for
	// this server, and a refused set_mode fails the turn.
	if d.sent("session/set_mode") {
		t.Fatal("the grok dialect must not ask for a mode nobody has seen its server honour")
	}
	if !d.sent("session/prompt") {
		t.Fatal("the held brief must go out once the session exists")
	}
	var session string
	for _, ev := range d.events {
		if ev.Kind == runner.KindSession {
			session = ev.SessionID
		}
	}
	if session != "55555555-5555-4555-8555-555555555555" {
		t.Fatalf("the session id must reach the room, got %q", session)
	}
}

func TestGrokACPLoadsASavedThreadOnlyWhenTheServerSaysItCan(t *testing.T) {
	const saved = "22222222-2222-4222-8222-222222222222"
	for _, tc := range []struct {
		name string
		init string
		load bool
	}{
		{"advertised", grokInitCanLoad, true},
		{"declined", grokInitNoLoad, false},
		{"unsaid", grokInitBare, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newACPProtocolWith(grokDialect, "C:/ws", saved, PostureWrite)
			d := drive(p, tc.init)
			if got := d.sent("session/load"); got != tc.load {
				t.Fatalf("session/load sent = %v, want %v — the schema says a client checks loadSession first", got, tc.load)
			}
			if got := d.sent("session/new"); got == tc.load {
				t.Fatalf("session/new sent = %v; a thread the server cannot load is spent on a fresh conversation", got)
			}
			for _, ev := range d.events {
				if ev.Kind == runner.KindError {
					t.Fatalf("an unloadable thread costs a round trip, not the turn: %+v", ev)
				}
			}
		})
	}
	// The cursor dialect keeps its measured behaviour: no gate, session/load
	// regardless, because every capture advertised the capability.
	p := newACPProtocol("C:/ws", saved, PostureWrite)
	if d := drive(p, grokInitBare); !d.sent("session/load") {
		t.Fatal("the cursor seat's measured load path must not grow the grok gate")
	}
}

func TestGrokACPStreamsTextAndEndsWithNoCost(t *testing.T) {
	p := grokDriver(PostureWrite)
	p.Turn("reply with the word ok")
	d := drive(p, grokInitBare, grokSession, grokChunk, grokPromptDone)
	if d.body != "ok" {
		t.Fatalf("streamed body = %q", d.body)
	}
	var end *runner.Event
	for i := range d.events {
		if d.events[i].EndsTurn {
			end = &d.events[i]
		}
	}
	if end == nil || end.Kind != runner.KindMeta {
		t.Fatalf("the prompt response must end the turn cleanly, got %+v", end)
	}
	// ABSENT, never zero. The batch seat reads the vendor's own
	// total_cost_usd off its `end` frame; this prompt response is
	// `{stopReason}` and nothing else, so there is no count and no cost.
	if end.CostUSD != nil {
		t.Fatalf("cost = %v on a wire nobody has seen carry one", *end.CostUSD)
	}
	if end.Tokens != nil {
		t.Fatalf("tokens = %+v on a frame that carried no _meta", *end.Tokens)
	}
}

// endOf is the event that ended the turn, or nil.
func endOf(d *driven) *runner.Event {
	var end *runner.Event
	for i := range d.events {
		if d.events[i].EndsTurn {
			end = &d.events[i]
		}
	}
	return end
}

// TestGrokACPCountsThisPromptsTokensAndStillShowsNoCost is the whole of the
// 1.0.13 change on this seat, and the two halves are one rule.
//
// The count that lands is the PER-PROMPT one at the top of `_meta`, never the
// session's running total under `usage`: the column's figure means this turn
// everywhere else in the room, and the vendor reports this turn's figure
// under its own name, so nothing is subtracted to get it. The cost stays
// ABSENT although `costUsdTicks` is right there on the same frame — a
// cumulative figure in a fixed-point unit that has never been checked against
// a dollar on this seam (acp.go's header says what is measured and what is
// not). Absent, not zero: `$0.0000` would be a claim.
func TestGrokACPCountsThisPromptsTokensAndStillShowsNoCost(t *testing.T) {
	p := grokDriver(PostureWrite)
	p.Turn("reply with the word ok")
	d := drive(p, grokInitBare, grokSession, grokChunk, grokPromptMeta)
	end := endOf(d)
	if end == nil || end.Kind != runner.KindMeta {
		t.Fatalf("the prompt response must end the turn cleanly, got %+v", end)
	}
	if end.Tokens == nil {
		t.Fatal("the per-prompt count on `_meta` did not reach the room")
	}
	if end.Tokens.Input != 3210 || end.Tokens.Output != 45 {
		t.Fatalf("tokens = %+v, want this prompt's own 3210/45, not the session total 7777/111", *end.Tokens)
	}
	if end.CostUSD != nil {
		t.Fatalf("cost = %v on a frame whose only cost is a tick nobody has priced on this seam", *end.CostUSD)
	}
	for _, ev := range d.events {
		if ev.CostUSD != nil {
			t.Fatalf("a cost reached the room on some other event: %+v", ev)
		}
	}
}

// TestGrokACPKeepsZeroTokensApartFromNoCount is §4a.1 on the count: a vendor
// that counted zero and a vendor that sent no count must not land alike, and a
// count with one half missing is no count.
func TestGrokACPKeepsZeroTokensApartFromNoCount(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame string
		want  *model.TokenCounts
	}{
		{"1.0.4 shape, no _meta", grokPromptDone, nil},
		{"a measured zero", grokPromptMetaZero, &model.TokenCounts{}},
		{"half a count", grokPromptMetaHalf, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := grokDriver(PostureWrite)
			p.Turn("the brief")
			end := endOf(drive(p, grokInitBare, grokSession, grokChunk, tc.frame))
			if end == nil {
				t.Fatal("the turn did not end")
			}
			switch {
			case tc.want == nil && end.Tokens != nil:
				t.Fatalf("tokens = %+v, want absent", *end.Tokens)
			case tc.want != nil && end.Tokens == nil:
				t.Fatal("tokens absent, want a measured zero")
			case tc.want != nil && *end.Tokens != *tc.want:
				t.Fatalf("tokens = %+v, want %+v", *end.Tokens, *tc.want)
			}
		})
	}
}

func TestGrokACPAnswersAPermissionByKindNeverBySpelling(t *testing.T) {
	for _, allow := range []bool{true, false} {
		p := grokDriver(PostureWrite)
		evs, out := p.Inbound([]byte(grokPermission))
		if len(out) != 0 {
			t.Fatalf("allow=%v: a write-posture request waits for the room, but the seat wrote %s", allow, out)
		}
		if len(evs) != 1 || evs[0].Kind != runner.KindGate || evs[0].Gate == nil {
			t.Fatalf("allow=%v: want a card, got %+v", allow, evs)
		}
		if evs[0].Gate.Text != "mkdir zzz" || evs[0].Gate.ToolUseID != "call-9" {
			t.Fatalf("allow=%v: the card is mis-named: %+v", allow, evs[0].Gate)
		}
		lines, err := p.Decide(evs[0].Gate.RequestID, allow, "", nil)
		if err != nil || len(lines) != 1 {
			t.Fatalf("allow=%v: Decide = %s %v", allow, lines, err)
		}
		want := "refuse"
		if allow {
			want = "proceed_once"
		}
		if !bytes.Contains(lines[0], []byte(`"optionId":"`+want+`"`)) {
			t.Fatalf("allow=%v: the answer must be the id this vendor offered for the kind, got %s", allow, lines[0])
		}
		for _, never := range []string{"allow-once", "reject-once", "proceed_always"} {
			if bytes.Contains(lines[0], []byte(never)) {
				t.Fatalf("allow=%v: answered with %q, an id the vendor did not offer for this kind or must never get: %s", allow, never, lines[0])
			}
		}
		if _, err := p.Decide(evs[0].Gate.RequestID, allow, "", nil); !errors.Is(err, ErrACPUnknownRequest) {
			t.Fatalf("an answered request is still answerable: %v", err)
		}
	}
}

func TestGrokACPCancelsARequestItCannotHonestlySelectOn(t *testing.T) {
	// The vendor offered only allow_always, which the room never selects. The
	// schema's own `cancelled` outcome is the answer: a prompt that is
	// cancelled is a call that does not run.
	p := grokDriver(PostureWrite)
	evs, _ := p.Inbound([]byte(grokPermissionAlwaysOnly))
	for _, allow := range []bool{true, false} {
		q := grokDriver(PostureWrite)
		e, _ := q.Inbound([]byte(grokPermissionAlwaysOnly))
		lines, err := q.Decide(e[0].Gate.RequestID, allow, "", nil)
		if err != nil || len(lines) != 1 {
			t.Fatalf("Decide = %s %v", lines, err)
		}
		if !bytes.Contains(lines[0], []byte(`"outcome":"cancelled"`)) || bytes.Contains(lines[0], []byte("optionId")) {
			t.Fatalf("allow=%v: want the cancelled outcome and no id, got %s", allow, lines[0])
		}
	}
	// And the read posture's own refusal takes the same road.
	r := grokDriver(PostureRead)
	_, out := r.Inbound([]byte(grokPermissionAlwaysOnly))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"outcome":"cancelled"`)) {
		t.Fatalf("a read-posture refusal with no reject option must cancel, got %s", out)
	}
	_ = evs
}

func TestGrokACPReadPostureRefusesAPermissionItself(t *testing.T) {
	p := grokDriver(PostureRead)
	evs, out := p.Inbound([]byte(grokPermission))
	if len(out) != 1 || !bytes.Contains(out[0], []byte(`"optionId":"refuse"`)) {
		t.Fatalf("a read-posture request must be refused with the vendor's reject id, got %s", out)
	}
	if len(evs) != 1 || evs[0].Kind != runner.KindActivity || evs[0].Acts[0].Outcome != runner.ActFailed {
		t.Fatalf("the refusal must land in the trace, got %+v", evs)
	}
	for _, ev := range evs {
		if ev.Kind == runner.KindGate {
			t.Fatal("a read-only room offered the user write authority it never granted")
		}
	}
}

func TestGrokACPInterruptRefusesWhatIsHeldThenCancels(t *testing.T) {
	p := grokDriver(PostureWrite)
	p.Turn("the brief")
	drive(p, grokInitBare, grokSession, grokPermission)
	lines, err := p.Interrupt("x")
	if err != nil || len(lines) != 2 {
		t.Fatalf("want a refusal then the cancel, got %s %v", lines, err)
	}
	if !bytes.Contains(lines[0], []byte(`"optionId":"refuse"`)) {
		t.Fatalf("the blocked call was not refused first: %s", lines[0])
	}
	if !bytes.Contains(lines[1], []byte(`"session/cancel"`)) {
		t.Fatalf("the turn was not cancelled after the vendor was unblocked: %s", lines[1])
	}
	// Before a session exists there is nothing to cancel, and the caller must
	// fall through to the kill.
	q := grokDriver(PostureWrite)
	q.Turn("the brief")
	if _, err := q.Interrupt("x"); !errors.Is(err, ErrACPTurnNotStarted) {
		t.Fatalf("a held brief must ask to be killed, got %v", err)
	}
}

func TestGrokACPClosingSaysGoodbyeOnlyToAnOpenTurn(t *testing.T) {
	// A turn in flight with a question held: refuse, then cancel.
	p := grokDriver(PostureWrite)
	p.Turn("the brief")
	drive(p, grokInitBare, grokSession, grokPermission)
	lines := p.Closing()
	if len(lines) != 2 || !bytes.Contains(lines[0], []byte("refuse")) || !bytes.Contains(lines[1], []byte("session/cancel")) {
		t.Fatalf("closing with a turn open: got %s", lines)
	}
	// An idle session says nothing: the prompt was answered.
	q := grokDriver(PostureWrite)
	q.Turn("the brief")
	drive(q, grokInitBare, grokSession, grokChunk, grokPromptDone)
	if lines := q.Closing(); len(lines) != 0 {
		t.Fatalf("an idle session has nothing to cancel, got %s", lines)
	}
	// No session at all: nothing, and a held brief is dropped.
	r := grokDriver(PostureWrite)
	r.Turn("the brief")
	if lines := r.Closing(); len(lines) != 0 {
		t.Fatalf("nothing to say before a session exists, got %s", lines)
	}
	if d := drive(r, grokInitBare, grokSession); d.sent("session/prompt") {
		t.Fatal("a brief held at teardown went out anyway")
	}
	if g := grokDriver(PostureRead).Grace(); g <= 0 {
		t.Fatalf("grace = %v, want a positive bound", g)
	}
}

func TestGrokACPFailedHandshakeIsTerminalAndTheFallbackIsTheMeasuredSeat(t *testing.T) {
	p := grokDriver(PostureWrite)
	p.Turn("the brief")
	d := drive(p, `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"not signed in"}}`)
	if len(d.events) != 1 || d.events[0].Kind != runner.KindError || !d.events[0].EndsTurn {
		t.Fatalf("a refused handshake must end the turn, got %+v", d.events)
	}
	if !p.Dead() {
		t.Fatal("the failure must be visible to the room's fallback decision")
	}
	if _, err := p.Turn("the next brief"); !errors.Is(err, ErrACPHandshakeFailed) {
		t.Fatalf("a dead protocol must refuse a turn, got %v", err)
	}
	if _, ok := (GrokAgent{}).Fallback().(Grok); !ok {
		t.Fatalf("the fallback must be the measured --single seat, got %T", GrokAgent{}.Fallback())
	}
	// The batch entry points ARE the measured seat's, so a caller that wants
	// one process per turn gets the invocation grok.go verified.
	got, _ := (GrokAgent{}).FirstTurn("--- fenced brief", "C:\\ws", "grok.exe", PostureWrite)
	want, _ := Grok{}.FirstTurn("--- fenced brief", "C:\\ws", "grok.exe", PostureWrite)
	if !slices.Equal(got.Args, want.Args) {
		t.Fatalf("FirstTurn diverged from the measured seat: %v vs %v", got.Args, want.Args)
	}
	if _, ok := Registry()[model.VendorGrok].(GrokAgent); !ok {
		t.Fatalf("the grok seat must be the ACP adapter, got %T", Registry()[model.VendorGrok])
	}
}
