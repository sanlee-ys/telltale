package councilhost

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"
)

// Outcome is how a client run ended, and it has four values because a caller
// has to print four different things.
//
// This is §4a.1 carried out of the render and into the control flow. A single
// error return would force the caller to parse a sentence to find out whether
// the room is still running, and a caller that guessed wrong would either orphan
// a host or claim one is there when it is not.
type Outcome int

const (
	// OutcomeEnded: the operator ended the room. Every seat was killed.
	OutcomeEnded Outcome = iota
	// OutcomeDetached: the operator left and the host is still running.
	OutcomeDetached
	// OutcomeHostExited: the pipe broke. The host is gone and so are the seats.
	OutcomeHostExited
	// OutcomeInputClosed: stdin reached EOF with no command.
	//
	// It ENDS the room, and that is the same ruling KindShutdown's doc carries:
	// a client whose input went away is not a client that asked to leave. A
	// piped or redirected run must not become an accidental detach.
	OutcomeInputClosed
)

// RunClient drives one hosted room from a line-oriented terminal.
//
// # Why this is not the council TUI, stated where a reader will look
//
// design.md §7.28 named the plain renderer "a legible proof that the wire
// carries a whole room — not a second council TUI", and making council's own
// Model the client's renderer is still the next slice. So this draws with
// Render, and the banner says so rather than letting an operator discover that
// their room looks different.
//
// The consequence is that the controls are WORDS and not keys. A key needs the
// alternate screen, the alternate screen needs the TUI, and the TUI needs Model
// on the wire. §7.29 rules the same thing from the other side: no key was added
// to the room and no golden moved, because the TUI room has no host to leave.
//
// # The one control that matters
//
// `/detach` is the feature. It is a command rather than a flag for §9.17's rule
// — a control you need mid-session cannot live in a flag — and the banner names
// it every time the room opens, because an operator who cannot find it will
// close the terminal instead and get a shutdown they did not want.
func RunClient(c *Client, in io.Reader, out io.Writer, width int) (Outcome, error) {
	fmt.Fprint(out, clientBanner(c.HostPID()))

	// The room frames arrive on their own goroutine, and the operator's lines on
	// this one. They cannot be merged: a read on stdin blocks for as long as the
	// operator is thinking, and a room that only redrew when somebody typed
	// would not be a room.
	var (
		mu       sync.Mutex
		outcome  = OutcomeHostExited
		runErr   error
		finished = make(chan struct{})
		// answers carries the host's replies to something this client ASKED
		// for, which today is only the detach. Buffered, so the frame loop is
		// never held up by a request nobody is waiting on any more.
		answers = make(chan Frame, 4)
	)
	go func() {
		defer close(finished)
		for {
			f, err := c.NextFrame()
			if err != nil {
				mu.Lock()
				outcome, runErr = OutcomeHostExited, err
				mu.Unlock()
				return
			}
			switch f.Kind {
			case KindRoom:
				if f.Room != nil {
					fmt.Fprint(out, Render(*f.Room, width))
					fmt.Fprint(out, clientPrompt())
				}
			case KindDetached:
				mu.Lock()
				outcome, runErr = OutcomeDetached, nil
				mu.Unlock()
				sendAnswer(answers, f)
				return
			case KindRefused:
				// A refusal is printed and the loop CARRIES ON, because the host
				// carries on too. The unwatched-write refusal is the one this
				// exists for: the client is exactly where it was, and treating
				// the refusal as fatal would end the room the operator was just
				// told they could not leave.
				fmt.Fprintf(out, "\n%s\n%s", f.Reason, clientPrompt())
				sendAnswer(answers, f)
			}
		}
	}()

	settled := func() (Outcome, error) {
		mu.Lock()
		defer mu.Unlock()
		return outcome, runErr
	}

	lines := bufio.NewScanner(in)
	// The room's own line ceiling, so a pasted brief is not cut by the client
	// before the host has had a chance to answer it.
	lines.Buffer(make([]byte, 0, 64<<10), maxFrame)
	for {
		select {
		case <-finished:
			return settled()
		default:
		}
		if !lines.Scan() {
			// Input closed. The room ENDS, and Outcome's doc says why a piped
			// run must not become an accidental detach.
			_ = c.Close()
			<-finished
			return OutcomeInputClosed, lines.Err()
		}
		line := strings.TrimSpace(lines.Text())
		switch {
		case line == "":
			fmt.Fprint(out, clientPrompt())
		case line == "/detach":
			// Drained first. A stale answer left over from an earlier refusal
			// would otherwise be read as this request's, and the operator would
			// be told they had left a room they are still sitting in.
			drainAnswers(answers)
			if err := c.RequestDetach(); err != nil {
				return OutcomeHostExited, err
			}
			// leave is the detach answer, acted on: the socket closed on the
			// client's side and the leaving sentence on screen, which is the
			// one line that names the pid still running and how to end it.
			leave := func() (Outcome, error) {
				if err := c.CloseDetached(); err != nil {
					return OutcomeDetached, err
				}
				fmt.Fprintf(out, "\n%s\n", RenderDetached(c.HostPID()))
				return OutcomeDetached, nil
			}
			select {
			case f := <-answers:
				if f.Kind != KindDetached {
					// Refused. The sentence is already on screen, printed by the
					// frame loop, and this client stays exactly where it was.
					continue
				}
				return leave()
			case <-finished:
				// The frame loop posts the detach answer and then returns, and
				// returning closes finished, so on a granted detach BOTH arms
				// of this select are ready at once and Go picks either. When
				// this arm wins the answer is already sitting in the channel:
				// read it before settling, or the client leaves the room with
				// its outcome right and its leaving sentence never printed,
				// which is what a loaded `go test ./...` measured once
				// (2026-09-02) and fifteen isolated runs never did.
				select {
				case f := <-answers:
					if f.Kind == KindDetached {
						return leave()
					}
				default:
				}
				return settled()
			}
		case line == "/quit":
			_ = c.Close()
			<-finished
			return OutcomeEnded, nil
		case line == "/interrupt":
			if err := c.Interrupt(); err != nil {
				return OutcomeHostExited, err
			}
		case strings.HasPrefix(line, "/"):
			fmt.Fprintf(out, "no such command: %s\n%s", line, clientPrompt())
		default:
			if err := c.Dispatch(line); err != nil {
				return OutcomeHostExited, err
			}
		}
	}
}

