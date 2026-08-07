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
	// on the days a turn is inexplicably slow. Empty — the default — measures
	// nothing, opens nothing and changes no pixel.
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
	}
	m.st.Briefed = b.Loaded()
	m.reattach(re)
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
	if m.flowWritePending {
		return m.flowWriteGateKey(msg)
	}
	if m.st.Mode == ModeComposing {
		return m.composeKey(msg)
	}
	return m.viewKey(msg)
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

// setDraft changes the brief and re-derives its routing.
//
// Routing is recomputed on every keystroke rather than at dispatch so the
// footer can show it as it is typed. Deleting the "x" from "@codex" has to move
// the indicator back to everyone at the moment it stops being a mention, not
// after enter.
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
		m.st.Expanded = !m.st.Expanded
	case "y":
		// The focused seat's reply. Reachable here only when nothing is gated:
		// key() routes a pending gate to gateKey first, and gateKey answers `y`
		// itself rather than falling through — so the approve key keeps the
		// letter it has always had and yank simply does not exist while a vendor
		// is blocked. That precedence is asserted rather than assumed, because
		// it is the one collision in this keymap where losing would mean a
		// keystroke the user believes approved a tool call quietly copying text
		// instead.
		return m, m.yank(m.st.YankColumn(m.st.Focus))
	case "Y":
		// The whole turn, every seat, labelled. A separate key rather than a
		// modifier on the first because they produce different documents, and
		// shift is what this room already uses for the wider version of a
		// motion (`G` against `g`).
		return m, m.yank(m.st.YankTurn())
	case "k":
		m.scrollBy(-1)
	case "j":
		m.scrollBy(1)
	case " ":
		m.scrollBy(m.pageSize())
	case "home", "g":
		m.scrollTo(0)
	case "end", "G":
		m.followFocused()
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

// openTrace opens the turn-clock file, returning the sink that writes to it and
// the function that closes it.
//
// APPEND, never truncate. The thing being chased is a turn that was slow once,
// so a run that erased the previous run's evidence on open would be the wrong
// tool for its only job.
//
// The writes are serialised here rather than relied on from the runner: seats
// finish independently, each on its own goroutine, and interleaved lines would
// be exactly as useless as no lines.
func openTrace(path string) (runner.Trace, func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, nil, err
	}
	var mu sync.Mutex
	sink := func(c runner.TurnClock) {
		mu.Lock()
		defer mu.Unlock()
		// Best effort, and silent. A full disk must not be able to take a room
		// down; the trace is a diagnostic and the room is the product.
		fmt.Fprintln(f, c)
	}
	return sink, func() {
		mu.Lock()
		defer mu.Unlock()
		_ = f.Close()
	}, nil
}

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

	// Opened before the alternate screen for the same reason the brief is: a
	// path that cannot be written must be a line on stderr, not a card behind a
	// TUI the user has to quit to read.
	if opts.TracePath != "" {
		sink, closeTrace, terr := openTrace(opts.TracePath)
		if terr != nil {
			return terr
		}
		runner.SetTrace(sink)
		defer func() {
			// Uninstalled before the file is closed, so nothing can write into a
			// closed handle on the way out.
			runner.SetTrace(nil)
			closeTrace()
		}()
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
