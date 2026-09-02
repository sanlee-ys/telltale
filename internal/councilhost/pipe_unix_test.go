//go:build !windows

package councilhost

import (
	"fmt"
	"net"
	"os"
	"testing"
	"time"
)

// testPipeName is a socket path no other test and no other process will use.
//
// NOT under t.TempDir(). A Unix socket path is limited to about a hundred
// bytes by the kernel (108 on Linux, 104 on macOS), and t.TempDir() puts the
// test's whole name in the path — this package's longest is over sixty
// characters on its own — so the socket would fail to bind with EINVAL and the
// failure would read like a broken listener. A short per-test directory under
// the system temp root keeps the path under sixty bytes on Linux and under
// ninety under macOS's per-user temp root.
func testPipeName(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ttc")
	if err != nil {
		t.Fatalf("could not make a socket directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return fmt.Sprintf("%s/h%d-%d.sock", dir, os.Getpid(), time.Now().UnixNano()%1_000_000)
}

// dialUnixRaw is a bare connect, for a test that measures what the host does
// to a client that read no lock and checked no peer.
func dialUnixRaw(name string) (*net.UnixConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: name, Net: "unix"})
}
