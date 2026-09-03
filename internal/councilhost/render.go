package councilhost

import (
	"fmt"
	"strings"
	"time"
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

	// The header is fitted like every other line. A title that overran the
	// width would wrap in the terminal and push the whole room out of
	// alignment, and it would do it on exactly the narrow terminals this render
	// exists to be correct at.
	fmt.Fprintf(&b, "%s\n\n", fit(fmt.Sprintf("telltale council — hosted room, turn %d", r.Turn), width))
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
		head := fmt.Sprintf("%s — %s", s.Vendor, s.Phase)
		// A measured exit code and no exit code are different facts, so an
		// absent one draws nothing rather than a zero (§4a.1).
		if s.ExitCode != nil {
			head += fmt.Sprintf(" (exit %d)", *s.ExitCode)
		}
		fmt.Fprintf(&b, "  %s\n", fit(head, width-2))
		if s.Note != "" {
			fmt.Fprintf(&b, "      %s\n", fit(s.Note, width-6))
		}
		for _, a := range s.Acts {
			fmt.Fprintf(&b, "      · %s\n", fit(actLine(a), width-8))
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

// The four notices below are design.md §7.29's four states, and the reason each
// one is its own function is §4a.1: `rebuilt`, `survived` and `died` must never
// render alike, and detach adds a fourth that must not look like any of them.
//
// They are functions rather than constants because three of them name a pid or
// a time that was MEASURED, and a notice that could be assembled by a caller is
// a notice two callers can assemble differently.

// RenderDetached is what the client that LEFT prints.
//
// It names the pid, because the operator now owns a process with no terminal
// and no window, and a process they cannot name is one they cannot end. Both
// ways out are on the second line for the same reason: this is the moment the
// room stops being visible, so it is the moment the two commands that reach it
// have to be on screen.
func RenderDetached(hostPID int) string {
	return fmt.Sprintf(
		"detached. the host keeps the seats and the conversation, and it is pid %d.\n"+
			"`telltale council` rejoins it. `telltale council kill` ends it, and every seat with it.",
		hostPID)
}

// RenderRejoined is what a client prints when it reached a host that never
// stopped.
//
// The second line's clause is the entire difference between this state and
// §9.52's `rebuilt`, and it is why this is not a variation on that notice.
// §9.52 rules that a room which rendered a rebuild as a continuation would tell
// the most expensive lie this surface can tell — so the sentence that IS a
// continuation has to say the opposite thing out loud, or the two collapse.
func RenderRejoined(f HostFile) string {
	return fmt.Sprintf(
		"rejoined the host that was already running — pid %d, started %s.\n"+
			"the seats kept working while you were away. nothing was rebuilt, and no session was resumed.",
		f.PID, f.StartedAt.Format(time.RFC3339))
}

// RejoinedNotice is RenderRejoined in one line, for the TUI's notice line
// (design.md §7.31).
//
// The notice line is one row and it truncates from the right, so the clause
// that separates this state from §9.52's `rebuilt` — the denial — has to sit
// where a narrow room still shows it. It is the same fact as RenderRejoined
// and never a variation on it: nothing was rebuilt, no session was resumed,
// and the process named is the one that never stopped.
func RejoinedNotice(f HostFile) string {
	return fmt.Sprintf(
		"rejoined the host that was already running (pid %d) — nothing was rebuilt, and no session was resumed",
		f.PID)
}

// actLine renders one tool call the way the trace does: the vendor's own text,
// with its outcome as a word rather than as a colour.
//
// The outcome words are separate values on purpose, and ActUnknown is the one
// that earns the type: a vendor that reports a step ENDED without saying
// whether it worked is a different fact from a vendor reporting success, and
// collapsing them is the failure §4a.1 exists to forbid.
func actLine(a Act) string {
	text := strings.TrimSpace(a.Text)
	if text == "" {
		return ""
	}
	switch a.Status {
	case ActOK:
		return text + " — ok"
	case ActFailed:
		if d := strings.TrimSpace(a.Detail); d != "" {
			return text + " — failed: " + d
		}
		return text + " — failed"
	case ActUnknown:
		return text + " — ended, outcome not reported"
	case ActDenied:
		return text + " — denied"
	default:
		return text
	}
}

// RenderHostDied is what a client prints when the host it left is gone.
//
// It is a THIRD sentence and not a variation on either of the two above, and
// §7.28's first crash mitigation is the reason: the operator cannot see a host
// die, because it has no terminal, so the client is the only thing that can say
// it happened.
//
// The last line points at §9.52's rebuild and uses §9.52's own word. A room that
// told an operator their conversation was gone, while the session ids sit on
// disk, would be making the error `council ls` refuses to make about a vendor
// that is missing from one machine.
func RenderHostDied(f HostFile, roomPath string) string {
	return fmt.Sprintf(
		"the host you left is gone, and the seats went with it.\n"+
			"it was pid %d, started %s, and nothing on screen could say when it ended.\n"+
			"the room's session ids are still in %s, so `telltale council` rebuilds those seats.",
		f.PID, f.StartedAt.Format(time.RFC3339), roomPath)
}

// RenderDetachRefused is design.md §7.29's unwatched-write ruling, on screen.
//
// The strings come from the host's own constants rather than from text typed
// here, so the sentence the operator reads and the sentence the host enforces
// cannot drift apart. A refusal whose wording lived in two places would
// eventually be two refusals.
func RenderDetachRefused() string {
	return UnwatchedWriteRefusal + "\n" + UnwatchedWriteRemedy
}

// RenderHostBusy is the refusal a client gets when a host is running and
// somebody is already in it.
//
// It is a REFUSAL and never a fall-through to a local room. §7.29 states why:
// falling through would open a second room over the same workspace on the same
// saved session ids, which is two rooms rebuilding one conversation — worse
// than any refusal.
func RenderHostBusy(f HostFile) string {
	return fmt.Sprintf(
		"a host is running for this room, pid %d, and a client is already in it.\n"+
			"one client at a time. close the other one, or end the room with `telltale council kill`.",
		f.PID)
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
