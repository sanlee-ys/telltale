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
)

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
func Registry() map[model.VendorID]Vendor {
	return map[model.VendorID]Vendor{
		model.VendorClaude:      Claude{},
		model.VendorCodex:       Codex{},
		model.VendorAntigravity: Antigravity{},
		// Cursor is registered even though it detects as unusable on Windows,
		// and the two facts are independent on purpose: the registry answers
		// "is there an adapter for this seat", detection answers "can this
		// machine drive it". Conflating them is how the same platform quirk
		// would come back as "no adapter yet" on a Mac, where the seat works.
		model.VendorCursor: Cursor{},
	}
}
