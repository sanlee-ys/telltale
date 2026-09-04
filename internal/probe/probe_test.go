package probe

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The drive is pinned over STUBBED PROCESSES, one test per outcome, and no test
// here starts anything.
//
// That is the repository's rule ("a council test never spawns a vendor") and in
// this package it is also the only way the outcomes are reachable at all. A
// handshake that never lands, a turn that never ends and a process that
// outlives its grace are PROCESS behaviours: reproducing them against a real
// vendor would mean shipping three fake vendor binaries and hoping each one
// misbehaves on cue, and reproducing them against a real ONE would mean paying
// for a turn per assertion. Scripting the events the runner would have
// delivered puts every branch under test with nothing on the machine.
//
// What is NOT stubbed is the seat's own wire. The adapter below builds a real
// runner.Spec and encodes a real turn line, so the call shapes this package
// makes of vendors.Persistent and vendors.GracefulStop are the production ones.

// fakeSeat is a Persistent adapter whose invocation resolves to nothing.
//
// The binary it is handed is a path exec.LookPath cannot find, which is what
// makes the suite's spawn guard let it through: nothing launches, so nothing
// costs anything. Everything the drive asks of a seat, the spec, the turn
// line and the parser, is answered here rather than mocked at the boundary,
// because the property under test is what this package DOES with a seat's
// answers.
type fakeSeat struct{ id model.VendorID }

func (f fakeSeat) ID() model.VendorID { return f.id }

func (f fakeSeat) Session(workspace, binary, hooksFile string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{Vendor: f.id, Binary: binary, Args: []string{"--session"}, Dir: workspace}, nil
}

func (f fakeSeat) SessionResume(workspace, binary, hooksFile, id string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, vendors.ErrNoResume
}

func (f fakeSeat) Turn(prompt string) ([]byte, error) {
	return []byte(`{"turn":"` + prompt + `"}`), nil
}
func (f fakeSeat) Interrupt(id string) ([]byte, error) { return []byte(`{"interrupt":true}`), nil }

func (f fakeSeat) Decide(requestID string, allow bool, reason string, input map[string]any) ([]byte, error) {
	return []byte(`{"decide":true}`), nil
}

func (f fakeSeat) FirstTurn(prompt, workspace, binary string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, errors.New("this seat has no batch invocation")
}

func (f fakeSeat) NextTurn(prompt, workspace, binary, id string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, errors.New("this seat has no batch invocation")
}

func (f fakeSeat) ParseEvent(line []byte) (runner.Event, bool) { return runner.Event{}, false }

// gracefulSeat is a fakeSeat that states a closing word and a grace, which is
// the shape three of the four registered seats have. The grace is a field so a
// test can time a stop in milliseconds instead of the seconds a real adapter
// states.
type gracefulSeat struct {
	fakeSeat
	grace time.Duration
}

func (g gracefulSeat) Closing() [][]byte    { return [][]byte{[]byte(`{"closing":true}`)} }
func (g gracefulSeat) Grace() time.Duration { return g.grace }

// stubSession is the process that never was.
type stubSession struct {
	mu     sync.Mutex
	turns  [][]byte
	asides [][]byte
	closed bool

	done      chan struct{}
	closeOnce sync.Once

	// exitAfterClose is how long after CloseInput this process "exits". A
	// negative value never exits, which is the case the stop check exists for:
	// §9.50 measured a real seat doing exactly that.
	exitAfterClose time.Duration
}

func newStub(exitAfterClose time.Duration) *stubSession {
	return &stubSession{done: make(chan struct{}), exitAfterClose: exitAfterClose}
}

func (s *stubSession) SendTurn(lines [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turns = append(s.turns, lines...)
	return nil
}

func (s *stubSession) SendAside(lines [][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asides = append(s.asides, lines...)
	return nil
}

func (s *stubSession) CloseInput() {
	s.mu.Lock()
	s.closed = true
	after := s.exitAfterClose
	s.mu.Unlock()
	if after < 0 {
		return
	}
	time.AfterFunc(after, s.exit)
}

func (s *stubSession) Kill()                 { s.exit() }
func (s *stubSession) Done() <-chan struct{} { return s.done }
func (s *stubSession) exit()                 { s.closeOnce.Do(func() { close(s.done) }) }

func (s *stubSession) sentTurns() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.turns)
}

