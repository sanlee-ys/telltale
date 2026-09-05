package council

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Tier is a responsive breakpoint, on width only, with a fixed shedding order —
// same contract as the HUD's, so the layout at any width is a pure function of
// that width and every golden is reproducible.
type Tier uint8

const (
	// TierFloor: too narrow to draw the room at all.
	TierFloor Tier = iota
	// TierTabs: one column at a time, with a tab bar.
	TierTabs
	// TierColumns: every seated column side by side.
	TierColumns
)

const (
	// MinWidth is the floor. Below it a column would be too narrow to read a
	// sentence in, and three of them would be shredded prose.
	MinWidth = 60
	// MinHeight leaves room for header, one body line, and the prompt footer.
	MinHeight = 10

	// columnsBreak is where three columns stop being readable. At 96 each
	// column gets ~30 usable cells, which is about six words — the point below
	// which side-by-side comparison stops beating tabs.
	columnsBreak = 96

	// framePad is the margin between the terminal's edge and anything the room
	// draws — one value, applied on BOTH sides of every line.
	//
	// It was a bare " " literal in about ten builders (the header, the notice,
	// the column grid, the tab bar, the single-column and turn-page bodies, the
	// composer's five row shapes, the mode line, the help panel) with its
	// arithmetic twin — a literal 2, being pad×2 — in eight more places that
	// subtract it back out to get a usable width. Those two families have to
	// agree exactly: a builder that padded more than its width arithmetic
	// subtracted would push the row past the terminal edge, which is the
	// off-by-one §9.11 found in the header's gap and fit was quietly eating.
	// Naming it makes that agreement checkable instead of a coincidence
	// maintained by hand.
	//
	// Sites that subtract it use framePad*2 rather than a literal, so the two
	// move together. A `- 2` NOT written that way is something else — labelRule's
	// two cells of air around its rule, for instance — and is deliberately left
	// alone.
	//
	// **Two, and it is the same two `gutter` is.** The room had it at one, so the
	// interior of the grid breathed and its edges did not: two cells each side of
	// every rail, one cell at the terminal's edge. That is a margin narrower than
	// the gutters inside it, which is the inverse of what a grid wants — the eye
	// reads the outer boundary as the tightest thing on screen and the whole
	// frame as crowded against the terminal, while the middle reads loose. The
	// screenshot pass that set `gutter` to 2 named exactly this feeling ("rigid /
	// cramped") and fixed it in the one place it was looking.
	//
	// It costs 2 cells of total width, which is the same trade `gutter` made and
	// is bounded the same way: `minColumn` and `stripColumn` are floors on what a
	// column may shrink to, and the tier drops to tabs before a column crosses
	// them, so the cells come out of reading width at wide frames and out of
	// nothing at narrow ones. `TestColumnsExactlyFillTheWidth` recomputes the
	// chrome from this constant, so the arithmetic cannot drift from the paint.
	framePad = 2

	// gutter is the space each side of the vertical separator between columns.
	//
	// Two, not one: a single cell left prose welded to the │ on Windows
	// Terminal (the reference host), which is the "rigid / cramped" read the
	// screenshot pass named. One extra cell each side costs ~1–2 cells of
	// wrap width per column at four-up and buys the air the frame was missing.
	// columnsBody must use the same constant — ColWidth math and the painted
	// sep have to agree or the row overflows the terminal.
	gutter = 2

	// minColumn is the narrowest a PRIMARY column may be before the tier drops.
	minColumn = 24

	// stripColumn is the width of a seat that is on screen but not owning the
	// frame this turn (unaddressed under a narrow route). Intent-controlled —
	// see State.FrameOwners.
	//
	// **Eighteen, and it is a READING width rather than an arithmetic floor.**
	// It was fourteen, and fourteen was derived — the widest phase word
	// (`streaming`, `cancelled`) is nine cells, its mark costs two more, and the
	// remaining three are exactly a two-letter vendor tag and the space after
	// it. That arithmetic is correct and it answers the wrong question. It says
	// what a strip's HEADER cannot go below; it says nothing about the prose
	// underneath, and prose is most of what a strip draws.
	//
	// At fourteen the prose shredded. §9.19's coalesced skip line — the ordinary
	// content of a backgrounded seat, on most turns the ONLY content it has —
	// came out as `○ not` / `addressed in` / `turn 4`, three lines to say one
	// short sentence, with the phrase that carries the meaning split across two
	// of them. §9.19's `last: turn 8 ✓` was likewise wrapping. A column whose
	// every line breaks mid-phrase is not narrow, it is unreadable, and the
	// point of keeping these seats on screen at all (§9.18) is that a reader can
	// take them in at a glance.
	//
	// Eighteen is the smallest width that lets `○ not addressed` sit on one line
	// and `last: turn 8 ✓` sit on one line — the two things a strip exists to
	// say. The header floor is still a floor and still holds: fourteen remains
	// the width below which the header itself would break, so eighteen clears it
	// by four and the shedding ladder §9.18 built is untouched.
	//
	// It costs four cells, taken from the primary column's reading width, and
	// `weightedWidths` refuses the split outright rather than shipping a primary
	// under `minColumn` — so at a width where four cells would matter, the frame
	// falls back to equal columns instead of trading a readable strip for an
	// unreadable seat.
	stripColumn = 18

	// stripWidth is the width at or below which a column stops rendering as a
	// seat and renders as a STRIP: identity collapses to its two-letter vendor
	// tag, the clock and the cost leave, and the badge row keeps only a posture
	// word that fits whole (view.go, stripHeader / stripBadges).
	//
	// It is stripColumn itself, so the two cannot disagree about what a strip is.
	//
	// Nothing else in this package can land here. A PRIMARY column never falls
	// below minColumn (24) — the tier drops to tabs first — and a tabbed column
	// is the frame minus two pads, so MinWidth already floors it at 56. So a
	// column at or under this width is a strip, and no second predicate is
	// needed to say so.
	stripWidth = stripColumn

	// promptChrome is the fixed part of the footer: the composer box's two
	// borders, and the key line below it. The composer itself is variable — see
	// Layout.Prompt, which counts the rows INSIDE the box.
	//
	// Three, and the third row is what §9.44 cost. It was two — one heavy rule
	// above the compose area, one mode line under it — and the box replaces the
	// rule with a top border and adds a bottom border to close the shape. Net one
	// row, taken from the body, and it is spent where every other row in this
	// budget is defended: the notice, the needs-you strip and the band all take a
	// body row to say something the body cannot. This one says where the room ends
	// and where you type.
	//
	// The room's own floor is unchanged by it. At MinHeight the budget is 2 header
	// rows + 3 here + a one-row composer, which still leaves the columns four rows
	// of reading area — TestFrameHeightFitsTheTerminal and TestFrameNeverTears
	// sweep the matrix that proves it.
	promptChrome = 3
	// headerRows is the title line plus its rule.
	headerRows = 2

	// minBandSeats is how many on-screen seats have to be carrying the LIVE
	// turn's brief before it is drawn once as a band instead of once per column
	// (§9.30).
	//
	// Two, because two is where the duplication starts. A turn routed to one seat
	// — the ordinary room since the default route stopped being everyone — has one
	// echo on screen, and hoisting it out of that column would move the user's
	// words away from the answer to them and buy nothing. The band is a fix for a
	// comparison surface saying the same thing two to four times, so it appears
	// exactly where that is true.
	minBandSeats = 2

	// maxBandBrief is how many rows of wrapped brief the band may spend.
	//
	// Four, and the last of the four is the TRUNCATION MARKER when the brief needs
	// more — so a long brief renders three rows and a line saying how much is left
	// and where to read it, never four rows that stop mid-sentence. Silent
	// clipping is the ambiguity §4a.1 forbids: a reader cannot tell a brief that
	// ended from a brief that was cut.
	//
	// It is a ceiling rather than the height. A one-line brief costs one row, which
	// is the common case, so the band is as tall as the words are and no taller —
	// the same shape maxComposerRows has, for the same reason: the body pays for
	// the difference.
	maxBandBrief = 4

	// minBandBody is how many body rows have to survive the band before it is
	// spent at all.
	//
	// Eight, and it is measured from what a column has to draw before a word of
	// the reply: columnChrome is three rows (the seat's name, its posture claim,
	// one blank), and the live turn's own separator is a fourth. A body that kept
	// fewer than four rows of reply under that is a column showing its chrome and
	// a ticker — and duplication removed from a reading area there is none of is
	// not worth a row. Below this the band yields ENTIRELY and the addressed
	// columns echo the brief themselves, which is the pre-band frame exactly.
	//
	// The test is against the band's rows and the composer's FLOOR, never the
	// composer's current height, so the answer is a pure function of the terminal
	// and the brief. A band that retired because the draft grew a row would be a
	// layout jump on a keystroke in the middle of a turn — §7.1 rule 4 — and it
	// would jump back when the user hit backspace.
	minBandBody = 8

	// paneStep is how far one press of a resize key moves a pane boundary
	// (§9.51).
	//
	// It is the separator's own width — one rail plus its two gutters — and it is
	// DERIVED rather than tuned. One press slides a boundary by exactly the gap a
	// reader already sees between two panes, so the move is visible on the first
	// press and the number has a name instead of a provenance the next reader has
	// to trust. A one-cell step re-wraps nothing on most lines, which reads as a
	// key that did not work; a step of half a column overshoots every width worth
	// stopping at.
	paneStep = 1 + 2*gutter

	// maxComposerRows is how tall the compose area may grow.
	//
	// Six, because a brief worth sending to four agents is a paragraph and one
	// elided line was not somewhere anyone could think. It is a ceiling rather
	// than the height: the composer is as tall as the draft needs and no
	// taller, so a room nobody is typing in looks exactly as it always did.
	// Body pays for the difference, which is why the ceiling exists at all.
	maxComposerRows = 6
)

