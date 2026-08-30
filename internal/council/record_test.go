package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The arena record (§9.47). Nothing here spawns a vendor and nothing here writes
// state: the tally is a pure function over two lists of ref names, and the one
// test that touches git builds its refs in a temp repository.

// hybridFixture is the record a repository holds once a HYBRID adoption has run
// (§9.37's 2026-08-29 hybrid amendment). Its own fixture and its own golden,
// rather than more rows on recordFixture, for the reason the golden files are one
// per named scenario: this pins what a hybrid does to the page, and mixing it in
// would make one golden move whenever either feature moves.
//
// Read the refs, not the counts:
//
//	t2  claude codex agy   adopted: claude, whole
//	t5  claude codex       adopted: claude + paths from codex (a hybrid)
//	t6  codex agy          adopted: agy + paths from codex (a hybrid)
//
// claude therefore has one whole adoption and one hybrid; codex has no whole
// adoption at all and two hybrids; agy has one whole loss and one hybrid.
func hybridFixture() ArenaRecord {
	return tallyArenaRefs("telltale",
		[]string{
			"arena/t2/claude", "arena/t2/codex", "arena/t2/agy",
			"arena/t5/claude", "arena/t5/codex",
			"arena/t6/codex", "arena/t6/agy",
		},
		[]string{
			"adopt/t2-claude",
			"adopt/t5-claude+codex",
			"adopt/t6-agy+codex",
		})
}

// recordFixture is a record with every rendering state in it at once, so one
// golden pins the whole vocabulary rather than four goldens pinning a word each.
//
// Read the refs it is built from, not the counts: the counts are what the tally
// is supposed to produce, and typing them here instead would make this fixture a
// second implementation that cannot disagree with the first.
//
//	t2  claude codex agy      adopted: claude
//	t3  claude codex          adopted: nobody
//	t4  claude codex cursor   adopted: codex
//	t7  cursor               adopted: nobody
//	t9  (arena branches dropped) adopted: claude, twice (a re-adoption)
//
// grok has no ref anywhere, which is the seat that must render ABSENT.
func recordFixture() ArenaRecord {
	return tallyArenaRefs("telltale",
		[]string{
			"arena/t2/claude", "arena/t2/codex", "arena/t2/agy",
			"arena/t3/claude", "arena/t3/codex",
			"arena/t4/claude", "arena/t4/codex", "arena/t4/cursor",
			"arena/t7/cursor",
		},
		[]string{
			"adopt/t2-claude",
			"adopt/t4-codex",
			"adopt/t9-claude", "adopt/t9-claude-2",
		})
}

// TestTheRecordCountsRacesAndNotRefs is the tally's arithmetic, pinned.
//
// The three properties that are easy to get wrong and invisible once wrong: one
// race counts once however many refs it left, an adopt ref proves the seat
// ENTERED that race (t9's arena branches are gone and claude still raced it), and
// a race nobody was adopted from raises Undecided rather than a denominator.
func TestTheRecordCountsRacesAndNotRefs(t *testing.T) {
	rec := recordFixture()

	if got, want := len(rec.Races), 5; got != want {
		t.Errorf("races = %d, want %d (%v)", got, want, rec.Races)
	}
	if got, want := rec.Decided, 3; got != want {
		t.Errorf("decided = %d, want %d", got, want)
	}
	if got, want := rec.Undecided(), 2; got != want {
		t.Errorf("undecided races = %d, want %d", got, want)
	}

	want := map[model.VendorID]SeatRecord{
		// t2, t3, t4 by arena ref and t9 by adopt ref; t3 was never decided.
		// Adopted is 1 for t2 and 1 for t9 — the -2 suffix is the SAME race.
		model.VendorClaude: {Entered: 4, Judged: 3, Adopted: 2},
		model.VendorCodex:  {Entered: 3, Judged: 2, Adopted: 1},
		// Raced once, into a decided race, and was not taken: a measured zero.
		model.VendorAntigravity: {Entered: 1, Judged: 1, Adopted: 0},
		// t4 was decided, t7 was not.
		model.VendorCursor: {Entered: 2, Judged: 1, Adopted: 0},
		// No ref anywhere.
		model.VendorGrok: {Entered: 0, Judged: 0, Adopted: 0},
	}
	for _, s := range rec.Seats {
		w := want[s.Vendor]
		if s.Entered != w.Entered || s.Judged != w.Judged || s.Adopted != w.Adopted {
			t.Errorf("%s: entered/judged/adopted = %d/%d/%d, want %d/%d/%d",
				s.Vendor, s.Entered, s.Judged, s.Adopted, w.Entered, w.Judged, w.Adopted)
		}
		if s.Adopted > s.Judged || s.Judged > s.Entered {
			t.Errorf("%s: adopted <= judged <= entered does not hold: %d/%d/%d",
				s.Vendor, s.Adopted, s.Judged, s.Entered)
		}
	}
}

