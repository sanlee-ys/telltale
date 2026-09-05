package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// This file is design.md §9.31: a draft that opens with a slash and names no
// room command is REFUSED rather than dispatched, and there is a one-keystroke
// way to send one that was meant as prose.
//
// The field report it exists for: `/unseat codex` was typed into a live room
// before /unseat existed. There was no such command, so the draft fell through
// as a brief and three seats were billed to discuss the string until the user
// cancelled the turn. Every assertion below is about that cost — what SPAWNED,
// what the room still holds — rather than about which branch was taken.

// enter is the draft's own way out of the composer, exactly as program.go wires
// it: a room command is handled and nothing dispatches, anything else goes to
// the vendors. Tests drive this rather than roomCommand alone, because "was not
// intercepted" and "was billed" are two different claims and only the second one
// is the defect.
// enter presses enter in the composer, and answers the write acknowledgement
// card when one goes up (ack.go). A refusal raises no card, so every test in
// this file that asserts a refusal is untouched by the second keystroke.
func enter(m *Model) {
	m.st.Mode = ModeComposing
	m.composeKey(key("enter"))
	answerAck(m)
}

// TestAMistypedCommandSpawnsNothing is the defect, asserted where it hurt: the
// spawn count. A refusal that merely set a notice while three processes started
// would pass every wording test in this file and still cost the turn.
func TestAMistypedCommandSpawnsNothing(t *testing.T) {
	for _, draft := range []string{
		"/unseet codex",       // the live typo
		"/clear",              // a vendor's command, not the room's
		"/seats claude,codex", // a plural away from a real one
		"/",                   // the bare slash
		"/flowchart the auth path",
	} {
		log := countSpawns(t)
		m := flowRoom(t, true)
		m.setDraft(draft)

		enter(m)

		if log.n() != 0 {
			t.Errorf("%q spawned %d process(es): %+v", draft, log.n(), log.specs)
		}
		if m.st.Turn != 0 {
			t.Errorf("%q was counted as turn %d", draft, m.st.Turn)
		}
		if m.anyInFlight() {
			t.Errorf("%q started a turn", draft)
		}
		if m.st.Draft != draft {
			// The draft is kept, on /cd's argument: nothing was dispatched, and a
			// line the user is about to edit must not have to be retyped.
			t.Errorf("%q was thrown away rather than handed back: %q", draft, m.st.Draft)
		}
		if !strings.Contains(m.st.Notice, "no command") {
			t.Errorf("%q was refused without saying so: %q", draft, m.st.Notice)
		}
	}
}

