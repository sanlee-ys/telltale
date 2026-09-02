package council

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sanlee-ys/telltale/internal/council/runner"
	"github.com/sanlee-ys/telltale/internal/council/vendors"
	"github.com/sanlee-ys/telltale/internal/model"
)

// tempHome cuts the test off from every piece of council state the DEVELOPER's
// machine happens to be carrying, and points it at the test's own tree.
//
// Both home variables, because os.UserHomeDir reads USERPROFILE on Windows and
// HOME everywhere else — and a test that got this wrong would not fail, it
// would quietly write into the developer's real ~/.telltale/council and leave a
// room behind that a later --resume would find.
//
// briefEnv is blanked for the mirror-image reason, and it is not hypothetical:
// TELLTALE_COUNCIL_BRIEF is exactly the variable a real user of this room sets
// once and forgets, so it is set on the maintainer's box and unset in CI.
// TestRunRejectsAMissingCdDirectory went red on a clean checkout of a green
// main the day that brief's repo was renamed out from under it — LoadBrief runs
// first in Run and failed on the stale path, so the test reported the wrong
// refusal while CI, which has no such variable, stayed green. A verdict that
// depends on un-versioned machine state is the same class of defect as a Render
// that reads the clock (CLAUDE.md): it does not fail where it is wrong.
func tempHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv(briefEnv, "")
	return dir
}

func savedRoom(ws string) SavedRoom {
	return SavedRoom{
		Workspace: ws,
		Posture:   "read",
		Turn:      3,
		Sessions: map[model.VendorID]string{
			model.VendorClaude: "claude-sess-1",
			model.VendorCodex:  "codex-thread-1",
		},
		BriefPath: "/home/dev/private/brief.md",
		SavedAt:   time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
	}
}

func TestSavedRoomRoundTrips(t *testing.T) {
	tempHome(t)
	ws := filepath.Join("/home/dev/code", "telltale")
	want := savedRoom(ws)

	if err := SaveRoom(want); err != nil {
		t.Fatal(err)
	}
	re, err := LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if !re.Active() {
		t.Fatalf("round-tripped room is not active: %q", re.Ignored)
	}
	if re.Room.Turn != want.Turn {
		t.Errorf("turn = %d, want %d", re.Room.Turn, want.Turn)
	}
	if re.Room.Sessions[model.VendorClaude] != "claude-sess-1" {
		t.Errorf("claude session = %q", re.Room.Sessions[model.VendorClaude])
	}
	if re.Room.Sessions[model.VendorCodex] != "codex-thread-1" {
		t.Errorf("codex session = %q", re.Room.Sessions[model.VendorCodex])
	}
	if !re.Room.SavedAt.Equal(want.SavedAt) {
		t.Errorf("saved-at = %v, want %v", re.Room.SavedAt, want.SavedAt)
	}
	if re.Room.BriefPath != want.BriefPath {
		t.Errorf("brief path = %q", re.Room.BriefPath)
	}
	if re.Room.Version != roomVersion {
		t.Errorf("version = %d, want %d", re.Room.Version, roomVersion)
	}
}

// TestNothingSavedIsErrNoSavedRoom: no room.json and no v1 file to adopt is the
// first-launch case. The caller opens fresh on it; what matters here is that
// the loader says so distinctly rather than inventing an Ignored notice for a
// file that never existed.
func TestNothingSavedIsErrNoSavedRoom(t *testing.T) {
	tempHome(t)
	if _, err := LoadRoom(); err != ErrNoSavedRoom {
		t.Fatalf("err = %v, want ErrNoSavedRoom", err)
	}
}

// TestCorruptSavedRoomIsIgnoredNotFatal is the OTHER half of that split. A file
// that exists and cannot be parsed is telltale's own state being damaged, not
// the user being wrong; the room is still perfectly usable unreattached, so a
// bad byte on disk must never be the reason someone cannot open their tool.
func TestCorruptSavedRoomIsIgnoredNotFatal(t *testing.T) {
	tempHome(t)
	ws := "/home/dev/code/telltale"
	if err := SaveRoom(savedRoom(ws)); err != nil {
		t.Fatal(err)
	}
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("a corrupt file must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a corrupt file was treated as a restorable room")
	}
	if re.Ignored == "" {
		t.Error("a corrupt file was refused without saying why")
	}
}

// TestARoomRecordingNoTurnsIsRefused keeps the loader's idea of a usable room
// and Reattach.Active's the same. A turn:0 file that loaded would restore every
// session id while the room rendered as cold — seats silently resumed behind a
// screen saying nothing was.
func TestARoomRecordingNoTurnsIsRefused(t *testing.T) {
	tempHome(t)
	ws := "/home/dev/code/telltale"
	room := savedRoom(ws)
	room.Turn = 0
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("a turn-less room must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a room recording no turns was restored")
	}

	m := newWithBrief(Options{}, Brief{}, GateHook{}, re)
	if len(m.sessions) != 0 {
		t.Error("a refused room restored sessions anyway")
	}
}

// TestAnOversizeSavedRoomIsRefusedWithoutBeingRead mirrors LoadBrief's ceiling.
// Something enormous at that path is not a room council wrote, and reading it
// into memory to discover that is the wrong order.
func TestAnOversizeSavedRoomIsRefusedWithoutBeingRead(t *testing.T) {
	tempHome(t)
	ws := "/home/dev/code/telltale"
	if err := SaveRoom(savedRoom(ws)); err != nil {
		t.Fatal(err)
	}
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, maxRoom+1), 0o600); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("an oversize file must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("an oversize file was restored")
	}
	if !strings.Contains(re.Ignored, "large") {
		t.Errorf("reason = %q, want it to name the size", re.Ignored)
	}
}

// TestADirectoryWhereTheRoomShouldBeIsNotFatal. The file "exists" and is
// unusable, which is the notice case — telltale's own state being damaged must
// never be the reason the room refuses to open.
func TestADirectoryWhereTheRoomShouldBeIsNotFatal(t *testing.T) {
	tempHome(t)
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("a directory at the room path must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a directory was restored as a room")
	}
	if re.Ignored == "" {
		t.Error("the refusal gave no reason")
	}
}

func TestVersionSkewIsIgnored(t *testing.T) {
	tempHome(t)
	ws := "/home/dev/code/telltale"
	room := savedRoom(ws)
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}

	// A file from a build whose schema this one does not know.
	room.Version = roomVersion + 7
	buf, err := json.Marshal(room)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("version skew must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a room from an unknown schema was restored anyway")
	}
	if !strings.Contains(re.Ignored, "v"+itoa(roomVersion+7)) {
		t.Errorf("the reason does not name the version it found: %q", re.Ignored)
	}
}

// TestARoomRecordingNoWorkspaceIsRefused: v2 removed the filename key, so the
// field is the only record of where the room was. A room that cannot say
// cannot be reopened anywhere in particular, and guessing (cwd, say) would
// silently reattach four conversations to a directory they never saw.
func TestARoomRecordingNoWorkspaceIsRefused(t *testing.T) {
	tempHome(t)
	room := savedRoom("/home/dev/code/telltale")
	room.Workspace = ""
	if err := SaveRoom(room); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("a workspace-less file must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a room with no workspace was restored")
	}
	if !strings.Contains(re.Ignored, "workspace") {
		t.Errorf("reason = %q, want it to name the missing workspace", re.Ignored)
	}
}

