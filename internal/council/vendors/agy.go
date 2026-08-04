package vendors

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Antigravity drives the Antigravity CLI (`agy`) in print mode.
//
// Every flag and every field name below was read off the installed binary on
// 2026-08-03 — `agy --help` for the flag set, and real captured stdout for the
// event schema. None of it is from memory, and the one thing that looks like a
// safety feature is documented here as not being one.
type Antigravity struct{}

func (Antigravity) ID() model.VendorID { return model.VendorAntigravity }

// -p is a STRING flag whose value IS the prompt. This is the trap in this CLI
// and it fails silently rather than loudly.
//
// `agy -p --output-format stream-json "Reply with exactly: OK"` does not error.
// -p swallows the literal text "--output-format" as the prompt, `stream-json`
// and the real prompt become ignored positional args, and the run comes back as
// plain text — the model helpfully explaining what an --output-format flag is.
// Verified: that exact invocation returned a paragraph about CLI output formats
// and exit 0. A trailing positional prompt is likewise ignored.
//
// So the prompt must be the value immediately following -p, and -p must be
// last. Two consequences the rest of this file depends on:
//
//   - The prompt goes in argv, not on stdin. `echo x | agy --output-format
//     stream-json -p` fails with "flag needs an argument: -p" (exit 2): agy
//     never reads stdin for the prompt. This is the one place this adapter
//     differs from Claude's, and it is not a preference — argv is the only
//     channel offered. agy resolves to a native agy.exe rather than a .cmd
//     shim, so runner's shell-shim refusal does not trip and the argv is passed
//     as a slice without a shell. The cost is the ~32K Windows command-line
//     limit, which a long multi-turn brief can reach; there is no workaround
//     short of upstream adding stdin support.
//   - Any flag added to this adapter must go BEFORE -p, or it becomes part of
//     the prompt.

// baseArgs is the shared invocation, minus the prompt.
//
// On the read-only flags, stated plainly because the alternative is a badge
// that lies: --mode plan and --sandbox DO NOT PREVENT WRITES. This was measured,
// not assumed. Running
//
//	agy --output-format stream-json --mode plan --sandbox -p "<create a file>"
//
// produced an init event whose permission_mode was still "request-review" and
// whose tools array still contained write_to_file, replace_file_content, sed_file
// and run_command — byte-identical to the same run without either flag. The
// agent then called write_to_file and the file was confirmed on disk. --sandbox's
// own help text says "terminal restrictions", which is about run_command and not
// about the filesystem.
//
// Whether it restricts the SHELL is no longer untested — first evidence, and it
// is not good news for the flag. Captured 2026-08-04 (agy 1.1.10, Windows):
// under `--mode plan --sandbox` a run_command step came back state ERROR with
// "granting access to C:\: Access is denied.", the agent gave up, and the turn
// ended status "ERROR" with an EMPTY response. The control run with both flags
// dropped ran its shell command and ended status "SUCCESS". Evidence class:
// measured, ONE trial per arm, with a confound stated plainly — the two turns
// issued DIFFERENT command lines (`pwsh -Command "Get-Location; Get-ChildItem"`
// versus `Get-ChildItem`), so this is not a clean A/B, and the refusal's mention
// of `C:\` may be about a drive root rather than about the flag.
//
// What it does establish is the observed COST: with these flags on, a turn that
// reaches for the shell can die with nothing to show for it. The flags are NOT
// changed here — that is a posture decision, not a parser one — and design.md
// §9.6b records the measurement with its confound.
//
// They are still passed, because they are the only read-only-leaning levers this
// CLI has and they cost nothing. But the claim this adapter supports is weaker
// than "unverified": the write LANDED, so the flags are refuted rather than
// merely unproven, and the room badges this vendor `unsandboxed` (council.
// SandboxNone) rather than `ro:requested`. Anything with an `ro:` prefix would
// read as a read-only posture at a glance, and this column can edit the
// workspace it is pointed at.
//
// This is the same mistake the Claude adapter made with --allowedTools, caught
// the same way: by running the command and reading what the session said about
// itself instead of trusting the flag's name.
func (Antigravity) baseArgs(p Posture) []string {
	args := []string{
		"--output-format", "stream-json",
		// A brief is arbitrary user text and may legitimately contain a line
		// starting with "/". Without this, agy expands that as a slash command or
		// skill. Effect not separately measured; it is a surface reduction, not a
		// safety claim.
		"--disable-slash-commands",
		// Deliberately absent: --dangerously-skip-permissions. It would
		// auto-approve every tool request. The tradeoff is real and is left
		// unmade on purpose — a turn that hits an "ask" permission has no TTY to
		// answer it and will sit until agy's own --print-timeout (default 5m)
		// expires. That default is also a ceiling on a long turn; it is left at
		// the vendor's value rather than guessed at here.
	}
	if p == PostureRead {
		// Requested, NOT enforced — see above. No badge may read these as a
		// sandbox claim. They are dropped in write posture because they were
		// only ever a read-only-leaning nudge, and agent-ops ADR-012 records
		// that --sandbox scopes TERMINAL access rather than writes: keeping it
		// in write mode would restrict something the user just asked to widen,
		// while bounding nothing they asked to bound.
		args = append(args, "--mode", "plan", "--sandbox")
	}
	return args
}

