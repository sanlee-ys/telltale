//go:build !windows

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

	"golang.org/x/sys/unix"
)

// runDetachHost is a REAL host in a real process, holding one stand-in seat —
// detach_windows_test.go's helper, on the Unix stack.
//
// The seat is started with a PLAIN exec.Command and NO per-seat group. That is
// what makes the measurement mean something: runner/proc_unix.go's per-seat
// group is what Shutdown kills through, so a seat with one would be reaped by
// the mechanism that already existed. This seat is reachable only as a member
// of the host's SESSION, which is the containment roomjob_unix.go claims.
//
// The seat is started AFTER the socket exists, for the ordering Serve's own
// comment calls load-bearing: a seat is only ever started by a process that
// already leads the session it claims to hold.
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

	deadline := time.Now().Add(20 * time.Second)
	for {
		st, perr := ProbePipe(name)
		if perr == nil && st != PipeAbsent {
			break
		}
		if time.Now().After(deadline) {
			fmt.Fprintln(os.Stderr, "detach host: the socket never appeared")
			return 1
		}
		time.Sleep(10 * time.Millisecond)
	}

	seat := exec.Command(os.Args[0])
	seat.Env = append(os.Environ(), helperEnv+"="+helperSeat)
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

// TestADetachedHostOutlivesItsClientProcessAndKillReapsEverySeat is design.md
// §7.29's two central claims on the Unix stack, and the second claim is
// weaker than the Windows one and is named as such (§7.30).
//
// # Claim one: the host survives the client's PROCESS
//
// spawn_unix.go starts the host with Setsid, so the client's terminal is not
// the host's and the client's exit is not the host's. That is a platform claim
// and it is run: a real client in a real process detaches and EXITS, and the
// host is driven again from here.
//
// # Claim two: `telltale council kill` reaps every seat, and it is the COMMAND
// that reaps, not the host's death
//
// On Windows the same test hard-kills the host and asserts the job reaped the
// seat. Nothing on this platform binds a seat's lifetime to the host
// (runner/proc_unix.go, 2026-08-17), so the equivalent claim is about the
// command: killProcess signals the host, sweeps its session, and the seat —
// which has no per-seat group and is reachable only through the session — is
// gone afterwards. What a SIGKILL of the host alone does is measured
// separately, in roomjob_unix_test.go, and it is the opposite.
func TestADetachedHostOutlivesItsClientProcessAndKillReapsEverySeat(t *testing.T) {
	name := testPipeName(t)
	host, seatPID := startDetachHost(t, name)

	if err := unix.Kill(seatPID, 0); err != nil {
		t.Fatalf("the seat %d was not alive before anything happened: %v", seatPID, err)
	}
	if sid, err := unix.Getsid(seatPID); err != nil || sid != host.Pid {
		t.Fatalf("the seat's session is %d (%v); the host's is %d, and a seat outside the "+
			"host's session is a seat nothing here can reap", sid, err, host.Pid)
	}

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

	if err := unix.Kill(seatPID, 0); err != nil {
		t.Fatal("the seat died when the client process exited. A detached room's whole " +
			"claim is that the seats keep working.")
	}
	if err := unix.Kill(host.Pid, 0); err != nil {
		t.Fatal("the host died when the client process exited")
	}

	// REJOINED, from this process, and the projection arrives at once.
	conn, err := Dial(name, 20*time.Second)
	if err != nil {
		t.Fatalf("the detached host could not be rejoined: %v — Serve must Rearm after a detach", err)
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
	room, err := readRoom(t, fr)
	if err != nil {
		t.Fatalf("the rejoined host sent no room: %v", err)
	}
	if room.Version != RoomVersion {
		t.Fatalf("the rejoined room carries version %d", room.Version)
	}

	// Leave the way the first client did, so the host under the kill is a host
	// nobody has ever ended — detach_windows_test.go records the race an
	// earlier version measured by closing bare here.
	if err := fw.Write(Frame{Kind: KindDetach}); err != nil {
		t.Fatal(err)
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
	if err := unix.Kill(host.Pid, 0); err != nil {
		t.Fatal("the host exited after a client DETACHED from it")
	}

	// REAPED in the background before the kill. This test process is the
	// host's parent, so a dead host is a zombie until something waits on it,
	// and kill(pid, 0) answers "alive" for a zombie — which would make
	// killProcess's own wait read a host that exited on SIGTERM as one that
	// ignored it. `telltale council kill` is never the host's parent, so the
	// production path has no zombie to be misled by; only a test does.
	go func() { _, _ = host.Wait() }()

	// The command's own kill path: SIGTERM, session sweep, SIGKILL sweep.
	if err := killProcess(host.Pid); err != nil {
		t.Fatalf("killProcess failed: %v", err)
	}
	if err := unix.Kill(host.Pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("the host is still there after killProcess (%v)", err)
	}
	awaitGone(t, seatPID, 15*time.Second,
		"the seat SURVIVED `telltale council kill`. The seat has no per-seat group, so the "+
			"session sweep is the only thing that can reach it, and a kill that leaves an agent "+
			"running is the worst spelling this command can have.")

	// The host removed its own discovery surface on the way out: no socket,
	// no lock file.
	for _, p := range []string{name, lockPath(name), heldPath(name)} {
		if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("the ended host left %s behind (%v)", p, err)
		}
	}
}

// TestAProbeDoesNotConsumeTheHostsSocket is discovery.go's own warning, turned
// into a gate for the Unix transport.
//
// On a Unix socket the trap is sharper than on a pipe: a probe that CONNECTED
// would sit in the backlog and be accepted the moment the host was free, read
// as a client with no handshake, and end the room. So the assertion is not
// that ProbePipe returns the right word — it is that the host is STILL THERE
// and still serves a client afterwards, and that a host with a client answers
// PipeBusy rather than collapsing into PipeAbsent.
func TestAProbeDoesNotConsumeTheHostsSocket(t *testing.T) {
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
		t.Fatalf("the host could not be reached after five probes: %v", err)
	}
	defer conn.Close()
	if conn.PeerPID() != host.Pid {
		t.Fatalf("reached pid %d and the host is %d", conn.PeerPID(), host.Pid)
	}

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
			t.Fatalf("a host with a client attached answered %v; PipeBusy was expected", st)
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
	// As a client starts a real host. NewRoomJob would setsid itself
	// otherwise, but the property measured is the production one.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start the host: %v", err)
	}
	t.Cleanup(func() {
		// The whole session, so a seat this test failed to reap does not
		// outlive the suite. sweepSession is the production sweep.
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_, _ = sweepSession(cmd.Process.Pid, syscall.SIGKILL)
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

// awaitGone waits for a pid to stop answering kill(pid, 0).
//
// A pid, not a handle: Unix has nothing to hold across a death the way a
// Windows handle is held, so pid reuse inside the wait would read as "still
// alive" and fail loudly rather than pass silently — the safe direction.
func awaitGone(t *testing.T, pid int, within time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if err := unix.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal(msg)
}

// TestAStaleHostFileIsNeverMistakenForALiveHost is §7.28's own limit, enforced
// against a pid that really ran and really exited.
//
// A pid that is certainly dead and certainly was ours: a stand-in seat, waited
// for. Probe must report the file dead and must NOT remove it — that is
// `telltale council ls`'s contract (§7.27) — and KillHost must remove it and
// terminate nothing.
func TestAStaleHostFileIsNeverMistakenForALiveHost(t *testing.T) {
	dead := exec.Command(os.Args[0], "-test.run=TestNothingByThisName")
	dead.Env = append(os.Environ(), helperEnv+"=")
	if err := dead.Run(); err != nil {
		t.Fatalf("could not run a process to be dead: %v", err)
	}
	deadPID := dead.Process.Pid

	dir := t.TempDir()
	if err := WriteHostFile(dir, HostFile{
		PID: deadPID, Pipe: testPipeName(t),
		StartedAt: time.Now().Add(-time.Hour), Workspace: dir,
	}); err != nil {
		t.Fatal(err)
	}

	rep := Probe(dir)
	if rep.State != HostDead {
		t.Fatalf("a stale discovery file probed as %v; HostDead was expected (%s)", rep.State, rep.Reason)
	}
	if rep.Live() {
		t.Fatal("a stale discovery file reported a live host")
	}
	if rep.Reason == "" {
		t.Error("a dead host was reported with no measured reason")
	}
	if _, err := ReadHostFile(dir); err != nil {
		t.Fatalf("Probe REMOVED the stale file (%v). A reader that tidied would be a writer, "+
			"and `telltale council ls` promises to be a reader.", err)
	}

	line, err := KillHost(dir)
	if err != nil {
		t.Fatalf("kill refused to act on a stale file with an error: %v", err)
	}
	if !strings.Contains(line, "no host was running") {
		t.Errorf("kill did not say the host was already gone: %q", line)
	}
	if _, err := ReadHostFile(dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("kill left the stale discovery file behind")
	}

	// pid 0 is refused before any kill: kill(0, 0) would probe this process's
	// own group and succeed, which is the one way a "no host" file could read
	// as a live one.
	if err := verifyHostProcess(0, time.Now()); !errors.Is(err, ErrHostGone) {
		t.Fatalf("pid 0 was reported as %v; ErrHostGone was expected", err)
	}
}

// dialRaw connects to a socket with no lock read and no peer check, so a test
// can measure what the HOST does to a client that skipped both.
func dialRaw(name string) (*Conn, error) {
	c, err := dialUnixRaw(name)
	if err != nil {
		return nil, err
	}
	return &Conn{c: c, name: name}, nil
}
