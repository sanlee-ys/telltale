package vendors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// Version-pinned wire fixtures: each vendor's REAL stream, sanitized.
//
// What these are for, in one sentence: a vendor can change its frames between a
// rehearsal and a demo, and nothing else in this repository would notice. Every
// other test in this package feeds a parser a line SOMEBODY WROTE — a literal in
// a test file, or `testdata/cursor-acp-turn.jsonl`, whose own design.md entry
// calls it "synthesized shapes". A synthesized line proves the parser handles the
// shape its author believed in. It cannot prove the vendor still sends it.
//
// So each fixture here is one short REAL turn, captured on the Windows reference
// box through the exact argv the seat builds, and then SANITIZED: every session
// id, uuid, path, username and operator-private command list replaced with an
// obviously-fake value of the same type and format, by string substitution rather
// than by re-marshalling, so keys, nesting, ordering and types are byte-identical
// to what came off the wire. Four were captured 2026-08-09; grok's was re-captured
// 2026-08-14 at 1.0.4. `testdata/wire/README.md` records what was substituted and
// what could not be captured at all.
//
// THE VERSION IS IN THE FILENAME, and that is the mechanism rather than a label.
// A fixture is a claim about ONE build of one CLI. When a vendor is upgraded, the
// honest move is a NEW capture under a new filename — not an edit to an old one,
// which would silently restate a measurement nobody re-ran. If a bump changes the
// frames, these tests fail loudly, here, against a file that names the version
// they were true of.
//
// The mechanism has now fired once and the result is recorded rather than assumed:
// grok reached 1.0.4 with nobody noticing four patch bumps, the seat was re-measured
// against it on 2026-08-14, and the wire came back UNCHANGED (design.md §9.39's
// 2026-08-14 amendment). A fixture that survives a bump is not a wasted one — it is
// the only thing that turns "probably fine" into a checked claim.
//
// The bar every fixture in this directory has to clear (ADR-001, design.md
// §4a.1): **a frame that could not be captured is NOT written from
// documentation.** Three of the five seats have no structured error frame at
// all — that is a measurement, recorded in the README as a measurement, and
// there is no invented `turn.failed` line anywhere in this directory.
const (
	claudeWireVersion = "2.1.226"            // claude --version
	codexWireVersion  = "0.147.0"            // codex --version -> codex-cli 0.147.0
	agyWireVersion    = "1.1.11"             // agy --version
	grokWireVersion   = "1.0.4 (d846eb93d9)" // grok --version
	cursorWireVersion = "2026.08.04-aaa8809" // cursor-agent --version
	// The codex seat has TWO pinned builds, and that is not a stale entry
	// above: they pin different SURFACES of one CLI. `codex-0.147.0-turn.jsonl`
	// is `codex exec --json`, the invocation the room seated until 2026-09-02
	// and keeps as the FALLBACK (design.md §9.57); this one is `codex
	// app-server`, the seat since then (§9.50 captured it, §9.57 seated it).
	// Both pins stay while both invocations can be dispatched.
	codexAppServerWireVersion = "0.149.1" // codex --version -> codex-cli 0.149.1
)

// wireFixture reads one captured stream, and refuses a line that is not valid
// JSON. That check is cheap and it is the sanitizer's regression test: the
// substitution is textual, so a bad replacement would corrupt a line rather than
// change a value, and every assertion below would then fail for the wrong reason.
func wireFixture(t *testing.T, name string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "wire", name))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !json.Valid([]byte(l)) {
			t.Fatalf("%s line %d is not valid JSON; the capture or its sanitization is corrupt", name, i+1)
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		t.Fatalf("%s is empty", name)
	}
	return out
}

// parsed is what one vendor's whole turn came to when its own ParseEvent read it.
type parsed struct {
	events    []runner.Event
	body      string
	acts      []runner.ActCall
	sessionID string
}

// replay feeds a whole captured turn through one seat's real ParseEvent.
//
// A declined line is skipped rather than counted: every adapter in this package
// deliberately ignores event types it does not model — that is what keeps an
// upstream addition from breaking a column — so "this line was dropped" is not
// evidence of anything on its own. What the assertions below check is the
// opposite and stronger claim: that the fields the room actually needs still
// come out the other side.
func replay(v Vendor, lines []string) *parsed {
	p := &parsed{}
	for _, l := range lines {
		ev, ok := v.ParseEvent([]byte(l))
		if !ok {
			continue
		}
		p.events = append(p.events, ev)
		if ev.Kind == runner.KindText {
			p.body += ev.Text
		}
		if ev.SessionID != "" {
			p.sessionID = ev.SessionID
		}
		p.acts = append(p.acts, ev.Acts...)
	}
	return p
}

