package vendors

import (
	"context"
	"slices"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestCursorFlagsMatchTheInstalledCLI pins the first-turn flags against
// cursor-agent 2026.07.23-e383d2b's own --help rather than against memory.
//
// Unlike the other adapters in this package, these flags were NOT confirmed by
// a run: the installed CLI reports "Not logged in" and checks authentication
// before it parses flags, so every probe returned the same auth error. --help
// and the shipped bundle are the evidence; the comments in cursor.go say which
// is which, and nothing here claims more than that.
func TestCursorFlagsMatchTheInstalledCLI(t *testing.T) {
	spec := mustCursorFirst(t, "brief")

	for _, want := range []string{"-p", "--output-format", "stream-json", "--mode", "plan"} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %q in %v", want, spec.Args)
		}
	}
	// --stream-partial-output is rejected by the CLI unless the format is
	// stream-json, so the pair travels together or not at all.
	if slices.Contains(spec.Args, "--stream-partial-output") &&
		!slices.Contains(spec.Args, "stream-json") {
		t.Error("--stream-partial-output without stream-json; the CLI rejects that combination")
	}
}

// TestCursorNeverPassesTheSkipPermissionsFlags is the safety rule for this
// vendor, in both postures.
//
// -f/--force and --yolo are cursor-agent's "run everything" flags, --trust
// accepts a workspace trust prompt on the user's behalf, and --approve-mcps
// auto-approves servers that reach OUTSIDE the directory council was pointed
// at. --write widens the workspace; it does not pre-approve whatever a model
// decides to try, and it does not consent to anything on the user's behalf.
func TestCursorNeverPassesTheSkipPermissionsFlags(t *testing.T) {
	for _, p := range []Posture{PostureRead, PostureWrite} {
		specs := []runner.Spec{
			mustCursorFirstPosture(t, "brief", p),
			mustCursorNextPosture(t, "brief", "sess-1", p),
		}
		for _, spec := range specs {
			for _, banned := range []string{"-f", "--force", "--yolo", "--trust", "--approve-mcps"} {
				if slices.Contains(spec.Args, banned) {
					t.Errorf("posture %v passes %q; that is a consent decision this adapter does not get to make", p, banned)
				}
			}
		}
	}
}

// TestCursorWritePostureDropsTheReadOnlyRequests: --write has to actually widen
// the vendor rather than merely change a badge.
func TestCursorWritePostureDropsTheReadOnlyRequests(t *testing.T) {
	read := mustCursorFirstPosture(t, "brief", PostureRead)
	if !slices.Contains(read.Args, "plan") || !slices.Contains(read.Args, "--sandbox") {
		t.Errorf("read posture asks for nothing: %v", read.Args)
	}

	write := mustCursorFirstPosture(t, "brief", PostureWrite)
	if slices.Contains(write.Args, "plan") || slices.Contains(write.Args, "--sandbox") {
		t.Errorf("write posture kept the read-only requests: %v", write.Args)
	}
	// Dropping the flags rather than passing --sandbox disabled: council is
	// declining to ask for a restriction, not overriding a user's own config to
	// remove one.
	if slices.Contains(write.Args, "disabled") {
		t.Error("write posture overrides the user's sandbox config instead of leaving it alone")
	}
}

// TestCursorPromptIsTheFinalArgument. The prompt is cursor-agent's variadic
// positional — print mode's own guard is "No prompt provided for print mode"
// against the joined argv — so anything appended after it would be swallowed
// into the prompt text, and any flag placed after it would be read as prose.
func TestCursorPromptIsTheFinalArgument(t *testing.T) {
	for _, spec := range []runner.Spec{
		mustCursorFirst(t, "the brief"),
		mustCursorNext(t, "the brief", "sess-1"),
	} {
		if got := spec.Args[len(spec.Args)-1]; got != "the brief" {
			t.Errorf("last arg = %q, want the prompt, in %v", got, spec.Args)
		}
	}
}

// TestCursorPromptGoesInArgvAndSaysSo is the honest inverse of Codex's rule.
//
// This CLI has no stdin path for the prompt: there is no `-` sentinel, no
// --prompt-file, and no code in the shipped bundle that reads stdin for it.
// StdinPrompt must therefore stay EMPTY — which is precisely what makes
// runner.ErrShellShimWithArgvPrompt refuse the Windows .cmd, and why detection
// marks that seat unusable instead of letting the refusal arrive as a mystery
// failed turn.
func TestCursorPromptGoesInArgvAndSaysSo(t *testing.T) {
	spec := mustCursorFirst(t, "brief")
	if spec.StdinPrompt != "" {
		t.Error("the adapter claims a stdin path this CLI does not have; a shim would then be driven through cmd.exe")
	}
	if !slices.Contains(spec.Args, "brief") {
		t.Errorf("the prompt is in neither channel: %v", spec.Args)
	}
}

