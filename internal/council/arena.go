package council

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// /arena: one brief raced across every seat, each in its own git worktree,
// compared by diff instead of by prose. Ruled 2026-08-08 (STATE.md; the design
// record is §9.37): per-turn isolation typed at the room, worktrees kept until
// the user deletes them, comparison lands in-column as `git diff --stat` with
// the full diff yankable.
//
// Every attempt is a FRESH vendor session, and that is a design position, not a
// shortcut. The three products nearest this feature (claudexor's best-of-N
// races, Crystal's same-prompt sessions, parallel-code's AI Arena) all race
// fresh attempts; a continued thread would anchor each seat on its own prior
// answers, which is the opposite of a race — and whether any vendor's resume
// even survives a cwd change is measured for none of the spawn-per-turn three.
// So arena turns never read m.sessions, never write it (dispatch guards the
// capture), and never touch the persistent seat's live process: the room's
// conversations are exactly as they were when the race ends.
//
// What council deliberately does NOT do here, having read the competition:
// claudexor AUTO-ADOPTS the winning patch into the live tree. This room offers
// the diffs and the human picks — the same rule as the figure that offers the
// jump (portfolio ADR-010) and §9.2's independent answers. Adoption is a git
// command printed for the user, never an action taken for them.

// arenaDiffBudget caps the full diff held in memory for yanking, per seat.
//
// A megabyte is far past anything a reader pastes into a review and exists so a
// seat that rewrote a vendored tree cannot balloon the model. Truncation is
// marked on the result and stated in the yank notice, never silent.
const arenaDiffBudget = 1 << 20

// ArenaResult is one seat's race outcome: where its attempt lives and what it
// changed. Held on the Column so Render stays pure over State — every field is
// computed once, at collection, never during a frame.
type ArenaResult struct {
	// Tree is the worktree's absolute path; Branch the branch the attempt is
	// parked on. Both are the receipt: the worktree is KEPT, and these two
	// strings are what the user needs to visit, adopt, or delete it.
	Tree   string
	Branch string
	// Base is the commit every attempt started from — one SHA recorded before
	// any seat spawned, so all four diffs answer against the same line. Diffing
	// against HEAD instead would let a seat that commits mid-turn show an empty
	// diff (measured shape in claude-squad's session/git/diff.go, which anchors
	// on a recorded base SHA for exactly this reason).
	Base string
	// Stat is `git diff <base> --stat`, verbatim. Empty means a measured
	// "no changes" — the seat ran and touched nothing — which renders as its own
	// sentence rather than as an absent block (§4a.1: zero and absent differ).
	Stat string
	// Diff is the full patch, capped at arenaDiffBudget.
	Diff          string
	DiffTruncated bool
	// Err is the one-line reason collection failed, when it did. A diff that
	// could not be read is reported, never rendered as "no changes" — that
	// collapse is the degraded-vs-zero bug §4a.1 exists to prevent.
	Err string

	// Commit is the sha that makes this attempt durable on Branch — exactly
	// what `git rev-parse HEAD` returned after the turn's commit landed, never
	// a value council derived (§9.37, amended 2026-08-09; the mechanic is
	// Crystal's commit-per-turn). Empty on a zero-diff attempt BY RULING: an
	// empty commit would be a receipt claiming work that did not happen — the
	// false nonzero, §4a.1's bug pointed the other way — so nothing is
	// committed and the "no changes" sentence stays the whole story.
	Commit string
	// CommitErr names why a commit that WAS owed could not be made (no
	// identity anywhere and the fallback failed, a signer that cannot run, a
	// tree that vanished mid-turn). It degrades this seat's receipt to
	// worktree-only — the attempt is still on disk, just not parked on the
	// branch — and it never aborts the race or touches the other racers: a
	// partial read degrades a field, not the row, and the same rule holds for
	// a partial write.
	CommitErr string
	// Undone records that the user took this attempt back (`u`): worktree and
	// branch were reset to Base. The Stat/Diff above it deliberately survive —
	// they are the measured record of what the attempt changed and the room
	// does not destroy its reading surface — but the block says the tree no
	// longer holds it, and a second undo is refused as already done rather
	// than re-run as a no-op that pretends to act.
	Undone bool

	// Rank is the order the ROOM saw this attempt land (1-based), Of how many
	// raced. Host-observed finish order, per the host-stamps rule: a vendor's
	// own claim about when it finished is an inferred value wearing measured
	// clothes, so the only clock that ranks a race is the room's. Zero means
	// unranked — a fixture a test built by hand, rendered as absent, not first.
	Rank, Of int

	// RaceN is the race number every name on this result was minted with —
	// the t<N> in Branch, Tree and the commit subject. Recorded from the
	// turn's own numbering (turnState.arenaRaceN) because it is NOT always
	// the column's TurnN: the race numbers itself past leftovers from older
	// rooms (arenaRaceNumber), so anything that re-derives a name from this
	// result — undoSeat's path guard is the consumer — must re-derive with
	// THIS number, or a race that outran its turn becomes unguardable.
	RaceN int

	// Seed is this seat's .worktreeinclude receipt, nil when the room repo has
	// no .worktreeinclude at all. The render draws NOTHING for nil and
	// "seeded 0 files" for a report that copied nothing — a repo that never
	// asked and a pattern file whose patterns found nothing are different
	// facts, and collapsing them is the zero-vs-absent bug (§4a.1).
	Seed *SeedReport
}

