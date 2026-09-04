package probe

import (
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/doctor"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The report is the OTHER half of the file's rule. file_test.go asserts that a
// failure reason never reaches the disk; this asserts that it reaches the
// operator, in the terminal where the probe ran. Either half alone is a
// different product: a reason nowhere leaves nobody able to act on a failed
// seat, and a reason on disk puts a vendor's paths in a file people paste.
func TestTheReportShowsTheReasonTheFileRefuses(t *testing.T) {
	res := Result{
		Vendor: model.VendorCodex, Label: "Codex", Version: "codex-cli 0.152.1",
		ProbedAt: stamp(),
		Checks: []Check{
			{Name: CheckHandshake, Status: doctor.Passed, Took: 900 * time.Millisecond},
			{Name: CheckTurn, Status: doctor.Failed, Took: 120 * time.Second,
				Detail: "not logged in: run `codex login`"},
			{Name: CheckStop, Status: doctor.NotChecked},
		},
	}
	out := Render([]Result{res}, "/home/x/.telltale/probe")

	// The column positions are asserted, not just the words. A status that
	// drifted out of its column would still contain every substring a looser
	// test looked for, and the whole point of the fixed grid is that two runs
	// of this report diff against each other.
	for _, want := range []string{
		"codex-cli 0.152.1",
		"  handshake  ok           0.90s",
		"  turn       FAILED       120.00s, not logged in: run `codex login`",
		"  stop       not checked\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the report does not carry %q:\n%s", want, out)
		}
	}
	// A check that did not run must carry no duration. "0.00s" beside `not
	// checked` reads as an instant pass, which is the collapse §4a.1 forbids.
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "not checked") && strings.Contains(line, "s") &&
			strings.Contains(line, "0.00") {
			t.Errorf("a `not checked` row carries a duration: %q", line)
		}
	}
}

// A skipped seat gets a row saying why. Dropping it would be worse than an
// empty row: a seat missing from this report reads as a seat that passed.
func TestASkippedSeatStillGetsARowAndNoFileLine(t *testing.T) {
	res := Result{Vendor: model.VendorCursor, Label: "Cursor", ProbedAt: stamp(),
		Skipped: "council has no adapter for this seat"}
	out := Render([]Result{res}, "/home/x/.telltale/probe")

	if !strings.Contains(out, "cursor") {
		t.Errorf("the skipped seat is missing from the report:\n%s", out)
	}
	if !strings.Contains(out, "not driven: council has no adapter") {
		t.Errorf("the report does not say why the seat was not driven:\n%s", out)
	}
	if strings.Contains(out, "written to") {
		t.Errorf("the report claims a file for a seat nothing drove:\n%s", out)
	}
}

// The warning names the seats and the word, because "all of them" is a
// different bill on a five-seat machine than on a one-seat one.
func TestTheWarningNamesTheCostTheSeatsAndTheWord(t *testing.T) {
	seats := []Seat{{Vendor: model.VendorClaude}, {Vendor: model.VendorGrok}}
	w := Warning(seats)
	for _, want := range []string{"SPENDS", "2 seats", "claude, grok", `"` + Brief + `"`, "throwaway"} {
		if !strings.Contains(w, want) {
			t.Errorf("the warning does not carry %q:\n%s", want, w)
		}
	}
	if one := Warning(seats[:1]); !strings.Contains(one, "1 seat ") {
		t.Errorf("a one-seat run reads %q", one)
	}
	if none := Warning(nil); !strings.Contains(none, "nothing would be probed") {
		t.Errorf("an empty roster reads %q", none)
	}
}
