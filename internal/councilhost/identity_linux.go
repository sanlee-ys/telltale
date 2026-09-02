//go:build linux

package councilhost

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// truncatedImageLen is the length at which this platform truncates an image
// name. Linux reports the full executable path, so nothing is truncated.
const truncatedImageLen = -1

// processImage reads a process's executable name from /proc/<pid>/exe.
//
// The link is readable by the process's own user without any privilege, which
// is the same access QueryFullProcessImageName needs on Windows. A deleted
// binary — a telltale rebuilt while a host from the old build is running —
// reads with a " (deleted)" suffix, and the suffix is stripped: the process is
// still the host it was.
func processImage(pid int) (string, error) {
	p, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return "", err
	}
	return filepath.Base(strings.TrimSuffix(p, " (deleted)")), nil
}

// processStart reads when a process started, from /proc/<pid>/stat.
//
// Field 22 is the start time in clock ticks since boot; /proc/stat's btime is
// the boot time. The tick is USER_HZ, which is 100 on every Linux ABI — a
// kernel constant for /proc reporting, not the scheduler's HZ — so no sysconf
// call is needed to convert it.
func processStart(pid int) (time.Time, error) {
	fields, err := statFields(pid)
	if err != nil {
		return time.Time{}, err
	}
	// fields[0] is the state (field 3 of the file), so field N is fields[N-3].
	if len(fields) < 20 {
		return time.Time{}, fmt.Errorf("/proc/%d/stat has %d fields after the command", pid, len(fields))
	}
	ticks, err := strconv.ParseInt(fields[19], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	btime, err := bootTime()
	if err != nil {
		return time.Time{}, err
	}
	return btime.Add(time.Duration(ticks) * (time.Second / 100)), nil
}

// sessionMembers lists every pid whose session id is sid, by walking /proc.
//
// Field 6 of /proc/<pid>/stat is the session. A process that vanished between
// the directory listing and the read is skipped rather than reported: the
// listing is a snapshot and a race here is the ordinary state of a process
// table.
func sessionMembers(sid int) ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid <= 0 {
			continue
		}
		fields, err := statFields(pid)
		if err != nil || len(fields) < 4 {
			continue
		}
		if s, err := strconv.Atoi(fields[3]); err == nil && s == sid {
			out = append(out, pid)
		}
	}
	return out, nil
}

// statFields returns the fields of /proc/<pid>/stat AFTER the command name.
//
// The command is wrapped in parentheses and may itself contain spaces and
// parentheses, so the split is taken from the LAST ')' rather than the first
// space — the one parsing rule everybody who reads this file eventually learns.
func statFields(pid int) ([]string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil, err
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return nil, errors.New("no command in stat")
	}
	return strings.Fields(s[i+1:]), nil
}

// bootTime reads btime from /proc/stat.
func bootTime() (time.Time, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return time.Time{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "btime "); ok {
			secs, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(secs, 0), nil
		}
	}
	if err := sc.Err(); err != nil {
		return time.Time{}, err
	}
	return time.Time{}, errors.New("no btime in /proc/stat")
}
