package probe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// This file is the probe's one WRITE, and the whole of what it is allowed to
// be (CLAUDE.md, "the read/write boundary"; SECURITY.md, the bounded write
// exceptions).
//
// # Why the file exists at all
//
// `telltale doctor` already says "telltale's field map for this vendor was
// measured at 0.147.0, but this machine reports 0.151.0". That sentence is
// honest and it is also an admission: every live claim this repository makes
// was paid by the owner's hand and written into prose, and nothing on the
// reader's machine re-measures any of it. A reader hears "the gauges are honest
// about being stale". The file turns one part of that debt into a fact a
// machine paid: this vendor, this build, driven HERE, on this day, through the
// three checks the room's own dispatch depends on.
//
// # What it may hold, and what it may never hold
//
// Numbers and keys only, exactly as `council/room.json`, `quota/<vendor>.json`
// and `usage/<vendor>.json` are. A probe DRIVES an agent, so the material it
// touches is the most sensitive telltale ever holds — the brief, the reply, the
// session id the vendor named, and the directory the seat ran in. None of the
// four reaches this file. What is written is the vendor id, the version string
// the binary printed, the day, the telltale build that probed, and three check
// results with their milliseconds.
//
// The failure REASON is deliberately absent too, and that is the sharpest of
// these decisions. A vendor's first stderr line routinely carries an absolute
// path, a workspace name or a session id, so a file that carried it would carry
// content by the back door — and it would do it on exactly the runs a reader is
// most likely to paste somewhere. The reason is printed in the operator's own
// terminal, where the probe ran, and it stops there. `doctor` therefore reports
// WHICH check failed and names the command that shows why, rather than quoting
// a sentence it cannot vouch for the contents of.
//
// `TestTheProbeFileCarriesKeysAndNumbersOnly` pins the serialized form, the way
// the three relay files beside it are each pinned.

// Record is one vendor's probe file.
//
// Every field is a key or a number. The struct IS the allowlist, on the same
// argument `internal/cursorhook` makes for its payload: `encoding/json` writes
// what has a destination and nothing else, so a field that does not exist here
// cannot reach the disk however the drive changes.
type Record struct {
	// Vendor is the lower-case vendor id, matching model.VendorID.
	Vendor string `json:"vendor"`
	// Version is the string the vendor's own binary printed, unchanged. Empty
	// when this machine did not print one — absent, never a guess.
	Version string `json:"version,omitempty"`
	// ProbedAt is when the drive ran, RFC 3339 through time.Time's own
	// marshaller.
	ProbedAt time.Time `json:"probed_at"`
	// TelltaleVersion is the telltale build that did the driving. A probe is a
	// claim about a vendor made by a program, and the program's own version is
	// what lets a reader weigh a result the current binary would not produce.
	TelltaleVersion string `json:"telltale_version"`
	// Checks are the three checks in the order they ran.
	Checks []CheckRecord `json:"checks"`
}

// CheckRecord is one check's outcome on disk.
type CheckRecord struct {
	// Name is the check, in one lower-case word: handshake, turn, stop.
	Name string `json:"name"`
	// Status is one of three words and there is no fourth: ok, failed,
	// not_run. Written as a word rather than as a number so a reader of the
	// raw file needs no table, and so a status this file does not know about
	// cannot arrive as an integer nobody notices.
	Status string `json:"status"`
	// Millis is how long the check took. A POINTER, so that "the check took no
	// measurable time" and "the check did not run" stay different states on
	// disk — design.md §4a.1's rule, applied to the one number this file
	// carries. A check that did not run writes no `ms` key at all.
	Millis *int64 `json:"ms,omitempty"`
}

// The three status words. They are the file's own vocabulary rather than a
// re-use of doctor's rendered words, because this is a wire format: `FAILED` is
// upper-case in a report to be found by eye, and a JSON value that changes case
// for emphasis is a value two readers parse differently.
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
	StatusNotRun = "not_run"
)

// Dir is the probe directory, ~/.telltale/probe — beside council's room.json,
// the quota relay and the token relay, and under the same root every telltale
// write lives under.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".telltale", "probe"), nil
}

// Write atomically replaces one vendor's probe file.
//
// Atomic for `quotacache.Write`'s reason: `doctor` reads this file, and a
// reader that caught a torn write would report a probe that never happened. A
// temp file in the SAME directory, then a rename — rename is only atomic within
// one volume.
//
// A record naming no vendor writes nothing. There is no file name for it, and
// inventing one would put a result on disk under a seat that did not produce
// it.
func Write(dir string, rec Record) error {
	if rec.Vendor == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, rec.Vendor+"-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, rec.Vendor+".json")); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Read returns one vendor's probe file, and reports whether there is one.
//
// An absent file and an unreadable file both report false, and that collapse is
// correct HERE and nowhere else: the caller renders "probed here: never", which
// is the honest sentence for both — nothing on this machine says this seat was
// driven. What must never happen is the other collapse, an absent file
// rendering as a pass, and the boolean is what stops a caller writing that by
// accident.
func Read(dir, vendor string) (Record, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, vendor+".json"))
	if err != nil {
		return Record{}, false
	}
	var rec Record
	if err := json.Unmarshal(raw, &rec); err != nil {
		return Record{}, false
	}
	if rec.Vendor == "" {
		// A file with no vendor in it names no seat. Reporting it as this
		// seat's result would be the file's own key going unread.
		return Record{}, false
	}
	return rec, true
}
