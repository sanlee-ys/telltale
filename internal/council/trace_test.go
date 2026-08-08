package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

func clock(v model.VendorID, spawn, wait, stream time.Duration, spawned bool) runner.TurnClock {
	return runner.TurnClock{
		Vendor: v,
		At:     time.Date(2026, 8, 6, 11, 22, 33, 0, time.UTC),
		Spawn:  runner.Span{D: spawn, Measured: spawned},
		Wait:   runner.Span{D: wait, Measured: true},
		Stream: runner.Span{D: stream, Measured: true},
	}
}

// TestTraceWritesOneLinePerTurn: the file is the whole surface this feature
// has, so what lands in it is asserted rather than assumed.
func TestTraceWritesOneLinePerTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	ts := newTraceSink()
	if _, err := ts.open(path); err != nil {
		t.Fatal(err)
	}
	ts.record(clock(model.VendorCursor, 400*time.Millisecond, 41*time.Second, 2*time.Second, true))
	ts.record(clock(model.VendorClaude, 0, 3*time.Second, 9*time.Second, false))
	ts.close()

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines for 2 turns: %v", len(lines), lines)
	}
	if !strings.Contains(lines[0], "cursor") || !strings.Contains(lines[0], "spawn=400ms") {
		t.Errorf("line 1 = %q, want the cursor turn with its launch named", lines[0])
	}
	// The seat whose process outlived the turn launched nothing, and the line
	// has to say so rather than print a zero that reads like a fast launch.
	if !strings.Contains(lines[1], "spawn=-") {
		t.Errorf("line 2 = %q, want an unmeasured launch rendered as -", lines[1])
	}
}

// TestTraceAppends: the turn worth explaining is one that was slow ONCE, so a
// run that truncated the file on open would delete the evidence it exists to
// keep.
func TestTraceAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "turns.log")
	if err := os.WriteFile(path, []byte("an earlier run\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ts := newTraceSink()
	if _, err := ts.open(path); err != nil {
		t.Fatal(err)
	}
	ts.record(clock(model.VendorCodex, time.Second, time.Second, time.Second, true))
	ts.close()

	lines := readLines(t, path)
	if len(lines) != 2 || lines[0] != "an earlier run" {
		t.Fatalf("lines = %v, want the earlier run kept and the new turn appended", lines)
	}
}

// TestTraceRefusesAnUnwritablePathBeforeTheRoomOpens. Run checks this ahead of
// the alternate screen for the same reason it checks --brief there: a path that
// cannot be written is a line on stderr, not a card behind a TUI.
func TestTraceRefusesAnUnwritablePathBeforeTheRoomOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "turns.log")
	if _, err := newTraceSink().open(path); err == nil {
		t.Fatal("a trace path inside a directory that does not exist was accepted")
	}
}

// TestTraceFlushesTheTurnsItAlreadyHeld is the whole reason the ring exists.
//
// The clock runs on every turn whether or not anything is writing it down, so a
// trace opened straight after a turn nobody can explain has to be able to write
// THAT turn. Without this, enabling from inside the room would only ever catch
// the next one — which moves the prediction from launch to the previous turn
// rather than retiring it (design.md §9.17).
func TestTraceFlushesTheTurnsItAlreadyHeld(t *testing.T) {
	ts := newTraceSink()
	ts.record(clock(model.VendorClaude, 0, time.Second, time.Second, false))
	ts.record(clock(model.VendorCodex, time.Second, 44*time.Second, 0, true))

	path := filepath.Join(t.TempDir(), "turns.log")
	n, err := ts.open(path)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("open reported %d held turns, want 2", n)
	}
	ts.close()

	lines := readLines(t, path)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want the 2 turns the room already held: %v", len(lines), lines)
	}
	if !strings.Contains(lines[1], "wait=44s") {
		t.Errorf("the slow turn did not survive into the file: %q", lines[1])
	}
}