func (s *stubSession) stdinClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *stubSession) sentAsides() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.asides)
}

// stubSpawn replaces the package's two spawn vars for one test and returns the
// session every drive in it will get. The scripted events are delivered on the
// runner's own channel, in order, exactly as pumpStdout would deliver them.
func stubSpawn(t *testing.T, sess *stubSession, script []runner.Event) {
	t.Helper()
	realSession, realRPC := startSession, startRPCSession
	t.Cleanup(func() { startSession, startRPCSession = realSession, realRPC })

	deliver := func(out chan<- runner.Event) {
		go func() {
			for _, ev := range script {
				select {
				case out <- ev:
				case <-sess.done:
					return
				}
			}
		}()
	}
	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
		parse runner.ParseFunc) (session, error) {
		deliver(out)
		return sess, nil
	}
	startRPCSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
		proto runner.Protocol) (session, error) {
		deliver(out)
		return sess, nil
	}
}

// testOptions keep every deadline in milliseconds and inject the version probe,
// so no test here waits on a clock or spawns `<binary> --version`.
func testOptions(version string) Options {
	return Options{
		Timeout:         200 * time.Millisecond,
		TelltaleVersion: "test",
		Now:             func() time.Time { return time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC) },
		Version: func(binary string, args []string) doctor.ProbeResult {
			return doctor.ProbeResult{Out: version}
		},
	}
}

func seatFor(a vendors.Vendor) Seat {
	return Seat{
		Vendor:      model.VendorClaude,
		Label:       "a seat that resolves to nothing",
		Binary:      "telltale-no-such-binary",
		Adapter:     a,
		VersionArgs: []string{"--version"},
	}
}

func checkNamed(t *testing.T, r Result, name string) Check {
	t.Helper()
	for _, c := range r.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no %s check in %+v", name, r.Checks)
	return Check{}
}

// Every check passing is the ordinary run, and the assertion that matters most
// in it is the LAST one: the brief actually went down the pipe. A drive that
// scored three passes without handing the seat anything would be reporting a
// turn nobody took.
func TestEveryCheckPasses(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{
		{Kind: runner.KindSession, SessionID: "session-1"},
		{Kind: runner.KindText, Text: "pong"},
		{Kind: runner.KindText, Text: "", EndsTurn: true},
	})

	res := RunSeat(context.Background(), seatFor(gracefulSeat{grace: time.Second}), testOptions("1.2.3"))

	if res.Skipped != "" {
		t.Fatalf("seat was skipped: %s", res.Skipped)
	}
	if res.Version != "1.2.3" {
		t.Errorf("version = %q, want the string the binary printed", res.Version)
	}
	for _, name := range []string{CheckHandshake, CheckTurn, CheckStop} {
		if c := checkNamed(t, res, name); c.Status != doctor.Passed {
			t.Errorf("%s = %v (%s), want Passed", name, c.Status, c.Detail)
		}
	}
	if sess.sentTurns() != 1 {
		t.Errorf("the seat was handed %d turn lines, want exactly the one brief", sess.sentTurns())
	}
	if !sess.stdinClosed() {
		t.Error("the stop check passed without closing the seat's stdin")
	}
}

// A seat that comes up and never names a session is the fault this check
// exists for: the room's next turn is a resume keyed on that id, so the column
// works exactly once. The two checks under it must read `not checked` and never
// be guessed at.
func TestAHandshakeThatNamesNoSessionFailsAndStopsTheSeat(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{{Kind: runner.KindText, Text: "hello"}})

	res := RunSeat(context.Background(), seatFor(fakeSeat{}), testOptions("1.2.3"))

	hs := checkNamed(t, res, CheckHandshake)
	if hs.Status != doctor.Failed {
		t.Fatalf("handshake = %v, want Failed", hs.Status)
	}
	if !strings.Contains(hs.Detail, "no session was named") {
		t.Errorf("handshake detail = %q, want it to name what did not happen", hs.Detail)
	}
	for _, name := range []string{CheckTurn, CheckStop} {
		if c := checkNamed(t, res, name); c.Status != doctor.NotChecked {
			t.Errorf("%s = %v, want NotChecked after a failed handshake", name, c.Status)
		}
		if c := checkNamed(t, res, name); c.Took != 0 {
			t.Errorf("%s carried a duration of %s, and a check that did not run has none", name, c.Took)
		}
	}
}

