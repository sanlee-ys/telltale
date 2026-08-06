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
	sink, closeTrace, err := openTrace(path)
	if err != nil {
		t.Fatal(err)
	}
	sink(clock(model.VendorCursor, 400*time.Millisecond, 41*time.Second, 2*time.Second, true))
	sink(clock(model.VendorClaude, 0, 3*time.Second, 9*time.Second, false))
	closeTrace()

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
	sink, closeTrace, err := openTrace(path)
	if err != nil {
		t.Fatal(err)
	}
	sink(clock(model.VendorCodex, time.Second, time.Second, time.Second, true))
	closeTrace()

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
	if _, _, err := openTrace(path); err == nil {
		t.Fatal("a trace path inside a directory that does not exist was accepted")
	}
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
}
