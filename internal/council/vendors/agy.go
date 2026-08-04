package vendors

import (
	"encoding/json"
	"strconv"

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
// about the filesystem; whether it restricts the shell was NOT tested and is not
// claimed here either.
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

	StepUpdate struct {
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
	} `json:"step_update"`

	Result struct {
		ConversationID string `json:"conversation_id"`
		Status         string `json:"status"`
		Response       string `json:"response"`
	} `json:"result"`
}

// agyStepEvent turns one tool step into an activity event.
//
// The id is the step index, which is scoped to the turn — and that is exactly
// the scope it needs, since council clears a column's trace on every dispatch,
// so index 3 of turn 2 can never collide with index 3 of turn 1. A step_update
// with no index carries no id, which leaves it permanently pending rather than
// letting it correlate with an unrelated step; no captured line is in that
// state.
func agyStepEvent(al agyLine, outcome runner.ActStatus) (runner.Event, bool) {
	id := ""
	if al.StepUpdate.StepIndex != nil {
		id = "step-" + strconv.Itoa(*al.StepUpdate.StepIndex)
	}
	return runner.Event{
		Kind: runner.KindActivity,
		Acts: []runner.ActCall{{
			ID: id,
			// The step TYPE is all agy offers here that fits a narrow column.
			// tool_name and tool_info.parameters are also present on tool
			// steps and would read far better; wiring them is a separate
			// change from carrying outcomes, and is not smuggled in here.
			Text:    al.StepUpdate.StepType,
			Outcome: outcome,
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
				return agyStepEvent(al, runner.ActPending)
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
				return agyStepEvent(al, runner.ActUnknown)
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
			ev.Note = "the vendor reported status " + al.Result.Status
			if al.Result.Response != "" {
				ev.Note = al.Result.Response
			}
		}
		return ev, true
	}
	return runner.Event{}, false
}