func (a Antigravity) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: a.ID(),
		Binary: binary,
		Args:   append(a.baseArgs(p), "-p", prompt),
		// StdinPrompt stays empty: agy does not read the prompt from stdin.
		Dir: workspace,
	}, nil
}

func (a Antigravity) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	return runner.Spec{
		Vendor: a.ID(),
		Binary: binary,
		// --conversation must precede -p, or it lands inside the prompt.
		// Verified as a real resume rather than a re-send: a second turn against a
		// captured conversation id echoed the same conversation_id, reported
		// num_turns 2, continued step_index from the first turn rather than
		// restarting at 0, and correctly answered a question that only the prior
		// turn's content could answer.
		Args: append(a.baseArgs(p), "--conversation", sessionID, "-p", prompt),
		Dir:  workspace,
	}, nil
}

// agyStep is one `step_update` payload.
//
// ToolName and ToolInfo were on the wire from the first capture and were not
// being read — the adapter rendered StepType, the literal string "tool", for
// every call agy made. That is the Cursor `tool_call.tool.case` miss in a
// second costume (ADR-008, tenth amendment): the fields the vendor sends and
// the fields the parser looks at were never compared against a captured line.
type agyStep struct {
	ConversationID string `json:"conversation_id"`
	State          string `json:"state"`
	StepType       string `json:"step_type"`
	TextDelta      string `json:"text_delta"`
	// StepIndex is what pairs a step's ACTIVE report with its DONE report:
	// captured output shows the same index on both (step_index 2 ACTIVE
	// carrying the reply text, step_index 2 DONE carrying the trailing
	// newline). A POINTER because 0 is a real index — the first step of a
	// turn is `user_input` at index 0 — and a plain int could not tell that
	// apart from a line with no index at all.
	StepIndex *int `json:"step_index"`

	// ToolName is agy's real name for the call — `list_dir`, `run_command`,
	// `write_to_file`. Top level on the step, and duplicated inside tool_info;
	// both were present and agreed on every captured tool line, so the top-level
	// one is preferred and the nested one is the fallback rather than a guess.
	ToolName string `json:"tool_name"`

	ToolInfo struct {
		Name string `json:"name"`
		// Parameters is an arbitrary object with vendor-specific key names.
		// Captured: {"DirectoryPath":"…"} on list_dir, {"CommandLine":"pwsh
		// -Command \"…\""} on run_command, {"TargetFile":"C:\\probe.txt"} on
		// write_to_file. RawMessage per value because a parameter is not
		// necessarily a string, and a map[string]string would fail the whole
		// line's unmarshal and lose the activity entry over one odd field.
		Parameters map[string]json.RawMessage `json:"parameters"`
		// Error is present only on state ERROR. Captured:
		// {"type":"TOOL_ERROR","message":"error executing cascade step:
		// CORTEX_STEP_TYPE_RUN_COMMAND: granting access to C:\\: Access is
		// denied."}
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	} `json:"tool_info"`
}

