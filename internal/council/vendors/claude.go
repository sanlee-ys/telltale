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

// Session is the persistent invocation: one process, many turns, fed JSONL on
// an stdin that stays open.
//
// VERIFIED LIVE 2026-08-04, Claude Code 2.1.220, on Windows. Two turns were
// sent down one stdin with a two-second pause between them; the pid was
// unchanged, the process was alive throughout, and both turns came back under
// the SAME session_id. Closing stdin exited it cleanly with status 0.
//
// The only addition to the spawn-per-turn flags is --input-format, which is
// what puts the process into realtime streaming input. --resume is deliberately
// absent: there is nothing to resume, because the session never ended.
//
// hooksFile is an ABSOLUTE path to a settings file containing the user's hooks
// and nothing else, or empty. See gateArgs for why it is only ever applied to
// the gated posture, and internal/council/hookset.go for what is in it.
func (c Claude) Session(workspace, binary, hooksFile string, p Posture) (runner.Spec, error) {
	args := append(c.baseArgs(p), "--input-format", "stream-json")
	// Gated posture ONLY, and the condition is the point rather than a
	// precaution. --settings is repair for a hole --setting-sources "" opens,
	// and --setting-sources "" is passed in exactly one posture. In the other
	// two the user's settings are loaded natively, so injecting the same hooks
	// again would run each of them twice — a guard that asks the user two
	// questions per call is a guard people turn off.
	if p == PostureWriteGated && hooksFile != "" {
		args = append(args, "--settings", hooksFile)
	}
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   args,
		// No StdinPrompt. Every turn is a Turn() line written later, which is
		// also why the shim refusal has nothing to catch here: no prompt text
		// can reach argv by any path.
		Dir: workspace,
	}, nil
}

// SessionResume is Session started on a conversation from a previous room.
//
// VERIFIED LIVE 2026-08-04, Claude Code 2.1.220, on Windows. The question this
// answers is whether `--resume` composes with `--input-format stream-json` at
// all — the two had only ever been used apart, `--resume` on the spawn-per-turn
// path and `--input-format` on the persistent one, and the sixth ADR-008
// amendment says explicitly that the persistent session passes no `--resume`
// because "there is nothing to resume". Reattaching is the case where there is.
//
// The probe: one turn in a throwaway directory captured a session id; a second
// process was started with the FULL persistent flag set plus `--resume <id>`,
// handed one turn as JSONL on stdin, and asked what word the first turn had
// used. Four things it settled:
//
//   - The process starts and takes the turn. The composition is accepted.
//   - It answered "ALPHA" — a fact only the PRIOR session's history carries. It
//     is a real resume, not a fresh session that happened to launch.
//   - The reported session_id is the SAME id, unchanged. That is what keeps the
//     saved-room file valid across repeated reattaches: the key does not rotate
//     out from under it every time the room is reopened.
//   - Closing stdin exited 0.
//
// One trap recorded because it looks like a check and is not: `num_turns` came
// back as 1, counting THIS PROCESS's turns rather than the conversation's. It
// cannot be used to tell a resume from a fresh start.
//
// And the failure shape, measured rather than assumed, because it decides what
// the room does with a thread that has aged out: a well-formed id with no
// conversation behind it exits 1 with `No conversation found with session ID:
// <id>` on stderr and a `result` carrying is_error and num_turns 0. It fails
// FAST and FREE — no model turn is spent — which is why the caller may simply
// let the seat die once and start it fresh on the next brief rather than
// pre-flighting the id.
// The hooks file rides along unchanged, and that is not incidental: a resumed
// seat is a gated seat like any other, and one that came back without the
// user's own PreToolUse screen while the badge still said the guard was wired
// would be the quietest false claim in the room. Reattaching restores a
// conversation, never a weaker posture.
func (c Claude) SessionResume(workspace, binary, hooksFile, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	spec, err := c.Session(workspace, binary, hooksFile, p)
	if err != nil {
		return runner.Spec{}, err
	}
	spec.Args = append(spec.Args, "--resume", sessionID)
	return spec, nil
}

// userMessage is the turn envelope, and it is the shape the live probe sent.
//
// Captured verbatim from the run that worked:
//
//	{"type":"user","message":{"role":"user","content":"Reply with exactly: ONE"}}
//
// Marshalled rather than assembled, because a brief is arbitrary user text: it
// contains quotes and newlines by the time anyone uses this feature seriously,
// and string building would produce a broken line rather than a wrong one.
type userMessage struct {
	Type    string      `json:"type"`
	Message userContent `json:"message"`
}

type userContent struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (Claude) Turn(prompt string) ([]byte, error) {
	return json.Marshal(userMessage{
		Type:    "user",
		Message: userContent{Role: "user", Content: prompt},
	})
}

