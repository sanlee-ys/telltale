//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// unixGroup kills a process tree with a process group.
//
// The child leads its own process group, so killing the negated pgid reaches
// every descendant that has not deliberately left the group. THAT half matches
// the Windows job object, and it is the half this type is named for.
//
// The other half does not match, and this comment used to claim it did. A job
// object carries JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so when telltale's own
// process dies its handle closes and Windows reaps the whole tree — however
// telltale died, including the ways no code can intercept (proc_windows.go says
// so in as many words). A process group has no such property. It names a set of
// processes; it does not bind their lifetime to this one. A group dies when
// something signals it and at no other moment, so a telltale that exits without
// calling kill leaves every seat running, reparented to init, holding a session
// and spending quota.
//
// Measured 2026-08-17 on the macOS box (Intel x86_64, macOS 26.5.2): a program
// of this shape took SIGINT, SIGTERM, SIGHUP and SIGKILL in turn, and its
// Setpgid child survived all four. So on unix the kill has to be MADE on the way
// out. internal/council/signals_unix.go is what makes it, on the three of those
// a process can catch; SIGKILL orphans here as it does everywhere, and nothing
// in this package can change that. PARITY.md records the difference.
type unixGroup struct {
	pgid int
}

func newProcGroup() procGroup { return &unixGroup{} }

func (g *unixGroup) prepare(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (g *unixGroup) attach(cmd *exec.Cmd) error {
	// With Setpgid the child's pgid equals its pid, so there is nothing to look
	// up and no window in which the group does not yet exist — the race the
	// Windows implementation has to document is absent here.
	g.pgid = cmd.Process.Pid
	return nil
}

func (g *unixGroup) kill() error {
	if g.pgid == 0 {
		return nil
	}
	return syscall.Kill(-g.pgid, syscall.SIGKILL)
}

func (g *unixGroup) close() error { return nil }
