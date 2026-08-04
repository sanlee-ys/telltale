package council

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestKindOfClassifiesShims is the Windows correctness case, not a style
// preference: Go's os/exec runs .cmd and .bat through cmd.exe, whose argument
// parsing cannot be safely quoted for arbitrary text. Misclassifying one of
// these as native is how a prompt containing a quote or an ampersand stops
// being a prompt.
func TestKindOfClassifiesShims(t *testing.T) {
	cases := map[string]BinaryKind{
		`C:\Users\dev\AppData\Roaming\npm\codex.cmd`: KindShim,
		`C:\Users\dev\AppData\Roaming\npm\codex.CMD`: KindShim,
		`C:\tools\wrapper.bat`:                       KindShim,
		`C:\Users\dev\.local\bin\claude.exe`:         KindNative,
		`/usr/local/bin/claude`:                      KindNative,
		`/home/dev/.local/bin/agy`:                   KindNative,
	}
	for path, want := range cases {
		if got := kindOf(path); got != want {
			t.Errorf("kindOf(%q) = %v, want %v", path, got, want)
		}
	}
}

// TestShimWithoutStdinIsRefused: a vendor that can only take its prompt as an
// argument, resolved to a shim, must be marked unusable rather than driven. The
// refusal is the feature.
func TestShimWithoutStdinIsRefused(t *testing.T) {
	info := classify(
		VendorInfo{Vendor: model.VendorAntigravity, Label: "Antigravity", Binary: `C:\npm\agy.cmd`, Kind: KindShim},
		candidate{vendor: model.VendorAntigravity, envVar: "TELLTALE_COUNCIL_AGY_BIN", stdinPrompt: false},
	)
	if info.Avail != AvailUnusable {
		t.Fatalf("shim + argv-only prompt = %v, want AvailUnusable", info.Avail)
	}
	if !strings.Contains(info.Note, "TELLTALE_COUNCIL_AGY_BIN") {
		t.Errorf("the note does not say how to fix it: %q", info.Note)
	}
}

// TestShimWithStdinIsSeated is the Codex case: npm installs it as codex.cmd,
// and it is drivable anyway because the prompt goes on stdin, so only fixed
// metacharacter-free flags ever cross cmd.exe.
func TestShimWithStdinIsSeated(t *testing.T) {
	info := classify(
		VendorInfo{Vendor: model.VendorCodex, Label: "Codex", Binary: `C:\npm\codex.cmd`, Kind: KindShim},
		candidate{vendor: model.VendorCodex, envVar: "TELLTALE_COUNCIL_CODEX_BIN", stdinPrompt: true},
	)
	if info.Avail != AvailInstalled {
		t.Fatalf("shim + stdin prompt = %v, want AvailInstalled", info.Avail)
	}
}

func TestEnvOverridePointingAtNothingIsReported(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "not-here")
	t.Setenv("TELLTALE_COUNCIL_CLAUDE_BIN", missing)

	info := detectOne(candidates()[0])
	if info.Avail != AvailNotInstalled {
		t.Fatalf("override at a missing path = %v, want AvailNotInstalled", info.Avail)
	}
	if !strings.Contains(info.Note, "does not exist") {
		t.Errorf("the note does not explain the override failed: %q", info.Note)
	}
}

func TestEnvOverrideIsHonoured(t *testing.T) {
	dir := t.TempDir()
	name := "fake-claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELLTALE_COUNCIL_CLAUDE_BIN", path)

	info := detectOne(candidates()[0])
	if info.Avail != AvailInstalled {
		t.Fatalf("override at an existing path = %v, want AvailInstalled", info.Avail)
	}
	if info.Binary != path {
		t.Errorf("Binary = %q, want the override %q", info.Binary, path)
	}
}

// TestDetectNamesEveryMissingVendor: an absent vendor must carry a note saying
// what was looked for. A blank card would be the same failure as a HUD column
// that renders an em dash for two different reasons.
func TestDetectNamesEveryMissingVendor(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, info := range Detect() {
		if info.Avail == AvailInstalled {
			continue
		}
		if info.Note == "" {
			t.Errorf("%s is unavailable with no explanation", info.Vendor)
		}
		if !strings.Contains(info.Note, "PATH") && !strings.Contains(info.Note, "shim") {
			t.Errorf("%s note does not say what went wrong: %q", info.Vendor, info.Note)
		}
	}
}

