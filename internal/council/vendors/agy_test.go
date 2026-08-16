package vendors

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Every fixture line below is real captured stdout from `agy --output-format
// stream-json` on 2026-08-03, pasted verbatim rather than hand-written from the
// docs. Hand-written fixtures are how a parser ends up passing its tests against
// a schema the vendor does not actually emit.

// TestAgyFlagsMatchTheInstalledCLI pins the flags against `agy --help`.
func TestAgyFlagsMatchTheInstalledCLI(t *testing.T) {
	spec, err := Antigravity{}.FirstTurn("brief", `C:\ws`, `C:\bin\agy.exe`, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--output-format", "stream-json", "--disable-slash-commands", "-p"} {
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
	if _, err := (Antigravity{}).NextTurn("p", "", "agy", "", PostureRead); err != ErrNoResume {
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
	// Contract change, 2026-08-04: the TOOL steps here are no longer DROPPED,
	// they are surfaced as KindActivity so a command-and-control room can show
	// what the vendor is doing. (The checkpoint, user_input and system_message
	// lines are dropped again as of the same day, as plumbing rather than as
	// chatter — see TestAgyPlumbingStepsNeverReachTheTrace.) The invariant the
	// original test protected is unchanged and is what is asserted here — tool
	// chatter must never become the vendor's PROSE. It is now enforced by the
	// event kind rather than by silence, which is stronger: the renderer draws
	// the two differently.
	var a Antigravity
	for _, l := range lines {
		ev, ok := a.ParseEvent(l)
		if ok && ev.Kind == runner.KindText {
			t.Errorf("non-assistant step became assistant text: %+v: %s", ev, l)
		}
	}
}

// TestAgyPlumbingStepsNeverReachTheTrace.
//
// Every line here is verbatim from the live captures of 2026-08-04 (agy 1.1.10,
// Windows) — the run whose room showed San `user_input ?`, `system_message ?`,
// `checkpoint ?` and `unknown ?` and prompted "it's not something I need to see
// from an end-user perspective".
//
// The suppression is an ALLOWLIST defended per kind, not a filter on what looks
// noisy, and the asymmetry is the whole point: hiding a vendor's ACTIONS would
// be a false gauge, hiding its plumbing is noise reduction. Note what each of
// these lines does NOT carry — no tool_name, no tool_info, no parameters. That
// is the evidence, and it is why this is not the same move as dropping a step
// whose type we merely failed to recognise.
//
// It is asserted at the PARSER, because a step that is not an action must never
// become one: council keeps Render pure over State, so filtering in the view
// would leave a lie in the model.
func TestAgyPlumbingStepsNeverReachTheTrace(t *testing.T) {
	lines := map[string][]byte{
		// turn 1, step 0 — the brief council itself just sent, echoed back.
		"user_input": []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":0,"state":"DONE","step_type":"user_input"}}`),
		// turn 1, step 1 — 0.5ms, in the preamble, naming nothing.
		"unknown": []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":1,"state":"DONE","step_type":"unknown","duration_seconds":0.0005175}}`),
		// turn 1, step 4 — a conversation bookmark, ~120 tokens, no workspace.
		"checkpoint": []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":4,"state":"DONE","step_type":"checkpoint","duration_seconds":0.553991,"usage":{"input_tokens":118,"output_tokens":5,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":123}}}`),
		// turn 1, step 10 — an empty marker on a failing turn. Safe to drop only
		// because the turn-level failure is reported by the result path with
		// words; TestAgyResultErrorCarriesTheVendorsSentence is that check.
		"error_message": []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":10,"state":"DONE","step_type":"error_message"}}`),
		// the resume turn, step 12 — agy placing its own message in the thread.
		"system_message": []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":12,"state":"DONE","step_type":"system_message"}}`),
	}
	var a Antigravity
	for kind, l := range lines {
		if ev, ok := a.ParseEvent(l); ok {
			t.Errorf("%s reached the trace as %+v; it is conversation plumbing, "+
				"and the room renders it as a gear icon with a shrug next to it", kind, ev)
		}
	}
}

