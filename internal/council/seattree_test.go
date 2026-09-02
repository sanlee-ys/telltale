package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// A writing seat's own worktree (seattree.go, design.md §9.55). Every test
// here dispatches through countSpawns, so no vendor starts; the git the tests
// run is real, against a temp repository, because a worktree is a git fact and
// a stub would test the map rather than the tree.

// seatRoom is crewRoom pointed at a real repository: four installed seats, a
// writing room, and a workspace git can cut worktrees beside.
func seatRoom(t *testing.T, write bool) *Model {
	t.Helper()
	m := flowRoom(t, write)
	m.st.Workspace = gitRepo(t)
	m.st.Width, m.st.Height = 120, 24
	m.st.Mode = ModeComposing
	return m
}

// pumpSeatSetup drives a seat setup to its landing, pumpArenaSetup's shape:
// one message at a time, each applied as Update would apply it.
func pumpSeatSetup(t *testing.T, m *Model, cmd tea.Cmd) tea.Cmd {
	t.Helper()
	for m.seatPrep != nil {
		if cmd == nil {
			t.Fatal("the setup stopped handing back a command with a prep still running")
		}
		raw := cmd()
		msg, ok := raw.(seatSetupMsg)
		if !ok {
			t.Fatalf("the setup produced %T, not a seat setup message", raw)
		}
		cmd = m.applySeatSetup(msg)
	}
	return cmd
}

// sendNow types a brief, presses enter, and drives whatever setup stood in
// front of it, so the seat has spawned when it returns.
func sendNow(t *testing.T, m *Model, brief string) {
	t.Helper()
	m.st.Mode = ModeComposing
	m.setDraft(brief)
	_, cmd := m.key(key("enter"))
	pumpSeatSetup(t, m, cmd)
}

func land(m *Model, v model.VendorID) {
	m.column(v).Body = "done"
	m.applyEvents([]runner.Event{{Vendor: v, Kind: runner.KindDone}})
}

// TestAWritingSeatGetsItsOwnWorktreeOnceAndReusesIt is the product change: the
// first writing brief to codex cuts <repo>-seat-codex on seat/codex from the
// room's HEAD, the process runs THERE, the badge says so — and the second
// brief spawns straight into the same tree with no setup at all.
func TestAWritingSeatGetsItsOwnWorktreeOnceAndReusesIt(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, true)
	ws := m.st.Workspace
	tree := seatTreePath(ws, model.VendorCodex)

	m.setDraft("@codex refactor the poller")
	_, cmd := m.key(key("enter"))
	if m.seatPrep == nil || cmd == nil {
		t.Fatalf("the first writing brief did not start a worktree setup (prep=%v cmd=%v)", m.seatPrep != nil, cmd != nil)
	}
	if log.n() != 0 {
		t.Fatal("a seat spawned before its worktree existed")
	}
	if m.st.TreeSetup == "" || !strings.Contains(render(m.st), "worktree:") {
		t.Errorf("the room is cutting a worktree and the frame does not say so: %q", m.st.TreeSetup)
	}
	pumpSeatSetup(t, m, cmd)

	if log.n() != 1 || m.turnOf(model.VendorCodex) == nil {
		t.Fatalf("the brief did not spawn after the setup: %d spawns, %q", log.n(), m.st.Notice)
	}
	if got := log.specs[0].Dir; !sameDir(got, tree) {
		t.Errorf("codex runs in %s, want its own worktree %s", got, tree)
	}
	if on, err := gitOut(tree, "symbolic-ref", "--short", "HEAD"); err != nil || on != "seat/codex" {
		t.Errorf("the worktree is on %q (%v), want seat/codex", on, err)
	}
	head, _ := gitOut(ws, "rev-parse", "HEAD")
	if at, _ := gitOut(tree, "rev-parse", "HEAD"); at != head {
		t.Errorf("the seat tree starts at %s, want the room's HEAD %s", at, head)
	}
	if b := m.column(model.VendorCodex).Containment.Badge(false); b != "wt: seat/codex" {
		t.Errorf("badge = %q, want wt: seat/codex", b)
	}
	if m.st.TreeSetup != "" {
		t.Errorf("the step line outlived the setup: %q", m.st.TreeSetup)
	}

	// The second brief: no setup, same tree.
	land(m, model.VendorCodex)
	m.setDraft("@codex and add tests")
	_, cmd = m.key(key("enter"))
	if m.seatPrep != nil {
		t.Fatal("the second brief cut a second worktree instead of reusing the first")
	}
	if log.n() != 2 || !sameDir(log.specs[1].Dir, tree) {
		t.Fatalf("the second brief did not spawn into the same tree: %d spawns, dir %s", log.n(), log.specs[1].Dir)
	}
	if list, _ := gitOut(ws, "worktree", "list"); strings.Count(list, "seat-codex") != 1 {
		t.Errorf("worktree list does not hold exactly one codex tree:\n%s", list)
	}
	_ = cmd
}

