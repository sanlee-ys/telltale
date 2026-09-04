package council

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// The STRIP FORM (the density pass, 2026-09-03).
//
// A narrow column is a strip, not a shrunken column. `stripHeader` and
// `stripBadges` already said that about the top two rows: at or below
// `stripWidth` a seat's identity collapses to a two-letter tag and the badge row
// keeps only a word that fits whole. The BODY never got the same rule, and that
// is finding 3 of the density pass. It ran the wide column's builder at a width
// the wide column's content cannot survive.
//
// The measurement, read off a real room at the owner's desk geometry: with one
// seat focused the other three fall to about twenty cells. There a Windows path
// wraps to ten rows, a trace of eight calls fills the column with fragments of
// `C:\Users\sanle\...`, and §9.19's skip sentence breaks across two rows. The
// room calls that trace its flight recorder (`docs/room-identity.md`). At twenty
// cells nobody can read it.
//
// So the body changes FORM below `stripWidth`, and the form is the answer to one
// question: what is this seat, on which turn, doing or done, and how do I get
// the rest? Five things, in this order:
//
//	turn 11  46s        the turn this strip is about, and its measured clock
//	sat out 12          the turns since, when the seat took none of them
//	⚙ Read ✓            one row per tool act: the TOOL NAME, no path
//	⚙ cmd.exe ✗
//	<last sentence>     the tail of the reply, which is the seat's conclusion
//	2 then f expand     how to read the rest of it
//
// Nothing here is inferred. The turn number, the clock, the acts and their
// outcome marks, and the reply text are the same measured values the wide column
// draws (§4a.1); what changes is how much of each one a strip spends a row on.
// The path is DROPPED rather than clipped, on `stripBadges`' own rule — a
// clipped Windows path is not a shorter path, it is a string that reads as a
// different one — and the whole path stays one keystroke away at full width,
// which is what the last row says out loud.
//
// The wide column is untouched. Everything above `stripWidth` renders exactly
// as it did before this pass, with CLOCK's shortened trace paths (`shortPaths`,
// `narrowTrace`). That boundary is a ruling of the audit, not a convenience: at
// a usable width the transcript is EVIDENCE and it stays a transcript, so the
// strip form is refused above the threshold.
//
// The strip draws NO scroll cue of its own. CLOCK's cue row already owns the
// chrome row in every column, at every width, and it carries the overflow count,
// the quiet clock and the turn coordinate there (cueRow). Where the two forms
// wanted the same row the cue row wins, and the STRIP lane's separate
// history-navigation row is refused with it.

// stripBody is a seat's transcript at strip width.
//
// It replaces `columnLines`' output rather than filtering it, because the two
// have different units: the wide body's unit is the LINE of a transcript that
// runs back fifty turns, and a strip's unit is the FACT. A filter over the wide
// body would still be a transcript with most of its rows removed, and the row a
// reader wants would still be the one the filter dropped.
//
// Returns nil for a seat that is not installed, so the caller keeps
// `unavailableCard` — a seat that cannot be driven has no turn to describe, and
// the card is already short enough for a strip.
func stripBody(st State, c Column, w int, sty Styles, g Glyphs) []string {
	n, acts, body, phase, clock, ok := stripTurn(st, c)
	var out []string
	if ok {
		out = append(out, sty.Muted.Render(fit(stripTurnLine(n, phase, clock, w, st, g), w)))
	}
	if line := stripSatOut(st, c, w, g); line != "" {
		out = append(out, sty.Muted.Render(fit(line, w)))
	}

	if len(acts) > 0 {
		out = append(out, "")
		for _, a := range acts {
			out = append(out, stripActLine(a, w, sty, g))
		}
	}

	switch {
	case !ok:
		// No turn behind this seat at all. The wide column's own sentence, which
		// fits a strip whole once it is wrapped.
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, dimmable(wrap("no turn dispatched yet.", w), sty)...)
	default:
		// An all-blank tail draws NOTHING, not a blank row and a blank row. `wrap`
		// answers an empty string with one empty line, and a strip that spent two
		// of its rows on that would push the widen instruction off a short frame.
		if tail := stripTail(st, c, body, phase, w, g); anyText(tail) {
			out = append(out, "")
			out = append(out, dimmable(tail, sty)...)
		}
	}

	// The NOTE stays at strip width, and the whole of it. A note is the room
	// reporting something that did not complete normally — a cancellation, a
	// vendor's own failure sentence — and it is true of THIS seat and no other,
	// which is exactly what a strip is for. It is the one block here that is not
	// shortened, because §4a.1 does not let a failure reason be summarised.
	//
	// A LIVE SKIP is the exception, and it is not an exception to that rule: its
	// note is `not addressed in turn N`, which the room line has already stated
	// once above the grid, naming every seat (satOutFact). The wide column drops
	// it on the same test in columnLines, so both forms agree.
	if c.Note != "" && !c.Skipped {
		out = append(out, "")
		out = append(out, noteCard(c.Note, c.NoteDetail, c.NoteCalm, w, sty, g)...)
	}

	if hint := stripWidenHint(st, c, w); hint != "" {
		out = append(out, "")
		out = append(out, sty.Muted.Render(fit(hint, w)))
	}
	return out
}

