//go:build !windows

package councilhost

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

// TestAHomeTooLongForASocketPathRetreatsToTheShortDirectory pins PipeName's
// retreat: a council directory whose path would not fit in sun_path yields a
// name that does, and the same key yields the same name twice, which is what
// lets a host and its clients meet without a discovery file.
func TestAHomeTooLongForASocketPathRetreatsToTheShortDirectory(t *testing.T) {
	long := filepath.Join(t.TempDir(), strings.Repeat("h", 90))
	t.Setenv("HOME", long)
	name := PipeName("k")
	if len(name) > sunPathMax {
		t.Fatalf("PipeName retreated to a path that still does not fit: %d bytes, %s", len(name), name)
	}
	if !strings.HasPrefix(name, shortSocketDir()) {
		t.Fatalf("the retreat went somewhere other than the short directory: %s", name)
	}
	if again := PipeName("k"); again != name {
		t.Fatalf("PipeName is not deterministic across calls: %s vs %s", name, again)
	}
	// A literal rather than t.TempDir(): this test's own name puts a temp
	// directory past the bound, which is the case above, not this one.
	// PipeName reads no disk, so the home need not exist.
	t.Setenv("HOME", "/tmp/short-home")
	if short := PipeName("k"); strings.HasPrefix(short, shortSocketDir()) {
		t.Fatalf("a home that fits retreated anyway: %s", short)
	}
}
