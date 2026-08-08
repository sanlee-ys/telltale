package vendors

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"

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
// This adapter was written twice, and the difference between the two versions
// is the point of the comments below.
//
// The first version could not run the CLI at all: the install reported "Not
// logged in", and it checks authentication BEFORE it parses flags, so every
// probe returned the same auth error. Its facts came from `--help` and from
// reading the shipped JavaScript bundle — better than docs, weaker than a
// measurement, and labelled as such everywhere.
//
// On 2026-08-04 the install was signed in and four turns ran against
// 2026.07.23-e383d2b. Three of the bundle-derived facts survived unchanged, and
// three did not:
//
//   - The tool_call discriminator is NOT `tool_call.tool.case`. That is the
//     bundle's internal protobuf representation; on the wire the oneof is
//     flattened to a key, `tool_call.readToolCall` / `.shellToolCall` / etc.
//     The old parser matched nothing and every trace entry read "tool call".
//   - Assistant deltas are followed by a repeat of the WHOLE message. The old
//     parser concatenated both and would have rendered "PONGPONG".
//   - `--sandbox enabled` does not weakly apply on Windows; it kills the turn.
//
// Which is the actual lesson, and it is not "the bundle was wrong". The bundle
// was right about what the program constructs. It could not be right about what
// arrives at the other end of a pipe, and that is the only thing a parser
// consumes. Each comment below now says which of the two it rests on.
type Cursor struct{}

// CursorNodeBundle is the JavaScript entry point a node interpreter has to be
// handed to become cursor-agent, or "" when this path is not a node at all.
//
// Exported because detection and this adapter must not derive it separately.
// Detection resolves the seat's binary to the node the vendor's own .cmd
// launcher would have run and checks that this file sits beside it; the
// invocation below puts that same file in argv[1]. Two copies of one
// filepath.Join is exactly the kind of agreement that silently stops holding.
//
// The sibling relationship is the launcher's, not this repo's: cursor-agent.ps1
// runs `& "$dir\node.exe" "$dir\index.js" $args` in both of its branches.
func CursorNodeBundle(binary string) string {
	base := strings.ToLower(filepath.Base(binary))
	if strings.TrimSuffix(base, filepath.Ext(base)) != "node" {
		return ""
	}
	return filepath.Join(filepath.Dir(binary), "index.js")
}

// Registration lives in vendor.go; this pins the interface at compile time.
var _ Vendor = Cursor{}

func (Cursor) ID() model.VendorID { return model.VendorCursor }

// baseArgs is the shared invocation, minus the prompt.
//
// Flag names are from `cursor-agent --help` on version 2026.07.23-e383d2b.
//
// Read posture is `--mode plan`, whose help reads "read-only/planning (analyze,
// propose plans, no edits)". It is REQUESTED and nothing more, and one live turn
// weakened even that: under `--mode plan` the agent selected and dispatched
// `cat …` and `ls -1` as shellToolCall invocations. A hook on the machine
// stopped them, so whether the mode itself would have refused them is still
// unobserved — but a "no edits" mode let a shell command get as far as the
// permission layer, and the badge says so.
//
// `--sandbox enabled` is passed ONLY off Windows, and that is a measurement
// rather than a portability nicety. On Windows the flag does not degrade, it
// aborts:
//
//	Error: Sandbox mode is enabled but not available on this system.
//	Sandbox requires macOS or Linux.
//
// exit 1, before any model call. Passing it there would have made every read
// posture turn fail — the flag that read as the stronger half of this posture
// was, on this OS, the reason the seat could not answer at all. Off Windows it
// is kept: a real cursorsandbox.exe ships in the install, and unlike here the
// flag at least does not refuse. What it covers is still unknown, and no badge
// may imply otherwise.
//
// Note what is NOT done about the Windows case: council does not pass
// `--sandbox disabled` to force its way past a user config that would fail the
// same way. Declining to ask for a restriction is council's business; reaching
// into someone's config to remove one is not. A user whose own config enables
// the sandbox on Windows gets a turn that fails with the vendor's own sentence,
// classified into an actionable card by runner.failureNote.
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
	return cursorBaseArgs(p, runtime.GOOS == "windows")
}

