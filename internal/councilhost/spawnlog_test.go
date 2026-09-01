package councilhost

import (
	"context"
	"os"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
)

// spawnLog counts and records every process this package can start.
//
// It is internal/council/flow_security_test.go's helper, carried over so that
// the two packages assert the same OBSERVABLE. Nothing here simulates a vendor:
// the fake session's only behaviours are the four the host actually calls on a
// live one, so a test cannot pass by exercising a fake that does less than the
// real thing.
//
// The whole point is that "nothing was spawned" can be asserted without paying
// for a spawn — and that a spawn which escaped the count could not let that
// assertion pass over a vendor that was launched.
type spawnLog struct {
	specs []runner.Spec
	// hosts records every host launch: the binary, the argv and the directory.
	// Kept apart from specs because a host is not a seat — a test asserting "no
	// vendor spawned" must not be answered by a host that spawned four.
	hosts []hostRun
}

// hostRun is one stubbed host launch.
type hostRun struct {
	exe  string
	args []string
	dir  string
}

// deadSession answers the four calls a live seat answers and does nothing.
type deadSession struct{ sent [][]byte }

func (d *deadSession) SendTurn(l [][]byte) error  { d.sent = append(d.sent, l...); return nil }
func (d *deadSession) SendAside(l [][]byte) error { d.sent = append(d.sent, l...); return nil }
func (d *deadSession) Kill()                      {}
func (d *deadSession) Alive() bool                { return true }

// countSpawns stubs all three spawn vars and restores them in t.Cleanup.
//
// Every test that builds a roster with a resolvable binary and then dispatches
// needs this. Without it TestMain's guard panics — which is the guard working,
// not a test failing for an unrelated reason.
func countSpawns(t *testing.T) *spawnLog {
	t.Helper()
	log := &spawnLog{}
	origSession, origProcess, origHost := startSession, startProcess, startHost
	startSession = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (seatSession, error) {
		log.specs = append(log.specs, spec)
		return &deadSession{}, nil
	}
	startProcess = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (*runner.Handle, error) {
		log.specs = append(log.specs, spec)
		return &runner.Handle{}, nil
	}
	// The third, counted for the first two's reason: a host launch that escaped
	// this count would let "nothing was spawned" pass over a process that
	// spawns vendors of its own.
	startHost = func(exe string, args []string, dir string) (*os.Process, error) {
		log.hosts = append(log.hosts, hostRun{exe: exe, args: args, dir: dir})
		return nil, errStubbedHost
	}
	t.Cleanup(func() {
		startSession, startProcess, startHost = origSession, origProcess, origHost
	})
	return log
}

// errStubbedHost is what the stubbed host launch reports.
//
// An ERROR rather than a fake process handle, and deliberately: os.Process has
// no interface to fake, and handing back a struct with a pid nobody owns would
// let a test's Kill land on whatever process holds that number. A test that
// wants a host end to end starts a real one over a real pipe — see
// TestOneClientDrivesAHostedRoomEndToEnd, which does not go through this stub.
var errStubbedHost = errStub("councilhost: the host spawn is stubbed in this test")

type errStub string

func (e errStub) Error() string { return string(e) }

func (l *spawnLog) n() int { return len(l.specs) }
