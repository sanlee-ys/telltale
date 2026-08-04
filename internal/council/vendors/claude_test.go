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

// TestToolCallsCarryTheirArgument. A trace of bare tool names is a half-built
// gauge: six lines reading "Bash" say that something happened six times and
// nothing about what. This is why the trace reads completed assistant messages
// rather than content_block_start, whose input is still empty.
// The line is verbatim from the live probe of 2026-08-04 (Claude Code 2.1.220),
// trimmed only of the usage/timestamp envelope. Note the `id` — it is what a
// tool_result is later matched against, and it is the field this whole feature
// hangs on.
func TestToolCallsCarryTheirArgument(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"toolu_01A9758ERJJ2QGcKSeeDkeA1","name":"Bash","input":{"command":"echo hi","description":"Echo hi"},"caller":{"type":"direct"}}]}}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindActivity {
		t.Fatalf("got (%+v, %v), want a KindActivity", ev, ok)
	}
	if len(ev.Acts) != 1 {
		t.Fatalf("Acts = %+v, want one call", ev.Acts)
	}
	if ev.Acts[0].Text != "Bash: echo hi" {
		t.Errorf("Text = %q, want the command alongside the tool name", ev.Acts[0].Text)
	}
	// Without the id the result arriving later has nothing to attach to, and
	// the trace goes back to showing commands with no outcomes.
	if ev.Acts[0].ID != "toolu_01A9758ERJJ2QGcKSeeDkeA1" {
		t.Errorf("ID = %q, want the tool_use id the result will quote", ev.Acts[0].ID)
	}
	if ev.Acts[0].Outcome != runner.ActPending {
		t.Errorf("Outcome = %v; an announced call has no outcome yet", ev.Acts[0].Outcome)
	}
}

// TestParallelToolBatchIsNotCollapsed: one assistant message really can carry
// several calls, and reporting them as one entry would under-report the work.
func TestParallelToolBatchIsNotCollapsed(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[` +
		`{"type":"tool_use","id":"toolu_a","name":"Read","input":{"file_path":"a.go"}},` +
		`{"type":"tool_use","id":"toolu_b","name":"Grep","input":{"pattern":"func main"}}]}}`)
	ev, _ := Claude{}.ParseEvent(line)
	if len(ev.Acts) != 2 {
		t.Fatalf("Acts = %+v, want two calls", ev.Acts)
	}
	if ev.Acts[0].Text != "Read: a.go" || ev.Acts[1].Text != "Grep: func main" {
		t.Errorf("batch lost a call: %+v", ev.Acts)
	}
	// Distinct ids, or the two results would resolve the same entry and one of
	// the two commands would silently take the other's outcome.
	if ev.Acts[0].ID == ev.Acts[1].ID {
		t.Error("a parallel batch shares one id; the results could not be told apart")
	}
}

// --- Tool RESULTS. Every line below is verbatim live capture. ---

// TestClaudeToolResultsCarryOutcome uses the two lines the live probe returned
// for a batch of one succeeding and one failing Bash call.
//
// The failure is a permission refusal rather than a non-zero exit, which is
// worth keeping visible: `is_error` is the harness's verdict on the CALL, not
// the shell's exit status. "this did not do what was asked" is the whole claim
// it supports, and it is the whole claim the trace makes.
func TestClaudeToolResultsCarryOutcome(t *testing.T) {
	ok := []byte(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01A9758ERJJ2QGcKSeeDkeA1","type":"tool_result","content":"hi","is_error":false}]},"parent_tool_use_id":null,"session_id":"3b60c20f-99ff-4722-9f11-b42ab1149874"}`)
	bad := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ls in '/nonexistent-xyz' was blocked. For security, Claude Code may only list files in the allowed working directories for this session.","is_error":true,"tool_use_id":"toolu_01Uk7Xp2kguFuDtxT4ovaXE5"}]},"session_id":"3b60c20f-99ff-4722-9f11-b42ab1149874"}`)

	ev, got := Claude{}.ParseEvent(ok)
	if !got || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
		t.Fatalf("got (%+v, %v), want one activity", ev, got)
	}
	if ev.Acts[0].Outcome != runner.ActOK {
		t.Errorf("Outcome = %v, want ActOK", ev.Acts[0].Outcome)
	}
	if ev.Acts[0].ID != "toolu_01A9758ERJJ2QGcKSeeDkeA1" {
		t.Errorf("ID = %q, want the id of the call it resolves", ev.Acts[0].ID)
	}
	// A success carries no detail: the trace records what was done, not the
	// output of everything that worked.
	if ev.Acts[0].Detail != "" {
		t.Errorf("Detail = %q; a successful call has nothing to explain", ev.Acts[0].Detail)
	}

	ev, got = Claude{}.ParseEvent(bad)
	if !got || len(ev.Acts) != 1 {
		t.Fatalf("got (%+v, %v), want one activity", ev, got)
	}
	if ev.Acts[0].Outcome != runner.ActFailed {
		t.Errorf("Outcome = %v, want ActFailed", ev.Acts[0].Outcome)
	}
	if !strings.Contains(ev.Acts[0].Detail, "was blocked") {
		t.Errorf("Detail = %q, want the vendor's own first line", ev.Acts[0].Detail)
	}
	// The key ORDER differs between these two captured lines — tool_use_id
	// leads on one and trails on the other — so nothing may be read from
	// position. That is the whole reason this pair is asserted together.
	if !strings.HasPrefix(string(ok), `{"type":"user","message":{"role":"user","content":[{"tool_use_id"`) ||
		!strings.Contains(string(bad), `"is_error":true,"tool_use_id"`) {
		t.Error("the fixtures no longer cover both captured field orders")
	}
}

