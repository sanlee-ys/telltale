package council

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/sanlee-ys/telltale/internal/model"
)

// `telltale council ls` — the saved room, read and never opened (design.md
// §7.27).
//
// THE SIXTH READER, AND IT HOLDS THE SAME CONTRACT. CLAUDE.md's read/write
// boundary names five: statusline, hud, snapshot (§7.22), mcp (§7.25) and
// history (§7.26). This is closest to history, because like history it reads no
// scan at all — it reads exactly one file, ~/.telltale/council/room.json,
// through LoadRoom, the same loader the room itself uses.
//
//   - It WRITES nothing. Not the file it read, not a cache, not a lock. A
//     refused file is left exactly where it was, which is already LoadRoom's
//     rule and is not weakened by a second reader.
//   - It SPAWNS nothing. Detect() resolves binary names against PATH, the
//     environment and a list of known locations; it starts no process. That is
//     the line `doctor` draws too — §9.42's "one moment probing is allowed" is
//     about running `--version`, which this mode does not do.
//   - It BINDS nothing. No port, no pipe. One io.Writer.
//   - It RELAYS no quota, so it holds the boundary with the same one item spare
//     that snapshot and history hold it with, and for the identical reason: it
//     renders no quota of its own, so it has none to relay.
//
// The reason that is spelled out at the same length the other five spell it is
// that council is this product's one ratified exception, for spawning and for a
// single write. A council sub-mode that read like a gauge and wrote like the
// room would be that exception growing by accident. This one is a gauge.
//
// WORDS AND NO COLOUR, on doctor's and history's precedent (main.go's reader
// modes). Every fact carries its own label, so nothing depends on column
// alignment surviving a narrow terminal.
//
// NO FLAGS AT ALL, and that is a decision rather than an omission. The other
// readers take --root to point at a corpus of VENDOR stores. This mode reads
// telltale's own state, so --root would have to mean something different here,
// and one flag with two meanings across two modes is worse than no flag. A test
// redirects the home directory instead, which is what the rest of this package
// already does.

// ListRooms prints what the saved room holds.
//
// It returns an error only for a failure that leaves nothing to say at all. A
// saved room that EXISTS and cannot be used is not that: LoadRoom hands back a
// reason in its own words, this prints the reason, and the exit code stays 0 —
// telltale's own state being damaged is a state to report, never the reason a
// read command fails.
func ListRooms(w io.Writer) error {
	re, err := LoadRoom()
	switch {
	case errors.Is(err, ErrNoSavedRoom):
		re, err = Reattachment{}, nil
	case err != nil:
		// The state directory could not even be located. Reported as a line,
		// on the same rule the room follows when it opens anyway and says why.
		re, err = Reattachment{Ignored: "the saved room could not be looked up: " + err.Error()}, nil
	}
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()
	for _, line := range listRoomLines(re, availability(), home, time.Now()) {
		if _, werr := fmt.Fprintln(w, line); werr != nil {
			return werr
		}
	}
	return nil
}

// availability is what this machine can actually run, per vendor.
//
// Read once and passed in, so listRoomLines stays pure over its arguments and
// can be tested without a PATH. That is Render's own rule (CLAUDE.md's purity
// note) applied to a reader: the function that decides the WORDS must not also
// decide the FACTS.
func availability() map[model.VendorID]Availability {
	out := map[model.VendorID]Availability{}
	for _, info := range Detect() {
		out[info.Vendor] = info.Avail
	}
	return out
}

