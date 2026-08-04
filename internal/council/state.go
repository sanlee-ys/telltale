// Package council is the dispatch room: one prompt, broadcast to several vendor
// CLIs at once, their replies streaming side by side.
//
// It is the one telltale subcommand that is not a gauge. `statusline` and `hud`
// read vendor files and never write, never call the network and never send
// anything to a running agent; that guarantee is unchanged and council does not
// live behind any of their keybindings. Council spawns vendor CLIs on purpose,
// says so on screen, and is entered as its own mode (ADR-008).
//
// The house rules from internal/hud carry over unchanged, because they are what
// makes this testable: Render is a pure function over State, State is a plain
// value a test can construct, and every distinction is carried by a glyph or a
// word before it is carried by colour.
package council

import (
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// Phase is what a column is doing right now.
//
// Waiting and Streaming are deliberately separate. They look almost the same to
// a user — nothing has been rendered yet — but they are different claims:
// Streaming means "output is arriving and you are seeing it as it lands", while
// Waiting means "this vendor does not report incremental output, so there is
// nothing to show until it finishes". Collapsing them would be this product's
// own failure mode, a gauge implying knowledge it does not have.
type Phase uint8

const (
	// PhaseIdle: no turn has been dispatched to this column yet.
	PhaseIdle Phase = iota
	// PhaseWaiting: dispatched, running, no incremental output available.
	PhaseWaiting
	// PhaseStreaming: dispatched, running, output arriving.
	PhaseStreaming
	// PhaseDone: the vendor finished this turn cleanly.
	PhaseDone
	// PhaseFailed: the process died, or the vendor reported an error.
	PhaseFailed
	// PhaseCancelled: the user interrupted the turn. Any output already on
	// screen was really produced; the column says it is partial rather than
	// pretending the turn completed.
	PhaseCancelled
)

// Availability is what detection found on PATH. It is LookPath only — council
// never probes a vendor by running a paid turn just to see whether it answers.
type Availability uint8

const (
	// AvailInstalled: a binary was resolved.
	AvailInstalled Availability = iota
	// AvailNotInstalled: nothing on PATH under any known name.
	AvailNotInstalled
	// AvailUnusable: a binary was resolved but council will not drive it — the
	// shim case, where the only entry point is a .cmd/.bat that would put
	// prompt text through cmd.exe. Distinct from NotInstalled because the fix
	// is different and the user deserves to be told which one they have.
	AvailUnusable
)

// SandboxLevel is how much of a read-only posture a vendor's own flags actually
// buy. Three levels, because the honest answer differs per vendor and a single
// blanket claim was the first draft of ADR-008's mistake.
type SandboxLevel uint8

const (
	// SandboxUnknown: no claim (used before detection, and for a column with
	// no vendor).
	SandboxUnknown SandboxLevel = iota
	// SandboxTools: the agent's tool set is restricted so the write tools are
	// not present in the session. Enforced by construction rather than by
	// policy — there is no Write tool to call.
	SandboxTools
	// SandboxEnforced: the vendor applies an OS-level sandbox.
	SandboxEnforced
	// SandboxRequested: the flag was passed and the vendor accepted it, but
	// what it enforces on this platform is not established. The badge says so.
	SandboxRequested
	// SandboxNone: the vendor's read-only flags were passed and DEMONSTRABLY do
	// not restrict it.
	//
	// This level exists because "unverified" turned out to be too generous for
	// a real vendor. Under `--mode plan --sandbox`, Antigravity was asked to
	// write a file and did: the file landed on disk, its reported permission
	// mode was byte-identical to a run without the flags, and its tool list
	// still held write_to_file. That is not an unestablished claim, it is a
	// refuted one, and rendering it as `ro:requested` alongside two vendors
	// that at least attempt something would imply a posture this vendor does
	// not have.
	SandboxNone
	// SandboxWrite: the room was started with --write and no read-only posture
	// was requested at all.
	SandboxWrite
	// SandboxGated: the room was started with --write, and this seat asks
	// before every tool call that changes anything.
	//
	// Not a level of read-only. The seat may do everything SandboxWrite allows;
	// what differs is that it has to be told yes first. It renders as its own
	// word rather than a qualifier on WRITES because the room header already
	// carries the persistent WRITE marker — so "gated" is read in a context
	// that has already said this room can write, and cannot be mistaken for a
	// column that cannot.
	SandboxGated
)

// SandboxClaim is one column's posture, as a claim we are willing to defend.
// Detail is shown in the degraded/help text, never abbreviated away.
type SandboxClaim struct {
	Level  SandboxLevel
	Detail string
}

// Granularity is how finely a vendor reports progress. Rendered in the column
// header so a column that cannot stream is never mistaken for one that is
// streaming slowly.
type Granularity uint8

const (
	// GranUnknown: not established yet.
	GranUnknown Granularity = iota
	// GranTokens: token-level deltas.
	GranTokens
	// GranEvents: coarser progress — whole messages or steps.
	GranEvents
	// GranFinalOnly: nothing until the result.
	GranFinalOnly
)

// InputMode is which of the two modes the room is in. The footer always
// announces it: a mode that changes what an unmodified key means without
// saying so is how a TUI surprises someone (design.md §7.8).
type InputMode uint8

const (
	// ModeComposing: keys are text. `q` is the letter q.
	ModeComposing InputMode = iota
	// ModeViewing: keys are commands.
	ModeViewing
)

// Act is one thing a vendor did this turn, and what is known about how it went.
//
// A struct rather than the plain string it started as, because a command trace
// without outcomes is a half-built gauge: it shows that the vendor ran the
// tests and says nothing about whether they passed, while the answer was
// sitting unparsed in the same stream the command came from.
//
// The status vocabulary lives in runner rather than here on purpose. Only an
// adapter can know an outcome — it is a reading of a vendor's own words — and a
// second enum on this side would be a mapping table that drifts.
type Act struct {
	// ID is the vendor's own id for the call, held only so a result arriving
	// later finds the entry it belongs to. Never rendered.
	ID string
	// Text is the call as it will be shown: "Bash: go test ./...". Already
	// redacted and sanitized, like everything else that reaches State.
	Text string
	// Status is what the vendor said about the outcome. runner.ActUnknown is a
	// real and common value, not a placeholder — see its doc comment.
	Status runner.ActStatus
	// Detail is the vendor's own first line about a failure, already redacted.
	// Empty whenever the vendor gave none, which is most of the time.
	Detail string
}

// Column is one vendor's seat at the table.
type Column struct {
	Vendor model.VendorID
	// Label is the display name ("Claude Code", "Codex"). Distinct from Vendor,
	// which is the stable lowercase id the footer and flags use.
	Label string

	Avail Availability
	// Binary is the resolved path, shown in the unavailable card so the user
	// can see WHICH thing was found when detection surprises them.
	Binary string

	Sandbox SandboxClaim
	Gran    Granularity

	Phase Phase
	// Body is the vendor's text for the current turn, already redacted and
	// sanitized. Held as a plain string: Render must be able to run over a
	// State a test typed out by hand.
	Body string
	// Note carries the one-line reason for Failed/Cancelled, and the
	// explanation on an unavailable column.
	Note string

	// Acts is what this vendor DID this turn: tool calls, shell commands, file
	// edits, in order — and what became of each one.
	//
	// Kept separate from Body rather than interleaved into it, because they are
	// different kinds of claim. Body is what the vendor said; Acts is what it
	// did. Concatenating them would let a tool name read as part of an answer,
	// which is the same category error as rendering a quoted reply as the
	// vendor's own words.
	Acts []Act

	// Started is when this column's current turn was dispatched. Zero when it
	// has never run.
	Started time.Time
	// Elapsed is how long the LAST completed turn took, kept after the turn
	// ends so a finished column can still say how long it made you wait.
	Elapsed time.Duration

	// Scroll is the first visible body line. Only consulted when Follow is
	// false — a column that is tailing derives its offset from the content, so
	// the two can never disagree about where the bottom is.
	Scroll int
	// Follow pins the column to the newest output.
	//
	// True by default and reset to true on dispatch, because during a turn the
	// interesting line is the one arriving. It goes false the moment the user
	// scrolls up: yanking someone back to the bottom while they are reading is
	// the single most irritating thing a streaming pane can do, and it also
	// hides content, which is the failure this whole surface is built to avoid.
	Follow bool

	// CostUSD is the spend AS REPORTED BY THE VENDOR. A pointer, so "reported
	// zero" and "reported nothing" stay distinguishable: council never derives a
	// cost from token counts, which is on this repo's deliberately-rejected list
	// (design.md §8).
	CostUSD *float64
	// CostSession reports that the figure above is the PROCESS's running total
	// rather than this turn's spend.
	//
	// It exists because keeping one process alive changed what the vendor's own
	// number means. Measured across two turns of one Claude process: the
	// reported total went $0.1061493 -> $0.1177296 while the per-turn usage
	// block stayed at 2 input tokens both times. Rendering a running total in a
	// cell that has always meant "this turn" would be a false reading of a true
	// number, and subtracting to recover the turn would be council inventing a
	// figure — so the badge names which one it is and neither happens.
	CostSession bool
}

// PendingGate is one tool call a vendor is blocked on, waiting to be told yes
// or no.
//
// A plain value on State like everything else here, so the approval card and
// the mode line can be rendered by a test that types one out by hand. The text
// is already redacted: it is vendor-authored and a shell command is one of the
// likeliest places for a token to appear on screen.
type PendingGate struct {
	Vendor model.VendorID
	// RequestID is the vendor's own id for the request. Never rendered; it is
	// what the answer carries back.
	RequestID string
	// ToolUseID ties the decision to the trace entry it decides.
	ToolUseID string
	// Text is the call as it will be shown, formatted exactly like a trace
	// entry: "Write: ~/ws/ping.txt".
	Text string
}

// State is everything Render reads. Nothing here is a clock, a file handle or
// an environment lookup — those live on Model, which Render never sees.
type State struct {
	Width, Height int

	// Now is stamped when a tick arrives, never read from the clock inside
	// Render. That is what keeps the renderer pure and its goldens
	// reproducible — the same discipline internal/hud uses for ages.
	Now time.Time

	// Workspace is the directory turns are dispatched against, already resolved
	// to an absolute path.
	Workspace string
	// Home is used to abbreviate Workspace for display only.
	Home string

	Columns []Column
	// Focus indexes Columns. It is the tab selection at narrow widths and the
	// emphasis at wide ones.
	Focus int

	Mode  InputMode
	Draft string

	// Quote arms cross-agent rebuttal for the NEXT dispatch: each vendor
	// receives the others' previous answers as fenced, labelled material.
	//
	// Per-turn and off by default, deliberately. It is the one control here
	// that puts one model's output into another model's input, which is a
	// prompt-injection path; the blast radius of a hostile reply should cost a
	// keystroke rather than be inherited from a mode set once and forgotten.
	// Turn 1 is always blind regardless (ADR-008 §4) — independent opinions are
	// the reason the room exists.
	Quote bool

	// Route is who the CURRENT draft is addressed to, re-derived on every
	// keystroke. Nil means everyone seated.
	//
	// It lives on State rather than being computed inside Render because the
	// footer has to show it while typing: the point of showing routing is that
	// an @typo reads as "this is going to everyone" BEFORE enter, and a value
	// the renderer derived privately could drift from the one dispatch uses.
	// One parse, one source of truth, displayed and acted on.
	Route Route

	// Turn counts dispatched turns, so the header can say which round this is.
	// Turn 0 means nothing has been sent.
	Turn int

	// Notice is a transient one-line message in the footer.
	Notice string

	Help bool

	// Briefed reports that shared operating context was loaded. The content
	// itself is deliberately NOT on State: it is the user's private file and
	// the renderer has no reason to be able to reach it.
	Briefed bool

	// Write reports that this room was started with --write: vendors are asked
	// for their widest posture rather than their most read-only one.
	//
	// A session-level flag rather than a toggle, on purpose. Widening what
	// three agents may do to a working tree is a decision about how the room
	// was OPENED, and one that should be visible in the command the user typed
	// and in the header for the whole session — not something reachable by a
	// keystroke mid-conversation.
	Write bool

	// Gates are the tool calls waiting on a decision, OLDEST FIRST.
	//
	// A queue rather than a single value, because one assistant message really
	// can ask for a parallel batch of calls and each one blocks separately.
	// Answering them in arrival order is the only ordering a person can follow:
	// the card names the call it is asking about, and a queue that reordered
	// itself would move the card under the keystroke.
	//
	// Its emptiness is the single source of truth for whether the room is
	// gating — the mode line and the keymap both read it, so they cannot
	// disagree about what `y` means.
	Gates []PendingGate

	// Expanded gives the focused column the whole width.
	//
	// Three columns are for comparing at a glance; one is for actually reading
	// a long reply. Both are the same renderer — expanding reuses the tabbed
	// path — so there is no second layout to keep in sync.
	Expanded bool

	// Spinner advances only while something is genuinely in flight.
	Spinner int

	// ASCII mirrors the glyph set, so Render can pick a different affordance
	// where a straight substitution does not work.
	ASCII bool
}

// NewState is the empty room.
func NewState() State {
	return State{Mode: ModeComposing, Focus: 0}
}

// Busy reports whether any column is still working. Drives the spinner and the
// meaning of ctrl+c.
func (s State) Busy() bool {
	for _, c := range s.Columns {
		if c.Phase == PhaseWaiting || c.Phase == PhaseStreaming {
			return true
		}
	}
	return false
}

// Gating reports that a vendor is blocked on a decision.
//
// Derived from the queue rather than stored beside it. A third InputMode would
// have been a second place for the same fact to live, and the two would drift
// the first time a gate was answered on a path that forgot to reset it — at
// which point `y` would still mean approve while the footer said something
// else, which is the exact surprise §7.8 forbids.
func (s State) Gating() bool { return len(s.Gates) > 0 }

// Seated reports the columns council will actually dispatch to.
func (s State) Seated() int {
	n := 0
	for _, c := range s.Columns {
		if c.Avail == AvailInstalled {
			n++
		}
	}
	return n
}

// String renders a phase as the word shown in a column header. Present tense
// for live states, past for terminal ones.
func (p Phase) String() string {
	switch p {
	case PhaseWaiting:
		return "waiting"
	case PhaseStreaming:
		return "streaming"
	case PhaseDone:
		return "done"
	case PhaseFailed:
		return "failed"
	case PhaseCancelled:
		return "cancelled"
	default:
		return "idle"
	}
}

// Badge is the short read-only claim rendered in a column header.
//
// There is no level that renders as an unqualified "read-only": the two that
// are actually enforced name their mechanism, and the one that is not says
// "requested" out loud.
func (c SandboxClaim) Badge() string {
	switch c.Level {
	case SandboxTools:
		return "ro:tools"
	case SandboxEnforced:
		return "ro:enforced"
	case SandboxRequested:
		return "ro:requested"
	case SandboxWrite:
		// Shouted, and without an "ro:" prefix for the same reason SandboxNone
		// drops it: the prefix is what a hurried reader takes in first.
		return "WRITES"
	case SandboxGated:
		return "gated"
	case SandboxNone:
		// Deliberately not spelled with an "ro:" prefix. Every other badge
		// begins that way, and a reader scanning three column headers reads the
		// prefix before the qualifier — so "ro:none" would land as a read-only
		// posture at a glance. This vendor has none, and the word has to break
		// the pattern to say so.
		return "unsandboxed"
	default:
		return ""
	}
}

// String is the granularity word for the column header.
func (g Granularity) String() string {
	switch g {
	case GranTokens:
		return "tokens"
	case GranEvents:
		return "events"
	case GranFinalOnly:
		return "final only"
	default:
		return ""
	}
}
