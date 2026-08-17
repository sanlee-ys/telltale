package council

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// ledgered is a finished turn 3 holding every act shape the ledger has to draw
// at once: a seat with several calls covering succeeded / failed-with-a-reason /
// refused-at-the-gate, a seat whose vendor reported an ending without an outcome
// and a call it never resolved at all, and a seat that took the turn and recorded
// nothing.
//
// The turn-2 acts on the first seat are the control for the projection: a ledger
// for turn 3 that reached back into an earlier record would be the same defect
// §9.15 named for replies, with a command line in it.
func ledgered() State {
	st := room()
	st.Turn = 3

	c := &st.Columns[0]
	c.startTurn(2, "an older question", false)
	c.Acts = []Act{{ID: "old", Text: "Bash: git status", Status: runner.ActOK}}
	c.Body, c.Phase, c.Elapsed = "an older answer from Claude Code", PhaseDone, 4*time.Second
	c.startTurn(3, pageBrief, false)
	c.Acts = []Act{
		{ID: "a1", Text: "Bash: go test ./internal/council", Status: runner.ActOK},
		{ID: "a2", Text: "Write: internal/council/ledger.go", Status: runner.ActFailed,
			Detail: "permission denied"},
		{ID: "a3", Text: "Bash: git commit -m the room writes down what it did",
			Status: runner.ActDenied},
	}
	c.Body, c.Phase, c.Elapsed = "About 30K redundant input tokens per vendor.", PhaseDone, 41*time.Second
	cost := 0.0123
	c.CostUSD = &cost

	x := &st.Columns[1]
	x.startTurn(3, pageBrief, false)
	x.Acts = []Act{
		{ID: "b1", Text: "Read: docs/design.md", Status: runner.ActUnknown},
		{ID: "b2", Text: "Bash: go vet ./...", Status: runner.ActPending},
	}
	x.Body, x.Phase, x.Elapsed = "Native resume avoids it entirely.", PhaseDone, 9*time.Second

	// Took the turn, ran nothing. A seat with no block at all would read as the
	// ledger dropping it, and "this seat did nothing" is not what the room can
	// claim either — only that nothing was recorded.
	a := &st.Columns[2]
	a.startTurn(3, pageBrief, false)
	a.Body, a.Phase, a.Elapsed = "Agreed.", PhaseDone, 6*time.Second

	st.Page = TurnView{Open: true, Turn: 3, Ledger: true}
	return st
}

// TestTheActLedgerReadsOneTurnForWhatTheSeatsDid is the feature.
//
// The page answers "what did the seats say about turn 3"; this answers "what did
// they DO in it" — the same records, at a width where the outcome is a word
// rather than one mark in a 37-cell column.
func TestTheActLedgerReadsOneTurnForWhatTheSeatsDid(t *testing.T) {
	got := render(ledgered())
	golden(t, "act-ledger", got)

	for _, want := range []string{
		// The heading names the turn and the face, and the rule states where the
		// turn went and how long the list under it is.
		"acts in turn 3", "→ everyone", "5 acts",
		// Every seat that took the turn, under its own rule.
		"Claude Code", "Codex", "Antigravity",
		// The calls themselves, with the outcome the vendor reported for each.
		"Bash: go test ./internal/council", "Write: internal/council/ledger.go",
		"Read: docs/design.md", "Bash: go vet ./...",
		// And the seat that ran nothing says so.
		noActs,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the ledger dropped %q:\n%s", want, got)
		}
	}

	// The brief once, under the composer's own mark — the page's rule, because a
	// list of commands under a question it does not contain is unreadable a week
	// later (§9.22).
	if n := strings.Count(got, pageBrief); n != 1 {
		t.Errorf("the brief appears %d times on the ledger, want exactly once", n)
	}

	// THE REPLIES ARE NOT HERE. That is the whole of the projection: a ledger
	// that carried the prose too would be the page with extra rows, and the key
	// that opens it would buy nothing.
	for _, prose := range []string{
		"About 30K redundant input tokens per vendor.",
		"Native resume avoids it entirely.",
	} {
		if strings.Contains(got, prose) {
			t.Errorf("the ledger printed a seat's reply %q:\n%s", prose, got)
		}
	}

	// And it does not reach back past the turn it is showing.
	if strings.Contains(got, "Bash: git status") {
		t.Errorf("the ledger swept in an earlier turn's acts:\n%s", got)
	}
}

