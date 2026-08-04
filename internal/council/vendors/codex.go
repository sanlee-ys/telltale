package vendors

import (
	"encoding/json"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Codex drives the Codex CLI in its non-interactive `exec` mode.
//
// Everything below was verified against codex-cli 0.146.0 on 2026-08-04 by
// running the real binary and reading its actual output, not by reading flag
// names. That distinction earned its keep on the Claude adapter, where
// `--allowedTools` was specified as the read-only mechanism on the strength of
// its name and the official docs, and turned out to pre-approve tools rather
// than remove them. A flag's name is not evidence of its effect.
//
// The two traps this adapter exists to route around:
//
//   - `codex` resolves to an npm shim (codex.cmd) on this machine. Go's os/exec
//     runs .cmd through cmd.exe, whose argument parsing cannot be safely quoted
//     for arbitrary text, and a council brief is arbitrary text. The prompt
//     therefore goes on stdin, always. runner.Start hard-refuses the other way
//     round, so a mistake here fails loudly rather than silently mis-parsing.
//   - `codex exec` and `codex exec resume` do NOT take the same flags. Three
//     flags the first turn relies on are rejected outright by resume. See
//     resumeOverrideFor(p).
type Codex struct{}

// Registry lives in vendor.go and this adapter deliberately does not edit it —
// registration is the integrator's call, since a seat with no verified
// invocation should render as unavailable rather than as a guess. That leaves
// nothing to check the interface at compile time, so this does it here.
var _ Vendor = Codex{}

func (Codex) ID() model.VendorID { return model.VendorCodex }

// sandboxMode is council's requested read-only posture for this vendor.
//
// READ THE VERIFICATION BEFORE PUTTING A BADGE ON THIS. The honest claim is
// "requested", not "enforced", and the difference is not pedantry.
//
// What was observed on 2026-08-04, Windows 11, codex-cli 0.146.0:
//
//   - Asked to write a file under `-s read-only`, codex tried, and the write
//     did not happen. The file did not exist afterwards.
//   - But the failure was `windows sandbox: runner failed during SpawnChild:
//     CreateProcessAsUserW failed: 5 (Access is denied.)` — a failure to LAUNCH
//     the child process, not a refusal of a write.
//   - The control run settles it: asked merely to LIST a directory, the exact
//     same spawn failed with the exact same error. The sandbox is not
//     discriminating between reads and writes on this machine. It is failing to
//     spawn anything at all.
//
// So the correct statement is: under `-s read-only` no shell command runs on
// this machine, which does mean no shell write can land, but the mechanism is a
// blanket spawn failure rather than demonstrated read-only semantics. If a
// future codex release fixes Windows sandbox spawning, reads would presumably
// start working and writes would presumably still be blocked — presumably being
// the operative word, because that has not been observed here and must not be
// claimed. `codex features list` shows `experimental_windows_sandbox` and
// `elevated_windows_sandbox` both as "removed", so this surface is in flux.
//
// One further gap, stated rather than papered over: the non-shell write path is
// UNVERIFIED. Asked to create a file with its built-in patch/edit tool instead
// of the shell, codex replied "REFUSED" without attempting a tool call. That is
// a model choice, not evidence of sandbox enforcement, and it means nothing is
// known about whether the sandbox would have stopped it.
const sandboxMode = "read-only"

// writeSandboxMode is what write posture asks for.
//
// workspace-write rather than danger-full-access: the containment council
// actually offers is the directory it was pointed at, so the vendor flag should
// agree with that boundary instead of removing it. It also happens to UNBREAK
// codex on Windows -- under read-only every sandboxed process spawn fails
// outright, including one asked merely to list a directory, so the read posture
// costs this vendor the ability to run anything at all.
const writeSandboxMode = "workspace-write"

func sandboxFor(p Posture) string {
	if p == PostureWrite {
		return writeSandboxMode
	}
	return sandboxMode
}

// resumeSandboxOverride carries the posture onto the resume path.
//
// This exists because `codex exec resume` rejects `-s/--sandbox` outright —
// verified, not inferred: the CLI answers `error: unexpected argument '-s'
// found`. Passing the first turn's flags to resume would fail the turn on every
// follow-up, and it would fail at argument parsing with nothing on stdout, so
// the column would go blank for a reason no card could explain.
//
// `-c/--config` IS accepted by resume, and `sandbox_mode` is a real
// configuration field rather than a typo silently swallowed: `-c` takes
// arbitrary keys, so the key name was checked with `--strict-config`, which
// rejects `bogus_key_xyz` ("unknown configuration field") and accepts
// `sandbox_mode`. The quoted form below is the one that was tested, and it is
// what Go passes verbatim through argv since no shell is involved.
//
// What this does NOT establish: that the override changes runtime behaviour.
// The key is recognised; its effect was not separately observable, because as
// documented above every sandboxed spawn already fails on this machine.
const resumeSandboxOverride = `sandbox_mode="read-only"`

// resumeOverrideFor mirrors sandboxFor through the -c channel, since resume
// will not take -s.
func resumeOverrideFor(p Posture) string {
	if p == PostureWrite {
		return `sandbox_mode="` + writeSandboxMode + `"`
	}
	return resumeSandboxOverride
}

func (c Codex) FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error) {
	args := []string{
		"exec",
		"--json",
		// -s is accepted here and ONLY here; resume rejects it.
		"-s", sandboxFor(p),
		// Council dispatches into whatever directory the user is sitting in,
		// which is frequently not a git repo. Without this codex refuses to run
		// at all rather than degrading.
		"--skip-git-repo-check",
	}
	if workspace != "" {
		// --cd is also first-turn-only; resume rejects it too. Dir below is set
		// regardless, so the resume path still lands in the right directory.
		args = append(args, "--cd", workspace)
	}
	// "-" is codex's documented sentinel for "read the prompt from stdin", and
	// it must be the final argument. This is the shim safety rule: the prompt
	// never becomes an argv element that cmd.exe would re-parse.
	args = append(args, "-")

	return runner.Spec{
		Vendor:      c.ID(),
		Binary:      binary,
		Args:        args,
		StdinPrompt: prompt,
		Dir:         workspace,
	}, nil
}

