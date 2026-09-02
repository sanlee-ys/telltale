//go:build !windows

package councilhost

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// runStandInHost is the helper process the containment measurement signals.
//
// It does what a real host does in the order a real host does it: build the
// room job — become a session leader, install the handlers — and only then
// start a seat. The seat is a PLAIN exec.Command with no per-seat group, for
// roomjob_windows_test.go's reason: a seat with one would be reaped by the
// per-seat mechanism that already existed, and the session would be measured
// holding nothing.
//
// It then waits on the job's own signal channel, which is what the real host
// does through Serve: a SIGTERM ends it on its ordinary path, and a SIGKILL
// ends it with no path at all. Both are measured below.
func runStandInHost() int {
	job, err := NewRoomJob()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stand-in host: could not build the room job:", err)
		return 1
	}
	seat := exec.Command(os.Args[0])
	seat.Env = append(os.Environ(), helperEnv+"="+helperSeat)
	if err := seat.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "stand-in host: could not start the seat:", err)
		return 1
	}
	fmt.Printf("seat %d\nhost %d\nsid %d\n", seat.Process.Pid, os.Getpid(), job.SID())
	os.Stdout.Sync()

	select {
	case <-job.Signalled():
		return 0
	case <-time.After(seatLifetime):
		return 0
	}
}

// TestASigkilledHostLeaksItsSeatAndKillSweepsIt is the measured asymmetry
// (design.md §7.30, PARITY.md), pinned in both halves.
//
// # Half one: the leak, asserted so it cannot be forgotten
//
// runner/proc_unix.go's 2026-08-17 measurement is that a process group does not
// bind lifetimes. A session does not either, and this asserts it against the
// host's own containment rather than citing the older run: SIGKILL the stand-in
// host and the seat is STILL THERE. A later change that made this half fail
// would be a platform that grew a lifetime binding, and PARITY.md's row would
// be the thing to update.
//
// # Half two: `telltale council kill` on the stale file reaps what was left
//
// A dead host's session id still names its orphans, and the kernel will not
// hand that pid out again while any of them carries it, so the sweep is safe.
// KillHost on a discovery file naming the dead pid must end the seat and say
// how many processes it ended.
func TestASigkilledHostLeaksItsSeatAndKillSweepsIt(t *testing.T) {
	seatPID, host, sid := startStandInHost(t)
	if sid != host.Pid {
		t.Fatalf("the stand-in host leads session %d and is pid %d; NewRoomJob must make them one", sid, host.Pid)
	}
	if s, err := unix.Getsid(seatPID); err != nil || s != sid {
		t.Fatalf("the seat is in session %d (%v), not the host's %d", s, err, sid)
	}
	members, err := sessionMembers(sid)
	if errors.Is(err, errNoProcessTableOnThisPlatform()) {
		t.Skip("no session enumeration on this platform")
	}
	if err != nil {
		t.Fatalf("sessionMembers failed: %v", err)
	}
	if !contains(members, host.Pid) || !contains(members, seatPID) {
		t.Fatalf("the session listing %v does not hold both the host %d and the seat %d",
			members, host.Pid, seatPID)
	}

	if err := host.Kill(); err != nil {
		t.Fatalf("could not SIGKILL the stand-in host: %v", err)
	}
	_, _ = host.Wait()
	time.Sleep(200 * time.Millisecond)
	if err := unix.Kill(seatPID, 0); err != nil {
		t.Fatalf("the seat died with a SIGKILLed host (%v). That is the Windows job's property, "+
			"and this platform was measured NOT to have it — if it has grown one, update "+
			"PARITY.md's row and roomjob_unix.go's doc rather than this test alone.", err)
	}

	dir := t.TempDir()
	stale := testPipeName(t)
	for _, p := range []string{stale, lockPath(stale), heldPath(stale)} {
		// What a hard-killed host leaves: nodes with nobody behind them.
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteHostFile(dir, HostFile{
		PID: host.Pid, Pipe: stale, StartedAt: time.Now(), Workspace: dir,
	}); err != nil {
		t.Fatal(err)
	}
	rep := Probe(dir)
	if rep.State != HostDead {
		t.Fatalf("a SIGKILLed host probed as %v (%s)", rep.State, rep.Reason)
	}
	line, err := KillHost(dir)
	if err != nil {
		t.Fatalf("KillHost on the stale file failed: %v", err)
	}
	if !strings.Contains(line, "1 process was still running") {
		t.Fatalf("KillHost did not report the orphan it swept: %q", line)
	}
	awaitGone(t, seatPID, 10*time.Second,
		"the seat survived `telltale council kill` on the dead host's file. The sweep of a "+
			"dead host's session is the only thing on this platform that reaps a leaked seat.")
	// The stale transport nodes go with the stale file. A dead host's socket
	// and lock files are the untidy case Listen already tolerates; the command
	// that removed its discovery file removes them too.
	for _, p := range []string{rep.File.Pipe, lockPath(rep.File.Pipe), heldPath(rep.File.Pipe)} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("kill left the dead host's %s behind (%v)", p, err)
		}
	}
}

