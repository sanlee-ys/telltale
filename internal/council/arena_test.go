package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// gitRepo builds a real repository with one commit — arena's mechanics are git
// mechanics, and asserting them against a stub would test the flag, not the
// effect. Offline: git init/commit touch no network. The identity flags keep
// the commit from depending on machine config, and the explicit initial branch
// silences the default-branch advice git otherwise prints to stderr.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := gitOut(".", "--version"); err != nil {
		t.Skip("git not available — arena tests need it; CI has it")
	}
	// The workspace is a CHILD of the temp dir, because arena worktrees are
	// SIBLINGS of the workspace and a repo at the temp root would scatter them
	// into the runner's shared temp directory.
	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit := func(args ...string) {
		t.Helper()
		if _, err := gitOut(ws, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	mustGit("init", "-b", "main")
	mustGit("config", "user.email", "arena@test.invalid")
	mustGit("config", "user.name", "arena test")
	// Byte-faithful checkouts, whatever the host's git thinks about line
	// endings. Git for Windows (and the windows-latest runner) defaults
	// core.autocrlf to true, which smudges LF to CRLF on every checkout —
	// including the one `reset --hard` performs — so a test that writes
	// "one\n", resets, and reads the file back would be measuring the HOST's
	// translation policy rather than whether the reset restored the content.
	// That is exactly how TestUndoTakesTheWholeTurnBack failed on CI while
	// passing on Linux: the undo worked, the comparison didn't.
	mustGit("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGit("add", "a.txt")
	mustGit("commit", "-m", "initial", "--no-gpg-sign")
	return ws
}

// TestArenaSetupRacesFromOneBase pins the two facts every diff depends on: all
// worktrees exist as siblings named by turn and seat, and every one of them
// starts at the SAME recorded commit.
func TestArenaSetupRacesFromOneBase(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorCodex, model.VendorAntigravity}

	_, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 7, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("seat errors on a clean repo: %v", seatErr)
	}
	head, _ := gitOut(ws, "rev-parse", "HEAD")
	if base != head {
		t.Errorf("base = %q, want the workspace HEAD %q", base, head)
	}
	for _, v := range seats {
		tree := trees[v]
		want := filepath.Join(filepath.Dir(ws), "repo-arena-t7-"+string(v))
		if tree != want {
			t.Errorf("%s tree = %q, want the sibling %q", v, tree, want)
		}
		if got, err := gitOut(tree, "rev-parse", "HEAD"); err != nil || got != base {
			t.Errorf("%s worktree HEAD = %q (%v), want the shared base", v, got, err)
		}
		if got, _ := gitOut(tree, "branch", "--show-current"); got != "arena/t7/"+string(v) {
			t.Errorf("%s branch = %q", v, got)
		}
	}
}

// TestArenaSetupRefusesOutsideARepo: the failure arrives as git's own sentence
// on the notice path, before any seat spawns.
func TestArenaSetupRefusesOutsideARepo(t *testing.T) {
	if _, err := gitOut(".", "--version"); err != nil {
		t.Skip("git not available")
	}
	_, _, _, _, _, err := arenaSetup(context.Background(), t.TempDir(), 1, []model.VendorID{model.VendorCodex}, nil)
	if err == nil {
		t.Fatal("a non-repo workspace was accepted — the race would have nowhere to diff against")
	}
}

// TestGitOutSurfacesTheFatalLineNotProgressChatter is the 2026-08-09 live
// failure as a fixture. `git worktree add -b <existing branch>` prints its
// "Preparing worktree" progress line BEFORE the fatal line and exits nonzero
// — the exact two-line stderr the live race produced on all four seats — and
// the error gitOut returns must carry git's own fatal sentence, not the
// chatter printed above it.
func TestGitOutSurfacesTheFatalLineNotProgressChatter(t *testing.T) {
	ws := gitRepo(t)
	// The branch exists first — an older room's leftover, minted by hand the
	// way the old room's arenaSetup minted it.
	if _, err := gitOut(ws, "branch", "arena/t3/claude"); err != nil {
		t.Fatal(err)
	}

	_, err := gitOut(ws, "worktree", "add", "-b", "arena/t3/claude",
		arenaTree(ws, 3, model.VendorClaude), "HEAD")
	if err == nil {
		t.Fatal("worktree add over an existing branch succeeded — the fixture lost its collision")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("the error does not carry git's fatal line: %q", err)
	}
	if strings.Contains(err.Error(), "Preparing") {
		t.Errorf("the error is progress chatter again — the live lie, back: %q", err)
	}
}

// TestGitErrLineFallsBackWhenGitMarksNothing: some git refusals print bare
// prose with no fatal:/error: prefix, and those must keep surfacing as the
// first non-empty line rather than as an empty error.
func TestGitErrLineFallsBackWhenGitMarksNothing(t *testing.T) {
	if got := gitErrLine("Preparing worktree (new branch 'arena/t3/claude')\nfatal: a branch named 'arena/t3/claude' already exists\n"); got != "fatal: a branch named 'arena/t3/claude' already exists" {
		t.Errorf("the marked line was not preferred: %q", got)
	}
	if got := gitErrLine("\nplain refusal, no prefix\n"); got != "plain refusal, no prefix" {
		t.Errorf("the fallback lost the only line there was: %q", got)
	}
	if got := gitErrLine("   \n"); got != "" {
		t.Errorf("blank stderr invented a sentence: %q", got)
	}
}

