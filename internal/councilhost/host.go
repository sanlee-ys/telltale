package councilhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// seatSession is the part of runner.Session the host uses.
//
// An interface rather than the concrete type for the same reason council's own
// seat path uses one: it is what lets the spawn vars below be replaced in a
// test without a vendor process existing. The four methods are exactly the four
// a live seat is asked for, so a fake cannot pass by implementing less.
type seatSession interface {
	SendTurn([][]byte) error
	SendAside([][]byte) error
	Kill()
	Alive() bool
}

// seatProcess is the part of runner.Handle the host uses: one method, because
// a spawn-per-turn child is only ever killed from here.
//
// An interface for a sharper reason than seatSession's. runner.Handle carries
// an unexported procGroup, so a zero value has a nil group and Handle.Kill
// calls a method on it unconditionally — a `&runner.Handle{}` returned from a
// test stub panics the whole test binary the first time Shutdown or interrupt
// reaches it. Nothing outside package runner can build a safe zero Handle, so
// the fix belongs on this side of the boundary: the var hands back an
// interface, and a stub implements it with a no-op.
type seatProcess interface {
	Kill()
}

// startSession, startProcess and startHost are this package's ONLY process
// spawns, and they are vars for one reason: the test guard.
//
// internal/council/main_test.go makes the council package's spawns fail closed,
// because the opposite default was MEASURED starting `codex exec --json -s
// danger-full-access` from a plain `go test` run — a live agent turn, with full
// write access, on the operator's own account. CI cannot catch that class: CI
// has no vendors installed, so every seat resolves as missing and nothing
// dispatches.
//
// A host spawns from a different process AND a different package, so it sits
// outside that wrap entirely. These vars exist so this package can carry the
// same guard, and internal/councilhost/main_test.go arms it on the same rule —
// a binary this machine can resolve panics, naming the call site and the argv.
//
// A CONVERSATIONAL seat (cursor-agent's ACP server) is not driven here yet, so
// there is deliberately no startRPCSession var. Adding that seat means adding
// the var AND adding it to this package's TestMain and countSpawns in the same
// change. Leaving an unused var here instead would be dead code that a reader
// could not tell from a guarded path.
var (
	startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (seatSession, error) {
		return runner.StartSession(ctx, spec, out, parse)
	}
	startProcess = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (seatProcess, error) {
		h, err := runner.Start(ctx, spec, out, parse)
		if err != nil {
			// Returned as a nil INTERFACE rather than a typed nil pointer. A
			// typed nil here would be non-nil as an interface, and every caller
			// checking `!= nil` before calling Kill would then call it on
			// nothing.
			return nil, err
		}
		return h, nil
	}
)

// newRoomJob is behind a var for a hazard rather than for tidiness.
//
// NewRoomJob assigns the CALLING process into a job carrying
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE. That is exactly right for a host and
// exactly wrong for a test binary: an in-process test that let Serve build a
// real one would put `go test` itself in that job, and the first Shutdown would
// close the last handle and terminate the suite mid-run.
//
// So a test that runs a host IN-PROCESS stubs this, and a test that wants the
// containment measured runs a real host in a real process instead
// (roomjob_windows_test.go). The two are not interchangeable and the split is
// deliberate: containment is a claim about a process dying, and a claim about a
// process dying cannot be asserted by the process making it.
var newRoomJob = NewRoomJob

// RosterEntry is one seat the host was told to hold: which vendor, and the
// binary detection resolved for it.
//
// The host is HANDED this rather than detecting for itself, and that is a
// dependency ruling. Detection lives in package council; importing it here to
// call one function would tie the host to the room's own package for no other
// reason, and the host's job is process ownership, parsing and state — not
// finding programs on PATH.
type RosterEntry struct {
	Vendor model.VendorID
	Binary string
}

// Config is everything a host needs to open a room.
type Config struct {
	// Workspace is the directory turns are dispatched against. Required: a
	// host with no workspace would dispatch against whatever directory it
	// happened to start in, which is a write posture pointed somewhere nobody
	// chose.
	Workspace string
	// PipeName is the full transport name, from PipeName.
	PipeName string
	// Roster is who is in the room, in the order it will be drawn.
	Roster []RosterEntry
	// Posture is what the seats may do.
	Posture vendors.Posture
	// CouncilDir is where host.json goes: the directory council already writes
	// room.json into. Passed rather than found, so that this package needs no
	// dependency on council's own RoomPath and the two cannot drift about where
	// the directory is. Empty writes no discovery file at all, which is what a
	// test wants and what a host with nowhere to write must do rather than
	// guess a path.
	CouncilDir string
	// Tick is how often a changed room is sent to the client. Zero uses
	// defaultTick.
	Tick time.Duration
}