// cursorBaseArgs is baseArgs with the OS as an argument, so both branches are
// reachable from a test on either machine. The seat's Windows behaviour is the
// half that was measured; a test that could only run it on Windows would be the
// half nobody checks.
func cursorBaseArgs(p Posture, windows bool) []string {
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
		args = append(args, "--mode", "plan")
		if !windows {
			args = append(args, "--sandbox", "enabled")
		}
	}
	return args
}

// promptArgs appends the workspace and then the prompt, which must be last.
//
// The prompt is cursor-agent's variadic positional argument. That is not a
// preference: print mode's own guard in the bundle is
// `t.trim() || "Error: No prompt provided for print mode"`, where t is the
// joined argv. There is no `-` sentinel and no --prompt-file.
//
// This comment used to add "and no code path anywhere in the bundle reads the
// prompt from stdin". That was measured on 2026.07.23-e383d2b and is FALSE on
// 2026.08.04-aaa8809: a prompt piped in with no positional argument runs a
// normal turn. Nothing here depends on it — council always passes the prompt in
// argv, which is why this is a comment fix and not a code one — but the stale
// half is removed rather than left standing, because the next reader would have
// inherited it as a premise.
//
// What stdin CANNOT do is carry a second turn, and that is the load-bearing
// fact for anyone eyeing this seat for §9.8-style persistence: print mode
// drains stdin to EOF and joins the whole of it into ONE prompt. Measured — a
// turn written to a held-open stdin produced nothing for sixty seconds, and ran
// only once the pipe was closed. The EOF that starts the turn is the EOF that
// ends the channel. §9.33 has the numbers and names the seam that does work.
//
// The bare "--" separator in front of it is now SETTLED, and it was left open
// as "run both forms once" precisely because getting it wrong breaks every
// brief rather than a rare one. Both forms were run on 2026-08-04:
//
//	… --workspace <ws> "--seriously reply with OK"
//	    → error: unknown option '--seriously reply with OK'
//	… --workspace <ws> -- "--seriously reply with OK"
//	    → a normal turn, result "OK"
//
// So a brief opening with "-" really would have died, the separator really is
// the fix, and it costs an ordinary brief nothing — the second form was run on
// dash-free prompts too and behaved identically.
func (c Cursor) promptArgs(prompt, workspace string, p Posture) []string {
	args := c.baseArgs(p)
	if workspace != "" {
		// Defaults to the working directory, which runner already sets from
		// Dir, so this is belt and braces against a saved workspace default in
		// the user's config winning instead.
		args = append(args, "--workspace", workspace)
	}
	return append(args, "--", prompt)
}

// nodeArgs prepends the JavaScript entry point when the resolved binary is a
// node interpreter rather than cursor-agent itself.
//
// Both cases are live. On Windows detection resolves this seat to the bundled
// node.exe, stepping over a .cmd launcher whose only job was to do the same
// thing; on macOS and Linux it resolves to cursor-agent, which needs no bundle.
// The adapter reads which case it is off the path it was handed rather than
// off runtime.GOOS, because an override (TELLTALE_COUNCIL_CURSOR_BIN) can put
// either shape on either OS.
func cursorArgs(binary string, rest []string) []string {
	bundle := CursorNodeBundle(binary)
	if bundle == "" {
		return rest
	}
	return append([]string{bundle}, rest...)
}

func (c Cursor) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   cursorArgs(binary, c.promptArgs(prompt, workspace, p)),
		// StdinPrompt stays empty: this CLI does not read the prompt from
		// stdin, and that has not changed. What changed is that it no longer
		// costs the seat anything — runner.ErrShellShimWithArgvPrompt refuses
		// an argv prompt on a .cmd, and detection now hands this adapter the
		// native node.exe that .cmd would have run, so no shell is in the
		// invocation for the rule to fire on.
		Dir: workspace,
	}, nil
}