// anyText reports that a block of rendered lines has at least one row with
// something on it.
func anyText(lines []string) bool {
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			return true
		}
	}
	return false
}

// stripTurn picks the ONE turn a strip is about, and takes every field from it.
//
// One turn, never a mix. The live fields describe the turn this seat is in; the
// newest record describes the last one it took. A strip that took its acts from
// one and its reply from the other would be two turns wearing one turn number,
// which is the same class of error as a clock that describes a different span
// from the word beside it (§4a.1).
//
// The live turn wins whenever it has anything at all to show. A seat that has
// been dispatched to and has not spoken yet is IN a turn — that is finding 2's
// case, and the strip says so with the phase word and the clock rather than with
// an empty rectangle.
func stripTurn(st State, c Column) (n int, acts []Act, body string, phase Phase, clock string, ok bool) {
	live := c.Prompt != "" || len(c.Acts) > 0 || c.Body != ""
	if c.TurnN > 0 && live {
		return c.TurnN, c.Acts, c.Body, c.Phase, elapsed(st, c), true
	}
	if len(c.History) > 0 {
		h := c.History[len(c.History)-1]
		// The record's OWN elapsed, never the column's. `elapsed` measures from
		// `Column.Started`, which belongs to the turn the seat is in; on a seat
		// that sat the last three turns out that clock has been running since a
		// turn this line does not name.
		cl := ""
		if h.Elapsed > 0 {
			cl = dur(vendorElapsed(h.Elapsed, h.GateWait))
		}
		return h.N, h.Acts, h.Body, h.Phase, cl, true
	}
	if c.TurnN > 0 {
		return c.TurnN, nil, "", c.Phase, elapsed(st, c), true
	}
	return 0, nil, "", c.Phase, "", false
}

// stripTurnLine is the strip's first row: which turn, how it stands, how long.
//
// It answers finding 6 at strip width. The final frame of a long room is four
// columns of tails, and a reader cannot tell which turn any column is on without
// scrolling its rule into view — so the number moves to the top of the body,
// where it is chrome that cannot scroll away.
//
// The MARK is the turn's own phase mark, not the column's. On a seat that sat
// the current turn out, the column's phase is what it is doing now (nothing) and
// this line names a turn that ended; printing the idle mark on a turn that
// finished would be the strip disagreeing with its own transcript.
//
// Shedding, longest first, the idiom `stripHeader` and `overflowMarker` already
// use: the clock goes before the mark, and the mark before the number. The turn
// number never goes — it is what this row exists to say.
func stripTurnLine(n int, phase Phase, clock string, w int, st State, g Glyphs) string {
	mark := phaseMark(phase, st, g)
	num := "turn " + strconv.Itoa(n)
	forms := []string{num + " " + mark + "  " + clock, num + " " + mark, num}
	if clock == "" {
		forms = []string{num + " " + mark, num}
	}
	for _, s := range forms {
		if lipgloss.Width(s) <= w {
			return s
		}
	}
	return truncate(num, w, g.Ellipsis)
}

// stripSatOut is the turns this seat has taken none of, in the shortest true
// form.
//
// §9.19's sentence is `not addressed in turns 10–12`, and at twenty cells it
// arrives as three rows with the phrase that carries the meaning split across
// two of them. It is also finding 1: three idle columns print it side by side,
// in the same words, about the same room-level routing decision.
//
// A strip therefore states the RANGE and drops the sentence. `sat out 10–12` is
// nine cells and it says the same thing; the sentence itself survives whole at
// every width above `stripWidth`, where the rows exist to hold it.
//
// It keeps §9.19's own two rules unchanged: singular for a run of one, and never
// extended past the oldest record this column kept.
//
// The HISTORICAL run only, and that is the LEDGER invariant applied at strip
// width. The STRIP lane extended this range to the live turn; on the graft the
// live skip is a fact about the DISPATCH and the room line states it once, above
// the grid, naming every seat (satOutFact). A strip that also drew it would be
// the duplication this pass deletes, at the width with the fewest rows to spend.
// The run above it stays here for the reason the wide column keeps it: it is a
// gap in THIS seat's own reading order and it differs from seat to seat.
func stripSatOut(st State, c Column, w int, g Glyphs) string {
	from, to, run := trailingSkip(st, c)
	if !run {
		return ""
	}
	// The glyph set's own range mark, never a literal en dash: `--ascii` spells
	// it with a hyphen, and one hardcoded character here would be the one cell of
	// the strip that did not survive the accessibility floor (\u00a79.19 uses the same
	// `g.Range` one function over).
	s := "sat out " + strconv.Itoa(from) + g.Range + strconv.Itoa(to)
	if from == to {
		s = "sat out " + strconv.Itoa(from)
	}
	if lipgloss.Width(s) > w {
		return ""
	}
	return s
}