// defaultTick coalesces room frames.
//
// A token delta is a channel send inside one process and a syscall plus a parse
// across two, and a five-seat streaming room produces a lot of them. Sending
// one frame per event would spend the pipe on frames a reader cannot perceive
// as separate. 50ms is under the threshold at which a person reads output as
// continuous and well above the rate at which a vendor emits deltas.
//
// The coalescing is why the whole room travels rather than a delta: a client
// that has just connected and a client that has been watching both want the
// same frame, and one frame shape cannot go out of sync with itself. A delta
// protocol would need a resync path, and a resync path is a second encoding of
// the same state that can disagree with the first.
const defaultTick = 50 * time.Millisecond

// firstConnectWindow bounds how long a host waits for the client that started
// it. See Serve for why it exists and why it applies to the first connect only.
const firstConnectWindow = 60 * time.Second

// ErrGatedPostureNotHosted is why a gated room is refused.
var ErrGatedPostureNotHosted = errors.New(
	"councilhost: a gated seat blocks on a question this host cannot carry yet — " +
		"run that room with `telltale council`")

// UnwatchedWriteRefusal is design.md §7.29's unwatched-write ruling, in the one
// sentence the operator reads.
//
// # The ruling, and why it is one condition rather than three
//
// §7.28 named the risk shape as detach plus a write posture plus `--auto`, and
// said the ruling was owed with detach and never before. On a hosted room those
// three collapse into one condition:
//
//   - §7.28 refuses PostureWriteGated outright, in New. A gated seat blocks on a
//     question this host cannot carry, so a hosted room is NEVER gated.
//   - A hosted room that is not read-only is therefore an UNGATED write room:
//     every tool call runs with nobody to ask. That is precisely what `--auto`
//     means on the room's own surface — dispatch.go's seatPosture returns
//     PostureWrite for write-and-not-asking.
//
// So the condition is the posture, and the refusal is keyed on it. Read
// detaches. Write does not.
//
// # What this is not
//
// It is not a claim that the write posture is unsafe: the room writes by default
// and that ruling stands. What it refuses is walking AWAY from one. It is not a
// supervisor either — nothing watches the room, nothing re-approves anything,
// and nothing self-terminates.
//
// It is also not the option the costing recommended. That recommended allowing
// the detach and reporting afterwards what happened while nobody watched. The
// owner overruled it on 2026-09-01: a report is a record of an act that already
// happened, and a receipt is not consent given in advance.
//
// # Enforced HERE, in the host, and never in the client
//
// The host is the process that would keep running, so it is the process that
// must refuse. A check in the client alone is a check a second client could
// simply not make. TestAWritingRoomRefusesToDetach pins it against the host and
// TestAReadRoomDetaches is its positive control — without the pair, a refusal
// that refused everything would pass.
const UnwatchedWriteRefusal = "this room writes to the workspace without asking, so it will not " +
	"detach: telltale never leaves an agent working while nobody is watching."

// UnwatchedWriteRemedy is the way out, and it is a SECOND LINE rather than a
// longer sentence.
//
// §9.17's tell is that a refusal with no remedy is this room's stated defect. A
// run-on sentence is not a remedy, so the sentence above stays one sentence and
// this stands beside it.
const UnwatchedWriteRemedy = "the room is still here and still yours. open it with " +
	"`telltale council --host --read` to get a room you can leave."

// errClientDetached is Serve's own signal that a client LEFT rather than ended
// the room.
//
// Unexported and never returned to a caller: it is the one thing that
// distinguishes "go back to Accept" from "tear the room down", and it travels
// exactly one function up. A caller that could see it would be able to treat a
// detach as an error, which is the opposite of what it means.
var errClientDetached = errors.New("councilhost: the client detached")

