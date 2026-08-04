package council

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestParseRoomCommand(t *testing.T) {
	cases := []struct {
		draft string
		arg   string
		ok    bool
	}{
		{"/cd", "", true},
		{"/cd kb-agent", "kb-agent", true},
		{"  /cd   ../elsewhere  ", "../elsewhere", true},
		// Not commands: the vocabulary stolen from the conversation is exactly
		// one verb, and nothing that merely resembles it.
		{"/cdx", "", false},
		{"/CD kb-agent", "", false},
		{"tell me about /cd", "", false},
		{"@claude /cd kb-agent", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		arg, ok := parseRoomCommand(c.draft)
		if ok != c.ok || arg != c.arg {
			t.Errorf("parseRoomCommand(%q) = %q, %v — want %q, %v", c.draft, arg, ok, c.arg, c.ok)
		}
	}
}

// cdRoom builds a model whose workspace is a real directory with a real
// sibling, because /cd resolution answers with os.Stat and a test against
// invented paths would pass on any string logic at all.
func cdRoom(t *testing.T) (*Model, string, string) {
	t.Helper()
	parent := t.TempDir()
	a := filepath.Join(parent, "telltale")
	b := filepath.Join(parent, "kb-agent")
	for _, d := range []string{a, b} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	m := newWithBrief(Options{Dir: a}, Brief{}, HookSet{}, Reattachment{})
	return m, a, b
}

func TestCdMovesTheRoomToASibling(t *testing.T) {
	m, _, b := cdRoom(t)
	m.setDraft("/cd kb-agent")
	if !m.roomCommand() {
		t.Fatal("/cd was not recognised as a room command")
	}
	if !sameDir(m.st.Workspace, b) {
		t.Errorf("workspace = %q, want the sibling %q", m.st.Workspace, b)
	}
	if m.st.Draft != "" {
		t.Errorf("the draft survived a successful /cd: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "next turn") {
		t.Errorf("the notice does not say when seats move: %q", m.st.Notice)
	}
}

func TestCdPrefersAChildOverASibling(t *testing.T) {
	m, a, _ := cdRoom(t)
	// A directory that exists BOTH under the workspace and beside it: the
	// shell-like reading (relative to here) has to win, or /cd would jump
	// somewhere a shell would not.
	if err := os.Mkdir(filepath.Join(a, "kb-agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.setDraft("/cd kb-agent")
	m.roomCommand()
	if want := filepath.Join(a, "kb-agent"); !sameDir(m.st.Workspace, want) {
		t.Errorf("workspace = %q, want the child %q", m.st.Workspace, want)
	}
}

func TestCdUnknownDirectoryKeepsTheDraft(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.setDraft("/cd no-such-repo")
	m.roomCommand()
	if !sameDir(m.st.Workspace, a) {
		t.Errorf("a failed /cd moved the room anyway: %q", m.st.Workspace)
	}
	if m.st.Draft != "/cd no-such-repo" {
		t.Errorf("the draft was discarded on a failed /cd: %q", m.st.Draft)
	}
	if !strings.Contains(m.st.Notice, "no-such-repo") {
		t.Errorf("the notice does not name what was not found: %q", m.st.Notice)
	}
}

func TestCdRefusedMidTurn(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.turn = &turnState{cancel: func() {}, live: map[model.VendorID]bool{}}
	m.setDraft("/cd kb-agent")
	m.roomCommand()
	if !sameDir(m.st.Workspace, a) {
		t.Error("/cd moved the room under a turn in flight")
	}
	if !strings.Contains(m.st.Notice, "in flight") {
		t.Errorf("the refusal does not say why: %q", m.st.Notice)
	}
}

func TestCdAloneNamesTheCurrentWorkspace(t *testing.T) {
	m, _, _ := cdRoom(t)
	m.setDraft("/cd")
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "/cd <dir>") {
		t.Errorf("bare /cd does not explain itself: %q", m.st.Notice)
	}
}

func TestCdExpandsALeadingTilde(t *testing.T) {
	m, _, b := cdRoom(t)
	m.st.Home = filepath.Dir(b)
	m.setDraft("/cd ~/kb-agent")
	m.roomCommand()
	if !sameDir(m.st.Workspace, b) {
		t.Errorf("workspace = %q, want %q via ~", m.st.Workspace, b)
	}
}

func TestCdToTheSameDirectoryIsANoOp(t *testing.T) {
	m, a, _ := cdRoom(t)
	m.setDraft("/cd " + a)
	m.roomCommand()
	if !strings.Contains(m.st.Notice, "already") {
		t.Errorf("a no-op /cd was not called one: %q", m.st.Notice)
	}
}

// fakeSession stands in for a live vendor process. See seatSession: the /cd
// respawn kills a RUNNING process, and this is how the branch is watched
// without spawning one.
type fakeSession struct {
	alive  bool
	killed bool
}

func (f *fakeSession) Send([]byte) error { return nil }
func (f *fakeSession) Kill()             { f.killed = true; f.alive = false }
func (f *fakeSession) Alive() bool       { return f.alive }

// TestAMovedRoomRespawnsThePersistentSeatOnItsOwnThread is the Claude-seat
// half of /cd. cwd is fixed at spawn — the stream-json envelope has no cwd
// field, and the documented mid-conversation move is respawn with --resume —
// so the live process pinned to the old directory is killed and the earned
// session id is spent on the replacement, through the SAME one-attempt
// probation rule the restored ids use.
func TestAMovedRoomRespawnsThePersistentSeatOnItsOwnThread(t *testing.T) {
	m, _, b := cdRoom(t)
	m.sessions[model.VendorClaude] = "claude-sess-1"
	old := &fakeSession{alive: true}
	m.procs[model.VendorClaude] = &seatProc{sess: old, sent: 2, dir: m.st.Workspace}

	m.setDraft("/cd " + b)
	if !m.roomCommand() {
		t.Fatal("/cd was not handled")
	}
	if old.killed {
		t.Error("/cd killed the seat eagerly — the respawn is lazy, on the next dispatch")
	}

	seat := &recordingSeat{}
	c := &Column{
		Vendor: model.VendorClaude, Avail: AvailInstalled,
		Binary: filepath.Join(t.TempDir(), "telltale-no-such-binary"),
	}
	// The launch fails on a missing binary; what is being watched is what the
	// attempt DID: killed the old process, spent the id on a resume in the new
	// directory, and put the thread on probation.
	if _, _, err := m.seatProcess(seat, c); err == nil {
		t.Fatal("a launch against a missing binary unexpectedly succeeded")
	}
	if !old.killed {
		t.Error("the old process survived the move")
	}
	if seat.resumeCalls != 1 || seat.lastID != "claude-sess-1" {
		t.Errorf("resume calls = %d id %q, want 1 spend of the earned id",
			seat.resumeCalls, seat.lastID)
	}
	if !m.unproven[model.VendorClaude] {
		t.Error("the moved thread is not on probation")
	}
	if id := m.resumeIDs[model.VendorClaude]; id != "" {
		t.Errorf("the id survived its one attempt: %q", id)
	}
}

// TestAnUnmovedRoomKeepsItsProcess is the other side: seatProcess must not
// respawn a seat whose directory still matches — that would pay a session
// init per turn and undo the sixth amendment.
func TestAnUnmovedRoomKeepsItsProcess(t *testing.T) {
	m, _, _ := cdRoom(t)
	sess := &fakeSession{alive: true}
	p := &seatProc{sess: sess, sent: 1, dir: m.st.Workspace}
	m.procs[model.VendorClaude] = p

	seat := &recordingSeat{}
	c := &Column{Vendor: model.VendorClaude, Avail: AvailInstalled, Binary: "claude"}
	got, note, err := m.seatProcess(seat, c)
	if err != nil || got != p || note != "" {
		t.Fatalf("seatProcess = %v, %q, %v — want the existing process untouched", got, note, err)
	}
	if sess.killed {
		t.Error("an unmoved seat's process was killed")
	}
	if seat.sessionCalls+seat.resumeCalls != 0 {
		t.Error("an unmoved seat respawned anyway")
	}
}