// framePadStr is framePad as the string the builders actually write. Derived
// rather than spelled, so the literal and the arithmetic cannot disagree.
var framePadStr = strings.Repeat(" ", framePad)

// Layout is the resolved plan for one frame.
type Layout struct {
	Tier  Tier
	Width int
	// Cols is how many columns are drawn side by side (1 in TierTabs).
	Cols int
	// ColWidth is the usable text width inside one column when the frame is
	// equal (or the tabbed single column). Weighted frames use ColWidths.
	ColWidth int
	// ColWidths is per drawn column when FrameOwners narrows the turn. Nil
	// means every column uses ColWidth (+ extraFor on the leftmost).
	ColWidths []int
	// Body is how many rows the column bodies get.
	Body int
	// Tabs reports that a tab bar is drawn above the body.
	//
	// Not the same as TierTabs. A room with ONE seat on screen is the tabs tier
	// — there is nothing to put side by side — but a bar holding a single tab
	// selects nothing and names the column the header underneath already names.
	// That used to be a rarity; collapsing the seats that cannot be driven made
	// it the ordinary room on a machine with one vendor installed.
	Tabs bool
	// Prompt is how many rows the compose area gets, at least 1.
	Prompt int
	// Notice is how many rows the ROOM LINE takes under the header: the seats
	// that are not on screen, the seats that sat the live turn out, and a room
	// fact too long for the footer (roomline.go). One row, two, or none.
	Notice int
	// NeedsYou is 1 when a row under the header names the seats a pending
	// approval gate is stopped on (§9.40), 0 otherwise.
	NeedsYou int
	// Ack is how many rows the write acknowledgement card spends above the
	// columns (ack.go): ackRows, or 0. It never sheds a row, for the reason
	// NeedsYou does not: the room is STOPPED behind it.
	Ack int
	// Band is how many rows the live turn's brief spends as a full-width band
	// above the columns (§9.30). Zero means no band — and it is the SAME zero the
	// columns read to decide whether to echo the brief themselves, so the two can
	// never disagree about where the user's words are.
	Band int
}

