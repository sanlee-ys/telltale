package antigravity

import (
	"os"
	"strings"
	"testing"
)

// TestTheCanaryInventoryMatchesThisAdapter pins design.md §3.10 against this
// adapter. See the codex copy for why the inventory is centralised and why it
// needs a guard beside each truth it copies.
func TestTheCanaryInventoryMatchesThisAdapter(t *testing.T) {
	b, err := os.ReadFile("../../../docs/design.md")
	if err != nil {
		t.Fatalf("cannot read the design doc that documents this adapter: %v", err)
	}
	doc := string(b)

	if !strings.Contains(doc, verifiedAgainst) {
		t.Errorf("design.md §3.10 does not name the version this adapter is pinned to (%q)", verifiedAgainst)
	}
	for _, c := range []string{canaryGenMetadata.Name, canaryTrajectory.Name} {
		if !strings.Contains(doc, c) {
			t.Errorf("design.md §3.10 does not name the %q canary; the inventory has drifted from the adapter", c)
		}
	}
}
