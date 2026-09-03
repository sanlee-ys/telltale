//go:build !windows

package councilhost

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestOneClientDrivesAHostedRoomEndToEnd is endtoend_windows_test.go's claim,
// measured over the Unix transport (design.md §7.30).
//
// Everything on the path is REAL except the vendor: a real Unix domain socket
// in an owner-only directory, the real lock file, the real peer check in both
// directions, the real handshake, the real dispatch, the real fold, the real
// coalescing tick and the real render. What is stubbed is the ONE thing a test
// must never do — start a vendor CLI (TestMain) — and the room job, for
// stubRoomJob's reason: a real one would make this test binary a session
// leader with a SIGTERM handler.
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
		t.Fatalf("the client could not reach the host's socket: %v", err)
	}
	defer conn.Close()

	// The client half of the peer check ran inside Dial. Asserting the pid it
	// found is what proves it was not skipped: the peer-credential read is the
	// anti-squatting arm, and a Dial that returned without it would look
	// identical from the outside.
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
	turned := awaitRoom(t, fr, func(r Room) bool { return r.Turn == 1 })
	if turned.Seats[0].Phase != PhaseWaiting {
		t.Fatalf("a dispatched seat with no output yet drew as %q", turned.Seats[0].Phase)
	}
	log.awaitN(t, 1)

	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: "morning"}
	streaming := awaitRoom(t, fr, func(r Room) bool { return r.Seats[0].Body == "morning" })
	if streaming.Seats[0].Phase != PhaseStreaming {
		t.Fatalf("text arrived and the seat still drew as %q", streaming.Seats[0].Phase)
	}

	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindSession, SessionID: "sess-1"}
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindDone, EndsTurn: true}
	done := awaitRoom(t, fr, func(r Room) bool { return r.Seats[0].Phase == PhaseDone })
	if done.Seats[0].SessionID != "sess-1" {
		t.Fatalf("the session id did not cross the wire: %+v", done.Seats[0])
	}

	wide := Render(done, 100)
	narrow := Render(done, 40)
	for _, want := range []string{"morning", "claude", "done"} {
		if !strings.Contains(wide, want) || !strings.Contains(narrow, want) {
			t.Fatalf("the render dropped %q:\n%s", want, wide)
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

	// A clean exit leaves NOTHING on disk for this name: no socket node, no
	// lock file. A node left behind is the stale-host shape, and it is only
	// ever the shape of a host that was hard-killed.
	for _, p := range []string{name, lockPath(name), heldPath(name)} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a cleanly ended host left %s behind (%v)", p, err)
		}
	}
}

