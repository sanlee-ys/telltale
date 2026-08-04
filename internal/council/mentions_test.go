package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

func TestParseRoute(t *testing.T) {
	cases := []struct {
		name  string
		draft string
		want  Route
		brief string
	}{
		{
			// The fleet contract, encoded: an unaddressed brief goes to the
			// control-plane lane alone, not to all three. Broadcasting "hello"
			// to a review lane and a tiebreak lane spends two constrained pools
			// on nothing.
			name:  "no mention goes to claude alone, not everyone",
			draft: "should we resume or re-send?",
			want:  Route{model.VendorClaude},
			brief: "should we resume or re-send?",
		},
		{
			name:  "one mention",
			draft: "@codex is the resume flag set right?",
			want:  Route{model.VendorCodex},
			brief: "is the resume flag set right?",
		},
		{
			name:  "several mentions",
			draft: "@claude @agy compare these two",
			want:  Route{model.VendorClaude, model.VendorAntigravity},
			brief: "compare these two",
		},
		{
			name:  "commas between mentions are how people write a list",
			draft: "@claude, @codex: who is right?",
			want:  Route{model.VendorClaude, model.VendorCodex},
			brief: "who is right?",
		},
		{
			name:  "both spellings of antigravity",
			draft: "@antigravity thoughts?",
			want:  Route{model.VendorAntigravity},
			brief: "thoughts?",
		},
		{
			name:  "case is not significant",
			draft: "@Claude @CODEX go",
			want:  Route{model.VendorClaude, model.VendorCodex},
			brief: "go",
		},
		{
			name:  "a repeated mention seats the vendor once",
			draft: "@codex @codex go",
			want:  Route{model.VendorCodex},
			brief: "go",
		},
		{
			// @all is now the ONLY way to convene the panel. Silence is no
			// longer a vote for everyone.
			name:  "@all convenes the whole room",
			draft: "@all what do you think?",
			want:  nil,
			brief: "what do you think?",
		},
		{
			// The rule that keeps routing predictable. Treating any @token
			// anywhere as routing would silently drop a vendor from a turn
			// because of a word in the middle of a sentence.
			name:  "a mention mid-sentence is prose, not routing",
			draft: "ask @claude about the resume flag",
			want:  Route{model.VendorClaude},
			brief: "ask @claude about the resume flag",
		},
		{
			// An unresolvable token is left alone rather than erroring. The
			// footer shows the resolved routing while typing, so a typo reads
			// as "going to everyone" before enter rather than after.
			name:  "an unknown mention stays in the brief",
			draft: "@claud fix the typo",
			want:  Route{model.VendorClaude},
			brief: "@claud fix the typo",
		},
		{
			name:  "a known mention followed by an unknown one stops at the unknown",
			draft: "@codex @nobody go",
			want:  Route{model.VendorCodex},
			brief: "@nobody go",
		},
		{
			name:  "leading whitespace does not defeat a mention",
			draft: "   @agy  go",
			want:  Route{model.VendorAntigravity},
			brief: "go",
		},
		{
			name:  "a mention with no brief leaves an empty brief",
			draft: "@codex",
			want:  Route{model.VendorCodex},
			brief: "",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, brief := ParseRoute(c.draft)
			if len(got) != len(c.want) {
				t.Fatalf("route = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("route = %v, want %v", got, c.want)
				}
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
	_, brief := ParseRoute("@codex @claude compare the two resume paths")
	if strings.Contains(brief, "@") {
		t.Errorf("brief still carries addressing: %q", brief)
	}
}

func TestNilRouteAddressesEveryone(t *testing.T) {
	var r Route
	for _, v := range []model.VendorID{model.VendorClaude, model.VendorCodex, model.VendorAntigravity} {
		if !r.addresses(v) {
			t.Errorf("nil route excluded %s; nil must mean everyone", v)
		}
	}
}

func TestRouteExcludesUnaddressedVendors(t *testing.T) {
	r := Route{model.VendorCodex}
	if !r.addresses(model.VendorCodex) {
		t.Error("route does not address the vendor it names")
	}
	if r.addresses(model.VendorClaude) {
		t.Error("route addresses a vendor it does not name")
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
	if got := render(st); !strings.Contains(got, "claude") {
		t.Error("an unaddressed brief does not name the default lane")
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

	// The typo case, which is the whole reason this is on screen: it must read
	// as going to the default lane, not silently to nobody.
	st.Draft = "@claud typo"
	st.Route, _ = ParseRoute(st.Draft)
	if got := render(st); !strings.Contains(got, "claude") {
		t.Error("an unresolved mention does not read as going to the default lane")
	}
}
