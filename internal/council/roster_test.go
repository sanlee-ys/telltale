package council

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The room remembers its SHAPE and never its AUTHORITY (§9.32). Everything in
// this file is one half of that line or the other: who was at the table comes
// back, who held the pen does not.

// dispatchedRoom is a model that has completed a turn, which is the only room
// that saves anything — saveRoom writes nothing at turn 0. Built directly
// rather than through Run, because Run enters the alternate screen.
func dispatchedRoom(t *testing.T, opts Options) *Model {
	t.Helper()
	tempHome(t)
	m := newWithBrief(opts, Brief{}, GateHook{}, Reattachment{})
	m.st.Turn = 1
	m.sessions[model.VendorClaude] = "claude-sess-1"
	return m
}

// savedNow reads the room file back off disk. Every assertion about persistence
// in here goes through this rather than through m.st, because the state is not
// the claim — a roster held in memory is exactly the bug §9.32 fixes.
func savedNow(t *testing.T) SavedRoom {
	t.Helper()
	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("nothing was saved: %v", err)
	}
	if !re.Active() {
		t.Fatalf("the saved room is not usable: %q", re.Ignored)
	}
	return re.Room
}

// --- the roster survives the file ----------------------------------------

// TestTheRosterRoundTrips. The narrowed room is the one worth pinning: `all`
// and the default table are both recoverable by typing, and a hand-picked set
// is the one a restart used to cost.
func TestTheRosterRoundTrips(t *testing.T) {
	tempHome(t)
	want := savedRoom(resolveWorkspace(""))
	want.Seats = Seats{Only: []model.VendorID{model.VendorClaude, model.VendorAntigravity}}

	if err := SaveRoom(want); err != nil {
		t.Fatal(err)
	}
	got := savedNow(t).Seats
	if !sameSeats(got, want.Seats) {
		t.Fatalf("roster = %+v, want %+v", got, want.Seats)
	}
	// Order is part of the roster: it is the order the grid draws.
	if got.Only[0] != model.VendorClaude || got.Only[1] != model.VendorAntigravity {
		t.Errorf("the roster came back reordered: %+v", got.Only)
	}
}

// TestSeatAllRoundTripsDistinctlyFromTheDefaultRoom. `/seat all` keeps the
// undrivable seats on screen and the default room folds them away, so a file
// that collapsed the two would put a user back in a room they had explicitly
// typed their way out of.
func TestSeatAllRoundTripsDistinctlyFromTheDefaultRoom(t *testing.T) {
	tempHome(t)
	room := savedRoom(resolveWorkspace(""))
	room.Seats = Seats{All: true}
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}
	if !savedNow(t).Seats.All {
		t.Error("/seat all came back as the default room")
	}

	room.Seats = Seats{}
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}
	if back := savedNow(t).Seats; back.typed() {
		t.Errorf("the default room came back as a typed roster: %+v", back)
	}
}

