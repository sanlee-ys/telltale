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
	// SandboxNone: this column has no read-only posture, and that is MEASURED
	// rather than merely unestablished.
	//
	// Two different routes arrive here, and the badge is the same word because
	// what a reader needs to know is the same in both:
	//
	//   - The flags were passed and DEMONSTRABLY do not restrict the vendor.
	//     Under `--mode plan --sandbox`, Antigravity was asked to write a file
	//     and did: the file landed on disk, its reported permission mode was
	//     byte-identical to a run without the flags, and its tool list still
	//     held write_to_file. A refuted claim, not an unverified one.
	//   - No read-only flag is passed AT ALL, because none of them work. Codex
	//     on Windows: both sandboxed modes fail every process spawn, so the
	//     only mode that runs is `danger-full-access`, and a seat that can read
	//     is worth more than a flag that stops it reading (ADR-008, twelfth
	//     amendment).
	//
	// This level exists because "unverified" turned out to be too generous for
	// either case: rendering `ro:requested` alongside vendors that at least
	// attempt something would imply a posture these columns do not have.
	SandboxNone
	// SandboxWrite: the room can write and no read-only posture
	// was requested at all.
	SandboxWrite
	// SandboxGated: the room can write, and this seat asks before every tool
	// call that changes anything.
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
//
// Detail is the full sentence behind the badge — what was passed, what was
// measured, and what is therefore claimed. It is never abbreviated: the badge
// is two words and the argument for it is a paragraph, and a badge whose
// argument cannot be read is a badge nobody can check.
//
// It renders on the help panel's posture page (view.go, HelpPostures), which
// is where it goes to be READ rather than skimmed. That is newer than this
// field: for several amendments the detail was written, tested, quoted into
// ADR-008 — and rendered nowhere at all, while this comment claimed it was
// "shown in the degraded/help text". §9.2 says a claim you cannot see is not a
// claim; the argument for a claim is under the same rule.
type SandboxClaim struct {
	Level  SandboxLevel
	Detail string
}

// HelpPage is which page of the help panel is open.
//
// Two pages rather than one long panel, because the panel's height budget is
// hard (17 rows, helpKeys) and the two things it has to say are different
// kinds of thing: what the keys do, and what the words on each column MEAN.
// Cramming the second into the first is what left the posture explanation as
// four muted lines below the fold, which is where it was when a user asked
// "why do i care codex and agy are 'unsandboxed'?" — a question the room had
// an answer to and no surface for.
type HelpPage uint8

const (
	// HelpClosed: the panel is not open and the columns are on screen.
	HelpClosed HelpPage = iota
	// HelpKeys: what the keys do.
	HelpKeys
	// HelpPostures: what the badge on each column means, in plain English,
	// followed by this room's own seats and the claim each one is making.
	HelpPostures
)

