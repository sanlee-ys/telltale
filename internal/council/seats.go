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
				" (want all, claude, codex, agy or cursor)")
		}
		if !out.names(v) {
			out.Only = append(out.Only, v)
		}
	}
	return out, nil
}

func (s Seats) names(v model.VendorID) bool {
	for _, id := range s.Only {
		if id == v {
			return true
		}
	}
	return false
}