// TestTheRefusalNamesWhatFailed. A refusal that only listed the room's
// vocabulary would leave the reader to spot their own typo in a line of seven
// similar words.
func TestTheRefusalNamesWhatFailed(t *testing.T) {
	m := flowRoom(t, true)
	m.setDraft("/unseet codex")
	m.roomCommand()

	if !strings.Contains(m.st.Notice, "/unseet") {
		t.Errorf("the refusal does not quote the word that failed: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Notice, "codex") {
		// The VERB failed, not the argument. Echoing the whole draft would put an
		// arbitrary amount of the user's text into a line that truncates.
		t.Errorf("the refusal quotes the argument as well as the verb: %q", m.st.Notice)
	}
}

// TestTheRefusalListsTheLiveCommandTable is the anti-staleness assertion, and it
// is deliberately a WALK of roomVerbs rather than a comparison against a list
// written here. A hardcoded expectation in a test is the same second copy the
// notice is forbidden to hold: /unseat itself would have shipped with a refusal
// that did not mention it, and both copies would have agreed.
func TestTheRefusalListsTheLiveCommandTable(t *testing.T) {
	m := flowRoom(t, true)
	m.setDraft("/nosuchverb")
	m.roomCommand()

	for _, rc := range roomVerbs() {
		if !strings.Contains(m.st.Notice, rc.verb) {
			t.Errorf("%s is a room command the refusal does not name: %q", rc.verb, m.st.Notice)
		}
	}
	if !strings.Contains(m.st.Notice, "leading space") {
		t.Errorf("the refusal does not name the way to send it anyway: %q", m.st.Notice)
	}
}

// TestTheRefusalFitsTheRoomItIsShownIn. This notice replaces the whole hint
// stack on the mode line, which truncates from the RIGHT — so at the room's
// reference width it has to fit, and the clause a narrower room loses has to be
// the vocabulary rather than the remedy. §9.17's defect shape is a refusal whose
// remedy cannot be found.
func TestTheRefusalFitsTheRoomItIsShownIn(t *testing.T) {
	m := flowRoom(t, true)
	m.st.Width, m.st.Height = 120, 24
	m.setDraft("/unseet codex")
	m.roomCommand()

	st := room()
	st.Mode = ModeComposing
	st.Draft = m.st.Draft
	st.Notice = m.st.Notice
	line := lastLine(render(st))
	if strings.Contains(line, UnicodeGlyphs().Ellipsis) {
		t.Errorf("the refusal is clipped at the room's own width:\n%q", line)
	}
	if !strings.Contains(line, "leading space") {
		t.Errorf("the remedy did not survive to the screen:\n%q", line)
	}
	golden(t, "slash-refusal", render(st))

	// The ascii twin. Only the GLYPHS change: the notice is prose the model
	// wrote, so its words are the same in both, which is what makes the pair
	// worth pinning — a refusal that only reads correctly in a Unicode terminal
	// would be a control the ascii room could not learn.
	st.ASCII = true
	golden(t, "slash-refusal-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestALeadingSpaceSendsASlashBriefToTheVendors is the escape hatch, and it is
// asserted end to end: a brief that legitimately opens with a slash reaches the
// vendors carrying the leading space, because sanitizeKeepingSpace does not trim
// and neither does anything between the composer and the spawn.
func TestALeadingSpaceSendsASlashBriefToTheVendors(t *testing.T) {
	for _, draft := range []string{
		" /usr/local/bin is on PATH — check it",
		" /^ERROR/ matches too much, tighten it",
		// The one prefix that used to be swallowed anyway: dispatch matched
		// /flow on a TrimSpace'd prefix, so an escaped path starting with those
		// five letters became a flow syntax error instead of a brief.
		" /flow/gate.log is the file I mean",
	} {
		log := countSpawns(t)
		m := flowRoom(t, true)
		m.setDraft(draft)

		enter(m)

		if log.n() == 0 {
			t.Errorf("%q was not dispatched: %q", draft, m.st.Notice)
			continue
		}
		if m.flowChain != nil {
			t.Errorf("%q was read as a flow chain", draft)
		}
		// The brief the room recorded for the seat it dispatched to, which is
		// the same string the vendor was handed — asserted here rather than off
		// the spec because the default route is a persistent seat and takes its
		// prompt on stdin.
		c := m.column(model.VendorClaude)
		if c == nil || c.TurnN == 0 {
			t.Errorf("%q reached no seat", draft)
			continue
		}
		if c.Prompt != draft {
			t.Errorf("%q reached the seat as %q — the escape was eaten on the way",
				draft, c.Prompt)
		}
	}
}

// TestTheEscapeHatchSurvivesTheComposer. The hatch is only real if the composer
// can hold the space that arms it: sanitizeKeepingSpace flattens a pasted
// newline and drops control characters, and a filter that also trimmed would
// make the remedy the refusal names impossible to type.
func TestTheEscapeHatchSurvivesTheComposer(t *testing.T) {
	if got := sanitizeKeepingSpace(" /etc/hosts"); got != " /etc/hosts" {
		t.Fatalf("the composer ate the leading space: %q", got)
	}
	m := flowRoom(t, true)
	m.st.Mode = ModeComposing
	for _, k := range []string{" ", "/", "c", "d"} {
		m.composeKey(key(k))
	}
	if m.st.Draft != " /cd" {
		t.Fatalf("typing the escape produced %q", m.st.Draft)
	}
	if m.roomCommand() {
		t.Errorf("an escaped /cd was still read as the command: %q", m.st.Notice)
	}
}

// TestAKnownCommandIsStillACommand. The refusal must not have swallowed the
// vocabulary it protects — every verb in the table is either handled here or
// (for /flow) handed on to dispatch, and none of them lands in the refusal.
func TestAKnownCommandIsStillACommand(t *testing.T) {
	for _, rc := range roomVerbs() {
		m := flowRoom(t, false)
		m.glyphs = GlyphsFor(false)
		m.trace = newTraceSink()
		t.Cleanup(m.trace.close)
		m.setDraft(rc.verb)

		handled := m.roomCommand()
		if strings.Contains(m.st.Notice, "no command") {
			t.Errorf("%s was refused by the table that lists it: %q", rc.verb, m.st.Notice)
		}
		if rc.run == nil {
			if handled {
				t.Errorf("%s was intercepted; dispatch.go owns it", rc.verb)
			}
			continue
		}
		if !handled {
			t.Errorf("%s dispatched to the vendors", rc.verb)
		}
	}
}

// TestFlowIsRecognisedByTheRoomsOneVocabularyRule. dispatch.go used to take any
// draft whose first non-space characters were "/flow", so a word that merely
// begins with them was an orchestration. One rule for every room word means
// "/flowchart …" is prose and reaches the refusal like any other slash slip.
func TestFlowIsRecognisedByTheRoomsOneVocabularyRule(t *testing.T) {
	for draft, want := range map[string]bool{
		"/flow @codex review -> @claude summarize": true,
		"/flow":                    true,
		"/flowchart the auth path": false,
		" /flow @codex review":     false,
		"tell me about /flow":      false,
	} {
		if got := isFlowCommand(draft); got != want {
			t.Errorf("isFlowCommand(%q) = %v, want %v", draft, got, want)
		}
	}
}

// TestUnseatSubtractsFromTheRoster is the command itself. `/seat` names who
// stays; this names who leaves, and the room it produces is the complement.
func TestUnseatSubtractsFromTheRoster(t *testing.T) {
	m := seatModel()
	m.setDraft("/unseat codex")

	if !m.roomCommand() {
		t.Fatal("/unseat dispatched to the vendors instead of being intercepted")
	}
	for _, v := range seatedNow(m) {
		if v == model.VendorCodex {
			t.Fatalf("codex is still seated: %v", seatedNow(m))
		}
	}
	if len(seatedNow(m)) != 2 {
		t.Fatalf("seated = %v, want the other two", seatedNow(m))
	}
	if m.st.Draft != "" {
		t.Errorf("the draft survived a successful /unseat: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "keep their threads") {
		t.Errorf("the notice does not say what an unseated seat keeps: %q", m.st.Notice)
	}
}

// TestUnseatKeepsEveryThread. Same ruling as /seat, asserted separately because
// it is the property a subtractive spelling makes easiest to reach for: nothing
// is killed, so /seat all puts the seat back mid-conversation.
func TestUnseatKeepsEveryThread(t *testing.T) {
	m := seatModel()
	before := map[model.VendorID]string{}
	for v, id := range m.sessions {
		before[v] = id
	}

	m.setDraft("/unseat codex")
	m.roomCommand()

	for v, id := range before {
		if m.sessions[v] != id {
			t.Errorf("%s's thread was dropped: %q became %q", v, id, m.sessions[v])
		}
		p, ok := m.procs[v]
		if !ok {
			t.Errorf("%s's process was reclaimed; the thread lives in it", v)
			continue
		}
		if ks, isKill := p.sess.(*killSession); isKill && ks.killed {
			t.Errorf("%s's process was killed by /unseat", v)
		}
	}

	m.setDraft("/seat all")
	m.roomCommand()
	if len(seatedNow(m)) != 3 {
		t.Errorf("/seat all did not put everyone back: %v", seatedNow(m))
	}
}

// TestUnseatSharesTheMentionVocabulary. One alias table for @mentions, --vendor,
// /seat and /unseat, or the room teaches two names for one seat.
func TestUnseatSharesTheMentionVocabulary(t *testing.T) {
	for _, alias := range []string{"agy", "antigravity", "@agy", "agy,"} {
		m := seatModel()
		m.setDraft("/unseat " + alias)
		m.roomCommand()

		for _, v := range seatedNow(m) {
			if v == model.VendorAntigravity {
				t.Errorf("/unseat %s left antigravity seated: %v", alias, seatedNow(m))
			}
		}
	}
}

// TestUnseatWillNotEmptyTheRoom. A room with no seats can answer nothing, so the
// last one is refused — /seat's own rule, reached by subtraction.
func TestUnseatWillNotEmptyTheRoom(t *testing.T) {
	m := seatModel()
	m.setDraft("/seat codex")
	m.roomCommand()

	m.setDraft("/unseat codex")
	if !m.roomCommand() {
		t.Fatal("/unseat dispatched instead of refusing")
	}
	if len(seatedNow(m)) != 1 {
		t.Fatalf("the room was emptied: %v", seatedNow(m))
	}
	if !strings.Contains(m.st.Notice, "at least one seat") {
		t.Errorf("the refusal does not say what is wrong: %q", m.st.Notice)
	}
	if m.st.Draft != "/unseat codex" {
		t.Errorf("the refused draft was discarded: %q", m.st.Draft)
	}

	// And the sentence a user is likelier to type for the same thing.
	all := seatModel()
	all.setDraft("/unseat all")
	all.roomCommand()
	if len(seatedNow(all)) != 3 {
		t.Fatalf("/unseat all emptied the room: %v", seatedNow(all))
	}
	if !strings.Contains(all.st.Notice, "at least one seat") {
		t.Errorf("/unseat all was refused as a spelling mistake: %q", all.st.Notice)
	}
}

// TestUnseatRefusesMidTurn. The roster is dispatch state: the grid for a turn in
// flight was decided at dispatch, so reseating under it would redraw the room
// around columns that are mid-answer. /seat's refusal, for /seat's reason.
func TestUnseatRefusesMidTurn(t *testing.T) {
	m := seatModel()
	was := seatedNow(m)
	occupy(m)

	m.setDraft("/unseat codex")
	if !m.roomCommand() {
		t.Fatal("/unseat dispatched during a turn")
	}
	if got := seatedNow(m); len(got) != len(was) {
		t.Errorf("the room was reseated mid-turn: %v became %v", was, got)
	}
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("the refusal did not say why: %q", m.st.Notice)
	}
}

// TestBareUnseatReports, the way bare /cd, /trace and /seat do: a command that
// half-asks a question answers it rather than doing something.
func TestBareUnseatReports(t *testing.T) {
	m := seatModel()
	was := seatedNow(m)

	m.setDraft("/unseat")
	if !m.roomCommand() {
		t.Fatal("bare /unseat was not intercepted")
	}
	if got := seatedNow(m); len(got) != len(was) {
		t.Error("bare /unseat changed the room instead of reporting it")
	}
	if !strings.Contains(m.st.Notice, "seated:") {
		t.Errorf("bare /unseat did not report who is seated: %q", m.st.Notice)
	}
}

// TestUnseatNamesASeatItCannotRemove. A typo is refused by the shared alias
// table; a seat that is simply not in the room is a different answer, and both
// have to change nothing — a command that quietly did less than it was asked is
// discovered several turns later as a seat still answering.
func TestUnseatNamesASeatItCannotRemove(t *testing.T) {
	typo := seatModel()
	typo.setDraft("/unseat claud")
	typo.roomCommand()
	if len(seatedNow(typo)) != 3 {
		t.Errorf("a typo changed the room: %v", seatedNow(typo))
	}
	if !strings.Contains(typo.st.Notice, "claud") {
		t.Errorf("the refusal did not name what it did not recognise: %q", typo.st.Notice)
	}

	out := seatModel()
	out.setDraft("/unseat cursor") // known name, no seat in this room
	out.roomCommand()
	if len(seatedNow(out)) != 3 {
		t.Errorf("naming an absent seat changed the room: %v", seatedNow(out))
	}
	if !strings.Contains(out.st.Notice, "not in the room") {
		t.Errorf("the notice does not distinguish absent from misspelled: %q", out.st.Notice)
	}
}

// TestUnseatRemovesASeatItCannotDrive. `/seat cursor` forces an uninstalled seat
// on screen, because a user who asked for it is owed the card saying why it is
// not there — so that seat IS in the room, and the subtraction has to reach it.
// Membership is what the room SHOWS; "can it answer" is a different question,
// and only the second one guards the last seat.
func TestUnseatRemovesASeatItCannotDrive(t *testing.T) {
	m := seatModel()
	m.st.Columns = append(m.st.Columns, Column{
		Vendor: model.VendorCursor, Label: "Cursor",
		Avail: AvailNotInstalled, Note: "not found on PATH",
	})
	m.st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCursor}}

	m.setDraft("/unseat cursor")
	if !m.roomCommand() {
		t.Fatal("/unseat was not intercepted")
	}
	if strings.Contains(m.st.Notice, "not in the room") {
		t.Errorf("a forced, undrivable seat was called absent: %q", m.st.Notice)
	}
	if m.showsVendor(model.VendorCursor) {
		t.Error("the card the user asked to be rid of is still drawn")
	}

	// And the other direction: unseating the only seat that CAN answer is
	// refused, even though a card would still be on screen.
	back := seatModel()
	back.st.Columns = append(back.st.Columns, Column{
		Vendor: model.VendorCursor, Label: "Cursor", Avail: AvailNotInstalled,
	})
	back.st.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCursor}}
	back.setDraft("/unseat claude")
	back.roomCommand()
	if !back.seatsVendor(model.VendorClaude) {
		t.Errorf("the room was left with nothing that can answer: %q", back.st.Notice)
	}
	if !strings.Contains(back.st.Notice, "at least one seat") {
		t.Errorf("the refusal does not say what is wrong: %q", back.st.Notice)
	}
}

