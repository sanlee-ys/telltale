package council

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

const spinnerInterval = 100 * time.Millisecond

// Options configure a council run.
type Options struct {
	// Dir is the workspace turns are dispatched against. Empty means the
	// process working directory.
	Dir     string
	ASCII   bool
	NoTitle bool
	// Write starts the room in write posture. See State.Write.
	Write bool
	// Auto restores the pre-gate behaviour: a write-mode seat approves its own
	// tool calls instead of asking.
	//
	// A flag on --write rather than a mode of its own, and it only subtracts.
	// Gating is the default because the room the user opened is the one they
	// are watching; unattended is the exception and it has to be typed.
	Auto bool
	// BriefPath names a file of shared operating context handed to every
	// vendor on its first turn. Empty falls back to TELLTALE_COUNCIL_BRIEF.
	BriefPath string
	// Seats is who is in the room, from --vendor. The zero value collapses the
	// seats that cannot be driven and keeps the rest.
	Seats Seats
	// Resume is accepted and redundant: reattaching to the one saved room is
	// what a zero-argument launch does now. Kept so the muscle memory and the
	// scripts that grew around it keep working, and so an explicit --resume
	// against a machine with nothing saved can be told so instead of silently
	// opening fresh.
	Resume bool
	// Fresh opts OUT of the reattach: the room opens with no history, and the
	// saved room — if one exists — is named once before the first dispatch
	// replaces it.
	Fresh bool
	// TracePath names a file each turn's measured clock is appended to: one line
	// per seat per turn, split into spawn, wait and stream.
	//
	// A file and not a surface. The room already carries a header, a badge line,
	// a card and a footer per column, and three more numbers on every one of
	// them would cost every reader something to answer a question that is asked
	// on the days a turn is inexplicably slow.
	//
	// "Empty measures nothing" was true and is not any more. The clock always
	// ran; only the writing was conditional, so a turn nobody had predicted was
	// measured and then discarded — which is what made this flag the §9.17 case
	// it became. The room now HOLDS the last turns either way (trace.go), and
	// `/trace <file>` typed after a slow turn writes that turn. Empty still opens
	// nothing and changes no pixel; it just no longer throws the numbers away.
	TracePath string
}

// Model is the Bubble Tea model. It owns State plus the things Render must not
// see: the style set, the glyph set, and (from PR 2) the running child
// processes.
type Model struct {
	opts   Options
	st     State
	styles Styles
	glyphs Glyphs

	// events is shared by every vendor in a turn. Bounded, so a slow consumer
	// stalls the vendors rather than losing their output (see dispatch.go).
	events chan runner.Event

	// turn is the dispatch in flight, or nil when the room is idle.
	turn *turnState
	// cancelling distinguishes "the user stopped this" from "the vendor
	// failed", which are different words on a column card.
	cancelling bool

	// procs holds the long-lived vendor processes, one per seat that supports
	// being kept alive. Empty until the first dispatch: council never starts a
	// vendor to see whether it answers.
	procs map[model.VendorID]*seatProc
	// roomCtx bounds those processes and is cancelled only by teardown.
	//
	// Deliberately NOT a turn's context. The whole value of a persistent seat is
	// that cancelling one turn does not cost the next one a session init, and
	// hanging the process off the turn would have quietly undone that the first
	// time anyone pressed ctrl+c.
	roomCtx    context.Context
	roomCancel context.CancelFunc
	// interrupts numbers the control messages sent to vendors, so each one
	// carries an id that can be recognised coming back.
	interrupts int

	// gateInputs holds each pending request's tool arguments, keyed by request
	// id, because an approval has to echo them back.
	//
	// On Model and NOT on State, for the same reason the brief is: it is the
	// entire argument blob — for a Write, the whole file content — and the
	// renderer has no business being able to reach it. Only the one-line
	// summary crosses onto State.
	gateInputs map[string]map[string]any

	// sessions holds each vendor's own session id, which is what makes a later
	// turn a resume rather than a transcript re-send.
	sessions map[model.VendorID]string
	// resumeIDs are the ids restored from a saved room that a PERSISTENT seat
	// has not spent yet.
	//
	// Separate from sessions, and only for the persistent path. A spawn-per-turn
	// vendor consumes its id through specFor on every turn, which already falls
	// back to a first turn when the vendor refuses it. A persistent seat spends
	// its id once, at process launch, and must not carry it into the replacement
	// process — hence a map that is emptied as it is read rather than a flag.
	resumeIDs map[model.VendorID]string
	// unproven names seats holding a RESTORED session id that has not yet
	// survived a turn.
	//
	// It exists because "the vendor refuses a stale id" is not a thing any of
	// these CLIs reports as such. Every adapter's NextTurn returns ErrNoResume
	// only for an EMPTY id; a well-formed id whose conversation has aged out
	// builds a perfectly valid invocation, and the failure arrives later as a
	// dead process. Nothing in the old code deleted the id, so a spawn-per-turn
	// seat would rebuild the same doomed `resume <dead-id>` invocation on every
	// turn for the life of the room — which is exactly the wedge the persistent
	// seat's one-shot rule was written to avoid, on the three seats that never
	// got it.
	//
	// So a restored id is on probation until a turn comes back clean. It is
	// dropped the first time one fails, and after that this seat is an ordinary
	// unrestored seat: ids EARNED in this process are never touched by any of
	// this, because a transient failure mid-conversation must not throw away a
	// working thread.
	unproven map[model.VendorID]bool
	// threadLost names seats whose reattach was refused during the current turn,
	// so a later event about the same failure cannot overwrite the one sentence
	// that tells the user what happens to their next brief. Cleared per turn at
	// dispatch, because it is a fact about one turn and not about the seat.
	threadLost map[model.VendorID]bool
	// forkWatch names, per seat, the conversation id THIS turn's dispatch asked
	// the vendor to resume — and it is populated only for a seat whose vendor has
	// been MEASURED to answer a resume it cannot honour by silently opening a new
	// conversation (vendors.SilentResumeFork; today that is agy alone).
	//
	// It is the requested half of §9.43's comparison. The returned half arrives
	// on the vendor's own session event, and adoptSession is where the two meet.
	// Keeping the vendor gate HERE rather than at the comparison is what makes
	// the honesty rule structural: a seat that never enters this map cannot
	// raise a lost-thread card, so no vendor is accused of losing a thread on
	// evidence nobody captured.
	//
	// Cleared per turn at dispatch, beside threadLost, and again the moment the
	// card fires: it is a fact about one dispatch, and a stale entry would let an
	// ordinary new conversation on a later turn be compared against an id nobody
	// asked for on it.
	forkWatch map[model.VendorID]string
	// failure is what this turn's failure said about the seat's CONVERSATION, as
	// classified where the evidence was — the runner's stderr classifier and the
	// adapters' own result parsers — rather than re-derived here from a rendered
	// note. Cleared per turn at dispatch, beside threadLost, for the same reason:
	// it is a fact about one turn.
	//
	// On Model and not on State. It is a decision input, never rendered, and
	// State is what the renderer can reach; a classification the view could read
	// is one a card could start quoting, which is how a conservative internal
	// judgement turns into a claim on screen.
	failure map[model.VendorID]runner.FailureClass
	// redactors are per vendor because each carries a partial-word buffer
	// across the chunks of one stream.
	redactors map[model.VendorID]*Redactor

	// saveErr is the last state-file write's outcome, kept so the save on the
	// way out can be reported after the TUI is gone.
	saveErr error

	// teardownMu and teardownDone make teardown a ONE-SHOT, and they are the
	// only concurrency control on this type.
	//
	// Everything else on Model is touched from the Bubble Tea update loop and
	// from nowhere else, which is what lets the rest of this file be written
	// without locks. teardown broke that the moment a signal could reach it
	// (signals_unix.go): a `kill` landing while the user presses q gives two
	// goroutines the same act, and the act ranges over m.procs while deleting
	// from it — two of those at once is a concurrent map write, which is a
	// runtime panic rather than a bad frame.
	//
	// A one-shot rather than a re-entrant lock because teardown has no second
	// meaning. It is what the room does on the way out, the room goes out once,
	// and every step of it — kill, cleanup, cancel — is already idempotent on
	// its own. The flag is what makes the WHOLE of it idempotent, so the second
	// caller returns instead of re-walking maps the first one emptied.
	teardownMu   sync.Mutex
	teardownDone bool

	// brief is the shared operating context. Held on Model, never on State:
	// its content is the user's private file and the renderer has no business
	// being able to reach it.
	brief Brief

	// hooks is council's own PreToolUse gate hook, written to a settings file
	// of its own so the gated seat can be pointed at it. Named `hooks` from
	// when it held the operator's; what it holds now is the room's own gate.
	hooks GateHook

	// flowChain holds the active workflow DAG sequence if an orchestrated flow was dispatched.
	flowChain *FlowChain
	// flowDraft is the draft string that produced flowChain (reuse after write gate).
	flowDraft string
	// trace is the room's turn-clock sink: a bounded ring of recent records, plus
	// a file once /trace or --trace opens one. Never nil while the room runs.
	//
	// A pointer, and the runner writes to it from the goroutines reading each
	// vendor's stdout — so nothing about it may be read during Render or touched
	// from Update without going through its own mutex. See trace.go.
	trace *traceSink
	// clearPending names the seat awaiting y/n before its thread is dropped, and
	// is empty when nothing is pending.
	//
	// Confirmed rather than immediate, which is the one place this feature spends
	// a keystroke on purpose. The complaint that produced it (§9.17) is about
	// friction — needing to remember a flag and quit the room — and one y is not
	// that. What it buys is the difference between a stray press in view mode and
	// a multi-turn conversation ending: the drop is irreversible, the session id
	// is the only handle on that thread, and no vendor here offers a way back to
	// one it has been told to forget.
	clearPending model.VendorID
	// undoPending names the seat awaiting y/n before its race attempt is reset
	// to base (§9.37, amended 2026-08-09), empty when nothing is pending.
	//
	// Confirmed for clearPending's reason, and the stake is the same shape: an
	// undo throws away a worktree's whole state, and while the arena BRANCH
	// commit usually survives in the reflog, the room must not lean on a
	// recovery path it never renders — a stray `u` in view mode has to cost a
	// y before it costs an attempt.
	undoPending model.VendorID
	// giveUpPending names the racing seat awaiting y/n before its racer is
	// killed mid-race (`x`, §9.37 amended 2026-08-09), empty when nothing is.
	//
	// Confirmed for clearPending's reason, at clearPending's stake: y kills a
	// process and retires a column, and no keystroke can restart the attempt —
	// a stray `x` in view mode has to cost a y before it costs a racer. The
	// measurement that forced the key is the second live /arena (2026-08-09,
	// Windows box): three seats landed, the fourth streamed for 26m40s with
	// the live stat honestly reading "no changes yet", and the operator sat
	// ~20 minutes after the race was decided because ctrl+c — the only exit —
	// cancels every seat at once. The room displayed the truth and offered no
	// per-seat act on it.
	giveUpPending model.VendorID
	// adoptPending names the racer awaiting y/n before its arena branch is
	// merged into the room's repo (/adopt, lifecycle.go), empty when nothing is.
	//
	// Confirmed for /write's reason, not `c`'s: nothing is destroyed by an
	// adopt — the merge is revertible with git's own tools, and the branch it
	// cuts with `git branch -D` — but it MUTATES THE USER'S REPO, which no
	// other act in this room does without a y. The question on the card names
	// the exact git commands, because "may I?" is only answerable when the
	// "what" is on screen.
	adoptPending model.VendorID
	// adoptOnto is the branch the pending adoption will cut and merge onto —
	// resolved when the card arms (freeAdoptBranch), not when y is pressed.
	//
	// It rides beside adoptPending rather than being recomputed at y for the
	// card's own contract: the question on screen names this branch, and a y
	// that cut a different one — the collision suffix moves the name — would
	// make the card a description of something else.
	adoptOnto string
	// lastRace is the most recent /arena race's receipt: workspace, turn, base,
	// and each racer's kept worktree (lifecycle.go). Nil until a race runs.
	// Held on Model rather than State because Render never reads it — the
	// on-screen arena block is Column.Arena, a per-turn fact; this outlives the
	// turn the way the worktrees themselves do (§9.37: kept until deleted).
	lastRace *arenaRace
	// arenaPrep is the race whose worktrees are being cut right now, off the
	// render loop (arenasetup.go, §9.37 amended 2026-08-17), and nil the rest of
	// the time.
	//
	// It sits beside turn rather than inside it because it is what stands
	// BEFORE a turn: no seat has spawned, no column has moved, and the dispatch
	// it is preparing does not exist yet. Every guard that asks "is a turn in
	// flight" would answer no while this is set, which is correct for all of
	// them except dispatch itself — a second race started here would cut names
	// from the same ref scan as the first.
	arenaPrep *arenaPrep
	// arenaPrepN numbers preps, so a message from a setup the room has already
	// stopped can be dropped by comparison. Never reset: an id has to be unique
	// over the room's whole life, not over the current setup.
	arenaPrepN int
	// writePending is true while /write awaits y/n before the room's posture is
	// loosened, and is the only one of the three room controls that asks.
	//
	// It is NOT flowWritePending under another name. That one gates a chain the
	// user already started, hop by hop; this one gates the room itself, and the
	// two can never be pending together because a flow hop only exists while a
	// turn is being assembled and this refuses to arm mid-turn at all.
	writePending bool
	// flowWritePending is true while a write hop awaits y/n before any vendor spawn.
	flowWritePending bool
	// flowWriteArmed is set by y so the next dispatch may Start the write hop.
	flowWriteArmed bool
	// flowReadHop marks the dispatch in progress as a /flow hop with NO declared
	// write target, which forces read posture regardless of the room's. It is
	// set per hop by dispatch and cleared on any non-flow dispatch, so it can
	// never outlive the step that earned it.
	flowReadHop bool
	// flowCarry is the fenced artifact from the immediately preceding hop. It is
	// consumed when the next hop starts; older artifacts never accumulate in a
	// downstream prompt.
	flowCarry string
	// flowAdvancePending asks Update to dispatch the next hop after the current
	// turn has fully retired. Dispatching earlier would trip the in-flight guard.
	flowAdvancePending bool
}

