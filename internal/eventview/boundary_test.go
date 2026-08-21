package eventview

import (
	"os/exec"
	"strings"
	"testing"
)

// The two gates in this file turn the read/write boundary's claims about this
// mode from sentences in CLAUDE.md into something the build runs.
//
// Both shell out to `go list` for the reason internal/statusline's
// TestFastPathNeverReachesTUIFramework does: it is the toolchain's own answer
// to what a package imports, honouring build tags, the module graph and the
// current GOOS, none of which a hand-rolled walker gets right. `go test` is
// launched by that toolchain, so the binary is on PATH by construction. A
// missing `go` is a broken environment, not a reason to skip, and a skipped
// gate reads as a pass.

const (
	sinkPkg = "github.com/sanlee-ys/telltale/internal/eventsink"
	viewPkg = "github.com/sanlee-ys/telltale/internal/eventview"
)

// gaugePkgs are the four read surfaces the boundary names. The statusline and
// the HUD are the gauges; snapshot is the third reader of the same scan and
// holds the same contract with one item spare (CLAUDE.md), and mcpserver is the
// fourth — it serves snapshot's own document to an agent (design.md §7.25), so
// an import of the event store there would put verbatim hook content in front
// of a model.
var gaugePkgs = []string{
	"github.com/sanlee-ys/telltale/internal/hud",
	"github.com/sanlee-ys/telltale/internal/statusline",
	"github.com/sanlee-ys/telltale/internal/snapshot",
	"github.com/sanlee-ys/telltale/internal/mcpserver",
}

// TestNoGaugeReadsTheEventStore is the property that lets a viewer exist at
// all.
//
// The event store is the only store under ~/.telltale/ holding hook payloads
// verbatim, and what contains it is scope rather than redaction: the operator
// starts the mode, the sink binds loopback, a web page is not a sender
// (design.md §7.24), and no gauge reads these files. Adding a reader spends
// none of those four only for as long as the reader stays out of the gauges.
// The failure this guards is not malice, it is convenience: one import of
// internal/eventview into internal/hud, to put a "last hook" line on a screen,
// would move verbatim hook content onto a surface that renders on every tick
// and would compile without a word of complaint.
//
// It asserts the transitive graph, so an indirect route through a third
// package fails it too.
func TestNoGaugeReadsTheEventStore(t *testing.T) {
	for _, pkg := range gaugePkgs {
		deps := listDeps(t, pkg)
		for _, dep := range deps {
			if dep == sinkPkg || dep == viewPkg {
				t.Errorf("read/write boundary violation: %s now reaches %s.\n"+
					"The event store holds hook payloads VERBATIM. No gauge may read it —\n"+
					"CLAUDE.md's read/write boundary and design.md §7.21 both rest on that,\n"+
					"and `telltale events view` is a separate foreground mode precisely so\n"+
					"this stays true.", pkg, dep)
			}
		}
	}
}

// TestTheViewerOpensNoSocket pins the trust position stated in this package's
// doc: the viewer reads the store's files and makes no network call.
//
// It reads the DIRECT imports of this package, which is the exact claim.
// A transitive assertion would be the wrong test and would fail today for the
// right reason: this package imports internal/eventsink for the Event type,
// the store's directory and its day-file pattern, and the sink's HTTP server
// lives in that same package. Linking that code is not calling it, the same
// distinction ADR-002's fast-path gate rests on.
//
// What it therefore does NOT cover, said out loud rather than implied: a
// future caller could reach the sink's server through the package this one
// already imports, without importing a net package here. This gate catches the
// likelier drift, which is this package growing an HTTP client of its own for
// /events/recent, and design.md §7.21's amendment records why that client
// would buy nothing.
func TestTheViewerOpensNoSocket(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", viewPkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", viewPkg, err)
	}
	imports := strings.Fields(string(out))
	if len(imports) == 0 {
		t.Fatalf("go list returned no imports for %s; the gate would pass vacuously", viewPkg)
	}
	for _, imp := range imports {
		if imp == "net" || strings.HasPrefix(imp, "net/") {
			t.Errorf("the viewer imports %s. This mode reads the sink's day files and opens\n"+
				"no socket: design.md §7.24 already ruled a local program's file read and its\n"+
				"loopback request equally trusted, and only the file read answers with no sink\n"+
				"running. If a socket is genuinely needed, that amendment is the thing to change\n"+
				"first.", imp)
		}
	}
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