func (c Codex) NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error) {
	if sessionID == "" {
		return runner.Spec{}, ErrNoResume
	}
	// Argument order is load-bearing: resume's usage is
	// `codex exec resume [OPTIONS] [SESSION_ID] [PROMPT]`, and the session id is
	// positional. It is placed before the options here because the trailing "-"
	// is itself the positional PROMPT, and putting the id after the flags keeps
	// the two positionals from being read in the wrong order.
	args := []string{
		"exec", "resume", sessionID,
		"-c", resumeOverrideFor(p),
		"--skip-git-repo-check",
		"--json",
		"-",
	}
	return runner.Spec{
		Vendor: c.ID(),
		Binary: binary,
		Args:   args,
		// Only the new turn is sent. Codex replays its own history from the
		// rollout it stored under this thread id.
		StdinPrompt: prompt,
		// Carries the workspace on the path where --cd is not available.
		Dir: workspace,
	}, nil
}

// codexLine is the subset of codex's --json JSONL schema council models.
//
// Captured from real runs rather than transcribed from docs. A whole turn looks
// like this, and this is the complete set of types that were observed:
//
//	{"type":"thread.started","thread_id":"019fca5f-2bbd-7541-a6dc-5917f32b5567"}
//	{"type":"turn.started"}
//	{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}
//	{"type":"turn.completed","usage":{"input_tokens":20507,"output_tokens":5,...}}
//
// Two things are deliberately absent from this struct:
//
//   - No cost field. `turn.completed` reports token counts and nothing else, so
//     CostUSD stays nil for this vendor forever. Deriving a dollar figure from
//     tokens is on the rejected list — a made-up number rendered next to a real
//     one is exactly the failure this product refuses.
//   - No error event. A failing turn was observed to write to stderr and exit
//     non-zero with NOTHING on stdout (a bad resume id gives
//     `Error: thread/resume: ... no rollout found`, exit 1). runner already
//     turns that into a KindError from the exit code and stderr tail, so there
//     is no stdout error shape to model. If codex later grows a `turn.failed`
//     event it will be dropped here, and the exit code will still carry it.
type codexLine struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`

	// item.started / item.completed
	Item struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Text   string `json:"text"`
		Status string `json:"status"`
		// command_execution carries the shell command, and this field name IS
		// verified — a captured spike line holds
		// `"command":"\"...pwsh.exe\" -Command Get-ChildItem"`, from the run
		// where the Windows sandbox refused to spawn it. Parsed defensively
		// anyway: when Command is empty the item type is shown instead, so the
		// column still reports that the vendor did something.
		Command string `json:"command"`
		// ExitCode is a POINTER because codex spells "still running" as
		// `"exit_code":null` and a real failure as `"exit_code":-1`. A plain
		// int would flatten the first into zero, which is the spelling of
		// success — the single most expensive confusion available on this
		// field.
		ExitCode *int `json:"exit_code"`
		// AggregatedOutput is the command's own output, and the only source of
		// a failure line this adapter is willing to show. Council does not
		// compose a diagnosis of its own.
		AggregatedOutput string `json:"aggregated_output"`
	} `json:"item"`
}

// codexOutcome maps a completed item to what is actually known about it.
//
// exit_code first, because it is the process's own answer and it was captured
// live on both paths (`null` while in_progress, `-1` on the sandbox spawn
// refusal). status is the fallback for item types that carry no exit code.
//
// The asymmetry here is deliberate and is the honest reading of what was
// observed. "failed" IS a captured status value. "completed" is NOT — no
// captured line carries it — so it is not mapped, and an item that resolves
// with neither an exit code nor a failure status renders Unknown rather than
// OK. Guessing the success spelling from the failure one would be a success
// claim built on a string nobody has seen, which is the exact move that put a
// read-only badge on a session that could write (§9.2). If a live run later
// shows the spelling, this becomes one more case and the trace gets sharper;
// until then Unknown is the truth and it is a survivable one.
func codexOutcome(status string, exitCode *int) runner.ActStatus {
	if exitCode != nil {
		if *exitCode == 0 {
			return runner.ActOK
		}
		return runner.ActFailed
	}
	if status == "failed" {
		return runner.ActFailed
	}
	return runner.ActUnknown
}

// isCodexTool reports whether an item type represents the vendor ACTING.
//
// command_execution is the only one observed live; the rest are named from
// codex's own item vocabulary and are unverified, which is safe in this
// direction — an unlisted type is dropped rather than guessed at.
func isCodexTool(t string) bool {
	switch t {
	case "command_execution", "file_change", "patch_apply", "mcp_tool_call", "web_search":
		return true
	}
	return false
}

// codexWhat names one item for the trace: the command it ran, or failing that
// the kind of thing it was. Clipped, because a heredoc or a generated patch can
// run to thousands of characters and a trace that scrolls the answer off screen
// has defeated its own purpose.
func codexWhat(cl codexLine) string {
	if cl.Item.Command != "" {
		return clipArg(cl.Item.Command)
	}
	return cl.Item.Type
}

func (Codex) ParseEvent(line []byte) (runner.Event, bool) {
	if len(line) == 0 || line[0] != '{' {
		// codex --json emits one object per line, so anything else is wrapper
		// noise and is not worth guessing at.
		return runner.Event{}, false
	}
	var cl codexLine
	if err := json.Unmarshal(line, &cl); err != nil {
		return runner.Event{}, false
	}

	switch cl.Type {
	case "thread.started":
		// codex calls it a thread, not a session, and this is the only place the
		// id appears. It is what makes the NEXT turn a resume instead of a
		// re-send.
		if cl.ThreadID != "" {
			return runner.Event{Kind: runner.KindSession, SessionID: cl.ThreadID}, true
		}
	case "item.started":
		// The call, announced. Captured live as
		// `{"type":"item.started","item":{"id":"item_1","type":"command_execution",
		// "command":"...","aggregated_output":"","exit_code":null,"status":"in_progress"}}`.
		//
		// It used to be dropped, and the trace only appeared once a command had
		// already finished — so a long build showed nothing at all while it ran.
		// Announcing here and resolving on item.completed is what makes a
		// running step visible AND lets its outcome land on the same entry
		// instead of below it as a second line.
		if isCodexTool(cl.Item.Type) {
			return runner.Event{
				Kind: runner.KindActivity,
				Acts: []runner.ActCall{{ID: cl.Item.ID, Text: codexWhat(cl), Outcome: runner.ActPending}},
			}, true
		}
	case "item.completed":
		if cl.Item.Type == "agent_message" && cl.Item.Text != "" {
			// The trailing newline is deliberate. Unlike Claude's token deltas,
			// which concatenate naturally, each agent_message is a COMPLETE
			// message, and a turn can contain several — the sandbox probe run
			// produced "I'll attempt the requested file creation with the shell."
			// followed by "REFUSED" as two separate items. Appending them raw
			// would run two sentences together with no space.
			return runner.Event{Kind: runner.KindText, Text: cl.Item.Text + "\n"}, true
		}
		// Every other item type is tool activity. It used to be dropped, on the
		// reasoning that the room compares opinions rather than tool traces.
		// That was right for a deliberation room and wrong for a command and
		// control console: a column that runs three commands and then answers
		// has to show both, and a vendor that reports nothing incremental is
		// otherwise a blank panel for the whole turn.
		//
		// A WHITELIST rather than "anything that is not agent_message". The
		// broad form swept up `reasoning` items (the model thinking, not
		// acting) and empty agent_messages, both of which would render as
		// phantom steps in the trace. An unrecognised item type is still
		// dropped, so a future codex release cannot invent activity here.
		if isCodexTool(cl.Item.Type) {
			// Text is carried on the RESULT too, not just the announcement.
			// The id normally matches an entry item.started already created and
			// the text is then redundant — but if a codex build ever stops
			// emitting item.started, the completion alone still names what ran
			// rather than resolving an entry that does not exist.
			act := runner.ActCall{
				ID:      cl.Item.ID,
				Text:    codexWhat(cl),
				Outcome: codexOutcome(cl.Item.Status, cl.Item.ExitCode),
			}
			if act.Outcome == runner.ActFailed {
				act.Detail = clipArg(firstLine(cl.Item.AggregatedOutput))
			}
			return runner.Event{Kind: runner.KindActivity, Acts: []runner.ActCall{act}}, true
		}
	case "turn.completed":
		// The end-of-turn marker. It carries no text and no cost: codex reports
		// only token counts, so unlike the Claude adapter there is no final-text
		// fallback available here — if no agent_message arrived, the column has
		// nothing to show, and that is the truth rather than a gap to fill.
		return runner.Event{Kind: runner.KindMeta}, true
	}
	return runner.Event{}, false
}
