package vendors

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// claudeIsPersistent pins the seat's capability at compile time. If Claude ever
// stops satisfying the interface, the room silently falls back to a spawn per
// turn and the only symptom is the bill.
var _ Persistent = Claude{}

// TestSessionInvocationMatchesTheVerifiedSpike.
//
// Every flag here was in the argv of the run that actually worked, and the one
// that makes the difference is --input-format: without it the process reads a
// single prompt and exits, which is exactly the behaviour being replaced.
func TestSessionInvocationMatchesTheVerifiedSpike(t *testing.T) {
	spec, err := Claude{}.Session(`C:\ws`, `C:\bin\claude.exe`, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"-p", "--input-format", "stream-json", "--output-format", "--verbose",
		"--strict-mcp-config",
	} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %q in %v", want, spec.Args)
		}
	}
	if i := slices.Index(spec.Args, "--input-format"); i < 0 || spec.Args[i+1] != "stream-json" {
		t.Errorf("--input-format value = %v, want stream-json", spec.Args)
	}
	// There is no session to resume: it never ended.
	if slices.Contains(spec.Args, "--resume") {
		t.Error("a persistent session must not also try to resume one")
	}
	// The prompt arrives later, on stdin, as a message. Nothing about it may be
	// resolved at launch.
	if spec.StdinPrompt != "" {
		t.Errorf("StdinPrompt = %q, want empty: turns are sent as messages", spec.StdinPrompt)
	}
	if spec.Dir != `C:\ws` {
		t.Errorf("Dir = %q, want the workspace", spec.Dir)
	}
}

// TestSessionKeepsThePostureFlags: keeping one process alive must not widen what
// it may do. The read posture's deny list and the MCP drop are the claim the
// badge makes, and they are per-invocation — so a persistent invocation that
// left them off would make the badge false for the whole room, permanently.
func TestSessionKeepsThePostureFlags(t *testing.T) {
	read, _ := Claude{}.Session("", "claude", PostureRead)
	if !slices.Contains(read.Args, "--disallowedTools") {
		t.Error("the read posture lost its deny list when it became persistent")
	}
	if !slices.Contains(read.Args, "--strict-mcp-config") {
		t.Error("the read posture lost --strict-mcp-config when it became persistent")
	}

	write, _ := Claude{}.Session("", "claude", PostureWrite)
	if slices.Contains(write.Args, "--disallowedTools") {
		t.Error("the write posture kept the deny list")
	}
	// Kept in BOTH postures. Write mode widens what a vendor may do inside the
	// directory council was pointed at; MCP servers reach outside it.
	if !slices.Contains(write.Args, "--strict-mcp-config") {
		t.Error("--strict-mcp-config must survive write posture: MCP reaches outside the workspace")
	}
}

// TestTurnEnvelopeMatchesTheCapturedLine.
//
// The line below is what the working probe sent, byte for byte after
// marshalling. It is asserted structurally rather than as a string so a key
// reordering by encoding/json does not fail the test for the wrong reason — but
// the field names and nesting are exact, and they are what the CLI parses.
func TestTurnEnvelopeMatchesTheCapturedLine(t *testing.T) {
	line, err := Claude{}.Turn(`a brief with "quotes" & an ampersand`)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &got); err != nil {
		t.Fatalf("the turn is not valid JSON: %v", err)
	}
	if got.Type != "user" {
		t.Errorf("type = %q, want user", got.Type)
	}
	if got.Message.Role != "user" {
		t.Errorf("message.role = %q, want user", got.Message.Role)
	}
	if got.Message.Content != `a brief with "quotes" & an ampersand` {
		t.Errorf("content = %q, want the prompt intact", got.Message.Content)
	}
	// One line, always. The stream is JSONL: an embedded newline would be read
	// as the end of the message and the rest as a second, malformed one.
	for _, b := range line {
		if b == '\n' {
			t.Fatal("the encoded turn contains a raw newline; JSONL would split it")
		}
	}
}