// agyLine is the subset of agy's stream-json schema council models.
//
// The shape is its own, and notably not Claude's: the discriminator is "event"
// rather than "type", and the payload hangs off a key named after the event
// rather than sitting at the top level. The conversation id is top-level on
// init but nested inside step_update and result, so it is read from both places.
type agyLine struct {
	Event string `json:"event"`

	// init carries the id at the top level, alongside an "init" object holding
	// cwd, the tool list and permission_mode.
	ConversationID string `json:"conversation_id"`

	StepUpdate agyStep `json:"step_update"`

	Result struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
		// Error is the vendor's own sentence about why the TURN died, and it is
		// the field agy uses for that — `response` was empty on both captured
		// failing turns while this carried "Agent execution terminated due to
		// error." Load-bearing beyond the note it fills: the `error_message`
		// step kind is suppressed from the trace (agyPlumbing) on the grounds
		// that the turn-level failure is already reported through this path, and
		// that argument only holds if this path says something.
		Error string `json:"error"`
	} `json:"result"`
}

// agyPlumbing reports whether a step is agy narrating its own message-passing
// rather than the vendor ACTING.
//
// The honesty line this list is drawn along, because the two mistakes are not
// symmetrical: hiding a vendor's ACTIONS would be a false gauge — the room
// would show a quiet column for an agent that was busy editing the workspace.
// Hiding its PLUMBING is noise reduction. So this is an allowlist of kinds each
// defended on its own captured evidence, never a blanket "drop what looks
// noisy". Every line cited below is from the live captures of 2026-08-04
// (agy 1.1.10, Windows).
//
//   - user_input — step_index 0 of every turn, DONE, and nothing else on the
//     line: no tool name, no parameters, no duration. It is the brief council
//     itself just sent, echoed back. A room cannot inform a user by replaying
//     their own keystrokes to them.
//   - system_message — same empty shape (resume capture, step_index 12, DONE
//     and no other field). agy placing its own message into the conversation.
//   - checkpoint — carries duration_seconds and a ~120-token usage block and
//     nothing else. A conversation bookmark: it touches the thread, never the
//     workspace.
//   - error_message — an empty marker, again with no message, no error field
//     and no duration; it is the flag that a message of kind "error" was
//     placed in the conversation, not the error. Dropping it is the case that
//     needed the most care, since it appears on failing turns and losing the
//     only sign that a turn went wrong is the OPPOSITE mistake. It is safe
//     here for a checked reason rather than an assumed one: on both captured
//     failing turns the `result` event carried status "ERROR" plus
//     error "Agent execution terminated due to error.", and ParseEvent's
//     result branch turns that into a KindError whose note is that sentence.
//     The turn-level failure is reported, with words, by a different path. A
//     rendered `error_message ?` is strictly less informative than that note —
//     an ominous name with an Unknown outcome attached.
//
// unknown is the one that had to be argued rather than listed, because
// suppressing a step whose type this adapter merely does not RECOGNISE would be
// the same class of mistake as inventing an outcome for it. The captured
// evidence says it is agy's own label, not our ignorance: both non-resume turns
// carry exactly one, at step_index 1, immediately after user_input and before
// any model output, with no tool_name, no tool_info, no parameters, and a
// duration of 0.0005s (turn 1) and 0.0045s (control turn). Half a millisecond
// is not enough to do anything, and every step in every capture that DID do
// something carried a tool_name. So it is a fixed preamble slot agy declines to
// name.
//
// What would reverse that is written as code rather than left in this comment:
// an `unknown` step carrying a tool name — top level or nested — is NOT
// suppressed and renders under that name. If agy ever starts acting through
// this label, the trace shows it the same turn, without anyone re-reading this
// paragraph.
func agyPlumbing(su agyStep) bool {
	switch su.StepType {
	case "user_input", "system_message", "checkpoint", "error_message":
		return true
	case "unknown":
		return su.ToolName == "" && su.ToolInfo.Name == ""
	}
	return false
}