// --- adopting the pre-cockpit per-workspace files -------------------------

// legacyFile writes a v1 per-workspace room the way the old build did: hashed
// filename, version 1. The hash itself no longer matters — adoption scans by
// content — so an abbreviated stand-in name is used.
func legacyFile(t *testing.T, home, name string, room SavedRoom) string {
	t.Helper()
	dir := filepath.Join(home, ".telltale", "council")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	room.Version = 1
	buf, err := json.Marshal(room)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestTheNewestLegacyRoomIsAdopted is the migration: the first launch after
// the room went global continues the conversation the user was most recently
// having, which is the P0 sentence — reattach to the PRIOR conversation.
func TestTheNewestLegacyRoomIsAdopted(t *testing.T) {
	home := tempHome(t)
	older := savedRoom("/home/dev/code/older")
	older.SavedAt = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	newer := savedRoom("/home/dev/code/newer")
	newer.Turn = 7
	newer.SavedAt = time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)
	legacyFile(t, home, "aaaa.json", older)
	newPath := legacyFile(t, home, "bbbb.json", newer)

	re, err := LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if !re.Active() {
		t.Fatalf("nothing was adopted: %q", re.Ignored)
	}
	if !re.Adopted {
		t.Error("the reattachment does not say it came from the old format")
	}
	if re.Room.Workspace != "/home/dev/code/newer" || re.Room.Turn != 7 {
		t.Errorf("adopted %q turn %d, want the newest file's room", re.Room.Workspace, re.Room.Turn)
	}
	if re.Path != newPath {
		t.Errorf("path = %q, want the legacy file it came from", re.Path)
	}
}

// TestAdoptionSkipsWhatItCannotReadAndTouchesNothing. The scan runs on every
// launch until the first save writes room.json, so a corrupt abandoned v1 file
// must be skipped silently rather than reported forever — and adoption is
// READ-ONLY: the v1 files are never rewritten, renamed or deleted, so a wrong
// adoption destroys nothing.
func TestAdoptionSkipsWhatItCannotReadAndTouchesNothing(t *testing.T) {
	home := tempHome(t)
	good := savedRoom("/home/dev/code/telltale")
	goodPath := legacyFile(t, home, "aaaa.json", good)
	dir := filepath.Join(home, ".telltale", "council")
	if err := os.WriteFile(filepath.Join(dir, "bbbb.json"), []byte("{torn"), 0o600); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if !re.Active() || re.Path != goodPath {
		t.Fatalf("the readable v1 room was not adopted: %+v", re)
	}
	if re.Ignored != "" {
		t.Errorf("a corrupt sibling produced a notice: %q", re.Ignored)
	}

	before, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveRoom(re.Room); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatalf("the v1 file is gone after a save: %v", err)
	}
	if string(before) != string(after) {
		t.Error("adoption rewrote the v1 file it read")
	}
	// And from here on room.json wins: the legacy scan is a one-time seed, not
	// a second source of truth.
	re2, err := LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if re2.Adopted {
		t.Error("room.json exists and the loader still adopted a legacy file")
	}
	roomJSON, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if re2.Path != roomJSON {
		t.Errorf("path = %q, want room.json once it exists", re2.Path)
	}
}

// TestACorruptGlobalRoomDoesNotResurrectALegacyOne. A damaged room.json is the
// Ignored-notice case, and it must NOT fall back to the v1 scan: the newest
// legacy room is an OLDER conversation, and reattaching it because the current
// one's file tore would silently rewind the user days without a word.
func TestACorruptGlobalRoomDoesNotResurrectALegacyOne(t *testing.T) {
	home := tempHome(t)
	legacyFile(t, home, "aaaa.json", savedRoom("/home/dev/code/older"))
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatalf("a corrupt room.json must not be fatal, got %v", err)
	}
	if re.Active() {
		t.Fatal("a corrupt room.json restored something anyway")
	}
	if re.Ignored == "" {
		t.Error("the refusal gave no reason")
	}
}

// TestSavingTwiceReplacesCleanly covers the atomic path: the rename must land
// over an existing file, and no temp file may be left behind.
func TestSavingTwiceReplacesCleanly(t *testing.T) {
	home := tempHome(t)
	ws := "/home/dev/code/telltale"

	first := savedRoom(ws)
	if err := SaveRoom(first); err != nil {
		t.Fatal(err)
	}
	second := savedRoom(ws)
	second.Turn = 9
	second.Sessions = map[model.VendorID]string{model.VendorClaude: "claude-sess-2"}
	if err := SaveRoom(second); err != nil {
		t.Fatal(err)
	}

	re, err := LoadRoom()
	if err != nil {
		t.Fatal(err)
	}
	if re.Room.Turn != 9 {
		t.Errorf("turn = %d, want the second save's 9", re.Room.Turn)
	}
	if re.Room.Sessions[model.VendorCodex] != "" {
		t.Error("the first save's sessions survived the second")
	}

	entries, err := os.ReadDir(filepath.Join(home, ".telltale", "council"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temp file was left behind: %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("%d files in the room directory, want exactly 1", len(entries))
	}
}

func TestSavedRoomIsNotWorldReadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows reports a mode it does not enforce this way. The control there
		// is the ACL inherited from the 0700 directory under the user profile,
		// which os.Stat cannot see — so asserting a mode here would be a test
		// passing on a claim the platform never made.
		t.Skip("posix file modes are not the access control on windows")
	}
	home := tempHome(t)
	ws := "/home/dev/code/telltale"
	if err := SaveRoom(savedRoom(ws)); err != nil {
		t.Fatal(err)
	}
	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	di, err := os.Stat(filepath.Join(home, ".telltale", "council"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := di.Mode().Perm(); perm != 0o700 {
		t.Errorf("dir mode = %o, want 700", perm)
	}
}

// TestTheSavedRoomHoldsKeysAndNeverContent is the privacy boundary, and it is
// the same one the brief keeps one layer up. The vendors hold their own
// history; this file holds the ids that reach it. A transcript, a reply, or a
// line of the operating brief appearing here would put a private conversation
// in a second place the user never chose.
func TestTheSavedRoomHoldsKeysAndNeverContent(t *testing.T) {
	home := tempHome(t)
	secret := "the private division-of-labour convention"
	reply := "here is what I think about the merger"
	draft := "what should we do about the seat"

	m := newWithBrief(Options{Dir: "/home/dev/code/telltale"},
		Brief{Path: "/home/dev/private/brief.md", Text: secret}, GateHook{}, Reattachment{})
	m.st.Turn = 2
	m.st.Draft = draft
	// A NARROWED roster, so the roster key is actually present below rather than
	// omitted by the default room. §9.32's claim is that a roster passes the
	// ninth amendment's keys-not-content test, and the way to assert a claim
	// about a field is to save a room that has one.
	m.st.Seats = Seats{Only: []model.VendorID{model.VendorClaude}}
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.st.Columns = []Column{{
		Vendor: model.VendorClaude, Label: "Claude Code",
		Avail: AvailInstalled, Body: reply,
		Acts: []Act{{Text: "Bash: git push origin secret-branch"}},
	}}
	m.saveRoom()

	path, err := RoomPath()
	if err != nil {
		t.Fatal(err)
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("no room was saved: %v", err)
	}
	body := string(buf)

	for _, forbidden := range []string{secret, reply, draft, "git push"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the saved room leaked %q", forbidden)
		}
	}
	if !strings.Contains(body, "claude-sess-1") {
		t.Error("the saved room does not hold the session id it exists for")
	}
	// The brief's PATH is kept and its content is not. That asymmetry is the
	// whole design, so it is asserted rather than assumed.
	if !strings.Contains(body, "/home/dev/private/brief.md") {
		t.Error("the saved room dropped the brief path")
	}

	// The exact key set, and this is the assertion that makes the contract fail
	// CLOSED. Grepping for four known strings only catches the leaks someone
	// thought of; a field added later carrying, say, a column's note — which can
	// hold raw vendor stderr — would sail past it. Adding a key to this file has
	// to be a deliberate act that breaks a test and gets read.
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(buf, &keys); err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"version": true, "workspace": true, "posture": true, "turn": true,
		"sessions": true, "brief_path": true, "saved_at": true,
		// "seats" was added by §9.32 and this is the deliberate act the comment
		// above demands. It passes the rule rather than being excused from it: a
		// roster is four vendor ids out of a closed four-name set — the words
		// `--vendor` takes and the footer prints — so it says who was in the room
		// and not one syllable of what was said there. Asserted for content
		// below, not merely allowed here.
		"seats": true,
	}
	for k := range keys {
		if !want[k] {
			t.Errorf("the saved room carries an unexpected key %q — is it content?", k)
		}
	}
	for k := range want {
		if _, ok := keys[k]; !ok && k != "brief_path" {
			t.Errorf("the saved room is missing key %q", k)
		}
	}
	// The roster holds NAMES, and only names a seat can be called by. A field
	// added to Seats later that carried anything else — a note, a reason, a
	// last-brief — would reach this file through the same tag and this is where
	// it gets caught.
	var roster struct {
		All  bool     `json:"all"`
		Only []string `json:"only"`
	}
	if err := json.Unmarshal(keys["seats"], &roster); err != nil {
		t.Fatalf("the saved roster is not a roster: %v", err)
	}
	if len(roster.Only) != 1 || roster.Only[0] != string(model.VendorClaude) {
		t.Errorf("saved roster = %+v, want claude alone", roster)
	}
	_ = home
}

