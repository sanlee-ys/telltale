package council

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Route is who a brief is addressed to.
//
// Shapes:
//
//	Route{}                                    everyone seated — explicit @all
//	Route{Vendors: [claude]}                   silence — Claude alone (defaultRoute)
//	Route{Vendors: [codex]}                    codex alone — a mention names a seat
//	Route{Vendors: [claude], Negated: true}     everyone seated EXCEPT claude
//
// Mixed is not a fourth shape. It is the refusal: a draft carrying both forms
// at once addresses nobody, because the two forms are two contradictory
// theories of who is in the room and it is not the room's place to pick one.
//
// INVARIANT: Negated implies at least one vendor. `-@all` is expanded to the
// whole addressable set at parse time rather than modelled as an empty
// exclusion, so "everyone but nobody" is not a state that exists.
type Route struct {
	// Vendors is who the brief is FOR when Negated is false, and who it is
	// explicitly NOT for when Negated is true.
	Vendors []model.VendorID
	Negated bool
	Mixed   bool
}

// defaultRoute is where an unaddressed brief goes: Claude alone.
//
// Cross-vendor fan-out is not a fleet default. Broadcasting every unaddressed
// turn bills every seated vendor's quota, including the scarce independence
// pool. Silence → control plane; @all / multi-mention widens when you want
// the committee.
//
// History: briefly inverted to everyone-by-default after San preferred
// committee-as-product ("if I only wanted one model, I can do that ad-hoc").
// A week of council use depleted Codex's weekly pool while every casual turn
// still billed it, proving the expensive default was the defect. Restored
// 2026-08-07.
func defaultRoute() Route {
	return Route{Vendors: []model.VendorID{model.VendorClaude}}
}

// mentionAliases maps what a user might type to a vendor.
//
// Both spellings survive for the same reason the HUD's --vendor flag accepts
// both: `agy` is the id the header and footer print, `antigravity` is what the
// product is called everywhere else in this repo, and rejecting the name a
// reader just saw on screen would be the room being clever at their expense.
func mentionAliases() map[string]model.VendorID {
	return map[string]model.VendorID{
		"claude":      model.VendorClaude,
		"codex":       model.VendorCodex,
		"agy":         model.VendorAntigravity,
		"antigravity": model.VendorAntigravity,
		// A seat nobody can address is only half seated: without this, @cursor
		// falls through as an unrecognised token, stays in the brief as prose,
		// and the turn silently goes to the default lane instead.
		"cursor": model.VendorCursor,
		// One spelling, because the binary and the product are the same word.
		// There is no second name to accept the way `antigravity` shadows `agy`.
		"grok": model.VendorGrok,
	}
}

// addressableVendors is every vendor a mention can name, in seating order.
//
// It exists for `-@all`, which has to become a LIST of exclusions rather than a
// fourth kind of route (see the Route invariant). The set is closed and small,
// so expanding it here keeps addresses() one rule instead of two.
//
// TestEveryAddressableVendorIsExcludable pins it against mentionAliases, so a
// fifth seat cannot be added to the vocabulary while `-@all` quietly goes on
// meaning the four this room happened to have.
func addressableVendors() []model.VendorID {
	return []model.VendorID{
		model.VendorClaude,
		model.VendorCodex,
		model.VendorAntigravity,
		model.VendorCursor,
		model.VendorGrok,
	}
}

// SeatNames is the addressable roster as the words a user types, in seating
// order. Every surface that TELLS someone who can be seated reads it from here.
//
// It exists because that list used to be typed out by hand in four such places
// — the `--vendor` flag help and the usage block in cmd/telltale, /seat's typo
// notice, and /arena drop's — and when grok became the fifth seat (§9.39) all
// four went on naming the four seats the room had before it. ParseSeats had
// accepted `grok` the whole time, so the help was describing a smaller room
// than the one that existed: a user reading `--help` was told a supported
// vendor did not exist. That is the honest-gauge rule (§4a.1) applied to a
// control surface rather than a value — what the room SAYS it accepts has to be
// derived from what it accepts, not restated alongside it.
//
// Aliases are deliberately excluded. `antigravity` is an accepted spelling of
// `agy`, not a sixth seat, and listing it would make a five-seat room read as
// six. This returns seats; mentionAliases stays the authority on spellings.
func SeatNames() []string {
	vendors := addressableVendors()
	out := make([]string, 0, len(vendors))
	for _, v := range vendors {
		out = append(out, string(v))
	}
	return out
}

// allAliases address the whole room explicitly. They are how you convene the
// committee now that silence goes to Claude alone (see defaultRoute).
var allAliases = map[string]bool{"all": true, "everyone": true, "council": true}

