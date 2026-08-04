package vendors

import (
	"encoding/json"

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
		// --strict-mcp-config must accompany the deny list: without it the
		// session inherits the user's connected MCP servers, whose tools no
		// fixed list can name in advance.
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

	// stream_event: the token-level delta
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"delta"`
	} `json:"event"`

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
