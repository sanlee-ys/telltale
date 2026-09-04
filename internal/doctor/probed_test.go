package doctor

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var errNoAnswer = errors.New("did not answer within 15s")

// The probe line's one job is that a seat nobody has driven can never read like
// a seat that passed. Every test here is a way of getting that wrong.

func probedAt(day time.Time, checks ...ProbedCheck) *Probed {
	return &Probed{Version: "2.1.226 (Claude Code)", When: day, TelltaleVersion: "0.3.0", Checks: checks}
}

func allThreePassed() []ProbedCheck {
	return []ProbedCheck{
		{Name: "handshake", Status: Passed, Took: 1200 * time.Millisecond},
		{Name: "turn", Status: Passed, Took: 4800 * time.Millisecond},
		{Name: "stop", Status: Passed, Took: 400 * time.Millisecond},
	}
}

func day() time.Time { return time.Date(2026, 9, 4, 9, 0, 0, 0, time.UTC) }

// The absent case, and it is the one this line exists for. A machine where
// nothing has ever driven this seat gets the sentence saying so and the command
// that fixes it — never a blank, never a missing line, and nothing shaped like
// a pass.
func TestASeatNobodyProbedSaysNeverAndNamesTheCommand(t *testing.T) {
	r := Run([]Seat{seat("claude")}, answering("2.1.226 (Claude Code)"))
	s := seatReport(t, r, "claude")

	if !strings.Contains(s.Probe, "never") {
		t.Fatalf("probe line = %q, want it to say nobody has probed here", s.Probe)
	}
	if !strings.Contains(s.Probe, "telltale probe claude") {
		t.Errorf("probe line = %q, want the command that would fix it", s.Probe)
	}
	out := flat(render(r))
	if !strings.Contains(out, flat(probedLabel+"never")) {
		t.Errorf("the report does not carry the labelled never line:\n%s", out)
	}
	for _, word := range []string{"probed here: ok", "probed here: 2.1.226"} {
		if strings.Contains(out, word) {
			t.Errorf("an unprobed seat rendered %q, which reads as a pass:\n%s", word, out)
		}
	}
}

// The ordinary passing shape, asserted on the rendered string as well as the
// struct for this package's recorded reason: code that computes correctly and
// then prints something else passes a struct assertion just as happily.
func TestAProbedSeatNamesTheBuildTheDayAndEachCheck(t *testing.T) {
	s := seat("claude")
	s.Probed = probedAt(day(), allThreePassed()...)
	r := Run([]Seat{s}, answering("2.1.226 (Claude Code)"))

	line := seatReport(t, r, "claude").Probe
	for _, want := range []string{
		"2.1.226 (Claude Code)", "2026-09-04",
		"handshake ok 1.2s", "turn ok 4.8s", "stop ok 0.4s",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("probe line = %q, want it to carry %q", line, want)
		}
	}
	if strings.Contains(line, "re-run") {
		t.Errorf("probe line = %q, want no staleness claim when the versions match", line)
	}
	if !strings.Contains(flat(render(r)), flat(probedLabel+line)) {
		t.Errorf("the probe line is not on the rendered report:\n%s", render(r))
	}
}

// A failed probe reports WHICH check failed and leaves the checks under it
// unrun. The reason is deliberately absent — it never reached the file — so the
// line must not imply it has one.
func TestAFailedProbeNamesTheCheckAndLeavesTheRestNotChecked(t *testing.T) {
	s := seat("codex")
	s.Probed = probedAt(day(),
		ProbedCheck{Name: "handshake", Status: Passed, Took: 900 * time.Millisecond},
		ProbedCheck{Name: "turn", Status: Failed, Took: 120 * time.Second},
		ProbedCheck{Name: "stop", Status: NotChecked},
	)
	r := Run([]Seat{s}, answering("2.1.226 (Claude Code)"))

	line := seatReport(t, r, "codex").Probe
	for _, want := range []string{"handshake ok 0.9s", "turn FAILED after 120.0s", "stop not checked"} {
		if !strings.Contains(line, want) {
			t.Errorf("probe line = %q, want it to carry %q", line, want)
		}
	}
	// A duration on a check that did not run would read as an instant pass.
	if strings.Contains(line, "stop not checked 0.0s") {
		t.Errorf("probe line = %q, want no duration on a check that did not run", line)
	}
}

// A probe recorded at a build that is no longer installed says so, in the shape
// the survey line beside it already uses. It is EQUALITY and never ordering,
// for pin.go's reason: a downgrade and an upgrade both mean the probe drove
// something else.
func TestAProbeAtAnotherBuildSaysSoAndAsksForAReRun(t *testing.T) {
	for _, installed := range []string{"2.1.230 (Claude Code)", "2.1.219 (Claude Code)"} {
		t.Run(installed, func(t *testing.T) {
			s := seat("claude")
			s.Probed = probedAt(day(), allThreePassed()...)
			r := Run([]Seat{s}, answering(installed))

			line := seatReport(t, r, "claude").Probe
			if !strings.Contains(line, "this machine now reports "+installed) {
				t.Errorf("probe line = %q, want it to name the installed build", line)
			}
			if !strings.Contains(line, "re-run `telltale probe claude`") {
				t.Errorf("probe line = %q, want the command that re-pays it", line)
			}
			for _, direction := range []string{"newer", "older", "ahead", "behind"} {
				if strings.Contains(line, direction) {
					t.Errorf("probe line = %q claims a direction with %q", line, direction)
				}
			}
		})
	}
}

