package council

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// checkRuns records what the stubbed check runner was asked to do.
type checkRuns struct {
	mu   sync.Mutex
	dirs []string
	argv [][]string
}

func (r *checkRuns) add(dir string, argv []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dirs = append(r.dirs, dir)
	r.argv = append(r.argv, append([]string(nil), argv...))
}

func (r *checkRuns) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.dirs)
}

func (r *checkRuns) dir(i int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.dirs) {
		return ""
	}
	return r.dirs[i]
}

// countCheckRuns stubs the check runner for one test and restores it in
// t.Cleanup — the countSpawns pattern (flow_security_test.go) applied to the
// one process this feature starts. No test in this package may run a real
// check: main_test.go panics on any that tries with a resolvable binary, and
// this is the helper that panic names.
func countCheckRuns(t *testing.T, answer func(dir string, argv []string) checkOutcome) *checkRuns {
	t.Helper()
	runs := &checkRuns{}
	prev := startCheck
	startCheck = func(_ context.Context, dir string, argv []string) checkOutcome {
		runs.add(dir, argv)
		if answer == nil {
			return checkOutcome{}
		}
		return answer(dir, argv)
	}
	t.Cleanup(func() { startCheck = prev })
	return runs
}

// checkedRoom is a three-seat room whose first seat has a settled attempt, with
// the check command already resolved. It builds the State by hand for the
// reason every render test here does: no terminal, no program loop, no vendor
// process.
func checkedRoom(t *testing.T, cmd string) *Model {
	t.Helper()
	m := clearModel()
	m.arenaCheck = resolveArenaCheck(cmd)
	m.st.Columns[0].Phase = PhaseDone
	m.st.Columns[0].Arena = &ArenaResult{
		Tree:   "/home/dev/code/telltale-arena-t7-claude",
		Branch: "arena/t7/claude",
		Base:   "422b1c3f0a11d2e3b4c5d6e7f8091a2b3c4d5e6f",
		Stat:   " internal/council/view.go | 3 +-\n 1 file changed, 2 insertions(+), 1 deletion(-)",
		RaceN:  7,
		Rank:   1, Of: 3,
	}
	return m
}

// drainCmd runs a command to completion and returns its messages, the way the
// Bubble Tea runtime would. tea.Batch hands back one command whose message is a
// BatchMsg carrying the rest, so unwrapping that is the whole of what a test
// needs.
func drainCmd(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		out = append(out, drainCmd(t, c)...)
	}
	return out
}

// TestNoCheckConfiguredDrawsNothingAtAll is the absent state, and it is the one
// this whole feature is judged against: a room that never named a check must
// render exactly what it rendered before the check existed. Absent is not FAIL
// (§4a.1), and the cheapest way to get that wrong is a default verdict.
func TestNoCheckConfiguredDrawsNothingAtAll(t *testing.T) {
	countCheckRuns(t, nil)
	m := checkedRoom(t, "")

	if cmd := m.dueArenaChecks(); cmd != nil {
		t.Fatal("a room with no check configured launched one")
	}
	if m.st.Columns[0].Arena.Check != nil {
		t.Fatal("a room with no check configured recorded one on the attempt")
	}
	if got := render(m.st); strings.Contains(got, "check") {
		t.Errorf("an unchecked room drew a check line:\n%s", got)
	}
}

// TestTheCheckReportsTheProcessesOwnExitCode: PASS and FAIL come from the exit
// code and from nothing else. Three runs, three codes — and the exit code
// itself stays on screen, because the verdict is a reading of it rather than a
// replacement for it.
func TestTheCheckReportsTheProcessesOwnExitCode(t *testing.T) {
	for _, tc := range []struct {
		code int
		want string
	}{
		{0, "check PASS · exit 0"},
		{1, "check FAIL · exit 1"},
		{7, "check FAIL · exit 7"},
	} {
		runs := countCheckRuns(t, func(string, []string) checkOutcome {
			return checkOutcome{exit: tc.code, elapsed: 44 * time.Second}
		})
		m := checkedRoom(t, "go test ./...")

		msgs := drainCmd(t, m.dueArenaChecks())
		if runs.count() != 1 {
			t.Fatalf("exit %d: the check ran %d times, want 1", tc.code, runs.count())
		}
		if k := m.st.Columns[0].Arena.Check; k == nil || !k.Running {
			t.Fatalf("exit %d: the launched check does not render as running", tc.code)
		}
		if got := render(m.st); !strings.Contains(got, "check · running") {
			t.Errorf("exit %d: a launched check says nothing while it runs:\n%s", tc.code, got)
		}
		for _, msg := range msgs {
			cm, ok := msg.(arenaCheckMsg)
			if !ok {
				t.Fatalf("exit %d: the check returned %T, want arenaCheckMsg", tc.code, msg)
			}
			m.applyArenaCheck(cm)
		}

		got := render(m.st)
		if !strings.Contains(got, tc.want) {
			t.Errorf("exit %d does not render %q:\n%s", tc.code, tc.want, got)
		}
		if !strings.Contains(got, "· 44s") {
			t.Errorf("exit %d loses the check's measured clock:\n%s", tc.code, got)
		}
		if !strings.Contains(got, "go test ./...") {
			t.Errorf("exit %d renders a verdict without the command that produced it:\n%s", tc.code, got)
		}
	}
}

