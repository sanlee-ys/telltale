package history

import (
	"os/exec"
	"strings"
	"testing"
)

// The gates in this file turn this mode's read/write boundary claims from
// sentences in CLAUDE.md and this package's doc into something the build runs.
//
// Both shell out to `go list`, on internal/eventview/boundary_test.go's
// precedent: it is the toolchain's own answer to what a package imports,
// honouring build tags, the module graph and the current GOOS, none of which a
// hand-rolled walker gets right. `go test` is launched by that toolchain, so the
// binary is on PATH by construction. A missing `go` is a broken environment, not
// a reason to skip, and a skipped gate reads as a pass.

const historyPkg = "github.com/sanlee-ys/telltale/internal/history"

// writerPkgs are every package in this repository that WRITES under
// ~/.telltale/. The read/write boundary enumerates its writers by name and this
// mode is not one of them, so reaching any of these from here would silently
// add another to that list.
//
// The list has no count in it on purpose. It gained `internal/probe` on
// 2026-09-04, and a comment that had said "three bounded exceptions" would have
// gone on saying three with four in the slice under it.
//
// The check is on DIRECT imports, and that is the right scope rather than a
// weaker one. A transitive assertion would fail today for a reason that is not a
// violation: this package imports internal/adapter/claudecode for Discover, and
// an adapter's dependency graph is not this mode's write surface. What this gate
// catches is the likely drift — this package growing a cache of its own, or
// relaying a figure it just rendered, both of which are one direct import.
var writerPkgs = []string{
	"github.com/sanlee-ys/telltale/internal/quotacache",
	"github.com/sanlee-ys/telltale/internal/usagecache",
	"github.com/sanlee-ys/telltale/internal/eventsink",
	"github.com/sanlee-ys/telltale/internal/eventview",
	"github.com/sanlee-ys/telltale/internal/council",
	"github.com/sanlee-ys/telltale/internal/probe",
}

// TestHistoryWritesNothing pins the sentence this package's doc opens its
// boundary section with, and the one CLAUDE.md now names this mode in.
//
// `telltale history` joins statusline, hud, snapshot and mcp as a reader. It
// relays no quota — it renders none, which is snapshot's argument for holding
// the contract with one item spare — and it keeps no cache of its own, because
// a ledger cached is a ledger that can disagree with the files it came from.
func TestHistoryWritesNothing(t *testing.T) {
	imports := listImports(t, historyPkg)
	for _, imp := range imports {
		for _, w := range writerPkgs {
			if imp == w {
				t.Errorf("read/write boundary violation: internal/history imports %s.\n"+
					"This mode writes NOTHING. CLAUDE.md's boundary section enumerates the three\n"+
					"bounded writers by name and this is not one of them; adding a fourth here\n"+
					"would make that list wrong without a word of complaint from the compiler.", w)
			}
		}
	}
}

// TestHistoryOpensNoSocket pins the other half: this mode reads files and makes
// no network call, binds no port, and reads no credential store.
//
// `os` and `os/exec` are the two ways a reader turns into something else here,
// and only the first is legitimate — this package opens transcripts. An exec
// would be this mode shelling out to a vendor CLI, which is council's exception
// and nobody else's.
func TestHistoryOpensNoSocket(t *testing.T) {
	for _, imp := range listImports(t, historyPkg) {
		if imp == "net" || strings.HasPrefix(imp, "net/") {
			t.Errorf("internal/history imports %s. This mode reads one vendor's own files "+
				"and calls no network.", imp)
		}
		if imp == "os/exec" {
			t.Errorf("internal/history imports os/exec. Spawning a vendor is council's " +
				"exception and nobody else's.")
		}
	}
}

// TestHistoryStaysOffTheFastPath. internal/theme and internal/model must not
// reach a TUI framework (ADR-002), and the reverse matters here: this mode
// renders plain text and must not pull Bubble Tea or Lipgloss in behind it, or
// the "no colour to switch off" claim in its own doc and in the help stops being
// structurally true and becomes a promise somebody has to keep by hand.
func TestHistoryStaysOffTheFastPath(t *testing.T) {
	for _, dep := range listDeps(t, historyPkg) {
		if strings.Contains(dep, "charmbracelet") {
			t.Errorf("internal/history reaches %s. This mode is plain text by design "+
				"(see view.go): every distinction it draws is a WORD, which is what makes "+
				"--ascii and NO_COLOR meaningless here rather than unimplemented.", dep)
		}
	}
}

func listImports(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", pkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", pkg, err)
	}
	imports := strings.Fields(string(out))
	if len(imports) == 0 {
		t.Fatalf("go list returned no imports for %s; the gate would pass vacuously", pkg)
	}
	return imports
}

func listDeps(t *testing.T, pkg string) []string {
	t.Helper()
	out, err := exec.Command("go", "list", "-deps", pkg).Output()
	if err != nil {
		t.Fatalf("go list -deps %s: %v", pkg, err)
	}
	deps := strings.Fields(string(out))
	if len(deps) == 0 {
		t.Fatalf("go list -deps %s returned nothing; the gate would pass vacuously", pkg)
	}
	return deps
}