// TestReadPostureKeepsTheSharedTree: nothing writes, so nothing is isolated,
// and the badge says shared tree with no reason — it was chosen.
func TestReadPostureKeepsTheSharedTree(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, false)
	sendNow(t, m, "@codex review the poller")
	if log.n() != 1 || !sameDir(log.specs[0].Dir, m.st.Workspace) {
		t.Fatalf("a read brief left the workspace: %d spawns, dir %s", log.n(), log.specs[0].Dir)
	}
	if _, err := os.Stat(seatTreePath(m.st.Workspace, model.VendorCodex)); err == nil {
		t.Error("a read brief cut a worktree")
	}
	if b := m.column(model.VendorCodex).Containment.Badge(false); b != "shared tree" {
		t.Errorf("badge = %q, want shared tree", b)
	}
}

// TestANonGitWorkspaceFallsBackToTheSharedTreeAndSaysSo: the fallback is
// synchronous, the process runs in the workspace, and the badge names why.
func TestANonGitWorkspaceFallsBackToTheSharedTreeAndSaysSo(t *testing.T) {
	log := countSpawns(t)
	m := crewRoom(t)
	m.setDraft("@codex refactor the poller")
	_, cmd := m.key(key("enter"))
	if m.seatPrep != nil {
		t.Fatal("a workspace outside git started a worktree setup")
	}
	if cmd == nil || log.n() != 1 || !sameDir(log.specs[0].Dir, m.st.Workspace) {
		t.Fatalf("the brief did not spawn into the shared tree: %d spawns", log.n())
	}
	c := m.column(model.VendorCodex)
	if b := c.Containment.Badge(false); b != "shared tree · not a git repo" {
		t.Errorf("badge = %q, want shared tree · not a git repo", b)
	}
	if b := c.Containment.Badge(true); b != "shared tree - not a git repo" {
		t.Errorf("ascii badge = %q", b)
	}
	// At column width the reason sheds and the mark stays; expanded, the
	// whole badge is on the frame.
	if !strings.Contains(render(m.st), "⚠ shared tree") {
		t.Error("the fallback badge is not on the frame")
	}
	m.st.Expanded = true
	m.st.Focus = 1 // codex
	if !strings.Contains(render(m.st), "⚠ shared tree · not a git repo") {
		t.Error("the expanded column does not carry the reason")
	}
}

// TestSharedTreeFlagKeepsEverySeatInTheWorkspace: the opt-out is the older
// room, and the badge says shared tree as a choice rather than a fallback.
func TestSharedTreeFlagKeepsEverySeatInTheWorkspace(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, true)
	m.opts.SharedTree = true
	sendNow(t, m, "@codex refactor the poller")
	if log.n() != 1 || !sameDir(log.specs[0].Dir, m.st.Workspace) {
		t.Fatalf("--shared-tree did not keep the seat in the workspace: dir %s", log.specs[0].Dir)
	}
	if _, err := os.Stat(seatTreePath(m.st.Workspace, model.VendorCodex)); err == nil {
		t.Error("--shared-tree cut a worktree anyway")
	}
	if b := m.column(model.VendorCodex).Containment.Badge(false); b != "shared tree" {
		t.Errorf("badge = %q, want shared tree", b)
	}
}

