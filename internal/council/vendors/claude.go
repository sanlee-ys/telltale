package vendors

import (
	"encoding/json"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Claude drives Claude Code in print mode.
//
// Flags verified against the installed binary's own --help rather than from
// memory, which is a standing rule and which earned its keep here: the design
// sketch for this adapter called the tool allowlist `--tools`, and no such flag
// exists. It is `--allowedTools`.
type Claude struct{}

func (Claude) ID() model.VendorID { return model.VendorClaude }

// deniedTools is council's read-only posture for this vendor.
//
// It is a DENY list, and that is not the design anyone would choose — it is
// what the CLI actually offers. `--allowedTools` sounds like the right flag and
// is not: it pre-approves tools for permission prompts, it does not remove them
// from the session. Verified against the live CLI on 2026-08-04 by reading the
// `system/init` event's own `tools` array, which with `--allowedTools
// Read,Glob,Grep` still listed Edit, Write and Bash. A `ro:tools` badge on top
// of that flag would have been exactly the false claim this ADR was amended to
// remove.
//
// What does work is `--disallowedTools` plus `--strict-mcp-config`. The same
// check with this list returns a session holding only read tools. Two things
// that list has to cover and that are easy to miss:
//
//   - PowerShell, not just Bash. Denying only Bash leaves a shell on Windows,
//     which is the platform this product targets.
//   - MCP servers, via --strict-mcp-config. Without it the session inherits
//     whatever the user has connected — the verification run surfaced Gmail
//     write tools — and no fixed deny list can name those in advance.
//
// The honest limitation, stated in the badge's Detail and in design.md §9.2: a
// deny list cannot cover a tool that does not exist yet. A future Claude Code
// release adding a write tool would appear in this session until this list is
// updated. That is why the claim is "these named tools are absent, verified",
// not "this session cannot write".
const deniedTools = "Edit,Write,NotebookEdit,Bash,PowerShell,Task,WebFetch,WebSearch," +
	"Artifact,SendMessage,RemoteTrigger,PushNotification,Workflow,Skill,ToolSearch," +
	"CronCreate,CronDelete,DesignSync,EnterWorktree,ExitWorktree," +
	"TaskCreate,TaskUpdate,TaskStop,ScheduleWakeup,Monitor"

func (c Claude) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   c.baseArgs(p),
		// The prompt goes on stdin, never in argv. Claude Code resolves to a
		// native .exe here so argv would technically be safe, but stdin also
		// sidesteps the ~32K Windows command-line limit, which a multi-turn
		// brief can reach.
		StdinPrompt: prompt,
		Dir:         workspace,
	}, nil
}

func (c Claude) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	return runner.Spec{
		Vendor:      c.ID(),
		Binary:      binary,
		Args:        append(c.baseArgs(p), "--resume", sessionID),
		StdinPrompt: prompt,
		Dir:         workspace,
	}, nil
}

func (c Claude) baseArgs(p Posture) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		// --verbose is required with stream-json in print mode; without it the
		// stream is suppressed and the column would sit silent for the whole
		// turn while the vendor worked normally.
		"--verbose",
		"--include-partial-messages",
		// --strict-mcp-config is kept in BOTH postures, deliberately. Write mode
		// widens what the vendor may do inside the workspace it was pointed at;
		// MCP servers reach OUTSIDE it — the verification run surfaced Gmail
		// write tools — and "may edit this worktree" is a different grant from
		// "may act on your accounts".
		"--strict-mcp-config",
	}
	if p == PostureWrite {
		// Verified live in a throwaway directory: with the deny list dropped
		// and acceptEdits set, print mode creates the file. Without a
		// permission mode it has nobody to ask and the turn stalls or refuses,
		// so this flag is what makes write mode functional rather than merely
		// unrestricted.
		return append(args, "--permission-mode", "acceptEdits")
	}
	return append(args, "--disallowedTools", deniedTools)
}

// toolCalls reads the tool_use blocks of one assistant message.
//
// One ActCall each rather than one joined string: a message really can carry
// several tool calls at once — a parallel batch is normal — and each one has to
// keep its own `id`, because that id is the only thing a later tool_result can
// be matched against.
func toolCalls(sl streamLine) []runner.ActCall {
	var out []runner.ActCall
	for _, b := range sl.Message.Content {
		if b.Type != "tool_use" || b.Name == "" {
			continue
		}
		call := runner.ActCall{ID: b.ID, Text: b.Name}
		if arg := toolArg(b.Input); arg != "" {
			call.Text = b.Name + ": " + arg
		}
		out = append(out, call)
	}
	return out
}

