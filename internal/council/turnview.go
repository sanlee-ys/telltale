package council

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The by-turn projection (design.md §9.22).
//
// The grid answers "what did each seat say"; this answers "what happened in
// turn 10". Both read the SAME transcript — §9.9's Column.History plus the live
// column — and neither owns a record the other cannot see, which is the
// property that makes a projection safe to add: nothing here is written down,
// nothing here is derived from anything but what a seat measured, and a turn
// with no records has no page rather than an empty one.
//
// Everything in this file draws with the functions the grid already draws with
// — labelRule, promptEcho, actLines, noteCard, the §9.14 waiting cards — for a
// reason stronger than reuse. Two builders for one transcript would eventually
// disagree, and they would disagree about which of two honest-looking documents
// is the real one.

// turnEntry is one seat's part in one turn: the filed record, or the live column
// when the turn is the one in flight.
//
// One type for both, because the page must not render a turn differently
// depending on whether it happens to be over. A reader who opens turn 10 while
// it streams and comes back to it an hour later is looking at the same document;
// two builders would make that two documents, and the difference between them
// would be invisible from either one.
//
// A seat that SAT the turn OUT has no entry at all. That is §9.15's rule for the
// yank document rather than a new one — filing a seat's older reply under this
// turn's heading would be the room inventing a conversation — and it is why the
// page and `Y` take their participants from the same call.
type turnEntry struct {
	Vendor model.VendorID
	Label  string

	Phase Phase
	Gran  Granularity

	// Prompt and Quoted are this seat's copy of the brief. They agree across
	// every seat in a turn — dispatch echoes one sanitized string to all of them
	// — and the page prints them once, which is the whole shape difference
	// between reading a turn and reading four columns.
	Prompt string
	Quoted bool

	Body       string
	Acts       []Act
	Note       string
	NoteDetail string
	NoteCalm   bool

	Elapsed     time.Duration
	CostUSD     *float64
	CostSession bool

	// GateWait is the operator's own share of this seat's turn, carried across
	// so the page states the same split the column does (§9.45). Unmeasured on
	// every turn that raised no approval card, which draws no figure at all.
	GateWait runner.Span

	// Started is the live turn's dispatch time, and zero on a filed record. The
	// clock on a running seat comes from State.Now minus this, exactly as the
	// column header's does, so Render still reads no clock of its own.
	Started time.Time

	// Live reports that this entry is the column's CURRENT turn rather than a
	// record. It decides one thing: whether the §9.14 waiting cards apply.
	// "working — the reply arrives whole." is a claim about a turn in flight,
	// and a turn in the transcript is over (pastTurn).
	Live bool

	// Settling carries Column.Settling onto this page, and it is carried for the
	// same reason every other live field is: the page renders the same words the
	// column does (columnStatus), and a seat that has answered while its process
	// winds down must not read as plainly finished on one surface and
	// `done … exiting` on the other. Only ever true on a LIVE entry — a filed
	// record's process is long gone, so nothing sets it on the history path.
	Settling bool
}