// TestCursorIsSeatedButOnlyUnderItsAgentName replaces TestCursorIsNotSeated,
// which pinned ADR-008 §7's original ruling. Half of that ruling was right and
// half of it was false, and the new contract has to keep the right half.
//
// Right: `cursor` on PATH is the editor launcher (diff/merge/goto), not an
// agent CLI. Seating that binary would produce a column that fails
// confusingly. `agent` is likewise not claimable — the installer drops an
// agent.cmd next to cursor-agent.cmd, but the name is far too generic to grab
// off a shared PATH.
//
// False: "cursor-agent is not installed". It was, at
// %LOCALAPPDATA%\cursor-agent, and had been since July. The check that produced
// that sentence was a PATH lookup in one shell.
func TestCursorIsSeatedButOnlyUnderItsAgentName(t *testing.T) {
	var cursor *candidate
	for i, c := range candidates() {
		if c.vendor == model.VendorCursor {
			cursor = &candidates()[i]
		}
	}
	if cursor == nil {
		t.Fatal("cursor is not seated; ADR-008 §7 was amended to seat it once cursor-agent was found installed")
	}
	if !slices.Contains(cursor.names, "cursor-agent") {
		t.Errorf("cursor does not look for cursor-agent: %v", cursor.names)
	}
	for _, banned := range []string{"cursor", "agent"} {
		if slices.Contains(cursor.names, banned) {
			t.Errorf("%q is not the agent CLI; claiming it off PATH is how a column fails confusingly", banned)
		}
	}
	if len(cursor.knownPaths) == 0 {
		t.Error("cursor has no known install locations, which is the check that would have caught the original false claim")
	}
	// argv-only, read out of the shipped bundle: print mode's prompt is the
	// variadic positional and no code path reads it from stdin. That is what
	// makes the Windows shim unusable rather than merely awkward.
	if cursor.stdinPrompt {
		t.Error("cursor is marked as taking its prompt on stdin; the bundle has no such path, and driving the .cmd on that belief would put prompt text through cmd.exe")
	}
}

// TestKnownPathResolutionIsHonestAboutWhereItLooked is the detection fix
// itself. A binary that exists but is not on this shell's PATH must resolve,
// and the card must say it came from somewhere else — "not installed" was the
// original false claim and an unlabelled find would be the next one.
func TestKnownPathResolutionIsHonestAboutWhereItLooked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", t.TempDir()) // a PATH with nothing in it
	name := "fake-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("not a real binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	c := candidate{
		vendor:     model.VendorCursor,
		label:      "Cursor",
		names:      []string{"nothing-by-this-name"},
		knownPaths: []string{path},
		envVar:     "TELLTALE_COUNCIL_CURSOR_BIN",
	}
	info := detectOne(c)
	if info.Avail != AvailInstalled {
		t.Fatalf("a binary off PATH detected as %v, want AvailInstalled", info.Avail)
	}
	if info.Binary != path {
		t.Errorf("Binary = %q, want %q", info.Binary, path)
	}
	if !strings.Contains(info.Source, "PATH") {
		t.Errorf("Source = %q; it must say the binary did not come from PATH", info.Source)
	}
}

// TestMissingVendorNamesTheKnownPathsToo: the absent card has to be falsifiable.
// "not found on PATH" alone is what let the original claim stand — a reader had
// no way to see that nowhere else had been checked.
func TestMissingVendorNamesTheKnownPathsToo(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	c := candidate{
		vendor:     model.VendorCursor,
		names:      []string{"nothing-by-this-name"},
		knownPaths: []string{filepath.Join(t.TempDir(), "also-not-here")},
		envVar:     "TELLTALE_COUNCIL_CURSOR_BIN",
	}
	info := detectOne(c)
	if info.Avail != AvailNotInstalled {
		t.Fatalf("Avail = %v, want AvailNotInstalled", info.Avail)
	}
	if !strings.Contains(info.Note, "also-not-here") {
		t.Errorf("the note does not say which locations were checked: %q", info.Note)
	}
}

// TestUnusableCardNamesTheBinaryItFound: "unusable" is a verdict users
// disbelieve, and the first question is always which binary council means.
func TestUnusableCardNamesTheBinaryItFound(t *testing.T) {
	info := classify(
		VendorInfo{
			Vendor: model.VendorCursor, Label: "Cursor",
			Binary: `C:\Users\dev\AppData\Local\cursor-agent\cursor-agent.cmd`,
			Kind:   KindShim,
			Source: "a known install location, not on this shell's PATH",
		},
		candidate{
			vendor: model.VendorCursor, envVar: "TELLTALE_COUNCIL_CURSOR_BIN",
			stdinPrompt:  false,
			unusableHint: "this install ships no native executable",
		},
	)
	if info.Avail != AvailUnusable {
		t.Fatalf("Avail = %v, want AvailUnusable", info.Avail)
	}
	if !strings.Contains(info.Note, "cursor-agent.cmd") {
		t.Errorf("the note does not name the binary: %q", info.Note)
	}
	if !strings.Contains(info.Note, "known install location") {
		t.Errorf("the note does not say where it came from: %q", info.Note)
	}
	// The generic "point the env var at the real executable" advice is wrong
	// for a vendor that ships no such executable, and advice that cannot be
	// followed is worse than none.
	if !strings.Contains(info.Note, "no native executable") {
		t.Errorf("the per-vendor hint was dropped for the generic one: %q", info.Note)
	}
}

