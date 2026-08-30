package council

import (
	"os"
	"path/filepath"
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
//   - /adopt <seat> +<seat> <path...> merges the first racer whole and then
//     takes exactly the named paths from the second — the hybrid form, added
//     2026-08-29. The card names both sources and every path, and the receipt
//     commit on the adopt branch names them again.
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
// THE HYBRID IS PER-PATH AND USER-TYPED, and both halves of that are the
// decision (§9.37's 2026-08-29 hybrid amendment).
//
// Per-PATH rather than per-hunk. The room's whole command style is a typed verb
// with typed arguments; it owns no picker, and a per-hunk selector is a new
// full-frame surface with its own scroll, its own keys and its own mode word —
// disproportionate to a v1 whose value is that the operator can take one file
// from the runner-up. A path is also the unit the operator already has in front
// of them: `git diff --stat` in the column, and the overlap clause on this very
// card, both name paths. Per-hunk stays open, and this shape does not block it:
// a hunk picker would narrow what `+<seat>` contributes, not change the grammar.
//
// User-typed rather than computed. §9.34 rejected a synthesis hop — no model
// merges the seats' answers — and that ruling binds here: council applies the
// paths the operator named and chooses nothing. A path that BOTH racers wrote is
// refused by name rather than resolved, because resolving it would be council
// deciding between two writers with nobody watching. So is a path the ROOM wrote
// since the race was cut: `git checkout <branch> -- <path>` overwrites without a
// merge and without a conflict marker, so a silent clobber is the same defect
// one level out.
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
				" — /adopt <seat> merges that seat's arena branch into the room" +
				" · +<seat> <path...> takes those paths from a second racer too"
		}
		m.setDraft("")
		return true
	}
	if race == nil {
		m.st.Notice = "no race has run — /arena <brief> races the seats first"
		return true
	}
	baseWord, donorWord, paths, ok := parseAdoptArg(arg)
	if !ok {
		// Neither shape. The refusal teaches both rather than only the one the
		// draft came closest to: a reader who typed the second form wrong needs
		// to see the second form.
		m.st.Notice = "/adopt takes a seat, or a seat plus paths from another — " +
			"/adopt <seat> · /adopt <seat> +<seat> <path...>"
		return true
	}
	v, ok := mentionAliases()[strings.ToLower(strings.TrimSpace(strings.TrimPrefix(baseWord, "@")))]
	if !ok {
		// The draft is kept: a typo is cheap to fix and nothing has run.
		m.st.Notice = "no racer called " + baseWord +
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

	// The donor half is resolved before anything is measured, so a mistyped seat
	// costs no git call and keeps the draft.
	var donor model.VendorID
	if donorWord != "" {
		d, ok := mentionAliases()[strings.ToLower(strings.TrimPrefix(donorWord, "@"))]
		if !ok {
			m.st.Notice = "no racer called " + donorWord +
				" — /adopt takes paths from " + strings.Join(SeatNames(), ", ")
			return true
		}
		if d == v {
			// Not a hybrid at all, and the plain verb already does it. Naming the
			// plain verb is the remedy, per §9.17's tell.
			m.st.Notice = "the two seats are the same — /adopt " + string(v) + " takes that attempt whole"
			return true
		}
		if _, raced := race.trees[d]; !raced {
			m.st.Notice = string(d) + " has no kept worktree from race t" + itoa(race.raceN) +
				" — there are no paths to take from it"
			return true
		}
		if len(paths) == 0 {
			m.st.Notice = "name the paths to take from " + string(d) +
				" — /adopt " + string(v) + " +" + string(d) + " <path...>"
			return true
		}
		donor = d
	}

	dirty, err := worktreePorcelain(tree)
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	// One read answers two questions. `ahead` is the same figure unadoptedCount
	// takes off `rev-list --count HEAD..<branch>` — the zero-change refusal
	// below is unchanged — and the same command also returns `behind`, which is
	// what the card needs to say where the merge is going. A failed read still
	// refuses by name, exactly as the older call did: an unreadable ref must
	// never reach the operator as a clean one.
	div, err := readAdoptDivergence(race.workspace, arenaBranch(race.raceN, v))
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	if len(dirty) == 0 && div.ahead == 0 {
		// A measured zero, not a guess: the worktree is clean and the branch
		// sits at commits the room already has. Adopting it would create an
		// empty merge commit claiming work that does not exist.
		m.st.Notice = string(v) + " changed nothing in the race — there is nothing to adopt"
		return true
	}

	// The hybrid's paths are checked HERE, before the gate arms, so the card can
	// name every one of them and mean it. Each refusal is its own sentence with
	// its own remedy, and none of them resolves anything on the operator's
	// behalf: a path two writers touched is handed back, never merged.
	var donorPaths []string
	donorDirty := false
	if donor != "" {
		var why string
		if donorPaths, why = m.hybridPaths(race, v, donor, paths); why != "" {
			m.st.Notice = why
			return true
		}
		// The card's commit clause is keyed on the donor's WHOLE tree, not on the
		// chosen paths, because `y` commits the whole tree — the same act the base
		// racer's attempt gets, for the same reason. A card that named the commit
		// only when a chosen path was dirty would stay silent while y committed
		// the donor's other files.
		lines, err := worktreePorcelain(race.trees[donor])
		if err != nil {
			m.st.Notice = "adopt: " + err.Error()
			return true
		}
		donorDirty = len(lines) > 0
	}

	// THE CLEAN-TREE GATE IS THE ONE HARD PRECONDITION. A merge writes into
	// the room's working tree, and if that tree holds uncommitted work the
	// merge can entangle or (on abort) discard it. Adopt must never eat the
	// user's own edits, so a dirty room refuses by name — but only over what
	// the merge can actually harm: tracked changes always, untracked paths
	// only when the adoption writes them (adoptBlockers, and the t9 incident
	// recorded on it — the first live adopt was refused over an untracked
	// settings directory the merge would never have touched).
	//
	// A hybrid writes the chosen paths too, so they are handed to the same check
	// rather than to a second one — one spelling of "what this adoption writes"
	// is what keeps the untracked-collision rule from disagreeing with itself.
	roomDirty, err := adoptBlockers(race.workspace, arenaBranch(race.raceN, v), donorPaths)
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
	want := adoptBranch(race.raceN, v)
	if donor != "" {
		want = hybridAdoptBranch(race.raceN, v, donor)
	}
	onto, err := freeAdoptBranch(race.workspace, want)
	if err != nil {
		m.st.Notice = "adopt: " + err.Error()
		return true
	}
	m.adoptPending = v
	m.adoptOnto = onto
	m.adoptDonor = donor
	m.adoptPaths = donorPaths
	// THE PREVIEW LEADS, and that placement is the decision (§9.37's 2026-08-29
	// amendment). This line truncates from the right at a narrow width, so
	// leading with the measured state can cost the action clause its tail — and
	// the action clause is the older contract. It leads anyway, for §9.41's
	// reason on the room's other gate: a card that names the act and not its
	// subject leaves "yes because I trust it" and "no because I don't", which is
	// the gate reduced to a mood. An operator who can see only the first clause
	// can still press n, and the preview is what makes that n a decision.
	//
	// THE HYBRID NAMES EVERY PATH, not a count with an example. The count-plus-
	// first-path grammar the overlap clause uses is right for a measurement the
	// room took; these paths are the SCOPE the y authorizes, and a card that
	// authorized "2 paths (a.txt)" would leave the second one unread. The
	// operator typed them, so the list is short by construction.
	commits := "commits its worktree"
	if donorDirty && len(dirty) > 0 {
		commits = "commits both worktrees"
	} else if donorDirty {
		commits = "commits " + string(donor) + "'s worktree"
	}
	act := " · y cuts " + onto + " and runs git merge --no-ff " + branch
	if len(dirty) > 0 || donorDirty {
		act = " · y " + commits + ", cuts " + onto +
			" and runs git merge --no-ff " + branch
	}
	head := "adopt " + string(v) + "? "
	if donor != "" {
		act += ", then takes " + strings.Join(donorPaths, ", ") + " from " + arenaBranch(race.raceN, donor)
		head = "adopt " + string(v) + " + " + itoa(len(donorPaths)) + " " +
			plural(len(donorPaths), "path") + " from " + string(donor) + "? "
	}
	m.st.Notice = head + div.sentence(len(dirty)) + act + " · n cancels"
	m.setDraft("")
	return true
}

