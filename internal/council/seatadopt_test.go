package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The room as integrator (§9.55): /adopt over a seat's own branch, and the
// record's separate count of those adoptions. Real git throughout, for
// racedModel's reason.

// seatedModel is racedModel's twin: a real repo, real seat worktrees cut by
// seatSetup for the named seats, and the receipt recorded the way a writing
// dispatch records it.
func seatedModel(t *testing.T, seats ...model.VendorID) (*Model, string) {
	t.Helper()
	ws := gitRepo(t)
	if _, err := gitOut(ws, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatal(err)
	}
	res := seatSetup(context.Background(), ws, seats, nil)
	if len(res.refused) != 0 {
		t.Fatalf("seat refusals on a clean repo: %v", res.refused)
	}
	m := clearModel()
	m.st.Workspace = ws
	m.seatTrees = map[model.VendorID]seatTree{}
	for v, st := range res.trees {
		m.seatTrees[v] = st
	}
	return m, ws
}

func seatScribble(t *testing.T, m *Model, v model.VendorID, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.seatTrees[v].tree, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAdoptTakesASeatsBranchAndResetsItsTree is the integrator: the seat's
// uncommitted work arrives in the room as a --no-ff merge of seat/codex onto
// adopt/seat-codex, and the seat's tree is then standing on the new HEAD.
func TestAdoptTakesASeatsBranchAndResetsItsTree(t *testing.T) {
	m, ws := seatedModel(t, model.VendorCodex)
	seatScribble(t, m, model.VendorCodex, "answer.go", "package answer\n")

	adopt(t, m, "codex", "")
	for _, want := range []string{"git merge --no-ff seat/codex", "cuts adopt/seat-codex", "resets seat/codex onto it", "commits its worktree"} {
		if !strings.Contains(m.st.Notice, want) {
			t.Errorf("the card does not say %q: %q", want, m.st.Notice)
		}
	}
	m.adoptGateKey(key("y"))

	if !strings.Contains(m.st.Notice, "adopted codex onto adopt/seat-codex") {
		t.Fatalf("adopt did not report success: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "seat/codex is reset onto it") {
		t.Errorf("the notice does not say the tree followed: %q", m.st.Notice)
	}
	if _, err := os.Stat(filepath.Join(ws, "answer.go")); err != nil {
		t.Error("the seat's file did not arrive in the room repo")
	}
	if on, _ := gitOut(ws, "symbolic-ref", "--short", "HEAD"); on != "adopt/seat-codex" {
		t.Errorf("the room stands on %q, want adopt/seat-codex", on)
	}
	if parents, _ := gitOut(ws, "rev-list", "--parents", "-n", "1", "HEAD"); len(strings.Fields(parents)) != 3 {
		t.Errorf("HEAD is not a two-parent merge: %q", parents)
	}
	head, _ := gitOut(ws, "rev-parse", "HEAD")
	tree := m.seatTrees[model.VendorCodex].tree
	if at, _ := gitOut(tree, "rev-parse", "HEAD"); at != head {
		t.Errorf("the seat tree is at %s, want the integrated HEAD %s", at, head)
	}
	if branch, _ := gitOut(ws, "rev-parse", "seat/codex"); branch != head {
		t.Errorf("seat/codex is at %s, want %s", branch, head)
	}
	if dirty, _ := worktreePorcelain(tree); len(dirty) != 0 {
		t.Errorf("the seat tree is dirty after the reset: %v", dirty)
	}
	// A second /adopt has nothing to take: the tree is the room's line now.
	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "nothing to adopt") {
		t.Errorf("a freshly reset seat was offered for adoption again: %q", m.st.Notice)
	}
}

// TestAdoptPrefersTheRaceOnTheSeatsCurrentTurn: a seat with both a race
// attempt on its current turn and a seat worktree is adopted from the race,
// because the block on screen is what the operator is reading.
func TestAdoptPrefersTheRaceOnTheSeatsCurrentTurn(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	res := seatSetup(context.Background(), ws, []model.VendorID{model.VendorCodex}, nil)
	m.seatTrees = map[model.VendorID]seatTree{model.VendorCodex: res.trees[model.VendorCodex]}
	scribble(t, m, model.VendorCodex, "attempt.go", "package attempt\n")
	seatScribble(t, m, model.VendorCodex, "ordinary.go", "package ordinary\n")

	m.column(model.VendorCodex).Arena = &ArenaResult{Tree: m.lastRace.trees[model.VendorCodex]}
	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "git merge --no-ff arena/t4/codex") {
		t.Errorf("the race was not preferred while its block is on the column: %q", m.st.Notice)
	}
	m.adoptGateKey(key("n"))

	// Once an ordinary turn has cleared the block, the seat branch is what
	// /adopt takes.
	m.column(model.VendorCodex).Arena = nil
	adopt(t, m, "codex", "")
	if !strings.Contains(m.st.Notice, "git merge --no-ff seat/codex") {
		t.Errorf("the seat branch was not offered after the race block cleared: %q", m.st.Notice)
	}
}

