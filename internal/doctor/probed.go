package doctor

import (
	"fmt"
	"strings"
	"time"
)

// This file is the preflight's THIRD claim line, and it is the first one that
// reports a measurement made on the reader's own machine.
//
// # What it is for
//
// pin.go's line says "telltale's field map for this vendor was measured at
// 0.147.0, but this machine reports 0.151.0 — re-measure before trusting the
// fields". That sentence is honest, and a reader hears the second half of it:
// every live claim this repository makes was paid by the maintainer's hand, and
// nothing on this machine re-pays any of it. `telltale probe` is what pays one
// part of it here, and this line is where the payment shows up.
//
// So the two lines are deliberately adjacent and deliberately different. The
// survey line is about TELLTALE — how old this repository's homework is. This
// line is about THIS MACHINE — whether the seat came up, took a brief and
// stopped, when somebody last asked it to. A reader who has both can tell
// "telltale has not looked at this vendor since August" from "this vendor does
// not work here", which used to be one undifferentiated worry.
//
// # It is not a check either, and for a sharper reason than the two above it
//
// Capability, the survey pin and the posture are all claims this repository
// wrote down. This one is a claim a PROBE wrote down, on a day that is not
// today, at a version that may not be the one installed now. It still is not a
// check on this run: nothing here spawned anything, nothing was re-measured,
// and a `probe` result is exactly as old as its stamp. So it renders outside
// the three-state block with the other claims, it moves no count, and it
// changes no exit code.
//
// # Absent renders as absent
//
// The one thing this line may never do is let a seat nobody probed read like a
// seat that passed. A machine with no probe file gets the sentence saying so,
// with the command that would fix it — never a blank, and never a missing line,
// because a seat whose row simply is not drawn reads as a seat with nothing to
// report. That is design.md §4a.1's zero-versus-absent rule on a surface that
// has no gauge on it.

// Probed is what a `telltale probe` run of this seat left behind, flattened
// into what the preflight prints.
//
// A plain struct of measured values, like Pin and Posture, so this package
// stays stdlib-only. It is filled by internal/council, which reads the probe
// file — the same seam the capability, the survey and the posture arrive
// through, and for the same stated reason: doctor holds no inventory and does
// no reading of its own.
type Probed struct {
	// Version is the vendor build the probe drove, as that binary printed it.
	// Empty when the probe could not read one, which is stated rather than
	// filled.
	Version string
	// When is when the probe ran.
	When time.Time
	// TelltaleVersion is the telltale build that did the driving.
	TelltaleVersion string
	// Checks are the probe's three checks, in the order it ran them.
	Checks []ProbedCheck
}

// ProbedCheck is one recorded check.
type ProbedCheck struct {
	// Name is the check, in the probe's own one-word vocabulary: handshake,
	// turn, stop.
	Name string
	// Status is the same three states this report prints everywhere else. One
	// vocabulary rather than two: a reader has just read the legend, and a
	// second set of words for the same distinction is how they end up believing
	// the two mean different things.
	Status Status
	// Took is how long the check took, and it is rendered only beside a status
	// that ran — a `not checked` row carrying "0.00s" would read as an instant
	// pass.
	Took time.Duration
}

// probedLabel prefixes the rendered line, and it states where the measurement
// was made before it says anything about the measurement. `here` is the whole
// point of the line: every other claim on this seat was measured somewhere
// else.
const probedLabel = "probed here: "

// probedNote words one seat's probe line.
//
// installed is the version this preflight actually READ on this run — the value
// off a version check that passed, and the empty string on every other branch.
// It is the same argument surveyNote makes with the same parameter: a
// comparison against a version nobody read is a verdict computed against
// nothing, so it is not made.
//
// The comparison is EQUALITY and never ordering, for pin.go's reason exactly.
// A downgraded vendor and an upgraded one produce the same notice, which is
// correct: both mean the probe drove a build that is not the build on this disk
// now.
func probedNote(vendor string, p *Probed, installed string) string {
	if p == nil {
		// The absent branch, and the only one that names no measurement. It
		// carries the command rather than only the fact, because a reader who
		// has just been told a seat is unproven and not told how to prove it
		// has been given a worry instead of a next step.
		return "never — nothing on this machine has driven this seat. `telltale probe " +
			vendor + "` brings it up, spends ONE turn of one word on it, and times its stop."
	}

	line := probedVersion(p.Version) + " on " + p.When.Format("2006-01-02") +
		", " + checkWords(p.Checks)

	if p.Version == "" || installed == "" {
		// Nothing to compare, so nothing is claimed in either direction —
		// neither that the probe is current nor that it is stale.
		return line
	}
	probed, here := versionToken(p.Version), versionToken(installed)
	if probed == "" || here == "" {
		return line
	}
	if probed == here {
		return line
	}
	return line + "; this machine now reports " + installed + " — re-run `telltale probe " +
		vendor + "` before trusting this row"
}

// probedVersion is the build the probe drove, or the honest blank. A date with
// no version in front of it would read as a probe of whatever is installed
// today, which is the claim this line exists to stop anyone making.
func probedVersion(v string) string {
	if strings.TrimSpace(v) == "" {
		return "a build this machine printed no version for"
	}
	return v
}

// checkWords renders the recorded checks as one clause each, in the order the
// probe ran them.
//
// A single line rather than three rows, and that is a deliberate demotion. The
// three-state block above holds checks this run made; these are checks some
// earlier run made. Giving them the same grid would put four rows of `ok` next
// to five rows of `ok` and invite a reader to read all nine as one preflight.
func checkWords(checks []ProbedCheck) string {
	if len(checks) == 0 {
		return "and it recorded no check at all, which is a probe file this report cannot read"
	}
	out := make([]string, 0, len(checks))
	for _, c := range checks {
		switch c.Status {
		case Passed:
			out = append(out, fmt.Sprintf("%s %s %.1fs", c.Name, c.Status.Word(), c.Took.Seconds()))
		case Failed:
			out = append(out, fmt.Sprintf("%s %s after %.1fs", c.Name, c.Status.Word(), c.Took.Seconds()))
		default:
			// No duration on a check that did not run. The probe stops a seat at
			// its first failure, so this is the ordinary shape of the two rows
			// under a failure and not an anomaly.
			out = append(out, c.Name+" "+c.Status.Word())
		}
	}
	return strings.Join(out, ", ")
}
