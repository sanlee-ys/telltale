//go:build linux

package councilhost

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerIdentity reads the uid and pid of the process at the other end of a
// connected Unix domain socket.
//
// SO_PEERCRED is the kernel's own record of who called connect() or listen(),
// taken at connection time, so it cannot be forged by the peer and cannot go
// stale under it. It is the Unix reading of what GetNamedPipeClientProcessId
// plus the token user answer on Windows, and both connect directions use it:
// the server to refuse a client of another account, the client to refuse a
// socket whose server is not this user (the anti-squatting arm).
func peerIdentity(c *net.UnixConn) (uid, pid int, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *unix.Ucred
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		cred, gerr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if gerr != nil {
		return 0, 0, gerr
	}
	return int(cred.Uid), int(cred.Pid), nil
}
