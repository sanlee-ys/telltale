package council

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The arena check's tests (§9.48). Two halves, and the split is deliberate.
//
// Everything reached through the MODEL stubs the run (countSpawns, which now
// covers startCheck as its fourth spawn) — a council test never starts a real
// program, and TestMain panics on one that tries. What the stub cannot pin is
// the single claim the whole feature rests on: that PASS and FAIL come from a
// real exit code. TestPassAndFailComeFromARealExitCode calls runCheck DIRECTLY,
// past the guarded var, with this test binary itself as the command — a program
// that reaches no account, costs nothing, and is the only honest way to assert
// that the exit code is read rather than assumed.

// drainChecks runs whatever dueArenaChecks queued and returns the messages,
// which is what the Update loop does with them.
func drainChecks(t *testing.T, m *Model) []arenaCheckMsg {
	t.Helper()
	cmd := m.dueArenaChecks()
	if cmd == nil {
		return nil
	}
	var out []arenaCheckMsg
	switch v := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range v {
			msg, ok := c().(arenaCheckMsg)
			if !ok {
				t.Fatalf("a check command returned %T", msg)
			}
			out = append(out, msg)
		}
	case arenaCheckMsg:
		out = append(out, v)
	default:
		t.Fatalf("dueArenaChecks returned %T", v)
	}
	return out
}

// raceRoom sets up a real race over a real repository and returns the model
// mid-turn, with every seat still live. The seats' processes are stubbed; the
// git is real, because the check runs against a worktree and asserting that
// against a fake path would test the flag rather than the effect.
func raceRoom(t *testing.T, seats ...model.VendorID) (*Model, map[model.VendorID]string) {
	t.Helper()
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 6, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := clearModel()
	m.st.Workspace = ws
	live := map[model.VendorID]bool{}
	for _, v := range seats {
		live[v] = true
		c := m.column(v)
		c.Phase = PhaseStreaming
		c.TurnN = 6
		c.Prompt = "make the retry loop back off"
	}
	m.turn = &turnState{
		live:       live,
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	}
	return m, trees
}

// TestTheCheckVerbNamesTheCommandSaysItAndTakesItBack walks the whole grammar
// through the draft, because that is the surface the operator has: three forms
// of `/arena check`, none of which may dispatch anything.
func TestTheCheckVerbNamesTheCommandSaysItAndTakesItBack(t *testing.T) {
	log := countSpawns(t)
	// Only `go` resolves, so the refusal path below is the machine's answer to
	// a word that is not a program rather than a blanket yes.
	lookPath = func(name string) (string, error) {
		if name == "go" {
			return "go", nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = origLookPath })

	m := clearModel()
	m.st.Mode = ModeComposing
	m.st.Write = true

	// Bare, with nothing named: the room says so and names the form.
	m.st.Draft = "/arena check"
	m.dispatch()
	if !strings.Contains(m.st.Notice, "no check is named") {
		t.Errorf("bare check with nothing named: %q", m.st.Notice)
	}

	m.st.Draft = "/arena check go test ./..."
	m.dispatch()
	if m.checkCmd != "go test ./..." {
		t.Fatalf("checkCmd = %q", m.checkCmd)
	}
	if got := strings.Join(m.checkArgv, "|"); got != "go|test|./..." {
		t.Errorf("checkArgv = %q — argv, never a shell", got)
	}
	if !strings.Contains(m.st.Notice, "exit code") {
		t.Errorf("the notice does not say where the verdict comes from: %q", m.st.Notice)
	}

	// Bare again: it reports what is named rather than clearing it.
	m.st.Draft = "/arena check"
	m.dispatch()
	if !strings.Contains(m.st.Notice, "go test ./...") || m.checkCmd == "" {
		t.Errorf("bare check did not report the named command: %q", m.st.Notice)
	}

	m.st.Draft = "/arena check off"
	m.dispatch()
	if m.checkCmd != "" || m.checkArgv != nil {
		t.Errorf("off left %q / %v behind", m.checkCmd, m.checkArgv)
	}
	if !strings.Contains(m.st.Notice, "go test ./...") {
		t.Errorf("off does not name what it stopped: %q", m.st.Notice)
	}

	// The verb runs in a READ-only room, like `/arena record` and for the same
	// reason: it spawns nothing and writes nothing. An ordinary brief beside it
	// still meets the read-room refusal, which is what proves the check parse
	// took only its own word.
	m.st.Write = false
	m.st.Draft = "/arena check go vet ./..."
	m.dispatch()
	if m.checkCmd != "go vet ./..." {
		t.Errorf("a read-only room refused a command that spawns nothing: %q", m.st.Notice)
	}
	m.st.Draft = "/arena fix the retry path"
	m.dispatch()
	if !strings.Contains(m.st.Notice, "/write") {
		t.Errorf("an ordinary brief did not reach the race path: %q", m.st.Notice)
	}
	m.st.Write = true

	if m.turn != nil {
		t.Error("naming a check dispatched a turn")
	}
	if log.n() != 0 {
		t.Errorf("naming a check spawned %d processes", log.n())
	}
	if len(log.checks) != 0 {
		t.Errorf("naming a check RAN it %d times — it names a later race's command", len(log.checks))
	}
}

