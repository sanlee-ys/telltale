package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The brief-file tests run against gitRepo's real temp repositories for the
// same reason the seeding tests do: the property under test is a git property.
// "Council's file never reaches the attempt's stat or its commit" is a claim
// about what `git add -N .`, `git add -A` and `git status --porcelain` do with
// a pathspec, and a stub would assert the argv rather than the outcome. No test
// here spawns a vendor.

// TestBriefLandsInEveryRacerTreeIdentically is the feature's founding case:
// every seat gets the file, and every seat gets the SAME bytes — the
// identical-per-seat position, checked rather than asserted in a comment.
func TestBriefLandsInEveryRacerTreeIdentically(t *testing.T) {
	ws := gitRepo(t)
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorGrok}

	raceN, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 1, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "rename the widget", trees, seatErr)
	if len(seatErr) != 0 {
		t.Fatalf("seat errors on a writable tree: %v", seatErr)
	}

	var first string
	for _, v := range seats {
		got, rerr := os.ReadFile(filepath.Join(trees[v], arenaBriefFileName))
		if rerr != nil {
			t.Fatalf("%s: %v", v, rerr)
		}
		if !strings.Contains(string(got), "rename the widget") {
			t.Errorf("%s brief file does not carry the brief: %q", v, got)
		}
		if !strings.Contains(string(got), arenaConduct) {
			t.Errorf("%s brief file does not carry the conduct line", v)
		}
		if !strings.HasPrefix(string(got), arenaBriefMarker) {
			t.Errorf("%s brief file does not open with the marker: %q", v, got)
		}
		if first == "" {
			first = string(got)
		} else if string(got) != first {
			t.Errorf("%s brief file differs from the first seat's — the race is no longer one question", v)
		}
	}
}

// TestBriefNeverReachesTheAttemptsDiff is the honesty property the whole
// pathspec exists for: the racer's stat names the racer's work and nothing
// council wrote.
func TestBriefNeverReachesTheAttemptsDiff(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 2, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "add a file", trees, seatErr)
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "new.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := collectArena(tree, base)
	if r.Err != "" {
		t.Fatalf("collect: %s", r.Err)
	}
	if strings.Contains(r.Stat, arenaBriefFileName) {
		t.Errorf("stat carries council's brief file:\n%s", r.Stat)
	}
	if !strings.Contains(r.Stat, "new.txt") {
		t.Errorf("stat lost the racer's own file:\n%s", r.Stat)
	}
	if strings.Contains(r.Diff, arenaBriefFileName) {
		t.Errorf("patch carries council's brief file:\n%s", r.Diff)
	}
}

// TestBriefOnlyTreeIsStillAMeasuredZero: a seat that changed nothing must keep
// the "no changes against <base>" sentence and commit nothing, even though
// council's own file is sitting in its tree. Without the pathspec on the dirty
// check this is the false nonzero — a receipt claiming work that never
// happened.
func TestBriefOnlyTreeIsStillAMeasuredZero(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 3, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "do nothing", trees, seatErr)
	tree := trees[model.VendorCodex]

	r := collectArena(tree, base)
	if r.Err != "" || r.Stat != "" {
		t.Fatalf("stat = %q err = %q, want a measured zero", r.Stat, r.Err)
	}
	sha, cerr := commitArena(tree, base, "arena t3: do nothing")
	if cerr != nil {
		t.Fatalf("commit: %v", cerr)
	}
	if sha != "" {
		t.Errorf("committed %s on a zero-diff attempt — the empty-commit ruling", sha)
	}
}

