package council

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// AGENTS.md as the race's second brief channel — the one context surface that
// needs no per-vendor prompt plumbing, because the seat reads the file itself.
//
// §9.37 amendment, 2026-08-29. The prompt is still the brief: every racer gets
// arenaConduct plus the operator's text on the wire exactly as before, and this
// file changes none of that. What it adds is a copy of the same words at the
// path the vendors already look at, so a seat that re-reads its instructions
// mid-turn finds them without council having to teach five CLIs five different
// context flags.
//
// WHY THIS IS ALLOWED TO EXIST beside arenaConduct's "ONE place the room adds
// words" position: the bend is bounded exactly the way that constant's is.
// Races only, and the file is IDENTICAL for every seat — same brief, same
// conduct line, same race number — so the cross-seat comparison the race
// exists for is undisturbed. Per-seat constraint text is deliberately
// unrepresentable here: there is no seat parameter on arenaBriefText, so a
// later change that wants one has to argue for it in §9.37 rather than slip it
// in as a formatting option.
//
// WHAT WAS MEASURED, 2026-08-29, one headless probe per vendor from a scratch
// directory holding an AGENTS.md that named a codename nothing else on the box
// knew. The claim "20+ tools read AGENTS.md natively" is a docs claim, and this
// repo does not ship docs claims (ADR-001):
//
//   - codex-cli 0.149.1 — READS IT UNPROMPTED. `codex exec --skip-git-repo-check
//     --cd <dir>` answered the codename with no tool call in the transcript, so
//     the file reached the model as context, not as a file it went looking for.
//   - grok 1.0.5 — READS IT UNPROMPTED, and says so on the wire: its own
//     reasoning stream named "the always_applied_workspace_rules, the Agents.md
//     file" before answering. No tool call either.
//   - Claude Code 2.1.251 — ANSWERS, BY GOING TO LOOK. Both trials ran `ls -la`
//     and then `cat` on the file before answering, recorded in the probe
//     sessions' own transcripts. That is a real read of a real file in the cwd,
//     and it is NOT the same fact as the other two: this seat discovered the
//     file in a directory holding one file, which is the easiest possible case.
//     Nothing here claims it auto-loads AGENTS.md.
//   - agy (Antigravity) — UNMEASURED. Headless agy auto-denies tool turns at
//     1.1.20, and the probe was not run. The room claims nothing about this
//     seat, per CapNone's rule pointed at a vendor instead of a field.
//   - cursor — UNMEASURED for the same reason nothing else about this seat is
//     assumed: it races over ACP through a throwaway session (§9.37's cursor
//     amendment), and no probe of that path ran.
//
// The honest reading of that table is the reason this file is written for
// every racer and ANNOUNCED for none: two seats demonstrably ingest it, one
// demonstrably reaches for it, two are unmeasured, and the room has no way to
// tell per race which happened. Writing it costs a seat nothing; claiming a
// seat was briefed by it would be a fact council never measured. So no column,
// no notice, and no snapshot field carries "briefed via AGENTS.md" — the file
// is offered, exactly like the worktree itself is offered.
//
// THE DIFF, and why the attempt's receipt stays honest. A file council writes
// into the racer's tree is not part of the racer's answer, and `git add -N .`
// would put it in the stat anyway — the "lying diff" §9.37 already names as a
// known limit of seeding. The three git reads that could pick it up exclude
// exactly this one path (arenaBriefPathspec), so the stat, the full patch and
// the commit-per-turn receipt all carry the racer's work and nothing else, and
// /adopt therefore merges a branch that never held the file. Two properties
// keep that exclusion from becoming its own lie:
//
//   - It is CONDITIONAL on the marker, not on a flag council remembers. The
//     path is excluded only while the file on disk still opens with
//     arenaBriefMarker (arenaBriefArgs re-reads it per call). A racer that
//     overwrites AGENTS.md with its own content has authored a file, and it
//     appears in the diff like any other — which is also what happens in a
//     repo that carries its own tracked AGENTS.md, because council never wrote
//     one there.
//   - Council NEVER OVERWRITES an AGENTS.md the checkout or `.worktreeinclude`
//     seeding already put in the tree. In a repo that ships one, no racer gets
//     the council file, all seats read the repo's own instructions identically,
//     and the comparison is as uniform as it was before this existed.
//
// Known limit, stated rather than hidden: while the marker stands, a racer that
// APPENDS to council's AGENTS.md is excluded from its own diff on that path. A
// racer editing the room's brief file is not an answer to the brief, and the
// alternative — an exclusion that stops the moment the file grows — would drop
// council's own file into every stat on the first stray edit.
const (
	// arenaBriefFileName is the repo-root path council writes, and the exact
	// spelling the vendors look for. Root only: AGENTS.md nested inside the
	// tree belongs to the repo, and the pathspec below is measured leaving it
	// alone (git 2.55.0.windows.3, 2026-08-29).
	arenaBriefFileName = "AGENTS.md"

	// arenaBriefMarker opens every file council writes and is how every later
	// read knows the file is still council's. It is an HTML comment because
	// AGENTS.md is markdown by convention and the marker must not read as an
	// instruction to the seat that ingests it.
	arenaBriefMarker = "<!-- telltale arena brief"

	// arenaBriefPathspec keeps council's own file out of the racer's diff and
	// out of its commit. Measured on git 2.55.0.windows.3: with this pathspec
	// `git add -N .` leaves AGENTS.md untracked (so `git diff <base>` never
	// reaches it), `git add -A` leaves it out of the commit, and
	// `git status --porcelain` reports a tree holding nothing but this file as
	// clean — which is what keeps the empty-commit ruling working.
	arenaBriefPathspec = ":(exclude)" + arenaBriefFileName
)

