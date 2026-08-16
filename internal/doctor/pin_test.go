package doctor

import (
	"errors"
	"strings"
	"testing"
)

// Fixtures here are synthesized like every other in this package: fake vendor
// ids, realistic shape only. The version strings ARE real shapes, though —
// "agy 1.1.13", "2.1.226 (Claude Code)", "grok 1.0.0 (3cd0d0cbce) [stable]" are
// the forms measured off these binaries on 2026-08-09 (internal/council's
// versionArgs doc). A pin comparison tested only against tidy `1.2.3` strings
// would pass while failing on every vendor this repo actually has.

// pinned returns a seat carrying a survey pin.
func pinned(name, verifiedAgainst, section string) Seat {
	s := seat(name)
	s.Pin = Pin{VerifiedAgainst: verifiedAgainst, Section: section}
	return s
}

// flat collapses whitespace so a rendered paragraph can be compared against the
// constant it was wrapped from.
//
// Worth the three lines: the first draft of these tests looked for driftNote
// verbatim in the output, which the wrap had already broken across lines. The
// positive assertion failed loudly — but the NEGATIVE one ("this note must not
// appear") passed for free, and would have gone on passing whatever the report
// printed. A guard that cannot fail is the thing this repository keeps writing
// tests about.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func seatReport(t *testing.T, r Report, vendor string) SeatReport {
	t.Helper()
	for _, s := range r.Seats {
		if s.Vendor == vendor {
			return s
		}
	}
	t.Fatalf("no seat %q in the report", vendor)
	return SeatReport{}
}

// TestADriftedVersionNamesTheSectionToReMeasure is the path this whole file
// exists for. The vendor self-updated past the build its field map was surveyed
// at, and nothing anywhere else in the program can notice: the adapter still
// parses, the row still renders, and CI has no vendor installed to compare
// against.
func TestADriftedVersionNamesTheSectionToReMeasure(t *testing.T) {
	r := Run([]Seat{pinned("antigravity", "agy 1.1.13", "§3.8")}, answering("1.1.14"))
	s := seatReport(t, r, "antigravity")

	if !s.Drifted {
		t.Fatalf("agy 1.1.14 against a pin of agy 1.1.13 reported no drift: %q", s.Survey)
	}
	out := render(r)
	for _, want := range []string{"agy 1.1.13", "§3.8", "1.1.14", "re-measure"} {
		if !strings.Contains(out, want) {
			t.Errorf("the drift notice does not carry %q:\n%s", want, out)
		}
	}
	// Asserted on the rendered string and not only on the struct, for this
	// package's own recorded reason: code that computes correctly and then prints
	// something else passes a struct assertion just as happily.
	if !strings.Contains(out, surveyLabel) {
		t.Errorf("the pin line is unlabelled, so it reads as a check:\n%s", out)
	}
}

// TestDriftIsNotAFailedCheck is the honesty rule for this feature, and it is the
// one that would be easiest to break by accident.
//
// A stale survey is a fact about this repository. The seat works, the binary
// answered, and every check that ran passed. If drift leaked into the three
// states it would redden a working vendor, move the tally, drop Ready, and — via
// any future caller that branches on failures — change an exit code that
// cmd/telltale documents as always 0.
func TestDriftIsNotAFailedCheck(t *testing.T) {
	r := Run([]Seat{pinned("codex", "codex-cli 0.146.0", "§3.2")}, answering("codex-cli 0.147.0"))
	s := seatReport(t, r, "codex")

	if !s.Drifted {
		t.Fatal("0.147.0 against a pin of 0.146.0 reported no drift")
	}
	for _, c := range s.Checks {
		if c.Status == Failed {
			t.Errorf("drift failed the %q check: %+v", c.Name, c)
		}
	}
	if !s.Ready() {
		t.Error("a drifted seat stopped being ready; the seat works and every check that ran passed")
	}
	passed, failed, notChecked := r.Tally()
	if failed != 0 {
		t.Errorf("drift moved the failed count to %d", failed)
	}
	// The three counts are exactly what they would be with no pin at all.
	bare := Run([]Seat{seat("codex")}, answering("codex-cli 0.147.0"))
	wp, wf, wn := bare.Tally()
	if passed != wp || failed != wf || notChecked != wn {
		t.Errorf("tally with a drifted pin = (%d,%d,%d), without one = (%d,%d,%d)",
			passed, failed, notChecked, wp, wf, wn)
	}
	// And the state words are still only the three.
	if strings.Contains(render(r), "STALE") {
		t.Error("drift invented a fourth state word")
	}
}

