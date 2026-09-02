//go:build darwin

package councilhost

import (
	"time"

	"golang.org/x/sys/unix"
)

// truncatedImageLen is MAXCOMLEN: kinfo_proc's p_comm holds at most sixteen
// bytes of the executable name, so an image that reads exactly sixteen long
// is compared as a prefix of this binary's name (sameImage).
const truncatedImageLen = 16

// processImage reads a process's command name from the kernel's process
// record.
//
// kern.proc.pid answers for any pid this user may see, with no privilege, and
// it is the reading `ps` itself makes. The name is p_comm rather than the full
// path — the path needs proc_pidpath from libproc, which is a C library this
// package does not link — so the sixteen-byte truncation is carried into
// sameImage rather than hidden by a longer read.
//
// **Unmeasured on macOS as of 2026-09-02.** Every call here exists in x/sys
// v0.47.0 and the file builds under `GOOS=darwin`; no macOS run is recorded,
// and PARITY.md says so.
func processImage(pid int) (string, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", err
	}
	return unix.ByteSliceToString(kp.Proc.P_comm[:]), nil
}

// processStart reads when a process started, from the same record.
func processStart(pid int) (time.Time, error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(kp.Proc.P_starttime.Sec, int64(kp.Proc.P_starttime.Usec)*1000), nil
}

// sessionMembers lists every pid whose session id is sid.
//
// kinfo_proc carries the process group but not the session id, so the whole
// table is listed and getsid(2) is asked per pid. A pid that vanished between
// the two is skipped, as on Linux.
func sessionMembers(sid int) ([]int, error) {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil, err
	}
	var out []int
	for i := range procs {
		pid := int(procs[i].Proc.P_pid)
		if pid <= 0 {
			continue
		}
		if s, err := unix.Getsid(pid); err == nil && s == sid {
			out = append(out, pid)
		}
	}
	return out, nil
}