// TestTheActLedgerNeverReadsAnAbsentOutcomeAsSuccess is the honesty claim this
// surface could get wrong for free.
//
// runner.ActStatus exists because a vendor that reports a step ENDED has not
// reported that it worked — antigravity's steps flip ACTIVE then DONE and no
// captured line has ever carried a success signal. The ledger states an outcome
// on every line, which is exactly the shape that invites a default, so the five
// statuses are asserted to produce five different words and the two that know
// nothing are asserted never to borrow the one that knows something.
func TestTheActLedgerNeverReadsAnAbsentOutcomeAsSuccess(t *testing.T) {
	got := render(ledgered())
	g := UnicodeGlyphs()

	for _, want := range []string{
		g.ActOK + " ok",
		g.ActFail + " failed",
		g.ActUnknown + " outcome unknown",
		g.ActFail + " denied by you",
		"— no outcome reported",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("the ledger does not state %q:\n%s", want, got)
		}
	}

	// The unknown call and the unresolved one must not wear the tick. Asserted on
	// the LINE rather than on the frame, because the frame contains a tick from
	// the call that really did succeed.
	for _, line := range strings.Split(got, "\n") {
		if !strings.Contains(line, "Read: docs/design.md") && !strings.Contains(line, "Bash: go vet") {
			continue
		}
		if strings.Contains(line, g.ActOK) || strings.Contains(line, " ok") {
			t.Errorf("an act with no reported outcome renders as success: %q", line)
		}
	}

	// The five words stay five: a status vocabulary that collapsed two values
	// would still pass every golden that happens not to hold both.
	seen := map[string]runner.ActStatus{}
	for _, s := range []runner.ActStatus{
		runner.ActOK, runner.ActFailed, runner.ActUnknown, runner.ActDenied, runner.ActPending,
	} {
		w := actWord(s, false)
		if w == "" {
			t.Errorf("status %v has no word — the ledger would state an outcome it cannot name", s)
			continue
		}
		if prior, dup := seen[w]; dup {
			t.Errorf("status %v and status %v both render as %q", prior, s, w)
		}
		seen[w] = s
	}
	// A status this build does not know renders NO outcome rather than a guessed
	// one — the gap a later constant has to arrive as.
	if w := actWord(runner.ActStatus(200), false); w != "" {
		t.Errorf("an unrecognised status renders %q, a claim nothing sourced", w)
	}
}

// TestAnUnresolvedCallSplitsOnWhetherTheTurnIsLive. "the vendor has not answered
// yet" and "the vendor never said" are different facts, and the turn being over
// is the only thing that tells them apart.
func TestAnUnresolvedCallSplitsOnWhetherTheTurnIsLive(t *testing.T) {
	if got := actWord(runner.ActPending, true); got != "running" {
		t.Errorf("a pending call on a live turn reads %q, want it to say it is running", got)
	}
	if got := actWord(runner.ActPending, false); got != "no outcome reported" {
		t.Errorf("a pending call on a FINISHED turn reads %q — the turn is over and nothing is running", got)
	}

	st := ledgered()
	st.Turn = 4
	st.Now = time.Date(2026, 8, 17, 9, 0, 0, 12, time.UTC)
	live := &st.Columns[0]
	live.startTurn(4, "what is left?", false)
	live.Phase, live.Started = PhaseStreaming, st.Now.Add(-12*time.Second)
	live.Acts = []Act{{ID: "l1", Text: "Bash: go build ./...", Status: runner.ActPending}}
	st.Page = TurnView{Open: true, Turn: 4, Ledger: true, Follow: true}

	got := render(st)
	if !strings.Contains(got, "— running") {
		t.Errorf("a call in flight on the live turn does not say it is running:\n%s", got)
	}
	if strings.Contains(got, "no outcome reported") {
		t.Errorf("a call in flight is reported as one the vendor never resolved:\n%s", got)
	}
}

