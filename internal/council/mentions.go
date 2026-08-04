package council

import (
	"strings"

	"github.com/sanlee-ys/telltale/internal/model"
)

// Route is who a brief is addressed to. A nil Route means everyone seated —
// the default, and the only behaviour that existed before mentions.
type Route []model.VendorID

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
	}
}

// allAliases address the whole room explicitly. Useful because typing @all is a
// statement of intent, where an unprefixed brief is merely the default.
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
// An unrecognised @token is left in the brief untouched rather than treated as
// an error. It might be a handle, a path, an email. What the room does instead
// is show the resolved routing in the footer while typing, so an @typo is
// visible as "this is going to everyone" BEFORE enter is pressed rather than
// afterwards.
//
// Returns a nil route when no mention was found, which means "everyone seated".
func ParseRoute(draft string) (Route, string) {
	aliases := mentionAliases()

	rest := draft
	var route Route
	seen := map[model.VendorID]bool{}
	all := false

	for {
		trimmed := strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(trimmed, "@") {
			break
		}
		word := trimmed
		if i := strings.IndexAny(trimmed, " \t"); i >= 0 {
			word = trimmed[:i]
		}
		name := strings.ToLower(strings.TrimPrefix(word, "@"))
		// Trailing punctuation is stripped so "@claude, @codex:" reads the way
		// it looks. A comma between mentions is how people actually write a
		// list, and refusing it would be a parser being pedantic at the user.
		name = strings.TrimRight(name, ",;:")

		if allAliases[name] {
			all = true
		} else if v, ok := aliases[name]; ok {
			if !seen[v] {
				seen[v] = true
				route = append(route, v)
			}
		} else {
			// Not a vendor. Stop here and leave it in the brief.
			break
		}
		rest = trimmed[len(word):]
	}

	brief := strings.TrimLeft(rest, " \t")
	if all {
		// @all is explicit "everyone", which is the same set as no mention at
		// all. Returning nil keeps one meaning for one thing downstream.
		return nil, brief
	}
	if len(route) == 0 {
		// No mentions consumed: the draft is unchanged, including any @token
		// that did not resolve.
		return nil, draft
	}
	return route, brief
}

// addresses reports whether a route includes a vendor. A nil route includes
// everyone.
func (r Route) addresses(v model.VendorID) bool {
	if len(r) == 0 {
		return true
	}
	for _, id := range r {
		if id == v {
			return true
		}
	}
	return false
}

// labels renders a route for the footer, using each vendor's own id so what is
// displayed matches what can be typed.
func (r Route) labels() []string {
	out := make([]string, 0, len(r))
	for _, v := range r {
		out = append(out, string(v))
	}
	return out
}