// turnEntries is who took turn n, in seating order.
//
// The live column is checked FIRST and the history second, which is not merely
// an ordering: a column's TurnN is the turn its Body, Acts and Prompt belong to,
// so while turn 10 is the live one there is no record for it to find, and once
// turn 11 is dispatched the record is what remains. Reading both would double a
// seat at the instant the turn is filed.
//
// A seat that cannot be driven is excluded outright. It has never been asked
// anything, so it is not absent from this turn — it was never in it, which is
// the distinction §4a.1 keeps between "we could not read this" and "there is
// nothing there".
func (s State) turnEntries(n int) []turnEntry {
	if n <= 0 {
		return nil
	}
	var out []turnEntry
	for _, idx := range s.VisibleColumns() {
		c := s.Columns[idx]
		if c.Avail != AvailInstalled {
			continue
		}
		if c.TurnN == n {
			e := turnEntry{
				Vendor: c.Vendor, Label: c.Label,
				Phase: c.Phase, Gran: c.Gran,
				Prompt: c.Prompt, Quoted: c.Quoted,
				Body: c.Body, Acts: c.Acts,
				Elapsed: c.Elapsed, GateWait: c.GateWait,
				CostUSD: c.CostUSD, CostSession: c.CostSession,
				Started: c.Started, Live: true,
				Settling: c.Settling,
			}
			if !c.Skipped {
				// A note on a SKIPPED column is about a later turn this seat sat
				// out, not about the turn it last took — startTurn drops it for
				// exactly this reason (Column.Skipped), and carrying it onto the
				// page would put "not addressed in turn 14" under turn 10's own
				// heading, on a turn that happened.
				e.Note, e.NoteDetail, e.NoteCalm = c.Note, c.NoteDetail, c.NoteCalm
			}
			out = append(out, e)
			continue
		}
		for _, h := range c.History {
			if h.N != n {
				continue
			}
			out = append(out, turnEntry{
				Vendor: c.Vendor, Label: c.Label,
				Phase: h.Phase, Gran: c.Gran,
				Prompt: h.Prompt, Quoted: h.Quoted,
				Body: h.Body, Acts: h.Acts,
				Note: h.Note, NoteDetail: h.NoteDetail, NoteCalm: h.NoteCalm,
				Elapsed: h.Elapsed, GateWait: h.GateWait,
				CostUSD: h.CostUSD, CostSession: h.CostSession,
			})
			break
		}
	}
	return out
}

// pageLines is the whole document for one turn, as a flat list of lines.
//
// The shape is §9.15's `Y` document drawn instead of copied, and that is the
// argument for the feature rather than a resemblance to it: the assembly rule —
// the brief once at the top, then only the seats that took THIS turn, labelled —
// already existed, was already ruled, and could only be read by pasting it
// somewhere else. What was missing was a surface.
//
// The boundaries are §9.11's three strengths, unchanged and in the same order: a
// labelled rule where the subject changes, a blank row where the speaker
// changes, a blank row where the kind of content changes. The only difference
// from a column is what the strongest boundary is ABOUT — a turn in the grid,
// a seat here — which is exactly what swapping the projection means.
func pageLines(st State, n, w int, sty Styles, g Glyphs) []string {
	if st.Page.Ledger {
		// The other face of the same turn (§9.22, amended 2026-08-17). Resolved
		// HERE rather than in the renderer, so the scroll window, the overflow
		// markers, the clamp and pageViewport's line count all point at whichever
		// document is on screen without a second copy of any of them.
		return ledgerLines(st, n, w, sty, g)
	}
	entries := st.turnEntries(n)
	if len(entries) == 0 {
		return evictedLines(n, w, sty)
	}

	// The page's own outline, and the only heading on it that owns every seat
	// below — so it takes the weight, and the seat rules under it keep theirs.
	// See strongLabelRule for why the grid's copy of this line does not.
	//
	// It is also the one line INSIDE the frame drawn at the heavy rule weight
	// (§9.26). The page is a projection where the turn is the unit, and the two
	// full-bleed rules that close the frame are the only other lines that bound a
	// whole document rather than a part of one; §9.23 gave this line the weight
	// of a root and this gives it the FORM of one. The grid's turn separator is
	// untouched for §9.23's own reason: there the turn is a child.
	out := []string{strongLabelRule("turn "+strconv.Itoa(n),
		pageMeta(st, entries), w, g.RuleHeavy, sty)}

	// The brief ONCE. In the grid it is echoed per column because each seat's
	// prompt is a fact about that seat (§9.9) — a turn can reach two seats and
	// not a third. On a page there is one turn and therefore one brief, and
	// printing it four times would be the room repeating the user's own words at
	// them in the one place they cannot be mistaken for anyone else's.
	//
	// Taken from the first participant rather than merged: dispatch echoes one
	// sanitized string to every seat it addresses, so there is one string here,
	// and a merge would be code defending against a state this room cannot be in.
	echo := promptEcho(entries[0].Prompt, entries[0].Quoted, w, sty, g)
	out = append(out, echo...)

	for _, e := range entries {
		// A blank row where the speaker changes — including before the first
		// seat, where the speaker changes from the user to a vendor. That is the
		// same row turnHead spends between a brief and the answer to it, for the
		// same reason (§9.11).
		out = append(out, "")
		out = append(out, pageSeat(st, e, w, sty, g)...)
	}
	return out
}

