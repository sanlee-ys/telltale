package doctor

import (
	"regexp"
	"strings"
)

// This file is the maintainer's half of the preflight, and it is deliberately
// the only part of this package that reports on TELLTALE rather than on the
// machine.
//
// # What this is, and what it must not be mistaken for
//
// Every adapter is pinned to the vendor build its field map was surveyed at
// (design.md §3.10). Two of these vendors self-update. So the pin goes stale
// silently: nothing fails, no row goes blank, and the survey behind a set of
// displayed fields is simply older than the program on disk. CI cannot see this
// — CI has no vendors installed, so every version probe there resolves to
// nothing — which is why the loop has to live where the vendors live. The owner
// ruled on 2026-08-16 that the place is here, and doctor is the obvious host: it
// already resolves each seat's binary and already asks it its version.
//
// It is NOT the canary mechanism and must not be read as one.
// `internal/adapter/drift` reports that a vendor's on-disk SHAPE moved, and its
// package doc rules a version comparison out of that job in as many words: a
// report that fires on every release is a report nobody reads. Nothing here
// touches an adapter's read path, degrades a field, or writes a diagnostic onto
// a session. The two answer different questions for different readers — "your
// row went quiet" versus "your survey is older than the vendor" — and the
// version comparison is only admissible at all because this second question is
// asked once, by hand, before the room opens.
//
// # It is a STALENESS FACT, not a failure
//
// A drifted pin is not one of the three states. It cannot be `FAILED`: nothing
// on this machine is broken, the seat still works, and marking it failed would
// change the tally, change Ready(), and put a red word next to a vendor that is
// behaving perfectly. It cannot be `Passed` either, because that would claim a
// check ran on the operator's machine when what actually happened is that this
// repository compared its own homework against a version string.
//
// So it renders OUTSIDE the three-state block, next to Capability, which is
// there for the neighbouring reason: Capability is a claim this repo measured
// once and wrote down rather than re-measured here. The pin is the same kind of
// claim, and this is the one line that says how old it is. The exit code is
// untouched (cmd/telltale's runDoctor returns nil whatever the report says).

// Pin is the survey pin for one seat, as design.md §3.10 records it. It is
// filled by internal/council from internal/adapter/pins; this package holds no
// inventory of its own, for the same reason it holds no seat list of its own.
type Pin struct {
	// VerifiedAgainst is the vendor build the adapter's field map was read at,
	// in the adapter's own words ("agy 1.1.13").
	VerifiedAgainst string
	// Section is the design.md section carrying that survey's evidence ("§3.8").
	// It is printed as the instruction for what to re-open.
	Section string
	// Incomparable, when set, is why this pin and an installed version cannot be
	// compared at all — the Cursor case, where the pin names the application and
	// the probe reads cursor-agent. A seat carrying it gets no verdict in either
	// direction.
	Incomparable string
}

// surveyLabel prefixes the rendered line. It states the line's epistemic status
// before its content, exactly as the capability line does, because the sentence
// that follows is about this repository and every other line on the seat is
// about the reader's machine.
const surveyLabel = "telltale's field map for this vendor was measured at: "

// driftNote is the argument, made once at the end of the report rather than
// under every drifted seat — the same economy the auth and network notes use.
//
// It is printed only when something actually drifted. The two standing unknowns
// print unconditionally because they are properties of what this mode does; this
// is a property of what it FOUND, and a paragraph explaining a notice nobody
// received is the kind of padding that stops reports being read to the bottom.
// `nextStep` is the precedent for a note that branches on the report.
const driftNote = "One or more seats above run a version other than the one telltale's " +
	"survey of that vendor was measured at. That is a staleness fact about telltale, not a " +
	"fault on this machine: no check above failed because of it, the tally is unchanged, and " +
	"this command still exits 0. The adapter goes on reading — what is unknown is whether the " +
	"private on-disk format it was surveyed against still looks the way it did, and the " +
	"design.md section named on the seat is where that survey lives. A format that has " +
	"actually moved is reported separately, on the HUD row itself, and does not wait for this."

// dottedVersion matches a dotted-numeric run: `2.1.219`, `0.146.0`, `1.1.13`.
//
// This is what makes two differently-shaped strings comparable at all. The pin
// and the probe agree on a number and on nothing else: the adapter writes
// "Claude Code 2.1.219" and the binary answers "2.1.226 (Claude Code)"; the
// adapter writes "grok 1.0.4 (d846eb93d9)" and the binary answers "grok 1.0.0
// (3cd0d0cbce) [stable]". A commit hash carries digits but no dot, so it cannot
// be mistaken for the version beside it.
var dottedVersion = regexp.MustCompile(`\d+(?:\.\d+)+`)

// versionToken is the first dotted-numeric run in s, or "" if it has none.
func versionToken(s string) string { return dottedVersion.FindString(s) }

// surveyNote words one seat's pin line and reports whether it drifted.
//
// installed is the version this preflight actually READ — the value off a
// version check that passed, and the empty string on every other branch. That
// argument is the whole of the honesty here: a seat whose version could not be
// read gets no verdict, rather than a verdict computed against nothing.
//
// The comparison is EQUALITY, never ordering. "Newer" and "older" would need
// per-vendor precedence rules this package has no business inventing, and the
// only thing the reader has to act on is that the two differ. So no line here
// claims a direction, and a downgraded vendor produces the same notice as an
// upgraded one — which is correct, because both mean the survey was measured
// somewhere else.
func surveyNote(p Pin, installed string) (note string, drifted bool) {
	if p.VerifiedAgainst == "" {
		// No surveyed adapter behind this seat. Silence is right: a line saying
		// "measured at nothing" invents a survey that was never done.
		return "", false
	}
	at := p.VerifiedAgainst
	if p.Section != "" {
		at += " (" + p.Section + ")"
	}

	if p.Incomparable != "" {
		// Stated rather than skipped. A seat that quietly grew no pin line would
		// read as a seat nobody thought about.
		return at + "; not compared here — " + p.Incomparable, false
	}
	if installed == "" {
		return at + "; this machine's version was not read, so nothing is claimed about drift " +
			"in either direction", false
	}

	pinned, here := versionToken(p.VerifiedAgainst), versionToken(installed)
	if pinned == "" || here == "" {
		// One of the two strings carries no version number. Saying so beats
		// comparing whole strings that were never going to match.
		return at + "; this machine reports " + installed + ", which carries no version number " +
			"this can compare, so nothing is claimed about drift", false
	}
	if pinned == here {
		return at + ", and that is the build installed here", false
	}
	return at + ", but this machine reports " + installed + " — re-measure " + reMeasure(p) +
		" before trusting the fields this adapter sources", true
}

// reMeasure names what to re-open. It falls back to the vendor's own survey
// wording when no section is recorded, rather than printing an empty pointer.
func reMeasure(p Pin) string {
	if s := strings.TrimSpace(p.Section); s != "" {
		return s
	}
	return "this adapter's survey in docs/design.md"
}
