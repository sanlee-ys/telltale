package doctor

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// This file renders the preflight as plain text on stdout, and the choice of
// "plain text" is the design rather than a shortcut.
//
// A preflight is read once, before anything is running, and it is read in the
// two places a TUI cannot go: piped into a file, and pasted into an issue by
// someone asking why a seat is empty. So there is no alternate screen, no
// Bubble Tea, no lipgloss and no colour — which means every distinction this
// report makes is carried by a WORD, satisfying CLAUDE.md's "colour, and any
// single glyph, is always a second signal" by having no first signal that is
// not a word. NO_COLOR and --ascii have nothing to switch off here, which is
// why neither is a flag on this mode: a flag that does nothing is a promise
// that something was configurable.
//
// Render is PURE over its Report — no time.Now, no filesystem, no env reads —
// for the same reason council's is (CLAUDE.md, golden-test traps). Everything
// measured, including how long a probe took, is measured in Run and arrives
// here as data.

// Options is the render's only knob.
type Options struct {
	// Width is the wrap column. Zero takes defaultWidth — a report piped into a
	// file has no terminal to ask, and guessing 80 is what every other tool
	// that prints into a pipe does.
	Width int
}

const (
	defaultWidth = 80
	// nameCol and statusCol are wide enough for the longest of each ("network",
	// "not checked"), plus a space. Fixed rather than computed from the data:
	// the columns must not move when a vendor is missing, or two runs of this
	// report stop being diffable against each other.
	nameCol   = 12
	statusCol = 13
	indent    = 2
)

