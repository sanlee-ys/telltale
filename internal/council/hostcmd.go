package council

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// The hosted room's council-side half (design.md §7.29).
//
// # Why this lives in package council and not in cmd/telltale
//
// The host takes a ROSTER and a council directory from its caller and detects
// nothing for itself, which is what keeps the dependency one-way — boundary_test.go's
// TestTheHostDoesNotImportTheRoom fails the build if internal/councilhost ever
// reaches back here. Somebody therefore has to do the detection, resolve the
// workspace and read the saved room, and every one of those already lives in
// this package. Doing it in main.go instead would put four council internals on
// a command's argv and leave the two halves of one decision in two files.
//
// The direction is council -> councilhost and never the reverse. That is the
// direction §7.28's last limitation says the next slice travels in, when
// council's own Model becomes the client's renderer.

// startHostedRoom and joinHostedRoom are this package's route to a host, and
// they are vars for one reason: the test guard.
//
// # main_test.go's own note asked for exactly this
//
// council's TestMain says, of internal/councilhost's guard: "Nothing in package
// `council` reaches that host, so `countSpawns` below needs no entry for it. If
// that ever changes — if the room here grows a path that starts or joins a host
// — the var behind it belongs in this wrap and in countSpawns, in the same
// change." design.md §7.29 is that change, so these are those vars.
//
// The hole they close is real and it is two processes deep.
// internal/councilhost's TestMain wraps `startHost` for THAT package's test
// binary. `go test ./internal/council` is a different binary, `startHost` is
// unexported, and nothing there wraps it — so a council test that reached
// StartHosted would start a real `telltale.exe council host`, which resolves on
// any machine that built it, and that process would then spawn real vendor CLIs
// against the operator's account. CI cannot catch it: CI has no vendors, so the
// host it started would sit there dispatching to nobody and the run would go
// green.
//
// JOINING is guarded too, and it is not a spawn. It reaches a host that is
// ALREADY holding vendor processes, and every turn a test then dispatched would
// be billed by seats this package never started. The guard's rule is the same
// question the operating system is about to ask, phrased for a pipe: a name
// that does not exist reaches nothing and is let through to fail, and a name
// that DOES exist is a live room about to take a turn.
var (
	startHostedRoom = councilhost.Open
	joinHostedRoom  = councilhost.Join
)

// ErrRoomHeld is returned when a host is running and another client is in it.
//
// A SENTINEL rather than a message, because the caller has already printed the
// sentence and only needs to know the exit code is not zero. §7.29 rules this a
// refusal rather than a fall-through to a local room: falling through would open
// a second room over the same workspace on the same saved session ids, which is
// two rooms rebuilding one conversation.
var ErrRoomHeld = errors.New("council: a host is running and a client is already in it")

// HostedOutcome says whether the hosted path took the launch.
type HostedOutcome int

const (
	// HostedNotTaken: no live host, so the caller opens the ordinary room. This
	// is the daily answer on a machine that has never run one.
	HostedNotTaken HostedOutcome = iota
	// HostedHandled: a hosted room ran and has finished. The caller must not
	// then open a second room.
	HostedHandled
)

// clientWidth is the width the plain client draws at.
//
// Fixed rather than read from the terminal, and the reason is Render's own
// purity rule: this renderer is pure over the room, and a client that queried
// the console would have to do it on every frame to be right. 100 columns is
// where the room's own goldens sit. A resize is free when it lands, because a
// client hands its own width to a pure function — §7.28 says that is the whole
// reason no resize protocol exists.
const clientWidth = 100

// Rejoin takes over a live host if there is one, and otherwise says so.
//
// This is what `telltale council` with no arguments consults BEFORE opening the
// ordinary room. Four answers, and §4a.1 is why none of them share a sentence
// with another:
//
//   - No discovery file: the caller opens the ordinary TUI room, unchanged.
//   - A live host, free: this client rejoins it. Nothing is rebuilt and no
//     session is resumed, and the notice says both.
//   - A live host somebody is in: REFUSED, naming `telltale council kill`.
//   - A discovery file whose host is gone: the died notice, then the ordinary
//     room, which rebuilds from room.json (§9.52).
func Rejoin(in io.Reader, out io.Writer) (HostedOutcome, error) {
	dir, err := councilDir()
	if err != nil {
		// No resolvable home directory. There can be no discovery file, so there
		// is no host — and telltale's own state being unreachable must never be
		// the reason the room refuses to open, which is the rule LoadRoom
		// already follows one file over.
		return HostedNotTaken, nil
	}
	rep := councilhost.Probe(dir)
	switch rep.State {
	case councilhost.HostNone:
		return HostedNotTaken, nil
	case councilhost.HostUnreadable:
		// Reported and stepped over. A discovery file that cannot be read is a
		// state to name, never a reason a room refuses to open.
		fmt.Fprintf(out, "the host discovery file could not be read: %s\n"+
			"opening the ordinary room instead.\n\n", rep.Reason)
		return HostedNotTaken, nil
	case councilhost.HostBusy:
		fmt.Fprintln(out, councilhost.RenderHostBusy(rep.File))
		return HostedHandled, ErrRoomHeld
	case councilhost.HostDead:
		roomPath, perr := RoomPath()
		if perr != nil {
			roomPath = "the saved room"
		}
		fmt.Fprintln(out, councilhost.RenderHostDied(rep.File, roomPath))
		// The stale file is REMOVED here and never by `telltale council ls`.
		// The room is already a writer of this directory — it is council's one
		// ratified write, and host.json is the file this same feature adds — so
		// removing a record of a process that is provably gone is the room
		// tidying its own state. A reader that did it would stop being a
		// reader, which is why §7.27's contract forbids exactly that in `ls`.
		if rerr := councilhost.RemoveHostFile(dir); rerr != nil {
			fmt.Fprintf(out, "its discovery file could not be removed: %v\n", rerr)
		}
		fmt.Fprintln(out)
		return HostedNotTaken, nil
	}

	c, err := joinHostedRoom(councilhost.JoinConfig{PipeName: rep.File.Pipe})
	if err != nil {
		// The probe said live and the connection says otherwise. That window is
		// real — a host can end between the two — and it is reported as what it
		// is rather than retried into a story.
		fmt.Fprintf(out, "a host was listening and it could not be joined: %v\n"+
			"opening the ordinary room instead.\n\n", err)
		return HostedNotTaken, nil
	}
	fmt.Fprintf(out, "%s\n\n", councilhost.RenderRejoined(rep.File))
	return HostedHandled, runHostedClient(c, in, out)
}

