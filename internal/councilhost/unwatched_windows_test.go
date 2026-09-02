//go:build windows

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
// connected, handshaken client.
//
// The room job is stubbed for newRoomJob's measured reason: a real one would put
// the `go test` binary into a kill-on-job-close job, and the first Shutdown
// would terminate the suite mid-run. The containment claim is measured in its
// own process instead (detach_windows_test.go), because a claim about a process
// dying cannot be asserted by the process making it.
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
	// CLOSED as well as sent on, so a test that reads it AND the cleanup that
	// reads it again both get an answer. A single buffered send is consumed by
	// whoever reads first, and the loser then waits out its whole timeout for a
	// host that returned perfectly.
	go func() { served <- h.Serve(ctx); close(served) }()
	t.Cleanup(func() {
		// The CONNECTION is closed before the context is cancelled, and the
		// order is not tidiness. serveClient's read loop parks in the kernel and
		// does not watch a context — it checks one only after a frame arrives —
		// so a cancel alone reaches a host with a client attached no sooner than
		// that client's next word. Closing the pipe is what wakes it. The cancel
		// still runs, because it is what wakes an Accept waiting after a detach.
		//
		// Production never meets this: runCouncilHost passes context.Background,
		// so nothing there cancels a host that has a client.
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

// TestAReadRoomDetaches is the POSITIVE CONTROL for the refusal below, and it
// is not optional.
//
// Without it, a detachAllowed that refused everything would pass the refusal
// test perfectly. A gate is only measured by a pair.
func TestAReadRoomDetaches(t *testing.T) {
	name, c, _ := serveInProcess(t, vendors.PostureRead)

	if err := c.RequestDetach(); err != nil {
		t.Fatalf("could not ask to detach: %v", err)
	}
	f := awaitAnswer(t, c)
	if f.Kind != KindDetached {
		t.Fatalf("a READ room refused to detach: %+v.\n"+
			"design.md §7.29 rules the refusal on the write posture only — a read room has "+
			"nothing to do while nobody is watching, so there is nothing to refuse.", f)
	}
	if f.HostPID == 0 {
		t.Error("the detach answer named no pid. The operator now owns a process with no " +
			"terminal and no window, and a process they cannot name is one they cannot end.")
	}
	if err := c.CloseDetached(); err != nil {
		t.Fatalf("could not close after a detach: %v", err)
	}

	// The host must still be there. A detach that ended the room would pass
	// every assertion above.
	back, err := Join(JoinConfig{PipeName: name, DialTimeout: 20 * time.Second})
	if err != nil {
		t.Fatalf("the detached host could not be rejoined in-process: %v", err)
	}
	_ = back.CloseDetached()
}

// TestAWritingRoomRefusesToDetach is design.md §7.29's unwatched-write ruling,
// pinned against the HOST.
//
// Enforced in the host and never in the client, because the host is the process
// that would keep running — a check in the client alone is a check a second
// client could simply not make. So this asks the host directly, over the wire,
// with no client-side logic in the way.
func TestAWritingRoomRefusesToDetach(t *testing.T) {
	_, c, _ := serveInProcess(t, vendors.PostureWrite)

	if err := c.RequestDetach(); err != nil {
		t.Fatalf("could not ask to detach: %v", err)
	}
	f := awaitAnswer(t, c)
	if f.Kind != KindRefused {
		t.Fatalf("a WRITE room agreed to detach: %+v.\n"+
			"§7.28 refuses to host a gated room at all, so a hosted room that is not "+
			"read-only runs every tool call with nobody to ask — which is what --auto means "+
			"on the room's own surface. telltale never leaves an agent working while nobody "+
			"is watching.", f)
	}
	if !strings.Contains(f.Reason, UnwatchedWriteRefusal) {
		t.Errorf("the refusal did not carry the ruled sentence.\ngot:  %q\nwant: %q",
			f.Reason, UnwatchedWriteRefusal)
	}
	// §9.17's tell: a refusal with no remedy is this room's stated defect.
	if !strings.Contains(f.Reason, UnwatchedWriteRemedy) {
		t.Errorf("the refusal names no way out:\n%s", f.Reason)
	}
	if !strings.Contains(f.Reason, "--read") {
		t.Errorf("the remedy does not name the flag that changes the answer:\n%s", f.Reason)
	}

	// A REFUSED detach leaves the client exactly where it was. A host that ended
	// the room here would be ending the room the operator was just told they
	// could not leave, which is worse than either outcome on its own.
	if err := c.Dispatch("still here?"); err != nil {
		t.Fatalf("the room ended after refusing a detach: %v", err)
	}
	if _, err := c.Next(); err != nil {
		t.Fatalf("the room stopped drawing after refusing a detach: %v", err)
	}
}

// TestTheRefusalIsOneSentenceAndTheRemedyIsAnother pins the SHAPE the ruling
// chose, not only the words.
//
// §7.29 rules the refusal one sentence on purpose and puts the remedy on a
// second line, because a run-on sentence is not a remedy. A later edit that
// merged them would still pass every "contains" assertion above.
func TestTheRefusalIsOneSentenceAndTheRemedyIsAnother(t *testing.T) {
	if strings.Contains(UnwatchedWriteRefusal, "\n") {
		t.Errorf("the refusal is more than one line: %q", UnwatchedWriteRefusal)
	}
	if n := strings.Count(UnwatchedWriteRefusal, ". "); n > 0 {
		t.Errorf("the refusal is more than one sentence: %q", UnwatchedWriteRefusal)
	}
	if !strings.HasSuffix(UnwatchedWriteRefusal, ".") {
		t.Errorf("the refusal does not end as a sentence: %q", UnwatchedWriteRefusal)
	}
	whole := RenderDetachRefused()
	if !strings.Contains(whole, UnwatchedWriteRefusal) || !strings.Contains(whole, UnwatchedWriteRemedy) {
		t.Errorf("the rendered refusal does not carry both halves:\n%s", whole)
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
		t.Fatalf("a /detach on a read room ended as %v; OutcomeDetached was expected:\n%s",
			outcome, out.String())
	}
	// The DETACHED notice itself, not a phrase from the banner. The banner names
	// /detach every time the room opens, so a looser assertion here would pass
	// on a client that printed nothing at all when it actually left.
	if !strings.Contains(out.String(), RenderDetached(c.HostPID())) {
		t.Errorf("the client did not say what it left behind:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "telltale council kill") {
		t.Errorf("the client left without naming the way to end what it left:\n%s", out.String())
	}
}

// TestTheClientStaysWhenTheRoomRefusesTheDetach is the same path, refused.
//
// The two outcomes must be different values and not one value plus a printed
// sentence: a caller decides whether a host is still running from the outcome,
// and a caller that guessed wrong would either orphan a host or claim one is
// there when it is not.
func TestTheClientStaysWhenTheRoomRefusesTheDetach(t *testing.T) {
	_, c, served := serveInProcess(t, vendors.PostureWrite)

	var out bytes.Buffer
	outcome, err := RunClient(c, strings.NewReader("/detach\n/quit\n"), &out, 100)
	if err != nil {
		t.Fatalf("the client failed: %v\n%s", err, out.String())
	}
	if outcome != OutcomeEnded {
		t.Fatalf("a refused detach followed by /quit ended as %v; OutcomeEnded was expected:\n%s",
			outcome, out.String())
	}
	if !strings.Contains(out.String(), UnwatchedWriteRefusal) {
		t.Errorf("the operator was not told why the room would not be left:\n%s", out.String())
	}
	// The detached notice's own opening, which the banner does not carry.
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
//
// Room frames arrive on the same stream and on their own schedule, so a test
// that read exactly one frame would be asserting on whichever arrived first.
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
