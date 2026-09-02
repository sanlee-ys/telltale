//go:build !windows

package councilhost

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// The Unix transport (design.md §7.30): a Unix domain socket in the council
// directory, with a lock file beside it that answers the questions a probe must
// answer WITHOUT connecting.
//
// # Why it is a socket and not a FIFO, and why the `net` package is reached for
//
// §7.28 refused loopback TCP on §7.24's measurement — a web page reached a
// loopback listener — and its whole force was that a browser has no URL scheme
// for `\\.\pipe\...`. A Unix domain socket holds the same property: `fetch`,
// `XMLHttpRequest` and `WebSocket` cannot address a filesystem path. So the
// ruling is unchanged and the gate that enforces it moved from "this package
// does not import net" to "this package reaches net for Unix domain sockets by
// name and for nothing else" — boundary_test.go's TestTheHostBindsNoPort walks
// the package's own source and lists every selector on the `net` identifier.
//
// # What the lock files are for, and why the socket alone cannot answer a probe
//
// WaitNamedPipe answers "is an instance available" without opening anything.
// A Unix socket has no such call: the only kernel question a path answers is
// "does a node exist", and a node outlives a hard-killed host. Worse, a probe
// that CONNECTED would be accepted by the host the moment it was free, read as
// a client with no handshake, and would end the room it was listing — the
// exact failure discovery.go's closing note records for a dialling probe.
//
// So two facts are carried by flock(2) on two zero-byte files beside the
// socket, and a probe reads each by trying a SHARED lock without waiting: it
// fails at once if the host holds the exclusive one, and it opens no
// connection either way.
//
//   - `<name>.lock`, held exclusively for the listener's whole life: SOMEBODY
//     IS LISTENING. A stale socket node with nobody behind it fails this test,
//     which is what lets Listen unlink such a node safely — no live host for
//     this name can be holding the lock — and what lets a second Listen refuse
//     a name that is taken, with the sentence FILE_FLAG_FIRST_PIPE_INSTANCE
//     earns.
//   - `<name>.held`, held exclusively while a client is attached: THE ROOM IS
//     HELD. §7.28's one-client rule, made readable from outside the process.
//
// A flock is released when its open file description closes, which the
// kernel does for a process that exits by any route, SIGKILL included, so a
// dead host never reads as listening. That is the one property a pid in a file
// cannot have, and it is why the lock is the WHETHER reading and host.json is
// only the WHAT.
//
// **flock, and not fcntl record locks, and the difference was measured.** The
// first version of this file used F_SETLK byte ranges on ONE file — two facts,
// one node — and every in-process test failed: fcntl locks belong to a
// PROCESS, so a probe in the host's own process reads its own lock as free,
// and closing the probe's descriptor released the listener's locks with it.
// flock belongs to an open file DESCRIPTION, so a second open in the same
// process conflicts with the first exactly as another process would, and
// closing one description never touches another. The cost is that flock is
// whole-file, hence two files. Two zero-byte nodes that carry no content are
// a cheaper price than a probe that answers wrongly from inside the host, and
// the host itself asks the question (runDetachHost, Dial) before every client.
//
// Go opens files O_CLOEXEC, so a seat the host starts does not inherit either
// description and cannot keep a dead host's locks alive.
//
// # The directory is the boundary, as ~/.telltale/ is on Windows
//
// The socket lives in `~/.telltale/council`, the directory council already
// writes, and Listen refuses to serve from it unless its mode admits only the
// owner — a socket carrying transcript content in a directory another local
// account can traverse is the leak the Windows descriptor exists to refuse.
// The socket node itself is set to 0600, and both connect directions verify
// the peer's uid through the kernel (peer_linux.go, peer_darwin.go), so the
// program on the other end is trusted exactly as far as the filesystem trusts
// it — §7.24's ratified boundary, unchanged.

// PipeName is the room's socket path, from a room key.
//
// Under the council directory rather than under the system temp directory:
// /tmp is world-writable and a sticky-bit directory is a weaker boundary than
// one the operator owns outright. The key is a hash of the workspace for the
// reason RoomKey gives, and it is not a secret: the socket's protection is the
// directory's mode and the peer check.
//
// A host with no resolvable home directory gets a per-uid directory under the
// temp root rather than a refused room, on the same degraded-not-failed rule
// Serve applies to a discovery file it could not write.
func PipeName(key string) string {
	return filepath.Join(socketDir(), "telltale-council-"+key+".sock")
}

func socketDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), fmt.Sprintf("telltale-%d", os.Getuid()))
	}
	return filepath.Join(home, ".telltale", "council")
}

// lockPath is the listening lock beside a socket path; heldPath is the
// attached lock.
func lockPath(name string) string { return strings.TrimSuffix(name, ".sock") + ".lock" }
func heldPath(name string) string { return strings.TrimSuffix(name, ".sock") + ".held" }

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("councilhost: the listener is closed")

// ErrPeerIsAnotherUser is returned when the process on the other end of the
// socket does not run as this user.
//
// The directory mode already refuses that account, so this is the second arm
// rather than the first, exactly as it is on Windows: a mode is a claim about
// what the filesystem allows, and this is a reading of who actually arrived.
var ErrPeerIsAnotherUser = errors.New("councilhost: the process at the other end of the socket is not this user")

// lockRetry bounds how long a lock take retries on EWOULDBLOCK before it is
// read as "somebody else holds this".
//
// A probe holds a SHARED lock for the microseconds between its flock and its
// close, and an exclusive take that lands inside that window fails with the
// same errno a squatted name fails with. A few retries over a short window
// tell the two apart: a probe lets go at once, and a holder does not.
const lockRetry = 200 * time.Millisecond

// Listener is a Unix domain socket serving ONE client at a time.
//
// One at a time is §7.28's ruling and not a simplification, and a Unix socket
// does not enforce it the way a single pipe instance does: connect() succeeds
// into the backlog whether or not anybody will ever accept. So the listener
// runs its own accept loop for its whole life, hands a connection to Accept
// only when Accept is WAITING, and answers everything else with KindRefused at
// once — a second client is told "one client at a time" by the host, naming the
// pid that holds the room, instead of sitting in a backlog until the first one
// leaves and then being served a room it did not ask to wait for.
type Listener struct {
	name string
	ul   *net.UnixListener
	lock *os.File
	held *os.File

	mu     sync.Mutex
	closed bool
	// armed says Accept may take the next client. False from the moment a Conn
	// is handed out until Rearm, which is the window in which newcomers are
	// refused.
	armed bool
	// waiting says an Accept is parked on handoff right now. The accept loop
	// refuses a connection unless this is set, so a client can never be queued
	// behind an Accept that is not there to take it.
	waiting bool
	holder  int

	handoff chan *net.UnixConn
	done    chan struct{}
}

// Listen creates the socket, takes the listening lock, and starts accepting.
//
// The socket exists and the lock is held as soon as this returns, so a host
// that has reached this point is reachable — §7.28's rule that liveness is read
// off the transport and never off host.json.
func Listen(name string) (*Listener, error) {
	dir := filepath.Dir(name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("councilhost: could not create %s: %w", dir, err)
	}
	if err := ownerOnlyDir(dir); err != nil {
		return nil, err
	}

	lock, err := os.OpenFile(lockPath(name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("councilhost: could not open the lock beside %s: %w", name, err)
	}
	if err := takeLock(lock, syscall.LOCK_EX); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			// The sentence FILE_FLAG_FIRST_PIPE_INSTANCE earns on Windows, for
			// the same fact: the name is somebody else's.
			return nil, fmt.Errorf(
				"councilhost: %s already exists and is owned by another process — "+
					"either a host is already running for this room, or the name is squatted: %w",
				name, err)
		}
		return nil, fmt.Errorf("councilhost: could not take the listening lock beside %s: %w", name, err)
	}
	held, err := os.OpenFile(heldPath(name), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		lock.Close()
		return nil, fmt.Errorf("councilhost: could not open the attached lock beside %s: %w", name, err)
	}

	// Nobody holds the listening lock, so no live host is behind this node. A
	// node left by a hard-killed host is the normal case, not a fault, and it
	// is unlinked here rather than reported: the lock has already answered the
	// question the node cannot.
	if err := os.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		lock.Close()
		held.Close()
		return nil, fmt.Errorf("councilhost: a stale %s could not be removed: %w", name, err)
	}
	ul, err := net.ListenUnix("unix", &net.UnixAddr{Name: name, Net: "unix"})
	if err != nil {
		lock.Close()
		held.Close()
		if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENAMETOOLONG) {
			return nil, fmt.Errorf("councilhost: could not listen on %s — a socket path is limited "+
				"to about a hundred bytes by the kernel, and this one is %d: %w", name, len(name), err)
		}
		return nil, fmt.Errorf("councilhost: could not listen on %s: %w", name, err)
	}
	// 0600 on the node itself. Linux and macOS both check write permission on
	// the node at connect(), so this is the first arm and the peer check is the
	// second.
	if err := os.Chmod(name, 0o600); err != nil {
		ul.Close()
		lock.Close()
		held.Close()
		return nil, fmt.Errorf("councilhost: could not set the mode of %s: %w", name, err)
	}

	l := &Listener{
		name:    name,
		ul:      ul,
		lock:    lock,
		held:    held,
		armed:   true,
		handoff: make(chan *net.UnixConn),
		done:    make(chan struct{}),
	}
	go l.loop()
	return l, nil
}

