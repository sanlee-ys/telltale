package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// seatModel is a room where every seat holds a thread AND a live process, which
// is the state the reversibility property is actually about. A model with empty
// maps would pass every assertion below without proving anything.
func seatModel() *Model {
	m := &Model{
		st:     room(),
		glyphs: GlyphsFor(false),
		sessions: map[model.VendorID]string{
			model.VendorClaude:      "claude-thread",
			model.VendorCodex:       "codex-thread",
			model.VendorAntigravity: "agy-thread",
		},
		resumeIDs: map[model.VendorID]string{},
		unproven:  map[model.VendorID]bool{},
		procs:     map[model.VendorID]*seatProc{},
	}
	for v := range m.sessions {
		m.procs[v] = &seatProc{sess: &killSession{}}
	}
	return m
}

func seatedNow(m *Model) []model.VendorID {
	var out []model.VendorID
	for _, c := range m.st.Columns {
		if m.st.seats(c) {
			out = append(out, c.Vendor)
		}
	}
	return out
}

// TestSeatNarrowsTheRoom. The width is the whole point: an unseated seat stops
// being drawn and stops being dispatched to, which is what hands its column to
// the seats that are answering.
func TestSeatNarrowsTheRoom(t *testing.T) {
	m := seatModel()
	m.setDraft("/seat codex")

	if !m.roomCommand() {
		t.Fatal("/seat dispatched to the vendors instead of being intercepted")
	}
	got := seatedNow(m)
	if len(got) != 1 || got[0] != model.VendorCodex {
		t.Fatalf("seated = %v, want codex alone", got)
	}
	if m.st.Draft != "" {
		t.Errorf("the draft survived: %q", m.st.Draft)
	}
}

// TestUnseatingKeepsEveryThread is the ruling this command was built to, and the
// reason it kills nothing.
//
// A seat with a live process and no reported session id holds its whole
// conversation IN that process (§9.8), so a /seat that killed processes to
// reclaim them would destroy a thread seatHasThread calls real — silently, on a
// command nobody reads as destructive. Dropping a thread is `c`'s job, and `c`
// asks first. Asserted on all three resume maps AND the process, because an id
// surviving in one map while the process died is not a kept thread.
func TestUnseatingKeepsEveryThread(t *testing.T) {
	m := seatModel()
	before := map[model.VendorID]string{}
	for v, id := range m.sessions {
		before[v] = id
	}

	m.setDraft("/seat codex")
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
			t.Errorf("%s's process was killed by an unseat", v)
		}
	}
	for i := range m.st.Columns {
		if m.st.Columns[i].Cleared {
			t.Errorf("%s was marked cleared by an unseat", m.st.Columns[i].Label)
		}
	}
}

// TestSeatAllPutsEveryoneBack. Reversibility is by construction rather than by a
// resume that could fail: nothing was torn down, so coming back cannot go wrong.
func TestSeatAllPutsEveryoneBack(t *testing.T) {
	m := seatModel()
	m.setDraft("/seat codex")
	m.roomCommand()

	m.setDraft("/seat all")
	if !m.roomCommand() {
		t.Fatal("/seat all was not intercepted")
	}
	if len(seatedNow(m)) < 3 {
		t.Fatalf("seated = %v after /seat all", seatedNow(m))
	}
	if m.sessions[model.VendorClaude] != "claude-thread" {
		t.Error("claude came back without the thread it left with")
	}
}

// TestSeatRefusesATypo. A typo that silently seated a smaller room than asked
// for would be discovered several turns later as an answer that never came.
func TestSeatRefusesATypo(t *testing.T) {
	m := seatModel()
	was := seatedNow(m)

	m.setDraft("/seat claud")
	if !m.roomCommand() {
		t.Fatal("/seat with a typo dispatched to the vendors")
	}
	if got := seatedNow(m); len(got) != len(was) {
		t.Errorf("a typo changed the room: %v became %v", was, got)
	}
	if m.st.Draft != "/seat claud" {
		t.Errorf("the refused draft was discarded: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "claud") {
		t.Errorf("the refusal did not name what it did not recognise: %q", m.st.Notice)
	}
}

// TestSeatSharesTheMentionVocabulary. Two alias tables would let /seat agy work
// and @agy not, or the reverse, and the room would be teaching two names for one
// seat.
func TestSeatSharesTheMentionVocabulary(t *testing.T) {
	for alias, want := range map[string]model.VendorID{
		"agy":         model.VendorAntigravity,
		"antigravity": model.VendorAntigravity,
		"@codex":      model.VendorCodex,
	} {
		m := seatModel()
		m.setDraft("/seat " + alias)
		m.roomCommand()

		got := seatedNow(m)
		if len(got) != 1 || got[0] != want {
			t.Errorf("/seat %s seated %v, want %s", alias, got, want)
		}
	}
}

// TestSeatWarnsWhenItUnseatsTheDefaultRoute. Silence goes to claude, so a room
// without claude answers nothing until every brief is @mentioned. Dispatch would
// say so once per turn; saying it here is the difference between a rule learned
// now and one discovered on the next enter.
func TestSeatWarnsWhenItUnseatsTheDefaultRoute(t *testing.T) {
	m := seatModel()
	m.setDraft("/seat codex")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "claude") {
		t.Errorf("unseating claude said nothing about the default route: %q", m.st.Notice)
	}

	m2 := seatModel()
	m2.setDraft("/seat claude,codex")
	m2.roomCommand()
	if strings.Contains(m2.st.Notice, "not seated") {
		t.Errorf("a room that still seats claude was warned anyway: %q", m2.st.Notice)
	}
}

// TestSeatRefusesMidTurn. The grid for a turn in flight was decided at dispatch,
// so reseating under it would redraw the room around columns that are
// mid-answer. /cd's refusal, for /cd's reason.
func TestSeatRefusesMidTurn(t *testing.T) {
	m := seatModel()
	was := seatedNow(m)
	m.turn = &turnState{}

	m.setDraft("/seat codex")
	if !m.roomCommand() {
		t.Fatal("/seat dispatched during a turn")
	}
	if got := seatedNow(m); len(got) != len(was) {
		t.Errorf("the room was reseated mid-turn: %v became %v", was, got)
	}
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("the refusal did not say why: %q", m.st.Notice)
	}
}

// TestBareSeatReports, the way bare /cd and bare /trace do: a command that
// half-asks a question answers it rather than doing something.
func TestBareSeatReports(t *testing.T) {
	m := seatModel()
	was := seatedNow(m)

	m.setDraft("/seat")
	if !m.roomCommand() {
		t.Fatal("bare /seat was not intercepted")
	}
	if got := seatedNow(m); len(got) != len(was) {
		t.Error("bare /seat changed the room instead of reporting it")
	}
	if !strings.Contains(m.st.Notice, "seated:") {
		t.Errorf("bare /seat did not report who is seated: %q", m.st.Notice)
	}
}

// TestSeatWillNotEmptyTheRoom. A list of nothing but separators is a slip, and a
// room with no seats can answer nothing at all.
func TestSeatWillNotEmptyTheRoom(t *testing.T) {
	m := seatModel()
	m.setDraft("/seat ,,")
	m.roomCommand()

	if len(seatedNow(m)) == 0 {
		t.Fatal("/seat ,, emptied the room")
	}
	if !strings.Contains(m.st.Notice, "at least one") {
		t.Errorf("the refusal did not say what was wrong: %q", m.st.Notice)
	}
}
