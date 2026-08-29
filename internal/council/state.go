// Package council is the dispatch room: one prompt, broadcast to several vendor
// CLIs at once, their replies streaming side by side.
//
// It is the one telltale subcommand that is not a gauge. `statusline` and `hud`
// read vendor files, never call the network and never send anything to a
// running agent (their one write is the statusline's numbers-only quota relay,
// design.md §7.15); that guarantee is unchanged and council does not live
// behind any of their keybindings. Council spawns vendor CLIs on purpose,
// says so on screen, and is entered as its own mode (ADR-008).
//
// The house rules from internal/hud carry over unchanged, because they are what
// makes this testable: Render is a pure function over State, State is a plain
// value a test can construct, and every distinction is carried by a glyph or a
// word before it is carried by colour.
package council

import (
	"sort"
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
// hard (16 rows, helpKeys) and the two things it has to say are different
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
	// GateWait is the operator's own share of this turn, on the same terms as
	// Column's: wall clock over the seat, unmeasured when no card was ever
	// raised. A record that dropped it would show the turn's whole wall time
	// under a word that means the vendor was working (§9.45).
	GateWait runner.Span
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
	// Settling reports that this seat has ANSWERED but its process has not
	// exited yet — the column is terminal, the turn is not.
	//
	// It exists for one vendor's measured behaviour and is written to name the
	// general case rather than that vendor. Codex ends a turn with a
	// `turn.completed` line and then lingers seconds before exiting (4.06s and
	// 4.25s measured 2026-08-16 on codex-cli 0.147.0; 7.94s in §9.33 on the same
	// build). The column settles on the line, because a seat that has delivered
	// its answer is not working; the TURN cannot end until the process is gone,
	// because the turn's context is what kills the child and the process may
	// still be doing vendor-side bookkeeping this repo has not measured.
	//
	// Rendering it is not decoration. Between those two moments the room has no
	// spinner and every column reads `done`, which is the picture of a finished
	// turn — while the composer is still locked and `q` is still refused. One
	// word on the column is what keeps that from being a room that has silently
	// stopped responding (§7.8).
	//
	// Cleared by finishColumn, which is the one place every retirement passes
	// through, and by startTurn, so a seat cannot carry it into a turn it is
	// answering fresh.
	Settling bool
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
	// Skipped reports that Note describes a turn this seat SAT OUT rather than
	// a turn it took.
	//
	// It is one bit and it does two things, and both are the same rule: a skip
	// is not a fact about any turn this column recorded.
	//
	//   - startTurn does not carry the note into the TurnRecord it files.
	//     Without that, a seat that answered turn 1 and then sat out 2 through 7
	//     filed turn 1's record wearing "not addressed in turn 7", so the
	//     transcript showed a turn that succeeded with someone else's absence
	//     stapled underneath it.
	//   - the renderer draws it as a skip rather than as a note (see skipLine):
	//     muted, led by the idle mark, never by ⚠. Post-#99 the default route is
	//     one seat, so three columns sit a turn out every ordinary turn — that is
	//     the room working, and a warning glyph spent on it is a warning glyph
	//     the eye stops trusting (§9.19).
	//
	// What the transcript says about the turns in BETWEEN is derived from the
	// gaps in History rather than stored, because that is the only place the
	// fact was ever measured — see skipSpan.
	Skipped bool

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
	//
	// WALL CLOCK, and the operator's own share is still inside it. What the
	// room draws is that share taken back out (vendorElapsed), because a figure
	// labelled `streaming` has to be time the vendor spent streaming — see
	// GateWait.
	Elapsed time.Duration

	// GateWait is how long the OPERATOR held this seat during the current turn,
	// counting only the stretches that have already ended (§9.45).
	//
	// A seat behind an approval card is stopped: the vendor is not thinking and
	// not writing, it is waiting to be told yes or no. Folding that into Elapsed
	// made a column read `⋮ streaming 5m` while the whole five minutes was a
	// person deciding — a stopped seat rendered as a moving one, which is the
	// failure TestWaitingIsNotStreaming's family exists to catch.
	//
	// WALL CLOCK OVER THE SEAT, never a sum over cards. One assistant message can
	// raise a parallel batch of requests, and a seat held by three cards at once
	// is stopped once; adding the three would charge the operator three times for
	// one stretch. The stopwatch therefore runs from the FIRST card this seat had
	// up to the moment its LAST one is answered — see gateStoppedAt.
	//
	// A runner.Span rather than a duration, for the field's own zero-vs-absent
	// rule (§4a.1): a turn with no card at all is UNMEASURED and renders no
	// operator figure, while a card answered inside a second is a measured zero
	// and renders `0s`. Those are different facts and this room does not draw
	// them alike. The type is runner's because the argument is already written
	// there, and one vocabulary for one distinction is the point.
	//
	// Reset by startTurn like every other per-turn fact, after the record it
	// belongs to is filed.
	GateWait runner.Span

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

	// Cleared reports that this seat's thread was dropped from inside the room
	// (design.md §9.17) and its next brief opens a new session.
	//
	// A field of its own rather than reusing !Restored, because "this seat never
	// had a thread" and "this seat had one and you ended it" are different facts
	// about the same empty slot — the zero-vs-absent rule (§4a.1) applied to a
	// conversation instead of a gauge. The transcript above it is NOT erased: the
	// turns happened, and a room that blanked them would be destroying the
	// reading surface to report a vendor-side change. What is gone is the thread
	// the next brief would have continued, and that is what the marker says.
	//
	// Cleared by the next dispatch, in resetForTurn: once the new session exists
	// the statement has stopped being true, and a marker that outlived it would
	// be claiming a break in a conversation that has since resumed.
	Cleared bool

	// Arena is this seat's race outcome for the CURRENT turn (arena.go), nil on
	// every ordinary one. On State because the columns render it; every field is
	// computed at collection time in finishColumn, so Render stays pure. Reset by
	// startTurn like every other per-turn fact — the worktree it points at
	// outlives the marker on purpose (kept until the user deletes it), the
	// on-screen block does not.
	Arena *ArenaResult
	// ArenaShowDiff flips the arena block from the stat to the full patch (`d`).
	// Per column: comparing seat A's stat against seat B's whole diff is a
	// legitimate way to read a race, so one seat's toggle must not drag the
	// others'.
	ArenaShowDiff bool
	// ArenaInterim is the latest MID-RACE stat read for the current turn's
	// attempt (arenalive.go), nil until a read has returned. Nil is absence
	// and renders nothing — "no read yet" is not a zero (§4a.1). Each read
	// replaces it wholesale, and finishColumn clears it the moment the
	// authoritative ArenaResult lands: the final replaces the interim, never
	// merges with it. Like Arena, every field is computed off the Update loop
	// (the read runs as a Cmd), so Render stays pure over State.
	ArenaInterim *ArenaInterim

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

	// Quota is this seat's relayed ACCOUNT quota reading (quota.go), or nil
	// when the relay has nothing to say about this vendor.
	//
	// A pointer, on CostUSD's own argument one field up: a vendor at 0% of its
	// window and a vendor with no reading at all are different facts, and a
	// value field could not tell them apart. Nil renders nothing anywhere — no
	// dash, no placeholder — which is what Cursor and grok render forever,
	// because neither writes quota to disk in any form telltale can read
	// (§7.17).
	//
	// NOT a per-turn fact, so startTurn deliberately does not file it into a
	// TurnRecord and resetForTurn does not clear it. Everything else on this
	// struct that startTurn moves describes a turn; this describes the account
	// behind the seat, and it is replaced only by the next read of the relay.
	// Its age is measured from SeatQuota.WrittenAt against State.Now, so Render
	// stays pure over State — the read itself runs as a Cmd.
	Quota *SeatQuota
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
		rec := TurnRecord{
			N:           c.TurnN,
			Prompt:      c.Prompt,
			Quoted:      c.Quoted,
			Body:        c.Body,
			Acts:        c.Acts,
			Note:        c.Note,
			NoteDetail:  c.NoteDetail,
			NoteCalm:    c.NoteCalm,
			Elapsed:     c.Elapsed,
			GateWait:    c.GateWait,
			CostUSD:     c.CostUSD,
			CostSession: c.CostSession,
			Phase:       c.Phase,
		}
		if c.Skipped {
			// The note on the column is about a turn this seat sat out, which is
			// a LATER turn than the one being filed here. Carrying it would put
			// "not addressed in turn 7" under turn 1's separator — a record of a
			// turn that happened, wearing the absence of one that did not. The
			// skipped turns are still reported; they are derived from the gap
			// between this record and the next (view.go, skipSpan).
			rec.Note, rec.NoteDetail, rec.NoteCalm = "", "", false
		}
		c.History = append(c.History, rec)
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
	// A seat dispatched to again is working, whatever its last process was still
	// doing when the room moved on.
	c.Settling = false
	// This seat is in the turn, so whatever it sat out is behind it now.
	c.Skipped = false
	// The marker retires the moment the brief it was warning about is sent: from
	// here on this seat HAS a thread again, and a "thread cleared" line left
	// standing would describe a break the room has already healed.
	c.Cleared = false
	c.Arena = nil
	c.ArenaInterim = nil
	c.ArenaShowDiff = false
	c.CostUSD = nil
	c.CostSession = false
	c.Started = time.Time{}
	c.Elapsed = 0
	// Back to UNMEASURED, not to zero. The record above owns the old turn's
	// figure, and a new turn that has raised no card has not made the operator
	// wait for nothing — it has not measured a wait at all (§9.45).
	c.GateWait = runner.Span{}
	// Re-arm the tail for the new turn. The history is still scrollable, but
	// what arrives now is what the user is waiting for.
	c.Follow = true
	c.Scroll = 0
}

