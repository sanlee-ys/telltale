//go:build !windows

package councilhost

import (
	"os/exec"
	"syscall"
)

// hideHostConsole starts the host in a session of its own.
//
// The name is the Windows one, because the property is the same one
// spawn_windows.go buys with CREATE_NO_WINDOW: the host must not be attached
// to the client's terminal, or closing that terminal ends the room. Here the
// terminal reaches a process through its SESSION — the hangup goes to the
// session the terminal controls — so Setsid at fork makes the host the leader
// of a new session with no controlling terminal at all, and the terminal
// closing is not an event the host is sent.
//
// exec.Cmd's nil Stdin, Stdout and Stderr already point at the null device,
// which is the other half a daemon needs: a host that inherited the client's
// terminal would hold it open after the client left. NewRoomJob then confirms
// the session leadership and installs the signal handling
// (roomjob_unix.go).
func hideHostConsole(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