// layoutInput is everything the frame plan is computed from.
//
// A struct rather than a longer parameter list because the last two arguments
// are both small integers with no natural order, and a caller that swapped them
// would produce a plausible frame rather than a compile error.
type layoutInput struct {
	Width, Height int
	// Cols is how many columns are DRAWN — the visible seats, not every
	// detected one.
	Cols     int
	Expanded bool
	// Composer is how many rows the draft wants, before the height floor.
	Composer int
	// Notice is how many rows the ROOM LINE takes: the seats that are not on
	// screen, the seats that sat the live turn out, and a room fact too long for
	// the footer (roomline.go). One row, two, or none.
	//
	// A count rather than the bool it was, because the room line now carries
	// several facts and a room with a collapsed seat and a rebuild's cost needs
	// two rows to state them all. roomLines caps it, so this can never grow past
	// maxRoomRows.
	Notice int
	// NeedsYou reports that the needs-you strip has at least one seat to name
	// (§9.40). One row or none — the strip sheds rather than wraps, so unlike
	// Band there is no height to pass in here.
	NeedsYou bool
	// Ack reports that the write acknowledgement card is up (ack.go). A bool
	// for NeedsYou's reason: the card sheds rather than wraps, so its height is
	// the constant ackRows and no width has to cross this boundary.
	Ack bool
	// Band is how many rows the live-turn band WANTS, before the tier and the
	// height floor get a say. Zero when the turn addresses fewer than two
	// on-screen seats, or when the body is not the grid at all — there is no
	// duplication to remove in either case.
	Band int
	// Primary marks which of the Cols drawn columns own the frame this turn.
	// Nil, empty, or all-true means equal widths. When set, length must equal
	// Cols; false entries get stripColumn and the rest share what remains.
	Primary []bool
	// Bias is the OPERATOR's own adjustment to each drawn pane's width, in
	// cells, indexed like Primary (§9.51).
	//
	// Nil, or all zeros, means the operator has moved no boundary — and that is
	// the case every frame this room drew before §9.51, so the arithmetic below
	// must reach the same answer it always did on it. That is why the bias is a
	// separate input applied OVER a finished apportionment rather than a term
	// mixed into the division: the untouched path stays the untouched path, and
	// every golden taken before this feature stays byte for byte correct.
	//
	// It is not required to sum to zero. The keys always move one boundary, so
	// the state they write does sum to zero — but a seat can fold out of the
	// grid between the keystroke and the frame, and a State a test typed out by
	// hand is under no obligation at all. normalizeBias repairs it.
	Bias []int
}