// SeedReport is what .worktreeinclude seeding measurably did for one seat.
// Files is the count actually COPIED into this seat's tree — never the
// pattern file's ambitions — per the rule that a displayed value comes from
// measured output. Notices carry every pattern-level fact worth saying out
// loud: the named refusals (absolute, `..`, negation, malformed), symlinks
// not followed, and each pattern that matched nothing. A no-match is a
// notice rather than silence because a .worktreeinclude is an
// allowlist-shaped file, and an allowlist must fail visibly on a stale
// entry, not only on a violation — but it is a notice rather than a failure,
// because a pattern for a file this clone happens to lack must not kill the
// race.
type SeedReport struct {
	Files   int
	Notices []string
}

// gitOut runs one git command with plain argv — never a shell (§9.3's rule
// applies to every process council starts, not only vendors) — and returns
// trimmed stdout. Errors carry the stderr line git itself marked as the
// problem (gitErrLine), which is the sentence the notice shows.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := gitErrLine(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", gitError(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// gitErrLine picks the one stderr line a failed git command's error carries:
// the first line git itself marks as the problem — its own `fatal:` / `error:`
// prefixes — falling back to the first non-empty line only when no marked line
// exists (some refusals, like `worktree remove` on a dirty tree, print bare
// prose).
//
// The preference exists because "first line" was measured lying (2026-08-09,
// live /arena race, Windows box). When the arena branch already existed,
// `git worktree add ... -b arena/t3/claude` wrote TWO stderr lines and exited
// nonzero:
//
//	Preparing worktree (new branch 'arena/t3/claude')
//	fatal: a branch named 'arena/t3/claude' already exists
//
// The old rule surfaced the first, so all four columns reported
// "arena: Preparing worktree (new branch ...)" as the reason they failed —
// progress chatter wearing the error's clothes, with the actual fatal line
// swallowed. git's prefixes are the measured marker of which line is the
// error; anything printed before them is narration (§4a.1: the displayed
// value must be the measured failure, not the nearest string to it).
func gitErrLine(stderr string) string {
	first := ""
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "fatal:") || strings.HasPrefix(line, "error:") {
			return line
		}
		if first == "" {
			first = line
		}
	}
	return first
}

type gitError string

func (e gitError) Error() string { return string(e) }

// arenaTree names one attempt's worktree: a SIBLING of the workspace,
// `<repo>-arena-t<N>-<vendor>` where N is the RACE number (arenaRaceNumber —
// the turn number until leftovers from an older room push it past), matching
// the README's own worktree convention (`git worktree add ../telltale-council`).
// Sibling rather than a directory under ~/.telltale because kept-until-deleted
// means the user must SEE what is kept — a receipt hidden in a state directory
// is a receipt nobody reads — and because /cd already resolves siblings by
// name, so `/cd telltale-arena-t7-codex` works with zero new code.
func arenaTree(workspace string, raceN int, v model.VendorID) string {
	name := filepath.Base(workspace) + "-arena-t" + itoa(raceN) + "-" + string(v)
	return filepath.Join(filepath.Dir(workspace), name)
}

func arenaBranch(raceN int, v model.VendorID) string {
	return "arena/t" + itoa(raceN) + "/" + string(v)
}

