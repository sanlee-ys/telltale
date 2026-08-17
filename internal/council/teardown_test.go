package council

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// countedKill records HOW MANY times a seat's process was killed, not merely
// that it was.
//
// clear_test.go's killSession answers "was it killed", which is the right
// question there. It is the wrong one here: the property is that a teardown
// arriving twice does not act twice, and a boolean cannot tell one kill from
// three. Kill itself is idempotent on a real runner.Session, so a stub that
// hid the count would let the concurrent map write this file exists to catch
// pass silently.
type countedKill struct {
	mu    sync.Mutex
	kills int
	// fired closes on the FIRST kill, so a test waiting on a signal has
	// something to block on instead of a sleep.
	fired chan struct{}
	once  sync.Once
	// hold is how long Kill dawdles before returning, and it is what makes the
	// racing test witness the defect instead of hoping for it.
	//
	// A real Session.Kill signals a process group and returns in microseconds,
	// so two unguarded teardowns overlap only by luck — the first run of this
	// file passed with the guard deliberately removed, which is the recorded
	// failure mode of a test that checks the flag instead of the effect. A hold
	// widens the window to something a scheduler cannot close: every racing
	// caller is still inside the sweep when the next one starts it.
	hold time.Duration
}

func newCountedKill() *countedKill { return &countedKill{fired: make(chan struct{})} }

func (s *countedKill) SendTurn([][]byte) error  { return nil }
func (s *countedKill) SendAside([][]byte) error { return nil }
func (s *countedKill) Alive() bool              { return true }

func (s *countedKill) Kill() {
	s.mu.Lock()
	s.kills++
	hold := s.hold
	s.mu.Unlock()
	s.once.Do(func() { close(s.fired) })
	if hold > 0 {
		time.Sleep(hold)
	}
}

func (s *countedKill) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.kills
}

// teardownRoom is a room with three persistent seats and a racer on the turn —
// the two collections teardown walks, and the two a second caller would walk
// again.
//
// Turn stays 0, so saveRoom returns before it touches disk (clear_test.go's
// rule). No vendor is spawned anywhere in this file: every process here is a
// countedKill, so countSpawns has nothing to guard.
func teardownRoom(t *testing.T) (*Model, map[model.VendorID]*countedKill, *countedKill, *int) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	seats := map[model.VendorID]*countedKill{}
	procs := map[model.VendorID]*seatProc{}
	var cols []Column
	for _, v := range []model.VendorID{
		model.VendorClaude, model.VendorCodex, model.VendorAntigravity,
	} {
		cols = append(cols, Column{
			Vendor: v, Label: string(v), Binary: string(v), Avail: AvailInstalled,
		})
		sess := newCountedKill()
		seats[v] = sess
		procs[v] = &seatProc{sess: sess}
	}

	racer := newCountedKill()
	cancels := 0
	m := &Model{
		st:         State{Columns: cols, Workspace: t.TempDir()},
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		failure:    map[model.VendorID]runner.FailureClass{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      procs,
		gateInputs: map[string]map[string]any{},
		events:     make(chan runner.Event, 8),
		roomCtx:    ctx,
		roomCancel: cancel,
		turn: &turnState{
			// The flat handles list is left empty on purpose: runner.Handle is a
			// concrete type whose Kill needs a process nothing here spawned, and
			// the runner's own tests already pin it. The racer below is the
			// fakeable member of the same sweep.
			cancel:         func() { cancels++ },
			live:           map[model.VendorID]bool{},
			persistent:     map[model.VendorID]bool{},
			arenaEphemeral: map[model.VendorID]seatSession{model.VendorCursor: racer},
		},
	}
	return m, seats, racer, &cancels
}

// TestTeardownActsOnceHoweverOftenItIsCalled is the sequential half, and it is
// a REGRESSION PIN rather than a witness — the distinction is worth stating,
// because it is the one this file got wrong first.
//
// Sequentially, teardown was already once-only before the guard landed, by
// accident of how it is written: it drains m.procs as it walks it and sets
// m.turn to nil at the end, so a second call finds nothing to do. The guard did
// not fix that and must not break it. What the guard fixes is the concurrent
// case below.
func TestTeardownActsOnceHoweverOftenItIsCalled(t *testing.T) {
	m, seats, racer, cancels := teardownRoom(t)

	m.teardown()
	m.teardown()
	m.teardown()

	for v, s := range seats {
		if got := s.count(); got != 1 {
			t.Errorf("%s was killed %d times, want 1", v, got)
		}
	}
	if got := racer.count(); got != 1 {
		t.Errorf("the racer was killed %d times, want 1", got)
	}
	if got := *cancels; got != 1 {
		t.Errorf("the turn was cancelled %d times, want 1", got)
	}
	if len(m.procs) != 0 {
		t.Errorf("%d seat processes are still registered after teardown", len(m.procs))
	}
	if m.turn != nil {
		t.Error("the turn survived teardown")
	}
	if m.roomCtx.Err() == nil {
		t.Error("the room context is still live, so a seat spawned after teardown would not be reaped")
	}
}

// TestATeardownRacingAnotherNeitherPanicsNorDoubleKills is the collision the
// guard exists for: a signal landing on a room the user is already quitting.
//
// This one IS a witness. Removing the guard from teardown makes it fail on the
// counts — sixteen callers each walk a map none of them has drained yet, so
// every seat is killed sixteen times — and the same overlap is a concurrent map
// write inside teardown's own delete-while-ranging loop, which the Go runtime
// answers with a fatal error no test binary can recover from. Either way the
// run goes red; the counts are what make it legible when it does.
//
// The hold on each seat's Kill is what makes that reliable rather than lucky:
// see countedKill.hold.
func TestATeardownRacingAnotherNeitherPanicsNorDoubleKills(t *testing.T) {
	m, seats, racer, cancels := teardownRoom(t)
	for _, s := range seats {
		s.hold = 20 * time.Millisecond
	}
	racer.hold = 20 * time.Millisecond

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.teardown()
		}()
	}
	wg.Wait()

	for v, s := range seats {
		if got := s.count(); got != 1 {
			t.Errorf("%s was killed %d times, want 1 — teardowns overlapped", v, got)
		}
	}
	if got := racer.count(); got != 1 {
		t.Errorf("the racer was killed %d times, want 1 — teardowns overlapped", got)
	}
	if got := *cancels; got != 1 {
		t.Errorf("the turn was cancelled %d times, want 1", got)
	}
}
