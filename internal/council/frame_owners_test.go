package council

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// TestFrameOwnersForEqualOnEveryone pins Phase 5: @all / zero route leaves
// FrameOwners empty so the equal grid (comparison) is intact.
func TestFrameOwnersForEqualOnEveryone(t *testing.T) {
	st := room()
	if got := frameOwnersFor(Route{}, st); got != nil {
		t.Errorf("everyone route owners = %v, want nil (equal columns)", got)
	}
	all := Route{Vendors: []model.VendorID{
		model.VendorClaude, model.VendorCodex, model.VendorAntigravity,
	}}
	// room() seats three; addressing all three is still equal.
	if got := frameOwnersFor(all, st); got != nil {
		t.Errorf("all-seated positive route owners = %v, want nil", got)
	}
}

// TestFrameOwnersForNarrowsToAddressedSeats: a single-seat dispatch owns the
// frame; the others become strips until the next enter.
func TestFrameOwnersForNarrowsToAddressedSeats(t *testing.T) {
	st := room()
	got := frameOwnersFor(Route{Vendors: []model.VendorID{model.VendorCodex}}, st)
	if len(got) != 1 || got[0] != model.VendorCodex {
		t.Fatalf("owners = %v, want [codex]", got)
	}
}

// TestWeightedLayoutGivesAddressedSeatsMoreWidth: intent controls geometry.
func TestWeightedLayoutGivesAddressedSeatsMoreWidth(t *testing.T) {
	st := room()
	st.Width, st.Height = 120, 24
	st.FrameOwners = []model.VendorID{model.VendorClaude}
	lay := layoutFor(st, GlyphsFor(false))
	if len(lay.ColWidths) != 3 {
		t.Fatalf("ColWidths = %v, want 3 entries", lay.ColWidths)
	}
	if lay.ColWidths[0] <= lay.ColWidths[1] || lay.ColWidths[0] <= lay.ColWidths[2] {
		t.Errorf("primary width %d should beat strips %d and %d",
			lay.ColWidths[0], lay.ColWidths[1], lay.ColWidths[2])
	}
	if lay.ColWidths[1] != stripColumn || lay.ColWidths[2] != stripColumn {
		t.Errorf("strips = %d,%d want %d", lay.ColWidths[1], lay.ColWidths[2], stripColumn)
	}

	// @all clears weighting.
	st.FrameOwners = nil
	eq := layoutFor(st, GlyphsFor(false))
	if len(eq.ColWidths) != 0 {
		t.Errorf("equal frame still weighted: %v", eq.ColWidths)
	}
}

// TestExpandedOutranksFrameOwners: f is the manual override.
func TestExpandedOutranksFrameOwners(t *testing.T) {
	st := room()
	st.Width, st.Height = 120, 24
	st.FrameOwners = []model.VendorID{model.VendorClaude}
	st.Expanded = true
	lay := layoutFor(st, GlyphsFor(false))
	if lay.Tier != TierTabs || lay.Cols != 1 {
		t.Fatalf("expanded lay = tier %d cols %d, want tabs/1", lay.Tier, lay.Cols)
	}
	if len(lay.ColWidths) != 0 {
		t.Errorf("expanded frame still carried ColWidths: %v", lay.ColWidths)
	}
}

// TestNarrowRouteFrameIsStableInRender: geometry does not depend on phase —
// activity must not reflow.
func TestNarrowRouteFrameIsStableInRender(t *testing.T) {
	st := room()
	st.Width, st.Height = 120, 24
	st.FrameOwners = []model.VendorID{model.VendorClaude}
	st.Columns[0].Phase = PhaseStreaming
	st.Columns[0].Body = "streaming answer that should wrap in the wide column"
	a := layoutFor(st, GlyphsFor(false)).ColWidths
	st.Columns[0].Phase = PhaseDone
	b := layoutFor(st, GlyphsFor(false)).ColWidths
	if len(a) != len(b) {
		t.Fatalf("widths changed length across phases: %v vs %v", a, b)
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("widths reflowed on phase change: %v → %v", a, b)
		}
	}
	got := render(st)
	if !strings.Contains(got, "streaming answer") && !strings.Contains(got, "Claude") {
		t.Error("weighted frame failed to render the primary seat")
	}
}