// controlRequest is a message going TO the process. The same envelope carries
// the vendor's questions in the other direction, which is why the parser has to
// tell inbound requests from the responses to our own.
type controlRequest struct {
	Type      string         `json:"type"`
	RequestID string         `json:"request_id"`
	Request   map[string]any `json:"request"`
}

// Interrupt abandons the turn in flight without killing the process.
//
// VERIFIED LIVE: sent while the vendor was blocked on a permission request, it
// came back as
//
//	{"type":"control_response","response":{"subtype":"success","request_id":"int-1","response":{"still_queued":[]}}}
//
// followed by a tool_result saying the user did not want to proceed, a
// "[Request interrupted by user for tool use]" line, and a `result` whose
// terminal_reason was "aborted_tools". The process stayed alive and took a
// further turn afterwards. That is what makes cancelling a turn cheap here:
// the room does not have to pay another session init to recover from it.
//
// The capability is advertised too — every system/init in the spike listed
// "interrupt_receipt_v1" — but the advertisement is not what this rests on.
func (Claude) Interrupt(id string) ([]byte, error) {
	return json.Marshal(controlRequest{
		Type:      "control_request",
		RequestID: id,
		Request:   map[string]any{"subtype": "interrupt"},
	})
}

// Decide answers one permission request.
//
// VERIFIED LIVE, both branches, in throwaway directories:
//
//	allow -> {"type":"control_response","response":{"subtype":"success","request_id":"179ce36e-…","response":{"behavior":"allow","updatedInput":{"file_path":"…","content":"PONG"}}}}
//	         the tool ran and the file was on disk.
//	deny  -> {"type":"control_response","response":{"subtype":"success","request_id":"d006e5d8-…","response":{"behavior":"deny","message":"denied by you (telltale council gate)"}}}
//	         the vendor turned it into
//	         {"type":"tool_result","content":"denied by you (telltale council gate)","is_error":true,"tool_use_id":"toolu_01UWig94…"}
//	         and the file was NOT created.
//
// Note the outer subtype is "success" in BOTH cases. It reports that the answer
// itself was well-formed, not what the answer was — reading it as the decision
// would approve every denial.
//
// The denial message is echoed back to the model verbatim, so it is the one
// piece of council-authored text a vendor ever reads. It says who denied it,
// because "denied" alone invites the model to retry a slightly different way.
func (Claude) Decide(requestID string, allow bool, reason string, input map[string]any) ([]byte, error) {
	resp := map[string]any{"behavior": "deny", "message": reason}
	if allow {
		// updatedInput is required on an allow. The bundle carries a diagnostic
		// for a handler that returns one on a deny, so the two branches do not
		// share a shape and must not be built as if they did.
		if input == nil {
			input = map[string]any{}
		}
		resp = map[string]any{"behavior": "allow", "updatedInput": input}
	}
	return json.Marshal(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   resp,
		},
	})
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
	switch p {
	case PostureWrite:
		// Verified live in a throwaway directory: with the deny list dropped
		// and acceptEdits set, print mode creates the file. Without a
		// permission mode it has nobody to ask and the turn stalls or refuses,
		// so this flag is what makes write mode functional rather than merely
		// unrestricted.
		//
		// acceptEdits covers EDITS and nothing else, which is the whole reason
		// the second flag is here. Observed in a live room: the seat wrote files
		// all session and every `git` call came back needing an approval that
		// this posture has nobody to give — --auto means the gate is off, so the
		// request went to no one. The seat could produce work and could not land
		// it, which stops the room one step short of the only step that makes
		// the work exist for anyone else.
		return append(args, "--permission-mode", "acceptEdits", "--allowedTools", autoAllowedTools)

	case PostureWriteGated:
		return append(args, gateArgs...)
	}
	return append(args, "--disallowedTools", deniedTools)
}

