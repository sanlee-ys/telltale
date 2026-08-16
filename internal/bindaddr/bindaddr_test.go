package bindaddr

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// TestARealCollisionIsDetectedOnThisPlatform is the whole reason InUse is not
// one errors.Is call. It provokes a real collision rather than constructing an
// error value, so the arm this platform actually needs is the arm the suite
// exercises: the numeric one on Windows (errno 10048, where
// syscall.EADDRINUSE is a synthetic constant and errors.Is answers false), and
// the errors.Is one everywhere else. A future simplification to either single
// arm fails here on the platform it breaks.
func TestARealCollisionIsDetectedOnThisPlatform(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	second, err := net.Listen("tcp", holder.Addr().String())
	if err == nil {
		second.Close()
		t.Skip("this platform allows a second bind on a held port; nothing to detect")
	}
	if !InUse(err) {
		t.Fatalf("InUse did not recognise a real collision on this platform: %#v", err)
	}
}

// The synthetic half of the same claim, stated as an assertion rather than
// left implicit in a doc comment: a bare errors.Is check is NOT sufficient on
// Windows, and this test says which platform it is measuring so a failure
// reads as "the measurement moved", not as "the test is flaky".
func TestTheNumericArmCoversWhatErrorsIsMisses(t *testing.T) {
	err := error(syscall.Errno(wsaeAddrInUse))
	if !InUse(err) {
		t.Errorf("InUse(errno %d) = false: the numeric arm is gone", wsaeAddrInUse)
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		// Unix: 10048 is not EADDRINUSE there either, so this branch simply
		// does not fire. Windows: this firing would mean the synthetic
		// constant measured in 2026-08-16 has changed, and the doc comment
		// needs re-measuring rather than the code needing a patch.
		t.Log("errors.Is matches errno 10048 on this platform; re-check bindaddr's measurement")
	}
}

func TestInUseIgnoresUnrelatedErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("bind: some other trouble"),
		syscall.Errno(2),
		nil,
	} {
		if InUse(err) {
			t.Errorf("InUse(%v) = true, want false", err)
		}
	}
}

func TestIsLoopbackAcceptsOnlyLoopback(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.0.0.9", "::1", "localhost"} {
		if !IsLoopback(host) {
			t.Errorf("IsLoopback(%q) = false, want true", host)
		}
	}
	for _, host := range []string{"0.0.0.0", "192.168.1.10", "::", "example.com", ""} {
		if IsLoopback(host) {
			t.Errorf("IsLoopback(%q) = true, want false", host)
		}
	}
}

func TestNextIsNeverThePortThatFailed(t *testing.T) {
	for _, tc := range []struct{ host, port, fallback, want string }{
		{"127.0.0.1", "4318", "4318", "127.0.0.1:4319"},
		{"127.0.0.1", "4519", "4519", "127.0.0.1:4520"},
		{"127.0.0.1", "4520", "4519", "127.0.0.1:4521"},
		{"::1", "4519", "4519", "[::1]:4520"},
		// Unparseable and last-port fall back to the caller's default plus one
		// rather than to arithmetic on nonsense.
		{"127.0.0.1", "sink", "4519", "127.0.0.1:4520"},
		{"127.0.0.1", "65535", "4519", "127.0.0.1:4520"},
		{"127.0.0.1", "0", "4318", "127.0.0.1:4319"},
	} {
		if got := Next(tc.host, tc.port, tc.fallback); got != tc.want {
			t.Errorf("Next(%q, %q, %q) = %q, want %q", tc.host, tc.port, tc.fallback, got, tc.want)
		}
	}
}