// New builds the model. Nothing renders until the first WindowSizeMsg arrives:
// one blank frame beats one frame of wrong layout.
//
// The brief is loaded by Run before this, so a bad path fails before the
// alternate screen is entered rather than as an unreadable error behind a TUI.
func New(opts Options) *Model {
	return newWithBrief(opts, Brief{}, GateHook{}, Reattachment{})
}

func newWithBrief(opts Options, b Brief, hs GateHook, re Reattachment) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		opts:       opts,
		st:         stateWith(opts, hs.Wired()),
		styles:     NewStyles(true), // assume dark until the terminal answers
		glyphs:     GlyphsFor(opts.ASCII),
		events:     make(chan runner.Event, eventBuffer),
		sessions:   map[model.VendorID]string{},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		failure:    map[model.VendorID]runner.FailureClass{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      map[model.VendorID]*seatProc{},
		gateInputs: map[string]map[string]any{},
		roomCtx:    ctx,
		roomCancel: cancel,
		brief:      b,
		hooks:      hs,
		// Never nil, so /trace has something to answer with in a model a test
		// built directly. Run replaces it with the sink it installed into the
		// runner, because there must be exactly one ring and the runner has to be
		// writing into the same one /trace reads.
		trace: newTraceSink(),
	}
	m.st.Briefed = b.Loaded()
	m.reattach(re)
	// Pickup-doc drift is a room-open fact, same class as a reattach notice:
	// said once when the room starts, then displaced by whatever the user does
	// next. Joined rather than replaced, so a reattach and a stale STATE.md
	// both land — either alone would hide the other (§4a.1 applied to notices).
	m.st.Notice = joinNotice(m.st.Notice, stateMDStaleNotice(m.st.Workspace))
	return m
}

// reattach restores a saved room onto a freshly built model.
//
// The ids land on Model and never on State — the same boundary the brief keeps
// — and only what the room can honestly SAY about them crosses over: a turn
// number, a timestamp, and a per-seat flag.
//
// The saved posture is read and NOT applied, which is the one decision in here
// worth arguing with. Restoring --write from a file would mean a room that can
// edit a tree because of something on disk rather than something the user
// typed, and the whole of ADR-008's third amendment is that a write-capable
// room announces itself in the command and in the header for the entire
// session. A flag that can arrive from a file is not a flag anyone typed.
func (m *Model) reattach(re Reattachment) {
	if re.Offered {
		// A usable room is here and --fresh declined it. Said once, plainly, and
		// then forgotten: the alternative is that the first dispatch overwrites
		// four vendors' session ids with nothing on screen having mentioned they
		// were there.
		saved := "a saved room"
		if re.Adopted {
			saved = "a saved room (old per-workspace format)"
		}
		m.st.Notice = saved + " from " + age(time.Since(re.Room.SavedAt)) +
			" (turn " + itoa(re.Room.Turn) +
			") exists — rerunning without --fresh reattaches to it; dispatching here replaces it"
		return
	}
	if re.Ignored != "" {
		// A state file that exists and cannot be used. The room opens anyway —
		// it is perfectly usable unreattached — but silence here would let a
		// user carry on believing they are continuing a conversation that is not
		// there. The next completed turn overwrites the bad file.
		m.st.Notice = "the saved room was not restored: " + re.Ignored
		return
	}
	if !re.Active() {
		return
	}

	seats := 0
	for v, id := range re.Room.Sessions {
		if id == "" {
			continue
		}
		m.sessions[v] = id
		m.resumeIDs[v] = id
		// On probation until it survives a turn. See Model.unproven — without
		// this, a thread the vendor no longer has would be retried on every turn
		// of the session instead of once.
		m.unproven[v] = true
		if c := m.column(v); c != nil && c.Avail == AvailInstalled {
			// Only a SEATED column is marked restored. An id for a vendor that
			// is not installed on this machine is dead weight rather than a
			// thread, and a card claiming a restored conversation above an
			// "is not seated" card would be two contradictory statements in one
			// column.
			c.Restored = true
			seats++
		}
	}

	m.st.Turn = re.Room.Turn
	m.st.Reattached = Reattach{
		Turn:    re.Room.Turn,
		SavedAt: re.Room.SavedAt,
	}
	// Says WHERE the state came from, not merely that there was some. A room
	// that reports a turn count it did not earn owes the user the file it read
	// it out of.
	//
	// The seat count is here rather than on State because Render has no use for
	// it — each column's own card already says whether ITS thread came back, and
	// a field the renderer never reads is a field that can drift without
	// anything noticing.
	source := abbreviate(re.Path, m.st.Home)
	if re.Adopted {
		// State restored from a file the user has never heard of is state the
		// room owes them the source of: the pre-cockpit format kept one room per
		// workspace, and this is the newest of those, adopted once.
		source += " (adopted from the old per-workspace format)"
	}
	m.st.Notice = "reattached from " + source +
		" — turn " + itoa(re.Room.Turn) + " was the last"
	if !re.Room.SavedAt.IsZero() {
		// Age is the room fact columns used to repeat; Notice is the one place
		// it lives after the hoist. Frozen at attach — a footer that counted
		// up every frame would be a moving cell §7.1 does not budget for.
		m.st.Notice += ", saved " + age(time.Since(re.Room.SavedAt))
	}
	m.st.Notice += ", " + itoa(seats) + "/" + itoa(m.st.Seated()) + " seats restored"
	switch {
	case re.WorkspaceGone != "":
		// The saved directory is not there any more — renamed, unmounted, or a
		// git worktree someone removed. Its OWN sentence, because the one below
		// reads identically to a --cd override and this is not one: the user
		// changed nothing, and what they are looking at is a room that lost the
		// place it was working in.
		//
		// Said loudly for a reason beyond accuracy. The room opens in the
		// current directory and the next completed turn writes THAT over the
		// saved workspace, so this notice is the only moment the old path is
		// ever shown again — the same argument Reattachment.Offered is built on.
		m.st.Notice += " (the room was in " + abbreviate(re.WorkspaceGone, m.st.Home) +
			", which no longer exists — it opened in " +
			abbreviate(m.st.Workspace, m.st.Home) + " instead)"
	case !sameDir(re.Room.Workspace, m.st.Workspace):
		// The room reopened somewhere other than where it was saved, and the
		// saved directory is still there: a --cd override. The seats'
		// conversations came back either way; only where they act moved, and
		// the mechanics are the same as an in-room /cd.
		m.st.Notice += " (the room was in " + abbreviate(re.Room.Workspace, m.st.Home) +
			"; it is now in " + abbreviate(m.st.Workspace, m.st.Home) + ")"
	}
	// LIVE on both sides, matching the writer exactly (§9.32). This is the saved
	// posture's only consumer, which is why the two moved in one change: a reader
	// asking `m.opts.Auto` about a room `a` can move would compare a description
	// of the live room against a description of the flags, and report a change to
	// a user who made none.
	//
	// Read at reattach time, where the room is still exactly its defaults plus
	// its flags — nothing has been typed into it yet — so this is also the point
	// at which "posture is never restored" is observable: whatever the file says,
	// m.st.Write and m.st.Asking() here came from Options and nowhere else.
	if now := savedPosture(m.st.Write, m.st.Asking()); re.Room.Posture != now {
		// Stated rather than applied. The saved room ran under a different
		// posture, and a user who reattaches a write room without retyping
		// --write should learn that from the room instead of from a vendor
		// refusing to edit a file.
		m.st.Notice += " (it ran " + re.Room.Posture + "; this room is " + now + ")"
	}
	if re.Room.BriefPath != m.brief.Path {
		// Same treatment as posture, and the same reason the path is stored at
		// all: reported, never acted on. Loading the saved path would read a
		// private file this invocation did not name, which is the one thing
		// --brief is built to keep deliberate. Only the PATHS are compared —
		// a file edited since the room was saved is invisible here, and saying
		// otherwise would need the content this file refuses to hold.
		was, now := re.Room.BriefPath, m.brief.Path
		if was == "" {
			was = "no brief"
		}
		if now == "" {
			now = "no brief"
		}
		m.st.Notice += " (it ran with " + abbreviate(was, m.st.Home) +
			"; this room has " + abbreviate(now, m.st.Home) + ")"
	}
}

// abbreviate shortens a home-relative path for display, matching the header's
// treatment of the workspace.
//
// The prefix match is checked at a SEPARATOR boundary, unlike the header's: a
// home of /home/dev against /home/developer/x is not a home-relative path, and
// the naive form renders it as the nonsense "~eloper/x".
func abbreviate(p, home string) string {
	if home == "" || !strings.HasPrefix(p, home) {
		return p
	}
	rest := p[len(home):]
	if rest != "" && rest[0] != '/' && rest[0] != '\\' {
		return p
	}
	return "~" + filepath.ToSlash(rest)
}

func stateWith(opts Options, hooked bool) State {
	st := NewState()
	st.ASCII = opts.ASCII
	st.Write = opts.Write
	st.GateOff = opts.Auto
	st.Seats = opts.Seats
	st.Now = time.Now()

	// Resolved once, here, so the render path never reads the environment.
	st.Workspace = resolveWorkspace(opts.Dir)
	if home, err := os.UserHomeDir(); err == nil {
		st.Home = home
	}

	windows := runtime.GOOS == "windows"
	for _, info := range Detect() {
		st.Columns = append(st.Columns, Column{
			Vendor:  info.Vendor,
			Label:   info.Label,
			Avail:   info.Avail,
			Binary:  info.Binary,
			Note:    info.Note,
			Sandbox: postureClaim(info.Vendor, windows, opts.Write, st.Asking(), hooked),
			Gran:    granularityFor(info.Vendor),
			Phase:   PhaseIdle,
			Follow:  true,
		})
	}
	// Focus lands on a column that is actually drawn. Left at 0 it would sit on
	// a collapsed seat on any machine whose first vendor is not installed, and
	// tab would appear to do nothing until it had wrapped all the way round.
	if vis := st.VisibleColumns(); len(vis) > 0 {
		st.Focus = vis[0]
	}
	return st
}

