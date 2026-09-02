//go:build darwin

package councilhost

import (
	"net"

	"golang.org/x/sys/unix"
)

// peerIdentity reads the uid and pid of the process at the other end of a
// connected Unix domain socket.
//
// Two sockopts rather than one, because macOS splits the record Linux returns
// whole: LOCAL_PEERCRED carries the credentials (an xucred, uid and groups) and
// LOCAL_PEERPID carries the pid. Both are the kernel's own record taken at
// connection time, so neither can be forged by the peer.
//
// **Unmeasured on macOS as of 2026-09-02.** This file compiles under
// `GOOS=darwin` and its calls exist in x/sys v0.47.0, but no macOS run of the
// host is recorded yet; the darwin CI job from another lane is what measures
// it. PARITY.md says so.
func peerIdentity(c *net.UnixConn) (uid, pid int, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var (
		cred *unix.Xucred
		cerr error
		perr error
	)
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		pid, perr = unix.GetsockoptInt(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERPID)
	}); err != nil {
		return 0, 0, err
	}
	if cerr != nil {
		return 0, 0, cerr
	}
	if perr != nil {
		return 0, 0, perr
	}
	return int(cred.Uid), pid, nil
}
