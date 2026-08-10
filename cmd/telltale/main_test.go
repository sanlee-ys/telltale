package main

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council"
)

// TestUsageNamesEverySeat stops the long help from describing a smaller room
// than the one that exists.
//
// This is a regression test with a specific history: grok became the fifth
// council seat (§9.39) while `--vendor`'s help text and this usage block both
// went on naming claude, codex, agy and cursor. ParseSeats accepted `grok` the
// whole time, so the only thing wrong was the sentence telling users what to
// type — which is the worst kind of wrong for a flag, because a user who
// believes it never tries the seat.
//
// The flag help is now interpolated from council.SeatNames() and cannot drift.
// This block cannot be, because it is hand-wrapped prose at a fixed column, so
// it is pinned instead: add a seat, and this fails until the paragraph names
// it. Substring matching is deliberate — the test asserts the seat is NAMED,
// not where or how it is punctuated, so rewrapping the paragraph stays free.
func TestUsageNamesEverySeat(t *testing.T) {
	for _, seat := range council.SeatNames() {
		if !strings.Contains(usageText, seat) {
			t.Errorf("usage text never names the %q seat — `telltale council --vendor %s` works, but the help says it does not exist", seat, seat)
		}
	}
}

// TestUsageDescribesCouncilSeatsNotHudFilter guards the neighbouring trap.
//
// Two different flags spell themselves `--vendor`: council's seat roster and
// the HUD's filter, and they take DIFFERENT vocabularies. They are not drifting
// copies of one list, so a well-meaning sweep that "fixes" one to match the
// other would break both. This test states the asymmetry so it reads as
// deliberate rather than as the next thing to tidy up.
//
// The asymmetry used to run both ways: the HUD had a gemini row and no grok
// one, council a grok seat and no gemini one. internal/adapter/grok closed half
// of it — grok is now a HUD filter too, and the assertion that it was not is
// gone rather than weakened, because a test asserting an absence that has been
// deliberately filled is a test that has to be deleted the day the work lands.
// Gemini is the half that remains: it has an adapter and no seat, because
// nothing drives it headlessly from council.
func TestUsageDescribesCouncilSeatsNotHudFilter(t *testing.T) {
	if _, err := parseFilter("grok"); err != nil {
		t.Errorf("the HUD filter no longer accepts grok (%v) — internal/adapter/grok reports rows under that id", err)
	}
	if _, err := parseFilter("gemini"); err != nil {
		t.Errorf("the HUD filter no longer accepts gemini: %v", err)
	}
	if _, err := council.ParseSeats("gemini"); err == nil {
		t.Error("council now seats gemini — the council --vendor help derives from SeatNames, but this test's premise is stale")
	}
}