// TestArenaRaceNumbersItselfPastLeftovers is the other half of the same live
// failure: arena branches and worktrees outlive the room, the turn counter
// does not, so a fresh room's turn 3 collided with an older room's t3 and
// every seat failed at worktree add. The race number is READ from the repo's
// arena refs — a fresh room racing at turn 3 over stale t3 branches numbers
// itself t4 and every seat races clean.
func TestArenaRaceNumbersItselfPastLeftovers(t *testing.T) {
	ws := gitRepo(t)
	// The old room's residue: branches only — its worktrees may or may not
	// still exist, and its in-memory receipt is gone either way.
	for _, leftover := range []string{"arena/t3/claude", "arena/t3/codex"} {
		if _, err := gitOut(ws, "branch", leftover); err != nil {
			t.Fatal(err)
		}
	}
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex}

	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 3, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("stale branches still fail the seats — the collision is back: %v", seatErr)
	}
	if raceN != 4 {
		t.Errorf("raceN = %d, want 4 — one past the leftovers' t3", raceN)
	}
	for _, v := range seats {
		if want := arenaTree(ws, 4, v); trees[v] != want {
			t.Errorf("%s tree = %q, want %q", v, trees[v], want)
		}
		if got, _ := gitOut(trees[v], "branch", "--show-current"); got != "arena/t4/"+string(v) {
			t.Errorf("%s branch = %q, want the renumbered arena/t4/%s", v, got, v)
		}
		if got, gerr := gitOut(trees[v], "rev-parse", "HEAD"); gerr != nil || got != base {
			t.Errorf("%s worktree HEAD = %q (%v), want the shared base", v, got, gerr)
		}
	}
	// The leftovers are not touched: numbering past them is the whole fix,
	// deleting them is the user's call (/arena drop could never reach these).
	if out, _ := gitOut(ws, "branch", "--list", "arena/t3/*"); !strings.Contains(out, "arena/t3/claude") {
		t.Error("the scan deleted an older room's branch")
	}
}

// TestArenaRaceNumberFloorsAtTheTurn: with no arena refs the race is t<turn>
// (the original naming), and a scan that cannot run at all — here, no repo —
// degrades to the same floor rather than bricking /arena.
func TestArenaRaceNumberFloorsAtTheTurn(t *testing.T) {
	ws := gitRepo(t)
	if got := arenaRaceNumber(context.Background(), ws, 3); got != 3 {
		t.Errorf("a repo with no arena refs numbered the race %d, want the turn 3", got)
	}
	if got := arenaRaceNumber(context.Background(), t.TempDir(), 3); got != 3 {
		t.Errorf("a failed scan numbered the race %d, want the turn-number floor", got)
	}
}

// TestArenaResidualCollisionNamesTheFatalLineAndTheRemedy: the scan reads
// refs, so a sibling DIRECTORY an old room left behind with no branch to be
// scanned still collides at worktree add. That seat's error must now be
// git's own fatal line (fix one) plus the named remedy — never the
// "Preparing worktree" chatter the live race showed.
func TestArenaResidualCollisionNamesTheFatalLineAndTheRemedy(t *testing.T) {
	ws := gitRepo(t)
	// A non-empty directory squatting on the exact tree name this race will
	// mint (no refs exist, so the race numbers itself t1).
	squat := arenaTree(ws, 1, model.VendorCodex)
	if err := os.MkdirAll(squat, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(squat, "stale.txt"), []byte("old room\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 1, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatalf("a per-seat collision escaped to the race channel: %v", err)
	}
	if _, ok := trees[model.VendorCodex]; ok {
		t.Fatal("the squatted seat still races")
	}
	why := seatErr[model.VendorCodex]
	if !strings.Contains(why, "already exists") {
		t.Errorf("the seat's error is not git's fatal line: %q", why)
	}
	if strings.Contains(why, "Preparing") {
		t.Errorf("the seat's error is progress chatter — the live lie, back: %q", why)
	}
	if !strings.Contains(why, "git worktree remove") {
		t.Errorf("the collision does not name its remedy: %q", why)
	}
}

// TestCollectArenaSeesNewFiles is the `git add -N` property, and the reason it
// is load-bearing: an attempt whose whole answer is a NEW file would otherwise
// read "no changes" — a false zero, the exact class §4a.1 exists to prevent.
func TestCollectArenaSeesNewFiles(t *testing.T) {
	ws := gitRepo(t)
	_, base, trees, _, _, err := arenaSetup(context.Background(), ws, 1, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "brand-new.go"), []byte("package new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := collectArena(tree, base)
	if r.Err != "" {
		t.Fatalf("collect failed: %s", r.Err)
	}
	if !strings.Contains(r.Stat, "brand-new.go") {
		t.Errorf("a created file is missing from the stat — the false-zero bug:\n%s", r.Stat)
	}
	if !strings.Contains(r.Diff, "brand-new.go") {
		t.Error("a created file is missing from the yankable diff")
	}
}

// TestCollectArenaZeroAndErrorAreDifferent: an untouched tree is a measured
// nothing; an unreadable one is a failure. The two must not meet in the middle.
func TestCollectArenaZeroAndErrorAreDifferent(t *testing.T) {
	ws := gitRepo(t)
	_, base, trees, _, _, err := arenaSetup(context.Background(), ws, 2, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}

	clean := collectArena(trees[model.VendorCodex], base)
	if clean.Err != "" || strings.TrimSpace(clean.Stat) != "" {
		t.Errorf("an untouched worktree should be a clean zero: err=%q stat=%q", clean.Err, clean.Stat)
	}

	broken := collectArena(filepath.Join(ws, "no-such-tree"), base)
	if broken.Err == "" {
		t.Error("an unreadable tree reported no error — degraded dressed as zero")
	}
}

// TestArenaTurnNeverTouchesSavedThreads is the correctness property that makes
// fresh-session racing safe to ship: a race's throwaway session ids must not
// replace the room's saved conversations. Asserted through applyEvents — the
// exact path a live vendor's session event takes.
func TestArenaTurnNeverTouchesSavedThreads(t *testing.T) {
	m := clearModel()
	m.st.Columns[1].Phase = PhaseStreaming
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true,
		arenaTrees: map[model.VendorID]string{},
	})

	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindSession, SessionID: "throwaway-race-id"}})

	if got := m.sessions[model.VendorCodex]; got != "codex-thread" {
		t.Errorf("the race replaced the saved thread: %q", got)
	}

	// The inverse guards the guard: an ordinary turn must still record ids, or
	// this test passes while resume quietly dies everywhere.
	m.turnOf(model.VendorCodex).arena = false
	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindSession, SessionID: "real-new-id"}})
	if got := m.sessions[model.VendorCodex]; got != "real-new-id" {
		t.Errorf("an ordinary turn stopped recording ids: %q", got)
	}
}

