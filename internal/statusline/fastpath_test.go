package statusline

import (
	"os/exec"
	"strings"
	"testing"
)

// fastPathPkg is the package whose import graph carries ADR-002's fast-path
// rule. The rule is about THIS graph, not about `cmd/telltale`'s — see the
// comment on TestFastPathNeverReachesTUIFramework for why that distinction is
// the whole of the claim.
const fastPathPkg = "github.com/sanlee-ys/telltale/internal/statusline"

// tuiModules are the two modules ADR-002 keeps off the fast path. They are
// matched as import-path prefixes rather than by module name, because a
// package can arrive from a subdirectory (`charm.land/lipgloss/v2/table`) that
// shares no name with the module root.
var tuiModules = []string{"charm.land/bubbletea", "charm.land/lipgloss"}

// TestFastPathNeverReachesTUIFramework turns ADR-002's fast-path rule from a
// convention a reviewer has to remember into a gate the build runs.
//
// The rule as CLAUDE.md and design.md §7.5 state it: `internal/theme` stays
// stdlib-only so the statusline never initializes Bubble Tea. Until this test
// existed the rule was enforced by nobody — a single `import "charm.land/
// lipgloss/v2"` added to `internal/theme` or `internal/model` for one
// convenient helper would have compiled, passed every golden, and quietly put
// the TUI framework's package init on a path that runs on every prompt.
//
// What this asserts is the PACKAGE GRAPH, and that is deliberate. The shipped
// artifact is one binary (§1), so `telltale.exe` does link both modules —
// `go version -m telltale.exe` lists `charm.land/bubbletea/v2 v2.0.8` and
// `charm.land/lipgloss/v2 v2.0.5`, and §9.8's hook measurement already
// recorded that the 14 MB binary carries them. `telltale hud` is in that
// binary and cannot not be. So an assertion over the binary's modules would
// fail today and would be asserting the wrong thing anyway. What ADR-002 buys
// is that the code REACHED by `telltale statusline` never touches the
// framework: no lipgloss renderer is constructed, no bubbletea program is
// started, and neither module's package-level init runs on that path. The
// import graph of this package is exactly that claim, and it is deterministic
// where a timing assertion is not (see the latency step in ci.yml, which
// reports rather than pins).
//
// This shells out to `go list` rather than walking `go/build` by hand because
// `go list` is the toolchain's own answer to "what does this package import,
// transitively" — it honours build tags, the module graph and the current
// GOOS, none of which a hand-rolled walker gets right. `go test` is itself
// launched by that toolchain, so the binary is on PATH by construction; a
// missing `go` is a broken environment, not a reason to skip. A skipped gate
// reads as a pass, which is the failure mode this repo's honesty rules
// (§4a.1) exist to prevent.
func TestFastPathNeverReachesTUIFramework(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", fastPathPkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", fastPathPkg, err)
	}
	deps := strings.Fields(string(out))
	if len(deps) == 0 {
		t.Fatalf("go list -deps %s returned nothing; the gate would pass vacuously", fastPathPkg)
	}

	var found []string
	for _, dep := range deps {
		for _, mod := range tuiModules {
			if dep == mod || strings.HasPrefix(dep, mod+"/") {
				found = append(found, dep)
			}
		}
	}
	if len(found) > 0 {
		t.Errorf("ADR-002 violation: the statusline fast path now reaches the TUI framework via %s.\n"+
			"Bubble Tea/Lipgloss must stay out of %s's import graph — `internal/theme` and\n"+
			"`internal/model` are stdlib-only precisely so this path never runs their package init.\n"+
			"Full graph: %d packages.",
			strings.Join(found, ", "), fastPathPkg, len(deps))
	}
}