// StartHosted opens a room in a host of its own and drives it from here.
//
// This is `telltale council --host`, and it is an opt-in flag rather than a
// change to the daily command. §7.29 states the reason at length: `telltale
// council` runs the single-process room and always has, so there is no host for
// a key in that TUI to detach from, and a hint on the help panel for a key that
// does nothing would be the honest-gauge failure this project exists to prevent,
// spent on its own surface.
func StartHosted(opts Options, in io.Reader, out io.Writer) error {
	re, err := LoadRoom()
	if err != nil && !errors.Is(err, ErrNoSavedRoom) {
		re = Reattachment{}
	}
	ws, _, err := openWorkspace(opts, re)
	if err != nil {
		return err
	}
	seats := seatsFor(opts.Seats, re.Room.Seats, re.Active())

	if opts.Write {
		// Said BEFORE the room opens, because it is the one thing about a
		// hosted write room an operator cannot discover by looking at it. §7.28
		// refuses to host a gated room at all, so a hosted room that is not
		// read-only runs every tool call with nobody to ask — which is what
		// --auto means on the room's own surface (dispatch.go's seatPosture).
		// §7.29's unwatched-write ruling then refuses the detach, and finding
		// that out only when you try to leave would be a refusal arriving at
		// the worst possible moment.
		fmt.Fprintln(out, "this hosted room writes to the workspace and CANNOT ask first: the host does not")
		fmt.Fprintln(out, "carry a gate, so every seat runs as though --auto were typed. it will not detach.")
		fmt.Fprintln(out, "open it with --read to get a room you can leave.")
		fmt.Fprintln(out)
	}

	c, err := startHostedRoom(councilhost.ClientConfig{
		Workspace: ws,
		RoomKey:   councilhost.RoomKey(ws),
		Seats:     hostSeatVendors(Detect(), seats),
		Read:      !opts.Write,
	})
	if err != nil {
		return err
	}
	return runHostedClient(c, in, out)
}

// runHostedClient runs the plain client and reports how it ended.
//
// The four outcomes are printed differently on purpose, and only one of them
// prints nothing: a detach has already said its piece inside RunClient, at the
// moment it happened.
func runHostedClient(c *councilhost.Client, in io.Reader, out io.Writer) error {
	outcome, err := councilhost.RunClient(c, in, out, clientWidth)
	switch outcome {
	case councilhost.OutcomeDetached:
		return nil
	case councilhost.OutcomeHostExited:
		fmt.Fprintf(out, "\n%s\n", councilhost.RenderHostExit())
		return nil
	default:
		// Ended, or input closed. RunClient has ALREADY closed on both of those
		// paths, and it has to — Close is what sends the shutdown frame and then
		// waits for the host to finish killing the seats, and that has to happen
		// before RunClient returns or the outcome would be reported before it
		// was true. A second Close here would write a frame down a closed pipe
		// and wait a second time on a process already reaped, so there is none.
		return err
	}
}

// KillHostedRoom ends a running host and every seat with it.
//
// `telltale council kill` — a sub-noun before the flag set, matching `ls` and
// `host`. It refuses rather than guessing when the probe disagrees with the
// file, and councilhost.KillHost carries the whole reason: killing a pid a
// stale file names is the one failure this command could make that nothing
// could undo.
func KillHostedRoom(w io.Writer) error {
	dir, err := councilDir()
	if err != nil {
		return fmt.Errorf("council kill: the state directory could not be found, so no host "+
			"could be looked up: %w", err)
	}
	line, err := councilhost.KillHost(dir)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, line)
	return nil
}

