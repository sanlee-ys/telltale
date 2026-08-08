package council

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// startSession and startProcess are the council package's ONLY two process
// spawns, behind vars so the security tests can count them.
//
// A var rather than an injected interface because the property under test is
// "nothing was spawned", and the cheapest honest way to assert that is to make
// the real call site countable. These are not mocks of vendor behaviour — a
// mock that invents a reply this product never produces is how a test ends up
// asserting the flag instead of the effect. They wrap the actual functions and
// production never replaces them.
var startSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, parse runner.ParseFunc) (seatSession, error) {
	return runner.StartSession(ctx, spec, out, parse)
}

// startRPCSession is the same spawn for a seat whose protocol answers back.
// Behind its own var for the same reason startSession is: the security tests
// count spawns, and a second call site that escaped the count would be a hole in
// exactly the assertion those tests exist to make.
var startRPCSession = func(ctx context.Context, spec runner.Spec, out chan<- runner.Event, proto runner.Protocol) (seatSession, error) {
	return runner.StartRPCSession(ctx, spec, out, proto)
}

var startProcess = runner.Start

// seatSession is the slice of runner.Session the seat logic drives.
//
// An interface for exactly one reason: the /cd respawn path kills a LIVE
// process, and a test that needed a real child just to watch it be killed
// would be spawning processes to check a branch. runner.Session is the only
// production implementation.
type seatSession interface {
	SendTurn(lines [][]byte) error
	SendAside(lines [][]byte) error
	Kill()
	Alive() bool
}

// seatWire is how the room says something to a live seat.
//
// Two shapes implement it and the room does not care which: a stateless
// stream-json adapter, wrapped by streamWire below, and a per-process
// runner.Protocol. Turning both into "here are the lines to write" at this
// boundary is what keeps a single dispatch path — the alternative was an
// `if conversational` at three call sites, each of which would have to be kept
// in step with the other two.
type seatWire interface {
	Turn(prompt string) ([][]byte, error)
	Interrupt(id string) ([][]byte, error)
	Decide(requestID string, allow bool, reason string, input map[string]any) ([][]byte, error)
}

// streamWire adapts a Persistent adapter, whose methods each produce exactly one
// line and can produce it at any time, to the many-lines-or-none shape an RPC
// protocol needs.
type streamWire struct{ v vendors.Persistent }

func (w streamWire) Turn(prompt string) ([][]byte, error) { return oneLine(w.v.Turn(prompt)) }

func (w streamWire) Interrupt(id string) ([][]byte, error) { return oneLine(w.v.Interrupt(id)) }

func (w streamWire) Decide(requestID string, allow bool, reason string, input map[string]any) ([][]byte, error) {
	return oneLine(w.v.Decide(requestID, allow, reason, input))
}

func oneLine(line []byte, err error) ([][]byte, error) {
	if err != nil {
		return nil, err
	}
	return [][]byte{line}, nil
}

// seatProc is one seat's long-lived vendor process.
//
// It outlives a turn on purpose and outlives the room never: teardown kills it,
// and the job object kills it again if telltale itself dies first.
type seatProc struct {
	sess seatSession
	// wire is how this process is spoken to. Held per PROCESS rather than looked
	// up per call, because for a Conversational seat it IS the process's state:
	// the session id, the turns queued behind a handshake, and the ids of the
	// questions the vendor is currently blocked on all live on it, and a fresh
	// one looked up from the registry would know none of them.
	wire seatWire
	// sent counts turns handed to THIS process. Zero means the next one is its
	// first, which is what decides whether the shared brief goes with it.
	//
	// Counted per PROCESS rather than per vendor, and that is the whole reason
	// it is not a boolean on the model: a seat whose process died and was
	// replaced is a stranger again, and the replacement has to be briefed like
	// the original was. A per-vendor flag would have remembered a briefing that
	// happened in a session that no longer exists.
	sent int
	// resumed reports that this process was launched on a session id restored
	// from a saved room, rather than opening a new conversation.
	//
	// Its only job is the brief: a resumed process already has the operating
	// context in the history it is replaying, so re-sending it would spend the
	// whole brief again for nothing. Whether the resume WORKED is not tracked
	// here — that is settleRestoredThread's question, and it is asked the same
	// way for all four seats rather than twice in two places.
	resumed bool
	// dir is the workspace this process was spawned in, and posture is what it
	// was spawned with. A mismatch against what the room now wants is what tells
	// seatProcess this process cannot serve the turn and has to be replaced.
	//
	// On the stream-json seat both are ARGV and the rule is forced: verified
	// against the live headless docs, the envelope has no cwd field and no way to
	// change the permission flags mid-session, so the documented way to move a
	// conversation is to respawn under --resume (§9.16, ninth amendment).
	//
	// On the ACP seat neither is argv, and the rule is therefore a CHOICE — one
	// worth naming here rather than leaving as an accident of shared code.
	// MEASURED (§9.36): `cwd` is a session/new parameter and the posture mode is
	// a session/set_mode call, and one process really did run two sessions in two
	// different directories, each reading its own file. So a move could cost a
	// new SESSION (~1s) instead of a new PROCESS (~3s). It costs a process
	// anyway, deliberately: what a move actually costs the user is a new
	// conversation either way, one rule across four seats is worth more than
	// three seconds, and re-opening a session inside a live process has failure
	// modes — a half-moved session, a queued turn addressed to the old one — that
	// nothing has measured. Revisit with a measurement, not with a preference.
	dir     string
	posture vendors.Posture
}

