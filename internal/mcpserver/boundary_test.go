package mcpserver

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The gates in this file turn this mode's two boundary claims — stdio only, and
// writes nothing — from sentences in the package doc into something the build
// runs.
//
// The import gate shells out to `go list` for the reason internal/eventview's
// boundary test does: it is the toolchain's own answer to what a package
// imports, honouring build tags, the module graph and the current GOOS, none of
// which a hand-rolled walker gets right. A missing `go` is a broken
// environment, not a reason to skip, and a skipped gate reads as a pass.

const serverPkg = "github.com/sanlee-ys/telltale/internal/mcpserver"

// TestTheServerOpensNoSocket pins the claim that makes §7.24 irrelevant to this
// mode.
//
// Both of telltale's other machine-facing surfaces listen on loopback, and
// §7.24 exists because a loopback bind is not containment on its own — a web
// page the operator merely visits reaches 127.0.0.1 too, which was measured
// planting a forged usage row and reading the event store. This mode has no
// such question to answer, and the reason is structural rather than careful:
// the client starts this process and owns both pipes, so there is nothing for a
// third party to connect to.
//
// The failure this guards is the convenient one. An MCP server that grew an
// HTTP transport "as well" would be a listener nobody wrote §7.24's argument
// for, and it would compile without a word of complaint.
//
// It reads the DIRECT imports of this package, which is the exact claim, and
// eventview's TestTheViewerOpensNoSocket is the precedent for saying why. A
// transitive assertion would be the wrong test and would fail today for the
// right reason: this package imports internal/snapshot for the document, that
// package imports internal/hud for the scan it reshapes, and internal/hud
// imports the Bubble Tea TUI framework — which reaches net. Linking that code
// is not calling it, the same distinction ADR-002's fast-path gate rests on.
//
// What it therefore does not cover, said out loud rather than implied: nothing
// stops a future caller reaching a socket through a package this one already
// imports. TestTheServerWritesNothing below is the behavioural half, and it
// measures a whole session rather than an import list.
func TestTheServerOpensNoSocket(t *testing.T) {
	out, err := exec.Command("go", "list", "-f", "{{join .Imports \"\\n\"}}", serverPkg).Output()
	if err != nil {
		t.Fatalf("go list %s: %v", serverPkg, err)
	}
	imports := strings.Fields(string(out))
	if len(imports) == 0 {
		t.Fatalf("go list returned no imports for %s; the gate would pass vacuously", serverPkg)
	}
	for _, imp := range imports {
		if imp == "net" || strings.HasPrefix(imp, "net/") {
			t.Errorf("the MCP server imports %s. This mode speaks stdio only: the client owns both\n"+
				"pipes, which is why design.md §7.24's question of who may reach a listener does not\n"+
				"arise here. A transport that binds a port needs that argument written first.", imp)
		}
	}
}

// TestTheServerWritesNothing is the read/write boundary, measured rather than
// asserted.
//
// It runs a whole session — handshake, list, call — with the home directory
// redirected, and compares the tree before and after. `telltale snapshot` holds
// the gauges' contract with one item spare (it does not even write the quota
// relay, because it renders no quota of its own to relay), and this mode is a
// reader of that same document, so it has one item spare too.
//
// The redirect is what makes the assertion real on a developer's box: without
// it the test would be reading the operator's own ~/.telltale, where any other
// process could move a byte mid-run and redden this for the wrong reason.
func TestTheServerWritesNothing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	before := tree(t, home)
	in := strings.NewReader(strings.Join([]string{
		initLine,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		callLine,
	}, "\n") + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), in, &out, options()); err != nil {
		t.Fatal(err)
	}
	if out.Len() == 0 {
		t.Fatal("the session produced no output; the gate below would pass vacuously")
	}
	if got := tree(t, home); got != before {
		t.Errorf("an MCP session changed something under the home directory:\nbefore %q\n after %q\n"+
			"This mode writes nothing at all — not even the quota relay, because it renders no quota\n"+
			"of its own to relay (design.md §7.22, §7.25).", before, got)
	}
}

// tree is a before/after fingerprint of everything under dir, by path and size.
// It reports the whole subtree rather than a Test-Path on one directory,
// because the write this guards against would be a new file in a directory
// that already exists.
func tree(t *testing.T, dir string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		lines = append(lines, path+"|"+info.ModTime().UTC().String()+"|"+strconv.FormatInt(info.Size(), 10))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}