// HostRoster turns detection plus a --vendor request into the host's seats.
//
// A seat that resolved no binary is still passed through when it was NAMED,
// because the host draws it as undrivable with the reason on its card — the same
// bargain ParseSeats documents, where asking for a seat forces it on screen so
// the user is owed the card explaining why it is not there. An unnamed seat that
// resolved nothing is simply left out.
func HostRoster(found []VendorInfo, room Seats) []councilhost.RosterEntry {
	byVendor := map[model.VendorID]VendorInfo{}
	for _, v := range found {
		byVendor[v.Vendor] = v
	}
	// A typed list keeps ITS order, because position is the navigation on every
	// council surface and re-sorting a roster somebody typed would draw a room
	// they did not ask for.
	if len(room.Only) > 0 {
		out := make([]councilhost.RosterEntry, 0, len(room.Only))
		for _, v := range room.Only {
			out = append(out, councilhost.RosterEntry{Vendor: v, Binary: byVendor[v].Binary})
		}
		return out
	}
	out := make([]councilhost.RosterEntry, 0, len(found))
	for _, v := range found {
		if !room.All && v.Avail != AvailInstalled {
			continue
		}
		out = append(out, councilhost.RosterEntry{Vendor: v.Vendor, Binary: v.Binary})
	}
	return out
}

// hostSeatVendors is HostRoster's vendor ids only.
//
// The CLIENT passes ids on argv and the HOST resolves the binaries, so no
// resolved path ever travels on a command line. That is §7.28's own split and it
// is what keeps a roster from carrying a machine's filesystem into a process
// argument list.
func hostSeatVendors(found []VendorInfo, room Seats) []model.VendorID {
	entries := HostRoster(found, room)
	out := make([]model.VendorID, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Vendor)
	}
	return out
}

// councilDir is the directory room.json and host.json share.
func councilDir() (string, error) {
	p, err := RoomPath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// hostLines is `telltale council ls`'s live-host section.
//
// # It stays a READER, and this is where that is easiest to break
//
// §7.27's contract is unchanged by this section and is worth restating exactly
// where the temptation lives:
//
//   - It WRITES nothing, INCLUDING no cleanup of a stale host.json. A reader
//     that tidied would be a writer. The room removes that file (see Rejoin);
//     the listing only reports it.
//   - It CONNECTS to nothing. councilhost.Probe asks whether a pipe NAME exists
//     and never opens it — which is exactly why that probe had to be built the
//     way it was, because a dialling probe would make `ls` capable of ending the
//     room it was listing.
//   - It SPAWNS nothing and BINDS nothing.
//
// Pure over its arguments, like listRoomLines beside it, so the words can be
// tested without a live host.
func hostLines(rep councilhost.HostReport, now time.Time) []string {
	switch rep.State {
	case councilhost.HostNone:
		return []string{
			"  host       none is running. `telltale council --host` opens a room in one.",
		}
	case councilhost.HostUnreadable:
		return []string{
			"  host       the discovery file could not be read: " + rep.Reason,
			"             it is left where it is. this mode does not repair telltale's own state.",
		}
	case councilhost.HostDead:
		return []string{
			"  host       a discovery file names pid " + itoa(rep.File.PID) + " and that host is gone",
			"             " + rep.Reason,
			"             the file is stale. `telltale council` removes it and rebuilds the seats;",
			"             `telltale council kill` removes it and does nothing else.",
		}
	case councilhost.HostBusy:
		return []string{
			"  host       RUNNING, pid " + itoa(rep.File.PID) + ", started " +
				rep.File.StartedAt.Format(time.RFC3339) + " (" + age(now.Sub(rep.File.StartedAt)) + ")",
			"             a client is already in it. one client at a time.",
			"             turn " + itoa(rep.File.Turn) + " so far. `telltale council kill` ends it and every seat.",
		}
	default:
		return []string{
			"  host       RUNNING, pid " + itoa(rep.File.PID) + ", started " +
				rep.File.StartedAt.Format(time.RFC3339) + " (" + age(now.Sub(rep.File.StartedAt)) + ")",
			"             nobody is in it. `telltale council` rejoins it;",
			"             `telltale council kill` ends it and every seat with it.",
			"             turn " + itoa(rep.File.Turn) + " so far. the conversation is in that process's memory",
			"             and nowhere on disk, so it dies with the host.",
		}
	}
}

// probeHost reads the live-host state for the listing.
//
// Split from hostLines so the words stay pure over their arguments and can be
// tested with no host anywhere — availability()'s own rule, applied to a second
// fact: the function that decides the WORDS must not also decide the FACTS.
func probeHost() councilhost.HostReport {
	dir, err := councilDir()
	if err != nil {
		return councilhost.HostReport{State: councilhost.HostUnreadable,
			Reason: "the state directory could not be found: " + err.Error()}
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		// No council directory at all is no host, and it is not a fault. It is
		// the ordinary state of a machine that has never opened a room.
		return councilhost.HostReport{State: councilhost.HostNone}
	}
	return councilhost.Probe(dir)
}
