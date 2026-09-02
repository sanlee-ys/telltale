//go:build !windows

package councilhost

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// ErrHostGone is returned when the pid a discovery file names is not running.
var ErrHostGone = errors.New("councilhost: the process host.json names is not running")

// ErrNotTelltale is returned when the pid IS running and is somebody else.
//
// Distinct from ErrHostGone and never folded into it, for process_windows.go's
// reason: a pid that is gone means a stale file to say so about; a pid that
// belongs to a stranger means a file that must NOT be acted on, and `telltale
// council kill` would otherwise terminate whatever took the number.
var ErrNotTelltale = errors.New("councilhost: the process host.json names is not a telltale host")

// processStartSlack is how far AFTER the recorded start a process may have
// been created and still be believed. process_windows.go says why it is small.
const processStartSlack = 5 * time.Second

// verifyHostProcess answers whether pid is a LIVE telltale host that started
// no later than the discovery file claims.
//
// # Four readings, and why kill(pid, 0) alone is the wrong probe
//
// kill(pid, 0) answers "is there a process with this number that I may
// signal", and that is all it answers. A pid is reusable and a stale host.json
// is the normal case after a hard kill (§7.28), so a probe that stopped there
// would read a recycled number as a live room and `telltale council kill` would
// terminate a stranger. The Windows check takes two more readings — the image
// name and the creation time — and this takes three:
//
//  1. kill(pid, 0). ESRCH is a pid nothing runs on: ErrHostGone. EPERM is a
//     process of another user, which this host would never have been:
//     ErrNotTelltale.
//  2. getsid(pid) == pid. Every host leads its own session (NewRoomJob), so a
//     recycled pid held by an ordinary process fails here before anything is
//     read about it.
//  3. The image name, read the way each platform lets a same-user process read
//     it (identity_linux.go, identity_darwin.go). A recycled pid held by a
//     browser or a compiler fails here.
//  4. The start time, against the file's started_at. A LATER telltale that took
//     the number — the one case the name cannot catch — fails here.
//
// An identity that cannot be read is ErrNotTelltale and not ErrHostGone: a file
// whose pid cannot be vouched for is a file that must not be acted on.
func verifyHostProcess(pid int, startedAt time.Time) error {
	if pid <= 0 {
		// Before any kill: a zero or negative pid addresses a GROUP, and
		// kill(0, 0) would probe this process's own group and succeed.
		return fmt.Errorf("%w: host.json names pid %d", ErrHostGone, pid)
	}
	switch err := unix.Kill(pid, 0); {
	case errors.Is(err, syscall.ESRCH):
		return fmt.Errorf("%w: pid %d is not running", ErrHostGone, pid)
	case errors.Is(err, syscall.EPERM):
		return fmt.Errorf("%w: pid %d belongs to another user", ErrNotTelltale, pid)
	case err != nil:
		return fmt.Errorf("%w: pid %d could not be probed: %v", ErrNotTelltale, pid, err)
	}
	sid, err := unix.Getsid(pid)
	if err != nil {
		return fmt.Errorf("%w: the session of pid %d could not be read: %v", ErrNotTelltale, pid, err)
	}
	if sid != pid {
		return fmt.Errorf("%w: pid %d does not lead its own session, and every host does — the pid was reused",
			ErrNotTelltale, pid)
	}
	image, err := processImage(pid)
	if err != nil {
		return fmt.Errorf("%w: could not read the image name of pid %d: %v", ErrNotTelltale, pid, err)
	}
	self, err := selfImageBase()
	if err != nil {
		return err
	}
	if !sameImage(image, self) {
		return fmt.Errorf("%w: pid %d is %s, and this binary is %s — the pid was reused",
			ErrNotTelltale, pid, image, self)
	}
	if !startedAt.IsZero() {
		created, err := processStart(pid)
		if err != nil {
			return fmt.Errorf("%w: could not read the start time of pid %d: %v", ErrNotTelltale, pid, err)
		}
		if created.After(startedAt.Add(processStartSlack)) {
			return fmt.Errorf("%w: pid %d started at %s and host.json was written at %s — "+
				"a later process took the number",
				ErrNotTelltale, pid, created.Format(time.RFC3339), startedAt.Format(time.RFC3339))
		}
	}
	return nil
}

// killGrace bounds how long a host is given to answer SIGTERM before the
// session is swept with SIGKILL. It covers a handler that kills a handful of
// process groups and returns, so it is generous by an order of magnitude.
const killGrace = 10 * time.Second

