//go:build windows

package councilhost

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
)

// TestAHostWithNoClientDoesNotWaitForever is the leak guard, measured.
//
// Accept blocks in the kernel and does not watch a context. Without the bounded
// first connect, a host whose client never arrived would hold a job object full
// of nothing, forever, with no surface for the operator to find it on — the
// stale-host failure §7.28 names, arriving on the host's first second.
//
// The context is what this asserts on, because a test that waited out
// firstConnectWindow would take a minute to prove a timeout exists. Cancelling
// covers the same code path: both arms close the listener, and closing it is
// what wakes Accept.
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

	// The pipe must exist before the cancel, or this would measure a race
	// against Listen rather than a wake-up from Accept.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c, err := Dial(h.cfg.PipeName, 50*time.Millisecond); err == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("the host never created its pipe")
		}
	}
	// That probe connected and closed, so this host has already served and lost
	// a client. Start a second one to measure the no-client path cleanly.
	<-done

	h2, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  testPipeName(t),
		Posture:   vendors.PostureRead,
	})
	if err != nil {
		t.Fatal(err)
	}
	done2 := make(chan error, 1)
	go func() { done2 <- h2.Serve(ctx) }()
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case <-done2:
	case <-time.After(15 * time.Second):
		t.Fatal("a cancelled host stayed blocked in Accept. Accept does not watch ctx, " +
			"so Serve must close the listener to wake it — otherwise a host whose client " +
			"never arrives is a process nothing can reach and nothing will reap.")
	}
}

// TestTheFirstConnectWindowIsGenerousButFinite pins the constant's intent.
//
// It bounds a process start and two kernel objects, never a vendor launch, so a
// value in the low seconds would make a busy machine look like a failed host. A
// value of zero, or none at all, is the leak the test above measures.
func TestTheFirstConnectWindowIsGenerousButFinite(t *testing.T) {
	if firstConnectWindow < 10*time.Second {
		t.Fatalf("firstConnectWindow is %s — it covers a process start and two kernel "+
			"objects, and a short value would report a busy machine as a failed host",
			firstConnectWindow)
	}
	if firstConnectWindow > 5*time.Minute {
		t.Fatalf("firstConnectWindow is %s — long enough that a leaked host is a leaked "+
			"host in practice", firstConnectWindow)
	}
}

// TestAGatedRoomIsRefusedBeforeAnythingStarts.
//
// The refusal happens in New, before a job object, a pipe or a process exists.
// A gated seat blocks on a question this host cannot carry, and finding that
// out after the seats are running would leave a room nobody can answer.
func TestAGatedRoomIsRefusedBeforeAnythingStarts(t *testing.T) {
	_, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  testPipeName(t),
		Posture:   vendors.PostureWriteGated,
	})
	if err == nil {
		t.Fatal("a gated room was accepted. That seat blocks on a question this host " +
			"cannot carry, so the room would hang with nothing on screen to say why.")
	}
	if !strings.Contains(err.Error(), "telltale council") {
		t.Fatalf("the refusal does not name the way out: %v", err)
	}
}

// TestAHostRefusesToGuessItsWorkspace.
//
// A host with no workspace would dispatch against whatever directory it
// happened to start in, which is a write posture pointed somewhere nobody
// chose. Refused rather than defaulted.
func TestAHostRefusesToGuessItsWorkspace(t *testing.T) {
	if _, err := New(Config{PipeName: testPipeName(t)}); err == nil {
		t.Fatal("a host with no workspace was accepted")
	}
	if _, err := New(Config{Workspace: t.TempDir()}); err == nil {
		t.Fatal("a host with no transport name was accepted")
	}
}
