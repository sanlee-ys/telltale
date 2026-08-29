package council

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The arena check (§9.48): one command the OPERATOR named, run once in each
// racer's own worktree, rendered PASS or FAIL from that run's real exit code.
//
// It answers the one question /arena could not. The race already reports what
// each attempt CHANGED — the stat, the patch, the commit receipt (§9.37) — and
// the record already reports which seat the operator TOOK (§9.47). Neither says
// whether the attempt works, and the operator answered that by opening each of
// five worktrees in a second terminal and running the same command five times.
//
// Four rulings hold the whole feature, and each one is a line this code may not
// cross:
//
//   - THE EXIT CODE IS THE ONLY SOURCE. PASS is exit 0 and FAIL is any other
//     code, both read back from the process (ArenaCheck.Exited). Nothing here
//     infers a verdict from output, from a diff, from a duration, or from a
//     model's opinion. An LLM judge is refused by ruling twice over — §9.2's
//     "no ranking stage, no chairman, no synthesis hop" and §9.44's declined
//     cross-seat quality mark — and it is refused here even wearing an
//     estimate's `~`, because `~` marks a figure telltale COMPUTED and an
//     opinion is not a computation.
//   - A COMMAND THAT COULD NOT RUN IS ITS OWN STATE. A missing binary, an
//     unreadable worktree, a run the deadline killed: none of them is a FAIL.
//     They carry Err and render as unavailable, because "this attempt failed
//     the check" and "nothing measured this attempt" are the degraded-vs-zero
//     distinction §4a.1 exists to keep apart.
//   - NO COMMAND NAMED IS ABSENT, and absence draws nothing at all. Not a
//     dash, not a 0, not a pending word. It is nil Seed's rule (§9.37) on a
//     second field: a room that never asked for a check has no check to report.
//   - THE ROOM CAPTURES NO OUTPUT. The exit code is the whole claim. Rendering
//     a failing command's stdout would put unredacted subprocess text on a
//     screen whose vendor streams all pass through a Redactor first, and it
//     would need a scroll surface the gate cards were already refused. The
//     block names the command and the worktree; the operator re-runs it there.

// arenaCheckTimeout bounds one check run, and it is deliberately generous.
//
// The number is anchored on this repository's own suite rather than guessed:
// CLAUDE.md records `go test ./...` at ~455s locally and ~4m22s in CI, which
// is the slowest command an operator is likely to name here. Ten minutes is
// past twice that, and the margin is the decision rather than the figure —
// what this bound must never do is kill a check that would have finished,
// because a killed run yields NO exit code and therefore no verdict at all.
//
// A check the deadline ends is reported as unavailable with the bound named,
// never as FAIL: the process was stopped, so nothing measured it (the ruling
// above). The check runs off the render loop, so the room stays usable for the
// whole of it either way.
const arenaCheckTimeout = 10 * time.Minute

// ArenaCheck is one racer's check run: what was named, whether it produced an
// exit code, and what that code was.
//
// On the ArenaResult so Render stays pure over State — every field is written
// when the run's message lands in Update, never during a frame, exactly as
// ArenaResult and ArenaInterim already are.
type ArenaCheck struct {
	// Cmd is the command verbatim as the operator named it, carried onto the
	// result rather than read from the room at render time. A copy, on purpose:
	// the operator may re-name the check between races, and a block that then
	// re-labelled a finished run with the NEW command would be a receipt
	// describing something that never ran.
	Cmd string
	// Running reports that the run was launched and has not reported back. Its
	// own state rather than an absence, because a check in flight and a check
	// that was never asked for are different facts about an empty verdict.
	Running bool
	// Exited reports that the process produced an exit code. It is the ONLY
	// gate on PASS/FAIL: a false Exited with an empty Code is not a pass.
	Exited bool
	// Code is that exit code, verbatim. Zero is a measured zero — and it is
	// only ever read beside Exited, which is what keeps it from reading as a
	// pass on a run that never happened.
	Code int
	// Err names why no exit code exists: the binary was not found, the tree
	// could not be entered, the deadline ended the run. Never a FAIL.
	Err string
	// Elapsed is the run's own measured wall time, stamped in the goroutine
	// that ran it. Absent (zero) on a check that never produced a result.
	Elapsed time.Duration
	// Dirty reports that the tree was CLEAN when the check started and was not
	// when it ended — the check itself wrote into the racer's worktree.
	//
	// It matters because of where the check sits in the finish line: the diff
	// is read and the attempt is committed BEFORE the check runs, so nothing a
	// check writes can reach the attempt's stat or its commit. What it can
	// reach is a later `/adopt`, which commits a dirty attempt before merging
	// it — so a build artifact would ride into the adoption wearing the racer's
	// name. Said here rather than cleaned up: the room does not reset a tree
	// the operator did not ask it to reset, and /adopt's own y/n card names
	// every command it will run before it runs one.
	//
	// False when the tree was already dirty before the run, and false when the
	// state could not be read at all — this field claims a measured change, not
	// the absence of one.
	Dirty bool
}

