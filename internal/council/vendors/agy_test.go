package vendors

import (
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// Every fixture line below is real captured stdout from `agy --output-format
// stream-json` on 2026-08-03, pasted verbatim rather than hand-written from the
// docs. Hand-written fixtures are how a parser ends up passing its tests against
// a schema the vendor does not actually emit.

// TestAgyFlagsMatchTheInstalledCLI pins the flags against `agy --help`.
func TestAgyFlagsMatchTheInstalledCLI(t *testing.T) {
	spec, err := Antigravity{}.FirstTurn("brief", `C:\ws`, `C:\bin\agy.exe`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--output-format", "stream-json", "--mode", "plan", "--sandbox", "--disable-slash-commands", "-p"} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %q in %v", want, spec.Args)
		}
	}
	// Would auto-approve every tool request. Never council's posture.
	if slices.Contains(spec.Args, "--dangerously-skip-permissions") {
		t.Error("--dangerously-skip-permissions auto-approves every tool request")
	}
	if spec.Dir != `C:\ws` {
		t.Errorf("Dir = %q, want the workspace", spec.Dir)
	}
}

// TestPromptIsTheValueOfPrintAndComesLast is the trap this CLI sets, pinned.
//
// -p is a STRING flag whose value IS the prompt, not a boolean. Verified: `agy
// -p --output-format stream-json "<prompt>"` exits 0 having taken the literal
// text "--output-format" as the prompt and ignored everything after it. Nothing
// errors — the turn just answers the wrong question in the wrong format. So -p
// must be last, and the prompt must be the very next element.
func TestPromptIsTheValueOfPrintAndComesLast(t *testing.T) {
	const prompt = `a "quoted" & piped | brief; with $vars`
	for _, spec := range []runner.Spec{
		mustAgyFirst(t, prompt),
		mustAgyNext(t, prompt, "2b18de13-bd04-4804-844e-0f75f2e3461e"),
	} {
		i := slices.Index(spec.Args, "-p")
		if i < 0 {
			t.Fatalf("no -p in %v", spec.Args)
		}
		if i != len(spec.Args)-2 {
			t.Errorf("-p is not second-to-last (%d of %d); a flag after it would be swallowed into the prompt: %v",
				i, len(spec.Args), spec.Args)
		}
		if spec.Args[i+1] != prompt {
			t.Errorf("prompt is not the value of -p: %q", spec.Args[i+1])
		}
	}
}

// TestPromptGoesToArgvNotStdin: the opposite of the Claude adapter, and not by
// choice. `echo x | agy --output-format stream-json -p` fails with "flag needs
// an argument: -p" (exit 2) — agy never reads the prompt from stdin. Asserted so
// that a future "make it consistent with Claude" edit fails here instead of at
// runtime.
func TestPromptGoesToArgvNotStdin(t *testing.T) {
	spec := mustAgyFirst(t, "brief")
	if spec.StdinPrompt != "" {
		t.Errorf("StdinPrompt = %q; agy does not read the prompt from stdin, "+
			"so anything put there is silently discarded and the turn runs promptless", spec.StdinPrompt)
	}
}

// TestAgyNextTurnResumesRatherThanResends. Verified live: a second turn against
// a captured conversation id echoed the same id, reported num_turns 2, and
// answered a question only the first turn's content could answer.
func TestAgyNextTurnResumesRatherThanResends(t *testing.T) {
	spec := mustAgyNext(t, "follow up", "2b18de13-bd04-4804-844e-0f75f2e3461e")
	i := slices.Index(spec.Args, "--conversation")
	if i < 0 || spec.Args[i+1] != "2b18de13-bd04-4804-844e-0f75f2e3461e" {
		t.Fatalf("no --conversation <id> in %v", spec.Args)
	}
	// --conversation must precede -p or the id lands inside the prompt text.
	if i > slices.Index(spec.Args, "-p") {
		t.Errorf("--conversation appears after -p, so it would be part of the prompt: %v", spec.Args)
	}
	// Only the new turn is sent; agy replays its own history.
	if spec.Args[len(spec.Args)-1] != "follow up" {
		t.Errorf("argv carries more than the new turn: %q", spec.Args[len(spec.Args)-1])
	}
}

func TestAgyNextTurnWithoutASessionRefuses(t *testing.T) {
	if _, err := (Antigravity{}).NextTurn("p", "", "agy", ""); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

// TestAgyParseConversationID uses the real init line, trimmed only in its tools
// array (the real one lists 55 tools). The id is TOP-LEVEL on init, unlike on
// every other event, which is the easy field to wire to the wrong place.
func TestAgyParseConversationID(t *testing.T) {
	line := []byte(`{"event":"init","conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","init":{"cwd":"C:\\ws","tools":["view_file","write_to_file","run_command"],"permission_mode":"request-review"}}`)
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindSession || ev.SessionID != "2b18de13-bd04-4804-844e-0f75f2e3461e" {
		t.Fatalf("got (%+v, %v), want a KindSession carrying the conversation id", ev, ok)
	}
}

// TestAgyParseTextFromBothStates: real output puts the reply text on the ACTIVE
// step and the trailing newline on the DONE step of the SAME step_index. They
// are different deltas, not a repeat, so accepting only one state drops half the
// reply and accepting both must not duplicate it.
func TestAgyParseTextFromBothStates(t *testing.T) {
	active := []byte(`{"event":"step_update","step_update":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"OK"}}`)
	done := []byte(`{"event":"step_update","step_update":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"\n","duration_seconds":73.0806477,"usage":{"input_tokens":9929,"output_tokens":31,"thinking_tokens":30,"cache_read_tokens":8141,"total_tokens":9960}}}`)

	var a Antigravity
	var got strings.Builder
	for _, l := range [][]byte{active, done} {
		ev, ok := a.ParseEvent(l)
		if !ok || ev.Kind != runner.KindText {
			t.Fatalf("line dropped or not KindText: %s", l)
		}
		got.WriteString(ev.Text)
	}
	if got.String() != "OK\n" {
		t.Errorf("reassembled %q, want %q", got.String(), "OK\n")
	}
}

// TestAgyToolStepsAreNotAssistantText: tool steps carry command output and file
// parameters. Rendering them into the column would present tool chatter as the
// vendor's answer. This fixture is the real list_permissions step, truncated.
func TestAgyToolStepsAreNotAssistantText(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":3,"state":"ACTIVE","step_type":"tool","tool_name":"list_permissions","tool_info":{"name":"list_permissions"}}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":6,"state":"DONE","step_type":"tool","tool_name":"write_to_file","duration_seconds":0.0604336,"tool_info":{"name":"write_to_file","parameters":{"TargetFile":"C:\\probe.txt"}}}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":4,"state":"DONE","step_type":"checkpoint","duration_seconds":0.4246165}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":0,"state":"DONE","step_type":"user_input"}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":5,"state":"DONE","step_type":"system_message"}}`),
	}
	var a Antigravity
	for _, l := range lines {
		if ev, ok := a.ParseEvent(l); ok {
			t.Errorf("non-assistant step produced %+v: %s", ev, l)
		}
	}
}