var origLookPath = lookPath

// TestTheCheckVerbRefusesRatherThanSwallowingABrief is the vocabulary rule
// applied to the one /arena sub-verb that takes free text.
//
// `/arena check the parser handles empty input` is a brief someone will
// genuinely type. It must not become a command, and — the expensive half — it
// must not race five agents either. The draft stays in the composer, which is
// what every refusal in this room does with a line it will not run.
func TestTheCheckVerbRefusesRatherThanSwallowingABrief(t *testing.T) {
	log := countSpawns(t)
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	t.Cleanup(func() { lookPath = origLookPath })

	m := clearModel()
	m.st.Mode = ModeComposing
	m.st.Write = true
	m.st.Draft = "/arena check the parser handles empty input"
	m.dispatch()

	if m.checkCmd != "" {
		t.Errorf("a brief became a check command: %q", m.checkCmd)
	}
	if m.turn != nil || m.arenaPrep != nil {
		t.Fatal("the refused draft raced anyway")
	}
	if log.n() != 0 {
		t.Errorf("%d processes spawned for a refused draft", log.n())
	}
	if !strings.Contains(m.st.Notice, "no program called the") {
		t.Errorf("the refusal does not name what it could not find: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "races prose") {
		t.Errorf("the refusal does not name the other reading: %q", m.st.Notice)
	}
	if m.st.Draft == "" {
		t.Error("the refused draft was thrown away instead of handed back")
	}
}

// TestAPathBearingCommandIsNotLookedUpOnPath: `./scripts/check.sh` is resolved
// against the RACER's tree at run time, not against the room's own directory,
// so the set-time PATH guard deliberately does not apply to it.
func TestAPathBearingCommandIsNotLookedUpOnPath(t *testing.T) {
	countSpawns(t)
	lookPath = func(string) (string, error) { return "", errors.New("never called for a path") }
	t.Cleanup(func() { lookPath = origLookPath })

	m := clearModel()
	m.st.Mode = ModeComposing
	m.st.Write = true
	m.st.Draft = "/arena check ./scripts/check.sh"
	m.dispatch()

	if m.checkCmd != "./scripts/check.sh" {
		t.Fatalf("a path-bearing command was refused: %q / %q", m.checkCmd, m.st.Notice)
	}
	// And the trap it exists for: os/exec resolves a relative program against
	// the PARENT's directory, never cmd.Dir, so the tree has to be joined on.
	got := checkPath(filepath.FromSlash("/x/repo-arena-t6-codex"), "./scripts/check.sh")
	want := filepath.Join(filepath.FromSlash("/x/repo-arena-t6-codex"), "scripts", "check.sh")
	if got != want {
		t.Errorf("checkPath = %q, want the tree-rooted %q", got, want)
	}
	if got := checkPath("/x/tree", "go"); got != "go" {
		t.Errorf("a bare name was rewritten to %q — it belongs on PATH", got)
	}
}

// TestEveryRacerRunsTheCheckInItsOwnWorktree is the core claim: one run per
// seat, in that seat's tree, with the command the operator named.
func TestEveryRacerRunsTheCheckInItsOwnWorktree(t *testing.T) {
	log := countSpawns(t)
	m, trees := raceRoom(t, model.VendorClaude, model.VendorCodex)
	m.checkCmd, m.checkArgv = "go test ./...", []string{"go", "test", "./..."}

	m.finishColumn(m.column(model.VendorClaude), PhaseDone)
	m.finishColumn(m.column(model.VendorCodex), PhaseDone)

	msgs := drainChecks(t, m)
	if len(msgs) != 2 {
		t.Fatalf("%d checks ran for a two-seat race", len(msgs))
	}
	if len(log.checks) != 2 {
		t.Fatalf("%d runs reached the spawn var", len(log.checks))
	}
	seen := map[string]bool{}
	for _, run := range log.checks {
		seen[run.tree] = true
		if strings.Join(run.argv, " ") != "go test ./..." {
			t.Errorf("a racer ran %q", run.argv)
		}
	}
	for v, tree := range trees {
		if !seen[tree] {
			t.Errorf("%s did not run its check in %s", v, tree)
		}
	}
	// Every racer's block carries the command it ran, and neither is a verdict
	// until its message lands.
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex} {
		ck := m.column(v).Arena.Check
		if ck == nil || !ck.Running || ck.Cmd != "go test ./..." {
			t.Fatalf("%s: check record = %+v", v, ck)
		}
		if ck.Passed() {
			t.Errorf("%s passed before its run reported back", v)
		}
	}

	for _, msg := range msgs {
		m.applyArenaCheck(msg)
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex} {
		if ck := m.column(v).Arena.Check; !ck.Passed() || ck.Running {
			t.Errorf("%s: %+v, want a settled PASS", v, ck)
		}
	}
}