// resolveWorkspace turns --cd into the absolute directory turns are dispatched
// against.
//
// Extracted from stateWith because Run needs the SAME answer before the model
// exists: the workspace is the key a saved room is filed under, so a --resume
// that resolved the path even slightly differently would look in the wrong
// place and report no saved room for a room that is right there.
func resolveWorkspace(dir string) string {
	if dir == "" {
		if wd, err := os.Getwd(); err == nil {
			dir = wd
		}
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
}

// openWorkspace decides which directory a launching room is pointed at: --cd if
// typed, else where the saved room was, else here.
//
// A FUNCTION rather than the block it used to be inside Run, and that is the
// finding of 2026-08-16 as much as the fallback below it. The decision lived
// inline in Run, Run enters the alternate screen, and so no test could reach it:
// when a live room reopened showing `~` instead of the repo it was saved in,
// nothing in the suite could say whether the restore had dropped the field or
// the file had honestly recorded a home-directory room. An untestable decision
// is one whose failures are all reported by the operator.
//
// Returns the directory, and separately the saved path it REFUSED, so the caller
// can say so. Both non-cwd sources are verified to be directories before the
// room is pointed at them, and the two failures are answered differently:
//
//   - A typed --cd that is not a directory is the LoadBrief discipline — a plain
//     error before the alternate screen, because the user named this path and a
//     silent substitution would act somewhere they did not ask for.
//   - A SAVED directory that is gone (a renamed repo, a removed git worktree, an
//     unmounted drive) is not the user being wrong, so it opens anyway in the
//     current directory. It is named rather than swallowed: the room falls back
//     AND the next completed turn writes that fallback over the saved workspace,
//     so a silent one costs the user the only record of where the room was, in
//     the same way and for the same reason --fresh's Offered notice exists.
func openWorkspace(opts Options, re Reattachment) (ws, gone string, err error) {
	switch {
	case opts.Dir != "":
		ws = resolveWorkspace(opts.Dir)
		if !isDir(ws) {
			return "", "", errors.New("--cd " + opts.Dir + ": not a directory")
		}
		return ws, "", nil
	case re.Active() && !re.Offered:
		// The restored room's own directory. Resolved rather than trusted as
		// written: the file holds whatever the saving room had, and every other
		// consumer of a workspace in this package gets an absolute path.
		ws = resolveWorkspace(re.Room.Workspace)
		if !isDir(ws) {
			return resolveWorkspace(""), re.Room.Workspace, nil
		}
		return ws, "", nil
	default:
		return resolveWorkspace(""), "", nil
	}
}

// isDir reports whether a path is a directory that exists right now.
//
// The error is deliberately collapsed into false. A path council cannot stat is
// a path it cannot dispatch four agents against, and telling a permission error
// apart from a missing directory would buy the room a distinction it has no
// different answer for.
func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

type spinMsg time.Time

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		// RequestBackgroundColor is a Msg in v2, not a Cmd: it is a request the
		// runtime turns into an OSC query, so it has to be lifted into a Cmd
		// rather than handed to Batch directly.
		func() tea.Msg { return tea.RequestBackgroundColor() },
		spin(),
		// The relay is read once at room open, so the first draft is composed
		// against a reading rather than against nothing (§9.21's 2026-08-17
		// amendment). A Cmd because it touches the filesystem, and batched
		// rather than sequenced because it is independent of everything else
		// here.
		readQuotaCmd(),
	)
}

func spin() tea.Cmd {
	return tea.Tick(spinnerInterval, func(t time.Time) tea.Msg { return spinMsg(t) })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.st.Width, m.st.Height = msg.Width, msg.Height
		return m, nil

	case tea.BackgroundColorMsg:
		m.styles = NewStyles(msg.IsDark())
		return m, nil

	case tea.KeyPressMsg:
		return m.key(msg)

	case tea.PasteMsg:
		// Bracketed paste, delivered whole. This case is what makes a paste
		// exist at all: before it, PasteMsg fell through this switch and the
		// clipboard's offer was silently discarded (see paste.go). PasteStartMsg
		// and PasteEndMsg still fall through unhandled, deliberately — the
		// content between them arrives inside this one message, so the markers
		// carry nothing the room needs.
		return m.paste(msg)

	case eventBatchMsg:
		m.applyEvents(msg.events)
		if m.turn == nil {
			if m.flowAdvancePending {
				m.flowAdvancePending = false
				return m, m.dispatch()
			}
			// The turn is over. Stop waiting on the channel: re-arming would
			// park a goroutine on a channel nothing will write to until the
			// next dispatch.
			//
			// It is also the moment the relay is worth re-reading: a turn just
			// consumed some of every addressed account, and the next draft is
			// composed against whatever this returns. The read lands late by
			// construction — council does not write the relay, so a vendor's
			// new figure appears only after its own statusline renders again —
			// which is what the age suffix on the reading is for.
			return m, readQuotaCmd()
		}
		// A racing seat's stream activity may have armed a live stat read
		// (arenalive.go); launch what is due alongside the next wait. Batched
		// rather than sequenced — the read is independent of the event
		// channel, and waitEvents blocks.
		if ref := m.dueArenaRefreshes(); ref != nil {
			return m, tea.Batch(m.waitEvents(), ref)
		}
		return m, m.waitEvents()

	case arenaSetupMsg:
		// One step of a race's worktree setup beginning, or the whole setup
		// landing. Everything it can do next — read the following step, spawn
		// the turn it was preparing, or report why it stopped — is decided by
		// the handler and returned as the next command (arenasetup.go).
		return m, m.applyArenaSetup(msg)

	case arenaStatMsg:
		// One interim read landing (or being dropped as stale — the drop
		// rules live with the handler). No follow-up command: the next read
		// is launched by the tick or the next event batch, through the same
		// due check everything else goes through.
		m.applyArenaStat(msg)
		return m, nil

	case quotaMsg:
		// One read of the quota relay landing. No follow-up command: the next
		// read is launched when a turn ends, because the file only changes when
		// the user's own statusline fires and a poll would re-read unmoved
		// bytes on every frame (quota.go).
		m.applyQuota(msg)
		return m, nil

	case spinMsg:
		// Now is stamped here, on the tick, so Render never reads a clock and
		// the elapsed counters advance on the same schedule as the spinner.
		m.st.Now = time.Time(msg)
		// The spinner only advances while a column is genuinely working — or
		// while a race's worktrees are being cut, which is the same claim about
		// a different worker: git is running off the loop and the moving cell is
		// how the room says so. A motionless room is the honest render of a room
		// where nothing is happening, and that was the exact lie a frozen setup
		// used to tell. §7.1's budget of one moving cell is untouched: no column
		// can be spinning during a setup, because nothing has been dispatched.
		if m.st.Busy() || m.st.ArenaSetup != "" {
			m.st.Spinner++
		}
		// The tick is the throttle's second leg: arming happens on activity,
		// but a seat armed mid-interval has to be read when the interval
		// expires even if the vendor has gone quiet since — a burst of writes
		// followed by silence is exactly the seat whose stat is most behind.
		if ref := m.dueArenaRefreshes(); ref != nil {
			return m, tea.Batch(spin(), ref)
		}
		return m, spin()
	}
	return m, nil
}

// key routes by mode. Compose mode is checked FIRST and swallows everything
// that carries text, because in it `q` is the letter q — the footer says which
// mode is active on every frame precisely so this is never a surprise.
//
// A pending gate outranks both, and it is the same rule rather than an
// exception to it: something is blocked on a keystroke, the footer says which
// keystroke, and the keymap is read from the same queue the footer is.
func (m *Model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// ctrl+c during a race's worktree setup stops the SETUP, in every mode, and
	// it is checked before anything else for the reason the setup moved off the
	// loop at all: this keystroke is the operator's only way to end a git
	// command that will not finish, and it was the one the frozen room ate.
	// Nothing below can answer it — no turn exists to cancel, so view mode would
	// quit the room and compose mode would quit it faster. Once stopped, a
	// second ctrl+c means what it always means.
	if m.arenaPrep != nil && msg.String() == "ctrl+c" {
		m.stopArenaSetup()
		return m, nil
	}
	if m.st.Gating() {
		return m.gateKey(msg)
	}
	if m.clearPending != "" {
		return m.clearGateKey(msg)
	}
	if m.undoPending != "" {
		return m.undoGateKey(msg)
	}
	if m.giveUpPending != "" {
		return m.giveUpGateKey(msg)
	}
	if m.adoptPending != "" {
		return m.adoptGateKey(msg)
	}
	if m.writePending {
		return m.writeGateKey(msg)
	}
	if m.flowWritePending {
		return m.flowWriteGateKey(msg)
	}
	if m.st.Mode == ModeComposing {
		return m.composeKey(msg)
	}
	return m.viewKey(msg)
}

// clearGateKey answers the confirmation armed by `c`.
//
// Anything that is not y or n cancels rather than falling through to viewKey.
// The flow gate falls through and this one does not, because the two are asking
// different questions: that one blocks a chain the user already started, so
// reading the columns first is part of deciding, while this one interrupts
// nothing and the safe answer to a key nobody meant to press is to put the
// thread back out of reach.
func (m *Model) clearGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.clearPending
	m.clearPending = ""
	switch msg.String() {
	case "y":
		m.clearSeat(v)
	case "n":
		m.st.Notice = "kept — nothing was cleared"
	default:
		m.st.Notice = "clear cancelled — y confirms, n declines"
	}
	return m, nil
}

// adoptGateKey answers the confirmation armed by /adopt.
//
// Anything that is not y or n cancels, matching clearGateKey and writeGateKey
// rather than the flow gate, for the same reason those two give: this
// interrupts nothing, the question is one sentence on screen, and the safe
// reading of a key nobody meant to press is to merge nothing. The commands y
// runs — and the branch it cuts — were named on the card when adoptCommand
// armed it (lifecycle.go).
func (m *Model) adoptGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.adoptPending
	onto := m.adoptOnto
	m.adoptPending, m.adoptOnto = "", ""
	switch msg.String() {
	case "y":
		m.st.Notice = m.adoptSeat(v, onto)
	case "n":
		m.st.Notice = "kept — nothing was merged"
	default:
		m.st.Notice = "adopt cancelled — y confirms, n declines"
	}
	return m, nil
}

// stopAsking turns the approval card off for the rest of the room, and clears
// every card already queued behind the one being answered.
//
// The queue is drained rather than left standing, and that is the whole
// difference between this and a setting. The cards behind the current one are
// the same question asked again; leaving them would make `a` mean "stop asking
// after these four", which is not what anybody presses it for.
//
// Approved, not discarded. A pending gate is a vendor STOPPED mid-call
// (queueGate's own comment: nothing here may quietly drop a request), so
// dropping the queue would leave columns waiting forever with no card left to
// explain why.
func (m *Model) stopAsking() {
	m.st.GateOff = true
	n := len(m.st.Gates)
	for len(m.st.Gates) > 0 {
		m.decideGate(true)
	}
	m.applyPosture(m.st.Write)
	m.st.Notice = "approved " + itoa(n) + " " + plural(n, "call") +
		" — nothing will ask again this session · a starts asking"
}

// toggleAsking is the way back, and the reason `a` is one key rather than a
// one-way door.
//
// In view mode rather than as a room command: `a` already means this on the
// card, and teaching one letter in two places beats spending a word out of the
// composer on the same idea (roomcmd.go's vocabulary rule). A room that could
// only ever stop asking would be the §9.17 defect rebuilt — a decision you can
// make once, in one direction, and then have to relaunch to undo.
func (m *Model) toggleAsking() {
	m.st.GateOff = !m.st.GateOff
	m.applyPosture(m.st.Write)
	if m.st.Asking() {
		if !m.st.Write {
			// Honest about the fact that nothing will ask, because in a
			// read-only room there is nothing to ask ABOUT. Reporting "the seat
			// asks again" here would promise a card that cannot arrive.
			m.st.Notice = "the seat will ask again once the room writes — /write lets it"
			return
		}
		m.st.Notice = "claude asks before each change again — y approves, n denies, a stops asking"
		return
	}
	m.st.Notice = "nothing will ask before it acts — a starts asking again"
}