// TestAgyParseResultCarriesFinalTextAndNoCost.
//
// The fallback text matters more for this vendor than for Claude: agy streams at
// step granularity, so a column can legitimately be empty until the very end.
//
// The cost must stay nil. agy reports token counts and no monetary figure
// anywhere in its output — deriving dollars from those tokens is the fabricated
// number council exists to refuse.
func TestAgyParseResultCarriesFinalTextAndNoCost(t *testing.T) {
	line := []byte(`{"event":"result","result":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","status":"SUCCESS","response":"OK\n","duration_seconds":73.7446404,"num_turns":1,"usage":{"input_tokens":10026,"output_tokens":35,"thinking_tokens":30,"cache_read_tokens":8141,"total_tokens":10061}}}`)
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("got (%+v, %v), want KindMeta", ev, ok)
	}
	if ev.Text != "OK\n" {
		t.Errorf("Text = %q, want the final response kept as a fallback", ev.Text)
	}
	if ev.SessionID != "2b18de13-bd04-4804-844e-0f75f2e3461e" {
		t.Errorf("SessionID = %q, want the nested conversation id", ev.SessionID)
	}
	if ev.CostUSD != nil {
		t.Errorf("CostUSD = %v; agy reports tokens and never a cost, so this must stay nil", *ev.CostUSD)
	}
}

// TestAgyNonSuccessStatusIsAnError, and its counterpart: an ABSENT status is not
// a failure. Only "SUCCESS" was ever observed live, so the failure path is
// written as "known-and-not-SUCCESS" rather than as a guessed enum. Reporting a
// turn as failed on a field never seen to fail would be inventing a diagnosis.
func TestAgyNonSuccessStatusIsAnError(t *testing.T) {
	ev, ok := Antigravity{}.ParseEvent([]byte(`{"event":"result","result":{"conversation_id":"c","status":"ERROR","response":"quota exhausted"}}`))
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%+v, %v), want KindError", ev, ok)
	}
	if ev.Note != "quota exhausted" {
		t.Errorf("Note = %q, want the vendor's own words", ev.Note)
	}

	ev, ok = Antigravity{}.ParseEvent([]byte(`{"event":"result","result":{"conversation_id":"c","response":"done"}}`))
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("a result with no status became %+v; absent status is unknown, not failed", ev)
	}
}

// TestAgyUnknownEventsAreIgnoredNotFatal. agy's discriminator is "event", not
// Claude's "type", so a Claude-shaped line must fall through rather than being
// half-parsed by a shared field name.
func TestAgyUnknownEventsAreIgnoredNotFatal(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"some_future_thing","payload":{"a":1}}`),
		[]byte(`{"event":"init"}`), // no conversation id
		[]byte(`{"event":"step_update","step_update":{"step_index":1,"state":"DONE","step_type":"unknown","duration_seconds":0.0017754}}`),
		[]byte(`{"event":"step_update","step_update":{"state":"ACTIVE","step_type":"agent_response"}}`), // no text
		[]byte(`{"type":"stream_event","event":"not-agy-shaped"}`),
		[]byte(`not json at all`),
		[]byte(``),
		[]byte(`   `),
	}
	var a Antigravity
	for _, l := range lines {
		if ev, ok := a.ParseEvent(l); ok {
			t.Errorf("line produced %+v when it should have been ignored: %s", ev, l)
		}
	}
}

// TestAgyParserSurvivesATruncatedStream: a cancelled turn cuts the pipe
// mid-line, so half a JSON object is normal, not a crash.
func TestAgyParserSurvivesATruncatedStream(t *testing.T) {
	var a Antigravity
	partial := []byte(`{"event":"step_update","step_update":{"conversation_id":"2b18de13","state":"ACTIVE","step_type":"agent_res`)
	if _, ok := a.ParseEvent(partial); ok {
		t.Error("a truncated line produced an event")
	}
}

func mustAgyFirst(t *testing.T, prompt string) runner.Spec {
	t.Helper()
	s, err := Antigravity{}.FirstTurn(prompt, `C:\ws`, "agy")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustAgyNext(t *testing.T, prompt, sess string) runner.Spec {
	t.Helper()
	s, err := Antigravity{}.NextTurn(prompt, `C:\ws`, "agy", sess)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