// TestAMatchingVersionSaysSoAndSaysNothingMore. A match is worth one quiet
// clause: the alternative is silence, and silence is indistinguishable from
// "this seat has no pin" and from "the comparison never ran". It must not
// borrow any of the drift wording.
func TestAMatchingVersionSaysSoAndSaysNothingMore(t *testing.T) {
	r := Run([]Seat{pinned("antigravity", "agy 1.1.13", "§3.8")}, answering("1.1.13"))
	s := seatReport(t, r, "antigravity")

	if s.Drifted {
		t.Errorf("a matching version reported drift: %q", s.Survey)
	}
	if !strings.Contains(s.Survey, "agy 1.1.13") {
		t.Errorf("the matching line does not name the build: %q", s.Survey)
	}
	out := flat(render(r))
	for _, unwanted := range []string{"re-measure", flat(driftNote)} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a matching seat printed drift wording (%q):\n%s", unwanted, out)
		}
	}
}

// TestAVersionThatCouldNotBeReadMakesNoDriftClaim — absent, never assumed, in
// EITHER direction. This is the zero-vs-absent rule at the pin: a probe that
// failed tells us nothing about the installed build, so claiming a match would
// be as wrong as claiming drift, and both would be claims built on no
// measurement at all.
func TestAVersionThatCouldNotBeReadMakesNoDriftClaim(t *testing.T) {
	dead := func(string, []string) ProbeResult {
		return ProbeResult{Err: errors.New("exit status 1")}
	}
	r := Run([]Seat{pinned("grok", "grok 1.0.4 (d846eb93d9)", "§3.9a")}, dead)
	s := seatReport(t, r, "grok")

	if s.Drifted {
		t.Errorf("an unreadable version produced a drift claim: %q", s.Survey)
	}
	if find(t, r, "grok", "version").Status != Failed {
		t.Fatal("the fixture did not actually fail the version probe")
	}
	if !strings.Contains(s.Survey, "was not read") {
		t.Errorf("the pin line does not say the version was unread: %q", s.Survey)
	}
	if strings.Contains(s.Survey, "installed here") {
		t.Errorf("the pin line claims a match it could not have measured: %q", s.Survey)
	}
}

// TestASeatWithNoBinaryNeverClaimsDrift. The other unreadable branch: nothing
// was probed because there was nothing to probe.
func TestASeatWithNoBinaryNeverClaimsDrift(t *testing.T) {
	s := pinned("codex", "codex-cli 0.146.0", "§3.2")
	s.Found, s.Drivable = false, false
	s.Note = "not on PATH"

	got := seatReport(t, Run([]Seat{s}, answering("codex-cli 0.147.0")), "codex")
	if got.Drifted {
		t.Errorf("a seat with no binary claimed drift: %q", got.Survey)
	}
}

// TestAnIncomparablePinSaysWhyRatherThanGoingQuiet is the Cursor case, and it is
// the reason Pin carries a reason string instead of a bool.
//
// That seat's pin names the Cursor application; what doctor probes is
// cursor-agent, which versions on a date-stamped scheme. Comparing 3.14.7 with
// 2026.08.04 would manufacture a permanent drift notice out of two unrelated
// numbering schemes — a notice that fired forever, on a correct install, which
// is precisely the report internal/adapter/drift's doc says nobody reads.
func TestAnIncomparablePinSaysWhyRatherThanGoingQuiet(t *testing.T) {
	s := pinned("cursor", "Cursor 3.14.7", "§3.9")
	s.Pin.Incomparable = "the pin names the Cursor application, and this seat's binary is cursor-agent"

	got := seatReport(t, Run([]Seat{s}, answering("2026.08.04-aaa8809")), "cursor")
	if got.Drifted {
		t.Errorf("two unrelated numbering schemes were compared and called drift: %q", got.Survey)
	}
	for _, want := range []string{"not compared", "cursor-agent"} {
		if !strings.Contains(got.Survey, want) {
			t.Errorf("the line does not say why no comparison happened (%q): %q", want, got.Survey)
		}
	}
}

// TestASeatWithNoPinRendersNoPinLine. A vendor this repo never surveyed must not
// grow a line about a survey — the zero Pin is the honest blank, exactly as the
// zero Status is.
func TestASeatWithNoPinRendersNoPinLine(t *testing.T) {
	r := Run([]Seat{seat("codex")}, answering("codex-cli 0.147.0"))
	if s := seatReport(t, r, "codex"); s.Survey != "" || s.Drifted {
		t.Errorf("an unsurveyed seat grew a pin line: %q, drifted=%v", s.Survey, s.Drifted)
	}
	if strings.Contains(render(r), surveyLabel) {
		t.Error("the report printed a pin label for a seat with no pin")
	}
}

// TestTheDriftNoteIsPrintedOnlyWhenSomethingDrifted. The two standing unknowns
// print always because they describe what this mode does; this one describes
// what it found, and a paragraph explaining a notice nobody received is padding
// in a report whose whole argument depends on being read to the bottom.
func TestTheDriftNoteIsPrintedOnlyWhenSomethingDrifted(t *testing.T) {
	quiet := Run([]Seat{pinned("antigravity", "agy 1.1.13", "§3.8")}, answering("1.1.13"))
	if strings.Contains(flat(render(quiet)), flat(driftNote)) {
		t.Error("the drift note printed with nothing drifted")
	}
	loud := Run([]Seat{pinned("antigravity", "agy 1.1.13", "§3.8")}, answering("1.1.14"))
	if !strings.Contains(flat(render(loud)), flat(driftNote)) {
		t.Error("something drifted and the note explaining what that means is missing")
	}
	// The note has to say the thing a reader would otherwise assume.
	if !strings.Contains(driftNote, "exits 0") {
		t.Error("the drift note does not say the exit code is unchanged")
	}
}