// TestNothingIsWrittenBeforeTheFirstTurn keeps ~/.telltale/council from filling
// with a file per accidental launch — including the one opened in the wrong
// directory and immediately quit.
func TestNothingIsWrittenBeforeTheFirstTurn(t *testing.T) {
	home := tempHome(t)
	m := newWithBrief(Options{Dir: "/home/dev/code/telltale"}, Brief{}, GateHook{}, Reattachment{})
	m.saveRoom()

	if _, err := os.Stat(filepath.Join(home, ".telltale", "council")); !os.IsNotExist(err) {
		t.Errorf("a room with no turns wrote state anyway (stat err = %v)", err)
	}
}

// TestSameDirFoldsCaseOnWindowsOnly. `C:\Users\...` and `c:\users\...` are one
// directory on Windows and /cd between them must be a no-op; on a
// case-sensitive filesystem they are two directories and folding them would
// treat a real move as staying put.
func TestSameDirFoldsCaseOnWindowsOnly(t *testing.T) {
	a, b := `C:\Users\dev\code\Telltale`, `c:\users\dev\code\telltale`
	if runtime.GOOS == "windows" {
		if !sameDir(a, b) {
			t.Error("two spellings of one windows directory read as different rooms")
		}
		return
	}
	if sameDir(a, b) {
		t.Error("two different directories read as one on a case-sensitive filesystem")
	}
}