// TestATurnIsNotPersistedAnywhere is the read/write boundary, asserted rather
// than promised — the Unix run of endtoend_windows_test.go's test of the same
// name.
//
// design.md §7.28 rules that transcript content NEVER reaches disk. The Unix
// transport adds three nodes beside host.json — a socket and two zero-byte
// lock files — and all are created under the redirected home here, so the
// search covers them: a socket is not a regular file and holds nothing, and
// the lock files' whole content is their length, which is asserted to be zero.
func TestATurnIsNotPersistedAnywhere(t *testing.T) {
	countSpawns(t)
	stubRoomJob(t)

	const marker = "TELLTALEsecretCONVERSATIONmarker"
	// A SHORT home, not t.TempDir(): the socket below has to bind under it,
	// and testPipeName's doc carries the path ceiling that rules t.TempDir()
	// out. Redirected for the whole test so nothing here reaches the real one.
	home, err := os.MkdirTemp("", "tth")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)
	work := t.TempDir()

	// The socket goes where a real host puts it — PipeName's own directory
	// under the redirected home — so the search below walks the directory a
	// real host writes into, and Listen's owner-only check runs against a
	// directory it created itself.
	name := PipeName("test")
	if underHome := filepath.Join(home, ".telltale", "council", "telltale-council-test.sock"); len(underHome) <= sunPathMax {
		if !strings.HasPrefix(name, home) {
			t.Fatalf("PipeName resolved %s outside the redirected home %s", name, home)
		}
	} else {
		// macOS's per-user temp root (`/var/folders/<xx>/<hash>/T/`) puts even
		// this "short" home past sun_path, which the first darwin CI run
		// measured (2026-09-02). PipeName's retreat is what a real host does
		// there, so the test follows it rather than asserting a path the
		// kernel would refuse, and it removes the nodes it leaves in the
		// shared retreat directory; host.json is the host's own to remove.
		if !strings.HasPrefix(name, shortSocketDir()) {
			t.Fatalf("PipeName resolved %s, neither under the home %s nor in the retreat %s", name, home, shortSocketDir())
		}
		t.Cleanup(func() {
			for _, p := range []string{name, lockPath(name), heldPath(name)} {
				_ = os.Remove(p)
			}
		})
	}
	h, err := New(Config{
		Workspace: work,
		PipeName:  name,
		Posture:   vendors.PostureRead,
		Roster:    []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		Tick:      5 * time.Millisecond,
		// The discovery file too, beside the socket, as a real host writes it.
		CouncilDir: filepath.Dir(name),
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := make(chan error, 1)
	go func() { served <- h.Serve(ctx) }()

	c, err := Join(JoinConfig{PipeName: name, DialTimeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("could not join the host: %v", err)
	}
	if err := c.Dispatch(marker + "-prompt"); err != nil {
		t.Fatal(err)
	}
	// The turn is opened on the host's dispatch goroutine, and opening it
	// clears the seat's body for the new turn (Seat.startTurn, §7.31). A real
	// seat cannot speak before the dispatch that started it, so the event is
	// fed only once the turn is open — the same order a process would give.
	deadline := time.Now().Add(5 * time.Second)
	for h.Snapshot().Turn != 1 {
		if time.Now().After(deadline) {
			t.Fatal("the host never opened the turn the client dispatched")
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindText, Text: marker + "-reply"}
	deadline = time.Now().Add(5 * time.Second)
	for !strings.Contains(h.Snapshot().Seats[0].Body, marker) {
		if time.Now().After(deadline) {
			t.Fatal("the fold never saw the marker, so the search below would prove nothing")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := ReadHostFile(filepath.Dir(name)); err != nil {
		t.Fatalf("the host wrote no discovery file, so the search below would miss the one file "+
			"this design adds: %v", err)
	}
	for _, p := range []string{lockPath(name), heldPath(name)} {
		if info, err := os.Stat(p); err != nil {
			t.Fatalf("no lock file at %s: %v", p, err)
		} else if info.Size() != 0 {
			t.Fatalf("%s holds %d bytes; a lock file carries locks and nothing else", p, info.Size())
		}
	}

	for _, root := range []string{home, work} {
		assertGrepTreeWorks(t, root, marker+"-canary")
		if found := grepTree(t, root, marker); found != "" {
			t.Fatalf("transcript content reached disk at %s. §7.28 rules that the room's "+
				"conversation lives in host memory and dies with the host.", found)
		}
	}

	_ = c.Close()
	select {
	case <-served:
	case <-time.After(10 * time.Second):
		t.Fatal("the host did not return after the client closed")
	}
}

// TestAHostAcrossAProcessBoundaryTakesAClient drives a host in ANOTHER process,
// the way the Windows test of the same name does.
//
// The roster is EMPTY, which is what makes it safe: a host with no seats cannot
// spawn a vendor by any path. It is started with Setsid, as a client starts a
// real host, so the real NewRoomJob runs — a session leader with the SIGTERM
// handler installed — and the property under test is the whole production
// stack minus the vendors.
func TestAHostAcrossAProcessBoundaryTakesAClient(t *testing.T) {
	name := testPipeName(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		helperEnv+"="+helperServe,
		helperPipeEnv+"="+name,
		helperWorkEnv+"="+t.TempDir(),
	)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
		t.Fatalf("the socket's server is pid %d, not the host this test started (%d)",
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
	if welcome.Kind != KindWelcome || welcome.HostPID != cmd.Process.Pid {
		t.Fatalf("the out-of-process host answered %+v", welcome)
	}
	room, err := readRoom(t, fr)
	if err != nil {
		t.Fatalf("no room crossed the process boundary: %v", err)
	}
	if room.Version != RoomVersion {
		t.Fatalf("the room that arrived carries version %d", room.Version)
	}

	// A second client is REFUSED, before it connects: the attached lock is
	// held, and Dial reads it. §7.28 rules one client at a time because
	// multi-client attach is tmux's feature and must not be acquired by
	// accident — and on a Unix socket, where connect() succeeds into a backlog,
	// it has to be refused by this code rather than by the kernel.
	if second, err := Dial(name, 500*time.Millisecond); err == nil {
		second.Close()
		t.Fatal("a second client was served. One client at a time is a ruling.")
	} else if !strings.Contains(err.Error(), "one client at a time") {
		t.Fatalf("a second client was refused for the wrong reason: %v", err)
	}

	// The host's own refusal is the second arm, for the window between the
	// lock read and the connect. A client that skips the lock and connects
	// anyway is told the same thing by the host.
	raw, err := dialRaw(name)
	if err != nil {
		t.Fatalf("could not connect a raw second client: %v", err)
	}
	defer raw.Close()
	refused, err := NewFrameReader(raw).Read()
	if err != nil {
		t.Fatalf("the host closed a second connection without saying why: %v", err)
	}
	if refused.Kind != KindRefused || refused.HolderPID != os.Getpid() {
		t.Fatalf("the host answered a second connection with %+v; a refusal naming this "+
			"process as the holder was expected", refused)
	}

	// A bare disconnect ends the room, exactly as it does on Windows.
	conn.Close()
	waited := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(15 * time.Second):
		t.Fatal("the host outlived a bare disconnect. A client that died is not a client " +
			"that left (design.md §7.29).")
	}
}

// TestTheGuardIsArmed proves the vendor-spawn guard fires on this platform's
// test binary too. A wrap that silently stopped panicking would leave every
// future test free to start a billed turn.
func TestTheGuardIsArmed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("startSession did NOT panic on a resolvable binary")
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
			t.Fatal("startHost did NOT panic on a resolvable binary")
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