// TestCtrlCStopsASeatSetupAndReturnsTheBrief: the key ends the git command,
// nothing spawns, and the draft is back where it was typed.
func TestCtrlCStopsASeatSetupAndReturnsTheBrief(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, true)
	m.setDraft("@codex refactor the poller")
	_, cmd := m.key(key("enter"))
	if m.seatPrep == nil {
		t.Fatal("no setup to stop")
	}
	m.key(key("ctrl+c"))
	if m.seatPrep != nil || m.st.TreeSetup != "" {
		t.Error("ctrl+c left the setup standing")
	}
	if m.st.Draft != "@codex refactor the poller" || m.st.Mode != ModeComposing {
		t.Errorf("the brief did not come back: %q mode %v", m.st.Draft, m.st.Mode)
	}
	if !strings.Contains(m.st.Notice, "stopped") || !strings.Contains(m.st.Notice, "kept") {
		t.Errorf("the notice does not say what happened: %q", m.st.Notice)
	}
	// The abandoned goroutine drains and is dropped by id; nothing spawns.
	for i := 0; i < 8 && cmd != nil; i++ {
		raw := cmd()
		if raw == nil {
			break
		}
		cmd = m.applySeatSetup(raw.(seatSetupMsg))
	}
	if log.n() != 0 || m.anyInFlight() {
		t.Errorf("a stopped setup still dispatched: %d spawns", log.n())
	}
}

// TestAWorktreeRefusalIsStatedNotSilent: a directory already at the tree's
// name that is not the seat's worktree is refused, the seat runs in the shared
// tree, the notice carries the reason, and the badge carries the short form.
func TestAWorktreeRefusalIsStatedNotSilent(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, true)
	squat := seatTreePath(m.st.Workspace, model.VendorCodex)
	if err := os.Mkdir(squat, 0o755); err != nil {
		t.Fatal(err)
	}
	sendNow(t, m, "@codex refactor the poller")
	if log.n() != 1 || !sameDir(log.specs[0].Dir, m.st.Workspace) {
		t.Fatalf("the refused seat did not fall back to the workspace: dir %s", log.specs[0].Dir)
	}
	if !strings.Contains(m.st.Notice, "codex: "+squat+" exists") || !strings.Contains(m.st.Notice, "shared tree") {
		t.Errorf("the notice does not state the fallback and its reason: %q", m.st.Notice)
	}
	if b := m.column(model.VendorCodex).Containment.Badge(false); b != "shared tree · worktree refused" {
		t.Errorf("badge = %q", b)
	}
	// Remembered: the next brief pays no second setup for the same refusal.
	land(m, model.VendorCodex)
	m.setDraft("@codex again")
	m.key(key("enter"))
	if m.seatPrep != nil {
		t.Error("a refused seat was set up again on the next brief")
	}
}

// TestASeatTreeIsReusedByALaterRoom: a second model over the same repository
// finds the tree on disk and says it is reusing it, cutting nothing new.
func TestASeatTreeIsReusedByALaterRoom(t *testing.T) {
	countSpawns(t)
	m := seatRoom(t, true)
	sendNow(t, m, "@codex refactor the poller")
	tree := m.seatTrees[model.VendorCodex].tree

	later := flowRoom(t, true)
	later.st.Workspace = m.st.Workspace
	sendNow(t, later, "@codex carry on")
	if got := later.seatTrees[model.VendorCodex].tree; !sameDir(got, tree) {
		t.Fatalf("the later room did not find the tree: %q", got)
	}
	if !strings.Contains(later.st.Notice, "reusing "+tree) {
		t.Errorf("the reuse is not stated: %q", later.st.Notice)
	}
}

// TestAFlowReadHopInAWriteRoomStaysInTheSharedTree: posture belongs to the
// hop (§9.16), a read hop writes nothing, and a worktree for it would be
// containment for a hazard that does not exist.
func TestAFlowReadHopInAWriteRoomStaysInTheSharedTree(t *testing.T) {
	log := countSpawns(t)
	m := seatRoom(t, true)
	sendNow(t, m, "/flow @codex review security -> @claude summarize")
	if log.n() != 1 || !sameDir(log.specs[0].Dir, m.st.Workspace) {
		t.Fatalf("a read hop left the workspace: dir %s", log.specs[0].Dir)
	}
	if _, err := os.Stat(seatTreePath(m.st.Workspace, model.VendorCodex)); err == nil {
		t.Error("a read hop cut a worktree")
	}
}