// writeGateKey answers the confirmation armed by /write.
//
// Anything that is not y or n cancels, matching clearGateKey rather than the
// flow gate, and for clearGateKey's reason: this interrupts nothing, so the safe
// reading of a key nobody meant to press is to leave the room read-only. The
// flow gate falls through to viewKey because scrolling the columns is part of
// deciding there; here there is nothing to read — the question is one sentence
// on screen and the answer does not depend on what any seat said.
//
// Cancelling and declining are different sentences on purpose. "n" is a decision
// and says only that the room was kept; the default branch says what the two
// keys are, because a user who landed here by accident is the one who does not
// know what is being asked.
func (m *Model) writeGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.writePending = false
	switch msg.String() {
	case "y":
		m.applyPosture(true)
		m.st.Notice = "the room writes — seats move on their next turn"
	case "n":
		m.st.Notice = "kept read-only"
	default:
		m.st.Notice = "cancelled — the room is still read-only · y confirms, n declines"
	}
	return m, nil
}

// askClearSeat arms the confirmation for the focused seat.
//
// The refusals here are all the same refusal: a thread cannot be dropped out
// from under something that is using it. A turn in flight is `/cd`'s rule
// (roomcmd.go) for the same reason — the seats were dispatched against the state
// this would change — and a seat with nothing to clear is told so rather than
// being handed a card whose y does nothing, which would teach that the key is
// unreliable rather than that the seat is empty.
func (m *Model) askClearSeat() {
	if m.turn != nil {
		m.st.Notice = "a turn is in flight — c clears a seat between turns"
		return
	}
	c := m.focused()
	if c == nil {
		m.st.Notice = "no seat is focused"
		return
	}
	if !m.seatHasThread(c.Vendor) {
		m.st.Notice = c.Label + " has no thread to clear — its next brief already opens a new session"
		return
	}
	m.clearPending = c.Vendor
	m.st.Notice = "clear " + c.Label + "'s thread? y confirms — its next brief starts a new session · n keeps it"
}

// seatHasThread reports whether this seat has anything a clear would drop.
//
// A live process counts even with no saved id yet: the persistent seat holds the
// conversation in the process itself (§9.8), so a first turn that has not
// reported a session id is still a thread on screen.
func (m *Model) seatHasThread(v model.VendorID) bool {
	if m.sessions[v] != "" || m.resumeIDs[v] != "" {
		return true
	}
	_, alive := m.procs[v]
	return alive
}

// clearSeat drops one seat's thread and leaves the other seats untouched.
//
// This is the drop dispatch.go already performs when a restored id fails its
// first turn, reached deliberately instead of by accident — the same three maps,
// for the same reason: an id left in any one of them gets rebuilt into the next
// invocation and the seat carries on the conversation the user just ended.
//
// The persistent seat needs its process killed as well, and the ORDER is
// load-bearing. seatProcess re-arms resumeIDs from m.sessions when it replaces a
// process, which is what carries a thread across a /cd — so a kill before the
// deletes would hand the id straight back and the next brief would resume the
// conversation this function exists to end.
func (m *Model) clearSeat(v model.VendorID) {
	delete(m.sessions, v)
	delete(m.resumeIDs, v)
	delete(m.unproven, v)
	if p, ok := m.procs[v]; ok {
		p.sess.Kill()
		m.dropProcess(v)
	}
	label := string(v)
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if c.Vendor != v {
			continue
		}
		label = c.Label
		c.Restored = false
		c.Cleared = true
	}
	// Saved now rather than at the next dispatch. The room file is what a
	// reattach reads, so a clear that lived only in memory would be undone by
	// quitting — the user would have ended a thread and found it waiting for
	// them, which is the failure this whole control was built to remove.
	m.saveRoom()
	m.st.Notice = label + "'s thread cleared — its next brief opens a new session"
}

// askUndoSeat arms the confirmation for undoing the focused seat's race
// attempt (§9.37, amended 2026-08-09) — askClearSeat's shape, because the two
// keys make the same kind of claim: something this seat holds is about to be
// irrevocably dropped, and a card whose y does nothing teaches that the key is
// unreliable rather than that there is nothing to drop.
//
// The refusals are each their own sentence because they are different facts
// with different remedies: no race this turn (there is nothing `u` addresses),
// a measured zero (the attempt changed nothing, so there is nothing an undo
// would remove), and already undone (it worked the first time — pressing again
// is not a way to make it more undone). Collapsing any two would be the
// degraded-vs-zero bug applied to a keystroke.
func (m *Model) askUndoSeat() {
	if m.turn != nil {
		m.st.Notice = "a turn is in flight — u undoes a race attempt between turns"
		return
	}
	c := m.focused()
	if c == nil {
		m.st.Notice = "no seat is focused"
		return
	}
	if c.Arena == nil {
		m.st.Notice = "no race on " + c.Label + "'s current turn — u takes a race attempt back"
		return
	}
	r := c.Arena
	if r.Undone {
		m.st.Notice = c.Label + "'s attempt is already undone — its worktree is back at " + shortSHA(r.Base)
		return
	}
	if r.Err == "" && strings.TrimSpace(r.Stat) == "" {
		m.st.Notice = c.Label + "'s attempt changed nothing — there is nothing to undo"
		return
	}
	m.undoPending = c.Vendor
	m.st.Notice = "undo " + c.Label + "'s attempt? y resets its worktree and branch to " + shortSHA(r.Base) + " · n keeps it"
}

// undoGateKey answers the confirmation armed by `u`. Anything that is not y
// or n cancels rather than falling through to viewKey — clearGateKey's rule,
// for clearGateKey's reason: this gate interrupts nothing, so the safe reading
// of a key nobody meant to press is to put the reset back out of reach.
func (m *Model) undoGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.undoPending
	m.undoPending = ""
	switch msg.String() {
	case "y":
		m.undoSeat(v)
	case "n":
		m.st.Notice = "kept — nothing was undone"
	default:
		m.st.Notice = "undo cancelled — y confirms, n declines"
	}
	return m, nil
}

// undoSeat resets one racer's worktree — and, through it, its arena branch —
// back to the recorded base (§9.37, amended 2026-08-09).
//
// The path guard is the whole safety argument, so it is explicit rather than
// trusted: the reset runs ONLY on a path that equals arenaTree(workspace,
// raceN, vendor) recomputed from the room's current workspace and the RACE
// number recorded on the result — the one name this room's arena would have
// minted for this seat this race, and a name that can never equal the
// workspace itself (the -arena-t<N>-<vendor> suffix sees to that). The race
// number, not c.TurnN: the race numbers itself past older rooms' leftovers
// (arenaRaceNumber, §9.37 amended 2026-08-09), so a guard recomputing from
// the turn would refuse every legitimate undo the moment the two numbers
// diverge. A recorded Tree that does not match — a forged fixture, a room
// that /cd'd away since the race, state damaged in any way — refuses without
// running git at all, because `reset --hard` pointed at the wrong directory
// is the exact irreversible act the confirm gate exists to price.
//
// A failed reset surfaces git's own first stderr line (the gitOut convention)
// and leaves Undone unset: the room must not claim a rollback it did not
// measure happening.
func (m *Model) undoSeat(v model.VendorID) {
	c := m.column(v)
	if c == nil || c.Arena == nil {
		m.st.Notice = "no race attempt to undo"
		return
	}
	r := c.Arena
	if r.Tree != arenaTree(m.st.Workspace, r.RaceN, v) {
		m.st.Notice = "undo refused: " + r.Tree + " is not an arena tree this room's race made — nothing was reset"
		return
	}
	if err := undoArena(r.Tree, r.Base); err != nil {
		m.st.Notice = "undo failed: " + err.Error()
		return
	}
	r.Undone = true
	m.st.Notice = c.Label + "'s attempt undone — " + r.Branch + " and its worktree are back at " + shortSHA(r.Base)
}

// askGiveUpSeat arms the confirmation for giving up on the focused LIVE seat
// mid-turn (§9.37, amended 2026-08-09 and again 2026-08-17) — the one per-seat
// act that runs while a turn is in flight, because mid-flight is the only time
// it means anything. The measurement that forced it: the second live /arena
// raced four seats, three landed in 5–27 minutes, and the fourth streamed for
// 26m40s with its live stat honestly reading "no changes yet against <base>"
// the whole time — so the operator sat ~20 minutes after the race was decided,
// because ctrl+c is the only exit and it cancels EVERYTHING. One stuck racer
// held the whole turn hostage while the room displayed the truth and offered
// no act on it.
//
// ORDINARY TURNS TOO, ruled 2026-08-17. The key shipped arena-only, and its
// refusal said an ordinary turn's seats share one fate by design. The owner
// reversed that: the one-fate line was a four-seat-era position, and the room
// now seats five (§9.39), which makes one stalled vendor on an @all turn the
// most probable live failure the room has. The hostage argument does not
// change when the brief is prose instead of a race — only what the cut costs
// changes, and that is what the card and the note say per seat kind.
//
// The refusals are each their own sentence because they are different facts
// with different remedies (askUndoSeat's rule): no turn in flight (there is
// nothing running to give up on), a turn ctrl+c is already stopping (every
// seat is going, so a per-seat act would only re-label one of them), and a
// seat that already landed (its result is settled; killing a corpse is not a
// way to make it more finished). The help panel's room-controls row is at its
// exact 114-cell budget and does not name this key: it is taught by these
// refusals and by §9.37's amendments, the same way /adopt and /arena drop are
// taught by theirs. ctrl+c is untouched and stays the whole-turn act.
func (m *Model) askGiveUpSeat() {
	if m.turn == nil {
		m.st.Notice = "no turn is in flight — x gives up on one live seat mid-turn"
		return
	}
	if m.cancelling {
		m.st.Notice = "ctrl+c is already cancelling this turn — every seat is stopping"
		return
	}
	c := m.focused()
	if c == nil {
		m.st.Notice = "no seat is focused"
		return
	}
	if !m.turn.live[c.Vendor] {
		m.st.Notice = c.Label + " already landed — there is nothing running to give up on"
		return
	}
	m.giveUpPending = c.Vendor
	m.st.Notice = "give up on " + c.Label + "? " + giveUpCost(m.turn, c.Vendor)
}

// giveUpCost is the second half of the y/n card: what pressing y actually costs
// THIS seat. Three sentences for three seat kinds, because the cost genuinely
// differs and a card that named only the common part would be asking the user
// to authorize an act it had not described.
//
// A racer is a throwaway one-shot in a worktree, so its work survives as the
// diff. An ordinary batch seat is a one-shot with no worktree, so what it
// streamed is all there is. The persistent seat is INTERRUPTED rather than
// killed, so the sentence has to say the conversation lives — the whole reason
// the interrupt exists is that killing it would throw away the session-init
// cost and make the next brief expensive (cancelTurn's own argument).
func giveUpCost(ts *turnState, v model.VendorID) string {
	switch {
	case ts.arena:
		return "y kills its racer and lands the column cancelled — anything it wrote stays in the diff · n lets it race"
	case ts.persistent[v]:
		return "y interrupts it and lands the column cancelled — its conversation survives, so the next brief resumes it · n lets it work"
	default:
		return "y kills its process and lands the column cancelled — what it streamed stays on the column · n lets it work"
	}
}

// giveUpGateKey answers the confirmation armed by `x`. Anything that is not y
// or n cancels rather than falling through to viewKey — clearGateKey's rule,
// for clearGateKey's reason: this gate interrupts nothing, so the safe reading
// of a key nobody meant to press is to leave the seat running.
func (m *Model) giveUpGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v := m.giveUpPending
	m.giveUpPending = ""
	switch msg.String() {
	case "y":
		m.giveUpSeat(v)
	case "n":
		m.st.Notice = "kept — the seat works on"
	default:
		m.st.Notice = "give-up cancelled — y confirms, n declines"
	}
	return m, nil
}

