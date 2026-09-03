//go:build windows

package councilhost

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
	"golang.org/x/sys/windows"
)

// TestOneClientDrivesAHostedRoomEndToEnd is the claim this whole change has to
// support: a client can drive a hosted room from end to end.
//
// Everything on the path is REAL except the vendor: a real named pipe with the
// real descriptor, the real peer check in both directions, the real handshake,
// the real dispatch, the real fold from runner events into the room, the real
// coalescing tick, and the real render. What is stubbed is the ONE thing a test
// must never do — start a vendor CLI, which would be a live agent turn on the
// operator's own account (see TestMain).
//
// The room job is stubbed too, and newRoomJob's doc says why: a real one would
// put this test binary in a kill-on-job-close job and take the suite down. The
// containment claim is measured in its own process instead
// (TestAHardKilledHostReapsEverySeat), because a claim about a process dying
// cannot be asserted by the process making it.
func TestOneClientDrivesAHostedRoomEndToEnd(t *testing.T) {
	log := countSpawns(t)
	stubRoomJob(t)

	name := testPipeName(t)
	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  name,
		Posture:   vendors.PostureRead,
		Roster:    []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("could not build the host: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- h.Serve(ctx) }()

	conn, err := Dial(name, 5*time.Second)
	if err != nil {
		t.Fatalf("the client could not reach the host's pipe: %v", err)
	}
	defer conn.Close()

	// The client half of the peer check ran inside Dial. Asserting the pid it
	// found is what proves it was not skipped: GetNamedPipeServerProcessId is
	// the anti-squatting arm, and a Dial that returned without consulting it
	// would look identical from the outside.
	if conn.PeerPID() != os.Getpid() {
		t.Fatalf("the client connected to pid %d, not to this process %d — "+
			"the server-identity check did not run", conn.PeerPID(), os.Getpid())
	}

	fr, fw := NewFrameReader(conn), NewFrameWriter(conn)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		t.Fatalf("could not send the handshake: %v", err)
	}
	welcome, err := fr.Read()
	if err != nil {
		t.Fatalf("no welcome came back: %v", err)
	}
	if welcome.Kind != KindWelcome || welcome.HostPID != os.Getpid() {
		t.Fatalf("the handshake answered %+v", welcome)
	}

	// The idle room goes out before any tick, so a client that just connected
	// draws something at once rather than an empty screen for up to one tick.
	idle, err := readRoom(t, fr)
	if err != nil {
		t.Fatalf("no opening room frame: %v", err)
	}
	if idle.Turn != 0 {
		t.Fatalf("the opening room claimed turn %d; nothing had been dispatched", idle.Turn)
	}
	if len(idle.Seats) != 1 || idle.Seats[0].Phase != PhaseIdle {
		t.Fatalf("the opening room's seats were %+v; one idle seat was expected", idle.Seats)
	}

	if err := fw.Write(Frame{Kind: KindDispatch, Prompt: "gm"}); err != nil {
		t.Fatalf("could not dispatch: %v", err)
	}

	// The turn's arrival is read off the WIRE, never off the host's own memory.
	// A test that asserted on h.Snapshot() would pass over a host that folded
	// correctly and never sent anything, which is the whole failure a wire
	// makes possible.
	turned := awaitRoom(t, fr, func(r Room) bool { return r.Turn == 1 })
	if turned.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("a dispatched seat with no output yet drew as %q; waiting and streaming "+
			"are different claims and a seat that has produced nothing is waiting",
			turned.Seats[0].Phase)
	}
	// Waited for rather than sampled: the room's turn is bumped BEFORE any seat
	// is spawned, so reading the count here once could land before the append.
	log.awaitN(t, 1)

	// Vendor output is injected as the runner would deliver it, so the fold
	// under test is the real one.
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "morning"}
	streaming := awaitRoom(t, fr, func(r Room) bool { return r.Seats[0].Body == "morning" })
	if streaming.Seats[0].Phase != PhaseStreaming {
		t.Fatalf("text arrived and the seat still drew as %q — text ARRIVING is what "+
			"separates streaming from waiting", streaming.Seats[0].Phase)
	}

	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-1"}
	// The turn ends the way the claude adapter really ends one: a `result`
	// line, which is KindMeta with EndsTurn set. This used to feed KindDone
	// with EndsTurn, a shape no adapter emits, and the test passed while the
	// live room drew a finished seat as `streaming` for the rest of its life.
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindMeta, EndsTurn: true, Text: "morning"}
	done := awaitRoom(t, fr, func(r Room) bool { return r.Seats[0].Phase == PhaseDone })
	if done.Seats[0].SessionID != "sess-1" {
		t.Fatalf("the session id did not cross the wire: %+v", done.Seats[0])
	}

	// The render is the client's half, and it is pure over what arrived. Drawn
	// at two widths from the SAME room, because repaint-at-any-width is the
	// property that makes a resize protocol unnecessary.
	wide := Render(done, 100)
	narrow := Render(done, 40)
	for _, want := range []string{"morning", "claude", "done"} {
		if !strings.Contains(wide, want) {
			t.Fatalf("the render dropped %q:\n%s", want, wide)
		}
		if !strings.Contains(narrow, want) {
			t.Fatalf("the narrow render dropped %q:\n%s", want, narrow)
		}
	}
	for _, line := range strings.Split(narrow, "\n") {
		if len([]rune(line)) > 40 {
			t.Fatalf("a 40-column render produced a %d-column line: %q", len([]rune(line)), line)
		}
	}

	if err := fw.Write(Frame{Kind: KindShutdown}); err != nil {
		t.Fatalf("could not send the shutdown: %v", err)
	}
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("the host returned %v on a clean shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the host did not return after a shutdown frame")
	}
}

