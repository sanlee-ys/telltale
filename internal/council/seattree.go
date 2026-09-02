package council

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// A writing seat gets its own worktree, and the room is the integrator
// (design.md §9.55).
//
// §9.54 made the seats concurrent and left them in ONE tree. Ordinary turns
// dispatched every seat against `State.Workspace`; a worktree existed only
// inside /arena. Two writing seats on two turns at once therefore wrote into
// the same checkout, which §9.37 had already ruled out for a race in one
// sentence: "four writers in one shared tree are not four answers, they are one
// trampled tree". The race's answer — one worktree per attempt — is the crew's
// answer too, with one difference that decides everything below: a racer's
// tree is thrown away with the race, and a seat's tree is the seat's own line
// in the repository, cut once and reused for every writing turn after it.
//
// Three rulings, each a refusal as much as a mechanic:
//
//   - THE CONTAINMENT IS STRUCTURAL, NOT ASKED FOR. Only the stream-json seat
//     can be asked before a write (canGate); the other four act unasked. So a
//     crew's containment cannot be a card, and it is not a flag either: in
//     write posture every seat is given its own tree BY DEFAULT, and
//     `--shared-tree` is the opt-out for an operator who wants the older room.
//     Read posture keeps the shared tree, because nothing in it writes.
//   - THE BADGE SAYS WHICH HOLDS. `wt: seat/codex` on a column whose process
//     runs in its own worktree; `shared tree` on one that runs in the
//     workspace; and a fallback the room could not avoid says why —
//     `shared tree · not a git repo`. A worktree that could not be created is
//     a stated fallback and never a silent one (§4a.1's rule on a directory).
//   - THE ROOM IS THE INTEGRATOR. Nothing a seat writes reaches the room's
//     tree on its own. `/adopt <seat>` merges the seat's branch onto a fresh
//     adopt branch with the same `--no-ff` receipt a race adoption gets, and
//     then resets the seat's tree onto the new HEAD so its next brief starts
//     from the integrated line (lifecycle.go). `/hand` moves a seat's work into
//     another seat's brief as fenced data (handcmd.go).
//
// The mechanics are arenasetup.go's, on purpose: the tree is cut OFF the render
// loop under the same deadline, the frame names the step and never the
// progress, git's own sentence is what a refusal quotes, and ctrl+c stops the
// setup rather than the room.

// ContainLevel is what actually contains one seat's process: the directory it
// runs in. Three states, kept apart for §4a.1's reason — a seat that has never
// been dispatched makes no claim, and a badge that read `shared tree` on it
// would be a claim about a process that does not exist.
type ContainLevel uint8

const (
	// ContainNone: no claim. The zero value, and every column before its
	// first dispatch. Renders nothing.
	ContainNone ContainLevel = iota
	// ContainShared: the seat's process runs in State.Workspace, the tree
	// every other shared-tree seat and the operator's own editor share.
	ContainShared
	// ContainSeatTree: the seat's process runs in its own worktree, on its own
	// branch. Branch names it.
	ContainSeatTree
)

// ContainClaim is one column's containment, as the badge states it.
//
// Why is the reason a write-posture seat is in the shared tree when the room
// would have given it a worktree: `not a git repo`, `worktree refused`, or
// empty when the shared tree was chosen (read posture, --shared-tree). It is
// short because the badge row is; the whole sentence — git's own words — went
// to the notice when the fallback happened.
type ContainClaim struct {
	Level  ContainLevel
	Branch string
	Why    string
}

// Badge is the containment word rendered on the badge row. ascii picks the
// separator between the word and its reason, so the distinction the reason
// carries survives --ascii on the words alone.
func (c ContainClaim) Badge(ascii bool) string {
	switch c.Level {
	case ContainSeatTree:
		return "wt: " + c.Branch
	case ContainShared:
		if c.Why == "" {
			return "shared tree"
		}
		sep := " · "
		if ascii {
			sep = " - "
		}
		return "shared tree" + sep + c.Why
	default:
		return ""
	}
}

// seatTree is one seat's own worktree, as the room recorded it: the repo it
// was cut beside (so a /cd elsewhere does not aim a seat at a tree of another
// repository), the tree and the branch.
//
// In memory only, like arenaRace: room.json stays keys-and-numbers (ADR-008),
// and a tree on disk is rediscovered by name at the next writing dispatch
// (seatSetup's reuse branch), so nothing needs saving to find it again.
type seatTree struct {
	workspace string
	tree      string
	branch    string
}