func (p *parsed) last(kind runner.EventKind) *runner.Event {
	for i := len(p.events) - 1; i >= 0; i-- {
		if p.events[i].Kind == kind {
			return &p.events[i]
		}
	}
	return nil
}

// TestClaudeWireIsPinnedAt_2_1_226 replays a real `claude -p` turn.
//
// The prompt was "reply with the word ok", under the seat's own read-posture
// argv — including the --disallowedTools list, which is why the captured
// system/init reports a `tools` array holding only read tools. That array is
// left VERBATIM in the fixture on purpose: it is the standing evidence for
// claude.go's forty-line argument that --disallowedTools removes tools where
// --allowedTools does not, and a future build that quietly re-admitted Bash
// would show it here.
func TestClaudeWireIsPinnedAt_2_1_226(t *testing.T) {
	p := replay(Claude{}, wireFixture(t, "claude-"+claudeWireVersion+"-turn.jsonl"))

	if p.sessionID == "" {
		t.Error("no session id came out of the stream; every follow-up turn would re-send the brief instead of resuming")
	}
	if p.body != "ok" {
		t.Errorf("streamed body = %q, want %q — the text_delta path stopped working", p.body, "ok")
	}
	end := p.last(runner.KindMeta)
	if end == nil {
		t.Fatal("no result event; on a persistent seat nothing would end the turn")
	}
	if !end.EndsTurn {
		t.Error("the result event does not end the turn")
	}
	if end.Text != "ok" {
		t.Errorf("the result's whole-reply fallback = %q; §9.6c leans on it for a turn that streamed nothing", end.Text)
	}
	// This vendor publishes its own dollar figure, so the seat passes one
	// through. Present-and-nonzero rather than a specific number: the figure is
	// the account's, not the schema's.
	if end.CostUSD == nil {
		t.Error("no cost on the result; this vendor reports total_cost_usd and the room shows what it read")
	} else if *end.CostUSD <= 0 {
		t.Errorf("cost = %v, want the vendor's own positive figure", *end.CostUSD)
	}
}

// TestClaudeWireResumeNotFoundIsPinnedAt_2_1_226 is the error shape, and it is
// also the only ZERO shape in this directory that was actually observed.
//
// Captured by resuming a well-formed session id with no conversation behind it.
// It fails FAST and FREE — no model turn is spent — which is what made it
// cheap enough to capture honestly.
//
// The zero half matters as much as the error half (§4a.1): this frame carries
// `total_cost_usd: 0`, an all-zero usage block, `modelUsage: {}` and
// `iterations: []`. A MEASURED zero, not an absence — so the seat must surface a
// non-nil pointer to 0.0, not nil. Collapsing those two is the one regression
// this repository exists to prevent.
func TestClaudeWireResumeNotFoundIsPinnedAt_2_1_226(t *testing.T) {
	p := replay(Claude{}, wireFixture(t, "claude-"+claudeWireVersion+"-resume-not-found.jsonl"))

	end := p.last(runner.KindError)
	if end == nil {
		t.Fatal("a failed turn did not surface as an error; the column would report success")
	}
	if !end.EndsTurn {
		t.Error("the failed turn never ends; the column would wait forever")
	}
	// The frame's `result` key is absent; the vendor's account of the failure
	// lives in its `errors` array, and the column shows that sentence rather
	// than a generic "the turn failed". The id in the fixture is the sanitized
	// one, so the assertion is on the sentence, not the id.
	if !strings.Contains(end.Note, "No conversation found with session ID") {
		t.Errorf("Note = %q, want the vendor's own sentence from the errors array", end.Note)
	}
	if end.CostUSD == nil {
		t.Fatal("cost is ABSENT on a frame that reported zero; zero and absent are different states (§4a.1)")
	}
	if *end.CostUSD != 0 {
		t.Errorf("cost = %v, want a measured 0", *end.CostUSD)
	}
}

