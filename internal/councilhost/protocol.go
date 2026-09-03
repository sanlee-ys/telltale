// Package councilhost splits the council room into a process that OWNS the
// vendor CLIs and a process that RENDERS them (design.md §7.28).
//
// Three rules shape everything here, and all three are safety rather than
// style. They restate §7.28 so that a reader of this package alone cannot miss
// them.
//
//   - The host PARSES. It does not merely hold the child processes. Nothing
//     else drains a vendor's stdout, so a host that stopped reading would fill
//     the operating system's pipe buffer and block the vendor mid-turn. A room
//     that silently stops working is the opposite of the property the split is
//     for.
//   - The transport is a named pipe with an EXPLICIT security descriptor,
//     never loopback TCP and never the default descriptor. §7.24 measured a
//     web page reaching a loopback listener and reading a verbatim store; this
//     pipe carries transcript content and accepts dispatch commands, so it is
//     a strictly worse surface to leave addressable. A browser has no URL
//     scheme that reaches `\\.\pipe\...` at all.
//   - Transcript content NEVER reaches disk. It lives in this process's memory
//     and dies with it. resume.go already ruled that for the same data, and the
//     process holding the data changed rather than the rule.
//
// Detach IS exposed, as of design.md §7.29, and it changed exactly one of the
// three rules above: nothing. It adds one frame and one rule beside them.
//
//   - A client leaves by SAYING so (KindDetach). A closed pipe still ends the
//     room, because a client that died is not a client that left.
//   - A room that writes to the workspace without asking will not detach. The
//     host refuses it, and the host is the process that must, because it is the
//     one that would keep running.
package councilhost

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"

	"github.com/sanlee-ys/telltale/internal/model"
)

// ProtocolVersion is the wire contract's own number.
//
// It is checked on the handshake and a mismatch is REFUSED rather than
// negotiated. A host and a client are always the same binary in this design —
// the client starts the host from its own executable path — so a mismatch means
// two different telltale builds are talking, which is a state to name rather
// than to paper over.
const ProtocolVersion = 1

// FrameKind is what one line on the wire says.
//
// Strings rather than integers, and deliberately: the framing is
// newline-delimited JSON so that a person debugging the room can read the
// stream, and an integer kind would make every frame need this file open beside
// it. §4's JSONL framing rule is the same choice for the same reason.
type FrameKind string

const (
	// KindHello is the client's opening frame. Carries ProtocolVersion.
	KindHello FrameKind = "hello"
	// KindWelcome is the host's answer. Carries the host's own pid, which is
	// what a client renders when it has to say which process owns the room.
	KindWelcome FrameKind = "welcome"
	// KindRefused is the host declining to serve this client, with a reason a
	// person can act on. A second simultaneous client gets this one, naming the
	// pid of the client that already holds the room (§7.28's one-client rule).
	KindRefused FrameKind = "refused"
	// KindDispatch is one turn, broadcast to every seat. Carries Prompt.
	KindDispatch FrameKind = "dispatch"
	// KindInterrupt asks the seats to abandon the turn in flight WITHOUT
	// killing them. It is the room's ctrl+c, and it is a frame rather than a
	// disconnect because abandoning a turn must not cost the next one a session
	// init.
	KindInterrupt FrameKind = "interrupt"
	// KindShutdown is the client saying it is finished. The host kills every
	// seat and exits.
	//
	// A BARE DISCONNECT still means this, and design.md §7.29 keeps it that way
	// on purpose. A client that died is not a client that left: a crash, a
	// taskkill on the terminal and a power-off all close the pipe exactly the
	// way a deliberate detach does, so a host that could not tell them apart
	// would keep a room running on an inference. Two facts must not reach the
	// same code path, which is §4a.1 applied to a process.
	KindShutdown FrameKind = "shutdown"
	// KindDetach is the client saying it is LEAVING and the room is to stay up.
	//
	// It is an explicit frame rather than a closed pipe for the reason
	// KindShutdown's comment gives. The host answers KindDetached when it
	// agrees and KindRefused when it does not, and a client must wait for that
	// answer before it closes anything: a client that assumed agreement would
	// walk away from a refusal it had provoked.
	KindDetach FrameKind = "detach"
	// KindDetached is the host agreeing to be left. Carries HostPID, which is
	// what the leaving client prints so the operator can name the process they
	// now own.
	//
	// The refusal case is KindRefused with a whole sentence in Reason —
	// design.md §7.29's unwatched-write ruling — and the host keeps serving
	// after it, because a refused detach leaves the client exactly where it
	// was.
	KindDetached FrameKind = "detached"
	// KindRoom is the host's whole room state, coalesced. See Host.tick for why
	// the whole state travels rather than a delta.
	KindRoom FrameKind = "room"
)