// arenaRaceNumber reads the number the next race must clear: one past the
// highest N among the workspace's existing arena/t<N>/... branches, floored
// at the room's own turn number (a repo with no arena refs races as t<turn>,
// the original naming, unchanged).
//
// READ from the refs, never guessed from the turn counter, because the two
// lifetimes disagree (§9.37, amended 2026-08-09): arena branches and
// worktrees are KEPT until the user deletes them, while the room's turn
// counter — and the in-memory race receipt /arena drop needs — reset with
// every launch. On the first live race to cross a relaunch, a fresh room's
// turn 3 collided with an older room's t3 leftovers: every seat failed at
// worktree add, and /arena drop could not reach the old trees because their
// receipt (Model.lastRace) had died with the old room — the only remedy was
// hand-run git. The refs are the one record that shares the leftovers'
// lifetime, so the refs are what numbers the race. for-each-ref over the
// arena/ namespace, argv through gitOut like every other git call here.
//
// A scan that cannot run degrades to the turn-number floor WITH THE RACE
// RUNNING — a broken for-each-ref must not brick /arena. The worst outcome
// of the floor is the collision itself, which the caller now reports with
// git's own fatal line (gitErrLine) instead of progress chatter: degraded
// numbering costs a named per-seat failure, never a silent one.
func arenaRaceNumber(workspace string, turn int) int {
	n := turn
	out, err := gitOut(workspace, "for-each-ref", "--format=%(refname:short)", "refs/heads/arena/")
	if err != nil {
		return n
	}
	for _, ref := range strings.Split(out, "\n") {
		rest, ok := strings.CutPrefix(ref, "arena/t")
		if !ok {
			continue
		}
		num, _, ok := strings.Cut(rest, "/")
		if !ok {
			continue
		}
		if k, aerr := strconv.Atoi(num); aerr == nil && k >= n {
			n = k + 1
		}
	}
	return n
}

// arenaSetup records the base, numbers the race against the refs the repo
// already holds, adds one worktree per racing seat, and seeds each worktree
// from the room repo's .worktreeinclude when one exists. raceN is the number
// every name this race mints carries — branch, tree, commit subject — and
// the caller must record it (turnState, the race receipt) so adopt, drop and
// undo re-derive the same names.
//
// The base is read ONCE, before any worktree exists, so every attempt races
// from the same commit — and the seed plan is read once for the same reason:
// every racer is offered the same bytes, so per seat only the copy itself can
// differ. Per-seat failures (a worktree that could not be added, a seed copy
// that failed) skip that seat (reported on its column) rather than aborting
// the race — a partial read degrades a field, not the row, and the same rule
// holds one level up.
func arenaSetup(workspace string, turn int, seats []model.VendorID) (raceN int, base string, trees map[model.VendorID]string, seeds map[model.VendorID]*SeedReport, seatErr map[model.VendorID]string, err error) {
	base, err = gitOut(workspace, "rev-parse", "HEAD")
	if err != nil {
		return 0, "", nil, nil, nil, err
	}
	raceN = arenaRaceNumber(workspace, turn)
	plan := loadSeedPlan(workspace, seedBudgetBytes)
	trees = map[model.VendorID]string{}
	seeds = map[model.VendorID]*SeedReport{}
	seatErr = map[model.VendorID]string{}
	for _, v := range seats {
		tree := arenaTree(workspace, raceN, v)
		if _, werr := gitOut(workspace, "worktree", "add", "-b", arenaBranch(raceN, v), tree, base); werr != nil {
			why := werr.Error()
			if strings.Contains(why, "already exists") {
				// Something still claimed this name despite the ref scan — a
				// sibling directory an old room left with no branch to be
				// scanned, or a ref minted between scan and add. That is an
				// older room's leftover, the receipt that could reach it is
				// gone (arenaRaceNumber's doc), so the remedy is named here:
				// hand-run git is the one tool that still reaches it.
				why += " — an older race's leftover; git worktree remove / git branch -D clears it"
			}
			seatErr[v] = why
			continue
		}
		if plan != nil {
			n, cerr := plan.copyInto(workspace, tree)
			if cerr != nil {
				// A copy error degrades THIS seat only, with the failed path
				// and the error's first line as the reason. The seat does not
				// race: a tree the room KNOWS is half-seeded would fail the
				// brief for the exact reason seeding exists to remove, and a
				// false failure wearing a vendor's name is worse than a named
				// skip. The worktree itself stays on disk — kept-until-deleted
				// receipts include the broken ones.
				seatErr[v] = "seeding failed: " + cerr.Error()
				continue
			}
			seeds[v] = &SeedReport{Files: n, Notices: plan.notices}
		}
		trees[v] = tree
	}
	return raceN, base, trees, seeds, seatErr, nil
}