// TestThePersistentSeatRespawnsIntoItsTree: the stream-json seat's cwd is
// argv, so a process pinned to the workspace is replaced by one in the seat's
// tree, on the same --resume composition /cd uses, and the note says which
// move it was.
func TestThePersistentSeatRespawnsIntoItsTree(t *testing.T) {
	countSpawns(t)
	m := seatRoom(t, true)
	// No saved thread: a resumed process says nothing about its move (the
	// reattach card already covers it), and this test is about the note.
	old := &fakeSession{alive: true}
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), sess: old, sent: 2, dir: m.st.Workspace}
	m.seatTrees = map[model.VendorID]seatTree{}
	m.seatTrees[model.VendorClaude] = seatTree{workspace: m.st.Workspace,
		tree: seatTreePath(m.st.Workspace, model.VendorClaude), branch: "seat/claude"}

	c := m.column(model.VendorClaude)
	p, note, err := m.seatProcess(&recordingSeat{}, c)
	if err != nil {
		t.Fatal(err)
	}
	if !old.killed {
		t.Error("the workspace-pinned process survived the move into the tree")
	}
	if !sameDir(p.dir, seatTreePath(m.st.Workspace, model.VendorClaude)) {
		t.Errorf("the new process runs in %s, want the seat tree", p.dir)
	}
	if !strings.Contains(note, "own worktree") || !strings.Contains(note, "seat/claude") {
		t.Errorf("the note does not say which move this was: %q", note)
	}
}

// TestTheContainmentBadges pins every badge state on one frame, and its ascii
// twin: the words carry the distinction, the separator is the only glyph.
func TestTheContainmentBadges(t *testing.T) {
	st := room()
	st.Columns[0].Containment = ContainClaim{Level: ContainSeatTree, Branch: "seat/claude"}
	st.Columns[1].Containment = ContainClaim{Level: ContainShared}
	st.Columns[2].Containment = ContainClaim{Level: ContainShared, Why: "not a git repo"}
	for i := range st.Columns {
		st.Columns[i].Sandbox = SandboxClaim{Level: SandboxWrite}
	}
	st.Write = true
	frame := render(st)
	for _, want := range []string{"wt: seat/claude", "  shared tree  ", "⚠ shared tree"} {
		if !strings.Contains(frame, want) {
			t.Errorf("the frame lacks %q", want)
		}
	}
	if strings.Contains(frame, "not a git r") {
		t.Error("the reason was clipped rather than shed")
	}
	golden(t, "containment-badges", frame)

	st.ASCII = true
	ascii := Render(st, PlainStyles(), GlyphsFor(true))
	if !strings.Contains(ascii, "! shared tree") {
		t.Error("the ascii frame lost the fallback's mark")
	}
	golden(t, "containment-badges-ascii", ascii)

	// Expanded, the column has the width and the reason is on the row.
	st.ASCII = false
	st.Expanded = true
	st.Focus = 2
	wide := render(st)
	if !strings.Contains(wide, "⚠ shared tree · not a git repo") {
		t.Error("the expanded column does not carry the reason")
	}
	golden(t, "containment-badges-expanded", wide)
}

// TestRenderStaysPureOverAContainmentBadge: the badge is a stamped claim, so a
// State with one renders identically twice and reads no directory.
func TestRenderStaysPureOverAContainmentBadge(t *testing.T) {
	st := room()
	st.Columns[0].Containment = ContainClaim{Level: ContainSeatTree, Branch: "seat/claude"}
	if render(st) != render(st) {
		t.Error("two renders of one State differ")
	}
	if _, err := os.Stat(filepath.Join(st.Workspace, "nothing")); err == nil {
		t.Error("the fixture workspace exists; the purity claim is untested")
	}
}
