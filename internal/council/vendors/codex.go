package vendors

import (
	"encoding/json"
	"path/filepath"
	"runtime"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Codex drives the Codex CLI in its non-interactive `exec` mode.
//
// Everything below was verified against codex-cli 0.146.0 on 2026-08-04 by
// running the real binary and reading its actual output, not by reading flag
// names. The three argv shapes were re-run against codex-cli 0.151.0 on
// 2026-09-01 (the turn.failed paragraph on codexLine holds that capture): the
// first turn in both postures and the resume each passed argument parsing and
// produced `thread.started`, so no flag moved between 0.149.1 and 0.151.0. The
// SANDBOX claims below were NOT re-measured on that run and stay pinned at
// 0.149.1: every turn that day died at the account's usage limit before a
// tool could be asked for. That distinction earned its keep on the Claude adapter, where
// `--allowedTools` was specified as the read-only mechanism on the strength of
// its name and the official docs, and turned out to pre-approve tools rather
// than remove them. A flag's name is not evidence of its effect.
//
// It earned its keep AGAIN in this file on the same date, one level subtler:
// `-s workspace-write` was documented here as un-breaking codex on Windows. No
// flag name was misread — the sentence was simply an inference nobody ran. It
// was false at codex-cli 0.146.0. See windowsWriteSandboxMode for the re-probe
// that caught it, and for the 2026-08-29 re-measurement that later moved the
// READ posture back to a real sandbox on Windows.
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

// sandboxMode is council's read-only posture for this vendor on EVERY OS.
//
// Until 2026-08-29 this constant was OFF-Windows only, because at codex-cli
// 0.146.0 the mode failed every process spawn on Windows and both postures
// there collapsed to danger-full-access (see windowsWriteSandboxMode for that
// record). Re-measured 2026-08-29 against codex-cli 0.149.1 on Windows 11, one
// turn per probe in a throwaway directory, and the mode now ENFORCES:
//
//   - a read turn completed: a directory listing and a file read both ran and
//     returned real contents, exit 0.
//   - a write turn was DENIED: `cmd /c echo probe> wrote-ro2.txt` came back
//     "Access is denied.", exit 1, and no file was on disk afterwards.
//
// One Windows caveat rides the read half, and it is liveness rather than
// safety. The sandboxed child could not spawn this machine's PowerShell —
// every pwsh spawn failed with the same `CreateProcessAsUserW failed: 5` line
// that 0.146.0 failed everything with — and the turns completed because codex
// retried through `C:\WINDOWS\system32\cmd.exe`, which spawns INSIDE the
// sandbox and obeys it. The retry is the model's own move, not a runtime
// guarantee, so a Windows read turn can still fail to inspect when the model
// stops at the spawn error instead. The cause of the pwsh-only failure is
// unmeasured; only the behavior is recorded.
//
// One gap on the claim, stated rather than papered over: the non-shell write
// path is UNVERIFIED. Asked to create a file with its built-in patch/edit tool
// instead of the shell, codex replied "REFUSED" without attempting a tool
// call. That is a model choice, not evidence of sandbox enforcement, and it
// means nothing is known about whether the sandbox would have stopped it.
const sandboxMode = "read-only"

// writeSandboxMode is what write posture asks for OFF Windows.
//
// workspace-write rather than danger-full-access: the containment council
// actually offers is the directory it was pointed at, so the vendor flag should
// agree with that boundary instead of removing it.
//
// On Windows the mode is real now too — measured 2026-08-29 at codex-cli
// 0.149.1: a write inside the workspace landed, a write outside it (and
// outside the temp roots the mode allows by design) was denied with no file on
// disk. Write posture still does not use it there, for the reason on
// windowsWriteSandboxMode: the `.git` carve-out cannot be bought back on
// Windows, so a workspace-write seat there can edit and never commit.
const writeSandboxMode = "workspace-write"

// windowsWriteSandboxMode is what WRITE posture passes on Windows.
//
// This is the loudest flag in the room and it is not chosen for convenience.
// Two dated measurements stand behind it, and they force different halves:
//
// 1. Re-probed 2026-08-04 against codex-cli 0.146.0 on Windows 11: BOTH
// sandboxed modes failed every process spawn with `CreateProcessAsUserW
// failed: 5 (Access is denied.)`, including a turn asked merely to list a
// directory. So both postures passed danger-full-access — `-s read-only` there
// was not a read/write distinction, it was a seat that could not read, which
// is how it surfaced: a live turn answered a "thoughts on this repo" brief
// with "I could not inspect the repository". The host-sandbox confound was
// ruled out: the probe re-run with the harness's own process sandbox disabled
// failed identically.
//
// 2. Re-measured 2026-08-29 against codex-cli 0.149.1 on Windows 11: the
// sandboxed modes now spawn (through cmd.exe; see sandboxMode) and ENFORCE, so
// the READ posture went back to `-s read-only` there. The write posture stays
// here, because `-s workspace-write` on Windows denies `.git` and the
// writable_roots override that buys it back on macOS does NOT work there:
// with `-c sandbox_workspace_write.writable_roots=["<ws>/.git"]` passed — in
// the forward-slash spelling gitWritableOverride emits AND in a backslash
// spelling — a write to `.git/probe.txt` still came back "Access is denied.",
// while the same override named an ordinary outside directory and unlocked it.
// The deny on `.git` outranks the override at this build. A workspace-write
// seat on Windows is therefore the exact defect gitWritableOverride exists to
// prevent: it edits files all session and never lands one.
//
// Why this is allowed to ship, in one line: **the workspace is the containment,
// not the flag** (ADR-008 third amendment, on the fleet contract in agent-ops
// ADR-012). The badge is what pays for it: the room's write posture renders its
// own loud badge, and the read posture's `ro:enforced` claim never covers this
// flag.
const windowsWriteSandboxMode = "danger-full-access"

func sandboxFor(p Posture) string {
	return codexSandboxFor(p, runtime.GOOS == "windows")
}

// codexSandboxFor is sandboxFor with the OS as an argument, so both branches are
// reachable from a test on either machine. The Windows branch is the half that
// was measured; a test that could only run it on Windows would be the half
// nobody checks — and this file's whole history is claims nobody checked.
func codexSandboxFor(p Posture, windows bool) string {
	if p == PostureWrite {
		if windows {
			// Not workspace-write: the mode enforces on Windows now, but its
			// .git carve-out cannot be bought back there, so a seat under it
			// builds and never commits. See windowsWriteSandboxMode.
			return windowsWriteSandboxMode
		}
		return writeSandboxMode
	}
	// Read posture is one value on every OS since codex-cli 0.149.1 — the
	// Windows measurement behind that is on sandboxMode.
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
// The override's runtime effect on the resume path is now OBSERVED, not just
// recognised. Measured 2026-08-29 against codex-cli 0.149.1 on Windows 11: a
// turn resumed with `-c sandbox_mode="read-only"` was asked to write a file
// through the shell, the write came back "Access is denied." at exit 1, and no
// file was on disk afterwards. Until 2026-08-04 this could not be measured,
// because every sandboxed spawn failed regardless; the note that stood here
// until 2026-08-29 said so and named the measurement still owed. It is paid.
func resumeOverrideFor(p Posture) string {
	return codexResumeOverrideFor(p, runtime.GOOS == "windows")
}

func codexResumeOverrideFor(p Posture, windows bool) string {
	return `sandbox_mode="` + codexSandboxFor(p, windows) + `"`
}

// gitWritableOverride makes the workspace's own .git writable in write posture.
//
// MEASURED 2026-08-05, codex-cli 0.146.0, macOS, with a control:
//
//	codex sandbox -c sandbox_mode="workspace-write" -- touch .git/probe
//	  → touch: .git/telltale-probe: Operation not permitted
//	codex sandbox -c sandbox_mode="workspace-write" -- touch ./probe
//	  → OK
//	same, plus -c sandbox_workspace_write.writable_roots=["<ws>/.git"]
//	  → OK
//
// So codex's seatbelt carves .git out of the writable workspace by default.
// Every commit writes .git/index, COMMIT_EDITMSG, refs and objects, which is why
// this seat could edit files all session and never land one — it reported the
// failure as an inability to commit, which reads like a git problem and is a
// sandbox one.
//
// This is a real widening and is stated rather than slipped in: a seat that can
// write .git can rewrite history, not merely change tracked files. It is scoped
// to the workspace's OWN .git — not a blanket writable root, not the parent, and
// never in read posture, where the seat has nothing to land in the first place.
// The containment remains the directory council was pointed at, which is the
// same sentence as everywhere else in this room; .git is inside that directory
// and the default was the anomaly.
//
// Carried as -c rather than --add-dir because `codex exec resume` rejects flags
// that `exec` accepts — the trap already recorded for -s and --cd — and -c is
// the channel resume already carries posture on. The dotted form was the one
// probed; both spellings worked, and this is the one that composes.
//
// ON WINDOWS THIS OVERRIDE DOES NOT WORK, and that is measured rather than
// suspected — 2026-08-29, codex-cli 0.149.1, Windows 11: under `-s
// workspace-write` a write to `.git/probe.txt` was denied, and it stayed
// denied with this override passed in the forward-slash spelling this function
// emits AND in a backslash spelling, while the same override named an ordinary
// outside directory and unlocked it. The `.git` deny outranks writable_roots
// at that build. This is the reason Windows write posture keeps
// danger-full-access (see windowsWriteSandboxMode); the override is still sent
// there, where it is harmless — danger-full-access has no sandbox for it to
// widen — so the spawn and resume shapes stay uniform across OSes.
func gitWritableOverride(workspace string) string {
	return `sandbox_workspace_write.writable_roots=["` + filepath.ToSlash(filepath.Join(workspace, ".git")) + `"]`
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
		if p == PostureWrite {
			args = append(args, "-c", gitWritableOverride(workspace))
		}
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
	}
	// The same widening as the spawn path, for the same reason it is derived from
	// one function there: a resume that dropped it would let the seat commit on
	// turn 1 and refuse on turn 2, and the symptom — a column that lands work
	// once and then reports it cannot — is the posture drift this file already
	// exists to prevent.
	if p == PostureWrite && workspace != "" {
		args = append(args, "-c", gitWritableOverride(workspace))
	}
	args = append(args,
		"--skip-git-repo-check",
		"--json",
		"-",
	)
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
//   - No error event, UNTIL codex-cli 0.151.0. Through 0.147.0 a failing turn
//     was observed to write to stderr and exit non-zero with NOTHING on stdout
//     (a bad resume id gives `Error: thread/resume: ... no rollout found`,
//     exit 1), and runner turned that into a KindError from the exit code and
//     the stderr tail. The paragraph that stood here said a `turn.failed`
//     event would be dropped and the exit code would still carry it. The
//     first half came true. The second half is the defect this struct closes.
//
// MEASURED 2026-09-01, codex-cli 0.151.0, Windows 11, from a scratch directory
// with the seat's exact argv (both postures and the resume shape), the prompt
// on stdin. Every turn ended the same way, and stderr was EMPTY:
//
//	{"type":"thread.started","thread_id":"01a05fe0-eeab-7773-81a7-4af560cfa4e1"}
//	{"type":"turn.started"}
//	{"type":"error","message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 11:45 PM."}
//	{"type":"turn.failed","error":{"message":"You've hit your usage limit. Upgrade to Pro (https://chatgpt.com/explore/pro), visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 11:45 PM."}}
//	(exit 1)
//
// The failure sentence now rides STDOUT as JSON, and the stderr tail the
// runner keeps for the card holds nothing. Dropping the line left the room
// with a bare `exit status 1` on a seat that had said exactly what was wrong.
// That is how this surfaced: a hosted read room and a gated write room both
// showed `codex — failed (exit 1)` and no sentence. `turn.failed` is
// therefore parsed, and its sentence is the card's note.
//
// `error` is NOT parsed, on purpose. In the capture it always paired with a
// `turn.failed` that carried the same text, and whether a lone `error` line
// means the turn is over is unmeasured. A column marked failed on an event
// that may be a recoverable hiccup would be the room inventing the verdict.
// `turn.failed` IS the verdict.
//
// The failure is left Unclassified. runner.FailureClass is grounded in strings
// captured off a run that positively never reached the conversation, and a
// usage-limit refusal says nothing either way about the thread: the resume
// shape produced `thread.started` with the requested id and then died the
// same way. Whether a room may keep a restored thread across that is a resume
// ruling (ADR-008, sixteenth amendment), and it is not made here.
type codexLine struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	// Error is turn.failed's payload, captured at 0.151.0 as
	// `"error":{"message":"..."}`. Only the message is modelled, because
	// nothing else was on the line.
	Error struct {
		Message string `json:"message"`
	} `json:"error"`

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
// observed. "failed" IS a captured status value. "completed" has now been
// captured too — 2026-08-29, codex-cli 0.149.1, on command_execution items —
// but every captured "completed" line also carried `"exit_code":0`, so the
// exit code already resolves those and mapping the status would change
// nothing observed. It stays unmapped for the item types that carry NO exit
// code (patch_apply and friends), where the spelling has still never been
// seen: claiming it across item types would be a success claim built on a
// string captured somewhere else, which is the exact move that put a
// read-only badge on a session that could write (§9.2). For those, Unknown is
// still the truth and it is a survivable one.
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
	case "turn.failed":
		// The vendor's own verdict on the turn, in its own words. Captured at
		// codex-cli 0.151.0 (see codexLine). Through 0.147.0 the failure was
		// stderr plus an exit code, and this line did not exist.
		//
		// The SHAPE is agy's (vendors/agy.go): a KindError with exit code 0, no
		// error and no EndsTurn, because the process has not exited yet. That
		// puts it on the branch failedturn_test.go pins. The column settles at
		// the vendor's sentence and the exit still retires it. What the exit
		// must NOT do is replace this sentence with its own `exit status 1`.
		// dispatch.go's KindError branch demotes the exit to the note's detail
		// line when the vendor has already spoken.
		note := "the vendor reported the turn failed"
		if cl.Error.Message != "" {
			note = cl.Error.Message
		}
		return runner.Event{Kind: runner.KindError, Note: note}, true
	case "turn.completed":
		// The end-of-turn marker. It carries no text and no cost: codex reports
		// only token counts, so unlike the Claude adapter there is no final-text
		// fallback available here — if no agent_message arrived, the column has
		// nothing to show, and that is the truth rather than a gap to fill.
		//
		// EndsTurn is set, and this is a spawn-per-turn seat, so that pairing is
		// the unusual part and it is measured rather than assumed. This process
		// DOES exit on its own, and the column used to wait for the exit — which
		// meant the seat rendered `streaming` for seconds after the answer was
		// complete and on screen. MEASURED 2026-08-16 against codex-cli 0.147.0
		// on Windows 11, read-only posture, a brief-shaped prompt in a throwaway
		// directory, two trials:
		//
		//	trial 1: turn.completed 6.619s → exit 10.870s   (4.251s of linger)
		//	trial 2: turn.completed 4.499s → exit  8.555s   (4.056s of linger)
		//
		// §9.33 measured 7.94s of the same on this build, so the size varies and
		// only the shape is stable. What makes it safe to settle the column here
		// is the second half of that capture: NOTHING rides the tail. On both
		// trials the last stdout line is this one, stderr stays empty, and the
		// vendor's own rollout file under ~/.codex/sessions took its final write
		// 51ms and 224ms after this event — i.e. ~4s BEFORE the exit. The receipts
		// are complete when this line lands.
		//
		// What that does NOT license is killing the process, and council does not:
		// both probe turns were told to use no tools, so a turn that ran commands
		// is unmeasured here. At the build it was measured on, probing one needed
		// danger-full-access, a redline; since codex-cli 0.149.1 `-s read-only`
		// runs commands on Windows (see sandboxMode), so that probe is possible
		// now and still unrun. So the exit is still what retires the column from the turn
		// (dispatch.go, KindMeta's spawn-per-turn branch) — this event only makes
		// the column stop claiming to be working.
		return runner.Event{Kind: runner.KindMeta, EndsTurn: true}, true
	}
	return runner.Event{}, false
}