// killProcess ends a host that verifyHostProcess has already cleared, and every
// process in its session.
//
// # The order, and why it is a sweep and not a signal to one pid
//
// design.md §7.29 rules that `telltale council kill` terminates rather than
// sending a shutdown frame, because a frame needs the socket and the socket
// can be held by a client. On Windows the termination alone is enough: the
// room job reaps the seats when the host's handle closes. Here nothing reaps,
// so this command does:
//
//  1. SIGTERM to the host. NewRoomJob's handler turns it into Shutdown, which
//     kills every seat the host registered through its per-seat group and
//     returns, and the host exits on its ordinary path — removing host.json.
//  2. SIGTERM to every other process in the host's session, at the same time.
//     A seat's tree that the host had lost track of is still in the session.
//  3. Wait for the host to exit, bounded by killGrace.
//  4. SIGKILL to everything still in the session, the host included if it is
//     still there. A vendor that ignored SIGTERM goes here.
//
// The sweep after the host is dead is safe against pid reuse for a kernel
// reason rather than a timing one: a pid number is not handed out again while
// any process still carries it as a session id, so `-sid` names either the
// dead host's own orphans or nothing.
//
// On a Unix this package cannot enumerate a session on (identity_other.go)
// the sweep degrades to the host's own process group, and a host that needed
// SIGKILL is reported as an error rather than as "every seat", because the
// claim would not have been measured.
func killProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("%w: host.json names pid %d", ErrHostGone, pid)
	}
	if err := unix.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("councilhost: could not signal pid %d: %w", pid, err)
	}
	_, sweepErr := sweepSession(pid, syscall.SIGTERM)

	deadline := time.Now().Add(killGrace)
	hostGone := false
	for time.Now().Before(deadline) {
		if err := unix.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			hostGone = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hostGone {
		_ = unix.Kill(pid, syscall.SIGKILL)
	}
	// The sweep, whether or not the host answered: a seat that ignored
	// SIGTERM is still in the session and is what this pass is for.
	_, sweepErr2 := sweepSession(pid, syscall.SIGKILL)
	if sweepErr == nil {
		sweepErr = sweepErr2
	}
	// Waited on, and bounded, so a caller that prints "ended" is reporting a
	// measurement rather than an intention.
	settle := time.Now().Add(2 * time.Second)
	for time.Now().Before(settle) {
		if err := unix.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !hostGone && sweepErr != nil {
		return fmt.Errorf("councilhost: pid %d did not exit on SIGTERM within %s and was killed, and this "+
			"platform cannot enumerate its session (%v) — its seats may still be running: "+
			"`pgrep -s %d` lists them", pid, killGrace, sweepErr, pid)
	}
	return nil
}

// sweepSession signals every process in a session other than this one.
//
// It reports how many it signalled, so a caller can say "ended N processes"
// rather than "ended the session" — the latter is not a measurement. A pid
// that disappeared between the listing and the signal is not counted and not
// an error.
func sweepSession(sid int, sig syscall.Signal) (int, error) {
	members, err := sessionMembers(sid)
	if err != nil {
		// Degraded to the process group, which a session leader's pid also
		// names, and reported so the caller can say what was not covered.
		if kerr := unix.Kill(-sid, sig); kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
			return 0, err
		}
		return 0, err
	}
	n := 0
	self := os.Getpid()
	for _, pid := range members {
		if pid == self {
			continue
		}
		if err := unix.Kill(pid, sig); err == nil {
			n++
		}
	}
	return n, nil
}

// reapOrphans ends what a DEAD host left in its session, and reports how many.
//
// `telltale council kill` calls this on a HostDead report. The guard is that the
// pid is gone: a pid that is alive is either a verified host (the live path
// handles it) or a stranger (nothing here may touch it), and the kernel's
// pid-reuse rule above makes a dead leader's session id name only its own
// orphans. A platform that cannot enumerate a session reports zero, which is
// the honest count of what it ended.
func reapOrphans(pid int) int {
	if pid <= 0 {
		return 0
	}
	if err := unix.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		return 0
	}
	n, err := sweepSession(pid, syscall.SIGTERM)
	if err != nil || n == 0 {
		return 0
	}
	time.Sleep(200 * time.Millisecond)
	_, _ = sweepSession(pid, syscall.SIGKILL)
	return n
}

// sameImage compares a process's image name with this binary's.
//
// EqualFold rather than equality, matching the Windows check, and a prefix
// match when the platform reports a truncated name — identity_darwin.go says
// how long its is.
func sameImage(image, self string) bool {
	if strings.EqualFold(image, self) {
		return true
	}
	return len(image) == truncatedImageLen && strings.HasPrefix(strings.ToLower(self), strings.ToLower(image))
}

// foldWorkspace leaves a workspace path alone off Windows.
//
// The identity function is the CORRECT answer here rather than a stub: every
// platform but Windows treats `/a/B` and `/a/b` as two directories, so folding
// them would merge two rooms into one — the same defect Windows folding
// prevents, pointed the other way.
func foldWorkspace(path string) string { return path }
