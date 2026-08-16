package vendors

import (
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestCodexFlagsMatchTheInstalledCLI pins the first-turn flags against the
// binary's own behaviour, verified against codex-cli 0.146.0 rather than
// remembered.
func TestCodexFlagsMatchTheInstalledCLI(t *testing.T) {
	spec, err := Codex{}.FirstTurn("brief", `C:\ws`, `C:\bin\codex.cmd`, PostureRead)
	if err != nil {
		t.Fatal(err)
	}

	// The sandbox VALUE is per-OS and is pinned by TestCodexPostureIsPerOS on
	// both branches; asserting a literal here would pass on one machine and fail
	// on the other while telling neither anything about the flag's shape.
	for _, want := range []string{"exec", "--json", "-s", sandboxFor(PostureRead), "--skip-git-repo-check", "--cd"} {
		if !slices.Contains(spec.Args, want) {
			t.Errorf("missing %q in %v", want, spec.Args)
		}
	}
	// -a/--ask-for-approval is a top-level/interactive flag and is NOT accepted
	// by `codex exec` — the CLI answers "unexpected argument '-a' found" and
	// exits 2 before doing any work. It is an easy one to reach for, since it
	// appears in `codex --help`.
	if slices.Contains(spec.Args, "-a") || slices.Contains(spec.Args, "--ask-for-approval") {
		t.Error("-a/--ask-for-approval is not a `codex exec` flag; it fails argument parsing")
	}
	// The flag that would silently undo the entire read-only posture.
	if slices.Contains(spec.Args, "--dangerously-bypass-approvals-and-sandbox") {
		t.Error("the sandbox bypass flag must never appear in a council invocation")
	}
}

// TestCodexStdinSentinelIsFinalArgument: "-" is what tells codex to read the
// prompt from stdin, and codex's usage puts PROMPT last. If it stopped being
// the final argument it could be consumed as the value of a preceding flag.
func TestCodexStdinSentinelIsFinalArgument(t *testing.T) {
	for _, spec := range []runner.Spec{
		mustCodexFirst(t, "brief"),
		mustCodexNext(t, "brief", "019fca5f-2bbd-7541-a6dc-5917f32b5567"),
	} {
		if got := spec.Args[len(spec.Args)-1]; got != "-" {
			t.Errorf("last arg = %q, want the stdin sentinel %q in %v", got, "-", spec.Args)
		}
	}
}

// TestCodexPromptNeverEntersArgv is THE safety rule for this vendor, not a
// stylistic preference.
//
// `codex` resolves to an npm shim (codex.cmd) on this machine, and Go's os/exec
// runs .cmd through cmd.exe, whose argument parsing cannot be safely quoted for
// arbitrary text. A brief containing a quote or an ampersand would either break
// the invocation or be re-parsed as something else entirely. runner.Start
// refuses the invocation outright when a shim is paired with an argv prompt, so
// this is belt and braces — but the belt is the part that has to hold.
func TestCodexPromptNeverEntersArgv(t *testing.T) {
	prompt := `a "quoted" & piped | brief; with $vars ^and %percent%`
	for _, spec := range []runner.Spec{
		mustCodexFirst(t, prompt),
		mustCodexNext(t, prompt, "019fca5f-2bbd-7541-a6dc-5917f32b5567"),
	} {
		if spec.StdinPrompt != prompt {
			t.Errorf("prompt did not go to stdin: %q", spec.StdinPrompt)
		}
		for _, a := range spec.Args {
			if strings.Contains(a, "brief") {
				t.Errorf("prompt text leaked into argv: %q", a)
			}
		}
	}
}

// TestCodexSandboxFlagIsAlwaysPassed pins that a posture reaches the CLI at all.
//
// It deliberately asserts the WIRING and not the value: what the value should be
// differs per OS and is pinned on both branches by TestCodexPostureIsPerOS. If
// -s ever went missing, codex would fall back to its own configured default and
// the column's badge would be describing an invocation council did not make —
// which is the failure this whole file exists to prevent, in either direction.
func TestCodexSandboxFlagIsAlwaysPassed(t *testing.T) {
	for _, p := range []Posture{PostureRead, PostureWrite} {
		spec, err := Codex{}.FirstTurn("brief", `C:\ws`, `C:\bin\codex.cmd`, p)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.Index(spec.Args, "-s")
		if i < 0 || i+1 >= len(spec.Args) {
			t.Fatalf("no -s sandbox flag in %v", spec.Args)
		}
		if got, want := spec.Args[i+1], sandboxFor(p); got != want {
			t.Errorf("posture %v sandbox = %q, want %q", p, got, want)
		}
	}
}

// TestCodexResumeDoesNotUseFirstTurnOnlyFlags is the trap this adapter was
// written around, and it is verified against the real CLI rather than assumed:
// `codex exec resume` rejects both -s/--sandbox and --cd with "unexpected
// argument ... found" and exit 2. Because that failure happens at argument
// parsing, it writes nothing to stdout — so a regression here would blank the
// column on every follow-up turn with no card able to explain why.
func TestCodexResumeDoesNotUseFirstTurnOnlyFlags(t *testing.T) {
	spec := mustCodexNext(t, "follow up", "019fca5f-2bbd-7541-a6dc-5917f32b5567")
	for _, banned := range []string{"-s", "--sandbox", "--cd", "-C"} {
		if slices.Contains(spec.Args, banned) {
			t.Errorf("%q is rejected by `codex exec resume`; the turn would fail at argument parsing", banned)
		}
	}
	// The workspace still has to reach the child, and Dir is the only route
	// left once --cd is off the table.
	if spec.Dir != `C:\ws` {
		t.Errorf("Dir = %q; with --cd unavailable this is the only way resume lands in the workspace", spec.Dir)
	}
}

// TestCodexResumeCarriesTheSandboxPostureViaConfig: since -s is rejected, the
// posture rides on -c instead. `sandbox_mode` was confirmed to be a real
// configuration field with --strict-config, which rejects unknown keys.
//
// The value is again taken from the spawn path rather than written out, because
// the thing worth pinning is that the two AGREE — see
// TestCodexResumeCarriesTheSamePostureAsSpawn for the per-OS matrix.
func TestCodexResumeCarriesTheSandboxPostureViaConfig(t *testing.T) {
	spec := mustCodexNext(t, "follow up", "019fca5f-2bbd-7541-a6dc-5917f32b5567")
	i := slices.Index(spec.Args, "-c")
	if i < 0 || i+1 >= len(spec.Args) {
		t.Fatalf("no -c config override in %v", spec.Args)
	}
	if !strings.Contains(spec.Args[i+1], "sandbox_mode") || !strings.Contains(spec.Args[i+1], sandboxFor(PostureRead)) {
		t.Errorf("config override = %q, want sandbox_mode=%q", spec.Args[i+1], sandboxFor(PostureRead))
	}
}

func TestCodexNextTurnResumesRatherThanResends(t *testing.T) {
	spec := mustCodexNext(t, "follow up", "019fca5f-2bbd-7541-a6dc-5917f32b5567")
	if !slices.Contains(spec.Args, "resume") {
		t.Fatalf("no resume subcommand in %v", spec.Args)
	}
	// The session id is POSITIONAL for codex, not the value of a flag, so it is
	// checked by position relative to the subcommand rather than by index of a
	// preceding flag name.
	i := slices.Index(spec.Args, "resume")
	if i+1 >= len(spec.Args) || spec.Args[i+1] != "019fca5f-2bbd-7541-a6dc-5917f32b5567" {
		t.Fatalf("session id does not follow `resume` in %v", spec.Args)
	}
	// Only the new turn is sent; codex replays its own history.
	if spec.StdinPrompt != "follow up" {
		t.Errorf("stdin carries more than the new turn: %q", spec.StdinPrompt)
	}
}

func TestCodexNextTurnWithoutASessionRefuses(t *testing.T) {
	if _, err := (Codex{}).NextTurn("p", "", "codex", "", PostureRead); err != ErrNoResume {
		t.Errorf("err = %v, want ErrNoResume", err)
	}
}

// TestCodexOmitsCdWhenThereIsNoWorkspace: `--cd` with an empty value would make
// codex treat "" as a directory rather than defaulting sensibly.
func TestCodexOmitsCdWhenThereIsNoWorkspace(t *testing.T) {
	spec, err := Codex{}.FirstTurn("brief", "", "codex", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(spec.Args, "--cd") {
		t.Errorf("--cd passed with no workspace: %v", spec.Args)
	}
	if spec.Args[len(spec.Args)-1] != "-" {
		t.Errorf("stdin sentinel lost when workspace was empty: %v", spec.Args)
	}
}

// --- Parser tests, over lines captured verbatim from real runs. ---

// TestCodexParseThreadStarted: codex calls it a thread_id, and this event is the
// only place it appears.
func TestCodexParseThreadStarted(t *testing.T) {
	line := []byte(`{"type":"thread.started","thread_id":"019fca5f-2bbd-7541-a6dc-5917f32b5567"}`)
	ev, ok := Codex{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindSession {
		t.Fatalf("got (%v, %v), want a KindSession", ev, ok)
	}
	if ev.SessionID != "019fca5f-2bbd-7541-a6dc-5917f32b5567" {
		t.Errorf("SessionID = %q", ev.SessionID)
	}
}

// TestCodexParseAgentMessage uses the real line from the minimal spike run.
func TestCodexParseAgentMessage(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}`)
	ev, ok := Codex{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindText {
		t.Fatalf("got (%v, %v), want a KindText", ev, ok)
	}
	if ev.Text != "OK\n" {
		t.Errorf("Text = %q, want the message plus a separating newline", ev.Text)
	}
}

// TestCodexSeparatesConsecutiveAgentMessages: a turn can contain several
// complete messages. These two lines are from the real sandbox probe run, where
// codex narrated an attempt and then reported the outcome. Without a separator
// the column would read "...with the shell.REFUSED".
func TestCodexSeparatesConsecutiveAgentMessages(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"I’ll attempt the requested file creation with the shell."}}`),
		[]byte(`{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"REFUSED"}}`),
	}
	var got string
	for _, l := range lines {
		ev, ok := Codex{}.ParseEvent(l)
		if !ok {
			t.Fatalf("dropped a real agent message: %s", l)
		}
		got += ev.Text
	}
	if !strings.Contains(got, "shell.\nREFUSED") {
		t.Errorf("messages ran together: %q", got)
	}
}

