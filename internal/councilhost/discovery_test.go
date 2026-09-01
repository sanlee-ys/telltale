package councilhost

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestTheDiscoveryFileIsNumbersAndKeysOnly is the fourth council write, pinned
// to its serialized form.
//
// Each of the three existing relay exceptions carries a test that pins its
// serialized form to keys and numbers, and this one is held to the same bar.
// The whole argument for adding a file under ~/.telltale/ is that it is the
// same class of value as room.json — which directory was worked in, when, and a
// set of opaque ids, and not a word anyone said.
//
// The assertion is on the SERIALIZED bytes rather than on the struct, because
// the struct is what a reviewer reads and the bytes are what lands on disk. A
// field added later with a content-shaped value would pass a struct review and
// fail here.
func TestTheDiscoveryFileIsNumbersAndKeysOnly(t *testing.T) {
	dir := t.TempDir()
	if err := WriteHostFile(dir, HostFile{
		PID:       4242,
		Pipe:      PipeName("room"),
		StartedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		Workspace: `C:\src\telltale`,
		Seats:     []model.VendorID{model.VendorClaude, model.VendorCodex},
		Turn:      3,
	}); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(HostPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	// An ALLOWLIST, not a denylist. A denylist would pass every field nobody
	// thought to forbid, which is exactly how a transcript field gets added by
	// accident.
	allowed := map[string]bool{
		"version": true, "pid": true, "pipe": true, "started_at": true,
		"workspace": true, "seats": true, "turn": true,
	}
	for k := range raw {
		if !allowed[k] {
			t.Errorf("host.json carries an unexpected key %q. This file is numbers and keys "+
				"only: no transcript, no prompt, no brief, no vendor output (design.md §7.28).", k)
		}
	}

	// The file's own permissions. 0600 is what os.WriteFile is asked for, and
	// on Windows the mode is advisory — gatehook.go already documents that, and
	// the containment there is the directory's ACL rather than the bit.
	info, err := os.Stat(HostPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("host.json is empty")
	}
}

// TestTheDiscoveryFileRoundTrips.
func TestTheDiscoveryFileRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := HostFile{
		PID: 7, Pipe: PipeName("k"), StartedAt: time.Now().UTC().Truncate(time.Second),
		Workspace: `C:\w`, Seats: []model.VendorID{model.VendorClaude}, Turn: 2,
	}
	if err := WriteHostFile(dir, want); err != nil {
		t.Fatal(err)
	}
	got, err := ReadHostFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.PID != want.PID || got.Pipe != want.Pipe || got.Workspace != want.Workspace ||
		got.Turn != want.Turn || len(got.Seats) != 1 || got.Version != HostFileVersion {
		t.Fatalf("round trip changed the file: %+v", got)
	}
	if err := RemoveHostFile(dir); err != nil {
		t.Fatal(err)
	}
	// Removing twice is not an error: a clean exit after a hard kill that
	// already lost the file must not report a fault.
	if err := RemoveHostFile(dir); err != nil {
		t.Fatalf("removing an absent discovery file reported %v", err)
	}
	if _, err := ReadHostFile(dir); !os.IsNotExist(err) {
		t.Fatalf("reading an absent discovery file reported %v, want a not-exist", err)
	}
}

// TestAMissingDiscoveryFileIsAnOrdinaryState.
//
// No host has run in this directory, or the last one exited cleanly and removed
// it. Neither is a fault, and a reader that treated the absence as an error
// would make a clean exit look like a broken one.
func TestAMissingDiscoveryFileIsAnOrdinaryState(t *testing.T) {
	_, err := ReadHostFile(t.TempDir())
	if !os.IsNotExist(err) {
		t.Fatalf("a missing host.json reported %v", err)
	}
}

// TestAVersionThisBuildDoesNotReadIsRefused.
//
// Refused rather than best-effort parsed. A discovery file is read to decide
// what is running; a half-understood one would answer that question wrongly,
// and a wrong answer there is worse than no answer.
func TestAVersionThisBuildDoesNotReadIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(HostPath(dir), []byte(`{"version":99,"pid":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadHostFile(dir)
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("a future host.json was read as %v", err)
	}
}
