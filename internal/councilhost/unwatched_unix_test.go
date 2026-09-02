//go:build !windows

package councilhost

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
)

// serveInProcess runs a host on this process's own goroutine and hands back a
// connected, handshaken client — unwatched_windows_test.go's helper, over the
// Unix transport. The room job is stubbed for stubRoomJob's reason.
func serveInProcess(t *testing.T, posture vendors.Posture) (name string, c *Client, served chan error) {
	t.Helper()
	countSpawns(t)
	stubRoomJob(t)

	name = testPipeName(t)
	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  name,
		Posture:   posture,
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("could not build the host: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served = make(chan error, 1)
	go func() { served <- h.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		if c != nil {
			_ = c.conn.Close()
		}
		cancel()
		select {
		case <-served:
		case <-time.After(20 * time.Second):
			t.Error("the host did not return after its client went away")
		}
	})

	c, err = Join(JoinConfig{PipeName: name, DialTimeout: 20 * time.Second})
	if err != nil {
		cancel()
		t.Fatalf("the client could not reach the host: %v", err)
	}
	return name, c, served
}

// TestAReadRoomDetaches is the POSITIVE CONTROL for the refusal below.
func TestAReadRoomDetaches(t *testing.T) {
	name, c, _ := serveInProcess(t, vendors.PostureRead)

	if err := c.RequestDetach(); err != nil {
		t.Fatalf("could not ask to detach: %v", err)
	}
	f := awaitAnswer(t, c)
	if f.Kind != KindDetached {
		t.Fatalf("a READ room refused to detach: %+v", f)
	}
	if f.HostPID == 0 {
		t.Error("the detach answer named no pid")
	}
	if err := c.CloseDetached(); err != nil {
		t.Fatalf("could not close after a detach: %v", err)
	}

	// The host must still be there, and the rejoined client receives the
	// current projection at once.
	back, err := Join(JoinConfig{PipeName: name, DialTimeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("the detached host could not be rejoined in-process: %v", err)
	}
	if _, err := back.Next(); err != nil {
		t.Fatalf("the rejoined client received no room: %v", err)
	}
	_ = back.CloseDetached()
}

// TestAWritingRoomRefusesToDetach is design.md §7.29's unwatched-write ruling,
// pinned against the HOST on this platform too.
func TestAWritingRoomRefusesToDetach(t *testing.T) {
	_, c, _ := serveInProcess(t, vendors.PostureWrite)

	if err := c.RequestDetach(); err != nil {
		t.Fatalf("could not ask to detach: %v", err)
	}
	f := awaitAnswer(t, c)
	if f.Kind != KindRefused {
		t.Fatalf("a WRITE room agreed to detach: %+v", f)
	}
	if !strings.Contains(f.Reason, UnwatchedWriteRefusal) || !strings.Contains(f.Reason, UnwatchedWriteRemedy) {
		t.Errorf("the refusal did not carry both ruled sentences:\n%s", f.Reason)
	}
	if err := c.Dispatch("still here?"); err != nil {
		t.Fatalf("the room ended after refusing a detach: %v", err)
	}
	if _, err := c.Next(); err != nil {
		t.Fatalf("the room stopped drawing after refusing a detach: %v", err)
	}
}

// TestTheClientLeavesWhenTheRoomLetsIt drives the whole detach through
// RunClient, which is what an operator actually types into.
func TestTheClientLeavesWhenTheRoomLetsIt(t *testing.T) {
	_, c, _ := serveInProcess(t, vendors.PostureRead)

	var out bytes.Buffer
	outcome, err := RunClient(c, strings.NewReader("/detach\n"), &out, 100)
	if err != nil {
		t.Fatalf("the client failed: %v\n%s", err, out.String())
	}
	if outcome != OutcomeDetached {
		t.Fatalf("a /detach on a read room ended as %v:\n%s", outcome, out.String())
	}
	if !strings.Contains(out.String(), RenderDetached(c.HostPID())) {
		t.Errorf("the client did not say what it left behind:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "telltale council kill") {
		t.Errorf("the client left without naming the way to end what it left:\n%s", out.String())
	}
}

// TestTheClientStaysWhenTheRoomRefusesTheDetach is the same path, refused.
func TestTheClientStaysWhenTheRoomRefusesTheDetach(t *testing.T) {
	_, c, served := serveInProcess(t, vendors.PostureWrite)

	var out bytes.Buffer
	outcome, err := RunClient(c, strings.NewReader("/detach\n/quit\n"), &out, 100)
	if err != nil {
		t.Fatalf("the client failed: %v\n%s", err, out.String())
	}
	if outcome != OutcomeEnded {
		t.Fatalf("a refused detach followed by /quit ended as %v:\n%s", outcome, out.String())
	}
	if !strings.Contains(out.String(), UnwatchedWriteRefusal) {
		t.Errorf("the operator was not told why the room would not be left:\n%s", out.String())
	}
	if strings.Contains(out.String(), "detached. the host keeps") {
		t.Errorf("a REFUSED detach printed the sentence a successful one prints:\n%s", out.String())
	}
	select {
	case <-served:
	case <-time.After(20 * time.Second):
		t.Fatal("the host did not end after /quit")
	}
}

// awaitAnswer reads until the host says something that is not a room frame.
func awaitAnswer(t *testing.T, c *Client) Frame {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		f, err := c.NextFrame()
		if err != nil {
			t.Fatalf("the host stopped talking: %v", err)
		}
		if f.Kind != KindRoom {
			return f
		}
	}
	t.Fatal("the host never answered")
	return Frame{}
}
