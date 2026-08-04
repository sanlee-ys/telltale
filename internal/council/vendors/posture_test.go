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

// TestCodexWritePostureAlsoUnbreaksIt: under read-only on Windows every
// sandboxed spawn fails, including one asked merely to list a directory — so
// the read posture costs this vendor the ability to run anything at all.
func TestCodexWritePostureAlsoUnbreaksIt(t *testing.T) {
	spec, _ := Codex{}.FirstTurn("brief", `C:\ws`, "codex.cmd", PostureWrite)
	i := slices.Index(spec.Args, "-s")
	if i < 0 || spec.Args[i+1] != "workspace-write" {
		t.Fatalf("write posture sandbox = %v, want workspace-write", spec.Args)
	}
	// workspace-write rather than danger-full-access: the flag should agree
	// with the boundary council actually offers, not remove it.
	if slices.Contains(spec.Args, "danger-full-access") {
		t.Error("write posture removed the workspace boundary entirely")
	}

	// Resume carries the posture through -c, because resume rejects -s.
	res, _ := Codex{}.NextTurn("brief", `C:\ws`, "codex.cmd", "sess-1", PostureWrite)
	if !slices.ContainsFunc(res.Args, func(a string) bool {
		return strings.Contains(a, "workspace-write")
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
