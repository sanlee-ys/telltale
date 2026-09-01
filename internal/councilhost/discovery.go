package councilhost

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// HostFile is what a host writes so a later launch can say WHAT is there.
//
// It is the fourth thing council writes under ~/.telltale/, and it needed an
// argument rather than an assumption, because quota.go states in as many words
// that the boundary "grants council exactly one write, council/room.json, and a
// second one would have to be argued from scratch". The argument: this is the
// SAME CLASS of file, in the SAME directory, holding the SAME class of value,
// for the SAME purpose — the keys that let a later launch find what is already
// there. resume.go already states the leak profile of exactly this shape, which
// directory was worked in, when, and a set of opaque ids, and not a word anyone
// said. A pid does not change that sentence.
//
// **Numbers and keys only. No transcript, no prompt, no brief, no vendor
// output.** The room's conversation lives in host memory and dies with the
// host, and design.md §7.28's read/write boundary says why that rule does not
// get to change because the process holding the data changed.
//
// There is precedent for a second council write that was argued and taken:
// gatehook.go's ephemeral settings file, which documents its own Windows ACL
// caveat rather than assuming one.
type HostFile struct {
	Version int `json:"version"`
	// PID is the host process. It is NOT a liveness answer: a pid is reusable,
	// and a stale host.json is the normal case after a hard kill. See the note
	// at the bottom of this file.
	PID int `json:"pid"`
	// Pipe is the transport's name. Derived, and not a secret: the pipe's
	// protection is its security descriptor.
	Pipe string `json:"pipe"`
	// StartedAt is when the host opened the room.
	StartedAt time.Time `json:"started_at"`
	// Workspace is the directory turns are dispatched against.
	Workspace string `json:"workspace"`
	// Seats is the roster, by vendor id.
	Seats []model.VendorID `json:"seats"`
	// Turn is how many turns the host has dispatched.
	Turn int `json:"turn"`
}

// HostFileVersion is HostFile.Version's current value.
const HostFileVersion = 1

// hostFileName is the file's name beside room.json.
const hostFileName = "host.json"

// HostPath is where the discovery file lives: beside room.json, in the
// directory council is already ratified to write.
//
// It takes the council directory rather than finding it, so that this package
// does not need council's own RoomPath and the two cannot drift about where
// the directory is. The caller that knows the room knows the directory.
func HostPath(councilDir string) string { return filepath.Join(councilDir, hostFileName) }

// WriteHostFile writes the discovery file, creating the directory if needed.
func WriteHostFile(councilDir string, f HostFile) error {
	if err := os.MkdirAll(councilDir, 0o700); err != nil {
		return err
	}
	f.Version = HostFileVersion
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	// Written whole and replaced, not appended. A half-written discovery file
	// would be read as a host that does not exist, which is the one answer this
	// file must never give wrongly.
	//
	// The temp name carries this process's pid. A fixed one let two hosts
	// sharing a council directory clobber each other's half-written file, and
	// left a stray `.tmp` that RemoveHostFile does not clean when the rename
	// failed.
	tmp := fmt.Sprintf("%s.%d.tmp", HostPath(councilDir), os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, HostPath(councilDir))
}

// ReadHostFile reads the discovery file.
//
// A missing file is reported as os.ErrNotExist and is a perfectly ordinary
// state, not a fault: no host has run in this directory, or the last one exited
// cleanly and removed it.
func ReadHostFile(councilDir string) (HostFile, error) {
	b, err := os.ReadFile(HostPath(councilDir))
	if err != nil {
		return HostFile{}, err
	}
	var f HostFile
	if err := json.Unmarshal(b, &f); err != nil {
		return HostFile{}, err
	}
	if f.Version != HostFileVersion {
		return HostFile{}, errors.New("councilhost: host.json carries a version this build does not read")
	}
	return f, nil
}

// RemoveHostFile deletes the discovery file on a clean exit.
func RemoveHostFile(councilDir string) error {
	err := os.Remove(HostPath(councilDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Liveness is NOT implemented here, and the omission is deliberate.
//
// design.md §7.28's ruling stands: the file says WHAT, the pipe says WHETHER. A
// pid is reusable and a stale host.json is the normal case after a hard kill,
// so this file must never be read for liveness.
//
// The obvious implementation — dial the pipe and see — is WRONG, and it was
// written and removed rather than shipped. Dialling CONSUMES the host's single
// pipe instance: the host's Accept returns, the probe closes, and the host
// reads that as its client disconnecting and ends the room. A liveness check
// that kills the room it is checking is worse than none. The other half is just
// as bad: against a BUSY host the dial comes back ERROR_PIPE_BUSY, which the
// removed version swallowed and reported as "not running" — a live room called
// dead.
//
// A correct probe has to answer "does this name exist" without connecting, and
// that belongs with the surface that needs it. Nothing in this change reads
// liveness: detach is not exposed, so the only client of a host is the process
// that started it and already knows. Rung 4 owns discovery, and it should build
// this against a rejoin path it can measure rather than inherit a function
// nobody exercised.