// A version this run could not read makes NO claim in either direction, exactly
// as the survey line refuses to. A verdict computed against nothing is the one
// thing worse than no verdict.
func TestAnUnreadVersionMakesNoStalenessClaim(t *testing.T) {
	s := seat("claude")
	s.Probed = probedAt(day(), allThreePassed()...)
	r := Run([]Seat{s}, func(string, []string) ProbeResult {
		return ProbeResult{Err: errNoAnswer}
	})

	line := seatReport(t, r, "claude").Probe
	if strings.Contains(line, "this machine now reports") || strings.Contains(line, "re-run") {
		t.Errorf("probe line = %q, want no claim when this run read no version", line)
	}
	if !strings.Contains(line, "2.1.226 (Claude Code)") {
		t.Errorf("probe line = %q, want the build the probe itself drove", line)
	}
}

// The probe line is NOT a check, and this pins it the way TestDriftIsNotAFailedCheck
// pins the survey pin: the same seat with and without the data, and the three
// counts plus Ready required to be identical. A probe is a measurement some
// earlier run made; letting it move a count here would let a stale file redden
// a machine where every check just passed.
func TestAProbeIsNotACheck(t *testing.T) {
	bare := seat("claude")
	probed := seat("claude")
	probed.Probed = probedAt(day(),
		ProbedCheck{Name: "handshake", Status: Failed, Took: time.Second},
		ProbedCheck{Name: "turn", Status: NotChecked},
		ProbedCheck{Name: "stop", Status: NotChecked},
	)

	withOut := Run([]Seat{bare}, answering("2.1.226 (Claude Code)"))
	with := Run([]Seat{probed}, answering("2.1.226 (Claude Code)"))

	p1, f1, n1 := withOut.Tally()
	p2, f2, n2 := with.Tally()
	if p1 != p2 || f1 != f2 || n1 != n2 {
		t.Errorf("a failed probe moved the tally: %d/%d/%d became %d/%d/%d", p1, f1, n1, p2, f2, n2)
	}
	if seatReport(t, withOut, "claude").Ready() != seatReport(t, with, "claude").Ready() {
		t.Error("a failed probe changed whether the seat is ready")
	}
	// And it wears none of the three state words as a status of its own.
	line := seatReport(t, with, "claude").Probe
	if strings.HasPrefix(strings.TrimSpace(line), Passed.Word()) ||
		strings.HasPrefix(strings.TrimSpace(line), Failed.Word()) {
		t.Errorf("probe line = %q, want it not to open with a status word", line)
	}
}

// A probe file that carries no check at all is a file this report cannot read,
// and saying so beats an empty clause that reads like three silent passes.
func TestAProbeWithNoChecksSaysItCannotBeRead(t *testing.T) {
	s := seat("grok")
	s.Probed = probedAt(day())
	r := Run([]Seat{s}, answering("2.1.226 (Claude Code)"))

	line := seatReport(t, r, "grok").Probe
	if !strings.Contains(line, "no check at all") {
		t.Errorf("probe line = %q, want it to say the file carries nothing", line)
	}
}

// A probe whose own version was never read states that, rather than putting a
// date after a blank — which would read as a probe of whatever is installed
// today.
func TestAProbeWithNoVersionSaysSoRatherThanShowingABlank(t *testing.T) {
	s := seat("cursor")
	p := probedAt(day(), allThreePassed()...)
	p.Version = ""
	s.Probed = p
	r := Run([]Seat{s}, answering("2026.08.04-aaa8809"))

	line := seatReport(t, r, "cursor").Probe
	if !strings.HasPrefix(line, "a build this machine printed no version for") {
		t.Errorf("probe line = %q, want the honest blank in front of the date", line)
	}
	if strings.Contains(line, "re-run") {
		t.Errorf("probe line = %q, want no staleness claim with nothing to compare", line)
	}
}

// The line wraps inside the report's column like every other seat line. A
// sentence that runs past the wrap is the one defect a rendered assertion
// catches and a struct assertion never does.
func TestTheProbeLineNeverRunsPastTheWrapColumn(t *testing.T) {
	s := seat("claude")
	s.Probed = probedAt(day(), allThreePassed()...)
	out := Render(Run([]Seat{s}, answering("2.1.230 (Claude Code)")), Options{Width: 80})
	// Runes, not bytes, for the reason view.go's own width counter gives: an em
	// dash is three bytes and one column, and counting bytes would fail this on
	// the report's own prose.
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > 80 {
			t.Errorf("a line is %d runes wide: %q", n, line)
		}
	}
}
