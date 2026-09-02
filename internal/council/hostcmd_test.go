package council

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/councilhost"
	"github.com/sanlee-ys/telltale/internal/model"
)

// noHostFixture is the ordinary state of a machine with no host running.
//
// A NAMED fixture rather than a zero value written at each call site, because
// "no host" is one of five states this listing renders and a bare
// councilhost.HostReport{} at seven call sites would stop saying which one is
// meant the first time the zero value changes.
func noHostFixture() councilhost.HostReport {
	return councilhost.HostReport{State: councilhost.HostNone}
}

// liveHostFixture is a host running with nobody in it.
func liveHostFixture(started time.Time) councilhost.HostReport {
	return councilhost.HostReport{
		State: councilhost.HostLive,
		File: councilhost.HostFile{
			Version: councilhost.HostFileVersion, PID: 4242,
			Pipe: councilhost.PipeName("fixture"), StartedAt: started,
			Workspace: "/home/dev/code/telltale",
			Seats:     []model.VendorID{model.VendorClaude}, Turn: 3,
		},
	}
}

// TestCouncilLsShowsALiveHostAndNamesTheWayBackIn.
//
// design.md §7.29 gives `telltale council ls` a live-host section, and the
// section is worth nothing if it reports a running process without naming the
// two commands that reach it. §9.17's tell: a state with no remedy is this
// room's stated defect.
func TestCouncilLsShowsALiveHostAndNamesTheWayBackIn(t *testing.T) {
	now := time.Now()
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	out := strings.Join(listRoomLines(re, nil, liveHostFixture(now.Add(-time.Hour)),
		"/home/dev", now), "\n")

	if !strings.Contains(out, "RUNNING, pid 4242") {
		t.Errorf("council ls does not report a live host:\n%s", out)
	}
	if !strings.Contains(out, "telltale council` rejoins it") {
		t.Errorf("council ls reports a live host with no way back into it:\n%s", out)
	}
	if !strings.Contains(out, "telltale council kill") {
		t.Errorf("council ls reports a live host with no way to end it:\n%s", out)
	}
	// The conversation's whereabouts, stated. It is in one process's memory and
	// on no disk, which is the fact an operator needs before deciding whether to
	// kill the thing (§7.28's read/write boundary).
	if !strings.Contains(out, "nowhere on disk") {
		t.Errorf("council ls does not say where a live host's conversation lives:\n%s", out)
	}
}

// TestCouncilLsRendersALiveHostAndADeadOneDifferently is §4a.1 on this surface.
//
// A host that is running and a discovery file whose host is gone are two facts,
// and an operator acts on them in opposite directions: one is a room to walk
// back into, the other is a stale file over a conversation that must be rebuilt.
func TestCouncilLsRendersALiveHostAndADeadOneDifferently(t *testing.T) {
	now := time.Now()
	live := liveHostFixture(now.Add(-time.Hour))
	dead := councilhost.HostReport{State: councilhost.HostDead, File: live.File,
		Reason: "nothing is listening on telltale-council-fixture"}

	liveOut := strings.Join(hostLines(live, now), "\n")
	deadOut := strings.Join(hostLines(dead, now), "\n")
	if liveOut == deadOut {
		t.Fatalf("a live host and a dead one render alike:\n%s", liveOut)
	}
	if !strings.Contains(deadOut, "that host is gone") {
		t.Errorf("a dead host does not say it is gone:\n%s", deadOut)
	}
	if strings.Contains(deadOut, "RUNNING") {
		t.Errorf("a dead host is described as running:\n%s", deadOut)
	}
	// A host somebody is already in is a THIRD state, and it must not read as
	// either of the first two: it is a refusal, not an invitation and not a
	// death.
	busy := councilhost.HostReport{State: councilhost.HostBusy, File: live.File}
	busyOut := strings.Join(hostLines(busy, now), "\n")
	if busyOut == liveOut || busyOut == deadOut {
		t.Fatalf("a host with a client in it renders like another state:\n%s", busyOut)
	}
	if !strings.Contains(busyOut, "one client at a time") {
		t.Errorf("a busy host does not say why it cannot be joined:\n%s", busyOut)
	}
}

// TestCouncilLsNeverSaysAHostIsRunningWhenNoneIs.
//
// The zero-versus-absent rule, on a process. A machine with no host must say so
// and must name the command that opens one, rather than printing nothing and
// leaving an operator to guess whether the section is missing or the host is.
func TestCouncilLsNeverSaysAHostIsRunningWhenNoneIs(t *testing.T) {
	out := strings.Join(hostLines(noHostFixture(), time.Now()), "\n")
	if strings.Contains(out, "RUNNING") {
		t.Fatalf("council ls reports a host on a machine with none:\n%s", out)
	}
	if !strings.Contains(out, "none is running") {
		t.Errorf("council ls does not state the absence:\n%s", out)
	}
	if !strings.Contains(out, "--host") {
		t.Errorf("council ls states an absence with no way to change it:\n%s", out)
	}
}

