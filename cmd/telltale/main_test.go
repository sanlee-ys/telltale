package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council"
	"github.com/sanlee-ys/telltale/internal/gatehook"
	"github.com/sanlee-ys/telltale/internal/history"
	"github.com/sanlee-ys/telltale/internal/mcpserver"
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

// TestFirstFrameAndUsageLeadWithTheRoom pins the identity line's word order on
// the two surfaces a stranger meets first — the bare `telltale` frame and
// `telltale help`. README.md leads with the room ("A dispatch room for your
// coding agents") and states the gauge second, underneath it; this test holds
// `firstFrameText` and `usageText` to the same order rather than letting a
// later edit slide the gauge back to the front, one clause at a time.
func TestFirstFrameAndUsageLeadWithTheRoom(t *testing.T) {
	for name, text := range map[string]string{"firstFrameText": firstFrameText, "usageText": usageText} {
		room := strings.Index(text, "dispatch room")
		if room == -1 {
			t.Errorf("%s never says \"dispatch room\" — the identity line no longer leads with the room", name)
			continue
		}
		gauge := strings.Index(text, "honest gauge")
		if gauge == -1 {
			t.Errorf("%s never says \"honest gauge\" — the gauge clause was dropped, not just reordered", name)
			continue
		}
		if gauge < room {
			t.Errorf("%s says \"honest gauge\" before \"dispatch room\" — the room must lead, the gauge is the second clause", name)
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

// TestOtelRefusesATakenPortWithTheWayOut pins the collector's most likely
// startup failure at the level the operator meets it.
//
// Measured 2026-08-16 on Windows 11, main at 4e0cf6b, with a throwaway listener
// holding 127.0.0.1:4318: `telltale otel grok` printed the raw bind error
// ("Only one usage of each socket address …") and exited 1. The exit code was
// already right and it stays 1 — this switch turns any error from runOtel into
// os.Exit(1) — so what the fix changes is the sentence, not the code.
//
// The test drives runOtel rather than the built binary because the exit is one
// line of the switch above and a spawned process would be the slow way to
// assert it. What it does assert is that the mode REFUSES: a runOtel that
// returned nil here would be a collector that had bound nothing and gone on
// running.
func TestOtelRefusesATakenPortWithTheWayOut(t *testing.T) {
	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	err = runOtel([]string{"grok", "--addr", holder.Addr().String()})
	if err == nil {
		t.Fatal("runOtel returned nil on a held port: the mode would run without listening")
	}
	for _, want := range []string{
		holder.Addr().String(),
		"already in use",
		"--addr",
		"OTEL_EXPORTER_OTLP_ENDPOINT",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never says %q:\n%s", want, err)
		}
	}
}

// TestUsageNamesTheOtelPortCollision: the flag help has to carry the same two
// halves the error does. A reader who moves only the collector gets a process
// that listens forever and counts nothing, which reads like "grok spent
// nothing" rather than like a misconfiguration.
func TestUsageNamesTheOtelPortCollision(t *testing.T) {
	for _, want := range []string{"OTEL_EXPORTER_OTLP_ENDPOINT", "counts nothing"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage text never mentions %q", want)
		}
	}
}

// TestEventsRefusesATakenPortWithTheWayOut is the sink's version of the
// collector test above, and it exists because the sink had the same residue.
//
// Measured 2026-08-16 on Windows 11, main at 1995b34, with a throwaway listener
// holding 127.0.0.1:4519: `telltale events` printed the raw bind error ("Only
// one usage of each socket address …") and exited 1. The exit code was already
// right and it stays 1, so what changed is the sentence.
//
// The home directory is redirected because runEvents opens the real store
// before it binds: without this the test would read the operator's own event
// log, and this repo's fixtures are synthesized (CLAUDE.md). USERPROFILE is
// what os.UserHomeDir reads on Windows — the primary target — and HOME is what
// it reads elsewhere, so both are set rather than one guessed.
func TestEventsRefusesATakenPortWithTheWayOut(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	holder, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()

	err = runEvents([]string{"--addr", holder.Addr().String()})
	if err == nil {
		t.Fatal("runEvents returned nil on a held port: the mode would run without listening")
	}
	for _, want := range []string{
		holder.Addr().String(),
		"already in use",
		"--addr",
		"--server-url",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal never says %q:\n%s", want, err)
		}
	}
}

