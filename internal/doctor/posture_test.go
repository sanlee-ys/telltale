package doctor

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// Fixtures here are synthesized like every other in this package, and the badge
// words ARE the real vocabulary — `ro:tools`, `ro:enforced`, `ro:requested`,
// `unsandboxed` are what internal/council renders on a column. A block tested
// only against a tidy invented word would pass while breaking on every seat this
// repo actually has.

// postured returns a seat carrying a sandbox claim.
func postured(name, badge, evidence string, canGate bool) Seat {
	s := seat(name)
	s.Posture = Posture{Badge: badge, Evidence: evidence, CanGate: canGate}
	return s
}

// postureRows is every rendered line of the posture block's table, which is the
// only place a state word could leak into a claim.
func postureRows(t *testing.T, out string) []string {
	t.Helper()
	var rows []string
	in := false
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "posture — "):
			in = true
		case in && strings.TrimSpace(line) == "":
			in = false
		case in && strings.HasPrefix(line, "  "):
			rows = append(rows, line)
		}
	}
	return rows
}

// TestAPostureIsNotACheck is the honesty rule for this block, and it is pinned
// the way TestDriftIsNotAFailedCheck pins the survey pin, by the same method:
// the same seat with and without the data, and the three counts required to be
// identical.
//
// A posture is a claim this repository measured once and wrote down. Nothing
// re-measured it here. If it leaked into the three states it would be wrong in
// both directions — a FAILED would redden a working install over a vendor's own
// design decision, and an ok would claim this preflight established a
// containment property it never probed and could not probe without spending a
// turn.
func TestAPostureIsNotACheck(t *testing.T) {
	claim := "MEASURED not to restrict — a live run refuted the flags rather than " +
		"leaving them unestablished"
	r := Run([]Seat{postured("agy", "unsandboxed", claim, false)}, answering("1.1.13"))
	s := seatReport(t, r, "agy")

	for _, c := range s.Checks {
		if c.Status == Failed {
			t.Errorf("a posture failed the %q check: %+v", c.Name, c)
		}
	}
	if !s.Ready() {
		t.Error("an unsandboxed seat stopped being ready; every check that ran passed")
	}
	passed, failed, notChecked := r.Tally()
	// The three counts are exactly what they would be with no posture at all.
	bare := Run([]Seat{seat("agy")}, answering("1.1.13"))
	wp, wf, wn := bare.Tally()
	if passed != wp || failed != wf || notChecked != wn {
		t.Errorf("tally with a posture = (%d,%d,%d), without one = (%d,%d,%d)",
			passed, failed, notChecked, wp, wf, wn)
	}
	if len(s.Checks) != len(seatReport(t, bare, "agy").Checks) {
		t.Error("the posture block added a check row")
	}
	// And no row of it wears one of the three state words.
	for _, row := range postureRows(t, render(r)) {
		for _, word := range []string{" ok ", "FAILED", "not checked"} {
			if strings.Contains(row, word) {
				t.Errorf("a posture row is wearing the state word %q: %q", word, row)
			}
		}
	}
}

// TestThePostureBlockCostsNoProbe. §9.42 draws the line at cost and side effect,
// and this block is on the free side of it: every string in it arrives with the
// seat. A posture that spawned anything to confirm itself would be a second,
// wider definition of what a preflight may do, arriving as a rendering change.
func TestThePostureBlockCostsNoProbe(t *testing.T) {
	spawns := 0
	counting := func(string, []string) ProbeResult {
		spawns++
		return ProbeResult{Out: "1.0.0"}
	}
	Run([]Seat{postured("claude", "ro:tools", "enforced by construction", true)}, counting)
	with := spawns
	spawns = 0
	Run([]Seat{seat("claude")}, counting)
	if with != spawns {
		t.Errorf("a seat with a posture spawned %d processes, one without spawned %d", with, spawns)
	}
}

// TestThePostureRowsNameTheArgvTheyBelongTo. Every other line in this report
// describes the machine, and a machine is the same machine whatever the reader
// types next. A posture is not: the room WRITES by default and these badges are
// what `--read` buys, so a block of `ro:` words with no argv on it reads as what
// `telltale council` gives you — which is the opposite of true.
func TestThePostureRowsNameTheArgvTheyBelongTo(t *testing.T) {
	out := flat(render(Run(
		[]Seat{postured("claude", "ro:tools", "enforced by construction", true)},
		answering("2.1.226 (Claude Code)"))))

	if !strings.Contains(out, "telltale council --read") {
		t.Errorf("the posture block does not say which room its badges belong to:\n%s", out)
	}
	if !strings.Contains(out, "opens by default WRITES") {
		t.Errorf("the block never says the default room writes:\n%s", out)
	}
	// And it says outright that it measured none of this here, for the reason the
	// capability line is labelled: a claim in a report full of measurements is
	// read as a measurement unless it says otherwise.
	if !strings.Contains(out, "Nothing below was probed here") {
		t.Errorf("the posture block does not say it probed nothing:\n%s", out)
	}
}