// TestAgyUnknownStepCarryingAToolIsNotSuppressed is the reversal condition for
// the one suppressed kind that had to be argued rather than listed.
//
// `unknown` is agy's own label, and suppressing a step merely because THIS
// adapter does not recognise its type would be the same class of mistake as
// inventing an outcome for it. The captured `unknown` steps are plumbing on the
// evidence — fixed preamble slot, half a millisecond, no tool name, no
// parameters — so they are dropped. But the decision is gated on that shape
// rather than on the label, so if agy ever starts ACTING through this type the
// trace shows it the same turn.
//
// This line is the one fixture in this file that is NOT a capture: no observed
// `unknown` step carries a tool. It is written to pin the reversal, and it is
// labelled so nobody later mistakes it for evidence that agy emits this.
func TestAgyUnknownStepCarryingAToolIsNotSuppressed(t *testing.T) {
	line := []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c","step_index":1,"state":"ACTIVE","step_type":"unknown","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"go test ./..."}}}}`)
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
		t.Fatalf("an `unknown` step that named a tool was suppressed: (%+v, %v)", ev, ok)
	}
	if ev.Acts[0].Text != "run_command: go test ./..." {
		t.Errorf("Text = %q, want the tool it named", ev.Acts[0].Text)
	}
}

// TestAgyToolStepsCarryTheirRealName: agy's tool names were on the wire the
// whole time and the adapter was rendering `al.StepUpdate.StepType` — the
// literal string "tool" — so every real call read as a bare `tool ?`.
//
// This is ADR-008's tenth amendment repeating itself: Cursor's `tool_call.tool.
// case` lookup matched nothing because the oneof arrives FLATTENED to a key, and
// every trace entry read "tool call". Same bug, same cause — the fields the
// vendor sends were never compared against the fields the parser reads — and the
// same fix, which is to parse what ARRIVES.
//
// Both lines are verbatim from turn 1 of the 2026-08-04 capture.
func TestAgyToolStepsCarryTheirRealName(t *testing.T) {
	for _, tc := range []struct {
		line []byte
		want string
	}{
		{
			[]byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":3,"state":"ACTIVE","step_type":"tool","tool_name":"list_dir","tool_info":{"name":"list_dir","parameters":{"DirectoryPath":"C:\\Users\\sanle\\.gemini\\antigravity-cli\\scratch"}}}}`),
			`list_dir: C:\Users\sanle\.gemini\antigravity-cli\scratch`,
		},
		{
			[]byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":8,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"pwsh -Command \"Get-Location; Get-ChildItem\""}}}}`),
			`run_command: pwsh -Command "Get-Location; Get-ChildItem"`,
		},
		// A tool that sends no parameters degrades to its bare name rather than
		// to the step type. Real captured list_permissions step.
		{
			[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":3,"state":"ACTIVE","step_type":"tool","tool_name":"list_permissions","tool_info":{"name":"list_permissions"}}}`),
			"list_permissions",
		},
	} {
		ev, ok := Antigravity{}.ParseEvent(tc.line)
		if !ok || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
			t.Fatalf("got (%+v, %v), want one activity", ev, ok)
		}
		if got := ev.Acts[0].Text; got != tc.want {
			t.Errorf("Text = %q, want %q", got, tc.want)
		}
		if ev.Acts[0].Text == "tool" {
			t.Error("the step TYPE is being rendered again; every call reads alike")
		}
	}
}

// TestAgyToolArgIsDeterministic pins the property that matters about the
// argument rule, which is not which key wins but that the SAME key always wins.
//
// Parameters are an arbitrary JSON object, so the candidate set comes out of a
// Go map, and Go randomises map iteration. A rule that let that order through
// would make a rendered trace line — and any golden containing one — flicker
// between runs. Lowest key name by byte order, hence "CommandLine" over
// "TargetFile" here.
//
// The multi-string case is not a captured shape: every observed agy tool sends
// exactly one string parameter. The rule exists so that the first tool to send
// two does not produce a coin flip.
func TestAgyToolArgIsDeterministic(t *testing.T) {
	line := []byte(`{"event":"step_update","step_update":{"step_index":9,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"TargetFile":"C:\\b.txt","CommandLine":"go vet ./...","Blocking":true,"Nested":{"k":"v"}}}}}`)
	const want = "run_command: go vet ./..."
	for i := 0; i < 64; i++ {
		ev, ok := Antigravity{}.ParseEvent(line)
		if !ok || len(ev.Acts) != 1 {
			t.Fatalf("got (%+v, %v)", ev, ok)
		}
		if got := ev.Acts[0].Text; got != want {
			t.Fatalf("iteration %d rendered %q, want %q; map order is leaking into the trace", i, got, want)
		}
	}
}