// TestClaudeSuccessOmitsIsErrorEntirely is the trap in this schema.
//
// Verbatim from the live probe: a successful Read result carries tool_use_id,
// type and content, and NO is_error field at all. A parser that treated an
// absent is_error as "unknown" would render every quiet success as an
// unanswered call, which is the opposite failure to the one this feature fixes.
func TestClaudeSuccessOmitsIsErrorEntirely(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01G6kWADf6TWXXm2tJDc6dZS","type":"tool_result","content":"1\talpha\n2\tbeta\n3\t"}]},"session_id":"e31d5d29-309d-4e91-a041-344197046e92"}`)
	ev, ok := Claude{}.ParseEvent(line)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("got (%+v, %v), want one activity", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActOK {
		t.Errorf("Outcome = %v; Claude Code marks failure and stays silent about success", ev.Acts[0].Outcome)
	}
}

// TestClaudeToolResultContentMayBeABlockArray: the string form is the only one
// captured live, but the schema allows an array of blocks and Claude Code emits
// that for image results. Handled defensively — and when the array carries no
// text, the detail is empty rather than a guess at what it held.
func TestClaudeToolResultContentMayBeABlockArray(t *testing.T) {
	withText := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_z","is_error":true,` +
		`"content":[{"type":"text","text":"exit status 2\nsee above"}]}]}}`)
	ev, _ := Claude{}.ParseEvent(withText)
	if len(ev.Acts) != 1 || ev.Acts[0].Detail != "exit status 2" {
		t.Errorf("Detail = %+v, want the first line of the text block", ev.Acts)
	}

	imageOnly := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_z","is_error":true,` +
		`"content":[{"type":"image","source":{"type":"base64"}}]}]}}`)
	ev, ok := Claude{}.ParseEvent(imageOnly)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("an unreadable result dropped the failure entirely: %+v", ev)
	}
	if ev.Acts[0].Outcome != runner.ActFailed {
		t.Error("the failure was lost because its detail was unreadable")
	}
	if ev.Acts[0].Detail != "" {
		t.Errorf("Detail = %q, want nothing rather than a guess", ev.Acts[0].Detail)
	}
}

// TestClaudeResultWithoutAToolUseIDIsDropped: with no id there is nothing to
// resolve, and appending it as a fresh nameless entry would put a bare mark in
// the trace saying that something unnamed failed.
func TestClaudeResultWithoutAToolUseIDIsDropped(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"orphan","is_error":true}]}}`)
	if ev, ok := (Claude{}).ParseEvent(line); ok {
		t.Errorf("an unattributable result produced %+v", ev)
	}
}

// TestAssistantTextIsNotActivity: a message with no tool_use is the vendor
// speaking, and the streaming text path already carries it. Emitting activity
// here would put an empty step in every trace.
func TestAssistantTextIsNotActivity(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"content":[{"type":"text","text":"hello"}]}}`)
	var c Claude
	if ev, ok := c.ParseEvent(line); ok {
		t.Errorf("a plain assistant message produced %+v", ev)
	}
}

// TestClipArgCutsOnRuneBoundaries. A path with a CJK or accented character
// would otherwise be sliced mid-rune and render as a replacement glyph.
func TestClipArgCutsOnRuneBoundaries(t *testing.T) {
	got := clipArg(strings.Repeat("日", 400))
	for _, r := range got {
		if r == '\uFFFD' {
			t.Fatal("clipArg cut through a rune")
		}
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("a clipped argument is not marked as clipped")
	}
	// Multi-line commands collapse: the consumer splits the event on newlines,
	// so an embedded newline would fabricate extra trace entries.
	if strings.Contains(clipArg("line one\nline two"), "\n") {
		t.Error("a multi-line command would become several phantom trace entries")
	}
}