// NextTurn resumes the vendor's own chat.
//
// VERIFIED on 2026-08-04, and this was the gap most worth closing: that the
// `session_id` on the event stream IS the id `--resume` wants back. Turn one
// answered "PONG" on session 6164d06a-…; turn two, invoked with `--resume
// 6164d06a-…`, was asked what word it had just said and answered "I said
// PONG.", re-reporting the same session_id on its own init event. Round trip
// closed: a real resume, not a re-send, and not a fresh conversation.
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
		Args:   cursorArgs(binary, append(args, "--", prompt)),
		Dir:    workspace,
	}, nil
}

// cursorLine is the subset of cursor-agent's stream-json schema council models.
//
// CAPTURED, on 2026-08-04, from four live turns. These are lines off the wire,
// abridged only where noted:
//
//	{"type":"system","subtype":"init","apiKeySource":"login","cwd":"C:\\...",
//	 "session_id":"6164d06a-…","model":"Auto","permissionMode":"default"}
//	{"type":"user","message":{"role":"user","content":[{"type":"text","text":"…"}]},"session_id":"…"}
//	{"type":"thinking","subtype":"delta","text":"The response will be","session_id":"…","timestamp_ms":…}
//	{"type":"thinking","subtype":"completed","session_id":"…","timestamp_ms":…}
//	{"type":"assistant","message":{…,"content":[{"type":"text","text":"P"}]},"session_id":"…","timestamp_ms":…}
//	{"type":"assistant","message":{…,"content":[{"type":"text","text":"ONG"}]},"session_id":"…","timestamp_ms":…}
//	{"type":"assistant","message":{…,"content":[{"type":"text","text":"PONG"}]},"session_id":"…"}
//	{"type":"tool_call","subtype":"started","call_id":"call-ab9b…","tool_call":{"readToolCall":{"args":{"path":"C:\\…"}},…},"session_id":"…"}
//	{"type":"tool_call","subtype":"completed","call_id":"call-ab9b…","tool_call":{"readToolCall":{"result":{"error":{"errorMessage":"…"}}},…},"session_id":"…"}
//	{"type":"result","subtype":"success","duration_ms":7827,"is_error":false,
//	 "result":"PONG","session_id":"…","request_id":"…","usage":{"inputTokens":22862,"outputTokens":57,…}}
//
// A SECOND capture, the same day, from a turn asked to speak, run a tool, and
// speak again. It is the one that matters for the whole-message repeat, because
// a turn with a tool in it is several model calls and each one ends in a repeat
// of its OWN segment. testdata/cursor-segmented-turn.jsonl is that turn in full,
// redacted; these are its shape-bearing lines:
//
//	{"type":"assistant","message":{…,"text":"Beginning"}]},"session_id":"…","timestamp_ms":1785894418573}
//	… seven more deltas, none carrying model_call_id …
//	{"type":"assistant","message":{…,"text":"Beginning the survey of this repository now."}]},
//	 "session_id":"…","model_call_id":"88fa1494-…-0-x7su","timestamp_ms":1785894419785}
//	{"type":"tool_call","subtype":"started",…,"model_call_id":"88fa1494-…-0-x7su",…}
//
// and the turn's final assistant event carries NEITHER model_call_id nor
// timestamp_ms, exactly as the PONG capture said. The `result` at the end
// carried all three segments concatenated.
//
// Three absences are deliberate:
//
//   - No cost field. `usage` carries token counts only — measured across all
//     four turns, and the bundle has no monetary figure anywhere — so CostUSD
//     stays nil for this vendor forever. Deriving dollars from tokens is on
//     this repo's rejected list.
//   - permissionMode is parsed by nobody. It is a hardcoded "default" literal
//     in the bundle, and the capture confirms it: all four turns reported
//     "default" regardless of the flags they were given. The technique that
//     caught Claude's --allowedTools — run it and read what the session says
//     about itself — returns a constant on this vendor.
//   - `thinking` is dropped, both subtypes. It is reasoning, so it is neither
//     the vendor's answer nor a thing the vendor DID; routing it to the body
//     would pad the column with commentary the user did not ask for, and
//     routing it to the trace would file it as a tool call. It was already
//     dropped when it was a hypothesis; it is still dropped now that it has
//     been seen.
type cursorLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`

	SessionID string `json:"session_id"`

	// TimestampMS separates a text delta from the whole-message repeat that ends
	// a turn, and it is a POINTER because its ABSENCE is the signal.
	//
	// The original capture (2026-08-04, "PONG") saw two deltas carrying
	// timestamp_ms followed by a whole-message event carrying none, and this
	// field alone was made the discriminator. That was TOO NARROW, and the
	// second capture the same day says so: it is only true of the whole-message
	// repeat that ends the TURN. A turn that runs a tool is cut into several
	// model calls, each of which ends in its own whole-message repeat, and those
	// mid-turn repeats DO carry timestamp_ms:
	//
	//	…"text":"Beginning"…,"timestamp_ms":1785894418573}
	//	… seven more deltas …
	//	…"text":"Beginning the survey of this repository now."…,
	//	  "model_call_id":"88fa1494-…-0-x7su","timestamp_ms":1785894419785}
	//
	// So this field is now the SECOND line of defence, covering the final
	// repeat, and ModelCallID is the first. Neither is load-bearing on its own.
	//
	// The safety net if upstream changes both is unchanged and is not
	// theoretical: the `result` event carries the entire reply — measured, on
	// the segmented capture, as every segment concatenated — and the room uses
	// it whenever a column streamed nothing. The failure mode of these fields
	// disappearing is a column that fills at the end instead of incrementally,
	// not a column that is wrong or empty.
	TimestampMS *int64 `json:"timestamp_ms"`

	// ModelCallID names the model call an event belongs to, and its PRESENCE on
	// an assistant event is the whole-message repeat.
	//
	// CAPTURED on 2026-08-04 from a three-segment turn: across all 108 assistant
	// events, every text delta carried NO model_call_id, and the two mid-turn
	// whole-message repeats each carried one ("…-0-x7su" and "…-1-15l2" — the
	// index in the suffix is the segment). The trailing digit-suffixed shape is
	// the vendor's own numbering of the calls in the turn, and the same ids
	// appear on the tool_call events that separate the segments.
	//
	// This is a better discriminator than an absent timestamp for the reason
	// absence is always the weaker signal: a field that is missing cannot
	// distinguish "the vendor is telling me this is a complete message" from
	// "the vendor stopped sending a field". model_call_id is the vendor
	// asserting which model call this text is the completed form of, and deltas
	// — which are fragments of a call still in flight — have nothing to assert.
	ModelCallID string `json:"model_call_id"`

	Message struct {
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`

	// CallID correlates a tool call's announcement with its result.
	//
	// One oddity, recorded because it looks like corruption and is not: these
	// ids contain a literal newline —
	// "call-ab9b3fba-…-0\nfc_88e24da8-…_0". Both halves of a call carry the
	// identical string, so correlation is unaffected, and the id is never
	// rendered.
	CallID string `json:"call_id"`

	// ToolCall is a protobuf oneof, and the wire form is NOT what the bundle's
	// internal representation suggested. Reading the bundle found it testing
	// `"shellToolCall" === e.tool.case`, so the first version of this adapter
	// looked for tool_call.tool.case; the capture shows the oneof flattened to
	// an object KEY instead:
	//
	//	"tool_call":{"readToolCall":{"args":{"path":"…"}},"toolCallId":"…"}
	//	"tool_call":{"shellToolCall":{"args":{"command":"ls -1",…}},…}
	//
	// Held as raw messages so the key can be found before its value is decoded.
	// The alternative — a struct with a field per tool — would silently render
	// nothing the first time upstream adds a tool, which for a trace is the
	// same failure as being wrong.
	ToolCall map[string]json.RawMessage `json:"tool_call"`

	// result
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// cursorToolCall is one tool call's payload, whichever tool it belongs to.
//
// The arg fields are the ones captured: `command` on shellToolCall, `path` on
// readToolCall and grepToolCall, `pattern` on grepToolCall, `targetDirectory`
// on globToolCall. A tool carrying none of them still names itself.
//
// Result is the oneof the bundle declares as {success, error, rejected} — read
// from its own field descriptors, `{no:1,name:"success",kind:"message",
// oneof:"result"}` — of which `error` and `rejected` were both captured live
// and `success` was not, because every tool call on this machine was stopped by
// a hook. All three are RAW: `error` was seen as an object in two different
// shapes and the bundle also declares scalar-string forms of it elsewhere, and
// a struct that guessed wrong would fail the whole line's unmarshal and lose
// the activity entry rather than just its detail.
type cursorToolCall struct {
	Args struct {
		Command         string `json:"command"`
		Pattern         string `json:"pattern"`
		Path            string `json:"path"`
		TargetDirectory string `json:"targetDirectory"`
	} `json:"args"`
	Result *struct {
		Success  json.RawMessage `json:"success"`
		Error    json.RawMessage `json:"error"`
		Rejected json.RawMessage `json:"rejected"`
	} `json:"result"`
}

