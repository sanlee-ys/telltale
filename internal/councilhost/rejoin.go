package councilhost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// HostState is what a discovery probe found, and it has FIVE values rather than
// a boolean.
//
// §4a.1 is the reason for every one of the splits. "No host has ever run here"
// and "the host you left is gone" are different facts and an operator acts on
// them differently. "A host is running" and "a host is running and somebody is
// already in it" are different facts for the same reason: the second is a
// refusal with a remedy, and collapsing it into the first would make a client
// try to rejoin a room it cannot have.
type HostState int

const (
	// HostNone: no discovery file. No host has run in this council directory,
	// or the last one exited cleanly and removed its file.
	HostNone HostState = iota
	// HostLive: a discovery file, a listening pipe with a free instance, and a
	// process that verified as this telltale. A client may rejoin.
	HostLive
	// HostBusy: all of the above, and a client already holds the room. §7.28's
	// one-client rule, seen from the outside.
	HostBusy
	// HostDead: a discovery file whose host is not there. The normal case after
	// a hard kill, and NOT a fault — Reason says what was measured.
	HostDead
	// HostUnreadable: a discovery file that could not be read or parsed.
	// Reported in the reader's own words and never repaired, on the same rule
	// LoadRoom follows.
	HostUnreadable
)

// HostReport is one probe's whole answer.
type HostReport struct {
	// State is what was found.
	State HostState
	// File is the discovery file's content. Populated whenever the file parsed,
	// including for HostDead — a dead host's pid and start time are exactly
	// what a returning client has to name.
	File HostFile
	// Reason says what was measured, in the measurer's own words. Empty for
	// HostLive and HostBusy, where the state IS the whole answer.
	Reason string
}

// Live reports whether a host is running, whether or not it is free.
func (r HostReport) Live() bool { return r.State == HostLive || r.State == HostBusy }

// Probe answers what is in a council directory, WITHOUT connecting to anything.
//
// # The three readings, and why all three are required
//
// design.md §7.28 ruled that the file says WHAT and the pipe says WHETHER, and
// §7.29 adds the third: the pid says WHO. Each catches a failure the other two
// cannot.
//
//  1. host.json is READ, never trusted for liveness. It names a pid, a pipe and
//     a start time.
//  2. ProbePipe asks whether the NAME exists, and does not open it. A probe
//     that dialled would consume the host's one instance and the host would
//     read the close as its client leaving — a liveness check that ends the
//     room it is checking (discovery.go's closing note records this in full).
//  3. verifyHostProcess asks whether that pid is a LIVE telltale that started no
//     later than the file claims. A pid is reusable; without this a stale file
//     pointing at a recycled number would be read as a live room, and
//     `telltale council kill` would terminate a stranger.
//
// Nothing here writes, including no cleanup of a stale file. That is what keeps
// `telltale council ls` the sixth READER it promises to be (§7.27): a reader
// that tidied would be a writer.
func Probe(councilDir string) HostReport {
	if councilDir == "" {
		return HostReport{State: HostNone}
	}
	f, err := ReadHostFile(councilDir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return HostReport{State: HostNone}
	case err != nil:
		return HostReport{State: HostUnreadable, Reason: err.Error()}
	}

	state, perr := ProbePipe(f.Pipe)
	if perr != nil {
		// A probe that FAILED is not a probe that answered "absent". Reported as
		// unreadable so a surface says "this could not be measured" rather than
		// "there is no host", which is §4a.1's degraded-versus-zero distinction
		// applied to a liveness read.
		return HostReport{State: HostUnreadable, File: f, Reason: perr.Error()}
	}
	if state == PipeAbsent {
		return HostReport{State: HostDead, File: f,
			Reason: fmt.Sprintf("nothing is listening on %s", displayName(f.Pipe))}
	}

	// The pipe exists, so SOMETHING is listening. Whether it is the host this
	// file describes is the question the identity check answers, and a pipe that
	// exists over a pid that does not verify is a name somebody else took.
	if err := verifyHostProcess(f.PID, f.StartedAt); err != nil {
		return HostReport{State: HostDead, File: f, Reason: err.Error()}
	}
	if state == PipeBusy {
		return HostReport{State: HostBusy, File: f}
	}
	return HostReport{State: HostLive, File: f}
}