// stripActLine is one trace entry at strip width: the mark, the TOOL, and the
// outcome. No argument at all.
//
// `recorderHead` already ruled that the tool name is what a reader scans a
// recorder strip for and gave it the weight; this drops everything the weight
// was competing with. What is dropped is an ARGUMENT — a path, a command line, a
// file — and at twenty cells an argument is never one row. Eight calls of
// `C:\Users\sanle\Desktop\telltale-rooms\scratch-seat-codex\hello.py` is the
// measured case, and it filled the whole column with fragments of one prefix.
//
// Dropped, never clipped, on `stripBadges`' rule: a clipped path reads as a
// different path, and a reader who acts on it acts on a file that does not
// exist. Nothing is lost that a keystroke does not return — the last row of the
// strip says which keystroke.
//
// The OUTCOME MARK stays at every width. It is the one thing on the row that
// says whether the call worked, and `actMark` already renders its four states
// distinctly with colour switched off.
func stripActLine(a Act, w int, sty Styles, g Glyphs) string {
	mark, style := actMark(a.Status, sty, g)
	name := stripActName(a.Text)
	head := sty.Muted.Render(g.Act + " ")
	room := w - lipgloss.Width(g.Act) - 1
	if mark != "" && room-1-lipgloss.Width(mark) > 0 {
		name = truncate(name, room-1-lipgloss.Width(mark), g.Ellipsis)
		return fit(head+sty.bold(sty.Text).Render(name)+" "+style.Render(mark), w)
	}
	return fit(head+sty.bold(sty.Text).Render(truncate(name, maxInt(1, room), g.Ellipsis)), w)
}

// stripActName is the tool out of a trace entry's text, with no argument and no
// path.
//
// Two cuts, in this order. Dispatch writes `<tool>: <argument>`, so the first
// `": "` ends the tool name and an entry with no separator is a tool name whole
// (`Glob`, `Read`) — that is `recorderHead`'s own split, and it is reused rather
// than re-derived so the two surfaces cannot disagree about where a tool name
// ends. What `recorderHead` does not do is the second cut, because a wide column
// has room for the rest: an entry whose "tool" is itself a program on disk
// arrives as `"C:\WINDOWS\system32\cmd.exe" /c 'type hello.py'`, one measured
// shape out of the real room. So the first whitespace-delimited token is taken,
// its quotes are stripped, and what survives is the base name — `cmd.exe`.
//
// The base name is the honest short form of a program: it is what the reader
// would type, and it is what every other tool entry on the strip already is.
func stripActName(text string) string {
	s := text
	if i := strings.Index(s, ": "); i >= 0 {
		s = s[:i]
	} else if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	s = strings.Trim(s, `"'`)
	if i := strings.LastIndexAny(s, `\/`); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	s = strings.Trim(s, `"'`)
	if s == "" {
		return text
	}
	return s
}

// stripTailRows is how many rows a strip spends on the reply.
//
// Three, and it is the same number and the same argument as `actDetailMaxRows`:
// one row is not enough for a sentence at twenty cells, and unbounded means the
// reply pushes the widen instruction — the row that says how to see the rest —
// off the bottom of the strip.
const stripTailRows = 3