// Frame is one line on the wire.
//
// One struct for both directions rather than two, because the alternative is
// two decoders that can disagree about the framing, and the framing is the part
// that must not drift. Which fields are legal for which Kind is stated at each
// constant above and enforced at the handler, not by the type.
type Frame struct {
	Kind FrameKind `json:"kind"`

	// Protocol is set on KindHello and KindWelcome only.
	Protocol int `json:"protocol,omitempty"`
	// HostPID is set on KindWelcome. HolderPID is set on KindRefused.
	HostPID   int `json:"host_pid,omitempty"`
	HolderPID int `json:"holder_pid,omitempty"`
	// Reason is set on KindRefused, and it is a whole sentence rather than a
	// code: the only consumer is a person reading a line the client printed.
	Reason string `json:"reason,omitempty"`
	// Prompt is set on KindDispatch. It is the ONE field on this wire that
	// carries what a person typed, and it is the reason the transport's
	// descriptor is argued rather than defaulted.
	Prompt string `json:"prompt,omitempty"`
	// Seats is set on KindDispatch and KindInterrupt: which seats the turn
	// goes to, or which seats to stop (design.md §7.31). The client resolves
	// the route — @codex, -@claude, @all, the default — against its own
	// State and sends the explicit list, so the host never parses a mention.
	// Empty means every drivable seat, which is what the plain client sends
	// and what §7.28's broadcast was.
	Seats []model.VendorID `json:"seats,omitempty"`
	// Room is set on KindRoom.
	Room *Room `json:"room,omitempty"`
}

// ErrFrameTooLong is returned when one line exceeds maxFrame.
var ErrFrameTooLong = errors.New("councilhost: a frame exceeded the wire's line ceiling")

// maxFrame caps one line on the wire.
//
// Matched to runner.maxLine (8 MiB) rather than picked, because the largest
// thing that can travel here is a room whose seats hold vendor output that
// arrived through that same ceiling. A smaller number here would drop exactly
// the longest replies, which is the failure bufio.Scanner's 64K default was
// rejected for one layer down.
const maxFrame = 8 << 20

// FrameReader decodes newline-delimited frames.
//
// It uses ReadBytes with an explicit ceiling rather than bufio.Scanner for
// runner.pumpStdout's reason: Scanner reports an over-long token as an error
// and there is no way to tell that apart from a broken stream.
type FrameReader struct {
	br *bufio.Reader
}

// NewFrameReader wraps r.
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{br: bufio.NewReaderSize(r, 64<<10)}
}

// Read returns the next frame, or an error. io.EOF means the peer is gone,
// which every caller here treats as the room ending.
func (fr *FrameReader) Read() (Frame, error) {
	var acc []byte
	for {
		chunk, err := fr.br.ReadBytes('\n')
		acc = append(acc, chunk...)
		if len(acc) > maxFrame {
			return Frame{}, ErrFrameTooLong
		}
		if err != nil {
			if len(acc) == 0 {
				return Frame{}, err
			}
			// A final line with no newline is still a line, and dropping it
			// would lose the last thing a peer said before a clean close.
			return decodeFrame(acc)
		}
		if len(chunk) > 0 && chunk[len(chunk)-1] == '\n' {
			return decodeFrame(acc)
		}
	}
}

func decodeFrame(b []byte) (Frame, error) {
	var f Frame
	if err := json.Unmarshal(trimEOL(b), &f); err != nil {
		return Frame{}, err
	}
	return f, nil
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}

// FrameWriter encodes frames, one per line.
//
// Writes are serialised under a mutex because two goroutines write to a host's
// client: the coalescing tick sends room frames while the handshake and refusal
// paths send their own. An interleaved write would corrupt the framing, which
// is the one failure this format cannot recover from.
type FrameWriter struct {
	mu sync.Mutex
	w  io.Writer
}

// NewFrameWriter wraps w.
func NewFrameWriter(w io.Writer) *FrameWriter { return &FrameWriter{w: w} }

// Write encodes one frame and terminates it with a newline.
func (fw *FrameWriter) Write(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if len(b)+1 > maxFrame {
		return ErrFrameTooLong
	}
	b = append(b, '\n')
	fw.mu.Lock()
	defer fw.mu.Unlock()
	_, err = fw.w.Write(b)
	return err
}
