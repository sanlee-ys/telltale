package council

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/gatehook"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestOnlyHooksIsWritten is the safety test of this file, and it is not
// belt-and-braces.
//
// Measured in the 2026-08-04 spike, and it is the reason this file is built by
// naming one key rather than by editing anything: a --settings file carrying
// {"permissions":{"allow":["Bash(mkdir:*)"]}} made `mkdir zzz-leak-probe` run
// with NO permission request and the directory landed on disk. An allow rule is
// step five of the evaluation and council's hook is step one, so the hook would
// still beat it — but a `permissions` key here is a second, silent way for the
// room to grant something, and there is no reason for this file to have one.
// The count is asserted, not the absence of a particular villain.
func TestOnlyHooksIsWritten(t *testing.T) {
	var got map[string]json.RawMessage
	if err := json.Unmarshal(gateHookSettings(`C:\bin\telltale.exe`), &got); err != nil {
		t.Fatalf("the file written is not valid JSON: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("the file has %d top-level keys, want exactly one: %v", len(got), keysOf(got))
	}
	if _, ok := got["hooks"]; !ok {
		t.Fatalf("the one key is not hooks: %v", keysOf(got))
	}
}

// TestTheHookIsMatcherless is the measurement this build turns on, pinned as a
// structural fact about the file.
//
// A matcher would make the gate a LIST, and a list leaks: `touch probe-marker`
// creates a file and ran ungated under the operator's own rules, so their rules
// cover more shapes than anyone would enumerate. Measured 2026-08-12 in one
// turn, four entries side by side: matcherless, "*" and "" each saw a Bash call
// AND a Write call; "Bash" saw only the Bash call.
//
// The absent field is asserted rather than an empty one because they were
// measured equivalent and only one of them cannot be read later as a pattern
// somebody should widen.
func TestTheHookIsMatcherless(t *testing.T) {
	var got struct {
		Hooks struct {
			PreToolUse []map[string]json.RawMessage `json:"PreToolUse"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(gateHookSettings(`C:\bin\telltale.exe`), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Hooks.PreToolUse) != 1 {
		t.Fatalf("want exactly one PreToolUse entry, got %d", len(got.Hooks.PreToolUse))
	}
	if _, ok := got.Hooks.PreToolUse[0]["matcher"]; ok {
		t.Error("the entry carries a matcher; the gate would see only the tools it names")
	}
}

// TestTheHookCommandSurvivesGitBash. THIS IS THE WINDOWS TRAP, and it is here
// because it cost the probe its first three arms before anyone saw it.
//
// Claude Code hands the command string to /usr/bin/bash — Git Bash, on the
// platform this product primarily targets (ADR-002). Bash eats an unquoted
// backslash, so a native Windows path arrives as
//
//	/usr/bin/bash: line 1: C:UserssanleAppDataLocaltelltale.exe: command not found
//
// exit_code 127. A hook that fails to run makes NO decision, so every call goes
// past the gate while the badge still claims one — the quietest failure this
// feature has. Forward slashes fix the separators and the quotes fix the spaces
// in `C:\Program Files\…`, and both are needed.
func TestTheHookCommandSurvivesGitBash(t *testing.T) {
	cmd := hookCommand(`C:\Program Files\telltale\telltale.exe`)

	if strings.Contains(cmd, `\`) {
		t.Errorf("a backslash survived into the hook command: %q — bash will eat it", cmd)
	}
	if !strings.HasPrefix(cmd, `"`) {
		t.Errorf("the binary path is unquoted: %q — a path with a space becomes two words", cmd)
	}
	if !strings.Contains(cmd, `"C:/Program Files/telltale/telltale.exe"`) {
		t.Errorf("the quoted forward-slash path is not in %q", cmd)
	}
	// The words the other side answers to. If these two ever drift the room
	// writes a settings file naming a mode the binary does not have.
	if !strings.HasSuffix(cmd, " "+gatehook.Mode+" "+gatehook.Verb) {
		t.Errorf("the command does not end in the hook mode: %q", cmd)
	}
}

// TestTheWrittenFileIsRealAndPrivate. The file is the gate; a room that wrote
// nothing, or wrote it where the child cannot resolve it, is a room whose seat
// launches ungated.
func TestTheWrittenFileIsRealAndPrivate(t *testing.T) {
	h := NewGateHook()
	defer h.Cleanup()

	if !h.Wired() {
		t.Fatalf("no gate hook was wired: %q", h.Absent)
	}
	// Measured, and it cost a probe: --settings resolves a relative path
	// against the CHILD's working directory, which is the workspace council was
	// pointed at rather than telltale's own. A relative path came back as
	// "Error: Settings file not found" and the seat exited 1.
	if !filepath.IsAbs(h.Path) {
		t.Errorf("Path = %q, want absolute: the child resolves it against the workspace", h.Path)
	}
	body, err := os.ReadFile(h.Path)
	if err != nil {
		t.Fatalf("the gate hook file is not readable: %v", err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("the gate hook file on disk is not valid JSON: %v", err)
	}
	if _, ok := parsed["hooks"]; !ok {
		t.Errorf("the file on disk has no hooks key: %s", body)
	}
}

// TestCleanupRemovesTheFile. One temp directory per room, and the room is the
// thing that ends; leaving them behind would accumulate settings files in the
// temp directory with nothing to say where they came from.
func TestCleanupRemovesTheFile(t *testing.T) {
	h := NewGateHook()
	path := h.Path
	h.Cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the gate hook file survived cleanup: stat err = %v", err)
	}
	// Called twice on the way out of a room — teardown and Run's defer — so the
	// second call must not panic or report anything.
	h.Cleanup()
	// And a zero GateHook is the normal state of a read-only room.
	GateHook{}.Cleanup()
}

// TestTheBadgeTracksTheFileAndNotTheIntent.
//
// The whole point of deriving the claim from h.Wired() is that a binary that
// could not locate itself, a temp directory that could not be created and a
// write that failed all end in the same place: no hook, and a seat that gates
// the older way instead. A badge keyed off "we tried" would survive all three.
func TestTheBadgeTracksTheFileAndNotTheIntent(t *testing.T) {
	wired := postureClaim(model.VendorClaude, true, true, true, true)
	absent := postureClaim(model.VendorClaude, true, true, true, false)

	if wired.Level != SandboxGated || absent.Level != SandboxGated {
		t.Fatal("the badge changed level over which gate is behind it; the seat asks either way")
	}
	if wired.Detail == absent.Detail {
		t.Fatal("a seat that keeps the operator's settings and one that drops them say the same thing")
	}
	// The claim the build exists to make, and it must never appear without a
	// file behind it.
	if !strings.Contains(wired.Detail, "settings stay loaded") {
		t.Errorf("the wired detail does not say the operator's settings survive: %q", wired.Detail)
	}
	if strings.Contains(absent.Detail, "settings stay loaded") {
		t.Errorf("a seat with no gate hook claims the settings survived: %q", absent.Detail)
	}
	if !strings.Contains(absent.Detail, "drops your settings") {
		t.Errorf("the fallback detail does not admit what it gave up: %q", absent.Detail)
	}
	// Both branches gate. That is the one thing neither may stop claiming.
	for _, d := range []string{wired.Detail, absent.Detail} {
		if !strings.Contains(d, "nothing runs until you answer") {
			t.Errorf("a gated branch stopped claiming the gate: %q", d)
		}
	}
}

// TestOnlyAGatedRoomWiresTheHook pins the condition in Run against the one in
// seatPosture. They are the same condition written twice, and the failure mode
// of them drifting is a --auto seat handed a hook that asks about every call
// with nobody in the room to answer it.
func TestOnlyAGatedRoomWiresTheHook(t *testing.T) {
	if wantsGateHook(Options{}) {
		t.Error("a read-only room wired the gate hook; it has nothing to gate")
	}
	if wantsGateHook(Options{Write: true, Auto: true}) {
		t.Error("--write --auto wired the gate hook; it would stall on a question nobody answers")
	}
	if !wantsGateHook(Options{Write: true}) {
		t.Error("the gated room did not wire the gate hook, which is the only thing holding its gate")
	}
}

// TestReadOnlyToolsAreAnsweredNotCarded is the price of the hook being
// matcherless, and the direction of the list is the argument.
//
// Measured 2026-08-12: under the older posture `Read`, `Glob` and `git status`
// raised NO permission request, because Claude Code approves what it classifies
// read-only before the callback. Under the hook all three raise one. Without
// this list the build that closed the gate's last hole would have tripled the
// cards, and this room already learned what that costs — the first session with
// the gate carded the user thirty-four times.
func TestReadOnlyToolsAreAnsweredNotCarded(t *testing.T) {
	for _, tool := range []string{"Read", "Glob", "Grep", "NotebookRead"} {
		if !isReadOnlyTool(tool) {
			t.Errorf("%s draws a card; it changes nothing and it is called constantly", tool)
		}
	}
	// The positive-list direction. Every one of these either changes something
	// or reaches outside the workspace, and an unknown tool must land here too:
	// a false card costs one keystroke, a false approval costs the operator a
	// call they never saw.
	for _, tool := range []string{"Bash", "Write", "Edit", "NotebookEdit", "WebFetch", "Task", "TodoWrite", "SomeToolClaudeCodeAddsNextMonth", ""} {
		if isReadOnlyTool(tool) {
			t.Errorf("%q is auto-approved without a card", tool)
		}
	}
}

// TestTheHookPathNeverReachesState is the same privacy boundary the brief has.
// The path names a file under the operator's temp directory that no part of the
// UI has any reason to show, so only the boolean crosses onto State.
func TestTheHookPathNeverReachesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "gate-tell-tale-marker.json")
	m := newWithBrief(Options{Write: true}, Brief{}, GateHook{Path: path}, Reattachment{})
	m.st.Width, m.st.Height = 120, 24

	if strings.Contains(Render(m.st, PlainStyles(), GlyphsFor(false)), "gate-tell-tale-marker") {
		t.Error("the gate hook file path was rendered to the screen")
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