// TestTraceRingIsBounded keeps a long room from growing without limit, and drops
// the OLDEST — the newest turns are the ones anyone opening a trace is asking
// about.
func TestTraceRingIsBounded(t *testing.T) {
	ts := newTraceSink()
	for i := 0; i < maxTraceRing+10; i++ {
		d := time.Duration(i) * time.Millisecond
		ts.record(clock(model.VendorCodex, 0, d, 0, false))
	}
	if got := ts.held(); got != maxTraceRing {
		t.Fatalf("held %d records, want the ring capped at %d", got, maxTraceRing)
	}

	path := filepath.Join(t.TempDir(), "turns.log")
	if _, err := ts.open(path); err != nil {
		t.Fatal(err)
	}
	ts.close()
	lines := readLines(t, path)
	// The last one recorded must be present and the first must not.
	if !strings.Contains(lines[len(lines)-1], "wait=209ms") {
		t.Errorf("the newest turn was dropped: %q", lines[len(lines)-1])
	}
	for _, l := range lines {
		if strings.Contains(l, "wait=0s") {
			t.Fatal("the oldest turn survived the cap — the ring is dropping the wrong end")
		}
	}
}

// TestTraceKeepsMeasuringWhileOff is what makes `/trace off` a cheap decision:
// stopping the file does not stop the clock, so turning it back on still reaches
// back over the gap.
func TestTraceKeepsMeasuringWhileOff(t *testing.T) {
	dir := t.TempDir()
	ts := newTraceSink()
	if _, err := ts.open(filepath.Join(dir, "first.log")); err != nil {
		t.Fatal(err)
	}
	ts.close()

	ts.record(clock(model.VendorClaude, 0, 7*time.Second, 0, false))

	n, err := ts.open(filepath.Join(dir, "second.log"))
	if err != nil {
		t.Fatal(err)
	}
	ts.close()
	if n != 1 {
		t.Errorf("the second trace saw %d held turns, want the 1 recorded while off", n)
	}
	if lines := readLines(t, filepath.Join(dir, "second.log")); !strings.Contains(lines[0], "wait=7s") {
		t.Errorf("the turn taken while the trace was off is missing: %q", lines[0])
	}
}

// TestTraceCommandVocabulary holds the line this file promises not to cross: a
// draft that merely starts with the verb is prose and goes to the vendors.
func TestTraceCommandVocabulary(t *testing.T) {
	for _, tc := range []struct {
		draft string
		arg   string
		ok    bool
	}{
		{"/trace", "", true},
		{"/trace turns.log", "turns.log", true},
		{"/trace off", "off", true},
		{"  /trace turns.log  ", "turns.log", true},
		// Prose. A question about tracing is a question, and a vendor should get it.
		{"/tracing is on?", "", false},
		{"/traceroute", "", false},
		{"what does /trace do", "", false},
		{"", "", false},
	} {
		arg, ok := parseCommand(tc.draft, "/trace")
		if ok != tc.ok || arg != tc.arg {
			t.Errorf("parseCommand(%q) = (%q, %v), want (%q, %v)", tc.draft, arg, ok, tc.arg, tc.ok)
		}
	}
}

// TestBareTraceReportsAndNeverEnables guards the read/write boundary.
//
// README.md and CLAUDE.md both say council writes one file of its own,
// room.json. A no-argument /trace that picked a path would make that sentence
// false, so the bare form reports and nothing else — and this asserts the
// absence of a file rather than the wording of the notice.
func TestBareTraceReportsAndNeverEnables(t *testing.T) {
	dir := t.TempDir()
	m := roomAt(t, dir)

	m.st.Draft = "/trace"
	if !m.roomCommand() {
		t.Fatal("/trace was not recognised as a room command")
	}

	if got := m.trace.target(); got != "" {
		t.Errorf("bare /trace opened %q — council chose a path", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("bare /trace wrote %d files into the workspace", len(entries))
	}
	if !strings.Contains(m.st.Notice, "not tracing") {
		t.Errorf("notice = %q, want it to say the trace is off", m.st.Notice)
	}
}

// TestTraceCommandEnablesAndReportsTheWindow. The count is the honest half: a
// trace opened mid-conversation reaches back only as far as the ring, and a
// short file must not read as a quiet session.
func TestTraceCommandEnablesAndReportsTheWindow(t *testing.T) {
	dir := t.TempDir()
	m := roomAt(t, dir)
	m.trace.record(clock(model.VendorCodex, 0, 44*time.Second, 0, false))

	m.st.Draft = "/trace turns.log"
	if !m.roomCommand() {
		t.Fatal("/trace <file> was not recognised")
	}

	if m.trace.target() == "" {
		t.Fatal("/trace <file> did not enable the trace")
	}
	if !strings.Contains(m.st.Notice, "1 held turn") {
		t.Errorf("notice = %q, want it to name how many held turns were written", m.st.Notice)
	}
	if m.st.Draft != "" {
		t.Errorf("draft = %q, want it cleared once the command took effect", m.st.Draft)
	}
	// Relative to the ROOM's workspace, not the process's cwd.
	if _, err := os.Stat(filepath.Join(dir, "turns.log")); err != nil {
		t.Errorf("the trace did not land beside the room's workspace: %v", err)
	}

	m.st.Draft = "/trace off"
	m.roomCommand()
	if got := m.trace.target(); got != "" {
		t.Errorf("/trace off left the trace writing to %q", got)
	}
}

// TestTraceDoesNotRefuseMidTurn is the deliberate difference from /cd and `c`.
//
// Those change state the seats are using. This opens a file on the room's side,
// and the turn you cannot explain is usually the one still running — so refusing
// here would refuse at the only moment that matters. clock.go emits at end(), so
// the in-flight turn is still caught.
func TestTraceDoesNotRefuseMidTurn(t *testing.T) {
	dir := t.TempDir()
	m := roomAt(t, dir)
	m.turn = &turnState{}

	m.st.Draft = "/trace turns.log"
	m.roomCommand()

	if m.trace.target() == "" {
		t.Fatalf("a trace was refused mid-turn: %q", m.st.Notice)
	}

	// The contrast, asserted in the same test so the two cannot drift apart.
	m.st.Draft = "/cd .."
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("/cd stopped refusing mid-turn: %q", m.st.Notice)
	}
}