func TestAgeIsCoarserThanASecond(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{-time.Hour, "just now"},
		{5 * time.Second, "just now"},
		{90 * time.Second, "1m ago"},
		{45 * time.Minute, "45m ago"},
		{3 * time.Hour, "3h ago"},
		{47 * time.Hour, "47h ago"},
		{72 * time.Hour, "3d ago"},
	}
	for _, c := range cases {
		if got := age(c.d); got != c.want {
			t.Errorf("age(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// --- reattaching a room --------------------------------------------------

func reattachedModel(t *testing.T, room SavedRoom, opts Options) *Model {
	t.Helper()
	return newWithBrief(opts, Brief{}, GateHook{}, Reattachment{
		Path: "/home/dev/.telltale/council/abc.json",
		Room: room,
	})
}

func TestReattachRestoresTheTurnCountAndTheThreads(t *testing.T) {
	tempHome(t)
	ws := resolveWorkspace("")
	m := reattachedModel(t, savedRoom(ws), Options{})

	if m.st.Turn != 3 {
		t.Errorf("turn = %d, want the saved 3 — a reattached room continues its count", m.st.Turn)
	}
	if m.sessions[model.VendorClaude] != "claude-sess-1" {
		t.Errorf("claude session not restored: %q", m.sessions[model.VendorClaude])
	}
	if m.resumeIDs[model.VendorClaude] != "claude-sess-1" {
		t.Error("the persistent seat has no id to spend at launch")
	}
	if !m.st.Reattached.Active() {
		t.Error("the room does not report itself reattached")
	}
	if !strings.Contains(m.st.Notice, "abc.json") {
		t.Errorf("the notice does not say where the state came from: %q", m.st.Notice)
	}
}

// TestReattachDoesNotRestoreWritePosture is the safety property of this whole
// feature. A --write room saved to disk must not reopen writable because of
// what is in a file: the grant comes from the flag the user typed, and the
// header's WRITE marker has to mean what it has always meant.
func TestReattachDoesNotRestoreWritePosture(t *testing.T) {
	tempHome(t)
	ws := resolveWorkspace("")
	room := savedRoom(ws)
	room.Posture = "write-gated"

	m := reattachedModel(t, room, Options{})
	if m.st.Write {
		t.Fatal("write posture was restored from a file instead of from a flag")
	}
	if m.posture() != vendors.PostureRead {
		t.Errorf("posture = %v, want read", m.posture())
	}
	if !strings.Contains(m.st.Notice, "write-gated") {
		t.Errorf("the posture change was applied-or-dropped silently: %q", m.st.Notice)
	}
	frame := Render(m.st, PlainStyles(), GlyphsFor(false))
	if strings.Contains(frame, "WRITE ") {
		t.Error("a reattached read room renders the WRITE marker")
	}
}

// TestOnlyASeatedColumnIsMarkedRestored. An id for a vendor this machine does
// not have is dead weight, and a "your thread came back" card above an "is not
// seated" card would be two contradictory statements in one column.
func TestOnlyASeatedColumnIsMarkedRestored(t *testing.T) {
	tempHome(t)
	ws := resolveWorkspace("")
	room := savedRoom(ws)
	room.Sessions[model.VendorGemini] = "gemini-not-a-seat"

	m := reattachedModel(t, room, Options{})
	for _, c := range m.st.Columns {
		if c.Avail != AvailInstalled && c.Restored {
			t.Errorf("%s is not seated but is marked restored", c.Vendor)
		}
		if c.Restored && m.sessions[c.Vendor] == "" {
			t.Errorf("%s is marked restored with no session id", c.Vendor)
		}
	}
	if !strings.Contains(m.st.Notice, "seats restored") {
		t.Errorf("the notice does not count the restored seats: %q", m.st.Notice)
	}
}

// TestASavedRoomDeclinedByFreshIsMentionedBeforeItIsReplaced.
//
// There is one room file, so opening --fresh and dispatching a single turn
// renames a new file over the old keys — four conversations become unreachable
// with nothing said. The room does not refuse and does not prompt; naming what
// rerunning without --fresh would have reattached is enough to make the loss a
// choice rather than an accident.
func TestASavedRoomDeclinedByFreshIsMentionedBeforeItIsReplaced(t *testing.T) {
	tempHome(t)
	ws := resolveWorkspace("")
	m := newWithBrief(Options{Fresh: true}, Brief{}, GateHook{}, Reattachment{
		Path:    "/home/dev/.telltale/council/room.json",
		Room:    savedRoom(ws),
		Offered: true,
	})

	if m.st.Turn != 0 {
		t.Errorf("turn = %d — an offer is not a reattach", m.st.Turn)
	}
	if len(m.sessions) != 0 {
		t.Error("an offer restored sessions without being asked")
	}
	if m.st.Reattached.Active() {
		t.Error("an offer was reported as a reattach")
	}
	if !strings.Contains(m.st.Notice, "--fresh") {
		t.Errorf("the notice does not name the flag that declined the room: %q", m.st.Notice)
	}
}

func TestAnIgnoredSavedRoomSaysSoAndStartsFresh(t *testing.T) {
	tempHome(t)
	m := newWithBrief(Options{}, Brief{}, GateHook{}, Reattachment{
		Path:    "/home/dev/.telltale/council/abc.json",
		Ignored: "the saved room file is not readable json",
	})

	if m.st.Turn != 0 {
		t.Errorf("turn = %d, want a fresh room", m.st.Turn)
	}
	if m.st.Reattached.Active() {
		t.Error("an ignored file was reported as a reattach")
	}
	if len(m.sessions) != 0 {
		t.Error("an ignored file restored sessions anyway")
	}
	if !strings.Contains(m.st.Notice, "not readable json") {
		t.Errorf("the room does not say why it did not reattach: %q", m.st.Notice)
	}
}

// TestAFreshRoomGainsNothing guards the promise that this feature is invisible
// until it is used: a room opened normally renders exactly as it did before,
// which is what keeps every other golden in this package honest.
func TestAFreshRoomGainsNothing(t *testing.T) {
	st := room()
	if st.Reattached.Active() {
		t.Fatal("a fresh room reports a reattach")
	}
	frame := render(st)
	if strings.Contains(frame, "reattached") {
		t.Error("a fresh room mentions reattaching")
	}
	if !strings.Contains(frame, "no turn dispatched yet") {
		t.Error("a fresh room lost its idle card")
	}
}

// --- the first dispatch after a reattach ---------------------------------

// TestFirstDispatchAfterReattachResumesTheThread. The restored id rides the
// EXISTING specFor path — nothing new decides this — so a reattached spawn-per
// turn seat produces the vendor's own resume invocation and, critically, does
// NOT re-send the brief: that context is already in the history being replayed.
func TestFirstDispatchAfterReattachResumesTheThread(t *testing.T) {
	tempHome(t)
	m := newWithBrief(Options{}, Brief{Path: "p", Text: "OPERATING CONTEXT"}, GateHook{}, Reattachment{
		Room: SavedRoom{
			Workspace: resolveWorkspace(""),
			Turn:      3,
			SavedAt:   time.Now().Add(-time.Hour),
			Sessions:  map[model.VendorID]string{model.VendorCodex: "codex-thread-1"},
		},
	})

	c := &Column{Vendor: model.VendorCodex, Binary: "codex", Avail: AvailInstalled}
	spec, resumed, err := m.specFor(vendors.Codex{}, c, "next question")
	if err != nil {
		t.Fatal(err)
	}
	// The id is reported back as well as spent, because it is the requested half
	// of §9.43's comparison and no caller can dig it back out of a
	// vendor-specific argv position.
	if resumed != "codex-thread-1" {
		t.Errorf("the resumed id was not reported to the caller: %q", resumed)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "resume") || !strings.Contains(joined, "codex-thread-1") {
		t.Errorf("args do not resume the saved thread: %v", spec.Args)
	}
	if strings.Contains(spec.StdinPrompt, "OPERATING CONTEXT") {
		t.Error("the brief was re-sent to a seat that already has it in its history")
	}
}

// refusingVendor is a seat that reports ErrNoResume for an id it was handed.
//
// NOTE what this is and is not. **No shipped adapter behaves this way** — every
// one returns ErrNoResume only for an EMPTY id, which is why the probation rule
// above exists and why this double must not be read as modelling a stale id.
// What it covers is the specFor fallback itself: a vendor that cannot build a
// resume invocation must still produce a briefed first turn rather than failing
// the column.
type refusingVendor struct{ vendors.Claude }

func (refusingVendor) NextTurn(prompt, workspace, binary, sessionID string, p vendors.Posture) (runner.Spec, error) {
	return runner.Spec{}, vendors.ErrNoResume
}

// TestAVendorThatCannotBuildAResumeStartsFreshAndIsBriefed covers the specFor
// fallback: the seat must not simply fail, it opens a new session AND gets the
// brief back, because a fresh session is a stranger again.
//
// The case where a vendor ACCEPTS a dead id and then dies is a different path
// entirely, and is covered by
// TestAStaleIdOnASpawnPerTurnSeatIsDroppedAfterOneTurn.
func TestAVendorThatCannotBuildAResumeStartsFreshAndIsBriefed(t *testing.T) {
	tempHome(t)
	m := newWithBrief(Options{}, Brief{Path: "p", Text: "OPERATING CONTEXT"}, GateHook{}, Reattachment{
		Room: SavedRoom{
			Workspace: resolveWorkspace(""),
			Turn:      3,
			SavedAt:   time.Now().Add(-time.Hour),
			Sessions:  map[model.VendorID]string{model.VendorClaude: "long-expired"},
		},
	})

	c := &Column{Vendor: model.VendorClaude, Binary: "claude", Avail: AvailInstalled}
	spec, resumed, err := m.specFor(refusingVendor{}, c, "next question")
	if err != nil {
		t.Fatal(err)
	}
	// Nothing was asked to be resumed, so nothing is reported — a turn that fell
	// back to FirstTurn must not leave an id for §9.43 to compare against, or the
	// fresh conversation it opens would be read as a fork.
	if resumed != "" {
		t.Errorf("a fallback first turn reported a resumed id: %q", resumed)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "long-expired") {
		t.Error("a refused id still reached the invocation")
	}
	if !strings.Contains(spec.StdinPrompt, "OPERATING CONTEXT") {
		t.Error("a seat starting a new session was not re-briefed")
	}
}

// recordingSeat watches how the persistent path spends a restored id.
type recordingSeat struct {
	vendors.Claude
	resumeCalls  int
	sessionCalls int
	lastID       string
	lastHooks    string
}

func (r *recordingSeat) Session(workspace, binary, hooksFile string, p vendors.Posture) (runner.Spec, error) {
	r.sessionCalls++
	return r.Claude.Session(workspace, binary, hooksFile, p)
}

func (r *recordingSeat) SessionResume(workspace, binary, hooksFile, sessionID string, p vendors.Posture) (runner.Spec, error) {
	r.resumeCalls++
	r.lastID = sessionID
	r.lastHooks = hooksFile
	return r.Claude.SessionResume(workspace, binary, hooksFile, sessionID, p)
}

// TestARestoredThreadIsSpentExactlyOnce is the property that keeps a stale id
// from wedging a seat for the whole session.
//
// A thread the vendor no longer has makes the process exit immediately, so a
// seat that retried the id would refuse every brief for the rest of the room
// with the same error. The id is deleted BEFORE the process is launched, which
// is why this can be checked without spawning anything: the launch here fails
// on a binary that does not exist, and the id is gone regardless.
func TestARestoredThreadIsSpentExactlyOnce(t *testing.T) {
	tempHome(t)
	// A real hooks path, so the guard assertion below is a claim rather than
	// two empty strings agreeing with each other.
	hooksPath := filepath.Join(t.TempDir(), "council-hooks.json")
	m := newWithBrief(Options{Write: true}, Brief{}, GateHook{Path: hooksPath}, Reattachment{
		Room: SavedRoom{
			Workspace: resolveWorkspace(""),
			Turn:      1,
			SavedAt:   time.Now().Add(-time.Hour),
			Sessions:  map[model.VendorID]string{model.VendorClaude: "claude-sess-1"},
		},
	})

	seat := &recordingSeat{}
	c := &Column{
		Vendor: model.VendorClaude, Avail: AvailInstalled,
		Binary: filepath.Join(t.TempDir(), "telltale-no-such-binary"),
	}

	if _, _, err := m.seatProcess(seat, c); err == nil {
		t.Fatal("a launch against a missing binary unexpectedly succeeded")
	}
	if seat.resumeCalls != 1 {
		t.Errorf("SessionResume called %d times, want 1", seat.resumeCalls)
	}
	if seat.lastID != "claude-sess-1" {
		t.Errorf("resumed with %q, want the restored id", seat.lastID)
	}
	// The reattached seat carries the user's own hook guard, like every other
	// seat. Reattaching restores a CONVERSATION; a seat that came back without
	// the guard while the badge still claimed it was wired would be the
	// quietest false claim in the room.
	if seat.lastHooks != hooksPath {
		t.Errorf("resumed seat got hooks %q, want %q", seat.lastHooks, hooksPath)
	}
	if id := m.resumeIDs[model.VendorClaude]; id != "" {
		t.Errorf("the restored id survived its one attempt: %q", id)
	}

	// Second attempt: a NEW session, unresumed, because the id was spent.
	if _, _, err := m.seatProcess(seat, c); err == nil {
		t.Fatal("a launch against a missing binary unexpectedly succeeded")
	}
	if seat.resumeCalls != 1 {
		t.Errorf("SessionResume called %d times overall, want 1 — the id was retried",
			seat.resumeCalls)
	}
	if seat.sessionCalls < 2 {
		t.Errorf("Session called %d times, want a fresh session on each attempt",
			seat.sessionCalls)
	}
}

// TestARefusedThreadSaysWhatHappensNext. The vendor reports a dead thread as a
// failed turn, whose stock wording reads as "this vendor broke" and sends the
// user looking for a problem with the vendor instead of retyping their brief.
//
// Renamed from TestARefusedThreadSaysTheHistoryIsGone: that claim was RETRACTED
// on 2026-08-04 when an agy conversation was round-tripped and demonstrably
// resumed on a turn that still failed, and a test name asserting it outlived the
// retraction by three amendments. A test can hold a false claim in place just as
// firmly as a true one (ADR-008, fifth amendment), and a test NAME is the half of
// it nobody reads.
func TestARefusedThreadSaysWhatHappensNext(t *testing.T) {
	m := turnModel(true)
	m.st.Columns[0].Restored = true
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), resumed: true, sent: 1}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note: "the vendor reported the turn failed", EndsTurn: true,
	}})

	c := m.st.Columns[0]
	if c.Phase != PhaseFailed {
		t.Errorf("phase = %v, want failed", c.Phase)
	}
	// Title first, mechanics under it. The title says the OUTCOME — a reader
	// scanning four columns learns from one short line that this seat starts
	// over — and the machinery that produced it is demoted to the body rather
	// than run together into one alarming sentence.
	if !strings.Contains(c.Note, "not restored") {
		t.Errorf("the card title does not name the outcome: %q", c.Note)
	}
	if c.NoteDetail == "" {
		t.Fatal("the card has no body: the mechanics have to survive the restyle")
	}
	if !strings.Contains(c.NoteDetail, "saved thread") {
		t.Errorf("the body does not name the lost thread: %q", c.NoteDetail)
	}
	if !strings.Contains(c.NoteDetail, "brief re-applied") {
		t.Errorf("the body does not say the new session gets the brief: %q", c.NoteDetail)
	}
	// No warning mark. This is the same fact reattachCard states calmly when no
	// thread came back at all, learned one turn later.
	if !c.NoteCalm {
		t.Error("the lost-thread card still renders as a warning")
	}
	if c.Restored {
		t.Error("the column still claims a restored thread after losing it")
	}
	// The id itself has to GO. Left in place it would be rebuilt into the same
	// dead invocation on every later turn of this room.
	if id := m.sessions[model.VendorClaude]; id != "" {
		t.Errorf("the dead session id survived the failure: %q", id)
	}
	if m.unproven[model.VendorClaude] {
		t.Error("the seat is still on probation after its thread was settled")
	}

	// The process then EXITS, and the runner reports that exit with the vendor's
	// own stderr attached. That second event arrives after the column has already
	// been told what happened, and it must not overwrite the sentence that says
	// what to do next with a raw `exit status 1`.
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note:     "exit status 1: No conversation found with session ID: claude-sess-1",
		ExitCode: 1,
	}})
	if d := m.st.Columns[0].NoteDetail; !strings.Contains(d, "brief re-applied") {
		t.Errorf("the process exit overwrote the lost-thread guidance: %q", d)
	}
}