// ownerOnlyDir refuses a socket directory another local account can enter.
//
// Refused rather than chmod'ed: the operator set that mode, and a host that
// silently tightened a directory shared with room.json would be changing state
// nobody asked it to. The sentence names the one-line remedy.
func ownerOnlyDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("councilhost: %s is mode %04o and a room's socket must not live in a "+
			"directory other accounts can enter — `chmod 700 %s` and try again",
			dir, info.Mode().Perm(), dir)
	}
	return nil
}

// SDDL is a Windows concept and is empty here. The boundary on this platform
// is the directory mode plus the peer check, and both are enforced in Listen.
func (l *Listener) SDDL() string { return "" }

// Name reports the socket path.
func (l *Listener) Name() string { return l.name }

// loop accepts for the listener's whole life and refuses what nobody is
// waiting for.
func (l *Listener) loop() {
	for {
		c, err := l.ul.AcceptUnix()
		if err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				return
			}
			// A transient accept failure is not the listener ending. The
			// backlog is still there; try again rather than strand a host.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		l.mu.Lock()
		waiting := l.waiting && l.armed
		holder := l.holder
		l.mu.Unlock()
		if !waiting {
			refuseConn(c, holder)
			continue
		}
		select {
		case l.handoff <- c:
		case <-l.done:
			c.Close()
			return
		default:
			// waiting was set a moment ago and Accept is no longer on the
			// channel: it took another connection in between. Refuse rather
			// than queue, for the reason the type's doc gives.
			refuseConn(c, holder)
		}
	}
}

// refuseConn tells a client the room is held and closes it.
//
// The write is best-effort and the close is bounded: a client that is not
// reading must not be able to park the accept loop, because that loop is what
// keeps the host reachable for the client that IS coming back.
func refuseConn(c *net.UnixConn, holder int) {
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	_ = NewFrameWriter(c).Write(Frame{
		Kind:      KindRefused,
		HolderPID: holder,
		Reason: fmt.Sprintf("this host is serving another client, pid %d — "+
			"one client at a time (design.md §7.28)", holder),
	})
	_ = c.Close()
}

// Rearm lets this listener take ANOTHER client, after a detach.
//
// It exists for the one caller the Windows Rearm exists for: a host whose
// client DETACHED (design.md §7.29). The attached lock is released here, so a
// probe stops reading the room as held at the same moment the host stops
// refusing newcomers — the two answers come from one state and cannot
// disagree. There is no name to release and re-take on this platform, so the
// same-user window the Windows Rearm documents does not exist here.
func (l *Listener) Rearm() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return ErrListenerClosed
	}
	if l.armed {
		return nil
	}
	if err := syscall.Flock(int(l.held.Fd()), syscall.LOCK_UN); err != nil {
		return fmt.Errorf("councilhost: could not release the attached lock beside %s: %w", l.name, err)
	}
	l.armed = true
	l.holder = 0
	return nil
}