// TestAdoptedCommitNeverCarriesTheBrief pins what /adopt merges. The branch is
// what an adoption lands, so council's file staying off it is the property that
// keeps a stray AGENTS.md out of the operator's repo.
func TestAdoptedCommitNeverCarriesTheBrief(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 4, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "add a file", trees, seatErr)
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, "new.txt"), []byte("work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	collectArena(tree, base)

	sha, cerr := commitArena(tree, base, "arena t4: add a file")
	if cerr != nil {
		t.Fatalf("commit: %v", cerr)
	}
	if sha == "" {
		t.Fatal("nothing committed for an attempt that changed a file")
	}
	names, gerr := gitOut(tree, "--no-pager", "show", "--name-only", "--format=", sha)
	if gerr != nil {
		t.Fatal(gerr)
	}
	if strings.Contains(names, arenaBriefFileName) {
		t.Errorf("the adopted commit carries council's brief file: %q", names)
	}
	if !strings.Contains(names, "new.txt") {
		t.Errorf("the adopted commit lost the racer's file: %q", names)
	}
	// And the file is still on disk: the worktree is KEPT, so what the seat was
	// told stays visible beside what it produced.
	if _, serr := os.Stat(filepath.Join(tree, arenaBriefFileName)); serr != nil {
		t.Errorf("brief file gone from the kept worktree: %v", serr)
	}
}

// TestRacerAuthoredAgentsFileIsNotHidden is the exclusion's own limit, pinned
// so it cannot quietly widen: the path is excluded only while the file is
// still council's. A racer that replaces it has authored a file, and the diff
// says so.
func TestRacerAuthoredAgentsFileIsNotHidden(t *testing.T) {
	ws := gitRepo(t)
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 5, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "write the contributor guide", trees, seatErr)
	tree := trees[model.VendorCodex]
	if err := os.WriteFile(filepath.Join(tree, arenaBriefFileName), []byte("# how to work here\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if args := arenaBriefArgs(tree); args != nil {
		t.Errorf("still excluding a file council no longer owns: %v", args)
	}
	r := collectArena(tree, base)
	if !strings.Contains(r.Stat, arenaBriefFileName) {
		t.Errorf("the racer's own AGENTS.md is missing from the stat:\n%s", r.Stat)
	}
}