// TestAHybridAdoptWorksAcrossSeatBranches: the winner whole plus one path
// from a second seat, on adopt/seat-<base>+<donor>, with the base's tree
// reset and the donor's left alone.
func TestAHybridAdoptWorksAcrossSeatBranches(t *testing.T) {
	m, ws := seatedModel(t, model.VendorClaude, model.VendorCodex)
	seatScribble(t, m, model.VendorClaude, "main.go", "package main\n")
	seatScribble(t, m, model.VendorCodex, "helper.go", "package helper\n")
	seatScribble(t, m, model.VendorCodex, "other.go", "package other\n")

	adopt(t, m, "claude +codex helper.go", "")
	if !strings.Contains(m.st.Notice, "cuts adopt/seat-claude+codex") || !strings.Contains(m.st.Notice, "takes helper.go from seat/codex") {
		t.Fatalf("the hybrid card does not name both sources: %q", m.st.Notice)
	}
	m.adoptGateKey(key("y"))
	if !strings.Contains(m.st.Notice, "adopted claude onto adopt/seat-claude+codex") {
		t.Fatalf("hybrid adopt did not land: %q", m.st.Notice)
	}
	for _, f := range []string{"main.go", "helper.go"} {
		if _, err := os.Stat(filepath.Join(ws, f)); err != nil {
			t.Errorf("%s did not arrive in the room", f)
		}
	}
	if _, err := os.Stat(filepath.Join(ws, "other.go")); err == nil {
		t.Error("a path the operator did not name arrived in the room")
	}
	head, _ := gitOut(ws, "rev-parse", "HEAD")
	if at, _ := gitOut(m.seatTrees[model.VendorClaude].tree, "rev-parse", "HEAD"); at != head {
		t.Error("the base seat's tree did not follow the merge")
	}
	if at, _ := gitOut(m.seatTrees[model.VendorCodex].tree, "rev-parse", "HEAD"); at == head {
		t.Error("the donor's tree was reset, orphaning the work its paths were taken from")
	}
	if subject, _ := gitOut(ws, "log", "-1", "--format=%s"); !strings.Contains(subject, "adopt seat/claude: seat/claude whole") {
		t.Errorf("the receipt commit does not name the seat branches: %q", subject)
	}
}

// TestAdoptRefusesMixingARaceAndASeatBranch: one receipt, one kind of branch.
func TestAdoptRefusesMixingARaceAndASeatBranch(t *testing.T) {
	m, ws := racedModel(t, model.VendorCodex)
	res := seatSetup(context.Background(), ws, []model.VendorID{model.VendorClaude}, nil)
	m.seatTrees = map[model.VendorID]seatTree{model.VendorClaude: res.trees[model.VendorClaude]}
	scribble(t, m, model.VendorCodex, "attempt.go", "package attempt\n")
	seatScribble(t, m, model.VendorClaude, "helper.go", "package helper\n")

	adopt(t, m, "codex +claude helper.go", "")
	if m.adoptPending != "" || !strings.Contains(m.st.Notice, "one kind") {
		t.Errorf("a race-plus-seat hybrid was not refused by name: %q", m.st.Notice)
	}
}

// TestAdoptOfASeatWithNothingNewRefuses: a clean tree at the room's HEAD is a
// measured zero.
func TestAdoptOfASeatWithNothingNewRefuses(t *testing.T) {
	m, _ := seatedModel(t, model.VendorCodex)
	adopt(t, m, "codex", "")
	if m.adoptPending != "" || !strings.Contains(m.st.Notice, "nothing to adopt") {
		t.Errorf("an empty seat branch armed a card: %q", m.st.Notice)
	}
}

