package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// killSession records the Kill that clearSeat owes a persistent seat.
//
// Separate from persistent_test.go's decisionSession, whose Kill is a no-op:
// the property under test here IS the kill, and a stub that silently accepts it
// would assert the call instead of the effect — the failure mode telltale's own
// CLAUDE.md names as this repo's recorded one.
type killSession struct {
	killed bool
	sent   [][]byte
}

func (s *killSession) SendTurn(lines [][]byte) error  { return s.record(lines) }
func (s *killSession) SendAside(lines [][]byte) error { return s.record(lines) }
func (s *killSession) record(lines [][]byte) error {
	for _, l := range lines {
		s.sent = append(s.sent, append([]byte(nil), l...))
	}
	return nil
}
func (s *killSession) Kill()       { s.killed = true }
func (s *killSession) Alive() bool { return !s.killed }

// clearModel is a three-seat room where every seat holds a thread.
//
// Turn is 0 so saveRoom returns before it touches disk. The one test that cares
// about persistence sets it and redirects HOME itself.
func clearModel() *Model {
	st := room()
	for i := range st.Columns {
		st.Columns[i].Restored = true
	}
	return &Model{
		st:     st,
		glyphs: GlyphsFor(false),
		sessions: map[model.VendorID]string{
			model.VendorClaude:      "claude-thread",
			model.VendorCodex:       "codex-thread",
			model.VendorAntigravity: "agy-thread",
		},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		procs:      map[model.VendorID]*seatProc{},
		turns:      map[model.VendorID]*turnState{},
		cancelling: map[model.VendorID]bool{},
		givenUp:    map[model.VendorID]bool{},
	}
}

// TestClearSeatDropsEveryResumeHandle pins the three maps together.
//
// Three rather than one because an id left in ANY of them is rebuilt into the
// next invocation — resumeIDs feeds the persistent spawn, sessions feeds
// specFor, and unproven decides whether a failure drops the id or keeps it — so
// a clear that forgot one would end the thread on screen and go on resuming it
// on the wire.
func TestClearSeatDropsEveryResumeHandle(t *testing.T) {
	m := clearModel()
	m.resumeIDs[model.VendorCodex] = "codex-thread"
	m.unproven[model.VendorCodex] = true

	m.clearSeat(model.VendorCodex)

	if got := m.sessions[model.VendorCodex]; got != "" {
		t.Errorf("sessions still holds %q", got)
	}
	if got := m.resumeIDs[model.VendorCodex]; got != "" {
		t.Errorf("resumeIDs still holds %q", got)
	}
	if m.unproven[model.VendorCodex] {
		t.Error("unproven still marks the cleared seat")
	}
	if got := m.sessions[model.VendorClaude]; got != "claude-thread" {
		t.Errorf("claude's thread was disturbed: %q", got)
	}
	if got := m.sessions[model.VendorAntigravity]; got != "agy-thread" {
		t.Errorf("antigravity's thread was disturbed: %q", got)
	}
}

// TestClearSeatKillsThePersistentProcessAndDoesNotRearmTheThread is the ordering
// property, and it is the one thing in this feature that fails silently.
//
// seatProcess re-arms resumeIDs from m.sessions whenever it replaces a live
// process, which is what carries a thread across a /cd. A clear that killed
// before it deleted would hand the id straight back, the next brief would resume
// the conversation the user just ended, and everything on screen would still say
// cleared.
func TestClearSeatKillsThePersistentProcessAndDoesNotRearmTheThread(t *testing.T) {
	m := clearModel()
	sess := &killSession{}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: sess}

	m.clearSeat(model.VendorClaude)

	if !sess.killed {
		t.Error("the persistent process outlived the clear")
	}
	if _, ok := m.procs[model.VendorClaude]; ok {
		t.Error("the dead process is still registered — the next turn would reuse it")
	}
	if got := m.resumeIDs[model.VendorClaude]; got != "" {
		t.Errorf("the thread was re-armed as %q — the next brief would resume it", got)
	}
	if got := m.sessions[model.VendorClaude]; got != "" {
		t.Errorf("sessions still holds %q", got)
	}
}

// TestClearSeatMarksOnlyTheClearedColumn keeps the marker per seat.
//
// Same argument Restored records: a room-level flag would let a seat that still
// holds a thread be read as having lost one.
func TestClearSeatMarksOnlyTheClearedColumn(t *testing.T) {
	m := clearModel()
	m.clearSeat(model.VendorCodex)

	for _, c := range m.st.Columns {
		cleared := c.Vendor == model.VendorCodex
		if c.Cleared != cleared {
			t.Errorf("%s: Cleared = %v, want %v", c.Vendor, c.Cleared, cleared)
		}
		if c.Vendor == model.VendorCodex && c.Restored {
			t.Error("a cleared seat still claims a restored thread")
		}
		if c.Vendor != model.VendorCodex && !c.Restored {
			t.Errorf("%s lost its restored mark", c.Vendor)
		}
	}
}

// TestClearedAndNeverHadAThreadRenderDifferently is zero-vs-absent (§4a.1)
// applied to a conversation.
//
// "This seat never had a thread" and "you ended this seat's thread" arrive at
// the same next brief, and collapsing them into one frame is the regression this
// repo exists to prevent — the same class as a 0% gauge drawn like an absent one.
func TestClearedAndNeverHadAThreadRenderDifferently(t *testing.T) {
	st := room()
	never := render(st)

	st.Columns[1].Cleared = true
	cleared := render(st)

	if never == cleared {
		t.Fatal("a cleared seat renders identically to one that never had a thread")
	}
	if !strings.Contains(cleared, "thread cleared") {
		t.Error("the cleared frame does not say so")
	}
	if strings.Contains(never, "thread cleared") {
		t.Error("an untouched room claims a cleared thread")
	}
}

