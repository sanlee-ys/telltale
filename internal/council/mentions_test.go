package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// to and not build the two positive/negative route shapes, so a table row reads
// as the sentence it is testing rather than as a struct literal.
func to(v ...model.VendorID) Route  { return Route{Vendors: v} }
func not(v ...model.VendorID) Route { return Route{Vendors: v, Negated: true} }

// sameRoute is spelled out because Route holds a slice and is therefore not
// comparable with ==. It compares every field, deliberately: a test that only
// checked the vendor list would pass an exclusion off as a narrowing, which is
// the one confusion this whole feature has to not make.
func sameRoute(a, b Route) bool {
	if a.Negated != b.Negated || a.Mixed != b.Mixed || len(a.Vendors) != len(b.Vendors) {
		return false
	}
	for i := range a.Vendors {
		if a.Vendors[i] != b.Vendors[i] {
			return false
		}
	}
	return true
}

func TestParseRoute(t *testing.T) {
	cases := []struct {
		name  string
		draft string
		want  Route
		brief string
	}{
		{
			// The committee rule, encoded: silence convenes the whole room. The
			// old rule sent this to Claude alone to protect two constrained
			// quota pools; that was overruled, and the bill is now every seated
			// vendor on every unaddressed turn.
			name:  "no mention goes to everyone, not claude alone",
			draft: "should we resume or re-send?",
			want:  Route{},
			brief: "should we resume or re-send?",
		},
		{
			name:  "one mention",
			draft: "@codex is the resume flag set right?",
			want:  to(model.VendorCodex),
			brief: "is the resume flag set right?",
		},
		{
			// The other half of the flip, and the one a user actually reaches
			// for: asking ONE model something without leaving the room. A
			// mention has to cut the room down to the seat it names, or the
			// ad-hoc case the default was inverted for does not exist.
			name:  "@claude narrows to one seat rather than widening",
			draft: "@claude just you on this one",
			want:  to(model.VendorClaude),
			brief: "just you on this one",
		},
		{
			name:  "several mentions",
			draft: "@claude @agy compare these two",
			want:  to(model.VendorClaude, model.VendorAntigravity),
			brief: "compare these two",
		},
		{
			name:  "commas between mentions are how people write a list",
			draft: "@claude, @codex: who is right?",
			want:  to(model.VendorClaude, model.VendorCodex),
			brief: "who is right?",
		},
		{
			name:  "both spellings of antigravity",
			draft: "@antigravity thoughts?",
			want:  to(model.VendorAntigravity),
			brief: "thoughts?",
		},
		{
			name:  "case is not significant",
			draft: "@Claude @CODEX go",
			want:  to(model.VendorClaude, model.VendorCodex),
			brief: "go",
		},
		{
			name:  "a repeated mention seats the vendor once",
			draft: "@codex @codex go",
			want:  to(model.VendorCodex),
			brief: "go",
		},
		{
			// @all is redundant now — it names the default — and it stays
			// accepted rather than erroring, so it has to resolve to the same
			// zero route silence does.
			name:  "@all is redundant and still convenes the whole room",
			draft: "@all what do you think?",
			want:  Route{},
			brief: "what do you think?",
		},
		{
			// The rule that keeps routing predictable. Treating any @token
			// anywhere as routing would silently drop a vendor from a turn
			// because of a word in the middle of a sentence.
			name:  "a mention mid-sentence is prose, not routing",
			draft: "ask @claude about the resume flag",
			want:  Route{},
			brief: "ask @claude about the resume flag",
		},
		{
			// An unresolvable token is left alone rather than erroring. The
			// footer shows the resolved routing while typing, so a typo reads
			// as "going to everyone" before enter rather than after.
			name:  "an unknown mention stays in the brief",
			draft: "@claud fix the typo",
			want:  Route{},
			brief: "@claud fix the typo",
		},
		{
			name:  "a known mention followed by an unknown one stops at the unknown",
			draft: "@codex @nobody go",
			want:  to(model.VendorCodex),
			brief: "@nobody go",
		},
		{
			name:  "leading whitespace does not defeat a mention",
			draft: "   @agy  go",
			want:  to(model.VendorAntigravity),
			brief: "go",
		},
		{
			name:  "a mention with no brief leaves an empty brief",
			draft: "@codex",
			want:  to(model.VendorCodex),
			brief: "",
		},

		// ------------------------------------------------- exclusion

		{
			// The sentence this feature was asked for, verbatim: "everyone
			// except claude" and the other three answer.
			name:  "a leading -@ excludes rather than narrows",
			draft: "-@claude everyone else, please",
			want:  not(model.VendorClaude),
			brief: "everyone else, please",
		},
		{
			name:  "several exclusions subtract several seats",
			draft: "-@claude -@codex what do the other two think?",
			want:  not(model.VendorClaude, model.VendorCodex),
			brief: "what do the other two think?",
		},
		{
			// Every property the positive form has, the negative form has too,
			// because it is the same parser and not a second one.
			name:  "exclusions take the same aliases, case and punctuation",
			draft: "-@ANTIGRAVITY, -@Cursor: go",
			want:  not(model.VendorAntigravity, model.VendorCursor),
			brief: "go",
		},
		{
			name:  "a repeated exclusion drops the seat once",
			draft: "-@agy -@agy go",
			want:  not(model.VendorAntigravity),
			brief: "go",
		},
		{
			// @all names the DEFAULT rather than adding a seat, so it names the
			// set the exclusion subtracts from. The two agree, so this is
			// redundant rather than contradictory — the same ruling @all got
			// when the room became the default (ADR-008, thirteenth).
			name:  "@all composes with an exclusion instead of colliding with it",
			draft: "@all -@claude go",
			want:  not(model.VendorClaude),
			brief: "go",
		},
		{
			// Over-specified, not under-specified: one form starts from nobody
			// and adds, the other starts from everyone and subtracts. The room
			// refuses rather than picking one silently.
			name:  "mixing the two forms is refused",
			draft: "@claude -@codex go",
			want:  Route{Mixed: true},
			brief: "@claude -@codex go",
		},
		{
			name:  "mixing is refused in the other order too",
			draft: "-@codex @claude go",
			want:  Route{Mixed: true},
			brief: "-@codex @claude go",
		},
		{
			// -@all is "not everyone", which is nobody. It is expanded to the
			// whole addressable set rather than modelled as a fourth shape, so
			// it lands in the same not-seated notice a room whose every seat was
			// excluded by name already lands in.
			name:  "-@all excludes every addressable seat",
			draft: "-@all go",
			want:  not(addressableVendors()...),
			brief: "go",
		},
		{
			name:  "-@all followed by a positive mention is still a mix",
			draft: "-@all @claude go",
			want:  Route{Mixed: true},
			brief: "-@all @claude go",
		},
		{
			// The same rule the positive form has: an unresolvable token is not
			// an error, it is prose. A brief that opens with a flag-looking word
			// must not be eaten by the router.
			name:  "an unknown exclusion stays in the brief",
			draft: "-@claud fix the typo",
			want:  Route{},
			brief: "-@claud fix the typo",
		},
		{
			// A bare dash is not routing. Only "-@" is.
			name:  "a leading dash that is not -@ is left alone",
			draft: "-- fix the flag parsing",
			want:  Route{},
			brief: "-- fix the flag parsing",
		},
		{
			name:  "an exclusion mid-sentence is prose, not routing",
			draft: "ask everyone -@claude style",
			want:  Route{},
			brief: "ask everyone -@claude style",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, brief := ParseRoute(c.draft)
			if !sameRoute(got, c.want) {
				t.Fatalf("route = %+v, want %+v", got, c.want)
			}
			if brief != c.brief {
				t.Errorf("brief = %q, want %q", brief, c.brief)
			}
		})
	}
}