// Host owns the vendor processes, the pipes and the room state.
type Host struct {
	cfg  Config
	job  *RoomJob
	ln   *Listener
	tick time.Duration

	// events is shared by every seat and is BOUNDED, which is the property the
	// whole split rests on. A slow consumer stalls the vendors rather than
	// losing their output, and the consumer here is this process's own fold
	// goroutine — never the client. So a client that stops reading stalls the
	// WIRE and the vendors keep working, which is exactly the inversion a host
	// exists to buy.
	events chan runner.Event

	mu    sync.Mutex
	room  Room
	dirty bool

	// pmu guards the process maps. Separate from mu because a spawn can take
	// hundreds of milliseconds and the fold goroutine must not wait on one to
	// record a token that already arrived.
	pmu      sync.Mutex
	sessions map[model.VendorID]seatSession
	handles  map[model.VendorID]seatProcess

	// roomCtx bounds every seat and is cancelled only by teardown. It is
	// deliberately NOT a turn's context: the whole value of a persistent seat
	// is that cancelling one turn does not cost the next one a session init.
	roomCtx    context.Context
	roomCancel context.CancelFunc

	// active is the client being served right now, or nil. Shutdown closes it,
	// and that is what lets a caught signal end a room that has a client
	// attached: serveClient's read loop parks in the kernel and does not watch
	// a context, so closing the listener alone reaches it no sooner than the
	// client's next word. Guarded by pmu, like the process maps, because it is
	// the same class of thing — a handle Shutdown must reach.
	active *Conn
	// ended is set when the room was ended by a signal the room job caught
	// (roomjob_unix.go), so Serve can return nil for a deliberate end rather
	// than the closed-listener error the wake-up otherwise looks like.
	ended atomic.Bool

	interrupts int
	// startedAt is stamped once, when the room opens, so a rewritten discovery
	// file keeps saying when the HOST started rather than when it was last
	// rewritten.
	startedAt time.Time

	// inFlight is true from the moment a turn is broadcast until every drivable
	// seat has settled.
	//
	// It exists because a second dispatch used to start a SECOND child for a
	// batch seat while the first was still streaming, and the first was then
	// dropped from the handles map — unreachable by interrupt and by Shutdown,
	// still folding text into the new turn's body, and reaped only by the room
	// job at host death. That is the backstop being used as the ordinary route,
	// which roomjob_windows.go says it must not be.
	//
	// Guarded by mu, because the answer is read off the same seat phases the
	// room draws from: what "a turn is running" means must not be able to
	// disagree with what the operator can see.
	inFlight bool
}

// eventBuffer is how many events may be in flight from the seats.
//
// Matched to council's own bounded channel in spirit rather than in number: big
// enough that a redraw does not stall a vendor mid-sentence, small enough that
// backpressure still reaches the vendor as a slow column rather than as an
// unbounded queue nobody can see.
const eventBuffer = 256

// New builds a host. It does not create the job, the pipe or any process —
// Serve does, so that a caller can construct one in a test without owning
// anything.
func New(cfg Config) (*Host, error) {
	if cfg.Workspace == "" {
		return nil, errors.New("councilhost: a host needs a workspace")
	}
	if cfg.PipeName == "" {
		return nil, errors.New("councilhost: a host needs a transport name")
	}
	if cfg.Posture == vendors.PostureWriteGated {
		return nil, ErrGatedPostureNotHosted
	}
	tick := cfg.Tick
	if tick <= 0 {
		tick = defaultTick
	}
	h := &Host{
		cfg:      cfg,
		tick:     tick,
		events:   make(chan runner.Event, eventBuffer),
		sessions: map[model.VendorID]seatSession{},
		handles:  map[model.VendorID]seatProcess{},
	}
	h.room = Room{
		Version:   RoomVersion,
		Workspace: cfg.Workspace,
		Posture:   postureWord(cfg.Posture),
		Seats:     make([]Seat, 0, len(cfg.Roster)),
	}
	reg := vendors.Registry()
	for _, e := range cfg.Roster {
		s := Seat{Vendor: e.Vendor, Binary: e.Binary, Phase: PhaseIdle, Drivable: true}
		switch {
		case e.Binary == "":
			s.Drivable, s.Phase = false, PhaseUndrivable
			s.Note = "no binary was resolved for this seat"
		case reg[e.Vendor] == nil:
			s.Drivable, s.Phase = false, PhaseUndrivable
			s.Note = "no adapter exists for this seat"
		default:
			if _, conversational := reg[e.Vendor].(vendors.Conversational); conversational {
				// A conversational seat cannot be driven by writing a line: its
				// turn cannot be built until the vendor has answered a request
				// of the room's own, and it asks questions back on the same
				// pipe (design.md §9.36). Refusing it in words is the honest
				// state. Dispatching to it and drawing a column that never
				// finishes would be the same fault as a blocked seat rendered
				// as a slow one.
				s.Drivable, s.Phase = false, PhaseUndrivable
				s.Note = "this seat speaks a request/response protocol the host does not drive yet"
			}
		}
		h.room.Seats = append(h.room.Seats, s)
	}
	return h, nil
}

func postureWord(p vendors.Posture) string {
	switch p {
	case vendors.PostureRead:
		return "read"
	case vendors.PostureWriteGated:
		return "gated"
	default:
		return "write"
	}
}