// ParseRoute splits a draft into its leading @mentions and the brief itself.
//
// Only LEADING mentions count. "@claude @codex compare these" addresses two
// vendors; "ask @claude about it" addresses nobody in particular and the @claude
// stays in the text, because at that point it is prose about Claude rather than
// an instruction to route. The alternative — treating any @token anywhere as
// routing — would silently drop a vendor from a turn because of a word in the
// middle of a sentence, and the user would have no way to see it happen.
//
// A leading `-@vendor` EXCLUDES instead of narrowing: `-@claude go` reaches
// every seated vendor except Claude. It is deliberately the same grammar in the
// same position rather than a second one — leading tokens only, same aliases,
// same case-insensitivity, same trailing punctuation, same dedupe — because a
// second routing vocabulary is a second thing to learn and a second thing to
// get wrong. `-@` is one keystroke away from `@` and reads as subtraction.
//
// MIXING THE TWO IS REFUSED. "@claude -@codex" is not under-specified, it is
// over-specified: the positive form starts from nobody and adds, the negative
// starts from everyone and subtracts, and a brief that does both states two
// contradictory theories of who is in the room. Reconciling them would mean the
// room picking one silently — exactly the class of hidden decision the footer's
// live routing exists to prevent. The user picks the form; the room does not
// guess. `@all -@claude` is NOT that case and IS accepted: `@all` names
// everyone, so it names the set the exclusion subtracts from, and the two
// agree.
//
// An unrecognised @token is left in the brief untouched rather than treated as
// an error. It might be a handle, a path, an email. What the room does instead
// is show the resolved routing in the footer while typing, so an @typo is
// visible as the resolved routing BEFORE enter is pressed rather than
// afterwards. A refused route is visible in the same cell, for the same reason
// and at the same moment.
//
// Returns defaultRoute() when no mention was found (Claude alone). Returns the
// zero Route for explicit @all (everyone seated).
func ParseRoute(draft string) (Route, string) {
	aliases := mentionAliases()

	rest := draft
	var pos, neg []model.VendorID
	seenPos := map[model.VendorID]bool{}
	seenNeg := map[model.VendorID]bool{}
	all := false

	for {
		trimmed := strings.TrimLeft(rest, " \t")
		negate := strings.HasPrefix(trimmed, "-@")
		if !negate && !strings.HasPrefix(trimmed, "@") {
			break
		}
		word := trimmed
		if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
			word = trimmed[:i]
		}
		name := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(word, "-"), "@"))
		// Trailing punctuation is stripped so "@claude, @codex:" reads the way
		// it looks. A comma between mentions is how people actually write a
		// list, and refusing it would be a parser being pedantic at the user.
		name = strings.TrimRight(name, ",;:")

		v, isVendor := aliases[name]
		if !isVendor && !allAliases[name] {
			// Not a vendor. Stop here and leave it in the brief.
			break
		}

		switch {
		case !isVendor && !negate:
			all = true
		case !isVendor:
			// `-@all` is "not everyone", which is nobody. It is expanded rather
			// than special-cased so it lands where a room whose every seat was
			// excluded by name already lands: seatedIn returns 0 and dispatch
			// says none of the vendors you addressed are seated. One notice for
			// one situation, and no fourth route shape to reason about.
			for _, id := range addressableVendors() {
				if !seenNeg[id] {
					seenNeg[id] = true
					neg = append(neg, id)
				}
			}
		case negate:
			if !seenNeg[v] {
				seenNeg[v] = true
				neg = append(neg, v)
			}
		default:
			if !seenPos[v] {
				seenPos[v] = true
				pos = append(pos, v)
			}
		}
		rest = trimmed[len(word):]
	}

	brief := strings.TrimLeft(rest, " \t")

	if len(pos) > 0 && len(neg) > 0 {
		// The draft is returned UNSTRIPPED. Nothing was routed, so nothing was
		// addressing — and the user is about to retype the line anyway. Handing
		// back a brief with the mentions removed would leave them editing text
		// they did not write.
		return Route{Mixed: true}, draft
	}
	if len(neg) > 0 {
		return Route{Vendors: neg, Negated: true}, brief
	}
	if all {
		// @all is explicit "everyone". Zero route is that set downstream
		// (addresses every seat; equal four-up columns).
		return Route{}, brief
	}
	if len(pos) == 0 {
		// No mentions consumed: the draft is unchanged, including any @token
		// that did not resolve, and it goes to Claude alone.
		return defaultRoute(), draft
	}
	return Route{Vendors: pos}, brief
}

// addresses reports whether a route includes a vendor. The zero route includes
// everyone.
func (r Route) addresses(v model.VendorID) bool {
	if r.Mixed {
		// A refused route addresses nobody. Defined rather than left to fall
		// through: dispatch stops before this is reached, and a caller that
		// forgets the check should narrow to nothing rather than broadcast to
		// four quotas on a line the room said it could not read.
		return false
	}
	for _, id := range r.Vendors {
		if id == v {
			return !r.Negated
		}
	}
	// Unnamed. On an exclusion that is the whole point; on a positive route it
	// is everyone only when nothing was named at all.
	return r.Negated || len(r.Vendors) == 0
}

// label renders a route for the footer, using each vendor's own id so what is
// displayed matches what can be typed.
//
// The negative form is spelled out rather than shown as "-claude": the cell is
// read as a sentence about where the brief is going, and a reader scanning a
// footer at speed can miss a leading minus in a way they cannot miss the word
// "but".
func (r Route) label() string {
	switch {
	case r.Mixed:
		// Not "nobody", which would describe the outcome and hide the cause.
		// This cell is read while the line is still being typed, so it has to
		// say what is wrong with what was typed.
		return "mixed @ and -@"
	case r.Negated:
		return "everyone but " + strings.Join(r.ids(), ", ")
	case len(r.Vendors) == 0:
		return "everyone"
	default:
		return strings.Join(r.ids(), ", ")
	}
}

func (r Route) ids() []string {
	out := make([]string, 0, len(r.Vendors))
	for _, v := range r.Vendors {
		out = append(out, string(v))
	}
	return out
}
