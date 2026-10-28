package council

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheRoomReadsWhatTheHostWrote holds the two writers of room.json
// together.
//
// The host cannot import this package (boundary_test.go), so it writes the
// file through its own RoomFile rather than through SaveRoom. That is two
// encodings of one file, and two encodings drift unless something reads one
// through the other. This is that something: a file the HOST wrote, read by
// LoadRoom — the loader the next `telltale council` uses — field by field. A
// version bump, a renamed key or a shape change on either side reddens it.
func TestTheRoomReadsWhatTheHostWrote(t *testing.T) {
	tempHome(t)
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	savedAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	if err := councilhost.WriteRoomFile(filepath.Dir(path), councilhost.RoomFile{
		Workspace: "/home/dev/code/telltale",
		Seats:     councilhost.RoomRoster{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}},
		Posture:   "read",
		Turn:      4,
		Sessions: map[model.VendorID]string{
			model.VendorClaude: "sess-hosted-claude",
			model.VendorCodex:  "sess-hosted-codex",
		},
		SavedAt: savedAt,
	}); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("the room could not read what the host wrote: %v", err)
	}
	if !re.Active() {
		t.Fatalf("the host's file was refused: %q", re.Ignored)
	}
	r := re.Room
	if r.Version != roomVersion {
		t.Fatalf("the host writes schema v%d and the room reads v%d", r.Version, roomVersion)
	}
	if r.Workspace != "/home/dev/code/telltale" || r.Turn != 4 || r.Posture != "read" || !r.SavedAt.Equal(savedAt) {
		t.Fatalf("a field lost its value on the way through: %+v", r)
	}
	if r.Sessions[model.VendorClaude] != "sess-hosted-claude" || r.Sessions[model.VendorCodex] != "sess-hosted-codex" {
		t.Fatalf("the session ids did not come through: %+v", r.Sessions)
	}
	if len(r.Seats.Only) != 2 || r.Seats.Only[0] != model.VendorClaude || r.Seats.Only[1] != model.VendorCodex {
		t.Fatalf("the roster did not come through as the typed list: %+v", r.Seats)
	}
	if r.BriefPath != "" {
		t.Fatalf("a brief path appeared from a host that takes no brief: %q", r.BriefPath)
	}

	// And the room REATTACHES from it exactly as from its own file: the turn
	// count and the threads, with the seated columns marked.
	m := newWithBrief(Options{Dir: "/home/dev/code/telltale"}, Brief{}, GateHook{}, re)
	if m.st.Turn != 4 || m.sessions[model.VendorClaude] != "sess-hosted-claude" {
		t.Fatalf("the room did not reattach from the host's file: turn %d, sessions %+v", m.st.Turn, m.sessions)
	}
}

// TestStartHostedHandsTheSavedKeysToTheHost pins the client half of the
// resume: a saved room's turn, timestamp and session ids ride the
// ClientConfig to the host, and `--fresh` declines them exactly as it declines
// them in the single-process room. The host spawn is stubbed, so nothing runs;
// what is asserted is what WOULD have been sent.
func TestStartHostedHandsTheSavedKeysToTheHost(t *testing.T) {
	tempHome(t)
	ws := t.TempDir()
	if err := SaveRoom(savedRoom(ws)); err != nil {
		t.Fatal(err)
	}
	orig := startHostedRoom
	var got []councilhost.ClientConfig
	startHostedRoom = func(cfg councilhost.ClientConfig) (*councilhost.Client, error) {
		got = append(got, cfg)
		return nil, errStubbedHost
	}
	t.Cleanup(func() { startHostedRoom = orig })

	var out bytes.Buffer
	if err := StartHosted(Options{Dir: ws}, strings.NewReader(""), &out); !errors.Is(err, errStubbedHost) {
		t.Fatalf("StartHosted did not reach the host spawn: %v", err)
	}
	if err := StartHosted(Options{Dir: ws, Fresh: true}, strings.NewReader(""), &out); !errors.Is(err, errStubbedHost) {
		t.Fatalf("StartHosted --fresh did not reach the host spawn: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two host launches, saw %d", len(got))
	}
	saved := got[0].Saved
	if saved == nil {
		t.Fatal("the saved room's keys were not handed to the host")
	}
	want := savedRoom(ws)
	if saved.Turn != want.Turn || !saved.SavedAt.Equal(want.SavedAt) {
		t.Fatalf("the saved turn did not travel: %+v", saved)
	}
	for v, id := range want.Sessions {
		if saved.Sessions[v] != id {
			t.Fatalf("the saved id for %s did not travel: %+v", v, saved.Sessions)
		}
	}
	if got[1].Saved != nil {
		t.Fatalf("--fresh still handed the host the saved keys: %+v", got[1].Saved)
	}
}

// TestAHostedRestoredSeatDrawsTheReattachCard: the host's Restored marker and
// its Reattached record reach the column and the State, so the hosted room
// draws council's own reattach card — and draws the OTHER card on a seat
// whose thread did not come back, the way the single-process room does.
func TestAHostedRestoredSeatDrawsTheReattachCard(t *testing.T) {
	r := hostedFixture()
	r.Turn = 3
	r.Reattached = &councilhost.Reattached{Turn: 3, SavedAt: time.Date(2026, 9, 2, 9, 0, 0, 0, time.UTC)}
	r.Seats[0].SessionID = "sess-restored"
	r.Seats[0].Restored = true
	st := hostedState(r)
	if !st.Reattached.Active() || st.Reattached.Turn != 3 {
		t.Fatalf("the saved room's provenance did not reach the State: %+v", st.Reattached)
	}
	if !st.Columns[0].Restored || st.Columns[1].Restored {
		t.Fatalf("Restored did not follow the wire: %+v / %+v", st.Columns[0].Restored, st.Columns[1].Restored)
	}
	got := render(st)
	if !strings.Contains(got, "this seat's thread came") {
		t.Fatalf("the restored seat did not draw the reattach card:\n%s", got)
	}
	if !strings.Contains(got, "no thread came back for") {
		t.Fatalf("a seat with no saved thread did not say so:\n%s", got)
	}

	// A later frame that lets the thread go takes the card with it: the
	// marker is the host's claim, never the previous column's.
	r.Seats[0].Restored = false
	r.Seats[0].SessionID = ""
	again := stateFromRoom(r, st, true)
	if again.Columns[0].Restored {
		t.Fatal("a dropped thread kept its Restored marker from the previous column")
	}
}