// TestArenaRendersThreeOutcomesDifferently: diff, measured zero, and unreadable
// are three renders, and the frame says which one each seat got.
func TestArenaRendersThreeOutcomesDifferently(t *testing.T) {
	st := room()
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Arena = &ArenaResult{Tree: "/x/repo-arena-t3-claude", Branch: "arena/t3/claude", Base: "abcdef1234", Stat: " a.txt | 2 +-\n 1 file changed"}
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Arena = &ArenaResult{Tree: "/x/repo-arena-t3-codex", Branch: "arena/t3/codex", Base: "abcdef1234"}
	st.Columns[2].Phase = PhaseDone
	st.Columns[2].Arena = &ArenaResult{Tree: "/x/repo-arena-t3-agy", Branch: "arena/t3/agy", Base: "abcdef1234", Err: "diff unavailable: boom"}

	got := render(st)
	for _, want := range []string{
		"arena arena/t3/claude", "a.txt | 2 +-",
		"no changes against abcdef1.",
		"diff unavailable: boom",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("frame is missing %q", want)
		}
	}
}

// TestYankPrefersTheArenaDiff: on a race turn, y copies the deliverable.
func TestYankPrefersTheArenaDiff(t *testing.T) {
	st := room()
	c := &st.Columns[0]
	c.Body = "I refactored the loop."
	c.TurnN = 3
	c.Arena = &ArenaResult{Diff: "diff --git a/a.txt b/a.txt\n-one\n+two\n"}

	y := st.YankColumn(0)
	if !strings.HasPrefix(y.Text, "diff --git") {
		t.Errorf("y copied %q, want the diff", y.Text[:min(len(y.Text), 30)])
	}
	if !strings.Contains(y.Notice, "arena diff") {
		t.Errorf("notice does not say a diff was taken: %q", y.Notice)
	}

	// With no diff captured (zero or error), y falls back to the reply — a race
	// answer is still an answer.
	c.Arena = &ArenaResult{}
	if y := st.YankColumn(0); !strings.Contains(y.Text, "refactored") {
		t.Errorf("fallback yank = %q", y.Text)
	}
}

// TestArenaCommandRefusals: the two pre-flight gates, with the remedies named
// in the notice — §9.17's rule that a refusal must never point at a relaunch.
func TestArenaCommandRefusals(t *testing.T) {
	m := clearModel()
	m.st.Mode = ModeComposing

	m.st.Write = false
	m.st.Draft = "/arena fix the bug"
	m.dispatch()
	if m.anyInFlight() {
		t.Fatal("a read room dispatched a write race")
	}
	if !strings.Contains(m.st.Notice, "/write") {
		t.Errorf("the read-room refusal does not name its in-room remedy: %q", m.st.Notice)
	}

	m.st.Write = true
	m.st.Draft = "/arena   "
	m.dispatch()
	if m.anyInFlight() {
		t.Fatal("an empty brief dispatched")
	}
	if !strings.Contains(m.st.Notice, "needs a brief") {
		t.Errorf("empty-brief notice: %q", m.st.Notice)
	}

	// Vocabulary: prose that merely starts with the verb dispatches as prose.
	if _, ok := parseCommand("/arenas are cool", "/arena"); ok {
		t.Error("'/arenas' was stolen from the conversation")
	}
}

// TestArenaRanksAreHostObserved: rank is the order finishColumn fired, every
// racer gets one — a DNF ranks too — and the render pairs rank with the phase
// word so a fast crash cannot read as a podium.
func TestArenaRanksAreHostObserved(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex}
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 4, seats, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	m.st.Columns[0].Phase = PhaseStreaming
	m.st.Columns[0].TurnN = 4
	m.st.Columns[1].Phase = PhaseStreaming
	m.st.Columns[1].TurnN = 4
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorClaude: true, model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true,
		arenaRaceN: raceN,
		arenaBase:  base,
		arenaTrees: trees,
		cancel:     func() {},
	})

	// Codex lands first, failed; Claude second, done. The order of these calls
	// IS the measurement.
	m.finishColumn(&m.st.Columns[1], PhaseFailed)
	m.finishColumn(&m.st.Columns[0], PhaseDone)

	cx, cc := m.st.Columns[1].Arena, m.st.Columns[0].Arena
	if cx == nil || cc == nil {
		t.Fatal("a racer landed without an arena result")
	}
	if cx.Rank != 1 || cc.Rank != 2 || cx.Of != 2 || cc.Of != 2 {
		t.Errorf("ranks = codex %d/%d, claude %d/%d — want 1/2 and 2/2 in landing order",
			cx.Rank, cx.Of, cc.Rank, cc.Of)
	}

	// The failed seat's finish line must carry the word, not just the number.
	st := m.st
	got := render(st)
	if !strings.Contains(got, "1st of 2 · failed") {
		t.Errorf("the DNF's rank prints without its phase word:\n%s", got)
	}
	if !strings.Contains(got, "2nd of 2 · done") {
		t.Errorf("the finisher's rank line is missing or unworded")
	}
}