// evictedLines is what a page draws when the turn it is open on is gone.
//
// Reachable in a long room: the fifty-turn cap can evict the last record of the
// open page while it is on screen. It says the record is gone rather than drawing
// an empty turn, because "nobody answered" and "the room no longer remembers" are
// different facts and this product does not render them alike (§4a.1).
//
// Both faces call it. The eviction is a fact about the RECORD rather than about
// which of its two readings is open, so a second sentence for the ledger would be
// two spellings of one fact — and the one that drifted would be the one nobody
// reads until a room is fifty turns deep.
func evictedLines(n, w int, sty Styles) []string {
	return styleAll(wrap("turn "+strconv.Itoa(n)+
		" is no longer in memory — the room keeps the last "+
		strconv.Itoa(maxHistory)+" turns per seat.", w), sty.Muted)
}

// pageSeat is one seat's block on a turn page: its name and how its turn ended
// on a labelled rule, then what it DID, then what it SAID.
//
// The trace first and the reply second, separated by a blank, is columnLines'
// own order and columnLines' own reason: the vendor acted, then answered, and
// concatenating the two would let a tool name read as part of an answer (§4a.1).
// A failed or cancelled seat keeps its note card, because a turn's page shows
// what actually happened — a turn that broke is one of the two a reader is
// scrolling back for.
func pageSeat(st State, e turnEntry, w int, sty Styles, g Glyphs) []string {
	out := []string{seatRule(e.Vendor, e.Label, seatMeta(st, e, g), w, sty, g)}
	// Where this seat's CONTENT starts. The rule is a label for what follows,
	// not a block the next thing has to be separated from — the same relation
	// turnHead's separator has to the brief under it — so a seat whose whole
	// turn is a failure note draws that note directly beneath its name rather
	// than across a blank row that says a speaker changed when none did (§9.11).
	head := len(out)
	for _, a := range e.Acts {
		out = append(out, actLines(a, w, sty, g)...)
	}

	var body []string
	switch {
	case e.Body == "" && e.Live && (e.Phase == PhaseWaiting || e.Phase == PhaseStreaming):
		// The §9.14 cards, on the same terms the column draws them: the claim is
		// on the seat's own rule above (`waiting` against `streaming`) and the
		// body says only what to expect.
		body = inFlightBody(e.Phase, e.Gran, e.Body, len(e.Acts) > 0, w)
	case e.Body != "":
		body = wrap(e.Body, w)
	case len(e.Acts) == 0 && e.Note == "":
		// Asked, and said nothing. A fact rather than a gap — an empty run of
		// lines here would read as the page dropping a seat (pastTurn).
		body = []string{sty.Muted.Render(padRight("(no reply)", w, g))}
	}
	if len(e.Acts) > 0 && len(body) > 0 {
		out = append(out, "")
	}
	out = append(out, body...)

	if e.Note != "" {
		if len(out) > head {
			out = append(out, "")
		}
		out = append(out, noteCard(e.Note, e.NoteDetail, e.NoteCalm, w, sty, g)...)
	}
	return out
}

// seatRule is one seat's heading inside a page: the name at weight, a rule, and
// how that seat's turn ended.
//
// The LIGHT rule, and that is the half of §9.26 this line exists to demonstrate.
// A seat rule is a child of the page's turn rule directly above it; giving both
// the heavy form would restate the hierarchy problem §9.23 fixed by weight, one
// level down. Two weights buy one distinction — outline against interior — and
// spending the heavy one on a heading inside the outline would spend it twice.
//
// The name takes the SEAT's own hue (§9.28), and this is the site with the
// highest payoff of the three: a turn page stacks every participating seat in
// one column, one after another, so position answers nothing about who is
// speaking. In the grid a column's x-coordinate has already answered it before
// the eye reaches the name — which is exactly why the grid's own headers are on
// the closed list of places this hue is NOT spent.
func seatRule(v model.VendorID, label, meta string, w int, sty Styles, g Glyphs) string {
	return labelledRule(label, meta, w, g.Rule, sty.SeatStrong(v), sty)
}

