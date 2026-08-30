package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The HYBRID adopt (§9.37's 2026-08-29 hybrid amendment): one attempt taken
// whole, plus named paths from another.
//
// Its own file rather than more of lifecycle_test.go, for the reason the golden
// files are one per scenario: this is one feature with one set of refusals, and a
// reader looking for what a hybrid may and may not do should find it in one
// place. The fixtures are lifecycle_test.go's own — racedModel, scribble, adopt,
// roomCommit — because a hybrid is the same lifecycle acting on two racers, and a
// second set of helpers would be a second definition of "a race that ran".
//
// Every test here runs against a real temp repository. The whole feature is git
// operations, so a stub would test the argument parse and not the effect.

// TestHybridAdoptTakesTheWinnerPlusNamedPaths is the happy ending. The base
// attempt arrives as the same --no-ff merge the whole verb makes, the donor's
// named path arrives beside it, and NOTHING ELSE of the donor's does — a hybrid
// that quietly carried the donor's other file would be the card lying about its
// own scope.
func TestHybridAdoptTakesTheWinnerPlusNamedPaths(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")
	scribble(t, m, model.VendorCodex, "unwanted.go", "package unwanted\n")

	adopt(t, m, "claude +codex helper.go", "")
	if m.adoptPending == "" {
		t.Fatalf("the hybrid card did not arm: %q", m.st.Notice)
	}
	// The card states exactly what is merged from where, both halves of it.
	for _, want := range []string{
		"adopt claude + 1 path from codex?",
		"cuts adopt/t4-claude+codex",
		"git merge --no-ff arena/t4/claude",
		"takes helper.go from arena/t4/codex",
		"commits both worktrees",
	} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the card does not say %q: %q", want, m.st.Notice)
		}
	}
	m.adoptGateKey(key("y"))

	if !strings.Contains(m.st.Notice, "adopted claude onto adopt/t4-claude+codex") {
		t.Fatalf("the hybrid did not report success: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "1 path from codex (helper.go)") {
		t.Errorf("the notice hides the mixed provenance: %q", m.st.Notice)
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "adopt/t4-claude+codex" {
		t.Fatalf("the room was left on %q, not on the branch it cut", on)
	}
	for _, f := range []string{"base.go", "helper.go"} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Errorf("%s did not arrive in the room repo", f)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "unwanted.go")); err == nil {
		t.Error("a path nobody named arrived with the hybrid")
	}
	// main did not move, so the hand-off is still one gh pr create.
	if n, _ := gitOut(ws, "rev-list", "--count", "main"); n != "1" {
		t.Errorf("main moved during the hybrid: %s commits", n)
	}
}

// TestTheHybridReceiptNamesBothSources. The commit on the adopt branch is where a
// reader meets this adoption a year later, so it must name both arena branches
// and every path taken. A receipt naming one seat over a tree holding two seats'
// work is the exact dishonesty this verb is shaped around.
func TestTheHybridReceiptNamesBothSources(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")

	adopt(t, m, "claude +codex helper.go", "y")
	if !strings.Contains(m.st.Notice, "adopted claude") {
		t.Fatalf("the hybrid did not land: %q", m.st.Notice)
	}

	msg, err := gitOut(ws, "--no-pager", "log", "-1", "--format=%B")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"arena/t4/claude", "arena/t4/codex", "helper.go", "race t4"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the receipt does not name %q:\n%s", want, msg)
		}
	}
	// The base arrived as a merge, under the receipt — two parents, still visible
	// in history as the adoption of a whole attempt.
	parents, _ := gitOut(ws, "rev-list", "--parents", "-n", "1", "HEAD~1")
	if len(strings.Fields(parents)) != 3 {
		t.Errorf("the base attempt is not a two-parent merge: %q", parents)
	}
}

