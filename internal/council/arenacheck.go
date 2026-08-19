package council

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/sanlee-ys/telltale/internal/model"
)

// The measured check (§9.37, amended 2026-08-18): after a racer settles, the
// room RUNS a command the operator named inside that racer's worktree, and the
// column reports PASS or FAIL from the process's real exit code.
//
// Until this, /arena ranked attempts by arrival order and by diff size, and
// nothing in the room had ever executed a single line of what the racers wrote.
// The one question a reader of five diffs actually has — does it work? — was
// the one question the room answered by guessing. The competition answers it
// (Crystal runs a per-workspace script; Codex cloud shows per-attempt test
// results), and an exit code is a MEASUREMENT, which is what makes this
// admissible here at all.
//
// # Why "check" and not "gate"
//
// The room already spends the word gate on the permission gate — gate cards,
// the gate hook, the gate clock, PostureWriteGated — where it means "the thing
// that asks you before a command runs". This runs a command and reports what it
// returned, which is close enough to be confused with it and different enough
// that the confusion would matter. One word, one meaning; the second word is
// check.
//
// # Three rulings bound what this may be
//
//   - NO JUDGE, in any form. A cross-seat quality opinion is refused by §9.2
//     ("no ranking stage, no chairman, no synthesis hop") and again by §9.44's
//     Declined ("it would be council judging them, which no adapter sourced").
//     A `~` would not rescue it: `~` marks a value telltale COMPUTED, never an
//     opinion it formed. An exit code is neither — it is a number a process
//     returned.
//   - THE OPERATOR NAMES THE COMMAND, and the operator's environment is where
//     it is named. Not a file the repository carries: §9.37's own seeding
//     ruling took HALF of agent-deck's .worktreeinclude on exactly this
//     boundary — copying bytes into a tree the room owns is containable,
//     EXECUTING content the repo carries is not — and a checked-in check file
//     is that refused half wearing this feature's name. A clone from a stranger
//     must not be able to run a program on this machine by containing a file.
//     Not a new room verb either: the unknown-command refusal has a hard width
//     budget (TestTheRefusalFitsTheRoomItIsShownIn), and §9.31's own note says
//     the next verb has to find its characters somewhere else.
//   - THE CHECK RUNS IN THE RACER'S WORKTREE, never in the room's own tree.
//     dueArenaChecks reads the tree off the settled ArenaResult, which is the
//     path arenaSetup created for that seat this race; nothing else reaches
//     this runner.
//
// Absent, zero and degraded stay three states (§4a.1), and the third is the one
// this feature could most easily get wrong: a check that could not RUN did not
// fail — a binary that is not installed, a spelling this room refuses, a tree
// whose diff was unreadable, and a deadline are all "unavailable", and
// rendering any of them as FAIL would put a verdict on an attempt nobody
// measured.

// arenaCheckEnv is the one place an operator names the check command.
const arenaCheckEnv = "TELLTALE_ARENA_CHECK"

// arenaCheckDeadline bounds one check run.
//
// Fifteen minutes, and the number is chosen against a measured suite rather
// than guessed: this repository's own `go test ./internal/council` runs ~455s
// on the reference workstation and ~4m22s for the whole suite in CI
// (CLAUDE.md), so anything under ten minutes would kill a legitimate check on
// the very repo this room was built in. The deadline is a fact about the room's
// patience, not about the check — a killed process has no exit code, so it
// reports as unavailable with the deadline named, never as FAIL.
//
// The room's own context is the other end of it: a check outlives the turn that
// started it (a test suite easily runs longer than the race), so it is bounded
// by the ROOM rather than by the turn, and quitting the room ends it.
const arenaCheckDeadline = 15 * time.Minute

// ArenaCheck is one attempt's check outcome, held on its ArenaResult so Render
// stays pure over State: every field is computed when the run lands, never
// during a frame.
//
// The four states it can be in are told apart by fields rather than by an enum,
// and the reading order is the render's:
//
//   - Running: launched, no exit code yet. The block says so, because a room
//     that renders nothing while a process it started is running is a room
//     hiding it.
//   - Err != "": no exit code exists, and why. NEVER a FAIL.
//   - Exit == 0: PASS, measured.
//   - Exit != 0: FAIL, measured.
//
// A nil *ArenaCheck is the fifth and it is the important one: no check is
// configured, so the block draws NOTHING. Absent is not FAIL (§4a.1).
type ArenaCheck struct {
	// Cmd is the command as the operator spelled it, carried for the render so
	// the verdict names what produced it. A PASS whose command is invisible is
	// a claim the reader cannot check.
	Cmd string
	// Running is true between the launch and the outcome landing.
	Running bool
	// Exit is the process's real exit code, valid only when Running is false
	// and Err is empty. It is read off exec's ExitError — never inferred from
	// output, never from a timer.
	Exit int
	// Err names why no exit code exists: a refused spelling, a binary that
	// would not start, a deadline, a tree whose diff was never read. It is the
	// degraded state, and it is deliberately not spelled as a failure.
	Err string
	// Elapsed is how long the run took, stamped by the goroutine that ran it.
	// Zero when nothing was timed.
	Elapsed time.Duration
}

