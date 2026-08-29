package council

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The room's arena record (design.md §9.47).
//
// `/arena record` answers one question the room could not: over every race this
// repository still holds, which seat did the operator actually take? §9.37 built
// the race, the ranks and the two lifecycle verbs, and then discarded the
// comparison the moment the turn ended — Column.Arena is a per-turn fact the next
// dispatch clears, and TurnRecord never carried it. So a room that had raced
// fourteen times could not say who had won one of them.
//
// NOTHING HERE IS STORED, and that is the whole design. The record is READ from
// the repository's own refs on every open:
//
//   - `arena/t<N>/<vendor>` is minted by arenaSetup for every racing seat, and
//     it is the same ref arenaRaceNumber already numbers a race against — "the
//     refs are the one record that shares the leftovers' lifetime" (§9.37's
//     renumbering amendment) is the sentence this whole file rests on.
//   - `adopt/t<N>-<vendor>` is minted by adoptSeat, on the operator's own `y`,
//     and only on an adoption that LANDED — undoAdoptBranch deletes the branch a
//     failed adoption cut, so a surviving adopt ref is a merge that happened.
//
// The alternative was a counts file under ~/.telltale. It was rejected: CLAUDE.md
// enumerates the writes the gauges are ratified to make, a fourth one is an
// owner-level contract edit rather than a quiet addition, and a tally derived from
// refs the repo already holds needs no such grant. The cost of deriving is stated
// on the page rather than hidden — see recordWindow.
//
// WHAT THE REFS CAN AND CANNOT SAY is the honesty boundary of this feature, and
// the page states both halves:
//
//   - They CAN say who entered a race and whom the operator adopted from it.
//   - They CANNOT say a rank, a phase word, or that a seat was cut with `x`.
//     Those are turn-scoped and die with the room. So this surface never claims a
//     LOSS: a seat that entered a race the operator never decided is counted as
//     undecided and reported beside the rate, never inside it. A race with a
//     give-up is exactly that case, and folding it into a denominator would be
//     the room scoring a seat for a race nobody judged.
//   - A dropped branch leaves the record. `/arena drop` deletes an arena ref by
//     design (kept-until-deleted cuts both ways), so the window this page counts
//     over is the refs that are still there — said out loud on the page, in the
//     act-ledger's own shape: a bounded claim states its bound.

// SeatRecord is one seat's standing over the races the refs still hold.
//
// Three counts rather than a won/lost pair, because "lost" is a fact none of
// these refs record. Entered is what the refs show the seat racing; Judged is the
// subset of those the operator decided by adopting SOMEBODY; Adopted is the
// subset of Judged the operator decided in this seat's favour. Adopted <= Judged
// <= Entered holds by construction, which is what keeps a rate off the far side
// of 100% when a winner's arena branch is dropped and its adopt branch survives —
// an adopt ref is itself evidence that seat entered that race.
type SeatRecord struct {
	Vendor model.VendorID
	Label  string

	Entered int
	Judged  int
	Adopted int
}

// Undecided is the races this seat entered that the operator never decided.
// Reported beside the rate and never inside it (see the file's header).
func (s SeatRecord) Undecided() int { return s.Entered - s.Judged }

// Rate is the seat's adoption rate over the DECIDED races it entered, as a
// percentage, and ok is false when there is no denominator to divide by.
//
// Integer arithmetic, rounded half up. No float reaches this: the figure is a
// ratio of two counts the page prints beside it, and the reader can check the
// division — which is the only reason a percentage is allowed here at all
// (§4a.1's rule that a displayed value comes from measured output, met by the
// carve-out for telltale's arithmetic on telltale's own observations, §7.12).
func (s SeatRecord) Rate() (pct int, ok bool) {
	if s.Judged <= 0 {
		return 0, false
	}
	return (s.Adopted*100 + s.Judged/2) / s.Judged, true
}

