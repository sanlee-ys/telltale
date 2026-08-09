package council

import (
	"os/exec"
	"path/filepath"
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
}

// gitOut runs one git command with plain argv — never a shell (§9.3's rule
// applies to every process council starts, not only vendors) — and returns
// trimmed stdout. Errors carry git's own first stderr line, which is the
// sentence the notice shows.
func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = msg[:i]
		}
		if msg == "" {
			msg = err.Error()
		}
		return "", gitError(msg)
	}
	return strings.TrimSpace(out.String()), nil
}

type gitError string

func (e gitError) Error() string { return string(e) }

// arenaTree names one attempt's worktree: a SIBLING of the workspace,
// `<repo>-arena-t<turn>-<vendor>`, matching the README's own worktree
// convention (`git worktree add ../telltale-council`). Sibling rather than a
// directory under ~/.telltale because kept-until-deleted means the user must
// SEE what is kept — a receipt hidden in a state directory is a receipt nobody
// reads — and because /cd already resolves siblings by name, so
// `/cd telltale-arena-t7-codex` works with zero new code.
func arenaTree(workspace string, turn int, v model.VendorID) string {
	name := filepath.Base(workspace) + "-arena-t" + itoa(turn) + "-" + string(v)
	return filepath.Join(filepath.Dir(workspace), name)
}

func arenaBranch(turn int, v model.VendorID) string {
	return "arena/t" + itoa(turn) + "/" + string(v)
}

// arenaSetup records the base and adds one worktree per racing seat.
//
// The base is read ONCE, before any worktree exists, so every attempt races
// from the same commit. Per-seat failures skip that seat (reported on its
// column) rather than aborting the race — a partial read degrades a field, not
// the row, and the same rule holds one level up.
func arenaSetup(workspace string, turn int, seats []model.VendorID) (base string, trees map[model.VendorID]string, seatErr map[model.VendorID]string, err error) {
	base, err = gitOut(workspace, "rev-parse", "HEAD")
	if err != nil {
		return "", nil, nil, err
	}
	trees = map[model.VendorID]string{}
	seatErr = map[model.VendorID]string{}
	for _, v := range seats {
		tree := arenaTree(workspace, turn, v)
		if _, werr := gitOut(workspace, "worktree", "add", "-b", arenaBranch(turn, v), tree, base); werr != nil {
			seatErr[v] = werr.Error()
			continue
		}
		trees[v] = tree
	}
	return base, trees, seatErr, nil
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

// arenaCommitMsg names one attempt's commit from the turn that produced it:
// "arena t<N>: <first line of the brief>". First line only and capped at 64
// bytes (cut on a rune boundary), because the subject line is a label for
// `git log --oneline` over a kept branch, not a second copy of the brief —
// the brief itself is in the transcript and in the commit the user adopts.
func arenaCommitMsg(turn int, brief string) string {
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
		return "arena t" + itoa(turn)
	}
	return "arena t" + itoa(turn) + ": " + line
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
