package vendors

import (
	"encoding/json"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Cursor drives cursor-agent, Cursor's headless CLI, in print mode.
//
// ADR-008 §7 scoped this seat out of v1 because cursor-agent was "not
// installed". That was false — it was installed at
// %LOCALAPPDATA%\cursor-agent\cursor-agent.cmd the whole time, just absent from
// the PATH of the shell that ran the check. The detection fix is in detect.go;
// this file is the adapter the ADR said would drop in when it turned up.
//
// The verification story for this vendor is DIFFERENT from every other adapter
// here, and the difference has to be stated before any of the comments below
// are read as equivalent to Codex's or Antigravity's:
//
//	The installed cursor-agent reports "Not logged in", and it checks
//	authentication BEFORE it parses flags. Every probe — a good invocation, a
//	deliberately invalid flag combination, a prompt on stdin — returned the
//	same authentication error. So nothing here was confirmed by running a
//	turn.
//
// What replaced the live run is the next best evidence available, and it is
// better than the docs this repo has twice been burned by: the CLI's own
// `--help`, and the shipped JavaScript bundle that implements print mode. Where
// a fact below comes from the bundle it says so. Where nothing establishes a
// fact, the code refuses to claim one rather than picking the likely answer:
// the sandbox badge says "requested", the granularity says nothing at all.
type Cursor struct{}

// Registration lives in vendor.go; this pins the interface at compile time.
var _ Vendor = Cursor{}

func (Cursor) ID() model.VendorID { return model.VendorCursor }

// baseArgs is the shared invocation, minus the prompt.
//
// Flag names are from `cursor-agent --help` on version 2026.07.23-e383d2b.
//
// Read posture is `--mode plan`, whose help reads "read-only/planning (analyze,
// propose plans, no edits)". It is REQUESTED and nothing more. The same help
// text says `-p` print mode "Has access to all tools, including write and
// shell", so the vendor itself describes the mode council runs in as
// unrestricted by default — everything rests on --mode plan being honoured, and
// that could not be tested. `--sandbox enabled` is passed alongside it: the
// install ships a real cursorsandbox.exe, so the mechanism at least exists, but
// what it covers is unknown and no badge may imply otherwise.
//
// Four flags are deliberately never passed, in EITHER posture:
//
//   - `-f/--force` and `--yolo` ("Force allow commands unless explicitly
//     denied" / "Run Everything"). This is the skip-permissions class of flag,
//     and the rule is the same one that keeps --dangerously-skip-permissions
//     out of the Antigravity adapter: --write asks to widen the workspace, not
//     to pre-approve everything a model might try.
//   - `--approve-mcps`. MCP servers reach OUTSIDE the directory council was
//     pointed at, which is the boundary --write widens. Auto-approving them is
//     a different grant, and the Claude adapter drops MCP entirely for exactly
//     this reason.
//   - `--trust` ("Trust the current workspace without prompting"). The honest
//     consequence is stated rather than papered over: an untrusted workspace
//     may block a print-mode turn with nobody to answer the prompt, and the
//     column will sit there. Trusting a directory on the user's behalf is the
//     user's call, made once in their own terminal.
func (Cursor) baseArgs(p Posture) []string {
	args := []string{
		// Non-interactive. Boolean, unlike agy's -p, which is a string flag
		// whose value is the prompt — the two CLIs use the same letter for
		// different things and the mistake is silent in agy's direction.
		"-p",
		"--output-format", "stream-json",
		// Asks for text deltas. Requested, not verified: see granularityFor in
		// detect.go for why this does not buy a streaming claim. The bundle
		// rejects this flag unless the format is stream-json, so the two belong
		// together.
		"--stream-partial-output",
	}
	if p == PostureRead {
		args = append(args, "--mode", "plan", "--sandbox", "enabled")
	}
	return args
}

// promptArgs appends the workspace and then the prompt, which must be last.
//
// The prompt is cursor-agent's variadic positional argument. That is not a
// preference: print mode's own guard in the bundle is
// `t.trim() || "Error: No prompt provided for print mode"`, where t is the
// joined argv, and no code path anywhere in the bundle reads the prompt from
// stdin. There is no `-` sentinel and no --prompt-file.
//
// UNRESOLVED, and recorded rather than guessed at: a brief whose first
// character is "-" would be parsed as an unknown option. The framework's usual
// answer is a bare "--" separator before the positional, but that is inferred
// from the argument parser rather than observed, and getting it wrong breaks
// EVERY brief instead of the rare one. Left out until someone with an
// authenticated CLI can run both forms once.
func (c Cursor) promptArgs(prompt, workspace string, p Posture) []string {
	args := c.baseArgs(p)
	if workspace != "" {
		// Defaults to the working directory, which runner already sets from
		// Dir, so this is belt and braces against a saved workspace default in
		// the user's config winning instead.
		args = append(args, "--workspace", workspace)
	}
	return append(args, prompt)
}

func (c Cursor) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   c.promptArgs(prompt, workspace, p),
		// StdinPrompt stays empty: this CLI does not read the prompt from
		// stdin. On Windows that combination is exactly what
		// runner.ErrShellShimWithArgvPrompt refuses, which is why detection
		// marks the seat unusable there rather than letting the refusal surface
		// as a failed turn.
		Dir: workspace,
	}, nil
}

