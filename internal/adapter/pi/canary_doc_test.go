package pi

import (
	"os"
	"strings"
	"testing"
)

// TestTheCanaryInventoryMatchesThisAdapter pins design.md §3.10 against this
// adapter. The parent adds the inventory row after this package exists. Until
// that row lands, this test skips rather than edit design.md.
func TestTheCanaryInventoryMatchesThisAdapter(t *testing.T) {
	b, err := os.ReadFile("../../../docs/design.md")
	if err != nil {
		t.Fatalf("cannot read the design doc that documents this adapter: %v", err)
	}
	doc := string(b)

	if !piInventoryRow(doc) {
		t.Skip("design.md §3.10 has no Pi row; parent must add the inventory row")
	}
	if !strings.Contains(doc, VerifiedAgainst) {
		t.Errorf("design.md §3.10 does not name the version this adapter is pinned to (%q)", VerifiedAgainst)
	}
	if !strings.Contains(doc, canarySessionHeaderID.Name) {
		t.Errorf("design.md §3.10 does not name the %q canary; the inventory has drifted from the adapter", canarySessionHeaderID.Name)
	}
}

// piInventoryRow reports a Pi adapter row in §3.10. A search of the whole
// file would hit §3.9b, which is the survey, not the inventory.
func piInventoryRow(doc string) bool {
	i := strings.Index(doc, "### 3.10")
	if i < 0 {
		return false
	}
	rest := doc[i:]
	if j := strings.Index(rest[4:], "\n### "); j >= 0 {
		rest = rest[:4+j]
	}
	return strings.Contains(rest, "| Pi ") || strings.Contains(rest, "| Pi |")
}