// toolResults reads the tool_result blocks of one `user`-type message, which is
// where Claude Code reports how a tool call it announced actually went.
//
// VERIFIED LIVE 2026-08-04, Claude Code 2.1.220, in a throwaway directory. Two
// of the captured lines, trimmed only of their trailing envelope fields:
//
//	{"type":"user","message":{"role":"user","content":[{"tool_use_id":"toolu_01A9758ERJJ2QGcKSeeDkeA1","type":"tool_result","content":"hi","is_error":false}]},...}
//	{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ls in '/nonexistent-xyz' was blocked. For security, ...","is_error":true,"tool_use_id":"toolu_01Uk7Xp2kguFuDtxT4ovaXE5"}]},...}
//
// Three things that run settled, each of which this parser depends on:
//
//   - The key ORDER differs between those two lines. Nothing may be inferred
//     from position; the id is read by name.
//   - `is_error` is ABSENT on some successes. A Read result came back as
//     `{"tool_use_id":...,"type":"tool_result","content":"1\talpha\n..."}` with
//     no is_error field at all, while the Bash success spelled it out as
//     false. So absence is success, not unknown — Claude Code marks failure
//     and stays silent about the rest.
//   - The results arrived OUT OF ORDER: the second call's failure landed
//     before the first call's success, on the very first probe rather than as
//     a theoretical worry. That is why correlation is by id and never by
//     arrival order.
//
// One honest limit: `is_error` is the harness's verdict on the tool call, not
// the command's exit status. The failure above was a permission refusal, not a
// non-zero exit. Both are "this call did not do what was asked", which is the
// claim the trace makes, and it is the only claim this field supports.
func toolResults(sl streamLine) []runner.ActCall {
	var out []runner.ActCall
	for _, b := range sl.Message.Content {
		if b.Type != "tool_result" || b.ToolUseID == "" {
			continue
		}
		res := runner.ActCall{ID: b.ToolUseID, Outcome: runner.ActOK}
		if b.IsError {
			res.Outcome = runner.ActFailed
			res.Detail = clipArg(firstLine(resultText(b.Content)))
		}
		out = append(out, res)
	}
	return out
}

// resultText pulls displayable text out of a tool_result's content field.
//
// A plain string in every line captured live — shell output, a Read's numbered
// lines, the blocked-command message. The Anthropic content-block schema also
// allows an ARRAY of blocks, which is how an image result would arrive; that
// shape was NOT observed here, so it is handled defensively and yields nothing
// when it carries no text block, rather than a guess at what it held.
func resultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			return b.Text
		}
	}
	return ""
}

// firstLine is the part of a multi-line failure a narrow column can show.
//
// Kept local to this package rather than shared with runner's identically named
// helper: they are the same three lines, and exporting one to reach the other
// would couple the adapter layer to the process layer for nothing.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// toolArg picks the one field of a tool's input worth showing in a trace.
//
// Ordered by how much it identifies the action: the command a shell ran tells
// you more than the file a read touched. Anything not listed renders as the
// bare tool name rather than a guess — dumping the whole input JSON into a
// narrow column would bury the trace it is meant to make readable.
func toolArg(in map[string]any) string {
	for _, k := range []string{"command", "file_path", "pattern", "path", "query", "url", "prompt"} {
		if v, ok := in[k].(string); ok && strings.TrimSpace(v) != "" {
			return clipArg(strings.TrimSpace(v))
		}
	}
	return ""
}