func tierFor(width, cols int, expanded bool) Tier {
	switch {
	case width < MinWidth:
		return TierFloor
	// Expanded is a deliberate request for one column at full width, so it
	// outranks the width breakpoint rather than competing with it.
	case expanded || cols <= 1 || width < columnsBreak:
		return TierTabs
	default:
		return TierColumns
	}
}

// resolveLayout plans a frame with a one-row composer and no notice row.
//
// The narrow entry point, kept because most of what asks about layout is asking
// about widths, which the composer cannot change.
func resolveLayout(width, height, n int, expanded bool) Layout {
	return resolveLayoutIn(layoutInput{Width: width, Height: height, Cols: n, Expanded: expanded})
}

// resolveLayoutIn plans the frame.
//
// Cols is the number of columns to seat. The separators cost (n-1) cells plus a
// gutter each side; whatever is left divides evenly, and the remainder goes to
// the LEFTMOST drawn column rather than being scattered — see extraFor, which
// this comment used to describe as giving it to the focused one. It does not,
// and must not: a remainder that followed focus would re-wrap two columns'
// worth of prose on every tab press, which is both a moving cell §7.1 does not
// budget for and a worse way to compare two answers than a stable grid.
//
// The tier is settled BEFORE any row is budgeted, because the tab bar costs a
// row and the fallback from columns to tabs happens on a width test. Budgeting
// first and dropping the tier afterwards is how the old shape worked, and it
// only survived because the composer was a constant: a taller one would have
// overflowed the terminal by exactly the tab bar.
func resolveLayoutIn(in layoutInput) Layout {
	l := Layout{Tier: tierFor(in.Width, in.Cols, in.Expanded), Width: in.Width, Prompt: 1}
	if l.Tier == TierFloor {
		return l
	}

	chrome := 2*framePad + (in.Cols-1)*(1+2*gutter)
	if l.Tier == TierColumns && (in.Width-chrome)/in.Cols < minColumn {
		// Every column would fall under the readability floor. Tabs.
		l.Tier = TierTabs
	}

	l.Tabs = l.Tier == TierTabs && in.Cols > 1
	rows := headerRows + promptChrome
	if l.Tabs {
		rows++
	}
	rows += in.Notice
	// The needs-you strip costs a row and, unlike the band below it, it does NOT
	// yield (§9.40).
	//
	// The band's whole value is removing a duplication, so a terminal too short to
	// afford it falls back to the frame as it was and loses nothing that was not
	// already on screen. This line is the opposite trade: what it removes is a
	// SEARCH, and it is the only place in the room that says which seat is stopped
	// — gateLabel prints the call and the card lives in the column the reader has
	// to find. A row spent on that is the last row this budget should reclaim, and
	// it is spent in every tier for the same reason: at the tabs tier the blocked
	// seat may be the one column NOT on screen, which is precisely where a reader
	// has no other way to learn it exists.
	//
	// Before the band, so the band yields to it rather than the other way round —
	// same ordering argument the notice already wins on, one urgency step up.
	if in.NeedsYou {
		rows++
		l.NeedsYou = 1
	}
	// The write acknowledgement card costs ackRows and it does NOT yield, on
	// the needs-you strip's own argument one step further: the strip says a
	// vendor is stopped, and this card IS the stop. The room has spawned
	// nothing and will spawn nothing until a key is pressed, so a frame that
	// reclaimed these rows would hide the only thing on screen that says why
	// enter did nothing.
	//
	// Spent in every tier, for the strip's reason as well. At the tabs tier
	// the seats the card names may be the columns that are not on screen,
	// which is exactly where a reader has no other way to learn they were
	// addressed.
	//
	// Before the band, so the band yields to it rather than the other way
	// round. The band describes the LIVE turn's brief; this card describes the
	// turn the room has not sent, and a room that dropped the question to keep
	// the last answer's heading would have the two backwards.
	if in.Ack {
		rows += ackRows
		l.Ack = ackRows
	}

	// The band is room chrome and it is spent HERE — after the tier, out of the
	// same budget the collapsed-seat notice comes from, and before the composer
	// (§9.5's ordering, §9.30's band).
	//
	// After the tier because it is a COLUMNS-tier device: the tabs tier draws one
	// column at a time, so the brief is on screen once already and hoisting it
	// would spend a row to remove a duplication that is not there. `f` (Expanded)
	// resolves to that tier too, so it needs no test of its own here.
	//
	// Before the composer because the composer yields to the body and the band
	// must not: a band whose survival depended on how much had been typed would
	// blink in and out under a reader mid-turn. What it yields to instead is a
	// short terminal, and it yields WHOLE — the columns fall back to echoing the
	// brief themselves, which is the frame exactly as it was before this feature.
	// Shedding a row or two off the band instead would leave a truncated brief
	// above four columns that no longer say what they were asked, which is worse
	// than either end of the trade.
	band := in.Band
	if l.Tier != TierColumns {
		band = 0
	}
	if band > 0 && in.Height-rows-band-1 < minBandBody {
		band = 0
	}
	rows += band
	l.Band = band

	// The composer takes what it wants, then yields to the floor: at the
	// minimum height a six-row draft would leave the columns nothing, and a
	// room where you can type but not read is not the trade anyone asked for.
	l.Prompt = in.Composer
	if l.Prompt > maxComposerRows {
		l.Prompt = maxComposerRows
	}
	if m := in.Height - rows - 1; l.Prompt > m {
		l.Prompt = m
	}
	if l.Prompt < 1 {
		l.Prompt = 1
	}
	l.Notice = in.Notice

	l.Body = in.Height - rows - l.Prompt
	if l.Body < 1 {
		l.Body = 1
	}

	if l.Tier == TierTabs {
		l.Cols, l.ColWidth = 1, in.Width-2*framePad // one pad each side
		if l.ColWidth < 1 {
			l.ColWidth = 1
		}
		return l
	}
	l.Cols = in.Cols
	if widths, ok := weightedWidths(in.Width, in.Cols, in.Primary); ok {
		l.ColWidths = widths
		l.ColWidth = widths[0] // callers that ignore ColWidths stay sane
	} else {
		l.ColWidth = (in.Width - chrome) / in.Cols
	}
	// The operator's own boundary, applied LAST and over whichever apportionment
	// ran above (§9.51). Both bases are legal underneath it: an even grid whose
	// boundary the operator has moved, and a split grid whose owner they have
	// then grown further. Applying it here rather than inside either base is what
	// keeps the two of them the only two ways this room divides a row.
	//
	// It yields whole on a refusal. biasedWidths returns ok=false when the bias
	// is empty, when it does not describe this row, or when no pane has the
	// cells to pay for it — and in every one of those cases the frame the reader
	// gets is the frame they would have got with no bias at all, which is a
	// legal frame rather than a repaired one.
	if widths, ok := biasedWidths(l.paneBase(), in.Bias); ok {
		l.ColWidths = widths
		l.ColWidth = widths[0]
	}
	return l
}