// TestTheActLedgerStatesItsOwnRetentionWindow is requirement (a) of the surface:
// "the acts" is a claim about a scope, and maxHistory puts a hard floor under it.
//
// Read off the constant rather than pinned to the number, so the sentence cannot
// outlive a change to the cap — a test that spelled "50" is how a stale 50
// survives (the §9.39 defect, one surface over).
func TestTheActLedgerStatesItsOwnRetentionWindow(t *testing.T) {
	want := "last " + strconv.Itoa(maxHistory) + " turns"
	got := render(ledgered())
	if !strings.Contains(got, want) {
		t.Errorf("the ledger's header does not qualify its scope with %q:\n%s", want, got)
	}

	// The clipboard document carries it too, and there it matters more: on screen
	// the reader can re-check the bound by pressing `[`, and in a file pasted a
	// week later this sentence is the only thing saying the record was bounded.
	if y := ledgered().YankPage(); !strings.Contains(y.Text, want) {
		t.Errorf("the yanked ledger drops the retention window:\n%s", y.Text)
	}

	// And an evicted turn says the record is gone rather than drawing an empty
	// ledger — the page's own answer, because the eviction is a fact about the
	// record and not about which face is open (§4a.1).
	gone := ledgered()
	gone.Page.Turn = 1
	if got := render(gone); !strings.Contains(got, "no longer in memory") {
		t.Errorf("a ledger for an evicted turn renders as a turn with no acts:\n%s", got)
	}
}

// TestASeatThatRecordedNothingIsNotASeatThatDidNothing. A trace is a reading of
// what a vendor chose to report, so the strongest claim the room owns is that
// nothing was RECORDED. "did nothing" would be council writing a fact no adapter
// sourced (§4a.1), on the one surface a reader would take as the record of it.
func TestASeatThatRecordedNothingIsNotASeatThatDidNothing(t *testing.T) {
	got := render(ledgered())
	if !strings.Contains(got, noActs) {
		t.Fatalf("the seat that ran nothing has no line at all:\n%s", got)
	}
	for _, claim := range []string{"did nothing", "no acts\n", "(none)"} {
		if strings.Contains(got, claim) {
			t.Errorf("the ledger states %q about a seat whose vendor reported no trace:\n%s", claim, got)
		}
	}
	// The rule's own line and the header's zero case say the same words, so a
	// reader cannot learn one spelling on the rule and another in the body.
	empty := ledgered()
	for i := range empty.Columns {
		empty.Columns[i].Acts = nil
	}
	if got := render(empty); strings.Count(got, noActs) != len(empty.Columns)+1 {
		t.Errorf("a turn where nothing was recorded does not say so on the rule and under every seat:\n%s", got)
	}
}

// TestTheActLedgerLeavesOutASeatThatSatTheTurnOut. §9.15's rule, and it binds
// harder here than on the page: the ledger's document is what someone pastes into
// a review, where an older turn's `git commit` filed under this turn's heading
// would be a history the room invented and nobody could check.
func TestTheActLedgerLeavesOutASeatThatSatTheTurnOut(t *testing.T) {
	st := ledgered()
	a := &st.Columns[2]
	*a = room().Columns[2]
	a.startTurn(2, "an older question", false)
	a.Acts = []Act{{ID: "c1", Text: "Bash: git stash", Status: runner.ActOK}}
	a.Body, a.Phase = "an older answer from Antigravity", PhaseDone
	a.Note, a.Skipped = "not addressed in turn 3", true

	got := render(st)
	if strings.Contains(got, "Antigravity") {
		t.Errorf("a seat that sat the turn out has a block on the ledger:\n%s", got)
	}
	if strings.Contains(got, "git stash") {
		t.Errorf("the ledger filed an older turn's act under this turn's heading:\n%s", got)
	}
	if y := st.YankPage(); strings.Contains(y.Text, "git stash") {
		t.Errorf("the yanked ledger filed an older turn's act under this turn:\n%s", y.Text)
	}
	// The route follows participation, so it names the two seats rather than
	// claiming everyone — the same measurement the page reads (§9.21).
	if !strings.Contains(got, "→ claude, codex") {
		t.Errorf("the ledger's rule does not name the seats the turn reached:\n%s", got)
	}
}