// strongLabelRule draws labelRule with the LABEL at weight and everything after
// it — the rule and the numbers hanging off its end — muted.
//
// labelRule's grammar AND labelRule's arithmetic: the plain line is built there
// and only then split, so nothing here can drift into a second spelling of a
// shape this room has drawn since §9.11. The split itself is the figure/ground
// one the column header and the mode line already make — the thing you scan for
// at full intensity, the numbers that belong to it receding — applied to a
// heading instead of to a key.
//
// It exists as its own function because a turn page has TWO levels of heading
// and they were drawn at one volume. §9.22 gives the page a turn rule at the top
// and a seat rule per participant under it, and the turn rule — the parent, the
// thing the whole document is about — rendered wholly Muted while its children
// took Strong. The room's own hierarchy inverted: the outline whispered and the
// entries shouted, so the eye landed on the four seats and had to hunt upward
// for which turn it was reading.
//
// The GRID's turn separator is untouched, and the asymmetry is the argument
// rather than an oversight. There a turn rule sits inside a column already
// headed by a seat name at weight, so it is the child and muted is its correct
// rank; here it is the root. The same line changes weight because it changed
// what it is the parent of, which is exactly what swapping the projection means
// (§9.22).
//
// PlainStyles renders both Strong and Muted as the identity function, so this
// moves no cell and no golden sees it; TestPageTurnRuleOutranksItsSeats asserts
// it where colour is asserted (§9.5).
//
// fit, not padRight, because the line is assembled from differently-styled
// pieces. That is §9.5's ANSI trap, and the goldens are blind to it.
func strongLabelRule(label, meta string, w int, ruleGlyph string, sty Styles) string {
	return labelledRule(label, meta, w, ruleGlyph, sty.Strong, sty)
}

// labelledRule is strongLabelRule with the LABEL's style supplied, so a seat's
// rule can carry that seat's own hue (§9.28) without a second copy of the
// split — which is the same argument labelRule makes for its own arithmetic.
//
// head is applied to the label and Muted to everything after it, always. The
// figure/ground relation is the grammar; only which figure is being drawn
// changes.
func labelledRule(label, meta string, w int, ruleGlyph string, head lipgloss.Style, sty Styles) string {
	plain := labelRuleIn(label, meta, w, ruleGlyph)
	rest, ok := strings.CutPrefix(plain, label)
	if !ok {
		return fit(sty.Muted.Render(plain), w)
	}
	return fit(head.Render(label)+sty.Muted.Render(rest), w)
}

