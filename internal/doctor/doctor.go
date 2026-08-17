// Package doctor is telltale's launch-time preflight: what is installed on
// this machine, what version it reports, and — stated as plainly as the two
// facts above — what was never looked at.
//
// # Why probing is allowed here and nowhere else
//
// `internal/council`'s detection is LookPath-and-stat only, and its own doc
// says why: "Council never runs a vendor to find out whether it works: a probe
// turn costs real quota, and 'is it authenticated?' is a question the first
// real dispatch answers for free" (ADR-008 §6). That rule is about a TURN. The
// room may not spend the user's money to answer a question it does not have to
// ask, and it may not spend it silently, mid-conversation, on a schedule nobody
// asked for.
//
// A preflight is the one moment where the user HAS asked. `telltale doctor`
// exists to be run before the room opens, it runs once, it is over in under a
// second per seat, and every probe it makes is a `--version` — a flag that
// parses argv, prints a string and exits, with no model, no session and no
// billing anywhere in it. §9.17's ruling is the frame: a fact that is true at
// launch and stays true belongs at launch. What binary is on this disk and what
// it calls itself is exactly that shape.
//
// The boundary is therefore drawn at *cost and side effect*, not at "running a
// vendor is forbidden": this package spawns `<binary> --version`, and it does
// not start a turn, does not read or write a credential store, does not touch
// ~/.telltale, and makes no network call. See CLAUDE.md's read/write boundary —
// doctor adds no fourth exception to the three writes listed there, because it
// writes nothing at all.
//
// # Three states, and no fourth
//
// §4a.1 rules that a value must come from measured output and that two kinds of
// nothing must not render alike. A preflight has THREE outcomes, and collapsing
// any pair of them is the failure this package exists to prevent:
//
//   - Passed — a check ran and succeeded. It carries the measured text: the
//     path that was stat'd, the string the vendor printed.
//   - Failed — a check ran and did not succeed. It carries the reason.
//   - NotChecked — the check did not run. It carries WHY it did not, and it
//     never carries a value.
//
// A report that said "auth: ok" because a binary was on disk would be the
// zero-vs-absent bug wearing a preflight's clothes: the reader takes it as
// evidence of a login nothing verified, and the failure lands on the one day
// they trusted it. So auth and network are `NotChecked` here — always, with the
// reason stated — because nothing in this package probes either one.
//
// Status's zero value is NotChecked for `GateOff`'s reason (design.md §9.17,
// "the field that was nearly a landmine"): a safety property whose default is
// the reassuring answer is the wrong way round however carefully the
// constructor sets it. Every Check a test types out by hand, and every Check a
// future caller forgets to fill in, is an honest blank rather than a silent
// pass.
//
// This package is stdlib-only and imports nothing from the rest of telltale.
// The seat list it renders is built by `internal/council` (DoctorSeats), which
// is where detection and the capability declarations already live; keeping the
// dependency pointing that way means the report can be exercised against
// synthesized seats with no vendor, no terminal and no filesystem in the test.
package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Status is one of three, and there is no fourth.
//
// Not a bool with a nullable value beside it, which is the shape this would
// take if "did the check run" and "did it pass" were tracked separately: two
// booleans admit four states, one of which ("did not run, and passed") is
// nonsense that nothing would stop a caller constructing.
type Status uint8

const (
	// NotChecked is the ZERO VALUE, deliberately. See the package doc.
	NotChecked Status = iota
	// Passed: the check ran and succeeded.
	Passed
	// Failed: the check ran and did not succeed.
	Failed
)

// Word is how a status prints. Words, not glyphs and not colour: the report is
// plain text on stdout and has to survive being piped into a file, so the
// distinction a reader needs is carried by the only signal that always
// survives. FAILED is upper-case because it is the line worth finding by eye in
// a wall of `ok`, and case is not colour.
func (s Status) Word() string {
	switch s {
	case Passed:
		return "ok"
	case Failed:
		return "FAILED"
	default:
		return "not checked"
	}
}

// Check is one question the preflight asked, and what came back.
type Check struct {
	// Name is the question, in one lower-case word.
	Name   string
	Status Status
	// Value is MEASURED output and nothing else — a path that was stat'd, the
	// line a vendor printed. It is empty on every status but Passed, and
	// Render will not print it otherwise; see TestOnlyAPassedCheckShowsAValue.
	Value string
	// Detail is words: why a check failed, why one did not run, or what a pass
	// actually establishes. Never a value.
	Detail string
	// Took is how long a probe that actually spawned something took. Zero for
	// every check that ran no process — which is why it is rendered only
	// alongside a probe's own result, never as "0s".
	Took time.Duration
}