// parseAdoptArg reads /adopt's argument in its two shapes:
//
//	<seat>                       — the whole attempt
//	<seat> +<seat> <path...>     — the attempt, plus named paths from another
//
// ok is false for anything else, and the caller then teaches BOTH forms rather
// than guessing which one was meant.
//
// The `+` is glued to the donor seat on purpose. A bare `+` as its own word
// would make `/adopt claude + codex` legal, and that reads as a request for two
// whole attempts — which this verb cannot do and must not appear to offer. Glued,
// the token is unmistakably one argument naming one seat.
//
// A missing path list parses as ok with an empty paths slice, because "you named
// a donor and no paths" has a better answer than "that is not a command": the
// caller refuses it by naming the seat and the form.
func parseAdoptArg(arg string) (base, donor string, paths []string, ok bool) {
	f := strings.Fields(arg)
	switch {
	case len(f) == 0:
		return "", "", nil, false
	case len(f) == 1:
		return f[0], "", nil, true
	}
	if !strings.HasPrefix(f[1], "+") || len(f[1]) == 1 {
		return "", "", nil, false
	}
	return f[0], strings.TrimPrefix(f[1], "+"), f[2:], true
}

// hybridPaths validates the paths a hybrid adopt would take from the donor and
// returns them de-duplicated, or the refusal sentence when one of them cannot be
// taken.
//
// FIVE REFUSALS, AND NOT ONE OF THEM RESOLVES ANYTHING. Each names the offending
// path and what to do instead, per §9.17's tell:
//
//  1. A path outside the repository. An absolute path or one climbing through
//     `..` is a pathspec aimed somewhere this verb has no business writing.
//  2. A path the donor did not write. Taking it would land the base attempt's own
//     content and call it an adoption from the donor — a receipt that lies.
//  3. A path the BASE racer also wrote. This is the hybrid's founding refusal:
//     two seats answered the same file, and `git checkout <donor> -- <path>`
//     would discard the base's answer with no merge and no marker. Council does
//     not pick between two writers (§9.34's no-synthesis ruling, applied to a
//     file rather than to prose).
//  4. A path the ROOM wrote since the race was cut. The same silent clobber, one
//     level out: the merge machinery never sees this path, so the room's own work
//     at it would vanish under the donor's copy.
//  5. A path the donor deleted, or that is not a file in its worktree. The
//     checkout would fail with git's own pathspec error after the branch was
//     already cut; refusing before the card arms is the same rule the clean-tree
//     gate follows. A hybrid takes files a racer wrote, never a deletion — a
//     stated v1 limit, not a silence.
//
// THE DONOR'S UNCOMMITTED WORK COUNTS AS WRITTEN, and that is a deliberate fork
// from the 2026-08-29 preview ruling, which declined to fold a racer's
// uncommitted paths into the OVERLAP set. That ruling was about a preview of a
// merge result — a guess about a commit nobody had made. This is not a guess:
// `y` commits both worktrees in the same act, with `git add -A`, so the paths
// that commit will hold are exactly (tracked changes ∪ untracked-and-not-ignored)
// — which is what racerWrites reads, by definition rather than by prediction.
// Refusing to read them would refuse every hybrid on the ordinary race, because
// arena seats leave their work uncommitted (commit-per-turn is deferred).
func (m *Model) hybridPaths(race *arenaRace, base, donor model.VendorID, typed []string) (paths []string, why string) {
	donorTree := race.trees[donor]
	donorCommitted, donorPending, err := racerWrites(race.workspace, donorTree, arenaBranch(race.raceN, donor))
	if err != nil {
		return nil, "adopt: " + err.Error()
	}
	baseCommitted, basePending, err := racerWrites(race.workspace, race.trees[base], arenaBranch(race.raceN, base))
	if err != nil {
		return nil, "adopt: " + err.Error()
	}
	// The room's own half over the same merge base readAdoptOverlap uses, so the
	// card's overlap clause and this refusal can never disagree about what the
	// room wrote.
	roomWrote, err := changedFiles(race.workspace, arenaBranch(race.raceN, donor)+"...HEAD")
	if err != nil {
		return nil, "adopt: " + err.Error()
	}

	donorAll := union(donorCommitted, donorPending)
	baseAll := union(baseCommitted, basePending)
	room := set(roomWrote)

	seen := map[string]bool{}
	for _, raw := range typed {
		p := cleanRepoPath(raw)
		if p == "" {
			return nil, raw + " is not a path inside the repository — /adopt takes paths as git spells them, from the repository root"
		}
		if seen[p] {
			// Typing one path twice is a slip, not a request; the second copy is
			// dropped rather than refused.
			continue
		}
		switch {
		case !donorAll[p]:
			return nil, string(donor) + " changed nothing at " + p +
				" — /adopt takes paths that racer wrote"
		case baseAll[p]:
			return nil, string(base) + " and " + string(donor) + " both wrote " + p +
				" — /adopt merges no path two seats changed; drop it, or merge it by hand afterwards"
		case room[p]:
			return nil, "the room and " + string(donor) + " both wrote " + p +
				" — /adopt would overwrite the room's copy; merge that path by hand"
		case !isFileIn(donorTree, p):
			return nil, string(donor) + " has no file at " + p +
				" — a hybrid adopt takes files a racer wrote, never a deletion"
		}
		seen[p] = true
		paths = append(paths, p)
	}
	return paths, ""
}

