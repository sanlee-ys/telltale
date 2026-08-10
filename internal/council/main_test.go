package council

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestMain makes the package's three spawn vars FAIL-CLOSED for the whole test
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
func TestMain(m *testing.M) {
	realProcess, realSession, realRPC := startProcess, startSession, startRPCSession

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

	os.Exit(m.Run())
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
			"flow_security_test.go does all three.",
		site, spec.Binary, spec.Args, spec.Dir))
}
