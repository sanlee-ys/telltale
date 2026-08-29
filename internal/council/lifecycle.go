package council

import (
	"strconv"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Worktree end-of-life: the two verbs that finish what /arena starts (§9.37's
// deferred list). A race leaves one kept worktree and one arena branch per
// racer, and §9.37's ruling is that council never deletes or adopts them on its
// own — "adoption is a git command the user runs against a kept branch". These
// verbs keep that ruling and move the typing into the room (§9.17: a control
// you need mid-session cannot live outside it):
//
//   - /adopt <seat> merges the racer's arena branch into the room's repo,
//     behind a y/n gate whose question names the exact git command.
//   - /arena drop <seat> (or all) deletes a racer's worktree and branch,
//     refusing while either still holds work the room has not taken.
//
// THE ADOPT IS A MERGE ONTO A FRESH BRANCH, and both halves of that are
// deliberate.
//
// The merge half is the older fork. claude-squad's adopt checks the attempt's
// branch out over the user's — which rewrites the working tree wholesale and
// leaves the user's own branch behind. A room may not mutate more state than
// the act requires (§9.37's whole posture: offer, never take), so council does
// the smallest git operation that lands the work: `git merge --no-ff
// arena/t<N>/<seat>`, with --no-ff so the adoption is a visible event in
// history — a fast-forward would dissolve the race into the room's own line,
// and the merge commit is the receipt that says where the work came from.
//
// THE FRESH BRANCH IS THE OWNER'S RULING OF 2026-08-11 (§9.37's open question,
// option b). The merge used to land on whatever branch the room was standing
// on, which for most workspaces is local `main`. That is the smallest act git
// can perform and it was the wrong one HERE: the owner's standing workflow is
// branch-then-PR on every repo and every machine, never a commit straight to
// main, so the first live adoption (race t9) ended with FOUR hand-run git
// commands — cut a branch, move the merge onto it, reset main back to origin,
// push — before the work could become a PR. So adopt now cuts
// `adopt/t<N>-<vendor>` from the room's current HEAD, checks it out, and merges
// there. The branch the room was on never moves, and the hand-off shrinks from
// four commands to one: `gh pr create`, which the notice names.
//
// The cost is stated rather than hidden: council now mints a branch name in the
// operator's repo, which is a bigger footprint than one merge commit. It stays
// revertible with one command (`git checkout <old branch>` and `git branch -D`),
// it is only ever cut on the user's own y, and a FAILED adoption removes it
// again — a room left standing on an empty branch it cut for a merge that never
// landed would be the verb charging for its own failure.
//
// Neither verb consults the room's read/write posture. Posture governs what
// the SEATS may do (§9.16); both of these run only on the user's own y or
// typed force word, which is the user acting, not a vendor — the same footing
// as /cd moving the room or the user running git in another terminal.

// arenaRace is the receipt of the most recent /arena race: where every
// racer's kept worktree and branch live, and the workspace they were created
// beside. Recorded at dispatch, when the worktrees are created, so the two
// end-of-life verbs still have their target after later ordinary turns clear
// Column.Arena (startTurn resets per-turn facts; the worktrees outlive them by
// design).
//
// In-memory only, deliberately: room.json stays keys-and-numbers (ADR-008),
// and a room reopened after a quit can still finish the lifecycle by hand with
// the same git commands these verbs print — the worktrees are visible siblings
// precisely so no session state is needed to find them.
type arenaRace struct {
	// workspace is the room repo the race was cut from — captured rather than
	// read from m.st.Workspace at use time, because /cd may have moved the room
	// since and every branch and tree here belongs to the repo that raced.
	workspace string
	// raceN is the number the race's names carry (the t<N> in every branch
	// and tree) — the value arenaSetup minted against the repo's existing
	// arena refs, which is NOT always the turn the race ran on: leftovers
	// from an older room push it past (arenaRaceNumber). Both verbs re-derive
	// branch and tree names from THIS number; deriving from a turn counter
	// here would aim adopt/drop at names the race never created.
	raceN int
	base  string
	// trees maps each racer to its worktree, exactly as arenaSetup created
	// them. Entries leave this map only via a successful drop, so "raced but
	// already dropped" and "never raced" answer differently.
	trees map[model.VendorID]string
}

// adoptCommand is /adopt <seat>: arm the y/n gate that merges a racer's arena
// branch into the room's repo.
//
// Everything that can be refused is refused HERE, before the gate arms, so the
// question the user answers with y is one that can actually be honored —
// a card whose y then fails on a precondition teaches that the key is
// unreliable (askClearSeat's rule). Each refusal is its own sentence with its
// own remedy, per §9.17's tell that a refusal must name an in-room way
// forward.
func (m *Model) adoptCommand(arg string) bool {
	if m.turn != nil {
		// The race's own trees are being written mid-turn, and a merge under a
		// running turn would race the racers. /cd's refusal, for /cd's reason.
		m.st.Notice = "a turn is in flight — /adopt merges between turns"
		return true
	}
	race := m.lastRace
	if arg == "" {
		// Bare /adopt answers the question it half-asks, the way bare /cd,
		// /trace, /seat and /unseat do.
		if race == nil {
			m.st.Notice = "no race has run — /arena <brief> races the seats, then /adopt <seat> takes the winner"
		} else {
			m.st.Notice = "the last race is t" + itoa(race.raceN) +
				" — /adopt <seat> merges that seat's arena branch into the room"
		}
		m.setDraft("")
		return true
	}
	if race == nil {
		m.st.Notice = "no race has run — /arena <brief> races the seats first"
		return true
	}
	v, ok := mentionAliases()[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "@")))]
	if !ok {
		// The draft is kept: a typo is cheap to fix and nothing has run.
		m.st.Notice = "no racer called " + arg +
			" — /adopt takes " + strings.Join(SeatNames(), ", ")
		return true
	}
	tree, raced := race.trees[v]
	if !raced {
		// Covers a seat that never raced AND one whose tree was already
		// dropped — either way there is no kept worktree to adopt from.
		m.st.Notice = string(v) + " has no kept worktree from race t" + itoa(race.raceN) + " — nothing to adopt"
		return true
	}

	// Council's brief file is not work, so it must not make a zero-work seat
	// look adoptable — the refusal below is a measured zero and would become a
	// false nonzero the moment an untracked AGENTS.md counted (arenabrief.go).
	dirty, err := worktreePorcelain(tree, arenaBriefArgs(tree)...)
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	ahead, err := unadoptedCount(race, v)
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	if len(dirty) == 0 && ahead == 0 {
		// A measured zero, not a guess: the worktree is clean and the branch
		// sits at commits the room already has. Adopting it would create an
		// empty merge commit claiming work that does not exist.
		m.st.Notice = string(v) + " changed nothing in the race — there is nothing to adopt"
		return true
	}

	// THE CLEAN-TREE GATE IS THE ONE HARD PRECONDITION. A merge writes into
	// the room's working tree, and if that tree holds uncommitted work the
	// merge can entangle or (on abort) discard it. Adopt must never eat the
	// user's own edits, so a dirty room refuses by name — but only over what
	// the merge can actually harm: tracked changes always, untracked paths
	// only when the adoption writes them (adoptBlockers, and the t9 incident
	// recorded on it — the first live adopt was refused over an untracked
	// settings directory the merge would never have touched).
	roomDirty, err := adoptBlockers(race.workspace, arenaBranch(race.raceN, v))
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	if n := len(roomDirty); n > 0 {
		m.st.Notice = "the room tree holds " + itoa(n) + " uncommitted " + plural(n, "path") +
			" the merge could harm (" + roomDirty[0] + ") — commit or stash them first"
		return true
	}

	// Armed. The question names the exact commands y will run — the same
	// contract the flow write gate keeps ("y authorizes @codex → docs/out.md")
	// — because the answer to "may I?" is only informed if the "what" is on
	// screen. Two clauses are conditional on what is actually true: the commit
	// that precedes the merge when the racer's work is still uncommitted, and
	// the branch cut, which is named because it is the one thing the adoption
	// leaves in the repo besides the merge.
	//
	// THE NAME IS RESOLVED HERE, NOT AT y, and then carried on adoptOnto. A card
	// promising `adopt/t9-claude` whose y cut `adopt/t9-claude-2` would be the
	// card lying about its own scope, which is the defect this whole gate
	// exists to avoid. A ref minted between the arming and the y is the
	// remaining window, and that one ends as a named checkout failure with git's
	// own line rather than as a silent rename.
	branch := arenaBranch(race.raceN, v)
	onto, err := freeAdoptBranch(race.workspace, race.raceN, v)
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	m.adoptPending = v
	m.adoptOnto = onto
	q := "adopt " + string(v) + "? y cuts " + onto + " and runs git merge --no-ff " + branch
	if len(dirty) > 0 {
		q = "adopt " + string(v) + "? y commits its worktree, cuts " + onto +
			" and runs git merge --no-ff " + branch
	}
	m.st.Notice = q + " · n cancels"
	m.setDraft("")
	return true
}