// NextTurn resumes the vendor's own chat.
//
// `--resume [chatId]` is in `--help`, and the bundle's handling of a string
// value is a chat id that becomes `{kind:"resume", sessionId}`. The id council
// passes is the `session_id` every print-mode event carries.
//
// UNVERIFIED, and the gap is specifically this: that the `session_id` on the
// event stream IS the id `--resume` wants back. Both come from the same
// variable in the bundle, which is why this is implemented rather than stubbed
// out — but the round trip was never run, because no turn can run here. If the
// two ids turn out to differ, a follow-up turn fails visibly with the vendor's
// own error on its card; it does not silently start a fresh conversation.
//
// One nearby trap, not hit but worth recording: --resume also accepts a
// relative form, `-N` for the Nth most recent chat. A session id that happened
// to look like "-1" would be read as an index instead. Cursor's ids are UUIDs,
// so this is a note rather than a guard.
func (c Cursor) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	args := c.baseArgs(p)
	args = append(args, "--resume", sessionID)
	if workspace != "" {
		args = append(args, "--workspace", workspace)
	}
	// Only the new turn is sent; cursor-agent replays its own history.
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   append(args, prompt),
		Dir:    workspace,
	}, nil
}

// cursorLine is the subset of cursor-agent's stream-json schema council models.
//
// Read out of the shipped bundle's own emit calls rather than captured from a
// run, since no run is possible here. Each line below is the object the bundle
// literally constructs and JSON-stringifies to stdout:
//
//	{"type":"system","subtype":"init","apiKeySource":"login","cwd":"...",
//	 "session_id":"...","model":"...","permissionMode":"default"}
//	{"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]},"session_id":"..."}
//	{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"..."}]},"session_id":"..."}
//	{"type":"tool_call","subtype":"started","call_id":"...","tool_call":{...},"session_id":"..."}
//	{"type":"result","subtype":"success","is_error":false,"duration_ms":0,
//	 "result":"<full text>","session_id":"...","request_id":"...","usage":{...}}
//
// Two absences are deliberate:
//
//   - No cost field. `usage` carries token counts only — the bundle has no
//     monetary figure anywhere — so CostUSD stays nil for this vendor forever.
//     Deriving dollars from tokens is on this repo's rejected list.
//   - permissionMode is parsed by nobody. It is a hardcoded "default" literal
//     in the bundle, not a readout of the session, so reading it would produce
//     a number-shaped nothing. This is worth naming because the technique that
//     caught Claude's --allowedTools was exactly "run it and read what the
//     session reports about itself", and on this vendor that technique returns
//     a constant.
type cursorLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	SessionID string `json:"session_id"`

	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`

	// ToolCall's inner shape is a protobuf-style oneof. `tool.case` is the
	// discriminator the bundle itself reads — it tests `"shellToolCall" ===
	// e.tool.case` — so the name is evidence rather than a guess, while the
	// value side is left unmodelled because nothing establishes its shape.
	ToolCall struct {
		Tool struct {
			Case string `json:"case"`
		} `json:"tool"`
	} `json:"tool_call"`

	// result
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// text joins the content blocks of one message.
func (cl cursorLine) text() string {
	var s string
	for _, c := range cl.Message.Content {
		if c.Type == "text" {
			s += c.Text
		}
	}
	return s
}

