package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// racedModel is a room that has already run a race: a real repo (gitRepo), a
// real arenaSetup for the named seats, and the receipt recorded the way
// dispatch records it. The lifecycle verbs act on the receipt plus the real
// git state, so a stub race would test the map and not the effect — the same
// argument gitRepo's own comment makes.
//
// Signing is pinned off repo-locally: adopt commits and merges with the
// user's own git config on purpose (lifecycle.go), and a host whose global
// config signs commits would otherwise make these tests measure the host.
func racedModel(t *testing.T, seats ...model.VendorID) (*Model, string) {
	t.Helper()
	ws := gitRepo(t)
	if _, err := gitOut(ws, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatal(err)
	}
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 4, seats, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(seatErr) != 0 {
		t.Fatalf("seat errors on a clean repo: %v", seatErr)
	}
	m := clearModel()
	m.st.Workspace = ws
	m.lastRace = &arenaRace{workspace: ws, raceN: raceN, base: base, trees: trees}
	return m, ws
}

// scribble makes one racer's attempt: a new file, uncommitted — exactly the
// state a finished arena seat leaves behind (commit-per-turn is deferred).
func scribble(t *testing.T, m *Model, v model.VendorID, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.lastRace.trees[v], name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// adopt drives the whole in-room path: the command through roomCommand (so the
// registry and the parse are under test, not just the function), then the gate.
func adopt(t *testing.T, m *Model, arg, answer string) {
	t.Helper()
	m.setDraft("/adopt " + arg)
	if !m.roomCommand() {
		t.Fatalf("/adopt %s dispatched to the vendors instead of being intercepted", arg)
	}
	if answer != "" {
		if m.adoptPending == "" {
			t.Fatalf("/adopt %s did not arm the gate: %q", arg, m.st.Notice)
		}
		m.adoptGateKey(key(answer))
	}
}

// drop drives /arena drop through dispatch, which is the path a real draft
// takes — the drop verb lives inside the /arena parse.
func drop(t *testing.T, m *Model, spec string) {
	t.Helper()
	m.setDraft("/arena drop " + spec)
	if cmd := m.dispatch(); cmd != nil {
		t.Fatalf("/arena drop %s returned a dispatch command — a drop must never spawn", spec)
	}
	if m.anyInFlight() {
		t.Fatalf("/arena drop %s started a turn", spec)
	}
}

// TestAdoptMergesTheRacerIntoTheRoom is the happy ending: the attempt —
// uncommitted work in the racer's tree — arrives in the room repo as a real
// --no-ff merge of the arena branch, and the worktree is NOT deleted (adopt
// and drop are two verbs on purpose).
func TestAdoptMergesTheRacerIntoTheRoom(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "git merge --no-ff arena/t4/codex") {
		t.Errorf("the gate's question does not name the command y will run: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "commits its worktree") {
		t.Errorf("the question hides the commit that precedes the merge: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "cuts adopt/t4-codex") {
		t.Errorf("the question hides the branch the adoption cuts: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))

	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("adopt did not report success: %q", m.st.Notice)
	}
	if _, err := os.Stat(filepath.Join(ws, "answer.go")); err != nil {
		t.Error("the attempt's file did not arrive in the room repo")
	}
	// Merged means REACHABLE: nothing left on the branch the room lacks.
	if n, err := gitOut(ws, "rev-list", "--count", "HEAD..arena/t4/codex"); err != nil || n != "0" {
		t.Errorf("the arena branch still holds unreachable commits: %q (%v)", n, err)
	}
	// A merge commit, not a fast-forward — the adoption stays visible.
	if parents, _ := gitOut(ws, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(parents)) != 3 {
		t.Errorf("HEAD is not a two-parent merge: %q", parents)
	}
	if _, err := os.Stat(m.lastRace.trees[model.VendorCodex]); err != nil {
		t.Error("adopt deleted the worktree — that is drop's job, on the user's word")
	}
}

// TestAdoptLandsOnAFreshBranch is the 2026-08-11 ruling (§9.37's open question,
// option b): the merge lands on adopt/t<N>-<vendor>, the room is left standing
// there, and the branch the room came from does not move at all — which is what
// makes the hand-off one `gh pr create` instead of the four hand-run git
// commands the first live adoption cost.
func TestAdoptLandsOnAFreshBranch(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	mainAt, err := gitOut(ws, "rev-parse", "main")
	if err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "codex", "y")

	if !strings.Contains(m.st.Notice, "adopted codex onto adopt/t4-codex") {
		t.Fatalf("the notice does not name the branch the adoption landed on: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "gh pr create") {
		t.Errorf("the notice does not name the next command: %q", m.st.Notice)
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "adopt/t4-codex" {
		t.Errorf("the room was left on %q, not on the branch it cut", on)
	}
	// The whole point: main is exactly where it was, so nothing has to be
	// reset before it is pushed.
	if now, _ := gitOut(ws, "rev-parse", "main"); now != mainAt {
		t.Errorf("main moved during the adoption: %q → %q", mainAt, now)
	}
	if n, _ := gitOut(ws, "rev-list", "--count", "main..adopt/t4-codex"); n != "2" {
		t.Errorf("the adopt branch holds %s commits over main, want the attempt plus its merge", n)
	}
}

// TestAdoptSuffixesAColliderRatherThanFailing: race numbers repeat once a
// race's branches are dropped, and an operator can adopt the same racer twice,
// so a taken adopt/t<N>-<vendor> takes the next name instead of failing or —
// far worse — landing the merge on an older adoption's branch. The card names
// the suffixed branch too, because the card names what y actually runs.
func TestAdoptSuffixesAColliderRatherThanFailing(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	// The older adoption's branch, still sitting in the repo.
	if _, err := gitOut(ws, "branch", "adopt/t4-codex"); err != nil {
		t.Fatal(err)
	}
	squatterAt, _ := gitOut(ws, "rev-parse", "adopt/t4-codex")

	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "cuts adopt/t4-codex-2") {
		t.Fatalf("the card does not name the free branch: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))

	if !strings.Contains(m.st.Notice, "adopted codex onto adopt/t4-codex-2") {
		t.Fatalf("the adoption did not take the next free name: %q", m.st.Notice)
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "adopt/t4-codex-2" {
		t.Errorf("the room was left on %q", on)
	}
	if now, _ := gitOut(ws, "rev-parse", "adopt/t4-codex"); now != squatterAt {
		t.Errorf("the older adoption's branch was moved: %q → %q", squatterAt, now)
	}
}

// TestAdoptRefusesADirtyRoomTree: adopt must never eat the user's uncommitted
// work, so a room with a TRACKED change refuses by name, before any gate arms.
// Tracked, deliberately — this test used to plant an untracked file, and under
// adoptBlockers' refined rule an untracked bystander is exactly what no longer
// blocks (its own test below); a modified tracked file is the unconditional
// case.
func TestAdoptRefusesADirtyRoomTree(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("half-typed edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "codex", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over a dirty room tree")
	}
	if !strings.Contains(m.st.Notice, "uncommitted") || !strings.Contains(m.st.Notice, "room tree") {
		t.Errorf("the refusal does not name the dirty room tree: %q", m.st.Notice)
	}
	if got, _ := gitOut(ws, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("something was committed or merged behind the refusal: %s commits", got)
	}
}

// TestAdoptIgnoresUntrackedBystanders is the t9 incident as a fixture: the
// first live adopt was refused over `?? .claude/` — an untracked settings
// directory the merge would never have written. An untracked path the
// adoption does not touch is a bystander, not a hazard, and the gate arms
// and the merge lands with it sitting right there, untouched.
func TestAdoptIgnoresUntrackedBystanders(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	if err := os.MkdirAll(filepath.Join(ws, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".claude", "settings.local.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "codex", "y")

	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("a bystander directory blocked the adopt: %q", m.st.Notice)
	}
	if body, err := os.ReadFile(filepath.Join(ws, ".claude", "settings.local.json")); err != nil || string(body) != "{}\n" {
		t.Errorf("the bystander did not survive the merge untouched: %q (%v)", body, err)
	}
}

// TestAdoptRefusesAnUntrackedCollisionByName: the one untracked shape that IS
// a hazard — a path the adoption itself writes — refuses before the gate
// arms, naming both the squatter and the incoming file, because letting the
// merge discover it would end in git's own overwrite refusal after y.
func TestAdoptRefusesAnUntrackedCollisionByName(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	// The racer's attempt must be ON its branch for the collision to be
	// computable at arm time — commit it the way finishColumn would have.
	tree := m.lastRace.trees[model.VendorCodex]
	if _, err := gitOut(tree, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(tree, "commit", "-m", "attempt", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "answer.go"), []byte("package squatter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "codex", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over an untracked file the merge writes")
	}
	if !strings.Contains(m.st.Notice, "answer.go") {
		t.Errorf("the refusal does not name the colliding path: %q", m.st.Notice)
	}
	if body, _ := os.ReadFile(filepath.Join(ws, "answer.go")); string(body) != "package squatter\n" {
		t.Errorf("the squatting file was disturbed behind a refusal: %q", body)
	}
}

// TestAdoptOfAZeroChangeRacerRefuses: a clean worktree on an unmoved branch is
// a measured nothing, and adopting it would mint an empty merge commit.
func TestAdoptOfAZeroChangeRacerRefuses(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)

	adopt(t, m, "codex", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over an attempt that changed nothing")
	}
	if !strings.Contains(m.st.Notice, "changed nothing") {
		t.Errorf("the zero refusal does not say so: %q", m.st.Notice)
	}
	if got, _ := gitOut(ws, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("an empty adopt still moved the room repo: %s commits", got)
	}
}

// commitAttempt parks a racer's scribble on its own arena branch, the way
// finishColumn's commitArena does at the end of a real race. The divergence
// preview reads COMMITTED state, so a test about what it counts has to put the
// attempt where a finished race would have left it.
func commitAttempt(t *testing.T, m *Model, v model.VendorID) {
	t.Helper()
	tree := m.lastRace.trees[v]
	if _, err := gitOut(tree, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(tree, "commit", "-m", "attempt", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}
}

// roomCommit moves the room's own branch, which is the state the preview exists
// for: everything that landed since the race was cut.
func roomCommit(t *testing.T, ws, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(ws, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "add", name); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "commit", "-m", "the room moves "+name, "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}
}

// TestTheAdoptCardSaysWhatItIsMergingInto is the preview on a room that has not
// moved: the counts carry the branch they are measured against, the overlap is
// a MEASURED zero, and the older contract — the exact commands y runs — is
// still on the card beside it.
func TestTheAdoptCardSaysWhatItIsMergingInto(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	commitAttempt(t, m, model.VendorCodex)

	adopt(t, m, "codex", "")
	if m.adoptPending == "" {
		t.Fatalf("the preview refused a clean adoption: %q", m.st.Notice)
	}
	// The baseline is named, and both counts sit under it.
	if !strings.Contains(m.st.Notice, "vs main: 1 ahead, 0 behind") {
		t.Errorf("the card does not say what it is merging into: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "no overlapping path") {
		t.Errorf("a measured zero overlap is not reported: %q", m.st.Notice)
	}
	// The preview ADDS to the card; it does not displace what y promises.
	if !strings.Contains(m.st.Notice, "git merge --no-ff arena/t4/codex") ||
		!strings.Contains(m.st.Notice, "cuts adopt/t4-codex") {
		t.Errorf("the preview cost the card its own contract: %q", m.st.Notice)
	}
}

// TestTheAdoptCardCountsWhatLandedSinceTheRace is the case the whole preview is
// for: the room moved after the race, so the racer is behind, and one path was
// written on both sides. Nothing here says "conflict" — this merge may still
// land cleanly, and the card claims only what git measured.
func TestTheAdoptCardCountsWhatLandedSinceTheRace(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "a.txt", "the racer's line\n")
	commitAttempt(t, m, model.VendorCodex)
	roomCommit(t, ws, "a.txt", "the room's line\n")

	adopt(t, m, "codex", "")
	if m.adoptPending == "" {
		t.Fatalf("a divergent racer was refused rather than previewed: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "vs main: 1 ahead, 1 behind") {
		t.Errorf("the card does not count the room's own commit: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "1 overlapping path (a.txt)") {
		t.Errorf("the card does not name the path both sides wrote: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Notice, "conflict") {
		t.Errorf("the card called an overlap a conflict — that is a merge result, not a read: %q", m.st.Notice)
	}
}

// TestTheAdoptCardSaysWhenTheCountsExcludeUncommittedWork is the preview's own
// limit, on the card rather than left to be discovered. Every figure is read
// off committed state, so a racer whose worktree is still dirty has work that
// none of them cover — and the card that will commit that worktree says so.
func TestTheAdoptCardSaysWhenTheCountsExcludeUncommittedWork(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "")
	if m.adoptPending == "" {
		t.Fatalf("an uncommitted attempt was refused: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "these counts exclude 1 uncommitted path") {
		t.Errorf("the card hides that its counts miss the racer's dirty tree: %q", m.st.Notice)
	}
	// The counts themselves stay honest rather than being adjusted upward to
	// cover a commit nobody has made: nothing is on the branch yet.
	if !strings.Contains(m.st.Notice, "vs main: 0 ahead, 0 behind") {
		t.Errorf("the counts were inflated past what the branch holds: %q", m.st.Notice)
	}
}

// TestOverlapAbsentAndUnreadableStayApart is §4a.1 on this preview's one
// advisory read. "Nothing overlaps" and "nobody could look" are different
// facts, and a check that could not run must never render as a clean one.
func TestOverlapAbsentAndUnreadableStayApart(t *testing.T) {
	ws := gitRepo(t)

	// A branch that does not exist is the read failing, not the answer being
	// empty — git refuses the range and says so.
	if _, err := readAdoptOverlap(ws, "arena/t4/nobody"); err == nil {
		t.Fatal("an unreadable range answered as if it had been read")
	} else if got := (adoptDivergence{base: "main", overlapErr: err}).sentence(0); !strings.Contains(got, "could not run") {
		t.Errorf("an unreadable overlap does not say so: %q", got)
	} else if strings.Contains(got, "no overlapping path") {
		t.Errorf("an unreadable overlap rendered as a clean one: %q", got)
	}

	zero := (adoptDivergence{base: "main"}).sentence(0)
	if !strings.Contains(zero, "no overlapping path") {
		t.Errorf("a measured zero does not say so: %q", zero)
	}
	if strings.Contains(zero, "could not run") {
		t.Errorf("a measured zero rendered as an unreadable one: %q", zero)
	}
}

// TestTheAdoptCountsAlwaysCarryTheirBaseline: a count with no baseline is a
// number with no question attached, so the clause is written even when git
// cannot name the ref — and a detached room is named by its commit rather than
// dropped.
func TestTheAdoptCountsAlwaysCarryTheirBaseline(t *testing.T) {
	unnamed := (adoptDivergence{ahead: 2}).sentence(0)
	if !strings.Contains(unnamed, "vs the room's HEAD: 2 ahead, 0 behind") {
		t.Errorf("a baseline git could not name was dropped instead of stated: %q", unnamed)
	}

	ws := gitRepo(t)
	if got := adoptBase(ws); got != "main" {
		t.Errorf("adoptBase on a checked-out branch = %q, want main", got)
	}
	if _, err := gitOut(ws, "checkout", "--detach"); err != nil {
		t.Fatal(err)
	}
	head, err := gitOut(ws, "rev-parse", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got := adoptBase(ws); got != head {
		t.Errorf("a detached room is named %q, want the commit %q", got, head)
	}
}

// TestAdoptConflictAbortsCleanly: a merge that conflicts is aborted, the room
// tree comes back exactly as it was, and the notice says a human merge is
// needed — never a repo left mid-merge with markers nobody asked for.
func TestAdoptConflictAbortsCleanly(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	// The racer rewrites a.txt one way…
	scribble(t, m, model.VendorCodex, "a.txt", "racer's line\n")
	// …and the room moves the same file the other way, committed, so the room
	// tree is clean and only the merge itself can fail.
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("room's line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(ws, "commit", "-am", "room moves a.txt", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "codex", "y")

	if !strings.Contains(m.st.Notice, "human merge") {
		t.Errorf("a conflicted adopt did not hand the merge to a human: %q", m.st.Notice)
	}
	if out, _ := gitOut(ws, "status", "--porcelain"); out != "" {
		t.Errorf("the room tree was not restored after the abort:\n%s", out)
	}
	if _, err := gitOut(ws, "rev-parse", "--verify", "MERGE_HEAD"); err == nil {
		t.Error("the repo is still mid-merge — abort did not run")
	}
	// An adoption that did not land leaves nothing behind: the room is back on
	// its own branch and the branch cut for the merge is gone, rather than an
	// empty branch handed over as the receipt of a failure.
	if !strings.Contains(m.st.Notice, "the room is back on main") {
		t.Errorf("the notice does not say where the room now stands: %q", m.st.Notice)
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "main" {
		t.Errorf("the room was left on %q after a failed adoption", on)
	}
	if out, _ := gitOut(ws, "branch", "--list", "adopt/t4-codex"); out != "" {
		t.Errorf("the failed adoption left its branch behind: %q", out)
	}
	if body, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(body) != "room's line\n" {
		t.Errorf("a.txt was left as %q — the room's own content did not survive", body)
	}
	// The attempt survives on its branch: aborting the merge must not cost the
	// work, which is now committed there for the human merge the notice names.
	if n, _ := gitOut(ws, "rev-list", "--count", "arena/t4/codex"); n != "2" {
		t.Errorf("the attempt is not on the arena branch after the abort: %s commits", n)
	}
}

// TestAdoptRefusalsNameTheMissingPiece: no race, an unknown name, and a seat
// that did not race are three different absences with three sentences.
func TestAdoptRefusalsNameTheMissingPiece(t *testing.T) {
	bare := clearModel()
	bare.setDraft("/adopt codex")
	bare.roomCommand()
	if !strings.Contains(bare.st.Notice, "no race has run") {
		t.Errorf("no-race refusal: %q", bare.st.Notice)
	}

	m, _ := racedModel(t, model.VendorCodex)
	m.setDraft("/adopt codexx")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "no racer called codexx") {
		t.Errorf("typo refusal: %q", m.st.Notice)
	}
	m.setDraft("/adopt claude")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "no kept worktree") {
		t.Errorf("did-not-race refusal: %q", m.st.Notice)
	}
	if m.adoptPending != "" {
		t.Fatal("a refusal armed the gate")
	}

	// And mid-turn, the standing rule: between turns, like /cd.
	occupy(m)
	m.setDraft("/adopt codex")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("mid-turn refusal: %q", m.st.Notice)
	}
}

// TestAdoptGateAnswers: n keeps everything, and a key nobody meant cancels —
// clearGateKey's rule, because this too interrupts nothing.
func TestAdoptGateAnswers(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "n")
	if got, _ := gitOut(ws, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("n still merged: %s commits", got)
	}
	if !strings.Contains(m.st.Notice, "nothing was merged") {
		t.Errorf("n's answer: %q", m.st.Notice)
	}

	adopt(t, m, "codex", "x")
	if m.adoptPending != "" {
		t.Error("a stray key left the gate armed")
	}
	if got, _ := gitOut(ws, "rev-list", "--count", "HEAD"); got != "1" {
		t.Errorf("a stray key merged: %s commits", got)
	}
}

// TestDropRemovesTreeAndBranch: a racer with nothing to lose — clean tree,
// nothing unadopted — drops without ceremony, tree and branch both.
func TestDropRemovesTreeAndBranch(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	tree := m.lastRace.trees[model.VendorCodex]

	drop(t, m, "codex")

	if !strings.Contains(m.st.Notice, "dropped codex") {
		t.Fatalf("drop did not report: %q", m.st.Notice)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Error("the worktree directory survived the drop")
	}
	if out, _ := gitOut(ws, "branch", "--list", "arena/t4/codex"); out != "" {
		t.Errorf("the arena branch survived the drop: %q", out)
	}
	// Dropped means gone from the receipt too: a second drop is told there is
	// no worktree, not handed a git error about a missing path.
	drop(t, m, "codex")
	if !strings.Contains(m.st.Notice, "no kept worktree") {
		t.Errorf("re-drop refusal: %q", m.st.Notice)
	}
}

// TestDropRefusesToLoseUncommittedWork, and the force word forces: the refusal
// names how many paths would be lost and spells the force form; the bang form
// then actually deletes.
func TestDropRefusesToLoseUncommittedWork(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	tree := m.lastRace.trees[model.VendorCodex]

	drop(t, m, "codex")
	if !strings.Contains(m.st.Notice, "1 uncommitted path") {
		t.Errorf("the refusal does not count what would be lost: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "/arena drop codex!") {
		t.Errorf("the refusal does not spell the force form: %q", m.st.Notice)
	}
	if _, err := os.Stat(tree); err != nil {
		t.Fatal("a refused drop still removed the tree")
	}
	if m.st.Draft == "" {
		t.Error("the refused draft was discarded — forcing means retyping the whole command")
	}

	drop(t, m, "codex!")
	if !strings.Contains(m.st.Notice, "dropped codex") {
		t.Fatalf("the force word did not force: %q", m.st.Notice)
	}
	if _, err := os.Stat(tree); !os.IsNotExist(err) {
		t.Error("the forced drop left the tree standing")
	}
}

// TestDropRefusesUnadoptedCommits: work committed on the arena branch but not
// merged into the room is named — count, the /adopt remedy, and the force
// form — and an adopt then clears the guard, which is the intended lifecycle.
func TestDropRefusesUnadoptedCommits(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	tree := m.lastRace.trees[model.VendorCodex]
	if _, err := gitOut(tree, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitOut(tree, "commit", "-m", "attempt", "--no-gpg-sign"); err != nil {
		t.Fatal(err)
	}

	drop(t, m, "codex")
	if !strings.Contains(m.st.Notice, "1 commit the room has not merged") {
		t.Errorf("the refusal does not count the unadopted commits: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "/adopt codex") || !strings.Contains(m.st.Notice, "/arena drop codex!") {
		t.Errorf("the refusal does not offer both ways forward: %q", m.st.Notice)
	}
	if _, err := os.Stat(tree); err != nil {
		t.Fatal("a refused drop still removed the tree")
	}

	adopt(t, m, "codex", "y")
	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("adopt after the refusal failed: %q", m.st.Notice)
	}
	drop(t, m, "codex")
	if !strings.Contains(m.st.Notice, "dropped codex") {
		t.Errorf("an adopted racer still would not drop: %q", m.st.Notice)
	}
}

// TestDropRefusesANonArenaTree is the path check: a receipt entry that does
// not re-derive to the name arenaSetup would have minted is refused even under
// force — whatever that path is, this room's arena did not create it.
func TestDropRefusesANonArenaTree(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	m.lastRace.trees[model.VendorCodex] = ws // doctored: the room repo itself

	drop(t, m, "codex!")
	if !strings.Contains(m.st.Notice, "not this race's worktree") {
		t.Errorf("the path check did not refuse: %q", m.st.Notice)
	}
	if _, err := os.Stat(filepath.Join(ws, "a.txt")); err != nil {
		t.Fatal("the doctored drop removed the room repo")
	}
}

// TestDropAllDegradesPerSeat: one clean racer and one dirty one — the clean
// tree goes, the dirty one is kept and named, and the force spelling then
// clears the survivor. A batch that refused wholesale would punish the clean
// trees for the dirty one.
func TestDropAllDegradesPerSeat(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex, model.VendorAntigravity)
	scribble(t, m, model.VendorAntigravity, "half.go", "package half\n")
	codexTree := m.lastRace.trees[model.VendorCodex]
	agyTree := m.lastRace.trees[model.VendorAntigravity]

	drop(t, m, "all")
	if !strings.Contains(m.st.Notice, "dropped codex") {
		t.Errorf("the clean racer was not dropped: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "agy:") || !strings.Contains(m.st.Notice, "uncommitted") {
		t.Errorf("the kept racer is not named with its reason: %q", m.st.Notice)
	}
	if _, err := os.Stat(codexTree); !os.IsNotExist(err) {
		t.Error("the clean tree survived drop all")
	}
	if _, err := os.Stat(agyTree); err != nil {
		t.Error("the dirty tree was removed without force")
	}

	drop(t, m, "all!")
	if _, err := os.Stat(agyTree); !os.IsNotExist(err) {
		t.Error("drop all! left the dirty tree standing")
	}
}

// TestDropVocabulary: bare drop reports usage, an unknown name is refused with
// the alphabet, and a three-word draft opening with "drop" is NOT the verb —
// it is a brief, per the only-a-draft-that-IS-a-command rule.
func TestDropVocabulary(t *testing.T) {
	m, _ := racedModel(t, model.VendorCodex)

	drop(t, m, "")
	if !strings.Contains(m.st.Notice, "/arena drop <seat>") {
		t.Errorf("bare drop does not teach the verb: %q", m.st.Notice)
	}
	drop(t, m, "everything")
	if !strings.Contains(m.st.Notice, "no racer called everything") {
		t.Errorf("unknown-racer refusal: %q", m.st.Notice)
	}

	if _, _, isDrop := parseArenaDrop("drop the cache layer"); isDrop {
		t.Error("a three-word brief opening with 'drop' was stolen from the race")
	}
	if seat, force, isDrop := parseArenaDrop("drop all!"); !isDrop || !force || seat != "all" {
		t.Errorf("drop all! parsed as (%q, %v, %v)", seat, force, isDrop)
	}

	// Mid-turn: worktrees in use, same standing refusal as every mutation.
	occupy(m)
	m.setDraft("/arena drop codex")
	m.arenaDrop("codex", false)
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("mid-turn refusal: %q", m.st.Notice)
	}
}

// TestAdoptGateOutranksTheComposer: while the adopt question is up, y answers
// it through key() — the pending gate routes ahead of both modes, so a y
// cannot land in the draft while the room merges nothing.
func TestAdoptGateOutranksTheComposer(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	m.st.Mode = ModeComposing

	adopt(t, m, "codex", "")
	m.key(key("y"))

	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("y did not reach the adopt gate: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Draft, "y") {
		t.Error("the y that answered the gate also landed in the draft")
	}
	if n, _ := gitOut(ws, "rev-list", "--count", "HEAD..arena/t4/codex"); n != "0" {
		t.Error("the gate's y did not merge")
	}
}

// TestAdoptAndDropReachARaceThatOutranItsTurn: an older room's leftover pushes
// the race number past the turn (arenaRaceNumber), and both end-of-life verbs
// must derive branch and tree from the RECORDED race number — a verb reading a
// turn counter here would merge or delete names this race never created. The
// leftover itself stays untouched throughout: it belongs to a room whose
// receipt is gone, and hand-run git is its only legitimate owner.
func TestAdoptAndDropReachARaceThatOutranItsTurn(t *testing.T) {
	ws := gitRepo(t)
	if _, err := gitOut(ws, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatal(err)
	}
	// The old room's residue at the exact number this room's turn would mint.
	if _, err := gitOut(ws, "branch", "arena/t4/codex"); err != nil {
		t.Fatal(err)
	}
	raceN, base, trees, _, seatErr, err := arenaSetup(context.Background(), ws, 4, []model.VendorID{model.VendorCodex}, nil)
	if err != nil || len(seatErr) != 0 {
		t.Fatalf("setup: %v %v", err, seatErr)
	}
	if raceN != 5 {
		t.Fatalf("fixture: raceN = %d, want 5", raceN)
	}
	m := clearModel()
	m.st.Workspace = ws
	m.lastRace = &arenaRace{workspace: ws, raceN: raceN, base: base, trees: trees}
	scribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "git merge --no-ff arena/t5/codex") {
		t.Fatalf("the gate's question names the wrong branch: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))
	if !strings.Contains(m.st.Notice, "adopted codex") {
		t.Fatalf("adopt failed against the renumbered race: %q", m.st.Notice)
	}
	if n, _ := gitOut(ws, "rev-list", "--count", "HEAD..arena/t5/codex"); n != "0" {
		t.Errorf("the renumbered branch still holds unreachable commits: %q", n)
	}

	drop(t, m, "codex")
	if !strings.Contains(m.st.Notice, "dropped codex") {
		t.Fatalf("drop failed against the renumbered race: %q", m.st.Notice)
	}
	if out, _ := gitOut(ws, "branch", "--list", "arena/t5/codex"); out != "" {
		t.Errorf("the race's own branch survived the drop: %q", out)
	}
	// The leftover was never this room's to merge or delete.
	if out, _ := gitOut(ws, "branch", "--list", "arena/t4/codex"); out == "" {
		t.Error("the verbs deleted an older room's branch")
	}
	if got, _ := gitOut(ws, "rev-parse", "arena/t4/codex"); got != base {
		t.Errorf("the older room's branch moved: %q", got)
	}
}