// TestTheActLedgerSurvivesASCII. Every distinction this room makes is carried by
// a word first, so --ascii and NO_COLOR must read identically (§9.11) — and the
// ledger is the surface where that rule pays: an outcome mark is the only thing
// the reduced set changes, and the word beside it is the fact.
func TestTheActLedgerSurvivesASCII(t *testing.T) {
	st := ledgered()
	st.ASCII = true
	got := Render(st, PlainStyles(), GlyphsFor(true))
	golden(t, "act-ledger-ascii", got)

	g := GlyphsFor(true)
	for _, want := range []string{
		"acts in turn 3", "→ everyone", "5 acts",
		"last " + strconv.Itoa(maxHistory) + " turns",
		g.ActOK + " ok", g.ActFail + " failed", g.ActUnknown + " outcome unknown",
		g.ActFail + " denied by you", "— no outcome reported", noActs,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--ascii dropped %q:\n%s", want, got)
		}
	}
	// The words are identical in both glyph sets. Only the mark changes, which is
	// the property that makes the mark affordable at all.
	for _, w := range []string{"ok", "failed", "outcome unknown", "denied by you", "no outcome reported"} {
		if !strings.Contains(got, w) {
			t.Errorf("--ascii lost the outcome word %q:\n%s", w, got)
		}
	}
}

// TestTAndTOpenTheTwoFacesOfOneTurn. The key does not navigate: it swaps which of
// one turn's two records is drawn, so a reader who has walked back to turn 2 is
// still on turn 2 in either face.
func TestTAndTOpenTheTwoFacesOfOneTurn(t *testing.T) {
	// A shifted letter has to reach viewKey as "T" or the binding is dead on
	// arrival, so the keypress is checked before the behaviour. ultraviolet's
	// Key.String returns Key.Text whenever it is non-empty, which is what makes a
	// printable keypress arrive as its character rather than as "shift+t" — the
	// same line `Y` has depended on since §9.15.
	if got := key("T").String(); got != "T" {
		t.Fatalf("a shifted letter arrives as %q — the ledger key would never fire", got)
	}

	st := ledgered()
	st.Page = TurnView{}
	m := &Model{st: st, glyphs: GlyphsFor(false)}

	m.viewKey(key("T"))
	if !m.st.Page.Open || !m.st.Page.Ledger {
		t.Fatalf("T did not open the act ledger (open=%v ledger=%v)", m.st.Page.Open, m.st.Page.Ledger)
	}
	if got := m.st.Page.Turn; got != 3 {
		t.Errorf("T opened turn %d, want the newest at 3", got)
	}

	// The subject survives the flip, in both directions.
	m.hopPage(-1)
	if got := m.st.Page.Turn; got != 2 {
		t.Fatalf("[ landed on turn %d", got)
	}
	m.viewKey(key("T"))
	if m.st.Page.Ledger {
		t.Error("T on the ledger did not return the reading face")
	}
	if got := m.st.Page.Turn; got != 2 {
		t.Errorf("the face flip moved the page to turn %d — it is not a navigation key", got)
	}
	m.viewKey(key("T"))
	if !m.st.Page.Ledger || m.st.Page.Turn != 2 {
		t.Errorf("flipping back moved the page (turn=%d ledger=%v)", m.st.Page.Turn, m.st.Page.Ledger)
	}

	// `t` still means the reading face, always: it closes the projection from
	// either face, and re-opening it lands on the replies rather than on whatever
	// the reader was last looking at.
	m.viewKey(key("t"))
	if m.st.Page.Open {
		t.Error("t did not return the grid from the ledger")
	}
	m.viewKey(key("t"))
	if !m.st.Page.Open || m.st.Page.Ledger {
		t.Errorf("t re-opened onto the ledger (open=%v ledger=%v)", m.st.Page.Open, m.st.Page.Ledger)
	}

	// A room with nothing behind it is told so rather than handed a blank ledger.
	empty := &Model{st: room(), glyphs: GlyphsFor(false)}
	empty.viewKey(key("T"))
	if empty.st.Page.Open {
		t.Error("T opened a ledger for a room with no turns")
	}
	if !strings.Contains(empty.st.Notice, "no turn has been taken yet") {
		t.Errorf("Notice = %q, want it to say why nothing opened", empty.st.Notice)
	}

	// And in compose it is the letter T — the contract q, f, c and t already keep.
	typing := &Model{st: ledgered()}
	typing.st.Page = TurnView{}
	typing.st.Mode = ModeComposing
	typing.composeKey(key("T"))
	if typing.st.Page.Open {
		t.Error("T opened the ledger while a brief was being typed")
	}
	if typing.st.Draft != "T" {
		t.Errorf("Draft = %q, want the letter T", typing.st.Draft)
	}
}

