package council

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestMain makes the package's spawn vars FAIL-CLOSED for the whole test
// binary: reaching one with a binary that actually RESOLVES, without having
// stubbed it, panics instead of launching a real vendor CLI.
//
// It exists because the default was the opposite, and the default was measured
// costing real money. On 2026-08-09 an ordinary `go test ./internal/council` on
// a Windows box with Codex installed was polled with `Win32_Process` while it
// ran, and the parent chain resolved to:
//
//	cmd.exe /c codex exec --json -s danger-full-access --skip-git-repo-check -
//	  <- council.test.exe
//	  <- go test -count=1 ./internal/council
//
// That argv is council's own Codex invocation, built by
// internal/council/vendors/codex.go. So the suite was starting a real agent
// turn, with full write access, on the operator's own account and quota — and
// then running that operator's Codex hooks, which spawn PowerShell, inherit the
// test binary's cwd (this package directory) and write a module cache into it.
// The one offender was TestAStaleClassificationCannotSpareTheNextTurnsThread,
// which appends an AvailInstalled Codex column purely so the dispatch it is
// testing has a seat to address, and then dispatches for real.
//
// CI never saw any of it, and could not: CI has no vendors installed, every
// seat resolves AvailNotInstalled, and nothing ever dispatches. A local-only
// defect by construction — which is precisely the class a green pipeline
// cannot be asked to catch, and the reason this is a guard and not a line in
// CLAUDE.md. The failure mode is a test that FORGETS to stub, and a convention
// is exactly what a forgetful test complies with.
//
// # Why "resolves", and not an opt-in helper
//
// Three tests spawn deliberately and want the spawn to FAIL — they hand over a
// `telltale-no-such-binary` path under t.TempDir() to exercise the
// process-died branch. A guard gated on some allowRealSpawn(t) marker would
// have made those three declare an intent, and the declaration would then be
// the thing a future test copies without meaning it.
//
// The binary resolving is a better rule because it is not a declaration at
// all: it is the same question the operating system is about to ask. A path
// exec cannot find spawns nothing, costs nothing, and reaches no account, so
// it is let through to the real call and fails there exactly as it did before
// this file existed. A path exec CAN find is a real program about to run on
// someone's machine, which is the entire hazard, and no spelling of the test's
// intent makes that cheaper.
//
// The panic is deliberately not a t.Fatal: these vars are called from the
// model's dispatch path, which has no *testing.T in reach, and a panic already
// carries the test name and the goroutine that got there. What it must carry
// beyond that is WHAT would have run — without the argv, the next person is
// back to polling the process table.
//
// # The suite's home is a sandbox, for the same reason
//
// TestMain also points HOME and USERPROFILE at a temporary directory for the
// whole binary, and this is the same defect in a quieter form. A council test
// that dispatches saves the room, and SaveRoom resolves ~/.telltale/council
// through os.UserHomeDir — so on 2026-09-01 the operator's real
// council/room.json held `"workspace":
// "…\Temp\TestCursorACPZeroTextChunksFillsColumnBody2631424074\001"`, with that
// morning's timestamp and a suffix that changed between two reads minutes
// apart. Every plain `go test ./internal/council` was overwriting the file the
// operator's next `telltale council` reattaches from, pointing it at a temp
// directory the test had already deleted.
//
// Five of some sixty test files redirected HOME by hand, which is what a
// convention gets you: the tests that remembered were fine and the rest wrote
// the operator's disk. The redirect belongs here for the reason the spawn
// guards do — the failure mode is a test that FORGETS, and one line covering
// the whole binary is not something a new test can forget. A test that wants
// its own home still calls t.Setenv and wins; it just no longer has to.
//
// CI could not catch this either, and for a sharper reason than the spawn
// guards: the runner's home is fresh every job, so the file the suite corrupts
// is created, corrupted and thrown away inside the same green run. The check
// after m.Run() is what makes the property observable at all.
//
// # This guard is not the only one any more
//
// It covers THIS PACKAGE's spawns and nothing else, and that is worth saying
// out loud because it used to be the same thing. Since design.md §7.28
// (2026-09-01) a council room can also run in its own process, and
// internal/councilhost carries a TestMain of its own on the identical rule,
// over three vars: the host's `startSession` and `startProcess`, and the
// client's `startHost` — which starts telltale's own binary, resolvable on any
// machine that built it, whose child then spawns real vendors two processes
// away from whatever assertion provoked them.
//
// Nothing in package `council` reaches that host, so `countSpawns` below needs
// no entry for it. **If that ever changes — if the room here grows a path that
// starts or joins a host — the var behind it belongs in this wrap and in
// countSpawns, in the same change.** A guard that lags the spawn it guards is
// the state this file exists to prevent.
func TestMain(m *testing.M) {
	operatorHome, _ = os.UserHomeDir()
	stateBefore := councilStateSnapshot(operatorHome)

	var homeErr error
	sandboxHome, homeErr = os.MkdirTemp("", "council-home-")
	if homeErr != nil {
		panic(fmt.Sprintf("council test suite could not make a sandbox home: %v", homeErr))
	}
	os.Setenv("HOME", sandboxHome)
	os.Setenv("USERPROFILE", sandboxHome)

	realProcess, realSession, realRPC := startProcess, startSession, startRPCSession
	realEditor := startEditor

	startProcess = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (*runner.Handle, error) {
		refuseRealVendor("startProcess", spec)
		return realProcess(ctx, spec, out, parse)
	}
	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (seatSession, error) {
		refuseRealVendor("startSession", spec)
		return realSession(ctx, spec, out, parse)
	}
	startRPCSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, proto runner.Protocol) (seatSession, error) {
		refuseRealVendor("startRPCSession", spec)
		return realRPC(ctx, spec, out, proto)
	}

	// The EDITOR spawn is guarded on the same rule and for a sharper version of
	// the same reason (§9.49). It is not a vendor and costs no quota, but it is
	// still a real program starting on the machine running the suite — and
	// unlike the three above it is spawned by a KEYSTROKE, so a test that
	// presses `o` then `y` on a box where $EDITOR resolves would open a window
	// on the operator's desktop from a `go test` run. Whether the binary
	// resolves is the same question the operating system is about to ask, so
	// the rule is copied rather than re-invented.
	startEditor = func(name string, args []string, dir string) error {
		if _, err := exec.LookPath(name); err == nil {
			panic(fmt.Sprintf(
				"council test started a REAL editor process — that opens a program "+
					"on the desktop of whoever ran the suite.\n"+
					"  binary: %s\n"+
					"  args:   %q\n"+
					"  dir:    %s\n"+
					"Stub startEditor in this test — countSpawns(t) in "+
					"flow_security_test.go does it with the vendor spawns.",
				name, args, dir))
		}
		return realEditor(name, args, dir)
	}

	// The live seat (design.md §9.53) is the SIXTH spawn, and it is guarded on
	// the same rule as the three above with no softening. Its output is display
	// only — the pane renders a screen and no gauge reads it — and that is a
	// claim about what the room may DRAW, not about what the process costs. A
	// pseudoconsole child is `claude` running interactively on the operator's
	// own account, and a spawn that escaped this wrap would let a suite run
	// start one.
	realPTY := startPTYSession
	startPTYSession = func(ctx context.Context, spec runner.Spec, cols, rows int, out chan<- runner.PTYChunk) (runner.PTYSession, error) {
		refuseRealVendor("startPTYSession", spec)
		return realPTY(ctx, spec, cols, rows, out)
	}

	// The arena check (arenacheck.go) is the fifth thing this package can
	// spawn, and it is guarded on the same rule for a wider reason than the
	// three above: what it runs is a command the OPERATOR named, so on the
	// machine running this suite it is by definition a program somebody meant
	// to have. A test that reached the model's check path unstubbed would run
	// that person's build or test suite from inside `go test`.
	realCheck := startCheck
	startCheck = func(ctx context.Context, tree string, argv []string) checkResult {
		refuseRealCheck(tree, argv)
		return realCheck(ctx, tree, argv)
	}

	code := m.Run()

	// The redirect above is the fix; this is the check on it. A test that
	// resolves the operator's real home again — because it set HOME back, or
	// because a new code path builds the path some other way — shows up here as
	// a file that moved under ~/.telltale/council while the suite ran.
	if diff := councilStateDiff(stateBefore, councilStateSnapshot(operatorHome)); len(diff) > 0 {
		fmt.Fprintf(os.Stderr,
			"\ncouncil test suite WROTE THE OPERATOR'S OWN council state, at %s:\n%s\n"+
				"That file is what `telltale council` reattaches from, so a suite run "+
				"just pointed the operator's next room at a temp directory that no "+
				"longer exists.\nRedirect HOME and USERPROFILE in the offending test "+
				"(TestMain already does it for the whole binary — check what put them "+
				"back).\nIf a real `telltale council` was running beside this suite, it "+
				"wrote that file itself and this is its own report, not a defect.\n",
			filepath.Join(operatorHome, ".telltale", "council"),
			joinLines(diff))
		if code == 0 {
			code = 1
		}
	}
	os.RemoveAll(sandboxHome)
	os.Exit(code)
}