// ArenaRecord is the whole page's data: one read of one repository's arena and
// adopt refs, resolved into per-seat counts.
//
// Computed in the command handler (Update) and parked on State, exactly as
// ArenaResult is computed in finishColumn — so Render stays pure over State and
// no golden depends on a git call (CLAUDE.md's Render rule).
type ArenaRecord struct {
	// Repo is the workspace's base name, for the page's own heading. The full
	// path is already in the header; repeating it here would spend the widest
	// line in the room on a fact the room states above it.
	Repo string

	// Err is git's own first line when the refs could not be read at all — a
	// workspace that is not a repository, a git that will not run. A record that
	// could not be read renders as unavailable and never as an empty one: that
	// collapse is the degraded-vs-zero bug §4a.1 exists to prevent.
	Err string

	// Races is every race number the refs still hold, ascending and deduplicated.
	// It is the WINDOW: the page says how many races it counted and which numbers
	// they run between, because a total without its window is a claim with an
	// unstated bound (§7.15's precedent, and the act ledger's retention line).
	Races []int

	// Decided is how many of those races the operator resolved by adopting some
	// seat. Races minus Decided is what nobody judged.
	Decided int

	// Seats is one entry per addressable vendor, in seating order — including the
	// seats that never raced, which is the whole reason this is not built from
	// the room's live columns. A vendor that is not installed today may still
	// have raced last week, and a seat that has never raced is ABSENT from the
	// record rather than sitting at 0% (§4a.1, the founding rule).
	Seats []SeatRecord
}

// Raced reports that the refs hold at least one race. False is the page's
// absence state, and it is not the same as every seat reading zero.
func (r ArenaRecord) Raced() bool { return len(r.Races) > 0 }

// Undecided is the races nobody was adopted from.
func (r ArenaRecord) Undecided() int { return len(r.Races) - r.Decided }

// readArenaRecord reads one repository's arena and adopt refs and tallies them.
//
// TWO `for-each-ref` scans and nothing else — the same call arenaRaceNumber and
// freeAdoptBranch already make, over the same two namespaces, through the same
// gitOut argv. Nothing is written, nothing is spawned, and the room's posture is
// not consulted: reading the operator's own refs is the operator acting, on the
// footing /adopt and /arena drop already run at.
//
// The ARENA scan decides whether the record can be read at all. An adopt scan
// that fails on its own is not fatal and not silently zero either: an empty
// adopt namespace is the ordinary state of a repo that has raced and adopted
// nothing, so a failed scan that rendered as one would turn a broken read into a
// measured zero — the exact collapse this repo exists to prevent. It carries
// git's line onto the page instead.
func readArenaRecord(workspace string) ArenaRecord {
	rec := ArenaRecord{Repo: baseName(workspace)}
	arena, err := gitOut(workspace, "for-each-ref", "--format=%(refname:short)", "refs/heads/arena/")
	if err != nil {
		rec.Err = err.Error()
		return rec
	}
	adopt, err := gitOut(workspace, "for-each-ref", "--format=%(refname:short)", "refs/heads/adopt/")
	if err != nil {
		rec.Err = err.Error()
		return rec
	}
	return tallyArenaRefs(rec.Repo, splitRefs(arena), splitRefs(adopt))
}

// splitRefs is one for-each-ref output as a list of names, empties dropped.
func splitRefs(out string) []string {
	var refs []string
	for _, r := range strings.Split(out, "\n") {
		if r = strings.TrimSpace(r); r != "" {
			refs = append(refs, r)
		}
	}
	return refs
}

// tallyArenaRefs turns two lists of ref names into the record. PURE, and
// deliberately: every rule about what counts is decided here, where a test can
// hand it a list of strings and no repository at all.
//
// A ref this room's own arena did not mint is IGNORED, not guessed at. dropRacer
// makes the same judgement about paths — "no state this room's arena created can
// have that name" — and it costs the same thing here: a hand-cut receipt like
// `adopt/t9-claude-helpers` (the one the first live adopt left behind, before the
// 2026-08-11 fresh-branch ruling gave the verb a spelling of its own) does not
// count. That is an undercount, and it is the honest direction to be wrong in: a
// looser parse would credit a seat for a branch somebody named after it.
func tallyArenaRefs(repo string, arenaRefs, adoptRefs []string) ArenaRecord {
	rec := ArenaRecord{Repo: repo}

	// entered[vendor] and won[vendor] are sets of race numbers, because one race
	// mints one arena branch per seat and MAY mint several adopt branches for one
	// seat (freeAdoptBranch's -2, -3 suffixes, cut when an operator adopts, reverts
	// and adopts again). Counting refs would score that seat twice for one race.
	entered := map[model.VendorID]map[int]bool{}
	won := map[model.VendorID]map[int]bool{}
	races := map[int]bool{}
	decided := map[int]bool{}

	mark := func(m map[model.VendorID]map[int]bool, v model.VendorID, n int) {
		if m[v] == nil {
			m[v] = map[int]bool{}
		}
		m[v][n] = true
	}

	for _, ref := range arenaRefs {
		n, v, ok := parseArenaRef(ref)
		if !ok {
			continue
		}
		races[n] = true
		mark(entered, v, n)
	}
	for _, ref := range adoptRefs {
		n, v, ok := parseAdoptRef(ref)
		if !ok {
			continue
		}
		races[n] = true
		decided[n] = true
		// An adopt ref is evidence the seat ENTERED that race as well as won it.
		// Without this the winner's dropped arena branch would leave a rate above
		// 100%, and the flow that produces it is the documented one: adoptSeat's
		// own notice offers `/arena drop <seat>` as the next command.
		mark(entered, v, n)
		mark(won, v, n)
	}

	rec.Races = sortedKeys(races)
	rec.Decided = len(decided)
	for _, v := range addressableVendors() {
		s := SeatRecord{Vendor: v, Label: vendorLabel(v)}
		for n := range entered[v] {
			s.Entered++
			if decided[n] {
				s.Judged++
			}
		}
		s.Adopted = len(won[v])
		rec.Seats = append(rec.Seats, s)
	}
	return rec
}