// TestTheRecordIgnoresRefsThisRoomDidNotMint is dropRacer's judgement about
// paths, applied to refs: a branch that merely mentions a seat is not a receipt.
//
// `adopt/t9-claude-helpers` is the real one — the first live adoption left it
// behind, hand-cut, before the 2026-08-11 ruling gave the verb a spelling of its
// own — and counting it would credit claude for a branch a person named. The
// undercount is the honest direction to be wrong in, and it is stated in the
// design section rather than hidden here.
func TestTheRecordIgnoresRefsThisRoomDidNotMint(t *testing.T) {
	rec := tallyArenaRefs("repo",
		[]string{
			"arena/t1/claude", // the only ref that counts
			"arena/tx/claude", // not a number
			"arena/t0/claude", // races are numbered from 1
			"arena/t2/notaseat",
			"arena/t2",        // no seat segment
			"arena-t2/claude", // not the namespace
		},
		[]string{
			"adopt/t9-claude-helpers", // hand-cut, pre-ruling
			"adopt/t9-notaseat",
			"adopt/claude",
		})

	if got, want := len(rec.Races), 1; got != want {
		t.Fatalf("races = %d, want %d (%v)", got, want, rec.Races)
	}
	if rec.Decided != 0 {
		t.Errorf("decided = %d, want 0 — no adopt ref here is one this room minted", rec.Decided)
	}
	for _, s := range rec.Seats {
		want := 0
		if s.Vendor == model.VendorClaude {
			want = 1
		}
		if s.Entered != want {
			t.Errorf("%s entered %d races, want %d", s.Vendor, s.Entered, want)
		}
	}
}

// TestTheRecordKeepsZeroAndAbsentApart is §4a.1's founding rule on this surface,
// and it is the whole reason the feature is allowed to exist.
//
// A seat the operator decided against is a measured zero and prints one. A seat
// with no ref at all has never raced and prints NO RATE — because "this vendor
// wins nothing" and "this vendor has never been asked" are different claims about
// a product, and the second one rendered as 0% is a verdict the room invented.
func TestTheRecordKeepsZeroAndAbsentApart(t *testing.T) {
	byVendor := map[model.VendorID]SeatRecord{}
	for _, s := range recordFixture().Seats {
		byVendor[s.Vendor] = s
	}

	zero := seatStanding(byVendor[model.VendorAntigravity])
	if !strings.Contains(zero, "0 of 1 adopted") || !strings.Contains(zero, "0%") {
		t.Errorf("a measured zero must print its zero: %q", zero)
	}

	absent := seatStanding(byVendor[model.VendorGrok])
	if absent != "never raced" {
		t.Errorf("a seat with no ref must render absent, got %q", absent)
	}
	if strings.Contains(absent, "%") || strings.Contains(absent, "0") {
		t.Errorf("absence must not render as a rate: %q", absent)
	}
}

