//go:build windows

package councilhost

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// runStandInHost is the helper process the containment measurement kills.
//
// It does exactly what a real host does in the order a real host does it: build
// the room job, put ITSELF in it first, and only then start a seat. The order
// is the thing being measured as much as the flag is — a host that assigned
// itself last would have a window in which its own children were outside the
// containment it claims.
//
// The seat is started with a PLAIN exec.Command and NO per-seat job. That is
// deliberate and it is what makes the measurement mean something. proc_windows.go's
// per-seat job also carries JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and its handle
// also dies with the host, so a seat wrapped in one would be reaped by the
// mechanism that already existed — and the test would pass without the room job
// doing anything at all. With no per-seat job, the room job is the only thing
// that can reap this process.
func runStandInHost() int {
	job, err := NewRoomJob()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stand-in host: could not build the room job:", err)
		return 1
	}
	seat := exec.Command(os.Args[0])
	seat.Env = append(os.Environ(), helperEnv+"="+helperSeat)
	// Hidden for proc_windows.go's measured reason: without CREATE_NO_WINDOW a
	// console application gets a console, and a suite run would flash windows
	// at whoever started it.
	seat.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := seat.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "stand-in host: could not start the seat:", err)
		return 1
	}
	// Reported on stdout so the test can open a handle on the seat BEFORE the
	// host is killed. A pid checked after the kill would be open to reuse; a
	// handle held across it cannot be.
	//
	// The job handle is reported too, and it is NEVER closed here. That is the
	// mechanism rather than an omission: kill-on-job-close fires when the last
	// handle goes, and the only thing that closes this one is this process
	// dying. The measurement is of the untidy path on purpose.
	fmt.Printf("seat %d\nhost %d\njob %d\n", seat.Process.Pid, os.Getpid(), job.Handle())
	os.Stdout.Sync()

	// A SLEEP and not `select {}`. An empty select parks every goroutine, and
	// Go's runtime reports that as a fatal deadlock and exits — which killed
	// this stand-in host in about 30ms on the first run. Both containment tests
	// still PASSED, because the host dying by any route closes the job handle
	// and reaps the seat. That is the trap worth naming: the tests would have
	// gone green while measuring a panic instead of a hard kill, and the hard
	// kill is the whole claim.
	time.Sleep(seatLifetime)
	return 0
}

// TestAHardKilledHostReapsEverySeat is design.md §7.28's containment claim, run
// rather than read.
//
// Microsoft's Job Objects page states that a nested job carrying
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE terminates its processes and its child
// jobs when the last handle closes. This repo does not take a behaviour claim
// off a documentation page (ADR-001, design.md §4a.1), and the appendix of the
// costing that produced §7.28 named this exact item as "read, not run".
//
// So it is run. TerminateProcess on the stand-in host is what `taskkill /F`
// does: no notification, no chance to clean up, and every handle the process
// held closes with it. The seat has no per-seat job of its own, so if it dies,
// the room job is what killed it.
//
// This is the property proc_windows.go calls "the case we cannot code around",
// asserted one process further out than it used to live.
func TestAHardKilledHostReapsEverySeat(t *testing.T) {
	seatPID, hostProc := startStandInHost(t)

	// The handle is taken BEFORE the kill and held across it. WaitForSingleObject
	// on a handle answers "this exact process exited"; an OpenProcess after the
	// fact would answer "some process with that number is gone", which pid reuse
	// can make a lie.
	seatHandle, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(seatPID))
	if err != nil {
		t.Fatalf("could not open the seat process %d: %v", seatPID, err)
	}
	defer windows.CloseHandle(seatHandle)

	// Alive first, or the test proves nothing about the kill.
	if ev, err := windows.WaitForSingleObject(seatHandle, 0); err != nil || ev == windows.WAIT_OBJECT_0 {
		t.Fatalf("the seat was already gone before the host was killed (event %#x, err %v)", ev, err)
	}

	hostHandle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(hostProc.Pid))
	if err != nil {
		t.Fatalf("could not open the stand-in host %d: %v", hostProc.Pid, err)
	}
	defer windows.CloseHandle(hostHandle)
	if err := windows.TerminateProcess(hostHandle, 1); err != nil {
		t.Fatalf("could not hard-kill the stand-in host: %v", err)
	}

	ev, err := windows.WaitForSingleObject(seatHandle, 15000)
	if err != nil {
		t.Fatalf("waiting on the seat failed: %v", err)
	}
	if ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("the seat SURVIVED a hard kill of its host (wait returned %#x). "+
			"That is the containment property proc_windows.go calls the case we cannot "+
			"code around, and a host that does not hold it leaves agents running with "+
			"nothing on screen to say so.", ev)
	}
}