// paneBase is the per-pane width this Layout has resolved so far, as a slice.
//
// It reads through widthAt rather than the fields, so the equal frame's
// remainder (extraFor) is already folded in and the biased frame cannot disagree
// with the unbiased one about how many cells there are to move.
func (l Layout) paneBase() []int {
	if l.Cols <= 0 {
		return nil
	}
	out := make([]int, l.Cols)
	for i := range out {
		out[i] = l.widthAt(i)
	}
	return out
}

// biasedWidths moves the operator's boundaries over a finished apportionment
// (§9.51).
//
// Contract: on ok, sum(out) == sum(base) and every out[i] >= stripColumn. The
// first half is what keeps the side-by-side join exact — the same property
// TestColumnsExactlyFillTheWidth asserts over the whole frame. The second is
// §9.18's floor: below 18 cells a column cannot say the two things a strip
// exists to say, so a boundary stops there rather than shredding the pane it is
// moving into.
//
// ok=false means "use the base", not "fail". An empty or all-zero bias takes
// that path deliberately: it is the room as it was before this feature, and it
// must render as it did.
func biasedWidths(base, bias []int) ([]int, bool) {
	if len(base) == 0 || len(bias) != len(base) {
		return nil, false
	}
	b := append([]int(nil), bias...)
	if !normalizeBias(b) {
		return nil, false
	}
	out := append([]int(nil), base...)
	for i := range out {
		out[i] += b[i]
	}
	if !repairPaneFloors(out) {
		return nil, false
	}
	return out, true
}