// sendPersistentTurn hands one turn to a seat's process, starting one if there
// is none. The returned string is a note for the column, empty when nothing
// worth saying happened.
func (m *Model) sendPersistentTurn(v vendors.Vendor, c *Column, prompt string) (string, error) {
	note, err := m.handTurnToSeat(v, c, prompt)
	if !errors.Is(err, vendors.ErrACPHandshakeFailed) {
		return note, err
	}
	// ONE retry, and only against this one error, which names a process that is
	// UP and useless.
	//
	// The retry is what makes the recovery whole rather than half. The seat has
	// just been killed and forgotten by the attempt above, so a second attempt
	// spawns a fresh process — and without it the user would spend one brief
	// discovering the seat was dead, get an error naming a handshake they cannot
	// see, and only then get a working column on the brief after that. The usual
	// cause is an unauthenticated CLI, which they may well have fixed between the
	// two.
	//
	// It cannot loop. A just-spawned protocol has not failed a handshake, so the
	// only error that reaches this line comes from a process that no longer
	// exists, and the second attempt's failure — whatever it is — is returned.
	return m.handTurnToSeat(v, c, prompt)
}

// handTurnToSeat is one attempt: find or launch the process, brief it if it is
// new, and hand it the turn.
func (m *Model) handTurnToSeat(v vendors.Vendor, c *Column, prompt string) (string, error) {
	p, note, err := m.seatProcess(v, c)
	if err != nil {
		return "", err
	}

	// First turn for THIS process, so it gets the operating context. Per
	// process rather than per room: a seat that respawned is unbriefed again,
	// and would otherwise be the only one guessing.
	//
	// A RESUMED process is the exception, and it is the same rule rather than a
	// carve-out from it: the brief is already in the history it is replaying, so
	// re-sending it would spend the whole thing again per turn against a metered
	// quota for a vendor that has already read it (brief.Apply's own reasoning).
	if p.sent == 0 && !p.resumed {
		prompt = m.brief.Apply(prompt)
	}

	lines, err := p.wire.Turn(prompt)
	if err != nil {
		// The protocol will not take another turn. On the ACP seat that means the
		// handshake failed, which leaves a process that is UP and useless — and a
		// live process is exactly what the stale-exit guard reads as "this seat is
		// fine", so leaving it registered would hand it every subsequent brief and
		// hang on each one.
		p.sess.Kill()
		m.dropProcess(c.Vendor)
		return "", err
	}
	// SendTurn rather than a write per line, and it starts the clock even when
	// there are NO lines: an ACP protocol may take a turn it cannot encode until
	// its handshake finishes, and the person who pressed enter is waiting from
	// this moment either way.
	if err := p.sess.SendTurn(lines); err != nil {
		// The process is there but will not take the turn. Dropping the seat
		// means the next brief starts a fresh one rather than writing into a
		// pipe nobody reads.
		m.dropProcess(c.Vendor)
		return "", err
	}
	p.sent++
	return note, nil
}