// TestTheComparisonReadsTheVendorsRealVersionShapes. Each pair below is a real
// pin from design.md §3.10 beside a real string the matching binary printed on
// this fleet. A comparison that only worked on bare semver would report drift on
// four of the five seats forever.
func TestTheComparisonReadsTheVendorsRealVersionShapes(t *testing.T) {
	for _, tc := range []struct {
		name, pin, installed string
		wantDrift            bool
	}{
		{"claude same build, words either side", "Claude Code 2.1.219", "2.1.219 (Claude Code)", false},
		{"claude moved", "Claude Code 2.1.219", "2.1.226 (Claude Code)", true},
		{"codex same", "codex-cli 0.146.0", "codex-cli 0.146.0", false},
		{"codex moved", "codex-cli 0.146.0", "codex-cli 0.147.0", true},
		{"agy bare number", "agy 1.1.13", "1.1.13", false},
		{"agy moved", "agy 1.1.13", "1.1.11", true},
		{"grok hash differs, version does not", "grok 1.0.4 (d846eb93d9)", "grok 1.0.4 (d846eb93d9) [stable]", false},
		{"grok moved", "grok 1.0.4 (d846eb93d9)", "grok 1.0.0 (3cd0d0cbce) [stable]", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, drifted := surveyNote(Pin{VerifiedAgainst: tc.pin, Section: "§3.x"}, tc.installed)
			if drifted != tc.wantDrift {
				t.Errorf("pin %q vs installed %q: drifted = %v, want %v",
					tc.pin, tc.installed, drifted, tc.wantDrift)
			}
		})
	}
}

// TestTheComparisonNeverClaimsADirection. Equality is the only claim this can
// support: "newer" and "older" need per-vendor precedence rules, and a vendor
// that renumbers its scheme would turn an invented rule into a confident lie.
// A downgrade and an upgrade must read identically.
func TestTheComparisonNeverClaimsADirection(t *testing.T) {
	up, upDrift := surveyNote(Pin{VerifiedAgainst: "agy 1.1.13", Section: "§3.8"}, "1.1.14")
	down, downDrift := surveyNote(Pin{VerifiedAgainst: "agy 1.1.13", Section: "§3.8"}, "1.1.12")
	if !upDrift || !downDrift {
		t.Fatal("one direction was not reported as drift at all")
	}
	for _, word := range []string{"newer", "older", "ahead", "behind", "outdated"} {
		if strings.Contains(up, word) || strings.Contains(down, word) {
			t.Errorf("the notice claims a direction it cannot establish: %q", word)
		}
	}
}

// TestAVersionlessStringIsNotComparedToAPin. A vendor that answers --version
// with a word rather than a number gets no verdict, not a verdict against a
// string that was never a version.
func TestAVersionlessStringIsNotComparedToAPin(t *testing.T) {
	note, drifted := surveyNote(Pin{VerifiedAgainst: "agy 1.1.13", Section: "§3.8"}, "unknown build")
	if drifted {
		t.Errorf("a versionless answer was called drift: %q", note)
	}
	if !strings.Contains(note, "no version number") {
		t.Errorf("the line does not say why nothing was compared: %q", note)
	}
}

// TestVersionTokenIgnoresDigitsThatAreNotAVersion. A commit hash carries digits;
// it carries no dot between them, which is the whole of why this rule can tell
// the two apart on grok's `grok 1.0.4 (d846eb93d9)`.
func TestVersionTokenIgnoresDigitsThatAreNotAVersion(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"grok 1.0.4 (d846eb93d9) [stable]", "1.0.4"},
		{"2.1.226 (Claude Code)", "2.1.226"},
		{"gemini-cli v0.53.1", "0.53.1"},
		{"codex-cli 0.146.0", "0.146.0"},
		{"d846eb93d9", ""},
		{"unknown", ""},
	} {
		if got := versionToken(tc.in); got != tc.want {
			t.Errorf("versionToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestThePinLineNeverRunsPastTheWrapColumn. The report is read in pipes and
// pastes, and the drift sentence is the longest line this mode can emit.
func TestThePinLineNeverRunsPastTheWrapColumn(t *testing.T) {
	r := Run([]Seat{pinned("antigravity", "agy 1.1.13", "§3.8")}, answering("1.1.14"))
	for _, line := range strings.Split(Render(r, Options{Width: 80}), "\n") {
		if n := len([]rune(line)); n > 80 {
			t.Errorf("a line is %d runes wide: %q", n, line)
		}
	}
}