// normalizeBias makes a bias sum to zero, and reports whether there was any
// bias at all.
//
// The keys write a bias that already sums to zero: one press gives a pane a
// step and takes the same step off its neighbour. What breaks that is a seat
// folding out of the grid between the keystroke and the frame — the folded
// seat's entry leaves the row, and the entry that paid for it does not. A row
// that no longer sums to zero would be a row that overflows the terminal or
// leaves a ragged edge, which is the one failure the grid may not have.
//
// It corrects leftmost-first, one cell at a time, which is deterministic and
// therefore golden-testable. Correctness matters here and fairness does not:
// the state being repaired is already stale, and the operator's next press
// writes over it.
func normalizeBias(b []int) bool {
	sum, any := 0, false
	for _, v := range b {
		sum += v
		if v != 0 {
			any = true
		}
	}
	if !any || len(b) == 0 {
		return false
	}
	for i := 0; sum != 0; i = (i + 1) % len(b) {
		if sum > 0 {
			b[i]--
			sum--
			continue
		}
		b[i]++
		sum++
	}
	return true
}

// repairPaneFloors lifts every pane back to stripColumn, paying for it out of
// the widest pane that can afford it, and reports whether it could.
//
// The keys refuse a move that would breach the floor before they write it
// (paneResize), so in a running room this loop does nothing. It exists for the
// State a test types out by hand and for a bias that survived a reflow: Render
// is pure over State, so State is an INPUT this package does not control, and
// an invariant that held only because the key handler was careful is an
// invariant the golden tests could break by accident.
//
// It terminates because each pass moves one cell from a pane above the floor to
// a pane below it, so the total deficit strictly decreases.
func repairPaneFloors(w []int) bool {
	for {
		low, high := -1, -1
		for i, v := range w {
			if v < stripColumn && (low < 0 || v < w[low]) {
				low = i
			}
		}
		if low < 0 {
			return true
		}
		for i, v := range w {
			if v > stripColumn && (high < 0 || v > w[high]) {
				high = i
			}
		}
		if high < 0 {
			// Nothing on the row has a cell to spare. The caller falls back to
			// the unbiased frame, which the tier has already floored.
			return false
		}
		w[low]++
		w[high]--
	}
}

