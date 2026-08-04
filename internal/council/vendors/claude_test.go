package vendors

import (
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestFlagsMatchTheInstalledCLI pins the flag names against the binary's own
// --help, verified rather than remembered.
//
// This test exists because the design sketch for this adapter specified
// `--tools`, which does not exist — the flag is `--allowedTools`. A wrong flag
// would not degrade gracefully: Claude Code would reject the invocation and the
// column would fail on every turn for a reason no card could explain.
func TestFlagsMatchTheInstalledCLI(t *testing.T) {
	spec, err := Claude{}.FirstTurn("brief", `C:\ws`, `C:\bin\claude.exe`, PostureRead)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"-p", "--output-format", "stream-json", "--verbose", "--include-partial-messages", "--disallowedTools", "--strict-mcp-config"} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %q in %v", want, spec.Args)
		}
	}
	// The flag that does not exist. If it ever reappears, the column silently
	// stops working.
	if slices.Contains(spec.Args, "--tools") {
		t.Error("--tools is not a Claude Code flag")
	}
	// The flag that exists but does NOT do what its name suggests. Verified
	// against the live CLI: with --allowedTools Read,Glob,Grep the session's own
	// init event still listed Edit, Write and Bash. It pre-approves; it does not
	// restrict. Using it as the enforcement mechanism would put a read-only
	// badge on a session that can write.
	if slices.Contains(spec.Args, "--allowedTools") {
		t.Error("--allowedTools pre-approves tools, it does not remove them; the enforcement flag is --disallowedTools")
	}
	// --bare would drop subscription OAuth and demand an API key.
	if slices.Contains(spec.Args, "--bare") {
		t.Error("--bare bypasses subscription auth and would break the column")
	}
}

// TestEveryWriteOrExecToolIsDenied is the sandbox claim, pinned. The badge says
// these tools are absent from the session; if one drops off this list, the
// badge on screen becomes a false statement.
func TestEveryWriteOrExecToolIsDenied(t *testing.T) {
	spec, _ := Claude{}.FirstTurn("brief", "", "claude", PostureRead)
	i := slices.Index(spec.Args, "--disallowedTools")
	if i < 0 || i+1 >= len(spec.Args) {
		t.Fatal("no --disallowedTools value")
	}
	denied := strings.Split(spec.Args[i+1], ",")

	// PowerShell is on this list for a reason worth keeping in front of the
	// next reader: denying only Bash leaves a working shell on Windows, which
	// is the platform this product targets.
	for _, want := range []string{"Edit", "Write", "NotebookEdit", "Bash", "PowerShell", "Task", "WebFetch"} {
		if !slices.Contains(denied, want) {
			t.Errorf("%s is not denied; the ro:tools badge would be a false claim", want)
		}
	}
}

// TestMCPServersAreDropped: no fixed deny list can name the tools a user's own
// MCP servers expose. The verification run surfaced Gmail write tools in a
// session that had every built-in write tool denied.
func TestMCPServersAreDropped(t *testing.T) {
	spec, _ := Claude{}.FirstTurn("brief", "", "claude", PostureRead)
	if !slices.Contains(spec.Args, "--strict-mcp-config") {
		t.Error("without --strict-mcp-config the session inherits the user's MCP servers, " +
			"whose tools the deny list cannot know about")
	}
}

// TestPromptNeverEntersArgv: a brief is arbitrary text, and argv is where
// arbitrary text becomes a quoting problem.
func TestPromptNeverEntersArgv(t *testing.T) {
	prompt := `a "quoted" & piped | brief; with $vars`
	for _, spec := range []runner.Spec{
		mustFirst(t, prompt),
		mustNext(t, prompt, "sess-123"),
	} {
		if spec.StdinPrompt != prompt {
			t.Errorf("prompt did not go to stdin: %q", spec.StdinPrompt)
		}
		for _, a := range spec.Args {
			if strings.Contains(a, "brief") {
				t.Errorf("prompt text leaked into argv: %q", a)
			}
		}
	}
}

func TestNextTurnResumesRatherThanResends(t *testing.T) {
	spec := mustNext(t, "follow up", "sess-abc")
	i := slices.Index(spec.Args, "--resume")
	if i < 0 || spec.Args[i+1] != "sess-abc" {
		t.Fatalf("no --resume sess-abc in %v", spec.Args)
	}
	// Only the new turn is sent. If the transcript were being re-sent, the
	// prompt would carry prior turns and this would be much longer.
	if spec.StdinPrompt != "follow up" {
		t.Errorf("stdin carries more than the new turn: %q", spec.StdinPrompt)
	}
}