// listRoomLines is the whole rendering, pure over its four arguments.
func listRoomLines(re Reattachment, avail map[model.VendorID]Availability, home string, now time.Time) []string {
	out := []string{"telltale council ls — the saved room, read and not opened", ""}

	switch {
	case re.Ignored != "":
		// A file that exists and cannot be used, in LoadRoom's own words. The
		// remedy is named, because a refusal without one is this room's stated
		// defect (§9.17's tell) and it costs one clause here.
		return append(out,
			"the saved room was not read: "+re.Ignored,
			"",
			"the file is left where it is. the next completed turn overwrites it.")
	case !re.Active():
		return append(out,
			"no room is saved yet.",
			"",
			"telltale council opens one. it saves after the first completed turn.")
	}

	room := re.Room
	path := abbreviate(re.Path, home)
	if re.Adopted {
		// State the user has never heard of is state this mode owes them the
		// source of — the same sentence the room's own reattach notice makes.
		path += " (the old per-workspace format, adopted on the next launch)"
	}

	out = append(out,
		"  file       "+path,
		"  saved      "+room.SavedAt.Format(time.RFC3339)+" ("+age(now.Sub(room.SavedAt))+")",
		"  turn       "+itoa(room.Turn)+" was the last",
		"  workspace  "+abbreviate(room.Workspace, home),
	)
	if room.Posture != "" {
		// RECORDED, NEVER RESTORED, and the clause says so. reattach() refuses
		// to re-apply this field on purpose — a posture that arrives from a file
		// is not one anyone typed — and a listing that printed it bare would
		// undo that ruling by implication.
		out = append(out, "  posture    "+room.Posture+" (what it stood in; the next room takes its posture from the command line)")
	}
	if room.BriefPath != "" {
		// The PATH, and the clause names the limit. SavedRoom stores the path
		// and never the text, for the reason the struct states: telltale is
		// public and the briefing it carries is not.
		out = append(out, "  brief      "+abbreviate(room.BriefPath, home)+" (the path only — this mode does not open it)")
	}
	if len(room.Seats.Only) == 0 {
		out = append(out, "  roster     every vendor that can be driven")
	} else {
		names := make([]string, 0, len(room.Seats.Only))
		for _, v := range room.Seats.Only {
			names = append(names, string(v))
		}
		out = append(out, "  roster     "+strings.Join(names, ", "))
	}

	out = append(out, "")
	out = append(out, seatLines(room, avail)...)
	out = append(out, "",
		// The limit, stated rather than left to be inferred from the word
		// "saved". Nothing this mode can read proves a vendor still holds a
		// session, so it never says live and never says resumable.
		"nothing here proves a thread is still live. only the vendor answers that, and only when",
		"a process asks it to resume — which is what telltale council does when it rebuilds a seat.")
	return out
}

// seatLines is one line per addressable vendor, in seating order.
//
// EVERY ADDRESSABLE VENDOR, not only the ones with an id. A seat that saved
// nothing is a fact worth a line: it says the room knows this vendor and has no
// thread for it, which is different from the vendor being absent from the
// listing for a reason the reader has to guess at.
//
// THREE STATES, KEPT APART (§4a.1). A saved id on a machine that cannot run the
// vendor is not the same fact as no saved id, and collapsing them would tell an
// operator on their second machine that a conversation is gone while its id sits
// on disk. The vendor being missing is a fact about THIS BOX; the id is a fact
// about the room.
func seatLines(room SavedRoom, avail map[model.VendorID]Availability) []string {
	// addressableVendors' own order, which is seating order — the same order the
	// footer, the grid and `--vendor`'s help already print. A listing sorted any
	// other way would be a sixth spelling of the roster.
	seats := addressableVendors()
	width := 0
	for _, v := range seats {
		if len(string(v)) > width {
			width = len(string(v))
		}
	}

	out := make([]string, 0, len(seats))
	for _, v := range seats {
		name := string(v) + strings.Repeat(" ", width-len(string(v)))
		id := room.Sessions[v]
		switch {
		case id == "":
			out = append(out, "  "+name+"  no thread saved")
		case avail[v] != AvailInstalled:
			out = append(out, "  "+name+"  saved, and this machine cannot run the vendor")
		default:
			out = append(out, "  "+name+"  saved")
		}
	}
	return out
}