// weightedWidths apportions usable width when some seats own the frame.
//
// Returns ok=false when the split would leave a primary under minColumn or the
// strips would consume the row — callers fall back to equal columns rather than
// ship an unreadable frame.
func weightedWidths(width, cols int, primary []bool) ([]int, bool) {
	if cols < 2 || len(primary) != cols {
		return nil, false
	}
	nPrim := 0
	for _, p := range primary {
		if p {
			nPrim++
		}
	}
	if nPrim == 0 || nPrim == cols {
		return nil, false
	}
	chrome := 2*framePad + (cols-1)*(1+2*gutter)
	usable := width - chrome
	nStrip := cols - nPrim
	if nStrip*stripColumn >= usable {
		return nil, false
	}
	rem := usable - nStrip*stripColumn
	if rem/nPrim < minColumn {
		return nil, false
	}
	base, leftover := rem/nPrim, rem%nPrim
	out := make([]int, cols)
	firstPrim := -1
	for i, p := range primary {
		if p {
			out[i] = base
			if firstPrim < 0 {
				firstPrim = i
			}
			continue
		}
		out[i] = stripColumn
	}
	if firstPrim >= 0 {
		out[firstPrim] += leftover
	}
	return out, true
}

// widthAt is the usable text width of drawn column idx.
func (l Layout) widthAt(idx int) int {
	if len(l.ColWidths) == l.Cols && idx >= 0 && idx < len(l.ColWidths) {
		return l.ColWidths[idx]
	}
	return l.ColWidth + l.extraFor(idx)
}

// extraFor returns leftover cells for equal frames only. Weighted frames fold
// the remainder into the first primary column inside weightedWidths.
func (l Layout) extraFor(idx int) int {
	if len(l.ColWidths) == l.Cols {
		return 0
	}
	if l.Tier != TierColumns || l.Cols == 0 {
		return 0
	}
	chrome := 2*framePad + (l.Cols-1)*(1+2*gutter)
	rem := (l.Width - chrome) - l.Cols*l.ColWidth
	if idx == 0 {
		return rem
	}
	return 0
}

// wrap breaks text to a display width, measured with lipgloss.Width rather than
// len() because vendor output is arbitrary text.
//
// It breaks on spaces where it can and mid-word where it must — a URL or a Go
// identifier longer than the column has to break somewhere, and dropping it or
// letting it run past the edge are both worse than a hard break. Existing
// newlines are honoured: they are the vendor's paragraphing, and reflowing them
// away would misrepresent the reply.
//
// Contract: every returned line is at most w cells wide, for w >= 2. At w == 1
// a double-width rune cannot be represented at all — there is no correct
// answer, only a choice between dropping content and overflowing — so wrap
// keeps the rune and the caller's padRight truncates it. That path is
// unreachable in a real frame: minColumn is 24, and the tier drops to tabs
// rather than seat a column below it. The frame-level guarantee is asserted
// where it actually matters, over whole rendered frames, in
// TestNoLineExceedsTheTerminalWidth.
func wrap(s string, w int) []string {
	if w <= 0 {
		return nil
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		if para == "" {
			out = append(out, "")
			continue
		}
		out = append(out, wrapLine(para, w)...)
	}
	return out
}