// seatMeta is the numbers that belong to one seat's rule: how its turn ended,
// how long it took, and what the vendor said it cost.
//
// The same three the column header and a turn separator already state, in the
// same order and the same words (columnStatus, historyMeta). What it does NOT do
// is historyMeta's "only a turn that ended badly names its phase": on a page
// every block is a different seat, so the outcome is the fact that distinguishes
// them and dropping it on the common case would leave four rules that differ
// only by a name.
func seatMeta(st State, e turnEntry, g Glyphs) string {
	// The operator's share, once, whichever branch below states the clock — and
	// the LONG spelling, because a page rule is the full frame wide and the room
	// says `waiting on you` everywhere it has the cells for it (§9.45).
	//
	// The OPEN stretch is added only to a live entry. State.Gates is a queue of
	// cards up right now, which belong to the turn in flight; folding it into a
	// filed record would grow a finished turn's figure every second the room
	// stayed stopped on a later one.
	op := e.GateWait
	if e.Live {
		op = operatorWait(st, e.Vendor, e.GateWait)
	}
	// A LIVE seat the room is stopped on says so here too, in the same words the
	// column header uses (stoppedOnYou, §9.45's amendment). A page states one
	// seat's turn on one rule, and a page saying `streaming` while the grid behind
	// it says `needs you` would be the two surfaces disagreeing about a fact both
	// read out of the same queue.
	//
	// It is LIVE only, and that is what keeps a filed turn honest: a record's
	// phase is how that turn ENDED, and the queue only ever describes now.
	blocked := e.Live && (e.Phase == PhaseWaiting || e.Phase == PhaseStreaming) &&
		st.gateStopped(e.Vendor)

	word, mark := e.Phase.String(), phaseMark(e.Phase, st, g)
	if blocked {
		word, mark = needsYouWord, g.Warn
	}
	parts := []string{mark + " " + word}
	switch {
	case blocked:
		// No vendor clock under the word, exactly as the header drops it: the
		// number the reader wants is the operator's, and it arrives below in its
		// long spelling — which this surface has the cells for.
	case e.Live && (e.Phase == PhaseWaiting || e.Phase == PhaseStreaming):
		// A running seat's clock is State.Now minus its own start, which is
		// where the column header reads it from — never a clock inside Render.
		if s := elapsedSince(st, e.Started, op); s != "" {
			parts = append(parts, s)
		}
	case e.Elapsed > 0:
		parts = append(parts, dur(vendorElapsed(e.Elapsed, op)))
	}
	// After the clock, exactly as columnStatus places it: the figure is the time
	// to the ANSWER, and the linger must not read as part of it. Both surfaces
	// say the same word for the same seconds, which is the point of carrying the
	// field this far — the footer already keeps `ctrl+c` up on this page.
	if e.Settling {
		parts = append(parts, "exiting")
	}
	if s := operatorCell(op, longForm); s != "" {
		parts = append(parts, s)
	}
	if e.CostUSD != nil {
		cost := "$" + strconv.FormatFloat(*e.CostUSD, 'f', 4, 64)
		if e.CostSession {
			// The word, not a symbol — a running total and a turn's spend are
			// different quantities and one rendering for both is the ambiguity
			// §4a.1 forbids (costCell).
			cost += " session"
		}
		parts = append(parts, cost)
	}
	return strings.Join(parts, "  ")
}

// pageMeta is what the turn's own rule carries beside its number: where the turn
// went, and how long it took.
func pageMeta(st State, entries []turnEntry) string {
	var parts []string
	if r := pageRoute(st, entries); r != "" {
		// The literal arrow the composer's routing cell and the header both use,
		// rather than a Glyphs entry — §9.21's rule, so one fact cannot drift
		// into two spellings, and it is identical under --ascii for the same
		// reason it is identical there today.
		parts = append(parts, "→ "+r)
	}
	if d := turnElapsed(entries); d > 0 {
		parts = append(parts, dur(d))
	}
	// Joined with historyMeta's own two spaces. This room has one grammar for
	// "the numbers that belong to a label" and a middle dot would be a second.
	return strings.Join(parts, "  ")
}

// pageRoute is where the turn went, read off participation.
//
// It is NOT State.TurnRoute, and that is what lets the page say it about a turn
// that ended an hour ago: §9.21 retires the live route the instant the last
// column lands, because the header describes the present and a header still
// naming a finished turn's destination would be describing the past in the one
// cell that cannot. What outlives it is the measurement — a TurnRecord exists
// for exactly the seats the brief reached — so the page states who took the
// turn, which is the fact each column wrote down rather than a route restored
// from anywhere.
//
// It prints through Route.label() rather than assembling words of its own, so
// what is displayed is what would have to be typed to reproduce it — §9.21's
// rule again, one surface over. "everyone" is claimed only when the turn reached
// every seat this room would dispatch to, which is what State.Seated() counts
// and what `@all` parses to.
func pageRoute(st State, entries []turnEntry) string {
	if len(entries) == 0 {
		return ""
	}
	if len(entries) == st.Seated() {
		return Route{}.label()
	}
	vs := make([]model.VendorID, 0, len(entries))
	for _, e := range entries {
		vs = append(vs, e.Vendor)
	}
	return Route{Vendors: vs}.label()
}

// turnElapsed is how long the turn made the user wait: the longest seat's own
// measured elapsed.
//
// A SELECTION from measured values, never an arithmetic over them. The turn is
// over when its slowest seat lands, so the largest of them is a duration a clock
// in this room really read; a sum would be the wall time of a room that
// dispatched serially, and a mean would be a figure no seat ever took. Deriving
// a number and presenting it as read is the top item on this repo's rejected
// list (§4a.1), and it does not stop applying because the number happens to be
// in seconds.
//
// Zero when no participant reported one, and the caller omits it — this room
// does not draw "0s" for a figure it does not have.
func turnElapsed(entries []turnEntry) time.Duration {
	var max time.Duration
	for _, e := range entries {
		if e.Elapsed > max {
			max = e.Elapsed
		}
	}
	return max
}