// TestCursorShimIsRefusedByTheRunner: the belt-and-braces check that the two
// halves of this change agree. Detection marks the Windows seat unusable; if
// that ever regressed, the runner is the backstop that still refuses.
func TestCursorShimIsRefusedByTheRunner(t *testing.T) {
	spec, err := Cursor{}.FirstTurn("brief", `C:\ws`, `C:\Users\dev\AppData\Local\cursor-agent\cursor-agent.cmd`, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Start(context.Background(), spec, make(chan runner.Event, 1), func([]byte) (runner.Event, bool) {
		return runner.Event{}, false
	}); err != runner.ErrShellShimWithArgvPrompt {
		t.Fatalf("runner accepted an argv prompt on a .cmd: err = %v", err)
	}
}

func TestCursorNextTurnResumesRatherThanResends(t *testing.T) {
	spec := mustCursorNext(t, "follow up", "0198c0de-1234-4321-8888-abcdefabcdef")
	i := slices.Index(spec.Args, "--resume")
	if i < 0 || i+1 >= len(spec.Args) {
		t.Fatalf("no --resume in %v", spec.Args)
	}
	// Unlike codex's positional session id, cursor-agent's is the VALUE of
	// --resume.
	if spec.Args[i+1] != "0198c0de-1234-4321-8888-abcdefabcdef" {
		t.Errorf("session id does not follow --resume in %v", spec.Args)
	}
	// Only the new turn is sent; the vendor replays its own history.
	if got := spec.Args[len(spec.Args)-1]; got != "follow up" {
		t.Errorf("resume carries more than the new turn: %q", got)
	}
}

func TestCursorNextTurnWithoutASessionRefuses(t *testing.T) {
	if _, err := (Cursor{}).NextTurn("p", "", "cursor-agent", "", PostureRead); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

// TestCursorOmitsWorkspaceWhenThereIsNone: --workspace "" would make the CLI
// treat the empty string as a path or a saved workspace name rather than
// defaulting to the working directory.
func TestCursorOmitsWorkspaceWhenThereIsNone(t *testing.T) {
	spec, err := Cursor{}.FirstTurn("brief", "", "cursor-agent", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(spec.Args, "--workspace") {
		t.Errorf("--workspace passed with no workspace: %v", spec.Args)
	}
	if spec.Args[len(spec.Args)-1] != "brief" {
		t.Errorf("prompt lost its final position when the workspace was empty: %v", spec.Args)
	}
}

// --- Parser tests, over lines built from the shipped bundle's own emit calls. ---
//
// These are NOT captured from a run — no run is possible on an unauthenticated
// CLI — and that is stated here rather than left for a reader to assume from the
// resemblance to codex_test.go. Each line below is the object cursor-agent's
// bundle literally constructs and JSON-stringifies to stdout in print mode.

func TestCursorParseInitCarriesTheSessionID(t *testing.T) {
	line := []byte(`{"type":"system","subtype":"init","apiKeySource":"login","cwd":"C:\\ws","session_id":"0198c0de-1234-4321-8888-abcdefabcdef","model":"Sonnet 4","permissionMode":"default"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindSession {
		t.Fatalf("got (%v, %v), want a KindSession", ev, ok)
	}
	if ev.SessionID != "0198c0de-1234-4321-8888-abcdefabcdef" {
		t.Errorf("SessionID = %q", ev.SessionID)
	}
}

func TestCursorParseAssistantText(t *testing.T) {
	line := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]},"session_id":"s1","timestamp_ms":1}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindText {
		t.Fatalf("got (%v, %v), want a KindText", ev, ok)
	}
	if ev.Text != "OK" {
		t.Errorf("Text = %q", ev.Text)
	}
}

// TestCursorDoesNotRenderTheEchoedPrompt is the trap in this schema: print mode
// emits council's OWN brief back as a user event on the same stream. Rendering
// it would put the user's words into the column as though the vendor had said
// them.
func TestCursorDoesNotRenderTheEchoedPrompt(t *testing.T) {
	line := []byte(`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"Reply with exactly: OK"}]},"session_id":"s1"}`)
	if ev, ok := (Cursor{}).ParseEvent(line); ok {
		t.Errorf("the echoed prompt produced a %v event carrying %q", ev.Kind, ev.Text)
	}
}

// TestCursorToolActivityIsNotRenderedAsSpeech. `tool.case` is the discriminator
// the bundle itself reads — it tests "shellToolCall" === e.tool.case — so the
// field name is evidence, not a guess.
func TestCursorToolActivityIsNotRenderedAsSpeech(t *testing.T) {
	line := []byte(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"tool":{"case":"shellToolCall"}},"session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindActivity {
		t.Fatalf("got (%v, %v), want a KindActivity", ev, ok)
	}
	if ev.Text != "shellToolCall" {
		t.Errorf("Text = %q, want the tool discriminator", ev.Text)
	}
	// "completed" is the matching half of the same call. Emitting both would
	// double every line of the trace.
	done := []byte(`{"type":"tool_call","subtype":"completed","call_id":"c1","tool_call":{"tool":{"case":"shellToolCall"}},"session_id":"s1"}`)
	if _, ok := (Cursor{}).ParseEvent(done); ok {
		t.Error("the completed half of a tool call produced a second activity line")
	}
}

// TestCursorUnparsedToolCallStillCountsAsActivity: a column that went quiet
// during the part of the turn it was busiest reads as hung. An unrecognised
// shape is still a thing that happened.
func TestCursorUnparsedToolCallStillCountsAsActivity(t *testing.T) {
	line := []byte(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"something":"else"},"session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindActivity {
		t.Fatalf("got (%v, %v), want a KindActivity", ev, ok)
	}
	if ev.Text == "" {
		t.Error("an activity line with no text renders as a blank step")
	}
}

// TestCursorResultCarriesNoCost is the honest-numbers rule, pinned. The usage
// object carries token counts and no monetary figure anywhere in the bundle, so
// CostUSD must stay nil rather than being derived from tokens.
func TestCursorResultCarriesNoCost(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"success","duration_ms":1200,"duration_api_ms":1200,"is_error":false,"result":"OK","session_id":"s1","request_id":"r1","usage":{"inputTokens":10,"outputTokens":2}}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("got (%v, %v), want a KindMeta", ev, ok)
	}
	if ev.CostUSD != nil {
		t.Errorf("CostUSD = %v; cursor-agent reports no cost, so absent must stay absent", *ev.CostUSD)
	}
	// The whole reply, carried as the fallback for a turn that streamed nothing
	// — which, given this vendor's unestablished granularity, may be every turn.
	if ev.Text != "OK" {
		t.Errorf("Text = %q, want the final result as a fallback", ev.Text)
	}
}

// TestCursorErrorResultIsNotRenderedAsAnAnswer. No failure-emitting result path
// was found in the bundle, which suggests failures ride stderr and the exit
// code. is_error is parsed anyway, because the cost of being wrong in the other
// direction is showing an error message as the vendor's opinion.
func TestCursorErrorResultIsNotRenderedAsAnAnswer(t *testing.T) {
	line := []byte(`{"type":"result","subtype":"error","is_error":true,"result":"the model refused","session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%v, %v), want a KindError", ev, ok)
	}
	if ev.Note != "the model refused" {
		t.Errorf("Note = %q", ev.Note)
	}
}