// seatTreePath names one seat's worktree: a SIBLING of the workspace,
// `<repo>-seat-<vendor>`, for arenaTree's reasons — kept trees must be visible,
// and /cd resolves siblings by name.
func seatTreePath(workspace string, v model.VendorID) string {
	return filepath.Join(filepath.Dir(workspace), filepath.Base(workspace)+"-seat-"+string(v))
}

// seatBranch names one seat's branch: `seat/<vendor>`. One branch per seat per
// repository, reused across turns and rooms — it is the seat's own line, and
// /adopt is what folds it back.
func seatBranch(v model.VendorID) string { return "seat/" + string(v) }

// isolating reports whether THIS dispatch gives a writing seat its own tree:
// the posture the seats are about to be spawned with is a writing one, and the
// operator did not opt out.
//
// Read off posture() rather than off State.Write, because a /flow read hop in a
// write room runs at read posture (§9.16) and a read hop writes nothing — the
// shared tree is the honest place for it, and a worktree cut for a hop that
// cannot write would be containment for a hazard that does not exist.
func (m *Model) isolating() bool {
	return m.posture() != vendors.PostureRead && !m.opts.SharedTree
}

// seatDir is the directory this seat's process runs in for the dispatch being
// built: its own worktree when the room is isolating and the tree exists for
// THIS workspace, the shared workspace otherwise.
//
// The one read every spawn path goes through — specFor, seatProcess, the flow
// receipt, /hand — so the badge, the argv and the receipt cannot disagree
// about where a seat is.
func (m *Model) seatDir(v model.VendorID) string {
	if m.isolating() {
		if st, ok := m.seatTrees[v]; ok && sameDir(st.workspace, m.st.Workspace) {
			return st.tree
		}
	}
	return m.st.Workspace
}

// containmentFor is the badge a seat dispatched NOW would wear, read off the
// same facts seatDir reads plus the recorded fallback.
func (m *Model) containmentFor(v model.VendorID) ContainClaim {
	if !m.isolating() {
		return ContainClaim{Level: ContainShared}
	}
	if st, ok := m.seatTrees[v]; ok && sameDir(st.workspace, m.st.Workspace) {
		return ContainClaim{Level: ContainSeatTree, Branch: st.branch}
	}
	if r, ok := m.seatRefused[v]; ok && sameDir(r.workspace, m.st.Workspace) {
		return ContainClaim{Level: ContainShared, Why: r.badge}
	}
	if !inGitRepo(m.st.Workspace) {
		return ContainClaim{Level: ContainShared, Why: "not a git repo"}
	}
	// Reachable only between deciding a seat needs a tree and the setup
	// landing; sendTurn never runs in that window.
	return ContainClaim{Level: ContainShared, Why: "no worktree yet"}
}

// seatRefusal records that a seat's worktree could not be cut for THIS
// workspace, so the next dispatch does not pay the same git call to hear the
// same refusal. badge is the short form on the column; why is git's sentence,
// said once in the notice when it happened. Forgotten with the workspace: a
// /cd elsewhere gets a fresh attempt.
type seatRefusal struct {
	workspace string
	badge     string
	why       string
}

// inGitRepo reports whether a directory is inside a git repository, by the
// one fact that decides it: a `.git` entry (a directory, or a worktree's
// pointer file) on the path up to the root.
//
// A stat walk rather than `git rev-parse`, and the difference is the render
// loop: this is asked on EVERY dispatch, synchronously, to decide whether a
// setup is worth starting, and a subprocess per keystroke is the freeze
// arenasetup.go exists to prevent. The answer it gives is the only one the
// badge needs — `not a git repo` is a fact about the directory, and the badge
// says exactly that.
func inGitRepo(dir string) bool {
	dir = filepath.Clean(dir)
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}