// TestCodexTurnCompletedCarriesNoCost is the honest-numbers rule, pinned.
//
// This is the REAL turn.completed line: codex reports token counts and no
// dollar figure anywhere. CostUSD must therefore stay nil rather than being
// derived from those tokens — a made-up cost rendered beside Claude's reported
// one would be exactly the false precision this product refuses.
func TestCodexTurnCompletedCarriesNoCost(t *testing.T) {
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":20507,"cached_input_tokens":6912,"cache_write_input_tokens":0,"output_tokens":5,"reasoning_output_tokens":0}}`)
	ev, ok := Codex{}.ParseEvent(line)
	if !ok || ev.Kind != runner.KindMeta {
		t.Fatalf("got (%v, %v), want a KindMeta", ev, ok)
	}
	if ev.CostUSD != nil {
		t.Errorf("CostUSD = %v; codex reports no cost, so absent must stay absent", *ev.CostUSD)
	}
}

// TestCodexTurnCompletedIsTheAnswerCompleteMarker.
//
// This vendor answers and then lingers seconds before its process exits — 4.06s
// and 4.25s measured 2026-08-16 against codex-cli 0.147.0, 7.94s in §9.33 on the
// same build. Without EndsTurn the column has no signal but the exit, so it
// renders `streaming` for that whole tail with the finished reply already on
// screen. This is the line that stops it, and it is the LAST line both captures
// saw, so nothing arrives after it to contradict a settled column.
func TestCodexTurnCompletedIsTheAnswerCompleteMarker(t *testing.T) {
	line := []byte(`{"type":"turn.completed","usage":{"input_tokens":20481,"output_tokens":5}}`)
	ev, ok := Codex{}.ParseEvent(line)
	if !ok {
		t.Fatal("turn.completed was dropped; the column would wait for the process exit")
	}
	if !ev.EndsTurn {
		t.Error("turn.completed did not end the turn — the seat renders as working for the whole linger")
	}
}

