package council

import (
	"errors"
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// ParseSeats turns a --vendor value into the room's seat set.
//
// It reuses the @mention vocabulary rather than defining a second one, which is
// not tidiness: `@codex` and `--vendor codex` name the same seat, and a flag
// that rejected the word the footer just printed would be the room being clever
// at the user's expense. The `all`/`everyone`/`council` spellings come from the
// same table too, so `--vendor all` reads exactly like `@all`.
//
// An empty value is the default room: every seat that can be driven, and none
// of the ones that cannot. A comma list is an explicit statement of who is in
// the room — drawn AND dispatched to — and it FORCES a named seat on screen
// even when it is not installed, because a user who asked for it is owed the
// card explaining why it is not there.
//
// `all` beats a list when both are given. It is the wider request, and the one
// a user reaches for when they want to see everything.
func ParseSeats(s string) (Seats, error) {
	var out Seats
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	aliases := mentionAliases()
	for _, part := range strings.Split(s, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		if allAliases[name] {
			out.All = true
			continue
		}
		v, ok := aliases[name]
		if !ok {
			return Seats{}, errors.New("unknown --vendor " + name +
				" (want all, " + strings.Join(SeatNames(), ", ") + ")")
		}
		if !out.names(v) {
			out.Only = append(out.Only, v)
		}
	}
	return out, nil
}

// seatsFor decides who is seated at launch: the roster typed at the door, the
// one the saved room recorded, or the detected table.
//
// **A typed `--vendor` OVERRIDES the saved roster and then rewrites it** (§9.32),
// which is `--cd`'s rule and `--cd`'s reasoning: an explicit launch control
// someone typed today outranks a file from yesterday, and the next save records
// the room they actually got rather than leaving the file describing a room that
// is no longer on screen. Run's workspace switch is the shape this mirrors, down
// to `restore` being the same `re.Active() && !re.Offered` — a room declined by
// `--fresh` restores neither field.
//
// The rewrite needs no code here and that is worth saying, because it looks like
// a missing branch: `stateWith` copies this into `State.Seats` and `saveRoom`
// writes `m.st.Seats`, so the first completed turn records whatever the room
// ended up with. The same one line is what makes `/seat` persist.
//
// Restoring is unconditional on the roster's own content — including the zero
// value, which is the default room saved as the default room. A saved roster
// that could only ever widen would be a `/seat` you could not undo by quitting.
func seatsFor(typed, saved Seats, restore bool) Seats {
	if typed.typed() || !restore {
		return typed
	}
	return saved
}

func (s Seats) names(v model.VendorID) bool {
	for _, id := range s.Only {
		if id == v {
			return true
		}
	}
	return false
}
