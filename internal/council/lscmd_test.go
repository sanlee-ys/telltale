package council

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// `telltale council ls` is a reader (design.md §7.27), so what these tests pin
// is the two properties a reader has: it says what is there, and it does not
// touch it.

func savedRoomFixture() SavedRoom {
	return SavedRoom{
		Version:   roomVersion,
		Workspace: "/home/dev/code/telltale",
		Seats:     Seats{Only: []model.VendorID{model.VendorClaude, model.VendorCodex}},
		Posture:   "write-gated",
		Turn:      7,
		Sessions: map[model.VendorID]string{
			model.VendorClaude: "sess-aaaa",
			model.VendorCodex:  "sess-bbbb",
		},
		BriefPath: "/home/dev/desk/brief.md",
		SavedAt:   time.Date(2026, 8, 31, 18, 4, 0, 0, time.UTC),
	}
}

func TestCouncilLsReportsWhatIsSaved(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	avail := map[model.VendorID]Availability{
		model.VendorClaude: AvailInstalled,
		model.VendorCodex:  AvailInstalled,
	}
	out := strings.Join(listRoomLines(re, avail, "/home/dev",
		time.Date(2026, 9, 1, 8, 4, 0, 0, time.UTC)), "\n")

	for _, want := range []string{
		"~/.telltale/council/room.json",
		"turn       7 was the last",
		"~/code/telltale",
		"claude, codex",
		"14h ago",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("council ls does not report %q:\n%s", want, out)
		}
	}
}

// The three per-seat states are three sentences (§4a.1). A saved id on a machine
// that cannot run the vendor is not the same fact as no saved id, and telling an
// operator on a second machine that a conversation is gone while its id sits on
// disk is exactly the collapse this repository exists to prevent.
func TestCouncilLsKeepsTheThreeSeatStatesApart(t *testing.T) {
	room := savedRoomFixture()
	// codex has a saved id and no binary here; gemini-class seats have neither.
	avail := map[model.VendorID]Availability{
		model.VendorClaude: AvailInstalled,
		model.VendorCodex:  AvailNotInstalled,
	}
	lines := seatLines(room, avail)
	got := map[string]string{}
	for _, l := range lines {
		f := strings.Fields(l)
		if len(f) > 1 {
			got[f[0]] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), f[0]))
		}
	}

	if want := "saved"; got["claude"] != want {
		t.Errorf("an installed seat with a saved id reads %q, want %q", got["claude"], want)
	}
	if !strings.Contains(got["codex"], "cannot run the vendor") {
		t.Errorf("a saved id for a vendor this machine lacks reads %q", got["codex"])
	}
	if got["cursor"] != "no thread saved" {
		t.Errorf("a seat with no saved id reads %q", got["cursor"])
	}
	// The three must not be spelled alike.
	if got["codex"] == got["cursor"] {
		t.Errorf("an unreachable saved id and no saved id render identically: %q", got["codex"])
	}
	if got["codex"] == got["claude"] {
		t.Errorf("an unreachable saved id and a reachable one render identically: %q", got["codex"])
	}
	// Every addressable vendor gets a line, so an absence is stated rather than
	// left as a gap the reader has to interpret.
	if len(lines) != len(addressableVendors()) {
		t.Errorf("council ls printed %d seat lines for %d addressable vendors",
			len(lines), len(addressableVendors()))
	}
}

// The mode must never claim a thread is alive: nothing it can read proves it.
func TestCouncilLsNeverClaimsAThreadIsLive(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	out := strings.Join(listRoomLines(re, map[model.VendorID]Availability{
		model.VendorClaude: AvailInstalled,
	}, "/home/dev", time.Now()), "\n")

	for _, forbidden := range []string{"resumable", "still live —", "is live"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("council ls claims %q:\n%s", forbidden, out)
		}
	}
	if !strings.Contains(out, "nothing here proves a thread is still live") {
		t.Errorf("council ls does not state its own limit:\n%s", out)
	}
}

// The saved posture is recorded and never restored (reattach's ruling). A
// listing that printed it bare would undo that by implication.
func TestCouncilLsSaysThePostureIsNotRestored(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	out := strings.Join(listRoomLines(re, nil, "/home/dev", time.Now()), "\n")

	if !strings.Contains(out, "write-gated") {
		t.Errorf("council ls drops the recorded posture:\n%s", out)
	}
	if !strings.Contains(out, "takes its posture from the command line") {
		t.Errorf("council ls prints the posture without saying it is not restored:\n%s", out)
	}
}

// The brief is a PATH in room.json and never its text. The listing says so, so
// nobody reads the line as the room having stored a private file.
func TestCouncilLsNamesTheBriefPathAndNotItsContent(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	out := strings.Join(listRoomLines(re, nil, "/home/dev", time.Now()), "\n")

	if !strings.Contains(out, "~/desk/brief.md") {
		t.Errorf("council ls drops the brief path:\n%s", out)
	}
	if !strings.Contains(out, "does not open it") {
		t.Errorf("council ls prints the brief path without stating it is unread:\n%s", out)
	}
}

// A file that exists and cannot be used is a state to REPORT. Failing the
// command over telltale's own damaged state is the mistake LoadRoom already
// refuses to make, and a second reader must not reintroduce it.
func TestCouncilLsReportsARefusedFileWithoutFailing(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json",
		Ignored: "the saved room file is not readable json"}
	out := strings.Join(listRoomLines(re, nil, "/home/dev", time.Now()), "\n")

	if !strings.Contains(out, "the saved room file is not readable json") {
		t.Errorf("council ls swallows LoadRoom's reason:\n%s", out)
	}
	if !strings.Contains(out, "left where it is") {
		t.Errorf("council ls does not say the refused file is left alone:\n%s", out)
	}
}

func TestCouncilLsSaysWhenNothingIsSaved(t *testing.T) {
	out := strings.Join(listRoomLines(Reattachment{}, nil, "/home/dev", time.Now()), "\n")

	if !strings.Contains(out, "no room is saved yet") {
		t.Errorf("council ls does not report an empty state:\n%s", out)
	}
	if !strings.Contains(out, "telltale council opens one") {
		t.Errorf("council ls reports an empty state with no way forward:\n%s", out)
	}
}

// THE BOUNDARY TEST. `telltale council ls` is the sixth reader
// (CLAUDE.md's read/write boundary), so a run of it must leave the state
// directory byte-identical — no rewrite of the file it read, no cache, no lock,
// no temp file left behind.
func TestCouncilLsWritesNothing(t *testing.T) {
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
	path := filepath.Join(dir, roomFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	beforeDir, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := ListRooms(&buf); err != nil {
		t.Fatalf("council ls failed on a good room: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("council ls printed nothing for a saved room")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("council ls rewrote the room file it read")
	}
	afterDir, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeDir) != len(afterDir) {
		t.Errorf("council ls changed the state directory: %d entries before, %d after",
			len(beforeDir), len(afterDir))
	}
}

// The listing carries no styling. It is a reader, on doctor's and history's
// precedent — words and no colour — and it lands in pipes.
func TestCouncilLsCarriesNoEscapes(t *testing.T) {
	re := Reattachment{Path: "/home/dev/.telltale/council/room.json", Room: savedRoomFixture()}
	for _, line := range listRoomLines(re, nil, "/home/dev", time.Now()) {
		if strings.ContainsRune(line, '\x1b') {
			t.Errorf("council ls carries an ANSI escape: %q", line)
		}
	}
}
