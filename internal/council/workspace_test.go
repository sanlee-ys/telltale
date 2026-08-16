package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reopened room comes back POINTED AT the directory it was saved in, and says
// so honestly when it cannot.
//
// The gap these pin was reported from a live 5/5 room on 2026-08-16: the
// reattach restored the roster and the workspace cell read `~`, and nothing in
// the suite could say which half had failed. It could not, because the decision
// was a switch inside Run and Run enters the alternate screen — so the first
// test here is the seam as much as the property. openWorkspace is that seam.
//
// Every path below is synthesized. The saved workspaces are temp directories
// and invented names under a temp home; none is a directory on the machine
// running the test.

// openedRoom is what Run does between LoadRoom and the model: load the saved
// room, decide the workspace, hand both to the constructor. Built here rather
// than reusing reattachedModel because that helper skips the decision entirely
// — it hands the model a Reattachment and lets stateWith take the cwd, which is
// the one step this file exists to test.
func openedRoom(t *testing.T, opts Options) *Model {
	t.Helper()
	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("the planted room did not load: %v", err)
	}
	ws, gone, werr := openWorkspace(opts, re)
	if werr != nil {
		t.Fatalf("openWorkspace refused: %v", werr)
	}
	re.WorkspaceGone = gone
	opts.Dir = ws
	return newWithBrief(opts, Brief{}, GateHook{}, re)
}

// TestASavedWorkspaceIsRestored is the reported symptom, stated as the property
// it violated: a room reopened with no flags works where it was saved, not where
// the terminal happened to be.
func TestASavedWorkspaceIsRestored(t *testing.T) {
	tempHome(t)
	ws := t.TempDir()
	if err := SaveRoom(savedRoom(ws)); err != nil {
		t.Fatal(err)
	}

	m := openedRoom(t, Options{})
	if !sameDir(m.st.Workspace, ws) {
		t.Fatalf("workspace = %q, want the saved %q — the reopened room lost where it was",
			m.st.Workspace, ws)
	}
	if strings.Contains(m.st.Notice, "no longer exists") {
		t.Errorf("a restored workspace was reported as missing: %q", m.st.Notice)
	}
}

// TestARelativeSavedWorkspaceIsResolved: the file holds whatever the saving room
// had, and every other consumer of a workspace in this package is handed an
// absolute path. A room that restored a relative one would dispatch four agents
// against a directory that means something different in every terminal.
func TestARelativeSavedWorkspaceIsResolved(t *testing.T) {
	tempHome(t)
	room := savedRoom(".")
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}

	ws, gone, err := openWorkspace(Options{}, mustLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if gone != "" {
		t.Fatalf("the current directory was reported gone: %q", gone)
	}
	if !filepath.IsAbs(ws) {
		t.Errorf("workspace = %q, want an absolute path", ws)
	}
}

// TestAGoneSavedWorkspaceOpensHereAndSaysSo is the honest fallback. A renamed
// repo, a removed git worktree or an unmounted drive must not make the room
// refuse to open — and must not be silent either, because the next completed
// turn writes the fallback over the saved workspace and the record of where the
// room was is then gone for good.
func TestAGoneSavedWorkspaceOpensHereAndSaysSo(t *testing.T) {
	home := tempHome(t)
	gone := filepath.Join(home, "code", "renamed-away")
	if err := SaveRoom(savedRoom(gone)); err != nil {
		t.Fatal(err)
	}

	m := openedRoom(t, Options{})
	if !sameDir(m.st.Workspace, resolveWorkspace("")) {
		t.Errorf("workspace = %q, want the current directory", m.st.Workspace)
	}
	if !strings.Contains(m.st.Notice, "no longer exists") {
		t.Errorf("the notice does not say the saved workspace is gone: %q", m.st.Notice)
	}
	if !strings.Contains(m.st.Notice, "renamed-away") {
		t.Errorf("the notice does not name the workspace it refused: %q", m.st.Notice)
	}
}

// TestAFileSittingWhereTheWorkspaceWasIsGoneToo: os.Stat succeeds on it, so a
// check that only asked whether the path exists would point four agents at a
// file.
func TestAFileSittingWhereTheWorkspaceWasIsGoneToo(t *testing.T) {
	home := tempHome(t)
	notADir := filepath.Join(home, "was-a-repo")
	if err := os.WriteFile(notADir, []byte("the repo is a file now"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveRoom(savedRoom(notADir)); err != nil {
		t.Fatal(err)
	}

	ws, gone, err := openWorkspace(Options{}, mustLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if gone != notADir {
		t.Errorf("gone = %q, want the file %q", gone, notADir)
	}
	if !sameDir(ws, resolveWorkspace("")) {
		t.Errorf("workspace = %q, want the current directory", ws)
	}
}

// TestATypedCdBeatsTheSavedWorkspace, and is not reported as a missing one. The
// two used to print the same sentence: "the room was in A; it is now in B" says
// nothing about whether A still exists.
func TestATypedCdBeatsTheSavedWorkspace(t *testing.T) {
	tempHome(t)
	saved, typed := t.TempDir(), t.TempDir()
	if err := SaveRoom(savedRoom(saved)); err != nil {
		t.Fatal(err)
	}

	ws, gone, err := openWorkspace(Options{Dir: typed}, mustLoad(t))
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(ws, typed) {
		t.Errorf("workspace = %q, want the typed %q", ws, typed)
	}
	if gone != "" {
		t.Errorf("a --cd override was reported as a missing workspace: %q", gone)
	}
}

// TestAFreshRoomTakesNoWorkspaceFromTheOfferedOne. --fresh declines the saved
// room; taking its directory anyway would restore the one piece of it the user
// can see while claiming nothing was restored.
func TestAFreshRoomTakesNoWorkspaceFromTheOfferedOne(t *testing.T) {
	tempHome(t)
	saved := t.TempDir()
	if err := SaveRoom(savedRoom(saved)); err != nil {
		t.Fatal(err)
	}

	re := mustLoad(t)
	re.Offered = true
	ws, gone, err := openWorkspace(Options{}, re)
	if err != nil {
		t.Fatal(err)
	}
	if sameDir(ws, saved) {
		t.Errorf("workspace = %q — --fresh reopened the declined room's directory", ws)
	}
	if gone != "" {
		t.Errorf("gone = %q, want empty — nothing was refused", gone)
	}
}

// TestARefusedSavedRoomTakesNoWorkspace: a corrupt or wrong-schema file has no
// usable workspace in it, and a room that took one anyway would act on the one
// field of a file it had just announced it could not read.
func TestARefusedSavedRoomTakesNoWorkspace(t *testing.T) {
	tempHome(t)
	ignored := Reattachment{Path: "-", Ignored: "the saved room file is not readable json",
		Room: SavedRoom{Workspace: filepath.Join(t.TempDir(), "unreachable")}}

	ws, gone, err := openWorkspace(Options{}, ignored)
	if err != nil {
		t.Fatal(err)
	}
	if !sameDir(ws, resolveWorkspace("")) {
		t.Errorf("workspace = %q, want the current directory", ws)
	}
	if gone != "" {
		t.Errorf("gone = %q, want empty — no workspace was ever offered", gone)
	}
}

func mustLoad(t *testing.T) Reattachment {
	t.Helper()
	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("the planted room did not load: %v", err)
	}
	if !re.Active() {
		t.Fatalf("the planted room is not usable: %q", re.Ignored)
	}
	return re
}