// adoptBranch names the branch an adoption lands on, from the same race number
// every other arena name carries (§9.37's renumbering rule: the race number,
// never the turn).
//
// `adopt/t<N>-<vendor>`, with the seat joined by a dash rather than a slash, so
// the arena branch and its adoption are told apart at the end of the name
// instead of in the middle: `arena/t9/claude` and `adopt/t9-claude` sit beside
// each other in one `git branch` listing, where the eye reads the last segment.
func adoptBranch(raceN int, v model.VendorID) string {
	return "adopt/t" + itoa(raceN) + "-" + string(v)
}

// adoptBranchLimit caps the collision suffixes freeAdoptBranch will try. A repo
// holding 50 adoptions of one racer from one race is a state to report, not to
// keep counting through.
const adoptBranchLimit = 50

// freeAdoptBranch picks the name this adoption cuts: adoptBranch's own spelling
// when nothing holds it, else the first free `-2`, `-3` … suffix.
//
// A COLLISION IS ORDINARY, not exotic. Race numbers repeat by design —
// arenaRaceNumber scans `refs/heads/arena/` alone, so dropping a race's
// branches frees its number for the next race to mint again (§9.37's own note
// that t9's leftovers were dropped and the scan will mint t9 next) — and an
// operator can adopt, revert the merge, and adopt again. Reusing the name would
// either fail the checkout or land the merge on an older adoption's branch,
// where the PR would carry work nobody asked about.
//
// ONE SCAN, NOT A PROBE PER CANDIDATE. `for-each-ref` over the adopt namespace
// answers every candidate at once, and it separates "no such branch" from "git
// could not answer" — which `rev-parse --verify --quiet` cannot, since both
// exit 1 with nothing on stderr. A scan that cannot run degrades to the plain
// name with the adoption still running (arenaRaceNumber's rule): the worst case
// is the collision itself, and `git checkout -b` reports that by name, carrying
// git's own fatal line.
func freeAdoptBranch(workspace string, raceN int, v model.VendorID) (string, error) {
	name := adoptBranch(raceN, v)
	out, err := gitOut(workspace, "for-each-ref", "--format=%(refname:short)", "refs/heads/adopt/")
	if err != nil {
		return name, nil
	}
	taken := map[string]bool{}
	for _, ref := range strings.Split(out, "\n") {
		if ref != "" {
			taken[ref] = true
		}
	}
	if !taken[name] {
		return name, nil
	}
	for i := 2; i <= adoptBranchLimit; i++ {
		if cand := name + "-" + itoa(i); !taken[cand] {
			return cand, nil
		}
	}
	return "", gitError(name + " and every suffix through -" + itoa(adoptBranchLimit) +
		" already exist — delete one before adopting this racer again")
}