// TestNoCommandNamedIsAbsentNotAVerdict: a race in a room that named no check
// draws no check line anywhere. Not a dash, not a zero, not a pending word —
// §4a.1's absence, which is the whole reason Check is a pointer.
func TestNoCommandNamedIsAbsentNotAVerdict(t *testing.T) {
	log := countSpawns(t)
	m, _ := raceRoom(t, model.VendorClaude)

	m.finishColumn(m.column(model.VendorClaude), PhaseDone)

	if ck := m.column(model.VendorClaude).Arena.Check; ck != nil {
		t.Fatalf("a room with no named check produced %+v", ck)
	}
	if len(log.checks) != 0 {
		t.Errorf("%d checks ran with none named", len(log.checks))
	}
	if got := render(m.st); strings.Contains(got, "check ") {
		t.Error("the frame drew a check line for a race that had none")
	}
}

// TestACancelledTurnRunsNoCheck: ctrl+c is the operator saying stop, and a room
// that answered it by starting a subprocess per seat would be ignoring the one
// act it exists to obey. A seat cut on its own with `x` is NOT this case —
// §9.37 rules a given-up seat lands like any other finisher.
func TestACancelledTurnRunsNoCheck(t *testing.T) {
	log := countSpawns(t)
	m, _ := raceRoom(t, model.VendorClaude, model.VendorCodex)
	m.checkCmd, m.checkArgv = "go build ./...", []string{"go", "build", "./..."}

	// One seat given up on its own: it still gets its check.
	m.finishColumn(m.column(model.VendorClaude), PhaseCancelled)
	if m.column(model.VendorClaude).Arena.Check == nil {
		t.Error("a seat given up with x lost its check — it lands like any other finisher")
	}

	// Then ctrl+c takes the rest of the turn.
	m.cancelling = true
	m.finishColumn(m.column(model.VendorCodex), PhaseDone)
	if ck := m.column(model.VendorCodex).Arena.Check; ck != nil {
		t.Errorf("a cancelled turn queued %+v", ck)
	}
	if len(drainChecks(t, m)) != 1 || len(log.checks) != 1 {
		t.Errorf("%d runs, want only the given-up seat's", len(log.checks))
	}
}