// TestAgyErrorStateIsAFailedCallNotAPendingOne is the fifth state, which was
// being dropped on the floor.
//
// The switch handled ACTIVE and DONE only, so this line matched nothing and the
// ACTIVE entry it was meant to resolve stayed PENDING for the rest of the room's
// life. A tool call the vendor had already refused rendered as one still
// running — a false gauge in the direction §9.6a cares about most.
//
// Verbatim from turn 1 of the 2026-08-04 capture, the pair that proves it: the
// same step_index 8 opens ACTIVE and resolves ERROR.
func TestAgyErrorStateIsAFailedCallNotAPendingOne(t *testing.T) {
	active := []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":8,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"pwsh -Command \"Get-Location; Get-ChildItem\""}}}}`)
	failed := []byte(`{"event":"step_update","step_update":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","step_index":8,"state":"ERROR","step_type":"tool","tool_name":"run_command","duration_seconds":0.6643425,"tool_info":{"name":"run_command","parameters":{"CommandLine":"pwsh -Command \"Get-Location; Get-ChildItem\""},"error":{"type":"TOOL_ERROR","message":"error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: granting access to C:\\: Access is denied."}}}}`)

	var a Antigravity
	ev, ok := a.ParseEvent(active)
	if !ok || ev.Acts[0].Outcome != runner.ActPending {
		t.Fatalf("ACTIVE produced (%+v, %v)", ev, ok)
	}
	openedAs := ev.Acts[0].ID

	ev, ok = a.ParseEvent(failed)
	if !ok || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
		t.Fatalf("the ERROR state was dropped: (%+v, %v); the call stays pending forever", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActFailed {
		t.Errorf("Outcome = %v, want ActFailed: the vendor said ERROR and named the reason", ev.Acts[0].Outcome)
	}
	if ev.Acts[0].ID != openedAs {
		t.Errorf("ERROR id %q does not match the ACTIVE id %q, so it opens a second entry "+
			"and leaves the first one pending", ev.Acts[0].ID, openedAs)
	}
	// The vendor's own first line, never a sentence composed here (§9.6a).
	const want = "error executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND: granting access to C:\\: Access is denied."
	if ev.Acts[0].Detail != want {
		t.Errorf("Detail = %q, want the vendor's own words %q", ev.Acts[0].Detail, want)
	}
}

// TestAgyResultErrorCarriesTheVendorsSentence.
//
// This is what makes suppressing the wordless `error_message` step honest: the
// turn-level failure it marks is reported through the result path, with the
// vendor's own diagnosis rather than a composed one. Verbatim from turn 1's
// final line, which carried an EMPTY response and the sentence in `error`.
func TestAgyResultErrorCarriesTheVendorsSentence(t *testing.T) {
	line := []byte(`{"event":"result","result":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","status":"ERROR","response":"","error":"Agent execution terminated due to error.","duration_seconds":5.1746031,"num_turns":1}}`)
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%+v, %v), want KindError", ev, ok)
	}
	if ev.Note != "Agent execution terminated due to error." {
		t.Errorf("Note = %q; with error_message suppressed this note is the only sign "+
			"the turn failed, so it must carry the vendor's words", ev.Note)
	}
	// And it says NOTHING about the conversation. This exact sentence was
	// captured on a turn whose thread was demonstrably alive (conversation_id
	// back, step_index 10 → 11, num_turns 2) and is also what a dead thread
	// would plausibly produce. A string on both sides of a distinction is
	// evidence for neither side of it.
	if ev.Failure != runner.FailureUnclassified {
		t.Errorf("Failure = %v, want Unclassified — this sentence was measured on a LIVE thread",
			ev.Failure)
	}
}