// TestMentionsAreStrippedFromWhatVendorsReceive: routing is addressing, not
// content. Leaving "@codex @claude" in the text makes every vendor read a
// header about who else is in the room.
func TestMentionsAreStrippedFromWhatVendorsReceive(t *testing.T) {
	for _, draft := range []string{
		"@codex @claude compare the two resume paths",
		"-@codex compare the two resume paths",
	} {
		if _, brief := ParseRoute(draft); strings.Contains(brief, "@") {
			t.Errorf("brief still carries addressing: %q", brief)
		}
	}
}

func TestNilRouteAddressesEveryone(t *testing.T) {
	var r Route
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity} {
		if !r.addresses(v) {
			t.Errorf("zero route excluded %s; the zero value must mean everyone", v)
		}
	}
}

func TestRouteExcludesUnaddressedVendors(t *testing.T) {
	r := to(model.VendorCodex)
	if !r.addresses(model.VendorCodex) {
		t.Error("route does not address the vendor it names")
	}
	if r.addresses(model.VendorClaude) {
		t.Error("route addresses a vendor it does not name")
	}
}

// TestANegatedRouteAddressesTheComplement is the whole feature in three
// assertions: the named seat is out, every other seat is in, and a seat this
// room has never heard of is in too — because an exclusion starts from
// "everyone seated", not from a list.
func TestANegatedRouteAddressesTheComplement(t *testing.T) {
	r := not(model.VendorClaude)
	if r.addresses(model.VendorClaude) {
		t.Error("an exclusion still addresses the seat it excludes")
	}
	for _, v := range []model.VendorID{model.VendorCodex, model.VendorAntigravity, model.VendorCursor} {
		if !r.addresses(v) {
			t.Errorf("-@claude dropped %s; it excludes one seat, not three", v)
		}
	}
}

