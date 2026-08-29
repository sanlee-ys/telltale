package runner

import (
	"context"
	"errors"
	"os/exec"
)

// RunContained runs one command to completion in dir and reports its exit code.
//
// It exists for the arena check (council, design.md §9.48), which is the first
// process this project starts that is neither a vendor nor a git call: a
// command the operator named, run once in a racer's worktree, whose EXIT CODE
// is the whole deliverable. Nothing is streamed, nothing is parsed, and no
// clock record is written — Start's whole apparatus is about a turn, and this
// is not one.
//
// What it takes from Start is the one thing a plain exec.CommandContext cannot
// do: the process GROUP. proc_windows.go explains why in the vendor's terms,
// and a check inherits the same shape — `npm test` on Windows resolves to a
// .cmd shim, so the process started here is cmd.exe, which starts node, which
// starts the real work. Killing the direct child on a deadline would leave that
// tree running with nothing on screen to say so. The job object (Windows) and
// the process group (unix) reach the descendants; unix's own limits are
// recorded in proc_unix.go and are unchanged by this caller.
//
// Returns are three facts kept apart on purpose, because the caller renders
// them as three different things (§4a.1). An exited process reports code with
// exited true — including a nonzero one, which is a measurement and not an
// error. A process that never ran, or that was killed before it could exit,
// reports exited false and the error that says so; there is no exit code in
// that case and the caller must not invent one.
//
// Output is DISCARDED. Both streams go to the null device rather than to a
// buffer, so a check that prints a gigabyte cannot grow this process. The
// caller's own ruling is that the exit code is the whole claim.
func RunContained(ctx context.Context, dir string, argv []string) (code int, exited bool, err error) {
	if len(argv) == 0 {
		return 0, false, errors.New("no command")
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir

	group := newProcGroup()
	group.prepare(cmd)
	if err := cmd.Start(); err != nil {
		group.close()
		return 0, false, err
	}
	// Start's own race applies here word for word: a child that spawns a
	// grandchild in the microseconds before assignment escapes the group, and
	// closing that needs a suspended start os/exec does not expose. Non-fatal
	// for the same reason it is there — the run still happens, and killing the
	// direct child remains possible.
	_ = group.attach(cmd)
	defer group.close()

	werr := cmd.Wait()
	if ctx.Err() != nil {
		// The context ended this run, so whatever Wait reports describes the
		// stop rather than the command's own verdict. The kill goes through the
		// group as well as through CommandContext's own child kill: the tree is
		// the point.
		_ = group.kill()
		return 0, false, ctx.Err()
	}
	if werr == nil {
		return 0, true, nil
	}
	var ee *exec.ExitError
	if errors.As(werr, &ee) && ee.ExitCode() >= 0 {
		return ee.ExitCode(), true, nil
	}
	// No exit code exists: killed by a signal (ExitCode reports -1 where those
	// exist), or a failure before the process ever ran.
	return 0, false, werr
}