// TestACheckThatCouldNotRunIsNeverAFail. Three ways to have no exit code — the
// binary would not start, the room refused the spelling, the attempt's own diff
// was never read — and none of them may render as a verdict. This is §4a.1's
// degraded-vs-zero rule on the surface where getting it wrong is worst: FAIL on
// an attempt nobody measured is a claim about somebody's code.
func TestACheckThatCouldNotRunIsNeverAFail(t *testing.T) {
	t.Run("the command would not start", func(t *testing.T) {
		countCheckRuns(t, func(string, []string) checkOutcome {
			return checkOutcome{err: `could not run: exec: "nosuchcheck": executable file not found in $PATH`}
		})
		m := checkedRoom(t, "nosuchcheck")
		for _, msg := range drainCmd(t, m.dueArenaChecks()) {
			m.applyArenaCheck(msg.(arenaCheckMsg))
		}
		assertCheckUnavailable(t, m, "executable file not found")
	})

	t.Run("the spelling was refused", func(t *testing.T) {
		runs := countCheckRuns(t, nil)
		m := checkedRoom(t, "go test ./... > out.txt")
		if cmd := m.dueArenaChecks(); cmd != nil {
			t.Error("a refused spelling still launched a process")
		}
		if runs.count() != 0 {
			t.Errorf("a refused spelling ran %d commands", runs.count())
		}
		assertCheckUnavailable(t, m, `">" cannot be part of one`)
	})

	t.Run("the attempt's diff was never read", func(t *testing.T) {
		runs := countCheckRuns(t, nil)
		m := checkedRoom(t, "go test ./...")
		m.st.Columns[0].Arena.Err = "diff unavailable: fatal: not a git repository"
		if cmd := m.dueArenaChecks(); cmd != nil {
			t.Error("an unreadable attempt still launched a check")
		}
		if runs.count() != 0 {
			t.Errorf("an unreadable attempt ran %d commands", runs.count())
		}
		assertCheckUnavailable(t, m, "the attempt's diff could not be read")
	})
}

// assertCheckUnavailable holds the degraded state to both halves of its
// contract: the REASON is carried whole on the result — asserted on the field,
// because a column wraps a sentence over several rows and a frame search would
// pass or fail on the terminal width rather than on the words — and the FRAME
// says unavailable without ever reaching for a verdict.
func assertCheckUnavailable(t *testing.T, m *Model, reason string) {
	t.Helper()
	k := m.st.Columns[0].Arena.Check
	if k == nil {
		t.Fatal("no check was recorded at all — that is the absent state, not the degraded one")
	}
	if k.Running {
		t.Error("a check that never ran is still rendering as running")
	}
	if !strings.Contains(k.Err, reason) {
		t.Errorf("the unavailable check reads %q, want it to carry %q", k.Err, reason)
	}
	got := render(m.st)
	if !strings.Contains(got, "check unavailable: ") {
		t.Errorf("a check with no exit code does not render as unavailable:\n%s", got)
	}
	if strings.Contains(got, "check FAIL") || strings.Contains(got, "check PASS") {
		t.Errorf("a check that never ran rendered a verdict:\n%s", got)
	}
}

// TestTheCheckRunsInTheRacersWorktreeAndNeverTheRoom. The whole read/write
// boundary of this feature is the working directory: council already owns the
// racer worktrees, and running an operator command in one of them is inside the
// exception /arena already carries. The room's own tree is not, and the day
// this points at the workspace is the day the feature is a defect.
func TestTheCheckRunsInTheRacersWorktreeAndNeverTheRoom(t *testing.T) {
	runs := countCheckRuns(t, nil)
	m := checkedRoom(t, "go test ./...")
	m.st.Workspace = "/home/dev/code/telltale"

	drainCmd(t, m.dueArenaChecks())
	if runs.count() != 1 {
		t.Fatalf("the check ran %d times, want 1", runs.count())
	}
	if got, want := runs.dir(0), m.st.Columns[0].Arena.Tree; got != want {
		t.Errorf("the check ran in %q, want the racer's worktree %q", got, want)
	}
	if runs.dir(0) == m.st.Workspace {
		t.Fatal("the check ran in the room's own tree")
	}
}