// TestCodexWireIsPinnedAt_0_147_0 replays a real `codex exec --json` turn.
//
// Four frames and no more: thread.started, turn.started, one item.completed
// carrying the whole agent_message, turn.completed carrying token counts. The
// absence is pinned too — turn.completed reports tokens and NO dollar figure, so
// this seat's CostUSD stays nil forever rather than being derived.
func TestCodexWireIsPinnedAt_0_147_0(t *testing.T) {
	p := replay(Codex{}, wireFixture(t, "codex-"+codexWireVersion+"-turn.jsonl"))

	if p.sessionID == "" {
		t.Error("no thread id came out of thread.started; the next turn could not resume")
	}
	// codex sends complete messages rather than deltas, and the adapter appends
	// a newline to each so two of them do not run together.
	if got := strings.TrimSpace(p.body); got != "ok" {
		t.Errorf("streamed body = %q, want %q", got, "ok")
	}
	end := p.last(runner.KindMeta)
	if end == nil {
		t.Fatal("no turn.completed reached the room")
	}
	if end.CostUSD != nil {
		t.Error("a cost appeared for codex; it reports token counts only, and deriving dollars from tokens is on the rejected list")
	}
}

// TestAgyWireIsPinnedAt_1_1_11 replays a real `agy -p` turn.
//
// One difference from the 1.1.10 capture agy.go was written against is visible
// in this fixture and is worth naming, because it is exactly the drift these
// files exist to catch: the reply arrived as a SINGLE step_update in state DONE
// carrying the whole `text_delta`, where 1.1.10 sent the text on ACTIVE and a
// trailing newline on DONE. The adapter accepts both states, so the column is
// unaffected — which is the point. The parser survived a schema change nobody
// announced, and now there is a file that would have shown it.
func TestAgyWireIsPinnedAt_1_1_11(t *testing.T) {
	p := replay(Antigravity{}, wireFixture(t, "agy-"+agyWireVersion+"-turn.jsonl"))

	if p.sessionID == "" {
		t.Error("no conversation id; --conversation resume would be impossible")
	}
	if got := strings.TrimSpace(p.body); got != "ok" {
		t.Errorf("streamed body = %q, want %q", got, "ok")
	}
	end := p.last(runner.KindMeta)
	if end == nil {
		t.Fatal("no result event reached the room")
	}
	if strings.TrimSpace(end.Text) != "ok" {
		t.Errorf("the result's whole-reply fallback = %q; for this vendor it is load-bearing, not defensive", end.Text)
	}
	if end.CostUSD != nil {
		t.Error("a cost appeared for agy; it publishes no monetary figure anywhere")
	}
	// user_input, the unnamed preamble step and checkpoint are conversation
	// plumbing, and agyPlumbing suppresses them. A build that started routing
	// them to the trace would show up as phantom steps here.
	for _, a := range p.acts {
		if strings.Contains(a.Text, "user_input") || strings.Contains(a.Text, "checkpoint") {
			t.Errorf("plumbing was rendered as an act: %q", a.Text)
		}
	}
}

// TestGrokWireIsPinnedAt_1_0_4 replays a real `grok --output-format
// streaming-json` turn.
//
// Three properties this fixture pins, each of which is a judgement call in
// grok.go rather than an accident of the schema:
//
//   - `text` deltas are genuinely token-level and concatenate to the reply.
//   - `thought` deltas are DROPPED. This capture is 32 thought frames against 1
//     of text, which is the ratio that argument was made on.
//   - `end` carries the vendor's OWN total_cost_usd, which is why this is the
//     one seat besides Claude that may show money at all.
//
// The 1.0.0 capture this replaces was taken with the prompt `reply with the word
// ok`, and this one is FENCED — the same `---` opening a briefed room sends. That
// is not cosmetic: §9.39's whole lesson is that a probe shaped like a greeting
// verified a case the product never sends, and the fixture is now the shape it
// does. The reply is still `ok`, so the assertions below did not move.
//
// What the 1.0.0 → 1.0.4 re-capture found, stated so the next reader does not
// re-derive it: the frame types, the key names, their nesting and their ordering
// are IDENTICAL. The one shape difference anywhere in the file is the key of the
// `modelUsage` map, `grok-4.5-build` → `grok-4.6-build`, which is the model id
// rather than a schema key — and this adapter does not read that map at all.
func TestGrokWireIsPinnedAt_1_0_4(t *testing.T) {
	p := replay(Grok{}, wireFixture(t, "grok-1.0.4-turn.jsonl"))

	if p.body != "ok" {
		t.Errorf("streamed body = %q, want %q", p.body, "ok")
	}
	if strings.Contains(p.body, "user wants") {
		t.Error("the model's reasoning leaked into the column body; `thought` must stay dropped")
	}
	end := p.last(runner.KindMeta)
	if end == nil {
		t.Fatal("no end event; this is the only frame carrying the session id, so the thread would be lost")
	}
	if end.SessionID == "" {
		t.Error("the end event names no session; the next turn could not resume")
	}
	if end.CostUSD == nil {
		t.Fatal("no cost on grok's end frame; it publishes total_cost_usd and the room shows what it read")
	}
	if *end.CostUSD <= 0 {
		t.Errorf("cost = %v, want the vendor's own positive figure", *end.CostUSD)
	}
}

