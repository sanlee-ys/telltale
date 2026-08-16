package doctor

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func render(r Report) string { return Render(r, Options{}) }

// TestOnlyAPassedCheckShowsAValue is the render-side half of the honesty rule,
// and it asserts on the rendered STRING rather than on the struct for this
// repo's recorded reason: a test that asks the code what it thinks passes just
// as happily when the code computes correctly and then prints the other thing.
//
// A value on a failed or unrun check is output that came from no successful
// measurement. Rendering it — even as a leftover a caller forgot to clear —
// would put a version number beside the word FAILED, which reads as "it is this
// version and something else is wrong" rather than as "we did not learn it".
func TestOnlyAPassedCheckShowsAValue(t *testing.T) {
	r := Report{Seats: []SeatReport{{
		Vendor: "codex", Label: "Codex",
		Checks: []Check{
			{Name: "version", Status: Failed, Value: "9.9.9-leftover", Detail: "exit status 1"},
			{Name: "auth", Status: NotChecked, Value: "8.8.8-leftover", Detail: "no login was probed"},
			{Name: "binary", Status: Passed, Value: `C:\fake\codex.exe`, Detail: "on PATH"},
		},
	}}}
	out := render(r)
	for _, leaked := range []string{"9.9.9-leftover", "8.8.8-leftover"} {
		if strings.Contains(out, leaked) {
			t.Errorf("a value survived onto a check that did not pass:\n%s", out)
		}
	}
	if !strings.Contains(out, `C:\fake\codex.exe`) {
		t.Errorf("the passing check's measured value is missing:\n%s", out)
	}
	// The failed check still says why. Dropping the value must not drop the row.
	if !strings.Contains(out, "exit status 1") {
		t.Errorf("the failure reason is missing:\n%s", out)
	}
}

// TestEveryStateIsAWordNotAColour. The report is piped into files and pasted
// into issues, so every distinction it makes has to survive losing every escape
// sequence — which it does here by never emitting one. CLAUDE.md's rule is that
// colour is always a SECOND signal; this surface has no first signal that is
// not a word.
func TestEveryStateIsAWordNotAColour(t *testing.T) {
	r := Report{Seats: []SeatReport{{
		Vendor: "claude", Label: "Claude Code",
		Checks: []Check{
			Pass("binary", `C:\fake\claude.exe`, "on PATH"),
			Fail("drivable", "a shell shim"),
			Skip("auth", "no login was probed"),
		},
	}}}
	out := render(r)
	if strings.Contains(out, "\x1b") {
		t.Error("the report emits ANSI escapes; it is read in pipes and pastes")
	}
	for _, word := range []string{"ok", "FAILED", "not checked"} {
		if !strings.Contains(out, word) {
			t.Errorf("the state word %q is not in the report:\n%s", word, out)
		}
	}
	// And the legend is not optional. Without it "not checked" is read as a
	// soft pass by anyone who has not been told otherwise, which is everyone
	// running this for the first time.
	for _, phrase := range []string{"Three states", "did not run"} {
		if !strings.Contains(out, phrase) {
			t.Errorf("the legend does not explain the states: %q missing", phrase)
		}
	}
}

// TestCapabilityIsLabelledAsADeclaration. What council can ask of a seat was
// measured once, against a live run, and written down — it is not re-measured
// by this preflight. Rendering it in the status column would give it a fourth
// state and imply a check that never happened.
func TestCapabilityIsLabelledAsADeclaration(t *testing.T) {
	r := Report{Seats: []SeatReport{{
		Vendor: "agy", Label: "Antigravity",
		Capability: "the reply arrives whole at the end of the turn",
		Checks:     []Check{Pass("binary", `C:\fake\agy.exe`, "on PATH")},
	}}}
	out := render(r)
	if !strings.Contains(out, "declares") || !strings.Contains(out, "did not check here") {
		t.Errorf("the capability line does not say it is a declaration:\n%s", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "the reply arrives whole") &&
			(strings.Contains(line, " ok ") || strings.Contains(line, "not checked")) {
			t.Errorf("the capability is wearing a check state: %q", line)
		}
	}
}

// TestRenderIsPureOverItsReport, the same property council's Render carries and
// for the same reason: nothing inside may read the clock, the filesystem or the
// environment, or two runs of a preflight stop being diffable against each
// other and any golden over this surface goes flaky in CI only.
func TestRenderIsPureOverItsReport(t *testing.T) {
	r := Run([]Seat{seat("claude")}, answering("2.1.226 (Claude Code)"))
	first := render(r)
	time.Sleep(2 * time.Millisecond)
	if second := render(r); second != first {
		t.Errorf("two renders of one report differ:\n%s\n---\n%s", first, second)
	}
}

// TestTheStandingUnknownsAreArguedOnce. Auth and network are the same
// non-answer on every seat, and the first draft repeated four lines of prose
// under all five of them — twenty identical lines that drowned the checks which
// actually differ. A report nobody reads to the bottom carries no warning at
// all, so the argument is made once and the ROWS stay, short, on every seat:
// what may be shortened is the prose, never the visible unanswered question.
func TestTheStandingUnknownsAreArguedOnce(t *testing.T) {
	out := render(Run([]Seat{seat("claude"), seat("codex")}, answering("1.0.0")))
	// Counted over the report with its wrapping flattened away: the assertion
	// is about how many times a sentence is SAID, and a wrap column is not
	// allowed to change that answer.
	flat := strings.Join(strings.Fields(out), " ")

	// The argument, exactly once, whatever the seat count.
	if n := strings.Count(flat, "turn costs quota"); n != 1 {
		t.Errorf("the auth argument appears %d times, want exactly 1", n)
	}
	if n := strings.Count(flat, "telltale makes no network calls"); n != 1 {
		t.Errorf("the network argument appears %d times, want exactly 1", n)
	}
	// And the row on every seat: two seats, two auth rows, two network rows.
	if n := strings.Count(flat, "auth not checked"); n != 2 {
		t.Errorf("auth rows = %d, want one per seat", n)
	}
	if n := strings.Count(flat, "network not checked"); n != 2 {
		t.Errorf("network rows = %d, want one per seat", n)
	}
}

