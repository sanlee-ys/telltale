package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The seeding tests run against gitRepo's real temp repositories, because
// seeding's contract IS git's contract: the candidate set is what
// `ls-files --others` lists, and the gap being filled is what a fresh
// worktree lacks. A stub would test the pattern grammar and miss both.

// seedWrite drops one untracked file into the workspace, parents included.
func seedWrite(t *testing.T, ws, rel, content string) {
	t.Helper()
	p := filepath.Join(ws, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestSeedCarriesIgnoredFilesIntoEveryRacerTree is the feature's founding
// case: a .env absent from a clean checkout arrives in each racer's tree,
// and the receipt counts what was actually copied, per seat.
func TestSeedCarriesIgnoredFilesIntoEveryRacerTree(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, ".env", "SECRET=1\n")
	seedWrite(t, ws, seedFileName, "# what a race needs to run\n\n.env\n")
	seats := []model.VendorID{model.VendorClaude, model.VendorCodex}

	_, _, trees, seeds, seatErr, err := arenaSetup(context.Background(), ws, 1, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("seat errors on a clean seed: %v", seatErr)
	}
	for _, v := range seats {
		got, rerr := os.ReadFile(filepath.Join(trees[v], ".env"))
		if rerr != nil || string(got) != "SECRET=1\n" {
			t.Errorf("%s tree .env = %q (%v), want the room's bytes", v, got, rerr)
		}
		s := seeds[v]
		if s == nil || s.Files != 1 {
			t.Errorf("%s seed report = %+v, want 1 file copied", v, s)
		}
		if s != nil && len(s.Notices) != 0 {
			t.Errorf("%s notices on a fully-matched file: %v", v, s.Notices)
		}
	}
}

// TestSeedCreatesNestedParents: a directory pattern seeds the subtree, and
// the copy builds the parent directories the relative path demands.
func TestSeedCreatesNestedParents(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, "config/local/dev.yaml", "port: 1\n")
	seedWrite(t, ws, seedFileName, "config/\n")

	_, _, trees, seeds, _, err := arenaSetup(context.Background(), ws, 2, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tree := trees[model.VendorCodex]
	got, rerr := os.ReadFile(filepath.Join(tree, "config", "local", "dev.yaml"))
	if rerr != nil || string(got) != "port: 1\n" {
		t.Errorf("nested seed = %q (%v)", got, rerr)
	}
	if s := seeds[model.VendorCodex]; s == nil || s.Files != 1 {
		t.Errorf("seed report = %+v", s)
	}
}

// TestSeedNamesThePatternThatMatchedNothing: a stale entry is a named notice
// — an allowlist-shaped file must fail visibly both ways — but it degrades
// nothing: the matched pattern still copies and the seat still races.
func TestSeedNamesThePatternThatMatchedNothing(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, ".env", "K=V\n")
	seedWrite(t, ws, seedFileName, ".env\n*.secret\n")

	_, _, trees, seeds, seatErr, err := arenaSetup(context.Background(), ws, 3, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("a no-match pattern degraded a seat: %v", seatErr)
	}
	if _, ok := trees[model.VendorCodex]; !ok {
		t.Fatal("a no-match pattern kept the seat out of the race")
	}
	s := seeds[model.VendorCodex]
	if s == nil || s.Files != 1 {
		t.Fatalf("seed report = %+v, want the matched file still copied", s)
	}
	joined := strings.Join(s.Notices, "\n")
	if !strings.Contains(joined, `"*.secret"`) || !strings.Contains(joined, "no untracked file matches") {
		t.Errorf("the stale pattern is not named: %v", s.Notices)
	}
}