// Serve runs the host until the client goes away or ctx ends.
//
// The order of the first three steps is load-bearing:
//
//  1. The room JOB is created first, and the host puts itself in it before any
//     seat exists. A host that assigned itself later would have a window in
//     which its own children were outside the containment it claims.
//  2. The PIPE is created second, so that "the pipe opens" only ever becomes
//     true after the containment is in place.
//  3. Only then is a client accepted.
//
// Detach IS exposed (design.md §7.29), so this no longer returns on every
// client that goes away. It returns when a client ENDS the room — by a shutdown
// frame, or by a bare disconnect, which still means the same thing it meant in
// §7.28. A client that DETACHES sends a frame saying so, and this loops back to
// Accept with every seat still running.
func (h *Host) Serve(ctx context.Context) error {
	job, err := newRoomJob()
	if err != nil {
		return err
	}
	h.job = job

	// A caught SIGTERM or SIGINT ends the room the way a shutdown frame does:
	// Shutdown kills every seat and closes what Serve is parked on, and Serve
	// returns on its ordinary path, removing host.json on the way out. On
	// Windows the channel is nil and this goroutine only ever sees stopWatch;
	// roomjob_unix.go says why the Unix host must reap for itself. Installed
	// before the socket exists, so there is no moment at which the host is
	// reachable and cannot be told to end.
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-stopWatch:
		case <-job.Signalled():
			h.ended.Store(true)
			h.Shutdown()
		}
	}()
	// WAITED for, not merely stopped. A signal-driven Shutdown runs on that
	// goroutine, and Serve returning — and the host process exiting on its
	// heels — while the listener is still unlinking its nodes was measured
	// leaving a cleanly ended host's lock files on disk.
	defer func() {
		close(stopWatch)
		<-watchDone
	}()

	ln, err := Listen(h.cfg.PipeName)
	if err != nil {
		// The job is NOT closed here. No seat exists yet, and closing it would
		// terminate this process before this error could be printed — see
		// Shutdown. Process exit releases the handle.
		return err
	}
	h.ln = ln

	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	defer h.Shutdown()

	// The discovery file is written AFTER the pipe exists, and it is removed on
	// the way out. The order matters in one direction only: the file says WHAT
	// is there and the pipe says WHETHER, so a file that appeared before the
	// pipe would describe a host nothing could reach. A file left behind by a
	// hard kill is the normal case and is not a fault — nothing reads it for
	// liveness (see Liveness).
	h.startedAt = time.Now()
	if h.cfg.CouncilDir != "" {
		seats := make([]model.VendorID, 0, len(h.cfg.Roster))
		for _, e := range h.cfg.Roster {
			seats = append(seats, e.Vendor)
		}
		// Numbers and keys only: no prompt, no reply, no brief. The whole
		// argument for this fourth council write is in HostFile's doc.
		if err := WriteHostFile(h.cfg.CouncilDir, HostFile{
			PID: os.Getpid(), Pipe: h.cfg.PipeName, StartedAt: h.startedAt,
			Workspace: h.cfg.Workspace, Seats: seats,
		}); err != nil {
			// Not fatal. A room that could not write its discovery file still
			// works for the client that started it; what is lost is the ability
			// of a LATER launch to say what is here, and that is worth a
			// degraded surface rather than a refused room (§4a.1's rule that a
			// partial read degrades a field and does not fail the row).
			h.mu.Lock()
			h.room.Notice = "this host could not write its discovery file: " + err.Error()
			h.dirty = true
			h.mu.Unlock()
		}
		defer func() { _ = RemoveHostFile(h.cfg.CouncilDir) }()
	}

	go h.fold()

	// The FIRST accept is BOUNDED, and this is a leak guard rather than a
	// timeout for its own sake.
	//
	// Accept blocks in the kernel and does not watch ctx, so without this a host
	// whose client never arrives would sit there forever holding a job object
	// full of nothing — a process the operator has no surface to find yet, which
	// is exactly the stale-host failure §7.28 names, arriving on the host's
	// first second. A host is STARTED BY a client, so a client that has not
	// connected in this long is a client that is not coming.
	//
	// The deadline covers a process start and two kernel objects, not a vendor
	// launch, so it is generous by an order of magnitude.
	//
	// # It applies to the first connect ONLY, and after a detach it must NOT
	//
	// Every later accept waits without a deadline, and that is a ruling rather
	// than an oversight. §7.28 refuses to let a stale host self-terminate on
	// idle — a detached room that dies on its own is precisely the failure the
	// operator cannot see — and a deadline on the rejoin accept would be that
	// self-termination wearing a different name. What answers a stale host is
	// discovery and `telltale council kill`, both of which shipped before this.
	first := true
	for {
		conn, err := h.accept(ctx, first)
		if err != nil {
			if h.ended.Load() {
				return nil
			}
			return err
		}
		h.pmu.Lock()
		h.active = conn
		h.pmu.Unlock()
		err = h.serveClient(ctx, conn)
		h.pmu.Lock()
		h.active = nil
		h.pmu.Unlock()
		conn.Close()
		if h.ended.Load() {
			return nil
		}
		if !errors.Is(err, errClientDetached) {
			// A shutdown frame, a bare disconnect, or a real failure. All three
			// end the room, and the deferred Shutdown above is what ends it.
			return err
		}
		// The client left and the seats are still running. The listener handed
		// its instance to that Conn, so a fresh one is needed before anybody can
		// come back — Rearm's doc carries the window that opens and why §7.24's
		// boundary bounds it.
		if err := h.ln.Rearm(); err != nil {
			return err
		}
		first = false
	}
}