// arenaCheckSpec is the operator's check as this room resolved it at startup:
// what they typed, the argv it splits into, and — when it cannot run at all —
// the sentence saying why.
//
// Resolved ONCE, in newWithBrief, rather than read per race: a room that
// changed check mid-session on an environment edit would make two races in one
// transcript answer different questions with the same word.
type arenaCheckSpec struct {
	// raw is the operator's own string. Non-empty means a check is configured,
	// which is the whole of what separates "absent" from every other state.
	raw string
	// argv is the command and its arguments. nil when why is set.
	argv []string
	// why refuses this spelling by name, empty for a runnable one.
	why string
}

func (s arenaCheckSpec) configured() bool { return s.raw != "" }

// arenaCheckShellChars are the characters that mean this string was written for
// a shell, and this room does not have one.
//
// §9.3 rules argv and never a shell for every process council starts, and the
// reason is Windows specifically: a `.cmd` shim runs through cmd.exe, whose
// argument parsing cannot be safely quoted for arbitrary text. So a check that
// contains a pipe, a redirection, a separator or a substitution is REFUSED by
// name rather than silently split into an argv that means something else — a
// `go test ./... > out.txt` quietly run as three arguments would report an exit
// code for a command the operator did not write.
const arenaCheckShellChars = "|&;<>()$`\n\r\"'"

// resolveArenaCheck turns the operator's environment into this room's check
// spec. An unset or blank variable is no check at all — the nil-render case —
// and is not an error: most rooms race without one.
func resolveArenaCheck(raw string) arenaCheckSpec {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return arenaCheckSpec{}
	}
	spec := arenaCheckSpec{raw: trimmed}
	if i := strings.IndexAny(trimmed, arenaCheckShellChars); i >= 0 {
		// The refused character is QUOTED back: an unquoted `|` in a sentence
		// reads as punctuation rather than as the character that was refused,
		// and strconv.Quote is also what makes a refused newline visible.
		spec.why = "council runs a check as argv and never through a shell, so " +
			strconv.Quote(trimmed[i:i+1]) + " cannot be part of one"
		return spec
	}
	spec.argv = strings.Fields(trimmed)
	if len(spec.argv) == 0 {
		// Unreachable after the TrimSpace above, and kept because argv[0] is
		// indexed elsewhere on the strength of this being impossible.
		spec.why = "names no command"
	}
	return spec
}

// checkOutcome is one finished check run as the goroutine measured it.
type checkOutcome struct {
	exit    int
	err     string
	elapsed time.Duration
}

// startCheck is the one place a check command is spawned, as a package var for
// the reason main_test.go's three spawn vars are: a council test must be able
// to stub it, and reaching the real one with a resolvable binary is a test
// running an arbitrary program on somebody's machine.
var startCheck = runCheckProcess

// runCheckProcess runs one check to completion inside one racer's worktree.
//
// Argv through exec.CommandContext, never a shell (§9.3). On Windows a command
// like `npm test` resolves to a `.cmd` shim that Go runs through cmd.exe, whose
// argument parsing §9.3 already records as unsafe for arbitrary text — which is
// the second reason the shell characters are refused up front rather than
// passed along: what reaches such a shim here is a list of plain tokens the
// operator typed.
//
// Output is read and DISCARDED on purpose: the verdict this feature ships is
// the exit code, and a test suite's stdout is megabytes of content the room has
// nowhere honest to put — a column is not a terminal, and half of a build log is
// worse than none of it. The exit code is the whole measurement, which is also
// why it is the whole claim.
func runCheckProcess(ctx context.Context, dir string, argv []string) checkOutcome {
	start := time.Now()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	err := cmd.Run()
	elapsed := time.Since(start)
	if ctx.Err() != nil {
		// The context ended this process, so whatever cmd.Run reports describes
		// the SIGNAL and not the check's verdict. Reading an exit code out of a
		// kill would be the estimate-as-measurement bug with a number attached
		// (§4a.1) — the honest answer is that nothing was measured.
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return checkOutcome{err: "stopped at the " + dur(arenaCheckDeadline) + " deadline", elapsed: elapsed}
		}
		return checkOutcome{err: "stopped when the room did", elapsed: elapsed}
	}
	if err == nil {
		return checkOutcome{exit: 0, elapsed: elapsed}
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return checkOutcome{exit: code, elapsed: elapsed}
		}
		// Killed by a signal nobody in this room sent. There is no exit code to
		// report, so none is invented.
		return checkOutcome{err: "no exit code: " + firstLine(err.Error()), elapsed: elapsed}
	}
	// Never started: the binary is not on PATH, the worktree is gone, the file
	// is not executable. A check that could not run did not fail.
	return checkOutcome{err: "could not run: " + firstLine(err.Error()), elapsed: elapsed}
}

