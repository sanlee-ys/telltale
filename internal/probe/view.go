package probe

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sanlee-ys/telltale/internal/doctor"
)

// This file prints the probe, and it prints it as plain words for the reason
// `internal/doctor/view.go` gives: the report is read once, it is pasted into
// an issue, and every distinction it makes has to survive a pipe. No colour, no
// alternate screen, nothing for --ascii or NO_COLOR to switch off.
//
// Render is PURE over its results. Everything measured — a duration, a version
// string, a vendor's own failure sentence — is measured in the drive and
// arrives here as data, which is the same rule the room's own Render and the
// preflight's both keep.

const (
	// The two fixed columns, wide enough for the longest of each ("handshake",
	// "not checked"), plus a space. Fixed rather than measured from the data so
	// two runs of this report stay diffable against each other.
	nameCol   = 11
	statusCol = 13
	indent    = 2
)

// Warning is the sentence the operator reads BEFORE anything is driven, and it
// is the reason this mode has a confirmation at all.
//
// It names the cost in the unit the operator pays it in — one turn per seat, on
// their own account, under their own vendor credentials — and it names the
// seats, because "all of them" is a different bill on a five-seat machine than
// on a one-seat one. Every other mode in this binary reads; this one spends, and
// a mode that spends without saying so is the thing this repository refuses one
// level up (§4a.1's rule, applied to money rather than to a gauge).
func Warning(seats []Seat) string {
	names := make([]string, 0, len(seats))
	for _, s := range seats {
		names = append(names, string(s.Vendor))
	}
	if len(names) == 0 {
		return "No seat on this machine can be driven, so nothing would be probed."
	}
	return fmt.Sprintf(
		"This SPENDS a turn. `telltale probe` drives %d seat(s) — %s — through a handshake, "+
			"one turn of one word, and a stop. Each turn runs on your own vendor "+
			"credentials, on your own account, and is billed like any other turn you type. "+
			"Every seat runs in a throwaway empty directory, never in this one, and the "+
			"brief is the single word %q.",
		len(names), strings.Join(names, ", "), Brief)
}

// Render draws the whole report.
func Render(results []Result, dir string) string {
	var b strings.Builder
	b.WriteString("telltale probe — the live shape of each seat, driven here\n\n")
	b.WriteString(legend + "\n")

	for _, r := range results {
		b.WriteByte('\n')
		fmt.Fprintf(&b, "%s — %s\n", r.Vendor, r.Label)
		if r.Skipped != "" {
			// One line, outside the check grid, because a skipped seat has no
			// checks to put on it. It gets a row rather than being dropped: a
			// seat missing from this report reads as a seat that passed.
			writeIndented(&b, "not driven: "+r.Skipped)
			continue
		}
		writeIndented(&b, "version: "+versionWords(r.Version))
		for _, c := range r.Checks {
			writeCheck(&b, c)
		}
		if r.Drove() {
			writeIndented(&b, "written to "+filepath.Join(dir, string(r.Vendor)+".json"))
		}
	}

	b.WriteByte('\n')
	b.WriteString(closing + "\n")
	return b.String()
}

const legend = "Three states and no fourth, the same three `telltale doctor` prints: `ok` is a " +
	"check that ran and passed, `FAILED` is a check that ran and did not, and `not checked` " +
	"is a check that did not run. A seat stops at its first failure, so the checks under it " +
	"read `not checked` rather than being guessed at."

const closing = "What was written: the vendor, the version, the day, this telltale build, and " +
	"the three results with their milliseconds. The failure reason above is NOT written — a " +
	"vendor's own error line carries paths and session ids, and the files under " +
	"~/.telltale hold numbers and keys only. Read the reason here, where it was measured. " +
	"`telltale doctor` reports what the file holds, on each seat's own row."

// versionWords is the version, or the honest blank. An empty cell where a
// version goes reads as a version this report could not display, which is the
// collapse §4a.1 forbids.
func versionWords(v string) string {
	if strings.TrimSpace(v) == "" {
		return "this machine printed none, so nothing is claimed about the build that was driven"
	}
	return v
}

func writeCheck(b *strings.Builder, c Check) {
	prefix := strings.Repeat(" ", indent) + pad(c.Name, nameCol) + pad(c.Status.Word(), statusCol)
	text := c.Detail
	if c.Status != doctor.NotChecked {
		// The duration goes beside the status and never on its own, so a `not
		// checked` row can never carry a "0.00s" that reads as an instant pass.
		took := fmt.Sprintf("%.2fs", c.Took.Seconds())
		if text == "" {
			text = took
		} else {
			text = took + " — " + text
		}
	}
	b.WriteString(prefix)
	b.WriteString(text)
	b.WriteByte('\n')
}

func writeIndented(b *strings.Builder, s string) {
	b.WriteString(strings.Repeat(" ", indent))
	b.WriteString(s)
	b.WriteByte('\n')
}

// pad never truncates, for `doctor`'s stated reason: these are fixed
// vocabularies whose longest member fits, and a clipped state word is the one
// thing §9.11 rules out.
func pad(s string, n int) string {
	if len(s) >= n {
		return s + " "
	}
	return s + strings.Repeat(" ", n-len(s))
}
