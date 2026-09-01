package councilhost

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
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
	startProcess = runner.Start
)

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

// ErrGatedPostureNotHosted is why a gated room is refused.
var ErrGatedPostureNotHosted = errors.New(
	"councilhost: a gated seat blocks on a question this host cannot carry yet — " +
		"run that room with `telltale council`")

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
	handles  map[model.VendorID]*runner.Handle

	// roomCtx bounds every seat and is cancelled only by teardown. It is
	// deliberately NOT a turn's context: the whole value of a persistent seat
	// is that cancelling one turn does not cost the next one a session init.
	roomCtx    context.Context
	roomCancel context.CancelFunc

	interrupts int
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
		handles:  map[model.VendorID]*runner.Handle{},
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
// Detach is NOT exposed, so this returns when the client disconnects, and the
// caller tears the room down. A host that outlived its client is rung 4.
func (h *Host) Serve(ctx context.Context) error {
	job, err := NewRoomJob()
	if err != nil {
		return err
	}
	h.job = job

	ln, err := Listen(h.cfg.PipeName)
	if err != nil {
		h.job.Close()
		return err
	}
	h.ln = ln

	h.roomCtx, h.roomCancel = context.WithCancel(ctx)
	defer h.Shutdown()

	go h.fold()

	conn, err := ln.Accept()
	if err != nil {
		return err
	}
	defer conn.Close()
	return h.serveClient(ctx, conn)
}

// Shutdown kills every seat and releases the room.
//
// The order is the reverse of Serve's and is just as deliberate. The seats are
// asked to die FIRST, through the per-seat jobs, so each gets the ordinary
// teardown. The room job's handle is closed LAST, because closing it is a
// termination that gives a process no chance to finish a write — it is the
// backstop for the paths that never reach here, not the ordinary route.
func (h *Host) Shutdown() {
	h.pmu.Lock()
	sessions := h.sessions
	handles := h.handles
	h.sessions = map[model.VendorID]seatSession{}
	h.handles = map[model.VendorID]*runner.Handle{}
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
	if h.job != nil {
		h.job.Close()
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

	stop := make(chan struct{})
	defer close(stop)
	go h.pump(fw, stop)
	// The first room goes out immediately rather than on the next tick, so a
	// client that connected to an idle room draws something at once instead of
	// showing nothing for up to one tick.
	h.mu.Lock()
	first := h.room.clone()
	h.dirty = false
	h.mu.Unlock()
	if err := fw.Write(Frame{Kind: KindRoom, Room: first}); err != nil {
		return err
	}

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
		case KindDispatch:
			h.dispatch(f.Prompt)
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
	// A previous turn's handle is replaced rather than kept. The child that
	// produced it has already exited — a batch seat says the turn is over by
	// dying — so holding it would only keep a dead job handle alive.
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
	handles := make(map[model.VendorID]*runner.Handle, len(h.handles))
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
