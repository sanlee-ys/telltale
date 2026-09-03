package councilhost

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// ErrHostExited is returned when the pipe breaks under a client.
//
// It is a DISTINCT error and not a wrapped io.EOF, because §7.28's first crash
// mitigation is that the client must render the host's death and must never
// render it the way it renders an ordinary disconnect. Two states render two
// ways is §4a.1's discipline, applied to a process instead of to a value — and
// the honest sentence is that the seats went with the host, because they did.
var ErrHostExited = errors.New(
	"councilhost: the host process exited and the seats went with it — " +
		"the room's session ids are still in ~/.telltale/council/room.json")

// ErrNoHost is returned when nothing is listening on a room's pipe.
var ErrNoHost = errors.New("councilhost: no host is running for this room")

// startHost is the client's spawn of a host, behind a var for the SAME reason
// the vendor spawns are: the test guard.
//
// This one is the sharper of the two. It starts telltale's own binary, which
// resolves on any machine that built it, and the process it starts then spawns
// REAL vendors — so an unguarded test would launch billed agent turns two
// processes away from the assertion that provoked them. Neither
// internal/council's TestMain nor CI would see it: council's guard wraps that
// package's vars, and CI has no vendors installed at all.
//
// internal/councilhost/main_test.go wraps this on the same rule the vendor
// spawns use — a binary this machine can resolve panics, naming the argv — and
// countSpawns stubs it beside them.
var startHost = func(exe string, args []string, dir string) (*os.Process, error) {
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	hideHostConsole(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd.Process, nil
}

// ClientConfig is what a client needs to open a room.
type ClientConfig struct {
	// Workspace is the directory turns are dispatched against.
	Workspace string
	// RoomKey names the room, and through PipeName it names the transport.
	RoomKey string
	// Seats is the roster, passed to the host as vendor ids. The host resolves
	// the binaries itself, so no path travels on argv.
	Seats []model.VendorID
	// Read opens a deliberation-only room.
	Read bool
	// Exe is the telltale binary to start as the host. Empty uses this
	// process's own executable, which is the ordinary case and the reason a
	// version mismatch on the handshake means something has gone wrong.
	Exe string
	// DialTimeout bounds the wait for a freshly started host to create its
	// pipe. Zero uses defaultDial.
	DialTimeout time.Duration
}

// defaultDial is how long a client waits for a host it just started.
//
// The host creates its pipe before it accepts anything, and it creates it after
// the room job — so this covers a process start and two kernel objects, not a
// vendor launch. Five seconds is far more than that needs and still short
// enough that a host which failed to start is reported rather than waited on.
const defaultDial = 5 * time.Second

// Client is one connection to a host.
type Client struct {
	conn *Conn
	fr   *FrameReader
	fw   *FrameWriter

	hostPID int
	// owned is the host process this client started, or nil when it JOINED one
	// that was already running.
	//
	// §7.28 wrote this field before anything could set it to nil, and said so.
	// Two things set it to nil now: Join, which never started a host, and
	// CloseDetached, which gives one up. Both matter for the same reason —
	// Close kills what it owns, and a client that still thought it owned a host
	// it had just detached from would kill the room it had left.
	owned *os.Process
}

// JoinConfig is what a client needs to reach a host that is ALREADY running.
//
// It carries a pipe name rather than a room key, because a rejoining client
// reads the name out of host.json rather than deriving it. That is not a detail:
// the file is the record of what the host actually created, and re-deriving the
// name from a key would silently disagree with it the day a key ever changes.
type JoinConfig struct {
	// PipeName is the transport, from a discovery file.
	PipeName string
	// DialTimeout bounds the wait. Zero uses defaultDial. A rejoin does not
	// wait on a process start, so it is generous here rather than tight.
	DialTimeout time.Duration
}

// Join connects to a host that is already running and starts NOTHING.
//
// This is design.md §7.29's rejoin, and the difference from Open is the whole
// point of the verb: Open starts a host and owns it, Join reaches one that
// outlived somebody else's client and owns nothing. §9.52 reserved `rejoin` for
// exactly this and refused to spend it on the rebuild, so that the two facts
// could stay one word apart.
//
// It performs no liveness check of its own. Probe already answered that, and
// asking twice would be two answers that can disagree; what this adds is the
// only check a connection can make and a probe cannot — Dial's server-side peer
// check, which refuses a pipe whose server is not this user.
func Join(cfg JoinConfig) (*Client, error) {
	if cfg.PipeName == "" {
		return nil, errors.New("councilhost: a rejoin needs the host's transport name")
	}
	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = defaultDial
	}
	conn, err := Dial(cfg.PipeName, timeout)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, fr: NewFrameReader(conn), fw: NewFrameWriter(conn)}
	if err := c.handshake(); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// Open STARTS a host for this room and connects to it.