// TestCursorClaimsNoMoreThanRequested. This is the weakest-evidence column in
// the room and the badge has to stay at "requested": the CLI is not signed in
// here and checks authentication before it validates flags, so no invocation
// ever got far enough to demonstrate or refute a posture.
func TestCursorClaimsNoMoreThanRequested(t *testing.T) {
	for _, win := range []bool{true, false} {
		claim := sandboxFor(model.VendorCursor, win)
		if claim.Level != SandboxRequested {
			t.Errorf("cursor (windows=%v) claims %v, want SandboxRequested", win, claim.Level)
		}
		if claim.Detail == "" {
			t.Errorf("cursor (windows=%v) has a badge with no explanation behind it", win)
		}
		// The detail must carry WHY this claim is weaker than the others, not
		// merely that a flag was passed.
		if !strings.Contains(claim.Detail, "not signed in") {
			t.Errorf("the detail does not say why nothing could be measured: %q", claim.Detail)
		}
	}
}

// TestCursorGranularityIsNotClaimed. --stream-partial-output's help promises
// "individual text deltas" and the shipped bundle does emit per chunk, but
// neither is a measurement — and Antigravity is the standing proof that a field
// named text_delta can carry a whole reply at once. No word is printed for this
// column until someone watches the pipe.
func TestCursorGranularityIsNotClaimed(t *testing.T) {
	if got := granularityFor(model.VendorCursor); got != GranUnknown {
		t.Errorf("cursor = %v, want GranUnknown — nothing about its streaming was observed", got)
	}
	if got := granularityFor(model.VendorCursor).String(); got != "" {
		t.Errorf("cursor prints %q in the column header; an unestablished granularity must print nothing", got)
	}
}

// TestNoVendorClaimsUnverifiedEnforcement is the ADR-008 §3 correction, pinned.
//
// Codex may only claim OS-level enforcement where it has been verified. On
// Windows it must downgrade to "requested" — claiming a sandbox we have not
// seen engage is exactly the overstatement this repo refuses.
func TestNoVendorClaimsUnverifiedEnforcement(t *testing.T) {
	if got := sandboxFor(model.VendorCodex, true).Level; got != SandboxRequested {
		t.Errorf("codex on windows claims %v, want SandboxRequested", got)
	}
	if got := sandboxFor(model.VendorCodex, false).Level; got != SandboxEnforced {
		t.Errorf("codex on unix claims %v, want SandboxEnforced", got)
	}
	// Antigravity is the strong case: not "unverified" but REFUTED. Asked to
	// write a file under --mode plan --sandbox, it wrote the file. Anything
	// above SandboxNone would put a read-only posture on a column measured not
	// to have one, and `ro:requested` reads at a glance as "some read-only
	// posture" — which is exactly the glance this badge must not survive.
	for _, win := range []bool{true, false} {
		if got := sandboxFor(model.VendorAntigravity, win).Level; got != SandboxNone {
			t.Errorf("agy claims %v, want SandboxNone — its flags were measured not to restrict it", got)
		}
	}
	if got := sandboxFor(model.VendorAntigravity, true).Badge(); strings.HasPrefix(got, "ro:") {
		t.Errorf("agy badge is %q; a vendor that can write must not wear an ro: prefix", got)
	}
	// Claude's mechanism is real but is a tool allowlist, not an OS sandbox,
	// and the badge must not imply otherwise.
	if got := sandboxFor(model.VendorClaude, true).Level; got != SandboxTools {
		t.Errorf("claude claims %v, want SandboxTools", got)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity} {
		for _, win := range []bool{true, false} {
			if sandboxFor(v, win).Detail == "" {
				t.Errorf("%s (windows=%v) has a badge with no explanation behind it", v, win)
			}
		}
	}
}

// TestGranularityMatchesWhatWasMeasured. All three were run; two of them are
// worse than the provisional guess and must say so, because a column labelled
// as streaming that sits silent for a minute is a lie the user has no way to
// check.
func TestGranularityMatchesWhatWasMeasured(t *testing.T) {
	if got := granularityFor(model.VendorClaude); got != GranTokens {
		t.Errorf("claude = %v, want GranTokens — token deltas were observed live", got)
	}
	// Codex emits one item per COMPLETE message; Antigravity emits a whole
	// response as a single delta. Neither streams in any sense a user would
	// recognise, and both must land in PhaseWaiting rather than PhaseStreaming.
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity} {
		if got := granularityFor(v); got != GranFinalOnly {
			t.Errorf("%s = %v, want GranFinalOnly — measured to produce nothing until the turn ends", v, got)
		}
	}
}