// liveSeat reports whether this vendor is driven as one long-lived process, by
// either of the two shapes that can be.
//
// One predicate rather than two interface assertions repeated at every call
// site: the room's question is "does this seat keep a process", and which
// protocol answers turns on that process is nobody's business above this file.
func liveSeat(v vendors.Vendor) bool {
	switch v.(type) {
	case vendors.Conversational, vendors.Persistent:
		return true
	}
	return false
}

// seatProcess returns the seat's process, launching one if it has none or if
// the one it had has gone.
// spawnPosture collapses the postures a given seat's PROCESS cannot tell apart.
//
// It exists because seatProcess respawns on a posture mismatch, and for the ACP
// seat two of the three postures produce a byte-identical process: `agent` mode
// either way, and whether a permission request becomes a card or an automatic
// yes is a decision the ROOM makes when one arrives, not a flag the process was
// launched with. Without this, pressing `a` mid-room would cost that seat a
// process and a session reload, and the column would announce "this hop needs a
// different posture" about a change its authority never saw.
//
// The stream-json seat is left alone, where the distinction IS argv: the gated
// posture passes three flags the ungated one does not, one of which is what
// makes the vendor ask at all.
func spawnPosture(v vendors.Vendor, p vendors.Posture) vendors.Posture {
	if _, ok := v.(vendors.Conversational); ok && p == vendors.PostureWriteGated {
		return vendors.PostureWrite
	}
	return p
}

func (m *Model) seatProcess(v vendors.Vendor, c *Column) (*seatProc, string, error) {
	existing, had := m.procs[c.Vendor]
	want := spawnPosture(v, m.seatPosture())
	moved := false
	repostured := false
	if had && existing.sess.Alive() {
		if sameDir(existing.dir, m.st.Workspace) && existing.posture == want {
			return existing, "", nil
		}
		// Two reasons a live process cannot serve this turn, and one remedy.
		//
		// The room moved (/cd) and this process is pinned to the old directory
		// — cwd is fixed at spawn, so the documented way to move a conversation
		// is the one the ninth amendment measured: respawn with --resume. The
		// earned id goes through the SAME one-attempt probation the restored
		// ids use, because a resume that fails here would otherwise be rebuilt
		// on every turn for the life of the room.
		//
		// Or a /flow hop needs a posture this process was not spawned with. The
		// alternative was to send the turn anyway, which is precisely the silent
		// downgrade (or silent upgrade) the per-step posture rule exists to
		// forbid: the column would say READ while the live process still held
		// the write flags it was launched with. Respawning costs one process and
		// carries the thread across on the same measured --resume composition.
		moved = !sameDir(existing.dir, m.st.Workspace)
		repostured = !moved
		existing.sess.Kill()
		m.dropProcess(c.Vendor)
		if id := m.sessions[c.Vendor]; id != "" && m.resumeIDs[c.Vendor] == "" {
			m.resumeIDs[c.Vendor] = id
			m.unproven[c.Vendor] = true
		}
	}

	// A thread restored from a saved room is spent HERE, on the first process
	// this seat opens, and forgotten whether or not it works.
	//
	// One attempt, never a loop, and that is the load-bearing part. A stale id is
	// refused immediately by both protocols — the stream-json seat exits 1 with
	// `No conversation found with session ID` and no model turn spent, the ACP
	// seat answers `-32602 … Session "…" not found` in 0.45s and stays up — so a
	// seat that retried it would refuse every brief for the rest of the session
	// with the same error. Spent once, the next dispatch opens a new conversation
	// and briefs it, which is the behaviour seatProcess already had for a seat
	// whose process died.
	resumeID := m.resumeIDs[c.Vendor]
	if resumeID != "" {
		delete(m.resumeIDs, c.Vendor)
	}

	sess, wire, resumed, err := m.spawnSeat(v, c, resumeID, want)
	if err != nil {
		return nil, "", err
	}
	p := &seatProc{sess: sess, wire: wire, resumed: resumed, dir: m.st.Workspace, posture: want}
	m.procs[c.Vendor] = p

	if resumed {
		// Deliberately NOT "reattached to the saved thread". Nothing has come
		// back yet — the process has been launched and that is all — and a
		// column claiming a resume it has not seen evidence of is this repo's own
		// failure mode. The card the room opened with already says a thread was
		// restored; if the vendor refuses it, resumeFailed replaces that with the
		// truth.
		return p, "", nil
	}
	if repostured {
		// Said out loud for the same reason /cd's note is: a seat whose process
		// was replaced under it has a new history, and a column that quietly
		// restarted while claiming continuity is the silent divergence again.
		return p, "this hop needs a different posture — this seat is starting a new session for it", nil
	}
	if moved {
		// The move itself was announced by /cd; what needs saying is that THIS
		// seat had no thread to carry across, so its history starts here.
		return p, "the room moved — this seat is starting a new session there", nil
	}
	if had {
		// Said out loud. The thread really was lost, and a seat that quietly
		// forgot the conversation while its neighbours remembered theirs is the
		// kind of silent divergence this product exists to refuse.
		return p, "the previous process ended — this seat is starting a new session", nil
	}
	return p, "", nil
}