// collectArena reads what one attempt changed, against the recorded base.
//
// `git add -N .` (intent-to-add) runs first so files the seat CREATED appear in
// the diff — without it a new-file-only answer reads as "no changes", which is
// a false zero. The -N entries are index-only markers; nothing is committed and
// the worktree's content is untouched. (Mechanism read from claude-squad's
// session/git/diff.go, verified against git's own documentation of --intent-to-add.)
func collectArena(tree, base string) ArenaResult {
	r := ArenaResult{Tree: tree, Base: base}
	if _, err := gitOut(tree, "add", "-N", "."); err != nil {
		r.Err = "diff unavailable: " + err.Error()
		return r
	}
	stat, err := gitOut(tree, "--no-pager", "diff", base, "--stat")
	if err != nil {
		r.Err = "diff unavailable: " + err.Error()
		return r
	}
	r.Stat = stat
	diff, err := gitOut(tree, "--no-pager", "diff", base)
	if err != nil {
		r.Err = "diff unavailable: " + err.Error()
		return r
	}
	if len(diff) > arenaDiffBudget {
		cut := arenaDiffBudget
		for cut > 0 && !utf8Start(diff[cut]) {
			cut--
		}
		diff, r.DiffTruncated = diff[:cut], true
	}
	r.Diff = diff
	return r
}

// arenaCommitMsg names one attempt's commit from the race that produced it:
// "arena t<N>: <first line of the brief>", N the recorded race number — the
// same N the branch carries, so the subject and the ref it lands on can never
// disagree. First line only and capped at 64 bytes (cut on a rune boundary),
// because the subject line is a label for `git log --oneline` over a kept
// branch, not a second copy of the brief — the brief itself is in the
// transcript and in the commit the user adopts.
func arenaCommitMsg(raceN int, brief string) string {
	line := brief
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	const subjectCap = 64
	if len(line) > subjectCap {
		cut := subjectCap
		for cut > 0 && !utf8Start(line[cut]) {
			cut--
		}
		line = strings.TrimRight(line[:cut], " ") + "…"
	}
	if line == "" {
		return "arena t" + itoa(raceN)
	}
	return "arena t" + itoa(raceN) + ": " + line
}