// TestCouncilLsLeavesAStaleHostFileAlone is the reader contract, extended to the
// file design.md §7.29 taught this mode to read.
//
// §7.27's contract says `ls` writes nothing, and a stale host.json is the exact
// temptation: it is obviously garbage and removing it would obviously be
// helpful. A reader that tidied would be a writer. The room removes it (Rejoin);
// the listing only reports it.
func TestCouncilLsLeavesAStaleHostFileAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".telltale", "council")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveRoom(savedRoomFixture()); err != nil {
		t.Fatal(err)
	}
	// A pid that certainly names no telltale host: 0 is never a process, so the
	// probe reports the file stale without this test having to invent a live
	// process to be wrong about.
	if err := councilhost.WriteHostFile(dir, councilhost.HostFile{
		PID: 0, Pipe: councilhost.PipeName("telltale-ls-stale-fixture"),
		StartedAt: time.Now().Add(-time.Hour), Workspace: home,
	}); err != nil {
		t.Fatal(err)
	}
	path := councilhost.HostPath(dir)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := ListRooms(&buf); err != nil {
		t.Fatalf("council ls failed with a stale host file present: %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("council ls REMOVED the stale host file. That makes it a writer. §7.27's "+
			"contract is that it writes nothing, including no cleanup — the room removes "+
			"this file, the listing only reports it: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("council ls rewrote the host file it read")
	}
}

// TestTheHostSpawnGuardIsArmed proves the wrap actually fires.
//
// It is the same assertion internal/councilhost makes about its own guard, and
// it earns its place for the same reason: a wrap that silently stopped panicking
// would leave every future test free to start a process that spawns billed
// vendor turns, and nothing would say so. CI cannot notice — CI has no vendors,
// so the host it started would dispatch to nobody and the run would go green.
//
// countSpawns is NOT called here. That is the point: this is the unstubbed path.
func TestTheHostSpawnGuardIsArmed(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("startHostedRoom did NOT panic. That call starts telltale's own binary, " +
				"which resolves on any machine that built it, and the process it starts " +
				"spawns real vendor CLIs two processes away from this test.")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "REAL council host") {
			t.Fatalf("the guard panicked with something else: %v", r)
		}
	}()
	_, _ = startHostedRoom(councilhost.ClientConfig{Workspace: t.TempDir(), RoomKey: "guard-probe"})
}

// TestTheJoinGuardLetsADeadPipeThrough is the guard's other half, and it is the
// half that keeps the rule honest.
//
// A pipe name nothing is listening on reaches nothing and costs nothing, so it
// is let through to the real call and fails there — exactly the bargain
// refuseRealVendor strikes with a binary exec cannot find. Without this, the
// join guard would be an unconditional refusal wearing a rule's clothes, and the
// tests that want to exercise the could-not-join branch would have nowhere to
// stand.
func TestTheJoinGuardLetsADeadPipeThrough(t *testing.T) {
	_, err := joinHostedRoom(councilhost.JoinConfig{
		PipeName:    councilhost.PipeName("telltale-guard-probe-nothing-listens-here"),
		DialTimeout: 10 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("joining a pipe nothing is listening on succeeded")
	}
}

// TestRoomKeyIsStableAndHidesTheWorkspace.
//
// Two claims, and both are load-bearing. STABLE, because two spellings of one
// directory must not produce two room names — an operator would then be able to
// start a second host over a room they already have. HIDDEN, because a pipe name
// is visible to every local process that enumerates \\.\pipe\, and publishing
// which directory somebody is working in buys nothing.
func TestRoomKeyIsStableAndHidesTheWorkspace(t *testing.T) {
	a := councilhost.RoomKey(`C:\Users\dev\code\telltale`)
	b := councilhost.RoomKey(`C:\Users\dev\code\telltale\`)
	c := councilhost.RoomKey(`c:\users\dev\code\TELLTALE`)
	if a != b {
		t.Errorf("a trailing separator changed the room key: %q vs %q", a, b)
	}
	if a != c {
		t.Errorf("case changed the room key: %q vs %q", a, c)
	}
	if strings.Contains(strings.ToLower(councilhost.PipeName(a)), "telltale\\") ||
		strings.Contains(strings.ToLower(a), "dev") {
		t.Errorf("the room key carries the workspace path: %q", a)
	}
	if councilhost.RoomKey(`C:\Users\dev\code\other`) == a {
		t.Error("two different workspaces share one room key")
	}
}
