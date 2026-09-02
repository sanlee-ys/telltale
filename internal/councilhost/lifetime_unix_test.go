//go:build !windows

package councilhost

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
)

// TestAHostWithNoClientDoesNotWaitForever is the leak guard, measured over the
// Unix transport — lifetime_windows_test.go's test, with the one difference the
// transport makes noted where it lands.
func TestAHostWithNoClientDoesNotWaitForever(t *testing.T) {
	countSpawns(t)
	stubRoomJob(t)

	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  testPipeName(t),
		Posture:   vendors.PostureRead,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- h.Serve(ctx) }()

	// The socket must exist before the cancel, or this would measure a race
	// against Listen rather than a wake-up from Accept. ProbePipe rather than
	// Dial: on this transport a probe opens nothing, so the host under test is
	// the same host afterwards.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if st, err := ProbePipe(h.cfg.PipeName); err == nil && st != PipeAbsent {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the host never created its socket")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("a cancelled host stayed blocked in Accept. Accept does not watch ctx, " +
			"so Serve must close the listener to wake it — otherwise a host whose client " +
			"never arrives is a process nothing can reach and nothing will reap.")
	}
}

// TestTheFirstConnectWindowIsGenerousButFinite pins the constant's intent.
func TestTheFirstConnectWindowIsGenerousButFinite(t *testing.T) {
	if firstConnectWindow < 10*time.Second {
		t.Fatalf("firstConnectWindow is %s — a short value would report a busy machine as a failed host",
			firstConnectWindow)
	}
	if firstConnectWindow > 5*time.Minute {
		t.Fatalf("firstConnectWindow is %s — a leaked host in practice", firstConnectWindow)
	}
}

// TestAGatedRoomIsRefusedBeforeAnythingStarts.
func TestAGatedRoomIsRefusedBeforeAnythingStarts(t *testing.T) {
	_, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  testPipeName(t),
		Posture:   vendors.PostureWriteGated,
	})
	if err == nil {
		t.Fatal("a gated room was accepted")
	}
	if !strings.Contains(err.Error(), "telltale council") {
		t.Fatalf("the refusal does not name the way out: %v", err)
	}
}

// TestAHostRefusesToGuessItsWorkspace.
func TestAHostRefusesToGuessItsWorkspace(t *testing.T) {
	if _, err := New(Config{PipeName: testPipeName(t)}); err == nil {
		t.Fatal("a host with no workspace was accepted")
	}
	if _, err := New(Config{Workspace: t.TempDir()}); err == nil {
		t.Fatal("a host with no transport name was accepted")
	}
}

// TestASocketDirectoryOtherAccountsCanEnterIsRefused is the directory-mode
// arm of the boundary, measured.
//
// The Windows descriptor test creates a pipe with the default descriptor and
// reads Everyone back off it. The Unix equivalent of "the default is the leak"
// is a directory somebody made 0755: Listen must refuse to put a socket that
// carries transcript content there, and name the one-line remedy.
func TestASocketDirectoryOtherAccountsCanEnterIsRefused(t *testing.T) {
	name := testPipeName(t)
	dir := name[:strings.LastIndex(name, "/")]
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Listen(name)
	if err == nil {
		t.Fatal("a host listened from a directory other accounts can enter")
	}
	if !strings.Contains(err.Error(), "chmod 700") {
		t.Fatalf("the refusal names no remedy: %v", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	ln, err := Listen(name)
	if err != nil {
		t.Fatalf("the same directory at 0700 was refused: %v", err)
	}
	ln.Close()
}
