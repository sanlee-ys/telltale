package councilhost

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The tests in this file are design.md §7.31's crew half: the host takes a
// brief per seat, refuses per seat, stops per seat, and carries on the wire
// what council's own Render reads.

// twoSeatHost is an in-process host over two batch seats whose binaries
// cannot resolve, with the spawns stubbed and the room job stubbed. The fold
// runs; nothing else does.
func twoSeatHost(t *testing.T) (*Host, *spawnLog) {
	t.Helper()
	log := countSpawns(t)
	stubRoomJob(t)
	h, err := New(Config{
		Workspace: t.TempDir(),
		PipeName:  "telltale-test-crew-" + t.Name(),
		Posture:   vendors.PostureRead,
		Roster: []RosterEntry{
			{Vendor: model.VendorCodex, Binary: "telltale-no-such-vendor-binary"},
			{Vendor: model.VendorGrok, Binary: "telltale-no-such-vendor-binary"},
		},
		Tick: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	go h.fold()
	return h, log
}

func seatOf(r Room, v model.VendorID) Seat {
	for _, s := range r.Seats {
		if s.Vendor == v {
			return s
		}
	}
	return Seat{}
}

// TestADispatchGoesToTheSeatsItNames is the crew's first rule: a brief names
// its seats, and the host spawns those and no other.
//
// Before §7.31 every dispatch was a broadcast, so `@codex` in a hosted room
// would have billed grok too. The unaddressed seat is told so in council's own
// words, with Skipped set, so the client draws a skip and not a warning.
func TestADispatchGoesToTheSeatsItNames(t *testing.T) {
	h, log := twoSeatHost(t)
	h.dispatch("refactor the parser", []model.VendorID{model.VendorCodex})
	log.awaitN(t, 1)

	r := awaitRoom2(t, h, func(r Room) bool { return r.Turn == 1 })
	codex, grok := seatOf(r, model.VendorCodex), seatOf(r, model.VendorGrok)
	// Both seats wear a request/response live shape since §9.57, and the host
	// drives each through its measured batch adapter and says so on the wire —
	// rather than refusing two of five columns as undrivable.
	if !codex.Drivable || !codex.FellBack || !grok.Drivable || !grok.FellBack {
		t.Fatalf("the host did not fall back to the measured adapters: codex=%+v grok=%+v", codex, grok)
	}
	if codex.Phase != PhaseWaiting || codex.Turn != 1 || codex.Prompt != "refactor the parser" {
		t.Fatalf("the named seat did not take the brief: %+v", codex)
	}
	if codex.Started.IsZero() {
		t.Fatal("the named seat's turn has no start time, so the client can draw no clock")
	}
	if grok.Phase != PhaseIdle || grok.Turn != 0 {
		t.Fatalf("an unnamed seat took the brief: %+v", grok)
	}
	if grok.Note != "not addressed in turn 1" || !grok.Skipped {
		t.Fatalf("the unnamed seat was not told it sat the turn out: note=%q skipped=%v", grok.Note, grok.Skipped)
	}
	if log.specs[0].Vendor != model.VendorCodex {
		t.Fatalf("the spawn went to %s", log.specs[0].Vendor)
	}
}

// TestABusySeatIsRefusedAndAnIdleSeatStillGoes is §9.54's per-seat guard in
// the host.
//
// The refusal names the busy seat and the turn it is on, in the sentence the
// room uses, and it must not stop the idle seat the same brief named: a crew
// whose one busy member blocked everyone would be the committee again.
func TestABusySeatIsRefusedAndAnIdleSeatStillGoes(t *testing.T) {
	h, log := twoSeatHost(t)
	h.dispatch("first", []model.VendorID{model.VendorCodex})
	log.awaitN(t, 1)
	awaitRoom2(t, h, func(r Room) bool { return seatOf(r, model.VendorCodex).Phase == PhaseWaiting })

	h.dispatch("second", nil)
	log.awaitN(t, 2)
	r := awaitRoom2(t, h, func(r Room) bool { return r.Turn == 2 })
	if got := seatOf(r, model.VendorCodex); got.Turn != 1 || got.Prompt != "first" {
		t.Fatalf("the busy seat was re-dispatched: %+v", got)
	}
	if got := seatOf(r, model.VendorGrok); got.Turn != 2 || got.Prompt != "second" {
		t.Fatalf("the idle seat did not take the second brief: %+v", got)
	}
	if !strings.Contains(r.Notice, "skipped: codex (turn 1)") {
		t.Fatalf("the partial send did not name the seat it skipped: %q", r.Notice)
	}

	// A brief that reaches ONLY busy seats sends nothing, counts no turn, and
	// says so with the per-seat remedy.
	h.dispatch("third", []model.VendorID{model.VendorCodex})
	r = awaitRoom2(t, h, func(r Room) bool { return strings.HasPrefix(r.Notice, "a turn is in flight on codex (turn 1)") })
	if r.Turn != 2 {
		t.Fatalf("a refused brief counted a turn: %d", r.Turn)
	}
	if !strings.Contains(r.Notice, "ctrl+c on its column") {
		t.Fatalf("the refusal has no per-seat remedy: %q", r.Notice)
	}
	if log.n() != 2 {
		t.Fatalf("a refused brief spawned something: %d spawns", log.n())
	}
}

// TestAnInterruptStopsOnlyTheSeatItNames is the crew's ctrl+c: the focused
// seat stops, its neighbour works on, and an interrupt naming nobody stops
// everyone — which is what the plain client sends.
func TestAnInterruptStopsOnlyTheSeatItNames(t *testing.T) {
	h, log := twoSeatHost(t)
	h.dispatch("both", nil)
	log.awaitN(t, 2)
	awaitRoom2(t, h, func(r Room) bool {
		return seatOf(r, model.VendorCodex).Phase == PhaseWaiting && seatOf(r, model.VendorGrok).Phase == PhaseWaiting
	})

	h.interrupt([]model.VendorID{model.VendorCodex})
	r := awaitRoom2(t, h, func(r Room) bool { return seatOf(r, model.VendorCodex).Phase == PhaseCancelled })
	if got := seatOf(r, model.VendorGrok); got.Phase != PhaseWaiting {
		t.Fatalf("an interrupt naming codex stopped grok too: %+v", got)
	}
	codex := seatOf(r, model.VendorCodex)
	if codex.Note != cancelledNote {
		t.Fatalf("a cancelled seat did not say its output is partial: %q", codex.Note)
	}
	if codex.Ended.IsZero() {
		t.Fatal("a cancelled seat was not retired, so the client's inbox cannot list it")
	}

	h.interrupt(nil)
	r = awaitRoom2(t, h, func(r Room) bool { return seatOf(r, model.VendorGrok).Phase == PhaseCancelled })
	if got := seatOf(r, model.VendorCodex); got.Phase != PhaseCancelled {
		t.Fatalf("a second interrupt re-labelled a cancelled seat: %+v", got)
	}
}

// TestTheWireCarriesWhatTheRoomDraws is the projection half of §7.31: a
// second turn files the first into History with its acts, its clock and its
// outcome, and every one of those survives a marshal.
//
// The stream is synthesized and folded through the real event shapes, so the
// history record carries what a live turn would.
func TestTheWireCarriesWhatTheRoomDraws(t *testing.T) {
	r := Room{Version: RoomVersion, Seats: []Seat{{Vendor: model.VendorCodex, Phase: PhaseIdle, Drivable: true}}}
	t0 := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	r.beginTurn(1, "first brief", map[model.VendorID]bool{model.VendorCodex: true}, t0)
	r.applyAt(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindText, Text: "answer one"}, t0.Add(time.Second))
	r.applyAt(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: "c1", Text: "Bash: go test"}}}, t0.Add(2*time.Second))
	r.applyAt(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindActivity,
		Acts: []runner.ActCall{{ID: "c1", Outcome: runner.ActFailed, Detail: "exit 1"}}}, t0.Add(3*time.Second))
	cost := 0.0123
	r.applyAt(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindMeta, CostUSD: &cost}, t0.Add(4*time.Second))
	r.applyAt(runner.Event{Vendor: model.VendorCodex, Kind: runner.KindDone}, t0.Add(5*time.Second))

	s := r.Seats[0]
	if s.Elapsed != 5*time.Second || s.Ended != t0.Add(5*time.Second) {
		t.Fatalf("the turn's clock was not stamped: elapsed=%v ended=%v", s.Elapsed, s.Ended)
	}
	if len(s.Acts) != 1 || s.Acts[0].Status != ActFailed || s.Acts[0].Detail != "exit 1" {
		t.Fatalf("the act's result did not fold onto its announcement by id: %+v", s.Acts)
	}
	if s.CostUSD == nil || *s.CostUSD != cost || s.CostSession {
		t.Fatalf("a batch seat's reported cost did not land as this turn's spend: %+v", s)
	}

	r.beginTurn(2, "second brief", map[model.VendorID]bool{model.VendorCodex: true}, t0.Add(time.Minute))
	s = r.Seats[0]
	if len(s.History) != 1 {
		t.Fatalf("the finished turn was not filed: %+v", s.History)
	}
	h := s.History[0]
	if h.N != 1 || h.Prompt != "first brief" || h.Body != "answer one" || h.Phase != PhaseDone ||
		h.Elapsed != 5*time.Second || len(h.Acts) != 1 || h.CostUSD == nil {
		t.Fatalf("the record lost a field on the way in: %+v", h)
	}
	if s.Body != "" || s.Acts != nil || s.Prompt != "second brief" || s.Turn != 2 || s.Elapsed != 0 || !s.Ended.IsZero() {
		t.Fatalf("the new turn inherited the old one's fields: %+v", s)
	}

	// The whole thing survives the wire, pointer fields included, and a
	// clone shares no history with the room.
	var buf bytes.Buffer
	if err := NewFrameWriter(&buf).Write(Frame{Kind: KindRoom, Room: r.clone()}); err != nil {
		t.Fatal(err)
	}
	f, err := NewFrameReader(&buf).Read()
	if err != nil {
		t.Fatal(err)
	}
	got := f.Room.Seats[0]
	if len(got.History) != 1 || got.History[0].CostUSD == nil || *got.History[0].CostUSD != cost {
		t.Fatalf("history did not survive the wire: %+v", got.History)
	}
	if got.History[0].Acts[0].Status != ActFailed {
		t.Fatalf("an act's status did not survive the wire: %+v", got.History[0].Acts)
	}
	c := r.clone()
	r.Seats[0].History[0].Body = "MUTATED"
	if c.Seats[0].History[0].Body == "MUTATED" {
		t.Fatal("the clone shares its History backing array with the room")
	}
}