func wrapLine(s string, w int) []string {
	var out []string
	cur := ""
	curw := 0
	flush := func() {
		out = append(out, cur)
		cur, curw = "", 0
	}
	for _, word := range strings.Split(s, " ") {
		ww := lipgloss.Width(word)
		switch {
		case ww > w:
			// Longer than the column on its own. Emit what is buffered, then
			// hard-break the word across as many lines as it needs.
			if curw > 0 {
				flush()
			}
			for _, chunk := range hardBreak(word, w) {
				out = append(out, chunk)
			}
			// The last chunk stays open so a following short word can join it.
			if len(out) > 0 {
				cur = out[len(out)-1]
				curw = lipgloss.Width(cur)
				out = out[:len(out)-1]
			}
		case curw == 0:
			cur, curw = word, ww
		case curw+1+ww <= w:
			cur += " " + word
			curw += 1 + ww
		default:
			flush()
			cur, curw = word, ww
		}
	}
	if curw > 0 || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}

func hardBreak(s string, w int) []string {
	var out []string
	cur := strings.Builder{}
	curw := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if curw+rw > w {
			out = append(out, cur.String())
			cur.Reset()
			curw = 0
		}
		cur.WriteRune(r)
		curw += rw
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// padRight left-aligns plain text in a fixed cell.
//
// This and its neighbours are duplicated from internal/hud rather than shared.
// The seam between the two surfaces is the normalized session model and
// internal/theme's numbers, and nothing else (cmd/telltale/main.go); exporting
// the HUD's layout internals to reach them here would create exactly the
// coupling that seam exists to prevent, for eighty lines.
func padRight(s string, w int, g Glyphs) string {
	if w <= 0 {
		return ""
	}
	s = truncate(s, w, g.Ellipsis)
	if d := w - lipgloss.Width(s); d > 0 {
		s += strings.Repeat(" ", d)
	}
	return s
}

// fit pads or truncates a possibly-STYLED string to exactly w cells.
//
// padRight below may only be used on plain text: it truncates rune by rune, so
// on a string that already carries ANSI escapes it would cut through an escape
// sequence, and its width arithmetic would count the escape bytes as content.
// That failure is invisible to the golden tests, which render with PlainStyles
// by design — so anywhere a line is assembled from differently-styled pieces
// (the tab bar, the help body), the padding has to be ANSI-aware, and this is
// it. lipgloss.Width and MaxWidth both skip escapes.
func fit(s string, w int) string {
	return fitOn(s, w, lipgloss.NewStyle())
}

// fitOn is fit with the PADDING's own style named by the caller.
//
// One row in the room needs it: the posture rail (style.go's RailGround), whose
// ground has to run the whole width of a cell rather than stopping where the
// badges stop. fit's plain spaces would leave the empty half of a posture row
// unpainted, which is the difference between a printed line with a gap in it and
// a line that simply ran out — and that difference is the whole reason the rail
// exists.
//
// The padding is the only thing styled. Whatever was already rendered into `s`
// passes through untouched, so this stays as ANSI-safe as fit is: the arithmetic
// is lipgloss.Width and MaxWidth throughout, both of which skip escapes. Under
// PlainStyles the caller hands in the identity style and the bytes are fit's own.
func fitOn(s string, w int, pad lipgloss.Style) string {
	if w <= 0 {
		return ""
	}
	s = lipgloss.NewStyle().MaxWidth(w).Render(s)
	if d := w - lipgloss.Width(s); d > 0 {
		s += pad.Render(strings.Repeat(" ", d))
	}
	return s
}

// truncate cuts a string to a display width, appending the ellipsis glyph.
func truncate(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ell)
	if ew >= w {
		return string([]rune(ell)[:1])
	}
	budget := w - ew
	var b strings.Builder
	used := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > budget {
			break
		}
		b.WriteRune(r)
		used += rw
	}
	return b.String() + ell
}

// elideLeft trims a path from the LEFT, where the uninformative part lives.
func elideLeft(s string, w int, ell string) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	ew := lipgloss.Width(ell)
	if ew >= w {
		return string([]rune(ell)[:1])
	}
	budget := w - ew
	runes := []rune(s)
	used := 0
	i := len(runes)
	for i > 0 {
		rw := lipgloss.Width(string(runes[i-1]))
		if used+rw > budget {
			break
		}
		used += rw
		i--
	}
	return ell + string(runes[i:])
}