// TestCodexOnlyTurnCompletedEndsTheTurn. EndsTurn settles a column, so a second
// line carrying it would settle the seat early — mid-answer, with text still to
// come. Every other line this adapter models must leave it false.
func TestCodexOnlyTurnCompletedEndsTheTurn(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"thread.started","thread_id":"01a00b2f-33db-78a2-afda-fe64c26965e1"}`),
		[]byte(`{"type":"turn.started"}`),
		[]byte(`{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"OK"}}`),
		[]byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"go test","exit_code":null,"status":"in_progress"}}`),
		[]byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"go test","exit_code":0,"status":"failed"}}`),
	}
	for _, l := range lines {
		ev, ok := Codex{}.ParseEvent(l)
		if ok && ev.EndsTurn {
			t.Errorf("%s ended the turn; only turn.completed may", l)
		}
	}
}

// TestCodexToolActivityIsNotRenderedAsSpeech: these are the real
// command_execution lines from the sandbox probe. The room compares opinions,
// not tool traces, and emitting a shell command as KindText would put words in
// the vendor's mouth that it never said.
func TestCodexToolActivityIsNotRenderedAsSpeech(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"","exit_code":null,"status":"in_progress"}}`),
		[]byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"execution error","exit_code":-1,"status":"failed"}}`),
	}
	// Contract change, 2026-08-04: command_execution is now surfaced as
	// KindActivity rather than dropped — a room built for command and control
	// has to show the commands. The original invariant still holds and is what
	// is asserted: this must never arrive as the vendor's prose.
	var c Codex
	for _, l := range lines {
		ev, ok := c.ParseEvent(l)
		if ok && ev.Kind == runner.KindText {
			t.Errorf("tool activity became assistant text: %s", l)
		}
		if ok && ev.Kind == runner.KindActivity {
			if len(ev.Acts) != 1 || !strings.Contains(ev.Acts[0].Text, "pwsh") {
				t.Errorf("activity dropped the command it was carrying: %+v", ev)
			}
		}
	}
}

// TestCodexStartedIsPendingAndCompletedResolvesIt, over the same two real lines.
//
// The pair is the point: `item.started` opens the entry so a long command is
// visible WHILE it runs, and `item.completed` resolves that same entry by id
// rather than appending a second one below it. Before this, the trace showed
// nothing until a command had already finished.
func TestCodexStartedIsPendingAndCompletedResolvesIt(t *testing.T) {
	started := []byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"","exit_code":null,"status":"in_progress"}}`)
	completed := []byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"execution error","exit_code":-1,"status":"failed"}}`)

	var c Codex
	ev, ok := c.ParseEvent(started)
	if !ok || ev.Kind != runner.KindActivity || len(ev.Acts) != 1 {
		t.Fatalf("item.started produced (%+v, %v), want one pending activity", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActPending {
		t.Errorf("Outcome = %v; a started command has not finished", ev.Acts[0].Outcome)
	}

	ev, ok = c.ParseEvent(completed)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("item.completed produced (%+v, %v)", ev, ok)
	}
	if ev.Acts[0].ID != "item_1" {
		t.Errorf("ID = %q, want the id item.started opened", ev.Acts[0].ID)
	}
	if ev.Acts[0].Outcome != runner.ActFailed {
		t.Errorf("Outcome = %v, want ActFailed for exit_code -1 / status failed", ev.Acts[0].Outcome)
	}
	if ev.Acts[0].Detail != "execution error" {
		t.Errorf("Detail = %q, want codex's own aggregated_output", ev.Acts[0].Detail)
	}
}

// TestCodexNullExitCodeIsNotSuccess is the single most expensive confusion
// available on this field, pinned.
//
// codex spells "still running" as `"exit_code":null`. Unmarshalled into a plain
// int that becomes 0, which is the spelling of SUCCESS — so a running command
// would render with a tick. The field is a pointer for exactly this reason.
func TestCodexNullExitCodeIsNotSuccess(t *testing.T) {
	line := []byte(`{"type":"item.started","item":{"id":"item_9","type":"command_execution","command":"go build ./...","exit_code":null,"status":"in_progress"}}`)
	ev, _ := Codex{}.ParseEvent(line)
	if len(ev.Acts) != 1 || ev.Acts[0].Outcome == runner.ActOK {
		t.Fatalf("a running command reported success: %+v", ev.Acts)
	}
}

// TestCodexZeroExitIsSuccess: the other half, so the mapping is not merely
// "never claim OK".
func TestCodexZeroExitIsSuccess(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"item_2","type":"command_execution","command":"go test ./...","aggregated_output":"ok","exit_code":0,"status":"completed"}}`)
	ev, _ := Codex{}.ParseEvent(line)
	if len(ev.Acts) != 1 || ev.Acts[0].Outcome != runner.ActOK {
		t.Fatalf("exit 0 did not resolve to success: %+v", ev.Acts)
	}
	if ev.Acts[0].Detail != "" {
		t.Errorf("Detail = %q; only a failure carries one", ev.Acts[0].Detail)
	}
}