// accept takes one client, bounding the wait only on the FIRST one.
//
// Split out of Serve because the loop needs it twice and the two calls differ by
// one bit that carries a ruling: a bounded first connect is a leak guard, and a
// bounded LATER connect would be the idle self-termination §7.28 refuses.
func (h *Host) accept(ctx context.Context, bounded bool) (*Conn, error) {
	stopAccept := make(chan struct{})
	go func() {
		if bounded {
			select {
			case <-stopAccept:
			case <-ctx.Done():
				h.ln.Close()
			case <-time.After(firstConnectWindow):
				h.ln.Close()
			}
			return
		}
		select {
		case <-stopAccept:
		case <-ctx.Done():
			h.ln.Close()
		}
	}()

	conn, err := h.ln.Accept()
	close(stopAccept)
	if err != nil {
		if bounded && errors.Is(err, ErrListenerClosed) && ctx.Err() == nil {
			return nil, fmt.Errorf("councilhost: no client connected within %s — "+
				"a host is started by a client, so it does not wait for one that is not coming",
				firstConnectWindow)
		}
		return nil, err
	}
	return conn, nil
}

// Shutdown kills every seat and releases the room.
//
// The seats are asked to die FIRST, through the per-seat jobs, so each gets the
// ordinary teardown.
//
// # The room job's handle is deliberately NOT closed here
//
// Closing it is a TERMINATION of this process, because this process is in the
// job and holds the only handle. An earlier version closed it at the end of
// this function and on Serve's Listen-failure path, which made every error the
// host could report unreachable: the process died inside Shutdown, before the
// error returned through Serve to runCouncilHost and onto stderr. The two
// errors an operator most needs — "this pipe name is already taken" and a
// failed accept — were computed and then destroyed by the host's own
// containment, leaving the client with nothing but a dial timeout.
//
// Nothing is lost by not closing it. The handle is released when the process
// exits, which is the same event by a different route, and the job reaps
// anything that outlived the kills above exactly as it would have. That is the
// mechanism the design always described: the containment is a consequence of
// the host dying, not an action the host takes.
func (h *Host) Shutdown() {
	h.pmu.Lock()
	sessions := h.sessions
	handles := h.handles
	active := h.active
	h.sessions = map[model.VendorID]seatSession{}
	h.handles = map[model.VendorID]seatProcess{}
	h.active = nil
	h.pmu.Unlock()
	for _, s := range sessions {
		s.Kill()
	}
	for _, hd := range handles {
		hd.Kill()
	}
	if h.roomCancel != nil {
		h.roomCancel()
	}
	if h.ln != nil {
		h.ln.Close()
	}
	if active != nil {
		// After the listener, so a client woken by this close cannot dial
		// back into a listener that is still taking clients.
		active.Close()
	}
}

// fold drains the seats and folds every event into the room.
//
// This loop is the whole difference between a host and a babysitter.
// runner.pumpStdout drains each child's stdout and nothing else does; if this
// stopped reading, the operating system's pipe buffer would fill and the vendor
// would block mid-turn. A room that silently stops working when nobody is
// watching is the opposite of what the split is for (design.md §7.28).
func (h *Host) fold() {
	for {
		select {
		case ev := <-h.events:
			h.mu.Lock()
			if h.room.Apply(ev) {
				h.dirty = true
			}
			h.mu.Unlock()
		case <-h.roomCtx.Done():
			return
		}
	}
}