// --- the seat whose resume fails by succeeding (§9.43) --------------------

// agyForkModel is a room with one agy column mid-turn, dispatched on a restored
// thread, with the fork comparison armed exactly as dispatch arms it.
//
// Built by hand rather than driven through Dispatch because the seam under test
// is the comparison, not the spawn: a real dispatch here would need an installed
// `agy` on the box running CI, which is the dependency the wire fixtures exist
// to remove.
func agyForkModel(asked string) *Model {
	m := &Model{
		st: State{Columns: []Column{{
			Vendor: model.VendorAntigravity, Label: "Antigravity",
			Avail: AvailInstalled, Phase: PhaseWaiting, Binary: "agy",
			Restored: true, Started: time.Now().Add(-time.Second),
		}}},
		sessions:   map[model.VendorID]string{model.VendorAntigravity: asked},
		resumeIDs:  map[model.VendorID]string{},
		unproven:   map[model.VendorID]bool{model.VendorAntigravity: true},
		threadLost: map[model.VendorID]bool{},
		forkWatch:  map[model.VendorID]string{},
		failure:    map[model.VendorID]runner.FailureClass{},
		redactors:  map[model.VendorID]*Redactor{},
		procs:      map[model.VendorID]*seatProc{},
		turns:      map[model.VendorID]*turnState{},
		cancelling: map[model.VendorID]bool{},
		givenUp:    map[model.VendorID]bool{},
	}
	m.holdTurn(&turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorAntigravity: true},
		persistent: map[model.VendorID]bool{},
	})
	if asked != "" {
		m.forkWatch[model.VendorAntigravity] = asked
	}
	return m
}

