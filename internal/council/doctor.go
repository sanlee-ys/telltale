package council

import (
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/adapter/pins"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
	"github.com/sanlee-ys/telltale/internal/probe"
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
	windows := runtime.GOOS == "windows"
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
			Posture:     doctorPosture(v, windows),
			VersionArgs: versionArgs(info),
		}
		if s.Drivable {
			s.DrivableDetail = drivableDetail(info)
		}
		// The survey pin, for the same reason the capability declaration is
		// attached here: doctor stays stdlib-only and holds no inventory of its
		// own, and this is already the one place that flattens what the rest of
		// the repo knows into a preflight's shape. A seat with no surveyed
		// adapter behind it keeps a zero Pin and renders no pin line.
		if p, ok := pins.For(v); ok {
			s.Pin = doctor.Pin{
				VerifiedAgainst: p.VerifiedAgainst,
				Section:         p.Section,
				Incomparable:    p.Incomparable,
			}
		}
		// The probe file, read at the same seam and for the same stated reason:
		// doctor stays stdlib-only and reads nothing of its own. Unlike the pin
		// and the capability this is not a claim from this repository — it is
		// what a `telltale probe` run on THIS machine recorded, and an absent
		// file stays a nil pointer so the preflight renders "never" rather than
		// a blank.
		s.Probed = probedSeat(v)
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

// probedSeat flattens one vendor's probe file into the shape the preflight
// prints, or nil when there is none.
//
// Every failure path here returns nil, and that collapse is the honest one: a
// home directory this process cannot resolve, a file that is not there, and a
// file that will not parse all mean the same thing to the reader — nothing on
// this machine says this seat was driven. What must never happen is the other
// collapse, so there is no branch here that returns a Probed with anything
// invented in it.
func probedSeat(v model.VendorID) *doctor.Probed {
	dir, err := probe.Dir()
	if err != nil {
		return nil
	}
	rec, ok := probe.Read(dir, string(v))
	if !ok {
		return nil
	}
	p := &doctor.Probed{
		Version:         rec.Version,
		When:            rec.ProbedAt,
		TelltaleVersion: rec.TelltaleVersion,
	}
	for _, c := range rec.Checks {
		pc := doctor.ProbedCheck{Name: c.Name, Status: probedStatus(c.Status)}
		if c.Millis != nil {
			pc.Took = time.Duration(*c.Millis) * time.Millisecond
		}
		p.Checks = append(p.Checks, pc)
	}
	return p
}

// probedStatus maps the file's three words onto the report's three states.
//
// A word this build does not know maps to NotChecked, which is the safe
// direction and the only one: a file written by a future telltale that grew a
// fourth word must not be able to render as a pass on a seat nobody proved.
func probedStatus(word string) doctor.Status {
	switch word {
	case probe.StatusOK:
		return doctor.Passed
	case probe.StatusFailed:
		return doctor.Failed
	default:
		return doctor.NotChecked
	}
}