// Accept blocks until a client connects, then verifies the client runs as this
// user before returning it.
//
// A client of another user is DISCONNECTED and the error is returned, exactly
// as on Windows: the directory mode already refuses that account, so this is
// the second arm rather than the first.
func (l *Listener) Accept() (*Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrListenerClosed
	}
	// ONE client at a time, refused explicitly. A second Accept while a Conn
	// is out would be the listener serving two, which §7.28 rules out.
	if !l.armed {
		l.mu.Unlock()
		return nil, ErrListenerClosed
	}
	l.waiting = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.waiting = false
		l.mu.Unlock()
	}()

	var c *net.UnixConn
	select {
	case c = <-l.handoff:
	case <-l.done:
		return nil, ErrListenerClosed
	}

	uid, pid, err := peerIdentity(c)
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("councilhost: could not read the client's identity: %w", err)
	}
	if uid != os.Getuid() {
		c.Close()
		return nil, fmt.Errorf("%w: process %d runs as uid %d, this process runs as uid %d",
			ErrPeerIsAnotherUser, pid, uid, os.Getuid())
	}

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		c.Close()
		return nil, ErrListenerClosed
	}
	if err := takeLock(l.held, syscall.LOCK_EX); err != nil {
		l.mu.Unlock()
		c.Close()
		return nil, fmt.Errorf("councilhost: could not take the attached lock beside %s: %w", l.name, err)
	}
	l.armed = false
	l.holder = pid
	l.mu.Unlock()
	return &Conn{c: c, name: l.name, pid: pid}, nil
}

// Close stops the listener, wakes a waiting Accept, and removes the socket and
// both lock files.
//
// Closing the lock descriptors releases both locks, so a probe reads this name
// as absent from the same instant a client can no longer reach it. The socket
// node goes with net.UnixListener's own unlink-on-close; the lock files are
// unlinked after their descriptors close, and the window in which a NEW host
// has taken the name and this removes its files is the same-user race the
// Windows Rearm documents, bounded by the same boundary.
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.mu.Unlock()

	// The nodes go BEFORE a waiting Accept is woken, and the order was
	// measured: with the wake first, Serve returned, the helper host exited,
	// and the two lock files were still on disk because this goroutine had
	// not reached the unlinks — a clean exit that read as a hard kill.
	err := l.ul.Close()
	_ = l.held.Close()
	_ = l.lock.Close()
	_ = os.Remove(heldPath(l.name))
	_ = os.Remove(lockPath(l.name))
	close(l.done)
	return err
}

// Conn is one connected client, or one connected host.
type Conn struct {
	c    *net.UnixConn
	name string
	pid  int
	once sync.Once
}

// PeerPID is the process id at the other end.
func (c *Conn) PeerPID() int { return c.pid }

func (c *Conn) Read(p []byte) (int, error)  { return c.c.Read(p) }
func (c *Conn) Write(p []byte) (int, error) { return c.c.Write(p) }

// Close releases the socket exactly once. A second close of a *net.UnixConn is
// already an error rather than a hazard, but every caller here treats Close as
// idempotent and the Windows Conn is, so this one is too.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() { err = c.c.Close() })
	return err
}

// Dial connects to a host's socket and verifies the SERVER runs as this user.
//
// It reads the two locks BEFORE connecting, and that ordering is the whole
// reason the locks exist on the client side: a connect into a held room would
// be refused by the host a moment later, but a client that never connects to
// a room somebody else holds cannot be mistaken by that host for anything at
// all. The host's refusal is the second arm, for the window between the read
// and the connect.
//
// A name nobody is listening on is RETRIED until the timeout, because the
// ordinary caller is a client that just started the host and is waiting for
// its socket to appear.
func Dial(name string, timeout time.Duration) (*Conn, error) {
	deadline := time.Now().Add(timeout)
	for {
		st, err := ProbePipe(name)
		if err != nil {
			return nil, err
		}
		switch st {
		case PipeAbsent:
			if time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("councilhost: could not open %s: nothing is listening on it", name)
		case PipeBusy:
			return nil, fmt.Errorf(
				"councilhost: %s is serving another client — one client at a time (design.md §7.28)",
				displayName(name))
		}
		c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: name, Net: "unix"})
		if err != nil {
			if time.Now().Before(deadline) {
				// The lock is held and the socket is not answering yet: Listen
				// takes the lock before it binds, so this is the gap between
				// the two, measured in microseconds.
				time.Sleep(20 * time.Millisecond)
				continue
			}
			return nil, fmt.Errorf("councilhost: could not open %s: %w", displayName(name), err)
		}
		uid, pid, err := peerIdentity(c)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("councilhost: could not read the host's identity: %w", err)
		}
		if uid != os.Getuid() {
			c.Close()
			return nil, fmt.Errorf("%w: process %d runs as uid %d, this process runs as uid %d",
				ErrPeerIsAnotherUser, pid, uid, os.Getuid())
		}
		return &Conn{c: c, name: name, pid: pid}, nil
	}
}

