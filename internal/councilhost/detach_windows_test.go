//go:build windows

package councilhost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// runDetachHost is a REAL host in a real process, holding one stand-in seat.
//
// It is the stand-in the containment measurement needs after a detach, and it
// differs from runStandInServer in exactly one way: it owns a seat. Everything
// else on the path is the production code — NewRoomJob, Listen with the explicit
// descriptor, the peer check, the handshake, the frame loop and the detach.
//
// # Two details make the measurement mean something
//
// The seat is started with a PLAIN exec.Command and NO per-seat job. The
// per-seat job also carries JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE and its handle
// also dies with the host, so a seat wrapped in one would be reaped by the
// mechanism that already existed and the room job could hold nothing at all.
// roomjob_windows_test.go's stand-in takes the same care for the same reason.
//
// The seat is started only AFTER the pipe exists, and that ordering is the
// point rather than a convenience. Serve creates the room job FIRST and the pipe
// SECOND, so a seat started once the pipe answers is a seat created by a process
// that is already inside the containment — which is the property Serve's own
// comment says the order is load-bearing for.
//
// The roster is EMPTY, so no vendor can be spawned by any path. The seat here is
// this test binary re-executed, not an agent.
func runDetachHost() int {
	name := os.Getenv(helperPipeEnv)
	h, err := New(Config{
		Workspace: os.Getenv(helperWorkEnv),
		PipeName:  name,
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "detach host:", err)
		return 1
	}

	served := make(chan error, 1)
	go func() { served <- h.Serve(context.Background()) }()

	// ProbePipe rather than Dial. A dial would consume the host's one instance
	// and the host would read the close as its client leaving, which is the
	// exact failure discovery.go's closing note records — and here it would end
	// the room this helper exists to keep.
	deadline := time.Now().Add(20 * time.Second)
	for {
		st, perr := ProbePipe(name)
		if perr == nil && st != PipeAbsent {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "detach host: the pipe never appeared")
			return 1
		}
		time.Sleep(10 * time.Millisecond)
	}

	seat := exec.Command(os.Args[0])
	seat.Env = append(os.Environ(), helperEnv+"="+helperSeat)
	seat.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := seat.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "detach host: could not start the seat:", err)
		return 1
	}
	fmt.Printf("seat %d\nhost %d\n", seat.Process.Pid, os.Getpid())
	os.Stdout.Sync()

	if err := <-served; err != nil {
		fmt.Fprintln(os.Stderr, "detach host:", err)
		return 1
	}
	return 0
}

// runDetachClient is a client in a process of its own that LEAVES and exits.
//
// A separate process, and that is the whole claim it exists to support. The
// socket half of a detach is easy to get right and easy to test in one process;
// the PROCESS half is the one that would silently regress, and it cannot be
// asserted by a client that never exits.
func runDetachClient() int {
	conn, err := Dial(os.Getenv(helperPipeEnv), 20*time.Second)
	if err != nil {
		fmt.Fprintln(os.Stderr, "detach client:", err)
		return 1
	}
	fr, fw := NewFrameReader(conn), NewFrameWriter(conn)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		fmt.Fprintln(os.Stderr, "detach client:", err)
		return 1
	}
	if f, err := fr.Read(); err != nil || f.Kind != KindWelcome {
		fmt.Fprintln(os.Stderr, "detach client: no welcome:", err)
		return 1
	}
	if err := fw.Write(Frame{Kind: KindDetach}); err != nil {
		fmt.Fprintln(os.Stderr, "detach client:", err)
		return 1
	}
	// The answer is WAITED FOR. A client that assumed agreement would walk away
	// from a refusal it had provoked, which is the rule KindDetach's doc states
	// and the reason detach is a frame with a reply rather than a closed pipe.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		f, err := fr.Read()
		if err != nil {
			fmt.Fprintln(os.Stderr, "detach client: the host went away:", err)
			return 1
		}
		if f.Kind == KindRefused {
			fmt.Fprintln(os.Stderr, "detach client: refused:", f.Reason)
			return 2
		}
		if f.Kind == KindDetached {
			conn.Close()
			fmt.Println("detached")
			os.Stdout.Sync()
			return 0
		}
	}
	fmt.Fprintln(os.Stderr, "detach client: no answer to the detach")
	return 1
}