// A process that dies before it names a session fails the handshake with the
// vendor's OWN words, not this package's. The runner has already assembled the
// child's stderr tail into Note, and that is the sentence a reader can act on.
func TestAProcessThatDiesBeforeItNamesASessionCarriesTheVendorsWords(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{{
		Kind: runner.KindError, Err: errors.New("exit status 1"),
		Note: "not logged in: run `claude login`",
	}})

	res := RunSeat(context.Background(), seatFor(fakeSeat{}), testOptions("1.2.3"))

	hs := checkNamed(t, res, CheckHandshake)
	if hs.Status != doctor.Failed {
		t.Fatalf("handshake = %v, want Failed", hs.Status)
	}
	if hs.Detail != "not logged in: run `claude login`" {
		t.Errorf("handshake detail = %q, want the vendor's own line", hs.Detail)
	}
}

// The turn check fails when the seat names a session and then never ends the
// turn. The handshake keeps the pass it earned, and only the stop goes
// unchecked.
func TestATurnThatNeverEndsFailsWithTheHandshakeStillPassed(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{
		{Kind: runner.KindSession, SessionID: "session-1"},
		{Kind: runner.KindText, Text: "thinking"},
	})

	res := RunSeat(context.Background(), seatFor(fakeSeat{}), testOptions("1.2.3"))

	if c := checkNamed(t, res, CheckHandshake); c.Status != doctor.Passed {
		t.Errorf("handshake = %v, want the pass it earned", c.Status)
	}
	turn := checkNamed(t, res, CheckTurn)
	if turn.Status != doctor.Failed {
		t.Fatalf("turn = %v, want Failed", turn.Status)
	}
	if !strings.Contains(turn.Detail, "did not end") {
		t.Errorf("turn detail = %q, want it to name what did not happen", turn.Detail)
	}
	if c := checkNamed(t, res, CheckStop); c.Status != doctor.NotChecked {
		t.Errorf("stop = %v, want NotChecked after a failed turn", c.Status)
	}
}

// The stop check is the one §9.50 forced. A closed stdin was measured NOT
// ending `codex app-server`. Four runs exited in 1.5 to 3.3 s, and one was
// alive at 15 s. So a check that accepted the room's own kill as an exit would
// report every seat passing. Here the process never exits on its own, and the
// check has to say so.
func TestAProcessThatOutlivesItsGraceFailsTheStop(t *testing.T) {
	sess := newStub(-1)
	stubSpawn(t, sess, []runner.Event{
		{Kind: runner.KindSession, SessionID: "session-1"},
		{Kind: runner.KindText, EndsTurn: true},
	})

	res := RunSeat(context.Background(),
		seatFor(gracefulSeat{grace: 20 * time.Millisecond}), testOptions("1.2.3"))

	stop := checkNamed(t, res, CheckStop)
	if stop.Status != doctor.Failed {
		t.Fatalf("stop = %v, want Failed", stop.Status)
	}
	if !strings.Contains(stop.Detail, "still running") {
		t.Errorf("stop detail = %q, want it to say the process was still there", stop.Detail)
	}
	if sess.sentAsides() != 1 {
		t.Errorf("the seat was sent %d closing lines, want the one it states", sess.sentAsides())
	}
}

// A spawn that fails is a failed handshake, not a skipped seat. The distinction
// is the same one detect.go refuses to collapse one mode out: "there is nothing
// here to drive" and "what is here would not come up" have different fixes.
func TestASpawnThatFailsIsAFailedHandshake(t *testing.T) {
	realSession := startSession
	t.Cleanup(func() { startSession = realSession })
	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event,
		parse runner.ParseFunc) (session, error) {
		return nil, errors.New("exec: file does not exist")
	}

	res := RunSeat(context.Background(), seatFor(fakeSeat{}), testOptions("1.2.3"))

	hs := checkNamed(t, res, CheckHandshake)
	if hs.Status != doctor.Failed {
		t.Fatalf("handshake = %v, want Failed", hs.Status)
	}
	if hs.Detail != "exec: file does not exist" {
		t.Errorf("handshake detail = %q, want the spawn's own error", hs.Detail)
	}
	if res.Skipped != "" {
		t.Errorf("the seat was reported skipped (%q); a spawn that failed is a measurement", res.Skipped)
	}
}