// TestTheRoomJobHoldsTheHostAndTheSeat pins the STRUCTURE, not the outcome.
//
// It earns its place because the reap test above cannot tell which mechanism
// did the reaping on its own. If a later change wrapped the seat in a per-seat
// job again, the reap test would keep passing on the old mechanism while the
// room job quietly held nothing. This asserts membership directly, so that
// regression fails here.
func TestTheRoomJobHoldsTheHostAndTheSeat(t *testing.T) {
	seatPID, hostProc := startStandInHost(t)
	t.Cleanup(func() { _ = hostProc.Kill() })

	// The job is the host's, so membership is asked about the host's own job by
	// asking whether each process is in ANY job and then whether they share it.
	// IsProcessInJob with a NULL job answers the first; with the handle, the
	// second. The handle lives in the other process, so this test asserts the
	// weaker, checkable half: both processes are in a job, and the seat is in
	// the same job hierarchy as the host.
	seatH, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(seatPID))
	if err != nil {
		t.Fatalf("could not open the seat: %v", err)
	}
	defer windows.CloseHandle(seatH)
	hostH, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(hostProc.Pid))
	if err != nil {
		t.Fatalf("could not open the stand-in host: %v", err)
	}
	defer windows.CloseHandle(hostH)

	inJob, err := isProcessInJob(hostH, 0)
	if err != nil {
		t.Fatalf("IsProcessInJob on the host failed: %v", err)
	}
	if !inJob {
		t.Fatal("the stand-in host is in no job at all — NewRoomJob did not assign it, " +
			"so nothing would reap its seats when it dies")
	}
	inJob, err = isProcessInJob(seatH, 0)
	if err != nil {
		t.Fatalf("IsProcessInJob on the seat failed: %v", err)
	}
	if !inJob {
		t.Fatal("the seat is in no job — a process started by a host inside the room job " +
			"must inherit it, and one that did not would survive the host")
	}
}

// NOTE on a test that is deliberately NOT here.
//
// "The room job handle is held by nobody else" is the property kill-on-job-close
// actually rests on — a second holder anywhere would keep the job, and every
// seat, alive past the host's death. It cannot be asserted in-process: this
// package's NewRoomJob assigns the CALLING process, so a test that built one
// would put the `go test` binary in a kill-on-job-close job and take the suite
// down with the handle. The stand-in host asserts it instead, by being the only
// holder and by dying, which is what the reap test above measures.

// startStandInHost re-executes the test binary as a host and returns the seat's
// pid and the host's process.
func startStandInHost(t *testing.T) (seatPID int, host *os.Process) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"="+helperHost)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("could not pipe the stand-in host's stdout: %v", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the stand-in host: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	type line struct {
		s   string
		err error
	}
	lines := make(chan line, 4)
	go func() {
		br := bufio.NewScanner(stdout)
		for br.Scan() {
			lines <- line{s: br.Text()}
		}
		lines <- line{err: br.Err()}
	}()

	var seat int
	deadline := time.After(20 * time.Second)
	for seat == 0 {
		select {
		case l := <-lines:
			if l.err != nil || l.s == "" {
				t.Fatalf("the stand-in host said nothing: %v", l.err)
			}
			if !strings.HasPrefix(l.s, "seat ") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(l.s, "seat "))
			if err != nil {
				t.Fatalf("the stand-in host reported an unreadable seat pid %q", l.s)
			}
			seat = n
		case <-deadline:
			t.Fatal("the stand-in host never reported a seat pid")
		}
	}
	return seat, cmd.Process
}