// TestADetachedHostOutlivesItsClientProcessAndStillReapsEverySeat is design.md
// §7.29's two central claims, measured in one run because they are one story.
//
// # Claim one: the host survives the client's PROCESS, not merely its socket
//
// §7.29 records that only the socket half is new code. The process half rests on
// spawn_windows.go's existing flags: CREATE_NO_WINDOW gives a console
// application its OWN console rather than the client's, and Windows does not
// end a child when its parent exits. That is a claim about the platform, and
// this repo does not take one off a documentation page (ADR-001), so it is run:
// a real client in a real process detaches and EXITS, and the host is then
// driven again from here.
//
// # Claim two: detach does not weaken the containment
//
// §7.28's table says a hard-killed host reaps every seat, and
// TestAHardKilledHostReapsEverySeat runs it — before detach existed. A detached
// host is the case that table was written for and never covered: the host is now
// the only process left, so if detach cost the room job anything, NOTHING would
// reap the seats. TerminateProcess is what `taskkill /F` does, and the seat here
// has no per-seat job of its own, so if it dies the room job is what killed it.
func TestADetachedHostOutlivesItsClientProcessAndStillReapsEverySeat(t *testing.T) {
	name := testPipeName(t)
	host, seatPID := startDetachHost(t, name)

	// Held across the kill. WaitForSingleObject on a handle answers "this exact
	// process exited"; an OpenProcess after the fact answers "some process with
	// that number is gone", which pid reuse can make a lie.
	seatHandle, err := windows.OpenProcess(
		windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(seatPID))
	if err != nil {
		t.Fatalf("could not open the seat process %d: %v", seatPID, err)
	}
	defer windows.CloseHandle(seatHandle)

	// A CLIENT PROCESS detaches and exits.
	client := exec.Command(os.Args[0])
	client.Env = append(os.Environ(), helperEnv+"="+helperDetachClient, helperPipeEnv+"="+name)
	client.Stderr = os.Stderr
	out, err := client.Output()
	if err != nil {
		t.Fatalf("the detaching client failed: %v", err)
	}
	if !strings.Contains(string(out), "detached") {
		t.Fatalf("the client did not report a detach: %q", out)
	}

	// The client's process is GONE by now — Output waited for it. Both the host
	// and its seat must still be here.
	if ev, werr := windows.WaitForSingleObject(seatHandle, 0); werr == nil && ev == windows.WAIT_OBJECT_0 {
		t.Fatal("the seat died when the client process exited. A detached room's whole " +
			"claim is that the seats keep working, and a seat that goes with the terminal " +
			"is the feature not existing.")
	}

	// REJOINED, from this process, over a fresh pipe instance. Nothing else
	// proves the listener re-armed: a host that stayed alive and could not be
	// reached again would be a stale process rather than a detached room.
	conn, err := Dial(name, 20*time.Second)
	if err != nil {
		t.Fatalf("the detached host could not be rejoined: %v.\n"+
			"Accept hands its instance to the Conn, so Serve must Rearm after a detach or the "+
			"room is unreachable for the rest of its life.", err)
	}
	if conn.PeerPID() != host.Pid {
		t.Fatalf("the rejoin reached pid %d and the host is %d", conn.PeerPID(), host.Pid)
	}
	fr, fw := NewFrameReader(conn), NewFrameWriter(conn)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	welcome, err := fr.Read()
	if err != nil || welcome.Kind != KindWelcome {
		t.Fatalf("the rejoined host did not welcome a second client: %+v (%v)", welcome, err)
	}
	if _, err := readRoom(t, fr); err != nil {
		t.Fatalf("the rejoined host sent no room: %v", err)
	}

	// The rejoined client LEAVES the way the first one did, and this is not
	// tidiness — an earlier version of this test closed the pipe bare here and
	// was measuring the wrong thing. A bare disconnect ENDS the room (§7.29's
	// central rule), so the host tore itself down and the TerminateProcess
	// below came back "Access is denied" on a process that was already gone.
	// It passed for a while on a race the fast path happened to win.
	//
	// So the host under the kill is a host nobody has ever ended, which is
	// exactly the state the containment claim is about.
	if err := fw.Write(Frame{Kind: KindDetach}); err != nil {
		t.Fatalf("could not ask to detach a second time: %v", err)
	}
	for {
		f, err := fr.Read()
		if err != nil {
			t.Fatalf("the host went away during the second detach: %v", err)
		}
		if f.Kind == KindDetached {
			break
		}
		if f.Kind == KindRefused {
			t.Fatalf("a read room refused the second detach: %s", f.Reason)
		}
	}
	conn.Close()

	// The host is asserted ALIVE before the kill. Without this the failure
	// above arrives as an opaque "Access is denied" from TerminateProcess,
	// which is what Windows says about a process that has already exited.
	hostHandle, err := windows.OpenProcess(
		windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(host.Pid))
	if err != nil {
		t.Fatalf("could not open the host %d: %v", host.Pid, err)
	}
	defer windows.CloseHandle(hostHandle)
	if ev, werr := windows.WaitForSingleObject(hostHandle, 0); werr == nil && ev == windows.WAIT_OBJECT_0 {
		t.Fatal("the host exited after a client DETACHED from it. Detach and shutdown are " +
			"two frames and two outcomes, and a detach that ended the room would be the " +
			"feature not existing.")
	}

	// A hard kill of the DETACHED host must still reap the seat.
	if err := windows.TerminateProcess(hostHandle, 1); err != nil {
		t.Fatalf("could not hard-kill the detached host: %v", err)
	}
	ev, err := windows.WaitForSingleObject(seatHandle, 20000)
	if err != nil {
		t.Fatalf("waiting on the seat failed: %v", err)
	}
	if ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("the seat SURVIVED a hard kill of the DETACHED host (wait returned %#x).\n"+
			"§7.28's containment table says the room job reaps every seat when the host dies, "+
			"and a detached host is the case that table was written for: it is the only process "+
			"left, so nothing else can reap them.", ev)
	}
}