// TestTheModeWordNamesWhichFaceIsOpen. §7.8 puts the always-on statement of what
// is on screen in the mode word, and two documents at one coordinate would leave
// it unable to tell them apart.
func TestTheModeWordNamesWhichFaceIsOpen(t *testing.T) {
	st := ledgered()
	st.Turn = 4
	if got := render(st); !strings.Contains(got, "ACTS 3/4") {
		t.Errorf("the mode word does not name the ledger face:\n%s", lastLine(got))
	}
	st.Page.Ledger = false
	if got := render(st); !strings.Contains(got, "TURN 3/4") {
		t.Errorf("the reading face lost its own mode word:\n%s", lastLine(got))
	}
}

// TestYankOnTheLedgerTakesTheLedger is requirement (b): the copy key follows the
// FACE as well as the turn, through the document the projection already builds.
//
// There is no second sanitizer and there must never be one — every string here
// arrived on State through the one redact-and-sanitize choke point, and a
// cleaning step of the ledger's own would be a second answer to what is safe to
// put on a clipboard.
func TestYankOnTheLedgerTakesTheLedger(t *testing.T) {
	// Fallback path, so the assertion stays about the KEY rather than about which
	// clipboard mechanism this machine happens to have.
	stubNoNativeClipboard(t)

	for _, k := range []string{"y", "Y"} {
		m := &Model{st: ledgered()}
		_, cmd := m.viewKey(key(k))
		if cmd == nil {
			t.Fatalf("%s produced no clipboard command on the ledger", k)
		}
		if !strings.Contains(m.st.Notice, "copied turn 3's acts") {
			t.Errorf("%s: Notice = %q, want the ledger's own document", k, m.st.Notice)
		}
		if !strings.Contains(m.st.Notice, "3 seats, 5 acts") {
			t.Errorf("%s: Notice = %q, want the seats and the acts it took", k, m.st.Notice)
		}
	}

	y := ledgered().YankPage()
	for _, want := range []string{
		"# turn 3 — acts",
		"> " + pageBrief,
		"## Claude Code",
		"- Bash: go test ./internal/council — ok",
		"- Write: internal/council/ledger.go — failed",
		"  permission denied",
		"— denied by you",
		"- Read: docs/design.md — outcome unknown",
		"- Bash: go vet ./... — no outcome reported",
		"## Antigravity\n\n" + noActs,
	} {
		if !strings.Contains(y.Text, want) {
			t.Errorf("the yanked ledger dropped %q:\n%s", want, y.Text)
		}
	}
	// The replies stay out of it, exactly as they stay off the screen.
	if strings.Contains(y.Text, "About 30K redundant") {
		t.Errorf("the yanked ledger carried a seat's reply:\n%s", y.Text)
	}
	// The reading face's own document is untouched by any of this.
	if p := ledgered().YankTurnN(3); !strings.Contains(p.Text, "About 30K redundant") {
		t.Errorf("the turn yank lost the replies:\n%s", p.Text)
	}

	// An empty ledger still issues no command: writing "" through OSC 52 is the
	// documented way to CLEAR a clipboard (§9.15).
	m := &Model{st: room()}
	m.st.Page = TurnView{Open: true, Turn: 1, Ledger: true}
	if cmd := m.yank(m.st.YankPage()); cmd != nil {
		t.Error("an empty ledger yank issued a clipboard write, which would CLEAR the clipboard")
	}
}