// pageChrome is the fixed top of a page: whatever a vendor is stopped behind,
// and one blank row before the reading starts.
//
// The gate is chrome here for the reason it is chrome on a column
// (columnChrome), and the reason is stronger on a page than in the grid: a
// vendor is STOPPED until a key is pressed, the live page follows its own tail
// while output arrives, and a card in the body would be pushed off screen by the
// output of the very call it is asking about.
func pageChrome(st State, w int, sty Styles, g Glyphs) []string {
	card := pageGateCard(st, w, sty, g)
	if len(card) == 0 {
		return nil
	}
	out := make([]string, 0, len(card)+1)
	for _, l := range card {
		out = append(out, fit(l, w))
	}
	return append(out, strings.Repeat(" ", maxInt(0, w)))
}

// pageGateCard is the approval prompt as a page has to state it.
//
// It NAMES THE SEAT, which the grid's card never had to: in the grid the card's
// position is the seat, and a projection with one page has no position left to
// carry it. A card asking approval for a write without saying which vendor is
// about to make it would be §9.2's "a claim you cannot see is not a claim" with
// the claim present and the subject missing.
//
// The oldest request only, with the whole queue's count behind it — gateCard's
// rule, unchanged: rendering the queue would put several decisions under one
// pair of keys.
func pageGateCard(st State, w int, sty Styles, g Glyphs) []string {
	if w < 12 || len(st.Gates) == 0 {
		return nil
	}
	label := string(st.Gates[0].Vendor)
	for _, c := range st.Columns {
		if c.Vendor == st.Gates[0].Vendor {
			label = c.Label
			break
		}
	}
	return gateCardLines(st.Gates, label, w, sty, g)
}

// pageHint is the keys a page's overflow marker names.
//
// The focused-column form and nothing else. `tab to focus` has nothing to point
// at — there is one page and no column focus for a key to move (§9.22) — and `f`
// would expand the only thing on screen to the width it already has, which is
// the key that does nothing §9.11 refuses to advertise on a marker or on a
// footer.
func pageHint(st State, g Glyphs) []string {
	_ = st
	return []string{g.Up + g.Down + " scroll"}
}