// TestAStaleCheckNeverWritesOverALaterTurn: a check outlives its turn by
// design — the last racer landing ends the turn while its `go test` runs — so
// the drop has to be by comparison rather than by hoping the timing worked out.
func TestAStaleCheckNeverWritesOverALaterTurn(t *testing.T) {
	countSpawns(t)
	m, _ := raceRoom(t, model.VendorClaude)
	m.checkCmd, m.checkArgv = "go vet ./...", []string{"go", "vet", "./..."}
	m.finishColumn(m.column(model.VendorClaude), PhaseDone)

	c := m.column(model.VendorClaude)
	// The next dispatch moved the column on. The old run reports back anyway.
	c.startTurn(7, "the next brief", false)
	m.applyArenaCheck(arenaCheckMsg{vendor: model.VendorClaude, turnN: 6, exited: true, code: 0})
	if c.Arena != nil {
		t.Fatal("a stale check rebuilt a cleared arena block")
	}

	// And a run for a turn number that never was, landing on a live result.
	m2, _ := raceRoom(t, model.VendorCodex)
	m2.checkCmd, m2.checkArgv = "go vet ./...", []string{"go", "vet", "./..."}
	m2.finishColumn(m2.column(model.VendorCodex), PhaseDone)
	m2.applyArenaCheck(arenaCheckMsg{vendor: model.VendorCodex, turnN: 99, exited: true, code: 3})
	if ck := m2.column(model.VendorCodex).Arena.Check; !ck.Running {
		t.Errorf("a check from another turn landed: %+v", ck)
	}
}

