package council

import (
	"strings"
	"testing"
)

// The closing line is the whole of rung 0 (design.md §9.52), so these tests are
// about WORDS. There is no render path to golden and no state machine to drive:
// the defect being closed is that the room said nothing, and the fix is a
// sentence, so what is pinned is that the sentence stays true.

func TestClosingLineNamesTheProcessesItEnded(t *testing.T) {
	out := strings.Join(closingLines(3, 7, "/home/dev/.telltale/council/room.json", "/home/dev"), "\n")

	for _, want := range []string{
		"3 vendor processes ended with it",
		"a seat never outlives the room",
		"the conversation is gone",
		"~/.telltale/council/room.json",
		"turn 7",
		"rebuilds those seats",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the closing line does not say %q:\n%s", want, out)
		}
	}
}

// One process is one process. A count that reads "1 vendor processes" is the
// kind of seam that makes a reader distrust the number beside it.
func TestClosingLineCountsOneProcessSingular(t *testing.T) {
	out := strings.Join(closingLines(1, 2, "/home/dev/.telltale/council/room.json", "/home/dev"), "\n")

	if !strings.Contains(out, "1 vendor process ended") {
		t.Errorf("one process is not reported in the singular:\n%s", out)
	}
	if strings.Contains(out, "processes") {
		t.Errorf("one process is reported in the plural:\n%s", out)
	}
}

// §4a.1 in prose. A room that spawned nothing has a different thing to say from
// a room that ended three seats, and "0 vendor processes ended with it" is the
// sentence that reads like the second while meaning the first.
func TestClosingLineSaysNothingWasRunningRatherThanZero(t *testing.T) {
	out := strings.Join(closingLines(0, 4, "/home/dev/.telltale/council/room.json", "/home/dev"), "\n")

	if !strings.Contains(out, "no vendor process was running") {
		t.Errorf("a room that spawned nothing does not say so:\n%s", out)
	}
	if strings.Contains(out, "0 vendor") {
		t.Errorf("the zero is rendered as a count rather than as a sentence:\n%s", out)
	}
	// The ids are still there — the seats being unspawned says nothing about
	// what earlier launches saved.
	if !strings.Contains(out, "turn 4") {
		t.Errorf("the saved turn is dropped when no process was running:\n%s", out)
	}
}

// A room with no completed turn has no saved room, because SaveRoom is only
// reached with a turn behind it and readRoom refuses a turn:0 file. Pointing
// the reader at room.json here would name a file that is not there.
func TestClosingLineDoesNotPromiseASaveThatNeverHappened(t *testing.T) {
	out := strings.Join(closingLines(2, 0, "/home/dev/.telltale/council/room.json", "/home/dev"), "\n")

	if !strings.Contains(out, "nothing was saved") {
		t.Errorf("a room with no completed turn does not say nothing was saved:\n%s", out)
	}
	if strings.Contains(out, "room.json") {
		t.Errorf("a room with no completed turn still names the saved file:\n%s", out)
	}
	if strings.Contains(out, "rebuilds those seats") {
		t.Errorf("a room with nothing saved still offers a rebuild:\n%s", out)
	}
	// What DID happen is still reported: the processes were real.
	if !strings.Contains(out, "2 vendor processes ended") {
		t.Errorf("the ended processes are dropped when nothing was saved:\n%s", out)
	}
}

// An unresolvable home is telltale's own state being unreachable, which this
// package's rule says must never be reported as the user's loss. The turn is a
// fact this room measured and is still stated; where it went is not claimed.
func TestClosingLineAdmitsWhenItCannotNameTheSavedRoom(t *testing.T) {
	out := strings.Join(closingLines(1, 9, "", "/home/dev"), "\n")

	if !strings.Contains(out, "could not be resolved") {
		t.Errorf("an unresolvable room path is not reported:\n%s", out)
	}
	if strings.Contains(out, "the conversation is gone. the session ids are not") {
		t.Errorf("the room claims where the ids are without knowing:\n%s", out)
	}
	if !strings.Contains(out, "turn 9") {
		t.Errorf("the measured turn is dropped with the path:\n%s", out)
	}
}

// The line lands on a plain stdout, often into a pipe or a captured log. It
// must carry no escape sequence and no styling of any kind — colour is always a
// second signal in this product and there is no first signal to second here.
func TestClosingLineCarriesNoEscapes(t *testing.T) {
	for _, line := range closingLines(3, 7, "/home/dev/.telltale/council/room.json", "/home/dev") {
		if strings.ContainsRune(line, '\x1b') {
			t.Errorf("the closing line carries an ANSI escape: %q", line)
		}
	}
}

// The vocabulary ruling of §9.52: the sentence about PROCESSES says "rebuild",
// never "reattach". `reattach` is the file half and is already taken, and the
// two words meaning one thing is what the ruling exists to prevent.
func TestClosingLineSaysRebuildAndNotReattach(t *testing.T) {
	out := strings.Join(closingLines(3, 7, "/home/dev/.telltale/council/room.json", "/home/dev"), "\n")

	if strings.Contains(out, "reattach") {
		t.Errorf("the closing line spends the word reattach on a process:\n%s", out)
	}
	if strings.Contains(out, "rejoin") {
		t.Errorf("the closing line spends the reserved word rejoin:\n%s", out)
	}
}