// arenaBriefText is the file every racer gets, and the seat it is written for
// is deliberately not a parameter — see the identical-per-seat position above.
//
// The order is marker, conduct, brief. The brief goes last because it is the
// part a reader scans for, and the conduct line comes first for the same
// reason it leads the prompt: the sentence that stops a racer pushing to the
// operator's remote should not be below the fold of its own brief.
func arenaBriefText(raceN int, brief string) string {
	var b strings.Builder
	b.WriteString(arenaBriefMarker + " t" + itoa(raceN) + " — council wrote this file; it is not part of this attempt -->\n\n")
	b.WriteString("# arena t" + itoa(raceN) + "\n\n")
	b.WriteString(arenaConduct + "\n\n")
	b.WriteString("This file is council's, not the repository's. It is excluded from the\n")
	b.WriteString("attempt's diff and from the attempt's commit, so do not commit it and do\n")
	b.WriteString("not rewrite it.\n\n")
	b.WriteString("## The brief\n\n")
	b.WriteString(strings.TrimSpace(brief) + "\n")
	return b.String()
}

// writeArenaBriefs puts the same brief file in every racer's tree, and it runs
// after arenaSetup rather than inside it so the setup's own signature — and the
// twenty tests standing on it — stay exactly as they were.
//
// A WRITE THAT FAILS SKIPS THAT SEAT, with the reason on its column, and that
// is the seeding rule applied for the seeding rule's reason: a tree the room
// KNOWS holds a different brief from its siblings races a different question,
// and a race whose seats were asked different things is not the comparison the
// operator opened. The skip is the same shape `.worktreeinclude` seeding uses
// (seatErr, worktree kept on disk), so nothing new has to be rendered for it.
//
// A tree that ALREADY holds an AGENTS.md is left alone and races — not a
// failure and not a skip. That file is the repo's, it arrived identically in
// every racer's checkout, and overwriting it would plant council's words on top
// of the repository's own and hide the racer's edits to it behind the pathspec.
//
// trees and seatErr are the setup's own maps and are mutated in place: a seat
// that is skipped here must leave the trees map exactly as a seat skipped
// inside arenaSetup does, or dispatch would race a column with no worktree.
func writeArenaBriefs(ctx context.Context, raceN int, brief string, trees map[model.VendorID]string, seatErr map[model.VendorID]string) {
	if len(trees) == 0 {
		return
	}
	// One text for every seat, built once — the cheapest possible enforcement
	// of the identical-per-seat position, since there is not even a per-seat
	// copy to diverge.
	text := arenaBriefText(raceN, brief)
	for v, tree := range trees {
		if ctx.Err() != nil {
			// The setup's own clock ended. The caller turns this into the
			// whole-setup stop; writing into trees the room is about to
			// abandon would be work nobody reads.
			return
		}
		p := filepath.Join(tree, arenaBriefFileName)
		if _, err := os.Stat(p); err == nil {
			continue
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			seatErr[v] = "brief file failed: " + firstLine(err.Error())
			delete(trees, v)
		}
	}
}

// arenaBriefArgs is the pathspec every read of a racer's tree appends, and it
// is nil unless council's file is still standing in that tree.
//
// It re-reads the marker per call rather than trusting a flag recorded at
// setup, because the fact that matters at diff time is what the file holds NOW.
// A racer that replaced council's file authored one, and a stale flag would
// hide that authorship from the stat the operator compares.
//
// A file it cannot read at all is treated as not council's: excluding a path on
// the strength of a read that failed would be the diff omitting a file for a
// reason nobody measured.
func arenaBriefArgs(tree string) []string {
	f, err := os.Open(filepath.Join(tree, arenaBriefFileName))
	if err != nil {
		return nil
	}
	defer f.Close()
	// The marker is the first bytes of the file by construction, so the read is
	// bounded rather than sized to the file: a racer that appended a megabyte
	// below it must not cost every refresh that megabyte.
	// io.ReadFull, not one Read: a Read that returns fewer bytes than asked for
	// is legal and would fail the comparison on a file that does carry the
	// marker — an exclusion that switched itself off on a short read would put
	// council's file in the racer's stat.
	buf := make([]byte, len(arenaBriefMarker))
	if _, err := io.ReadFull(f, buf); err != nil || string(buf) != arenaBriefMarker {
		return nil
	}
	return []string{arenaBriefPathspec}
}

// removeArenaBrief takes council's own file back, and ONLY council's own file:
// it is a no-op unless the marker is still there.
//
// It has exactly one caller, `/arena drop`, and the reason is git's rather than
// tidiness. `git worktree remove` counts an untracked file as a dirty worktree
// and refuses, so a brief file left standing would make every ordinary drop
// fail and demand the `!` spelling — the room's own write turning a plain verb
// into a forced one. Nothing else deletes it: the worktree is KEPT until the
// user drops it, and until then the file is the visible record of what that
// seat was told.
//
// The failure is deliberately swallowed. The caller hands the whole tree to
// `git worktree remove` next, which reports its own refusal in its own words; a
// second sentence about one file inside a directory that is going away would
// name a problem the operator cannot act on separately.
func removeArenaBrief(tree string) {
	if arenaBriefArgs(tree) == nil {
		return
	}
	_ = os.Remove(filepath.Join(tree, arenaBriefFileName))
}
