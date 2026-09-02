//go:build windows

package councilhost

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// RoomJob is one Job Object holding the HOST and, through the per-seat jobs,
// every vendor process under it.
//
// It exists to preserve one property while moving the process that owns it.
// runner/proc_windows.go states that property in its own words:
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE covers "the case we cannot code around" —
// if telltale itself dies, the handle closes and Windows reaps the whole tree.
// Splitting the room into a host and a client would have quietly moved that
// guarantee off the process that now owns the agents. This moves it back, one
// process outward.
//
// The per-seat job is UNCHANGED and stays. Seat eviction and turn cancellation
// still kill exactly one tree, which is what Session.Kill means. This is a
// second, outer job, and the two are not alternatives:
//
//   - The per-seat job answers "kill this one seat".
//   - The room job answers "the host died and nobody is left to kill anything".
//
// Nested jobs are supported from Windows 8 and Server 2012 onward. Before
// Windows 8 a second assignment failed with ERROR_ACCESS_DENIED. telltale is
// Windows-first with windows-latest CI (ADR-002), so this is inside the support
// window.
//
// The platform's own claim is that a nested job carrying KILL_ON_JOB_CLOSE
// terminates its processes AND its child jobs when the last handle closes. This
// repo does not take a behaviour claim off a documentation page (ADR-001), so
// TestAHardKilledHostReapsEverySeat runs it: it re-executes the test binary as
// a stand-in host, TerminateProcess's it the way `taskkill /F` does, and
// asserts a grandchild seat is gone.
type RoomJob struct {
	job windows.Handle
}

// NewRoomJob creates the job and puts THIS process in it.
//
// The order matters and is not incidental. The host assigns itself FIRST, so
// that every seat it later starts is created by a process already inside the
// job and lands in the hierarchy under it. A host that assigned itself last
// would have a window in which its own children were outside the containment it
// is about to claim.
//
// The handle is held by this process and by nothing else. That is what makes
// the death of this process the trigger: there is no other holder to keep the
// job alive.
func NewRoomJob() (*RoomJob, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("councilhost: could not create the room job: %w", err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("councilhost: could not set kill-on-job-close on the room job: %w", err)
	}
	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return nil, fmt.Errorf("councilhost: could not put the host in the room job: %w", err)
	}
	return &RoomJob{job: job}, nil
}

// Handle exposes the job handle for the tests that assert membership.
func (j *RoomJob) Handle() windows.Handle {
	if j == nil {
		return 0
	}
	return j.job
}

// Signalled is the channel the Unix job delivers a caught SIGTERM on, and it
// is nil here: the room job needs no handler, because kill-on-job-close fires
// on the host's death by any route. A receive from a nil channel blocks
// forever, which is exactly what Serve's watcher should do on this platform.
func (j *RoomJob) Signalled() <-chan os.Signal { return nil }

// Kill terminates every process in the job, the host included.
//
// Used by the clean-quit path AFTER the seats have been asked to stop, so that
// a vendor which ignores its own kill still goes. It is deliberately the last
// move rather than the first: a job termination gives a process no chance to
// finish a write, and the room's own teardown wants that chance.
func (j *RoomJob) Kill() error {
	if j == nil || j.job == 0 {
		return nil
	}
	return windows.TerminateJobObject(j.job, 1)
}

// Close releases the handle.
//
// Calling this while seats are running is what reaps them, so it is NOT a
// cleanup to sprinkle on a defer. The host closes it on the way out, after the
// seats are already gone, and every other path lets process death close it.
func (j *RoomJob) Close() error {
	if j == nil || j.job == 0 {
		return nil
	}
	h := j.job
	j.job = 0
	return windows.CloseHandle(h)
}

// isProcessInJob answers whether a process belongs to a job.
//
// x/sys/windows v0.47.0 does not export IsProcessInJob, so it is bound here.
// That is the same call decisions/001 already sanctioned for the hand-rolled
// OTLP reader, the byte-level SQLite reader and the hand-rolled WebSocket: a
// page of checked code rather than a dependency.
//
// It exists for TestTheRoomJobHoldsTheHostAndTheSeat, and the test earns it. A
// reap test alone cannot tell WHICH job did the reaping — the per-seat job also
// carries kill-on-job-close and its handle also dies with the host — so a pass
// there could be bought entirely by the mechanism that already existed. This
// asserts the structure instead.
func isProcessInJob(process, job windows.Handle) (bool, error) {
	var res int32
	r, _, err := procIsProcessInJob.Call(
		uintptr(process),
		uintptr(job),
		uintptr(unsafe.Pointer(&res)),
	)
	if r == 0 {
		return false, err
	}
	return res != 0, nil
}

var (
	modkernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procIsProcessInJob = modkernel32.NewProc("IsProcessInJob")
)