// autoAllowedTools pre-approves the calls that land work, and ONLY in the
// ungated posture.
//
// --allowedTools is the flag this file spends forty lines warning about, and it
// is the right one here for exactly the reason it was the wrong one there. It
// does not remove tools from a session — measured, which is why the read posture
// is a deny list — but what it DOES do is pre-approve them for permission
// prompts. The read posture needed removal and got the wrong flag. This posture
// needs pre-approval, which is the thing the flag actually is.
//
// STRENGTH: the mechanism is measured, and as of 2026-08-09 the RULE SYNTAX is
// too — first by two live races tripping over it, then by a four-arm probe on
// the reference box (claude CLI, seat-shaped invocation: -p,
// --permission-mode acceptEdits, --allowedTools). What the probe pinned:
//
//   - The rules work as prefixes: `git status` under `Bash(git status:*)`
//     ran. The grant this constant makes is real.
//   - THE MATCHER IS PREFIX-ONLY AND CANNOT SEE THROUGH -C. In the
//     seat-shaped invocation, `git -C . status` was BLOCKED under
//     `Bash(git status:*)` and equally blocked under the mid-wildcard
//     `Bash(git -C * status:*)` — a wildcard between tokens does not match in
//     the flag path. There is no rule spelling that scopes a verb behind -C,
//     so there is no rule-shaped fix, and `Bash(git -C:*)` stays rejected for
//     the reason it was always rejected: it would pre-approve every -C verb,
//     the destructive ones included.
//   - CONTEXT IS WHY IT ONLY EVER BIT IN RACES. In a TRUSTED workspace the
//     operator's own settings allowlists apply on top of this flag and can
//     cover -C shapes — the same command that blocked in a worktree ran in
//     the main repo under an identical flag. An arena worktree is a freshly
//     created, never-trusted directory, so there this constant is the whole
//     grant.
//
// The residue is friction, not deadlock: a seat that reaches for `git -C` in
// this posture raises an approval the room now surfaces on the column (the
// `a` approval flow), where the operator answers it — measured working across
// two races. A seat's cwd is already the directory its -C would name, so the
// prompt is usually the model spelling a path it did not need.
//
// SCOPED TO THE VERBS THAT LAND WORK, not to git. `reset`, `clean` and
// `branch -D` are absent on purpose: this is the posture where nobody is
// watching, and "commit what you built" is a different grant from "rewrite what
// is already there". The honest hole in that sentence is `git push`, which this
// syntax cannot sub-scope away from `--force` — so a forced push is reachable
// here, and its containment is the same one everything else in this room rests
// on, which is the directory council was pointed at.
//
// The gated posture deliberately gets none of this broad verb allowlist.
// Council answers its permission callback for safe command variants instead,
// which is what lets `git push` proceed while keeping `git push --force`
// visible and gated.
// The go verbs are here for the same reason the git ones are, and their
// absence was the sharper hole. A seat in this posture could `git commit` and
// `git push` but could not `go build` or `go test`, which is not a narrower
// grant than the git one — it is the same grant with the check removed. It
// could land work it had no way to verify, and it did: a night was spent
// producing changes to this package that could not be compiled, and the only
// safe thing left to do with them was park them on a branch marked UNVERIFIED.
//
// `go test` runs the repository's own test code, so this is a real widening
// and is worth saying plainly rather than filing under tooling. Its
// containment is the one everything else in this posture rests on — the
// directory council was pointed at — and it is the same containment already
// accepted for `git push`, which reaches further than any of these do.
//
// `go run` is deliberately absent. It executes an arbitrary main package,
// which is a different claim from building and testing what is already here.
const autoAllowedTools = "Bash(git add:*),Bash(git commit:*),Bash(git push:*)," +
	"Bash(git checkout:*),Bash(git switch:*),Bash(git status:*),Bash(git log:*)," +
	"Bash(git diff:*),Bash(git pull:*),Bash(git fetch:*),Bash(gh pr:*),Bash(gh run:*)," +
	"Bash(go build:*),Bash(go test:*),Bash(go vet:*),Bash(gofmt:*)"