// TestATurnIsNotPersistedAnywhere is the read/write boundary, asserted rather
// than promised.
//
// design.md §7.28 rules that transcript content NEVER reaches disk: not under
// ~/.telltale/, not in a temp file, not compressed, not "only the last N
// turns". resume.go already ruled it for the same data, and the rule does not
// change because the process holding the data changed.
//
// So a marker is pushed through the fold and the host's whole world — its
// workspace and a redirected HOME — is then searched for it.
func TestATurnIsNotPersistedAnywhere(t *testing.T) {
	countSpawns(t)
	stubRoomJob(t)

	const marker = "TELLTALEsecretCONVERSATIONmarker"
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	work := t.TempDir()

	h, err := New(Config{
		Workspace: work,
		PipeName:  testPipeName(t),
		Posture:   vendors.PostureRead,
		Roster:    []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	defer cancel()
	go h.fold()

	h.dispatch(marker+"-prompt", nil)
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: marker + "-reply"}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(h.Snapshot().Seats[0].Body, marker) {
		if time.Now().After(deadline) {
			t.Fatal("the fold never saw the marker, so the search below would prove nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}

	// The discovery file is written on the same path a real host writes it, so
	// the search covers the one file this design DOES add.
	if err := WriteHostFile(home, HostFile{
		PID: os.Getpid(), Pipe: h.cfg.PipeName, StartedAt: time.Now(),
		Workspace: work, Seats: []model.VendorID{model.VendorClaude}, Turn: 1,
	}); err != nil {
		t.Fatal(err)
	}

	for _, root := range []string{home, work} {
		// The searcher is proved before it is trusted. Without this the
		// assertion below passes on a broken walk, a wrong root, or a read
		// error — all of which look identical to "clean".
		assertGrepTreeWorks(t, root, marker+"-canary")
		if found := grepTree(t, root, marker); found != "" {
			t.Fatalf("transcript content reached disk at %s. §7.28 rules that the room's "+
				"conversation lives in host memory and dies with the host, on resume.go's "+
				"own ruling for the same data.", found)
		}
	}
}

// TestAHostAcrossAProcessBoundaryTakesAClient drives a host in ANOTHER process.
//
// The in-process test above exercises every layer except the process boundary
// itself, and the boundary is the point of the change. This one re-executes the
// test binary as a real host — real room job, real pipe, real Serve — and
// connects to it from here.
//
// The roster is EMPTY, and that is what makes it safe. A host with no seats
// cannot spawn a vendor by any path, so this crosses the boundary without going
// anywhere near a billed turn. It does not go through startHost either: that
// var starts telltale.exe, and TestMain refuses it precisely because the
// process it starts would spawn vendors of its own.
func TestAHostAcrossAProcessBoundaryTakesAClient(t *testing.T) {
	name := testPipeName(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		helperEnv+"="+helperServe,
		helperPipeEnv+"="+name,
		helperWorkEnv+"="+t.TempDir(),
	)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the out-of-process host: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	conn, err := Dial(name, 20*time.Second)
	if err != nil {
		t.Fatalf("could not reach the out-of-process host: %v", err)
	}
	defer conn.Close()
	if conn.PeerPID() != cmd.Process.Pid {
		t.Fatalf("the pipe's server is pid %d, not the host this test started (%d)",
			conn.PeerPID(), cmd.Process.Pid)
	}

	fr, fw := NewFrameReader(conn), NewFrameWriter(conn)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	welcome, err := fr.Read()
	if err != nil {
		t.Fatalf("the out-of-process host sent no welcome: %v", err)
	}
	if welcome.Kind != KindWelcome {
		t.Fatalf("the out-of-process host answered %+v", welcome)
	}
	if welcome.HostPID != cmd.Process.Pid {
		t.Fatalf("the host reported pid %d and it is %d", welcome.HostPID, cmd.Process.Pid)
	}
	room, err := readRoom(t, fr)
	if err != nil {
		t.Fatalf("no room crossed the process boundary: %v", err)
	}
	if room.Version != RoomVersion {
		t.Fatalf("the room that arrived carries version %d", room.Version)
	}

	// A second client is REFUSED, and the operating system does the refusing:
	// the pipe is created with one instance, so a second open comes back
	// ERROR_PIPE_BUSY. §7.28 rules one client at a time because multi-client
	// attach is tmux's feature and must not be acquired by accident.
	if second, err := Dial(name, 500*time.Millisecond); err == nil {
		second.Close()
		t.Fatal("a second client was served. One client at a time is a ruling, and the " +
			"single pipe instance is what enforces it.")
	} else if !strings.Contains(err.Error(), "one client at a time") {
		t.Fatalf("a second client was refused for the wrong reason: %v", err)
	}

	// A bare disconnect ends the room, which is what keeps detach unexposed.
	conn.Close()
	waited := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(15 * time.Second):
		t.Fatal("the host outlived its client. Detach is NOT exposed in this change, so a " +
			"disconnect must end the room — a host that survived one would be delivering " +
			"rung 4 by accident.")
	}
}

// TestTheGuardIsArmed proves the guard actually fires.
//
// council's own guard has no such test, and it is worth having: a wrap that
// silently stopped panicking would leave every future test free to start a
// billed turn, and nothing would say so. The binary used is this test binary,
// which certainly resolves.
func TestTheGuardIsArmed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("startSession did NOT panic on a resolvable binary. The guard in " +
				"main_test.go is what stops a test starting a live agent turn on the " +
				"operator's own account, and CI cannot catch its absence: CI has no " +
				"vendors installed, so nothing there ever dispatches.")
		}
		if !strings.Contains(toString(r), "REAL vendor process") {
			t.Fatalf("the guard panicked with something else: %v", r)
		}
	}()
	_, _ = startSession(context.Background(),
		runner.Spec{Vendor: model.VendorClaude, Binary: os.Args[0]},
		make(chan runner.Event, 1), func([]byte) (runner.Event, bool) { return runner.Event{}, false })
}

// TestTheHostGuardIsArmed is the same assertion for the host spawn.
func TestTheHostGuardIsArmed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("startHost did NOT panic on a resolvable binary. That spawn starts a " +
				"process which spawns real vendors of its own, two processes away from " +
				"the test that provoked it.")
		}
		if !strings.Contains(toString(r), "REAL council host") {
			t.Fatalf("the guard panicked with something else: %v", r)
		}
	}()
	_, _ = startHost(os.Args[0], []string{"council", "host"}, t.TempDir())
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
