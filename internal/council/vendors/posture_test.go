package vendors

import (
	"slices"
	"strings"
	"testing"
)

// TestWritePostureDropsTheDenyList: --write has to actually widen the vendor,
// not merely change a badge. Verified live in a throwaway directory — with the
// deny list gone and acceptEdits set, print mode creates the file.
func TestWritePostureDropsTheDenyList(t *testing.T) {
	spec, err := Claude{}.FirstTurn("brief", `C:\ws`, "claude", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(spec.Args, "--disallowedTools") {
		t.Error("write posture still denies tools; the flag would be theatre")
	}
	if !slices.Contains(spec.Args, "--permission-mode") {
		t.Error("write posture sets no permission mode: print mode has nobody to ask, so the turn stalls or refuses")
	}
	// MCP stays off in BOTH postures. Write mode widens what a vendor may do
	// inside the workspace it was pointed at; MCP servers reach outside it —
	// the verification run surfaced Gmail write tools — and "may edit this
	// worktree" is a different grant from "may act on your accounts".
	if !slices.Contains(spec.Args, "--strict-mcp-config") {
		t.Error("write posture dropped --strict-mcp-config, widening past the workspace")
	}
}

func TestReadPostureStillDenies(t *testing.T) {
	spec, _ := Claude{}.FirstTurn("brief", `C:\ws`, "claude", PostureRead)
	if !slices.Contains(spec.Args, "--disallowedTools") {
		t.Error("read posture stopped denying tools")
	}
	if slices.Contains(spec.Args, "--permission-mode") {
		t.Error("read posture sets a permission mode; the deny list is the mechanism there")
	}
}

// TestCodexPostureIsPerOS pins BOTH branches, which is the whole point of
// threading the OS in rather than reading runtime.GOOS inside the branch.
//
// This test replaces TestCodexWritePostureAlsoUnbreaksIt, which asserted the
// flag `workspace-write` and named, in its own title, an effect nobody had run.
// On 2026-08-04 the effect was run: workspace-write fails every process spawn
// on Windows exactly as read-only does. The old test passed throughout, because
// it checked the argv it was given rather than the sentence it was named for —
// this file's oldest lesson, wearing the newest costume.
func TestCodexPostureIsPerOS(t *testing.T) {
	// Off Windows the OS sandbox is real, so the two postures stay graded.
	if got := codexSandboxFor(PostureRead, false); got != "read-only" {
		t.Errorf("unix read posture = %q, want read-only (enforced there)", got)
	}
	if got := codexSandboxFor(PostureWrite, false); got != "workspace-write" {
		t.Errorf("unix write posture = %q, want workspace-write", got)
	}
	// danger-full-access must never leak onto a platform where a real sandbox
	// works. It is a Windows-only concession, not this adapter's new default.
	for _, p := range []Posture{PostureRead, PostureWrite} {
		if got := codexSandboxFor(p, false); got == "danger-full-access" {
			t.Errorf("unix posture %v removed the sandbox entirely", p)
		}
	}

	// On Windows both collapse to the only mode that can spawn a process at
	// all. Read is the branch San's complaint was about: a read seat that
	// answered "I could not inspect the repository".
	for _, p := range []Posture{PostureRead, PostureWrite} {
		if got := codexSandboxFor(p, true); got != "danger-full-access" {
			t.Errorf("windows posture %v = %q, want danger-full-access — every other mode fails to spawn", p, got)
		}
	}
}

// TestCodexResumeCarriesTheSamePostureAsSpawn: the two shapes take different
// flags (-s is rejected by resume, -c is not), so they are the classic place
// for a posture to drift. A resume that carried a weaker posture would give the
// seat a different capability on turn 2 than on turn 1, and the symptom is a
// column that answers once and then goes quiet.
func TestCodexResumeCarriesTheSamePostureAsSpawn(t *testing.T) {
	for _, windows := range []bool{true, false} {
		for _, p := range []Posture{PostureRead, PostureWrite} {
			want := `sandbox_mode="` + codexSandboxFor(p, windows) + `"`
			if got := codexResumeOverrideFor(p, windows); got != want {
				t.Errorf("resume override (windows=%v, posture=%v) = %q, want %q", windows, p, got, want)
			}
		}
	}

	// And the wiring, on whatever OS this test is running: the spec-building
	// path has to reach those functions rather than hardcode a mode beside them.
	spec, _ := Codex{}.FirstTurn("brief", `C:\ws`, "codex.cmd", PostureWrite)
	i := slices.Index(spec.Args, "-s")
	if i < 0 || spec.Args[i+1] != sandboxFor(PostureWrite) {
		t.Fatalf("first turn sandbox = %v, want %q", spec.Args, sandboxFor(PostureWrite))
	}
	// Resume must never grow -s back: the CLI answers `error: unexpected
	// argument '-s' found` and the turn dies at argument parsing with nothing
	// on stdout, so the column blanks for a reason no card can explain.
	res, _ := Codex{}.NextTurn("brief", `C:\ws`, "codex.cmd", "sess-1", PostureWrite)
	if slices.Contains(res.Args, "-s") {
		t.Error("resume passed -s, which the CLI rejects outright")
	}
	if !slices.ContainsFunc(res.Args, func(a string) bool {
		return strings.Contains(a, sandboxFor(PostureWrite))
	}) {
		t.Errorf("resume did not carry the write posture: %v", res.Args)
	}
}

// TestAgyWritePostureDropsTheNudges: --mode plan and --sandbox were only ever a
// read-only-leaning nudge, and ADR-012 records that --sandbox scopes TERMINAL
// access rather than writes. Keeping them in write mode would restrict
// something the user just asked to widen while bounding nothing they asked to
// bound.
func TestAgyWritePostureDropsTheNudges(t *testing.T) {
	read, _ := Antigravity{}.FirstTurn("brief", `C:\ws`, "agy.exe", PostureRead)
	if !slices.Contains(read.Args, "--sandbox") {
		t.Error("read posture dropped the nudge flags")
	}

	write, _ := Antigravity{}.FirstTurn("brief", `C:\ws`, "agy.exe", PostureWrite)
	if slices.Contains(write.Args, "--sandbox") || slices.Contains(write.Args, "plan") {
		t.Error("write posture kept flags that restrict terminals without bounding writes")
	}
	// The escape hatch stays unmade in BOTH postures. agy's print mode
	// auto-denies approval-needing tools and points at this flag; passing it
	// would auto-approve every tool request, which is a bigger grant than
	// "--write" asks for and is San's call, not this adapter's.
	if slices.Contains(write.Args, "--dangerously-skip-permissions") {
		t.Error("write posture silently enabled --dangerously-skip-permissions")
	}
}

// TestPromptStaysOffArgvInBothPostures: the shim safety rule is not a
// read-mode-only property.
func TestPromptStaysOffArgvInBothPostures(t *testing.T) {
	for _, p := range []Posture{PostureRead, PostureWrite} {
		spec, _ := Codex{}.FirstTurn("a \"quoted\" & piped brief", `C:\ws`, "codex.cmd", p)
		if spec.StdinPrompt == "" {
			t.Errorf("posture %v put the prompt somewhere other than stdin", p)
		}
		for _, a := range spec.Args {
			if strings.Contains(a, "quoted") {
				t.Errorf("posture %v leaked prompt text into argv: %q", p, a)
			}
		}
	}
}
