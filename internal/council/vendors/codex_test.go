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

	for _, want := range []string{"exec", "--json", "-s", "read-only", "--skip-git-repo-check", "--cd"} {
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

// TestCodexReadOnlySandboxIsRequested pins the posture flag. The badge this
// backs says "requested", not "enforced" — see the sandboxMode comment for the
// evidence — but if the flag itself vanished, even that claim would be false.
func TestCodexReadOnlySandboxIsRequested(t *testing.T) {
	spec := mustCodexFirst(t, "brief")
	i := slices.Index(spec.Args, "-s")
	if i < 0 || i+1 >= len(spec.Args) {
		t.Fatalf("no -s sandbox flag in %v", spec.Args)
	}
	if spec.Args[i+1] != "read-only" {
		t.Errorf("sandbox = %q, want read-only", spec.Args[i+1])
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
// read-only posture rides on -c instead. `sandbox_mode` was confirmed to be a
// real configuration field with --strict-config, which rejects unknown keys.
func TestCodexResumeCarriesTheSandboxPostureViaConfig(t *testing.T) {
	spec := mustCodexNext(t, "follow up", "019fca5f-2bbd-7541-a6dc-5917f32b5567")
	i := slices.Index(spec.Args, "-c")
	if i < 0 || i+1 >= len(spec.Args) {
		t.Fatalf("no -c config override in %v", spec.Args)
	}
	if !strings.Contains(spec.Args[i+1], "sandbox_mode") || !strings.Contains(spec.Args[i+1], "read-only") {
		t.Errorf("config override = %q, want a read-only sandbox_mode", spec.Args[i+1])
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

// TestCodexToolActivityIsNotRenderedAsSpeech: these are the real
// command_execution lines from the sandbox probe. The room compares opinions,
// not tool traces, and emitting a shell command as KindText would put words in
// the vendor's mouth that it never said.
func TestCodexToolActivityIsNotRenderedAsSpeech(t *testing.T) {
	lines := [][]byte{
		[]byte(`{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"","exit_code":null,"status":"in_progress"}}`),
		[]byte(`{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"\"C:\\\\Users\\\\sanle\\\\pwsh.exe\" -Command Get-ChildItem","aggregated_output":"execution error","exit_code":-1,"status":"failed"}}`),
	}
	var c Codex
	for _, l := range lines {
		if ev, ok := c.ParseEvent(l); ok {
			t.Errorf("tool activity surfaced as %v: %s", ev.Kind, l)
		}
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