// TestHybridRefusesAPathBothSeatsWrote is the founding refusal. `git checkout
// <donor> -- <path>` would discard the base attempt's answer with no merge and no
// marker, so council refuses by name and the operator decides. Nothing runs.
func TestHybridRefusesAPathBothSeatsWrote(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "shared.go", "claude's answer\n")
	scribble(t, m, model.VendorCodex, "shared.go", "codex's answer\n")

	adopt(t, m, "claude +codex shared.go", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over a path two seats wrote")
	}
	if !strings.Contains(m.st.Notice, "both wrote shared.go") {
		t.Errorf("the refusal does not name the path: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "by hand") {
		t.Errorf("the refusal does not name a way forward: %q", m.st.Notice)
	}
	if out, _ := gitOut(ws, "branch", "--list", "adopt/*"); out != "" {
		t.Errorf("a refused hybrid cut a branch anyway: %q", out)
	}
}

// TestHybridRefusesAPathTheRoomWrote is the same rule one level out. The merge
// machinery never sees a path taken by checkout, so the room's own work at that
// path would vanish under the donor's copy with nothing to notice it.
func TestHybridRefusesAPathTheRoomWrote(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "a.txt", "codex rewrote it\n")
	roomCommit(t, ws, "a.txt", "the room rewrote it\n")

	adopt(t, m, "claude +codex a.txt", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over a path the room had moved")
	}
	if !strings.Contains(m.st.Notice, "the room and codex both wrote a.txt") {
		t.Errorf("the refusal does not name the room's own write: %q", m.st.Notice)
	}
	if body, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(body) != "the room rewrote it\n" {
		t.Errorf("the room's own content did not survive the refusal: %q", body)
	}
}

// TestHybridRefusesAPathTheDonorDidNotWrite. Taking it would land the base
// attempt's own content and file a receipt saying it came from the donor.
func TestHybridRefusesAPathTheDonorDidNotWrite(t *testing.T) {
	m, _ := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")

	adopt(t, m, "claude +codex a.txt", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over a path the donor never touched")
	}
	if !strings.Contains(m.st.Notice, "codex changed nothing at a.txt") {
		t.Errorf("the refusal does not say what is missing: %q", m.st.Notice)
	}
}

// TestHybridRefusesAPathOutsideTheRepository. A pathspec that climbs out of the
// workspace is a mistake worth naming rather than resolving.
func TestHybridRefusesAPathOutsideTheRepository(t *testing.T) {
	m, _ := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")

	for _, arg := range []string{"claude +codex ../outside.go", "claude +codex /etc/passwd"} {
		adopt(t, m, arg, "")
		if m.adoptPending != "" {
			t.Fatalf("%s armed the gate", arg)
		}
		if !strings.Contains(m.st.Notice, "not a path inside the repository") {
			t.Errorf("%s was not refused as an outside path: %q", arg, m.st.Notice)
		}
	}
}

// TestHybridGrammarTeachesBothForms. The refusals are the only place the second
// form is taught — the help panel's room-commands row is at its width budget — so
// each one has to name the shape that would have worked (§9.17's tell).
func TestHybridGrammarTeachesBothForms(t *testing.T) {
	m, _ := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")

	// Bare /adopt is the discovery surface — it half-asks a question, so it
	// answers with BOTH forms rather than with the older one alone.
	adopt(t, m, "", "")
	if !strings.Contains(m.st.Notice, "+<seat> <path...>") {
		t.Errorf("bare /adopt does not teach the hybrid form: %q", m.st.Notice)
	}
	// A second word that is not a +seat is neither shape.
	adopt(t, m, "claude codex", "")
	if !strings.Contains(m.st.Notice, "/adopt <seat> +<seat> <path...>") {
		t.Errorf("the refusal does not teach the hybrid form: %q", m.st.Notice)
	}
	// A donor with no paths has a better answer than "that is not a command".
	adopt(t, m, "claude +codex", "")
	if !strings.Contains(m.st.Notice, "name the paths to take from codex") {
		t.Errorf("an empty path list was not answered by name: %q", m.st.Notice)
	}
	// A hybrid of a seat with itself is the plain verb, and says so.
	adopt(t, m, "claude +claude base.go", "")
	if !strings.Contains(m.st.Notice, "takes that attempt whole") {
		t.Errorf("a self-hybrid was not sent to the plain verb: %q", m.st.Notice)
	}
	// A donor that never raced has no paths to give.
	adopt(t, m, "claude +grok base.go", "")
	if !strings.Contains(m.st.Notice, "grok has no kept worktree") {
		t.Errorf("an unraced donor was not refused by name: %q", m.st.Notice)
	}
	if m.adoptPending != "" {
		t.Fatal("a refused hybrid left the gate armed")
	}
}