// seatsNeedingTrees is every seat this dispatch would isolate that has no tree
// yet and no recorded refusal — the seats a setup has to run for before
// anything spawns. Empty is the common case (read posture, --shared-tree, a
// workspace outside git, or every seat already cut), and empty means dispatch
// spawns synchronously exactly as it did before §9.55.
func (m *Model) seatsNeedingTrees(route Route) []model.VendorID {
	if !m.isolating() || !inGitRepo(m.st.Workspace) {
		return nil
	}
	var need []model.VendorID
	for _, c := range m.st.Columns {
		if !m.st.seats(c) || !route.addresses(c.Vendor) || m.turnOf(c.Vendor) != nil {
			continue
		}
		if st, ok := m.seatTrees[c.Vendor]; ok && sameDir(st.workspace, m.st.Workspace) {
			continue
		}
		if r, ok := m.seatRefused[c.Vendor]; ok && sameDir(r.workspace, m.st.Workspace) {
			continue
		}
		need = append(need, c.Vendor)
	}
	return need
}

// The step vocabulary, arenasetup.go's rule: words for what is happening,
// never a count of how far along it is.
const seatStepBase = "reading the room's HEAD"

func seatStepTree(v model.VendorID) string { return "preparing worktree for " + string(v) }

// seatSetupResult is one finished seat setup. trees holds every seat that now
// has a worktree; refused holds every seat that does not, with git's sentence;
// notes are facts worth one line in the notice (a tree reused from an earlier
// room). cancelled is the operator's ctrl+c, the one ending that dispatches
// nothing.
type seatSetupResult struct {
	workspace string
	trees     map[model.VendorID]seatTree
	refused   map[model.VendorID]seatRefusal
	notes     []string
	cancelled bool
}

// seatPrep is the setup standing in front of a dispatch, arenaPrep's shape for
// arenaPrep's reasons: the dispatch cannot be re-derived when the setup lands
// (the composer is empty by then), so the whole of it rides here — as the
// launch that will spawn it, because a flow stage and an ordinary brief spawn
// differently and this file has no business knowing how.
type seatPrep struct {
	id     int
	ch     chan seatSetupMsg
	cancel context.CancelFunc
	// launch spawns the dispatch once the trees exist. It runs on the update
	// loop, in applySeatSetup, never on the setup's goroutine.
	launch func() tea.Cmd
	// brief is the draft exactly as typed, returned to the composer on ctrl+c.
	brief string
	// flow marks a /flow stage's setup: a cancel ends the chain as well.
	flow bool
}

// seatSetupMsg is one step beginning, or the whole setup landing — one
// channel, in order, arenaSetupMsg's shape.
type seatSetupMsg struct {
	prep int
	step string
	done *seatSetupResult
}

// beginSeatSetup cuts the named seats' worktrees off the loop and returns the
// command that reads the first step. The draft is cleared here, for
// beginArenaSetup's reason: the brief was sent the moment enter was pressed.
func (m *Model) beginSeatSetup(need []model.VendorID, launch func() tea.Cmd) tea.Cmd {
	ctx, cancel := context.WithTimeout(context.Background(), arenaSetupDeadline)
	m.seatPrepN++
	p := &seatPrep{
		id:     m.seatPrepN,
		ch:     make(chan seatSetupMsg, len(need)+4),
		cancel: cancel,
		launch: launch,
		brief:  m.st.Draft,
		flow:   m.flowChain != nil,
	}
	m.seatPrep = p
	m.setDraft("")
	m.st.Mode = ModeViewing
	m.st.Notice = ""
	m.st.TreeSetup = seatStepBase
	id, ch, ws := p.id, p.ch, m.st.Workspace
	seats := append([]model.VendorID(nil), need...)
	go func() {
		defer close(ch)
		res := seatSetup(ctx, ws, seats, func(s string) { ch <- seatSetupMsg{prep: id, step: s} })
		ch <- seatSetupMsg{prep: id, done: &res}
	}()
	return m.waitSeatSetup()
}