// roomRef names where the room's HEAD stands, in the form `git checkout` takes
// back: the branch name when one is checked out, else the commit itself.
//
// Both forms are read because both are reachable. A workspace on a detached
// HEAD is unusual and entirely legal, and an adoption that could not name where
// it started could not put the room back after a merge that failed.
func roomRef(workspace string) (string, error) {
	if name, err := gitOut(workspace, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && name != "" {
		return name, nil
	}
	return gitOut(workspace, "rev-parse", "HEAD")
}

// undoAdoptBranch puts the room back where it started after an adoption that
// did not land, and returns the sentence saying where the room now stands.
//
// A FAILED ADOPTION LEAVES NOTHING BEHIND. The branch was cut for a merge that
// never arrived, so it holds exactly what the room's own branch holds and
// deleting it loses nothing — while leaving it would hand the operator a repo
// standing on an empty branch they never asked for, as the receipt of a failure.
// This is the conservative half of the 2026-08-11 ruling: the ruling covers
// where a SUCCESSFUL adoption lands, and the alternative reading — leave the
// room on the fresh branch so the conflict is resolved there — is recorded in
// §9.37's ruling block rather than chosen here.
//
// Each step that cannot be taken is named rather than swallowed, because what
// the room is standing on is the one fact the next command depends on.
func undoAdoptBranch(workspace, from, onto string) string {
	if _, err := gitOut(workspace, "checkout", from); err != nil {
		return "the room is left on " + onto + " — git checkout " + from + " puts it back"
	}
	if _, err := gitOut(workspace, "branch", "-D", onto); err != nil {
		return "the room is back on " + from + ", with an empty " + onto + " left behind"
	}
	return "the room is back on " + from
}

// adoptSeat performs the adopt the gate approved, and returns the sentence the
// notice shows. Reached only from adoptGateKey's y.
//
// Steps, each a plain git command through gitOut's argv (never a shell):
//
//  1. If the racer's worktree holds uncommitted work, commit it — in the
//     WORKTREE, on the racer's own arena branch. The attempt has to be a
//     commit before a merge can carry it, arena seats leave their work
//     uncommitted (commit-per-turn is deferred, §9.37), and the arena branch
//     is the one ref that exists to hold exactly this. Signing and author
//     identity come from the user's own git config, unmodified: an adopt
//     commit enters the room's history, and council inventing an identity or
//     skipping the user's signing rule there would be the room writing
//     history that misstates its provenance.
//  2. Re-check the room tree is clean. Between arming and y no turn can run
//     (the pending gate swallows every key), but nothing stops an external
//     process from writing to the workspace — the check is one git status.
//  3. `git checkout -b adopt/t<N>-<vendor>` in the room repo: the fresh branch
//     the 2026-08-11 ruling lands the adoption on, cut from wherever the room
//     is standing. Where the room stood is read FIRST, because it is what a
//     failure has to restore.
//  4. `git merge --no-ff --no-edit <branch>` on that new branch. On failure:
//     if a merge is actually in progress (MERGE_HEAD exists — a conflict
//     stopped it midway), `git merge --abort` puts the tree back and the
//     notice says a human merge is needed; if no merge started (git refused
//     outright), the tree was never touched and the notice says that instead.
//     The two endings are different facts and are not collapsed (§4a.1).
//     Either way the branch cut in step 3 is removed and the room goes back to
//     the branch it was on (undoAdoptBranch).
func (m *Model) adoptSeat(v model.VendorID, onto string) string {
	race := m.lastRace
	if race == nil {
		// Unreachable from the gate, which only arms over a live race — kept so
		// this function cannot nil-deref if a future caller finds it.
		return "no race has run — nothing to adopt"
	}
	tree, ok := race.trees[v]
	if !ok {
		return string(v) + " has no kept worktree from race t" + itoa(race.raceN)
	}
	branch := arenaBranch(race.raceN, v)
	if onto == "" {
		// Unreachable from the gate, which resolves the name when it arms. Kept
		// so a future caller cannot land an adoption on an unnamed branch — the
		// same defensive shape the nil-race check above keeps.
		var err error
		if onto, err = freeAdoptBranch(race.workspace, race.raceN, v); err != nil {
			return "adopt: " + err.Error()
		}
	}

	// Council's own AGENTS.md is excluded from BOTH halves, and the dirty read
	// is the half that matters more. This commit is the one /adopt makes for a
	// racer whose work never reached commitArena — the give-up path, mostly —
	// and without the exclusion a seat whose tree holds nothing but council's
	// brief file would read as work owed, land a commit carrying only that
	// file, and merge it into the operator's repo (arenabrief.go).
	exclude := arenaBriefArgs(tree)
	dirty, err := worktreePorcelain(tree, exclude...)
	if err != nil {
		return "adopt: " + err.Error()
	}
	if len(dirty) > 0 {
		if _, err := gitOut(tree, append([]string{"add", "-A"}, exclude...)...); err != nil {
			return "adopt: " + err.Error()
		}
		if _, err := gitOut(tree, "commit", "-m", string(v)+"'s arena attempt, race t"+itoa(race.raceN)); err != nil {
			return "adopt: the attempt could not be committed to " + branch + " — " + err.Error()
		}
	}

	roomDirty, err := adoptBlockers(race.workspace, branch)
	if err != nil {
		return "adopt: " + err.Error()
	}
	if n := len(roomDirty); n > 0 {
		return "the room tree holds " + itoa(n) + " uncommitted " + plural(n, "path") +
			" the merge could harm (" + roomDirty[0] + ") — nothing was merged; commit or stash them first"
	}

	// Where the room stands, read before anything moves it: this is the ref a
	// failed adoption returns to, and reading it after the checkout would read
	// the branch the adoption itself just cut.
	from, err := roomRef(race.workspace)
	if err != nil {
		return "adopt: " + err.Error()
	}
	if _, err := gitOut(race.workspace, "checkout", "-b", onto); err != nil {
		// Nothing to restore: the checkout is the first act that moves the room,
		// so a checkout that failed leaves it exactly where it was.
		return "the branch could not be cut: " + err.Error() + " — the room is still on " + from
	}

	if _, err := gitOut(race.workspace, "merge", "--no-ff", "--no-edit", branch); err != nil {
		conflicted := false
		if _, mh := gitOut(race.workspace, "rev-parse", "--verify", "MERGE_HEAD"); mh == nil {
			// A conflict stopped the merge midway. Aborting is the restore, not
			// a retreat: the alternative leaves the user's repo mid-merge with
			// conflict markers they never asked for, discovered later as a
			// broken build. The work is still whole on the arena branch.
			conflicted = true
			_, _ = gitOut(race.workspace, "merge", "--abort")
		}
		where := undoAdoptBranch(race.workspace, from, onto)
		if conflicted {
			return "the merge failed: " + err.Error() + " — aborted, the room tree is restored and " +
				where + "; adopting " + branch + " needs a human merge"
		}
		return "the merge failed: " + err.Error() + " — the room tree is untouched and " + where
	}
	// The next command is named because the adoption is deliberately NOT the
	// end of the hand-off: publishing is the operator's act, as it is for every
	// other commit (§9.37's founding posture), and the branch this now stands on
	// is what makes that act one command instead of four.
	return "adopted " + string(v) + " onto " + onto +
		" — gh pr create opens the PR · /arena drop " + string(v) + " removes its worktree"
}

// parseArenaDrop recognises the drop verb inside a /arena draft: "drop",
// "drop <seat>", "drop <seat>!", "drop all", "drop all!" — and NOTHING longer.
//
// The length cap is the vocabulary rule (roomcmd.go): only a draft that IS the
// command is intercepted. "/arena drop the cache layer and rebuild" is a brief
// someone will genuinely race, and a looser prefix match would steal it — so a
// third word hands the whole draft back to the race path as prose. The cost is
// the two-word brief "drop codex", which now needs rewording to race; that
// brief asks four agents to delete a teammate's worktree, and a room that
// raced it instead of asking would be the cheaper mistake to regret.
func parseArenaDrop(brief string) (seat string, force, ok bool) {
	f := strings.Fields(brief)
	if len(f) == 0 || f[0] != "drop" || len(f) > 2 {
		return "", false, false
	}
	if len(f) == 1 {
		return "", false, true
	}
	seat = f[1]
	if strings.HasSuffix(seat, "!") {
		force, seat = true, strings.TrimSuffix(seat, "!")
	}
	return seat, force, true
}

// arenaDrop is /arena drop <seat> (or all): delete a racer's worktree and its
// arena branch, guarded so nothing the room has not taken can be lost silently.
//
// THE FORCE IS A SPELLING, NOT A KEYSTROKE. A guarded drop refuses with a
// sentence that says exactly what would be lost and names the force form
// (`/arena drop <seat>!`); forcing means re-running the command with the bang.
// A y/n confirm was considered and rejected for this verb: y is one keystroke
// answered against a notice the user may not have finished reading, while the
// bang travels IN the command — the user types the destruction they are
// asking for, the draft records that they asked, and a stray key can never
// produce it. It is also a vocabulary git users already own (-D, -f, :q!).
// /adopt keeps the y/n shape because its act is additive (a merge, revertible
// with git's own tools); drop deletes work with no ref left pointing at it.
func (m *Model) arenaDrop(word string, force bool) {
	if m.turn != nil {
		// An arena turn in flight is WRITING to these trees; an ordinary turn
		// still holds the room's dispatch state. Between turns, like every
		// other mutation typed at the room.
		m.st.Notice = "a turn is in flight — /arena drop removes worktrees between turns"
		return
	}
	race := m.lastRace
	if race == nil {
		m.st.Notice = "no race has run — there is no arena worktree to drop"
		return
	}
	if word == "" {
		// Bare "drop" half-asks; answer it, the house shape.
		m.st.Notice = "/arena drop <seat> deletes that racer's worktree and branch — all takes every one, a trailing ! discards unadopted work"
		m.setDraft("")
		return
	}

	var targets []model.VendorID
	if strings.EqualFold(word, "all") {
		// Grid order, so the report reads in the order the room draws seats.
		for _, c := range m.st.Columns {
			if _, ok := race.trees[c.Vendor]; ok {
				targets = append(targets, c.Vendor)
			}
		}
		if len(targets) == 0 {
			m.st.Notice = "every worktree from race t" + itoa(race.raceN) + " is already dropped"
			m.setDraft("")
			return
		}
	} else {
		v, ok := mentionAliases()[strings.ToLower(strings.TrimPrefix(word, "@"))]
		if !ok {
			m.st.Notice = "no racer called " + word +
				" — /arena drop takes " + strings.Join(SeatNames(), ", ") + ", or all"
			return
		}
		if _, raced := race.trees[v]; !raced {
			m.st.Notice = string(v) + " has no kept worktree from race t" + itoa(race.raceN)
			return
		}
		targets = []model.VendorID{v}
	}

	// Per seat, refusals do not abort the batch: "drop all" over one dirty
	// tree drops the clean ones and names the survivor — a partial act that
	// reports itself beats an all-or-nothing that punishes three clean trees
	// for one dirty one (§4a.1's degrade-the-field rule, one level up).
	var dropped, kept []string
	for _, v := range targets {
		if why := m.dropRacer(v, force); why == "" {
			dropped = append(dropped, string(v))
		} else {
			kept = append(kept, why)
		}
	}
	var parts []string
	if len(dropped) == 1 && len(targets) == 1 {
		parts = append(parts, "dropped "+dropped[0]+" — its worktree and arena branch are deleted")
	} else if len(dropped) > 0 {
		parts = append(parts, "dropped "+strings.Join(dropped, ", ")+" — worktrees and arena branches deleted")
	}
	parts = append(parts, kept...)
	m.st.Notice = strings.Join(parts, " · ")
	if len(kept) == 0 {
		m.setDraft("")
	}
	// A refusal keeps the draft: the force form is one keystroke of editing
	// away, and retyping the whole command to add a bang would be the room
	// charging for its own caution.
}

// dropRacer removes one racer's worktree and branch, or returns the sentence
// explaining why it did not. Empty string means dropped.
//
// THE PATH CHECK IS THE MECHANICAL GUARD, not the map membership. Every tree
// this function will ever remove must re-derive, from the recorded race's own
// (workspace, turn, seat), to exactly the path arenaSetup would have created —
// arenaTree is the single spelling of that name, shared with setup. A tree
// that fails the check is refused even under force, because no state this
// room's arena created can have that name: whatever put it in the map, it is
// not ours to delete. Stolen from Pane's deletion guard, where the rule is the
// same: a destructive verb only ever aims at paths the tool itself minted.
func (m *Model) dropRacer(v model.VendorID, force bool) string {
	race := m.lastRace
	tree := race.trees[v]
	branch := arenaBranch(race.raceN, v)
	if want := arenaTree(race.workspace, race.raceN, v); tree != want {
		return string(v) + ": " + tree + " is not this race's worktree — refusing to remove it"
	}

	spell := "/arena drop " + string(v) + "!"
	if !force {
		// Two guards, two sentences, each naming what would be lost AND how to
		// proceed — a refusal without its remedy is §9.17's defect.
		// Council's brief file excluded: a drop that demanded the `!` spelling
		// on every clean attempt would be refusing over the room's own write
		// (arenabrief.go).
		dirty, err := worktreePorcelain(tree, arenaBriefArgs(tree)...)
		if err != nil {
			return string(v) + ": " + err.Error()
		}
		if n := len(dirty); n > 0 {
			return string(v) + ": its worktree holds " + itoa(n) + " uncommitted " + plural(n, "path") +
				" — " + spell + " discards them"
		}
		n, err := unadoptedCount(race, v)
		if err != nil {
			return string(v) + ": " + err.Error()
		}
		if n > 0 {
			return string(v) + ": " + branch + " holds " + itoa(n) + " " + plural(n, "commit") +
				" the room has not merged — /adopt " + string(v) + " takes them, " + spell + " discards them"
		}
	}

	// Council takes its own brief file back before git is asked to remove the
	// tree. `git worktree remove` counts an untracked file as dirty and refuses,
	// so leaving it there would make every ordinary drop fail and demand the `!`
	// spelling — the room's own write turning a plain verb into a forced one.
	// removeArenaBrief deletes only a file still carrying council's marker, so a
	// racer's own AGENTS.md keeps the refusal it has earned (arenabrief.go).
	removeArenaBrief(tree)

	args := []string{"worktree", "remove"}
	if force {
		// git itself refuses to remove a dirty worktree; the force the user
		// spelled is handed to git as git's own flag rather than reimplemented
		// as an rm — the deletion stays a git operation end to end.
		args = append(args, "--force")
	}
	if _, err := gitOut(race.workspace, append(args, tree)...); err != nil {
		return string(v) + ": " + err.Error()
	}
	if _, err := gitOut(race.workspace, "branch", "-D", branch); err != nil {
		// Half-done is reported as half-done: the tree is gone, the ref is not.
		return string(v) + ": worktree removed, but " + branch + " remains — " + err.Error()
	}
	delete(race.trees, v)
	return ""
}

// worktreePorcelain is one `git status --porcelain`, split into its lines: the
// count is what refusals name, and empty means a measured clean.
//
// pathspec is what a caller reading a RACER's tree passes: council's own brief
// file is not the racer's uncommitted work, and counting it would turn every
// clean attempt into a dirty one (arenabrief.go). Callers reading the ROOM's
// tree pass nothing — an AGENTS.md there is the repository's own.
func worktreePorcelain(dir string, pathspec ...string) ([]string, error) {
	out, err := gitOut(dir, append([]string{"status", "--porcelain"}, pathspec...)...)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// adoptBlockers names what in the room tree actually stands in a merge's way,
// or returns nothing for a room the adoption cannot harm.
//
// The first live adopt (2026-08-09, race t9) was refused over `?? .claude/` —
// an untracked settings directory the merge would never have touched — because
// the gate counted every porcelain line as danger. That was the gate being
// blunt, not safe: the hazard the clean-tree rule exists for is a merge
// ENTANGLING uncommitted work, and a merge can only entangle paths it writes.
// So the rule is split along what git itself distinguishes:
//
//   - A TRACKED change blocks unconditionally. The merge machinery reads and
//     rewrites tracked state, an abort resets it, and reasoning about which
//     tracked change is safe would be a guess where the stake is the
//     operator's own edits.
//   - An UNTRACKED path blocks only if the adoption would write it — the
//     branch's own file list (`git diff --name-only HEAD...branch`, the
//     merge-base half git merge actually applies), compared path-for-path,
//     with an untracked DIRECTORY (porcelain prints those with a trailing
//     slash) blocking on any branch file under it. git refuses exactly this
//     overlap itself ("untracked working tree files would be overwritten");
//     the gate saying it first, by name, before y is armed, is the whole
//     improvement.
func adoptBlockers(workspace, branch string) ([]string, error) {
	lines, err := worktreePorcelain(workspace)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, nil
	}
	var blockers, untracked []string
	for _, l := range lines {
		if strings.HasPrefix(l, "??") {
			untracked = append(untracked, strings.TrimSpace(strings.TrimPrefix(l, "??")))
			continue
		}
		blockers = append(blockers, strings.TrimSpace(l))
	}
	if len(untracked) > 0 {
		out, err := gitOut(workspace, "--no-pager", "diff", "--name-only", "HEAD..."+branch)
		if err != nil {
			return nil, err
		}
		incoming := strings.Split(out, "\n")
		for _, u := range untracked {
			for _, f := range incoming {
				if f == "" {
					continue
				}
				if f == u || (strings.HasSuffix(u, "/") && strings.HasPrefix(f, u)) {
					blockers = append(blockers, "?? "+u+" (the adoption writes "+f+")")
					break
				}
			}
		}
	}
	return blockers, nil
}

// unadoptedCount is how many commits the racer's branch holds that the room's
// HEAD cannot reach — the work a drop would orphan and an adopt would take.
// Measured with rev-list against the room repo's own HEAD rather than the
// recorded base, because "unadopted" is a fact about where the ROOM is now: a
// branch already merged counts zero even though it is ahead of the base.
func unadoptedCount(race *arenaRace, v model.VendorID) (int, error) {
	out, err := gitOut(race.workspace, "rev-list", "--count", "HEAD.."+arenaBranch(race.raceN, v))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}
