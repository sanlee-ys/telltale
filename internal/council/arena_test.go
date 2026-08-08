package council

import (
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

	base, trees, seatErr, err := arenaSetup(ws, 7, seats)
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
	_, _, _, err := arenaSetup(t.TempDir(), 1, []model.VendorID{model.VendorCodex})
	if err == nil {
		t.Fatal("a non-repo workspace was accepted — the race would have nowhere to diff against")
	}
}

// TestCollectArenaSeesNewFiles is the `git add -N` property, and the reason it
// is load-bearing: an attempt whose whole answer is a NEW file would otherwise
// read "no changes" — a false zero, the exact class §4a.1 exists to prevent.
func TestCollectArenaSeesNewFiles(t *testing.T) {
	ws := gitRepo(t)
	base, trees, _, err := arenaSetup(ws, 1, []model.VendorID{model.VendorCodex})
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
	base, trees, _, err := arenaSetup(ws, 2, []model.VendorID{model.VendorCodex})
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
	m.turn = &turnState{
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
		arena:      true,
		arenaTrees: map[model.VendorID]string{},
	}

	m.applyEvents([]runner.Event{{Vendor: model.VendorCodex, Kind: runner.KindSession, SessionID: "throwaway-race-id"}})

	if got := m.sessions[model.VendorCodex]; got != "codex-thread" {
		t.Errorf("the race replaced the saved thread: %q", got)
	}

	// The inverse guards the guard: an ordinary turn must still record ids, or
	// this test passes while resume quietly dies everywhere.
	m.turn.arena = false
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
	if m.turn != nil {
		t.Fatal("a read room dispatched a write race")
	}
	if !strings.Contains(m.st.Notice, "/write") {
		t.Errorf("the read-room refusal does not name its in-room remedy: %q", m.st.Notice)
	}

	m.st.Write = true
	m.st.Draft = "/arena   "
	m.dispatch()
	if m.turn != nil {
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
