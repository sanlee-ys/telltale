package councilhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// RoomFile is `council/room.json` as the HOST writes it: the same file, the
// same keys, the same schema version as the one the single-process room
// writes through council's SaveRoom (resume.go), so that the next
// `telltale council` — hosted or not — reads a hosted room's session ids
// through the one loader the room already has.
//
// # Why this is a second struct and not an import
//
// The host cannot import package council (boundary_test.go's
// TestTheHostDoesNotImportTheRoom), so it cannot reach SaveRoom. The choice was
// between lifting council's SavedRoom into a leaf package both could import,
// and writing the same bytes from here. The second is the smaller change and
// it is pinned from BOTH sides rather than trusted: this package asserts the
// serialized key set (TestTheHostsRoomFileIsNumbersAndKeysOnly), and package
// council asserts that council.LoadRoom reads what this writes, field by field
// (TestTheRoomReadsWhatTheHostWrote). A drift in either schema reddens one of
// the two.
//
// # What is in it, and what is not — resume.go's ruling, unchanged
//
// Numbers and keys only. The workspace, the roster, the posture word, the turn
// count, one opaque id per vendor and a timestamp. No transcript, no prompt, no
// brief, no vendor output, no note: every one of those lives in host memory and
// dies with the host (the package doc's third rule). If this file leaked, it
// would disclose which directory was worked in, when, and a set of opaque ids —
// resume.go's leak sentence, true of this writer as it is of that one.
//
// There is no `brief_path`. The host takes no brief file (hostcmd.go refuses
// `--brief` with `--host`), and council's own field is `omitempty`, so the key
// is absent here as it is absent from a single-process room opened without one.
type RoomFile struct {
	Version int `json:"version"`
	// Workspace is the directory the room is pointed at.
	Workspace string `json:"workspace"`
	// Seats is the roster. A hosted room's roster is ALWAYS explicit — the
	// client resolved it and passed it on argv — so it is recorded as the
	// typed form (`only`), which is exactly the seats this host held.
	Seats RoomRoster `json:"seats,omitempty"`
	// Posture is the room's word for what the seats may do: "read" or
	// "write". Never "write-gated": New refuses a gated room, and a hosted
	// write room is the ungated one council's savedPosture spells "write".
	Posture string `json:"posture"`
	// Turn is the room's turn count, continued from the saved room when one
	// was reattached (Host.reattach).
	Turn int `json:"turn"`
	// Sessions maps a vendor id to that vendor's OWN session id. The keys the
	// file exists for.
	Sessions map[model.VendorID]string `json:"sessions"`
	// SavedAt is when this file was written.
	SavedAt time.Time `json:"saved_at"`
}

// RoomRoster is council.Seats' serialized form: the typed list, by vendor id.
// `all` is never written by a host — its roster is the explicit list.
type RoomRoster struct {
	Only []model.VendorID `json:"only,omitempty"`
}

// RoomFileVersion is council's roomVersion, and it has to stay equal to it.
// The council-side round-trip test is what holds the two together.
const RoomFileVersion = 2

// roomFileName is the file's name: the one council.RoomPath names.
const roomFileName = "room.json"

// RoomFilePath is where the room file lives: beside host.json, in the directory
// the caller handed the host (HostPath's reasoning).
func RoomFilePath(councilDir string) string { return filepath.Join(councilDir, roomFileName) }

// WriteRoomFile writes the room file atomically, on SaveRoom's own terms.
//
// Temp file in the SAME directory, synced, then renamed over the target, so a
// reader never observes a torn file — the exact input council.LoadRoom has to
// treat as corrupt. 0600 on the file and 0700 on the directory, tightened on
// every write rather than trusted to creation, for SaveRoom's reason: the ids
// are handles to the operator's private conversations with four vendors.
func WriteRoomFile(councilDir string, f RoomFile) error {
	if err := os.MkdirAll(councilDir, 0o700); err != nil {
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(councilDir, 0o700)
	}
	f.Version = RoomFileVersion
	buf, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')

	tmp, err := os.CreateTemp(councilDir, "room-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()

	if err := tmp.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(buf); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, RoomFilePath(councilDir))
}