// TestAForkedAgyThreadIsSaidOutLoudAndTheNewOneIsKept is the honesty gap this
// seat carried in STATE.md until it was owned.
//
// MEASURED 2026-08-09, agy 1.1.11: handed a `--conversation` id it does not
// hold, that CLI does not refuse it — it opens a NEW conversation, answers, and
// reports `status: "SUCCESS"` with exit 0 and a different `conversation_id`.
// Every other seat either resumes or says the history is gone. Read on status
// and exit code alone this turn is indistinguishable from a clean resume, so
// before this the room rendered a continued conversation over a reply that had
// no history behind it.
//
// The three properties, in the order a user meets them: the reply survives, the
// card says the history did not come with it, and the conversation the vendor
// actually answered in becomes this seat's thread.
func TestAForkedAgyThreadIsSaidOutLoudAndTheNewOneIsKept(t *testing.T) {
	m := agyForkModel("agy-saved-thread")

	// The turn as it really arrives: init names the new conversation, the reply
	// streams, and the result reports success in that same new conversation.
	m.applyEvents([]runner.Event{
		{Vendor: model.VendorAntigravity, Kind: runner.KindSession, SessionID: "agy-brand-new"},
		{Vendor: model.VendorAntigravity, Kind: runner.KindText, Text: "ok "},
		{Vendor: model.VendorAntigravity, Kind: runner.KindMeta, Text: "ok", SessionID: "agy-brand-new"},
	})

	c := m.st.Columns[0]
	// The turn SUCCEEDED. Failing the column to punish a bookkeeping mismatch
	// would throw away an answer the user paid for.
	if c.Phase == PhaseFailed {
		t.Error("a successful turn was failed because its thread forked")
	}
	if !strings.Contains(c.Body, "ok") {
		t.Errorf("the reply was discarded: %q", c.Body)
	}
	// The established calm card, reused rather than reinvented: the fact is the
	// same one settleRestoredThread states when a reattach is refused.
	if !strings.Contains(c.Note, "not restored") {
		t.Errorf("the card title does not name the outcome: %q", c.Note)
	}
	if !c.NoteCalm {
		t.Error("the fork rendered as a warning; it is the same calm fact as a refused reattach")
	}
	if !strings.Contains(c.NoteDetail, "new conversation") {
		t.Errorf("the body does not say where the answer actually came from: %q", c.NoteDetail)
	}
	if c.Restored {
		t.Error("the column still claims a restored thread after the vendor answered somewhere else")
	}
	// The new id is ADOPTED. The reply happened inside it, so discarding it
	// would orphan a real turn and rebuild the same forking invocation next time.
	if got := m.sessions[model.VendorAntigravity]; got != "agy-brand-new" {
		t.Errorf("the seat's thread = %q, want the conversation the vendor actually answered in", got)
	}
	if m.unproven[model.VendorAntigravity] {
		t.Error("the seat is still on probation for an id that is gone")
	}
	if !m.threadLost[model.VendorAntigravity] {
		t.Error("the turn's later events are free to overwrite the one line that says the history is gone")
	}
	// One card, not two. Both the init and the result frame carry an id, and the
	// second must not restate the first.
	if _, still := m.forkWatch[model.VendorAntigravity]; still {
		t.Error("the comparison stayed armed after firing and would raise the card again")
	}
}

// TestAnAgyResumeThatWorkedSaysNothing. The card is a measured claim, so it must
// stay silent on the ordinary case: a resume the vendor honoured echoes the id
// it was given back, which is the shape agy.go's NextTurn comment records from a
// live round trip (same conversation_id, step_index continued, num_turns 2).
func TestAnAgyResumeThatWorkedSaysNothing(t *testing.T) {
	m := agyForkModel("agy-saved-thread")

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorAntigravity, Kind: runner.KindSession, SessionID: "agy-saved-thread"},
		{Vendor: model.VendorAntigravity, Kind: runner.KindMeta, Text: "ok", SessionID: "agy-saved-thread"},
	})

	if note := m.st.Columns[0].Note; note != "" {
		t.Errorf("a thread that resumed was reported lost: %q", note)
	}
	if m.threadLost[model.VendorAntigravity] {
		t.Error("a working resume was flagged as a lost thread")
	}
	if got := m.sessions[model.VendorAntigravity]; got != "agy-saved-thread" {
		t.Errorf("the seat's thread = %q, want the id it resumed", got)
	}
}

// TestAFreshAgyTurnIsNotAForkedThread. A first turn asks to resume nothing, so
// the new conversation id it comes back with is simply this seat's thread. The
// gate is the empty forkWatch, which is what dispatch leaves behind whenever
// specFor fell through to FirstTurn — without it every opening turn in the room
// would announce a lost thread.
func TestAFreshAgyTurnIsNotAForkedThread(t *testing.T) {
	m := agyForkModel("")
	m.st.Columns[0].Restored = false
	delete(m.unproven, model.VendorAntigravity)
	delete(m.sessions, model.VendorAntigravity)

	m.applyEvents([]runner.Event{
		{Vendor: model.VendorAntigravity, Kind: runner.KindSession, SessionID: "agy-brand-new"},
	})

	if note := m.st.Columns[0].Note; note != "" {
		t.Errorf("a first turn was reported as a lost thread: %q", note)
	}
	if got := m.sessions[model.VendorAntigravity]; got != "agy-brand-new" {
		t.Errorf("a first turn did not record its own thread: %q", got)
	}
}

// TestOnlyAMeasuredVendorArmsTheForkComparison pins the honesty gate where it
// actually lives — at dispatch, on the vendor's own declaration — rather than
// only in the seat that happens to make the claim today.
//
// The comparison is vendor-neutral arithmetic; what is NOT neutral is the
// conclusion drawn from it. A vendor that re-keys a resumed thread while keeping
// its history would look identical on the wire, so a room that compared ids for
// everybody would announce lost threads it never measured (§4a.1).
func TestOnlyAMeasuredVendorArmsTheForkComparison(t *testing.T) {
	var declared []model.VendorID
	for id, v := range vendors.Registry() {
		if _, ok := v.(vendors.SilentResumeFork); ok {
			declared = append(declared, id)
		}
	}
	if len(declared) != 1 || declared[0] != model.VendorAntigravity {
		t.Fatalf("seats declaring a silent resume fork = %v, want agy alone — every other one would need its own capture first", declared)
	}
}

// TestAThreadThatAnsweredIsNeverThrownAway is the other half of the probation
// rule, and the more important half.
//
// A restored id that comes back clean is PROVEN, and from then on it is an
// ordinary session: a later failure mid-conversation must not discard it. The
// whole value of resume is that history survives a bad turn, so a rule that
// dropped the thread on any failure would quietly undo the feature it is part
// of the first time a vendor hiccuped.
func TestAThreadThatAnsweredIsNeverThrownAway(t *testing.T) {
	m := turnModel(true)
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true
	m.procs[model.VendorClaude] = &seatProc{wire: claudeWire(), resumed: true, sent: 1}

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindMeta,
		Text: "an answer", EndsTurn: true,
	}})
	if m.unproven[model.VendorClaude] {
		t.Fatal("a clean turn did not take the restored thread off probation")
	}

	// A second turn, and this time the process dies mid-flight.
	m.holdTurn(&turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{model.VendorClaude: true},
	})
	m.st.Columns[0].Phase = PhaseStreaming
	m.applyEvents([]runner.Event{{Vendor: model.VendorClaude, Kind: runner.KindDone}})

	if note := m.st.Columns[0].Note; strings.Contains(note, "saved thread") {
		t.Errorf("a working seat's death was blamed on the reattach: %q", note)
	}
	if m.sessions[model.VendorClaude] != "claude-sess-1" {
		t.Error("a proven thread was discarded because a later turn failed")
	}
}

