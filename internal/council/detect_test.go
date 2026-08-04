package council

import (
	"os"
	"path/filepath"
	"runtime"
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

// TestCursorIsNotSeated pins ADR-008 §7. `cursor` on PATH is the editor
// launcher, not an agent CLI; seating it would produce a column that fails
// confusingly instead of an honest empty seat.
func TestCursorIsNotSeated(t *testing.T) {
	for _, c := range candidates() {
		if c.vendor == model.VendorCursor {
			t.Fatal("cursor is seated; ADR-008 §7 scopes it out of v1 until cursor-agent is installed")
		}
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
	if got := sandboxFor(model.VendorAntigravity, false).Level; got != SandboxRequested {
		t.Errorf("agy claims %v, want SandboxRequested — its flags are unverified", got)
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

// TestGranularityIsNotOverclaimed: only Claude's token-level streaming is
// verified. Guessing high would put a "streaming" label on a silent column.
func TestGranularityIsNotOverclaimed(t *testing.T) {
	if granularityFor(model.VendorClaude) != GranTokens {
		t.Error("claude should be GranTokens — its stream-json deltas are documented")
	}
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity} {
		if granularityFor(v) == GranTokens {
			t.Errorf("%s claims token-level streaming, which is not verified", v)
		}
	}
}