func TestNextTurnWithoutASessionRefuses(t *testing.T) {
	if _, err := (Claude{}).NextTurn("p", "", "claude", "", PostureRead); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

func TestParseTextDeltas(t *testing.T) {
	line := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"Hello"}}}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindText || ev.Text != "Hello" {
		t.Fatalf("got (%v, %v), want a KindText carrying Hello", ev, ok)
	}
}

func TestParseSessionID(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"init","session_id":"abc-123","model":"claude-opus-5"}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindSession || ev.SessionID != "abc-123" {
		t.Fatalf("got (%v, %v), want a KindSession carrying abc-123", ev, ok)
	}
}

// TestParseResultCarriesReportedCostOnly: the cost is a pointer so "reported
// zero" and "reported nothing" stay distinguishable. Council never derives a
// cost from token counts — that is on the deliberately-rejected list.
func TestParseResultCarriesReportedCostOnly(t *testing.T) {
	ev, ok := Claude{}.ParseEvent([]byte(`{"type":"result","session_id":"s","result":"done","total_cost_usd":0.0123}`))
	if !ok || ev.CostUSD == nil || *ev.CostUSD != 0.0123 {
		t.Fatalf("cost not carried: %+v", ev)
	}

	ev, ok = Claude{}.ParseEvent([]byte(`{"type":"result","session_id":"s","result":"done"}`))
	if !ok {
		t.Fatal("result without a cost was dropped")
	}
	if ev.CostUSD != nil {
		t.Errorf("a vendor that reported no cost produced %v; absent must stay absent", *ev.CostUSD)
	}
}

// TestResultCarriesFinalTextAsFallback: the consumer uses this only when the
// turn streamed nothing, so a build that does not emit partials still shows a
// reply instead of an empty column.
func TestResultCarriesFinalTextAsFallback(t *testing.T) {
	ev, _ := Claude{}.ParseEvent([]byte(`{"type":"result","result":"the whole answer"}`))
	if ev.Text != "the whole answer" {
		t.Errorf("Text = %q, want the final result kept as a fallback", ev.Text)
	}
}

func TestVendorReportedErrorIsAnError(t *testing.T) {
	ev, ok := Claude{}.ParseEvent([]byte(`{"type":"result","is_error":true,"result":"context limit reached"}`))
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%v, %v), want KindError", ev, ok)
	}
	if ev.Note != "context limit reached" {
		t.Errorf("Note = %q, want the vendor's own words", ev.Note)
	}
}

// TestUnknownEventsAreIgnoredNotFatal: the stream-json schema carries far more
// than a comparison room needs, and a parser that failed on an unrecognised
// event would turn every upstream addition into a broken column.
func TestUnknownEventsAreIgnoredNotFatal(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"assistant","message":{"content":[]}}`),
		[]byte(`{"type":"user","message":{}}`),
		[]byte(`{"type":"stream_event","event":{"type":"ping"}}`),
		[]byte(`{"type":"some_future_thing","payload":{"a":1}}`),
		[]byte(`not json at all`),
		[]byte(``),
		[]byte(`{"type":"system"}`), // no session id
	}
	var c Claude
	for _, l := range lines {
		if _, ok := c.ParseEvent(l); ok {
			t.Errorf("line produced an event it should have ignored: %s", l)
		}
	}
}

// TestParserSurvivesATruncatedStream: a cancelled turn cuts the pipe mid-line,
// so half a JSON object is a normal thing to see, not a crash.
func TestParserSurvivesATruncatedStream(t *testing.T) {
	var c Claude
	partial := []byte(`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_de`)
	if _, ok := c.ParseEvent(partial); ok {
		t.Error("a truncated line produced an event")
	}
}

func mustFirst(t *testing.T, prompt string) runner.Spec {
	t.Helper()
	s, err := Claude{}.FirstTurn(prompt, `C:\ws`, "claude", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustNext(t *testing.T, prompt, sess string) runner.Spec {
	t.Helper()
	s, err := Claude{}.NextTurn(prompt, `C:\ws`, "claude", sess, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
