package council

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// roomVersion is the schema version of the saved-room file.
//
// Bumped whenever a field changes meaning rather than whenever one is added: a
// reader that finds a version it does not know refuses the file outright, so a
// bump costs every user their reattach. Additive fields are handled by the zero
// value instead.
const roomVersion = 1

// maxRoom bounds the state file, mirroring LoadBrief's ceiling on the brief.
//
// The file council writes is a few hundred bytes: six scalars and one id per
// vendor. This is orders of magnitude of headroom, and it exists so that
// something else at that path cannot be read into memory before it is rejected.
const maxRoom = 1 << 20

// ErrNoSavedRoom is returned when --resume was asked for and this workspace has
// no state file.
//
// It is an ERROR rather than a notice, and the split from a merely unreadable
// file is deliberate (see LoadRoom). Nothing was saved here, so the user is
// wrong about the room they think they are reattaching to — most often a --cd
// pointing somewhere other than the room they remember. Opening a fresh room
// and calling it a reattach would hide that.
var ErrNoSavedRoom = errors.New("council: no saved room for this workspace")

// SavedRoom is the one file council writes. It is the KEYS to reattach, and
// nothing else.
//
// The gauges' never-writes contract is untouched: `statusline` and `hud` still
// write nothing at all (ADR-008 §2). Council was always the exception that
// spawns processes, and this is the one file it is ratified to write.
//
// What is deliberately NOT here is the whole design. No transcripts, no vendor
// output, no brief content, no prompts — every vendor already stores its own
// history against its own session id, so duplicating any of it here would be a
// second copy of a private conversation living somewhere the user did not ask
// for it. This file holds the ids that let each vendor find its own history,
// and the handful of scalars needed to say honestly what is being reattached
// to. If this file leaked, it would disclose which directory was worked in,
// when, and a set of opaque ids — not a word anyone said.
type SavedRoom struct {
	Version int `json:"version"`

	// Workspace is the absolute directory this room dispatched against. Stored
	// as well as hashed into the filename so a load can VERIFY it landed on the
	// right file rather than trusting a hash it cannot check.
	Workspace string `json:"workspace"`

	// Posture is what the room was opened as: "read", "write" or "write-gated".
	//
	// Recorded to be DISPLAYED, never to be re-applied. Restoring write posture
	// from a file would mean a room that writes to a tree without --write
	// having been typed — a grant arriving from disk instead of from a
	// keystroke, which is precisely the thing the third ADR-008 amendment made
	// visible in the header for the whole session.
	Posture string `json:"posture"`

	// Turn is how many turns the saved room dispatched.
	Turn int `json:"turn"`

	// Sessions maps a vendor id to that vendor's OWN session id. These are the
	// keys the whole file exists for: each one is an opaque handle the vendor
	// resolves against history it already holds.
	Sessions map[model.VendorID]string `json:"sessions"`

	// BriefPath is the PATH of the operating brief, never its content.
	//
	// The distinction is the same one Brief itself is built on: telltale is
	// public and the briefing it carries is not. The path is recorded so a
	// reattached room can be reopened with the same --brief; writing the text
	// here would put the user's private file into a second location they never
	// chose.
	BriefPath string `json:"brief_path,omitempty"`

	// SavedAt is when this file was written, so a reattach can say how stale it
	// is rather than presenting an old room as a current one.
	SavedAt time.Time `json:"saved_at"`
}

// Reattachment is the loader's answer: a room to restore, or a reason there is
// none.
//
// It lives here and on Model, never on State — the same boundary the brief
// keeps. Only what Render legitimately needs (a turn number, a timestamp, a
// per-seat flag) crosses over.
type Reattachment struct {
	// Path is the state file that was read.
	Path string
	// Room is the restored state. Zero when Ignored is set.
	Room SavedRoom
	// Ignored names why a file that EXISTS was not used — corrupt, written by a
	// schema this build does not know, or belonging to another workspace. Empty
	// when the load succeeded.
	Ignored string
	// Offered reports a usable saved room that the user did NOT ask to reattach
	// to, so the room can mention it before overwriting it.
	//
	// It exists because the destruction is otherwise silent and total: the state
	// file is keyed by workspace, so opening a room in a directory that already
	// has one and dispatching a single turn renames a fresh file over the old
	// keys, and four conversations become unreachable with nothing said. The
	// room does not refuse and does not prompt — one line naming the flag that
	// would have reattached is enough to make the loss a choice.
	Offered bool
}