// ParseEvent maps cursor-agent's print-mode schema. Unknown lines are dropped
// rather than failing the turn, so a future event type cannot break the column.
func (Cursor) ParseEvent(line []byte) (runner.Event, bool) {
	if len(line) == 0 || line[0] != '{' {
		return runner.Event{}, false
	}
	var cl cursorLine
	if err := json.Unmarshal(line, &cl); err != nil {
		return runner.Event{}, false
	}

	switch cl.Type {
	case "system":
		// The id that makes the next turn a --resume. Gated on the init subtype:
		// every event carries session_id, including task_notification, and
		// council wants the one place it is first announced rather than a
		// re-assertion on every line.
		if cl.Subtype == "init" && cl.SessionID != "" {
			return runner.Event{Kind: runner.KindSession, SessionID: cl.SessionID}, true
		}

	case "assistant":
		// The vendor speaking. With --stream-partial-output this is one event
		// per text chunk; without it, one per complete message. Emitted raw and
		// unseparated in both cases, because the chunked form concatenates and
		// the whole-message form is followed by the result event carrying the
		// same text — a separator would show up as a stray newline in one of
		// the two and there is no live capture to say which.
		if t := cl.text(); t != "" {
			return runner.Event{Kind: runner.KindText, Text: t}, true
		}

	case "user":
		// Dropped, deliberately and with a comment because this one is a trap:
		// print mode echoes council's OWN prompt back as a user event on the
		// same stream. Rendering it would put the brief into the column as
		// though the vendor had said it.

	case "tool_call":
		// What the vendor DID, never what it said. "started" only: the bundle
		// emits a matching "completed" for every call, and taking both would
		// double every line of the trace.
		if cl.Subtype == "started" {
			if name := cl.ToolCall.Tool.Case; name != "" {
				return runner.Event{Kind: runner.KindActivity, Text: name}, true
			}
			// A tool call whose discriminator did not parse is still a thing
			// that happened, and a silent drop would leave the column looking
			// idle during the part of the turn it was busiest.
			return runner.Event{Kind: runner.KindActivity, Text: "tool call"}, true
		}

	case "result":
		// End of turn. Result is the whole final reply, carried as the fallback
		// for a turn that streamed nothing — which, given that the streaming
		// claim for this vendor is unestablished, may well be every turn.
		//
		// is_error is parsed defensively. The only result the bundle was seen to
		// construct is subtype "success" with is_error hardcoded false; no
		// failure-emitting path was found, which suggests failures arrive on
		// stderr with a non-zero exit, where runner already handles them. If an
		// error result does exist upstream, this catches it rather than
		// rendering a failure as a normal answer.
		if cl.IsError {
			ev := runner.Event{Kind: runner.KindError, Note: "the vendor reported an error result"}
			if cl.Result != "" {
				ev.Note = cl.Result
			}
			return ev, true
		}
		return runner.Event{
			Kind:      runner.KindMeta,
			Text:      cl.Result,
			SessionID: cl.SessionID,
		}, true
	}
	return runner.Event{}, false
}
