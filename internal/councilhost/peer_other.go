//go:build !windows && !linux && !darwin

package councilhost

import (
	"errors"
	"net"
)

// peerIdentity refuses on a Unix this package has no peer-credential reading
// for.
//
// Refused rather than assumed. A connection whose peer cannot be identified is
// a connection the boundary cannot vouch for, and Accept and Dial both treat
// this error as a refusal — so on such a platform the host builds and every
// connection is turned away with a sentence, which is the honest state until
// somebody measures the platform's own sockopt here.
func peerIdentity(c *net.UnixConn) (uid, pid int, err error) {
	return 0, 0, errors.New("councilhost: no peer-credential reading exists for this platform yet")
}
