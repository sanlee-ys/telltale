package vendors

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestCursorFlagsMatchTheInstalledCLI pins the first-turn flags against
// cursor-agent 2026.07.23-e383d2b's own --help.
//
// These flags are now RUN flags rather than read flags: this exact combination
// produced four live turns on 2026-08-04. The one that did not survive contact
// is asserted separately in TestCursorDropsTheSandboxFlagOnWindows.
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
	for _, windows := range []bool{true, false} {
		read := cursorBaseArgs(PostureRead, windows)
		if !slices.Contains(read, "plan") {
			t.Errorf("windows=%v read posture asks for nothing: %v", windows, read)
		}
		write := cursorBaseArgs(PostureWrite, windows)
		if slices.Contains(write, "plan") || slices.Contains(write, "--sandbox") {
			t.Errorf("windows=%v write posture kept the read-only requests: %v", windows, write)
		}
		// Dropping the flags rather than passing --sandbox disabled: council is
		// declining to ask for a restriction, not overriding a user's own config
		// to remove one.
		if slices.Contains(write, "disabled") {
			t.Error("write posture overrides the user's sandbox config instead of leaving it alone")
		}
	}
}

// TestCursorDropsTheSandboxFlagOnWindows is a measurement, not a portability
// nicety, and it is the difference between a seat that answers and one that
// cannot.
//
// Captured 2026-08-04: `--sandbox enabled` on Windows does not weakly apply, it
// aborts before any model call —
//
//	Error: Sandbox mode is enabled but not available on this system.
//	Sandbox requires macOS or Linux.
//
// with exit 1. Passing it there would fail every read-posture turn, which is
// how the strongest-looking half of this posture would have silently become the
// reason the column never spoke. Off Windows it stays: the install ships a real
// cursorsandbox.exe and the flag at least does not refuse.
func TestCursorDropsTheSandboxFlagOnWindows(t *testing.T) {
	if args := cursorBaseArgs(PostureRead, true); slices.Contains(args, "--sandbox") {
		t.Errorf("windows read posture passes --sandbox: %v — the CLI exits 1 on that flag here", args)
	}
	args := cursorBaseArgs(PostureRead, false)
	i := slices.Index(args, "--sandbox")
	if i < 0 || i+1 >= len(args) || args[i+1] != "enabled" {
		t.Errorf("non-windows read posture dropped --sandbox enabled: %v", args)
	}
}

