package cursor

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

	if !strings.Contains(doc, VerifiedAgainst) {
		t.Errorf("design.md §3.10 does not name the version this adapter is pinned to (%q)", VerifiedAgainst)
	}
	for _, c := range []string{canaryRowClock.Name, canaryChatsClock.Name} {
		if !strings.Contains(doc, c) {
			t.Errorf("design.md §3.10 does not name the %q canary; the inventory has drifted from the adapter", c)
		}
	}
	// The CLI half carries its own pin, because it reads a store a different
	// program writes on a numbering scheme of its own. §3.10's `verified
	// against` cell cannot hold it — internal/adapter/pins keeps one row per
	// vendor — so the doc has to name it somewhere for it to be checkable at
	// all.
	if !strings.Contains(doc, chatsVerifiedAgainst) {
		t.Errorf("design.md does not name the version the CLI manifest reader is pinned to (%q)", chatsVerifiedAgainst)
	}
}