// serveClient runs the handshake and then the frame loop for one client.
func (h *Host) serveClient(ctx context.Context, conn *Conn) error {
	fr := NewFrameReader(conn)
	fw := NewFrameWriter(conn)

	hello, err := fr.Read()
	if err != nil {
		return fmt.Errorf("councilhost: the client sent no handshake: %w", err)
	}
	if hello.Kind != KindHello {
		_ = fw.Write(Frame{Kind: KindRefused, Reason: "the first frame must be a hello"})
		return errors.New("councilhost: the client's first frame was not a hello")
	}
	if hello.Protocol != ProtocolVersion {
		// Refused rather than negotiated. A host and a client are the same
		// binary by construction — the client starts the host from its own
		// executable path — so a mismatch means two telltale builds are
		// talking, which is a state to name.
		reason := fmt.Sprintf("this host speaks protocol %d and the client speaks %d — "+
			"they are different telltale builds", ProtocolVersion, hello.Protocol)
		_ = fw.Write(Frame{Kind: KindRefused, Reason: reason, Protocol: ProtocolVersion})
		return errors.New("councilhost: " + reason)
	}
	if err := fw.Write(Frame{
		Kind: KindWelcome, Protocol: ProtocolVersion, HostPID: os.Getpid(),
	}); err != nil {
		return err
	}

	// The first room goes out immediately rather than on the next tick, so a
	// client that connected to an idle room draws something at once instead of
	// showing nothing for up to one tick.
	//
	// It is sent BEFORE pump starts, and the order is not cosmetic. Starting the
	// ticker first leaves a window in which a newer room could go out and this
	// snapshot land after it — the writer's lock stops the two frames from
	// corrupting each other, and does nothing at all about the client then
	// rendering the older one until the next change.
	h.mu.Lock()
	first := h.room.clone()
	h.dirty = false
	h.mu.Unlock()
	if err := fw.Write(Frame{Kind: KindRoom, Room: first}); err != nil {
		return err
	}

	stop := make(chan struct{})
	defer close(stop)
	go h.pump(fw, stop)

	for {
		f, err := fr.Read()
		if err != nil {
			// A bare disconnect ends the room, exactly as an explicit shutdown
			// does. That is what keeps detach unexposed: the two paths are
			// deliberately the same, and rung 4 is where they stop being.
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		switch f.Kind {
		case KindDetach:
			// The refusal is answered on THIS connection and the loop carries
			// on, because a refused detach leaves the client exactly where it
			// was. Returning here would end the room the operator was told they
			// could not leave, which is the one outcome worse than either.
			if err := h.detachAllowed(); err != nil {
				_ = fw.Write(Frame{Kind: KindRefused, Reason: err.Error()})
				continue
			}
			_ = fw.Write(Frame{Kind: KindDetached, HostPID: os.Getpid()})
			return errClientDetached
		case KindDispatch:
			// On its OWN goroutine, so the read loop keeps answering while a
			// broadcast is in progress. A dispatch builds a spec and starts a
			// process per seat, and running that inline meant the host read
			// nothing for its whole span — so an interrupt sent mid-broadcast,
			// which KindInterrupt's doc calls the room's ctrl+c, was not acted
			// on until the broadcast finished, and an adapter that blocked took
			// the control channel with it.
			//
			// Safe because dispatch refuses to start a second turn while one is
			// running, so these goroutines cannot overlap on the seats.
			go h.dispatch(f.Prompt)
		case KindInterrupt:
			h.interrupt()
		case KindShutdown:
			return nil
		default:
			// An unknown frame is DROPPED rather than fatal, on the same rule
			// runner.ParseFunc follows: a parser ignores what it does not model
			// instead of failing the turn on a shape it has not seen.
		}
	}
}

// detachAllowed is design.md §7.29's unwatched-write ruling, asked of this
// room's posture.
//
// One condition, and UnwatchedWriteRefusal's doc has the whole argument for why
// three conditions collapse into it: the host never runs a gated room, so a
// hosted room that is not read-only is an ungated write room, which is exactly
// what `--auto` means on the room's own surface.
//
// The error carries BOTH lines — the refusal and its remedy — because this is
// the only place they travel together, and a client that had to reassemble them
// could drop the half that says what to do instead.
func (h *Host) detachAllowed() error {
	if h.cfg.Posture == vendors.PostureRead {
		return nil
	}
	return errors.New(UnwatchedWriteRefusal + "\n" + UnwatchedWriteRemedy)
}

// pump sends the room whenever it has changed, at most once per tick.
func (h *Host) pump(fw *FrameWriter, stop <-chan struct{}) {
	t := time.NewTicker(h.tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-h.roomCtx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			if !h.dirty {
				h.mu.Unlock()
				continue
			}
			h.dirty = false
			snap := h.room.clone()
			h.mu.Unlock()
			if err := fw.Write(Frame{Kind: KindRoom, Room: snap}); err != nil {
				return
			}
		}
	}
}

