//go:build !windows

package council

import (
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// fakeProgram stands in for *tea.Program, and records only the one act the
// watcher performs on it.
//
// A real program would mean a terminal, an alternate screen and an event loop,
// none of which the property needs: the question is whether the SEATS died
// before the room went out, and whether the program was asked to end itself on
// the one signal Bubble Tea does not answer.
type fakeProgram struct {
	mu     sync.Mutex
	killed bool
}

func (p *fakeProgram) Kill() {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
}

func (p *fakeProgram) wasKilled() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.killed
}

// signalSettle is how long a "did not happen" assertion waits before it is
// believed. It bounds nothing else in this file — every positive assertion
// blocks on a channel the seat's own Kill closes.
const signalSettle = 250 * time.Millisecond

// TestASignalKillsTheSeatsBeforeTheRoomGoesOut pins the whole non-Windows fix:
// on a signal, the seats die.
//
// The measurement behind it (signals_unix.go, 2026-08-17): Bubble Tea answers
// SIGINT and SIGTERM above the model's head — its event loop returns on
// InterruptMsg and QuitMsg BEFORE calling Update — so council's teardown, which
// only the q and ctrl+c keys reach, never ran on any signal, and all five seats
// survived the room. SIGHUP was not registered at all.
//
// No vendor is spawned: every seat here is a countedKill, so this test asserts
// the kill rather than a flag saying one was intended.
func TestASignalKillsTheSeatsBeforeTheRoomGoesOut(t *testing.T) {
	for _, tc := range []struct {
		name string
		sig  syscall.Signal
		// killsProgram is the asymmetry the watcher's doc comment argues for:
		// Bubble Tea already ends the program on SIGINT and SIGTERM, so the
		// watcher must not start a second shutdown there; nothing ends it on
		// SIGHUP, so the watcher must.
		killsProgram bool
	}{
		{"SIGINT is answered by bubble tea, so only the seats are ours", syscall.SIGINT, false},
		{"SIGTERM is answered by bubble tea, so only the seats are ours", syscall.SIGTERM, false},
		{"SIGHUP is answered by nobody, so the watcher ends the room too", syscall.SIGHUP, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, seats, racer, _ := teardownRoom(t)
			prog := &fakeProgram{}
			stop := watchExitSignals(m, prog)
			t.Cleanup(stop)

			if err := syscall.Kill(os.Getpid(), tc.sig); err != nil {
				t.Fatalf("could not signal this process: %v", err)
			}

			for v, s := range seats {
				select {
				case <-s.fired:
				case <-time.After(5 * time.Second):
					t.Fatalf("%s was still running 5s after %v — the signal orphaned it", v, tc.sig)
				}
			}
			select {
			case <-racer.fired:
			case <-time.After(5 * time.Second):
				t.Fatalf("the racer was still running 5s after %v", tc.sig)
			}

			// Settled before the program is judged, because one arm of this
			// table asserts an absence and an absence needs a bound. The
			// teardown above has already completed, so this waits only on the
			// step that would follow it.
			time.Sleep(signalSettle)
			if got := prog.wasKilled(); got != tc.killsProgram {
				t.Errorf("program killed = %v, want %v after %v", got, tc.killsProgram, tc.sig)
			}
		})
	}
}

// TestTheWatcherStopsWhenTheRoomEndsAnyOtherWay pins the returned stop.
//
// Run defers it, and it has to be real: a watcher left subscribed after the
// room has gone would hold a goroutine on a channel, and — worse — would keep
// answering SIGHUP for a process that has no room to tear down, displacing the
// default disposition that should end it.
//
// Asserted by the SEATS rather than by the goroutine: after stop, a teardown
// this test performs by hand is the only thing that kills them, so a watcher
// still listening would show up as a kill nobody in this test asked for. The
// signal is not re-sent — the disposition is back to default and it would end
// the test binary, which is exactly the behaviour being claimed.
func TestTheWatcherStopsWhenTheRoomEndsAnyOtherWay(t *testing.T) {
	m, seats, _, _ := teardownRoom(t)
	stop := watchExitSignals(m, &fakeProgram{})
	// Twice, because the closure sits beside a context.CancelFunc and will be
	// read as idempotent whether or not it is. The second call closing an
	// already-closed channel would panic here.
	stop()
	stop()

	time.Sleep(signalSettle)
	for v, s := range seats {
		if s.count() != 0 {
			t.Errorf("%s was killed with no signal sent", v)
		}
	}

	m.teardown()
	if got := seats[model.VendorClaude].count(); got != 1 {
		t.Errorf("claude was killed %d times by the ordinary quit, want 1", got)
	}
}