// commitArena parks one attempt's tree state on its arena branch, and returns
// the resulting tip as `git rev-parse HEAD` reported it — the one sha the
// column may render, because it is read back rather than inferred (§4a.1).
//
// Staging everything (`add -A`) is correct HERE and only here: the worktree
// contains nothing but the racer's own output, so there is no bystander state
// to sweep up — the reason blanket staging is wrong in a real workspace does
// not exist in this one. Argv through gitOut, never a shell (§9.3).
//
// A racer that committed for ITSELF mid-turn leaves a clean tree ahead of
// base; its own tip is the durable receipt and is reported as such rather
// than papered over with an empty commit. And the commit honors the repo's
// own config — including a configured signer — on purpose: a -c override that
// silently skipped signing would park an unsigned commit on a machine whose
// owner asked for signed ones. A signer (or anything else) that cannot run
// fails this seat's commit with git's own first stderr line, which the
// column then carries as a named degradation.
func commitArena(tree, base, msg string) (string, error) {
	if _, err := gitOut(tree, "add", "-A"); err != nil {
		return "", err
	}
	dirty, err := gitOut(tree, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if dirty == "" {
		head, err := gitOut(tree, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		if head != base {
			// The attempt's own mid-turn commit already parked it.
			return head, nil
		}
		// Clean at base: nothing owed. The caller skips the zero-diff case
		// before calling; this is the belt to that suspender.
		return "", nil
	}
	args := arenaIdentity(tree)
	args = append(args, "commit", "-m", msg)
	if _, err := gitOut(tree, args...); err != nil {
		return "", err
	}
	return gitOut(tree, "rev-parse", "HEAD")
}

// arenaIdentity supplies a committer identity ONLY where the machine has
// none — CI runners and fresh boxes, where `git commit` otherwise refuses
// and every race would degrade on arrival. Per-command `-c` flags rather
// than `git config`, because a worktree shares its repo's config file: a
// config write "inside the racer worktree" would actually land in the
// room's repo, which is the one thing arena is forbidden to touch. Each
// half is checked separately so a machine with a real name and no email
// (or the reverse) keeps the half it has.
func arenaIdentity(tree string) []string {
	var args []string
	if _, err := gitOut(tree, "config", "user.name"); err != nil {
		args = append(args, "-c", "user.name=telltale arena")
	}
	if _, err := gitOut(tree, "config", "user.email"); err != nil {
		args = append(args, "-c", "user.email=arena@telltale.invalid")
	}
	return args
}

// undoArena takes one attempt back: `git reset --hard <base>` inside the
// racer worktree ONLY (§9.37, amended 2026-08-09; the shape is cc-haha's
// turn-level undo). Branch and tree agree by construction rather than by a
// second command: the worktree has arena/t<N>/<vendor> checked out, so
// --hard moves that ref itself — there is no window where the branch says
// one thing and the tree another. Everything the attempt produced is
// tracked or staged by the time this can run (collectArena's add -N, then
// commitArena's add -A), so the reset removes created files too instead of
// stranding them as untracked survivors. The caller owns the guard that
// tree really is an arena worktree this room made this turn; this function
// deliberately does nothing to make it safe to point elsewhere.
func undoArena(tree, base string) error {
	_, err := gitOut(tree, "reset", "--hard", base)
	return err
}

// ---------------------------------------------------------------------------
// .worktreeinclude: a race carries the files git ignores, when the repo
// names them.
//
// `git worktree add` gives every racer a CLEAN checkout, which is the whole
// point — and also the trap: the files a project needs to RUN that git
// deliberately does not carry (.env, local config, untracked fixtures) are
// absent from a fresh worktree, so the first real race on any repo that needs
// them fails falsely, on every seat at once. §9.37 deferred exactly this and
// predicted where it would surface ("the first real arena run on a repo
// needing .env"); this is that deferral, landed.
//
// The mechanism is agent-deck's .worktreeinclude — HALF of it, on purpose.
// agent-deck pairs seeding with repo-carried setup scripts that run after the
// copy, and that half is deliberately NOT taken: copying bytes into a tree
// the room already owns is containable, but EXECUTING content the repo
// carries crosses a trust boundary this project has explicitly parked
// (byte-level trust gating is on the parked list pending an audit — §9.37).
// A repo that could run code on the machine by merely containing a file is a
// different product with a different threat model. Copy only, never execute.
//
// Candidates are `git ls-files --others` — untracked files only, and that
// restriction is itself an honesty rule: a tracked file already arrives with
// the checkout, so seeding the room's possibly-DIRTY copy of it would plant
// the room's own edits in every seat's diff — a lying diff, the exact class
// §4a.1 exists to prevent. Known limit, stated rather than hidden: a seeded
// file that is untracked but NOT git-ignored still surfaces in the seat's
// diff through collection's `git add -N .`; name git-ignored files in
// .worktreeinclude and that cannot happen, because add refuses ignored paths.
//
// Containment: patterns resolve from the repo root and can never reach past
// it — matches come FROM the ls-files enumeration (structurally inside the
// root), and absolute or `..`-carrying patterns are refused by name before
// they match anything. Symlinks are never followed: Windows is the primary
// target (ADR-002) and symlink semantics differ per platform, so a symlink
// match copies nothing and says so.

// seedFileName is read from the room repo's root only — not from parents,
// not from the racer trees.
const seedFileName = ".worktreeinclude"

// seedBudgetBytes caps the total bytes seeded into each racer's tree, 64 MiB.
//
// The budget exists for the node_modules pattern: one over-broad line in
// .worktreeinclude must fail loud and named, not hang the room copying a
// dependency tree into four worktrees. 64 MiB is far past any honest .env /
// config / fixture set while staying small enough that even the worst case —
// four seats, all refused at the cap mid-copy — costs seconds, not minutes.
// The plan refuses over-budget up front (nothing copied, reason named), and
// the copy enforces it again on ACTUAL bytes, because a file can grow between
// the plan's stat and the copy.
const seedBudgetBytes int64 = 64 << 20

// seedFile is one planned copy: a slash-separated repo-relative path, with
// the size and mode the plan measured.
type seedFile struct {
	rel  string
	size int64
	mode os.FileMode
}

// seedPlan is the room repo's .worktreeinclude, resolved once per race:
// which untracked files the patterns name, and every per-pattern fact the
// column must state (refusals, no-matches, symlinks, the budget verdict).
// nil plan = no .worktreeinclude, which renders as nothing at all.
type seedPlan struct {
	files   []seedFile
	notices []string
	budget  int64
}

// loadSeedPlan reads workspace/.worktreeinclude and resolves it against the
// repo's untracked files. Returns nil when the file does not exist — absence
// is a different fact from a file that matched nothing, and nil is how the
// render keeps them apart. Every other problem (unreadable file, refused
// pattern, no-match pattern, symlink, over-budget total) is a NOTICE on the
// plan, never an error: the plan degrades and says so, the race runs.
//
// budget is a parameter rather than a read of seedBudgetBytes so the refusal
// path is testable without a 64 MiB fixture; every non-test caller passes the
// constant.
func loadSeedPlan(workspace string, budget int64) *seedPlan {
	raw, err := os.ReadFile(filepath.Join(workspace, seedFileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return &seedPlan{budget: budget, notices: []string{seedFileName + " unreadable: " + firstLine(err.Error())}}
	}
	plan := &seedPlan{budget: budget}
	var patterns []string
	for _, line := range strings.Split(string(raw), "\n") {
		p := strings.TrimSpace(line)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		if why := refuseSeedPattern(p); why != "" {
			plan.notices = append(plan.notices, `refused "`+p+`": `+why)
			continue
		}
		patterns = append(patterns, p)
	}
	if len(patterns) == 0 {
		return plan
	}

	// The candidate set: every untracked file, ignored ones included — which
	// is exactly the set a fresh worktree lacks. git does the walking, so the
	// enumeration cannot leave the repo root and never descends into .git.
	others, err := gitOut(workspace, "ls-files", "--others", "-z")
	if err != nil {
		plan.notices = append(plan.notices, "seeding unavailable: "+err.Error())
		return plan
	}
	var candidates []string
	for _, c := range strings.Split(others, "\x00") {
		if c != "" {
			candidates = append(candidates, filepath.ToSlash(c))
		}
	}

	seen := map[string]bool{}
	var total int64
	for _, p := range patterns {
		matched := false
		for _, rel := range candidates {
			if !seedMatch(p, rel) {
				continue
			}
			matched = true
			if seen[rel] {
				continue
			}
			seen[rel] = true
			fi, lerr := os.Lstat(filepath.Join(workspace, filepath.FromSlash(rel)))
			switch {
			case lerr != nil:
				plan.notices = append(plan.notices, "not copied: "+rel+": "+firstLine(lerr.Error()))
			case fi.Mode()&os.ModeSymlink != 0:
				plan.notices = append(plan.notices, "symlink not copied: "+rel)
			case !fi.Mode().IsRegular():
				plan.notices = append(plan.notices, "not a regular file, not copied: "+rel)
			default:
				plan.files = append(plan.files, seedFile{rel: rel, size: fi.Size(), mode: fi.Mode().Perm()})
				total += fi.Size()
			}
		}
		if !matched {
			// A stale pattern is visible, not silent — but it is a fact, not
			// a failure. This clone simply holds nothing the pattern names.
			plan.notices = append(plan.notices, `no untracked file matches "`+p+`"`)
		}
	}
	if total > budget {
		// Refused wholesale rather than truncated at the cap: seeding SOME of
		// what the file names would hand every seat a tree that half-works,
		// and which half would depend on ls-files ordering. Nothing is
		// copied, the reason carries the measured total, and the race runs —
		// unseeded, and saying so.
		plan.files = nil
		plan.notices = append(plan.notices,
			"seeding refused: matches total "+seedSize(total)+", past the "+seedSize(budget)+" budget")
	}
	return plan
}

// refuseSeedPattern names why a pattern may not run at all, or returns ""
// for a runnable one. Refusals are per-pattern and by name so one bad line
// cannot silently disable — or worse, silently widen — the rest of the file.
func refuseSeedPattern(p string) string {
	if strings.HasPrefix(p, "!") {
		return "negation is not supported"
	}
	// Leading separator, drive letter, or anything the host calls absolute:
	// all refused as one class. Gitignore spells root-anchoring with a
	// leading slash, and this file deliberately does not — patterns resolve
	// from the repo root already, and an absolute-looking pattern is more
	// often an attempt (or an accident) aimed outside the repo than an
	// anchor. The refusal says where patterns resolve from, so the fix is in
	// the sentence.
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, "\\") || filepath.IsAbs(p) || filepath.VolumeName(p) != "" {
		return "absolute — patterns resolve from the repo root"
	}
	for _, seg := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if seg == ".." {
			return "may not reach above the repo root"
		}
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, "probe"); err != nil {
			return "malformed pattern"
		}
	}
	return ""
}