// TestCursorACPWireIsPinnedAt_2026_08_04 replays a real ACP conversation.
//
// Unlike the other four this seat is Conversational, so the fixture is not fed
// to a ParseEvent — it is driven through the real acpProtocol, which answers the
// server as it goes. The capture is therefore the whole transcript INCLUDING the
// handshake, taken by a probe that issued byte-for-byte the requests Opening and
// promptLine build, so the ids line up and the state machine runs unmodified.
//
// The absences are the assertions that matter here, because each is a thing print
// mode had and ACP does not: no cost, no token usage, and no final whole reply.
func TestCursorACPWireIsPinnedAt_2026_08_04(t *testing.T) {
	p := newACPProtocol("C:/Users/dev/code/example-app", "", PostureWrite)
	p.Turn("reply with the word ok")
	d := drive(p, wireFixture(t, "cursor-agent-"+cursorWireVersion+"-turn.jsonl")...)

	if d.body != "ok" {
		t.Errorf("streamed body = %q, want %q", d.body, "ok")
	}
	if strings.Contains(d.body, "user requested") {
		t.Error("agent_thought_chunk leaked into the column body")
	}
	// available_commands_update and the generated chat title are chrome for an
	// interactive client; rendering either would bury the turn beside it.
	if strings.Contains(d.body, "example-command") || strings.Contains(d.body, "Okay Reply") {
		t.Error("client chrome was rendered as though the vendor had said it")
	}

	var session, end *runner.Event
	for i := range d.events {
		switch {
		case d.events[i].Kind == runner.KindSession:
			session = &d.events[i]
		case d.events[i].EndsTurn:
			end = &d.events[i]
		}
	}
	if session == nil || session.SessionID == "" {
		t.Fatal("session/new never produced a session id; the saved room would have nothing to store")
	}
	if end == nil {
		t.Fatal("nothing ended the turn; on a live process the column would wait forever")
	}
	if end.Kind != runner.KindError && end.CostUSD != nil {
		t.Error("a cost appeared; this vendor publishes no monetary figure anywhere")
	}
	if end.Text != "" {
		t.Error("the turn's end carries reply text; ACP has no final whole reply and inventing one would be a fabrication")
	}
}

// TestCursorACPWireLoadNotFoundIsPinnedAt_2026_08_04 is the reattach failure,
// captured on the same id sequence the seat itself uses when a saved room hands
// it a thread id.
//
// The assertion is the RECOVERY rather than an error, and that is the measured
// behaviour: a `session/load` on a thread the vendor no longer has answers
// -32602 and THE PROCESS SURVIVES, so the protocol spends the dead id, opens a
// fresh conversation in the same process, and the room hears about it through
// the ordinary turn. A build that started failing the seat here instead would
// cost a respawn on every aged-out thread.
func TestCursorACPWireLoadNotFoundIsPinnedAt_2026_08_04(t *testing.T) {
	p := newACPProtocol("C:/Users/dev/code/example-app",
		"22222222-2222-4222-8222-222222222222", PostureWrite)
	d := drive(p, wireFixture(t, "cursor-agent-"+cursorWireVersion+"-load-not-found.jsonl")...)

	if !d.sent("session/load") {
		t.Fatal("a resuming seat never asked to load its thread")
	}
	if !d.sent("session/new") {
		t.Error("a dead thread did not open a fresh conversation; the seat would be up and useless")
	}
	for _, ev := range d.events {
		if ev.Kind == runner.KindError {
			t.Errorf("a recoverable dead thread failed the seat: %q", ev.Note)
		}
	}
}