// TestUsageNamesTheEventSinkPortCollision: same contract as the collector's
// help. A reader who moves only the sink gets a server that listens forever
// and stores nothing, and the emitters report that by exiting 0 — so the help
// has to carry the emitter half, not just the flag.
func TestUsageNamesTheEventSinkPortCollision(t *testing.T) {
	for _, want := range []string{"--server-url", "stores nothing"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage text never mentions %q", want)
		}
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

// TestUsageNamesTheMCPMode, and names the two things a reader cannot guess: the
// tool's name, and the fact that this mode is wired into a client rather than
// typed. A mode nobody can see in the help is a mode nobody runs, and this one
// has no interactive surface at all to stumble into.
func TestUsageNamesTheMCPMode(t *testing.T) {
	for _, want := range []string{"telltale mcp", mcpserver.ToolName, "mcp add"} {
		if !strings.Contains(usageText, want) {
			t.Errorf("usage text never mentions %q", want)
		}
	}
}

// TestUsageNamesTheHistoryModeAndWhatItCannotAnswer.
//
// This mode's help carries a load its neighbours' do not, and it is the reason
// this test exists rather than being a third copy of the two above. `telltale
// history` reads ONE vendor of seven. A help entry that named the mode and left
// the coverage out would send a reader to a table headed "claude" and let them
// carry it away as a fleet answer — which is the exact failure the report's own
// coverage block is built to prevent, arriving one surface earlier.
func TestUsageNamesTheHistoryModeAndWhatItCannotAnswer(t *testing.T) {
	if !strings.Contains(usageText, "telltale history") {
		t.Fatal("usage text never mentions telltale history")
	}
	// Every vendor the survey knows has to be named in the help's entry, covered
	// or not, for the reason internal/history's own TestEveryFleetVendorHasAVerdict
	// exists: silence about a vendor on a spend surface reads as zero.
	entry := usageText[strings.Index(usageText, "telltale history"):]
	for _, c := range history.Survey() {
		if !strings.Contains(entry, string(c.Vendor)) {
			t.Errorf("the history help never names %s, so a reader cannot tell whether it is "+
				"covered, uncovered, or forgotten", c.Vendor)
		}
	}
	// And the two refusals, because both are things a reader will otherwise
	// assume the mode does and be quietly wrong about.
	for _, want := range []string{"never sums two", "no denominator"} {
		if !strings.Contains(entry, want) {
			t.Errorf("the history help never says %q", want)
		}
	}
}

// TestHistoryFailsLoudOnWhatItCannotDo. The flag contract matters here for
// snapshot's reason and one more: every number this mode prints belongs to
// exactly one vendor, so a --vendor that was silently ignored would put a table
// of claude's counts under a heading somebody typed "codex" into.
func TestHistoryFailsLoudOnWhatItCannotDo(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--json"}, "not defined"},
		{"positional argument", []string{"claude"}, "unexpected argument"},
		{"unknown vendor", []string{"--vendor", "chatgpt"}, "unknown --vendor"},
		{"zero days", []string{"--days", "0"}, "positive number of days"},
		{"zero timeout", []string{"--timeout", "0"}, "positive duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runHistory(tc.args)
			if err == nil {
				t.Fatalf("runHistory(%v) printed a report instead of refusing", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry the correction %q", err, tc.want)
			}
		})
	}
}

// TestARefusedVendorGetsTheSurveysVerdict. "unsupported" sends a reader to open
// an issue about work that is already understood. The refusal here hands back
// the survey's own sentence — the field, the file and what is missing — so the
// reader finds out whether the gap is telltale's or the vendor's without asking.
func TestARefusedVendorGetsTheSurveysVerdict(t *testing.T) {
	for _, c := range history.Survey() {
		err := historyVendor(string(c.Vendor))
		if c.Covered {
			if err != nil {
				t.Errorf("%s is covered and was refused: %v", c.Vendor, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s is not covered and was accepted", c.Vendor)
			continue
		}
		// The first clause of the verdict, which proves the reason travelled
		// rather than being replaced by a generic sentence.
		head := strings.Join(strings.Fields(c.Why)[:5], " ")
		if !strings.Contains(strings.Join(strings.Fields(err.Error()), " "), head) {
			t.Errorf("%s's refusal does not carry its verdict.\ngot:  %v\nwant it to contain: %q",
				c.Vendor, err, head)
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
	if _, err := parseFilter("pi"); err != nil {
		t.Errorf("the HUD filter does not accept pi (%v) — internal/adapter/pi reports rows under that id", err)
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

// TestMCPFailsLoudOnWhatItCannotDo is `snapshot`'s flag contract, for the mode
// whose reader is an agent — and the argument for it is one step stronger here.
// Nobody types this command: it is written once into an MCP client's config and
// then never read again, so a flag that is silently ignored stays wrong for as
// long as the client is wired up.
func TestMCPFailsLoudOnWhatItCannotDo(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"--vendor", "claude"}, "not defined"},
		{"positional argument", []string{"serve"}, "unexpected argument"},
		{"zero timeout", []string{"--timeout", "0"}, "positive duration"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := runMCP(tc.args)
			if err == nil {
				t.Fatalf("runMCP(%v) started serving instead of refusing", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not carry the correction %q", err, tc.want)
			}
		})
	}
}

// TestMCPAnswersTheHandshakeThroughTheWiring drives the mode the way its client
// does — lines of JSON-RPC down a real stdin, lines of JSON-RPC back up a real
// stdout — and asserts the parts internal/mcpserver's own tests cannot see: that
// `mcp` reaches the server at all, and that this path prints nothing but
// protocol frames. One stray banner on this stdout is not noise, it is a frame
// the client cannot parse, the same way one stray line breaks `hook gate` above.
//
// It stops at tools/list on purpose. A tools/call here would run a real scan of
// whatever machine the suite is on, and the call's document is already pinned
// against a fixture in internal/mcpserver and against the built binary in CI.
func TestMCPAnswersTheHandshakeThroughTheWiring(t *testing.T) {
	stdin, stdout := os.Stdin, os.Stdout
	defer func() { os.Stdin, os.Stdout = stdin, stdout }()

	in, inw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		io.WriteString(inw, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`+"\n")
		io.WriteString(inw, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")
		inw.Close()
	}()
	out, outw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin, os.Stdout = in, outw

	done := make(chan error, 1)
	go func() {
		done <- runMCP(nil)
		outw.Close()
	}()
	got, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("runMCP returned %v; a closed stdin is a clean end of session", err)
	}

	lines := strings.Split(strings.TrimRight(string(got), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("two requests produced %d lines of stdout; this path carries protocol frames and nothing else:\n%s", len(lines), got)
	}
	for _, line := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("stdout carried a line no client could parse: %q (%v)", line, err)
		}
	}
	if !strings.Contains(lines[1], mcpserver.ToolName) {
		t.Errorf("tools/list did not name %s through the wiring: %s", mcpserver.ToolName, lines[1])
	}
}