// TestClearedMarkerRetiresOnTheNextTurn stops the marker outliving its claim.
//
// Once the brief is sent the seat HAS a thread again, so a line still reporting
// a break would describe one the room has already healed.
func TestClearedMarkerRetiresOnTheNextTurn(t *testing.T) {
	c := &Column{Vendor: model.VendorCodex, Cleared: true}
	c.startTurn(2, "next brief", false)
	if c.Cleared {
		t.Error("the marker survived the brief it was warning about")
	}
}

func TestAskClearSeatRefusesWhileATurnIsInFlight(t *testing.T) {
	m := clearModel()
	occupy(m)

	m.askClearSeat()

	if m.clearPending != "" {
		t.Errorf("armed a clear mid-turn on %s", m.clearPending)
	}
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("notice does not explain the refusal: %q", m.st.Notice)
	}
	if m.sessions[model.VendorClaude] == "" {
		t.Error("a thread was dropped despite the refusal")
	}
}

// TestAskClearSeatSaysSoWhenThereIsNothingToClear refuses rather than arming a
// card whose y would do nothing — which teaches that the key is unreliable
// instead of that the seat is empty.
func TestAskClearSeatSaysSoWhenThereIsNothingToClear(t *testing.T) {
	m := clearModel()
	m.sessions = map[model.VendorID]string{}

	m.askClearSeat()

	if m.clearPending != "" {
		t.Errorf("armed a clear with no thread to drop: %s", m.clearPending)
	}
	if !strings.Contains(m.st.Notice, "no thread to clear") {
		t.Errorf("notice does not name the reason: %q", m.st.Notice)
	}
}

// TestSeatHasThreadCountsALiveProcess covers the persistent seat's first turn:
// the conversation lives in the process before any session id is reported, so a
// check that only read the maps would call an answering seat empty.
func TestSeatHasThreadCountsALiveProcess(t *testing.T) {
	m := clearModel()
	m.sessions = map[model.VendorID]string{}
	if m.seatHasThread(model.VendorClaude) {
		t.Fatal("a seat with no id and no process reports a thread")
	}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: &killSession{}}
	if !m.seatHasThread(model.VendorClaude) {
		t.Error("a live process is not counted as a thread")
	}
}

func TestClearGate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		press   string
		cleared bool
		notice  string
	}{
		{"y clears", "y", true, "cleared"},
		{"n keeps", "n", false, "kept"},
		// Anything else cancels rather than falling through to viewKey: this gate
		// blocks nothing, so the safe reading of a key nobody meant to press is
		// to put the thread back out of reach.
		{"a stray key cancels", "j", false, "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := clearModel()
			m.st.Focus = 1
			m.askClearSeat()
			if m.clearPending != model.VendorCodex {
				t.Fatalf("c did not arm the focused seat: %q", m.clearPending)
			}

			m.clearGateKey(key(tc.press))

			if m.clearPending != "" {
				t.Error("the gate is still pending after an answer")
			}
			got := m.sessions[model.VendorCodex] == ""
			if got != tc.cleared {
				t.Errorf("cleared = %v, want %v", got, tc.cleared)
			}
			if !strings.Contains(m.st.Notice, tc.notice) {
				t.Errorf("notice %q does not contain %q", m.st.Notice, tc.notice)
			}
		})
	}
}

// TestClearIsViewModeOnly keeps the contract q and f already keep: in compose,
// a letter is a letter.
func TestClearIsViewModeOnly(t *testing.T) {
	m := clearModel()
	m.st.Mode = ModeComposing

	m.key(key("c"))

	if m.clearPending != "" {
		t.Errorf("c armed a clear while composing: %s", m.clearPending)
	}
	if !strings.Contains(m.st.Draft, "c") {
		t.Errorf("c was swallowed instead of typed: draft = %q", m.st.Draft)
	}
}

// TestClearSeatPersistsTheDrop is the half that memory alone cannot carry.
//
// The saved room is what a reattach reads, so a clear held only in the model
// would be undone by quitting — the user ends a thread and finds it waiting for
// them, which is the failure the whole control was built to remove.
func TestClearSeatPersistsTheDrop(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)

	m := clearModel()
	m.st.Turn = 3

	m.clearSeat(model.VendorCodex)

	if m.saveErr != nil {
		t.Fatalf("save failed: %v", m.saveErr)
	}
	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got, ok := re.Room.Sessions[model.VendorCodex]; ok {
		t.Errorf("the saved room still holds the cleared thread: %q", got)
	}
	if got := re.Room.Sessions[model.VendorClaude]; got != "claude-thread" {
		t.Errorf("the saved room lost an untouched seat: %q", got)
	}
}

// TestClearedFrame is the whole room with one seat's thread ended, and it is a
// golden because the property is a LAYOUT one: the marker has to arrive below
// the turns it follows, in the transcript's own grammar, without displacing the
// two seats either side of it.
//
// Built on `talking` — a seat mid-conversation is the only state where clearing
// means anything, and a fixture with nothing above the marker would pin the one
// case that cannot regress.
func TestClearedFrame(t *testing.T) {
	st := talking()
	// Claude is the seat with three turns behind it, which is what makes the
	// "the record stays, the thread does not" claim visible in one frame.
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Cleared = true
	st.Columns[1].Phase = PhaseDone
	golden(t, "thread-cleared", render(st))
}