// agyWhat names one step for the trace, in the grammar the other three
// adapters already use: the tool's real name, then ": " and the one argument
// that identifies the call (`Glob: **/*.go`, `Bash: go test ./...`).
//
// A step with no tool name falls back to its step_type, which is what a future
// step kind this adapter has never seen should read as — agy's own word for it,
// not a guess.
func agyWhat(su agyStep) string {
	name := su.ToolName
	if name == "" {
		name = su.ToolInfo.Name
	}
	if name == "" {
		return su.StepType
	}
	if arg := agyToolArg(su.ToolInfo.Parameters); arg != "" {
		return name + ": " + arg
	}
	return name
}

// agyToolArg picks the one parameter worth showing in a 37-cell column.
//
// The rule, in full, because it has to be defensible rather than tuned:
//
//  1. Only STRING values are candidates. An object or an array is not a line,
//     and dumping a whole parameter blob into the trace would bury the thing
//     the trace exists to make readable.
//  2. Exactly one such parameter — which is every shape captured so far —
//     renders it. `list_dir` sends only DirectoryPath, `run_command` only
//     CommandLine, `write_to_file` only TargetFile.
//  3. Several, and the LOWEST KEY NAME by byte order wins.
//  4. None, and the entry is the bare tool name.
//
// Rule 3 is the one worth defending. agy's parameter keys are vendor-specific
// PascalCase and no captured tool sends two strings at once, so any claim about
// which key "matters" would be a guess dressed as a table — and a per-tool table
// is exactly what this declines to become. What rule 3 does guarantee is the
// property that actually matters: the answer never depends on Go's randomised
// map iteration order, so a rendered line and a golden cannot flicker between
// runs. `TestAgyToolArgIsDeterministic` pins that by parsing the same
// multi-parameter line repeatedly and demanding one answer.
func agyToolArg(params map[string]json.RawMessage) string {
	best, bestKey, found := "", "", false
	for k, raw := range params {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if !found || k < bestKey {
			best, bestKey, found = s, k, true
		}
	}
	return clipArg(best)
}

// agyStepEvent turns one step into an activity event.
//
// The id is the step index, which is scoped to the turn — and that is exactly
// the scope it needs, since council clears a column's trace on every dispatch,
// so index 3 of turn 2 can never collide with index 3 of turn 1. A step_update
// with no index carries no id, which leaves it permanently pending rather than
// letting it correlate with an unrelated step; no captured line is in that
// state.
func agyStepEvent(al agyLine, outcome runner.ActStatus, detail string) (runner.Event, bool) {
	id := ""
	if al.StepUpdate.StepIndex != nil {
		id = "step-" + strconv.Itoa(*al.StepUpdate.StepIndex)
	}
	return runner.Event{
		Kind: runner.KindActivity,
		Acts: []runner.ActCall{{
			ID:      id,
			Text:    agyWhat(al.StepUpdate),
			Outcome: outcome,
			Detail:  clipArg(firstLine(detail)),
		}},
	}, true
}

