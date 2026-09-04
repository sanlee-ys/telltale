package probe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// TestMain makes this package's two spawn vars FAIL-CLOSED for the whole test
// binary, on internal/council/main_test.go's rule and for a sharper version of
// its reason.
//
// That guard exists because a plain `go test ./internal/council` was measured
// starting `codex exec --json -s danger-full-access`, a live agent turn with
// full write access, on the operator's own account. This package is the one
// place in the repository where spawning a vendor is the POINT, so the same
// defect here would not be an accident in a test that wanted a second column:
// it would be the suite running the exact thing this mode charges the operator
// for.
//
// The rule is copied rather than re-invented. A binary `exec.LookPath` cannot
// resolve launches nothing, costs nothing and reaches no account, so it is let
// through to the real call and fails there. That is what the tests below
// assert on. A binary it CAN resolve is a real program about to run on
// somebody's machine, and no spelling of a test's intent makes that cheaper.
//
// HOME and USERPROFILE are redirected for the same file's second reason. This
// package writes ~/.telltale/probe, and a suite that wrote the operator's own
// probe files would put a result on their disk that no probe of theirs
// produced, and `telltale doctor` would then report it as a measurement made
// here. That is worse than the council case it is copied from: council's file
// pointed a room at a deleted directory, and this one would be a false claim on
// a surface built to carry true ones.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "probe-home-")
	if err != nil {
		panic(fmt.Sprintf("probe test suite could not make a sandbox home: %v", err))
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)

	realSession, realRPC := startSession, startRPCSession
	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
		parse runner.ParseFunc) (session, error) {
		refuseRealVendor("startSession", spec)
		return realSession(ctx, spec, out, parse)
	}
	startRPCSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
		proto runner.Protocol) (session, error) {
		refuseRealVendor("startRPCSession", spec)
		return realRPC(ctx, spec, out, proto)
	}

	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func refuseRealVendor(site string, spec runner.Spec) {
	if _, err := exec.LookPath(spec.Binary); err != nil {
		return
	}
	panic(fmt.Sprintf(
		"probe test spawned a REAL vendor process via %s. This package's whole job is "+
			"to spend a billed turn, so a suite that reaches a resolvable binary spends "+
			"the operator's money.\n"+
			"  binary: %s\n"+
			"  args:   %q\n"+
			"  dir:    %s\n"+
			"Stub the spawn vars in this test: stubSpawn(t) does both.",
		site, spec.Binary, spec.Args, spec.Dir))
}