// giveUpSeat stops ONE live seat and lands its column cancelled, leaving the
// rest of the turn running — which is the entire point: the turn's live set
// drains through finishColumn exactly as it does for a seat that landed on its
// own, so the turn can end when the others do.
//
// HOW a seat is stopped is per seat kind, and the three arms are not
// interchangeable:
//
//   - The ephemeral ACP racer and the keyed one-shot racer are killed. Both are
//     minted by dispatch's arena branch and both live on the TURN — never
//     m.procs — so a room seat idling behind the same vendor id survives its
//     racer being given up on (applyEvents' KindDone attribution rule). The
//     ephemeral kill is repeated by finishColumn moments later (it kills before
//     reading the diff, so the receipt is a snapshot of a stopped attempt); both
//     Kill implementations are idempotent by contract, and the double call is
//     cheaper than a second code path finishColumn's ordering comment would have
//     to carry.
//   - An ordinary turn's batch seat is killed through turnState.seatHandles, the
//     same act on the same kind of process, keyed the same way.
//   - The PERSISTENT seat is INTERRUPTED, never killed. Killing it would work,
//     and it would also throw away the conversation and the session-init cost
//     that bought it, so cutting one turn would silently make the next one
//     expensive — cancelTurn's argument, unchanged, applied per seat. The next
//     brief resumes the seat, and the column's note says so.
//
// The events the stopped seat already queued arrive later and land inert, and
// from 2026-08-17 that is a guard rather than a coincidence: turnState.givenUp
// is set BEFORE anything is stopped, so the buffered stdout of a killed child
// and the interrupted vendor's own failed `result` both meet it. A kill this
// function performs is never re-labelled a vendor failure — runner.Handle
// reports a killed child as a clean exit for exactly this case.
func (m *Model) giveUpSeat(v model.VendorID) {
	c := m.column(v)
	// Re-checked, not trusted: events drain between the card arming and the y,
	// so the seat can land — or the whole turn end — while the question is up.
	// A give-up that ran anyway would re-finish a settled column.
	if m.turn == nil || c == nil || !m.turn.live[v] {
		m.st.Notice = "the seat landed while the question was up — nothing was stopped"
		return
	}
	// Recorded first, so that nothing the stop itself provokes can arrive at
	// applyEvents ahead of the fact that explains it.
	if m.turn.givenUp == nil {
		m.turn.givenUp = map[model.VendorID]bool{}
	}
	m.turn.givenUp[v] = true

	persistent := m.turn.persistent[v]
	switch {
	case persistent:
		m.interruptSeat(v)
	default:
		if es, ok := m.turn.arenaEphemeral[v]; ok {
			es.Kill()
		} else if h, ok := m.turn.arenaHandles[v]; ok {
			h.Kill()
		} else if h, ok := m.turn.seatHandles[v]; ok {
			h.Kill()
		}
	}
	// The KindDone exit path's own retirement steps, in its order: flush the
	// redactor's held tail (a give-up must not eat the seat's last word),
	// stamp the clock, name what happened, then finishColumn does everything
	// already built — on a race that is kill-before-diff, collect,
	// commit-per-turn, rank (a DNF finished too, and the render welds the rank
	// to the phase word so "4th · cancelled" cannot read as a result) and clear
	// the interim stat; on every turn it is the drain of this seat from the
	// turn's live set.
	c.Body += m.flush(v)
	c.Elapsed = time.Since(c.Started)
	c.Note = giveUpNote(m.turn, v, c)
	m.finishColumn(c, PhaseCancelled)
	m.st.Notice = "gave up on " + c.Label + " — " + giveUpOutcome(m.turn, v)
}

// giveUpNote is the sentence the cut column keeps, and it exists to keep FOUR
// column states apart on screen (§4a.1, applied to the ways a column can end
// without an answer):
//
//   - given up — this note, naming the elapsed and what became of the seat;
//   - not addressed — "not addressed in turn N", with Column.Skipped set;
//   - ctrl+c — "cancelled — the output above is partial", the whole-turn act;
//   - a measured empty answer — a body reading "[Turn completed with 0 text
//     chunks streamed]" under PhaseDone, which a cut seat must never acquire.
//
// The empty case says itself rather than leaning on the absent body alone: a
// seat that never spoke and a seat that answered nothing are different facts,
// and only one of them was stopped by the operator. Past tense on purpose —
// "when it was cut" stays true if a killed child's last buffered chunk lands
// after the column has retired, which is the one thing this note cannot
// prevent and must not contradict.
func giveUpNote(ts *turnState, v model.VendorID, c *Column) string {
	if ts.arena {
		return "given up after " + dur(c.Elapsed) + " — anything it wrote is in the diff"
	}
	arrived := "nothing had arrived when it was cut"
	if strings.TrimSpace(c.Body) != "" {
		arrived = "what arrived before the cut is above"
	}
	fate := "its process is dead"
	if ts.persistent[v] {
		fate = "its conversation survives, so the next brief resumes it"
	}
	return "given up after " + dur(c.Elapsed) + " — " + arrived + "; " + fate
}

// giveUpOutcome is the footer's half of the same fact. It names the MECHANISM
// because that is the part the user cannot see: a killed seat and an
// interrupted one both land a cancelled column, and only one of them still has
// a conversation behind it.
func giveUpOutcome(ts *turnState, v model.VendorID) string {
	if ts.persistent[v] {
		return "it was interrupted, its column landed cancelled, and its conversation is intact"
	}
	if ts.arena {
		return "its racer is dead and its column landed cancelled"
	}
	return "its process is dead and its column landed cancelled"
}

// flowWriteGateKey authorizes or cancels a /flow write hop before any seat is spawned.
func (m *Model) flowWriteGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.flowWriteArmed = true
		m.flowWritePending = false
		m.st.Notice = "write hop authorized — dispatching"
		return m, m.dispatch()
	case "n":
		// The whole chain goes, not just the gated hop — a chain whose write was
		// refused has nothing legal to do next — and it goes through endFlowChain
		// so the header's hop marker goes with it. This path used to clear the
		// chain by hand and leave the marker up: a room claiming "hop 1/2" over a
		// chain the user had just refused, until the next dispatch happened to
		// clean it (§9.35).
		m.endFlowChain()
		m.st.Notice = "flow write hop cancelled"
		return m, nil
	}
	m.st.Notice = "flow write gate — y authorizes, n cancels"
	return m, nil
}

// toggleFlowStop arms or disarms stop-after-this-hop for the live chain (§9.35).
//
// Arming is a promise about the room's NEXT act, so it does not live only in
// this notice: the header's hop cell says "stops here" for as long as the
// promise stands, and the mode line's `s` cell flips to name the reversal. A
// dead key must say why it did nothing (§9.12's attribution rule), so a press
// with no chain running is answered rather than swallowed — and the last hop
// refuses to arm at all, because the chain ends there whether or not `s` is
// pressed, and a room that let the key "work" would be claiming credit for an
// outcome it did not cause.
func (m *Model) toggleFlowStop() {
	if m.flowChain == nil || m.flowChain.Current() == nil {
		m.st.Notice = "no flow chain is running — s stops one after its current hop"
		return
	}
	hop, total := m.flowChain.CurrentIndex+1, len(m.flowChain.Steps)
	if m.st.FlowStop {
		m.st.FlowStop = false
		m.st.Notice = fmt.Sprintf("the chain continues — hop %d/%d hands off when it returns", hop, total)
		return
	}
	if hop == total {
		m.st.Notice = fmt.Sprintf("hop %d/%d is the last — the chain ends here anyway", hop, total)
		return
	}
	m.st.FlowStop = true
	m.st.Notice = fmt.Sprintf("the chain stops after hop %d/%d — %s will not be dispatched · s continues it",
		hop, total, hopsWord(total-hop))
}

// gateKey is the keymap while a vendor is waiting to be told yes or no.
//
// Only two keys are added, and neither is a modifier: a decision the user is
// being asked to make dozens of times in a session cannot cost a chord. They
// are unambiguous single letters that mean the same thing in every prompt
// anyone has answered, and the footer spells both out on every frame.
func (m *Model) gateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.decideGate(true)
		return m, nil
	case "n":
		m.decideGate(false)
		return m, nil
	case "a":
		// The third key, and the §9.17 surface for --auto. It is on the CARD
		// rather than in the composer because this is the one preference nobody
		// forms before the room opens: you decide to stop being asked while
		// looking at the eleventh identical card, not at a shell prompt.
		//
		// It approves the card in front of you as well as the ones after it.
		// An `a` that turned asking off and left the current request pending
		// would answer the general question and not the one on screen.
		m.stopAsking()
		return m, nil
	case "i", "enter":
		// Composing here would swallow y and n as text while a vendor sat
		// blocked behind a card the user could no longer answer.
		m.st.Notice = "a vendor is waiting on you — y approves, n denies, a stops asking"
		return m, nil
	}
	// Everything else keeps meaning what it meant: scrolling, focus, expand,
	// help and cancel all stay available, because deciding well means being
	// able to read the column first.
	return m.viewKey(msg)
}

func (m *Model) composeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// The one key that always means the same thing. Children die with the
		// room; quitting must never leave an agent running invisibly.
		m.teardown()
		return m, tea.Quit
	case "esc":
		// esc leaves compose but KEEPS the draft. Unlike the HUD's find query,
		// a half-typed brief is expensive to retype and discarding it is not a
		// safety property — nothing has been dispatched.
		m.st.Mode = ModeViewing
		m.st.Notice = ""
	case "ctrl+r":
		// Arms rebuttal for the next dispatch. Available in compose mode
		// because that is where the decision is made, and it needs a modifier
		// because every unmodified key here is text.
		m.toggleQuote()
	case "ctrl+j":
		// A deliberate newline. It goes in RAW rather than through
		// sanitizeKeepingSpace, which is the whole change: that filter flattens
		// every newline that arrives as key TEXT — decoder noise, or a paste
		// being replayed keystroke by keystroke in a terminal with no bracketed
		// paste. What it must not do is flatten the one the user asked for by
		// name — a keystroke is not noise, and a composer where the newline key
		// inserts a space is a composer nobody can write a paragraph in. (A
		// bracketed paste keeps its newlines through the other door: paste.go.)
		m.setDraft(m.st.Draft + "\n")
		m.st.Notice = ""
	case "enter":
		// A draft addressed to the room itself — /cd — is handled here and
		// never reaches a vendor. Everything else dispatches.
		if m.roomCommand() {
			return m, nil
		}
		return m, m.dispatch()
	case "backspace":
		if d := []rune(m.st.Draft); len(d) > 0 {
			m.setDraft(string(d[:len(d)-1]))
		}
		m.st.Notice = ""
	case "ctrl+u":
		// Backspace at paste scale: the whole draft in one keystroke. The
		// ruling — why this chord, why no y/n, what the empty draft does —
		// lives on clearDraft (§9.38).
		m.clearDraft()
	default:
		// A key that carries NO text is not text, so it keeps the meaning it has
		// in view mode. The test is msg.Text rather than a hand-kept list of
		// exceptions, which is the whole point: compose mode swallows keys
		// because they are letters, and a key that cannot be a letter was only
		// ever being swallowed by accident.
		//
		// It was a bad accident. A finished turn drops the room back into
		// compose (turnColumnFinished), which is exactly the moment four long
		// answers land — so every scroll key died at the instant the user most
		// needed one, and stayed dead until they guessed at `esc`. Scrolling had
		// existed since the first version; it was unreachable from the mode the
		// room puts you in.
		if msg.Text == "" && m.navKey(msg.String()) {
			return m, nil
		}
		if t := msg.Text; t != "" {
			m.setDraft(m.st.Draft + sanitizeKeepingSpace(t))
			m.st.Notice = ""
		}
	}
	return m, nil
}

// navKey handles the keys that move the VIEW rather than the draft, and reports
// whether it consumed one.
//
// Shared by both modes so they cannot drift: a key routed here means the same
// thing whichever mode is live, which is what lets the mode line keep promising
// scrolling in compose without a second implementation to keep in step.
//
// Only the unambiguous keys are here. The letter aliases (`j`, `k`, `h`, `l`,
// `g`, `G`, space) stay in viewKey, because in compose they are the letters
// j, k, h, l, g, G and a space — the same rule that keeps `q` the letter q.
//
// `left`, `right`, `home` and `end` are deliberately NOT here even though they
// are dead in compose today. They are where an in-draft cursor goes if the
// composer ever grows one, and binding them to focus now would make that a
// breaking change to muscle memory rather than an addition. `tab` carries focus
// instead, and it has to carry it: scroll keys that address the focused column
// are useless in a mode with no way to change which column that is.
func (m *Model) navKey(name string) bool {
	switch name {
	case "up":
		m.scrollBy(-1)
	case "down":
		m.scrollBy(1)
	case "pgup":
		m.scrollBy(-m.pageSize())
	case "pgdown":
		m.scrollBy(m.pageSize())
	case "tab":
		m.focusBy(1)
	case "shift+tab":
		m.focusBy(-1)
	default:
		return false
	}
	return true
}