// TestACancelledTurnKeepsTheThreadOnProbation. Stopping a turn says nothing
// about whether the vendor still has the conversation, so the id is neither
// trusted nor thrown away — it gets its real first turn next time.
func TestACancelledTurnKeepsTheThreadOnProbation(t *testing.T) {
	m := turnModel(true)
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true
	markCancelling(m, model.VendorClaude)

	m.finishColumn(&m.st.Columns[0], PhaseFailed)

	if !m.unproven[model.VendorClaude] {
		t.Error("a cancelled turn settled the thread it learned nothing about")
	}
	if m.sessions[model.VendorClaude] == "" {
		t.Error("a cancelled turn threw the restored thread away")
	}
}

// TestAStaleIdOnASpawnPerTurnSeatIsDroppedAfterOneTurn is the regression this
// design exists for, and it is the case the ErrNoResume fallback does NOT
// cover.
//
// Every adapter returns ErrNoResume only for an EMPTY id. A well-formed id whose
// conversation has aged out builds a perfectly valid `resume <dead-id>`
// invocation, and the failure shows up later as a dead process — so specFor's
// fallback never fires, and without the probation rule the room would rebuild
// the same doomed invocation on every turn until it was quit. That is three of
// the four seats, on the ordinary path of reattaching a room a few days later.
func TestAStaleIdOnASpawnPerTurnSeatIsDroppedAfterOneTurn(t *testing.T) {
	tempHome(t)
	m := newWithBrief(Options{}, Brief{Path: "p", Text: "OPERATING CONTEXT"}, GateHook{}, Reattachment{
		Room: SavedRoom{
			Workspace: resolveWorkspace(""),
			Turn:      3,
			SavedAt:   time.Now().Add(-72 * time.Hour),
			Sessions:  map[model.VendorID]string{model.VendorCodex: "long-expired"},
		},
	})
	m.st.Columns = []Column{{
		Vendor: model.VendorCodex, Label: "Codex",
		Avail: AvailInstalled, Phase: PhaseWaiting, Binary: "codex",
	}}
	m.st.Columns[0].Restored = true
	m.holdTurn(&turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorCodex: true},
		persistent: map[model.VendorID]bool{},
	})

	// The vendor could not find the conversation, so the process died.
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorCodex, Kind: runner.KindError,
		Note: "exit status 1: no rollout found", ExitCode: 1,
	}})

	if id := m.sessions[model.VendorCodex]; id != "" {
		t.Fatalf("the stale id survived its failed turn and will be retried forever: %q", id)
	}
	if !strings.Contains(m.st.Columns[0].NoteDetail, "saved thread") {
		t.Errorf("the column does not say the thread was lost: %q", m.st.Columns[0].NoteDetail)
	}

	// And the NEXT turn is therefore a first turn, briefed, rather than another
	// resume of a conversation that is not there.
	c := &Column{Vendor: model.VendorCodex, Binary: "codex", Avail: AvailInstalled}
	spec, resumed, err := m.specFor(vendors.Codex{}, c, "next question")
	if err != nil {
		t.Fatal(err)
	}
	if resumed != "" {
		t.Errorf("the dropped id was still reported as resumed: %q", resumed)
	}
	if strings.Contains(strings.Join(spec.Args, " "), "long-expired") {
		t.Error("the dead id was rebuilt into the next invocation")
	}
	if !strings.Contains(spec.StdinPrompt, "OPERATING CONTEXT") {
		t.Error("the fresh session was not re-briefed")
	}
}

// --- what the reattached room looks like ---------------------------------

func TestReattachedRoomGolden(t *testing.T) {
	st := room()
	st.Now = time.Date(2026, 8, 4, 14, 0, 0, 0, time.UTC)
	st.Turn = 3
	st.Reattached = Reattach{
		Turn:    3,
		SavedAt: st.Now.Add(-2 * time.Hour),
	}
	// Room fact once, in the notice — columns only say whether THEIR thread
	// came back. Two seats restored and one not is the case the card exists to
	// tell apart.
	st.Notice = "reattached from ~/.telltale/council/room.json — turn 3 was the last, saved 2h ago, 2/3 seats restored"
	st.Columns[0].Restored = true
	st.Columns[1].Restored = true

	got := render(st)
	if !strings.Contains(got, "turn 3") {
		t.Error("the header does not continue the saved turn count")
	}
	if !strings.Contains(got, "saved 2h ago") {
		t.Error("the room notice does not say how stale the saved room is")
	}
	if n := strings.Count(got, "was the last"); n != 1 {
		t.Errorf("room reattach fact appears %d times, want 1 (notice only)", n)
	}
	if !strings.Contains(got, "this seat's thread came back") {
		t.Error("a restored seat does not say its thread came back")
	}
	if !strings.Contains(got, "no thread came back for this seat") {
		t.Error("the unrestored seat is not distinguished from the restored ones")
	}
	golden(t, "reattached", got)
}

// --- the transient/dead split (ADR-008, sixteenth amendment) --------------

// TestATransientFailureDoesNotForfeitARestoredThread.
//
// One-attempt probation cannot tell "the thread is gone" from "the vendor
// hiccuped", and the ninth amendment accepted that because nothing observable
// told them apart. Two signals have since been captured that do, and this is the
// first: a refusal raised BEFORE any model call. The vendor never looked at the
// conversation, so losing it here costs the user four turns of history for a
// login prompt.
func TestATransientFailureDoesNotForfeitARestoredThread(t *testing.T) {
	m := turnModel(false)
	m.st.Columns[0].Restored = true
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note:     "not signed in — authenticate this vendor in your own terminal, then dispatch again",
		Failure:  runner.FailurePreflight,
		ExitCode: 1,
	}})

	c := m.st.Columns[0]
	if id := m.sessions[model.VendorClaude]; id != "claude-sess-1" {
		t.Errorf("a pre-flight refusal cost the seat its thread: id = %q", id)
	}
	if !c.Restored {
		t.Error("the column stopped claiming a restored thread over a failure that never reached it")
	}
	// Still on probation, deliberately. The exception is one turn's reprieve,
	// not a promotion: the thread has still never been proven, so the NEXT
	// unclassified failure has to cost it the id exactly as before.
	if !m.unproven[model.VendorClaude] {
		t.Error("a transient failure took the seat off probation — the id is now unproven forever")
	}
	// And the seat says nothing about threads. What the user needs is the
	// vendor's own actionable sentence, not a reassurance stapled to it.
	if c.NoteCalm || c.NoteDetail != "" {
		t.Errorf("a transient failure rendered the lost-thread card: %q / %q", c.Note, c.NoteDetail)
	}
	if !strings.Contains(c.Note, "not signed in") {
		t.Errorf("the vendor's own actionable note was replaced: %q", c.Note)
	}
}