// waitSeatSetup reads one message from the running setup, one at a time, for
// waitArenaSetup's reason: each step is a frame the operator is meant to see.
func (m *Model) waitSeatSetup() tea.Cmd {
	if m.seatPrep == nil {
		return nil
	}
	ch := m.seatPrep.ch
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

// applySeatSetup lands one step or the finished setup. A finished setup
// records what it cut and what it could not, says the fallbacks out loud, and
// then launches the dispatch it was standing in front of — every seat spawns,
// each in the directory the record now names for it.
func (m *Model) applySeatSetup(msg seatSetupMsg) tea.Cmd {
	p := m.seatPrep
	if p == nil || p.id != msg.prep {
		return nil
	}
	if msg.done == nil {
		m.st.TreeSetup = msg.step
		return m.waitSeatSetup()
	}
	res := msg.done
	m.seatPrep, m.st.TreeSetup = nil, ""
	p.cancel()
	if res.cancelled {
		// Unreachable through stopSeatSetup, which drops the prep before the
		// goroutine can answer; kept so a cancelled result arriving on any
		// other path still hands the room back rather than launching a
		// dispatch the operator just stopped.
		m.setDraft(p.brief)
		m.st.Mode = ModeComposing
		return nil
	}
	// A Model a test typed out as a literal has no maps; the constructor is
	// the only other place they are made (holdTurn's own tolerance).
	if m.seatTrees == nil {
		m.seatTrees = map[model.VendorID]seatTree{}
	}
	if m.seatRefused == nil {
		m.seatRefused = map[model.VendorID]seatRefusal{}
	}
	for v, st := range res.trees {
		m.seatTrees[v] = st
	}
	var said []string
	for _, c := range m.st.Columns {
		if r, ok := res.refused[c.Vendor]; ok {
			m.seatRefused[c.Vendor] = r
			said = append(said, string(c.Vendor)+": "+r.why+" — working in the shared tree")
		}
	}
	said = append(said, res.notes...)
	cmd := p.launch()
	// The fallback outranks whatever the launch said, and joins it rather than
	// replacing it: a seat writing into the shared tree is the fact this file
	// exists to state, and a partial-send notice beside it is still true.
	if len(said) > 0 {
		m.st.Notice = joinNotice(strings.Join(said, " · "), m.st.Notice)
	}
	return cmd
}

// stopSeatSetup is ctrl+c during a seat setup: the context ends, nothing is
// sent, the brief comes back, and any tree already cut is KEPT — it is the
// seat's own line and the next writing brief reuses it, so a cancel costs
// nothing but the wait.
func (m *Model) stopSeatSetup() {
	p := m.seatPrep
	if p == nil {
		return
	}
	m.seatPrep, m.st.TreeSetup = nil, ""
	p.cancel()
	m.setDraft(p.brief)
	m.st.Mode = ModeComposing
	if p.flow {
		m.endFlowChain()
	}
	m.st.Notice = "stopped while the seats' worktrees were being prepared — nothing was sent; a tree already added is kept and reused"
}

// seatSetup cuts one worktree per named seat, serially, in seat order —
// arenaSetup's rule, for arenaSetup's reason: `git worktree add` writes the
// repository's own refs, and N at once contend for the lock this deadline
// exists to survive.
//
// Per seat, four endings, and none of them stops the others:
//
//   - the tree is on disk and is checked out on the seat's branch: REUSED, and
//     said so. A room reopened after a quit finds the trees it cut last time,
//     and a seat keeps its line across rooms.
//   - the tree is absent: cut from the room's HEAD on a new `seat/<vendor>`
//     branch. If the branch already exists without a tree (an earlier room's
//     tree was removed by hand), the branch is checked out again rather than
//     reset — it is the seat's line, and cutting over it would discard work
//     /adopt has not taken.
//   - the tree is on disk and is NOT the seat's worktree — a directory of the
//     same name somebody else made: REFUSED by name, shared tree for this seat.
//   - git refused: git's own sentence, shared tree for this seat.
//
// A context that ends stops the whole setup, and every seat not yet cut falls
// back with the reason (the deadline, or the cancel). Nothing here writes
// into a tree: no seeding, no brief file. Those are arena affordances built for
// a fresh throwaway tree, and a seat's tree is neither — it is reused, and a
// file council wrote into it once would be stale by the second turn.
func seatSetup(ctx context.Context, workspace string, seats []model.VendorID, step func(string)) seatSetupResult {
	if step == nil {
		step = func(string) {}
	}
	res := seatSetupResult{
		workspace: workspace,
		trees:     map[model.VendorID]seatTree{},
		refused:   map[model.VendorID]seatRefusal{},
	}
	refuseRest := func(from int, why string) {
		for _, v := range seats[from:] {
			if _, done := res.trees[v]; done {
				continue
			}
			res.refused[v] = seatRefusal{workspace: workspace, badge: "worktree refused", why: why}
		}
	}
	step(seatStepBase)
	head, err := gitOutCtx(ctx, workspace, "rev-parse", "HEAD")
	if err != nil {
		if ctx.Err() == context.Canceled {
			res.cancelled = true
			return res
		}
		refuseRest(0, arenaSetupStop(ctx, seatStepBase, err).Error())
		return res
	}
	for i, v := range seats {
		if ctx.Err() != nil {
			if ctx.Err() == context.Canceled {
				res.cancelled = true
				return res
			}
			refuseRest(i, arenaSetupStop(ctx, seatStepTree(v), nil).Error())
			return res
		}
		tree, branch := seatTreePath(workspace, v), seatBranch(v)
		step(seatStepTree(v))
		if _, serr := os.Stat(tree); serr == nil {
			// Something is already at the name. Reused only if it is THIS
			// seat's worktree — checked out on the seat's branch — and refused
			// by name otherwise, because a directory council did not make is
			// not one it may spawn a writing process into.
			on, gerr := gitOutCtx(ctx, tree, "symbolic-ref", "--quiet", "--short", "HEAD")
			if gerr == nil && on == branch {
				res.trees[v] = seatTree{workspace: workspace, tree: tree, branch: branch}
				res.notes = append(res.notes, string(v)+": reusing "+tree+" on "+branch)
				continue
			}
			if ctx.Err() != nil {
				refuseRest(i, arenaSetupStop(ctx, seatStepTree(v), gerr).Error())
				return res
			}
			res.refused[v] = seatRefusal{workspace: workspace, badge: "worktree refused",
				why: tree + " exists and is not " + branch + "'s worktree — move it, or git worktree add it there by hand"}
			continue
		}
		args := []string{"worktree", "add", "-b", branch, tree, head}
		if _, berr := gitOutCtx(ctx, workspace, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); berr == nil {
			// The branch outlived its tree. Checked out again as it stands,
			// never reset: the commits on it are work no adoption took.
			args = []string{"worktree", "add", tree, branch}
			res.notes = append(res.notes, string(v)+": "+branch+" already existed and is checked out as it stands")
		}
		if _, werr := gitOutCtx(ctx, workspace, args...); werr != nil {
			if ctx.Err() != nil {
				refuseRest(i, arenaSetupStop(ctx, seatStepTree(v), werr).Error())
				return res
			}
			res.refused[v] = seatRefusal{workspace: workspace, badge: "worktree refused", why: werr.Error()}
			continue
		}
		res.trees[v] = seatTree{workspace: workspace, tree: tree, branch: branch}
	}
	return res
}

// resetSeatTree moves a seat's tree and branch onto a commit — after /adopt
// merged the seat's work, so its next brief starts from the integrated line.
//
// `reset --hard` inside the seat's worktree only, undoArena's shape: the tree
// has `seat/<vendor>` checked out, so the ref moves with the tree and there is
// no window where the two disagree. The caller has just committed everything
// the tree held (commitRacerAttempt runs before the merge), so nothing
// uncommitted is on the floor for this to discard. Git's own sentence on
// refusal, carried onto the adopt notice rather than swallowed.
func resetSeatTree(tree, onto string) error {
	_, err := gitOut(tree, "reset", "--hard", onto)
	return err
}

// mergeBaseFor is where a seat's branch parted from the room's HEAD — the
// commit a seat's whole contribution answers against, for /hand. Read from
// the workspace rather than the tree so it is measured against the ROOM's
// line, which is what the reader of the hand-off is standing on.
func mergeBaseFor(workspace, branch string) (string, error) {
	return gitOut(workspace, "merge-base", "HEAD", branch)
}

// containDetail is the sentence behind the containment badge, for the `?`
// postures page — the one place the reason a fallback happened is spelled
// out whole, because the badge row sheds it at column width (badgeRow).
func containDetail(c ContainClaim) string {
	switch c.Level {
	case ContainSeatTree:
		return "Its process runs in its own worktree on branch " + c.Branch +
			", a sibling of the workspace; /adopt merges what you keep, /hand passes it on"
	case ContainShared:
		if c.Why == "" {
			return "Its process runs in the shared workspace"
		}
		return "Its process runs in the shared workspace because the room could not give it a worktree: " + c.Why
	default:
		return ""
	}
}