// clipArg bounds one trace line. A heredoc or a generated patch can run to
// thousands of characters, and a trace that scrolls the answer off screen has
// defeated its own purpose.
func clipArg(s string) string {
	// Collapse to one line first: a multi-line command would otherwise become
	// several trace entries when the consumer splits on newlines.
	s = strings.Join(strings.Fields(s), " ")
	const max = 120
	// Counted and cut in RUNES, not bytes. A command carrying a path with an
	// accent or a CJK character would otherwise be sliced through the middle of
	// a rune and render as a replacement glyph.
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// streamLine is the subset of Claude Code's stream-json schema council models.
//
// Unknown types are ignored rather than rejected: the schema carries far more
// than a comparison room needs, and a parser that failed on an unrecognised
// event would turn every upstream addition into a broken column.
type streamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`

	// system/init
	Model string `json:"model"`

	// stream_event: the token-level delta, and the tool-call announcement.
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`

	// assistant: the completed message, whose content blocks carry tool calls
	// WITH their inputs. content_block_start announces a tool_use earlier but
	// with an empty input — the arguments arrive afterwards as
	// input_json_delta fragments — so a trace built from the announcement can
	// only ever say "Bash", six times, which is what it said.
	//
	// user: the SAME shape carries tool_result blocks, which is where the
	// outcome of each of those calls arrives. Modelled as one block struct
	// rather than two because the envelope really is identical — only the
	// message type and the populated fields differ.
	Message struct {
		Content []struct {
			Type string `json:"type"`

			// tool_use
			Name  string         `json:"name"`
			ID    string         `json:"id"`
			Input map[string]any `json:"input"`

			// tool_result. Content is held raw because the field is a string in
			// every captured line but is a block ARRAY in the schema; see
			// resultText.
			ToolUseID string          `json:"tool_use_id"`
			IsError   bool            `json:"is_error"`
			Content   json.RawMessage `json:"content"`
		} `json:"content"`
	} `json:"message"`

	// result
	Result       string   `json:"result"`
	IsError      bool     `json:"is_error"`
	TotalCostUSD *float64 `json:"total_cost_usd"`
}

func (Claude) ParseEvent(line []byte) (runner.Event, bool) {
	if len(line) == 0 || line[0] != '{' {
		// Not JSON. Claude Code in stream-json mode emits one object per line,
		// so anything else is noise from a wrapper and is not worth guessing at.
		return runner.Event{}, false
	}
	var sl streamLine
	if err := json.Unmarshal(line, &sl); err != nil {
		return runner.Event{}, false
	}

	switch sl.Type {
	case "system":
		// init carries the session id, which is what makes the NEXT turn a
		// resume instead of a re-send.
		if sl.SessionID != "" {
			return runner.Event{Kind: runner.KindSession, SessionID: sl.SessionID}, true
		}
	case "stream_event":
		if sl.Event.Delta.Type == "text_delta" && sl.Event.Delta.Text != "" {
			return runner.Event{Kind: runner.KindText, Text: sl.Event.Delta.Text}, true
		}
	case "assistant":
		// A tool call is the vendor ACTING rather than answering. Read from the
		// completed message rather than from content_block_start, because only
		// here is the input populated — and a trace of bare tool names is a
		// half-built gauge: six lines reading "Bash" say that something
		// happened six times and nothing about what.
		if acts := toolCalls(sl); len(acts) > 0 {
			return runner.Event{Kind: runner.KindActivity, Acts: acts}, true
		}
	case "user":
		// A tool RESULT. Claude Code feeds each call's outcome back into the
		// conversation as a user-role message, which is why the outcome half of
		// the trace lives under a type that looks like it should be the human
		// talking. It is not: no user is typing during a `-p` turn.
		//
		// This branch is the reason the trace can say whether anything worked.
		// The results were in the stream all along and were being dropped.
		if res := toolResults(sl); len(res) > 0 {
			return runner.Event{Kind: runner.KindActivity, Acts: res}, true
		}
	case "result":
		// Text is the whole final reply. It is carried so the room has a
		// fallback when a turn streamed nothing — a flag that stopped working,
		// or a vendor build that does not emit partials. The consumer uses it
		// ONLY if the column is still empty, so the normal streaming path never
		// renders the reply twice.
		ev := runner.Event{
			Kind:      runner.KindMeta,
			Text:      sl.Result,
			SessionID: sl.SessionID,
			CostUSD:   sl.TotalCostUSD,
		}
		if sl.IsError {
			// The process may still exit 0 while the turn itself failed. The
			// column has to show the failure either way, so it is reported here
			// rather than inferred from the exit code.
			ev.Kind = runner.KindError
			ev.Note = "the vendor reported the turn failed"
			if sl.Result != "" {
				ev.Note = sl.Result
			}
		}
		return ev, true
	}
	return runner.Event{}, false
}