// displayName is the socket path as a person reads it. There is no `\.\pipe\`
// prefix to strip on this platform; the path IS the name.
func displayName(n string) string { return n }

// PipeState is what a NON-CONNECTING probe of a transport name found.
type PipeState int

const (
	// PipeAbsent: nobody holds the listening lock, so no host is listening on
	// this name — whether or not a stale socket node is on disk.
	PipeAbsent PipeState = iota
	// PipeFree: a host is listening and nobody is attached to it.
	PipeFree
	// PipeBusy: a host is listening and a client already holds the room
	// (§7.28's one-client rule).
	PipeBusy
)

// ProbePipe answers whether a host is listening, WITHOUT connecting to it.
//
// A non-blocking SHARED flock fails at once when the host holds the exclusive
// one, and succeeds — and is released on the spot — when nobody does. It takes
// no connection and reaches no accept loop, so `telltale council ls` can read
// liveness while staying the reader §7.27 promises it is. It is the Unix
// spelling of WaitNamedPipe's question, and the three answers are kept apart
// rather than collapsed into a boolean for the reason the Windows probe
// gives: "no host" and "a host somebody else is in" are different facts a
// caller renders differently (§4a.1).
//
// A missing lock file is PipeAbsent and not an error: a host that exited
// cleanly removed it, and a name that has never been used never had one. A
// lock file that cannot be opened at all is an error, because a probe that
// FAILED is not a probe that answered "absent".
func ProbePipe(name string) (PipeState, error) {
	listening, err := lockHeld(lockPath(name))
	if err != nil {
		return PipeAbsent, fmt.Errorf("councilhost: could not probe %s: %w", displayName(name), err)
	}
	if !listening {
		return PipeAbsent, nil
	}
	attached, err := lockHeld(heldPath(name))
	if err != nil {
		return PipeAbsent, fmt.Errorf("councilhost: could not probe %s: %w", displayName(name), err)
	}
	if attached {
		return PipeBusy, nil
	}
	return PipeFree, nil
}

// takeLock takes an exclusive flock without blocking, retrying through the
// window a probe's shared lock occupies. See lockRetry.
func takeLock(f *os.File, how int) error {
	deadline := time.Now().Add(lockRetry)
	for {
		err := syscall.Flock(int(f.Fd()), how|syscall.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// lockHeld reports whether some open file description holds the exclusive
// flock on path — this process's own included, which is what lets a host probe
// the name it serves.
//
// The shared lock it takes to find out is released by the close on every
// path, and it is held for the microseconds between the two calls: that
// window is the reason takeLock retries.
func lockHeld(path string) (bool, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return false, nil
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}

// removeStaleTransport unlinks the socket and lock files a dead host left.
//
// Only when nobody holds the listening lock: a live host's nodes are a live
// host's, whatever a discovery file says about them. Listen already treats a
// stale socket node this way on the next start, so this is tidiness owed by
// the command that just removed the same host's discovery file, not a second
// mechanism — and a failure here changes nothing a later Listen cannot fix.
func removeStaleTransport(name string) {
	if name == "" {
		return
	}
	if held, err := lockHeld(lockPath(name)); err != nil || held {
		return
	}
	for _, p := range []string{name, heldPath(name), lockPath(name)} {
		_ = os.Remove(p)
	}
}
