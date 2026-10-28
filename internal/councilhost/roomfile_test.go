package councilhost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// hostOverSavedRoom builds an in-process host with one claude seat, a council
// directory of its own, and the saved keys handed over the way the first hello
// hands them. No vendor is spawned: countSpawns stubs the session, and the
// binary is one no machine resolves, so TestMain's guard lets the stub run.
//
// The council directory is a temp dir rather than ~/.telltale/council — the
// host takes it as Config.CouncilDir on purpose, and a test that let it find
// the operator's own would be the suite rewriting the file the operator's
// next `telltale council` reattaches from (CLAUDE.md's measured 2026-09-01
// defect, one package over).
func hostOverSavedRoom(t *testing.T, saved *SavedKeys) (*Host, string, *spawnLog) {
	t.Helper()
	log := countSpawns(t)
	stubRoomJob(t)
	dir := t.TempDir()
	h, err := New(Config{
		Workspace:  "/home/dev/code/telltale",
		PipeName:   "telltale-test-room-file",
		Posture:    vendors.PostureRead,
		Roster:     []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		CouncilDir: dir,
		Tick:       5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	// Cleanup waits for fold to return, not just for the cancel to be sent.
	// fold writes room.json synchronously after a seat retires, and a test
	// that ends on the seat's phase alone can return while that write still
	// holds its temp file. The TempDir cleanup registered above runs after
	// this one, so by then the directory is quiet. Windows refuses to remove
	// a directory with an open file in it; Linux does not, which is why this
	// only reddened CI.
	folded := make(chan struct{})
	go func() {
		defer close(folded)
		h.fold()
	}()
	t.Cleanup(func() {
		cancel()
		<-folded
	})
	h.reattach(saved)
	return h, dir, log
}

// claudeTurn feeds the fold exactly the lines a `--input-format stream-json`
// claude process emits for one turn that answered, parsed by the real adapter
// so the event shapes are the ones a live process produces.
func claudeTurn(t *testing.T, h *Host, sessionID string) {
	t.Helper()
	parse := vendors.Registry()[model.VendorClaude].ParseEvent
	for _, line := range []string{
		`{"type":"system","subtype":"init","session_id":"` + sessionID + `","model":"claude-opus-5"}`,
		`{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"the merger looks fine to me"}}}`,
		`{"type":"result","session_id":"` + sessionID + `","result":"the merger looks fine to me","total_cost_usd":0.01}`,
	} {
		ev, ok := parse([]byte(line))
		if !ok {
			t.Fatalf("the claude adapter dropped a line this test relies on: %s", line)
		}
		ev.Vendor = model.VendorClaude
		h.events <- ev
	}
}

// awaitRoomFile waits for room.json to exist and parses it.
func awaitRoomFile(t *testing.T, dir string) (RoomFile, []byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		b, err := os.ReadFile(RoomFilePath(dir))
		if err == nil {
			var f RoomFile
			if err := json.Unmarshal(b, &f); err != nil {
				t.Fatalf("room.json is not readable json: %v\n%s", err, b)
			}
			return f, b
		}
		if time.Now().After(deadline) {
			t.Fatalf("the host never wrote %s", RoomFilePath(dir))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTheHostWritesTheRoomFileWhenATurnEnds closes §7.31's named gap: the
// host writes `council/room.json` on the room's own schedule, so a hosted
// room's session ids reach disk and the next `telltale council` — hosted or
// not — rebuilds from them.
//
// It is pinned on the SERIALIZED form, the way every council write is: an
// allowlist of keys, the ids present, and the prompt and the reply absent.
// The prompt is the one field on the wire that carries what a person typed,
// and the body is the field the transport's descriptor exists for; neither
// may reach this file (resume.go, and the package doc's third rule).
func TestTheHostWritesTheRoomFileWhenATurnEnds(t *testing.T) {
	h, dir, _ := hostOverSavedRoom(t, nil)
	if _, err := os.Stat(RoomFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("room.json exists before any turn (stat: %v) — council never writes a room with no turns", err)
	}

	const prompt = "what should we do about the merger"
	h.dispatch(prompt, nil)
	claudeTurn(t, h, "sess-synth-1")
	awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseDone })

	f, raw := awaitRoomFile(t, dir)
	if f.Version != RoomFileVersion || f.Turn != 1 || f.Workspace != "/home/dev/code/telltale" || f.Posture != "read" {
		t.Fatalf("the room file does not describe the room: %+v", f)
	}
	if f.Sessions[model.VendorClaude] != "sess-synth-1" {
		t.Fatalf("the room file does not hold the session id it exists for: %+v", f)
	}
	if len(f.Seats.Only) != 1 || f.Seats.Only[0] != model.VendorClaude {
		t.Fatalf("the roster is not the host's: %+v", f.Seats)
	}
	if f.SavedAt.IsZero() {
		t.Fatal("the room file carries no timestamp")
	}

	body := string(raw)
	for _, forbidden := range []string{prompt, "the merger looks fine", "claude-opus-5"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("room.json leaked %q:\n%s", forbidden, body)
		}
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	// The exact key set council's own TestTheSavedRoomHoldsKeysAndNeverContent
	// pins, minus brief_path, which the host has no brief to carry. An
	// ALLOWLIST: a field added later with a content-shaped value fails here.
	want := map[string]bool{
		"version": true, "workspace": true, "seats": true, "posture": true,
		"turn": true, "sessions": true, "saved_at": true,
	}
	for k := range keys {
		if !want[k] {
			t.Errorf("room.json carries an unexpected key %q — is it content?", k)
		}
	}
	for k := range want {
		if _, ok := keys[k]; !ok {
			t.Errorf("room.json is missing key %q", k)
		}
	}
	if _, ok := keys["brief_path"]; ok {
		t.Error("room.json carries a brief_path; the host takes no brief")
	}
}

// TestTheHostsRoomFileIsNotWorldReadable: the ids are handles to the
// operator's private conversations, so the file is 0600 and the directory
// 0700, SaveRoom's own terms.
func TestTheHostsRoomFileIsNotWorldReadable(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRoomFile(dir, RoomFile{
		Workspace: "/home/dev/code", Posture: "read", Turn: 1,
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-1"},
		SavedAt:  time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(RoomFilePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() == 0 {
		t.Fatal("room.json is empty")
	}
	if os.Getenv("OS") == "Windows_NT" {
		// The mode is advisory there; the directory's ACL is the control.
		return
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("room.json is %v; it must be readable by its owner alone", perm)
	}
	left, _ := os.ReadDir(dir)
	for _, e := range left {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a temp file was left behind: %s", e.Name())
		}
	}
}

// TestASavedThreadIsResumedThroughTheHost closes §7.31's other named gap: a
// hosted room no longer starts every seat fresh. The saved id lands on the
// seat, marked Restored so the client draws the card, the turn count
// continues from the saved number, and the first dispatch spawns the vendor's
// RESUME shape with that id on its argv — which is the whole of what council's
// own reattach promises (program.go, persistent.go).
func TestASavedThreadIsResumedThroughTheHost(t *testing.T) {
	savedAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	h, _, log := hostOverSavedRoom(t, &SavedKeys{
		Turn: 7, SavedAt: savedAt,
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-saved-7"},
	})

	r := h.Snapshot()
	if r.Turn != 7 {
		t.Fatalf("the turn count did not continue from the saved room: %d", r.Turn)
	}
	if r.Reattached == nil || r.Reattached.Turn != 7 || !r.Reattached.SavedAt.Equal(savedAt) {
		t.Fatalf("the room does not carry its provenance: %+v", r.Reattached)
	}
	s := r.Seats[0]
	if s.SessionID != "sess-saved-7" || !s.Restored {
		t.Fatalf("the saved id did not land on the seat, marked restored: %+v", s)
	}

	h.dispatch("continue", nil)
	log.awaitN(t, 1)
	args := strings.Join(log.specs[0].Args, " ")
	if !strings.Contains(args, "--resume sess-saved-7") {
		t.Fatalf("the first turn did not resume the saved thread: argv %q", args)
	}
	if got := h.Snapshot().Turn; got != 8 {
		t.Fatalf("the first hosted brief is turn %d, want 8", got)
	}

	// The turn answered on the same id, so the thread is proven: the marker
	// stays and the file records the id at the new turn.
	claudeTurn(t, h, "sess-saved-7")
	seat := awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseDone })
	if !seat.Restored || seat.SessionID != "sess-saved-7" {
		t.Fatalf("a thread that answered was let go: %+v", seat)
	}
}

// TestARestoredThreadWhoseFirstTurnFailsIsLetGo is council's one-attempt rule
// on the host: a restored id whose first turn fails is dropped, with council's
// own words on the card, so the seat cannot wedge retrying a dead id on every
// turn for the life of the room. The file written afterwards no longer holds
// the id — which is what makes the drop survive the host.
func TestARestoredThreadWhoseFirstTurnFailsIsLetGo(t *testing.T) {
	h, dir, _ := hostOverSavedRoom(t, &SavedKeys{
		Turn: 3, SavedAt: time.Now(),
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-dead"},
	})
	h.dispatch("continue", nil)
	h.events <- runner.Event{Vendor: model.VendorClaude, Kind: runner.KindError,
		Err: errors.New("No conversation found with session ID: sess-dead"), ExitCode: 1}

	seat := awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseFailed })
	if seat.SessionID != "" || seat.Restored {
		t.Fatalf("the dead id was kept: %+v", seat)
	}
	if seat.Note != "thread not restored — starting fresh" {
		t.Fatalf("the card does not say what happens next: %q", seat.Note)
	}
	f, _ := awaitRoomFile(t, dir)
	if _, ok := f.Sessions[model.VendorClaude]; ok {
		t.Fatalf("the dropped id was written back to disk: %+v", f.Sessions)
	}
	if f.Turn != 4 {
		t.Fatalf("the file's turn is %d, want 4", f.Turn)
	}
}

