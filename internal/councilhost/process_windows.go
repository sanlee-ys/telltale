//go:build windows

package councilhost

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// ErrHostGone is returned when the pid a discovery file names is not running.
var ErrHostGone = errors.New("councilhost: the process host.json names is not running")

// ErrNotTelltale is returned when the pid IS running and is somebody else.
//
// It is a distinct error from ErrHostGone and never folded into it, because the
// two demand opposite actions. A pid that is gone means a stale file to say so
// about; a pid that belongs to a stranger means a file that must NOT be acted
// on — and `telltale council kill` is a command that would otherwise terminate
// whatever process happened to take the number.
var ErrNotTelltale = errors.New("councilhost: the process host.json names is not a telltale host")

// processStartSlack is how far AFTER the recorded start a process may have been
// created and still be believed.
//
// A host writes host.json after it has created its job and its pipe, so its own
// creation time is always EARLIER than the started_at it records. The slack is
// therefore not for the ordinary case at all — it is for clock granularity and
// for a file rewritten by refreshHostFile, and it is small on purpose: the
// check exists to catch a pid that was recycled by a LATER telltale, and a
// generous window is exactly what would let that through.
const processStartSlack = 5 * time.Second

// verifyHostProcess answers whether pid is a LIVE telltale process that started
// no later than the discovery file claims.
//
// # Why a pid alone answers nothing, and why two readings are needed
//
// design.md §7.28 states the limit this function works around: a pid is
// reusable, and a stale host.json is the normal case after a hard kill. So the
// number in the file is a claim about a process that may no longer exist, and
// may have been replaced by an unrelated one. Two readings settle it, and they
// catch different failures:
//
//   - The IMAGE NAME catches a recycled pid held by something that is not
//     telltale at all — a browser, a compiler, anything. QueryFullProcessImageName
//     is used rather than the module list because it needs only
//     PROCESS_QUERY_LIMITED_INFORMATION, which succeeds against a process of the
//     same user with no privilege at all.
//   - The CREATION TIME catches the narrower case the name check cannot: a
//     LATER telltale process that took the number. A host cannot have started
//     after the file that describes it was written.
//
// Neither is sufficient and both are cheap, so both run. This is §4a.1's rule
// about a measured value applied to an identity: "the file names a pid" is not
// the same claim as "that pid is the host".
func verifyHostProcess(pid int, startedAt time.Time) error {
	if pid <= 0 {
		return fmt.Errorf("%w: host.json names pid %d", ErrHostGone, pid)
	}
	h, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// A pid nothing can be opened on is a pid nothing is running on, as far
		// as this user is concerned. A process of ANOTHER user would also land
		// here, and that is the right answer too: this host would not have been
		// ours.
		return fmt.Errorf("%w: pid %d could not be opened: %v", ErrHostGone, pid, err)
	}
	defer windows.CloseHandle(h)

	// An open handle is NOT proof of life: a handle keeps an exited process's
	// record alive. WaitForSingleObject with a zero timeout is the reading that
	// answers "has this exited", and it is asked before anything else so a dead
	// host is never described by its image name.
	if ev, waitErr := windows.WaitForSingleObject(h, 0); waitErr == nil && ev == windows.WAIT_OBJECT_0 {
		return fmt.Errorf("%w: pid %d has exited", ErrHostGone, pid)
	}

	name, err := processImageName(h)
	if err != nil {
		return fmt.Errorf("%w: could not read the image name of pid %d: %v", ErrNotTelltale, pid, err)
	}
	self, err := selfImageBase()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Base(name), self) {
		return fmt.Errorf("%w: pid %d is %s, and this binary is %s — the pid was reused",
			ErrNotTelltale, pid, filepath.Base(name), self)
	}

	if !startedAt.IsZero() {
		created, err := processCreationTime(h)
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

// processImageName reads a process's full executable path.
func processImageName(h windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

// processCreationTime reads when a process started.
func processCreationTime(h windows.Handle) (time.Time, error) {
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, creation.Nanoseconds()), nil
}

// killProcess terminates a pid that verifyHostProcess has already cleared.
//
// TerminateProcess is what `taskkill /F` does, and design.md §7.29 rules that
// `telltale council kill` uses it deliberately rather than sending a shutdown
// frame. The reason is that this leans on the mechanism §7.28 already MEASURED:
// the room job carries JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, the host holds the
// only handle, so the host's death closes it and Windows reaps every seat.
// TestAHardKilledHostReapsEverySeat runs exactly this call against a stand-in
// host, and
// TestADetachedHostOutlivesItsClientProcessAndStillReapsEverySeat runs it
// against a host a client has already left.
//
// A shutdown frame would have needed the pipe, and the pipe can be held by a
// client — a kill that could not end a room because somebody was in it would be
// useless for the case the command exists for.
func killProcess(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("councilhost: could not open pid %d to end it: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return fmt.Errorf("councilhost: could not end pid %d: %w", pid, err)
	}
	// Waited on, and bounded. The seats are reaped by the job when this handle
	// closes, so a caller that printed "ended" before the process was gone
	// would be reporting an intention rather than a measurement.
	_, _ = windows.WaitForSingleObject(h, 10000)
	return nil
}

// foldWorkspace lower-cases a workspace path for RoomKey.
//
// Windows compares both file paths and pipe names case-insensitively, so
// `C:\code\telltale` and `c:\CODE\Telltale` are one directory and must be one
// room. Folding here is what stops a second host being started over a room the
// operator already has.
func foldWorkspace(path string) string { return strings.ToLower(path) }

// reapOrphans has nothing to reap on Windows, and says zero rather than
// guessing: the room job carries JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so a
// host that is gone took every seat with it when its handle closed. The Unix
// half (process_unix.go) is where a dead host's session still has members.
func reapOrphans(pid int) int { return 0 }

// removeStaleTransport has nothing to remove on Windows: a named pipe is a
// kernel object that goes with its last handle, so a dead host leaves no node.
func removeStaleTransport(name string) {}