// A seat with nothing to drive is SKIPPED, carries no check at all, and writes
// no file. A row of three `not_run` results would be the probe claiming a visit
// it never made, and an absent file already says nobody drove this seat here.
func TestASeatWithNothingToDriveIsSkippedAndCarriesNoChecks(t *testing.T) {
	for _, tc := range []struct {
		name string
		seat Seat
		want string
	}{
		{"no binary", Seat{Vendor: model.VendorCodex, Adapter: fakeSeat{}}, "no binary"},
		{"no adapter", Seat{Vendor: model.VendorCodex, Binary: "telltale-no-such-binary"}, "no adapter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := RunSeat(context.Background(), tc.seat, testOptions("1.2.3"))
			if res.Skipped == "" {
				t.Fatal("the seat was driven, and there was nothing to drive")
			}
			if !strings.Contains(res.Skipped, tc.want) {
				t.Errorf("skipped = %q, want it to name %q", res.Skipped, tc.want)
			}
			if len(res.Checks) != 0 {
				t.Errorf("a skipped seat carried %d checks, want none", len(res.Checks))
			}
			if res.Drove() {
				t.Error("a skipped seat reported that it was driven")
			}
		})
	}
}

// A batch-only seat has no live shape, and saying so beats reporting a
// handshake nobody could have made.
func TestABatchOnlySeatFailsTheHandshakeWithTheReason(t *testing.T) {
	res := RunSeat(context.Background(), seatFor(batchOnlySeat{}), testOptions("1.2.3"))
	hs := checkNamed(t, res, CheckHandshake)
	if hs.Status != doctor.Failed {
		t.Fatalf("handshake = %v, want Failed", hs.Status)
	}
	if !strings.Contains(hs.Detail, "batch program") {
		t.Errorf("handshake detail = %q, want it to name the shape", hs.Detail)
	}
}

// batchOnlySeat satisfies Vendor and neither of the two live interfaces, which
// is what every registered seat looked like before design.md §9.57.
type batchOnlySeat struct{}

func (batchOnlySeat) ID() model.VendorID { return model.VendorCodex }

func (batchOnlySeat) FirstTurn(prompt, workspace, binary string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, nil
}

func (batchOnlySeat) NextTurn(prompt, workspace, binary, id string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, nil
}

func (batchOnlySeat) ParseEvent(line []byte) (runner.Event, bool) { return runner.Event{}, false }

// A version this machine did not print stays EMPTY, and the drive runs anyway.
// An invented version on a real measurement would be worse than no version at
// all: doctor compares that string against the installed build, and a guess
// there produces a drift verdict nobody measured.
func TestAnUnreadVersionStaysEmptyAndTheDriveStillRuns(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{
		{Kind: runner.KindSession, SessionID: "session-1"},
		{Kind: runner.KindText, EndsTurn: true},
	})
	o := testOptions("")
	o.Version = func(binary string, args []string) doctor.ProbeResult {
		return doctor.ProbeResult{Err: errors.New("did not answer within 15s")}
	}

	res := RunSeat(context.Background(), seatFor(gracefulSeat{grace: time.Second}), o)

	if res.Version != "" {
		t.Errorf("version = %q, want the empty string when nothing was read", res.Version)
	}
	if c := checkNamed(t, res, CheckTurn); c.Status != doctor.Passed {
		t.Errorf("turn = %v, want the drive to have run anyway", c.Status)
	}
}

// A seat whose adapter states no grace still gets a bound. The stream-json seat
// is in that position because a closed stdin was MEASURED sufficient for it, so
// the adapter has nothing to say. But a check that could wait forever is not a
// check.
func TestASeatWithNoStatedGraceStillStopsWithinABound(t *testing.T) {
	sess := newStub(0)
	stubSpawn(t, sess, []runner.Event{
		{Kind: runner.KindSession, SessionID: "session-1"},
		{Kind: runner.KindText, EndsTurn: true},
	})

	res := RunSeat(context.Background(), seatFor(fakeSeat{}), testOptions("1.2.3"))

	if c := checkNamed(t, res, CheckStop); c.Status != doctor.Passed {
		t.Fatalf("stop = %v (%s), want Passed", c.Status, c.Detail)
	}
	if sess.sentAsides() != 0 {
		t.Errorf("a seat that states no closing word was sent %d lines", sess.sentAsides())
	}
}
