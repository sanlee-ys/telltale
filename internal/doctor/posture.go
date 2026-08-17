package doctor

import (
	"fmt"
	"strings"
)

// This file is the preflight's POSTURE block: what each seat's read-only claim
// is actually worth, and what CLASS of evidence stands behind it.
//
// # Why a preflight owes this at all
//
// The room already says it. Every council column carries a badge, and the help
// panel's posture page carries the measured argument under each one (§9.13,
// SandboxClaim.Detail). Both of those are read INSIDE the room — which is after
// the decision they inform. A user picks a workspace and a posture before the
// room opens, and until now the only surface that runs before the room opens
// said nothing about either. §9.17's frame settles that the fact belongs here:
// what a vendor's own flags buy on this machine is true at launch and stays
// true, and it is a property of the vendor and the OS rather than of a turn.
//
// # One source, two surfaces
//
// Nothing in this block is written here. `council.DoctorSeats` builds it from
// `postureClaim` — the same function the room's own columns are built from —
// and hands over the badge word off `SandboxClaim.Badge()` and the evidence
// class off the claim's Level. That is the whole point of routing it through
// council rather than restating it: a preflight that grew its own posture table
// would agree with the badges on the day it was written and diverge on the day
// a level moved, and the reader would have no way to tell which of the two
// surfaces was lying. The capability line above it is attached for the same
// reason and by the same seam.
//
// # It is not a check, and it must never become one
//
// A posture is a CLAIM, exactly like the capability line and the survey pin: it
// was measured once, against a live run, and written into this repository —
// nothing re-measures it on the reader's machine now. So it renders outside the
// three-state block, it carries no status word, it moves no count, and it
// changes no exit code. Making it a check would be wrong in both directions: a
// `FAILED` would redden a working install over a vendor's design decision, and
// an `ok` would claim this preflight established a containment property that it
// did not probe and could not probe without spending a turn.
// `TestAPostureIsNotACheck` pins that the way `TestDriftIsNotAFailedCheck` pins
// the survey pin, and by the same method: the same seat with and without the
// data, and the three counts required to be identical.
//
// # What it deliberately does NOT do
//
// No probe, no network call, no login check, and no new read of anything. The
// block costs the report zero processes: every string in it arrives with the
// seat. That is what keeps this a rendering change rather than a second, wider
// definition of what a preflight is allowed to do (§9.42 draws that line at
// cost and side effect, and this side of it costs nothing).

// Posture is one seat's sandbox claim, flattened into what a preflight prints.
//
// A plain struct of already-worded strings, like Capability and Survey, so this
// package stays stdlib-only and so a test can synthesize a seat whose posture no
// vendor on this machine has. Nothing here is computed by this package — see the
// file doc.
type Posture struct {
	// Badge is council's own badge word for this claim (`ro:tools`,
	// `unsandboxed`, …), off SandboxClaim.Badge and not off a copy of it. Empty
	// when council states no posture for the seat, which renders as an honest
	// blank rather than as a missing row.
	Badge string
	// Evidence is the CLASS of evidence behind that badge — enforced by
	// construction, enforced by an operating system, asked for and never
	// observed, measured not to restrict. It is the question the badge word
	// alone cannot answer: two seats can both fail to be read-only and have
	// arrived there by a refuted measurement and by an unasked question, which
	// are not the same fact about the seat.
	Evidence string
	// CanGate reports that this seat can be asked to ask first, off council's
	// own canGate. It is not per-seat prose: the report counts it and states the
	// result once, because "one of five" is the shape of that fact and five
	// lines each saying "not this one" is not.
	CanGate bool
}

// stated reports that council gave this seat a posture at all.
func (p Posture) stated() bool { return p.Badge != "" }

// postureHeader introduces the block, and it spends its first clause naming the
// argv these badges belong to.
//
// That matters more here than anywhere else in the report. Every other line
// above describes the machine, which is the same machine whatever the reader
// types next; a posture is not, because the room writes by DEFAULT and these
// words are what `--read` buys. A block of `ro:` badges with no argv on it would
// read as what `telltale council` gives you, which is the opposite of true —
// the §9.17 defect (a surface crediting a posture the typed command does not
// reach), committed in the block that exists to prevent it.
const postureHeader = "posture — what each seat's read-only claim is worth in the room " +
	"`telltale council --read` opens, and what class of evidence stands behind it. Nothing " +
	"below was probed here: it is read off the same data the room's own column badges are " +
	"drawn from, so the two surfaces cannot disagree."

// The honest blank for a seat council states no posture for — a vendor with no
// adapter behind it, or one detection never reached. Dropping the row would be
// worse than either: a seat missing from a posture table reads as a seat with
// nothing to declare, and this one has an unanswered question instead.
//
// The word is `no claim` rather than anything shaped like `not checked`. The
// three state words are spoken for, and a fourth column borrowing one of them
// would put this block back inside the block it is deliberately outside of.
const (
	postureNoClaimBadge = "no claim"
	postureNoClaim      = "council states no posture for this seat, so this preflight states none " +
		"either — an unanswered question, not a permissive one"
)

// postureDeclaration is the block's one closing line, and it is the only
// sentence in the block that is about the ROOM rather than about a seat.
//
// It exists because the rows above describe a posture nobody gets by default.
// `telltale council` writes; `--read` is the opt-out (cmd/telltale, and the help
// panel's own legend was corrected for crediting the retired `--write` flag). A
// reader who took the rows for the default would come away believing this room
// cannot touch their files, which is the single most expensive wrong belief this
// product can hand anyone.
//
// The gating half is COUNTED rather than written down. "One of five seats can be
// asked to ask first" is a fact about canGate, and canGate is a measurement that
// has already moved once — the Cursor seat became a live process that can be
// asked and still does not ask about edits, which is exactly the case a
// hand-written "only claude" sentence would have gone on getting wrong. So the
// count and the names come off the report.
func postureDeclaration(r Report) string {
	var gating []string
	for _, s := range r.Seats {
		if s.Posture.CanGate {
			gating = append(gating, s.Vendor)
		}
	}
	const tail = " What contains a room that writes is the workspace you point it at, not any " +
		"of these words — point council at a throwaway worktree when that matters."

	if len(gating) == 0 {
		return "The room `telltale council` opens by default WRITES, and none of the " +
			fmt.Sprint(len(r.Seats)) + " seats above can be asked to ask first: every one of " +
			"them carries `WRITES` there, whatever it carries under `--read`." + tail
	}
	return fmt.Sprintf(
		"The room `telltale council` opens by default WRITES, and %d of the %d seats above can "+
			"be asked to ask first: %s carries `gated` there and asks before every tool call "+
			"that changes anything, while the rest carry `WRITES`.%s",
		len(gating), len(r.Seats), strings.Join(gating, ", "), tail)
}