// racerWrites is every path an adoption of one racer will write: what its arena
// branch already holds over the room, and what its worktree is about to commit.
//
// The second half is read as git itself defines the commit `adoptSeat` makes.
// `git add -A` stages tracked changes and untracked files that are not ignored,
// so `diff --name-only HEAD` plus `ls-files --others --exclude-standard` IS that
// commit's path list — not a forecast of it. Both are read with `-z`, so a path
// carrying a space or a quote arrives whole instead of C-quoted.
func racerWrites(workspace, tree, branch string) (committed, pending []string, err error) {
	if committed, err = changedFiles(workspace, "HEAD..."+branch); err != nil {
		return nil, nil, err
	}
	tracked, err := gitOut(tree, "--no-pager", "diff", "--name-only", "-z", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	others, err := gitOut(tree, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, nil, err
	}
	return committed, append(splitNUL(tracked), splitNUL(others)...), nil
}

// splitNUL reads a `-z` git listing into paths, empties dropped.
func splitNUL(out string) []string {
	var paths []string
	for _, p := range strings.Split(out, "\x00") {
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

func set(paths []string) map[string]bool {
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		if p != "" {
			m[p] = true
		}
	}
	return m
}

func union(a, b []string) map[string]bool {
	m := set(a)
	for _, p := range b {
		if p != "" {
			m[p] = true
		}
	}
	return m
}

// cleanRepoPath is a typed path as git spells one, or empty when it is not a
// path this verb may touch.
//
// Backslashes become forward slashes so a Windows operator typing a Windows path
// is understood — every other path in this room comes out of git, which spells
// them with forward slashes, and the two must compare equal. An absolute path or
// one containing a `..` segment is refused rather than resolved: a hybrid writes
// inside the room repo, and a pathspec that leaves it is a mistake worth naming.
func cleanRepoPath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, `\`, "/"))
	p = strings.TrimPrefix(p, "./")
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "//") {
		return ""
	}
	if len(p) > 1 && p[1] == ':' {
		// A drive letter — `C:/…`. Absolute on the one platform this room calls
		// primary (ADR-002).
		return ""
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." || seg == "" {
			return ""
		}
	}
	return p
}

// isFileIn reports that the racer's worktree holds a regular file at this path —
// the fact `git checkout <branch> -- <path>` will need, read off the tree whose
// contents that branch is about to hold.
func isFileIn(tree, p string) bool {
	fi, err := os.Stat(filepath.Join(tree, filepath.FromSlash(p)))
	return err == nil && fi.Mode().IsRegular()
}

// adoptDivergence is what the /adopt card reports BEFORE y: how far the racer's
// branch and the room have moved apart, and which paths both of them wrote
// since they parted.
//
// WHY IT IS ON THE CARD AT ALL. /adopt already handles a bad merge reactively —
// `git merge --no-ff`, then `git merge --abort` when a conflict stops it, the
// branch removed and the tree restored — so nothing here saves the operator
// from a failure the verb could not already survive. What it ends is the
// operator answering y without knowing what the merge is going INTO. That is
// §9.41's finding, on the room's other gate: a card that names the act and not
// its subject leaves only "yes because I trust it" and "no because I don't".
//
// EVERY FIELD IS READ AND NONE IS INFERRED (§4a.1). The counts come from one
// `rev-list --left-right --count`, the paths from two `diff --name-only` reads,
// and the baseline is the room's own HEAD because that is the commit /adopt
// cuts the adopt branch from. Nothing here predicts a merge RESULT: the word
// "conflict" belongs to a merge that ran, and an overlap is only the measured
// fact that both sides changed one path. A repository can overlap on a path and
// merge cleanly, and this card must never be read as promising otherwise.
type adoptDivergence struct {
	// base names the ref the adoption merges into, for a human to read. Empty
	// when git could not answer, which the sentence says rather than hides.
	base string
	// ahead is commits on the arena branch the room's HEAD cannot reach, and
	// behind is commits on the room's HEAD the arena branch cannot reach. The
	// second is the one the operator has no other way to see: it is everything
	// that landed in the room since the race was cut.
	ahead, behind int
	// overlap is the paths BOTH sides changed since their merge base, in the
	// order the incoming side lists them.
	//
	// Three states, kept apart on purpose (§4a.1, and the whole reason this is
	// a slice plus an error rather than a slice alone): a nil error with no
	// entries is a MEASURED zero and renders "no overlapping path"; a nil error
	// with entries renders the count and names the first; a non-nil error
	// renders that the check could not run, carrying git's own line. "Nothing
	// overlaps" and "nobody looked" are different facts.
	overlap    []string
	overlapErr error
}

// readAdoptDivergence measures one racer's branch against the room as it stands
// now.
//
// The counts are load-bearing and the overlap is advisory, so they fail
// differently. A counts read that fails returns an error and /adopt refuses by
// name — the caller needs `ahead` for the zero-change refusal, and an
// unreadable ref rendered as a clean one is the exact defect this repository
// exists to prevent. An overlap read that fails is carried on the struct and
// the card still arms, because a broken advisory read must not brick a verb
// (arenaRaceNumber's rule, applied to a preview).
func readAdoptDivergence(workspace, branch string) (adoptDivergence, error) {
	var d adoptDivergence
	// `A...B` is the symmetric difference: the left count is what only A holds
	// and the right count is what only B holds. With the room on the left, left
	// is how far the racer is BEHIND and right is how far it is AHEAD. One
	// command for both, measured 2026-08-29 at git 2.55.0.windows.3, which
	// prints them tab-separated on one line.
	out, err := gitOut(workspace, "rev-list", "--left-right", "--count", "HEAD..."+branch)
	if err != nil {
		return d, err
	}
	f := strings.Fields(out)
	if len(f) != 2 {
		return d, gitError("rev-list answered " + strconv.Quote(out) + " rather than two counts")
	}
	if d.behind, err = strconv.Atoi(f[0]); err != nil {
		return d, gitError("rev-list answered " + strconv.Quote(out) + " rather than two counts")
	}
	if d.ahead, err = strconv.Atoi(f[1]); err != nil {
		return d, gitError("rev-list answered " + strconv.Quote(out) + " rather than two counts")
	}
	d.base = adoptBase(workspace)
	d.overlap, d.overlapErr = readAdoptOverlap(workspace, branch)
	return d, nil
}

// readAdoptOverlap is the paths both sides changed since their merge base.
//
// Two diffs against the same base, intersected. `HEAD...<branch>` is the
// incoming half — the merge-base..branch range `git merge` actually applies,
// and the same range adoptBlockers reads — and `<branch>...HEAD` is the room's
// own half over the identical base. A path in both is a path two people wrote
// after they agreed, which is the fact worth showing and the whole of what is
// claimed here.
//
// An empty half short-circuits the other read, because an intersection with an
// empty set is empty whichever way it is measured; that is arithmetic on a read
// value, not a skipped measurement.
func readAdoptOverlap(workspace, branch string) ([]string, error) {
	incoming, err := changedFiles(workspace, "HEAD..."+branch)
	if err != nil || len(incoming) == 0 {
		return nil, err
	}
	room, err := changedFiles(workspace, branch+"...HEAD")
	if err != nil || len(room) == 0 {
		return nil, err
	}
	in := make(map[string]bool, len(room))
	for _, f := range room {
		in[f] = true
	}
	var both []string
	for _, f := range incoming {
		if in[f] {
			both = append(both, f)
		}
	}
	return both, nil
}

// changedFiles is one `diff --name-only` over a range, split into paths — the
// one spelling of "which files does this range touch", shared by the overlap
// read and by adoptBlockers' untracked-collision check so the two can never
// disagree about what an adoption writes.
func changedFiles(workspace, rng string) ([]string, error) {
	out, err := gitOut(workspace, "--no-pager", "diff", "--name-only", rng)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// adoptBase names, for a reader, the ref the adoption is cut from and merged
// into: the branch when one is checked out, else the short commit.
//
// It is NOT roomRef, and the two must not be merged. roomRef's answer is handed
// back to `git checkout` after a failed adoption, so it has to be exact and
// carries the full sha; this one is read by a person on one status line, where
// forty hex characters would crowd out the sentence they are labelling. An
// empty answer is legal and the sentence names the baseline anyway.
func adoptBase(workspace string) string {
	if name, err := gitOut(workspace, "symbolic-ref", "--quiet", "--short", "HEAD"); err == nil && name != "" {
		return name
	}
	if short, err := gitOut(workspace, "rev-parse", "--short", "HEAD"); err == nil && short != "" {
		return short
	}
	return ""
}

// sentence is the preview as the card says it, given how many paths the racer's
// worktree still holds uncommitted.
//
// EVERY COUNT CARRIES ITS BASELINE. "3 ahead" alone is a number with no
// question attached, so the baseline opens the clause and both counts sit under
// it. When git could not name the ref, the baseline is still stated — "the
// room's HEAD" is true, and dropping the clause would leave the counts floating
// against nothing.
//
// THE UNCOMMITTED CLAUSE IS THE PREVIEW'S OWN LIMIT, stated rather than left to
// be discovered. Every figure here is read off COMMITTED state, and a racer
// whose worktree is still dirty has work that is not in any of it — the commit
// y makes is what puts it there. So a dirty racer's card says which counts it
// is outside of. Without that clause "no overlapping path" would be read as a
// claim about the whole merge, and TestAdoptConflictAbortsCleanly is the case
// where that reading is wrong: an uncommitted racer edit conflicts against a
// room commit the overlap read correctly reports as touching nothing shared.
func (d adoptDivergence) sentence(uncommitted int) string {
	base := d.base
	if base == "" {
		base = "the room's HEAD"
	}
	parts := []string{"vs " + base + ": " + itoa(d.ahead) + " ahead, " + itoa(d.behind) + " behind"}
	switch {
	case d.overlapErr != nil:
		parts = append(parts, "the overlap check could not run: "+d.overlapErr.Error())
	case len(d.overlap) == 0:
		parts = append(parts, "no overlapping path")
	default:
		n := len(d.overlap)
		parts = append(parts, itoa(n)+" overlapping "+plural(n, "path")+" ("+d.overlap[0]+")")
	}
	if uncommitted > 0 {
		parts = append(parts, "these counts exclude "+itoa(uncommitted)+
			" uncommitted "+plural(uncommitted, "path"))
	}
	return strings.Join(parts, " · ")
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

// hybridAdoptBranch names the branch a hybrid adoption lands on:
// `adopt/t<N>-<base>+<donor>`.
//
// THE NAME CARRIES BOTH SOURCES, and that is the naming decision (§9.37's
// 2026-08-29 hybrid amendment). The alternative was to keep `adopt/t<N>-<base>`
// and let the commit message carry the donor — which would leave one seat's name
// alone on a branch holding another seat's work, in the one place `git branch`
// shows a reader. The arena record derives everything it knows from these refs
// (record.go), so a hybrid whose ref looked like a whole adoption would be
// counted as one, and the base seat would be credited with an adoption it only
// half won.
//
// `+` is the joiner because it is legal in a ref name, it is not `-` (the
// collision suffix already owns that) and it is not `/` (the arena namespace
// owns that). parseHybridAdoptRef reads this spelling back, so the two must
// change together.
func hybridAdoptBranch(raceN int, base, donor model.VendorID) string {
	return adoptBranch(raceN, base) + "+" + string(donor)
}

// adoptBranchLimit caps the collision suffixes freeAdoptBranch will try. A repo
// holding 50 adoptions of one racer from one race is a state to report, not to
// keep counting through.
const adoptBranchLimit = 50

// freeAdoptBranch picks the name this adoption cuts: the caller's own spelling
// when nothing holds it, else the first free `-2`, `-3` … suffix.
//
// It takes the wanted NAME rather than a race and a seat, because there are two
// spellings now (adoptBranch and hybridAdoptBranch) and one scan answers both.
// A second collision loop over the same namespace would be the place the two
// forms start disagreeing about what "taken" means.
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
func freeAdoptBranch(workspace, name string) (string, error) {
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
//  5. A HYBRID ONLY: `git checkout arena/t<N>/<donor> -- <path...>` on the same
//     branch, then a commit whose message names both arena branches and every
//     path. `git checkout` is the smallest git operation that lands exactly the
//     chosen paths and nothing else, and it merges nothing — which is why the
//     card refuses every path a second writer touched before it arms. A failure
//     here restores exactly as a failed merge does: the branch is deleted and
//     the room goes back where it came from.
func (m *Model) adoptSeat(v model.VendorID, onto string, donor model.VendorID, paths []string) string {
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
	if donor != "" && len(paths) == 0 {
		// Unreachable from the gate, which refuses an empty path list before it
		// arms. A hybrid with no paths is a whole adoption wearing a hybrid's
		// branch name, and the name would then lie to the arena record.
		return "adopt: a hybrid names the paths it takes — nothing was merged"
	}
	donorBranch := ""
	if donor != "" {
		donorTree, ok := race.trees[donor]
		if !ok {
			return string(donor) + " has no kept worktree from race t" + itoa(race.raceN)
		}
		donorBranch = arenaBranch(race.raceN, donor)
		// The donor's attempt has to be a commit before a checkout can read it,
		// for the reason the base racer's does: arena seats leave their work
		// uncommitted. Committed FIRST, before anything moves the room, so a
		// failure here leaves the room untouched.
		if why := commitRacerAttempt(donorTree, donorBranch, donor, race.raceN); why != "" {
			return why
		}
	}
	if onto == "" {
		// Unreachable from the gate, which resolves the name when it arms. Kept
		// so a future caller cannot land an adoption on an unnamed branch — the
		// same defensive shape the nil-race check above keeps.
		want := adoptBranch(race.raceN, v)
		if donor != "" {
			want = hybridAdoptBranch(race.raceN, v, donor)
		}
		var err error
		if onto, err = freeAdoptBranch(race.workspace, want); err != nil {
			return "adopt: " + err.Error()
		}
	}

	if why := commitRacerAttempt(tree, branch, v, race.raceN); why != "" {
		return why
	}

	roomDirty, err := adoptBlockers(race.workspace, branch, paths)
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

	if donor != "" {
		if why := takeDonorPaths(race.workspace, from, onto, donorBranch, branch, donor, race.raceN, paths); why != "" {
			return why
		}
	}

	// The next command is named because the adoption is deliberately NOT the
	// end of the hand-off: publishing is the operator's act, as it is for every
	// other commit (§9.37's founding posture), and the branch this now stands on
	// is what makes that act one command instead of four.
	//
	// A hybrid names both seats here too. The notice is the last place the room
	// speaks about this adoption, and a sentence saying only "adopted claude"
	// over a branch holding codex's file would be the room hiding a mixed
	// provenance at the one moment the operator is still looking at it.
	took := ""
	drops := " · /arena drop " + string(v) + " removes its worktree"
	if donor != "" {
		took = ", with " + itoa(len(paths)) + " " + plural(len(paths), "path") +
			" from " + string(donor) + " (" + strings.Join(paths, ", ") + ")"
		drops = " · /arena drop " + string(v) + " and /arena drop " + string(donor) +
			" remove their worktrees"
	}
	return "adopted " + string(v) + " onto " + onto + took +
		" — gh pr create opens the PR" + drops
}

// commitRacerAttempt commits one racer's worktree onto its own arena branch, or
// returns the refusal sentence. Empty means the tree was already clean or the
// commit landed.
//
// Lifted out of adoptSeat when the hybrid gave it a second caller. Signing and
// author identity come from the user's own git config, unmodified: an adopt
// commit enters the room's history, and council inventing an identity or skipping
// the user's signing rule there would be the room writing history that misstates
// its provenance.
func commitRacerAttempt(tree, branch string, v model.VendorID, raceN int) string {
	dirty, err := worktreePorcelain(tree)
	if err != nil {
		return "adopt: " + err.Error()
	}
	if len(dirty) == 0 {
		return ""
	}
	if _, err := gitOut(tree, "add", "-A"); err != nil {
		return "adopt: " + err.Error()
	}
	if _, err := gitOut(tree, "commit", "-m", string(v)+"'s arena attempt, race t"+itoa(raceN)); err != nil {
		return "adopt: the attempt could not be committed to " + branch + " — " + err.Error()
	}
	return ""
}

// takeDonorPaths is the hybrid's second half: the chosen paths, checked out of
// the donor's arena branch onto the adoption branch and committed with a receipt
// that names where each half came from. Empty means it landed.
//
// THE RECEIPT IS THE HONESTY OF THIS FEATURE. A hybrid adoption's history holds
// two sources, and the one place a reader meets it a year later is `git log`. So
// the message names both arena branches, lists every path taken, and says what
// council refused to do — take a path two seats wrote. Nothing in it is inferred:
// the branches and the paths are the same strings the card showed before `y`.
//
// A FAILURE RESTORES EXACTLY AS A FAILED MERGE DOES. The `reset --hard` before
// the restore is bounded and it is not a general-purpose escape: the room tree
// was measured clean before the branch was cut, the branch is about to be
// deleted, and the only content it can discard is the half-checked-out copy of
// files that exist whole on the donor's own branch. Without it, `git checkout`
// back to the room's branch would refuse over the very paths this step staged.
func takeDonorPaths(workspace, from, onto, donorBranch, baseBranch string, donor model.VendorID, raceN int, paths []string) string {
	args := append([]string{"checkout", donorBranch, "--"}, paths...)
	if _, err := gitOut(workspace, args...); err != nil {
		return "the paths could not be taken from " + donorBranch + ": " + err.Error() +
			" — the merge is discarded and " + undoHybrid(workspace, from, onto)
	}

	var b strings.Builder
	b.WriteString("adopt race t" + itoa(raceN) + ": " + baseBranch + " whole, plus " +
		itoa(len(paths)) + " " + plural(len(paths), "path") + " from " + donorBranch + "\n\n")
	b.WriteString("the merge below this commit carries " + baseBranch + " whole. this commit\n" +
		"adds the paths that came from " + donorBranch + ", and it adds nothing else:\n\n")
	for _, p := range paths {
		b.WriteString("  " + p + "\n")
	}
	b.WriteString("\ntelltale council took no path that both seats wrote. a shared path is\n" +
		"refused by name, and the operator merges it.\n")

	if _, err := gitOut(workspace, "commit", "-m", b.String()); err != nil {
		return "the paths from " + string(donor) + " could not be committed: " + err.Error() +
			" — the merge is discarded and " + undoHybrid(workspace, from, onto)
	}
	return ""
}

// undoHybrid drops whatever the failed second half staged and then restores the
// room the way every other failed adoption restores it.
func undoHybrid(workspace, from, onto string) string {
	_, _ = gitOut(workspace, "reset", "--hard")
	return undoAdoptBranch(workspace, from, onto)
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
		dirty, err := worktreePorcelain(tree)
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
func worktreePorcelain(dir string) ([]string, error) {
	out, err := gitOut(dir, "status", "--porcelain")
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
//
// `extra` is what the adoption writes BESIDES the merge — a hybrid's chosen
// paths, nil for a whole adoption. It joins the branch's own file list rather
// than getting a check of its own, because "what this adoption writes" has to
// have exactly one answer.
func adoptBlockers(workspace, branch string, extra []string) ([]string, error) {
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
		incoming, err := changedFiles(workspace, "HEAD..."+branch)
		if err != nil {
			return nil, err
		}
		incoming = append(incoming, extra...)
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
//
// This is the drop guard's reader. /adopt takes the identical figure off
// readAdoptDivergence's wider command (`adoptDivergence.ahead`), which answers
// the card's `behind` in the same call rather than paying for a second one.
func unadoptedCount(race *arenaRace, v model.VendorID) (int, error) {
	out, err := gitOut(race.workspace, "rev-list", "--count", "HEAD.."+arenaBranch(race.raceN, v))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(out)
}