// TestACancelledFirstTurnKeepsTheThreadOnProbation: the operator stopped it,
// so nothing was learned about the thread either way (council's
// settleRestoredThread, the cancelled case).
func TestACancelledFirstTurnKeepsTheThreadOnProbation(t *testing.T) {
	h, dir, _ := hostOverSavedRoom(t, &SavedKeys{
		Turn: 1, SavedAt: time.Now(),
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-kept"},
	})
	h.dispatch("continue", nil)
	awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseWaiting })
	h.interrupt(nil)
	seat := awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseCancelled })
	if seat.SessionID != "sess-kept" || !seat.Restored {
		t.Fatalf("a cancelled turn forfeited the thread: %+v", seat)
	}
	// The cancel retired the turn, so the room was saved with the id intact.
	f, _ := awaitRoomFile(t, dir)
	if f.Sessions[model.VendorClaude] != "sess-kept" {
		t.Fatalf("the file lost the id: %+v", f.Sessions)
	}
}

// TestAnUnseatedSavedIdSurvivesTheHostsWrite: council keeps every saved id
// whether or not the vendor is seated and writes them all back. A host whose
// roster is one seat must not cost the operator the other three threads on
// its first write.
func TestAnUnseatedSavedIdSurvivesTheHostsWrite(t *testing.T) {
	h, dir, _ := hostOverSavedRoom(t, &SavedKeys{
		Turn: 2, SavedAt: time.Now(),
		Sessions: map[model.VendorID]string{
			model.VendorClaude: "sess-claude",
			model.VendorCodex:  "sess-codex-unseated",
		},
	})
	if h.Snapshot().Seats[0].SessionID != "sess-claude" {
		t.Fatal("the seated id did not land")
	}
	h.dispatch("go", nil)
	claudeTurn(t, h, "sess-claude")
	awaitSeat(t, h, func(s Seat) bool { return s.Phase == PhaseDone })
	f, _ := awaitRoomFile(t, dir)
	if f.Sessions[model.VendorCodex] != "sess-codex-unseated" {
		t.Fatalf("the unseated vendor's thread was lost: %+v", f.Sessions)
	}
	if f.Sessions[model.VendorClaude] != "sess-claude" {
		t.Fatalf("the seated id was lost: %+v", f.Sessions)
	}
}