// TestUnseatWarnsWhenItRemovesTheDefaultRoute. Silence goes to claude, so a room
// without claude answers nothing until every brief is @mentioned. The warning
// lives in applySeats, shared with /seat, precisely so the subtractive spelling
// cannot be the one that says nothing.
func TestUnseatWarnsWhenItRemovesTheDefaultRoute(t *testing.T) {
	m := seatModel()
	m.setDraft("/unseat claude")
	m.roomCommand()

	if !strings.Contains(m.st.Notice, "not seated") {
		t.Errorf("unseating claude said nothing about the default route: %q", m.st.Notice)
	}
}

// TestUnseatingTheFocusedSeatHandsFocusOff. Focus indexes Columns, so a roster
// that drops the focused one leaves it pointing at a column the grid no longer
// draws: the focus mark disappears and the scroll keys go on moving a hidden
// transcript. stateWith puts focus on the first drawn column at launch; this is
// the same rule wherever the roster moves.
func TestUnseatingTheFocusedSeatHandsFocusOff(t *testing.T) {
	m := seatModel()
	m.st.Focus = 1 // Codex
	m.setDraft("/unseat codex")
	m.roomCommand()

	vis := m.st.VisibleColumns()
	found := false
	for _, i := range vis {
		if i == m.st.Focus {
			found = true
		}
	}
	if !found {
		t.Fatalf("focus = %d, which is not among the drawn columns %v", m.st.Focus, vis)
	}
	if m.st.Columns[m.st.Focus].Vendor == model.VendorCodex {
		t.Error("focus stayed on the seat that just left")
	}

	// The same hole /seat had, closed by the same helper: naming who stays can
	// unseat the focused column just as easily as naming who leaves.
	s := seatModel()
	s.st.Focus = 1
	s.setDraft("/seat claude")
	s.roomCommand()
	if s.st.Columns[s.st.Focus].Vendor == model.VendorCodex {
		t.Error("/seat left focus on an unseated column")
	}
}