// Pass records a check that ran and succeeded. value is measured text and may
// be empty when the pass is a verdict rather than a reading (a binary council
// will drive); detail says what the pass establishes.
func Pass(name, value, detail string) Check {
	return Check{Name: name, Status: Passed, Value: value, Detail: detail}
}

// Fail records a check that ran and did not succeed. There is no value
// parameter on purpose: a failed check has nothing measured to show, and the
// one thing a reader needs is the reason.
func Fail(name, detail string) Check {
	return Check{Name: name, Status: Failed, Detail: detail}
}

// Skip records a check that did not run, and why. `why` is required in spirit —
// a bare "not checked" is the sentence that makes a reader assume the tool
// simply had nothing to say, rather than that nothing was looked at.
func Skip(name, why string) Check {
	return Check{Name: name, Status: NotChecked, Detail: why}
}

// Seat is one vendor as detection found it, flattened into what a preflight
// needs.
//
// It is a plain struct rather than council.VendorInfo so this package can stay
// stdlib-only and so a test can synthesize a machine that does not exist. Every
// field here is something council MEASURED (a stat, a path, a classification of
// the binary's extension) or DECLARED (Capability) — nothing in it is inferred
// by this package.
type Seat struct {
	// Vendor is the lower-case vendor id, matching model.VendorID.
	Vendor string
	Label  string
	// Found reports that detection resolved a binary and it exists.
	//
	// Separate from `Binary != ""` because those are different facts: an
	// override env var pointing at a path that is not there sets Binary and
	// finds nothing, and reporting that as found would be the report inventing
	// a file.
	Found bool
	// Binary is the resolved path, which for one seat is not the path the user
	// installed: council steps over cursor-agent's .cmd launcher to the bundled
	// node it runs. VersionArgs is what keeps that honest — see its doc.
	Binary string
	// Source is how the binary was resolved, in council's own words.
	Source string
	// Note explains a detection that did not end in a drivable binary.
	Note string
	// Drivable reports that council will actually seat this vendor. A binary
	// can be Found and not Drivable — the shim case — and the fix for the two
	// is different, which is why they are two checks and not one.
	Drivable bool
	// DrivableDetail says what makes it drivable, for the passing branch.
	DrivableDetail string
	// Capability is what council DECLARES it can ask of this seat, in the
	// room's own words. It is not a check and never wears one of the three
	// statuses: it is a claim this repo measured once against a live run and
	// wrote down, not something re-measured on this machine now. Rendered on
	// its own line, labelled, for exactly that reason.
	Capability string
	// VersionArgs is the argv AFTER Binary that asks this vendor its own
	// version.
	//
	// It is per-seat data rather than a constant `--version` because of the
	// Cursor seat, and the trap there is measured rather than theoretical. That
	// seat's Binary is a bundled node.exe; `node.exe --version` answers
	// v24.5.0, which is node's version and not cursor-agent's, and printing it
	// under a row labelled `cursor` would be a displayed value that came from
	// the wrong program entirely. Handed the bundle first — `node.exe index.js
	// --version` — the same install answers 2026.08.04-aaa8809. Both measured
	// on this machine, 2026-08-09.
	VersionArgs []string
	// Pin is the vendor build telltale's own survey of this adapter was measured
	// at (design.md §3.10). Like Capability it is DECLARED rather than measured
	// here, and like Capability it is filled by council, which is where the
	// inventory already lives. Zero for a seat with no surveyed adapter behind
	// it, which renders no pin line at all — see pin.go.
	Pin Pin
	// Posture is this seat's sandbox claim, and it is the third DECLARED field
	// on this struct for the third time the same reason: council measured it
	// once against a live run, council renders it on the column badges, and a
	// second copy here would be a table that agrees today and drifts later. See
	// posture.go.
	Posture Posture
}

// ProbeResult is what one bounded version probe produced. Out is the vendor's
// own output; Err is non-nil when the process failed to start, exited non-zero,
// or ran past its deadline.
type ProbeResult struct {
	Out  string
	Err  error
	Took time.Duration
}

// Probe runs one version probe. Injected so the report can be built over fake
// results in a test — the alternative is a test that can only run on a machine
// with five vendor CLIs installed, which is to say a test that never runs.
type Probe func(binary string, args []string) ProbeResult