// TestNothingIsWrittenBeforeTheFirstHostedTurn: a reattach alone writes
// nothing, and neither does a fresh room that was only opened — council's
// rule that a file per accidental launch is refused. Shutdown over a
// reattached room DOES write, as council's quit path does, refreshing
// saved_at with every id intact.
func TestNothingIsWrittenBeforeTheFirstHostedTurn(t *testing.T) {
	fresh, dir, _ := hostOverSavedRoom(t, nil)
	fresh.Shutdown()
	if _, err := os.Stat(RoomFilePath(dir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a room with no turns wrote room.json on the way out (stat: %v)", err)
	}

	h, dir2, _ := hostOverSavedRoom(t, &SavedKeys{
		Turn: 5, SavedAt: time.Now().Add(-time.Hour),
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-5"},
	})
	time.Sleep(20 * time.Millisecond)
	if _, err := os.Stat(RoomFilePath(dir2)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a reattach with no new turn wrote room.json (stat: %v)", err)
	}
	h.Shutdown()
	f, _ := awaitRoomFile(t, dir2)
	if f.Turn != 5 || f.Sessions[model.VendorClaude] != "sess-5" {
		t.Fatalf("the way out did not preserve the saved room: %+v", f)
	}
	if time.Since(f.SavedAt) > time.Minute {
		t.Fatalf("saved_at was not refreshed on the way out: %v", f.SavedAt)
	}
}

// TestSavedKeysAreTakenOnceAndNeverOverALiveRoom: a rejoin sends no keys, and
// a hello that carried some over a room that has moved on is ignored — a
// saved id over a live thread would be the host forgetting a conversation it
// is holding.
func TestSavedKeysAreTakenOnceAndNeverOverALiveRoom(t *testing.T) {
	h, _, _ := hostOverSavedRoom(t, &SavedKeys{
		Turn: 2, SavedAt: time.Now(),
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-first"},
	})
	h.reattach(&SavedKeys{Turn: 9, Sessions: map[model.VendorID]string{model.VendorClaude: "sess-second"}})
	r := h.Snapshot()
	if r.Turn != 2 || r.Seats[0].SessionID != "sess-first" {
		t.Fatalf("a second hello re-seeded the room: %+v", r)
	}

	live, _, _ := hostOverSavedRoom(t, nil)
	live.dispatch("go", nil)
	claudeTurn(t, live, "sess-live")
	awaitSeat(t, live, func(s Seat) bool { return s.Phase == PhaseDone })
	live.reattach(&SavedKeys{Turn: 9, Sessions: map[model.VendorID]string{model.VendorClaude: "sess-stale"}})
	s := live.Snapshot().Seats[0]
	if s.SessionID != "sess-live" || s.Restored {
		t.Fatalf("saved keys landed over a live thread: %+v", s)
	}
}

// TestTheRoomFileRoundTripsAndTheWireCarriesTheMarker: the file reads back
// through this package's own decoder, and Restored and Reattached survive a
// frame, so a rejoining client builds the same card from its first frame.
func TestTheRoomFileRoundTripsAndTheWireCarriesTheMarker(t *testing.T) {
	r := Room{Version: RoomVersion, Turn: 3,
		Reattached: &Reattached{Turn: 3, SavedAt: time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)},
		Seats:      []Seat{{Vendor: model.VendorClaude, Drivable: true, SessionID: "sess-1", Restored: true}}}
	c := r.clone()
	r.Reattached.Turn = 99
	if c.Reattached.Turn != 3 {
		t.Fatal("the clone shares its Reattached pointer with the room")
	}
	b, err := json.Marshal(Frame{Kind: KindRoom, Room: c})
	if err != nil {
		t.Fatal(err)
	}
	var f Frame
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if f.Room.Reattached == nil || f.Room.Reattached.Turn != 3 || !f.Room.Seats[0].Restored {
		t.Fatalf("the marker did not survive the wire: %+v", f.Room)
	}
	if strings.Contains(string(b), "unproven") {
		t.Fatal("the host's probation bookkeeping reached the wire")
	}
}

