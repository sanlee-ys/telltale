package runner

import (
	"errors"

	"github.com/sanlee-ys/telltale/internal/model"
)

// A PTY session is the second kind of child this package starts, and it is
// different from every other one in what it produces: a SCREEN, not a stream of
// parsed events (design.md §9.53).
//
// Everything else in this file exists to keep those two apart. A PTY child's
// bytes never become a runner.Event, never reach a ParseFunc, and never touch
// the turn clock. The room draws them and measures nothing from them, because a
// screen of ANSI is a picture of a program and a number read off a picture is
// inferred rather than measured — which is the one thing ADR-001 refuses.
//
// The line-oriented pump in runner.go is deliberately NOT reused. It reads to a
// newline, and a terminal screen has no newlines to read to: a spinner repaints
// with a carriage return and a full-screen guest emits none at all. A PTY needs
// a chunk reader, so it gets its own.

// PTYChunk is one read off a pseudoconsole, exactly as it came.
//
// Data is RAW. It carries cursor moves, erase sequences, mode sets and window
// manipulations, and forwarding it to a terminal would execute all of them
// against the HOST — a measured capture asked the real terminal to resize
// itself to 20 by 60 (§9.53). Nothing may render Data. It goes to an emulator
// that consumes the escapes, and the emulator's decoded cells are what a reader
// ever sees.
type PTYChunk struct {
	Vendor model.VendorID
	// Data is the raw byte run. Nil on the terminal chunk.
	Data []byte
	// Done marks the LAST chunk from this session: the child exited, or the
	// read failed. Exactly one chunk carries it, for the same reason a Session
	// emits exactly one terminal event.
	Done bool
	// Note says why the session ended, when the reason is not "the child chose
	// to". Empty on a clean exit and on a session the room killed, because a
	// process we killed did not fail (runner.go's rule for Handle, applied
	// here unchanged).
	Note string
}

// PTYSession is the slice of a live pseudoconsole the room drives.
//
// An interface for the reason seatSession is one in package council: the
// production implementation is Windows-only, and a room that could not be
// compiled or tested on any other platform would be a worse trade than one
// method set.
type PTYSession interface {
	// Resize changes the pseudoconsole's cell rectangle. The caller must resize
	// its emulator in the same operation, or the grid and the pty disagree
	// about how wide a row is.
	Resize(cols, rows int) error
	// Write sends keystrokes to the child.
	//
	// Nothing in council calls this today, and §9.53 says why: no key is bound
	// to the live pane in its first cut. It is here because the pseudoconsole's
	// input pipe exists whether or not anyone writes to it, and the alternative
	// — a session that owns a handle it does not expose — hides the seam rather
	// than removing it.
	Write(p []byte) error
	// Kill ends the child and every descendant, through the job object.
	Kill()
	// Alive reports whether the child is still running.
	Alive() bool
	// Pid is the child's process id, or zero. Reported so an operator chasing a
	// stray agent has the number, and read by nothing that renders.
	Pid() int
}

// ErrPTYUnsupported is the refusal on a platform with no pseudoconsole path.
//
// A sentence rather than a bare failure: the pane renders this text, and
// "unavailable" with no reason is the shape §4a.1 spends the whole room
// avoiding.
var ErrPTYUnsupported = errors.New(
	"a live seat is unavailable on this OS: the pseudoconsole path is Windows-only")

// minPTYBuild is the Windows build ConPTY was introduced in (Windows 10 1809).
//
// DOCUMENTATION, not a measurement. The spike behind §9.53 ran on build 26200
// and on nothing else, so every claim about repaint volume and about the close
// path rests on Windows 11. This constant exists so a machine below the floor
// gets a NAMED refusal instead of a blank pane, and so the untested range is
// visible in the code rather than only in a design section.
const minPTYBuild = 17763