// spawnSeat launches one live seat, by whichever of the two protocols it speaks.
//
// The branch is here and nowhere else. Everything either side of it —
// probation, the brief, the notes, teardown — is one path for all four seats,
// which is the property worth protecting: a room that behaved differently
// depending on a vendor's wire format would be a room whose rules nobody could
// state.
func (m *Model) spawnSeat(v vendors.Vendor, c *Column, resumeID string, want vendors.Posture) (seatSession, seatWire, bool, error) {
	// The ROOM's context, never the turn's. A turn that is cancelled must not
	// take this process with it — that is the entire point of keeping it — so
	// only quitting the room cancels this.
	if cv, ok := v.(vendors.Conversational); ok {
		spec, proto, err := cv.Open(m.st.Workspace, c.Binary, resumeID, want)
		if err != nil {
			return nil, nil, false, err
		}
		if proto == nil {
			// Unreachable today — Cursor.Open always returns one — and checked
			// because the alternative failure is a nil dereference on the seat's
			// first turn rather than a column that says what went wrong.
			return nil, nil, false, errNotALiveSeat
		}
		sess, err := startRPCSession(m.roomCtx, spec, m.events, proto)
		if err != nil {
			return nil, nil, false, err
		}
		// resumed is claimed from the ATTEMPT, exactly as the stream-json branch
		// claims it: the id was handed to a protocol that will try to load it.
		// Whether it WORKED is settleRestoredThread's question, asked the same way
		// for all four seats — and if the load is refused the protocol opens a new
		// conversation in the same process, so the turn still runs and the seat
		// still reports honestly that its thread did not come back.
		return sess, proto, resumeID != "", nil
	}

	pv, ok := v.(vendors.Persistent)
	if !ok {
		return nil, nil, false, errNotALiveSeat
	}
	// The hooks file is handed over on every spawn rather than only the first.
	// A seat whose process died and was replaced is a stranger again — it is
	// already re-briefed for that reason — and a replacement that came back
	// unscreened while the badge still said the guard was wired would be the
	// quietest false claim in the room.
	spec, err := pv.Session(m.st.Workspace, c.Binary, m.hooks.Path, want)
	if err != nil {
		return nil, nil, false, err
	}
	resumed := false
	if resumeID != "" {
		if rs, rerr := pv.SessionResume(m.st.Workspace, c.Binary, m.hooks.Path,
			resumeID, want); rerr == nil {
			spec = rs
			resumed = true
		}
	}
	sess, err := startSession(m.roomCtx, spec, m.events, pv.ParseEvent)
	if err != nil {
		return nil, nil, false, err
	}
	return sess, streamWire{pv}, resumed, nil
}

// errNotALiveSeat is unreachable through dispatch, which asks liveSeat before it
// gets here. It exists so that a future seat added to the registry without a
// protocol fails loudly at its first brief rather than silently taking a path
// meant for a different one.
var errNotALiveSeat = errors.New("council: this seat has no live-process protocol")

// dropProcess forgets a seat's process. The kill is separate on purpose: a
// process that died on its own does not need killing, and one the room is
// tearing down is killed by teardown.
func (m *Model) dropProcess(v model.VendorID) {
	delete(m.procs, v)
}

// interruptSeat abandons the turn in flight on a persistent seat without
// killing the process.
//
// Falls back to a kill if the message cannot be queued, because a cancel that
// silently did nothing would leave the user watching a column they believe they
// stopped — the one outcome worse than paying for a new session.
func (m *Model) interruptSeat(v model.VendorID) {
	p, ok := m.procs[v]
	if !ok {
		return
	}
	m.interrupts++
	lines, err := p.wire.Interrupt("telltale-interrupt-" + strconv.Itoa(m.interrupts))
	if err == nil && p.sess.SendAside(lines) == nil {
		return
	}
	p.sess.Kill()
	m.dropProcess(v)
}