// TestTheCheckRunsAfterTheDiffAndTheCommit pins the ordering ruling. Anything
// the check writes must be outside the attempt's stat and outside its commit,
// because a receipt carrying the check's own build output would claim work the
// racer did not do.
func TestTheCheckRunsAfterTheDiffAndTheCommit(t *testing.T) {
	log := countSpawns(t)
	m, trees := raceRoom(t, model.VendorCodex)
	tree := trees[model.VendorCodex]
	m.checkCmd, m.checkArgv = "go build .", []string{"go", "build", "."}
	// The racer's own work, before it lands.
	if err := os.WriteFile(filepath.Join(tree, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The stubbed run writes into the tree, exactly as a build would.
	origStart := startCheck
	startCheck = func(_ context.Context, dir string, argv []string) checkResult {
		log.checks = append(log.checks, checkRun{tree: dir, argv: argv})
		if err := os.WriteFile(filepath.Join(dir, "built.bin"), []byte("x"), 0o644); err != nil {
			t.Error(err)
		}
		return checkResult{exited: true, code: 0, dirty: true}
	}
	t.Cleanup(func() { startCheck = origStart })

	m.finishColumn(m.column(model.VendorCodex), PhaseDone)
	r := m.column(model.VendorCodex).Arena
	if r.Commit == "" {
		t.Fatalf("the attempt was not committed: %q", r.CommitErr)
	}
	for _, msg := range drainChecks(t, m) {
		m.applyArenaCheck(msg)
	}

	shown, _ := gitOut(tree, "show", "--name-only", "--format=%s", "HEAD")
	if strings.Contains(shown, "built.bin") {
		t.Errorf("the check's output rode into the attempt's commit:\n%s", shown)
	}
	if strings.Contains(r.Stat, "built.bin") {
		t.Errorf("the check's output is in the attempt's stat:\n%s", r.Stat)
	}
	if !r.Check.Dirty {
		t.Error("the check wrote into a clean tree and the column does not say so")
	}
	if got := render(m.st); !strings.Contains(got, "the check wrote into this tree") {
		t.Error("the dirty-tree sentence is not on the column")
	}
}

// TestCheckRendersPassFailAndUnavailableApart is the honesty boundary as a
// frame: a verdict, a different verdict, and a run that produced no verdict at
// all. The third is the one this test exists for — "unavailable" must never be
// spelled as FAIL.
func TestCheckRendersPassFailAndUnavailableApart(t *testing.T) {
	st := room()
	base := "abcdef1234567"
	for i := range st.Columns {
		st.Columns[i].Phase = PhaseDone
		st.Columns[i].TurnN = 6
		st.Columns[i].Elapsed = 20 * time.Second
	}
	st.Columns[0].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-claude", Branch: "arena/t6/claude", Base: base,
		Stat: " a.txt | 2 +-\n 1 file changed", Rank: 1, Of: 3, Commit: "1111111aaaa",
		Check: &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 0, Elapsed: 74 * time.Second},
	}
	st.Columns[1].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-codex", Branch: "arena/t6/codex", Base: base,
		Stat: " b.go | 9 ++++-----", Rank: 2, Of: 3, Commit: "2222222bbbb",
		Check: &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 2, Elapsed: 31 * time.Second},
	}
	st.Columns[2].Arena = &ArenaResult{
		Tree: "/home/dev/code/telltale-arena-t6-agy", Branch: "arena/t6/agy", Base: base,
		Rank: 3, Of: 3,
		Check: &ArenaCheck{Cmd: "go test ./...", Err: `exec: "go": executable file not found in $PATH`},
	}

	got := render(st)
	for _, want := range []string{
		"check PASS", "check FAIL", "exit 2", "check unavailable:",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
	if strings.Count(got, "check FAIL") != 1 {
		t.Error("a run that could not happen was rendered as a failure")
	}
	golden(t, "arena-check", got)

	st.ASCII = true
	golden(t, "arena-check-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}

// TestCheckLineAndColourSpendNoNewSignal: every state is carried by a WORD
// first, and the colour is the room's existing severity pair — SevOK and
// SevCrit, exactly as ForDiffLine spends them (style.go's no-new-hues rule).
// A run that could not happen wears neither.
func TestCheckLineAndColourSpendNoNewSignal(t *testing.T) {
	sty := NewStyles(true)
	pass := &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 0, Elapsed: 74 * time.Second}
	fail := &ArenaCheck{Cmd: "go test ./...", Exited: true, Code: 2}
	dead := &ArenaCheck{Cmd: "go test ./...", Err: "boom"}
	run := &ArenaCheck{Cmd: "go test ./...", Running: true}

	if got := checkLine(pass); got != "check PASS · 1m14s · go test ./..." {
		t.Errorf("pass line = %q", got)
	}
	if got := checkLine(fail); !strings.Contains(got, "check FAIL · exit 2") {
		t.Errorf("fail line = %q", got)
	}
	if got := checkLine(dead); !strings.HasPrefix(got, "check unavailable: boom") {
		t.Errorf("unavailable line = %q", got)
	}
	if got := checkLine(run); !strings.HasPrefix(got, "check running") {
		t.Errorf("running line = %q", got)
	}
	// Compared by what they RENDER: a lipgloss.Style holds slices and cannot be
	// compared directly, and the bytes are the thing the terminal sees anyway.
	same := func(a, b lipgloss.Style) bool { return a.Render("x") == b.Render("x") }
	if !same(checkStyle(sty, pass), sty.SevOK) {
		t.Error("PASS does not wear the room's own ok token")
	}
	if !same(checkStyle(sty, fail), sty.SevCrit) {
		t.Error("FAIL does not wear the room's own crit token")
	}
	if !same(checkStyle(sty, dead), sty.Muted) || !same(checkStyle(sty, run), sty.Muted) {
		t.Error("a run with no verdict wears a verdict's colour")
	}
	if same(sty.SevOK, sty.SevCrit) {
		t.Fatal("the fixture cannot tell the two tokens apart")
	}
	// The zero value is not a pass. Exited is the only gate.
	if (&ArenaCheck{}).Passed() {
		t.Fatal("an unrun check reported PASS — the false zero, pointed at a verdict")
	}
}

// TestPassAndFailComeFromARealExitCode is the one test that runs a process, and
// the process is THIS test binary — no vendor, no account, no cost. It calls
// runCheck directly rather than through startCheck, because the guarded var is
// what stops the model's own path from launching a real program and this is not
// that path (main_test.go says so beside the guard).
//
// Without it the whole feature rests on a stub returning the answer it was
// handed, which would pin the render and nothing about the claim.
func TestPassAndFailComeFromARealExitCode(t *testing.T) {
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Skip("cannot resolve the test binary")
	}
	ws := gitRepo(t)
	helper := []string{self, "-test.run=^TestArenaCheckHelperProcess$"}

	t.Setenv("TELLTALE_CHECK_HELPER", "exit")
	for _, want := range []int{0, 2, 7} {
		t.Setenv("TELLTALE_CHECK_EXIT", strconv.Itoa(want))
		res := runCheck(context.Background(), ws, helper)
		if !res.exited {
			t.Fatalf("exit %d: no code measured (%s)", want, res.err)
		}
		if res.code != want {
			t.Errorf("code = %d, want the process's own %d", res.code, want)
		}
		if res.dirty {
			t.Error("a check that wrote nothing was reported as having written")
		}
	}

	// A command with no exit code is not a FAIL. It is the third state.
	res := runCheck(context.Background(), ws, []string{filepath.Join(t.TempDir(), "telltale-no-such-binary")})
	if res.exited {
		t.Errorf("a missing binary produced exit %d", res.code)
	}
	if res.err == "" {
		t.Error("a run that could not happen carries no reason")
	}

	// A check that WRITES into a clean tree says so — the fact /adopt would
	// otherwise carry silently.
	t.Setenv("TELLTALE_CHECK_HELPER", "write")
	t.Setenv("TELLTALE_CHECK_EXIT", "0")
	if res := runCheck(context.Background(), ws, helper); !res.dirty {
		t.Errorf("a check that wrote a file left dirty=false (%+v)", res)
	}
}