// operatorHome is the home directory of whoever ran the suite, captured before
// TestMain redirects it. sandboxHome is what every test resolves instead.
//
// Both are read by TestTheSuiteRunsAgainstASandboxHome, which is where the
// redirect stops being an implementation detail of this file and becomes a
// property with a name.
var (
	operatorHome string
	sandboxHome  string
)

// councilStateSnapshot records one directory — the operator's real
// ~/.telltale/council — by name, size and modification time.
//
// Deliberately narrow. The whole of ~/.telltale is the wrong scope: the
// statusline's quota relay writes ~/.telltale/quota on every prompt of every
// other tool the operator has open, so a snapshot of the parent would fail on a
// busy desk for a reason that has nothing to do with this suite. The council
// directory has exactly one writer, and this package is it.
//
// A missing directory snapshots as empty rather than as an error, which is the
// case on CI and on any machine that has never run `telltale council`. Empty
// before and empty after is a pass, and it is the pass CI gets.
func councilStateSnapshot(home string) map[string]string {
	snap := map[string]string{}
	if home == "" {
		return snap
	}
	root := filepath.Join(home, ".telltale", "council")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			snap[rel] = "unreadable"
			return nil
		}
		snap[rel] = fmt.Sprintf("%d bytes, modified %s",
			info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano))
		return nil
	})
	return snap
}

