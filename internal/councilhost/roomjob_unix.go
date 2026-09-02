//go:build !windows

package councilhost

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
)

// RoomJob is the Unix containment: the host leads its own SESSION, every seat
// is born into that session, and the host reaps what it owns on every exit it
// can catch.
//
// # What a session contains, and what a Job Object contains that it does not
//
// The Windows room job (roomjob_windows.go) is one kernel object with one
// property this platform has no equivalent for: JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// binds the LIFETIME of every process in the job to the last handle, so a host
// that dies by any route — `taskkill /F` included — takes every seat with it,
// and a child cannot leave the job by anything it does. runner/proc_unix.go
// measured the Unix side of that on 2026-08-17: a process group NAMES a set of
// processes and does not bind their lifetimes, and a program of that shape took
// SIGINT, SIGTERM, SIGHUP and SIGKILL with its Setpgid child surviving all four.
//
// So the containment here is built from the two Unix facts that DO hold, and
// the difference is stated rather than papered over:
//
//   - MEMBERSHIP is inherited and cannot be lost by accident. A session id is
//     copied into every child at fork and changes only when a process calls
//     setsid(2) itself; a process group id is the same, one level down. Every
//     vendor a seat starts, and every shell that vendor starts, carries the
//     host's session id unless something in that tree deliberately calls
//     setsid. That is the one escape: **a child that calls setsid leaves the
//     session, and nothing here can see it go. A Job Object child cannot
//     leave.** Nothing council dispatches is known to do this — a vendor that
//     daemonised its own tool calls would be a finding for PARITY.md — but it
//     is a property of the platform and not of this code.
//   - LIFETIME is not bound. Nothing reaps the session when its leader dies.
//     So the host reaps: NewRoomJob installs a handler for SIGTERM and SIGINT,
//     Serve turns that into Shutdown, and Shutdown kills every seat through
//     its per-seat group. `telltale council kill` sends SIGTERM to the host
//     and then SWEEPS the session — every process still carrying the host's
//     session id gets SIGTERM and, after a bounded grace, SIGKILL
//     (process_unix.go's killProcess) — so a host that could not run its
//     handler still has its seats ended by the command that ends it. What is
//     left uncovered is exactly the SIGKILL-the-host-and-walk-away case, and
//     PARITY.md's row says so: the seats keep running until the next
//     `telltale council kill` sweeps the dead host's session.
//
// # Why a SESSION and not the host's process group
//
// The task of a room-level container is to hold every seat, and the task of a
// per-seat container is to kill ONE seat's whole tree on an interrupt or an
// eviction without touching the others. runner/proc_unix.go builds the second
// from a process group per seat (Setpgid), and it is the containment the
// ordinary `telltale council` room measured and relies on. A host whose seats
// joined its OWN process group would have to give that up: `kill -TERM -pgid`
// on one seat would then signal every seat. A session sits one level above the
// process group and holds all of them, which is the same nesting the Windows
// side has — the room job over the per-seat jobs — expressed in the platform's
// own hierarchy.
//
// # SIGHUP is ignored, and the host has no controlling terminal to send it
//
// spawn_unix.go starts the host with Setsid, so it is born a session leader
// with no controlling terminal, and closing the operator's terminal sends
// SIGHUP to THAT terminal's session — which the host is not in. Ignoring SIGHUP
// here is the belt over that: a host that was started some other way, or that
// a person signals by hand, does not lose a detached room to a signal whose
// default disposition is to end the process without running a handler.
//
// # The order in Serve is the same order it is on Windows, for the same reason
//
// Serve builds this BEFORE it creates the socket and before any seat exists, so
// a seat is only ever started by a process that already leads the session it
// is claiming to hold.
type RoomJob struct {
	sid int
	sig chan os.Signal
}

// NewRoomJob makes THIS process a session leader, or confirms that it already
// is one, and installs the host's signal handling.
//
// setsid(2) fails with EPERM when the caller is already a process group
// leader, which is what a process started from an interactive shell is. That
// case is refused rather than worked around: a host that is not a session
// leader cannot be told apart from a stranger by verifyHostProcess, and the
// client always starts it with Setsid, so the only way to reach this refusal
// is to type the command a person is not meant to type.
func NewRoomJob() (*RoomJob, error) {
	pid := os.Getpid()
	sid, err := unix.Getsid(pid)
	if err != nil {
		return nil, fmt.Errorf("councilhost: could not read this process's session: %w", err)
	}
	if sid != pid {
		if _, err := unix.Setsid(); err != nil {
			return nil, fmt.Errorf("councilhost: this host is not a session leader and could not become "+
				"one (%v) — start it through `telltale council --host`, which does", err)
		}
	}
	// Ignored, not caught: there is nothing a host should DO on a hangup
	// except keep going, and the default disposition would end it.
	signal.Ignore(syscall.SIGHUP)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	return &RoomJob{sid: pid, sig: sig}, nil
}

// SID reports the session this host leads. Exported for the tests that assert
// a seat was born into it.
func (j *RoomJob) SID() int {
	if j == nil {
		return 0
	}
	return j.sid
}

// Signalled is the channel a caught SIGTERM or SIGINT arrives on.
//
// Nil for a zero RoomJob, which is what an in-process test's stub hands back:
// a receive from a nil channel blocks forever, so a test host installs no
// handler and the test binary keeps its own signal disposition.
func (j *RoomJob) Signalled() <-chan os.Signal {
	if j == nil {
		return nil
	}
	return j.sig
}

// Kill ends every process in the session other than this one.
//
// The counterpart of TerminateJobObject, and like it a last move after the
// seats have been asked to stop: a SIGKILL gives a process no chance to finish
// a write. It is the same sweep `telltale council kill` makes from outside,
// made from inside.
func (j *RoomJob) Kill() error {
	if j == nil || j.sid == 0 {
		return nil
	}
	_, err := sweepSession(j.sid, syscall.SIGKILL)
	return err
}

// Close hands the signals back to their default disposition.
func (j *RoomJob) Close() error {
	if j == nil || j.sig == nil {
		return nil
	}
	signal.Stop(j.sig)
	return nil
}