// TestTheFirstHelloCarriesTheSavedKeysToTheHost is the wire half: the keys
// ride the hello, not argv, and the client that started the host sees the
// Restored card and the saved turn on its FIRST room frame.
func TestTheFirstHelloCarriesTheSavedKeysToTheHost(t *testing.T) {
	countSpawns(t)
	stubRoomJob(t)
	name := testPipeName(t)
	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  name,
		Posture:   vendors.PostureRead,
		Roster:    []RosterEntry{{Vendor: model.VendorClaude, Binary: "telltale-no-such-vendor-binary"}},
		Tick:      5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() { served <- h.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-served:
		case <-time.After(20 * time.Second):
			t.Error("the host did not return")
		}
	})

	conn, err := Dial(name, 20*time.Second)
	if err != nil {
		t.Fatalf("could not reach the host: %v", err)
	}
	defer conn.Close()
	fr, fw := NewFrameReader(conn), NewFrameWriter(conn)
	savedAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	if err := fw.Write(Frame{Kind: KindHello, Protocol: ProtocolVersion, Saved: &SavedKeys{
		Turn: 4, SavedAt: savedAt,
		Sessions: map[model.VendorID]string{model.VendorClaude: "sess-over-the-wire"},
	}}); err != nil {
		t.Fatal(err)
	}
	if f, err := fr.Read(); err != nil || f.Kind != KindWelcome {
		t.Fatalf("no welcome: %v %+v", err, f)
	}
	first, err := readRoom(t, fr)
	if err != nil {
		t.Fatal(err)
	}
	if first.Turn != 4 || first.Reattached == nil || !first.Reattached.SavedAt.Equal(savedAt) {
		t.Fatalf("the first frame does not carry the saved turn: %+v", first)
	}
	if s := first.Seats[0]; s.SessionID != "sess-over-the-wire" || !s.Restored {
		t.Fatalf("the first frame does not draw the restored seat: %+v", s)
	}
}