// TestCursorUnknownEventsAreIgnoredNotFatal: upstream will add event types, and
// a parser that failed on an unrecognised one would turn every cursor-agent
// release into a broken column.
func TestCursorUnknownEventsAreIgnoredNotFatal(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"thinking","subtype":"delta","text":"hmm","session_id":"s1"}`),
		[]byte(`{"type":"system","subtype":"task_notification","task_id":"t1","status":"running","title":"x","session_id":"s1"}`),
		[]byte(`{"type":"some.future.thing","payload":{"a":1}}`),
		[]byte(`{"type":"system","subtype":"init"}`), // no session id
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":""}]}}`),
		[]byte(`not json at all`),
		[]byte(``),
		[]byte(`Error: Authentication required. Please run 'agent login' first.`), // the real stderr-shaped line
	}
	var c Cursor
	for _, l := range lines {
		if ev, ok := c.ParseEvent(l); ok {
			t.Errorf("line produced an event it should have ignored: %s -> %v", l, ev.Kind)
		}
	}
}

// TestCursorParserSurvivesATruncatedStream: a cancelled turn cuts the pipe
// mid-line, so half a JSON object is a normal thing to see, not a crash.
func TestCursorParserSurvivesATruncatedStream(t *testing.T) {
	var c Cursor
	for _, partial := range [][]byte{
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"te`),
		[]byte(`{"type":"system","subtype":"in`),
		[]byte(`{`),
	} {
		if _, ok := c.ParseEvent(partial); ok {
			t.Errorf("a truncated line produced an event: %s", partial)
		}
	}
}

func mustCursorFirst(t *testing.T, prompt string) runner.Spec {
	t.Helper()
	return mustCursorFirstPosture(t, prompt, PostureRead)
}

func mustCursorFirstPosture(t *testing.T, prompt string, p Posture) runner.Spec {
	t.Helper()
	s, err := Cursor{}.FirstTurn(prompt, `C:\ws`, "cursor-agent", p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustCursorNext(t *testing.T, prompt, sess string) runner.Spec {
	t.Helper()
	return mustCursorNextPosture(t, prompt, sess, PostureRead)
}

func mustCursorNextPosture(t *testing.T, prompt, sess string, p Posture) runner.Spec {
	t.Helper()
	s, err := Cursor{}.NextTurn(prompt, `C:\ws`, "cursor-agent", sess, p)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