// TestAProbeDoesNotConsumeTheHostsPipeInstance is discovery.go's own warning,
// turned into a gate.
//
// §7.28 left the liveness probe unbuilt and said the obvious implementation was
// wrong: a probe that DIALS consumes the host's single pipe instance, the host
// reads the close as its client leaving, and the room ends. `telltale council
// ls` calls this probe on a surface that promises to write nothing and end
// nothing, so a regression to a dialling probe would make a listing capable of
// killing the room it lists.
//
// The assertion is not that ProbePipe returns the right word. It is that the
// host is STILL THERE and still serves a client afterwards.
func TestAProbeDoesNotConsumeTheHostsPipeInstance(t *testing.T) {
	name := testPipeName(t)
	host, _ := startDetachHost(t, name)

	for i := 0; i < 5; i++ {
		st, err := ProbePipe(name)
		if err != nil {
			t.Fatalf("the probe failed: %v", err)
		}
		if st != PipeFree {
			t.Fatalf("a host with no client answered %v; PipeFree was expected", st)
		}
	}

	conn, err := Dial(name, 20*time.Second)
	if err != nil {
		t.Fatalf("the host could not be reached after five probes: %v.\n"+
			"A probe that CONNECTS consumes the one pipe instance and the host reads the "+
			"close as its client leaving — a liveness check that ends the room it is "+
			"checking (discovery.go's closing note).", err)
	}
	defer conn.Close()
	if conn.PeerPID() != host.Pid {
		t.Fatalf("reached pid %d and the host is %d", conn.PeerPID(), host.Pid)
	}

	// A host WITH a client answers differently, and both answers are used. "No
	// host" and "a host somebody else is in" are different facts and a caller
	// renders them differently (§4a.1).
	fw := NewFrameWriter(conn)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		st, err := ProbePipe(name)
		if err != nil {
			t.Fatalf("the probe failed with a client attached: %v", err)
		}
		if st == PipeBusy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a host with a client attached answered %v; PipeBusy was expected, and "+
				"collapsing it into PipeAbsent would report a live room as dead", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// startDetachHost re-executes the test binary as a real host holding one seat.
func startDetachHost(t *testing.T, pipe string) (host *os.Process, seatPID int) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		helperEnv+"="+helperDetachServe,
		helperPipeEnv+"="+pipe,
		helperWorkEnv+"="+t.TempDir(),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("could not pipe the host's stdout: %v", err)
	}
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NO_WINDOW,
		HideWindow:    true,
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the host: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	lines := make(chan string, 4)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				t.Fatal("the host said nothing and exited")
			}
			if !strings.HasPrefix(l, "seat ") {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(l, "seat "))
			if err != nil {
				t.Fatalf("the host reported an unreadable seat pid %q", l)
			}
			return cmd.Process, n
		case <-timeout:
			t.Fatal("the host never reported a seat pid")
		}
	}
}