// TestArenaDiffToggleShowsThePatchAndNamesItsRefusals: d flips stat to patch on
// the focused seat only, and each nothing-to-show case names its reason.
func TestArenaDiffToggleShowsThePatchAndNamesItsRefusals(t *testing.T) {
	m := clearModel()
	c := &m.st.Columns[0]
	m.st.Focus = 0

	m.toggleArenaDiff()
	if !strings.Contains(m.st.Notice, "no race") {
		t.Errorf("no-race refusal: %q", m.st.Notice)
	}

	c.Arena = &ArenaResult{Err: "diff unavailable: boom"}
	m.toggleArenaDiff()
	if !strings.Contains(m.st.Notice, "boom") {
		t.Errorf("error refusal does not carry the reason: %q", m.st.Notice)
	}

	c.Arena = &ArenaResult{}
	m.toggleArenaDiff()
	if !strings.Contains(m.st.Notice, "changed nothing") {
		t.Errorf("zero refusal: %q", m.st.Notice)
	}

	c.Arena = &ArenaResult{Stat: " a.txt | 1 +", Diff: "diff --git a/a.txt b/a.txt\n+two"}
	m.toggleArenaDiff()
	if !c.ArenaShowDiff {
		t.Fatal("d did not flip to the patch")
	}
	got := render(m.st)
	if !strings.Contains(got, "+two") {
		t.Error("the patch is not on screen after d")
	}
	if strings.Contains(got, "a.txt | 1 +") {
		t.Error("the stat is still on screen beside the patch — the toggle shows one or the other")
	}
	// Per column: the second seat keeps its own stat view.
	if m.st.Columns[1].ArenaShowDiff {
		t.Error("one seat's toggle dragged its neighbour")
	}
	m.toggleArenaDiff()
	if c.ArenaShowDiff {
		t.Fatal("d did not flip back")
	}
}

// TestArenaDiffRenderIsCapped: the transcript carries at most
// arenaDiffScreenLines of patch, and the cutoff names what was dropped and both
// routes to the rest. Asserted on columnLines rather than the frame, because
// the cutoff sits at the BOTTOM of a 450-line transcript and a 24-row viewport
// shows a screenful of it — the property is about what the column holds, not
// which slice of it happens to be scrolled into view.
func TestArenaDiffRenderIsCapped(t *testing.T) {
	st := room()
	var b strings.Builder
	for i := 0; i < arenaDiffScreenLines+50; i++ {
		b.WriteString("+line\n")
	}
	st.Columns[0].Arena = &ArenaResult{Diff: b.String(), Stat: "x"}
	st.Columns[0].ArenaShowDiff = true
	st.Columns[0].Phase = PhaseDone

	lines, _ := columnLines(st, st.Columns[0], 38, PlainStyles(), GlyphsFor(false))
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "50 more lines") {
		t.Error("an over-cap patch renders without saying how much was cut")
	}
	if strings.Count(joined, "+line") > arenaDiffScreenLines {
		t.Errorf("the column holds %d patch lines, past the %d cap",
			strings.Count(joined, "+line"), arenaDiffScreenLines)
	}
}

// --- commit-per-turn and undo (§9.37, amended 2026-08-09) ---

// TestArenaCommitMsgIsATurnLabel: first line only, capped at 64 bytes on a
// rune boundary, because the subject is a `git log --oneline` label for a kept
// branch, not a second copy of the brief.
func TestArenaCommitMsgIsATurnLabel(t *testing.T) {
	if got := arenaCommitMsg(4, "fix it\nand more detail"); got != "arena t4: fix it" {
		t.Errorf("multiline brief: %q", got)
	}
	if got := arenaCommitMsg(9, "   "); got != "arena t9" {
		t.Errorf("empty brief: %q", got)
	}
	long := arenaCommitMsg(1, strings.Repeat("x", 80))
	if !strings.HasSuffix(long, "…") {
		t.Errorf("an over-cap brief was not truncated: %q", long)
	}
	if strings.Contains(long, "\n") || len(long) > len("arena t1: ")+64+len("…") {
		t.Errorf("subject too long or multiline: %q", long)
	}
}

