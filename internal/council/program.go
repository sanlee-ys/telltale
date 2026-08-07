package council

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

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

	// brief is the shared operating context. Held on Model, never on State:
	// its content is the user's private file and the renderer has no business
	// being able to reach it.
	brief Brief

	// hooks is the user's own hook configuration, copied into a file of its
	// own so the gated seat can be pointed at it.
	hooks HookSet

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
	return newWithBrief(opts, Brief{}, HookSet{}, Reattachment{})
}

func newWithBrief(opts Options, b Brief, hs HookSet, re Reattachment) *Model {
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
	if !sameDir(re.Room.Workspace, m.st.Workspace) {
		// The room reopened somewhere other than where it was saved — a --cd
		// override, or a saved directory that no longer exists. The seats'
		// conversations came back either way; only where they act moved, and
		// the mechanics are the same as an in-room /cd.
		m.st.Notice += " (the room was in " + abbreviate(re.Room.Workspace, m.st.Home) +
			"; it is now in " + abbreviate(m.st.Workspace, m.st.Home) + ")"
	}
	if re.Room.Posture != savedPosture(m.st.Write, m.opts.Auto) {
		// Stated rather than applied. The saved room ran under a different
		// posture, and a user who reattaches a write room without retyping
		// --write should learn that from the room instead of from a vendor
		// refusing to edit a file.
		m.st.Notice += " (it ran " + re.Room.Posture + "; this room is " +
			savedPosture(m.st.Write, m.opts.Auto) + ")"
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
			Sandbox: postureClaim(info.Vendor, windows, opts.Write, !opts.Auto, hooked),
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

type spinMsg time.Time

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		// RequestBackgroundColor is a Msg in v2, not a Cmd: it is a request the
		// runtime turns into an OSC query, so it has to be lifted into a Cmd
		// rather than handed to Batch directly.
		func() tea.Msg { return tea.RequestBackgroundColor() },
		spin(),
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
			return m, nil
		}
		return m, m.waitEvents()

	case spinMsg:
		// Now is stamped here, on the tick, so Render never reads a clock and
		// the elapsed counters advance on the same schedule as the spinner.
		m.st.Now = time.Time(msg)
		// The spinner only advances while a column is genuinely working. A
		// motionless room is the honest render of a room where nothing is
		// happening, and it keeps §7.1's budget of one moving cell.
		if m.st.Busy() {
			m.st.Spinner++
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
	if m.st.Gating() {
		return m.gateKey(msg)
	}
	if m.clearPending != "" {
		return m.clearGateKey(msg)
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

// flowWriteGateKey authorizes or cancels a /flow write hop before any seat is spawned.
func (m *Model) flowWriteGateKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y":
		m.flowWriteArmed = true
		m.flowWritePending = false
		m.st.Notice = "write hop authorized — dispatching"
		return m, m.dispatch()
	case "n":
		m.flowWritePending = false
		m.flowWriteArmed = false
		m.flowChain = nil
		m.flowDraft = ""
		m.flowReadHop = false
		m.st.Notice = "flow write hop cancelled"
		return m, nil
	}
	m.st.Notice = "flow write gate — y authorizes, n cancels"
	return m, nil
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
	case "i", "enter":
		// Composing here would swallow y and n as text while a vendor sat
		// blocked behind a card the user could no longer answer.
		m.st.Notice = "a vendor is waiting on you — y approves, n denies"
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
		// every newline to a space precisely so a PASTED one cannot tear the
		// footer apart, and it still does. What it must not do is flatten the
		// one the user asked for by name — a keystroke is not a paste, and a
		// composer where the newline key inserts a space is a composer nobody
		// can write a paragraph in.
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
// A room with no turn is told so rather than handed an empty page. askClearSeat's
// shape and askClearSeat's reason: a control that opens onto nothing teaches
// that the key is unreliable, not that the room is empty.
func (m *Model) toggleTurnView() {
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

func (m *Model) viewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The keys that mean the same thing in both modes, resolved from one place.
	if m.navKey(msg.String()) {
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
		if m.pageOpen() {
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
	case "c":
		// Clear the focused seat's thread — the first control built to §9.17's
		// rule, and a key rather than a room command for a reason recorded
		// there: focus already names the seat, while a `/clear` would take a
		// word out of the conversation that people mean for a vendor.
		//
		// View mode only, and that is not an oversight. In compose `c` is the
		// letter c, which is the same contract `q` and `f` already keep.
		m.askClearSeat()
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
		if m.pageOpen() {
			// On a page `y` and `Y` produce the same document, because the page
			// IS that document (§9.15's `Y`, rendered). A per-seat `y` would need
			// a per-seat focus, and a projection whose whole point is that the
			// turn is the unit deliberately has none — so the narrower key takes
			// the wider document rather than guessing which seat was meant.
			return m, m.yank(m.st.YankTurnN(m.st.Page.Turn))
		}
		return m, m.yank(m.st.YankColumn(m.st.Focus))
	case "Y":
		// The whole turn, every seat, labelled. A separate key rather than a
		// modifier on the first because they produce different documents, and
		// shift is what this room already uses for the wider version of a
		// motion (`G` against `g`).
		if m.pageOpen() {
			return m, m.yank(m.st.YankTurnN(m.st.Page.Turn))
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
func (m *Model) yank(y Yank) tea.Cmd {
	m.st.Notice = y.Notice
	if y.Empty() {
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

// wantsHooks reports whether this room is the one that needs its hooks copied.
//
// Exactly the room that passes --setting-sources "", and it is written as the
// same condition as seatPosture's rather than as "a write room" so the two
// cannot drift apart. A read-only or --auto room loads the user's settings
// natively: there is nothing to repair there, no reason to leave a temporary
// file on disk for it, and injecting the hooks anyway would fire each of them
// twice.
func wantsHooks(opts Options) bool { return opts.Write && !opts.Auto }

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

	// The workspace: --cd if typed, else where the room was, else here. Both
	// non-cwd sources are verified to be directories before the room is
	// pointed at them: a typed --cd that is not one is the LoadBrief
	// discipline — a plain error before the alternate screen — and a saved
	// directory that is gone (a renamed repo) surfaces as one honest sentence
	// in the notice, not as four seats failing their first turn against a
	// path that no longer exists.
	ws := ""
	switch {
	case opts.Dir != "":
		ws = resolveWorkspace(opts.Dir)
		if fi, serr := os.Stat(ws); serr != nil || !fi.IsDir() {
			return errors.New("--cd " + opts.Dir + ": not a directory")
		}
	case re.Active() && !re.Offered:
		ws = re.Room.Workspace
		if fi, serr := os.Stat(ws); serr != nil || !fi.IsDir() {
			ws = resolveWorkspace("")
		}
	default:
		ws = resolveWorkspace("")
	}
	// Threaded through Options so stateWith and the model see the one answer.
	opts.Dir = ws

	var hooks HookSet
	if wantsHooks(opts) {
		hooks = LoadHookSet()
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
