package councilhost

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

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
	// mu guards both slices. They are appended on the HOST's goroutines — the
	// frame loop, and now the dispatch goroutine — and read on the test's, and
	// the room frame a test waits on is not a happens-before edge for them:
	// beginTurn bumps the turn BEFORE any spawn, so an assertion that waited
	// for turn 1 could sample the count before the append landed. A race, and
	// an occasional wrong count.
	mu    sync.Mutex
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
		log.add(spec)
		return &deadSession{}, nil
	}
	startProcess = func(_ context.Context, spec runner.Spec, _ chan<- runner.Event, _ runner.ParseFunc) (seatProcess, error) {
		log.add(spec)
		// A no-op fake, NOT a &runner.Handle{}. A zero Handle carries a nil
		// procGroup and its Kill calls a method on it unconditionally, so the
		// first Shutdown or interrupt that reached one would panic the whole
		// test binary rather than fail a case. Latent only while every test
		// roster seats claude, the one persistent vendor; the first test to
		// seat codex, agy or grok would have found it the hard way.
		return deadProcess{}, nil
	}
	// The third, counted for the first two's reason: a host launch that escaped
	// this count would let "nothing was spawned" pass over a process that
	// spawns vendors of its own.
	startHost = func(exe string, args []string, dir string) (*os.Process, error) {
		log.addHost(hostRun{exe: exe, args: args, dir: dir})
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

func (l *spawnLog) add(spec runner.Spec) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.specs = append(l.specs, spec)
}

func (l *spawnLog) addHost(h hostRun) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.hosts = append(l.hosts, h)
}

func (l *spawnLog) n() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.specs)
}

// awaitN waits for the spawn count to REACH want and then holds still long
// enough to catch an extra.
//
// Sampling once is what made the count racy: the room's turn is bumped before
// any seat is spawned, so a test that waited for turn 1 could read the count
// before the append. Waiting for the number and then confirming it settled
// asserts both halves — that the spawns happened, and that no more did.
func (l *spawnLog) awaitN(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for l.n() < want {
		if time.Now().After(deadline) {
			t.Fatalf("expected %d spawns, saw %d", want, l.n())
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	if got := l.n(); got != want {
		t.Fatalf("expected exactly %d spawns, saw %d", want, got)
	}
}

// deadProcess answers the one call a live spawn-per-turn child answers.
type deadProcess struct{}

func (deadProcess) Kill() {}