// TestAgyClassifiesItsMeasured503AndNothingElse.
//
// The one agy failure that is known not to be about the conversation, quoted
// verbatim off a capture (agy 1.1.10, Windows, 2026-08-04). Note the EMPTY
// conversation_id on the captured line: the turn died before a thread was
// involved at all, which is the corroboration that makes the classification a
// reading rather than an inference.
//
// It is paired with a near-miss on purpose. This classifier's whole job is to
// stay narrow — a false transient wedges a seat retrying a dead id forever —
// so the test that matters is the one that does NOT match.
func TestAgyClassifiesItsMeasured503AndNothingElse(t *testing.T) {
	line := []byte(`{"event":"result","result":{"conversation_id":"","status":"ERROR","response":"","error":"Eligibility check failed: UNAVAILABLE (code 503): The service is currently unavailable.","duration_seconds":1.2,"num_turns":0}}`)
	ev, ok := Antigravity{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindError {
		t.Fatalf("got (%+v, %v), want KindError", ev, ok)
	}
	if ev.Failure != runner.FailureVendorUnavailable {
		t.Errorf("Failure = %v, want FailureVendorUnavailable", ev.Failure)
	}
	// The vendor's own sentence still reaches the card. Classifying a failure
	// must never be a reason to stop quoting it.
	if !strings.Contains(ev.Note, "503") {
		t.Errorf("Note = %q, want the vendor's own words", ev.Note)
	}

	// A different outage-flavoured sentence nobody has captured. Reading
	// "unavailable" alone as a service outage is how a narrow classifier turns
	// into a guess.
	near := []byte(`{"event":"result","result":{"conversation_id":"abc","status":"ERROR","response":"","error":"The requested model is unavailable for this account.","num_turns":1}}`)
	ev, ok = Antigravity{}.ParseEvent(near)
	if !ok {
		t.Fatal("the near-miss line did not parse")
	}
	if ev.Failure != runner.FailureUnclassified {
		t.Errorf("Failure = %v on an uncaptured sentence, want Unclassified", ev.Failure)
	}
}

// TestAgyDoneResolvesToUnknownNeverToSuccess is the honest-gauge rule applied
// to the one vendor whose outcome genuinely cannot be read.
//
// Every captured DONE line carries duration_seconds, sometimes a tool_info with
// the call's parameters, and NOTHING that says whether the step achieved
// anything — no status, no exit code, no error field. agy reports success or
// failure exactly once per turn, in the final `result` event, and that verdict
// is about the TURN rather than about any one step.
//
// So a finished agy step is a step whose outcome has not been observed. If this
// ever resolves to ActOK, the room has started inventing results on a vendor's
// behalf, which is the single thing this product exists not to do.
func TestAgyDoneResolvesToUnknownNeverToSuccess(t *testing.T) {
	// Both lines are real captured output, and they are a matched pair: the
	// same step_index is what lets DONE resolve the entry ACTIVE opened.
	active := []byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":6,"state":"ACTIVE","step_type":"tool","tool_name":"write_to_file","tool_info":{"name":"write_to_file"}}}`)
	done := []byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":6,"state":"DONE","step_type":"tool","tool_name":"write_to_file","duration_seconds":0.0604336,"tool_info":{"name":"write_to_file","parameters":{"TargetFile":"C:\\probe.txt"}}}}`)

	var a Antigravity
	ev, ok := a.ParseEvent(active)
	if !ok || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
		t.Fatalf("ACTIVE produced (%+v, %v), want one activity", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActPending {
		t.Errorf("Outcome = %v; a step that just started has not ended", ev.Acts[0].Outcome)
	}
	openedAs := ev.Acts[0].ID

	ev, ok = a.ParseEvent(done)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("DONE produced (%+v, %v)", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActUnknown {
		t.Errorf("Outcome = %v, want ActUnknown: agy reports no per-step success signal", ev.Acts[0].Outcome)
	}
	if ev.Acts[0].ID != openedAs || openedAs == "" {
		t.Errorf("DONE id %q does not match the ACTIVE id %q; the two would render as separate steps",
			ev.Acts[0].ID, openedAs)
	}
	if ev.Acts[0].Detail != "" {
		t.Errorf("Detail = %q; agy offers none, so none may be shown", ev.Acts[0].Detail)
	}
}

// TestAgyStepZeroIsNotMistakenForAMissingIndex: the first step of a turn is
// `user_input` at step_index 0, so a plain int could not tell a real index 0
// from a line carrying no index at all — and every indexless step would then
// correlate with it.
//
// Both fixtures are the captured list_dir step with ONE field edited — the index
// set to 0 on the first, removed on the second — because the steps that actually
// sit at index 0 and that actually arrive without an index are all plumbing, and
// plumbing no longer reaches the trace to be correlated. The pointer still has to
// hold: any FUTURE step kind lands on this same path.
func TestAgyStepZeroIsNotMistakenForAMissingIndex(t *testing.T) {
	zero := []byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":0,"state":"DONE","step_type":"tool","tool_name":"list_dir","tool_info":{"name":"list_dir"}}}`)
	none := []byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","state":"DONE","step_type":"tool","tool_name":"list_dir","tool_info":{"name":"list_dir"}}}`)

	var a Antigravity
	ev, _ := a.ParseEvent(zero)
	if len(ev.Acts) != 1 || ev.Acts[0].ID != "step-0" {
		t.Fatalf("index 0 lost its id: %+v", ev.Acts)
	}
	ev, _ = a.ParseEvent(none)
	if len(ev.Acts) != 1 || ev.Acts[0].ID != "" {
		t.Errorf("a step with no index was given the id %q; it would resolve someone else's entry", ev.Acts[0].ID)
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

// TestAgyNamesNoEndOfTurn pins a measured decision rather than an oversight.
//
// `result` IS this seat's answer-complete marker: it is the last line on stdout,
// it carries the whole response, and this adapter already parses it. Setting
// EndsTurn on it would settle the column at the answer, the way the codex seat
// settles on `turn.completed`. It is deliberately left unset, because the tail
// that change exists to delete is not there. MEASURED 2026-08-16 on agy 1.1.13,
// three trials including one ~600-word reply: the process exits 0.314s, 0.049s
// and 0.135s after this line, against codex's 4.06s and 4.25s on the same box
// the same day (design.md §9.43's 2026-08-16 amendment).
//
// The test exists so that arming the flag costs a deliberate edit with a fresh
// measurement behind it, instead of looking like an obvious one-line fix. What
// would justify arming it is a re-measured tail that is material — and the two
// cases nobody has measured are named in agy.go: the failing turn and the
// tool-using turn.
func TestAgyNamesNoEndOfTurn(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"init","conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","init":{"cwd":"C:\\ws","tools":["view_file","write_to_file","run_command"],"permission_mode":"request-review"}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","step_index":2,"state":"ACTIVE","step_type":"agent_response","text_delta":"OK"}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","step_index":2,"state":"DONE","step_type":"agent_response","text_delta":"\n","duration_seconds":73.0806477}}`),
		[]byte(`{"event":"step_update","step_update":{"conversation_id":"09716b44","step_index":6,"state":"DONE","step_type":"tool","tool_name":"write_to_file","duration_seconds":0.0604336,"tool_info":{"name":"write_to_file","parameters":{"TargetFile":"C:\\probe.txt"}}}}`),
		[]byte(`{"event":"result","result":{"conversation_id":"2b18de13-bd04-4804-844e-0f75f2e3461e","status":"SUCCESS","response":"OK\n","duration_seconds":73.7446404,"num_turns":1}}`),
		[]byte(`{"event":"result","result":{"conversation_id":"14f3918c-ff9e-4962-81b9-357f5a658d1e","status":"ERROR","response":"","error":"Agent execution terminated due to error.","duration_seconds":5.1746031,"num_turns":1}}`),
	}
	for _, l := range lines {
		ev, ok := Antigravity{}.ParseEvent(l)
		if ok && ev.EndsTurn {
			t.Errorf("%s ended the turn; this seat is retired by its process exit, and the tail a marker would save measured 0.049s to 0.314s", l)
		}
	}
}

// TestAgyUnknownEventsAreIgnoredNotFatal. agy's discriminator is "event", not
// Claude's "type", so a Claude-shaped line must fall through rather than being
// half-parsed by a shared field name.
func TestAgyUnknownEventsAreIgnoredNotFatal(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"event":"some_future_thing","payload":{"a":1}}`),
		[]byte(`{"event":"init"}`), // no conversation id
		// An agent_response step carrying no text_delta: an empty message
		// rather than a step the vendor took.
		[]byte(`{"event":"step_update","step_update":{"state":"ACTIVE","step_type":"agent_response"}}`),
		// A state no captured run has produced. Dropped rather than mapped: an
		// unrecognised state is not evidence of anything, and inventing an
		// outcome for it is how a trace starts lying quietly.
		[]byte(`{"event":"step_update","step_update":{"step_index":1,"state":"QUEUED","step_type":"tool"}}`),
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

// TestAgyDeclaresItsSilentResumeFork. The seat's claim about the fork is what
// arms the room's comparison, so the claim itself is pinned here rather than
// left implicit in a type assertion three packages away.
//
// The version string is asserted to name the build, because that is the point of
// requiring one (vendors.SilentResumeFork): a claim about vendor behaviour in
// this repository carries the measurement it rests on, and a bump that fixes the
// fork has to leave this method looking stale.
func TestAgyDeclaresItsSilentResumeFork(t *testing.T) {
	f, ok := any(Antigravity{}).(SilentResumeFork)
	if !ok {
		t.Fatal("the agy seat no longer declares the silent resume fork; the room's lost-thread card would stop firing for it")
	}
	if got := f.SilentResumeForkMeasuredAt(); !strings.Contains(got, agyWireVersion) {
		t.Errorf("the fork claim names %q, which is not the build the wire fixture pins (%s) — one of the two was re-measured and the other was not",
			got, agyWireVersion)
	}
	// Nothing else may claim it. The measurement is agy's; asserting it for a
	// vendor nobody probed would make the card fire on an inference.
	for id, v := range Registry() {
		if id == model.VendorAntigravity {
			continue
		}
		if _, ok := any(v).(SilentResumeFork); ok {
			t.Errorf("%s declares a silent resume fork with no capture behind it", id)
		}
	}
}

// TestAgyForkedConversationSurfacesTheNewID is the parser half of the tell.
//
// The room compares the id it ASKED to resume against the id the stream reports.
// The comparison lives in council (§9.43) because ParseEvent sees one line at a
// time and never learns what was requested — but it is only possible if the
// returned id reaches the room at all, on a turn that looks entirely successful.
// That is what this pins.
//
// **The fixture is DERIVED, not captured, and that is stated because it matters.**
// `testdata/agy-forked-conversation.jsonl` is a byte-for-byte copy of the real
// 1.1.11 capture in `testdata/wire/` with ONE textual substitution: every
// `conversation_id` value changed from `2222…` to `3333…`. Nothing else moved —
// not a key, not a token count, not the status. It is deliberately NOT in
// `testdata/wire/`, whose contract is real captures only: the forked turn itself
// was measured (README, "Antigravity has no error frame reachable this way"), but
// that probe's stream was not kept, and a hand-edited file sitting among the
// captures would restate a measurement nobody re-ran.
func TestAgyForkedConversationSurfacesTheNewID(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "agy-forked-conversation.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	const requested = "22222222-2222-4222-8222-222222222222"
	const returned = "33333333-3333-4333-8333-333333333333"

	var ids []string
	var status runner.EventKind = runner.KindError
	body := ""
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ev, ok := Antigravity{}.ParseEvent([]byte(l))
		if !ok {
			continue
		}
		if ev.SessionID != "" {
			ids = append(ids, ev.SessionID)
		}
		if ev.Kind == runner.KindText {
			body += ev.Text
		}
		if ev.Kind == runner.KindMeta {
			status = ev.Kind
		}
	}

	if status != runner.KindMeta {
		t.Fatal("the forked turn did not come back as an ordinary successful result; the whole difficulty is that it does")
	}
	if strings.TrimSpace(body) != "ok" {
		t.Errorf("streamed body = %q, want %q — the reply is real and must still render", body, "ok")
	}
	if len(ids) == 0 {
		t.Fatal("no conversation id reached the room; the mismatch could never be seen")
	}
	for _, id := range ids {
		if id == requested {
			t.Errorf("the stream reported the REQUESTED id %q; the fixture no longer models a fork", requested)
		}
		if id != returned {
			t.Errorf("session id = %q, want the new conversation %q", id, returned)
		}
	}
}

func mustAgyFirst(t *testing.T, prompt string) runner.Spec {
	t.Helper()
	s, err := Antigravity{}.FirstTurn(prompt, `C:\ws`, "agy", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustAgyNext(t *testing.T, prompt, sess string) runner.Spec {
	t.Helper()
	s, err := Antigravity{}.NextTurn(prompt, `C:\ws`, "agy", sess, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