// TestSeedRefusesEscapesByName: absolute patterns, `..` traversal, and
// negation are each refused per-pattern with the reason in the sentence —
// and a refusal is a notice, never a race failure.
func TestSeedRefusesEscapesByName(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, seedFileName, "../outside\n/etc/passwd\n!.env\n")

	_, _, trees, seeds, seatErr, err := arenaSetup(context.Background(), ws, 4, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("a refused pattern degraded a seat: %v", seatErr)
	}
	if _, ok := trees[model.VendorCodex]; !ok {
		t.Fatal("a refused pattern kept the seat out of the race")
	}
	s := seeds[model.VendorCodex]
	if s == nil {
		t.Fatal("no seed report — a refused file still ran seeding")
	}
	if s.Files != 0 {
		t.Errorf("refused patterns copied %d files", s.Files)
	}
	joined := strings.Join(s.Notices, "\n")
	for pat, why := range map[string]string{
		`"../outside"`:  "above the repo root",
		`"/etc/passwd"`: "absolute",
		`"!.env"`:       "negation",
	} {
		if !strings.Contains(joined, pat) || !strings.Contains(joined, why) {
			t.Errorf("refusal for %s (%s) is unnamed in %v", pat, why, s.Notices)
		}
	}
}

// TestSeedRefusesOverBudgetByName: past the budget nothing is copied and the
// refusal carries the measured total. Asserted at the plan level with a tiny
// budget — the constant stays 64 MiB and no test writes a 64 MiB fixture.
func TestSeedRefusesOverBudgetByName(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, "big.bin", strings.Repeat("x", 100))
	seedWrite(t, ws, seedFileName, "big.bin\n")

	plan := loadSeedPlan(context.Background(), ws, 10)
	if plan == nil {
		t.Fatal("plan is nil with a .worktreeinclude present")
	}
	if len(plan.files) != 0 {
		t.Errorf("an over-budget plan still holds %d files to copy", len(plan.files))
	}
	joined := strings.Join(plan.notices, "\n")
	if !strings.Contains(joined, "seeding refused") || !strings.Contains(joined, "100 B") || !strings.Contains(joined, "10 B budget") {
		t.Errorf("the budget refusal does not carry the measured total: %v", plan.notices)
	}

	// The same file under the real budget copies — the refusal above was the
	// budget's doing, not the pattern's.
	if real := loadSeedPlan(context.Background(), ws, seedBudgetBytes); len(real.files) != 1 {
		t.Errorf("under the real budget the plan holds %d files, want 1", len(real.files))
	}
}

// TestSeedCopyErrorDegradesTheSeatNotTheRace pins the error CHANNEL: a copy
// that fails lands in seatErr — the same per-seat lane a failed worktree add
// uses — never in the race-level err. The trees are byte-identical per seat,
// so a deterministic copy error necessarily hits every seat alike; what the
// channel guarantees is that each failure is scoped to its own seat and the
// race machinery (base recorded, no wholesale abort) survives.
//
// The collision: the base commit tracks cfg/ as a DIRECTORY, so the checkout
// creates it in every racer tree; the room's working copy replaced it with an
// untracked FILE named cfg, which seeding then cannot write over the
// directory.
func TestSeedCopyErrorDegradesTheSeatNotTheRace(t *testing.T) {
	ws := gitRepo(t)
	seedWrite(t, ws, "cfg/inner.txt", "tracked\n")
	if _, err := gitOut(ws, "add", "cfg"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "commit", "-m", "track cfg dir", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(ws, "cfg")); err != nil {
		t.Fatal(err)
	}
	seedWrite(t, ws, "cfg", "now a file\n")
	seedWrite(t, ws, seedFileName, "cfg\n")

	_, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 5, []model.VendorID{model.VendorClaude, model.VendorCodex}, nil)
	if err != nil {
		t.Fatalf("a per-seat copy error escaped to the race channel: %v", err)
	}
	if base == "" {
		t.Error("the base was not recorded — the race machinery did not survive")
	}
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex} {
		why := seatErr[v]
		if !strings.Contains(why, "seeding failed") || !strings.Contains(why, "cfg") {
			t.Errorf("%s degrade reason does not name the seed or the path: %q", v, why)
		}
		if _, ok := trees[v]; ok {
			t.Errorf("%s still races from a tree the room knows is half-seeded", v)
		}
		// The worktree stays on disk: kept-until-deleted receipts include the
		// broken ones.
		if _, serr := os.Stat(arenaTree(ws, 5, v)); serr != nil {
			t.Errorf("%s worktree receipt is gone: %v", v, serr)
		}
	}
}

