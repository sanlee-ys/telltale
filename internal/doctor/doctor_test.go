package doctor

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Fixtures here are synthesized: fake vendor ids, fake paths, realistic shape
// only (CLAUDE.md — this repository is public). Nothing below is read off the
// machine running the test, which is also what lets these assertions mean the
// same thing on a box with five vendors and on CI with none.

func seat(name string) Seat {
	return Seat{
		Vendor: name, Label: strings.ToUpper(name[:1]) + name[1:],
		Found:  true,
		Binary: filepath.Join(`C:\`, "fake", name+".exe"), Source: "on PATH",
		Drivable: true, DrivableDetail: "a native executable",
		Capability:  "streams its reply as it is written",
		VersionArgs: []string{"--version"},
	}
}

// answering is a probe that always succeeds, so a test can vary one seat
// without every other cell going red for an unrelated reason.
func answering(version string) Probe {
	return func(string, []string) ProbeResult {
		return ProbeResult{Out: version, Took: 120 * time.Millisecond}
	}
}

func find(t *testing.T, r Report, vendor, check string) Check {
	t.Helper()
	for _, s := range r.Seats {
		if s.Vendor != vendor {
			continue
		}
		for _, c := range s.Checks {
			if c.Name == check {
				return c
			}
		}
		t.Fatalf("%s has no %q check", vendor, check)
	}
	t.Fatalf("no seat %q in the report", vendor)
	return Check{}
}

// TestNotCheckedIsTheZeroStatus is the landmine test, and it is the same
// finding design.md §9.17 recorded for GateOff: a safety property whose default
// is the reassuring answer is the wrong way round however carefully the
// constructor sets it.
//
// Every Check a caller builds as a literal, and every field a future change
// forgets to fill in, must read as "nobody looked" rather than as a pass. If
// Passed were iota, a zero Check would claim a vendor is fine on the strength
// of nothing at all — which is exactly the report this mode exists to not be.
func TestNotCheckedIsTheZeroStatus(t *testing.T) {
	var zero Check
	if zero.Status != NotChecked {
		t.Fatalf("the zero Check reads %v; a forgotten field must never be a pass", zero.Status.Word())
	}
	if zero.Value != "" {
		t.Error("the zero Check carries a value")
	}
	var zeroReport Report
	if p, f, n := zeroReport.Tally(); p+f+n != 0 {
		t.Errorf("an empty report tallies %d/%d/%d, want nothing at all", p, f, n)
	}
}

// TestThreeStatesSurviveOneReport: the point of three states is that they stay
// three. One machine, one run, all three outcomes on the surfaces they actually
// arrive from — a resolved binary, a binary council refuses to drive, and one
// that is not there.
func TestThreeStatesSurviveOneReport(t *testing.T) {
	present := seat("claude")

	refused := seat("codex")
	refused.Drivable = false
	refused.DrivableDetail = ""
	refused.Note = "found C:\\fake\\codex.cmd, a shell shim that takes its prompt as an argument"

	absent := seat("grok")
	absent.Found = false
	absent.Binary = ""
	absent.Note = "not found on PATH (looked for grok)"

	r := Run([]Seat{present, refused, absent}, answering("1.2.3"))

	if got := find(t, r, "claude", "binary").Status; got != Passed {
		t.Errorf("a resolved binary reads %v, want ok", got.Word())
	}
	if got := find(t, r, "claude", "version").Value; got != "1.2.3" {
		t.Errorf("version = %q, want the string the vendor printed", got)
	}

	// Found and refused is NOT the same as absent, and detect.go already
	// refuses to collapse them — the fix differs and the user deserves to be
	// told which one they have. The preflight has to carry that difference
	// through: `binary` passed, `drivable` failed.
	if got := find(t, r, "codex", "binary").Status; got != Passed {
		t.Errorf("a shim that IS installed reads binary=%v, want ok", got.Word())
	}
	c := find(t, r, "codex", "drivable")
	if c.Status != Failed {
		t.Errorf("a binary council will not drive reads drivable=%v, want FAILED", c.Status.Word())
	}
	if !strings.Contains(c.Detail, "shim") {
		t.Errorf("the failure does not say why: %q", c.Detail)
	}
	// And its version is still probed. What is installed is worth knowing even
	// where the room cannot use it, and a fixed --version flag carries none of
	// the prompt text that made the seat undrivable in the first place.
	if got := find(t, r, "codex", "version").Status; got != Passed {
		t.Errorf("an undrivable seat's version reads %v; the probe is safe and should have run", got.Word())
	}

	// Absent: the binary check FAILED (it ran — detection is a stat), and
	// everything downstream is NOT CHECKED rather than failed. "We looked and
	// it is not there" and "we could not look" are different sentences.
	if got := find(t, r, "grok", "binary").Status; got != Failed {
		t.Errorf("a missing binary reads %v, want FAILED", got.Word())
	}
	for _, name := range []string{"drivable", "version"} {
		c := find(t, r, "grok", name)
		if c.Status != NotChecked {
			t.Errorf("%s on an absent seat reads %v, want not checked", name, c.Status.Word())
		}
		if c.Detail == "" {
			t.Errorf("%s says nothing about why it did not run", name)
		}
		if c.Value != "" {
			t.Errorf("%s carries a value %q on a check that did not run", name, c.Value)
		}
	}
}

// TestAuthAndNetworkAreNotCheckedEvenOnAPerfectSeat is the brief's sharpest
// rule, pinned on the seat where it is most tempting to break: everything else
// about this vendor passed.
//
// A binary that exists and answers --version establishes nothing whatever about
// a login or about reachability, and a report that let those two ride along on
// the good news would be believed on the one day it was wrong.
func TestAuthAndNetworkAreNotCheckedEvenOnAPerfectSeat(t *testing.T) {
	r := Run([]Seat{seat("claude")}, answering("2.1.226 (Claude Code)"))
	for _, name := range []string{"auth", "network"} {
		c := find(t, r, "claude", name)
		if c.Status != NotChecked {
			t.Errorf("%s reads %v on a seat that is installed and answering; nothing probed it", name, c.Status.Word())
		}
		if c.Value != "" {
			t.Errorf("%s displays %q, which came from no probe", name, c.Value)
		}
		if c.Detail == "" {
			t.Errorf("%s does not say why it was not checked, which is what makes it read as a soft pass", name)
		}
	}
	// The row's reason is short and points at the argument, which is made once
	// at the end of the report — see TestTheStandingUnknownsAreArguedOnce.
	if d := find(t, r, "claude", "auth").Detail; !strings.Contains(d, "notes") {
		t.Errorf("the auth row does not point at the reason: %q", d)
	}
}

// TestNoProbeRunsWithoutABinary. A preflight that spawned a process for a
// vendor it had just reported absent would be reporting a measurement it could
// not have taken.
func TestNoProbeRunsWithoutABinary(t *testing.T) {
	absent := seat("agy")
	absent.Found = false

	spawned := 0
	Run([]Seat{absent}, func(string, []string) ProbeResult {
		spawned++
		return ProbeResult{Out: "1.1.11"}
	})
	if spawned != 0 {
		t.Errorf("%d probes ran for a vendor that is not installed, want 0", spawned)
	}
}

// TestTheProbeIsHandedTheSeatsOwnArgv is the Cursor trap in miniature: that
// seat's binary is a bundled node.exe, and asking IT for a version answers
// node's. The seat carries the argv that asks the right program, and Run must
// pass it through untouched rather than assuming `--version`.
func TestTheProbeIsHandedTheSeatsOwnArgv(t *testing.T) {
	s := seat("cursor")
	s.Binary = `C:\fake\cursor-agent\versions\2026.08.04-aaa8809\node.exe`
	s.VersionArgs = []string{`C:\fake\cursor-agent\versions\2026.08.04-aaa8809\index.js`, "--version"}

	var gotBin string
	var gotArgs []string
	Run([]Seat{s}, func(bin string, args []string) ProbeResult {
		gotBin, gotArgs = bin, args
		return ProbeResult{Out: "2026.08.04-aaa8809"}
	})
	if gotBin != s.Binary {
		t.Errorf("probed %q, want the resolved binary %q", gotBin, s.Binary)
	}
	if len(gotArgs) != 2 || gotArgs[0] != s.VersionArgs[0] {
		t.Errorf("argv = %v; the bundle must go first, or the version printed is node's", gotArgs)
	}
}

// TestAFailedProbeIsFailedNotAbsent. The vendor ran and did not answer: that is
// a measured fact about this machine and must not degrade into a skip, which
// would read as "we never asked".
func TestAFailedProbeIsFailedNotAbsent(t *testing.T) {
	r := Run([]Seat{seat("agy")}, func(string, []string) ProbeResult {
		return ProbeResult{Err: errors.New("exit status 1"), Took: 90 * time.Millisecond}
	})
	c := find(t, r, "agy", "version")
	if c.Status != Failed {
		t.Fatalf("a probe that errored reads %v, want FAILED", c.Status.Word())
	}
	if !strings.Contains(c.Detail, "exit status 1") {
		t.Errorf("the failure does not carry the vendor's own error: %q", c.Detail)
	}
	if c.Value != "" {
		t.Errorf("a failed probe displays %q", c.Value)
	}
}

// TestSilentSuccessIsAFailure: exit 0 and no output. Passing that would put an
// empty cell where a version goes, and an empty cell reads as a version this
// report could not fit — the §4a.1 collapse, one column over.
func TestSilentSuccessIsAFailure(t *testing.T) {
	r := Run([]Seat{seat("codex")}, func(string, []string) ProbeResult {
		return ProbeResult{Out: "", Took: time.Millisecond}
	})
	if got := find(t, r, "codex", "version").Status; got != Failed {
		t.Errorf("exit 0 with no output reads %v, want FAILED", got.Word())
	}
}

// TestALiveProbeAtANonexistentPathFailsAndDoesNotPanic exercises the real
// spawn path — the one part of this package that touches the operating system —
// without needing a vendor installed. A path that is not there is the case CI
// has for every seat.
func TestALiveProbeAtANonexistentPathFailsAndDoesNotPanic(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-vendor.exe")
	res := ExecProbe(2*time.Second)(missing, []string{"--version"})
	if res.Err == nil {
		t.Fatal("spawning a binary that does not exist returned no error")
	}
	if res.Out != "" {
		t.Errorf("a spawn that never happened produced output %q", res.Out)
	}

	s := seat("grok")
	s.Binary = missing
	c := find(t, Run([]Seat{s}, ExecProbe(2*time.Second)), "grok", "version")
	if c.Status != Failed {
		t.Errorf("a nonexistent binary reads %v, want FAILED", c.Status.Word())
	}
}

// TestATimeoutSaysItWasATimeout. os/exec reports a killed child as a signal or
// an exit status, and a reader who saw that would blame their vendor for a
// deadline this flag set. The one thing the message has to name is the timeout.
func TestATimeoutSaysItWasATimeout(t *testing.T) {
	// Any real program will do: the deadline is shorter than a process can be
	// created in. `go` is present wherever this test suite runs.
	res := ExecProbe(time.Nanosecond)("go", []string{"version"})
	if res.Err == nil {
		t.Skip("the probe beat a one-nanosecond deadline; nothing to assert")
	}
	if !strings.Contains(res.Err.Error(), "did not answer within") {
		t.Errorf("a timeout reports %q, which does not name the deadline as the cause", res.Err)
	}
}

// TestFirstLineKeepsOneLine. Versions are one line today on all five vendors,
// and a vendor that starts printing an update banner must not be able to reflow
// this report.
func TestFirstLineKeepsOneLine(t *testing.T) {
	cases := map[string]string{
		"2.1.226 (Claude Code)\n":                   "2.1.226 (Claude Code)",
		"codex-cli 0.147.0\r\n":                     "codex-cli 0.147.0",
		"  1.1.11  ":                                "1.1.11",
		"grok 1.0.0 (3cd0d0cbce) [stable]\nupdate!": "grok 1.0.0 (3cd0d0cbce) [stable]",
		"": "",
	}
	for in, want := range cases {
		if got := firstLine(in); got != want {
			t.Errorf("firstLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTallyKeepsSkipsOutOfTheDenominator. Folding "not checked" in with
// "failed" would overstate what is broken; folding it in with "passed" would
// overstate what is known. It is its own number because it is its own state.
func TestTallyKeepsSkipsOutOfTheDenominator(t *testing.T) {
	absent := seat("grok")
	absent.Found = false
	r := Run([]Seat{seat("claude"), absent}, answering("1.0.0"))

	passed, failed, notChecked := r.Tally()
	if passed != 3 {
		t.Errorf("passed = %d, want the three checks that ran and succeeded on the present seat", passed)
	}
	if failed != 1 {
		t.Errorf("failed = %d, want the absent seat's binary check", failed)
	}
	// auth and network on both seats, plus drivable and version on the absent
	// one. All four questions nobody answered.
	if notChecked != 6 {
		t.Errorf("notChecked = %d, want 6", notChecked)
	}
}

// TestReadyNeedsACheckToHaveActuallyPassed: a seat whose every check was
// skipped has established nothing, and counting it as ready would let a report
// of pure ignorance summarise as a clean bill.
func TestReadyNeedsACheckToHaveActuallyPassed(t *testing.T) {
	all := SeatReport{Checks: []Check{Skip("binary", "why"), Skip("version", "why")}}
	if all.Ready() {
		t.Error("a seat with nothing but skips reports ready")
	}
	one := SeatReport{Checks: []Check{Pass("binary", "C:\\fake\\x.exe", ""), Skip("auth", "why")}}
	if !one.Ready() {
		t.Error("a seat whose checks all passed does not report ready")
	}
	bad := SeatReport{Checks: []Check{Pass("binary", "C:\\fake\\x.exe", ""), Fail("version", "exit 1")}}
	if bad.Ready() {
		t.Error("a seat with a failed check reports ready")
	}
}