// TestGateRequestIsParsedFromTheCapturedLine.
//
// VERBATIM from the live run, trimmed of nothing. This is the message that
// blocks the vendor, and every field the room needs is read from it by name.
func TestGateRequestIsParsedFromTheCapturedLine(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"179ce36e-c5d1-4b95-a761-ec7aa1fd5494","request":{"subtype":"can_use_tool","tool_name":"Write","display_name":"Write","input":{"file_path":"C:\\ws\\ping.txt","content":"PONG"},"description":"ping.txt","permission_suggestions":[{"type":"setMode","mode":"acceptEdits","destination":"session"}],"tool_use_id":"toolu_01MagQh7Ep8kzC1edrDr17jL"}}`)

	ev, ok := Claude{}.ParseEvent(line)
	if !ok {
		t.Fatal("the gate request was dropped; the vendor would block forever")
	}
	if ev.Kind != runner.KindGate || ev.Gate == nil {
		t.Fatalf("kind = %v gate = %v, want KindGate with a gate", ev.Kind, ev.Gate)
	}
	if ev.Gate.RequestID != "179ce36e-c5d1-4b95-a761-ec7aa1fd5494" {
		t.Errorf("request id = %q; the answer would go to nobody", ev.Gate.RequestID)
	}
	// The id that ties the card to the trace entry it decides.
	if ev.Gate.ToolUseID != "toolu_01MagQh7Ep8kzC1edrDr17jL" {
		t.Errorf("tool_use_id = %q, want the id the activity trace keys on", ev.Gate.ToolUseID)
	}
	if ev.Gate.Tool != "Write" {
		t.Errorf("tool = %q, want Write", ev.Gate.Tool)
	}
	// Same formatting as the trace: the card and the entry are the same call.
	if ev.Gate.Text != `Write: C:\ws\ping.txt` {
		t.Errorf("text = %q, want the tool and its argument line", ev.Gate.Text)
	}
}

// TestBashGateShowsTheCommand: for a shell call the argument that identifies the
// action is the command, and it is the thing a person is actually deciding on.
// Captured from the run where the gate fired on `mkdir zzz`.
func TestBashGateShowsTheCommand(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"04d99bca-1735-495b-b604-2edaf346465f","request":{"subtype":"can_use_tool","tool_name":"Bash","display_name":"Bash","input":{"command":"mkdir zzz","description":"Create directory zzz"},"description":"Create directory zzz","tool_use_id":"toolu_01J9PxtA1Y4k7Qq6P4AjgHkv"}}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Gate == nil {
		t.Fatal("the Bash gate request was dropped")
	}
	if ev.Gate.Text != "Bash: mkdir zzz" {
		t.Errorf("text = %q, want the command", ev.Gate.Text)
	}
}

// TestUnknownControlSubtypeIsNotAnswered. The same envelope carries other
// requests. Turning one this parser does not understand into a gate would put a
// card on screen asking the user to approve something council cannot describe.
func TestUnknownControlSubtypeIsNotAnswered(t *testing.T) {
	line := []byte(`{"type":"control_request","request_id":"x","request":{"subtype":"request_user_dialog","title":"pick one"}}`)
	ev, ok := Claude{}.ParseEvent(line)
	if ok {
		t.Errorf("an unmodelled control subtype produced %v; it must be left alone", ev.Kind)
	}
}

// TestControlResponseIsIgnored: the vendor's answer to OUR interrupt uses the
// same channel. Reading it as anything would double-count the turn.
func TestControlResponseIsIgnored(t *testing.T) {
	line := []byte(`{"type":"control_response","response":{"subtype":"success","request_id":"int-1","response":{"still_queued":[]}}}`)
	_, ok := Claude{}.ParseEvent(line)
	if ok {
		t.Error("a control_response was turned into an event")
	}
}

// TestDecideMatchesTheCapturedAnswers.
//
// Both branches were sent live and both were honoured — the allow ran the tool
// and put the file on disk, the deny came back as an is_error tool_result
// carrying this exact message and the file was never created.
func TestDecideMatchesTheCapturedAnswers(t *testing.T) {
	type answer struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Response  struct {
				Behavior     string         `json:"behavior"`
				Message      string         `json:"message"`
				UpdatedInput map[string]any `json:"updatedInput"`
			} `json:"response"`
		} `json:"response"`
	}

	in := map[string]any{"file_path": `C:\ws\ping.txt`, "content": "PONG"}
	raw, err := Claude{}.Decide("req-1", true, "", in)
	if err != nil {
		t.Fatal(err)
	}
	var ok answer
	if err := json.Unmarshal(raw, &ok); err != nil {
		t.Fatal(err)
	}
	if ok.Type != "control_response" {
		t.Errorf("type = %q, want control_response", ok.Type)
	}
	// "success" reports that the ANSWER was well formed, not what it decided.
	// Reading it as the decision would approve every denial.
	if ok.Response.Subtype != "success" {
		t.Errorf("subtype = %q, want success on both branches", ok.Response.Subtype)
	}
	if ok.Response.RequestID != "req-1" {
		t.Errorf("request_id = %q, want it echoed back", ok.Response.RequestID)
	}
	if ok.Response.Response.Behavior != "allow" {
		t.Errorf("behavior = %q, want allow", ok.Response.Response.Behavior)
	}
	// Required on an allow: the vendor re-reads the input from the answer.
	if ok.Response.Response.UpdatedInput["content"] != "PONG" {
		t.Errorf("updatedInput = %v, want the tool input carried back", ok.Response.Response.UpdatedInput)
	}

	raw, err = Claude{}.Decide("req-2", false, "denied by you", in)
	if err != nil {
		t.Fatal(err)
	}
	var no answer
	if err := json.Unmarshal(raw, &no); err != nil {
		t.Fatal(err)
	}
	if no.Response.Response.Behavior != "deny" {
		t.Errorf("behavior = %q, want deny", no.Response.Response.Behavior)
	}
	if no.Response.Response.Message != "denied by you" {
		t.Errorf("message = %q, want the reason the model reads back", no.Response.Response.Message)
	}
	// A deny carries no updatedInput. The two branches do not share a shape, and
	// the CLI carries a diagnostic for a handler that confuses them.
	if no.Response.Response.UpdatedInput != nil {
		t.Errorf("a denial carried updatedInput %v", no.Response.Response.UpdatedInput)
	}
}

// TestInterruptMatchesTheCapturedRequest. Sent while the vendor was blocked on a
// permission request; it ended the turn and left the process answering.
func TestInterruptMatchesTheCapturedRequest(t *testing.T) {
	raw, err := Claude{}.Interrupt("telltale-interrupt-1")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		Request   struct {
			Subtype string `json:"subtype"`
		} `json:"request"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Type != "control_request" || got.Request.Subtype != "interrupt" {
		t.Errorf("interrupt = %+v, want a control_request with subtype interrupt", got)
	}
	if got.RequestID != "telltale-interrupt-1" {
		t.Errorf("request_id = %q, want the id the caller can recognise", got.RequestID)
	}
}

// TestResultEndsTheTurn. On a persistent process this line is the ONLY signal
// that a turn is over — there is no exit to infer it from — so a parser that
// dropped the flag would leave the column spinning forever.
func TestResultEndsTheTurn(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"ONE","session_id":"03d6ed06-b774-4b0e-bd56-933bb7d71a44","total_cost_usd":0.1061493}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok {
		t.Fatal("the result line was dropped")
	}
	if !ev.EndsTurn {
		t.Error("result did not end the turn; a persistent column would never finish")
	}
}

// TestVendorReportedFailureStillEndsTheTurn. An interrupted turn comes back as
// a result with is_error true, so the failure branch has to carry the same
// signal — otherwise cancelling a turn would hang the column it cancelled.
func TestVendorReportedFailureStillEndsTheTurn(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"error_during_execution","is_error":true,"stop_reason":"tool_use","terminal_reason":"aborted_tools","session_id":"5a306d4b"}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok {
		t.Fatal("the failing result line was dropped")
	}
	if ev.Kind != runner.KindError {
		t.Errorf("kind = %v, want KindError", ev.Kind)
	}
	if !ev.EndsTurn {
		t.Error("a failed turn did not end the turn; the column would hang on a cancel")
	}
}
