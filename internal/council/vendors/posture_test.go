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

// TestUngatedWritePostureCanLandWork.
//
// acceptEdits covers edits and nothing else, so this posture produced a seat
// that wrote files all session and could not commit one: every git call raised
// an approval request, and --auto is precisely the posture with nobody to answer
// it. The room stopped one step short of the step that makes work exist for
// anyone but the machine it ran on.
//
// The assertions are about the ARGV, and this test says so rather than claiming
// the effect. The rule syntax has not been driven against a live CLI; if it is
// wrong the seat goes back to asking and landing nothing, which is where it was.
func TestUngatedWritePostureCanLandWork(t *testing.T) {
	spec, err := Claude{}.FirstTurn("brief", `C:\ws`, "claude", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(spec.Args, "--allowedTools")
	if i < 0 || i == len(spec.Args)-1 {
		t.Fatal("ungated write posture pre-approves nothing, so the seat cannot commit what it just wrote")
	}
	rules := spec.Args[i+1]
	for _, want := range []string{"git add", "git commit", "git push"} {
		if !strings.Contains(rules, want) {
			t.Errorf("missing %q: the seat can build and cannot land", want)
		}
	}
	// Scoped to landing work, not to git. This is the posture where nobody is
	// watching, and rewriting existing history is a different grant from
	// committing what was just built.
	for _, never := range []string{"git reset", "git clean", "branch -D"} {
		if strings.Contains(rules, never) {
			t.Errorf("%q is pre-approved in the unattended posture", never)
		}
	}
}

// FirstTurn is the spawn-per-turn path, which is never handed a gate-hook file
// — so it gets the fallback posture, and that is the safe default rather than
// an oversight: a caller that has not been taught about the hook must not
// launch a seat keeping the operator's allow rules with nothing in front of
// them. The persistent path is where the hook rides; see
// TestAWiredGateHookKeepsTheOperatorsSettings.
func TestGatedPostureLeavesCommandClassificationToCouncil(t *testing.T) {
	spec, _ := Claude{}.FirstTurn("brief", `C:\ws`, "claude", PostureWriteGated)
	if !slices.Contains(spec.Args, "--setting-sources") {
		t.Error("the gated seat kept the user's allow rules, so the gate sits behind them")
	}
	if slices.Contains(spec.Args, "--allowedTools") {
		t.Error("the gated seat bypasses Council's safe/destructive command classifier")
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
// On 2026-08-04 the effect was run: workspace-write failed every process spawn
// on Windows exactly as read-only did, at codex-cli 0.146.0. The old test
// passed throughout, because it checked the argv it was given rather than the
// sentence it was named for — this file's oldest lesson, wearing the newest
// costume.
//
// The Windows half moved on 2026-08-29, and by measurement rather than by
// hope: at codex-cli 0.149.1 `-s read-only` enforces there (a shell write was
// denied with no file on disk, a read ran clean), so the read posture is one
// value on every OS. The write posture stays split — workspace-write also
// enforces on Windows now, but its .git deny cannot be bought back by the
// writable_roots override there, so a seat under it edits and never commits.
// Both measurements are recorded on the constants in codex.go.
func TestCodexPostureIsPerOS(t *testing.T) {
	// The read posture asks for the real sandbox EVERYWHERE. Until 2026-08-29
	// the Windows branch was danger-full-access, and a regression back to it
	// would put a working sandbox back on the loudest flag in the room.
	for _, windows := range []bool{true, false} {
		if got := codexSandboxFor(PostureRead, windows); got != "read-only" {
			t.Errorf("read posture (windows=%v) = %q, want read-only — measured enforcing on both branches", windows, got)
		}
	}
	if got := codexSandboxFor(PostureWrite, false); got != "workspace-write" {
		t.Errorf("unix write posture = %q, want workspace-write", got)
	}
	// danger-full-access must never leak onto a platform where the graded mode
	// can land work. It is a Windows-write-only concession, not this adapter's
	// default.
	for _, p := range []Posture{PostureRead, PostureWrite} {
		if got := codexSandboxFor(p, false); got == "danger-full-access" {
			t.Errorf("unix posture %v removed the sandbox entirely", p)
		}
	}

	// Windows write posture: the one place the loud flag remains, because a
	// workspace-write seat there cannot write .git even with the override, and
	// a seat that cannot commit builds and never lands.
	if got := codexSandboxFor(PostureWrite, true); got != "danger-full-access" {
		t.Errorf("windows write posture = %q, want danger-full-access — workspace-write cannot reach .git there", got)
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

// TestAgyAsksForNothingInEitherPosture.
//
// Renamed from TestAgyWritePostureDropsTheNudges, which pinned the read posture
// as still carrying --mode plan and --sandbox. Those "nudges" were measured to
// restrict nothing — asked to write a file under both, agy wrote it — and the
// one effect either flag was ever observed to have was a run_command refusal
// that killed the whole turn and left an empty column. The owner ruled them off
// on 2026-08-04 (ADR-008, seventeenth amendment; design.md §9.6b's open
// decision, closed).
//
// So this seat's invocation no longer varies by posture at all, and this test
// pins that rather than the old asymmetry: a flag reappearing on either side is
// council asking for a restriction it has measured to be worthless and seen kill
// a turn. The badge is unaffected either way — it was already `unsandboxed`,
// because a claim about what restricts this column was never a claim about which
// flags were sent.
func TestAgyAsksForNothingInEitherPosture(t *testing.T) {
	read, _ := Antigravity{}.FirstTurn("brief", `C:\ws`, "agy.exe", PostureRead)
	if slices.Contains(read.Args, "--sandbox") || slices.Contains(read.Args, "plan") {
		t.Errorf("read posture still asks for a refuted restriction: %v", read.Args)
	}
	resume, err := Antigravity{}.NextTurn("brief", `C:\ws`, "agy.exe", "conv-1", PostureRead)
	if err != nil {
		t.Fatal(err)
	}
	// The resume path builds from the same baseArgs, and this is where a
	// posture drifts if it is ever going to: ADR-008's twelfth amendment
	// records exactly that failure on the Codex seat, where spawn and resume
	// take different flags.
	if slices.Contains(resume.Args, "--sandbox") || slices.Contains(resume.Args, "plan") {
		t.Errorf("the resume path still carries the dropped flags: %v", resume.Args)
	}

	write, _ := Antigravity{}.FirstTurn("brief", `C:\ws`, "agy.exe", PostureWrite)
	if slices.Contains(write.Args, "--sandbox") || slices.Contains(write.Args, "plan") {
		t.Error("write posture kept flags that restrict terminals without bounding writes")
	}
	if !slices.Equal(read.Args, write.Args) {
		t.Errorf("this seat's postures diverged: read %v vs write %v", read.Args, write.Args)
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

// TestCodexWritePostureCanReachDotGit.
//
// MEASURED 2026-08-05, codex-cli 0.146.0, macOS, with a control: under
// `-s workspace-write` a write to .git/ came back "Operation not permitted"
// while a write to the workspace root succeeded, and adding
// sandbox_workspace_write.writable_roots=["<ws>/.git"] made the .git write
// succeed. Codex's seatbelt carves .git out of the writable workspace, so the
// seat could edit files all session and never commit one.
//
// Asserted on BOTH shapes, because spawn and resume take different flags and
// this file's recorded failure mode is a posture that drifts between them: a
// seat that lands work on turn 1 and refuses on turn 2.
func TestCodexWritePostureCanReachDotGit(t *testing.T) {
	const ws = "/home/dev/ws"

	spawn, err := Codex{}.FirstTurn("brief", ws, "codex", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(spawn.Args, gitWritableOverride(ws)) {
		t.Errorf("spawn cannot write .git, so it can build and never land:\n%v", spawn.Args)
	}

	res, err := Codex{}.NextTurn("brief", ws, "codex", "sess-1", PostureWrite)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(res.Args, gitWritableOverride(ws)) {
		t.Errorf("resume dropped the .git widening: the seat commits on turn 1 and refuses on turn 2:\n%v", res.Args)
	}

	// Read posture never gets it. A seat with nothing to land has no reason to
	// reach the one directory that holds history rather than working files.
	ro, _ := Codex{}.FirstTurn("brief", ws, "codex", PostureRead)
	for _, a := range ro.Args {
		if strings.Contains(a, "writable_roots") {
			t.Errorf("read posture widened the sandbox: %q", a)
		}
	}

	// Scoped to THIS workspace's .git, never a bare directory name or a parent.
	if got := gitWritableOverride(ws); !strings.Contains(got, ws+"/.git") {
		t.Errorf("override is not anchored to the workspace: %q", got)
	}
}