// TestCursorACPZeroTextChunksRendersFallbackSummary asserts that when an ACP stream
// emits 0 text chunks before ending, the turn end event carries a fallback summary.
func TestCursorACPZeroTextChunksRendersFallbackSummary(t *testing.T) {
	p := newACPProtocol("C:/Users/dev/code/example-app", "", PostureWrite)
	p.Turn("do something with tools")
	p.Opening()
	p.Inbound([]byte(`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":1,"agentCapabilities":{}}}`))
	p.Inbound([]byte(`{"jsonrpc":"2.0","id":2,"result":{"sessionId":"test-session"}}`))

	// Tool call without text chunks
	p.Inbound([]byte(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"test-session","update":{"sessionUpdate":"tool_call","toolCallId":"call-1","title":"Read File","kind":"read"}}}`))
	evs, _ := p.Inbound([]byte(`{"jsonrpc":"2.0","id":3,"result":{"stopReason":"end_turn"}}`))

	var end *runner.Event
	for i := range evs {
		if evs[i].EndsTurn {
			end = &evs[i]
		}
	}
	if end == nil {
		t.Fatal("turn end event missing")
	}
	if end.Text == "" {
		t.Error("end.Text is empty; expected fallback summary when 0 text chunks were streamed")
	}
	if !strings.Contains(end.Text, "0 text chunks") || !strings.Contains(end.Text, "Read File") {
		t.Errorf("end.Text = %q, want summary naming 0 text chunks and tool call Read File", end.Text)
	}
}

// TestCodexAppServerWireIsPinnedAt_0_149_1 replays a real `codex app-server`
// conversation.
//
// Like the Cursor seat and unlike the other four, this fixture is not fed to a
// ParseEvent — it is driven through the real appServerProtocol, which answers
// the server as it goes. The capture is the whole transcript INCLUDING the
// handshake, taken by a probe that issued byte-for-byte the requests Opening
// and openThread build, so the ids line up and the state machine runs
// unmodified.
//
// The turn behind it is the SANDBOX ARM (design.md §9.50): a read-posture seat
// told to write a file through cmd.exe. That is what makes it worth pinning
// rather than a cheaper "reply with ok" — the frames it exercises are the ones
// the room's honesty depends on. A denial arriving as a success, or the
// vendor's own `Access is denied.` going missing, would fail here.
func TestCodexAppServerWireIsPinnedAt_0_149_1(t *testing.T) {
	p := newAppServerProtocol("C:/Users/dev/code/example-app", "", PostureRead)
	p.Turn("write a file through the shell")
	d := drive(p, wireFixture(t, "codex-app-server-"+codexAppServerWireVersion+"-turn.jsonl")...)

	// THE REPEAT. The deltas and the completed item carry the same text, and a
	// parser reading both would print every message twice. The capture holds 94
	// deltas across two messages, so a regression here is loud. Both messages
	// are checked, because the guard is keyed per ITEM and a per-turn version of
	// it would pass the first and fail the second.
	for _, once := range []string{
		"without PowerShell wrapping", // the commentary message
		"is not recognized as an",     // the final answer
	} {
		if n := strings.Count(d.body, once); n != 1 {
			t.Errorf("%q appears %d times; every message must be read exactly once. body = %q", once, n, d.body)
		}
	}
	// The brief coming back as a `userMessage` item, and the model's `reasoning`
	// items, are both in this capture and neither belongs in the column.
	if strings.Contains(d.body, "operating context") {
		t.Error("the brief was rendered as though the vendor had said it")
	}

	var session, end *runner.Event
	for i := range d.events {
		if d.events[i].Kind == runner.KindSession {
			session = &d.events[i]
		}
		if d.events[i].EndsTurn {
			end = &d.events[i]
		}
	}
	if session == nil || session.SessionID == "" {
		t.Fatal("no thread id came out of the handshake; every follow-up turn would start a new conversation")
	}
	if end == nil {
		t.Fatal("no end event; this process does not exit between turns, so nothing else would end the turn")
	}
	if end.Kind != runner.KindMeta {
		t.Errorf("the turn ended as %v; the capture's turn/completed carries status completed and no error", end.Kind)
	}
	// codex publishes no monetary figure on EITHER surface, so this stays nil
	// forever. Deriving one from the token counts this protocol does carry is on
	// the rejected list.
	if end.CostUSD != nil {
		t.Errorf("cost = %v; this vendor reports token counts and no money", *end.CostUSD)
	}

	// THE SANDBOX DENIAL, as the room would show it. The capture's write came
	// back exit 1 with the vendor's own line, and that is the fact the read
	// posture's whole claim rests on.
	var denied bool
	for _, a := range d.acts {
		if a.Outcome == runner.ActFailed && strings.Contains(a.Detail, "Access is denied") {
			denied = true
		}
	}
	if !denied {
		t.Errorf("the sandbox denial did not reach the trace as a failure; acts = %+v", d.acts)
	}

	// Both figures the `exec --json` stream does not carry at all. Captured and
	// held, rendered nowhere — see the field comments on appServerProtocol.
	if p.usage == nil || p.usage.ModelContextWindow == 0 {
		t.Errorf("thread/tokenUsage/updated stopped carrying a context window: %+v", p.usage)
	}
	if p.limits == nil || p.limits.Primary == nil || p.limits.Primary.WindowDurationMins == 0 {
		t.Errorf("account/rateLimits/updated stopped carrying a quota window: %+v", p.limits)
	}
}
