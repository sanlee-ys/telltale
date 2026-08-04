package council

import (
	"context"
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
	// Resume reopens the room last saved for this workspace: the turn count and
	// each vendor's own session id come back, so the next brief continues the
	// conversation instead of starting four new ones.
	//
	// Composes with --cd, and the workspace is the KEY rather than an extra
	// filter — one saved room per directory. Two rooms open on one directory
	// would be two conversations claiming the same state file, and the second to
	// quit would silently overwrite the first.
	Resume bool
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
	//
	// On Model for the same reason the brief is, and it is the same rule rather
	// than a resemblance: it names a path into the user's private configuration,
	// and only the boolean "is anything wired" crosses onto State.
	hooks HookSet
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
		// A usable room is here and was not asked for. Said once, plainly, and
		// then forgotten: the alternative is that the first dispatch overwrites
		// four vendors' session ids with nothing on screen having mentioned they
		// were there.
		m.st.Notice = "this workspace has a saved room from " +
			age(time.Since(re.Room.SavedAt)) + " (turn " + itoa(re.Room.Turn) +
			") — --resume reattaches to it; dispatching here replaces it"
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
	m.st.Notice = "reattached from " + abbreviate(re.Path, m.st.Home) +
		" — turn " + itoa(re.Room.Turn) + " was the last, " +
		itoa(seats) + "/" + itoa(m.st.Seated()) + " seats restored"
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
	if m.st.Mode == ModeComposing {
		return m.composeKey(msg)
	}
	return m.viewKey(msg)
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
	case "enter":
		return m, m.dispatch()
	case "backspace":
		if d := []rune(m.st.Draft); len(d) > 0 {
			m.setDraft(string(d[:len(d)-1]))
		}
		m.st.Notice = ""
	default:
		if t := msg.Text; t != "" {
			m.setDraft(m.st.Draft + sanitizeKeepingSpace(t))
			m.st.Notice = ""
		}
	}
	return m, nil
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
		m.st.Help = !m.st.Help
	case "i", "enter":
		m.st.Mode = ModeComposing
		m.st.Help = false
		m.st.Notice = ""
	case "tab", "right", "l":
		m.focusBy(1)
	case "shift+tab", "left", "h":
		m.focusBy(-1)
	case "ctrl+r", "r":
		m.toggleQuote()
	case "f":
		// One column at full width. Three columns are for comparing at a
		// glance; one is for actually reading a long reply.
		m.st.Expanded = !m.st.Expanded
	case "up", "k":
		m.scrollBy(-1)
	case "down", "j":
		m.scrollBy(1)
	case "pgup":
		m.scrollBy(-m.pageSize())
	case "pgdown", " ":
		m.scrollBy(m.pageSize())
	case "home", "g":
		m.scrollTo(0)
	case "end", "G":
		m.followFocused()
	}
	return m, nil
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
	lay := resolveLayout(m.st.Width, m.st.Height, len(m.st.Columns), m.st.Expanded)
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
func (m *Model) focusBy(d int) {
	n := len(m.st.Columns)
	if n == 0 {
		return
	}
	m.st.Focus = ((m.st.Focus+d)%n + n) % n
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

	// Same discipline, same reason. --resume against a workspace that has no
	// saved room is a plain error out here, because the usual cause is a --cd
	// pointing at a directory other than the one the room was in — and a room
	// that opened "successfully" with a quiet note would have the user typing
	// their next brief into four fresh sessions believing it continued
	// something. A file that exists but cannot be read is the other case and is
	// handled inside, as a notice: see LoadRoom.
	//
	// Resolved BEFORE the hooks are copied, so a bad --resume fails without
	// having written a temporary file it would then have to clean up.
	ws := resolveWorkspace(opts.Dir)
	var re Reattachment
	if opts.Resume {
		re, err = LoadRoom(ws)
		if err != nil {
			return err
		}
	} else if found, ferr := LoadRoom(ws); ferr == nil && found.Active() {
		// Not reattaching, but there is something here to lose. Carried in as an
		// OFFER rather than a restore: nothing is applied, and the room only
		// mentions it exists before the first dispatch replaces it.
		re = Reattachment{Path: found.Path, Room: found.Room, Offered: true}
	}

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