// queueGate puts one blocked tool call in front of the user.
//
// The vendor is stopped until this is answered, which is the property the whole
// feature rests on and also the reason nothing here may quietly drop a request:
// a gate that vanished would leave a column waiting forever with no card to
// explain it.
func (m *Model) queueGate(c *Column, g *runner.Gate) {
	if g == nil || g.RequestID == "" {
		return
	}
	// Two ways a call is approved without a card, and they are not the same
	// claim. autoApproveRoutine is a judgement about the CALL — this one is
	// routine, on a list that has been argued over. !Asking is a decision about
	// the ROOM: the user said stop asking, so nothing is being classified and
	// nothing is being trusted, the answer is just yes.
	//
	// It is checked here rather than left to the invocation because a process
	// already running keeps the gate flags it was spawned with. `a` pressed
	// mid-turn has to hold for the rest of THAT turn, or "stop asking" would
	// keep asking until the turn ended — the promise broken at the moment it
	// was made. The respawn that drops the flags happens later, on the next
	// dispatch, through seatPosture.
	if !m.st.Asking() || autoApproveRoutine(g) {
		m.sendDecision(PendingGate{
			Vendor: c.Vendor, RequestID: g.RequestID, ToolUseID: g.ToolUseID,
			Text: g.Text,
		}, true, g.Input)
		return
	}
	// Redacted whole, like every other complete string a vendor produced. It
	// matters more here than in prose: the argument line of a tool call is one
	// of the likeliest places for a token to appear on screen, and this one is
	// rendered in chrome that does not scroll away.
	text := strings.TrimSpace(m.redactWhole(g.Text))
	if text == "" {
		text = g.Tool
	}
	if m.gateInputs == nil {
		m.gateInputs = map[string]map[string]any{}
	}
	m.st.Gates = append(m.st.Gates, PendingGate{
		Vendor:    c.Vendor,
		RequestID: g.RequestID,
		ToolUseID: g.ToolUseID,
		Text:      text,
	})
	m.gateInputs[g.RequestID] = g.Input
}

// autoApproveRoutine recognizes the operations that make up an ordinary
// development loop: look at the tree, build it, test it, and turn a finished
// edit into reviewable work.
//
// It started as git and gh only, and the first real session with it carded the
// user THIRTY-FOUR times — every `go test`, `grep`, `ls` and `cat` between the
// commits. A gate that fires on everything is one people stop reading, and a
// waved-through card is worse than no card: it is the same keystroke with the
// user's attention spent. Widening what is routine is what keeps the remaining
// cards meaning something.
//
// Still deliberately conservative, and the ordering matters. Shell COMPOSITION
// is refused before anything is classified — no newlines, no `;`, `&`, `|`,
// backticks, redirection or `$(`. That guard is what makes an argv[0] allowlist
// sound at all: without it `ls; rm -rf ~` classifies as `ls`. Everything past it
// is a single command with literal arguments.
//
// A false negative costs one keystroke. A false positive spends a user's trust
// on a call they never saw, so anything ambiguous falls through to the card.
func autoApproveRoutine(g *runner.Gate) bool {
	if g == nil || g.Tool != "Bash" {
		return false
	}
	command, ok := g.Input["command"].(string)
	if !ok {
		return false
	}
	segments, ok := routineSegments(command)
	if !ok {
		return false
	}
	for _, seg := range segments {
		if !routineCommand(seg) {
			return false
		}
	}
	return true
}