// TurnView is the by-turn projection of the transcript: which turn is on
// screen, and where that one page is scrolled (design.md §9.22).
//
// View state and only view state. It is never written to the room file —
// room.json stays keys-only (ADR-008, ninth amendment) — for the same reason
// §9.9 refuses to persist the scrollback: which turn a reader happened to be
// looking at is not a fact the next session should inherit, and a projection
// restored from disk is a room that opens somewhere nobody asked for.
//
// Turn is a turn NUMBER and never an index into History or into PageTurns. That
// is the coordinate every other surface in this room already prints — the turn
// separators, the overflow markers, the yank notice — and an index would be a
// second vocabulary for one fact, in the one place a reader is meant to be able
// to read a number off the footer and type it back into a conversation.
type TurnView struct {
	// Open reports that the body draws one turn across every seat rather than
	// the by-seat grid.
	Open bool

	// Turn is the turn on screen, and it deliberately does NOT track
	// State.Turn.
	//
	// A turn arriving while an older page is open must not move the view (§7.1
	// rule 4): the room is a reading surface, and content that jumps out from
	// under a reader because a vendor finished is the thing the whole
	// bottom-anchor / no-mid-stream-reflow discipline exists to prevent. The
	// drift that creates is carried by the mode word instead — "TURN 10/11" —
	// which is where a reader already looks to find out what the keys mean.
	Turn int

	// Scroll and Follow are this page's position, on exactly the terms a
	// column's are.
	//
	// Not a second scroll model: a page is a flat list of lines, which is what
	// §9.9 already argued a transcript is, so the window, the overflow markers,
	// the tail and the clamp are the code that was here — pointed at a
	// different list. Follow goes false the moment the reader scrolls up and G
	// restores it, which is the grid's rule unchanged.
	Scroll int
	Follow bool

	// Ledger selects the page's OTHER FACE: what the seats did in this turn
	// rather than what they said (design.md §9.22, amended 2026-08-17).
	//
	// A face on the existing projection rather than a third projection beside it,
	// and the argument is the one §9.22 made for the page itself: the turn, its
	// participants and its brief are already decided by turnEntries, so a second
	// TurnView would be a second answer to "which turn is on screen" and a second
	// scroll model to keep in step with it. What changes is which of one turn's
	// two records is drawn — the acts or the replies — so the coordinate stays
	// exactly where it was and `[`, `]`, `g` and `G` move it unchanged.
	//
	// It is never persisted, for TurnView's own reason, and it is never moved by
	// anything but the two keys: openPage leaves it alone, so a dispatch and a
	// turn hop keep the face the reader chose.
	Ledger bool
}