// TestAStoppedCheckIsNotAFailure: the deadline (or the room closing) kills the
// process, so no exit code exists. Reporting that as FAIL would blame an
// attempt for a clock.
func TestAStoppedCheckIsNotAFailure(t *testing.T) {
	self, err := filepath.Abs(os.Args[0])
	if err != nil {
		t.Skip("cannot resolve the test binary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := runCheck(ctx, t.TempDir(), []string{self, "-test.run=^TestArenaCheckHelperProcess$"})
	if res.exited {
		t.Errorf("a stopped run reported exit %d as a verdict", res.code)
	}
	if !strings.Contains(res.err, "10m0s") {
		t.Errorf("the stop does not name its bound: %q", res.err)
	}
}

// TestArenaCheckHelperProcess is not a test. It is the program the two tests
// above run: it exits with the code they ask for, having optionally written a
// file into its working directory. The env gate keeps it inert in every
// ordinary run of the suite.
func TestArenaCheckHelperProcess(t *testing.T) {
	mode := os.Getenv("TELLTALE_CHECK_HELPER")
	if mode == "" {
		t.Skip("not a helper invocation")
	}
	if mode == "write" {
		if err := os.WriteFile("check-wrote-this.txt", []byte("x\n"), 0o644); err != nil {
			os.Exit(97)
		}
	}
	code, _ := strconv.Atoi(os.Getenv("TELLTALE_CHECK_EXIT"))
	os.Exit(code)
}