// TestTheRecordNeverPrintsARateWithoutItsCount walks every seat state this
// surface can reach and asserts the pairing directly.
//
// The percentage is telltale's arithmetic on telltale's own observations, which
// §7.12 names as the one computed figure this product may show — and the carve-out
// is conditional on the reader being able to check the division. A bare `43%`
// would be a claim with its evidence removed.
func TestTheRecordNeverPrintsARateWithoutItsCount(t *testing.T) {
	for _, s := range recordFixture().Seats {
		line := seatStanding(s)
		if !strings.Contains(line, "%") {
			continue
		}
		if !strings.Contains(line, " of ") || !strings.Contains(line, "adopted") {
			t.Errorf("%s: a rate with no count beside it: %q", s.Vendor, line)
		}
	}
}

// TestAnUndecidedRaceIsNeverALoss. A race whose seats were cut with `x`, or that
// the operator simply walked away from, leaves no adopt ref — and the refs cannot
// tell those two apart from a race nobody liked. So none of them may reach a
// denominator: the seat's rate is over the races the operator DECIDED, and the
// rest are counted beside it in words.
func TestAnUndecidedRaceIsNeverALoss(t *testing.T) {
	// One seat, two races, neither decided.
	rec := tallyArenaRefs("repo",
		[]string{"arena/t1/claude", "arena/t2/claude"}, nil)
	s := rec.Seats[0]
	if s.Vendor != model.VendorClaude {
		t.Fatalf("seating order changed: %s", s.Vendor)
	}
	if _, ok := s.Rate(); ok {
		t.Error("a seat with no decided race must have no rate at all")
	}
	if got := seatStanding(s); got != "no decided race  2 undecided" {
		t.Errorf("standing = %q", got)
	}

	// Decide one of them, for the other seat. claude's denominator becomes 1,
	// not 2 — the undecided race stays outside it.
	rec = tallyArenaRefs("repo",
		[]string{"arena/t1/claude", "arena/t1/codex", "arena/t2/claude"},
		[]string{"adopt/t1-codex"})
	s = rec.Seats[0]
	if s.Judged != 1 || s.Adopted != 0 || s.Undecided() != 1 {
		t.Fatalf("judged/adopted/undecided = %d/%d/%d, want 1/0/1", s.Judged, s.Adopted, s.Undecided())
	}
	if got := seatStanding(s); got != "0 of 1 adopted  0%  1 undecided" {
		t.Errorf("standing = %q", got)
	}
}

// TestTheRecordStatesTheWindowItCounted. Every number on this page is bounded by
// which branches the repository still holds, and the sentence that says so is a
// LINE rather than the rule's meta — labelRuleIn drops meta whole at a narrow
// width, and the bound would then vanish exactly where the room has least room to
// state it (the act ledger's own ruling on its retention line).
func TestTheRecordStatesTheWindowItCounted(t *testing.T) {
	rec := recordFixture()
	win := recordWindow(rec)
	for _, want := range []string{"still holds", "t2 through t9", "3 decided by you", "2 nobody was adopted from", "dropped"} {
		if !strings.Contains(win, want) {
			t.Errorf("the window sentence does not say %q:\n%s", want, win)
		}
	}

	// And it survives at the narrowest width the room draws, because it is a
	// wrapped line rather than a rule's meta.
	lines := recordLines(rec, MinWidth-4, PlainStyles(), GlyphsFor(false))
	if !strings.Contains(strings.Join(lines, " "), "still holds") {
		t.Error("the window sentence was dropped at the narrow width")
	}
}