// Active reports that there is a room to reattach to.
func (r Reattachment) Active() bool { return r.Ignored == "" && !r.Room.SavedAt.IsZero() }

// roomDir is where saved rooms live: ~/.telltale/council.
//
// Under the user's home rather than beside the workspace, and that is a privacy
// decision rather than a filesystem one. A dotfile dropped into the directory
// council was pointed at would end up in someone's repo, in their `git status`,
// and eventually in a commit — carrying the vendor session ids of a private
// conversation into a public tree.
func roomDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".telltale", "council"), nil
}

// roomKey is the filename for one workspace: the hash of its path.
//
// Hashed rather than escaped so the name has a fixed length and reveals
// nothing. A directory name can be long, can carry characters no filesystem
// agrees on, and — the reason this matters here — is often the name of a
// private project; the listing of ~/.telltale/council should not be a list of
// what the user works on.
//
// Case is folded on Windows only, because that is where the same directory has
// several spellings: `C:\Users\...` and `c:\users\...` are one directory and
// must resolve to one room. Folding on a case-sensitive filesystem would merge
// two genuinely different directories instead.
func roomKey(workspace string) string {
	k := filepath.Clean(workspace)
	if runtime.GOOS == "windows" {
		k = strings.ToLower(k)
	}
	sum := sha256.Sum256([]byte(k))
	// Half the digest. 128 bits of a collision-resistant hash is far past what a
	// filename needs, and the stored workspace is checked on load anyway — so
	// even a collision is caught rather than silently reattaching the wrong room.
	return hex.EncodeToString(sum[:16])
}

// RoomPath is the state file for one workspace.
func RoomPath(workspace string) (string, error) {
	dir, err := roomDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, roomKey(workspace)+".json"), nil
}