// TestBriefNeverOverwritesTheRepositorysOwn: a repo that ships an AGENTS.md
// keeps it, on every seat, and council adds no words to the file channel at
// all. The pathspec must then be off, or a racer's edit to the repo's file
// would vanish from the diff.
func TestBriefNeverOverwritesTheRepositorysOwn(t *testing.T) {
	ws := gitRepo(t)
	if err := os.WriteFile(filepath.Join(ws, arenaBriefFileName), []byte("# the repo's own\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "add", arenaBriefFileName); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "commit", "-m", "agents", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}

	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 6, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "edit the guide", trees, seatErr)
	if len(seatErr) != 0 {
		t.Fatalf("a repo carrying its own AGENTS.md must not fail a seat: %v", seatErr)
	}
	tree := trees[model.VendorCodex]
	got, rerr := os.ReadFile(filepath.Join(tree, arenaBriefFileName))
	if rerr != nil || string(got) != "# the repo's own\n" {
		t.Fatalf("repo's AGENTS.md = %q (%v), want it untouched", got, rerr)
	}
	if args := arenaBriefArgs(tree); args != nil {
		t.Errorf("excluding the repository's own file: %v", args)
	}
	if err := os.WriteFile(filepath.Join(tree, arenaBriefFileName), []byte("# the repo's own\nedited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := collectArena(tree, base)
	if !strings.Contains(r.Stat, arenaBriefFileName) {
		t.Errorf("a racer's edit to the repo's own AGENTS.md is missing from the stat:\n%s", r.Stat)
	}
}

// TestBriefWriteFailureSkipsThatSeat: a tree the room cannot brief races a
// different question from its siblings, so it does not race — the seeding
// rule, applied for the seeding rule's reason.
func TestBriefWriteFailureSkipsThatSeat(t *testing.T) {
	ws := gitRepo(t)
	raceN, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 7, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	// The tree itself removed: os.WriteFile then fails on both platforms this
	// ships to, which is what the branch needs. Windows is the primary target
	// (ADR-002), so a permission trick that only bites on Unix would leave the
	// branch untested where it matters most.
	if err := os.RemoveAll(tree); err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "brief", trees, seatErr)
	if seatErr[model.VendorCodex] == "" {
		t.Error("a seat that could not be briefed races anyway, with nothing said")
	}
	if !strings.HasPrefix(seatErr[model.VendorCodex], "brief file failed: ") {
		t.Errorf("seat error = %q, want the named reason", seatErr[model.VendorCodex])
	}
	if _, ok := trees[model.VendorCodex]; ok {
		t.Error("a skipped seat kept its worktree in the dispatch map")
	}
}

// TestBriefLeavesAPreExistingPathAlone: the never-overwrite rule is about the
// PATH, not about the file's shape. Anything already standing there is the
// repository's, so council writes nothing, fails nothing, and excludes
// nothing.
func TestBriefLeavesAPreExistingPathAlone(t *testing.T) {
	ws := gitRepo(t)
	raceN, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 9, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	if err := os.Mkdir(filepath.Join(tree, arenaBriefFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	writeArenaBriefs(context.Background(), raceN, "brief", trees, seatErr)
	if len(seatErr) != 0 {
		t.Fatalf("a pre-existing path must not fail the seat: %v", seatErr)
	}
	if _, ok := trees[model.VendorCodex]; !ok {
		t.Error("the seat lost its worktree over a file council chose not to write")
	}
	if args := arenaBriefArgs(tree); args != nil {
		t.Errorf("excluding a path council never wrote: %v", args)
	}
}

// TestAdoptOfABriefOnlyRacerStillRefuses: /adopt reads the racer's tree to
// decide whether there is anything to adopt at all, and council's own file must
// not answer that question. Without the exclusion the room would offer to adopt
// a seat that changed nothing — the false nonzero, on the verb where it costs a
// merge commit.
func TestAdoptOfABriefOnlyRacerStillRefuses(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	writeArenaBriefs(context.Background(), m.lastRace.raceN, "do nothing",
		m.lastRace.trees, map[model.VendorID]string{})

	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "changed nothing in the race") {
		t.Fatalf("adopt armed over a tree holding only council's brief file: %q", m.st.Notice)
	}
}

// TestAdoptNeverMergesTheBriefFile is the same property one verb further on:
// a racer whose work never reached commitArena is committed by /adopt itself,
// and that commit must carry the racer's work alone. This is the give-up
// path's shape — race t9 cut two seats exactly this way.
func TestAdoptNeverMergesTheBriefFile(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	writeArenaBriefs(context.Background(), m.lastRace.raceN, "add a file",
		m.lastRace.trees, map[model.VendorID]string{})
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "y")
	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("adopt did not report success: %q", m.st.Notice)
	}
	if _, err := os.Stat(filepath.Join(ws, "answer.go")); err != nil {
		t.Error("the attempt's file did not arrive in the room repo")
	}
	if _, err := os.Stat(filepath.Join(ws, arenaBriefFileName)); err == nil {
		t.Error("the adoption planted council's brief file in the operator's repo")
	}
}

// TestDropOfABriefOnlyRacerNeedsNoForce: `/arena drop` refuses over
// uncommitted work and names the `!` spelling that discards it. Council's own
// file is not the racer's work, so a clean attempt must drop on the plain
// spelling — otherwise this feature makes every drop a forced one.
func TestDropOfABriefOnlyRacerNeedsNoForce(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	tree := m.lastRace.trees[model.VendorCodex]
	writeArenaBriefs(context.Background(), m.lastRace.raceN, "do nothing",
		m.lastRace.trees, map[model.VendorID]string{})

	drop(t, m, "codex")
	if strings.Contains(m.st.Notice, "uncommitted") {
		t.Fatalf("drop refused over council's own brief file: %q", m.st.Notice)
	}
	if _, err := os.Stat(tree); err == nil {
		t.Error("the worktree survived a drop that reported no refusal")
	}
}

// TestBriefStopsOnAnEndedSetup: the setup's context is the room's patience, and
// a brief pass that outlived it writes into trees nobody will race.
func TestBriefStopsOnAnEndedSetup(t *testing.T) {
	ws := gitRepo(t)
	raceN, _, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 8, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	writeArenaBriefs(ctx, raceN, "brief", trees, seatErr)
	if _, serr := os.Stat(filepath.Join(trees[model.VendorCodex], arenaBriefFileName)); serr == nil {
		t.Error("wrote a brief file for a setup that had already ended")
	}
}