// dispatch broadcasts one turn to every drivable seat.
//
// Every seat is dispatched INDEPENDENTLY and a failure on one is that seat's
// card, never the turn's. Three of these vendors are separate programs with
// separate ways of being unhappy, and a room that abandoned the broadcast on
// the first refusal would lose the answers of the seats that were fine.
func (h *Host) dispatch(prompt string) {
	if prompt == "" {
		return
	}
	h.mu.Lock()
	if h.inFlight {
		// REFUSED, and said out loud. A room that silently swallowed the second
		// turn would look identical to one that lost it, and a room that ran it
		// anyway would leave the first turn's children unreachable.
		h.room.Notice = "a turn is already running in this room — wait for it, or interrupt it"
		h.dirty = true
		h.mu.Unlock()
		return
	}
	h.inFlight = true
	h.room.Notice = ""
	h.room.beginTurn()
	h.dirty = true
	seats := make([]Seat, len(h.room.Seats))
	copy(seats, h.room.Seats)
	h.mu.Unlock()

	reg := vendors.Registry()
	for _, s := range seats {
		if !s.Drivable {
			continue
		}
		if err := h.dispatchSeat(reg[s.Vendor], s, prompt); err != nil {
			h.noteSeat(s.Vendor, PhaseFailed, err.Error())
		}
	}
	go h.watchTurn()
	h.refreshHostFile()
}

// refreshHostFile rewrites the discovery file so its turn count is not a lie.
//
// HostFile.Turn says "how many turns the host has dispatched", and it used to
// be written once at startup and never again — so a room twenty turns in
// described itself as having run none. A stale number in a file whose whole
// purpose is to say what is there is worse than no number: a later reader
// cannot tell it from a room that really has done nothing.
//
// A failure is ignored on purpose, for the same reason the first write's is:
// this degrades a discovery surface and must never fail a turn.
func (h *Host) refreshHostFile() {
	if h.cfg.CouncilDir == "" {
		return
	}
	h.mu.Lock()
	turn := h.room.Turn
	h.mu.Unlock()
	seats := make([]model.VendorID, 0, len(h.cfg.Roster))
	for _, e := range h.cfg.Roster {
		seats = append(seats, e.Vendor)
	}
	_ = WriteHostFile(h.cfg.CouncilDir, HostFile{
		PID: os.Getpid(), Pipe: h.cfg.PipeName, StartedAt: h.startedAt,
		Workspace: h.cfg.Workspace, Seats: seats, Turn: turn,
	})
}

// watchTurn clears the in-flight flag once no seat is still running.
//
// It polls the room's own phases rather than counting terminal events, because
// the phases are what the operator sees: a room that said "a turn is already
// running" while every column read `done` would be the host disagreeing with
// its own screen. A seat that never settles holds the flag, which is the
// honest outcome — the way out of that is the interrupt, not a timer that
// declares a running turn over.
func (h *Host) watchTurn() {
	t := time.NewTicker(25 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-h.roomCtx.Done():
			return
		case <-t.C:
			h.mu.Lock()
			running := false
			for i := range h.room.Seats {
				switch h.room.Seats[i].Phase {
				case PhaseWaiting, PhaseStreaming:
					running = true
				}
			}
			if !running {
				h.inFlight = false
			}
			h.mu.Unlock()
			if !running {
				return
			}
		}
	}
}

// dispatchSeat sends one turn to one seat.
func (h *Host) dispatchSeat(v vendors.Vendor, s Seat, prompt string) error {
	if v == nil {
		return errors.New("no adapter for this seat")
	}
	if p, ok := v.(vendors.Persistent); ok {
		return h.dispatchPersistent(p, s, prompt)
	}
	return h.dispatchBatch(v, s, prompt)
}

// dispatchPersistent feeds a turn to a seat that is ONE process taking many
// turns.
//
// The process is started on the first turn and never before it: council never
// starts a vendor to see whether it answers, and a host that pre-warmed its
// seats would spend a session init on a room nobody typed into.
func (h *Host) dispatchPersistent(p vendors.Persistent, s Seat, prompt string) error {
	h.pmu.Lock()
	sess, live := h.sessions[s.Vendor]
	if live && !sess.Alive() {
		delete(h.sessions, s.Vendor)
		live = false
	}
	h.pmu.Unlock()

	if !live {
		// hooksFile is empty here. Carrying the operator's own hooks into a
		// hosted seat means writing gatehook.go's ephemeral settings file from
		// this process, and that file's whole argument is about a gate this
		// host refuses to run. An empty path is the honest state rather than a
		// path to a file nobody wrote.
		var (
			spec runner.Spec
			err  error
		)
		if s.SessionID != "" {
			spec, err = p.SessionResume(h.cfg.Workspace, s.Binary, "", s.SessionID, h.cfg.Posture)
			if errors.Is(err, vendors.ErrNoResume) {
				spec, err = p.Session(h.cfg.Workspace, s.Binary, "", h.cfg.Posture)
			}
		} else {
			spec, err = p.Session(h.cfg.Workspace, s.Binary, "", h.cfg.Posture)
		}
		if err != nil {
			return err
		}
		sess, err = startSession(h.roomCtx, spec, h.events, v0Parse(p))
		if err != nil {
			return err
		}
		h.pmu.Lock()
		h.sessions[s.Vendor] = sess
		h.pmu.Unlock()
	}

	line, err := p.Turn(prompt)
	if err != nil {
		return err
	}
	return sess.SendTurn([][]byte{line})
}

