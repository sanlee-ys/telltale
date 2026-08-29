package history

import (
	"strings"
	"testing"

	"github.com/sanlee-ys/telltale/internal/model"
)

// fleetOrder is the order every per-vendor list in this product renders in
// (internal/hud/view.go's own `fleetOrder`, minus the self-reported row).
//
// It is restated here rather than imported because that variable is unexported
// and internal/hud is a TUI package this one must not link. A restated constant
// is exactly the copy that goes stale, which is why this file's first test pins
// the two against each other by CONTENT: a vendor added to the fleet and not to
// the survey fails here, naming the vendor.
//
// self-reported is deliberately absent and is not an omission. It is the
// drop-file relay (§7.23) — rows a tool telltale ships no adapter for wrote
// about itself. There is no session store behind it to walk, so it has no
// answer to either of the survey's two questions, and giving it a "not covered"
// verdict would imply there is a file somewhere this mode could learn to read.
var fleetOrder = []model.VendorID{
	model.VendorClaude, model.VendorCodex, model.VendorGemini,
	model.VendorAntigravity, model.VendorCursor, model.VendorGrok,
	model.VendorPi,
}

// TestEveryFleetVendorHasAVerdict is the gate that stops this mode from going
// quiet about a vendor.
//
// The failure it exists to catch is an eighth vendor arriving: an adapter lands,
// the HUD grows a row for it, and `telltale history` says nothing about it at
// all — which on this surface reads as "that vendor spent nothing", because the
// coverage block is the only thing separating a one-vendor table from a fleet
// answer. Silence is the one output this mode may never produce about a vendor.
func TestEveryFleetVendorHasAVerdict(t *testing.T) {
	for _, v := range fleetOrder {
		if _, ok := Verdict(v); !ok {
			t.Errorf("the fleet has %s and the survey has no verdict for it.\n"+
				"An unnamed vendor's silence on this surface reads as zero. Add a row to\n"+
				"survey.go saying whether its counts reach disk and whether anything dates them.", v)
		}
	}
	if got, want := len(Survey()), len(fleetOrder); got != want {
		t.Errorf("the survey has %d rows and the fleet has %d vendors; one of the two moved", got, want)
	}
}

// TestTheSurveyIsInFleetOrder. Every per-vendor list in this product walks one
// order so a vendor sits in the same place on every surface (§7.17). Ordering
// this one by how close each vendor is to coverage was considered and declined —
// survey.go's doc carries the argument — and this test is what keeps the decision
// from being quietly reversed by an edit that inserts a row where it reads best.
func TestTheSurveyIsInFleetOrder(t *testing.T) {
	rows := Survey()
	if len(rows) != len(fleetOrder) {
		t.Fatalf("survey has %d rows, fleet has %d", len(rows), len(fleetOrder))
	}
	for i, v := range fleetOrder {
		if rows[i].Vendor != v {
			t.Errorf("survey position %d is %s, fleet order wants %s", i, rows[i].Vendor, v)
		}
	}
}

// TestEveryVerdictNamesWhatItLookedAt. "Not supported" sends a reader to open an
// issue about work that is already understood; a verdict that names the field
// and the file lets them see whether the gap is telltale's or the vendor's.
//
// The check is structural rather than a word list: a verdict must be a real
// sentence and must name at least one concrete thing — a field, a file, or a
// design-doc section. That is loose on purpose. A tighter test would pin the
// prose and would then have to be edited every time a verdict is reworded, which
// is how a test stops being read.
func TestEveryVerdictNamesWhatItLookedAt(t *testing.T) {
	concrete := []string{"_", ".", "§"}
	for _, c := range Survey() {
		if len(strings.Fields(c.Why)) < 10 {
			t.Errorf("%s's verdict is %d words. It has to say what was looked at and what "+
				"was missing, not just that the answer was no.", c.Vendor, len(strings.Fields(c.Why)))
		}
		named := false
		for _, mark := range concrete {
			if strings.Contains(c.Why, mark) {
				named = true
			}
		}
		if !named {
			t.Errorf("%s's verdict names no field, file or design-doc section: %q", c.Vendor, c.Why)
		}
	}
}

// TestAtLeastOneVendorIsCovered. A survey where nothing is covered is a mode
// that prints a coverage block and no ledger — which would compile, pass every
// other test in this package, and be useless.
func TestAtLeastOneVendorIsCovered(t *testing.T) {
	if len(CoveredVendors()) == 0 {
		t.Fatal("no vendor is covered, so this mode reads nothing")
	}
}

// TestSurveyHandsOutACopy. A package-level slice returned directly is one
// caller's append away from a second caller seeing a table it did not build —
// the aliasing bug internal/adapter/claudecode copies its Extras to avoid.
func TestSurveyHandsOutACopy(t *testing.T) {
	a := Survey()
	if len(a) == 0 {
		t.Fatal("empty survey")
	}
	a[0].Vendor = "tampered"
	if Survey()[0].Vendor == "tampered" {
		t.Error("Survey() aliases the package-level table; one caller can rewrite every other's")
	}
}