// next cycles the help panel: closed → keys → postures → closed.
//
// `?` stays one key and stays a toggle-shaped thing, which is the property that
// matters: it is the only documented way OUT of the panel, so it must never
// become a key that can strand a reader on a page. Three presses always return
// the room, from anywhere.
func (h HelpPage) next() HelpPage {
	if h >= HelpPostures {
		return HelpClosed
	}
	return h + 1
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

// maxHistory is how many finished turns one column keeps.
//
// Generous rather than tight, because the whole point of the transcript is that
// the room remembers a conversation and a cap you can reach in an afternoon is
// a room that forgets again. Fifty turns of four seats is a few hundred KB of
// strings in the worst case — cheap against the thing it buys. Nothing here is
// written to disk: the state file (#38) holds session keys and no content, and
// scrollback is not state worth persisting.
const maxHistory = 50

// TurnRecord is one finished turn on one column.
//
// It carries the PROMPT as well as the reply, because a transcript that showed
// only the answers would be half a conversation — and the prompts genuinely
// differ per column: a turn can be routed to two seats and not a third, so what
// this seat was asked is a fact about this seat.
//
// Everything the column header and badge line say about a live turn is copied
// in here too (elapsed, cost, phase), because that chrome only ever describes
// the CURRENT turn. Without the copy, scrolling back would show three replies
// and no way to tell which one took two minutes or which one failed.
type TurnRecord struct {
	// N is the turn number, matching the header's count.
	N int
	// Prompt is the user's brief as this seat received it, already sanitized.
	// Never the fully-expanded text that went down the wire: see Column.Prompt.
	Prompt string
	// Quoted reports that the other seats' answers rode along with this brief.
	Quoted bool

	Body string
	Acts []Act
	// Note is the turn's own card line — the failure reason, the cancellation,
	// the "not addressed" — kept so a past turn that ended badly still says why
	// instead of rendering as a silent one.
	//
	// NoteDetail and NoteCalm ride with it for the same reason the note does: a
	// turn scrolled back to must render as it did when it was live, and a card
	// that lost its body or grew a warning mark on the way into history would be
	// the transcript disagreeing with what the user saw.
	Note       string
	NoteDetail string
	NoteCalm   bool

	Elapsed time.Duration
	// CostUSD and CostSession are the vendor's own figure and what it meant, on
	// the same terms as Column's. A running total that lost its "session" word
	// on the way into history would read as that turn's spend.
	CostUSD     *float64
	CostSession bool

	Phase Phase
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

	// History is the turns this seat has finished, OLDEST FIRST.
	//
	// It exists because dispatching turn N used to erase turn N-1 from the
	// screen, which made the room a per-turn ticker rather than somewhere a
	// conversation lives. A finished turn is pushed here instead of being
	// cleared, and columnText renders the whole thing as one scrollable
	// transcript.
	History []TurnRecord
	// Prompt is the user's brief for the CURRENT turn, as this seat received
	// it, already sanitized. Empty until this seat is dispatched to.
	//
	// The user's brief and NOT the literal bytes that went down the wire, and
	// the difference is deliberate twice over. A first turn is sent with the
	// --brief file prepended, whose content is the user's private file and is
	// held off State on purpose (see Model.brief); a rebuttal turn is sent with
	// the other seats' answers fenced in front of it, which are other vendors'
	// words rather than the principal's. Both are reported instead: Quoted
	// below, and the header's "briefed". What is echoed here is what the user
	// typed, which is the thing the room was failing to show at all.
	Prompt string
	// TurnN is which turn Body, Acts and Prompt belong to. Zero means this seat
	// has never been dispatched to, which is what keeps an unaddressed column's
	// history truthful — it records nothing for a turn it sat out.
	TurnN int
	// Quoted reports that the current turn's brief carried the other seats'
	// previous answers.
	Quoted bool
	// Note carries the one-line reason for Failed/Cancelled, and the
	// explanation on an unavailable column.
	Note string
	// NoteDetail is the note's body: the mechanics, demoted below the title.
	//
	// Empty for most notes, which are one sentence and are the whole story. It
	// exists for the ones where the outcome and the machinery are different
	// facts of different urgency — the restored thread that was let go being the
	// case that earned it, where a single sentence carrying both read as one
	// long alarm about something the user cannot act on.
	NoteDetail string
	// NoteCalm drops the warning mark from the note.
	//
	// It selects the SHAPE, never the words: what a card says is decided where
	// the fact is known, and this only says how loudly to draw it. Set where a
	// note reports an outcome rather than a problem — the same judgement
	// reattachCard already makes by carrying no ⚠ at all — so the mark keeps
	// meaning "something went wrong" on the notes that do.
	NoteCalm bool

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
	// Restored reports that this seat's vendor session id came back from a saved
	// room rather than from a turn dispatched in this process.
	//
	// Per column and not per room, because reattaching is not all-or-nothing: a
	// seat that never answered has no id to save, and a seat added since the
	// room was saved has none either. Both open beside seats that DO have a
	// thread, and a room-level flag would let either be read as continuing a
	// conversation it is not in.
	Restored bool

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

// startTurn moves a column onto a new turn.
//
// The finished turn is PUSHED to history rather than erased, which is the whole
// of PR 4: everything that described the old turn — its reply, its trace, its
// timing, its cost, its card — travels together into one record, so scrolling
// back shows a turn the way it actually ended rather than a body with someone
// else's clock beside it.
//
// Only a column that has actually taken a turn records one. A seat dispatched to
// for the first time has nothing behind it, and a seat left out of a turn never
// reaches here at all.
func (c *Column) startTurn(n int, prompt string, quoted bool) {
	if c.TurnN > 0 {
		c.History = append(c.History, TurnRecord{
			N:           c.TurnN,
			Prompt:      c.Prompt,
			Quoted:      c.Quoted,
			Body:        c.Body,
			Acts:        c.Acts,
			Note:        c.Note,
			NoteDetail:  c.NoteDetail,
			NoteCalm:    c.NoteCalm,
			Elapsed:     c.Elapsed,
			CostUSD:     c.CostUSD,
			CostSession: c.CostSession,
			Phase:       c.Phase,
		})
		if len(c.History) > maxHistory {
			// Oldest first out. A room this long-lived has scrolled past them,
			// and the alternative — dropping the newest — would make the cap
			// look like the transcript had stopped recording.
			c.History = c.History[len(c.History)-maxHistory:]
		}
	}

	c.TurnN = n
	c.Prompt = prompt
	c.Quoted = quoted

	c.Body = ""
	// Nil rather than truncated: the record above now owns that slice, and
	// reusing the array would let the new turn's calls overwrite the old turn's
	// trace in place.
	c.Acts = nil
	c.Note = ""
	c.NoteDetail = ""
	c.NoteCalm = false
	c.CostUSD = nil
	c.CostSession = false
	c.Started = time.Time{}
	c.Elapsed = 0
	// Re-arm the tail for the new turn. The history is still scrollable, but
	// what arrives now is what the user is waiting for.
	c.Follow = true
	c.Scroll = 0
}

// Seats is which columns the room draws and dispatches to.
//
// The zero value is the default and the interesting one: every seat that can
// actually be driven, and none of the ones that cannot. A seat that is not
// installed used to hold a full column of the grid for the whole session —
// 25% of the width on a four-seat machine — to display one card that never
// changes. The card is still reachable; it just no longer costs a quarter of
// every reply to say the same thing.
//
// Both fields come from --vendor, which is an explicit statement about who is
// in the room. It is not the same control as an @mention: mentions route ONE
// turn, this decides who is seated at all, so a seat left out here is not
// drawn and not dispatched to. Anything else would spend a quota on a column
// nobody can see.
type Seats struct {
	// All keeps every detected seat on screen, including the ones that cannot
	// be driven. This is the pre-collapse room, available by typing for it.
	All bool
	// Only, when non-empty, is the exact set asked for — and asking for a seat
	// FORCES it on screen even if it is not installed, because a user who named
	// it is owed the card that says why it is not there.
	Only []model.VendorID
}

// shows reports whether a column is drawn. The raw rule, without the
// everything-collapsed fallback VisibleColumns applies.
func (s State) shows(c Column) bool {
	if s.Seats.All {
		return true
	}
	if len(s.Seats.Only) > 0 {
		for _, v := range s.Seats.Only {
			if v == c.Vendor {
				return true
			}
		}
		return false
	}
	return c.Avail == AvailInstalled
}

// seats reports whether a column takes turns: drivable, and in the room the
// user asked for.
func (s State) seats(c Column) bool {
	return c.Avail == AvailInstalled && s.shows(c)
}

// VisibleColumns is the indices of the columns this room draws, in order.
//
// The fallback matters: a machine with nothing installed collapses to nothing,
// and an empty grid would be a room that says less than the four cards it
// folded away. When every seat is collapsed the cards ARE the content, so they
// all come back.
func (s State) VisibleColumns() []int {
	var vis []int
	for i, c := range s.Columns {
		if s.shows(c) {
			vis = append(vis, i)
		}
	}
	if len(vis) == 0 {
		for i := range s.Columns {
			vis = append(vis, i)
		}
	}
	return vis
}

// CollapsedColumns is the seats that were folded out of the grid.
//
// Never silently: Render turns this into the one line under the header that
// names each of them and why, because a failure that disappears entirely is
// the failure §4a.1 forbids — worse here than a dropped column, since a user
// who never saw the seat has no reason to go looking for it.
func (s State) CollapsedColumns() []int {
	if len(s.Columns) == 0 {
		return nil
	}
	vis := s.VisibleColumns()
	shown := make(map[int]bool, len(vis))
	for _, i := range vis {
		shown[i] = true
	}
	var out []int
	for i := range s.Columns {
		if !shown[i] {
			out = append(out, i)
		}
	}
	return out
}

// CollapseReason says why a seat is not on screen, in the words the notice
// line uses.
func (s State) CollapseReason(c Column) string {
	switch c.Avail {
	case AvailNotInstalled:
		return "not installed"
	case AvailUnusable:
		// The same distinction the unavailable card draws, kept at one line:
		// absence and unusability have different fixes.
		return "installed but not drivable"
	default:
		return "left out by --vendor"
	}
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

// Reattach is what a resumed room says about where it came from.
//
// The zero value is a fresh room, and a fresh room renders exactly as it always
// did — nothing is added to the header, no card appears, and the goldens for
// every other state are unchanged. Reattaching is the exception that announces
// itself; opening normally is not an event.
//
// Like Brief, only the reportable part of the saved room crosses onto State.
// The vendor session ids stay on Model: they are opaque handles to private
// conversations and the renderer has no reason to be able to reach them.
type Reattach struct {
	// Turn is the last turn the saved room completed. Zero means no reattach.
	Turn int
	// SavedAt is when the state file was written. Rendered as an age against
	// State.Now, never against a clock, so Render stays pure and the goldens
	// stay reproducible.
	SavedAt time.Time
}

// Active reports that this room was restored from a saved one.
func (r Reattach) Active() bool { return r.Turn > 0 && !r.SavedAt.IsZero() }

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

	Mode InputMode
	// Draft is the brief being typed. It may contain newlines — ctrl+j puts one
	// there deliberately — so the composer is a block of rows rather than the
	// single elided line it started as.
	Draft string

	// Seats is who is in the room. The zero value collapses the seats that
	// cannot be driven; --vendor is how a user says otherwise.
	Seats Seats

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
	//
	// A reattached room starts at the saved count and CONTINUES from it: the
	// next dispatch is turn N+1, not turn 1. Restarting the count would make
	// the header disagree with the vendors, each of which is replaying a history
	// several turns long, and would quietly reframe a resumed conversation as a
	// new one.
	Turn int

	// Reattached describes the saved room this one was restored from. Zero for a
	// room opened normally.
	Reattached Reattach

	// Notice is a transient one-line message in the footer.
	Notice string

	// Help is which page of the help panel is open, if any.
	Help HelpPage

	// Briefed reports that shared operating context was loaded. The content
	// itself is deliberately NOT on State: it is the user's private file and
	// the renderer has no reason to be able to reach it.
	Briefed bool

	// Write reports that vendors are asked for their widest posture rather than
	// their most read-only one. This is the DEFAULT; --read is the opt-out.
	//
	// Still a session-level property rather than a toggle. What may act on a
	// working tree is decided when the room is opened and is visible in the
	// header for the whole session — not reachable by a keystroke mid-
	// conversation, in either direction. What changed is only which way it
	// defaults, once the approval card started guarding the calls the flag used
	// to guard the room against.
	Write bool

	// FlowHop / FlowSteps / FlowVendor describe where a /flow chain stands, and
	// exist because the room started dispatching without a keystroke.
	//
	// Every other turn here is something the user pressed enter on. A chain
	// advances itself, so between hops the room sends a brief nobody typed to a
	// seat nobody selected — and with three of four columns idle that looks the
	// same as a room acting on its own. FlowSteps == 0 means no chain, which is
	// the ordinary case and renders nothing.
	FlowHop, FlowSteps int
	FlowVendor         model.VendorID

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
	// path — so there is no second layout to keep in sync. Expanded outranks
	// FrameOwners for the frames it is on.
	Expanded bool

	// FrameOwners are the vendors that own column width until the NEXT
	// dispatch. Empty means equal four-up (an @all / everyone turn).
	//
	// Intent controls geometry, activity controls styling: the set is computed
	// once from the route (or the current /flow hop) at dispatch, stays fixed
	// for the whole turn and after completion, and is replaced only by the next
	// enter. Mid-stream resize is forbidden — seats finishing at different
	// times must not reflow neighbours under the reader's eyes. `f` (Expanded)
	// still overrides to one full-width column without clearing this set.
	FrameOwners []model.VendorID

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
//
// Drivable AND in the room: a seat excluded by --vendor is not dispatched to,
// so counting it here would make the header's "3/4 seated" describe a room the
// user does not have.
func (s State) Seated() int {
	n := 0
	for _, c := range s.Columns {
		if s.seats(c) {
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