// PageTurns is every turn a page can be opened on, oldest first.
//
// Derived from the records themselves rather than counted off State.Turn, and
// the difference is the fifty-turn cap (§9.9): a room on turn 214 whose early
// records were evicted has nothing to draw for turn 3, and offering a page for
// it would be the room inventing a conversation — §9.19's error about absence,
// run one surface up.
//
// A turn is listed when ANY seat on screen recorded it, because a turn routed
// to one seat is still a turn — that is the whole of the post-#99 room, where
// the ordinary brief reaches Claude alone. The live turn is included through
// Column.TurnN, which dispatch sets on every seat the route addressed, so a page
// exists for a turn from the instant it is sent rather than from the instant it
// lands.
func (s State) PageTurns() []int {
	seen := map[int]bool{}
	var out []int
	add := func(n int) {
		if n > 0 && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for _, idx := range s.VisibleColumns() {
		c := s.Columns[idx]
		if c.Avail != AvailInstalled {
			// A seat that cannot be driven has never been asked anything, so it
			// contributes no turn — the same answer columnLines gives its hop
			// keys (§9.20).
			continue
		}
		for _, h := range c.History {
			add(h.N)
		}
		add(c.TurnN)
	}
	sort.Ints(out)
	return out
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
// Since §9.32 this is also SHAPE the room saves: room.json carries the roster
// verbatim, this struct marshalled into the `seats` field. Both members are
// `omitempty` so the default room is `{}` in a file a user is meant to be able
// to read, and so an OLDER file with no roster at all decodes to this exact
// zero value — which is why a room.json written before §9.32 goes on meaning
// what it always meant. The json tags live on the render type rather than on a
// parallel struct in resume.go deliberately: a roster is a roster, and a second
// shape to translate between would be the thing that drifts.
type Seats struct {
	// All keeps every detected seat on screen, including the ones that cannot
	// be driven. This is the pre-collapse room, available by typing for it.
	All bool `json:"all,omitempty"`
	// Only, when non-empty, is the exact set asked for — and asking for a seat
	// FORCES it on screen even if it is not installed, because a user who named
	// it is owed the card that says why it is not there.
	Only []model.VendorID `json:"only,omitempty"`
}

// typed reports that this roster was ASKED for — by `--vendor` at the door or
// by `/seat` inside — rather than being the default detected table.
//
// The zero value is the default room, which is what makes this the same test
// `--cd` applies to `opts.Dir != ""` (§9.32): an explicit control someone typed
// today outranks a file from yesterday, and nothing typed leaves the file in
// charge.
func (s Seats) typed() bool { return s.All || len(s.Only) > 0 }

// sameSeats compares two rosters. Seats holds a slice, so `==` does not compile
// and a roster change cannot be detected by assignment — which is exactly what
// roomCommand's save choke point needs to ask (§9.32).
//
// ORDER-SENSITIVE, on purpose. `/seat claude,codex` and `/seat codex,claude`
// produce different Only orders, and that order is what the grid draws, so
// treating them as one roster would be calling two different rooms the same.
func sameSeats(a, b Seats) bool {
	if a.All != b.All || len(a.Only) != len(b.Only) {
		return false
	}
	for i := range a.Only {
		if a.Only[i] != b.Only[i] {
			return false
		}
	}
	return true
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

// SeatNumber is the key that focuses this seat: its one-based position among the
// VISIBLE columns, or 0 for a seat that is not on screen (§9.29).
//
// **Positional, exactly like the columns are.** It is not an identity — a seat
// does not own its number, it owns the place it is sitting in — which is why a
// room with two seats has keys 1 and 2 and nothing else, and why the numbers
// RENUMBER when a seat folds out. That renumbering happens only at events which
// already reflow the whole frame (a seat collapsing, `--vendor` reseating the
// room), never mid-turn otherwise, so §7.1 rule 4's still-by-default frame is
// intact: the room is not quietly relabelling itself while a reply lands.
//
// Matched on the vendor rather than carried as a parameter through five
// builders, on framePrimary's own precedent: a seat is identified by its vendor
// everywhere else in this package, one seat per vendor, and threading a position
// through columnCell → columnChrome → columnHeader → stripHeader would put four
// copies of one fact in flight.
// Zero in a room with ONE seat on screen, and that is §9.11's rule rather than
// a special case: `f` and `tab` are dropped outright there because they address
// a choice that does not exist, and a number labelling the only column there is
// is the same cell spent on the same nothing. The mode line drops its `1-N seat`
// cell on exactly this predicate, so the key and its label appear and vanish
// together — a footer that named a key the header did not would be §7.8's
// surprise split across two rows.
func (s State) SeatNumber(c Column) int {
	vis := s.VisibleColumns()
	if len(vis) < 2 {
		return 0
	}
	for j, i := range vis {
		if s.Columns[i].Vendor == c.Vendor {
			return j + 1
		}
	}
	return 0
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

	// StoppedAt is when this SEAT stopped — not when this card was raised
	// (§9.45).
	//
	// The two differ, and the difference is the whole reason the field is named
	// for the seat. One assistant message can ask for a parallel batch, each
	// call blocks separately, and a vendor already stopped does not stop again
	// when its second card goes up. So the FIRST card of a stretch is stamped
	// with the moment it was raised and every later card in the same stretch
	// INHERITS that stamp — which is what keeps the figure on screen from
	// jumping backwards when the first of two cards is answered, and what keeps
	// the room from charging one wait twice.
	//
	// Stamped by queueGate, which is the one place a card is raised, and NEVER
	// by Render: the renderer reads it against State.Now, exactly as it reads
	// Reattach.SavedAt, so two renders of one State stay identical.
	//
	// A gate that is auto-approved never reaches this field. autoApproveRoutine,
	// isReadOnlyTool and an ungated room all answer inside queueGate without a
	// card, and no human read anything — charging that to the operator would be
	// the room inventing a wait (§4a.1).
	//
	// Zero is legal and means "not stamped": every State a test types out by
	// hand carries no timestamp, and the operator's figure is then absent rather
	// than zero, which is the same distinction Column.CostUSD draws.
	StoppedAt time.Time

	// Old and New are the before and after of a structured file edit, measured
	// off the vendor's own permission payload and already redacted (§9.41).
	//
	// This is the ONE projection of runner.Gate.Input that crosses onto State,
	// and it is two named strings rather than the blob for exactly the reason
	// the blob is kept off this side: a whole argument map on the renderer's
	// side of the boundary is a Write's entire file content one careless line
	// away from the screen. Two fields with one purpose cannot be reached for
	// by accident.
	//
	// A PAIR or nothing. The adapter fills both or neither, so the renderer's
	// test is simply whether they DIFFER — an edit whose payload carried only
	// the new half, or no halves at all (every non-Edit call, and every call on
	// the Cursor seat), leaves both empty, reads as identical, and draws no
	// preview. Nothing here is ever read from disk or reconstructed: §4a.1's
	// rule is that a field nothing sourced is absent, and a guessed diff on the
	// one card in this room that guards a write would be the worst place in the
	// product to invent something.
	Old, New string
}

// HasPreview reports whether this request carried enough to draw a before/after.
//
// The whole test is that the two halves DIFFER, which folds three separate
// "nothing to show" cases into one honest answer: no payload halves at all
// (both empty), and an edit that would change nothing (both equal). It is a
// method rather than a field so there is no second copy of the rule to drift —
// the card, the layout budget and the tests all ask this one question.
func (p PendingGate) HasPreview() bool { return p.Old != p.New }

// gateStoppedAt is when this seat's CURRENT stopped stretch began: the oldest
// stamp among the cards it still has up (§9.45).
//
// It is a query over the queue rather than a field on the seat, and the queue
// can answer it because PendingGate.StoppedAt is a fact about the seat that
// later cards inherit — so the answer does not move when one card of several is
// taken away. The oldest is taken anyway: it costs nothing, and it is right by
// construction rather than by trusting every writer to have inherited correctly.
//
// A card with no stamp is skipped rather than treated as the epoch. Every State
// a test types out by hand is unstamped, and an unstamped card that counted as
// "stopped in 1970" would put a fifty-year wait on the screen — the invented
// figure §4a.1 forbids, arrived at by arithmetic on an absence.
//
// Reports false when this seat has nothing up, which is what makes the caller's
// "has the LAST card gone" test a single call rather than a bookkeeping flag.
func (s State) gateStoppedAt(v model.VendorID) (time.Time, bool) {
	var first time.Time
	for _, p := range s.Gates {
		if p.Vendor != v || p.StoppedAt.IsZero() {
			continue
		}
		if first.IsZero() || p.StoppedAt.Before(first) {
			first = p.StoppedAt
		}
	}
	return first, !first.IsZero()
}

// gateStopped reports whether the room is stopped on the operator for this seat:
// the queue holds at least one card nobody has answered.
//
// The companion to gateStoppedAt and deliberately NOT the same question. That one
// asks WHEN the stretch began, so it skips an unstamped card — a duration derived
// from an absence is the invented figure §4a.1 forbids. This one asks WHETHER the
// seat is stopped, which is a fact the card's existence establishes on its own, so
// a card with no stamp still answers yes. Every State this package's tests type out
// by hand is unstamped, and each of them draws the approval card; a predicate that
// called those seats unblocked would have the header contradicting the card two
// rows under it.
//
// It walks the queue rather than reading a flag on the column for needsYou's
// reason: the queue is the only thing that knows a vendor is waiting on a
// keystroke, and a second copy of that fact drifts the first time a card is
// answered while another is still up.
func (s State) gateStopped(v model.VendorID) bool {
	for _, p := range s.Gates {
		if p.Vendor == v {
			return true
		}
	}
	return false
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
	// keystroke. The zero Route means explicit @all; NewState starts with the
	// control-plane default so an empty first draft is not displayed as a
	// broadcast before the first keystroke.
	//
	// It lives on State rather than being computed inside Render because the
	// footer has to show it while typing: the point of showing routing is that
	// an @typo reads as "this is going to claude" BEFORE enter, and a value
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

	// Page is the by-turn projection: `t` swaps the body between the grid and
	// one turn read across every seat (§9.22).
	//
	// It sits on State because Render has to draw it and Render is pure over
	// State — the same reason Focus and Expanded are here. What is NOT here is
	// any part of it reaching room.json: see TurnView.
	Page TurnView

	// Record is the room's arena record when `/arena record` has opened it, nil
	// otherwise (§9.47, record.go).
	//
	// A POINTER holding a completed read, never a flag the renderer would have to
	// fill in. Every count on it is tallied in the command handler, off two
	// `git for-each-ref` calls, so Render stays pure over State exactly as it does
	// for ArenaResult — a body whose content came from a subprocess inside Render
	// would make the goldens depend on the repository the tests happen to run in.
	//
	// Nil is the closed page and NOT an empty record: a repository with no arena
	// branches opens a record that says so, which is a different fact from never
	// having asked (§4a.1). Nothing here reaches room.json — the record is
	// re-read on every open, because the refs are the record and a copy of them
	// would be a second answer that goes stale.
	Record *ArenaRecord

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
	// The paragraph above was written when posture WAS launch-only and is now
	// wrong in its second half: §9.17 made it reachable from inside the room,
	// and /read and /write are the keystrokes it says do not exist. Kept rather
	// than deleted, because the reasoning it records — that what may act on a
	// working tree must be visible in the header for the whole session — is
	// still the rule. Only the "not reachable" claim was overruled.
	Write bool

	// GateOff records that the user has told the room to stop asking, and is
	// stored in the NEGATIVE on purpose.
	//
	// The obvious field is `Asking bool`, and it is a landmine: its zero value
	// is "does not ask", so every State built as a literal — which is most of
	// them in tests, and would be the first hand-built one in production —
	// silently gets an ungated room. A safety property whose default is off is
	// the wrong way round no matter how well the constructor sets it. Written
	// this way, the zero value is the guarded room and turning the gate off has
	// to be an act.
	//
	// Read it through Asking(), never directly.
	GateOff bool

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

	// FlowStop is `s` armed: the chain ends after the hop that is running now,
	// and later hops are not dispatched. On State rather than on Model because
	// it is a promise about the room's next act, and a promise held only in a
	// notice scrolls away while the state it describes persists — the WRITE
	// badge's argument (§9.35). The header's hop cell renders it; clearFlowMarker
	// retires it with the marker, so it can never outlive the chain it stops.
	FlowStop bool

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

	// TurnRoute is where the turn IN FLIGHT was sent, or nil between turns.
	//
	// A pointer, and that is the zero-vs-absent rule rather than a style choice
	// (§4a.1): `Route{}` is a real and common route — it is what `@all` parses
	// to — so a value field could not tell "this turn went to everyone" from
	// "no turn is running". The same distinction Column.CostUSD draws with the
	// same mechanism.
	//
	// Distinct from Route, which is the DRAFT's routing and is recomputed on
	// every keystroke: the moment a turn is dispatched the composer clears and
	// its route falls back to the default, so the live turn's destination has
	// nowhere else to be read from. Set at dispatch, cleared when the last
	// column lands, because it is a fact about a turn and not about the room
	// (§9.21).
	TurnRoute *Route

	// ArenaSetup is the step of a race's worktree setup that is running right
	// now, in WORDS — "preparing worktree for codex" — and empty when no setup
	// is running (§9.37, amended 2026-08-17).
	//
	// Words and nothing else, deliberately. There is no percentage here, no
	// count of seats finished and no elapsed figure, because council cannot
	// measure how long a checkout will take and §4a.1 forbids drawing a number
	// it did not measure. The honest claim is what is happening, not how far
	// along it is — and it is a string rather than a (stage, seat) pair for the
	// same reason: the room draws this sentence, so the sentence is the state.
	//
	// It sits on State because Render draws it and Render is pure over State.
	// The setup itself is Model.arenaPrep, which the renderer cannot reach.
	ArenaSetup string

	// Spinner advances only while something is genuinely in flight.
	Spinner int

	// ASCII mirrors the glyph set, so Render can pick a different affordance
	// where a straight substitution does not work.
	ASCII bool
}

// NewState is the empty room.
func NewState() State {
	return State{Mode: ModeComposing, Focus: 0, Route: defaultRoute()}
}

// Asking reports whether the gated seat still raises a card before each change.
//
// Derived rather than stored, which is `Gating()`'s idiom two fields down and is
// here for the stronger of its two reasons: the safe answer is the one you get
// by default. --auto seeds it at launch and `a` moves it either way, and the
// invocation, the badge and queueGate all read THIS — so the three can never
// disagree about whether anything is asking.
func (s State) Asking() bool { return !s.GateOff }

// Busy reports whether any column is still working. Drives the spinner.
//
// It used to say it drove the meaning of ctrl+c as well. It never did — that
// key reads Model.turn (program.go) — and the distinction stopped being
// pedantic once a column could settle ahead of its process: Busy goes false
// while the turn is still live, so a footer that had spent this predicate on
// ctrl+c would have offered `q` into a turn that refuses it. See Settling.
func (s State) Busy() bool {
	for _, c := range s.Columns {
		if c.Phase == PhaseWaiting || c.Phase == PhaseStreaming {
			return true
		}
	}
	return false
}

// Settling reports that a seat has answered and its process has not exited.
//
// The room is between two true things here: nothing is working, and the turn is
// not over. Derived from the columns rather than stored, for the same reason
// Gating is — a second home for the fact is a second thing to forget to reset.
func (s State) Settling() bool {
	for _, c := range s.Columns {
		if c.Settling {
			return true
		}
	}
	return false
}

// InFlight reports that a seat is working OR is still winding down. It is what
// the FOOTER asks before it chooses between offering `ctrl+c cancel` and
// `q quit`.
//
// Busy alone was that test, and it was right for exactly as long as a column's
// last phase change and its process's death were the same event. They are not
// on a seat that settles early, and the failure is not cosmetic: `q` is refused
// outright while a turn is live, so the footer would name a key that answers
// with a notice. A footer must never advertise a key that does nothing (§7.8,
// and the same argument that dropped `f` and `tab` from a one-seat room).
//
// It is deliberately NOT named as "a turn is live", because it is not that and
// cannot be: State does not see Model.turn, which is the only exact predicate.
// The one gap this method shipped with is CLOSED (§9.33's 2026-08-16 amendment):
// agy reports a failed turn as a KindError carrying exit code 0 and no error
// (vendors/agy.go), which set PhaseFailed without retiring the column, so that
// seat was neither Busy nor Settling while its turn was still live and `q` was
// still refused. dispatch.go now settles that column instead — the failure is
// the same split as an answer that lands ahead of its process, so it wears the
// same word. Retiring it was the alternative and is refused on §9.33's own
// argument: turnColumnFinished cancels the turn's context and that kills a
// process which is still winding down.
//
// What the method is still not, and what the closure does not change: a seat
// whose column is terminal and whose Settling nobody set is invisible here. The
// predicate is only ever as good as the retirement paths that feed it, so a NEW
// path that leaves a column terminal inside a live turn re-opens this — which is
// why the argument above is written out rather than left at "fixed".
func (s State) InFlight() bool { return s.Busy() || s.Settling() }

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

// SeatsIn is how many seats a route would actually be DISPATCHED to: seated ∩
// addressed.
//
// Both halves are load-bearing and each one alone would produce a number the
// room cannot stand behind. A route naming a vendor that is not installed — or
// one --vendor left out of the room — addresses it and bills nothing, so
// counting the route's own vendors would quote a price for a seat that will
// never be spawned. Counting the seated columns alone would ignore the routing
// entirely. This is the same intersection dispatch loops over before spawning
// anything, which is why Model.seatedIn now delegates here rather than keeping
// a second copy: a bill derived from different arithmetic than the dispatch is
// a bill for a turn that did not happen.
func (s State) SeatsIn(r Route) int {
	n := 0
	for _, c := range s.Columns {
		if s.seats(c) && r.addresses(c.Vendor) {
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
