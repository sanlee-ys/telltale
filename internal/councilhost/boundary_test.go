package councilhost

import (
	"os/exec"
	"strings"
	"testing"
)

// The gates in this file turn design.md §7.28's claims about the gauges from
// sentences into something the build runs.
//
// Both shell out to `go list` for the reason internal/eventview's
// TestNoGaugeReadsTheEventStore does, and for the reason
// internal/statusline's TestFastPathNeverReachesTUIFramework did before it: it
// is the toolchain's own answer to what a package imports, honouring build
// tags, the module graph and the current GOOS, none of which a hand-rolled
// walker gets right. `go test` is launched by that toolchain, so `go` is on
// PATH by construction. A missing `go` is a broken environment, not a reason to
// skip, and a skipped gate reads as a pass.

const hostPkg = "github.com/sanlee-ys/telltale/internal/councilhost"

// gaugePkgs are the read surfaces the boundary names: the two gauges, plus
// snapshot and mcpserver, which hold the same contract with one item spare, and
// history, which is the fifth reader.
var gaugePkgs = []string{
	"github.com/sanlee-ys/telltale/internal/hud",
	"github.com/sanlee-ys/telltale/internal/statusline",
	"github.com/sanlee-ys/telltale/internal/snapshot",
	"github.com/sanlee-ys/telltale/internal/mcpserver",
	"github.com/sanlee-ys/telltale/internal/history",
}

// TestNoGaugeReachesTheCouncilHost is the argument §7.28 rests its whole thesis
// answer on, asserted rather than promised.
//
// The case that this change is compatible with "telltale never writes" is that
// the THESIS is a gauge thesis: statusline, hud, snapshot, mcp and history are
// the reads-never-writes product, and a council host does not touch one line of
// them. That sentence is only true for as long as nobody imports this package
// into one of them, and the failure would not be malice — it is convenience. One
// import into internal/hud, to put a "hosted room" row on a screen, would give a
// surface that renders on every tick a handle on a process that spawns agents,
// and it would compile without a word of complaint.
//
// It asserts the TRANSITIVE graph, so an indirect route through a third package
// fails it too.
func TestNoGaugeReachesTheCouncilHost(t *testing.T) {
	for _, pkg := range gaugePkgs {
		for _, dep := range listDeps(t, pkg) {
			if dep == hostPkg {
				t.Errorf("read/write boundary violation: %s now reaches %s.\n"+
					"design.md §7.28's answer to the no-server thesis is that the thesis is a\n"+
					"GAUGE thesis and the host touches none of them. A gauge that can reach the\n"+
					"host can reach a process that spawns vendor CLIs, from a surface that\n"+
					"renders on every prompt.", pkg, dep)
			}
		}
	}
}

// TestTheHostBindsNoPort is the transport ruling, made mechanical.
//
// §7.28 refuses loopback TCP on §7.24's measurement: a web page reached a
// loopback listener, and this socket carries transcript content and accepts
// dispatch commands, so it is a strictly worse surface to leave addressable. The
// whole force of that refusal is that a browser has no URL scheme for
// `\\.\pipe\...` — which stops being true the moment this package grows a
// net.Listen.
//
// DIRECT imports, which is the exact claim: this package must not open a socket
// itself. A transitive assertion would be the wrong test and would fail on
// something harmless, the same distinction ADR-002's fast-path gate rests on.
func TestTheHostBindsNoPort(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", hostPkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", hostPkg, err)
	}
	imports := strings.Fields(string(out))
	if len(imports) == 0 {
		t.Fatalf("go list returned no imports for %s; the gate would pass vacuously", hostPkg)
	}
	for _, imp := range imports {
		if imp == "net" || strings.HasPrefix(imp, "net/") {
			t.Errorf("the council host imports %s.\n"+
				"§7.28 refuses loopback TCP on §7.24's measurement, and the force of that\n"+
				"refusal is that a browser cannot address a named pipe AT ALL — which makes\n"+
				"internal/localonly's check unnecessary here rather than merely satisfied. A\n"+
				"socket in this package gives that class of sender a way back in, on a surface\n"+
				"that carries transcript content and accepts dispatch commands.", imp)
		}
	}
}

// TestTheHostDoesNotImportTheRoom pins the dependency direction.
//
// The host takes a roster and a council directory from its caller rather than
// calling council.Detect and council.RoomPath itself. That is not fussiness: it
// is what keeps the direction one-way, so that package council can later become
// the CLIENT — which is where §7.28's last limitation says the next slice goes —
// without a cycle to unpick first.
func TestTheHostDoesNotImportTheRoom(t *testing.T) {
	const roomPkg = "github.com/sanlee-ys/telltale/internal/council"
	for _, dep := range listDeps(t, hostPkg) {
		if dep == roomPkg {
			t.Fatalf("the host reaches %s.\n"+
				"The direction is one-way on purpose: package council becomes the client in\n"+
				"the next slice (§7.28), and an import here would make that a cycle. The host\n"+
				"is HANDED its roster and its council directory; its job is process ownership,\n"+
				"parsing and state.", roomPkg)
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
