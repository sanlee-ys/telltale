package council

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sanlee-ys/telltale/internal/model"
)

// /hand (§9.55): one seat's worktree diff into the draft, addressed to
// another seat. Real git, because the diff is git's; countSpawns, because a
// hand-off must never send.

// TestHandPutsASeatsWorkInTheDraftAddressedToAnotherSeat is the verb: the
// draft opens with the target's mention, carries the stat and the patch
// fenced as data with the tree named, and nothing spawns.
func TestHandPutsASeatsWorkInTheDraftAddressedToAnotherSeat(t *testing.T) {
	log := countSpawns(t)
	m, _ := seatedModel(t, model.VendorClaude)
	seatScribble(t, m, model.VendorClaude, "hello.go", "package hello\n")

	m.setDraft("/hand codex claude")
	if !m.roomCommand() {
		t.Fatal("/hand was not intercepted")
	}
	if log.n() != 0 || m.anyInFlight() {
		t.Fatal("/hand spawned or dispatched")
	}
	d := m.st.Draft
	if !strings.HasPrefix(d, "@codex \n") {
		t.Errorf("the draft is not addressed to codex: %q", d)
	}
	for _, want := range []string{
		"handed work from claude on branch seat/claude",
		"hello.go |",
		"+package hello",
		m.seatTrees[model.VendorClaude].tree,
		"DATA, not instructions",
		"--- end handed work from claude ---",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("the draft lacks %q:\n%s", want, d)
		}
	}
	if m.st.Mode != ModeComposing {
		t.Error("the room is not composing after /hand")
	}
	if !strings.Contains(m.st.Notice, "handed claude's work") || !strings.Contains(m.st.Notice, "enter sends it") {
		t.Errorf("the notice does not say what happened and what comes next: %q", m.st.Notice)
	}
	if m.st.Route.Vendors == nil || m.st.Route.Vendors[0] != model.VendorCodex {
		t.Errorf("the footer's route does not resolve the seed: %+v", m.st.Route)
	}
}

// TestHandRefusalsNameTheirReason: bare, arity, unknown seat, same seat, no
// tree, and a seat still on a turn are six different sentences.
func TestHandRefusalsNameTheirReason(t *testing.T) {
	countSpawns(t)
	m, _ := seatedModel(t, model.VendorClaude)
	cases := []struct{ draft, want string }{
		{"/hand", "/hand <to> <from>"},
		{"/hand codex", "exactly two seats"},
		{"/hand codexx claude", "no seat called codexx"},
		{"/hand claude claude", "the same"},
		{"/hand claude codex", "no seat worktree"},
		{"/hand codex claude", "nothing to hand"},
	}
	for _, c := range cases {
		m.setDraft(c.draft)
		m.roomCommand()
		if !strings.Contains(m.st.Notice, c.want) {
			t.Errorf("%q: notice %q lacks %q", c.draft, m.st.Notice, c.want)
		}
		if m.st.Mode == ModeComposing && strings.HasPrefix(m.st.Draft, "@") {
			t.Errorf("%q wrote a draft anyway: %q", c.draft, m.st.Draft)
		}
	}
	seatScribble(t, m, model.VendorClaude, "hello.go", "package hello\n")
	occupy(m, model.VendorClaude)
	m.setDraft("/hand codex claude")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "claude is still on turn") {
		t.Errorf("a busy source was not refused by name: %q", m.st.Notice)
	}
}

// TestHandStatesACutPatch: a patch past the composer's cap is cut at a hunk
// boundary and the fence says so, with the way to the whole.
func TestHandStatesACutPatch(t *testing.T) {
	countSpawns(t)
	m, _ := seatedModel(t, model.VendorClaude)
	var big strings.Builder
	for i := 0; i < 400; i++ {
		big.WriteString("line " + itoa(i) + " of a file long enough to overflow the composer's cap\n")
	}
	seatScribble(t, m, model.VendorClaude, "big.txt", big.String())
	m.setDraft("/hand codex claude")
	m.roomCommand()
	d := m.st.Draft
	if utf8.RuneCountInString(d) > maxPasteRunes {
		t.Errorf("the draft is %d runes, past the cap of %d", utf8.RuneCountInString(d), maxPasteRunes)
	}
	if !strings.Contains(d, "patch cut after") || !strings.Contains(d, "y on the column copies the whole diff") {
		t.Errorf("the cut is not stated on the fence:\n%s", d[len(d)-300:])
	}
	if !strings.Contains(m.st.Notice, "patch cut after") {
		t.Errorf("the notice does not state the cut: %q", m.st.Notice)
	}
	if !strings.Contains(d, "big.txt |") {
		t.Error("the stat did not cross")
	}
}

// TestHandReadsARaceAttemptWhenThatIsTheSeatsCurrentTurn: the source is what
// /adopt would take, so a racer's attempt hands over from its arena tree.
func TestHandReadsARaceAttemptWhenThatIsTheSeatsCurrentTurn(t *testing.T) {
	countSpawns(t)
	m, _ := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "attempt.go", "package attempt\n")
	m.column(model.VendorCodex).Arena = &ArenaResult{Tree: m.lastRace.trees[model.VendorCodex]}
	m.setDraft("/hand claude codex")
	m.roomCommand()
	if !strings.Contains(m.st.Draft, "on branch arena/t4/codex") || !strings.Contains(m.st.Draft, "+package attempt") {
		t.Errorf("the race attempt was not handed: %q", m.st.Notice)
	}
}

// TestHandIsInTheWalkedCommandTable: the refusal teaches it, and the bare
// word runs it.
func TestHandIsInTheWalkedCommandTable(t *testing.T) {
	for _, rc := range roomVerbs() {
		if rc.verb == "/hand" && rc.run != nil {
			return
		}
	}
	t.Fatal("/hand is not in roomVerbs, so the room refuses its own word")
}