// cursorAct turns one tool_call line into a trace entry.
//
// The tool is found by the single key under tool_call whose name ends in
// "ToolCall" — the oneof discriminator, as it appears on the wire. Anything
// else there (`toolCallId`, `hookAdditionalContexts`, `startedAtMs`,
// `completedAtMs`, all captured) is metadata about the call rather than the
// call, and is skipped by that suffix rather than by an allow-list, so a tool
// this repo has never seen still names itself correctly.
func cursorAct(cl cursorLine) (runner.ActCall, bool) {
	name, payload := "", json.RawMessage(nil)
	for k, v := range cl.ToolCall {
		if strings.HasSuffix(k, "ToolCall") {
			name, payload = strings.TrimSuffix(k, "ToolCall"), v
			break
		}
	}
	if name == "" {
		return runner.ActCall{}, false
	}

	act := runner.ActCall{ID: cl.CallID, Text: name}
	var tc cursorToolCall
	if err := json.Unmarshal(payload, &tc); err != nil {
		// The tool is named and its payload is not a shape this adapter models.
		// The entry still lands: a column that went quiet during the part of the
		// turn it was busiest reads as hung, and "read" alone is a true and
		// useful line.
		return act, true
	}

	// Whichever argument this tool actually carries, most informative first.
	// Nothing is composed here — each of these is a field the vendor sent.
	for _, arg := range []string{
		tc.Args.Command, tc.Args.Pattern, tc.Args.Path, tc.Args.TargetDirectory,
	} {
		if arg != "" {
			act.Text = name + ": " + clipArg(arg)
			break
		}
	}

	if cl.Subtype != "completed" {
		// An announcement. Outcome stays ActPending, which is what makes a
		// running call visible before it resolves.
		return act, true
	}
	if tc.Result == nil {
		// It ended and said nothing about how. ActUnknown rather than ActOK:
		// inventing a success on a vendor's behalf is the one thing this trace
		// is built not to do.
		act.Outcome = runner.ActUnknown
		return act, true
	}
	switch {
	case len(tc.Result.Rejected) > 0:
		// A refusal by the vendor's own permission layer or by a user hook.
		// ActFailed, deliberately NOT ActDenied: that value is council's record
		// of ITS OWN gate keystroke, first-hand, and a refusal council merely
		// read off a stream is not that. Captured as
		// {"rejected":{"command":"ls -1","reason":"Hook blocked with message: …"}}.
		act.Outcome = runner.ActFailed
		act.Detail = clipArg(firstLine(cursorFailText(tc.Result.Rejected)))
	case len(tc.Result.Error) > 0:
		// Captured in two shapes on the same stream — {"errorMessage":"…"} from
		// readToolCall and {"error":"…"} from grepToolCall and globToolCall —
		// which is why the text is dug out rather than declared.
		act.Outcome = runner.ActFailed
		act.Detail = clipArg(firstLine(cursorFailText(tc.Result.Error)))
	case len(tc.Result.Success) > 0:
		act.Outcome = runner.ActOK
	default:
		act.Outcome = runner.ActUnknown
	}
	return act, true
}