// TestSummaryNamesTheSkipsRatherThanBuryingThem. A tally that printed only
// "12 of 14 checks passed" would fold the two unknowns into a denominator, and
// a reader would take the missing two as failures rather than as questions
// nobody asked.
func TestSummaryNamesTheSkipsRatherThanBuryingThem(t *testing.T) {
	absent := seat("grok")
	absent.Found = false
	out := render(Run([]Seat{seat("claude"), absent}, answering("1.0.0")))

	// The summary is found by its own words rather than by being the last
	// paragraph. It stopped being last when nextStep was added below it
	// (2026-08-15), and a positional assertion would have gone on passing
	// against whatever paragraph happened to be there instead.
	tail := paragraphContaining(out, "checks passed")
	for _, want := range []string{"not checked", "is not a pass", "auth and network"} {
		if !strings.Contains(tail, want) {
			t.Errorf("the summary does not say %q:\n%s", want, tail)
		}
	}
}

// paragraphContaining returns the blank-line-delimited block holding needle,
// with its wrap collapsed back to single spaces. The wrap column is a rendering
// decision and a caller asserting on the WORDS must not have to know where the
// line broke — otherwise every reflow of this prose breaks a test that has no
// opinion about reflow.
func paragraphContaining(out, needle string) string {
	for _, p := range strings.Split(out, "\n\n") {
		flat := strings.Join(strings.Fields(p), " ")
		if strings.Contains(flat, needle) {
			return flat
		}
	}
	return ""
}

// TestTheReportSaysWhatToRunNext. The preflight is the mode a stranger reaches
// first, and on a machine with no vendor CLI its whole report is FAILED rows
// under a count that opens "0 checks passed" — true, and silent about both what
// to do and about telltale working correctly. Every branch names a command.
func TestTheReportSaysWhatToRunNext(t *testing.T) {
	absent := seat("grok")
	absent.Found = false

	none := render(Run([]Seat{absent}, answering("1.0.0")))
	next := paragraphContaining(none, "What runs next")
	if next == "" {
		t.Fatalf("a report with no seatable vendor never says what to run:\n%s", none)
	}
	// It must not read as telltale being broken, and it must leave the reader a
	// command that works on the machine it just described.
	for _, want := range []string{"install one", "telltale hud"} {
		if !strings.Contains(next, want) {
			t.Errorf("the no-seat next step does not say %q:\n%s", want, next)
		}
	}

	some := render(Run([]Seat{seat("claude")}, answering("1.0.0")))
	next = paragraphContaining(some, "What runs next")
	for _, want := range []string{"telltale council", "telltale hud", "telltale statusline"} {
		if !strings.Contains(next, want) {
			t.Errorf("the ready next step does not name %q:\n%s", want, next)
		}
	}
	// It says nothing the report did not measure. Auth and network are `not
	// checked` on every seat, always, so a next step promising a seat will
	// ANSWER would be the one dishonest sentence on an otherwise careful page.
	for _, forbidden := range []string{"signed in", "ready to use", "will answer"} {
		if strings.Contains(next, forbidden) {
			t.Errorf("the next step claims %q, which this mode never probed:\n%s", forbidden, next)
		}
	}
}

// TestNoLineRunsPastTheWrapColumn, except a long unbroken path — a wrapped path
// is a path nobody can copy, and copying it is the first thing anyone does with
// a preflight that says a binary is in the wrong place.
func TestNoLineRunsPastTheWrapColumn(t *testing.T) {
	longPath := `C:\Users\dev\AppData\Local\cursor-agent\versions\2026.08.04-aaa8809\node.exe`
	r := Report{Seats: []SeatReport{{
		Vendor: "cursor", Label: "Cursor",
		Checks: []Check{
			Pass("binary", longPath, "a known install location, not on this shell's PATH, "+
				"stepping over its launcher to the bundled node.exe it runs"),
			Skip("auth", authSkip),
		},
	}}}
	out := Render(r, Options{Width: 80})
	for _, line := range strings.Split(out, "\n") {
		// Runes, not bytes: the report's own prose carries em dashes and a §,
		// and counting their bytes would fail this test on correct output.
		n := utf8.RuneCountInString(line)
		if n <= 80 || strings.Contains(line, longPath) {
			continue
		}
		if len(strings.Fields(strings.TrimSpace(line))) > 1 {
			t.Errorf("a wrappable line ran to %d columns: %q", n, line)
		}
	}
}

// TestAProbesDurationRidesWithItsValue: the time a probe took is measured, so
// it may be shown — but only beside the result it belongs to. A check that ran
// no process has a zero duration, and printing "0.00s" there would invent a
// measurement of nothing.
func TestAProbesDurationRidesWithItsValue(t *testing.T) {
	r := Report{Seats: []SeatReport{{
		Vendor: "claude", Label: "Claude Code",
		Checks: []Check{
			{Name: "version", Status: Passed, Value: "2.1.226", Detail: "claude --version", Took: 420 * time.Millisecond},
			Skip("network", "no network call was made"),
		},
	}}}
	out := render(r)
	if !strings.Contains(out, "0.42s") {
		t.Errorf("the probe's measured duration is missing:\n%s", out)
	}
	if strings.Contains(out, "0.00s") {
		t.Errorf("a check that spawned nothing is showing a duration:\n%s", out)
	}
}
