package councilhost

import (
	"fmt"
	"strings"
)

// Render draws a room as plain text at width columns.
//
// PURE over Room, and that is the property the whole client half rests on. It
// reads no clock, no file and no environment, exactly as council's own Render
// does and for the same reason: a reattaching client hands its OWN width to the
// same function, so repaint at any size is free and needs no resize protocol.
// tmux's hardest problem — tell the server the new size, resize the pty,
// repaint — does not exist here, because the host holds semantic state rather
// than a screen (design.md §7.28).
//
// Words and no colour, on `doctor`'s and `history`'s argument. Every
// distinction this draws is a WORD, so there is nothing for --ascii or NO_COLOR
// to switch off. This is the client of a rung where detach is not exposed, so
// it is deliberately a legible proof that the wire carries a whole room — not a
// second council TUI. Making council's own Model the renderer is the next
// slice, and §7.28's last limitation says so.
func Render(r Room, width int) string {
	if width < 24 {
		width = 24
	}
	var b strings.Builder

	fmt.Fprintf(&b, "telltale council — hosted room, turn %d\n\n", r.Turn)
	fmt.Fprintf(&b, "  workspace   %s\n", fit(r.Workspace, width-14))
	fmt.Fprintf(&b, "  posture     %s\n", r.Posture)
	if r.Notice != "" {
		fmt.Fprintf(&b, "  notice      %s\n", fit(r.Notice, width-14))
	}
	b.WriteString("\n")

	if len(r.Seats) == 0 {
		b.WriteString("  no seats\n")
		return b.String()
	}
	for i := range r.Seats {
		s := r.Seats[i]
		fmt.Fprintf(&b, "  %s — %s", s.Vendor, s.Phase)
		// A measured exit code and no exit code are different facts, so an
		// absent one draws nothing rather than a zero (§4a.1).
		if s.ExitCode != nil {
			fmt.Fprintf(&b, " (exit %d)", *s.ExitCode)
		}
		b.WriteString("\n")
		if s.Note != "" {
			fmt.Fprintf(&b, "      %s\n", fit(s.Note, width-6))
		}
		for _, a := range s.Acts {
			fmt.Fprintf(&b, "      · %s\n", fit(a, width-8))
		}
		for _, line := range wrap(s.Body, width-6) {
			fmt.Fprintf(&b, "      %s\n", line)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// RenderHostExit is the notice a client draws when the pipe breaks.
//
// It exists as its own function so that the sentence cannot drift from
// ErrHostExited's. §7.28's first crash mitigation is that a client must render
// the host's death, and must render it DIFFERENTLY from an ordinary
// disconnect — the operator cannot see the host go, because it has no terminal,
// so the client is the only thing that can say it happened.
func RenderHostExit() string {
	return "the host process exited and the seats went with it.\n" +
		"nothing was left running. the room's session ids are still in " +
		"~/.telltale/council/room.json, so `telltale council` reattaches to the " +
		"conversations from there."
}

// fit truncates one line to n columns, marking that something was removed.
//
// Rune-wise, because a byte cut lands inside a multi-byte character. This
// renderer never emits an escape sequence, so the ANSI trap view.go documents
// does not apply here — and that is a property of THIS surface being plain
// text, not a general licence.
func fit(s string, n int) string {
	if n < 4 {
		n = 4
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// wrap breaks a body into lines of at most n columns, on word boundaries where
// it can and mid-word where a single word is longer than the line.
func wrap(s string, n int) []string {
	if s == "" {
		return nil
	}
	if n < 8 {
		n = 8
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		line := ""
		for _, word := range strings.Fields(para) {
			switch {
			case line == "":
				line = word
			case len([]rune(line))+1+len([]rune(word)) <= n:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
			for len([]rune(line)) > n {
				r := []rune(line)
				out = append(out, string(r[:n]))
				line = string(r[n:])
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