// TestArenaCommitMakesTheAttemptDurable is half one of the amendment: once a
// racer lands and its diff is read, the whole tree state is a commit on
// arena/t<N>/<vendor> — created files included — the recorded sha is exactly
// what rev-parse reported, and the room's own repo has not moved an inch.
func TestArenaCommitMakesTheAttemptDurable(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 5, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	// One modified file and one created file: the created one is the half a
	// stage that trusted the diff's stat would miss.
	if err := os.WriteFile(filepath.Join(tree, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "new.go"), []byte("package new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	c := &m.st.Columns[1] // codex
	c.Phase = PhaseStreaming
	c.TurnN = 5
	c.Prompt = "fix the flaky poller retry loop so CI stops going red on Tuesdays"
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)

	r := c.Arena
	if r == nil {
		t.Fatal("no arena result")
	}
	if r.CommitErr != "" {
		t.Fatalf("the commit degraded: %s", r.CommitErr)
	}
	head, _ := gitOut(tree, "rev-parse", "HEAD")
	if r.Commit == "" || r.Commit != head {
		t.Errorf("Commit = %q, want the measured tip %q", r.Commit, head)
	}
	if r.Commit == base {
		t.Error("the tip did not move — nothing was committed")
	}
	// The branch ref agrees, read from the shared repo rather than the tree —
	// this is what makes the worktree deletable without losing the attempt.
	if got, _ := gitOut(ws, "rev-parse", "arena/t5/codex"); got != r.Commit {
		t.Errorf("arena/t5/codex = %q, want %q", got, r.Commit)
	}
	// The commit carries the racer's files and the turn-derived subject.
	shown, _ := gitOut(tree, "show", "--name-only", "--format=%s", "HEAD")
	for _, want := range []string{"arena t5: fix the flaky poller retry loop", "a.txt", "new.go"} {
		if !strings.Contains(shown, want) {
			t.Errorf("the commit is missing %q:\n%s", want, shown)
		}
	}
	if dirty, _ := gitOut(tree, "status", "--porcelain"); dirty != "" {
		t.Errorf("the worktree is dirty after its own commit:\n%s", dirty)
	}
	// The room's repo: same HEAD, same branch, clean tree. The commit ran in
	// the racer's tree ONLY.
	if got, _ := gitOut(ws, "rev-parse", "HEAD"); got != base {
		t.Errorf("the room repo's HEAD moved: %q", got)
	}
	if got, _ := gitOut(ws, "branch", "--show-current"); got != "main" {
		t.Errorf("the room repo's branch moved: %q", got)
	}
	if dirty, _ := gitOut(ws, "status", "--porcelain"); dirty != "" {
		t.Errorf("the room repo picked up state:\n%s", dirty)
	}
	// And the column renders the receipt short — display truncates, the
	// record does not.
	if got := render(m.st); !strings.Contains(got, "committed "+shortSHA(r.Commit)+".") {
		t.Error("the commit receipt is not on the column")
	}
}

// TestArenaCommitFallsBackToALocalIdentity is the CI-runner shape: a machine
// with NO git identity anywhere still parks the attempt, via per-command -c
// flags — never a config write, which on a worktree would land in the shared
// repo config, i.e. in the room's repo.
func TestArenaCommitFallsBackToALocalIdentity(t *testing.T) {
	if _, err := gitOut(".", "--version"); err != nil {
		t.Skip("git not available")
	}
	// Point every config layer at an empty file so the machine's own identity
	// cannot leak into the fixture: the property is "no identity, still commits".
	empty := filepath.Join(t.TempDir(), "empty-gitconfig")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", empty)
	t.Setenv("GIT_CONFIG_SYSTEM", empty)

	root := t.TempDir()
	ws := filepath.Join(root, "repo")
	if err := os.Mkdir(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	// The fixture's own commit supplies identity inline, so the REPO stays
	// identity-less the way a fresh runner's does.
	if _, err := gitOut(ws, "-c", "user.name=fixture", "-c", "user.email=fixture@test.invalid",
		"commit", "--allow-empty", "-m", "initial", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}

	_, base, trees, _, _, err := arenaSetup(context.Background(), ws, 1, []model.VendorID{model.VendorClaude}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorClaude]
	if err := os.WriteFile(filepath.Join(tree, "answer.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sha, err := commitArena(tree, base, "arena t1: x")
	if err != nil {
		t.Fatalf("an identity-less machine could not commit: %v", err)
	}
	if sha == "" || sha == base {
		t.Fatalf("no commit landed: %q", sha)
	}
	if got, _ := gitOut(tree, "log", "-1", "--format=%ce"); got != "arena@telltale.invalid" {
		t.Errorf("committer email = %q, want the fallback identity", got)
	}
}

// TestArenaCommitFailureDegradesTheSeatAlone: a commit that cannot land is a
// named reason on THAT seat's receipt. Its diff is still read, its phase is
// still its own, the other racer commits fine, and the room repo is
// untouched. The injected failure is a stale lock on the seat's own branch
// ref — git's classic crashed-process residue, and a seam that fails ONLY
// the ref update: add, status and diff all still work, which is what lets
// the test hold the diff-was-still-read property at the same time. (Assumes
// git's default files ref backend, like the loose-ref reads in gitRepo.)
func TestArenaCommitFailureDegradesTheSeatAlone(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex}
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 3, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	bad, good := trees[model.VendorClaude], trees[model.VendorCodex]
	lock := filepath.Join(ws, ".git", "refs", "heads", "arena", "t3", "claude.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tr := range []string{bad, good} {
		if err := os.WriteFile(filepath.Join(tr, "work.txt"), []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := clearModel()
	for _, i := range []int{0, 1} {
		m.st.Columns[i].Phase = PhaseStreaming
		m.st.Columns[i].TurnN = 3
		m.st.Columns[i].Prompt = "try it"
	}
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorClaude: true, model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(&m.st.Columns[0], PhaseDone)
	m.finishColumn(&m.st.Columns[1], PhaseDone)

	cl, cx := m.st.Columns[0].Arena, m.st.Columns[1].Arena
	if cl == nil || cx == nil {
		t.Fatal("a racer landed without an arena result")
	}
	if cl.Commit != "" {
		t.Errorf("a commit that failed to sign reported a sha: %q", cl.Commit)
	}
	if !strings.HasPrefix(cl.CommitErr, "not committed: ") {
		t.Errorf("the failure is not a named receipt degradation: %q", cl.CommitErr)
	}
	if m.st.Columns[0].Phase != PhaseDone {
		t.Errorf("a commit failure re-labelled the seat's own turn: %v", m.st.Columns[0].Phase)
	}
	if cl.Err != "" || !strings.Contains(cl.Stat, "work.txt") {
		t.Errorf("the failed-commit seat lost its diff: err=%q stat=%q", cl.Err, cl.Stat)
	}
	if got, _ := gitOut(ws, "rev-parse", "arena/t3/claude"); got != base {
		t.Errorf("the failed seat's branch moved anyway: %q", got)
	}
	// The neighbour is not this seat's blast radius.
	if cx.CommitErr != "" || cx.Commit == "" {
		t.Errorf("the other racer was dragged down: commit=%q err=%q", cx.Commit, cx.CommitErr)
	}
	if got, _ := gitOut(ws, "rev-parse", "HEAD"); got != base {
		t.Errorf("the room repo's HEAD moved: %q", got)
	}
	if got := render(m.st); !strings.Contains(got, "not committed:") {
		t.Error("the degradation is not on the column")
	}
}

// TestArenaZeroDiffCommitsNothing pins the empty-commit ruling: a measured
// zero gets NO commit — an empty commit would be a receipt claiming work that
// did not happen — and no failure either, because nothing was owed. The
// branch stays at base and the "no changes" sentence stays the whole story.
func TestArenaZeroDiffCommitsNothing(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 2, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	c := &m.st.Columns[1]
	c.Phase = PhaseStreaming
	c.TurnN = 2
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)

	r := c.Arena
	if r.Commit != "" || r.CommitErr != "" {
		t.Errorf("a measured zero produced a commit receipt: commit=%q err=%q", r.Commit, r.CommitErr)
	}
	if got, _ := gitOut(ws, "rev-parse", "arena/t2/codex"); got != base {
		t.Errorf("the branch moved on a zero-diff attempt: %q", got)
	}
	got := render(m.st)
	if !strings.Contains(got, "no changes against") {
		t.Error("the zero sentence is gone")
	}
	if strings.Contains(got, "committed ") || strings.Contains(got, "not committed") {
		t.Error("a zero-diff column carries a commit line")
	}
}

// TestArenaSelfCommittedAttemptKeepsItsOwnTip: a racer that committed for
// itself mid-turn leaves a clean tree ahead of base. Its own tip IS the
// durable receipt — reported as measured, never papered over with an empty
// commit on top.
func TestArenaSelfCommittedAttemptKeepsItsOwnTip(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 6, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "own.txt"), []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(tree, "add", "own.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(tree, "commit", "-m", "racer's own commit", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}
	own, _ := gitOut(tree, "rev-parse", "HEAD")

	m := clearModel()
	c := &m.st.Columns[1]
	c.Phase = PhaseStreaming
	c.TurnN = 6
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)

	r := c.Arena
	if r.Commit != own {
		t.Errorf("Commit = %q, want the racer's own tip %q", r.Commit, own)
	}
	if r.CommitErr != "" {
		t.Errorf("a self-committed attempt was called a failure: %q", r.CommitErr)
	}
	if got, _ := gitOut(tree, "rev-parse", "HEAD"); got != own {
		t.Errorf("an empty commit was stacked on the racer's own: %q", got)
	}
	// The diff still answers against BASE, so the mid-turn commit cannot hide
	// the work (the claude-squad anchor rule, exercised end to end).
	if !strings.Contains(r.Stat, "own.txt") {
		t.Errorf("the self-committed work vanished from the stat:\n%s", r.Stat)
	}
}

// TestUndoTakesTheWholeTurnBack is half two: u, y-confirmed, resets the
// racer's worktree AND its arena branch to the recorded base — one command,
// agreeing by construction — and the column says so while the room repo
// never moves.
func TestUndoTakesTheWholeTurnBack(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 7, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tree, "created.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	m.st.Workspace = ws
	m.st.Focus = 1
	c := &m.st.Columns[1]
	c.Phase = PhaseStreaming
	c.TurnN = 7
	c.Prompt = "race it"
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)
	if c.Arena.Commit == "" {
		t.Fatalf("fixture: the attempt was not committed: %+v", c.Arena)
	}

	m.askUndoSeat()
	if m.undoPending != model.VendorCodex {
		t.Fatalf("u did not arm the focused seat: %q (notice %q)", m.undoPending, m.st.Notice)
	}
	m.undoGateKey(key("y"))

	if !c.Arena.Undone {
		t.Fatal("the result does not record the undo")
	}
	if got, _ := gitOut(tree, "rev-parse", "HEAD"); got != base {
		t.Errorf("the worktree is not back at base: %q", got)
	}
	if b, err := os.ReadFile(filepath.Join(tree, "a.txt")); err != nil || string(b) != "one\n" {
		t.Errorf("a modified file was not restored: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(tree, "created.txt")); !os.IsNotExist(err) {
		t.Error("a created file survived the undo")
	}
	if dirty, _ := gitOut(tree, "status", "--porcelain"); dirty != "" {
		t.Errorf("the worktree is dirty after the undo:\n%s", dirty)
	}
	// Branch and tree agree: the ref moved back with the reset.
	if got, _ := gitOut(ws, "rev-parse", "arena/t7/codex"); got != base {
		t.Errorf("the branch still holds the undone commit: %q", got)
	}
	// The room repo: never touched by either half.
	if got, _ := gitOut(ws, "rev-parse", "HEAD"); got != base {
		t.Errorf("the room repo's HEAD moved: %q", got)
	}
	if dirty, _ := gitOut(ws, "status", "--porcelain"); dirty != "" {
		t.Errorf("the room repo picked up state:\n%s", dirty)
	}
	if !strings.Contains(m.st.Notice, "undone") {
		t.Errorf("the notice does not report the undo: %q", m.st.Notice)
	}
	// Asserted on the column's own lines at a width the sentence fits in one
	// piece, because at grid width it wraps — the property is that the COLUMN
	// says it, not that the notice does.
	lines, _ := columnLines(m.st, m.st.Columns[1], 80, PlainStyles(), GlyphsFor(false))
	if got := strings.Join(lines, "\n"); !strings.Contains(got, "undone — worktree and branch are back at "+shortSHA(base)+".") {
		t.Errorf("the column does not say the attempt was taken back:\n%s", got)
	}

	// A second press is refused as already done — not re-run as a no-op that
	// pretends to act.
	m.askUndoSeat()
	if m.undoPending != "" {
		t.Error("a second undo armed on an already-undone attempt")
	}
	if !strings.Contains(m.st.Notice, "already undone") {
		t.Errorf("the second press is not its own sentence: %q", m.st.Notice)
	}
}

// TestUndoRefusalsEachNameTheirReason: no race, changed nothing, already
// undone, and mid-turn are four different facts and four different sentences.
func TestUndoRefusalsEachNameTheirReason(t *testing.T) {
	m := clearModel()
	m.st.Focus = 0
	c := &m.st.Columns[0]

	occupy(m)
	m.askUndoSeat()
	if m.undoPending != "" || !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("mid-turn refusal: pending=%q notice=%q", m.undoPending, m.st.Notice)
	}
	idle(m)

	m.askUndoSeat()
	if m.undoPending != "" || !strings.Contains(m.st.Notice, "no race") {
		t.Errorf("no-race refusal: %q", m.st.Notice)
	}

	c.Arena = &ArenaResult{Tree: "/x/repo-arena-t1-claude", Base: "abcdef1234"}
	m.askUndoSeat()
	if m.undoPending != "" || !strings.Contains(m.st.Notice, "changed nothing") {
		t.Errorf("zero-diff refusal: %q", m.st.Notice)
	}

	c.Arena = &ArenaResult{Tree: "/x/repo-arena-t1-claude", Base: "abcdef1234",
		Stat: " a.txt | 1 +", Undone: true}
	m.askUndoSeat()
	if m.undoPending != "" || !strings.Contains(m.st.Notice, "already undone") {
		t.Errorf("already-undone refusal: %q", m.st.Notice)
	}
}

// TestUndoResetFailureSurfacesGitsOwnSentence: refusal three — the reset
// itself failed, and the notice carries git's first stderr line rather than a
// generic shrug. Undone stays unset: the room may not claim a rollback it did
// not measure happening.
func TestUndoResetFailureSurfacesGitsOwnSentence(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 8, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "gone.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	m.st.Workspace = ws
	m.st.Focus = 1
	c := &m.st.Columns[1]
	c.Phase = PhaseStreaming
	c.TurnN = 8
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)

	// The tree vanishes between the race and the undo — a user deleted it by
	// hand, which kept-until-deleted invites.
	if err := os.RemoveAll(tree); err != nil {
		t.Fatal(err)
	}
	m.askUndoSeat()
	if m.undoPending == "" {
		t.Fatalf("undo did not arm: %q", m.st.Notice)
	}
	m.undoGateKey(key("y"))

	if !strings.HasPrefix(m.st.Notice, "undo failed: ") || len(m.st.Notice) <= len("undo failed: ") {
		t.Errorf("the failure does not carry git's own sentence: %q", m.st.Notice)
	}
	if c.Arena.Undone {
		t.Error("a failed reset was recorded as an undo")
	}
}

// TestUndoNeverTouchesTheRoomRepo is the path guard: the reset runs only on
// the exact tree name this room would have created for this seat this turn.
// A recorded tree that points anywhere else — here, forged to the workspace
// itself — is refused before git runs at all.
func TestUndoNeverTouchesTheRoomRepo(t *testing.T) {
	ws := gitRepo(t)
	base, _ := gitOut(ws, "rev-parse", "HEAD")
	// Uncommitted work in the room repo: exactly what a --hard reset aimed
	// wrong would destroy.
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("precious uncommitted edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	m.st.Workspace = ws
	m.st.Focus = 0
	c := &m.st.Columns[0]
	c.TurnN = 2
	c.Arena = &ArenaResult{Tree: ws, Base: base, Branch: "arena/t2/claude", RaceN: 2, Stat: " a.txt | 1 +"}

	m.askUndoSeat()
	if m.undoPending == "" {
		t.Fatalf("fixture did not arm: %q", m.st.Notice)
	}
	m.undoGateKey(key("y"))

	if !strings.Contains(m.st.Notice, "not an arena tree") {
		t.Errorf("the guard did not name its refusal: %q", m.st.Notice)
	}
	if c.Arena.Undone {
		t.Error("a refused undo was recorded as done")
	}
	if b, err := os.ReadFile(filepath.Join(ws, "a.txt")); err != nil || string(b) != "precious uncommitted edit\n" {
		t.Fatalf("THE ROOM REPO WAS RESET: %q %v", b, err)
	}
}

// TestUndoGateKeepsAndCancels: n is a decision, a stray key is not, and the
// two get different sentences — clearGateKey's contract on the new gate.
func TestUndoGateKeepsAndCancels(t *testing.T) {
	for _, tc := range []struct {
		name, press, notice string
	}{
		{"n keeps", "n", "kept"},
		{"a stray key cancels", "j", "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := clearModel()
			m.st.Focus = 0
			c := &m.st.Columns[0]
			c.TurnN = 1
			c.Arena = &ArenaResult{
				Tree: arenaTree(m.st.Workspace, 1, model.VendorClaude),
				Base: "abcdef1234", RaceN: 1, Stat: " a.txt | 1 +",
			}
			m.askUndoSeat()
			if m.undoPending != model.VendorClaude {
				t.Fatalf("u did not arm: %q", m.st.Notice)
			}
			m.undoGateKey(key(tc.press))
			if m.undoPending != "" {
				t.Error("the gate is still pending after an answer")
			}
			if c.Arena.Undone {
				t.Error("a declined undo ran anyway")
			}
			if !strings.Contains(m.st.Notice, tc.notice) {
				t.Errorf("notice %q does not contain %q", m.st.Notice, tc.notice)
			}
		})
	}
}

// TestUndoIsViewModeOnly keeps the contract q, f and c already keep: in
// compose, u is the letter u.
func TestUndoIsViewModeOnly(t *testing.T) {
	m := clearModel()
	m.st.Mode = ModeComposing

	m.key(key("u"))

	if m.undoPending != "" {
		t.Errorf("u armed an undo while composing: %s", m.undoPending)
	}
	if !strings.Contains(m.st.Draft, "u") {
		t.Errorf("u was swallowed instead of typed: draft = %q", m.st.Draft)
	}
}

// TestARaceThatOutranItsTurnStillCommitsAndUndoes drives the diverged-number
// case end to end: an older room's t3 leftover pushes a turn-3 race to t4, so
// Column.TurnN (3) and the race number (4) disagree — and everything that
// derives a name must read the RECORDED race number, or the receipt claims a
// branch that does not exist and undo's path guard refuses a legitimate tree.
func TestARaceThatOutranItsTurnStillCommitsAndUndoes(t *testing.T) {
	ws := gitRepo(t)
	if _, err := gitOut(ws, "branch", "arena/t3/codex"); err != nil {
		t.Fatal(err)
	}
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 3, []model.VendorID{model.VendorCodex}, nil)
	if err != nil || len(seatErr) != 0 {
		t.Fatalf("setup: %v %v", err, seatErr)
	}
	if raceN != 4 {
		t.Fatalf("fixture: raceN = %d, want 4", raceN)
	}
	tree := trees[model.VendorCodex]
	if werr := os.WriteFile(filepath.Join(tree, "a.txt"), []byte("two\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}

	m := clearModel()
	m.st.Workspace = ws
	m.st.Focus = 1
	c := &m.st.Columns[1]
	c.Phase = PhaseStreaming
	c.TurnN = 3 // the room's own turn — deliberately NOT the race number
	c.Prompt = "race past the leftovers"
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true, arenaRaceN: raceN, arenaBase: base, arenaTrees: trees,
		cancel: func() {},
	})
	m.finishColumn(c, PhaseDone)

	r := c.Arena
	if r == nil {
		t.Fatal("no arena result")
	}
	// The receipt carries the race's names, not the turn's.
	if r.Branch != "arena/t4/codex" || r.RaceN != 4 {
		t.Fatalf("receipt = branch %q raceN %d, want the renumbered arena/t4/codex", r.Branch, r.RaceN)
	}
	if r.CommitErr != "" || r.Commit == "" {
		t.Fatalf("the attempt did not commit: %q %q", r.Commit, r.CommitErr)
	}
	if got, _ := gitOut(ws, "rev-parse", "arena/t4/codex"); got != r.Commit {
		t.Errorf("arena/t4/codex = %q, want the receipt's %q", got, r.Commit)
	}
	if subject, _ := gitOut(tree, "log", "-1", "--format=%s"); !strings.HasPrefix(subject, "arena t4: ") {
		t.Errorf("commit subject = %q, want the race's t4, not the turn's t3", subject)
	}

	// Undo's path guard recomputes from the RECORDED race number and matches.
	m.askUndoSeat()
	if m.undoPending != model.VendorCodex {
		t.Fatalf("undo did not arm: %q", m.st.Notice)
	}
	m.undoGateKey(key("y"))
	if !r.Undone {
		t.Fatalf("the diverged numbers refused a legitimate undo: %q", m.st.Notice)
	}
	if got, _ := gitOut(ws, "rev-parse", "arena/t4/codex"); got != base {
		t.Errorf("the branch did not come back to base: %q", got)
	}
	// The old room's leftover was never this race's to touch.
	if got, _ := gitOut(ws, "rev-parse", "arena/t3/codex"); got != base {
		t.Errorf("the leftover branch moved: %q", got)
	}
}

