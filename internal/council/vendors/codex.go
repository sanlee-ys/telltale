package vendors

import (
	"encoding/json"
	"runtime"

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
// It earned its keep AGAIN in this file on the same date, one level subtler:
// `-s workspace-write` was documented here as un-breaking codex on Windows. No
// flag name was misread — the sentence was simply an inference nobody ran. It
// is false. See windowsSandboxMode for the re-probe that caught it.
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

// sandboxMode is council's read-only posture for this vendor OFF Windows,
// where the OS sandbox is real and enforces the read/write distinction.
//
// READ THE VERIFICATION BEFORE PUTTING A BADGE ON THIS. The Windows story is a
// different one entirely and is carried by windowsSandboxMode below.
//
// One gap on the non-Windows claim, stated rather than papered over: the
// non-shell write path is UNVERIFIED. Asked to create a file with its built-in
// patch/edit tool instead of the shell, codex replied "REFUSED" without
// attempting a tool call. That is a model choice, not evidence of sandbox
// enforcement, and it means nothing is known about whether the sandbox would
// have stopped it.
const sandboxMode = "read-only"

// writeSandboxMode is what write posture asks for OFF Windows.
//
// workspace-write rather than danger-full-access: the containment council
// actually offers is the directory it was pointed at, so the vendor flag should
// agree with that boundary instead of removing it.
//
// It used to carry the parenthetical "which also un-breaks it on Windows".
// That was never measured, and on 2026-08-04 it was measured and is FALSE. See
// windowsSandboxMode.
const writeSandboxMode = "workspace-write"

// windowsSandboxMode is what BOTH postures pass on Windows, and it is the only
// value under which this seat can run anything there at all.
//
// This is the loudest flag in the room and it is not chosen for convenience, so
// the measurement that forces it is recorded in full. Re-probed 2026-08-04
// against codex-cli 0.146.0 on Windows 11, in a throwaway directory, one turn
// per mode, each asked merely to LIST the directory and read a text file:
//
//	-s read-only        → every spawn fails, exit_code -1
//	-s workspace-write  → every spawn fails, exit_code -1
//	-s danger-full-access → exit_code 0, real listing, real file contents
//
// The failure is identical in both sandboxed modes, and it is a failure to
// LAUNCH rather than a refusal of an operation:
//
//	windows sandbox: runner failed during SpawnChild: CreateProcessAsUserW
//	failed: 5 (Access is denied.) | ... | si_flags=256 | creation_flags=525312
//	(Windows error 5)
//
// Three things follow, and each is a measurement rather than a reading:
//
//   - `-s read-only` on Windows is not a read/write distinction. It is a seat
//     that cannot read. That is the whole of the complaint this constant fixes.
//   - `-s workspace-write` does NOT un-break it. The third amendment of ADR-008
//     said it did, in a parenthetical, and nothing had ever run it. So the
//     WRITE posture was broken on Windows too, silently, for exactly as long.
//   - `danger-full-access` is the only mode that skips the Windows sandbox
//     spawn path, so it is the only one that runs. There is no middle setting:
//     `-c sandbox_permissions=["disk-full-read-access"]`, the escalation the
//     CLI's own `--config` help advertises, is rejected outright by this build
//     — "unknown configuration field sandbox_permissions" — so there is no way
//     to buy back read access while keeping a sandbox.
//
// The host-sandbox confound was ruled out rather than assumed: the read-only
// probe was re-run with the harness's own process sandbox disabled and failed
// identically, so this is codex's Windows sandbox and not something wrapped
// around it.
//
// Why this is allowed to ship, in one line: **the workspace is the containment,
// not the flag** (ADR-008 third amendment, on the fleet contract in agent-ops
// ADR-012). The alternative on this OS is not a safer seat — it is a seat that
// cannot read the repo it was convened to discuss, which is what San hit live.
// The badge is what pays for it: this seat renders `unsandboxed` on Windows and
// must never render `ro:` anything there.
const windowsSandboxMode = "danger-full-access"

func sandboxFor(p Posture) string {
	return codexSandboxFor(p, runtime.GOOS == "windows")
}

// codexSandboxFor is sandboxFor with the OS as an argument, so both branches are
// reachable from a test on either machine. The Windows branch is the half that
// was measured; a test that could only run it on Windows would be the half
// nobody checks — and this file's whole history is claims nobody checked.
func codexSandboxFor(p Posture, windows bool) string {
	if windows {
		// Posture-independent ON PURPOSE. Read and write collapse to one flag
		// here because every other value fails to spawn, and grading them would
		// imply a safety difference that does not exist — the same reasoning
		// ADR-008's third amendment used to give every write column one badge.
		return windowsSandboxMode
	}
	if p == PostureWrite {
		return writeSandboxMode
	}
	return sandboxMode
}

// resumeOverrideFor carries the posture onto the resume path.
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
// `sandbox_mode`. The quoted form is what Go passes verbatim through argv since
// no shell is involved.
//
// It is derived from the SAME function as the spawn path rather than repeating
// the branch, which is the point: a resume turn carrying a different posture
// from its spawn turn would change what the seat can do on turn 2 of a
// conversation, and the symptom would be a column that answered once and then
// went quiet. These two shapes have drifted before — that is the entire reason
// resume takes -c at all — so they are wired to one source instead of kept in
// step by hand.
//
// What this still does NOT establish: that the override changes runtime
// behaviour on the resume path. The key is recognised; its effect was never
// separately observed. Until 2026-08-04 it could not be, because every
// sandboxed spawn failed regardless — now that the Windows value is one that
// actually runs, this became measurable, and it has not yet been measured.
func resumeOverrideFor(p Posture) string {
	return codexResumeOverrideFor(p, runtime.GOOS == "windows")
}

func codexResumeOverrideFor(p Posture, windows bool) string {
	return `sandbox_mode="` + codexSandboxFor(p, windows) + `"`
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