// TestAdoptRefusesADirtyRoomForASeatBranch: the clean-tree gate is the same
// gate whichever branch is merged.
func TestAdoptRefusesADirtyRoomForASeatBranch(t *testing.T) {
	m, ws := seatedModel(t, model.VendorCodex)
	seatScribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	if err := os.WriteFile(filepath.Join(ws, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	adopt(t, m, "codex", "")
	if m.adoptPending != "" || !strings.Contains(m.st.Notice, "uncommitted") {
		t.Errorf("a dirty room was not refused: %q", m.st.Notice)
	}
}

// TestBareAdoptNamesTheSeatsWithABranch: the half-asked question, answered.
func TestBareAdoptNamesTheSeatsWithABranch(t *testing.T) {
	m, _ := seatedModel(t, model.VendorCodex)
	m.setDraft("/adopt")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "codex has a seat worktree") {
		t.Errorf("bare /adopt does not name the seat with a branch: %q", m.st.Notice)
	}
}

// TestTheRecordCountsSeatAdoptsApartFromRaces is the tally, pure: seat refs
// raise their own count and touch no race figure.
func TestTheRecordCountsSeatAdoptsApartFromRaces(t *testing.T) {
	rec := tallyArenaRefs("telltale",
		[]string{"arena/t2/claude", "arena/t2/codex"},
		[]string{"adopt/t2-claude", "adopt/seat-codex", "adopt/seat-codex-2", "adopt/seat-claude+codex", "adopt/seat-nobody"})
	if len(rec.Races) != 1 || rec.Decided != 1 {
		t.Errorf("seat adopts moved the race figures: races=%v decided=%d", rec.Races, rec.Decided)
	}
	for _, s := range rec.Seats {
		if s.Vendor == model.VendorCodex && (s.Adopted != 0 || s.Judged != 1) {
			t.Errorf("codex's race standing moved: %+v", s)
		}
	}
	want := map[model.VendorID][2]int{model.VendorClaude: {0, 1}, model.VendorCodex: {2, 1}}
	if len(rec.SeatAdopts) != 2 {
		t.Fatalf("seat adopts = %+v, want claude and codex", rec.SeatAdopts)
	}
	for _, s := range rec.SeatAdopts {
		if w, ok := want[s.Vendor]; !ok || s.Whole != w[0] || s.Hybrid != w[1] {
			t.Errorf("%s: whole %d hybrid %d, want %v", s.Vendor, s.Whole, s.Hybrid, w)
		}
	}
	line := recordSeatAdopts(rec)
	if !strings.Contains(line, "Codex 2") || !strings.Contains(line, "Claude Code 0 (part of 1 hybrid adopt)") {
		t.Errorf("the sentence does not carry the counts: %q", line)
	}
	if !strings.Contains(line, "not a verdict") {
		t.Errorf("the sentence does not say why it sits outside the rate: %q", line)
	}
	if strings.Contains(recordSeatAdopts(recordFixture()), "Seat adopts") {
		t.Error("a record with no seat adopt drew the line anyway")
	}
}

// TestTheRecordReadsSeatAdoptsFromRealRefs: an adoption through the verb
// leaves exactly the ref the record reads, and the page and the yank say it.
func TestTheRecordReadsSeatAdoptsFromRealRefs(t *testing.T) {
	m, ws := seatedModel(t, model.VendorCodex)
	seatScribble(t, m, model.VendorCodex, "answer.go", "package answer\n")
	adopt(t, m, "codex", "y")
	rec := readArenaRecord(ws)
	if len(rec.SeatAdopts) != 1 || rec.SeatAdopts[0].Vendor != model.VendorCodex || rec.SeatAdopts[0].Whole != 1 {
		t.Fatalf("the record did not read the seat adopt: %+v (err %q)", rec.SeatAdopts, rec.Err)
	}
	if rec.Raced() {
		t.Error("a seat adopt was read as a race")
	}
	m.st.Record = &rec
	if y := m.st.YankRecord(); !strings.Contains(y.Text, "Seat adopts") {
		t.Errorf("the yank drops the seat adopts: %q", y.Text)
	}
}

// TestTheSeatAdoptRecordPage pins the sentence on the page beside the race
// rows it is kept apart from.
func TestTheSeatAdoptRecordPage(t *testing.T) {
	st := room()
	rec := tallyArenaRefs("telltale",
		[]string{"arena/t2/claude", "arena/t2/codex", "arena/t2/agy", "arena/t4/claude", "arena/t4/codex"},
		[]string{"adopt/t2-claude", "adopt/seat-codex", "adopt/seat-codex-2", "adopt/seat-claude"})
	st.Record = &rec
	golden(t, "arena-record-seats", render(st))
	st.ASCII = true
	golden(t, "arena-record-seats-ascii", Render(st, PlainStyles(), GlyphsFor(true)))
}
