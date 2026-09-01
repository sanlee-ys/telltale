package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheSuiteRunsAgainstASandboxHome gives the redirect in TestMain a name and
// a failure of its own.
//
// The check after m.Run() only speaks when something already wrote the
// operator's disk, and it speaks after the whole suite has finished. This one
// fails the moment the redirect stops being in force — if a later edit removes
// it, if MkdirTemp lands somewhere unexpected, or if the two paths this package
// resolves under the home ever stop agreeing with it.
//
// It asserts the two paths council can actually write, rather than asserting on
// the environment variables: the variables are the mechanism, and the mechanism
// is the thing most likely to change. os.UserHomeDir reads USERPROFILE on
// Windows and HOME elsewhere, so a test that read only one of them would pass
// on the wrong platform for free.
func TestTheSuiteRunsAgainstASandboxHome(t *testing.T) {
	if sandboxHome == "" {
		t.Fatal("TestMain made no sandbox home, so every test in this package writes the operator's own")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("the suite cannot resolve a home at all: %v", err)
	}
	if home != sandboxHome {
		t.Fatalf("os.UserHomeDir() = %s, want the sandbox %s", home, sandboxHome)
	}
	if operatorHome != "" && home == operatorHome {
		t.Fatalf("the suite resolves the operator's real home, %s", operatorHome)
	}

	// The room file. This is the one that was measured corrupted.
	got, err := RoomPath()
	if err != nil {
		t.Fatalf("RoomPath failed under the sandbox home: %v", err)
	}
	want := filepath.Join(sandboxHome, ".telltale", "council", roomFile)
	if got != want {
		t.Errorf("RoomPath() = %s, want %s", got, want)
	}

	// The artifact store, which MkdirAlls its directory on construction — so
	// merely calling NewArtifactStore creates a directory in whichever home is
	// in force.
	store, err := NewArtifactStore()
	if err != nil {
		t.Fatalf("NewArtifactStore failed under the sandbox home: %v", err)
	}
	prefix := sandboxHome + string(os.PathSeparator)
	if !strings.HasPrefix(store.baseDir, prefix) {
		t.Errorf("artifact store base = %s, want a path under the sandbox %s", store.baseDir, sandboxHome)
	}
}

// TestTheStateDiffNamesWhatMoved covers the reporting half of the guard.
//
// The check in TestMain runs once, after every test, and only on a machine
// where the defect is live — so without this it would ship untested and the
// first person to trip it would get whatever the code happens to do. Each case
// is one thing that can happen to a file.
func TestTheStateDiffNamesWhatMoved(t *testing.T) {
	before := map[string]string{
		"room.json":           "259 bytes, modified 2026-09-01T13:44:14Z",
		"artifacts/turn-1.md": "12 bytes, modified 2026-08-04T00:46:00Z",
		"abandoned-v1.json":   "313 bytes, modified 2026-08-04T14:55:00Z",
	}
	after := map[string]string{
		"room.json":           "204 bytes, modified 2026-09-01T18:02:41Z",
		"artifacts/turn-1.md": "12 bytes, modified 2026-08-04T00:46:00Z",
		"artifacts/turn-2.md": "40 bytes, modified 2026-09-01T18:02:41Z",
	}

	diff := councilStateDiff(before, after)
	if len(diff) != 3 {
		t.Fatalf("diff = %d lines, want 3:\n%s", len(diff), joinLines(diff))
	}
	joined := joinLines(diff)
	for _, want := range []string{
		"created:   artifacts/turn-2.md",
		"deleted:   abandoned-v1.json",
		"rewritten: room.json",
		"259 bytes",
		"204 bytes",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("diff does not report %q:\n%s", want, joined)
		}
	}
	// The untouched file is not news, and a guard that reports it is a guard
	// people learn to skim.
	if strings.Contains(joined, "turn-1.md") {
		t.Errorf("diff reports a file nothing touched:\n%s", joined)
	}

	if got := councilStateDiff(before, before); len(got) != 0 {
		t.Errorf("an unchanged directory diffed as %v", got)
	}
}

// TestAnAbsentStateDirectorySnapshotsEmpty is the CI case, and the case on any
// machine that has never run `telltale council`. It must be a pass, not an
// error — otherwise the guard fails everywhere it has nothing to guard.
func TestAnAbsentStateDirectorySnapshotsEmpty(t *testing.T) {
	empty := t.TempDir()
	if snap := councilStateSnapshot(empty); len(snap) != 0 {
		t.Errorf("a home with no .telltale snapshotted %v", snap)
	}
	if snap := councilStateSnapshot(""); len(snap) != 0 {
		t.Errorf("an unresolvable home snapshotted %v", snap)
	}
}

// TestTheSnapshotSeesAWriteUnderTheStateDirectory proves the snapshot half
// actually observes a file, rather than returning empty for every input and
// making the guard silently vacuous.
func TestTheSnapshotSeesAWriteUnderTheStateDirectory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".telltale", "council", "artifacts")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-1-claude.md"), []byte("body"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := councilStateSnapshot(home)
	if len(before) != 1 {
		t.Fatalf("snapshot = %v, want the one file", before)
	}
	if err := os.WriteFile(filepath.Join(home, ".telltale", "council", roomFile), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	diff := councilStateDiff(before, councilStateSnapshot(home))
	if len(diff) != 1 || !strings.Contains(diff[0], roomFile) {
		t.Fatalf("a new room file diffed as %v", diff)
	}
}