// TestCursorRunsTheBundleThroughNodeDirectly is the seat's whole existence on
// Windows.
//
// Detection resolves this vendor to the node.exe that cursor-agent.cmd would
// have run; the adapter has to hand that node its JavaScript entry point as the
// FIRST argument, or it starts a REPL and the turn hangs. The bundle is derived
// from the binary rather than passed alongside it, so the two can never
// disagree about which install is being driven.
func TestCursorRunsTheBundleThroughNodeDirectly(t *testing.T) {
	node := filepath.Join(`C:\Users\dev\AppData\Local\cursor-agent\versions\2026.07.23-e383d2b`, "node.exe")
	spec, err := Cursor{}.FirstTurn("brief", `C:\ws`, node, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(filepath.Dir(node), "index.js")
	if len(spec.Args) == 0 || spec.Args[0] != want {
		t.Fatalf("Args[0] = %v, want the bundle %q", spec.Args, want)
	}
	// Still argv, still last, still no stdin — the transport did not change,
	// only the shell that used to be in front of it.
	if spec.Args[len(spec.Args)-1] != "brief" {
		t.Errorf("the prompt is no longer the final argument: %v", spec.Args)
	}
	if spec.StdinPrompt != "" {
		t.Error("a stdin prompt appeared on a CLI that has no stdin path for one")
	}
	// The flags still have to come between the bundle and the prompt.
	if !slices.Contains(spec.Args, "-p") || !slices.Contains(spec.Args, "stream-json") {
		t.Errorf("flags were lost when the bundle was prepended: %v", spec.Args)
	}

	// The resume path takes the same treatment; a bundle on turn one and not on
	// turn two would be a seat that answers once.
	next, err := Cursor{}.NextTurn("follow up", `C:\ws`, node, "sess-1", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if len(next.Args) == 0 || next.Args[0] != want {
		t.Errorf("NextTurn lost the bundle: %v", next.Args)
	}
}

// TestCursorLeavesANativeEntryPointAlone: on macOS and Linux the resolved
// binary IS cursor-agent, and prepending a JavaScript path to its argv would
// make the first thing it sees a file it was never asked to read.
func TestCursorLeavesANativeEntryPointAlone(t *testing.T) {
	spec, err := Cursor{}.FirstTurn("brief", "/ws", "/usr/local/bin/cursor-agent", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Args[0] != "-p" {
		t.Errorf("Args[0] = %q, want the first flag — nothing should be prepended here: %v", spec.Args[0], spec.Args)
	}
}

// TestCursorSeparatesTheBriefFromTheFlags closes the hazard ADR-008's fifth
// amendment recorded as unresolved, with the run it asked for.
//
// The prompt is a variadic positional, so a brief opening with "-" is read as
// an option. Measured 2026-08-04:
//
//	… --workspace <ws> "--seriously reply with OK"
//	    → error: unknown option '--seriously reply with OK'
//	… --workspace <ws> -- "--seriously reply with OK"
//	    → a normal turn, result "OK"
//
// The separator was left out originally because getting it wrong breaks every
// brief rather than a rare one. It is in now because both forms were run.
func TestCursorSeparatesTheBriefFromTheFlags(t *testing.T) {
	for _, spec := range []runner.Spec{
		mustCursorFirst(t, "-- not a flag"),
		mustCursorNext(t, "-- not a flag", "sess-1"),
	} {
		n := len(spec.Args)
		if n < 2 || spec.Args[n-2] != "--" {
			t.Errorf("no -- immediately before the prompt: %v", spec.Args)
		}
		if spec.Args[n-1] != "-- not a flag" {
			t.Errorf("the brief was altered: %q", spec.Args[n-1])
		}
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

// TestCursorShimIsRefusedByTheRunner: the rule that made this seat unusable is
// still armed, and must stay armed.
//
// Detection no longer hands the adapter a .cmd — it resolves the bundled node
// the .cmd would have run — but nothing about the refusal changed, and the
// refusal is what makes the resolution matter. If detection ever regressed to
// passing the shim through, the runner is the backstop that still says no
// rather than putting a brief through cmd.exe.
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

// --- Parser tests, over lines CAPTURED from live turns on 2026-08-04. ---
//
// The previous version of this block said the opposite: nothing below had been
// run, because the CLI was not signed in. It is signed in now and these lines
// are off the wire, abridged only where a field is irrelevant to the assertion.
// The two places where the wire disagreed with the bundle — the tool_call
// discriminator and the whole-message repeat — each have their own test, and
// each says which is which.
//
// One shape here is still bundle-derived rather than captured, and it is
// labelled at its own case: a SUCCESSFUL tool call. Every tool call on the
// probe machine was blocked by a hook, so `result.success` has never actually
// come down this pipe.

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

// TestCursorDoesNotRenderTheWholeMessageRepeat is the bug a live run found and
// no amount of bundle reading would have.
//
// cursor-agent sends its text deltas AND then the complete message as one more
// assistant event. Captured, in full, from a turn asked to reply "PONG":
//
//	…"text":"P"…,"timestamp_ms":1785855682260}
//	…"text":"ONG"…,"timestamp_ms":1785855682264}
//	…"text":"PONG"…}          ← no timestamp_ms
//
// Appending all three renders "PONGPONG". The absence of timestamp_ms held on
// all three captured turns of that shape — every one of which was a turn with
// no tool call in it, which is why it read as the whole rule and was not. See
// TestCursorSegmentedTurnRendersEachPassageOnce for the half it missed; this
// test is kept unchanged because that half must not be fixed by breaking this
// one.
func TestCursorDoesNotRenderTheWholeMessageRepeat(t *testing.T) {
	var body string
	for _, line := range [][]byte{
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"P"}]},"session_id":"s1","timestamp_ms":1785855682260}`),
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ONG"}]},"session_id":"s1","timestamp_ms":1785855682264}`),
		[]byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"PONG"}]},"session_id":"s1"}`),
	} {
		if ev, ok := (Cursor{}).ParseEvent(line); ok && ev.Kind == runner.KindText {
			body += ev.Text
		}
	}
	if body != "PONG" {
		t.Errorf("body = %q, want %q — the whole-message repeat was concatenated onto its own deltas", body, "PONG")
	}
}

// TestCursorSegmentedTurnRendersEachPassageOnce replays a real turn, whole,
// and is the test the "X X Y" the owner saw would have failed.
//
// The rule above — drop the assistant event with no timestamp_ms — was derived
// from turns that used no tools, and every such turn is ONE model call with one
// whole-message repeat at its end. A turn that runs a tool is several model
// calls, and each of THEM ends in a repeat of its own segment. Those mid-turn
// repeats carry timestamp_ms like any delta, so the old rule passed them
// straight through and the column rendered the segment, then the segment again,
// then the next one.
//
// What separates them is model_call_id, present on the repeat and absent from
// every delta. The assertion is not a substring count picked to fit: the deltas
// alone must reconstruct the reply the vendor itself put in the `result` event,
// which is the same turn's own answer to what it said.
func TestCursorSegmentedTurnRendersEachPassageOnce(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "cursor-segmented-turn.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	var body, whole string
	var segments int
	for _, line := range bytes.Split(raw, []byte("\n")) {
		ev, ok := Cursor{}.ParseEvent(line)
		if !ok {
			continue
		}
		switch ev.Kind {
		case runner.KindText:
			body += ev.Text
		case runner.KindMeta:
			whole = ev.Text
		}
	}
	if whole == "" {
		t.Fatal("the fixture carried no result event; the invariant below has nothing to check against")
	}
	if body != whole {
		t.Errorf("streamed body and the vendor's own result disagree:\n body   = %q\n result = %q", body, whole)
	}

	// Named explicitly as well, because "the two agree" would also be satisfied
	// if a future change doubled BOTH. This is the passage the owner watched
	// render twice.
	const passage = "Beginning the survey of this repository now."
	if n := strings.Count(body, passage); n != 1 {
		t.Errorf("the first segment appears %d times in the body, want 1", n)
	}
	segments = strings.Count(whole, "The Read tool hit a hook error")
	if segments != 1 {
		t.Errorf("fixture drifted: the middle segment appears %d times in the result", segments)
	}
}

// TestCursorWholeMessageRepeatIsDroppedByModelCallID pins the discriminator on
// its own, at one line, so a failure says which of the two rules broke.
//
// Copied from the fixture. It carries timestamp_ms — that is the whole point,
// and is why the older rule let it through — and it carries model_call_id,
// which no delta in 108 captured assistant events ever did.
func TestCursorWholeMessageRepeatIsDroppedByModelCallID(t *testing.T) {
	repeat := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Beginning the survey of this repository now."}]},"session_id":"s1","model_call_id":"mc-0","timestamp_ms":1785894419785}`)
	if ev, ok := (Cursor{}).ParseEvent(repeat); ok && ev.Kind == runner.KindText {
		t.Fatalf("a mid-turn whole-message repeat rendered as a delta: %q", ev.Text)
	}
	delta := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Beginning"}]},"session_id":"s1","timestamp_ms":1785894418573}`)
	ev, ok := Cursor{}.ParseEvent(delta)
	if !ok || ev.Kind != runner.KindText || ev.Text != "Beginning" {
		t.Fatalf("the delta beside it was dropped too: (%v, %v)", ev, ok)
	}
}

