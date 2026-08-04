//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// unixGroup kills a process tree with a process group.
//
// Same guarantee the Windows job object gives, by the older mechanism: the
// child leads its own process group, and killing the negated pgid reaches every
// descendant that has not deliberately left the group.
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