// TestHybridConflictRestoresTheRoom: a hybrid whose BASE merge conflicts ends
// exactly where the conservative whole adopt ends — tree restored, branch gone,
// room back where it started, and the donor's paths never written.
func TestHybridConflictRestoresTheRoom(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "a.txt", "claude's line\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")
	roomCommit(t, ws, "a.txt", "the room's line\n")

	adopt(t, m, "claude +codex helper.go", "y")

	if !strings.Contains(m.st.Notice, "human merge") {
		t.Errorf("a conflicted hybrid did not hand the merge to a human: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "the room is back on main") {
		t.Errorf("the notice does not say where the room now stands: %q", m.st.Notice)
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "main" {
		t.Errorf("the room was left on %q after a failed hybrid", on)
	}
	if out, _ := gitOut(ws, "branch", "--list", "adopt/t4-claude+codex"); out != "" {
		t.Errorf("the failed hybrid left its branch behind: %q", out)
	}
	if out, _ := gitOut(ws, "status", "--porcelain"); out != "" {
		t.Errorf("the room tree was not restored:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(ws, "helper.go")); err == nil {
		t.Error("the donor's path was left in the room after a failed hybrid")
	}
	if body, _ := os.ReadFile(filepath.Join(ws, "a.txt")); string(body) != "the room's line\n" {
		t.Errorf("a.txt was left as %q — the room's own content did not survive", body)
	}
}

// TestHybridRefusesADeletion. A path the donor deleted cannot be checked out, and
// git would say so only after the branch was already cut. The refusal names the
// limit before the card arms — a hybrid takes files a racer wrote, never a
// deletion.
func TestHybridRefusesADeletion(t *testing.T) {
	m, _ := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	if err := os.Remove(filepath.Join(m.lastRace.trees[model.VendorCodex], "a.txt")); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "claude +codex a.txt", "")
	if m.adoptPending != "" {
		t.Fatal("the gate armed over a path the donor deleted")
	}
	if !strings.Contains(m.st.Notice, "never a deletion") {
		t.Errorf("the refusal does not state the limit: %q", m.st.Notice)
	}
}

// TestHybridSuffixesACollider: the collision rule is the whole adopt's, applied
// to the hybrid's own spelling — one scan answers both names, so the two forms
// cannot disagree about what "taken" means.
func TestHybridSuffixesACollider(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")
	if _, err := gitOut(ws, "branch", "adopt/t4-claude+codex"); err != nil {
		t.Fatal(err)
	}

	adopt(t, m, "claude +codex helper.go", "")
	if !strings.Contains(m.st.Notice, "cuts adopt/t4-claude+codex-2") {
		t.Fatalf("the card does not name the free branch: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "adopt/t4-claude+codex-2" {
		t.Errorf("the room was left on %q", on)
	}
}

// TestHybridTakesACommittedPathToo. A racer whose attempt is already committed —
// the state a finished race leaves once commit-per-turn lands, and the state a
// re-armed card sees after a first `y` — is read off the branch rather than off
// the tree. Both halves feed one answer (racerWrites), and the card's commit
// clause drops when there is nothing left to commit.
func TestHybridTakesACommittedPathToo(t *testing.T) {
	m, ws := racedModel(t, model.VendorClaude, model.VendorCodex)
	scribble(t, m, model.VendorClaude, "base.go", "package base\n")
	scribble(t, m, model.VendorCodex, "helper.go", "package helper\n")
	commitAttempt(t, m, model.VendorCodex)

	adopt(t, m, "claude +codex helper.go", "")
	if m.adoptPending == "" {
		t.Fatalf("a committed donor was refused: %q", m.st.Notice)
	}
	if strings.Contains(m.st.Notice, "commits both worktrees") {
		t.Errorf("the card offers to commit a tree that is already clean: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))
	if _, err := os.Stat(filepath.Join(ws, "helper.go")); err != nil {
		t.Error("the committed donor path did not arrive in the room repo")
	}
}