// TestCursorFinalOnlyTurnStillRendersThroughTheResult is why the discriminator
// above is safe to be wrong about.
//
// Without --stream-partial-output the vendor sends ONLY the whole message, with
// no timestamp_ms — so the rule above drops everything. Captured that way on
// purpose. The column is not empty, because the result event carries the entire
// reply and the room uses it whenever a column streamed nothing: the failure
// mode of this field changing upstream is a column that fills at the end, not
// one that is wrong.
func TestCursorFinalOnlyTurnStillRendersThroughTheResult(t *testing.T) {
	whole := []byte(`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]},"session_id":"s1"}`)
	if ev, ok := (Cursor{}).ParseEvent(whole); ok && ev.Kind == runner.KindText {
		t.Fatalf("the whole-message event rendered as a delta: %q", ev.Text)
	}
	res := []byte(`{"type":"result","subtype":"success","is_error":false,"result":"OK","session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(res)
	if !ok || ev.Kind != runner.KindMeta || ev.Text != "OK" {
		t.Fatalf("the result did not carry the reply as a fallback: (%v, %v)", ev, ok)
	}
}

// TestCursorToolActivityUsesTheWireDiscriminator. The first version of this
// adapter looked for `tool_call.tool.case`, because the bundle tests
// `"shellToolCall" === e.tool.case` internally. On the wire the protobuf oneof
// is FLATTENED to an object key, so that lookup matched nothing and every trace
// entry read "tool call". This line is copied from a capture.
func TestCursorToolActivityUsesTheWireDiscriminator(t *testing.T) {
	line := []byte(`{"type":"tool_call","subtype":"started","call_id":"c1","tool_call":{"shellToolCall":{"args":{"command":"ls -1","workingDirectory":"C:\\ws"}},"toolCallId":"c1","startedAtMs":"1785855754954"},"session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindActivity {
		t.Fatalf("got (%v, %v), want a KindActivity", ev, ok)
	}
	// Acts, not Text: the room reads tool news off Acts and an adapter that set
	// Text alone would produce a permanently empty trace. That is what the old
	// version did.
	if len(ev.Acts) != 1 {
		t.Fatalf("Acts = %v, want exactly one call", ev.Acts)
	}
	if ev.Acts[0].Text != "shell: ls -1" {
		t.Errorf("Text = %q, want the tool and its command", ev.Acts[0].Text)
	}
	if ev.Acts[0].ID != "c1" {
		t.Errorf("ID = %q; without it the result cannot resolve this entry", ev.Acts[0].ID)
	}
	if ev.Acts[0].Outcome != runner.ActPending {
		t.Errorf("Outcome = %v, want ActPending — an announced call has not resolved", ev.Acts[0].Outcome)
	}
	// The metadata keys beside the discriminator must not be mistaken for it.
	if strings.Contains(ev.Acts[0].Text, "toolCallId") || strings.Contains(ev.Acts[0].Text, "startedAtMs") {
		t.Errorf("a metadata key was read as the tool name: %q", ev.Acts[0].Text)
	}
}

// TestCursorToolOutcomesLandOnTheSameEntry. Both halves of a call are taken
// now, correlated by call_id: "started" opens the entry, "completed" resolves
// it. Taking only the announcement was right while the trace was append-only
// and is wrong now that a running command has to read differently from a failed
// one.
//
// Every failure shape below was captured. There are two of them for `error`
// alone — readToolCall sends {"errorMessage":…} and grepToolCall sends
// {"error":…} — on the same stream, which is why the text is dug out rather
// than declared.
func TestCursorToolOutcomesLandOnTheSameEntry(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		want    runner.ActStatus
		detail  string
		actText string
	}{
		{
			name:    "rejected by a hook",
			line:    `{"type":"tool_call","subtype":"completed","call_id":"c1","tool_call":{"shellToolCall":{"result":{"rejected":{"command":"ls -1","reason":"Hook blocked with message: nope"}}}},"session_id":"s1"}`,
			want:    runner.ActFailed,
			detail:  "Hook blocked with message: nope",
			actText: "shell",
		},
		{
			name:    "errorMessage shape",
			line:    `{"type":"tool_call","subtype":"completed","call_id":"c2","tool_call":{"readToolCall":{"result":{"error":{"errorMessage":"could not open the file"}}}},"session_id":"s1"}`,
			want:    runner.ActFailed,
			detail:  "could not open the file",
			actText: "read",
		},
		{
			name:    "error shape",
			line:    `{"type":"tool_call","subtype":"completed","call_id":"c3","tool_call":{"grepToolCall":{"result":{"error":{"error":"pattern was rejected"}}}},"session_id":"s1"}`,
			want:    runner.ActFailed,
			detail:  "pattern was rejected",
			actText: "grep",
		},
		{
			// The oneof's third case, read off the bundle's own field
			// descriptors ({no:1,name:"success",kind:"message",oneof:"result"}).
			// NOT captured: every tool call on the probe machine was stopped by
			// a hook, so no success ever came down the pipe. Recorded here as
			// the bundle-derived half of this parser.
			name:    "success",
			line:    `{"type":"tool_call","subtype":"completed","call_id":"c4","tool_call":{"readToolCall":{"result":{"success":{"content":"…"}}}},"session_id":"s1"}`,
			want:    runner.ActOK,
			actText: "read",
		},
		{
			// Ended, and said nothing about how. ActUnknown rather than ActOK:
			// inventing a success on a vendor's behalf is the one move this
			// trace is built to refuse.
			name:    "no result at all",
			line:    `{"type":"tool_call","subtype":"completed","call_id":"c5","tool_call":{"someFutureToolCall":{}},"session_id":"s1"}`,
			want:    runner.ActUnknown,
			actText: "someFuture",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := Cursor{}.ParseEvent([]byte(tc.line))
			if !ok || len(ev.Acts) != 1 {
				t.Fatalf("got (%v, %v), want one act", ev, ok)
			}
			got := ev.Acts[0]
			if got.Outcome != tc.want {
				t.Errorf("Outcome = %v, want %v", got.Outcome, tc.want)
			}
			if tc.detail != "" && got.Detail != tc.detail {
				t.Errorf("Detail = %q, want the vendor's own words %q", got.Detail, tc.detail)
			}
			if got.Text != tc.actText {
				t.Errorf("Text = %q, want %q", got.Text, tc.actText)
			}
			// ActDenied is council's record of ITS OWN gate keystroke. A refusal
			// read off a vendor's stream is not that, and rendering it as one
			// would claim the user was asked something they never saw.
			if got.Outcome == runner.ActDenied {
				t.Error("a vendor-reported refusal was rendered as a council gate denial")
			}
		})
	}
}

