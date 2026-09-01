//go:build windows

package councilhost

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PipePrefix is where every Windows named pipe lives. Stated as a constant so
// a caller can build a display name without spelling the escape twice.
const PipePrefix = `\\.\pipe\`

// PipeName builds the room's pipe name from a room key.
//
// The name is DERIVED and is not a secret. A pipe's protection is its security
// descriptor, and a name nobody can guess would be a second, weaker mechanism
// pretending to be a first one — the same reasoning §7.24 used to refuse a
// bearer token beside a file the reader already has.
func PipeName(key string) string { return PipePrefix + "telltale-council-" + key }

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("councilhost: the listener is closed")

// ErrPeerIsAnotherUser is returned when the process on the other end of the
// pipe does not run as this user.
//
// It is not a hypothetical. On the server side it is the check §7.24 wanted for
// loopback TCP and could not have, because loopback peer identity needs
// GetExtendedTcpTable and does not answer the browser case anyway. On the
// client side it is the anti-squatting arm: the classic Windows attack is a
// lower-privilege account that pre-creates a well-known pipe name so a later
// client attaches to theirs.
var ErrPeerIsAnotherUser = errors.New("councilhost: the process at the other end of the pipe is not this user")

// ownerOnlySDDL builds the descriptor every council pipe is created with.
//
//	D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;<this user's SID>)
//
// The DEFAULT descriptor must never be used, and that is measured rather than
// cited: with a NULL SecurityAttributes, CreateNamedPipe grants read access to
// the Everyone group and to the anonymous account, which for a pipe carrying
// agent transcript content is every local account able to read the operator's
// conversation. TestTheDefaultDescriptorIsTheLeakWeRefuse creates such a pipe
// and asserts S-1-1-0 is in its DACL, so the claim above rests on this
// machine's own answer and not on a documentation page (ADR-001).
//
// Three entries and no more:
//
//   - D:P protects the DACL, so nothing is inherited into it.
//   - SY is LocalSystem and BA is the Administrators group. Administrators are
//     admitted DELIBERATELY: an administrator can already open the host process
//     for debug, so a denial here would be theatre, and a descriptor that looks
//     stricter than it is reads worse than one that says what it does.
//     gatehook.go takes the same posture about 0600 on Windows.
//   - The third entry is the literal SID of the account this process runs as,
//     read from its own token. OW (CREATOR OWNER) is a placeholder an object
//     substitutes at creation, and whether a named pipe substitutes it the way
//     a file does was not measured here — the literal SID needs no such answer.
//
// The result is a pipe EXACTLY as permissive as ~/.telltale/ and no more, which
// is the boundary §7.24 ratified: a program running on this machine as a
// principal that directory's ACL admits is trusted by this listener exactly as
// far as it is trusted by the filesystem.
func ownerOnlySDDL() (string, error) {
	sid, err := currentUserSID()
	if err != nil {
		return "", err
	}
	return "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + sid.String() + ")", nil
}

// currentUserSID reads this process's own token user.
func currentUserSID() (*windows.SID, error) {
	tok, err := windows.OpenCurrentProcessToken()
	if err != nil {
		return nil, err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	// Copied out of the token buffer. The Tokenuser block is freed with the
	// token, and a SID pointer into it would dangle the moment this returns.
	return u.User.Sid.Copy()
}

// processUserSID reads the token user of another process by pid.
//
// PROCESS_QUERY_LIMITED_INFORMATION rather than PROCESS_QUERY_INFORMATION: it
// is the least this needs, and it is the access right that succeeds against a
// process of the same user without any privilege at all.
func processUserSID(pid uint32) (*windows.SID, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return nil, err
	}
	defer tok.Close()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid.Copy()
}

// Listener is a named pipe serving ONE client at a time.
//
// One instance at a time is a design ruling and not a simplification: §7.28
// refuses a second simultaneous client, because multi-client attach is tmux's
// feature, it is not free, and it must not be acquired by accident. That ruling
// is what lets this use a blocking ConnectNamedPipe on one goroutine and avoid
// overlapped I/O entirely, which is the whole reason no dependency was added
// for the transport.
type Listener struct {
	name string
	sddl string

	mu      sync.Mutex
	handle  windows.Handle
	closed  bool
	waking  bool
	hasInst bool
}

// Listen creates the pipe and its first instance.
//
// The instance exists as soon as this returns, which is what makes "the pipe
// opens" a truthful answer to "is a host running". §7.28 rules that liveness is
// read off the pipe and never off host.json, because a pid is reusable and a
// stale file is the normal case after a hard kill.
func Listen(name string) (*Listener, error) {
	sddl, err := ownerOnlySDDL()
	if err != nil {
		return nil, fmt.Errorf("councilhost: could not read this process's own user SID: %w", err)
	}
	l := &Listener{name: name, sddl: sddl}
	if err := l.newInstance(true); err != nil {
		return nil, err
	}
	return l, nil
}

// SDDL reports the descriptor string this listener applies. Exported so a test
// can assert what was requested against what the object actually carries.
func (l *Listener) SDDL() string { return l.sddl }

// Name reports the pipe's full name.
func (l *Listener) Name() string { return l.name }

// newInstance creates one pipe instance carrying the explicit descriptor.
//
// FILE_FLAG_FIRST_PIPE_INSTANCE is set on every create, not only the first. A
// name another process already holds then fails the create OUTRIGHT rather than
// adding an instance to somebody else's pipe, which is the server half of the
// anti-squatting answer. The client half is the peer check in Dial, and both
// exist because the window between one instance closing and the next opening is
// real.
func (l *Listener) newInstance(first bool) error {
	sd, err := windows.SecurityDescriptorFromString(l.sddl)
	if err != nil {
		return fmt.Errorf("councilhost: the pipe descriptor %q did not parse: %w", l.sddl, err)
	}
	sa := windows.SecurityAttributes{
		Length:             uint32(unsafe.Sizeof(windows.SecurityAttributes{})),
		SecurityDescriptor: sd,
		InheritHandle:      0,
	}
	namep, err := windows.UTF16PtrFromString(l.name)
	if err != nil {
		return err
	}
	h, err := windows.CreateNamedPipe(
		namep,
		windows.PIPE_ACCESS_DUPLEX|windows.FILE_FLAG_FIRST_PIPE_INSTANCE,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT,
		1, // one instance: §7.28's one-client rule, enforced by the OS
		64<<10,
		64<<10,
		0,
		&sa,
	)
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) && first {
			return fmt.Errorf(
				"councilhost: %s already exists and is owned by another process — "+
					"either a host is already running for this room, or the name is squatted: %w",
				l.name, err)
		}
		return fmt.Errorf("councilhost: could not create %s: %w", l.name, err)
	}
	l.mu.Lock()
	l.handle = h
	l.hasInst = true
	l.mu.Unlock()
	return nil
}

// Accept blocks until a client connects, then verifies the client runs as this
// user before returning it.
//
// A client of another user is DISCONNECTED and the error is returned. The
// descriptor already refuses that account, so this is the second arm rather
// than the first: a descriptor is a claim about what the object allows, and
// this is a reading of who actually arrived.
func (l *Listener) Accept() (*Conn, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrListenerClosed
	}
	if !l.hasInst {
		l.mu.Unlock()
		if err := l.newInstance(false); err != nil {
			return nil, err
		}
		l.mu.Lock()
	}
	h := l.handle
	l.mu.Unlock()

	err := windows.ConnectNamedPipe(h, nil)
	// ERROR_PIPE_CONNECTED means the client won the race and connected between
	// the create and this call. It is a success, not a failure, and treating it
	// as an error would drop exactly the fastest clients.
	if err != nil && !errors.Is(err, windows.ERROR_PIPE_CONNECTED) {
		l.mu.Lock()
		closed := l.closed
		l.mu.Unlock()
		if closed {
			return nil, ErrListenerClosed
		}
		return nil, err
	}

	l.mu.Lock()
	closed := l.closed
	waking := l.waking
	l.waking = false
	l.mu.Unlock()
	if closed || waking {
		l.discard()
		return nil, ErrListenerClosed
	}

	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(h, &pid); err != nil {
		l.discard()
		return nil, fmt.Errorf("councilhost: could not read the client's process id: %w", err)
	}
	if err := sameUser(pid); err != nil {
		l.discard()
		return nil, err
	}

	// Ownership of the handle moves to the Conn. The listener creates a fresh
	// instance on the next Accept rather than holding two.
	l.mu.Lock()
	l.hasInst = false
	l.handle = 0
	l.mu.Unlock()
	return newConn(h, l.name, int(pid)), nil
}

// sameUser reports whether pid runs as this process's user.
func sameUser(pid uint32) error {
	mine, err := currentUserSID()
	if err != nil {
		return err
	}
	theirs, err := processUserSID(pid)
	if err != nil {
		return fmt.Errorf("councilhost: could not read the user of process %d: %w", pid, err)
	}
	if !windows.EqualSid(mine, theirs) {
		return fmt.Errorf("%w: process %d runs as %s, this process runs as %s",
			ErrPeerIsAnotherUser, pid, theirs.String(), mine.String())
	}
	return nil
}

// discard drops the current instance without handing it to a caller.
func (l *Listener) discard() {
	l.mu.Lock()
	h := l.handle
	l.handle = 0
	l.hasInst = false
	l.mu.Unlock()
	if h != 0 {
		windows.DisconnectNamedPipe(h)
		windows.CloseHandle(h)
	}
}

// Close stops the listener and unblocks an Accept waiting in
// ConnectNamedPipe.
//
// The wake is a connection this process makes to its own pipe. There is no
// cancel for a blocking ConnectNamedPipe on a synchronous handle, and the
// alternative is overlapped I/O and an IOCP — which is the dependency this
// design declined. A self-connect is a page of checked code instead
// (decisions/001).
func (l *Listener) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	l.waking = true
	pending := l.hasInst
	l.mu.Unlock()

	if pending {
		namep, err := windows.UTF16PtrFromString(l.name)
		if err == nil {
			h, err := windows.CreateFile(namep, windows.GENERIC_READ, 0, nil,
				windows.OPEN_EXISTING, 0, 0)
			if err == nil {
				windows.CloseHandle(h)
			}
		}
	}
	l.discard()
	return nil
}

// Conn is one connected client, or one connected host.
//
// The handle is wrapped in an os.File so that Read and Write are the ordinary
// blocking calls and Close is the ordinary one. A byte-mode pipe created
// without FILE_FLAG_OVERLAPPED behaves exactly like a file handle here, which
// is the reason the byte mode was chosen over the message mode: the message
// mode buys record boundaries this protocol already gets from a newline.
type Conn struct {
	f    *os.File
	name string
	pid  int
	once sync.Once
}

func newConn(h windows.Handle, name string, pid int) *Conn {
	return &Conn{f: os.NewFile(uintptr(h), name), name: name, pid: pid}
}

// PeerPID is the process id at the other end.
func (c *Conn) PeerPID() int { return c.pid }

func (c *Conn) Read(p []byte) (int, error)  { return c.f.Read(p) }
func (c *Conn) Write(p []byte) (int, error) { return c.f.Write(p) }

// Close releases the handle exactly once. A double close on a pipe handle can
// land on a handle Windows has already reused, so the guard is not defensive
// style — it is the difference between closing this pipe and closing whatever
// took its number.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() { err = c.f.Close() })
	return err
}

// Dial connects to a host's pipe and verifies the SERVER runs as this user.
//
// The client-side check is the anti-squatting arm and it is not optional. A
// lower-privilege account that pre-created this name would be handed every
// prompt the operator types, and the descriptor cannot help: it protects the
// pipe THIS process creates, not the one this process opens.
func Dial(name string, timeout time.Duration) (*Conn, error) {
	namep, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		h, err := windows.CreateFile(namep,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0, nil, windows.OPEN_EXISTING, 0, 0)
		if err == nil {
			var pid uint32
			if err := windows.GetNamedPipeServerProcessId(h, &pid); err != nil {
				windows.CloseHandle(h)
				return nil, fmt.Errorf("councilhost: could not read the host's process id: %w", err)
			}
			if err := sameUser(pid); err != nil {
				windows.CloseHandle(h)
				return nil, err
			}
			return newConn(h, name, int(pid)), nil
		}
		// ERROR_PIPE_BUSY means every instance is taken, which for this design
		// means another client already holds the room. ERROR_FILE_NOT_FOUND
		// means no host is running, and that is the answer rather than a
		// failure to retry forever.
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		if errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, fmt.Errorf(
				"councilhost: %s is serving another client — one client at a time (design.md §7.28)",
				displayName(name))
		}
		return nil, fmt.Errorf("councilhost: could not open %s: %w", displayName(name), err)
	}
}

func displayName(n string) string { return strings.TrimPrefix(n, PipePrefix) }

// readPipeDACL reads an open pipe handle's DACL back as an SDDL string.
//
// It exists for the tests, and it is the only honest way to assert that the
// descriptor was APPLIED: passing a SecurityAttributes proves what was asked
// for, and this proves what the object carries.
func readPipeDACL(h windows.Handle) (string, error) {
	sd, err := windows.GetSecurityInfo(h, windows.SE_KERNEL_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return "", err
	}
	return sd.String(), nil
}