// TestTheClosingDeclarationCountsTheSeatsThatCanBeAskedFirst. The count is a
// fact about canGate, and canGate has already moved once — the Cursor seat
// became a live process that can be asked and still does not ask about edits. A
// hand-written "only claude" sentence would have gone on being right by accident
// and wrong the next time.
func TestTheClosingDeclarationCountsTheSeatsThatCanBeAskedFirst(t *testing.T) {
	some := []Seat{
		postured("claude", "ro:tools", "enforced by construction", true),
		postured("codex", "ro:enforced", "enforced by an operating system", false),
		postured("grok", "unsandboxed", "measured not to restrict", false),
	}
	out := flat(render(Run(some, answering("1.0.0"))))
	if !strings.Contains(out, "1 of the 3 seats above can be asked to ask first: claude") {
		t.Errorf("the declaration does not count and name the gating seat:\n%s", out)
	}

	// The zero branch says so rather than going quiet: a block that simply
	// stopped mentioning gating on a machine with no gating seat would leave the
	// reader to assume the room asks first, which is the expensive direction.
	none := []Seat{
		postured("codex", "ro:enforced", "enforced by an operating system", false),
		postured("grok", "unsandboxed", "measured not to restrict", false),
	}
	out = flat(render(Run(none, answering("1.0.0"))))
	if !strings.Contains(out, "none of the 2 seats above can be asked to ask first") {
		t.Errorf("a room where nothing can gate does not say so:\n%s", out)
	}
	if strings.Contains(out, "carries `gated` there") {
		t.Errorf("a room with no gating seat still promised a gate:\n%s", out)
	}
}

// TestTheBadgeIsPrintedAsCouncilWordedIt. The whole point of routing this
// through council.DoctorSeats is that the badge on a column and the badge in the
// preflight are one value read twice. A render that re-worded, shortened or
// title-cased it would put the two surfaces back into a position where they can
// disagree, which is the state this block was built to make impossible.
func TestTheBadgeIsPrintedAsCouncilWordedIt(t *testing.T) {
	seats := []Seat{
		postured("claude", "ro:tools", "enforced by construction", true),
		postured("codex", "ro:enforced", "enforced by an operating system", false),
		postured("cursor", "ro:requested", "asked for, and never observed", false),
		postured("grok", "unsandboxed", "measured not to restrict", false),
	}
	out := render(Run(seats, answering("1.0.0")))
	for _, badge := range []string{"ro:tools", "ro:enforced", "ro:requested", "unsandboxed"} {
		if !strings.Contains(out, badge) {
			t.Errorf("the badge %q is not in the report as council words it:\n%s", badge, out)
		}
	}
	// `unsandboxed` breaks the `ro:` prefix on purpose (state.go). Nothing here
	// may put it back.
	if strings.Contains(out, "ro:none") || strings.Contains(out, "ro:unsandboxed") {
		t.Errorf("the block gave an unsandboxed seat an ro: prefix:\n%s", out)
	}
}

// TestASeatWithNoPostureSaysSoRatherThanGoingQuiet. A seat missing from a
// posture table reads as a seat with nothing to declare. This one has an
// unanswered question instead, and the row has to carry the difference — the
// same distinction Skip carries inside the check block, in a place the three
// state words are not allowed to go.
func TestASeatWithNoPostureSaysSoRatherThanGoingQuiet(t *testing.T) {
	seats := []Seat{
		postured("claude", "ro:tools", "enforced by construction", true),
		seat("pi"),
	}
	out := render(Run(seats, answering("1.0.0")))
	row := ""
	for _, line := range postureRows(t, out) {
		if strings.HasPrefix(strings.TrimSpace(line), "pi ") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("the seat with no posture was dropped from the block entirely:\n%s", out)
	}
	if !strings.Contains(row, "no claim") {
		t.Errorf("the blank row does not say the claim is missing: %q", row)
	}
	if strings.Contains(flat(out), "not checked either") {
		t.Errorf("the blank row borrowed a state word:\n%s", out)
	}
}

// TestAReportWithNoPostureRendersNoBlock. Silence here is right for pin.go's
// reason: a header over five `no claim` rows is a block about nothing, and a
// closing declaration explaining a table nobody was shown is the padding that
// stops reports being read to the bottom.
func TestAReportWithNoPostureRendersNoBlock(t *testing.T) {
	out := render(Run([]Seat{seat("claude"), seat("codex")}, answering("1.0.0")))
	for _, unwanted := range []string{"posture — ", "opens by default WRITES"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("a report with no posture data printed %q:\n%s", unwanted, out)
		}
	}
}

// TestNoPostureLineRunsPastTheWrapColumn. The block is laid out on the check
// block's own columns, so a badge that overflowed its cell would push the
// evidence text past the wrap rather than truncating — invisible in a terminal,
// obvious in the file this gets piped to.
//
// Only the lines this block ADDED are held to the column, and they are found by
// diffing against the same seats rendered with their postures stripped. The
// alternative — asserting over the whole report — measures the wrap floor that
// was already there (`textWidth` bottoms out at 20, and the title is a fixed 70
// columns), so it fails on correct output at a narrow width and says nothing
// about this change.
func TestNoPostureLineRunsPastTheWrapColumn(t *testing.T) {
	seats := []Seat{
		postured("antigravity", "ro:requested",
			"ASKED FOR, and never observed — the flag was accepted and what it enforces on "+
				"this machine is not established. Weaker than a construction or an OS "+
				"sandbox, and says so", false),
		postured("claude", "ro:tools", "enforced by construction", true),
	}
	bare := make([]Seat, len(seats))
	for i, s := range seats {
		s.Posture = Posture{}
		bare[i] = s
	}
	with, without := Run(seats, answering("1.0.0")), Run(bare, answering("1.0.0"))

	for _, cols := range []int{80, 60} {
		already := map[string]bool{}
		for _, line := range strings.Split(Render(without, Options{Width: cols}), "\n") {
			already[line] = true
		}
		for _, line := range strings.Split(Render(with, Options{Width: cols}), "\n") {
			n := utf8.RuneCountInString(line)
			if already[line] || n <= cols || len(strings.Fields(strings.TrimSpace(line))) <= 1 {
				continue
			}
			t.Errorf("at --width %d a posture line ran to %d columns: %q", cols, n, line)
		}
	}
}