// arenaCheckMsg is one finished check arriving back in the Update loop. It
// names the vendor AND the race, because a check outlives the turn that started
// it: a suite running longer than the race is the ordinary case, not the edge,
// so the receipt this answers about has to be identified rather than assumed.
type arenaCheckMsg struct {
	vendor  model.VendorID
	raceN   int
	outcome checkOutcome
}

// runArenaCheckCmd runs one check off the Update loop. The closure captures
// plain strings — a Cmd runs on a goroutine, and the only thing it may share
// with the room is the message it returns.
func runArenaCheckCmd(ctx context.Context, v model.VendorID, raceN int, tree string, argv []string) tea.Cmd {
	return func() tea.Msg {
		cctx, cancel := context.WithTimeout(ctx, arenaCheckDeadline)
		defer cancel()
		return arenaCheckMsg{vendor: v, raceN: raceN, outcome: startCheck(cctx, tree, argv)}
	}
}

// dueArenaChecks launches the check for every settled attempt that has not had
// one, and returns the batch (nil when nothing is due).
//
// Called from the spinner tick rather than from finishColumn, and that is the
// decision worth stating: finishColumn returns no command and is reached from
// two dozen call sites, while the tick runs unconditionally for the room's
// whole life — which is exactly the property this needs, because the LAST racer
// settles the turn, and a launch that waited for the next event batch would
// wait for a channel nothing writes to any more.
//
// The launch is marked by setting the Check itself, so one attempt can never
// start two: every branch below either leaves the field nil (no check
// configured) or fills it in.
func (m *Model) dueArenaChecks() tea.Cmd {
	if !m.arenaCheck.configured() {
		return nil
	}
	// A Model a test built by hand has no room context. Background is the
	// honest stand-in: it carries no cancel, which is exactly the room such a
	// model has — none to quit.
	ctx := m.roomCtx
	if ctx == nil {
		ctx = context.Background()
	}
	var cmds []tea.Cmd
	for i := range m.st.Columns {
		c := &m.st.Columns[i]
		if c.Arena == nil || c.Arena.Check != nil {
			continue
		}
		switch {
		case m.arenaCheck.why != "":
			// The spelling was refused at startup. Said per attempt rather than
			// once at room open, because the block is where a reader asks what
			// the check said — a refusal that lives only in a notice the room
			// scrolled past is a check silently absent.
			c.Arena.Check = &ArenaCheck{Cmd: m.arenaCheck.raw, Err: m.arenaCheck.why}
		case c.Arena.Err != "":
			// The attempt's own tree could not be read. Running a check in it
			// would measure something, but not this attempt — and saying
			// nothing would collapse "no check configured" into "check
			// skipped", which is the absent-vs-degraded bug (§4a.1).
			c.Arena.Check = &ArenaCheck{Cmd: m.arenaCheck.raw, Err: "not run — the attempt's diff could not be read"}
		case c.Arena.Tree == "":
			c.Arena.Check = &ArenaCheck{Cmd: m.arenaCheck.raw, Err: "not run — this attempt has no worktree"}
		default:
			c.Arena.Check = &ArenaCheck{Cmd: m.arenaCheck.raw, Running: true}
			cmds = append(cmds, runArenaCheckCmd(ctx, c.Vendor, c.Arena.RaceN, c.Arena.Tree, m.arenaCheck.argv))
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// applyArenaCheck lands one check outcome on its attempt — or drops it, and the
// drop conditions are the point. A check that outlived the receipt it ran for
// (the seat took a new turn, the column was cleared, a later race replaced the
// block) says nothing: it measured a tree for an attempt that is no longer on
// screen, and writing it onto whatever is there now would put one attempt's
// verdict under another attempt's diff.
func (m *Model) applyArenaCheck(msg arenaCheckMsg) {
	c := m.column(msg.vendor)
	if c == nil || c.Arena == nil || c.Arena.RaceN != msg.raceN {
		return
	}
	k := c.Arena.Check
	if k == nil || !k.Running {
		return
	}
	k.Running = false
	k.Exit, k.Err, k.Elapsed = msg.outcome.exit, msg.outcome.err, msg.outcome.elapsed
}