// TestCodexCompletionWithNoExitCodeIsUnknownNotOK is the deliberately weak
// claim, and it is weak on purpose.
//
// No captured codex line has ever carried `"status":"completed"` — the observed
// values are "in_progress" and "failed". So an item that finishes with neither
// an exit code nor a failure status resolves UNKNOWN. Guessing the success
// spelling from the failure one would be a success claim built on a string
// nobody has seen, which is precisely how a read-only badge once ended up on a
// session that could write.
func TestCodexCompletionWithNoExitCodeIsUnknownNotOK(t *testing.T) {
	line := []byte(`{"type":"item.completed","item":{"id":"item_4","type":"patch_apply","status":"completed"}}`)
	ev, ok := Codex{}.ParseEvent(line)
	if !ok || len(ev.Acts) != 1 {
		t.Fatalf("got (%+v, %v), want one activity", ev, ok)
	}
	if ev.Acts[0].Outcome != runner.ActUnknown {
		t.Errorf("Outcome = %v, want ActUnknown for a status this adapter has never observed", ev.Acts[0].Outcome)
	}
}

// TestCodexUnknownEventsAreIgnoredNotFatal: upstream will add event types, and a
// parser that failed on an unrecognised one would turn every codex release into
// a broken column. turn.started is real and simply carries nothing council
// needs.
func TestCodexUnknownEventsAreIgnoredNotFatal(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"turn.started"}`),
		[]byte(`{"type":"item.completed","item":{"id":"i","type":"reasoning","text":"thinking"}}`),
		[]byte(`{"type":"some.future.thing","payload":{"a":1}}`),
		[]byte(`{"type":"thread.started"}`), // no thread id
		[]byte(`{"type":"item.completed","item":{"id":"i","type":"agent_message","text":""}}`),
		[]byte(`not json at all`),
		[]byte(``),
		[]byte(`2026-08-04T01:25:25Z ERROR codex_core::exec: exec error`), // stderr-shaped noise
	}
	var c Codex
	for _, l := range lines {
		if ev, ok := c.ParseEvent(l); ok {
			t.Errorf("line produced an event it should have ignored: %s -> %v", l, ev.Kind)
		}
	}
}

// TestCodexParserSurvivesATruncatedStream: a cancelled turn cuts the pipe
// mid-line, so half a JSON object is a normal thing to see, not a crash.
func TestCodexParserSurvivesATruncatedStream(t *testing.T) {
	var c Codex
	for _, partial := range [][]byte{
		[]byte(`{"type":"item.completed","item":{"id":"item_0","type":"agent_mess`),
		[]byte(`{"type":"thread.started","thread_id":"019fca5f-2bbd`),
		[]byte(`{`),
	} {
		if _, ok := c.ParseEvent(partial); ok {
			t.Errorf("a truncated line produced an event: %s", partial)
		}
	}
}

func mustCodexFirst(t *testing.T, prompt string) runner.Spec {
	t.Helper()
	s, err := Codex{}.FirstTurn(prompt, `C:\ws`, "codex", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustCodexNext(t *testing.T, prompt, sess string) runner.Spec {
	t.Helper()
	s, err := Codex{}.NextTurn(prompt, `C:\ws`, "codex", sess, PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