// ParseEvent maps agy's observed schema. Unknown lines are dropped rather than
// failing the turn.
//
// A note the room needs, because it changes what the column should render:
// agy's "text_delta" is not token-level. A whole agent_response arrives as ONE
// delta when the step flips to ACTIVE, plus a trailing "\n" when it flips to
// DONE. The measured effect is a column that sits completely empty for the full
// generation — 73 seconds on a one-word reply — and then fills in a single
// paint. That is a PhaseWaiting case, not a streaming one.
func (Antigravity) ParseEvent(line []byte) (runner.Event, bool) {
	if len(line) == 0 || line[0] != '{' {
		return runner.Event{}, false
	}
	var al agyLine
	if err := json.Unmarshal(line, &al); err != nil {
		return runner.Event{}, false
	}

	switch al.Event {
	case "init":
		// The id that makes the next turn a --conversation resume.
		if al.ConversationID != "" {
			return runner.Event{Kind: runner.KindSession, SessionID: al.ConversationID}, true
		}
	case "step_update":
		// Conversation plumbing never becomes an act. Done HERE rather than in
		// the renderer on purpose: council keeps Render pure over State, so a
		// step that is not an action must never reach State in the first place.
		// The per-kind justification is agyPlumbing's own comment, and the line
		// it holds is that hiding a vendor's ACTIONS would be a false gauge
		// while hiding its plumbing is noise reduction.
		if agyPlumbing(al.StepUpdate) {
			return runner.Event{}, false
		}
		// Gated on agent_response deliberately. Other step types seen in real
		// output (tool, checkpoint, user_input, system_message, "unknown") carry
		// tool parameters and command output, and none of them carried a
		// text_delta in any captured run — routing them to the column would
		// render tool chatter as the vendor's answer. If a future step type does
		// carry assistant text, the result event's full response still backstops
		// it, which is what makes the strict gate safe rather than lossy.
		//
		// Both ACTIVE and DONE are accepted: they carry DIFFERENT deltas (the
		// text on ACTIVE, the trailing newline on DONE), so taking only one state
		// would silently drop half of a reply.
		if al.StepUpdate.StepType == "agent_response" && al.StepUpdate.TextDelta != "" {
			return runner.Event{Kind: runner.KindText, Text: al.StepUpdate.TextDelta}, true
		}
		// Every other step type is the vendor ACTING. It is surfaced as
		// activity rather than dropped, and the strict gate above is what makes
		// that safe: tool chatter cannot leak into the column's prose, because
		// the two are different event kinds the renderer draws differently.
		//
		// This matters more for agy than for anyone else. Its reply arrives in
		// one paint at the very end, so without activity its column is blank
		// for the entire turn — indistinguishable from a hung process.
		//
		// agent_response is excluded explicitly: it is speech, handled above,
		// and a response step carrying no delta is an empty message rather
		// than a step the vendor took.
		//
		// BOTH states are surfaced now, where this used to take ACTIVE only.
		// That was the right call while the trace was append-only — a step
		// reports twice and every line would have doubled — and it is the wrong
		// one now that the two reports can be correlated by step_index: ACTIVE
		// opens the entry, DONE resolves it, and the trace shows a step still
		// running as different from a step that has ended.
		if al.StepUpdate.StepType != "" && al.StepUpdate.StepType != "agent_response" {
			switch al.StepUpdate.State {
			case "ACTIVE":
				return agyStepEvent(al, runner.ActPending, "")
			case "ERROR":
				// A FIFTH state, and it was being dropped on the floor. Until
				// this landed the switch handled ACTIVE and DONE only, so a tool
				// call agy REFUSED stayed rendered as still-pending forever —
				// the trace claiming a command was running when the vendor had
				// already given up on it. That is a false gauge in the direction
				// this repo cares about most, and unlike the DONE case there is
				// nothing weak about the evidence: the line says ERROR and
				// carries the reason.
				//
				// Captured 2026-08-04 under --mode plan --sandbox:
				//
				//	"state":"ERROR","step_type":"tool","tool_name":"run_command",
				//	"tool_info":{…,"error":{"type":"TOOL_ERROR","message":"error
				//	executing cascade step: CORTEX_STEP_TYPE_RUN_COMMAND:
				//	granting access to C:\\: Access is denied."}}
				//
				// ActFailed rather than ActDenied, on the same reasoning the
				// Cursor adapter states: ActDenied is council's first-hand record
				// of its OWN gate keystroke, and a refusal read off a vendor's
				// stream is not that. The detail is the vendor's own first line,
				// never a sentence composed here (§9.6a).
				return agyStepEvent(al, runner.ActFailed, al.StepUpdate.ToolInfo.Error.Message)
			case "DONE":
				// UNKNOWN, not OK, and this is the whole point of that value
				// existing. Every captured DONE line carries duration_seconds,
				// sometimes tool_info with the call's parameters, and NOTHING
				// that says whether the step achieved anything — no status, no
				// exit code, no error field. agy reports success or failure
				// exactly once per turn, in the final `result` event, and that
				// verdict is about the TURN rather than about any one step.
				//
				// So a finished agy step is a step whose outcome has not been
				// observed, and it renders with its own neutral mark. Reusing
				// the success mark here would be this product inventing a
				// result on a vendor's behalf, which is the one thing it is
				// built not to do.
				//
				// Narrower than it was, in one direction only: agy DOES report
				// a per-step FAILURE, as state ERROR, handled above. Silence on
				// DONE is still silence.
				return agyStepEvent(al, runner.ActUnknown, "")
			}
			// Any other state is a value no captured run has shown. Dropped
			// rather than mapped: an unrecognised state is not evidence of
			// anything, and inventing an outcome for it is how a trace starts
			// lying quietly.
		}
	case "result":
		// Response is the whole final reply, carried as the fallback for a turn
		// that streamed nothing. For this vendor that fallback is load-bearing
		// rather than defensive, given the granularity noted above.
		//
		// CostUSD is left nil, always. agy reports token counts (input, output,
		// thinking, cache_read) and no monetary figure anywhere in its output.
		// Multiplying tokens by a remembered price is exactly the derived number
		// council refuses to display.
		ev := runner.Event{
			Kind:      runner.KindMeta,
			Text:      al.Result.Response,
			SessionID: al.Result.ConversationID,
		}
		// "SUCCESS" is the only status value observed. The test is therefore
		// "known-and-not-SUCCESS" rather than a guessed list of failure names,
		// and an absent status is treated as unknown rather than as a failure —
		// reporting a turn as failed on a field this adapter has never seen fail
		// would be inventing the diagnosis.
		if al.Result.Status != "" && al.Result.Status != "SUCCESS" {
			ev.Kind = runner.KindError
			// The vendor's own words, preferred in the order of how specifically
			// they diagnose the failure. `error` is the field agy uses to say
			// what went wrong and is the only one populated on both captured
			// failing turns ("Agent execution terminated due to error."), while
			// `response` on a failing turn is a partial ANSWER rather than a
			// diagnosis. The composed status sentence is the last resort.
			//
			// This is the sign of a failed turn that the suppressed
			// `error_message` step was the other, wordless half of.
			ev.Note = "the vendor reported status " + al.Result.Status
			switch {
			case al.Result.Error != "":
				ev.Note = al.Result.Error
			case al.Result.Response != "":
				ev.Note = al.Result.Response
			}
			ev.Failure = agyFailureClass(al.Result.Error)
		}
		return ev, true
	}
	return runner.Event{}, false
}