// ProbeSeats is what `telltale probe` drives, and it is DoctorSeats' sibling:
// the same detection, the same binary resolution, the same registry, flattened
// for a mode that spends a turn instead of reading a version.
//
// It returns only seats detection says council will actually drive. A seat that
// is not installed, or whose only entry point is a shim council refuses, has no
// live shape to bring up — so it is absent from this list rather than present
// with a failure attached, and `telltale doctor` is the mode that already
// explains why. A probe that reported a failed handshake for a binary that is
// not on the machine would be spending a row on a question detection answered
// for free.
func ProbeSeats() []probe.Seat {
	reg := vendors.Registry()
	out := make([]probe.Seat, 0, len(reg))
	for _, info := range Detect() {
		if info.Avail != AvailInstalled {
			continue
		}
		adapter, ok := reg[info.Vendor]
		if !ok {
			continue
		}
		out = append(out, probe.Seat{
			Vendor:      info.Vendor,
			Label:       info.Label,
			Binary:      info.Binary,
			Adapter:     adapter,
			VersionArgs: versionArgs(info),
		})
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

// doctorPosture is one seat's sandbox claim, flattened for the preflight's
// posture block (design.md §9.42, amended 2026-08-17).
//
// It goes THROUGH postureClaim rather than beside it, and that is the whole
// design. `postureClaim(v, windows, false, false, false)` is literally what
// `telltale council --read` builds for this column — same function, same
// arguments, same platform branch — so the badge the preflight prints and the
// badge the column wears are one value read twice. A preflight that grew its own
// per-vendor table would agree on the day it was written and diverge the day a
// level moved, and a reader looking at two disagreeing surfaces has no way to
// tell which one is lying. The capability declaration above is attached at this
// seam for the same stated reason.
//
// The `--read` room is the posture reported because it is the only one that is a
// fact about the MACHINE. The default room writes, and what it writes with is a
// property of the argv the reader has not typed yet — so it is stated once, in
// the block's closing declaration, rather than five times in a column where
// every cell would read the same word.
func doctorPosture(v model.VendorID, windows bool) doctor.Posture {
	claim := postureClaim(v, windows, false, false, false)
	return doctor.Posture{
		Badge:    claim.Badge(),
		Evidence: evidenceClass(claim.Level),
		// Off canGate, not off a list of vendor names. That measurement has
		// already moved once — the Cursor seat became a live process that can be
		// asked and still does not ask about edits — and a copy here would have
		// gone on saying the old thing.
		CanGate: canGate(v),
	}
}

// evidenceClass is what KIND of evidence stands behind a badge, keyed by the
// level that renders it.
//
// The badge word says what the posture IS; this says what it RESTS ON, and the
// two are different questions with different answers. `unsandboxed` is the case
// that proves it: two seats can both fail to be read-only because a live run
// refuted the flags and because no flag was ever passed, and a reader deciding
// whether to point council at a worktree needs the second sentence, not the
// first. §4a.1's rule that two kinds of nothing must not render alike is the same
// rule one level up.
//
// A table keyed by level rather than prose, so a test can walk the type and fail
// the build the day a sixth level renders a badge with nothing here to classify
// it — see TestEveryPostureLevelHasAnEvidenceClass, which is the guard
// helpBadgeGloss already carries for the room's own legend, one surface out.
//
// NOTHING HERE MAY WEAKEN A CLAIM, on helpBadgeGloss's terms exactly. These are
// classifications of evidence, not softenings of it: `unsandboxed` still says
// nothing restricts the vendor, `ro:requested` still admits nobody observed what
// it enforces, and no line here may call any posture read-only, safe, or unable
// to write.
func evidenceClass(l SandboxLevel) string {
	switch l {
	case SandboxTools:
		return "enforced by CONSTRUCTION — the write and shell tools are absent from that " +
			"session, read off what the session reported about itself rather than off a flag. " +
			"The residual is that a deny list cannot cover a tool a future release adds"
	case SandboxEnforced:
		return "enforced by an OPERATING SYSTEM — the vendor's own sandbox, and the one posture " +
			"in this table that a flag is not the last thing standing behind"
	case SandboxRequested:
		// It does NOT say "weaker than the two above", which is how the room's own
		// legend words it. That legend is ordered by level; these rows are ordered
		// by seat, so "above" points at whatever vendor happens to sort first. The
		// comparison is named instead of pointed at.
		return "ASKED FOR, and never observed — the flag was accepted and what it enforces on " +
			"this machine is not established. Weaker than a construction or an OS sandbox, " +
			"and says so"
	case SandboxNone:
		return "MEASURED not to restrict — a live run refuted the flags rather than leaving " +
			"them unestablished, so treat this seat as able to change your files"
	case SandboxWrite:
		return "nothing was asked for — this seat may edit and run, and no restriction was " +
			"requested of it at all"
	case SandboxGated:
		return "YOUR KEYSTROKE — this seat asks before every tool call that changes anything, " +
			"and nothing runs until you answer"
	default:
		// SandboxUnknown renders no badge, so there is nothing to classify. An
		// invented sentence here would be a claim about a seat council makes none
		// about; doctor prints its own honest blank instead.
		return ""
	}
}
