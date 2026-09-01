//go:build !windows

package councilhost

import (
	"errors"
	"io"
	"sync"
	"time"
)

// PipePrefix has no meaning off Windows and is kept only so a caller compiles
// on both platforms.
const PipePrefix = ""

// PipeName is the room's transport name. Off Windows it is a socket path
// rather than a pipe name, and nothing here builds one yet.
func PipeName(key string) string { return "telltale-council-" + key }

// ErrNotBuiltHere is why every entry point in this file refuses.
//
// The Unix transport is a domain socket in a directory at mode 0700, it is
// stdlib, and it is genuinely easier than the Windows side. It is NOT stubbed
// out here for effort. It is withheld because it carries a measured asymmetry
// that has to reach PARITY.md before anything depends on it:
// runner/proc_unix.go records, dated 2026-08-17 on macOS 26.5.2, that a
// process group NAMES a set of processes and does not BIND their lifetimes. So
// a `kill -9` on a host would LEAK every seat there, while on Windows
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE reaps them. That is the reverse of the
// usual platform direction, it will surprise someone, and a socket that works
// before that row is written would let it surprise them silently.
//
// Refusing with a sentence beats a partial implementation, because a partial
// one would make the containment claim in design.md §7.28 false on one platform
// while reading as true on both.
var ErrNotBuiltHere = errors.New(
	"councilhost: the council host is Windows-only today — the Unix socket is stdlib and easy, " +
		"and it is withheld until proc_unix.go's measured kill -9 seat leak is recorded in PARITY.md " +
		"(design.md §7.28)")

// ErrListenerClosed is returned by Accept after Close.
var ErrListenerClosed = errors.New("councilhost: the listener is closed")

// ErrPeerIsAnotherUser is returned when the peer does not run as this user.
var ErrPeerIsAnotherUser = errors.New("councilhost: the process at the other end is not this user")

// Listener is the Unix half, and it is not built. See ErrNotBuiltHere.
type Listener struct {
	name string
	mu   sync.Mutex
}

// Listen refuses off Windows.
func Listen(name string) (*Listener, error) { return nil, ErrNotBuiltHere }

// SDDL is a Windows concept and is empty here.
func (l *Listener) SDDL() string { return "" }

// Name reports the transport's name.
func (l *Listener) Name() string { return l.name }

// Accept refuses off Windows.
func (l *Listener) Accept() (*Conn, error) { return nil, ErrNotBuiltHere }

// Rearm refuses off Windows, like everything else here.
func (l *Listener) Rearm() error { return ErrNotBuiltHere }

// PipeState is what a non-connecting probe of a transport name found.
type PipeState int

const (
	// PipeAbsent: nothing is listening on the name.
	PipeAbsent PipeState = iota
	// PipeFree: a host is listening and nobody is attached.
	PipeFree
	// PipeBusy: a host is listening and a client already holds the room.
	PipeBusy
)

// ProbePipe reports PipeAbsent off Windows, and it does NOT report an error.
//
// The distinction matters to every caller. A host cannot run on this platform
// at all (ErrNotBuiltHere), so "no host is listening" is the TRUE answer here
// and not a failed measurement. Returning an error instead would make
// `telltale council ls` print a fault on a Mac for a feature that is simply not
// built there, which is the opposite of what §4a.1 asks: an absence that was
// measured is an absence, not a degraded read.
func ProbePipe(name string) (PipeState, error) { return PipeAbsent, nil }

// Close is a no-op off Windows.
func (l *Listener) Close() error { return nil }

// Conn is one connected peer.
type Conn struct {
	rw  io.ReadWriteCloser
	pid int
}

// PeerPID is the process id at the other end.
func (c *Conn) PeerPID() int { return c.pid }

func (c *Conn) Read(p []byte) (int, error)  { return c.rw.Read(p) }
func (c *Conn) Write(p []byte) (int, error) { return c.rw.Write(p) }

// Close releases the peer.
func (c *Conn) Close() error { return c.rw.Close() }

// Dial refuses off Windows.
func Dial(name string, timeout time.Duration) (*Conn, error) { return nil, ErrNotBuiltHere }
