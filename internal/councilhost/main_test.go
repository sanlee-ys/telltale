package councilhost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestMain makes this package's spawn vars FAIL-CLOSED for the whole test
// binary, and it is a deliberate copy of internal/council/main_test.go rather
// than a variation on it.
//
// # Why this file has to exist
//
// council's guard wraps THAT package's three spawn vars. A host spawns from a
// different process and a different package, so every seat it starts is outside
// that wrap. The guarantee council's guard buys would have been silently lost
// the moment the room moved here — which is exactly why it is extended in the
// same change that moves it, and not in a follow-up.
//
// The reason the guard exists at all is a measurement, not a preference. On
// 2026-08-09 an ordinary `go test ./internal/council` on a Windows box with
// Codex installed was polled with Win32_Process while it ran, and the parent
// chain resolved to a real `codex exec --json -s danger-full-access` — a live
// agent turn, with full write access, on the operator's own account and quota.
// **CI could never catch it**: CI has no vendors installed, so every seat
// resolves as missing and nothing dispatches. A green pipeline over a local-only
// defect is what a guard is for.
//
// # The third var is the sharper one
//
// startHost is not a vendor. It starts telltale's own binary, which resolves on
// any machine that BUILT it — so unlike a vendor spawn there is no machine
// where this one is harmlessly absent. And the process it starts then spawns
// real vendors, two processes away from whatever assertion provoked it. A test
// that reached it unstubbed would bill agent turns from a spawn nothing in the
// test's own package can see.
//
// # Why "resolves", and not an opt-in helper
//
// Copied verbatim from council's reasoning, because the reasoning is what has
// to be identical for the two guards to mean the same thing. A path exec cannot
// find spawns nothing, costs nothing and reaches no account, so it is let
// through to the real call and fails there — which is what the could-not-spawn
// tests assert on. A path exec CAN find is a real program about to run on
// someone's machine, and no spelling of the test's intent makes that cheaper. A
// marker like allowRealSpawn(t) would become the thing a future test copies
// without meaning it.
//
// The panic is deliberately not a t.Fatal: these vars are called from the
// host's dispatch path, which has no *testing.T in reach. What it must carry is
// WHAT would have run, or the next person is back to polling the process table.
func TestMain(m *testing.M) {
	// The re-exec helpers run BEFORE the guard is armed and before any test
	// runs, because they are not tests: they are the stand-in host and the
	// stand-in seat that TestAHardKilledHostReapsEverySeat measures. Neither
	// spawns a vendor and neither reaches a spawn var.
	if code, isHelper := runTestHelper(); isHelper {
		os.Exit(code)
	}

	realSession, realProcess, realHost := startSession, startProcess, startHost

	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (seatSession, error) {
		refuseRealVendor("startSession", spec)
		return realSession(ctx, spec, out, parse)
	}
	startProcess = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (*runner.Handle, error) {
		refuseRealVendor("startProcess", spec)
		return realProcess(ctx, spec, out, parse)
	}
	startHost = func(exe string, args []string, dir string) (*os.Process, error) {
		refuseRealHost(exe, args, dir)
		return realHost(exe, args, dir)
	}

	os.Exit(m.Run())
}

// refuseRealVendor panics if spec.Binary names a program this machine can
// actually run. It says which call site, which binary and the full argv, so the
// fix is visible from the failure alone.
func refuseRealVendor(site string, spec runner.Spec) {
	if _, err := exec.LookPath(spec.Binary); err != nil {
		// Nothing to launch. Let the real spawn run and fail, which is what the
		// tests that reach here are asserting on.
		return
	}
	panic(fmt.Sprintf(
		"councilhost test spawned a REAL vendor process via %s — on a machine where "+
			"this vendor is installed, that runs a live agent turn against the "+
			"operator's own account and quota.\n"+
			"  binary: %s\n"+
			"  args:   %q\n"+
			"  dir:    %s\n"+
			"Stub the spawn vars in this test — countSpawns(t) in "+
			"spawnlog_test.go does all three.",
		site, spec.Binary, spec.Args, spec.Dir))
}

// refuseRealHost panics if the host binary named here resolves.
//
// The rule is the same one, and the consequence is worse. A telltale binary
// resolves on any machine that built it, and the host it starts spawns real
// vendors — so this guard is the only thing between a forgetful test and a
// billed turn started by a process the test never mentions.
func refuseRealHost(exe string, args []string, dir string) {
	if _, err := exec.LookPath(exe); err != nil {
		if _, statErr := os.Stat(exe); statErr != nil {
			// Neither on PATH nor on disk. Nothing launches, so let the real
			// call fail exactly as it did before this guard existed.
			return
		}
	}
	panic(fmt.Sprintf(
		"councilhost test started a REAL council host — that process spawns live "+
			"vendor CLIs of its own, against the operator's account and quota, "+
			"two processes away from this test.\n"+
			"  binary: %s\n"+
			"  args:   %q\n"+
			"  dir:    %s\n"+
			"Stub the spawn vars in this test — countSpawns(t) in "+
			"spawnlog_test.go does all three.",
		exe, args, dir))
}