// TestOneAttemptRunsOneCheck. The launch is marked by the Check field itself,
// so the due scan can be reached on every tick — which it is — without a second
// process per frame. A check that ran per tick would be an operator's test
// suite started ten times a second.
func TestOneAttemptRunsOneCheck(t *testing.T) {
	runs := countCheckRuns(t, nil)
	m := checkedRoom(t, "go test ./...")

	drainCmd(t, m.dueArenaChecks())
	for i := 0; i < 5; i++ {
		if cmd := m.dueArenaChecks(); cmd != nil {
			t.Fatalf("tick %d launched a second check for one attempt", i)
		}
	}
	if runs.count() != 1 {
		t.Errorf("one attempt ran %d checks", runs.count())
	}
}

// TestEveryRacerGetsItsOwnCheckRun: a race is a comparison, so a verdict on one
// seat says nothing about another. Two settled attempts, two runs, each in its
// own tree.
func TestEveryRacerGetsItsOwnCheckRun(t *testing.T) {
	runs := countCheckRuns(t, nil)
	m := checkedRoom(t, "go test ./...")
	m.st.Columns[1].Phase = PhaseFailed
	m.st.Columns[1].Arena = &ArenaResult{
		Tree:   "/home/dev/code/telltale-arena-t7-codex",
		Branch: "arena/t7/codex",
		Base:   m.st.Columns[0].Arena.Base,
		Stat:   " main.go | 1 +\n",
		RaceN:  7,
	}

	drainCmd(t, m.dueArenaChecks())
	if runs.count() != 2 {
		t.Fatalf("two settled attempts ran %d checks", runs.count())
	}
	if runs.dir(0) == runs.dir(1) {
		t.Errorf("both racers' checks ran in the same tree: %q", runs.dir(0))
	}
}

// TestACheckOutcomeThatOutlivedItsAttemptIsDropped. A check outlives the turn
// that started it by construction — a suite runs longer than a race — so the
// message has to name the receipt it answers about. Writing a stale verdict
// onto whatever is on the column now would put one attempt's PASS under another
// attempt's diff.
func TestACheckOutcomeThatOutlivedItsAttemptIsDropped(t *testing.T) {
	countCheckRuns(t, nil)
	m := checkedRoom(t, "go test ./...")
	drainCmd(t, m.dueArenaChecks())

	// The seat took another turn: a later race replaced the block.
	m.st.Columns[0].Arena = &ArenaResult{Tree: "/home/dev/code/telltale-arena-t8-claude", RaceN: 8}
	m.applyArenaCheck(arenaCheckMsg{vendor: model.VendorClaude, raceN: 7, outcome: checkOutcome{exit: 0}})
	if m.st.Columns[0].Arena.Check != nil {
		t.Error("a check run for race 7 landed on race 8's attempt")
	}

	// And the column cleared entirely.
	m.st.Columns[0].Arena = nil
	m.applyArenaCheck(arenaCheckMsg{vendor: model.VendorClaude, raceN: 7, outcome: checkOutcome{exit: 0}})
	if m.st.Columns[0].Arena != nil {
		t.Error("a stale check rebuilt an attempt the room had cleared")
	}
}

// TestTheCheckSpellingRefusesAShellAndNamesTheCharacter walks the refusal
// vocabulary directly. Argv and never a shell is §9.3's rule for every process
// council starts; the point of refusing rather than splitting is that a
// `go test > out.txt` quietly run as three arguments would report an exit code
// for a command nobody wrote.
func TestTheCheckSpellingRefusesAShellAndNamesTheCharacter(t *testing.T) {
	for _, raw := range []string{
		"go test ./... | tee out",
		"go test ./... && go vet ./...",
		"go test ./... > out.txt",
		"go test $(ls)",
		`go test "./..."`,
		"go test ./...; echo done",
	} {
		spec := resolveArenaCheck(raw)
		if spec.why == "" {
			t.Errorf("%q was accepted as argv", raw)
			continue
		}
		if spec.argv != nil {
			t.Errorf("%q was refused and still produced an argv", raw)
		}
		if !spec.configured() {
			t.Errorf("%q refused itself into looking unconfigured — that is the absent state, not this one", raw)
		}
	}

	spec := resolveArenaCheck("  go test ./internal/council  ")
	if spec.why != "" {
		t.Fatalf("a plain command was refused: %s", spec.why)
	}
	if len(spec.argv) != 3 || spec.argv[0] != "go" || spec.argv[2] != "./internal/council" {
		t.Errorf("argv = %q", spec.argv)
	}
	if spec.raw != "go test ./internal/council" {
		t.Errorf("the raw command kept its padding: %q", spec.raw)
	}
	if blank := resolveArenaCheck("   "); blank.configured() {
		t.Error("a blank variable configured a check")
	}
}