// routineSegments splits a command on the only two composition operators a
// development loop actually needs, and refuses every other form outright.
//
// The first cut of this guard rejected any command containing & | < or >, which
// was correct and unusable: the shapes it turned away were `go build && echo OK`
// and `grep -rn foo internal | head`, whose every stage is a read. It carded the
// user for the punctuation rather than for the command, which is the failure
// this whole classifier exists to stop.
//
// PIPES AND && ARE SAFE HERE FOR ONE REASON, and it is not that pipes are
// harmless: it is that EVERY segment must independently classify as routine, and
// nothing in that allowlist writes. `cat x | rm` fails on the second segment.
// The moment a segment cannot be classified the whole command falls through to
// the card, so composition never becomes a way to smuggle a stage past the list.
//
// Everything else stays refused, each for a reason that survives the pipe
// argument:
//
//   - `;` and a LONE `&` sequence commands rather than connect them, and a
//     background job outlives the turn that started it.
//   - redirection (`<`, `>`) writes to a path the classifier never inspects,
//     which would turn `echo` — the most harmless entry in the list — into an
//     arbitrary file write. This is also why `2>&1` is refused: it is real
//     redirection, and picking that one spelling out by hand is how a parser
//     starts guessing.
//   - backticks and `$(` substitute a command's OUTPUT into the argv this
//     function is reading, so what gets classified is not what runs.
func routineSegments(command string) ([]string, bool) {
	if strings.ContainsAny(command, "\n\r;`<>") || strings.Contains(command, "$(") {
		return nil, false
	}
	// && is connection; a lone & is backgrounding. Take the pairs out first so
	// the survivor test below sees only the single ampersands.
	const andSentinel = "\x00"
	work := strings.ReplaceAll(command, "&&", andSentinel)
	if strings.Contains(work, "&") {
		return nil, false
	}
	var segments []string
	for _, part := range strings.FieldsFunc(work, func(r rune) bool {
		return r == '|' || r == '\x00'
	}) {
		if seg := strings.TrimSpace(part); seg != "" {
			segments = append(segments, seg)
		}
	}
	if len(segments) == 0 {
		return nil, false
	}
	// A separator with nothing on one side of it — `a || b`, `foo &&` — means the
	// text does not say what it appears to. Counting is enough to catch it and
	// does not require this function to become a shell parser.
	if strings.Count(work, "|")+strings.Count(work, andSentinel) != len(segments)-1 {
		return nil, false
	}
	return segments, true
}

func routineCommand(segment string) bool {
	args := strings.Fields(segment)
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "git":
		return safeGitArgs(args[1:])
	case "gh":
		return safeGHArgs(args[1:])
	case "go":
		return safeGoArgs(args[1:])
	case "find":
		return safeFindArgs(args[1:])
	}
	return readOnlyCommands[args[0]]
}

// readOnlyCommands look at the tree and report. None of them writes, and with
// redirection already refused above, none of them can be made to.
//
// Absent on purpose, each for its own reason rather than by oversight:
// `sed` and `awk` write in place and to arbitrary paths; `env` and `printenv`
// dump the environment, which is where credentials live and which the eighth
// amendment's hook exists to screen; `rm`, `mv`, `cp`, `mkdir` and `chmod`
// change the tree without being part of reading it; `curl`, `wget` and `ssh`
// reach outside the workspace, which is the boundary --write widens and not one
// it removes.
var readOnlyCommands = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"grep": true, "rg": true, "tree": true, "diff": true, "cmp": true,
	"pwd": true, "which": true, "file": true, "stat": true,
	"basename": true, "dirname": true, "echo": true, "date": true,
	"sort": true, "uniq": true, "du": true, "df": true,
}

// safeGoArgs allows the build loop and refuses the subcommands that fetch,
// install or execute something other than the package's own tests.
//
// `test` runs code, and that is not the objection it looks like: this seat can
// already write files, so a test binary grants nothing a plain edit did not.
// `run`, `install` and `get` are excluded anyway — they are not part of the loop
// this exists to unblock, and `get` reaches the network.
func safeGoArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "build", "test", "vet", "list", "version", "env", "fmt", "doc":
		return true
	case "mod":
		return len(args) > 1 && (args[1] == "tidy" || args[1] == "download" || args[1] == "verify")
	}
	return false
}

// safeFindArgs refuses the flags that turn a search into an execution or a
// deletion. find is otherwise a read, and it is the one read in this list that
// ships with a loaded gun in the same binary.
func safeFindArgs(args []string) bool {
	for _, a := range args {
		switch a {
		case "-exec", "-execdir", "-delete", "-ok", "-okdir", "-fprint", "-fprintf":
			return false
		}
	}
	return true
}