// councilStateDiff names every file that appeared, changed or went away.
//
// It reports the size and the timestamp rather than a bare "changed", because
// the first question anybody asks of this failure is which run did it, and the
// second is what the file says now.
func councilStateDiff(before, after map[string]string) []string {
	var lines []string
	for name, now := range after {
		was, existed := before[name]
		switch {
		case !existed:
			lines = append(lines, fmt.Sprintf("  created:   %s (%s)", name, now))
		case was != now:
			lines = append(lines, fmt.Sprintf("  rewritten: %s (%s -> %s)", name, was, now))
		}
	}
	for name := range before {
		if _, survives := after[name]; !survives {
			lines = append(lines, fmt.Sprintf("  deleted:   %s", name))
		}
	}
	sort.Strings(lines)
	return lines
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

// refuseRealCheck panics if the check's first word names a program this
// machine can actually run — refuseRealVendor's rule, stated for a command
// rather than for a vendor spec.
//
// A path exec cannot find launches nothing, so it is let through to the real
// call and fails there, which is what the could-not-run tests assert on. The
// one deliberate way past this guard is calling runCheck directly rather than
// through the var: arenacheck_test.go does so with THIS TEST BINARY as the
// command, which is the only way to assert the claim the whole feature rests
// on — that PASS and FAIL come from a real exit code.
func refuseRealCheck(tree string, argv []string) {
	if len(argv) == 0 {
		return
	}
	if _, err := exec.LookPath(argv[0]); err != nil {
		return
	}
	panic(fmt.Sprintf(
		"council test ran a REAL arena check via startCheck — that executes a "+
			"program on this machine from inside the suite.\n"+
			"  args: %q\n"+
			"  dir:  %s\n"+
			"Stub the spawn vars in this test — countSpawns(t) in "+
			"flow_security_test.go does all of them.",
		argv, tree))
}

// refuseRealVendor panics if spec.Binary names a program this machine can
// actually run. It says which call site, which binary and the full argv, so
// the fix is visible from the failure alone.
func refuseRealVendor(site string, spec runner.Spec) {
	if _, err := exec.LookPath(spec.Binary); err != nil {
		// Nothing to launch. Let the real spawn run and fail, which is what the
		// tests that reach here are asserting on.
		return
	}
	panic(fmt.Sprintf(
		"council test spawned a REAL vendor process via %s — on a machine where "+
			"this vendor is installed, that runs a live agent turn against the "+
			"operator's own account and quota.\n"+
			"  binary: %s\n"+
			"  args:   %q\n"+
			"  dir:    %s\n"+
			"Stub the spawn vars in this test — countSpawns(t) in "+
			"flow_security_test.go does all of them.",
		site, spec.Binary, spec.Args, spec.Dir))
}