// TestADispatchFrameCarriesItsSeats pins the one field §7.31 added to the
// wire: a client that resolved `@codex` sends codex, and a client that typed
// nothing sends nothing, which the host reads as everyone.
func TestADispatchFrameCarriesItsSeats(t *testing.T) {
	var buf bytes.Buffer
	fw := NewFrameWriter(&buf)
	if err := fw.Write(Frame{Kind: KindDispatch, Prompt: "x", Seats: []model.VendorID{model.VendorCodex}}); err != nil {
		t.Fatal(err)
	}
	if err := fw.Write(Frame{Kind: KindInterrupt}); err != nil {
		t.Fatal(err)
	}
	raw := buf.String()
	if !strings.Contains(raw, `"seats":["codex"]`) {
		t.Fatalf("the seat list is not on the wire as vendor ids: %s", raw)
	}
	fr := NewFrameReader(strings.NewReader(raw))
	d, err := fr.Read()
	if err != nil || len(d.Seats) != 1 || d.Seats[0] != model.VendorCodex {
		t.Fatalf("the seat list did not survive the wire: %+v (%v)", d, err)
	}
	i, err := fr.Read()
	if err != nil || i.Seats != nil {
		t.Fatalf("an unaddressed interrupt grew a seat list: %+v (%v)", i, err)
	}
	// omitempty: a broadcast carries no seats key at all, so an older log of
	// the wire reads exactly as it did.
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(raw, "\n", 3)[1]), &m); err != nil {
		t.Fatal(err)
	}
	if _, has := m["seats"]; has {
		t.Fatalf("an interrupt naming nobody wrote a seats key: %v", m)
	}
}

// TestAPlainClientBriefStillReachesEverySeat is the compatibility half: the
// plain client names no seats, and that still means the broadcast §7.28
// shipped.
func TestAPlainClientBriefStillReachesEverySeat(t *testing.T) {
	h, log := twoSeatHost(t)
	h.dispatch("everyone", nil)
	log.awaitN(t, 2)
	r := awaitRoom2(t, h, func(r Room) bool { return r.Turn == 1 })
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorGrok} {
		if s := seatOf(r, v); s.Turn != 1 || s.Phase != PhaseWaiting {
			t.Fatalf("%s did not take the broadcast: %+v", v, s)
		}
	}
}