// seedMatch reports whether one pattern names one untracked file, both
// slash-separated and repo-root-relative. The grammar is a documented subset
// of gitignore's: a pattern without a slash matches its name at any depth
// (file or directory — matching a directory seeds everything under it); a
// pattern with a slash anchors at the repo root, `*` stays within one path
// segment (path.Match), `**` spans segments, and a trailing slash means the
// directory and its contents. No negation — refuseSeedPattern already turned
// `!` away by name, because half a negation grammar is worse than none.
func seedMatch(pattern, rel string) bool {
	segs := strings.Split(rel, "/")
	if !strings.Contains(pattern, "/") {
		for _, seg := range segs {
			if ok, _ := path.Match(pattern, seg); ok {
				return true
			}
		}
		return false
	}
	pat := strings.Split(strings.Trim(pattern, "/"), "/")
	// The file itself, or any ancestor directory: a pattern that names a
	// directory seeds the whole subtree, matching gitignore's own reading.
	for n := len(segs); n >= 1; n-- {
		if seedSegsMatch(pat, segs[:n]) {
			return true
		}
	}
	return false
}

// seedSegsMatch matches pattern segments against path segments, `**`
// consuming zero or more. Inputs are pattern-file lines and repo paths —
// both short — so the recursion is bounded by hand-written input, not data.
func seedSegsMatch(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		if seedSegsMatch(pat[1:], name) {
			return true
		}
		return len(name) > 0 && seedSegsMatch(pat, name[1:])
	}
	if len(name) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], name[0]); err != nil || !ok {
		return false
	}
	return seedSegsMatch(pat[1:], name[1:])
}