// pageOpen reports that the body is showing one turn rather than the grid.
//
// Read in the handful of places a key means something different across the two
// projections, so the condition is spelled once and the keymap and the mode line
// are asking the same question (§9.22).
func (m *Model) pageOpen() bool { return m.st.Page.Open }

// toggleTurnView swaps the body between the by-seat grid and one turn read
// across every seat.
//
// It opens on the NEWEST turn, which is the turn the grid's columns are already
// following when `t` is pressed: the projection changes and the subject does
// not, so the key reads as turning the transcript ninety degrees rather than as
// navigating somewhere. That is also why it is a toggle on one key — the two
// views are one transcript, and a pair of keys would make them two places.
//
// toggleArenaDiff flips the focused seat's arena block between the stat and
// the full patch. Refused with the reason named when there is nothing to flip
// to — askClearSeat's rule: a key that silently does nothing teaches that the
// key is unreliable, and "no race", "changed nothing" and "unreadable" are
// three different reasons deserving three different sentences.
func (m *Model) toggleArenaDiff() {
	c := m.focused()
	if c == nil {
		m.st.Notice = "no seat is focused"
		return
	}
	switch {
	case c.Arena == nil:
		m.st.Notice = "no race on " + c.Label + "'s current turn — d shows an arena diff"
	case c.Arena.Err != "":
		m.st.Notice = "no diff to show — " + c.Arena.Err
	case c.Arena.Diff == "":
		m.st.Notice = c.Label + "'s attempt changed nothing — there is no diff to show"
	default:
		c.ArenaShowDiff = !c.ArenaShowDiff
	}
}

// A room with no turn is told so rather than handed an empty page. askClearSeat's
// shape and askClearSeat's reason: a control that opens onto nothing teaches
// that the key is unreliable, not that the room is empty.
func (m *Model) toggleTurnView() {
	if m.closeRecord() {
		// `t` is the room's one way back to the columns from a full-frame body,
		// and the arena record is one (§9.47). Giving the record a key of its own
		// would be a second thing to remember for the same act, and giving it none
		// would be a body reached by a typed command with no keyed way out. It
		// closes to the GRID rather than to the turn page, because that is what
		// the cell on the mode line says the key does.
		m.st.Notice = ""
		return
	}
	if m.st.Page.Open {
		m.st.Page.Open = false
		m.st.Notice = ""
		return
	}
	turns := m.st.PageTurns()
	if len(turns) == 0 {
		m.st.Notice = "no turn has been taken yet — t reads the room one turn at a time"
		return
	}
	// `t` means the READING face, always. The face a reader last chose is not
	// carried back across a close: this key's own documentation is that it gives
	// one turn the whole room, and a `t` that sometimes opened onto the ledger
	// instead would be two keys wearing one name (§9.22, amended 2026-08-17).
	m.st.Page.Ledger = false
	m.openPage(turns[len(turns)-1])
}

// toggleActLedger swaps the open turn between its two faces — what the seats
// SAID and what they DID — and opens the projection on the newest turn when it is
// closed.
//
// One key doing both is toggleArenaDiff's shape rather than a shortcut, and the
// two are the same act at two scales: `d` flips one seat's arena block between
// the stat and the whole patch, and this flips one turn between its replies and
// its acts. Neither navigates — the subject is untouched — so a reader who has
// walked back to turn 7 stays on turn 7 in either face.
//
// The POSITION is re-resolved through openPage rather than kept, because the two
// faces are different lengths: a scroll offset that meant "halfway down the
// replies" would mean something arbitrary in the acts, and the live turn's tail is
// the one place a reader expects to land.
//
// A room with no turn is told so rather than handed an empty ledger, which is
// toggleTurnView's own refusal and askClearSeat's reason: a control that opens
// onto nothing teaches that the key is unreliable, not that the room is empty.
func (m *Model) toggleActLedger() {
	if m.st.Page.Open {
		m.st.Page.Ledger = !m.st.Page.Ledger
		m.openPage(m.st.Page.Turn)
		return
	}
	turns := m.st.PageTurns()
	if len(turns) == 0 {
		m.st.Notice = "no turn has been taken yet — T reads what the seats did, one turn at a time"
		return
	}
	m.st.Page.Ledger = true
	m.openPage(turns[len(turns)-1])
}

// openPage puts one turn on screen.
//
// The LIVE turn opens FOLLOWING its tail and a finished one opens at its head,
// and that is the grid's rule rather than a second one: what arrives next belongs
// at the bottom of a turn still running, and a turn that is over is a document
// whose top is the brief that produced it. Pure over State — it asks State.Turn,
// never a clock.
func (m *Model) openPage(n int) {
	m.st.Page.Open = true
	m.st.Page.Turn = n
	m.st.Page.Scroll = 0
	m.st.Page.Follow = n == m.st.Turn
	m.st.Notice = ""
}

// hopPage walks the page a turn at a time, in `[` and `]`'s existing vocabulary
// (§9.20) — one motion, one unit, both projections.
//
// The two ends are asymmetric exactly as the grid's hops are. `[` at the first
// turn does nothing: there is no turn 0, and a wrap would make a key pressed one
// time too many jump a whole conversation. `]` past the newest restores that
// page's tail, because what comes after the last turn is the live output — G's
// answer to the same question rather than a second one.
func (m *Model) hopPage(d int) {
	turns := m.st.PageTurns()
	if len(turns) == 0 {
		return
	}
	pos := -1
	for i, n := range turns {
		if n == m.st.Page.Turn {
			pos = i
			break
		}
	}
	if pos < 0 {
		// The open page's last record was evicted by the fifty-turn cap while it
		// was on screen (§9.9). The hop lands on the newest turn rather than
		// refusing: the reader asked to move, and the alternative is a key that
		// silently does nothing on the one page that cannot be read any more.
		m.openPage(turns[len(turns)-1])
		return
	}
	switch next := pos + d; {
	case next < 0:
		return
	case next >= len(turns):
		m.followPage()
	default:
		m.openPage(turns[next])
	}
}

// gotoPage is `g` and `G` in the by-turn projection: the first turn still in
// memory, or the newest.
//
// The same two positions those keys already reach in a column — the beginning of
// what this room remembers, and the live end — measured in turns because that is
// the unit this projection moves in. `G` on the newest page also restores its
// tail, through openPage's own rule, so the key answers "take me to now" in one
// press from anywhere.
func (m *Model) gotoPage(d int) {
	turns := m.st.PageTurns()
	if len(turns) == 0 {
		return
	}
	if d < 0 {
		m.openPage(turns[0])
		return
	}
	m.openPage(turns[len(turns)-1])
}

// followPage pins the page back to its newest line.
func (m *Model) followPage() {
	m.st.Page.Follow = true
	m.st.Page.Scroll = PageMaxScroll(m.st)
}

// pageScrollBy moves the page and takes it off the tail, on exactly the terms
// scrollBy moves a column: scrolling down into the bottom re-arms following,
// scrolling up disarms it.
func (m *Model) pageScrollBy(d int) {
	max := PageMaxScroll(m.st)
	cur := m.st.Page.Scroll
	if m.st.Page.Follow {
		cur = max
	}
	off := cur + d
	if off < 0 {
		off = 0
	}
	if off >= max {
		m.st.Page.Scroll, m.st.Page.Follow = max, true
		return
	}
	m.st.Page.Scroll, m.st.Page.Follow = off, false
}

// setDraft changes the brief and re-derives its routing.
//
// Routing is recomputed on every keystroke rather than at dispatch so the
// footer can show it as it is typed. Deleting the "x" from "@codex" has to move
// the indicator back to the Claude default at the moment it stops being a
// mention, not after enter.
func (m *Model) setDraft(s string) {
	m.st.Draft = s
	m.st.Route, _ = ParseRoute(s)
}

// clearDraft empties the composer in one keystroke, and says how much it took.
//
// It exists because paste changed the arithmetic (§9.38). A draft used to cost
// at most a typed sentence, so backspace's rune-at-a-time delete was
// proportionate to any mistake the composer could hold; one wrong paste is now
// up to 8,192 runes in one gesture, and 8,192 backspaces is not an editor. The
// key is ctrl+u — readline's kill-line, the idiom every shell user's hands
// already know for exactly this act — and it matters that it is a CHORD:
// sanitizePaste drops every control character, so no paste can carry this key
// into the room, and no stray letter can fire it. The one gesture that can
// empty a draft is a deliberate hand on ctrl, which is the same argument
// paste.go makes for why a pasted \x03 must not cancel.
//
// No y/n gate, deliberately — unlike `c` and `u`, whose confirms price a drop
// nothing can reverse (a session id is the only handle on a thread). A cleared
// draft's ways back are ordinary: the clipboard still holds a paste, the
// keyboard re-types a sentence. What the room owes instead of a gate is the
// HONEST STATEMENT of the loss, because "cheap to regret" stops being true at
// an 8k paste: the notice carries the rune count of the string just dropped —
// a measured value off the draft itself, in pasteRefusal's own unit and
// spelling ("chars", ungrouped itoa), so the refusal that would not let 20481
// chars in and the notice that let 1204 out are one vocabulary (§4a.1: the
// number is read, never derived from anything but the string it counts).
//
// The empty draft is silent, and that is backspace's precedent applied rather
// than a hole in §9.12's attribution rule. Backspace on an empty draft deletes
// nothing and clears the notice; ctrl+u is the same act at a different size
// and gets the same treatment. The rule demands a sentence from a key whose
// effect was refused or is invisible — but leaning on ctrl+u over an empty
// composer produces exactly the state the key promises, already on screen, and
// an every-press "nothing to clear" would put noise in the cell where dispatch
// answers land.
//
// Nothing but the draft moves. setDraft("") re-derives Route, and that is the
// draft falling, not a second casualty: the routing cell is a statement about
// the draft, and a footer still promising "→ codex" over an empty composer
// would be the mode line lying about what enter does next. Mode stays compose
// (the operator is mid-edit, not leaving), flow state, quote arming, focus and
// the columns are untouched — and a pending y/n can never reach here at all,
// because key() routes every gate ahead of composeKey. Each gate answers a
// stray ctrl+u by its own standing rule (cancel, or restate the question);
// draftclear_test.go asserts that ordering rather than trusting it.
//
// In VIEW mode ctrl+u matches nothing and falls through viewKey silently —
// there is no draft on screen to clear, and esc's contract when it parked the
// draft was "keeping the draft"; a chord that revoked that promise from the
// mode the promise was made to would make esc unsafe in hindsight. The key is
// taught on the help panel's compose-keys row (ctrl+j/u/esc — view.go), not on
// the compose mode line, whose hint row is at its width budget already.
func (m *Model) clearDraft() {
	if m.st.Draft == "" {
		m.st.Notice = ""
		return
	}
	n := utf8.RuneCountInString(m.st.Draft)
	m.setDraft("")
	m.st.Notice = "draft cleared — " + itoa(n) + " " + plural(n, "char")
}

