package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The arena record (§9.47). Nothing here spawns a vendor and nothing here writes
// state: the tally is a pure function over two lists of ref names, and the one
// test that touches git builds its refs in a temp repository.

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