// pageCell renders the page to exactly h lines of exactly w cells.
//
// columnCell's body, pointed at a different list, and the parts that look like
// duplication are the parts that must not be: the overflow markers, the
// bottom-anchor and the ANSI-safe fit are one contract about how a reading area
// behaves in this room, and a page that anchored differently would make `t` a
// jump rather than a projection.
//
// The markers carry NO turn coordinate. §9.20 put one there because a column's
// window can straddle several turns and "↑ 509 more above" answered the wrong
// question; every line on a page belongs to the same turn, and the mode word
// already names it, so a coordinate here would be the room saying one thing
// twice and spending the marker's narrowest cells to do it.
func pageCell(st State, w, h int, sty Styles, g Glyphs) []string {
	chrome := pageChrome(st, w, sty, g)
	if len(chrome) > h {
		chrome = chrome[:h]
	}

	body := pageLines(st, st.Page.Turn, w, sty, g)
	avail := h - len(chrome)
	win, above, below := scrollWindowAt(st.Page.Scroll, st.Page.Follow, body, avail)
	hint := pageHint(st, g)

	lines := make([]string, 0, h)
	lines = append(lines, chrome...)

	bodyLines := make([]string, 0, len(win))
	for i, l := range win {
		switch {
		case i == 0 && above > 0:
			bodyLines = append(bodyLines, sty.Muted.Render(padRight(
				overflowMarker(g.Up, above, "above", "", hint, w, g), w, g)))
		case i == len(win)-1 && below > 0:
			h := hint
			if above > 0 {
				// Named once per reading area, the same rule columnCell keeps:
				// twice in one cell makes the count harder to find.
				h = nil
			}
			bodyLines = append(bodyLines, sty.Muted.Render(padRight(
				overflowMarker(g.Down, below, "below", "", h, w, g), w, g)))
		default:
			// fit, not padRight — the ANSI trap (§9.5). A page's lines carry an
			// outcome mark, a seat name at weight and a note's warning glyph.
			bodyLines = append(bodyLines, fit(l, w))
		}
	}

	blank := strings.Repeat(" ", maxInt(0, w))
	// Spare rows ABOVE the document, which is the grid's own bottom-anchor: what
	// arrives next belongs beside the composer, and a page of a live turn is the
	// case that argument was written for.
	for pad := avail - len(bodyLines); pad > 0; pad-- {
		lines = append(lines, blank)
	}
	lines = append(lines, bodyLines...)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// pageBody frames the page into the body area, with tabBody's one pad each side
// — it is the same single-column geometry and there is no second layout path.
func pageBody(st State, lay Layout, sty Styles, g Glyphs) string {
	cell := pageCell(st, lay.ColWidth, lay.Body, sty, g)
	var b strings.Builder
	for i, l := range cell {
		b.WriteString(framePadStr + l + framePadStr)
		if i < len(cell)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// pageViewport resolves what the page is drawn into: its lines and how many body
// rows survive the chrome.
//
// columnViewport's argument applies unchanged — two derivations of "how tall is
// this content at this width" drift the day a card grows a row, and they drift
// silently because both answers are still plausible line numbers — so the
// keystroke clamp and the renderer measure through the same call.
func pageViewport(st State) (lines []string, avail int, ok bool) {
	if !st.Page.Open || st.Width < MinWidth || st.Height < MinHeight {
		return nil, 0, false
	}
	lay := layoutFor(st, GlyphsFor(st.ASCII))
	if lay.Tier == TierFloor {
		return nil, 0, false
	}
	// PlainStyles because only the line COUNT is wanted, and no style in this
	// package changes one — every style here is a wrapper, never a re-wrap.
	sty, gl := PlainStyles(), GlyphsFor(st.ASCII)
	avail = lay.Body - len(pageChrome(st, lay.ColWidth, sty, gl))
	return pageLines(st, st.Page.Turn, lay.ColWidth, sty, gl), avail, true
}

// PageMaxScroll is the largest useful offset for the open page. Exported beside
// MaxScroll, for the same reason: the program loop clamps a keystroke against
// the geometry the renderer resolved rather than one it kept its own copy of.
func PageMaxScroll(st State) int {
	lines, avail, ok := pageViewport(st)
	if !ok {
		return 0
	}
	if m := len(lines) - avail; m > 0 {
		return m
	}
	return 0
}

// pageLabel is the mode word for the by-turn projection, and it carries the
// drift the body refuses to.
//
// TurnView.Turn does not follow State.Turn, because a turn arriving must not
// move the view (§7.1 rule 4) — so something has to say that the room has moved
// on, and the mode word is where a reader already looks to learn what the keys
// mean. "TURN 10/11" is the page and the newest turn, in the numbers the
// separators and the yank notice already print.
//
// §9.20 declined "turn 3 of 7", and this is not that decision reversed. That was
// a progress bar for a conversation, offered in the NOTICE line — a transient
// message, describing a hop that had already happened. This is the always-on
// mode label answering which of two projections is live and which turn it is
// showing, which is precisely what §7.8 requires a mode line to state and what
// the body has been ruled out of stating.
//
// The WORD changes with the face, and it has to: `t` and `T` open the same
// coordinate onto two different documents, so a mode line that said TURN over
// both would leave the room's one always-on statement of what is on screen
// unable to tell them apart. ACTS against TURN is that distinction in the cell
// §7.8 reserves for exactly it, and the numbers behind it are unchanged because
// the coordinate is unchanged.
func pageLabel(st State) string {
	word := "TURN "
	if st.Page.Ledger {
		word = "ACTS "
	}
	s := word + strconv.Itoa(st.Page.Turn)
	if st.Turn > 0 {
		s += "/" + strconv.Itoa(st.Turn)
	}
	return s
}