// TestASigtermedHostReapsItsSeatsOnTheWayOut is the host's own reaping,
// measured through the production path: a real Serve, a real NewRoomJob, and a
// seat registered with the host through a stubbed spawn is not possible across
// a process boundary — so the seat here is the stand-in, reachable through the
// session, and killProcess is what reaps it. The detach test measures the same
// command on a host with a client history; this one pins it on a bare host.
func TestASigtermedHostReapsItsSeatsOnTheWayOut(t *testing.T) {
	seatPID, host, _ := startStandInHost(t)
	if _, err := sessionMembers(host.Pid); errors.Is(err, errNoProcessTableOnThisPlatform()) {
		t.Skip("no session enumeration on this platform")
	}
	// Reaped in the background first, for detach_unix_test.go's reason: a
	// zombie child answers kill(pid, 0), and this process is the parent.
	go func() { _, _ = host.Wait() }()
	started := time.Now()
	if err := killProcess(host.Pid); err != nil {
		t.Fatalf("killProcess failed: %v", err)
	}
	if took := time.Since(started); took > killGrace/2 {
		t.Fatalf("killProcess took %s: the host did not exit on SIGTERM and had to be killed, "+
			"so the handler NewRoomJob installs did not run", took)
	}
	if err := unix.Kill(host.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the host is still there after killProcess (%v)", err)
	}
	awaitGone(t, seatPID, 10*time.Second, "the seat survived killProcess on a live host")
}

// TestTheIdentityCheckRefusesARecycledPID is process_windows.go's four-reading
// identity check, measured on the Unix stand-ins.
//
// Three processes that are all THIS binary answer three different ways, and
// the differences are the check:
//
//   - the stand-in host, a session leader with a fresh start time: verified;
//   - the same host against a start time an hour before it began: refused as a
//     LATER process that took the number;
//   - the seat, alive and this binary and not a session leader: refused,
//     because every host leads its own session and a pid that does not is a
//     pid something else holds.
func TestTheIdentityCheckRefusesARecycledPID(t *testing.T) {
	seatPID, host, _ := startStandInHost(t)
	if _, err := processImage(host.Pid); errors.Is(err, errNoProcessTableOnThisPlatform()) {
		t.Skip("no process-table reading on this platform")
	}

	if err := verifyHostProcess(host.Pid, time.Now()); err != nil {
		t.Fatalf("a live session-leading host of this binary did not verify: %v", err)
	}
	err := verifyHostProcess(host.Pid, time.Now().Add(-365*24*time.Hour))
	if err == nil {
		t.Fatal("a process that started long after the discovery file was written verified as its host")
	}
	if !errors.Is(err, ErrNotTelltale) || !strings.Contains(err.Error(), "took the number") {
		t.Fatalf("the refusal was reported as the wrong kind of failure: %v", err)
	}
	if err := verifyHostProcess(seatPID, time.Now()); !errors.Is(err, ErrNotTelltale) {
		t.Fatalf("a live non-leader of this binary was reported as %v; ErrNotTelltale was expected", err)
	}
	if err := verifyHostProcess(0, time.Now()); !errors.Is(err, ErrHostGone) {
		t.Fatalf("pid 0 was reported as %v; ErrHostGone was expected", err)
	}
}

// startStandInHost re-executes the test binary as a host and returns the seat's
// pid, the host's process and the session it reported.
func startStandInHost(t *testing.T) (seatPID int, host *os.Process, sid int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+helperHost)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("could not pipe the stand-in host's stdout: %v", err)
	}
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the stand-in host: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_, _ = sweepSession(cmd.Process.Pid, syscall.SIGKILL)
	})

	lines := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	deadline := time.After(20 * time.Second)
	for seatPID == 0 || sid == 0 {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the stand-in host said nothing and exited")
			}
			if v, ok := strings.CutPrefix(l, "seat "); ok {
				seatPID, _ = strconv.Atoi(v)
			}
			if v, ok := strings.CutPrefix(l, "sid "); ok {
				sid, _ = strconv.Atoi(v)
			}
		case <-deadline:
			t.Fatal("the stand-in host never reported its seat and session")
		}
	}
	return seatPID, cmd.Process, sid
}

func contains(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
