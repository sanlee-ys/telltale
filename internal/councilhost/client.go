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
	// owned is the host process this client started, or nil when it joined one
	// somebody else started. Detach is not exposed, so today it is always set —
	// the field exists because Close's behaviour differs and pretending it does
	// not is how a host gets orphaned by accident.
	owned *os.Process
}

// Open starts a host for this room and connects to it.
//
// Start-then-connect rather than connect-then-start, and there is no fallback
// to an existing host: detach is not exposed in this change, so a pipe that
// already exists is not a room to rejoin — it is a host somebody left behind,
// and attaching to it silently would deliver the unexposed feature by accident.
// The create fails loudly instead, because FILE_FLAG_FIRST_PIPE_INSTANCE makes
// a second create on the same name fail rather than share.
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

// Dispatch sends one turn to every seat.
func (c *Client) Dispatch(prompt string) error {
	return c.send(Frame{Kind: KindDispatch, Prompt: prompt})
}

// Interrupt asks the seats to abandon the turn in flight.
func (c *Client) Interrupt() error { return c.send(Frame{Kind: KindInterrupt}) }

func (c *Client) send(f Frame) error {
	if err := c.fw.Write(f); err != nil {
		return ErrHostExited
	}
	return nil
}

// Next blocks for the host's next room frame.
//
// A broken pipe comes back as ErrHostExited and never as a bare io.EOF, so that
// no caller can accidentally render a dead host the way it renders a quiet one.
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