// TestCursorCompletedWithoutAnAnnouncementStillLands: captured — a taskToolCall
// arrived already resolved, with no "started" before it. The room appends that
// as a finished entry, but only if the adapter names it; an act with no text is
// dropped upstream and the step disappears.
func TestCursorCompletedWithoutAnAnnouncementStillLands(t *testing.T) {
	line := []byte(`{"type":"tool_call","subtype":"completed","call_id":"c9","tool_call":{"taskToolCall":{"result":{"error":{"error":"Task blocked by preToolUse hook"}}}},"session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(line)
	if !ok || len(ev.Acts) != 1 || ev.Acts[0].Text == "" {
		t.Fatalf("an unannounced completion produced %v (%v); it must still name itself", ev, ok)
	}
}

// TestCursorCallIDsSurviveTheirEmbeddedNewline. These ids really do contain a
// literal \n — "call-ab9b…-0\nfc_88e2…_0" — and both halves of a call carry the
// identical string. It looks like corruption and is not; correlation depends on
// it being passed through untouched.
func TestCursorCallIDsSurviveTheirEmbeddedNewline(t *testing.T) {
	const id = "call-ab9b3fba-8b2f-4bb2-aaae-bf7d4e845eb7-0\nfc_88e24da8-008f-98fe-b3b2-16e7b20caf7b_0"
	started := []byte(`{"type":"tool_call","subtype":"started","call_id":"call-ab9b3fba-8b2f-4bb2-aaae-bf7d4e845eb7-0\nfc_88e24da8-008f-98fe-b3b2-16e7b20caf7b_0","tool_call":{"readToolCall":{"args":{"path":"C:\\ws\\note.txt"}}},"session_id":"s1"}`)
	ev, ok := Cursor{}.ParseEvent(started)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("got (%v, %v)", ev, ok)
	}
	if ev.Acts[0].ID != id {
		t.Errorf("ID = %q, want it passed through unaltered", ev.Acts[0].ID)
	}
	// clipArg collapses whitespace, so the newline cannot reach the column as a
	// second trace line even though it is in the id.
	if strings.Contains(ev.Acts[0].Text, "\n") {
		t.Errorf("a newline reached the rendered text: %q", ev.Acts[0].Text)
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
	if len(ev.Acts) != 1 || ev.Acts[0].Text == "" {
		t.Errorf("an activity line with no text renders as a blank step: %v", ev.Acts)
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