// TestAStaleHostFileIsNeverMistakenForALiveHost is §7.28's own limit, enforced.
//
// "A pid is reusable, and a stale host.json is the normal case after a hard
// kill" — so the number in that file is a claim about a process that may not
// exist and may have been replaced. Probe must never read it as a live room, and
// KillHost must never terminate what it names.
func TestAStaleHostFileIsNeverMistakenForALiveHost(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHostFile(dir, HostFile{
		PID: 0, Pipe: PipeName("telltale-stale-fixture-never-created"),
		StartedAt: time.Now().Add(-time.Hour), Workspace: dir,
	}); err != nil {
		t.Fatal(err)
	}

	rep := Probe(dir)
	if rep.State != HostDead {
		t.Fatalf("a stale discovery file probed as %v; HostDead was expected", rep.State)
	}
	if rep.Live() {
		t.Fatal("a stale discovery file reported a live host")
	}
	if rep.Reason == "" {
		t.Error("a dead host was reported with no measured reason, so nothing on screen " +
			"could say what was actually read")
	}

	line, err := KillHost(dir)
	if err != nil {
		t.Fatalf("kill refused to act on a stale file with an error: %v", err)
	}
	if !strings.Contains(line, "no host was running") {
		t.Errorf("kill did not say the host was already gone: %q", line)
	}
	if _, err := ReadHostFile(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("kill left the stale discovery file behind. A hard-killed host cannot " +
			"remove its own file, so removing it is part of the same act.")
	}
}

// TestTheIdentityCheckRefusesARecycledPID.
//
// The image-name half of verifyHostProcess, measured against a process that is
// certainly alive and certainly not a telltale host: this test's own parent
// story inverted, using a pid that exists. Without this check a stale file
// pointing at a recycled number would be read as a live room, and `telltale
// council kill` would terminate a stranger.
func TestTheIdentityCheckRefusesARecycledPID(t *testing.T) {
	// This process IS a telltale test binary, so it passes the name check. The
	// assertion is on the other half: a start time LATER than the file claims
	// means a different process took the number.
	if err := verifyHostProcess(os.Getpid(), time.Now().Add(-365*24*time.Hour)); err == nil {
		t.Fatal("a process that started long after the discovery file was written verified " +
			"as its host. A pid is reusable, so the start time is what tells a recycled " +
			"number from the process the file describes.")
	} else if !errors.Is(err, ErrNotTelltale) {
		t.Fatalf("the refusal was reported as the wrong kind of failure: %v", err)
	}

	// A pid nothing runs on is GONE and not a stranger, and the two must stay
	// apart: one means a stale file to say so about, the other means a file that
	// must never be acted on.
	if err := verifyHostProcess(0, time.Now()); !errors.Is(err, ErrHostGone) {
		t.Fatalf("pid 0 was reported as %v; ErrHostGone was expected", err)
	}
}
