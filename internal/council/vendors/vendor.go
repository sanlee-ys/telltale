// Package vendors turns a prompt into one vendor's invocation, and that
// vendor's stdout back into events.
//
// One file per vendor, mirroring internal/adapter's shape on the observation
// side: the differences between these CLIs are large and specific, and the way
// to keep them from leaking into the room is to make each one own its own
// flags, its own event schema, and its own honest claim about what it enforces.
package vendors

import (
	"errors"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// ErrNoResume is returned by NextTurn when a vendor has no session to resume —
// the first turn failed, or it never reported an id. The caller falls back to
// starting a fresh turn and says so on screen rather than pretending the
// conversation continued.
var ErrNoResume = errors.New("vendors: no session to resume")

// Posture is how much the room lets a vendor do.
//
// Two values, and the distinction is real rather than cosmetic. Council's
// read-only default is a per-INVOCATION choice about what this tool asks for;
// it is not a claim about what the vendor is capable of. The fleet contract
// (agent-ops ADR-012) rules that all four vendors read and write, and that
// guard wiring rather than lane shape is the control — so nothing here may be
// read as "this vendor is read-only".
type Posture uint8

const (
	// PostureRead asks each vendor for the most read-only invocation it
	// actually honours. What that buys differs per vendor and is stated on
	// screen per column rather than claimed once for the room.
	PostureRead Posture = iota
	// PostureWrite drops those requests. The containment then is the WORKSPACE
	// — which directory council was pointed at — not a flag.
	PostureWrite
	// PostureWriteGated drops them too, and puts a person in front of every
	// tool call instead.
	//
	// Only a vendor that can ASK can be given this posture, which today is one
	// of the four. It is not a middle setting between read and write: the seat
	// may do anything write mode allows, and the difference is that it has to
	// be told yes first.
	PostureWriteGated
)

// Conversational is a seat driven as ONE live process whose protocol is a
// CONVERSATION rather than a stream.
//
// It is a sibling of Persistent, not a subtype of it, and the split is the
// measurement rather than a taste in interfaces. A Persistent seat's turn is a
// line the room can encode whenever it likes and write down a pipe; a
// Conversational seat's turn cannot be built until the vendor has answered a
// request of the room's own, and the vendor asks questions back on the same
// pipe. cursor-agent's ACP server is the second kind — design.md §9.36 — and
// trying to express it through Persistent's `Turn(prompt) ([]byte, error)` was
// the first of the three forks §9.33 declined to guess at.
//
// So everything stateful lives on runner.Protocol, which exists for exactly one
// process, and this interface is only the door to it.
type Conversational interface {
	Vendor

	// Open returns the invocation and the per-process protocol driver.
	//
	// sessionID is a thread to reload, or empty for a new conversation. Unlike
	// Persistent's Session/SessionResume pair it is one method, because unlike
	// there the two cases are the same invocation: resuming changes a JSON-RPC
	// method the protocol picks later, not argv.
	Open(workspace, binary, sessionID string, p Posture) (runner.Spec, runner.Protocol, error)
}

// SilentResumeFork names a seat whose vendor, handed a conversation id it no
// longer holds, does NOT refuse it: it opens a NEW conversation, answers the
// brief normally, and reports success.
//
// It exists because that behaviour breaks the assumption every other resume path
// in this package is built on — that a vendor either continues the thread or
// says it cannot. A vendor in this set says neither, so a room reading only
// status and exit code would report a resumed conversation on a turn that has no
// history behind it. The one tell such a stream carries is that the id it reports
// back is not the id it was asked for, and comparing those two is the whole of
// what implementing this interface buys.
//
// Implementing it is a claim about a LIVE RUN, never about what a CLI's help
// text implies (ADR-001, design.md §4a.1), which is why the method returns the
// build the fork was measured against rather than being a bare marker: a seat
// cannot make the claim without naming its evidence, and a vendor bump that
// fixes the behaviour leaves a version string that no longer matches the
// fixtures beside it.
//
// Deliberately NOT implemented by default. An id mismatch on a vendor whose
// resume semantics have not been measured is not evidence of anything — it could
// as easily be a vendor that re-keys a resumed thread while keeping its history
// — and firing a "your history is gone" card on that would be exactly the
// inference §4a.1 forbids.
type SilentResumeFork interface {
	Vendor

	// SilentResumeForkMeasuredAt names the vendor build, and the date, the fork
	// was observed on.
	SilentResumeForkMeasuredAt() string
}

// LiveFallback is a live seat that names the batch adapter the room may fall
// back to when the live protocol cannot be brought up.
//
// It exists because three seats moved to long-lived processes on 2026-09-02
// (design.md §9.54) on the strength of vendor documentation rather than a live
// run, and a seat built that way needs a measured path to retreat to: the
// `codex app-server` handshake can be refused by a build that predates it, a
// `grok agent stdio` server can decline `initialize`, and an `agy` that does
// not know `--input-format` exits at argument parsing. Each of those has a
// batch invocation beside it that WAS measured, and the room should spend a
// brief on that rather than on a column that reports the same refusal every
// turn.
//
// The interface names the fallback and nothing else. WHEN to fall back is the
// room's call, made from what it can see — a protocol reporting Dead(), or a
// live process exiting before it ever named a session — and the doc comment on
// each implementer says which shape its failure takes. Nothing here decides
// for the room, because the seat cannot see the process exit and the room
// cannot see the handshake.
type LiveFallback interface {
	Vendor

	// Fallback returns the spawn-per-turn adapter for the same vendor id. It is
	// a measured adapter, and it may be handed to FirstTurn/NextTurn/ParseEvent
	// exactly as a batch seat is.
	Fallback() Vendor
}

// GracefulStop is a live seat that wants a last word before its process is
// killed.
//
// The room's teardown used to be one call — Kill — and for the stream-json
// seat that was measured sufficient: Claude Code exits 0 on a closed stdin.
// `codex app-server` was measured NOT doing that at 0.149.1 (design.md §9.50:
// four runs exited in 1.5–3.3 s after stdin closed, one was still alive 15 s
// later), so its seat owns the kill in three steps — say what has to be said,
// close the pipe, wait a bounded grace, then kill. This interface is the first
// step and the bound; the runner's kill path is unchanged and is what actually
// ends the process.
//
// A Protocol implements it for a Conversational seat, and the adapter itself
// implements it for a Persistent one, because those are the two things the
// room holds per process.
type GracefulStop interface {
	// Closing returns the lines to write before stdin is closed: an interrupt
	// for a turn still in flight, a refusal for any question the vendor is
	// blocked on, and nothing at all for an idle process.
	Closing() [][]byte
	// Grace is how long the room waits after closing stdin before it kills the
	// process. A bound, not a promise that the process exits within it.
	Grace() time.Duration
}

// Persistent is a vendor that can be driven as ONE process taking many turns,
// rather than a fresh child per turn.
//
// Two vendors implement it since 2026-09-02, and the second is the one to read
// this comment for. Claude Code's stream-json session was MEASURED (claude.go);
// Antigravity's `--input-format stream-json` was read from its documentation
// at 1.1.24 and is seated on that reading, with `agy -p` kept beside it as the
// measured fallback (agystream.go, design.md §9.54). `codex exec` stays a
// batch program and is now the fallback behind the app-server seat; `grok
// --single` likewise behind the ACP seat. Neither batch adapter has a channel
// on which to ask a question mid-turn, which is the whole reason the live
// shapes exist.
//
// The Cursor seat used to be a third batch column and is now neither: it is
// Conversational, above.
type Persistent interface {
	Vendor

	// Session builds the invocation for a process that will take many turns.
	// No prompt appears anywhere in it: every turn arrives later, on stdin.
	//
	// hooksFile is an absolute path to a settings file holding the user's own
	// hooks and nothing else, or empty when the room could not carry any over.
	// It is in this signature rather than in Posture because it is a path that
	// exists for the lifetime of one room, and Posture is a value the renderer
	// and the golden tests construct freely.
	Session(workspace, binary, hooksFile string, p Posture) (runner.Spec, error)

	// SessionResume is Session, started on a conversation the vendor already
	// holds. Returns ErrNoResume when there is no id to resume.
	//
	// It is a separate method rather than an optional argument to Session
	// because the two are different claims. Session opens a NEW conversation and
	// the caller owes the vendor a brief; this one continues an existing one and
	// must not re-send it. Folding them together behind an empty-string check
	// would put that decision in the caller's hands on every call site.
	SessionResume(workspace, binary, hooksFile, sessionID string, p Posture) (runner.Spec, error)

	// Turn encodes one turn as the line the process expects on its stdin.
	Turn(prompt string) ([]byte, error)

	// Interrupt encodes a request to abandon the turn in flight WITHOUT killing
	// the process. id is any string the caller can recognise coming back.
	Interrupt(id string) ([]byte, error)

	// Decide encodes the answer to one Gate. allow=false carries reason back to
	// the vendor as the tool's error text.
	Decide(requestID string, allow bool, reason string, input map[string]any) ([]byte, error)
}

// Vendor is one seat at the table.
type Vendor interface {
	ID() model.VendorID

	// FirstTurn is the blind opening dispatch: this vendor's own brief, with no
	// other vendor's answer in it. That isolation is what makes the three
	// opinions independent rather than anchored, so it is a property of the
	// interface rather than a convention callers are trusted to follow.
	FirstTurn(prompt, workspace, binary string, p Posture) (runner.Spec, error)

	// NextTurn resumes the vendor's own session. Only the new prompt is sent;
	// the vendor replays its own history.
	NextTurn(prompt, workspace, binary, sessionID string, p Posture) (runner.Spec, error)

	// ParseEvent converts one line of stdout. Returning false drops the line,
	// which is how a parser ignores event types it does not model instead of
	// failing the turn on a schema it has not seen before.
	ParseEvent(line []byte) (runner.Event, bool)
}

// Registry maps a vendor id to its adapter. Only vendors whose invocation has
// actually been verified appear here; a seat with no adapter renders as an
// unavailable column rather than a guess.
//
// A VAR since 2026-09-02, on the precedent of council's spawn vars and for a
// related reason. Every registered seat is now a live process, so a council
// test that needs "an ordinary batch seat" — the give-up and re-send suites
// exercise a code path that kills a one-shot child — has no registry entry to
// find one in. That path is not dead: it is the room's state after a
// LiveFallback retreat, and it is every arena racer. A test reaches it by
// swapping in FallbackRegistry for the test's lifetime, which is a production
// state rather than a mock of vendor behaviour. Production never reassigns
// this.
var Registry = registry

func registry() map[model.VendorID]Vendor {
	return map[model.VendorID]Vendor{
		model.VendorClaude: Claude{},
		// Three seats moved to long-lived processes on 2026-09-02 (design.md
		// §9.54), and each entry below is the live shape with its measured
		// batch adapter reachable through LiveFallback. The invocations the
		// live shapes build were read from vendor documentation at the
		// versions named on each type and have NOT been driven from here; the
		// badges say "unmeasured" for that reason and design.md §9.54 lists
		// the runs that would let them stop.
		model.VendorCodex:       CodexAppServer{},
		model.VendorAntigravity: AntigravityStream{},
		// Cursor is registered even though it detects as unusable on Windows,
		// and the two facts are independent on purpose: the registry answers
		// "is there an adapter for this seat", detection answers "can this
		// machine drive it". Conflating them is how the same platform quirk
		// would come back as "no adapter yet" on a Mac, where the seat works.
		//
		// It satisfies Vendor only formally: its batch entry points return
		// ErrCursorIsLiveOnly, because the seat is Conversational and there is no
		// spawn-per-turn invocation of it any more.
		model.VendorCursor: Cursor{},
		model.VendorGrok:   GrokAgent{},
	}
}

// FallbackRegistry is Registry with every LiveFallback seat replaced by its
// measured batch adapter: the room a full retreat would leave.
//
// It exists so that state can be constructed rather than reached. The room
// falls back one seat at a time, on a refused handshake it has watched; a test
// that wants the batch shape of a seat should not have to script a refusal to
// get there, and a reader who wants to know what the batch room looks like
// should not have to reassemble it from five doc comments.
func FallbackRegistry() map[model.VendorID]Vendor {
	reg := registry()
	for id, v := range reg {
		if lf, ok := v.(LiveFallback); ok {
			reg[id] = lf.Fallback()
		}
	}
	return reg
}