// dispatchBatch spawns one child for one turn.
func (h *Host) dispatchBatch(v vendors.Vendor, s Seat, prompt string) error {
	var (
		spec runner.Spec
		err  error
	)
	if s.SessionID != "" {
		spec, err = v.NextTurn(prompt, h.cfg.Workspace, s.Binary, s.SessionID, h.cfg.Posture)
		if errors.Is(err, vendors.ErrNoResume) {
			spec, err = v.FirstTurn(prompt, h.cfg.Workspace, s.Binary, h.cfg.Posture)
		}
	} else {
		spec, err = v.FirstTurn(prompt, h.cfg.Workspace, s.Binary, h.cfg.Posture)
	}
	if err != nil {
		return err
	}
	hd, err := startProcess(h.roomCtx, spec, h.events, v.ParseEvent)
	if err != nil {
		return err
	}
	h.pmu.Lock()
	// The previous turn's child is KILLED before its handle is replaced, and
	// this is a belt beside the in-flight refusal rather than a duplicate of it.
	// A batch seat says its turn is over by dying, so ordinarily this handle is
	// already spent — but "ordinarily" was doing load-bearing work here, and
	// dropping a live child from this map makes it unreachable by interrupt and
	// by Shutdown, with only the room job left to reap it at host death.
	// Killing a dead child costs nothing; losing a live one costs an agent
	// nobody can see.
	if prev := h.handles[s.Vendor]; prev != nil {
		prev.Kill()
	}
	h.handles[s.Vendor] = hd
	h.pmu.Unlock()
	return nil
}

// v0Parse is the vendor's own line parser, named so the spawn call reads as
// what it is.
func v0Parse(p vendors.Persistent) runner.ParseFunc { return p.ParseEvent }

// interrupt asks every running seat to abandon the turn in flight.
//
// A persistent seat is INTERRUPTED and not killed, because keeping the process
// is the whole point of it: killing one would cost the next turn a session init
// that was already measured at about 25 seconds and $0.23. A batch seat has no
// such channel — its stdin was written and closed before the first token
// arrived — so for that one the kill IS the interrupt.
func (h *Host) interrupt() {
	h.pmu.Lock()
	h.interrupts++
	id := fmt.Sprintf("host-%d", h.interrupts)
	sessions := make(map[model.VendorID]seatSession, len(h.sessions))
	for k, v := range h.sessions {
		sessions[k] = v
	}
	handles := make(map[model.VendorID]seatProcess, len(h.handles))
	for k, v := range h.handles {
		handles[k] = v
	}
	h.pmu.Unlock()

	reg := vendors.Registry()
	for vid, sess := range sessions {
		p, ok := reg[vid].(vendors.Persistent)
		if !ok {
			continue
		}
		line, err := p.Interrupt(id)
		if err != nil {
			continue
		}
		_ = sess.SendAside([][]byte{line})
	}
	for _, hd := range handles {
		hd.Kill()
	}
	h.mu.Lock()
	for i := range h.room.Seats {
		switch h.room.Seats[i].Phase {
		case PhaseWaiting, PhaseStreaming:
			// Cancelled and not Failed. Output already on screen was really
			// produced, and blaming the vendor for the operator's keystroke is
			// the distinction council.PhaseCancelled exists for.
			h.room.Seats[i].Phase = PhaseCancelled
		}
	}
	h.dirty = true
	h.mu.Unlock()
}

// noteSeat records a host-side refusal on one seat's card.
func (h *Host) noteSeat(v model.VendorID, ph Phase, note string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if i := h.room.seatIndex(v); i >= 0 {
		h.room.Seats[i].Phase = ph
		h.room.Seats[i].Note = note
		h.dirty = true
	}
}

// Snapshot copies the room out. For tests and for a caller that wants the state
// without the wire.
func (h *Host) Snapshot() Room {
	h.mu.Lock()
	defer h.mu.Unlock()
	return *h.room.clone()
}