// parseArenaRef reads `arena/t<N>/<vendor>` — arenaBranch's own spelling, read
// back. Anything else is not a ref this room minted.
func parseArenaRef(ref string) (n int, v model.VendorID, ok bool) {
	rest, found := strings.CutPrefix(ref, "arena/t")
	if !found {
		return 0, "", false
	}
	num, seat, found := strings.Cut(rest, "/")
	if !found {
		return 0, "", false
	}
	n, ok = raceNumber(num)
	if !ok {
		return 0, "", false
	}
	v, ok = knownVendor(seat)
	return n, v, ok
}

// parseAdoptRef reads `adopt/t<N>-<vendor>` and freeAdoptBranch's collision
// forms, `adopt/t<N>-<vendor>-<k>`.
//
// The suffix is stripped from the RIGHT and only when it is a number, which is
// unambiguous because no vendor id carries a dash (model.VendorID's five
// constants). A ref whose tail is a word — the hand-cut `adopt/t9-claude-helpers`
// — falls through to the vendor check and is refused there, which is the intent:
// this counts the receipts council minted, not the ones that mention a seat.
func parseAdoptRef(ref string) (n int, v model.VendorID, ok bool) {
	rest, found := strings.CutPrefix(ref, "adopt/t")
	if !found {
		return 0, "", false
	}
	num, seat, found := strings.Cut(rest, "-")
	if !found {
		return 0, "", false
	}
	n, ok = raceNumber(num)
	if !ok {
		return 0, "", false
	}
	if head, tail, cut := lastDash(seat); cut && allDigits(tail) {
		seat = head
	}
	v, ok = knownVendor(seat)
	return n, v, ok
}

func lastDash(s string) (head, tail string, ok bool) {
	i := strings.LastIndex(s, "-")
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// raceNumber parses the t<N> half. Positive integers only: arenaSetup floors the
// number at the room's turn counter, which starts at 1, so a `t0` or a `t-3` is
// not a name this room can have produced.
func raceNumber(s string) (int, bool) {
	if !allDigits(s) {
		return 0, false
	}
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
		if n > 1<<20 {
			// A ref whose number cannot be a race number is a ref this room did
			// not mint; refusing it beats overflowing on it.
			return 0, false
		}
	}
	if n <= 0 {
		return 0, false
	}
	return n, true
}

// knownVendor accepts only the five addressable ids, spelled exactly as
// arenaBranch spells them.
func knownVendor(s string) (model.VendorID, bool) {
	for _, v := range addressableVendors() {
		if s == string(v) {
			return v, true
		}
	}
	return "", false
}

// vendorLabel is a vendor's display name, read off the SAME table detection reads
// it from, so a seat's name on this page and its name on a column cannot drift.
// Falls back to the id, which is what every refusal in this package prints.
func vendorLabel(v model.VendorID) string {
	for _, c := range candidates() {
		if c.vendor == v {
			return c.label
		}
	}
	return string(v)
}

func sortedKeys(m map[int]bool) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// Insertion sort: the input is at most one entry per race a repository has
	// ever kept, and a sort import for that is a dependency on a hot path that
	// does not exist.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// baseName is the last element of a path, with both separators recognised —