// TestCheckStatesInOneFrame is the golden that pins the states side by side: a
// measured PASS, a measured FAIL, and a check with no exit code. Collapsing any
// pair of them is the regression this file exists to prevent.
func TestCheckStatesInOneFrame(t *testing.T) {
	golden(t, "arena-check", render(checkRoom()))
}

func checkRoom() State {
	st := room()
	st.Height = 40
	st.Turn = 7
	base := "422b1c3f0a11d2e3b4c5d6e7f8091a2b3c4d5e6f"
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Elapsed = 19 * time.Second
	st.Columns[0].Body = "Done. The parser reads the header before the body."
	st.Columns[0].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t7-claude", Branch: "arena/t7/claude", Base: base,
		Stat:  " parse.go | 12 ++++++---\n 1 file changed, 9 insertions(+), 3 deletions(-)",
		RaceN: 7, Rank: 1, Of: 3, Commit: "9f3a1c2d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8091",
		Check: &ArenaCheck{Cmd: "go test ./...", Exit: 0, Elapsed: 44 * time.Second},
	}
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Elapsed = 25 * time.Second
	st.Columns[1].Body = "Rewrote the parser."
	st.Columns[1].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t7-codex", Branch: "arena/t7/codex", Base: base,
		Stat:  " parse.go | 30 +++++++++-----\n 1 file changed, 21 insertions(+), 9 deletions(-)",
		RaceN: 7, Rank: 2, Of: 3, Commit: "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9012",
		Check: &ArenaCheck{Cmd: "go test ./...", Exit: 1, Elapsed: 51 * time.Second},
	}
	st.Columns[2].Phase = PhaseFailed
	st.Columns[2].Elapsed = 8 * time.Second
	st.Columns[2].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t7-agy", Branch: "arena/t7/agy", Base: base,
		Err: "diff unavailable: fatal: not a git repository", RaceN: 7, Rank: 3, Of: 3,
		Check: &ArenaCheck{Cmd: "go test ./...", Err: "not run — the attempt's diff could not be read"},
	}
	return st
}

// TestTheCheckReadsTheSameWithoutColour. Every distinction this room makes is
// carried by a word first (§9.11): PASS, FAIL and unavailable have to survive
// NO_COLOR and --ascii, because colour and weight are the second signal and
// never the only one. PlainStyles is the identity set that stands in for a
// monochrome terminal, so the assertion is that the words are in the bytes.
func TestTheCheckReadsTheSameWithoutColour(t *testing.T) {
	plain := Render(checkRoom(), PlainStyles(), GlyphsFor(true))
	for _, word := range []string{"check PASS", "check FAIL", "check unavailable"} {
		if !strings.Contains(plain, word) {
			t.Errorf("%q does not survive --ascii and NO_COLOR:\n%s", word, plain)
		}
	}
}

// TestAnUnmeasuredCheckClockIsNotPrintedAsZero. A finished check that reported
// no duration prints no duration — the same rule as every other figure on this
// surface, and the one a `0s` would quietly break by looking like a measurement
// of an instant run.
func TestAnUnmeasuredCheckClockIsNotPrintedAsZero(t *testing.T) {
	st := room()
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t7-claude", Branch: "arena/t7/claude",
		Base: "422b1c3", Stat: " x.go | 1 +", RaceN: 7,
		Check: &ArenaCheck{Cmd: "go test ./...", Exit: 0},
	}
	got := render(st)
	if !strings.Contains(got, "check PASS · exit 0") {
		t.Fatalf("the verdict is missing:\n%s", got)
	}
	if strings.Contains(got, "exit 0 · 0s") {
		t.Errorf("an unmeasured check clock printed as a measured zero:\n%s", got)
	}
}