// TestARecordThatCouldNotBeReadIsNotAnEmptyOne is the degraded-vs-zero rule on
// the one surface where collapsing them would be believed: "this repository has
// kept no race" and "git would not answer" are different facts.
func TestARecordThatCouldNotBeReadIsNotAnEmptyOne(t *testing.T) {
	bad := ArenaRecord{Repo: "repo", Err: "fatal: not a git repository"}
	got := strings.Join(recordLines(bad, 100, PlainStyles(), GlyphsFor(false)), " ")
	if !strings.Contains(got, "unavailable") || !strings.Contains(got, "not a git repository") {
		t.Errorf("an unreadable record must carry git's reason: %q", got)
	}

	empty := tallyArenaRefs("repo", nil, nil)
	got = strings.Join(recordLines(empty, 100, PlainStyles(), GlyphsFor(false)), " ")
	if strings.Contains(got, "unavailable") {
		t.Errorf("a repository with no races is not an unreadable one: %q", got)
	}
	if !strings.Contains(got, "no arena branch is left") {
		t.Errorf("a repository with no races must say so: %q", got)
	}
	if strings.Contains(got, "never raced") {
		t.Error("five seats each absent is not the true statement; the repository kept no race")
	}
}

// TestTheRecordPage is the frame, in both glyph sets.
func TestTheRecordPage(t *testing.T) {
	st := room()
	rec := recordFixture()
	st.Record = &rec
	golden(t, "arena-record", render(st))

	st.ASCII = true
	golden(t, "arena-record-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestTheRecordPageSaysHowToLeave. A full-frame body reached by a typed command
// must name its own way out, or it is the help panel's missing `?` with a whole
// surface behind it. The mode word tells it apart from the room's other two
// bodies (§7.8's always-on statement of what is on screen).
func TestTheRecordPageSaysHowToLeave(t *testing.T) {
	st := room()
	rec := recordFixture()
	st.Record = &rec
	frame := render(st)

	if !strings.Contains(lastLine(frame), "t grid") {
		t.Errorf("the way out is not on the mode line:\n%q", lastLine(frame))
	}
	if !strings.Contains(frame, "RECORD") {
		t.Error("the mode word does not name the body on screen")
	}
	// tab and f address columns, and this body has none. A footer that named
	// them would be the false promise §7.8 forbids.
	for _, absent := range []string{"tab focus", "f full"} {
		if strings.Contains(lastLine(frame), absent) {
			t.Errorf("the mode line promises %q, which does nothing here", absent)
		}
	}
}

// TestTGivesTheGridBackFromTheRecord: one key is the way back to the columns
// from whichever full-frame body is open. `t` closes the record without opening
// the turn page, because "grid" is what the cell it is named on promises.
func TestTGivesTheGridBackFromTheRecord(t *testing.T) {
	m := flowRoom(t, true)
	rec := recordFixture()
	m.st.Record = &rec

	m.toggleTurnView()

	if m.st.Record != nil {
		t.Error("t did not close the record")
	}
	if m.st.Page.Open {
		t.Error("t opened the turn page instead of giving the grid back")
	}
}

// TestYankRecordTakesWhatIsOnScreen. The copy key on a full-frame body must take
// the body, not the column hidden behind it — and the document carries the window
// sentence, because a table pasted into a review a week later has nothing else
// saying these counts were bounded.
func TestYankRecordTakesWhatIsOnScreen(t *testing.T) {
	st := room()
	rec := recordFixture()
	st.Record = &rec

	y := st.YankRecord()
	if !strings.Contains(y.Text, "arena record") {
		t.Errorf("the document is not the record:\n%s", y.Text)
	}
	if !strings.Contains(y.Text, "still holds") {
		t.Errorf("the window sentence did not travel with the numbers:\n%s", y.Text)
	}
	for _, seat := range rec.Seats {
		if !strings.Contains(y.Text, seat.Label+": "+seatStanding(seat)) {
			t.Errorf("%s is missing or disagrees with the screen:\n%s", seat.Label, y.Text)
		}
	}
	if !strings.Contains(y.Notice, "5 races") {
		t.Errorf("the notice does not say what was copied: %q", y.Notice)
	}

	// An unreadable record copies NOTHING rather than a document claiming an
	// empty repository.
	st.Record = &ArenaRecord{Repo: "repo", Err: "fatal: not a git repository"}
	if y := st.YankRecord(); y.Text != "" {
		t.Errorf("an unreadable record must not produce a document:\n%s", y.Text)
	}
}

// TestArenaRecordIsOnlyItsOwnWord. The vocabulary rule /arena drop keeps: only
// the exact form is the verb, and anything longer after /arena is a brief that
// races as prose.
//
// Measured in a READ-ONLY room, which is what makes the second half provable
// without spawning anything: a race there is refused by name, so a draft that
// reached the refusal is a draft the record verb did not take. The record itself
// still opens in that room, because reading refs is not writing.
func TestArenaRecordIsOnlyItsOwnWord(t *testing.T) {
	log := countSpawns(t)
	m := flowRoom(t, false)
	m.st.Workspace = gitRepo(t)

	m.setDraft("/arena record the wins")
	enter(m)
	if m.st.Record != nil {
		t.Error("a longer draft was taken as the record verb")
	}
	if !strings.Contains(m.st.Notice, "read-only") {
		t.Errorf("the longer draft did not reach the race path: %q", m.st.Notice)
	}

	m.setDraft("/arena record")
	enter(m)
	if m.st.Record == nil {
		t.Fatalf("the record verb did not open the page: %q", m.st.Notice)
	}
	if m.st.Record.Err != "" {
		t.Errorf("the record could not be read from a real repository: %q", m.st.Record.Err)
	}
	if m.st.Draft != "" {
		t.Errorf("the command left its own words in the composer: %q", m.st.Draft)
	}
	if log.n() != 0 {
		t.Errorf("%d processes were spawned by a read", log.n())
	}
}

// TestTheRecordReadsTheRefsARaceReallyLeaves closes the loop between the tally
// and the names arenaBranch and adoptBranch actually mint: the parser is written
// against those two functions, so it is asserted against them rather than against
// the strings this file happens to type.
func TestTheRecordReadsTheRefsARaceReallyLeaves(t *testing.T) {
	ws := gitRepo(t)
	base, err := gitOut(ws, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{
		arenaBranch(4, model.VendorClaude),
		arenaBranch(4, model.VendorCodex),
		adoptBranch(4, model.VendorCodex),
	} {
		if _, err := gitOut(ws, "branch", ref, base); err != nil {
			t.Fatalf("git branch %s: %v", ref, err)
		}
	}

	rec := readArenaRecord(ws)
	if rec.Err != "" {
		t.Fatalf("the record could not be read: %s", rec.Err)
	}
	if got, want := len(rec.Races), 1; got != want {
		t.Fatalf("races = %d, want %d (%v)", got, want, rec.Races)
	}
	if rec.Repo != "repo" {
		t.Errorf("repo = %q, want the workspace's own base name", rec.Repo)
	}
	for _, s := range rec.Seats {
		switch s.Vendor {
		case model.VendorClaude:
			if s.Entered != 1 || s.Adopted != 0 || s.Judged != 1 {
				t.Errorf("claude: %d/%d/%d, want 1/1/0 entered/judged/adopted", s.Entered, s.Judged, s.Adopted)
			}
		case model.VendorCodex:
			if s.Entered != 1 || s.Adopted != 1 {
				t.Errorf("codex: entered %d adopted %d, want 1 and 1", s.Entered, s.Adopted)
			}
		default:
			if s.Entered != 0 {
				t.Errorf("%s entered %d races in a repo it never raced in", s.Vendor, s.Entered)
			}
		}
	}
}

// TestAHybridAdoptIsItsOwnState, and it is the honesty question this feature had
// to answer before it could ship. The refs record that the operator DECIDED a
// race and that two seats were in the adoption; they cannot record which paths
// came from where, because that lives in a commit message this page never reads.
//
// So a hybrid is counted in three places and credited in none: the race is
// decided, both seats entered it, and neither seat's adopted-of-decided rate moves
// at all. Crediting the base seat would score it for work the donor wrote;
// counting it against both would score two seats down for a race the operator
// resolved in both their favour.
func TestAHybridAdoptIsItsOwnState(t *testing.T) {
	rec := hybridFixture()

	if got, want := len(rec.Races), 3; got != want {
		t.Fatalf("races = %d, want %d (%v)", got, want, rec.Races)
	}
	if got, want := rec.Decided, 3; got != want {
		t.Errorf("decided = %d, want %d — a hybrid is a race the operator decided", got, want)
	}
	if got, want := rec.Hybrid, 2; got != want {
		t.Errorf("hybrid races = %d, want %d", got, want)
	}
	if rec.Undecided() != 0 {
		t.Errorf("undecided = %d, want 0 — no race here was walked away from", rec.Undecided())
	}

	want := map[model.VendorID]SeatRecord{
		// t2 whole, t5 hybrid.
		model.VendorClaude: {Entered: 2, Judged: 1, Adopted: 1, Hybrid: 1},
		// Never adopted whole, and in both hybrids: no rate at all.
		model.VendorCodex: {Entered: 3, Judged: 1, Adopted: 0, Hybrid: 2},
		// t2 decided against it, t6 a hybrid it was the base of.
		model.VendorAntigravity: {Entered: 2, Judged: 1, Adopted: 0, Hybrid: 1},
		model.VendorCursor:      {},
		model.VendorGrok:        {},
	}
	for _, s := range rec.Seats {
		w := want[s.Vendor]
		if s.Entered != w.Entered || s.Judged != w.Judged || s.Adopted != w.Adopted || s.Hybrid != w.Hybrid {
			t.Errorf("%s: entered/judged/adopted/hybrid = %d/%d/%d/%d, want %d/%d/%d/%d",
				s.Vendor, s.Entered, s.Judged, s.Adopted, s.Hybrid,
				w.Entered, w.Judged, w.Adopted, w.Hybrid)
		}
		// The invariant the whole page rests on, with the fourth count added: a
		// seat's races are decided whole, decided by a hybrid, or undecided, and
		// they never add up to more than it entered.
		if s.Adopted > s.Judged || s.Judged+s.Hybrid > s.Entered || s.Undecided() < 0 {
			t.Errorf("%s: the counts do not partition its races: %d/%d/%d/%d",
				s.Vendor, s.Adopted, s.Judged, s.Hybrid, s.Entered)
		}
	}
}

// TestAHybridRefIsNeverReadAsAWholeAdoption. This is the failure the branch
// naming exists to prevent: `adopt/t5-claude+codex` read by the older parse would
// credit one seat with an adoption it only half won, or — worse — fall through
// every parse and report a decided race as one nobody was adopted from.
func TestAHybridRefIsNeverReadAsAWholeAdoption(t *testing.T) {
	if _, _, ok := parseAdoptRef("adopt/t5-claude+codex"); ok {
		t.Error("the whole-adoption parse accepted a hybrid ref")
	}
	n, base, donor, ok := parseHybridAdoptRef("adopt/t5-claude+codex")
	if !ok || n != 5 || base != model.VendorClaude || donor != model.VendorCodex {
		t.Errorf("parseHybridAdoptRef = %d %q %q %v, want 5 claude codex true", n, base, donor, ok)
	}
	// freeAdoptBranch's collision suffix reads back the same way it does on a
	// whole adoption, and the race is still t5.
	if n, base, donor, ok := parseHybridAdoptRef("adopt/t5-claude+codex-2"); !ok || n != 5 ||
		base != model.VendorClaude || donor != model.VendorCodex {
		t.Errorf("a suffixed hybrid ref = %d %q %q %v, want 5 claude codex true", n, base, donor, ok)
	}
	// A ref this room did not mint stays uncounted, in both parses.
	for _, ref := range []string{"adopt/t5-claude+notaseat", "adopt/t5-notaseat+codex", "adopt/t5-claude+"} {
		if _, _, _, ok := parseHybridAdoptRef(ref); ok {
			t.Errorf("%q was read as a hybrid receipt", ref)
		}
	}
	// hybridAdoptBranch and the parse are one spelling, asserted against each
	// other rather than against the string this test happens to type.
	ref := hybridAdoptBranch(9, model.VendorAntigravity, model.VendorGrok)
	if n, base, donor, ok := parseHybridAdoptRef(ref); !ok || n != 9 ||
		base != model.VendorAntigravity || donor != model.VendorGrok {
		t.Errorf("%s did not read back: %d %q %q %v", ref, n, base, donor, ok)
	}
}

// TestTheHybridPageSaysWhatNoSeatClaims. A reader adding up the seats' adopted
// columns on a page with hybrids in it comes out short of the decided count, and
// the window sentence is where that difference has to be — a bounded claim states
// its bound.
func TestTheHybridPageSaysWhatNoSeatClaims(t *testing.T) {
	rec := hybridFixture()
	window := recordWindow(rec)
	if !strings.Contains(window, "2 by a hybrid adopt, counted for no seat") {
		t.Errorf("the window sentence does not account for the hybrids: %q", window)
	}

	byVendor := map[model.VendorID]SeatRecord{}
	for _, s := range rec.Seats {
		byVendor[s.Vendor] = s
	}
	// A seat whose whole-adoption record and whose hybrids are BOTH real keeps
	// both, and keeps them apart: the rate covers the race it was judged in, and
	// the hybrids sit beside it. codex lost t2 outright and was taken from twice.
	codex := seatStanding(byVendor[model.VendorCodex])
	for _, want := range []string{"0 of 1 adopted", "0%", "part of 2 hybrid adopts"} {
		if !strings.Contains(codex, want) {
			t.Errorf("codex standing does not say %q: %q", want, codex)
		}
	}
	// A seat whose ONLY decided races were hybrids has no rate to state, and the
	// sentence says which fact that is. It is not `0%`: the operator did not
	// decide against this attempt, they took part of it.
	only := tallyArenaRefs("repo",
		[]string{"arena/t1/claude", "arena/t1/codex"},
		[]string{"adopt/t1-claude+codex"})
	for _, s := range only.Seats {
		if s.Vendor != model.VendorCodex {
			continue
		}
		line := seatStanding(s)
		if strings.Contains(line, "%") {
			t.Errorf("a seat with no decided whole race printed a rate: %q", line)
		}
		for _, want := range []string{"no attempt adopted whole", "part of 1 hybrid adopt"} {
			if !strings.Contains(line, want) {
				t.Errorf("a hybrid-only seat does not say %q: %q", want, line)
			}
		}
	}
	// A seat with both keeps its rate over the races it WAS judged in, with the
	// hybrid beside it and never inside it.
	claude := seatStanding(byVendor[model.VendorClaude])
	if !strings.Contains(claude, "1 of 1 adopted") || !strings.Contains(claude, "100%") {
		t.Errorf("claude lost the rate it earned whole: %q", claude)
	}
	if !strings.Contains(claude, "part of 1 hybrid adopt") {
		t.Errorf("claude standing hides the hybrid: %q", claude)
	}
	// And a seat with neither is untouched by any of this.
	if got := seatStanding(byVendor[model.VendorGrok]); got != "never raced" {
		t.Errorf("a seat with no ref = %q, want never raced", got)
	}
}

// TestTheHybridRecordPage is the frame, in both glyph sets.
func TestTheHybridRecordPage(t *testing.T) {
	st := room()
	rec := hybridFixture()
	st.Record = &rec
	golden(t, "arena-record-hybrid", render(st))

	st.ASCII = true
	golden(t, "arena-record-hybrid-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}