// oneShotRacer is a racerHandle that records its kill, standing in for the
// process dispatch's arena branch keys by vendor. Kill is the whole interface.
type oneShotRacer struct{ killed bool }

func (h *oneShotRacer) Kill() { h.killed = true }

// TestAWarmSeatsRacerRetiresOnItsOwnExit is the one-shot half of KindDone's
// two-processes-one-vendor-id attribution rule, and it exists because the room
// shipped without it.
//
// A one-shot racer ends its turn by EXITING, so that exit is its column's only
// retirement signal. When the same vendor ALSO runs a persistent room seat, the
// stale-exit guard reads that live process as "this seat is fine" and eats the
// exit — and the column then streams until the user cancels, with no diff, no
// commit, no rank and no seed receipt, because every one of those runs inside
// finishColumn.
//
// Measured on the reference box, race t10, 2026-08-13: the racer exited 52
// seconds in with its reply complete and the room rendered `streaming` for 21
// minutes. The trigger is a WARM seat — m.procs must already hold a live
// process for that vendor, which is why a race run before the room's first
// ordinary brief always landed and this one could not.
func TestAWarmSeatsRacerRetiresOnItsOwnExit(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, _, err := arenaSetup(context.Background(), ws, 4, []model.VendorID{model.VendorClaude}, nil)
	if err != nil {
		t.Fatal(err)
	}

	m := clearModel()
	m.st.Workspace = ws
	c := m.column(model.VendorClaude)
	c.Phase = PhaseStreaming
	c.TurnN = 4
	c.Body = "done."
	handle := &oneShotRacer{}
	m.holdTurn(&turnState{
		live:         map[model.VendorID]bool{model.VendorClaude: true},
		persistent:   map[model.VendorID]bool{},
		arena:        true,
		arenaRaceN:   raceN,
		arenaBase:    base,
		arenaTrees:   trees,
		arenaHandles: map[model.VendorID]racerHandle{model.VendorClaude: handle},
		cancel:       func() {},
	})
	// The room's own seat, ALIVE, wearing the same vendor id — the whole
	// precondition. deadSession reports Alive() true.
	m.procs[model.VendorClaude] = &seatProc{sess: deadSession{}, dir: ws}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})

	if c.Phase != PhaseDone {
		t.Fatalf("phase = %v — the racer's exit did not retire its column; a live room seat ate it", c.Phase)
	}
	if c.Arena == nil {
		t.Error("the column landed with no arena result — the diff, the commit and the seed receipt all live behind this")
	}
	if _, ok := m.procs[model.VendorClaude]; !ok {
		t.Error("the racer's exit took the ROOM's live process with it — it is still running and now invisible")
	}
	if m.turnOf(model.VendorClaude) != nil {
		t.Error("the landed racer never left the turn's live set, so the turn cannot end")
	}
}

// TestABackgroundRoomSeatDeathDoesNotRetireAOneShotRace is the rule's other
// half, and the reason arenaRacing is keyed presence rather than a hole in the
// guard: a vendor that is NOT racing keeps the stale-exit guard exactly as it
// was, so an ordinary turn's predecessor exit is still discarded.
func TestABackgroundRoomSeatDeathDoesNotRetireAOneShotRace(t *testing.T) {
	m := clearModel()
	c := m.column(model.VendorClaude)
	c.Phase = PhaseStreaming
	m.holdTurn(&turnState{
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{model.VendorClaude: true},
		cancel:     func() {},
	})
	m.procs[model.VendorClaude] = &seatProc{sess: deadSession{}}

	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})

	if c.Phase != PhaseStreaming {
		t.Errorf("phase = %v — a predecessor's exit retired a column its process was not driving", c.Phase)
	}
}