//
// Start-then-connect, and still no fallback to an existing host — the fallback
// is a separate function now (Join), and keeping the two apart is deliberate.
// §7.28 refused a silent fallback because it would have delivered an unexposed
// feature by accident; §7.29 exposes the feature and the reason survives in a
// different form. A caller that says Open is saying "make me a room", and one
// that says Join is saying "take me back to the one that is there". A single
// function that guessed between them would decide, on the operator's behalf,
// whether a room they are about to type into is a fresh one or one holding
// somebody's half-finished turn.
//
// The create still fails loudly on a taken name, because
// FILE_FLAG_FIRST_PIPE_INSTANCE makes a second create fail rather than share.
func Open(cfg ClientConfig) (*Client, error) {
	exe := cfg.Exe
	if exe == "" {
		self, err := os.Executable()
		if err != nil {
			return nil, fmt.Errorf("councilhost: could not find this binary: %w", err)
		}
		exe = self
	}
	name := PipeName(cfg.RoomKey)
	args := []string{"council", "host", "--pipe", name, "--workspace", cfg.Workspace}
	if cfg.Read {
		args = append(args, "--read")
	}
	if len(cfg.Seats) > 0 {
		ids := make([]string, 0, len(cfg.Seats))
		for _, s := range cfg.Seats {
			ids = append(ids, string(s))
		}
		args = append(args, "--vendor", strings.Join(ids, ","))
	}
	proc, err := startHost(exe, args, cfg.Workspace)
	if err != nil {
		return nil, fmt.Errorf("councilhost: could not start the host: %w", err)
	}

	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = defaultDial
	}
	conn, err := Dial(name, timeout)
	if err != nil {
		// The host is killed rather than left. A host nobody connected to is
		// the stale-host failure §7.28 names, arriving on its very first
		// second, and the operator has no surface to find it on yet.
		_ = proc.Kill()
		return nil, err
	}

	c := &Client{conn: conn, fr: NewFrameReader(conn), fw: NewFrameWriter(conn), owned: proc}
	if err := c.handshake(); err != nil {
		conn.Close()
		_ = proc.Kill()
		return nil, err
	}
	return c, nil
}

func (c *Client) handshake() error {
	if err := c.fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion}); err != nil {
		return err
	}
	f, err := c.fr.Read()
	if err != nil {
		return ErrHostExited
	}
	switch f.Kind {
	case KindWelcome:
		c.hostPID = f.HostPID
		return nil
	case KindRefused:
		return fmt.Errorf("councilhost: the host refused this client: %s", f.Reason)
	default:
		return fmt.Errorf("councilhost: the host answered a hello with %q", f.Kind)
	}
}

// HostPID is the process id of the host this client is talking to.
func (c *Client) HostPID() int { return c.hostPID }

// Dispatch sends one turn to the named seats, or to every drivable seat when
// none is named (design.md §7.31). The plain client names none; the TUI
// resolves the route and names them.
func (c *Client) Dispatch(prompt string, seats ...model.VendorID) error {
	return c.send(Frame{Kind: KindDispatch, Prompt: prompt, Seats: seats})
}

// Interrupt asks the named seats — every seat, when none is named — to abandon
// the turn in flight.
func (c *Client) Interrupt(seats ...model.VendorID) error {
	return c.send(Frame{Kind: KindInterrupt, Seats: seats})
}

func (c *Client) send(f Frame) error {
	if err := c.fw.Write(f); err != nil {
		return ErrHostExited
	}
	return nil
}

// RequestDetach asks the host to keep the room and let this client go.
//
// It only SENDS. The answer — KindDetached or KindRefused — arrives on the same
// stream as the room frames, so it is read by whoever is reading them, which is
// NextFrame's caller. Waiting for it here would mean two readers on one
// connection, and the loser of that race would swallow a room frame or an
// answer at random.
//
// A client must not close anything until that answer arrives. design.md §7.29:
// a client that assumed agreement would walk away from a refusal it provoked.
func (c *Client) RequestDetach() error { return c.send(Frame{Kind: KindDetach}) }

// NextFrame blocks for the host's next frame, whatever kind it is.
//
// This is the loop's read, and Next below is the narrower one. A run loop has to
// see KindDetached and KindRefused, because those are answers to something it
// asked — and Next drops the first and turns the second into a fatal error,
// which for a refused detach would end the room the operator was just told they
// could not leave.
//
// A broken pipe comes back as ErrHostExited and never as a bare io.EOF, so that
// no caller can accidentally render a dead host the way it renders a quiet one.
func (c *Client) NextFrame() (Frame, error) {
	f, err := c.fr.Read()
	if err != nil {
		return Frame{}, ErrHostExited
	}
	return f, nil
}

// Next blocks for the host's next ROOM frame, dropping everything else.
//
// Kept for a caller that only draws. It is the wrong read for a client that can
// detach — see NextFrame.
func (c *Client) Next() (Room, error) {
	for {
		f, err := c.fr.Read()
		if err != nil {
			return Room{}, ErrHostExited
		}
		if f.Kind == KindRoom && f.Room != nil {
			return *f.Room, nil
		}
		if f.Kind == KindRefused {
			return Room{}, fmt.Errorf("councilhost: the host refused: %s", f.Reason)
		}
	}
}

// CloseDetached closes the pipe and GIVES UP the host, leaving it running.
//
// It is the whole client half of a detach, and every line of it is the opposite
// of Close:
//
//   - No shutdown frame. Close sends one; sending one here would end the room
//     this call exists to preserve.
//   - No Wait and no Kill on the owned process. Close does both, and doing
//     either here would end it a second way.
//   - owned is cleared FIRST, so that a later Close — a deferred one, say —
//     cannot reach a host this client no longer owns. That ordering is the
//     defect this method is most likely to grow, and it is written down rather
//     than left to a reader to notice.
//
// The host has already agreed by this point: a caller reaches this only after
// KindDetached arrived.
func (c *Client) CloseDetached() error {
	c.owned = nil
	return c.conn.Close()
}

// Close ends the room.
//
// It sends a shutdown, closes the pipe, and then WAITS for the host it started.
// The wait is not tidiness: without it this process can exit while the host is
// still tearing seats down, and the operator is left with agent processes that
// nothing on screen accounts for. That is the exact failure the per-seat job
// object was built to prevent, arriving one process higher up.
func (c *Client) Close() error {
	_ = c.fw.Write(Frame{Kind: KindShutdown})
	err := c.conn.Close()
	if c.owned != nil {
		done := make(chan struct{})
		go func() { _, _ = c.owned.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			// A host that will not leave is killed. Its room job carries
			// kill-on-job-close, so its death reaps every seat with it.
			_ = c.owned.Kill()
		}
	}
	return err
}