// Passed is the verdict, and the only place it is computed. Exited first,
// always: a zero Code on a run that never happened would otherwise read as a
// pass, which is the false zero §4a.1 exists to prevent, pointed at a verdict.
func (ck *ArenaCheck) Passed() bool { return ck != nil && ck.Exited && ck.Code == 0 }

// ---------------------------------------------------------------------------
// Naming the command: `/arena check`

// lookPath resolves a bare command name against PATH, as a var so a test can
// drive both branches of the refusal below without depending on which programs
// the machine running the suite happens to have installed.
var lookPath = exec.LookPath

// startCheck runs one check process. A package var for the reason the three
// spawn vars in dispatch.go are: the council suite must never launch a real
// program from the model's own path, and TestMain wraps this one with the same
// resolvable-binary guard (main_test.go) while countSpawns stubs it.
var startCheck = runCheck

// parseArenaCheck recognises the check verb inside a /arena draft: "check",
// "check off", and "check <command>". The second return is everything after
// the verb, with the operator's own spacing inside it preserved.
//
// This is the ONE /arena sub-verb that takes free text, and it is the reason
// the refusal in arenaCheckCommand exists. `/arena drop` and `/arena record`
// protect a brief by taking only their exact form (parseArenaDrop's own note);
// a command is free text, so no length cap can do that work here. What does it
// instead is the PATH check on the first word: a draft whose first word after
// `check` is not a program this machine can run is refused and handed back,
// never raced and never set.
//
// The cost, stated the way parseArenaDrop states its own: a brief that opens
// with the word `check` needs rewording to race. The narrower case that
// survives is a brief opening `check <something that IS on PATH>` — "check
// make sure the retry backs off" would name a command instead of racing — and
// the mitigation is that nothing spawns, nothing is billed, the notice names
// exactly what was set, and `/arena check off` takes it back in one line.
func parseArenaCheck(brief string) (arg string, ok bool) {
	s := strings.TrimSpace(brief)
	if s == "check" {
		return "", true
	}
	rest, found := strings.CutPrefix(s, "check ")
	if !found {
		return "", false
	}
	return strings.TrimSpace(rest), true
}

