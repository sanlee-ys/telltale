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