// sendAnswer posts a reply without ever blocking the frame loop.
//
// A dropped answer is the right failure here. The only reader is a request that
// is still waiting, and a request nobody is waiting on any more must not be able
// to stall the redraw of the room.
func sendAnswer(ch chan Frame, f Frame) {
	select {
	case ch <- f:
	default:
	}
}

// drainAnswers empties the channel before a new request goes out.
func drainAnswers(ch chan Frame) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// clientBanner is what a hosted room says when it opens.
//
// It states what this surface is before it states what the controls are,
// because the first surprise is that it does not look like `telltale council`.
// §7.29 rules that the difference is named rather than discovered.
func clientBanner(hostPID int) string {
	return fmt.Sprintf(
		"telltale council — hosted room, held by pid %d.\n"+
			"this is the host's plain client, not the council TUI: the room lives in another\n"+
			"process and this one only draws it.\n"+
			"\n"+
			"  type a brief and press enter   dispatch it to every seat\n"+
			"  /detach                        leave, and the host keeps the seats running\n"+
			"  /interrupt                     abandon the turn in flight, keep the seats\n"+
			"  /quit                          end the room and every seat with it\n"+
			"\n%s", hostPID, clientPrompt())
}

// clientPrompt is the one line that says the client is waiting for the operator
// rather than for the host.
func clientPrompt() string { return "> " }