// filepath.Base alone answers by the host's rule, and a test that types a
// POSIX-shaped workspace must get the same answer on Windows.
func baseName(p string) string {
	p = strings.TrimRight(p, `/\`)
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// arenaRecordCommand is `/arena record`: read the refs and open the page.
//
// The read is synchronous, in Update, like /adopt's and /arena drop's own git
// calls — two `for-each-ref` scans over a local repository, on a keystroke the
// operator just pressed. It is NOT refused mid-turn, and that is the difference
// between this verb and the other two: they mutate worktrees a race is writing,
// while this one reads refs. A record read during a race is a measurement of a
// moment already past, which is the same thing every other reading in this room
// is.
//
// A second `/arena record` while the page is open re-reads and redraws. Closing
// is `t`, the key that already means "give me the grid back" from a full-frame
// body (§9.22), and the mode line says so.
func (m *Model) arenaRecordCommand() {
	rec := readArenaRecord(m.st.Workspace)
	m.st.Record = &rec
	m.st.Notice = "the arena record, read from this repo's own branches — t returns to the grid"
	m.setDraft("")
}

// closeRecord puts the grid back, and reports whether there was a record to
// close. Called by `t` before the by-turn projection, so one key is the way out
// of whichever full-frame body is open rather than two keys to remember.
func (m *Model) closeRecord() bool {
	if m.st.Record == nil {
		return false
	}
	m.st.Record = nil
	return true
}

// recordLines is the whole page, as a flat list of lines.
//
// The shape is the turn page's, drawn with the turn page's own functions
// (strongLabelRule for the heading, seatRule for each seat), and for §9.22's
// reason rather than for tidiness: two builders for one room's heading grammar
// drift, and the drift is invisible until someone reads both.
//
// THE WINDOW SENTENCE IS A LINE, NOT META. labelRuleIn drops a rule's meta whole
// when the width will not take it — correct for a count, wrong for the sentence
// that bounds the claim, which would then vanish exactly where the room has least
// room to make it. That is the act ledger's own ruling on its retention line
// (§9.22's 2026-08-17 amendment), and this page's claim is bounded the same way:
// by which branches are still in the repository.
func recordLines(rec ArenaRecord, w int, sty Styles, g Glyphs) []string {
	if rec.Err != "" {
		// Degraded, never an empty record. "The refs say nothing" and "the refs
		// could not be read" are different facts (§4a.1), and this page is where
		// collapsing them would be believed.
		return styleAll(wrap("the arena record is unavailable: "+rec.Err+
			" — /arena record reads this repository's own arena/ and adopt/ branches.", w), sty.Muted)
	}

	out := []string{strongLabelRule("arena record", recordMeta(rec), w, g.RuleHeavy, sty)}
	out = append(out, styleAll(wrap(recordWindow(rec), w), sty.Muted)...)
	if !rec.Raced() {
		// Absence of the whole record, stated once. Five seats each reading
		// "never raced" would be five true sentences standing in for the one true
		// sentence, which is that this repository has never kept a race.
		return out
	}
	for _, s := range rec.Seats {
		out = append(out, "", seatRule(s.Vendor, s.Label, seatStanding(s), w, sty, g))
	}
	return out
}

// recordMeta is the count that hangs off the heading: how many races the refs
// hold. Sheddable, because it is restated in full by the window line under it.
func recordMeta(rec ArenaRecord) string {
	if !rec.Raced() {
		return rec.Repo
	}
	return rec.Repo + "  " + itoa(len(rec.Races)) + " " + plural(len(rec.Races), "race")
}

// recordWindow is the sentence that bounds every number on this page: where the
// counts came from, how many races they cover, and what has left the record.
//
// It says "still holds" rather than "has run", and the difference is the whole
// honesty of the surface. /arena drop deletes an arena branch by design
// (kept-until-deleted cuts both ways, §9.37), so a race whose branches were
// dropped is not in these counts and cannot be — a page claiming to know how many
// races a repository has ever run would be claiming a record nothing keeps.
func recordWindow(rec ArenaRecord) string {
	if !rec.Raced() {
		return "no arena branch is left in " + rec.Repo +
			" — /arena <brief> races the seats, and the branches a race leaves are this record."
	}
	span := "t" + itoa(rec.Races[0])
	if last := rec.Races[len(rec.Races)-1]; last != rec.Races[0] {
		span += " through t" + itoa(last)
	}
	s := "read from the arena/ and adopt/ branches this repository still holds, " + span +
		": " + itoa(rec.Decided) + " decided by you"
	if u := rec.Undecided(); u > 0 {
		s += ", " + itoa(u) + " nobody was adopted from"
	}
	return s + ". A race whose branches were dropped is no longer in the record."
}

// seatStanding is one seat's numbers, as its rule states them.
//
// THREE RENDERS FOR THREE FACTS, and keeping them apart is the point of the whole
// feature:
//
//   - A seat with no ref at all has NEVER RACED. That is absence, and it is not
//     0% — §4a.1's founding rule, on the surface where a zero would be read as a
//     verdict about the vendor.
//   - A seat that raced only into races nobody was adopted from has no rate to
//     state. Its races are reported as undecided; inventing a denominator out of
//     them would score a seat for races the operator never judged, which is
//     exactly what a race with a give-up or a cut seat leaves behind.
//   - A seat the operator decided against is a MEASURED zero and says so:
//     `0 of 4 adopted  0%`.
//
// THE RATE NEVER APPEARS WITHOUT ITS COUNT, and in that order — the fraction
// first, the percentage second. The counts are what was measured; the percentage
// is telltale's arithmetic on telltale's own observations, which §7.12 names as
// the one kind of computed figure this product may show, and which is only
// checkable because the two numbers it divides are printed beside it.
func seatStanding(s SeatRecord) string {
	if s.Entered == 0 {
		return "never raced"
	}
	var parts []string
	if pct, ok := s.Rate(); ok {
		parts = append(parts, itoa(s.Adopted)+" of "+itoa(s.Judged)+" adopted", itoa(pct)+"%")
	} else {
		parts = append(parts, "no decided race")
	}
	if u := s.Undecided(); u > 0 {
		parts = append(parts, itoa(u)+" undecided")
	}
	// The room's one grammar for the numbers that belong to a label: two spaces,
	// never a middle dot (historyMeta, seatMeta).
	return strings.Join(parts, "  ")
}

// YankRecord copies the record the page is showing.
//
// It exists for the reason the page's own `y` exists (§9.22): on a full-frame
// body the copy key must take the thing in front of the reader, and a `y` that
// quietly took the focused column's reply while a record was on screen would
// break that claim into a file. The mode line names the key here for the same
// reason it names it there.
//
// THE WINDOW SENTENCE TRAVELS WITH THE NUMBERS, and here it matters more than it
// does on screen. A reader looking at the page can re-check the bound by reading
// the line under the heading; a table pasted into a pull request a week later has
// nothing else saying these counts were ever bounded by which branches survived.
// That is the act ledger's ruling on its retention line, applied to the one claim
// this page makes.
func (s State) YankRecord() Yank {
	if s.Record == nil {
		return Yank{Notice: "nothing to copy — the arena record is not open"}
	}
	rec := *s.Record
	if rec.Err != "" {
		return Yank{Notice: "nothing to copy — the arena record could not be read: " + rec.Err}
	}

	var b strings.Builder
	b.WriteString("# arena record — " + rec.Repo + "\n\n" + recordWindow(rec) + "\n")
	if rec.Raced() {
		for _, seat := range rec.Seats {
			b.WriteString("\n- " + seat.Label + ": " + seatStanding(seat))
		}
		b.WriteString("\n")
	}
	notice := "copied the arena record — " + itoa(len(rec.Races)) + " " + plural(len(rec.Races), "race")
	return Yank{Text: b.String(), Notice: notice}
}

// recordCell renders the record to exactly h lines of exactly w cells.
//
// It carries pageCell's two contracts and nothing else it does not need. The
// bottom anchor is the same: spare rows go ABOVE the document, so what the room
// says sits against the composer (§7.1). The overflow marker is the same too, and
// it is why this page has no scroll keys — the record is one short line per seat,
// so the only geometry that can clip it is the height floor, and there the honest
// answer is the marker rather than a motion the mode line would have to promise.
func recordCell(rec ArenaRecord, w, h int, sty Styles, g Glyphs) []string {
	body := recordLines(rec, w, sty, g)
	if len(body) > h && h > 0 {
		// Never a silent cut. Everywhere else in this room content that does not
		// fit spends a row saying how much is missing (columnCell, helpRows), on
		// the argument that silent clipping is indistinguishable from there being
		// nothing more to say.
		keep := h - 1
		cut := append([]string{}, body[:keep]...)
		body = append(cut, sty.Muted.Render(
			overflowMarker(g.Down, len(body)-keep, "below", "", nil, w, g)))
	}

	blank := strings.Repeat(" ", maxInt(0, w))
	lines := make([]string, 0, h)
	for pad := h - len(body); pad > 0; pad-- {
		lines = append(lines, blank)
	}
	for _, l := range body {
		// fit, not padRight — the ANSI trap (§9.5). Every line here carries a
		// style, and a seat rule carries that seat's own hue.
		lines = append(lines, fit(l, w))
	}
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return lines[:h]
}

// recordBody frames the record into the body area, with pageBody's one pad each
// side — it is the same single-column geometry and there is no second layout path.
func recordBody(st State, lay Layout, sty Styles, g Glyphs) string {
	cell := recordCell(*st.Record, lay.ColWidth, lay.Body, sty, g)
	var b strings.Builder
	for i, l := range cell {
		b.WriteString(framePadStr + l + framePadStr)
		if i < len(cell)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}