// TestARefusedRouteAddressesNobody. dispatch stops on Route.Mixed before this
// is reached; the value is defined anyway so that a caller which forgets the
// check narrows to nothing rather than broadcasting to four metered quotas on a
// line the room has already said it cannot read.
func TestARefusedRouteAddressesNobody(t *testing.T) {
	r := Route{Mixed: true}
	for _, v := range addressableVendors() {
		if r.addresses(v) {
			t.Errorf("a refused route still addresses %s", v)
		}
	}
}

// TestEveryAddressableVendorIsExcludable stops -@all from silently meaning
// "the four seats this room happened to have when it was written". A fifth
// alias added to mentionAliases has to be added here too, or excluding everyone
// leaves one seat quietly in the room.
func TestEveryAddressableVendorIsExcludable(t *testing.T) {
	in := map[model.VendorID]bool{}
	for _, v := range addressableVendors() {
		in[v] = true
	}
	for alias, v := range mentionAliases() {
		if !in[v] {
			t.Errorf("@%s resolves to %s, which -@all cannot exclude", alias, v)
		}
	}
	if len(in) != len(addressableVendors()) {
		t.Error("addressableVendors repeats a vendor; -@all would name a seat twice")
	}
}

// TestFooterShowsRoutingBeforeDispatch is the honest-gauge rule applied to an
// action rather than a value: the user has to be able to see where a brief is
// going while there is still time to change it. Finding out afterwards means a
// wasted turn against three metered quotas.
func TestFooterShowsRoutingBeforeDispatch(t *testing.T) {
	st := room()
	st.Mode = ModeComposing

	st.Draft = "an unaddressed brief"
	st.Route, _ = ParseRoute(st.Draft)
	if got := render(st); !strings.Contains(got, "everyone") {
		t.Error("an unaddressed brief does not say it is convening everyone")
	}

	st.Draft = "@all convene"
	st.Route, _ = ParseRoute(st.Draft)
	if got := render(st); !strings.Contains(got, "everyone") {
		t.Error("@all does not say it is convening everyone")
	}

	st.Draft = "@codex just you"
	st.Route, _ = ParseRoute(st.Draft)
	got := render(st)
	if !strings.Contains(got, "codex") {
		t.Error("an addressed brief does not name its target")
	}
	if strings.Contains(got, "everyone") {
		t.Error("an addressed brief still claims it is going to everyone")
	}

	// The typo case, which is the whole reason this is on screen: an @typo does
	// not narrow, so it must read as going to the whole room — four quotas —
	// while there is still time to fix it, not silently to nobody.
	st.Draft = "@claud typo"
	st.Route, _ = ParseRoute(st.Draft)
	if got := render(st); !strings.Contains(got, "everyone") {
		t.Error("an unresolved mention does not read as going to everyone")
	}
}