func (m *Model) viewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The keys that mean the same thing in both modes, resolved from one place.
	if m.navKey(msg.String()) {
		return m, nil
	}
	// A digit is a seat (§9.29). View mode only, and by the same contract that
	// keeps `q` the letter q in compose: composeKey routes any key carrying text
	// to the draft, and a digit carries text, so there is no second list to
	// maintain and no way for the two modes to disagree.
	//
	// Handled before the switch rather than as four cases in it, because the
	// binding is over a RANGE whose top is however many seats are on screen — and
	// spelling it as `case "1", "2", "3", "4"` would hard-code a room size the
	// rest of this file takes from VisibleColumns().
	if n := int(msg.Code - '0'); msg.Text != "" && n >= 1 && n <= 9 {
		m.focusSeat(n)
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		// Cancels the turn in flight; quits when there is none. Two meanings
		// for one key is a compromise, but the alternative is a quit key that
		// silently abandons three running agents, and the mode line states
		// which meaning is live on every frame.
		if m.turn != nil {
			m.cancelTurn()
			return m, nil
		}
		// teardown even with no turn in flight. A persistent seat's process
		// outlives its turn by design, so "no turn" stopped meaning "no
		// children" the moment that landed — and an idle room quit this way
		// would have leaked exactly the invisible agent this product refuses.
		m.teardown()
		return m, tea.Quit
	case "q":
		if m.turn != nil {
			m.st.Notice = "a turn is in flight — ctrl+c cancels it first"
			return m, nil
		}
		m.teardown()
		return m, tea.Quit
	case "a":
		// Same letter as the card's, deliberately. There it answers the question
		// in front of you; here it reports and reverses. Safe with a turn in
		// flight, unlike /cd and /seat: queueGate reads Asking per REQUEST, so a
		// running seat starts or stops being carded on its next call rather than
		// needing the process it is mid-turn on to be replaced.
		m.toggleAsking()
		return m, nil
	case "?":
		m.st.Help = m.st.Help.next()
	case "i", "enter":
		m.st.Mode = ModeComposing
		m.st.Help = HelpClosed
		m.st.Notice = ""
	case "right", "l":
		m.focusBy(1)
	case "left", "h":
		m.focusBy(-1)
	case "ctrl+r", "r":
		m.toggleQuote()
	case "f":
		// One column at full width. Three columns are for comparing at a
		// glance; one is for actually reading a long reply.
		if m.pageOpen() || m.st.Record != nil {
			// A page is already the whole frame and has no column to expand.
			// Swallowed rather than allowed to flip Expanded invisibly: the mode
			// line does not offer this key here, and a key the room says is
			// absent must not change what the grid looks like when you go back.
			return m, nil
		}
		m.st.Expanded = !m.st.Expanded
	case "t":
		// The by-turn projection. One key, a toggle, because the two views are
		// one transcript read two ways (§9.22). View mode only: in compose `t` is
		// the letter t, which is the same contract `q`, `f` and `c` already keep.
		m.toggleTurnView()
	case "T":
		// The act ledger: the same turn, read for what the seats DID (§9.22,
		// amended 2026-08-17).
		//
		// SHIFT on the key whose subject it shares, which is the only spelling
		// that puts a third reading of one transcript beside the two it belongs
		// with. Every free lowercase letter left in this keymap is free because it
		// means nothing here, and a projection under an unrelated letter is a
		// projection a reader finds by accident. The capital is unclaimed — `Y`
		// and `G` are the only two this room binds — and it costs the panel no
		// row, because it is taught on the row that already teaches `t`.
		//
		// In compose it is the letter T, which needs no second list: composeKey
		// routes any key carrying text into the draft, the contract `q`, `f`, `c`
		// and `t` already keep.
		//
		// That a shifted letter arrives here as `"T"` rather than as `"shift+t"`
		// was READ off the pinned module, not assumed: ultraviolet's Key.String
		// (v0.0.0-20260811164956) returns Key.Text whenever it is non-empty and
		// not a space, and falls through to Keystroke — where the modifiers get
		// spelled — only otherwise. A printable keypress carries its character,
		// so the Mod bits never reach this switch. `Y` has depended on the same
		// line since §9.15.
		m.toggleActLedger()
	case "d":
		// The focused seat's arena block flips stat ↔ full patch. A key rather
		// than a second yank, because reading and taking are different acts, and
		// per COLUMN because comparing A's stat against B's whole diff is a
		// legitimate way to read a race. Refusals name the reason: a seat with
		// no race this turn, and an attempt whose diff is a measured nothing or
		// an error, are different facts and get different sentences.
		m.toggleArenaDiff()
	case "u":
		// Undo the focused seat's whole race attempt — worktree and arena
		// branch back to the recorded base (§9.37, amended 2026-08-09). A key
		// beside `d` because they address the same block, y/n-confirmed like
		// `c` because both throw away something a keystroke cannot bring
		// back. View mode only: in compose `u` is the letter u, the contract
		// q, f and c already keep.
		m.askUndoSeat()
	case "x":
		// Give up on the focused LIVE seat, mid-turn (§9.37, amended
		// 2026-08-09 for a race and 2026-08-17 for every turn): stop that one
		// seat, land its column cancelled, let the rest of the turn run on —
		// the per-seat exit the second live /arena measured the room lacking,
		// when one stuck racer held a decided race hostage for ~20 minutes
		// because ctrl+c cancels everything. `x` because it is free in view
		// mode and unclaimed by any gate or nav key, and the act is a
		// cross-out, not an undo — `u` takes back what a FINISHED attempt
		// wrote; `x` stops one still running. y/n-confirmed like `c` and `u`:
		// on a batch seat y kills a process nothing can restart. View mode
		// only: in compose `x` is the letter x, the contract q, f and c
		// already keep.
		m.askGiveUpSeat()
	case "c":
		// Clear the focused seat's thread — the first control built to §9.17's
		// rule, and a key rather than a room command for a reason recorded
		// there: focus already names the seat, while a `/clear` would take a
		// word out of the conversation that people mean for a vendor.
		//
		// View mode only, and that is not an oversight. In compose `c` is the
		// letter c, which is the same contract `q` and `f` already keep.
		m.askClearSeat()
	case "s":
		// Stop the /flow chain after the hop that is running now (§9.35). A key
		// rather than a room command for `c`'s reason — no vocabulary leaves the
		// composer — and pressed WHILE a hop streams, because that is when the
		// decision is formed: you are reading hop 2's output when you learn hops
		// 3 and 4 are no longer worth their quota. No y/n gate, unlike `c`,
		// because nothing is destroyed — the current hop finishes on its own
		// terms, artifact and receipt included, and `s` again re-arms the
		// handoff. View mode only: in compose `s` is the letter s.
		m.toggleFlowStop()
	case "y":
		// The focused seat's reply. Reachable here only when nothing is gated:
		// key() routes a pending gate to gateKey first, and gateKey answers `y`
		// itself rather than falling through — so the approve key keeps the
		// letter it has always had and yank simply does not exist while a vendor
		// is blocked. That precedence is asserted rather than assumed, because
		// it is the one collision in this keymap where losing would mean a
		// keystroke the user believes approved a tool call quietly copying text
		// instead. That precedence is unchanged by the by-turn page below: key()
		// still routes a pending gate to gateKey first, in either projection.
		if m.st.Record != nil {
			// The record is a full-frame body, so `y` takes what is on it — the
			// page's own rule (§9.47). A `y` that copied the focused column's
			// reply from behind a body the reader is looking at would break the
			// one claim that earns this key a footer cell.
			return m, m.yank(m.st.YankRecord())
		}
		if m.pageOpen() {
			// On a page `y` and `Y` produce the same document, because the page
			// IS that document (§9.15's `Y`, rendered). A per-seat `y` would need
			// a per-seat focus, and a projection whose whole point is that the
			// turn is the unit deliberately has none — so the narrower key takes
			// the wider document rather than guessing which seat was meant.
			//
			// YankPage rather than YankTurnN, so the key follows the FACE as well
			// as the turn: on the act ledger the document in front of the reader
			// is the acts, and a copy key that took the replies instead would break
			// the one claim that earned it a footer cell here (§9.22, amended
			// 2026-08-17).
			return m, m.yank(m.st.YankPage())
		}
		return m, m.yank(m.st.YankColumn(m.st.Focus))
	case "Y":
		// The whole turn, every seat, labelled. A separate key rather than a
		// modifier on the first because they produce different documents, and
		// shift is what this room already uses for the wider version of a
		// motion (`G` against `g`).
		if m.st.Record != nil {
			// `Y` follows `y` here for the page's own reason: the record has no
			// per-seat focus for a narrower key to address, so both take the one
			// document the body is showing.
			return m, m.yank(m.st.YankRecord())
		}
		if m.pageOpen() {
			return m, m.yank(m.st.YankPage())
		}
		return m, m.yank(m.st.YankTurn())
	case "k":
		m.scrollBy(-1)
	case "j":
		m.scrollBy(1)
	case " ":
		m.scrollBy(m.pageSize())
	case "home", "g":
		// The two ends, in whichever unit the body is currently in: the first
		// line of a column's transcript, or the first turn still in memory.
		if m.pageOpen() {
			m.gotoPage(-1)
			return m, nil
		}
		m.scrollTo(0)
	case "end", "G":
		if m.pageOpen() {
			m.gotoPage(1)
			return m, nil
		}
		m.followFocused()
	case "[":
		// The transcript's unit is the turn and the scroll keys' unit is the
		// line, which at "↑ 509 more above" is a number nobody can act on
		// (§9.20). View mode only: in compose these are the characters `[` and
		// `]`, and composeKey's own rule — a key that carries text IS text —
		// keeps them there without a second list to maintain.
		//
		// In the by-turn projection the same keys move the same unit — the page
		// itself — so there is one motion to learn rather than a second binding
		// for the same idea one view over (§9.22).
		if m.pageOpen() {
			m.hopPage(-1)
			return m, nil
		}
		m.hopTurn(-1)
	case "]":
		if m.pageOpen() {
			m.hopPage(1)
			return m, nil
		}
		m.hopTurn(1)
	}
	return m, nil
}

// yank puts text on the system clipboard and says what it took.
//
// The mechanism is OSC 52, through bubbletea's own tea.SetClipboard, and the
// choice is not really a choice: council must not grow a clipboard dependency
// for one key, and OSC 52 needs no disk, no daemon and no library. Verified by
// reading the installed module rather than the internet — v1 answers for this
// are wrong — charm.land/bubbletea/v2@v2.0.8 clipboard.go returns a Cmd whose
// message tea.go turns into ansi.SetSystemClipboard, which emits
// ESC ] 52 ; c ; <base64> BEL unconditionally: no capability probe, no terminal
// query, nothing that can silently decline.
//
// WHICH IS THE HONEST LIMIT, and it is stated here rather than in a commit
// message. That sequence is a write into the terminal with no acknowledgement
// of any kind, so this room cannot know whether the terminal honoured it.
// Windows Terminal accepts OSC 52 writes in current builds — INFERRED from its
// documented behaviour, not measured, and unmeasurable from a test, since the
// only observer that could settle it is the terminal itself. So the notice
// claims what council DID ("copied…"), never what the machine now holds, and
// the one-keystroke check is a person pressing y and then ctrl+v.
//
// A nil Cmd for an empty yank, deliberately. Writing "" through OSC 52 is the
// documented way to CLEAR a clipboard, so a copy key that found nothing to copy
// would silently destroy whatever the user had — the most expensive possible
// spelling of "nothing happened".
// AMENDED 2026-08-10, and the amendment is the paragraph above being measured
// false. On macOS this key reported "copied …" and the clipboard was untouched:
// Terminal.app does not implement OSC 52 clipboard writes and iTerm2 ships the
// permission off. The limitation was not theoretical, it was untested — the
// reference box happened to be the one terminal that honours the sequence, and
// nothing in a suite could have caught it, because the only observer that can
// settle it is the terminal.
//
// The native helper is tried FIRST wherever one exists (clipboard.go). Not
// because it is better plumbing but because it is CHECKABLE: pbcopy's exit
// status is a fact about the clipboard, and the escape sequence has none. OSC 52
// remains the fallback and is still the only thing that works over SSH.
func (m *Model) yank(y Yank) tea.Cmd {
	m.st.Notice = y.Notice
	if y.Empty() {
		return nil
	}
	if nativeClipboard(y.Text) {
		// Nothing more to emit. Sending the escape sequence as well would put
		// the same text on the clipboard twice on a terminal that honours both,
		// which is harmless, and would ALSO make the failure of one invisible
		// behind the success of the other — which is not.
		return nil
	}
	return tea.SetClipboard(y.Text)
}

// toggleQuote arms or disarms cross-agent rebuttal, and says which.
//
// The notice is not decoration: this is the one toggle that changes what the
// vendors SEE rather than how the room looks, so flipping it silently would
// mean a user could arm it by mistake and never learn that three agents read
// each other.
func (m *Model) toggleQuote() {
	m.st.Quote = !m.st.Quote
	switch {
	case !m.st.Quote:
		m.st.Notice = "rebuttal off — each vendor sees only its own thread"
	case m.st.Turn == 0:
		m.st.Notice = "rebuttal armed — but turn 1 is always blind, so it starts from turn 2"
	default:
		m.st.Notice = "rebuttal armed — each vendor will see the others' last answers, quoted"
	}
}

