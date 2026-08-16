package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council"
	"github.com/sanlee-ys/telltale/internal/gatehook"
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

// TestTheFirstFrameIsShortAndNamesTheModeThatMeasures pins the zero-config
// entry point (design.md §7.7, 2026-08-15).
//
// Measured before the change: a bare `telltale` printed all of `usageText` —
// 203 lines — on stderr, and exited 2. Nothing in it was untrue; it was still
// the answer that strands a stranger, because the one mode that tells them
// anything about THEIR machine was entry eight of eight, sixty lines down,
// under the word "preflight". The two properties asserted here are what fixed
// that, and both are easy to lose to a later edit that "just adds one more
// paragraph".
func TestTheFirstFrameIsShortAndNamesTheModeThatMeasures(t *testing.T) {
	lines := strings.Count(firstFrameText, "\n") + 1
	if lines > 30 {
		t.Errorf("the first frame is %d lines; it exists because 203 was too many, "+
			"and a frame nobody reads to the end is the manual again", lines)
	}
	for _, want := range []string{"telltale doctor", "telltale hud", "telltale council",
		"telltale statusline", "telltale help", "telltale version"} {
		if !strings.Contains(firstFrameText, want) {
			t.Errorf("the first frame never names %q", want)
		}
	}
	// `telltale help` has to be a command, not a suggestion. Before this frame
	// existed every route to usageText was an error path, so the pointer would
	// have been the frame inventing one.
	if !strings.Contains(usageText, "telltale help") {
		t.Error("the long help does not name `telltale help`, which the first frame sends readers to")
	}
}

// TestTheFirstFrameClaimsNothingAboutThisMachine. main() has stat'd no store
// and resolved no binary by the time this prints, so any sentence about what is
// installed, configured or missing would be the invented claim ADR-001 refuses.
// The frame's whole job is to point at the modes that DO measure.
func TestTheFirstFrameClaimsNothingAboutThisMachine(t *testing.T) {
	for _, forbidden := range []string{
		"not configured", "no vendor", "not installed", "nothing is set up", "not detected",
	} {
		if strings.Contains(strings.ToLower(firstFrameText), forbidden) {
			t.Errorf("the first frame asserts %q, which nothing has measured at this point", forbidden)
		}
	}
}

// TestSnapshotFailsLoudOnWhatItCannotDo pins the flag contract of the one mode
// whose reader is a program.
//
// It matters more here than in the interactive modes. A person who mistypes a
// HUD flag sees the wrong screen and retries; a script that mistypes a snapshot
// flag would, if the flag were ignored, receive a well-formed document that
// answers a different question — and nothing downstream can tell. So every
// input this mode cannot honour is an error with the correction in it, and the
// document is never printed.
func TestSnapshotFailsLoudOnWhatItCannotDo(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--json"}, "not defined"},
		{"positional argument", []string{"claude"}, "unexpected argument"},
		{"unknown vendor", []string{"--vendor", "chatgpt"}, "unknown --vendor"},
		{"zero timeout", []string{"--timeout", "0"}, "positive duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runSnapshot(tc.args)
			if err == nil {
				t.Fatalf("runSnapshot(%v) printed a document instead of refusing", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry the correction %q", err, tc.want)
			}
		})
	}
}

// TestUsageNamesTheSnapshotMode: the mode exists, so the help has to say so.
// Same failure shape as TestUsageNamesEverySeat above — a reader who does not
// see it in the help never runs it.
func TestUsageNamesTheSnapshotMode(t *testing.T) {
	for _, want := range []string{"telltale snapshot", "--compact", "unsupported"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage text never mentions %q", want)
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

// TestHookGateWritesTheDecisionAndNothingElse is the end-to-end assertion for
// the mode nobody types.
//
// The unit test in internal/gatehook pins the JSON. This one pins the WIRING —
// that `hook gate` reaches it at all, and that stdout carries the decision and
// only the decision. That second half is why this exists as a separate test: a
// hook's stdout IS its result, so one stray banner or debug line printed
// anywhere on this path is not noise, it is a malformed decision. Claude Code
// then reads no decision, and every tool call on the gated seat runs while the
// column still says nothing runs without a keystroke.
func TestHookGateWritesTheDecisionAndNothingElse(t *testing.T) {
	stdin, stdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()

	// A realistic payload, because the mode has to drain it: the vendor writes
	// the tool call down this pipe and a hook that exits without reading gives
	// it a broken pipe instead of an answer.
	in, inw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.WriteString(inw, `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"mkdir zzz"}}`)
		inw.Close()
	}()
	out, outw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = in, outw

	runHook([]string{gatehook.Verb})
	outw.Close()

	got, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(gatehook.Decision()) {
		t.Errorf("stdout = %q, want exactly %q", got, gatehook.Decision())
	}
}