// TestTheFooterRendersTheNegativeFormTruthfully. The cell is read before enter
// to check who is about to be billed, so an exclusion may not render as either
// of the two lies available to it: "everyone" (which is who it was BEFORE the
// -@) or "claude" (which is the one seat it is NOT going to).
func TestTheFooterRendersTheNegativeFormTruthfully(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Draft = "-@claude everyone else"
	st.Route, _ = ParseRoute(st.Draft)

	got := render(st)
	if !strings.Contains(got, "everyone but claude") {
		t.Errorf("the footer does not name the exclusion:\n%s", got)
	}

	// And the refusal is visible in the same cell, at the same moment — while
	// the line is still being typed, rather than as a notice after a keystroke
	// the user cannot take back.
	st.Draft = "@claude -@codex both forms"
	st.Route, _ = ParseRoute(st.Draft)
	if got := render(st); !strings.Contains(got, "mixed @ and -@") {
		t.Errorf("a refused route does not say so in the footer:\n%s", got)
	}
}

// TestAMixedRouteIsRefusedAtDispatchToo. The footer states the refusal while
// the line is being typed; this is the backstop for the user who typed it and
// pressed enter anyway. Nothing is sent, and the notice says which of the two
// forms to keep rather than only that something was wrong.
func TestAMixedRouteIsRefusedAtDispatchToo(t *testing.T) {
	m := traceModel()
	m.st.Draft = "@claude -@codex who is right?"
	if cmd := m.dispatch(); cmd != nil {
		t.Error("a brief the room said it could not route was dispatched anyway")
	}
	if !strings.Contains(m.st.Notice, "one form") {
		t.Errorf("notice = %q; it does not say what to do about it", m.st.Notice)
	}
}

// TestExcludingEverySeatedVendorLandsInTheExistingNotice. An exclusion that
// happens to name every seat in the room is the same situation as a mention
// that names only unseated ones: the turn reaches nobody. It gets the notice
// that situation already had rather than a second one of its own, because the
// user's problem is identical and so is the fix.
func TestExcludingEverySeatedVendorLandsInTheExistingNotice(t *testing.T) {
	for _, draft := range []string{"-@claude anyone else?", "-@all anyone at all?"} {
		// traceModel seats exactly one vendor: Claude.
		m := traceModel()
		m.st.Draft = draft
		if cmd := m.dispatch(); cmd != nil {
			t.Errorf("%q dispatched a turn to nobody", draft)
		}
		if m.st.Notice != "none of the vendors you addressed are seated" {
			t.Errorf("%q: notice = %q, want the existing not-seated notice", draft, m.st.Notice)
		}
	}
}

// TestTheRouteSurvivesTheNarrowestFooter. The route is FIRST on the compose
// mode line precisely so the two-copy truncation rule (statusLine) eats the
// keybindings before it eats the destination — the keys are recoverable from
// the help panel and a mis-sent turn is not. The negative form is the longest
// this cell gets, so it is the one worth pinning.
func TestTheRouteSurvivesTheNarrowestFooter(t *testing.T) {
	st := room()
	st.Mode = ModeComposing
	st.Width = 60
	st.Draft = "-@claude everyone else"
	st.Route, _ = ParseRoute(st.Draft)

	if got := render(st); !strings.Contains(got, "everyone but claude") {
		t.Errorf("the exclusion was elided out of a 60-column footer:\n%s", got)
	}
}