// TestUnseatIsPersistedByTheChokePoint is the seam between this change and
// §9.32, which landed in parallel and predicted it: the roster save watches
// roomCommand's OBSERVED roster rather than living inside the command that
// moved it, "which is what lets a /unseat written in parallel compose with this
// without either side being told about the other". That composition is a claim,
// so it is asserted against the file rather than against the state — and the
// refusal above must not save at all, since it changes nothing.
//
// This is the one test here built on a room that DETECTED its seats rather than
// on the hand-built fixture, so it runs on a machine with four vendors installed
// and on CI with none. That difference caught a real bug: membership was tested
// with seatsVendor (drivable), so on CI every /unseat was answered "not in the
// room" and the file never moved. Membership is what the room shows.
func TestUnseatIsPersistedByTheChokePoint(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	m.setDraft("/seat claude,codex,agy")
	m.roomCommand()

	m.setDraft("/unseat codex")
	if !m.roomCommand() {
		t.Fatal("/unseat was not intercepted")
	}
	want := Seats{Only: []model.VendorID{model.VendorClaude, model.VendorAntigravity}}
	if got := savedNow(t).Seats; !sameSeats(got, want) {
		t.Errorf("the file says %+v, want %+v", got, want)
	}
	first := savedNow(t).SavedAt

	// A refused subtraction and a refused verb both leave the roster alone, so
	// neither may refresh SavedAt — the age a reattach shows.
	for _, draft := range []string{"/unseat", "/unseat claud", "/unseat cursor", "/unseet codex"} {
		m.setDraft(draft)
		m.roomCommand()
		if got := savedNow(t).SavedAt; !got.Equal(first) {
			t.Errorf("%q rewrote the room file: %v became %v", draft, first, got)
		}
	}
}

// TestHelpNamesUnseatAboveTheFold. helpBody clips at the body height and does
// not scroll, so a control named past the fold cannot be found in the UI at all
// (§9.20) — which is the failure that put /trace below it once already.
func TestHelpNamesUnseatAboveTheFold(t *testing.T) {
	lines := helpKeys(layoutFor(room(), GlyphsFor(false)), PlainStyles(), GlyphsFor(false))
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
	if !strings.Contains(strings.Join(lines[:fold+1], "\n"), "/unseat") {
		t.Error("/unseat is not named above the fold — it cannot be discovered in the UI")
	}
}