// KillHost ends the host a council directory describes, and every seat with it.
//
// It refuses rather than guessing. A pid the probe cannot confirm is REPORTED
// and never terminated: killing a number a stale file names is the one failure
// this command could make that nothing could undo. design.md §7.29 states the
// rule and this is where it is enforced.
//
// The discovery file is removed only after the process is gone, and only when
// this call is what ended it. A host that exits cleanly removes its own file;
// a hard-killed one cannot, which is why removing it here is part of the same
// act rather than a tidy-up somebody else has to do.
func KillHost(councilDir string) (string, error) {
	rep := Probe(councilDir)
	switch rep.State {
	case HostNone:
		return "no host is running for this room. nothing to end.", nil
	case HostUnreadable:
		return "", fmt.Errorf("councilhost: the discovery file could not be read, so nothing was ended: %s",
			rep.Reason)
	case HostDead:
		// The file is stale and the process is gone. Removing it is the honest
		// act and it is stated, because a command that silently deleted state
		// would be doing something the operator did not ask for.
		// What the dead host LEFT is swept before its file goes. On Windows the
		// room job reaped the seats when the host died and this finds nothing;
		// on macOS and Linux nothing binds a seat's lifetime to the host
		// (roomjob_unix.go), so a hard-killed host's seats are still running
		// with its session id, and this is the one command that can end them.
		// It acts only on a pid that is GONE — reapOrphans's own guard — and
		// the count is printed because "ended its session" is not a
		// measurement and "ended 3 processes" is.
		swept := reapOrphans(rep.File.PID)
		removeStaleTransport(rep.File.Pipe)
		if err := RemoveHostFile(councilDir); err != nil {
			return "", fmt.Errorf("councilhost: the host was already gone and its discovery file "+
				"could not be removed: %w", err)
		}
		if swept > 0 {
			noun := "processes were"
			if swept == 1 {
				noun = "process was"
			}
			return fmt.Sprintf("no host was running: %s.\n%d %s still running in that host's "+
				"session and %s ended.\nthe stale %s was removed.",
				rep.Reason, swept, noun, map[bool]string{true: "was", false: "were"}[swept == 1],
				filepath.Base(HostPath(councilDir))), nil
		}
		return fmt.Sprintf("no host was running: %s.\nthe stale %s was removed. nothing else changed.",
			rep.Reason, filepath.Base(HostPath(councilDir))), nil
	}

	pid := rep.File.PID
	if err := killProcess(pid); err != nil {
		return "", err
	}
	// A host that answered SIGTERM removed its own transport on the way out;
	// one that had to be killed could not, and this is the same act as
	// removing its discovery file.
	removeStaleTransport(rep.File.Pipe)
	if err := RemoveHostFile(councilDir); err != nil {
		return "", fmt.Errorf("councilhost: pid %d was ended and its discovery file could not be "+
			"removed: %w", pid, err)
	}
	return fmt.Sprintf(
		"ended the host, pid %d, and every seat it was holding.\n"+
			"the room's conversation is gone with it; the session ids are still in the saved room, so "+
			"`telltale council` rebuilds those seats.", pid), nil
}

// RoomKey names the room a workspace belongs to, for PipeName.
//
// # A hash, and the workspace is deliberately not readable in it
//
// A pipe name is visible to every process on the machine that cares to
// enumerate `\\.\pipe\`, and it is not protected by anything — the pipe's
// protection is its security descriptor, which PipeName's own doc states. So
// putting a path in the name would publish which directory the operator is
// working in to any local process, for no gain: the name only has to be stable
// and unique per workspace.
//
// SHA-256 truncated to sixteen hex characters. Truncated because a pipe name
// has a length ceiling and a full digest buys nothing here — the failure a
// collision causes is two workspaces sharing one room name, which the FIRST
// PIPE INSTANCE create then refuses out loud rather than silently merging.
//
// Cleaned and then folded by the PLATFORM's own rule, because two spellings of
// one directory must produce one room name — otherwise an operator can start a
// second host over a room they already have. Windows treats paths and pipe
// names case-insensitively and foldWorkspace lower-cases there; every other
// platform treats `/a/B` and `/a/b` as two directories, and folding them would
// be the same defect in the other direction.
func RoomKey(workspace string) string {
	sum := sha256.Sum256([]byte(foldWorkspace(filepath.Clean(workspace))))
	return hex.EncodeToString(sum[:])[:16]
}

// selfImageBase is this executable's own file name.
//
// A var rather than a function call at each site, so a test that has to describe
// a host running under a DIFFERENT binary name can say so. Nothing in production
// replaces it.
var selfImageBase = func() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("councilhost: could not find this binary, so a host's identity "+
			"cannot be checked: %w", err)
	}
	return filepath.Base(self), nil
}
