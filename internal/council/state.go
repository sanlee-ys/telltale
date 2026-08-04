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

	// CostUSD is this turn's spend AS REPORTED BY THE VENDOR. A pointer, so
	// "reported zero" and "reported nothing" stay distinguishable: council
	// never derives a cost from token counts, which is on this repo's
	// deliberately-rejected list (design.md §8).
	CostUSD *float64
}

// State is everything Render reads. Nothing here is a clock, a file handle or
// an environment lookup — those live on Model, which Render never sees.
type State struct {
	Width, Height int

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