// scrollBy moves the focused column's view and takes it off the tail.
//
// Scrolling down INTO the bottom re-arms following, so a user who reads to the
// end of a streaming reply keeps receiving the rest without a second keystroke.
// Scrolling up disarms it: yanking someone back to the bottom mid-read is the
// most irritating thing a streaming pane can do, and it hides content.
func (m *Model) scrollBy(d int) {
	if m.pageOpen() {
		// The line-wise keys move whatever the body is showing. Routed here
		// rather than at every call site so ↑ ↓, pgup/pgdn, j, k and space reach
		// the page through the one function that already knows what scrolling
		// means in this room (§9.22).
		m.pageScrollBy(d)
		return
	}
	c := m.focused()
	if c == nil {
		return
	}
	max := MaxScroll(m.st, m.st.Focus)
	cur := c.Scroll
	if c.Follow {
		cur = max
	}
	m.applyScroll(c, cur+d, max)
}

func (m *Model) scrollTo(off int) {
	c := m.focused()
	if c == nil {
		return
	}
	m.applyScroll(c, off, MaxScroll(m.st, m.st.Focus))
}

func (m *Model) applyScroll(c *Column, off, max int) {
	if off < 0 {
		off = 0
	}
	if off >= max {
		off = max
		c.Follow = true
		c.Scroll = max
		return
	}
	c.Follow = false
	c.Scroll = off
}

// hopTurn walks the focused column a turn at a time.
//
// It reads the current offset the way scrollBy does — a following column is at
// max, not at its stale Scroll — so a hop out of the tail measures from where
// the reader actually is, and then hands the landing offset to applyScroll:
// Follow goes false exactly as it does for every other scroll key, because a
// column pinned to the tail while displaying turn 3 would be lying about which
// of the two it is doing.
//
// The two ends are deliberately NOT symmetric, and that asymmetry is the same
// one `g` and `G` already have. Backwards past the first turn does nothing:
// there is no turn 0, and wrapping to the end would make a key pressed one time
// too many jump a whole conversation. Forwards past the last turn restores the
// tail, because "after the last turn" is the live output — the thing G means —
// rather than a place the transcript does not go.
func (m *Model) hopTurn(d int) {
	c := m.focused()
	if c == nil {
		return
	}
	max := MaxScroll(m.st, m.st.Focus)
	cur := c.Scroll
	if c.Follow {
		cur = max
	}
	off, ok := TurnHop(m.st, m.st.Focus, cur, d)
	if !ok {
		if d > 0 {
			m.followFocused()
		}
		return
	}
	m.applyScroll(c, off, max)
}

// followFocused pins the focused column back to the newest output.
func (m *Model) followFocused() {
	if c := m.focused(); c != nil {
		c.Follow = true
		c.Scroll = MaxScroll(m.st, m.st.Focus)
	}
}

// pageSize is one screenful of the body area, less a line of overlap so a page
// jump keeps a shared line of context rather than teleporting.
func (m *Model) pageSize() int {
	lay := layoutFor(m.st, m.glyphs)
	if n := lay.Body - 3; n > 1 {
		return n
	}
	return 1
}

func (m *Model) focused() *Column {
	if m.st.Focus < 0 || m.st.Focus >= len(m.st.Columns) {
		return nil
	}
	return &m.st.Columns[m.st.Focus]
}

// focusBy moves the focused column, wrapping. Focus is an index into Columns,
// which is a fixed set resolved once at startup — unlike the HUD's cursor, it
// cannot be invalidated by a re-sort, so there is no selection-by-key
// machinery here.
//
// It steps through the columns that are DRAWN. Tabbing onto a collapsed seat
// would leave the marker nowhere on screen and the scroll keys addressing a
// column nobody can see.
func (m *Model) focusBy(d int) {
	if m.pageOpen() {
		// Focus is a property of the GRID. One page has no columns to move
		// between, so tab, shift+tab and the arrow aliases do nothing here — and
		// the mode line drops the hint to match, because a footer that promises a
		// key which does nothing is §7.8's surprise pointing the other way
		// (§9.22). Silently moving focus instead would change what the grid shows
		// the next time `t` is pressed, from a key the room said was inert.
		return
	}
	vis := m.st.VisibleColumns()
	if len(vis) == 0 {
		return
	}
	pos := 0
	for j, v := range vis {
		if v == m.st.Focus {
			pos = j
			break
		}
	}
	m.st.Focus = vis[((pos+d)%len(vis)+len(vis))%len(vis)]
}

// focusSeat puts the keys on the nth VISIBLE seat, one-based, in seating order
// (§9.29). It reports whether the key did anything.
//
// Positional, exactly like the columns are, which is what lets one keystroke
// reach a seat instead of tab pressed three times through three full redraws.
// A room with two seats has keys 1 and 2 and nothing else: `3` in a two-seat
// room is a no-op rather than a wrap or a clamp, because a key that quietly
// lands somewhere else is the surprise §7.8 forbids, and a wrap would make the
// number stop meaning the position it is printed at.
//
// Page-gated the same way focusBy is, and from the same argument: a page has one
// reading area, so focus has nothing to move between, and silently moving it
// would change what the grid shows the next time `t` is pressed — from a key the
// mode line says is not there.
func (m *Model) focusSeat(n int) bool {
	if m.pageOpen() {
		return false
	}
	vis := m.st.VisibleColumns()
	if n < 1 || n > len(vis) {
		return false
	}
	m.st.Focus = vis[n-1]
	return true
}

// View is a thin wrapper over the pure renderer.
//
// Unlike the HUD's, this view DOES place a cursor: council has something to
// type into, and the one moving cell §7.1 budgets is spent here while
// composing and on a working column's spinner otherwise.
func (m *Model) View() tea.View {
	if m.st.Width == 0 || m.st.Height == 0 {
		v := tea.NewView("")
		v.AltScreen = true
		return v
	}
	v := tea.NewView(Render(m.st, m.styles, m.glyphs))
	v.AltScreen = true
	v.Cursor = nil
	if !m.opts.NoTitle {
		// The title says which room this is. Someone with a HUD and a council
		// open in two tabs should not have to guess from the taskbar.
		v.WindowTitle = "telltale council"
	}
	return v
}

// wantsGateHook reports whether this room is the one that gates.
//
// Written as the same condition as seatPosture's rather than as "a write room"
// so the two cannot drift apart. A read-only or --auto room has nobody to ask,
// so a hook that answers "ask" there would stall the seat on a question no card
// is drawn for — and there is no reason to leave a temporary file on disk for a
// room that will not read it.
func wantsGateHook(opts Options) bool { return opts.Write && !opts.Auto }

// roomProgram is the one method of *tea.Program the exit-signal watcher drives.
//
// An interface for seatSession's reason and no other: the property under test is
// "the seats were killed before the room went out", and a test that needed a
// real Bubble Tea program — an alternate screen, a terminal, an event loop —
// just to watch a signal land would be measuring the framework rather than the
// watcher. *tea.Program is the only production implementation.
type roomProgram interface{ Kill() }

// Run starts the room.
//
// Council is the one telltale mode that dispatches to vendor CLIs. The
// observation surfaces — statusline and hud — keep their read-only guarantee
// unchanged, and nothing here is reachable from either of them (ADR-008).
func Run(opts Options) error {
	// Loaded before the program starts. A bad --brief path must surface as a
	// plain error on stderr, not as a card inside a TUI the user then has to
	// quit to read.
	b, err := LoadBrief(opts.BriefPath)
	if err != nil {
		return err
	}

	// The sink is installed for the whole life of the room, with or without a
	// --trace path, because the clock was ALWAYS running and only the writing was
	// conditional (trace.go). Holding the last turns in memory is what lets
	// `/trace <file>`, typed after a turn nobody can explain, write that turn
	// instead of the next one.
	trace := newTraceSink()
	runner.SetTrace(trace.record)
	defer func() {
		// Uninstalled before the file is closed, so nothing can write into a
		// closed handle on the way out.
		runner.SetTrace(nil)
		trace.close()
	}()

	// A --trace path is still opened before the alternate screen, for the same
	// reason the brief is: a path that cannot be written must be a line on
	// stderr, not a card behind a TUI the user has to quit to read. Typed at the
	// room instead, the same failure is a notice, because by then there is a
	// footer to put it in.
	if opts.TracePath != "" {
		if _, terr := trace.open(opts.TracePath); terr != nil {
			return terr
		}
	}

	// The room is the persistent object, so reattaching is the DEFAULT: a
	// zero-argument launch reopens the one saved room, and --fresh is the way
	// to decline it. Nothing saved yet is not an error any more — with one
	// global room there is no wrong-key case for an error to catch, so a first
	// launch simply opens fresh. A file that exists but cannot be used is
	// still a notice: see LoadRoom.
	var re Reattachment
	if opts.Fresh {
		if found, ferr := LoadRoom(); ferr == nil && found.Active() {
			// Declined, but there is something here to lose. Carried in as an
			// OFFER rather than a restore: nothing is applied, and the room only
			// mentions it exists before the first dispatch replaces it. Adopted
			// rides along so the offer can name the old format like the reattach
			// notice does.
			re = Reattachment{Path: found.Path, Room: found.Room,
				Offered: true, Adopted: found.Adopted}
		}
	} else {
		re, err = LoadRoom()
		switch {
		case errors.Is(err, ErrNoSavedRoom):
			re, err = Reattachment{}, nil
			if opts.Resume {
				// Asked for explicitly, and there is nothing. Not an error — the
				// flag now names the default — but silence would let a first-run
				// machine masquerade as a continued conversation.
				re.Ignored = "nothing has been saved yet — this is a fresh room"
				re.Path = "-"
			}
		case err != nil:
			// The state DIRECTORY could not even be located (no resolvable home
			// directory). Telltale's own state being unreachable must never be
			// the reason the room refuses to open — the same rule a corrupt
			// file already follows — so it opens unreattached and says why.
			re, err = Reattachment{Path: "-",
				Ignored: "the saved room could not be looked up: " + err.Error()}, nil
		}
	}

	ws, gone, err := openWorkspace(opts, re)
	if err != nil {
		return err
	}
	// Carried on the Reattachment because the notice is written by reattach(),
	// which must not stat the path a second time: two reads of the same
	// directory a moment apart can disagree, and the room would then choose its
	// workspace on one answer and describe it with the other.
	re.WorkspaceGone = gone
	// Threaded through Options so stateWith and the model see the one answer.
	opts.Dir = ws

	// The roster: --vendor if typed, else who the saved room seated, else the
	// detected table. Decided HERE, beside the workspace, because they are the
	// two halves of the same thing — the room's SHAPE, which §9.32 restores and
	// an explicit launch flag overrides. Neither is restored from a room --fresh
	// declined; see seatsFor.
	opts.Seats = seatsFor(opts.Seats, re.Room.Seats, re.Active() && !re.Offered)

	var hooks GateHook
	if wantsGateHook(opts) {
		hooks = NewGateHook()
	}
	// Cleaned up here as well as in teardown. teardown covers the quit paths;
	// this covers every other way out of a program, including one that returns
	// an error before a quit key was ever pressed.
	defer hooks.Cleanup()

	mdl := newWithBrief(opts, b, hooks, re)
	// One ring, and the runner is already writing into it. The constructor's own
	// sink is discarded here rather than never made, so a Model built by a test
	// still has one and /trace never dereferences nil.
	mdl.trace = trace
	p := tea.NewProgram(mdl)
	// Installed before the program runs, and it is the ONLY thing standing
	// between an abnormal exit and five orphaned agents on macOS and Linux. The
	// q and ctrl+c keys reach teardown through the update loop; a signal does
	// not reach the update loop at all (signals_unix.go states the measurement).
	// A no-op on Windows, where the job object already covers it.
	stopSignals := watchExitSignals(mdl, p)
	defer stopSignals()
	_, err = p.Run()
	if err != nil {
		return err
	}
	// The save on the way out has nowhere to render: the notice it sets lands on
	// a model that will never be viewed again. Reported here instead, because a
	// user who quits believing they can reattach and then cannot has been told
	// something false by silence. Not an error — the room ran fine and the
	// state from the last completed turn is still on disk.
	if mdl.saveErr != nil {
		fmt.Fprintln(os.Stderr, "telltale council: the room state could not be saved:", mdl.saveErr)
	}
	return nil
}

var _ tea.Model = (*Model)(nil)