// agyFailureClass answers one question about a failed agy turn: does it say
// anything about the conversation the turn was resuming? (ADR-008, sixteenth.)
//
// One string qualifies, and it is quoted rather than paraphrased because it is
// the whole evidence for the case. MEASURED 2026-08-04, agy 1.1.10, Windows:
//
//	Eligibility check failed: UNAVAILABLE (code 503): The service is currently
//	unavailable.
//
// That capture arrived as a bare `result` with an EMPTY conversation_id — the
// turn died before a thread was involved at all — which is what makes "this
// claims nothing about the thread" a reading of the capture rather than an
// inference from the words. Matching is on the vendor's own sentence and not on
// the empty id, because a 503 raised on a turn that HAD reached a conversation
// would be exactly as transient; the empty id is the corroboration, not the
// test.
//
// Everything else agy fails with stays Unclassified, and the sentence it fails
// with most often is the reason. "Agent execution terminated due to error." was
// captured on a turn whose thread was demonstrably ALIVE (same conversation_id
// back, step_index 10 → 11, num_turns 2) and is also what a genuinely dead
// thread would plausibly produce. A string that appears on both sides of the
// distinction cannot be evidence for either, so it is not read as either.
func agyFailureClass(errText string) runner.FailureClass {
	low := strings.ToLower(errText)
	if strings.Contains(low, "eligibility check failed") &&
		strings.Contains(low, "the service is currently unavailable") {
		return runner.FailureVendorUnavailable
	}
	return runner.FailureUnclassified
}
