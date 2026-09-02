package runner

import "os/exec"

// procGroup isolates a child process and everything it spawns, so cancelling a
// turn cannot leave an agent running invisibly.
//
// The two implementations are not equivalent conveniences over one API: on
// Windows the vendor we most need to kill is a grandchild behind an npm shim,
// so only a Job Object reaches it, while on unix a process group has always
// been enough. Both are exercised by the same tree-kill test.
type procGroup interface {
	// prepare sets platform flags. Called before Start.
	prepare(*exec.Cmd)
	// attach adopts the running child. Called immediately after Start.
	attach(*exec.Cmd) error
	// attachPid adopts a running child by pid, for the one spawn that has no
	// exec.Cmd to hand over.
	//
	// The live seat cannot use os/exec at all — a ConPTY child needs a
	// STARTUPINFOEX attribute list, and SysProcAttr has no field for one
	// (design.md §9.53) — so it calls windows.CreateProcess itself and holds a
	// pid rather than a *exec.Cmd. The containment is NOT forked for it: this
	// is the same job object, measured working unchanged on a pseudoconsole
	// child, and attach above is now one line over it.
	attachPid(pid uint32) error
	// kill terminates the child and its descendants.
	kill() error
	// close releases any handle the group holds.
	close() error
}