// TestTraceBadPathIsANoticeNotACrash, and the draft survives it — a path is
// expensive to retype, which is the same reason /cd keeps one.
func TestTraceBadPathIsANoticeNotACrash(t *testing.T) {
	m := roomAt(t, t.TempDir())

	m.st.Draft = "/trace no-such-dir/turns.log"
	m.roomCommand()

	if m.trace.target() != "" {
		t.Error("an unwritable path was accepted")
	}
	if !strings.Contains(m.st.Notice, "trace:") {
		t.Errorf("notice = %q, want the failure named", m.st.Notice)
	}
	if m.st.Draft == "" {
		t.Error("the mistyped path was thrown away")
	}
}

// roomAt is a room pointed at dir, with nothing dispatched. Named for the
// workspace rather than the trace because dispatch_test.go already owns
// traceModel, which is a different fixture entirely.
//
// The Cleanup is load-bearing on Windows and invisible on Linux, which is
// exactly how it got here: a test that enables a trace and leaves it open
// passes locally and fails in CI, because Linux unlinks an open file happily
// and Windows refuses — t.TempDir's own RemoveAll is what reports it, so the
// failure names the cleanup rather than the test that leaked the handle.
// Windows is the primary target (ADR-002); closing here means no future test
// using this fixture has to remember.
func roomAt(t *testing.T, dir string) *Model {
	t.Helper()
	st := room()
	st.Workspace = dir
	st.Home = dir
	m := &Model{st: st, glyphs: GlyphsFor(false), trace: newTraceSink()}
	t.Cleanup(m.trace.close)
	return m
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
}

// TestHelpNamesEveryRoomControlAboveTheFold pins what was only a comment.
//
// helpBody clips at the body height and does not scroll, so a line past the fold
// is not a demoted line, it is an absent one — the failure that split this panel
// into two pages in the first place. The budget is 17 rows to the `?` line on a
// 24-row terminal, and every control that changes the room from inside it
// (design.md §9.17) has to be named inside that window or it cannot be found
// without reading the source.
func TestHelpNamesEveryRoomControlAboveTheFold(t *testing.T) {
	lines := helpKeys(layoutFor(room(), GlyphsFor(false)), PlainStyles(), GlyphsFor(false))

	// Where the panel's own way out sits. Everything a reader must find has to be
	// at or above it, because that is the last row a 24-row terminal draws.
	fold := -1
	for i, l := range lines {
		if strings.Contains(l, "? ") && strings.Contains(l, "next page") {
			fold = i
			break
		}
	}
	if fold < 0 {
		t.Fatal("the `?` line is gone — the panel has no documented way out")
	}
	if fold > 16 {
		t.Errorf("the `?` line is at row %d, past the 17-row budget for a 24-row terminal", fold+1)
	}

	above := strings.Join(lines[:fold+1], "\n")
	for _, control := range []string{"/cd", "c clears", "/trace"} {
		if !strings.Contains(above, control) {
			t.Errorf("%q is not named above the fold — it cannot be discovered in the UI", control)
		}
	}
}