// gateArgs is what makes the vendor ask before every tool call.
//
// THREE flags, and none of them is optional. Each was established by running
// the CLI and reading what it did, because two of the three do nothing on their
// own and the third is not in --help at all.
//
//   - --permission-prompt-tool stdio. Absent from `claude --help` in 2.1.220,
//     and real: an invented flag is rejected with "unknown option" while this
//     one parses. Confirmed from the shipped binary, whose own SDK spawn code
//     reads `if(M){…G.push("--permission-prompt-tool","stdio")}` where M is a
//     canUseTool callback. ALONE IT DOES NOTHING: passed by itself, the session
//     reported permissionMode "auto", no request was ever emitted, and the file
//     was written.
//
//   - --permission-mode manual, which the session reports back as "default".
//     ALONE IT ALSO DOES NOTHING USEFUL: with nobody to ask, the call
//     short-circuits to a refusal — "Claude requested permissions to write to
//     …, but you haven't granted it yet" — and the vendor gives up rather than
//     waiting. Together with the flag above, the request appears on stdout and
//     the turn blocks on it.
//
//   - --setting-sources "" , and this one is the whole honesty of the feature.
//     Permission ALLOW RULES in the user's settings files are consulted BEFORE
//     the callback, so a call they cover never reaches the gate. Measured on a
//     machine whose settings allow `Bash(mkdir:*)`: with default setting
//     sources, `mkdir zzz` ran ungated and the directory was created; with
//     sources dropped, the same call raised a request, the denial was honoured
//     and nothing was created. Without this flag "nothing writes without your
//     keystroke" is simply false, and it would be false quietly.
//
// The cost of the third was stated rather than buried, and then it was PAID
// rather than left standing: dropping the setting sources also drops the user's
// own hooks and their user-level commands from this seat. Half of that is
// deliberate — the allow rules are what the gate is replacing — and half was
// collateral. A PreToolUse hook is a screen the user built, nothing was
// replacing it, and the calls it covered are disproportionately the ones the
// gate never sees, because a shell command the CLI classifies read-only is
// approved without asking.
//
// The hooks are now carried back in on --settings, which composes with
// --setting-sources "" (measured; see hookset.go for the two verbatim lines).
// The allow rules are NOT, and that separation is enforced by construction
// rather than by care: the file council writes is built by naming the single
// key `hooks`, because the same spike showed a permissions block in that file
// re-admits the rules and puts calls straight past the gate.
//
// What is still dropped, and stays dropped: the user's user-level slash
// commands, and their permission rules. Only the hooks come back.
//
// One limit that no flag closes: shell commands the CLI itself classifies as
// read-only are approved without asking. `git status` was ungated under BOTH
// setting-source configurations. The claim this posture supports is about
// calls that change things, and it is worded that way everywhere it appears.
var gateArgs = []string{
	"--permission-prompt-tool", "stdio",
	"--permission-mode", "manual",
	"--setting-sources", "",
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

// editHalves reads the before and after of a structured file edit out of a
// permission payload — the measured input to the gate card's preview (§9.41).
//
// `old_string` and `new_string` are Claude Code's own key names for the Edit
// tool, captured live at 2.1.226 (the request is quoted whole on runner.Gate).
// Nothing else in the payload is treated as an edit: `content` — the Write
// tool's single field, captured in the same session — is deliberately NOT read
// here, because a payload carrying only the AFTER cannot say what the file
// holds now, and a preview that showed one half as if it were a diff would be
// council inventing the other.
//
// BOTH OR NEITHER, and that is the guard rather than a nicety. The renderer's
// whole test for "may I draw a preview" is that these two differ, so a function
// that filled one from a payload carrying one would turn a Write into a green
// block of added lines against an empty file it never measured.
//
// An empty `new_string` beside a non-empty `old_string` is a legal, measured
// DELETION and passes: the pair was carried, the halves differ, and the preview
// is all removals. Two equal halves — an edit that changes nothing — return as
// they are and the renderer draws nothing, because there is nothing to show.
func editHalves(in map[string]any) (string, string, bool) {
	oldS, oldOK := in["old_string"].(string)
	newS, newOK := in["new_string"].(string)
	if !oldOK || !newOK {
		return "", "", false
	}
	return oldS, newS, true
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

	// control_request: the vendor asking permission and BLOCKING on the answer.
	// Only ever seen on a persistent process — the request arrives on stdout and
	// its answer goes back on the same process's stdin.
	RequestID string `json:"request_id"`
	Request   struct {
		Subtype   string         `json:"subtype"`
		ToolName  string         `json:"tool_name"`
		Input     map[string]any `json:"input"`
		ToolUseID string         `json:"tool_use_id"`
	} `json:"request"`

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
	case "control_request":
		// The vendor is asking, and it is BLOCKED until answered. Only
		// can_use_tool is modelled: the same envelope carries other subtypes,
		// and answering one this code does not understand would be worse than
		// leaving it to the caller's timeout.
		if sl.Request.Subtype != "can_use_tool" || sl.RequestID == "" {
			return runner.Event{}, false
		}
		g := &runner.Gate{
			RequestID: sl.RequestID,
			ToolUseID: sl.Request.ToolUseID,
			Tool:      sl.Request.ToolName,
			Text:      sl.Request.ToolName,
			Input:     sl.Request.Input,
		}
		// The two halves of an edit, when the vendor sent both. Read here rather
		// than in council because the KEY NAMES are this vendor's, exactly as
		// Text's formatting is: the room renders a preview, and only the adapter
		// knows which fields of which payload carry one.
		if oldS, newS, ok := editHalves(sl.Request.Input); ok {
			g.OldContent, g.NewContent = oldS, newS
		}
		if arg := toolArg(sl.Request.Input); arg != "" {
			// The same formatting the activity trace uses, on purpose: the card
			// and the trace entry it decides are the same call, so a user who
			// approves "Bash: go test ./..." should find that exact line in the
			// trace afterwards rather than a second phrasing of it.
			g.Text = sl.Request.ToolName + ": " + arg
		}
		return runner.Event{Kind: runner.KindGate, Gate: g}, true

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
			// One `result` per turn, verified across two turns of one process.
			// On a spawn-per-turn child this is redundant with the process
			// exit; on a persistent one it is the ONLY end-of-turn signal there
			// is, because the process does not exit.
			EndsTurn: true,
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
