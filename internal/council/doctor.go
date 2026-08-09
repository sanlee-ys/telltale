package council

import (
	"path/filepath"
	"strings"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
)

// DoctorSeats flattens what detection found into the shape `telltale doctor`
// reports on.
//
// It lives here rather than in internal/doctor because everything it reads is
// already here and must not be re-derived: the candidate list, the known
// install locations, the shim classification, and the capability declarations
// the room's own badges are drawn from. Two copies of "where does cursor-agent
// live" is exactly the agreement CursorNodeBundle's doc says silently stops
// holding — a preflight that looked in a different place from the room would
// tell a user their seat is fine and then hand them an empty column.
//
// The dependency points one way on purpose: council knows about doctor, doctor
// knows nothing about council. That is what lets the report be exercised
// against a synthesized machine with no vendors installed, which is the only
// kind of machine CI has.
func DoctorSeats() []doctor.Seat {
	reg := vendors.Registry()
	infos := Detect()
	out := make([]doctor.Seat, 0, len(infos))
	for _, info := range infos {
		v := info.Vendor
		s := doctor.Seat{
			Vendor:      string(v),
			Label:       info.Label,
			Found:       info.Avail != AvailNotInstalled,
			Binary:      info.Binary,
			Source:      info.Source,
			Note:        info.Note,
			Drivable:    info.Avail == AvailInstalled,
			Capability:  declaredCapability(v, reg),
			VersionArgs: versionArgs(info),
		}
		if s.Drivable {
			s.DrivableDetail = drivableDetail(info)
		}
		// A resolved binary with no note still owes the reader a sentence: an
		// unusable seat always carries one (classify writes it), but a seat that
		// detection never reached at all would otherwise render a blank reason.
		if !s.Found && s.Note == "" {
			s.Note = "detection resolved nothing and said nothing about why"
		}
		out = append(out, s)
	}
	return out
}

// versionArgs is the argv after the binary that asks a vendor its own version.
//
// Every seat takes `--version`, verified by running each of them on this
// machine (2026-08-09): claude answers "2.1.226 (Claude Code)", codex
// "codex-cli 0.147.0", agy "1.1.11", grok "grok 1.0.0 (3cd0d0cbce) [stable]".
//
// The Cursor seat is the reason this is a function rather than a constant, and
// the reason is measured rather than defensive. Detection resolves that seat to
// the bundled node.exe its .cmd launcher would have run, so `--version` alone
// answers `v24.5.0` — node's version, printed under a row labelled `cursor`.
// The bundle has to go first, exactly as the dispatch path already puts it
// there (cursor.go's argv builder), and then the same install answers
// `2026.08.04-aaa8809`. Both measured here, side by side.
//
// It reuses vendors.CursorNodeBundle rather than joining "index.js" itself, for
// that function's own stated reason: two copies of one filepath.Join is the
// agreement that stops holding.
func versionArgs(info VendorInfo) []string {
	if info.Vendor == model.VendorCursor {
		if bundle := vendors.CursorNodeBundle(info.Binary); bundle != "" {
			return []string{bundle, "--version"}
		}
	}
	return []string{"--version"}
}

// drivableDetail says WHY a resolved binary is one council will drive, in the
// terms detect.go decides it on: whether a shell ends up in the invocation, and
// if the launcher was stepped over, that it was.
func drivableDetail(info VendorInfo) string {
	s := "council will seat this vendor"
	switch info.Kind {
	case KindShim:
		// Reachable only for a vendor that takes its prompt on stdin — the
		// refusal in classify() is what makes that true — so the sentence can
		// state the reason rather than hedge.
		s += "; its entry point is a " + filepath.Ext(info.Binary) +
			" shim, drivable because the prompt goes on stdin and only fixed flags cross cmd.exe"
	default:
		s += "; a native executable, so the prompt goes in argv and no shell sees it"
	}
	return s
}

// declaredCapability is what the room will ask of this seat, in the room's own
// words — and it is a DECLARATION, not a check.
//
// Every clause here was measured once, against a live run, and written into
// this repo (§9.7, §9.33, §9.36, §9.39 for the streaming words; §9.8 and
// canGate for the gate). None of it is re-measured by a preflight, and doctor
// renders it outside the three-state block for exactly that reason. Stating it
// at all is worth the line: "installed" and "will stream to you" are different
// promises, and the seat a user is most likely to think is broken is the one
// that is working and silent until the end of the turn.
func declaredCapability(v model.VendorID, reg map[model.VendorID]vendors.Vendor) string {
	adapter, ok := reg[v]
	if !ok {
		return "no adapter — council cannot drive this seat even where the binary is present"
	}
	var parts []string
	switch granularityFor(v) {
	case GranFinalOnly:
		parts = append(parts, "the reply arrives whole at the end of the turn, not as it is written")
	case GranTokens:
		parts = append(parts, "streams its reply as it is written")
	case GranEvents:
		parts = append(parts, "streams whole messages as they complete")
	default:
		parts = append(parts, "how it streams has not been established")
	}
	switch adapter.(type) {
	case vendors.Conversational:
		parts = append(parts, "driven as one live process that is asked and answers")
	case vendors.Persistent:
		parts = append(parts, "driven as one live process across turns")
	default:
		parts = append(parts, "a batch program: one process per turn")
	}
	if canGate(v) {
		parts = append(parts, "can be asked to ask first, before every tool call that changes anything")
	}
	return strings.Join(parts, "; ")
}