// ExecProbe is the live probe: spawn the binary, hand it its version argv, and
// give it `timeout` to answer.
//
// Bounded on purpose. A preflight that can hang has failed at the one job it
// has, which is to answer before the room opens; the deadline turns a wedged
// vendor into a Failed check with a reason rather than into a terminal nobody
// can get out of. The timeout is a check FAILURE and not a skip: the probe did
// run, and "this binary did not answer --version in n seconds" is a measured
// fact about this machine worth printing.
//
// Nothing here goes near a credential store or the network, and there is no
// prompt anywhere in the argv — every argument is a fixed flag or a path this
// package resolved, so a .cmd shim crossing cmd.exe carries no arbitrary text
// and none of detect.go's quoting problem applies.
func ExecProbe(timeout time.Duration) Probe {
	return func(binary string, args []string) ProbeResult {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		cmd := exec.CommandContext(ctx, binary, args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		// Explicitly nothing on stdin. A vendor that decided to read stdin
		// rather than print a version would otherwise inherit this terminal's
		// and block until the deadline; closed input makes that case fail fast
		// and honestly.
		cmd.Stdin = nil

		start := time.Now()
		err := cmd.Run()
		took := time.Since(start)

		out := firstLine(stdout.String())
		if out == "" {
			// Some CLIs answer --version on stderr. Preferring stdout and
			// falling back is the honest order: this reports what the process
			// actually wrote, wherever it wrote it, rather than declaring a
			// version absent because it arrived on the other stream.
			out = firstLine(stderr.String())
		}
		if err != nil && ctx.Err() != nil {
			// os/exec reports a killed child as "signal: killed" or, on
			// Windows, as an exit status — neither of which says the deadline
			// is what killed it. Naming the timeout is the difference between a
			// reader blaming their vendor and a reader reaching for --timeout.
			err = fmt.Errorf("did not answer within %s", timeout)
		}
		return ProbeResult{Out: out, Err: err, Took: took}
	}
}

// firstLine is the version string as the vendor printed it, trimmed, with
// anything after the first line dropped. Vendors print one line here (measured:
// claude "2.1.226 (Claude Code)", codex "codex-cli 0.147.0", agy "1.1.11", grok
// "grok 1.0.0 (3cd0d0cbce) [stable]", cursor-agent "2026.08.04-aaa8809"), and a
// vendor that one day prints a banner must not be able to reflow this report.
func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// SeatReport is one seat's checks, in the order they were asked.
type SeatReport struct {
	Vendor     string
	Label      string
	Capability string
	Checks     []Check
	// Survey is the pin line for this seat, already worded (pin.go), or empty
	// when no surveyed adapter stands behind it. Worded in Run rather than in
	// Render for the reason every other measurement is: Render stays pure over
	// its Report, and everything it prints arrives as data.
	Survey string
	// Drifted reports that this seat runs a version other than the one telltale
	// surveyed it at. It is NOT a check and never wears a Status — see pin.go for
	// why a staleness fact must not become a fourth state. Nothing in Tally or
	// Ready reads it, so the counts and the exit code are unchanged by it.
	Drifted bool
	// Posture is the seat's sandbox claim, carried through unchanged from the
	// Seat. Nothing in Tally or Ready reads it either, for posture.go's reason:
	// a claim about what a vendor's flags buy is not a check on this machine.
	Posture Posture
}

// Ready reports that every check that RAN on this seat passed. A seat with
// nothing but skips is not ready and does not claim to be — Ready is used to
// count, never to render a state of its own.
func (s SeatReport) Ready() bool {
	ran := false
	for _, c := range s.Checks {
		switch c.Status {
		case Failed:
			return false
		case Passed:
			ran = true
		}
	}
	return ran
}

// Report is the whole preflight.
type Report struct{ Seats []SeatReport }

// Tally counts checks by status across every seat. Three numbers because there
// are three states; a "checks passed / total" pair would fold the skips into
// the denominator and quietly re-describe "we did not look" as "we looked and
// it did not pass".
func (r Report) Tally() (passed, failed, notChecked int) {
	for _, s := range r.Seats {
		for _, c := range s.Checks {
			switch c.Status {
			case Passed:
				passed++
			case Failed:
				failed++
			default:
				notChecked++
			}
		}
	}
	return
}

// AnyDrifted reports that at least one seat runs a version other than the one it
// was surveyed at. It is deliberately separate from Tally: a stale survey is not
// a check result and must not move any of those three numbers.
func (r Report) AnyDrifted() bool {
	for _, s := range r.Seats {
		if s.Drifted {
			return true
		}
	}
	return false
}

// Auth and network are the two questions this preflight is asked most often and
// answers least: both are NotChecked on every seat, always. They are constants
// rather than strings built per seat because a reason that varied by vendor
// would read as though some vendor HAD been checked.
//
// The row keeps a SHORT reason and the argument is made once, at the end of the
// report (Notes). Repeating four lines of prose under five seats was the first
// draft, and it drowned the checks that differ per seat in twenty identical
// lines — a report nobody reads to the bottom does not carry a warning at all.
// What may not be shortened is the row itself: every seat goes on carrying both
// questions, visibly unanswered, because that is the thing a reader must not be
// able to scroll past.
const (
	authSkip    = "no login was probed — see the notes below"
	networkSkip = "no network call was made — see the notes below"

	authNote = "Auth is `not checked` on every seat, always. Signing in is not a flag " +
		"telltale can read: the only thing that establishes it is a real turn, and a " +
		"turn costs quota (ADR-008 §6). A seat that is installed and signed out " +
		"reports its own auth failure on its column the first time you dispatch to it."
	networkNote = "Network is `not checked` on every seat, always. telltale makes no " +
		"network calls — not here, and not in the gauges (CLAUDE.md, the read/write " +
		"boundary). Whether a vendor's API is reachable from this machine is unknown " +
		"to this report rather than assumed good."
)

// Run asks every check of every seat and returns the report. It spawns exactly
// one process per seat that has a binary to run, and none at all for a seat
// that does not.
func Run(seats []Seat, probe Probe) Report {
	rep := Report{Seats: make([]SeatReport, 0, len(seats))}
	for _, s := range seats {
		rep.Seats = append(rep.Seats, runSeat(s, probe))
	}
	return rep
}

func runSeat(s Seat, probe Probe) SeatReport {
	out := SeatReport{
		Vendor: s.Vendor, Label: s.Label,
		Capability: s.Capability,
		// Copied, never derived. runSeat is where a probe could be reached, and a
		// posture that was computed here would be a posture this package invented
		// — the one thing posture.go rules out.
		Posture: s.Posture,
	}

	// binary — always runs. Detection is a stat, so there is no branch in which
	// this question goes unasked.
	if s.Found {
		out.Checks = append(out.Checks, Pass("binary", s.Binary, s.Source))
	} else {
		out.Checks = append(out.Checks, Fail("binary", s.Note))
	}

	// drivable — a separate question from "is it there", because the two have
	// different fixes and detect.go already refuses to collapse them.
	switch {
	case !s.Found:
		out.Checks = append(out.Checks, Skip("drivable", "there is no binary to judge"))
	case s.Drivable:
		out.Checks = append(out.Checks, Pass("drivable", "", s.DrivableDetail))
	default:
		out.Checks = append(out.Checks, Fail("drivable", s.Note))
	}

	// version — the one probe. Run whenever a binary exists, INCLUDING one
	// council will not drive: what is installed is worth knowing either way,
	// and a fixed `--version` flag carries none of the prompt text that made
	// the seat undrivable.
	vc := versionCheck(s, probe)
	out.Checks = append(out.Checks, vc)

	// The pin comparison hangs off that probe and off nothing else. A version
	// check that did not pass hands over the empty string, and surveyNote makes
	// no drift claim on it — an unread version is absent, never assumed equal and
	// never assumed drifted.
	installed := ""
	if vc.Status == Passed {
		installed = vc.Value
	}
	out.Survey, out.Drifted = surveyNote(s.Pin, installed)

	out.Checks = append(out.Checks,
		Skip("auth", authSkip),
		Skip("network", networkSkip),
	)
	return out
}

func versionCheck(s Seat, probe Probe) Check {
	if !s.Found {
		return Skip("version", "there is no binary to run")
	}
	if probe == nil {
		// Reachable only from a caller that built a report without a probe.
		// Saying so beats spawning nothing and reporting a pass.
		return Skip("version", "no probe was configured")
	}
	res := probe(s.Binary, s.VersionArgs)
	c := Check{Name: "version", Took: res.Took}
	switch {
	case res.Err != nil:
		c.Status = Failed
		c.Detail = res.Err.Error()
		if res.Out != "" {
			c.Detail += ": " + res.Out
		}
	case res.Out == "":
		// Exit 0 and nothing printed. A pass here would put an empty cell where
		// a version goes and let it read as a version this report could not
		// display, which is the same collapse §4a.1 forbids.
		c.Status = Failed
		c.Detail = "exited 0 and printed nothing"
	default:
		c.Status = Passed
		c.Value = res.Out
		c.Detail = probeWords(s.VersionArgs)
	}
	return c
}

// probeWords is the invocation that produced a version, so the reader can run
// it themselves. A displayed value whose provenance is not printable is one
// nobody can falsify — the same argument detect.go makes for putting the
// resolved path on an "unusable" card.
//
// The binary is named as "the binary above" rather than repeated: it is on the
// `binary` row of the same seat, three lines up, and for the Cursor seat that
// path is 80 columns of version directory. What is NOT elided is the argv,
// because that is where the one interesting difference lives — the bundle this
// seat has to be handed before `--version` means anything.
func probeWords(args []string) string {
	return strings.TrimSpace("the binary above, given " + strings.Join(args, " "))
}