// TestSeedSymlinksAreNamedNotFollowed: a symlink match copies nothing and
// says so — Windows is primary and symlink semantics differ per platform, so
// following one would be a different behavior per machine.
func TestSeedSymlinksAreNamedNotFollowed(t *testing.T) {
	ws := gitRepo(t)
	if err := os.Symlink("a.txt", filepath.Join(ws, "link.env")); err != nil {
		t.Skipf("symlinks unavailable here (Windows without privilege): %v", err)
	}
	seedWrite(t, ws, seedFileName, "link.env\n")

	_, _, trees, seeds, _, err := arenaSetup(context.Background(), ws, 6, []model.VendorID{model.VendorCodex}, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := seeds[model.VendorCodex]
	if s == nil || s.Files != 0 {
		t.Fatalf("seed report = %+v, want zero copies", s)
	}
	if !strings.Contains(strings.Join(s.Notices, "\n"), "symlink not copied: link.env") {
		t.Errorf("the symlink refusal is unnamed: %v", s.Notices)
	}
	if _, serr := os.Lstat(filepath.Join(trees[model.VendorCodex], "link.env")); !os.IsNotExist(serr) {
		t.Errorf("something landed at the symlink's path: %v", serr)
	}
}

// TestSeedLineRendersMeasuredZeroAndAbsentDiffer: "seeded N files" states
// the per-seat measured count, a report that copied nothing renders
// "seeded 0 files", and a seat with no report (no .worktreeinclude) renders
// no seed line at all — zero and absent stay two facts on screen.
func TestSeedLineRendersMeasuredZeroAndAbsentDiffer(t *testing.T) {
	st := room()
	st.Columns[0].Phase = PhaseDone
	st.Columns[0].Arena = &ArenaResult{
		Tree: "/x/repo-arena-t3-claude", Branch: "arena/t3/claude", Base: "abcdef1234",
		Seed: &SeedReport{Files: 3, Notices: []string{`no untracked file matches "*.secret"`}},
	}
	st.Columns[1].Phase = PhaseDone
	st.Columns[1].Arena = &ArenaResult{Tree: "/x/repo-arena-t3-codex", Branch: "arena/t3/codex", Base: "abcdef1234"}
	st.Columns[2].Phase = PhaseDone
	st.Columns[2].Arena = &ArenaResult{
		Tree: "/x/repo-arena-t3-agy", Branch: "arena/t3/agy", Base: "abcdef1234",
		Seed: &SeedReport{Files: 0},
	}

	got := render(st)
	if !strings.Contains(got, "seeded 3 files") {
		t.Error("the measured count is not on the column")
	}
	if !strings.Contains(got, `no untracked file matches "*.secret"`) {
		t.Error("the stale-pattern notice is not on the column")
	}
	if !strings.Contains(got, "seeded 0 files") {
		t.Error("a measured zero does not render — zero collapsed into absent")
	}
	if n := strings.Count(got, "seeded "); n != 2 {
		t.Errorf("%d seed lines rendered, want 2 — the seedless seat must draw nothing", n)
	}
}

// TestSeedMatchGrammar pins the documented subset: bare names match at any
// depth, anchored patterns match from the root, * stays within a segment,
// ** spans segments, and a directory pattern takes the subtree.
func TestSeedMatchGrammar(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{".env", ".env", true},
		{".env", "sub/dir/.env", true},
		{"*.local", "conf/a.local", true},
		{"fixtures", "test/fixtures/big.bin", true}, // bare name matches a dir segment
		{"config/", "config/local/dev.yaml", true},  // trailing slash: the subtree
		{"config/*.yaml", "config/a.yaml", true},
		{"config/*.yaml", "config/deep/a.yaml", false}, // * does not cross /
		{"config/**/*.yaml", "config/deep/a.yaml", true},
		{"config/**", "config/deep/a.yaml", true},
		{".env", "not-env", false},
		{"a/b", "b", false}, // anchored: no bare-tail match
	}
	for _, c := range cases {
		if got := seedMatch(c.pattern, c.rel); got != c.want {
			t.Errorf("seedMatch(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}