// TestARoomFileWithoutARosterIsUnchanged is the back-compat contract, stated as
// the thing it protects: a room.json written before §9.32 must not change
// meaning because a field was added to the struct that reads it. An old file
// has no `seats` key, that decodes to the zero Seats, and the zero Seats is the
// full detected table — which is exactly what that file has always opened.
func TestARoomFileWithoutARosterIsUnchanged(t *testing.T) {
	tempHome(t)
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(strings.TrimSuffix(path, roomFile), 0o700); err != nil {
		t.Fatal(err)
	}
	// Hand-written rather than round-tripped, because a room saved by THIS build
	// would carry the field and prove nothing. These are the v2 keys as they
	// stood before the roster joined them.
	old := `{
  "version": 2,
  "workspace": ` + quoteJSON(t, resolveWorkspace("")) + `,
  "posture": "read",
  "turn": 4,
  "sessions": {"claude": "claude-sess-1"},
  "saved_at": "2026-08-04T12:00:00Z"
}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	room := savedNow(t)
	if room.Seats.typed() {
		t.Fatalf("a file with no roster restored one: %+v", room.Seats)
	}
	// And the room it opens is the detected table, seat for seat.
	withFile := stateWith(Options{Seats: seatsFor(Seats{}, room.Seats, true)}, false)
	plain := stateWith(Options{}, false)
	if len(withFile.VisibleColumns()) != len(plain.VisibleColumns()) {
		t.Errorf("an old file changed who is drawn: %d columns, want %d",
			len(withFile.VisibleColumns()), len(plain.VisibleColumns()))
	}
}

// TestAnUnknownSeatIsDroppedNotObeyed. The roster is the one restored field
// whose value is a NAME, so it is the one a hand-edit can fill with a word this
// build has no seat for. Obeying it would seat nobody, fall through to the
// everything-collapsed table, and hand the user the default room while the file
// claimed a narrowed one.
func TestAnUnknownSeatIsDroppedNotObeyed(t *testing.T) {
	mixed := knownSeats(Seats{Only: []model.VendorID{model.VendorClaude, "chatgpt"}})
	if len(mixed.Only) != 1 || mixed.Only[0] != model.VendorClaude {
		t.Errorf("roster = %+v, want claude alone", mixed)
	}
	if got := knownSeats(Seats{Only: []model.VendorID{"chatgpt"}}); got.typed() {
		t.Errorf("a roster of nothing this build knows survived as %+v", got)
	}
	// A roster is SHAPE: dropping the seating plan must not cost the sessions.
	tempHome(t)
	room := savedRoom(resolveWorkspace(""))
	room.Seats = Seats{Only: []model.VendorID{"chatgpt"}}
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}
	back := savedNow(t)
	if back.Sessions[model.VendorClaude] != "claude-sess-1" {
		t.Error("an unreadable roster cost the room its threads")
	}
}

// --- /seat writes the file, now -------------------------------------------

// TestSeatSavesImmediately is `c`'s argument applied to the roster: the room
// file is what a reattach reads, so a change held only in memory is undone by
// quitting. Witnessed on the FILE — asserting on m.st.Seats would pass with no
// persistence at all, which is precisely the state this fixes.
func TestSeatSavesImmediately(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	m.setDraft("/seat claude,agy")
	if !m.roomCommand() {
		t.Fatal("/seat was not intercepted")
	}

	got := savedNow(t).Seats
	want := Seats{Only: []model.VendorID{model.VendorClaude, model.VendorAntigravity}}
	if !sameSeats(got, want) {
		t.Fatalf("the file says %+v, the room says %+v", got, m.st.Seats)
	}
}

// TestSeatAllSavesImmediately, because putting everyone back is a roster change
// in the widening direction and the save is an observation of movement rather
// than a call inside one command.
func TestSeatAllSavesImmediately(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	m.setDraft("/seat codex")
	m.roomCommand()
	m.setDraft("/seat all")
	m.roomCommand()

	if !savedNow(t).Seats.All {
		t.Error("/seat all was not saved")
	}
}

// TestARosterThatDidNotMoveIsNotRewritten. Bare `/seat`, a typo and a mid-turn
// `/seat` all report without reseating; rewriting the file on each would
// refresh SavedAt — the age a reattach shows — for a room that did nothing.
func TestARosterThatDidNotMoveIsNotRewritten(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	m.setDraft("/seat codex")
	m.roomCommand()
	first := savedNow(t).SavedAt

	for _, draft := range []string{"/seat", "/seat claud", "/seat ,,"} {
		m.setDraft(draft)
		m.roomCommand()
	}
	if got := savedNow(t).SavedAt; !got.Equal(first) {
		t.Errorf("a report rewrote the room file: %v became %v", first, got)
	}
}

// TestAnUnchangedRosterIsNotSavedByAnotherRoomCommand. The save watches the
// roster and not the command, so /read, /write and /trace must leave the file
// where it is. This is also what keeps the choke point cheap enough to sit on
// every room command.
func TestAnUnchangedRosterIsNotSavedByAnotherRoomCommand(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	m.setDraft("/seat codex")
	m.roomCommand()
	first := savedNow(t).SavedAt

	m.setDraft("/read")
	m.roomCommand()
	if got := savedNow(t).SavedAt; !got.Equal(first) {
		t.Errorf("/read rewrote the room file: %v became %v", first, got)
	}
}

// TestEveryRosterChangeGoesThroughOneChokePoint.
//
// The persistence is on roomCommand rather than inside seatCommand so that a
// narrowing command written later inherits it without being told this wrapper
// exists. `/unseat` was the one in flight when this was written and has since
// landed (§9.31) having needed no change here, which is the prediction coming
// good; TestUnseatIsPersistedByTheChokePoint asserts it against the file. This
// one stays on `/seat` alone, so the choke point is still pinned by a test that
// does not depend on which commands happen to exist.
func TestEveryRosterChangeGoesThroughOneChokePoint(t *testing.T) {
	m := dispatchedRoom(t, Options{})
	for _, step := range []struct {
		draft string
		want  Seats
	}{
		{"/seat claude,codex,agy", Seats{Only: []model.VendorID{
			model.VendorClaude, model.VendorCodex, model.VendorAntigravity}}},
		{"/seat claude,agy", Seats{Only: []model.VendorID{
			model.VendorClaude, model.VendorAntigravity}}},
		{"/seat claude", Seats{Only: []model.VendorID{model.VendorClaude}}},
		{"/seat all", Seats{All: true}},
	} {
		m.setDraft(step.draft)
		if !m.roomCommand() {
			t.Fatalf("%q was not intercepted", step.draft)
		}
		if got := savedNow(t).Seats; !sameSeats(got, step.want) {
			t.Errorf("after %q the file says %+v, want %+v", step.draft, got, step.want)
		}
	}
}

// --- who outranks whom at the door ----------------------------------------

// TestLaunchVendorOverridesTheSavedRoster is the §9.32 rule and `--cd`'s
// reasoning: a control someone typed today outranks a file from yesterday.
func TestLaunchVendorOverridesTheSavedRoster(t *testing.T) {
	saved := Seats{Only: []model.VendorID{model.VendorCodex}}
	typed := Seats{Only: []model.VendorID{model.VendorClaude, model.VendorAntigravity}}

	if got := seatsFor(typed, saved, true); !sameSeats(got, typed) {
		t.Errorf("seats = %+v, want the typed --vendor %+v", got, typed)
	}
	if got := seatsFor(Seats{All: true}, saved, true); !got.All {
		t.Error("--vendor all lost to the saved roster")
	}
}

// TestReattachSeatsTheSavedRoster: nothing typed, so the file answers.
func TestReattachSeatsTheSavedRoster(t *testing.T) {
	saved := Seats{Only: []model.VendorID{model.VendorCodex}}
	if got := seatsFor(Seats{}, saved, true); !sameSeats(got, saved) {
		t.Errorf("seats = %+v, want the saved %+v", got, saved)
	}
	// A room --fresh declined restores neither half of the shape.
	if got := seatsFor(Seats{}, saved, false); got.typed() {
		t.Errorf("a declined room seated its roster anyway: %+v", got)
	}
}

// TestLaunchVendorRewritesTheSavedRoster. Overriding is only half of it: the
// next save has to record the room the user actually got, or the file goes on
// describing a room that is not on screen and the NEXT launch restores it.
func TestLaunchVendorRewritesTheSavedRoster(t *testing.T) {
	typed := Seats{Only: []model.VendorID{model.VendorClaude}}
	m := dispatchedRoom(t, Options{Seats: typed})
	m.saveRoom()

	if got := savedNow(t).Seats; !sameSeats(got, typed) {
		t.Errorf("the file says %+v, want the typed --vendor %+v", got, typed)
	}
}

// --- authority is never restored ------------------------------------------

// TestReattachRestoresNoPostureAndNoGate is the safety property §9.32 leaves
// exactly where it found it. A write room saved to disk reopens read, and a
// room saved with the gate off reopens asking — both because the file said so
// about yesterday and neither is something anyone typed today.
//
// Witnessed on the rendered frame as well as the fields: the WRITE marker is
// the thing a user reads to know what the room may do.
func TestReattachRestoresNoPostureAndNoGate(t *testing.T) {
	tempHome(t)
	room := savedRoom(resolveWorkspace(""))
	room.Posture = "write" // write AND ungated: the loudest room there is

	m := reattachedModel(t, room, Options{})
	if m.st.Write {
		t.Error("write posture arrived from a file instead of from a flag")
	}
	if !m.st.Asking() {
		t.Error("the gate was turned off by a file")
	}
	if strings.Contains(Render(m.st, PlainStyles(), GlyphsFor(false)), "WRITE ") {
		t.Error("a reattached read room renders the WRITE marker")
	}

	// The other direction, so this cannot pass by the room being read-only for
	// some reason of its own: the flags, and only the flags, decide.
	flagged := reattachedModel(t, savedRoom(resolveWorkspace("")), Options{Write: true, Auto: true})
	if !flagged.st.Write || flagged.st.Asking() {
		t.Error("the typed --write --auto did not decide the reattached room")
	}
}

// TestSavedPostureRecordsTheRoomAsItStood is the incident, in one test.
//
// A gated write room told `a` is ungated, and the field used to be written from
// m.st.Write (live) beside m.opts.Auto (the launch flag) — so the file said
// "write-gated" about a room with nothing left asking. §9.17's own rule is what
// it broke: a flag with an in-room twin is only the seed.
func TestSavedPostureRecordsTheRoomAsItStood(t *testing.T) {
	for _, c := range []struct {
		name  string
		opts  Options
		write bool
		off   bool
		want  string
	}{
		// Opened gated, then `a`. The file used to say write-gated.
		{"a in a gated write room", Options{Write: true}, true, true, "write"},
		// Opened ungated, then `a` again to turn asking back on — `a` is not a
		// one-way door, and the file used to say "write" through it.
		{"a back on in an auto room", Options{Write: true, Auto: true}, true, false, "write-gated"},
		{"untouched gated write", Options{Write: true}, true, false, "write-gated"},
		{"untouched auto write", Options{Write: true, Auto: true}, true, true, "write"},
		// /read wins over both, and an ungated read room is still just read:
		// there is no authority left for a gate to be guarding.
		{"/read after a", Options{Write: true, Auto: true}, false, true, "read"},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := dispatchedRoom(t, c.opts)
			m.applyPosture(c.write)
			m.st.GateOff = c.off
			m.saveRoom()

			if got := savedNow(t).Posture; got != c.want {
				t.Errorf("the file says %q, the room stood %q", got, c.want)
			}
		})
	}
}

// TestTheMismatchNoticeCannotFireSpuriously. The saved posture's ONLY consumer
// is this notice, which is why the writer and every reader had to move at once:
// a room saved live and read against the launch flags would report a change to a
// user who made none.
//
// Round-tripped rather than hand-set, because the property is that the two sides
// agree — a fixture asserting a string would pass with both of them wrong.
func TestTheMismatchNoticeCannotFireSpuriously(t *testing.T) {
	for _, c := range []struct {
		name string
		opts Options
		off  bool
	}{
		{"gated write", Options{Write: true}, false},
		{"ungated write", Options{Write: true, Auto: true}, true},
		{"read", Options{}, false},
		// The `a` room: opened gated, ungated by a keystroke, reopened with the
		// flags that describe what it BECAME. Nothing changed, so nothing is said.
		{"a in a gated write room, reopened as what it became", Options{Write: true}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := dispatchedRoom(t, c.opts)
			m.st.GateOff = c.off
			m.saveRoom()
			saved := savedNow(t)

			// Reopened with flags naming the room that was saved.
			reopened := reattachedModel(t, saved, Options{
				Write: m.st.Write, Auto: !m.st.Asking()})
			if strings.Contains(reopened.st.Notice, "it ran ") {
				t.Errorf("an unchanged room was told its posture moved: %q", reopened.st.Notice)
			}
		})
	}
}

// TestTheMismatchNoticeStillFiresWhenItShould. The other half: the notice exists
// because a user reattaching a write room without retyping --write should learn
// it from the room rather than from a vendor refusing to edit a file. Making it
// quiet is not the fix.
func TestTheMismatchNoticeStillFiresWhenItShould(t *testing.T) {
	tempHome(t)
	room := savedRoom(resolveWorkspace(""))
	room.Posture = "write"

	m := reattachedModel(t, room, Options{})
	if !strings.Contains(m.st.Notice, "it ran write") {
		t.Errorf("a write room reattached read said nothing: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "this room is read") {
		t.Errorf("the notice does not say what this room is: %q", m.st.Notice)
	}

	// And the gate alone is a real difference, now that the field records it:
	// "write" and "write-gated" are two different rooms to hand four seats.
	gated := savedRoom(resolveWorkspace(""))
	gated.Posture = "write-gated"
	g := reattachedModel(t, gated, Options{Write: true, Auto: true})
	if !strings.Contains(g.st.Notice, "it ran write-gated") {
		t.Errorf("a gated room reopened ungated said nothing: %q", g.st.Notice)
	}
}

// quoteJSON is a JSON string literal for a path a Windows test will hand it
// with backslashes in.
func quoteJSON(t *testing.T, s string) string {
	t.Helper()
	buf, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(buf)
}