// Render draws the whole report.
func Render(r Report, o Options) string {
	cols := o.Width
	if cols <= 0 {
		cols = defaultWidth
	}
	textCol := indent + nameCol + statusCol
	// A pathological --width cannot be allowed to produce a negative wrap
	// column; the report degrades to one word per line rather than panicking.
	textWidth := max(cols-textCol, 20)

	var b strings.Builder
	b.WriteString("telltale doctor — what is installed here, and what was never looked at\n\n")
	for _, line := range wrap(legend, cols) {
		b.WriteString(line)
		b.WriteByte('\n')
	}

	for _, s := range r.Seats {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s — %s\n", s.Vendor, s.Label)
		for _, c := range s.Checks {
			writeCheck(&b, c, textCol, textWidth)
		}
		if s.Capability != "" {
			// Outside the check block and labelled, because it is not one. A
			// capability is a claim this repo measured once against a live run
			// and wrote down; putting it in the status column would give it a
			// fourth state and imply it had been re-measured here.
			writeSeatLine(&b, "council declares, and did not check here: "+s.Capability, cols)
		}
		if s.Survey != "" {
			// Also outside the check block, and for a neighbouring reason: this
			// line is about telltale's own survey of the vendor, not about the
			// reader's machine. Giving it a status word would make a stale
			// survey look like a failed check on a seat that works. See pin.go.
			writeSeatLine(&b, surveyLabel+s.Survey, cols)
		}
	}

	// The two standing unknowns, argued once. Printed even for an empty report:
	// they are properties of what this mode does, not of what it found.
	notes := []string{authNote, networkNote}
	if r.AnyDrifted() {
		// Conditional, unlike the two above, because it is a property of what
		// this run FOUND. `nextStep` is the precedent for a note that branches on
		// the report. See pin.go for why it is worded once here rather than
		// repeated under every drifted seat.
		notes = append(notes, driftNote)
	}
	for _, note := range append(notes, summary(r), nextStep(r)) {
		b.WriteByte('\n')
		for _, line := range wrap(note, cols) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// legend is printed every time rather than being left for the reader to infer.
// The whole point of three states is that "not checked" is not a soft pass, and
// a reader who has not been told that will read it as one.
const legend = "Three states and no fourth: `ok` is a check that ran and passed, " +
	"`FAILED` is a check that ran and did not, and `not checked` is a check that " +
	"did not run at all. Every value below was measured — a path that was stat'd, " +
	"a line a vendor printed — and nothing is inferred from anything else."

// writeSeatLine draws one indented, hanging-indented paragraph under a seat, for
// the lines that are not checks: what council declares, and how old telltale's
// survey of this vendor is. Both sit outside the three-state block, so both are
// laid out the same way — a reader can tell a check from a claim by shape alone,
// before reading a word of either.
func writeSeatLine(b *strings.Builder, text string, cols int) {
	for i, line := range wrap(text, cols-indent-2) {
		b.WriteString(strings.Repeat(" ", indent))
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func writeCheck(b *strings.Builder, c Check, textCol, textWidth int) {
	prefix := strings.Repeat(" ", indent) + pad(c.Name, nameCol) + pad(c.Status.Word(), statusCol)
	cont := strings.Repeat(" ", textCol)

	// The value comes first and is the ONLY thing on this line, so a reader
	// scanning the column reads measurements and not prose. It is written only
	// for a passing check: a value on a failed or unrun check would be output
	// nothing measured, which is the whole thing this package refuses.
	first := true
	write := func(s string) {
		if first {
			b.WriteString(prefix)
			first = false
		} else {
			b.WriteString(cont)
		}
		b.WriteString(s)
		b.WriteByte('\n')
	}
	if c.Status == Passed && c.Value != "" {
		for _, line := range wrap(c.Value, textWidth) {
			write(line)
		}
	}
	detail := c.Detail
	if c.Took > 0 {
		// Appended to the invocation rather than given a column: it is the cost
		// of the probe that produced the value above, and it means nothing
		// apart from it.
		took := fmt.Sprintf("%.2fs", c.Took.Seconds())
		if detail == "" {
			detail = "took " + took
		} else {
			detail += ", " + took
		}
	}
	for _, line := range wrap(detail, textWidth) {
		write(line)
	}
	if first {
		// A check with nothing to say still gets its row: a missing line would
		// silently drop a question from the report.
		b.WriteString(strings.TrimRight(prefix, " "))
		b.WriteByte('\n')
	}
}

// summary counts the three states, and counts SEATS separately, because they
// answer different questions: how much of the preflight ran, and how many seats
// would open.
func summary(r Report) string {
	passed, failed, notChecked := r.Tally()
	ready := 0
	for _, s := range r.Seats {
		if s.Ready() {
			ready++
		}
	}
	return fmt.Sprintf(
		"%d checks passed, %d failed, %d not checked, over %d seats — of which %d had "+
			"every check that ran come back ok. `not checked` is not a pass: auth and "+
			"network are unknown to this report, on every seat, because it probes neither.",
		passed, failed, notChecked, len(r.Seats), ready)
}

// nextStep names what to run after reading this, and it is the last paragraph
// because it is the only one that is not a measurement (added 2026-08-15,
// design.md §7.7).
//
// It earns its place on the first-run path. A stranger reaches this mode before
// anything works, and on a machine with no vendor CLI the report they get is
// five FAILED rows and a count that opens "0 checks passed" — every word of it
// true, and nothing in it saying what to do or that telltale is behaving
// correctly. That is the report stranding its reader.
//
// It stays PURE over the Report like everything else here, and it makes no
// claim the report does not already carry: it branches on the seat count the
// summary above just printed, and says nothing about auth, network, or whether
// a vendor would answer.
func nextStep(r Report) string {
	ready := 0
	for _, s := range r.Seats {
		if s.Ready() {
			ready++
		}
	}
	if ready == 0 {
		return "What runs next: no seat above passed every check that ran, which is this " +
			"report working rather than telltale failing — council drives a vendor CLI, so " +
			"install one and run this again. `telltale hud` runs either way, and it names " +
			"every vendor store it looked in, found or not."
	}
	return "What runs next: `telltale council` opens the room with the seats above, and " +
		"`telltale hud` reads what those vendors write to disk. The statusline is the one " +
		"mode you wire in rather than run — put `telltale statusline` in Claude Code's or " +
		"Antigravity CLI's `statusLine.command`; the README's Install section carries the " +
		"block to paste."
}

func pad(s string, n int) string {
	if width(s) >= n {
		// Never truncated. These are fixed vocabularies whose longest member
		// fits; a clipped state word is §9.11's ruling on what must not happen,
		// and out here there is no width pressure to justify it.
		return s + " "
	}
	return s + strings.Repeat(" ", n-width(s))
}

// width counts runes, not bytes. Not a display-width library — the repo's own
// rule is that internal/theme and this print path stay clear of the TUI stack —
// but enough that an em dash in this file's own prose cannot silently push a
// line three columns past the wrap.
func width(s string) int { return utf8.RuneCountInString(s) }

// wrap breaks text on spaces at max. Deliberately naive: it counts runes, not
// grapheme clusters, because everything it wraps is this repo's own prose or a
// vendor version string, and importing a width library here would drag the TUI
// stack into a mode that prints to a pipe. A path with no spaces in it is left
// long rather than broken — a wrapped path is a path nobody can copy.
func wrap(s string, max int) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		switch {
		case line == "":
			line = word
		case width(line)+1+width(word) <= max:
			line += " " + word
		default:
			out = append(out, line)
			line = word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}