// cursorFailText digs the vendor's own words out of a failure payload.
//
// Three keys, all observed: `reason` on a rejection, `errorMessage` and `error`
// on an error. A bare string is accepted too, because the bundle declares
// scalar forms of the same field on other tools. Anything else yields "", and
// the entry renders as a failure with no detail rather than with a shape name.
func cursorFailText(raw json.RawMessage) string {
	var obj struct {
		Reason       string `json:"reason"`
		ErrorMessage string `json:"errorMessage"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		for _, s := range []string{obj.Reason, obj.ErrorMessage, obj.Error} {
			if s != "" {
				return s
			}
		}
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
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
		// The vendor speaking, one event per text chunk — measured token-level,
		// which is what earns this column GranTokens.
		//
		// The whole-message repeat is dropped HERE, and it has to be dropped
		// somewhere: cursor-agent sends a model call's deltas and then that
		// call's complete message, so appending both renders the passage twice.
		//
		// TWO tests, not one, because there are two of these events and they do
		// not look alike. A repeat that ends a mid-turn model call carries
		// model_call_id; the one that ends the turn carries neither that nor
		// timestamp_ms. Dropping on the first condition is what fixes the "X X Y"
		// a live segmented turn renders; keeping the second is the belt that has
		// held since the PONGPONG capture. See cursorLine.ModelCallID and
		// cursorLine.TimestampMS for the lines off the wire.
		if cl.ModelCallID != "" || cl.TimestampMS == nil {
			return runner.Event{}, false
		}
		// Emitted raw and unseparated: the chunks concatenate into the reply
		// exactly as sent ("I" + " said" + " P" + "ONG" + "."), and any
		// separator this adapter added would be a character the vendor did not
		// write.
		if t := cl.text(); t != "" {
			return runner.Event{Kind: runner.KindText, Text: t}, true
		}

	case "user":
		// Dropped, deliberately and with a comment because this one is a trap:
		// print mode echoes council's OWN prompt back as a user event on the
		// same stream. Rendering it would put the brief into the column as
		// though the vendor had said it. Confirmed live — every captured turn
		// opened with the brief coming straight back.

	case "tool_call":
		// What the vendor DID, never what it said.
		//
		// BOTH halves are taken now, where this used to take "started" only.
		// That was right while the trace was append-only and every call would
		// have doubled; it is wrong now that the two reports correlate by
		// call_id — "started" opens the entry and "completed" resolves it with
		// the vendor's own outcome, so a running command reads differently from
		// one that failed. The same correction agy's adapter took.
		//
		// A "completed" with no matching "started" is real and was captured (a
		// taskToolCall arrived resolved, with no announcement); the room's
		// recordAct lands it as an already-finished entry rather than dropping
		// it.
		if act, ok := cursorAct(cl); ok {
			return runner.Event{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}, true
		}
		// A tool call whose discriminator is not there at all is still a thing
		// that happened, and a silent drop would leave the column looking idle
		// during the part of the turn it was busiest.
		return runner.Event{
			Kind: runner.KindActivity,
			Acts: []runner.ActCall{{ID: cl.CallID, Text: "tool call"}},
		}, true

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