// TestTheMeasured503DoesNotForfeitARestoredThread is the second captured
// signal, and the one San's session actually hit.
//
// MEASURED 2026-08-04, agy 1.1.10: a turn died on "Eligibility check failed:
// UNAVAILABLE (code 503)" with an EMPTY conversation_id — before a thread was
// involved at all. Under the old rule that vendor-side outage spent the whole
// conversation.
func TestTheMeasured503DoesNotForfeitARestoredThread(t *testing.T) {
	m := turnModel(false)
	m.st.Columns[0].Vendor = model.VendorAntigravity
	m.st.Columns[0].Label = "Antigravity"
	m.st.Columns[0].Restored = true
	ts := m.turnOf(model.VendorClaude)
	ts.live = map[model.VendorID]bool{model.VendorAntigravity: true}
	m.turns = map[model.VendorID]*turnState{model.VendorAntigravity: ts}
	m.sessions[model.VendorAntigravity] = "agy-conv-1"
	m.unproven[model.VendorAntigravity] = true

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorAntigravity, Kind: runner.KindError,
		Note: "Eligibility check failed: UNAVAILABLE (code 503): The service is " +
			"currently unavailable.",
		Failure:  runner.FailureVendorUnavailable,
		ExitCode: 1,
	}})

	if id := m.sessions[model.VendorAntigravity]; id != "agy-conv-1" {
		t.Errorf("a vendor-side outage cost the seat its conversation: id = %q", id)
	}
	if !m.unproven[model.VendorAntigravity] {
		t.Error("the seat came off probation on a turn that proved nothing")
	}
}

// TestAnUnclassifiedFailureAfterATransientOneStillDropsTheThread.
//
// The reprieve must not accumulate into an exemption. A seat that survives one
// classified failure is still on probation, so the next failure this code cannot
// read still spends the id — which is what keeps the wedge the ninth amendment
// closed from reopening one transient failure at a time.
func TestAnUnclassifiedFailureAfterATransientOneStillDropsTheThread(t *testing.T) {
	m := turnModel(false)
	m.st.Columns[0].Restored = true
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true

	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Failure: runner.FailurePreflight, Note: "not signed in", ExitCode: 1,
	}})
	if m.sessions[model.VendorClaude] == "" {
		t.Fatal("the transient reprieve did not apply")
	}

	// Next turn. The per-turn classification is cleared at dispatch; here the
	// turn is re-armed directly, which is what dispatch does to it.
	delete(m.failure, model.VendorClaude)
	delete(m.threadLost, model.VendorClaude)
	m.st.Columns[0].Phase = PhaseStreaming
	m.holdTurn(&turnState{
		cancel:     func() {},
		live:       map[model.VendorID]bool{model.VendorClaude: true},
		persistent: map[model.VendorID]bool{},
	})
	m.applyEvents([]runner.Event{{
		Vendor: model.VendorClaude, Kind: runner.KindError,
		Note: "exit status 1", ExitCode: 1,
	}})

	if id := m.sessions[model.VendorClaude]; id != "" {
		t.Errorf("an unclassified failure spared the id: %q", id)
	}
	if !m.st.Columns[0].NoteCalm {
		t.Error("the seat did not render the lost-thread card after the unclassified failure")
	}
}

// TestAStaleClassificationCannotSpareTheNextTurnsThread.
//
// The classification is a fact about ONE turn and is cleared with the note it
// belongs to. Left set, a transient failure on turn 4 would spare a genuinely
// dead thread on turn 5 — the wedge, arriving one turn late.
func TestAStaleClassificationCannotSpareTheNextTurnsThread(t *testing.T) {
	tempHome(t)
	// The dispatch below is a REAL one, and the Codex seat added underneath is
	// AvailInstalled with `codex` as its binary — so on any machine that has
	// Codex, this test used to start a live `codex exec -s danger-full-access`
	// turn on the operator's own account, and CI (which has no vendors) could
	// never see it. The seat is here only so the dispatch has somebody to
	// address once Claude is excluded; nothing about this test's claim needs a
	// process to exist.
	countSpawns(t)
	m := turnModel(false)
	idle(m)
	m.st.Columns[0].Phase = PhaseIdle
	m.st.Columns = append(m.st.Columns, Column{
		Vendor: model.VendorCodex, Label: "Codex",
		Avail: AvailInstalled, Phase: PhaseIdle, Binary: "codex",
	})
	// Left over from a turn that failed transiently.
	m.failure[model.VendorClaude] = runner.FailurePreflight

	// Claude sits this turn out, so nothing in this dispatch can re-classify it.
	// A stale verdict surviving here is exactly the wedge one turn late: turn
	// N's hiccup sparing turn N+1's genuinely dead thread.
	m.st.Draft = "-@claude next brief"
	m.dispatch()

	if _, ok := m.failure[model.VendorClaude]; ok {
		t.Errorf("last turn's failure classification survived into this turn: %v",
			m.failure[model.VendorClaude])
	}
}

// TestADeadProcessExitCannotUnclassifyATransientTurn.
//
// A failed turn produces TWO events — the vendor's own failure, then the process
// exit carrying its stderr — and the second one has no classification on it. The
// ninth amendment already established that the second must not overwrite the
// first's WORDS; the same is true of its verdict, for the same reason: only one
// of the two events knows anything.
func TestADeadProcessExitCannotUnclassifyATransientTurn(t *testing.T) {
	m := turnModel(false)
	m.st.Columns[0].Restored = true
	m.sessions[model.VendorClaude] = "claude-sess-1"
	m.unproven[model.VendorClaude] = true

	m.applyEvents([]runner.Event{
		{
			Vendor: model.VendorClaude, Kind: runner.KindError, EndsTurn: true,
			Failure: runner.FailureVendorUnavailable, Note: "the service is currently unavailable",
		},
		{
			Vendor: model.VendorClaude, Kind: runner.KindError,
			Note: "exit status 1", ExitCode: 1,
		},
	})

	if id := m.sessions[model.VendorClaude]; id != "claude-sess-1" {
		t.Errorf("the process exit downgraded the turn's classification and spent the id: %q", id)
	}
}

// TestTheLostThreadCardIsACardGolden pins the shape San asked for: a short calm
// title a reader takes in at a glance, with the mechanics demoted underneath.
//
// The frame is what this is about. The previous version was one sentence
// carrying an outcome and a mechanism, opened by a ⚠, wrapping to three lines of
// uniform weight in a 37-cell column — three columns of that is a room that
// looks like it is on fire over a seat that simply starts a new session.
func TestTheLostThreadCardIsACardGolden(t *testing.T) {
	st := room()
	st.Turn = 4
	st.Columns[0].Phase = PhaseFailed
	st.Columns[0].TurnN = 4
	st.Columns[0].Prompt = "what changed in the runner?"
	st.Columns[0].Note = "thread not restored — starting fresh"
	st.Columns[0].NoteDetail = "the first turn on the saved thread failed, so this seat " +
		"let it go. your next brief opens a new session, with the brief re-applied."
	st.Columns[0].NoteCalm = true

	got := render(st)
	if !strings.Contains(got, "thread not restored") {
		t.Error("the card lost its title")
	}
	// No warning mark on this card. The glyph has to keep meaning "something
	// went wrong" for the notes where something did.
	if strings.Contains(got, "⚠ thread not restored") {
		t.Error("the calm card is still drawn as a warning")
	}
	golden(t, "thread-lost", got)

	// The same distinction under --ascii, where the mark would be "!" and the
	// weight is not rendered at all — so the words are carrying it alone, which
	// is the case this repo's rules are actually written for.
	a := Render(st, PlainStyles(), GlyphsFor(true))
	if strings.Contains(a, "! thread not restored") {
		t.Error("the calm card grew an ascii warning mark")
	}
	if !strings.Contains(a, "thread not restored") {
		t.Error("the title did not survive --ascii")
	}
}
