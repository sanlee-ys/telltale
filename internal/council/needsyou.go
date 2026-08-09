package council

import (
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The needs-you strip (design.md §9.40).
//
// The gate (§9.8) stops a vendor until a key is pressed, and the room says so in
// two places: a card inside the blocked seat's own column, and `GATE` on the mode
// line. Neither of them names the SEAT. The card does not have to — its position
// is the seat (gateCard) — and the mode line cannot: gateLabel prints the oldest
// call's own text, `Write: internal/council/gate.go`, which says what is blocked
// and never who. So in a room of four or five seats the footer announces that
// something is stopped and the reader has to go looking for it, one column at a
// time, while it stays stopped. That is the stall this line exists to delete.
//
// **It is driven by the gate queue and by nothing else.** State.Gates is a
// structured record of vendors that asked for permission and have not been
// answered; every name on this strip comes from one of those entries. A seat that
// has gone quiet, a seat streaming nothing, a seat whose reply merely looks like a
// question — none of them reach this line, because none of them is a measurement
// that anyone is blocked (§4a.1). "Needs you" is a claim about a vendor waiting on
// a keystroke, and the queue is the only thing that knows.
//
// **A seat leaves the strip when the user goes to it, and that is the only thing
// besides answering that takes it off.** Derived from Focus rather than stored as
// an acknowledged set, on Gating()'s own argument: a stored set is a second place
// for the same fact to live, and the two drift the first time a seat's gate is
// answered and a new one arrives while the old acknowledgement is still in the
// map — at which point the anti-stall silently omits a seat that is waiting,
// which is the one failure it exists to prevent. Derived, the worst that happens
// is the strip re-listing a seat the reader has already visited and left, and
// that is TRUE: it is still stopped and they are no longer looking at it.
//
// **The default focus is a real hole in that rule and it is deliberately left
// open.** NewState seats the keys on column 0 without the user pressing anything,
// so a gate on the seat that happens to be focused never appears here at all.
// That is the correct outcome rather than a gap to close: the reader is looking
// at the column whose card is already spelling the question out, and a room-level
// line naming the seat under the reader's own cursor is the duplication §9.30
// spent a whole section removing.
type needsYouCell struct {
	// num is the seat's key (§9.29), or empty when there is no live key to
	// print — see needsYouLine.
	num string
	// name is the seat's identity at whichever rung of the ladder is being
	// tried: its label, or its two-letter tag.
	name string
}

func (c needsYouCell) text() string {
	if c.num == "" {
		return c.name
	}
	return c.num + " " + c.name
}

// needsYouLead is the words, and the words are the whole signal.
//
// Upper case for the same reason `WRITE` and `GATE` are: this is one of the three
// things in the room that says a state rather than describes content, and it has
// to be findable in one pass over a frame with four columns of vendor prose in it.
// Colour and weight are added on top and neither is load-bearing — under --ascii
// and NO_COLOR the phrase reads exactly the same, which is the property every
// distinction this UI makes has to have.
const needsYouLead = "NEEDS YOU"

// needsYouGap separates the seats on the strip.
//
// Three cells, which is the gate card's own spacing for its key list
// (`y approve   n deny   a stop asking`) and not the two the tab bar uses. The
// difference is what the entries are: a tab is one word and this is a number
// welded to a name, so at two cells `2 Codex  3 Antigravity` reads as one run of
// four tokens rather than two seats.
const needsYouGap = "   "

// needsYou is the seats a pending gate is stopped on, in SEATING order, minus the
// seat the reader is already looking at.
//
// Seating order rather than the queue's arrival order, and the two differ. The
// queue is oldest-first because that is the only order a person can answer cards
// in (State.Gates); this line is not answered, it is SCANNED, and its numbers are
// the keys that reach each seat — so they run 1, 2, 3 down the row exactly as the
// tab bar and the column headers print them. A strip whose numbers arrived out of
// order would be asking the reader to sort five two-digit facts to find the one
// they want.
//
// It walks every column rather than the visible ones. A seat folded out of the
// grid can still hold an unanswered gate — its vendor is blocked whether or not
// the room drew it a column — and a blocked vendor with no card, no column and no
// line anywhere is the disappearance §4a.1 forbids. It gets no seat number (there
// is no key that reaches an unseated column) and it is therefore the one entry
// here that cannot be cleared by focus, which is honest: nothing the reader can
// press from this room will unblock it.
func needsYou(st State) []int {
	if len(st.Gates) == 0 {
		return nil
	}
	waiting := make(map[model.VendorID]bool, len(st.Gates))
	for _, p := range st.Gates {
		waiting[p.Vendor] = true
	}
	var out []int
	for i, c := range st.Columns {
		if i == st.Focus || !waiting[c.Vendor] {
			continue
		}
		out = append(out, i)
	}
	return out
}

// needsYouRows is how many rows the strip wants: one, or none.
//
// A predicate rather than a measurement — the opposite of bandRows, which has to
// draw the band to find out how tall it is. This line never wraps: it sheds
// (needsYouLine), so its height is one row for as long as it has anything to say
// and the layout can decide it from the queue alone.
func needsYouRows(st State) int {
	if len(needsYou(st)) == 0 {
		return 0
	}
	return 1
}

// needsYouLine draws the strip at width w.
//
// **The ladder is longest-first, widest-that-fits-wins** — stripHeader's idiom, so
// a reader of this package meets one shedding shape rather than three. Its three
// rungs are:
//
//  1. every seat, by name.
//  2. every seat, by the two-letter tag §9.25 made permanent.
//  3. as many tagged seats as fit, and a count of the rest: `+2 more`.
//
// **Identity yields before a SEAT does**, which is §9.18's order and the one place
// it had to be reasoned about again here. A four-seat strip at sixty columns can
// hold `2 Codex   3 Antigravity   +2 more` or it can hold `2 CX   3 AG   4 CU
// 5 GR`, and the second is better by the measure this line exists for: the reader
// is looking for WHO is stopped, four abbreviations they already know answer it
// completely, and two names plus a number answer half of it. So the tag rung is
// tried at full roster before any seat is dropped.
//
// Nothing here is ever clipped, at any rung. A clipped seat name is not a
// shortened seat name — `Ant` is a seat this room does not have (§9.18) — so an
// entry either survives whole or leaves and is counted.
//
// **The floor keeps the words and loses everyone.** Below the width where even one
// tagged seat fits, what survives is `⚠ NEEDS YOU` on its own: still true, still
// the signal that something is stopped, and honest about being unable to say who
// at that width. The alternative — dropping the line entirely — would trade the
// only room-level statement that a vendor is blocked for three cells.
//
// **The seat NUMBER is printed only where the key is live.** Digits focus a seat
// through viewKey, and gateKey falls through to it, so while the room is gating
// the number works in both modes — except on a turn page, where focusSeat refuses
// outright because a page has no columns to move between (§9.22). A number
// printed there would be the room naming a key that does nothing, which is §7.8's
// surprise; the names stay, because who is stopped is still true on a page.
//
// Styled after it is measured, never before: every width test here runs over the
// plain string, and the assembled line carries escapes from two styles, so its
// caller has to use fit rather than padRight (§9.5's ANSI trap).
func needsYouLine(st State, w int, sty Styles, g Glyphs) string {
	seats := needsYou(st)
	if len(seats) == 0 || w < 1 {
		return ""
	}
	lead := g.Warn + " " + needsYouLead

	cells := func(name func(Column) string) []needsYouCell {
		out := make([]needsYouCell, 0, len(seats))
		for _, i := range seats {
			c := st.Columns[i]
			num := ""
			if n := st.SeatNumber(c); n > 0 && !st.Page.Open {
				num = strconv.Itoa(n)
			}
			out = append(out, needsYouCell{num: num, name: name(c)})
		}
		return out
	}
	// Both identities are ones the room has already taught: the tag rides in
	// front of every seat name at every width wide enough to print one (§9.25),
	// so a strip that falls back to `CX` is using a two-letter word the reader
	// learned from the column header rather than introducing one here.
	byLabel := cells(func(c Column) string { return c.Label })
	byTag := cells(func(c Column) string { return vendorTag(c.Vendor) })
	if s, ok := needsYouWhole(byLabel, lead, w, sty); ok {
		return s
	}
	if s, ok := needsYouWhole(byTag, lead, w, sty); ok {
		return s
	}
	if s, ok := needsYouFit(byTag, lead, w, sty); ok {
		return s
	}
	return sty.Alert.Render(truncate(lead, w, g.Ellipsis))
}

// needsYouWhole is the strip with every waiting seat on it, or nothing.
//
// The two upper rungs: no seat is dropped and no count is printed, because there
// is nothing to count.
func needsYouWhole(cs []needsYouCell, lead string, w int, sty Styles) (string, bool) {
	plain, styled := needsYouJoin(cs, 0, lead, sty)
	if lipgloss.Width(plain) <= w {
		return styled, true
	}
	return "", false
}

// needsYouFit assembles the strip from as many WHOLE entries as w allows, and
// reports whether at least one survived.
//
// The count of what was dropped is never traded away, on overflowMarker's rule:
// a line telling a reader that seats are hidden without saying how many is the
// marker §9.10 shipped and got reported as a room that could not scroll. So the
// `+N more` cell is measured as part of every candidate rather than appended to
// one that already fit.
func needsYouFit(cs []needsYouCell, lead string, w int, sty Styles) (string, bool) {
	for keep := len(cs) - 1; keep >= 1; keep-- {
		plain, styled := needsYouJoin(cs[:keep], len(cs)-keep, lead, sty)
		if lipgloss.Width(plain) <= w {
			return styled, true
		}
	}
	return "", false
}

// needsYouJoin writes the strip twice — once plain for the width arithmetic, once
// styled for the screen — from one walk, so the two cannot disagree about what is
// on the line.
//
// The seat NUMBERS are Muted and everything else is Alert. That split is the
// grammar the column header and the tab bar already use for the same two things:
// the number is chrome and the name is the anchor. Alert — SevWarn at weight — is
// the gate card's own title style, and this line is that card's claim hoisted to
// the room: `waiting on you` in one column, `NEEDS YOU` above all of them. The
// `+N more` cell stays Muted with the numbers, because it counts seats rather
// than naming one.
func needsYouJoin(cs []needsYouCell, dropped int, lead string, sty Styles) (plain, styled string) {
	var p, s strings.Builder
	p.WriteString(lead)
	s.WriteString(sty.Alert.Render(lead))
	for _, c := range cs {
		p.WriteString(needsYouGap + c.text())
		s.WriteString(needsYouGap)
		if c.num == "" {
			s.WriteString(sty.Alert.Render(c.name))
			continue
		}
		s.WriteString(sty.Muted.Render(c.num) + " " + sty.Alert.Render(c.name))
	}
	if dropped > 0 {
		more := "+" + strconv.Itoa(dropped) + " more"
		p.WriteString(needsYouGap + more)
		s.WriteString(needsYouGap + sty.Muted.Render(more))
	}
	return p.String(), s.String()
}