// arenaCheckCommand is `/arena check`: name the command every racer runs, say
// what is named, or take it back.
//
// Room state rather than repo state, and that is a ruling with a reason. The
// obvious alternative was a file in the repository — `.worktreeinclude`'s
// sibling — and arena.go's own seeding doc refuses exactly that: agent-deck
// pairs seeding with repo-carried setup scripts, "copy only, never execute",
// because a repository that can run a command on the machine by merely
// CONTAINING a file is a different product with a different threat model. A
// check command a person typed into their own room is that person's act. A
// check command a clone brought with it is not.
//
// It is not refused mid-turn (`/arena record`'s rule): naming a command
// changes nothing that is running, and the races already in flight carry the
// command they were launched with on their own results.
//
// It survives `/cd`, which is the posture rule rather than an oversight:
// `/write` is room state and moving the workspace does not quieten it either.
// The mitigation is that every check result names the command it ran, so a
// command left over from another repository is visible in the verdict rather
// than assumed behind it.
//
// It does NOT survive the room, and that is the same ruling read the other
// way. room.json holds session ids and a workspace — keys and numbers, never
// content — so a command has nowhere honest to be saved, and saving one would
// run it in a session whose operator never typed it. Re-naming it is one line.
func (m *Model) arenaCheckCommand(arg string) {
	switch {
	case arg == "":
		if m.checkCmd == "" {
			m.st.Notice = "no check is named — /arena check <command> runs it in every racer's worktree and reads its exit code"
		} else {
			m.st.Notice = "each racer runs " + m.checkCmd + " — /arena check off stops it"
		}
		m.setDraft("")
	case arg == "off":
		if m.checkCmd == "" {
			// Nothing to take back. Said rather than answered with a cheerful
			// confirmation of an act that did not happen.
			m.st.Notice = "no check was named — races already report no check at all"
			m.setDraft("")
			return
		}
		was := m.checkCmd
		m.checkCmd, m.checkArgv = "", nil
		m.st.Notice = "the check is off — " + was + " will not run again"
		m.setDraft("")
	default:
		argv := strings.Fields(arg)
		// Fields, not a shell split, and the limit is stated because it is a
		// real one: argv never a shell (§9.3), so one word is one argument and
		// quoting is not a grammar this room has. A command that needs a quoted
		// argument goes in a script, which is one word.
		first := argv[0]
		if !strings.ContainsAny(first, `/\`) {
			if _, err := lookPath(first); err != nil {
				// The one guard that keeps a brief from being swallowed. A
				// path-bearing first word is NOT checked here on purpose: it is
				// resolved against the racer's worktree at run time, where this
				// process's own directory has no say (checkPath).
				m.st.Notice = "no program called " + first +
					" — /arena check <command> names what each racer runs · /arena <brief> races prose"
				return
			}
		}
		m.checkCmd, m.checkArgv = arg, argv
		m.st.Notice = "each racer will run " + arg +
			" in its own worktree — PASS or FAIL comes from its exit code · /arena check off stops it"
		m.setDraft("")
	}
}

// ---------------------------------------------------------------------------
// Running it

// arenaCheckPending is one queued run: the seat, the turn it belongs to, and
// the tree it runs in.
//
// A queue rather than a direct launch because finishColumn cannot return a
// tea.Cmd — it is reached from applyEvents, several frames deep. Update drains
// this on the same two legs dueArenaRefreshes uses (the event batch and the
// spinner tick), so the wait is one tick at worst.
type arenaCheckPending struct {
	vendor model.VendorID
	turnN  int
	tree   string
	argv   []string
}

// arenaCheckMsg is one finished run arriving back in Update. It names the
// vendor AND the turn for arenaStatMsg's reason: the run happened on a
// goroutine and outlives its turn by design, so a stale message must be
// droppable by comparison rather than by hoping the timing worked out.
type arenaCheckMsg struct {
	vendor  model.VendorID
	turnN   int
	exited  bool
	code    int
	err     string
	elapsed time.Duration
	dirty   bool
}

// armArenaCheck decides whether this landing seat gets a check, and returns
// the record its column will render while the run is in flight.
//
// Three refusals, and each is a fact rather than a policy:
//
//   - No command is named: there is nothing to run and nothing to report.
//   - The whole turn is being cancelled: the operator pressed ctrl+c, and a
//     room that answered that by starting five subprocesses would be ignoring
//     the one act it exists to obey. A seat cut on its own with `x` is NOT
//     this case — §9.37's give-up ruling says a given-up seat lands like any
//     other finisher, and its partial work is as worth checking as its diff.
//   - The check ran already for this result: collection is once-only
//     (finishColumn's c.Arena == nil guard) and so is this.
func (m *Model) armArenaCheck(v model.VendorID, turnN int, tree string) *ArenaCheck {
	if m.checkCmd == "" || len(m.checkArgv) == 0 {
		return nil
	}
	if m.cancelling {
		return nil
	}
	argv := append([]string(nil), m.checkArgv...)
	m.pendingChecks = append(m.pendingChecks, arenaCheckPending{
		vendor: v, turnN: turnN, tree: tree, argv: argv,
	})
	return &ArenaCheck{Cmd: m.checkCmd, Running: true}
}

// dueArenaChecks launches every queued run and returns the batch, nil when the
// queue is empty.
//
// Called from Update on the event batch AND on the spinner tick, which is
// dueArenaRefreshes' own two-leg rule for a different reason: the event batch
// that retires the LAST racer also ends the turn, and that branch returns
// before the refresh check, so the tick is what guarantees a queued run is
// never stranded by the turn ending on top of it.
func (m *Model) dueArenaChecks() tea.Cmd {
	if len(m.pendingChecks) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(m.pendingChecks))
	for _, p := range m.pendingChecks {
		cmds = append(cmds, checkArenaCmd(m.roomCtx, p))
	}
	m.pendingChecks = nil
	return tea.Batch(cmds...)
}

// checkArenaCmd runs one check off the Update loop. The closure captures plain
// values and a context, never the Model — a Cmd runs on a goroutine, and the
// only thing it may share with the room is the message it returns
// (refreshArenaCmd's rule).
//
// The context is the ROOM's, not the turn's, and the distinction is the whole
// lifetime question. A check outlives its turn on purpose: the last racer
// landing ends the turn, and a `go test` that started at that moment must
// still be allowed to finish and report onto a column that is still on screen.
// What it must not outlive is the room — teardown cancels roomCtx, so quitting
// kills a running check for the same reason it kills every other child this
// room started.
func checkArenaCmd(roomCtx context.Context, p arenaCheckPending) tea.Cmd {
	return func() tea.Msg {
		if roomCtx == nil {
			roomCtx = context.Background()
		}
		ctx, cancel := context.WithTimeout(roomCtx, arenaCheckTimeout)
		defer cancel()
		res := startCheck(ctx, p.tree, p.argv)
		return arenaCheckMsg{
			vendor: p.vendor, turnN: p.turnN,
			exited: res.exited, code: res.code, err: res.err,
			elapsed: res.elapsed, dirty: res.dirty,
		}
	}
}

// checkResult is what one run measured. Separate from ArenaCheck because this
// is the goroutine's reading and that is the column's record; the message
// between them is what keeps Render pure.
type checkResult struct {
	exited  bool
	code    int
	err     string
	elapsed time.Duration
	dirty   bool
}

// runCheck runs the operator's command in one racer's worktree and reports
// what it measured.
//
// Argv, never a shell (§9.3's rule applies to every process council starts,
// not only to vendors), and no output is captured at all — the ruling at the
// top of this file. The run goes through runner.RunContained rather than
// through exec directly, for the reason that package owns: a check is a
// process TREE, and on Windows `npm test` is a .cmd shim whose real work runs
// two processes down, so a deadline that killed only the direct child would
// leave it running with nothing on screen to say so.
//
// Two `git status --porcelain` reads bracket the run, and they are the only
// reason this function touches git: a check that WROTE into a tree it found
// clean is a fact /adopt would otherwise carry silently.
func runCheck(ctx context.Context, tree string, argv []string) checkResult {
	if len(argv) == 0 {
		return checkResult{err: "no command named"}
	}
	cleanBefore, knownBefore := treeIsClean(tree)

	start := time.Now()
	cmd := append([]string{checkPath(tree, argv[0])}, argv[1:]...)
	code, exited, err := runner.RunContained(ctx, tree, cmd)
	res := checkResult{elapsed: time.Since(start)}
	switch {
	case exited:
		// The measurement, including a nonzero code: that is a FAIL, and a FAIL
		// is a reading rather than an error.
		res.exited, res.code = true, code
	case ctx.Err() != nil:
		// The deadline or the room ended this process, so no exit code exists
		// and the error describes the stop. Reporting the stop as a code would
		// be the estimate-as-reading bug pointed at a failure: nothing measured
		// this attempt.
		res.err = "stopped after " + dur(arenaCheckTimeout)
	default:
		// The binary was not found, the directory was gone, or the process died
		// on a signal. Every one of those is a run that did not happen, not a
		// run that failed.
		res.err = firstLine(err.Error())
	}
	if cleanAfter, knownAfter := treeIsClean(tree); knownBefore && knownAfter {
		res.dirty = cleanBefore && !cleanAfter
	}
	return res
}

// checkPath resolves the command's first word for a process whose Dir is the
// racer's worktree.
//
// It exists for a documented trap in os/exec: cmd.Dir sets the CHILD's working
// directory, but a relative program path is resolved against the PARENT's —
// so `./scripts/check.sh` would be looked for beside the room's own cwd, and
// would either miss the script entirely or, worse, run a same-named one from
// somewhere the operator did not mean. A path-bearing first word is therefore
// joined to the tree by hand. A bare name is left alone and resolved on PATH,
// which is what the operator means by `go` or `npm`.
func checkPath(tree, first string) string {
	if !strings.ContainsAny(first, `/\`) {
		return first
	}
	if filepath.IsAbs(first) {
		return first
	}
	return filepath.Join(tree, first)
}

// treeIsClean reports whether a worktree holds no uncommitted change, and
// whether that question could be answered at all. The second return is the
// difference between "clean" and "unknown", which is what stops a failed read
// from being reported as a check that changed nothing.
func treeIsClean(tree string) (clean, known bool) {
	out, err := gitOut(tree, "status", "--porcelain")
	if err != nil {
		return false, false
	}
	return strings.TrimSpace(out) == "", true
}

// applyArenaCheck lands one finished run on its column — or drops it, and the
// drop rules are the point. A run that outlived its turn (the next dispatch
// cleared the column's Arena, or moved its turn number) says nothing about the
// race now on screen, and a stale goroutine must never write over a later one.
func (m *Model) applyArenaCheck(msg arenaCheckMsg) {
	c := m.column(msg.vendor)
	if c == nil || c.TurnN != msg.turnN || c.Arena == nil || c.Arena.Check == nil {
		return
	}
	ck := c.Arena.Check
	ck.Running = false
	ck.Exited, ck.Code, ck.Err = msg.exited, msg.code, msg.err
	ck.Elapsed, ck.Dirty = msg.elapsed, msg.dirty
}