// TestAGateOutranksTheActLedger. The one collision in this keymap where losing
// means a keystroke the user believes approved a tool call quietly copies text
// instead — asserted again on the third surface `y` now reaches (§9.15, §9.22).
func TestAGateOutranksTheActLedger(t *testing.T) {
	stubNoNativeClipboard(t)
	m := &Model{st: ledgered(), gateInputs: map[string]map[string]any{}}
	m.st.Gates = []PendingGate{{
		Vendor: model.VendorClaude, RequestID: "r1", ToolUseID: "t1",
		Text: "Write: internal/council/ledger.go",
	}}

	_, cmd := m.key(key("y"))
	if len(m.st.Gates) != 0 {
		t.Fatal("y did not answer the pending gate — the approve key was stolen by the ledger's yank")
	}
	if cmd != nil {
		t.Error("y issued a clipboard write while a vendor was blocked on it")
	}
	if strings.Contains(m.st.Notice, "copied") {
		t.Errorf("the room reported a copy for a keystroke that approved a tool call: %q", m.st.Notice)
	}
}

// TestTheHelpPanelNamesTheActLedgerAboveTheFold. helpBody clips at the body
// height and does not scroll, so a row past the fold is not a demoted row, it is
// no row — and a projection nobody can find is a projection that does not exist.
//
// The row budget is why `T` is on `f`'s row rather than one of its own, and the
// width budget is why the row had to buy the words back: a panel row wider than
// the frame is a row that truncates, which is the same defect one axis over.
func TestTheHelpPanelNamesTheActLedgerAboveTheFold(t *testing.T) {
	g := GlyphsFor(false)
	st := room()
	st.Width, st.Height = 120, 24
	lay := layoutFor(st, g)
	lines := helpKeys(lay, PlainStyles(), g)

	fold := -1
	for i, l := range lines {
		if strings.Contains(l, "? ") && strings.Contains(l, "next page") {
			fold = i
			break
		}
	}
	if fold < 0 {
		t.Fatal("the `?` line is gone — the panel has no documented way out")
	}
	above := strings.Join(lines[:fold+1], "\n")
	if !strings.Contains(above, "f / t / T") {
		t.Error("`T` is not named above the fold — the ledger cannot be discovered in the UI")
	}
	if !strings.Contains(above, "acts") {
		t.Error("the row names the key and never says what it opens")
	}

	// It took no row of its own: the panel's budget is hard, and the `?` line is
	// the only documented way out of it.
	for _, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "T ") {
			t.Errorf("the ledger key took a row of its own: %q", l)
		}
	}
	// And the prose column still lines up with every other row's.
	row := ""
	for _, l := range lines {
		if strings.Contains(l, "f / t / T") {
			row = l
		}
	}
	if i := strings.Index(row, "f gives"); i != helpIndent {
		t.Errorf("the row's prose starts at column %d, want helpIndent (%d): %q", i, helpIndent, row)
	}
	if n := lipgloss.Width(row); n > lay.Width-2*framePad {
		t.Errorf("the row is %d cells against a %d-cell panel — it truncates: %q",
			n, lay.Width-2*framePad, row)
	}
}