// stripTail is the LAST SENTENCE of the seat's reply.
//
// The last sentence, not the first, and that is the whole choice. A vendor's
// reply opens with what it is about to do and closes with what it concluded, and
// a reader scanning three narrow strips beside one wide column is looking for
// the conclusions. The wide column keeps the whole reply.
//
// An empty reply falls through to `inFlightBody`, which is the room's existing
// vocabulary for a turn that has produced nothing yet, and the reason a strip on
// a dispatch frame says what it is waiting for instead of drawing twenty blank
// rows (finding 2).
//
// Bounded, with the ellipsis this product already uses, so a clipped tail can
// never be mistaken for a short one.
func stripTail(st State, c Column, body string, phase Phase, w int, g Glyphs) []string {
	if strings.TrimSpace(body) == "" {
		switch phase {
		case PhaseDone, PhaseFailed, PhaseCancelled:
			// A turn that ENDED with no reply. `inFlightBody` describes a turn in
			// flight and would say "working" about one that is over, so the
			// transcript's own sentence for this is used instead (`pastTurn`).
			// One vocabulary for one fact.
			return wrap("(no reply)", w)
		}
		return inFlightBody(phase, c.Gran, "", len(c.Acts) > 0, w)
	}
	lines := wrap(lastSentence(body), w)
	if len(lines) > stripTailRows {
		lines = lines[:stripTailRows]
		last := len(lines) - 1
		// The glyph set's ellipsis, never a literal: `actDetail` names this exact
		// trap one file over, and the ASCII spelling is `>`.
		lines[last] = truncate(lines[last]+" "+g.Ellipsis, w, g.Ellipsis)
	}
	return lines
}

// lastSentence is the final sentence of a block of vendor prose.
//
// Deliberately crude, and the crudeness is bounded by what it is used for. It
// takes the last non-empty PARAGRAPH and then the text after the last sentence
// terminator in it; a reply that ends in a code fence or a bullet has no
// terminator, and the whole of that last paragraph is returned and then clipped
// by the caller's row budget. The failure mode is a tail that is longer than one
// sentence, which is a strip showing more of the reply than it promised — never
// a strip inventing one.
func lastSentence(body string) string {
	paras := strings.Split(strings.TrimSpace(body), "\n")
	last := ""
	for i := len(paras) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(paras[i]); s != "" {
			last = s
			break
		}
	}
	if last == "" {
		return ""
	}
	// The terminator has to be followed by a space to end a sentence: `1.` in a
	// numbered list and `hello.py` in a filename both carry a full stop that ends
	// nothing.
	cut := -1
	for _, sep := range []string{". ", "! ", "? "} {
		if i := strings.LastIndex(last, sep); i > cut {
			cut = i + len(sep) - 1
		}
	}
	if cut >= 0 && cut+1 < len(last) {
		return strings.TrimSpace(last[cut+1:])
	}
	return last
}

// stripWidenHint is the strip's last row: how to read the rest of this seat.
//
// The form's own honesty clause. A strip drops an argument, clips a reply and
// coalesces a run of turns, and every one of those is defensible only because
// the whole of it is one keystroke away. A room that hides content and does not
// say how to unhide it was reported as broken twice — that is `focusHint`'s
// finding, and this is the same rule applied to a form that hides more.
//
// It names the SEAT NUMBER rather than `tab`, because the number is the key that
// reaches THIS seat while `tab` reaches the next one, and the strip's own header
// is printing that number two rows above.
//
// Mode-aware, for `scrollHint`'s reason and no other: in the composer a digit is
// a digit and `f` is the letter f, so naming either would be the room teaching a
// key that does nothing (design.md §7.8). `tab` moves focus in both modes
// (§9.10), so that is what a composing room is offered instead.
//
// Silent in a room with one seat on screen, where `f` expands a column to the
// width it already has — `scrollHint` drops the key there for the same reason.
func stripWidenHint(st State, c Column, w int) string {
	if len(st.VisibleColumns()) < 2 {
		return ""
	}
	var forms []string
	if st.Mode == ModeComposing {
		forms = []string{"tab to focus", "tab"}
	} else {
		switch {
		case st.focusedIs(c):
			// The keys are already here, so `f` expands this column by itself. A
			// key in front of it would name a press that changes nothing
			// (design.md §7.8, the same rule `scrollHint` drops `f` under). The
			// STRIP lane offered `tab then f widens` on this column, which is a
			// false instruction: it sends the reader away from the seat they are
			// expanding.
			//
			// `f expand` is the mode line's own spelling, and this row uses it
			// rather than the lane's `f widens`. One key, one word: two spellings
			// of one press read as two presses.
			forms = []string{"f expand"}
		case st.SeatNumber(c) > 0:
			// The seat's own number, because it is the key that reaches THIS seat
			// while `tab` reaches the next one, and the strip's header prints that
			// number two rows above.
			num := strconv.Itoa(st.SeatNumber(c))
			forms = []string{num + " then f expand", num + " then f", "f expand"}
		default:
			forms = []string{"tab then f expand", "f expand"}
		}
	}
	for _, s := range forms {
		if lipgloss.Width(s) <= w {
			return s
		}
	}
	return ""
}