// copyInto seeds one racer's tree, creating parent directories as the
// relative paths demand, and returns how many files actually landed — the
// number the column states. The first error stops THIS tree and is returned
// with its path and first line; the caller degrades that one seat and the
// other racers never hear about it.
func (p *seedPlan) copyInto(workspace, tree string) (int, error) {
	var total int64
	copied := 0
	for _, f := range p.files {
		src := filepath.Join(workspace, filepath.FromSlash(f.rel))
		dst := filepath.Join(tree, filepath.FromSlash(f.rel))
		n, err := seedCopyFile(src, dst, f.mode, p.budget-total)
		total += n
		if err != nil {
			return copied, errors.New(f.rel + ": " + firstLine(err.Error()))
		}
		copied++
	}
	return copied, nil
}

// seedCopyFile copies one regular file, refusing to write more than room
// bytes — the budget re-enforced on ACTUAL bytes, because the plan's sizes
// are a stat from moments ago and a log file grows. The re-Lstat closes the
// same gap for symlinks: a path that became a link since the plan is refused
// here rather than followed.
func seedCopyFile(src, dst string, mode os.FileMode, room int64) (int64, error) {
	fi, err := os.Lstat(src)
	if err != nil {
		return 0, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("became a symlink; not followed")
	}
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	// LimitReader at room+1: reading one byte past the room is how "over
	// budget" is detected without ever copying unboundedly.
	n, cerr := io.Copy(out, io.LimitReader(in, room+1))
	if err := out.Close(); cerr == nil {
		cerr = err
	}
	if n > room {
		return n, errors.New("grew past the seeding budget mid-copy")
	}
	return n, cerr
}

// seedSize prints a byte count for a refusal sentence: whole MiB rounded UP
// past a mebibyte (a budget refusal must never understate what it measured),
// raw bytes below one. itoa over fmt, matching the rest of the render path.
func seedSize(n int64) string {
	if n >= 1<<20 {
		return itoa(int((n+(1<<20-1))>>20)) + " MiB"
	}
	return itoa(int(n)) + " B"
}

// firstLine trims a multi-line error to the sentence a column can carry —
// the same shape gitOut already gives git's own failures.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