// SaveRoom writes one room's keys, atomically.
//
// Atomic because the alternative is a torn file, and a torn file is the exact
// input LoadRoom has to treat as corrupt — so a crash mid-write would cost the
// user the reattach this feature exists to give them. Write to a temp file in
// the SAME directory (a rename across filesystems is not atomic), then rename
// over the target, which replaces on both platforms this ships to.
//
// The guarantee is stated precisely rather than generously: a reader can never
// observe a partly-written file, because the rename is atomic with respect to
// readers and the contents are synced before it. The DIRECTORY entry is not
// fsynced, so a power loss in the instant after the rename can still leave the
// previous file in place. That is the honest boundary — this survives a crash,
// not a power cut — and the cost of the stronger guarantee is a directory
// handle Windows does not offer on the same terms.
//
// 0600 on the file and 0700 on the directory. The contents are opaque ids
// rather than secrets, but they are handles to a user's private conversations
// with four vendors, and the default umask is not a good enough answer for that.
func SaveRoom(room SavedRoom) error {
	dir, err := roomDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		// MkdirAll is a no-op on a directory that already exists, WHATEVER its
		// mode — so the 0700 above is a claim about first creation only. A
		// directory made 0755 by an earlier build, a restored backup or a sync
		// tool would keep that mode forever, and its listing is the set of
		// workspace hashes. Tightened explicitly, every time, rather than
		// trusted to how it was created. A failure here is not fatal: the file
		// itself is 0600 either way.
		_ = os.Chmod(dir, 0o700)
	}

	room.Version = roomVersion
	// Indented on purpose. This is a file about what a tool did on the user's
	// machine; they should be able to open it and read every line of it without
	// a JSON formatter, which is the same argument the room makes for putting
	// its posture on screen.
	buf, err := json.MarshalIndent(room, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	tmp, err := os.CreateTemp(dir, "room-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	// Removed on every failure path below. A temp file left behind would
	// accumulate one per crash in a directory the user never looks at.
	defer func() { _ = os.Remove(name) }()

	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		// Windows reports a mode it does not enforce this way; the ACL inherited
		// from a 0700 directory under the user profile is the control there.
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	// Synced before the rename, not after. A rename that lands ahead of the
	// bytes it points at is the one ordering that turns a crash into the torn
	// file this whole dance is avoiding.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, target(dir, room.Workspace))
}

func target(dir, workspace string) string {
	return filepath.Join(dir, roomKey(workspace)+".json")
}

// LoadRoom reads the saved room for one workspace.
//
// TWO failure modes, answered differently, and the split is the whole of this
// function's judgement:
//
//   - No file at all -> ErrNoSavedRoom. The caller turns this into a plain
//     error before the alternate screen is entered, exactly as LoadBrief does
//     for a bad --brief path. Nothing was ever saved here, so the user is
//     wrong about which room they are reattaching to, and the fix is a
//     different --cd rather than anything council can do for them.
//
//   - A file that exists but cannot be used -> a Reattachment carrying the
//     reason, and NO error. Corrupt JSON, a schema version this build does not
//     know, or a file whose stored workspace is not the one asked for. The room
//     is still perfectly usable unreattached, so it opens, says loudly why the
//     saved state was refused, and gets on with it. Crashing on a damaged file
//     would make a bad byte on disk the reason someone cannot open their tool.
//
// A refused file is left in place rather than deleted. The next completed turn
// overwrites it, which heals it without this function ever destroying something
// it merely failed to parse.
func LoadRoom(workspace string) (Reattachment, error) {
	path, err := RoomPath(workspace)
	if err != nil {
		return Reattachment{}, err
	}

	fi, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Reattachment{}, ErrNoSavedRoom
	}
	if err != nil {
		// Exists, but cannot even be described — a permission problem, or a
		// directory sitting where the file should be. That is a file that exists
		// and is unusable, which is the NOTICE case: telltale's own state being
		// damaged must not be the reason the room refuses to open.
		return Reattachment{Path: path, Ignored: "the saved room file could not be read"}, nil
	}
	if fi.Size() > maxRoom {
		// Bounded for the same reason LoadBrief bounds the brief. This file is
		// six scalars and a handful of ids; anything at this path large enough
		// to matter is not a room council wrote, and reading it into memory to
		// find that out is the wrong order to do things in.
		return Reattachment{Path: path, Ignored: "the saved room file is implausibly large"}, nil
	}

	buf, err := os.ReadFile(path)
	if err != nil {
		return Reattachment{Path: path, Ignored: "the saved room file could not be read"}, nil
	}

	var room SavedRoom
	if err := json.Unmarshal(buf, &room); err != nil {
		return Reattachment{Path: path, Ignored: "the saved room file is not readable json"}, nil
	}
	if room.Version != roomVersion {
		return Reattachment{Path: path, Ignored: "the saved room was written by schema v" +
			strconv.Itoa(room.Version) + ", this build reads v" + strconv.Itoa(roomVersion)}, nil
	}
	// The hash said this file is this workspace's; the file itself has to agree.
	// A mismatch means a collision or a hand-edited file, and reattaching a
	// DIFFERENT directory's vendor sessions would silently hand one project's
	// conversation to another.
	if roomKey(room.Workspace) != roomKey(workspace) {
		return Reattachment{Path: path, Ignored: "the saved room belongs to a different workspace"}, nil
	}
	if room.SavedAt.IsZero() {
		return Reattachment{Path: path, Ignored: "the saved room carries no timestamp"}, nil
	}
	// A room with no turns has nothing to reattach TO, and council never writes
	// one. Checked here anyway so the loader's idea of a usable room matches
	// Reattach.Active's: without it, a hand-edited turn:0 file would restore
	// every session id while the room rendered as cold, which is the worst of
	// both — seats silently resumed and a screen that says otherwise.
	if room.Turn <= 0 {
		return Reattachment{Path: path, Ignored: "the saved room records no turns"}, nil
	}
	return Reattachment{Path: path, Room: room}, nil
}

// savedPosture names the posture the room was opened with, for the record only.
func savedPosture(write, auto bool) string {
	switch {
	case write && !auto:
		return "write-gated"
	case write:
		return "write"
	default:
		return "read"
	}
}

// age renders how stale a saved room is.
//
// Coarser than dur(), which measures how long a model took to think and is
// counted in seconds. This measures how long ago a conversation was, where the
// useful distinction is hours and days — reporting "7284s ago" would be
// precision that answers no question anyone has.
func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		// Negative lands here too, and deliberately. A file dated in the future
		// is a clock change or a room copied between machines; "just now" is
		// wrong by less than inventing a negative age would be.
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m ago"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h ago"
	default:
		return strconv.Itoa(int(d.Hours()/24)) + "d ago"
	}
}