func safeGitArgs(args []string) bool {
	// Accept git -C <workspace> ..., but no other global switches whose effect
	// Council would have to interpret.
	if len(args) >= 3 && args[0] == "-C" {
		args = args[2:]
	}
	if len(args) == 0 {
		return false
	}
	sub, rest := args[0], args[1:]
	has := func(forbidden ...string) bool {
		for _, arg := range rest {
			for _, bad := range forbidden {
				if arg == bad || strings.HasPrefix(arg, bad+"=") {
					return true
				}
			}
		}
		return false
	}
	switch sub {
	case "status", "log", "diff", "show", "fetch":
		return true
	case "add":
		return !has("-p", "--patch")
	case "commit":
		return !has("--amend", "--fixup", "--squash")
	case "pull":
		return !has("--force")
	case "push":
		if has("-f", "--force", "--force-with-lease", "--delete", "--mirror", "--prune") {
			return false
		}
		for _, arg := range rest {
			if strings.HasPrefix(arg, "+") {
				return false
			}
		}
		return true
	case "switch":
		return !has("-C", "--force-create", "-f", "--force", "--discard-changes")
	case "checkout":
		// Checkout is only unambiguous when creating a new branch. `checkout x`
		// may mean a path and can overwrite working-tree content.
		return len(rest) >= 2 && (rest[0] == "-b" || rest[0] == "--branch")
	case "branch":
		return !has("-d", "-D", "--delete", "-m", "-M", "--move", "-f", "--force")
	default:
		return false
	}
}

func safeGHArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	switch args[0] + " " + args[1] {
	case "pr create", "pr view", "pr list", "pr status", "pr checks",
		"run list", "run view", "run watch":
		return true
	default:
		return false
	}
}

// decideGate answers the OLDEST pending request.
//
// Oldest first because that is the one the card is showing. Answering anything
// else would decide a call the user was not looking at, which on an approval
// gate is not a UI wrinkle — it is approving the wrong thing.
func (m *Model) decideGate(allow bool) {
	if len(m.st.Gates) == 0 {
		return
	}
	pending := m.st.Gates[0]
	m.st.Gates = m.st.Gates[1:]
	input := m.gateInputs[pending.RequestID]
	delete(m.gateInputs, pending.RequestID)

	c := m.column(pending.Vendor)
	if !allow && c != nil {
		// Recorded NOW, from the keystroke, rather than later from the vendor's
		// echo of it. The vendor reports a denial as an is_error tool_result
		// carrying this refusal text back, which read off the stream alone is
		// indistinguishable from a tool that broke.
		c.recordAct(runner.ActCall{
			ID:      pending.ToolUseID,
			Text:    pending.Text,
			Outcome: runner.ActDenied,
		}, m.redactWhole)
	}

	m.sendDecision(pending, allow, input)

	if len(m.st.Gates) == 0 {
		m.st.Notice = ""
	}
}

// denialText is what the model reads back when a call is refused.
//
// It names WHO refused. "Denied" alone reads to a model as an obstacle to route
// around, and the observed behaviour of a vendor told only that much is to try
// a slightly different spelling of the same call — which produces a second
// request for a user who has already said no once.
const denialText = "denied by the person running this council room. " +
	"Do not retry this call or a variation of it; say what you wanted to do and why."

func (m *Model) sendDecision(pending PendingGate, allow bool, input map[string]any) {
	p, ok := m.procs[pending.Vendor]
	if !ok {
		return
	}
	lines, err := p.wire.Decide(pending.RequestID, allow, denialText, input)
	if err != nil {
		return
	}
	// SendAside, not Send: an answer to a question the vendor asked mid-turn
	// belongs to the turn it is holding up, and starting a new one on it would
	// time the user's keystroke as a fresh wait.
	if err := p.sess.SendAside(lines); err != nil && m.column(pending.Vendor) != nil {
		m.column(pending.Vendor).Note = "the decision could not be delivered: " + err.Error()
	}
}

// dropGates discards a seat's pending requests.
//
// Called when the turn they belong to ends by any route — cancelled,
// interrupted, or the process dying. A card left on screen for a vendor that is
// no longer waiting would invite a keystroke that decides nothing, and the room
// would keep saying it was gating.
func (m *Model) dropGates(v model.VendorID) {
	kept := m.st.Gates[:0]
	for _, g := range m.st.Gates {
		if g.Vendor == v {
			delete(m.gateInputs, g.RequestID)
			continue
		}
		kept = append(kept, g)
	}
	m.st.Gates = kept
}
